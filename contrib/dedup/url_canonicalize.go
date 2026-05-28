package dedup

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

var defaultTrackingParamNames = []string{
	"fbclid",
	"gclid",
	"msclkid",
	"dclid",
	"yclid",
	"igshid",
}

var defaultTrackingParamPrefixes = []string{"utm_"}

// URLCanonicalizerOptions 定义 URL 规范化规则。
type URLCanonicalizerOptions struct {
	// KeepFragments 是否保留 URL fragment。
	// 默认 false，表示移除 #fragment。
	KeepFragments bool

	// DropTrackingParams 是否移除常见追踪参数。
	// 默认 true，移除 utm_*、fbclid、gclid 等参数。
	DropTrackingParams bool

	// TrackingParamNames 是需要移除的追踪参数名，大小写不敏感。
	// 为空时使用默认列表：fbclid、gclid、msclkid、dclid、yclid、igshid。
	TrackingParamNames []string

	// TrackingParamPrefixes 是需要移除的追踪参数前缀，大小写不敏感。
	// 为空时使用默认列表：utm_。
	TrackingParamPrefixes []string
}

// DefaultURLCanonicalizerOptions 返回 URL 规范化默认配置。
func DefaultURLCanonicalizerOptions() *URLCanonicalizerOptions {
	return &URLCanonicalizerOptions{
		DropTrackingParams:    true,
		TrackingParamNames:    append([]string(nil), defaultTrackingParamNames...),
		TrackingParamPrefixes: append([]string(nil), defaultTrackingParamPrefixes...),
	}
}

// CanonicalizeURL 对 URL 进行稳定规范化。
//
// 规范化规则：
//  1. scheme 和 host 转小写
//  2. 移除默认端口（http:80、https:443）
//  3. 查询参数按 key/value 排序
//  4. 按配置移除 utm_*、fbclid 等追踪参数
//  5. 默认移除 fragment
//  6. 空 path 规范化为 /
func CanonicalizeURL(rawURL string, opts *URLCanonicalizerOptions) string {
	normalizedOpts := normalizeURLCanonicalizerOptions(opts)

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	u.Scheme = strings.ToLower(u.Scheme)
	normalizeHost(u)
	normalizeQuery(u, normalizedOpts)

	if !normalizedOpts.KeepFragments {
		u.Fragment = ""
		u.RawFragment = ""
	}

	if u.Path == "" {
		u.Path = "/"
	}

	return u.String()
}

// URLCanonicalDupeFilter 是基于 URL 规范化指纹的去重过滤器。
//
// 它实现 scheduler.DupeFilter 接口。指纹由 HTTP Method、规范化后的 URL
// 和 Body 共同计算，既能过滤 tracking query 导致的重复 URL，又不会错误合并
// 不同 Method 或不同 Body 的请求。
type URLCanonicalDupeFilter struct {
	opts         *URLCanonicalizerOptions
	fingerprints sync.Map
	seenCount    atomic.Int64
	closed       atomic.Bool
}

// NewURLCanonicalDupeFilter 创建 URL 规范化去重过滤器。
func NewURLCanonicalDupeFilter(opts *URLCanonicalizerOptions) *URLCanonicalDupeFilter {
	return &URLCanonicalDupeFilter{opts: normalizeURLCanonicalizerOptions(opts)}
}

// Open 初始化过滤器。
func (f *URLCanonicalDupeFilter) Open(ctx context.Context) error {
	f.closed.Store(false)
	return nil
}

// Close 关闭过滤器并释放内存状态。
func (f *URLCanonicalDupeFilter) Close(reason string) error {
	if !f.closed.CompareAndSwap(false, true) {
		return nil
	}
	f.fingerprints = sync.Map{}
	f.seenCount.Store(0)
	return nil
}

// RequestSeen 检查请求是否已见过。
func (f *URLCanonicalDupeFilter) RequestSeen(request *shttp.Request) bool {
	if request == nil || request.URL == nil || f.closed.Load() {
		return false
	}

	fp := CanonicalRequestFingerprint(request, f.opts)
	_, loaded := f.fingerprints.LoadOrStore(fp, struct{}{})
	if !loaded {
		f.seenCount.Add(1)
	}
	return loaded
}

// SeenCount 返回已记录的规范化 URL 指纹数量。
func (f *URLCanonicalDupeFilter) SeenCount() int {
	return int(f.seenCount.Load())
}

// Clear 清空已记录的 URL 指纹。
func (f *URLCanonicalDupeFilter) Clear() {
	f.fingerprints = sync.Map{}
	f.seenCount.Store(0)
}

// CanonicalRequestFingerprint 计算请求的 URL 规范化指纹。
func CanonicalRequestFingerprint(request *shttp.Request, opts *URLCanonicalizerOptions) string {
	if request == nil || request.URL == nil {
		return ""
	}

	h := sha1.New()
	h.Write([]byte(strings.ToUpper(request.Method)))
	h.Write([]byte{0})
	h.Write([]byte(CanonicalizeURL(request.URL.String(), opts)))
	h.Write([]byte{0})
	h.Write(request.Body)
	return hex.EncodeToString(h.Sum(nil))
}

func normalizeURLCanonicalizerOptions(opts *URLCanonicalizerOptions) *URLCanonicalizerOptions {
	if opts == nil {
		return DefaultURLCanonicalizerOptions()
	}

	normalized := *opts
	if normalized.DropTrackingParams {
		if len(normalized.TrackingParamNames) == 0 {
			normalized.TrackingParamNames = append([]string(nil), defaultTrackingParamNames...)
		} else {
			normalized.TrackingParamNames = append([]string(nil), normalized.TrackingParamNames...)
		}
		if len(normalized.TrackingParamPrefixes) == 0 {
			normalized.TrackingParamPrefixes = append([]string(nil), defaultTrackingParamPrefixes...)
		} else {
			normalized.TrackingParamPrefixes = append([]string(nil), normalized.TrackingParamPrefixes...)
		}
	}
	return &normalized
}

func normalizeHost(u *url.URL) {
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return
	}

	port := u.Port()
	if port == "" || isDefaultPort(u.Scheme, port) {
		u.Host = hostForURL(host)
		return
	}
	u.Host = net.JoinHostPort(host, port)
}

func isDefaultPort(scheme, port string) bool {
	return (scheme == "http" && port == "80") || (scheme == "https" && port == "443")
}

func hostForURL(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func normalizeQuery(u *url.URL, opts *URLCanonicalizerOptions) {
	if u.RawQuery == "" {
		return
	}

	params := u.Query()
	keys := make([]string, 0, len(params))
	for key := range params {
		if opts.DropTrackingParams && isTrackingParam(key, opts) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		values := append([]string(nil), params[key]...)
		sort.Strings(values)
		for _, value := range values {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	u.RawQuery = strings.Join(parts, "&")
}

func isTrackingParam(key string, opts *URLCanonicalizerOptions) bool {
	lowerKey := strings.ToLower(key)
	for _, name := range opts.TrackingParamNames {
		if lowerKey == strings.ToLower(name) {
			return true
		}
	}
	for _, prefix := range opts.TrackingParamPrefixes {
		if strings.HasPrefix(lowerKey, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}
