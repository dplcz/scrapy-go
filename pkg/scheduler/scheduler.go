// Package scheduler 实现了 scrapy-go 框架的请求调度系统。
//
// 调度器负责请求的入队、去重和出队，是 Engine 和 Downloader 之间的桥梁。
// 对应 Scrapy Python 版本中 scrapy.core.scheduler 模块的功能。
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
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
	EnqueueRequest(request *shttp.Request) bool

	// NextRequest 返回下一个待处理的请求。
	// 返回 nil 表示当前没有可用的请求。
	NextRequest() *shttp.Request

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
// 当配置了 JOBDIR 或外部队列时，同时使用持久化队列实现断点续爬：
//   - 可序列化的请求优先存入持久化队列
//   - 不可序列化的请求回退到内存队列
//   - 出队时内存队列优先于持久化队列
//   - 关闭时持久化队列状态和 DupeFilter 状态
//
// 持久化队列通过 PriorityAwareQueue 接口抽象，支持磁盘队列、Redis 队列等
// 不同后端的无缝替换。
//
// 对应 Scrapy 的 Scheduler 类。
type DefaultScheduler struct {
	mu         sync.Mutex
	dupeFilter DupeFilter
	pq         *PriorityQueue     // 内存优先级队列
	dq         PriorityAwareQueue // 持久化队列（可选，JOBDIR 或外部队列启用时使用）
	serializer *RequestSerializer
	stats      stats.Collector
	logger     *slog.Logger
	debug      bool   // 是否输出调试日志
	jobDir     string // 断点续爬目录（空字符串表示不启用磁盘队列）
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

// WithJobDir 设置断点续爬目录。
// 设置后启用磁盘队列，支持断点续爬功能。
// 对应 Scrapy 的 JOBDIR 配置。
func WithJobDir(jobDir string) DefaultSchedulerOption {
	return func(s *DefaultScheduler) {
		s.jobDir = jobDir
	}
}

// WithCallbackRegistry 设置回调函数注册表。
// 用于磁盘队列序列化/反序列化时恢复 Callback/Errback 函数引用。
func WithCallbackRegistry(registry *shttp.CallbackRegistry) DefaultSchedulerOption {
	return func(s *DefaultScheduler) {
		if s.serializer == nil {
			s.serializer = NewRequestSerializer(registry, s.logger)
		} else {
			s.serializer = NewRequestSerializer(registry, s.serializer.logger)
		}
	}
}

// WithExternalQueue 设置外部持久化队列。
//
// 通过此选项可以注入任意实现了 PriorityAwareQueue 接口的队列后端
// （如 Redis 分布式队列），替代默认的磁盘队列。
//
// 注意：如果同时设置了 WithJobDir 和 WithExternalQueue，
// 外部队列优先级更高，WithJobDir 将被忽略。
func WithExternalQueue(q PriorityAwareQueue) DefaultSchedulerOption {
	return func(s *DefaultScheduler) {
		s.dq = q
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
	if s.serializer == nil {
		s.serializer = NewRequestSerializer(nil, s.logger)
	}

	return s
}

// Open 初始化调度器。
//
// 队列初始化优先级：
//  1. 如果通过 WithExternalQueue 设置了外部队列，直接使用
//  2. 如果配置了 JOBDIR，创建磁盘队列
//  3. 否则仅使用内存队列
func (s *DefaultScheduler) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果已通过 WithExternalQueue 注入了外部队列，跳过磁盘队列初始化
	if s.dq != nil {
		if s.dq.Len() > 0 {
			s.logger.Info("resuming crawl from external queue",
				"pending_requests", s.dq.Len(),
			)
		}
	} else if s.jobDir != "" {
		// 初始化磁盘队列
		queueDir := filepath.Join(s.jobDir, "requests.queue")
		dq, err := NewDiskQueue(queueDir)
		if err != nil {
			return fmt.Errorf("failed to open disk queue: %w", err)
		}
		s.dq = dq

		if dq.Len() > 0 {
			s.logger.Info("resuming crawl from disk queue",
				"pending_requests", dq.Len(),
				"jobdir", s.jobDir,
			)
		}
	}

	return s.dupeFilter.Open(ctx)
}

// Close 关闭调度器。
// 如果启用了磁盘队列，会持久化队列状态和 DupeFilter 状态。
func (s *DefaultScheduler) Close(ctx context.Context, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error

	// 关闭磁盘队列（持久化数据）
	if s.dq != nil {
		if err := s.dq.Close(); err != nil {
			s.logger.Error("failed to close disk queue", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// 关闭去重过滤器
	if err := s.dupeFilter.Close(reason); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// EnqueueRequest 将请求入队。
//
// 处理流程：
//  1. 如果请求未设置 DontFilter，通过 DupeFilter 检查是否重复
//  2. 如果是重复请求，记录统计并返回 false
//  3. 如果启用了磁盘队列，尝试序列化并存入磁盘队列
//  4. 如果序列化失败或未启用磁盘队列，存入内存优先级队列
//  5. 记录统计并返回 true
//
// 对齐 Scrapy 的 Scheduler.enqueue_request：磁盘队列优先，序列化失败回退内存队列。
func (s *DefaultScheduler) EnqueueRequest(request *shttp.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 去重检查
	if !request.DontFilter && s.dupeFilter.RequestSeen(request) {
		s.stats.IncValue("dupefilter/filtered", 1, 0)
		if s.debug {
			s.logger.Debug("request filtered by dupefilter",
				"request", request.String(),
			)
		}
		return false
	}

	// 尝试存入磁盘队列
	if s.dq != nil {
		if s.enqueueToDisk(request) {
			s.stats.IncValue("scheduler/enqueued", 1, 0)
			s.stats.IncValue("scheduler/enqueued/disk", 1, 0)
			return true
		}
		// 序列化失败，回退到内存队列
	}

	// 存入内存队列
	s.pq.Push(request)
	s.stats.IncValue("scheduler/enqueued", 1, 0)
	s.stats.IncValue("scheduler/enqueued/memory", 1, 0)

	return true
}

// NextRequest 返回下一个待处理的请求。
//
// 出队优先级：内存队列 > 磁盘队列。
// 这与 Scrapy 的行为一致：内存中的请求优先处理，
// 因为它们可能是不可序列化的请求或新入队的高优先级请求。
func (s *DefaultScheduler) NextRequest() *shttp.Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 优先从内存队列出队
	request := s.pq.Pop()
	if request != nil {
		s.stats.IncValue("scheduler/dequeued", 1, 0)
		s.stats.IncValue("scheduler/dequeued/memory", 1, 0)
		return request
	}

	// 内存队列为空，尝试从磁盘队列出队
	if s.dq != nil {
		request = s.dequeueFromDisk()
		if request != nil {
			s.stats.IncValue("scheduler/dequeued", 1, 0)
			s.stats.IncValue("scheduler/dequeued/disk", 1, 0)
			return request
		}
	}

	return nil
}

// HasPendingRequests 返回是否有待处理的请求。
func (s *DefaultScheduler) HasPendingRequests() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pq.Len() > 0 {
		return true
	}
	if s.dq != nil && s.dq.Len() > 0 {
		return true
	}
	return false
}

// Len 返回队列中的请求总数（内存 + 磁盘）。
func (s *DefaultScheduler) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := s.pq.Len()
	if s.dq != nil {
		total += s.dq.Len()
	}
	return total
}

// HasDiskQueue 返回是否启用了磁盘队列。
//
// Deprecated: 请使用 HasExternalQueue，该方法保留用于向后兼容。
func (s *DefaultScheduler) HasDiskQueue() bool {
	return s.HasExternalQueue()
}

// HasExternalQueue 返回是否启用了持久化队列（磁盘队列或外部队列）。
func (s *DefaultScheduler) HasExternalQueue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dq != nil
}

// ============================================================================
// 内部方法
// ============================================================================

// enqueueToDisk 尝试将请求序列化并存入磁盘队列。
// 返回 true 表示成功，false 表示序列化失败。
func (s *DefaultScheduler) enqueueToDisk(request *shttp.Request) bool {
	data, err := s.serializer.Serialize(request)
	if err != nil {
		if s.debug {
			s.logger.Debug("unable to serialize request, falling back to memory queue",
				"request", request.String(),
				"error", err,
			)
		}
		s.stats.IncValue("scheduler/unserializable", 1, 0)
		return false
	}

	if err := s.dq.PushWithPriority(data, request.Priority); err != nil {
		s.logger.Error("failed to push request to disk queue",
			"request", request.String(),
			"error", err,
		)
		return false
	}

	return true
}

// dequeueFromDisk 从磁盘队列出队并反序列化请求。
func (s *DefaultScheduler) dequeueFromDisk() *shttp.Request {
	data, _, err := s.dq.PopWithPriority()
	if err != nil {
		s.logger.Error("failed to pop from disk queue", "error", err)
		return nil
	}
	if data == nil {
		return nil
	}

	request, err := s.serializer.Deserialize(data)
	if err != nil {
		s.logger.Error("failed to deserialize request from disk queue",
			"error", err,
		)
		return nil
	}

	return request
}
