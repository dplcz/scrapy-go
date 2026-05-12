package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// RetryBackoffStrategy 定义重试退避策略类型。
type RetryBackoffStrategy int

const (
	// RetryBackoffNone 无退避（立即重试，向后兼容默认行为）。
	RetryBackoffNone RetryBackoffStrategy = iota
	// RetryBackoffExponential 指数退避 + 抖动。
	RetryBackoffExponential
	// RetryBackoffFixed 固定延迟退避。
	RetryBackoffFixed
)

// RetryConditionFunc 定义自定义重试条件判断函数。
// 返回 true 表示应该重试，false 表示不重试。
type RetryConditionFunc func(statusCode int, err error) bool

// RetryMiddleware 在请求失败时自动重试。
// 支持基于 HTTP 状态码和异常类型的重试，以及指数退避、抖动和差异化策略。
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
//   - RETRY_BACKOFF_ENABLED: 是否启用指数退避（默认 false）
//   - RETRY_BACKOFF_BASE_DELAY: 退避基础延迟（默认 1s）
//   - RETRY_BACKOFF_MAX_DELAY: 退避最大延迟（默认 60s）
//   - RETRY_BACKOFF_JITTER: 是否启用抖动（默认 true）
//   - RETRY_PER_STATUS_MAX_TIMES: 按状态码差异化最大重试次数（map[int]int）
type RetryMiddleware struct {
	BaseDownloaderMiddleware
	maxRetryTimes  int
	retryHTTPCodes map[int]struct{}
	priorityAdjust int
	stats          stats.Collector
	logger         *slog.Logger

	// 指数退避配置
	backoffStrategy  RetryBackoffStrategy
	backoffBaseDelay time.Duration
	backoffMaxDelay  time.Duration
	backoffJitter    bool

	// 差异化重试策略：按 HTTP 状态码配置不同的最大重试次数
	perStatusMaxRetries map[int]int

	// 自定义重试条件（可选，为 nil 时使用默认逻辑）
	retryCondition RetryConditionFunc
}

// RetryOption 是 RetryMiddleware 的可选配置函数。
type RetryOption func(*RetryMiddleware)

// WithRetryBackoff 启用指数退避策略。
// baseDelay 是基础延迟，maxDelay 是最大延迟上限，jitter 是否启用随机抖动。
func WithRetryBackoff(baseDelay, maxDelay time.Duration, jitter bool) RetryOption {
	return func(m *RetryMiddleware) {
		m.backoffStrategy = RetryBackoffExponential
		m.backoffBaseDelay = baseDelay
		m.backoffMaxDelay = maxDelay
		m.backoffJitter = jitter
	}
}

// WithRetryFixedDelay 启用固定延迟退避策略。
func WithRetryFixedDelay(delay time.Duration) RetryOption {
	return func(m *RetryMiddleware) {
		m.backoffStrategy = RetryBackoffFixed
		m.backoffBaseDelay = delay
		m.backoffMaxDelay = delay
		m.backoffJitter = false
	}
}

// WithPerStatusMaxRetries 设置按 HTTP 状态码差异化的最大重试次数。
// 例如 map[int]int{429: 5, 503: 3} 表示 429 最多重试 5 次，503 最多重试 3 次。
func WithPerStatusMaxRetries(perStatus map[int]int) RetryOption {
	return func(m *RetryMiddleware) {
		m.perStatusMaxRetries = perStatus
	}
}

// WithRetryCondition 设置自定义重试条件判断函数。
// 当设置此函数时，将替代默认的 HTTP 状态码和 IsRetryable 判断逻辑。
func WithRetryCondition(fn RetryConditionFunc) RetryOption {
	return func(m *RetryMiddleware) {
		m.retryCondition = fn
	}
}

// NewRetryMiddleware 创建一个 Retry 中间件。
func NewRetryMiddleware(maxRetryTimes int, retryHTTPCodes []int, priorityAdjust int, sc stats.Collector, logger *slog.Logger, opts ...RetryOption) *RetryMiddleware {
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

	m := &RetryMiddleware{
		maxRetryTimes:    maxRetryTimes,
		retryHTTPCodes:   codes,
		priorityAdjust:   priorityAdjust,
		stats:            sc,
		logger:           logger,
		backoffStrategy:  RetryBackoffNone,
		backoffBaseDelay: time.Second,
		backoffMaxDelay:  60 * time.Second,
		backoffJitter:    true,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// ProcessResponse 检查响应状态码，如果在重试列表中则返回 NewRequestError 触发重试。
func (m *RetryMiddleware) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	// 检查 dont_retry meta
	if dontRetry, ok := request.GetMeta("dont_retry"); ok {
		if dr, ok := dontRetry.(bool); ok && dr {
			return response, nil
		}
	}

	needRetry := false
	if m.retryCondition != nil {
		needRetry = m.retryCondition(response.Status, nil)
	} else {
		_, needRetry = m.retryHTTPCodes[response.Status]
	}

	if needRetry {
		reason := fmt.Sprintf("%d %s", response.Status, statusText(response.Status))
		retryReq := m.retry(request, reason, response.Status)
		if retryReq != nil {
			// 返回 NewRequestError，由 Manager 传播给 Engine 重新调度
			return nil, serrors.NewNewRequestError(retryReq, "retry")
		}
	}

	return response, nil
}

// ProcessException 检查异常是否可重试。
func (m *RetryMiddleware) ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error) {
	// 检查 dont_retry meta
	if dontRetry, ok := request.GetMeta("dont_retry"); ok {
		if dr, ok := dontRetry.(bool); ok && dr {
			return nil, nil
		}
	}

	shouldRetry := false
	if m.retryCondition != nil {
		shouldRetry = m.retryCondition(0, err)
	} else {
		shouldRetry = serrors.IsRetryable(err)
	}

	if shouldRetry {
		retryReq := m.retry(request, err.Error(), 0)
		if retryReq != nil {
			// 返回 NewRequestError，由 Manager 传播给 Engine 重新调度
			return nil, serrors.NewNewRequestError(retryReq, "retry")
		}
	}

	return nil, nil // 继续传播异常
}

// retry 创建重试请求。
func (m *RetryMiddleware) retry(request *shttp.Request, reason string, statusCode int) *shttp.Request {
	retryTimes := 0
	if v, ok := request.GetMeta("retry_times"); ok {
		if rt, ok := v.(int); ok {
			retryTimes = rt
		}
	}
	retryTimes++

	maxRetryTimes := m.getMaxRetryTimes(request, statusCode)

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

		// 计算退避延迟并存入 Meta
		if m.backoffStrategy != RetryBackoffNone {
			delay := m.calculateBackoffDelay(retryTimes)
			newReq.SetMeta("download_delay", delay)
		}

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

// getMaxRetryTimes 获取当前请求的最大重试次数。
// 优先级：请求级 Meta > 按状态码配置 > 全局配置。
func (m *RetryMiddleware) getMaxRetryTimes(request *shttp.Request, statusCode int) int {
	// 请求级别覆盖最大重试次数
	if v, ok := request.GetMeta("max_retry_times"); ok {
		if mrt, ok := v.(int); ok {
			return mrt
		}
	}

	// 按状态码差异化配置
	if statusCode > 0 && m.perStatusMaxRetries != nil {
		if maxTimes, ok := m.perStatusMaxRetries[statusCode]; ok {
			return maxTimes
		}
	}

	return m.maxRetryTimes
}

// calculateBackoffDelay 计算退避延迟。
// 指数退避公式：delay = min(base * 2^(attempt-1) + jitter, maxDelay)
func (m *RetryMiddleware) calculateBackoffDelay(attempt int) time.Duration {
	switch m.backoffStrategy {
	case RetryBackoffExponential:
		// 指数退避：base * 2^(attempt-1)
		exp := math.Pow(2, float64(attempt-1))
		delay := time.Duration(float64(m.backoffBaseDelay) * exp)

		// 添加抖动：[0, delay * 0.5)
		if m.backoffJitter {
			jitter := time.Duration(rand.Float64() * float64(delay) * 0.5)
			delay += jitter
		}

		// 限制最大延迟
		if delay > m.backoffMaxDelay {
			delay = m.backoffMaxDelay
		}

		return delay

	case RetryBackoffFixed:
		return m.backoffBaseDelay

	default:
		return 0
	}
}

// GetRetryRequest 是一个公共辅助函数，可在 Spider 回调中手动触发重试。
// 对应 Scrapy 的 get_retry_request 函数。
func GetRetryRequest(request *shttp.Request, reason string, maxRetryTimes int, priorityAdjust int, sc stats.Collector, logger *slog.Logger) *shttp.Request {
	m := &RetryMiddleware{
		maxRetryTimes:  maxRetryTimes,
		priorityAdjust: priorityAdjust,
		stats:          sc,
		logger:         logger,
	}
	return m.retry(request, reason, 0)
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
