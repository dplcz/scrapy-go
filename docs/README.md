# 🕷️ scrapy-go — 完整参考文档

> 📌 这是 scrapy-go 的完整参考文档。如需快速了解项目，请查看 [项目 README](../README.md)。

**scrapy-go** 是一个用 Go 语言实现的高性能异步爬虫框架，架构设计对齐 Python [Scrapy](https://scrapy.org/)，在保留 Scrapy 核心设计理念的同时，充分利用 Go 的并发模型和类型安全特性，提供更高的运行效率和更低的资源消耗。

> 📌 当前版本：**v1.2.4** &nbsp;|&nbsp; 📋 [更新日志](#-更新日志)

---

## 📑 目录

- [🎯 项目概述](#-项目概述)
- [✨ 功能特性](#-功能特性)
- [🚀 快速开始](#-快速开始)
- [📖 使用示例](#-使用示例)
- [⚙ 配置说明](#-配置说明)
- [🏗 架构设计](#-架构设计)
- [🔄 与 Scrapy 的对比](#-与-scrapy-的对比)
- [🚧 当前版本限制](#-当前版本限制)
- [📝 更新日志](#-更新日志)

---

## 🎯 项目概述

scrapy-go 的目标是为 Go 开发者提供一个**生产级的爬虫框架**，具备以下核心价值：

- 🔗 **Scrapy 兼容架构** — Engine → Scheduler → Downloader → Scraper 经典数据流，零学习成本迁移
- ⚡ **Go 原生并发** — 基于 goroutine 和 channel 实现真正的多核并行，无 GIL 限制
- 🔒 **类型安全** — 编译期类型检查，避免运行时错误
- 🔍 **内置 HTML 解析** — 集成 goquery（CSS）和 htmlquery（XPath），提供链式选择器 API
- 🧩 **可扩展中间件** — 下载器中间件 + Spider 中间件，灵活定制处理流程

---

## ✨ 功能特性

### 🏗 核心引擎

完整实现 Scrapy 经典五大组件：

- **Engine** — 核心调度引擎，协调所有组件，支持暂停/恢复，使用 `errgroup` 统一管理多 goroutine 生命周期
- **Scheduler** — 基于内存优先级队列 + 磁盘队列的请求调度，支持断点续爬（`JOBDIR`），有序优先级切片 O(1) 出队，可插拔 Redis 分布式队列（`contrib/redisqueue`），支持 Pipeline 批量去重优化，内存队列溢出保护（`WithMemoryQueueThreshold`）
- **Downloader** — 基于 Slot 机制的 HTTP 下载，按域名分组控制并发和延迟，支持 HTTP/2 多路复用优化和连接池精细化管理
- **Scraper** — 调用 Spider 回调并分发结果（Request/Item），`semaphore.Weighted` 控制 CONCURRENT_ITEMS
- **Crawler** — 顶层编排器，一行代码组装并启动爬虫
- **Runner** — 多爬虫调度器，支持并发/顺序运行多个 Spider 并统一信号传播（对齐 Scrapy 的 `CrawlerRunner`）

### 🚀 并发与性能

- **errgroup 生命周期管理** — Engine 使用 `errgroup` 统一管理心跳/初始请求消费/信号监听 goroutine，任一出错自动取消 context
- **semaphore.Weighted** — 替代 channel 信号量，支持 context 取消（阻塞获取时可响应 ctx.Done()）
- **sync.Pool 对象池** — `pkg/pool` 包提供 Request/Response/Bytes 对象池，减少高并发场景 GC 压力
- **两阶段优雅关闭** — 第一次 SIGINT 优雅关闭（等待 in-flight + Pipeline 排空），第二次 SIGINT 强制退出
- **pprof 调试端点** — `pkg/debug` 包提供 pprof Extension，运行时 CPU/内存/goroutine 分析
- **DiskQueue 有序优先级** — 维护有序切片，Pop 取最高优先级 O(1)，Push 二分插入 O(log N)
- **性能基准测试套件** — `benchmarks/` 包提供完整的 QPS 和内存基准测试，本地 benchmark 服务器排除网络干扰

#### 📊 性能基准数据

<!-- 测量条件：本地 benchmark 服务器（最小响应体），TestQPSAcceptance 验收测试（10000 请求），
     TestMemoryAcceptance（100000 请求），GOMAXPROCS=96，无 pprof 采样开销，禁用非必要中间件 -->

| 指标 | 结果 | 说明 |
|------|------|------|
| QPS（16 并发） | ~18,900 req/s | 本地服务器，最小响应 |
| QPS（64 并发） | ~12,400 req/s | 本地服务器，最小响应 |
| 系统内存（10 万请求） | ~900 MB total_alloc | 含 Go 运行时开销 |
| 堆内存（10 万请求） | ~12 MB | 实际数据占用 |
| 每请求分配 | ~9.4 KB | 含 Request/Response/网络缓冲 |
| Request 对象创建 | 780 ns/op, 6 allocs | 单次 NewRequest 调用 |

#### 🔄 框架对比数据（vs Colly / Geziyor 真实框架）

<!-- 测量条件：TestComparisonQPS 对比测试（10000 请求/框架/并发级别），本地 benchmark 服务器，
     真实第三方库（gocolly/colly v2.1.0、geziyor v0.0.0-20240812），GOMAXPROCS=96 -->

**公平性说明**：scrapy-go 使用**默认配置**（保留所有中间件：重试、Cookie、压缩、代理、robots.txt），
Colly/Geziyor 同样使用默认配置（它们本身不含这些功能，天然更轻量）。

| 框架 | QPS (conc=16) | 开销比 | Bytes/Req | 延迟场景效率 |
|------|--------------|--------|-----------|-------------|
| raw net/http | ~27,400 | 1.00x | ~8.4 KB | 92% |
| Colly (v2.1.0) | ~9,600 | 0.35x | ~14.4 KB | 86% |
| Geziyor | ~13,300 | 0.49x | ~28.2 KB | 92% |
| **scrapy-go** | **~29,200** | **1.06x** | **~10.3 KB** | **92%** |

> scrapy-go 即使保留完整中间件栈（重试、Cookie、压缩、代理、robots.txt），
> QPS 仍为 Colly 的 **~2.5x**、Geziyor 的 **~2.0x**，内存效率也显著优于两者。
> 在有网络延迟的真实场景中，scrapy-go 的并发调度效率与裸 HTTP 客户端持平（92%）。

运行基准测试：

```bash
# QPS 基准测试
go test -bench=BenchmarkQPS -benchmem -timeout=300s ./benchmarks/

# 内存基准测试
go test -bench=BenchmarkMemory -benchmem -timeout=300s ./benchmarks/

# 真实框架对比测试（独立子模块，引入 Colly/Geziyor 真实依赖）
cd benchmarks/comparison
go test -run "TestComparison" -timeout=300s -v ./...
go test -bench "BenchmarkComparison" -benchmem -timeout=300s ./...

# 快速验收测试（CI 集成，主模块内）
go test -run "TestQPSAcceptance|TestMemoryAcceptance|TestComparisonOverheadAcceptance" -timeout=300s ./benchmarks/
```

### 📡 请求与响应

- **Request** — 支持多种 HTTP 方法、自定义 Headers、Cookies、Meta 元数据、优先级、Callback/Errback
- **NewJSONRequest** — JSON API 请求构造器，自动设置 Content-Type 和序列化 Body（对齐 Scrapy `JsonRequest`）
- **NewFormRequest** — 表单请求构造器，POST 写 Body / GET 写查询参数（对齐 Scrapy `FormRequest`）
- **FormRequestFromResponse** — 从 HTML 响应自动提取 `<form>` 信息创建表单请求（对齐 Scrapy `FormRequest.from_response()`），支持 formname/formid/formnumber/formxpath/formcss 五种表单定位
- **NewMultipartFormRequest** — multipart/form-data 文件上传请求构造器，基于 `mime/multipart` 标准库
- **NoCallback** — 哨兵值，标记请求不需要回调函数（对齐 Scrapy `NO_CALLBACK`）
- **ToDict / FromDict** — Request 序列化与反序列化，支撑磁盘队列断点续爬（对齐 Scrapy `to_dict` / `request_from_dict`）
- **CallbackRegistry** — 回调函数注册表，支持 `RegisterSpider` 通过 reflect 自动扫描注册（替代 Scrapy `getattr` 反射），方法命名遵循 Go PascalCase 规范；新增 `LookupByFunc` 反向查找（`runtime.FuncForPC` + 反向索引双策略 O(1)），确保断点续爬时回调名称正确序列化
- **FromCURL** — 从 curl 命令字符串创建 Request（对齐 Scrapy `Request.from_curl()`），自实现 shell 词法分析器
- **ToCURL** — 将 Request 转换为 curl 命令字符串（对齐 Scrapy `request_to_curl()`）
- **便捷 Option** — `WithRawBody` / `WithBasicAuth` / `WithUserAgent` / `WithFormData`
- **Response** — 支持 Text/JSON 解析、URLJoin 相对路径解析、Follow 链接跟踪、CSS/XPath 选择器、JSON 路径查询（gjson）
- **Functional Options** — 类型安全的构建模式

### 🏷️ Request Meta 参考

`Request.Meta` 是一个 `map[string]any` 类型的元数据字典，用于在请求生命周期中传递控制参数和上下文信息。
以下是框架内置支持的所有 Meta 键：

#### 下载器控制

| Meta 键 | 类型 | 默认值 | 说明 | 使用组件 |
|---------|------|--------|------|----------|
| `download_slot` | string | 域名 | 自定义 Slot 分组键，覆盖默认的按域名分组 | Downloader |
| `download_timeout` | time.Duration | Settings 值 | 请求级超时覆盖 | DownloadTimeout 中间件 |
| `proxy` | string/nil | Settings 值 | 请求级代理 URL，设为 `nil` 显式禁用代理 | HttpProxy 中间件 / Handler |
| `download_maxsize` | int | Settings 值 | 请求级最大下载大小（字节） | HttpCompression 中间件 |
| `download_warnsize` | int | Settings 值 | 请求级下载警告阈值（字节） | HttpCompression 中间件 |
| `download_progress_callback` | func(bytesRead, totalSize int64) | nil | 下载进度回调函数 | ProgressHTTPDownloadHandler |

#### 重试控制

| Meta 键 | 类型 | 默认值 | 说明 | 使用组件 |
|---------|------|--------|------|----------|
| `dont_retry` | bool | false | 设为 true 跳过自动重试 | Retry 中间件 |
| `retry_times` | int | 0 | 当前已重试次数（框架自动设置） | Retry 中间件 |
| `max_retry_times` | int | Settings 值 | 请求级最大重试次数覆盖 | Retry 中间件 |
| `download_delay` | time.Duration | — | 退避延迟（启用指数退避时由 Retry 中间件自动设置） | Retry 中间件 |

#### 重定向控制

| Meta 键 | 类型 | 默认值 | 说明 | 使用组件 |
|---------|------|--------|------|----------|
| `dont_redirect` | bool | false | 设为 true 禁止自动重定向 | Redirect 中间件 |
| `redirect_times` | int | 0 | 当前已重定向次数（框架自动设置） | Redirect 中间件 |
| `redirect_ttl` | int | Settings 值 | 剩余重定向次数 | Redirect 中间件 |
| `redirect_urls` | []string | nil | 重定向历史 URL 列表（框架自动追加） | Redirect 中间件 |
| `redirect_reasons` | []int | nil | 重定向状态码列表（框架自动追加） | Redirect 中间件 |

#### Cookie 控制

| Meta 键 | 类型 | 默认值 | 说明 | 使用组件 |
|---------|------|--------|------|----------|
| `dont_merge_cookies` | bool | false | 设为 true 跳过 Cookie 处理 | Cookies 中间件 |
| `cookiejar` | any | "default" | Cookie Jar 标识键，不同值使用不同会话 | Cookies 中间件 |

#### 认证控制

| Meta 键 | 类型 | 默认值 | 说明 | 使用组件 |
|---------|------|--------|------|----------|
| `http_user` | string | Settings 值 | 请求级 Basic Auth 用户名覆盖 | HttpAuth 中间件 |
| `http_pass` | string | Settings 值 | 请求级 Basic Auth 密码覆盖 | HttpAuth 中间件 |

#### 缓存控制

| Meta 键 | 类型 | 默认值 | 说明 | 使用组件 |
|---------|------|--------|------|----------|
| `dont_cache` | bool | false | 设为 true 跳过 HTTP 缓存 | HttpCache 中间件 |
| `cached_response` | *Response | nil | 缓存命中时存储的缓存响应（框架自动设置） | HttpCache 中间件 |

#### Robots.txt 控制

| Meta 键 | 类型 | 默认值 | 说明 | 使用组件 |
|---------|------|--------|------|----------|
| `dont_obey_robotstxt` | bool | false | 设为 true 跳过 robots.txt 检查 | RobotsTxt 中间件 |

#### Spider 中间件控制

| Meta 键 | 类型 | 默认值 | 说明 | 使用组件 |
|---------|------|--------|------|----------|
| `depth` | int | 0 | 当前请求深度（框架自动设置和递增） | Depth 中间件 |
| `allow_offsite` | bool | false | 设为 true 允许跨域请求 | Offsite 中间件 |
| `handle_httpstatus_all` | bool | false | 设为 true 允许所有 HTTP 状态码通过 | HttpError 中间件 |
| `handle_httpstatus_list` | []int | nil | 请求级允许通过的状态码列表 | HttpError 中间件 |

#### CrawlSpider 内部

| Meta 键 | 类型 | 默认值 | 说明 | 使用组件 |
|---------|------|--------|------|----------|
| `rule` | int | - | 匹配的规则索引（框架自动设置） | CrawlSpider |
| `link_text` | string | - | 链接的锚文本（框架自动设置） | CrawlSpider |

#### 使用示例

```go
import shttp "github.com/example/scrapy-go/pkg/http"

// 设置请求级超时和代理
req := shttp.NewRequest("GET", "https://example.com")
req.SetMeta("download_timeout", 30*time.Second)
req.SetMeta("proxy", "http://proxy.example.com:8080")

// 跳过重试和重定向
req2 := shttp.NewRequest("GET", "https://api.example.com/data")
req2.SetMeta("dont_retry", true)
req2.SetMeta("dont_redirect", true)

// 设置下载进度回调
req3 := shttp.NewRequest("GET", "https://example.com/large-file.zip")
req3.SetMeta("download_progress_callback", func(bytesRead, totalSize int64) {
    fmt.Printf("下载进度: %d / %d\n", bytesRead, totalSize)
})

// 多会话 Cookie 隔离
req4 := shttp.NewRequest("GET", "https://example.com/login")
req4.SetMeta("cookiejar", "session-user-1")
```

> **⚠️ 注意**：推荐使用 `SetMeta(key, value)` 逐键设置，而非 `WithMeta(map[string]any{...})`。
> `SetMeta` 内置 nil 保护且不会覆盖已有的 Meta 键；`WithMeta` 会整体替换 Meta map，
> 丢弃 `NewRequest` 预分配的 map 和其他中间件已写入的值。

#### 🧬 Meta 泛型类型还原（`GetMetaAs[T]`）

当在 Meta 中传递自定义结构体时（特别是开启 JOBDIR 断点续爬场景），使用 `GetMetaAs[T]` 泛型辅助函数可以类型安全地还原结构体：

```go
type DetailItem struct {
    Title string  `json:"title"`
    Price float64 `json:"price"`
}

// Spider.Parse 中设置结构体到 Meta
func (s *MySpider) Parse(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
    item := DetailItem{Title: "Go Book", Price: 49.99}
    req, _ := resp.Follow("/detail",
        shttp.WithCallback(s.ParseDetail),
    )
    req.SetMeta("item", item)
    return []spider.Output{{Request: req}}, nil
}

// Spider.ParseDetail 中使用 GetMetaAs[T] 恢复结构体
func (s *MySpider) ParseDetail(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
    // 快路径：内存中直接类型断言（零分配）
    // 慢路径：磁盘队列反序列化后自动 JSON 往返转换
    item, err := shttp.GetMetaAs[DetailItem](resp, "item")
    if err != nil {
        return nil, err
    }
    fmt.Printf("Title: %s, Price: %.2f\n", item.Title, item.Price)
    return nil, nil
}
```

> **💡 提示**：`GetMetaAs[T]` 在未经过磁盘序列化时（内存请求）走快路径直接类型断言，零分配零开销；
> 仅在断点续爬恢复时（Meta 值已变为 `map[string]any`）才触发 JSON 往返转换（~2μs）。
> Request 端可使用对称的 `GetRequestMetaAs[T](req, key)` 函数。

### 🔁 去重与调度

- **RFPDupeFilter** — 基于请求指纹（URL + Method + Body SHA1）去重
- **PersistentRFPDupeFilter** — 支持持久化的去重过滤器，断点续爬时恢复去重状态
- **DontFilter** — 支持跳过去重（如初始请求）
- **NoDupeFilter** — 可选的无过滤模式
- **优先级队列** — 高优先级请求优先处理
- **磁盘队列** — 基于文件系统的持久化队列，支持断点续爬（`JOBDIR` 配置启用）
- **断点续爬** — 中断后重启自动从断点继续，不重复爬取已完成 URL

### ⏱️ 并发与延迟控制

- 全局并发限制（`CONCURRENT_REQUESTS`）
- Item 并发处理（`CONCURRENT_ITEMS`，默认 100，对齐 Scrapy）
- 域名级并发限制（`CONCURRENT_REQUESTS_PER_DOMAIN`）
- 可配置下载延迟及随机化
- 通过 `download_slot` Meta 自定义分组

### 🔌 下载器中间件

支持优先级排序的中间件执行链（ProcessRequest 正序、ProcessResponse 逆序），内置 11 个中间件：

| 中间件 | 优先级 | 功能 |
|--------|--------|------|
| RobotsTxt | 100 | robots.txt 遵守（按 netloc 缓存，需启用 `ROBOTSTXT_OBEY`） |
| DownloadTimeout | 300 | 请求超时控制 |
| DefaultHeaders | 400 | 自动注入默认请求头 |
| HttpAuth | 410 | Basic Auth 认证 |
| UserAgent | 500 | User-Agent 设置 |
| CircuitBreaker | 545 | 域名级熔断器（连续失败自动熔断，需启用 `CIRCUIT_BREAKER_ENABLED`） |
| Retry | 550 | 失败请求自动重试（支持指数退避 + 抖动 + 差异化策略） |
| HttpCompression | 590 | gzip/deflate/brotli 解压 |
| Redirect | 600 | HTTP 重定向处理 |
| Cookies | 700 | 多会话 Cookie 管理 |
| HttpCache | 900 | HTTP 缓存（DummyPolicy / RFC2616Policy，需启用 `HTTPCACHE_ENABLED`） |

通过实现 `DownloaderMiddleware` 接口或嵌入 `BaseDownloaderMiddleware` 自定义扩展。

### 🔍 HTML 解析（Selector）

- 内置 CSS 和 XPath 选择器（基于 goquery/htmlquery）
- 支持 `::text` 伪元素和属性提取
- 链式调用和批量操作（`GetAll()` / `Get()` / `First()`）
- Response 快捷方法：`CSS()` / `CSSAttr()` / `XPath()` / `Selector()`

### 🔗 JSON 路径查询（gjson）

- 基于 `github.com/tidwall/gjson`，直接在 `[]byte` 上做路径查询，零中间分配
- `JSONGet(path)` — 点路径查询，返回 `gjson.Result`（自带类型访问器）
- `JSONGetMany(paths...)` — 一次扫描多路径，性能优于多次 `JSONGet`
- `JSONExists(path)` — 检查路径是否存在
- `JSONForEach(path, iter)` — 流式遍历数组/对象，避免大数组一次性分配
- 支持数组投影（`#.field`）、条件过滤（`#(cond)#`）、修饰符管道（`|@reverse`）
- 与 `JSON(v any) error` 整体反序列化互补共存，不破坏旧 API

### 🕷️ CrawlSpider 基于规则的自动爬取

- **CrawlSpider** — 基于 Rule 规则的自动链接提取和跟踪（对齐 Scrapy `CrawlSpider`）
- **LinkExtractor 接口** — 可插拔的链接提取器，内置 `HTMLLinkExtractor`（基于 goquery）
- **Rule 规则** — 支持 `allow`/`deny` 正则过滤、域名过滤、`restrictCSS`/`restrictXPath` 范围限制
- **Callback/Errback** — 直接接受函数值（舍弃 Scrapy 字符串方法名反射，更符合 Go 风格）
- **ProcessLinks/ProcessRequest** — 链接和请求后处理钩子
- **跨规则去重** — 同一链接只被第一个匹配的规则处理
- **Functional Options** — 全部配置通过 `WithAllow`/`WithDeny`/`WithAllowDomains` 等选项函数设置

### 🛡️ 下载器中间件（接口隔离设计）

- **接口隔离原则（ISP）** — 拆分为三个细粒度接口：`RequestProcessor` / `ResponseProcessor` / `ExceptionProcessor`
- **按需实现** — 中间件只需实现关心的接口，无需为不需要的方法提供空实现
- **类型断言适配** — Manager 通过缓存的类型断言自动跳过未实现对应接口的中间件
- **向后兼容** — 原有 `DownloaderMiddleware` 全功能接口保留，已有中间件无需修改

### 🛡️ Spider 中间件 & 📦 Item Pipeline

- **Spider 中间件** — 拦截 Spider 输入（响应）和输出（Request/Item），支持自定义扩展
- **Item Pipeline** — 按优先级顺序处理 Item，支持数据清洗、验证、持久化
- **TypedPipeline[T] 泛型包装** — 编译期类型约束，类型不匹配时自动跳过，多个 TypedPipeline 可共存
- **ItemAdapter 自动验证** — `SetValidateItems(true)` 启用后自动填充默认值 + 校验 required
- **DropItem** — 丢弃无效 Item，中断后续处理链
- **FromCrawler 工厂约定** — Pipeline 可实现 `CrawlerAwarePipeline` 接口，在 Open 前获取 Crawler 引用以访问 Settings/Stats/Signals
- **ItemAdapter 统一访问** — 通过 `pkg/item.Adapt` 以统一接口访问 `map` / `struct` / 自定义类型，Pipeline 无需关心 Item 具体类型

### 📦 Item 体系（ItemAdapter + struct tag 增强）

- 对齐 Scrapy 的 [`itemadapter`](https://github.com/scrapy/itemadapter) 设计，提供统一的 Item 访问抽象
- **三类内置适配**
  - `MapAdapter` — `map[string]any` / `map[string]string` / 其他 `key=string` 的 map
  - `StructAdapter` — 任意 struct / *struct，字段名解析顺序 `item` tag → `json` tag → Go 字段名，支持 `item:"-"` 显式隐藏
  - 自实现 — 业务方只需实现 `ItemAdapter` 接口即可被自动识别
- **struct tag 字段元数据增强**
  - `item:"name,required"` — 标记必填字段
  - `item:"name,default=value"` — 标记默认值（支持 string/int/float/bool）
  - `item:"name,omitempty"` — 序列化时忽略零值
  - `item.Validate(item)` — 先填充默认值，再校验 required
- **go generate 代码生成器** — `scrapy-go generate-adapter -type=Book` 从 struct tag 自动生成 ItemAdapter 实现，消除运行时反射开销
- **扩展点** — `item.Register(factory)` 注册自定义工厂（支持 ORM 模型、protobuf Message 等）
- **FieldMeta 驱动序列化** — Feed Export 根据 `FieldMeta` 中的 `serializer` 键自动调用已注册的序列化函数
- **性能** — struct 级元数据通过 `sync.Map` 进程级缓存，重复反射成本归零

### 📤 Feed Export 数据导出

- **四种内置格式** — JSON、JSON Lines、CSV、XML，对齐 Scrapy `FEED_FORMATS`
- **两种存储后端** — 本地文件（`FileStorage`，支持 `file://` URI 与自动创建目录）、标准输出（`StdoutStorage`）
- **URI 模板占位符** — `%(name)s` / `%(time)s` / `%(batch_id)d` / `%(batch_time)s`
- **多目标并行** — 同一次爬取同时写入多个目标，互不阻塞
- **Item 过滤器** — 每条 Feed 独立的 `Filter func(item any) bool`，支持按条件分流
- **StoreEmpty** — 即使没有 Item 也可生成占位文件（空 JSON 数组、空 JSON Lines 等）
- **FieldsToExport** — 指定字段顺序与列投影，支持 CSV 按字段缺失输出空值
- **多值字段** — CSV 支持 `JoinMultivalued` 拼接 `[]string`、`[]any`
- **Item 兼容** — 同时支持 `map[string]any`、`map[string]string`、自定义 `map`、`struct`（通过 `item` / `json` tag）
- **serialize_field 钩子** — 通过 `feedexport.RegisterSerializer` 注册命名序列化函数，Exporter 根据 `FieldMeta` 自动调用
- **两种配置入口** — 代码注入（`crawler.AddFeed`）或 `Settings.FEEDS`；也兼容 Scrapy 旧版 `FEED_URI` / `FEED_FORMAT`

### 📡 信号系统

- 18 种内置信号，覆盖引擎生命周期、Spider 状态、请求/响应/Item 事件
- 组件间通过信号松耦合通信，支持自定义信号处理器

### ⚙ 配置系统

- 六级优先级覆盖：default → command → addon → project → spider → cmdline
- Spider 级别类型安全配置（`CustomSettings()`）
- 配置冻结，防止运行时意外修改
- `_BASE` + 用户配置合并，负数优先级禁用组件
- **泛型类型安全 API** — `Key[T]` + `Get[T]`/`Set[T]` 编译期类型检查，消除魔法字符串

### 📊 统计与日志

- **统计收集** — 基于内存的 MemoryCollector，Spider 关闭时自动 Dump
- **HTTP 状态码统计** — 自动统计每个响应状态码数量
- **彩色结构化日志** — 基于 `slog` 的英文日志，终端彩色输出
- **Scrapy 风格日志** — 中间件、Pipeline、统计信息使用列表格式输出
- **优雅关闭** — 监听 SIGINT/SIGTERM 信号

### 🔭 可观测性扩展点

- **Tracer 接口** — `pkg/telemetry` 定义轻量级 `Tracer`/`Span`/`SpanContext` 接口，支持分布式追踪
- **MetricsRegistry 接口** — 定义 `Counter`/`Gauge`/`Histogram` 指标接口，支持 Prometheus 等后端
- **零开销默认** — `NoopTracer` + `NoopMetricsRegistry` 空操作实现，未配置后端时无运行时开销
- **可插拔架构** — 具体的 OpenTelemetry/Prometheus 适配器由 `contrib/telemetry` 独立子模块提供
- **信号钩子预留** — 接口设计兼容框架信号系统，Extension 可通过信号监听自动创建 Span 和更新指标
- **OpenTelemetry 适配器** — `contrib/telemetry/otel` 包提供 OTel Tracer 适配器，支持导出到 Jaeger/Zipkin/OTLP 等后端
- **Prometheus 适配器** — `contrib/telemetry/prometheus` 包提供 Prometheus MetricsRegistry 适配器，内置 HTTP `/metrics` 端点
- **Prometheus Label 维度** — `contrib/telemetry/prometheus.LabeledRegistry` 支持按 Spider 名称/域名等维度分组指标，与 Grafana 模板变量联动
- **信号驱动扩展** — `TraceExtension` 自动为 Spider 生命周期和 HTTP 请求创建追踪 Span（按 Request 指针关联活跃 Span，实现请求-响应完整追踪）；`MetricsExtension` 自动收集 9 个核心指标
- **Grafana Dashboard** — `contrib/telemetry/grafana/` 提供开箱即用的 Dashboard JSON 模板（Spider 概览/请求延迟/错误率/队列深度/按域名维度）
- **独立子模块** — `contrib/telemetry` 独立 `go.mod`，主模块不引入 OTel/Prometheus 依赖，按需安装

### 🛡️ Panic Recovery

- 所有关键路径均内置 panic 恢复（Spider 回调、Pipeline、Downloader、Start 方法）
- panic 不会导致进程崩溃
- 恢复后转换为结构化的 `PanicError`（含堆栈信息）
- 支持 `errors.Is(err, ErrPanic)` 匹配
- 自动递增 `spider_exceptions/panic` 统计

### 🛠️ 项目脚手架工具

命令行工具 `scrapy-go` 提供项目创建和爬虫生成功能（零外部依赖）：

- **`scrapy-go startproject <name>`** — 创建完整的项目骨架（main.go / project/ / spiders/ / go.mod / scrapy-go.toml）
- **`scrapy-go genspider <name> <domain>`** — 在项目中生成爬虫文件到 `spiders/` 目录，支持 basic（默认）和 crawl 两种模板（必须在项目根目录下执行）
- **`scrapy-go version [-v]`** — 打印版本信息

```bash
# 安装脚手架工具
go install github.com/dplcz/scrapy-go/cmd/scrapy-go@latest

# 创建新项目
scrapy-go startproject myproject
cd myproject

# 生成爬虫
scrapy-go genspider quotes quotes.toscrape.com
scrapy-go genspider -t crawl articles blog.example.com
```

### 📦 外部依赖

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/PuerkitoBio/goquery` | v1.12.0 | CSS 选择器引擎 |
| `github.com/antchfx/htmlquery` | v1.3.6 | XPath 查询引擎 |
| `github.com/tidwall/gjson` | v1.19.0 | JSON 路径查询引擎 |
| `golang.org/x/net` | v0.53.0 | HTML 解析 |

---

## 🚀 快速开始

### 安装

```bash
go get scrapy-go
```

> 📋 **要求**：Go 1.25.1+

### 最简示例

```go
package main

import (
    "context"
    "fmt"

    "github.com/dplcz/scrapy-go/pkg/crawler"
    shttp "github.com/dplcz/scrapy-go/pkg/http"
    "github.com/dplcz/scrapy-go/pkg/spider"
)

type MySpider struct {
    spider.Base
}

func NewMySpider() *MySpider {
    return &MySpider{
        Base: spider.Base{
            SpiderName: "my_spider",
            StartURLs:  []string{"https://example.com"},
        },
    }
}

func (s *MySpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
    fmt.Printf("Status: %d, URL: %s\n", response.Status, response.URL)
    fmt.Printf("Body length: %d bytes\n", len(response.Body))
    return nil, nil
}

func main() {
    c := crawler.NewDefault()
    ctx := context.Background()
    c.Run(ctx, NewMySpider())
}
```

---

## 📖 使用示例

项目提供了三个完整示例，均使用本地 `httptest` 服务器，**无需外部网络**即可运行：

| 示例 | 说明 | 运行命令 |
|------|------|----------|
| 🕷️ **quotes** | 多页爬取 + CSS/XPath 解析 | `go run examples/quotes/main.go` |
| 📚 **books_json** | JSON API + Pipeline 数据处理 | `go run examples/books_json/main.go` |
| 🔧 **custom_middleware** | 认证/日志/缓存中间件 | `go run examples/custom_middleware/main.go` |
| 📤 **feedexport** | Feed Export 全部核心 API 演示（格式/存储/序列化器/FeedSlot/Crawler 集成） | `go run examples/feedexport/main.go` |
| 📦 **itemadapter** | ItemAdapter 全部核心 API 演示（MapAdapter/StructAdapter/FieldMeta/自定义工厂） | `go run examples/itemadapter/main.go` |
| 🔗 **json_api** | JSON 路径查询（gjson）— JSONGet/JSONGetMany/JSONExists/JSONForEach | `go run examples/json_api/main.go` |

此外，`examples/template/` 目录提供了对齐 Scrapy CLI 模板的 Go 代码模板，可直接复制到项目中使用：

| 模板 | 对齐 Scrapy | 路径 | 说明 |
|------|-------------|------|------|
| 📋 **settings** | `settings.py.tmpl` | `examples/template/project/settings.go` | 项目配置模板，包含所有常用配置项及注释 |
| 📦 **pipelines** | `pipelines.py.tmpl` | `examples/template/project/pipelines.go` | Item Pipeline 接口实现模板 |
| 🔌 **middlewares** | `middlewares.py.tmpl` | `examples/template/project/middlewares.go` | 下载器中间件 + Spider 中间件实现模板 |
| 🕷️ **basic** | `spiders/basic.tmpl` | `examples/template/spiders/basic/main.go` | 基础爬虫模板，可直接运行 |

---

## 🔀 多爬虫调度（Runner）

`crawler.Runner` 对应 Scrapy 的 `CrawlerRunner`，用于在同一进程中同时/顺序运行多个 Spider，并统一管理信号传播与优雅关闭。

### 并发运行多个 Spider

```go
runner := crawler.NewRunner()

// 跨爬虫信号传播：所有 Spider 的 SpiderOpened/SpiderClosed 均会触发此处理器
runner.ConnectSignal(signal.SpiderOpened, func(params map[string]any) error {
    if sp, ok := params["spider"].(spider.Spider); ok {
        fmt.Println("spider opened:", sp.Name())
    }
    return nil
})

err := runner.StartConcurrent(ctx,
    crawler.NewJob(crawler.NewDefault(), spiderA),
    crawler.NewJob(crawler.NewDefault(), spiderB),
    crawler.NewJob(crawler.NewDefault(), spiderC),
)
```

### 顺序运行多个 Spider

```go
err := runner.StartSequentially(ctx,
    crawler.NewJob(crawler.NewDefault(), spiderA),
    crawler.NewJob(crawler.NewDefault(), spiderB),
)
```

### 核心能力

| 方法 | 说明 |
|------|------|
| `Crawl(ctx, c, sp)` | 异步启动单个 Crawler，返回 `<-chan error` |
| `StartConcurrent(ctx, jobs...)` | 并发启动多个 Crawler，阻塞直到全部完成 |
| `StartSequentially(ctx, jobs...)` | 顺序启动多个 Crawler，前一个完成后再启动下一个 |
| `ConnectSignal(sig, handler)` | 为所有当前/未来加入的 Crawler 注册同一个信号处理器 |
| `Stop()` / `Wait()` / `Close()` | 统一的停止、等待与关闭接口 |

**与 Scrapy 原版的差异**：

- 舍弃 Python `spider_loader`（字符串名加载 Spider 类），Go 直接传入 Spider 实例
- 舍弃 `CrawlerProcess`/reactor 生命周期管理，改为内置 OS 信号处理（两阶段 SIGINT：第一次优雅关闭，第二次强制退出）
- 使用 `sync.WaitGroup` + channel 替代 Twisted Deferred / asyncio.Task 集合
- 多个 Crawler 错误通过 `errors.Join` 聚合，自动忽略 `context.Canceled`/`DeadlineExceeded`

---

## ⚙ 配置说明

### 配置方式

scrapy-go 支持四种配置方式（按优先级从低到高）：

**① 框架默认配置** — 所有配置项都有合理的默认值，开箱即用

**② TOML 配置文件** — 通过 `scrapy-go.toml` 文件设置（`PriorityAddon` 级别）：

```toml
# scrapy-go.toml
bot_name = "mybot"
concurrent_requests = 32
download_delay = 1
log_level = "INFO"
robotstxt_obey = false
retry_http_codes = [500, 502, 503, 504, 429]
```

配置文件自动探测顺序：`SCRAPY_GO_CONFIG` 环境变量 → 当前目录 `scrapy-go.toml`

**③ 全局配置** — 通过 `Settings` 对象设置：

```go
s := settings.New()
s.Set("CONCURRENT_REQUESTS", 32, settings.PriorityProject)
s.Set("DOWNLOAD_DELAY", time.Second, settings.PriorityProject)


**⑤ 泛型类型安全 API（推荐）** — 编译期类型检查，消除魔法字符串：

```go
// 使用类型化键常量，编译期确定返回类型
concurrency := settings.Get(s, settings.KeyConcurrentRequests) // int
botName := settings.Get(s, settings.KeyBotName)               // string
enabled := settings.Get(s, settings.KeyRetryEnabled)          // bool

// 类型安全的设置（编译期约束值类型）
settings.Set(s, settings.KeyConcurrentRequests, 32, settings.PriorityProject)
```
c := crawler.New(crawler.WithSettings(s))
```

**④ Spider 级别配置** — 通过 `CustomSettings()` 返回类型安全的配置：

```go
func (s *MySpider) CustomSettings() *spider.Settings {
    return &spider.Settings{
        ConcurrentRequests:         spider.IntPtr(4),
        ConcurrentRequestsPerDomain: spider.IntPtr(2),
        DownloadDelay:              spider.DurationPtr(500 * time.Millisecond),
        RandomizeDownloadDelay:     spider.BoolPtr(true),
        RetryTimes:                 spider.IntPtr(3),
        LogLevel:                   spider.StringPtr("INFO"),
        UserAgent:                  spider.StringPtr("MyBot/1.0"),
    }
}
```

### 核心配置参数

<details>
<summary>🔀 <b>并发控制</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `CONCURRENT_REQUESTS` | int | 16 | 全局最大并发请求数 |
| `CONCURRENT_REQUESTS_PER_DOMAIN` | int | 8 | 每个域名（Slot）的最大并发数 |
| `CONCURRENT_ITEMS` | int | 100 | 最大并发 Item 处理数 |

</details>

<details>
<summary>⬇️ <b>下载配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DOWNLOAD_DELAY` | Duration/int | 0 | 同一 Slot 内请求间隔（0 表示无延迟） |
| `RANDOMIZE_DOWNLOAD_DELAY` | bool | true | 是否在 [0.5×delay, 1.5×delay) 范围内随机化延迟 |
| `USER_AGENT` | string | `scrapy-go/0.1.0` | 默认 User-Agent |

</details>

<details>
<summary>🔐 <b>超时与认证</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DOWNLOAD_TIMEOUT` | int | 180 | 下载超时（秒） |
| `HTTP2_ENABLED` | bool | false | 启用 HTTP/2 优化下载处理器 |
| `DOWNLOAD_PROGRESS_ENABLED` | bool | false | 启用下载进度回调 |
| `DOWNLOAD_PROGRESS_MIN_INTERVAL` | int | 100 | 进度报告最小间隔（毫秒） |
| `CONNPOOL_MAX_IDLE_CONNS` | int | 100 | 最大空闲连接总数 |
| `CONNPOOL_MAX_IDLE_CONNS_PER_HOST` | int | 10 | 每 host 最大空闲连接数 |
| `CONNPOOL_MAX_CONNS_PER_HOST` | int | 0 | 每 host 最大连接数（0=不限制） |
| `CONNPOOL_IDLE_CONN_TIMEOUT` | int | 90 | 空闲连接超时（秒） |
| `CONNPOOL_TLS_HANDSHAKE_TIMEOUT` | int | 10 | TLS 握手超时（秒） |
| `CONNPOOL_DIAL_TIMEOUT` | int | 30 | TCP 连接超时（秒） |
| `CONNPOOL_DISABLE_KEEPALIVES` | bool | false | 禁用 HTTP keep-alive |
| `HTTPAUTH_USER` | string | "" | Basic Auth 用户名 |
| `HTTPAUTH_PASS` | string | "" | Basic Auth 密码 |
| `HTTPAUTH_DOMAIN` | string | "" | 限制认证的域名（空表示所有域名） |

</details>

<details>
<summary>🍪 <b>Cookies 配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `COOKIES_ENABLED` | bool | true | 是否启用 Cookies 中间件 |
| `COOKIES_DEBUG` | bool | false | 是否输出 Cookies 调试日志 |

</details>

<details>
<summary>📦 <b>压缩配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `COMPRESSION_ENABLED` | bool | true | 是否启用 HttpCompression 中间件 |

</details>

<details>
<summary>🔄 <b>重试配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `RETRY_ENABLED` | bool | true | 是否启用自动重试 |
| `RETRY_TIMES` | int | 2 | 最大重试次数（不含首次请求） |
| `RETRY_HTTP_CODES` | []int | [500,502,503,504,522,524,408,429] | 触发重试的 HTTP 状态码 |
| `RETRY_PRIORITY_ADJUST` | int | -1 | 重试请求的优先级调整值 |
| `RETRY_BACKOFF_ENABLED` | bool | false | 是否启用指数退避重试 |
| `RETRY_BACKOFF_BASE_DELAY` | float64 | 1.0 | 退避基础延迟（秒） |
| `RETRY_BACKOFF_MAX_DELAY` | float64 | 60.0 | 退避最大延迟（秒） |
| `RETRY_BACKOFF_JITTER` | bool | true | 是否启用随机抖动 |
| `RETRY_PER_STATUS_MAX_TIMES` | map[int]int | {} | 按状态码差异化最大重试次数 |

</details>

<details>
<summary>🔌 <b>熔断器配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `CIRCUIT_BREAKER_ENABLED` | bool | false | 是否启用域名级熔断器 |
| `CIRCUIT_BREAKER_FAIL_THRESHOLD` | int | 5 | 连续失败阈值（达到后熔断） |
| `CIRCUIT_BREAKER_RECOVERY_TIMEOUT` | int | 30 | 恢复超时时间（秒） |
| `CIRCUIT_BREAKER_HALF_OPEN_MAX_REQUESTS` | int | 1 | 半开状态最大探测请求数 |
| `CIRCUIT_BREAKER_SUCCESS_THRESHOLD` | int | 2 | 半开状态恢复所需连续成功次数 |
| `CIRCUIT_BREAKER_HTTP_CODES` | []int | [500,502,503,504] | 触发熔断的 HTTP 状态码 |

</details>

<details>
<summary>↪️ <b>重定向配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `REDIRECT_ENABLED` | bool | true | 是否启用自动重定向 |
| `REDIRECT_MAX_TIMES` | int | 20 | 最大重定向次数 |
| `REDIRECT_PRIORITY_ADJUST` | int | 2 | 重定向请求的优先级调整值 |

</details>

<details>
<summary>🎛️ <b>AutoThrottle 自适应限速配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `AUTOTHROTTLE_ENABLED` | bool | false | 是否启用自适应限速 |
| `AUTOTHROTTLE_START_DELAY` | float64 | 5.0 | 初始下载延迟（秒） |
| `AUTOTHROTTLE_MAX_DELAY` | float64 | 60.0 | 最大下载延迟（秒） |
| `AUTOTHROTTLE_TARGET_CONCURRENCY` | float64 | 1.0 | 目标并发数（每个域名） |
| `AUTOTHROTTLE_DEBUG` | bool | false | 是否输出调试日志 |

启用后，框架会根据实际下载延迟动态调整每个域名的下载延迟：
- 使用 EWMA（指数加权移动平均）平滑延迟抖动
- 根据目标并发数计算理想延迟：`target_delay = latency / target_concurrency`
- 新延迟 = `(old_delay + target_delay) / 2`，并钳制在 `[start_delay * 0.2, max_delay]` 范围内

```go
// 启用 AutoThrottle
s.Set("AUTOTHROTTLE_ENABLED", true, settings.PriorityProject)
s.Set("AUTOTHROTTLE_TARGET_CONCURRENCY", 2.0, settings.PriorityProject)
s.Set("AUTOTHROTTLE_MAX_DELAY", 30.0, settings.PriorityProject)
```

</details>

<details>
<summary>🔌 <b>中间件配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DOWNLOADER_MIDDLEWARES_BASE` | map[string]int | 见下方 | 内置下载器中间件及优先级 |
| `DOWNLOADER_MIDDLEWARES` | map[string]int | {} | 用户自定义中间件优先级覆盖 |
| `SPIDER_MIDDLEWARES_BASE` | map[string]int | 见下方 | 内置 Spider 中间件及优先级 |
| `SPIDER_MIDDLEWARES` | map[string]int | {} | 用户自定义 Spider 中间件优先级覆盖 |

内置下载器中间件默认优先级：

```
RobotsTxt:        100
DownloadTimeout:  300
DefaultHeaders:   400
HttpAuth:         410
UserAgent:        500
CircuitBreaker:   545
Retry:            550
HttpCompression:  590
Redirect:         600
Cookies:          700
HttpProxy:        750
DownloaderStats:  850
```

内置 Spider 中间件默认优先级：

```
HttpError:  50
Offsite:    500
Referer:    700
UrlLength:  800
Depth:      900
```

禁用内置中间件的方式：

```go
s := settings.New()
// 方式 1：设置优先级为负数
s.Set("DOWNLOADER_MIDDLEWARES", map[string]int{"Retry": -1}, settings.PriorityProject)
// 方式 2：通过开关配置
s.Set("RETRY_ENABLED", false, settings.PriorityProject)
```

</details>

<details>
<summary>🤖 <b>Robots.txt 配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `ROBOTSTXT_OBEY` | bool | false | 是否启用 robots.txt 遵守 |
| `ROBOTSTXT_USER_AGENT` | string | "" | 用于 robots.txt 匹配的 User-Agent（为空时使用请求的 User-Agent） |

启用后，中间件会在首次请求某个域名时自动下载并缓存该域名的 robots.txt，后续请求直接使用缓存结果。
被 robots.txt 禁止的请求会返回 `ErrIgnoreRequest` 错误。

跳过单个请求的 robots.txt 检查：
```go
req, _ := shttp.NewRequest("https://example.com/page",
    shttp.WithMeta(map[string]any{"dont_obey_robotstxt": true}),
)
```

</details>

<details>
<summary>🌐 <b>HTTP 代理配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `HTTPPROXY_ENABLED` | bool | false | 是否启用 HttpProxy 中间件 |
| `HTTPPROXY_AUTH_ENCODING` | string | "latin-1" | 代理认证信息编码（Go 中使用 UTF-8） |

代理来源（按优先级从高到低）：
1. `Request.Meta["proxy"]` — 请求级代理（设为 `nil` 可显式禁用代理）
2. 环境变量 `http_proxy`/`HTTP_PROXY`、`https_proxy`/`HTTPS_PROXY`

支持带认证的代理 URL：
```go
req, _ := shttp.NewRequest("https://example.com",
    shttp.WithMeta(map[string]any{
        "proxy": "http://user:password@proxy.example.com:8080",
    }),
)
```

</details>

<details>
<summary>📝 <b>日志与统计</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `LOG_LEVEL` | string | "DEBUG" | 日志级别：DEBUG/INFO/WARN/ERROR |
| `STATS_DUMP` | bool | true | Spider 关闭时是否输出统计信息 |
| `SCHEDULER_DEBUG` | bool | false | 是否输出调度器调试日志 |
| `MEMORY_QUEUE_THRESHOLD` | int | 0 | 内存队列最大容量阈值（0=不限制），超阈值自动溢出到磁盘队列 |
| `DOWNLOADER_STATS` | bool | true | 是否启用下载器统计中间件 |
> 🎨 **日志颜色**：终端自动启用彩色输出，非终端时自动禁用
> - 🔵 **DEBUG** (`DBG`): 青色
> - 🟢 **INFO** (`INF`): 绿色
> - 🟡 **WARN** (`WRN`): 粗体黄色
> - 🔴 **ERROR** (`ERR`): 粗体红色

启用下载器统计中间件后自动统计以下指标：
> - `downloader/request_count` — 总请求数
> - `downloader/request_method_count/{METHOD}` — 按 HTTP 方法统计
> - `downloader/request_bytes` — 请求总字节数
> - `downloader/response_count` — 总响应数
> - `downloader/response_status_count/{STATUS}` — 按状态码统计
> - `downloader/response_bytes` — 响应总字节数
> - `downloader/exception_count` — 总异常数
> - `downloader/exception_type_count/{TYPE}` — 按异常类型统计
> - `downloader/max_download_time` — 最大下载耗时

</details>

---

## 📤 Feed Export 使用说明

Feed Export 对应 Scrapy 的 `scrapy.extensions.feedexport`，提供统一的 Item 导出能力，支持多种格式、多目标并行输出。

### 两种配置方式

**方式一：代码注入（推荐）**

```go
import (
    "github.com/dplcz/scrapy-go/pkg/crawler"
    "github.com/dplcz/scrapy-go/pkg/feedexport"
)

c := crawler.NewDefault()

// 导出 JSON（整体数组）
c.AddFeed(feedexport.FeedConfig{
    URI:       "output.json",
    Format:    feedexport.FormatJSON,
    Overwrite: true,
})

// 同时导出 CSV（指定字段顺序）
opts := feedexport.DefaultExporterOptions()
opts.FieldsToExport = []string{"title", "price", "stock"}
c.AddFeed(feedexport.FeedConfig{
    URI:       "output.csv",
    Format:    feedexport.FormatCSV,
    Overwrite: true,
    Options:   opts,
})

c.Run(ctx, mySpider)
```

**方式二：Settings.FEEDS（兼容 Scrapy 风格）**

```go
s := settings.New()
s.Set("FEEDS", map[string]map[string]any{
    "output.jsonl": {"format": "jsonlines", "overwrite": true},
    "output.csv":   {"format": "csv"},
}, settings.PriorityProject)

c := crawler.New(crawler.WithSettings(s))
```

### 支持的格式

| Format 常量 | 别名 | 说明 |
|-------------|------|------|
| `FormatJSON` | `json` | 整体 JSON 数组；支持 `Indent` 缩进 |
| `FormatJSONLines` | `jsonlines` / `jl` / `jsonl` | 逐行 JSON，适合流式/大数据 |
| `FormatCSV` | `csv` | 自动表头；支持 `JoinMultivalued` 拼接多值 |
| `FormatXML` | `xml` | 可配置 `RootElement` / `ItemElement` |

### URI 模板占位符

`FeedConfig.URI` 支持以下占位符（在 `SpiderOpened` 时由 Spider 名与当前时间渲染）：

| 占位符 | 说明 | 示例 |
|--------|------|------|
| `%(name)s` | Spider 名称 | `out-%(name)s.json` → `out-myspider.json` |
| `%(time)s` | 启动时间（`YYYY-MM-DDTHH-MM-SS`） | `log-%(time)s.csv` |
| `%(batch_id)d` | 分片 ID（预留） | `part-%(batch_id)d.json` |
| `%(batch_time)s` | 分片时间（预留） | `batch-%(batch_time)s.xml` |

### 常用选项（`FeedConfig`）

| 字段 | 说明 |
|------|------|
| `URI` | 输出路径，支持相对、绝对、`file://`、`stdout:` / `-` |
| `Format` | 格式；`""` 时从 URI 扩展名推断 |
| `Overwrite` | `true` 覆盖；`false` 追加 |
| `StoreEmpty` | `true` 即使没有 Item 也创建文件（空数组/空文件） |
| `Filter` | `func(item any) bool`，仅返回 `true` 的 Item 会被导出 |
| `Options.FieldsToExport` | 字段白名单 + 顺序 |
| `Options.Indent` | JSON/XML 缩进（空格数） |
| `Options.Encoding` | 编码（保留字段，当前以 UTF-8 输出） |

### 统计指标

Feed Export 会写入以下 stats，便于监控：

- `feedexport/success_count/<uri>` — 成功完成的 Feed 数
- `feedexport/items_count/<uri>` — 每个 Feed 导出的 Item 数
- `feedexport/failed_count/<uri>` — 关闭失败的 Feed 数
- `feedexport/error_count/<uri>` — 写入 Item 失败次数

### 与 Scrapy 原版的差异

- **未实现**：S3/FTP/GCS 等远程存储、`BATCH_ITEM_COUNT` 分片、`PostProcessing`（gzip/lz4 压缩）— 可通过自定义 `FeedStorage` 实现
- **保留**：`FEED_URI` + `FEED_FORMAT` 的旧式单文件配置，仅作兼容
- **改进**：Go 类型安全的 `FeedConfig`，避免 Scrapy 字典配置的运行时字段错误

---

## 🏗 架构设计

### 整体架构

scrapy-go 的架构完全对齐 Scrapy 的经典数据流模型：

```
                              ┌─────────────┐
                              │   Crawler    │
                              │  (编排器)     │
                              └──────┬───────┘
                                     │
                              ┌──────▼───────┐
                    ┌────────►│   Engine      │◄────────┐
                    │         │  (调度引擎)    │         │
                    │         └──┬────┬───┬───┘         │
                    │            │    │   │              │
              Requests      Requests │  Responses    Requests
                    │            │    │   │           + Items
                    │            │    │   │              │
             ┌──────┴──────┐    │    │   │     ┌────────┴────────┐
             │  Scheduler   │◄──┘    │   └────►│    Scraper       │
             │  (调度器)     │        │         │   (处理器)       │
             │  ┌─────────┐ │        │         │  ┌────────────┐ │
             │  │PQueue   │ │        │         │  │Spider MW   │ │
             │  │DupeFilter│ │       │         │  │Pipeline    │ │
             │  └─────────┘ │        │         │  └────────────┘ │
             └──────────────┘        │         └────────┬────────┘
                                     │                  │
                              ┌──────▼───────┐          │
                              │  DL MW Chain  │     ┌───▼───┐
                              │ (中间件链)     │     │ Spider │
                              └──────┬───────┘     │(用户)  │
                                     │             └───────┘
                              ┌──────▼───────┐
                              │  Downloader   │
                              │  (下载器)      │
                              │  ┌─────────┐  │
                              │  │Slot(域名)│  │
                              │  │Slot(域名)│  │
                              │  └─────────┘  │
                              └───────────────┘
```

### 📊 数据流

1. 🕷️ **Spider** 产出初始请求 → **Engine** 接收
2. 📥 **Engine** 将请求送入 **Scheduler**（去重 + 优先级队列）
3. 📤 **Engine** 从 **Scheduler** 取出请求 → 经过**下载器中间件链**（正序 ProcessRequest）
4. ⬇️ **Downloader** 按 Slot 分组执行 HTTP 下载（并发 + 延迟控制）
5. ⬆️ 响应经过**下载器中间件链**（逆序 ProcessResponse）→ 返回 **Engine**
6. 🔄 **Engine** 将响应送入 **Scraper** → 经过 **Spider 中间件链** → 调用 **Spider** 回调
7. 📦 Spider 回调产出 **Request**（回到步骤 2）或 **Item**（进入 Pipeline）
8. ⚙ **Item Pipeline** 按优先级顺序处理 Item（清洗 → 验证 → 持久化）

### 🧱 核心组件

| 组件 | 包路径 | 职责 |
|------|--------|------|
| **Crawler** | `pkg/crawler` | 顶层编排器，组装所有组件，提供用户 API |
| **Engine** | `pkg/engine` | 核心调度引擎，协调 Scheduler/Downloader/Scraper |
| **Scheduler** | `pkg/scheduler` | 请求调度（优先级队列 + 去重过滤） |
| **Downloader** | `pkg/downloader` | HTTP 下载管理（Slot 并发/延迟控制） |
| **Scraper** | `pkg/scraper` | 响应处理（调用 Spider 回调 + 分发结果） |
| **Spider** | `pkg/spider` | 用户爬虫接口（定义爬取逻辑） |
| **Pipeline** | `pkg/pipeline` | Item 数据处理管道 |
| **Extension** | `pkg/extension` | 扩展系统（5 个内置扩展 + 信号驱动生命周期管理 + AutoThrottle 自适应限速） |
| **DL Middleware** | `pkg/downloader/middleware` | 下载器中间件接口与实现（11 个内置中间件） |
| **DL MW Manager** | `pkg/downloader` | 下载器中间件管理器（编排中间件链 + 调用下载函数） |
| **Spider Middleware** | `pkg/spider/middleware` | Spider 中间件（5 个内置 + 输入/输出拦截） |
| **Settings** | `pkg/settings` | 多优先级配置系统 |
| **Signal** | `pkg/signal` | 事件/信号系统 |
| **Stats** | `pkg/stats` | 统计收集器（含 HTTP 状态码统计） |
| **Selector** | `pkg/selector` | CSS/XPath 选择器（对齐 Scrapy Selector） |
| **HTTP** | `pkg/http` | Request/Response 数据模型 |
| **Errors** | `pkg/errors` | 框架错误类型（对齐 Scrapy exceptions，含 PanicError） |
| **Log** | `pkg/log` | 日志工具（含 ColorHandler 彩色输出） |

### 🎰 Slot 机制

Downloader 通过 Slot 机制实现精细的并发和延迟控制：

- 每个域名（或自定义 `download_slot`）对应一个独立的 Slot
- Slot 内部通过队列驱动串行出队，用 `lastSeen` 时间戳精确控制请求间隔
- 不同 Slot 之间完全并行，互不阻塞
- 支持通过 Request Meta 自定义 Slot 分组：

```go
req, _ := shttp.NewRequest("https://example.com/api",
    shttp.WithMeta(map[string]any{
        "download_slot": "my-custom-group",
    }),
)
```

---

## 🔄 与 Scrapy 的对比

### ✅ 相同点

| 方面 | 说明 |
|------|------|
| **架构模型** | Engine → Scheduler → Downloader → Scraper 的经典数据流完全一致 |
| **Spider 接口** | `Start()` / `Parse()` / `CustomSettings()` / `Closed()` 对应 Scrapy 同名方法 |
| **Request/Response** | 字段设计（URL、Method、Headers、Meta、Priority、Callback、Errback）完全对齐 |
| **中间件体系** | 下载器中间件和 Spider 中间件接口一致 |
| **中间件执行顺序** | ProcessRequest 正序、ProcessResponse/ProcessException 逆序 |
| **Item Pipeline** | Open/Close/ProcessItem 接口对齐，支持 DropItem |
| **配置系统** | 多优先级覆盖、`_BASE` + 用户配置合并、负数优先级禁用 |
| **去重过滤** | RFPDupeFilter 基于请求指纹去重 |
| **信号系统** | spider_opened/spider_closed/spider_idle 等信号对齐 |
| **Slot 机制** | 按域名分组的并发/延迟控制 |
| **错误类型** | DropItem/CloseSpider/IgnoreRequest/NotConfigured 等异常对齐 |

### ⚡ 区别

| 方面 | Scrapy (Python) | scrapy-go (Go) |
|------|-----------------|----------------|
| 🌐 **语言** | Python 3 + Twisted | Go + goroutine |
| 🔀 **并发模型** | 单线程事件循环（受 GIL 限制） | 多核并行，无 GIL |
| 🔒 **类型安全** | 动态类型，运行时检查 | 静态类型，编译期检查 |
| ⚙ **Spider 配置** | `custom_settings` 返回 dict | `CustomSettings()` 返回类型安全结构体 |
| 🏗 **Request 构建** | 关键字参数 | Functional Options 模式 |
| 📦 **部署** | 需要 Python 运行时 | 编译为单一二进制文件 |
| 💾 **内存占用** | 较高 | 较低（Go 值类型 + 紧凑布局） |

---

## 🚧 当前版本限制

以下功能尚未实现，计划在后续版本中逐步完善：

### 未实现的功能

| 功能 | 说明 |
|------|------|
| 🗜️ Zstd | HttpCompression 暂不支持 zstd |
| 🐚 Scrapy Shell | 不支持交互式调试 |

### ⚠️ 已知约束

- **Go 版本要求** — 需要 Go 1.25.1+
- **回调函数类型安全** — `CallbackFunc`/`ErrbackFunc` 为具体函数类型（P5-021 已消除 `any` 类型），编译期即可捕获签名错误

---

## 📁 项目结构

```
scrapy-go/
├── 📂 cmd/                         # 命令行工具
│   └── scrapy-go/                  # 脚手架工具（startproject/genspider/version）
│       ├── main.go                 # CLI 入口与子命令分发
│       ├── startproject.go         # startproject 命令实现
│       ├── genspider.go            # genspider 命令实现
│       ├── version.go              # version 命令实现
│       └── templates/              # go:embed 嵌入模板
│           ├── project/            # 项目骨架模板
│           └── spiders/            # 爬虫模板（basic/crawl）
├── 📂 examples/                    # 示例代码
│   ├── quotes/                     # 基础爬取示例
│   ├── books_json/                 # Pipeline + JSON API 示例
│   ├── custom_middleware/          # 自定义中间件示例
│   ├── feedexport/                 # Feed Export 全部 API 示例
│   ├── itemadapter/                # ItemAdapter 全部 API 示例
│   └── template/                   # 代码模板（对齐 Scrapy CLI 模板）
│       ├── project/                # 项目级模板（settings/pipelines/middlewares）
│       └── spiders/                # 爬虫模板（basic）
├── 📂 internal/
│   └── utils/                      # 内部工具（指纹计算、URL 规范化）
├── 📂 pkg/
│   ├── crawler/                    # Crawler 编排器
│   ├── engine/                     # Engine 调度引擎
│   ├── scheduler/                  # Scheduler 调度器 + 去重过滤器
│   ├── downloader/                 # Downloader 下载器 + Slot 机制 + 中间件管理器
│   │   ├── handler.go             # HTTP/1.1 & HTTP/2 下载处理器（ALPN 自动协商 + h2c 可选）
│   │   ├── connpool.go            # 连接池精细化管理（含 h2c 支持）
│   │   ├── progress.go            # 下载进度回调支持
│   │   └── middleware/             # 下载器中间件接口与实现（11 个内置）
│   ├── scraper/                    # Scraper 响应处理器
│   ├── spider/                     # Spider 接口 + 配置
│   │   └── middleware/             # Spider 中间件
│   ├── pipeline/                   # Item Pipeline
│   ├── extension/                  # Extension 扩展系统（4 个内置扩展 + Feed Export）
│   ├── feedexport/                 # Feed Export 数据导出（JSON/JSONL/CSV/XML）
│   ├── item/                       # Item 体系与 ItemAdapter 统一访问抽象
│   ├── http/                       # Request/Response 数据模型
│   ├── selector/                   # CSS/XPath 选择器
│   ├── settings/                   # 多优先级配置系统
│   ├── signal/                     # 信号/事件系统
│   ├── stats/                      # 统计收集器
│   ├── errors/                     # 框架错误类型
│   └── log/                        # 日志工具
├── 📂 tests/
│   └── integration/                # 端到端集成测试
└── go.mod
```

---
### v1.2.1 🐛

> **Bug 修复 — 扩展/中间件组件引用失效问题**

- 🐛 **修复 Signals/Stats 重建导致扩展引用失效** — `crawl()` 中因 Spider `CustomSettings` 重建 Logger/Signals/Stats 时，会导致之前注入的扩展（如 `TraceExtension`/`MetricsExtension`）内部持有的旧引用失效
- 🐛 **修复 `WithStats`/`WithSignals` 注入被覆盖** — 用户通过 `WithStats()`/`WithSignals()` 注入的自定义组件在 `Run` 时被无条件覆盖
- 🚀 **新增 `CrawlerAwareExtension` 接口** — 扩展可实现 `FromCrawler(c Crawler) error` 方法，在 `Open` 之前自动获取最新的 Signals/Logger/Stats 引用
- 🚀 **新增 `customSignals`/`customStats` 标记** — 与已有的 `customLogger` 对齐，确保用户注入的自定义组件不会被重建逻辑覆盖
- ✅ **telemetry 扩展适配** — `TraceExtension` 和 `MetricsExtension` 实现 `CrawlerAwareExtension` 接口，自动获取最新引用

### v1.2.0 🚀

> **生产增强里程碑 M7 正式发布**

- 📊 **Prometheus Label 维度支持** — `LabeledCounter`/`LabeledGauge`/`LabeledHistogram` 接口扩展，支持按 Spider 名称/域名分组指标
- 📊 **Grafana Dashboard 模板** — 开箱即用的 14 面板 Dashboard（Spider 概览/请求延迟/错误率/队列深度/按域名维度）
- 🔭 **TraceExtension Span 生命周期增强** — `sync.Map` 按 Request 指针关联活跃 Span，实现请求-响应完整追踪
- 💾 **通用持久化存储适配器** — MongoDB/PostgreSQL/Elasticsearch 批量写入 Pipeline（`contrib/storage`）
- 🛡️ **高级重试策略** — 指数退避 + 抖动 + 域名级熔断器（Closed → Open → Half-Open）
- ⏱️ **分布式限速器** — Redis 滑动窗口算法，按域名差异化配置（`contrib/ratelimit`）
- 📊 **Scheduler 内存队列溢出保护** — 超阈值自动溢出到磁盘队列，防止 OOM
- ⚡ **Redis 去重 Pipeline 批量优化** — 批量 SISMEMBER + Pipeline 模式，减少网络往返
- 🌐 **轻量级 REST API** — Spider 注册表 + HTTP 管理接口（`contrib/web`）
- 🏗️ **下载器 HTTP/2 架构重构** — 删除冗余 Handler，连接池统计集成，h2c 支持
- 🔒 **公共类型包重构** — `CallbackFunc`/`ErrbackFunc` 从 `any` 替换为具体函数类型，编译期类型安全
- 🎯 **泛型 Settings API** — `Key[T]` + `Get[T]`/`Set[T]` 编译期类型检查，消除魔法字符串
- 🧬 **Meta 结构体序列化** — `GetMetaAs[T]` 泛型辅助函数，磁盘队列往返后类型安全还原
- 🐛 **断点续爬回调修复** — `CallbackRegistry` 反向索引，O(1) 函数值查找


## 📝 更新日志

### v1.2.4 🐛

> **修复 download_latency 设置时机（对齐 Scrapy 原版 handler 层面设置）**

- 🐛 **修复 `download_latency` 在 `RequestLeftDownloader` 信号发出时不可用** — 将 `download_latency` 的设置从 `DownloaderStatsMiddleware.ProcessResponse`（中间件层）移至 `HTTPDownloadHandler.Download()`（handler 层），对齐 Scrapy 原版在 handler 内部设置的语义，确保 `RequestLeftDownloader` 信号发出时 `download_latency` 已可用
- ♻️ **移除 `_download_start_time` 内部 Meta key** — 该 key 为 scrapy-go 自创（Scrapy 原版不存在），现由 handler 内部局部变量替代
- ♻️ **`DownloaderStatsMiddleware` 简化** — `ProcessRequest` 不再设置内部时间戳，`ProcessResponse` 改为从 Meta 读取 handler 已设置的 `download_latency`

### v1.2.3 🐛

> **修复 ResponseDownloaded 信号发送顺序 + download_latency 设置**

- 🐛 **修复 `ResponseDownloaded` 信号发送顺序** — 对齐 Scrapy 原版语义，先发送 `ResponseDownloaded`（仅成功时），再发送 `RequestLeftDownloader`（无论成功失败）
- 🐛 **修复 `download_latency` 未写入 Request Meta** — `DownloaderStatsMiddleware.ProcessResponse` 中将 `download_latency`（`time.Duration`）设置到 `request.Meta`，供 AutoThrottle、Telemetry 等扩展消费
- 🐛 **清理 Telemetry 扩展无效参数读取** — `TraceExtension` 改为从 `request.GetMeta("download_latency")` 读取延迟，移除对 `params["status"]` 的死代码
- ♻️ **`RequestLeftDownloader` 信号新增 `error` 参数** — 供 Telemetry 扩展记录失败请求的错误信息

### v1.2.2 🚀

> **P5-024 Response JSON 选择器（gjson 集成）**

- 🚀 **`Response.JSONGet(path string) gjson.Result`** — gjson 路径查询，零中间分配
- 🚀 **`Response.JSONGetMany(paths ...string) []gjson.Result`** — 一次扫描多路径提取
- 🚀 **`Response.JSONExists(path string) bool`** — 路径存在性检查
- 🚀 **`Response.JSONForEach(path string, iter func(key, value gjson.Result) bool)`** — 流式遍历
- 📦 **新增依赖** — `github.com/tidwall/gjson` v1.19.0
- 📖 **示例爬虫** — `examples/json_api/` 演示 JSON API 深层字段提取

### v1.1.7 🧬

> **P5-025 Meta 结构体序列化支持 + 泛型类型还原辅助函数（`GetMetaAs[T]`）**

- 🧬 **`GetMetaAs[T]` 泛型辅助函数** — 从 Response/Request 的 Meta 中类型安全地还原结构体，快路径零分配，慢路径兼容磁盘队列反序列化
- 🧬 **`GetRequestMetaAs[T]` 对称 API** — Request 端泛型辅助函数，与 Response 版本共享核心逻辑
- 🚀 **`isJSONSerializable` 增强** — 基于 `reflect.Kind` 智能判断，允许结构体/指针/切片通过序列化检查，结果缓存避免重复反射
- 🔧 **磁盘队列结构体往返** — `SetMeta(struct)` → ToDict → JSON → 磁盘 → FromDict → `GetMetaAs[T]` 完整链路正确恢复

### v1.1.6 ⚡

> **P5-023 下载器 HTTP/2 架构重构 + Scheduler 单队列优先级修复**

- 🏗️ **P5-023 HTTP/2 架构重构** — 删除冗余 `HTTP2DownloadHandler`，连接池统计集成至默认 Handler，新增 h2c 配置支持
- 🗑️ **删除 `handler_h2.go`** — Go 标准库原生支持 HTTP/2 ALPN 自动协商，独立 Handler 属于直译反模式
- 📊 **`ConnPoolStats` 集成** — `NewHTTPDownloadHandlerWithConfig` 支持连接池运行时统计
- ⚙️ **`AllowH2C` 配置项** — 新增 HTTP/2 over cleartext 支持（内网/测试场景）
- 🐛 **修复跨批次优先级失效** — 双锁分离设计中 `inBuffer`/`outQueue` 物理隔离导致高优先级请求被低优先级请求"饿死"
- ♻️ **单队列 + 单锁设计** — 合并为单个 `PriorityQueue` + 单个 `sync.Mutex`，保证全局优先级排序绝对正确
- ⚡ **DupeFilter 锁外执行** — 去重检查移至队列锁外，最小化临界区（`sync.Map.LoadOrStore` 原子操作）
- 📈 **性能对比** — 单线程 -11%，并行场景 -46%，并发入队 +28%（IO-bound 场景可忽略）
- 🧪 **新增 4 个优先级正确性测试** + 1 个跨批次场景基准测试

### v1.1.5 🐛

> **P5-022 断点续爬回调序列化修复 + Meta 类型还原 + 性能基准测试**

- 🎯 **`LookupByFunc` / `LookupErrbackByFunc`** — 通过 `runtime.FuncForPC` + 反向索引双策略 O(1) 查找回调注册名称
- 🐛 **修复回调序列化 bug** — 原 `fmt.Sprintf("%v", func)` 比较方式失败，改为函数名提取 + 指针反向索引
- ♻️ **`RequestSerializer` 简化** — `lookupCallbackName` 从 O(N) 遍历降为 O(1) 查找
- 🔧 **Meta 类型还原** — `FromDict` 反序列化后递归将 `float64` 无损还原为 `int`，解决 `meta["page"].(int)` 断言失败
- 📈 **性能基准测试** — `LookupByFunc` ~150ns/op 零分配；完整往返 ~8.8μs 支撑 ~11 万次/秒
- 🔧 **脚手架工具增强** — `genspider` 模板更新 + `scrapy-go.toml` 配置模板精简

### v1.1.4 🎉

> **TD-004 Settings 编译期类型安全增强 + P5-021 类型安全增强（TD-003 偿还）+ 扩展按需加载优化**

- 🛡️ **泛型类型安全 API** — 新增 `Key[T]` 泛型类型 + `Get[T]`/`Set[T]`/`MustGet[T]` 顶层函数
- 🔑 **类型化配置键常量** — 80+ 个框架内置配置项定义为 `Key[T]` 常量，消除魔法字符串
- ♻️ **Crawler 迁移** — `pkg/crawler` 中 50+ 处调用迁移至泛型 API
- ✅ **TD-004 已偿还** — 旧 API 保留完全向后兼容
- 🔒 **`CallbackFunc`/`ErrbackFunc` 具体类型定义** — 从 `any` 替换为具体函数签名，编译期捕获签名错误
- 🏗️ **`Output` 类型下沉至 `pkg/http`** — `pkg/spider` 通过类型别名保持完全向后兼容
- ✅ **消除运行时类型断言** — `Scraper` 中不再需要 `request.Callback.(spider.CallbackFunc)` 断言
- 🎯 **`CallbackRegistry` 精确签名匹配** — 使用 `reflect.Value.Convert` 零开销类型转换
- 🔄 **`NoCallback` 哨兵值重构** — 从结构体改为函数值，通过指针比较实现
- ⚡ **扩展按需加载** — 未启用的扩展（AutoThrottle/MemoryUsage/CloseSpider/LogStats）不再实例化
- 🔧 **`EXTENSIONS_BASE` 优先级分配** — 消除优先级冲突警告，确定性执行顺序
- 🛡️ **默认值保守化** — `HTTPPROXY_ENABLED`/`MEMUSAGE_ENABLED`/`REFERER_ENABLED`/`ROBOTSTXT_OBEY` 默认改为 `false`

### v1.1.3 🎉

> **Post-v1.0 Sprint 13 生产增强 — P5-012 分布式限速器 + P5-017 Scheduler 内存队列溢出优化**

- ⏱️ **P5-012 分布式限速器** — `contrib/ratelimit` 独立模块，基于 Redis 滑动窗口算法实现多实例全局速率控制
- 🎯 **RateLimiter 接口** — `Allow(domain) bool` 非阻塞检查 + `Wait(ctx, domain) error` 阻塞等待
- 🔧 **Redis Lua 脚本原子操作** — 滑动窗口移除过期记录 → 计数 → 添加新记录，无竞态条件
- 🌐 **按域名差异化限速** — `DomainRates` 配置允许为特定域名设置独立速率
- 🔌 **RateLimitExtension** — 监听 `RequestReachedDownloader` 信号自动限速，可注册到 Crawler 扩展系统
- 🛡️ **优雅降级** — Redis 不可用时自动允许所有请求通过，不阻塞爬虫运行
- 🔗 **共享连接** — `NewRedisSlidingWindowLimiterFromClient` 支持与 `contrib/redisqueue` 复用 Redis 连接
- 🧪 **P5-012 测试覆盖率 88.0%** — 27 个测试全部通过，`go test -race` 竞态检测通过
- 🛡️ **P5-017 内存队列溢出保护** — `DefaultScheduler` 新增 `WithMemoryQueueThreshold(n int)` Option
  - 内存队列请求数超过阈值时，可序列化请求自动溢出到磁盘队列
  - 未配置 `jobDir` 时自动创建临时磁盘队列目录，爬虫结束后自动清理
  - 新增 `MemoryQueueLen()` / `MemoryQueueThreshold()` 监控方法
  - 新增 `scheduler/overflow_to_disk` 统计指标
- 🧪 **P5-017 测试覆盖率 82.2%** — 10 个单元测试 + 4 个基准测试，`go test -race` 竞态检测通过

### v1.1.2 🎉

> **Post-v1.0 Sprint 13 生态完善 — P5-005 REST API + P5-008 Redis Pipeline 优化**

- 🌐 **P5-005 Web 管理 API** — `contrib/web` 独立模块，基于标准库 `net/http` 实现零外部依赖 REST API
- 📡 **REST API 端点** — `GET /api/spiders` / `POST /api/spiders/:name/start` / `POST /api/spiders/:name/stop` / `GET /api/spiders/:name/stats` / `GET /api/health`
- 🎯 **启动项参数注入** — `POST /api/spiders/:name/start` 支持 JSON 请求体传入 `args` 参数，以 `PriorityCmdline` 最高优先级注入 Crawler Settings
- 📊 **状态查询回显** — `GET /api/spiders/:name/stats` 响应中同步返回对应任务的启动项参数
- 🚀 **P5-008 Pipeline 批量去重** — 新增 `PipelinedRedisDupeFilter`，聚合多个 SADD 为 Redis Pipeline 批量提交，减少网络往返
- 🌸 **布隆过滤器支持** — 与 `RedisDupeFilter` 一致的布隆过滤器一级缓存
- 🧪 **测试覆盖率 86.0%+** — 42 个 Web API 测试 + 24 个 Redis Pipeline 测试全部通过，`go test -race` 竞态检测通过

### v1.1.1 🎉

> **Post-v1.0 Sprint 12 完成 — 生态完善里程碑 M6**

- 🚀 **P5-001 高级下载器特性** — HTTP/2 优化下载处理器 + 连接池精细化管理 + 下载进度回调
- 🎛️ **P5-002 AutoThrottle 扩展** — 基于延迟反馈的自适应速率调整，自动优化下载延迟
- 🌐 **P5-003 Redis 队列可插拔扩展** — `contrib/redisqueue` 独立模块，支持分布式爬取 + 布隆过滤器加速
- 📊 **P5-007 可观测性具体实现** — `contrib/telemetry` 独立模块，OpenTelemetry 追踪 + Prometheus 指标 + 信号驱动自动采集
- ⚡ **性能优化 P4-007a~m** — ~~Scheduler 双锁分离~~（已在 v1.1.6 替换为单队列设计）、Worker Pool 化、信号系统 COW、Meta 预初始化等 13 项优化
- 🧪 **全量测试通过** — `go test -race` 竞态检测通过，`go vet` 无告警，`gofmt` 格式化通过

### v1.0.3

> **Post-v1.0 Sprint 12 — P5-002 AutoThrottle 自适应限速扩展**

- 🎛️ **AutoThrottle 扩展** — 基于延迟反馈的自适应速率调整（对应 Scrapy `scrapy.extensions.throttle.AutoThrottle`）
  - 监听 `ResponseDownloaded` 信号，使用 EWMA 平滑延迟抖动
  - 根据目标并发数动态计算理想下载延迟，每域名独立调整
  - 通过 `DelayAdjuster` 接口回调调整 Slot 延迟，与 Downloader 解耦
- ⚙️ **5 个配置项** — `AUTOTHROTTLE_ENABLED` / `AUTOTHROTTLE_START_DELAY` / `AUTOTHROTTLE_MAX_DELAY` / `AUTOTHROTTLE_TARGET_CONCURRENCY` / `AUTOTHROTTLE_DEBUG`
- 📊 **运行时统计** — `autothrottle/request_count` / `autothrottle/latency_avg` / `autothrottle/delay_adjusted_count`
- 🔌 **Downloader 增强** — 新增 `AdjustDelay()` 方法 + `Slot.SetDelay()` 动态延迟调整
- 🧪 **33 个测试用例** — 覆盖率 90.7%，`go test -race` 通过

### v1.0.2

> **Post-v1.0 Sprint 12 — P5-003 Redis 队列可插拔扩展（独立模块）**

- 🌐 **Redis 分布式队列** — 新增 `contrib/redisqueue` 独立 Go 子模块，基于 Redis Sorted Set 实现 `PriorityAwareQueue` 接口，支持多实例分布式爬取
- 🔒 **Redis 分布式去重** — `RedisDupeFilter` 基于 Redis Set 实现 `DupeFilter` 接口，SADD 原子操作保证多实例并发安全
- 🌸 **布隆过滤器加速** — 可选本地布隆过滤器一级缓存，新请求跳过 Redis 查询，减少 90%+ 网络往返
- 🔌 **零侵入可插拔** — 独立 `go.mod`，主模块不引入 Redis/Bloom 依赖，通过 `WithExternalQueue` / `WithDupeFilter` 注入
- ⚙️ **完整配置** — `Options` 结构体支持连接/Key/行为/布隆过滤器参数配置
- 🧪 **测试覆盖率 89.9%** — 52+ 测试用例，`go test -race` 通过，使用 miniredis 内存 Mock
- 📚 **完整文档** — `contrib/redisqueue/README.md` + `doc.go` 包含快速开始/分布式爬取/布隆过滤器示例

### v1.1.0-alpha.1

> **Post-v1.0 Sprint 12 — P5-001 高级下载器特性**

- 🚀 **HTTP/2 优化下载处理器** — ~~新增 `HTTP2DownloadHandler`~~ （v1.1.7 已重构：HTTP/2 支持通过默认 `HTTPDownloadHandler` + `ConnPoolConfig.ForceHTTP2` 实现，`HTTP2DownloadHandler` 已删除）
- 🔧 **连接池精细化管理** — 新增 `ConnPoolConfig`（14 项参数）+ `ConnPoolStats`（atomic 无锁统计）+ `ManagedTransport`，通过 `CONNPOOL_*` 配置项集成
- 📈 **下载进度回调** — 新增 `ProgressHTTPDownloadHandler`，通过 `Request.Meta["download_progress_callback"]` 设置进度回调，支持已知/未知大小响应，可配置最小报告间隔
- ⚙️ **新增 14 项配置** — `HTTP2_ENABLED` / `DOWNLOAD_PROGRESS_ENABLED` / `CONNPOOL_*` 系列连接池参数
- 🧪 **30 个新增测试** — HTTP/2 处理器 12 个 + 连接池 6 个 + 进度回调 12 个，`go test -race` 全部通过
- 📊 **性能无回退** — QPS ~18,754（基线 ~18,900），新增功能默认关闭，零开销

### v1.0.1

> **性能优化 P4-007j：Scheduler 双锁分离（入队/出队解耦）** ⚠️ *已在 v1.1.6 中替换为单队列设计以修复优先级排序问题*

- ⚡ ~~**Scheduler 双锁分离**~~ — 已在 v1.1.6 中回退为单锁设计，修复跨批次优先级失效问题
- ⚡ **DupeFilter sync.Map 无锁化** — `LoadOrStore` 原子操作替代全局锁，高并发去重性能提升 4.6~6.6x
- ⚡ **HasPendingRequests/Len atomic 无锁** — `pendingCount atomic.Int64`，~9,300x faster
- 📉 **Scheduler sec/op geomean -58.73%**，B/op -24.15%，端到端 QPS (16c) -5.42%
- 🧪 **新增 Scheduler 微基准测试** — 5 个场景覆盖入队/出队/并发/去重/查询

### v1.0.1-alpha.4

> **性能优化 P4-007i：Downloader Slot Worker Pool 化**

- ⚡ **Slot Worker Pool 化** — 将 per-request `go func()` 重构为固定大小 Worker Pool（N = concurrency），消除 goroutine 创建/销毁开销
- ⚡ **移除 transferSem** — 由 worker 数量天然限制并发，减少信号量 Acquire/Release 开销
- 📉 **SlotEnqueue allocs -50%**，time -19~24%，1ms-c128 并发利用率 +46%（benchstat p<0.01）
- ⚠️ **P4-007i-4 决策** — Engine 层 Worker Pool 经实测收益为负，仅保留 Slot 层优化

### v1.0.1-alpha.3

> **性能优化 P4-007k + P4-007l + P4-007m：Meta 预初始化 & DefaultHeaders 直接赋值 & Handler Header 复用**

- ⚡ **Meta 预初始化** — `NewRequest` 预分配 `make(map[string]any, 4)`，消除中间件 SetMeta 懒初始化分配（~90 MB）
- ⚡ **DefaultHeaders 直接赋值** — slice 引用直接赋值替代逐个 Add，消除 slice 扩容分配（~91 MB）
- ⚡ **Handler Header 复用** — `httpReq.Header = request.Headers` 零拷贝复用，消除 make(http.Header) 分配（~65 MB）
- ⚠️ **安全性注释增强** — 三处优化均添加详细的对象复用安全约束警告注释

### v1.0.1-alpha.2

> **性能优化 P4-007c + P4-007d：信号系统快速跳过 & NeedsBackout atomic 无锁化**

- ⚡ **信号系统快速跳过** — `HasHandlers` atomic 无锁快速路径，无处理器时跳过 map 创建（Allocs -7.3%, B/op -9.9%）
- ⚡ **NeedsBackout 无锁化** — `activeCount atomic.Int64` 替代 RWMutex，调度循环高频调用性能提升 ~30,000x
- ⚡ **热路径前置检查** — 每请求节省 4 次 map 分配 + 4 次 RLock
- 🧪 **新增微基准测试** — NeedsBackout atomic vs RWMutex 对比 benchmark（6 个场景）

### v1.0.1-alpha.1

> **性能优化 P4-007a：HTTPDownloadHandler 避免 URL 重复解析**

- ⚡ **消除 URL 重复解析** — `Download()` 直接构造 `http.Request{URL: request.URL}`，避免 URL 序列化+反序列化（CPU -34%, Allocs -46%）
- ⚡ **请求头零拷贝赋值** — Header 复制改为直接 slice 引用赋值，减少内存分配
- 🧪 **新增 Benchmark 套件** — `pkg/downloader/handler_bench_test.go` 提供 6 个性能基准测试

### v1.0.0 🎉

> **scrapy-go v1.0.0 正式发布** — 生产就绪的 Go 语言高性能爬虫框架

- 🎉 **首个正式发布版本** — 经过 Phase 1-4 共 22 周迭代，框架达到生产就绪状态
- 📊 **性能验证** — QPS ~18,900（16 并发），内存 ~12MB heap_inuse（10 万请求），框架开销仅 1.11x（测量条件：本地 benchmark 服务器，TestQPSAcceptance 10000 请求，TestMemoryAcceptance 100000 请求，GOMAXPROCS=96，无 pprof 开销）
- ✅ **全量回归测试** — `tests/integration/phase4_test.go` 覆盖所有核心功能端到端验证（10 个测试场景）
- 📖 **文档完备** — API 参考 + 用户指南 + 架构设计 + 迁移指南全部完成
- 🔒 **质量保证** — 测试覆盖率 83.6%，竞态检测全部通过，静态分析零告警

### v0.6.0-alpha.10

> **Phase 4 Sprint 11 — 文档完善：用户指南 + 架构设计文档 + 迁移指南** — P4-005b/c/d

- 📖 **用户指南 Getting Started** — `docs/guide/getting-started.md` 完整用户指南（安装/创建项目/Spider 编写/选择器/链接跟踪/Pipeline/Feed Export/CrawlSpider/配置/中间件/多爬虫/优雅关闭/调试）
- 📖 **架构设计文档** — `docs/architecture/architecture.md` 完整架构文档（五层架构/核心组件内部结构/数据流时序图/并发模型/信号系统/中间件链/扩展系统/配置体系/错误处理/与 Scrapy 差异）
- 📖 **迁移指南** — `docs/migration/migration-from-python.md` 从 Python Scrapy 迁移对照手册（30+ 概念映射/完整代码对比/API 对照表/4 种迁移模式/性能对比/迁移检查清单）
- ✅ **P4-005 文档完善全部完成** — API 参考文档 ✅ / 用户指南 ✅ / 架构设计文档 ✅ / 迁移指南 ✅

### v0.6.0-alpha.9

> **Phase 4 Sprint 11 — 基础设施层 API 参考文档（godoc 格式）** — P4-005a-v

- 📖 **`pkg/settings` 包文档** — `doc.go` 完整包级别 godoc（六级优先级体系/配置冻结/TOML 加载/组件优先级字典/9 种 Get 方法）
- 📖 **`pkg/signal` 包文档** — `doc.go` 完整包级别 godoc（18 种信号/处理器注册注销/三种发送方式/特殊错误语义）
- 📖 **`pkg/stats` 包文档** — `doc.go` 完整包级别 godoc（Collector 接口/MemoryCollector/DummyCollector/15+ 常用统计项）
- 📖 **`pkg/extension` 包文档** — `doc.go` 完整包级别 godoc（Extension 接口/Manager/5 个内置扩展详解/自定义扩展示例）
- 📖 **`pkg/spider` 包文档** — `doc.go` 完整包级别 godoc（Spider 接口/Base/CrawlSpider/Rule/Settings 类型安全配置）
- 📖 **`pkg/errors` 包文档** — `doc.go` 完整包级别 godoc（13 个哨兵错误/6 种结构化错误/IsRetryable/使用模式）
- 📖 **`pkg/log` 包文档** — `doc.go` 完整包级别 godoc（3 种 Logger/ColorHandler/上下文关联/便捷函数）
- 📖 **`pkg/pool` 包文档** — `doc.go` 完整包级别 godoc（3 个对象池/使用模式/性能收益/注意事项）
- 📖 **`pkg/debug` 包文档** — `doc.go` 完整包级别 godoc（PprofExtension/8 个端点/安全注意事项）
- 🧪 **Example 函数** — 12 个 godoc Example（Settings 多优先级/冻结/Duration/组件字典/Signal 注册/发送/Stats 收集器/Extension 管理器/CloseSpider/Spider Base/Settings ToMap）
- ✅ **所有导出符号注释完备** — Settings/Signal/Manager/Collector/Extension/Spider/CrawlSpider/Rule 及全部导出方法均有 godoc 注释
- ✅ **P4-005a API 参考文档全部完成** — 5 层 godoc 全部交付（核心引擎层 ✅ / HTTP 与选择器层 ✅ / 调度与下载层 ✅ / 数据处理层 ✅ / 基础设施层 ✅）

### v0.6.0-alpha.8

> **Phase 4 Sprint 11 — 数据处理层 API 参考文档（godoc 格式）** — P4-005a-iv

- 📖 **`pkg/item` 包文档** — `doc.go` 完整包级别 godoc（ItemAdapter 体系/适配检测顺序/Struct Tag 语法/自定义适配器）
- 📖 **`pkg/pipeline` 包文档** — `doc.go` 完整包级别 godoc（Pipeline 接口/泛型 TypedPipeline/CrawlerAwarePipeline/处理流程/信号集成）
- 📖 **`pkg/feedexport` 包文档** — `doc.go` 完整包级别 godoc（4 种 Exporter/2 种 Storage/Exporter 生命周期/FeedSlot 工作流）
- 🧪 **Example 函数** — 9 个 godoc Example（Adapt/Struct/IsItem/AsMap/Manager/TypedPipeline/NormalizeFormat/ExporterOptions/AcceptAll）
- ✅ **所有导出符号注释完备** — ItemAdapter/MapAdapter/StructAdapter/Pipeline/Manager/TypedPipeline/ItemExporter/FeedStorage 及全部导出方法均有 godoc 注释

### v0.6.0-alpha.7

> **Phase 4 Sprint 11 — 调度与下载层 API 参考文档（godoc 格式）** — P4-005a-iii

- 📖 **`pkg/scheduler` 包文档** — `doc.go` 完整包级别 godoc（双层队列架构/去重机制/优先级调度/断点续爬）
- 📖 **`pkg/downloader` 包文档** — `doc.go` 完整包级别 godoc（Slot 机制/并发控制/延迟控制/信号集成）
- 📖 **`pkg/downloader/middleware` 包文档** — `doc.go` 完整包级别 godoc（ISP 接口隔离/12 个内置中间件一览/处理流程/返回值语义）
- 🧪 **Example 函数** — 8 个 godoc Example（Scheduler 创建/去重/断点续爬/Downloader/Handler/MiddlewareManager/RequestProcessor/Base 嵌入）
- ✅ **所有导出符号注释完备** — Scheduler/DupeFilter/Queue/Downloader/Slot/Handler/MiddlewareManager 及全部导出方法均有 godoc 注释

### v0.6.0-alpha.6

> **Phase 4 Sprint 11 — HTTP 与选择器层 API 参考文档（godoc 格式）** — P4-005a-ii

- 📖 **`pkg/http` 包文档** — `doc.go` 完整包级别 godoc（核心类型/请求构造器/Options/序列化/Headers 工具/与 Scrapy 差异）
- 📖 **`pkg/selector` 包文档** — `doc.go` 完整包级别 godoc（CSS/XPath/链式查询/伪元素支持/性能说明）
- 📖 **`pkg/linkextractor` 包文档** — `doc.go` 完整包级别 godoc（过滤规则/CrawlSpider 集成/配置选项）
- 🧪 **Example 函数** — 15 个 godoc Example（Request/FormRequest/JSON/cURL/Response CSS/Follow/Headers/Registry/Selector/XPath/CSSAttr/List/LinkExtractor/Filters/Matches）
- ✅ **所有导出符号注释完备** — Request/Response/Selector/List/LinkExtractor/HTMLLinkExtractor 及全部导出方法均有 godoc 注释

### v0.6.0-alpha.5

> **Phase 4 Sprint 11 — 核心引擎层 API 参考文档（godoc 格式）** — P4-005a-i

- 📖 **`pkg/crawler` 包文档** — `doc.go` 完整包级别 godoc（概述/架构图/使用方式/与 Scrapy 差异/并发安全/优雅关闭）
- 📖 **`pkg/engine` 包文档** — `doc.go` 完整包级别 godoc（核心职责/并发模型/回退机制/优雅关闭 8 步流程/信号系统 9 种信号）
- 📖 **`pkg/scraper` 包文档** — `doc.go` 完整包级别 godoc（并发控制/回退机制/错误处理/Panic 恢复）
- 🧪 **Example 函数** — 8 个 godoc Example（Crawler 创建/Option 配置/Runner 并发/Runner 顺序/Engine 创建/Engine 暂停/Scraper 创建/Scraper 回退）
- ✅ **所有导出符号注释完备** — Crawler/Runner/Engine/Slot/Scraper 及全部导出方法、Option、错误变量均有 godoc 注释

### v0.6.0-alpha.4

> **Phase 4 Sprint 11 — 可观测性接口定义** — 轻量级 Telemetry 扩展点

- 🔭 **Tracer 接口** — `pkg/telemetry/tracer.go`，定义 `Tracer`/`Span`/`SpanContext` 轻量级接口，支持分布式追踪
- 📊 **Metrics 接口** — `pkg/telemetry/metrics.go`，定义 `Counter`/`Gauge`/`Histogram`/`MetricsRegistry` 指标接口
- ⚡ **零开销 Noop 实现** — `pkg/telemetry/noop.go`，`NoopTracer` + `NoopMetricsRegistry` 默认实现，未配置后端时无运行时开销
- ✅ **100% 测试覆盖** — 26 个测试用例，覆盖接口契约/并发安全/Extension 集成点，`go test -race` 通过
- 🧩 **可插拔架构** — 具体 OTel/Prometheus 适配器由 `contrib/telemetry` 独立子模块提供（Post-v1.0）

### v0.6.0-alpha.3

> **Phase 4 Sprint 11 — CI 集成自动化 benchmark 回归** — 持续集成流水线完善

- 🔄 **主 CI 工作流** — `.github/workflows/ci.yml`，Lint → Test(race+coverage) → Build → Benchmark Acceptance 完整流水线
- 📊 **Benchmark 回归工作流** — `.github/workflows/benchmark.yml`，自动化性能基线存储 + benchstat 对比 + PR 评论回归警告
- ✅ **覆盖率门禁** — CI 强制要求全局覆盖率 >= 80%，未达标则失败
- 🛡️ **竞态检测** — 所有测试强制 `-race` 标志
- 📁 **路径过滤** — Benchmark 工作流仅在核心代码变更时触发，避免无关 PR 浪费资源

### v0.6.0-alpha.2

> **Phase 4 Sprint 11 — 与 Colly/Geziyor 真实框架对比测试** — 框架性能对比验证

- 🔄 **真实框架对比测试** — 引入 Colly v2.1.0 和 Geziyor 真实库，独立子模块 `benchmarks/comparison/` 避免依赖污染
- ⚖️ **公平对比设计** — scrapy-go 保留所有默认中间件（重试/Cookie/压缩/代理/robots.txt），Colly/Geziyor 使用默认配置
- 📊 **对比报告** — 格式化输出 QPS 表、框架开销比表、内存分配表（4 个并发级别 × 4 种框架）
- ✅ **开销验收** — scrapy-go 带完整中间件栈 QPS 仍为 Colly 的 ~2.5x、Geziyor 的 ~2.0x
- ⏱️ **延迟场景** — 10ms 延迟下 scrapy-go 并发调度效率 92%，与裸 HTTP 客户端持平
- 🏃 **Go Benchmark 集成** — 8 个标准 benchmark 函数，支持 `go test -bench BenchmarkComparison`

### v0.6.0-alpha.1

> **Phase 4 Sprint 11 — 性能基准测试套件** — QPS 吞吐量 + 内存效率验证

- 📊 **本地 Benchmark 服务器** — `benchmarks/server/` 包，轻量级 HTTP 服务器，多端点支持（最小响应/HTML/JSON/延迟/链接页面）
- ⚡ **QPS 基准测试** — 5 个并发级别（8/16/32/64/128），验收标准 16 并发 >= 5000 QPS（实测 ~17000）
- 🧠 **内存占用基准测试** — 10k/50k/100k 请求量级，验收标准 10 万请求 < 500MB（实测 ~151MB）
- 🔍 **内存泄漏检测** — 5 阶段爬取验证堆内存稳定（~4.5MB），无泄漏
- 🏃 **细粒度分配测试** — Request 创建/拷贝/Crawler 创建的 allocs/op 基线

### v0.5.1

> **配置模板完善** — TOML 模板补全 + 移除无实现配置项

- 📄 **TOML 模板补充完整配置项** — 重试、重定向、Cookies、HTTP 代理/压缩、缓存、统计、断点续爬、深度控制、关闭条件、优雅关闭、数据导出、调试监控
- 🗑️ **移除无实现的 DNS 配置项** — `DNSCACHE_ENABLED`/`DNSCACHE_SIZE`/`DNS_TIMEOUT`（Go 依赖 OS 层 DNS 缓存）

### v0.5.0 🎉

> **生产就绪版本** — 并发模型优化 + 接口隔离 + 泛型 Pipeline + TOML 配置

- ⚡ **errgroup 生命周期管理** — Engine 使用 `errgroup` 统一管理多 goroutine，任一出错自动取消 context
- 🔒 **semaphore.Weighted** — 替代 channel 信号量，Slot 并发控制 + CONCURRENT_ITEMS 均支持 context 取消
- 🏊 **sync.Pool 对象池** — 新增 `pkg/pool` 包，Request/Response/Bytes 对象复用减少 GC 压力
- 🛑 **两阶段优雅关闭** — 第一次 SIGINT 等待 in-flight + Pipeline 排空，第二次强制退出
- 🔍 **pprof 调试端点** — 新增 `pkg/debug` 包，`PPROF_ENABLED` 配置控制 pprof HTTP 端点
- 📊 **DiskQueue 优化** — 有序优先级切片，Pop O(1) / Push O(log N)，偿还 TD-010
- 🧩 **DL Middleware 接口隔离** — 拆分为 `RequestProcessor`/`ResponseProcessor`/`ExceptionProcessor` 细粒度接口
- 🔬 **TypedPipeline[T] 泛型 Pipeline** — 编译期约束 Item 类型，类型不匹配自动跳过
- 🏷️ **struct tag 字段元数据** — `item:"name,required"` / `item:"name,default=value"` + Validate 函数
- ⚙️ **go generate 代码生成器** — `scrapy-go generate-adapter` 自动生成 ItemAdapter 实现
- 📄 **TOML 配置文件加载** — `scrapy-go.toml` 自动探测，`PriorityAddon` 级别，支持标量/列表/map 类型
- ✅ **Phase 3 集成测试套件** — 11 个端到端场景覆盖 CrawlSpider/RobotsTxt/HttpCache/FormRequest/优雅关闭等

### v0.5.0-beta.2

> **Sprint 10 功能交付** — 并发模型优化 + 接口隔离 + 泛型 Pipeline + Item 体系增强

- ⚡ **errgroup 生命周期管理** — Engine 使用 `errgroup` 统一管理多 goroutine，任一出错自动取消 context
- 🔒 **semaphore.Weighted** — 替代 channel 信号量，Slot 并发控制 + CONCURRENT_ITEMS 均支持 context 取消
- 🏊 **sync.Pool 对象池** — 新增 `pkg/pool` 包，Request/Response/Bytes 对象复用减少 GC 压力
- 🛑 **两阶段优雅关闭** — 第一次 SIGINT 等待 in-flight + Pipeline 排空，第二次强制退出
- 🔍 **pprof 调试端点** — 新增 `pkg/debug` 包，`PPROF_ENABLED` 配置控制 pprof HTTP 端点
- 📊 **DiskQueue 优化** — 有序优先级切片，Pop O(1) / Push O(log N)，偿还 TD-010
- 🧩 **DL Middleware 接口隔离** — 拆分为 `RequestProcessor`/`ResponseProcessor`/`ExceptionProcessor` 细粒度接口
- 🔬 **TypedPipeline[T] 泛型 Pipeline** — 编译期约束 Item 类型，类型不匹配自动跳过
- 🏷️ **struct tag 字段元数据** — `item:"name,required"` / `item:"name,default=value"` + Validate 函数
- ⚙️ **go generate 代码生成器** — `scrapy-go generate-adapter` 自动生成 ItemAdapter 实现
- 📋 全部测试通过，`go test -race` 无竞态，核心包覆盖率 ≥85%

### v0.5.0-beta.1 🎉

> **首个公开体验版** — Phase 3 核心功能全部可用

- 🗄️ **HttpCache 中间件**（P3-005）— 可插拔缓存存储 + 缓存策略，支持 DummyPolicy / RFC2616Policy
- 🛠️ **项目脚手架工具**（P3-006）— `startproject` / `genspider` / `version` 命令，零外部依赖
- 🔒 **genspider 项目检测强制化** — 必须在项目中执行，settings 升级为项目级配置
- 🏗️ **项目结构重构** — 组件文件分离到 `project/` 子包，符合 Go 多包规范
- 📝 **爬虫模板增强** — 新增 `CustomSettings()` 方法和包注释
- 📋 全部测试通过，`go test -race` 无竞态，核心包覆盖率 ≥80%

### v0.5.0-alpha.5

- 📝 **爬虫模板增强** — basic/crawl 模板新增 `CustomSettings()` 方法和包注释
  - 新增 `// Package spiders` 包注释，符合 Go 文档规范
  - 新增 `CustomSettings() *spider.Settings` 方法，支持 Spider 级别配置覆盖（默认返回 nil）

### v0.5.0-alpha.4

- 🔒 **genspider 项目检测强制化** — `genspider` 命令必须在 scrapy-go 项目中执行
  - 检测当前目录是否存在 `scrapy-go.toml`，不存在则直接报错
  - 移除 standalone 模式（不再支持项目外生成爬虫文件）
  - 爬虫文件固定生成到 `spiders/` 目录，使用 `package spiders`
- 🔧 **settings 模板升级为项目级配置** — 使用 `settings.Settings` + `PriorityProject` 优先级
  - 替换原有的 `spider.Settings`（爬虫级配置）为 `settings.New()` + `s.Set(key, value, priority)` 模式
  - 支持 BOT_NAME / CONCURRENT_REQUESTS / ROBOTSTXT_OBEY / LOG_LEVEL 等项目级配置
  - `main.go` 模板更新为 `crawler.New(crawler.WithSettings(projectSettings))` 模式
- 46 个测试全部通过，`go test -race` 无竞态，cmd/scrapy-go 包覆盖率 81.3%

### v0.5.0-alpha.3

- 🏗️ **脚手架项目结构重构** — 将组件文件分离到 `project/` 子包
  - `settings.go` / `middlewares.go` / `pipelines.go` / `items.go` 从 `package main` 迁移到 `package project`
  - `main.go` 新增 `_ "<module>/project"` 导入，保持组件可达
  - 注释中的注册示例同步更新为 `project.` 前缀（如 `&project.MyPipeline{}`）
  - 生成的项目结构更符合 Go 多包组织规范，职责分离更清晰
- 42 个测试全部通过，`go test -race` 无竞态，cmd/scrapy-go 包覆盖率 81.0%

### v0.5.0-alpha.2

- 🛠️ **项目脚手架工具**（P3-006）— 命令行工具 `cmd/scrapy-go`，零外部依赖
  - `scrapy-go startproject <name> [dir]` — 创建完整项目骨架（main.go / project/ / spiders/ / go.mod / scrapy-go.toml）
  - `scrapy-go genspider <name> <domain>` — 生成爬虫文件，支持 basic（默认）和 crawl 两种模板
  - `scrapy-go version [-v]` — 打印版本信息（支持 Go 版本/平台详情）
  - `go:embed` 嵌入模板 + `text/template` 渲染，编译期打包无需运行时文件系统
  - 域名自动提取、URL scheme 补全、名称自动转换为 Go 大驼峰类型名
  - 舍弃 `crawl`/`list`/`settings` 命令（Go 静态编译无法动态加载 Spider）
- 42 个测试全部通过，`go test -race` 无竞态，cmd/scrapy-go 包覆盖率 82.1%

### v0.5.0-alpha.1

- 🗄️ **HttpCache 中间件**（P3-005）— HTTP 缓存中间件，注册优先级 900
  - `CacheStorage` + `CachePolicy` 可插拔接口设计
  - `FilesystemCacheStorage` — JSON 元数据 + gzip 压缩 + 原子写入 + 按指纹分桶目录
  - `DummyPolicy` — 无条件缓存策略（排除指定 scheme/status code）
  - `RFC2616Policy` — HTTP 缓存语义策略（Cache-Control/Expires/ETag/Last-Modified/条件验证）
  - 9 种统计指标（hit/miss/store/firsthand/revalidate/invalidate/uncacheable/errorrecovery/ignore）
  - `dont_cache` Meta 跳过 + `HTTPCACHE_IGNORE_MISSING` 模式 + 下载异常错误恢复
  - 10 个新增配置项（`HTTPCACHE_ENABLED`/`HTTPCACHE_DIR`/`HTTPCACHE_POLICY` 等）
- 951 个测试全部通过，`go test -race` 无竞态，httpcache 包覆盖率 88.9%

### v0.4.0 🎉

- 🕷️ **CrawlSpider 基于规则的自动爬取**（P3-001）— `HTMLLinkExtractor` + `Rule` + `CrawlSpider`
- 🤖 **RobotsTxt 中间件**（P3-002）— 内置解析器，通配符支持，按 netloc 缓存
- 📝 **FormRequest 系列增强**（P3-012）— `FormRequestFromResponse` + `NewMultipartFormRequest`
- 🔄 **Request 序列化与 curl 互操作**（P3-013）— `ToDict`/`FromDict` + `CallbackRegistry` + `FromCURL`/`ToCURL`
- 💾 **磁盘队列与断点续爬**（P3-003）— `DiskQueue` 即时写盘 + `RFPDupeFilter` 持久化 + JOBDIR 集成
- 873 个测试全部通过，`go test -race` 无竞态，核心包覆盖率 ≥82%

### v0.3.0 🎉

> **Phase 2 正式发布** — 扩展体系与数据导出

- 🏛️ **Extension 系统** — 完整的扩展接口 + 5 个内置扩展（CoreStats / CloseSpider / LogStats / MemoryUsage / AutoThrottle）
- 📤 **Feed Export 数据导出** — JSON / JSON Lines / CSV / XML 四种格式 + 本地文件/标准输出存储 + URI 模板
- 📦 **Item 体系与 ItemAdapter** — 统一的 Item 访问抽象（map / struct / 自定义类型）+ FieldMeta 驱动序列化
- ⚡ **CONCURRENT_ITEMS** — 并发 Pipeline 处理（默认 100，对齐 Scrapy）
- 🔧 **Request 便捷 API** — `NewJSONRequest` / `NewFormRequest` / `NoCallback` / `WithBasicAuth` / `WithUserAgent`
- 🔀 **CrawlerRunner** — 多爬虫调度器（并发/顺序/跨爬虫信号传播）
- 🔌 **下载器中间件** — HttpProxy（代理）+ DownloaderStats（统计）
- 🔍 **Spider 内置中间件** — HttpError / Offsite / Referer / UrlLength / Depth
- 🏠 **Pipeline FromCrawler** — 工厂约定，对齐 Scrapy `from_crawler`
- 📋 **626 个测试全部通过**，核心包覆盖率均 ≥85%
- 📖 **完善示例** — 重写 `feedexport` 示例覆盖全部核心 API + 新增 `itemadapter` 示例覆盖全部核心 API

### v0.3.0-alpha.10

- ⚡ **CONCURRENT_ITEMS 并发 Pipeline 处理**（P2-012）
  - Scraper 层引入信号量控制同时在 Pipeline 链中的 Item 上限（默认 100）
  - 多个 Item 之间并发处理，单 Item 内 Pipeline 仍串行（对齐 Scrapy 语义）
  - `Scraper.Close` 等待 in-flight Item 全部处理完毕（优雅关闭协同）
  - Item 处理 goroutine 内置 panic recovery
- 🔧 **Request 便捷 Option 与 JSON 支持**（P2-011）
  - `NewJSONRequest` — JSON API 请求构造器（对齐 Scrapy `JsonRequest`）
  - `NewFormRequest` — 表单请求构造器（对齐 Scrapy `FormRequest`）
  - `NoCallback` 哨兵值 + `IsNoCallback` 检测（对齐 Scrapy `NO_CALLBACK`）
  - `WithRawBody` / `WithBasicAuth` / `WithUserAgent` / `WithFormData` 便捷 Option
- 🧪 **Phase 2 集成测试**（P2-007）
  - 新增 8 个端到端集成测试覆盖 JSON API、表单请求、NoCallback、CONCURRENT_ITEMS、扩展系统、Basic Auth
- 📋 **全部 626 个测试通过**，`go test -race` 无竞态，`go vet` 无告警

### v0.3.0-alpha.9

- 🔧 **FieldMeta 驱动序列化 — Feed Export `serialize_field` 钩子**（P2-009-ext1）
  - 新增 `pkg/feedexport/serializer.go`：`RegisterSerializer` / `LookupSerializer` / `SerializeField` 注册表机制
  - JSON / JSON Lines / CSV / XML 四种 Exporter 均已接入 `serializeItemFields`，所有字段在写入前自动经过 serialize_field 钩子处理
  - struct 类型的 Item 可通过 `item:"price,serializer=to_int"` tag 声明序列化器
  - 对齐 Scrapy `BaseItemExporter.serialize_field`，采用注册表模式替代虚方法覆盖
- 🏭 **Pipeline `FromCrawler` 工厂约定**（P2-009-ext2）
  - 新增 `pipeline.Crawler` 接口（`GetSettings` / `GetStats` / `GetSignals` / `GetLogger`），在 pipeline 包定义避免循环依赖
  - 新增 `CrawlerAwarePipeline` 可选接口，Pipeline 可在 Open 前通过 `FromCrawler(c Crawler)` 获取 Crawler 引用
  - `Manager.SetCrawler` + `Manager.Open` 自动调用 FromCrawler
  - `crawler.Crawler` 新增 Getter 方法满足 `pipeline.Crawler` 接口
  - 对齐 Scrapy `from_crawler(cls, crawler)` 工厂方法约定（需求 13 验收标准 6）

### v0.3.0-alpha.8

- 📦 **Item 体系与 ItemAdapter**（P2-009） — 新增 `pkg/item` 包，提供统一的 Item 访问抽象
  - `ItemAdapter` 接口：`FieldNames` / `GetField` / `SetField` / `HasField` / `AsMap` / `Len` / `FieldMeta`
  - `MapAdapter`：适配 `map[string]any` / `map[string]string` / 其他 `key=string` 的 map
  - `StructAdapter`：基于 `reflect` 适配任意 struct，支持 `item` tag → `json` tag → Go 字段名优先级解析
  - `Adapt(item)` 自动检测工厂 + `Register(factory)` 自定义工厂注册
  - `FieldMeta` 字段元数据（从 struct tag 自动解析）
  - Feed Export 重写为 `item.Adapt` 的薄封装，所有 Exporter 通过 `ItemAdapter` 统一读取字段
- 🐛 **修复 item_scraped_count / item_dropped_count 重复计数** — Pipeline 直接 IncValue + CoreStats 信号双写导致计数翻倍，统一由 CoreStatsExtension 通过信号机制完成

### v0.3.0-alpha.7

- 📤 **Feed Export 数据导出系统**（P2-008） — 新增 `pkg/feedexport` 包，对齐 Scrapy `feedexport` + `exporters`
  - 四种内置格式：JSON / JSON Lines / CSV / XML
  - 两种存储后端：本地文件（`FileStorage`）、标准输出（`StdoutStorage`）
  - URI 模板占位符：`%(name)s` / `%(time)s` / `%(batch_id)d` / `%(batch_time)s`
  - `FeedExportExtension` 通过信号系统接入 Spider 生命周期
  - `Crawler.AddFeed()` 代码注入 + `Settings.FEEDS` 配置驱动
- 📋 **Request 便捷 API 规划登记** — P2-011 / P3-012 / P3-013 三项规划纳入迭代日程

### v0.3.0-alpha.6

- 🐛 **Engine closeSpider 收尾顺序修复** — 修复"先关闭扩展再派发 SpiderClosed 信号"导致的最终指标丢失问题，调整为信号派发 → 扩展关闭 → stats dump

### v0.3.0-alpha.5

- 🐛 **下载层统计职责归位** — 移除 Engine 中越界的 `response_received_count` 和 `downloader/response_status_count` 直接写入，统一由 CoreStats 扩展和 DownloaderStats 中间件通过信号/中间件机制完成

### v0.3.0-alpha.4

- 🔀 **CrawlerRunner 多爬虫调度器**（P2-010） — 新增 `crawler.Runner` 实现 Scrapy `CrawlerRunner` 的对等能力
  - `Crawl(ctx, c, sp)` 异步启动单个 Crawler
  - `StartConcurrent(ctx, jobs...)` 并发运行多个 Spider
  - `StartSequentially(ctx, jobs...)` 顺序运行多个 Spider
  - `ConnectSignal(sig, handler)` 跨爬虫信号处理器广播
  - `Stop()` / `Wait()` / `Close()` 统一停止、等待与关闭接口
  - 内置 OS 信号处理（两阶段 SIGINT：第一次优雅关闭，第二次强制退出）
  - 使用 `sync.WaitGroup` + channel 替代 Twisted Deferred / asyncio.Task 集合
  - 多 Crawler 错误通过 `errors.Join` 聚合，自动忽略 `context.Canceled`/`DeadlineExceeded`
- 🆕 **Crawler 新增 API**
  - `Crawler.Crawl(ctx, sp)` — 不安装 OS 信号处理器的爬取入口（供 Runner 调用）
  - `Crawler.Stop()` — 请求优雅停止，多次调用安全
  - `Crawler.Spider()` / `Crawler.IsCrawling()` — 状态查询辅助方法
  - Crawler 实例只能运行一次（CAS 保护），避免误用

### v0.3.0-alpha.3

- 🕷️ **Spider 内置中间件（5 个）** — HttpError(50)、Offsite(500)、Referer(700)、UrlLength(800)、Depth(900)

### v0.3.0-alpha.2

- 🧩 **内置扩展（4 个）** — CoreStats、CloseSpider、LogStats、MemoryUsage

### v0.3.0-alpha.1

- 🎛 **Extension 系统框架** — 定义 `Extension` 接口 + `ExtensionManager` 生命周期管理
- 🌐 **HttpProxy 中间件**（优先级 750） — 环境变量代理 + 请求级代理 + 代理认证
- 📊 **DownloaderStats 中间件**（优先级 850） — 请求/响应/异常/耗时多维度统计

### v0.2.3

- 🔧 **NewRequestError 处理修复** — 在中间件管理器的 `processResponse` 和 `processException` 中添加 `NewRequestError` 的显式检查，确保重试/重定向产生的新请求能正确传播给 Engine 重新调度
- 🏗 **MiddlewareManager 重构** — 将下载器中间件管理器从 `pkg/downloader/middleware/` 移到 `pkg/downloader/` 包下
  - `middleware.Manager` → `downloader.MiddlewareManager`
  - `middleware.Entry` → `downloader.MiddlewareEntry`
  - 更贴近 Scrapy 原版设计（Manager 属于 downloader 核心，而非中间件本身）
  - Engine 可直接使用 `downloader.MiddlewareManager`，无需包别名

### v0.2.2

- 🛡️ **Panic Recovery** — 为所有关键 goroutine 添加 panic 恢复机制
  - Engine: `downloadAndScrape`、`consumeStartRequests`
  - Downloader: `processQueue`（自动重启）、下载 goroutine
  - Spider: `Base.Start()` 内部 goroutine
- 🆕 **PanicError** — 新增 `ErrPanic` 哨兵错误和 `PanicError` 结构化错误类型
- 📊 **HTTP 状态码统计** — 自动统计响应状态码数量（`downloader/response_status_count/XXX`）
- 📈 **Panic 统计** — 自动递增 `spider_exceptions/panic` 计数器

### v0.2.1

- 🌍 **日志英文化** — 所有框架日志统一改为英文格式
- 🎨 **彩色日志** — 新增 `ColorHandler`，不同级别使用不同 ANSI 颜色
- 📋 **Scrapy 风格日志** — 中间件、Pipeline、统计信息使用列表格式输出
- 📦 **Pipeline 日志** — 补充 Pipeline 组件的启用状态日志

### v0.2.0

- 🎉 Phase 1 全部功能完成

---

## 📄 License

MIT
