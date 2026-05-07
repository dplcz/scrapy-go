// Package integration 的 runner_test.go 提供 CrawlerRunner 多爬虫调度器的端到端集成测试。
//
// 对应 Phase 2 Sprint 5 任务 P2-010 的验收标准：
//   - StartConcurrent() 可同时运行多个 Spider
//   - StartSequentially() 按顺序运行
//   - 跨爬虫 Signal（SpiderOpened/SpiderClosed）正确传播
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
	sig "github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 集成测试辅助
// ============================================================================

// countingSpider 是记录处理页面数的简易 Spider，支持单个起始 URL。
type countingSpider struct {
	spider.Base
	pageCount atomic.Int32
}

func newCountingSpider(name string, urls []string) *countingSpider {
	return &countingSpider{
		Base: spider.Base{
			SpiderName: name,
			StartURLs:  urls,
		},
	}
}

func (s *countingSpider) Parse(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
	s.pageCount.Add(1)
	return nil, nil
}

// newRunnerTestSite 启动一个简单的本地 HTTP 服务器，按路径返回不同的 HTML 内容。
func newRunnerTestSite(t *testing.T, perRequestDelay time.Duration) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if perRequestDelay > 0 {
			time.Sleep(perRequestDelay)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h1>%s</h1></body></html>", r.URL.Path)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// ============================================================================
// 并发启动：StartConcurrent
// ============================================================================

// TestRunner_E2E_ConcurrentMultipleSpiders 验证 Runner 能同时运行多个 Spider，
// 每个 Spider 独立拥有自己的组件（Scheduler/Downloader/Pipelines），互不干扰。
func TestRunner_E2E_ConcurrentMultipleSpiders(t *testing.T) {
	site := newRunnerTestSite(t, 80*time.Millisecond)
	r := crawler.NewRunner(crawler.WithOSSignalHandling(false))

	const n = 5
	spiders := make([]*countingSpider, n)
	jobs := make([]crawler.Job, n)
	for i := 0; i < n; i++ {
		spiders[i] = newCountingSpider(
			fmt.Sprintf("spider-%d", i),
			[]string{site.URL + fmt.Sprintf("/spider-%d", i)},
		)
		jobs[i] = crawler.NewJob(crawler.NewDefault(), spiders[i])
	}

	start := time.Now()
	err := r.StartConcurrent(context.Background(), jobs...)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StartConcurrent returned error: %v", err)
	}

	// 每个 spider 应恰好处理一次请求
	for i, sp := range spiders {
		if got := sp.pageCount.Load(); got != 1 {
			t.Errorf("spider[%d] pageCount = %d, want 1", i, got)
		}
	}

	// 验收标准：并发运行应显著快于顺序运行（5 * 80ms = 400ms）
	// 并发理论耗时接近 80ms，给出宽松上限 1.5s 以适应 CI 波动
	if elapsed > 1500*time.Millisecond {
		t.Errorf("concurrent run took too long: %v", elapsed)
	}
	t.Logf("concurrent run finished in %v (vs sequential ~%v)", elapsed, time.Duration(n)*80*time.Millisecond)
}

// TestRunner_E2E_ConcurrentFiveOrMoreSpiders 对应全局成功指标「支持同时运行 >= 5 个 Spider」。
func TestRunner_E2E_ConcurrentFiveOrMoreSpiders(t *testing.T) {
	site := newRunnerTestSite(t, 0)
	r := crawler.NewRunner(crawler.WithOSSignalHandling(false))

	const n = 8
	spiders := make([]*countingSpider, n)
	jobs := make([]crawler.Job, n)
	for i := 0; i < n; i++ {
		spiders[i] = newCountingSpider(
			fmt.Sprintf("spider-%d", i),
			[]string{site.URL + "/"},
		)
		jobs[i] = crawler.NewJob(crawler.NewDefault(), spiders[i])
	}

	if err := r.StartConcurrent(context.Background(), jobs...); err != nil {
		t.Fatalf("StartConcurrent returned error: %v", err)
	}
	if got := len(r.Crawlers()); got != 0 {
		t.Errorf("expected 0 crawlers after completion, got %d", got)
	}
	for i, sp := range spiders {
		if got := sp.pageCount.Load(); got != 1 {
			t.Errorf("spider[%d] pageCount = %d, want 1", i, got)
		}
	}
}

// ============================================================================
// 顺序启动：StartSequentially
// ============================================================================

// TestRunner_E2E_SequentialOrder 验证 StartSequentially 严格按顺序执行 Spider。
func TestRunner_E2E_SequentialOrder(t *testing.T) {
	site := newRunnerTestSite(t, 30*time.Millisecond)
	r := crawler.NewRunner(crawler.WithOSSignalHandling(false))

	var mu sync.Mutex
	var order []string

	// 通过 SpiderClosed 信号记录完成顺序
	r.ConnectSignal(sig.SpiderClosed, func(params map[string]any) error {
		if sp, ok := params["spider"].(spider.Spider); ok {
			mu.Lock()
			order = append(order, sp.Name())
			mu.Unlock()
		}
		return nil
	})

	const n = 4
	jobs := make([]crawler.Job, n)
	for i := 0; i < n; i++ {
		jobs[i] = crawler.NewJob(
			crawler.NewDefault(),
			newCountingSpider(
				fmt.Sprintf("spider-%d", i),
				[]string{site.URL + "/"},
			),
		)
	}

	start := time.Now()
	if err := r.StartSequentially(context.Background(), jobs...); err != nil {
		t.Fatalf("StartSequentially returned error: %v", err)
	}
	elapsed := time.Since(start)

	// 顺序执行耗时应至少等于 n * perRequestDelay
	minExpected := time.Duration(n) * 30 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("sequential run finished too fast (%v < %v), may be running concurrently", elapsed, minExpected)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != n {
		t.Fatalf("expected %d SpiderClosed signals, got %d (order=%v)", n, len(order), order)
	}
	for i, name := range order {
		want := fmt.Sprintf("spider-%d", i)
		if name != want {
			t.Errorf("order[%d] = %q, want %q (full order: %v)", i, name, want, order)
		}
	}
}

// ============================================================================
// 跨爬虫 Signal 传播
// ============================================================================

// TestRunner_E2E_CrossCrawlerSignalPropagation 验证 ConnectSignal 注册的处理器在所有爬虫中都能被触发。
func TestRunner_E2E_CrossCrawlerSignalPropagation(t *testing.T) {
	site := newRunnerTestSite(t, 0)
	r := crawler.NewRunner(crawler.WithOSSignalHandling(false))

	var (
		mu          sync.Mutex
		openedNames []string
		closedNames []string
		openedCount atomic.Int32
		closedCount atomic.Int32
	)

	r.ConnectSignal(sig.SpiderOpened, func(params map[string]any) error {
		openedCount.Add(1)
		if sp, ok := params["spider"].(spider.Spider); ok {
			mu.Lock()
			openedNames = append(openedNames, sp.Name())
			mu.Unlock()
		}
		return nil
	})
	r.ConnectSignal(sig.SpiderClosed, func(params map[string]any) error {
		closedCount.Add(1)
		if sp, ok := params["spider"].(spider.Spider); ok {
			mu.Lock()
			closedNames = append(closedNames, sp.Name())
			mu.Unlock()
		}
		return nil
	})

	const n = 6
	jobs := make([]crawler.Job, n)
	for i := 0; i < n; i++ {
		jobs[i] = crawler.NewJob(
			crawler.NewDefault(),
			newCountingSpider(fmt.Sprintf("s%d", i), []string{site.URL + "/"}),
		)
	}

	if err := r.StartConcurrent(context.Background(), jobs...); err != nil {
		t.Fatalf("StartConcurrent returned error: %v", err)
	}

	if got := openedCount.Load(); got != n {
		t.Errorf("SpiderOpened fired %d times, want %d", got, n)
	}
	if got := closedCount.Load(); got != n {
		t.Errorf("SpiderClosed fired %d times, want %d", got, n)
	}

	// 所有 Spider 名称均应出现在 opened 和 closed 列表中
	mu.Lock()
	defer mu.Unlock()
	checkAllPresent := func(label string, names []string) {
		if len(names) != n {
			t.Errorf("%s: got %d names, want %d: %v", label, len(names), n, names)
			return
		}
		seen := make(map[string]bool)
		for _, name := range names {
			seen[name] = true
		}
		for i := 0; i < n; i++ {
			want := fmt.Sprintf("s%d", i)
			if !seen[want] {
				t.Errorf("%s: missing spider name %q (full list: %v)", label, want, names)
			}
		}
	}
	checkAllPresent("openedNames", openedNames)
	checkAllPresent("closedNames", closedNames)
}

// ============================================================================
// 优雅停止：Stop + Wait
// ============================================================================

// TestRunner_E2E_StopGracefullyInterruptsAllCrawlers 验证 Runner.Stop 能同时中断所有正在运行的 Crawler。
func TestRunner_E2E_StopGracefullyInterruptsAllCrawlers(t *testing.T) {
	// 较长延迟模拟长时间运行的爬虫
	site := newRunnerTestSite(t, 3*time.Second)
	r := crawler.NewRunner(crawler.WithOSSignalHandling(false))

	const n = 3
	jobs := make([]crawler.Job, n)
	for i := 0; i < n; i++ {
		jobs[i] = crawler.NewJob(
			crawler.NewDefault(),
			newCountingSpider(fmt.Sprintf("slow-%d", i), []string{site.URL + "/"}),
		)
	}

	// 异步启动所有爬虫
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.StartConcurrent(context.Background(), jobs...)
	}()

	// 等待爬虫启动
	time.Sleep(200 * time.Millisecond)

	// 发起 Stop，所有爬虫应被优雅中断
	stopStart := time.Now()
	r.Stop()
	r.Wait()
	stopElapsed := time.Since(stopStart)

	// Stop 后应在远短于 3s 的时间内完成（中断了正在 sleep 的请求）
	if stopElapsed > 3*time.Second {
		t.Errorf("Stop + Wait took too long: %v", stopElapsed)
	}

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("StartConcurrent did not return within 5s after Stop")
	}

	if got := len(r.Crawlers()); got != 0 {
		t.Errorf("expected 0 crawlers after Stop+Wait, got %d", got)
	}
}
