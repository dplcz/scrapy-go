package httpcache

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/internal/utils"
	scrapyerrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// 测试辅助函数
// ============================================================================

func mustNewRequest(rawURL string) *shttp.Request {
	u, _ := url.Parse(rawURL)
	return &shttp.Request{
		URL:     u,
		Method:  "GET",
		Headers: make(http.Header),
		Meta:    make(map[string]any),
	}
}

func mustNewResponse(rawURL string, status int) *shttp.Response {
	u, _ := url.Parse(rawURL)
	return &shttp.Response{
		URL:     u,
		Status:  status,
		Headers: make(http.Header),
		Body:    []byte("test body"),
	}
}

func newTestStorage(t *testing.T) (*FilesystemCacheStorage, string) {
	t.Helper()
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir)
	if err := storage.Open("test_spider"); err != nil {
		t.Fatalf("open storage: %v", err)
	}
	return storage, dir
}

func newTestMiddleware(t *testing.T) (*HttpCacheMiddleware, stats.Collector) {
	t.Helper()
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir)
	policy := NewDummyPolicy()
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewHttpCacheMiddleware(policy, storage, sc)
	if err := mw.Open("test_spider"); err != nil {
		t.Fatalf("open middleware: %v", err)
	}
	return mw, sc
}

// ============================================================================
// CacheStorage 接口测试
// ============================================================================

func TestFilesystemCacheStorage_StoreAndRetrieve(t *testing.T) {
	storage, _ := newTestStorage(t)
	defer storage.Close()

	req := mustNewRequest("http://example.com/page1")
	resp := mustNewResponse("http://example.com/page1", 200)
	resp.Headers.Set("Content-Type", "text/html")
	resp.Body = []byte("<html>hello</html>")

	// 存储响应
	if err := storage.StoreResponse(req, resp); err != nil {
		t.Fatalf("store response: %v", err)
	}

	// 检索响应
	cached, err := storage.RetrieveResponse(req)
	if err != nil {
		t.Fatalf("retrieve response: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached response, got nil")
	}

	if cached.Status != 200 {
		t.Errorf("status: got %d, want 200", cached.Status)
	}
	if string(cached.Body) != "<html>hello</html>" {
		t.Errorf("body: got %q, want %q", string(cached.Body), "<html>hello</html>")
	}
	if cached.Headers.Get("Content-Type") != "text/html" {
		t.Errorf("content-type: got %q, want %q", cached.Headers.Get("Content-Type"), "text/html")
	}
	if cached.URL.String() != "http://example.com/page1" {
		t.Errorf("url: got %q, want %q", cached.URL.String(), "http://example.com/page1")
	}
}

func TestFilesystemCacheStorage_CacheMiss(t *testing.T) {
	storage, _ := newTestStorage(t)
	defer storage.Close()

	req := mustNewRequest("http://example.com/nonexistent")
	cached, err := storage.RetrieveResponse(req)
	if err != nil {
		t.Fatalf("retrieve response: %v", err)
	}
	if cached != nil {
		t.Fatal("expected nil for cache miss")
	}
}

func TestFilesystemCacheStorage_Expiration(t *testing.T) {
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir, WithExpirationSecs(1))
	if err := storage.Open("test_spider"); err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer storage.Close()

	req := mustNewRequest("http://example.com/expire")
	resp := mustNewResponse("http://example.com/expire", 200)

	if err := storage.StoreResponse(req, resp); err != nil {
		t.Fatalf("store response: %v", err)
	}

	// 立即检索应该命中
	cached, err := storage.RetrieveResponse(req)
	if err != nil {
		t.Fatalf("retrieve response: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached response before expiration")
	}

	// 等待过期
	time.Sleep(1100 * time.Millisecond)

	// 过期后应该未命中
	cached, err = storage.RetrieveResponse(req)
	if err != nil {
		t.Fatalf("retrieve response after expiration: %v", err)
	}
	if cached != nil {
		t.Fatal("expected nil after expiration")
	}
}

func TestFilesystemCacheStorage_GzipCompression(t *testing.T) {
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir, WithGzip(true))
	if err := storage.Open("test_spider"); err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer storage.Close()

	req := mustNewRequest("http://example.com/gzip")
	resp := mustNewResponse("http://example.com/gzip", 200)
	resp.Body = []byte("compressed content")

	if err := storage.StoreResponse(req, resp); err != nil {
		t.Fatalf("store response: %v", err)
	}

	cached, err := storage.RetrieveResponse(req)
	if err != nil {
		t.Fatalf("retrieve response: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached response")
	}
	if string(cached.Body) != "compressed content" {
		t.Errorf("body: got %q, want %q", string(cached.Body), "compressed content")
	}
}

func TestFilesystemCacheStorage_DirectoryStructure(t *testing.T) {
	storage, dir := newTestStorage(t)
	defer storage.Close()

	req := mustNewRequest("http://example.com/structure")
	resp := mustNewResponse("http://example.com/structure", 200)

	if err := storage.StoreResponse(req, resp); err != nil {
		t.Fatalf("store response: %v", err)
	}

	// 验证目录结构：{cacheDir}/{spiderName}/{fp[0:2]}/{fp}/
	fp := utils.RequestFingerprint(req, nil, false)
	expectedDir := filepath.Join(dir, "test_spider", fp[:2], fp)

	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Fatalf("expected cache directory %s to exist", expectedDir)
	}

	// 验证文件存在
	for _, file := range []string{"meta.json", "response_headers", "response_body", "request_headers", "request_body"} {
		path := filepath.Join(expectedDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", path)
		}
	}
}

func TestFilesystemCacheStorage_MultipleHeaders(t *testing.T) {
	storage, _ := newTestStorage(t)
	defer storage.Close()

	req := mustNewRequest("http://example.com/multiheader")
	resp := mustNewResponse("http://example.com/multiheader", 200)
	resp.Headers.Set("Content-Type", "text/html")
	resp.Headers.Set("X-Custom", "value1")
	resp.Headers.Add("Set-Cookie", "a=1")
	resp.Headers.Add("Set-Cookie", "b=2")

	if err := storage.StoreResponse(req, resp); err != nil {
		t.Fatalf("store response: %v", err)
	}

	cached, err := storage.RetrieveResponse(req)
	if err != nil {
		t.Fatalf("retrieve response: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached response")
	}

	if cached.Headers.Get("Content-Type") != "text/html" {
		t.Errorf("Content-Type: got %q, want %q", cached.Headers.Get("Content-Type"), "text/html")
	}
	if cached.Headers.Get("X-Custom") != "value1" {
		t.Errorf("X-Custom: got %q, want %q", cached.Headers.Get("X-Custom"), "value1")
	}
	cookies := cached.Headers.Values("Set-Cookie")
	if len(cookies) != 2 {
		t.Errorf("Set-Cookie count: got %d, want 2", len(cookies))
	}
}

// ============================================================================
// DummyPolicy 测试
// ============================================================================

func TestDummyPolicy_ShouldCacheRequest(t *testing.T) {
	policy := NewDummyPolicy()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"http scheme", "http://example.com", true},
		{"https scheme", "https://example.com", true},
		{"file scheme", "file:///tmp/test", false},
		{"ftp scheme", "ftp://example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mustNewRequest(tt.url)
			got := policy.ShouldCacheRequest(req)
			if got != tt.want {
				t.Errorf("ShouldCacheRequest(%s): got %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestDummyPolicy_ShouldCacheRequest_CustomSchemes(t *testing.T) {
	policy := NewDummyPolicy(WithIgnoreSchemes([]string{"ftp", "file"}))

	req := mustNewRequest("ftp://example.com/file")
	if policy.ShouldCacheRequest(req) {
		t.Error("expected ftp to be ignored")
	}

	req = mustNewRequest("http://example.com")
	if !policy.ShouldCacheRequest(req) {
		t.Error("expected http to be cached")
	}
}

func TestDummyPolicy_ShouldCacheResponse(t *testing.T) {
	policy := NewDummyPolicy(WithIgnoreHTTPCodes([]int{404, 500}))

	tests := []struct {
		status int
		want   bool
	}{
		{200, true},
		{301, true},
		{404, false},
		{500, false},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			req := mustNewRequest("http://example.com")
			resp := mustNewResponse("http://example.com", tt.status)
			got := policy.ShouldCacheResponse(resp, req)
			if got != tt.want {
				t.Errorf("ShouldCacheResponse(status=%d): got %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestDummyPolicy_AlwaysFresh(t *testing.T) {
	policy := NewDummyPolicy()
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 200)

	if !policy.IsCachedResponseFresh(resp, req) {
		t.Error("DummyPolicy should always return fresh")
	}
}

func TestDummyPolicy_AlwaysValid(t *testing.T) {
	policy := NewDummyPolicy()
	req := mustNewRequest("http://example.com")
	cached := mustNewResponse("http://example.com", 200)
	newResp := mustNewResponse("http://example.com", 200)

	if !policy.IsCachedResponseValid(cached, newResp, req) {
		t.Error("DummyPolicy should always return valid")
	}
}

func TestDummyPolicy_NilURL(t *testing.T) {
	policy := NewDummyPolicy()
	req := &shttp.Request{Method: "GET", Headers: make(http.Header)}
	if policy.ShouldCacheRequest(req) {
		t.Error("expected false for nil URL")
	}
}

// ============================================================================
// RFC2616Policy 测试
// ============================================================================

func TestRFC2616Policy_ShouldCacheRequest_NoStore(t *testing.T) {
	policy := NewRFC2616Policy()

	req := mustNewRequest("http://example.com")
	req.Headers.Set("Cache-Control", "no-store")

	if policy.ShouldCacheRequest(req) {
		t.Error("expected no-store request to not be cached")
	}
}

func TestRFC2616Policy_ShouldCacheRequest_IgnoreScheme(t *testing.T) {
	policy := NewRFC2616Policy()

	req := mustNewRequest("file:///tmp/test")
	if policy.ShouldCacheRequest(req) {
		t.Error("expected file scheme to not be cached")
	}
}

func TestRFC2616Policy_ShouldCacheResponse_NoStore(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "no-store")

	if policy.ShouldCacheResponse(resp, req) {
		t.Error("expected no-store response to not be cached")
	}
}

func TestRFC2616Policy_ShouldCacheResponse_304(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 304)

	if policy.ShouldCacheResponse(resp, req) {
		t.Error("expected 304 response to not be cached")
	}
}

func TestRFC2616Policy_ShouldCacheResponse_AlwaysStore(t *testing.T) {
	policy := NewRFC2616Policy(WithAlwaysStore(true))
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 200)

	if !policy.ShouldCacheResponse(resp, req) {
		t.Error("expected always_store to cache response")
	}
}

func TestRFC2616Policy_ShouldCacheResponse_MaxAge(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "max-age=3600")

	if !policy.ShouldCacheResponse(resp, req) {
		t.Error("expected response with max-age to be cached")
	}
}

func TestRFC2616Policy_ShouldCacheResponse_Expires(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Expires", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))

	if !policy.ShouldCacheResponse(resp, req) {
		t.Error("expected response with Expires to be cached")
	}
}

func TestRFC2616Policy_ShouldCacheResponse_PermanentRedirect(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")

	for _, status := range []int{300, 301, 308} {
		resp := mustNewResponse("http://example.com", status)
		if !policy.ShouldCacheResponse(resp, req) {
			t.Errorf("expected status %d to be cached", status)
		}
	}
}

func TestRFC2616Policy_ShouldCacheResponse_WithValidator(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")

	// 200 with ETag
	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("ETag", `"abc123"`)
	if !policy.ShouldCacheResponse(resp, req) {
		t.Error("expected 200 with ETag to be cached")
	}

	// 200 with Last-Modified
	resp2 := mustNewResponse("http://example.com", 200)
	resp2.Headers.Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	if !policy.ShouldCacheResponse(resp2, req) {
		t.Error("expected 200 with Last-Modified to be cached")
	}

	// 200 without validator
	resp3 := mustNewResponse("http://example.com", 200)
	if policy.ShouldCacheResponse(resp3, req) {
		t.Error("expected 200 without validator to not be cached")
	}
}

func TestRFC2616Policy_IsCachedResponseFresh_MaxAge(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")

	// 新鲜的缓存
	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "max-age=3600")
	resp.Headers.Set("Date", time.Now().UTC().Format(http.TimeFormat))

	if !policy.IsCachedResponseFresh(resp, req) {
		t.Error("expected fresh response with max-age=3600")
	}
}

func TestRFC2616Policy_IsCachedResponseFresh_Expired(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")

	// 过期的缓存
	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "max-age=0")
	resp.Headers.Set("Date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))

	if policy.IsCachedResponseFresh(resp, req) {
		t.Error("expected expired response")
	}
}

func TestRFC2616Policy_IsCachedResponseFresh_NoCache(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "no-cache")

	if policy.IsCachedResponseFresh(resp, req) {
		t.Error("expected no-cache to not be fresh")
	}
}

func TestRFC2616Policy_IsCachedResponseFresh_RequestNoCache(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	req.Headers.Set("Cache-Control", "no-cache")

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "max-age=3600")
	resp.Headers.Set("Date", time.Now().UTC().Format(http.TimeFormat))

	if policy.IsCachedResponseFresh(resp, req) {
		t.Error("expected request no-cache to force revalidation")
	}
}

func TestRFC2616Policy_IsCachedResponseFresh_MaxStale(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	req.Headers.Set("Cache-Control", "max-stale=3600")

	// 过期 10 秒的缓存
	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "max-age=1")
	resp.Headers.Set("Date", time.Now().Add(-10*time.Second).UTC().Format(http.TimeFormat))

	if !policy.IsCachedResponseFresh(resp, req) {
		t.Error("expected max-stale to accept stale response")
	}
}

func TestRFC2616Policy_IsCachedResponseFresh_MaxStaleUnlimited(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	req.Headers.Set("Cache-Control", "max-stale")

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "max-age=0")
	resp.Headers.Set("Date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))

	if !policy.IsCachedResponseFresh(resp, req) {
		t.Error("expected unlimited max-stale to accept any stale response")
	}
}

func TestRFC2616Policy_IsCachedResponseFresh_MustRevalidate(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	req.Headers.Set("Cache-Control", "max-stale=3600")

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "max-age=0, must-revalidate")
	resp.Headers.Set("Date", time.Now().Add(-10*time.Second).UTC().Format(http.TimeFormat))

	if policy.IsCachedResponseFresh(resp, req) {
		t.Error("expected must-revalidate to override max-stale")
	}
}

func TestRFC2616Policy_IsCachedResponseFresh_ConditionalValidators(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "max-age=0")
	resp.Headers.Set("Date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	resp.Headers.Set("ETag", `"abc123"`)
	resp.Headers.Set("Last-Modified", time.Now().Add(-24*time.Hour).UTC().Format(http.TimeFormat))

	// 调用 IsCachedResponseFresh 应该设置条件验证头
	policy.IsCachedResponseFresh(resp, req)

	if req.Headers.Get("If-None-Match") != `"abc123"` {
		t.Errorf("If-None-Match: got %q, want %q", req.Headers.Get("If-None-Match"), `"abc123"`)
	}
	if req.Headers.Get("If-Modified-Since") == "" {
		t.Error("expected If-Modified-Since to be set")
	}
}

func TestRFC2616Policy_IsCachedResponseValid_304(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	cached := mustNewResponse("http://example.com", 200)
	newResp := mustNewResponse("http://example.com", 304)

	if !policy.IsCachedResponseValid(cached, newResp, req) {
		t.Error("expected 304 to validate cached response")
	}
}

func TestRFC2616Policy_IsCachedResponseValid_ServerError(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	cached := mustNewResponse("http://example.com", 200)
	newResp := mustNewResponse("http://example.com", 500)

	if !policy.IsCachedResponseValid(cached, newResp, req) {
		t.Error("expected server error to use cached response")
	}
}

func TestRFC2616Policy_IsCachedResponseValid_ServerErrorMustRevalidate(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	cached := mustNewResponse("http://example.com", 200)
	cached.Headers.Set("Cache-Control", "must-revalidate")
	newResp := mustNewResponse("http://example.com", 500)

	if policy.IsCachedResponseValid(cached, newResp, req) {
		t.Error("expected must-revalidate to not use cached response on server error")
	}
}

func TestRFC2616Policy_IgnoreResponseCacheControls(t *testing.T) {
	policy := NewRFC2616Policy(WithIgnoreResponseCacheControls([]string{"no-store"}))
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "no-store, max-age=3600")

	// no-store 被忽略，应该可以缓存
	if !policy.ShouldCacheResponse(resp, req) {
		t.Error("expected response to be cached when no-store is ignored")
	}
}

func TestRFC2616Policy_RequestMaxAge(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	req.Headers.Set("Cache-Control", "max-age=1")

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Cache-Control", "max-age=3600")
	resp.Headers.Set("Date", time.Now().Add(-2*time.Second).UTC().Format(http.TimeFormat))

	// 请求的 max-age=1 应该限制新鲜度
	if policy.IsCachedResponseFresh(resp, req) {
		t.Error("expected request max-age to limit freshness")
	}
}

// ============================================================================
// Cache-Control 解析测试
// ============================================================================

func TestParseCacheControl(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   map[string]string
	}{
		{
			"empty",
			"",
			map[string]string{},
		},
		{
			"single directive",
			"no-cache",
			map[string]string{"no-cache": ""},
		},
		{
			"max-age",
			"max-age=3600",
			map[string]string{"max-age": "3600"},
		},
		{
			"multiple directives",
			"public, max-age=3600",
			map[string]string{"public": "", "max-age": "3600"},
		},
		{
			"with spaces",
			"  no-cache ,  max-age = 100  ",
			map[string]string{"no-cache": "", "max-age": "100"},
		},
		{
			"complex",
			"no-store, no-cache, must-revalidate, max-age=0",
			map[string]string{"no-store": "", "no-cache": "", "must-revalidate": "", "max-age": "0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCacheControl(tt.header)
			if len(got) != len(tt.want) {
				t.Errorf("len: got %d, want %d", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// ============================================================================
// HTTP 日期解析测试
// ============================================================================

func TestParseHTTPDate(t *testing.T) {
	tests := []struct {
		name    string
		dateStr string
		wantOK  bool
	}{
		{"RFC 1123", "Mon, 02 Jan 2006 15:04:05 GMT", true},
		{"RFC 850", "Monday, 02-Jan-06 15:04:05 GMT", true},
		{"ANSI C", "Mon Jan  2 15:04:05 2006", true},
		{"empty", "", false},
		{"invalid", "not a date", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseHTTPDate(tt.dateStr)
			if tt.wantOK && result.IsZero() {
				t.Errorf("expected valid date for %q", tt.dateStr)
			}
			if !tt.wantOK && !result.IsZero() {
				t.Errorf("expected zero time for %q", tt.dateStr)
			}
		})
	}
}

// ============================================================================
// HttpCacheMiddleware 测试
// ============================================================================

func TestMiddleware_ProcessRequest_CacheMiss(t *testing.T) {
	mw, sc := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/miss")

	resp, err := mw.ProcessRequest(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response for cache miss")
	}

	if sc.GetValue("httpcache/miss", 0) != 1 {
		t.Error("expected httpcache/miss to be 1")
	}
}

func TestMiddleware_ProcessRequest_CacheHit(t *testing.T) {
	mw, sc := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/hit")
	resp := mustNewResponse("http://example.com/hit", 200)
	resp.Body = []byte("cached content")

	// 先存储
	if err := mw.storage.StoreResponse(req, resp); err != nil {
		t.Fatalf("store: %v", err)
	}

	// 再查找
	cached, err := mw.ProcessRequest(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached response")
	}
	if string(cached.Body) != "cached content" {
		t.Errorf("body: got %q, want %q", string(cached.Body), "cached content")
	}
	if !hasFlag(cached.Flags, "cached") {
		t.Error("expected 'cached' flag")
	}

	if sc.GetValue("httpcache/hit", 0) != 1 {
		t.Error("expected httpcache/hit to be 1")
	}
}

func TestMiddleware_ProcessRequest_DontCache(t *testing.T) {
	mw, _ := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/dontcache")
	req.SetMeta("dont_cache", true)

	// 先存储
	resp := mustNewResponse("http://example.com/dontcache", 200)
	if err := mw.storage.StoreResponse(req, resp); err != nil {
		t.Fatalf("store: %v", err)
	}

	// dont_cache 应该跳过缓存查找
	cached, err := mw.ProcessRequest(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached != nil {
		t.Fatal("expected nil response when dont_cache is set")
	}
}

func TestMiddleware_ProcessRequest_IgnoreMissing(t *testing.T) {
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir)
	policy := NewDummyPolicy()
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewHttpCacheMiddleware(policy, storage, sc, WithIgnoreMissing(true))
	if err := mw.Open("test_spider"); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/missing")

	_, err := mw.ProcessRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for ignore_missing")
	}
	if err != scrapyerrors.ErrIgnoreRequest {
		t.Errorf("expected ErrIgnoreRequest, got %v", err)
	}

	if sc.GetValue("httpcache/miss", 0) != 1 {
		t.Error("expected httpcache/miss to be 1")
	}
	if sc.GetValue("httpcache/ignore", 0) != 1 {
		t.Error("expected httpcache/ignore to be 1")
	}
}

func TestMiddleware_ProcessResponse_Store(t *testing.T) {
	mw, sc := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/store")
	resp := mustNewResponse("http://example.com/store", 200)

	result, err := mw.ProcessResponse(ctx, req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response")
	}

	if sc.GetValue("httpcache/firsthand", 0) != 1 {
		t.Error("expected httpcache/firsthand to be 1")
	}
	if sc.GetValue("httpcache/store", 0) != 1 {
		t.Error("expected httpcache/store to be 1")
	}

	// 验证 Date 头被设置
	if resp.Headers.Get("Date") == "" {
		t.Error("expected Date header to be set")
	}
}

func TestMiddleware_ProcessResponse_DontCache(t *testing.T) {
	mw, sc := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/dontcache")
	req.SetMeta("dont_cache", true)
	resp := mustNewResponse("http://example.com/dontcache", 200)

	result, err := mw.ProcessResponse(ctx, req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response")
	}

	// 不应该存储
	if sc.GetValue("httpcache/store", 0) != 0 {
		t.Error("expected httpcache/store to be 0")
	}
}

func TestMiddleware_ProcessResponse_SkipCached(t *testing.T) {
	mw, sc := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/cached")
	resp := mustNewResponse("http://example.com/cached", 200)
	resp.Flags = append(resp.Flags, "cached")

	result, err := mw.ProcessResponse(ctx, req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response")
	}

	// 不应该存储已缓存的响应
	if sc.GetValue("httpcache/store", 0) != 0 {
		t.Error("expected httpcache/store to be 0")
	}
}

func TestMiddleware_ProcessResponse_Revalidate(t *testing.T) {
	mw, sc := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/revalidate")
	cachedResp := mustNewResponse("http://example.com/revalidate", 200)
	cachedResp.Body = []byte("cached body")
	req.SetMeta("cached_response", cachedResp)

	// DummyPolicy 的 IsCachedResponseValid 始终返回 true
	newResp := mustNewResponse("http://example.com/revalidate", 200)
	newResp.Body = []byte("new body")

	result, err := mw.ProcessResponse(ctx, req, newResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Body) != "cached body" {
		t.Errorf("expected cached body, got %q", string(result.Body))
	}

	if sc.GetValue("httpcache/revalidate", 0) != 1 {
		t.Error("expected httpcache/revalidate to be 1")
	}
}

func TestMiddleware_ProcessResponse_Uncacheable(t *testing.T) {
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir)
	policy := NewDummyPolicy(WithIgnoreHTTPCodes([]int{404}))
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewHttpCacheMiddleware(policy, storage, sc)
	if err := mw.Open("test_spider"); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/notfound")
	resp := mustNewResponse("http://example.com/notfound", 404)

	_, err := mw.ProcessResponse(ctx, req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sc.GetValue("httpcache/uncacheable", 0) != 1 {
		t.Error("expected httpcache/uncacheable to be 1")
	}
}

func TestMiddleware_ProcessException_ErrorRecovery(t *testing.T) {
	mw, sc := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/error")
	cachedResp := mustNewResponse("http://example.com/error", 200)
	cachedResp.Body = []byte("cached on error")
	req.SetMeta("cached_response", cachedResp)

	result, err := mw.ProcessException(ctx, req, scrapyerrors.ErrDownloadTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected cached response on error recovery")
	}
	if string(result.Body) != "cached on error" {
		t.Errorf("body: got %q, want %q", string(result.Body), "cached on error")
	}

	if sc.GetValue("httpcache/errorrecovery", 0) != 1 {
		t.Error("expected httpcache/errorrecovery to be 1")
	}
}

func TestMiddleware_ProcessException_NoCachedResponse(t *testing.T) {
	mw, _ := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/error")

	result, err := mw.ProcessException(ctx, req, scrapyerrors.ErrDownloadTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil when no cached response")
	}
}

func TestMiddleware_ProcessException_NonDownloadError(t *testing.T) {
	mw, sc := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/error")
	cachedResp := mustNewResponse("http://example.com/error", 200)
	req.SetMeta("cached_response", cachedResp)

	// 非下载异常不应该触发错误恢复
	result, err := mw.ProcessException(ctx, req, scrapyerrors.ErrDropItem)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil for non-download exception")
	}

	if sc.GetValue("httpcache/errorrecovery", 0) != 0 {
		t.Error("expected httpcache/errorrecovery to be 0")
	}
}

func TestMiddleware_ProcessResponse_DontCacheMeta(t *testing.T) {
	mw, _ := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/dontcache_meta")
	req.SetMeta("_dont_cache", true)
	resp := mustNewResponse("http://example.com/dontcache_meta", 200)

	result, err := mw.ProcessResponse(ctx, req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response")
	}

	// _dont_cache 应该被清理
	if _, ok := req.GetMeta("_dont_cache"); ok {
		t.Error("expected _dont_cache to be cleaned up")
	}
}

// ============================================================================
// 端到端测试
// ============================================================================

func TestMiddleware_EndToEnd_CacheAndRetrieve(t *testing.T) {
	mw, sc := newTestMiddleware(t)
	defer mw.Close()

	ctx := context.Background()
	reqURL := "http://example.com/e2e"

	// 第一次请求：缓存未命中
	req1 := mustNewRequest(reqURL)
	cached, err := mw.ProcessRequest(ctx, req1)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	if cached != nil {
		t.Fatal("expected cache miss on first request")
	}

	// 模拟下载响应
	resp := mustNewResponse(reqURL, 200)
	resp.Body = []byte("first response")
	resp.Headers.Set("Content-Type", "text/html")

	// ProcessResponse 存储缓存
	_, err = mw.ProcessResponse(ctx, req1, resp)
	if err != nil {
		t.Fatalf("process response: %v", err)
	}

	// 第二次请求：缓存命中
	req2 := mustNewRequest(reqURL)
	cached, err = mw.ProcessRequest(ctx, req2)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cache hit on second request")
	}
	if string(cached.Body) != "first response" {
		t.Errorf("body: got %q, want %q", string(cached.Body), "first response")
	}
	if !hasFlag(cached.Flags, "cached") {
		t.Error("expected 'cached' flag")
	}

	// 验证统计
	if sc.GetValue("httpcache/miss", 0) != 1 {
		t.Errorf("httpcache/miss: got %v, want 1", sc.GetValue("httpcache/miss", 0))
	}
	if sc.GetValue("httpcache/hit", 0) != 1 {
		t.Errorf("httpcache/hit: got %v, want 1", sc.GetValue("httpcache/hit", 0))
	}
	if sc.GetValue("httpcache/store", 0) != 1 {
		t.Errorf("httpcache/store: got %v, want 1", sc.GetValue("httpcache/store", 0))
	}
	if sc.GetValue("httpcache/firsthand", 0) != 1 {
		t.Errorf("httpcache/firsthand: got %v, want 1", sc.GetValue("httpcache/firsthand", 0))
	}
}

func TestMiddleware_EndToEnd_RFC2616_ConditionalValidation(t *testing.T) {
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir)
	policy := NewRFC2616Policy()
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewHttpCacheMiddleware(policy, storage, sc)
	if err := mw.Open("test_spider"); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer mw.Close()

	ctx := context.Background()
	reqURL := "http://example.com/rfc2616"

	// 第一次请求
	req1 := mustNewRequest(reqURL)
	_, _ = mw.ProcessRequest(ctx, req1)

	// 响应带 ETag 和 max-age=0（立即过期）
	resp := mustNewResponse(reqURL, 200)
	resp.Body = []byte("original content")
	resp.Headers.Set("ETag", `"v1"`)
	resp.Headers.Set("Cache-Control", "max-age=0")
	resp.Headers.Set("Date", time.Now().UTC().Format(http.TimeFormat))

	_, _ = mw.ProcessResponse(ctx, req1, resp)

	// 第二次请求：缓存过期，应该设置条件验证头
	req2 := mustNewRequest(reqURL)
	cached, _ := mw.ProcessRequest(ctx, req2)
	if cached != nil {
		t.Fatal("expected cache miss for expired response")
	}

	// 验证条件验证头被设置
	if req2.Headers.Get("If-None-Match") != `"v1"` {
		t.Errorf("If-None-Match: got %q, want %q", req2.Headers.Get("If-None-Match"), `"v1"`)
	}

	// 模拟 304 响应
	resp304 := mustNewResponse(reqURL, 304)
	result, _ := mw.ProcessResponse(ctx, req2, resp304)

	// 应该返回缓存的响应
	if string(result.Body) != "original content" {
		t.Errorf("body: got %q, want %q", string(result.Body), "original content")
	}

	if sc.GetValue("httpcache/revalidate", 0) != 1 {
		t.Errorf("httpcache/revalidate: got %v, want 1", sc.GetValue("httpcache/revalidate", 0))
	}
}

// ============================================================================
// 辅助函数测试
// ============================================================================

func TestRequestFingerprint(t *testing.T) {
	req1 := mustNewRequest("http://example.com/page1")
	req2 := mustNewRequest("http://example.com/page1")
	req3 := mustNewRequest("http://example.com/page2")

	fp1 := utils.RequestFingerprint(req1, nil, false)
	fp2 := utils.RequestFingerprint(req2, nil, false)
	fp3 := utils.RequestFingerprint(req3, nil, false)

	if fp1 != fp2 {
		t.Error("same request should have same fingerprint")
	}
	if fp1 == fp3 {
		t.Error("different requests should have different fingerprints")
	}
	if len(fp1) != 40 {
		t.Errorf("fingerprint length: got %d, want 40", len(fp1))
	}
}

func TestRequestFingerprint_MethodMatters(t *testing.T) {
	req1 := mustNewRequest("http://example.com/page")
	req2 := mustNewRequest("http://example.com/page")
	req2.Method = "POST"

	fp1 := utils.RequestFingerprint(req1, nil, false)
	fp2 := utils.RequestFingerprint(req2, nil, false)

	if fp1 == fp2 {
		t.Error("different methods should have different fingerprints")
	}
}

func TestSerializeAndParseHeaders(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Content-Type", "text/html")
	headers.Set("X-Custom", "value")
	headers.Add("Set-Cookie", "a=1")
	headers.Add("Set-Cookie", "b=2")

	raw := serializeHeaders(headers)
	parsed := parseRawHeaders(raw)

	if parsed.Get("Content-Type") != "text/html" {
		t.Errorf("Content-Type: got %q, want %q", parsed.Get("Content-Type"), "text/html")
	}
	if parsed.Get("X-Custom") != "value" {
		t.Errorf("X-Custom: got %q, want %q", parsed.Get("X-Custom"), "value")
	}
	cookies := parsed.Values("Set-Cookie")
	if len(cookies) != 2 {
		t.Errorf("Set-Cookie count: got %d, want 2", len(cookies))
	}
}

func TestCanonicalizeURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"simple", "http://example.com/page", "http://example.com/page"},
		{"with fragment", "http://example.com/page#section", "http://example.com/page"},
		{"with query", "http://example.com/page?b=2&a=1", "http://example.com/page?a=1&b=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utils.CanonicalizeURL(tt.url, false)
			if got != tt.want {
				t.Errorf("CanonicalizeURL(%q): got %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsDownloadException(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"download timeout", scrapyerrors.ErrDownloadTimeout, true},
		{"connection refused", scrapyerrors.ErrConnectionRefused, true},
		{"download failed", scrapyerrors.ErrDownloadFailed, true},
		{"data loss", scrapyerrors.ErrResponseDataLoss, true},
		{"cannot resolve host", scrapyerrors.ErrCannotResolveHost, true},
		{"drop item", scrapyerrors.ErrDropItem, false},
		{"close spider", scrapyerrors.ErrCloseSpider, false},
		{"ignore request", scrapyerrors.ErrIgnoreRequest, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDownloadException(tt.err)
			if got != tt.want {
				t.Errorf("isDownloadException(%v): got %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestHasFlag(t *testing.T) {
	flags := []string{"cached", "compressed"}
	if !hasFlag(flags, "cached") {
		t.Error("expected true for 'cached'")
	}
	if hasFlag(flags, "missing") {
		t.Error("expected false for 'missing'")
	}
	if hasFlag(nil, "cached") {
		t.Error("expected false for nil flags")
	}
}

// ============================================================================
// RFC2616 新鲜度计算测试
// ============================================================================

func TestRFC2616_ComputeFreshnessLifetime_Expires(t *testing.T) {
	policy := NewRFC2616Policy()
	now := time.Now()

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Date", now.UTC().Format(http.TimeFormat))
	resp.Headers.Set("Expires", now.Add(time.Hour).UTC().Format(http.TimeFormat))

	lifetime := policy.computeFreshnessLifetime(resp, now)
	// 允许 1 秒误差
	if lifetime < 59*time.Minute || lifetime > 61*time.Minute {
		t.Errorf("freshness lifetime: got %v, want ~1h", lifetime)
	}
}

func TestRFC2616_ComputeFreshnessLifetime_LastModified(t *testing.T) {
	policy := NewRFC2616Policy()
	now := time.Now()

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Date", now.UTC().Format(http.TimeFormat))
	resp.Headers.Set("Last-Modified", now.Add(-10*time.Hour).UTC().Format(http.TimeFormat))

	lifetime := policy.computeFreshnessLifetime(resp, now)
	// 启发式：(date - lastModified) / 10 = 1h
	if lifetime < 59*time.Minute || lifetime > 61*time.Minute {
		t.Errorf("freshness lifetime: got %v, want ~1h", lifetime)
	}
}

func TestRFC2616_ComputeFreshnessLifetime_PermanentRedirect(t *testing.T) {
	policy := NewRFC2616Policy()
	now := time.Now()

	resp := mustNewResponse("http://example.com", 301)
	lifetime := policy.computeFreshnessLifetime(resp, now)

	expected := time.Duration(maxAge) * time.Second
	if lifetime != expected {
		t.Errorf("freshness lifetime: got %v, want %v", lifetime, expected)
	}
}

func TestRFC2616_ComputeCurrentAge(t *testing.T) {
	policy := NewRFC2616Policy()
	now := time.Now()

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Date", now.Add(-30*time.Second).UTC().Format(http.TimeFormat))

	age := policy.computeCurrentAge(resp, now)
	if age < 29*time.Second || age > 31*time.Second {
		t.Errorf("current age: got %v, want ~30s", age)
	}
}

func TestRFC2616_ComputeCurrentAge_WithAgeHeader(t *testing.T) {
	policy := NewRFC2616Policy()
	now := time.Now()

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Date", now.Add(-10*time.Second).UTC().Format(http.TimeFormat))
	resp.Headers.Set("Age", "60")

	age := policy.computeCurrentAge(resp, now)
	if age < 59*time.Second || age > 61*time.Second {
		t.Errorf("current age: got %v, want ~60s (Age header takes precedence)", age)
	}
}

// ============================================================================
// 额外覆盖率测试
// ============================================================================

func TestFilesystemCacheStorage_EmptyBody(t *testing.T) {
	storage, _ := newTestStorage(t)
	defer storage.Close()

	req := mustNewRequest("http://example.com/empty")
	resp := mustNewResponse("http://example.com/empty", 204)
	resp.Body = nil

	if err := storage.StoreResponse(req, resp); err != nil {
		t.Fatalf("store response: %v", err)
	}

	cached, err := storage.RetrieveResponse(req)
	if err != nil {
		t.Fatalf("retrieve response: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached response")
	}
	if len(cached.Body) != 0 {
		t.Errorf("body length: got %d, want 0", len(cached.Body))
	}
}

func TestFilesystemCacheStorage_NilHeaders(t *testing.T) {
	raw := serializeHeaders(nil)
	if raw != nil {
		t.Errorf("expected nil for nil headers, got %v", raw)
	}

	parsed := parseRawHeaders(nil)
	if len(parsed) != 0 {
		t.Errorf("expected empty headers for nil input, got %v", parsed)
	}
}

func TestMiddleware_ProcessRequest_UncacheableScheme(t *testing.T) {
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir)
	policy := NewDummyPolicy()
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewHttpCacheMiddleware(policy, storage, sc)
	if err := mw.Open("test_spider"); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("file:///tmp/test.html")

	resp, err := mw.ProcessRequest(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response for uncacheable scheme")
	}

	// 应该设置 _dont_cache Meta
	if _, ok := req.GetMeta("_dont_cache"); !ok {
		t.Error("expected _dont_cache meta to be set")
	}
}

func TestMiddleware_NilStats(t *testing.T) {
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir)
	policy := NewDummyPolicy()
	// 传入 nil stats，应该使用 DummyCollector
	mw := NewHttpCacheMiddleware(policy, storage, nil)
	if err := mw.Open("test_spider"); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer mw.Close()

	ctx := context.Background()
	req := mustNewRequest("http://example.com/nilstats")
	_, err := mw.ProcessRequest(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMiddleware_WithMiddlewareLogger(t *testing.T) {
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(dir)
	policy := NewDummyPolicy()
	sc := stats.NewMemoryCollector(false, nil)
	customLogger := slog.Default()
	mw := NewHttpCacheMiddleware(policy, storage, sc, WithMiddlewareLogger(customLogger))
	if mw.logger != customLogger {
		t.Error("expected custom logger to be set")
	}
}

func TestRFC2616Policy_ShouldCacheRequest_NilURL(t *testing.T) {
	policy := NewRFC2616Policy()
	req := &shttp.Request{Method: "GET", Headers: make(http.Header)}
	if policy.ShouldCacheRequest(req) {
		t.Error("expected false for nil URL")
	}
}

func TestRFC2616Policy_ShouldCacheResponse_Status203WithValidator(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 203)
	resp.Headers.Set("ETag", `"abc"`)
	if !policy.ShouldCacheResponse(resp, req) {
		t.Error("expected 203 with ETag to be cached")
	}
}

func TestRFC2616Policy_ShouldCacheResponse_Status401WithValidator(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 401)
	resp.Headers.Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	if !policy.ShouldCacheResponse(resp, req) {
		t.Error("expected 401 with Last-Modified to be cached")
	}
}

func TestRFC2616Policy_ShouldCacheResponse_UncacheableStatus(t *testing.T) {
	policy := NewRFC2616Policy()
	req := mustNewRequest("http://example.com")
	resp := mustNewResponse("http://example.com", 403)
	if policy.ShouldCacheResponse(resp, req) {
		t.Error("expected 403 without hints to not be cached")
	}
}

func TestRFC2616Policy_ComputeFreshnessLifetime_InvalidExpires(t *testing.T) {
	policy := NewRFC2616Policy()
	now := time.Now()

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Date", now.UTC().Format(http.TimeFormat))
	resp.Headers.Set("Expires", "invalid-date")

	lifetime := policy.computeFreshnessLifetime(resp, now)
	if lifetime != 0 {
		t.Errorf("expected 0 for invalid Expires, got %v", lifetime)
	}
}

func TestRFC2616Policy_ComputeCurrentAge_InvalidAge(t *testing.T) {
	policy := NewRFC2616Policy()
	now := time.Now()

	resp := mustNewResponse("http://example.com", 200)
	resp.Headers.Set("Date", now.Add(-10*time.Second).UTC().Format(http.TimeFormat))
	resp.Headers.Set("Age", "not-a-number")

	age := policy.computeCurrentAge(resp, now)
	// 应该忽略无效的 Age 头，使用 Date 计算
	if age < 9*time.Second || age > 11*time.Second {
		t.Errorf("current age: got %v, want ~10s", age)
	}
}

func TestCacheControlDirectives_GetInt_Invalid(t *testing.T) {
	cc := parseCacheControl("max-age=abc")
	_, ok := cc.getInt("max-age")
	if ok {
		t.Error("expected false for invalid int value")
	}
}

func TestCacheControlDirectives_GetInt_Missing(t *testing.T) {
	cc := parseCacheControl("no-cache")
	_, ok := cc.getInt("max-age")
	if ok {
		t.Error("expected false for missing key")
	}
}

func TestCacheControlDirectives_GetInt_Negative(t *testing.T) {
	cc := parseCacheControl("max-age=-10")
	val, ok := cc.getInt("max-age")
	if !ok {
		t.Error("expected true for negative value")
	}
	if val != 0 {
		t.Errorf("expected 0 for negative max-age, got %d", val)
	}
}

func TestWithRFC2616IgnoreSchemes(t *testing.T) {
	policy := NewRFC2616Policy(WithRFC2616IgnoreSchemes([]string{"ftp", "file"}))
	req := mustNewRequest("ftp://example.com/file")
	if policy.ShouldCacheRequest(req) {
		t.Error("expected ftp to be ignored")
	}
}

func TestMiddleware_ProcessResponse_StoreError(t *testing.T) {
	// 使用只读目录模拟存储错误
	dir := t.TempDir()
	storage := NewFilesystemCacheStorage(filepath.Join(dir, "readonly"))
	policy := NewDummyPolicy()
	sc := stats.NewMemoryCollector(false, nil)
	mw := NewHttpCacheMiddleware(policy, storage, sc)
	if err := mw.Open("test_spider"); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer mw.Close()

	// 创建只读目录
	readonlyDir := filepath.Join(dir, "readonly", "test_spider")
	os.MkdirAll(readonlyDir, 0o755)
	os.Chmod(readonlyDir, 0o444)
	defer os.Chmod(readonlyDir, 0o755)

	ctx := context.Background()
	req := mustNewRequest("http://example.com/storeerror")
	resp := mustNewResponse("http://example.com/storeerror", 200)

	// 不应该 panic，应该记录警告
	result, err := mw.ProcessResponse(ctx, req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resp {
		t.Error("expected same response even on store error")
	}
}
