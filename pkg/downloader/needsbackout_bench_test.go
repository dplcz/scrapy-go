package downloader

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/settings"
)

// BenchmarkNeedsBackout_Atomic 测试使用 atomic 实现的 NeedsBackout 性能。
// 模拟调度循环中高频调用 NeedsBackout 的场景。
func BenchmarkNeedsBackout_Atomic(b *testing.B) {
	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	d := NewDownloader(s, NewHTTPDownloadHandler(10*time.Second), nil, nil, nil)
	defer d.Close()

	// 预填充一些活跃请求
	for i := 0; i < 8; i++ {
		req := shttp.MustNewRequest("http://example.com/page")
		d.AddActive(req)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			d.NeedsBackout()
		}
	})
}

// BenchmarkNeedsBackout_RWMutex_Baseline 作为对比基线，模拟旧的 RWMutex 实现。
func BenchmarkNeedsBackout_RWMutex_Baseline(b *testing.B) {
	type oldDownloader struct {
		mu               sync.RWMutex
		active           map[*shttp.Request]struct{}
		totalConcurrency int
	}

	d := &oldDownloader{
		active:           make(map[*shttp.Request]struct{}),
		totalConcurrency: 16,
	}

	// 预填充一些活跃请求
	for i := 0; i < 8; i++ {
		req := shttp.MustNewRequest("http://example.com/page")
		d.active[req] = struct{}{}
	}

	needsBackout := func() bool {
		d.mu.RLock()
		defer d.mu.RUnlock()
		return len(d.active) >= d.totalConcurrency
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			needsBackout()
		}
	})
}

// BenchmarkNeedsBackout_Contended_Atomic 测试在写竞争下 atomic NeedsBackout 的性能。
// 模拟高并发场景：多个 goroutine 同时读取 NeedsBackout，同时有 goroutine 在 AddActive/RemoveActive。
func BenchmarkNeedsBackout_Contended_Atomic(b *testing.B) {
	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 128, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	d := NewDownloader(s, NewHTTPDownloadHandler(10*time.Second), nil, nil, nil)
	defer d.Close()

	// 启动后台写入者，模拟持续的 AddActive/RemoveActive
	done := make(chan struct{})
	var writerCount atomic.Int64
	for i := 0; i < 4; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					req := shttp.MustNewRequest("http://example.com/page")
					d.AddActive(req)
					writerCount.Add(1)
					d.RemoveActive(req)
				}
			}
		}()
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			d.NeedsBackout()
		}
	})
	b.StopTimer()
	close(done)
	_ = writerCount.Load() // 防止编译器优化
}

// BenchmarkNeedsBackout_Contended_RWMutex_Baseline 作为对比基线，模拟旧的 RWMutex 在写竞争下的性能。
func BenchmarkNeedsBackout_Contended_RWMutex_Baseline(b *testing.B) {
	type oldDownloader struct {
		mu               sync.RWMutex
		active           map[*shttp.Request]struct{}
		totalConcurrency int
	}

	d := &oldDownloader{
		active:           make(map[*shttp.Request]struct{}),
		totalConcurrency: 128,
	}

	needsBackout := func() bool {
		d.mu.RLock()
		defer d.mu.RUnlock()
		return len(d.active) >= d.totalConcurrency
	}

	addActive := func(req *shttp.Request) {
		d.mu.Lock()
		d.active[req] = struct{}{}
		d.mu.Unlock()
	}

	removeActive := func(req *shttp.Request) {
		d.mu.Lock()
		delete(d.active, req)
		d.mu.Unlock()
	}

	// 启动后台写入者
	done := make(chan struct{})
	var writerCount atomic.Int64
	for i := 0; i < 4; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					req := shttp.MustNewRequest("http://example.com/page")
					addActive(req)
					writerCount.Add(1)
					removeActive(req)
				}
			}
		}()
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			needsBackout()
		}
	})
	b.StopTimer()
	close(done)
	_ = writerCount.Load()
}

// BenchmarkAddRemoveActive_Atomic 测试 AddActive/RemoveActive 的吞吐量。
func BenchmarkAddRemoveActive_Atomic(b *testing.B) {
	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 128, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	d := NewDownloader(s, NewHTTPDownloadHandler(10*time.Second), nil, nil, nil)
	defer d.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := shttp.MustNewRequest("http://example.com/page")
			d.AddActive(req)
			d.RemoveActive(req)
		}
	})
}

// BenchmarkAddRemoveActive_RWMutex_Baseline 作为对比基线。
func BenchmarkAddRemoveActive_RWMutex_Baseline(b *testing.B) {
	type oldDownloader struct {
		mu               sync.RWMutex
		active           map[*shttp.Request]struct{}
		totalConcurrency int
	}

	d := &oldDownloader{
		active:           make(map[*shttp.Request]struct{}),
		totalConcurrency: 128,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := shttp.MustNewRequest("http://example.com/page")
			d.mu.Lock()
			d.active[req] = struct{}{}
			d.mu.Unlock()
			d.mu.Lock()
			delete(d.active, req)
			d.mu.Unlock()
		}
	})
}
