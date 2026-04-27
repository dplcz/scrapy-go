package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	scrapy_errors "github.com/dplcz/scrapy-go/pkg/errors"
	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
)

// RedirectMiddleware 处理 HTTP 重定向。
// 支持 301、302、303、307、308 状态码的重定向跟踪。
//
// 对应 Scrapy 的 RedirectMiddleware（优先级 600）。
//
// 当需要重定向时，中间件返回 NewRequestError，由 Engine 将新请求重新调度到 Scheduler。
// 这种方式替代了之前通过 Meta 键传递重定向请求的 hack 方式。
//
// 相关配置：
//   - REDIRECT_ENABLED: 是否启用重定向（默认 true）
//   - REDIRECT_MAX_TIMES: 最大重定向次数（默认 20）
//   - REDIRECT_PRIORITY_ADJUST: 重定向请求的优先级调整值（默认 +2）
type RedirectMiddleware struct {
	BaseDownloaderMiddleware
	maxRedirectTimes int
	priorityAdjust   int
	logger           *slog.Logger
}

// NewRedirectMiddleware 创建一个 Redirect 中间件。
func NewRedirectMiddleware(maxRedirectTimes int, priorityAdjust int, logger *slog.Logger) *RedirectMiddleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &RedirectMiddleware{
		maxRedirectTimes: maxRedirectTimes,
		priorityAdjust:   priorityAdjust,
		logger:           logger,
	}
}

// ProcessResponse 检查响应是否为重定向，如果是则返回 NewRequestError 触发重定向。
func (m *RedirectMiddleware) ProcessResponse(ctx context.Context, request *scrapy_http.Request, response *scrapy_http.Response) (*scrapy_http.Response, error) {
	// 检查 dont_redirect meta
	if dontRedirect, ok := request.GetMeta("dont_redirect"); ok {
		if dr, ok := dontRedirect.(bool); ok && dr {
			return response, nil
		}
	}

	// 检查是否为重定向状态码
	if !isRedirectStatus(response.Status) {
		return response, nil
	}

	// 获取 Location 头
	location := response.Headers.Get("Location")
	if location == "" {
		return response, nil
	}

	// 解析重定向 URL
	redirectURL, err := resolveRedirectURL(request.URL, location)
	if err != nil {
		m.logger.Warn("invalid redirect URL",
			"location", location,
			"request", request.String(),
			"error", err,
		)
		return response, nil
	}

	// 检查重定向次数
	redirectTimes := 0
	if v, ok := request.GetMeta("redirect_times"); ok {
		if rt, ok := v.(int); ok {
			redirectTimes = rt
		}
	}
	redirectTimes++

	ttl := m.maxRedirectTimes
	if v, ok := request.GetMeta("redirect_ttl"); ok {
		if t, ok := v.(int); ok {
			ttl = t
		}
	}

	if redirectTimes > m.maxRedirectTimes || (ttl > 0 && redirectTimes > ttl) {
		m.logger.Debug("max redirects reached, dropping request",
			"request", request.String(),
		)
		return nil, scrapy_errors.ErrIgnoreRequest
	}

	// 构建重定向请求
	redirectReq := m.buildRedirectRequest(request, response, redirectURL)
	redirectReq.SetMeta("redirect_times", redirectTimes)
	redirectReq.SetMeta("redirect_ttl", ttl-1)

	// 记录重定向 URL 历史
	redirectURLs := []string{}
	if v, ok := request.GetMeta("redirect_urls"); ok {
		if urls, ok := v.([]string); ok {
			redirectURLs = urls
		}
	}
	redirectURLs = append(redirectURLs, request.URL.String())
	redirectReq.SetMeta("redirect_urls", redirectURLs)

	// 记录重定向原因历史
	redirectReasons := []any{}
	if v, ok := request.GetMeta("redirect_reasons"); ok {
		if reasons, ok := v.([]any); ok {
			redirectReasons = reasons
		}
	}
	redirectReasons = append(redirectReasons, response.Status)
	redirectReq.SetMeta("redirect_reasons", redirectReasons)

	redirectReq.DontFilter = request.DontFilter
	redirectReq.Priority = request.Priority + m.priorityAdjust

	m.logger.Debug("redirecting",
		"status", response.Status,
		"from", request.URL.String(),
		"to", redirectURL,
	)

	// 返回 NewRequestError，由 Manager 传播给 Engine 重新调度
	return nil, scrapy_errors.NewNewRequestError(redirectReq, fmt.Sprintf("redirect %d", response.Status))
}

// buildRedirectRequest 构建重定向请求。
func (m *RedirectMiddleware) buildRedirectRequest(request *scrapy_http.Request, response *scrapy_http.Response, redirectURL string) *scrapy_http.Request {
	u, _ := url.Parse(redirectURL)

	newReq := request.Copy()
	newReq.URL = u

	// 301/302 + POST → GET（RFC 7231）
	// 303 + 非 GET/HEAD → GET
	if (response.Status == 301 || response.Status == 302) && request.Method == "POST" {
		newReq.Method = "GET"
		newReq.Body = nil
		newReq.Headers.Del("Content-Type")
		newReq.Headers.Del("Content-Length")
	} else if response.Status == 303 && request.Method != "GET" && request.Method != "HEAD" {
		newReq.Method = "GET"
		newReq.Body = nil
		newReq.Headers.Del("Content-Type")
		newReq.Headers.Del("Content-Length")
	}

	// 跨域重定向时移除敏感头
	if request.URL.Host != u.Host {
		newReq.Headers.Del("Cookie")
		newReq.Headers.Del("Authorization")
	}

	return newReq
}

// isRedirectStatus 检查状态码是否为重定向。
func isRedirectStatus(status int) bool {
	return status == 301 || status == 302 || status == 303 || status == 307 || status == 308
}

// resolveRedirectURL 解析重定向 URL（支持相对 URL）。
func resolveRedirectURL(base *url.URL, location string) (string, error) {
	ref, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("invalid redirect URL: %w", err)
	}
	resolved := base.ResolveReference(ref)
	// 只允许 http/https 协议
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", fmt.Errorf("unsupported redirect scheme: %s", resolved.Scheme)
	}
	return resolved.String(), nil
}
