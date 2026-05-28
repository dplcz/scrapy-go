# Proxy Pool — 代理池管理器（独立模块）

[![Coverage](https://img.shields.io/badge/coverage-91%25-brightgreen)](./coverage.out)

`contrib/proxy` 是 scrapy-go 框架的可选代理池扩展，提供生产级代理管理能力：

- 🎯 **多种轮换策略**：`round_robin` / `random` / `weighted`
- 🩺 **后台健康检查**：周期性探测代理可用性，失败自动剔除，恢复后自动重新启用
- 🔁 **失败自动重试**：与下载链路集成，下载失败时自动切换代理重试（受最大次数限制）
- 🌐 **多种代理来源**：`StaticProvider` / `FileProvider` / `HTTPAPIProvider` / `CompositeProvider`
- 🪶 **零侵入**：未引入此模块时主模块不依赖任何代理相关额外代码
- ⚡ **高并发友好**：`atomic` + `sync.RWMutex` 组合优化，候选切片对象池复用

## 与内置 HttpProxyMiddleware 的关系

| 维度 | 内置 `pkg/downloader/middleware.HttpProxyMiddleware` | 本模块 `contrib/proxy.Middleware` |
|------|------|------|
| 优先级 | 750 | 740（先于内置执行） |
| 代理来源 | 仅环境变量 `HTTP_PROXY`/`HTTPS_PROXY` | Static / File / HTTP API / 自定义 Provider |
| 多代理轮换 | ❌ 不支持 | ✅ RoundRobin / Random / Weighted |
| 健康检查 | ❌ | ✅ 后台 goroutine 周期性探测 |
| 失败自动重试 | ❌ | ✅ 与 RetryMiddleware 协作 |
| 依赖 | 0 | 0（仅依赖主模块） |

**协作语义**：本中间件优先级 740 先执行，将选定代理写入 `Meta["proxy"]`；
内置中间件 750 后执行检测到 `Meta["proxy"]` 已存在则不会覆盖，从而实现自然的优先级覆盖。

## 安装

```bash
go get github.com/dplcz/scrapy-go/contrib/proxy
```

> 📋 **要求**：Go 1.25.1+

## 快速开始

### 静态列表 + 轮询策略

```go
package main

import (
    "context"
    "log"

    "github.com/dplcz/scrapy-go/contrib/proxy"
    "github.com/dplcz/scrapy-go/pkg/crawler"
)

func main() {
    opts := proxy.DefaultOptions()
    opts.Strategy = proxy.StrategyRoundRobin

    provider := proxy.NewStaticProvider([]string{
        "http://user:pass@proxy1.example.com:8080",
        "http://proxy2.example.com:8080",
        "http://proxy3.example.com:8080|10", // 权重 10（仅 weighted 策略使用）
    })

    pool, err := proxy.NewPool(opts, provider)
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    mw := proxy.NewMiddlewareWithOptions(pool, opts, nil)

    c := crawler.NewDefault()
    // 注册中间件，建议优先级 740（先于内置 HttpProxy 750）
    c.AddDownloaderMiddleware(mw, "ProxyPool", proxy.DefaultPriority)

    // ... 启动爬虫 ...
    _ = ctx
}

var ctx = context.Background()
```

### 加权随机策略 + 文件来源

```go
opts := proxy.DefaultOptions()
opts.Strategy = proxy.StrategyWeighted

// 文件每行一个代理 URL；# 开头为注释
provider := proxy.NewFileProvider("/etc/scrapy-go/proxies.txt")

pool, _ := proxy.NewPool(opts, provider)
defer pool.Close()
```

文件格式（`/etc/scrapy-go/proxies.txt`）：

```text
# 高权重主用代理
http://user1:pass1@premium-proxy.example.com:8080|10

# 低权重备用代理
http://backup1.example.com:8080|1
http://backup2.example.com:8080|1
```

### HTTP API 来源 + 周期性刷新

```go
opts := proxy.DefaultOptions()
opts.Strategy = proxy.StrategyRandom
opts.ProviderRefreshInterval = 5 * time.Minute // 每 5 分钟从 API 重新拉取

provider, err := proxy.NewHTTPAPIProvider(&proxy.HTTPAPIProviderOptions{
    URL:            "https://proxy-vendor.example.com/api/v1/proxies",
    Method:         "GET",
    Headers:        map[string]string{"Authorization": "Bearer xxx"},
    ResponseFormat: "json_field",
    JSONFieldPath:  "data.proxies",
})
if err != nil {
    log.Fatal(err)
}

pool, _ := proxy.NewPool(opts, provider)
defer pool.Close()
```

### 多个代理来源组合

```go
provider := proxy.NewCompositeProvider(
    proxy.NewStaticProvider([]string{"http://emergency-proxy:8080"}),
    proxy.NewFileProvider("/etc/scrapy-go/proxies.txt"),
    httpAPIProvider,
)
```

## 配置项详解

```go
opts := proxy.DefaultOptions()

// 策略
opts.Strategy = proxy.StrategyRoundRobin // round_robin / random / weighted

// 健康判定
opts.MaxFailures = 3           // 连续失败 N 次后标记为 Unhealthy
opts.RecoveryThreshold = 1     // 健康检查连续通过 N 次后从 Unhealthy 恢复

// 健康检查
opts.HealthCheckEnabled = true
opts.HealthCheckURL = "http://www.google.com/generate_204"
opts.HealthCheckInterval = 30 * time.Second
opts.HealthCheckTimeout = 5 * time.Second
opts.HealthCheckExpectedStatus = 204

// Provider 周期刷新
opts.ProviderRefreshInterval = 0 // 0 表示不周期性刷新

// 自动重试
opts.AutoRetryOnFailure = true
opts.MaxProxyRetries = 3 // 同一请求最多切换 N 次代理
```

## 架构设计

### 核心组件

```text
┌──────────────────────────────────────────────────────┐
│                      Crawler                          │
│  ┌──────────┐    ┌─────────────────┐    ┌─────────┐ │
│  │ Engine   │ -> │ DownloaderMW    │ -> │  HTTP   │ │
│  │          │    │ (proxy.Mw 740)  │    │  Client │ │
│  └──────────┘    └────────┬────────┘    └─────────┘ │
└───────────────────────────┼──────────────────────────┘
                            │ Get() / Mark()
                            ▼
                     ┌──────────────┐
                     │  Pool        │
                     │  ┌────────┐  │     ┌──────────────┐
                     │  │Strategy│◀─┼──── │HealthChecker │
                     │  └────────┘  │     │ (后台 goroutine)│
                     │  ┌────────┐  │     └──────────────┘
                     │  │Proxies │  │
                     │  └────────┘  │
                     └──────┬───────┘
                            │ Refresh()
                            ▼
                     ┌──────────────┐
                     │  Provider    │
                     │ (Static/File │
                     │  /HTTPAPI)   │
                     └──────────────┘
```

### 设计决策

| Python/Scrapy 风格 | Go 替换方案 | 原因 |
|---|---|---|
| 全局 lock 包住整个 pool | `sync.RWMutex` 读写分离 + `atomic` 统计字段 | 读多写少场景，避免读路径锁竞争 |
| RoundRobin 计数 `i = (i + 1) % n` 加锁 | `atomic.Uint64` 自增取模 | 高并发下零锁开销 |
| Random 用 `math/rand` 全局 source | `math/rand/v2` (Go 1.22+) | 无锁实现，无需用户初始化种子 |
| Weighted 选择 O(n) 遍历 | 累积权重数组 + `sort.SearchInts` 二分 O(log n)；小池子退化为线性扫描以避免分配 | 大池时显著性能差异 |
| 健康检查阻塞触发 | 后台 `goroutine` + `time.Ticker`，`context.Context` 控制退出 | 避免泄漏、避免阻塞业务路径 |
| 失败重试 `for { ... time.Sleep }` | 返回 `errors.NewRequestError` 由 Engine 复用 RetryMiddleware 重新调度 | 避免重复造轮子 |

## 性能特性

- **Get 路径无堆分配**：候选切片通过 `sync.Pool` 复用
- **统计字段无锁**：`Failures`/`Successes`/`TotalUsed`/`State` 全部使用 `atomic` 操作
- **健康检查并发限制**：信号量限制最大并发探测数为 8，避免大池时一次性占用过多 socket

## 测试覆盖率

```bash
go test ./... -race -coverprofile=coverage.out
go tool cover -func=coverage.out
# coverage: 91.0% of statements
```

## License

MIT
