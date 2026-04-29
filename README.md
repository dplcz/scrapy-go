# 🕷️ scrapy-go

**scrapy-go** 是一个用 Go 语言实现的高性能异步爬虫框架，架构设计对齐 Python [Scrapy](https://scrapy.org/)，在保留 Scrapy 核心设计理念的同时，充分利用 Go 的并发模型和类型安全特性，提供更高的运行效率和更低的资源消耗。

> 📌 当前版本：**v0.3.0** &nbsp;|&nbsp; 📋 [更新日志](#-更新日志)

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

- **Engine** — 核心调度引擎，协调所有组件，支持暂停/恢复
- **Scheduler** — 基于内存优先级队列的请求调度
- **Downloader** — 基于 Slot 机制的 HTTP 下载，按域名分组控制并发和延迟
- **Scraper** — 调用 Spider 回调并分发结果（Request/Item）
- **Crawler** — 顶层编排器，一行代码组装并启动爬虫
- **Runner** — 多爬虫调度器，支持并发/顺序运行多个 Spider 并统一信号传播（对齐 Scrapy 的 `CrawlerRunner`）

### 📡 请求与响应

- **Request** — 支持多种 HTTP 方法、自定义 Headers、Cookies、Meta 元数据、优先级、Callback/Errback
- **NewJSONRequest** — JSON API 请求构造器，自动设置 Content-Type 和序列化 Body（对齐 Scrapy `JsonRequest`）
- **NewFormRequest** — 表单请求构造器，POST 写 Body / GET 写查询参数（对齐 Scrapy `FormRequest`）
- **NoCallback** — 哨兵值，标记请求不需要回调函数（对齐 Scrapy `NO_CALLBACK`）
- **便捷 Option** — `WithRawBody` / `WithBasicAuth` / `WithUserAgent` / `WithFormData`
- **Response** — 支持 Text/JSON 解析、URLJoin 相对路径解析、Follow 链接跟踪、CSS/XPath 选择器
- **Functional Options** — 类型安全的构建模式

### 🔁 去重与调度

- **RFPDupeFilter** — 基于请求指纹（URL + Method + Body SHA1）去重
- **DontFilter** — 支持跳过去重（如初始请求）
- **NoDupeFilter** — 可选的无过滤模式
- **优先级队列** — 高优先级请求优先处理

### ⏱️ 并发与延迟控制

- 全局并发限制（`CONCURRENT_REQUESTS`）
- Item 并发处理（`CONCURRENT_ITEMS`，默认 100，对齐 Scrapy）
- 域名级并发限制（`CONCURRENT_REQUESTS_PER_DOMAIN`）
- 可配置下载延迟及随机化
- 通过 `download_slot` Meta 自定义分组

### 🔌 下载器中间件

支持优先级排序的中间件执行链（ProcessRequest 正序、ProcessResponse 逆序），内置 8 个中间件：

| 中间件 | 优先级 | 功能 |
|--------|--------|------|
| DownloadTimeout | 300 | 请求超时控制 |
| DefaultHeaders | 400 | 自动注入默认请求头 |
| HttpAuth | 410 | Basic Auth 认证 |
| UserAgent | 500 | User-Agent 设置 |
| Retry | 550 | 失败请求自动重试 |
| HttpCompression | 590 | gzip/deflate/brotli 解压 |
| Redirect | 600 | HTTP 重定向处理 |
| Cookies | 700 | 多会话 Cookie 管理 |

通过实现 `DownloaderMiddleware` 接口或嵌入 `BaseDownloaderMiddleware` 自定义扩展。

### 🔍 HTML 解析（Selector）

- 内置 CSS 和 XPath 选择器（基于 goquery/htmlquery）
- 支持 `::text` 伪元素和属性提取
- 链式调用和批量操作（`GetAll()` / `Get()` / `First()`）
- Response 快捷方法：`CSS()` / `CSSAttr()` / `XPath()` / `Selector()`

### 🕷️ CrawlSpider 基于规则的自动爬取

- **CrawlSpider** — 基于 Rule 规则的自动链接提取和跟踪（对齐 Scrapy `CrawlSpider`）
- **LinkExtractor 接口** — 可插拔的链接提取器，内置 `HTMLLinkExtractor`（基于 goquery）
- **Rule 规则** — 支持 `allow`/`deny` 正则过滤、域名过滤、`restrictCSS`/`restrictXPath` 范围限制
- **Callback/Errback** — 直接接受函数值（舍弃 Scrapy 字符串方法名反射，更符合 Go 风格）
- **ProcessLinks/ProcessRequest** — 链接和请求后处理钩子
- **跨规则去重** — 同一链接只被第一个匹配的规则处理
- **Functional Options** — 全部配置通过 `WithAllow`/`WithDeny`/`WithAllowDomains` 等选项函数设置

### 🕸️ Spider 中间件 & 📦 Item Pipeline

- **Spider 中间件** — 拦截 Spider 输入（响应）和输出（Request/Item），支持自定义扩展
- **Item Pipeline** — 按优先级顺序处理 Item，支持数据清洗、验证、持久化
- **DropItem** — 丢弃无效 Item，中断后续处理链
- **FromCrawler 工厂约定** — Pipeline 可实现 `CrawlerAwarePipeline` 接口，在 Open 前获取 Crawler 引用以访问 Settings/Stats/Signals
- **ItemAdapter 统一访问** — 通过 `pkg/item.Adapt` 以统一接口访问 `map` / `struct` / 自定义类型，Pipeline 无需关心 Item 具体类型

### 📦 Item 体系（ItemAdapter）

- 对齐 Scrapy 的 [`itemadapter`](https://github.com/scrapy/itemadapter) 设计，提供统一的 Item 访问抽象
- **三类内置适配**
  - `MapAdapter` — `map[string]any` / `map[string]string` / 其他 `key=string` 的 map
  - `StructAdapter` — 任意 struct / *struct，字段名解析顺序 `item` tag → `json` tag → Go 字段名，支持 `item:"-"` 显式隐藏
  - 自实现 — 业务方只需实现 `ItemAdapter` 接口即可被自动识别
- **扩展点** — `item.Register(factory)` 注册自定义工厂（支持 ORM 模型、protobuf Message 等）
- **字段元数据** — `item`/`json` tag 的非首个 token 自动进入 `FieldMeta`，供下游 Exporter / Pipeline 读取
- **FieldMeta 驱动序列化** — Feed Export 根据 `FieldMeta` 中的 `serializer` 键自动调用已注册的序列化函数（如 `item:"price,serializer=to_int"`），未命中回退原始值
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

### 📊 统计与日志

- **统计收集** — 基于内存的 MemoryCollector，Spider 关闭时自动 Dump
- **HTTP 状态码统计** — 自动统计每个响应状态码数量
- **彩色结构化日志** — 基于 `slog` 的英文日志，终端彩色输出
- **Scrapy 风格日志** — 中间件、Pipeline、统计信息使用列表格式输出
- **优雅关闭** — 监听 SIGINT/SIGTERM 信号

### 🛡️ Panic Recovery

- 所有关键路径均内置 panic 恢复（Spider 回调、Pipeline、Downloader、Start 方法）
- panic 不会导致进程崩溃
- 恢复后转换为结构化的 `PanicError`（含堆栈信息）
- 支持 `errors.Is(err, ErrPanic)` 匹配
- 自动递增 `spider_exceptions/panic` 统计

### 📦 外部依赖

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/PuerkitoBio/goquery` | v1.12.0 | CSS 选择器引擎 |
| `github.com/antchfx/htmlquery` | v1.3.6 | XPath 查询引擎 |
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

scrapy-go 支持三种配置方式（按优先级从低到高）：

**① 框架默认配置** — 所有配置项都有合理的默认值，开箱即用

**② 全局配置** — 通过 `Settings` 对象设置：

```go
s := settings.New()
s.Set("CONCURRENT_REQUESTS", 32, settings.PriorityProject)
s.Set("DOWNLOAD_DELAY", time.Second, settings.PriorityProject)

c := crawler.New(crawler.WithSettings(s))
```

**③ Spider 级别配置** — 通过 `CustomSettings()` 返回类型安全的配置：

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
<summary>🔌 <b>中间件配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DOWNLOADER_MIDDLEWARES_BASE` | map[string]int | 见下方 | 内置下载器中间件及优先级 |
| `DOWNLOADER_MIDDLEWARES` | map[string]int | {} | 用户自定义中间件优先级覆盖 |
| `SPIDER_MIDDLEWARES_BASE` | map[string]int | 见下方 | 内置 Spider 中间件及优先级 |
| `SPIDER_MIDDLEWARES` | map[string]int | {} | 用户自定义 Spider 中间件优先级覆盖 |

内置下载器中间件默认优先级：

```
DownloadTimeout:  300
DefaultHeaders:   400
HttpAuth:         410
UserAgent:        500
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
<summary>🌐 <b>HTTP 代理配置</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `HTTPPROXY_ENABLED` | bool | true | 是否启用 HttpProxy 中间件 |
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
| **Extension** | `pkg/extension` | 扩展系统（4 个内置扩展 + 信号驱动生命周期管理） |
| **DL Middleware** | `pkg/downloader/middleware` | 下载器中间件接口与实现（10 个内置中间件） |
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
| 💾 磁盘队列 | Scheduler 仅支持内存队列，不支持磁盘持久化 |
| 🗄️ HTTP 缓存 | 缓存中间件未实现 |
| 🤖 Robots.txt | RobotsTxt 中间件未实现 |
| 🗜️ Zstd | HttpCompression 暂不支持 zstd |
| 🐚 Scrapy Shell | 不支持交互式调试 |
| 🌍 分布式爬取 | 不支持分布式调度 |

### ⚠️ 已知约束

- **Go 版本要求** — 需要 Go 1.25.1+
- **回调函数类型** — `Callback`/`Errback` 使用 `any` 类型，需运行时类型断言

---

## 📁 项目结构

```
scrapy-go/
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
│   │   └── middleware/             # 下载器中间件接口与实现（10 个内置）
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

## 📝 更新日志

### v0.3.0 🎉

> **Phase 2 正式发布** — 扩展体系与数据导出

- 🏛️ **Extension 系统** — 完整的扩展接口 + 4 个内置扩展（CoreStats / CloseSpider / LogStats / MemoryUsage）
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
