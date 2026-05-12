package otel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/pkg/telemetry"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// ============================================================================
// Tracer 适配器测试
// ============================================================================

func newTestTracer() (*Tracer, *tracetest.InMemoryExporter) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	return NewTracer(tp), exporter
}

func TestTracer_Start(t *testing.T) {
	tracer, exporter := newTestTracer()
	ctx := context.Background()

	newCtx, span := tracer.Start(ctx, "test.operation")

	if newCtx == nil {
		t.Fatal("Start 返回的 context 不应为 nil")
	}
	if span == nil {
		t.Fatal("Start 返回的 Span 不应为 nil")
	}

	span.End()

	// 验证 Span 已导出
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("期望 1 个 Span，实际: %d", len(spans))
	}
	if spans[0].Name != "test.operation" {
		t.Errorf("Span 名称期望 'test.operation'，实际: %q", spans[0].Name)
	}
}

func TestTracer_StartWithOptions(t *testing.T) {
	tracer, exporter := newTestTracer()
	ctx := context.Background()

	startTime := time.Now().Add(-1 * time.Second)
	opts := telemetry.SpanOption{
		Kind: telemetry.SpanKindClient,
		Attributes: map[string]string{
			"http.method": "GET",
			"http.url":    "http://example.com",
		},
		StartTime: startTime,
	}

	_, span := tracer.Start(ctx, "http.request", opts)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("期望 1 个 Span，实际: %d", len(spans))
	}

	// 验证属性
	found := make(map[string]string)
	for _, attr := range spans[0].Attributes {
		found[string(attr.Key)] = attr.Value.AsString()
	}
	if found["http.method"] != "GET" {
		t.Errorf("http.method 期望 'GET'，实际: %q", found["http.method"])
	}
	if found["http.url"] != "http://example.com" {
		t.Errorf("http.url 期望 'http://example.com'，实际: %q", found["http.url"])
	}
}

func TestTracer_Shutdown(t *testing.T) {
	tracer, _ := newTestTracer()
	ctx := context.Background()

	err := tracer.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown 应返回 nil，实际: %v", err)
	}
}

func TestTracer_ShutdownNonSDKProvider(t *testing.T) {
	// 使用不支持 Shutdown 的 TracerProvider（OTel 内置 Noop 不实现 Shutdown）
	tracer := NewTracer(oteltrace.NewNoopTracerProvider())
	err := tracer.Shutdown(context.Background())
	if err != nil {
		t.Errorf("非 SDK TracerProvider 的 Shutdown 应返回 nil，实际: %v", err)
	}
}

// ============================================================================
// Span 适配器测试
// ============================================================================

func TestSpan_SetAttributes(t *testing.T) {
	tracer, exporter := newTestTracer()
	_, span := tracer.Start(context.Background(), "test")

	span.SetAttributes(map[string]string{
		"key1": "value1",
		"key2": "value2",
	})
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("期望 1 个 Span，实际: %d", len(spans))
	}

	found := make(map[string]string)
	for _, attr := range spans[0].Attributes {
		found[string(attr.Key)] = attr.Value.AsString()
	}
	if found["key1"] != "value1" {
		t.Errorf("key1 期望 'value1'，实际: %q", found["key1"])
	}
	if found["key2"] != "value2" {
		t.Errorf("key2 期望 'value2'，实际: %q", found["key2"])
	}
}

func TestSpan_SetAttributesNil(t *testing.T) {
	tracer, _ := newTestTracer()
	_, span := tracer.Start(context.Background(), "test")

	// nil map 不应 panic
	span.SetAttributes(nil)
	span.End()
}

func TestSpan_SetStatus(t *testing.T) {
	tracer, exporter := newTestTracer()

	tests := []struct {
		name   string
		status telemetry.SpanStatus
		desc   string
	}{
		{"OK", telemetry.SpanStatusOK, "success"},
		{"Error", telemetry.SpanStatusError, "something went wrong"},
		{"Unset", telemetry.SpanStatusUnset, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, span := tracer.Start(context.Background(), "test."+tt.name)
			span.SetStatus(tt.status, tt.desc)
			span.End()
		})
	}

	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("期望 3 个 Span，实际: %d", len(spans))
	}
}

func TestSpan_RecordError(t *testing.T) {
	tracer, exporter := newTestTracer()
	_, span := tracer.Start(context.Background(), "test")

	testErr := errors.New("test error")
	span.RecordError(testErr)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("期望 1 个 Span，实际: %d", len(spans))
	}

	// 验证事件中包含错误
	if len(spans[0].Events) == 0 {
		t.Error("期望至少 1 个事件（错误记录）")
	}
}

func TestSpan_RecordErrorNil(t *testing.T) {
	tracer, _ := newTestTracer()
	_, span := tracer.Start(context.Background(), "test")

	// nil error 不应 panic
	span.RecordError(nil)
	span.End()
}

func TestSpan_SpanContext(t *testing.T) {
	tracer, _ := newTestTracer()
	_, span := tracer.Start(context.Background(), "test")

	sc := span.SpanContext()
	if !sc.IsValid() {
		t.Error("SpanContext 应有效（TraceID 和 SpanID 非空）")
	}
	if sc.TraceID == "" {
		t.Error("TraceID 不应为空")
	}
	if sc.SpanID == "" {
		t.Error("SpanID 不应为空")
	}

	span.End()
}

func TestSpan_AddEvent(t *testing.T) {
	tracer, exporter := newTestTracer()
	_, span := tracer.Start(context.Background(), "test")

	span.AddEvent("request.sent", map[string]string{"url": "http://example.com"})
	span.AddEvent("simple.event", nil)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("期望 1 个 Span，实际: %d", len(spans))
	}
	if len(spans[0].Events) != 2 {
		t.Errorf("期望 2 个事件，实际: %d", len(spans[0].Events))
	}
}

// ============================================================================
// SpanKind 映射测试
// ============================================================================

func TestMapSpanKind(t *testing.T) {
	tests := []struct {
		input    telemetry.SpanKind
		expected string
	}{
		{telemetry.SpanKindInternal, "internal"},
		{telemetry.SpanKindClient, "client"},
		{telemetry.SpanKindServer, "server"},
		{telemetry.SpanKindProducer, "producer"},
		{telemetry.SpanKindConsumer, "consumer"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := mapSpanKind(tt.input)
			if result.String() != tt.expected {
				t.Errorf("mapSpanKind(%d) = %q, 期望 %q", tt.input, result.String(), tt.expected)
			}
		})
	}
}

// ============================================================================
// 并发安全测试
// ============================================================================

func TestTracer_ConcurrentStart(t *testing.T) {
	tracer, _ := newTestTracer()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, span := tracer.Start(ctx, "concurrent.operation")
			span.SetAttributes(map[string]string{"key": "value"})
			span.SetStatus(telemetry.SpanStatusOK, "")
			span.End()
		}()
	}
	wg.Wait()
}

// ============================================================================
// 接口可赋值性测试
// ============================================================================

func TestInterfaceAssignability(t *testing.T) {
	tracer, _ := newTestTracer()

	var iTracer telemetry.Tracer = tracer
	if iTracer == nil {
		t.Error("Tracer 接口变量不应为 nil")
	}

	ctx, span := iTracer.Start(context.Background(), "test")
	if ctx == nil || span == nil {
		t.Error("通过接口调用 Start 应返回有效值")
	}

	var iSpan telemetry.Span = span
	if iSpan == nil {
		t.Error("Span 接口变量不应为 nil")
	}
	iSpan.End()
}

// ============================================================================
// 父子 Span 关系测试
// ============================================================================

func TestTracer_ParentChildSpans(t *testing.T) {
	tracer, exporter := newTestTracer()
	ctx := context.Background()

	// 创建父 Span
	ctx, parentSpan := tracer.Start(ctx, "parent.operation")

	// 创建子 Span（通过传递包含父 Span 的 context）
	_, childSpan := tracer.Start(ctx, "child.operation", telemetry.SpanOption{
		Kind: telemetry.SpanKindClient,
	})

	childSpan.End()
	parentSpan.End()

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("期望 2 个 Span，实际: %d", len(spans))
	}

	// 验证父子关系：两个 Span 应有相同的 TraceID
	parentSC := parentSpan.SpanContext()
	childSC := childSpan.SpanContext()
	if parentSC.TraceID != childSC.TraceID {
		t.Errorf("父子 Span 应有相同的 TraceID，父: %q, 子: %q", parentSC.TraceID, childSC.TraceID)
	}
}
