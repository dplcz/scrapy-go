package middleware

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/url"

	scrapy_http "scrapy-go/pkg/http"
)

// HttpAuthMiddleware 为请求设置 HTTP Basic Authentication 头。
//
// 对应 Scrapy 的 HttpAuthMiddleware（优先级 410）。
//
// 认证信息来源（按优先级）：
//  1. Request.Meta["http_user"] / Request.Meta["http_pass"]（请求级覆盖）
//  2. 构造时传入的全局 user/pass
//
// 可选的域名限制：
//   - 当设置了 domain 时，仅对匹配该域名的请求注入认证头
//   - 当 domain 为空时，对所有请求注入认证头
//
// 相关配置：
//   - HTTP_USER: HTTP Basic Auth 用户名
//   - HTTP_PASS: HTTP Basic Auth 密码
//   - HTTP_AUTH_DOMAIN: 限制认证的域名（可选）
type HttpAuthMiddleware struct {
	BaseDownloaderMiddleware
	user   string
	pass   string
	domain string
	logger *slog.Logger
}

// NewHttpAuthMiddleware 创建一个 HttpAuth 中间件。
// user 和 pass 为全局默认的认证凭据。
// domain 为可选的域名限制，为空时对所有请求生效。
func NewHttpAuthMiddleware(user, pass, domain string, logger *slog.Logger) *HttpAuthMiddleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &HttpAuthMiddleware{
		user:   user,
		pass:   pass,
		domain: domain,
		logger: logger,
	}
}

// ProcessRequest 为请求注入 Authorization 头（Basic Auth）。
// 仅在以下条件全部满足时注入：
//  1. 请求尚未设置 Authorization 头
//  2. 存在有效的认证凭据（user 或 pass 非空）
//  3. 域名匹配（如果设置了域名限制）
func (m *HttpAuthMiddleware) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	// 如果请求已经设置了 Authorization 头，不覆盖
	if request.Headers.Get("Authorization") != "" {
		return nil, nil
	}

	// 获取认证凭据（请求级覆盖 > 全局配置）
	user, pass := m.user, m.pass
	if v, ok := request.GetMeta("http_user"); ok {
		if u, ok := v.(string); ok {
			user = u
		}
	}
	if v, ok := request.GetMeta("http_pass"); ok {
		if p, ok := v.(string); ok {
			pass = p
		}
	}

	// 无认证凭据，跳过
	if user == "" && pass == "" {
		return nil, nil
	}

	// 检查域名限制
	if m.domain != "" && !urlIsFromDomain(request.URL, m.domain) {
		return nil, nil
	}

	// 注入 Basic Auth 头
	auth := basicAuthHeader(user, pass)
	request.Headers.Set("Authorization", auth)

	return nil, nil
}

// basicAuthHeader 生成 HTTP Basic Auth 头的值。
func basicAuthHeader(user, pass string) string {
	credentials := user + ":" + pass
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	return "Basic " + encoded
}

// urlIsFromDomain 检查 URL 是否属于指定域名。
// 支持精确匹配和子域名匹配。
func urlIsFromDomain(u *url.URL, domain string) bool {
	if u == nil {
		return false
	}
	host := u.Hostname()
	if host == domain {
		return true
	}
	// 检查子域名匹配（如 sub.example.com 匹配 example.com）
	suffix := "." + domain
	if len(host) > len(suffix) && host[len(host)-len(suffix):] == suffix {
		return true
	}
	return false
}
