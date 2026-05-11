package redisqueue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/redis/go-redis/v9"
)

// ============================================================================
// 测试辅助函数
// ============================================================================

// setupMiniredis 创建一个 miniredis 实例用于测试。
func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *Options) {
	t.Helper()
	mr := miniredis.RunT(t)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "test"

	return mr, opts
}

// setupRedisClient 创建一个连接到 miniredis 的 Redis 客户端。
func setupRedisClient(t *testing.T, mr *miniredis.Miniredis) *redis.Client {
	t.Helper()
	return redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
}

// ============================================================================
// RedisQueue 测试
// ============================================================================

func TestRedisQueue_PushAndPop(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	// 测试 Push
	data := []byte(`{"url":"http://example.com","method":"GET"}`)
	if err := queue.Push(data); err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// 验证长度
	if queue.Len() != 1 {
		t.Errorf("expected Len() = 1, got %d", queue.Len())
	}

	// 测试 Pop
	result, err := queue.Pop()
	if err != nil {
		t.Fatalf("Pop failed: %v", err)
	}
	if string(result) != string(data) {
		t.Errorf("expected %s, got %s", data, result)
	}

	// 验证空队列
	if queue.Len() != 0 {
		t.Errorf("expected Len() = 0 after pop, got %d", queue.Len())
	}
}

func TestRedisQueue_PushWithPriority(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	// 推入不同优先级的数据
	low := []byte(`{"url":"http://low.com","priority":0}`)
	mid := []byte(`{"url":"http://mid.com","priority":5}`)
	high := []byte(`{"url":"http://high.com","priority":10}`)

	if err := queue.PushWithPriority(low, 0); err != nil {
		t.Fatalf("PushWithPriority(0) failed: %v", err)
	}
	if err := queue.PushWithPriority(high, 10); err != nil {
		t.Fatalf("PushWithPriority(10) failed: %v", err)
	}
	if err := queue.PushWithPriority(mid, 5); err != nil {
		t.Fatalf("PushWithPriority(5) failed: %v", err)
	}

	// 验证按优先级出队（高优先级先出）
	data, priority, err := queue.PopWithPriority()
	if err != nil {
		t.Fatalf("PopWithPriority failed: %v", err)
	}
	if priority != 10 {
		t.Errorf("expected priority 10, got %d", priority)
	}
	if string(data) != string(high) {
		t.Errorf("expected high priority data, got %s", data)
	}

	data, priority, err = queue.PopWithPriority()
	if err != nil {
		t.Fatalf("PopWithPriority failed: %v", err)
	}
	if priority != 5 {
		t.Errorf("expected priority 5, got %d", priority)
	}
	if string(data) != string(mid) {
		t.Errorf("expected mid priority data, got %s", data)
	}

	data, priority, err = queue.PopWithPriority()
	if err != nil {
		t.Fatalf("PopWithPriority failed: %v", err)
	}
	if priority != 0 {
		t.Errorf("expected priority 0, got %d", priority)
	}
	if string(data) != string(low) {
		t.Errorf("expected low priority data, got %s", data)
	}
}

func TestRedisQueue_LIFO_SamePriority(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	// 推入相同优先级的数据
	first := []byte(`{"url":"http://first.com"}`)
	second := []byte(`{"url":"http://second.com"}`)
	third := []byte(`{"url":"http://third.com"}`)

	queue.PushWithPriority(first, 0)
	queue.PushWithPriority(second, 0)
	queue.PushWithPriority(third, 0)

	// LIFO：后入队的先出
	data, _, _ := queue.PopWithPriority()
	if string(data) != string(third) {
		t.Errorf("expected third (LIFO), got %s", data)
	}

	data, _, _ = queue.PopWithPriority()
	if string(data) != string(second) {
		t.Errorf("expected second (LIFO), got %s", data)
	}

	data, _, _ = queue.PopWithPriority()
	if string(data) != string(first) {
		t.Errorf("expected first (LIFO), got %s", data)
	}
}

func TestRedisQueue_Peek(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	// 空队列 Peek
	data, err := queue.Peek()
	if err != nil {
		t.Fatalf("Peek on empty queue failed: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil on empty queue, got %s", data)
	}

	// 推入数据后 Peek
	expected := []byte(`{"url":"http://example.com"}`)
	queue.Push(expected)

	data, err = queue.Peek()
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if string(data) != string(expected) {
		t.Errorf("expected %s, got %s", expected, data)
	}

	// Peek 不应移除数据
	if queue.Len() != 1 {
		t.Errorf("Peek should not remove data, Len() = %d", queue.Len())
	}
}

func TestRedisQueue_EmptyPop(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	// 空队列 Pop
	data, err := queue.Pop()
	if err != nil {
		t.Fatalf("Pop on empty queue failed: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil on empty queue, got %s", data)
	}

	// 空队列 PopWithPriority
	data, priority, err := queue.PopWithPriority()
	if err != nil {
		t.Fatalf("PopWithPriority on empty queue failed: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil on empty queue, got %s", data)
	}
	if priority != 0 {
		t.Errorf("expected priority 0 on empty queue, got %d", priority)
	}
}

func TestRedisQueue_Close(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}

	// 关闭队列
	if err := queue.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 关闭后操作应返回错误
	if err := queue.Push([]byte("test")); err == nil {
		t.Error("expected error on Push after Close")
	}

	data, err := queue.Pop()
	if err == nil {
		t.Error("expected error on Pop after Close")
	}
	if data != nil {
		t.Error("expected nil data on Pop after Close")
	}

	// 重复关闭不应报错
	if err := queue.Close(); err != nil {
		t.Errorf("double Close should not error, got: %v", err)
	}
}

func TestRedisQueue_FlushOnStart(t *testing.T) {
	mr, opts := setupMiniredis(t)

	// 先推入一些数据
	client := setupRedisClient(t, mr)
	defer client.Close()

	ctx := context.Background()
	client.ZAdd(ctx, opts.queueFullKey(), redis.Z{Score: 1, Member: "old_data"})

	// 使用 FlushOnStart 创建队列
	opts.FlushOnStart = true
	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue with FlushOnStart failed: %v", err)
	}
	defer queue.Close()

	// 验证旧数据已被清空
	if queue.Len() != 0 {
		t.Errorf("expected empty queue after FlushOnStart, got Len() = %d", queue.Len())
	}
}

func TestRedisQueue_FromClient(t *testing.T) {
	mr, opts := setupMiniredis(t)

	client := setupRedisClient(t, mr)
	defer client.Close()

	queue, err := NewRedisQueueFromClient(client, opts)
	if err != nil {
		t.Fatalf("NewRedisQueueFromClient failed: %v", err)
	}

	// 正常操作
	if err := queue.Push([]byte("test")); err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	if queue.Len() != 1 {
		t.Errorf("expected Len() = 1, got %d", queue.Len())
	}

	// Close 不应关闭共享的 client
	queue.Close()

	// client 仍然可用
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Errorf("shared client should still be usable after queue.Close(), got: %v", err)
	}
}

func TestRedisQueue_NilClient(t *testing.T) {
	opts := DefaultOptions()
	_, err := NewRedisQueueFromClient(nil, opts)
	if err == nil {
		t.Error("expected error when client is nil")
	}
}

func TestRedisQueue_ConcurrentPushPop(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	const numGoroutines = 10
	const numOps = 100

	var wg sync.WaitGroup

	// 并发 Push
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				data := []byte(fmt.Sprintf(`{"id":%d,"seq":%d}`, id, j))
				if err := queue.PushWithPriority(data, id); err != nil {
					t.Errorf("concurrent Push failed: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	// 验证总数
	expectedLen := numGoroutines * numOps
	if queue.Len() != expectedLen {
		t.Errorf("expected Len() = %d, got %d", expectedLen, queue.Len())
	}

	// 并发 Pop
	var popCount int64
	var mu sync.Mutex
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				data, err := queue.Pop()
				if err != nil {
					t.Errorf("concurrent Pop failed: %v", err)
					return
				}
				if data != nil {
					mu.Lock()
					popCount++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	if popCount != int64(expectedLen) {
		t.Errorf("expected %d pops, got %d", expectedLen, popCount)
	}
}

func TestRedisQueue_Clear(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	// 推入数据
	for i := 0; i < 5; i++ {
		queue.Push([]byte(fmt.Sprintf(`{"i":%d}`, i)))
	}

	if queue.Len() != 5 {
		t.Fatalf("expected Len() = 5, got %d", queue.Len())
	}

	// 清空
	if err := queue.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if queue.Len() != 0 {
		t.Errorf("expected Len() = 0 after Clear, got %d", queue.Len())
	}
}

func TestRedisQueue_Stats(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	queue.PushWithPriority([]byte("a"), 1)
	queue.PushWithPriority([]byte("b"), 2)

	stats := queue.Stats()
	if stats["key"] != opts.queueFullKey() {
		t.Errorf("expected key %s, got %v", opts.queueFullKey(), stats["key"])
	}
	if stats["length"].(int64) != 2 {
		t.Errorf("expected length 2, got %v", stats["length"])
	}
}

func TestRedisQueue_NegativePriority(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	// 测试负优先级
	queue.PushWithPriority([]byte("negative"), -5)
	queue.PushWithPriority([]byte("zero"), 0)
	queue.PushWithPriority([]byte("positive"), 5)

	// 正优先级先出
	data, priority, _ := queue.PopWithPriority()
	if priority != 5 {
		t.Errorf("expected priority 5, got %d", priority)
	}
	if string(data) != "positive" {
		t.Errorf("expected 'positive', got %s", data)
	}

	// 然后是 0
	data, priority, _ = queue.PopWithPriority()
	if priority != 0 {
		t.Errorf("expected priority 0, got %d", priority)
	}
	if string(data) != "zero" {
		t.Errorf("expected 'zero', got %s", data)
	}

	// 最后是负优先级
	data, priority, _ = queue.PopWithPriority()
	if priority != -5 {
		t.Errorf("expected priority -5, got %d (data: %s)", priority, data)
	}
	if string(data) != "negative" {
		t.Errorf("expected 'negative', got %s", data)
	}
}

// ============================================================================
// RedisDupeFilter 测试
// ============================================================================

func TestRedisDupeFilter_RequestSeen(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	if err := df.Open(context.Background()); err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// 创建测试请求
	req, _ := shttp.NewRequest("http://example.com/page1")

	// 第一次应该返回 false（新请求）
	if df.RequestSeen(req) {
		t.Error("first call to RequestSeen should return false")
	}

	// 第二次应该返回 true（重复请求）
	if !df.RequestSeen(req) {
		t.Error("second call to RequestSeen should return true")
	}

	// 不同 URL 应该返回 false
	req2, _ := shttp.NewRequest("http://example.com/page2")
	if df.RequestSeen(req2) {
		t.Error("different URL should return false")
	}
}

func TestRedisDupeFilter_SeenCount(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	df.Open(context.Background())

	// 初始为 0
	if df.SeenCount() != 0 {
		t.Errorf("expected SeenCount() = 0, got %d", df.SeenCount())
	}

	// 添加请求
	req1, _ := shttp.NewRequest("http://example.com/1")
	req2, _ := shttp.NewRequest("http://example.com/2")
	req3, _ := shttp.NewRequest("http://example.com/3")

	df.RequestSeen(req1)
	df.RequestSeen(req2)
	df.RequestSeen(req3)

	if df.SeenCount() != 3 {
		t.Errorf("expected SeenCount() = 3, got %d", df.SeenCount())
	}

	// 重复请求不增加计数
	df.RequestSeen(req1)
	if df.SeenCount() != 3 {
		t.Errorf("expected SeenCount() = 3 after duplicate, got %d", df.SeenCount())
	}
}

func TestRedisDupeFilter_FlushOnStart(t *testing.T) {
	mr, opts := setupMiniredis(t)

	// 先添加一些指纹
	client := setupRedisClient(t, mr)
	defer client.Close()

	ctx := context.Background()
	client.SAdd(ctx, opts.dupeFilterFullKey(), "fp1", "fp2", "fp3")

	// 使用 FlushOnStart 创建过滤器
	opts.FlushOnStart = true
	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter with FlushOnStart failed: %v", err)
	}
	defer df.Close("test")

	df.Open(context.Background())

	// 验证旧数据已被清空
	if df.SeenCount() != 0 {
		t.Errorf("expected SeenCount() = 0 after FlushOnStart, got %d", df.SeenCount())
	}
}

func TestRedisDupeFilter_Contains(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	df.Open(context.Background())

	req, _ := shttp.NewRequest("http://example.com/check")

	// 未添加前不包含
	if df.Contains(req) {
		t.Error("Contains should return false before RequestSeen")
	}

	// 添加后包含
	df.RequestSeen(req)
	if !df.Contains(req) {
		t.Error("Contains should return true after RequestSeen")
	}
}

func TestRedisDupeFilter_Close(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}

	df.Open(context.Background())

	// 关闭
	if err := df.Close("test"); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 关闭后 RequestSeen 应返回 false（保守处理）
	req, _ := shttp.NewRequest("http://example.com")
	if df.RequestSeen(req) {
		t.Error("RequestSeen after Close should return false")
	}

	// 重复关闭不应报错
	if err := df.Close("test"); err != nil {
		t.Errorf("double Close should not error, got: %v", err)
	}
}

func TestRedisDupeFilter_FromClient(t *testing.T) {
	mr, opts := setupMiniredis(t)

	client := setupRedisClient(t, mr)
	defer client.Close()

	df, err := NewRedisDupeFilterFromClient(client, opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilterFromClient failed: %v", err)
	}

	df.Open(context.Background())

	req, _ := shttp.NewRequest("http://example.com")
	df.RequestSeen(req)

	if df.SeenCount() != 1 {
		t.Errorf("expected SeenCount() = 1, got %d", df.SeenCount())
	}

	// Close 不应关闭共享的 client
	df.Close("test")

	// client 仍然可用
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Errorf("shared client should still be usable after df.Close(), got: %v", err)
	}
}

func TestRedisDupeFilter_NilClient(t *testing.T) {
	opts := DefaultOptions()
	_, err := NewRedisDupeFilterFromClient(nil, opts)
	if err == nil {
		t.Error("expected error when client is nil")
	}
}

func TestRedisDupeFilter_ConcurrentRequestSeen(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	df.Open(context.Background())

	const numGoroutines = 10
	const numURLs = 50

	// 生成唯一 URL
	urls := make([]string, numURLs)
	for i := 0; i < numURLs; i++ {
		urls[i] = fmt.Sprintf("http://example.com/page/%d", i)
	}

	// 并发调用 RequestSeen
	var wg sync.WaitGroup
	var seenTrue int64
	var seenFalse int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, url := range urls {
				req, _ := shttp.NewRequest(url)
				if df.RequestSeen(req) {
					atomic.AddInt64(&seenTrue, 1)
				} else {
					atomic.AddInt64(&seenFalse, 1)
				}
			}
		}()
	}
	wg.Wait()

	// 每个 URL 应该只有一次返回 false（新请求）
	if seenFalse != int64(numURLs) {
		t.Errorf("expected %d new requests, got %d", numURLs, seenFalse)
	}

	// 其余都是重复
	expectedDupes := int64(numGoroutines*numURLs) - int64(numURLs)
	if seenTrue != expectedDupes {
		t.Errorf("expected %d duplicates, got %d", expectedDupes, seenTrue)
	}

	// 最终去重集合大小应等于唯一 URL 数
	if df.SeenCount() != numURLs {
		t.Errorf("expected SeenCount() = %d, got %d", numURLs, df.SeenCount())
	}
}

func TestRedisDupeFilter_Clear(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	df.Open(context.Background())

	// 添加一些请求
	for i := 0; i < 5; i++ {
		req, _ := shttp.NewRequest(fmt.Sprintf("http://example.com/%d", i))
		df.RequestSeen(req)
	}

	if df.SeenCount() != 5 {
		t.Fatalf("expected SeenCount() = 5, got %d", df.SeenCount())
	}

	// 清空
	if err := df.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if df.SeenCount() != 0 {
		t.Errorf("expected SeenCount() = 0 after Clear, got %d", df.SeenCount())
	}
}

// ============================================================================
// 集成测试：RedisQueue + RedisDupeFilter 协同工作
// ============================================================================

func TestIntegration_QueueAndDupeFilter(t *testing.T) {
	mr, opts := setupMiniredis(t)

	// 使用共享 client
	client := setupRedisClient(t, mr)
	defer client.Close()

	queue, err := NewRedisQueueFromClient(client, opts)
	if err != nil {
		t.Fatalf("NewRedisQueueFromClient failed: %v", err)
	}

	df, err := NewRedisDupeFilterFromClient(client, opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilterFromClient failed: %v", err)
	}

	df.Open(context.Background())

	// 模拟调度器行为：去重 + 入队
	urls := []string{
		"http://example.com/1",
		"http://example.com/2",
		"http://example.com/1", // 重复
		"http://example.com/3",
		"http://example.com/2", // 重复
	}

	enqueued := 0
	for _, url := range urls {
		req, _ := shttp.NewRequest(url)
		if !df.RequestSeen(req) {
			data := []byte(`{"url":"` + url + `"}`)
			if err := queue.PushWithPriority(data, 0); err != nil {
				t.Fatalf("PushWithPriority failed: %v", err)
			}
			enqueued++
		}
	}

	// 应该只入队 3 个（去重后）
	if enqueued != 3 {
		t.Errorf("expected 3 enqueued, got %d", enqueued)
	}
	if queue.Len() != 3 {
		t.Errorf("expected queue Len() = 3, got %d", queue.Len())
	}
	if df.SeenCount() != 3 {
		t.Errorf("expected SeenCount() = 3, got %d", df.SeenCount())
	}

	// 出队所有
	for i := 0; i < 3; i++ {
		data, err := queue.Pop()
		if err != nil {
			t.Fatalf("Pop failed: %v", err)
		}
		if data == nil {
			t.Fatalf("unexpected nil data at pop %d", i)
		}
	}

	// 队列应为空
	if queue.Len() != 0 {
		t.Errorf("expected empty queue, got Len() = %d", queue.Len())
	}

	queue.Close()
	df.Close("test")
}

func TestIntegration_ResumeFromRedis(t *testing.T) {
	mr, opts := setupMiniredis(t)

	// 第一次运行：推入数据
	queue1, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue (first) failed: %v", err)
	}

	queue1.PushWithPriority([]byte(`{"url":"http://example.com/1"}`), 0)
	queue1.PushWithPriority([]byte(`{"url":"http://example.com/2"}`), 5)
	queue1.PushWithPriority([]byte(`{"url":"http://example.com/3"}`), 10)

	// 模拟进程退出（不清空数据）
	queue1.Close()

	// 第二次运行：从 Redis 恢复
	opts2 := DefaultOptions()
	opts2.Addr = mr.Addr()
	opts2.KeyPrefix = "test"

	queue2, err := NewRedisQueue(opts2)
	if err != nil {
		t.Fatalf("NewRedisQueue (second) failed: %v", err)
	}
	defer queue2.Close()

	// 验证数据仍在
	if queue2.Len() != 3 {
		t.Errorf("expected Len() = 3 after resume, got %d", queue2.Len())
	}

	// 验证优先级顺序
	_, priority, _ := queue2.PopWithPriority()
	if priority != 10 {
		t.Errorf("expected priority 10 first, got %d", priority)
	}
}

// ============================================================================
// Options 测试
// ============================================================================

func TestOptions_DefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.Addr != "localhost:6379" {
		t.Errorf("expected default Addr 'localhost:6379', got %s", opts.Addr)
	}
	if opts.KeyPrefix != "scrapy-go" {
		t.Errorf("expected default KeyPrefix 'scrapy-go', got %s", opts.KeyPrefix)
	}
	if opts.QueueKey != "queue" {
		t.Errorf("expected default QueueKey 'queue', got %s", opts.QueueKey)
	}
	if opts.DupeFilterKey != "dupefilter" {
		t.Errorf("expected default DupeFilterKey 'dupefilter', got %s", opts.DupeFilterKey)
	}
	if opts.PoolSize != 10 {
		t.Errorf("expected default PoolSize 10, got %d", opts.PoolSize)
	}
}

func TestOptions_FullKeys(t *testing.T) {
	opts := &Options{
		KeyPrefix:     "myproject",
		QueueKey:      "requests",
		DupeFilterKey: "seen",
		StartURLsKey:  "urls",
	}

	if opts.queueFullKey() != "myproject:requests" {
		t.Errorf("expected 'myproject:requests', got %s", opts.queueFullKey())
	}
	if opts.dupeFilterFullKey() != "myproject:seen" {
		t.Errorf("expected 'myproject:seen', got %s", opts.dupeFilterFullKey())
	}
	if opts.startURLsFullKey() != "myproject:urls" {
		t.Errorf("expected 'myproject:urls', got %s", opts.startURLsFullKey())
	}
}

// ============================================================================
// Score 编码/解码测试
// ============================================================================

func TestEncodeDecodeScore(t *testing.T) {
	tests := []struct {
		priority int
		seq      int64
	}{
		{0, 1},
		{1, 100},
		{10, 999},
		{-5, 50},
		{100, 1000000},
	}

	for _, tt := range tests {
		score := encodeScore(tt.priority, tt.seq)
		decoded := decodeScore(score)
		if decoded != tt.priority {
			t.Errorf("encodeScore(%d, %d) = %f, decodeScore = %d, expected %d",
				tt.priority, tt.seq, score, decoded, tt.priority)
		}
	}
}

func TestEncodeScore_Ordering(t *testing.T) {
	// 高优先级的 score 应该更大
	s1 := encodeScore(0, 1)
	s2 := encodeScore(5, 1)
	s3 := encodeScore(10, 1)

	if s1 >= s2 || s2 >= s3 {
		t.Errorf("score ordering violated: s1=%f, s2=%f, s3=%f", s1, s2, s3)
	}

	// 相同优先级，后入队的 score 更大（LIFO）
	s4 := encodeScore(5, 1)
	s5 := encodeScore(5, 2)
	s6 := encodeScore(5, 3)

	if s4 >= s5 || s5 >= s6 {
		t.Errorf("LIFO ordering violated: s4=%f, s5=%f, s6=%f", s4, s5, s6)
	}
}

func TestRedisQueue_PriorityStats(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	// 推入不同优先级的数据
	queue.PushWithPriority([]byte("a1"), 1)
	queue.PushWithPriority([]byte("a2"), 1)
	queue.PushWithPriority([]byte("b1"), 5)
	queue.PushWithPriority([]byte("c1"), 10)
	queue.PushWithPriority([]byte("c2"), 10)
	queue.PushWithPriority([]byte("c3"), 10)

	stats, err := queue.PriorityStats()
	if err != nil {
		t.Fatalf("PriorityStats failed: %v", err)
	}

	if stats[1] != 2 {
		t.Errorf("expected priority 1 count = 2, got %d", stats[1])
	}
	if stats[5] != 1 {
		t.Errorf("expected priority 5 count = 1, got %d", stats[5])
	}
	if stats[10] != 3 {
		t.Errorf("expected priority 10 count = 3, got %d", stats[10])
	}
}

func TestRedisQueue_LenByPriority(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	queue.PushWithPriority([]byte("a"), 0)
	queue.PushWithPriority([]byte("b"), 0)
	queue.PushWithPriority([]byte("c"), 5)

	count, err := queue.LenByPriority(0)
	if err != nil {
		t.Fatalf("LenByPriority failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected LenByPriority(0) = 2, got %d", count)
	}

	count, err = queue.LenByPriority(5)
	if err != nil {
		t.Fatalf("LenByPriority failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected LenByPriority(5) = 1, got %d", count)
	}

	count, err = queue.LenByPriority(99)
	if err != nil {
		t.Fatalf("LenByPriority failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected LenByPriority(99) = 0, got %d", count)
	}
}

func TestRedisQueue_Client(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	client := queue.Client()
	if client == nil {
		t.Error("Client() should not return nil")
	}

	// 验证 client 可用
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Errorf("Client() returned unusable client: %v", err)
	}
}

func TestRedisDupeFilter_Client(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	client := df.Client()
	if client == nil {
		t.Error("Client() should not return nil")
	}
}

func TestRedisQueue_ClosedOperations(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}

	queue.Close()

	// 所有操作应返回错误或零值
	if err := queue.PushWithPriority([]byte("test"), 0); err == nil {
		t.Error("PushWithPriority after Close should error")
	}

	_, _, err = queue.PopWithPriority()
	if err == nil {
		t.Error("PopWithPriority after Close should error")
	}

	data, err := queue.Peek()
	if err == nil {
		t.Error("Peek after Close should error")
	}
	if data != nil {
		t.Error("Peek after Close should return nil data")
	}

	if queue.Len() != 0 {
		t.Error("Len after Close should return 0")
	}

	if err := queue.Clear(); err == nil {
		t.Error("Clear after Close should error")
	}

	_, err = queue.PriorityStats()
	if err == nil {
		t.Error("PriorityStats after Close should error")
	}

	_, err = queue.LenByPriority(0)
	if err == nil {
		t.Error("LenByPriority after Close should error")
	}
}

func TestRedisDupeFilter_ClosedOperations(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}

	df.Open(context.Background())
	df.Close("test")

	// SeenCount 应返回 0
	if df.SeenCount() != 0 {
		t.Error("SeenCount after Close should return 0")
	}

	// Contains 应返回 false
	req, _ := shttp.NewRequest("http://example.com")
	if df.Contains(req) {
		t.Error("Contains after Close should return false")
	}

	// Clear 应返回错误
	if err := df.Clear(); err == nil {
		t.Error("Clear after Close should error")
	}
}

func TestRedisQueue_StatsAfterClose(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}

	queue.Close()

	stats := queue.Stats()
	if stats["closed"] != true {
		t.Error("Stats after Close should indicate closed")
	}
}

// ============================================================================
// 构造函数 nil opts 测试
// ============================================================================

func TestNewRedisQueue_NilOpts(t *testing.T) {
	mr := miniredis.RunT(t)

	// 使用 nil opts 应该使用默认配置，但连接到 miniredis
	// 由于默认 Addr 是 localhost:6379，不一定能连上，所以这里测试 nil 分支
	// 先用正常 opts 创建，然后测试 nil opts 的 FromClient 路径
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	// NewRedisQueueFromClient with nil opts
	queue, err := NewRedisQueueFromClient(client, nil)
	if err != nil {
		t.Fatalf("NewRedisQueueFromClient with nil opts failed: %v", err)
	}
	defer queue.Close()

	// 验证使用了默认配置
	if queue.key != "scrapy-go:queue" {
		t.Errorf("expected default key 'scrapy-go:queue', got %s", queue.key)
	}
}

func TestNewRedisDupeFilter_NilOpts(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	// NewRedisDupeFilterFromClient with nil opts
	df, err := NewRedisDupeFilterFromClient(client, nil)
	if err != nil {
		t.Fatalf("NewRedisDupeFilterFromClient with nil opts failed: %v", err)
	}
	defer df.Close("test")

	// 验证使用了默认配置
	if df.key != "scrapy-go:dupefilter" {
		t.Errorf("expected default key 'scrapy-go:dupefilter', got %s", df.key)
	}
}

// ============================================================================
// 连接失败测试
// ============================================================================

func TestNewRedisQueue_ConnectionFailure(t *testing.T) {
	opts := DefaultOptions()
	opts.Addr = "localhost:1" // 不可达的端口
	opts.DialTimeout = 1     // 最短超时

	_, err := NewRedisQueue(opts)
	if err == nil {
		t.Error("expected error when connecting to unreachable Redis")
	}
}

func TestNewRedisDupeFilter_ConnectionFailure(t *testing.T) {
	opts := DefaultOptions()
	opts.Addr = "localhost:1" // 不可达的端口
	opts.DialTimeout = 1     // 最短超时

	_, err := NewRedisDupeFilter(opts)
	if err == nil {
		t.Error("expected error when connecting to unreachable Redis")
	}
}

// ============================================================================
// FromClient FlushOnStart 测试
// ============================================================================

func TestNewRedisQueueFromClient_FlushOnStart(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	// 先推入一些数据
	ctx := context.Background()
	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "test"
	client.ZAdd(ctx, opts.queueFullKey(), redis.Z{Score: 1, Member: "old_data"})

	// 使用 FlushOnStart 创建
	opts.FlushOnStart = true
	queue, err := NewRedisQueueFromClient(client, opts)
	if err != nil {
		t.Fatalf("NewRedisQueueFromClient with FlushOnStart failed: %v", err)
	}
	defer queue.Close()

	if queue.Len() != 0 {
		t.Errorf("expected empty queue after FlushOnStart, got Len() = %d", queue.Len())
	}
}

// ============================================================================
// DupeFilter Open 不带 FlushOnStart 测试
// ============================================================================

func TestRedisDupeFilter_OpenWithoutFlush(t *testing.T) {
	mr, opts := setupMiniredis(t)

	// 先添加一些指纹
	client := setupRedisClient(t, mr)
	defer client.Close()

	ctx := context.Background()
	client.SAdd(ctx, opts.dupeFilterFullKey(), "fp1", "fp2")

	// 不使用 FlushOnStart
	opts.FlushOnStart = false
	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	if err := df.Open(context.Background()); err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// 旧数据应该保留
	if df.SeenCount() != 2 {
		t.Errorf("expected SeenCount() = 2 (preserved), got %d", df.SeenCount())
	}
}

// ============================================================================
// canonicalizeURL 测试
// ============================================================================

func TestCanonicalizeURL(t *testing.T) {
	tests := []struct {
		name          string
		rawURL        string
		keepFragments bool
		expected      string
	}{
		{
			name:     "基本 URL 规范化",
			rawURL:   "HTTP://EXAMPLE.COM/path",
			expected: "http://example.com/path",
		},
		{
			name:     "移除 http 默认端口 80",
			rawURL:   "http://example.com:80/path",
			expected: "http://example.com/path",
		},
		{
			name:     "移除 https 默认端口 443",
			rawURL:   "https://example.com:443/path",
			expected: "https://example.com/path",
		},
		{
			name:     "保留非默认端口",
			rawURL:   "http://example.com:8080/path",
			expected: "http://example.com:8080/path",
		},
		{
			name:     "查询参数排序",
			rawURL:   "http://example.com/path?z=3&a=1&m=2",
			expected: "http://example.com/path?a=1&m=2&z=3",
		},
		{
			name:     "移除 fragment",
			rawURL:   "http://example.com/path#section",
			expected: "http://example.com/path",
		},
		{
			name:          "保留 fragment",
			rawURL:        "http://example.com/path#section",
			keepFragments: true,
			expected:      "http://example.com/path#section",
		},
		{
			name:     "空路径补全为 /",
			rawURL:   "http://example.com",
			expected: "http://example.com/",
		},
		{
			name:     "无效 URL 返回原始值",
			rawURL:   "://invalid",
			expected: "://invalid",
		},
		{
			name:     "带查询参数和 fragment",
			rawURL:   "http://example.com/path?b=2&a=1#frag",
			expected: "http://example.com/path?a=1&b=2",
		},
		{
			name:     "多值查询参数排序",
			rawURL:   "http://example.com/?tag=b&tag=a",
			expected: "http://example.com/?tag=a&tag=b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeURL(tt.rawURL, tt.keepFragments)
			if result != tt.expected {
				t.Errorf("canonicalizeURL(%q, %v) = %q, expected %q",
					tt.rawURL, tt.keepFragments, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// requestFingerprint 测试
// ============================================================================

func TestRequestFingerprint(t *testing.T) {
	// 相同请求应产生相同指纹
	req1, _ := shttp.NewRequest("http://example.com/page")
	req2, _ := shttp.NewRequest("http://example.com/page")

	fp1 := requestFingerprint(req1)
	fp2 := requestFingerprint(req2)

	if fp1 != fp2 {
		t.Errorf("same URL should produce same fingerprint: %s != %s", fp1, fp2)
	}

	// 不同 URL 应产生不同指纹
	req3, _ := shttp.NewRequest("http://example.com/other")
	fp3 := requestFingerprint(req3)

	if fp1 == fp3 {
		t.Errorf("different URLs should produce different fingerprints: %s == %s", fp1, fp3)
	}

	// 指纹应该是 40 字符的十六进制字符串（SHA1）
	if len(fp1) != 40 {
		t.Errorf("fingerprint should be 40 chars (SHA1 hex), got %d: %s", len(fp1), fp1)
	}
}

func TestRequestFingerprint_MethodMatters(t *testing.T) {
	req1, _ := shttp.NewRequest("http://example.com/page", shttp.WithMethod("GET"))
	req2, _ := shttp.NewRequest("http://example.com/page", shttp.WithMethod("POST"))

	fp1 := requestFingerprint(req1)
	fp2 := requestFingerprint(req2)

	if fp1 == fp2 {
		t.Error("different methods should produce different fingerprints")
	}
}

func TestRequestFingerprint_URLNormalization(t *testing.T) {
	// URL 规范化后应产生相同指纹
	req1, _ := shttp.NewRequest("HTTP://EXAMPLE.COM/page")
	req2, _ := shttp.NewRequest("http://example.com/page")

	fp1 := requestFingerprint(req1)
	fp2 := requestFingerprint(req2)

	if fp1 != fp2 {
		t.Errorf("normalized URLs should produce same fingerprint: %s != %s", fp1, fp2)
	}
}

// ============================================================================
// Peek 多数据测试
// ============================================================================

func TestRedisQueue_PeekWithMultiplePriorities(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	// 推入多个优先级
	queue.PushWithPriority([]byte("low"), 1)
	queue.PushWithPriority([]byte("high"), 10)
	queue.PushWithPriority([]byte("mid"), 5)

	// Peek 应返回最高优先级的数据
	data, err := queue.Peek()
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if string(data) != "high" {
		t.Errorf("expected 'high' (highest priority), got %s", data)
	}

	// Peek 不应移除数据
	if queue.Len() != 3 {
		t.Errorf("Peek should not remove data, Len() = %d", queue.Len())
	}

	// 多次 Peek 应返回相同结果
	data2, _ := queue.Peek()
	if string(data) != string(data2) {
		t.Errorf("multiple Peeks should return same data: %s != %s", data, data2)
	}
}

// ============================================================================
// DupeFilter 不同 HTTP 方法测试
// ============================================================================

func TestRedisDupeFilter_DifferentMethods(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	df.Open(context.Background())

	// GET 和 POST 到同一 URL 应该是不同的请求
	getReq, _ := shttp.NewRequest("http://example.com/api", shttp.WithMethod("GET"))
	postReq, _ := shttp.NewRequest("http://example.com/api", shttp.WithMethod("POST"))

	if df.RequestSeen(getReq) {
		t.Error("GET request should be new")
	}
	if df.RequestSeen(postReq) {
		t.Error("POST request should be new (different method)")
	}

	// 重复的 GET 应该被检测到
	if !df.RequestSeen(getReq) {
		t.Error("duplicate GET should be detected")
	}

	if df.SeenCount() != 2 {
		t.Errorf("expected SeenCount() = 2, got %d", df.SeenCount())
	}
}

// ============================================================================
// 大量数据测试
// ============================================================================

func TestRedisQueue_LargeDataset(t *testing.T) {
	_, opts := setupMiniredis(t)

	queue, err := NewRedisQueue(opts)
	if err != nil {
		t.Fatalf("NewRedisQueue failed: %v", err)
	}
	defer queue.Close()

	const count = 500

	// 推入大量数据
	for i := 0; i < count; i++ {
		priority := i % 10
		data := []byte(fmt.Sprintf(`{"url":"http://example.com/%d"}`, i))
		if err := queue.PushWithPriority(data, priority); err != nil {
			t.Fatalf("Push %d failed: %v", i, err)
		}
	}

	if queue.Len() != count {
		t.Errorf("expected Len() = %d, got %d", count, queue.Len())
	}

	// 验证出队顺序（高优先级先出）
	lastPriority := 100
	for i := 0; i < count; i++ {
		_, priority, err := queue.PopWithPriority()
		if err != nil {
			t.Fatalf("Pop %d failed: %v", i, err)
		}
		if priority > lastPriority {
			t.Errorf("priority ordering violated at pop %d: %d > %d", i, priority, lastPriority)
		}
		lastPriority = priority
	}

	if queue.Len() != 0 {
		t.Errorf("expected empty queue, got Len() = %d", queue.Len())
	}
}

// ============================================================================
// Options 额外字段测试
// ============================================================================

func TestOptions_AllDefaults(t *testing.T) {
	opts := DefaultOptions()

	if opts.Password != "" {
		t.Errorf("expected empty Password, got %s", opts.Password)
	}
	if opts.DB != 0 {
		t.Errorf("expected DB 0, got %d", opts.DB)
	}
	if opts.FlushOnStart != false {
		t.Error("expected FlushOnStart false")
	}
	if opts.Serializer != "json" {
		t.Errorf("expected Serializer 'json', got %s", opts.Serializer)
	}
	if opts.StartURLsKey != "start_urls" {
		t.Errorf("expected StartURLsKey 'start_urls', got %s", opts.StartURLsKey)
	}
	if opts.DialTimeout != 5*1e9 { // 5s in nanoseconds
		t.Errorf("expected DialTimeout 5s, got %v", opts.DialTimeout)
	}
	if opts.ReadTimeout != 3*1e9 {
		t.Errorf("expected ReadTimeout 3s, got %v", opts.ReadTimeout)
	}
	if opts.WriteTimeout != 3*1e9 {
		t.Errorf("expected WriteTimeout 3s, got %v", opts.WriteTimeout)
	}
}

// ============================================================================
// decodeScore 边界测试
// ============================================================================

func TestDecodeScore_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		score    float64
		expected int
	}{
		{"zero", 0, 0},
		{"positive exact", 50000000000, 5},
		{"positive with seq", 50000000123, 5},
		{"negative exact", -50000000000, -5},
		{"negative with seq", -49999999950, -5},
		{"large positive", 1000000000000, 100},
		{"large negative", -1000000000000, -100},
		{"small positive seq", 1, 0},
		{"negative one", -10000000000, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decodeScore(tt.score)
			if result != tt.expected {
				t.Errorf("decodeScore(%f) = %d, expected %d", tt.score, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// 集成测试：多优先级出入队完整流程
// ============================================================================

func TestIntegration_MultiplePriorityFullCycle(t *testing.T) {
	mr, opts := setupMiniredis(t)

	client := setupRedisClient(t, mr)
	defer client.Close()

	queue, err := NewRedisQueueFromClient(client, opts)
	if err != nil {
		t.Fatalf("NewRedisQueueFromClient failed: %v", err)
	}
	defer queue.Close()

	df, err := NewRedisDupeFilterFromClient(client, opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilterFromClient failed: %v", err)
	}
	defer df.Close("test")

	df.Open(context.Background())

	// 模拟多优先级请求
	type reqInfo struct {
		url      string
		priority int
	}
	requests := []reqInfo{
		{"http://example.com/high1", 10},
		{"http://example.com/high2", 10},
		{"http://example.com/mid1", 5},
		{"http://example.com/low1", 0},
		{"http://example.com/low2", 0},
		{"http://example.com/high1", 10}, // 重复
		{"http://example.com/mid1", 5},   // 重复
	}

	enqueued := 0
	for _, ri := range requests {
		req, _ := shttp.NewRequest(ri.url)
		if !df.RequestSeen(req) {
			data := []byte(fmt.Sprintf(`{"url":"%s"}`, ri.url))
			queue.PushWithPriority(data, ri.priority)
			enqueued++
		}
	}

	if enqueued != 5 {
		t.Errorf("expected 5 enqueued (2 dupes), got %d", enqueued)
	}

	// 验证 PriorityStats
	stats, err := queue.PriorityStats()
	if err != nil {
		t.Fatalf("PriorityStats failed: %v", err)
	}
	if stats[10] != 2 {
		t.Errorf("expected 2 high priority, got %d", stats[10])
	}
	if stats[5] != 1 {
		t.Errorf("expected 1 mid priority, got %d", stats[5])
	}
	if stats[0] != 2 {
		t.Errorf("expected 2 low priority, got %d", stats[0])
	}

	// 验证 LenByPriority
	highCount, _ := queue.LenByPriority(10)
	if highCount != 2 {
		t.Errorf("expected LenByPriority(10) = 2, got %d", highCount)
	}

	// 出队验证优先级顺序
	prevPriority := 100
	for i := 0; i < 5; i++ {
		_, p, err := queue.PopWithPriority()
		if err != nil {
			t.Fatalf("Pop %d failed: %v", i, err)
		}
		if p > prevPriority {
			t.Errorf("priority order violated: %d > %d", p, prevPriority)
		}
		prevPriority = p
	}
}

// ============================================================================
// DupeFilter 与 Contains 一致性测试
// ============================================================================

func TestRedisDupeFilter_ContainsConsistency(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	df.Open(context.Background())

	urls := []string{
		"http://example.com/a",
		"http://example.com/b",
		"http://example.com/c",
	}

	// 添加请求
	for _, u := range urls {
		req, _ := shttp.NewRequest(u)
		df.RequestSeen(req)
	}

	// Contains 应该对所有已添加的 URL 返回 true
	for _, u := range urls {
		req, _ := shttp.NewRequest(u)
		if !df.Contains(req) {
			t.Errorf("Contains should return true for %s", u)
		}
	}

	// Contains 对未添加的 URL 返回 false
	req, _ := shttp.NewRequest("http://example.com/not-added")
	if df.Contains(req) {
		t.Error("Contains should return false for unseen URL")
	}
}
