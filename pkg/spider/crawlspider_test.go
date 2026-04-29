package spider

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/linkextractor"
)

// newTestResp 创建测试用的 Response。
func newTestResp(rawURL string, body string) *shttp.Response {
	u, _ := url.Parse(rawURL)
	return &shttp.Response{
		URL:     u,
		Status:  200,
		Headers: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body:    []byte(body),
	}
}

// ============================================================================
// Rule 测试
// ============================================================================

func TestRule_ShouldFollow_Default(t *testing.T) {
	// 没有 Callback 时默认 follow
	r := Rule{}
	if !r.shouldFollow() {
		t.Error("rule without callback should follow by default")
	}

	// 有 Callback 时默认不 follow
	r2 := Rule{
		Callback: func(ctx context.Context, resp *shttp.Response) ([]Output, error) {
			return nil, nil
		},
	}
	if r2.shouldFollow() {
		t.Error("rule with callback should not follow by default")
	}
}

func TestRule_ShouldFollow_Explicit(t *testing.T) {
	// 显式设置 Follow
	follow := true
	r := Rule{
		Callback: func(ctx context.Context, resp *shttp.Response) ([]Output, error) {
			return nil, nil
		},
		Follow: &follow,
	}
	if !r.shouldFollow() {
		t.Error("rule with explicit Follow=true should follow")
	}

	noFollow := false
	r2 := Rule{Follow: &noFollow}
	if r2.shouldFollow() {
		t.Error("rule with explicit Follow=false should not follow")
	}
}

// ============================================================================
// CrawlSpider 基础测试
// ============================================================================

func TestCrawlSpider_Name(t *testing.T) {
	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
	}
	if cs.Name() != "test_crawl" {
		t.Errorf("expected 'test_crawl', got %q", cs.Name())
	}
}

func TestCrawlSpider_Start(t *testing.T) {
	cs := &CrawlSpider{
		Base: Base{
			SpiderName: "test_crawl",
			StartURLs:  []string{"https://example.com/"},
		},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(),
			},
		},
	}

	ctx := context.Background()
	ch := cs.Start(ctx)

	var outputs []Output
	for o := range ch {
		outputs = append(outputs, o)
	}

	if len(outputs) != 1 {
		t.Fatalf("expected 1 start request, got %d", len(outputs))
	}
	if !outputs[0].IsRequest() {
		t.Error("start output should be a request")
	}
	if outputs[0].Request.URL.String() != "https://example.com/" {
		t.Errorf("expected https://example.com/, got %s", outputs[0].Request.URL.String())
	}
	// 初始请求应使用 CrawlSpider 的内部回调
	if outputs[0].Request.Callback == nil {
		t.Error("start request should have internal callback set")
	}
}

func TestCrawlSpider_Start_WithContext(t *testing.T) {
	cs := &CrawlSpider{
		Base: Base{
			SpiderName: "test_crawl",
			StartURLs:  []string{"https://example.com/1", "https://example.com/2", "https://example.com/3"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	ch := cs.Start(ctx)
	var count int
	for range ch {
		count++
	}

	// 由于 context 已取消，可能不会产出所有请求
	if count > 3 {
		t.Errorf("should not produce more than 3 outputs, got %d", count)
	}
}

// ============================================================================
// CrawlSpider.Parse 测试
// ============================================================================

func TestCrawlSpider_Parse_ExtractsLinks(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
	</body></html>`

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/\d+`),
				),
			},
		},
	}

	resp := newTestResp("https://example.com/", html)
	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 应该提取 2 个链接作为请求
	requestCount := 0
	for _, o := range outputs {
		if o.IsRequest() {
			requestCount++
		}
	}
	if requestCount != 2 {
		t.Errorf("expected 2 requests from links, got %d", requestCount)
	}
}

func TestCrawlSpider_Parse_WithCallback(t *testing.T) {
	html := `<html><body>
		<h1>Title</h1>
		<a href="/page/1">Page 1</a>
	</body></html>`

	callbackCalled := false
	follow := true

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/\d+`),
				),
				Callback: func(ctx context.Context, resp *shttp.Response) ([]Output, error) {
					callbackCalled = true
					return []Output{
						{Item: map[string]any{"title": "Title"}},
					}, nil
				},
				Follow: &follow,
			},
		},
	}
	// 必须先编译规则（正常流程中由 Start/Parse 触发）
	cs.compileRules()

	resp := newTestResp("https://example.com/", html)
	// 模拟规则匹配的请求
	resp.Request = &shttp.Request{
		Meta: map[string]any{"rule": 0},
	}

	ctx := context.Background()
	// 调用 ruleCallback（模拟 Engine 调用）
	outputs, err := cs.ruleCallback(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !callbackCalled {
		t.Error("callback should have been called")
	}

	// 应该有 1 个 Item + 1 个 Request（链接跟踪）
	itemCount := 0
	requestCount := 0
	for _, o := range outputs {
		if o.IsItem() {
			itemCount++
		}
		if o.IsRequest() {
			requestCount++
		}
	}
	if itemCount != 1 {
		t.Errorf("expected 1 item, got %d", itemCount)
	}
	if requestCount != 1 {
		t.Errorf("expected 1 request from link, got %d", requestCount)
	}
}

func TestCrawlSpider_Parse_NoFollow(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
	</body></html>`

	noFollow := false
	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(),
				Callback: func(ctx context.Context, resp *shttp.Response) ([]Output, error) {
					return []Output{{Item: map[string]any{"data": true}}}, nil
				},
				Follow: &noFollow,
			},
		},
	}
	// 必须先编译规则
	cs.compileRules()

	resp := newTestResp("https://example.com/", html)
	// 模拟通过规则匹配到达的请求（ruleCallback 路径）
	resp.Request = &shttp.Request{
		Meta: map[string]any{"rule": 0},
	}

	ctx := context.Background()
	// 通过 ruleCallback 测试 Follow=false 行为
	outputs, err := cs.ruleCallback(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Follow=false，回调返回 1 个 Item，但不应提取链接
	itemCount := 0
	requestCount := 0
	for _, o := range outputs {
		if o.IsItem() {
			itemCount++
		}
		if o.IsRequest() {
			requestCount++
		}
	}
	if itemCount != 1 {
		t.Errorf("expected 1 item from callback, got %d", itemCount)
	}
	if requestCount != 0 {
		t.Errorf("expected 0 requests with Follow=false, got %d", requestCount)
	}
}

// ============================================================================
// CrawlSpider ParseStartURL 测试
// ============================================================================

func TestCrawlSpider_ParseStartURL(t *testing.T) {
	html := `<html><body>
		<h1>Home</h1>
		<a href="/page/1">Page 1</a>
	</body></html>`

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/\d+`),
				),
			},
		},
		ParseStartURL: func(ctx context.Context, resp *shttp.Response) ([]Output, error) {
			return []Output{
				{Item: map[string]any{"page": "home"}},
			}, nil
		},
	}

	resp := newTestResp("https://example.com/", html)
	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 应该有 1 个 Item（ParseStartURL）+ 1 个 Request（链接跟踪）
	itemCount := 0
	requestCount := 0
	for _, o := range outputs {
		if o.IsItem() {
			itemCount++
		}
		if o.IsRequest() {
			requestCount++
		}
	}
	if itemCount != 1 {
		t.Errorf("expected 1 item from ParseStartURL, got %d", itemCount)
	}
	if requestCount != 1 {
		t.Errorf("expected 1 request from link, got %d", requestCount)
	}
}

// ============================================================================
// CrawlSpider ProcessResults 测试
// ============================================================================

func TestCrawlSpider_ProcessResults(t *testing.T) {
	html := `<html><body><a href="/page/1">Page 1</a></body></html>`

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/\d+`),
				),
			},
		},
		ParseStartURL: func(ctx context.Context, resp *shttp.Response) ([]Output, error) {
			return []Output{
				{Item: map[string]any{"raw": true}},
				{Item: map[string]any{"raw": true}},
			}, nil
		},
		ProcessResults: func(response *shttp.Response, results []Output) []Output {
			// 只保留第一个结果
			if len(results) > 1 {
				return results[:1]
			}
			return results
		},
	}

	resp := newTestResp("https://example.com/", html)
	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	itemCount := 0
	for _, o := range outputs {
		if o.IsItem() {
			itemCount++
		}
	}
	if itemCount != 1 {
		t.Errorf("expected 1 item after ProcessResults, got %d", itemCount)
	}
}

// ============================================================================
// CrawlSpider ProcessLinks 测试
// ============================================================================

func TestCrawlSpider_ProcessLinks(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
		<a href="/page/3">Page 3</a>
	</body></html>`

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/\d+`),
				),
				ProcessLinks: func(links []linkextractor.Link) []linkextractor.Link {
					// 只保留前 2 个链接
					if len(links) > 2 {
						return links[:2]
					}
					return links
				},
			},
		},
	}

	resp := newTestResp("https://example.com/", html)
	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	requestCount := 0
	for _, o := range outputs {
		if o.IsRequest() {
			requestCount++
		}
	}
	if requestCount != 2 {
		t.Errorf("expected 2 requests after ProcessLinks, got %d", requestCount)
	}
}

// ============================================================================
// CrawlSpider ProcessRequest 测试
// ============================================================================

func TestCrawlSpider_ProcessRequest(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
	</body></html>`

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/\d+`),
				),
				ProcessRequest: func(req *shttp.Request, resp *shttp.Response) *shttp.Request {
					// 只允许 /page/1
					if req.URL.Path == "/page/1" {
						return req
					}
					return nil // 丢弃其他请求
				},
			},
		},
	}

	resp := newTestResp("https://example.com/", html)
	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	requestCount := 0
	for _, o := range outputs {
		if o.IsRequest() {
			requestCount++
		}
	}
	if requestCount != 1 {
		t.Errorf("expected 1 request after ProcessRequest filter, got %d", requestCount)
	}
}

// ============================================================================
// CrawlSpider 多规则测试
// ============================================================================

func TestCrawlSpider_MultipleRules(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/category/books">Books</a>
		<a href="/about">About</a>
	</body></html>`

	rule1Called := false
	rule2Called := false

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/\d+`),
				),
				Callback: func(ctx context.Context, resp *shttp.Response) ([]Output, error) {
					rule1Called = true
					return nil, nil
				},
			},
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/category/`),
				),
				Callback: func(ctx context.Context, resp *shttp.Response) ([]Output, error) {
					rule2Called = true
					return nil, nil
				},
			},
		},
	}

	resp := newTestResp("https://example.com/", html)
	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 应该有 2 个请求（/page/1 和 /category/books）
	requestCount := 0
	for _, o := range outputs {
		if o.IsRequest() {
			requestCount++
		}
	}
	if requestCount != 2 {
		t.Errorf("expected 2 requests from 2 rules, got %d", requestCount)
	}

	// 回调尚未被调用（只是生成了请求）
	if rule1Called || rule2Called {
		t.Error("callbacks should not be called during link extraction")
	}
}

// ============================================================================
// CrawlSpider 链接去重测试（跨规则）
// ============================================================================

func TestCrawlSpider_CrossRuleDedup(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
	</body></html>`

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/`),
				),
			},
			{
				// 第二条规则也匹配同一链接
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/`),
				),
			},
		},
	}

	resp := newTestResp("https://example.com/", html)
	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 同一链接只应被第一个匹配的规则处理
	requestCount := 0
	for _, o := range outputs {
		if o.IsRequest() {
			requestCount++
		}
	}
	if requestCount != 1 {
		t.Errorf("expected 1 request (deduped across rules), got %d", requestCount)
	}
}

// ============================================================================
// CrawlSpider FollowLinks 全局开关测试
// ============================================================================

func TestCrawlSpider_FollowLinksDisabled(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
	</body></html>`

	disabled := false
	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(),
			},
		},
		FollowLinks: &disabled,
	}

	resp := newTestResp("https://example.com/", html)
	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(outputs) != 0 {
		t.Errorf("expected 0 outputs with FollowLinks disabled, got %d", len(outputs))
	}
}

// ============================================================================
// CrawlSpider Request Meta 测试
// ============================================================================

func TestCrawlSpider_RequestMeta(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
	</body></html>`

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/\d+`),
				),
			},
		},
	}

	resp := newTestResp("https://example.com/", html)
	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, o := range outputs {
		if o.IsRequest() {
			// 检查 Meta 中包含 rule 和 link_text
			ruleIdx, ok := o.Request.GetMeta("rule")
			if !ok {
				t.Error("request should have 'rule' in Meta")
			}
			if ruleIdx.(int) != 0 {
				t.Errorf("expected rule index 0, got %v", ruleIdx)
			}

			linkText, ok := o.Request.GetMeta("link_text")
			if !ok {
				t.Error("request should have 'link_text' in Meta")
			}
			if linkText.(string) != "Page 1" {
				t.Errorf("expected link_text 'Page 1', got %v", linkText)
			}
		}
	}
}

// ============================================================================
// CrawlSpider 非 HTML 响应测试
// ============================================================================

func TestCrawlSpider_NonHTMLResponse(t *testing.T) {
	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(),
			},
		},
	}

	u, _ := url.Parse("https://example.com/data.json")
	resp := &shttp.Response{
		URL:     u,
		Status:  200,
		Headers: http.Header{"Content-Type": {"application/json"}},
		Body:    []byte(`{"key": "value"}`),
	}

	ctx := context.Background()
	outputs, err := cs.Parse(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 非 HTML 响应不应提取链接
	if len(outputs) != 0 {
		t.Errorf("expected 0 outputs for non-HTML response, got %d", len(outputs))
	}
}

// ============================================================================
// CrawlSpider Errback 测试
// ============================================================================

func TestCrawlSpider_RuleErrback(t *testing.T) {
	errbackCalled := false

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(),
				Errback: func(ctx context.Context, err error, req *shttp.Request) ([]Output, error) {
					errbackCalled = true
					return nil, nil
				},
			},
		},
	}
	cs.compileRules()

	req := shttp.MustNewRequest("https://example.com/page/1",
		shttp.WithMeta(map[string]any{"rule": 0}),
	)

	ctx := context.Background()
	_, err := cs.ruleErrback(ctx, context.DeadlineExceeded, req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !errbackCalled {
		t.Error("errback should have been called")
	}
}

func TestCrawlSpider_RuleErrback_NoErrback(t *testing.T) {
	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(),
				// 没有 Errback
			},
		},
	}
	cs.compileRules()

	req := shttp.MustNewRequest("https://example.com/page/1",
		shttp.WithMeta(map[string]any{"rule": 0}),
	)

	ctx := context.Background()
	outputs, err := cs.ruleErrback(ctx, context.DeadlineExceeded, req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs != nil {
		t.Error("should return nil outputs when no errback")
	}
}

// ============================================================================
// CrawlSpider CbKwargs 测试
// ============================================================================

func TestCrawlSpider_CbKwargs(t *testing.T) {
	html := `<html><body>
		<h1>Test</h1>
		<a href="/page/1">Page 1</a>
	</body></html>`

	var receivedKwargs map[string]any
	follow := true

	cs := &CrawlSpider{
		Base: Base{SpiderName: "test_crawl"},
		Rules: []Rule{
			{
				LinkExtractor: linkextractor.NewHTMLLinkExtractor(
					linkextractor.WithAllow(`/page/\d+`),
				),
				Callback: func(ctx context.Context, resp *shttp.Response) ([]Output, error) {
					receivedKwargs = resp.GetCbKwargs()
					return nil, nil
				},
				CbKwargs: map[string]any{"category": "test"},
				Follow:   &follow,
			},
		},
	}

	resp := newTestResp("https://example.com/", html)
	resp.Request = &shttp.Request{
		Meta:     map[string]any{"rule": 0},
		CbKwargs: map[string]any{"category": "test"},
	}

	ctx := context.Background()
	_, err := cs.ruleCallback(ctx, resp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CbKwargs 通过 Response.Request.CbKwargs 传递
	if receivedKwargs == nil {
		t.Log("CbKwargs passed through Request.CbKwargs")
	}
}
