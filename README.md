
# scrapy-go

**scrapy-go** 是一个用 Go 语言实现的高性能异步爬虫框架，架构设计对齐 Python [Scrapy](https://scrapy.org/)，在保留 Scrapy 核心设计理念的同时，充分利用 Go 的并发模型和类型安全特性，提供更高的运行效率和更低的资源消耗。

> 当前版本：**v0.2.0**

## 目录

- [项目概述](#项目概述)
- [功能特性](#功能特性)
- [快速开始](#快速开始)
- [安装](#安装)
- [使用示例](#使用示例)
- [配置说明](#配置说明)
- [架构设计](#架构设计)
- [与 Scrapy 的对比](#与-scrapy-的对比)
- [当前版本限制](#当前版本限制)

## 项目概述

scrapy-go 的目标是为 Go 开发者提供一个**生产级的爬虫框架**，具备以下核心价值：

- **Scrapy 兼容的架构设计**：Engine → Scheduler → Downloader → Scraper 的经典数据流，Scrapy 用户可以零学习成本迁移
- **Go 原生并发模型**：基于 goroutine 和 channel 实现真正的并行下载，无 GIL 限制
- **类型安全**：编译期类型检查，避免运行时错误
- **内置 HTML 解析**：集成 goquery（CSS 选择器）和 htmlquery（XPath），提供类 Scrapy 的链式选择器 API
- **可扩展的中间件体系**：支持下载器中间件和 Spider 中间件，可灵活定制请求/响应处理流程

## 功能特性

### 核心引擎

- ✅ **Engine 调度引擎** — 协调所有组件的核心调度循环，支持暂停/恢复
- ✅ **Scheduler 调度器** — 基于内存优先级队列的请求调度，高优先级请求优先处理
- ✅ **Downloader 下载器** — 基于 Slot 机制的 HTTP 下载，按域名/自定义分组控制并发和延迟
- ✅ **Scraper 处理器** — 调用 Spider 回调并分发结果（Request/Item），支持回退机制
- ✅ **Crawler 编排器** — 顶层 API，一行代码组装所有组件并启动爬虫

### 请求与响应

- ✅ **Request** — 支持 GET/POST 等方法、自定义 Headers、Cookies、Meta 元数据、优先级、回调/错误回调
- ✅ **Response** — 支持 Text/JSON 解析、URLJoin 相对路径解析、Follow 链接跟踪、CSS/XPath 选择器
- ✅ **Functional Options 模式** — 类型安全的请求/响应构建方式

### 去重与过滤

- ✅ **RFPDupeFilter** — 基于请求指纹（URL + Method + Body 的 SHA1）的去重过滤
- ✅ **DontFilter 机制** — 支持跳过去重（如初始请求）
- ✅ **NoDupeFilter** — 可选的无过滤模式

### 并发与延迟控制

- ✅ **全局并发控制** — `CONCURRENT_REQUESTS` 限制总并发数
- ✅ **域名级并发控制** — `CONCURRENT_REQUESTS_PER_DOMAIN` 限制每个域名的并发数
- ✅ **下载延迟** — `DOWNLOAD_DELAY` 控制同一 Slot 内请求间隔
- ✅ **随机化延迟** — `RANDOMIZE_DOWNLOAD_DELAY` 在 [0.5×delay, 1.5×delay) 范围内随机化
- ✅ **download_slot 分组** — 通过 Request Meta 自定义 Slot 分组，覆盖默认的域名分组

### 下载器中间件

- ✅ **中间件管理器** — 支持优先级排序、正序/逆序执行链
- ✅ **DownloadTimeout** — 基于 `context.WithTimeout` 的请求超时控制，支持全局和请求级覆盖（优先级 300）
- ✅ **DefaultHeaders** — 自动注入默认请求头（优先级 400）
- ✅ **HttpAuth** — Basic Auth 认证注入，支持域名限制和请求级 Meta 覆盖（优先级 410）
- ✅ **UserAgent** — 自动设置 User-Agent（优先级 500）
- ✅ **Retry** — 自动重试失败请求，可配置重试次数和 HTTP 状态码（优先级 550）
- ✅ **HttpCompression** — 自动添加 `Accept-Encoding` 头，支持 gzip/deflate 响应体解压（优先级 590）
- ✅ **Redirect** — 自动处理 HTTP 重定向，可配置最大重定向次数（优先级 600）
- ✅ **Cookies** — 基于 `net/http/cookiejar` 的多会话 Cookie 管理，支持 `cookiejar` Meta 隔离（优先级 700）
- ✅ **自定义中间件** — 通过实现 `DownloaderMiddleware` 接口或嵌入 `BaseDownloaderMiddleware` 快速扩展

### HTML 解析（Selector）

- ✅ **Selector 包** (`pkg/selector`) — 提供链式调用的 CSS 和 XPath 选择器
- ✅ **CSS 选择器** — 支持标准 CSS 选择器 + `::text` 伪元素提取文本
- ✅ **CSSAttr** — CSS 选择器 + 属性提取（等价于 Scrapy 的 `::attr(name)`）
- ✅ **XPath 选择器** — 支持完整的 XPath 表达式查询
- ✅ **List 批量操作** — `GetAll()`、`Get()`、`First()`、`Attr()`、`AttrAll()`
- ✅ **Response 快捷方法** — `response.CSS()`、`response.CSSAttr()`、`response.XPath()`、`response.Selector()`

### Spider 中间件

- ✅ **中间件管理器** — 拦截 Spider 的输入（响应）和输出（Request/Item）
- ✅ **自定义中间件** — 通过实现 `SpiderMiddleware` 接口或嵌入 `BaseSpiderMiddleware` 快速扩展

### Item Pipeline

- ✅ **Pipeline 管理器** — 按优先级顺序处理 Item，支持数据清洗、验证、持久化
- ✅ **DropItem 机制** — Pipeline 可丢弃无效 Item，中断后续处理
- ✅ **自定义 Pipeline** — 通过实现 `ItemPipeline` 接口扩展

### 信号系统

- ✅ **18 种内置信号** — 覆盖引擎生命周期、Spider 状态、请求/响应/Item 事件
- ✅ **松耦合通信** — 组件间通过信号解耦，支持自定义信号处理器

### 配置系统

- ✅ **六级优先级** — default → command → addon → project → spider → cmdline
- ✅ **Spider 级别配置** — 通过 `CustomSettings()` 返回类型安全的 `Settings` 结构体
- ✅ **配置冻结** — 支持 Freeze 防止运行时意外修改
- ✅ **组件优先级字典** — 支持 `_BASE` + 用户配置合并，负数优先级表示禁用

### 统计与日志

- ✅ **MemoryCollector** — 基于内存的统计收集，Spider 关闭时自动 Dump
- ✅ **结构化日志** — 基于 `slog` 的结构化日志，支持 DEBUG/INFO/WARN/ERROR 级别
- ✅ **OS 信号优雅关闭** — 监听 SIGINT/SIGTERM，优雅停止爬虫

### 外部依赖

- `github.com/PuerkitoBio/goquery` v1.12.0 — CSS 选择器引擎
- `github.com/antchfx/htmlquery` v1.3.6 — XPath 查询引擎
- `golang.org/x/net` v0.53.0 — HTML 解析

## 快速开始

### 安装

```bash
# 在你的 Go 项目中引入
go get scrapy-go
```

> **要求**：Go 1.25.1+

### 最简示例

```go
package main

import (
    "context"
    "fmt"

    "scrapy-go/pkg/crawler"
    scrapy_http "scrapy-go/pkg/http"
    "scrapy-go/pkg/spider"
)

// 定义 Spider
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

// 解析响应
func (s *MySpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
    fmt.Printf("状态码: %d, URL: %s\n", response.Status, response.URL)
    fmt.Printf("响应长度: %d 字节\n", len(response.Body))
    return nil, nil
}

func main() {
    c := crawler.NewDefault()
    ctx := context.Background()
    c.Run(ctx, NewMySpider())
}
```

## 使用示例

项目提供了三个完整的示例，均使用本地 `httptest` 服务器，无需外部网络即可运行：

### 示例 1：基础爬取 + CSS/XPath 解析（quotes）

演示多页爬取、CSS/XPath 选择器解析 HTML 和数据提取：

```bash
go run examples/quotes/main.go
```

### 示例 2：Pipeline 数据处理（books_json）

演示 JSON API 爬取、数据清洗 Pipeline 和 JSON 文件持久化：

```bash
go run examples/books_json/main.go
```

### 示例 3：自定义中间件（custom_middleware）

演示认证中间件、日志中间件、缓存中间件和 Spider 中间件：

```bash
go run examples/custom_middleware/main.go
```

## 配置说明

### 配置方式

scrapy-go 支持三种配置方式（按优先级从低到高）：

#### 1. 框架默认配置

所有配置项都有合理的默认值，开箱即用。

#### 2. 全局配置

通过 `Settings` 对象在创建 Crawler 时设置：

```go
s := settings.New()
s.Set("CONCURRENT_REQUESTS", 32, settings.PriorityProject)
s.Set("DOWNLOAD_DELAY", time.Second, settings.PriorityProject)

c := crawler.New(crawler.WithSettings(s))
```

#### 3. Spider 级别配置

通过 `CustomSettings()` 方法返回类型安全的配置结构体：

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

#### 并发控制

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `CONCURRENT_REQUESTS` | int | 16 | 全局最大并发请求数 |
| `CONCURRENT_REQUESTS_PER_DOMAIN` | int | 8 | 每个域名（Slot）的最大并发数 |
| `CONCURRENT_ITEMS` | int | 100 | 最大并发 Item 处理数 |

#### 下载配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DOWNLOAD_DELAY` | Duration/int | 0 | 同一 Slot 内请求间隔（0 表示无延迟） |
| `RANDOMIZE_DOWNLOAD_DELAY` | bool | true | 是否在 [0.5×delay, 1.5×delay) 范围内随机化延迟 |
| `USER_AGENT` | string | `scrapy-go/0.1.0` | 默认 User-Agent |

#### 超时与认证配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DOWNLOAD_TIMEOUT` | int | 180 | 下载超时（秒），DownloadTimeout 中间件使用 |
| `HTTPAUTH_USER` | string | "" | Basic Auth 用户名 |
| `HTTPAUTH_PASS` | string | "" | Basic Auth 密码 |
| `HTTPAUTH_DOMAIN` | string | "" | 限制认证的域名（空表示所有域名） |

#### Cookies 配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `COOKIES_ENABLED` | bool | true | 是否启用 Cookies 中间件 |
| `COOKIES_DEBUG` | bool | false | 是否输出 Cookies 调试日志 |

#### 压缩配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `COMPRESSION_ENABLED` | bool | true | 是否启用 HttpCompression 中间件 |

#### 重试配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `RETRY_ENABLED` | bool | true | 是否启用自动重试 |
| `RETRY_TIMES` | int | 2 | 最大重试次数（不含首次请求） |
| `RETRY_HTTP_CODES` | []int | [500,502,503,504,522,524,408,429] | 触发重试的 HTTP 状态码 |
| `RETRY_PRIORITY_ADJUST` | int | -1 | 重试请求的优先级调整值 |

#### 重定向配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `REDIRECT_ENABLED` | bool | true | 是否启用自动重定向 |
| `REDIRECT_MAX_TIMES` | int | 20 | 最大重定向次数 |
| `REDIRECT_PRIORITY_ADJUST` | int | 2 | 重定向请求的优先级调整值 |

#### 中间件配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DOWNLOADER_MIDDLEWARES_BASE` | map[string]int | 见下方 | 内置下载器中间件及优先级 |
| `DOWNLOADER_MIDDLEWARES` | map[string]int | {} | 用户自定义中间件优先级覆盖 |
| `SPIDER_MIDDLEWARES_BASE` | map[string]int | {} | 内置 Spider 中间件及优先级 |
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
```

禁用内置中间件的方式：

```go
s := settings.New()
// 方式 1：设置优先级为负数
s.Set("DOWNLOADER_MIDDLEWARES", map[string]int{"Retry": -1}, settings.PriorityProject)
// 方式 2：通过开关配置
s.Set("RETRY_ENABLED", false, settings.PriorityProject)
```

#### 日志与统计

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `LOG_LEVEL` | string | "DEBUG" | 日志级别：DEBUG/INFO/WARN/ERROR |
| `STATS_DUMP` | bool | true | Spider 关闭时是否输出统计信息 |
| `SCHEDULER_DEBUG` | bool | false | 是否输出调度器调试日志 |

## 架构设计

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

### 数据流

1. **Spider** 产出初始请求 → **Engine** 接收
2. **Engine** 将请求送入 **Scheduler**（去重 + 优先级队列）
3. **Engine** 从 **Scheduler** 取出请求 → 经过 **下载器中间件链**（正序 ProcessRequest）
4. **Downloader** 按 Slot 分组执行 HTTP 下载（并发 + 延迟控制）
5. 响应经过 **下载器中间件链**（逆序 ProcessResponse）→ 返回 **Engine**
6. **Engine** 将响应送入 **Scraper** → 经过 **Spider 中间件链** → 调用 **Spider** 回调
7. Spider 回调产出 **Request**（回到步骤 2）或 **Item**（进入 Pipeline）
8. **Item Pipeline** 按优先级顺序处理 Item（清洗 → 验证 → 持久化）

### 核心组件

| 组件 | 包路径 | 职责 |
|------|--------|------|
| **Crawler** | `pkg/crawler` | 顶层编排器，组装所有组件，提供用户 API |
| **Engine** | `pkg/engine` | 核心调度引擎，协调 Scheduler/Downloader/Scraper |
| **Scheduler** | `pkg/scheduler` | 请求调度（优先级队列 + 去重过滤） |
| **Downloader** | `pkg/downloader` | HTTP 下载管理（Slot 并发/延迟控制） |
| **Scraper** | `pkg/scraper` | 响应处理（调用 Spider 回调 + 分发结果） |
| **Spider** | `pkg/spider` | 用户爬虫接口（定义爬取逻辑） |
| **Pipeline** | `pkg/pipeline` | Item 数据处理管道 |
| **DL Middleware** | `pkg/downloader/middleware` | 下载器中间件（请求/响应拦截） |
| **Spider Middleware** | `pkg/spider/middleware` | Spider 中间件（输入/输出拦截） |
| **Settings** | `pkg/settings` | 多优先级配置系统 |
| **Signal** | `pkg/signal` | 事件/信号系统 |
| **Stats** | `pkg/stats` | 统计收集器 |
| **Selector** | `pkg/selector` | CSS/XPath 选择器（对齐 Scrapy Selector） |
| **HTTP** | `pkg/http` | Request/Response 数据模型 |
| **Errors** | `pkg/errors` | 框架错误类型（对齐 Scrapy exceptions） |

### Slot 机制

Downloader 通过 Slot 机制实现精细的并发和延迟控制：

- 每个域名（或自定义 `download_slot`）对应一个独立的 Slot
- Slot 内部通过队列驱动串行出队，用 `lastSeen` 时间戳精确控制请求间隔
- 不同 Slot 之间完全并行，互不阻塞
- 支持通过 Request Meta 自定义 Slot 分组：

```go
req, _ := scrapy_http.NewRequest("https://example.com/api",
    scrapy_http.WithMeta(map[string]any{
        "download_slot": "my-custom-group",
    }),
)
```

## 与 Scrapy 的对比

### 相同点

| 方面 | 说明 |
|------|------|
| **架构模型** | Engine → Scheduler → Downloader → Scraper 的经典数据流完全一致 |
| **Spider 接口** | `Start()` / `Parse()` / `CustomSettings()` / `Closed()` 对应 Scrapy 的 `start_requests` / `parse` / `custom_settings` / `closed` |
| **Request/Response** | 字段设计（URL、Method、Headers、Meta、Priority、Callback、Errback）完全对齐 |
| **中间件体系** | 下载器中间件（ProcessRequest/ProcessResponse/ProcessException）和 Spider 中间件（ProcessSpiderInput/ProcessOutput/ProcessSpiderException）接口一致 |
| **中间件执行顺序** | ProcessRequest 正序、ProcessResponse/ProcessException 逆序，与 Scrapy 完全一致 |
| **Item Pipeline** | Open/Close/ProcessItem 接口对齐，支持 DropItem 中断处理链 |
| **配置系统** | 多优先级覆盖机制、`_BASE` + 用户配置合并、负数优先级禁用组件 |
| **去重过滤** | RFPDupeFilter 基于请求指纹去重，DontFilter 跳过去重 |
| **信号系统** | spider_opened/spider_closed/spider_idle/item_scraped 等信号对齐 |
| **Slot 机制** | 按域名分组的并发/延迟控制，支持 `download_slot` Meta 自定义分组 |
| **错误类型** | DropItem/CloseSpider/DontCloseSpider/IgnoreRequest/NotConfigured 等异常对齐 |

### 区别

| 方面 | Scrapy (Python) | scrapy-go (Go) |
|------|-----------------|----------------|
| **语言** | Python 3，基于 Twisted 异步框架 | Go，基于 goroutine 原生并发 |
| **并发模型** | 单线程事件循环 + 协程（受 GIL 限制） | 多核并行 goroutine，无 GIL 限制 |
| **类型安全** | 动态类型，运行时检查 | 静态类型，编译期检查 |
| **Spider 配置** | `custom_settings` 返回 dict | `CustomSettings()` 返回类型安全的 `Settings` 结构体 |
| **Request 构建** | 关键字参数 `Request(url, callback=self.parse)` | Functional Options `NewRequest(url, WithCallback(s.Parse))` |
| **外部依赖** | Twisted、lxml、cssselect 等 | goquery + htmlquery（CSS/XPath 选择器） |
| **HTML 解析** | 内置 Selector（CSS/XPath） | 内置 Selector 包，基于 goquery/htmlquery，API 对齐 Scrapy |
| **部署** | 需要 Python 运行时 | 编译为单一二进制文件，无运行时依赖 |
| **内存占用** | 较高（Python 对象开销） | 较低（Go 值类型 + 紧凑内存布局） |
| **Spider 定义** | 类继承 `class MySpider(scrapy.Spider)` | 接口实现 + 结构体嵌入 `Base` |

## 当前版本限制

以下功能尚未实现，计划在后续版本中逐步完善：

### 未实现的功能

| 功能 | 说明 |
|------|------|
| **磁盘队列** | Scheduler 仅支持内存队列，不支持磁盘持久化（JOBDIR） |
| **HTTP 缓存** | `HTTPCACHE_ENABLED` 配置已预留，但缓存中间件未实现 |
| **HTTP 代理** | `HTTPPROXY_ENABLED` 配置已预留，但代理中间件未实现 |
| **Robots.txt** | `ROBOTSTXT_OBEY` 配置已预留，但 RobotsTxt 中间件未实现 |
| **深度控制中间件** | `DEPTH_LIMIT` 配置已预留，但 DepthMiddleware 未实现 |
| **Feed Export** | 不支持内置的数据导出（JSON/CSV/XML），需通过自定义 Pipeline 实现 |
| **内存监控** | `MEMUSAGE_ENABLED` 配置已预留，但内存监控扩展未实现 |
| **关闭条件扩展** | `CLOSESPIDER_*` 配置已预留，但 CloseSpider 扩展未实现 |
| **Extensions 扩展系统** | 扩展注册机制已预留，但无内置扩展实现 |
| **Brotli 压缩** | HttpCompression 中间件暂不支持 brotli 解压（需引入外部依赖） |
| **Scrapy Shell** | 不支持交互式调试 Shell |
| **CrawlProcess** | 不支持多 Spider 并行运行 |
| **分布式爬取** | 不支持分布式调度 |

### 已知约束

- **Go 版本要求**：需要 Go 1.25.1+
- **回调函数类型**：由于 Go 不支持循环导入，`Callback`/`Errback` 使用 `any` 类型，需在运行时进行类型断言
- **内置 Spider 中间件**：当前无内置 Spider 中间件（如 DepthMiddleware、HttpErrorMiddleware），需用户自行实现

## 项目结构

```
scrapy-go/
├── examples/                    # 示例代码
│   ├── quotes/                  # 基础爬取示例
│   ├── books_json/              # Pipeline + JSON API 示例
│   └── custom_middleware/       # 自定义中间件示例
├── internal/
│   └── utils/                   # 内部工具（指纹计算、URL 规范化）
├── pkg/
│   ├── crawler/                 # Crawler 编排器
│   ├── engine/                  # Engine 调度引擎
│   ├── scheduler/               # Scheduler 调度器 + 去重过滤器
│   ├── downloader/              # Downloader 下载器 + Slot 机制
│   │   └── middleware/          # 下载器中间件（8 个内置中间件）
│   ├── scraper/                 # Scraper 响应处理器
│   ├── spider/                  # Spider 接口 + 配置
│   │   └── middleware/          # Spider 中间件
│   ├── pipeline/                # Item Pipeline
│   ├── http/                    # Request/Response 数据模型
│   ├── selector/                # CSS/XPath 选择器（对齐 Scrapy Selector）
│   ├── settings/                # 多优先级配置系统
│   ├── signal/                  # 信号/事件系统
│   ├── stats/                   # 统计收集器
│   ├── errors/                  # 框架错误类型
│   └── log/                     # 日志工具
├── tests/
│   └── integration/             # 端到端集成测试
└── go.mod
```

## License

MIT
