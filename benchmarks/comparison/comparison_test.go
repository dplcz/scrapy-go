// Package comparison 提供 scrapy-go 与真实第三方爬虫框架的性能对比测试。
//
// 本模块作为独立子模块存在，避免将第三方爬虫框架的依赖引入主模块 go.mod。
//
// 对比框架：
//   - raw net/http：绝对基线（无框架开销）
//   - Colly (gocolly/colly)：Go 生态最流行的爬虫框架
//   - Geziyor：高性能 Go 爬虫框架
//   - scrapy-go：本项目完整框架
//
// 对比维度：
//   - QPS 吞吐量（不同并发级别）
//   - 内存分配（总分配量 + 每请求分配）
//   - 框架开销比（相对于 raw net/http 基线）
//
// 运行方式：
//
//	cd benchmarks/comparison
//	go test -run "TestComparison" -timeout=300s -v ./...
//	go test -bench "BenchmarkComparison" -benchmem -timeout=300s ./...
package comparison

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/benchmarks/server"
	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/geziyor/geziyor"
	"github.com/geziyor/geziyor/client"
	"github.com/gocolly/colly/v2"
)

// ============================================================================
// 对比测试结果结构
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

// rawHTTPCrawl 使用原始 net/http 进行并发爬取（绝对基线）。
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
// Colly 真实框架实现
// ============================================================================

// collyCrawl 使用真实的 Colly 框架进行爬取。
//
// 配置说明：
//   - Async + Parallelism: 设置并发数以对齐测试条件
//   - AllowURLRevisit: 必须开启，因为测试重复访问同一 URL
//   - Colly 默认不含：重试、Cookie 管理、压缩处理、代理、robots.txt
//   - 因此 Colly 的「默认配置」天然比 scrapy-go 轻量
func collyCrawl(ctx context.Context, baseURL string, totalRequests, concurrency int) (int64, error) {
	var completed atomic.Int64

	c := colly.NewCollector(
		colly.Async(true),
		colly.MaxDepth(1),
	)

	// 设置并发和延迟
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: concurrency,
		Delay:       0,
	})

	// 设置超时
	c.SetRequestTimeout(30 * time.Second)

	// 禁用 URL 重复过滤（我们需要多次访问同一 URL）
	c.AllowURLRevisit = true

	// 注册回调
	c.OnResponse(func(r *colly.Response) {
		completed.Add(1)
	})

	c.OnError(func(r *colly.Response, err error) {
		// 忽略错误，继续计数
	})

	// 发起请求
	for i := 0; i < totalRequests; i++ {
		select {
		case <-ctx.Done():
			break
		default:
		}
		c.Visit(baseURL)
	}

	// 等待所有请求完成
	c.Wait()

	return completed.Load(), nil
}

// ============================================================================
// Geziyor 真实框架实现
// ============================================================================

// geziyorCrawl 使用真实的 Geziyor 框架进行爬取。
//
// 配置说明：
//   - URLRevisitEnabled: 必须开启，因为测试重复访问同一 URL
//   - RobotsTxtDisabled: 关闭 robots.txt（本地测试服务器无 robots.txt，
//     若不关闭会产生额外失败请求，影响公平性）
//   - LogDisabled: 关闭日志（与 scrapy-go 的 LOG_LEVEL=ERROR 对齐，减少 I/O 干扰）
//   - 其他保持默认（包括 Geziyor 内置的 metrics 收集）
func geziyorCrawl(ctx context.Context, baseURL string, totalRequests, concurrency int) (int64, error) {
	var completed atomic.Int64

	// 生成起始 URL 列表
	startURLs := make([]string, totalRequests)
	for i := 0; i < totalRequests; i++ {
		startURLs[i] = baseURL
	}

	geziyor.NewGeziyor(&geziyor.Options{
		StartURLs:          startURLs,
		ConcurrentRequests: concurrency,
		ParseFunc: func(g *geziyor.Geziyor, r *client.Response) {
			completed.Add(1)
		},
		URLRevisitEnabled: true,
		RobotsTxtDisabled: true,
		LogDisabled:       true,
	}).Start()

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
// 这是最公平的对比方式——展示框架在「开箱即用」状态下的真实性能。
//
// 配置对齐说明：
//   - Colly 默认无重试、无 Cookie 管理、无压缩、无代理、无 robots.txt
//   - Geziyor 默认有 robots.txt（已在测试中关闭）、有 metrics，无重试/Cookie/压缩/代理
//   - scrapy-go 默认全部开启，此处保持默认，体现完整框架的真实开销
//
// 仅关闭以下与测试机制冲突的配置：
//   - DOWNLOAD_DELAY = 0（避免人为限速）
//   - RANDOMIZE_DOWNLOAD_DELAY = false（同上）
//   - LOG_LEVEL = ERROR（减少日志 I/O 干扰）
func scrapyGoCrawl(ctx context.Context, baseURL string, totalRequests, concurrency int) (int64, error) {
	st := settings.New()
	st.Set("CONCURRENT_REQUESTS", concurrency, settings.PriorityProject)
	st.Set("CONCURRENT_REQUESTS_PER_DOMAIN", concurrency, settings.PriorityProject)
	st.Set("DOWNLOAD_DELAY", 0, settings.PriorityProject)
	st.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityProject)
	st.Set("LOG_LEVEL", "ERROR", settings.PriorityProject)
	// 保留所有默认中间件：RETRY、COOKIES、COMPRESSION、HTTPPROXY、ROBOTSTXT、DOWNLOADER_STATS
	// 这些是 scrapy-go 的核心功能，保持开启才能公平展示完整框架开销

	c := crawler.New(crawler.WithSettings(st))
	sp := newComparisonSpider(baseURL, totalRequests)

	err := c.Crawl(ctx, sp)
	return sp.requestsSent.Load(), err
}

// ============================================================================
// 对比测试：QPS 吞吐量
// ============================================================================

// TestComparisonQPS 对比不同框架在相同条件下的 QPS 吞吐量。
//
// 公平性说明：
//   - 所有框架均使用默认配置，仅调整并发数和关闭 URL 去重
//   - scrapy-go 保留所有默认中间件（重试、Cookie、压缩、代理、robots.txt）
//   - Colly/Geziyor 本身不具备这些功能，因此它们的「默认配置」天然更轻量
//   - 这种对比方式展示的是各框架「开箱即用」的真实性能差异
func TestComparisonQPS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping comparison QPS test in short mode")
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
	concurrencyLevels := []int{8, 16, 32, 64}

	t.Logf("=== QPS Comparison Test (Real Frameworks, Default Config) ===")
	t.Logf("GOMAXPROCS=%d, Total Requests=%d", runtime.GOMAXPROCS(0), totalRequests)
	t.Logf("NOTE: scrapy-go runs with ALL default middlewares enabled (retry, cookies, compression, proxy, robots.txt)")
	t.Logf("      Colly/Geziyor do NOT have these features, so their default config is inherently lighter.")
	t.Logf("")

	var allResults []*comparisonResult

	for _, concurrency := range concurrencyLevels {
		t.Logf("--- Concurrency: %d ---", concurrency)

		frameworks := []struct {
			name string
			fn   func(ctx context.Context, url string, total, conc int) (int64, error)
		}{
			{"raw-net/http", rawHTTPCrawl},
			{"colly", collyCrawl},
			{"geziyor", geziyorCrawl},
			{"scrapy-go", scrapyGoCrawl},
		}

		for _, fw := range frameworks {
			srv.ResetStats()
			runtime.GC()

			var memBefore runtime.MemStats
			runtime.ReadMemStats(&memBefore)

			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			start := time.Now()
			completed, err := fw.fn(ctx, benchURL, totalRequests, concurrency)
			elapsed := time.Since(start)
			cancel()

			runtime.GC()
			var memAfter runtime.MemStats
			runtime.ReadMemStats(&memAfter)

			if err != nil && err != context.DeadlineExceeded {
				t.Logf("  %s: error: %v", fw.name, err)
			}

			allocBytes := memAfter.TotalAlloc - memBefore.TotalAlloc
			allocsMade := memAfter.Mallocs - memBefore.Mallocs

			result := &comparisonResult{
				Framework:     fw.name,
				Concurrency:   concurrency,
				TotalRequests: completed,
				Elapsed:       elapsed,
				QPS:           float64(completed) / elapsed.Seconds(),
				TotalAllocMB:  float64(allocBytes) / (1024 * 1024),
				BytesPerReq:   float64(allocBytes) / float64(maxInt64(completed, 1)),
				AllocsPerReq:  float64(allocsMade) / float64(maxInt64(completed, 1)),
			}
			allResults = append(allResults, result)

			t.Logf("  %s", result.String())
		}
		t.Logf("")
	}

	// 生成对比报告
	t.Logf("=== Comparison Summary ===")
	t.Logf("")
	printComparisonReport(t, allResults, concurrencyLevels)
}

// printComparisonReport 输出格式化的对比报告。
func printComparisonReport(t *testing.T, results []*comparisonResult, concurrencyLevels []int) {
	t.Helper()

	frameworks := []string{"raw-net/http", "colly", "geziyor", "scrapy-go"}

	// QPS 对比表
	t.Logf("--- QPS (requests/sec) ---")
	t.Logf("%-16s %12s %12s %12s %12s", "Framework", "conc=8", "conc=16", "conc=32", "conc=64")
	for _, fw := range frameworks {
		line := fmt.Sprintf("%-16s", fw)
		for _, conc := range concurrencyLevels {
			for _, r := range results {
				if r.Framework == fw && r.Concurrency == conc {
					line += fmt.Sprintf(" %12.0f", r.QPS)
					break
				}
			}
		}
		t.Logf("%s", line)
	}
	t.Logf("")

	// 框架开销比（相对于 raw net/http）
	t.Logf("--- Overhead Ratio (vs raw net/http) ---")
	t.Logf("%-16s %12s %12s %12s %12s", "Framework", "conc=8", "conc=16", "conc=32", "conc=64")
	for _, fw := range frameworks {
		line := fmt.Sprintf("%-16s", fw)
		for _, conc := range concurrencyLevels {
			var baseQPS, fwQPS float64
			for _, r := range results {
				if r.Framework == "raw-net/http" && r.Concurrency == conc {
					baseQPS = r.QPS
				}
				if r.Framework == fw && r.Concurrency == conc {
					fwQPS = r.QPS
				}
			}
			if baseQPS > 0 {
				ratio := fwQPS / baseQPS
				line += fmt.Sprintf(" %11.2fx", ratio)
			}
		}
		t.Logf("%s", line)
	}
	t.Logf("")

	// 内存分配对比
	t.Logf("--- Memory (bytes/request) ---")
	t.Logf("%-16s %12s %12s %12s %12s", "Framework", "conc=8", "conc=16", "conc=32", "conc=64")
	for _, fw := range frameworks {
		line := fmt.Sprintf("%-16s", fw)
		for _, conc := range concurrencyLevels {
			for _, r := range results {
				if r.Framework == fw && r.Concurrency == conc {
					line += fmt.Sprintf(" %12.0f", r.BytesPerReq)
					break
				}
			}
		}
		t.Logf("%s", line)
	}
}

// ============================================================================
// 对比测试：内存效率
// ============================================================================

// TestComparisonMemory 对比不同框架在大量请求下的内存效率。
func TestComparisonMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping comparison memory test in short mode")
	}

	srv := server.New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start benchmark server: %v", err)
	}
	defer srv.Close()

	benchURL := "http://" + addr + "/"
	totalRequests := 10000
	concurrency := 16

	t.Logf("=== Memory Comparison Test (Real Frameworks) ===")
	t.Logf("GOMAXPROCS=%d, Total Requests=%d, Concurrency=%d",
		runtime.GOMAXPROCS(0), totalRequests, concurrency)
	t.Logf("")

	frameworks := []struct {
		name string
		fn   func(ctx context.Context, url string, total, conc int) (int64, error)
	}{
		{"raw-net/http", rawHTTPCrawl},
		{"colly", collyCrawl},
		{"geziyor", geziyorCrawl},
		{"scrapy-go", scrapyGoCrawl},
	}

	t.Logf("%-16s %12s %12s %12s %12s %12s",
		"Framework", "Requests", "TotalAlloc", "HeapInUse", "Bytes/Req", "Allocs/Req")
	t.Logf("%-16s %12s %12s %12s %12s %12s",
		"---", "---", "---", "---", "---", "---")

	for _, fw := range frameworks {
		srv.ResetStats()

		runtime.GC()
		runtime.GC()
		var memBefore runtime.MemStats
		runtime.ReadMemStats(&memBefore)

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		completed, err := fw.fn(ctx, benchURL, totalRequests, concurrency)
		cancel()

		if err != nil && err != context.DeadlineExceeded {
			t.Logf("  %s: error: %v", fw.name, err)
		}

		runtime.GC()
		runtime.GC()
		var memAfter runtime.MemStats
		runtime.ReadMemStats(&memAfter)

		allocBytes := memAfter.TotalAlloc - memBefore.TotalAlloc
		allocsMade := memAfter.Mallocs - memBefore.Mallocs
		heapInUse := memAfter.HeapInuse

		bytesPerReq := float64(allocBytes) / float64(maxInt64(completed, 1))
		allocsPerReq := float64(allocsMade) / float64(maxInt64(completed, 1))

		t.Logf("%-16s %12d %10.2fMB %10.2fMB %12.0f %12.1f",
			fw.name, completed,
			float64(allocBytes)/(1024*1024),
			float64(heapInUse)/(1024*1024),
			bytesPerReq, allocsPerReq)
	}
}

// ============================================================================
// 对比测试：框架开销验收
// ============================================================================

// TestComparisonOverheadAcceptance 验证 scrapy-go 的框架开销在可接受范围内。
//
// 公平性说明：scrapy-go 使用默认配置（所有中间件开启），
// Colly/Geziyor 使用默认配置（天然不含重试/Cookie/压缩/代理等功能）。
// 验收标准：即使带着完整中间件栈，scrapy-go 的 QPS 也不应低于 Colly 的 60%。
func TestComparisonOverheadAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping comparison overhead acceptance test in short mode")
	}

	srv := server.New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start benchmark server: %v", err)
	}
	defer srv.Close()

	benchURL := "http://" + addr + "/"
	totalRequests := 10000
	concurrency := 16

	t.Logf("=== Overhead Acceptance Test (concurrency=%d, requests=%d) ===",
		concurrency, totalRequests)
	t.Logf("NOTE: scrapy-go uses default config with ALL middlewares enabled")

	// 1. 运行 raw net/http 基线
	srv.ResetStats()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	start := time.Now()
	rawCompleted, _ := rawHTTPCrawl(ctx, benchURL, totalRequests, concurrency)
	rawElapsed := time.Since(start)
	cancel()
	rawQPS := float64(rawCompleted) / rawElapsed.Seconds()

	// 2. 运行 Colly
	srv.ResetStats()
	ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
	start = time.Now()
	collyCompleted, _ := collyCrawl(ctx, benchURL, totalRequests, concurrency)
	collyElapsed := time.Since(start)
	cancel()
	collyQPS := float64(collyCompleted) / collyElapsed.Seconds()

	// 3. 运行 Geziyor
	srv.ResetStats()
	ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
	start = time.Now()
	geziyorCompleted, _ := geziyorCrawl(ctx, benchURL, totalRequests, concurrency)
	geziyorElapsed := time.Since(start)
	cancel()
	geziyorQPS := float64(geziyorCompleted) / geziyorElapsed.Seconds()

	// 4. 运行 scrapy-go
	srv.ResetStats()
	ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
	start = time.Now()
	sgCompleted, _ := scrapyGoCrawl(ctx, benchURL, totalRequests, concurrency)
	sgElapsed := time.Since(start)
	cancel()
	sgQPS := float64(sgCompleted) / sgElapsed.Seconds()

	// 5. 输出对比结果
	t.Logf("  raw net/http:  %d reqs in %v (QPS: %.2f)", rawCompleted, rawElapsed.Round(time.Millisecond), rawQPS)
	t.Logf("  colly:         %d reqs in %v (QPS: %.2f)", collyCompleted, collyElapsed.Round(time.Millisecond), collyQPS)
	t.Logf("  geziyor:       %d reqs in %v (QPS: %.2f)", geziyorCompleted, geziyorElapsed.Round(time.Millisecond), geziyorQPS)
	t.Logf("  scrapy-go:     %d reqs in %v (QPS: %.2f)", sgCompleted, sgElapsed.Round(time.Millisecond), sgQPS)
	t.Logf("")

	// 对比 scrapy-go vs raw
	rawOverhead := rawQPS / sgQPS
	t.Logf("  scrapy-go vs raw:    %.2fx overhead (scrapy-go is %.1f%% of raw speed)", rawOverhead, (sgQPS/rawQPS)*100)

	// 对比 scrapy-go vs colly
	if collyQPS > 0 {
		collyRatio := sgQPS / collyQPS
		t.Logf("  scrapy-go vs colly:  %.2fx (scrapy-go is %.1f%% of colly speed)", collyRatio, collyRatio*100)
	}

	// 对比 scrapy-go vs geziyor
	if geziyorQPS > 0 {
		geziyorRatio := sgQPS / geziyorQPS
		t.Logf("  scrapy-go vs geziyor: %.2fx (scrapy-go is %.1f%% of geziyor speed)", geziyorRatio, geziyorRatio*100)
	}

	// 验收标准：即使带着完整中间件栈，scrapy-go 的框架开销不超过 raw net/http 的 5x
	// （因为保留了重试、Cookie、压缩、代理、robots.txt 等全部中间件）
	const maxOverhead = 5.0
	if rawOverhead > maxOverhead {
		t.Errorf("framework overhead too high vs raw: %.2fx > %.1fx limit (scrapy-go QPS=%.0f, raw QPS=%.0f)",
			rawOverhead, maxOverhead, sgQPS, rawQPS)
	}

	// 额外验收：scrapy-go QPS 不低于 Colly 的 60%（即使 scrapy-go 多了完整中间件栈）
	if collyQPS > 0 && sgQPS < collyQPS*0.6 {
		t.Errorf("scrapy-go too slow vs colly: scrapy-go QPS=%.0f < colly QPS=%.0f * 60%%",
			sgQPS, collyQPS)
	}
}

// ============================================================================
// Go Benchmark 框架集成
// ============================================================================

// BenchmarkComparison_RawHTTP_16 基准测试 raw net/http（16 并发）。
func BenchmarkComparison_RawHTTP_16(b *testing.B) {
	benchmarkFramework(b, "raw-net/http", rawHTTPCrawl, 16, 3000)
}

// BenchmarkComparison_Colly_16 基准测试 Colly（16 并发）。
func BenchmarkComparison_Colly_16(b *testing.B) {
	benchmarkFramework(b, "colly", collyCrawl, 16, 3000)
}

// BenchmarkComparison_Geziyor_16 基准测试 Geziyor（16 并发）。
func BenchmarkComparison_Geziyor_16(b *testing.B) {
	benchmarkFramework(b, "geziyor", geziyorCrawl, 16, 3000)
}

// BenchmarkComparison_ScrapyGo_16 基准测试 scrapy-go（16 并发）。
func BenchmarkComparison_ScrapyGo_16(b *testing.B) {
	benchmarkFramework(b, "scrapy-go", scrapyGoCrawl, 16, 3000)
}

// BenchmarkComparison_RawHTTP_64 基准测试 raw net/http（64 并发）。
func BenchmarkComparison_RawHTTP_64(b *testing.B) {
	benchmarkFramework(b, "raw-net/http", rawHTTPCrawl, 64, 5000)
}

// BenchmarkComparison_Colly_64 基准测试 Colly（64 并发）。
func BenchmarkComparison_Colly_64(b *testing.B) {
	benchmarkFramework(b, "colly", collyCrawl, 64, 5000)
}

// BenchmarkComparison_Geziyor_64 基准测试 Geziyor（64 并发）。
func BenchmarkComparison_Geziyor_64(b *testing.B) {
	benchmarkFramework(b, "geziyor", geziyorCrawl, 64, 5000)
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
// 对比测试：延迟场景
// ============================================================================

// TestComparisonWithLatency 对比不同框架在有网络延迟时的表现。
func TestComparisonWithLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping comparison latency test in short mode")
	}

	srv := server.New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start benchmark server: %v", err)
	}
	defer srv.Close()

	// 使用 10ms 延迟端点
	benchURL := "http://" + addr + "/latency?ms=10"
	totalRequests := 5000
	concurrency := 32

	t.Logf("=== Latency Comparison Test (latency=10ms, concurrency=%d, requests=%d) ===",
		concurrency, totalRequests)
	t.Logf("")

	frameworks := []struct {
		name string
		fn   func(ctx context.Context, url string, total, conc int) (int64, error)
	}{
		{"raw-net/http", rawHTTPCrawl},
		{"colly", collyCrawl},
		{"geziyor", geziyorCrawl},
		{"scrapy-go", scrapyGoCrawl},
	}

	// 理论最优时间：totalRequests * latency / concurrency = 500 * 10ms / 32 ≈ 156ms
	theoreticalMin := time.Duration(totalRequests) * 10 * time.Millisecond / time.Duration(concurrency)
	t.Logf("Theoretical minimum: %v (perfect parallelism)", theoreticalMin)
	t.Logf("")

	t.Logf("%-16s %12s %12s %12s %12s",
		"Framework", "Requests", "Elapsed", "QPS", "Efficiency")
	t.Logf("%-16s %12s %12s %12s %12s",
		"---", "---", "---", "---", "---")

	for _, fw := range frameworks {
		srv.ResetStats()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		start := time.Now()
		completed, err := fw.fn(ctx, benchURL, totalRequests, concurrency)
		elapsed := time.Since(start)
		cancel()

		if err != nil && err != context.DeadlineExceeded {
			t.Logf("  %s: error: %v", fw.name, err)
		}

		qps := float64(completed) / elapsed.Seconds()
		efficiency := float64(theoreticalMin) / float64(elapsed) * 100

		t.Logf("%-16s %12d %12v %12.2f %10.1f%%",
			fw.name, completed, elapsed.Round(time.Millisecond), qps, efficiency)
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

// maxInt64 返回两个 int64 中的较大值。
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
