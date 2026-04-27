package extension

import (
	"context"
	"log/slog"
	"time"

	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// CoreStatsExtension 收集核心统计信息。
// 对应 Scrapy 的 scrapy.extensions.corestats.CoreStats。
//
// 统计项：
//   - start_time — Spider 启动时间
//   - finish_time — Spider 结束时间
//   - elapsed_time_seconds — 运行耗时（秒）
//   - finish_reason — 关闭原因
//   - item_scraped_count — 已抓取 Item 数量（通过信号递增）
//   - item_dropped_count — 已丢弃 Item 数量（通过信号递增）
//   - response_received_count — 已接收响应数量（通过信号递增）
type CoreStatsExtension struct {
	BaseExtension

	stats   stats.Collector
	signals *signal.Manager
	logger  *slog.Logger

	startTime time.Time

	// handlerIDs 存储注册的信号处理器 ID，用于 Close 时注销
	handlerIDs []handlerRegistration
}

// handlerRegistration 存储信号处理器的注册信息。
type handlerRegistration struct {
	id  uint64
	sig signal.Signal
}

// NewCoreStatsExtension 创建一个新的 CoreStats 扩展。
func NewCoreStatsExtension(sc stats.Collector, signals *signal.Manager, logger *slog.Logger) *CoreStatsExtension {
	if logger == nil {
		logger = slog.Default()
	}
	return &CoreStatsExtension{
		stats:   sc,
		signals: signals,
		logger:  logger,
	}
}

// Open 注册信号处理器。
func (e *CoreStatsExtension) Open(ctx context.Context) error {
	e.connectSignal(signal.SpiderOpened, e.onSpiderOpened)
	e.connectSignal(signal.SpiderClosed, e.onSpiderClosed)
	e.connectSignal(signal.ItemScraped, e.onItemScraped)
	e.connectSignal(signal.ItemDropped, e.onItemDropped)
	e.connectSignal(signal.ResponseReceived, e.onResponseReceived)
	return nil
}

// Close 注销所有信号处理器。
func (e *CoreStatsExtension) Close(ctx context.Context) error {
	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil
	return nil
}

// connectSignal 注册信号处理器并记录 ID。
func (e *CoreStatsExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// onSpiderOpened 记录启动时间。
func (e *CoreStatsExtension) onSpiderOpened(params map[string]any) error {
	e.startTime = time.Now()
	e.stats.SetValue("start_time", e.startTime)
	return nil
}

// onSpiderClosed 记录结束时间和耗时。
func (e *CoreStatsExtension) onSpiderClosed(params map[string]any) error {
	finishTime := time.Now()
	elapsed := finishTime.Sub(e.startTime)

	e.stats.SetValue("finish_time", finishTime)
	e.stats.SetValue("elapsed_time_seconds", elapsed.Seconds())

	if reason, ok := params["reason"]; ok {
		e.stats.SetValue("finish_reason", reason)
	} else {
		e.stats.SetValue("finish_reason", "finished")
	}

	return nil
}

// onItemScraped 递增已抓取 Item 计数。
func (e *CoreStatsExtension) onItemScraped(params map[string]any) error {
	e.stats.IncValue("item_scraped_count", 1, 0)
	return nil
}

// onItemDropped 递增已丢弃 Item 计数。
func (e *CoreStatsExtension) onItemDropped(params map[string]any) error {
	e.stats.IncValue("item_dropped_count", 1, 0)
	return nil
}

// onResponseReceived 递增已接收响应计数。
func (e *CoreStatsExtension) onResponseReceived(params map[string]any) error {
	e.stats.IncValue("response_received_count", 1, 0)
	return nil
}
