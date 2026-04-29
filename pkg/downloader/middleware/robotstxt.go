package middleware

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// RobotsTxtMiddleware 实现 robots.txt 遵守中间件。
// 对应 Scrapy 的 RobotsTxtMiddleware，注册优先级 100。
//
// 功能：
//   - 按 netloc（scheme://host:port）缓存 robots.txt 解析结果
//   - 使用 sync.Once + sync.WaitGroup 确保每个 netloc 只下载一次 robots.txt
//   - 被 robots.txt 禁止的请求返回 ErrIgnoreRequest
//   - 支持 ROBOTSTXT_OBEY 配置开关
//   - 支持 ROBOTSTXT_USER_AGENT 配置自定义 User-Agent
//   - 支持 Request.Meta["dont_obey_robotstxt"] 跳过检查
//
// 配置项：
//   - ROBOTSTXT_OBEY: 是否启用 robots.txt 遵守（默认 false）
//   - ROBOTSTXT_USER_AGENT: 用于 robots.txt 匹配的 User-Agent（默认使用请求的 User-Agent）
//   - USER_AGENT: 默认 User-Agent（当请求头中无 User-Agent 时使用）
type RobotsTxtMiddleware struct {
	BaseDownloaderMiddleware

	// mu 保护 parsers map 的并发访问
	mu sync.RWMutex

	// parsers 按 netloc 缓存 robots.txt 解析结果。
	// value 为 *robotsData，包含解析结果和加载状态。
	parsers map[string]*robotsData

	// defaultUserAgent 是默认的 User-Agent（来自 USER_AGENT 配置）。
	defaultUserAgent string

	// robotstxtUserAgent 是用于 robots.txt 匹配的自定义 User-Agent。
	// 为空时使用请求的 User-Agent 头。
	robotstxtUserAgent string

	// httpClient 用于下载 robots.txt 文件。
	httpClient *http.Client

	// stats 统计收集器。
	stats stats.Collector

	// logger 日志记录器。
	logger *slog.Logger
}

// robotsData 存储单个 netloc 的 robots.txt 解析结果。
type robotsData struct {
	// once 确保只下载和解析一次。
	once sync.Once

	// wg 用于等待下载完成。
	// 第一个请求触发下载，后续请求等待 wg.Done()。
	wg sync.WaitGroup

	// parser 是解析后的 robots.txt 规则。
	// 为 nil 表示下载失败或无 robots.txt。
	parser *robotsRules
}

// robotsRules 封装 robots.txt 解析结果。
// 使用简单的规则匹配实现，不依赖外部库。
type robotsRules struct {
	// groups 按 User-Agent 分组的规则。
	groups []*robotsGroup
}

// robotsGroup 表示 robots.txt 中一个 User-Agent 分组。
type robotsGroup struct {
	// agents 是该分组匹配的 User-Agent 列表（小写）。
	agents []string

	// disallow 是禁止的路径前缀列表。
	disallow []string

	// allow 是允许的路径前缀列表（优先级高于 disallow）。
	allow []string
}

// RobotsTxtOption 是 RobotsTxtMiddleware 的配置选项。
type RobotsTxtOption func(*RobotsTxtMiddleware)

// WithRobotsTxtUserAgent 设置用于 robots.txt 匹配的 User-Agent。
func WithRobotsTxtUserAgent(ua string) RobotsTxtOption {
	return func(m *RobotsTxtMiddleware) {
		m.robotstxtUserAgent = ua
	}
}

// WithRobotsTxtDefaultUserAgent 设置默认 User-Agent。
func WithRobotsTxtDefaultUserAgent(ua string) RobotsTxtOption {
	return func(m *RobotsTxtMiddleware) {
		m.defaultUserAgent = ua
	}
}

// WithRobotsTxtHTTPClient 设置自定义 HTTP 客户端。
func WithRobotsTxtHTTPClient(client *http.Client) RobotsTxtOption {
	return func(m *RobotsTxtMiddleware) {
		m.httpClient = client
	}
}

// NewRobotsTxtMiddleware 创建一个新的 RobotsTxt 中间件。
func NewRobotsTxtMiddleware(sc stats.Collector, logger *slog.Logger, opts ...RobotsTxtOption) *RobotsTxtMiddleware {
	if logger == nil {
		logger = slog.Default()
	}
	if sc == nil {
		sc = stats.NewDummyCollector()
	}

	m := &RobotsTxtMiddleware{
		parsers:          make(map[string]*robotsData),
		defaultUserAgent: "scrapy-go",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			// 禁用自动重定向，手动处理
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		stats:  sc,
		logger: logger,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// ProcessRequest 在请求发送前检查 robots.txt 规则。
// 如果请求被 robots.txt 禁止，返回 ErrIgnoreRequest。
func (m *RobotsTxtMiddleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	// 检查 Meta 中的 dont_obey_robotstxt 标记
	if val, ok := request.GetMeta("dont_obey_robotstxt"); ok {
		if boolVal, ok := val.(bool); ok && boolVal {
			return nil, nil
		}
	}

	// 跳过 data: 和 file: 协议
	urlStr := request.URL.String()
	if strings.HasPrefix(urlStr, "data:") || strings.HasPrefix(urlStr, "file:") {
		return nil, nil
	}

	// 获取或下载 robots.txt
	rules := m.getRobotsRules(ctx, request.URL)

	// 如果没有 robots.txt 规则（下载失败或不存在），允许访问
	if rules == nil {
		return nil, nil
	}

	// 确定用于匹配的 User-Agent
	userAgent := m.getUserAgent(request)

	// 检查是否允许访问
	if !rules.isAllowed(request.URL.Path, userAgent) {
		m.logger.Debug("forbidden by robots.txt",
			"url", request.URL.String(),
			"user_agent", userAgent,
		)
		m.stats.IncValue("robotstxt/forbidden", 1, 0)
		return nil, fmt.Errorf("%w: forbidden by robots.txt: %s", serrors.ErrIgnoreRequest, request.URL.String())
	}

	return nil, nil
}

// getUserAgent 确定用于 robots.txt 匹配的 User-Agent。
// 优先级：ROBOTSTXT_USER_AGENT > 请求头 User-Agent > USER_AGENT 默认值
func (m *RobotsTxtMiddleware) getUserAgent(request *shttp.Request) string {
	if m.robotstxtUserAgent != "" {
		return m.robotstxtUserAgent
	}

	if request.Headers != nil {
		if ua := request.Headers.Get("User-Agent"); ua != "" {
			return ua
		}
	}

	return m.defaultUserAgent
}

// getRobotsRules 获取指定 URL 对应的 robots.txt 规则。
// 使用 sync.Once 确保每个 netloc 只下载一次。
func (m *RobotsTxtMiddleware) getRobotsRules(ctx context.Context, u *url.URL) *robotsRules {
	netloc := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	// 快速路径：检查是否已缓存
	m.mu.RLock()
	data, exists := m.parsers[netloc]
	m.mu.RUnlock()

	if !exists {
		// 慢路径：创建新条目
		m.mu.Lock()
		// 双重检查
		data, exists = m.parsers[netloc]
		if !exists {
			data = &robotsData{}
			data.wg.Add(1)
			m.parsers[netloc] = data
		}
		m.mu.Unlock()

		if !exists {
			// 当前 goroutine 负责下载
			go func() {
				defer data.wg.Done()
				data.once.Do(func() {
					data.parser = m.downloadRobotsTxt(ctx, netloc)
				})
			}()
		}
	}

	// 等待下载完成
	data.wg.Wait()
	return data.parser
}

// downloadRobotsTxt 下载并解析指定 netloc 的 robots.txt。
func (m *RobotsTxtMiddleware) downloadRobotsTxt(ctx context.Context, netloc string) *robotsRules {
	robotsURL := netloc + "/robots.txt"

	m.stats.IncValue("robotstxt/request_count", 1, 0)

	req, err := http.NewRequestWithContext(ctx, "GET", robotsURL, nil)
	if err != nil {
		m.logger.Error("failed to create robots.txt request",
			"url", robotsURL,
			"error", err,
		)
		m.stats.IncValue("robotstxt/exception_count", 1, 0)
		return nil
	}

	// 设置 User-Agent
	if m.robotstxtUserAgent != "" {
		req.Header.Set("User-Agent", m.robotstxtUserAgent)
	} else {
		req.Header.Set("User-Agent", m.defaultUserAgent)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		m.logger.Debug("failed to download robots.txt",
			"url", robotsURL,
			"error", err,
		)
		m.stats.IncValue("robotstxt/exception_count", 1, 0)
		return nil
	}
	defer resp.Body.Close()

	m.stats.IncValue("robotstxt/response_count", 1, 0)
	m.stats.IncValue(fmt.Sprintf("robotstxt/response_status_count/%d", resp.StatusCode), 1, 0)

	// 非 2xx 状态码视为无 robots.txt
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		m.logger.Debug("robots.txt returned non-2xx status",
			"url", robotsURL,
			"status", resp.StatusCode,
		)
		return nil
	}

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		m.logger.Error("failed to read robots.txt body",
			"url", robotsURL,
			"error", err,
		)
		m.stats.IncValue("robotstxt/exception_count", 1, 0)
		return nil
	}

	// 解析 robots.txt
	rules := parseRobotsTxt(string(body))
	return rules
}

// parseRobotsTxt 解析 robots.txt 内容。
// 实现标准的 robots.txt 解析规则：
//   - User-agent: 指定适用的爬虫
//   - Disallow: 禁止访问的路径
//   - Allow: 允许访问的路径（优先级高于 Disallow）
func parseRobotsTxt(content string) *robotsRules {
	rules := &robotsRules{}
	var currentGroup *robotsGroup

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		// 去除注释
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		// 解析指令
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		directive := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch directive {
		case "user-agent":
			// 新的 User-Agent 分组
			if currentGroup == nil || len(currentGroup.disallow) > 0 || len(currentGroup.allow) > 0 {
				// 如果当前分组已有规则，创建新分组
				currentGroup = &robotsGroup{}
				rules.groups = append(rules.groups, currentGroup)
			}
			currentGroup.agents = append(currentGroup.agents, strings.ToLower(value))

		case "disallow":
			if currentGroup == nil {
				currentGroup = &robotsGroup{agents: []string{"*"}}
				rules.groups = append(rules.groups, currentGroup)
			}
			if value != "" {
				currentGroup.disallow = append(currentGroup.disallow, value)
			}

		case "allow":
			if currentGroup == nil {
				currentGroup = &robotsGroup{agents: []string{"*"}}
				rules.groups = append(rules.groups, currentGroup)
			}
			if value != "" {
				currentGroup.allow = append(currentGroup.allow, value)
			}
		}
	}

	return rules
}

// isAllowed 检查指定路径是否允许指定 User-Agent 访问。
func (r *robotsRules) isAllowed(path string, userAgent string) bool {
	if path == "" {
		path = "/"
	}

	ua := strings.ToLower(userAgent)

	// 查找匹配的分组
	group := r.findGroup(ua)
	if group == nil {
		// 没有匹配的规则，默认允许
		return true
	}

	// 检查 Allow 规则（优先级高于 Disallow）
	// 使用最长匹配原则
	longestAllow := 0
	longestDisallow := 0

	for _, pattern := range group.allow {
		if matchPath(path, pattern) {
			if len(pattern) > longestAllow {
				longestAllow = len(pattern)
			}
		}
	}

	for _, pattern := range group.disallow {
		if matchPath(path, pattern) {
			if len(pattern) > longestDisallow {
				longestDisallow = len(pattern)
			}
		}
	}

	// 如果有 Allow 匹配且长度 >= Disallow 匹配，则允许
	if longestAllow > 0 && longestAllow >= longestDisallow {
		return true
	}

	// 如果有 Disallow 匹配，则禁止
	if longestDisallow > 0 {
		return false
	}

	// 默认允许
	return true
}

// findGroup 查找匹配指定 User-Agent 的规则分组。
// 优先匹配具体的 User-Agent，其次匹配通配符 "*"。
func (r *robotsRules) findGroup(ua string) *robotsGroup {
	var wildcardGroup *robotsGroup

	for _, group := range r.groups {
		for _, agent := range group.agents {
			if agent == "*" {
				wildcardGroup = group
			} else if strings.Contains(ua, agent) {
				return group
			}
		}
	}

	return wildcardGroup
}

// matchPath 检查路径是否匹配 robots.txt 中的模式。
// 支持：
//   - 前缀匹配（默认）
//   - 通配符 "*"（匹配任意字符序列）
//   - 结尾 "$"（精确匹配结尾）
func matchPath(path, pattern string) bool {
	// 处理结尾 $ 锚定
	exactEnd := false
	if strings.HasSuffix(pattern, "$") {
		exactEnd = true
		pattern = pattern[:len(pattern)-1]
	}

	// 处理通配符 *
	if strings.Contains(pattern, "*") {
		return matchWildcard(path, pattern, exactEnd)
	}

	// 简单前缀匹配
	if exactEnd {
		return path == pattern
	}
	return strings.HasPrefix(path, pattern)
}

// matchWildcard 使用通配符匹配路径。
func matchWildcard(path, pattern string, exactEnd bool) bool {
	parts := strings.Split(pattern, "*")

	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}

		idx := strings.Index(path[pos:], part)
		if idx < 0 {
			return false
		}

		// 第一个部分必须从头匹配
		if i == 0 && idx != 0 {
			return false
		}

		pos += idx + len(part)
	}

	if exactEnd {
		return pos == len(path)
	}

	return true
}
