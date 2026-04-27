package middleware

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// HttpProxyMiddleware 实现 HTTP 代理支持。
// 对应 Scrapy 的 HttpProxyMiddleware。
//
// 代理来源（按优先级从高到低）：
//  1. Request.Meta["proxy"] — 请求级代理
//  2. 环境变量 http_proxy / https_proxy / HTTP_PROXY / HTTPS_PROXY
//
// 支持代理认证（Basic Auth），认证信息可以包含在代理 URL 中：
//
//	http://user:password@proxy.example.com:8080
//
// 配置项：
//   - HTTPPROXY_ENABLED: 是否启用代理中间件（默认 true）
//   - HTTPPROXY_AUTH_ENCODING: 认证信息编码（默认 "latin-1"，Go 中使用 UTF-8）
type HttpProxyMiddleware struct {
	BaseDownloaderMiddleware

	// proxies 存储从环境变量读取的代理配置。
	// key 为 scheme（"http" 或 "https"），value 为解析后的代理信息。
	proxies map[string]*proxyInfo

	logger *slog.Logger
}

// proxyInfo 存储解析后的代理信息。
type proxyInfo struct {
	// proxyURL 是代理服务器的 URL（不含认证信息）。
	proxyURL string
	// credentials 是 Base64 编码的认证信息（"user:password"），为空表示无认证。
	credentials string
}

// NewHttpProxyMiddleware 创建一个新的 HttpProxy 中间件。
func NewHttpProxyMiddleware(logger *slog.Logger) *HttpProxyMiddleware {
	if logger == nil {
		logger = slog.Default()
	}

	m := &HttpProxyMiddleware{
		proxies: make(map[string]*proxyInfo),
		logger:  logger,
	}

	// 从环境变量读取代理配置
	m.loadProxiesFromEnv()

	return m
}

// loadProxiesFromEnv 从环境变量加载代理配置。
// 支持 http_proxy / HTTP_PROXY 和 https_proxy / HTTPS_PROXY。
func (m *HttpProxyMiddleware) loadProxiesFromEnv() {
	envVars := map[string][]string{
		"http":  {"http_proxy", "HTTP_PROXY"},
		"https": {"https_proxy", "HTTPS_PROXY"},
	}

	for scheme, vars := range envVars {
		for _, envVar := range vars {
			if proxyURL := os.Getenv(envVar); proxyURL != "" {
				info, err := parseProxyURL(proxyURL)
				if err != nil {
					m.logger.Warn("failed to parse proxy URL from environment",
						"env", envVar,
						"error", err,
					)
					continue
				}
				m.proxies[scheme] = info
				m.logger.Debug("loaded proxy from environment",
					"scheme", scheme,
					"proxy", info.proxyURL,
					"has_auth", info.credentials != "",
				)
				break // 优先使用小写环境变量
			}
		}
	}
}

// ProcessRequest 在请求发送前设置代理。
func (m *HttpProxyMiddleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	var info *proxyInfo

	// 1. 检查 Request.Meta["proxy"]
	if proxyVal, ok := request.GetMeta("proxy"); ok {
		if proxyVal == nil {
			// Meta["proxy"] = nil 表示显式禁用代理
			return nil, nil
		}
		if proxyStr, ok := proxyVal.(string); ok && proxyStr != "" {
			parsed, err := parseProxyURL(proxyStr)
			if err != nil {
				m.logger.Warn("failed to parse proxy URL from request meta",
					"url", request.URL.String(),
					"proxy", proxyStr,
					"error", err,
				)
				return nil, nil
			}
			info = parsed
		}
	} else {
		// 2. 使用环境变量代理
		scheme := request.URL.Scheme
		if p, ok := m.proxies[scheme]; ok {
			info = p
		}
	}

	if info == nil {
		return nil, nil
	}

	// 设置代理 URL 到 Meta
	request.SetMeta("proxy", info.proxyURL)

	// 设置代理认证头
	if info.credentials != "" {
		if request.Headers == nil {
			request.Headers = make(http.Header)
		}
		request.Headers.Set("Proxy-Authorization", "Basic "+info.credentials)
	}

	return nil, nil
}

// parseProxyURL 解析代理 URL，提取认证信息。
// 输入格式：http://user:password@host:port 或 http://host:port
func parseProxyURL(rawURL string) (*proxyInfo, error) {
	// 确保有 scheme
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL %q: %w", rawURL, err)
	}

	info := &proxyInfo{}

	// 提取认证信息
	if u.User != nil {
		username := u.User.Username()
		password, _ := u.User.Password()
		credentials := fmt.Sprintf("%s:%s", username, password)
		info.credentials = base64.StdEncoding.EncodeToString([]byte(credentials))

		// 构建不含认证信息的代理 URL
		u.User = nil
	}

	info.proxyURL = u.String()

	return info, nil
}
