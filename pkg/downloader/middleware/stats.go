package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// DownloaderStatsMiddleware 统计下载器的请求/响应/异常数据。
// 对应 Scrapy 的 DownloaderStats 中间件。
//
// 统计项：
//   - downloader/request_count — 总请求数
//   - downloader/request_method_count/{METHOD} — 按 HTTP 方法统计请求数
//   - downloader/request_bytes — 请求总字节数（估算）
//   - downloader/response_count — 总响应数
//   - downloader/response_status_count/{STATUS} — 按状态码统计响应数
//   - downloader/response_bytes — 响应总字节数
//   - downloader/exception_count — 总异常数
//   - downloader/exception_type_count/{TYPE} — 按异常类型统计异常数
//
// 配置项：
//   - DOWNLOADER_STATS: 是否启用下载器统计（默认 true）
type DownloaderStatsMiddleware struct {
	BaseDownloaderMiddleware

	stats  stats.Collector
	logger *slog.Logger
}

// NewDownloaderStatsMiddleware 创建一个新的下载器统计中间件。
func NewDownloaderStatsMiddleware(sc stats.Collector, logger *slog.Logger) *DownloaderStatsMiddleware {
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DownloaderStatsMiddleware{
		stats:  sc,
		logger: logger,
	}
}

// ProcessRequest 统计请求信息。
func (m *DownloaderStatsMiddleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	m.stats.IncValue("downloader/request_count", 1, 0)
	m.stats.IncValue(fmt.Sprintf("downloader/request_method_count/%s", request.Method), 1, 0)

	// 估算请求字节数
	reqBytes := estimateRequestSize(request)
	m.stats.IncValue("downloader/request_bytes", reqBytes, 0)

	// 记录请求开始时间到 Meta，用于计算响应耗时
	request.SetMeta("_download_start_time", time.Now())

	return nil, nil
}

// ProcessResponse 统计响应信息。
func (m *DownloaderStatsMiddleware) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	m.stats.IncValue("downloader/response_count", 1, 0)
	m.stats.IncValue(fmt.Sprintf("downloader/response_status_count/%d", response.Status), 1, 0)

	// 统计响应字节数
	respBytes := estimateResponseSize(response)
	m.stats.IncValue("downloader/response_bytes", respBytes, 0)

	// 统计下载耗时
	if startTime, ok := request.GetMeta("_download_start_time"); ok {
		if t, ok := startTime.(time.Time); ok {
			elapsed := time.Since(t)
			m.stats.MaxValue("downloader/max_download_time", elapsed.Seconds())
		}
	}

	return response, nil
}

// ProcessException 统计异常信息。
func (m *DownloaderStatsMiddleware) ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error) {
	m.stats.IncValue("downloader/exception_count", 1, 0)

	// 获取错误类型名称
	errTypeName := getErrorTypeName(err)
	m.stats.IncValue(fmt.Sprintf("downloader/exception_type_count/%s", errTypeName), 1, 0)

	return nil, nil
}

// estimateRequestSize 估算请求的字节大小。
// 包括请求行、请求头和请求体。
func estimateRequestSize(request *shttp.Request) int {
	// 请求行: "METHOD URL HTTP/1.1\r\n"
	size := len(request.Method) + 1 + len(request.URL.String()) + len(" HTTP/1.1\r\n")

	// 请求头
	for key, values := range request.Headers {
		for _, v := range values {
			// "Key: Value\r\n"
			size += len(key) + 2 + len(v) + 2
		}
	}

	// 空行
	size += 2

	// 请求体
	size += len(request.Body)

	return size
}

// estimateResponseSize 估算响应的字节大小。
// 包括状态行、响应头和响应体。
func estimateResponseSize(response *shttp.Response) int {
	// 状态行: "HTTP/1.1 STATUS REASON\r\n"（约 15 字节 + 状态描述）
	size := 15

	// 响应头
	for key, values := range response.Headers {
		for _, v := range values {
			size += len(key) + 2 + len(v) + 2
		}
	}

	// 空行
	size += 2

	// 响应体
	size += len(response.Body)

	return size
}

// getErrorTypeName 获取错误的类型名称。
func getErrorTypeName(err error) string {
	if err == nil {
		return "unknown"
	}

	t := reflect.TypeOf(err)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	return t.String()
}
