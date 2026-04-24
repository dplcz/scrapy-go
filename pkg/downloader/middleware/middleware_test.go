package middleware

import (
	"context"
	"errors"
	"net/http"
	"testing"

	scrapy_errors "scrapy-go/pkg/errors"
	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/stats"
)

// ============================================================================
// Manager 测试
// ============================================================================

func TestManagerDownloadNormal(t *testing.T) {
	m := NewManager(nil)

	downloadFunc := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
	}

	req := scrapy_http.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

func TestManagerProcessRequestOrder(t *testing.T) {
	m := NewManager(nil)

	var order []string
	mw1 := &orderTrackingMW{name: "mw1", order: &order}
	mw2 := &orderTrackingMW{name: "mw2", order: &order}
	mw3 := &orderTrackingMW{name: "mw3", order: &order}

	m.AddMiddleware(mw1, "mw1", 100)
	m.AddMiddleware(mw2, "mw2", 200)
	m.AddMiddleware(mw3, "mw3", 300)

	downloadFunc := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		order = append(order, "download")
		return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
	}

	req := scrapy_http.MustNewRequest("https://example.com")
	m.Download(context.Background(), downloadFunc, req)

	// ProcessRequest: 正序 (100 → 200 → 300)
	// Download
	// ProcessResponse: 逆序 (300 → 200 → 100)
	expected := []string{
		"mw1:request", "mw2:request", "mw3:request",
		"download",
		"mw3:response", "mw2:response", "mw1:response",
	}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, exp := range expected {
		if order[i] != exp {
			t.Errorf("step %d: expected %s, got %s", i, exp, order[i])
		}
	}
}

func TestManagerProcessRequestShortCircuit(t *testing.T) {
	m := NewManager(nil)

	// mw1 直接返回响应（短路）
	m.AddMiddleware(&shortCircuitMW{}, "short", 100)

	downloadCalled := false
	downloadFunc := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		downloadCalled = true
		return nil, nil
	}

	req := scrapy_http.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if downloadCalled {
		t.Error("download should not be called when middleware short-circuits")
	}
	if resp.Status != 403 {
		t.Errorf("expected status 403, got %d", resp.Status)
	}
}

func TestManagerProcessException(t *testing.T) {
	m := NewManager(nil)

	// 添加一个将异常转换为响应的中间件
	m.AddMiddleware(&exceptionHandlerMW{}, "handler", 100)

	downloadFunc := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		return nil, scrapy_errors.ErrDownloadTimeout
	}

	req := scrapy_http.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 504 {
		t.Errorf("expected status 504, got %d", resp.Status)
	}
}

func TestManagerProcessExceptionUnhandled(t *testing.T) {
	m := NewManager(nil)

	downloadFunc := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		return nil, scrapy_errors.ErrDownloadTimeout
	}

	req := scrapy_http.MustNewRequest("https://example.com")
	_, err := m.Download(context.Background(), downloadFunc, req)
	if !errors.Is(err, scrapy_errors.ErrDownloadTimeout) {
		t.Errorf("expected ErrDownloadTimeout, got %v", err)
	}
}

func TestManagerCount(t *testing.T) {
	m := NewManager(nil)
	if m.Count() != 0 {
		t.Error("new manager should have 0 middlewares")
	}
	m.AddMiddleware(&BaseDownloaderMiddleware{}, "mw1", 100)
	m.AddMiddleware(&BaseDownloaderMiddleware{}, "mw2", 200)
	if m.Count() != 2 {
		t.Errorf("expected 2 middlewares, got %d", m.Count())
	}
}

// ============================================================================
// DefaultHeaders 测试
// ============================================================================

func TestDefaultHeadersMiddleware(t *testing.T) {
	headers := http.Header{
		"Accept":          {"text/html"},
		"Accept-Language": {"en"},
	}
	mw := NewDefaultHeadersMiddleware(headers)

	// 请求没有设置 Accept，应被设置
	req := scrapy_http.MustNewRequest("https://example.com")
	mw.ProcessRequest(context.Background(), req)

	if req.Headers.Get("Accept") != "text/html" {
		t.Errorf("expected Accept=text/html, got %s", req.Headers.Get("Accept"))
	}
	if req.Headers.Get("Accept-Language") != "en" {
		t.Errorf("expected Accept-Language=en, got %s", req.Headers.Get("Accept-Language"))
	}

	// 请求已设置 Accept，不应被覆盖
	req2 := scrapy_http.MustNewRequest("https://example.com",
		scrapy_http.WithHeader("Accept", "application/json"),
	)
	mw.ProcessRequest(context.Background(), req2)

	if req2.Headers.Get("Accept") != "application/json" {
		t.Errorf("existing header should not be overridden: %s", req2.Headers.Get("Accept"))
	}
}

// ============================================================================
// UserAgent 测试
// ============================================================================

func TestUserAgentMiddleware(t *testing.T) {
	mw := NewUserAgentMiddleware("scrapy-go/0.1")

	req := scrapy_http.MustNewRequest("https://example.com")
	mw.ProcessRequest(context.Background(), req)

	if req.Headers.Get("User-Agent") != "scrapy-go/0.1" {
		t.Errorf("expected User-Agent=scrapy-go/0.1, got %s", req.Headers.Get("User-Agent"))
	}

	// 已设置 User-Agent 不应被覆盖
	req2 := scrapy_http.MustNewRequest("https://example.com",
		scrapy_http.WithHeader("User-Agent", "custom-agent"),
	)
	mw.ProcessRequest(context.Background(), req2)

	if req2.Headers.Get("User-Agent") != "custom-agent" {
		t.Errorf("existing User-Agent should not be overridden: %s", req2.Headers.Get("User-Agent"))
	}
}

func TestUserAgentMiddlewareEmpty(t *testing.T) {
	mw := NewUserAgentMiddleware("")

	req := scrapy_http.MustNewRequest("https://example.com")
	mw.ProcessRequest(context.Background(), req)

	if req.Headers.Get("User-Agent") != "" {
		t.Error("empty user agent should not set header")
	}
}

// ============================================================================
// Retry 测试
// ============================================================================

func TestRetryMiddlewareHTTPCode(t *testing.T) {
	sc := stats.NewMemoryStatsCollector(false, nil)
	mw := NewRetryMiddleware(2, []int{500, 502, 503}, -1, sc, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 500,
		scrapy_http.WithRequest(req),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}

	// 检查是否创建了重试请求
	retryReq, ok := req.GetMeta("_retry_request")
	if !ok {
		t.Fatal("should have _retry_request in meta")
	}
	rr := retryReq.(*scrapy_http.Request)
	if rr.Priority != -1 {
		t.Errorf("retry request priority should be -1, got %d", rr.Priority)
	}
	if !rr.DontFilter {
		t.Error("retry request should have DontFilter=true")
	}

	// 验证统计
	retryCount := sc.GetValue("retry/count", 0)
	if retryCount != 1 {
		t.Errorf("expected retry/count=1, got %v", retryCount)
	}
}

func TestRetryMiddlewareNonRetryCode(t *testing.T) {
	mw := NewRetryMiddleware(2, []int{500, 502, 503}, -1, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 200 {
		t.Error("non-retry status should pass through")
	}

	_, hasRetry := req.GetMeta("_retry_request")
	if hasRetry {
		t.Error("should not have retry request for 200")
	}
}

func TestRetryMiddlewareMaxRetries(t *testing.T) {
	sc := stats.NewMemoryStatsCollector(false, nil)
	mw := NewRetryMiddleware(2, []int{500}, -1, sc, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	req.SetMeta("retry_times", 2) // 已重试 2 次

	resp := scrapy_http.MustNewResponse("https://example.com", 500,
		scrapy_http.WithRequest(req),
	)

	mw.ProcessResponse(context.Background(), req, resp)

	_, hasRetry := req.GetMeta("_retry_request")
	if hasRetry {
		t.Error("should not retry when max retries reached")
	}

	maxReached := sc.GetValue("retry/max_reached", 0)
	if maxReached != 1 {
		t.Errorf("expected retry/max_reached=1, got %v", maxReached)
	}
}

func TestRetryMiddlewareDontRetry(t *testing.T) {
	mw := NewRetryMiddleware(2, []int{500}, -1, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	req.SetMeta("dont_retry", true)

	resp := scrapy_http.MustNewResponse("https://example.com", 500,
		scrapy_http.WithRequest(req),
	)

	result, _ := mw.ProcessResponse(context.Background(), req, resp)
	if result.Status != 500 {
		t.Error("dont_retry should pass through")
	}
}

func TestRetryMiddlewareException(t *testing.T) {
	sc := stats.NewMemoryStatsCollector(false, nil)
	mw := NewRetryMiddleware(2, nil, -1, sc, nil)

	req := scrapy_http.MustNewRequest("https://example.com")

	resp, err := mw.ProcessException(context.Background(), req, scrapy_errors.ErrDownloadTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("should return response for retryable exception")
	}

	_, hasRetry := req.GetMeta("_retry_request")
	if !hasRetry {
		t.Error("should have retry request for retryable exception")
	}
}

// ============================================================================
// Redirect 测试
// ============================================================================

func TestRedirectMiddleware301(t *testing.T) {
	mw := NewRedirectMiddleware(20, 2, nil)

	req := scrapy_http.MustNewRequest("https://example.com/old")
	resp := scrapy_http.MustNewResponse("https://example.com/old", 301,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Location": {"https://example.com/new"},
		}),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}

	// 检查重定向请求
	redirectReq, ok := req.GetMeta("_redirect_request")
	if !ok {
		t.Fatal("should have _redirect_request in meta")
	}
	rr := redirectReq.(*scrapy_http.Request)
	if rr.URL.String() != "https://example.com/new" {
		t.Errorf("redirect URL should be /new, got %s", rr.URL.String())
	}
}

func TestRedirectMiddleware302POST(t *testing.T) {
	mw := NewRedirectMiddleware(20, 2, nil)

	req := scrapy_http.MustNewRequest("https://example.com/old",
		scrapy_http.WithMethod("POST"),
		scrapy_http.WithBody([]byte("data")),
	)
	resp := scrapy_http.MustNewResponse("https://example.com/old", 302,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Location": {"/new"},
		}),
	)

	mw.ProcessResponse(context.Background(), req, resp)

	redirectReq, ok := req.GetMeta("_redirect_request")
	if !ok {
		t.Fatal("should have redirect request")
	}
	rr := redirectReq.(*scrapy_http.Request)
	// 302 + POST → GET
	if rr.Method != "GET" {
		t.Errorf("302 POST should redirect as GET, got %s", rr.Method)
	}
	if len(rr.Body) != 0 {
		t.Error("body should be cleared on POST→GET redirect")
	}
}

func TestRedirectMiddleware307(t *testing.T) {
	mw := NewRedirectMiddleware(20, 2, nil)

	req := scrapy_http.MustNewRequest("https://example.com/old",
		scrapy_http.WithMethod("POST"),
		scrapy_http.WithBody([]byte("data")),
	)
	resp := scrapy_http.MustNewResponse("https://example.com/old", 307,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Location": {"/new"},
		}),
	)

	mw.ProcessResponse(context.Background(), req, resp)

	redirectReq, ok := req.GetMeta("_redirect_request")
	if !ok {
		t.Fatal("should have redirect request")
	}
	rr := redirectReq.(*scrapy_http.Request)
	// 307 保持原方法
	if rr.Method != "POST" {
		t.Errorf("307 should preserve method, got %s", rr.Method)
	}
}

func TestRedirectMiddlewareMaxRedirects(t *testing.T) {
	mw := NewRedirectMiddleware(2, 2, nil)

	req := scrapy_http.MustNewRequest("https://example.com/old")
	req.SetMeta("redirect_times", 2) // 已重定向 2 次

	resp := scrapy_http.MustNewResponse("https://example.com/old", 301,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Location": {"/new"},
		}),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, scrapy_errors.ErrIgnoreRequest) {
		t.Errorf("should return ErrIgnoreRequest when max redirects reached, got %v", err)
	}
}

func TestRedirectMiddlewareDontRedirect(t *testing.T) {
	mw := NewRedirectMiddleware(20, 2, nil)

	req := scrapy_http.MustNewRequest("https://example.com/old")
	req.SetMeta("dont_redirect", true)

	resp := scrapy_http.MustNewResponse("https://example.com/old", 301,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Location": {"/new"},
		}),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 301 {
		t.Error("dont_redirect should pass through")
	}
}

func TestRedirectMiddlewareNoLocation(t *testing.T) {
	mw := NewRedirectMiddleware(20, 2, nil)

	req := scrapy_http.MustNewRequest("https://example.com/old")
	resp := scrapy_http.MustNewResponse("https://example.com/old", 301,
		scrapy_http.WithRequest(req),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 301 {
		t.Error("no Location header should pass through")
	}
}

func TestRedirectMiddlewareNon3xx(t *testing.T) {
	mw := NewRedirectMiddleware(20, 2, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 200 {
		t.Error("non-redirect status should pass through")
	}
}

func TestRedirectMiddlewareCrossDomain(t *testing.T) {
	mw := NewRedirectMiddleware(20, 2, nil)

	req := scrapy_http.MustNewRequest("https://example.com/old",
		scrapy_http.WithHeader("Cookie", "session=abc"),
		scrapy_http.WithHeader("Authorization", "Bearer token"),
	)
	resp := scrapy_http.MustNewResponse("https://example.com/old", 301,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Location": {"https://other.com/new"},
		}),
	)

	mw.ProcessResponse(context.Background(), req, resp)

	redirectReq, ok := req.GetMeta("_redirect_request")
	if !ok {
		t.Fatal("should have redirect request")
	}
	rr := redirectReq.(*scrapy_http.Request)
	// 跨域应移除敏感头
	if rr.Headers.Get("Cookie") != "" {
		t.Error("Cookie should be removed on cross-domain redirect")
	}
	if rr.Headers.Get("Authorization") != "" {
		t.Error("Authorization should be removed on cross-domain redirect")
	}
}

// ============================================================================
// 接口实现验证
// ============================================================================

func TestInterfaceImplementations(t *testing.T) {
	var _ DownloaderMiddleware = (*BaseDownloaderMiddleware)(nil)
	var _ DownloaderMiddleware = (*DefaultHeadersMiddleware)(nil)
	var _ DownloaderMiddleware = (*UserAgentMiddleware)(nil)
	var _ DownloaderMiddleware = (*RetryMiddleware)(nil)
	var _ DownloaderMiddleware = (*RedirectMiddleware)(nil)
}

// ============================================================================
// 测试辅助类型
// ============================================================================

type orderTrackingMW struct {
	BaseDownloaderMiddleware
	name  string
	order *[]string
}

func (m *orderTrackingMW) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	*m.order = append(*m.order, m.name+":request")
	return nil, nil
}

func (m *orderTrackingMW) ProcessResponse(ctx context.Context, request *scrapy_http.Request, response *scrapy_http.Response) (*scrapy_http.Response, error) {
	*m.order = append(*m.order, m.name+":response")
	return response, nil
}

type shortCircuitMW struct {
	BaseDownloaderMiddleware
}

func (m *shortCircuitMW) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	return scrapy_http.MustNewResponse(request.URL.String(), 403), nil
}

type exceptionHandlerMW struct {
	BaseDownloaderMiddleware
}

func (m *exceptionHandlerMW) ProcessException(ctx context.Context, request *scrapy_http.Request, err error) (*scrapy_http.Response, error) {
	if errors.Is(err, scrapy_errors.ErrDownloadTimeout) {
		return scrapy_http.MustNewResponse(request.URL.String(), 504), nil
	}
	return nil, nil
}
