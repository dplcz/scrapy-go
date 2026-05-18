# 🌐 scrapy-go/contrib/web — 轻量级 REST API 管理模块

[![Go Version](https://img.shields.io/badge/Go-1.25.1+-00ADD8?style=flat&logo=go)](https://go.dev/)

`contrib/web` 为 scrapy-go 框架提供轻量级 REST API 管理能力，支持通过 HTTP 接口注册、启动、停止和监控 Spider。

> **Phase 1**（当前版本）提供 REST API；**Phase 2**（v1.3.0）将增加 WebSocket 实时事件推送和 Dashboard UI。

## ✨ 特性

- 🔌 **零外部依赖** — 基于 Go 标准库 `net/http` 实现，不引入第三方 Web 框架
- 🕷️ **Spider 注册表** — 按名称注册工厂函数，支持动态创建 Spider 实例
- 🚀 **多实例并发** — 同名 Spider 可启动多个实例，每个实例独立运行
- 📊 **统计查询** — 实时获取运行中 Spider 的统计数据
- 🎯 **启动项参数** — 启动 Spider 时可传入自定义 JSON 参数，以最高优先级注入 Crawler Settings
- 🔄 **自动清理** — Spider 完成后自动从运行列表中移除
- 🛡️ **独立子模块** — 不引入 `contrib/web` 时，主模块编译产物不包含 Web 相关代码

## 📦 安装

```bash
go get github.com/dplcz/scrapy-go/contrib/web
```

## 🚀 快速开始

```go
package main

import (
    "context"
    "log"

    "github.com/dplcz/scrapy-go/contrib/web"
    shttp "github.com/dplcz/scrapy-go/pkg/http"
    "github.com/dplcz/scrapy-go/pkg/spider"
)

// 定义 Spider
type QuotesSpider struct {
    spider.Base
}

func NewQuotesSpider() spider.Spider {
    return &QuotesSpider{
        Base: spider.Base{
            SpiderName: "quotes",
            StartURLs:  []string{"https://quotes.toscrape.com"},
        },
    }
}

func (s *QuotesSpider) Parse(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
    // 爬取逻辑...
    return nil, nil
}

func main() {
    // 创建 Web 管理服务器
    srv := web.NewServer(":8080")

    // 注册 Spider 工厂函数
    srv.Register("quotes", NewQuotesSpider)

    // 启动服务器（阻塞）
    ctx := context.Background()
    if err := srv.ListenAndServe(ctx); err != nil {
        log.Fatal(err)
    }
}
```

## 📡 REST API

### GET /api/spiders

获取已注册的 Spider 列表及运行状态。

**响应示例：**

```json
{
  "code": 200,
  "message": "ok",
  "data": [
    {"name": "quotes", "running_instances": 1},
    {"name": "books", "running_instances": 0}
  ]
}
```

### POST /api/spiders/:name/start

按名称启动一个 Spider。每次调用创建新的 Crawler + Spider 实例。

**请求体（可选，JSON）：**

```json
{
  "args": {
    "CONCURRENT_REQUESTS": 4,
    "DOWNLOAD_DELAY": "1s",
    "target_category": "electronics",
    "max_pages": 10
  }
}
```

`args` 中的参数会以最高优先级（`PriorityCmdline`）注入到 Crawler 的 Settings 中，覆盖所有其他级别的同名配置（包括 Spider.CustomSettings）。可用于传递框架配置覆盖或自定义业务参数。

**响应示例：**

```json
{
  "code": 200,
  "message": "spider started",
  "data": {
    "id": "quotes-1",
    "name": "quotes",
    "start_time": "2026-05-13T12:00:00Z",
    "args": {"CONCURRENT_REQUESTS": 4, "target_category": "electronics"}
  }
}
```

### POST /api/spiders/:name/stop

按名称停止正在运行的 Spider。

**查询参数：**
- `id`（可选）— 指定停止某个运行实例，不指定则停止该名称的所有实例

**响应示例：**

```json
{
  "code": 200,
  "message": "stopped 1 instance of spider \"quotes\""
}
```

### GET /api/spiders/:name/stats

获取指定 Spider 的运行统计数据。

**响应示例：**

```json
{
  "code": 200,
  "message": "ok",
  "data": [
    {
      "id": "quotes-1",
      "name": "quotes",
      "start_time": "2026-05-13T12:00:00Z",
      "running": true,
      "args": {"CONCURRENT_REQUESTS": 4, "target_category": "electronics"},
      "stats": {
        "item_scraped_count": 42,
        "request_count": 100,
        "elapsed_time_seconds": 12.5
      }
    }
  ]
}
```

> `args` 字段仅在启动时传入了参数时才会出现。

### GET /api/health

健康检查端点。

**响应示例：**

```json
{
  "code": 200,
  "message": "ok",
  "data": {
    "status": "healthy",
    "registered_spiders": 2,
    "running_spiders": 1
  }
}
```

## ⚙️ 配置选项

```go
// 自定义日志记录器
srv := web.NewServer(":8080", web.WithLogger(myLogger))

// 自定义 Runner（共享已有的 Runner 实例）
srv := web.NewServer(":8080", web.WithRunner(myRunner))

// 自定义 Registry（预注册 Spider）
registry := web.NewRegistry()
registry.Register("quotes", NewQuotesSpider)
srv := web.NewServer(":8080", web.WithRegistry(registry))
```

## 🔧 高级用法

### 为 Spider 配置 Pipeline

通过 `CrawlerConfigurator` 回调为每次启动的 Crawler 注册 Pipeline：

```go
srv.Register("quotes", NewQuotesSpider, func(c web.CrawlerConfig) {
    c.AddPipeline(&SavePipeline{}, "save", 300)
})
```

### 与 Prometheus 指标集成

Web 管理 API 可与 `contrib/telemetry` 的 Prometheus 指标端点共存：

```go
srv := web.NewServer(":8080")
// Web API 在 /api/* 路径
// Prometheus 指标在 /metrics 路径（通过 telemetry 扩展独立配置）
```

## 🗺️ 路线图

| 版本 | 功能 | 状态 |
|------|------|------|
| v1.1.7 | Phase 1：REST API（当前） | ✅ |
| v1.3.0 | Phase 2：WebSocket 实时事件推送 | 📋 计划中 |
| v1.3.0 | Phase 2：前端 Dashboard UI | 📋 计划中 |
| v1.3.0 | Phase 2：爬取历史持久化 | 📋 计划中 |

## 📄 License

MIT — 与 scrapy-go 主项目一致。
