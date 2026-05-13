// Package ratelimit 提供分布式限速器实现，用于控制爬虫的请求速率。
//
// 本模块是 scrapy-go 框架的独立 contrib 扩展，提供基于 Redis 的分布式限速能力，
// 适用于多实例部署场景下的全局请求速率控制。
//
// # 核心组件
//
//   - RateLimiter 接口：定义限速器的通用行为（Allow/Wait）
//   - RedisSlidingWindowLimiter：基于 Redis Lua 脚本的滑动窗口限速器实现
//   - RateLimitExtension：框架扩展，监听 RequestReachedDownloader 信号实现自动限速
//
// # 设计决策
//
//   - 使用 Redis 滑动窗口算法，相比固定窗口更平滑，避免窗口边界突发
//   - 通过 Lua 脚本保证原子性，避免竞态条件
//   - 支持按域名（domain）独立限速，不同域名可配置不同速率
//   - 与 contrib/redisqueue 共享 Redis 连接配置模式，降低使用成本
//
// # 使用示例
//
//	opts := ratelimit.DefaultOptions()
//	opts.Addr = "localhost:6379"
//	opts.DefaultRate = 10  // 每秒 10 个请求
//	opts.DefaultBurst = 20 // 突发容量 20
//
//	limiter, err := ratelimit.NewRedisSlidingWindowLimiter(opts)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer limiter.Close()
//
//	ext := ratelimit.NewRateLimitExtension(limiter, signals, nil)
//	// 注册到 Crawler 的扩展系统
package ratelimit
