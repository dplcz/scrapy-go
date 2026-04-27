package middleware

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	scrapy_errors "github.com/dplcz/scrapy-go/pkg/errors"
	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

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
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRetryMiddleware(2, []int{500, 502, 503}, -1, sc, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 500,
		scrapy_http.WithRequest(req),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)

	// 应返回 NewRequestError
	if !errors.Is(err, scrapy_errors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *scrapy_errors.NewRequestError
	if !errors.As(err, &newReqErr) {
		t.Fatal("should be able to extract NewRequestError")
	}

	rr, ok := newReqErr.Request.(*scrapy_http.Request)
	if !ok {
		t.Fatal("NewRequestError.Request should be *http.Request")
	}
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
}

func TestRetryMiddlewareMaxRetries(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRetryMiddleware(2, []int{500}, -1, sc, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	req.SetMeta("retry_times", 2) // 已重试 2 次

	resp := scrapy_http.MustNewResponse("https://example.com", 500,
		scrapy_http.WithRequest(req),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	// 达到最大重试次数，应正常返回响应（不重试）
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 500 {
		t.Errorf("expected status 500 when max retries reached, got %d", result.Status)
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

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != 500 {
		t.Error("dont_retry should pass through")
	}
}

func TestRetryMiddlewareException(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRetryMiddleware(2, nil, -1, sc, nil)

	req := scrapy_http.MustNewRequest("https://example.com")

	_, err := mw.ProcessException(context.Background(), req, scrapy_errors.ErrDownloadTimeout)

	// 应返回 NewRequestError
	if !errors.Is(err, scrapy_errors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest for retryable exception, got %v", err)
	}

	var newReqErr *scrapy_errors.NewRequestError
	if !errors.As(err, &newReqErr) {
		t.Fatal("should be able to extract NewRequestError")
	}

	rr, ok := newReqErr.Request.(*scrapy_http.Request)
	if !ok {
		t.Fatal("NewRequestError.Request should be *http.Request")
	}
	if !rr.DontFilter {
		t.Error("retry request should have DontFilter=true")
	}
}

func TestRetryMiddlewareExceptionNonRetryable(t *testing.T) {
	mw := NewRetryMiddleware(2, nil, -1, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com")

	resp, err := mw.ProcessException(context.Background(), req, errors.New("some random error"))
	// 非可重试异常，应返回 nil, nil（继续传播）
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Error("non-retryable exception should return nil response")
	}
}

func TestRetryMiddlewareRequestLevelMaxRetries(t *testing.T) {
	mw := NewRetryMiddleware(2, []int{500}, -1, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	req.SetMeta("max_retry_times", 5) // 请求级覆盖

	resp := scrapy_http.MustNewResponse("https://example.com", 500,
		scrapy_http.WithRequest(req),
	)

	// 第一次重试应成功
	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, scrapy_errors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
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

	_, err := mw.ProcessResponse(context.Background(), req, resp)

	// 应返回 NewRequestError
	if !errors.Is(err, scrapy_errors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *scrapy_errors.NewRequestError
	if !errors.As(err, &newReqErr) {
		t.Fatal("should be able to extract NewRequestError")
	}

	rr, ok := newReqErr.Request.(*scrapy_http.Request)
	if !ok {
		t.Fatal("NewRequestError.Request should be *http.Request")
	}
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

	_, err := mw.ProcessResponse(context.Background(), req, resp)

	if !errors.Is(err, scrapy_errors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *scrapy_errors.NewRequestError
	errors.As(err, &newReqErr)
	rr := newReqErr.Request.(*scrapy_http.Request)

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

	_, err := mw.ProcessResponse(context.Background(), req, resp)

	if !errors.Is(err, scrapy_errors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *scrapy_errors.NewRequestError
	errors.As(err, &newReqErr)
	rr := newReqErr.Request.(*scrapy_http.Request)

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

	_, err := mw.ProcessResponse(context.Background(), req, resp)

	if !errors.Is(err, scrapy_errors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *scrapy_errors.NewRequestError
	errors.As(err, &newReqErr)
	rr := newReqErr.Request.(*scrapy_http.Request)

	// 跨域应移除敏感头
	if rr.Headers.Get("Cookie") != "" {
		t.Error("Cookie should be removed on cross-domain redirect")
	}
	if rr.Headers.Get("Authorization") != "" {
		t.Error("Authorization should be removed on cross-domain redirect")
	}
}

func TestRedirectMiddlewareRedirectHistory(t *testing.T) {
	mw := NewRedirectMiddleware(20, 2, nil)

	req := scrapy_http.MustNewRequest("https://example.com/page1")
	resp := scrapy_http.MustNewResponse("https://example.com/page1", 301,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Location": {"https://example.com/page2"},
		}),
	)

	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if !errors.Is(err, scrapy_errors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *scrapy_errors.NewRequestError
	errors.As(err, &newReqErr)
	rr := newReqErr.Request.(*scrapy_http.Request)

	// 检查重定向历史
	redirectURLs, ok := rr.GetMeta("redirect_urls")
	if !ok {
		t.Fatal("should have redirect_urls in meta")
	}
	urls := redirectURLs.([]string)
	if len(urls) != 1 || urls[0] != "https://example.com/page1" {
		t.Errorf("unexpected redirect_urls: %v", urls)
	}

	redirectReasons, ok := rr.GetMeta("redirect_reasons")
	if !ok {
		t.Fatal("should have redirect_reasons in meta")
	}
	reasons := redirectReasons.([]any)
	if len(reasons) != 1 || reasons[0] != 301 {
		t.Errorf("unexpected redirect_reasons: %v", reasons)
	}
}

// ============================================================================
// DownloadTimeout 测试
// ============================================================================

func TestDownloadTimeoutMiddleware(t *testing.T) {
	mw := NewDownloadTimeoutMiddleware(30*time.Second, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	mw.ProcessRequest(context.Background(), req)

	v, ok := req.GetMeta("download_timeout")
	if !ok {
		t.Fatal("should set download_timeout in meta")
	}
	timeout, ok := v.(time.Duration)
	if !ok {
		t.Fatal("download_timeout should be time.Duration")
	}
	if timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", timeout)
	}
}

func TestDownloadTimeoutMiddlewareNoOverride(t *testing.T) {
	mw := NewDownloadTimeoutMiddleware(30*time.Second, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	req.SetMeta("download_timeout", 10*time.Second) // 请求级覆盖

	mw.ProcessRequest(context.Background(), req)

	v, _ := req.GetMeta("download_timeout")
	timeout := v.(time.Duration)
	if timeout != 10*time.Second {
		t.Errorf("should not override existing timeout, got %v", timeout)
	}
}

func TestDownloadTimeoutMiddlewareZero(t *testing.T) {
	mw := NewDownloadTimeoutMiddleware(0, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	mw.ProcessRequest(context.Background(), req)

	_, ok := req.GetMeta("download_timeout")
	if ok {
		t.Error("zero timeout should not set meta")
	}
}

// ============================================================================
// HttpAuth 测试
// ============================================================================

func TestHttpAuthMiddleware(t *testing.T) {
	mw := NewHttpAuthMiddleware("user", "pass", "", nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	mw.ProcessRequest(context.Background(), req)

	auth := req.Headers.Get("Authorization")
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if auth != expected {
		t.Errorf("expected Authorization=%s, got %s", expected, auth)
	}
}

func TestHttpAuthMiddlewareNoOverride(t *testing.T) {
	mw := NewHttpAuthMiddleware("user", "pass", "", nil)

	req := scrapy_http.MustNewRequest("https://example.com",
		scrapy_http.WithHeader("Authorization", "Bearer existing-token"),
	)
	mw.ProcessRequest(context.Background(), req)

	auth := req.Headers.Get("Authorization")
	if auth != "Bearer existing-token" {
		t.Errorf("should not override existing Authorization header, got %s", auth)
	}
}

func TestHttpAuthMiddlewareDomainMatch(t *testing.T) {
	mw := NewHttpAuthMiddleware("user", "pass", "example.com", nil)

	// 匹配域名
	req1 := scrapy_http.MustNewRequest("https://example.com/page")
	mw.ProcessRequest(context.Background(), req1)
	if req1.Headers.Get("Authorization") == "" {
		t.Error("should set auth for matching domain")
	}

	// 子域名匹配
	req2 := scrapy_http.MustNewRequest("https://sub.example.com/page")
	mw.ProcessRequest(context.Background(), req2)
	if req2.Headers.Get("Authorization") == "" {
		t.Error("should set auth for subdomain")
	}

	// 不匹配域名
	req3 := scrapy_http.MustNewRequest("https://other.com/page")
	mw.ProcessRequest(context.Background(), req3)
	if req3.Headers.Get("Authorization") != "" {
		t.Error("should not set auth for non-matching domain")
	}
}

func TestHttpAuthMiddlewareNoCredentials(t *testing.T) {
	mw := NewHttpAuthMiddleware("", "", "", nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	mw.ProcessRequest(context.Background(), req)

	if req.Headers.Get("Authorization") != "" {
		t.Error("should not set auth when no credentials")
	}
}

func TestHttpAuthMiddlewareMetaOverride(t *testing.T) {
	mw := NewHttpAuthMiddleware("global_user", "global_pass", "", nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	req.SetMeta("http_user", "meta_user")
	req.SetMeta("http_pass", "meta_pass")
	mw.ProcessRequest(context.Background(), req)

	auth := req.Headers.Get("Authorization")
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("meta_user:meta_pass"))
	if auth != expected {
		t.Errorf("expected meta-level auth, got %s", auth)
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
	var _ DownloaderMiddleware = (*DownloadTimeoutMiddleware)(nil)
	var _ DownloaderMiddleware = (*HttpAuthMiddleware)(nil)
	var _ DownloaderMiddleware = (*CookiesMiddleware)(nil)
	var _ DownloaderMiddleware = (*HttpCompressionMiddleware)(nil)
}

// ============================================================================
// 测试辅助类型
// ============================================================================



// ============================================================================
// Cookies 测试
// ============================================================================

func TestCookiesMiddlewareProcessResponse(t *testing.T) {
	mw := NewCookiesMiddleware(false, nil)

	req := scrapy_http.MustNewRequest("https://example.com/login")
	resp := scrapy_http.MustNewResponse("https://example.com/login", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Set-Cookie": {"session=abc123; Path=/"},
		}),
	)

	// ProcessResponse 应提取 Set-Cookie 并存入 Jar
	_, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 后续请求应携带 Cookie
	req2 := scrapy_http.MustNewRequest("https://example.com/dashboard")
	mw.ProcessRequest(context.Background(), req2)

	cookieHeader := req2.Headers.Get("Cookie")
	if cookieHeader == "" {
		t.Fatal("should have Cookie header after Set-Cookie")
	}
	if cookieHeader != "session=abc123" {
		t.Errorf("expected Cookie=session=abc123, got %s", cookieHeader)
	}
}

func TestCookiesMiddlewareMultipleSetCookies(t *testing.T) {
	mw := NewCookiesMiddleware(false, nil)

	req := scrapy_http.MustNewRequest("https://example.com/login")
	resp := scrapy_http.MustNewResponse("https://example.com/login", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Set-Cookie": {
				"session=abc123; Path=/",
				"user=john; Path=/",
			},
		}),
	)

	mw.ProcessResponse(context.Background(), req, resp)

	req2 := scrapy_http.MustNewRequest("https://example.com/page")
	mw.ProcessRequest(context.Background(), req2)

	cookieHeader := req2.Headers.Get("Cookie")
	// 应包含两个 Cookie
	if cookieHeader == "" {
		t.Fatal("should have Cookie header")
	}
	if !containsSubstring(cookieHeader, "session=abc123") {
		t.Errorf("should contain session cookie, got %s", cookieHeader)
	}
	if !containsSubstring(cookieHeader, "user=john") {
		t.Errorf("should contain user cookie, got %s", cookieHeader)
	}
}

func TestCookiesMiddlewareDontMergeCookies(t *testing.T) {
	mw := NewCookiesMiddleware(false, nil)

	// 先设置一个 Cookie
	req1 := scrapy_http.MustNewRequest("https://example.com/login")
	resp := scrapy_http.MustNewResponse("https://example.com/login", 200,
		scrapy_http.WithRequest(req1),
		scrapy_http.WithResponseHeaders(http.Header{
			"Set-Cookie": {"session=abc123; Path=/"},
		}),
	)
	mw.ProcessResponse(context.Background(), req1, resp)

	// 带 dont_merge_cookies 的请求不应注入 Cookie
	req2 := scrapy_http.MustNewRequest("https://example.com/page")
	req2.SetMeta("dont_merge_cookies", true)
	mw.ProcessRequest(context.Background(), req2)

	if req2.Headers.Get("Cookie") != "" {
		t.Error("dont_merge_cookies should prevent Cookie injection")
	}
}

func TestCookiesMiddlewareMultiSession(t *testing.T) {
	mw := NewCookiesMiddleware(false, nil)

	// 会话 1：设置 Cookie
	req1 := scrapy_http.MustNewRequest("https://example.com/login")
	req1.SetMeta("cookiejar", "session1")
	resp1 := scrapy_http.MustNewResponse("https://example.com/login", 200,
		scrapy_http.WithRequest(req1),
		scrapy_http.WithResponseHeaders(http.Header{
			"Set-Cookie": {"user=alice; Path=/"},
		}),
	)
	mw.ProcessResponse(context.Background(), req1, resp1)

	// 会话 2：设置不同的 Cookie
	req2 := scrapy_http.MustNewRequest("https://example.com/login")
	req2.SetMeta("cookiejar", "session2")
	resp2 := scrapy_http.MustNewResponse("https://example.com/login", 200,
		scrapy_http.WithRequest(req2),
		scrapy_http.WithResponseHeaders(http.Header{
			"Set-Cookie": {"user=bob; Path=/"},
		}),
	)
	mw.ProcessResponse(context.Background(), req2, resp2)

	// 会话 1 的请求应携带 alice 的 Cookie
	req3 := scrapy_http.MustNewRequest("https://example.com/page")
	req3.SetMeta("cookiejar", "session1")
	mw.ProcessRequest(context.Background(), req3)
	if req3.Headers.Get("Cookie") != "user=alice" {
		t.Errorf("session1 should have user=alice, got %s", req3.Headers.Get("Cookie"))
	}

	// 会话 2 的请求应携带 bob 的 Cookie
	req4 := scrapy_http.MustNewRequest("https://example.com/page")
	req4.SetMeta("cookiejar", "session2")
	mw.ProcessRequest(context.Background(), req4)
	if req4.Headers.Get("Cookie") != "user=bob" {
		t.Errorf("session2 should have user=bob, got %s", req4.Headers.Get("Cookie"))
	}

	// 验证 Jar 数量
	if mw.JarCount() != 2 {
		t.Errorf("expected 2 jars, got %d", mw.JarCount())
	}
}

func TestCookiesMiddlewareRequestCookies(t *testing.T) {
	mw := NewCookiesMiddleware(false, nil)

	req := scrapy_http.MustNewRequest("https://example.com/page",
		scrapy_http.WithCookies([]*http.Cookie{
			{Name: "init", Value: "cookie1"},
		}),
	)
	mw.ProcessRequest(context.Background(), req)

	// Request.Cookies 应被注入到 Jar 中
	u, _ := url.Parse("https://example.com/page")
	cookies := mw.GetCookies(nil, u)
	if len(cookies) == 0 {
		t.Fatal("Request.Cookies should be stored in jar")
	}

	found := false
	for _, c := range cookies {
		if c.Name == "init" && c.Value == "cookie1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("should find init=cookie1 in jar")
	}
}

func TestCookiesMiddlewareCrossDomain(t *testing.T) {
	mw := NewCookiesMiddleware(false, nil)

	// 在 example.com 设置 Cookie
	req1 := scrapy_http.MustNewRequest("https://example.com/login")
	resp := scrapy_http.MustNewResponse("https://example.com/login", 200,
		scrapy_http.WithRequest(req1),
		scrapy_http.WithResponseHeaders(http.Header{
			"Set-Cookie": {"session=abc; Path=/"},
		}),
	)
	mw.ProcessResponse(context.Background(), req1, resp)

	// 对 other.com 的请求不应携带 example.com 的 Cookie
	req2 := scrapy_http.MustNewRequest("https://other.com/page")
	mw.ProcessRequest(context.Background(), req2)
	if req2.Headers.Get("Cookie") != "" {
		t.Error("should not send cookies to different domain")
	}
}

func TestCookiesMiddlewareDebug(t *testing.T) {
	// 仅验证 debug 模式不会 panic
	mw := NewCookiesMiddleware(true, nil)

	req := scrapy_http.MustNewRequest("https://example.com/login")
	resp := scrapy_http.MustNewResponse("https://example.com/login", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Set-Cookie": {"session=abc; Path=/"},
		}),
	)

	mw.ProcessResponse(context.Background(), req, resp)

	req2 := scrapy_http.MustNewRequest("https://example.com/page")
	mw.ProcessRequest(context.Background(), req2)
	// 不 panic 即为通过
}

func TestCookiesMiddlewareDontMergeResponse(t *testing.T) {
	mw := NewCookiesMiddleware(false, nil)

	req := scrapy_http.MustNewRequest("https://example.com/login")
	req.SetMeta("dont_merge_cookies", true)
	resp := scrapy_http.MustNewResponse("https://example.com/login", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Set-Cookie": {"session=abc; Path=/"},
		}),
	)

	mw.ProcessResponse(context.Background(), req, resp)

	// dont_merge_cookies 应阻止 Set-Cookie 被存入 Jar
	req2 := scrapy_http.MustNewRequest("https://example.com/page")
	mw.ProcessRequest(context.Background(), req2)
	if req2.Headers.Get("Cookie") != "" {
		t.Error("dont_merge_cookies should prevent Set-Cookie extraction")
	}
}

// ============================================================================
// HttpCompression 测试
// ============================================================================

func TestHttpCompressionMiddlewareAcceptEncoding(t *testing.T) {
	mw := NewHttpCompressionMiddleware(1024*1024, 32*1024, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	mw.ProcessRequest(context.Background(), req)

	ae := req.Headers.Get("Accept-Encoding")
	if ae == "" {
		t.Fatal("should set Accept-Encoding header")
	}
	if !containsSubstring(ae, "gzip") {
		t.Errorf("Accept-Encoding should contain gzip, got %s", ae)
	}
	if !containsSubstring(ae, "deflate") {
		t.Errorf("Accept-Encoding should contain deflate, got %s", ae)
	}
}

func TestHttpCompressionMiddlewareNoOverrideAcceptEncoding(t *testing.T) {
	mw := NewHttpCompressionMiddleware(1024*1024, 32*1024, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com",
		scrapy_http.WithHeader("Accept-Encoding", "identity"),
	)
	mw.ProcessRequest(context.Background(), req)

	if req.Headers.Get("Accept-Encoding") != "identity" {
		t.Error("should not override existing Accept-Encoding")
	}
}

func TestHttpCompressionMiddlewareGzip(t *testing.T) {
	mw := NewHttpCompressionMiddleware(1024*1024, 32*1024, nil, nil)

	originalBody := []byte("Hello, World! This is a test body for gzip compression.")
	compressedBody := gzipCompress(t, originalBody)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseBody(compressedBody),
		scrapy_http.WithResponseHeaders(http.Header{
			"Content-Encoding": {"gzip"},
		}),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(result.Body) != string(originalBody) {
		t.Errorf("decompressed body mismatch: got %q", string(result.Body))
	}

	// Content-Encoding 应被移除
	if result.Headers.Get("Content-Encoding") != "" {
		t.Error("Content-Encoding should be removed after decompression")
	}
}

func TestHttpCompressionMiddlewareNoContentEncoding(t *testing.T) {
	mw := NewHttpCompressionMiddleware(1024*1024, 32*1024, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseBody([]byte("plain text")),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Body) != "plain text" {
		t.Error("uncompressed body should pass through unchanged")
	}
}

func TestHttpCompressionMiddlewareHeadRequest(t *testing.T) {
	mw := NewHttpCompressionMiddleware(1024*1024, 32*1024, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com",
		scrapy_http.WithMethod("HEAD"),
	)
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Content-Encoding": {"gzip"},
		}),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// HEAD 请求不应尝试解压
	if result.Headers.Get("Content-Encoding") != "gzip" {
		t.Error("HEAD request should not decompress")
	}
}

func TestHttpCompressionMiddlewareEmptyBody(t *testing.T) {
	mw := NewHttpCompressionMiddleware(1024*1024, 32*1024, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseHeaders(http.Header{
			"Content-Encoding": {"gzip"},
		}),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Body) != 0 {
		t.Error("empty body should remain empty")
	}
}

func TestHttpCompressionMiddlewareStats(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewHttpCompressionMiddleware(1024*1024, 32*1024, sc, nil)

	originalBody := []byte("test body for stats")
	compressedBody := gzipCompress(t, originalBody)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseBody(compressedBody),
		scrapy_http.WithResponseHeaders(http.Header{
			"Content-Encoding": {"gzip"},
		}),
	)

	mw.ProcessResponse(context.Background(), req, resp)

	count := sc.GetValue("httpcompression/response_count", 0)
	if count != 1 {
		t.Errorf("expected response_count=1, got %v", count)
	}

	bytesVal := sc.GetValue("httpcompression/response_bytes", 0)
	if bytesVal != len(originalBody) {
		t.Errorf("expected response_bytes=%d, got %v", len(originalBody), bytesVal)
	}
}

func TestHttpCompressionMiddlewareMaxSize(t *testing.T) {
	// 设置一个很小的 maxSize
	mw := NewHttpCompressionMiddleware(10, 5, nil, nil)

	// 创建一个解压后超过 10 字节的 gzip 数据
	originalBody := []byte("This is a body that exceeds the max size limit of 10 bytes")
	compressedBody := gzipCompress(t, originalBody)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseBody(compressedBody),
		scrapy_http.WithResponseHeaders(http.Header{
			"Content-Encoding": {"gzip"},
		}),
	)

	// 超过 maxSize 时应返回原始响应（不解压）
	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 解压失败时应返回原始响应
	if string(result.Body) != string(compressedBody) {
		t.Error("should return original response when decompression exceeds max size")
	}
}

func TestHttpCompressionMiddlewareUnknownEncoding(t *testing.T) {
	mw := NewHttpCompressionMiddleware(1024*1024, 32*1024, nil, nil)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseBody([]byte("some data")),
		scrapy_http.WithResponseHeaders(http.Header{
			"Content-Encoding": {"br"},
		}),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 不支持的编码应原样返回
	if string(result.Body) != "some data" {
		t.Error("unknown encoding should pass through unchanged")
	}
}

func TestHttpCompressionMiddlewareDeflate(t *testing.T) {
	mw := NewHttpCompressionMiddleware(1024*1024, 32*1024, nil, nil)

	originalBody := []byte("Hello, World! This is a test body for deflate compression.")
	compressedBody := deflateCompress(t, originalBody)

	req := scrapy_http.MustNewRequest("https://example.com")
	resp := scrapy_http.MustNewResponse("https://example.com", 200,
		scrapy_http.WithRequest(req),
		scrapy_http.WithResponseBody(compressedBody),
		scrapy_http.WithResponseHeaders(http.Header{
			"Content-Encoding": {"deflate"},
		}),
	)

	result, err := mw.ProcessResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(result.Body) != string(originalBody) {
		t.Errorf("decompressed body mismatch: got %q, want %q", string(result.Body), string(originalBody))
	}

	if result.Headers.Get("Content-Encoding") != "" {
		t.Error("Content-Encoding should be removed after decompression")
	}
}

// ============================================================================
// 测试辅助函数
// ============================================================================

// gzipCompress 将数据进行 gzip 压缩。
func gzipCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write(data)
	if err != nil {
		t.Fatalf("gzip write error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close error: %v", err)
	}
	return buf.Bytes()
}

// deflateCompress 将数据进行 deflate（raw deflate）压缩。
func deflateCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		t.Fatalf("deflate writer error: %v", err)
	}
	_, err = writer.Write(data)
	if err != nil {
		t.Fatalf("deflate write error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("deflate close error: %v", err)
	}
	return buf.Bytes()
}

// containsSubstring 检查字符串是否包含子串。
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringImpl(s, substr))
}

func containsSubstringImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
