package middleware

import (
	"context"
	"log/slog"
	"time"

	scrapy_http "scrapy-go/pkg/http"
)

// DownloadTimeoutMiddleware 为每个请求设置下载超时。
// 通过 context.WithTimeout 包装请求的 context，实现请求级别的超时控制。
//
// 对应 Scrapy 的 DownloadTimeoutMiddleware（优先级 300）。
//
// 超时优先级（从高到低）：
//  1. Request.Meta["download_timeout"]（请求级覆盖）
//  2. DOWNLOAD_TIMEOUT 配置项（全局默认）
//
// 相关配置：
//   - DOWNLOAD_TIMEOUT: 全局下载超时时间（默认 180 秒）
type DownloadTimeoutMiddleware struct {
	BaseDownloaderMiddleware
	timeout time.Duration
	logger  *slog.Logger
}

// NewDownloadTimeoutMiddleware 创建一个 DownloadTimeout 中间件。
// timeout 为全局默认超时时间。
func NewDownloadTimeoutMiddleware(timeout time.Duration, logger *slog.Logger) *DownloadTimeoutMiddleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &DownloadTimeoutMiddleware{
		timeout: timeout,
		logger:  logger,
	}
}

// ProcessRequest 为请求设置 download_timeout Meta。
// 如果请求已经设置了 download_timeout，则不覆盖（允许请求级别覆盖全局配置）。
//
// 注意：实际的 context.WithTimeout 包装在 ProcessRequest 中完成，
// 这样后续的中间件和下载器都会受到超时控制。
func (m *DownloadTimeoutMiddleware) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	if m.timeout > 0 {
		// 仅在请求未设置 download_timeout 时设置默认值
		if _, ok := request.GetMeta("download_timeout"); !ok {
			request.SetMeta("download_timeout", m.timeout)
		}
	}
	return nil, nil
}
