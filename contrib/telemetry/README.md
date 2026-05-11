# contrib/telemetry — scrapy-go 可观测性扩展

[![Go Version](https://img.shields.io/badge/Go-1.25.1+-00ADD8?style=flat&logo=go)](https://go.dev/)

`contrib/telemetry` 是 scrapy-go 框架的可观测性具体实现模块，作为独立的 Go 子模块发布。

本模块将 [OpenTelemetry](https://opentelemetry.io/) 和 [Prometheus](https://prometheus.io/) 适配为 scrapy-go 的 `pkg/telemetry` 接口，使框架能够导出分布式追踪和指标数据。

## 核心组件

| 组件 | 说明 | 实现接口 |
|------|------|---------|
| `otel.Tracer` | OpenTelemetry Tracer 适配器 | `telemetry.Tracer` |
| `prometheus.Registry` | Prometheus MetricsRegistry 适配器 | `telemetry.MetricsRegistry` |
| `TraceExtension` | 信号驱动的分布式追踪扩展 | `extension.Extension` |
| `MetricsExtension` | 信号驱动的指标收集扩展（含 HTTP `/metrics` 端点） | `extension.Extension` |

## 安装

```bash
go get github.com/dplcz/scrapy-go/contrib/telemetry
```

> 主模块 `go.mod` 不引入 OpenTelemetry / Prometheus 依赖，实现零侵入可插拔设计。

## 快速开始

### 启用 Prometheus 指标

```go
package main

import (
    "context"

    "github.com/dplcz/scrapy-go/contrib/telemetry"
    "github.com/dplcz/scrapy-go/contrib/telemetry/prometheus"
    "github.com/dplcz/scrapy-go/pkg/crawler"
)

func main() {
    c := crawler.NewDefault()

    // 创建 Prometheus 指标注册中心
    registry := prometheus.NewRegistry()

    // 创建指标扩展（含 HTTP /metrics 端点）
    metricsExt := telemetry.NewMetricsExtension(
        registry,
        ":9090",        // HTTP 端点监听地址
        c.Signals,
        c.Logger,
    )
    c.AddExtension(metricsExt, "MetricsExtension", 101)

    // 运行爬虫后访问 http://localhost:9090/metrics 查看指标
    c.Run(context.Background(), NewMySpider())
}
```

### 启用 OpenTelemetry 追踪

```go
package main

import (
    "context"

    "github.com/dplcz/scrapy-go/contrib/telemetry"
    "github.com/dplcz/scrapy-go/contrib/telemetry/otel"
    "github.com/dplcz/scrapy-go/pkg/crawler"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

func main() {
    ctx := context.Background()

    // 初始化 OTel TracerProvider（导出到 OTLP 后端）
    exporter, _ := otlptracehttp.New(ctx)
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
    )
    defer tp.Shutdown(ctx)

    // 创建 OTel Tracer 适配器
    tracer := otel.NewTracer(tp)

    c := crawler.NewDefault()

    // 创建追踪扩展
    traceExt := telemetry.NewTraceExtension(tracer, c.Signals, c.Logger)
    c.AddExtension(traceExt, "TraceExtension", 100)

    c.Run(ctx, NewMySpider())
}
```

### 同时启用追踪和指标

```go
// 创建适配器
tracer := otel.NewTracer(tp)
registry := prometheus.NewRegistry()

// 注入扩展
c.AddExtension(
    telemetry.NewTraceExtension(tracer, c.Signals, c.Logger),
    "TraceExtension", 100,
)
c.AddExtension(
    telemetry.NewMetricsExtension(registry, ":9090", c.Signals, c.Logger),
    "MetricsExtension", 101,
)
```

## 采集的指标

### Prometheus 指标

| 指标名称 | 类型 | 说明 |
|---------|------|------|
| `scrapy_requests_total` | Counter | 总请求数 |
| `scrapy_responses_total` | Counter | 总响应数 |
| `scrapy_items_scraped_total` | Counter | 已抓取 Item 总数 |
| `scrapy_items_dropped_total` | Counter | 已丢弃 Item 总数 |
| `scrapy_errors_total` | Counter | 总错误数 |
| `scrapy_active_requests` | Gauge | 当前活跃请求数 |
| `scrapy_spider_state` | Gauge | Spider 状态（0=关闭, 1=运行中） |
| `scrapy_request_duration_seconds` | Histogram | 请求延迟分布 |
| `scrapy_spider_elapsed_seconds` | Gauge | Spider 运行时长（秒） |

### 追踪 Span

| Span 名称 | Kind | 触发信号 | 说明 |
|-----------|------|---------|------|
| `spider.crawl` | Internal | SpiderOpened/Closed | Spider 生命周期根 Span |
| `http.request` | Client | RequestReachedDownloader | HTTP 请求子 Span |

## HTTP 端点

MetricsExtension 内置 HTTP 服务器，提供以下端点：

| 端点 | 说明 |
|------|------|
| `/metrics` | Prometheus 格式指标输出 |
| `/health` | 健康检查（返回 `ok`） |

## 设计决策

- **适配器模式** — 将 OTel/Prometheus 的具体实现适配为 `pkg/telemetry` 定义的轻量级接口
- **信号驱动** — 通过框架信号系统自动采集数据，无需修改业务代码
- **独立子模块** — 避免主模块引入重量级依赖，用户按需安装
- **零侵入** — 未启用时使用 `NoopTracer`/`NoopMetricsRegistry`，零运行时开销
- **线程安全** — 所有组件保证并发安全，支持多 goroutine 同时访问

## 测试

```bash
# 运行所有测试（含竞态检测）
cd contrib/telemetry
go test ./... -race -v

# 查看覆盖率
go test ./... -coverprofile=cover.out
go tool cover -func=cover.out
```

测试覆盖率：
- `otel` 包：100%
- `prometheus` 包：94.6%
- `extension` 包：89.2%
- **总体：92.2%**
