// Package downloader 实现了 scrapy-go 框架的下载器系统。
//
// 下载器负责执行 HTTP 请求并返回响应，通过 Slot 机制控制并发和延迟。
// 对应 Scrapy Python 版本中 scrapy.core.downloader 模块的功能。
package downloader

import (
	"context"
	"io"
	"maps"
	"net/http"
	"net/url"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// DownloadHandler 定义下载处理器接口。
// 不同协议（http、https、ftp 等）可以有不同的处理器实现。
type DownloadHandler interface {
	// Download 执行下载请求并返回响应。
	Download(ctx context.Context, request *shttp.Request) (*shttp.Response, error)

	// Close 关闭处理器，释放资源。
	Close() error
}

// HTTPDownloadHandler 是基于 net/http 的 HTTP 下载处理器。
// 支持 HTTP/1.1 和 HTTP/2（通过 ALPN 自动协商）。
//
// 当通过 NewHTTPDownloadHandlerWithConfig 创建时，支持连接池精细化配置和运行时统计。
// 当通过 NewHTTPDownloadHandler 创建时，使用默认配置，行为与重构前完全一致。
type HTTPDownloadHandler struct {
	client           *http.Client
	transport        *http.Transport
	managedTransport *ManagedTransport // 可选，仅在使用 WithConfig 构造时非 nil
	config           *ConnPoolConfig   // 可选，仅在使用 WithConfig 构造时非 nil
}

// NewHTTPDownloadHandler 创建一个新的 HTTP 下载处理器。
// 使用默认的连接池配置，不启用连接池统计。
func NewHTTPDownloadHandler(timeout time.Duration) *HTTPDownloadHandler {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		// 禁用标准库的自动解压，由 HttpCompressionMiddleware 统一管理。
		// 标准库在用户未设置 Accept-Encoding 时会自动添加 gzip 并解压，
		// 但这会与中间件的解压逻辑冲突，且无法统计解压字节数和控制解压大小限制。
		DisableCompression: true,
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// 禁用自动重定向
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &HTTPDownloadHandler{
		client:    client,
		transport: transport,
	}
}

// NewHTTPDownloadHandlerWithConfig 创建一个带精细化配置的 HTTP 下载处理器。
//
// 相比 NewHTTPDownloadHandler，此构造函数支持：
//   - ConnPoolConfig 配置注入（连接池参数、HTTP/2 控制、h2c 支持等）
//   - ConnPoolStats 连接池运行时统计
//   - ForceHTTP2=true 时设置 ForceAttemptHTTP2（用于自定义 DialContext 场景）
//   - AllowH2C=true 时注册 http2.Transport 作为 http:// scheme 的 handler
//
// 参数：
//   - timeout: 全局下载超时时间
//   - config: 连接池配置（传 nil 使用默认配置）
func NewHTTPDownloadHandlerWithConfig(timeout time.Duration, config *ConnPoolConfig) *HTTPDownloadHandler {
	if config == nil {
		config = DefaultConnPoolConfig()
	}

	mt := NewManagedTransport(config)

	client := &http.Client{
		Timeout:   timeout,
		Transport: mt.Transport,
		// 禁用自动重定向
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &HTTPDownloadHandler{
		client:           client,
		transport:        mt.Transport,
		managedTransport: mt,
		config:           config,
	}
}

// Download 执行 HTTP 下载。
// 如果 request.Meta["proxy"] 设置了代理 URL，则通过该代理发送请求。
func (h *HTTPDownloadHandler) Download(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	// 直接构造 net/http.Request，避免 URL 重复解析。
	// 原实现使用 http.NewRequestWithContext(ctx, method, url.String(), nil)，
	// 会将已解析的 *url.URL 序列化为字符串后再次解析，造成不必要的 CPU 和内存开销。
	// 优化后直接复用 request.URL（已是 *url.URL），消除序列化+反序列化过程。
	//
	// ⚠️ 注意：Header 直接复用 request.Headers（零拷贝），net/http.Client.Do 仅读取不修改。
	// 但下方 AddCookie 会通过 httpReq.Header.Add("Cookie", ...) 修改原始 request.Headers。
	// 这是可接受的，因为 Handler 是下载链路的最后一环，Headers 修改不影响后续流程。
	// 如果未来 ProcessResponse 中间件需要依赖 request.Headers 中 Cookie 头的精确值，
	// 需要注意此行为（CookiesMiddleware 已在 ProcessRequest 阶段通过 Headers.Set 设置了 Cookie）。
	httpReq := &http.Request{
		Method:     request.Method,
		URL:        request.URL,
		Host:       request.URL.Host,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     request.Headers, // 直接复用请求 Headers（零拷贝），net/http.Client.Do 仅读取不修改
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
	// ⚠️ AddCookie 会调用 httpReq.Header.Add("Cookie", ...)，由于 Header 直接引用
	// request.Headers，这会修改原始请求的 Headers。此副作用是安全的：
	// 1. Handler 是请求处理的最后一步，修改后请求即发出，不再被 ProcessRequest 使用；
	// 2. 启用 CookiesMiddleware（优先级 700）时，Cookie 已通过 Headers.Set 注入，
	//    request.Cookies 通常为空，此分支不会执行。
	for _, cookie := range request.Cookies {
		httpReq.AddCookie(cookie)
	}

	// 选择 HTTP 客户端：如果设置了代理，使用带代理的临时客户端
	client := h.client
	if proxyURL := h.getProxyURL(request); proxyURL != nil {
		client = h.clientWithProxy(proxyURL)
	}

	// 执行请求
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	// 读取响应体。
	// 当 Content-Length 已知时，预分配精确大小 buffer 避免多次 grow+copy。
	// Content-Length 未知时（chunked），直接使用 io.ReadAll。
	var body []byte
	if httpResp.ContentLength > 0 {
		// Content-Length 已知：精确预分配，避免 grow
		body = make([]byte, httpResp.ContentLength)
		_, err = io.ReadFull(httpResp.Body, body)
		if err != nil {
			return nil, err
		}
	} else {
		// Content-Length 未知或 chunked：直接读取全部内容。
		// 经分析，BytesPool 复用中间缓冲区的收益有限：
		// 最终仍需 make+copy 创建独立 slice，池化仅节省 Buffer grow 中间分配；
		// 且大响应体会导致池中 buffer 膨胀，长期驻留内存。
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

// getProxyURL 从 request.Meta["proxy"] 中提取代理 URL。
// 返回 nil 表示不使用代理。
func (h *HTTPDownloadHandler) getProxyURL(request *shttp.Request) *url.URL {
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
// 复用原始 Transport 的大部分配置，仅覆盖 Proxy 函数。
func (h *HTTPDownloadHandler) clientWithProxy(proxyURL *url.URL) *http.Client {
	proxyTransport := h.transport.Clone()
	proxyTransport.Proxy = http.ProxyURL(proxyURL)
	// TODO 因标准库的bug，需要手动clone
	// 每次都会创建新的对象，后续进行性能优化
	proxyTransport.TLSNextProto = maps.Clone(h.transport.TLSNextProto)

	return &http.Client{
		Timeout:   h.client.Timeout,
		Transport: proxyTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Close 关闭 HTTP 处理器。
func (h *HTTPDownloadHandler) Close() error {
	h.client.CloseIdleConnections()
	return nil
}

// ConnPoolStats 返回连接池统计信息。
// 仅在通过 NewHTTPDownloadHandlerWithConfig 创建时可用，否则返回 nil。
func (h *HTTPDownloadHandler) ConnPoolStats() *ConnPoolStats {
	if h.managedTransport != nil {
		return h.managedTransport.Stats()
	}
	return nil
}

// Config 返回当前连接池配置。
// 仅在通过 NewHTTPDownloadHandlerWithConfig 创建时可用，否则返回 nil。
func (h *HTTPDownloadHandler) Config() *ConnPoolConfig {
	return h.config
}

// bytesReader 是一个简单的 bytes.Reader 包装。
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data}
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
