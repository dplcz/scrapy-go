package downloader

import (
	"context"
	"fmt"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// downloadTaskPool 是 downloadTask 对象池。
// 通过复用 downloadTask 结构体和其内部的 resultCh channel，
// 避免每个请求都分配新的 downloadTask 和 channel，减少 GC 压力。
var downloadTaskPool = sync.Pool{
	New: func() any {
		return &downloadTask{
			resultCh: make(chan downloadResult, 1),
		}
	},
}

// downloadTask 表示一个排队中的下载任务。
// 通过 downloadTaskPool 进行对象复用，减少内存分配。
type downloadTask struct {
	ctx      context.Context
	request  *shttp.Request
	resultCh chan downloadResult
}

// Reset 重置 downloadTask 的字段，准备归还对象池。
// resultCh 不需要重新创建，只需确保 channel 中无残留数据。
func (t *downloadTask) Reset() {
	t.ctx = nil
	t.request = nil
	// 排空 resultCh 中可能残留的数据（正常流程不会有残留，防御性编程）
	select {
	case <-t.resultCh:
	default:
	}
}

// getDownloadTask 从对象池获取一个 downloadTask 并初始化。
func getDownloadTask(ctx context.Context, request *shttp.Request) *downloadTask {
	task := downloadTaskPool.Get().(*downloadTask)
	task.ctx = ctx
	task.request = request
	return task
}

// putDownloadTask 将 downloadTask 归还对象池。
func putDownloadTask(task *downloadTask) {
	task.Reset()
	downloadTaskPool.Put(task)
}

// downloadResult 表示下载任务的结果。
type downloadResult struct {
	response *shttp.Response
	err      error
}

// Slot 控制单个域名/IP 的并发和延迟。
//
// Worker Pool 化设计（P4-007i）：
//   - 请求先入队到 queue channel
//   - 启动固定数量的 worker goroutine（N = concurrency），从 queue 直接消费 task
//   - worker 复用整个 Slot 生命周期，消除 per-request goroutine 创建/销毁开销
//   - 通过 gateMu + lastSeen 串行化 delay 等待，保证同一 Slot 内请求间隔语义
//   - 不同 Slot 之间完全并行（每个 Slot 有自己的 worker pool）
//
// 与旧设计（per-request `go func()`）的对比：
//   - 减少 goroutine 创建/销毁：每请求节省 ~2 us 调度开销
//   - 稳定的 goroutine 数量，降低 GC 扫描压力
//   - 替换 transferSem（semaphore.Weighted），由 worker 数量天然限制并发
//
// 对应 Scrapy 的 Slot 类。
type Slot struct {
	mu             sync.Mutex
	concurrency    int64
	delay          time.Duration
	randomizeDelay bool

	active       map[*shttp.Request]struct{} // 所有活跃请求（包括排队和传输中的）
	transferring map[*shttp.Request]struct{} // 正在传输的请求
	lastSeen     time.Time                   // 上一次实际发出请求的时间戳

	// queue 是请求排队的 channel，worker pool 从中消费
	queue chan *downloadTask

	// gateMu 在 delay > 0 时串行化 worker 的 delay 等待路径，
	// 确保同一时刻只有一个 worker 在计算 penalty 并 sleep，避免多 worker
	// 同时苏醒导致请求间隔被压缩。delay == 0 时不使用此锁（快速路径）。
	gateMu sync.Mutex

	// downloadFn 是实际执行下载的函数，由 Downloader 注入
	downloadFn func(ctx context.Context, request *shttp.Request) (*shttp.Response, error)

	// done 用于关闭 worker pool
	done chan struct{}

	// wg 用于等待所有 worker 退出
	wg sync.WaitGroup

	// closed 标记 Slot 是否已关闭
	closed   bool
	closedMu sync.Mutex
}

// NewSlot 创建一个新的下载 Slot 并启动 worker pool。
//
// worker pool 大小等于 concurrency，每个 worker 是一个长生命周期的 goroutine，
// 直接从 queue channel 消费 task 并执行下载。
func NewSlot(
	concurrency int,
	delay time.Duration,
	randomizeDelay bool,
	downloadFn func(ctx context.Context, request *shttp.Request) (*shttp.Response, error),
) *Slot {
	if concurrency <= 0 {
		concurrency = 8
	}

	s := &Slot{
		concurrency:    int64(concurrency),
		delay:          delay,
		randomizeDelay: randomizeDelay,
		active:         make(map[*shttp.Request]struct{}),
		transferring:   make(map[*shttp.Request]struct{}),
		lastSeen:       time.Time{}, // 零值，第一个请求不需要等待
		queue:          make(chan *downloadTask, 1024),
		downloadFn:     downloadFn,
		done:           make(chan struct{}),
	}

	// 启动 N 个 worker goroutine（N = concurrency），复用整个 Slot 生命周期
	s.wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go s.worker()
	}

	return s
}

// Enqueue 将请求入队，阻塞等待结果返回。
// 这是外部调用的主要接口。
// 使用 downloadTaskPool 复用 downloadTask 对象和 resultCh channel，
// 避免每请求分配，减少约 10% 的内存分配开销。
func (s *Slot) Enqueue(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	// 从对象池获取 task（复用 downloadTask 结构体和 resultCh channel）
	task := getDownloadTask(ctx, request)

	// 入队
	select {
	case <-ctx.Done():
		putDownloadTask(task)
		return nil, ctx.Err()
	case <-s.done:
		putDownloadTask(task)
		return nil, context.Canceled
	case s.queue <- task:
	}

	// 等待结果
	select {
	case <-ctx.Done():
		// ctx 取消时 task 可能正在被 worker 处理，
		// 不能归还 task，由 GC 回收（此场景极少发生）。
		return nil, ctx.Err()
	case result := <-task.resultCh:
		// 正常完成，归还 task 到对象池
		putDownloadTask(task)
		return result.response, result.err
	}
}

// worker 是 Worker Pool 中单个 goroutine 的主循环。
// 从共享 queue channel 消费 task，执行下载流程：延迟控制 → 标记传输中 → 下载 → 写入结果。
//
// panic recovery：捕获 worker 内的 panic 并自动重启，确保 Slot 对外服务不中断。
func (s *Slot) worker() {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			// worker 意外 panic（理论上 processTask 已经隔离了下载链 panic，
			// 这里只可能是更底层的运行时异常）。重启 worker 维持 pool 容量。
			_ = debug.Stack()
			s.wg.Add(1)
			go s.worker()
		}
	}()
	for {
		select {
		case <-s.done:
			return
		case task := <-s.queue:
			s.processTask(task)
		}
	}
}

// processTask 处理单个下载任务（在 worker goroutine 中同步执行）。
//
// 关键行为：
//   - 当 delay > 0 时，通过 gateMu 串行化 delay 等待路径，确保同一 Slot 内请求间隔不被压缩
//   - 当 delay == 0 时，跳过 gate 走快速路径，多 worker 完全并行
//   - 下载链中的 panic 在本函数内被捕获并转换为 error，不会影响 worker 后续循环
func (s *Slot) processTask(task *downloadTask) {
	// 1. 读取当前延迟（受 mu 保护，因为 AutoThrottle 可能动态修改 delay）
	s.mu.Lock()
	currentDelay := s.delay
	s.mu.Unlock()

	// 2. 计算并等待延迟（仅当 delay > 0）
	if currentDelay > 0 {
		s.gateMu.Lock()
		delay := s.getDownloadDelay()
		s.mu.Lock()
		lastSeen := s.lastSeen
		s.mu.Unlock()

		// 只有 lastSeen 不是零值时才计算 penalty
		if !lastSeen.IsZero() {
			penalty := delay - time.Since(lastSeen)
			if penalty > 0 {
				// 等待延迟期间响应 ctx/done 取消，避免阻塞关闭流程
				timer := time.NewTimer(penalty)
				select {
				case <-timer.C:
				case <-task.ctx.Done():
					timer.Stop()
					s.gateMu.Unlock()
					task.resultCh <- downloadResult{err: task.ctx.Err()}
					return
				case <-s.done:
					timer.Stop()
					s.gateMu.Unlock()
					task.resultCh <- downloadResult{err: context.Canceled}
					return
				}
			}
		}

		// 在持有 gateMu 期间更新 lastSeen，确保下一个等待的 worker 看到最新时间戳
		s.mu.Lock()
		s.lastSeen = time.Now()
		s.mu.Unlock()
		s.gateMu.Unlock()
	} else {
		// 快速路径：delay == 0 时无需 gate 锁，仅更新 lastSeen 用于诊断
		s.mu.Lock()
		s.lastSeen = time.Now()
		s.mu.Unlock()
	}

	// 2. 标记为传输中
	s.AddTransferring(task.request)

	// 3. 同步执行下载（不再 fork goroutine —— 由 worker pool 提供并发）
	// 保存 request 引用，因为 task 可能在写入 resultCh 后被 Enqueue 归还并重置
	request := task.request

	var result downloadResult

	// panic recovery: 防止下载处理器中的 panic 导致 worker goroutine 崩溃
	func() {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				result = downloadResult{
					err: fmt.Errorf("panic in download handler: %v\n%s", r, stack),
				}
			}
		}()

		// 应用超时，确保超时仅覆盖网络传输阶段
		downloadCtx := task.ctx
		if v, ok := task.request.GetMeta("download_timeout"); ok {
			if timeout, ok := v.(time.Duration); ok && timeout > 0 {
				var cancel context.CancelFunc
				downloadCtx, cancel = context.WithTimeout(downloadCtx, timeout)
				defer cancel()
			}
		}

		// 执行实际下载
		resp, err := s.downloadFn(downloadCtx, task.request)
		result = downloadResult{response: resp, err: err}
	}()

	// 先完成清理操作（使用保存的 request 引用）
	s.RemoveTransferring(request)

	// 最后写入 resultCh，通知 Enqueue 调用者。
	// 写入后 Enqueue 可能立即归还 task，因此此后不能再访问 task 的任何字段。
	task.resultCh <- result
}

// getDownloadDelay 返回当前的下载延迟。
// 如果启用了随机化延迟，返回 [0.5*delay, 1.5*delay) 范围内的随机值。
// 线程安全：通过 mu 保护 delay 字段的读取（AutoThrottle 可能动态修改）。
func (s *Slot) getDownloadDelay() time.Duration {
	s.mu.Lock()
	delay := s.delay
	randomize := s.randomizeDelay
	s.mu.Unlock()

	if randomize && delay > 0 {
		factor := 0.5 + rand.Float64() // [0.5, 1.5)
		return time.Duration(float64(delay) * factor)
	}
	return delay
}

// DownloadDelay 返回配置的下载延迟（公开方法，用于外部查询）。
func (s *Slot) DownloadDelay() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delay
}

// SetDelay 动态调整下载延迟。
// 由 AutoThrottle 扩展通过 Downloader.AdjustDelay 间接调用。
// 线程安全：通过 mu 保护 delay 字段的写入。
func (s *Slot) SetDelay(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = delay
}

// FreeTransferSlots 返回可用的传输槽位数。
func (s *Slot) FreeTransferSlots() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int(s.concurrency) - len(s.transferring)
}

// AddActive 将请求添加到活跃集合。
func (s *Slot) AddActive(request *shttp.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[request] = struct{}{}
}

// RemoveActive 从活跃集合中移除请求。
func (s *Slot) RemoveActive(request *shttp.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, request)
}

// AddTransferring 将请求添加到传输中集合。
func (s *Slot) AddTransferring(request *shttp.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transferring[request] = struct{}{}
}

// RemoveTransferring 从传输中集合移除请求。
func (s *Slot) RemoveTransferring(request *shttp.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.transferring, request)
}

// ActiveCount 返回活跃请求数。
func (s *Slot) ActiveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.active)
}

// TransferringCount 返回传输中请求数。
func (s *Slot) TransferringCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.transferring)
}

// IsIdle 检查 Slot 是否空闲（无活跃请求）。
func (s *Slot) IsIdle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.active) == 0
}

// LastSeen 返回最后一次活动时间。
func (s *Slot) LastSeen() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastSeen.IsZero() {
		return time.Now() // 从未使用过的 Slot 返回当前时间
	}
	return s.lastSeen
}

// Close 关闭 Slot，停止 worker pool 并等待所有 worker 退出。
func (s *Slot) Close() {
	s.closedMu.Lock()
	if s.closed {
		s.closedMu.Unlock()
		return
	}
	s.closed = true
	close(s.done)
	s.closedMu.Unlock()

	// 等待所有 worker 退出（Close 是关闭流程，可以阻塞等待）
	s.wg.Wait()
}
