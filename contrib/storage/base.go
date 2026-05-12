package storage

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/dplcz/scrapy-go/pkg/item"
)

// BasePipeline 提供通用的批量缓冲 Pipeline 基础实现。
//
// 所有存储适配器（Mongo/Postgres/Elasticsearch）均嵌入此结构体，
// 复用批量缓冲、Item 转换、刷新逻辑。
//
// 线程安全：通过 sync.Mutex 保护内部缓冲区，支持 CONCURRENT_ITEMS 并发调用。
type BasePipeline struct {
	writer    StorageWriter
	converter ItemConverter
	logger    *slog.Logger

	batchSize int
	upsertKey string // 非空时启用 Upsert 模式

	mu     sync.Mutex
	buffer []map[string]any
}

// BasePipelineConfig 是 BasePipeline 的配置。
type BasePipelineConfig struct {
	Writer    StorageWriter
	Converter ItemConverter
	Logger    *slog.Logger
	BatchSize int
	UpsertKey string
}

// NewBasePipeline 创建一个新的 BasePipeline。
func NewBasePipeline(cfg BasePipelineConfig) *BasePipeline {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Converter == nil {
		cfg.Converter = defaultConverter
	}
	return &BasePipeline{
		writer:    cfg.Writer,
		converter: cfg.Converter,
		logger:    cfg.Logger,
		batchSize: cfg.BatchSize,
		upsertKey: cfg.UpsertKey,
		buffer:    make([]map[string]any, 0, cfg.BatchSize),
	}
}

// Open 建立与存储后端的连接。
func (p *BasePipeline) Open(ctx context.Context) error {
	return p.writer.Connect(ctx)
}

// Close 刷新剩余缓冲数据并关闭连接。
func (p *BasePipeline) Close(ctx context.Context) error {
	// 刷新剩余缓冲
	if err := p.Flush(ctx); err != nil {
		p.logger.Error("刷新剩余缓冲失败",
			"error", err,
		)
		// 继续关闭连接，不因刷新失败而中断
	}
	return p.writer.Close(ctx)
}

// ProcessItem 将 Item 转换为 map 并加入缓冲区。
// 当缓冲区满时自动触发批量写入。
func (p *BasePipeline) ProcessItem(ctx context.Context, it any) (any, error) {
	record, err := p.converter(it)
	if err != nil {
		return nil, fmt.Errorf("storage: item 转换失败: %w", err)
	}
	if record == nil {
		return it, nil
	}

	p.mu.Lock()
	p.buffer = append(p.buffer, record)
	shouldFlush := len(p.buffer) >= p.batchSize
	p.mu.Unlock()

	if shouldFlush {
		if err := p.Flush(ctx); err != nil {
			return nil, fmt.Errorf("storage: 批量写入失败: %w", err)
		}
	}

	return it, nil
}

// Flush 将缓冲区中的数据写入存储后端。
func (p *BasePipeline) Flush(ctx context.Context) error {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return nil
	}
	// 交换缓冲区，减少锁持有时间
	items := p.buffer
	p.buffer = make([]map[string]any, 0, p.batchSize)
	p.mu.Unlock()

	var (
		n   int
		err error
	)

	if p.upsertKey != "" {
		if uw, ok := p.writer.(UpsertWriter); ok {
			n, err = uw.UpsertBatch(ctx, p.upsertKey, items)
		} else {
			n, err = p.writer.WriteBatch(ctx, items)
		}
	} else {
		n, err = p.writer.WriteBatch(ctx, items)
	}

	if err != nil {
		p.logger.Error("批量写入失败",
			"count", len(items),
			"written", n,
			"error", err,
		)
		return err
	}

	p.logger.Debug("批量写入完成",
		"count", n,
	)
	return nil
}

// defaultConverter 使用 ItemAdapter 将 Item 转换为 map。
func defaultConverter(it any) (map[string]any, error) {
	adapter := item.Adapt(it)
	if adapter == nil {
		// 尝试直接断言为 map
		if m, ok := it.(map[string]any); ok {
			return m, nil
		}
		return nil, fmt.Errorf("storage: 不支持的 Item 类型: %T", it)
	}
	return adapter.AsMap(), nil
}
