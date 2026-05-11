package redisqueue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/redis/go-redis/v9"
)

// RedisQueue 是基于 Redis Sorted Set 的分布式优先级队列。
//
// 实现 scheduler.PriorityAwareQueue 接口，可通过 scheduler.WithExternalQueue
// 注入到 DefaultScheduler 中，替代默认的磁盘队列。
//
// 设计决策：
//   - 使用 Sorted Set 存储请求数据，score 为优先级（高优先级先出队）
//   - 使用 ZADD 入队，ZPOPMAX 出队，保证多实例并发安全
//   - 相同优先级的请求按 LIFO 顺序出队（通过 score 微调实现）
//   - 序列化格式为 JSON，与 DiskQueue 一致
//
// 对应 Scrapy-Redis 的 PriorityQueue 实现。
type RedisQueue struct {
	client *redis.Client
	opts   *Options
	key    string // 队列的完整 Redis Key

	// closed 标记队列是否已关闭
	closed atomic.Bool

	// mu 保护 client 的关闭操作
	mu sync.RWMutex

	// ownsClient 标记是否拥有 client 的所有权（是否需要在 Close 时关闭）
	ownsClient bool

	// seqCounter 用于相同优先级内的 LIFO 排序。
	// 通过在 score 中编码 priority + 微小增量实现：
	// score = priority * 1e10 + seqCounter（seqCounter 递增保证后入队的 score 更大）
	seqCounter atomic.Int64
}

// NewRedisQueue 创建一个新的 Redis 分布式优先级队列。
//
// 参数：
//   - opts: Redis 配置选项，为 nil 时使用默认配置
//
// 返回创建的队列实例和可能的错误（连接失败等）。
func NewRedisQueue(opts *Options) (*RedisQueue, error) {
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
		return nil, fmt.Errorf("redisqueue: failed to connect to Redis at %s: %w", opts.Addr, err)
	}

	key := opts.queueFullKey()

	rq := &RedisQueue{
		client:     client,
		opts:       opts,
		key:        key,
		ownsClient: true,
	}

	// 如果配置了启动时清空
	if opts.FlushOnStart {
		if err := client.Del(ctx, key).Err(); err != nil {
			client.Close()
			return nil, fmt.Errorf("redisqueue: failed to flush queue on start: %w", err)
		}
	}

	// 初始化 seqCounter：从 Redis 中获取当前队列大小作为起始值，
	// 确保重启后 seq 不会与已有数据冲突
	size, err := client.ZCard(ctx, key).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		client.Close()
		return nil, fmt.Errorf("redisqueue: failed to get queue size: %w", err)
	}
	rq.seqCounter.Store(size)

	return rq, nil
}

// NewRedisQueueFromClient 使用已有的 Redis 客户端创建队列。
//
// 适用于需要共享 Redis 连接的场景（如同时使用 RedisQueue 和 RedisDupeFilter）。
// 注意：Close 时不会关闭传入的 client。
func NewRedisQueueFromClient(client *redis.Client, opts *Options) (*RedisQueue, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	if client == nil {
		return nil, errors.New("redisqueue: client must not be nil")
	}

	key := opts.queueFullKey()

	rq := &RedisQueue{
		client:     client,
		opts:       opts,
		key:        key,
		ownsClient: false,
	}

	// 如果配置了启动时清空
	ctx := context.Background()
	if opts.FlushOnStart {
		if err := client.Del(ctx, key).Err(); err != nil {
			return nil, fmt.Errorf("redisqueue: failed to flush queue on start: %w", err)
		}
	}

	// 初始化 seqCounter
	size, err := client.ZCard(ctx, key).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redisqueue: failed to get queue size: %w", err)
	}
	rq.seqCounter.Store(size)

	return rq, nil
}

// Push 将数据推入默认优先级（0）的队列中。
// 实现 scheduler.Queue 接口。
func (rq *RedisQueue) Push(data []byte) error {
	return rq.PushWithPriority(data, 0)
}

// PushWithPriority 将数据推入指定优先级的队列中。
//
// 使用 Redis Sorted Set 的 ZADD 命令，score 编码优先级和入队顺序：
//   - score = priority * 1e10 + seqCounter
//   - 高优先级的 score 更大，ZPOPMAX 时先出队
//   - 相同优先级内，后入队的 seqCounter 更大，实现 LIFO
//
// 实现 scheduler.PriorityAwareQueue 接口。
func (rq *RedisQueue) PushWithPriority(data []byte, priority int) error {
	if rq.closed.Load() {
		return errors.New("redisqueue: queue is closed")
	}

	rq.mu.RLock()
	defer rq.mu.RUnlock()

	seq := rq.seqCounter.Add(1)
	score := encodeScore(priority, seq)

	ctx := context.Background()
	err := rq.client.ZAdd(ctx, rq.key, redis.Z{
		Score:  score,
		Member: data,
	}).Err()
	if err != nil {
		return fmt.Errorf("redisqueue: failed to push to queue: %w", err)
	}

	return nil
}

// Pop 从队列中弹出最高优先级的数据。
// 实现 scheduler.Queue 接口。
func (rq *RedisQueue) Pop() ([]byte, error) {
	data, _, err := rq.PopWithPriority()
	return data, err
}

// PopWithPriority 从最高优先级的桶中弹出数据。
//
// 使用 Redis 的 ZPOPMAX 命令原子性地弹出 score 最大的元素。
// 返回数据和对应的优先级。
// 如果队列为空，返回 nil, 0, nil。
//
// 实现 scheduler.PriorityAwareQueue 接口。
func (rq *RedisQueue) PopWithPriority() (data []byte, priority int, err error) {
	if rq.closed.Load() {
		return nil, 0, errors.New("redisqueue: queue is closed")
	}

	rq.mu.RLock()
	defer rq.mu.RUnlock()

	ctx := context.Background()
	results, err := rq.client.ZPopMax(ctx, rq.key, 1).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("redisqueue: failed to pop from queue: %w", err)
	}

	if len(results) == 0 {
		return nil, 0, nil
	}

	member := results[0].Member
	score := results[0].Score

	// 解码优先级
	priority = decodeScore(score)

	// member 可能是 string 或 []byte，取决于 go-redis 版本
	switch v := member.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return nil, 0, fmt.Errorf("redisqueue: unexpected member type: %T", member)
	}

	return data, priority, nil
}

// Peek 查看队列头部数据但不弹出。
//
// 使用 ZREVRANGE 获取 score 最大的元素但不移除。
// 如果队列为空，返回 nil, nil。
//
// 实现 scheduler.Queue 接口。
func (rq *RedisQueue) Peek() ([]byte, error) {
	if rq.closed.Load() {
		return nil, errors.New("redisqueue: queue is closed")
	}

	rq.mu.RLock()
	defer rq.mu.RUnlock()

	ctx := context.Background()
	results, err := rq.client.ZRevRangeWithScores(ctx, rq.key, 0, 0).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("redisqueue: failed to peek queue: %w", err)
	}

	if len(results) == 0 {
		return nil, nil
	}

	member := results[0].Member
	switch v := member.(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	default:
		return nil, fmt.Errorf("redisqueue: unexpected member type: %T", member)
	}
}

// Len 返回队列中的元素数量。
//
// 使用 ZCARD 命令获取 Sorted Set 的元素数量。
//
// 实现 scheduler.Queue 接口。
func (rq *RedisQueue) Len() int {
	if rq.closed.Load() {
		return 0
	}

	rq.mu.RLock()
	defer rq.mu.RUnlock()

	ctx := context.Background()
	count, err := rq.client.ZCard(ctx, rq.key).Result()
	if err != nil {
		return 0
	}
	return int(count)
}

// Close 关闭队列，释放 Redis 连接。
//
// 注意：如果队列是通过 NewRedisQueueFromClient 创建的，
// Close 不会关闭传入的 Redis 客户端。
//
// 实现 scheduler.Queue 接口。
func (rq *RedisQueue) Close() error {
	if !rq.closed.CompareAndSwap(false, true) {
		return nil // 已经关闭
	}

	rq.mu.Lock()
	defer rq.mu.Unlock()

	// 仅关闭自己创建的 client
	if rq.ownsClient {
		return rq.client.Close()
	}
	return nil
}

// Client 返回底层的 Redis 客户端。
// 用于高级场景（如自定义 Lua 脚本）。
func (rq *RedisQueue) Client() *redis.Client {
	return rq.client
}

// ============================================================================
// Score 编码/解码
// ============================================================================

// encodeScore 将优先级和序号编码为 Sorted Set 的 score。
//
// 编码规则：score = priority * 1e10 + seq
//   - priority 占高位，保证高优先级的 score 更大
//   - seq 占低位，保证相同优先级内后入队的 score 更大（LIFO）
//   - 1e10 的间隔足够容纳 100 亿个请求的序号
func encodeScore(priority int, seq int64) float64 {
	return float64(priority)*1e10 + float64(seq)
}

// decodeScore 从 score 中解码出优先级。
// 使用 math 向零取整确保负优先级正确解码。
func decodeScore(score float64) int {
	// 对于正数，int() 向零截断即可
	// 对于负数，需要向负无穷取整（floor）
	if score >= 0 {
		return int(score / 1e10)
	}
	// 负数：-49999999950 / 1e10 = -4.999... → 应该是 -5
	// 使用减法修正：先取绝对值计算，再取负
	result := int(score / 1e10)
	if float64(result)*1e10 > score {
		result--
	}
	return result
}

// ============================================================================
// 辅助方法
// ============================================================================

// Stats 返回队列的统计信息。
func (rq *RedisQueue) Stats() map[string]any {
	if rq.closed.Load() {
		return map[string]any{"closed": true}
	}

	ctx := context.Background()
	count, _ := rq.client.ZCard(ctx, rq.key).Result()
	memoryUsage, _ := rq.client.MemoryUsage(ctx, rq.key).Result()

	return map[string]any{
		"key":          rq.key,
		"length":       count,
		"memory_bytes": memoryUsage,
		"seq_counter":  rq.seqCounter.Load(),
	}
}

// Clear 清空队列中的所有数据。
// 用于测试或重置场景。
func (rq *RedisQueue) Clear() error {
	if rq.closed.Load() {
		return errors.New("redisqueue: queue is closed")
	}

	ctx := context.Background()
	return rq.client.Del(ctx, rq.key).Err()
}

// PriorityStats 返回各优先级的请求数量分布。
func (rq *RedisQueue) PriorityStats() (map[int]int64, error) {
	if rq.closed.Load() {
		return nil, errors.New("redisqueue: queue is closed")
	}

	ctx := context.Background()
	// 获取所有元素的 score
	results, err := rq.client.ZRangeWithScores(ctx, rq.key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("redisqueue: failed to get priority stats: %w", err)
	}

	stats := make(map[int]int64)
	for _, z := range results {
		p := decodeScore(z.Score)
		stats[p]++
	}

	return stats, nil
}

// LenByPriority 返回指定优先级的请求数量。
func (rq *RedisQueue) LenByPriority(priority int) (int64, error) {
	if rq.closed.Load() {
		return 0, errors.New("redisqueue: queue is closed")
	}

	ctx := context.Background()
	minScore := strconv.FormatFloat(float64(priority)*1e10, 'f', -1, 64)
	maxScore := strconv.FormatFloat(float64(priority+1)*1e10, 'f', -1, 64)

	count, err := rq.client.ZCount(ctx, rq.key, minScore, "("+maxScore).Result()
	if err != nil {
		return 0, fmt.Errorf("redisqueue: failed to count by priority: %w", err)
	}

	return count, nil
}
