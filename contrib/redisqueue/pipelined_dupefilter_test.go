package redisqueue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/redis/go-redis/v9"
)

// ============================================================================
// PipelinedRedisDupeFilter 单元测试
// ============================================================================

func TestPipelinedRedisDupeFilter_RequestSeen(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(4), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	req1 := newTestRequest("http://example.com/page1")
	req2 := newTestRequest("http://example.com/page2")

	// 第一次应该返回 false（新请求）
	if df.RequestSeen(req1) {
		t.Error("expected RequestSeen(req1) = false for new request")
	}

	// 第二次应该返回 true（重复请求）
	if !df.RequestSeen(req1) {
		t.Error("expected RequestSeen(req1) = true for duplicate request")
	}

	// 不同请求应该返回 false
	if df.RequestSeen(req2) {
		t.Error("expected RequestSeen(req2) = false for different request")
	}
}

func TestPipelinedRedisDupeFilter_SeenCount(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(2), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	// 添加 3 个不同请求
	for i := 0; i < 3; i++ {
		req := newTestRequest(fmt.Sprintf("http://example.com/page%d", i))
		df.RequestSeen(req)
	}

	// 等待 Pipeline 刷新
	time.Sleep(100 * time.Millisecond)

	count := df.SeenCount()
	if count != 3 {
		t.Errorf("expected SeenCount() = 3, got %d", count)
	}
}

func TestPipelinedRedisDupeFilter_FlushOnStart(t *testing.T) {
	mr := miniredis.RunT(t)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "test"

	// 先创建一个过滤器并添加数据
	df1, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(2), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}

	req := newTestRequest("http://example.com/page1")
	df1.RequestSeen(req)
	time.Sleep(100 * time.Millisecond)
	df1.Close("test")

	// 创建新过滤器，启用 FlushOnStart
	opts2 := DefaultOptions()
	opts2.Addr = mr.Addr()
	opts2.KeyPrefix = "test"
	opts2.FlushOnStart = true

	df2, err := NewPipelinedRedisDupeFilter(opts2, WithBatchSize(2), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df2.Close("test")

	if err := df2.Open(context.Background()); err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// FlushOnStart 后，之前的请求应该被视为新请求
	if df2.RequestSeen(req) {
		t.Error("expected RequestSeen = false after FlushOnStart")
	}
}

func TestPipelinedRedisDupeFilter_Contains(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(2), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	req := newTestRequest("http://example.com/page1")

	// 未添加前，Contains 应返回 false
	if df.Contains(req) {
		t.Error("expected Contains = false before RequestSeen")
	}

	// 添加后，Contains 应返回 true
	df.RequestSeen(req)
	time.Sleep(100 * time.Millisecond)

	if !df.Contains(req) {
		t.Error("expected Contains = true after RequestSeen")
	}
}

func TestPipelinedRedisDupeFilter_Close(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(100), WithFlushInterval(1*time.Second))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}

	// 添加一些请求（不等待刷新）
	for i := 0; i < 5; i++ {
		req := newTestRequest(fmt.Sprintf("http://example.com/page%d", i))
		go df.RequestSeen(req)
	}

	// 短暂等待确保请求已提交到 channel
	time.Sleep(20 * time.Millisecond)

	// Close 应该排空缓冲区并正常关闭
	if err := df.Close("test"); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 关闭后 RequestSeen 应返回 false
	req := newTestRequest("http://example.com/new")
	if df.RequestSeen(req) {
		t.Error("expected RequestSeen = false after Close")
	}

	// 重复关闭应该是幂等的
	if err := df.Close("test"); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestPipelinedRedisDupeFilter_FromClient(t *testing.T) {
	mr := miniredis.RunT(t)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "test"

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	df, err := NewPipelinedRedisDupeFilterFromClient(client, opts, WithBatchSize(4))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilterFromClient failed: %v", err)
	}
	defer df.Close("test")

	req := newTestRequest("http://example.com/page1")
	if df.RequestSeen(req) {
		t.Error("expected RequestSeen = false for new request")
	}
	if !df.RequestSeen(req) {
		t.Error("expected RequestSeen = true for duplicate request")
	}
}

func TestPipelinedRedisDupeFilter_NilClient(t *testing.T) {
	opts := DefaultOptions()
	_, err := NewPipelinedRedisDupeFilterFromClient(nil, opts)
	if err == nil {
		t.Error("expected error for nil client")
	}
}

func TestPipelinedRedisDupeFilter_ConcurrentRequestSeen(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(16), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	const numGoroutines = 50
	const numRequests = 20

	// 每个 goroutine 提交相同的请求集合
	var wg sync.WaitGroup
	seenCounts := make([]int, numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gIdx int) {
			defer wg.Done()
			for i := 0; i < numRequests; i++ {
				req := newTestRequest(fmt.Sprintf("http://example.com/page%d", i))
				if df.RequestSeen(req) {
					seenCounts[gIdx]++
				}
			}
		}(g)
	}

	wg.Wait()

	// 等待所有 Pipeline 刷新完成
	time.Sleep(200 * time.Millisecond)

	// 去重集合中应该恰好有 numRequests 个不同指纹
	count := df.SeenCount()
	if count != numRequests {
		t.Errorf("expected SeenCount() = %d, got %d", numRequests, count)
	}

	// 总共应该有 numGoroutines * numRequests 次调用，
	// 其中 numRequests 次返回 false（新请求），其余返回 true（重复）
	totalSeen := 0
	for _, c := range seenCounts {
		totalSeen += c
	}
	expectedSeen := numGoroutines*numRequests - numRequests
	if totalSeen != expectedSeen {
		t.Errorf("expected total seen = %d, got %d", expectedSeen, totalSeen)
	}
}

func TestPipelinedRedisDupeFilter_Clear(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(2), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	req := newTestRequest("http://example.com/page1")
	df.RequestSeen(req)
	time.Sleep(100 * time.Millisecond)

	if df.SeenCount() != 1 {
		t.Errorf("expected SeenCount() = 1, got %d", df.SeenCount())
	}

	if err := df.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if df.SeenCount() != 0 {
		t.Errorf("expected SeenCount() = 0 after Clear, got %d", df.SeenCount())
	}
}

func TestPipelinedRedisDupeFilter_ClosedOperations(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(4), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}

	df.Close("test")

	// 关闭后的操作应该安全返回
	req := newTestRequest("http://example.com/page1")
	if df.RequestSeen(req) {
		t.Error("expected RequestSeen = false after Close")
	}
	if df.SeenCount() != 0 {
		t.Error("expected SeenCount = 0 after Close")
	}
	if df.Contains(req) {
		t.Error("expected Contains = false after Close")
	}
	if err := df.Clear(); err == nil {
		t.Error("expected error from Clear after Close")
	}
}

func TestPipelinedRedisDupeFilter_ConnectionFailure(t *testing.T) {
	opts := DefaultOptions()
	opts.Addr = "localhost:59999" // 不存在的端口
	opts.DialTimeout = 100 * time.Millisecond

	_, err := NewPipelinedRedisDupeFilter(opts)
	if err == nil {
		t.Error("expected error for connection failure")
	}
}

func TestPipelinedRedisDupeFilter_BatchSizeTrigger(t *testing.T) {
	_, opts := setupMiniredis(t)

	// 设置较大的刷新间隔，确保是批量大小触发刷新
	df, err := NewPipelinedRedisDupeFilter(opts,
		WithBatchSize(4),
		WithFlushInterval(10*time.Second),
	)
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	// 并发提交恰好 batchSize 个请求，确保它们同时进入 channel
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := newTestRequest(fmt.Sprintf("http://example.com/batch%d", idx))
			df.RequestSeen(req)
		}(i)
	}

	wg.Wait()

	// 批量大小触发后，数据应该已写入 Redis
	time.Sleep(50 * time.Millisecond)
	count := df.SeenCount()
	if count != 4 {
		t.Errorf("expected SeenCount() = 4 after batch trigger, got %d", count)
	}
}

func TestPipelinedRedisDupeFilter_FlushIntervalTrigger(t *testing.T) {
	_, opts := setupMiniredis(t)

	// 设置较大的批量大小，确保是定时器触发刷新
	df, err := NewPipelinedRedisDupeFilter(opts,
		WithBatchSize(1000),
		WithFlushInterval(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	// 提交少于 batchSize 的请求
	for i := 0; i < 3; i++ {
		req := newTestRequest(fmt.Sprintf("http://example.com/timer%d", i))
		df.RequestSeen(req)
	}

	// 等待定时器触发
	time.Sleep(200 * time.Millisecond)
	count := df.SeenCount()
	if count != 3 {
		t.Errorf("expected SeenCount() = 3 after flush interval, got %d", count)
	}
}

func TestPipelinedRedisDupeFilter_PipelineStats(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts,
		WithBatchSize(4),
		WithFlushInterval(50*time.Millisecond),
		WithBufferSize(1024),
	)
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	// 提交一些请求
	for i := 0; i < 8; i++ {
		req := newTestRequest(fmt.Sprintf("http://example.com/stats%d", i))
		df.RequestSeen(req)
	}

	time.Sleep(200 * time.Millisecond)

	stats := df.PipelineStats()
	if stats["batch_size"] != 4 {
		t.Errorf("expected batch_size = 4, got %v", stats["batch_size"])
	}
	if stats["buffer_size"] != 1024 {
		t.Errorf("expected buffer_size = 1024, got %v", stats["buffer_size"])
	}

	flushes := stats["pipeline_flushes"].(int64)
	if flushes < 1 {
		t.Errorf("expected pipeline_flushes >= 1, got %d", flushes)
	}

	items := stats["pipeline_items"].(int64)
	if items != 8 {
		t.Errorf("expected pipeline_items = 8, got %d", items)
	}
}

func TestPipelinedRedisDupeFilter_DifferentMethods(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(4), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	// GET 和 POST 到同一 URL 应该是不同的请求
	getReq := newTestRequest("http://example.com/api")
	postReq := newTestRequestWithMethod("http://example.com/api", "POST")

	if df.RequestSeen(getReq) {
		t.Error("expected GET request to be new")
	}
	if df.RequestSeen(postReq) {
		t.Error("expected POST request to be new (different from GET)")
	}
}

func TestPipelinedRedisDupeFilter_OpenWithoutFlush(t *testing.T) {
	mr := miniredis.RunT(t)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "test"

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(2), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}

	if err := df.Open(context.Background()); err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	req := newTestRequest("http://example.com/page1")
	df.RequestSeen(req)
	time.Sleep(100 * time.Millisecond)
	df.Close("test")

	// 重新创建（不启用 FlushOnStart）
	opts2 := DefaultOptions()
	opts2.Addr = mr.Addr()
	opts2.KeyPrefix = "test"

	df2, err := NewPipelinedRedisDupeFilter(opts2, WithBatchSize(2), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df2.Close("test")

	if err := df2.Open(context.Background()); err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// 之前的请求应该仍然被视为重复
	if !df2.RequestSeen(req) {
		t.Error("expected RequestSeen = true for previously seen request")
	}
}

func TestPipelinedRedisDupeFilter_NilOpts(t *testing.T) {
	mr := miniredis.RunT(t)

	// 使用 nil opts 但通过 client 创建
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	df, err := NewPipelinedRedisDupeFilterFromClient(client, nil)
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilterFromClient with nil opts failed: %v", err)
	}
	defer df.Close("test")

	req := newTestRequest("http://example.com/page1")
	if df.RequestSeen(req) {
		t.Error("expected RequestSeen = false for new request")
	}
}

// ============================================================================
// PipelinedRedisDupeFilter 布隆过滤器测试
// ============================================================================

func TestPipelinedRedisDupeFilter_BloomFilter_Basic(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.BloomFilterEnabled = true
	opts.BloomExpectedItems = 10000
	opts.BloomFalsePositiveRate = 0.001

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(4), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	req1 := newTestRequest("http://example.com/bloom1")
	req2 := newTestRequest("http://example.com/bloom2")

	// 新请求
	if df.RequestSeen(req1) {
		t.Error("expected RequestSeen(req1) = false")
	}

	// 重复请求
	if !df.RequestSeen(req1) {
		t.Error("expected RequestSeen(req1) = true for duplicate")
	}

	// 不同请求
	if df.RequestSeen(req2) {
		t.Error("expected RequestSeen(req2) = false")
	}
}

func TestPipelinedRedisDupeFilter_BloomFilter_Stats(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.BloomFilterEnabled = true
	opts.BloomExpectedItems = 10000
	opts.BloomFalsePositiveRate = 0.01

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(4), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	// 添加一些新请求
	for i := 0; i < 10; i++ {
		req := newTestRequest(fmt.Sprintf("http://example.com/bloom-stats%d", i))
		df.RequestSeen(req)
	}

	stats := df.BloomStats()
	if stats == nil {
		t.Fatal("expected non-nil BloomStats")
	}
	if stats["enabled"] != true {
		t.Error("expected enabled = true")
	}

	hits := stats["bloom_hits"].(int64)
	if hits < 1 {
		t.Errorf("expected bloom_hits >= 1, got %d", hits)
	}
}

func TestPipelinedRedisDupeFilter_BloomFilter_Disabled(t *testing.T) {
	_, opts := setupMiniredis(t)
	// 默认 BloomFilterEnabled = false

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(4), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	stats := df.BloomStats()
	if stats != nil {
		t.Error("expected nil BloomStats when bloom filter is disabled")
	}
}

func TestPipelinedRedisDupeFilter_BloomFilter_Concurrent(t *testing.T) {
	_, opts := setupMiniredis(t)
	opts.BloomFilterEnabled = true
	opts.BloomExpectedItems = 100000
	opts.BloomFalsePositiveRate = 0.001

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(16), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	const numGoroutines = 20
	const numRequests = 50

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numRequests; i++ {
				req := newTestRequest(fmt.Sprintf("http://example.com/bloom-concurrent%d", i))
				df.RequestSeen(req)
			}
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	count := df.SeenCount()
	if count != numRequests {
		t.Errorf("expected SeenCount() = %d, got %d", numRequests, count)
	}
}

// ============================================================================
// PipelinedRedisDupeFilter Pipeline 配置选项测试
// ============================================================================

func TestPipelineOption_WithBatchSize(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(128))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	if df.batchSize != 128 {
		t.Errorf("expected batchSize = 128, got %d", df.batchSize)
	}
}

func TestPipelineOption_WithBatchSize_Invalid(t *testing.T) {
	_, opts := setupMiniredis(t)

	// 无效值应该保持默认
	df, err := NewPipelinedRedisDupeFilter(opts, WithBatchSize(0), WithBatchSize(-1))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	if df.batchSize != DefaultPipelineBatchSize {
		t.Errorf("expected batchSize = %d (default), got %d", DefaultPipelineBatchSize, df.batchSize)
	}
}

func TestPipelineOption_WithFlushInterval(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithFlushInterval(500*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	if df.flushInterval != 500*time.Millisecond {
		t.Errorf("expected flushInterval = 500ms, got %v", df.flushInterval)
	}
}

func TestPipelineOption_WithFlushInterval_Invalid(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithFlushInterval(0), WithFlushInterval(-1*time.Second))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	if df.flushInterval != DefaultPipelineFlushInterval {
		t.Errorf("expected flushInterval = %v (default), got %v", DefaultPipelineFlushInterval, df.flushInterval)
	}
}

func TestPipelineOption_WithBufferSize(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBufferSize(2048))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	if df.bufferSize != 2048 {
		t.Errorf("expected bufferSize = 2048, got %d", df.bufferSize)
	}
}

func TestPipelineOption_WithBufferSize_Invalid(t *testing.T) {
	_, opts := setupMiniredis(t)

	df, err := NewPipelinedRedisDupeFilter(opts, WithBufferSize(0))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("test")

	if df.bufferSize != DefaultPipelineBufferSize {
		t.Errorf("expected bufferSize = %d (default), got %d", DefaultPipelineBufferSize, df.bufferSize)
	}
}

// ============================================================================
// PipelinedRedisDupeFilter 与 RedisDupeFilter 一致性测试
// ============================================================================

func TestPipelinedRedisDupeFilter_ConsistencyWithRedisDupeFilter(t *testing.T) {
	mr := miniredis.RunT(t)

	// 创建两个过滤器，使用不同的 key 前缀
	opts1 := DefaultOptions()
	opts1.Addr = mr.Addr()
	opts1.KeyPrefix = "test-standard"

	opts2 := DefaultOptions()
	opts2.Addr = mr.Addr()
	opts2.KeyPrefix = "test-pipelined"

	standardDF, err := NewRedisDupeFilter(opts1)
	if err != nil {
		t.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer standardDF.Close("test")

	pipelinedDF, err := NewPipelinedRedisDupeFilter(opts2, WithBatchSize(4), WithFlushInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer pipelinedDF.Close("test")

	// 对相同的请求集合，两个过滤器应该返回相同的结果
	requests := []*shttp.Request{
		newTestRequest("http://example.com/page1"),
		newTestRequest("http://example.com/page2"),
		newTestRequest("http://example.com/page3"),
		newTestRequest("http://example.com/page1"), // 重复
		newTestRequest("http://example.com/page2"), // 重复
	}

	for i, req := range requests {
		standardResult := standardDF.RequestSeen(req)
		pipelinedResult := pipelinedDF.RequestSeen(req)

		if standardResult != pipelinedResult {
			t.Errorf("request %d: standard=%v, pipelined=%v (expected same)",
				i, standardResult, pipelinedResult)
		}
	}

	// 等待 Pipeline 刷新
	time.Sleep(200 * time.Millisecond)

	// 两者的 SeenCount 应该相同
	if standardDF.SeenCount() != pipelinedDF.SeenCount() {
		t.Errorf("SeenCount mismatch: standard=%d, pipelined=%d",
			standardDF.SeenCount(), pipelinedDF.SeenCount())
	}
}

// ============================================================================
// PipelinedRedisDupeFilter 关闭时排空测试
// ============================================================================

func TestPipelinedRedisDupeFilter_DrainOnClose(t *testing.T) {
	mr := miniredis.RunT(t)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "test-drain"

	// 使用较小的刷新间隔但较大的批量大小，确保关闭时有待刷新数据
	df, err := NewPipelinedRedisDupeFilter(opts,
		WithBatchSize(1000),
		WithFlushInterval(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}

	// 并发提交一些请求
	const numRequests = 10
	var wg sync.WaitGroup
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := newTestRequest(fmt.Sprintf("http://example.com/drain%d", idx))
			df.RequestSeen(req)
		}(i)
	}

	wg.Wait()

	// 关闭过滤器（应该排空缓冲区）
	if err := df.Close("test"); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 验证所有数据都已写入 Redis
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	count, err := client.SCard(context.Background(), "test-drain:dupefilter").Result()
	if err != nil {
		t.Fatalf("SCard failed: %v", err)
	}

	if int(count) != numRequests {
		t.Errorf("expected %d items in Redis after drain, got %d", numRequests, count)
	}
}

// ============================================================================
// 基准测试：逐条 vs Pipeline 吞吐量对比
// ============================================================================

func BenchmarkRedisDupeFilter_RequestSeen(b *testing.B) {
	mr := miniredis.RunT(b)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "bench-standard"

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		b.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("bench")

	requests := newBenchRequests(b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		df.RequestSeen(requests[i%len(requests)])
	}
}

func BenchmarkPipelinedRedisDupeFilter_RequestSeen(b *testing.B) {
	mr := miniredis.RunT(b)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "bench-pipelined"

	df, err := NewPipelinedRedisDupeFilter(opts,
		WithBatchSize(64),
		WithFlushInterval(50*time.Millisecond),
	)
	if err != nil {
		b.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("bench")

	requests := newBenchRequests(b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		df.RequestSeen(requests[i%len(requests)])
	}
}

func BenchmarkRedisDupeFilter_RequestSeen_Parallel(b *testing.B) {
	mr := miniredis.RunT(b)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "bench-standard-parallel"

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		b.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("bench")

	requests := newBenchRequests(10000)

	b.ResetTimer()
	var counter atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := counter.Add(1)
			df.RequestSeen(requests[int(idx)%len(requests)])
		}
	})
}

func BenchmarkPipelinedRedisDupeFilter_RequestSeen_Parallel(b *testing.B) {
	mr := miniredis.RunT(b)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "bench-pipelined-parallel"

	df, err := NewPipelinedRedisDupeFilter(opts,
		WithBatchSize(64),
		WithFlushInterval(50*time.Millisecond),
	)
	if err != nil {
		b.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("bench")

	requests := newBenchRequests(10000)

	b.ResetTimer()
	var counter atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := counter.Add(1)
			df.RequestSeen(requests[int(idx)%len(requests)])
		}
	})
}

// BenchmarkRedisDupeFilter_WithBloom_Parallel 逐条模式 + 布隆过滤器并行基准测试。
func BenchmarkRedisDupeFilter_WithBloom_Parallel(b *testing.B) {
	mr := miniredis.RunT(b)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "bench-standard-bloom"
	opts.BloomFilterEnabled = true
	opts.BloomExpectedItems = 100000
	opts.BloomFalsePositiveRate = 0.001

	df, err := NewRedisDupeFilter(opts)
	if err != nil {
		b.Fatalf("NewRedisDupeFilter failed: %v", err)
	}
	defer df.Close("bench")

	requests := newBenchRequests(10000)

	b.ResetTimer()
	var counter atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := counter.Add(1)
			df.RequestSeen(requests[int(idx)%len(requests)])
		}
	})
}

// BenchmarkPipelinedRedisDupeFilter_WithBloom_Parallel Pipeline 模式 + 布隆过滤器并行基准测试。
func BenchmarkPipelinedRedisDupeFilter_WithBloom_Parallel(b *testing.B) {
	mr := miniredis.RunT(b)

	opts := DefaultOptions()
	opts.Addr = mr.Addr()
	opts.KeyPrefix = "bench-pipelined-bloom"
	opts.BloomFilterEnabled = true
	opts.BloomExpectedItems = 100000
	opts.BloomFalsePositiveRate = 0.001

	df, err := NewPipelinedRedisDupeFilter(opts,
		WithBatchSize(64),
		WithFlushInterval(50*time.Millisecond),
	)
	if err != nil {
		b.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
	}
	defer df.Close("bench")

	requests := newBenchRequests(10000)

	b.ResetTimer()
	var counter atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := counter.Add(1)
			df.RequestSeen(requests[int(idx)%len(requests)])
		}
	})
}

// BenchmarkPipelinedRedisDupeFilter_BatchSizes 不同批量大小的性能对比。
func BenchmarkPipelinedRedisDupeFilter_BatchSizes(b *testing.B) {
	batchSizes := []int{8, 16, 32, 64, 128, 256}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("batch_%d", batchSize), func(b *testing.B) {
			mr := miniredis.RunT(b)

			opts := DefaultOptions()
			opts.Addr = mr.Addr()
			opts.KeyPrefix = fmt.Sprintf("bench-batch-%d", batchSize)

			df, err := NewPipelinedRedisDupeFilter(opts,
				WithBatchSize(batchSize),
				WithFlushInterval(50*time.Millisecond),
			)
			if err != nil {
				b.Fatalf("NewPipelinedRedisDupeFilter failed: %v", err)
			}
			defer df.Close("bench")

			requests := newBenchRequests(10000)

			b.ResetTimer()
			var counter atomic.Int64
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					idx := counter.Add(1)
					df.RequestSeen(requests[int(idx)%len(requests)])
				}
			})
		})
	}
}

// ============================================================================
// 测试辅助函数
// ============================================================================

// newTestRequest 创建一个用于 Pipeline 去重测试的 HTTP 请求。
func newTestRequest(rawURL string) *shttp.Request {
	req, _ := shttp.NewRequest(rawURL)
	return req
}

// newTestRequestWithMethod 创建一个指定 HTTP 方法的测试请求。
func newTestRequestWithMethod(rawURL, method string) *shttp.Request {
	req, _ := shttp.NewRequest(rawURL, shttp.WithMethod(method))
	return req
}

// newBenchRequests 创建一批用于基准测试的请求。
func newBenchRequests(n int) []*shttp.Request {
	if n > 10000 {
		n = 10000
	}
	requests := make([]*shttp.Request, n)
	for i := 0; i < n; i++ {
		requests[i] = newTestRequest(fmt.Sprintf("http://example.com/bench/%d", i))
	}
	return requests
}
