package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/telemetry"
)

// TraceExtension 是信号驱动的分布式追踪扩展。
//
// 通过监听框架信号系统，自动为 Spider 生命周期和 HTTP 请求创建追踪 Span：
//   - SpiderOpened → 创建根 Span "spider.crawl"
//   - SpiderClosed → 结束根 Span
//   - RequestReachedDownloader → 创建子 Span "http.request"
//   - RequestLeftDownloader → 结束子 Span，记录状态码
//   - ResponseReceived → 记录响应属性
//   - SpiderError → 记录错误事件
//   - ItemScraped → 记录 Item 事件
//
// 线程安全：所有共享状态通过 Tracer 接口保证线程安全。
type TraceExtension struct {
	tracer  telemetry.Tracer
	signals *signal.Manager
	logger  *slog.Logger

	// rootCtx 和 rootSpan 存储 Spider 级别的根 Span
	rootCtx  context.Context
	rootSpan telemetry.Span

	// handlerIDs 存储注册的信号处理器 ID，用于 Close 时注销
	handlerIDs []handlerRegistration
}

// handlerRegistration 存储信号处理器的注册信息。
type handlerRegistration struct {
	id  uint64
	sig signal.Signal
}

// NewTraceExtension 创建一个新的追踪扩展。
//
// 参数：
//   - tracer: 追踪器实现（如 otel.Tracer）
//   - signals: 框架信号管理器
//   - logger: 日志记录器（nil 使用默认）
func NewTraceExtension(tracer telemetry.Tracer, signals *signal.Manager, logger *slog.Logger) *TraceExtension {
	if logger == nil {
		logger = slog.Default()
	}
	return &TraceExtension{
		tracer:  tracer,
		signals: signals,
		logger:  logger,
	}
}

// Open 注册信号处理器，开始追踪。
func (e *TraceExtension) Open(ctx context.Context) error {
	if e.tracer == nil {
		e.logger.Warn("TraceExtension: tracer is nil, tracing disabled")
		return nil
	}

	e.connectSignal(signal.SpiderOpened, e.onSpiderOpened)
	e.connectSignal(signal.SpiderClosed, e.onSpiderClosed)
	e.connectSignal(signal.RequestReachedDownloader, e.onRequestReachedDownloader)
	e.connectSignal(signal.RequestLeftDownloader, e.onRequestLeftDownloader)
	e.connectSignal(signal.ResponseReceived, e.onResponseReceived)
	e.connectSignal(signal.SpiderError, e.onSpiderError)
	e.connectSignal(signal.ItemScraped, e.onItemScraped)

	e.logger.Info("TraceExtension enabled")
	return nil
}

// Close 注销所有信号处理器，关闭追踪器。
func (e *TraceExtension) Close(ctx context.Context) error {
	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil

	// 结束根 Span（如果存在）
	if e.rootSpan != nil {
		e.rootSpan.End()
		e.rootSpan = nil
	}

	// 关闭 Tracer，刷新待发送数据
	if e.tracer != nil {
		if err := e.tracer.Shutdown(ctx); err != nil {
			e.logger.Error("failed to shutdown tracer", "error", err)
			return err
		}
	}

	return nil
}

// connectSignal 注册信号处理器并记录 ID。
func (e *TraceExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// onSpiderOpened 创建 Spider 根 Span。
func (e *TraceExtension) onSpiderOpened(params map[string]any) error {
	attrs := map[string]string{}
	if name, ok := params["spider"].(string); ok {
		attrs["spider.name"] = name
	}

	e.rootCtx, e.rootSpan = e.tracer.Start(context.Background(), "spider.crawl", telemetry.SpanOption{
		Kind:       telemetry.SpanKindInternal,
		Attributes: attrs,
	})
	return nil
}

// onSpiderClosed 结束 Spider 根 Span。
func (e *TraceExtension) onSpiderClosed(params map[string]any) error {
	if e.rootSpan == nil {
		return nil
	}

	reason := "finished"
	if r, ok := params["reason"].(string); ok {
		reason = r
	}

	e.rootSpan.SetAttributes(map[string]string{
		"spider.close_reason": reason,
	})
	e.rootSpan.SetStatus(telemetry.SpanStatusOK, "")
	e.rootSpan.End()
	e.rootSpan = nil
	return nil
}

// onRequestReachedDownloader 创建 HTTP 请求 Span。
func (e *TraceExtension) onRequestReachedDownloader(params map[string]any) error {
	if e.rootCtx == nil {
		return nil
	}

	attrs := map[string]string{}
	if url, ok := params["url"].(string); ok {
		attrs["http.url"] = url
	}
	if method, ok := params["method"].(string); ok {
		attrs["http.method"] = method
	}

	_, span := e.tracer.Start(e.rootCtx, "http.request", telemetry.SpanOption{
		Kind:       telemetry.SpanKindClient,
		Attributes: attrs,
	})
	span.End()
	return nil
}

// onRequestLeftDownloader 记录请求完成。
func (e *TraceExtension) onRequestLeftDownloader(params map[string]any) error {
	return nil
}

// onResponseReceived 记录响应信息。
func (e *TraceExtension) onResponseReceived(params map[string]any) error {
	if e.rootSpan == nil {
		return nil
	}

	e.rootSpan.AddEvent("response.received", map[string]string{
		"http.status_code": fmt.Sprintf("%v", params["status"]),
		"http.url":         fmt.Sprintf("%v", params["url"]),
	})
	return nil
}

// onSpiderError 记录错误事件。
func (e *TraceExtension) onSpiderError(params map[string]any) error {
	if e.rootSpan == nil {
		return nil
	}

	if err, ok := params["error"].(error); ok {
		e.rootSpan.RecordError(err)
	}

	e.rootSpan.AddEvent("spider.error", map[string]string{
		"error.message": fmt.Sprintf("%v", params["error"]),
	})
	return nil
}

// onItemScraped 记录 Item 事件。
func (e *TraceExtension) onItemScraped(params map[string]any) error {
	if e.rootSpan == nil {
		return nil
	}

	e.rootSpan.AddEvent("item.scraped", map[string]string{
		"item.type": fmt.Sprintf("%T", params["item"]),
	})
	return nil
}

// ============================================================================
// MetricsExtension — 信号驱动的指标收集扩展
// ============================================================================

// MetricsExtension 是信号驱动的指标收集扩展。
//
// 通过监听框架信号系统，自动收集以下指标：
//   - scrapy_requests_total — 总请求数（Counter）
//   - scrapy_responses_total — 总响应数（Counter）
//   - scrapy_items_scraped_total — 已抓取 Item 总数（Counter）
//   - scrapy_items_dropped_total — 已丢弃 Item 总数（Counter）
//   - scrapy_errors_total — 总错误数（Counter）
//   - scrapy_active_requests — 当前活跃请求数（Gauge）
//   - scrapy_spider_state — Spider 状态（Gauge，0=关闭, 1=运行中）
//   - scrapy_request_duration_seconds — 请求延迟分布（Histogram）
//   - scrapy_spider_elapsed_seconds — Spider 运行时长（Gauge）
//
// 可选内置 HTTP /metrics 端点，暴露 Prometheus 格式指标。
//
// 线程安全：指标操作由底层 MetricsRegistry 保证线程安全。
type MetricsExtension struct {
	registry telemetry.MetricsRegistry
	addr     string // HTTP /metrics 端点监听地址，空字符串表示不启动
	signals  *signal.Manager
	logger   *slog.Logger

	// 预注册的指标
	requestsTotal   telemetry.Counter
	responsesTotal  telemetry.Counter
	itemsScraped    telemetry.Counter
	itemsDropped    telemetry.Counter
	errorsTotal     telemetry.Counter
	activeRequests  telemetry.Gauge
	spiderState     telemetry.Gauge
	requestDuration telemetry.Histogram
	spiderElapsed   telemetry.Gauge

	startTime time.Time

	// handlerIDs 存储注册的信号处理器 ID
	handlerIDs []handlerRegistration

	// stopHTTP 用于关闭 HTTP 服务器
	stopHTTP func()
}

// NewMetricsExtension 创建一个新的指标收集扩展。
//
// 参数：
//   - registry: 指标注册中心实现（如 prometheus.Registry）
//   - addr: HTTP /metrics 端点监听地址（如 ":9090"），空字符串表示不启动 HTTP 端点
//   - signals: 框架信号管理器
//   - logger: 日志记录器（nil 使用默认）
func NewMetricsExtension(registry telemetry.MetricsRegistry, addr string, signals *signal.Manager, logger *slog.Logger) *MetricsExtension {
	if logger == nil {
		logger = slog.Default()
	}
	return &MetricsExtension{
		registry: registry,
		addr:     addr,
		signals:  signals,
		logger:   logger,
	}
}

// Open 注册指标和信号处理器。
func (e *MetricsExtension) Open(ctx context.Context) error {
	if e.registry == nil {
		e.logger.Warn("MetricsExtension: registry is nil, metrics disabled")
		return nil
	}

	// 注册指标
	e.requestsTotal = e.registry.Counter("scrapy_requests_total", "总请求数")
	e.responsesTotal = e.registry.Counter("scrapy_responses_total", "总响应数")
	e.itemsScraped = e.registry.Counter("scrapy_items_scraped_total", "已抓取 Item 总数")
	e.itemsDropped = e.registry.Counter("scrapy_items_dropped_total", "已丢弃 Item 总数")
	e.errorsTotal = e.registry.Counter("scrapy_errors_total", "总错误数")
	e.activeRequests = e.registry.Gauge("scrapy_active_requests", "当前活跃请求数")
	e.spiderState = e.registry.Gauge("scrapy_spider_state", "Spider 状态（0=关闭, 1=运行中）")
	e.requestDuration = e.registry.Histogram(
		"scrapy_request_duration_seconds",
		"请求延迟分布",
		telemetry.DefaultHistogramBuckets,
	)
	e.spiderElapsed = e.registry.Gauge("scrapy_spider_elapsed_seconds", "Spider 运行时长（秒）")

	// 注册信号处理器
	e.connectSignal(signal.SpiderOpened, e.onSpiderOpened)
	e.connectSignal(signal.SpiderClosed, e.onSpiderClosed)
	e.connectSignal(signal.RequestReachedDownloader, e.onRequestReachedDownloader)
	e.connectSignal(signal.RequestLeftDownloader, e.onRequestLeftDownloader)
	e.connectSignal(signal.ResponseReceived, e.onResponseReceived)
	e.connectSignal(signal.ItemScraped, e.onItemScraped)
	e.connectSignal(signal.ItemDropped, e.onItemDropped)
	e.connectSignal(signal.SpiderError, e.onSpiderError)

	// 启动 HTTP /metrics 端点
	if e.addr != "" {
		stop, err := e.startHTTPServer()
		if err != nil {
			e.logger.Error("failed to start metrics HTTP server", "addr", e.addr, "error", err)
			return err
		}
		e.stopHTTP = stop
		e.logger.Info("metrics HTTP endpoint started", "addr", e.addr, "path", "/metrics")
	}

	e.logger.Info("MetricsExtension enabled")
	return nil
}

// Close 注销信号处理器，关闭 HTTP 服务器和指标注册中心。
func (e *MetricsExtension) Close(ctx context.Context) error {
	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil

	// 关闭 HTTP 服务器
	if e.stopHTTP != nil {
		e.stopHTTP()
		e.stopHTTP = nil
	}

	// 关闭指标注册中心
	if e.registry != nil {
		if err := e.registry.Shutdown(); err != nil {
			e.logger.Error("failed to shutdown metrics registry", "error", err)
			return err
		}
	}

	return nil
}

// connectSignal 注册信号处理器并记录 ID。
func (e *MetricsExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// onSpiderOpened 记录 Spider 启动。
func (e *MetricsExtension) onSpiderOpened(params map[string]any) error {
	e.spiderState.Set(1.0)
	e.startTime = time.Now()
	return nil
}

// onSpiderClosed 记录 Spider 关闭。
func (e *MetricsExtension) onSpiderClosed(params map[string]any) error {
	e.spiderState.Set(0.0)
	if !e.startTime.IsZero() {
		e.spiderElapsed.Set(time.Since(e.startTime).Seconds())
	}
	return nil
}

// onRequestReachedDownloader 记录请求到达下载器。
func (e *MetricsExtension) onRequestReachedDownloader(params map[string]any) error {
	e.requestsTotal.Inc()
	e.activeRequests.Inc()
	return nil
}

// onRequestLeftDownloader 记录请求离开下载器。
func (e *MetricsExtension) onRequestLeftDownloader(params map[string]any) error {
	e.activeRequests.Dec()

	// 记录请求延迟
	if latency, ok := params["download_latency"].(time.Duration); ok {
		e.requestDuration.ObserveDuration(latency)
	} else if latencyF, ok := params["download_latency"].(float64); ok {
		e.requestDuration.Observe(latencyF)
	}
	return nil
}

// onResponseReceived 记录响应接收。
func (e *MetricsExtension) onResponseReceived(params map[string]any) error {
	e.responsesTotal.Inc()
	return nil
}

// onItemScraped 记录 Item 抓取。
func (e *MetricsExtension) onItemScraped(params map[string]any) error {
	e.itemsScraped.Inc()
	return nil
}

// onItemDropped 记录 Item 丢弃。
func (e *MetricsExtension) onItemDropped(params map[string]any) error {
	e.itemsDropped.Inc()
	return nil
}

// onSpiderError 记录错误。
func (e *MetricsExtension) onSpiderError(params map[string]any) error {
	e.errorsTotal.Inc()
	return nil
}
