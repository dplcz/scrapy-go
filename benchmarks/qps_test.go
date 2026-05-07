// Package benchmarks 提供 scrapy-go 框架的性能基准测试套件。
//
// 本包包含 QPS 吞吐量测试和内存占用测试，用于验证框架在不同并发级别下的性能表现。
// 测试使用本地 benchmark 服务器，排除网络因素干扰。
//
// 运行方式：
//
//	go test -bench=. -benchmem -timeout=300s ./benchmarks/
//	go test -bench=BenchmarkQPS -benchmem -timeout=300s ./benchmarks/
//	go test -bench=BenchmarkMemory -benchmem -timeout=300s ./benchmarks/
//
// 验收标准（Phase 4）：
//   - 单机 >= 5000 QPS（本地服务器，16 并发）
//   - 10 万请求内存增长 < 500MB
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
// QPS 基准测试 Spider
// ============================================================================

// qpsBenchSpider 是用于 QPS 基准测试的 Spider。
// 生成大量轻量级请求，测量框架的吞吐量。
type qpsBenchSpider struct {
	spider.Base
	benchURL     string
	totalReqs    int
	requestsSent atomic.Int64
}

// newQPSBenchSpider 创建 QPS 基准测试 Spider。
func newQPSBenchSpider(benchURL string, totalReqs int) *qpsBenchSpider {
	return &qpsBenchSpider{
		Base: spider.Base{
			SpiderName: "qps_bench",
		},
		benchURL:  benchURL,
		totalReqs: totalReqs,
	}
}

// Start 生成指定数量的请求。
func (s *qpsBenchSpider) Start(ctx context.Context) <-chan spider.Output {
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

// Parse 空回调，不产出任何 Item 或新请求。
func (s *qpsBenchSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

// RequestsSent 返回已发送的请求数。
func (s *qpsBenchSpider) RequestsSent() int64 {
	return s.requestsSent.Load()
}

// ============================================================================
// QPS 基准测试
// ============================================================================

// BenchmarkQPS_Concurrent8 测试 8 并发下的 QPS。
func BenchmarkQPS_Concurrent8(b *testing.B) {
	benchmarkQPS(b, 8, 5000)
}

// BenchmarkQPS_Concurrent16 测试 16 并发下的 QPS（验收标准：>= 5000 QPS）。
func BenchmarkQPS_Concurrent16(b *testing.B) {
	benchmarkQPS(b, 16, 5000)
}

// BenchmarkQPS_Concurrent32 测试 32 并发下的 QPS。
func BenchmarkQPS_Concurrent32(b *testing.B) {
	benchmarkQPS(b, 32, 10000)
}

// BenchmarkQPS_Concurrent64 测试 64 并发下的 QPS。
func BenchmarkQPS_Concurrent64(b *testing.B) {
	benchmarkQPS(b, 64, 10000)
}

// BenchmarkQPS_Concurrent128 测试 128 并发下的 QPS。
func BenchmarkQPS_Concurrent128(b *testing.B) {
	benchmarkQPS(b, 128, 20000)
}

// benchmarkQPS 执行 QPS 基准测试。
// concurrency: 并发请求数
// totalRequests: 总请求数
func benchmarkQPS(b *testing.B, concurrency int, totalRequests int) {
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
		srv.ResetStats()

		// 创建 Crawler 并配置并发
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
		sp := newQPSBenchSpider(benchURL, totalRequests)

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		err := c.Crawl(ctx, sp)
		cancel()

		if err != nil {
			b.Logf("crawl finished with error: %v", err)
		}

		// 计算 QPS
		stats := srv.GetStats()
		b.ReportMetric(float64(stats.TotalRequests), "requests")
		b.ReportMetric(float64(stats.MaxConcurrent), "max_concurrent")
	}

	b.StopTimer()
}

// ============================================================================
// QPS 验收测试（非 benchmark，用于 CI 验证）
// ============================================================================

// TestQPSAcceptance_Concurrent16 验证 16 并发下 QPS >= 5000 的验收标准。
// 此测试使用固定请求数和时间测量，而非 Go benchmark 框架。
func TestQPSAcceptance_Concurrent16(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping QPS acceptance test in short mode")
	}

	// 启动本地 benchmark 服务器
	srv := server.New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start benchmark server: %v", err)
	}
	defer srv.Close()

	benchURL := "http://" + addr + "/"
	totalRequests := 10000
	concurrency := 16

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
	sp := newQPSBenchSpider(benchURL, totalRequests)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	err = c.Crawl(ctx, sp)
	cancel()
	elapsed := time.Since(start)

	if err != nil {
		t.Logf("crawl finished with error: %v", err)
	}

	stats := srv.GetStats()
	qps := float64(stats.TotalRequests) / elapsed.Seconds()

	t.Logf("QPS Acceptance Test Results:")
	t.Logf("  Concurrency:     %d", concurrency)
	t.Logf("  Total Requests:  %d", stats.TotalRequests)
	t.Logf("  Elapsed:         %v", elapsed)
	t.Logf("  QPS:             %.2f", qps)
	t.Logf("  Max Concurrent:  %d", stats.MaxConcurrent)

	// 验收标准：QPS >= 5000
	if qps < 5000 {
		t.Errorf("QPS acceptance failed: got %.2f, want >= 5000", qps)
	}
}

// TestQPSScaling 测试不同并发级别下的 QPS 扩展性。
func TestQPSScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping QPS scaling test in short mode")
	}

	// 启动本地 benchmark 服务器
	srv := server.New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start benchmark server: %v", err)
	}
	defer srv.Close()

	benchURL := "http://" + addr + "/"

	concurrencyLevels := []int{1, 4, 8, 16, 32, 64}
	requestsPerLevel := 3000

	t.Logf("QPS Scaling Test (GOMAXPROCS=%d)", runtime.GOMAXPROCS(0))
	t.Logf("%-12s %-12s %-12s %-15s", "Concurrency", "Requests", "Elapsed", "QPS")
	t.Logf("%-12s %-12s %-12s %-15s", "---", "---", "---", "---")

	var prevQPS float64
	for _, concurrency := range concurrencyLevels {
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
		sp := newQPSBenchSpider(benchURL, requestsPerLevel)

		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := c.Crawl(ctx, sp)
		cancel()
		elapsed := time.Since(start)

		if err != nil {
			t.Logf("  concurrency=%d crawl error: %v", concurrency, err)
		}

		stats := srv.GetStats()
		qps := float64(stats.TotalRequests) / elapsed.Seconds()

		scalingNote := ""
		if prevQPS > 0 && concurrency > 1 {
			ratio := qps / prevQPS
			scalingNote = fmt.Sprintf(" (%.1fx vs prev)", ratio)
		}
		prevQPS = qps

		t.Logf("%-12d %-12d %-12v %-15.2f%s",
			concurrency, stats.TotalRequests, elapsed.Round(time.Millisecond), qps, scalingNote)
	}
}
