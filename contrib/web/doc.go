// Package web 提供 scrapy-go 框架的全栈 Web 管理平台。
//
// 本包是 scrapy-go 框架的可插拔扩展模块（contrib），作为独立的 Go 子模块发布，
// 主模块 go.mod 不引入任何 Web 框架依赖，基于标准库 net/http 实现，
// 实现零侵入的可插拔设计。
//
// # 核心组件
//
//   - Server：HTTP 服务器，管理 REST API 端点、SSE 事件推送和 Spider 生命周期
//   - Registry：Spider 注册表，按名称注册工厂函数并动态创建 Spider 实例
//   - Handler：REST API 处理器，提供爬虫列表、启动、停止、统计查询等端点
//   - EventHub：SSE 事件中心，管理客户端连接和实时事件广播
//   - Store：爬取历史持久化存储，记录运行记录和统计快照
//   - SpiderSpec：声明式爬虫配置，通过 JSON 描述爬取规则并转换为 CrawlSpider
//
// # REST API 端点
//
//   - GET    /api/spiders              — 获取已注册的 Spider 列表及运行状态
//   - POST   /api/spiders/register     — 通过声明式配置动态注册 Spider（SpiderSpec → CrawlSpider）
//   - DELETE /api/spiders/:name        — 注销已注册的 Spider
//   - POST   /api/spiders/:name/start  — 按名称启动一个 Spider（支持 JSON 请求体传入启动项参数）
//   - POST   /api/spiders/:name/stop   — 按名称停止一个正在运行的 Spider
//   - GET    /api/spiders/:name/stats  — 获取指定 Spider 的统计数据（含启动项参数）
//   - GET    /api/events               — SSE 实时事件推送端点（Spider 生命周期事件）
//   - GET    /api/events/stats         — SSE 连接统计
//   - GET    /api/history              — 获取爬取历史记录
//   - GET    /api/history/{id}         — 获取单条历史记录
//   - GET    /api/history/stats        — 获取历史统计汇总
//   - GET    /api/health               — 健康检查
//   - GET    /                         — Dashboard Web UI
//
// # 使用方式
//
//	import (
//	    "context"
//	    "github.com/dplcz/scrapy-go/contrib/web"
//	    "github.com/dplcz/scrapy-go/pkg/spider"
//	)
//
//	// 创建 Web 管理服务器
//	srv := web.NewServer(":8080")
//
//	// 注册 Spider 工厂函数
//	srv.Register("quotes", func() spider.Spider {
//	    return NewQuotesSpider()
//	})
//
//	// 启动服务器（阻塞，访问 http://localhost:8080 查看 Dashboard）
//	if err := srv.ListenAndServe(context.Background()); err != nil {
//	    log.Fatal(err)
//	}
//
// # 设计决策
//
//   - 零外部依赖：基于 Go 标准库 net/http.ServeMux 实现路由，不引入第三方 Web 框架
//   - Runner 集成：内部使用 crawler.Runner 管理多爬虫并发执行，复用框架已有的生命周期管理
//   - 工厂模式：Spider 注册表存储工厂函数而非实例，每次启动创建新的 Crawler + Spider 实例
//   - 独立子模块：避免主模块引入 Web 相关依赖，用户按需安装
//   - SSE 替代 WebSocket：使用 Server-Sent Events 实现实时推送，无需第三方 WebSocket 库
//   - 声明式爬虫创建：通过 JSON SpiderSpec 配置自动生成 CrawlSpider，覆盖 80% 常见场景
//   - 嵌入式前端：Dashboard UI 通过 go:embed 嵌入，无需额外部署静态文件
package web
