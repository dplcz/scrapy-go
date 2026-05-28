# Dedup — 高级去重策略（独立模块）

[![Coverage](https://img.shields.io/badge/coverage-89.9%25-brightgreen)](./coverage.out)

`contrib/dedup` 是 scrapy-go 框架的可选高级去重扩展，面向复杂爬取场景提供比默认 `RFPDupeFilter` 更灵活的去重策略：

- 🔗 **URL 规范化去重**：参数排序、移除 `utm_*`、`fbclid`、`gclid` 等追踪参数
- 🧬 **SimHash 近似去重**：基于内容指纹与汉明距离阈值识别相似新闻/文章
- 🔁 **组合去重策略**：实现 `scheduler.DupeFilter` 接口，支持多策略链式组合
- 🪶 **零侵入**：未引入此模块时主模块不依赖任何高级去重相关代码
- ⚡ **高并发友好**：URL 精确集合使用 `sync.Map`，SimHash 使用 LSH 分桶缩小候选集合

## 与内置 RFPDupeFilter 的关系

| 维度 | 内置 `pkg/scheduler.RFPDupeFilter` | 本模块 `contrib/dedup` |
|------|------------------------------------|------------------------|
| URL 参数排序 | ✅ | ✅ |
| 追踪参数过滤 | ❌ | ✅ `utm_*` / `fbclid` / `gclid` 等 |
| 内容近似去重 | ❌ | ✅ SimHash + 汉明距离阈值 |
| 组合策略 | ❌ | ✅ `CompositeDupeFilter` |
| 主模块依赖 | 内置 | 可选独立子模块 |

**协作语义**：本模块实现 `scheduler.DupeFilter` 接口，可通过 `scheduler.WithDupeFilter(filter)` 注入默认调度器。`CompositeDupeFilter` 使用 OR 语义，任一子策略判定重复即过滤请求。

## 安装

```bash
go get github.com/dplcz/scrapy-go/contrib/dedup
```

> 📋 **要求**：Go 1.25.1+

## 快速开始

### 默认高级组合策略

```go
package main

import (
    "context"
    "log"

    "github.com/dplcz/scrapy-go/contrib/dedup"
    "github.com/dplcz/scrapy-go/pkg/scheduler"
)

func main() {
    filter, err := dedup.NewDupeFilter(dedup.DefaultOptions())
    if err != nil {
        log.Fatal(err)
    }

    sched := scheduler.NewDefaultScheduler(
        scheduler.WithDupeFilter(filter),
    )

    if err := sched.Open(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer sched.Close(context.Background(), "finished")

    // ... 将 sched 注入 Crawler 或自定义 Engine 组装流程 ...
}
```

### URL 规范化去重

```go
filter := dedup.NewURLCanonicalDupeFilter(&dedup.URLCanonicalizerOptions{
    DropTrackingParams: true,
    TrackingParamNames: []string{"fbclid", "gclid", "session_id"},
    TrackingParamPrefixes: []string{"utm_", "trk_"},
})
```

以下 URL 会被视为同一页面：

```text
https://example.com/article?id=1&utm_source=news
https://EXAMPLE.com:443/article?fbclid=abc&id=1
```

规范化结果：

```text
https://example.com/article?id=1
```

### SimHash 内容近似去重

`SimHashDupeFilter` 默认从请求 `Meta` 中读取 `dedup_content` 字段。未提供内容时会自动跳过该策略，不影响 URL 去重。

```go
filter, _ := dedup.NewSimHashDupeFilter(&dedup.SimHashOptions{
    HammingThreshold: 3,
})

req, _ := shttp.NewRequest("https://example.com/news/1")
req.SetMeta(dedup.MetaContentKey, "这里是文章正文或摘要内容")

isDuplicate := filter.RequestSeen(req)
```

也可以提供自定义内容提取器：

```go
filter, _ := dedup.NewSimHashDupeFilter(&dedup.SimHashOptions{
    HammingThreshold: 4,
    ContentExtractor: func(req *shttp.Request) ([]byte, bool) {
        title, ok := req.GetMeta("title")
        if !ok {
            return nil, false
        }
        return []byte(fmt.Sprint(title)), true
    },
})
```

## 配置项详解

### Options

```go
opts := dedup.DefaultOptions()
opts.URLCanonicalization = true
opts.SimHash = true
opts.URLOptions = dedup.DefaultURLCanonicalizerOptions()
opts.SimHashOptions = dedup.DefaultSimHashOptions()
```

### URLCanonicalizerOptions

```go
urlOpts := dedup.DefaultURLCanonicalizerOptions()
urlOpts.KeepFragments = false
urlOpts.DropTrackingParams = true
urlOpts.TrackingParamNames = []string{"fbclid", "gclid", "msclkid"}
urlOpts.TrackingParamPrefixes = []string{"utm_"}
```

### SimHashOptions

```go
simOpts := dedup.DefaultSimHashOptions()
simOpts.HammingThreshold = 3 // 0 表示仅完全相同 SimHash，常用范围 3~5
simOpts.BandCount = 4        // 0 时按阈值自动选择
simOpts.MetaContentKey = dedup.MetaContentKey
```

## 架构设计

```text
┌──────────────────────────────────────────────────────┐
│                    Scheduler                         │
│  EnqueueRequest(req)                                 │
│        │                                             │
│        ▼                                             │
│  ┌──────────────────────────────────────────────┐    │
│  │          CompositeDupeFilter                 │    │
│  │  ┌──────────────────────┐                    │    │
│  │  │ URLCanonicalFilter   │  Method + URL + Body│    │
│  │  └──────────────────────┘                    │    │
│  │  ┌──────────────────────┐                    │    │
│  │  │ SimHashFilter        │  Meta 内容近似去重  │    │
│  │  └──────────────────────┘                    │    │
│  └──────────────────────────────────────────────┘    │
│        │ false                                       │
│        ▼                                             │
│      Queue                                           │
└──────────────────────────────────────────────────────┘
```

## 设计决策

| Python/Scrapy 风格 | Go 替换方案 | 原因 |
|---|---|---|
| 动态回调中读取响应内容再全局去重 | 通过 `Request.Meta` 显式传入内容或自定义 `ContentExtractor` | Scheduler 阶段没有 Response，显式数据流更清晰 |
| 近似去重线性扫描全部指纹 | LSH 分桶候选集 + 汉明距离精确校验 | 避免指纹数量增长后 O(N) 扫描成为瓶颈 |
| 全局锁保护所有策略 | URL 策略用 `sync.Map`；SimHash 仅在索引更新时加锁 | 减少高并发入队路径锁竞争 |
| 隐式修改主模块配置 | 独立子模块 + `scheduler.WithDupeFilter` 注入 | 保持主模块零侵入与可插拔架构 |

## 测试覆盖率

```bash
go test ./... -race -coverprofile=coverage.out
go tool cover -func=coverage.out
# coverage: 89.9% of statements
```

## License

MIT
