package scheduler

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// P5-017 内存队列溢出优化 — 单元测试
// ============================================================================

// TestMemoryQueueThresholdBasic 验证基本的阈值触发溢出行为。
func TestMemoryQueueThresholdBasic(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(5),
		WithDupeFilter(NewNoDupeFilter()),
		WithStats(sc),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 验证磁盘队列已自动创建
	if !s.HasExternalQueue() {
		t.Fatal("expected disk queue to be created for overflow protection")
	}

	// 入队 5 个请求（不超过阈值），应全部存入内存
	for i := 0; i < 5; i++ {
		req := shttp.MustNewRequest(fmt.Sprintf("https://example.com/%d", i),
			shttp.WithDontFilter(true),
		)
		if !s.EnqueueRequest(req) {
			t.Fatalf("request %d should be enqueued", i)
		}
	}

	if s.MemoryQueueLen() != 5 {
		t.Errorf("expected memory queue len=5, got %d", s.MemoryQueueLen())
	}

	// 入队第 6 个请求（超过阈值），应溢出到磁盘
	req := shttp.MustNewRequest("https://example.com/overflow",
		shttp.WithDontFilter(true),
	)
	if !s.EnqueueRequest(req) {
		t.Fatal("overflow request should be enqueued")
	}

	// 内存队列数量不应增加
	if s.MemoryQueueLen() != 5 {
		t.Errorf("expected memory queue len=5 after overflow, got %d", s.MemoryQueueLen())
	}

	// 总队列长度应为 6
	if s.Len() != 6 {
		t.Errorf("expected total len=6, got %d", s.Len())
	}

	// 验证溢出统计
	overflowCount := sc.GetValue("scheduler/overflow_to_disk", 0)
	if overflowCount != 1 {
		t.Errorf("expected scheduler/overflow_to_disk=1, got %v", overflowCount)
	}
}

// TestMemoryQueueThresholdZero 验证阈值为 0 时不限制（默认行为）。
func TestMemoryQueueThresholdZero(t *testing.T) {
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(0), // 不限制
		WithDupeFilter(NewNoDupeFilter()),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 不应创建磁盘队列
	if s.HasExternalQueue() {
		t.Fatal("threshold=0 should not create disk queue")
	}

	// 验证阈值返回值
	if s.MemoryQueueThreshold() != 0 {
		t.Errorf("expected threshold=0, got %d", s.MemoryQueueThreshold())
	}
}

// TestMemoryQueueThresholdNegative 验证负数阈值被忽略。
func TestMemoryQueueThresholdNegative(t *testing.T) {
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(-1), // 无效值，应被忽略
		WithDupeFilter(NewNoDupeFilter()),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 不应创建磁盘队列
	if s.HasExternalQueue() {
		t.Fatal("negative threshold should not create disk queue")
	}

	if s.MemoryQueueThreshold() != 0 {
		t.Errorf("expected threshold=0 for negative input, got %d", s.MemoryQueueThreshold())
	}
}

// TestMemoryQueueThresholdWithJobDir 验证同时设置阈值和 jobDir 时的行为。
func TestMemoryQueueThresholdWithJobDir(t *testing.T) {
	tmpDir := t.TempDir()
	sc := stats.NewMemoryCollector(false, nil)
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(3),
		WithJobDir(tmpDir),
		WithDupeFilter(NewNoDupeFilter()),
		WithStats(sc),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// jobDir 优先级高于自动创建临时目录
	if !s.HasExternalQueue() {
		t.Fatal("expected disk queue from jobDir")
	}

	// 入队 3 个请求（不超过阈值），应存入内存
	for i := 0; i < 3; i++ {
		req := shttp.MustNewRequest(fmt.Sprintf("https://example.com/%d", i),
			shttp.WithDontFilter(true),
		)
		s.EnqueueRequest(req)
	}

	if s.MemoryQueueLen() != 3 {
		t.Errorf("expected memory queue len=3, got %d", s.MemoryQueueLen())
	}

	// 入队第 4 个请求（超过阈值），应溢出到磁盘
	req := shttp.MustNewRequest("https://example.com/overflow",
		shttp.WithDontFilter(true),
	)
	s.EnqueueRequest(req)

	if s.MemoryQueueLen() != 3 {
		t.Errorf("expected memory queue len=3 after overflow, got %d", s.MemoryQueueLen())
	}

	overflowCount := sc.GetValue("scheduler/overflow_to_disk", 0)
	if overflowCount != 1 {
		t.Errorf("expected scheduler/overflow_to_disk=1, got %v", overflowCount)
	}
}

// TestMemoryQueueThresholdDequeueOrder 验证溢出后出队优先级正确性。
// 内存队列应优先于磁盘队列出队。
func TestMemoryQueueThresholdDequeueOrder(t *testing.T) {
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(2),
		WithDupeFilter(NewNoDupeFilter()),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 入队 2 个高优先级请求到内存
	highReq1 := shttp.MustNewRequest("https://example.com/high1",
		shttp.WithPriority(10),
		shttp.WithDontFilter(true),
	)
	highReq2 := shttp.MustNewRequest("https://example.com/high2",
		shttp.WithPriority(10),
		shttp.WithDontFilter(true),
	)
	s.EnqueueRequest(highReq1)
	s.EnqueueRequest(highReq2)

	// 入队 2 个低优先级请求（超过阈值，溢出到磁盘）
	lowReq1 := shttp.MustNewRequest("https://example.com/low1",
		shttp.WithPriority(1),
		shttp.WithDontFilter(true),
	)
	lowReq2 := shttp.MustNewRequest("https://example.com/low2",
		shttp.WithPriority(1),
		shttp.WithDontFilter(true),
	)
	s.EnqueueRequest(lowReq1)
	s.EnqueueRequest(lowReq2)

	// 验证总数
	if s.Len() != 4 {
		t.Fatalf("expected total len=4, got %d", s.Len())
	}

	// 出队：内存队列优先，高优先级先出
	first := s.NextRequest()
	if first == nil || first.URL.Path != "/high2" && first.URL.Path != "/high1" {
		path := ""
		if first != nil {
			path = first.URL.Path
		}
		t.Errorf("expected high priority request first, got %s", path)
	}

	second := s.NextRequest()
	if second == nil {
		t.Fatal("expected second request")
	}

	// 内存队列出完后，从磁盘队列出队
	third := s.NextRequest()
	if third == nil {
		t.Fatal("expected third request from disk queue")
	}

	fourth := s.NextRequest()
	if fourth == nil {
		t.Fatal("expected fourth request from disk queue")
	}

	// 队列应为空
	if s.HasPendingRequests() {
		t.Error("expected empty queue after dequeuing all")
	}
}

// TestMemoryQueueThresholdTempDirCleanup 验证临时目录在 Close 后被清理。
func TestMemoryQueueThresholdTempDirCleanup(t *testing.T) {
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(10),
		WithDupeFilter(NewNoDupeFilter()),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}

	// 获取临时目录路径
	tempDir := s.tempDir
	if tempDir == "" {
		t.Fatal("expected temp dir to be created")
	}

	// 验证目录存在
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Fatal("temp dir should exist before close")
	}

	// 入队一些请求
	for i := 0; i < 15; i++ {
		req := shttp.MustNewRequest(fmt.Sprintf("https://example.com/%d", i),
			shttp.WithDontFilter(true),
		)
		s.EnqueueRequest(req)
	}

	// 关闭调度器
	if err := s.Close(context.Background(), "finished"); err != nil {
		t.Fatalf("failed to close scheduler: %v", err)
	}

	// 验证目录已被清理
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("temp dir should be removed after close, err=%v", err)
	}
}

// TestMemoryQueueThresholdConcurrency 验证并发场景下溢出保护的正确性。
func TestMemoryQueueThresholdConcurrency(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(100),
		WithDupeFilter(NewNoDupeFilter()),
		WithStats(sc),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	n := 500
	var wg sync.WaitGroup

	// 并发入队
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := shttp.MustNewRequest(fmt.Sprintf("https://example.com/%d", i),
				shttp.WithPriority(i%10),
				shttp.WithDontFilter(true),
			)
			s.EnqueueRequest(req)
		}(i)
	}
	wg.Wait()

	// 验证总数正确
	if s.Len() != n {
		t.Errorf("expected %d requests, got %d", n, s.Len())
	}

	// 内存队列不应超过阈值太多（由于并发，可能略微超过）
	memLen := s.MemoryQueueLen()
	if memLen > 200 { // 允许一定的并发误差，但不应远超阈值
		t.Errorf("memory queue len=%d, significantly exceeds threshold=100", memLen)
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

// TestMemoryQueueThresholdAllDequeued 验证所有请求（内存+磁盘）都能被正确出队。
func TestMemoryQueueThresholdAllDequeued(t *testing.T) {
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(5),
		WithDupeFilter(NewNoDupeFilter()),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	total := 20
	for i := 0; i < total; i++ {
		req := shttp.MustNewRequest(fmt.Sprintf("https://example.com/%d", i),
			shttp.WithDontFilter(true),
		)
		s.EnqueueRequest(req)
	}

	// 出队所有请求
	dequeued := 0
	for s.HasPendingRequests() {
		if req := s.NextRequest(); req != nil {
			dequeued++
		}
	}

	if dequeued != total {
		t.Errorf("expected %d dequeued, got %d", total, dequeued)
	}
}

// TestMemoryQueueThresholdWithExternalQueue 验证设置了外部队列时阈值仍然生效。
func TestMemoryQueueThresholdWithExternalQueue(t *testing.T) {
	tmpDir := t.TempDir()
	dq, err := NewDiskQueue(tmpDir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}

	sc := stats.NewMemoryCollector(false, nil)
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(3),
		WithExternalQueue(dq),
		WithDupeFilter(NewNoDupeFilter()),
		WithStats(sc),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	// 入队 3 个请求（不超过阈值），应存入内存
	for i := 0; i < 3; i++ {
		req := shttp.MustNewRequest(fmt.Sprintf("https://example.com/%d", i),
			shttp.WithDontFilter(true),
		)
		s.EnqueueRequest(req)
	}

	if s.MemoryQueueLen() != 3 {
		t.Errorf("expected memory queue len=3, got %d", s.MemoryQueueLen())
	}

	// 入队第 4 个请求（超过阈值），应溢出到外部队列
	req := shttp.MustNewRequest("https://example.com/overflow",
		shttp.WithDontFilter(true),
	)
	s.EnqueueRequest(req)

	if s.MemoryQueueLen() != 3 {
		t.Errorf("expected memory queue len=3 after overflow, got %d", s.MemoryQueueLen())
	}

	overflowCount := sc.GetValue("scheduler/overflow_to_disk", 0)
	if overflowCount != 1 {
		t.Errorf("expected scheduler/overflow_to_disk=1, got %v", overflowCount)
	}
}

// TestMemoryQueueThresholdAccessors 验证 MemoryQueueLen 和 MemoryQueueThreshold 方法。
func TestMemoryQueueThresholdAccessors(t *testing.T) {
	s := NewDefaultScheduler(
		WithMemoryQueueThreshold(42),
		WithDupeFilter(NewNoDupeFilter()),
	)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("failed to open scheduler: %v", err)
	}
	defer s.Close(context.Background(), "finished")

	if s.MemoryQueueThreshold() != 42 {
		t.Errorf("expected threshold=42, got %d", s.MemoryQueueThreshold())
	}

	if s.MemoryQueueLen() != 0 {
		t.Errorf("expected memory queue len=0, got %d", s.MemoryQueueLen())
	}

	// 入队一些请求
	for i := 0; i < 10; i++ {
		req := shttp.MustNewRequest(fmt.Sprintf("https://example.com/%d", i),
			shttp.WithDontFilter(true),
		)
		s.EnqueueRequest(req)
	}

	if s.MemoryQueueLen() != 10 {
		t.Errorf("expected memory queue len=10, got %d", s.MemoryQueueLen())
	}

	// 出队一些请求
	s.NextRequest()
	s.NextRequest()

	if s.MemoryQueueLen() != 8 {
		t.Errorf("expected memory queue len=8, got %d", s.MemoryQueueLen())
	}
}

// ============================================================================
// P5-017 内存队列溢出优化 — 基准测试
// ============================================================================

// BenchmarkSchedulerWithOverflow 测试启用溢出保护时的入队/出队性能。
func BenchmarkSchedulerWithOverflow(b *testing.B) {
	thresholds := []int{100, 1000, 10000}

	for _, threshold := range thresholds {
		b.Run(fmt.Sprintf("threshold_%d", threshold), func(b *testing.B) {
			s := NewDefaultScheduler(
				WithMemoryQueueThreshold(threshold),
				WithDupeFilter(NewNoDupeFilter()),
			)
			s.Open(context.Background())
			defer s.Close(context.Background(), "finished")

			req := shttp.MustNewRequest("https://example.com/bench",
				shttp.WithDontFilter(true),
			)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.EnqueueRequest(req)
				s.NextRequest()
			}
		})
	}
}

// BenchmarkSchedulerOverflowBurst 测试突发大量请求时溢出保护的性能。
// 模拟真实场景：先大量入队（触发溢出），再逐步出队。
func BenchmarkSchedulerOverflowBurst(b *testing.B) {
	burstSizes := []int{1000, 5000, 10000}

	for _, burstSize := range burstSizes {
		b.Run(fmt.Sprintf("burst_%d", burstSize), func(b *testing.B) {
			threshold := burstSize / 10 // 阈值为突发量的 10%

			s := NewDefaultScheduler(
				WithMemoryQueueThreshold(threshold),
				WithDupeFilter(NewNoDupeFilter()),
			)
			s.Open(context.Background())
			defer s.Close(context.Background(), "finished")

			req := shttp.MustNewRequest("https://example.com/bench",
				shttp.WithDontFilter(true),
			)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// 突发入队
				for j := 0; j < burstSize; j++ {
					s.EnqueueRequest(req)
				}
				// 全部出队
				for s.HasPendingRequests() {
					s.NextRequest()
				}
			}
		})
	}
}

// BenchmarkSchedulerMemoryComparison 对比有无溢出保护时的内存占用。
func BenchmarkSchedulerMemoryComparison(b *testing.B) {
	requestCount := 50000

	b.Run("no_threshold", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := NewDefaultScheduler(
				WithDupeFilter(NewNoDupeFilter()),
			)
			s.Open(context.Background())

			req := shttp.MustNewRequest("https://example.com/bench",
				shttp.WithDontFilter(true),
			)

			for j := 0; j < requestCount; j++ {
				s.EnqueueRequest(req)
			}

			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			b.ReportMetric(float64(m.Alloc)/1024/1024, "MB_alloc")

			// 出队所有
			for s.HasPendingRequests() {
				s.NextRequest()
			}
			s.Close(context.Background(), "finished")
		}
	})

	b.Run("with_threshold_5000", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s := NewDefaultScheduler(
				WithMemoryQueueThreshold(5000),
				WithDupeFilter(NewNoDupeFilter()),
			)
			s.Open(context.Background())

			req := shttp.MustNewRequest("https://example.com/bench",
				shttp.WithDontFilter(true),
			)

			for j := 0; j < requestCount; j++ {
				s.EnqueueRequest(req)
			}

			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			b.ReportMetric(float64(m.Alloc)/1024/1024, "MB_alloc")

			// 出队所有
			for s.HasPendingRequests() {
				s.NextRequest()
			}
			s.Close(context.Background(), "finished")
		}
	})
}

// BenchmarkSchedulerOverflowConcurrent 测试并发场景下溢出保护的性能。
func BenchmarkSchedulerOverflowConcurrent(b *testing.B) {
	concurrencyLevels := []int{8, 32, 64}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("c%d", concurrency), func(b *testing.B) {
			s := NewDefaultScheduler(
				WithMemoryQueueThreshold(1000),
				WithDupeFilter(NewNoDupeFilter()),
			)
			s.Open(context.Background())
			defer s.Close(context.Background(), "finished")

			req := shttp.MustNewRequest("https://example.com/bench",
				shttp.WithDontFilter(true),
			)

			// 预填充到阈值
			for i := 0; i < 1000; i++ {
				s.EnqueueRequest(req)
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					s.EnqueueRequest(req)
					s.NextRequest()
				}
			})
		})
	}
}
