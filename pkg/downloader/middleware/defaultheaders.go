package middleware

import (
	"context"
	"net/http"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// DefaultHeadersMiddleware 为请求设置默认请求头。
// 仅在请求头中不存在对应 Header 时才设置（setdefault 语义）。
//
// 对应 Scrapy 的 DefaultHeadersMiddleware（优先级 400）。
type DefaultHeadersMiddleware struct {
	BaseDownloaderMiddleware
	headers http.Header
}

// NewDefaultHeadersMiddleware 创建一个 DefaultHeaders 中间件。
func NewDefaultHeadersMiddleware(headers http.Header) *DefaultHeadersMiddleware {
	return &DefaultHeadersMiddleware{
		headers: headers,
	}
}

// ProcessRequest 为请求设置默认请求头。
//
// ⚠️ 实现说明：此方法直接将 m.headers 中的 []string slice 引用赋值给 request.Headers，
// 而非逐个 Add（避免 slice 扩容分配）。这要求：
//  1. m.headers 在初始化后为只读配置，不可修改其 values slice 内容；
//  2. 后续中间件对相同 key 应使用 Set（创建新 slice）而非 Add（append 到共享 slice），
//     否则可能污染全局配置。当前内置中间件均使用 Set，满足此约束。
//  3. 自定义中间件如需对 DefaultHeaders 已设置的 key 追加值，应使用 Set 替代 Add。
func (m *DefaultHeadersMiddleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	for key, values := range m.headers {
		if request.Headers.Get(key) == "" {
			// 直接赋值 slice 引用（零拷贝），避免逐个 Add 触发 slice 扩容。
			// m.headers 为只读配置（初始化后不再修改），直接引用安全。
			// ⚠️ 后续中间件对相同 key 必须使用 Set 而非 Add，否则会污染 m.headers。
			request.Headers[key] = values
		}
	}
	return nil, nil
}
