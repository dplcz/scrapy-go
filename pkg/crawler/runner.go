// Package crawler 中的 Runner 实现了多爬虫的并发/顺序调度。
//
// Runner 对应 Scrapy Python 版本中的 CrawlerRunner / AsyncCrawlerRunner。
// 它负责：
//   - 跟踪和管理多个 Crawler 实例
//   - 支持并发（StartConcurrent）与顺序（StartSequentially）两种调度模式
//   - 统一 OS 信号处理（SIGINT/SIGTERM）与优雅关闭
//   - 跨爬虫 Signal 传播（通过 ConnectSignal 为所有当前/未来 Crawler 注册处理器）
//
// 与 Scrapy 原版的差异：
//   - 舍弃 Python spider_loader（通过字符串名加载 Spider 类）——Go 直接传入 Spider 实例
//   - 舍弃 CrawlerProcess（reactor 生命周期管理）——Go 无全局 reactor，信号处理集成在 Runner 中
//   - 使用 sync.WaitGroup + channel 替代 Twisted Deferred / asyncio.Task 集合
//   - 使用 errors.Join 聚合多个 Crawler 的错误
package crawler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	sslog "github.com/dplcz/scrapy-go/pkg/log"
	sig "github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ErrRunnerClosed 表示 Runner 已被关闭，无法再接受新的爬虫任务。
var ErrRunnerClosed = errors.New("crawler runner is closed")

// ErrCrawlerAlreadyManaged 表示同一个 Crawler 实例被重复添加到 Runner。
var ErrCrawlerAlreadyManaged = errors.New("crawler is already managed by this runner")

// ============================================================================
// Job：Crawler 与 Spider 的绑定
// ============================================================================

// Job 表示一个爬取任务，由一个 Crawler 实例和一个 Spider 实例组成。
//
// Runner 会按 Job 的顺序调度执行。对于并发模式，所有 Job 同时启动；
// 对于顺序模式，Job 按切片顺序依次执行，前一个完成后再启动下一个。
type Job struct {
	// Crawler 是执行爬取的组件编排器。必须非 nil，且未启动过。
	Crawler *Crawler

	// Spider 是本次爬取的爬虫实例。必须非 nil。
	Spider spider.Spider
}

// NewJob 创建一个新的爬取任务。
func NewJob(c *Crawler, sp spider.Spider) Job {
	return Job{Crawler: c, Spider: sp}
}

// ============================================================================
// Runner 实现
// ============================================================================

// Runner 管理多个 Crawler 的并发或顺序执行。
//
// 使用方式：
//
//	runner := crawler.NewRunner()
//	runner.ConnectSignal(signal.SpiderOpened, onSpiderOpened)
//	err := runner.StartConcurrent(ctx,
//	    crawler.NewJob(crawler.NewDefault(), spiderA),
//	    crawler.NewJob(crawler.NewDefault(), spiderB),
//	)
//
// 并发安全：所有公共方法均可被多个 goroutine 安全调用。
type Runner struct {
	logger *slog.Logger

	// mu 保护 crawlers 和 closed 字段
	mu sync.RWMutex

	// closed 标记 Runner 是否已关闭
	closed bool

	// crawlers 跟踪所有由 Runner 管理的 Crawler 实例
	crawlers map[*Crawler]struct{}

	// wg 用于等待所有爬虫任务完成（对应 Scrapy 的 _active / join）
	wg sync.WaitGroup

	// globalHandlers 存储跨爬虫 Signal 处理器，新加入的 Crawler 会自动注册这些处理器
	handlersMu     sync.Mutex
	globalHandlers []globalHandlerEntry

	// installOSSignals 决定 Start* 方法是否安装 OS 信号处理器
	installOSSignals bool

	// bootstrapFailed 记录是否有任意 Crawler 在启动/运行阶段失败。
	// 对应 Scrapy CrawlerRunnerBase.bootstrap_failed。
	bootstrapFailed atomic.Bool
}

// globalHandlerEntry 是跨爬虫 Signal 注册项。
type globalHandlerEntry struct {
	signal  sig.Signal
	handler sig.Handler
}

// RunnerOption 是 Runner 的可选配置函数。
type RunnerOption func(*Runner)

// WithRunnerLogger 设置 Runner 自身的日志记录器。
// 若未设置，则使用带默认配置的彩色日志记录器。
func WithRunnerLogger(logger *slog.Logger) RunnerOption {
	return func(r *Runner) {
		if logger != nil {
			r.logger = logger
		}
	}
}

// WithOSSignalHandling 控制 Runner 是否安装 OS 信号（SIGINT/SIGTERM）处理器。
// 默认启用。在测试或由外部统一管理信号时可关闭。
func WithOSSignalHandling(enabled bool) RunnerOption {
	return func(r *Runner) {
		r.installOSSignals = enabled
	}
}

// NewRunner 创建一个新的 Runner。
func NewRunner(opts ...RunnerOption) *Runner {
	r := &Runner{
		crawlers:         make(map[*Crawler]struct{}),
		installOSSignals: true,
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.logger == nil {
		r.logger = sslog.NewColorLogger("INFO", nil, false)
	}
	return r
}

// ============================================================================
// 跨爬虫 Signal 传播
// ============================================================================

// ConnectSignal 为所有当前和未来加入的 Crawler 注册同一个信号处理器。
//
// 典型用途：在运行多个 Spider 时统一监听 SpiderOpened / SpiderClosed / ItemScraped 等事件。
//
// 注意：对于 ConnectSignal 调用时已完成 Crawl 的 Crawler，不会追溯注册。
// 建议在启动爬虫之前调用 ConnectSignal。
func (r *Runner) ConnectSignal(s sig.Signal, handler sig.Handler) {
	if handler == nil {
		return
	}
	r.handlersMu.Lock()
	r.globalHandlers = append(r.globalHandlers, globalHandlerEntry{signal: s, handler: handler})
	r.handlersMu.Unlock()
}

// snapshotHandlers 返回当前所有跨爬虫处理器的快照。
func (r *Runner) snapshotHandlers() []globalHandlerEntry {
	r.handlersMu.Lock()
	defer r.handlersMu.Unlock()
	out := make([]globalHandlerEntry, len(r.globalHandlers))
	copy(out, r.globalHandlers)
	return out
}

// applyGlobalHandlers 将所有 globalHandlers 注册到指定 Crawler 的 Signals 管理器。
func (r *Runner) applyGlobalHandlers(c *Crawler) {
	if c == nil || c.Signals == nil {
		return
	}
	for _, entry := range r.snapshotHandlers() {
		c.Signals.Connect(entry.handler, entry.signal)
	}
}

// ============================================================================
// Crawler 管理
// ============================================================================

// Crawlers 返回当前由 Runner 管理的所有 Crawler 的快照。
// 返回的切片是拷贝，调用方修改不会影响内部状态。
func (r *Runner) Crawlers() []*Crawler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Crawler, 0, len(r.crawlers))
	for c := range r.crawlers {
		out = append(out, c)
	}
	return out
}

// BootstrapFailed 返回是否有任意 Crawler 在启动/运行阶段失败。
// 对应 Scrapy CrawlerRunnerBase.bootstrap_failed。
func (r *Runner) BootstrapFailed() bool {
	return r.bootstrapFailed.Load()
}

// addCrawler 将 Crawler 注册到管理集合。返回 err 表示 Runner 已关闭或重复注册。
func (r *Runner) addCrawler(c *Crawler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRunnerClosed
	}
	if _, ok := r.crawlers[c]; ok {
		return ErrCrawlerAlreadyManaged
	}
	r.crawlers[c] = struct{}{}
	return nil
}

// removeCrawler 将 Crawler 从管理集合移除。
func (r *Runner) removeCrawler(c *Crawler) {
	r.mu.Lock()
	delete(r.crawlers, c)
	r.mu.Unlock()
}

// ============================================================================
// 单爬虫异步调度
// ============================================================================

// Crawl 异步启动单个 Crawler，立即返回一个 channel，爬虫完成时 channel 会接收一次错误值（nil 表示成功）然后关闭。
//
// 调用方式：
//
//	done := runner.Crawl(ctx, c, sp)
//	err := <-done  // 阻塞等待完成
//
// 如果 Runner 已关闭或参数非法，返回的 channel 会立即接收到相应错误并关闭。
func (r *Runner) Crawl(ctx context.Context, c *Crawler, sp spider.Spider) <-chan error {
	done := make(chan error, 1)

	if c == nil {
		done <- errors.New("crawler must not be nil")
		close(done)
		return done
	}
	if sp == nil {
		done <- errors.New("spider must not be nil")
		close(done)
		return done
	}

	if err := r.addCrawler(c); err != nil {
		done <- err
		close(done)
		return done
	}

	// 通过 onBeforeStart 钩子在组件组装完成、Engine 启动之前注册跨爬虫信号处理器。
	// 该时机保证能捕获 EngineStarted / SpiderOpened 等早期信号。
	c.onBeforeStart(func(cc *Crawler) {
		r.applyGlobalHandlers(cc)
	})

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer close(done)
		defer r.removeCrawler(c)

		err := c.Crawl(ctx, sp)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			r.bootstrapFailed.Store(true)
			r.logger.Error("crawler finished with error",
				"spider", sp.Name(),
				"error", err,
			)
		} else {
			r.logger.Info("crawler finished", "spider", sp.Name())
		}
		done <- err
	}()

	return done
}

// ============================================================================
// 并发 / 顺序调度
// ============================================================================

// StartConcurrent 并发启动多个 Crawler，阻塞直到全部完成或 context 被取消。
//
// 返回值是多个 Crawler 错误的聚合（通过 errors.Join，忽略 context.Canceled / DeadlineExceeded）。
//
// 若启用 OS 信号处理（默认启用，可通过 WithOSSignalHandling(false) 关闭），
// 收到 SIGINT/SIGTERM 时 Runner 会取消派生 context，将取消信号广播给所有 Crawler，
// 并等待其 goroutine 退出；第二次收到同类信号将强制退出进程（exit code 130）。
//
// 注意：当前 Runner 层面的「停止」仅负责取消 context 的传播与 goroutine 汇合；
// Engine 内部 in-flight 请求的排空、Pipeline 排空与超时强制退出等完整优雅关闭语义
// 将在 Phase 3（P3-007e）补齐，此处不提供。
func (r *Runner) StartConcurrent(ctx context.Context, jobs ...Job) error {
	if len(jobs) == 0 {
		return nil
	}
	if err := r.validateJobs(jobs); err != nil {
		return err
	}

	ctx, cancel := r.installSignalHandler(ctx)
	defer cancel()

	dones := make([]<-chan error, 0, len(jobs))
	for _, job := range jobs {
		dones = append(dones, r.Crawl(ctx, job.Crawler, job.Spider))
	}

	return joinErrors(dones)
}

// StartSequentially 按给定顺序依次启动多个 Crawler，前一个完成后再启动下一个。
//
// 如果 ctx 在中途被取消，当前正在运行的 Crawler 会收到取消信号并退出，
// 后续未启动的 Crawler 将不会被启动。
// 返回值是已执行 Crawler 错误的聚合（通过 errors.Join，忽略 context.Canceled / DeadlineExceeded）。
//
// 注意：Engine 内部 in-flight 请求的排空与 Pipeline 排空等完整优雅关闭语义
// 将在 Phase 3（P3-007e）补齐。
func (r *Runner) StartSequentially(ctx context.Context, jobs ...Job) error {
	if len(jobs) == 0 {
		return nil
	}
	if err := r.validateJobs(jobs); err != nil {
		return err
	}

	ctx, cancel := r.installSignalHandler(ctx)
	defer cancel()

	var errs []error
	for i, job := range jobs {
		if ctx.Err() != nil {
			r.logger.Info("sequential run cancelled, remaining jobs skipped",
				"remaining", len(jobs)-i,
				"reason", ctx.Err(),
			)
			return errors.Join(errs...)
		}

		done := r.Crawl(ctx, job.Crawler, job.Spider)
		if err := <-done; err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				errs = append(errs, fmt.Errorf("spider %q: %w", job.Spider.Name(), err))
			}
		}
	}
	return errors.Join(errs...)
}

// validateJobs 校验 jobs 切片：crawler/spider 不为 nil、crawler 不重复。
func (r *Runner) validateJobs(jobs []Job) error {
	seen := make(map[*Crawler]struct{}, len(jobs))
	for i, job := range jobs {
		if job.Crawler == nil {
			return fmt.Errorf("jobs[%d]: crawler must not be nil", i)
		}
		if job.Spider == nil {
			return fmt.Errorf("jobs[%d]: spider must not be nil", i)
		}
		if _, dup := seen[job.Crawler]; dup {
			return fmt.Errorf("jobs[%d]: crawler is referenced multiple times", i)
		}
		seen[job.Crawler] = struct{}{}
	}
	return nil
}

// joinErrors 从所有 done channel 中读取错误并聚合（忽略 context.Canceled / DeadlineExceeded）。
func joinErrors(dones []<-chan error) error {
	var errs []error
	for _, done := range dones {
		if err := <-done; err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// ============================================================================
// 停止与等待
// ============================================================================

// Stop 请求所有正在运行的 Crawler 优雅停止，立即返回（不等待完成）。
// 如需阻塞等待停止完成，请在 Stop 后调用 Wait。
func (r *Runner) Stop() {
	for _, c := range r.Crawlers() {
		c.Stop()
	}
}

// Wait 阻塞等待所有由 Runner 管理的 Crawler 完成（对应 Scrapy 的 join）。
//
// 如果 Runner 未启动任何 Crawler，Wait 立即返回。多次调用 Wait 是安全的。
func (r *Runner) Wait() {
	r.wg.Wait()
}

// Close 关闭 Runner，停止所有正在运行的 Crawler 并等待完成。
// Close 之后 Runner 不再接受新的 Crawler。
//
// Close 返回后，所有 Crawler 均已退出。多次调用 Close 是安全的。
func (r *Runner) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()

	r.Stop()
	r.Wait()
}

// ============================================================================
// OS 信号处理
// ============================================================================

// installSignalHandler 基于 r.installOSSignals 决定是否安装 SIGINT/SIGTERM 处理器。
// 返回派生的 ctx 和配套的 cancel 函数；调用方 defer cancel() 即可。
func (r *Runner) installSignalHandler(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	if !r.installOSSignals {
		return ctx, cancel
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		count := 0
		for {
			select {
			case s, ok := <-sigCh:
				if !ok {
					return
				}
				count++
				if count == 1 {
					r.logger.Info("received OS signal, shutting down all crawlers gracefully",
						"signal", s.String(),
					)
					cancel()
				} else {
					r.logger.Warn("received OS signal twice, forcing exit",
						"signal", s.String(),
					)
					os.Exit(130)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// 返回的 cancel 需要同时停止 signal.Notify
	wrappedCancel := func() {
		signal.Stop(sigCh)
		cancel()
	}
	return ctx, wrappedCancel
}
