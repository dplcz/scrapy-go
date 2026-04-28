package middleware

import (
	"context"
	"log/slog"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// UrlLengthMiddleware 过滤超长 URL 的请求。
// 对应 Scrapy 的 scrapy.spidermiddlewares.urllength.UrlLengthMiddleware。
//
// 在 ProcessOutput 阶段检查输出中的 Request URL 长度，
// 超过 URLLENGTH_LIMIT 的请求将被丢弃并记录日志。
//
// 配置项：
//   - URLLENGTH_LIMIT: URL 最大长度（默认 2083），设为 0 禁用
type UrlLengthMiddleware struct {
	BaseSpiderMiddleware

	maxLength int
	stats     stats.Collector
	logger    *slog.Logger
}

// NewUrlLengthMiddleware 创建一个新的 UrlLength 中间件。
// maxLength 为 URL 最大长度，设为 0 禁用。
func NewUrlLengthMiddleware(maxLength int, sc stats.Collector, logger *slog.Logger) *UrlLengthMiddleware {
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &UrlLengthMiddleware{
		maxLength: maxLength,
		stats:     sc,
		logger:    logger,
	}
}

// ProcessOutput 过滤超长 URL 的请求。
func (m *UrlLengthMiddleware) ProcessOutput(ctx context.Context, response *shttp.Response, result []spider.Output) ([]spider.Output, error) {
	if m.maxLength <= 0 {
		return result, nil
	}

	filtered := make([]spider.Output, 0, len(result))
	for _, output := range result {
		if output.IsRequest() && output.Request.URL != nil {
			urlStr := output.Request.URL.String()
			if len(urlStr) > m.maxLength {
				m.logger.Info("ignoring link (url length > limit)",
					"maxlength", m.maxLength,
					"url_length", len(urlStr),
					"url", urlStr,
				)
				m.stats.IncValue("urllength/request_ignored_count", 1, 0)
				continue
			}
		}
		filtered = append(filtered, output)
	}

	return filtered, nil
}
