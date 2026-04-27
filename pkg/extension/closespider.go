package extension

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// CloseSpiderExtension 在满足特定条件时自动关闭 Spider。
// 对应 Scrapy 的 scrapy.extensions.closespider.CloseSpider。
//
// 支持的关闭条件（通过配置项控制，值为 0 表示禁用）：
//   - CLOSESPIDER_TIMEOUT — 运行超时（秒），通过 time.AfterFunc 实现
//   - CLOSESPIDER_ITEMCOUNT — 达到指定 Item 数量后关闭
//   - CLOSESPIDER_PAGECOUNT — 达到指定页面数量后关闭
//   - CLOSESPIDER_ERRORCOUNT — 达到指定错误数量后关闭
//
// 当任一条件满足时，扩展会通过信号系统返回 ErrCloseSpider 请求关闭 Spider。
type CloseSpiderExtension struct {
	BaseExtension

	signals *signal.Manager
	stats   stats.Collector
	logger  *slog.Logger

	timeout    float64
	itemCount  int
	pageCount  int
	errorCount int

	// 原子计数器
	currentItems  atomic.Int64
	currentPages  atomic.Int64
	currentErrors atomic.Int64

	// closing 标记是否已触发关闭，避免重复关闭
	closing atomic.Bool

	// cancelTimeout 用于取消超时定时器
	cancelTimeout context.CancelFunc

	// handlerIDs 存储注册的信号处理器 ID
	handlerIDs []handlerRegistration
}

// NewCloseSpiderExtension 创建一个新的 CloseSpider 扩展。
// timeout 单位为秒，itemCount/pageCount/errorCount 为 0 表示禁用对应条件。
func NewCloseSpiderExtension(
	timeout float64,
	itemCount, pageCount, errorCount int,
	signals *signal.Manager,
	sc stats.Collector,
	logger *slog.Logger,
) *CloseSpiderExtension {
	if logger == nil {
		logger = slog.Default()
	}
	return &CloseSpiderExtension{
		signals:    signals,
		stats:      sc,
		logger:     logger,
		timeout:    timeout,
		itemCount:  itemCount,
		pageCount:  pageCount,
		errorCount: errorCount,
	}
}

// Open 检查配置并注册信号处理器。
// 如果所有关闭条件均为 0，返回 ErrNotConfigured。
func (e *CloseSpiderExtension) Open(ctx context.Context) error {
	if e.timeout == 0 && e.itemCount == 0 && e.pageCount == 0 && e.errorCount == 0 {
		return serrors.ErrNotConfigured
	}

	if e.errorCount > 0 {
		e.connectSignal(signal.SpiderError, e.onSpiderError)
	}
	if e.pageCount > 0 {
		e.connectSignal(signal.ResponseReceived, e.onResponseReceived)
	}
	if e.itemCount > 0 {
		e.connectSignal(signal.ItemScraped, e.onItemScraped)
	}
	if e.timeout > 0 {
		e.connectSignal(signal.SpiderOpened, e.onSpiderOpened)
	}

	// 注册 spider_closed 信号用于清理
	e.connectSignal(signal.SpiderClosed, e.onSpiderClosed)

	e.logger.Info("CloseSpider extension enabled",
		"timeout", e.timeout,
		"itemcount", e.itemCount,
		"pagecount", e.pageCount,
		"errorcount", e.errorCount,
	)

	return nil
}

// Close 注销所有信号处理器。
func (e *CloseSpiderExtension) Close(ctx context.Context) error {
	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil

	if e.cancelTimeout != nil {
		e.cancelTimeout()
		e.cancelTimeout = nil
	}

	return nil
}

// connectSignal 注册信号处理器并记录 ID。
func (e *CloseSpiderExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// onSpiderOpened 启动超时定时器。
func (e *CloseSpiderExtension) onSpiderOpened(params map[string]any) error {
	ctx, cancel := context.WithCancel(context.Background())
	e.cancelTimeout = cancel

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(e.timeout * float64(time.Second))):
			e.requestClose("closespider_timeout")
		}
	}()

	return nil
}

// onSpiderClosed 清理超时定时器。
func (e *CloseSpiderExtension) onSpiderClosed(params map[string]any) error {
	if e.cancelTimeout != nil {
		e.cancelTimeout()
		e.cancelTimeout = nil
	}
	return nil
}

// onItemScraped 检查 Item 数量条件。
func (e *CloseSpiderExtension) onItemScraped(params map[string]any) error {
	count := e.currentItems.Add(1)
	if int(count) >= e.itemCount {
		e.requestClose("closespider_itemcount")
	}
	return nil
}

// onResponseReceived 检查页面数量条件。
func (e *CloseSpiderExtension) onResponseReceived(params map[string]any) error {
	count := e.currentPages.Add(1)
	if int(count) >= e.pageCount {
		e.requestClose("closespider_pagecount")
	}
	return nil
}

// onSpiderError 检查错误数量条件。
func (e *CloseSpiderExtension) onSpiderError(params map[string]any) error {
	count := e.currentErrors.Add(1)
	if int(count) >= e.errorCount {
		e.requestClose("closespider_errorcount")
	}
	return nil
}

// requestClose 请求关闭 Spider。
// 使用 CAS 确保只触发一次关闭。
func (e *CloseSpiderExtension) requestClose(reason string) {
	if !e.closing.CompareAndSwap(false, true) {
		return
	}

	e.logger.Info("closing spider", "reason", reason)

	// 通过 SpiderIdle 信号返回 ErrCloseSpider 来请求关闭
	// 这里直接发送信号通知引擎关闭
	e.signals.SendCatchLog(signal.SpiderIdle, map[string]any{
		"close_reason": reason,
	})
}
