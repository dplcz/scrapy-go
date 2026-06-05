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

    // 创建追踪扩展（v2：以回调链因果关系为核心）
    traceExt := telemetry.NewTraceExtension(tracer, c.Signals, c.Logger,
        telemetry.WithCallbackRegistry(c.CallbackRegistry),  // 启用回调名称解析
        telemetry.WithMaxActiveSpans(10000),                 // 最大活跃 Span 数
        // 默认 Session 策略：仅在同一 session 内延续 Trace（断点续爬安全）
        // telemetry.WithPropagationPolicy(telemetry.PropagateWithinSession),
        // 默认 HTTP 下载追踪：仅记录 Event，不创建独立 Span（高信噪比）
        // telemetry.WithTraceHTTPDownload(false),
    )
    c.AddExtension(traceExt, "TraceExtension", 100)

    // 将 TraceExtension 注入 Engine 作为 TraceContextInjector
    // 启用回调链因果关系追踪（trace context 自动传播）
    c.Engine.SetTraceInjector(traceExt)

    c.Run(ctx, NewMySpider())
}
```

### Trace Context 传播策略（断点续爬 / 分布式）

通过 `WithPropagationPolicy` 配置 Trace Context 在请求链中的传播行为：

```go
// 单机部署 + 断点续爬（默认推荐）
// 重启后开启新 Trace，避免 Jaeger 中出现僵尸链路
traceExt := telemetry.NewTraceExtension(tracer, c.Signals, c.Logger,
    telemetry.WithPropagationPolicy(telemetry.PropagateWithinSession),
)

// 分布式爬取：Worker A 下发请求 → Worker B 处理（跨进程保留完整链路）
traceExt := telemetry.NewTraceExtension(tracer, c.Signals, c.Logger,
    telemetry.WithPropagationPolicy(telemetry.PropagateAlways),
)

// 调试模式：每个回调创建独立 Trace
traceExt := telemetry.NewTraceExtension(tracer, c.Signals, c.Logger,
    telemetry.WithPropagationPolicy(telemetry.PropagateNever),
)
```

| 策略 | _trace_parent | _trace_session | 适用场景 |
|------|---------------|----------------|---------|
| `PropagateWithinSession`（默认） | ✅ 注入 | ✅ 注入 | 单机 + 断点续爬，避免僵尸 Trace |
| `PropagateAlways` | ✅ 注入 | ❌ 不注入 | 分布式实时爬取（多 Worker 共享 Redis 队列） |
| `PropagateNever` | ❌ 不注入 | ❌ 不注入 | 调试，每回调独立 Trace |

> **断点续爬原理**：`PropagateWithinSession` 模式下，Engine 启动时为本次运行生成一个唯一 sessionID（16 字节随机十六进制），注入新请求的 `_trace_session` Meta。重启后 sessionID 会变化，从磁盘队列恢复的旧请求 `_trace_session` 不再匹配，因此 `BeforeScrape` 会忽略其旧 traceparent，使用新的 rootCtx 作为 parent，避免在追踪后端产生跨越数小时的僵尸链路。

### HTTP 下载追踪开关（v1 兼容模式）

默认情况下，HTTP 下载只在 scrape Span 上记录 `http.download` / `http.response` Event，
不创建独立子 Span，以提高信噪比并减少 Span 数量。

如需恢复 v1 旧行为（每个 HTTP 请求创建独立 `http.request` 子 Span），开启 `WithTraceHTTPDownload`：

```go
traceExt := telemetry.NewTraceExtension(tracer, c.Signals, c.Logger,
    telemetry.WithTraceHTTPDownload(true), // 为每个 HTTP 请求创建独立 Span
)
```

> ⚠️ 注意：开启此开关后 Span 数量约增加 2 倍，建议仅在需要精确分析每个 HTTP 请求耗时的场景下使用。

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

### 追踪 Span（v2）

| Span 名称 | Kind | 触发方式 | 说明 |
|-----------|------|---------|------|
| `spider.crawl` | Internal | SpiderOpened/Closed 信号 | Spider 生命周期根 Span（含 `trace.session_id` / `trace.propagation_policy` 属性） |
| `scrape:{callback}` | Internal | Engine.downloadAndScrape → TraceContextInjector | 回调执行 Span（如 `scrape:ParseDetail`），parent 从 `_trace_parent` Meta 恢复 |
| `item.pipeline` | Internal | Scraper.processOutputs → TraceContextInjector | Item Pipeline 处理 Span，parent 为对应的 scrape Span |
| `http.request` | Client | RequestReachedDownloader / RequestLeftDownloader 信号 | **可选** — 仅在 `WithTraceHTTPDownload(true)` 时创建，每个 HTTP 请求一个独立子 Span |

**Event（记录在 scrape Span 上）**：

| Event 名称 | 触发信号 | 说明 |
|-----------|---------|------|
| `http.download` | RequestReachedDownloader | 记录下载开始（URL、Method） |
| `http.response` | RequestLeftDownloader | 记录下载完成（状态码、延迟） |
| `spider.error` | SpiderError | 记录错误事件 |
| `item.scraped` | ItemScraped | 记录 Item 产出 |
| `item.dropped` | ItemDropped | 记录 Item 丢弃 |
| `item.error` | ItemError | 记录 Item 处理错误 |

**Event（记录在 item.pipeline Span 上）**：

| Event 名称 | 触发条件 | 说明 |
|-----------|---------|------|
| `pipeline.success` | Pipeline 处理成功（err == nil） | 所有 Pipeline 阶段通过 |
| `pipeline.dropped` | Pipeline 返回 ErrDropItem | Item 被业务逻辑丢弃 |
| `pipeline.error` | Pipeline 返回其他错误 | Pipeline 处理异常 |

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
- **信号驱动 + Engine 注入** — 通过框架信号系统自动采集数据，同时通过 `TraceContextInjector` 接口在 Engine 和 Scraper 层实现 trace context 传播，无需修改业务代码
- **回调链因果追踪（v2）** — 追踪粒度从 HTTP 请求提升到回调执行，`scrape:{callback}` Span 替代 `http.request` Span，HTTP 下载降级为 Event
- **Item Pipeline 独立 Span** — 每个 Item 的 Pipeline 处理创建 `item.pipeline` 子 Span，精确记录处理耗时和结果（成功/丢弃/错误），可通过 `WithTraceItemPipeline(false)` 禁用
- **Trace Context 传播** — 通过 `Request.Meta["_trace_parent"]` 传播 W3C traceparent 格式字符串，天然兼容序列化（磁盘队列/Redis 队列）
- **Session 策略（断点续爬安全）** — 默认 `PropagateWithinSession` 模式通过 `_trace_session` Meta 区分本次运行与历史运行，避免断点续爬恢复的旧请求产生跨越数小时的僵尸 Trace；分布式场景可切换为 `PropagateAlways`，调试场景可切换为 `PropagateNever`
- **HTTP 下载追踪开关（v1 兼容）** — `WithTraceHTTPDownload(true)` 可恢复 v1 旧行为（为每个 HTTP 请求创建独立 `http.request` 子 Span），默认关闭以提高信噪比
- **独立子模块** — 避免主模块引入重量级依赖，用户按需安装
- **零侵入** — 未启用时使用 `NoopTracer`/`NoopMetricsRegistry`，零运行时开销
- **线程安全** — 所有组件保证并发安全，支持多 goroutine 同时访问
- **标签维度** — `LabeledRegistry` 支持按 Spider 名称/域名等维度分组指标，与 Grafana 模板变量联动
- **CrawlerAwareExtension** — `TraceExtension` 和 `MetricsExtension` 实现 `CrawlerAwareExtension` 接口，在 `Open` 之前自动从 Crawler 获取最新的 Signals/Logger 引用，避免因 Spider `CustomSettings` 重建组件导致引用失效（v1.2.1+）

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
- `extension` 包：89.5%
- **总体：95.0%+**
