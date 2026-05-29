package telemetry

import (
	"context"
	"time"
)

// ============================================================================
// NoopTracer — 零开销追踪器实现
// ============================================================================

// NoopTracer 是 Tracer 接口的空操作实现。
// 所有方法均为空操作，不产生任何运行时开销。
// 当未配置追踪后端时，框架默认使用 NoopTracer。
type NoopTracer struct{}

// NewNoopTracer 创建一个空操作追踪器。
func NewNoopTracer() *NoopTracer {
	return &NoopTracer{}
}

// Start 返回原始 context 和一个 NoopSpan。
func (t *NoopTracer) Start(ctx context.Context, operationName string, opts ...SpanOption) (context.Context, Span) {
	return ctx, &NoopSpan{}
}

// ContextWithRemoteSpanContext 空操作，返回原始 context。
func (t *NoopTracer) ContextWithRemoteSpanContext(ctx context.Context, sc SpanContext) context.Context {
	return ctx
}

// Shutdown 空操作，立即返回 nil。
func (t *NoopTracer) Shutdown(ctx context.Context) error {
	return nil
}

// ============================================================================
// NoopSpan — 零开销 Span 实现
// ============================================================================

// NoopSpan 是 Span 接口的空操作实现。
// 所有方法均为空操作，不记录任何追踪数据。
type NoopSpan struct{}

// End 空操作。
func (s *NoopSpan) End() {}

// SetAttributes 空操作。
func (s *NoopSpan) SetAttributes(attrs map[string]string) {}

// SetStatus 空操作。
func (s *NoopSpan) SetStatus(status SpanStatus, description string) {}

// RecordError 空操作。
func (s *NoopSpan) RecordError(err error) {}

// SpanContext 返回一个无效的空 SpanContext。
func (s *NoopSpan) SpanContext() SpanContext {
	return SpanContext{}
}

// AddEvent 空操作。
func (s *NoopSpan) AddEvent(name string, attrs map[string]string) {}

// ============================================================================
// NoopMetricsRegistry — 零开销指标注册中心实现
// ============================================================================

// NoopMetricsRegistry 是 MetricsRegistry 接口的空操作实现。
// 所有方法返回对应的 Noop 指标实例，不产生任何运行时开销。
// 当未配置指标后端时，框架默认使用 NoopMetricsRegistry。
type NoopMetricsRegistry struct{}

// NewNoopMetricsRegistry 创建一个空操作指标注册中心。
func NewNoopMetricsRegistry() *NoopMetricsRegistry {
	return &NoopMetricsRegistry{}
}

// Counter 返回一个空操作计数器。
func (r *NoopMetricsRegistry) Counter(name string, description string) Counter {
	return &NoopCounter{}
}

// Gauge 返回一个空操作仪表盘。
func (r *NoopMetricsRegistry) Gauge(name string, description string) Gauge {
	return &NoopGauge{}
}

// Histogram 返回一个空操作直方图。
func (r *NoopMetricsRegistry) Histogram(name string, description string, buckets []float64) Histogram {
	return &NoopHistogram{}
}

// Shutdown 空操作，立即返回 nil。
func (r *NoopMetricsRegistry) Shutdown() error {
	return nil
}

// ============================================================================
// NoopCounter — 零开销计数器实现
// ============================================================================

// NoopCounter 是 Counter 接口的空操作实现。
type NoopCounter struct{}

// Inc 空操作。
func (c *NoopCounter) Inc() {}

// Add 空操作。
func (c *NoopCounter) Add(delta float64) {}

// ============================================================================
// NoopGauge — 零开销仪表盘实现
// ============================================================================

// NoopGauge 是 Gauge 接口的空操作实现。
type NoopGauge struct{}

// Set 空操作。
func (g *NoopGauge) Set(value float64) {}

// Inc 空操作。
func (g *NoopGauge) Inc() {}

// Dec 空操作。
func (g *NoopGauge) Dec() {}

// Add 空操作。
func (g *NoopGauge) Add(delta float64) {}

// ============================================================================
// NoopHistogram — 零开销直方图实现
// ============================================================================

// NoopHistogram 是 Histogram 接口的空操作实现。
type NoopHistogram struct{}

// Observe 空操作。
func (h *NoopHistogram) Observe(value float64) {}

// ObserveDuration 空操作。
func (h *NoopHistogram) ObserveDuration(d time.Duration) {}

// ============================================================================
// 编译期接口实现检查
// ============================================================================

var (
	_ Tracer          = (*NoopTracer)(nil)
	_ Span            = (*NoopSpan)(nil)
	_ MetricsRegistry = (*NoopMetricsRegistry)(nil)
	_ Counter         = (*NoopCounter)(nil)
	_ Gauge           = (*NoopGauge)(nil)
	_ Histogram       = (*NoopHistogram)(nil)
)
