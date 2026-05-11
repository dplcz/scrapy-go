package downloader

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"golang.org/x/net/http2"
)

// HTTP2DownloadHandler 是针对 HTTP/2 协议优化的下载处理器。
//
// 相比通用的 HTTPDownloadHandler，HTTP2DownloadHandler 提供以下优化：
//   - 使用 http2.Transport 直接建立 HTTP/2 连接（跳过 ALPN 协商开销）
//   - 利用 HTTP/2 多路复用特性，单连接支持多并发流
//   - 支持 HTTP/2 Server Push 感知（预留接口）
//   - 连接池统计与监控
//   - 自动降级：当目标不支持 HTTP/2 时回退到 HTTP/1.1
//
// 对应 Scrapy 中通过 Twisted 的 HTTP/2 客户端实现的功能，
// 但 Go 的 net/http + x/net/http2 提供了更原生的 HTTP/2 支持。
type HTTP2DownloadHandler struct {
	// h2Client 使用 HTTP/2 专用 Transport
	h2Client *http.Client

	// fallbackClient 使用标准 Transport（自动协商 HTTP/1.1 或 HTTP/2）
	fallbackClient *http.Client

	// h2Transport 是 HTTP/2 专用 Transport
	h2Transport *http2.Transport

	// fallbackTransport 是标准 Transport（支持自动协商）
	fallbackTransport *ManagedTransport

	// connPoolStats 连接池统计
	connPoolStats *ConnPoolStats

	// config 连接池配置
	config *ConnPoolConfig
}

// NewHTTP2DownloadHandler 创建一个 HTTP/2 优化的下载处理器。
//
// 参数：
//   - timeout: 全局下载超时时间
//   - config: 连接池配置（传 nil 使用默认配置）
func NewHTTP2DownloadHandler(timeout time.Duration, config *ConnPoolConfig) *HTTP2DownloadHandler {
	if config == nil {
		config = DefaultConnPoolConfig()
		config.ForceHTTP2 = true
	}

	// 创建 HTTP/2 专用 Transport
	h2Transport := &http2.Transport{
		// 允许使用 http:// scheme（用于测试和内网场景）
		AllowHTTP: true,
		// TLS 配置
		TLSClientConfig: &tls.Config{
			NextProtos:         []string{"h2"},
			InsecureSkipVerify: config.TLSInsecureSkipVerify,
		},
		// 读取空闲超时
		ReadIdleTimeout: config.IdleConnTimeout,
		// Ping 超时
		PingTimeout: 15 * time.Second,
	}

	h2Client := &http.Client{
		Timeout:   timeout,
		Transport: h2Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 创建带统计的 fallback Transport（自动协商 HTTP/1.1 或 HTTP/2）
	fallbackTransport := NewManagedTransport(config)

	fallbackClient := &http.Client{
		Timeout:   timeout,
		Transport: fallbackTransport.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &HTTP2DownloadHandler{
		h2Client:          h2Client,
		fallbackClient:    fallbackClient,
		h2Transport:       h2Transport,
		fallbackTransport: fallbackTransport,
		connPoolStats:     fallbackTransport.Stats(),
		config:            config,
	}
}

// Download 执行 HTTP/2 优化的下载。
//
// 策略：
//  1. 如果请求 Meta 中设置了 "force_http2" = true，强制使用 HTTP/2 Transport
//  2. 如果目标是 HTTPS，优先使用 HTTP/2（通过 ALPN 自动协商）
//  3. 如果目标是 HTTP，使用标准 Transport（HTTP/2 cleartext 需要服务端支持）
//  4. 如果 HTTP/2 连接失败，自动降级到 HTTP/1.1
func (h *HTTP2DownloadHandler) Download(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	// 构造 net/http.Request
	httpReq := &http.Request{
		Method:     request.Method,
		URL:        request.URL,
		Host:       request.URL.Host,
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		ProtoMinor: 0,
		Header:     request.Headers,
	}
	httpReq = httpReq.WithContext(ctx)

	// 设置请求体
	if len(request.Body) > 0 {
		httpReq.Body = io.NopCloser(newBytesReader(request.Body))
		httpReq.ContentLength = int64(len(request.Body))
		httpReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(newBytesReader(request.Body)), nil
		}
	}

	// 设置 Cookies
	for _, cookie := range request.Cookies {
		httpReq.AddCookie(cookie)
	}

	// 选择客户端
	client := h.selectClient(request)

	// 如果设置了代理，使用带代理的临时客户端
	if proxyURL := h.getProxyURL(request); proxyURL != nil {
		client = h.clientWithProxy(proxyURL)
	}

	// 执行请求
	httpResp, err := client.Do(httpReq)
	if err != nil {
		// HTTP/2 失败时自动降级到 fallback
		if client == h.h2Client {
			httpReq = httpReq.Clone(ctx)
			httpResp, err = h.fallbackClient.Do(httpReq)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	defer httpResp.Body.Close()

	// 更新连接池统计
	h.connPoolStats.ActiveConns.Add(1)
	defer h.connPoolStats.ActiveConns.Add(-1)

	// 读取响应体
	var body []byte
	if httpResp.ContentLength > 0 {
		body = make([]byte, httpResp.ContentLength)
		_, err = io.ReadFull(httpResp.Body, body)
		if err != nil {
			return nil, err
		}
	} else {
		body, err = io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, err
		}
	}

	// 构建 scrapy Response
	resp := &shttp.Response{
		URL:      request.URL,
		Status:   httpResp.StatusCode,
		Headers:  httpResp.Header,
		Body:     body,
		Request:  request,
		Protocol: httpResp.Proto,
	}

	return resp, nil
}

// selectClient 根据请求特征选择合适的 HTTP 客户端。
func (h *HTTP2DownloadHandler) selectClient(request *shttp.Request) *http.Client {
	// 检查 Meta 中是否强制使用 HTTP/2
	if v, ok := request.GetMeta("force_http2"); ok {
		if force, ok := v.(bool); ok && force {
			return h.h2Client
		}
	}

	// 如果全局配置强制 HTTP/2 且目标是 HTTPS，使用 fallback（支持 ALPN 自动协商 HTTP/2）
	// fallback transport 已配置 ForceAttemptHTTP2 = true，会通过 ALPN 协商 HTTP/2
	return h.fallbackClient
}

// getProxyURL 从 request.Meta["proxy"] 中提取代理 URL。
func (h *HTTP2DownloadHandler) getProxyURL(request *shttp.Request) *url.URL {
	proxyVal, ok := request.GetMeta("proxy")
	if !ok || proxyVal == nil {
		return nil
	}

	proxyStr, ok := proxyVal.(string)
	if !ok || proxyStr == "" {
		return nil
	}

	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		return nil
	}

	return proxyURL
}

// clientWithProxy 创建一个使用指定代理的临时 HTTP 客户端。
func (h *HTTP2DownloadHandler) clientWithProxy(proxyURL *url.URL) *http.Client {
	proxyTransport := h.fallbackTransport.Transport.Clone()
	proxyTransport.Proxy = http.ProxyURL(proxyURL)

	return &http.Client{
		Timeout:   h.fallbackClient.Timeout,
		Transport: proxyTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Close 关闭 HTTP/2 处理器，释放所有连接。
func (h *HTTP2DownloadHandler) Close() error {
	h.h2Client.CloseIdleConnections()
	h.fallbackClient.CloseIdleConnections()
	h.h2Transport.CloseIdleConnections()
	return nil
}

// ConnPoolStats 返回连接池统计信息。
func (h *HTTP2DownloadHandler) ConnPoolStats() *ConnPoolStats {
	return h.connPoolStats
}

// Config 返回当前连接池配置。
func (h *HTTP2DownloadHandler) Config() *ConnPoolConfig {
	return h.config
}
