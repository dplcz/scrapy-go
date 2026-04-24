# Changelog

本文件记录 scrapy-go 项目的所有重要变更。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [v0.2.3] - 2026-04-24

### 修复
- **NewRequestError 处理** — 在中间件管理器的 `processResponse` 和 `processException` 中添加 `NewRequestError` 的显式检查，确保重试/重定向产生的新请求能正确传播给 Engine 重新调度

### 重构
- **MiddlewareManager 位置调整** — 将下载器中间件管理器从 `pkg/downloader/middleware/manager.go` 移到 `pkg/downloader/middleware_manager.go`
  - `middleware.Manager` → `downloader.MiddlewareManager`
  - `middleware.NewManager()` → `downloader.NewMiddlewareManager()`
  - `middleware.Entry` → `downloader.MiddlewareEntry`
  - 更贴近 Scrapy 原版设计：Manager 是 downloader 的编排层，不是中间件本身
  - Engine 可直接使用 `downloader.MiddlewareManager`，无需 `dl_mw` 包别名
- **测试迁移** — Manager 相关测试从 `middleware/middleware_test.go` 移到 `downloader/middleware_manager_test.go`

### 变更
- 更新所有引用文件的导入路径：`engine.go`、`engine_test.go`、`engine_panic_test.go`、`crawler.go`
- 更新 README 核心组件表格和项目结构描述

---

## [v0.2.2] - 2026-04-24

### 新增
- **Panic Recovery** — 为所有关键 goroutine 添加 panic 恢复机制
  - Engine: `downloadAndScrape`、`consumeStartRequests`
  - Downloader: `processQueue`（自动重启）、下载 goroutine
  - Spider: `Base.Start()` 内部 goroutine
- **PanicError 错误类型** — 新增 `ErrPanic` 哨兵错误和 `PanicError` 结构化错误类型，包含 panic 值和堆栈信息
- **HTTP 状态码统计** — 自动统计每个 HTTP 响应状态码的数量（`downloader/response_status_count/XXX`）
- **Panic 统计** — 自动递增 `spider_exceptions/panic` 计数器

---

## [v0.2.1] - 2026-04-24

### 新增
- **日志英文化** — 所有框架日志信息统一改为英文格式，便于国际化和机器解析
- **彩色日志输出** — 新增 `ColorHandler`（自定义 `slog.Handler`），不同日志级别使用不同 ANSI 颜色，非终端时自动禁用
- **Scrapy 风格列表日志** — 中间件、Pipeline、统计信息使用 Scrapy 风格的一条日志包含完整列表
- **Pipeline 启用日志** — 补充 Pipeline 组件的启用状态日志

---

## [v0.2.0] - 2026-04-24

### 新增

#### 下载器中间件
- **DownloadTimeout 中间件**（优先级 300）— 基于 `context.WithTimeout` 的请求超时控制，支持全局和请求级覆盖
- **HttpAuth 中间件**（优先级 410）— Basic Auth 认证注入，支持域名限制和请求级 Meta 覆盖
- **HttpCompression 中间件**（优先级 590）— 自动添加 `Accept-Encoding` 头，支持 gzip/deflate 响应体解压
- **Cookies 中间件**（优先级 700）— 基于 `net/http/cookiejar` 的多会话 Cookie 管理，支持 `cookiejar` Meta 隔离

#### HTML 解析
- **Selector 包** (`pkg/selector`) — 提供链式调用的 CSS 和 XPath 选择器
  - `Selector.CSS()` — CSS 选择器查询，支持 `::text` 伪元素
  - `Selector.CSSAttr()` — CSS 选择器 + 属性提取（等价于 Scrapy 的 `::attr(name)`）
  - `Selector.XPath()` — XPath 表达式查询
  - `List` — 批量操作：`GetAll()`、`Get()`、`First()`、`Attr()`、`AttrAll()`
- **Response 扩展** — `Response.CSS()`、`Response.CSSAttr()`、`Response.XPath()`、`Response.Selector()` 快捷方法

#### 架构优化
- **NewRequestError 机制** — 重试/重定向通过错误类型传递新请求，替代 Meta 键 hack
- **Slot context 传播** — `downloadTask` 正确传播上游 context，修复超时控制

### 变更
- 标准库 HTTP Transport 禁用自动解压（`DisableCompression: true`），由 HttpCompression 中间件统一管理
- **Go 命名规范修复** — 消除 13 处包名前缀冗余，影响 34 个文件：
  - `spider.SpiderOutput` → `spider.Output`
  - `spider.SpiderSettings` → `spider.Settings`
  - `spider.BaseSpider` → `spider.Base`
  - `signal.SignalManager` → `signal.Manager`
  - `signal.NewSignalManager` → `signal.NewManager`
  - `pipeline.PipelineEntry` → `pipeline.Entry`
  - `middleware.MiddlewareEntry` → `middleware.Entry`（downloader/spider 两个包）
  - `stats.StatsCollector` → `stats.Collector`
  - `stats.MemoryStatsCollector` → `stats.MemoryCollector`
  - `stats.DummyStatsCollector` → `stats.DummyCollector`
  - `selector.SelectorList` → `selector.List`

### 依赖
- 新增 `github.com/PuerkitoBio/goquery` v1.12.0 — CSS 选择器
- 新增 `github.com/antchfx/htmlquery` v1.3.6 — XPath 查询
- 新增 `golang.org/x/net` v0.53.0 — HTML 解析

### 质量
- 测试总数：300 个（含 8 个端到端集成测试）
- Selector 包覆盖率：98.0%
- 中间件包覆盖率：86.4%
- 竞态检测：全部通过
- `go vet`：全部通过

---

## [v0.1.0] - 2026-04-24

### 新增
- 核心框架 MVP 版本
- Engine 调度引擎
- Scheduler 调度器（优先级队列 + 去重过滤）
- Downloader 下载器（并发控制 + Slot 管理）
- Scraper 处理器
- Item Pipeline 管理器
- Signal 信号系统
- Stats 统计收集器
- 内置下载器中间件：DefaultHeaders、UserAgent、Retry、Redirect
- Spider 中间件框架
- Settings 配置系统（优先级覆盖）
- 示例爬虫：quotes、books_json、custom_middleware
