package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthChecker_AllHealthy(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false // 手动控制 checker
	opts.HealthCheckURL = srv.URL
	opts.HealthCheckExpectedStatus = http.StatusNoContent

	p := newTestPool(t, opts, "http://p1:8080", "http://p2:8080").(*pool)

	// 用一个直接通过的 probe 替换默认（避免真的连接代理）
	checker := newHealthChecker(p, opts, slog.Default())
	checker.probe = func(_ context.Context, _ *Proxy, _ string) error {
		return nil
	}

	checker.checkAll(context.Background())

	if p.Healthy() != 2 {
		t.Errorf("Healthy=%d, want 2", p.Healthy())
	}
}

func TestHealthChecker_FailureMarksUnhealthy(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	opts.MaxFailures = 2

	p := newTestPool(t, opts, "http://p1:8080").(*pool)

	checker := newHealthChecker(p, opts, slog.Default())
	probeErr := errors.New("probe failed")
	checker.probe = func(_ context.Context, _ *Proxy, _ string) error {
		return probeErr
	}

	// 探测两次：第一次 Failures=1 → Degraded（失败半数+1），第二次 Failures=2 → Unhealthy
	checker.checkAll(context.Background())
	checker.checkAll(context.Background())

	if p.Healthy() != 0 {
		t.Errorf("Healthy=%d, want 0", p.Healthy())
	}
}

func TestHealthChecker_RecoveryFromUnhealthy(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	opts.MaxFailures = 1
	opts.RecoveryThreshold = 2

	p := newTestPool(t, opts, "http://p1:8080").(*pool)

	checker := newHealthChecker(p, opts, slog.Default())

	var shouldFail atomic.Bool
	shouldFail.Store(true)

	checker.probe = func(_ context.Context, _ *Proxy, _ string) error {
		if shouldFail.Load() {
			return errors.New("fail")
		}
		return nil
	}

	// 第一轮：失败 → Unhealthy
	checker.checkAll(context.Background())
	if p.Healthy() != 0 {
		t.Fatal("expected unhealthy after first failure")
	}

	// 切换为成功，需要 RecoveryThreshold=2 次成功才恢复
	shouldFail.Store(false)
	checker.checkAll(context.Background())
	if p.Healthy() != 0 {
		t.Errorf("after 1 success healthy=%d, want 0 (need 2)", p.Healthy())
	}
	checker.checkAll(context.Background())
	if p.Healthy() != 1 {
		t.Errorf("after 2 successes healthy=%d, want 1", p.Healthy())
	}
}

func TestHealthChecker_LastCheckedUpdated(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false

	p := newTestPool(t, opts, "http://p1:8080").(*pool)

	checker := newHealthChecker(p, opts, slog.Default())
	checker.probe = func(_ context.Context, _ *Proxy, _ string) error { return nil }

	before := time.Now().Unix()
	checker.checkAll(context.Background())
	after := time.Now().Unix()

	for _, pr := range p.proxies {
		ts := pr.LastChecked().Unix()
		if ts < before || ts > after {
			t.Errorf("LastChecked out of range: ts=%d, [%d, %d]", ts, before, after)
		}
	}
}

func TestHealthChecker_RunRespectsCtxCancel(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	opts.HealthCheckInterval = 10 * time.Millisecond
	opts.HealthCheckTimeout = 50 * time.Millisecond

	p := newTestPool(t, opts, "http://p1:8080").(*pool)

	checker := newHealthChecker(p, opts, slog.Default())
	// 替换 probe 为快速返回的实现，避免真实网络请求拖慢 ctx 取消响应
	checker.probe = func(_ context.Context, _ *Proxy, _ string) error { return nil }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		checker.run(ctx)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("checker.run did not return after ctx cancel")
	}
}

// ---------------------------------------------------------------------------
// 全链路集成测试：HealthCheckEnabled=true 通过实际的后台 goroutine
// ---------------------------------------------------------------------------

func TestPool_BackgroundHealthCheck(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = true
	opts.HealthCheckURL = srv.URL
	opts.HealthCheckInterval = 50 * time.Millisecond
	opts.HealthCheckTimeout = 200 * time.Millisecond

	provider := NewStaticProvider([]string{"http://nonexistent.invalid:9"})
	p, err := NewPool(opts, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// 等待健康检查执行至少一轮（实际 probe 会失败）
	time.Sleep(150 * time.Millisecond)

	// 这里不强制断言代理的最终状态（取决于 probe 是否完成），
	// 但 Close 应当正常返回，验证后台 goroutine 退出干净。
}
