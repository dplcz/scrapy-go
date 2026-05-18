package telemetry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	promreg "github.com/dplcz/scrapy-go/contrib/telemetry/prometheus"
	scrapyhttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/telemetry"
)

// ============================================================================
// TraceExtension 测试
// ============================================================================

func TestTraceExtension_OpenClose(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 验证信号处理器已注册
	expectedSignals := []signal.Signal{
		signal.SpiderOpened,
		signal.SpiderClosed,
		signal.RequestReachedDownloader,
		signal.RequestLeftDownloader,
		signal.ResponseReceived,
		signal.SpiderError,
		signal.ItemScraped,
	}
	for _, sig := range expectedSignals {
		if !signals.HasHandlers(sig) {
			t.Errorf("信号 %s 应有处理器", sig)
		}
	}

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 验证信号处理器已注销
	for _, sig := range expectedSignals {
		if signals.HasHandlers(sig) {
			t.Errorf("Close 后信号 %s 不应有处理器", sig)
		}
	}
}

func TestTraceExtension_NilTracer(t *testing.T) {
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(nil, signals, nil)

	// nil tracer 不应 panic
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestTraceExtension_SpiderLifecycle(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 模拟 Spider 生命周期
	signals.SendCatchLog(signal.SpiderOpened, map[string]any{
		"spider": "test_spider",
	})

	req, _ := scrapyhttp.NewRequest("http://example.com")

	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req,
	})

	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": req,
	})

	signals.SendCatchLog(signal.ResponseReceived, map[string]any{
		"status": 200,
		"url":    "http://example.com",
	})

	signals.SendCatchLog(signal.ItemScraped, map[string]any{
		"item": map[string]string{"title": "test"},
	})

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{
		"reason": "finished",
	})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestTraceExtension_SpiderError(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	signals.SendCatchLog(signal.SpiderError, map[string]any{
		"error": fmt.Errorf("test error"),
	})

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestTraceExtension_NilParams(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// nil params 不应 panic
	signals.SendCatchLog(signal.SpiderOpened, nil)
	signals.SendCatchLog(signal.RequestReachedDownloader, nil)
	signals.SendCatchLog(signal.RequestLeftDownloader, nil)
	signals.SendCatchLog(signal.ResponseReceived, nil)
	signals.SendCatchLog(signal.SpiderError, nil)
	signals.SendCatchLog(signal.ItemScraped, nil)
	signals.SendCatchLog(signal.SpiderClosed, nil)

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_SpanLifecycle 验证 Span 生命周期增强：
// 请求到达下载器时创建 Span，离开时结束 Span。
func TestTraceExtension_SpanLifecycle(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{
		"spider": "lifecycle_test",
	})

	// 创建多个请求，验证每个请求都有独立的 Span
	req1, _ := scrapyhttp.NewRequest("http://example.com/page1")
	req2, _ := scrapyhttp.NewRequest("http://example.com/page2")
	req3, _ := scrapyhttp.NewRequest("http://example.com/page3")

	// 请求 1 到达下载器
	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req1,
	})
	// 请求 2 到达下载器
	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req2,
	})
	// 请求 3 到达下载器
	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req3,
	})

	// 验证 activeSpans 中有 3 个活跃 Span
	count := 0
	ext.activeSpans.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 3 {
		t.Errorf("期望 3 个活跃 Span，实际: %d", count)
	}

	// 请求 2 先完成（乱序完成）
	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request":          req2,
		"download_latency": 100 * time.Millisecond,
		"status":           200,
	})

	// 验证 activeSpans 中剩 2 个
	count = 0
	ext.activeSpans.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 2 {
		t.Errorf("期望 2 个活跃 Span，实际: %d", count)
	}

	// 请求 1 完成（带错误状态码）
	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request":          req1,
		"download_latency": 200 * time.Millisecond,
		"status":           500,
	})

	// 请求 3 完成（带错误）
	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": req3,
		"error":   fmt.Errorf("connection timeout"),
	})

	// 验证所有 Span 已结束
	count = 0
	ext.activeSpans.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("期望 0 个活跃 Span，实际: %d", count)
	}

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{
		"reason": "finished",
	})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_SpanCleanupOnClose 验证 Close 时清理未完成的活跃 Span。
func TestTraceExtension_SpanCleanupOnClose(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 创建请求但不完成
	req1, _ := scrapyhttp.NewRequest("http://example.com/pending1")
	req2, _ := scrapyhttp.NewRequest("http://example.com/pending2")

	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req1,
	})
	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req2,
	})

	// 不发送 RequestLeftDownloader，直接关闭
	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 验证 activeSpans 已清空
	count := 0
	ext.activeSpans.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("Close 后期望 0 个活跃 Span，实际: %d", count)
	}
}

// TestTraceExtension_DuplicateRequestLeft 验证重复的 RequestLeftDownloader 不会 panic。
func TestTraceExtension_DuplicateRequestLeft(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com")

	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req,
	})

	// 第一次离开（正常）
	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": req,
	})

	// 第二次离开（重复，不应 panic）
	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": req,
	})

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_RequestLeftWithoutReached 验证未到达的请求离开不会 panic。
func TestTraceExtension_RequestLeftWithoutReached(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 直接发送 RequestLeftDownloader，没有对应的 Reached
	req, _ := scrapyhttp.NewRequest("http://example.com/orphan")
	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": req,
	})

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// ============================================================================
// MetricsExtension 测试
// ============================================================================

func TestMetricsExtension_OpenClose(t *testing.T) {
	registry := telemetry.NewNoopMetricsRegistry()
	signals := signal.NewManager(nil)
	ext := NewMetricsExtension(registry, "", signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 验证信号处理器已注册
	expectedSignals := []signal.Signal{
		signal.SpiderOpened,
		signal.SpiderClosed,
		signal.RequestReachedDownloader,
		signal.RequestLeftDownloader,
		signal.ResponseReceived,
		signal.ItemScraped,
		signal.ItemDropped,
		signal.SpiderError,
	}
	for _, sig := range expectedSignals {
		if !signals.HasHandlers(sig) {
			t.Errorf("信号 %s 应有处理器", sig)
		}
	}

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 验证信号处理器已注销
	for _, sig := range expectedSignals {
		if signals.HasHandlers(sig) {
			t.Errorf("Close 后信号 %s 不应有处理器", sig)
		}
	}
}

func TestMetricsExtension_NilRegistry(t *testing.T) {
	signals := signal.NewManager(nil)
	ext := NewMetricsExtension(nil, "", signals, nil)

	// nil registry 不应 panic
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestMetricsExtension_SpiderLifecycle(t *testing.T) {
	registry := telemetry.NewNoopMetricsRegistry()
	signals := signal.NewManager(nil)
	ext := NewMetricsExtension(registry, "", signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 模拟完整的 Spider 生命周期
	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	for i := 0; i < 10; i++ {
		signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{})
		signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
			"download_latency": 100 * time.Millisecond,
		})
		signals.SendCatchLog(signal.ResponseReceived, map[string]any{})
	}

	for i := 0; i < 5; i++ {
		signals.SendCatchLog(signal.ItemScraped, map[string]any{})
	}

	signals.SendCatchLog(signal.ItemDropped, map[string]any{})
	signals.SendCatchLog(signal.SpiderError, map[string]any{})

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestMetricsExtension_LatencyFloat64(t *testing.T) {
	registry := telemetry.NewNoopMetricsRegistry()
	signals := signal.NewManager(nil)
	ext := NewMetricsExtension(registry, "", signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 测试 float64 类型的延迟
	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"download_latency": 0.5,
	})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestMetricsExtension_NilParams(t *testing.T) {
	registry := telemetry.NewNoopMetricsRegistry()
	signals := signal.NewManager(nil)
	ext := NewMetricsExtension(registry, "", signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// nil params 不应 panic
	signals.SendCatchLog(signal.SpiderOpened, nil)
	signals.SendCatchLog(signal.RequestReachedDownloader, nil)
	signals.SendCatchLog(signal.RequestLeftDownloader, nil)
	signals.SendCatchLog(signal.ResponseReceived, nil)
	signals.SendCatchLog(signal.ItemScraped, nil)
	signals.SendCatchLog(signal.ItemDropped, nil)
	signals.SendCatchLog(signal.SpiderError, nil)
	signals.SendCatchLog(signal.SpiderClosed, nil)

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// ============================================================================
// MetricsExtension HTTP 端点测试
// ============================================================================

func TestMetricsExtension_HTTPEndpoint(t *testing.T) {
	registry := promreg.NewRegistry()
	signals := signal.NewManager(nil)

	// 使用随机端口
	ext := NewMetricsExtension(registry, "127.0.0.1:0", signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 等待 HTTP 服务器启动
	time.Sleep(50 * time.Millisecond)

	// 模拟一些指标数据
	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})
	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{})
	signals.SendCatchLog(signal.ResponseReceived, map[string]any{})
	signals.SendCatchLog(signal.ItemScraped, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestMetricsExtension_HTTPEndpointWithPrometheus(t *testing.T) {
	registry := promreg.NewRegistry()
	signals := signal.NewManager(slog.Default())

	// 使用随机端口
	ext := NewMetricsExtension(registry, "127.0.0.1:0", signals, slog.Default())

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 等待 HTTP 服务器启动
	time.Sleep(50 * time.Millisecond)

	// 模拟指标数据
	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})
	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{})
	signals.SendCatchLog(signal.ResponseReceived, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestMetricsExtension_HealthEndpoint(t *testing.T) {
	registry := promreg.NewRegistry()
	signals := signal.NewManager(nil)

	// 使用固定端口进行 HTTP 请求测试
	ext := NewMetricsExtension(registry, "127.0.0.1:19876", signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 等待 HTTP 服务器启动
	time.Sleep(100 * time.Millisecond)

	// 测试 /health 端点
	resp, err := http.Get("http://127.0.0.1:19876/health")
	if err != nil {
		t.Fatalf("GET /health 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("/health 状态码期望 200，实际: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("/health 响应体期望 'ok'，实际: %q", string(body))
	}

	// 测试 /metrics 端点
	resp2, err := http.Get("http://127.0.0.1:19876/metrics")
	if err != nil {
		t.Fatalf("GET /metrics 失败: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("/metrics 状态码期望 200，实际: %d", resp2.StatusCode)
	}

	metricsBody, _ := io.ReadAll(resp2.Body)
	metricsStr := string(metricsBody)

	// 验证 Prometheus 格式的指标输出
	expectedMetrics := []string{
		"scrapy_requests_total",
		"scrapy_responses_total",
		"scrapy_items_scraped_total",
		"scrapy_active_requests",
		"scrapy_spider_state",
	}
	for _, metric := range expectedMetrics {
		if !strings.Contains(metricsStr, metric) {
			t.Errorf("/metrics 输出应包含 %q", metric)
		}
	}

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestMetricsExtension_InvalidAddr(t *testing.T) {
	registry := promreg.NewRegistry()
	signals := signal.NewManager(nil)

	// 使用无效地址
	ext := NewMetricsExtension(registry, "invalid-addr-no-port", signals, nil)

	err := ext.Open(context.Background())
	if err == nil {
		// 如果没有报错，也需要正常关闭
		ext.Close(context.Background())
	}
	// 无效地址可能导致 Open 失败，这是预期行为
}

// ============================================================================
// 并发安全测试
// ============================================================================

func TestTraceExtension_ConcurrentSignals(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req, _ := scrapyhttp.NewRequest(fmt.Sprintf("http://example.com/page/%d", idx))
			signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
				"request": req,
			})
			signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
				"request":          req,
				"download_latency": time.Duration(idx) * time.Millisecond,
				"status":           200,
			})
			signals.SendCatchLog(signal.ResponseReceived, map[string]any{
				"status": 200,
				"url":    fmt.Sprintf("http://example.com/page/%d", idx),
			})
		}(i)
	}
	wg.Wait()

	// 验证所有 Span 已结束
	count := 0
	ext.activeSpans.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("并发完成后期望 0 个活跃 Span，实际: %d", count)
	}

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

func TestMetricsExtension_ConcurrentSignals(t *testing.T) {
	registry := telemetry.NewNoopMetricsRegistry()
	signals := signal.NewManager(nil)
	ext := NewMetricsExtension(registry, "", signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{})
			signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
				"download_latency": 50 * time.Millisecond,
			})
			signals.SendCatchLog(signal.ResponseReceived, map[string]any{})
			signals.SendCatchLog(signal.ItemScraped, map[string]any{})
		}()
	}
	wg.Wait()

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// ============================================================================
// Prometheus 集成测试
// ============================================================================

func TestMetricsExtension_PrometheusIntegration(t *testing.T) {
	registry := promreg.NewRegistry()
	signals := signal.NewManager(nil)
	ext := NewMetricsExtension(registry, "", signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 模拟完整的 Spider 生命周期
	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	for i := 0; i < 5; i++ {
		signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{})
		signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
			"download_latency": time.Duration(i*100) * time.Millisecond,
		})
		signals.SendCatchLog(signal.ResponseReceived, map[string]any{})
	}

	for i := 0; i < 3; i++ {
		signals.SendCatchLog(signal.ItemScraped, map[string]any{})
	}

	signals.SendCatchLog(signal.ItemDropped, map[string]any{})
	signals.SendCatchLog(signal.SpiderError, map[string]any{})

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	// 验证 Prometheus 指标
	mfs, err := registry.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather 失败: %v", err)
	}

	expectedMetrics := map[string]float64{
		"scrapy_requests_total":      5.0,
		"scrapy_responses_total":     5.0,
		"scrapy_items_scraped_total": 3.0,
		"scrapy_items_dropped_total": 1.0,
		"scrapy_errors_total":        1.0,
		"scrapy_spider_state":        0.0, // Spider 已关闭
	}

	for _, mf := range mfs {
		name := mf.GetName()
		if expected, ok := expectedMetrics[name]; ok {
			var actual float64
			metric := mf.GetMetric()[0]
			if metric.GetCounter() != nil {
				actual = metric.GetCounter().GetValue()
			} else if metric.GetGauge() != nil {
				actual = metric.GetGauge().GetValue()
			}
			if actual != expected {
				t.Errorf("指标 %s 期望 %f，实际: %f", name, expected, actual)
			}
		}
	}

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}
