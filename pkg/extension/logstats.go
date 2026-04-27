package extension

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// LogStatsExtension 定期输出爬取统计摘要。
// 对应 Scrapy 的 scrapy.extensions.logstats.LogStats。
//
// 定期输出以下信息：
//   - 已爬取页面数和每分钟页面速率（RPM）
//   - 已抓取 Item 数和每分钟 Item 速率（IPM）
//
// 在 Spider 关闭时输出最终的平均速率。
//
// 配置项：
//   - LOGSTATS_INTERVAL — 输出间隔（秒），默认 60.0，设为 0 禁用
type LogStatsExtension struct {
	BaseExtension

	stats    stats.Collector
	signals  *signal.Manager
	logger   *slog.Logger
	interval float64

	// multiplier 用于将间隔内的增量转换为每分钟速率
	multiplier float64

	// 上一次统计快照
	pagesPrev int
	itemsPrev int

	// cancelTicker 用于停止定期输出
	cancelTicker context.CancelFunc

	// handlerIDs 存储注册的信号处理器 ID
	handlerIDs []handlerRegistration
}

// NewLogStatsExtension 创建一个新的 LogStats 扩展。
// interval 为输出间隔（秒），设为 0 禁用。
func NewLogStatsExtension(interval float64, sc stats.Collector, signals *signal.Manager, logger *slog.Logger) *LogStatsExtension {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogStatsExtension{
		stats:      sc,
		signals:    signals,
		logger:     logger,
		interval:   interval,
		multiplier: 60.0 / interval,
	}
}

// Open 检查配置并注册信号处理器。
func (e *LogStatsExtension) Open(ctx context.Context) error {
	if e.interval <= 0 {
		return serrors.ErrNotConfigured
	}

	e.connectSignal(signal.SpiderOpened, e.onSpiderOpened)
	e.connectSignal(signal.SpiderClosed, e.onSpiderClosed)
	return nil
}

// Close 注销所有信号处理器并停止定期输出。
func (e *LogStatsExtension) Close(ctx context.Context) error {
	if e.cancelTicker != nil {
		e.cancelTicker()
		e.cancelTicker = nil
	}

	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil
	return nil
}

// connectSignal 注册信号处理器并记录 ID。
func (e *LogStatsExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// onSpiderOpened 启动定期输出。
func (e *LogStatsExtension) onSpiderOpened(params map[string]any) error {
	e.pagesPrev = 0
	e.itemsPrev = 0

	ctx, cancel := context.WithCancel(context.Background())
	e.cancelTicker = cancel

	go e.logLoop(ctx)
	return nil
}

// onSpiderClosed 停止定期输出并记录最终统计。
func (e *LogStatsExtension) onSpiderClosed(params map[string]any) error {
	if e.cancelTicker != nil {
		e.cancelTicker()
		e.cancelTicker = nil
	}

	// 计算最终平均速率
	e.calculateFinalStats()
	return nil
}

// logLoop 定期输出统计信息。
func (e *LogStatsExtension) logLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(e.interval * float64(time.Second)))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.logStats()
		}
	}
}

// logStats 输出当前统计信息。
func (e *LogStatsExtension) logStats() {
	pages := toInt(e.stats.GetValue("response_received_count", 0))
	items := toInt(e.stats.GetValue("item_scraped_count", 0))

	pageRate := float64(pages-e.pagesPrev) * e.multiplier
	itemRate := float64(items-e.itemsPrev) * e.multiplier

	e.pagesPrev = pages
	e.itemsPrev = items

	e.logger.Info(fmt.Sprintf("Crawled %d pages (at %.0f pages/min), scraped %d items (at %.0f items/min)",
		pages, pageRate, items, itemRate))
}

// calculateFinalStats 计算并记录最终平均速率。
func (e *LogStatsExtension) calculateFinalStats() {
	startTime := e.stats.GetValue("start_time", nil)
	finishTime := e.stats.GetValue("finish_time", nil)

	if startTime == nil || finishTime == nil {
		return
	}

	elapsedSeconds := e.stats.GetValue("elapsed_time_seconds", nil)
	if elapsedSeconds == nil {
		return
	}

	elapsed := toFloat64(elapsedSeconds)
	if elapsed <= 0 {
		return
	}

	minsElapsed := elapsed / 60.0
	if minsElapsed == 0 {
		return
	}

	pages := toInt(e.stats.GetValue("response_received_count", 0))
	items := toInt(e.stats.GetValue("item_scraped_count", 0))

	rpm := float64(pages) / minsElapsed
	ipm := float64(items) / minsElapsed

	e.stats.SetValue("responses_per_minute", rpm)
	e.stats.SetValue("items_per_minute", ipm)
}

// toInt 将 any 类型转换为 int。
func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}

// toFloat64 将 any 类型转换为 float64。
func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}
