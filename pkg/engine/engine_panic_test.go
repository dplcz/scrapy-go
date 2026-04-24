package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"scrapy-go/pkg/downloader"
	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/pipeline"
	"scrapy-go/pkg/scheduler"
	"scrapy-go/pkg/scraper"
	"scrapy-go/pkg/settings"
	"scrapy-go/pkg/signal"
	"scrapy-go/pkg/spider"
	spider_mw "scrapy-go/pkg/spider/middleware"
	"scrapy-go/pkg/stats"
)

// ============================================================================
// Panic 测试用 Spider 实现
// ============================================================================

// panicParseSpider 在 Parse 中触发 panic。
type panicParseSpider struct {
	spider.Base
	parseCalled atomic.Bool
}

func newPanicParseSpider(url string) *panicParseSpider {
	return &panicParseSpider{
		Base: spider.Base{
			SpiderName: "panic-parse",
			StartURLs:  []string{url},
		},
	}
}

func (s *panicParseSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	s.parseCalled.Store(true)
	panic("intentional panic in Parse")
}

// panicStartSpider 在 Start 的内部 goroutine 中触发 panic。
// 使用 Base 的 Start 模式，内部 goroutine 的 panic 由 Base.Start 的 recover 捕获。
type panicStartSpider struct {
	spider.Base
	panicRecovered atomic.Bool
}

func (s *panicStartSpider) Name() string { return "panic-start" }

func (s *panicStartSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		// 模拟用户在 Start goroutine 中的 panic，需要自行 recover
		defer func() {
			if r := recover(); r != nil {
				s.panicRecovered.Store(true)
			}
		}()
		panic("intentional panic in Start")
	}()
	return ch
}

func (s *panicStartSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	return nil, nil
}

// panicStartConsumerSpider 在 Start 方法体中直接 panic（不在 goroutine 中）。
// 这测试 consumeStartRequests 中的 recover 能否捕获 Start() 调用中的 panic。
type panicStartConsumerSpider struct {
	spider.Base
}

func (s *panicStartConsumerSpider) Name() string { return "panic-start-consumer" }

func (s *panicStartConsumerSpider) Start(ctx context.Context) <-chan spider.Output {
	// 直接在 Start 方法体中 panic，不在 goroutine 中
	panic("intentional panic in Start method body")
}

func (s *panicStartConsumerSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	return nil, nil
}

// panicCallbackSpider 在自定义 Callback 中触发 panic。
type panicCallbackSpider struct {
	spider.Base
}

func newPanicCallbackSpider(url string) *panicCallbackSpider {
	return &panicCallbackSpider{
		Base: spider.Base{
			SpiderName: "panic-callback",
			StartURLs:  []string{url},
		},
	}
}

func (s *panicCallbackSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	// 返回一个带有 panic callback 的请求
	req, _ := scrapy_http.NewRequest(response.URL.String()+"/next",
		scrapy_http.WithCallback(spider.CallbackFunc(func(ctx context.Context, resp *scrapy_http.Response) ([]spider.Output, error) {
			panic("intentional panic in Callback")
		})),
	)
	return []spider.Output{{Request: req}}, nil
}

// panicPipelineSpider 返回 Item 触发 Pipeline panic。
type panicPipelineSpider struct {
	spider.Base
}

func newPanicPipelineSpider(url string) *panicPipelineSpider {
	return &panicPipelineSpider{
		Base: spider.Base{
			SpiderName: "panic-pipeline",
			StartURLs:  []string{url},
		},
	}
}

func (s *panicPipelineSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	return []spider.Output{
		{Item: map[string]any{"url": response.URL.String()}},
	}, nil
}

// panicPipeline 在 ProcessItem 中触发 panic。
type panicPipeline struct{}

func (p *panicPipeline) Open(ctx context.Context) error  { return nil }
func (p *panicPipeline) Close(ctx context.Context) error { return nil }
func (p *panicPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	panic("intentional panic in Pipeline")
}

// ============================================================================
// 测试辅助函数
// ============================================================================

// newSimpleTestSite 创建一个简单的测试网站。
func newSimpleTestSite() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body>Hello World</body></html>`)
	})
	mux.HandleFunc("/next", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body>Next Page</body></html>`)
	})
	return httptest.NewServer(mux)
}

// buildTestEngineWithPipeline 构建一个带自定义 Pipeline 的测试 Engine。
func buildTestEngineWithPipeline(sp spider.Spider, pm *pipeline.Manager, sc stats.Collector, sm *signal.Manager) *Engine {
	s := defaultTestSettings()
	if sc == nil {
		sc = stats.NewMemoryCollector(false, nil)
	}
	if sm == nil {
		sm = signal.NewManager(nil)
	}

	sched := scheduler.NewDefaultScheduler(scheduler.WithStats(sc))
	handler := downloader.NewHTTPDownloadHandler(10 * time.Second)
	dl := downloader.NewDownloader(s, handler, sm, sc, nil)
	dlMW := downloader.NewMiddlewareManager(nil)
	spMW := spider_mw.NewManager(nil)

	if pm == nil {
		pm = pipeline.NewManager(sm, sc, nil)
	}

	sc2 := scraper.NewScraper(spMW, pm, sp, sm, sc, nil, 5000000)
	return NewEngine(sp, sched, dl, dlMW, sc2, sm, sc, nil)
}

// defaultTestSettings 返回测试用的默认配置。
func defaultTestSettings() *settings.Settings {
	s := settings.New()
	s.Set("CONCURRENT_REQUESTS", 4, settings.PriorityProject)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 2, settings.PriorityProject)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityProject)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityProject)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityProject)
	return s
}

// ============================================================================
// Panic Recovery 测试
// ============================================================================

// TestPanicInSpiderParse 验证 Spider.Parse 中的 panic 不会导致进程崩溃。
func TestPanicInSpiderParse(t *testing.T) {
	site := newSimpleTestSite()
	defer site.Close()

	sp := newPanicParseSpider(site.URL + "/")
	sc := stats.NewMemoryCollector(false, nil)
	eng := buildTestEngine(sp, nil, sc, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 如果没有 panic recovery，这里会导致进程崩溃
	err := eng.Start(ctx)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 Parse 确实被调用了
	if !sp.parseCalled.Load() {
		t.Error("Parse should have been called")
	}

	// 验证 panic 统计计数器
	panicCount := sc.GetValue("spider_exceptions/panic", 0)
	if panicCount == nil || panicCount == 0 {
		t.Error("spider_exceptions/panic should be incremented")
	}
}

// TestPanicInSpiderStart 验证 Spider.Start 内部 goroutine 中的 panic 不会导致进程崩溃。
func TestPanicInSpiderStart(t *testing.T) {
	sp := &panicStartSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	eng := buildTestEngine(sp, nil, sc, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 如果没有 panic recovery，这里会导致进程崩溃
	err := eng.Start(ctx)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 panic 被 Start 内部的 recover 捕获
	if !sp.panicRecovered.Load() {
		t.Error("panic in Start goroutine should be recovered")
	}
}

// TestPanicInConsumeStartRequests 验证 consumeStartRequests 中处理 Item 时的 panic 不会导致进程崩溃。
func TestPanicInConsumeStartRequests(t *testing.T) {
	sp := &panicStartConsumerSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	eng := buildTestEngine(sp, nil, sc, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 如果没有 panic recovery，这里会导致进程崩溃
	err := eng.Start(ctx)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 panic 统计计数器
	panicCount := sc.GetValue("spider_exceptions/panic", 0)
	if panicCount == nil || panicCount == 0 {
		t.Error("spider_exceptions/panic should be incremented for consumeStartRequests panic")
	}
}

// TestPanicInSpiderCallback 验证自定义 Callback 中的 panic 不会导致进程崩溃。
func TestPanicInSpiderCallback(t *testing.T) {
	site := newSimpleTestSite()
	defer site.Close()

	sp := newPanicCallbackSpider(site.URL)
	sc := stats.NewMemoryCollector(false, nil)
	eng := buildTestEngine(sp, nil, sc, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 如果没有 panic recovery，这里会导致进程崩溃
	err := eng.Start(ctx)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 panic 统计计数器（Parse 成功 + Callback panic = 至少 1 次 panic）
	panicCount := sc.GetValue("spider_exceptions/panic", 0)
	if panicCount == nil || panicCount == 0 {
		t.Error("spider_exceptions/panic should be incremented for Callback panic")
	}
}

// TestPanicInPipeline 验证 Pipeline.ProcessItem 中的 panic 不会导致进程崩溃。
func TestPanicInPipeline(t *testing.T) {
	site := newSimpleTestSite()
	defer site.Close()

	sp := newPanicPipelineSpider(site.URL + "/")
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)

	pm := pipeline.NewManager(sm, sc, nil)
	pm.AddPipeline(&panicPipeline{}, "panic", 100)

	eng := buildTestEngineWithPipeline(sp, pm, sc, sm)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 如果没有 panic recovery，这里会导致进程崩溃
	err := eng.Start(ctx)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 panic 统计计数器
	panicCount := sc.GetValue("spider_exceptions/panic", 0)
	if panicCount == nil || panicCount == 0 {
		t.Error("spider_exceptions/panic should be incremented for Pipeline panic")
	}
}

// TestPanicDoesNotAffectOtherRequests 验证一个请求的 panic 不会影响其他请求的处理。
func TestPanicDoesNotAffectOtherRequests(t *testing.T) {
	site := newSimpleTestSite()
	defer site.Close()

	// 使用一个在第一次调用时 panic，后续正常的 Spider
	sp := &selectivePanicSpider{
		Base: spider.Base{
			SpiderName: "selective-panic",
			StartURLs:  []string{site.URL + "/", site.URL + "/next"},
		},
	}

	sc := stats.NewMemoryCollector(false, nil)
	eng := buildTestEngine(sp, nil, sc, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := eng.Start(ctx)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证有 panic 发生
	panicCount := sc.GetValue("spider_exceptions/panic", 0)
	if panicCount == nil || panicCount == 0 {
		t.Error("should have at least 1 panic")
	}

	// 验证有成功的响应（说明 panic 没有影响其他请求）
	responseCount := sc.GetValue("response_received_count", 0)
	if responseCount == nil || responseCount == 0 {
		t.Error("should have received at least 1 response (panic should not block other requests)")
	}

	// 验证成功处理的 Item
	if sp.successCount.Load() < 1 {
		t.Error("should have at least 1 successful parse (panic should not block other requests)")
	}
}

// selectivePanicSpider 在第一次 Parse 调用时 panic，后续正常。
type selectivePanicSpider struct {
	spider.Base
	callCount    atomic.Int32
	successCount atomic.Int32
}

func (s *selectivePanicSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	n := s.callCount.Add(1)
	if n == 1 {
		panic("first call panic")
	}
	s.successCount.Add(1)
	return []spider.Output{
		{Item: map[string]any{"url": response.URL.String()}},
	}, nil
}
