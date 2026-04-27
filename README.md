# 🕷️ scrapy-go

**scrapy-go** 是一个用 Go 语言实现的高性能异步爬虫框架，架构设计对齐 Python [Scrapy](https://scrapy.org/)，在保留 Scrapy 核心设计理念的同时，充分利用 Go 的并发模型和类型安全特性，提供更高的运行效率和更低的资源消耗。

> 📌 当前版本：**v0.2.3** &nbsp;|&nbsp; 📋 [更新日志](#-更新日志)

---

## 📑 目录

- [🎯 项目概述](#-项目概述)
- [✨ 功能特性](#-功能特性)
- [🚀 快速开始](#-快速开始)
- [📖 使用示例](#-使用示例)
- [⚙️ 配置说明](#️-配置说明)
- [🏗️ 架构设计](#️-架构设计)
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

### 🏗️ 核心引擎

完整实现 Scrapy 经典五大组件：

- **Engine** — 核心调度引擎，协调所有组件，支持暂停/恢复
- **Scheduler** — 基于内存优先级队列的请求调度
- **Downloader** — 基于 Slot 机制的 HTTP 下载，按域名分组控制并发和延迟
- **Scraper** — 调用 Spider 回调并分发结果（Request/Item）
- **Crawler** — 顶层编排器，一行代码组装并启动爬虫

### 📡 请求与响应

- **Request** — 支持多种 HTTP 方法、自定义 Headers、Cookies、Meta 元数据、优先级、Callback/Errback
- **Response** — 支持 Text/JSON 解析、URLJoin 相对路径解析、Follow 链接跟踪、CSS/XPath 选择器
- **Functional Options** — 类型安全的构建模式

### 🔁 去重与调度

- **RFPDupeFilter** — 基于请求指纹（URL + Method + Body SHA1）去重
- **DontFilter** — 支持跳过去重（如初始请求）
- **NoDupeFilter** — 可选的无过滤模式
- **优先级队列** — 高优先级请求优先处理

### ⏱️ 并发与延迟控制

- 全局并发限制（`CONCURRENT_REQUESTS`）
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
| HttpCompression | 590 | gzip/deflate 解压 |
| Redirect | 600 | HTTP 重定向处理 |
| Cookies | 700 | 多会话 Cookie 管理 |

通过实现 `DownloaderMiddleware` 接口或嵌入 `BaseDownloaderMiddleware` 自定义扩展。

### 🔍 HTML 解析（Selector）

- 内置 CSS 和 XPath 选择器（基于 goquery/htmlquery）
- 支持 `::text` 伪元素和属性提取
- 链式调用和批量操作（`GetAll()` / `Get()` / `First()`）
- Response 快捷方法：`CSS()` / `CSSAttr()` / `XPath()` / `Selector()`

### 🕸️ Spider 中间件 & 📦 Item Pipeline

- **Spider 中间件** — 拦截 Spider 输入（响应）和输出（Request/Item），支持自定义扩展
- **Item Pipeline** — 按优先级顺序处理 Item，支持数据清洗、验证、持久化
- **DropItem** — 丢弃无效 Item，中断后续处理链

### 📡 信号系统

- 18 种内置信号，覆盖引擎生命周期、Spider 状态、请求/响应/Item 事件
- 组件间通过信号松耦合通信，支持自定义信号处理器

### ⚙️ 配置系统

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

此外，`examples/template/` 目录提供了对齐 Scrapy CLI 模板的 Go 代码模板，可直接复制到项目中使用：

| 模板 | 对齐 Scrapy | 路径 | 说明 |
|------|-------------|------|------|
| 📋 **settings** | `settings.py.tmpl` | `examples/template/project/settings.go` | 项目配置模板，包含所有常用配置项及注释 |
| 📦 **pipelines** | `pipelines.py.tmpl` | `examples/template/project/pipelines.go` | Item Pipeline 接口实现模板 |
| 🔌 **middlewares** | `middlewares.py.tmpl` | `examples/template/project/middlewares.go` | 下载器中间件 + Spider 中间件实现模板 |
| 🕷️ **basic** | `spiders/basic.tmpl` | `examples/template/spiders/basic/main.go` | 基础爬虫模板，可直接运行 |

---

## ⚙️ 配置说明

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

</details>

<details>
<summary>📊 <b>日志与统计</b></summary>

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `LOG_LEVEL` | string | "DEBUG" | 日志级别：DEBUG/INFO/WARN/ERROR |
| `STATS_DUMP` | bool | true | Spider 关闭时是否输出统计信息 |
| `SCHEDULER_DEBUG` | bool | false | 是否输出调度器调试日志 |

> 🎨 **日志颜色**：终端自动启用彩色输出，非终端时自动禁用
> - 🔵 **DEBUG** (`DBG`): 青色
> - 🟢 **INFO** (`INF`): 绿色
> - 🟡 **WARN** (`WRN`): 粗体黄色
> - 🔴 **ERROR** (`ERR`): 粗体红色

</details>

---

## 🏗️ 架构设计

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
8. ⚙️ **Item Pipeline** 按优先级顺序处理 Item（清洗 → 验证 → 持久化）

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
| **DL Middleware** | `pkg/downloader/middleware` | 下载器中间件接口与实现（8 个内置中间件） |
| **DL MW Manager** | `pkg/downloader` | 下载器中间件管理器（编排中间件链 + 调用下载函数） |
| **Spider Middleware** | `pkg/spider/middleware` | Spider 中间件（输入/输出拦截） |
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
| ⚙️ **Spider 配置** | `custom_settings` 返回 dict | `CustomSettings()` 返回类型安全结构体 |
| 🏗️ **Request 构建** | 关键字参数 | Functional Options 模式 |
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
| 🌐 HTTP 代理 | 代理中间件未实现 |
| 🤖 Robots.txt | RobotsTxt 中间件未实现 |
| 📏 深度控制 | DepthMiddleware 未实现 |
| 📤 Feed Export | 不支持内置数据导出，需自定义 Pipeline |
| 📈 内存监控 | 内存监控扩展未实现 |
| 🛑 关闭条件 | CloseSpider 扩展未实现 |
| 🧩 Extensions | 扩展系统已预留，无内置实现 |
| 🗜️ Brotli | HttpCompression 暂不支持 brotli |
| 🐚 Scrapy Shell | 不支持交互式调试 |
| 🔀 CrawlProcess | 不支持多 Spider 并行运行 |
| 🌍 分布式爬取 | 不支持分布式调度 |

### ⚠️ 已知约束

- **Go 版本要求** — 需要 Go 1.25.1+
- **回调函数类型** — `Callback`/`Errback` 使用 `any` 类型，需运行时类型断言
- **内置 Spider 中间件** — 当前无内置 Spider 中间件，需用户自行实现

---

## 📁 项目结构

```
scrapy-go/
├── 📂 examples/                    # 示例代码
│   ├── quotes/                     # 基础爬取示例
│   ├── books_json/                 # Pipeline + JSON API 示例
│   ├── custom_middleware/          # 自定义中间件示例
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
│   │   └── middleware/             # 下载器中间件接口与实现（8 个内置）
│   ├── scraper/                    # Scraper 响应处理器
│   ├── spider/                     # Spider 接口 + 配置
│   │   └── middleware/             # Spider 中间件
│   ├── pipeline/                   # Item Pipeline
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

### v0.2.3

- 🔧 **NewRequestError 处理修复** — 在中间件管理器的 `processResponse` 和 `processException` 中添加 `NewRequestError` 的显式检查，确保重试/重定向产生的新请求能正确传播给 Engine 重新调度
- 🏗️ **MiddlewareManager 重构** — 将下载器中间件管理器从 `pkg/downloader/middleware/` 移到 `pkg/downloader/` 包下
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
