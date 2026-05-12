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

	signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"url":    "http://example.com",
		"method": "GET",
	})

	signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{})

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
	signals.SendCatchLog(signal.ResponseReceived, nil)
	signals.SendCatchLog(signal.SpiderError, nil)
	signals.SendCatchLog(signal.ItemScraped, nil)
	signals.SendCatchLog(signal.SpiderClosed, nil)

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
			signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
				"url":    fmt.Sprintf("http://example.com/page/%d", idx),
				"method": "GET",
			})
			signals.SendCatchLog(signal.ResponseReceived, map[string]any{
				"status": 200,
				"url":    fmt.Sprintf("http://example.com/page/%d", idx),
			})
		}(i)
	}
	wg.Wait()

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
