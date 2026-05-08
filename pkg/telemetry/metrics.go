package telemetry

import "time"

// MetricsRegistry 定义指标注册中心接口。
//
// MetricsRegistry 负责创建和管理各类指标（Counter、Gauge、Histogram）。
// 框架通过 MetricsRegistry 注册指标，并在运行时更新指标值。
//
// 实现者需保证所有方法的线程安全性。
type MetricsRegistry interface {
	// Counter 获取或创建一个计数器。
	// 计数器只能递增，适用于记录请求总数、错误总数等。
	Counter(name string, description string) Counter

	// Gauge 获取或创建一个仪表盘。
	// 仪表盘可增可减，适用于记录当前活跃请求数、队列长度等。
	Gauge(name string, description string) Gauge

	// Histogram 获取或创建一个直方图。
	// 直方图用于记录值的分布，适用于记录请求延迟、响应大小等。
	Histogram(name string, description string, buckets []float64) Histogram

	// Shutdown 关闭指标注册中心，刷新所有待发送的指标数据。
	// 应在应用退出前调用。
	Shutdown() error
}

// Counter 定义计数器接口。
//
// 计数器是单调递增的指标，适用于记录累计值，如：
//   - scrapy_requests_total: 总请求数
//   - scrapy_responses_total: 总响应数
//   - scrapy_items_total: 总 Item 数
//   - scrapy_errors_total: 总错误数
type Counter interface {
	// Inc 将计数器加 1。
	Inc()

	// Add 将计数器加上指定的非负值。
	// delta 必须 >= 0，否则行为未定义。
	Add(delta float64)
}

// Gauge 定义仪表盘接口。
//
// 仪表盘是可增可减的指标，适用于记录瞬时值，如：
//   - scrapy_active_requests: 当前活跃请求数
//   - scrapy_scheduler_queue_size: 调度器队列长度
//   - scrapy_spider_state: Spider 状态（0=关闭, 1=运行中）
type Gauge interface {
	// Set 设置仪表盘的值。
	Set(value float64)

	// Inc 将仪表盘加 1。
	Inc()

	// Dec 将仪表盘减 1。
	Dec()

	// Add 将仪表盘加上指定值（可为负数）。
	Add(delta float64)
}

// Histogram 定义直方图接口。
//
// 直方图用于记录值的分布情况，适用于记录延迟、大小等，如：
//   - scrapy_request_duration_seconds: 请求延迟分布
//   - scrapy_response_size_bytes: 响应大小分布
type Histogram interface {
	// Observe 记录一个观测值。
	Observe(value float64)

	// ObserveDuration 记录一个时间段的观测值（秒）。
	ObserveDuration(d time.Duration)
}

// DefaultHistogramBuckets 提供默认的直方图桶边界（秒），
// 适用于 HTTP 请求延迟场景。
var DefaultHistogramBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}
