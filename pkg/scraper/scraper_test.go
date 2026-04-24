package scraper

import (
	"context"
	"errors"
	"testing"

	scrapy_errors "scrapy-go/pkg/errors"
	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/pipeline"
	"scrapy-go/pkg/spider"
	spider_mw "scrapy-go/pkg/spider/middleware"
	"scrapy-go/pkg/stats"
)

func TestScraperBasic(t *testing.T) {
	sp := &testSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	s := NewScraper(nil, nil, sp, nil, sc, nil, 0)
	s.Open(context.Background())
	defer s.Close(context.Background())

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithResponseBody([]byte("<html>Hello</html>")),
		scrapy_http.WithRequest(req),
	)

	newReqs, err := s.Scrape(context.Background(), resp, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// testSpider.Parse 返回 1 个 Item 和 1 个 Request
	if len(newReqs) != 1 {
		t.Errorf("expected 1 new request, got %d", len(newReqs))
	}
	if newReqs[0].URL.String() != "https://example.com/next" {
		t.Errorf("unexpected URL: %s", newReqs[0].URL.String())
	}
}

func TestScraperWithCallback(t *testing.T) {
	sp := &testSpider{}
	s := NewScraper(nil, nil, sp, nil, nil, nil, 0)
	s.Open(context.Background())
	defer s.Close(context.Background())

	// 设置自定义回调
	customCallback := spider.CallbackFunc(func(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
		return []spider.Output{
			{Item: map[string]any{"custom": true}},
		}, nil
	})

	req := scrapy_http.MustNewRequest("https://example.com",
		scrapy_http.WithCallback(customCallback),
	)
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
	)

	newReqs, err := s.Scrape(context.Background(), resp, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(newReqs) != 0 {
		t.Errorf("expected 0 new requests (only item), got %d", len(newReqs))
	}
}

func TestScraperCallbackError(t *testing.T) {
	sp := &errorSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	s := NewScraper(nil, nil, sp, nil, sc, nil, 0)
	s.Open(context.Background())
	defer s.Close(context.Background())

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
	)

	newReqs, err := s.Scrape(context.Background(), resp, req)
	if err != nil {
		t.Error("callback error should be handled, not propagated")
	}
	if len(newReqs) != 0 {
		t.Error("should have no new requests on error")
	}

	// 验证统计
	excCount := sc.GetValue("spider_exceptions/count", 0)
	if excCount != 1 {
		t.Errorf("expected spider_exceptions/count=1, got %v", excCount)
	}
}

func TestScraperCloseSpiderError(t *testing.T) {
	sp := &closeSpiderSpider{}
	s := NewScraper(nil, nil, sp, nil, nil, nil, 0)
	s.Open(context.Background())
	defer s.Close(context.Background())

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
	)

	_, err := s.Scrape(context.Background(), resp, req)
	if !errors.Is(err, scrapy_errors.ErrCloseSpider) {
		t.Errorf("expected ErrCloseSpider, got %v", err)
	}
}

func TestScraperWithSpiderMiddleware(t *testing.T) {
	sp := &testSpider{}
	mw := spider_mw.NewManager(nil)
	mw.AddMiddleware(&filterItemMW{}, "filter", 100)

	s := NewScraper(mw, nil, sp, nil, nil, nil, 0)
	s.Open(context.Background())
	defer s.Close(context.Background())

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithResponseBody([]byte("test")),
		scrapy_http.WithRequest(req),
	)

	newReqs, err := s.Scrape(context.Background(), resp, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// filterItemMW 过滤掉所有 Item，只保留 Request
	if len(newReqs) != 1 {
		t.Errorf("expected 1 request (items filtered), got %d", len(newReqs))
	}
}

func TestScraperWithPipeline(t *testing.T) {
	sp := &testSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	pm := pipeline.NewManager(nil, sc, nil)
	pm.AddPipeline(&countPipeline{}, "count", 100)

	s := NewScraper(nil, pm, sp, nil, sc, nil, 0)
	s.Open(context.Background())
	defer s.Close(context.Background())

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithResponseBody([]byte("test")),
		scrapy_http.WithRequest(req),
	)

	s.Scrape(context.Background(), resp, req)

	scraped := sc.GetValue("item_scraped_count", 0)
	if scraped != 1 {
		t.Errorf("expected item_scraped_count=1, got %v", scraped)
	}
}

func TestScraperNeedsBackout(t *testing.T) {
	sp := &testSpider{}
	s := NewScraper(nil, nil, sp, nil, nil, nil, 2048)

	if s.NeedsBackout() {
		t.Error("should not need backout initially")
	}
}

func TestScraperScrapeError(t *testing.T) {
	sp := &testSpider{}
	s := NewScraper(nil, nil, sp, nil, nil, nil, 0)
	s.Open(context.Background())
	defer s.Close(context.Background())

	req := scrapy_http.MustNewRequest("https://example.com")

	// 无 errback
	newReqs, err := s.ScrapeError(context.Background(), errors.New("download failed"), req)
	if err != nil {
		t.Error("should not propagate error")
	}
	if len(newReqs) != 0 {
		t.Error("should have no new requests")
	}
}

func TestScraperScrapeErrorWithErrback(t *testing.T) {
	sp := &testSpider{}
	s := NewScraper(nil, nil, sp, nil, nil, nil, 0)
	s.Open(context.Background())
	defer s.Close(context.Background())

	errbackCalled := false
	errback := spider.ErrbackFunc(func(ctx context.Context, err error, request *scrapy_http.Request) ([]spider.Output, error) {
		errbackCalled = true
		return []spider.Output{
			{Request: scrapy_http.MustNewRequest("https://example.com/retry")},
		}, nil
	})

	req := scrapy_http.MustNewRequest("https://example.com",
		scrapy_http.WithErrback(errback),
	)

	newReqs, err := s.ScrapeError(context.Background(), errors.New("download failed"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !errbackCalled {
		t.Error("errback should be called")
	}
	if len(newReqs) != 1 {
		t.Fatalf("expected 1 new request from errback, got %d", len(newReqs))
	}
}

// ============================================================================
// 测试辅助类型
// ============================================================================

type testSpider struct {
	spider.Base
}

func (s *testSpider) Name() string { return "test" }

func (s *testSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	return []spider.Output{
		{Item: map[string]any{"url": response.URL.String()}},
		{Request: scrapy_http.MustNewRequest("https://example.com/next")},
	}, nil
}

type errorSpider struct {
	spider.Base
}

func (s *errorSpider) Name() string { return "error" }

func (s *errorSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	return nil, errors.New("parse error")
}

type closeSpiderSpider struct {
	spider.Base
}

func (s *closeSpiderSpider) Name() string { return "close" }

func (s *closeSpiderSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	return nil, scrapy_errors.NewCloseSpiderError("item_count_exceeded")
}

// filterItemMW 过滤掉所有 Item，只保留 Request
type filterItemMW struct {
	spider_mw.BaseSpiderMiddleware
}

func (m *filterItemMW) ProcessOutput(ctx context.Context, response *scrapy_http.Response, result []spider.Output) ([]spider.Output, error) {
	var filtered []spider.Output
	for _, output := range result {
		if output.IsRequest() {
			filtered = append(filtered, output)
		}
	}
	return filtered, nil
}

type countPipeline struct{}

func (p *countPipeline) Open(ctx context.Context) error  { return nil }
func (p *countPipeline) Close(ctx context.Context) error { return nil }
func (p *countPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	return item, nil
}
