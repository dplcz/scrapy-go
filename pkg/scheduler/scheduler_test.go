package scheduler

import (
	"context"
	"sync"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

func TestDefaultSchedulerBasic(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	s := NewDefaultScheduler(WithStats(sc))
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	// 初始状态
	if s.HasPendingRequests() {
		t.Error("new scheduler should have no pending requests")
	}
	if s.Len() != 0 {
		t.Error("new scheduler should have length 0")
	}
	if s.NextRequest() != nil {
		t.Error("next request from empty scheduler should be nil")
	}

	// 入队
	req := shttp.MustNewRequest("https://example.com")
	if !s.EnqueueRequest(req) {
		t.Error("first request should be enqueued successfully")
	}

	if !s.HasPendingRequests() {
		t.Error("should have pending requests after enqueue")
	}
	if s.Len() != 1 {
		t.Errorf("expected length 1, got %d", s.Len())
	}

	// 出队
	dequeued := s.NextRequest()
	if dequeued == nil {
		t.Fatal("dequeued request should not be nil")
	}
	if dequeued.URL.String() != "https://example.com" {
		t.Errorf("unexpected URL: %s", dequeued.URL.String())
	}

	if s.HasPendingRequests() {
		t.Error("should have no pending requests after dequeue")
	}
}

func TestDefaultSchedulerDupeFilter(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	s := NewDefaultScheduler(WithStats(sc))
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	req1 := shttp.MustNewRequest("https://example.com/page")
	req2 := shttp.MustNewRequest("https://example.com/page") // 重复

	// 第一次入队成功
	if !s.EnqueueRequest(req1) {
		t.Error("first request should be enqueued")
	}

	// 重复请求被过滤
	if s.EnqueueRequest(req2) {
		t.Error("duplicate request should be filtered")
	}

	// 队列中只有 1 个请求
	if s.Len() != 1 {
		t.Errorf("expected length 1, got %d", s.Len())
	}

	// 验证统计
	filtered := sc.GetValue("dupefilter/filtered", 0)
	if filtered != 1 {
		t.Errorf("expected dupefilter/filtered=1, got %v", filtered)
	}
}

func TestDefaultSchedulerDontFilter(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	s := NewDefaultScheduler(WithStats(sc))
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	req1 := shttp.MustNewRequest("https://example.com/page")
	req2 := shttp.MustNewRequest("https://example.com/page",
		shttp.WithDontFilter(true),
	)

	// 第一次入队
	s.EnqueueRequest(req1)

	// DontFilter=true 的请求不被过滤
	if !s.EnqueueRequest(req2) {
		t.Error("DontFilter request should not be filtered")
	}

	// 队列中应有 2 个请求
	if s.Len() != 2 {
		t.Errorf("expected length 2, got %d", s.Len())
	}
}

func TestDefaultSchedulerPriorityOrder(t *testing.T) {
	s := NewDefaultScheduler()
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	low := shttp.MustNewRequest("https://example.com/low",
		shttp.WithPriority(1),
		shttp.WithDontFilter(true),
	)
	high := shttp.MustNewRequest("https://example.com/high",
		shttp.WithPriority(10),
		shttp.WithDontFilter(true),
	)
	mid := shttp.MustNewRequest("https://example.com/mid",
		shttp.WithPriority(5),
		shttp.WithDontFilter(true),
	)

	s.EnqueueRequest(low)
	s.EnqueueRequest(high)
	s.EnqueueRequest(mid)

	// 应按优先级从高到低出队
	first := s.NextRequest()
	if first.URL.Path != "/high" {
		t.Errorf("expected /high, got %s", first.URL.Path)
	}
	second := s.NextRequest()
	if second.URL.Path != "/mid" {
		t.Errorf("expected /mid, got %s", second.URL.Path)
	}
	third := s.NextRequest()
	if third.URL.Path != "/low" {
		t.Errorf("expected /low, got %s", third.URL.Path)
	}
}

func TestDefaultSchedulerStats(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	s := NewDefaultScheduler(WithStats(sc))
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	// 入队 3 个请求（1 个重复）
	s.EnqueueRequest(shttp.MustNewRequest("https://example.com/1"))
	s.EnqueueRequest(shttp.MustNewRequest("https://example.com/2"))
	s.EnqueueRequest(shttp.MustNewRequest("https://example.com/1")) // 重复

	// 验证入队统计
	enqueued := sc.GetValue("scheduler/enqueued", 0)
	if enqueued != 2 {
		t.Errorf("expected scheduler/enqueued=2, got %v", enqueued)
	}
	enqueuedMemory := sc.GetValue("scheduler/enqueued/memory", 0)
	if enqueuedMemory != 2 {
		t.Errorf("expected scheduler/enqueued/memory=2, got %v", enqueuedMemory)
	}

	// 出队 2 个请求
	s.NextRequest()
	s.NextRequest()

	// 验证出队统计
	dequeued := sc.GetValue("scheduler/dequeued", 0)
	if dequeued != 2 {
		t.Errorf("expected scheduler/dequeued=2, got %v", dequeued)
	}
	dequeuedMemory := sc.GetValue("scheduler/dequeued/memory", 0)
	if dequeuedMemory != 2 {
		t.Errorf("expected scheduler/dequeued/memory=2, got %v", dequeuedMemory)
	}

	// 验证去重统计
	filtered := sc.GetValue("dupefilter/filtered", 0)
	if filtered != 1 {
		t.Errorf("expected dupefilter/filtered=1, got %v", filtered)
	}
}

func TestDefaultSchedulerConcurrency(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	// 使用 NoDupeFilter 避免去重影响并发测试
	s := NewDefaultScheduler(
		WithStats(sc),
		WithDupeFilter(NewNoDupeFilter()),
	)
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	n := 1000
	var wg sync.WaitGroup

	// 并发入队
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := shttp.MustNewRequest("https://example.com/page",
				shttp.WithPriority(i%10),
				shttp.WithDontFilter(true),
			)
			s.EnqueueRequest(req)
		}(i)
	}
	wg.Wait()

	if s.Len() != n {
		t.Errorf("expected %d requests, got %d", n, s.Len())
	}

	// 并发出队
	var dequeuedCount int
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if req := s.NextRequest(); req != nil {
				mu.Lock()
				dequeuedCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if dequeuedCount != n {
		t.Errorf("expected %d dequeued, got %d", n, dequeuedCount)
	}

	if s.Len() != 0 {
		t.Errorf("expected empty queue, got %d", s.Len())
	}
}

func TestDefaultSchedulerOpenClose(t *testing.T) {
	s := NewDefaultScheduler()

	// Open
	err := s.Open(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on open: %v", err)
	}

	// 入队一些请求
	s.EnqueueRequest(shttp.MustNewRequest("https://example.com/1"))
	s.EnqueueRequest(shttp.MustNewRequest("https://example.com/2"))

	// Close
	err = s.Close(context.Background(), "finished")
	if err != nil {
		t.Fatalf("unexpected error on close: %v", err)
	}
}

func TestDefaultSchedulerWithCustomDupeFilter(t *testing.T) {
	// 使用 NoDupeFilter，所有请求都不被过滤
	s := NewDefaultScheduler(
		WithDupeFilter(NewNoDupeFilter()),
	)
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	req := shttp.MustNewRequest("https://example.com")

	// 同一请求入队多次都应成功
	for i := 0; i < 5; i++ {
		if !s.EnqueueRequest(req) {
			t.Errorf("request %d should be enqueued with NoDupeFilter", i)
		}
	}

	if s.Len() != 5 {
		t.Errorf("expected 5 requests, got %d", s.Len())
	}
}

func TestDefaultSchedulerDebugMode(t *testing.T) {
	// debug 模式不应 panic
	s := NewDefaultScheduler(WithDebug(true))
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	req := shttp.MustNewRequest("https://example.com")
	s.EnqueueRequest(req)
	s.EnqueueRequest(req) // 触发 debug 日志
}

// TestSchedulerInterface 验证 DefaultScheduler 实现了 Scheduler 接口
func TestSchedulerInterface(t *testing.T) {
	var _ Scheduler = (*DefaultScheduler)(nil)
}

// TestDupeFilterInterface 验证 RFPDupeFilter 和 NoDupeFilter 实现了 DupeFilter 接口
func TestDupeFilterInterface(t *testing.T) {
	var _ DupeFilter = (*RFPDupeFilter)(nil)
	var _ DupeFilter = (*NoDupeFilter)(nil)
}
