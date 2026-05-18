# Grafana Dashboard 模板

本目录提供 scrapy-go 框架的开箱即用 Grafana Dashboard JSON 模板。

## 包含面板

| 面板 | 说明 | 指标来源 |
|------|------|---------|
| Spider 状态 | 运行/停止状态指示 | `scrapy_spider_state` |
| 运行时长 | Spider 已运行时间 | `scrapy_spider_elapsed_seconds` |
| 总请求/响应数 | 累计请求和响应计数 | `scrapy_requests_total` / `scrapy_responses_total` |
| 已抓取 Items | 成功处理的 Item 总数 | `scrapy_items_scraped_total` |
| 总错误数 | 累计错误计数 | `scrapy_errors_total` |
| 请求延迟分位数 | P50/P90/P99 延迟曲线 | `scrapy_request_duration_seconds` |
| 请求/响应吞吐量 | QPS 实时曲线 | `scrapy_requests_total` / `scrapy_responses_total` |
| 错误率 | 错误占请求比例 | `scrapy_errors_total` / `scrapy_requests_total` |
| 错误 & Item 丢弃速率 | 错误和丢弃的速率曲线 | `scrapy_errors_total` / `scrapy_items_dropped_total` |
| 活跃请求数 | 当前并发请求数 | `scrapy_active_requests` |
| 调度器队列深度 | 待处理请求队列长度 | `scrapy_scheduler_queue_size` |
| 按域名请求 QPS | 分域名的请求吞吐量 | `scrapy_requests_total{domain=...}` |
| 按域名请求延迟 P90 | 分域名的延迟分位数 | `scrapy_request_duration_seconds{domain=...}` |

## 模板变量

| 变量 | 说明 | 数据源查询 |
|------|------|-----------|
| `$spider` | Spider 名称过滤 | `label_values(scrapy_spider_state, spider)` |
| `$domain` | 域名过滤 | `label_values(scrapy_requests_total, domain)` |

## 导入方式

### 方式一：Grafana UI 导入

1. 打开 Grafana → Dashboards → Import
2. 上传 `scrapy-go-dashboard.json` 文件
3. 选择 Prometheus 数据源
4. 点击 Import

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

1. scrapy-go 应用已启用 `MetricsExtension`（Prometheus 指标收集）
2. 使用 `LabeledRegistry` 以支持按 Spider/域名维度的面板
3. Prometheus 已配置抓取 scrapy-go 的 `/metrics` 端点

### Prometheus 抓取配置示例

```yaml
scrape_configs:
  - job_name: 'scrapy-go'
    scrape_interval: 10s
    static_configs:
      - targets: ['localhost:9090']
```

## 自定义

Dashboard 模板使用 Grafana 标准的 `__inputs` 机制，导入时会自动提示选择数据源。

如需自定义面板，可在导入后直接在 Grafana UI 中编辑，或修改 JSON 文件后重新导入。
