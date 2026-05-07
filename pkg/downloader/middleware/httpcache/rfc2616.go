package httpcache

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// maxAge 是 RFC2616 策略的最大缓存时间（一年）。
const maxAge = 3600 * 24 * 365

// RFC2616Policy 实现 RFC 2616 HTTP 缓存语义的缓存策略（实验性）。
//
// 支持以下 HTTP 缓存机制：
//   - Cache-Control 指令（max-age、no-cache、no-store、must-revalidate、max-stale）
//   - Expires 头
//   - ETag / If-None-Match 条件验证
//   - Last-Modified / If-Modified-Since 条件验证
//   - 304 Not Modified 响应处理
//
// 对应 Scrapy 的 scrapy.extensions.httpcache.RFC2616Policy。
//
// 配置项：
//   - HTTPCACHE_ALWAYS_STORE: 是否始终存储响应（默认 false）
//   - HTTPCACHE_IGNORE_SCHEMES: 不缓存的 URL scheme 列表（默认 ["file"]）
//   - HTTPCACHE_IGNORE_RESPONSE_CACHE_CONTROLS: 忽略的响应 Cache-Control 指令
type RFC2616Policy struct {
	alwaysStore                 bool
	ignoreSchemes               map[string]bool
	ignoreResponseCacheControls map[string]bool
}

// RFC2616PolicyOption 是 RFC2616Policy 的可选配置函数。
type RFC2616PolicyOption func(*RFC2616Policy)

// WithAlwaysStore 设置是否始终存储响应。
func WithAlwaysStore(alwaysStore bool) RFC2616PolicyOption {
	return func(p *RFC2616Policy) {
		p.alwaysStore = alwaysStore
	}
}

// WithRFC2616IgnoreSchemes 设置不缓存的 URL scheme 列表。
func WithRFC2616IgnoreSchemes(schemes []string) RFC2616PolicyOption {
	return func(p *RFC2616Policy) {
		p.ignoreSchemes = make(map[string]bool, len(schemes))
		for _, s := range schemes {
			p.ignoreSchemes[s] = true
		}
	}
}

// WithIgnoreResponseCacheControls 设置忽略的响应 Cache-Control 指令。
func WithIgnoreResponseCacheControls(controls []string) RFC2616PolicyOption {
	return func(p *RFC2616Policy) {
		p.ignoreResponseCacheControls = make(map[string]bool, len(controls))
		for _, c := range controls {
			p.ignoreResponseCacheControls[strings.ToLower(c)] = true
		}
	}
}

// NewRFC2616Policy 创建一个新的 RFC2616Policy。
func NewRFC2616Policy(opts ...RFC2616PolicyOption) *RFC2616Policy {
	p := &RFC2616Policy{
		ignoreSchemes:               map[string]bool{"file": true},
		ignoreResponseCacheControls: make(map[string]bool),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ShouldCacheRequest 判断请求是否应该被缓存。
// 排除 ignoreSchemes 中的 scheme 和包含 no-store 指令的请求。
func (p *RFC2616Policy) ShouldCacheRequest(request *shttp.Request) bool {
	if request.URL == nil {
		return false
	}
	if p.ignoreSchemes[request.URL.Scheme] {
		return false
	}
	cc := parseCacheControl(request.Headers.Get("Cache-Control"))
	return !cc.has("no-store")
}

// ShouldCacheResponse 判断响应是否应该被缓存。
// 遵循 RFC 2616 Section 13.4 和 Section 14.9.1 的规则。
func (p *RFC2616Policy) ShouldCacheResponse(response *shttp.Response, request *shttp.Request) bool {
	cc := p.parseResponseCacheControl(response)

	// 遵循 no-store 指令
	if cc.has("no-store") {
		return false
	}

	// 不缓存 304 响应（缓存无法处理部分内容）
	if response.Status == 304 {
		return false
	}

	// 如果配置了始终存储，则无条件缓存
	if p.alwaysStore {
		return true
	}

	// 有过期提示的响应可以缓存
	if cc.has("max-age") || response.Headers.Get("Expires") != "" {
		return true
	}

	// Firefox 对这些状态码回退到一年过期
	if response.Status == 300 || response.Status == 301 || response.Status == 308 {
		return true
	}

	// 其他状态码需要至少一个验证器
	if response.Status == 200 || response.Status == 203 || response.Status == 401 {
		return response.Headers.Get("Last-Modified") != "" || response.Headers.Get("ETag") != ""
	}

	return false
}

// IsCachedResponseFresh 判断缓存的响应是否仍然新鲜。
func (p *RFC2616Policy) IsCachedResponseFresh(cachedResponse *shttp.Response, request *shttp.Request) bool {
	cc := p.parseResponseCacheControl(cachedResponse)
	ccReq := parseCacheControl(request.Headers.Get("Cache-Control"))

	// no-cache 指令要求重新验证
	if cc.has("no-cache") || ccReq.has("no-cache") {
		return false
	}

	now := time.Now()
	freshnessLifetime := p.computeFreshnessLifetime(cachedResponse, now)
	currentAge := p.computeCurrentAge(cachedResponse, now)

	// 请求的 max-age 限制
	if reqMaxAge, ok := ccReq.getInt("max-age"); ok {
		if reqMaxAge < int(freshnessLifetime.Seconds()) {
			freshnessLifetime = time.Duration(reqMaxAge) * time.Second
		}
	}

	if currentAge < freshnessLifetime {
		return true
	}

	// max-stale 允许接受过期响应
	if ccReq.has("max-stale") && !cc.has("must-revalidate") {
		staleAge := ccReq.get("max-stale")
		if staleAge == "" {
			return true // 接受任何过期响应
		}
		if staleSeconds, err := strconv.Atoi(staleAge); err == nil {
			staleDuration := time.Duration(staleSeconds) * time.Second
			if currentAge < freshnessLifetime+staleDuration {
				return true
			}
		}
	}

	// 缓存过期，设置条件验证头
	p.setConditionalValidators(request, cachedResponse)
	return false
}

// IsCachedResponseValid 判断缓存的响应在收到新响应后是否仍然有效。
func (p *RFC2616Policy) IsCachedResponseValid(cachedResponse *shttp.Response, response *shttp.Response, request *shttp.Request) bool {
	// 服务器错误时使用缓存响应（除非 must-revalidate）
	if response.Status >= 500 {
		cc := p.parseResponseCacheControl(cachedResponse)
		if !cc.has("must-revalidate") {
			return true
		}
	}

	// 304 Not Modified 表示缓存仍然有效
	return response.Status == 304
}

// setConditionalValidators 设置条件验证请求头。
func (p *RFC2616Policy) setConditionalValidators(request *shttp.Request, cachedResponse *shttp.Response) {
	if lastModified := cachedResponse.Headers.Get("Last-Modified"); lastModified != "" {
		request.Headers.Set("If-Modified-Since", lastModified)
	}
	if etag := cachedResponse.Headers.Get("ETag"); etag != "" {
		request.Headers.Set("If-None-Match", etag)
	}
}

// computeFreshnessLifetime 计算响应的新鲜度生命周期。
// 参考 Mozilla nsHttpResponseHead::ComputeFreshnessLifetime。
func (p *RFC2616Policy) computeFreshnessLifetime(response *shttp.Response, now time.Time) time.Duration {
	cc := p.parseResponseCacheControl(response)

	// 优先使用 max-age
	if maxAgeVal, ok := cc.getInt("max-age"); ok {
		return time.Duration(maxAgeVal) * time.Second
	}

	// 解析 Date 头或使用当前时间
	date := parseHTTPDate(response.Headers.Get("Date"))
	if date.IsZero() {
		date = now
	}

	// 尝试 Expires 头
	if expiresStr := response.Headers.Get("Expires"); expiresStr != "" {
		expires := parseHTTPDate(expiresStr)
		if expires.IsZero() {
			return 0 // 解析失败视为已过期
		}
		diff := expires.Sub(date)
		if diff < 0 {
			return 0
		}
		return diff
	}

	// 启发式：使用 Last-Modified
	if lastModifiedStr := response.Headers.Get("Last-Modified"); lastModifiedStr != "" {
		lastModified := parseHTTPDate(lastModifiedStr)
		if !lastModified.IsZero() && !lastModified.After(date) {
			return date.Sub(lastModified) / 10
		}
	}

	// 永久重定向可以无限期缓存
	if response.Status == 300 || response.Status == 301 || response.Status == 308 {
		return time.Duration(maxAge) * time.Second
	}

	return 0
}

// computeCurrentAge 计算响应的当前年龄。
// 参考 Mozilla nsHttpResponseHead::ComputeCurrentAge。
func (p *RFC2616Policy) computeCurrentAge(response *shttp.Response, now time.Time) time.Duration {
	var currentAge time.Duration

	date := parseHTTPDate(response.Headers.Get("Date"))
	if date.IsZero() {
		date = now
	}

	if now.After(date) {
		currentAge = now.Sub(date)
	}

	if ageStr := response.Headers.Get("Age"); ageStr != "" {
		if age, err := strconv.Atoi(ageStr); err == nil {
			ageDuration := time.Duration(age) * time.Second
			if ageDuration > currentAge {
				currentAge = ageDuration
			}
		}
	}

	return currentAge
}

// parseResponseCacheControl 解析响应的 Cache-Control 头，并过滤掉忽略的指令。
func (p *RFC2616Policy) parseResponseCacheControl(response *shttp.Response) cacheControlDirectives {
	cc := parseCacheControl(response.Headers.Get("Cache-Control"))
	for key := range p.ignoreResponseCacheControls {
		delete(cc, key)
	}
	return cc
}

// ============================================================================
// Cache-Control 解析
// ============================================================================

// cacheControlDirectives 表示解析后的 Cache-Control 指令。
// key 为指令名称（小写），value 为指令值（无值时为空字符串）。
type cacheControlDirectives map[string]string

// parseCacheControl 解析 Cache-Control 头。
//
// 示例：
//
//	parseCacheControl("public, max-age=3600") => {"public": "", "max-age": "3600"}
//	parseCacheControl("no-cache") => {"no-cache": ""}
func parseCacheControl(header string) cacheControlDirectives {
	directives := make(cacheControlDirectives)
	if header == "" {
		return directives
	}

	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, found := strings.Cut(part, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		if found {
			directives[key] = strings.TrimSpace(value)
		} else {
			directives[key] = ""
		}
	}
	return directives
}

// has 检查是否存在指定的指令。
func (cc cacheControlDirectives) has(key string) bool {
	_, ok := cc[key]
	return ok
}

// get 获取指令的值。
func (cc cacheControlDirectives) get(key string) string {
	return cc[key]
}

// getInt 获取指令的整数值。
func (cc cacheControlDirectives) getInt(key string) (int, bool) {
	v, ok := cc[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	if n < 0 {
		n = 0
	}
	return n, true
}

// ============================================================================
// HTTP 日期解析
// ============================================================================

// httpDateFormats 是 HTTP 日期的常见格式。
var httpDateFormats = []string{
	http.TimeFormat,                  // "Mon, 02 Jan 2006 15:04:05 GMT"
	time.RFC1123,                     // "Mon, 02 Jan 2006 15:04:05 MST"
	time.RFC1123Z,                    // "Mon, 02 Jan 2006 15:04:05 -0700"
	"Mon, 2 Jan 2006 15:04:05 GMT",   // 单数日期
	"Monday, 02-Jan-06 15:04:05 GMT", // RFC 850
	"Mon Jan  2 15:04:05 2006",       // ANSI C asctime()
}

// parseHTTPDate 解析 HTTP 日期字符串。
// 支持 RFC 1123、RFC 850 和 ANSI C asctime() 格式。
// 解析失败返回零值 time.Time。
func parseHTTPDate(dateStr string) time.Time {
	if dateStr == "" {
		return time.Time{}
	}
	for _, format := range httpDateFormats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t
		}
	}
	return time.Time{}
}
