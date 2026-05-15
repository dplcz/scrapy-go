// Package scheduler 实现了 scrapy-go 框架的请求调度系统。
//
// # 概述
//
// scheduler 包是 Engine 和 Downloader 之间的桥梁，负责请求的入队、去重和出队。
// 对应 Scrapy Python 版本中 scrapy.core.scheduler 模块的功能。
//
// # 核心类型
//
// 本包提供以下核心类型：
//   - [Scheduler]：调度器接口，定义 Open/Close/EnqueueRequest/NextRequest 方法
//   - [DefaultScheduler]：默认调度器实现，支持内存队列 + 磁盘队列双层存储
//   - [DupeFilter]：去重过滤器接口
//   - [RFPDupeFilter]：基于请求指纹（SHA1）的去重过滤器实现
//   - [Queue]：统一队列接口（操作原始字节切片）
//   - [PriorityAwareQueue]：支持优先级感知的队列扩展接口
//   - [PriorityQueue]：内存优先级队列（有序切片，Pop O(1)，Push O(log N)）
//   - [DiskQueue]：磁盘持久化队列（支持断点续爬）
//   - [MemoryQueue]：内存 LIFO 队列
//   - [RequestSerializer]：请求序列化/反序列化器
//
// # 架构设计
//
// 调度器采用双层队列架构：
//
//	                    Engine
//	                      │
//	                      ▼
//	┌─────────────────────────────────────────┐
//	│           DefaultScheduler               │
//	├─────────────────────────────────────────┤
//	│  1. DupeFilter（去重检查）               │
//	│  2. 入队策略：                           │
//	│     ├─ 可序列化 → 磁盘队列（优先）       │
//	│     └─ 不可序列化 → 内存队列（回退）     │
//	│  3. 出队策略：                           │
//	│     ├─ 内存队列（优先）                  │
//	│     └─ 磁盘队列（次之）                  │
//	└─────────────────────────────────────────┘
//	       │                        │
//	       ▼                        ▼
//	┌─────────────┐        ┌──────────────────┐
//	│PriorityQueue│        │DiskQueue / Redis  │
//	│  (内存)      │        │  (持久化)         │
//	└─────────────┘        └──────────────────┘
//
// # 使用方式
//
// 基本使用（纯内存调度）：
//
//	sched := scheduler.NewDefaultScheduler()
//	sched.Open(ctx)
//	defer sched.Close(ctx, "finished")
//
//	sched.EnqueueRequest(req)  // 入队
//	next := sched.NextRequest() // 出队
//
// 启用断点续爬（磁盘队列）：
//
//	sched := scheduler.NewDefaultScheduler(
//	    scheduler.WithJobDir("/tmp/crawl-job-1"),
//	    scheduler.WithCallbackRegistry(registry),
//	)
//
// 注入外部队列（如 Redis）：
//
//	sched := scheduler.NewDefaultScheduler(
//	    scheduler.WithExternalQueue(redisQueue),
//	    scheduler.WithCallbackRegistry(registry),
//	)
//
// # 去重机制
//
// [RFPDupeFilter] 使用请求指纹进行去重：
//   - 指纹 = SHA1(规范化URL + Method + Body)
//   - 支持持久化（配合 JOBDIR 实现断点续爬时的去重状态恢复）
//   - Request.DontFilter = true 时跳过去重检查
//
// # 优先级调度
//
// [PriorityQueue] 使用有序切片实现优先级调度：
//   - Push：二分插入，O(log N)
//   - Pop：取最高优先级元素，O(1)
//   - 高优先级（数值大）的请求先出队
//
// # 内存队列溢出保护
//
// 通过 [WithMemoryQueueThreshold] 设置内存队列最大容量阈值：
//   - 当内存队列中的请求数超过阈值时，新入队的请求自动溢出到磁盘队列
//   - 未配置 jobDir 时自动创建临时磁盘队列目录，爬虫结束后自动清理
//   - 出队时内存队列仍然优先于磁盘队列，确保低延迟
//   - 适用于大规模爬取场景，防止内存队列无限增长导致 OOM
//
// 示例（限制内存队列最多 10000 个请求）：
//
//	sched := scheduler.NewDefaultScheduler(
//	    scheduler.WithMemoryQueueThreshold(10000),
//	)
//
// # 断点续爬
//
// 当配置了 JOBDIR 时，调度器支持断点续爬：
//   - 可序列化的请求存入磁盘队列
//   - 不可序列化的请求（如携带闭包回调）回退到内存队列
//   - DupeFilter 指纹集合持久化到磁盘
//   - 下次启动时自动恢复队列和去重状态
//
// # 与 Scrapy 的差异
//
//   - 使用 [Queue] 接口操作原始字节切片，替代 Python queuelib 直接存储对象
//   - 使用 [PriorityAwareQueue] 接口抽象持久化队列，支持 Redis 等外部后端
//   - 使用 [RequestSerializer] 显式序列化，替代 Python pickle
//   - 使用 [CallbackRegistry] 注册表模式恢复函数引用，替代 getattr 反射
//   - 使用 sync.Mutex 保护并发访问，替代 Twisted 的协作式并发
//   - DiskQueue 使用有序优先级切片，Pop O(1)，Push O(log N)
//
// # 并发安全
//
// [DefaultScheduler] 的所有公共方法均为并发安全：
//   - 内存优先级队列使用单个 sync.Mutex 保护，保证全局优先级排序正确性
//   - DupeFilter 使用 sync.Map 实现无锁并发去重，在队列锁外执行以减少临界区
//   - HasPendingRequests/Len 使用 atomic 计数器，无锁快速路径
//
// [RFPDupeFilter] 同样是并发安全的（内部使用 sync.Map）。
//
// # 优先级排序保证
//
// [DefaultScheduler] 使用单队列设计保证全局优先级排序的绝对正确性：
//   - 所有内存中的请求共享同一个优先级堆
//   - 新入队的高优先级请求能立即参与全局排序
//   - 不会出现高优先级请求被低优先级请求"饿死"的情况
//   - 适用于回调中产生高优先级请求的场景（如详情页优先于列表页）
package scheduler
