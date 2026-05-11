package prometheus

import (
	"sync"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// ============================================================================
// Registry 测试
// ============================================================================

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("NewRegistry 不应返回 nil")
	}
	if reg.PrometheusRegistry() == nil {
		t.Fatal("PrometheusRegistry() 不应返回 nil")
	}
}

func TestNewRegistryFrom(t *testing.T) {
	promReg := prometheus.NewRegistry()
	reg := NewRegistryFrom(promReg)
	if reg == nil {
		t.Fatal("NewRegistryFrom 不应返回 nil")
	}
	if reg.PrometheusRegistry() != promReg {
		t.Error("PrometheusRegistry() 应返回传入的 Registry")
	}
}

// ============================================================================
// Counter 测试
// ============================================================================

func TestRegistry_Counter(t *testing.T) {
	reg := NewRegistry()
	counter := reg.Counter("test_counter_total", "测试计数器")

	if counter == nil {
		t.Fatal("Counter 不应返回 nil")
	}

	// 操作不应 panic
	counter.Inc()
	counter.Add(5.0)

	// 验证值
	val := getCounterValue(t, reg, "test_counter_total")
	if val != 6.0 {
		t.Errorf("Counter 值期望 6.0，实际: %f", val)
	}
}

func TestRegistry_CounterIdempotent(t *testing.T) {
	reg := NewRegistry()

	// 多次获取同名 Counter 应返回同一实例
	c1 := reg.Counter("test_counter_total", "测试计数器")
	c2 := reg.Counter("test_counter_total", "测试计数器")

	c1.Inc()
	c2.Inc()

	val := getCounterValue(t, reg, "test_counter_total")
	if val != 2.0 {
		t.Errorf("同名 Counter 应共享状态，期望 2.0，实际: %f", val)
	}
}

// ============================================================================
// Gauge 测试
// ============================================================================

func TestRegistry_Gauge(t *testing.T) {
	reg := NewRegistry()
	gauge := reg.Gauge("test_gauge", "测试仪表盘")

	if gauge == nil {
		t.Fatal("Gauge 不应返回 nil")
	}

	gauge.Set(10.0)
	val := getGaugeValue(t, reg, "test_gauge")
	if val != 10.0 {
		t.Errorf("Gauge 值期望 10.0，实际: %f", val)
	}

	gauge.Inc()
	val = getGaugeValue(t, reg, "test_gauge")
	if val != 11.0 {
		t.Errorf("Gauge Inc 后期望 11.0，实际: %f", val)
	}

	gauge.Dec()
	val = getGaugeValue(t, reg, "test_gauge")
	if val != 10.0 {
		t.Errorf("Gauge Dec 后期望 10.0，实际: %f", val)
	}

	gauge.Add(-5.0)
	val = getGaugeValue(t, reg, "test_gauge")
	if val != 5.0 {
		t.Errorf("Gauge Add(-5) 后期望 5.0，实际: %f", val)
	}
}

func TestRegistry_GaugeIdempotent(t *testing.T) {
	reg := NewRegistry()

	g1 := reg.Gauge("test_gauge", "测试仪表盘")
	g2 := reg.Gauge("test_gauge", "测试仪表盘")

	g1.Set(10.0)
	g2.Add(5.0)

	val := getGaugeValue(t, reg, "test_gauge")
	if val != 15.0 {
		t.Errorf("同名 Gauge 应共享状态，期望 15.0，实际: %f", val)
	}
}

// ============================================================================
// Histogram 测试
// ============================================================================

func TestRegistry_Histogram(t *testing.T) {
	reg := NewRegistry()
	histo := reg.Histogram(
		"test_duration_seconds",
		"测试直方图",
		telemetry.DefaultHistogramBuckets,
	)

	if histo == nil {
		t.Fatal("Histogram 不应返回 nil")
	}

	histo.Observe(0.5)
	histo.Observe(1.2)
	histo.ObserveDuration(100 * time.Millisecond)
}

func TestRegistry_HistogramNilBuckets(t *testing.T) {
	reg := NewRegistry()
	histo := reg.Histogram(
		"test_histo_nil_buckets",
		"测试直方图（nil buckets）",
		nil,
	)

	if histo == nil {
		t.Fatal("Histogram 不应返回 nil（即使 buckets 为 nil）")
	}

	histo.Observe(0.5)
}

func TestRegistry_HistogramIdempotent(t *testing.T) {
	reg := NewRegistry()

	h1 := reg.Histogram("test_histo", "测试", telemetry.DefaultHistogramBuckets)
	h2 := reg.Histogram("test_histo", "测试", telemetry.DefaultHistogramBuckets)

	h1.Observe(0.5)
	h2.Observe(1.0)

	// 不 panic 即为通过
}

// ============================================================================
// Shutdown 测试
// ============================================================================

func TestRegistry_Shutdown(t *testing.T) {
	reg := NewRegistry()
	err := reg.Shutdown()
	if err != nil {
		t.Errorf("Shutdown 应返回 nil，实际: %v", err)
	}
}

// ============================================================================
// 并发安全测试
// ============================================================================

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter := reg.Counter("concurrent_counter_total", "并发计数器")
			counter.Inc()
			counter.Add(1.0)

			gauge := reg.Gauge("concurrent_gauge", "并发仪表盘")
			gauge.Set(1.0)
			gauge.Inc()
			gauge.Dec()

			histo := reg.Histogram("concurrent_histo_seconds", "并发直方图", telemetry.DefaultHistogramBuckets)
			histo.Observe(0.5)
			histo.ObserveDuration(time.Millisecond)
		}()
	}
	wg.Wait()

	// 验证 Counter 值
	val := getCounterValue(t, reg, "concurrent_counter_total")
	if val != 200.0 { // 100 * (Inc + Add(1.0))
		t.Errorf("并发 Counter 值期望 200.0，实际: %f", val)
	}
}

// ============================================================================
// 接口可赋值性测试
// ============================================================================

func TestInterfaceAssignability(t *testing.T) {
	var registry telemetry.MetricsRegistry = NewRegistry()
	if registry == nil {
		t.Error("MetricsRegistry 接口变量不应为 nil")
	}

	counter := registry.Counter("test_total", "test")
	if counter == nil {
		t.Error("Counter 不应为 nil")
	}

	gauge := registry.Gauge("test_gauge", "test")
	if gauge == nil {
		t.Error("Gauge 不应为 nil")
	}

	histo := registry.Histogram("test_histo", "test", nil)
	if histo == nil {
		t.Error("Histogram 不应为 nil")
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

func getCounterValue(t *testing.T, reg *Registry, name string) float64 {
	t.Helper()
	mfs, err := reg.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather 失败: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf.GetMetric()[0].GetCounter().GetValue()
		}
	}
	t.Fatalf("未找到指标: %s", name)
	return 0
}

func getGaugeValue(t *testing.T, reg *Registry, name string) float64 {
	t.Helper()
	mfs, err := reg.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather 失败: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatalf("未找到指标: %s", name)
	return 0
}

// 确保 dto 包被使用（Gather 返回的 MetricFamily 使用此包的类型）
var _ = (*dto.MetricFamily)(nil)
