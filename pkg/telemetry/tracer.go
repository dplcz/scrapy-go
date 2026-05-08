// Package telemetry 定义了 scrapy-go 框架的可观测性扩展点接口。
//
// 本包仅包含接口定义和零开销的空操作（Noop）实现，不引入任何第三方依赖。
// 具体的 OpenTelemetry / Prometheus 适配器由 contrib/telemetry 独立子模块提供。
//
// 设计原则：
//   - 接口最小化：仅暴露框架核心需要的追踪和指标能力
//   - 零开销默认：未配置后端时使用 NoopTracer/NoopMetricsRegistry，无运行时开销
//   - 可插拔：用户通过注入实现了接口的适配器来启用可观测性
//
// 使用方式：
//
//	// 默认零开销（无追踪）
//	tracer := telemetry.NewNoopTracer()
//
//	// 注入 OpenTelemetry 适配器（由 contrib/telemetry/otel 提供）
//	// tracer := oteltracer.New(tp)
package telemetry

import (
	"context"
	"time"
)

// SpanKind 表示 Span 的类型，用于区分不同角色的操作。
type SpanKind int

const (
	// SpanKindInternal 表示内部操作（默认）。
	SpanKindInternal SpanKind = iota

	// SpanKindClient 表示客户端发起的请求（如 HTTP 请求）。
	SpanKindClient

	// SpanKindServer 表示服务端处理的请求。
	SpanKindServer

	// SpanKindProducer 表示消息生产者。
	SpanKindProducer

	// SpanKindConsumer 表示消息消费者。
	SpanKindConsumer
)

// SpanStatus 表示 Span 的状态。
type SpanStatus int

const (
	// SpanStatusUnset 表示未设置状态。
	SpanStatusUnset SpanStatus = iota

	// SpanStatusOK 表示操作成功。
	SpanStatusOK

	// SpanStatusError 表示操作失败。
	SpanStatusError
)

// SpanContext 包含 Span 的标识信息，用于跨进程/跨组件传播追踪上下文。
type SpanContext struct {
	// TraceID 是追踪链路的全局唯一标识（通常为 16 字节 hex 编码）。
	TraceID string

	// SpanID 是当前 Span 的唯一标识（通常为 8 字节 hex 编码）。
	SpanID string

	// TraceFlags 包含追踪标志位（如采样决策）。
	TraceFlags byte

	// IsRemote 标识该 SpanContext 是否来自远程传播。
	IsRemote bool
}

// IsValid 检查 SpanContext 是否有效（TraceID 和 SpanID 均非空）。
func (sc SpanContext) IsValid() bool {
	return sc.TraceID != "" && sc.SpanID != ""
}

// SpanOption 用于配置 Span 创建时的选项。
type SpanOption struct {
	// Kind 指定 Span 类型。
	Kind SpanKind

	// Attributes 指定 Span 创建时的初始属性。
	Attributes map[string]string

	// StartTime 指定 Span 的开始时间（零值表示使用当前时间）。
	StartTime time.Time
}

// Tracer 定义追踪器接口。
//
// Tracer 负责创建 Span，每个 Span 代表一个操作的执行跟踪。
// 框架在关键路径（Spider 生命周期、HTTP 请求、Pipeline 处理）上调用 Tracer
// 创建 Span，用于记录操作的耗时、状态和上下文信息。
//
// 实现者需保证所有方法的线程安全性。
type Tracer interface {
	// Start 创建并启动一个新的 Span。
	//
	// 参数：
	//   - ctx: 父上下文，可能包含父 Span 信息
	//   - operationName: 操作名称（如 "spider.crawl"、"http.request"）
	//   - opts: 可选的 Span 配置
	//
	// 返回：
	//   - 包含新 Span 的 context（用于传播给子操作）
	//   - 新创建的 Span（调用方负责调用 End() 结束）
	Start(ctx context.Context, operationName string, opts ...SpanOption) (context.Context, Span)

	// Shutdown 关闭 Tracer，刷新所有待发送的追踪数据。
	// 应在应用退出前调用。
	Shutdown(ctx context.Context) error
}

// Span 定义追踪 Span 接口。
//
// Span 代表一个操作的执行跟踪，记录操作的开始时间、结束时间、
// 属性和状态。Span 创建后必须调用 End() 来结束。
//
// 实现者需保证所有方法的线程安全性。
type Span interface {
	// End 结束 Span，记录结束时间。
	// 调用 End 后不应再调用 Span 的其他方法。
	End()

	// SetAttributes 设置 Span 属性（键值对）。
	// 属性用于记录操作的上下文信息，如 HTTP 方法、URL、状态码等。
	SetAttributes(attrs map[string]string)

	// SetStatus 设置 Span 的状态。
	// 当操作失败时应设置为 SpanStatusError 并提供错误描述。
	SetStatus(status SpanStatus, description string)

	// RecordError 记录一个错误事件到 Span。
	// 不会自动设置 Span 状态为 Error，需要显式调用 SetStatus。
	RecordError(err error)

	// SpanContext 返回 Span 的上下文标识信息。
	SpanContext() SpanContext

	// AddEvent 添加一个事件到 Span。
	// 事件表示 Span 生命周期中的一个时间点事件。
	AddEvent(name string, attrs map[string]string)
}
