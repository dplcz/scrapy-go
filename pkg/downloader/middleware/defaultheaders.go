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
func (m *DefaultHeadersMiddleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	for key, values := range m.headers {
		if request.Headers.Get(key) == "" {
			for _, v := range values {
				request.Headers.Add(key, v)
			}
		}
	}
	return nil, nil
}
