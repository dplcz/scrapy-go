package middleware

import (
	"context"

	scrapy_http "scrapy-go/pkg/http"
)

// UserAgentMiddleware 为请求设置 User-Agent 请求头。
// 仅在请求头中不存在 User-Agent 时才设置。
//
// 对应 Scrapy 的 UserAgentMiddleware（优先级 500）。
type UserAgentMiddleware struct {
	BaseDownloaderMiddleware
	userAgent string
}

// NewUserAgentMiddleware 创建一个 UserAgent 中间件。
func NewUserAgentMiddleware(userAgent string) *UserAgentMiddleware {
	return &UserAgentMiddleware{
		userAgent: userAgent,
	}
}

// ProcessRequest 为请求设置 User-Agent。
func (m *UserAgentMiddleware) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	if m.userAgent != "" && request.Headers.Get("User-Agent") == "" {
		request.Headers.Set("User-Agent", m.userAgent)
	}
	return nil, nil
}
