# scrapy-go/contrib/redisqueue

[![Go Version](https://img.shields.io/badge/Go-1.25.1+-00ADD8?style=flat&logo=go)](https://go.dev/)

基于 Redis 的分布式队列和去重过滤器扩展，为 scrapy-go 提供分布式爬取能力。

## 特性

- 🔌 **可插拔设计** — 通过 `WithExternalQueue` / `WithDupeFilter` 注入，主模块零 Redis 依赖
- 🌐 **分布式爬取** — 多实例共享队列和去重集合，实现分布式协作
- ⚡ **高性能** — 基于 Redis Sorted Set + Set，O(log N) 入队，O(1) 出队和去重
- 🌸 **布隆过滤器加速** — 可选本地布隆过滤器一级缓存，新请求跳过 Redis 查询，大幅减少网络往返
- 💾 **断点续爬** — Redis 持久化保证数据不丢失，重启后自动恢复
- 🔒 **并发安全** — 使用 Redis 原子命令（ZADD/ZPOPMAX/SADD），多实例无锁协作
- 📊 **优先级支持** — 基于 Sorted Set score 编码优先级，高优先级请求先处理

## 安装

```bash
go get github.com/dplcz/scrapy-go/contrib/redisqueue
```

## 快速开始

```go
package main

import (
    "context"

    "github.com/dplcz/scrapy-go/contrib/redisqueue"
    "github.com/dplcz/scrapy-go/pkg/crawler"
    "github.com/dplcz/scrapy-go/pkg/scheduler"
    "github.com/dplcz/scrapy-go/pkg/spider"
)

func main() {
    // 配置 Redis 连接
    opts := redisqueue.DefaultOptions()
    opts.Addr = "localhost:6379"
    opts.KeyPrefix = "scrapy:myspider"

    // 创建 Redis 队列和去重过滤器
    queue, err := redisqueue.NewRedisQueue(opts)
    if err != nil {
        panic(err)
    }

    dupeFilter, err := redisqueue.NewRedisDupeFilter(opts)
    if err != nil {
        panic(err)
    }

    // 注入到调度器
    c := crawler.New(
        crawler.WithScheduler(scheduler.NewDefaultScheduler(
            scheduler.WithExternalQueue(queue),
            scheduler.WithDupeFilter(dupeFilter),
        )),
    )

    ctx := context.Background()
    c.Run(ctx, NewMySpider())
}
```

## 配置选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `Addr` | `localhost:6379` | Redis 服务器地址 |
| `Password` | `""` | Redis 认证密码 |
| `DB` | `0` | Redis 数据库编号 |
| `KeyPrefix` | `scrapy-go` | 所有 Key 的前缀 |
| `QueueKey` | `queue` | 队列 Key 后缀 |
| `DupeFilterKey` | `dupefilter` | 去重集合 Key 后缀 |
| `DialTimeout` | `5s` | 连接超时 |
| `ReadTimeout` | `3s` | 读超时 |
| `WriteTimeout` | `3s` | 写超时 |
| `PoolSize` | `10` | 连接池大小 |
| `FlushOnStart` | `false` | 启动时是否清空数据 |
| `Serializer` | `json` | 序列化格式 |
| `BloomFilterEnabled` | `false` | 是否启用本地布隆过滤器一级缓存 |
| `BloomExpectedItems` | `1000000` | 布隆过滤器预估不重复请求数 |
| `BloomFalsePositiveRate` | `0.001` | 布隆过滤器误判率（0.1%） |

## 布隆过滤器加速

启用本地布隆过滤器后，新请求可跳过 Redis 读查询，大幅降低延迟：

```go
opts := redisqueue.DefaultOptions()
opts.Addr = "localhost:6379"
opts.BloomFilterEnabled = true          // 启用布隆过滤器
opts.BloomExpectedItems = 5_000_000     // 预估 500 万不重复请求
opts.BloomFalsePositiveRate = 0.001     // 0.1% 误判率

df, _ := redisqueue.NewRedisDupeFilter(opts)

// 运行时查看布隆过滤器统计
stats := df.BloomStats()
// {"enabled": true, "bloom_hits": 95000, "bloom_misses": 5000, "bloom_hit_rate": 0.95}
```

**工作原理：**

```
RequestSeen(request)
  │
  ├─ 计算指纹 fp（锁外，纯 CPU）
  │
  ├─ 布隆过滤器.TestAndAdd(fp)
  │   ├─ "不存在"(100%准确) → Redis SADD + 返回
  │   └─ "可能存在"(有误判) → 穿透到 Redis SADD 精确判断
  │
  └─ 正确性完全由 Redis 保证
```

**内存占用参考：**

| 预估请求量 | 误判率 | 内存占用 |
|-----------|--------|--------|
| 100 万 | 0.1% | ~1.71 MB |
| 500 万 | 0.1% | ~8.55 MB |
| 1000 万 | 0.1% | ~17.1 MB |

## 分布式爬取

多个爬虫实例可共享同一 Redis，实现分布式协作：

```go
// Worker 1
opts := redisqueue.DefaultOptions()
opts.Addr = "redis-cluster:6379"
opts.KeyPrefix = "scrapy:distributed-spider"

queue1, _ := redisqueue.NewRedisQueue(opts)
df1, _ := redisqueue.NewRedisDupeFilter(opts)

// Worker 2（相同配置）
queue2, _ := redisqueue.NewRedisQueue(opts)
df2, _ := redisqueue.NewRedisDupeFilter(opts)

// 两个 Worker 共享队列和去重集合，自动协作
```

## Redis Key 结构

```
{KeyPrefix}:{QueueKey}       → Sorted Set（请求队列）
{KeyPrefix}:{DupeFilterKey}  → Set（去重指纹集合）
{KeyPrefix}:{StartURLsKey}   → List（起始 URL，可选）
```

## 共享 Redis 客户端

如果需要在队列和去重过滤器之间共享 Redis 连接：

```go
import "github.com/redis/go-redis/v9"

client := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

queue, _ := redisqueue.NewRedisQueueFromClient(client, opts)
df, _ := redisqueue.NewRedisDupeFilterFromClient(client, opts)

// 使用完毕后手动关闭 client
defer client.Close()
```

## 设计说明

### Score 编码

队列使用 Redis Sorted Set，score 编码规则：

```
score = priority × 10^10 + sequence_number
```

- `priority` 占高位，保证高优先级先出队
- `sequence_number` 占低位，保证相同优先级内 LIFO（后进先出）
- ZPOPMAX 弹出 score 最大的元素，实现优先级 + LIFO 语义

### 与 DiskQueue 的对比

| 特性 | DiskQueue | RedisQueue |
|------|-----------|------------|
| 存储位置 | 本地文件系统 | Redis 服务器 |
| 分布式 | ❌ 单机 | ✅ 多实例共享 |
| 持久化 | 文件系统 | Redis RDB/AOF |
| 性能 | 受磁盘 IO 限制 | 内存级速度 |
| 依赖 | 无外部依赖 | 需要 Redis 服务 |
| 适用场景 | 单机断点续爬 | 分布式爬取 |

## 依赖

- [go-redis/v9](https://github.com/redis/go-redis) — Redis 客户端
- [bits-and-blooms/bloom/v3](https://github.com/bits-and-blooms/bloom) — 布隆过滤器（可选，仅开启 BloomFilterEnabled 时使用）
- [miniredis/v2](https://github.com/alicebob/miniredis) — 测试用内存 Redis（仅测试依赖）

## License

MIT
