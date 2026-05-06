package downloader

import (
	"context"
	"fmt"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// downloadTask 表示一个排队中的下载任务。
type downloadTask struct {
	ctx      context.Context
	request  *shttp.Request
	resultCh chan downloadResult
}

// downloadResult 表示下载任务的结果。
type downloadResult struct {
	response *shttp.Response
	err      error
}

// Slot 控制单个域名/IP 的并发和延迟。
// 采用 Scrapy 原版的队列驱动模型：
//   - 请求先入队到 queue channel
//   - 由 processQueue goroutine 串行出队
//   - 通过 lastSeen 时间戳精确控制同一 Slot 内两次请求的最小间隔
//   - 通过 semaphore.Weighted 控制并发传输数（支持 context 取消和加权获取）
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

	// queue 是请求排队的 channel，processQueue goroutine 从中消费
	queue chan *downloadTask

	// transferSem 使用 semaphore.Weighted 控制并发传输数，
	// 相比 channel 信号量支持 context 取消（Acquire 时可响应 ctx.Done()）。
	transferSem *semaphore.Weighted

	// downloadFn 是实际执行下载的函数，由 Downloader 注入
	downloadFn func(ctx context.Context, request *shttp.Request) (*shttp.Response, error)

	// done 用于关闭 processQueue goroutine
	done chan struct{}
	// closed 标记 Slot 是否已关闭
	closed bool
}

// NewSlot 创建一个新的下载 Slot 并启动队列处理 goroutine。
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
		transferSem:    semaphore.NewWeighted(int64(concurrency)),
		downloadFn:     downloadFn,
		done:           make(chan struct{}),
	}

	// 启动队列处理 goroutine
	go s.processQueue()

	return s
}

// Enqueue 将请求入队，阻塞等待结果返回。
// 这是外部调用的主要接口。
func (s *Slot) Enqueue(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	task := &downloadTask{
		ctx:      ctx,
		request:  request,
		resultCh: make(chan downloadResult, 1),
	}

	// 入队
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, context.Canceled
	case s.queue <- task:
	}

	// 等待结果
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-task.resultCh:
		return result.response, result.err
	}
}

// processQueue 是 Slot 的核心调度循环。
// 采用 Scrapy 原版的策略：
//   - 从队列中取出请求
//   - 计算距离上次请求还需等待多久（penalty = delay - (now - lastSeen)）
//   - 等待 penalty 时间
//   - 获取传输信号量（控制并发）
//   - 更新 lastSeen 并启动下载
//
// 关键行为：当配置了 delay 时，同一 Slot 内的请求自动串行化出队，
// 确保两次请求之间至少间隔 delay 时间。
func (s *Slot) processQueue() {
	defer func() {
		if r := recover(); r != nil {
			// processQueue 是长期运行的 goroutine，恢复后重启
			_ = debug.Stack() // 记录堆栈信息
			go s.processQueue()
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

// processTask 处理单个下载任务。
func (s *Slot) processTask(task *downloadTask) {
	// 1. 计算并等待延迟
	if s.delay > 0 {
		delay := s.getDownloadDelay()
		s.mu.Lock()
		elapsed := time.Since(s.lastSeen)
		s.mu.Unlock()

		// 只有 lastSeen 不是零值时才计算 penalty
		if !s.lastSeen.IsZero() {
			penalty := delay - elapsed
			if penalty > 0 {
				time.Sleep(penalty)
			}
		}
	}

	// 2. 获取传输信号量（使用 semaphore.Weighted，支持 context 取消）
	if err := s.transferSem.Acquire(task.ctx, 1); err != nil {
		// context 已取消，直接返回错误
		task.resultCh <- downloadResult{err: err}
		return
	}

	// 3. 更新 lastSeen（在实际发出请求的时刻）
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()

	// 4. 标记为传输中
	s.AddTransferring(task.request)

	// 5. 启动下载（在新 goroutine 中执行，以便 processQueue 可以继续处理下一个任务）
	// 注意：当 delay > 0 时，下一个任务会在 processQueue 中等待 delay，
	// 但实际下载可以并行执行（受 transferSem 限制）。
	// 当 delay == 0 时，请求会尽快出队并并行下载。
	go func() {
		defer func() {
			// 释放传输信号量
			s.transferSem.Release(1)
			// 移除传输中标记
			s.RemoveTransferring(task.request)
		}()

		// panic recovery: 防止下载处理器中的 panic 导致进程崩溃
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				task.resultCh <- downloadResult{
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
		task.resultCh <- downloadResult{response: resp, err: err}
	}()
}

// getDownloadDelay 返回当前的下载延迟。
// 如果启用了随机化延迟，返回 [0.5*delay, 1.5*delay) 范围内的随机值。
func (s *Slot) getDownloadDelay() time.Duration {
	if s.randomizeDelay && s.delay > 0 {
		factor := 0.5 + rand.Float64() // [0.5, 1.5)
		return time.Duration(float64(s.delay) * factor)
	}
	return s.delay
}

// DownloadDelay 返回配置的下载延迟（公开方法，用于外部查询）。
func (s *Slot) DownloadDelay() time.Duration {
	return s.delay
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

// Close 关闭 Slot，停止 processQueue goroutine。
func (s *Slot) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.done)
	}
}
