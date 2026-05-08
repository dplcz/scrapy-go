// Package scraper 实现了 scrapy-go 框架的 Scraper 组件。
//
// # 概述
//
// scraper 包负责处理下载完成的响应，是连接 Downloader 和 Spider 回调的桥梁。
// [Scraper] 类型协调 Spider 中间件链和 Item Pipeline，将下载结果转化为结构化数据。
// 对应 Scrapy Python 版本中 scrapy.core.scraper 模块的 Scraper 类。
//
// # 架构定位
//
// Scraper 位于 Engine 和 Spider/Pipeline 之间：
//
//	Engine
//	  │
//	  │ (Response + Request)
//	  ▼
//	┌──────────────────────────────────────────┐
//	│              Scraper  ← 本包              │
//	├──────────────────────────────────────────┤
//	│  1. Spider 中间件链 (ProcessSpiderInput) │
//	│  2. Spider 回调 (Callback / Parse)       │
//	│  3. Spider 中间件链 (ProcessOutput)      │
//	│  4. 输出分发                              │
//	│     ├─ Request → 返回 Engine 重新调度     │
//	│     └─ Item → Pipeline 并发处理          │
//	└──────────────────────────────────────────┘
//
// # 核心职责
//
// Scraper 承担以下核心职责：
//   - 通过 Spider 中间件链的 ProcessSpiderInput 预处理响应
//   - 确定并调用请求的回调函数（Request.Callback 或 Spider.Parse）
//   - 通过 Spider 中间件链的 ProcessOutput 后处理输出
//   - 将 Spider 输出分发到正确的目标：
//   - Request 类型输出返回给 Engine 重新调度
//   - Item 类型输出发送到 Item Pipeline 处理
//   - 处理下载错误（调用 Request.Errback）
//   - 管理活跃响应大小，提供回退信号（NeedsBackout）
//
// # 并发控制
//
// Scraper 使用 [semaphore.Weighted] 控制 Item 的并发处理数量，
// 对齐 Scrapy 的 CONCURRENT_ITEMS 配置（默认 100）：
//   - 每个 Item 在独立 goroutine 中通过 Pipeline 链处理
//   - Pipeline 链内部按优先级串行执行（保证处理顺序）
//   - Item 之间并发执行（提高吞吐量）
//   - 使用 semaphore.Weighted 替代 channel 信号量，支持 context 取消
//
// # 回退机制
//
// Scraper 通过 [Scraper.NeedsBackout] 方法向 Engine 报告是否需要回退。
// 当活跃响应的总大小超过 maxActiveSize（默认 5MB，可通过
// SCRAPER_SLOT_MAX_ACTIVE_SIZE 配置）时，Engine 会暂停从 Scheduler 取新请求，
// 防止内存过载。
//
// # 优雅关闭
//
// [Scraper.Close] 方法会等待所有 in-flight Item 处理完毕后再释放 Pipeline 资源：
//   - 使用 sync.WaitGroup 追踪 in-flight Item 数量
//   - 支持 context 超时控制，防止无限等待
//   - 超时后记录警告日志并强制关闭
//
// # 错误处理
//
// Scraper 提供两种错误处理路径：
//   - Spider 回调异常：记录日志、更新统计、发送 SpiderError 信号
//   - 下载错误：调用 Request.Errback（如果设置），否则记录错误日志
//
// 特殊错误类型：
//   - [errors.ErrCloseSpider]：触发 Spider 关闭流程
//   - [errors.ErrDropItem]：Pipeline 丢弃 Item（正常行为，不记录为错误）
//
// # Panic 恢复
//
// Pipeline 处理 goroutine 内置 panic recovery，防止用户 Pipeline 代码中的
// panic 导致整个进程崩溃。被恢复的 panic 会记录到日志并计入统计。
//
// # 与 Scrapy 的差异
//
//   - 使用 semaphore.Weighted 替代 Twisted DeferredSemaphore
//   - Item 在独立 goroutine 中并发处理（真正的并行），而非 Twisted 的协作式并发
//   - 使用 sync.WaitGroup 替代 Deferred 集合追踪 in-flight Item
//   - 使用 atomic.Int64 追踪活跃响应大小（无锁，高性能）
//   - 回调解析支持 NoCallback 哨兵值（跳过回调处理）
package scraper
