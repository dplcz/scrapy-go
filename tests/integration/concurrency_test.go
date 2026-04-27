// Package integration 提供并发控制的端到端集成测试。
//
// 这些测试验证 CONCURRENT_REQUESTS 和 CONCURRENT_REQUESTS_PER_DOMAIN 参数
// 是否正确限制了并发请求数量。
package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 测试辅助：并发追踪服务器
// ============================================================================

// concurrencyTracker 追踪服务器端的并发请求数。
type concurrencyTracker struct {
	mu             sync.Mutex
	current        int64         // 当前并发数
	peak           int64         // 峰值并发数
	totalRequests  int64         // 总请求数
	requestDelay   time.Duration // 每个请求的处理延迟（模拟慢速服务器）
	concurrencyLog []int64       // 每次请求到达时记录的并发数
}

func newConcurrencyTracker(requestDelay time.Duration) *concurrencyTracker {
	return &concurrencyTracker{
		requestDelay: requestDelay,
	}
}

func (ct *concurrencyTracker) handler(w http.ResponseWriter, r *http.Request) {
	// 递增当前并发数
	current := atomic.AddInt64(&ct.current, 1)
	atomic.AddInt64(&ct.totalRequests, 1)

	// 记录并发数
	ct.mu.Lock()
	ct.concurrencyLog = append(ct.concurrencyLog, current)
	// 更新峰值
	if current > ct.peak {
		ct.peak = current
	}
	ct.mu.Unlock()

	// 模拟处理延迟
	if ct.requestDelay > 0 {
		time.Sleep(ct.requestDelay)
	}

	// 递减当前并发数
	atomic.AddInt64(&ct.current, -1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><body>
<h1>Page %s</h1>
<p>OK</p>
</body></html>`, r.URL.Path)
}

func (ct *concurrencyTracker) getPeak() int64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.peak
}

func (ct *concurrencyTracker) getTotalRequests() int64 {
	return atomic.LoadInt64(&ct.totalRequests)
}

func (ct *concurrencyTracker) getConcurrencyLog() []int64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	result := make([]int64, len(ct.concurrencyLog))
	copy(result, ct.concurrencyLog)
	return result
}

// newTrackedServer 创建一个带并发追踪的测试服务器。
func newTrackedServer(tracker *concurrencyTracker) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", tracker.handler)
	return httptest.NewServer(mux)
}

// ============================================================================
// 测试辅助：批量 URL Spider
// ============================================================================

// batchSpider 生成大量请求以测试并发控制。
type batchSpider struct {
	spider.Base
	baseURL            string
	numRequests        int
	concurrentRequests int
	perDomain          int
	downloadDelay      time.Duration
}

func (s *batchSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		for i := 0; i < s.numRequests; i++ {
			url := fmt.Sprintf("%s/page/%d", s.baseURL, i)
			req, err := shttp.NewRequest(url, shttp.WithDontFilter(true))
			if err != nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- spider.Output{Request: req}:
			}
		}
	}()
	return ch
}

func (s *batchSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

func (s *batchSpider) CustomSettings() *spider.Settings {
	ss := &spider.Settings{
		ConcurrentRequests: spider.IntPtr(s.concurrentRequests),
		DownloadDelay:      spider.DurationPtr(s.downloadDelay),
		LogLevel:           spider.StringPtr("WARN"),
	}
	if s.perDomain > 0 {
		ss.ConcurrentRequestsPerDomain = spider.IntPtr(s.perDomain)
	}
	return ss
}

// ============================================================================
// 测试辅助：多域名 Spider
// ============================================================================

// multiDomainSpider 向多个不同域名发送请求。
type multiDomainSpider struct {
	spider.Base
	servers            []*httptest.Server
	requestsPerServer  int
	concurrentRequests int
	perDomain          int
	downloadDelay      time.Duration
}

func (s *multiDomainSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		for _, srv := range s.servers {
			for i := 0; i < s.requestsPerServer; i++ {
				url := fmt.Sprintf("%s/page/%d", srv.URL, i)
				req, err := shttp.NewRequest(url, shttp.WithDontFilter(true))
				if err != nil {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case ch <- spider.Output{Request: req}:
				}
			}
		}
	}()
	return ch
}

func (s *multiDomainSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

func (s *multiDomainSpider) CustomSettings() *spider.Settings {
	ss := &spider.Settings{
		ConcurrentRequests: spider.IntPtr(s.concurrentRequests),
		DownloadDelay:      spider.DurationPtr(s.downloadDelay),
		LogLevel:           spider.StringPtr("WARN"),
	}
	if s.perDomain > 0 {
		ss.ConcurrentRequestsPerDomain = spider.IntPtr(s.perDomain)
	}
	return ss
}

// ============================================================================
// 测试 1：单域名 - CONCURRENT_REQUESTS=1 应严格串行
// ============================================================================

func TestConcurrentRequests_SingleDomain_Limit1(t *testing.T) {
	tracker := newConcurrencyTracker(100 * time.Millisecond)
	server := newTrackedServer(tracker)
	defer server.Close()

	sp := &batchSpider{
		Base: spider.Base{
			SpiderName: "concurrency-1",
		},
		baseURL:            server.URL,
		numRequests:        10,
		concurrentRequests: 1,
		perDomain:          1,
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()

	t.Logf("CONCURRENT_REQUESTS=1: peak=%d, total=%d", peak, total)
	t.Logf("并发日志: %v", tracker.getConcurrencyLog())

	if total < 10 {
		t.Errorf("期望至少 10 个请求完成, 实际 %d", total)
	}

	// 严格串行时，峰值并发应该为 1
	if peak > 1 {
		t.Errorf("CONCURRENT_REQUESTS=1 时峰值并发应为 1, 实际峰值为 %d（全局并发限制未生效）", peak)
	}
}

// ============================================================================
// 测试 2：单域名 - CONCURRENT_REQUESTS=2 应限制并发为 2
// ============================================================================

func TestConcurrentRequests_SingleDomain_Limit2(t *testing.T) {
	tracker := newConcurrencyTracker(200 * time.Millisecond)
	server := newTrackedServer(tracker)
	defer server.Close()

	sp := &batchSpider{
		Base: spider.Base{
			SpiderName: "concurrency-2",
		},
		baseURL:            server.URL,
		numRequests:        20,
		concurrentRequests: 2,
		perDomain:          2,
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()

	t.Logf("CONCURRENT_REQUESTS=2: peak=%d, total=%d", peak, total)
	t.Logf("并发日志: %v", tracker.getConcurrencyLog())

	if total < 20 {
		t.Errorf("期望至少 20 个请求完成, 实际 %d", total)
	}

	if peak > 2 {
		t.Errorf("CONCURRENT_REQUESTS=2 时峰值并发应 ≤ 2, 实际峰值为 %d（全局并发限制未生效）", peak)
	}
}

// ============================================================================
// 测试 3：单域名 - CONCURRENT_REQUESTS=4 应限制并发为 4
// ============================================================================

func TestConcurrentRequests_SingleDomain_Limit4(t *testing.T) {
	tracker := newConcurrencyTracker(200 * time.Millisecond)
	server := newTrackedServer(tracker)
	defer server.Close()

	sp := &batchSpider{
		Base: spider.Base{
			SpiderName: "concurrency-4",
		},
		baseURL:            server.URL,
		numRequests:        30,
		concurrentRequests: 4,
		perDomain:          4,
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()

	t.Logf("CONCURRENT_REQUESTS=4: peak=%d, total=%d", peak, total)
	t.Logf("并发日志: %v", tracker.getConcurrencyLog())

	if total < 30 {
		t.Errorf("期望至少 30 个请求完成, 实际 %d", total)
	}

	if peak > 4 {
		t.Errorf("CONCURRENT_REQUESTS=4 时峰值并发应 ≤ 4, 实际峰值为 %d（全局并发限制未生效）", peak)
	}
}

// ============================================================================
// 测试 4：多域名 - 全局 CONCURRENT_REQUESTS 应跨域名生效
// ============================================================================

func TestConcurrentRequests_MultiDomain_GlobalLimit(t *testing.T) {
	// 创建 4 个不同的服务器（模拟不同域名）
	trackers := make([]*concurrencyTracker, 4)
	servers := make([]*httptest.Server, 4)
	for i := 0; i < 4; i++ {
		trackers[i] = newConcurrencyTracker(200 * time.Millisecond)
		servers[i] = newTrackedServer(trackers[i])
		defer servers[i].Close()
	}

	sp := &multiDomainSpider{
		Base: spider.Base{
			SpiderName: "multi-domain-global",
		},
		servers:            servers,
		requestsPerServer:  10,
		concurrentRequests: 4, // 全局限制 4
		perDomain:          8, // 每域名限制 8（大于全局限制）
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	// 计算所有服务器的总峰值并发
	// 注意：由于各服务器独立追踪，我们需要一个全局视角
	// 这里检查各服务器的峰值之和是否超过全局限制
	var totalPeak int64
	var totalRequests int64
	for i, tracker := range trackers {
		peak := tracker.getPeak()
		total := tracker.getTotalRequests()
		totalPeak += peak
		totalRequests += total
		t.Logf("服务器 %d: peak=%d, total=%d", i, peak, total)
	}

	t.Logf("多域名: 各服务器峰值之和=%d（注意：峰值可能不在同一时刻，之和不等于全局峰值）, 总请求=%d", totalPeak, totalRequests)

	if totalRequests < 40 {
		t.Errorf("期望至少 40 个请求完成, 实际 %d", totalRequests)
	}

	// 注意：各服务器独立追踪峰值，峰值之和 ≠ 全局同时并发峰值。
	// 此测试仅验证请求是否全部完成。
	// 精确的全局并发验证请参见 TestConcurrentRequests_MultiDomain_GlobalTracker。
	for i, tracker := range trackers {
		perDomainPeak := tracker.getPeak()
		// 每域名的峰值不应超过 perDomain 限制（8）和全局限制（4）中的较小值
		if perDomainPeak > 4 {
			t.Logf("⚠️  服务器 %d 的峰值并发为 %d，超过了全局限制 4（NeedsBackout 竞态窗口导致）", i, perDomainPeak)
		}
	}
}

// ============================================================================
// 测试 5：CONCURRENT_REQUESTS_PER_DOMAIN 应限制单域名并发
// ============================================================================

func TestConcurrentRequestsPerDomain(t *testing.T) {
	tracker := newConcurrencyTracker(200 * time.Millisecond)
	server := newTrackedServer(tracker)
	defer server.Close()

	sp := &batchSpider{
		Base: spider.Base{
			SpiderName: "per-domain-limit",
		},
		baseURL:            server.URL,
		numRequests:        20,
		concurrentRequests: 16, // 全局限制较高
		perDomain:          2,  // 每域名限制为 2
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()

	t.Logf("PER_DOMAIN=2, GLOBAL=16: peak=%d, total=%d", peak, total)
	t.Logf("并发日志: %v", tracker.getConcurrencyLog())

	if total < 20 {
		t.Errorf("期望至少 20 个请求完成, 实际 %d", total)
	}

	// 单域名时，域名级限制应生效
	if peak > 2 {
		t.Errorf("CONCURRENT_REQUESTS_PER_DOMAIN=2 时峰值并发应 ≤ 2, 实际峰值为 %d（域名级并发限制未生效）", peak)
	}
}

// ============================================================================
// 测试 6：通过 Settings 对象设置 CONCURRENT_REQUESTS
// ============================================================================

func TestConcurrentRequests_ViaSettings(t *testing.T) {
	tracker := newConcurrencyTracker(200 * time.Millisecond)
	server := newTrackedServer(tracker)
	defer server.Close()

	// 使用 Settings 对象而非 CustomSettings
	s := settings.New()
	s.Set("CONCURRENT_REQUESTS", 2, settings.PriorityProject)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 2, settings.PriorityProject)
	s.Set("LOG_LEVEL", "WARN", settings.PriorityProject)

	sp := &batchSpider{
		Base: spider.Base{
			SpiderName: "settings-concurrency",
		},
		baseURL:            server.URL,
		numRequests:        15,
		concurrentRequests: 16, // Spider 级别设置较高
		perDomain:          16,
	}

	// 注意：Settings 的 PriorityProject(20) < PrioritySpider(30)
	// 所以 Spider 的 CustomSettings 会覆盖 Settings 对象的值
	// 这里测试的是 Settings 对象的优先级行为
	c := crawler.New(crawler.WithSettings(s))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()

	t.Logf("Settings CONCURRENT_REQUESTS=2 (被 Spider 覆盖为 16): peak=%d, total=%d", peak, total)

	// Spider 的 CustomSettings 优先级更高，所以实际生效的是 16
	// 这个测试验证优先级覆盖机制是否正确
	if total < 15 {
		t.Errorf("期望至少 15 个请求完成, 实际 %d", total)
	}
}

// ============================================================================
// 测试 7：Settings 优先级 - cmdline 级别应覆盖 Spider 级别
// ============================================================================

func TestConcurrentRequests_CmdlineOverridesSpider(t *testing.T) {
	tracker := newConcurrencyTracker(200 * time.Millisecond)
	server := newTrackedServer(tracker)
	defer server.Close()

	// 使用 cmdline 优先级（最高），应覆盖 Spider 的 CustomSettings
	s := settings.New()
	s.Set("CONCURRENT_REQUESTS", 2, settings.PriorityCmdline)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 2, settings.PriorityCmdline)
	s.Set("LOG_LEVEL", "WARN", settings.PriorityCmdline)

	sp := &batchSpider{
		Base: spider.Base{
			SpiderName: "cmdline-override",
		},
		baseURL:            server.URL,
		numRequests:        15,
		concurrentRequests: 16, // Spider 级别设置 16，但应被 cmdline 覆盖为 2
		perDomain:          16,
	}

	c := crawler.New(crawler.WithSettings(s))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()

	t.Logf("Cmdline CONCURRENT_REQUESTS=2 (覆盖 Spider 的 16): peak=%d, total=%d", peak, total)
	t.Logf("并发日志: %v", tracker.getConcurrencyLog())

	if total < 15 {
		t.Errorf("期望至少 15 个请求完成, 实际 %d", total)
	}

	// cmdline 优先级最高，应覆盖 Spider 的设置
	if peak > 2 {
		t.Errorf("Cmdline 级别 CONCURRENT_REQUESTS=2 应覆盖 Spider 级别的 16, 实际峰值为 %d", peak)
	}
}

// ============================================================================
// 测试 8：并发数从小到大的对比测试
// ============================================================================

func TestConcurrentRequests_ScalingComparison(t *testing.T) {
	limits := []int{1, 2, 4, 8}

	for _, limit := range limits {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			tracker := newConcurrencyTracker(150 * time.Millisecond)
			server := newTrackedServer(tracker)
			defer server.Close()

			sp := &batchSpider{
				Base: spider.Base{
					SpiderName: fmt.Sprintf("scaling-%d", limit),
				},
				baseURL:            server.URL,
				numRequests:        20,
				concurrentRequests: limit,
				perDomain:          limit,
			}

			c := crawler.NewDefault()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			start := time.Now()
			err := c.Run(ctx, sp)
			elapsed := time.Since(start)

			if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
				t.Fatalf("crawl error: %v", err)
			}

			peak := tracker.getPeak()
			total := tracker.getTotalRequests()

			t.Logf("CONCURRENT_REQUESTS=%d: peak=%d, total=%d, elapsed=%v", limit, peak, total, elapsed)

			if total < 20 {
				t.Errorf("期望至少 20 个请求完成, 实际 %d", total)
			}

			if peak > int64(limit) {
				t.Errorf("CONCURRENT_REQUESTS=%d 时峰值并发应 ≤ %d, 实际峰值为 %d", limit, limit, peak)
			}
		})
	}
}

// ============================================================================
// 测试 9：全局并发追踪器 - 精确测量跨域名的全局并发
// ============================================================================

// globalConcurrencyTracker 跨多个服务器追踪全局并发数。
type globalConcurrencyTracker struct {
	mu      sync.Mutex
	current int64
	peak    int64
	total   int64
	delay   time.Duration
	log     []int64
}

func newGlobalTracker(delay time.Duration) *globalConcurrencyTracker {
	return &globalConcurrencyTracker{delay: delay}
}

func (gt *globalConcurrencyTracker) handler(w http.ResponseWriter, r *http.Request) {
	gt.mu.Lock()
	gt.current++
	gt.total++
	current := gt.current
	if current > gt.peak {
		gt.peak = current
	}
	gt.log = append(gt.log, current)
	gt.mu.Unlock()

	if gt.delay > 0 {
		time.Sleep(gt.delay)
	}

	gt.mu.Lock()
	gt.current--
	gt.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<html><body><h1>OK</h1></body></html>`)
}

func TestConcurrentRequests_MultiDomain_GlobalTracker(t *testing.T) {
	// 使用全局追踪器，精确测量跨域名的并发数
	globalTracker := newGlobalTracker(200 * time.Millisecond)

	// 创建 4 个服务器，共享同一个全局追踪器
	servers := make([]*httptest.Server, 4)
	for i := 0; i < 4; i++ {
		mux := http.NewServeMux()
		mux.HandleFunc("/", globalTracker.handler)
		servers[i] = httptest.NewServer(mux)
		defer servers[i].Close()
	}

	sp := &multiDomainSpider{
		Base: spider.Base{
			SpiderName: "global-tracker",
		},
		servers:            servers,
		requestsPerServer:  8,
		concurrentRequests: 4, // 全局限制 4
		perDomain:          4, // 每域名限制 4
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	globalTracker.mu.Lock()
	peak := globalTracker.peak
	total := globalTracker.total
	log := make([]int64, len(globalTracker.log))
	copy(log, globalTracker.log)
	globalTracker.mu.Unlock()

	t.Logf("全局追踪: peak=%d, total=%d", peak, total)
	t.Logf("全局并发日志: %v", log)

	if total < 32 {
		t.Errorf("期望至少 32 个请求完成, 实际 %d", total)
	}

	// 全局并发限制为 4，峰值不应超过 4
	if peak > 4 {
		t.Errorf("CONCURRENT_REQUESTS=4 时全局峰值并发应 ≤ 4, 实际峰值为 %d（全局并发限制在多域名场景下未生效）", peak)
	}
}

// ============================================================================
// 测试 10：NeedsBackout 竞态条件验证
// ============================================================================

func TestConcurrentRequests_RaceCondition(t *testing.T) {
	// 使用极短的请求延迟，放大竞态条件
	tracker := newConcurrencyTracker(50 * time.Millisecond)
	server := newTrackedServer(tracker)
	defer server.Close()

	sp := &batchSpider{
		Base: spider.Base{
			SpiderName: "race-condition",
		},
		baseURL:            server.URL,
		numRequests:        50,
		concurrentRequests: 2,
		perDomain:          2,
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()
	log := tracker.getConcurrencyLog()

	t.Logf("竞态条件测试: CONCURRENT_REQUESTS=2, peak=%d, total=%d", peak, total)

	// 统计超过限制的次数
	overLimitCount := 0
	for _, c := range log {
		if c > 2 {
			overLimitCount++
		}
	}

	if overLimitCount > 0 {
		t.Logf("⚠️  检测到 %d 次并发数超过限制（共 %d 次请求）", overLimitCount, len(log))
		t.Logf("并发日志: %v", log)
	}

	if peak > 2 {
		t.Errorf("CONCURRENT_REQUESTS=2 时峰值并发应 ≤ 2, 实际峰值为 %d（存在竞态条件）", peak)
	}
}

// ============================================================================
// 测试 11：基于耗时验证 - 模拟慢速服务器（类似 quotes 场景）
// ============================================================================

// newSlowServer 创建一个每个请求都有固定耗时的服务器。
// 同时追踪并发数和请求时间线。
func newSlowServer(delay time.Duration) (*httptest.Server, *concurrencyTracker) {
	tracker := newConcurrencyTracker(delay)
	server := newTrackedServer(tracker)
	return server, tracker
}

// TestConcurrentRequests_ElapsedTime_Serial 验证 CONCURRENT_REQUESTS=1 时，
// N 个请求应串行执行，总耗时 ≈ N * requestDelay。
// 这是对 quotes 场景的精确复现：每个请求 sleep 固定时间。
func TestConcurrentRequests_ElapsedTime_Serial(t *testing.T) {
	const requestDelay = 500 * time.Millisecond
	const numRequests = 6

	server, tracker := newSlowServer(requestDelay)
	defer server.Close()

	sp := &batchSpider{
		Base: spider.Base{
			SpiderName: "elapsed-serial",
		},
		baseURL:            server.URL,
		numRequests:        numRequests,
		concurrentRequests: 1,
		perDomain:          1,
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	err := c.Run(ctx, sp)
	elapsed := time.Since(start)

	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()

	t.Logf("CONCURRENT_REQUESTS=1, 每请求耗时=%v, 请求数=%d", requestDelay, numRequests)
	t.Logf("结果: peak=%d, total=%d, 总耗时=%v", peak, total, elapsed)
	t.Logf("并发日志: %v", tracker.getConcurrencyLog())

	if total < int64(numRequests) {
		t.Errorf("期望 %d 个请求完成, 实际 %d", numRequests, total)
	}

	// 串行执行时，总耗时应 ≥ numRequests * requestDelay
	expectedMinElapsed := time.Duration(numRequests) * requestDelay
	if elapsed < expectedMinElapsed {
		t.Errorf("CONCURRENT_REQUESTS=1 时 %d 个请求（每个 %v）应至少耗时 %v, 实际仅 %v（并发控制未生效，请求被并行执行了）",
			numRequests, requestDelay, expectedMinElapsed, elapsed)
	}

	if peak > 1 {
		t.Errorf("CONCURRENT_REQUESTS=1 时峰值并发应为 1, 实际 %d", peak)
	}
}

// TestConcurrentRequests_ElapsedTime_Parallel2 验证 CONCURRENT_REQUESTS=2 时，
// N 个请求应以 2 并发执行，总耗时 ≈ (N/2) * requestDelay。
func TestConcurrentRequests_ElapsedTime_Parallel2(t *testing.T) {
	const requestDelay = 500 * time.Millisecond
	const numRequests = 6

	server, tracker := newSlowServer(requestDelay)
	defer server.Close()

	sp := &batchSpider{
		Base: spider.Base{
			SpiderName: "elapsed-parallel-2",
		},
		baseURL:            server.URL,
		numRequests:        numRequests,
		concurrentRequests: 2,
		perDomain:          2,
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	err := c.Run(ctx, sp)
	elapsed := time.Since(start)

	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()

	t.Logf("CONCURRENT_REQUESTS=2, 每请求耗时=%v, 请求数=%d", requestDelay, numRequests)
	t.Logf("结果: peak=%d, total=%d, 总耗时=%v", peak, total, elapsed)
	t.Logf("并发日志: %v", tracker.getConcurrencyLog())

	if total < int64(numRequests) {
		t.Errorf("期望 %d 个请求完成, 实际 %d", numRequests, total)
	}

	// 2 并发时，总耗时应 ≈ ceil(N/2) * requestDelay
	// 允许一定的调度开销，但不应低于理论最小值
	batchCount := (numRequests + 1) / 2 // ceil(6/2) = 3
	expectedMinElapsed := time.Duration(batchCount) * requestDelay
	// 不应快于 2 并发的理论最小值（说明并发数超过了 2）
	if elapsed < expectedMinElapsed-200*time.Millisecond {
		t.Errorf("CONCURRENT_REQUESTS=2 时 %d 个请求应至少耗时 %v, 实际仅 %v（并发数可能超过 2）",
			numRequests, expectedMinElapsed, elapsed)
	}

	// 不应慢于串行执行（说明并发确实生效了）
	serialElapsed := time.Duration(numRequests) * requestDelay
	if elapsed > serialElapsed+500*time.Millisecond {
		t.Errorf("CONCURRENT_REQUESTS=2 时不应慢于串行执行 (%v), 实际 %v（并发可能未生效）",
			serialElapsed, elapsed)
	}

	if peak > 2 {
		t.Errorf("CONCURRENT_REQUESTS=2 时峰值并发应 ≤ 2, 实际 %d", peak)
	}
}

// TestConcurrentRequests_ElapsedTime_Comparison 对比不同并发数下的耗时，
// 验证并发数翻倍时耗时应大致减半。
func TestConcurrentRequests_ElapsedTime_Comparison(t *testing.T) {
	const requestDelay = 300 * time.Millisecond
	const numRequests = 12

	type result struct {
		limit   int
		elapsed time.Duration
		peak    int64
		total   int64
	}

	var results []result

	for _, limit := range []int{1, 2, 4, 6} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			server, tracker := newSlowServer(requestDelay)
			defer server.Close()

			sp := &batchSpider{
				Base: spider.Base{
					SpiderName: fmt.Sprintf("elapsed-cmp-%d", limit),
				},
				baseURL:            server.URL,
				numRequests:        numRequests,
				concurrentRequests: limit,
				perDomain:          limit,
			}

			c := crawler.NewDefault()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			start := time.Now()
			err := c.Run(ctx, sp)
			elapsed := time.Since(start)

			if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
				t.Fatalf("crawl error: %v", err)
			}

			peak := tracker.getPeak()
			total := tracker.getTotalRequests()

			r := result{limit: limit, elapsed: elapsed, peak: peak, total: total}
			results = append(results, r)

			// 理论最小耗时 = ceil(numRequests/limit) * requestDelay
			batchCount := (numRequests + limit - 1) / limit
			expectedMin := time.Duration(batchCount) * requestDelay

			t.Logf("CONCURRENT_REQUESTS=%d: peak=%d, total=%d, 耗时=%v, 理论最小=%v",
				limit, peak, total, elapsed, expectedMin)
			t.Logf("并发日志: %v", tracker.getConcurrencyLog())

			if total < int64(numRequests) {
				t.Errorf("期望 %d 个请求完成, 实际 %d", numRequests, total)
			}

			if peak > int64(limit) {
				t.Errorf("CONCURRENT_REQUESTS=%d 时峰值并发应 ≤ %d, 实际 %d", limit, limit, peak)
			}

			// 耗时不应低于理论最小值（允许 200ms 误差）
			if elapsed < expectedMin-200*time.Millisecond {
				t.Errorf("耗时 %v 低于理论最小值 %v（并发数可能超过 %d）", elapsed, expectedMin, limit)
			}
		})
	}
}

// ============================================================================
// 测试 12：仅设置 CONCURRENT_REQUESTS 不设置 PER_DOMAIN 时的行为
// （复现用户 quotes 场景：ConcurrentRequests=1 但未设置 perDomain）
// ============================================================================

// quotesLikeSpider 模拟 quotes 场景：只设置 ConcurrentRequests，不设置 perDomain。
type quotesLikeSpider struct {
	spider.Base
	baseURL            string
	numRequests        int
	concurrentRequests int
}

func (s *quotesLikeSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		for i := 0; i < s.numRequests; i++ {
			url := fmt.Sprintf("%s/page/%d", s.baseURL, i)
			req, err := shttp.NewRequest(url, shttp.WithDontFilter(true))
			if err != nil {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- spider.Output{Request: req}:
			}
		}
	}()
	return ch
}

func (s *quotesLikeSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

func (s *quotesLikeSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(s.concurrentRequests),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("WARN"),
	}
}

func TestConcurrentRequests_OnlyGlobalLimit_NoPerDomain(t *testing.T) {
	const requestDelay = 500 * time.Millisecond
	const numRequests = 6

	server, tracker := newSlowServer(requestDelay)
	defer server.Close()

	// 只设置 ConcurrentRequests=1，不设置 perDomain（默认为 8）
	sp := &quotesLikeSpider{
		Base: spider.Base{
			SpiderName: "quotes-like",
		},
		baseURL:            server.URL,
		numRequests:        numRequests,
		concurrentRequests: 1,
	}

	c := crawler.NewDefault()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	err := c.Run(ctx, sp)
	elapsed := time.Since(start)

	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	peak := tracker.getPeak()
	total := tracker.getTotalRequests()

	t.Logf("仅设置 CONCURRENT_REQUESTS=1（perDomain 默认 8）, 每请求耗时=%v, 请求数=%d", requestDelay, numRequests)
	t.Logf("结果: peak=%d, total=%d, 总耗时=%v", peak, total, elapsed)
	t.Logf("并发日志: %v", tracker.getConcurrencyLog())

	if total < int64(numRequests) {
		t.Errorf("期望 %d 个请求完成, 实际 %d", numRequests, total)
	}

	// 如果 CONCURRENT_REQUESTS=1 真正生效，6 个请求串行应至少 3 秒
	expectedMinElapsed := time.Duration(numRequests) * requestDelay
	if elapsed < expectedMinElapsed {
		t.Errorf("CONCURRENT_REQUESTS=1 时 %d 个请求（每个 %v）应至少耗时 %v, 实际仅 %v\n"+
			"    → 这说明 CONCURRENT_REQUESTS=1 未生效！\n"+
			"    → 原因：CONCURRENT_REQUESTS_PER_DOMAIN 默认为 8，Slot 的 transferSem 容量为 8，\n"+
			"      允许 8 个请求并行传输，绕过了全局并发限制。",
			numRequests, requestDelay, expectedMinElapsed, elapsed)
	}

	if peak > 1 {
		t.Errorf("CONCURRENT_REQUESTS=1 时峰值并发应为 1, 实际 %d\n"+
			"    → 这说明 CONCURRENT_REQUESTS=1 未生效！\n"+
			"    → Slot 的 transferSem 容量为 CONCURRENT_REQUESTS_PER_DOMAIN（默认 8），\n"+
			"      而非 CONCURRENT_REQUESTS（1），导致域名内并发不受全局限制约束。",
			peak)
	}
}
