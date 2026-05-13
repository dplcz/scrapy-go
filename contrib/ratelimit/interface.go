package ratelimit

import "context"

// RateLimiter 定义分布式限速器的通用接口。
//
// 实现者需保证线程安全，支持多 goroutine 并发调用。
// domain 参数用于按域名独立限速，不同域名拥有独立的速率窗口。
type RateLimiter interface {
	// Allow 检查指定域名的请求是否被允许通过。
	//
	// 返回 true 表示请求被允许（未超过速率限制），
	// 返回 false 表示请求被拒绝（已超过速率限制）。
	//
	// 此方法为非阻塞调用，适用于快速判断场景。
	Allow(domain string) bool

	// Wait 阻塞等待直到指定域名的请求被允许通过。
	//
	// 如果当前速率未超限，立即返回 nil。
	// 如果当前速率已超限，阻塞等待直到有可用配额或 context 取消。
	//
	// 返回 context.Canceled 或 context.DeadlineExceeded 表示等待被取消。
	Wait(ctx context.Context, domain string) error

	// Close 关闭限速器，释放底层资源（如 Redis 连接）。
	Close() error
}
