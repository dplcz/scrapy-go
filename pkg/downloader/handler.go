// Package downloader 实现了 scrapy-go 框架的下载器系统。
//
// 下载器负责执行 HTTP 请求并返回响应，通过 Slot 机制控制并发和延迟。
// 对应 Scrapy Python 版本中 scrapy.core.downloader 模块的功能。
package downloader

import (
	"context"
	"io"
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
// 支持 HTTP/1.1 和 HTTP/2。
type HTTPDownloadHandler struct {
	client    *http.Client
	transport *http.Transport
}

// NewHTTPDownloadHandler 创建一个新的 HTTP 下载处理器。
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

// Download 执行 HTTP 下载。
// 如果 request.Meta["proxy"] 设置了代理 URL，则通过该代理发送请求。
func (h *HTTPDownloadHandler) Download(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	// 构建 net/http.Request
	httpReq, err := http.NewRequestWithContext(ctx, request.Method, request.URL.String(), nil)
	if err != nil {
		return nil, err
	}

	// 设置请求体
	if len(request.Body) > 0 {
		httpReq.Body = io.NopCloser(newBytesReader(request.Body))
		httpReq.ContentLength = int64(len(request.Body))
	}

	// 复制请求头
	for key, values := range request.Headers {
		for _, v := range values {
			httpReq.Header.Add(key, v)
		}
	}

	// 设置 Cookies
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

	// 读取响应体
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
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
