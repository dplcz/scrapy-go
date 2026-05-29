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
// TraceContextInjector 接口 — Engine 层使用的轻量追踪注入接口
// ============================================================================

// TraceContextInjector 定义 Engine 层的追踪上下文注入接口。
//
// Engine 通过此接口在 downloadAndScrape 流程中：
//   - 创建 scrape Span（基于请求的 _trace_parent 恢复父上下文）
//   - 为回调产出的新请求注入当前 Span 的 trace context
//   - 结束 scrape Span
//
// 此接口定义在 pkg/telemetry 中（不依赖 contrib），
// 由 contrib/telemetry 的 TraceExtension 实现并注入到 Engine。
//
// 当 Engine 未配置 TraceContextInjector 时（nil），所有追踪操作为空操作，零开销。
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
}

// ScrapeYield 记录回调产出的统计信息。
type ScrapeYield struct {
	// Requests 是回调产出的新请求数量。
	Requests int

	// Items 是回调产出的 Item 数量。
	Items int
}
