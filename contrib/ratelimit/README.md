# contrib/ratelimit — 分布式限速器

[![Go Reference](https://pkg.go.dev/badge/github.com/dplcz/scrapy-go/contrib/ratelimit.svg)](https://pkg.go.dev/github.com/dplcz/scrapy-go/contrib/ratelimit)

基于 Redis 滑动窗口算法的分布式限速器，用于控制爬虫的请求速率。

## 特性

- **分布式限速**：基于 Redis 实现，支持多实例部署场景下的全局速率控制
- **滑动窗口算法**：相比固定窗口更平滑，避免窗口边界突发流量
- **原子操作**：通过 Redis Lua 脚本保证并发安全，无竞态条件
- **按域名限速**：不同域名拥有独立的速率窗口，支持差异化配置
- **优雅降级**：Redis 不可用时自动降级为不限速，不阻塞爬虫运行
- **信号驱动**：通过 `RateLimitExtension` 自动集成到框架信号系统
- **共享连接**：支持与 `contrib/redisqueue` 共享 Redis 连接

## 安装

```bash
go get github.com/dplcz/scrapy-go/contrib/ratelimit
```

## 快速开始

### 基本用法

```go
package main

import (
    "context"
    "log"

    "github.com/dplcz/scrapy-go/contrib/ratelimit"
)

func main() {
    opts := ratelimit.DefaultOptions()
    opts.Addr = "localhost:6379"
    opts.DefaultRate = 10  // 每秒 10 个请求
    opts.DefaultBurst = 20 // 突发容量 20

    limiter, err := ratelimit.NewRedisSlidingWindowLimiter(opts)
    if err != nil {
        log.Fatal(err)
    }
    defer limiter.Close()

    // 非阻塞检查
    if limiter.Allow("example.com") {
        // 请求被允许
    }

    // 阻塞等待
    ctx := context.Background()
    if err := limiter.Wait(ctx, "example.com"); err != nil {
        log.Printf("rate limit wait failed: %v", err)
    }
}
```

### 与框架集成（Extension 模式）

```go
package main

import (
    "log/slog"

    "github.com/dplcz/scrapy-go/contrib/ratelimit"
    "github.com/dplcz/scrapy-go/pkg/signal"
)

func main() {
    // 创建限速器
    opts := ratelimit.DefaultOptions()
    opts.Addr = "localhost:6379"
    opts.DefaultRate = 10
    opts.DefaultBurst = 10

    limiter, err := ratelimit.NewRedisSlidingWindowLimiter(opts)
    if err != nil {
        log.Fatal(err)
    }

    // 创建扩展（传入框架的信号管理器）
    signals := signal.NewManager(slog.Default())
    ext := ratelimit.NewRateLimitExtension(limiter, signals, nil)

    // 扩展会自动监听 RequestReachedDownloader 信号
    // 在请求到达下载器时进行限速检查
    _ = ext
}
```

### 按域名差异化限速

```go
opts := ratelimit.DefaultOptions()
opts.DefaultRate = 10 // 默认每秒 10 个请求
opts.DomainRates = map[string]int{
    "api.example.com":  5,   // API 接口限制更严格
    "cdn.example.com":  100, // CDN 可以更快
    "slow.example.com": 1,   // 慢速站点
}
```

### 共享 Redis 连接

```go
import "github.com/redis/go-redis/v9"

// 创建共享的 Redis 客户端
client := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

// 限速器使用共享连接（Close 时不会关闭 client）
opts := ratelimit.DefaultOptions()
limiter, err := ratelimit.NewRedisSlidingWindowLimiterFromClient(client, opts)
```

## 配置选项

| 选项 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Addr` | string | `localhost:6379` | Redis 服务器地址 |
| `Password` | string | `""` | Redis 认证密码 |
| `DB` | int | `0` | Redis 数据库编号 |
| `KeyPrefix` | string | `scrapy-go:ratelimit` | Redis Key 前缀 |
| `DefaultRate` | int | `10` | 每个窗口周期内允许的最大请求数 |
| `DefaultBurst` | int | `20` | 突发容量 |
| `Window` | Duration | `1s` | 滑动窗口时间长度 |
| `DomainRates` | map[string]int | `nil` | 按域名配置独立速率 |
| `WaitTimeout` | Duration | `30s` | Wait 方法默认超时 |
| `DialTimeout` | Duration | `5s` | Redis 连接超时 |
| `ReadTimeout` | Duration | `3s` | Redis 读超时 |
| `WriteTimeout` | Duration | `3s` | Redis 写超时 |
| `PoolSize` | int | `10` | Redis 连接池大小 |
| `KeyExpiration` | Duration | `1h` | 限速 Key 过期时间 |

## 算法说明

### 滑动窗口限速

使用 Redis Sorted Set 实现滑动窗口：

1. 每个请求以当前时间戳（微秒）作为 score 存入 Sorted Set
2. 每次检查时，先移除窗口外的过期记录（`ZREMRANGEBYSCORE`）
3. 统计当前窗口内的请求数量（`ZCARD`）
4. 如果未超过限制，添加当前请求并返回允许
5. 如果已超过限制，返回拒绝并计算建议重试时间

### Lua 脚本原子性

所有限速逻辑封装在单个 Lua 脚本中执行，保证：
- 移除过期记录、计数、添加新记录三步操作的原子性
- 多实例并发场景下无竞态条件
- 避免分布式锁的开销

### 降级策略

当 Redis 不可用时：
- `Allow()` 返回 `true`（允许所有请求）
- `Wait()` 立即返回 `nil`（不阻塞）
- 限速器关闭后同样降级为不限速

## Redis Key 格式

```
{KeyPrefix}:{domain}
```

示例：
```
scrapy-go:ratelimit:example.com
scrapy-go:ratelimit:api.example.com
```

每个 Key 是一个 Sorted Set，member 为 `{timestamp}:{random}`，score 为时间戳微秒值。

## 测试

```bash
# 运行所有测试（含竞态检测）
go test -v -race ./...

# 查看覆盖率
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## 依赖

- [go-redis/v9](https://github.com/redis/go-redis) — Redis 客户端
- [miniredis/v2](https://github.com/alicebob/miniredis) — 测试用内存 Redis

## 许可证

与 scrapy-go 主项目保持一致。
