package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dplcz/scrapy-go/pkg/extension"
	scrapyhttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/telemetry"
)

// ============================================================================
// TraceExtension v2 — 以 Item 产出链路为核心的分布式追踪扩展
// ============================================================================

// TraceExtension 是以回调链因果关系为核心的分布式追踪扩展（v2）。
//
// 追踪模型：
//   - SpiderOpened → 创建根 Span "spider.crawl"
//   - SpiderClosed → 结束根 Span
//   - Engine.downloadAndScrape 中通过 TraceContextInjector 接口：
//   - BeforeScrape → 创建 "scrape:{callback}" Span（parent 从 _trace_parent Meta 恢复）
//   - InjectContext → 为新请求注入当前 Span 的 trace context
//   - AfterScrape → 结束 scrape Span
//   - RequestReachedDownloader → 在 scrape Span 上记录 http.download Event
//   - RequestLeftDownloader → 在 scrape Span 上记录 http.response Event
//   - SpiderError → 在根 Span 上记录错误事件
//   - ItemScraped → 在根 Span 上记录 Item 事件
//
// 同时实现 telemetry.TraceContextInjector 接口，由 Engine 调用。
//
// 线程安全：scrapeSpans 通过 sync.Map 管理，保证并发安全。
type TraceExtension struct {
	tracer  telemetry.Tracer
	signals *signal.Manager
	logger  *slog.Logger

	// 配置选项
	callbackRegistry *scrapyhttp.CallbackRegistry
	maxActiveSpans   int

	// rootCtx 和 rootSpan 存储 Spider 级别的根 Span
	rootCtx  context.Context
	rootSpan telemetry.Span

	// scrapeSpans 按 scrapeID 关联活跃的 scrape Span。
	// key: uint64 (scrapeID)
	// value: *scrapeSpanEntry
	scrapeSpans sync.Map

	// requestScrapeID 按 Request 指针关联 scrapeID，
	// 用于信号处理器中查找对应的 scrape Span。
	// key: *scrapyhttp.Request
	// value: uint64 (scrapeID)
	requestScrapeID sync.Map

	// nextScrapeID 是 scrapeID 的原子递增计数器
	nextScrapeID atomic.Uint64

	// activeSpanCount 追踪当前活跃 Span 数量，防止内存泄漏
	activeSpanCount atomic.Int64

	// handlerIDs 存储注册的信号处理器 ID，用于 Close 时注销
	handlerIDs []handlerRegistration
}

// scrapeSpanEntry 存储一个 scrape 操作的追踪上下文。
type scrapeSpanEntry struct {
	ctx  context.Context // 包含当前 Span 的 context
	span telemetry.Span  // scrape:{callback} Span
}

// handlerRegistration 存储信号处理器的注册信息。
type handlerRegistration struct {
	id  uint64
	sig signal.Signal
}

// TraceExtensionOption 是 TraceExtension 的配置选项函数。
type TraceExtensionOption func(*TraceExtension)

// WithCallbackRegistry 设置回调注册表，用于 Span 命名。
//
// 当设置了 CallbackRegistry 时，TraceExtension 能够将回调函数解析为
// 可读的方法名（如 "ParseDetail"），用于 Span 名称 "scrape:ParseDetail"。
// 未设置时，Span 名称默认为 "scrape:parse"（Spider.Parse）或 "scrape:anonymous"。
func WithCallbackRegistry(registry *scrapyhttp.CallbackRegistry) TraceExtensionOption {
	return func(e *TraceExtension) {
		e.callbackRegistry = registry
	}
}

// WithMaxActiveSpans 设置最大活跃 Span 数量，防止内存泄漏。
//
// 当活跃 Span 数量超过此限制时，新的 BeforeScrape 调用将返回 0（不追踪）。
// 默认值为 10000。
func WithMaxActiveSpans(n int) TraceExtensionOption {
	return func(e *TraceExtension) {
		if n > 0 {
			e.maxActiveSpans = n
		}
	}
}

// NewTraceExtension 创建一个新的追踪扩展（v2）。
//
// 参数：
//   - tracer: 追踪器实现（如 otel.Tracer）
//   - signals: 框架信号管理器
//   - logger: 日志记录器（nil 使用默认）
//   - opts: 可选配置
func NewTraceExtension(tracer telemetry.Tracer, signals *signal.Manager, logger *slog.Logger, opts ...TraceExtensionOption) *TraceExtension {
	if logger == nil {
		logger = slog.Default()
	}
	ext := &TraceExtension{
		tracer:         tracer,
		signals:        signals,
		logger:         logger,
		maxActiveSpans: 10000,
	}
	for _, opt := range opts {
		opt(ext)
	}
	return ext
}

// FromCrawler 实现 CrawlerAwareExtension 接口。
// 在 Open 之前调用，获取最新的 Signals 和 Logger 引用，
// 避免因 Crawler 重建组件导致引用失效。
func (e *TraceExtension) FromCrawler(c extension.Crawler) error {
	e.signals = c.GetSignals()
	e.logger = c.GetLogger()
	return nil
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
	e.connectSignal(signal.SpiderError, e.onSpiderError)
	e.connectSignal(signal.ItemScraped, e.onItemScraped)

	e.logger.Info("TraceExtension v2 enabled", "max_active_spans", e.maxActiveSpans)
	return nil
}

// Close 注销所有信号处理器，结束所有活跃 Span，关闭追踪器。
func (e *TraceExtension) Close(ctx context.Context) error {
	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil

	// 结束所有未完成的活跃 scrape Span
	e.scrapeSpans.Range(func(key, value any) bool {
		if entry, ok := value.(*scrapeSpanEntry); ok {
			entry.span.SetStatus(telemetry.SpanStatusError, "spider closed before scrape completed")
			entry.span.End()
		}
		e.scrapeSpans.Delete(key)
		return true
	})
	e.activeSpanCount.Store(0)

	// 清理 requestScrapeID 映射
	e.requestScrapeID.Range(func(key, _ any) bool {
		e.requestScrapeID.Delete(key)
		return true
	})

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

// ============================================================================
// TraceContextInjector 接口实现
// ============================================================================

// BeforeScrape 在回调执行前调用，创建 scrape Span。
//
// 从 request.Meta["_trace_parent"] 恢复父上下文，
// 通过 CallbackRegistry 解析回调名称用于 Span 命名。
func (e *TraceExtension) BeforeScrape(request any) (scrapeID uint64) {
	if e.tracer == nil {
		return 0
	}

	// 检查活跃 Span 数量限制
	if e.activeSpanCount.Load() >= int64(e.maxActiveSpans) {
		e.logger.Warn("TraceExtension: max active spans reached, skipping trace",
			"max", e.maxActiveSpans,
		)
		return 0
	}

	req, ok := request.(*scrapyhttp.Request)
	if !ok || req == nil {
		return 0
	}

	// 确定父上下文
	parentCtx := e.resolveParentContext(req)

	// 解析回调名称
	callbackName := e.resolveCallbackName(req)

	// 创建 scrape Span
	attrs := map[string]string{
		"scrape.callback": callbackName,
	}
	if req.URL != nil {
		attrs["scrape.url"] = req.URL.String()
	}

	spanCtx, span := e.tracer.Start(parentCtx, "scrape:"+callbackName, telemetry.SpanOption{
		Kind:       telemetry.SpanKindInternal,
		Attributes: attrs,
	})

	// 生成 scrapeID 并存储
	scrapeID = e.nextScrapeID.Add(1)
	e.scrapeSpans.Store(scrapeID, &scrapeSpanEntry{
		ctx:  spanCtx,
		span: span,
	})
	e.activeSpanCount.Add(1)

	// 建立 request → scrapeID 映射（供信号处理器使用）
	e.requestScrapeID.Store(req, scrapeID)

	return scrapeID
}

// AfterScrape 在回调执行后调用，结束 scrape Span。
func (e *TraceExtension) AfterScrape(scrapeID uint64, yielded telemetry.ScrapeYield, err error) {
	if scrapeID == 0 {
		return
	}

	value, ok := e.scrapeSpans.LoadAndDelete(scrapeID)
	if !ok {
		return
	}
	e.activeSpanCount.Add(-1)

	entry := value.(*scrapeSpanEntry)

	// 记录产出统计
	entry.span.SetAttributes(map[string]string{
		"scrape.yielded_requests": fmt.Sprintf("%d", yielded.Requests),
		"scrape.yielded_items":    fmt.Sprintf("%d", yielded.Items),
	})

	if err != nil {
		entry.span.RecordError(err)
		entry.span.SetStatus(telemetry.SpanStatusError, err.Error())
	} else {
		entry.span.SetStatus(telemetry.SpanStatusOK, "")
	}

	entry.span.End()
}

// InjectContext 为新产出的请求注入当前 scrape Span 的 trace context。
func (e *TraceExtension) InjectContext(scrapeID uint64, newRequest any) {
	if scrapeID == 0 {
		return
	}

	req, ok := newRequest.(*scrapyhttp.Request)
	if !ok || req == nil {
		return
	}

	value, ok := e.scrapeSpans.Load(scrapeID)
	if !ok {
		return
	}

	entry := value.(*scrapeSpanEntry)

	// 将当前 Span 的 SpanContext 编码为 traceparent 字符串
	traceparent := telemetry.FormatTraceparent(entry.span.SpanContext())
	if traceparent == "" {
		return
	}

	req.SetMeta(telemetry.MetaKeyTraceparent, traceparent)
}

// ============================================================================
// 内部方法
// ============================================================================

// connectSignal 注册信号处理器并记录 ID。
func (e *TraceExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// resolveParentContext 从请求的 _trace_parent Meta 恢复父上下文。
// 如果 Meta 中没有有效的 traceparent，回退到 rootCtx。
func (e *TraceExtension) resolveParentContext(req *scrapyhttp.Request) context.Context {
	if traceparent, ok := req.GetMeta(telemetry.MetaKeyTraceparent); ok {
		if tp, ok := traceparent.(string); ok && tp != "" {
			sc := telemetry.ParseTraceparent(tp)
			if sc.IsValid() {
				// 使用 Tracer 的 ContextWithRemoteSpanContext 注入远程 SpanContext
				baseCtx := e.rootCtx
				if baseCtx == nil {
					baseCtx = context.Background()
				}
				return e.tracer.ContextWithRemoteSpanContext(baseCtx, sc)
			}
		}
	}

	if e.rootCtx != nil {
		return e.rootCtx
	}
	return context.Background()
}

// resolveCallbackName 解析请求的回调函数名称。
func (e *TraceExtension) resolveCallbackName(req *scrapyhttp.Request) string {
	if req.Callback == nil {
		return "Parse" // Spider.Parse 默认回调
	}
	if scrapyhttp.IsNoCallback(req.Callback) {
		return "NoCallback"
	}
	if e.callbackRegistry != nil {
		if name, ok := e.callbackRegistry.LookupByFunc(req.Callback); ok {
			return name
		}
	}
	return "anonymous"
}

// ============================================================================
// 信号处理器
// ============================================================================

// onSpiderOpened 创建 Spider 根 Span。
func (e *TraceExtension) onSpiderOpened(params map[string]any) error {
	attrs := map[string]string{}
	if sp, ok := params["spider"].(spider.Spider); ok {
		attrs["spider.name"] = sp.Name()
	} else if name, ok := params["spider"].(string); ok {
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

// onRequestReachedDownloader 在对应的 scrape Span 上记录 http.download Event。
// HTTP 下载不再创建独立 Span，降级为 Event 以提高信噪比。
func (e *TraceExtension) onRequestReachedDownloader(params map[string]any) error {
	req, _ := params["request"].(*scrapyhttp.Request)
	if req == nil {
		return nil
	}

	// 查找对应的 scrapeID
	idValue, ok := e.requestScrapeID.Load(req)
	if !ok {
		return nil
	}
	scrapeID := idValue.(uint64)

	// 查找对应的 scrape Span
	value, ok := e.scrapeSpans.Load(scrapeID)
	if !ok {
		return nil
	}
	entry := value.(*scrapeSpanEntry)

	// 记录 http.download Event
	attrs := map[string]string{}
	if req.URL != nil {
		attrs["http.url"] = req.URL.String()
	}
	if req.Method != "" {
		attrs["http.method"] = req.Method
	}
	entry.span.AddEvent("http.download", attrs)
	return nil
}

// onRequestLeftDownloader 在对应的 scrape Span 上记录 http.response Event。
func (e *TraceExtension) onRequestLeftDownloader(params map[string]any) error {
	req, _ := params["request"].(*scrapyhttp.Request)
	if req == nil {
		return nil
	}

	// 查找对应的 scrapeID（不删除，因为 scrape 还未结束）
	idValue, ok := e.requestScrapeID.Load(req)
	if !ok {
		return nil
	}
	scrapeID := idValue.(uint64)

	// 查找对应的 scrape Span
	value, ok := e.scrapeSpans.Load(scrapeID)
	if !ok {
		return nil
	}
	entry := value.(*scrapeSpanEntry)

	// 记录 http.response Event
	attrs := map[string]string{}
	if status, ok := params["status"].(int); ok {
		attrs["http.status_code"] = fmt.Sprintf("%d", status)
	}
	if latency, ok := req.GetMeta("download_latency"); ok {
		if d, ok := latency.(time.Duration); ok {
			attrs["http.duration_ms"] = fmt.Sprintf("%d", d.Milliseconds())
		}
	}
	if err, ok := params["error"].(error); ok {
		attrs["http.error"] = err.Error()
	}
	entry.span.AddEvent("http.response", attrs)

	// 下载完成后清理 requestScrapeID 映射
	// 注意：AfterScrape 会在回调执行完毕后由 Engine 调用
	e.requestScrapeID.Delete(req)

	return nil
}

// onSpiderError 在根 Span 上记录错误事件。
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

// onItemScraped 在根 Span 上记录 Item 事件。
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
// 编译期接口实现检查
// ============================================================================

var (
	_ telemetry.TraceContextInjector  = (*TraceExtension)(nil)
	_ extension.Extension             = (*TraceExtension)(nil)
	_ extension.CrawlerAwareExtension = (*TraceExtension)(nil)
)

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

// FromCrawler 实现 CrawlerAwareExtension 接口。
// 在 Open 之前调用，获取最新的 Signals 和 Logger 引用，
// 避免因 Crawler 重建组件导致引用失效。
func (e *MetricsExtension) FromCrawler(c extension.Crawler) error {
	e.signals = c.GetSignals()
	e.logger = c.GetLogger()
	return nil
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
