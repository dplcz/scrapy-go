// Package extension 实现了 scrapy-go 框架的扩展（Extension）系统。
//
// # 概述
//
// extension 包提供了 [Extension] 接口、[Manager] 管理器和一组内置扩展，
// 用于通过信号系统监听框架事件并实现自定义逻辑。
// 对应 Scrapy Python 版本中 scrapy.extensions 模块和 scrapy.extension 模块的功能。
//
// # 架构定位
//
// Extension 系统是 scrapy-go 的插件机制，通过信号系统与框架核心解耦：
//
//	┌─────────────────────────────────────────────────────────┐
//	│                    Crawler                               │
//	│  (根据 EXTENSIONS_BASE + EXTENSIONS 配置创建扩展)        │
//	└────────────────────────┬────────────────────────────────┘
//	                         │
//	                         ▼
//	┌─────────────────────────────────────────────────────────┐
//	│              Extension Manager                           │
//	│  (按优先级排序、管理生命周期、处理 ErrNotConfigured)      │
//	└───┬──────────────┬──────────────┬──────────────┬────────┘
//	    │              │              │              │
//	    ▼              ▼              ▼              ▼
//	┌────────┐   ┌──────────┐  ┌──────────┐  ┌──────────┐
//	│CoreStats│  │CloseSpider│  │ LogStats │  │MemUsage  │
//	└────────┘   └──────────┘  └──────────┘  └──────────┘
//	    │              │              │              │
//	    └──────────────┴──────────────┴──────────────┘
//	                         │
//	                         ▼ Connect(handler, signal)
//	┌─────────────────────────────────────────────────────────┐
//	│                  Signal Manager                          │
//	└─────────────────────────────────────────────────────────┘
//
// # Extension 接口
//
// [Extension] 接口定义了扩展的生命周期：
//   - Open：Spider 打开时调用，用于注册信号处理器和初始化资源
//   - Close：Spider 关闭时调用，用于注销信号处理器和释放资源
//
// 扩展在 Open 中返回 [errors.ErrNotConfigured] 表示该扩展未配置，
// Manager 将跳过该扩展并记录调试日志。
//
// [BaseExtension] 提供默认的空实现，扩展可以嵌入此结构体只覆盖需要的方法。
//
// # Manager
//
// [Manager] 负责管理扩展的完整生命周期：
//   - 按优先级排序管理扩展（优先级数值小的先初始化）
//   - Open 时按顺序初始化所有扩展，处理 ErrNotConfigured
//   - Close 时按逆序关闭所有扩展（确保依赖关系正确释放）
//
// # 内置扩展
//
// 框架提供以下内置扩展：
//
// [CoreStatsExtension]：收集核心统计信息
//   - 记录 start_time、finish_time、elapsed_time_seconds
//   - 通过信号递增 item_scraped_count、item_dropped_count、response_received_count
//   - 对应 Scrapy 的 scrapy.extensions.corestats.CoreStats
//
// [CloseSpiderExtension]：在满足特定条件时自动关闭 Spider
//   - 支持四种关闭条件：CLOSESPIDER_TIMEOUT、CLOSESPIDER_ITEMCOUNT、
//     CLOSESPIDER_PAGECOUNT、CLOSESPIDER_ERRORCOUNT
//   - 使用 atomic 计数器和 CAS 确保只触发一次关闭
//   - 对应 Scrapy 的 scrapy.extensions.closespider.CloseSpider
//
// [LogStatsExtension]：定期输出爬取统计摘要
//   - 输出已爬取页面数/RPM 和已抓取 Item 数/IPM
//   - 通过 LOGSTATS_INTERVAL 配置输出间隔（默认 60 秒）
//   - 对应 Scrapy 的 scrapy.extensions.logstats.LogStats
//
// [MemoryUsageExtension]：监控 Go 运行时内存使用
//   - 使用 runtime.MemStats 替代 Python 的 resource 模块
//   - 支持内存限制（超限关闭 Spider）和警告阈值
//   - 对应 Scrapy 的 scrapy.extensions.memusage.MemoryUsage
//
// [FeedExportExtension]：数据导出扩展
//   - 监听 ItemScraped 信号将 Item 分发到配置的导出目标
//   - 支持多目标并行导出（JSON、CSV、XML、JSONLines）
//   - 对应 Scrapy 的 scrapy.extensions.feedexport.FeedExporter
//
// # 自定义扩展
//
// 用户可以实现 [Extension] 接口创建自定义扩展：
//
//	type MyExtension struct {
//	    extension.BaseExtension
//	    signals *signal.Manager
//	}
//
//	func (e *MyExtension) Open(ctx context.Context) error {
//	    e.signals.Connect(e.onSpiderOpened, signal.SpiderOpened)
//	    return nil
//	}
//
// 通过 Crawler 注册自定义扩展：
//
//	crawler.New(
//	    crawler.WithExtension(&MyExtension{}, "MyExtension", 0),
//	)
//
// # 与 Scrapy 的差异
//
//   - 使用 Go 接口替代 Python 的 @classmethod from_crawler 工厂模式
//   - 使用 [BaseExtension] 嵌入替代 Python 的类继承
//   - 使用 atomic 操作替代 Python 的 GIL 保护（CloseSpider 计数器）
//   - 使用 context.Context 管理定时器生命周期，替代 Twisted callLater
//   - Manager 在 Close 时按逆序关闭（确保后初始化的先释放）
//   - 扩展通过构造函数注入依赖（signals、stats），而非 from_crawler
//
// # 并发安全
//
// [Manager] 的 Open/Close 方法应在单一 goroutine 中调用（由 Crawler 保证）。
// 内置扩展的信号处理器均为并发安全（使用 atomic 操作或无共享状态）。
package extension
