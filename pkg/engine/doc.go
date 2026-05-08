// Package engine 实现了 scrapy-go 框架的核心调度引擎。
//
// # 概述
//
// engine 包是 scrapy-go 框架的心脏，[Engine] 类型负责协调 Scheduler、Downloader、
// Scraper 和 Spider 之间的交互，驱动整个爬取流程。
// 对应 Scrapy Python 版本中 scrapy.core.engine 模块的 ExecutionEngine。
//
// # 架构定位
//
// Engine 位于框架的中间层，向上接受 Crawler 的编排，向下驱动各组件协作：
//
//	                  Crawler
//	                     │
//	                     ▼
//	              ┌─────────────┐
//	              │   Engine    │  ← 本包
//	              └──────┬──────┘
//	     ┌───────────────┼───────────────┐
//	     ▼               ▼               ▼
//	┌─────────┐    ┌──────────┐    ┌─────────┐
//	│Scheduler│    │Downloader│    │ Scraper │
//	└─────────┘    └──────────┘    └─────────┘
//	     │               │               │
//	     └───────────────┼───────────────┘
//	                     ▼
//	                  Spider
//
// # 核心职责
//
// Engine 承担以下核心职责：
//   - 消费 Spider 产出的初始请求（Start Requests）并注入 Scheduler
//   - 从 Scheduler 取出待处理请求，通过 Downloader 中间件链执行下载
//   - 将下载结果交给 Scraper 处理（调用 Spider 回调、分发 Item 到 Pipeline）
//   - 将 Spider 回调产出的新请求重新注入 Scheduler（形成爬取循环）
//   - 监控 Spider 空闲状态，触发 SpiderIdle 信号并决定是否关闭
//   - 管理优雅关闭流程（等待 in-flight 请求完成、Pipeline 排空、超时强制退出）
//
// # 并发模型
//
// Engine 使用 [errgroup] 统一管理多个 goroutine 的生命周期：
//   - goroutine 1：消费 Spider 初始请求（consumeStartRequests）
//   - goroutine 2：主调度循环（心跳 + 即时通知驱动）
//   - goroutine 3：OS 信号监听（两阶段 SIGINT 处理）
//
// 任一 goroutine 出错会自动取消 context，触发其他 goroutine 退出。
// 这替代了 Scrapy 中 Twisted reactor 的事件循环机制。
//
// 调度循环采用「心跳 + 即时通知」双驱动模式：
//   - 心跳（默认 5 秒）：定期检查 Scheduler 队列，防止请求饥饿
//   - 即时通知（scheduleNotify channel）：新请求入队时立即唤醒调度循环
//
// # 回退机制（Backout）
//
// Engine 在每次调度循环中检查是否需要回退（needsBackout），条件包括：
//   - Engine 未在运行
//   - Slot 正在关闭
//   - Downloader 并发数达到上限
//   - Scraper 活跃响应大小超过阈值
//
// 这确保了系统不会因过载而崩溃，对齐 Scrapy 的 _needs_backout 机制。
//
// # 优雅关闭
//
// Engine 支持两阶段优雅关闭：
//  1. 第一次 SIGINT/context 取消：停止取新请求，等待 in-flight 请求完成
//  2. 第二次 SIGINT：强制退出进程
//
// 关闭流程（gracefulClose）按以下顺序执行：
//  1. 等待 in-flight 请求完成（受 GRACEFUL_SHUTDOWN_TIMEOUT 控制，默认 30s）
//  2. 关闭 Downloader
//  3. 关闭 Scraper（等待 in-flight Item 处理完毕）
//  4. 关闭 Scheduler（持久化队列状态）
//  5. 发送 SpiderClosed 信号
//  6. 关闭 Extension 系统
//  7. 关闭统计收集器（触发 dump）
//  8. 调用 Spider.Closed
//
// # 信号系统集成
//
// Engine 在关键节点发送以下信号：
//   - EngineStarted：引擎启动完成
//   - SpiderOpened：Spider 打开（组件初始化完成）
//   - SchedulerEmpty：调度器队列为空
//   - SpiderIdle：Spider 空闲（无 in-flight 请求且调度器为空）
//   - ResponseReceived：收到下载响应
//   - RequestScheduled：请求被调度器接受
//   - RequestDropped：请求被过滤（去重）
//   - SpiderClosed：Spider 关闭
//   - EngineStopped：引擎停止
//
// # 与 Scrapy 的差异
//
//   - 使用 errgroup 替代 Twisted reactor 事件循环
//   - 使用 context.Context 传播取消信号，替代 Deferred 链
//   - 使用 goroutine 替代 Twisted callLater/deferToThread
//   - 使用 sync.Once 确保关闭流程只执行一次
//   - 使用 atomic.Bool 替代 Python 的布尔标志（并发安全）
//   - 下载操作在独立 goroutine 中执行（真正的并行），而非 Twisted 的协作式并发
//
// # Panic 恢复
//
// Engine 在以下关键路径设置了 panic recovery：
//   - downloadAndScrape：防止用户回调/中间件/Pipeline 中的 panic 导致进程崩溃
//   - consumeStartRequests：防止 Spider.Start() 中的 panic 导致进程崩溃
//
// 被恢复的 panic 会记录到日志并计入 spider_exceptions/panic 统计。
package engine
