// Package prometheus 提供 Prometheus MetricsRegistry 适配器。
package prometheus

import (
	"sync"
	"time"

	"github.com/dplcz/scrapy-go/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
)

// ============================================================================
// LabeledCounter — 带标签维度的 Prometheus 计数器
// ============================================================================

// LabeledCounter 是 telemetry.LabeledCounter 接口的 Prometheus 实现。
//
// 内部使用 prometheus.CounterVec 实现按标签值分组的计数器。
// 线程安全：由 prometheus.CounterVec 内部保证。
type LabeledCounter struct {
	vec *prometheus.CounterVec
}

// With 返回指定标签值的 Counter 实例。
func (c *LabeledCounter) With(labelValues ...string) telemetry.Counter {
	return &labeledCounterChild{counter: c.vec.WithLabelValues(labelValues...)}
}

// labeledCounterChild 包装 prometheus.Counter 实现 telemetry.Counter 接口。
type labeledCounterChild struct {
	counter prometheus.Counter
}

// Inc 将计数器加 1。
func (c *labeledCounterChild) Inc() {
	c.counter.Inc()
}

// Add 将计数器加上指定的非负值。
func (c *labeledCounterChild) Add(delta float64) {
	c.counter.Add(delta)
}

// ============================================================================
// LabeledGauge — 带标签维度的 Prometheus 仪表盘
// ============================================================================

// LabeledGauge 是 telemetry.LabeledGauge 接口的 Prometheus 实现。
//
// 内部使用 prometheus.GaugeVec 实现按标签值分组的仪表盘。
// 线程安全：由 prometheus.GaugeVec 内部保证。
type LabeledGauge struct {
	vec *prometheus.GaugeVec
}

// With 返回指定标签值的 Gauge 实例。
func (g *LabeledGauge) With(labelValues ...string) telemetry.Gauge {
	return &labeledGaugeChild{gauge: g.vec.WithLabelValues(labelValues...)}
}

// labeledGaugeChild 包装 prometheus.Gauge 实现 telemetry.Gauge 接口。
type labeledGaugeChild struct {
	gauge prometheus.Gauge
}

// Set 设置仪表盘的值。
func (g *labeledGaugeChild) Set(value float64) {
	g.gauge.Set(value)
}

// Inc 将仪表盘加 1。
func (g *labeledGaugeChild) Inc() {
	g.gauge.Inc()
}

// Dec 将仪表盘减 1。
func (g *labeledGaugeChild) Dec() {
	g.gauge.Dec()
}

// Add 将仪表盘加上指定值。
func (g *labeledGaugeChild) Add(delta float64) {
	g.gauge.Add(delta)
}

// ============================================================================
// LabeledHistogram — 带标签维度的 Prometheus 直方图
// ============================================================================

// LabeledHistogram 是 telemetry.LabeledHistogram 接口的 Prometheus 实现。
//
// 内部使用 prometheus.HistogramVec 实现按标签值分组的直方图。
// 线程安全：由 prometheus.HistogramVec 内部保证。
type LabeledHistogram struct {
	vec *prometheus.HistogramVec
}

// With 返回指定标签值的 Histogram 实例。
func (h *LabeledHistogram) With(labelValues ...string) telemetry.Histogram {
	return &labeledHistogramChild{observer: h.vec.WithLabelValues(labelValues...)}
}

// labeledHistogramChild 包装 prometheus.Observer 实现 telemetry.Histogram 接口。
type labeledHistogramChild struct {
	observer prometheus.Observer
}

// Observe 记录一个观测值。
func (h *labeledHistogramChild) Observe(value float64) {
	h.observer.Observe(value)
}

// ObserveDuration 记录一个时间段的观测值（秒）。
func (h *labeledHistogramChild) ObserveDuration(d time.Duration) {
	h.observer.Observe(d.Seconds())
}

// ============================================================================
// LabeledRegistry — 支持标签维度的 Prometheus 注册中心
// ============================================================================

// LabeledRegistry 是 telemetry.LabeledMetricsRegistry 接口的 Prometheus 实现。
//
// 在 Registry 的基础上增加了带标签维度的指标创建能力，
// 支持按 Spider 名称、域名等维度分组指标。
//
// 线程安全：内部使用 sync.RWMutex 保护指标注册表。
type LabeledRegistry struct {
	*Registry
	mu              sync.RWMutex
	labeledCounters map[string]*LabeledCounter
	labeledGauges   map[string]*LabeledGauge
	labeledHistos   map[string]*LabeledHistogram
}

// NewLabeledRegistry 创建一个支持标签维度的 Prometheus MetricsRegistry 适配器。
//
// 使用独立的 prometheus.Registry（非全局默认），避免与用户代码冲突。
func NewLabeledRegistry() *LabeledRegistry {
	return &LabeledRegistry{
		Registry:        NewRegistry(),
		labeledCounters: make(map[string]*LabeledCounter),
		labeledGauges:   make(map[string]*LabeledGauge),
		labeledHistos:   make(map[string]*LabeledHistogram),
	}
}

// NewLabeledRegistryFrom 基于已有的 prometheus.Registry 创建支持标签维度的适配器。
func NewLabeledRegistryFrom(reg *prometheus.Registry) *LabeledRegistry {
	return &LabeledRegistry{
		Registry:        NewRegistryFrom(reg),
		labeledCounters: make(map[string]*LabeledCounter),
		labeledGauges:   make(map[string]*LabeledGauge),
		labeledHistos:   make(map[string]*LabeledHistogram),
	}
}

// LabeledCounter 获取或创建一个带标签维度的 Prometheus 计数器。
func (r *LabeledRegistry) LabeledCounter(name string, description string, labelNames ...string) telemetry.LabeledCounter {
	r.mu.RLock()
	if c, ok := r.labeledCounters[name]; ok {
		r.mu.RUnlock()
		return c
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 双重检查
	if c, ok := r.labeledCounters[name]; ok {
		return c
	}

	vec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: name,
		Help: description,
	}, labelNames)
	r.Registry.reg.MustRegister(vec)

	c := &LabeledCounter{vec: vec}
	r.labeledCounters[name] = c
	return c
}

// LabeledGauge 获取或创建一个带标签维度的 Prometheus 仪表盘。
func (r *LabeledRegistry) LabeledGauge(name string, description string, labelNames ...string) telemetry.LabeledGauge {
	r.mu.RLock()
	if g, ok := r.labeledGauges[name]; ok {
		r.mu.RUnlock()
		return g
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 双重检查
	if g, ok := r.labeledGauges[name]; ok {
		return g
	}

	vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: name,
		Help: description,
	}, labelNames)
	r.Registry.reg.MustRegister(vec)

	g := &LabeledGauge{vec: vec}
	r.labeledGauges[name] = g
	return g
}

// LabeledHistogram 获取或创建一个带标签维度的 Prometheus 直方图。
func (r *LabeledRegistry) LabeledHistogram(name string, description string, buckets []float64, labelNames ...string) telemetry.LabeledHistogram {
	r.mu.RLock()
	if h, ok := r.labeledHistos[name]; ok {
		r.mu.RUnlock()
		return h
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 双重检查
	if h, ok := r.labeledHistos[name]; ok {
		return h
	}

	if len(buckets) == 0 {
		buckets = telemetry.DefaultHistogramBuckets
	}

	vec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    name,
		Help:    description,
		Buckets: buckets,
	}, labelNames)
	r.Registry.reg.MustRegister(vec)

	h := &LabeledHistogram{vec: vec}
	r.labeledHistos[name] = h
	return h
}

// ============================================================================
// 编译期接口实现检查
// ============================================================================

var (
	_ telemetry.LabeledCounter         = (*LabeledCounter)(nil)
	_ telemetry.LabeledGauge           = (*LabeledGauge)(nil)
	_ telemetry.LabeledHistogram       = (*LabeledHistogram)(nil)
	_ telemetry.LabeledMetricsRegistry = (*LabeledRegistry)(nil)
	_ telemetry.Counter                = (*labeledCounterChild)(nil)
	_ telemetry.Gauge                  = (*labeledGaugeChild)(nil)
	_ telemetry.Histogram              = (*labeledHistogramChild)(nil)
)
