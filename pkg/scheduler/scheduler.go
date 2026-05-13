package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

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
// DefaultScheduler 实现（双锁分离：入队/出队解耦）
// ============================================================================

// DefaultScheduler 是默认的调度器实现。
//
// 使用双锁分离设计（类似 Java LinkedBlockingQueue）：
//   - enqueueMu 保护入队路径（EnqueueRequest）
//   - dequeueMu 保护出队路径（NextRequest）
//   - pendingCount 使用 atomic 提供无锁的 HasPendingRequests/Len
//
// 入队/出队可并行执行，消除调度循环与 Spider 回调之间的锁竞争。
//
// 内部使用双队列设计：
//   - inBuffer：入队缓冲区，由 enqueueMu 保护
//   - outQueue：出队队列，由 dequeueMu 保护
//   - 当 outQueue 为空时，获取 enqueueMu 将 inBuffer 内容转移到 outQueue
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
// 内存队列溢出保护：
//   - 通过 WithMemoryQueueThreshold 设置内存队列最大容量阈值
//   - 当内存队列（inBuffer + outQueue）中的请求数超过阈值时，
//     新入队的请求自动溢出到磁盘队列
//   - 未配置 jobDir 时自动创建临时磁盘队列目录，爬虫结束后清理
//   - 出队时内存队列仍然优先于磁盘队列，确保低延迟
//
// 对应 Scrapy 的 Scheduler 类。
type DefaultScheduler struct {
	// 双锁分离：入队锁和出队锁独立，消除入队/出队的锁竞争
	enqueueMu sync.Mutex // 保护入队路径（inBuffer + dupeFilter 写入）
	dequeueMu sync.Mutex // 保护出队路径（outQueue + 磁盘队列出队）

	// pendingCount 使用 atomic 提供无锁的 HasPendingRequests/Len
	pendingCount atomic.Int64

	// memoryCount 追踪内存队列中的请求数（inBuffer + outQueue），由 enqueueMu 保护写入
	memoryCount atomic.Int64

	dupeFilter DupeFilter
	inBuffer   *PriorityQueue     // 入队缓冲区，由 enqueueMu 保护
	outQueue   *PriorityQueue     // 出队队列，由 dequeueMu 保护
	dq         PriorityAwareQueue // 持久化队列（可选，JOBDIR 或外部队列启用时使用）
	serializer *RequestSerializer
	stats      stats.Collector
	logger     *slog.Logger
	debug      bool   // 是否输出调试日志
	jobDir     string // 断点续爬目录（空字符串表示不启用磁盘队列）

	// 内存队列溢出保护
	memoryQueueThreshold int    // 内存队列最大容量阈值（0 表示不限制）
	tempDir              string // 自动创建的临时磁盘队列目录（需在 Close 时清理）
	ownsTempDir          bool   // 标记临时目录是否由本调度器创建（用于 Close 时判断是否清理）
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

// WithMemoryQueueThreshold 设置内存队列最大容量阈值。
//
// 当内存队列中的请求数超过此阈值时，新入队的可序列化请求将自动溢出到磁盘队列。
// 如果未配置 jobDir 且未注入外部队列，将自动创建临时磁盘队列目录，
// 爬虫结束时自动清理。
//
// 参数 n 必须为正整数，设置为 0 或负数将被忽略（等同于不限制）。
//
// 典型使用场景：
//   - 大规模爬取时防止内存队列无限增长导致 OOM
//   - 配合 AutoThrottle 使用，在下载速度受限时缓冲过多的待处理请求
//
// 示例：
//
//	sched := scheduler.NewDefaultScheduler(
//	    scheduler.WithMemoryQueueThreshold(10000),
//	)
func WithMemoryQueueThreshold(n int) DefaultSchedulerOption {
	return func(s *DefaultScheduler) {
		if n > 0 {
			s.memoryQueueThreshold = n
		}
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
		inBuffer: NewPriorityQueue(),
		outQueue: NewPriorityQueue(),
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
//  3. 如果设置了 memoryQueueThreshold 但无磁盘队列，自动创建临时磁盘队列
//  4. 否则仅使用内存队列
func (s *DefaultScheduler) Open(ctx context.Context) error {
	// Open 在启动阶段调用，无并发，获取两把锁确保安全
	s.enqueueMu.Lock()
	s.dequeueMu.Lock()
	defer s.dequeueMu.Unlock()
	defer s.enqueueMu.Unlock()

	// 如果已通过 WithExternalQueue 注入了外部队列，跳过磁盘队列初始化
	if s.dq != nil {
		pending := s.dq.Len()
		if pending > 0 {
			s.pendingCount.Store(int64(pending))
			s.logger.Info("resuming crawl from external queue",
				"pending_requests", pending,
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

		pending := dq.Len()
		if pending > 0 {
			s.pendingCount.Store(int64(pending))
			s.logger.Info("resuming crawl from disk queue",
				"pending_requests", pending,
				"jobdir", s.jobDir,
			)
		}
	} else if s.memoryQueueThreshold > 0 {
		// 设置了内存队列阈值但未配置磁盘队列，自动创建临时目录
		tmpDir, err := os.MkdirTemp("", "scrapy-go-overflow-*")
		if err != nil {
			return fmt.Errorf("failed to create temp dir for overflow queue: %w", err)
		}
		s.tempDir = tmpDir
		s.ownsTempDir = true

		queueDir := filepath.Join(tmpDir, "requests.queue")
		dq, err := NewDiskQueue(queueDir)
		if err != nil {
			os.RemoveAll(tmpDir)
			s.tempDir = ""
			s.ownsTempDir = false
			return fmt.Errorf("failed to open overflow disk queue: %w", err)
		}
		s.dq = dq

		s.logger.Info("memory queue overflow protection enabled",
			"threshold", s.memoryQueueThreshold,
			"overflow_dir", tmpDir,
		)
	}

	return s.dupeFilter.Open(ctx)
}

// Close 关闭调度器。
// 如果启用了磁盘队列，会持久化队列状态和 DupeFilter 状态。
// 如果使用了自动创建的临时磁盘队列目录，会在关闭后清理。
func (s *DefaultScheduler) Close(ctx context.Context, reason string) error {
	// Close 在关闭阶段调用，获取两把锁确保安全
	s.enqueueMu.Lock()
	s.dequeueMu.Lock()
	defer s.dequeueMu.Unlock()
	defer s.enqueueMu.Unlock()

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

	// 清理自动创建的临时目录
	if s.ownsTempDir && s.tempDir != "" {
		if err := os.RemoveAll(s.tempDir); err != nil {
			s.logger.Error("failed to cleanup temp overflow dir",
				"dir", s.tempDir,
				"error", err,
			)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			s.logger.Debug("cleaned up temp overflow dir", "dir", s.tempDir)
		}
		s.tempDir = ""
		s.ownsTempDir = false
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
//  3. 如果启用了磁盘队列：
//     a. 未设置内存阈值：所有可序列化请求优先存入磁盘队列（原有行为）
//     b. 设置了内存阈值且未超阈值：存入内存队列
//     c. 设置了内存阈值且已超阈值：溢出到磁盘队列
//  4. 如果序列化失败或未启用磁盘队列，存入入队缓冲区（inBuffer）
//  5. 记录统计并返回 true
//
// 仅获取 enqueueMu，不阻塞出队路径。
// 对齐 Scrapy 的 Scheduler.enqueue_request：磁盘队列优先，序列化失败回退内存队列。
func (s *DefaultScheduler) EnqueueRequest(request *shttp.Request) bool {
	s.enqueueMu.Lock()
	defer s.enqueueMu.Unlock()

	// 去重检查（DupeFilter 由 enqueueMu 保护，仅入队路径写入）
	if !request.DontFilter && s.dupeFilter.RequestSeen(request) {
		s.stats.IncValue("dupefilter/filtered", 1, 0)
		if s.debug {
			s.logger.Debug("request filtered by dupefilter",
				"request", request.String(),
			)
		}
		return false
	}

	// 判断是否需要溢出到磁盘队列
	if s.dq != nil {
		if s.shouldOverflowToDisk() {
			// 内存队列已超阈值（或未设置阈值时的原有行为），溢出到磁盘
			if s.enqueueToDisk(request) {
				s.pendingCount.Add(1)
				s.stats.IncValue("scheduler/enqueued", 1, 0)
				s.stats.IncValue("scheduler/enqueued/disk", 1, 0)
				s.stats.IncValue("scheduler/overflow_to_disk", 1, 0)
				return true
			}
			// 序列化失败，回退到内存队列
		}
	}

	// 存入入队缓冲区
	s.inBuffer.Push(request)
	s.pendingCount.Add(1)
	s.memoryCount.Add(1)
	s.stats.IncValue("scheduler/enqueued", 1, 0)
	s.stats.IncValue("scheduler/enqueued/memory", 1, 0)

	return true
}

// NextRequest 返回下一个待处理的请求。
//
// 出队优先级：内存队列（outQueue + inBuffer 转移） > 磁盘队列。
// 这与 Scrapy 的行为一致：内存中的请求优先处理，
// 因为它们可能是不可序列化的请求或新入队的高优先级请求。
//
// 仅获取 dequeueMu，不阻塞入队路径。
// 当 outQueue 为空时，短暂获取 enqueueMu 将 inBuffer 转移到 outQueue。
func (s *DefaultScheduler) NextRequest() *shttp.Request {
	s.dequeueMu.Lock()
	defer s.dequeueMu.Unlock()

	// 优先从出队队列出队
	request := s.outQueue.Pop()
	if request != nil {
		s.pendingCount.Add(-1)
		s.memoryCount.Add(-1)
		s.stats.IncValue("scheduler/dequeued", 1, 0)
		s.stats.IncValue("scheduler/dequeued/memory", 1, 0)
		return request
	}

	// outQueue 为空，尝试从 inBuffer 转移
	s.enqueueMu.Lock()
	if s.inBuffer.Len() > 0 {
		// 交换 inBuffer 和 outQueue（O(1) 操作）
		s.inBuffer, s.outQueue = s.outQueue, s.inBuffer
	}
	s.enqueueMu.Unlock()

	// 再次尝试从 outQueue 出队（此时 outQueue 已是原 inBuffer 的内容）
	request = s.outQueue.Pop()
	if request != nil {
		s.pendingCount.Add(-1)
		s.memoryCount.Add(-1)
		s.stats.IncValue("scheduler/dequeued", 1, 0)
		s.stats.IncValue("scheduler/dequeued/memory", 1, 0)
		return request
	}

	// 内存队列为空，尝试从磁盘队列出队
	if s.dq != nil {
		request = s.dequeueFromDisk()
		if request != nil {
			s.pendingCount.Add(-1)
			s.stats.IncValue("scheduler/dequeued", 1, 0)
			s.stats.IncValue("scheduler/dequeued/disk", 1, 0)
			return request
		}
	}

	return nil
}

// HasPendingRequests 返回是否有待处理的请求。
// 使用 atomic 计数器，无锁快速路径。
func (s *DefaultScheduler) HasPendingRequests() bool {
	return s.pendingCount.Load() > 0
}

// Len 返回队列中的请求总数（内存 + 磁盘）。
// 使用 atomic 计数器，无锁快速路径。
func (s *DefaultScheduler) Len() int {
	return int(s.pendingCount.Load())
}

// HasDiskQueue 返回是否启用了磁盘队列。
//
// Deprecated: 请使用 HasExternalQueue，该方法保留用于向后兼容。
func (s *DefaultScheduler) HasDiskQueue() bool {
	return s.HasExternalQueue()
}

// HasExternalQueue 返回是否启用了持久化队列（磁盘队列或外部队列）。
func (s *DefaultScheduler) HasExternalQueue() bool {
	s.enqueueMu.Lock()
	defer s.enqueueMu.Unlock()
	return s.dq != nil
}

// MemoryQueueLen 返回当前内存队列中的请求数量。
// 用于监控和调试内存队列溢出保护的工作状态。
func (s *DefaultScheduler) MemoryQueueLen() int {
	return int(s.memoryCount.Load())
}

// MemoryQueueThreshold 返回配置的内存队列阈值。
// 返回 0 表示未设置阈值（不限制）。
func (s *DefaultScheduler) MemoryQueueThreshold() int {
	return s.memoryQueueThreshold
}

// ============================================================================
// 内部方法
// ============================================================================

// shouldOverflowToDisk 判断是否应该将请求溢出到磁盘队列。
//
// 决策逻辑：
//   - 如果未设置内存队列阈值（memoryQueueThreshold == 0），始终溢出（保持原有行为）
//   - 如果设置了阈值，仅当内存队列请求数超过阈值时才溢出
//
// 调用方必须持有 enqueueMu。
func (s *DefaultScheduler) shouldOverflowToDisk() bool {
	if s.memoryQueueThreshold == 0 {
		// 未设置阈值，保持原有行为：所有可序列化请求优先存入磁盘队列
		return true
	}
	// 设置了阈值，仅当内存队列超过阈值时溢出
	return int(s.memoryCount.Load()) >= s.memoryQueueThreshold
}

// enqueueToDisk 尝试将请求序列化并存入磁盘队列。
// 返回 true 表示成功，false 表示序列化失败。
// 调用方必须持有 enqueueMu。
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
// 调用方必须持有 dequeueMu。
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
