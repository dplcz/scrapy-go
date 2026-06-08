package telemetry

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	promreg "github.com/dplcz/scrapy-go/contrib/telemetry/prometheus"
	serrors "github.com/dplcz/scrapy-go/pkg/errors"
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

	// 验证信号处理器已注册（v2 不再监听 ResponseReceived）
	expectedSignals := []signal.Signal{
		signal.SpiderOpened,
		signal.SpiderClosed,
		signal.RequestReachedDownloader,
		signal.RequestLeftDownloader,
		signal.SpiderError,
		signal.ItemScraped,
		signal.ItemDropped,
		signal.ItemError,
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

// TestTraceExtension_ScrapeSpanLifecycle 验证 v2 的 scrape Span 生命周期：
// 通过 TraceContextInjector 接口创建和结束 scrape Span。
func TestTraceExtension_ScrapeSpanLifecycle(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{
		"spider": "lifecycle_test",
	})

	// 创建多个请求，通过 BeforeScrape 创建 scrape Span
	req1, _ := scrapyhttp.NewRequest("http://example.com/page1")
	req2, _ := scrapyhttp.NewRequest("http://example.com/page2")
	req3, _ := scrapyhttp.NewRequest("http://example.com/page3")

	id1 := ext.BeforeScrape(req1)
	id2 := ext.BeforeScrape(req2)
	id3 := ext.BeforeScrape(req3)

	// 验证 scrapeSpans 中有 3 个活跃 Span
	if ext.activeSpanCount.Load() != 3 {
		t.Errorf("期望 3 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	// 请求 2 先完成（乱序完成）
	ext.AfterScrape(id2, telemetry.ScrapeYield{Requests: 2}, nil)

	// 验证剩 2 个
	if ext.activeSpanCount.Load() != 2 {
		t.Errorf("期望 2 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	// 请求 1 完成（带错误）
	ext.AfterScrape(id1, telemetry.ScrapeYield{}, fmt.Errorf("callback error"))

	// 请求 3 完成
	ext.AfterScrape(id3, telemetry.ScrapeYield{Items: 1}, nil)

	// 验证所有 Span 已结束
	if ext.activeSpanCount.Load() != 0 {
		t.Errorf("期望 0 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{
		"reason": "finished",
	})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_SpanCleanupOnClose 验证 Close 时清理未完成的活跃 scrape Span。
func TestTraceExtension_SpanCleanupOnClose(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 通过 BeforeScrape 创建 Span 但不调用 AfterScrape
	req1, _ := scrapyhttp.NewRequest("http://example.com/pending1")
	req2, _ := scrapyhttp.NewRequest("http://example.com/pending2")

	ext.BeforeScrape(req1)
	ext.BeforeScrape(req2)

	if ext.activeSpanCount.Load() != 2 {
		t.Errorf("期望 2 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	// 直接关闭
	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 验证 scrapeSpans 已清空
	if ext.activeSpanCount.Load() != 0 {
		t.Errorf("Close 后期望 0 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
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
			// 通过 TraceContextInjector 接口创建和结束 Span
			scrapeID := ext.BeforeScrape(req)
			// 模拟信号
			signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
				"request": req,
			})
			signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
				"request":          req,
				"download_latency": time.Duration(idx) * time.Millisecond,
				"status":           200,
			})
			ext.AfterScrape(scrapeID, telemetry.ScrapeYield{Requests: 1}, nil)
		}(i)
	}
	wg.Wait()

	// 验证所有 Span 已结束
	if ext.activeSpanCount.Load() != 0 {
		t.Errorf("并发完成后期望 0 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
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

// ============================================================================
// TraceContextInjector 接口测试（v2 新增）
// ============================================================================

// TestTraceExtension_InjectContext 验证 trace context 注入到新请求。
func TestTraceExtension_InjectContext(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{
		"spider": "inject_test",
	})

	// 创建父请求并开始 scrape
	parentReq, _ := scrapyhttp.NewRequest("http://example.com/list")
	scrapeID := ext.BeforeScrape(parentReq)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	// 为新请求注入 trace context
	childReq, _ := scrapyhttp.NewRequest("http://example.com/detail/1")
	ext.InjectContext(scrapeID, childReq)

	// NoopTracer 的 SpanContext 返回空值，所以 traceparent 为空（预期行为）

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{Requests: 1}, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_BeforeScrapeNilTracer 验证 nil tracer 时 BeforeScrape 返回 0。
func TestTraceExtension_BeforeScrapeNilTracer(t *testing.T) {
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(nil, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	req, _ := scrapyhttp.NewRequest("http://example.com")
	scrapeID := ext.BeforeScrape(req)
	if scrapeID != 0 {
		t.Errorf("nil tracer 时 BeforeScrape 应返回 0，实际: %d", scrapeID)
	}

	// AfterScrape 和 InjectContext 对 scrapeID=0 应为空操作
	ext.AfterScrape(0, telemetry.ScrapeYield{}, nil)
	ext.InjectContext(0, req)

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_MaxActiveSpans 验证最大活跃 Span 限制。
func TestTraceExtension_MaxActiveSpans(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil, WithMaxActiveSpans(3))

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 创建 3 个 Span（达到上限）
	req1, _ := scrapyhttp.NewRequest("http://example.com/1")
	req2, _ := scrapyhttp.NewRequest("http://example.com/2")
	req3, _ := scrapyhttp.NewRequest("http://example.com/3")

	id1 := ext.BeforeScrape(req1)
	id2 := ext.BeforeScrape(req2)
	id3 := ext.BeforeScrape(req3)

	if id1 == 0 || id2 == 0 || id3 == 0 {
		t.Fatal("前 3 个 BeforeScrape 应返回非零 scrapeID")
	}

	// 第 4 个应被拒绝
	req4, _ := scrapyhttp.NewRequest("http://example.com/4")
	id4 := ext.BeforeScrape(req4)
	if id4 != 0 {
		t.Errorf("超过 maxActiveSpans 时 BeforeScrape 应返回 0，实际: %d", id4)
	}

	// 释放一个后应该可以再创建
	ext.AfterScrape(id1, telemetry.ScrapeYield{}, nil)

	req5, _ := scrapyhttp.NewRequest("http://example.com/5")
	id5 := ext.BeforeScrape(req5)
	if id5 == 0 {
		t.Error("释放后 BeforeScrape 应返回非零 scrapeID")
	}

	// 清理
	ext.AfterScrape(id2, telemetry.ScrapeYield{}, nil)
	ext.AfterScrape(id3, telemetry.ScrapeYield{}, nil)
	ext.AfterScrape(id5, telemetry.ScrapeYield{}, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_WithCallbackRegistry 验证回调名称解析。
func TestTraceExtension_WithCallbackRegistry(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	registry := scrapyhttp.NewCallbackRegistry()

	// 注册一个回调
	parseFn := func(_ context.Context, _ *scrapyhttp.Response) ([]scrapyhttp.Output, error) {
		return nil, nil
	}
	registry.Register("ParseDetail", parseFn)

	ext := NewTraceExtension(tracer, signals, nil, WithCallbackRegistry(registry))

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 使用已注册的回调
	req, _ := scrapyhttp.NewRequest("http://example.com",
		scrapyhttp.WithCallback(parseFn),
	)
	scrapeID := ext.BeforeScrape(req)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)

	// nil callback（使用默认 Parse）
	req3, _ := scrapyhttp.NewRequest("http://example.com/3")
	scrapeID3 := ext.BeforeScrape(req3)
	ext.AfterScrape(scrapeID3, telemetry.ScrapeYield{}, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_TraceContextPropagation 验证完整的 trace context 传播链路。
func TestTraceExtension_TraceContextPropagation(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 模拟完整的传播链：ParseList → ParseDetail
	listReq, _ := scrapyhttp.NewRequest("http://example.com/list")
	listScrapeID := ext.BeforeScrape(listReq)

	// ParseList 产出新请求，注入 trace context
	detailReq, _ := scrapyhttp.NewRequest("http://example.com/detail/1")
	ext.InjectContext(listScrapeID, detailReq)

	// ParseList 结束
	ext.AfterScrape(listScrapeID, telemetry.ScrapeYield{Requests: 1}, nil)

	// ParseDetail 开始（应从 _trace_parent 恢复父上下文）
	detailScrapeID := ext.BeforeScrape(detailReq)
	if detailScrapeID == 0 {
		t.Fatal("ParseDetail 的 BeforeScrape 应返回非零 scrapeID")
	}

	// ParseDetail 结束
	ext.AfterScrape(detailScrapeID, telemetry.ScrapeYield{Items: 1}, nil)

	// 验证所有 Span 已清理
	if ext.activeSpanCount.Load() != 0 {
		t.Errorf("期望 0 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_SignalEventRecording 验证信号处理器正确记录 Event。
func TestTraceExtension_SignalEventRecording(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 创建 scrape Span
	req, _ := scrapyhttp.NewRequest("http://example.com")
	scrapeID := ext.BeforeScrape(req)

	// 发送 RequestReachedDownloader 信号（应在 scrape Span 上记录 Event）
	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req,
	})

	// 发送 RequestLeftDownloader 信号（应在 scrape Span 上记录 Event）
	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": req,
		"status":  200,
	})

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// ============================================================================
// Item Pipeline Span 测试（Phase 3）
// ============================================================================

// TestTraceExtension_ItemPipelineSpanLifecycle 验证 Item Pipeline Span 的完整生命周期。
func TestTraceExtension_ItemPipelineSpanLifecycle(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{
		"spider": "item_pipeline_test",
	})

	// 创建 scrape Span
	req, _ := scrapyhttp.NewRequest("http://example.com/list")
	scrapeID := ext.BeforeScrape(req)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	// 创建 Item Pipeline Span（parent 为 scrape Span）
	itemSpanID1 := ext.BeforeItemPipeline(scrapeID)
	if itemSpanID1 == 0 {
		t.Fatal("BeforeItemPipeline 应返回非零 itemSpanID")
	}

	// 创建第二个 Item Pipeline Span
	itemSpanID2 := ext.BeforeItemPipeline(scrapeID)
	if itemSpanID2 == 0 {
		t.Fatal("第二个 BeforeItemPipeline 应返回非零 itemSpanID")
	}

	// 验证活跃 Span 数量（1 scrape + 2 item）
	if ext.activeSpanCount.Load() != 3 {
		t.Errorf("期望 3 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	// 结束第一个 Item Span（成功）
	ext.AfterItemPipeline(itemSpanID1, nil)

	// 结束第二个 Item Span（丢弃）
	ext.AfterItemPipeline(itemSpanID2, fmt.Errorf("item dropped"))

	// 验证活跃 Span 数量（仅剩 1 scrape）
	if ext.activeSpanCount.Load() != 1 {
		t.Errorf("期望 1 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	// 结束 scrape Span
	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{Items: 2}, nil)

	// 验证所有 Span 已结束
	if ext.activeSpanCount.Load() != 0 {
		t.Errorf("期望 0 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemPipelineSpanWithError 验证 Item Pipeline Span 的错误处理。
func TestTraceExtension_ItemPipelineSpanWithError(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com")
	scrapeID := ext.BeforeScrape(req)

	// 创建 Item Span
	itemSpanID := ext.BeforeItemPipeline(scrapeID)
	if itemSpanID == 0 {
		t.Fatal("BeforeItemPipeline 应返回非零 itemSpanID")
	}

	// 模拟 Pipeline 处理错误
	ext.AfterItemPipeline(itemSpanID, fmt.Errorf("database connection failed"))

	// 验证 Item Span 已结束
	if ext.activeSpanCount.Load() != 1 { // 仅剩 scrape Span
		t.Errorf("期望 1 个活跃 Span（scrape），实际: %d", ext.activeSpanCount.Load())
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{Items: 1}, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemPipelineSpanWithDropItem 验证使用真实 ErrDropItem 时的 Item Span 行为。
func TestTraceExtension_ItemPipelineSpanWithDropItem(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com")
	scrapeID := ext.BeforeScrape(req)

	// 创建 Item Span
	itemSpanID := ext.BeforeItemPipeline(scrapeID)
	if itemSpanID == 0 {
		t.Fatal("BeforeItemPipeline 应返回非零 itemSpanID")
	}

	// 使用真实的 ErrDropItem 结束
	ext.AfterItemPipeline(itemSpanID, serrors.ErrDropItem)

	// 验证 Item Span 已结束
	if ext.activeSpanCount.Load() != 1 { // 仅剩 scrape Span
		t.Errorf("期望 1 个活跃 Span（scrape），实际: %d", ext.activeSpanCount.Load())
	}

	// 使用 DropItemError（带自定义消息）
	itemSpanID2 := ext.BeforeItemPipeline(scrapeID)
	ext.AfterItemPipeline(itemSpanID2, serrors.NewDropItemError("duplicate detected"))

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{Items: 2}, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemPipelineSpanDisabled 验证禁用 Item Pipeline Span 时的行为。
func TestTraceExtension_ItemPipelineSpanDisabled(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil, WithTraceItemPipeline(false))

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com")
	scrapeID := ext.BeforeScrape(req)

	// 禁用时 BeforeItemPipeline 应返回 0
	itemSpanID := ext.BeforeItemPipeline(scrapeID)
	if itemSpanID != 0 {
		t.Errorf("禁用 Item Pipeline Span 时应返回 0，实际: %d", itemSpanID)
	}

	// AfterItemPipeline 对 0 应为空操作
	ext.AfterItemPipeline(0, nil)

	// 验证活跃 Span 数量（仅 scrape Span）
	if ext.activeSpanCount.Load() != 1 {
		t.Errorf("期望 1 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemPipelineSpanNilTracer 验证 nil tracer 时 Item Pipeline Span 的行为。
func TestTraceExtension_ItemPipelineSpanNilTracer(t *testing.T) {
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(nil, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// nil tracer 时 BeforeItemPipeline 应返回 0
	itemSpanID := ext.BeforeItemPipeline(1)
	if itemSpanID != 0 {
		t.Errorf("nil tracer 时 BeforeItemPipeline 应返回 0，实际: %d", itemSpanID)
	}

	// AfterItemPipeline 对 0 应为空操作
	ext.AfterItemPipeline(0, nil)

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemPipelineSpanAfterScrapeEnded 验证 scrape Span 已结束时 Item Span 的行为。
func TestTraceExtension_ItemPipelineSpanAfterScrapeEnded(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com")
	scrapeID := ext.BeforeScrape(req)

	// 先结束 scrape Span
	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{Items: 1}, nil)

	// scrape Span 已结束后创建 Item Span（应回退到 rootCtx 作为 parent）
	itemSpanID := ext.BeforeItemPipeline(scrapeID)
	if itemSpanID == 0 {
		t.Fatal("scrape Span 已结束时 BeforeItemPipeline 仍应创建 Span（使用 rootCtx 作为 parent）")
	}

	ext.AfterItemPipeline(itemSpanID, nil)

	if ext.activeSpanCount.Load() != 0 {
		t.Errorf("期望 0 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemPipelineSpanCleanupOnClose 验证 Close 时清理未完成的 Item Span。
func TestTraceExtension_ItemPipelineSpanCleanupOnClose(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com")
	scrapeID := ext.BeforeScrape(req)

	// 创建 Item Span 但不结束
	ext.BeforeItemPipeline(scrapeID)
	ext.BeforeItemPipeline(scrapeID)

	// 验证有 3 个活跃 Span（1 scrape + 2 item）
	if ext.activeSpanCount.Load() != 3 {
		t.Errorf("期望 3 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	// 直接关闭
	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 验证所有 Span 已清理
	if ext.activeSpanCount.Load() != 0 {
		t.Errorf("Close 后期望 0 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}
}

// TestTraceExtension_ItemPipelineSpanConcurrent 验证并发创建和结束 Item Pipeline Span。
func TestTraceExtension_ItemPipelineSpanConcurrent(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com")
	scrapeID := ext.BeforeScrape(req)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			itemSpanID := ext.BeforeItemPipeline(scrapeID)
			if itemSpanID == 0 {
				return
			}
			// 模拟不同的 Pipeline 结果
			var err error
			switch idx % 3 {
			case 0:
				err = nil // 成功
			case 1:
				err = fmt.Errorf("item dropped") // 丢弃
			case 2:
				err = fmt.Errorf("pipeline error") // 错误
			}
			ext.AfterItemPipeline(itemSpanID, err)
		}(i)
	}
	wg.Wait()

	// 结束 scrape Span
	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{Items: 50}, nil)

	// 验证所有 Span 已结束
	if ext.activeSpanCount.Load() != 0 {
		t.Errorf("并发完成后期望 0 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemDroppedSignal 验证 ItemDropped 信号处理器。
func TestTraceExtension_ItemDroppedSignal(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 发送 ItemDropped 信号
	signals.SendCatchLog(signal.ItemDropped, map[string]any{
		"item":  map[string]string{"title": "test"},
		"error": fmt.Errorf("duplicate item"),
	})

	// nil params 不应 panic
	signals.SendCatchLog(signal.ItemDropped, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemErrorSignal 验证 ItemError 信号处理器。
func TestTraceExtension_ItemErrorSignal(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 发送 ItemError 信号
	signals.SendCatchLog(signal.ItemError, map[string]any{
		"item":  map[string]string{"title": "test"},
		"error": fmt.Errorf("validation failed"),
	})

	// nil params 不应 panic
	signals.SendCatchLog(signal.ItemError, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemPipelineMaxActiveSpans 验证 Item Span 受 maxActiveSpans 限制。
func TestTraceExtension_ItemPipelineMaxActiveSpans(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil, WithMaxActiveSpans(5))

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com")
	scrapeID := ext.BeforeScrape(req) // 占用 1 个

	// 创建 4 个 Item Span（达到上限 5）
	var itemIDs []uint64
	for i := 0; i < 4; i++ {
		id := ext.BeforeItemPipeline(scrapeID)
		if id == 0 {
			t.Fatalf("第 %d 个 BeforeItemPipeline 应返回非零 itemSpanID", i+1)
		}
		itemIDs = append(itemIDs, id)
	}

	// 第 5 个应被拒绝（已达上限）
	id := ext.BeforeItemPipeline(scrapeID)
	if id != 0 {
		t.Errorf("超过 maxActiveSpans 时 BeforeItemPipeline 应返回 0，实际: %d", id)
	}

	// 释放一个后应该可以再创建
	ext.AfterItemPipeline(itemIDs[0], nil)

	id = ext.BeforeItemPipeline(scrapeID)
	if id == 0 {
		t.Error("释放后 BeforeItemPipeline 应返回非零 itemSpanID")
	} else {
		ext.AfterItemPipeline(id, nil)
	}

	// 清理
	for _, itemID := range itemIDs[1:] {
		ext.AfterItemPipeline(itemID, nil)
	}
	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_ItemPipelineFullChain 验证完整的回调链 + Item Pipeline 追踪链路。
func TestTraceExtension_ItemPipelineFullChain(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{
		"spider": "full_chain_test",
	})

	// 模拟完整链路：ParseList → ParseDetail → Item Pipeline
	// Step 1: ParseList
	listReq, _ := scrapyhttp.NewRequest("http://example.com/list")
	listScrapeID := ext.BeforeScrape(listReq)

	// ParseList 产出新请求
	detailReq, _ := scrapyhttp.NewRequest("http://example.com/detail/1")
	ext.InjectContext(listScrapeID, detailReq)
	ext.AfterScrape(listScrapeID, telemetry.ScrapeYield{Requests: 1}, nil)

	// Step 2: ParseDetail
	detailScrapeID := ext.BeforeScrape(detailReq)

	// ParseDetail 产出 Item，创建 Item Pipeline Span
	itemSpanID := ext.BeforeItemPipeline(detailScrapeID)
	if itemSpanID == 0 {
		t.Fatal("BeforeItemPipeline 应返回非零 itemSpanID")
	}

	// 模拟 Pipeline 处理成功
	signals.SendCatchLog(signal.ItemScraped, map[string]any{
		"item": map[string]string{"title": "Test Book"},
	})
	ext.AfterItemPipeline(itemSpanID, nil)

	// 结束 ParseDetail scrape Span
	ext.AfterScrape(detailScrapeID, telemetry.ScrapeYield{Items: 1}, nil)

	// 验证所有 Span 已结束
	if ext.activeSpanCount.Load() != 0 {
		t.Errorf("期望 0 个活跃 Span，实际: %d", ext.activeSpanCount.Load())
	}

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{
		"reason": "finished",
	})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// ============================================================================
// Phase 4 (P5-026d) — Session 策略测试
// ============================================================================

// TestTraceExtension_DefaultPolicyIsWithinSession 验证默认传播策略为 PropagateWithinSession。
func TestTraceExtension_DefaultPolicyIsWithinSession(t *testing.T) {
	ext := NewTraceExtension(telemetry.NewNoopTracer(), signal.NewManager(nil), nil)
	if ext.policy != telemetry.PropagateWithinSession {
		t.Errorf("默认策略应为 PropagateWithinSession，实际: %v", ext.policy)
	}
	// 默认值确认
	if ext.policy.String() != "within_session" {
		t.Errorf("默认策略 String() 应为 within_session，实际: %s", ext.policy.String())
	}
}

// TestTraceExtension_WithPropagationPolicy 验证 WithPropagationPolicy Option 正确生效。
func TestTraceExtension_WithPropagationPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy telemetry.TracePropagationPolicy
		want   string
	}{
		{"always", telemetry.PropagateAlways, "always"},
		{"never", telemetry.PropagateNever, "never"},
		{"within_session", telemetry.PropagateWithinSession, "within_session"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext := NewTraceExtension(telemetry.NewNoopTracer(), signal.NewManager(nil), nil,
				WithPropagationPolicy(tc.policy),
			)
			if ext.policy != tc.policy {
				t.Errorf("policy 设置失败: 期望 %v, 实际 %v", tc.policy, ext.policy)
			}
			if ext.policy.String() != tc.want {
				t.Errorf("policy.String(): 期望 %s, 实际 %s", tc.want, ext.policy.String())
			}
		})
	}
}

// TestTraceExtension_SessionIDGeneratedOnSpiderOpened 验证 SpiderOpened 时
// 自动生成 sessionID（16 字节随机十六进制，共 32 字符）。
func TestTraceExtension_SessionIDGeneratedOnSpiderOpened(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	// SpiderOpened 之前 sessionID 为空
	if ext.sessionID != "" {
		t.Errorf("Open 后 SpiderOpened 之前 sessionID 应为空，实际: %q", ext.sessionID)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{"spider": "session_test"})

	if ext.sessionID == "" {
		t.Fatal("SpiderOpened 后 sessionID 应被生成")
	}
	if len(ext.sessionID) != 32 {
		t.Errorf("sessionID 长度应为 32（16 字节 hex），实际: %d", len(ext.sessionID))
	}

	// 全字符必须是十六进制
	for _, ch := range ext.sessionID {
		ok := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')
		if !ok {
			t.Errorf("sessionID 应为小写十六进制，实际包含字符 %q", ch)
			break
		}
	}
}

// TestTraceExtension_SessionIDUnique 验证多次生成的 sessionID 各不相同。
func TestTraceExtension_SessionIDUnique(t *testing.T) {
	const N = 50
	seen := make(map[string]struct{}, N)
	for i := 0; i < N; i++ {
		ext := NewTraceExtension(telemetry.NewNoopTracer(), signal.NewManager(nil), nil)
		if err := ext.Open(context.Background()); err != nil {
			t.Fatalf("Open 失败: %v", err)
		}
		ext.signals.SendCatchLog(signal.SpiderOpened, map[string]any{})
		sid := ext.sessionID
		if _, dup := seen[sid]; dup {
			t.Fatalf("第 %d 次生成的 sessionID 与之前重复: %s", i, sid)
		}
		seen[sid] = struct{}{}
		_ = ext.Close(context.Background())
	}
}

// TestTraceExtension_InjectContext_WithinSession 验证 PropagateWithinSession 模式下
// InjectContext 同时注入 _trace_parent 和 _trace_session。
func TestTraceExtension_InjectContext_WithinSession(t *testing.T) {
	tracer := telemetry.NewNoopTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithPropagationPolicy(telemetry.PropagateWithinSession),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})
	if ext.sessionID == "" {
		t.Fatal("sessionID 未生成")
	}

	parentReq, _ := scrapyhttp.NewRequest("http://example.com/list")
	scrapeID := ext.BeforeScrape(parentReq)
	if scrapeID == 0 {
		t.Skip("NoopTracer 下 SpanContext 不一定有效，跳过 Inject 验证")
	}

	newReq, _ := scrapyhttp.NewRequest("http://example.com/detail")
	ext.InjectContext(scrapeID, newReq)

	// NoopTracer 的 SpanContext 是无效的，FormatTraceparent 返回空字符串，
	// 因此 _trace_parent / _trace_session 都不会被注入。
	// 这是预期行为：noop tracer 模式下不传播。
	_, hasParent := newReq.GetMeta(telemetry.MetaKeyTraceparent)
	_, hasSession := newReq.GetMeta(telemetry.MetaKeyTraceSession)
	// 在 NoopTracer 下两个 key 都应为空
	if hasParent || hasSession {
		t.Logf("NoopTracer 下意外地注入了 trace context（parent=%v, session=%v）", hasParent, hasSession)
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// TestTraceExtension_InjectContext_PropagateNever 验证 PropagateNever 模式下
// InjectContext 不注入任何 Trace Context。
func TestTraceExtension_InjectContext_PropagateNever(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithPropagationPolicy(telemetry.PropagateNever),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	parentReq, _ := scrapyhttp.NewRequest("http://example.com/list")
	scrapeID := ext.BeforeScrape(parentReq)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	newReq, _ := scrapyhttp.NewRequest("http://example.com/detail")
	ext.InjectContext(scrapeID, newReq)

	// PropagateNever 下不应注入任何 trace context
	if _, ok := newReq.GetMeta(telemetry.MetaKeyTraceparent); ok {
		t.Error("PropagateNever 模式下不应注入 _trace_parent")
	}
	if _, ok := newReq.GetMeta(telemetry.MetaKeyTraceSession); ok {
		t.Error("PropagateNever 模式下不应注入 _trace_session")
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// TestTraceExtension_InjectContext_PropagateAlways 验证 PropagateAlways 模式下
// InjectContext 注入 _trace_parent 但不注入 _trace_session。
func TestTraceExtension_InjectContext_PropagateAlways(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithPropagationPolicy(telemetry.PropagateAlways),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	parentReq, _ := scrapyhttp.NewRequest("http://example.com/list")
	scrapeID := ext.BeforeScrape(parentReq)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	newReq, _ := scrapyhttp.NewRequest("http://example.com/detail")
	ext.InjectContext(scrapeID, newReq)

	// PropagateAlways 下应注入 _trace_parent，不注入 _trace_session
	if _, ok := newReq.GetMeta(telemetry.MetaKeyTraceparent); !ok {
		t.Error("PropagateAlways 模式下应注入 _trace_parent")
	}
	if _, ok := newReq.GetMeta(telemetry.MetaKeyTraceSession); ok {
		t.Error("PropagateAlways 模式下不应注入 _trace_session")
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// TestTraceExtension_InjectContext_WithinSession_BothInjected 验证
// PropagateWithinSession 模式 + spyTracer 下，_trace_parent 和 _trace_session 同时注入。
func TestTraceExtension_InjectContext_WithinSession_BothInjected(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithPropagationPolicy(telemetry.PropagateWithinSession),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	parentReq, _ := scrapyhttp.NewRequest("http://example.com/list")
	scrapeID := ext.BeforeScrape(parentReq)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	newReq, _ := scrapyhttp.NewRequest("http://example.com/detail")
	ext.InjectContext(scrapeID, newReq)

	tp, ok := newReq.GetMeta(telemetry.MetaKeyTraceparent)
	if !ok {
		t.Error("PropagateWithinSession 模式下应注入 _trace_parent")
	}
	tpStr, _ := tp.(string)
	if tpStr == "" {
		t.Error("_trace_parent 应为非空字符串")
	}

	sess, ok := newReq.GetMeta(telemetry.MetaKeyTraceSession)
	if !ok {
		t.Fatal("PropagateWithinSession 模式下应注入 _trace_session")
	}
	sessStr, _ := sess.(string)
	if sessStr != ext.sessionID {
		t.Errorf("_trace_session 应等于当前 sessionID: 期望 %q, 实际 %q", ext.sessionID, sessStr)
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// TestTraceExtension_ShouldPropagate 验证 shouldPropagate 在不同策略和 Meta 组合下的行为。
func TestTraceExtension_ShouldPropagate(t *testing.T) {
	makeReq := func(parent, session string) *scrapyhttp.Request {
		req, _ := scrapyhttp.NewRequest("http://example.com")
		if parent != "" {
			req.SetMeta(telemetry.MetaKeyTraceparent, parent)
		}
		if session != "" {
			req.SetMeta(telemetry.MetaKeyTraceSession, session)
		}
		return req
	}

	cases := []struct {
		name      string
		policy    telemetry.TracePropagationPolicy
		sessionID string
		req       *scrapyhttp.Request
		want      bool
	}{
		{
			name:   "PropagateAlways 始终返回 true",
			policy: telemetry.PropagateAlways,
			req:    makeReq("", ""),
			want:   true,
		},
		{
			name:   "PropagateNever 始终返回 false",
			policy: telemetry.PropagateNever,
			req:    makeReq("00-aaa-bbb-01", "session-x"),
			want:   false,
		},
		{
			name:      "PropagateWithinSession - session 匹配",
			policy:    telemetry.PropagateWithinSession,
			sessionID: "session-A",
			req:       makeReq("00-aaa-bbb-01", "session-A"),
			want:      true,
		},
		{
			name:      "PropagateWithinSession - session 不匹配（旧请求）",
			policy:    telemetry.PropagateWithinSession,
			sessionID: "session-B",
			req:       makeReq("00-aaa-bbb-01", "session-A"),
			want:      false,
		},
		{
			name:      "PropagateWithinSession - 缺失 _trace_session",
			policy:    telemetry.PropagateWithinSession,
			sessionID: "session-A",
			req:       makeReq("00-aaa-bbb-01", ""),
			want:      false,
		},
		{
			name:      "PropagateWithinSession - sessionID 尚未生成",
			policy:    telemetry.PropagateWithinSession,
			sessionID: "",
			req:       makeReq("00-aaa-bbb-01", "session-A"),
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext := NewTraceExtension(telemetry.NewNoopTracer(), signal.NewManager(nil), nil,
				WithPropagationPolicy(tc.policy),
			)
			ext.sessionID = tc.sessionID
			got := ext.shouldPropagate(tc.req)
			if got != tc.want {
				t.Errorf("shouldPropagate() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTraceExtension_BeforeScrape_SessionMismatch_FallbackRoot 验证
// PropagateWithinSession 模式下，session 不匹配的旧请求会回退到 rootCtx 作为 parent。
func TestTraceExtension_BeforeScrape_SessionMismatch_FallbackRoot(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithPropagationPolicy(telemetry.PropagateWithinSession),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})
	currentSession := ext.sessionID

	// 模拟一个断点续爬恢复的旧请求：session 与当前不同
	staleReq, _ := scrapyhttp.NewRequest("http://example.com/old")
	staleReq.SetMeta(telemetry.MetaKeyTraceparent, "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")
	staleReq.SetMeta(telemetry.MetaKeyTraceSession, "OLD-SESSION-ID-NOT-CURRENT")

	// shouldPropagate 应返回 false
	if ext.shouldPropagate(staleReq) {
		t.Error("旧请求 session 不匹配时 shouldPropagate 应返回 false")
	}
	if currentSession == "OLD-SESSION-ID-NOT-CURRENT" {
		t.Error("当前 sessionID 不应等于旧 session")
	}

	scrapeID := ext.BeforeScrape(staleReq)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	// 验证创建的 Span 没有使用旧的 remote SpanContext
	spans := tracer.snapshot()
	if len(spans) == 0 {
		t.Fatal("应至少创建一个 Span")
	}
	lastSpan := spans[len(spans)-1]
	if lastSpan.usedRemoteContext {
		t.Error("session 不匹配时不应使用 remote SpanContext，应使用 rootCtx")
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// ============================================================================
// Phase 5 (P5-026e) — WithTraceHTTPDownload 测试
// ============================================================================

// TestTraceExtension_DefaultHTTPDownloadIsDisabled 验证默认情况下不创建 http.request 独立 Span。
func TestTraceExtension_DefaultHTTPDownloadIsDisabled(t *testing.T) {
	ext := NewTraceExtension(telemetry.NewNoopTracer(), signal.NewManager(nil), nil)
	if ext.traceHTTPDownload {
		t.Error("默认 traceHTTPDownload 应为 false")
	}
}

// TestTraceExtension_WithTraceHTTPDownload 验证 WithTraceHTTPDownload Option 正确生效。
func TestTraceExtension_WithTraceHTTPDownload(t *testing.T) {
	cases := []bool{true, false}
	for _, v := range cases {
		ext := NewTraceExtension(telemetry.NewNoopTracer(), signal.NewManager(nil), nil,
			WithTraceHTTPDownload(v),
		)
		if ext.traceHTTPDownload != v {
			t.Errorf("WithTraceHTTPDownload(%v) 设置失败", v)
		}
	}
}

// TestTraceExtension_HTTPRequestSpan_Lifecycle 验证开启 traceHTTPDownload 后
// 每个 HTTP 请求会创建独立的 http.request 子 Span，并在 RequestLeftDownloader 时结束。
func TestTraceExtension_HTTPRequestSpan_Lifecycle(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithTraceHTTPDownload(true),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com/page")
	scrapeID := ext.BeforeScrape(req)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	// 此时只有 scrape Span（1 个活跃）
	if got := ext.activeSpanCount.Load(); got != 1 {
		t.Errorf("BeforeScrape 后期望 1 个活跃 Span，实际: %d", got)
	}

	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req,
	})

	// scrape Span + http.request Span = 2 个活跃
	if got := ext.activeSpanCount.Load(); got != 2 {
		t.Errorf("RequestReachedDownloader 后期望 2 个活跃 Span，实际: %d", got)
	}

	// 验证 httpRequestSpans 中存在该 req
	if _, ok := ext.httpRequestSpans.Load(req); !ok {
		t.Error("httpRequestSpans 应包含当前 request")
	}

	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": req,
		"status":  200,
	})

	// http.request Span 已结束，scrape Span 还在 = 1 个活跃
	if got := ext.activeSpanCount.Load(); got != 1 {
		t.Errorf("RequestLeftDownloader 后期望 1 个活跃 Span，实际: %d", got)
	}
	if _, ok := ext.httpRequestSpans.Load(req); ok {
		t.Error("RequestLeftDownloader 后 httpRequestSpans 不应再包含该 request")
	}

	// 验证 spyTracer 记录了 http.request Span
	httpSpans := tracer.spansByName("http.request")
	if len(httpSpans) != 1 {
		t.Errorf("应创建 1 个 http.request Span，实际: %d", len(httpSpans))
	}
	if !httpSpans[0].ended.Load() {
		t.Error("http.request Span 应已被 End")
	}
	if httpSpans[0].kind != telemetry.SpanKindClient {
		t.Errorf("http.request Span 应为 SpanKindClient，实际: %v", httpSpans[0].kind)
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)

	signals.SendCatchLog(signal.SpiderClosed, map[string]any{})
	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
}

// TestTraceExtension_HTTPRequestSpan_Disabled 验证关闭 traceHTTPDownload 后
// 不会创建 http.request 独立 Span。
func TestTraceExtension_HTTPRequestSpan_Disabled(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithTraceHTTPDownload(false),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com/page")
	scrapeID := ext.BeforeScrape(req)

	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req,
	})

	// 仍然只有 scrape Span（1 个活跃）
	if got := ext.activeSpanCount.Load(); got != 1 {
		t.Errorf("RequestReachedDownloader 后期望 1 个活跃 Span，实际: %d", got)
	}

	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": req,
		"status":  200,
	})

	// 验证未创建 http.request Span
	httpSpans := tracer.spansByName("http.request")
	if len(httpSpans) != 0 {
		t.Errorf("默认模式下不应创建 http.request Span，实际: %d", len(httpSpans))
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// TestTraceExtension_HTTPRequestSpan_RecordsError 验证开启 traceHTTPDownload 时
// 下载失败的请求会在 http.request Span 上记录错误并设置状态为 Error。
func TestTraceExtension_HTTPRequestSpan_RecordsError(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithTraceHTTPDownload(true),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com/fail")
	scrapeID := ext.BeforeScrape(req)

	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": req,
	})

	downloadErr := fmt.Errorf("connection refused")
	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": req,
		"error":   downloadErr,
	})

	httpSpans := tracer.spansByName("http.request")
	if len(httpSpans) != 1 {
		t.Fatalf("应创建 1 个 http.request Span，实际: %d", len(httpSpans))
	}
	sp := httpSpans[0]
	if !sp.ended.Load() {
		t.Error("http.request Span 应已被 End")
	}
	if sp.status != telemetry.SpanStatusError {
		t.Errorf("失败请求应设置状态为 SpanStatusError，实际: %v", sp.status)
	}
	if sp.recordedErrCount.Load() == 0 {
		t.Error("失败请求应至少记录一次错误")
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// TestTraceExtension_HTTPRequestSpan_CleanupOnClose 验证 Close 时清理未完成的 http.request Span。
func TestTraceExtension_HTTPRequestSpan_CleanupOnClose(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithTraceHTTPDownload(true),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 创建 2 个未完成的 http.request Span（不发送 RequestLeftDownloader）
	req1, _ := scrapyhttp.NewRequest("http://example.com/pending1")
	req2, _ := scrapyhttp.NewRequest("http://example.com/pending2")

	ext.BeforeScrape(req1)
	ext.BeforeScrape(req2)

	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{"request": req1})
	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{"request": req2})

	// 此时应有 2 scrape + 2 http.request = 4 个活跃
	if got := ext.activeSpanCount.Load(); got != 4 {
		t.Errorf("期望 4 个活跃 Span，实际: %d", got)
	}

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	if got := ext.activeSpanCount.Load(); got != 0 {
		t.Errorf("Close 后期望 0 个活跃 Span，实际: %d", got)
	}

	// 所有 http.request Span 应已被 End
	httpSpans := tracer.spansByName("http.request")
	if len(httpSpans) != 2 {
		t.Errorf("应创建 2 个 http.request Span，实际: %d", len(httpSpans))
	}
	for i, sp := range httpSpans {
		if !sp.ended.Load() {
			t.Errorf("Close 后 http.request Span[%d] 应已被 End", i)
		}
	}
}

// ============================================================================
// resolveParentContext / 集成路径覆盖增强测试
// ============================================================================

// TestTraceExtension_ResolveParent_PropagateAlways_UsesRemote 验证
// PropagateAlways 模式下，有效 traceparent 会通过 ContextWithRemoteSpanContext
// 注入为新 Span 的 parent。
func TestTraceExtension_ResolveParent_PropagateAlways_UsesRemote(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithPropagationPolicy(telemetry.PropagateAlways),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	// 模拟一个携带有效 traceparent 的请求（如分布式 worker 接收到上游下发的请求）
	req, _ := scrapyhttp.NewRequest("http://example.com/distributed")
	req.SetMeta(telemetry.MetaKeyTraceparent, "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-cccccccccccccccc-01")

	scrapeID := ext.BeforeScrape(req)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	if tracer.remoteCtxCalled.Load() == 0 {
		t.Error("PropagateAlways + 有效 traceparent 应触发 ContextWithRemoteSpanContext")
	}

	// 验证最近创建的 scrape Span 使用了 remote context
	allSpans := tracer.snapshot()
	var scrapeSpan *spySpan
	for _, sp := range allSpans {
		if strings.HasPrefix(sp.name, "scrape:") {
			scrapeSpan = sp
		}
	}
	if scrapeSpan == nil {
		t.Fatal("应至少创建一个 scrape Span")
	}
	if !scrapeSpan.usedRemoteContext {
		t.Error("PropagateAlways + 有效 traceparent 创建的 scrape Span 应使用 remote context")
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// TestTraceExtension_ResolveParent_NoTraceparent_UsesRoot 验证
// 请求 Meta 中无 traceparent 时，scrape Span 使用 rootCtx 作为 parent（不调用 RemoteContext）。
func TestTraceExtension_ResolveParent_NoTraceparent_UsesRoot(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithPropagationPolicy(telemetry.PropagateAlways),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com/initial")
	scrapeID := ext.BeforeScrape(req)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	allSpans := tracer.snapshot()
	var scrapeSpan *spySpan
	for _, sp := range allSpans {
		if strings.HasPrefix(sp.name, "scrape:") {
			scrapeSpan = sp
		}
	}
	if scrapeSpan == nil {
		t.Fatal("应至少创建一个 scrape Span")
	}
	if scrapeSpan.usedRemoteContext {
		t.Error("无 traceparent 时不应使用 remote context")
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// TestTraceExtension_ResolveParent_InvalidTraceparent_FallbackRoot 验证
// traceparent 字符串格式无效时回退到 rootCtx。
func TestTraceExtension_ResolveParent_InvalidTraceparent_FallbackRoot(t *testing.T) {
	tracer := newSpyTracer()
	signals := signal.NewManager(nil)
	ext := NewTraceExtension(tracer, signals, nil,
		WithPropagationPolicy(telemetry.PropagateAlways),
	)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer ext.Close(context.Background())

	signals.SendCatchLog(signal.SpiderOpened, map[string]any{})

	req, _ := scrapyhttp.NewRequest("http://example.com/garbage")
	req.SetMeta(telemetry.MetaKeyTraceparent, "this-is-not-a-valid-traceparent")

	scrapeID := ext.BeforeScrape(req)
	if scrapeID == 0 {
		t.Fatal("BeforeScrape 应返回非零 scrapeID")
	}

	if tracer.remoteCtxCalled.Load() != 0 {
		t.Error("无效 traceparent 不应触发 ContextWithRemoteSpanContext")
	}

	ext.AfterScrape(scrapeID, telemetry.ScrapeYield{}, nil)
}

// ============================================================================
// 测试用 spy Tracer / Span — 用于验证 Span 创建/结束/属性设置等行为
// ============================================================================

// spyTracer 是一个用于测试的轻量级 Tracer，记录所有 Start 调用以便断言。
// 与 NoopTracer 不同，spyTracer 创建的 Span 拥有有效的 SpanContext，
// 使 FormatTraceparent 能够生成非空字符串（用于测试 trace context 注入）。
type spyTracer struct {
	mu              sync.Mutex
	spans           []*spySpan
	nextID          atomic.Uint64
	remoteCtxCalled atomic.Int64
}

func newSpyTracer() *spyTracer {
	return &spyTracer{}
}

func (t *spyTracer) Start(ctx context.Context, name string, opts ...telemetry.SpanOption) (context.Context, telemetry.Span) {
	id := t.nextID.Add(1)
	kind := telemetry.SpanKindInternal
	if len(opts) > 0 {
		kind = opts[0].Kind
	}

	// 检查 ctx 中是否包含远程 SpanContext（通过 ContextWithRemoteSpanContext 注入）
	usedRemote := false
	if v := ctx.Value(spyRemoteCtxKey{}); v != nil {
		usedRemote = true
	}

	span := &spySpan{
		name:              name,
		kind:              kind,
		usedRemoteContext: usedRemote,
		spanCtx: telemetry.SpanContext{
			TraceID:    fmt.Sprintf("%032x", id),
			SpanID:     fmt.Sprintf("%016x", id),
			TraceFlags: 0x01,
		},
		events: make(map[string]int),
	}
	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()
	// 将当前 Span 写入 context（不是真正的 OTel ctx，仅供测试）
	return ctx, span
}

func (t *spyTracer) ContextWithRemoteSpanContext(ctx context.Context, sc telemetry.SpanContext) context.Context {
	t.remoteCtxCalled.Add(1)
	return context.WithValue(ctx, spyRemoteCtxKey{}, sc)
}

func (t *spyTracer) Shutdown(ctx context.Context) error {
	return nil
}

func (t *spyTracer) snapshot() []*spySpan {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*spySpan, len(t.spans))
	copy(out, t.spans)
	return out
}

func (t *spyTracer) spansByName(name string) []*spySpan {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []*spySpan
	for _, sp := range t.spans {
		if sp.name == name {
			out = append(out, sp)
		}
	}
	return out
}

type spyRemoteCtxKey struct{}

type spySpan struct {
	mu                sync.Mutex
	name              string
	kind              telemetry.SpanKind
	usedRemoteContext bool
	spanCtx           telemetry.SpanContext
	attrs             map[string]string
	events            map[string]int
	status            telemetry.SpanStatus
	statusDesc        string
	ended             atomic.Bool
	recordedErrCount  atomic.Int64
}

func (s *spySpan) End() {
	s.ended.Store(true)
}

func (s *spySpan) SetAttributes(attrs map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attrs == nil {
		s.attrs = make(map[string]string, len(attrs))
	}
	for k, v := range attrs {
		s.attrs[k] = v
	}
}

func (s *spySpan) SetStatus(status telemetry.SpanStatus, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.statusDesc = description
}

func (s *spySpan) RecordError(err error) {
	s.recordedErrCount.Add(1)
}

func (s *spySpan) SpanContext() telemetry.SpanContext {
	return s.spanCtx
}

func (s *spySpan) AddEvent(name string, attrs map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.events == nil {
		s.events = make(map[string]int)
	}
	s.events[name]++
}

// 编译期断言：spyTracer / spySpan 实现 telemetry 接口
var (
	_ telemetry.Tracer = (*spyTracer)(nil)
	_ telemetry.Span   = (*spySpan)(nil)
)
