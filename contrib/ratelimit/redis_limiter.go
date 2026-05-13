package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// slidingWindowScript 是基于 Redis 滑动窗口算法的 Lua 脚本。
//
// 算法原理：
//  1. 使用 Sorted Set 存储每个请求的时间戳（score = 时间戳微秒）
//  2. 每次请求时，先移除窗口外的过期记录
//  3. 统计当前窗口内的请求数量
//  4. 如果未超过限制，添加当前请求并返回 1（允许）
//  5. 如果已超过限制，返回 0（拒绝）
//
// 参数：
//   - KEYS[1]: 限速器的 Redis Key（{prefix}:{domain}）
//   - ARGV[1]: 当前时间戳（微秒）
//   - ARGV[2]: 窗口大小（微秒）
//   - ARGV[3]: 最大请求数（rate）
//   - ARGV[4]: Key 过期时间（秒）
//
// 返回值：
//   - [1, retryAfterMs]: 允许通过，retryAfterMs 为 0
//   - [0, retryAfterMs]: 拒绝，retryAfterMs 为建议的重试等待时间（毫秒）
const slidingWindowScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local expiration = tonumber(ARGV[4])

-- 移除窗口外的过期记录
local window_start = now - window
redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

-- 统计当前窗口内的请求数量
local count = redis.call('ZCARD', key)

if count < limit then
    -- 未超限：添加当前请求，返回允许
    redis.call('ZADD', key, now, now .. ':' .. math.random(1000000))
    redis.call('EXPIRE', key, expiration)
    return {1, 0}
else
    -- 已超限：计算最早记录的过期时间作为重试建议
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local retry_after = 0
    if #oldest >= 2 then
        local oldest_time = tonumber(oldest[2])
        retry_after = math.ceil((oldest_time + window - now) / 1000)
        if retry_after < 0 then
            retry_after = 0
        end
    end
    return {0, retry_after}
end
`

// RedisSlidingWindowLimiter 是基于 Redis 滑动窗口算法的分布式限速器。
//
// 特性：
//   - 使用 Lua 脚本保证原子性，多实例并发安全
//   - 滑动窗口算法，相比固定窗口更平滑
//   - 支持按域名独立限速
//   - 自动清理过期的限速 Key
//
// 线程安全：所有方法均可被多个 goroutine 并发调用。
type RedisSlidingWindowLimiter struct {
	client *redis.Client
	opts   *Options
	script *redis.Script

	// closed 标记限速器是否已关闭
	closed atomic.Bool

	// mu 保护 client 的关闭操作
	mu sync.RWMutex

	// ownsClient 标记是否拥有 client 的所有权
	ownsClient bool
}

// NewRedisSlidingWindowLimiter 创建一个新的 Redis 滑动窗口限速器。
//
// 参数：
//   - opts: 限速器配置选项，为 nil 时使用默认配置
//
// 返回创建的限速器实例和可能的错误（连接失败等）。
func NewRedisSlidingWindowLimiter(opts *Options) (*RedisSlidingWindowLimiter, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	client := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  opts.DialTimeout,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
		PoolSize:     opts.PoolSize,
	})

	// 验证连接
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ratelimit: failed to connect to Redis at %s: %w", opts.Addr, err)
	}

	limiter := &RedisSlidingWindowLimiter{
		client:     client,
		opts:       opts,
		script:     redis.NewScript(slidingWindowScript),
		ownsClient: true,
	}

	return limiter, nil
}

// NewRedisSlidingWindowLimiterFromClient 使用已有的 Redis 客户端创建限速器。
//
// 适用于需要共享 Redis 连接的场景（如同时使用 RedisQueue 和 RateLimiter）。
// 注意：Close 时不会关闭传入的 client。
func NewRedisSlidingWindowLimiterFromClient(client *redis.Client, opts *Options) (*RedisSlidingWindowLimiter, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	if client == nil {
		return nil, errors.New("ratelimit: client must not be nil")
	}

	limiter := &RedisSlidingWindowLimiter{
		client:     client,
		opts:       opts,
		script:     redis.NewScript(slidingWindowScript),
		ownsClient: false,
	}

	return limiter, nil
}

// Allow 检查指定域名的请求是否被允许通过。
//
// 非阻塞调用，立即返回结果。
// 如果限速器已关闭，返回 true（降级策略：不阻塞请求）。
func (l *RedisSlidingWindowLimiter) Allow(domain string) bool {
	if l.closed.Load() {
		return true
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	result, err := l.evalScript(context.Background(), domain)
	if err != nil {
		// Redis 不可用时降级：允许请求通过
		return true
	}

	return result.allowed
}

// Wait 阻塞等待直到指定域名的请求被允许通过。
//
// 如果当前速率未超限，立即返回 nil。
// 如果当前速率已超限，按照 Redis 返回的重试建议时间等待后重试。
//
// 返回 context.Canceled 或 context.DeadlineExceeded 表示等待被取消。
// 如果限速器已关闭，立即返回 nil（降级策略）。
func (l *RedisSlidingWindowLimiter) Wait(ctx context.Context, domain string) error {
	if l.closed.Load() {
		return nil
	}

	// 如果 context 没有 deadline，使用默认超时
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.opts.WaitTimeout)
		defer cancel()
	}

	for {
		l.mu.RLock()
		result, err := l.evalScript(ctx, domain)
		l.mu.RUnlock()

		if err != nil {
			// Redis 不可用时降级：允许请求通过
			return nil
		}

		if result.allowed {
			return nil
		}

		// 计算等待时间
		waitDuration := time.Duration(result.retryAfterMs) * time.Millisecond
		if waitDuration <= 0 {
			// 最小等待时间，避免忙等
			waitDuration = 10 * time.Millisecond
		}

		// 等待或被取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
			// 重试
		}
	}
}

// Close 关闭限速器，释放 Redis 连接。
//
// 如果限速器是通过 NewRedisSlidingWindowLimiterFromClient 创建的，
// Close 不会关闭传入的 Redis 客户端。
func (l *RedisSlidingWindowLimiter) Close() error {
	if !l.closed.CompareAndSwap(false, true) {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.ownsClient {
		return l.client.Close()
	}
	return nil
}

// Client 返回底层的 Redis 客户端。
// 用于高级场景（如与 redisqueue 共享连接）。
func (l *RedisSlidingWindowLimiter) Client() *redis.Client {
	return l.client
}

// scriptResult 存储 Lua 脚本的执行结果。
type scriptResult struct {
	allowed      bool
	retryAfterMs int64
}

// evalScript 执行限速 Lua 脚本。
func (l *RedisSlidingWindowLimiter) evalScript(ctx context.Context, domain string) (scriptResult, error) {
	key := l.opts.keyForDomain(domain)
	rate := l.opts.rateForDomain(domain)
	now := time.Now().UnixMicro()
	windowMicro := l.opts.Window.Microseconds()
	expirationSec := int64(l.opts.KeyExpiration.Seconds())

	// 使用突发容量作为实际限制（如果配置了的话）
	limit := rate
	if l.opts.DefaultBurst > rate {
		limit = l.opts.DefaultBurst
	}
	// 如果域名有独立配置，使用域名的速率（不使用全局 burst）
	if l.opts.DomainRates != nil {
		if _, ok := l.opts.DomainRates[domain]; ok {
			limit = rate
		}
	}

	result, err := l.script.Run(ctx, l.client, []string{key},
		now, windowMicro, limit, expirationSec,
	).Int64Slice()
	if err != nil {
		return scriptResult{}, fmt.Errorf("ratelimit: script execution failed: %w", err)
	}

	if len(result) < 2 {
		return scriptResult{}, errors.New("ratelimit: unexpected script result")
	}

	return scriptResult{
		allowed:      result[0] == 1,
		retryAfterMs: result[1],
	}, nil
}

// Stats 返回限速器的统计信息。
func (l *RedisSlidingWindowLimiter) Stats(ctx context.Context, domain string) (map[string]any, error) {
	if l.closed.Load() {
		return map[string]any{"closed": true}, nil
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	key := l.opts.keyForDomain(domain)
	now := time.Now().UnixMicro()
	windowStart := now - l.opts.Window.Microseconds()

	// 获取当前窗口内的请求数
	count, err := l.client.ZCount(ctx, key, fmt.Sprintf("%d", windowStart), fmt.Sprintf("%d", now)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("ratelimit: failed to get stats: %w", err)
	}

	rate := l.opts.rateForDomain(domain)

	return map[string]any{
		"domain":          domain,
		"key":             key,
		"current_count":   count,
		"rate_limit":      rate,
		"window":          l.opts.Window.String(),
		"remaining":       max(0, int64(rate)-count),
		"utilization_pct": float64(count) / float64(rate) * 100,
	}, nil
}

// Reset 重置指定域名的限速计数器。
func (l *RedisSlidingWindowLimiter) Reset(ctx context.Context, domain string) error {
	if l.closed.Load() {
		return errors.New("ratelimit: limiter is closed")
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	key := l.opts.keyForDomain(domain)
	return l.client.Del(ctx, key).Err()
}
