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
// # 分布式特性
//
// 多个爬虫实例可共享同一 Redis 实例，实现分布式爬取：
//   - 多实例共享队列：多个 Worker 从同一队列消费请求
//   - 分布式去重：多实例共享去重集合，避免重复爬取
//   - 断点续爬：Redis 持久化保证队列数据不丢失
//
// # 设计决策
//
//   - 使用 Sorted Set 按优先级分桶存储（score = priority），支持按优先级出队
//   - 使用 Set 存储请求指纹，实现 O(1) 去重查询
//   - 使用 ZPOPMAX 原子操作出队，保证多实例并发安全
//   - 使用 SADD 原子操作去重，保证多实例一致性
//   - 序列化格式与 DiskQueue 一致（JSON），支持跨后端迁移
package redisqueue
