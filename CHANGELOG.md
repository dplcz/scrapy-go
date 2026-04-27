# Changelog

本文件记录 scrapy-go 项目的所有重要变更。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [v0.3.0-alpha.4] - 2026-04-27

### 新增

#### CrawlerRunner 多爬虫调度器（P2-010）

对应 Scrapy Python 版本的 `CrawlerRunner` / `AsyncCrawlerRunner`，实现了多 Crawler 的并发/顺序调度与统一生命周期管理。

- **`Runner` 类型** — 新增 `pkg/crawler/runner.go`，封装多爬虫调度逻辑
  - `NewRunner(opts ...RunnerOption) *Runner` — 构造器
  - `WithRunnerLogger(logger)` / `WithOSSignalHandling(enabled)` — 可选配置
- **单爬虫异步调度**
  - `Runner.Crawl(ctx, c, sp) <-chan error` — 异步启动单个 Crawler，返回完成通知 channel
- **多爬虫并发/顺序调度**
  - `Runner.StartConcurrent(ctx, jobs...)` — 并发启动多个 Spider，阻塞直到全部完成
  - `Runner.StartSequentially(ctx, jobs...)` — 按 jobs 顺序依次启动，前一个完成后再启动下一个
  - `Job` / `NewJob(c, sp)` — 描述 Crawler + Spider 绑定的爬取任务
- **跨爬虫 Signal 传播**
  - `Runner.ConnectSignal(sig, handler)` — 为所有当前/未来加入的 Crawler 注册同一个信号处理器
  - 通过 Crawler 内部的 `beforeStart` 钩子在组件组装完成、Engine 启动之前注册，保证 `EngineStarted`/`SpiderOpened` 等早期信号能被捕获
- **生命周期控制**
  - `Runner.Stop()` — 请求所有正在运行的 Crawler 优雅停止（立即返回）
  - `Runner.Wait()` — 阻塞等待所有 Crawler 完成（对应 Scrapy 的 `join`）
  - `Runner.Close()` — 停止所有 Crawler 并等待完成，之后不再接受新的 Crawler
  - `Runner.Crawlers()` / `Runner.BootstrapFailed()` — 状态查询
- **OS 信号处理**
  - 默认监听 SIGINT/SIGTERM，两阶段处理：第一次优雅关闭，第二次强制退出（exit code 130）
  - 通过 `WithOSSignalHandling(false)` 关闭内置信号处理（适合测试或外部统一管理信号）

#### Crawler 新增 API

- **`Crawler.Crawl(ctx, sp)`** — 与 `Run` 并列的爬取入口，**不安装 OS 信号处理器**，供 Runner 等上层编排器调用
- **`Crawler.Stop()`** — 请求优雅停止当前运行的爬虫，多次调用安全
- **`Crawler.Spider()`** — 返回关联的 Spider 实例
- **`Crawler.IsCrawling()`** — 查询爬虫是否正在运行
- **单次运行约束** — Crawler 实例只能运行一次，通过 `atomic.Bool.CompareAndSwap` 保护

### 变更

- `Crawler.Run` 逻辑抽取到内部 `crawl` 方法，`Run` 与 `Crawl` 共享核心路径（前者安装 OS 信号处理器，后者不安装）
- 新增内部 `Crawler.onBeforeStart(hook)` 钩子机制（非导出），供 Runner 在组件重建后注入跨爬虫信号处理器
- 修复 Crawler 在 OS 信号到达时 `signal.Notify` 的 cleanup 时机，改用 `defer signal.Stop` 避免 channel 泄漏

### 质量

- 新增 `pkg/crawler/runner_test.go` — 26 个单元测试，覆盖 Job/Option/Crawler 管理/Crawl/StartConcurrent/StartSequentially/ConnectSignal/Stop/Wait/Close/并发安全压力测试
- 新增 `tests/integration/runner_test.go` — 5 个端到端集成测试
  - `TestRunner_E2E_ConcurrentMultipleSpiders` — 5 个 Spider 并发耗时验证
  - `TestRunner_E2E_ConcurrentFiveOrMoreSpiders` — 8 个 Spider 并发（对应全局成功指标）
  - `TestRunner_E2E_SequentialOrder` — 顺序执行正确性验证
  - `TestRunner_E2E_CrossCrawlerSignalPropagation` — 跨爬虫 Signal 传播验证
  - `TestRunner_E2E_StopGracefullyInterruptsAllCrawlers` — 优雅停止验证
- 测试总数：442 个
- `pkg/crawler` 包 runner.go 行级覆盖率：约 80%（installSignalHandler 中的真实信号分支难以稳定单测，已通过启用/禁用两种路径覆盖）
- 竞态检测：`go test ./... -race` 全部通过
- `go vet`：全部通过

### Phase 2 Sprint 5 进度

- ✅ P2-004 内置扩展（上一版已完成）
- ✅ P2-005 Spider 内置中间件（上一版已完成）
- ✅ **P2-010 CrawlerRunner 多爬虫调度器（本版）**

---

## [v0.3.0-alpha.3] - 2026-04-27

### 新增

#### Spider 内置中间件（P2-005，5 个）

- **HttpError 中间件**（优先级 50）— 过滤非 2xx 响应（`pkg/spider/middleware/httperror.go`）
  - 支持 `HTTPERROR_ALLOW_ALL` 全局允许所有状态码
  - 支持 `HTTPERROR_ALLOWED_CODES` 全局允许特定状态码列表
  - 支持 `Request.Meta["handle_httpstatus_all"]` 请求级允许所有
  - 支持 `Request.Meta["handle_httpstatus_list"]` 请求级允许列表
  - 统计：`httperror/response_ignored_count`、`httperror/response_ignored_status_count/{STATUS}`

- **Offsite 中间件**（优先级 500）— 站外请求过滤（`pkg/spider/middleware/offsite.go`）
  - 基于 Spider `AllowedDomains()` 接口过滤站外请求
  - 支持子域名匹配（`example.com` 匹配 `www.example.com`）
  - `Request.DontFilter=true` 或 `Meta["allow_offsite"]=true` 跳过过滤
  - 统计：`offsite/filtered`、`offsite/domains`

- **Referer 中间件**（优先级 700）— 自动设置 Referer 头（`pkg/spider/middleware/referer.go`）
  - 使用简化的 scrapy-default 策略（no-referrer-when-downgrade）
  - HTTPS→HTTP 降级不发送 Referer；本地 scheme（file://、data://）不发送
  - 自动去除 URL 中的 fragment 和认证信息
  - 不覆盖已存在的 Referer 头
  - 配置项：`REFERER_ENABLED`（默认 true）

- **UrlLength 中间件**（优先级 800）— 过滤超长 URL（`pkg/spider/middleware/urllength.go`）
  - 在 ProcessOutput 阶段过滤 URL 长度超过 `URLLENGTH_LIMIT` 的请求
  - 统计：`urllength/request_ignored_count`

- **Depth 中间件**（优先级 900）— 爬取深度控制（`pkg/spider/middleware/depth.go`）
  - 自动为请求设置 `depth` Meta（父响应 depth + 1）
  - `DEPTH_LIMIT` 超过限制的请求被丢弃
  - `DEPTH_PRIORITY` 根据深度调整请求优先级
  - `DEPTH_STATS_VERBOSE` 记录各深度请求数统计
  - 统计：`request_depth_max`、`request_depth_count/{N}`

### 变更
- `SPIDER_MIDDLEWARES_BASE` 默认注册 5 个内置中间件：HttpError(50)、Offsite(500)、Referer(700)、UrlLength(800)、Depth(900)
- Crawler `buildSpiderMiddlewares()` 新增 `builtinSpiderMiddlewareFactories` 注册表
- 新增配置项：`HTTPERROR_ALLOW_ALL`（默认 false）、`HTTPERROR_ALLOWED_CODES`（默认 []）

### 质量
- 新增 35 个 Spider 中间件单元测试
- 测试总数：411 个
- Spider 中间件包覆盖率：82.0%
- 竞态检测：全部通过
- `go vet`：全部通过

---

## [v0.3.0-alpha.2] - 2026-04-27

### 新增

#### 内置扩展实现（P2-004）

- **CoreStats 扩展** — 收集核心统计信息（`pkg/extension/corestats.go`）
  - 监听 `spider_opened`/`spider_closed`/`item_scraped`/`item_dropped`/`response_received` 信号
  - 记录 `start_time`、`finish_time`、`elapsed_time_seconds`、`finish_reason`
  - 通过信号自动递增 `item_scraped_count`、`item_dropped_count`、`response_received_count`

- **CloseSpider 扩展** — 条件自动关闭 Spider（`pkg/extension/closespider.go`）
  - `CLOSESPIDER_TIMEOUT` — 运行超时自动关闭（秒）
  - `CLOSESPIDER_ITEMCOUNT` — 达到 Item 数量自动关闭
  - `CLOSESPIDER_PAGECOUNT` — 达到页面数量自动关闭
  - `CLOSESPIDER_ERRORCOUNT` — 达到错误数量自动关闭
  - 使用原子计数器和 CAS 确保并发安全，所有条件为 0 时返回 `ErrNotConfigured` 自动禁用

- **LogStats 扩展** — 定期输出爬取统计摘要（`pkg/extension/logstats.go`）
  - 定期输出 RPM（每分钟页面数）和 IPM（每分钟 Item 数）
  - Spider 关闭时计算并记录最终平均速率（`responses_per_minute`、`items_per_minute`）
  - `LOGSTATS_INTERVAL` 配置输出间隔（秒），设为 0 自动禁用

- **MemoryUsage 扩展** — Go 运行时内存监控（`pkg/extension/memusage.go`）
  - 使用 `runtime.MemStats.Sys` 监控系统内存占用
  - `MEMUSAGE_LIMIT_MB` — 超过限制自动关闭 Spider
  - `MEMUSAGE_WARNING_MB` — 超过阈值记录警告日志（仅一次）
  - 统计项：`memusage/startup`、`memusage/max`、`memusage/limit_reached`、`memusage/warning_reached`

### 变更
- `EXTENSIONS_BASE` 默认注册 4 个内置扩展：CoreStats、CloseSpider、LogStats、MemoryUsage
- Crawler `buildExtensions()` 新增 `builtinExtensionFactories` 注册表，按配置实例化内置扩展
- 内置扩展通过 `ErrNotConfigured` 机制自动禁用未配置的扩展

### 质量
- 新增 21 个内置扩展单元测试
- 测试总数：376 个
- Extension 包覆盖率：81.6%
- 竞态检测：全部通过
- `go vet`：全部通过

---

## [v0.3.0-alpha.1] - 2026-04-27

### 新增

#### Extension 系统框架（P2-001）
- **Extension 接口** — 定义 `Extension` 接口（`Open`/`Close` 生命周期），提供 `BaseExtension` 默认实现
- **ExtensionManager** — 扩展管理器，支持按优先级排序、`ErrNotConfigured` 自动跳过、逆序关闭
- **Crawler 集成** — Extension 系统集成到 Crawler 组件编排流程，支持 `EXTENSIONS_BASE`/`EXTENSIONS` 配置
- **AddExtension API** — Crawler 新增 `AddExtension(ext, name, priority)` 方法注册自定义扩展

#### HttpProxy 中间件（P2-002，优先级 750）
- **环境变量代理** — 自动读取 `http_proxy`/`HTTP_PROXY`、`https_proxy`/`HTTPS_PROXY` 环境变量
- **请求级代理** — 支持 `Request.Meta["proxy"]` 设置请求级代理，`nil` 值显式禁用代理
- **代理认证** — 支持 `http://user:password@host:port` 格式的代理 URL，自动设置 `Proxy-Authorization` 头
- **配置项** — `HTTPPROXY_ENABLED`（默认 true）

#### DownloaderStats 中间件（P2-003，优先级 850）
- **请求统计** — `downloader/request_count`、`downloader/request_method_count/{METHOD}`、`downloader/request_bytes`
- **响应统计** — `downloader/response_count`、`downloader/response_status_count/{STATUS}`、`downloader/response_bytes`
- **异常统计** — `downloader/exception_count`、`downloader/exception_type_count/{TYPE}`
- **耗时统计** — `downloader/max_download_time`（最大下载耗时）
- **配置项** — `DOWNLOADER_STATS`（默认 true）

### 变更
- `DOWNLOADER_MIDDLEWARES_BASE` 新增 `HttpProxy`(750) 和 `DownloaderStats`(850)
- Engine 构造函数新增 `extensions` 参数，支持扩展系统生命周期管理
- Engine `openSpider` 中初始化扩展，`closeSpider` 中关闭扩展

### 质量
- 新增 30+ 个单元测试（Extension 10 个、HttpProxy 8 个、DownloaderStats 12 个）
- 测试总数：353 个
- Extension 包覆盖率：100%
- 中间件包覆盖率：86.7%
- 竞态检测：全部通过

---

## [v0.2.4] - 2026-04-27

### 新增
- **Brotli 解压支持** — HttpCompression 中间件新增 brotli (br) 编码解压，引入 `andybalholm/brotli` 外部依赖
  - `Accept-Encoding` 请求头自动包含 `br`
  - 响应体 `Content-Encoding: br` 自动解压
  - 支持 maxSize 限制和统计收集

### 依赖
- 新增 `github.com/andybalholm/brotli` v1.2.1 — Brotli 压缩/解压

### 质量
- 新增 4 个 brotli 相关单元测试（解压、maxSize、统计、Accept-Encoding 验证）
- 全量测试通过，竞态检测通过

### 技术债务
- TD-006 已偿还：HttpCompression 现已支持 brotli 解压

---

## [v0.2.3] - 2026-04-24

### 修复
- **NewRequestError 处理** — 在中间件管理器的 `processResponse` 和 `processException` 中添加 `NewRequestError` 的显式检查，确保重试/重定向产生的新请求能正确传播给 Engine 重新调度

### 重构
- **MiddlewareManager 位置调整** — 将下载器中间件管理器从 `pkg/downloader/middleware/manager.go` 移到 `pkg/downloader/middleware_manager.go`
  - `middleware.Manager` → `downloader.MiddlewareManager`
  - `middleware.NewManager()` → `downloader.NewMiddlewareManager()`
  - `middleware.Entry` → `downloader.MiddlewareEntry`
  - 更贴近 Scrapy 原版设计：Manager 是 downloader 的编排层，不是中间件本身
  - Engine 可直接使用 `downloader.MiddlewareManager`，无需 `dmiddle` 包别名
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
