package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// 测试用 Spider
// ============================================================================

// testSpider 是用于测试的简单 Spider。
type testSpider struct {
	spider.Base
}

func newTestSpider() spider.Spider {
	return &testSpider{
		Base: spider.Base{
			SpiderName: "test_spider",
			StartURLs:  []string{},
		},
	}
}

func (s *testSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return nil, nil
}

// ============================================================================
// Registry 测试
// ============================================================================

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	r.Register("test", newTestSpider)

	if !r.Has("test") {
		t.Fatal("expected spider 'test' to be registered")
	}
	if r.Has("nonexistent") {
		t.Fatal("expected spider 'nonexistent' to not be registered")
	}
	if r.Len() != 1 {
		t.Fatalf("expected 1 registered spider, got %d", r.Len())
	}
}

func TestRegistry_Register_PanicOnEmpty(t *testing.T) {
	r := NewRegistry()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty name")
		}
	}()
	r.Register("", newTestSpider)
}

func TestRegistry_Register_PanicOnNilFactory(t *testing.T) {
	r := NewRegistry()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil factory")
		}
	}()
	r.Register("test", nil)
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.Register("charlie", newTestSpider)
	r.Register("alpha", newTestSpider)
	r.Register("bravo", newTestSpider)

	names := r.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	// 应按字母排序
	if names[0] != "alpha" || names[1] != "bravo" || names[2] != "charlie" {
		t.Fatalf("expected sorted names, got %v", names)
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	r.Register("test", newTestSpider)
	r.Unregister("test")

	if r.Has("test") {
		t.Fatal("expected spider 'test' to be unregistered")
	}
	if r.Len() != 0 {
		t.Fatalf("expected 0 registered spiders, got %d", r.Len())
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	r.Register("test", newTestSpider)

	factory, _, ok := r.Get("test")
	if !ok {
		t.Fatal("expected to find spider 'test'")
	}
	sp := factory()
	if sp.Name() != "test_spider" {
		t.Fatalf("expected spider name 'test_spider', got %q", sp.Name())
	}

	_, _, ok = r.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find spider 'nonexistent'")
	}
}

func TestRegistry_MustGet_Panic(t *testing.T) {
	r := NewRegistry()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on MustGet for unregistered spider")
		}
	}()
	r.MustGet("nonexistent")
}

func TestRegistry_RegisterWithConfigurator(t *testing.T) {
	r := NewRegistry()
	called := false
	r.Register("test", newTestSpider, func(c CrawlerConfig) {
		called = true
	})

	_, configurator, ok := r.Get("test")
	if !ok {
		t.Fatal("expected to find spider 'test'")
	}
	if configurator == nil {
		t.Fatal("expected configurator to be set")
	}
	// 调用 configurator 验证
	configurator(nil)
	if !called {
		t.Fatal("expected configurator to be called")
	}
}

func TestRegistry_Overwrite(t *testing.T) {
	r := NewRegistry()
	r.Register("test", func() spider.Spider {
		return &testSpider{Base: spider.Base{SpiderName: "v1"}}
	})
	r.Register("test", func() spider.Spider {
		return &testSpider{Base: spider.Base{SpiderName: "v2"}}
	})

	factory, _, _ := r.Get("test")
	sp := factory()
	if sp.Name() != "v2" {
		t.Fatalf("expected overwritten spider name 'v2', got %q", sp.Name())
	}
	if r.Len() != 1 {
		t.Fatalf("expected 1 registered spider after overwrite, got %d", r.Len())
	}
}

// ============================================================================
// Server 测试辅助
// ============================================================================

// newTestServer 创建一个用于测试的 Server，使用 httptest 内存服务器。
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()

	srv := NewServer(":0",
		WithRunner(crawler.NewRunner(
			crawler.WithOSSignalHandling(false),
		)),
	)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	ts := httptest.NewServer(mux)
	t.Cleanup(func() {
		ts.Close()
		srv.runner.Close()
	})

	return srv, ts
}

// injectRunningSpider 向 Server 注入一个模拟的运行中 Spider（用于测试 API 行为）。
func injectRunningSpider(s *Server, id, name string) {
	c := crawler.NewDefault()
	sp := newTestSpider()
	sc := stats.NewMemoryCollector(false, nil)
	sc.Open()
	sc.SetValue("item_scraped_count", 42)
	c.Stats = sc

	done := make(chan error, 1)

	s.mu.Lock()
	s.running[id] = &runningSpider{
		name:      name,
		id:        id,
		crawler:   c,
		spider:    sp,
		startTime: time.Now(),
		done:      done,
	}
	s.mu.Unlock()
}

// removeInjectedSpider 移除注入的模拟 Spider。
func removeInjectedSpider(s *Server, id string) {
	s.mu.Lock()
	delete(s.running, id)
	s.mu.Unlock()
}

// doRequest 发送 HTTP 请求并返回响应。
func doRequest(t *testing.T, method, url string, body string) (*http.Response, apiResponse) {
	t.Helper()

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	resp.Body.Close()

	return resp, result
}

// ============================================================================
// Handler 测试
// ============================================================================

func TestHandleListSpiders_Empty(t *testing.T) {
	_, ts := newTestServer(t)

	resp, result := doRequest(t, "GET", ts.URL+"/api/spiders", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if result.Code != http.StatusOK {
		t.Fatalf("expected code 200, got %d", result.Code)
	}

	data, ok := result.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", result.Data)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty array, got %d items", len(data))
	}
}

func TestHandleListSpiders_WithRegistered(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("quotes", newTestSpider)
	srv.Register("books", newTestSpider)

	resp, result := doRequest(t, "GET", ts.URL+"/api/spiders", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	data, ok := result.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", result.Data)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 spiders, got %d", len(data))
	}
}

func TestHandleListSpiders_WithRunningInstances(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("quotes", newTestSpider)
	injectRunningSpider(srv, "quotes-1", "quotes")
	defer removeInjectedSpider(srv, "quotes-1")

	_, result := doRequest(t, "GET", ts.URL+"/api/spiders", "")

	data, _ := result.Data.([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 spider, got %d", len(data))
	}
	m, _ := data[0].(map[string]any)
	if m["name"] != "quotes" {
		t.Fatalf("expected name 'quotes', got %v", m["name"])
	}
	if m["running_instances"].(float64) != 1 {
		t.Fatalf("expected 1 running instance, got %v", m["running_instances"])
	}
}

func TestHandleStartSpider_NotRegistered(t *testing.T) {
	_, ts := newTestServer(t)

	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/nonexistent/start", "")

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "not registered") {
		t.Fatalf("expected 'not registered' in message, got %q", result.Message)
	}
}

func TestHandleStartSpider_Success(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("test", newTestSpider)

	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/test/start", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if result.Message != "spider started" {
		t.Fatalf("expected message 'spider started', got %q", result.Message)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be object, got %T", result.Data)
	}
	if data["name"] != "test" {
		t.Fatalf("expected name 'test', got %v", data["name"])
	}
	if data["id"] == nil || data["id"] == "" {
		t.Fatal("expected non-empty id")
	}
}

func TestHandleStartSpider_MultipleInstances(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("test", newTestSpider)

	// 启动两个实例
	_, result1 := doRequest(t, "POST", ts.URL+"/api/spiders/test/start", "")
	_, result2 := doRequest(t, "POST", ts.URL+"/api/spiders/test/start", "")

	data1, _ := result1.Data.(map[string]any)
	data2, _ := result2.Data.(map[string]any)

	// 两个实例应有不同的 ID
	if data1["id"] == data2["id"] {
		t.Fatalf("expected different IDs, got %v and %v", data1["id"], data2["id"])
	}
}

func TestHandleStopSpider_NotRunning(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("test", newTestSpider)

	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/test/stop", "")

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "no running spider") {
		t.Fatalf("expected 'no running spider' in message, got %q", result.Message)
	}
}

func TestHandleStopSpider_ByName(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("quotes", newTestSpider)
	injectRunningSpider(srv, "quotes-1", "quotes")

	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/quotes/stop", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "stopped") {
		t.Fatalf("expected 'stopped' in message, got %q", result.Message)
	}
}

func TestHandleStopSpider_ById(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("quotes", newTestSpider)
	injectRunningSpider(srv, "quotes-1", "quotes")
	injectRunningSpider(srv, "quotes-2", "quotes")

	// 按 ID 停止特定实例
	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/quotes/stop?id=quotes-1", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "quotes-1") {
		t.Fatalf("expected 'quotes-1' in message, got %q", result.Message)
	}
}

func TestHandleStopSpider_ByIdNotFound(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("quotes", newTestSpider)

	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/quotes/stop?id=nonexistent", "")

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "not found") {
		t.Fatalf("expected 'not found' in message, got %q", result.Message)
	}
}

func TestHandleGetStats_NotRegistered(t *testing.T) {
	_, ts := newTestServer(t)

	resp, result := doRequest(t, "GET", ts.URL+"/api/spiders/nonexistent/stats", "")

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "not registered") {
		t.Fatalf("expected 'not registered' in message, got %q", result.Message)
	}
}

func TestHandleGetStats_NoRunning(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("test", newTestSpider)

	resp, result := doRequest(t, "GET", ts.URL+"/api/spiders/test/stats", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	data, ok := result.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", result.Data)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty array, got %d items", len(data))
	}
}

func TestHandleGetStats_WithRunning(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("quotes", newTestSpider)
	injectRunningSpider(srv, "quotes-1", "quotes")
	defer removeInjectedSpider(srv, "quotes-1")

	resp, result := doRequest(t, "GET", ts.URL+"/api/spiders/quotes/stats", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	data, ok := result.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", result.Data)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 stats entry, got %d", len(data))
	}

	entry, ok := data[0].(map[string]any)
	if !ok {
		t.Fatalf("expected entry to be object, got %T", data[0])
	}
	if entry["name"] != "quotes" {
		t.Fatalf("expected name 'quotes', got %v", entry["name"])
	}
	if entry["id"] != "quotes-1" {
		t.Fatalf("expected id 'quotes-1', got %v", entry["id"])
	}
	// 验证统计数据存在
	statsMap, ok := entry["stats"].(map[string]any)
	if !ok {
		t.Fatalf("expected stats to be object, got %T", entry["stats"])
	}
	if statsMap["item_scraped_count"].(float64) != 42 {
		t.Fatalf("expected item_scraped_count 42, got %v", statsMap["item_scraped_count"])
	}
}

func TestHandleHealth(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("test", newTestSpider)

	resp, result := doRequest(t, "GET", ts.URL+"/api/health", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be object, got %T", result.Data)
	}
	if data["status"] != "healthy" {
		t.Fatalf("expected status 'healthy', got %v", data["status"])
	}
	if data["registered_spiders"].(float64) != 1 {
		t.Fatalf("expected 1 registered spider, got %v", data["registered_spiders"])
	}
}

func TestHandleHealth_WithRunning(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("test", newTestSpider)
	injectRunningSpider(srv, "test-1", "test")
	defer removeInjectedSpider(srv, "test-1")

	_, result := doRequest(t, "GET", ts.URL+"/api/health", "")

	data, _ := result.Data.(map[string]any)
	if data["running_spiders"].(float64) != 1 {
		t.Fatalf("expected 1 running spider, got %v", data["running_spiders"])
	}
}

// ============================================================================
// 声明式注册端点测试（预留接口）
// ============================================================================

func TestHandleRegisterSpider_NotImplemented(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{
		"name": "quotes",
		"start_urls": ["https://quotes.toscrape.com"],
		"allowed_domains": ["quotes.toscrape.com"],
		"rules": [
			{
				"link_extractor": {
					"allow": ["/page/\\d+"],
					"restrict_css": ["li.next a"]
				},
				"follow": true
			}
		],
		"item_schemas": {
			"parse_detail": {
				"title": {"css": "h1::text"},
				"url": {"value": "_response_url"}
			}
		}
	}`

	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/register", body)

	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("expected status 501, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "not yet implemented") {
		t.Fatalf("expected 'not yet implemented' in message, got %q", result.Message)
	}

	// 验证返回了解析后的摘要信息
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be object, got %T", result.Data)
	}
	if data["name"] != "quotes" {
		t.Fatalf("expected name 'quotes', got %v", data["name"])
	}
	if data["rules_count"].(float64) != 1 {
		t.Fatalf("expected 1 rule, got %v", data["rules_count"])
	}
	if data["schemas_count"].(float64) != 1 {
		t.Fatalf("expected 1 schema, got %v", data["schemas_count"])
	}
}

func TestHandleRegisterSpider_MissingName(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"start_urls": ["https://example.com"]}`
	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/register", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "name is required") {
		t.Fatalf("expected 'name is required' in message, got %q", result.Message)
	}
}

func TestHandleRegisterSpider_MissingStartURLs(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"name": "test"}`
	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/register", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "start_url is required") {
		t.Fatalf("expected 'start_url is required' in message, got %q", result.Message)
	}
}

func TestHandleRegisterSpider_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)

	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/register", "{invalid}")

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "invalid request body") {
		t.Fatalf("expected 'invalid request body' in message, got %q", result.Message)
	}
}

// ============================================================================
// 注销端点测试
// ============================================================================

func TestHandleUnregisterSpider_Success(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Register("quotes", newTestSpider)

	resp, result := doRequest(t, "DELETE", ts.URL+"/api/spiders/quotes", "")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "unregistered") {
		t.Fatalf("expected 'unregistered' in message, got %q", result.Message)
	}

	// 验证已从注册表中移除
	if srv.Registry().Has("quotes") {
		t.Fatal("expected spider 'quotes' to be unregistered")
	}
}

func TestHandleUnregisterSpider_NotFound(t *testing.T) {
	_, ts := newTestServer(t)

	resp, result := doRequest(t, "DELETE", ts.URL+"/api/spiders/nonexistent", "")

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
	if !strings.Contains(result.Message, "not registered") {
		t.Fatalf("expected 'not registered' in message, got %q", result.Message)
	}
}

// ============================================================================
// Server 生命周期测试
// ============================================================================

func TestServer_ListenAndServe(t *testing.T) {
	srv := NewServer(":0")
	srv.Register("test", newTestSpider)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	// 取消 context 触发关闭
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}

func TestServer_Register_Convenience(t *testing.T) {
	srv := NewServer(":0")
	srv.Register("test", newTestSpider)

	if !srv.Registry().Has("test") {
		t.Fatal("expected spider 'test' to be registered via convenience method")
	}
}

func TestServer_DefaultOptions(t *testing.T) {
	srv := NewServer(":8080")
	if srv.logger == nil {
		t.Fatal("expected default logger")
	}
	if srv.registry == nil {
		t.Fatal("expected default registry")
	}
	if srv.runner == nil {
		t.Fatal("expected default runner")
	}
}

// ============================================================================
// formatStopMessage 测试
// ============================================================================

func TestFormatStopMessage(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		expected string
	}{
		{"quotes", 1, `stopped 1 instance of spider "quotes"`},
		{"books", 3, `stopped 3 instances of spider "books"`},
	}

	for _, tt := range tests {
		got := formatStopMessage(tt.name, tt.count)
		if got != tt.expected {
			t.Errorf("formatStopMessage(%q, %d) = %q, want %q", tt.name, tt.count, got, tt.expected)
		}
	}
}

// ============================================================================
// 并发安全测试
// ============================================================================

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	done := make(chan struct{})

	// 并发注册
	go func() {
		for i := 0; i < 100; i++ {
			r.Register("spider_"+string(rune('a'+i%26)), newTestSpider)
		}
		done <- struct{}{}
	}()

	// 并发读取
	go func() {
		for i := 0; i < 100; i++ {
			r.Names()
			r.Len()
			r.Has("spider_a")
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}

// ============================================================================
// writeJSON 测试
// ============================================================================

func TestWriteJSON_ContentType(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected Content-Type 'application/json', got %q", ct)
	}
}

// ============================================================================
// 端到端集成测试：启动 → 完成 → 清理
// ============================================================================

func TestIntegration_StartAndAutoCleanup(t *testing.T) {
	srv, ts := newTestServer(t)
	// 使用 testSpider（无 StartURLs，会立即完成）
	srv.Register("quick", newTestSpider)

	// 启动 Spider
	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/quick/start", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	data, _ := result.Data.(map[string]any)
	id := data["id"].(string)
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	// 等待 Spider 完成并被自动清理
	time.Sleep(500 * time.Millisecond)

	// 验证 Spider 已从 running 列表中移除
	srv.mu.RLock()
	_, exists := srv.running[id]
	srv.mu.RUnlock()
	if exists {
		t.Fatal("expected spider to be removed from running list after completion")
	}
}
