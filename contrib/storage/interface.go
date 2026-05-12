package storage

import (
	"context"
)

// StorageWriter 定义通用的存储写入接口。
//
// 所有持久化存储适配器（MongoDB、PostgreSQL、Elasticsearch 等）
// 均实现此接口，提供统一的批量写入和资源管理语义。
//
// 设计原则：
//   - 批量写入：减少网络往返，提升吞吐量
//   - Upsert 语义：支持基于唯一键的插入或更新
//   - 资源管理：通过 Connect/Close 管理连接生命周期
type StorageWriter interface {
	// Connect 建立与存储后端的连接。
	// 应在 Pipeline.Open 中调用。
	Connect(ctx context.Context) error

	// Close 关闭与存储后端的连接并释放资源。
	// 应在 Pipeline.Close 中调用。
	Close(ctx context.Context) error

	// WriteBatch 批量写入多条记录。
	// items 中每个元素为 map[string]any 格式的字段映射。
	// 返回成功写入的记录数和可能的错误。
	WriteBatch(ctx context.Context, items []map[string]any) (int, error)
}

// UpsertWriter 扩展 StorageWriter，支持基于唯一键的 Upsert 操作。
//
// 当存储后端支持 Upsert 语义时（如 MongoDB 的 UpdateOne with upsert、
// PostgreSQL 的 INSERT ... ON CONFLICT ... DO UPDATE），
// 适配器可同时实现此接口。
type UpsertWriter interface {
	StorageWriter

	// UpsertBatch 批量执行 Upsert 操作。
	// uniqueKey 指定用于判断记录唯一性的字段名。
	// items 中每个元素为 map[string]any 格式的字段映射。
	// 返回受影响的记录数和可能的错误。
	UpsertBatch(ctx context.Context, uniqueKey string, items []map[string]any) (int, error)
}

// ItemConverter 定义 Item 到 map 的转换函数类型。
//
// 用户可提供自定义转换函数，控制 Item 如何序列化为存储记录。
// 默认使用 item.Adapt(item).AsMap() 进行转换。
type ItemConverter func(item any) (map[string]any, error)
