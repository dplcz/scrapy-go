package prometheus

import (
	"sync"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/pkg/telemetry"
	dto "github.com/prometheus/client_model/go"
)

// ============================================================================
// LabeledCounter 测试
// ============================================================================

func TestLabeledRegistry_LabeledCounter(t *testing.T) {
	reg := NewLabeledRegistry()
	counter := reg.LabeledCounter("scrapy_requests_by_spider_total", "按 Spider 分组的请求数", "spider", "domain")

	if counter == nil {
		t.Fatal("LabeledCounter 不应返回 nil")
	}

	// 按不同标签值操作
	counter.With("spider_a", "example.com").Inc()
	counter.With("spider_a", "example.com").Add(4.0)
	counter.With("spider_b", "test.org").Inc()

	// 验证 spider_a + example.com 的值
	val := getLabeledCounterValue(t, reg, "scrapy_requests_by_spider_total", map[string]string{
		"spider": "spider_a",
		"domain": "example.com",
	})
	if val != 5.0 {
		t.Errorf("spider_a/example.com Counter 值期望 5.0，实际: %f", val)
	}

	// 验证 spider_b + test.org 的值
	val = getLabeledCounterValue(t, reg, "scrapy_requests_by_spider_total", map[string]string{
		"spider": "spider_b",
		"domain": "test.org",
	})
	if val != 1.0 {
		t.Errorf("spider_b/test.org Counter 值期望 1.0，实际: %f", val)
	}
}

func TestLabeledRegistry_LabeledCounterIdempotent(t *testing.T) {
	reg := NewLabeledRegistry()

	c1 := reg.LabeledCounter("test_labeled_counter_total", "测试", "spider")
	c2 := reg.LabeledCounter("test_labeled_counter_total", "测试", "spider")

	c1.With("spider_a").Inc()
	c2.With("spider_a").Inc()

	val := getLabeledCounterValue(t, reg, "test_labeled_counter_total", map[string]string{
		"spider": "spider_a",
	})
	if val != 2.0 {
		t.Errorf("同名 LabeledCounter 应共享状态，期望 2.0，实际: %f", val)
	}
}

// ============================================================================
// LabeledGauge 测试
// ============================================================================

func TestLabeledRegistry_LabeledGauge(t *testing.T) {
	reg := NewLabeledRegistry()
	gauge := reg.LabeledGauge("scrapy_active_requests_by_spider", "按 Spider 分组的活跃请求数", "spider")

	if gauge == nil {
		t.Fatal("LabeledGauge 不应返回 nil")
	}

	gauge.With("spider_a").Set(10.0)
	gauge.With("spider_a").Inc()
	gauge.With("spider_b").Set(5.0)
	gauge.With("spider_b").Dec()

	// 验证 spider_a 的值
	val := getLabeledGaugeValue(t, reg, "scrapy_active_requests_by_spider", map[string]string{
		"spider": "spider_a",
	})
	if val != 11.0 {
		t.Errorf("spider_a Gauge 值期望 11.0，实际: %f", val)
	}

	// 验证 spider_b 的值
	val = getLabeledGaugeValue(t, reg, "scrapy_active_requests_by_spider", map[string]string{
		"spider": "spider_b",
	})
	if val != 4.0 {
		t.Errorf("spider_b Gauge 值期望 4.0，实际: %f", val)
	}
}

func TestLabeledRegistry_LabeledGaugeAdd(t *testing.T) {
	reg := NewLabeledRegistry()
	gauge := reg.LabeledGauge("test_labeled_gauge", "测试", "label")

	gauge.With("a").Set(10.0)
	gauge.With("a").Add(-3.0)

	val := getLabeledGaugeValue(t, reg, "test_labeled_gauge", map[string]string{
		"label": "a",
	})
	if val != 7.0 {
		t.Errorf("Gauge Add(-3) 后期望 7.0，实际: %f", val)
	}
}

func TestLabeledRegistry_LabeledGaugeIdempotent(t *testing.T) {
	reg := NewLabeledRegistry()

	g1 := reg.LabeledGauge("test_labeled_gauge_idem", "测试", "spider")
	g2 := reg.LabeledGauge("test_labeled_gauge_idem", "测试", "spider")

	g1.With("spider_a").Set(10.0)
	g2.With("spider_a").Add(5.0)

	val := getLabeledGaugeValue(t, reg, "test_labeled_gauge_idem", map[string]string{
		"spider": "spider_a",
	})
	if val != 15.0 {
		t.Errorf("同名 LabeledGauge 应共享状态，期望 15.0，实际: %f", val)
	}
}

// ============================================================================
// LabeledHistogram 测试
// ============================================================================

func TestLabeledRegistry_LabeledHistogram(t *testing.T) {
	reg := NewLabeledRegistry()
	histo := reg.LabeledHistogram(
		"scrapy_request_duration_by_spider_seconds",
		"按 Spider 分组的请求延迟",
		telemetry.DefaultHistogramBuckets,
		"spider", "domain",
	)

	if histo == nil {
		t.Fatal("LabeledHistogram 不应返回 nil")
	}

	histo.With("spider_a", "example.com").Observe(0.5)
	histo.With("spider_a", "example.com").Observe(1.2)
	histo.With("spider_b", "test.org").ObserveDuration(100 * time.Millisecond)

	// 不 panic 即为通过
}

func TestLabeledRegistry_LabeledHistogramNilBuckets(t *testing.T) {
	reg := NewLabeledRegistry()
	histo := reg.LabeledHistogram(
		"test_labeled_histo_nil_buckets",
		"测试（nil buckets）",
		nil,
		"spider",
	)

	if histo == nil {
		t.Fatal("LabeledHistogram 不应返回 nil（即使 buckets 为 nil）")
	}

	histo.With("spider_a").Observe(0.5)
}

func TestLabeledRegistry_LabeledHistogramIdempotent(t *testing.T) {
	reg := NewLabeledRegistry()

	h1 := reg.LabeledHistogram("test_labeled_histo_idem", "测试", telemetry.DefaultHistogramBuckets, "spider")
	h2 := reg.LabeledHistogram("test_labeled_histo_idem", "测试", telemetry.DefaultHistogramBuckets, "spider")

	h1.With("spider_a").Observe(0.5)
	h2.With("spider_a").Observe(1.0)

	// 不 panic 即为通过
}

// ============================================================================
// LabeledRegistry 基础功能测试
// ============================================================================

func TestNewLabeledRegistry(t *testing.T) {
	reg := NewLabeledRegistry()
	if reg == nil {
		t.Fatal("NewLabeledRegistry 不应返回 nil")
	}
	if reg.PrometheusRegistry() == nil {
		t.Fatal("PrometheusRegistry() 不应返回 nil")
	}
}

func TestNewLabeledRegistryFrom(t *testing.T) {
	promReg := NewRegistry().PrometheusRegistry()
	reg := NewLabeledRegistryFrom(promReg)
	if reg == nil {
		t.Fatal("NewLabeledRegistryFrom 不应返回 nil")
	}
}

func TestLabeledRegistry_InheritsBaseRegistry(t *testing.T) {
	reg := NewLabeledRegistry()

	// 验证基础 Counter/Gauge/Histogram 仍然可用
	counter := reg.Counter("base_counter_total", "基础计数器")
	counter.Inc()

	gauge := reg.Gauge("base_gauge", "基础仪表盘")
	gauge.Set(42.0)

	histo := reg.Histogram("base_histo_seconds", "基础直方图", telemetry.DefaultHistogramBuckets)
	histo.Observe(0.5)

	// 不 panic 即为通过
}

func TestLabeledRegistry_Shutdown(t *testing.T) {
	reg := NewLabeledRegistry()
	err := reg.Shutdown()
	if err != nil {
		t.Errorf("Shutdown 应返回 nil，实际: %v", err)
	}
}

// ============================================================================
// 并发安全测试
// ============================================================================

func TestLabeledRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewLabeledRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			spider := "spider_a"
			if idx%2 == 0 {
				spider = "spider_b"
			}

			counter := reg.LabeledCounter("concurrent_labeled_counter_total", "并发计数器", "spider")
			counter.With(spider).Inc()

			gauge := reg.LabeledGauge("concurrent_labeled_gauge", "并发仪表盘", "spider")
			gauge.With(spider).Set(1.0)
			gauge.With(spider).Inc()
			gauge.With(spider).Dec()

			histo := reg.LabeledHistogram("concurrent_labeled_histo_seconds", "并发直方图", telemetry.DefaultHistogramBuckets, "spider")
			histo.With(spider).Observe(0.5)
			histo.With(spider).ObserveDuration(time.Millisecond)
		}(i)
	}
	wg.Wait()

	// 验证 Counter 值（spider_a 50 次 + spider_b 50 次）
	valA := getLabeledCounterValue(t, reg, "concurrent_labeled_counter_total", map[string]string{
		"spider": "spider_a",
	})
	valB := getLabeledCounterValue(t, reg, "concurrent_labeled_counter_total", map[string]string{
		"spider": "spider_b",
	})
	if valA+valB != 100.0 {
		t.Errorf("并发 LabeledCounter 总值期望 100.0，实际: %f", valA+valB)
	}
}

// ============================================================================
// 接口可赋值性测试
// ============================================================================

func TestLabeledInterfaceAssignability(t *testing.T) {
	var registry telemetry.LabeledMetricsRegistry = NewLabeledRegistry()
	if registry == nil {
		t.Error("LabeledMetricsRegistry 接口变量不应为 nil")
	}

	counter := registry.LabeledCounter("test_total", "test", "spider")
	if counter == nil {
		t.Error("LabeledCounter 不应为 nil")
	}

	gauge := registry.LabeledGauge("test_gauge", "test", "spider")
	if gauge == nil {
		t.Error("LabeledGauge 不应为 nil")
	}

	histo := registry.LabeledHistogram("test_histo", "test", nil, "spider")
	if histo == nil {
		t.Error("LabeledHistogram 不应为 nil")
	}

	// 验证也满足 MetricsRegistry 接口
	var baseRegistry telemetry.MetricsRegistry = registry
	if baseRegistry == nil {
		t.Error("LabeledMetricsRegistry 应同时满足 MetricsRegistry 接口")
	}
}

// ============================================================================
// LabeledObserveDuration 辅助函数测试
// ============================================================================

func TestLabeledObserveDuration(t *testing.T) {
	reg := NewLabeledRegistry()
	histo := reg.LabeledHistogram("test_observe_duration_seconds", "测试", telemetry.DefaultHistogramBuckets, "spider")

	// 不 panic 即为通过
	telemetry.LabeledObserveDuration(histo, 100*time.Millisecond, "spider_a")
}

// ============================================================================
// 辅助函数
// ============================================================================

func getLabeledCounterValue(t *testing.T, reg *LabeledRegistry, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := reg.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather 失败: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			for _, m := range mf.GetMetric() {
				if matchLabels(m.GetLabel(), labels) {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	t.Fatalf("未找到指标: %s (labels: %v)", name, labels)
	return 0
}

func getLabeledGaugeValue(t *testing.T, reg *LabeledRegistry, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := reg.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather 失败: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			for _, m := range mf.GetMetric() {
				if matchLabels(m.GetLabel(), labels) {
					return m.GetGauge().GetValue()
				}
			}
		}
	}
	t.Fatalf("未找到指标: %s (labels: %v)", name, labels)
	return 0
}

func matchLabels(metricLabels []*dto.LabelPair, expected map[string]string) bool {
	if len(metricLabels) != len(expected) {
		return false
	}
	for _, lp := range metricLabels {
		expectedVal, ok := expected[lp.GetName()]
		if !ok || expectedVal != lp.GetValue() {
			return false
		}
	}
	return true
}
