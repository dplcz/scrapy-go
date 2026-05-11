package redisqueue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/bits-and-blooms/bloom/v3"
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
//   - 支持可选的本地布隆过滤器一级缓存，减少 Redis 查询量
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

	// bf 是本地布隆过滤器（可选，由 BloomFilterEnabled 控制）。
	// 作为一级去重缓存，在查询 Redis 之前先本地判断：
	//   - 布隆过滤器判断"不存在" → 100% 是新请求，跳过 Redis 读查询
	//   - 布隆过滤器判断"可能存在" → 穿透到 Redis 做精确判断
	bf   *bloom.BloomFilter
	bfMu sync.Mutex // 保护布隆过滤器的并发写入

	// 布隆过滤器统计
	bloomHits   atomic.Int64 // 布隆过滤器拦截次数（新请求，避免了 Redis 读查询）
	bloomMisses atomic.Int64 // 穿透到 Redis 的次数（可能存在，需精确判断）
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
	df.initBloomFilter()

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
	df.initBloomFilter()

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
// 当启用布隆过滤器时，处理流程：
//  1. 计算请求指纹（锁外，纯 CPU 操作）
//  2. 查询本地布隆过滤器：
//     - "不存在" → 100% 新请求，写入 Redis + 布隆过滤器，返回 false
//     - "可能存在" → 穿透到 Redis SADD 做精确判断
//
// 未启用布隆过滤器时，直接使用 Redis SADD 原子操作。
//
// 实现 scheduler.DupeFilter 接口。
func (df *RedisDupeFilter) RequestSeen(request *shttp.Request) bool {
	if df.closed.Load() {
		return false
	}

	// 指纹计算是纯 CPU 操作，不需要锁保护，移到锁外以减少锁持有时间
	fp := df.computeFingerprint(request)

	// 布隆过滤器一级缓存
	if df.bf != nil {
		return df.requestSeenWithBloom(fp)
	}

	return df.requestSeenDirect(fp)
}

// requestSeenDirect 直接通过 Redis SADD 判断请求是否已见过。
func (df *RedisDupeFilter) requestSeenDirect(fp string) bool {
	df.mu.RLock()
	defer df.mu.RUnlock()

	ctx := context.Background()
	added, err := df.client.SAdd(ctx, df.key, fp).Result()
	if err != nil {
		// Redis 错误时保守处理：视为未见过，允许请求通过
		return false
	}

	// SADD 返回 0 表示元素已存在（重复请求）
	return added == 0
}

// requestSeenWithBloom 通过本地布隆过滤器 + Redis 二级去重判断请求是否已见过。
//
// 布隆过滤器特性：
//   - 判断"不存在"时 100% 准确，可直接确认为新请求
//   - 判断"可能存在"时有误判率，需穿透到 Redis 精确判断
//
// 正确性完全由 Redis 保证，布隆过滤器仅作为性能优化。
func (df *RedisDupeFilter) requestSeenWithBloom(fp string) bool {
	fpBytes := []byte(fp)

	// 查询并更新布隆过滤器（TestAndAdd 是原子操作，但需要锁保护并发写入）
	df.bfMu.Lock()
	mightExist := df.bf.TestAndAdd(fpBytes)
	df.bfMu.Unlock()

	if !mightExist {
		// 布隆过滤器确认"不存在" → 100% 是新请求
		// 仍需写入 Redis 保证多机共享去重状态，
		// 并检查返回值确保并发正确性
		df.mu.RLock()
		ctx := context.Background()
		added, err := df.client.SAdd(ctx, df.key, fp).Result()
		df.mu.RUnlock()

		df.bloomHits.Add(1)

		if err != nil {
			return false
		}
		// 如果 Redis 返回 0，说明其他实例已写入（多机场景），视为重复
		return added == 0
	}

	// 布隆过滤器说"可能存在" → 穿透到 Redis 精确判断
	df.bloomMisses.Add(1)
	return df.requestSeenDirect(fp)
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

	// 指纹计算是纯 CPU 操作，不需要锁保护，移到锁外以减少锁持有时间
	fp := df.computeFingerprint(request)

	df.mu.RLock()
	defer df.mu.RUnlock()

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

// BloomStats 返回布隆过滤器的统计信息。
// 如果未启用布隆过滤器，返回 nil。
func (df *RedisDupeFilter) BloomStats() map[string]any {
	if df.bf == nil {
		return nil
	}

	hits := df.bloomHits.Load()
	misses := df.bloomMisses.Load()
	total := hits + misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return map[string]any{
		"enabled":             true,
		"expected_items":      df.opts.BloomExpectedItems,
		"false_positive_rate": df.opts.BloomFalsePositiveRate,
		"bloom_hits":          hits,
		"bloom_misses":        misses,
		"bloom_hit_rate":      hitRate,
	}
}

// initBloomFilter 根据配置初始化本地布隆过滤器。
func (df *RedisDupeFilter) initBloomFilter() {
	if !df.opts.BloomFilterEnabled {
		return
	}

	expected := df.opts.BloomExpectedItems
	if expected == 0 {
		expected = 1_000_000
	}
	fpRate := df.opts.BloomFalsePositiveRate
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.001
	}

	df.bf = bloom.NewWithEstimates(expected, fpRate)
}
