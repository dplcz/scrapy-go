package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dplcz/scrapy-go/pkg/downloader"
	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	sslog "github.com/dplcz/scrapy-go/pkg/log"
	"github.com/dplcz/scrapy-go/pkg/extension"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
	"github.com/dplcz/scrapy-go/pkg/scraper"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

const (
	defaultHeartbeatInterval = 5 * time.Second
)

// Engine 是框架的核心引擎，协调所有组件。
// 对应 Scrapy 的 ExecutionEngine。
type Engine struct {
	mu sync.Mutex

	slot       *Slot
	spider     spider.Spider
	scheduler  scheduler.Scheduler
	downloader *downloader.Downloader
	dlMW       *downloader.MiddlewareManager
	scraper    *scraper.Scraper
	extensions *extension.Manager
	signals    *signal.Manager
	stats      stats.Collector
	logger     *slog.Logger

	running   atomic.Bool
	paused    atomic.Bool
	startTime time.Time

	heartbeatInterval time.Duration

	// scheduleNotify 用于在有新请求入队时即时通知调度循环
	scheduleNotify chan struct{}

	// startRequestsDone 标记初始请求是否已全部消费
	startRequestsDone atomic.Bool
}

// NewEngine 创建一个新的 Engine。
func NewEngine(
	sp spider.Spider,
	sched scheduler.Scheduler,
	dl *downloader.Downloader,
	dlMW *downloader.MiddlewareManager,
	sc *scraper.Scraper,
	signals *signal.Manager,
	statsCollector stats.Collector,
	logger *slog.Logger,
	ext *extension.Manager,
) *Engine {
	if signals == nil {
		signals = signal.NewManager(nil)
	}
	if statsCollector == nil {
		statsCollector = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Engine{
		spider:            sp,
		scheduler:         sched,
		downloader:        dl,
		dlMW:              dlMW,
		scraper:           sc,
		extensions:        ext,
		signals:           signals,
		stats:             statsCollector,
		logger:            logger,
		heartbeatInterval: defaultHeartbeatInterval,
		scheduleNotify:    make(chan struct{}, 1),
	}
}

// Start 启动引擎，开始爬取流程。
// 此方法会阻塞直到爬取完成或 context 被取消。
func (e *Engine) Start(ctx context.Context) error {
	if e.running.Load() {
		return errors.New("engine already running")
	}

	e.startTime = time.Now()
	e.running.Store(true)
	defer e.running.Store(false)

	// 初始化组件
	if err := e.openSpider(ctx); err != nil {
		return err
	}

	// 发送 engine_started 信号
	e.signals.SendCatchLog(signal.EngineStarted, nil)

	// 启动初始请求消费
	go e.consumeStartRequests(ctx)

	// 主调度循环
	err := e.run(ctx)

	// 关闭
	e.closeSpider(ctx, e.closeReason(err))

	// 发送 engine_stopped 信号
	e.signals.SendCatchLog(signal.EngineStopped, nil)

	return err
}

// Pause 暂停引擎。
func (e *Engine) Pause() {
	e.paused.Store(true)
}

// Unpause 恢复引擎。
func (e *Engine) Unpause() {
	e.paused.Store(false)
}

// IsRunning 返回引擎是否正在运行。
func (e *Engine) IsRunning() bool {
	return e.running.Load()
}

// IsPaused 返回引擎是否已暂停。
func (e *Engine) IsPaused() bool {
	return e.paused.Load()
}

// ============================================================================
// 内部方法
// ============================================================================

// openSpider 初始化所有组件。
func (e *Engine) openSpider(ctx context.Context) error {
	// 打开调度器
	if err := e.scheduler.Open(ctx); err != nil {
		return err
	}

	// 创建 Slot
	e.slot = NewSlot(e.scheduler, true)

	// 打开 Scraper
	if err := e.scraper.Open(ctx); err != nil {
		return err
	}

	// 打开统计收集器
	e.stats.Open()

	// 打开扩展系统
	if e.extensions != nil {
		if err := e.extensions.Open(ctx); err != nil {
			return err
		}
	}

	e.logger.Info("spider opened", "spider", e.spider.Name())

	// 发送 spider_opened 信号
	e.signals.SendCatchLog(signal.SpiderOpened, map[string]any{
		"spider": e.spider,
	})

	return nil
}

// closeSpider 关闭所有组件。
func (e *Engine) closeSpider(ctx context.Context, reason string) {
	e.logger.Info("closing spider", "reason", reason)

	e.slot.closing.Store(true)

	// 关闭下载器
	if err := e.downloader.Close(); err != nil {
		e.logger.Error("failed to close downloader", "error", err)
	}

	// 关闭 Scraper
	if err := e.scraper.Close(ctx); err != nil {
		e.logger.Error("failed to close scraper", "error", err)
	}

	// 关闭调度器
	if err := e.scheduler.Close(ctx, reason); err != nil {
		e.logger.Error("failed to close scheduler", "error", err)
	}

	// 关闭扩展系统
	if e.extensions != nil {
		if err := e.extensions.Close(ctx); err != nil {
			e.logger.Error("failed to close extensions", "error", err)
		}
	}

	// 发送 spider_closed 信号
	e.signals.SendCatchLog(signal.SpiderClosed, map[string]any{
		"spider": e.spider,
		"reason": reason,
	})

	// 关闭统计收集器
	e.stats.Close(reason)

	// 调用 Spider.Closed
	e.spider.Closed(reason)

	e.logger.Info("spider closed", "reason", reason)
}

// run 是主调度循环。
func (e *Engine) run(ctx context.Context) error {
	ticker := time.NewTicker(e.heartbeatInterval)
	defer ticker.Stop()

	for {
		// 检查是否正在关闭
		if e.slot != nil && e.slot.closing.Load() && e.slot.IsIdle() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			e.processScheduledRequests(ctx)
		case <-e.scheduleNotify:
			e.processScheduledRequests(ctx)
		}
	}
}

// processScheduledRequests 处理调度队列中的请求。
// 对齐 Scrapy 原版 _start_scheduled_requests 的设计：
//   - 在同步循环中从 Scheduler 取出请求
//   - 同步将请求添加到 Downloader.active（确保 NeedsBackout 立即可见）
//   - 然后异步启动下载 goroutine
func (e *Engine) processScheduledRequests(ctx context.Context) {
	if e.paused.Load() || e.slot == nil || e.slot.closing.Load() {
		return
	}

	for !e.needsBackout() {
		req := e.scheduler.NextRequest()
		if req == nil {
			e.signals.SendCatchLog(signal.SchedulerEmpty, nil)
			break
		}

		// 对齐 Scrapy 原版：在同步路径中将请求添加到 Engine.slot.inprogress 和 Downloader.active。
		// 这样下一次循环的 needsBackout() 能立即看到最新的计数，
		// 避免因 goroutine 启动延迟导致的竞态条件。
		e.slot.AddRequest(req)
		e.downloader.AddActive(req)
		go e.downloadAndScrape(ctx, req)
	}

	if e.spiderIsIdle() && e.slot.closeIfIdle {
		e.handleSpiderIdle(ctx)
	}
}

// needsBackout 检查是否需要回退。
func (e *Engine) needsBackout() bool {
	return !e.running.Load() ||
		e.slot == nil ||
		e.slot.closing.Load() ||
		e.downloader.NeedsBackout() ||
		e.scraper.NeedsBackout()
}

// spiderIsIdle 检查 Spider 是否空闲。
func (e *Engine) spiderIsIdle() bool {
	if e.slot == nil {
		return false
	}
	if !e.slot.IsIdle() {
		return false
	}
	if e.downloader.ActiveCount() > 0 {
		return false
	}
	if !e.startRequestsDone.Load() {
		return false
	}
	return !e.scheduler.HasPendingRequests()
}

// handleSpiderIdle 处理 Spider 空闲状态。
func (e *Engine) handleSpiderIdle(ctx context.Context) {
	errs := e.signals.SendCatchLog(signal.SpiderIdle, map[string]any{
		"spider": e.spider,
	})

	// 检查是否有处理器请求不关闭
	if signal.ContainsDontCloseSpider(errs) {
		return
	}

	// 再次确认空闲
	if e.spiderIsIdle() {
		reason := "finished"
		if closeErr := signal.ContainsCloseSpider(errs); closeErr != nil {
			reason = closeErr.Reason
		}
		e.slot.closing.Store(true)
		e.logger.Info("spider idle, closing", "reason", reason)
		// 通知调度循环检查退出条件
		e.notifySchedule()
	}
}

// downloadAndScrape 下载并处理单个请求。
func (e *Engine) downloadAndScrape(ctx context.Context, request *shttp.Request) {
	defer func() {
		e.slot.RemoveRequest(request)
		e.downloader.RemoveActive(request)
		e.notifySchedule()
	}()

	// panic recovery: 防止用户回调/中间件/Pipeline 中的 panic 导致进程崩溃
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			panicErr := serrors.NewPanicError(r, stack)
			e.logger.Error("panic recovered in downloadAndScrape",
				"request", request.String(),
				"error", panicErr,
			)
			e.stats.IncValue("spider_exceptions/panic", 1, 0)
		}
	}()

	// 通过下载器中间件链下载
	var resp *shttp.Response
	var err error

	if e.dlMW != nil && e.dlMW.Count() > 0 {
		resp, err = e.dlMW.Download(ctx, func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
			return e.downloader.Download(ctx, req)
		}, request)
	} else {
		resp, err = e.downloader.Download(ctx, request)
	}

	if err != nil {
		// 检查是否为 NewRequestError（重试/重定向产生的新请求）
		var newReqErr *serrors.NewRequestError
		if errors.As(err, &newReqErr) {
			if newReq, ok := newReqErr.Request.(*shttp.Request); ok {
				e.crawl(newReq)
			}
			return
		}

		// 下载失败
		if errors.Is(err, context.Canceled) {
			return // context 取消，不处理
		}

		e.logger.Debug("download failed",
			"request", request.String(),
			"error", err,
		)

		// 调用 Scraper 的错误处理
		newReqs, _ := e.scraper.ScrapeError(ctx, err, request)
		e.scheduleNewRequests(newReqs)
		return
	}

	// 发送 response_received 信号
	// 注：下游核心指标（response_received_count）由 CoreStats 扩展监听该信号后递增，
	// 下载层指标（downloader/response_count、downloader/response_status_count/{STATUS}、
	// downloader/response_bytes 等）由 DownloaderStats 中间件统一负责。
	// 引擎仅负责派发信号与引擎视角的调度日志，不直接写入下载层统计。
	e.signals.SendCatchLog(signal.ResponseReceived, map[string]any{
		"response": resp,
		"request":  request,
	})

	e.logger.Debug("response received",
		"status", fmt.Sprintf("%s%d%s", sslog.ColorByStatusCode(resp.Status), resp.Status, sslog.ColorReset),
		"url", resp.URL.String(),
	)

	// 通过 Scraper 处理响应
	newReqs, err := e.scraper.Scrape(ctx, resp, request)
	if err != nil {
		if errors.Is(err, serrors.ErrCloseSpider) {
			e.slot.closing.Store(true)
			return
		}
	}

	// 调度新请求
	e.scheduleNewRequests(newReqs)
}

// consumeStartRequests 消费 Spider 的初始请求。
func (e *Engine) consumeStartRequests(ctx context.Context) {
	defer e.startRequestsDone.Store(true)

	// panic recovery: 防止 Spider.Start() 中的 panic 导致进程崩溃
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			panicErr := serrors.NewPanicError(r, stack)
			e.logger.Error("panic recovered in consumeStartRequests",
				"error", panicErr,
			)
			e.stats.IncValue("spider_exceptions/panic", 1, 0)
		}
	}()

	ch := e.spider.Start(ctx)
	for output := range ch {
		if !e.running.Load() {
			return
		}
		if output.IsRequest() {
			e.crawl(output.Request)
		} else if output.IsItem() {
			// 初始 Item 直接进入 Pipeline（通过 Scraper）
			e.logger.Debug("start item", "item", output.Item)
		}
	}

	e.logger.Debug("start requests consumed")
	e.notifySchedule()
}

// crawl 将请求注入调度器。
func (e *Engine) crawl(request *shttp.Request) {
	if !e.scheduler.EnqueueRequest(request) {
		// 请求被过滤（去重）
		e.signals.SendCatchLog(signal.RequestDropped, map[string]any{
			"request": request,
		})
		return
	}

	e.signals.SendCatchLog(signal.RequestScheduled, map[string]any{
		"request": request,
	})

	e.notifySchedule()
}

// scheduleNewRequests 调度新请求。
func (e *Engine) scheduleNewRequests(requests []*shttp.Request) {
	for _, req := range requests {
		e.crawl(req)
	}
}

// notifySchedule 通知调度循环有新请求。
func (e *Engine) notifySchedule() {
	select {
	case e.scheduleNotify <- struct{}{}:
	default:
	}
}

// closeReason 根据错误确定关闭原因。
func (e *Engine) closeReason(err error) string {
	if err == nil || errors.Is(err, context.Canceled) {
		return "finished"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var closeErr *serrors.CloseSpiderError
	if errors.As(err, &closeErr) {
		return closeErr.Reason
	}
	return "shutdown"
}
