// Package redisqueue 提供基于 Redis 的分布式队列和去重过滤器实现。
//
// 本包是 scrapy-go 框架的可插拔扩展模块（contrib），作为独立的 Go 子模块发布，
// 主模块 go.mod 不引入 Redis 相关依赖，实现零侵入的可插拔设计。
//
// # 核心组件
//
//   - RedisQueue：基于 Redis Sorted Set 的分布式优先级队列，实现 scheduler.PriorityAwareQueue 接口
//   - RedisDupeFilter：基于 Redis Set 的分布式去重过滤器，实现 scheduler.DupeFilter 接口
//
// # 使用方式
//
// 通过 scheduler.WithExternalQueue 和 scheduler.WithDupeFilter 注入到 DefaultScheduler：
//
//	import (
//	    "github.com/dplcz/scrapy-go/contrib/redisqueue"
//	    "github.com/dplcz/scrapy-go/pkg/scheduler"
//	)
//
//	opts := redisqueue.DefaultOptions()
//	opts.Addr = "localhost:6379"
//	opts.KeyPrefix = "scrapy:myspider"
//
//	queue, _ := redisqueue.NewRedisQueue(opts)
//	dupeFilter, _ := redisqueue.NewRedisDupeFilter(opts)
//
//	sched := scheduler.NewDefaultScheduler(
//	    scheduler.WithExternalQueue(queue),
//	    scheduler.WithDupeFilter(dupeFilter),
//	)
//
// # 布隆过滤器加速
//
// RedisDupeFilter 支持可选的本地布隆过滤器作为一级去重缓存。
// 启用后，新请求可跳过 Redis 网络查询，大幅降低延迟和 Redis 负载：
//
//   - 布隆过滤器判断"不存在" → 100% 是新请求，直接写入 Redis 并返回
//   - 布隆过滤器判断"可能存在" → 穿透到 Redis SADD 做精确判断
//   - 正确性完全由 Redis 保证，布隆过滤器仅作为性能优化
//
// 通过配置项启用：
//
//	opts := redisqueue.DefaultOptions()
//	opts.Addr = "localhost:6379"
//	opts.BloomFilterEnabled = true          // 启用布隆过滤器
//	opts.BloomExpectedItems = 5_000_000     // 预估 500 万不重复请求
//	opts.BloomFalsePositiveRate = 0.001     // 0.1% 误判率（~8.55 MB 内存）
//
//	dupeFilter, _ := redisqueue.NewRedisDupeFilter(opts)
//
//	// 运行时查看布隆过滤器统计
//	stats := dupeFilter.BloomStats()
//	// stats["bloom_hits"]     — 布隆过滤器拦截次数（新请求，跳过 Redis 读查询）
//	// stats["bloom_misses"]   — 穿透到 Redis 的次数（可能重复，需精确判断）
//	// stats["bloom_hit_rate"] — 命中率（越高说明 Redis 查询节省越多）
//
// # 分布式特性
//
// 多个爬虫实例可共享同一 Redis 实例，实现分布式爬取：
//   - 多实例共享队列：多个 Worker 从同一队列消费请求
//   - 分布式去重：多实例共享去重集合，避免重复爬取
//   - 断点续爬：Redis 持久化保证队列数据不丢失
//   - 布隆过滤器各实例独立维护，不影响分布式去重正确性
//
// # 设计决策
//
//   - 使用 Sorted Set 按优先级分桶存储（score = priority），支持按优先级出队
//   - 使用 Set 存储请求指纹，实现 O(1) 去重查询
//   - 使用 ZPOPMAX 原子操作出队，保证多实例并发安全
//   - 使用 SADD 原子操作去重，保证多实例一致性
//   - 序列化格式与 DiskQueue 一致（JSON），支持跨后端迁移
//   - 布隆过滤器作为可选本地缓存层，减少 Redis 网络往返
package redisqueue
