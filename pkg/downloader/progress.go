package downloader

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// DownloadProgressCallback 定义下载进度回调函数类型。
// 参数：
//   - bytesRead: 已读取的字节数
//   - totalBytes: 总字节数（-1 表示未知，如 chunked 传输）
//   - request: 关联的请求
type DownloadProgressCallback func(bytesRead int64, totalBytes int64, request *shttp.Request)

// DownloadProgressMetaKey 是请求 Meta 中存储进度回调的键名。
const DownloadProgressMetaKey = "download_progress_callback"

// ProgressHTTPDownloadHandler 是支持下载进度回调的 HTTP 下载处理器。
//
// 在标准 HTTPDownloadHandler 的基础上增加了下载进度通知能力：
//   - 通过 Request.Meta["download_progress_callback"] 设置进度回调
//   - 支持已知大小（Content-Length）和未知大小（chunked）的进度报告
//   - 进度回调在下载 goroutine 中同步调用，不引入额外 goroutine
//   - 可配置进度报告的最小间隔，避免高频回调影响性能
//
// 使用场景：
//   - 大文件下载进度展示
//   - 下载速率监控
//   - 超大响应体的流式处理决策
type ProgressHTTPDownloadHandler struct {
	client    *http.Client
	transport *ManagedTransport

	// minReportInterval 进度报告的最小间隔。
	// 避免小文件或高速下载时回调过于频繁。
	minReportInterval time.Duration
}

// NewProgressHTTPDownloadHandler 创建一个支持进度回调的 HTTP 下载处理器。
//
// 参数：
//   - timeout: 全局下载超时时间
//   - config: 连接池配置（传 nil 使用默认配置）
//   - minReportInterval: 进度报告最小间隔（传 0 使用默认值 100ms）
func NewProgressHTTPDownloadHandler(timeout time.Duration, config *ConnPoolConfig, minReportInterval time.Duration) *ProgressHTTPDownloadHandler {
	if config == nil {
		config = DefaultConnPoolConfig()
	}
	if minReportInterval <= 0 {
		minReportInterval = 100 * time.Millisecond
	}

	transport := NewManagedTransport(config)

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &ProgressHTTPDownloadHandler{
		client:            client,
		transport:         transport,
		minReportInterval: minReportInterval,
	}
}

// Download 执行带进度回调的 HTTP 下载。
//
// 如果请求 Meta 中设置了 download_progress_callback，则在读取响应体时
// 定期调用回调函数报告进度。否则行为与标准 HTTPDownloadHandler 完全一致。
//
// 下载完成后会将 download_latency（time.Duration）设置到 request.Meta 中，
// 供 AutoThrottle、Telemetry 等扩展在 RequestLeftDownloader 信号中消费。
func (h *ProgressHTTPDownloadHandler) Download(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	// 记录下载开始时间，用于计算 download_latency
	startTime := time.Now()

	// 构造 net/http.Request
	httpReq := &http.Request{
		Method:     request.Method,
		URL:        request.URL,
		Host:       request.URL.Host,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
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

	// 选择 HTTP 客户端
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

	// 获取进度回调
	progressCb := h.getProgressCallback(request)

	// 读取响应体（带进度回调）
	var body []byte
	totalBytes := httpResp.ContentLength

	if progressCb != nil {
		body, err = h.readBodyWithProgress(httpResp.Body, totalBytes, request, progressCb)
	} else {
		// 无进度回调时使用标准读取路径（零开销）
		if totalBytes > 0 {
			body = make([]byte, totalBytes)
			_, err = io.ReadFull(httpResp.Body, body)
		} else {
			body, err = io.ReadAll(httpResp.Body)
		}
	}

	if err != nil {
		return nil, err
	}

	// 设置 download_latency 到 Request Meta（对齐 Scrapy 原版：在 handler 层面设置）
	request.SetMeta("download_latency", time.Since(startTime))

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

// readBodyWithProgress 带进度回调地读取响应体。
func (h *ProgressHTTPDownloadHandler) readBodyWithProgress(
	reader io.Reader,
	totalBytes int64,
	request *shttp.Request,
	callback DownloadProgressCallback,
) ([]byte, error) {
	// 创建进度追踪 reader
	pr := &progressReader{
		reader:            reader,
		totalBytes:        totalBytes,
		callback:          callback,
		request:           request,
		minReportInterval: h.minReportInterval,
	}

	// 根据是否已知大小选择读取策略
	if totalBytes > 0 {
		body := make([]byte, totalBytes)
		_, err := io.ReadFull(pr, body)
		if err != nil {
			return nil, err
		}
		// 确保最终进度为 100%
		callback(totalBytes, totalBytes, request)
		return body, nil
	}

	// 未知大小：使用 io.ReadAll
	body, err := io.ReadAll(pr)
	if err != nil {
		return nil, err
	}
	// 报告最终大小
	callback(int64(len(body)), int64(len(body)), request)
	return body, nil
}

// progressReader 包装 io.Reader，在读取过程中触发进度回调。
type progressReader struct {
	reader            io.Reader
	totalBytes        int64
	bytesRead         atomic.Int64
	callback          DownloadProgressCallback
	request           *shttp.Request
	minReportInterval time.Duration
	lastReport        time.Time
}

// Read 实现 io.Reader 接口，在读取数据时触发进度回调。
func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 {
		current := pr.bytesRead.Add(int64(n))

		// 检查是否需要报告进度（基于时间间隔）
		now := time.Now()
		if now.Sub(pr.lastReport) >= pr.minReportInterval {
			pr.lastReport = now
			pr.callback(current, pr.totalBytes, pr.request)
		}
	}
	return n, err
}

// getProgressCallback 从请求 Meta 中获取进度回调函数。
func (h *ProgressHTTPDownloadHandler) getProgressCallback(request *shttp.Request) DownloadProgressCallback {
	v, ok := request.GetMeta(DownloadProgressMetaKey)
	if !ok || v == nil {
		return nil
	}

	cb, ok := v.(DownloadProgressCallback)
	if !ok {
		// 尝试兼容 func(int64, int64, *shttp.Request) 签名
		if fn, ok := v.(func(int64, int64, *shttp.Request)); ok {
			return DownloadProgressCallback(fn)
		}
		return nil
	}

	return cb
}

// getProxyURL 从 request.Meta["proxy"] 中提取代理 URL。
func (h *ProgressHTTPDownloadHandler) getProxyURL(request *shttp.Request) *url.URL {
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
func (h *ProgressHTTPDownloadHandler) clientWithProxy(proxyURL *url.URL) *http.Client {
	proxyTransport := h.transport.Transport.Clone()
	proxyTransport.Proxy = http.ProxyURL(proxyURL)

	return &http.Client{
		Timeout:   h.client.Timeout,
		Transport: proxyTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Close 关闭处理器，释放所有连接。
func (h *ProgressHTTPDownloadHandler) Close() error {
	h.client.CloseIdleConnections()
	return nil
}

// ConnPoolStats 返回连接池统计信息。
func (h *ProgressHTTPDownloadHandler) ConnPoolStats() *ConnPoolStats {
	return h.transport.Stats()
}
