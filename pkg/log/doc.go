// Package log 提供了 scrapy-go 框架的结构化日志封装。
//
// # 概述
//
// log 包基于 Go 标准库 log/slog 包，提供框架级别的日志工具函数，
// 支持多种输出格式（Text、JSON、Color）、日志级别配置和 Spider 上下文关联。
// 这是 Go 特有的日志基础设施，Scrapy 使用 Python 的 logging 模块。
//
// # 架构定位
//
// log 包为 scrapy-go 所有组件提供统一的日志基础设施：
//
//	┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐
//	│  Engine  │  │Downloader│  │ Pipeline │  │Extension │
//	└────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘
//	     │              │              │              │
//	     │  ForSpider   │ ForComponent │  ForSpider   │
//	     ▼              ▼              ▼              ▼
//	┌─────────────────────────────────────────────────────────┐
//	│                  slog.Logger                             │
//	│  (由 log 包创建，带 spider/component 属性)               │
//	└────────────────────────┬────────────────────────────────┘
//	                         │
//	                         ▼
//	┌─────────────────────────────────────────────────────────┐
//	│              slog.Handler                                │
//	│  (TextHandler / JSONHandler / ColorHandler)              │
//	└─────────────────────────────────────────────────────────┘
//
// # Logger 创建
//
// 包提供三种 Logger 创建函数：
//   - [NewLogger]：创建标准文本格式的 Logger（适合生产环境）
//   - [NewJSONLogger]：创建 JSON 格式的 Logger（适合日志聚合系统）
//   - [NewColorLogger]：创建带颜色输出的 Logger（适合开发调试）
//
// 所有创建函数接受相同的参数：
//   - level：日志级别字符串（DEBUG、INFO、WARN、ERROR）
//   - output：输出目标（nil 时输出到 os.Stderr）
//   - addSource：是否添加源代码位置信息
//
// # 颜色输出
//
// [ColorHandler] 是自定义的 slog.Handler 实现，为不同日志级别使用不同颜色：
//   - DEBUG：青色（Cyan）
//   - INFO：绿色（Green）
//   - WARN：粗体黄色（Bold Yellow）
//   - ERROR：粗体红色（Bold Red）
//
// 当输出目标不是终端时（如重定向到文件），颜色自动禁用。
//
// 包还提供辅助颜色函数：
//   - [ColorByPriority]：根据组件优先级返回颜色（用于中间件链可视化）
//   - [ColorByStatusCode]：根据 HTTP 状态码返回颜色（用于响应日志）
//
// # 上下文关联
//
// 通过 context.Context 传递日志上下文信息：
//   - [WithSpiderName]：在 context 中设置 Spider 名称
//   - [WithComponent]：在 context 中设置组件名称
//   - [SpiderNameFromContext]：从 context 中获取 Spider 名称
//   - [ComponentFromContext]：从 context 中获取组件名称
//
// # 便捷函数
//
// 创建带属性的子 Logger：
//   - [ForSpider]：创建带 spider 属性的子 Logger
//   - [ForComponent]：创建带 component 属性的子 Logger
//   - [ForSpiderComponent]：创建同时带 spider 和 component 属性的子 Logger
//
// 示例：
//
//	logger := log.NewColorLogger("DEBUG", nil, false)
//	spiderLogger := log.ForSpider(logger, "myspider")
//	engineLogger := log.ForSpiderComponent(logger, "myspider", "engine")
//
// # 日志级别解析
//
// [ParseLevel] 将字符串解析为 slog.Level，不区分大小写：
//   - "DEBUG" → slog.LevelDebug
//   - "INFO" → slog.LevelInfo
//   - "WARN" / "WARNING" → slog.LevelWarn
//   - "ERROR" → slog.LevelError
//   - 无法识别 → slog.LevelInfo（默认）
//
// # 与 Scrapy 的差异
//
//   - 基于 Go 标准库 log/slog（Scrapy 使用 Python logging）
//   - 结构化日志（key=value 格式），而非 Python 的格式化字符串
//   - 使用 slog.Handler 接口实现可插拔的输出格式
//   - ColorHandler 是 Go 特有的终端美化实现
//   - 通过 context.Context 传递日志上下文（Scrapy 使用 LogRecord adapter）
//   - 无全局 Logger 状态，每个组件持有独立的 Logger 实例
//
// # 并发安全
//
// [ColorHandler] 通过 sync.Mutex 保护输出操作，确保并发写入不会交错。
// slog.Logger 本身是并发安全的。
package log
