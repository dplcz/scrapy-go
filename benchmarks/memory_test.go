package benchmarks

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/benchmarks/server"
	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 内存基准测试 Spider
// ============================================================================

// memoryBenchSpider 是用于内存基准测试的 Spider。
// 生成大量请求并跟踪内存使用情况。
type memoryBenchSpider struct {
	spider.Base
	benchURL     string
	totalReqs    int
	requestsSent atomic.Int64
}

// newMemoryBenchSpider 创建内存基准测试 Spider。
func newMemoryBenchSpider(benchURL string, totalReqs int) *memoryBenchSpider {
	return &memoryBenchSpider{
		Base: spider.Base{
			SpiderName: "memory_bench",
		},
		benchURL:  benchURL,
		totalReqs: totalReqs,
	}
}

// Start 生成指定数量的请求。
func (s *memoryBenchSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output, 100)
	go func() {
		defer close(ch)
		for i := 0; i < s.totalReqs; i++ {
			req, err := shttp.NewRequest(s.benchURL,
				shttp.WithDontFilter(true),
			)
			if err != nil {
				continue
			}
			s.requestsSent.Add(1)
			select {
			case <-ctx.Done():
				return
			case ch <- spider.Output{Request: req}:
			}
		}
	}()
	return ch
}

// Parse 空回调。
func (s *memoryBenchSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

// ============================================================================
// 内存基准测试（Go Benchmark 框架）
// ============================================================================

// BenchmarkMemory_10kRequests 测试 1 万请求的内存占用。
func BenchmarkMemory_10kRequests(b *testing.B) {
	benchmarkMemory(b, 10000)
}

// BenchmarkMemory_50kRequests 测试 5 万请求的内存占用。
func BenchmarkMemory_50kRequests(b *testing.B) {
	benchmarkMemory(b, 50000)
}

// BenchmarkMemory_100kRequests 测试 10 万请求的内存占用（验收标准：增长 < 500MB）。
func BenchmarkMemory_100kRequests(b *testing.B) {
	benchmarkMemory(b, 100000)
}

// benchmarkMemory 执行内存基准测试。
func benchmarkMemory(b *testing.B, totalRequests int) {
	b.Helper()

	// 启动本地 benchmark 服务器
	srv := server.New()
	addr, err := srv.Start()
	if err != nil {
		b.Fatalf("failed to start benchmark server: %v", err)
	}
	defer srv.Close()

	benchURL := "http://" + addr + "/"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 强制 GC 获取基线内存
		runtime.GC()
		var memBefore runtime.MemStats
		runtime.ReadMemStats(&memBefore)

		// 配置 Crawler
		s := settings.New()
		s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityProject)
		s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 16, settings.PriorityProject)
		s.Set("DOWNLOAD_DELAY", 0, settings.PriorityProject)
		s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityProject)
		s.Set("LOG_LEVEL", "ERROR", settings.PriorityProject)
		s.Set("RETRY_ENABLED", false, settings.PriorityProject)
		s.Set("COOKIES_ENABLED", false, settings.PriorityProject)
		s.Set("ROBOTSTXT_OBEY", false, settings.PriorityProject)
		s.Set("COMPRESSION_ENABLED", false, settings.PriorityProject)
		s.Set("HTTPPROXY_ENABLED", false, settings.PriorityProject)
		s.Set("DOWNLOADER_STATS", false, settings.PriorityProject)

		c := crawler.New(crawler.WithSettings(s))
		sp := newMemoryBenchSpider(benchURL, totalRequests)

		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		err := c.Crawl(ctx, sp)
		cancel()

		if err != nil {
			b.Logf("crawl finished with error: %v", err)
		}

		// 测量内存增长
		runtime.GC()
		var memAfter runtime.MemStats
		runtime.ReadMemStats(&memAfter)

		allocBytes := memAfter.TotalAlloc - memBefore.TotalAlloc
		heapInUse := memAfter.HeapInuse

		b.ReportMetric(float64(allocBytes)/(1024*1024), "total_alloc_MB")
		b.ReportMetric(float64(heapInUse)/(1024*1024), "heap_inuse_MB")
		b.ReportMetric(float64(allocBytes)/float64(totalRequests), "bytes/request")
	}

	b.StopTimer()
}

// ============================================================================
// 内存验收测试（非 benchmark，用于 CI 验证）
// ============================================================================

// TestMemoryAcceptance_100kRequests 验证 10 万请求内存增长 < 500MB 的验收标准。
func TestMemoryAcceptance_100kRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory acceptance test in short mode")
	}

	// 启动本地 benchmark 服务器
	srv := server.New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start benchmark server: %v", err)
	}
	defer srv.Close()

	benchURL := "http://" + addr + "/"
	totalRequests := 100000
	concurrency := 16

	// 强制 GC 获取基线
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// 配置 Crawler
	s := settings.New()
	s.Set("CONCURRENT_REQUESTS", concurrency, settings.PriorityProject)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", concurrency, settings.PriorityProject)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityProject)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityProject)
	s.Set("LOG_LEVEL", "ERROR", settings.PriorityProject)
	s.Set("RETRY_ENABLED", false, settings.PriorityProject)
	s.Set("COOKIES_ENABLED", false, settings.PriorityProject)
	s.Set("ROBOTSTXT_OBEY", false, settings.PriorityProject)
	s.Set("COMPRESSION_ENABLED", false, settings.PriorityProject)
	s.Set("HTTPPROXY_ENABLED", false, settings.PriorityProject)
	s.Set("DOWNLOADER_STATS", false, settings.PriorityProject)

	c := crawler.New(crawler.WithSettings(s))
	sp := newMemoryBenchSpider(benchURL, totalRequests)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	err = c.Crawl(ctx, sp)
	cancel()
	elapsed := time.Since(start)

	if err != nil {
		t.Logf("crawl finished with error: %v", err)
	}

	// 测量内存
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	totalAllocMB := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / (1024 * 1024)
	heapInUseMB := float64(memAfter.HeapInuse) / (1024 * 1024)
	sysMB := float64(memAfter.Sys) / (1024 * 1024)
	heapObjectsAfter := memAfter.HeapObjects
	numGC := memAfter.NumGC - memBefore.NumGC
	bytesPerReq := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / float64(totalRequests)

	serverStats := srv.GetStats()

	t.Logf("Memory Acceptance Test Results:")
	t.Logf("  Total Requests:    %d", totalRequests)
	t.Logf("  Completed:         %d", serverStats.TotalRequests)
	t.Logf("  Concurrency:       %d", concurrency)
	t.Logf("  Elapsed:           %v", elapsed)
	t.Logf("  Total Alloc:       %.2f MB (cumulative, includes GC'd memory)", totalAllocMB)
	t.Logf("  Heap In-Use:       %.2f MB (actual memory footprint)", heapInUseMB)
	t.Logf("  System Memory:     %.2f MB", sysMB)
	t.Logf("  Heap Objects:      %d", heapObjectsAfter)
	t.Logf("  Bytes/Request:     %.2f (avg alloc per request)", bytesPerReq)
	t.Logf("  GC Cycles:         %d", numGC)
	t.Logf("  GOMAXPROCS:        %d", runtime.GOMAXPROCS(0))

	// 验收标准：10 万请求内存增长 < 500MB
	// 使用 Sys（进程向 OS 申请的总内存）作为判断依据，
	// 这是最接近实际内存占用的指标。
	// HeapInuse 是当前堆使用量，Sys 包含堆 + 栈 + 其他运行时开销。
	const maxMemoryMB = 500.0
	if sysMB > maxMemoryMB {
		t.Errorf("memory acceptance failed: system memory %.2f MB > %.2f MB limit", sysMB, maxMemoryMB)
	}
}

// TestMemoryProfile_GrowthRate 测试内存增长率，确保无内存泄漏。
// 通过分阶段爬取并比较各阶段的堆内存使用，验证内存不会持续增长。
func TestMemoryProfile_GrowthRate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory growth rate test in short mode")
	}

	// 启动本地 benchmark 服务器
	srv := server.New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start benchmark server: %v", err)
	}
	defer srv.Close()

	benchURL := "http://" + addr + "/"
	concurrency := 16
	requestsPerPhase := 10000
	phases := 5

	t.Logf("Memory Growth Rate Test (phases=%d, requests/phase=%d)", phases, requestsPerPhase)
	t.Logf("%-8s %-15s %-15s %-15s %-12s",
		"Phase", "HeapInUse(MB)", "HeapObjects", "TotalAlloc(MB)", "GC Cycles")
	t.Logf("%-8s %-15s %-15s %-15s %-12s",
		"---", "---", "---", "---", "---")

	var heapSizes []float64

	for phase := 1; phase <= phases; phase++ {
		srv.ResetStats()

		s := settings.New()
		s.Set("CONCURRENT_REQUESTS", concurrency, settings.PriorityProject)
		s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", concurrency, settings.PriorityProject)
		s.Set("DOWNLOAD_DELAY", 0, settings.PriorityProject)
		s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityProject)
		s.Set("LOG_LEVEL", "ERROR", settings.PriorityProject)
		s.Set("RETRY_ENABLED", false, settings.PriorityProject)
		s.Set("COOKIES_ENABLED", false, settings.PriorityProject)
		s.Set("ROBOTSTXT_OBEY", false, settings.PriorityProject)
		s.Set("COMPRESSION_ENABLED", false, settings.PriorityProject)
		s.Set("HTTPPROXY_ENABLED", false, settings.PriorityProject)
		s.Set("DOWNLOADER_STATS", false, settings.PriorityProject)

		c := crawler.New(crawler.WithSettings(s))
		sp := newMemoryBenchSpider(benchURL, requestsPerPhase)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := c.Crawl(ctx, sp)
		cancel()

		if err != nil {
			t.Logf("  phase %d crawl error: %v", phase, err)
		}

		// 强制 GC 后测量
		runtime.GC()
		runtime.GC() // 双重 GC 确保 finalizer 执行
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		heapMB := float64(mem.HeapInuse) / (1024 * 1024)
		totalAllocMB := float64(mem.TotalAlloc) / (1024 * 1024)
		heapSizes = append(heapSizes, heapMB)

		t.Logf("%-8d %-15.2f %-15d %-15.2f %-12d",
			phase, heapMB, mem.HeapObjects, totalAllocMB, mem.NumGC)
	}

	// 验证：后期阶段的堆内存不应显著大于前期
	// 允许 50% 的波动（GC 时机不确定）
	if len(heapSizes) >= 3 {
		firstPhaseHeap := heapSizes[0]
		lastPhaseHeap := heapSizes[len(heapSizes)-1]

		// 如果最后阶段的堆内存是第一阶段的 3 倍以上，可能存在泄漏
		if firstPhaseHeap > 0 && lastPhaseHeap > firstPhaseHeap*3 {
			t.Errorf("potential memory leak detected: phase 1 heap=%.2f MB, phase %d heap=%.2f MB (%.1fx growth)",
				firstPhaseHeap, len(heapSizes), lastPhaseHeap, lastPhaseHeap/firstPhaseHeap)
		}
	}
}

// ============================================================================
// 内存分配细粒度测试
// ============================================================================

// BenchmarkMemory_RequestAllocation 测试单个 Request 对象的内存分配。
func BenchmarkMemory_RequestAllocation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := shttp.NewRequest(fmt.Sprintf("http://localhost:8080/page/%d", i),
			shttp.WithDontFilter(true),
		)
		_ = req
	}
}

// BenchmarkMemory_RequestCopy 测试 Request.Copy() 的内存分配。
func BenchmarkMemory_RequestCopy(b *testing.B) {
	b.ReportAllocs()
	req, _ := shttp.NewRequest("http://localhost:8080/page/1",
		shttp.WithDontFilter(true),
		shttp.WithMeta(map[string]any{"key": "value"}),
		shttp.WithHeader("X-Custom", "test"),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copied := req.Copy()
		_ = copied
	}
}

// BenchmarkMemory_CrawlerCreation 测试 Crawler 创建的内存分配。
func BenchmarkMemory_CrawlerCreation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := settings.New()
		s.Set("LOG_LEVEL", "ERROR", settings.PriorityProject)
		c := crawler.New(crawler.WithSettings(s))
		_ = c
	}
}
