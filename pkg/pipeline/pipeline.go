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

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	"github.com/dplcz/scrapy-go/pkg/settings"
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
// CrawlerAwarePipeline 可选接口
// ============================================================================

// Crawler 定义 Pipeline 可访问的 Crawler 能力子集。
//
// 此接口在 pipeline 包中定义（而非引用 crawler.Crawler 具体类型），
// 以避免 pipeline → crawler 的循环依赖。crawler.Crawler 隐式满足此接口。
//
// 对齐 Scrapy 的 `from_crawler(cls, crawler)` 中 Pipeline 可访问的 Crawler 属性。
type Crawler interface {
	GetSettings() *settings.Settings
	GetStats() stats.Collector
	GetSignals() *signal.Manager
	GetLogger() *slog.Logger
}

// CrawlerAwarePipeline 是一个可选接口，Pipeline 可以实现此接口以在初始化时
// 获取 Crawler 引用，从而访问 Settings、Stats、Signals 等框架组件。
//
// 对齐 Scrapy 的 `from_crawler(cls, crawler)` 工厂方法约定（需求 13 验收标准 6）。
//
// 若 Pipeline 实现了此接口，Manager.Open 会在调用 Pipeline.Open 之前
// 调用 FromCrawler 并传入 Crawler 引用。
//
// 用法：
//
//	type MyPipeline struct {
//	    stats stats.Collector
//	}
//
//	func (p *MyPipeline) FromCrawler(c pipeline.Crawler) error {
//	    p.stats = c.GetStats()
//	    return nil
//	}
type CrawlerAwarePipeline interface {
	ItemPipeline
	// FromCrawler 在 Open 之前调用，传入 Crawler 引用。
	// Pipeline 可在此方法中获取 Settings、Stats、Signals 等框架组件。
	// 返回 error 将阻止该 Pipeline 的 Open 调用。
	FromCrawler(c Crawler) error
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
	crawler   Crawler // 可选的 Crawler 引用，供 CrawlerAwarePipeline 使用
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

// SetCrawler 设置 Crawler 引用，供 CrawlerAwarePipeline 在 Open 时使用。
// 必须在 Open 之前调用。
func (m *Manager) SetCrawler(c Crawler) {
	m.crawler = c
}

// Open 打开所有 Pipeline。
// 若 Pipeline 实现了 CrawlerAwarePipeline 接口且 Crawler 引用已设置，
// 会在调用 Open 之前先调用 FromCrawler。
func (m *Manager) Open(ctx context.Context) error {
	for _, entry := range m.pipelines {
		// 若 Pipeline 实现了 CrawlerAwarePipeline，先调用 FromCrawler
		if m.crawler != nil {
			if cap, ok := entry.Pipeline.(CrawlerAwarePipeline); ok {
				if err := cap.FromCrawler(m.crawler); err != nil {
					m.logger.Error("pipeline FromCrawler failed",
						"pipeline", entry.Name,
						"error", err,
					)
					return err
				}
			}
		}
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
			if errors.Is(err, serrors.ErrDropItem) {
				// Item 被丢弃
				// 注意：item_dropped_count 由 CoreStats 扩展通过 ItemDropped 信号递增，
				// 此处不再直接操作统计，避免重复计数。
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
	// 注意：item_scraped_count 由 CoreStats 扩展通过 ItemScraped 信号递增，
	// 此处不再直接操作统计，避免重复计数。
	m.signals.SendCatchLog(signal.ItemScraped, map[string]any{
		"item":     item,
		"response": response,
	})

	return item, nil
}
