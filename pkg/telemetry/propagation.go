// Package telemetry 定义了 scrapy-go 框架的可观测性扩展点接口。
package telemetry

import "fmt"

// ============================================================================
// Trace Context 传播相关常量和工具函数
// ============================================================================

const (
	// MetaKeyTraceparent 存储 W3C traceparent 格式字符串。
	// 格式: "00-{traceID}-{spanID}-{flags}"
	// 用于在 Request.Meta 中传播追踪上下文，实现回调链因果关系追踪。
	MetaKeyTraceparent = "_trace_parent"

	// MetaKeyTraceSession 存储当前爬取 session 的唯一标识。
	// 用于断点续爬时判断是否延续旧 Trace。
	MetaKeyTraceSession = "_trace_session"

	// traceparentVersion 是 W3C traceparent 的版本号。
	traceparentVersion = "00"
)

// FormatTraceparent 将 SpanContext 编码为 W3C traceparent 格式字符串。
//
// 格式: "00-{traceID}-{spanID}-{flags}"
//
// 如果 SpanContext 无效（TraceID 或 SpanID 为空），返回空字符串。
func FormatTraceparent(sc SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s-%02x", traceparentVersion, sc.TraceID, sc.SpanID, sc.TraceFlags)
}

// ParseTraceparent 从 W3C traceparent 格式字符串中解析出 SpanContext。
//
// 格式: "00-{traceID}-{spanID}-{flags}"
//
// 如果字符串格式无效，返回零值 SpanContext（IsValid() == false）。
func ParseTraceparent(traceparent string) SpanContext {
	if traceparent == "" {
		return SpanContext{}
	}

	var version, traceID, spanID string
	var flags byte

	// 使用 fmt.Sscanf 解析固定格式
	n, err := fmt.Sscanf(traceparent, "%2s-%32s-%16s-%2x", &version, &traceID, &spanID, &flags)
	if err != nil || n != 4 {
		return SpanContext{}
	}

	// 验证版本号
	if version != traceparentVersion {
		return SpanContext{}
	}

	return SpanContext{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		IsRemote:   true, // 从字符串恢复的 context 标记为远程
	}
}

// ============================================================================
// TracePropagationPolicy — Trace Context 传播策略
// ============================================================================

// TracePropagationPolicy 控制 Trace Context 在请求链中的传播行为。
//
// 设计目的：在断点续爬场景下，磁盘队列中可能存在数小时甚至数天前的请求，
// 其 _trace_parent 指向的 Trace 可能已在后端过期（Jaeger 默认 TTL 7 天）。
// 强行延续会产生"僵尸 Trace"。通过 Session ID 机制区分本次运行与历史运行，
// 可以避免此问题。
type TracePropagationPolicy int

const (
	// PropagateWithinSession 仅在同一 session 内延续 Trace（默认，推荐）。
	//
	// 工作方式：
	//   - Engine 启动时生成一个 sessionID（16 字节随机十六进制）
	//   - 注入 trace context 时同时写入 _trace_session = sessionID
	//   - 恢复 trace context 时检查 _trace_session 是否匹配当前 sessionID
	//   - 不匹配（断点续爬恢复的旧请求） → 忽略旧 traceparent，创建新的根 Span
	//
	// 适用场景：单机部署、断点续爬，避免僵尸 Trace。
	PropagateWithinSession TracePropagationPolicy = iota

	// PropagateAlways 始终延续 Trace，忽略 session 检查。
	//
	// 适用场景：分布式实时爬取（多 Worker 共享 Redis 队列），
	// 需要跨进程保留完整调用链。
	PropagateAlways

	// PropagateNever 不传播 Trace Context，每个回调创建独立的根 Span。
	//
	// 适用场景：调试模式，或仅关注单次回调耗时不关心因果链。
	PropagateNever
)

// String 实现 fmt.Stringer，便于日志记录。
func (p TracePropagationPolicy) String() string {
	switch p {
	case PropagateWithinSession:
		return "within_session"
	case PropagateAlways:
		return "always"
	case PropagateNever:
		return "never"
	default:
		return "unknown"
	}
}

// ============================================================================
// TraceContextInjector 接口 — Engine 层使用的轻量追踪注入接口
// ============================================================================

// TraceContextInjector 定义 Engine/Scraper 层的追踪上下文注入接口。
//
// Engine 通过此接口在 downloadAndScrape 流程中：
//   - 创建 scrape Span（基于请求的 _trace_parent 恢复父上下文）
//   - 为回调产出的新请求注入当前 Span 的 trace context
//   - 结束 scrape Span
//
// Scraper 通过此接口在 Item Pipeline 处理流程中：
//   - 创建 item.pipeline 子 Span（parent 为 scrape Span）
//   - 结束 item.pipeline Span（记录 Pipeline 处理结果）
//
// 此接口定义在 pkg/telemetry 中（不依赖 contrib），
// 由 contrib/telemetry 的 TraceExtension 实现并注入到 Engine 和 Scraper。
//
// 当未配置 TraceContextInjector 时（nil），所有追踪操作为空操作，零开销。
type TraceContextInjector interface {
	// BeforeScrape 在回调执行前调用，创建 scrape Span。
	//
	// 实现者应从 request 中提取 _trace_parent Meta 恢复父上下文，
	// 并通过内部持有的 CallbackRegistry 解析回调名称用于 Span 命名。
	//
	// 参数：
	//   - request: 当前正在处理的请求（*shttp.Request 类型，使用 any 避免循环依赖）
	//
	// 返回：
	//   - scrapeID: 本次 scrape 操作的唯一标识，用于后续 AfterScrape/InjectContext 关联
	//     返回 0 表示不追踪（如 tracer 为 nil）
	BeforeScrape(request any) (scrapeID uint64)

	// AfterScrape 在回调执行后调用，结束 scrape Span。
	//
	// 参数：
	//   - scrapeID: BeforeScrape 返回的标识
	//   - yielded: 回调产出的请求数和 Item 数（用于记录 Span 属性）
	//   - err: 回调执行的错误（nil 表示成功）
	AfterScrape(scrapeID uint64, yielded ScrapeYield, err error)

	// InjectContext 为新产出的请求注入当前 scrape Span 的 trace context。
	//
	// 将当前 scrape Span 的 SpanContext 编码为 traceparent 字符串，
	// 写入 newRequest.Meta["_trace_parent"] 和 Meta["_trace_session"]。
	//
	// 参数：
	//   - scrapeID: BeforeScrape 返回的标识（用于查找当前 Span）
	//   - newRequest: 回调产出的新请求（*shttp.Request 类型，使用 any 避免循环依赖）
	InjectContext(scrapeID uint64, newRequest any)

	// BeforeItemPipeline 在 Item 进入 Pipeline 处理前调用，创建 item.pipeline 子 Span。
	//
	// 创建的 Span 以对应的 scrape Span 为 parent，形成完整的因果链：
	//   scrape:{callback} → item.pipeline
	//
	// 参数：
	//   - scrapeID: BeforeScrape 返回的标识（用于确定 parent Span）
	//     如果 scrapeID 为 0 或对应的 scrape Span 已结束，使用 rootCtx 作为 parent
	//
	// 返回：
	//   - itemSpanID: Item Pipeline 操作的唯一标识，用于后续 AfterItemPipeline 关联
	//     返回 0 表示不追踪（如 tracer 为 nil 或 Item Pipeline 追踪已禁用）
	BeforeItemPipeline(scrapeID uint64) (itemSpanID uint64)

	// AfterItemPipeline 在 Item Pipeline 处理完成后调用，结束 item.pipeline Span。
	//
	// 根据 err 参数设置 Span 状态：
	//   - err == nil: 状态 OK，记录 "pipeline.success" Event
	//   - errors.Is(err, ErrDropItem): 状态 OK，记录 "pipeline.dropped" Event
	//   - 其他 error: 状态 Error，记录 "pipeline.error" Event
	//
	// 参数：
	//   - itemSpanID: BeforeItemPipeline 返回的标识
	//   - err: Pipeline 处理结果（nil 表示成功，ErrDropItem 表示丢弃，其他表示错误）
	AfterItemPipeline(itemSpanID uint64, err error)
}

// ScrapeYield 记录回调产出的统计信息。
type ScrapeYield struct {
	// Requests 是回调产出的新请求数量。
	Requests int

	// Items 是回调产出的 Item 数量。
	Items int
}
