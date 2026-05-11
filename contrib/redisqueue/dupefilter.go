package redisqueue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/redis/go-redis/v9"
)

// RedisDupeFilter 是基于 Redis Set 的分布式去重过滤器。
//
// 实现 scheduler.DupeFilter 接口，可通过 scheduler.WithDupeFilter
// 注入到 DefaultScheduler 中，替代默认的内存去重过滤器。
//
// 设计决策：
//   - 使用 Redis Set 存储请求指纹（SHA1 哈希），O(1) 查重
//   - 使用 SADD 原子操作，多实例并发安全
//   - 指纹计算逻辑与 RFPDupeFilter 一致（URL + Method + Body 的 SHA1）
//   - 支持 FlushOnStart 配置，控制是否在启动时清空去重集合
//
// 多实例共享同一 Redis 时，去重集合自动共享，实现分布式去重。
//
// 对应 Scrapy-Redis 的 RFPDupeFilter 实现。
type RedisDupeFilter struct {
	client *redis.Client
	opts   *Options
	key    string // 去重集合的完整 Redis Key

	// closed 标记过滤器是否已关闭
	closed atomic.Bool

	// mu 保护 client 的关闭操作
	mu sync.RWMutex

	// ownsClient 标记是否拥有 client 的所有权（是否需要在 Close 时关闭）
	ownsClient bool

	// debug 是否输出调试日志
	debug bool

	// logDupes 是否已输出过重复日志（仅输出一次提示）
	logDupes atomic.Bool
}

// NewRedisDupeFilter 创建一个新的 Redis 分布式去重过滤器。
//
// 参数：
//   - opts: Redis 配置选项，为 nil 时使用默认配置
//
// 返回创建的过滤器实例和可能的错误（连接失败等）。
func NewRedisDupeFilter(opts *Options) (*RedisDupeFilter, error) {
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
		return nil, fmt.Errorf("redisqueue: dupefilter failed to connect to Redis at %s: %w", opts.Addr, err)
	}

	df := &RedisDupeFilter{
		client:     client,
		opts:       opts,
		key:        opts.dupeFilterFullKey(),
		ownsClient: true,
	}
	df.logDupes.Store(true)

	return df, nil
}

// NewRedisDupeFilterFromClient 使用已有的 Redis 客户端创建去重过滤器。
//
// 适用于需要共享 Redis 连接的场景。
// 注意：Close 时不会关闭传入的 client。
func NewRedisDupeFilterFromClient(client *redis.Client, opts *Options) (*RedisDupeFilter, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	if client == nil {
		return nil, errors.New("redisqueue: client must not be nil")
	}

	df := &RedisDupeFilter{
		client:     client,
		opts:       opts,
		key:        opts.dupeFilterFullKey(),
		ownsClient: false,
	}
	df.logDupes.Store(true)

	return df, nil
}

// Open 初始化去重过滤器。
//
// 如果配置了 FlushOnStart，会清空 Redis 中的去重集合。
//
// 实现 scheduler.DupeFilter 接口。
func (df *RedisDupeFilter) Open(ctx context.Context) error {
	if df.opts.FlushOnStart {
		if err := df.client.Del(ctx, df.key).Err(); err != nil {
			return fmt.Errorf("redisqueue: failed to flush dupefilter on start: %w", err)
		}
	}
	return nil
}

// Close 关闭去重过滤器。
//
// 如果过滤器拥有 Redis 客户端的所有权，会关闭连接。
//
// 实现 scheduler.DupeFilter 接口。
func (df *RedisDupeFilter) Close(reason string) error {
	if !df.closed.CompareAndSwap(false, true) {
		return nil // 已经关闭
	}

	df.mu.Lock()
	defer df.mu.Unlock()

	if df.ownsClient {
		return df.client.Close()
	}
	return nil
}

// RequestSeen 检查请求是否已见过。
//
// 使用 SADD 命令原子性地尝试将请求指纹添加到 Redis Set 中：
//   - 如果指纹不存在，SADD 返回 1（新请求），记录并返回 false
//   - 如果指纹已存在，SADD 返回 0（重复请求），返回 true
//
// 多实例并发调用时，Redis 保证 SADD 的原子性，不会出现重复爬取。
//
// 实现 scheduler.DupeFilter 接口。
func (df *RedisDupeFilter) RequestSeen(request *shttp.Request) bool {
	if df.closed.Load() {
		return false
	}

	df.mu.RLock()
	defer df.mu.RUnlock()

	fp := df.computeFingerprint(request)

	ctx := context.Background()
	added, err := df.client.SAdd(ctx, df.key, fp).Result()
	if err != nil {
		// Redis 错误时保守处理：视为未见过，允许请求通过
		return false
	}

	// SADD 返回 0 表示元素已存在（重复请求）
	return added == 0
}

// computeFingerprint 计算请求的指纹。
// 使用与 RFPDupeFilter 相同的指纹算法，确保兼容性。
func (df *RedisDupeFilter) computeFingerprint(request *shttp.Request) string {
	return requestFingerprint(request)
}

// SeenCount 返回去重集合中的指纹数量。
func (df *RedisDupeFilter) SeenCount() int {
	if df.closed.Load() {
		return 0
	}

	ctx := context.Background()
	count, err := df.client.SCard(ctx, df.key).Result()
	if err != nil {
		return 0
	}
	return int(count)
}

// Clear 清空去重集合。
// 用于测试或重置场景。
func (df *RedisDupeFilter) Clear() error {
	if df.closed.Load() {
		return errors.New("redisqueue: dupefilter is closed")
	}

	ctx := context.Background()
	return df.client.Del(ctx, df.key).Err()
}

// Contains 检查指定请求的指纹是否存在于去重集合中。
// 与 RequestSeen 不同，此方法不会将指纹添加到集合中。
func (df *RedisDupeFilter) Contains(request *shttp.Request) bool {
	if df.closed.Load() {
		return false
	}

	df.mu.RLock()
	defer df.mu.RUnlock()

	fp := df.computeFingerprint(request)

	ctx := context.Background()
	exists, err := df.client.SIsMember(ctx, df.key, fp).Result()
	if err != nil {
		return false
	}
	return exists
}

// Client 返回底层的 Redis 客户端。
func (df *RedisDupeFilter) Client() *redis.Client {
	return df.client
}
