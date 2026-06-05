package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
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
//   - SpiderOpened → 创建根 Span "spider.crawl"，生成 sessionID
//   - SpiderClosed → 结束根 Span
//   - Engine.downloadAndScrape 中通过 TraceContextInjector 接口：
//   - BeforeScrape → 创建 "scrape:{callback}" Span（parent 从 _trace_parent Meta 恢复）
//   - InjectContext → 为新请求注入当前 Span 的 trace context + session ID
//   - AfterScrape → 结束 scrape Span
//   - RequestReachedDownloader → 在 scrape Span 上记录 http.download Event
//     （或当 traceHTTPDownload 为 true 时，创建独立 "http.request" 子 Span）
//   - RequestLeftDownloader → 在 scrape Span 上记录 http.response Event
//     （或当 traceHTTPDownload 为 true 时，结束 "http.request" Span）
//   - SpiderError → 在根 Span 上记录错误事件
//   - ItemScraped → 在根 Span 上记录 Item 事件
//
// Session 策略：
//   - PropagateWithinSession（默认）：仅在同一 session 内延续 Trace，
//     断点续爬重启后开启新 Trace，避免僵尸链路
//   - PropagateAlways：始终延续 Trace（适合分布式实时爬取）
//   - PropagateNever：不传播 Trace（每个回调独立 Trace，调试用）
//
// 同时实现 telemetry.TraceContextInjector 接口，由 Engine 调用。
//
// 线程安全：scrapeSpans/itemSpans/httpRequestSpans 通过 sync.Map 管理，保证并发安全。
type TraceExtension struct {
	tracer  telemetry.Tracer
	signals *signal.Manager
	logger  *slog.Logger

	// 配置选项
	callbackRegistry  *scrapyhttp.CallbackRegistry
	maxActiveSpans    int
	traceItemPipeline bool                             // 是否为 Item Pipeline 处理创建独立子 Span（默认 true）
	traceHTTPDownload bool                             // 是否为 HTTP 下载创建独立 "http.request" 子 Span（默认 false）
	policy            telemetry.TracePropagationPolicy // Trace Context 传播策略

	// sessionID 是当前 Spider 运行的唯一标识。
	// 在 onSpiderOpened 中生成，用于 PropagateWithinSession 策略下
	// 区分本次运行与历史运行（断点续爬恢复的旧请求）。
	sessionID string

	// rootCtx 和 rootSpan 存储 Spider 级别的根 Span
	rootCtx  context.Context
	rootSpan telemetry.Span

	// scrapeSpans 按 scrapeID 关联活跃的 scrape Span。
	// key: uint64 (scrapeID)
	// value: *scrapeSpanEntry
	scrapeSpans sync.Map

	// itemSpans 按 itemSpanID 关联活跃的 item.pipeline Span。
	// key: uint64 (itemSpanID)
	// value: *itemSpanEntry
	itemSpans sync.Map

	// httpRequestSpans 按 *Request 指针关联活跃的 http.request Span（仅在 traceHTTPDownload 为 true 时使用）。
	// key: *scrapyhttp.Request
	// value: telemetry.Span
	httpRequestSpans sync.Map

	// requestScrapeID 按 Request 指针关联 scrapeID，
	// 用于信号处理器中查找对应的 scrape Span。
	// key: *scrapyhttp.Request
	// value: uint64 (scrapeID)
	requestScrapeID sync.Map

	// nextScrapeID 是 scrapeID 的原子递增计数器
	nextScrapeID atomic.Uint64

	// nextItemSpanID 是 itemSpanID 的原子递增计数器
	nextItemSpanID atomic.Uint64

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

// itemSpanEntry 存储一个 item.pipeline 操作的追踪上下文。
type itemSpanEntry struct {
	ctx  context.Context // 包含当前 Span 的 context
	span telemetry.Span  // item.pipeline Span
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

// WithTraceItemPipeline 设置是否为 Item Pipeline 处理创建独立子 Span。
//
// 默认为 true。当启用时，每个 Item 的 Pipeline 处理会创建一个
// "item.pipeline" 子 Span（parent 为对应的 scrape Span），精确记录
// Pipeline 处理耗时和结果（成功/丢弃/错误）。
//
// 设为 false 时，Item Pipeline 不创建独立 Span，仅在根 Span 上记录 Event。
func WithTraceItemPipeline(enable bool) TraceExtensionOption {
	return func(e *TraceExtension) {
		e.traceItemPipeline = enable
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

// WithPropagationPolicy 设置 Trace Context 传播策略。
//
// 默认为 PropagateWithinSession（推荐），断点续爬重启后开启新 Trace，
// 避免产生跨越数小时的僵尸 Trace。
//
// 可选值：
//   - PropagateWithinSession：仅在同一 session 内延续 Trace（默认）
//   - PropagateAlways：始终延续 Trace（适合分布式实时爬取）
//   - PropagateNever：不传播 Trace（每个回调独立 Trace，调试用）
//
// 详见 telemetry.TracePropagationPolicy 文档。
func WithPropagationPolicy(policy telemetry.TracePropagationPolicy) TraceExtensionOption {
	return func(e *TraceExtension) {
		e.policy = policy
	}
}

// WithTraceHTTPDownload 设置是否为 HTTP 下载创建独立 "http.request" 子 Span。
//
// 默认为 false。在默认行为下，HTTP 下载仅在 scrape Span 上记录
// http.download / http.response Event，以提高信噪比、减少 Span 数量。
//
// 设为 true 时（恢复 v1 旧行为），每个 HTTP 请求会创建一个独立的 "http.request"
// 子 Span（parent 为对应的 scrape Span），便于在 Jaeger 中精确查看每个 HTTP
// 请求的耗时分布。注意：此模式下 Span 数量会显著增加（约 2 倍）。
func WithTraceHTTPDownload(enable bool) TraceExtensionOption {
	return func(e *TraceExtension) {
		e.traceHTTPDownload = enable
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
		tracer:            tracer,
		signals:           signals,
		logger:            logger,
		maxActiveSpans:    10000,
		traceItemPipeline: true,                             // 默认启用 Item Pipeline Span
		traceHTTPDownload: false,                            // 默认 HTTP 下载仅记录 Event
		policy:            telemetry.PropagateWithinSession, // 默认 session 内传播
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
	e.connectSignal(signal.ItemDropped, e.onItemDropped)
	e.connectSignal(signal.ItemError, e.onItemError)

	e.logger.Info("TraceExtension v2 enabled",
		"max_active_spans", e.maxActiveSpans,
		"trace_item_pipeline", e.traceItemPipeline,
		"trace_http_download", e.traceHTTPDownload,
		"propagation_policy", e.policy.String(),
	)
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

	// 结束所有未完成的活跃 item.pipeline Span
	e.itemSpans.Range(func(key, value any) bool {
		if entry, ok := value.(*itemSpanEntry); ok {
			entry.span.SetStatus(telemetry.SpanStatusError, "spider closed before item pipeline completed")
			entry.span.End()
		}
		e.itemSpans.Delete(key)
		return true
	})

	// 结束所有未完成的活跃 http.request Span（仅在 traceHTTPDownload 为 true 时存在）
	e.httpRequestSpans.Range(func(key, value any) bool {
		if span, ok := value.(telemetry.Span); ok {
			span.SetStatus(telemetry.SpanStatusError, "spider closed before http request completed")
			span.End()
		}
		e.httpRequestSpans.Delete(key)
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
//
// 同时根据当前传播策略注入 _trace_session：
//   - PropagateNever: 不注入任何 Trace Context
//   - PropagateAlways: 仅注入 _trace_parent
//   - PropagateWithinSession: 注入 _trace_parent + _trace_session（用于断点续爬区分）
func (e *TraceExtension) InjectContext(scrapeID uint64, newRequest any) {
	if scrapeID == 0 {
		return
	}

	// PropagateNever: 不注入任何 Trace Context
	if e.policy == telemetry.PropagateNever {
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

	// PropagateWithinSession: 同时注入 _trace_session 用于断点续爬区分
	if e.policy == telemetry.PropagateWithinSession && e.sessionID != "" {
		req.SetMeta(telemetry.MetaKeyTraceSession, e.sessionID)
	}
}

// BeforeItemPipeline 在 Item 进入 Pipeline 处理前调用，创建 item.pipeline 子 Span。
//
// 创建的 Span 以对应的 scrape Span 为 parent，形成完整的因果链：
//
//	scrape:{callback} → item.pipeline
//
// 如果 traceItemPipeline 为 false，返回 0（不创建 Item Span）。
func (e *TraceExtension) BeforeItemPipeline(scrapeID uint64) (itemSpanID uint64) {
	if e.tracer == nil || !e.traceItemPipeline {
		return 0
	}

	// 检查活跃 Span 数量限制
	if e.activeSpanCount.Load() >= int64(e.maxActiveSpans) {
		return 0
	}

	// 确定 parent context：优先使用对应的 scrape Span，否则回退到 rootCtx
	var parentCtx context.Context
	if scrapeID != 0 {
		if value, ok := e.scrapeSpans.Load(scrapeID); ok {
			entry := value.(*scrapeSpanEntry)
			parentCtx = entry.ctx
		}
	}
	if parentCtx == nil {
		if e.rootCtx != nil {
			parentCtx = e.rootCtx
		} else {
			parentCtx = context.Background()
		}
	}

	// 创建 item.pipeline Span
	itemCtx, itemSpan := e.tracer.Start(parentCtx, "item.pipeline", telemetry.SpanOption{
		Kind: telemetry.SpanKindInternal,
	})

	// 生成 itemSpanID 并存储
	itemSpanID = e.nextItemSpanID.Add(1)
	e.itemSpans.Store(itemSpanID, &itemSpanEntry{
		ctx:  itemCtx,
		span: itemSpan,
	})
	e.activeSpanCount.Add(1)

	return itemSpanID
}

// AfterItemPipeline 在 Item Pipeline 处理完成后调用，结束 item.pipeline Span。
//
// 根据 err 参数设置 Span 状态和 Event：
//   - err == nil: 状态 OK，记录 "pipeline.success" Event
//   - ErrDropItem: 状态 OK，记录 "pipeline.dropped" Event
//   - 其他 error: 状态 Error，记录 "pipeline.error" Event
func (e *TraceExtension) AfterItemPipeline(itemSpanID uint64, err error) {
	if itemSpanID == 0 {
		return
	}

	value, ok := e.itemSpans.LoadAndDelete(itemSpanID)
	if !ok {
		return
	}
	e.activeSpanCount.Add(-1)

	entry := value.(*itemSpanEntry)

	if err == nil {
		// Pipeline 处理成功
		entry.span.AddEvent("pipeline.success", nil)
		entry.span.SetStatus(telemetry.SpanStatusOK, "")
	} else if isDropItemError(err) {
		// Item 被丢弃（业务正常行为，不视为错误）
		entry.span.AddEvent("pipeline.dropped", map[string]string{
			"drop.reason": err.Error(),
		})
		entry.span.SetStatus(telemetry.SpanStatusOK, "item dropped")
	} else {
		// Pipeline 处理错误
		entry.span.RecordError(err)
		entry.span.AddEvent("pipeline.error", map[string]string{
			"error.message": err.Error(),
		})
		entry.span.SetStatus(telemetry.SpanStatusError, err.Error())
	}

	entry.span.End()
}

// isDropItemError 检查错误是否为 ErrDropItem。
func isDropItemError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, serrors.ErrDropItem)
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
//
// 根据传播策略决定是否使用 Meta 中的 traceparent：
//   - PropagateAlways: 始终使用（如果 traceparent 有效）
//   - PropagateWithinSession: 仅当 _trace_session 匹配当前 sessionID 时使用
//   - PropagateNever: 始终忽略，使用 rootCtx
//
// 当 Meta 中没有有效的 traceparent 或 session 不匹配时，回退到 rootCtx。
func (e *TraceExtension) resolveParentContext(req *scrapyhttp.Request) context.Context {
	if e.shouldPropagate(req) {
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
	}

	if e.rootCtx != nil {
		return e.rootCtx
	}
	return context.Background()
}

// shouldPropagate 根据传播策略判断是否应当从 Request.Meta 恢复 Trace Context。
//
// 决策规则：
//   - PropagateAlways: 始终返回 true
//   - PropagateNever: 始终返回 false
//   - PropagateWithinSession: 仅当 _trace_session 匹配当前 sessionID 时返回 true
//     （用于断点续爬场景下区分本次运行与历史运行）
func (e *TraceExtension) shouldPropagate(req *scrapyhttp.Request) bool {
	switch e.policy {
	case telemetry.PropagateAlways:
		return true
	case telemetry.PropagateNever:
		return false
	case telemetry.PropagateWithinSession:
		// 当前 sessionID 为空（如 SpiderOpened 尚未触发）时，保守起见不传播
		if e.sessionID == "" {
			return false
		}
		session, ok := req.GetMeta(telemetry.MetaKeyTraceSession)
		if !ok {
			// 旧请求或未通过 InjectContext 注入：不延续 Trace
			return false
		}
		s, ok := session.(string)
		if !ok {
			return false
		}
		return s == e.sessionID
	default:
		return false
	}
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

// onSpiderOpened 创建 Spider 根 Span，并生成本次运行的 sessionID。
func (e *TraceExtension) onSpiderOpened(params map[string]any) error {
	// 生成本次 Spider 运行的 sessionID（16 字节随机十六进制，共 32 字符）。
	// 使用 crypto/rand 保证全局唯一性，避免引入外部 UUID 依赖。
	e.sessionID = generateSessionID()

	attrs := map[string]string{
		"trace.session_id":         e.sessionID,
		"trace.propagation_policy": e.policy.String(),
	}
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

// generateSessionID 生成一个全局唯一的 session ID（16 字节随机十六进制）。
//
// 使用 crypto/rand 作为熵源，输出格式为 32 字符的小写十六进制字符串，
// 与 W3C TraceID 长度一致。在极小概率下熵源不可用时，回退到时间戳 + 计数器，
// 但不会返回空字符串以保证 PropagateWithinSession 策略始终可工作。
func generateSessionID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// 熵源不可用时的兜底：使用纳秒时间戳作为 fallback
		// 此分支在生产环境中几乎不会触发
		return fmt.Sprintf("%016x%016x", time.Now().UnixNano(), time.Now().Unix())
	}
	return hex.EncodeToString(buf[:])
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
// HTTP 下载默认不再创建独立 Span，降级为 Event 以提高信噪比。
//
// 当 traceHTTPDownload 为 true 时（v1 兼容模式），同时创建一个独立的
// "http.request" 子 Span，便于在追踪 UI 中精确查看每个 HTTP 请求耗时。
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

	// 记录 http.download Event（始终记录，作为轻量级标记）
	attrs := map[string]string{}
	if req.URL != nil {
		attrs["http.url"] = req.URL.String()
	}
	if req.Method != "" {
		attrs["http.method"] = req.Method
	}
	entry.span.AddEvent("http.download", attrs)

	// v1 兼容模式：创建独立 "http.request" 子 Span
	if e.traceHTTPDownload {
		// 检查活跃 Span 数量限制（避免溢出）
		if e.activeSpanCount.Load() >= int64(e.maxActiveSpans) {
			return nil
		}

		spanAttrs := map[string]string{
			"span.kind": "client",
		}
		if req.URL != nil {
			spanAttrs["http.url"] = req.URL.String()
		}
		if req.Method != "" {
			spanAttrs["http.method"] = req.Method
		}

		_, httpSpan := e.tracer.Start(entry.ctx, "http.request", telemetry.SpanOption{
			Kind:       telemetry.SpanKindClient,
			Attributes: spanAttrs,
		})
		e.httpRequestSpans.Store(req, httpSpan)
		e.activeSpanCount.Add(1)
	}

	return nil
}

// onRequestLeftDownloader 在对应的 scrape Span 上记录 http.response Event。
//
// 当 traceHTTPDownload 为 true 时（v1 兼容模式），同时结束对应的
// "http.request" 子 Span，并将 HTTP 状态码、错误等信息记录到该 Span。
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

	// v1 兼容模式：结束独立 "http.request" 子 Span
	if e.traceHTTPDownload {
		if spanValue, ok := e.httpRequestSpans.LoadAndDelete(req); ok {
			if httpSpan, ok := spanValue.(telemetry.Span); ok {
				httpSpan.SetAttributes(attrs)
				if errVal, ok := params["error"].(error); ok && errVal != nil {
					httpSpan.RecordError(errVal)
					httpSpan.SetStatus(telemetry.SpanStatusError, errVal.Error())
				} else {
					httpSpan.SetStatus(telemetry.SpanStatusOK, "")
				}
				httpSpan.End()
				e.activeSpanCount.Add(-1)
			}
		}
	}

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

// onItemScraped 在根 Span 上记录 Item 成功事件。
// 当启用了 Item Pipeline Span 时，详细的 Pipeline 处理信息已由
// AfterItemPipeline 记录在独立的 item.pipeline Span 中。
func (e *TraceExtension) onItemScraped(params map[string]any) error {
	if e.rootSpan == nil {
		return nil
	}

	e.rootSpan.AddEvent("item.scraped", map[string]string{
		"item.type": fmt.Sprintf("%T", params["item"]),
	})
	return nil
}

// onItemDropped 在根 Span 上记录 Item 丢弃事件。
func (e *TraceExtension) onItemDropped(params map[string]any) error {
	if e.rootSpan == nil {
		return nil
	}

	attrs := map[string]string{
		"item.type": fmt.Sprintf("%T", params["item"]),
	}
	if err, ok := params["error"].(error); ok {
		attrs["drop.reason"] = err.Error()
	}
	e.rootSpan.AddEvent("item.dropped", attrs)
	return nil
}

// onItemError 在根 Span 上记录 Item 错误事件。
func (e *TraceExtension) onItemError(params map[string]any) error {
	if e.rootSpan == nil {
		return nil
	}

	attrs := map[string]string{
		"item.type": fmt.Sprintf("%T", params["item"]),
	}
	if err, ok := params["error"].(error); ok {
		attrs["error.message"] = err.Error()
		e.rootSpan.RecordError(err)
	}
	e.rootSpan.AddEvent("item.error", attrs)
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
