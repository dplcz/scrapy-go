package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// BenchmarkSchedulerEnqueueDequeue 测试入队/出队的基本性能。
func BenchmarkSchedulerEnqueueDequeue(b *testing.B) {
	s := NewDefaultScheduler(
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
}

// BenchmarkSchedulerConcurrentEnqueueDequeue 测试并发入队/出队性能。
// 模拟真实场景：多个 goroutine 并发入队（Spider 回调），单个 goroutine 出队（调度循环）。
func BenchmarkSchedulerConcurrentEnqueueDequeue(b *testing.B) {
	concurrencyLevels := []int{8, 16, 32, 64, 128}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("c%d", concurrency), func(b *testing.B) {
			s := NewDefaultScheduler(
				WithDupeFilter(NewNoDupeFilter()),
			)
			s.Open(context.Background())
			defer s.Close(context.Background(), "finished")

			req := shttp.MustNewRequest("https://example.com/bench",
				shttp.WithDontFilter(true),
			)

			b.ResetTimer()

			var wg sync.WaitGroup
			var enqueued atomic.Int64

			// 启动多个入队 goroutine（模拟 Spider 回调）
			for g := 0; g < concurrency; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						n := enqueued.Add(1)
						if n > int64(b.N) {
							return
						}
						s.EnqueueRequest(req)
					}
				}()
			}

			// 主 goroutine 出队（模拟调度循环）
			dequeued := 0
			for dequeued < b.N {
				if r := s.NextRequest(); r != nil {
					dequeued++
				}
			}

			wg.Wait()
		})
	}
}

// BenchmarkSchedulerParallelEnqueueDequeue 测试入队和出队完全并行的场景。
func BenchmarkSchedulerParallelEnqueueDequeue(b *testing.B) {
	concurrencyLevels := []int{8, 16, 32, 64, 128}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("c%d", concurrency), func(b *testing.B) {
			s := NewDefaultScheduler(
				WithDupeFilter(NewNoDupeFilter()),
			)
			s.Open(context.Background())
			defer s.Close(context.Background(), "finished")

			req := shttp.MustNewRequest("https://example.com/bench",
				shttp.WithDontFilter(true),
			)

			// 预填充一些请求
			for i := 0; i < concurrency*10; i++ {
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

// BenchmarkDupeFilterRequestSeen 测试去重过滤器的并发性能。
func BenchmarkDupeFilterRequestSeen(b *testing.B) {
	concurrencyLevels := []int{1, 8, 32, 96}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("c%d", concurrency), func(b *testing.B) {
			df := NewRFPDupeFilter(nil, false)
			df.Open(context.Background())
			defer df.Close("finished")

			// 预生成不同的请求（避免全部命中缓存）
			requests := make([]*shttp.Request, 1000)
			for i := range requests {
				requests[i] = shttp.MustNewRequest(fmt.Sprintf("https://example.com/%d", i))
			}

			b.ResetTimer()
			b.SetParallelism(concurrency)
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					df.RequestSeen(requests[i%len(requests)])
					i++
				}
			})
		})
	}
}

// BenchmarkSchedulerHasPendingRequests 测试 HasPendingRequests 的无锁快速路径。
func BenchmarkSchedulerHasPendingRequests(b *testing.B) {
	s := NewDefaultScheduler(
		WithDupeFilter(NewNoDupeFilter()),
	)
	s.Open(context.Background())
	defer s.Close(context.Background(), "finished")

	// 入队一些请求
	for i := 0; i < 100; i++ {
		s.EnqueueRequest(shttp.MustNewRequest("https://example.com/bench",
			shttp.WithDontFilter(true),
		))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.HasPendingRequests()
		}
	})
}
