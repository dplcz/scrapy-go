# contrib/telemetry — scrapy-go 可观测性扩展

[![Go Version](https://img.shields.io/badge/Go-1.25.1+-00ADD8?style=flat&logo=go)](https://go.dev/)

`contrib/telemetry` 是 scrapy-go 框架的可观测性具体实现模块，作为独立的 Go 子模块发布。

本模块将 [OpenTelemetry](https://opentelemetry.io/) 和 [Prometheus](https://prometheus.io/) 适配为 scrapy-go 的 `pkg/telemetry` 接口，使框架能够导出分布式追踪和指标数据。

## 核心组件

| 组件 | 说明 | 实现接口 |
|------|------|---------|
| `otel.Tracer` | OpenTelemetry Tracer 适配器 | `telemetry.Tracer` |
| `prometheus.Registry` | Prometheus MetricsRegistry 适配器 | `telemetry.MetricsRegistry` |
| `prometheus.LabeledRegistry` | 支持标签维度的 Prometheus 适配器 | `telemetry.LabeledMetricsRegistry` |
| `TraceExtension` | 信号驱动的分布式追踪扩展 | `extension.Extension` |
| `MetricsExtension` | 信号驱动的指标收集扩展（含 HTTP `/metrics` 端点） | `extension.Extension` |
| `grafana/` | 开箱即用的 Grafana Dashboard JSON 模板 | — |

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

### 使用带标签维度的指标（按 Spider/域名分组）

```go
package main

import (
    "github.com/dplcz/scrapy-go/contrib/telemetry/prometheus"
    "github.com/dplcz/scrapy-go/pkg/telemetry"
)

func main() {
    // 创建支持标签维度的 Prometheus 注册中心
    registry := prometheus.NewLabeledRegistry()

    // 创建带标签的计数器（按 Spider 名称和域名分组）
    requestCounter := registry.LabeledCounter(
        "scrapy_requests_total",
        "总请求数",
        "spider", "domain",
    )

    // 按标签值记录指标
    requestCounter.With("my_spider", "example.com").Inc()
    requestCounter.With("my_spider", "test.org").Add(5.0)

    // 带标签的仪表盘
    activeGauge := registry.LabeledGauge(
        "scrapy_active_requests",
        "活跃请求数",
        "spider",
    )
    activeGauge.With("my_spider").Inc()

    // 带标签的直方图
    durationHisto := registry.LabeledHistogram(
        "scrapy_request_duration_seconds",
        "请求延迟分布",
        telemetry.DefaultHistogramBuckets,
        "spider", "domain",
    )
    durationHisto.With("my_spider", "example.com").Observe(0.5)
}
```

> `LabeledRegistry` 完全兼容 `MetricsRegistry` 接口，可直接传入 `MetricsExtension`。

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
| `http.request` | Client | RequestReachedDownloader/RequestLeftDownloader | HTTP 请求子 Span（按 Request 指针关联，完整追踪请求-响应生命周期） |

## HTTP 端点

MetricsExtension 内置 HTTP 服务器，提供以下端点：

| 端点 | 说明 |
|------|------|
| `/metrics` | Prometheus 格式指标输出 |
| `/health` | 健康检查（返回 `ok`） |

## Grafana Dashboard

`grafana/` 目录提供开箱即用的 Grafana Dashboard JSON 模板，包含以下面板：

- 🕷️ **Spider 概览** — 状态、运行时长、请求/响应/Item/错误总数
- ⚡ **请求延迟** — P50/P90/P99 分位数曲线、QPS 吞吐量
- 🚨 **错误率** — 错误占比、错误与 Item 丢弃速率
- 📊 **队列深度** — 活跃请求数、调度器队列深度
- 🌐 **按域名维度** — 分域名 QPS 和延迟

导入方式：Grafana → Dashboards → Import → 上传 `grafana/scrapy-go-dashboard.json`

详见 [grafana/README.md](grafana/README.md)。

## 设计决策

- **适配器模式** — 将 OTel/Prometheus 的具体实现适配为 `pkg/telemetry` 定义的轻量级接口
- **信号驱动** — 通过框架信号系统自动采集数据，无需修改业务代码
- **独立子模块** — 避免主模块引入重量级依赖，用户按需安装
- **零侵入** — 未启用时使用 `NoopTracer`/`NoopMetricsRegistry`，零运行时开销
- **线程安全** — 所有组件保证并发安全，支持多 goroutine 同时访问
- **标签维度** — `LabeledRegistry` 支持按 Spider 名称/域名等维度分组指标，与 Grafana 模板变量联动

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
- `prometheus` 包：98.2%
- `extension` 包：91.5%
- **总体：95.0%+**
