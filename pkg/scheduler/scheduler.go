// Package scheduler 实现了 scrapy-go 框架的请求调度系统。
//
// 调度器负责请求的入队、去重和出队，是 Engine 和 Downloader 之间的桥梁。
// 对应 Scrapy Python 版本中 scrapy.core.scheduler 模块的功能。
package scheduler

import (
	"context"
	"log/slog"
	"sync"

	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/stats"
)

// ============================================================================
// Scheduler 接口
// ============================================================================

// Scheduler 定义调度器接口。
// 调度器负责存储从 Engine 接收的请求，并在 Engine 请求时返回它们。
//
// 对应 Scrapy 的 BaseScheduler 抽象类。
type Scheduler interface {
	// Open 初始化调度器，在 Spider 打开时调用。
	Open(ctx context.Context) error

	// Close 关闭调度器，在 Spider 关闭时调用。
	Close(ctx context.Context, reason string) error

	// EnqueueRequest 将请求入队。
	// 返回 true 表示请求成功入队，false 表示请求被过滤（如去重）。
	// 当返回 false 时，Engine 会发出 request_dropped 信号。
	EnqueueRequest(request *scrapy_http.Request) bool

	// NextRequest 返回下一个待处理的请求。
	// 返回 nil 表示当前没有可用的请求。
	NextRequest() *scrapy_http.Request

	// HasPendingRequests 返回是否有待处理的请求。
	HasPendingRequests() bool

	// Len 返回队列中的请求总数。
	Len() int
}

// ============================================================================
// DefaultScheduler 实现
// ============================================================================

// DefaultScheduler 是默认的调度器实现。
//
// 使用内存优先级队列存储请求，通过可插拔的 DupeFilter 进行去重。
// 请求按优先级排序，高优先级的请求先出队。
//
// 对应 Scrapy 的 Scheduler 类（仅内存队列部分，MVP 不含磁盘队列）。
type DefaultScheduler struct {
	mu         sync.Mutex
	dupeFilter DupeFilter
	pq         *PriorityQueue
	stats      stats.Collector
	logger     *slog.Logger
	debug      bool // 是否输出调试日志
}

// DefaultSchedulerOption 是 DefaultScheduler 的可选配置函数。
type DefaultSchedulerOption func(*DefaultScheduler)

// WithDupeFilter 设置去重过滤器。
func WithDupeFilter(df DupeFilter) DefaultSchedulerOption {
	return func(s *DefaultScheduler) {
		s.dupeFilter = df
	}
}

// WithStats 设置统计收集器。
func WithStats(sc stats.Collector) DefaultSchedulerOption {
	return func(s *DefaultScheduler) {
		s.stats = sc
	}
}

// WithSchedulerLogger 设置日志记录器。
func WithSchedulerLogger(logger *slog.Logger) DefaultSchedulerOption {
	return func(s *DefaultScheduler) {
		s.logger = logger
	}
}

// WithDebug 设置是否输出调试日志。
func WithDebug(debug bool) DefaultSchedulerOption {
	return func(s *DefaultScheduler) {
		s.debug = debug
	}
}

// NewDefaultScheduler 创建一个新的默认调度器。
func NewDefaultScheduler(opts ...DefaultSchedulerOption) *DefaultScheduler {
	s := &DefaultScheduler{
		pq: NewPriorityQueue(),
	}

	for _, opt := range opts {
		opt(s)
	}

	// 设置默认值
	if s.dupeFilter == nil {
		s.dupeFilter = NewRFPDupeFilter(nil, false)
	}
	if s.stats == nil {
		s.stats = stats.NewDummyCollector()
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}

	return s
}

// Open 初始化调度器。
func (s *DefaultScheduler) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.dupeFilter.Open(ctx)
}

// Close 关闭调度器。
func (s *DefaultScheduler) Close(ctx context.Context, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.dupeFilter.Close(reason)
}

// EnqueueRequest 将请求入队。
//
// 处理流程：
//  1. 如果请求未设置 DontFilter，通过 DupeFilter 检查是否重复
//  2. 如果是重复请求，记录统计并返回 false
//  3. 否则将请求推入优先级队列，记录统计并返回 true
func (s *DefaultScheduler) EnqueueRequest(request *scrapy_http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 去重检查
	if !request.DontFilter && s.dupeFilter.RequestSeen(request) {
		s.stats.IncValue("dupefilter/filtered", 1, 0)
		if s.debug {
			s.logger.Debug("请求被去重过滤",
				"request", request.String(),
			)
		}
		return false
	}

	// 入队
	s.pq.Push(request)
	s.stats.IncValue("scheduler/enqueued", 1, 0)
	s.stats.IncValue("scheduler/enqueued/memory", 1, 0)

	return true
}

// NextRequest 返回下一个待处理的请求。
// 按优先级顺序返回，高优先级的请求先出队。
// 如果队列为空，返回 nil。
func (s *DefaultScheduler) NextRequest() *scrapy_http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	request := s.pq.Pop()
	if request != nil {
		s.stats.IncValue("scheduler/dequeued", 1, 0)
		s.stats.IncValue("scheduler/dequeued/memory", 1, 0)
	}
	return request
}

// HasPendingRequests 返回是否有待处理的请求。
func (s *DefaultScheduler) HasPendingRequests() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.pq.Len() > 0
}

// Len 返回队列中的请求总数。
func (s *DefaultScheduler) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.pq.Len()
}
