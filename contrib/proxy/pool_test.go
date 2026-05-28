package proxy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ----------------------------------------------------------------------------
// Proxy 数据结构测试
// ----------------------------------------------------------------------------

func TestProxy_StateTransition(t *testing.T) {
	t.Parallel()

	p := &Proxy{URL: "http://x", Weight: 1}
	if p.State() != StateHealthy {
		t.Errorf("default state should be Healthy, got %v", p.State())
	}

	p.SetState(StateDegraded)
	if p.State() != StateDegraded {
		t.Errorf("want Degraded, got %v", p.State())
	}

	p.SetState(StateUnhealthy)
	if p.State() != StateUnhealthy {
		t.Errorf("want Unhealthy, got %v", p.State())
	}
}

func TestProxy_StateString(t *testing.T) {
	t.Parallel()

	cases := map[State]string{
		StateHealthy:   "healthy",
		StateDegraded:  "degraded",
		StateUnhealthy: "unhealthy",
		State(99):      "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestProxy_FailureSuccessTracking(t *testing.T) {
	t.Parallel()

	p := &Proxy{URL: "http://x"}

	if p.markFailure() != 1 {
		t.Error("first failure should return 1")
	}
	if p.markFailure() != 2 {
		t.Error("second failure should return 2")
	}
	if p.Failures() != 2 {
		t.Errorf("Failures()=%d, want 2", p.Failures())
	}

	p.markSuccess()
	if p.Failures() != 0 {
		t.Errorf("Failures should reset to 0 after success, got %d", p.Failures())
	}
	if p.Successes() != 1 {
		t.Errorf("Successes()=%d, want 1", p.Successes())
	}
}

func TestProxy_UsageTimestamp(t *testing.T) {
	t.Parallel()

	p := &Proxy{URL: "http://x"}
	if !p.LastUsed().IsZero() {
		t.Error("LastUsed should be zero before any usage")
	}

	p.markUsed()
	if p.LastUsed().IsZero() {
		t.Error("LastUsed should be set after markUsed")
	}
	if p.TotalUsed() != 1 {
		t.Errorf("TotalUsed=%d, want 1", p.TotalUsed())
	}
}

func TestProxy_Snapshot(t *testing.T) {
	t.Parallel()

	p := &Proxy{URL: "http://x", Weight: 5}
	p.SetState(StateDegraded)
	p.markFailure()
	p.markUsed()

	snap := p.Snapshot()
	if snap.URL != "http://x" {
		t.Errorf("URL=%s", snap.URL)
	}
	if snap.State != StateDegraded {
		t.Errorf("State=%v", snap.State)
	}
	if snap.Weight != 5 {
		t.Errorf("Weight=%d", snap.Weight)
	}
	if snap.Failures != 1 {
		t.Errorf("Failures=%d", snap.Failures)
	}
	if snap.TotalUsed != 1 {
		t.Errorf("TotalUsed=%d", snap.TotalUsed)
	}
}

func TestProxy_ConcurrentAtomicAccess(t *testing.T) {
	t.Parallel()

	p := &Proxy{URL: "http://x"}
	const goroutines = 20
	const ops = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				p.markUsed()
				p.markFailure()
				p.markSuccess()
			}
		}()
	}
	wg.Wait()

	if p.TotalUsed() != int64(goroutines*ops) {
		t.Errorf("TotalUsed=%d, want %d", p.TotalUsed(), goroutines*ops)
	}
	if p.Successes() != int64(goroutines*ops) {
		t.Errorf("Successes=%d, want %d", p.Successes(), goroutines*ops)
	}
}

// ----------------------------------------------------------------------------
// parseProxyEntry 测试
// ----------------------------------------------------------------------------

func TestParseProxyEntry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw          string
		wantURL      string
		wantHasCreds bool
		wantWeight   int
		wantErr      bool
	}{
		{"http://proxy.example.com:8080", "http://proxy.example.com:8080", false, 1, false},
		{"http://user:pass@proxy.example.com:8080", "http://proxy.example.com:8080", true, 1, false},
		{"proxy.example.com:8080", "http://proxy.example.com:8080", false, 1, false},
		{"http://proxy.example.com:8080|5", "http://proxy.example.com:8080", false, 5, false},
		{"http://user:pass@proxy.example.com:8080|10", "http://proxy.example.com:8080", true, 10, false},
		{"", "", false, 0, true},
		{"   ", "", false, 0, true},
		{"http://", "", false, 0, true}, // 缺 host
	}

	for _, c := range cases {
		gotURL, gotCreds, gotWeight, gotErr := parseProxyEntry(c.raw)
		hasErr := gotErr != nil
		if hasErr != c.wantErr {
			t.Errorf("raw=%q: error=%v, wantErr=%v", c.raw, gotErr, c.wantErr)
			continue
		}
		if c.wantErr {
			continue
		}
		if gotURL != c.wantURL {
			t.Errorf("raw=%q: URL=%q, want %q", c.raw, gotURL, c.wantURL)
		}
		hasCreds := gotCreds != ""
		if hasCreds != c.wantHasCreds {
			t.Errorf("raw=%q: hasCreds=%v, want %v (creds=%q)", c.raw, hasCreds, c.wantHasCreds, gotCreds)
		}
		if gotWeight != c.wantWeight {
			t.Errorf("raw=%q: weight=%d, want %d", c.raw, gotWeight, c.wantWeight)
		}
	}
}

// ----------------------------------------------------------------------------
// Pool 测试
// ----------------------------------------------------------------------------

func newTestPool(t *testing.T, opts *Options, urls ...string) Pool {
	t.Helper()
	if opts == nil {
		opts = DefaultOptions()
		opts.HealthCheckEnabled = false // 测试中关闭后台健康检查避免干扰
	}
	provider := NewStaticProvider(urls)
	p, err := NewPool(opts, provider)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(func() {
		_ = p.Close()
	})
	return p
}

func TestPool_BasicGet(t *testing.T) {
	t.Parallel()

	p := newTestPool(t, nil,
		"http://p1.example.com:8080",
		"http://p2.example.com:8080",
	)

	if p.Size() != 2 {
		t.Errorf("Size=%d, want 2", p.Size())
	}
	if p.Healthy() != 2 {
		t.Errorf("Healthy=%d, want 2", p.Healthy())
	}

	got, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("got nil proxy")
	}
}

func TestPool_NoProxy_EmptyPool(t *testing.T) {
	t.Parallel()

	p := newTestPool(t, nil)
	_, err := p.Get(context.Background())
	if !errors.Is(err, ErrNoProxy) {
		t.Errorf("want ErrNoProxy, got %v", err)
	}
}

func TestPool_NoProxy_AllUnhealthy(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	opts.MaxFailures = 1
	provider := NewStaticProvider([]string{"http://p1:8080"})
	p, err := NewPool(opts, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// 标记唯一代理失败到不健康
	pr, _ := p.Get(context.Background())
	p.Mark(pr, false)

	if p.Healthy() != 0 {
		t.Errorf("Healthy=%d, want 0", p.Healthy())
	}

	if _, err := p.Get(context.Background()); !errors.Is(err, ErrNoProxy) {
		t.Errorf("want ErrNoProxy, got %v", err)
	}
}

func TestPool_MarkSuccess_RecoverFromDegraded(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	opts.MaxFailures = 4

	provider := NewStaticProvider([]string{"http://p1:8080"})
	p, _ := NewPool(opts, provider)
	defer p.Close()

	pr, _ := p.Get(context.Background())
	// 失败 3 次进入 Degraded（半数 + 1 = 3）
	p.Mark(pr, false)
	p.Mark(pr, false)
	p.Mark(pr, false)
	if pr.State() != StateDegraded {
		t.Fatalf("after 3 failures want Degraded, got %v", pr.State())
	}

	// 一次成功后恢复 Healthy
	p.Mark(pr, true)
	if pr.State() != StateHealthy {
		t.Errorf("after success want Healthy, got %v", pr.State())
	}
}

func TestPool_GetCtxCancel(t *testing.T) {
	t.Parallel()

	p := newTestPool(t, nil, "http://p1:8080")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.Get(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestPool_RoundRobin(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	opts.Strategy = StrategyRoundRobin
	provider := NewStaticProvider([]string{
		"http://p1:8080", "http://p2:8080", "http://p3:8080",
	})
	p, _ := NewPool(opts, provider)
	defer p.Close()

	seen := make(map[string]int)
	for i := 0; i < 9; i++ {
		got, err := p.Get(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		seen[got.URL]++
	}
	for url, count := range seen {
		if count != 3 {
			t.Errorf("URL %s hit %d times, want 3 (RoundRobin)", url, count)
		}
	}
}

func TestPool_Snapshots(t *testing.T) {
	t.Parallel()

	p := newTestPool(t, nil, "http://p1:8080", "http://p2:8080")
	got := p.Snapshots()
	if len(got) != 2 {
		t.Errorf("snapshots len=%d, want 2", len(got))
	}
	for _, s := range got {
		if s.State != StateHealthy {
			t.Errorf("snapshot state=%v, want Healthy", s.State)
		}
	}
}

func TestPool_Refresh_IncrementalMerge(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false

	// 自定义 Provider：每次返回不同列表
	provider := &mutableProvider{}
	provider.set([]string{"http://p1:8080", "http://p2:8080"})

	p, err := NewPool(opts, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// 取出 p1，记录失败次数
	pr1, _ := p.Get(context.Background())
	p.Mark(pr1, false)
	p.Mark(pr1, false)
	if pr1.Failures() != 2 {
		t.Fatalf("want 2 failures, got %d", pr1.Failures())
	}

	// 刷新：保留 p1，移除 p2，新增 p3
	provider.set([]string{"http://p1:8080", "http://p3:8080"})
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	if p.Size() != 2 {
		t.Errorf("size=%d, want 2", p.Size())
	}

	// p1 的统计应当被保留（增量合并）
	for _, snap := range p.Snapshots() {
		if snap.URL == "http://p1:8080" && snap.Failures != 2 {
			t.Errorf("p1 failures should preserve, got %d", snap.Failures)
		}
	}
}

func TestPool_CloseIdempotent(t *testing.T) {
	t.Parallel()

	p := newTestPool(t, nil, "http://p1:8080")
	if err := p.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}

	// Close 后 Get 应返回 ErrPoolClosed
	if _, err := p.Get(context.Background()); !errors.Is(err, ErrPoolClosed) {
		t.Errorf("want ErrPoolClosed, got %v", err)
	}
}

func TestPool_NilProvider(t *testing.T) {
	t.Parallel()

	if _, err := NewPool(DefaultOptions(), nil); err == nil {
		t.Error("expected error for nil provider")
	}
}

func TestPool_InvalidOptions(t *testing.T) {
	t.Parallel()

	bad := DefaultOptions()
	bad.Strategy = "invalid"
	_, err := NewPool(bad, NewStaticProvider([]string{"http://x"}))
	if err == nil {
		t.Error("expected error for invalid Strategy")
	}
}

func TestPool_ConcurrentGet(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	opts.Strategy = StrategyRandom
	// 将 MaxFailures 调高，避免并发场景下连续失败导致代理被标记 Unhealthy。
	// 本测试关注并发安全（race），不关注失败累计语义。
	opts.MaxFailures = 1000
	provider := NewStaticProvider([]string{
		"http://p1:8080", "http://p2:8080", "http://p3:8080", "http://p4:8080",
	})
	p, _ := NewPool(opts, provider)
	defer p.Close()

	const goroutines = 50
	const ops = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errCh := make(chan error, goroutines*ops)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				pr, err := p.Get(context.Background())
				if err != nil {
					errCh <- err
					return
				}
				p.Mark(pr, j%2 == 0)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestPool_ProviderRefreshInterval(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	opts.ProviderRefreshInterval = 50 * time.Millisecond

	provider := &mutableProvider{}
	provider.set([]string{"http://p1:8080"})

	p, err := NewPool(opts, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if p.Size() != 1 {
		t.Fatalf("initial size=%d, want 1", p.Size())
	}

	provider.set([]string{"http://p1:8080", "http://p2:8080"})
	time.Sleep(150 * time.Millisecond)

	if p.Size() != 2 {
		t.Errorf("after refresh size=%d, want 2", p.Size())
	}
}

// mutableProvider 用于测试的可变 Provider。
type mutableProvider struct {
	mu   sync.Mutex
	urls []string
}

func (m *mutableProvider) set(urls []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.urls = append([]string(nil), urls...)
}

func (m *mutableProvider) Fetch(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.urls))
	copy(out, m.urls)
	return out, nil
}

func (m *mutableProvider) Name() string { return "mutable" }
