package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// StaticProvider：静态代理列表
// ============================================================================

// staticProvider 从硬编码的代理列表提供代理。
// 适用于测试或代理列表固定的场景。
type staticProvider struct {
	proxies []string
}

// NewStaticProvider 创建一个静态代理列表 Provider。
// proxies 是代理 URL 字符串切片，例如：
//
//	[]string{
//	    "http://user:pass@proxy1.example.com:8080",
//	    "http://proxy2.example.com:8080",
//	}
func NewStaticProvider(proxies []string) Provider {
	// 拷贝一份切片防止外部修改
	cp := make([]string, len(proxies))
	copy(cp, proxies)
	return &staticProvider{proxies: cp}
}

// Fetch 返回静态代理列表的副本。
func (p *staticProvider) Fetch(_ context.Context) ([]string, error) {
	out := make([]string, len(p.proxies))
	copy(out, p.proxies)
	return out, nil
}

// Name 返回 Provider 名称。
func (p *staticProvider) Name() string { return "static" }

// ============================================================================
// FileProvider：从本地文件加载
// ============================================================================

// fileProvider 从本地文件每行一个代理的方式加载代理列表。
// 支持 # 开头的注释行和空行。
type fileProvider struct {
	path string
}

// NewFileProvider 创建一个文件代理列表 Provider。
//
// 文件格式：
//   - 每行一个代理 URL
//   - 以 # 开头的行视为注释
//   - 空行被忽略
//
// 示例：
//
//	# 主代理
//	http://user:pass@proxy1.example.com:8080
//	http://proxy2.example.com:8080
func NewFileProvider(path string) Provider {
	return &fileProvider{path: path}
}

// Fetch 读取文件并解析代理列表。
// 文件读取错误会被返回，调用方可决定是否重试。
func (p *fileProvider) Fetch(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	f, err := os.Open(p.path)
	if err != nil {
		return nil, fmt.Errorf("proxy file provider: open %q: %w", p.path, err)
	}
	defer f.Close()

	var proxies []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		proxies = append(proxies, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("proxy file provider: scan %q: %w", p.path, err)
	}
	return proxies, nil
}

// Name 返回 Provider 名称。
func (p *fileProvider) Name() string { return "file" }

// ============================================================================
// HTTPAPIProvider：从 HTTP API 拉取
// ============================================================================

// HTTPAPIProviderOptions 配置 HTTPAPIProvider 的行为。
type HTTPAPIProviderOptions struct {
	// URL 是代理列表 API 的请求地址。
	URL string

	// Method 是 HTTP 方法，默认 GET。
	Method string

	// Headers 是附加请求头（可选）。
	Headers map[string]string

	// Body 是请求体（POST 等场景使用，可选）。
	Body []byte

	// Timeout 是单次拉取请求的超时时间。
	// 默认 10s。
	Timeout time.Duration

	// ResponseFormat 指定 API 响应解析格式。
	// 支持 "json_array"（默认）/ "json_field" / "lines"。
	ResponseFormat string

	// JSONFieldPath 仅 json_field 格式使用，
	// 指定代理列表所在的字段名（如 "data.proxies"）。
	JSONFieldPath string
}

// httpAPIProvider 从 HTTP API 拉取代理列表。
type httpAPIProvider struct {
	opts   *HTTPAPIProviderOptions
	client *http.Client
}

// NewHTTPAPIProvider 创建一个 HTTP API 代理列表 Provider。
//
// 支持三种响应格式：
//
//   - "json_array"（默认）: 响应体为字符串数组，例如：
//     ["http://proxy1:8080", "http://proxy2:8080"]
//
//   - "json_field": 响应体为对象，代理列表位于指定字段（点分路径），例如：
//     {"code":0,"data":{"proxies":["http://proxy1:8080"]}}
//     此时设置 JSONFieldPath = "data.proxies"
//
//   - "lines": 响应体为纯文本，每行一个代理 URL。
func NewHTTPAPIProvider(opts *HTTPAPIProviderOptions) (Provider, error) {
	if opts == nil {
		return nil, fmt.Errorf("proxy http api provider: options is nil")
	}
	if opts.URL == "" {
		return nil, fmt.Errorf("proxy http api provider: URL is empty")
	}

	// 复制一份 opts 避免外部修改
	cp := *opts
	if cp.Method == "" {
		cp.Method = http.MethodGet
	}
	if cp.Timeout <= 0 {
		cp.Timeout = 10 * time.Second
	}
	if cp.ResponseFormat == "" {
		cp.ResponseFormat = "json_array"
	}
	switch cp.ResponseFormat {
	case "json_array", "json_field", "lines":
	default:
		return nil, fmt.Errorf("proxy http api provider: unsupported ResponseFormat %q", cp.ResponseFormat)
	}
	if cp.ResponseFormat == "json_field" && cp.JSONFieldPath == "" {
		return nil, fmt.Errorf("proxy http api provider: JSONFieldPath required for json_field format")
	}

	return &httpAPIProvider{
		opts: &cp,
		client: &http.Client{
			Timeout: cp.Timeout,
		},
	}, nil
}

// Fetch 调用 HTTP API 并解析响应。
func (p *httpAPIProvider) Fetch(ctx context.Context) ([]string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()

	var bodyReader io.Reader
	if len(p.opts.Body) > 0 {
		bodyReader = strings.NewReader(string(p.opts.Body))
	}

	req, err := http.NewRequestWithContext(reqCtx, p.opts.Method, p.opts.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("proxy http api provider: build request: %w", err)
	}
	for k, v := range p.opts.Headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy http api provider: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("proxy http api provider: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("proxy http api provider: read body: %w", err)
	}

	switch p.opts.ResponseFormat {
	case "json_array":
		return parseJSONArray(body)
	case "json_field":
		return parseJSONField(body, p.opts.JSONFieldPath)
	case "lines":
		return parseLines(body), nil
	default:
		return nil, fmt.Errorf("proxy http api provider: unsupported format %q", p.opts.ResponseFormat)
	}
}

// Name 返回 Provider 名称。
func (p *httpAPIProvider) Name() string { return "http_api" }

// parseJSONArray 解析 ["url1","url2"] 格式。
func parseJSONArray(body []byte) ([]string, error) {
	var arr []string
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, fmt.Errorf("proxy http api provider: parse json_array: %w", err)
	}
	return arr, nil
}

// parseJSONField 按点分路径从 JSON 对象中提取字符串数组。
func parseJSONField(body []byte, path string) ([]string, error) {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("proxy http api provider: parse json_field: %w", err)
	}

	cur := raw
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("proxy http api provider: path %q not found at segment %q", path, seg)
		}
		v, exists := m[seg]
		if !exists {
			return nil, fmt.Errorf("proxy http api provider: path %q segment %q not exists", path, seg)
		}
		cur = v
	}

	arr, ok := cur.([]any)
	if !ok {
		return nil, fmt.Errorf("proxy http api provider: field %q is not an array", path)
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("proxy http api provider: array element is not string")
		}
		out = append(out, s)
	}
	return out, nil
}

// parseLines 解析按行分隔的代理 URL，忽略空行和注释。
func parseLines(body []byte) []string {
	var proxies []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		proxies = append(proxies, line)
	}
	return proxies
}

// ============================================================================
// CompositeProvider：组合多个 Provider
// ============================================================================

// compositeProvider 串联多个 Provider，将它们的结果合并去重。
type compositeProvider struct {
	providers []Provider
	mu        sync.Mutex
}

// NewCompositeProvider 创建一个组合 Provider，
// 同时从多个 Provider 拉取代理列表并合并。
//
// 任何子 Provider 返回错误时整体返回错误，但已成功的代理不会丢弃。
func NewCompositeProvider(providers ...Provider) Provider {
	return &compositeProvider{providers: providers}
}

// Fetch 串行拉取所有 Provider 的代理列表。
//
// 串行而非并发是为了：
//   - Provider 列表通常较少（1-3 个），并发收益有限
//   - 避免 goroutine 泄漏与错误聚合复杂度
//   - context 取消语义更清晰
func (p *compositeProvider) Fetch(ctx context.Context) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	seen := make(map[string]struct{})
	var merged []string
	var firstErr error

	for _, sub := range p.providers {
		if err := ctx.Err(); err != nil {
			return merged, err
		}
		list, err := sub.Fetch(ctx)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("proxy composite: provider %q: %w", sub.Name(), err)
			continue
		}
		for _, raw := range list {
			if _, ok := seen[raw]; ok {
				continue
			}
			seen[raw] = struct{}{}
			merged = append(merged, raw)
		}
	}

	return merged, firstErr
}

// Name 返回 Provider 名称。
func (p *compositeProvider) Name() string { return "composite" }
