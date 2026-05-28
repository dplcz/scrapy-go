package proxy

import (
	"context"
	"errors"
)

// 错误定义。
var (
	// ErrNoProxy 表示代理池中没有可用代理。
	// Pool.Get 在所有代理均不健康或池为空时返回此错误。
	ErrNoProxy = errors.New("proxy pool: no available proxy")

	// ErrInvalidProxy 表示代理 URL 格式非法。
	ErrInvalidProxy = errors.New("proxy pool: invalid proxy URL")

	// ErrPoolClosed 表示代理池已被关闭。
	ErrPoolClosed = errors.New("proxy pool: closed")
)

// Pool 定义代理池的对外接口。
//
// 实现者必须保证所有方法均可被多个 goroutine 并发调用。
// 典型工作流：
//
//  1. 通过 Provider 加载初始代理列表
//  2. 中间件每次 ProcessRequest 调用 Get 获取一个代理
//  3. 请求完成后调用 Mark(proxy, success) 反馈结果
//  4. HealthChecker 后台周期性调用 Refresh 触发健康检查
//  5. 应用退出时调用 Close 释放资源
type Pool interface {
	// Get 选取一个代理。
	// 当所有代理都不健康或池为空时返回 ErrNoProxy。
	// 当 ctx 被取消时返回 ctx.Err()。
	Get(ctx context.Context) (*Proxy, error)

	// Mark 反馈一次代理使用结果。
	// success=true 表示请求成功，失败计数会重置；
	// success=false 表示失败，失败计数加 1，达到阈值后状态会被自动调整。
	Mark(proxy *Proxy, success bool)

	// Refresh 触发一次代理列表刷新（从所有 Provider 重新拉取）。
	// 同步操作，可能阻塞，建议由 HealthChecker 后台调用。
	Refresh(ctx context.Context) error

	// Snapshots 返回当前所有代理的只读快照，便于监控。
	Snapshots() []Snapshot

	// Size 返回池中代理总数（不区分状态）。
	Size() int

	// Healthy 返回当前健康代理数量（StateHealthy）。
	Healthy() int

	// Close 关闭代理池，停止健康检查后台 goroutine 并释放资源。
	// 重复调用安全。
	Close() error
}

// Strategy 定义代理选择策略。
//
// Strategy 仅负责"在一组健康代理中选择一个"的决策，
// 不持有任何代理状态，由 Pool 负责传入候选列表。
//
// 实现者必须保证 Select 是线程安全的。
type Strategy interface {
	// Select 从候选代理列表中挑选一个返回。
	// candidates 由 Pool 过滤后传入，已保证全部为健康可用代理。
	// 实现者不得修改 candidates 切片。
	//
	// 当 candidates 为空时应返回 nil（Pool 会处理 ErrNoProxy 语义）。
	Select(candidates []*Proxy) *Proxy

	// Name 返回策略名称，用于日志和监控。
	Name() string
}

// Provider 定义代理来源接口。
//
// Provider 负责将外部代理列表（静态列表、文件、HTTP API 等）注入到 Pool。
// 实现者应保证 Fetch 是线程安全的，可被多次调用以触发刷新。
type Provider interface {
	// Fetch 拉取代理列表，返回原始代理 URL 字符串切片。
	// 每个 URL 应为完整的代理地址，例如：
	//   "http://user:pass@proxy.example.com:8080"
	//
	// 当 ctx 被取消时应尽快返回错误。
	Fetch(ctx context.Context) ([]string, error)

	// Name 返回 Provider 的可读名称，用于日志记录。
	Name() string
}
