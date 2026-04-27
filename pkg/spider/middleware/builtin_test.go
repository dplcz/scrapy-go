package middleware

import (
	"context"
	"net/http"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// HttpError 测试
// ============================================================================

func TestHttpErrorMiddleware_Allow2xx(t *testing.T) {
	mw := NewHttpErrorMiddleware(false, nil, nil, nil)

	for _, status := range []int{200, 201, 204, 299} {
		resp := shttp.MustNewResponse("https://example.com", status)
		if err := mw.ProcessSpiderInput(context.Background(), resp); err != nil {
			t.Errorf("status %d should be allowed, got error: %v", status, err)
		}
	}
}

func TestHttpErrorMiddleware_BlockNon2xx(t *testing.T) {
	mw := NewHttpErrorMiddleware(false, nil, nil, nil)

	for _, status := range []int{301, 404, 500, 503} {
		resp := shttp.MustNewResponse("https://example.com", status)
		resp.Request = shttp.MustNewRequest("https://example.com")
		if err := mw.ProcessSpiderInput(context.Background(), resp); err == nil {
			t.Errorf("status %d should be blocked", status)
		}
	}
}

func TestHttpErrorMiddleware_AllowAll(t *testing.T) {
	mw := NewHttpErrorMiddleware(true, nil, nil, nil)

	resp := shttp.MustNewResponse("https://example.com", 404)
	resp.Request = shttp.MustNewRequest("https://example.com")
	if err := mw.ProcessSpiderInput(context.Background(), resp); err != nil {
		t.Errorf("allowAll should allow 404, got error: %v", err)
	}
}

func TestHttpErrorMiddleware_AllowCodes(t *testing.T) {
	mw := NewHttpErrorMiddleware(false, []int{404, 500}, nil, nil)

	resp404 := shttp.MustNewResponse("https://example.com", 404)
	resp404.Request = shttp.MustNewRequest("https://example.com")
	if err := mw.ProcessSpiderInput(context.Background(), resp404); err != nil {
		t.Errorf("404 should be allowed, got error: %v", err)
	}

	resp503 := shttp.MustNewResponse("https://example.com", 503)
	resp503.Request = shttp.MustNewRequest("https://example.com")
	if err := mw.ProcessSpiderInput(context.Background(), resp503); err == nil {
		t.Error("503 should be blocked")
	}
}

func TestHttpErrorMiddleware_MetaHandleAll(t *testing.T) {
	mw := NewHttpErrorMiddleware(false, nil, nil, nil)

	req := shttp.MustNewRequest("https://example.com",
		shttp.WithMeta(map[string]any{"handle_httpstatus_all": true}),
	)
	resp := shttp.MustNewResponse("https://example.com", 500)
	resp.Request = req

	if err := mw.ProcessSpiderInput(context.Background(), resp); err != nil {
		t.Errorf("handle_httpstatus_all should allow 500, got error: %v", err)
	}
}

func TestHttpErrorMiddleware_MetaHandleList(t *testing.T) {
	mw := NewHttpErrorMiddleware(false, nil, nil, nil)

	req := shttp.MustNewRequest("https://example.com",
		shttp.WithMeta(map[string]any{"handle_httpstatus_list": []int{403, 404}}),
	)
	resp := shttp.MustNewResponse("https://example.com", 404)
	resp.Request = req

	if err := mw.ProcessSpiderInput(context.Background(), resp); err != nil {
		t.Errorf("handle_httpstatus_list should allow 404, got error: %v", err)
	}

	// 500 不在列表中，应该被阻止
	resp500 := shttp.MustNewResponse("https://example.com", 500)
	resp500.Request = req
	if err := mw.ProcessSpiderInput(context.Background(), resp500); err == nil {
		t.Error("500 should be blocked by handle_httpstatus_list")
	}
}

func TestHttpErrorMiddleware_ProcessSpiderException(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewHttpErrorMiddleware(false, nil, sc, nil)

	resp := shttp.MustNewResponse("https://example.com", 404)
	httpErr := newHttpError(404)

	outputs, err := mw.ProcessSpiderException(context.Background(), resp, httpErr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs == nil {
		t.Fatal("expected non-nil outputs")
	}
	if len(outputs) != 0 {
		t.Errorf("expected empty outputs, got %d", len(outputs))
	}

	if sc.GetValue("httperror/response_ignored_count", 0) != 1 {
		t.Error("expected httperror/response_ignored_count=1")
	}
	if sc.GetValue("httperror/response_ignored_status_count/404", 0) != 1 {
		t.Error("expected httperror/response_ignored_status_count/404=1")
	}
}

// ============================================================================
// UrlLength 测试
// ============================================================================

func TestUrlLengthMiddleware_AllowShortURL(t *testing.T) {
	mw := NewUrlLengthMiddleware(100, nil, nil)

	req := shttp.MustNewRequest("https://example.com/short")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Errorf("expected 1 output, got %d", len(outputs))
	}
}

func TestUrlLengthMiddleware_FilterLongURL(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewUrlLengthMiddleware(30, sc, nil)

	req := shttp.MustNewRequest("https://example.com/very/long/path/that/exceeds/limit")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 0 {
		t.Errorf("expected 0 outputs (filtered), got %d", len(outputs))
	}
	if sc.GetValue("urllength/request_ignored_count", 0) != 1 {
		t.Error("expected urllength/request_ignored_count=1")
	}
}

func TestUrlLengthMiddleware_PassItems(t *testing.T) {
	mw := NewUrlLengthMiddleware(10, nil, nil)

	result := []spider.Output{
		{Item: map[string]any{"title": "test"}},
	}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Errorf("items should not be filtered, got %d outputs", len(outputs))
	}
}

func TestUrlLengthMiddleware_Disabled(t *testing.T) {
	mw := NewUrlLengthMiddleware(0, nil, nil)

	req := shttp.MustNewRequest("https://example.com/very/long/path")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Error("disabled middleware should pass all requests")
	}
}

// ============================================================================
// Depth 测试
// ============================================================================

func TestDepthMiddleware_InitDepth(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewDepthMiddleware(0, 0, true, sc, nil)

	req := shttp.MustNewRequest("https://example.com")
	resp := shttp.MustNewResponse("https://example.com", 200, shttp.WithRequest(req))

	childReq := shttp.MustNewRequest("https://example.com/page2")
	result := []spider.Output{{Request: childReq}}

	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}

	// 父响应应该被初始化为 depth=0
	depth, ok := req.GetMeta("depth")
	if !ok {
		t.Fatal("expected depth meta on parent request")
	}
	if depth != 0 {
		t.Errorf("expected parent depth=0, got %v", depth)
	}

	// 子请求应该是 depth=1
	childDepth, ok := outputs[0].Request.GetMeta("depth")
	if !ok {
		t.Fatal("expected depth meta on child request")
	}
	if childDepth != 1 {
		t.Errorf("expected child depth=1, got %v", childDepth)
	}
}

func TestDepthMiddleware_MaxDepth(t *testing.T) {
	mw := NewDepthMiddleware(2, 0, false, nil, nil)

	// 模拟 depth=2 的响应
	req := shttp.MustNewRequest("https://example.com",
		shttp.WithMeta(map[string]any{"depth": 2}),
	)
	resp := shttp.MustNewResponse("https://example.com", 200, shttp.WithRequest(req))

	childReq := shttp.MustNewRequest("https://example.com/page3")
	result := []spider.Output{{Request: childReq}}

	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 0 {
		t.Errorf("expected 0 outputs (depth exceeded), got %d", len(outputs))
	}
}

func TestDepthMiddleware_Priority(t *testing.T) {
	mw := NewDepthMiddleware(0, 1, false, nil, nil)

	req := shttp.MustNewRequest("https://example.com",
		shttp.WithMeta(map[string]any{"depth": 0}),
	)
	resp := shttp.MustNewResponse("https://example.com", 200, shttp.WithRequest(req))

	childReq := shttp.MustNewRequest("https://example.com/page2",
		shttp.WithPriority(10),
	)
	result := []spider.Output{{Request: childReq}}

	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}

	// depth=1, priority=1, 所以优先级应该减少 1*1=1
	if outputs[0].Request.Priority != 9 {
		t.Errorf("expected priority=9, got %d", outputs[0].Request.Priority)
	}
}

func TestDepthMiddleware_VerboseStats(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewDepthMiddleware(0, 0, true, sc, nil)

	req := shttp.MustNewRequest("https://example.com",
		shttp.WithMeta(map[string]any{"depth": 0}),
	)
	resp := shttp.MustNewResponse("https://example.com", 200, shttp.WithRequest(req))

	childReq := shttp.MustNewRequest("https://example.com/page2")
	result := []spider.Output{{Request: childReq}}

	mw.ProcessOutput(context.Background(), resp, result)

	if sc.GetValue("request_depth_count/1", 0) != 1 {
		t.Error("expected request_depth_count/1=1")
	}
}

func TestDepthMiddleware_MaxDepthStat(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewDepthMiddleware(10, 0, false, sc, nil)

	req := shttp.MustNewRequest("https://example.com",
		shttp.WithMeta(map[string]any{"depth": 3}),
	)
	resp := shttp.MustNewResponse("https://example.com", 200, shttp.WithRequest(req))

	childReq := shttp.MustNewRequest("https://example.com/page")
	result := []spider.Output{{Request: childReq}}

	mw.ProcessOutput(context.Background(), resp, result)

	maxDepth := sc.GetValue("request_depth_max", 0)
	if maxDepth != 4 {
		t.Errorf("expected request_depth_max=4, got %v", maxDepth)
	}
}

// ============================================================================
// Offsite 测试
// ============================================================================

func TestOffsiteMiddleware_AllowSameDomain(t *testing.T) {
	mw := NewOffsiteMiddleware([]string{"example.com"}, nil, nil)

	req := shttp.MustNewRequest("https://example.com/page")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Error("same domain request should be allowed")
	}
}

func TestOffsiteMiddleware_AllowSubdomain(t *testing.T) {
	mw := NewOffsiteMiddleware([]string{"example.com"}, nil, nil)

	req := shttp.MustNewRequest("https://www.example.com/page")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Error("subdomain request should be allowed")
	}
}

func TestOffsiteMiddleware_FilterOffsite(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewOffsiteMiddleware([]string{"example.com"}, sc, nil)

	req := shttp.MustNewRequest("https://other.com/page")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 0 {
		t.Error("offsite request should be filtered")
	}
	if sc.GetValue("offsite/filtered", 0) != 1 {
		t.Error("expected offsite/filtered=1")
	}
}

func TestOffsiteMiddleware_DontFilter(t *testing.T) {
	mw := NewOffsiteMiddleware([]string{"example.com"}, nil, nil)

	req := shttp.MustNewRequest("https://other.com/page",
		shttp.WithDontFilter(true),
	)
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Error("DontFilter request should be allowed")
	}
}

func TestOffsiteMiddleware_MetaAllowOffsite(t *testing.T) {
	mw := NewOffsiteMiddleware([]string{"example.com"}, nil, nil)

	req := shttp.MustNewRequest("https://other.com/page",
		shttp.WithMeta(map[string]any{"allow_offsite": true}),
	)
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Error("allow_offsite request should be allowed")
	}
}

func TestOffsiteMiddleware_NoDomains(t *testing.T) {
	mw := NewOffsiteMiddleware(nil, nil, nil)

	req := shttp.MustNewRequest("https://any-domain.com/page")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Error("no domains configured should allow all")
	}
}

func TestOffsiteMiddleware_MultipleDomains(t *testing.T) {
	mw := NewOffsiteMiddleware([]string{"example.com", "test.org"}, nil, nil)

	req1 := shttp.MustNewRequest("https://example.com/page")
	req2 := shttp.MustNewRequest("https://test.org/page")
	req3 := shttp.MustNewRequest("https://other.com/page")
	result := []spider.Output{
		{Request: req1},
		{Request: req2},
		{Request: req3},
	}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 2 {
		t.Errorf("expected 2 outputs (example.com + test.org), got %d", len(outputs))
	}
}

func TestOffsiteMiddleware_PassItems(t *testing.T) {
	mw := NewOffsiteMiddleware([]string{"example.com"}, nil, nil)

	result := []spider.Output{
		{Item: map[string]any{"title": "test"}},
	}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Error("items should not be filtered")
	}
}

// ============================================================================
// Referer 测试
// ============================================================================

func TestRefererMiddleware_SetReferer(t *testing.T) {
	mw := NewRefererMiddleware()

	req := shttp.MustNewRequest("https://example.com/page2")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com/page1", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}

	referer := outputs[0].Request.Headers.Get("Referer")
	if referer != "https://example.com/page1" {
		t.Errorf("expected Referer=https://example.com/page1, got %s", referer)
	}
}

func TestRefererMiddleware_NoRefererOnDowngrade(t *testing.T) {
	mw := NewRefererMiddleware()

	req := shttp.MustNewRequest("http://example.com/page2")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com/page1", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	referer := outputs[0].Request.Headers.Get("Referer")
	if referer != "" {
		t.Errorf("expected no Referer on HTTPS→HTTP downgrade, got %s", referer)
	}
}

func TestRefererMiddleware_HttpToHttp(t *testing.T) {
	mw := NewRefererMiddleware()

	req := shttp.MustNewRequest("http://example.com/page2")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("http://example.com/page1", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	referer := outputs[0].Request.Headers.Get("Referer")
	if referer != "http://example.com/page1" {
		t.Errorf("expected Referer=http://example.com/page1, got %s", referer)
	}
}

func TestRefererMiddleware_StripFragment(t *testing.T) {
	mw := NewRefererMiddleware()

	req := shttp.MustNewRequest("https://example.com/page2")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com/page1#section", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	referer := outputs[0].Request.Headers.Get("Referer")
	if referer != "https://example.com/page1" {
		t.Errorf("expected fragment stripped, got %s", referer)
	}
}

func TestRefererMiddleware_DontOverrideExisting(t *testing.T) {
	mw := NewRefererMiddleware()

	req := shttp.MustNewRequest("https://example.com/page2",
		shttp.WithHeaders(http.Header{"Referer": {"https://custom.com"}}),
	)
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("https://example.com/page1", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	referer := outputs[0].Request.Headers.Get("Referer")
	if referer != "https://custom.com" {
		t.Errorf("existing Referer should not be overridden, got %s", referer)
	}
}

func TestRefererMiddleware_LocalScheme(t *testing.T) {
	mw := NewRefererMiddleware()

	req := shttp.MustNewRequest("https://example.com/page2")
	result := []spider.Output{{Request: req}}

	resp := shttp.MustNewResponse("file:///tmp/test.html", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	referer := outputs[0].Request.Headers.Get("Referer")
	if referer != "" {
		t.Errorf("expected no Referer for file:// scheme, got %s", referer)
	}
}

func TestRefererMiddleware_PassItems(t *testing.T) {
	mw := NewRefererMiddleware()

	result := []spider.Output{
		{Item: map[string]any{"title": "test"}},
	}

	resp := shttp.MustNewResponse("https://example.com", 200)
	outputs, err := mw.ProcessOutput(context.Background(), resp, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Error("items should not be affected")
	}
}

// ============================================================================
// 辅助函数测试
// ============================================================================

func TestIntSliceContains(t *testing.T) {
	if !intSliceContains([]int{1, 2, 3}, 2) {
		t.Error("should contain 2")
	}
	if intSliceContains([]int{1, 2, 3}, 4) {
		t.Error("should not contain 4")
	}
	if intSliceContains(nil, 1) {
		t.Error("nil slice should not contain anything")
	}
}

func TestIsLocalScheme(t *testing.T) {
	localSchemes := []string{"about", "blob", "data", "filesystem", "file", "s3"}
	for _, s := range localSchemes {
		if !isLocalScheme(s) {
			t.Errorf("%s should be local scheme", s)
		}
	}
	if isLocalScheme("http") {
		t.Error("http should not be local scheme")
	}
	if isLocalScheme("https") {
		t.Error("https should not be local scheme")
	}
}

func TestIsTLSScheme(t *testing.T) {
	if !isTLSScheme("https") {
		t.Error("https should be TLS scheme")
	}
	if !isTLSScheme("ftps") {
		t.Error("ftps should be TLS scheme")
	}
	if isTLSScheme("http") {
		t.Error("http should not be TLS scheme")
	}
}

func TestBuildHostRegex(t *testing.T) {
	mw := &OffsiteMiddleware{logger: nil}
	mw.logger = nil

	// 空域名列表
	regex := mw.buildHostRegex(nil)
	if regex != nil {
		t.Error("nil domains should return nil regex")
	}

	// 正常域名
	regex = mw.buildHostRegex([]string{"example.com"})
	if regex == nil {
		t.Fatal("expected non-nil regex")
	}
	if !regex.MatchString("example.com") {
		t.Error("should match example.com")
	}
	if !regex.MatchString("www.example.com") {
		t.Error("should match www.example.com")
	}
	if regex.MatchString("other.com") {
		t.Error("should not match other.com")
	}
}
