package ratelimit

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/redis/go-redis/v9"
)

// ============================================================================
// 辅助函数
// ============================================================================

// setupMiniredis 创建一个 miniredis 实例用于测试。
func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *Options) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	return mr, opts
}

// ============================================================================
// RedisSlidingWindowLimiter 单元测试
// ============================================================================

func TestNewRedisSlidingWindowLimiter(t *testing.T) {
	_, opts := setupMiniredis(t)

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	if limiter.client == nil {
		t.Error("expected client to be non-nil")
	}
	if !limiter.ownsClient {
		t.Error("expected ownsClient to be true")
	}
}

func TestNewRedisSlidingWindowLimiter_ConnectionError(t *testing.T) {
	opts := DefaultOptions()
	opts.Addr = "localhost:1" // 不可达的地址
	opts.DialTimeout = 100 * time.Millisecond

	_, err := NewRedisSlidingWindowLimiter(opts)
	if err == nil {
		t.Fatal("expected error for unreachable Redis")
	}
}

func TestNewRedisSlidingWindowLimiterFromClient(t *testing.T) {
	mr, _ := setupMiniredis(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	opts := DefaultOptions()
	limiter, err := NewRedisSlidingWindowLimiterFromClient(client, opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiterFromClient() error = %v", err)
	}
	defer limiter.Close()

	if limiter.ownsClient {
		t.Error("expected ownsClient to be false")
	}
}

func TestNewRedisSlidingWindowLimiterFromClient_NilClient(t *testing.T) {
	_, err := NewRedisSlidingWindowLimiterFromClient(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestAllow_UnderLimit(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 5
	opts.DefaultBurst = 5
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	// 5 个请求应该全部通过
	for i := 0; i < 5; i++ {
		if !limiter.Allow("example.com") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestAllow_OverLimit(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 3
	opts.DefaultBurst = 3
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	// 前 3 个请求应该通过
	for i := 0; i < 3; i++ {
		if !limiter.Allow("example.com") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 第 4 个请求应该被拒绝
	if limiter.Allow("example.com") {
		t.Error("request 4 should be rejected")
	}
}

func TestAllow_DifferentDomains(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 2
	opts.DefaultBurst = 2
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	// 域名 A 用完配额
	limiter.Allow("a.com")
	limiter.Allow("a.com")
	if limiter.Allow("a.com") {
		t.Error("a.com should be rate limited")
	}

	// 域名 B 应该不受影响
	if !limiter.Allow("b.com") {
		t.Error("b.com should not be rate limited")
	}
}

func TestAllow_DomainRates(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 10
	opts.DefaultBurst = 10
	opts.DomainRates = map[string]int{
		"slow.com": 2,
	}
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	// slow.com 只允许 2 个请求
	limiter.Allow("slow.com")
	limiter.Allow("slow.com")
	if limiter.Allow("slow.com") {
		t.Error("slow.com should be rate limited at 2 req/s")
	}

	// 默认域名允许 10 个请求
	for i := 0; i < 10; i++ {
		if !limiter.Allow("fast.com") {
			t.Errorf("fast.com request %d should be allowed", i+1)
		}
	}
}

func TestAllow_ClosedLimiter(t *testing.T) {
	_, opts := setupMiniredis(t)

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}

	limiter.Close()

	// 关闭后应该降级为允许
	if !limiter.Allow("example.com") {
		t.Error("closed limiter should allow all requests (degradation)")
	}
}

func TestWait_UnderLimit(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 10
	opts.DefaultBurst = 10
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	ctx := context.Background()
	start := time.Now()
	err = limiter.Wait(ctx, "example.com")
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Wait() error = %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("Wait() should return immediately, took %v", elapsed)
	}
}

func TestWait_ContextCanceled(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 1
	opts.DefaultBurst = 1
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	// 用完配额
	limiter.Allow("example.com")

	// 使用带超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = limiter.Wait(ctx, "example.com")
	if err == nil {
		t.Error("Wait() should return error when context is canceled")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("Wait() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestWait_ClosedLimiter(t *testing.T) {
	_, opts := setupMiniredis(t)

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}

	limiter.Close()

	err = limiter.Wait(context.Background(), "example.com")
	if err != nil {
		t.Errorf("closed limiter Wait() should return nil (degradation), got %v", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	_, opts := setupMiniredis(t)

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}

	// 多次关闭不应 panic
	if err := limiter.Close(); err != nil {
		t.Errorf("first Close() error = %v", err)
	}
	if err := limiter.Close(); err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}

func TestClose_SharedClient(t *testing.T) {
	mr, _ := setupMiniredis(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	opts := DefaultOptions()
	limiter, err := NewRedisSlidingWindowLimiterFromClient(client, opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiterFromClient() error = %v", err)
	}

	// 关闭限速器不应关闭共享的 client
	limiter.Close()

	// client 应该仍然可用
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Errorf("shared client should still be usable after limiter close, got %v", err)
	}
}

func TestStats(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 10
	opts.DefaultBurst = 10
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	// 发送 3 个请求
	limiter.Allow("example.com")
	limiter.Allow("example.com")
	limiter.Allow("example.com")

	stats, err := limiter.Stats(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}

	count := stats["current_count"].(int64)
	if count != 3 {
		t.Errorf("Stats() current_count = %d, want 3", count)
	}

	remaining := stats["remaining"].(int64)
	if remaining != 7 {
		t.Errorf("Stats() remaining = %d, want 7", remaining)
	}
}

func TestReset(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 3
	opts.DefaultBurst = 3
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	// 用完配额
	limiter.Allow("example.com")
	limiter.Allow("example.com")
	limiter.Allow("example.com")

	if limiter.Allow("example.com") {
		t.Error("should be rate limited before reset")
	}

	// 重置
	if err := limiter.Reset(context.Background(), "example.com"); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}

	// 重置后应该可以继续请求
	if !limiter.Allow("example.com") {
		t.Error("should be allowed after reset")
	}
}

func TestConcurrentAccess(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 100
	opts.DefaultBurst = 100
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	var wg sync.WaitGroup
	var allowed atomic.Int64

	// 并发发送 200 个请求
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow("example.com") {
				allowed.Add(1)
			}
		}()
	}

	wg.Wait()

	// 应该有大约 100 个请求被允许（允许一定误差）
	count := allowed.Load()
	if count > 105 || count < 95 {
		t.Errorf("concurrent access: allowed %d requests, expected ~100", count)
	}
}

// ============================================================================
// RateLimitExtension 单元测试
// ============================================================================

func TestRateLimitExtension_Open(t *testing.T) {
	_, opts := setupMiniredis(t)

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}

	signals := signal.NewManager(slog.Default())
	ext := NewRateLimitExtension(limiter, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer ext.Close(context.Background())

	// 验证信号处理器已注册
	if !signals.HasHandlers(signal.RequestReachedDownloader) {
		t.Error("expected RequestReachedDownloader handler to be registered")
	}
}

func TestRateLimitExtension_Open_NilLimiter(t *testing.T) {
	signals := signal.NewManager(slog.Default())
	ext := NewRateLimitExtension(nil, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// 不应注册任何处理器
	if signals.HasHandlers(signal.RequestReachedDownloader) {
		t.Error("nil limiter should not register handlers")
	}
}

func TestRateLimitExtension_Close(t *testing.T) {
	_, opts := setupMiniredis(t)

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}

	signals := signal.NewManager(slog.Default())
	ext := NewRateLimitExtension(limiter, signals, nil)

	ext.Open(context.Background())

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// 验证信号处理器已注销
	if signals.HasHandlers(signal.RequestReachedDownloader) {
		t.Error("expected RequestReachedDownloader handler to be disconnected")
	}
}

func TestRateLimitExtension_SignalHandler(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 2
	opts.DefaultBurst = 2
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}

	signals := signal.NewManager(slog.Default())
	ext := NewRateLimitExtension(limiter, signals, nil)
	ext.Open(context.Background())
	defer ext.Close(context.Background())

	// 模拟发送 RequestReachedDownloader 信号
	params := map[string]any{
		"url":    "https://example.com/page1",
		"method": "GET",
	}

	// 前 2 个请求应该立即通过
	for i := 0; i < 2; i++ {
		errs := signals.Send(signal.RequestReachedDownloader, params)
		if len(errs) > 0 {
			t.Errorf("signal %d returned errors: %v", i+1, errs)
		}
	}
}

func TestRateLimitExtension_SignalHandler_InvalidURL(t *testing.T) {
	_, opts := setupMiniredis(t)

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}

	signals := signal.NewManager(slog.Default())
	ext := NewRateLimitExtension(limiter, signals, nil)
	ext.Open(context.Background())
	defer ext.Close(context.Background())

	// 空 URL 不应导致错误
	params := map[string]any{
		"url": "",
	}
	errs := signals.Send(signal.RequestReachedDownloader, params)
	if len(errs) > 0 {
		t.Errorf("empty URL should not cause errors: %v", errs)
	}

	// 缺少 URL 不应导致错误
	params2 := map[string]any{
		"method": "GET",
	}
	errs = signals.Send(signal.RequestReachedDownloader, params2)
	if len(errs) > 0 {
		t.Errorf("missing URL should not cause errors: %v", errs)
	}
}

// ============================================================================
// Options 单元测试
// ============================================================================

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.Addr != "localhost:6379" {
		t.Errorf("Addr = %s, want localhost:6379", opts.Addr)
	}
	if opts.DefaultRate != 10 {
		t.Errorf("DefaultRate = %d, want 10", opts.DefaultRate)
	}
	if opts.DefaultBurst != 20 {
		t.Errorf("DefaultBurst = %d, want 20", opts.DefaultBurst)
	}
	if opts.Window != time.Second {
		t.Errorf("Window = %v, want 1s", opts.Window)
	}
	if opts.KeyPrefix != "scrapy-go:ratelimit" {
		t.Errorf("KeyPrefix = %s, want scrapy-go:ratelimit", opts.KeyPrefix)
	}
}

func TestOptions_RateForDomain(t *testing.T) {
	opts := DefaultOptions()
	opts.DefaultRate = 10
	opts.DomainRates = map[string]int{
		"slow.com": 2,
		"fast.com": 50,
	}

	if rate := opts.rateForDomain("slow.com"); rate != 2 {
		t.Errorf("rateForDomain(slow.com) = %d, want 2", rate)
	}
	if rate := opts.rateForDomain("fast.com"); rate != 50 {
		t.Errorf("rateForDomain(fast.com) = %d, want 50", rate)
	}
	if rate := opts.rateForDomain("other.com"); rate != 10 {
		t.Errorf("rateForDomain(other.com) = %d, want 10 (default)", rate)
	}
}

func TestOptions_KeyForDomain(t *testing.T) {
	opts := DefaultOptions()
	opts.KeyPrefix = "test:ratelimit"

	key := opts.keyForDomain("example.com")
	expected := "test:ratelimit:example.com"
	if key != expected {
		t.Errorf("keyForDomain() = %s, want %s", key, expected)
	}
}

// ============================================================================
// extractDomain 单元测试
// ============================================================================

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"simple URL", "https://example.com/path", "example.com"},
		{"URL with port", "https://example.com:8080/path", "example.com"},
		{"URL with subdomain", "https://www.example.com/path", "www.example.com"},
		{"HTTP URL", "http://api.example.com/v1/data", "api.example.com"},
		{"empty URL", "", ""},
		{"invalid URL", "://invalid", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDomain(tt.url)
			if result != tt.expected {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// 集成测试
// ============================================================================

func TestIntegration_RateLimitWithExtension(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 5
	opts.DefaultBurst = 5
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}

	signals := signal.NewManager(slog.Default())
	ext := NewRateLimitExtension(limiter, signals, nil)

	// 模拟 Spider 生命周期
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// 模拟 5 个请求（应该全部通过）
	for i := 0; i < 5; i++ {
		params := map[string]any{
			"url":    "https://example.com/page" + string(rune('0'+i)),
			"method": "GET",
		}
		errs := signals.Send(signal.RequestReachedDownloader, params)
		if len(errs) > 0 {
			t.Errorf("request %d: unexpected errors: %v", i+1, errs)
		}
	}

	// 关闭
	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestIntegration_MultiDomainRateLimit(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.DefaultRate = 3
	opts.DefaultBurst = 3
	opts.DomainRates = map[string]int{
		"slow.example.com": 1,
	}
	opts.Window = time.Second

	limiter, err := NewRedisSlidingWindowLimiter(opts)
	if err != nil {
		t.Fatalf("NewRedisSlidingWindowLimiter() error = %v", err)
	}
	defer limiter.Close()

	// slow.example.com 只允许 1 个请求
	if !limiter.Allow("slow.example.com") {
		t.Error("first request to slow.example.com should be allowed")
	}
	if limiter.Allow("slow.example.com") {
		t.Error("second request to slow.example.com should be rejected")
	}

	// 默认域名允许 3 个请求
	for i := 0; i < 3; i++ {
		if !limiter.Allow("normal.example.com") {
			t.Errorf("request %d to normal.example.com should be allowed", i+1)
		}
	}
	if limiter.Allow("normal.example.com") {
		t.Error("4th request to normal.example.com should be rejected")
	}
}
