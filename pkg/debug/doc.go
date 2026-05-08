// Package debug 提供 scrapy-go 框架的调试和性能分析工具。
//
// # 概述
//
// debug 包提供运行时调试能力，当前包含 [PprofExtension]，允许在爬虫运行时
// 通过标准 Go pprof 工具分析 CPU、内存、goroutine 等性能指标。
// 这是 Go 特有的调试手段，Scrapy（Python）无此功能。
//
// # 架构定位
//
// debug 包作为可选的调试扩展集成到框架中：
//
//	┌─────────────────────────────────────────────────────────┐
//	│                    Crawler                               │
//	│  (PPROF_ENABLED=true 时自动启用)                         │
//	└────────────────────────┬────────────────────────────────┘
//	                         │
//	                         ▼
//	┌─────────────────────────────────────────────────────────┐
//	│              PprofExtension                              │
//	│  (实现 Extension 接口，管理 pprof HTTP 服务器)           │
//	└────────────────────────┬────────────────────────────────┘
//	                         │
//	                         ▼
//	┌─────────────────────────────────────────────────────────┐
//	│           net/http/pprof HTTP Server                     │
//	│  (:6060/debug/pprof/*)                                  │
//	└─────────────────────────────────────────────────────────┘
//
// # PprofExtension
//
// [PprofExtension] 实现了 Extension 接口，在 Spider 启动时开启 pprof HTTP 服务器，
// 在 Spider 关闭时优雅停止服务器。
//
// 提供的 pprof 端点：
//   - /debug/pprof/         — 索引页面
//   - /debug/pprof/profile  — CPU profile（支持 ?seconds=N 参数）
//   - /debug/pprof/heap     — 堆内存 profile
//   - /debug/pprof/goroutine — goroutine 堆栈
//   - /debug/pprof/trace    — 执行 trace
//   - /debug/pprof/allocs   — 内存分配 profile
//   - /debug/pprof/block    — 阻塞 profile
//   - /debug/pprof/mutex    — 互斥锁竞争 profile
//
// # 配置
//
// 通过 Settings 配置：
//   - PPROF_ENABLED：是否启用 pprof（默认 false）
//   - PPROF_ADDR：监听地址（默认 ":6060"）
//
// 通过 Option 模式配置：
//   - [WithPprofAddr]：设置监听地址
//   - [WithPprofLogger]：设置日志记录器
//
// # 使用方式
//
// 启用 pprof 后，使用标准 Go 工具进行性能分析：
//
//	# CPU profile（30 秒采样）
//	go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
//
//	# 堆内存分析
//	go tool pprof http://localhost:6060/debug/pprof/heap
//
//	# goroutine 泄漏检测
//	go tool pprof http://localhost:6060/debug/pprof/goroutine
//
//	# 执行 trace（5 秒）
//	curl -o trace.out http://localhost:6060/debug/pprof/trace?seconds=5
//	go tool trace trace.out
//
// # 安全注意事项
//
//   - pprof 端点不应暴露到公网（包含敏感的运行时信息）
//   - 生产环境建议仅在需要调试时临时启用
//   - CPU profile 会增加约 5% 的 CPU 开销
//   - 如果端口被占用，扩展会记录警告日志但不阻止爬虫启动
//
// # 并发安全
//
// [PprofExtension] 的 Open/Close 方法应在单一 goroutine 中调用（由 Crawler 保证）。
// pprof HTTP 服务器本身是并发安全的。
package debug
