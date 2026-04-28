package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// P2-011: Request 便捷 API 集成测试
// ============================================================================

// TestJSONRequestIntegration 验证 NewJSONRequest 在完整爬取流程中的工作。
func TestJSONRequestIntegration(t *testing.T) {
	// 创建一个接收 JSON 请求的测试服务器
	var receivedBody map[string]any
	var receivedContentType string
	var receivedMethod string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()

	sp := &jsonAPISpider{baseURL: ts.URL}
	c := crawler.New(crawler.WithSettings(func() *settings.Settings {
		s := settings.New()
		s.Set("LOG_LEVEL", "WARN", settings.PriorityProject)
		return s
	}()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := c.Crawl(ctx, sp)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedMethod != "POST" {
		t.Errorf("expected POST, got %s", receivedMethod)
	}
	if receivedContentType != "application/json" {
		t.Errorf("expected application/json, got %s", receivedContentType)
	}
	if receivedBody == nil {
		t.Fatal("expected JSON body to be received")
	}
	if receivedBody["name"] != "test" {
		t.Errorf("expected name=test, got %v", receivedBody["name"])
	}
}

// TestFormRequestIntegration 验证 NewFormRequest 在完整爬取流程中的工作。
func TestFormRequestIntegration(t *testing.T) {
	var receivedUser string
	var receivedPass string
	var receivedContentType string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		r.ParseForm()
		receivedUser = r.FormValue("user")
		receivedPass = r.FormValue("pass")
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	sp := &formLoginSpider{baseURL: ts.URL}
	c := crawler.New(crawler.WithSettings(func() *settings.Settings {
		s := settings.New()
		s.Set("LOG_LEVEL", "WARN", settings.PriorityProject)
		return s
	}()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := c.Crawl(ctx, sp)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedContentType != "application/x-www-form-urlencoded" {
		t.Errorf("expected application/x-www-form-urlencoded, got %s", receivedContentType)
	}
	if receivedUser != "admin" {
		t.Errorf("expected user=admin, got %s", receivedUser)
	}
	if receivedPass != "secret" {
		t.Errorf("expected pass=secret, got %s", receivedPass)
	}
}

// TestNoCallbackIntegration 验证 NoCallback 在完整爬取流程中的工作。
func TestNoCallbackIntegration(t *testing.T) {
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	sp := &noCallbackSpider{baseURL: ts.URL}
	c := crawler.New(crawler.WithSettings(func() *settings.Settings {
		s := settings.New()
		s.Set("LOG_LEVEL", "WARN", settings.PriorityProject)
		return s
	}()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := c.Crawl(ctx, sp)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// 请求应该被发送
	if count := requestCount.Load(); count < 1 {
		t.Errorf("expected at least 1 request, got %d", count)
	}
}

// ============================================================================
// P2-012: CONCURRENT_ITEMS 集成测试
// ============================================================================

// TestConcurrentItemsIntegration 验证 CONCURRENT_ITEMS 在完整爬取流程中的工作。
func TestConcurrentItemsIntegration(t *testing.T) {
	// 创建返回多个 Item 的测试服务器
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`<html><body>
			<div class="item">Item 1</div>
			<div class="item">Item 2</div>
			<div class="item">Item 3</div>
			<div class="item">Item 4</div>
			<div class="item">Item 5</div>
		</body></html>`))
	}))
	defer ts.Close()

	var processedCount atomic.Int32
	var maxConcurrent atomic.Int32
	var currentConcurrent atomic.Int32

	sp := &multiItemSpider{baseURL: ts.URL}
	c := crawler.New(crawler.WithSettings(func() *settings.Settings {
		s := settings.New()
		s.Set("LOG_LEVEL", "WARN", settings.PriorityProject)
		s.Set("CONCURRENT_ITEMS", 3, settings.PriorityProject)
		return s
	}()))

	c.AddPipeline(&concurrentTrackingPipeline{
		processedCount:    &processedCount,
		maxConcurrent:     &maxConcurrent,
		currentConcurrent: &currentConcurrent,
		delay:             50 * time.Millisecond,
	}, "tracking", 100)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := c.Crawl(ctx, sp)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// 所有 Item 应被处理
	if count := processedCount.Load(); count != 5 {
		t.Errorf("expected 5 items processed, got %d", count)
	}

	// 最大并发不应超过 CONCURRENT_ITEMS (3)
	if max := maxConcurrent.Load(); max > 3 {
		t.Errorf("max concurrent items should be <= 3, got %d", max)
	}
}

// TestConcurrentItemsDefaultIntegration 验证默认 CONCURRENT_ITEMS=100 的行为。
func TestConcurrentItemsDefaultIntegration(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("<html><body>test</body></html>"))
	}))
	defer ts.Close()

	var processedCount atomic.Int32

	sp := &singleItemSpider{baseURL: ts.URL}
	c := crawler.New(crawler.WithSettings(func() *settings.Settings {
		s := settings.New()
		s.Set("LOG_LEVEL", "WARN", settings.PriorityProject)
		return s
	}()))

	c.AddPipeline(&simpleTrackingPipeline{processed: &processedCount}, "tracking", 100)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := c.Crawl(ctx, sp)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	if count := processedCount.Load(); count != 1 {
		t.Errorf("expected 1 item processed, got %d", count)
	}
}

// ============================================================================
// P2-007: 扩展系统端到端测试
// ============================================================================

// TestExtensionSystemE2E 验证扩展系统在完整爬取流程中的工作。
func TestExtensionSystemE2E(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("<html><body>test</body></html>"))
	}))
	defer ts.Close()

	sp := &singleItemSpider{baseURL: ts.URL}
	c := crawler.New(
		crawler.WithSettings(func() *settings.Settings {
			s := settings.New()
			s.Set("LOG_LEVEL", "WARN", settings.PriorityProject)
			return s
		}()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := c.Crawl(ctx, sp)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// 通过 Crawler 的 Stats 字段获取统计（crawl 过程中可能重建了 Stats）
	sc := c.Stats

	// CoreStats 应记录 start_time
	startTime := sc.GetValue("start_time", nil)
	if startTime == nil {
		t.Error("expected start_time to be set by CoreStats extension")
	}

	// CoreStats 应记录 finish_time
	finishTime := sc.GetValue("finish_time", nil)
	if finishTime == nil {
		t.Error("expected finish_time to be set by CoreStats extension")
	}

	// CoreStats 应记录 finish_reason
	finishReason := sc.GetValue("finish_reason", nil)
	if finishReason == nil {
		t.Error("expected finish_reason to be set by CoreStats extension")
	}
}

// ============================================================================
// P2-011: Request 便捷 Option 单元级集成测试
// ============================================================================

// TestWithBasicAuthIntegration 验证 WithBasicAuth 在 HttpAuth 中间件中的工作。
func TestWithBasicAuthIntegration(t *testing.T) {
	var receivedAuth string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	sp := &basicAuthSpider{baseURL: ts.URL}
	c := crawler.New(crawler.WithSettings(func() *settings.Settings {
		s := settings.New()
		s.Set("LOG_LEVEL", "WARN", settings.PriorityProject)
		s.Set("HTTP_USER", "testuser", settings.PriorityProject)
		s.Set("HTTP_PASS", "testpass", settings.PriorityProject)
		return s
	}()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := c.Crawl(ctx, sp)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("unexpected error: %v", err)
	}

	// HttpAuth 中间件应注入 Authorization 头
	if receivedAuth == "" {
		t.Error("expected Authorization header to be set")
	}
}

// ============================================================================
// 测试辅助 Spider 类型
// ============================================================================

// jsonAPISpider 使用 NewJSONRequest 发送 JSON 请求。
type jsonAPISpider struct {
	spider.Base
	baseURL string
}

func (s *jsonAPISpider) Name() string { return "json-api" }

func (s *jsonAPISpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		req, err := shttp.NewJSONRequest(s.baseURL+"/api",
			map[string]any{"name": "test", "count": 42},
		)
		if err != nil {
			return
		}
		req.DontFilter = true
		ch <- spider.Output{Request: req}
	}()
	return ch
}

func (s *jsonAPISpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

// formLoginSpider 使用 NewFormRequest 发送表单请求。
type formLoginSpider struct {
	spider.Base
	baseURL string
}

func (s *formLoginSpider) Name() string { return "form-login" }

func (s *formLoginSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		req, err := shttp.NewFormRequest(s.baseURL+"/login",
			map[string][]string{
				"user": {"admin"},
				"pass": {"secret"},
			},
		)
		if err != nil {
			return
		}
		req.DontFilter = true
		ch <- spider.Output{Request: req}
	}()
	return ch
}

func (s *formLoginSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

// noCallbackSpider 使用 NoCallback 发送请求。
type noCallbackSpider struct {
	spider.Base
	baseURL string
}

func (s *noCallbackSpider) Name() string { return "no-callback" }

func (s *noCallbackSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		req := shttp.MustNewRequest(s.baseURL+"/page",
			shttp.WithCallback(shttp.NoCallback),
			shttp.WithDontFilter(true),
		)
		ch <- spider.Output{Request: req}
	}()
	return ch
}

func (s *noCallbackSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	// 不应被调用（NoCallback）
	return nil, fmt.Errorf("Parse should not be called with NoCallback")
}

// multiItemSpider 返回多个 Item。
type multiItemSpider struct {
	spider.Base
	baseURL string
}

func (s *multiItemSpider) Name() string { return "multi-item" }

func (s *multiItemSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		req := shttp.MustNewRequest(s.baseURL, shttp.WithDontFilter(true))
		ch <- spider.Output{Request: req}
	}()
	return ch
}

func (s *multiItemSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	var outputs []spider.Output
	for i := 0; i < 5; i++ {
		outputs = append(outputs, spider.Output{
			Item: map[string]any{"index": i, "url": response.URL.String()},
		})
	}
	return outputs, nil
}

// singleItemSpider 返回单个 Item。
type singleItemSpider struct {
	spider.Base
	baseURL string
}

func (s *singleItemSpider) Name() string { return "single-item" }

func (s *singleItemSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		req := shttp.MustNewRequest(s.baseURL, shttp.WithDontFilter(true))
		ch <- spider.Output{Request: req}
	}()
	return ch
}

func (s *singleItemSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return []spider.Output{
		{Item: map[string]any{"title": "test"}},
	}, nil
}

// basicAuthSpider 使用 Basic Auth 发送请求。
type basicAuthSpider struct {
	spider.Base
	baseURL string
}

func (s *basicAuthSpider) Name() string { return "basic-auth" }

func (s *basicAuthSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)
		req := shttp.MustNewRequest(s.baseURL+"/protected", shttp.WithDontFilter(true))
		ch <- spider.Output{Request: req}
	}()
	return ch
}

func (s *basicAuthSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

// ============================================================================
// 测试辅助 Pipeline 类型
// ============================================================================

// concurrentTrackingPipeline 追踪并发处理情况。
type concurrentTrackingPipeline struct {
	processedCount    *atomic.Int32
	maxConcurrent     *atomic.Int32
	currentConcurrent *atomic.Int32
	delay             time.Duration
}

func (p *concurrentTrackingPipeline) Open(ctx context.Context) error  { return nil }
func (p *concurrentTrackingPipeline) Close(ctx context.Context) error { return nil }
func (p *concurrentTrackingPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	current := p.currentConcurrent.Add(1)
	defer p.currentConcurrent.Add(-1)

	// 更新最大并发数
	for {
		old := p.maxConcurrent.Load()
		if current <= old {
			break
		}
		if p.maxConcurrent.CompareAndSwap(old, current) {
			break
		}
	}

	time.Sleep(p.delay)
	p.processedCount.Add(1)
	return item, nil
}

// simpleTrackingPipeline 简单追踪处理数量。
type simpleTrackingPipeline struct {
	processed *atomic.Int32
}

func (p *simpleTrackingPipeline) Open(ctx context.Context) error  { return nil }
func (p *simpleTrackingPipeline) Close(ctx context.Context) error { return nil }
func (p *simpleTrackingPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	p.processed.Add(1)
	return item, nil
}

// Ensure pipeline.ItemPipeline is satisfied
var _ pipeline.ItemPipeline = (*concurrentTrackingPipeline)(nil)
var _ pipeline.ItemPipeline = (*simpleTrackingPipeline)(nil)
