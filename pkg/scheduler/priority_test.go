package scheduler

import (
	"context"
	"sync"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// ============================================================================
// 跨批次优先级排序正确性测试
// ============================================================================

// TestCrossBatchPriorityOrdering 验证跨批次入队时全局优先级排序的正确性。
//
// 场景：第一批大量低优先级请求入队后，部分被消费，然后第二批高优先级请求入队。
// 期望：高优先级请求应立即排在剩余低优先级请求之前出队。
//
// 这是单队列设计（方案 E）修复的核心问题：双锁分离设计中，
// 高优先级请求被困在 inBuffer 中，必须等 outQueue 耗尽才能出队。
func TestCrossBatchPriorityOrdering(t *testing.T) {
	s := NewDefaultScheduler(
		WithDupeFilter(NewNoDupeFilter()),
	)
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	// 第一批：入队 100 个低优先级请求 (priority=0)
	for i := 0; i < 100; i++ {
		req := shttp.MustNewRequest("https://example.com/low",
			shttp.WithPriority(0),
			shttp.WithDontFilter(true),
		)
		s.EnqueueRequest(req)
	}

	// 消费部分请求（模拟 Engine 调度循环消费了一些）
	for i := 0; i < 10; i++ {
		req := s.NextRequest()
		if req == nil {
			t.Fatalf("expected request at position %d", i)
		}
	}

	// 第二批：入队 5 个高优先级请求 (priority=10)
	for i := 0; i < 5; i++ {
		req := shttp.MustNewRequest("https://example.com/high",
			shttp.WithPriority(10),
			shttp.WithDontFilter(true),
		)
		s.EnqueueRequest(req)
	}

	// 验证：接下来出队的 5 个请求应该全部是高优先级的
	for i := 0; i < 5; i++ {
		req := s.NextRequest()
		if req == nil {
			t.Fatalf("expected high priority request at position %d", i)
		}
		if req.Priority != 10 {
			t.Errorf("position %d: expected priority=10, got priority=%d (URL=%s)",
				i, req.Priority, req.URL.Path)
		}
	}

	// 剩余的应该全部是低优先级请求
	remaining := 0
	for {
		req := s.NextRequest()
		if req == nil {
			break
		}
		if req.Priority != 0 {
			t.Errorf("remaining request should be priority=0, got %d", req.Priority)
		}
		remaining++
	}

	if remaining != 90 {
		t.Errorf("expected 90 remaining low priority requests, got %d", remaining)
	}
}

// TestCrossBatchPriorityWithConcurrentEnqueue 验证并发入队时优先级排序的正确性。
//
// 场景：多个 goroutine 并发入队不同优先级的请求，同时有消费者出队。
// 期望：出队顺序严格按照全局优先级排序（高优先级先出）。
func TestCrossBatchPriorityWithConcurrentEnqueue(t *testing.T) {
	s := NewDefaultScheduler(
		WithDupeFilter(NewNoDupeFilter()),
	)
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	// 先入队一批低优先级请求
	for i := 0; i < 50; i++ {
		req := shttp.MustNewRequest("https://example.com/low",
			shttp.WithPriority(1),
			shttp.WithDontFilter(true),
		)
		s.EnqueueRequest(req)
	}

	// 消费 10 个（触发旧设计中的 swap）
	for i := 0; i < 10; i++ {
		s.NextRequest()
	}

	// 并发入队高优先级请求（模拟多个 Spider 回调同时返回高优先级请求）
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				req := shttp.MustNewRequest("https://example.com/high",
					shttp.WithPriority(100),
					shttp.WithDontFilter(true),
				)
				s.EnqueueRequest(req)
			}
		}()
	}
	wg.Wait()

	// 验证：接下来出队的 20 个请求应该全部是高优先级的
	highCount := 0
	for i := 0; i < 20; i++ {
		req := s.NextRequest()
		if req == nil {
			t.Fatalf("expected request at position %d", i)
		}
		if req.Priority == 100 {
			highCount++
		} else {
			t.Errorf("position %d: expected priority=100, got priority=%d",
				i, req.Priority)
		}
	}

	if highCount != 20 {
		t.Errorf("expected 20 high priority requests first, got %d", highCount)
	}
}

// TestPriorityInterleavedEnqueueDequeue 验证交错入队/出队时优先级的正确性。
//
// 场景：入队和出队交替进行，每次入队后立即出队，验证始终出队最高优先级。
func TestPriorityInterleavedEnqueueDequeue(t *testing.T) {
	s := NewDefaultScheduler(
		WithDupeFilter(NewNoDupeFilter()),
	)
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	// 入队 priority=1
	s.EnqueueRequest(shttp.MustNewRequest("https://example.com/p1",
		shttp.WithPriority(1), shttp.WithDontFilter(true)))

	// 入队 priority=5
	s.EnqueueRequest(shttp.MustNewRequest("https://example.com/p5",
		shttp.WithPriority(5), shttp.WithDontFilter(true)))

	// 出队应该是 priority=5
	req := s.NextRequest()
	if req.Priority != 5 {
		t.Errorf("expected priority=5, got %d", req.Priority)
	}

	// 入队 priority=10
	s.EnqueueRequest(shttp.MustNewRequest("https://example.com/p10",
		shttp.WithPriority(10), shttp.WithDontFilter(true)))

	// 出队应该是 priority=10（而不是之前剩余的 priority=1）
	req = s.NextRequest()
	if req.Priority != 10 {
		t.Errorf("expected priority=10, got %d", req.Priority)
	}

	// 最后出队 priority=1
	req = s.NextRequest()
	if req.Priority != 1 {
		t.Errorf("expected priority=1, got %d", req.Priority)
	}

	// 队列应为空
	if s.NextRequest() != nil {
		t.Error("expected empty queue")
	}
}

// TestPriorityUnderHighConcurrency 在高并发下验证优先级排序的最终一致性。
//
// 场景：大量 goroutine 并发入队不同优先级的请求，单消费者出队。
// 期望：出队序列整体呈优先级递减趋势（允许同优先级内的 LIFO 乱序）。
func TestPriorityUnderHighConcurrency(t *testing.T) {
	s := NewDefaultScheduler(
		WithDupeFilter(NewNoDupeFilter()),
	)
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	const (
		numProducers    = 8
		reqsPerProducer = 100
		totalReqs       = numProducers * reqsPerProducer
	)

	// 并发入队：每个 producer 入队不同优先级的请求
	var wg sync.WaitGroup
	for p := 0; p < numProducers; p++ {
		wg.Add(1)
		go func(priority int) {
			defer wg.Done()
			for i := 0; i < reqsPerProducer; i++ {
				req := shttp.MustNewRequest("https://example.com/page",
					shttp.WithPriority(priority),
					shttp.WithDontFilter(true),
				)
				s.EnqueueRequest(req)
			}
		}(p * 10) // priorities: 0, 10, 20, 30, 40, 50, 60, 70
	}
	wg.Wait()

	if s.Len() != totalReqs {
		t.Fatalf("expected %d requests, got %d", totalReqs, s.Len())
	}

	// 出队并验证优先级单调递减
	var lastPriority int = 1000 // 初始值大于任何可能的优先级
	violations := 0
	for i := 0; i < totalReqs; i++ {
		req := s.NextRequest()
		if req == nil {
			t.Fatalf("unexpected nil at position %d", i)
		}
		if req.Priority > lastPriority {
			violations++
		}
		lastPriority = req.Priority
	}

	// 单队列设计应该保证零违规
	if violations > 0 {
		t.Errorf("priority ordering violations: %d (should be 0 with single queue design)", violations)
	}
}

// BenchmarkCrossBatchPriority 基准测试：模拟跨批次优先级场景的性能。
//
// 模拟真实爬虫场景：大量低优先级请求 + 少量高优先级请求交替入队/出队。
func BenchmarkCrossBatchPriority(b *testing.B) {
	s := NewDefaultScheduler(
		WithDupeFilter(NewNoDupeFilter()),
	)
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	lowReq := shttp.MustNewRequest("https://example.com/low",
		shttp.WithPriority(0), shttp.WithDontFilter(true))
	highReq := shttp.MustNewRequest("https://example.com/high",
		shttp.WithPriority(10), shttp.WithDontFilter(true))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 入队 10 个低优先级
		for j := 0; j < 10; j++ {
			s.EnqueueRequest(lowReq)
		}
		// 消费 5 个
		for j := 0; j < 5; j++ {
			s.NextRequest()
		}
		// 入队 2 个高优先级
		s.EnqueueRequest(highReq)
		s.EnqueueRequest(highReq)
		// 消费剩余 7 个
		for j := 0; j < 7; j++ {
			s.NextRequest()
		}
	}
}
