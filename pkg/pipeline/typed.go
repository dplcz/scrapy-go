// Package pipeline 的泛型扩展：TypedPipeline[T]。
//
// TypedPipeline[T] 提供编译期类型约束的 Pipeline 包装器，
// 替代 Scrapy Python 版本中运行时类型检查的模式。
//
// # 设计动机
//
// Scrapy Python 的 Pipeline.process_item 接收 `item: Any` 参数，
// 需要在运行时通过 isinstance() 判断 Item 类型。
// Go 1.18+ 泛型允许在编译期约束 Item 类型，消除运行时类型断言开销。
//
// # 使用方式
//
//	type BookPipeline struct{}
//
//	func (p *BookPipeline) Open(ctx context.Context) error { return nil }
//	func (p *BookPipeline) Close(ctx context.Context) error { return nil }
//	func (p *BookPipeline) ProcessItem(ctx context.Context, item *Book) (*Book, error) {
//	    item.Title = strings.TrimSpace(item.Title)
//	    return item, nil
//	}
//
//	// 注册到 Manager：
//	typed := pipeline.NewTypedPipeline[*Book](&BookPipeline{})
//	manager.AddPipeline(typed, "BookPipeline", 300)
//
// TypedPipeline 内部通过类型断言将 any 转为 T，若类型不匹配则跳过处理。
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
)

// TypedItemPipeline 定义类型安全的 Pipeline 接口。
// 泛型参数 T 约束了 ProcessItem 接收和返回的 Item 类型。
//
// 对应 Scrapy 的 process_item(self, item, spider)，
// 但通过 Go 泛型在编译期保证类型安全。
type TypedItemPipeline[T any] interface {
	// Open 在 Spider 打开时调用，用于初始化资源。
	Open(ctx context.Context) error

	// Close 在 Spider 关闭时调用，用于释放资源。
	Close(ctx context.Context) error

	// ProcessItem 处理一个类型为 T 的 Item。
	// 返回处理后的 Item 和 error。
	// 返回 ErrDropItem 表示丢弃该 Item。
	ProcessItem(ctx context.Context, item T) (T, error)
}

// TypedPipeline 是 TypedItemPipeline[T] 到 ItemPipeline 的适配器。
// 它将类型安全的泛型 Pipeline 包装为框架通用的 ItemPipeline 接口，
// 使其可以注册到 Manager 中。
//
// 当 Item 类型不匹配时，TypedPipeline 会跳过处理（透传 Item），
// 不会返回错误。这允许多个 TypedPipeline 共存于同一 Manager 中，
// 各自只处理自己关心的 Item 类型。
type TypedPipeline[T any] struct {
	inner  TypedItemPipeline[T]
	logger *slog.Logger
}

// NewTypedPipeline 创建一个 TypedPipeline 适配器。
//
// inner 是用户实现的类型安全 Pipeline。
// opts 可选配置（如日志器）。
func NewTypedPipeline[T any](inner TypedItemPipeline[T], opts ...TypedPipelineOption) *TypedPipeline[T] {
	tp := &TypedPipeline[T]{
		inner:  inner,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(tp)
	}
	return tp
}

// TypedPipelineOption 是 TypedPipeline 的可选配置函数。
type TypedPipelineOption func(tp any)

// WithTypedLogger 设置 TypedPipeline 的日志器。
func WithTypedLogger(logger *slog.Logger) TypedPipelineOption {
	return func(tp any) {
		switch v := tp.(type) {
		case interface{ setLogger(*slog.Logger) }:
			v.setLogger(logger)
		}
	}
}

func (tp *TypedPipeline[T]) setLogger(logger *slog.Logger) {
	tp.logger = logger
}

// Open 实现 ItemPipeline.Open。
func (tp *TypedPipeline[T]) Open(ctx context.Context) error {
	return tp.inner.Open(ctx)
}

// Close 实现 ItemPipeline.Close。
func (tp *TypedPipeline[T]) Close(ctx context.Context) error {
	return tp.inner.Close(ctx)
}

// ProcessItem 实现 ItemPipeline.ProcessItem。
//
// 类型匹配策略：
//  1. 尝试将 item 直接断言为 T
//  2. 若类型不匹配，跳过处理（透传 item）
//
// 这种设计允许多个 TypedPipeline 共存，各自处理不同类型的 Item。
func (tp *TypedPipeline[T]) ProcessItem(ctx context.Context, item any) (any, error) {
	typedItem, ok := item.(T)
	if !ok {
		// 类型不匹配，跳过处理，透传 item
		tp.logger.Debug("TypedPipeline: item type mismatch, skipping",
			"expected", fmt.Sprintf("%T", *new(T)),
			"actual", fmt.Sprintf("%T", item),
		)
		return item, nil
	}

	result, err := tp.inner.ProcessItem(ctx, typedItem)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ============================================================================
// 编译期接口满足性检查
// ============================================================================

var _ ItemPipeline = (*TypedPipeline[any])(nil)
