package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"

	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
)

// CookiesMiddleware 管理 HTTP Cookie，支持多会话隔离。
//
// 对应 Scrapy 的 CookiesMiddleware（优先级 700）。
//
// 核心功能：
//   - 在 ProcessRequest 中将 Cookie Jar 中的 Cookie 注入请求头
//   - 在 ProcessResponse 中从 Set-Cookie 响应头提取 Cookie 存入 Jar
//   - 支持通过 Request.Meta["cookiejar"] 实现多会话隔离（不同 key 使用不同 Jar）
//   - 支持通过 Request.Meta["dont_merge_cookies"] 跳过 Cookie 处理
//   - 支持 Request.Cookies 字段中的初始 Cookie
//
// 相关配置：
//   - COOKIES_ENABLED: 是否启用 Cookie 管理（默认 true）
//   - COOKIES_DEBUG: 是否输出 Cookie 调试日志（默认 false）
type CookiesMiddleware struct {
	BaseDownloaderMiddleware

	mu   sync.RWMutex
	jars map[any]*cookiejar.Jar // cookiejar key → Jar

	debug  bool
	logger *slog.Logger
}

// NewCookiesMiddleware 创建一个 Cookies 中间件。
// debug 为 true 时输出 Cookie 调试日志。
func NewCookiesMiddleware(debug bool, logger *slog.Logger) *CookiesMiddleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &CookiesMiddleware{
		jars:   make(map[any]*cookiejar.Jar),
		debug:  debug,
		logger: logger,
	}
}

// getJar 获取或创建指定 key 的 Cookie Jar。
// key 为 nil 时使用默认 Jar。
func (m *CookiesMiddleware) getJar(key any) *cookiejar.Jar {
	m.mu.RLock()
	jar, ok := m.jars[key]
	m.mu.RUnlock()
	if ok {
		return jar
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查
	if jar, ok = m.jars[key]; ok {
		return jar
	}

	// cookiejar.New 的 Options 为 nil 时不做公共后缀检查，
	// 这与 Scrapy 的行为一致（Scrapy 自行处理公共域名检查）。
	jar, _ = cookiejar.New(nil)
	m.jars[key] = jar
	return jar
}

// ProcessRequest 将 Cookie Jar 中的 Cookie 注入请求头。
// 同时处理 Request.Cookies 字段中的初始 Cookie。
func (m *CookiesMiddleware) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	// 检查 dont_merge_cookies meta
	if dontMerge, ok := request.GetMeta("dont_merge_cookies"); ok {
		if dm, ok := dontMerge.(bool); ok && dm {
			return nil, nil
		}
	}

	// 获取对应的 Cookie Jar
	var jarKey any
	if v, ok := request.GetMeta("cookiejar"); ok {
		jarKey = v
	}
	jar := m.getJar(jarKey)

	// 将 Request.Cookies 中的初始 Cookie 设置到 Jar 中
	if len(request.Cookies) > 0 {
		m.setRequestCookies(jar, request)
	}

	// 从 Jar 中获取匹配当前 URL 的 Cookie 并注入请求头
	cookies := jar.Cookies(request.URL)
	if len(cookies) > 0 {
		// 清除已有的 Cookie 头，使用 Jar 中的 Cookie 替代
		request.Headers.Del("Cookie")
		cookieStrs := make([]string, 0, len(cookies))
		for _, c := range cookies {
			cookieStrs = append(cookieStrs, c.Name+"="+c.Value)
		}
		request.Headers.Set("Cookie", strings.Join(cookieStrs, "; "))
	}

	if m.debug {
		m.debugCookie(request)
	}

	return nil, nil
}

// ProcessResponse 从响应的 Set-Cookie 头提取 Cookie 存入 Jar。
func (m *CookiesMiddleware) ProcessResponse(ctx context.Context, request *scrapy_http.Request, response *scrapy_http.Response) (*scrapy_http.Response, error) {
	// 检查 dont_merge_cookies meta
	if dontMerge, ok := request.GetMeta("dont_merge_cookies"); ok {
		if dm, ok := dontMerge.(bool); ok && dm {
			return response, nil
		}
	}

	// 获取对应的 Cookie Jar
	var jarKey any
	if v, ok := request.GetMeta("cookiejar"); ok {
		jarKey = v
	}
	jar := m.getJar(jarKey)

	// 从 Set-Cookie 头提取 Cookie 并存入 Jar
	// 使用 http.Response 来解析 Set-Cookie 头
	setCookies := response.Headers.Values("Set-Cookie")
	if len(setCookies) > 0 {
		// 构造一个临时的 http.Response 来利用标准库的 Cookie 解析
		httpResp := &http.Response{
			Header: make(http.Header),
		}
		for _, sc := range setCookies {
			httpResp.Header.Add("Set-Cookie", sc)
		}
		cookies := httpResp.Cookies()
		if len(cookies) > 0 {
			jar.SetCookies(request.URL, cookies)
		}
	}

	if m.debug {
		m.debugSetCookie(response)
	}

	return response, nil
}

// setRequestCookies 将 Request.Cookies 中的 Cookie 设置到 Jar 中。
func (m *CookiesMiddleware) setRequestCookies(jar *cookiejar.Jar, request *scrapy_http.Request) {
	cookies := make([]*http.Cookie, 0, len(request.Cookies))
	for _, c := range request.Cookies {
		cookie := &http.Cookie{
			Name:   c.Name,
			Value:  c.Value,
			Path:   c.Path,
			Domain: c.Domain,
			Secure: c.Secure,
		}
		// 如果 Cookie 没有指定 Domain，使用请求的域名
		if cookie.Domain == "" {
			cookie.Domain = request.URL.Hostname()
		}
		// 如果 Cookie 没有指定 Path，使用根路径
		if cookie.Path == "" {
			cookie.Path = "/"
		}
		cookies = append(cookies, cookie)
	}
	if len(cookies) > 0 {
		jar.SetCookies(request.URL, cookies)
	}
}

// debugCookie 输出发送的 Cookie 调试日志。
func (m *CookiesMiddleware) debugCookie(request *scrapy_http.Request) {
	cookieHeader := request.Headers.Get("Cookie")
	if cookieHeader != "" {
		m.logger.Debug("sending cookies",
			"request", request.String(),
			"cookie", cookieHeader,
		)
	}
}

// debugSetCookie 输出接收的 Set-Cookie 调试日志。
func (m *CookiesMiddleware) debugSetCookie(response *scrapy_http.Response) {
	setCookies := response.Headers.Values("Set-Cookie")
	for _, sc := range setCookies {
		m.logger.Debug("received Set-Cookie",
			"response", response.String(),
			"set-cookie", sc,
		)
	}
}

// JarCount 返回当前管理的 Cookie Jar 数量（用于测试和调试）。
func (m *CookiesMiddleware) JarCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.jars)
}

// GetCookies 返回指定 Jar 中匹配给定 URL 的 Cookie（用于测试和调试）。
func (m *CookiesMiddleware) GetCookies(jarKey any, u *url.URL) []*http.Cookie {
	m.mu.RLock()
	jar, ok := m.jars[jarKey]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return jar.Cookies(u)
}
