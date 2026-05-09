// Package benchmarks 提供 scrapy-go 框架的性能对比测试。
//
// 真实框架对比测试（使用 Colly、Geziyor 等真实第三方库）位于独立子模块：
//
//	cd benchmarks/comparison
//	go test -run "TestComparison" -timeout=300s -v ./...
//	go test -bench "BenchmarkComparison" -benchmem -timeout=300s ./...
//
// 本文件保留 raw net/http 基线与 scrapy-go 的快速对比测试，
// 用于 CI 中快速验证框架开销是否在可接受范围内。
package benchmarks

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
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
// 对比测试框架
// ============================================================================

// comparisonResult 存储单次对比测试的结果。
type comparisonResult struct {
	Framework     string
	Concurrency   int
	TotalRequests int64
	Elapsed       time.Duration
	QPS           float64
	TotalAllocMB  float64
	BytesPerReq   float64
	AllocsPerReq  float64
}

// String 返回格式化的结果字符串。
func (r *comparisonResult) String() string {
	return fmt.Sprintf("%-16s conc=%-4d reqs=%-8d elapsed=%-12v qps=%-12.2f alloc=%-8.2fMB bytes/req=%-10.0f allocs/req=%.1f",
		r.Framework, r.Concurrency, r.TotalRequests, r.Elapsed.Round(time.Millisecond),
		r.QPS, r.TotalAllocMB, r.BytesPerReq, r.AllocsPerReq)
}

// ============================================================================
// Raw net/http 基线实现
// ============================================================================

// rawHTTPCrawl 使用原始 net/http 进行并发爬取。
// 这是绝对基线：无框架开销，仅有 HTTP 客户端 + goroutine 池。
func rawHTTPCrawl(ctx context.Context, baseURL string, totalRequests, concurrency int) (int64, error) {
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        concurrency * 2,
			MaxIdleConnsPerHost: concurrency * 2,
			MaxConnsPerHost:     concurrency * 2,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  true,
		},
		Timeout: 30 * time.Second,
	}
	defer client.CloseIdleConnections()

	var completed atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i := 0; i < totalRequests; i++ {
		select {
		case <-ctx.Done():
			break
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
			if err != nil {
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			completed.Add(1)
		}()
	}

	wg.Wait()
	return completed.Load(), nil
}

// ============================================================================
// scrapy-go 框架实现
// ============================================================================

// comparisonSpider 是用于对比测试的 scrapy-go Spider。
type comparisonSpider struct {
	spider.Base
	benchURL     string
	totalReqs    int
	requestsSent atomic.Int64
}

// newComparisonSpider 创建对比测试 Spider。
func newComparisonSpider(benchURL string, totalReqs int) *comparisonSpider {
	return &comparisonSpider{
		Base: spider.Base{
			SpiderName: "comparison_bench",
		},
		benchURL:  benchURL,
		totalReqs: totalReqs,
	}
}

// Start 生成指定数量的请求。
func (s *comparisonSpider) Start(ctx context.Context) <-chan spider.Output {
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
func (s *comparisonSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

// scrapyGoCrawl 使用 scrapy-go 框架的默认配置进行爬取。
// 保留所有默认中间件（重试、Cookie、压缩、代理、robots.txt 等），
// 仅关闭与测试机制冲突的配置（延迟、日志）。
func scrapyGoCrawl(ctx context.Context, baseURL string, totalRequests, concurrency int) (int64, error) {
	st := settings.New()
	st.Set("CONCURRENT_REQUESTS", concurrency, settings.PriorityProject)
	st.Set("CONCURRENT_REQUESTS_PER_DOMAIN", concurrency, settings.PriorityProject)
	st.Set("DOWNLOAD_DELAY", 0, settings.PriorityProject)
	st.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityProject)
	st.Set("LOG_LEVEL", "ERROR", settings.PriorityProject)
	// 保留所有默认中间件：RETRY、COOKIES、COMPRESSION、HTTPPROXY、ROBOTSTXT、DOWNLOADER_STATS

	c := crawler.New(crawler.WithSettings(st))
	sp := newComparisonSpider(baseURL, totalRequests)

	err := c.Crawl(ctx, sp)
	return sp.requestsSent.Load(), err
}

// ============================================================================
// 快速对比测试：scrapy-go vs raw net/http
// ============================================================================

// TestComparisonOverheadAcceptance 验证 scrapy-go 的框架开销在可接受范围内。
// 验收标准：scrapy-go QPS 不低于 raw net/http 的 30%（即框架开销不超过 3.3x）。
//
// 注意：完整的多框架对比测试（包含真实 Colly、Geziyor）位于 benchmarks/comparison/ 子模块。
func TestComparisonOverheadAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping comparison overhead acceptance test in short mode")
	}

	// 启动本地 benchmark 服务器
	srv := server.New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start benchmark server: %v", err)
	}
	defer srv.Close()

	benchURL := "http://" + addr + "/"
	totalRequests := 5000
	concurrency := 16

	t.Logf("=== Overhead Acceptance Test (concurrency=%d, requests=%d) ===",
		concurrency, totalRequests)

	// 1. 运行 raw net/http 基线
	srv.ResetStats()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	start := time.Now()
	rawCompleted, _ := rawHTTPCrawl(ctx, benchURL, totalRequests, concurrency)
	rawElapsed := time.Since(start)
	cancel()
	rawQPS := float64(rawCompleted) / rawElapsed.Seconds()

	// 2. 运行 scrapy-go
	srv.ResetStats()
	ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
	start = time.Now()
	sgCompleted, _ := scrapyGoCrawl(ctx, benchURL, totalRequests, concurrency)
	sgElapsed := time.Since(start)
	cancel()
	sgQPS := float64(sgCompleted) / sgElapsed.Seconds()

	// 3. 计算开销比
	overheadRatio := rawQPS / sgQPS

	t.Logf("  raw net/http:  %d reqs in %v (QPS: %.2f)", rawCompleted, rawElapsed.Round(time.Millisecond), rawQPS)
	t.Logf("  scrapy-go:     %d reqs in %v (QPS: %.2f)", sgCompleted, sgElapsed.Round(time.Millisecond), sgQPS)
	t.Logf("  Overhead:      %.2fx (scrapy-go is %.1f%% of raw speed)", overheadRatio, (sgQPS/rawQPS)*100)
	t.Logf("")
	t.Logf("  提示：完整多框架对比（Colly、Geziyor）请运行：")
	t.Logf("    cd benchmarks/comparison && go test -run TestComparison -v -timeout=300s ./...")

	// 验收标准：scrapy-go QPS >= raw net/http 的 30%
	// 框架提供了调度、中间件、信号等完整功能，3.3x 以内的开销是合理的
	const maxOverhead = 3.3
	if overheadRatio > maxOverhead {
		t.Errorf("framework overhead too high: %.2fx > %.1fx limit (scrapy-go QPS=%.0f, raw QPS=%.0f)",
			overheadRatio, maxOverhead, sgQPS, rawQPS)
	}

	// 额外验证：scrapy-go 绝对 QPS >= 5000（与 P4-001b 验收标准一致）
	if sgQPS < 5000 {
		t.Errorf("scrapy-go absolute QPS too low: %.0f < 5000", sgQPS)
	}
}

// ============================================================================
// Go Benchmark 框架集成
// ============================================================================

// BenchmarkComparison_RawHTTP_16 基准测试 raw net/http（16 并发）。
func BenchmarkComparison_RawHTTP_16(b *testing.B) {
	benchmarkFramework(b, "raw-net/http", rawHTTPCrawl, 16, 3000)
}

// BenchmarkComparison_ScrapyGo_16 基准测试 scrapy-go（16 并发）。
func BenchmarkComparison_ScrapyGo_16(b *testing.B) {
	benchmarkFramework(b, "scrapy-go", scrapyGoCrawl, 16, 3000)
}

// BenchmarkComparison_RawHTTP_64 基准测试 raw net/http（64 并发）。
func BenchmarkComparison_RawHTTP_64(b *testing.B) {
	benchmarkFramework(b, "raw-net/http", rawHTTPCrawl, 64, 5000)
}

// BenchmarkComparison_ScrapyGo_64 基准测试 scrapy-go（64 并发）。
func BenchmarkComparison_ScrapyGo_64(b *testing.B) {
	benchmarkFramework(b, "scrapy-go", scrapyGoCrawl, 64, 5000)
}

// benchmarkFramework 执行框架对比基准测试。
func benchmarkFramework(b *testing.B, name string, fn func(ctx context.Context, url string, total, conc int) (int64, error), concurrency, totalRequests int) {
	b.Helper()

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

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		completed, err := fn(ctx, benchURL, totalRequests, concurrency)
		cancel()

		if err != nil && err != context.DeadlineExceeded {
			b.Logf("%s: error: %v", name, err)
		}

		b.ReportMetric(float64(completed), "requests")
	}

	b.StopTimer()
}

// ============================================================================
// 辅助函数
// ============================================================================

// max 返回两个 int64 中的较大值。
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
