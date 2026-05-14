# 🕷️ scrapy-go

[![Go Version](https://img.shields.io/badge/Go-1.25.1+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Version](https://img.shields.io/badge/version-v1.1.5-blue)](docs/README.md#-更新日志)

**scrapy-go** 是一个用 Go 语言实现的高性能异步爬虫框架，架构设计对齐 Python [Scrapy](https://scrapy.org/)，在保留 Scrapy 核心设计理念的同时，充分利用 Go 的并发模型和类型安全特性，提供更高的运行效率和更低的资源消耗。

## ✨ 核心特性

- 🔗 **Scrapy 兼容架构** — Engine → Scheduler → Downloader → Scraper 经典数据流，零成本迁移
- ⚡ **高性能并发** — goroutine + channel 原生并发，HTTP/2 多路复用，~18,900 req/s
- 🧩 **可扩展中间件** — 11 个下载器中间件 + 5 个 Spider 中间件，支持 AutoThrottle、高级重试等
- 🔍 **内置选择器** — goquery（CSS）+ htmlquery（XPath）链式 API，CrawlSpider 规则化爬取
- 💾 **断点续爬** — 磁盘队列 + 持久化去重 + 内存溢出保护，中断后自动恢复
- 📤 **数据导出** — Feed Export（JSON/CSV/XML）+ MongoDB/PostgreSQL/ES 批量写入
- 🌐 **分布式支持** — Redis 队列 + 布隆过滤器 + 滑动窗口限速 + Web 管理 API
- 📊 **可观测性** — OpenTelemetry 追踪 + Prometheus 指标
- 🛡️ **类型安全配置** — 泛型 `Key[T]` + `Get[T]` API，编译期类型检查，消除魔法字符串

## 📊 性能数据

| 指标 | 结果 | 说明 |
|------|------|------|
| QPS（16 并发） | ~18,900 req/s | 本地服务器，最小响应 |
| 堆内存（10 万请求） | ~12 MB | 实际数据占用 |
| 每请求分配 | ~9.4 KB | 含 Request/Response/网络缓冲 |
| vs Colly | **~2.5x** QPS | 保留完整中间件栈 |
| vs Geziyor | **~2.0x** QPS | 保留完整中间件栈 |

## 🚀 快速开始

### 安装

```bash
go get github.com/dplcz/scrapy-go
```

> 📋 **要求**：Go 1.25.1+

### 脚手架工具

```bash
go install github.com/dplcz/scrapy-go/cmd/scrapy-go@latest

scrapy-go startproject myproject && cd myproject
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
    title := response.CSS("title::text").Get()
    fmt.Printf("Title: %s\n", title)
    return nil, nil
}

func main() {
    c := crawler.NewDefault()
    c.Run(context.Background(), NewMySpider())
}
```

> 📖 更多示例（Pipeline、中间件、Feed Export 等）请查看 [完整参考文档](docs/README.md#-使用示例)

## 🏗️ 架构概览

```
Engine (调度引擎)
├── Scheduler (请求调度 + 去重 + 优先级队列)
│   └── [可选] contrib/redisqueue (Redis 分布式队列)
├── Downloader (HTTP 下载 + Slot 并发控制)
│   ├── HTTP/1.1 & HTTP/2 下载处理器
│   └── Middleware Chain (11 个内置中间件)
└── Scraper (响应处理 + Spider 回调)
    ├── Spider Middleware (5 个内置中间件)
    └── Item Pipeline → [可选] contrib/storage (持久化)
```

## ⚠️ 注意事项

- **Go 版本要求** — 需要 Go 1.25.1+
- **回调函数类型安全** — `CallbackFunc`/`ErrbackFunc` 为具体函数类型（非 `any`），编译期即可捕获签名错误；使用 `CallbackRegistry` 注册回调函数支持断点续爬时的回调恢复
- **对象池** — 高并发场景建议使用 `pkg/pool` 包的对象池减少 GC 压力
- **优雅关闭** — 第一次 Ctrl+C 优雅关闭（等待进行中的请求完成），第二次强制退出
- **中间件顺序** — ProcessRequest 按优先级正序执行，ProcessResponse 按优先级逆序执行

## 📚 文档

| 文档 | 说明 |
|------|------|
| [📖 完整参考文档](docs/README.md) | 功能特性、配置项、更新日志的完整说明 |
| [🚀 快速入门指南](docs/guide/getting-started.md) | 从零开始的详细教程 |
| [🏗️ 架构设计](docs/architecture/architecture.md) | 核心组件内部结构与数据流 |
| [🔄 从 Python Scrapy 迁移](docs/migration/migration-from-python.md) | 概念映射 + 代码对比 + 迁移检查清单 |

## 📄 License

MIT
