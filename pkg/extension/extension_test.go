package extension

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// CoreStats 测试
// ============================================================================

func TestCoreStatsExtension_Open(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewCoreStatsExtension(sc, sm, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	// 验证信号处理器已注册
	if !sm.HasHandlers(signal.SpiderOpened) {
		t.Error("expected SpiderOpened handler registered")
	}
	if !sm.HasHandlers(signal.SpiderClosed) {
		t.Error("expected SpiderClosed handler registered")
	}
	if !sm.HasHandlers(signal.ItemScraped) {
		t.Error("expected ItemScraped handler registered")
	}
	if !sm.HasHandlers(signal.ItemDropped) {
		t.Error("expected ItemDropped handler registered")
	}
	if !sm.HasHandlers(signal.ResponseReceived) {
		t.Error("expected ResponseReceived handler registered")
	}
}

func TestCoreStatsExtension_SpiderLifecycle(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewCoreStatsExtension(sc, sm, nil)

	ext.Open(context.Background())

	// 模拟 spider_opened
	sm.SendCatchLog(signal.SpiderOpened, map[string]any{"spider": "test"})

	startTime := sc.GetValue("start_time", nil)
	if startTime == nil {
		t.Fatal("expected start_time to be set")
	}
	if _, ok := startTime.(time.Time); !ok {
		t.Fatal("expected start_time to be time.Time")
	}

	// 模拟一些事件
	sm.SendCatchLog(signal.ItemScraped, map[string]any{"item": "test"})
	sm.SendCatchLog(signal.ItemScraped, map[string]any{"item": "test2"})
	sm.SendCatchLog(signal.ItemDropped, map[string]any{"item": "test3"})
	sm.SendCatchLog(signal.ResponseReceived, map[string]any{"response": "test"})

	if sc.GetValue("item_scraped_count", 0) != 2 {
		t.Errorf("expected item_scraped_count=2, got %v", sc.GetValue("item_scraped_count", 0))
	}
	if sc.GetValue("item_dropped_count", 0) != 1 {
		t.Errorf("expected item_dropped_count=1, got %v", sc.GetValue("item_dropped_count", 0))
	}
	if sc.GetValue("response_received_count", 0) != 1 {
		t.Errorf("expected response_received_count=1, got %v", sc.GetValue("response_received_count", 0))
	}

	// 模拟 spider_closed
	sm.SendCatchLog(signal.SpiderClosed, map[string]any{"reason": "finished"})

	finishTime := sc.GetValue("finish_time", nil)
	if finishTime == nil {
		t.Fatal("expected finish_time to be set")
	}

	elapsed := sc.GetValue("elapsed_time_seconds", nil)
	if elapsed == nil {
		t.Fatal("expected elapsed_time_seconds to be set")
	}

	reason := sc.GetValue("finish_reason", nil)
	if reason != "finished" {
		t.Errorf("expected finish_reason=finished, got %v", reason)
	}
}

func TestCoreStatsExtension_Close(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewCoreStatsExtension(sc, sm, nil)

	ext.Open(context.Background())

	// 验证处理器已注册
	if sm.HandlerCount(signal.SpiderOpened) != 1 {
		t.Errorf("expected 1 SpiderOpened handler, got %d", sm.HandlerCount(signal.SpiderOpened))
	}

	// 关闭扩展
	ext.Close(context.Background())

	// 验证处理器已注销
	if sm.HandlerCount(signal.SpiderOpened) != 0 {
		t.Errorf("expected 0 SpiderOpened handlers after close, got %d", sm.HandlerCount(signal.SpiderOpened))
	}
}

// ============================================================================
// CloseSpider 测试
// ============================================================================

func TestCloseSpiderExtension_NotConfigured(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewCloseSpiderExtension(0, 0, 0, 0, sm, sc, nil)

	err := ext.Open(context.Background())
	if err == nil {
		t.Fatal("expected ErrNotConfigured")
	}
	if !errors.Is(err, serrors.ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestCloseSpiderExtension_ItemCount(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewCloseSpiderExtension(0, 3, 0, 0, sm, sc, slog.Default())

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	// 模拟 3 个 item_scraped 事件
	sm.SendCatchLog(signal.ItemScraped, nil)
	sm.SendCatchLog(signal.ItemScraped, nil)

	// 第 3 个应该触发关闭
	sm.SendCatchLog(signal.ItemScraped, nil)

	if !ext.closing.Load() {
		t.Error("expected closing to be true after reaching itemcount")
	}

	ext.Close(context.Background())
}

func TestCloseSpiderExtension_PageCount(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewCloseSpiderExtension(0, 0, 2, 0, sm, sc, slog.Default())

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	sm.SendCatchLog(signal.ResponseReceived, nil)
	sm.SendCatchLog(signal.ResponseReceived, nil)

	if !ext.closing.Load() {
		t.Error("expected closing to be true after reaching pagecount")
	}

	ext.Close(context.Background())
}

func TestCloseSpiderExtension_ErrorCount(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewCloseSpiderExtension(0, 0, 0, 2, sm, sc, slog.Default())

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	sm.SendCatchLog(signal.SpiderError, nil)
	sm.SendCatchLog(signal.SpiderError, nil)

	if !ext.closing.Load() {
		t.Error("expected closing to be true after reaching errorcount")
	}

	ext.Close(context.Background())
}

func TestCloseSpiderExtension_Timeout(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	// 设置 0.1 秒超时
	ext := NewCloseSpiderExtension(0.1, 0, 0, 0, sm, sc, slog.Default())

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	// 触发 spider_opened 启动定时器
	sm.SendCatchLog(signal.SpiderOpened, nil)

	// 等待超时触发
	time.Sleep(300 * time.Millisecond)

	if !ext.closing.Load() {
		t.Error("expected closing to be true after timeout")
	}

	ext.Close(context.Background())
}

func TestCloseSpiderExtension_Close(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewCloseSpiderExtension(0, 5, 0, 0, sm, sc, nil)

	ext.Open(context.Background())

	handlersBefore := sm.HandlerCount(signal.ItemScraped)
	if handlersBefore == 0 {
		t.Error("expected ItemScraped handler registered")
	}

	ext.Close(context.Background())

	handlersAfter := sm.HandlerCount(signal.ItemScraped)
	if handlersAfter != 0 {
		t.Errorf("expected 0 ItemScraped handlers after close, got %d", handlersAfter)
	}
}

// ============================================================================
// LogStats 测试
// ============================================================================

func TestLogStatsExtension_NotConfigured(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewLogStatsExtension(0, sc, sm, nil)

	err := ext.Open(context.Background())
	if err == nil {
		t.Fatal("expected ErrNotConfigured")
	}
	if !errors.Is(err, serrors.ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestLogStatsExtension_Open(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewLogStatsExtension(60.0, sc, sm, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	if !sm.HasHandlers(signal.SpiderOpened) {
		t.Error("expected SpiderOpened handler registered")
	}
	if !sm.HasHandlers(signal.SpiderClosed) {
		t.Error("expected SpiderClosed handler registered")
	}

	ext.Close(context.Background())
}

func TestLogStatsExtension_CalculateFinalStats(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewLogStatsExtension(60.0, sc, sm, slog.Default())

	ext.Open(context.Background())

	// 设置统计数据
	startTime := time.Now().Add(-2 * time.Minute)
	sc.SetValue("start_time", startTime)
	sc.SetValue("finish_time", time.Now())
	sc.SetValue("elapsed_time_seconds", 120.0)
	sc.SetValue("response_received_count", 100)
	sc.SetValue("item_scraped_count", 50)

	ext.calculateFinalStats()

	rpm := sc.GetValue("responses_per_minute", nil)
	if rpm == nil {
		t.Fatal("expected responses_per_minute to be set")
	}

	ipm := sc.GetValue("items_per_minute", nil)
	if ipm == nil {
		t.Fatal("expected items_per_minute to be set")
	}

	// 100 pages / 2 min = 50 RPM
	if rpmVal, ok := rpm.(float64); ok {
		if rpmVal < 49 || rpmVal > 51 {
			t.Errorf("expected RPM ~50, got %f", rpmVal)
		}
	}

	// 50 items / 2 min = 25 IPM
	if ipmVal, ok := ipm.(float64); ok {
		if ipmVal < 24 || ipmVal > 26 {
			t.Errorf("expected IPM ~25, got %f", ipmVal)
		}
	}

	ext.Close(context.Background())
}

func TestLogStatsExtension_Close(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewLogStatsExtension(60.0, sc, sm, nil)

	ext.Open(context.Background())
	ext.Close(context.Background())

	if sm.HandlerCount(signal.SpiderOpened) != 0 {
		t.Error("expected handlers to be disconnected after close")
	}
}

// ============================================================================
// MemoryUsage 测试
// ============================================================================

func TestMemoryUsageExtension_NotConfigured(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewMemoryUsageExtension(false, 0, 0, 60.0, sc, sm, nil)

	err := ext.Open(context.Background())
	if err == nil {
		t.Fatal("expected ErrNotConfigured")
	}
	if !errors.Is(err, serrors.ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestMemoryUsageExtension_Open(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewMemoryUsageExtension(true, 0, 0, 60.0, sc, sm, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	if !sm.HasHandlers(signal.EngineStarted) {
		t.Error("expected EngineStarted handler registered")
	}
	if !sm.HasHandlers(signal.EngineStopped) {
		t.Error("expected EngineStopped handler registered")
	}

	ext.Close(context.Background())
}

func TestMemoryUsageExtension_EngineStarted(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewMemoryUsageExtension(true, 0, 0, 1.0, sc, sm, slog.Default())

	ext.Open(context.Background())

	// 触发 engine_started
	sm.SendCatchLog(signal.EngineStarted, nil)

	// 等待一小段时间让 goroutine 执行
	time.Sleep(100 * time.Millisecond)

	startupMem := sc.GetValue("memusage/startup", nil)
	if startupMem == nil {
		t.Fatal("expected memusage/startup to be set")
	}

	maxMem := sc.GetValue("memusage/max", nil)
	if maxMem == nil {
		t.Fatal("expected memusage/max to be set")
	}

	// 停止
	sm.SendCatchLog(signal.EngineStopped, nil)
	ext.Close(context.Background())
}

func TestMemoryUsageExtension_Warning(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	// 设置一个非常小的警告阈值（1MB），确保会触发
	ext := NewMemoryUsageExtension(true, 0, 1, 0.1, sc, sm, slog.Default())

	ext.Open(context.Background())
	sm.SendCatchLog(signal.EngineStarted, nil)

	// 等待检查执行
	time.Sleep(200 * time.Millisecond)

	warningReached := sc.GetValue("memusage/warning_reached", nil)
	if warningReached == nil {
		t.Fatal("expected memusage/warning_reached to be set")
	}
	if warningReached != 1 {
		t.Errorf("expected memusage/warning_reached=1, got %v", warningReached)
	}

	sm.SendCatchLog(signal.EngineStopped, nil)
	ext.Close(context.Background())
}

func TestMemoryUsageExtension_Close(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	ext := NewMemoryUsageExtension(true, 0, 0, 60.0, sc, sm, nil)

	ext.Open(context.Background())
	ext.Close(context.Background())

	if sm.HandlerCount(signal.EngineStarted) != 0 {
		t.Error("expected handlers to be disconnected after close")
	}
}

func TestGetMemoryUsage(t *testing.T) {
	mem := getMemoryUsage()
	if mem == 0 {
		t.Error("expected non-zero memory usage")
	}
}

// ============================================================================
// 辅助函数测试
// ============================================================================

func TestToInt(t *testing.T) {
	tests := []struct {
		input    any
		expected int
	}{
		{0, 0},
		{42, 42},
		{int64(100), 100},
		{float64(3.14), 3},
		{"invalid", 0},
		{nil, 0},
	}

	for _, tt := range tests {
		result := toInt(tt.input)
		if result != tt.expected {
			t.Errorf("toInt(%v) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input    any
		expected float64
	}{
		{0.0, 0.0},
		{3.14, 3.14},
		{42, 42.0},
		{int64(100), 100.0},
		{"invalid", 0.0},
		{nil, 0.0},
	}

	for _, tt := range tests {
		result := toFloat64(tt.input)
		if result != tt.expected {
			t.Errorf("toFloat64(%v) = %f, expected %f", tt.input, result, tt.expected)
		}
	}
}
