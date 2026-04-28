package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	"github.com/dplcz/scrapy-go/pkg/feedexport"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// FeedExportExtension 实现数据导出（Feed Export）扩展。
// 对应 Scrapy 的 scrapy.extensions.feedexport.FeedExporter。
//
// 功能：
//   - 监听 SpiderOpened 信号：为每一条 Feed 配置打开 FeedSlot
//   - 监听 ItemScraped 信号：将 Item 分发到所有符合过滤器的 Feed
//   - 监听 SpiderClosed 信号：关闭全部 FeedSlot 并提交存储
//
// 与 Scrapy 的差异：
//  1. 配置通过 `Configs` 切片直接传入（对应 Scrapy 的 FEEDS 字典）
//  2. 无 addon/load_object 动态加载，全部依赖 Go 的类型安全组合
//  3. 一个 Extension 实例管理所有 Feed（Scrapy 会为每个 Spider 创建一个 FeedExporter）
type FeedExportExtension struct {
	BaseExtension

	signals *signal.Manager
	stats   stats.Collector
	logger  *slog.Logger

	configs []feedexport.FeedConfig

	mu    sync.Mutex
	slots []*feedexport.FeedSlot

	handlerIDs []handlerRegistration
}

// NewFeedExportExtension 创建一个新的 FeedExport 扩展。
//
// configs 为空切片时，扩展会返回 ErrNotConfigured（由 Manager 统一跳过）。
func NewFeedExportExtension(
	configs []feedexport.FeedConfig,
	signals *signal.Manager,
	sc stats.Collector,
	logger *slog.Logger,
) *FeedExportExtension {
	if logger == nil {
		logger = slog.Default()
	}
	return &FeedExportExtension{
		signals: signals,
		stats:   sc,
		logger:  logger,
		configs: configs,
	}
}

// Open 注册信号处理器，准备接收 Spider 生命周期事件。
// 如果配置为空，返回 ErrNotConfigured，框架将跳过此扩展。
func (e *FeedExportExtension) Open(ctx context.Context) error {
	if len(e.configs) == 0 {
		return serrors.ErrNotConfigured
	}

	// 预校验配置：格式是否可识别、URI 是否有效
	for i, cfg := range e.configs {
		f := feedexport.NormalizeFormat(string(cfg.Format))
		if _, ok := feedexport.LookupExporter(f); !ok {
			return fmt.Errorf("feedexport extension: config[%d] unknown format %q", i, cfg.Format)
		}
		if cfg.URI == "" {
			return fmt.Errorf("feedexport extension: config[%d] empty URI", i)
		}
	}

	e.connectSignal(signal.SpiderOpened, e.onSpiderOpened)
	e.connectSignal(signal.ItemScraped, e.onItemScraped)
	e.connectSignal(signal.SpiderClosed, e.onSpiderClosed)

	e.logger.Info("feed export extension enabled",
		"feeds", len(e.configs),
	)
	return nil
}

// Close 注销所有信号处理器并关闭尚未关闭的 Slot（防御性清理）。
func (e *FeedExportExtension) Close(ctx context.Context) error {
	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil

	// 防御性清理：若 SpiderClosed 未派发（极端异常路径），确保 slot 被关闭
	e.mu.Lock()
	slots := e.slots
	e.slots = nil
	e.mu.Unlock()
	for _, slot := range slots {
		if err := slot.Close(ctx, nil); err != nil {
			e.logger.Error("failed to close feed slot", "uri", slot.URI(), "error", err)
		}
	}
	return nil
}

// connectSignal 注册信号处理器并记录 ID。
func (e *FeedExportExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// onSpiderOpened 为每条 Feed 配置构造 FeedSlot。
// 注意：Slot 的 Start 延迟到首个 Item 到达时调用（除非 StoreEmpty=true，则立即 Start）。
func (e *FeedExportExtension) onSpiderOpened(params map[string]any) error {
	sp, _ := params["spider"].(spider.Spider)
	spiderName := ""
	if sp != nil {
		spiderName = sp.Name()
	}

	uriParams := feedexport.NewURIParams(spiderName)

	e.mu.Lock()
	defer e.mu.Unlock()

	var firstErr error
	for _, cfg := range e.configs {
		// 渲染 URI 模板
		cfg.URI = uriParams.Render(cfg.URI)

		slot, err := feedexport.NewFeedSlot(cfg, e.logger)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			e.logger.Error("failed to create feed slot",
				"uri", cfg.URI,
				"format", cfg.Format,
				"error", err,
			)
			continue
		}

		// 如果配置要求即使没有 Item 也写入（StoreEmpty），提前 Start
		if cfg.StoreEmpty {
			if err := slot.Start(context.Background(), sp); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				e.logger.Error("failed to start feed slot",
					"uri", cfg.URI,
					"error", err,
				)
				continue
			}
		}

		e.slots = append(e.slots, slot)
	}
	return firstErr
}

// onItemScraped 将 Item 分发到所有 FeedSlot。
func (e *FeedExportExtension) onItemScraped(params map[string]any) error {
	item := params["item"]
	if item == nil {
		return nil
	}
	sp, _ := params["spider"].(spider.Spider)

	e.mu.Lock()
	slots := make([]*feedexport.FeedSlot, len(e.slots))
	copy(slots, e.slots)
	e.mu.Unlock()

	var errs []error
	ctx := context.Background()
	for _, slot := range slots {
		if err := slot.ExportItem(ctx, sp, item); err != nil {
			errs = append(errs, err)
			e.stats.IncValue(
				fmt.Sprintf("feedexport/error_count/%s", slot.URI()),
				1, 0,
			)
			e.logger.Error("failed to export item", "uri", slot.URI(), "error", err)
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// onSpiderClosed 关闭所有 FeedSlot 并更新统计。
func (e *FeedExportExtension) onSpiderClosed(params map[string]any) error {
	sp, _ := params["spider"].(spider.Spider)

	e.mu.Lock()
	slots := e.slots
	e.slots = nil
	e.mu.Unlock()

	ctx := context.Background()
	var errs []error
	for _, slot := range slots {
		items := slot.ItemCount()
		uri := slot.URI()
		if err := slot.Close(ctx, sp); err != nil {
			errs = append(errs, err)
			e.stats.IncValue(fmt.Sprintf("feedexport/failed_count/%s", uri), 1, 0)
			continue
		}
		e.stats.IncValue(fmt.Sprintf("feedexport/success_count/%s", uri), 1, 0)
		e.stats.SetValue(fmt.Sprintf("feedexport/items_count/%s", uri), items)
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
