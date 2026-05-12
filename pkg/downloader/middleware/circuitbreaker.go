package middleware

import (
	"context"
	"log/slog"
	"net/url"
	"sync"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// CircuitState 表示熔断器状态。
type CircuitState int

const (
	// CircuitClosed 关闭状态（正常通行）。
	CircuitClosed CircuitState = iota
	// CircuitOpen 打开状态（拒绝请求）。
	CircuitOpen
	// CircuitHalfOpen 半开状态（允许少量探测请求）。
	CircuitHalfOpen
)

// String 返回熔断器状态的字符串表示。
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// circuitBreaker 表示单个域名的熔断器实例。
type circuitBreaker struct {
	mu               sync.Mutex
	state            CircuitState
	consecutiveFails int
	lastFailTime     time.Time
	halfOpenSucc     int // 半开状态下连续成功次数
}

// CircuitBreakerMiddleware 实现域名级别的熔断器模式。
// 当某个域名连续失败次数达到阈值时，熔断器进入 Open 状态，暂停对该域名的请求。
// 经过恢复超时后进入 Half-Open 状态，允许少量探测请求通过。
// 如果探测成功则恢复为 Closed 状态，否则重新进入 Open 状态。
//
// 状态机：
//
//	Closed ──(连续失败 >= threshold)──► Open
//	Open ──(超时到期)──► Half-Open
//	Half-Open ──(探测成功 >= successThreshold)──► Closed
//	Half-Open ──(探测失败)──► Open
//
// 相关配置：
//   - CIRCUIT_BREAKER_ENABLED: 是否启用熔断器（默认 false）
//   - CIRCUIT_BREAKER_FAIL_THRESHOLD: 连续失败阈值（默认 5）
//   - CIRCUIT_BREAKER_RECOVERY_TIMEOUT: 恢复超时时间（默认 30s）
//   - CIRCUIT_BREAKER_HALF_OPEN_MAX_REQUESTS: 半开状态最大探测请求数（默认 1）
//   - CIRCUIT_BREAKER_SUCCESS_THRESHOLD: 半开状态恢复所需连续成功次数（默认 2）
//   - CIRCUIT_BREAKER_HTTP_CODES: 触发熔断的 HTTP 状态码列表（默认 [500, 502, 503, 504]）
type CircuitBreakerMiddleware struct {
	BaseDownloaderMiddleware

	// 配置
	failThreshold    int           // 连续失败阈值
	recoveryTimeout  time.Duration // Open → Half-Open 的恢复超时
	halfOpenMaxReqs  int           // 半开状态最大探测请求数
	successThreshold int           // 半开状态恢复所需连续成功次数
	tripHTTPCodes    map[int]struct{}

	// 状态
	breakers sync.Map // map[string]*circuitBreaker，key 为域名

	// 依赖
	stats  stats.Collector
	logger *slog.Logger

	// 时间函数（便于测试）
	nowFunc func() time.Time
}

// CircuitBreakerOption 是 CircuitBreakerMiddleware 的可选配置函数。
type CircuitBreakerOption func(*CircuitBreakerMiddleware)

// WithCircuitBreakerFailThreshold 设置连续失败阈值。
func WithCircuitBreakerFailThreshold(n int) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		if n > 0 {
			m.failThreshold = n
		}
	}
}

// WithCircuitBreakerRecoveryTimeout 设置恢复超时时间。
func WithCircuitBreakerRecoveryTimeout(d time.Duration) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		if d > 0 {
			m.recoveryTimeout = d
		}
	}
}

// WithCircuitBreakerHalfOpenMaxRequests 设置半开状态最大探测请求数。
func WithCircuitBreakerHalfOpenMaxRequests(n int) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		if n > 0 {
			m.halfOpenMaxReqs = n
		}
	}
}

// WithCircuitBreakerSuccessThreshold 设置半开状态恢复所需连续成功次数。
func WithCircuitBreakerSuccessThreshold(n int) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		if n > 0 {
			m.successThreshold = n
		}
	}
}

// WithCircuitBreakerHTTPCodes 设置触发熔断的 HTTP 状态码列表。
func WithCircuitBreakerHTTPCodes(codes []int) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		m.tripHTTPCodes = make(map[int]struct{}, len(codes))
		for _, code := range codes {
			m.tripHTTPCodes[code] = struct{}{}
		}
	}
}

// withCircuitBreakerNowFunc 设置时间函数（仅用于测试）。
func withCircuitBreakerNowFunc(fn func() time.Time) CircuitBreakerOption {
	return func(m *CircuitBreakerMiddleware) {
		m.nowFunc = fn
	}
}

// NewCircuitBreakerMiddleware 创建一个熔断器中间件。
func NewCircuitBreakerMiddleware(sc stats.Collector, logger *slog.Logger, opts ...CircuitBreakerOption) *CircuitBreakerMiddleware {
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}

	m := &CircuitBreakerMiddleware{
		failThreshold:    5,
		recoveryTimeout:  30 * time.Second,
		halfOpenMaxReqs:  1,
		successThreshold: 2,
		tripHTTPCodes: map[int]struct{}{
			500: {},
			502: {},
			503: {},
			504: {},
		},
		stats:   sc,
		logger:  logger,
		nowFunc: time.Now,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// ProcessRequest 检查目标域名的熔断器状态。
// 如果熔断器处于 Open 状态且未超时，则拒绝请求（返回 ErrIgnoreRequest）。
func (m *CircuitBreakerMiddleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	domain := m.getDomain(request)
	cb := m.getBreaker(domain)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		// 正常通行
		return nil, nil

	case CircuitOpen:
		// 检查是否超过恢复超时
		if m.nowFunc().Sub(cb.lastFailTime) >= m.recoveryTimeout {
			// 转入半开状态
			cb.state = CircuitHalfOpen
			cb.halfOpenSucc = 0
			m.logger.Info("circuit breaker transitioning to half-open",
				"domain", domain,
			)
			m.stats.IncValue("circuitbreaker/half_open", 1, 0)
			return nil, nil
		}
		// 仍在 Open 状态，拒绝请求
		m.logger.Debug("circuit breaker rejecting request (open)",
			"domain", domain,
			"request", request.String(),
		)
		m.stats.IncValue("circuitbreaker/rejected", 1, 0)
		return nil, serrors.ErrIgnoreRequest

	case CircuitHalfOpen:
		// 半开状态允许通行（探测请求）
		return nil, nil
	}

	return nil, nil
}

// ProcessResponse 根据响应结果更新熔断器状态。
func (m *CircuitBreakerMiddleware) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	domain := m.getDomain(request)
	cb := m.getBreaker(domain)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	// 检查响应是否为失败
	_, isFail := m.tripHTTPCodes[response.Status]

	switch cb.state {
	case CircuitClosed:
		if isFail {
			cb.consecutiveFails++
			cb.lastFailTime = m.nowFunc()
			if cb.consecutiveFails >= m.failThreshold {
				cb.state = CircuitOpen
				m.logger.Warn("circuit breaker opened",
					"domain", domain,
					"consecutive_fails", cb.consecutiveFails,
				)
				m.stats.IncValue("circuitbreaker/opened", 1, 0)
			}
		} else {
			// 成功请求重置连续失败计数
			cb.consecutiveFails = 0
		}

	case CircuitHalfOpen:
		if isFail {
			// 探测失败，重新打开熔断器
			cb.state = CircuitOpen
			cb.lastFailTime = m.nowFunc()
			cb.halfOpenSucc = 0
			m.logger.Warn("circuit breaker re-opened from half-open",
				"domain", domain,
			)
			m.stats.IncValue("circuitbreaker/reopened", 1, 0)
		} else {
			// 探测成功
			cb.halfOpenSucc++
			if cb.halfOpenSucc >= m.successThreshold {
				// 恢复为关闭状态
				cb.state = CircuitClosed
				cb.consecutiveFails = 0
				cb.halfOpenSucc = 0
				m.logger.Info("circuit breaker closed (recovered)",
					"domain", domain,
				)
				m.stats.IncValue("circuitbreaker/closed", 1, 0)
			}
		}
	}

	return response, nil
}

// ProcessException 处理下载异常，更新熔断器状态。
func (m *CircuitBreakerMiddleware) ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error) {
	// 只有可重试的异常才触发熔断器计数
	if !serrors.IsRetryable(err) {
		return nil, nil
	}

	domain := m.getDomain(request)
	cb := m.getBreaker(domain)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		cb.consecutiveFails++
		cb.lastFailTime = m.nowFunc()
		if cb.consecutiveFails >= m.failThreshold {
			cb.state = CircuitOpen
			m.logger.Warn("circuit breaker opened (exception)",
				"domain", domain,
				"consecutive_fails", cb.consecutiveFails,
				"error", err.Error(),
			)
			m.stats.IncValue("circuitbreaker/opened", 1, 0)
		}

	case CircuitHalfOpen:
		// 探测失败，重新打开
		cb.state = CircuitOpen
		cb.lastFailTime = m.nowFunc()
		cb.halfOpenSucc = 0
		m.logger.Warn("circuit breaker re-opened from half-open (exception)",
			"domain", domain,
			"error", err.Error(),
		)
		m.stats.IncValue("circuitbreaker/reopened", 1, 0)
	}

	return nil, nil // 继续传播异常
}

// GetBreakerState 获取指定域名的熔断器状态（用于监控和调试）。
func (m *CircuitBreakerMiddleware) GetBreakerState(domain string) CircuitState {
	cb := m.getBreaker(domain)
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// GetBreakerConsecutiveFails 获取指定域名的连续失败次数（用于监控和调试）。
func (m *CircuitBreakerMiddleware) GetBreakerConsecutiveFails(domain string) int {
	cb := m.getBreaker(domain)
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.consecutiveFails
}

// ResetBreaker 重置指定域名的熔断器状态（用于手动恢复）。
func (m *CircuitBreakerMiddleware) ResetBreaker(domain string) {
	cb := m.getBreaker(domain)
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitClosed
	cb.consecutiveFails = 0
	cb.halfOpenSucc = 0
}

// getBreaker 获取或创建指定域名的熔断器实例。
func (m *CircuitBreakerMiddleware) getBreaker(domain string) *circuitBreaker {
	if v, ok := m.breakers.Load(domain); ok {
		return v.(*circuitBreaker)
	}
	cb := &circuitBreaker{state: CircuitClosed}
	actual, _ := m.breakers.LoadOrStore(domain, cb)
	return actual.(*circuitBreaker)
}

// getDomain 从请求中提取域名。
func (m *CircuitBreakerMiddleware) getDomain(request *shttp.Request) string {
	// 优先使用 download_slot meta（与 Downloader Slot 一致）
	if slot, ok := request.GetMeta("download_slot"); ok {
		if s, ok := slot.(string); ok && s != "" {
			return s
		}
	}
	return getDomainFromURL(request.URL)
}

// getDomainFromURL 从 URL 中提取域名（host:port 或 host）。
func getDomainFromURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Host
}
