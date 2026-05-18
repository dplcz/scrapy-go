package telemetry

import "time"

// LabeledCounter 定义带标签维度的计数器接口。
//
// LabeledCounter 支持按标签值分组统计，适用于需要按维度区分的场景，如：
//   - 按 Spider 名称统计请求数
//   - 按域名统计响应数
//   - 按状态码统计错误数
//
// 使用示例：
//
//	counter := registry.LabeledCounter("scrapy_requests_total", "总请求数", "spider", "domain")
//	counter.With("my_spider", "example.com").Inc()
type LabeledCounter interface {
	// With 返回指定标签值的 Counter 实例。
	// labelValues 的顺序必须与创建时指定的 labelNames 一致。
	With(labelValues ...string) Counter
}

// LabeledGauge 定义带标签维度的仪表盘接口。
//
// LabeledGauge 支持按标签值分组记录瞬时值，适用于需要按维度区分的场景，如：
//   - 按 Spider 名称统计活跃请求数
//   - 按域名统计队列深度
//
// 使用示例：
//
//	gauge := registry.LabeledGauge("scrapy_active_requests", "活跃请求数", "spider", "domain")
//	gauge.With("my_spider", "example.com").Inc()
type LabeledGauge interface {
	// With 返回指定标签值的 Gauge 实例。
	// labelValues 的顺序必须与创建时指定的 labelNames 一致。
	With(labelValues ...string) Gauge
}

// LabeledHistogram 定义带标签维度的直方图接口。
//
// LabeledHistogram 支持按标签值分组记录分布，适用于需要按维度区分的场景，如：
//   - 按 Spider 名称统计请求延迟分布
//   - 按域名统计响应大小分布
//
// 使用示例：
//
//	histo := registry.LabeledHistogram("scrapy_request_duration_seconds", "请求延迟", buckets, "spider", "domain")
//	histo.With("my_spider", "example.com").Observe(0.5)
type LabeledHistogram interface {
	// With 返回指定标签值的 Histogram 实例。
	// labelValues 的顺序必须与创建时指定的 labelNames 一致。
	With(labelValues ...string) Histogram
}

// LabeledMetricsRegistry 定义支持标签维度的指标注册中心接口。
//
// LabeledMetricsRegistry 扩展了 MetricsRegistry，增加了带标签维度的指标创建能力。
// 实现者需保证所有方法的线程安全性。
type LabeledMetricsRegistry interface {
	MetricsRegistry

	// LabeledCounter 获取或创建一个带标签维度的计数器。
	// labelNames 定义标签名称列表，后续通过 With() 传入对应的标签值。
	LabeledCounter(name string, description string, labelNames ...string) LabeledCounter

	// LabeledGauge 获取或创建一个带标签维度的仪表盘。
	// labelNames 定义标签名称列表，后续通过 With() 传入对应的标签值。
	LabeledGauge(name string, description string, labelNames ...string) LabeledGauge

	// LabeledHistogram 获取或创建一个带标签维度的直方图。
	// labelNames 定义标签名称列表，后续通过 With() 传入对应的标签值。
	LabeledHistogram(name string, description string, buckets []float64, labelNames ...string) LabeledHistogram
}

// ============================================================================
// NoopLabeled — 零开销带标签指标实现
// ============================================================================

// NoopLabeledCounter 是 LabeledCounter 接口的空操作实现。
type NoopLabeledCounter struct{}

// With 返回一个空操作 Counter。
func (c *NoopLabeledCounter) With(labelValues ...string) Counter {
	return &NoopCounter{}
}

// NoopLabeledGauge 是 LabeledGauge 接口的空操作实现。
type NoopLabeledGauge struct{}

// With 返回一个空操作 Gauge。
func (g *NoopLabeledGauge) With(labelValues ...string) Gauge {
	return &NoopGauge{}
}

// NoopLabeledHistogram 是 LabeledHistogram 接口的空操作实现。
type NoopLabeledHistogram struct{}

// With 返回一个空操作 Histogram。
func (h *NoopLabeledHistogram) With(labelValues ...string) Histogram {
	return &NoopHistogram{}
}

// NoopLabeledMetricsRegistry 是 LabeledMetricsRegistry 接口的空操作实现。
type NoopLabeledMetricsRegistry struct {
	NoopMetricsRegistry
}

// NewNoopLabeledMetricsRegistry 创建一个空操作带标签指标注册中心。
func NewNoopLabeledMetricsRegistry() *NoopLabeledMetricsRegistry {
	return &NoopLabeledMetricsRegistry{}
}

// LabeledCounter 返回一个空操作带标签计数器。
func (r *NoopLabeledMetricsRegistry) LabeledCounter(name string, description string, labelNames ...string) LabeledCounter {
	return &NoopLabeledCounter{}
}

// LabeledGauge 返回一个空操作带标签仪表盘。
func (r *NoopLabeledMetricsRegistry) LabeledGauge(name string, description string, labelNames ...string) LabeledGauge {
	return &NoopLabeledGauge{}
}

// LabeledHistogram 返回一个空操作带标签直方图。
func (r *NoopLabeledMetricsRegistry) LabeledHistogram(name string, description string, buckets []float64, labelNames ...string) LabeledHistogram {
	return &NoopLabeledHistogram{}
}

// ============================================================================
// 编译期接口实现检查
// ============================================================================

var (
	_ LabeledCounter         = (*NoopLabeledCounter)(nil)
	_ LabeledGauge           = (*NoopLabeledGauge)(nil)
	_ LabeledHistogram       = (*NoopLabeledHistogram)(nil)
	_ LabeledMetricsRegistry = (*NoopLabeledMetricsRegistry)(nil)
)

// ============================================================================
// LabeledObserveDuration 辅助函数
// ============================================================================

// LabeledObserveDuration 是一个辅助函数，用于在 LabeledHistogram 上记录时间段。
// 等价于 histo.With(labelValues...).ObserveDuration(d)。
func LabeledObserveDuration(histo LabeledHistogram, d time.Duration, labelValues ...string) {
	histo.With(labelValues...).ObserveDuration(d)
}
