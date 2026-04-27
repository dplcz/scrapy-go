package crawler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	sig "github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 测试辅助：简易 Spider 与本地测试服务器
// ============================================================================

// simpleSpider 是一个最小化 Spider 实现，用于测试。
// 它产生指定数量的起始 URL 请求，不递归、不产出 Item。
type simpleSpider struct {
	spider.Base
	parsedCount atomic.Int32
}

func newSimpleSpider(name string, urls []string) *simpleSpider {
	return &simpleSpider{
		Base: spider.Base{
			SpiderName: name,
			StartURLs:  urls,
		},
	}
}

func (s *simpleSpider) Parse(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
	s.parsedCount.Add(1)
	return nil, nil
}

// newTestServer 启动一个始终返回简短 HTML 的本地测试服务器。
func newTestServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body>ok</body></html>")
	}))
	t.Cleanup(ts.Close)
	return ts
}

// newTestRunner 构造一个不安装 OS 信号处理器的 Runner，适合在测试中使用。
func newTestRunner() *Runner {
	return NewRunner(WithOSSignalHandling(false))
}

// ============================================================================
// Job / NewJob
// ============================================================================

func TestNewJob(t *testing.T) {
	c := NewDefault()
	sp := newSimpleSpider("s1", nil)
	job := NewJob(c, sp)

	if job.Crawler != c {
		t.Errorf("Job.Crawler = %v, want %v", job.Crawler, c)
	}
	if job.Spider != sp {
		t.Errorf("Job.Spider = %v, want %v", job.Spider, sp)
	}
}

// ============================================================================
// NewRunner / Options
// ============================================================================

func TestNewRunner_Defaults(t *testing.T) {
	r := NewRunner()
	if r == nil {
		t.Fatal("NewRunner returned nil")
	}
	if r.logger == nil {
		t.Error("Runner.logger should be initialized by default")
	}
	if !r.installOSSignals {
		t.Error("installOSSignals should default to true")
	}
	if r.crawlers == nil {
		t.Error("crawlers map should be initialized")
	}
}

func TestNewRunner_WithOptions(t *testing.T) {
	r := NewRunner(
		WithOSSignalHandling(false),
		WithRunnerLogger(nil), // 传 nil 不应覆盖默认 logger
	)
	if r.installOSSignals {
		t.Error("installOSSignals should be false")
	}
	if r.logger == nil {
		t.Error("logger should remain non-nil even if WithRunnerLogger(nil) is passed")
	}
}

// ============================================================================
// Crawler 管理
// ============================================================================

func TestRunner_AddAndRemoveCrawler(t *testing.T) {
	r := newTestRunner()
	c := NewDefault()

	if err := r.addCrawler(c); err != nil {
		t.Fatalf("addCrawler failed: %v", err)
	}
	if len(r.Crawlers()) != 1 {
		t.Errorf("expected 1 crawler, got %d", len(r.Crawlers()))
	}

	// 重复添加应返回错误
	if err := r.addCrawler(c); !errors.Is(err, ErrCrawlerAlreadyManaged) {
		t.Errorf("expected ErrCrawlerAlreadyManaged, got %v", err)
	}

	r.removeCrawler(c)
	if len(r.Crawlers()) != 0 {
		t.Errorf("expected 0 crawlers after removeCrawler, got %d", len(r.Crawlers()))
	}
}

func TestRunner_AddCrawler_AfterClose(t *testing.T) {
	r := newTestRunner()
	r.Close()

	c := NewDefault()
	if err := r.addCrawler(c); !errors.Is(err, ErrRunnerClosed) {
		t.Errorf("expected ErrRunnerClosed after Close, got %v", err)
	}
}

// ============================================================================
// 单爬虫 Crawl
// ============================================================================

func TestRunner_Crawl_Success(t *testing.T) {
	ts := newTestServer(t, 0)
	r := newTestRunner()
	c := NewDefault()
	sp := newSimpleSpider("ok", []string{ts.URL + "/"})

	done := r.Crawl(context.Background(), c, sp)
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Crawl returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Crawl did not finish within 10s")
	}

	if sp.parsedCount.Load() != 1 {
		t.Errorf("expected Parse called once, got %d", sp.parsedCount.Load())
	}
	if r.BootstrapFailed() {
		t.Error("BootstrapFailed should be false after successful run")
	}
	// 爬虫完成后应从 Runner 中移除
	if len(r.Crawlers()) != 0 {
		t.Errorf("expected 0 crawlers after finish, got %d", len(r.Crawlers()))
	}
}

func TestRunner_Crawl_NilCrawler(t *testing.T) {
	r := newTestRunner()
	done := r.Crawl(context.Background(), nil, newSimpleSpider("s", nil))
	err := <-done
	if err == nil {
		t.Fatal("expected error for nil crawler")
	}
}

func TestRunner_Crawl_NilSpider(t *testing.T) {
	r := newTestRunner()
	done := r.Crawl(context.Background(), NewDefault(), nil)
	err := <-done
	if err == nil {
		t.Fatal("expected error for nil spider")
	}
}

func TestRunner_Crawl_CrawlerReuse(t *testing.T) {
	ts := newTestServer(t, 0)
	r := newTestRunner()
	c := NewDefault()
	sp1 := newSimpleSpider("s1", []string{ts.URL + "/"})

	<-r.Crawl(context.Background(), c, sp1)

	// 相同 Crawler 再次运行应失败
	sp2 := newSimpleSpider("s2", []string{ts.URL + "/"})
	err := <-r.Crawl(context.Background(), c, sp2)
	if err == nil {
		t.Fatal("expected error when reusing Crawler instance")
	}
}

// ============================================================================
// 并发启动
// ============================================================================

func TestRunner_StartConcurrent_MultipleSpiders(t *testing.T) {
	ts := newTestServer(t, 50*time.Millisecond)
	r := newTestRunner()

	const n = 5
	jobs := make([]Job, n)
	spiders := make([]*simpleSpider, n)
	for i := 0; i < n; i++ {
		spiders[i] = newSimpleSpider(fmt.Sprintf("s%d", i), []string{ts.URL + "/"})
		jobs[i] = NewJob(NewDefault(), spiders[i])
	}

	start := time.Now()
	err := r.StartConcurrent(context.Background(), jobs...)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("StartConcurrent returned error: %v", err)
	}
	// 5 个 spider 并发运行，总耗时应明显少于 5 * 50ms = 250ms
	// 给出宽松上限避免 CI 波动：1.5s
	if elapsed > 1500*time.Millisecond {
		t.Errorf("concurrent run took too long: %v", elapsed)
	}
	for i, sp := range spiders {
		if got := sp.parsedCount.Load(); got != 1 {
			t.Errorf("spider[%d] parsedCount = %d, want 1", i, got)
		}
	}
}

func TestRunner_StartConcurrent_Empty(t *testing.T) {
	r := newTestRunner()
	if err := r.StartConcurrent(context.Background()); err != nil {
		t.Errorf("empty jobs should return nil, got %v", err)
	}
}

func TestRunner_StartConcurrent_DuplicateCrawler(t *testing.T) {
	r := newTestRunner()
	c := NewDefault()
	sp := newSimpleSpider("s", nil)

	err := r.StartConcurrent(context.Background(),
		NewJob(c, sp),
		NewJob(c, sp),
	)
	if err == nil {
		t.Fatal("expected error for duplicate crawler in jobs")
	}
}

func TestRunner_StartConcurrent_NilCrawlerInJobs(t *testing.T) {
	r := newTestRunner()
	err := r.StartConcurrent(context.Background(), Job{Crawler: nil, Spider: newSimpleSpider("s", nil)})
	if err == nil {
		t.Fatal("expected error for nil crawler in jobs")
	}
}

// ============================================================================
// 顺序启动
// ============================================================================

func TestRunner_StartSequentially_Order(t *testing.T) {
	ts := newTestServer(t, 20*time.Millisecond)
	r := newTestRunner()

	var mu sync.Mutex
	var finishOrder []string

	const n = 3
	jobs := make([]Job, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("s%d", i)
		sp := newSimpleSpider(name, []string{ts.URL + "/"})
		c := NewDefault()
		jobs[i] = NewJob(c, sp)

		// 为每个 spider 记录完成顺序（通过 Runner 的 ConnectSignal）
		_ = name
	}

	// 通过 ConnectSignal 监听 SpiderClosed 事件以验证顺序
	r.ConnectSignal(sig.SpiderClosed, func(params map[string]any) error {
		// spider 可通过 params["spider"] 获取
		if sp, ok := params["spider"].(spider.Spider); ok {
			mu.Lock()
			finishOrder = append(finishOrder, sp.Name())
			mu.Unlock()
		}
		return nil
	})

	start := time.Now()
	if err := r.StartSequentially(context.Background(), jobs...); err != nil {
		t.Fatalf("StartSequentially returned error: %v", err)
	}
	elapsed := time.Since(start)

	// 3 个 spider 顺序运行，总耗时应至少 3 * 20ms = 60ms
	if elapsed < 60*time.Millisecond {
		t.Errorf("sequential run too fast (may be running concurrently): %v", elapsed)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(finishOrder) != n {
		t.Fatalf("expected %d spider_closed signals, got %d: %v", n, len(finishOrder), finishOrder)
	}
	for i, name := range finishOrder {
		want := fmt.Sprintf("s%d", i)
		if name != want {
			t.Errorf("finishOrder[%d] = %q, want %q (full: %v)", i, name, want, finishOrder)
		}
	}
}

func TestRunner_StartSequentially_CancelMidway(t *testing.T) {
	ts := newTestServer(t, 100*time.Millisecond)
	r := newTestRunner()

	ctx, cancel := context.WithCancel(context.Background())

	jobs := []Job{
		NewJob(NewDefault(), newSimpleSpider("s1", []string{ts.URL + "/"})),
		NewJob(NewDefault(), newSimpleSpider("s2", []string{ts.URL + "/"})),
		NewJob(NewDefault(), newSimpleSpider("s3", []string{ts.URL + "/"})),
	}

	// 在 s1 运行中途取消 ctx
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_ = r.StartSequentially(ctx, jobs...)

	// s2, s3 不应被启动（started=false）
	if jobs[1].Crawler.IsCrawling() {
		t.Error("s2 should not have been started")
	}
	if jobs[2].Crawler.IsCrawling() {
		t.Error("s3 should not have been started")
	}
}

// ============================================================================
// 跨爬虫 Signal 传播
// ============================================================================

func TestRunner_ConnectSignal_CrossCrawlerPropagation(t *testing.T) {
	ts := newTestServer(t, 0)
	r := newTestRunner()

	var openedCount atomic.Int32
	var closedCount atomic.Int32
	r.ConnectSignal(sig.SpiderOpened, func(params map[string]any) error {
		openedCount.Add(1)
		return nil
	})
	r.ConnectSignal(sig.SpiderClosed, func(params map[string]any) error {
		closedCount.Add(1)
		return nil
	})

	const n = 4
	jobs := make([]Job, n)
	for i := 0; i < n; i++ {
		jobs[i] = NewJob(NewDefault(),
			newSimpleSpider(fmt.Sprintf("s%d", i), []string{ts.URL + "/"}))
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
}

func TestRunner_ConnectSignal_NilHandlerIgnored(t *testing.T) {
	r := newTestRunner()
	r.ConnectSignal(sig.SpiderOpened, nil)

	if got := len(r.snapshotHandlers()); got != 0 {
		t.Errorf("nil handler should be ignored, got %d handlers", got)
	}
}

// ============================================================================
// Stop / Wait / Close
// ============================================================================

func TestRunner_Stop_InterruptsRunningCrawler(t *testing.T) {
	// 服务器响应非常慢，模拟一个长时间运行的爬虫
	ts := newTestServer(t, 2*time.Second)
	r := newTestRunner()

	sp := newSimpleSpider("slow", []string{ts.URL + "/"})
	done := r.Crawl(context.Background(), NewDefault(), sp)

	// 给爬虫一点启动时间
	time.Sleep(100 * time.Millisecond)
	r.Stop()

	select {
	case <-done:
		// 成功在合理时间内停止
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not interrupt the crawler within 5s")
	}
}

func TestRunner_Close_StopsAndWaits(t *testing.T) {
	ts := newTestServer(t, 500*time.Millisecond)
	r := newTestRunner()

	for i := 0; i < 3; i++ {
		sp := newSimpleSpider(fmt.Sprintf("s%d", i), []string{ts.URL + "/"})
		_ = r.Crawl(context.Background(), NewDefault(), sp)
	}

	time.Sleep(50 * time.Millisecond) // 让所有 crawler 启动

	done := make(chan struct{})
	go func() {
		r.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return within 5s")
	}

	if len(r.Crawlers()) != 0 {
		t.Errorf("expected 0 crawlers after Close, got %d", len(r.Crawlers()))
	}

	// Close 之后不能再添加 Crawler
	if err := r.addCrawler(NewDefault()); !errors.Is(err, ErrRunnerClosed) {
		t.Errorf("expected ErrRunnerClosed after Close, got %v", err)
	}

	// 多次 Close 应该安全
	r.Close()
}

func TestRunner_Wait_NoCrawlersReturnsImmediately(t *testing.T) {
	r := newTestRunner()
	done := make(chan struct{})
	go func() {
		r.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Wait with no crawlers should return immediately")
	}
}

// ============================================================================
// Crawler 新增的辅助方法
// ============================================================================

func TestCrawler_StopAndSpider(t *testing.T) {
	ts := newTestServer(t, 1*time.Second)
	c := NewDefault()
	sp := newSimpleSpider("s", []string{ts.URL + "/"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Crawl(context.Background(), sp)
	}()

	// 等待 Crawler 真正开始运行
	deadline := time.Now().Add(2 * time.Second)
	for !c.IsCrawling() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !c.IsCrawling() {
		t.Fatal("crawler did not start within 2s")
	}
	if c.Spider() != sp {
		t.Errorf("Crawler.Spider() = %v, want %v", c.Spider(), sp)
	}

	c.Stop()

	select {
	case <-errCh:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("Crawler.Stop did not terminate Crawl within 3s")
	}

	if c.IsCrawling() {
		t.Error("IsCrawling should be false after Crawl returned")
	}
}

func TestCrawler_CrawlTwice_Rejected(t *testing.T) {
	ts := newTestServer(t, 0)
	c := NewDefault()
	sp := newSimpleSpider("s", []string{ts.URL + "/"})

	if err := c.Crawl(context.Background(), sp); err != nil {
		t.Fatalf("first Crawl failed: %v", err)
	}

	err := c.Crawl(context.Background(), sp)
	if err == nil {
		t.Fatal("expected error on second Crawl with same instance")
	}
}

func TestCrawler_Stop_WhenNotRunning_Noop(t *testing.T) {
	c := NewDefault()
	// 未运行时调用 Stop 不应 panic
	c.Stop()
}

// ============================================================================
// 并发安全压力测试
// ============================================================================

func TestRunner_ConcurrentAddAndClose_RaceSafe(t *testing.T) {
	r := newTestRunner()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.addCrawler(NewDefault())
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.Close()
	}()
	wg.Wait()
	// 主要目的：配合 -race 检测数据竞争
}

// ============================================================================
// OS 信号处理路径（不发送真实信号，仅验证 installSignalHandler 的启用路径不会 panic 或泄漏）
// ============================================================================

func TestRunner_InstallSignalHandler_Enabled(t *testing.T) {
	r := NewRunner() // 默认启用 OS 信号处理
	ctx, cancel := r.installSignalHandler(context.Background())

	// 手动 cancel 触发 goroutine 退出路径
	cancel()

	// ctx 应立即被取消
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ctx was not cancelled after wrappedCancel was called")
	}
}

func TestRunner_StartConcurrent_WithOSSignalsEnabled(t *testing.T) {
	// 使用默认 Runner（启用 OS 信号），验证正常流程不会因信号 goroutine 出问题
	ts := newTestServer(t, 0)
	r := NewRunner()

	job := NewJob(NewDefault(), newSimpleSpider("s", []string{ts.URL + "/"}))
	if err := r.StartConcurrent(context.Background(), job); err != nil {
		t.Fatalf("StartConcurrent returned error: %v", err)
	}
}
