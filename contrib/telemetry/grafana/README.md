# Grafana Dashboard 模板

本目录提供 scrapy-go 框架的开箱即用 Grafana Dashboard JSON 模板。

> 📌 **v1.3.0 (TraceExtension v2)**：Dashboard 已适配以 Item 产出链路为核心的追踪模型，
> 新增 `📦 Item 产出链路` 与 `🔍 分布式追踪` 两行面板，并提供跳转到 Tempo / Jaeger 的入口。

## 包含面板

### 🕷️ Spider 概览

| 面板 | 说明 | 指标来源 |
|------|------|---------|
| Spider 状态 | 运行/停止状态指示 | `scrapy_spider_state` |
| 运行时长 | Spider 已运行时间 | `scrapy_spider_elapsed_seconds` |
| 总请求/响应数 | 累计请求和响应计数 | `scrapy_requests_total` / `scrapy_responses_total` |
| 已抓取 Items | 成功处理的 Item 总数 | `scrapy_items_scraped_total` |
| 总错误数 | 累计错误计数 | `scrapy_errors_total` |

### ⚡ 请求延迟

| 面板 | 说明 | 指标来源 |
|------|------|---------|
| 请求延迟分位数 | P50/P90/P99 延迟曲线 | `scrapy_request_duration_seconds` |
| 请求/响应吞吐量 | QPS 实时曲线 | `scrapy_requests_total` / `scrapy_responses_total` |

### 🚨 错误率

| 面板 | 说明 | 指标来源 |
|------|------|---------|
| 错误率 | 错误占请求比例 | `scrapy_errors_total` / `scrapy_requests_total` |
| 错误 & Item 丢弃速率 | 错误和丢弃的速率曲线 | `scrapy_errors_total` / `scrapy_items_dropped_total` |

### 📊 队列深度 & 活跃请求

| 面板 | 说明 | 指标来源 |
|------|------|---------|
| 活跃请求数 | 当前并发请求数 | `scrapy_active_requests` |
| 调度器队列深度 | 待处理请求队列长度 | `scrapy_scheduler_queue_size` |

### 🌐 按域名维度

| 面板 | 说明 | 指标来源 |
|------|------|---------|
| 按域名请求 QPS | 分域名的请求吞吐量 | `scrapy_requests_total{domain=...}` |
| 按域名请求延迟 P90 | 分域名的延迟分位数 | `scrapy_request_duration_seconds{domain=...}` |

### 📦 Item 产出链路（v1.3.0 新增，对齐 TraceExtension v2）

| 面板 | 说明 | 指标 / Trace 来源 |
|------|------|-------------------|
| Item 产出 / 丢弃速率 | 堆叠时序图，展示 Pipeline 输出速率 | `scrapy_items_scraped_total` / `scrapy_items_dropped_total` |
| Item Pipeline 成功率 | scraped / (scraped + dropped) 仪表盘，<95% 黄色，<90% 红色 | 同上聚合 |
| 已抓取 Item 总数 | 累计 stat | `scrapy_items_scraped_total` |
| 已丢弃 Item 总数 | 累计 stat（黄/红阈值） | `scrapy_items_dropped_total` |
| 请求 P95 延迟（Item 产出近似下限） | 由于 Item 产出依赖回调返回，请求 P95 是其耗时下限 | `scrapy_request_duration_seconds` |

> 该行面板对应 TraceExtension v2 中 `item.pipeline` Span 的三种 Event：
> `pipeline.success` / `pipeline.dropped` / `pipeline.error`。
> 当成功率下降时，建议通过本 Dashboard 顶部 Links 跳转到 Trace 后端，
> 按 `event.name = "pipeline.dropped"` 过滤定位被丢弃的具体 Item。

### 🔍 分布式追踪（v1.3.0 新增）

该行不依赖 Prometheus 指标，提供两块说明性 Text Panel：

| 面板 | 说明 |
|------|------|
| 📖 Trace 模型与排查指南 | 简述 v2 追踪层级（`spider.crawl` → `scrape:{Callback}` → `item.pipeline`）、关键 Span 属性、指标 vs Trace 职责划分 |
| 🔎 常用 TraceQL 示例 | 列出 Tempo / Jaeger 中查询慢回调、被丢弃 Item、按 callback / session 过滤等常见查询语句 |

Dashboard 顶部右侧 **Links** 提供两个跳转入口：

- 📖 **TraceExtension v2 设计文档**：跳转到 [`contrib/telemetry/README.md`](../README.md)
- 🔍 **跳转到 Trace 后端**：通过 `${tracing_ds}` 变量自动跳转到 Grafana Explore，并预填 `{ .spider.name = "$spider" }` 查询条件

## 模板变量

| 变量 | 类型 | 说明 | 数据源查询 |
|------|------|------|-----------|
| `$spider` | query | Spider 名称过滤 | `label_values(scrapy_spider_state, spider)` |
| `$domain` | query | 域名过滤 | `label_values(scrapy_requests_total, domain)` |
| `$tracing_ds` | datasource | Trace 后端数据源（Tempo / Jaeger） | `tempo,jaeger`（v1.3.0 新增） |

## 指标 vs Trace 的职责划分（v1.3.0 设计准则）

| 维度 | 指标（本 Dashboard） | Trace（Tempo / Jaeger） |
|------|---------------------|-------------------------|
| **目标** | 长期趋势、SLO、告警 | 单次请求的因果链路与耗时分解 |
| **粒度** | Spider / 域名聚合 | 单个回调（`scrape:{callback}`）/ 单个 Item（`item.pipeline`） |
| **存储成本** | 低（按 label 聚合） | 中等（每 Span ~ KB 级） |
| **典型问题** | "近 1 小时错误率为何升高？" | "这个被丢弃的 Item 是哪个 callback 产出的？" |
| **保留期** | 长（数月） | 短（默认 7 天） |

> 简言之：**指标回答"系统现在健康吗"**，**Trace 回答"这次请求为什么这样"**。

## 导入方式

### 方式一：Grafana UI 导入

1. 打开 Grafana → Dashboards → Import
2. 上传 `scrapy-go-dashboard.json` 文件
3. 选择 Prometheus 数据源（**必需**）
4. 选择 Trace 数据源（**可选**，Tempo / Jaeger；不配置时 `🔍 分布式追踪` 行的跳转链接会失效，但其他面板正常工作）
5. 点击 Import

### 方式二：Grafana Provisioning

将 JSON 文件放入 Grafana provisioning 目录：

```yaml
# /etc/grafana/provisioning/dashboards/scrapy-go.yaml
apiVersion: 1
providers:
  - name: scrapy-go
    orgId: 1
    folder: "Scrapy-Go"
    type: file
    options:
      path: /var/lib/grafana/dashboards/scrapy-go
```

然后将 `scrapy-go-dashboard.json` 复制到 `/var/lib/grafana/dashboards/scrapy-go/` 目录。

## 前置条件

### 必需：Prometheus 指标

1. scrapy-go 应用已启用 `MetricsExtension`（Prometheus 指标收集）
2. 使用 `LabeledRegistry` 以支持按 Spider/域名维度的面板
3. Prometheus 已配置抓取 scrapy-go 的 `/metrics` 端点

#### Prometheus 抓取配置示例

```yaml
scrape_configs:
  - job_name: 'scrapy-go'
    scrape_interval: 10s
    static_configs:
      - targets: ['localhost:9090']
```

### 可选：Trace 后端（Tempo / Jaeger）

启用 `🔍 分布式追踪` 行的跳转能力时需要：

1. scrapy-go 应用已启用 `TraceExtension`（v2，OTLP 导出）：

   ```go
   tracer := otel.NewTracer(...) // 通过 OTLP 上报到 Tempo / Jaeger
   ext := telemetry.NewTraceExtension(tracer, signals, logger,
       telemetry.WithCallbackRegistry(registry),
       telemetry.WithPropagationPolicy(telemetry.PropagateWithinSession),
       telemetry.WithTraceItemPipeline(true),
   )
   crawler.AddExtension(ext, "TraceExtension", 100)
   engine.SetTraceInjector(ext)
   ```

2. Grafana 中已配置 Tempo / Jaeger 数据源（导入时选中 `Tracing` 数据源）。

3. （推荐）在 Tempo 数据源配置中开启 **Trace to Metrics** 关联，从 Span 反向跳转回本 Dashboard。

## 自定义

Dashboard 模板使用 Grafana 标准的 `__inputs` 机制，导入时会自动提示选择数据源。

如需自定义面板，可在导入后直接在 Grafana UI 中编辑，或修改 JSON 文件后重新导入。

## 变更历史

- **v2 (2026-06-08)** — 配套 TraceExtension v2 重构（CHANGELOG P5-026f / Phase 6）
  - 新增 `📦 Item 产出链路` 行（5 个面板）
  - 新增 `🔍 分布式追踪` 行（2 个 Text 面板 + 跳转链接）
  - 新增 `tracing_ds` 数据源变量
  - 顶部新增两个 Dashboard Links（设计文档 / Trace 后端）
- **v1** — 首版 Dashboard，覆盖 Spider 概览 / 延迟 / 错误 / 队列 / 域名维度
