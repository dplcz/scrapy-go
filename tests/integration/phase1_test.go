// Package integration 提供 Phase 1 的端到端集成测试。
//
// 这些测试验证所有 Sprint 1-3 的功能在完整爬取流程中协同工作：
//   - 下载器中间件链（DownloadTimeout、DefaultHeaders、HttpAuth、UserAgent、
//     Retry、HttpCompression、Redirect、Cookies）
//   - HTML 选择器（CSS/XPath）
//   - Spider 回调和 Item Pipeline
//   - 并发控制和信号系统
package integration

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 测试辅助：本地 HTML 网站
// ============================================================================

func newTestHTMLSite() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Test Site</title></head>
<body>
<h1 class="title">Integration Test</h1>
<div class="item" data-id="1">
  <span class="name">Item One</span>
  <span class="value">100</span>
  <a href="/detail/1">详情</a>
</div>
<div class="item" data-id="2">
  <span class="name">Item Two</span>
  <span class="value">200</span>
  <a href="/detail/2">详情</a>
</div>
<nav><a class="next" href="/page/2">下一页</a></nav>
</body></html>`)
	})

	mux.HandleFunc("/page/2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Test Site - Page 2</title></head>
<body>
<h1 class="title">Integration Test - Page 2</h1>
<div class="item" data-id="3">
  <span class="name">Item Three</span>
  <span class="value">300</span>
</div>
</body></html>`)
	})

	mux.HandleFunc("/detail/1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><body>
<h1 class="detail-title">Item One Detail</h1>
<p class="description">This is the detailed description of Item One.</p>
</body></html>`)
	})

	mux.HandleFunc("/detail/2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><body>
<h1 class="detail-title">Item Two Detail</h1>
<p class="description">This is the detailed description of Item Two.</p>
</body></html>`)
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// 测试辅助：需要认证 + Cookie 的网站
// ============================================================================

func newTestAuthCookieSite() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.SetCookie(w, &http.Cookie{
			Name:  "session",
			Value: "test-session-123",
			Path:  "/",
		})
		fmt.Fprint(w, `<!DOCTYPE html>
<html><body>
<h1>Login Success</h1>
<a href="/dashboard">Go to Dashboard</a>
</body></html>`)
	})

	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		// 检查 Cookie
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value != "test-session-123" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `<html><body><h1>Unauthorized</h1></body></html>`)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><body>
<h1 class="dashboard-title">Dashboard</h1>
<div class="data">Secret Data: 42</div>
</body></html>`)
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// 测试辅助：Gzip 压缩网站
// ============================================================================

func newTestGzipSite() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 检查客户端是否接受 gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<html><body><h1>No Compression</h1></body></html>`)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Encoding", "gzip")

		gz := gzip.NewWriter(w)
		defer gz.Close()
		fmt.Fprint(gz, `<!DOCTYPE html>
<html><body>
<h1 class="compressed">Gzip Compressed Content</h1>
<div class="data">Compressed data value: 99</div>
</body></html>`)
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// 测试辅助：重定向网站
// ============================================================================

func newTestRedirectSite() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusMovedPermanently)
	})

	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><body>
<h1 class="redirected">Redirected Page</h1>
<div class="content">You were redirected here.</div>
</body></html>`)
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// 测试辅助：收集 Pipeline
// ============================================================================

type collectPipeline struct {
	mu    sync.Mutex
	items []any
}

func (p *collectPipeline) Open(ctx context.Context) error  { return nil }
func (p *collectPipeline) Close(ctx context.Context) error { return nil }
func (p *collectPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = append(p.items, item)
	return item, nil
}

func (p *collectPipeline) getItems() []any {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]any, len(p.items))
	copy(result, p.items)
	return result
}

// ============================================================================
// 测试 1：端到端 HTML 爬取 + CSS 选择器
// ============================================================================

// htmlSpider 使用 CSS 选择器解析 HTML 页面。
type htmlSpider struct {
	spider.Base
	mu    sync.Mutex
	items []map[string]string
}

func (s *htmlSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	var outputs []spider.Output

	// 使用 CSS 选择器提取数据
	items := response.CSS("div.item")
	for _, item := range items {
		name := item.CSS("span.name::text").Get("")
		value := item.CSS("span.value::text").Get("")
		detailURL := item.CSSAttr("a", "href").Get("")

		data := map[string]string{
			"name":  name,
			"value": value,
		}
		s.mu.Lock()
		s.items = append(s.items, data)
		s.mu.Unlock()
		outputs = append(outputs, spider.Output{Item: data})

		// 跟踪详情链接
		if detailURL != "" {
			absURL, err := response.URLJoin(detailURL)
			if err == nil {
				cb := spider.CallbackFunc(s.ParseDetail)
				req, _ := scrapy_http.NewRequest(absURL, scrapy_http.WithCallback(cb))
				outputs = append(outputs, spider.Output{Request: req})
			}
		}
	}

	// 提取下一页链接
	nextURL := response.CSSAttr("a.next", "href").Get("")
	if nextURL != "" {
		absURL, err := response.URLJoin(nextURL)
		if err == nil {
			req, _ := scrapy_http.NewRequest(absURL)
			outputs = append(outputs, spider.Output{Request: req})
		}
	}

	return outputs, nil
}

func (s *htmlSpider) ParseDetail(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	title := response.CSS("h1.detail-title::text").Get("")
	desc := response.CSS("p.description::text").Get("")

	item := map[string]string{
		"detail_title": title,
		"description":  desc,
	}
	s.mu.Lock()
	s.items = append(s.items, item)
	s.mu.Unlock()
	return []spider.Output{{Item: item}}, nil
}

func (s *htmlSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(4),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("WARN"),
	}
}

func TestEndToEndHTMLCrawl(t *testing.T) {
	site := newTestHTMLSite()
	defer site.Close()

	sp := &htmlSpider{
		Base: spider.Base{
			SpiderName: "html-test",
			StartURLs:  []string{site.URL + "/"},
		},
	}

	collector := &collectPipeline{}
	c := crawler.NewDefault()
	c.AddPipeline(collector, "Collect", 100)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	items := collector.getItems()
	// 应该有：2 个列表 Item（page 1）+ 1 个列表 Item（page 2）+ 2 个详情 Item
	if len(items) < 3 {
		t.Errorf("expected at least 3 items, got %d", len(items))
	}

	// 验证列表 Item
	sp.mu.Lock()
	defer sp.mu.Unlock()

	foundItemOne := false
	foundItemTwo := false
	foundItemThree := false
	foundDetail := false

	for _, item := range sp.items {
		if item["name"] == "Item One" && item["value"] == "100" {
			foundItemOne = true
		}
		if item["name"] == "Item Two" && item["value"] == "200" {
			foundItemTwo = true
		}
		if item["name"] == "Item Three" && item["value"] == "300" {
			foundItemThree = true
		}
		if item["detail_title"] != "" {
			foundDetail = true
		}
	}

	if !foundItemOne {
		t.Error("should find Item One")
	}
	if !foundItemTwo {
		t.Error("should find Item Two")
	}
	if !foundItemThree {
		t.Error("should find Item Three (from page 2)")
	}
	if !foundDetail {
		t.Error("should find at least one detail item")
	}
}

// ============================================================================
// 测试 2：XPath 选择器端到端
// ============================================================================

type xpathSpider struct {
	spider.Base
	mu    sync.Mutex
	items []map[string]string
}

func (s *xpathSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	var outputs []spider.Output

	// 使用 XPath 选择器提取数据
	items := response.XPath("//div[@class='item']")
	for _, item := range items {
		name := item.XPath(".//span[@class='name']/text()").Get("")
		value := item.XPath(".//span[@class='value']/text()").Get("")

		data := map[string]string{
			"name":  name,
			"value": value,
		}
		s.mu.Lock()
		s.items = append(s.items, data)
		s.mu.Unlock()
		outputs = append(outputs, spider.Output{Item: data})
	}

	return outputs, nil
}

func (s *xpathSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(2),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("WARN"),
	}
}

func TestEndToEndXPathCrawl(t *testing.T) {
	site := newTestHTMLSite()
	defer site.Close()

	sp := &xpathSpider{
		Base: spider.Base{
			SpiderName: "xpath-test",
			StartURLs:  []string{site.URL + "/"},
		},
	}

	c := crawler.NewDefault()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if len(sp.items) < 2 {
		t.Errorf("expected at least 2 items, got %d", len(sp.items))
	}

	foundItemOne := false
	for _, item := range sp.items {
		if item["name"] == "Item One" && item["value"] == "100" {
			foundItemOne = true
			break
		}
	}
	if !foundItemOne {
		t.Error("should find Item One via XPath")
	}
}

// ============================================================================
// 测试 3：Cookie 管理端到端
// ============================================================================

type cookieSpider struct {
	spider.Base
	mu            sync.Mutex
	dashboardData string
}

func (s *cookieSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	// 登录页面 → 服务器设置 Cookie → 跟踪到 dashboard
	nextURL := response.CSSAttr("a", "href").Get("")
	if nextURL != "" {
		absURL, err := response.URLJoin(nextURL)
		if err == nil {
			cb := spider.CallbackFunc(s.ParseDashboard)
			req, _ := scrapy_http.NewRequest(absURL, scrapy_http.WithCallback(cb))
			return []spider.Output{{Request: req}}, nil
		}
	}
	return nil, nil
}

func (s *cookieSpider) ParseDashboard(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	title := response.CSS("h1.dashboard-title::text").Get("")
	data := response.CSS("div.data::text").Get("")

	s.mu.Lock()
	s.dashboardData = title + " | " + data
	s.mu.Unlock()

	return []spider.Output{{Item: map[string]string{
		"title": title,
		"data":  data,
	}}}, nil
}

func (s *cookieSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(1),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("WARN"),
	}
}

func TestEndToEndCookieManagement(t *testing.T) {
	site := newTestAuthCookieSite()
	defer site.Close()

	sp := &cookieSpider{
		Base: spider.Base{
			SpiderName: "cookie-test",
			StartURLs:  []string{site.URL + "/login"},
		},
	}

	c := crawler.NewDefault()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Cookie 应该被自动携带到 dashboard 请求
	if sp.dashboardData == "" {
		t.Fatal("dashboard data should not be empty — Cookie management failed")
	}
	if !strings.Contains(sp.dashboardData, "Dashboard") {
		t.Errorf("expected dashboard title, got %q", sp.dashboardData)
	}
	if !strings.Contains(sp.dashboardData, "Secret Data: 42") {
		t.Errorf("expected secret data, got %q", sp.dashboardData)
	}
}

// ============================================================================
// 测试 4：Gzip 压缩端到端
// ============================================================================

type gzipSpider struct {
	spider.Base
	mu   sync.Mutex
	data string
}

func (s *gzipSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	title := response.CSS("h1.compressed::text").Get("")
	data := response.CSS("div.data::text").Get("")

	s.mu.Lock()
	s.data = title + " | " + data
	s.mu.Unlock()

	return []spider.Output{{Item: map[string]string{
		"title": title,
		"data":  data,
	}}}, nil
}

func (s *gzipSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(1),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("WARN"),
	}
}

func TestEndToEndGzipDecompression(t *testing.T) {
	site := newTestGzipSite()
	defer site.Close()

	sp := &gzipSpider{
		Base: spider.Base{
			SpiderName: "gzip-test",
			StartURLs:  []string{site.URL + "/"},
		},
	}

	c := crawler.NewDefault()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.data == "" {
		t.Fatal("gzip data should not be empty — decompression failed")
	}
	if !strings.Contains(sp.data, "Gzip Compressed Content") {
		t.Errorf("expected compressed content title, got %q", sp.data)
	}
	if !strings.Contains(sp.data, "Compressed data value: 99") {
		t.Errorf("expected compressed data value, got %q", sp.data)
	}
}

// ============================================================================
// 测试 5：重定向端到端
// ============================================================================

type redirectSpider struct {
	spider.Base
	mu      sync.Mutex
	content string
}

func (s *redirectSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	title := response.CSS("h1.redirected::text").Get("")
	content := response.CSS("div.content::text").Get("")

	s.mu.Lock()
	s.content = title + " | " + content
	s.mu.Unlock()

	return []spider.Output{{Item: map[string]string{
		"title":   title,
		"content": content,
	}}}, nil
}

func (s *redirectSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(1),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("WARN"),
	}
}

func TestEndToEndRedirect(t *testing.T) {
	site := newTestRedirectSite()
	defer site.Close()

	sp := &redirectSpider{
		Base: spider.Base{
			SpiderName: "redirect-test",
			StartURLs:  []string{site.URL + "/old"},
		},
	}

	c := crawler.NewDefault()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.content == "" {
		t.Fatal("redirect content should not be empty")
	}
	if !strings.Contains(sp.content, "Redirected Page") {
		t.Errorf("expected redirected page title, got %q", sp.content)
	}
}

// ============================================================================
// 测试 6：Pipeline 链端到端
// ============================================================================

// filterPipeline 过滤掉 value < 200 的 Item。
type filterPipeline struct{}

func (p *filterPipeline) Open(ctx context.Context) error  { return nil }
func (p *filterPipeline) Close(ctx context.Context) error { return nil }
func (p *filterPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	if m, ok := item.(map[string]string); ok {
		if m["value"] != "" && m["value"] < "200" {
			return nil, fmt.Errorf("drop item: value too low: %s", m["value"])
		}
	}
	return item, nil
}

func TestEndToEndPipelineChain(t *testing.T) {
	site := newTestHTMLSite()
	defer site.Close()

	sp := &htmlSpider{
		Base: spider.Base{
			SpiderName: "pipeline-test",
			StartURLs:  []string{site.URL + "/"},
		},
	}

	collector := &collectPipeline{}
	c := crawler.NewDefault()
	c.AddPipeline(&filterPipeline{}, "Filter", 100)
	c.AddPipeline(collector, "Collect", 200)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	items := collector.getItems()
	// 过滤后应该没有 value="100" 的 Item
	for _, item := range items {
		if m, ok := item.(map[string]string); ok {
			if m["value"] == "100" {
				t.Error("filter pipeline should have dropped item with value=100")
			}
		}
	}
}

// ============================================================================
// 测试 7：配置覆盖端到端
// ============================================================================

func TestEndToEndSettingsOverride(t *testing.T) {
	site := newTestHTMLSite()
	defer site.Close()

	sp := &htmlSpider{
		Base: spider.Base{
			SpiderName: "settings-test",
			StartURLs:  []string{site.URL + "/"},
		},
	}

	s := settings.New()
	s.Set("COOKIES_ENABLED", false, settings.PriorityProject)
	s.Set("COMPRESSION_ENABLED", false, settings.PriorityProject)

	c := crawler.New(crawler.WithSettings(s))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	// 验证爬取仍然成功（即使禁用了 Cookies 和 Compression）
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if len(sp.items) < 2 {
		t.Errorf("expected at least 2 items even with disabled middlewares, got %d", len(sp.items))
	}
}

// ============================================================================
// 测试 8：JSON API 端到端（验证 Response.JSON 与选择器共存）
// ============================================================================

func newTestJSONAPI() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]string{
				{"name": "Alpha", "value": "10"},
				{"name": "Beta", "value": "20"},
			},
			"next_page": "",
		})
	})
	return httptest.NewServer(mux)
}

type jsonSpider struct {
	spider.Base
	mu    sync.Mutex
	items []map[string]string
}

func (s *jsonSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	var data struct {
		Items    []map[string]string `json:"items"`
		NextPage string              `json:"next_page"`
	}
	if err := response.JSON(&data); err != nil {
		return nil, err
	}

	var outputs []spider.Output
	for _, item := range data.Items {
		s.mu.Lock()
		s.items = append(s.items, item)
		s.mu.Unlock()
		outputs = append(outputs, spider.Output{Item: item})
	}
	return outputs, nil
}

func (s *jsonSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(1),
		LogLevel:           spider.StringPtr("WARN"),
	}
}

func TestEndToEndJSONAPI(t *testing.T) {
	api := newTestJSONAPI()
	defer api.Close()

	sp := &jsonSpider{
		Base: spider.Base{
			SpiderName: "json-test",
			StartURLs:  []string{api.URL + "/api/data"},
		},
	}

	c := crawler.NewDefault()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Fatalf("crawl error: %v", err)
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if len(sp.items) != 2 {
		t.Errorf("expected 2 items, got %d", len(sp.items))
	}
}

// ============================================================================
// 测试辅助：验证 Pipeline 接口
// ============================================================================

func TestPipelineInterface(t *testing.T) {
	var _ pipeline.ItemPipeline = (*collectPipeline)(nil)
	var _ pipeline.ItemPipeline = (*filterPipeline)(nil)
}
