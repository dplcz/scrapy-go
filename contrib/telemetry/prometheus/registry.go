// Package prometheus 提供 Prometheus MetricsRegistry 适配器。
//
// 本包将 Prometheus client_golang 的指标类型适配为 scrapy-go 的 telemetry.MetricsRegistry 接口，
// 使框架能够通过 Prometheus 标准格式暴露指标数据。
package prometheus

import (
	"sync"
	"time"

	"github.com/dplcz/scrapy-go/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
)

// Registry 是 telemetry.MetricsRegistry 接口的 Prometheus 实现。
//
// 通过包装 prometheus.Registry，将 scrapy-go 的指标接口映射到
// Prometheus 标准指标类型（Counter、Gauge、Histogram）。
//
// 线程安全：内部使用 sync.RWMutex 保护指标注册表。
type Registry struct {
	mu       sync.RWMutex
	reg      *prometheus.Registry
	counters map[string]*Counter
	gauges   map[string]*Gauge
	histos   map[string]*Histogram
}

// NewRegistry 创建一个 Prometheus MetricsRegistry 适配器。
//
// 使用独立的 prometheus.Registry（非全局默认），避免与用户代码冲突。
func NewRegistry() *Registry {
	return &Registry{
		reg:      prometheus.NewRegistry(),
		counters: make(map[string]*Counter),
		gauges:   make(map[string]*Gauge),
		histos:   make(map[string]*Histogram),
	}
}

// NewRegistryFrom 基于已有的 prometheus.Registry 创建适配器。
//
// 适用于用户已有 Prometheus Registry 的场景，可复用已有注册表。
func NewRegistryFrom(reg *prometheus.Registry) *Registry {
	return &Registry{
		reg:      reg,
		counters: make(map[string]*Counter),
		gauges:   make(map[string]*Gauge),
		histos:   make(map[string]*Histogram),
	}
}

// PrometheusRegistry 返回底层的 prometheus.Registry。
//
// 用于将 Registry 传递给 promhttp.HandlerFor 创建 HTTP 端点。
func (r *Registry) PrometheusRegistry() *prometheus.Registry {
	return r.reg
}

// Counter 获取或创建一个 Prometheus 计数器。
func (r *Registry) Counter(name string, description string) telemetry.Counter {
	r.mu.RLock()
	if c, ok := r.counters[name]; ok {
		r.mu.RUnlock()
		return c
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 双重检查
	if c, ok := r.counters[name]; ok {
		return c
	}

	promCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: name,
		Help: description,
	})
	r.reg.MustRegister(promCounter)

	c := &Counter{counter: promCounter}
	r.counters[name] = c
	return c
}

// Gauge 获取或创建一个 Prometheus 仪表盘。
func (r *Registry) Gauge(name string, description string) telemetry.Gauge {
	r.mu.RLock()
	if g, ok := r.gauges[name]; ok {
		r.mu.RUnlock()
		return g
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 双重检查
	if g, ok := r.gauges[name]; ok {
		return g
	}

	promGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: name,
		Help: description,
	})
	r.reg.MustRegister(promGauge)

	g := &Gauge{gauge: promGauge}
	r.gauges[name] = g
	return g
}

// Histogram 获取或创建一个 Prometheus 直方图。
func (r *Registry) Histogram(name string, description string, buckets []float64) telemetry.Histogram {
	r.mu.RLock()
	if h, ok := r.histos[name]; ok {
		r.mu.RUnlock()
		return h
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 双重检查
	if h, ok := r.histos[name]; ok {
		return h
	}

	if len(buckets) == 0 {
		buckets = telemetry.DefaultHistogramBuckets
	}

	promHisto := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    name,
		Help:    description,
		Buckets: buckets,
	})
	r.reg.MustRegister(promHisto)

	h := &Histogram{histogram: promHisto}
	r.histos[name] = h
	return h
}

// Shutdown 关闭指标注册中心。
// Prometheus 客户端无需显式关闭，此方法为空操作。
func (r *Registry) Shutdown() error {
	return nil
}

// ============================================================================
// Counter 适配器
// ============================================================================

// Counter 是 telemetry.Counter 接口的 Prometheus 实现。
type Counter struct {
	counter prometheus.Counter
}

// Inc 将计数器加 1。
func (c *Counter) Inc() {
	c.counter.Inc()
}

// Add 将计数器加上指定的非负值。
func (c *Counter) Add(delta float64) {
	c.counter.Add(delta)
}

// ============================================================================
// Gauge 适配器
// ============================================================================

// Gauge 是 telemetry.Gauge 接口的 Prometheus 实现。
type Gauge struct {
	gauge prometheus.Gauge
}

// Set 设置仪表盘的值。
func (g *Gauge) Set(value float64) {
	g.gauge.Set(value)
}

// Inc 将仪表盘加 1。
func (g *Gauge) Inc() {
	g.gauge.Inc()
}

// Dec 将仪表盘减 1。
func (g *Gauge) Dec() {
	g.gauge.Dec()
}

// Add 将仪表盘加上指定值。
func (g *Gauge) Add(delta float64) {
	g.gauge.Add(delta)
}

// ============================================================================
// Histogram 适配器
// ============================================================================

// Histogram 是 telemetry.Histogram 接口的 Prometheus 实现。
type Histogram struct {
	histogram prometheus.Histogram
}

// Observe 记录一个观测值。
func (h *Histogram) Observe(value float64) {
	h.histogram.Observe(value)
}

// ObserveDuration 记录一个时间段的观测值（秒）。
func (h *Histogram) ObserveDuration(d time.Duration) {
	h.histogram.Observe(d.Seconds())
}

// ============================================================================
// 编译期接口实现检查
// ============================================================================

var (
	_ telemetry.MetricsRegistry = (*Registry)(nil)
	_ telemetry.Counter         = (*Counter)(nil)
	_ telemetry.Gauge           = (*Gauge)(nil)
	_ telemetry.Histogram       = (*Histogram)(nil)
)
