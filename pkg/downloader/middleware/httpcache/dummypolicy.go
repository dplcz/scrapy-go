package httpcache

import (
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// DummyPolicy 无条件缓存策略。
// 除了被排除的 scheme 和 HTTP 状态码外，所有请求和响应都会被缓存。
// 缓存的响应始终被认为是新鲜的。
//
// 这是最简单的缓存策略，适用于开发和调试场景。
//
// 对应 Scrapy 的 scrapy.extensions.httpcache.DummyPolicy。
//
// 配置项：
//   - HTTPCACHE_IGNORE_SCHEMES: 不缓存的 URL scheme 列表（默认 ["file"]）
//   - HTTPCACHE_IGNORE_HTTP_CODES: 不缓存的 HTTP 状态码列表（默认 []）
type DummyPolicy struct {
	ignoreSchemes   map[string]bool
	ignoreHTTPCodes map[int]bool
}

// DummyPolicyOption 是 DummyPolicy 的可选配置函数。
type DummyPolicyOption func(*DummyPolicy)

// WithIgnoreSchemes 设置不缓存的 URL scheme 列表。
func WithIgnoreSchemes(schemes []string) DummyPolicyOption {
	return func(p *DummyPolicy) {
		p.ignoreSchemes = make(map[string]bool, len(schemes))
		for _, s := range schemes {
			p.ignoreSchemes[s] = true
		}
	}
}

// WithIgnoreHTTPCodes 设置不缓存的 HTTP 状态码列表。
func WithIgnoreHTTPCodes(codes []int) DummyPolicyOption {
	return func(p *DummyPolicy) {
		p.ignoreHTTPCodes = make(map[int]bool, len(codes))
		for _, c := range codes {
			p.ignoreHTTPCodes[c] = true
		}
	}
}

// NewDummyPolicy 创建一个新的 DummyPolicy。
func NewDummyPolicy(opts ...DummyPolicyOption) *DummyPolicy {
	p := &DummyPolicy{
		ignoreSchemes:   map[string]bool{"file": true},
		ignoreHTTPCodes: make(map[int]bool),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ShouldCacheRequest 判断请求是否应该被缓存。
// 排除 ignoreSchemes 中的 scheme。
func (p *DummyPolicy) ShouldCacheRequest(request *shttp.Request) bool {
	if request.URL == nil {
		return false
	}
	return !p.ignoreSchemes[request.URL.Scheme]
}

// ShouldCacheResponse 判断响应是否应该被缓存。
// 排除 ignoreHTTPCodes 中的状态码。
func (p *DummyPolicy) ShouldCacheResponse(response *shttp.Response, request *shttp.Request) bool {
	return !p.ignoreHTTPCodes[response.Status]
}

// IsCachedResponseFresh 判断缓存的响应是否仍然新鲜。
// DummyPolicy 始终返回 true（缓存永不过期）。
func (p *DummyPolicy) IsCachedResponseFresh(cachedResponse *shttp.Response, request *shttp.Request) bool {
	return true
}

// IsCachedResponseValid 判断缓存的响应在收到新响应后是否仍然有效。
// DummyPolicy 始终返回 true（始终使用缓存响应）。
func (p *DummyPolicy) IsCachedResponseValid(cachedResponse *shttp.Response, response *shttp.Response, request *shttp.Request) bool {
	return true
}
