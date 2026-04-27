package middleware

import (
	"context"
	"fmt"
	"log/slog"

	scrapy_errors "github.com/dplcz/scrapy-go/pkg/errors"
	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// RetryMiddleware 在请求失败时自动重试。
// 支持基于 HTTP 状态码和异常类型的重试。
//
// 对应 Scrapy 的 RetryMiddleware（优先级 550）。
//
// 当需要重试时，中间件返回 NewRequestError，由 Engine 将新请求重新调度到 Scheduler。
// 这种方式替代了之前通过 Meta 键传递重试请求的 hack 方式。
//
// 相关配置：
//   - RETRY_ENABLED: 是否启用重试（默认 true）
//   - RETRY_TIMES: 最大重试次数（默认 2，即总共 3 次请求）
//   - RETRY_HTTP_CODES: 需要重试的 HTTP 状态码列表
//   - RETRY_PRIORITY_ADJUST: 重试请求的优先级调整值（默认 -1）
type RetryMiddleware struct {
	BaseDownloaderMiddleware
	maxRetryTimes  int
	retryHTTPCodes map[int]struct{}
	priorityAdjust int
	stats          stats.Collector
	logger         *slog.Logger
}

// NewRetryMiddleware 创建一个 Retry 中间件。
func NewRetryMiddleware(maxRetryTimes int, retryHTTPCodes []int, priorityAdjust int, sc stats.Collector, logger *slog.Logger) *RetryMiddleware {
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}

	codes := make(map[int]struct{}, len(retryHTTPCodes))
	for _, code := range retryHTTPCodes {
		codes[code] = struct{}{}
	}

	return &RetryMiddleware{
		maxRetryTimes:  maxRetryTimes,
		retryHTTPCodes: codes,
		priorityAdjust: priorityAdjust,
		stats:          sc,
		logger:         logger,
	}
}

// ProcessResponse 检查响应状态码，如果在重试列表中则返回 NewRequestError 触发重试。
func (m *RetryMiddleware) ProcessResponse(ctx context.Context, request *scrapy_http.Request, response *scrapy_http.Response) (*scrapy_http.Response, error) {
	// 检查 dont_retry meta
	if dontRetry, ok := request.GetMeta("dont_retry"); ok {
		if dr, ok := dontRetry.(bool); ok && dr {
			return response, nil
		}
	}

	// 检查状态码是否需要重试
	if _, needRetry := m.retryHTTPCodes[response.Status]; needRetry {
		reason := fmt.Sprintf("%d %s", response.Status, statusText(response.Status))
		retryReq := m.retry(request, reason)
		if retryReq != nil {
			// 返回 NewRequestError，由 Manager 传播给 Engine 重新调度
			return nil, scrapy_errors.NewNewRequestError(retryReq, "retry")
		}
	}

	return response, nil
}

// ProcessException 检查异常是否可重试。
func (m *RetryMiddleware) ProcessException(ctx context.Context, request *scrapy_http.Request, err error) (*scrapy_http.Response, error) {
	// 检查 dont_retry meta
	if dontRetry, ok := request.GetMeta("dont_retry"); ok {
		if dr, ok := dontRetry.(bool); ok && dr {
			return nil, nil
		}
	}

	// 检查是否为可重试的异常
	if scrapy_errors.IsRetryable(err) {
		retryReq := m.retry(request, err.Error())
		if retryReq != nil {
			// 返回 NewRequestError，由 Manager 传播给 Engine 重新调度
			return nil, scrapy_errors.NewNewRequestError(retryReq, "retry")
		}
	}

	return nil, nil // 继续传播异常
}

// retry 创建重试请求。
func (m *RetryMiddleware) retry(request *scrapy_http.Request, reason string) *scrapy_http.Request {
	retryTimes := 0
	if v, ok := request.GetMeta("retry_times"); ok {
		if rt, ok := v.(int); ok {
			retryTimes = rt
		}
	}
	retryTimes++

	maxRetryTimes := m.maxRetryTimes
	// 允许请求级别覆盖最大重试次数
	if v, ok := request.GetMeta("max_retry_times"); ok {
		if mrt, ok := v.(int); ok {
			maxRetryTimes = mrt
		}
	}

	if retryTimes <= maxRetryTimes {
		m.logger.Debug("retrying request",
			"request", request.String(),
			"retry_times", retryTimes,
			"reason", reason,
		)

		newReq := request.Copy()
		newReq.SetMeta("retry_times", retryTimes)
		newReq.DontFilter = true
		newReq.Priority = request.Priority + m.priorityAdjust

		m.stats.IncValue("retry/count", 1, 0)
		m.stats.IncValue(fmt.Sprintf("retry/reason_count/%s", reason), 1, 0)
		return newReq
	}

	m.stats.IncValue("retry/max_reached", 1, 0)
	m.logger.Error("gave up retrying request",
		"request", request.String(),
		"retry_times", retryTimes,
		"reason", reason,
	)
	return nil
}

// GetRetryRequest 是一个公共辅助函数，可在 Spider 回调中手动触发重试。
// 对应 Scrapy 的 get_retry_request 函数。
func GetRetryRequest(request *scrapy_http.Request, reason string, maxRetryTimes int, priorityAdjust int, sc stats.Collector, logger *slog.Logger) *scrapy_http.Request {
	m := &RetryMiddleware{
		maxRetryTimes:  maxRetryTimes,
		priorityAdjust: priorityAdjust,
		stats:          sc,
		logger:         logger,
	}
	return m.retry(request, reason)
}

// statusText 返回 HTTP 状态码的文本描述。
func statusText(code int) string {
	switch code {
	case 400:
		return "Bad Request"
	case 408:
		return "Request Timeout"
	case 429:
		return "Too Many Requests"
	case 500:
		return "Internal Server Error"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Gateway Timeout"
	case 522:
		return "Connection Timed Out"
	case 524:
		return "A Timeout Occurred"
	default:
		return fmt.Sprintf("HTTP %d", code)
	}
}
