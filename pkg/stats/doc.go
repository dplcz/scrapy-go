// Package stats 实现了 scrapy-go 框架的统计收集系统。
//
// # 概述
//
// stats 包提供了 [Collector] 接口和基于内存的默认实现 [MemoryCollector]，
// 用于在爬取过程中收集和管理各类统计数据。
// 对应 Scrapy Python 版本中 scrapy.statscollectors 模块的功能。
//
// # 架构定位
//
// 统计收集器是 scrapy-go 的全局数据汇聚点，各组件通过它记录运行时指标：
//
//	┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐
//	│  Engine  │  │Downloader│  │ Pipeline │  │Extension │
//	└────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘
//	     │              │              │              │
//	     │  IncValue    │  IncValue    │  IncValue    │  SetValue
//	     ▼              ▼              ▼              ▼
//	┌─────────────────────────────────────────────────────────┐
//	│              Stats Collector                             │
//	│  (response_received_count, item_scraped_count, ...)     │
//	└─────────────────────────────────────────────────────────┘
//	     │                                            │
//	     ▼ GetStats()                                 ▼ Close()
//	┌──────────┐                              ┌──────────────┐
//	│ LogStats │                              │  Dump Stats  │
//	│Extension │                              │  (日志输出)   │
//	└──────────┘                              └──────────────┘
//
// # Collector 接口
//
// [Collector] 定义了统计收集器的标准接口，所有方法均为线程安全：
//   - [Collector.GetValue] / [Collector.GetStats]：读取统计数据
//   - [Collector.SetValue] / [Collector.SetStats]：设置统计数据
//   - [Collector.IncValue]：原子递增计数器
//   - [Collector.MaxValue] / [Collector.MinValue]：更新极值
//   - [Collector.ClearStats]：清空统计
//   - [Collector.Open] / [Collector.Close]：生命周期管理
//
// # 实现
//
// 包提供两种实现：
//   - [MemoryCollector]：基于内存的统计收集器（默认），支持 Spider 关闭时 dump 统计到日志
//   - [DummyCollector]：空操作收集器，用于禁用统计收集的场景
//
// # 常用统计项
//
// 框架内置组件会自动记录以下统计项：
//
//	// 核心统计（由 CoreStatsExtension 记录）
//	start_time              — Spider 启动时间
//	finish_time             — Spider 结束时间
//	elapsed_time_seconds    — 运行耗时（秒）
//	finish_reason           — 关闭原因
//	item_scraped_count      — 已抓取 Item 数量
//	item_dropped_count      — 已丢弃 Item 数量
//	response_received_count — 已接收响应数量
//
//	// 下载器统计（由 DownloaderStats 中间件记录）
//	downloader/request_count         — 总请求数
//	downloader/request_method_count/GET — GET 请求数
//	downloader/response_count        — 总响应数
//	downloader/response_status_count/200 — 200 响应数
//	downloader/response_bytes         — 响应总字节数
//
//	// 调度器统计
//	scheduler/enqueued       — 入队请求数
//	scheduler/dequeued       — 出队请求数
//	scheduler/enqueued/disk  — 磁盘队列入队数
//
//	// 内存监控（由 MemoryUsageExtension 记录）
//	memusage/startup — 启动时内存使用
//	memusage/max     — 最大内存使用
//
// # 与 Scrapy 的差异
//
//   - 使用 sync.RWMutex 保证并发安全（Scrapy 依赖 GIL）
//   - IncValue 使用 start 参数指定初始值（Scrapy 默认从 0 开始）
//   - 使用 any 类型存储值（对应 Scrapy 的 Python 动态类型）
//   - DummyCollector 替代 Scrapy 的 STATS_CLASS 配置（Go 无动态类加载）
//   - Close 时的 dump 通过构造参数控制（对应 Scrapy 的 STATS_DUMP 配置）
//
// # 并发安全
//
// [MemoryCollector] 的所有方法均为并发安全，通过 sync.RWMutex 保护。
// [DummyCollector] 天然并发安全（无状态）。
package stats
