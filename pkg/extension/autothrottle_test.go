package extension

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// AutoThrottle 测试
// ============================================================================

// mockDelayAdjuster 是测试用的延迟调整器。
type mockDelayAdjuster struct {
	mu      sync.Mutex
	delays  map[string]time.Duration
	callCnt int
}

func newMockDelayAdjuster() *mockDelayAdjuster {
	return &mockDelayAdjuster{
		delays: make(map[string]time.Duration),
	}
}

func (m *mockDelayAdjuster) AdjustDelay(slotKey string, delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delays[slotKey] = delay
	m.callCnt++
}

func (m *mockDelayAdjuster) getDelay(slotKey string) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.delays[slotKey]
}

func (m *mockDelayAdjuster) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCnt
}

// ============================================================================
// 构造函数测试
// ============================================================================

func TestNewAutoThrottleExtension(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)

	if ext == nil {
		t.Fatal("expected non-nil extension")
	}
	if ext.startDelay != 5*time.Second {
		t.Errorf("expected startDelay=5s, got %v", ext.startDelay)
	}
	if ext.maxDelay != 60*time.Second {
		t.Errorf("expected maxDelay=60s, got %v", ext.maxDelay)
	}
	if ext.targetConcurrency != 1.0 {
		t.Errorf("expected targetConcurrency=1.0, got %v", ext.targetConcurrency)
	}
}

func TestNewAutoThrottleExtension_DefaultValues(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	// 测试负值和零值参数的默认处理
	ext := NewAutoThrottleExtension(true, -1, -1, -1, false, nil, sm, sc, nil)

	if ext.startDelay != 5*time.Second {
		t.Errorf("expected default startDelay=5s, got %v", ext.startDelay)
	}
	if ext.maxDelay != 60*time.Second {
		t.Errorf("expected default maxDelay=60s, got %v", ext.maxDelay)
	}
	if ext.targetConcurrency != 1.0 {
		t.Errorf("expected default targetConcurrency=1.0, got %v", ext.targetConcurrency)
	}
}

func TestNewAutoThrottleExtension_MaxDelayLessThanStartDelay(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	// maxDelay < startDelay 时应被调整为 startDelay
	ext := NewAutoThrottleExtension(true, 10.0, 5.0, 1.0, false, nil, sm, sc, nil)

	if ext.maxDelay != ext.startDelay {
		t.Errorf("expected maxDelay >= startDelay, got maxDelay=%v startDelay=%v",
			ext.maxDelay, ext.startDelay)
	}
}

// ============================================================================
// Open/Close 生命周期测试
// ============================================================================

func TestAutoThrottleExtension_NotConfigured(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	ext := NewAutoThrottleExtension(false, 5.0, 60.0, 1.0, false, nil, sm, sc, nil)

	err := ext.Open(context.Background())
	if err == nil {
		t.Fatal("expected ErrNotConfigured")
	}
	if !errors.Is(err, serrors.ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestAutoThrottleExtension_Open(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	// 验证信号处理器已注册
	if !sm.HasHandlers(signal.ResponseDownloaded) {
		t.Error("expected ResponseDownloaded handler registered")
	}
	if !sm.HasHandlers(signal.SpiderOpened) {
		t.Error("expected SpiderOpened handler registered")
	}
}

func TestAutoThrottleExtension_Close(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)

	ext.Open(context.Background())

	// 验证处理器已注册
	if sm.HandlerCount(signal.ResponseDownloaded) != 1 {
		t.Errorf("expected 1 ResponseDownloaded handler, got %d",
			sm.HandlerCount(signal.ResponseDownloaded))
	}

	// 关闭扩展
	ext.Close(context.Background())

	// 验证处理器已注销
	if sm.HandlerCount(signal.ResponseDownloaded) != 0 {
		t.Errorf("expected 0 ResponseDownloaded handlers after close, got %d",
			sm.HandlerCount(signal.ResponseDownloaded))
	}
	if sm.HandlerCount(signal.SpiderOpened) != 0 {
		t.Errorf("expected 0 SpiderOpened handlers after close, got %d",
			sm.HandlerCount(signal.SpiderOpened))
	}
}

func TestAutoThrottleExtension_CloseUpdatesStats(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())

	// 模拟一些请求
	sm.SendCatchLog(signal.SpiderOpened, nil)
	sendResponseDownloaded(sm, "example.com", 100*time.Millisecond)

	ext.Close(context.Background())

	// 验证统计已更新
	reqCount := sc.GetValue("autothrottle/request_count", int64(0))
	if reqCount == int64(0) {
		t.Error("expected autothrottle/request_count > 0 after close")
	}
}

// ============================================================================
// SpiderOpened 信号测试
// ============================================================================

func TestAutoThrottleExtension_SpiderOpened(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())

	// 触发 spider_opened
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 验证统计项已初始化
	if sc.GetValue("autothrottle/request_count", nil) == nil {
		t.Error("expected autothrottle/request_count to be initialized")
	}
	if sc.GetValue("autothrottle/latency_avg", nil) == nil {
		t.Error("expected autothrottle/latency_avg to be initialized")
	}
	if sc.GetValue("autothrottle/delay_adjusted_count", nil) == nil {
		t.Error("expected autothrottle/delay_adjusted_count to be initialized")
	}

	ext.Close(context.Background())
}

// ============================================================================
// 延迟调整算法测试
// ============================================================================

func TestAutoThrottleExtension_AdjustDelay_FirstRequest(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 发送第一个响应（延迟 200ms）
	sendResponseDownloaded(sm, "example.com", 200*time.Millisecond)

	// 第一个请求：
	// latencyEWMA = 200ms（首次直接使用）
	// targetDelay = 200ms / 1.0 = 200ms
	// newDelay = (5000ms + 200ms) / 2 = 2600ms
	// minDelay = 5000ms * 0.2 = 1000ms
	// newDelay = clamp(2600ms, 1000ms, 60000ms) = 2600ms
	delay := adjuster.getDelay("example.com")
	if delay != 2600*time.Millisecond {
		t.Errorf("expected delay=2600ms, got %v", delay)
	}

	ext.Close(context.Background())
}

func TestAutoThrottleExtension_AdjustDelay_ConvergesToTarget(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	// targetConcurrency=2.0，延迟 100ms
	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 2.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 发送多个相同延迟的响应，观察延迟收敛
	for i := 0; i < 50; i++ {
		sendResponseDownloaded(sm, "example.com", 100*time.Millisecond)
	}

	// 经过多次迭代，延迟应收敛到 latency / targetConcurrency = 100ms / 2.0 = 50ms
	// 但受 minDelay = 5000ms * 0.2 = 1000ms 限制
	delay := adjuster.getDelay("example.com")
	minDelay := time.Duration(float64(5*time.Second) * minDelayFactor)
	if delay < minDelay {
		t.Errorf("delay should not go below minDelay=%v, got %v", minDelay, delay)
	}

	ext.Close(context.Background())
}

func TestAutoThrottleExtension_AdjustDelay_MaxDelayClamp(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	// 设置较小的 maxDelay
	ext := NewAutoThrottleExtension(true, 1.0, 3.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 发送高延迟响应
	sendResponseDownloaded(sm, "example.com", 10*time.Second)

	delay := adjuster.getDelay("example.com")
	if delay > 3*time.Second {
		t.Errorf("delay should not exceed maxDelay=3s, got %v", delay)
	}

	ext.Close(context.Background())
}

func TestAutoThrottleExtension_AdjustDelay_MinDelayClamp(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	// startDelay=2s, targetConcurrency=100（非常高的并发目标）
	ext := NewAutoThrottleExtension(true, 2.0, 60.0, 100.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 发送多个低延迟响应
	for i := 0; i < 50; i++ {
		sendResponseDownloaded(sm, "example.com", 10*time.Millisecond)
	}

	delay := adjuster.getDelay("example.com")
	minDelay := time.Duration(float64(2*time.Second) * minDelayFactor) // 400ms
	if delay < minDelay {
		t.Errorf("delay should not go below minDelay=%v, got %v", minDelay, delay)
	}

	ext.Close(context.Background())
}

func TestAutoThrottleExtension_AdjustDelay_MultipleSlots(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 1.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 不同域名不同延迟
	sendResponseDownloaded(sm, "fast.example.com", 50*time.Millisecond)
	sendResponseDownloaded(sm, "slow.example.com", 2*time.Second)

	// 验证两个 Slot 独立调整
	if ext.SlotCount() != 2 {
		t.Errorf("expected 2 slots, got %d", ext.SlotCount())
	}

	fastDelay := adjuster.getDelay("fast.example.com")
	slowDelay := adjuster.getDelay("slow.example.com")

	if fastDelay >= slowDelay {
		t.Errorf("fast slot delay (%v) should be less than slow slot delay (%v)",
			fastDelay, slowDelay)
	}

	ext.Close(context.Background())
}

func TestAutoThrottleExtension_AdjustDelay_HighLatencyIncreasesDelay(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 0.5, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 先发送低延迟请求让延迟稳定
	for i := 0; i < 10; i++ {
		sendResponseDownloaded(sm, "example.com", 100*time.Millisecond)
	}
	lowLatencyDelay := adjuster.getDelay("example.com")

	// 然后发送高延迟请求
	for i := 0; i < 10; i++ {
		sendResponseDownloaded(sm, "example.com", 5*time.Second)
	}
	highLatencyDelay := adjuster.getDelay("example.com")

	if highLatencyDelay <= lowLatencyDelay {
		t.Errorf("high latency should increase delay: low=%v high=%v",
			lowLatencyDelay, highLatencyDelay)
	}

	ext.Close(context.Background())
}

// ============================================================================
// EWMA 平滑测试
// ============================================================================

func TestAutoThrottleExtension_EWMA_Smoothing(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 1.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 发送第一个请求（100ms）
	sendResponseDownloaded(sm, "example.com", 100*time.Millisecond)
	ewma1 := ext.GetSlotLatencyEWMA("example.com")

	// 第一个请求 EWMA = 100ms
	if ewma1 != 100*time.Millisecond {
		t.Errorf("expected first EWMA=100ms, got %v", ewma1)
	}

	// 发送第二个请求（300ms）
	sendResponseDownloaded(sm, "example.com", 300*time.Millisecond)
	ewma2 := ext.GetSlotLatencyEWMA("example.com")

	// EWMA = 0.5 * 300ms + 0.5 * 100ms = 200ms
	expectedEWMA := time.Duration(0.5*float64(300*time.Millisecond) + 0.5*float64(100*time.Millisecond))
	if ewma2 != expectedEWMA {
		t.Errorf("expected EWMA=%v, got %v", expectedEWMA, ewma2)
	}

	ext.Close(context.Background())
}

// ============================================================================
// 信号参数边界测试
// ============================================================================

func TestAutoThrottleExtension_NilParams(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())

	// nil params 不应 panic
	sm.SendCatchLog(signal.ResponseDownloaded, nil)

	if adjuster.getCallCount() != 0 {
		t.Error("expected no delay adjustment for nil params")
	}

	ext.Close(context.Background())
}

func TestAutoThrottleExtension_MissingRequest(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())

	// 缺少 request
	sm.SendCatchLog(signal.ResponseDownloaded, map[string]any{
		"response": &shttp.Response{},
	})

	if adjuster.getCallCount() != 0 {
		t.Error("expected no delay adjustment for missing request")
	}

	ext.Close(context.Background())
}

func TestAutoThrottleExtension_MissingResponse(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())

	req := createTestRequest("http://example.com", 100*time.Millisecond)
	sm.SendCatchLog(signal.ResponseDownloaded, map[string]any{
		"request": req,
	})

	if adjuster.getCallCount() != 0 {
		t.Error("expected no delay adjustment for missing response")
	}

	ext.Close(context.Background())
}

func TestAutoThrottleExtension_ZeroLatency(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())

	// 没有 download_latency Meta 的请求
	req, _ := shttp.NewRequest("http://example.com")
	req.SetMeta("download_slot", "example.com")
	resp := &shttp.Response{}

	sm.SendCatchLog(signal.ResponseDownloaded, map[string]any{
		"request":  req,
		"response": resp,
	})

	if adjuster.getCallCount() != 0 {
		t.Error("expected no delay adjustment for zero latency")
	}

	ext.Close(context.Background())
}

// ============================================================================
// GetSlotKey 测试
// ============================================================================

func TestAutoThrottleExtension_GetSlotKey_FromMeta(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)

	req, _ := shttp.NewRequest("http://example.com")
	req.SetMeta("download_slot", "custom_slot")

	key := ext.getSlotKey(req)
	if key != "custom_slot" {
		t.Errorf("expected slot key 'custom_slot', got '%s'", key)
	}
}

func TestAutoThrottleExtension_GetSlotKey_FromURL(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)

	req, _ := shttp.NewRequest("http://example.com/path")

	key := ext.getSlotKey(req)
	if key != "example.com" {
		t.Errorf("expected slot key 'example.com', got '%s'", key)
	}
}

// ============================================================================
// GetResponseLatency 测试
// ============================================================================

func TestAutoThrottleExtension_GetResponseLatency_FromRequestMeta(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, nil, sm, sc, nil)

	req, _ := shttp.NewRequest("http://example.com")
	req.SetMeta("download_latency", 150*time.Millisecond)
	resp := &shttp.Response{}

	latency := ext.getResponseLatency(req, resp)
	if latency != 150*time.Millisecond {
		t.Errorf("expected latency=150ms, got %v", latency)
	}
}

func TestAutoThrottleExtension_GetResponseLatency_FromRequestMetaFloat64(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, nil, sm, sc, nil)

	req, _ := shttp.NewRequest("http://example.com")
	req.SetMeta("download_latency", 0.15) // 0.15 秒
	resp := &shttp.Response{}

	latency := ext.getResponseLatency(req, resp)
	expected := time.Duration(0.15 * float64(time.Second))
	if latency != expected {
		t.Errorf("expected latency=%v, got %v", expected, latency)
	}
}

func TestAutoThrottleExtension_GetResponseLatency_NoLatency(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, nil, sm, sc, nil)

	// 没有设置 download_latency 的请求
	req, _ := shttp.NewRequest("http://example.com")
	resp := &shttp.Response{}

	latency := ext.getResponseLatency(req, resp)
	if latency != 0 {
		t.Errorf("expected latency=0, got %v", latency)
	}
}

// ============================================================================
// Debug 模式测试
// ============================================================================

func TestAutoThrottleExtension_DebugMode(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	// 启用 debug 模式不应 panic
	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, true, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	sendResponseDownloaded(sm, "example.com", 100*time.Millisecond)

	// 验证正常工作（debug 模式只是多输出日志）
	if adjuster.getCallCount() != 1 {
		t.Errorf("expected 1 delay adjustment, got %d", adjuster.getCallCount())
	}

	ext.Close(context.Background())
}

// ============================================================================
// 统计数据测试
// ============================================================================

func TestAutoThrottleExtension_Stats(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 发送 3 个响应
	sendResponseDownloaded(sm, "example.com", 100*time.Millisecond)
	sendResponseDownloaded(sm, "example.com", 200*time.Millisecond)
	sendResponseDownloaded(sm, "example.com", 150*time.Millisecond)

	// 验证统计
	reqCount := sc.GetValue("autothrottle/request_count", int64(0))
	if reqCount != int64(3) {
		t.Errorf("expected request_count=3, got %v", reqCount)
	}

	latencyAvg := sc.GetValue("autothrottle/latency_avg", 0.0)
	if latencyAvg == 0.0 {
		t.Error("expected non-zero latency_avg")
	}

	ext.Close(context.Background())
}

// ============================================================================
// 并发安全测试
// ============================================================================

func TestAutoThrottleExtension_ConcurrentAccess(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	adjuster := newMockDelayAdjuster()

	ext := NewAutoThrottleExtension(true, 1.0, 60.0, 2.0, false, adjuster, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 并发发送响应
	var wg sync.WaitGroup
	domains := []string{"a.com", "b.com", "c.com", "d.com", "e.com"}

	for _, domain := range domains {
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(d string) {
				defer wg.Done()
				sendResponseDownloaded(sm, d, 100*time.Millisecond)
			}(domain)
		}
	}

	wg.Wait()

	// 验证所有域名都被跟踪
	if ext.SlotCount() != len(domains) {
		t.Errorf("expected %d slots, got %d", len(domains), ext.SlotCount())
	}

	// 验证请求计数
	ext.Close(context.Background())
	reqCount := sc.GetValue("autothrottle/request_count", int64(0))
	if reqCount != int64(100) {
		t.Errorf("expected request_count=100, got %v", reqCount)
	}
}

// ============================================================================
// GetSlotDelay / GetSlotLatencyEWMA 测试
// ============================================================================

func TestAutoThrottleExtension_GetSlotDelay_NonExistent(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, nil, sm, sc, nil)

	// 不存在的 Slot 返回 startDelay
	delay := ext.GetSlotDelay("nonexistent.com")
	if delay != 5*time.Second {
		t.Errorf("expected startDelay=5s for non-existent slot, got %v", delay)
	}
}

func TestAutoThrottleExtension_GetSlotLatencyEWMA_NonExistent(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, nil, sm, sc, nil)

	// 不存在的 Slot 返回 0
	ewma := ext.GetSlotLatencyEWMA("nonexistent.com")
	if ewma != 0 {
		t.Errorf("expected 0 for non-existent slot, got %v", ewma)
	}
}

// ============================================================================
// DelayAdjusterFunc 测试
// ============================================================================

func TestDelayAdjusterFunc(t *testing.T) {
	var called bool
	var gotKey string
	var gotDelay time.Duration

	fn := DelayAdjusterFunc(func(slotKey string, delay time.Duration) {
		called = true
		gotKey = slotKey
		gotDelay = delay
	})

	fn.AdjustDelay("test.com", 500*time.Millisecond)

	if !called {
		t.Error("expected function to be called")
	}
	if gotKey != "test.com" {
		t.Errorf("expected key='test.com', got '%s'", gotKey)
	}
	if gotDelay != 500*time.Millisecond {
		t.Errorf("expected delay=500ms, got %v", gotDelay)
	}
}

// ============================================================================
// NilDelayAdjuster 测试
// ============================================================================

func TestAutoThrottleExtension_NilDelayAdjuster(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	// nil adjuster 不应 panic
	ext := NewAutoThrottleExtension(true, 5.0, 60.0, 1.0, false, nil, sm, sc, nil)
	ext.Open(context.Background())
	sm.SendCatchLog(signal.SpiderOpened, nil)

	sendResponseDownloaded(sm, "example.com", 100*time.Millisecond)

	// 验证内部状态仍然正确更新
	if ext.SlotCount() != 1 {
		t.Errorf("expected 1 slot, got %d", ext.SlotCount())
	}

	ext.Close(context.Background())
}

// ============================================================================
// 辅助函数测试
// ============================================================================

func TestClampDuration(t *testing.T) {
	tests := []struct {
		name     string
		d        time.Duration
		min      time.Duration
		max      time.Duration
		expected time.Duration
	}{
		{"within range", 500 * time.Millisecond, 100 * time.Millisecond, 1 * time.Second, 500 * time.Millisecond},
		{"below min", 50 * time.Millisecond, 100 * time.Millisecond, 1 * time.Second, 100 * time.Millisecond},
		{"above max", 2 * time.Second, 100 * time.Millisecond, 1 * time.Second, 1 * time.Second},
		{"equal to min", 100 * time.Millisecond, 100 * time.Millisecond, 1 * time.Second, 100 * time.Millisecond},
		{"equal to max", 1 * time.Second, 100 * time.Millisecond, 1 * time.Second, 1 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clampDuration(tt.d, tt.min, tt.max)
			if result != tt.expected {
				t.Errorf("clampDuration(%v, %v, %v) = %v, expected %v",
					tt.d, tt.min, tt.max, result, tt.expected)
			}
		})
	}
}

func TestRoundDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected time.Duration
	}{
		{1500 * time.Microsecond, 2 * time.Millisecond},
		{1400 * time.Microsecond, 1 * time.Millisecond},
		{100 * time.Millisecond, 100 * time.Millisecond},
		{0, 0},
	}

	for _, tt := range tests {
		result := roundDuration(tt.input)
		if result != tt.expected {
			t.Errorf("roundDuration(%v) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

// ============================================================================
// Table-Driven 延迟调整测试
// ============================================================================

func TestAutoThrottleExtension_AdjustDelay_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		startDelay        float64
		maxDelay          float64
		targetConcurrency float64
		latency           time.Duration
		expectMinDelay    time.Duration
		expectMaxDelay    time.Duration
	}{
		{
			name:              "normal latency with concurrency 1",
			startDelay:        5.0,
			maxDelay:          60.0,
			targetConcurrency: 1.0,
			latency:           200 * time.Millisecond,
			expectMinDelay:    200 * time.Millisecond,
			expectMaxDelay:    5 * time.Second,
		},
		{
			name:              "high latency clamped to max",
			startDelay:        1.0,
			maxDelay:          2.0,
			targetConcurrency: 1.0,
			latency:           10 * time.Second,
			expectMinDelay:    1 * time.Second,
			expectMaxDelay:    2 * time.Second,
		},
		{
			name:              "low latency with high concurrency",
			startDelay:        2.0,
			maxDelay:          60.0,
			targetConcurrency: 10.0,
			latency:           50 * time.Millisecond,
			expectMinDelay:    time.Duration(float64(2*time.Second) * minDelayFactor),
			expectMaxDelay:    2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := signal.NewManager(nil)
			sc := stats.NewMemoryCollector(false, nil)
			adjuster := newMockDelayAdjuster()

			ext := NewAutoThrottleExtension(true, tt.startDelay, tt.maxDelay,
				tt.targetConcurrency, false, adjuster, sm, sc, nil)
			ext.Open(context.Background())
			sm.SendCatchLog(signal.SpiderOpened, nil)

			sendResponseDownloaded(sm, "test.com", tt.latency)

			delay := adjuster.getDelay("test.com")
			if delay < tt.expectMinDelay {
				t.Errorf("delay %v < expected min %v", delay, tt.expectMinDelay)
			}
			if delay > tt.expectMaxDelay {
				t.Errorf("delay %v > expected max %v", delay, tt.expectMaxDelay)
			}

			ext.Close(context.Background())
		})
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

// sendResponseDownloaded 模拟发送 ResponseDownloaded 信号。
func sendResponseDownloaded(sm *signal.Manager, domain string, latency time.Duration) {
	u, _ := url.Parse("http://" + domain + "/page")
	req, _ := shttp.NewRequest(u.String())
	req.SetMeta("download_slot", domain)
	req.SetMeta("download_latency", latency)

	resp := &shttp.Response{}

	sm.SendCatchLog(signal.ResponseDownloaded, map[string]any{
		"request":  req,
		"response": resp,
	})
}

// createTestRequest 创建一个带有 download_latency Meta 的测试请求。
func createTestRequest(rawURL string, latency time.Duration) *shttp.Request {
	req, _ := shttp.NewRequest(rawURL)
	req.SetMeta("download_latency", latency)
	return req
}
