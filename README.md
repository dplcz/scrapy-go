# 🕷️ scrapy-go

[![Go Version](https://img.shields.io/badge/Go-1.25.1+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Version](https://img.shields.io/badge/version-v1.1.2-blue)](docs/README.md#-更新日志)

**scrapy-go** 是一个用 Go 语言实现的高性能异步爬虫框架，架构设计对齐 Python [Scrapy](https://scrapy.org/)，在保留 Scrapy 核心设计理念的同时，充分利用 Go 的并发模型和类型安全特性，提供更高的运行效率和更低的资源消耗。

## ✨ 核心特性

- 🔗 **Scrapy 兼容架构** — Engine → Scheduler → Downloader → Scraper 经典数据流，零学习成本迁移
- ⚡ **Go 原生并发** — 基于 goroutine 和 channel 实现真正的多核并行，无 GIL 限制
- 🌐 **HTTP/2 优化** — 内置 HTTP/2 专用下载器，支持多路复用、自动协商、透明降级
- 🔒 **类型安全** — 编译期类型检查，避免运行时错误
- 🔍 **内置 HTML 解析** — 集成 goquery（CSS）和 htmlquery（XPath），提供链式选择器 API
- 🧩 **可扩展中间件** — 下载器中间件 + Spider 中间件，灵活定制处理流程
- 📤 **Feed Export** — JSON / JSON Lines / CSV / XML 多格式数据导出
- 🕷️ **CrawlSpider** — 基于规则的自动链接提取和跟踪
- 💾 **断点续爬** — 磁盘队列 + 持久化去重，中断后自动恢复
- 🌐 **Redis 分布式队列** — 可插拔 Redis 扩展，支持多实例分布式爬取 + 本地布隆过滤器加速 + Pipeline 批量去重优化（`contrib/redisqueue`）
- 🎛️ **AutoThrottle** — 基于延迟反馈的自适应速率调整，自动优化下载延迟
- 🛡️ **高级重试策略** — 指数退避 + 随机抖动 + 域名级熔断器 + 按状态码差异化重试
- 📊 **可观测性** — OpenTelemetry 分布式追踪 + Prometheus 指标收集，信号驱动自动采集（`contrib/telemetry`）
- 💾 **持久化存储** — MongoDB / PostgreSQL / Elasticsearch 批量写入 + Upsert，可插拔存储适配器（`contrib/storage`）
- 🌐 **Web 管理 API** — 轻量级 REST API，支持 Spider 注册/启动/停止/统计查询，零外部 Web 框架依赖（`contrib/web`）
- 🛡️ **生产就绪** — Panic Recovery、优雅关闭、统计收集、pprof 调试

## 📊 性能数据

| 指标 | 结果 | 说明 |
|------|------|------|
| QPS（16 并发） | ~18,900 req/s | 本地服务器，最小响应 |
| 堆内存（10 万请求） | ~12 MB | 实际数据占用 |
| 每请求分配 | ~9.4 KB | 含 Request/Response/网络缓冲 |
| vs Colly | **~2.5x** QPS | 保留完整中间件栈 |
| vs Geziyor | **~2.0x** QPS | 保留完整中间件栈 |

> 测量条件：本地 benchmark 服务器，GOMAXPROCS=96，scrapy-go 保留所有默认中间件（重试、Cookie、压缩、代理、robots.txt）

## 🚀 快速开始

### 安装

```bash
go get github.com/dplcz/scrapy-go
```

> 📋 **要求**：Go 1.25.1+

### 脚手架工具

```bash
# 安装 CLI 工具
go install github.com/dplcz/scrapy-go/cmd/scrapy-go@latest

# 创建新项目
scrapy-go startproject myproject
cd myproject

# 生成爬虫
scrapy-go genspider quotes quotes.toscrape.com
```

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
    // CSS 选择器提取数据
    title := response.CSS("title::text").Get()
    fmt.Printf("Title: %s\n", title)

    // 跟踪链接
    var results []spider.Output
    links := response.CSS("a::attr(href)").GetAll()
    for _, link := range links {
        req, _ := shttp.NewRequest(response.URLJoin(link),
            shttp.WithCallback(s.Parse),
        )
        results = append(results, spider.Output{Request: req})
    }
    return results, nil
}

func main() {
    c := crawler.NewDefault()
    ctx := context.Background()
    c.Run(ctx, NewMySpider())
}
```

### 使用 Item Pipeline

```go
// 定义 Item
type QuoteItem struct {
    Text   string `item:"text,required"`
    Author string `item:"author,required"`
    Tags   string `item:"tags"`
}

// 定义 Pipeline
type SavePipeline struct {
    pipeline.Base
}

func (p *SavePipeline) ProcessItem(ctx context.Context, item any, sp spider.Spider) (any, error) {
    fmt.Printf("Saving: %+v\n", item)
    return item, nil
}

// 注册 Pipeline
c := crawler.NewDefault()
c.AddPipeline(&SavePipeline{}, 300)
c.Run(ctx, NewMySpider())
```

### Spider 级别配置

```go
func (s *MySpider) CustomSettings() *spider.Settings {
    return &spider.Settings{
        ConcurrentRequests:          spider.IntPtr(8),
        ConcurrentRequestsPerDomain: spider.IntPtr(4),
        DownloadDelay:               spider.DurationPtr(500 * time.Millisecond),
        RandomizeDownloadDelay:      spider.BoolPtr(true),
        UserAgent:                   spider.StringPtr("MyBot/1.0"),
    }
}
```

## 📖 示例

项目提供了完整示例，均使用本地 `httptest` 服务器，**无需外部网络**即可运行：

```bash
go run examples/quotes/main.go           # 多页爬取 + CSS/XPath 解析
go run examples/books_json/main.go       # JSON API + Pipeline 数据处理
go run examples/custom_middleware/main.go # 认证/日志/缓存中间件
go run examples/feedexport/main.go       # Feed Export 数据导出
go run examples/itemadapter/main.go      # ItemAdapter 统一访问
```

## ⚙️ 配置

scrapy-go 支持四种配置方式（按优先级从低到高）：

1. **框架默认配置** — 开箱即用
2. **TOML 配置文件** — `scrapy-go.toml`
3. **全局 Settings** — `settings.New()` + `s.Set(key, value, priority)`
4. **Spider 级别** — `CustomSettings()` 返回类型安全结构体

常用配置项：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `CONCURRENT_REQUESTS` | 16 | 全局最大并发请求数 |
| `CONCURRENT_REQUESTS_PER_DOMAIN` | 8 | 每域名最大并发数 |
| `DOWNLOAD_DELAY` | 0 | 请求间隔 |
| `RETRY_TIMES` | 2 | 最大重试次数 |
| `RETRY_BACKOFF_ENABLED` | false | 启用指数退避重试 |
| `CIRCUIT_BREAKER_ENABLED` | false | 启用域名级熔断器 |
| `ROBOTSTXT_OBEY` | false | 是否遵守 robots.txt |
| `HTTP2_ENABLED` | false | 启用 HTTP/2 优化下载器 |
| `LOG_LEVEL` | "DEBUG" | 日志级别 |
| `AUTOTHROTTLE_ENABLED` | false | 启用自适应限速 |
| `AUTOTHROTTLE_TARGET_CONCURRENCY` | 1.0 | 目标并发数 |

> 完整配置参考请查看 [详细文档](docs/README.md#-配置说明)

## 🏗️ 架构概览

```
Engine (调度引擎)
├── Scheduler (请求调度 + 去重 + 优先级队列)
│   └── [可选] contrib/redisqueue (Redis 分布式队列 + 去重)
├── Downloader (HTTP 下载 + Slot 并发控制)
│   ├── HTTPDownloadHandler (HTTP/1.1 标准处理器)
│   ├── HTTP2DownloadHandler (HTTP/2 优化处理器)
│   ├── ProgressHTTPDownloadHandler (进度回调处理器)
│   └── Middleware Chain (11 个内置中间件)
└── Scraper (响应处理 + Spider 回调)
    ├── Spider Middleware (5 个内置中间件)
    └── Item Pipeline (数据清洗/验证/持久化)
        └── [可选] contrib/storage (MongoDB/PostgreSQL/Elasticsearch 持久化)
```

## ⚠️ 注意事项

- **Go 版本要求** — 需要 Go 1.25.1+
- **回调函数注册** — 使用 `CallbackRegistry` 注册回调函数，支持断点续爬时的回调恢复
- **对象池** — 高并发场景建议使用 `pkg/pool` 包的对象池减少 GC 压力
- **优雅关闭** — 第一次 Ctrl+C 优雅关闭（等待进行中的请求完成），第二次强制退出
- **中间件顺序** — ProcessRequest 按优先级正序执行，ProcessResponse 按优先级逆序执行

## 📚 文档

| 文档 | 说明 |
|------|------|
| [📖 完整参考文档](docs/README.md) | 所有功能特性、配置项、更新日志的完整说明 |
| [🚀 快速入门指南](docs/guide/getting-started.md) | 从零开始的详细教程 |
| [🏗️ 架构设计](docs/architecture/architecture.md) | 核心组件内部结构与数据流 |
| [🔄 从 Python Scrapy 迁移](docs/migration/migration-from-python.md) | 概念映射 + 代码对比 + 迁移检查清单 |
| [🌐 Redis 分布式队列](contrib/redisqueue/README.md) | Redis 队列扩展安装、配置与分布式爬取 |
| [📊 可观测性扩展](contrib/telemetry/README.md) | OpenTelemetry 追踪 + Prometheus 指标 |
| [💾 持久化存储适配器](contrib/storage/README.md) | MongoDB / PostgreSQL / Elasticsearch 批量写入 |
| [🌐 Web 管理 API](contrib/web/README.md) | REST API 管理 Spider 启动/停止/统计查询 |

## 📄 License

MIT
