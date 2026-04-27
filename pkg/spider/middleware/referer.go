package middleware

import (
	"context"
	"net/url"
	"strings"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// RefererMiddleware 自动为请求设置 Referer 头。
// 对应 Scrapy 的 scrapy.spidermiddlewares.referer.RefererMiddleware。
//
// 在 ProcessOutput 阶段，将父响应的 URL 设置为子请求的 Referer 头。
// 使用简化的 "scrapy-default" 策略（等价于 no-referrer-when-downgrade），
// 舍弃了 Scrapy 中复杂的 W3C Referrer Policy 体系。
//
// Go 适配说明：
//   - Scrapy 原版支持 9 种 W3C Referrer Policy，通过动态类加载实现策略切换
//   - Go 版本简化为单一的默认策略：HTTPS→HTTPS 和 HTTP→任意 发送 Referer，HTTPS→HTTP 不发送
//   - 这覆盖了绝大多数爬虫场景，避免了过度设计
//
// 配置项：
//   - REFERER_ENABLED: 是否启用 Referer 中间件（默认 true）
type RefererMiddleware struct {
	BaseSpiderMiddleware
}

// NewRefererMiddleware 创建一个新的 Referer 中间件。
func NewRefererMiddleware() *RefererMiddleware {
	return &RefererMiddleware{}
}

// ProcessOutput 为输出中的请求设置 Referer 头。
func (m *RefererMiddleware) ProcessOutput(ctx context.Context, response *shttp.Response, result []spider.Output) ([]spider.Output, error) {
	for i, output := range result {
		if output.IsRequest() {
			m.setReferer(response, result[i].Request)
		}
	}
	return result, nil
}

// setReferer 根据默认策略设置 Referer 头。
// 策略（scrapy-default / no-referrer-when-downgrade）：
//   - 如果响应 URL 是 file:// 或 data:// 等本地 scheme，不设置 Referer
//   - 如果响应 URL 是 HTTPS 且请求 URL 是 HTTP（降级），不设置 Referer
//   - 其他情况，设置 Referer 为响应 URL（去除 fragment 和认证信息）
func (m *RefererMiddleware) setReferer(response *shttp.Response, request *shttp.Request) {
	if response.URL == nil || request.URL == nil {
		return
	}

	// 如果请求已经设置了 Referer，不覆盖
	if request.Headers.Get("Referer") != "" {
		return
	}

	responseURL := response.URL
	requestURL := request.URL

	// 本地 scheme 不发送 Referer
	if isLocalScheme(responseURL.Scheme) {
		return
	}

	// HTTPS → HTTP 降级不发送 Referer
	if isTLSScheme(responseURL.Scheme) && !isTLSScheme(requestURL.Scheme) {
		return
	}

	// 构建 Referer（去除 fragment 和认证信息）
	referer := stripURL(responseURL)
	if referer != "" {
		request.Headers.Set("Referer", referer)
	}
}

// stripURL 去除 URL 中的 fragment 和认证信息，返回清理后的 URL 字符串。
func stripURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	// 创建副本，避免修改原始 URL
	cleaned := *u
	cleaned.Fragment = ""
	cleaned.User = nil
	return cleaned.String()
}

// isLocalScheme 检查是否为本地 scheme（不应发送 Referer）。
func isLocalScheme(scheme string) bool {
	s := strings.ToLower(scheme)
	switch s {
	case "about", "blob", "data", "filesystem", "file", "s3":
		return true
	}
	return false
}

// isTLSScheme 检查是否为 TLS 加密的 scheme。
func isTLSScheme(scheme string) bool {
	s := strings.ToLower(scheme)
	return s == "https" || s == "ftps"
}
