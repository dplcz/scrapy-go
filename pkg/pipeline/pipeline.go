// Package pipeline 定义了 scrapy-go 框架的 Item Pipeline 接口和管理器。
//
// Item Pipeline 负责处理 Spider 产出的数据项（Item），
// 支持数据清洗、验证、去重和持久化等操作。
// 对应 Scrapy Python 版本中 scrapy.pipelines 模块的功能。
package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	scrapy_errors "github.com/dplcz/scrapy-go/pkg/errors"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// ItemPipeline 接口
// ============================================================================

// ItemPipeline 定义数据处理管道接口。
// 每个 Pipeline 按优先级顺序处理 Item。
type ItemPipeline interface {
	// Open 在 Spider 打开时调用，用于初始化资源。
	Open(ctx context.Context) error

	// Close 在 Spider 关闭时调用，用于释放资源。
	Close(ctx context.Context) error

	// ProcessItem 处理一个 Item。
	// 返回处理后的 Item（可修改）和 error。
	// 返回 ErrDropItem 表示丢弃该 Item，后续 Pipeline 不再处理。
	ProcessItem(ctx context.Context, item any) (any, error)
}

// ============================================================================
// Entry
// ============================================================================

// Entry 表示一个带优先级的 Pipeline 条目。
type Entry struct {
	Pipeline ItemPipeline
	Name     string
	Priority int
}

// ============================================================================
// Manager 管理器
// ============================================================================

// Manager 管理 Item Pipeline 链。
// 对应 Scrapy 的 ItemPipelineManager。
type Manager struct {
	pipelines []Entry // 按优先级排序
	signals   *signal.Manager
	stats     stats.Collector
	logger    *slog.Logger
}

// NewManager 创建一个新的 Pipeline 管理器。
func NewManager(signals *signal.Manager, sc stats.Collector, logger *slog.Logger) *Manager {
	if signals == nil {
		signals = signal.NewManager(nil)
	}
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		signals: signals,
		stats:   sc,
		logger:  logger,
	}
}

// AddPipeline 添加一个 Pipeline。
// Pipeline 按优先级排序，优先级数值小的先执行。
func (m *Manager) AddPipeline(p ItemPipeline, name string, priority int) {
	m.pipelines = append(m.pipelines, Entry{
		Pipeline: p,
		Name:     name,
		Priority: priority,
	})
	sort.Slice(m.pipelines, func(i, j int) bool {
		return m.pipelines[i].Priority < m.pipelines[j].Priority
	})
}

// Count 返回 Pipeline 数量。
func (m *Manager) Count() int {
	return len(m.pipelines)
}

// Open 打开所有 Pipeline。
func (m *Manager) Open(ctx context.Context) error {
	for _, entry := range m.pipelines {
		if err := entry.Pipeline.Open(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭所有 Pipeline。
// 即使某个 Pipeline 关闭失败，也会继续关闭其他 Pipeline。
func (m *Manager) Close(ctx context.Context) error {
	var firstErr error
	for _, entry := range m.pipelines {
		if err := entry.Pipeline.Close(ctx); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.logger.Error("failed to close pipeline",
				"pipeline", entry.Name,
				"error", err,
			)
		}
	}
	return firstErr
}

// ProcessItem 按优先级顺序通过所有 Pipeline 处理 Item。
//
// 处理流程：
//  1. 按优先级顺序调用每个 Pipeline 的 ProcessItem
//  2. 如果某个 Pipeline 返回 ErrDropItem，停止后续处理并发出 item_dropped 信号
//  3. 如果某个 Pipeline 返回其他错误，发出 item_error 信号
//  4. 所有 Pipeline 处理成功后，发出 item_scraped 信号
func (m *Manager) ProcessItem(ctx context.Context, item any, response any) (any, error) {
	var err error
	for _, entry := range m.pipelines {
		item, err = entry.Pipeline.ProcessItem(ctx, item)
		if err != nil {
			if errors.Is(err, scrapy_errors.ErrDropItem) {
				// Item 被丢弃
				m.stats.IncValue("item_dropped_count", 1, 0)
				m.signals.SendCatchLog(signal.ItemDropped, map[string]any{
					"item":     item,
					"response": response,
					"error":    err,
				})
				return nil, err
			}

			// 其他错误
			m.stats.IncValue("item_error_count", 1, 0)
			m.logger.Error("pipeline failed to process item",
				"pipeline", entry.Name,
				"error", err,
			)
			m.signals.SendCatchLog(signal.ItemError, map[string]any{
				"item":     item,
				"response": response,
				"error":    err,
			})
			return nil, err
		}
	}

	// 所有 Pipeline 处理成功
	m.stats.IncValue("item_scraped_count", 1, 0)
	m.signals.SendCatchLog(signal.ItemScraped, map[string]any{
		"item":     item,
		"response": response,
	})

	return item, nil
}
