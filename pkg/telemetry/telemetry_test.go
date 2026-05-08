package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// Tracer 接口契约测试
// ============================================================================

func TestNoopTracer_Start(t *testing.T) {
	tracer := NewNoopTracer()
	ctx := context.Background()

	newCtx, span := tracer.Start(ctx, "test.operation")

	if newCtx == nil {
		t.Fatal("Start 返回的 context 不应为 nil")
	}
	if span == nil {
		t.Fatal("Start 返回的 Span 不应为 nil")
	}

	// 验证返回的 context 与传入的相同（NoopTracer 不注入任何值）
	if newCtx != ctx {
		t.Error("NoopTracer.Start 应返回原始 context")
	}
}

func TestNoopTracer_StartWithOptions(t *testing.T) {
	tracer := NewNoopTracer()
	ctx := context.Background()

	opts := SpanOption{
		Kind: SpanKindClient,
		Attributes: map[string]string{
			"http.method": "GET",
			"http.url":    "http://example.com",
		},
		StartTime: time.Now(),
	}

	newCtx, span := tracer.Start(ctx, "http.request", opts)

	if newCtx == nil {
		t.Fatal("Start 返回的 context 不应为 nil")
	}
	if span == nil {
		t.Fatal("Start 返回的 Span 不应为 nil")
	}
}

func TestNoopTracer_Shutdown(t *testing.T) {
	tracer := NewNoopTracer()
	ctx := context.Background()

	err := tracer.Shutdown(ctx)
	if err != nil {
		t.Errorf("NoopTracer.Shutdown 应返回 nil，实际返回: %v", err)
	}
}

func TestNoopTracer_ShutdownWithCancelledContext(t *testing.T) {
	tracer := NewNoopTracer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// NoopTracer 即使 context 已取消也应返回 nil
	err := tracer.Shutdown(ctx)
	if err != nil {
		t.Errorf("NoopTracer.Shutdown 应返回 nil，实际返回: %v", err)
	}
}

// ============================================================================
// Span 接口契约测试
// ============================================================================

func TestNoopSpan_End(t *testing.T) {
	span := &NoopSpan{}
	// End 不应 panic
	span.End()
}

func TestNoopSpan_SetAttributes(t *testing.T) {
	span := &NoopSpan{}
	// SetAttributes 不应 panic
	span.SetAttributes(map[string]string{
		"http.method":      "GET",
		"http.status_code": "200",
	})
	// nil map 也不应 panic
	span.SetAttributes(nil)
}

func TestNoopSpan_SetStatus(t *testing.T) {
	span := &NoopSpan{}
	// 各种状态都不应 panic
	span.SetStatus(SpanStatusUnset, "")
	span.SetStatus(SpanStatusOK, "success")
	span.SetStatus(SpanStatusError, "something went wrong")
}

func TestNoopSpan_RecordError(t *testing.T) {
	span := &NoopSpan{}
	// RecordError 不应 panic
	span.RecordError(errors.New("test error"))
	span.RecordError(nil)
}

func TestNoopSpan_SpanContext(t *testing.T) {
	span := &NoopSpan{}
	sc := span.SpanContext()

	// NoopSpan 返回无效的 SpanContext
	if sc.IsValid() {
		t.Error("NoopSpan.SpanContext() 应返回无效的 SpanContext")
	}
	if sc.TraceID != "" {
		t.Errorf("NoopSpan.SpanContext().TraceID 应为空，实际: %q", sc.TraceID)
	}
	if sc.SpanID != "" {
		t.Errorf("NoopSpan.SpanContext().SpanID 应为空，实际: %q", sc.SpanID)
	}
}

func TestNoopSpan_AddEvent(t *testing.T) {
	span := &NoopSpan{}
	// AddEvent 不应 panic
	span.AddEvent("request.sent", map[string]string{"url": "http://example.com"})
	span.AddEvent("error.occurred", nil)
}

// ============================================================================
// SpanContext 测试
// ============================================================================

func TestSpanContext_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		sc    SpanContext
		valid bool
	}{
		{
			name:  "空 SpanContext 无效",
			sc:    SpanContext{},
			valid: false,
		},
		{
			name:  "仅有 TraceID 无效",
			sc:    SpanContext{TraceID: "abc123"},
			valid: false,
		},
		{
			name:  "仅有 SpanID 无效",
			sc:    SpanContext{SpanID: "def456"},
			valid: false,
		},
		{
			name:  "TraceID 和 SpanID 均有效",
			sc:    SpanContext{TraceID: "abc123", SpanID: "def456"},
			valid: true,
		},
		{
			name:  "完整 SpanContext 有效",
			sc:    SpanContext{TraceID: "abc123", SpanID: "def456", TraceFlags: 1, IsRemote: true},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sc.IsValid(); got != tt.valid {
				t.Errorf("SpanContext.IsValid() = %v, 期望 %v", got, tt.valid)
			}
		})
	}
}

// ============================================================================
// MetricsRegistry 接口契约测试
// ============================================================================

func TestNoopMetricsRegistry_Counter(t *testing.T) {
	registry := NewNoopMetricsRegistry()
	counter := registry.Counter("scrapy_requests_total", "总请求数")

	if counter == nil {
		t.Fatal("Counter 不应返回 nil")
	}

	// 操作不应 panic
	counter.Inc()
	counter.Add(5.0)
	counter.Add(0)
}

func TestNoopMetricsRegistry_Gauge(t *testing.T) {
	registry := NewNoopMetricsRegistry()
	gauge := registry.Gauge("scrapy_active_requests", "当前活跃请求数")

	if gauge == nil {
		t.Fatal("Gauge 不应返回 nil")
	}

	// 操作不应 panic
	gauge.Set(10.0)
	gauge.Inc()
	gauge.Dec()
	gauge.Add(5.0)
	gauge.Add(-3.0)
}

func TestNoopMetricsRegistry_Histogram(t *testing.T) {
	registry := NewNoopMetricsRegistry()
	histogram := registry.Histogram(
		"scrapy_request_duration_seconds",
		"请求延迟分布",
		DefaultHistogramBuckets,
	)

	if histogram == nil {
		t.Fatal("Histogram 不应返回 nil")
	}

	// 操作不应 panic
	histogram.Observe(0.5)
	histogram.Observe(1.2)
	histogram.ObserveDuration(100 * time.Millisecond)
	histogram.ObserveDuration(2 * time.Second)
}

func TestNoopMetricsRegistry_HistogramWithNilBuckets(t *testing.T) {
	registry := NewNoopMetricsRegistry()
	histogram := registry.Histogram(
		"scrapy_response_size_bytes",
		"响应大小分布",
		nil,
	)

	if histogram == nil {
		t.Fatal("Histogram 不应返回 nil（即使 buckets 为 nil）")
	}
}

func TestNoopMetricsRegistry_Shutdown(t *testing.T) {
	registry := NewNoopMetricsRegistry()
	err := registry.Shutdown()
	if err != nil {
		t.Errorf("NoopMetricsRegistry.Shutdown 应返回 nil，实际返回: %v", err)
	}
}

// ============================================================================
// 并发安全测试
// ============================================================================

func TestNoopTracer_ConcurrentStart(t *testing.T) {
	tracer := NewNoopTracer()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, span := tracer.Start(ctx, "concurrent.operation")
			span.SetAttributes(map[string]string{"key": "value"})
			span.End()
		}()
	}
	wg.Wait()
}

func TestNoopMetricsRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewNoopMetricsRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter := registry.Counter("test_counter", "test")
			counter.Inc()
			counter.Add(1.0)

			gauge := registry.Gauge("test_gauge", "test")
			gauge.Set(1.0)
			gauge.Inc()
			gauge.Dec()

			histogram := registry.Histogram("test_histogram", "test", DefaultHistogramBuckets)
			histogram.Observe(0.5)
			histogram.ObserveDuration(time.Millisecond)
		}()
	}
	wg.Wait()
}

// ============================================================================
// SpanKind 和 SpanStatus 常量测试
// ============================================================================

func TestSpanKind_Values(t *testing.T) {
	// 验证 SpanKind 枚举值的正确性
	if SpanKindInternal != 0 {
		t.Errorf("SpanKindInternal 应为 0，实际: %d", SpanKindInternal)
	}
	if SpanKindClient != 1 {
		t.Errorf("SpanKindClient 应为 1，实际: %d", SpanKindClient)
	}
	if SpanKindServer != 2 {
		t.Errorf("SpanKindServer 应为 2，实际: %d", SpanKindServer)
	}
	if SpanKindProducer != 3 {
		t.Errorf("SpanKindProducer 应为 3，实际: %d", SpanKindProducer)
	}
	if SpanKindConsumer != 4 {
		t.Errorf("SpanKindConsumer 应为 4，实际: %d", SpanKindConsumer)
	}
}

func TestSpanStatus_Values(t *testing.T) {
	// 验证 SpanStatus 枚举值的正确性
	if SpanStatusUnset != 0 {
		t.Errorf("SpanStatusUnset 应为 0，实际: %d", SpanStatusUnset)
	}
	if SpanStatusOK != 1 {
		t.Errorf("SpanStatusOK 应为 1，实际: %d", SpanStatusOK)
	}
	if SpanStatusError != 2 {
		t.Errorf("SpanStatusError 应为 2，实际: %d", SpanStatusError)
	}
}

// ============================================================================
// DefaultHistogramBuckets 测试
// ============================================================================

func TestDefaultHistogramBuckets(t *testing.T) {
	if len(DefaultHistogramBuckets) == 0 {
		t.Fatal("DefaultHistogramBuckets 不应为空")
	}

	// 验证桶边界单调递增
	for i := 1; i < len(DefaultHistogramBuckets); i++ {
		if DefaultHistogramBuckets[i] <= DefaultHistogramBuckets[i-1] {
			t.Errorf("DefaultHistogramBuckets 应单调递增，但 [%d]=%f <= [%d]=%f",
				i, DefaultHistogramBuckets[i], i-1, DefaultHistogramBuckets[i-1])
		}
	}

	// 验证所有值为正数
	for i, v := range DefaultHistogramBuckets {
		if v <= 0 {
			t.Errorf("DefaultHistogramBuckets[%d] 应为正数，实际: %f", i, v)
		}
	}
}

// ============================================================================
// Extension 集成点测试 — 验证信号钩子预留
// ============================================================================

// TestTracerIntegrationPoint 验证 Tracer 可以在 Spider 生命周期中使用。
// 模拟框架在关键路径上创建 Span 的场景。
func TestTracerIntegrationPoint_SpiderLifecycle(t *testing.T) {
	tracer := NewNoopTracer()
	ctx := context.Background()

	// 模拟 Spider 开始爬取
	ctx, spiderSpan := tracer.Start(ctx, "spider.crawl", SpanOption{
		Kind:       SpanKindInternal,
		Attributes: map[string]string{"spider.name": "example"},
	})
	defer spiderSpan.End()

	// 模拟 HTTP 请求
	_, requestSpan := tracer.Start(ctx, "http.request", SpanOption{
		Kind: SpanKindClient,
		Attributes: map[string]string{
			"http.method": "GET",
			"http.url":    "http://example.com/page",
		},
	})

	// 模拟请求完成
	requestSpan.SetAttributes(map[string]string{
		"http.status_code":        "200",
		"http.response.body.size": "4096",
	})
	requestSpan.SetStatus(SpanStatusOK, "")
	requestSpan.End()

	// 模拟 Spider 完成
	spiderSpan.SetStatus(SpanStatusOK, "")
}

// TestTracerIntegrationPoint_ErrorHandling 验证 Tracer 在错误场景下的行为。
func TestTracerIntegrationPoint_ErrorHandling(t *testing.T) {
	tracer := NewNoopTracer()
	ctx := context.Background()

	_, span := tracer.Start(ctx, "http.request", SpanOption{
		Kind: SpanKindClient,
	})

	// 模拟请求失败
	err := errors.New("connection timeout")
	span.RecordError(err)
	span.SetStatus(SpanStatusError, err.Error())
	span.End()
}

// TestMetricsIntegrationPoint 验证 MetricsRegistry 可以在框架中使用。
// 模拟框架在运行时更新指标的场景。
func TestMetricsIntegrationPoint_RequestMetrics(t *testing.T) {
	registry := NewNoopMetricsRegistry()

	// 模拟框架注册指标
	requestsTotal := registry.Counter("scrapy_requests_total", "总请求数")
	responsesTotal := registry.Counter("scrapy_responses_total", "总响应数")
	errorsTotal := registry.Counter("scrapy_errors_total", "总错误数")
	activeRequests := registry.Gauge("scrapy_active_requests", "当前活跃请求数")
	requestDuration := registry.Histogram(
		"scrapy_request_duration_seconds",
		"请求延迟分布",
		DefaultHistogramBuckets,
	)

	// 模拟请求开始
	activeRequests.Inc()
	requestsTotal.Inc()
	start := time.Now()

	// 模拟请求完成
	duration := time.Since(start)
	requestDuration.ObserveDuration(duration)
	activeRequests.Dec()
	responsesTotal.Inc()

	// 模拟错误
	errorsTotal.Inc()
}

// TestMetricsIntegrationPoint_SpiderState 验证 Spider 状态指标的使用。
func TestMetricsIntegrationPoint_SpiderState(t *testing.T) {
	registry := NewNoopMetricsRegistry()

	spiderState := registry.Gauge("scrapy_spider_state", "Spider 状态")
	itemsTotal := registry.Counter("scrapy_items_total", "总 Item 数")

	// Spider 启动
	spiderState.Set(1.0)

	// 处理 Items
	for i := 0; i < 10; i++ {
		itemsTotal.Inc()
	}

	// Spider 关闭
	spiderState.Set(0.0)
}

// ============================================================================
// 接口可赋值性测试 — 验证接口设计的灵活性
// ============================================================================

// TestInterfaceAssignability 验证 Noop 实现可以赋值给接口变量。
func TestInterfaceAssignability(t *testing.T) {
	var tracer Tracer = NewNoopTracer()
	var registry MetricsRegistry = NewNoopMetricsRegistry()

	if tracer == nil {
		t.Error("Tracer 接口变量不应为 nil")
	}
	if registry == nil {
		t.Error("MetricsRegistry 接口变量不应为 nil")
	}

	// 验证通过接口调用方法
	ctx, span := tracer.Start(context.Background(), "test")
	if ctx == nil || span == nil {
		t.Error("通过接口调用 Start 应返回有效值")
	}
	span.End()

	counter := registry.Counter("test", "test")
	if counter == nil {
		t.Error("通过接口调用 Counter 应返回有效值")
	}
}
