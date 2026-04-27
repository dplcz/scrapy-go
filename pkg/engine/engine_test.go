package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/pkg/downloader"
	dmiddle "github.com/dplcz/scrapy-go/pkg/downloader/middleware"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
	"github.com/dplcz/scrapy-go/pkg/scraper"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
	smiddle "github.com/dplcz/scrapy-go/pkg/spider/middleware"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// 本地测试网站
// ============================================================================

// newTestSite 创建一个本地测试网站，模拟 quotes.toscrape.com 的结构。
// 包含 3 个页面，每页 2 条引用，有分页链接。
func newTestSite() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body>
<div class="quote"><span class="text">Quote 1</span><span class="author">Author A</span></div>
<div class="quote"><span class="text">Quote 2</span><span class="author">Author B</span></div>
<nav><a href="/page/2" class="next">Next</a></nav>
</body></html>`)
	})

	mux.HandleFunc("/page/2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body>
<div class="quote"><span class="text">Quote 3</span><span class="author">Author C</span></div>
<div class="quote"><span class="text">Quote 4</span><span class="author">Author D</span></div>
<nav><a href="/page/3" class="next">Next</a></nav>
</body></html>`)
	})

	mux.HandleFunc("/page/3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body>
<div class="quote"><span class="text">Quote 5</span><span class="author">Author E</span></div>
<div class="quote"><span class="text">Quote 6</span><span class="author">Author F</span></div>
</body></html>`)
	})

	return httptest.NewServer(mux)
}

// newRedirectTestSite 创建一个带重定向的测试网站。
func newRedirectTestSite() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/new")
		w.WriteHeader(301)
	})

	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>Redirected Page</body></html>`)
	})

	return httptest.NewServer(mux)
}

// newRetryTestSite 创建一个需要重试的测试网站。
func newRetryTestSite() *httptest.Server {
	var count atomic.Int32
	mux := http.NewServeMux()

	mux.HandleFunc("/flaky", func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if n <= 2 {
			w.WriteHeader(500)
			fmt.Fprint(w, "Server Error")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>Success after retries</body></html>`)
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// 测试 Spider 实现
// ============================================================================

// quotesSpider 是一个简单的测试爬虫，爬取本地测试网站的引用。
type quotesSpider struct {
	spider.Base
	items []map[string]string
	mu    sync.Mutex
}

func newQuotesSpider(baseURL string) *quotesSpider {
	return &quotesSpider{
		Base: spider.Base{
			SpiderName: "quotes",
			StartURLs:  []string{baseURL + "/"},
		},
	}
}

func (s *quotesSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	var outputs []spider.Output

	// 简单的文本解析（不使用 HTML 解析器，保持测试简单）
	body := response.Text()

	// 提取引用（简单字符串匹配）
	for i := 1; i <= 10; i++ {
		text := fmt.Sprintf("Quote %d", i)
		if contains(body, text) {
			item := map[string]string{
				"text": text,
				"url":  response.URL.String(),
			}
			s.mu.Lock()
			s.items = append(s.items, item)
			s.mu.Unlock()
			outputs = append(outputs, spider.Output{Item: item})
		}
	}

	// 提取下一页链接
	if nextURL := extractNextPage(body); nextURL != "" {
		absURL, err := response.URLJoin(nextURL)
		if err == nil {
			req, _ := shttp.NewRequest(absURL)
			outputs = append(outputs, spider.Output{Request: req})
		}
	}

	return outputs, nil
}

// singlePageSpider 只爬取一个页面的简单爬虫。
type singlePageSpider struct {
	spider.Base
	parseCalled atomic.Bool
}

func newSinglePageSpider(url string) *singlePageSpider {
	return &singlePageSpider{
		Base: spider.Base{
			SpiderName: "single",
			StartURLs:  []string{url},
		},
	}
}

func (s *singlePageSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	s.parseCalled.Store(true)
	return []spider.Output{
		{Item: map[string]any{"url": response.URL.String(), "status": response.Status}},
	}, nil
}

// ============================================================================
// 辅助函数
// ============================================================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func extractNextPage(body string) string {
	// 简单提取 href="/page/X"
	marker := `href="/page/`
	idx := 0
	for i := 0; i <= len(body)-len(marker); i++ {
		if body[i:i+len(marker)] == marker {
			idx = i + len(`href="`)
			break
		}
	}
	if idx == 0 {
		return ""
	}
	end := idx
	for end < len(body) && body[end] != '"' {
		end++
	}
	return body[idx:end]
}

// buildTestEngine 构建一个用于测试的完整 Engine。
func buildTestEngine(sp spider.Spider, s *settings.Settings, sc stats.Collector, sm *signal.Manager) *Engine {
	if s == nil {
		s = settings.New()
		s.Set("CONCURRENT_REQUESTS", 4, settings.PriorityProject)
		s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 2, settings.PriorityProject)
		s.Set("DOWNLOAD_DELAY", 0, settings.PriorityProject)
		s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityProject)
		s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityProject)
	}
	if sc == nil {
		sc = stats.NewMemoryCollector(false, nil)
	}
	if sm == nil {
		sm = signal.NewManager(nil)
	}

	sched := scheduler.NewDefaultScheduler(
		scheduler.WithStats(sc),
	)

	timeout := s.GetDuration("DOWNLOAD_TIMEOUT", 10*time.Second)
	handler := downloader.NewHTTPDownloadHandler(timeout)
	dl := downloader.NewDownloader(s, handler, sm, sc, nil)

	dlMW := downloader.NewMiddlewareManager(nil)
	dlMW.AddMiddleware(dmiddle.NewUserAgentMiddleware("scrapy-go-test/0.1"), "UserAgent", 500)

	spMW := smiddle.NewManager(nil)
	pm := pipeline.NewManager(sm, sc, nil)
	sc2 := scraper.NewScraper(spMW, pm, sp, sm, sc, nil, 5000000)

	return NewEngine(sp, sched, dl, dlMW, sc2, sm, sc, nil, nil)
}

// ============================================================================
// 集成测试
// ============================================================================

func TestEngineBasicCrawl(t *testing.T) {
	site := newTestSite()
	defer site.Close()

	sp := newSinglePageSpider(site.URL + "/")
	sc := stats.NewMemoryCollector(false, nil)
	eng := buildTestEngine(sp, nil, sc, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := eng.Start(ctx)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sp.parseCalled.Load() {
		t.Error("Parse should have been called")
	}

	// 验证统计
	responseCount := sc.GetValue("response_received_count", 0)
	if responseCount == nil || responseCount == 0 {
		t.Error("should have received at least 1 response")
	}

	// 验证 HTTP 状态码统计
	status200Count := sc.GetValue("downloader/response_status_count/200", 0)
	if status200Count == nil || status200Count == 0 {
		t.Error("should have at least 1 response with status 200")
	}
}

func TestEngineMultiPageCrawl(t *testing.T) {
	site := newTestSite()
	defer site.Close()

	sp := newQuotesSpider(site.URL)
	sc := stats.NewMemoryCollector(false, nil)
	eng := buildTestEngine(sp, nil, sc, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	eng.Start(ctx)

	sp.mu.Lock()
	itemCount := len(sp.items)
	sp.mu.Unlock()

	if itemCount < 4 {
		t.Errorf("expected at least 4 items (from 2+ pages), got %d", itemCount)
	}

	// 验证调度器统计
	enqueued := sc.GetValue("scheduler/enqueued", 0)
	if enqueued == nil || enqueued == 0 {
		t.Error("should have enqueued requests")
	}
}

func TestEnginePauseResume(t *testing.T) {
	site := newTestSite()
	defer site.Close()

	sp := newSinglePageSpider(site.URL + "/")
	eng := buildTestEngine(sp, nil, nil, nil)

	if eng.IsPaused() {
		t.Error("should not be paused initially")
	}

	eng.Pause()
	if !eng.IsPaused() {
		t.Error("should be paused after Pause()")
	}

	eng.Unpause()
	if eng.IsPaused() {
		t.Error("should not be paused after Unpause()")
	}
}

func TestEngineContextCancellation(t *testing.T) {
	site := newTestSite()
	defer site.Close()

	sp := newSinglePageSpider(site.URL + "/")
	eng := buildTestEngine(sp, nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- eng.Start(ctx)
	}()

	// 等待引擎启动
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not stop after context cancellation")
	}
}

func TestEngineSignals(t *testing.T) {
	site := newTestSite()
	defer site.Close()

	sp := newSinglePageSpider(site.URL + "/")
	sm := signal.NewManager(nil)

	var engineStarted, engineStopped, spiderOpened, spiderClosed atomic.Bool

	sm.Connect(func(params map[string]any) error {
		engineStarted.Store(true)
		return nil
	}, signal.EngineStarted)

	sm.Connect(func(params map[string]any) error {
		engineStopped.Store(true)
		return nil
	}, signal.EngineStopped)

	sm.Connect(func(params map[string]any) error {
		spiderOpened.Store(true)
		return nil
	}, signal.SpiderOpened)

	sm.Connect(func(params map[string]any) error {
		spiderClosed.Store(true)
		return nil
	}, signal.SpiderClosed)

	eng := buildTestEngine(sp, nil, nil, sm)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eng.Start(ctx)

	if !engineStarted.Load() {
		t.Error("engine_started signal should have been sent")
	}
	if !engineStopped.Load() {
		t.Error("engine_stopped signal should have been sent")
	}
	if !spiderOpened.Load() {
		t.Error("spider_opened signal should have been sent")
	}
	if !spiderClosed.Load() {
		t.Error("spider_closed signal should have been sent")
	}
}

func TestEngineWithPipeline(t *testing.T) {
	site := newTestSite()
	defer site.Close()

	sp := newSinglePageSpider(site.URL + "/")
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)

	sched := scheduler.NewDefaultScheduler(scheduler.WithStats(sc))

	s := settings.New()
	s.Set("CONCURRENT_REQUESTS", 4, settings.PriorityProject)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 2, settings.PriorityProject)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityProject)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityProject)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityProject)

	handler := downloader.NewHTTPDownloadHandler(10 * time.Second)
	dl := downloader.NewDownloader(s, handler, sm, sc, nil)

	dlMW := downloader.NewMiddlewareManager(nil)
	spMW := smiddle.NewManager(nil)

	// 添加一个收集 Item 的 Pipeline
	collector := &itemCollectorPipeline{}
	pm := pipeline.NewManager(sm, sc, nil)
	pm.AddPipeline(collector, "collector", 100)

	sc2 := scraper.NewScraper(spMW, pm, sp, sm, sc, nil, 5000000)

	eng := NewEngine(sp, sched, dl, dlMW, sc2, sm, sc, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eng.Start(ctx)

	collector.mu.Lock()
	count := len(collector.items)
	collector.mu.Unlock()

	if count < 1 {
		t.Errorf("expected at least 1 item collected, got %d", count)
	}
}

func TestSlotBasic(t *testing.T) {
	sched := scheduler.NewDefaultScheduler()
	slot := NewSlot(sched, true)

	if !slot.IsIdle() {
		t.Error("new slot should be idle")
	}
	if slot.InProgressCount() != 0 {
		t.Error("new slot should have 0 in progress")
	}

	req := shttp.MustNewRequest("https://example.com")
	slot.AddRequest(req)

	if slot.IsIdle() {
		t.Error("should not be idle with request")
	}
	if slot.InProgressCount() != 1 {
		t.Errorf("expected 1 in progress, got %d", slot.InProgressCount())
	}

	slot.RemoveRequest(req)
	if !slot.IsIdle() {
		t.Error("should be idle after removing request")
	}
}

// ============================================================================
// 测试辅助类型
// ============================================================================

type itemCollectorPipeline struct {
	mu    sync.Mutex
	items []any
}

func (p *itemCollectorPipeline) Open(ctx context.Context) error  { return nil }
func (p *itemCollectorPipeline) Close(ctx context.Context) error { return nil }
func (p *itemCollectorPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	p.mu.Lock()
	p.items = append(p.items, item)
	p.mu.Unlock()
	return item, nil
}
