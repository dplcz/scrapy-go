package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// RetryMiddleware 指数退避测试
// ============================================================================

func TestRetryMiddlewareBackoffExponential(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRetryMiddleware(3, []int{500}, -1, sc, nil,
		WithRetryBackoff(100*time.Millisecond, 10*time.Second, false),
	)

	req := shttp.MustNewRequest("https://example.com")
	resp := shttp.MustNewResponse("https://example.com", 500,
		shttp.WithRequest(req),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *serrors.NewRequestError
	if !errors.As(err, &newReqErr) {
		t.Fatal("should be able to extract NewRequestError")
	}

	rr := newReqErr.Request.(*shttp.Request)

	// 第一次重试：delay = 100ms * 2^0 = 100ms（无抖动）
	delay, ok := rr.GetMeta("download_delay")
	if !ok {
		t.Fatal("retry request should have download_delay meta")
	}
	d := delay.(time.Duration)
	if d != 100*time.Millisecond {
		t.Errorf("expected 100ms delay for first retry, got %v", d)
	}
}

func TestRetryMiddlewareBackoffExponentialSecondRetry(t *testing.T) {
	mw := NewRetryMiddleware(3, []int{500}, -1, nil, nil,
		WithRetryBackoff(100*time.Millisecond, 10*time.Second, false),
	)

	req := shttp.MustNewRequest("https://example.com")
	req.SetMeta("retry_times", 1) // 已重试 1 次
	resp := shttp.MustNewResponse("https://example.com", 500,
		shttp.WithRequest(req),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *serrors.NewRequestError
	errors.As(err, &newReqErr)
	rr := newReqErr.Request.(*shttp.Request)

	// 第二次重试：delay = 100ms * 2^1 = 200ms
	delay, _ := rr.GetMeta("download_delay")
	d := delay.(time.Duration)
	if d != 200*time.Millisecond {
		t.Errorf("expected 200ms delay for second retry, got %v", d)
	}
}

func TestRetryMiddlewareBackoffMaxDelay(t *testing.T) {
	mw := NewRetryMiddleware(10, []int{500}, -1, nil, nil,
		WithRetryBackoff(1*time.Second, 5*time.Second, false),
	)

	req := shttp.MustNewRequest("https://example.com")
	req.SetMeta("retry_times", 9) // 已重试 9 次
	resp := shttp.MustNewResponse("https://example.com", 500,
		shttp.WithRequest(req),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *serrors.NewRequestError
	errors.As(err, &newReqErr)
	rr := newReqErr.Request.(*shttp.Request)

	// 延迟应被限制在 maxDelay
	delay, _ := rr.GetMeta("download_delay")
	d := delay.(time.Duration)
	if d > 5*time.Second {
		t.Errorf("delay should be capped at 5s, got %v", d)
	}
	if d != 5*time.Second {
		t.Errorf("expected exactly 5s (capped), got %v", d)
	}
}

func TestRetryMiddlewareBackoffWithJitter(t *testing.T) {
	mw := NewRetryMiddleware(3, []int{500}, -1, nil, nil,
		WithRetryBackoff(100*time.Millisecond, 10*time.Second, true),
	)

	req := shttp.MustNewRequest("https://example.com")
	resp := shttp.MustNewResponse("https://example.com", 500,
		shttp.WithRequest(req),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *serrors.NewRequestError
	errors.As(err, &newReqErr)
	rr := newReqErr.Request.(*shttp.Request)

	// 有抖动时：delay = 100ms + [0, 50ms)，范围 [100ms, 150ms)
	delay, _ := rr.GetMeta("download_delay")
	d := delay.(time.Duration)
	if d < 100*time.Millisecond || d >= 150*time.Millisecond {
		t.Errorf("expected delay in [100ms, 150ms) with jitter, got %v", d)
	}
}

func TestRetryMiddlewareBackoffFixed(t *testing.T) {
	mw := NewRetryMiddleware(3, []int{500}, -1, nil, nil,
		WithRetryFixedDelay(500*time.Millisecond),
	)

	req := shttp.MustNewRequest("https://example.com")
	resp := shttp.MustNewResponse("https://example.com", 500,
		shttp.WithRequest(req),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *serrors.NewRequestError
	errors.As(err, &newReqErr)
	rr := newReqErr.Request.(*shttp.Request)

	delay, _ := rr.GetMeta("download_delay")
	d := delay.(time.Duration)
	if d != 500*time.Millisecond {
		t.Errorf("expected fixed 500ms delay, got %v", d)
	}
}

func TestRetryMiddlewareNoBackoff(t *testing.T) {
	mw := NewRetryMiddleware(3, []int{500}, -1, nil, nil)

	req := shttp.MustNewRequest("https://example.com")
	resp := shttp.MustNewResponse("https://example.com", 500,
		shttp.WithRequest(req),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *serrors.NewRequestError
	errors.As(err, &newReqErr)
	rr := newReqErr.Request.(*shttp.Request)

	// 无退避时不应设置 download_delay
	_, ok := rr.GetMeta("download_delay")
	if ok {
		t.Error("no backoff should not set download_delay meta")
	}
}

// ============================================================================
// RetryMiddleware 差异化重试策略测试
// ============================================================================

func TestRetryMiddlewarePerStatusMaxRetries(t *testing.T) {
	mw := NewRetryMiddleware(2, []int{429, 500, 503}, -1, nil, nil,
		WithPerStatusMaxRetries(map[int]int{
			429: 5, // 429 允许更多重试
			503: 1, // 503 只允许 1 次重试
		}),
	)

	tests := []struct {
		name        string
		statusCode  int
		retryTimes  int
		shouldRetry bool
	}{
		{"429 first retry", 429, 0, true},
		{"429 fifth retry", 429, 4, true},
		{"429 sixth retry (max reached)", 429, 5, false},
		{"503 first retry", 503, 0, true},
		{"503 second retry (max reached)", 503, 1, false},
		{"500 first retry (global default)", 500, 0, true},
		{"500 second retry (global default)", 500, 1, true},
		{"500 third retry (global max reached)", 500, 2, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := shttp.MustNewRequest("https://example.com")
			if tt.retryTimes > 0 {
				req.SetMeta("retry_times", tt.retryTimes)
			}
			resp := shttp.MustNewResponse("https://example.com", tt.statusCode,
				shttp.WithRequest(req),
			)

			_, err := mw.ProcessResponse(context.Background(), req, resp)
			gotRetry := errors.Is(err, serrors.ErrNewRequest)
			if gotRetry != tt.shouldRetry {
				t.Errorf("shouldRetry=%v, got %v (err=%v)", tt.shouldRetry, gotRetry, err)
			}
		})
	}
}

func TestRetryMiddlewarePerStatusOverriddenByMeta(t *testing.T) {
	mw := NewRetryMiddleware(2, []int{500}, -1, nil, nil,
		WithPerStatusMaxRetries(map[int]int{500: 1}),
	)

	req := shttp.MustNewRequest("https://example.com")
	req.SetMeta("max_retry_times", 10) // Meta 优先级最高
	req.SetMeta("retry_times", 5)
	resp := shttp.MustNewResponse("https://example.com", 500,
		shttp.WithRequest(req),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)
	// Meta 设置 max_retry_times=10，当前 retry_times=5，应该继续重试
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Fatalf("meta max_retry_times should override per-status config, got %v", err)
	}
}

func TestRetryMiddlewareCustomCondition(t *testing.T) {
	// 自定义条件：只重试 429 和特定错误
	mw := NewRetryMiddleware(3, nil, -1, nil, nil,
		WithRetryCondition(func(statusCode int, err error) bool {
			if statusCode == 429 {
				return true
			}
			if err != nil && errors.Is(err, serrors.ErrDownloadTimeout) {
				return true
			}
			return false
		}),
	)

	// 429 应该重试
	req := shttp.MustNewRequest("https://example.com")
	resp := shttp.MustNewResponse("https://example.com", 429, shttp.WithRequest(req))
	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Error("429 should trigger retry with custom condition")
	}

	// 500 不应该重试（自定义条件不包含 500）
	req2 := shttp.MustNewRequest("https://example.com")
	resp2 := shttp.MustNewResponse("https://example.com", 500, shttp.WithRequest(req2))
	result, err := mw.ProcessResponse(context.Background(), req2, resp2)
	if err != nil {
		t.Errorf("500 should not trigger retry with custom condition, got err=%v", err)
	}
	if result.Status != 500 {
		t.Error("500 should pass through")
	}

	// ErrDownloadTimeout 应该重试
	req3 := shttp.MustNewRequest("https://example.com")
	_, err = mw.ProcessException(context.Background(), req3, serrors.ErrDownloadTimeout)
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Error("ErrDownloadTimeout should trigger retry with custom condition")
	}

	// 其他错误不应该重试
	req4 := shttp.MustNewRequest("https://example.com")
	_, err = mw.ProcessException(context.Background(), req4, errors.New("random error"))
	if err != nil {
		t.Error("random error should not trigger retry with custom condition")
	}
}

// ============================================================================
// CircuitBreakerMiddleware 测试
// ============================================================================

func TestCircuitBreakerClosedState(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(3),
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 正常状态应该允许请求通过
	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("closed circuit should allow request, got err=%v", err)
	}
	if resp != nil {
		t.Error("closed circuit should return nil response")
	}
}

func TestCircuitBreakerTripsOnConsecutiveFailures(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewCircuitBreakerMiddleware(sc, nil,
		WithCircuitBreakerFailThreshold(3),
		WithCircuitBreakerRecoveryTimeout(1*time.Hour), // 长超时确保不会自动恢复
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 连续 3 次失败
	for i := 0; i < 3; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	// 熔断器应该打开
	state := mw.GetBreakerState("example.com")
	if state != CircuitOpen {
		t.Errorf("expected CircuitOpen after 3 failures, got %v", state)
	}

	// 后续请求应该被拒绝
	_, err := mw.ProcessRequest(context.Background(), req)
	if !errors.Is(err, serrors.ErrIgnoreRequest) {
		t.Fatalf("open circuit should reject request, got err=%v", err)
	}

	// 验证统计
	opened := sc.GetValue("circuitbreaker/opened", 0)
	if opened != 1 {
		t.Errorf("expected circuitbreaker/opened=1, got %v", opened)
	}
	rejected := sc.GetValue("circuitbreaker/rejected", 0)
	if rejected != 1 {
		t.Errorf("expected circuitbreaker/rejected=1, got %v", rejected)
	}
}

func TestCircuitBreakerResetOnSuccess(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(3),
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 2 次失败
	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	// 1 次成功应该重置计数
	resp := shttp.MustNewResponse("https://example.com/page", 200, shttp.WithRequest(req))
	mw.ProcessResponse(context.Background(), req, resp)

	// 再 2 次失败不应该触发熔断
	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	state := mw.GetBreakerState("example.com")
	if state != CircuitClosed {
		t.Errorf("expected CircuitClosed (success reset), got %v", state)
	}
}

func TestCircuitBreakerRecoveryTimeout(t *testing.T) {
	now := time.Now()
	currentTime := now
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(2),
		WithCircuitBreakerRecoveryTimeout(5*time.Second),
		withCircuitBreakerNowFunc(func() time.Time { return currentTime }),
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 触发熔断
	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	// 确认已打开
	state := mw.GetBreakerState("example.com")
	if state != CircuitOpen {
		t.Fatalf("expected CircuitOpen, got %v", state)
	}

	// 超时前仍然拒绝
	currentTime = now.Add(4 * time.Second)
	_, err := mw.ProcessRequest(context.Background(), req)
	if !errors.Is(err, serrors.ErrIgnoreRequest) {
		t.Error("should still reject before recovery timeout")
	}

	// 超时后应转为半开
	currentTime = now.Add(6 * time.Second)
	_, err = mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Errorf("should allow request after recovery timeout, got err=%v", err)
	}

	state = mw.GetBreakerState("example.com")
	if state != CircuitHalfOpen {
		t.Errorf("expected CircuitHalfOpen after timeout, got %v", state)
	}
}

func TestCircuitBreakerHalfOpenRecovery(t *testing.T) {
	now := time.Now()
	currentTime := now
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewCircuitBreakerMiddleware(sc, nil,
		WithCircuitBreakerFailThreshold(2),
		WithCircuitBreakerRecoveryTimeout(1*time.Second),
		WithCircuitBreakerSuccessThreshold(2),
		withCircuitBreakerNowFunc(func() time.Time { return currentTime }),
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 触发熔断
	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	// 超时后转为半开
	currentTime = now.Add(2 * time.Second)
	mw.ProcessRequest(context.Background(), req)

	// 2 次成功探测应恢复
	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 200, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	state := mw.GetBreakerState("example.com")
	if state != CircuitClosed {
		t.Errorf("expected CircuitClosed after successful probes, got %v", state)
	}

	closed := sc.GetValue("circuitbreaker/closed", 0)
	if closed != 1 {
		t.Errorf("expected circuitbreaker/closed=1, got %v", closed)
	}
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	now := time.Now()
	currentTime := now
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewCircuitBreakerMiddleware(sc, nil,
		WithCircuitBreakerFailThreshold(2),
		WithCircuitBreakerRecoveryTimeout(1*time.Second),
		withCircuitBreakerNowFunc(func() time.Time { return currentTime }),
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 触发熔断
	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	// 超时后转为半开
	currentTime = now.Add(2 * time.Second)
	mw.ProcessRequest(context.Background(), req)

	// 探测失败应重新打开
	resp := shttp.MustNewResponse("https://example.com/page", 503, shttp.WithRequest(req))
	mw.ProcessResponse(context.Background(), req, resp)

	state := mw.GetBreakerState("example.com")
	if state != CircuitOpen {
		t.Errorf("expected CircuitOpen after probe failure, got %v", state)
	}

	reopened := sc.GetValue("circuitbreaker/reopened", 0)
	if reopened != 1 {
		t.Errorf("expected circuitbreaker/reopened=1, got %v", reopened)
	}
}

func TestCircuitBreakerExceptionTrip(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(2),
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 可重试异常应计入失败
	mw.ProcessException(context.Background(), req, serrors.ErrDownloadTimeout)
	mw.ProcessException(context.Background(), req, serrors.ErrConnectionRefused)

	state := mw.GetBreakerState("example.com")
	if state != CircuitOpen {
		t.Errorf("expected CircuitOpen after retryable exceptions, got %v", state)
	}
}

func TestCircuitBreakerNonRetryableExceptionIgnored(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(2),
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 不可重试的异常不应计入失败
	mw.ProcessException(context.Background(), req, errors.New("some random error"))
	mw.ProcessException(context.Background(), req, errors.New("another error"))

	state := mw.GetBreakerState("example.com")
	if state != CircuitClosed {
		t.Errorf("non-retryable exceptions should not trip breaker, got %v", state)
	}
}

func TestCircuitBreakerMultipleDomains(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(2),
	)

	req1 := shttp.MustNewRequest("https://domain1.com/page")
	req2 := shttp.MustNewRequest("https://domain2.com/page")

	// domain1 连续失败
	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://domain1.com/page", 500, shttp.WithRequest(req1))
		mw.ProcessResponse(context.Background(), req1, resp)
	}

	// domain1 应该打开
	state1 := mw.GetBreakerState("domain1.com")
	if state1 != CircuitOpen {
		t.Errorf("domain1 should be open, got %v", state1)
	}

	// domain2 应该仍然关闭
	state2 := mw.GetBreakerState("domain2.com")
	if state2 != CircuitClosed {
		t.Errorf("domain2 should be closed, got %v", state2)
	}

	// domain2 请求应该正常通过
	_, err := mw.ProcessRequest(context.Background(), req2)
	if err != nil {
		t.Errorf("domain2 request should pass, got err=%v", err)
	}
}

func TestCircuitBreakerCustomHTTPCodes(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(2),
		WithCircuitBreakerHTTPCodes([]int{429, 503}),
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 500 不在自定义列表中，不应计入失败
	for i := 0; i < 5; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	state := mw.GetBreakerState("example.com")
	if state != CircuitClosed {
		t.Errorf("500 not in custom codes, should remain closed, got %v", state)
	}

	// 429 在列表中
	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 429, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	state = mw.GetBreakerState("example.com")
	if state != CircuitOpen {
		t.Errorf("429 in custom codes, should be open, got %v", state)
	}
}

func TestCircuitBreakerResetBreaker(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(2),
	)

	req := shttp.MustNewRequest("https://example.com/page")

	// 触发熔断
	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	// 手动重置
	mw.ResetBreaker("example.com")

	state := mw.GetBreakerState("example.com")
	if state != CircuitClosed {
		t.Errorf("expected CircuitClosed after reset, got %v", state)
	}

	fails := mw.GetBreakerConsecutiveFails("example.com")
	if fails != 0 {
		t.Errorf("expected 0 consecutive fails after reset, got %d", fails)
	}
}

func TestCircuitBreakerDownloadSlotMeta(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(2),
	)

	// 使用 download_slot meta 作为域名
	req := shttp.MustNewRequest("https://example.com/page")
	req.SetMeta("download_slot", "custom-slot")

	for i := 0; i < 2; i++ {
		resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))
		mw.ProcessResponse(context.Background(), req, resp)
	}

	// 应该使用 custom-slot 作为 key
	state := mw.GetBreakerState("custom-slot")
	if state != CircuitOpen {
		t.Errorf("expected CircuitOpen for custom-slot, got %v", state)
	}

	// example.com 应该仍然关闭
	state = mw.GetBreakerState("example.com")
	if state != CircuitClosed {
		t.Errorf("example.com should remain closed, got %v", state)
	}
}

func TestCircuitBreakerConcurrency(t *testing.T) {
	mw := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(100), // 高阈值避免触发
	)

	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			req := shttp.MustNewRequest("https://example.com/page")
			resp := shttp.MustNewResponse("https://example.com/page", 500, shttp.WithRequest(req))

			mw.ProcessRequest(context.Background(), req)
			mw.ProcessResponse(context.Background(), req, resp)
			mw.ProcessException(context.Background(), req, serrors.ErrDownloadTimeout)
		}(i)
	}

	for i := 0; i < 50; i++ {
		<-done
	}

	// 不应 panic，验证并发安全
	_ = mw.GetBreakerState("example.com")
	_ = mw.GetBreakerConsecutiveFails("example.com")
}

func TestCircuitBreakerStateString(t *testing.T) {
	tests := []struct {
		state    CircuitState
		expected string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

// ============================================================================
// 集成测试：RetryMiddleware + CircuitBreaker 协同
// ============================================================================

func TestRetryWithCircuitBreakerIntegration(t *testing.T) {
	// 模拟场景：重试中间件和熔断器协同工作
	// 重试中间件在前（优先级 550），熔断器在后（优先级 545）
	retryMW := NewRetryMiddleware(2, []int{500, 503}, -1, nil, nil,
		WithRetryBackoff(100*time.Millisecond, 5*time.Second, false),
	)
	cbMW := NewCircuitBreakerMiddleware(nil, nil,
		WithCircuitBreakerFailThreshold(3),
	)

	req := shttp.MustNewRequest("https://example.com/api")

	// 模拟 3 次连续 500 响应
	for i := 0; i < 3; i++ {
		resp := shttp.MustNewResponse("https://example.com/api", 500, shttp.WithRequest(req))

		// 熔断器记录失败
		cbMW.ProcessResponse(context.Background(), req, resp)

		// 重试中间件触发重试
		_, err := retryMW.ProcessResponse(context.Background(), req, resp)
		if !errors.Is(err, serrors.ErrNewRequest) {
			t.Fatalf("retry %d: expected ErrNewRequest, got %v", i, err)
		}
	}

	// 熔断器应该已打开
	state := cbMW.GetBreakerState("example.com")
	if state != CircuitOpen {
		t.Errorf("expected CircuitOpen after 3 failures, got %v", state)
	}

	// 后续请求应被熔断器拒绝
	_, err := cbMW.ProcessRequest(context.Background(), req)
	if !errors.Is(err, serrors.ErrIgnoreRequest) {
		t.Errorf("expected ErrIgnoreRequest from circuit breaker, got %v", err)
	}
}
