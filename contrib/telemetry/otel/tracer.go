// Package otel 提供 OpenTelemetry Tracer 适配器。
//
// 本包将 OpenTelemetry SDK 的 Tracer 适配为 scrapy-go 的 telemetry.Tracer 接口，
// 使框架能够通过 OTel 标准协议导出追踪数据到 Jaeger、Zipkin、OTLP 等后端。
package otel

import (
	"context"

	"github.com/dplcz/scrapy-go/pkg/telemetry"
	otelattr "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Tracer 是 telemetry.Tracer 接口的 OpenTelemetry 实现。
//
// 通过包装 OTel SDK 的 TracerProvider，将 scrapy-go 的追踪接口
// 映射到 OpenTelemetry 标准 API，支持导出到任意 OTel 兼容后端。
//
// 线程安全：底层 OTel SDK 保证线程安全。
type Tracer struct {
	tracer   oteltrace.Tracer
	provider oteltrace.TracerProvider
}

// NewTracer 创建一个 OpenTelemetry Tracer 适配器。
//
// 参数：
//   - provider: OTel TracerProvider，由用户通过 OTel SDK 初始化
//
// 示例：
//
//	tp := sdktrace.NewTracerProvider(
//	    sdktrace.WithBatcher(exporter),
//	    sdktrace.WithResource(resource),
//	)
//	tracer := otel.NewTracer(tp)
func NewTracer(provider oteltrace.TracerProvider) *Tracer {
	return &Tracer{
		tracer:   provider.Tracer("scrapy-go"),
		provider: provider,
	}
}

// Start 创建并启动一个新的 Span。
//
// 将 scrapy-go 的 SpanOption 映射到 OTel 的 SpanStartOption：
//   - SpanOption.Kind → oteltrace.SpanKind
//   - SpanOption.Attributes → otelattr.KeyValue
//   - SpanOption.StartTime → oteltrace.WithTimestamp
func (t *Tracer) Start(ctx context.Context, operationName string, opts ...telemetry.SpanOption) (context.Context, telemetry.Span) {
	var startOpts []oteltrace.SpanStartOption

	for _, opt := range opts {
		startOpts = append(startOpts, oteltrace.WithSpanKind(mapSpanKind(opt.Kind)))

		if len(opt.Attributes) > 0 {
			attrs := mapAttributes(opt.Attributes)
			startOpts = append(startOpts, oteltrace.WithAttributes(attrs...))
		}

		if !opt.StartTime.IsZero() {
			startOpts = append(startOpts, oteltrace.WithTimestamp(opt.StartTime))
		}
	}

	ctx, otelSpan := t.tracer.Start(ctx, operationName, startOpts...)
	return ctx, &Span{span: otelSpan}
}

// Shutdown 关闭 TracerProvider，刷新所有待发送的追踪数据。
//
// 如果 TracerProvider 实现了 Shutdown 方法（如 sdktrace.TracerProvider），
// 则调用其 Shutdown；否则为空操作。
func (t *Tracer) Shutdown(ctx context.Context) error {
	type shutdowner interface {
		Shutdown(ctx context.Context) error
	}
	if s, ok := t.provider.(shutdowner); ok {
		return s.Shutdown(ctx)
	}
	return nil
}

// ContextWithRemoteSpanContext 将远程 SpanContext 注入到 context 中。
//
// 通过 OTel 的 trace.ContextWithRemoteSpanContext 实现，
// 使后续通过 Start 创建的 Span 能够正确建立父子关系。
func (t *Tracer) ContextWithRemoteSpanContext(ctx context.Context, sc telemetry.SpanContext) context.Context {
	if !sc.IsValid() {
		return ctx
	}

	// 将 scrapy-go SpanContext 转换为 OTel SpanContext
	traceID, err := oteltrace.TraceIDFromHex(sc.TraceID)
	if err != nil {
		return ctx
	}
	spanID, err := oteltrace.SpanIDFromHex(sc.SpanID)
	if err != nil {
		return ctx
	}

	otelSC := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: oteltrace.TraceFlags(sc.TraceFlags),
		Remote:     true,
	})

	return oteltrace.ContextWithRemoteSpanContext(ctx, otelSC)
}

// Span 是 telemetry.Span 接口的 OpenTelemetry 实现。
//
// 包装 OTel SDK 的 trace.Span，将 scrapy-go 的 Span 操作
// 映射到 OTel 标准 API。
//
// 线程安全：底层 OTel Span 保证线程安全。
type Span struct {
	span oteltrace.Span
}

// End 结束 Span。
func (s *Span) End() {
	s.span.End()
}

// SetAttributes 设置 Span 属性。
func (s *Span) SetAttributes(attrs map[string]string) {
	if len(attrs) == 0 {
		return
	}
	s.span.SetAttributes(mapAttributes(attrs)...)
}

// SetStatus 设置 Span 状态。
func (s *Span) SetStatus(status telemetry.SpanStatus, description string) {
	switch status {
	case telemetry.SpanStatusOK:
		s.span.SetStatus(codes.Ok, description)
	case telemetry.SpanStatusError:
		s.span.SetStatus(codes.Error, description)
	default:
		s.span.SetStatus(codes.Unset, description)
	}
}

// RecordError 记录错误事件。
func (s *Span) RecordError(err error) {
	if err == nil {
		return
	}
	s.span.RecordError(err)
}

// SpanContext 返回 Span 的上下文标识信息。
func (s *Span) SpanContext() telemetry.SpanContext {
	sc := s.span.SpanContext()
	return telemetry.SpanContext{
		TraceID:    sc.TraceID().String(),
		SpanID:     sc.SpanID().String(),
		TraceFlags: byte(sc.TraceFlags()),
		IsRemote:   sc.IsRemote(),
	}
}

// AddEvent 添加事件到 Span。
func (s *Span) AddEvent(name string, attrs map[string]string) {
	if len(attrs) == 0 {
		s.span.AddEvent(name)
		return
	}
	s.span.AddEvent(name, oteltrace.WithAttributes(mapAttributes(attrs)...))
}

// ============================================================================
// 辅助函数
// ============================================================================

// mapSpanKind 将 scrapy-go SpanKind 映射到 OTel SpanKind。
func mapSpanKind(kind telemetry.SpanKind) oteltrace.SpanKind {
	switch kind {
	case telemetry.SpanKindClient:
		return oteltrace.SpanKindClient
	case telemetry.SpanKindServer:
		return oteltrace.SpanKindServer
	case telemetry.SpanKindProducer:
		return oteltrace.SpanKindProducer
	case telemetry.SpanKindConsumer:
		return oteltrace.SpanKindConsumer
	default:
		return oteltrace.SpanKindInternal
	}
}

// mapAttributes 将 map[string]string 映射到 OTel KeyValue 切片。
func mapAttributes(attrs map[string]string) []otelattr.KeyValue {
	kvs := make([]otelattr.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		kvs = append(kvs, otelattr.String(k, v))
	}
	return kvs
}

// ============================================================================
// 编译期接口实现检查
// ============================================================================

var (
	_ telemetry.Tracer = (*Tracer)(nil)
	_ telemetry.Span   = (*Span)(nil)
)
