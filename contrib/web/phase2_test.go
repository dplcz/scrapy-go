package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Store 测试
// ============================================================================

func TestStore_RecordStartAndFinish(t *testing.T) {
	store := NewStore()

	store.RecordStart("spider-1", "quotes", map[string]any{"page": 1})

	record, ok := store.GetRecord("spider-1")
	if !ok {
		t.Fatal("expected record to exist")
	}
	if record.SpiderName != "quotes" {
		t.Fatalf("expected spider_name 'quotes', got %q", record.SpiderName)
	}
	if record.Status != "running" {
		t.Fatalf("expected status 'running', got %q", record.Status)
	}
	if record.Args["page"] != 1 {
		t.Fatalf("expected args page=1, got %v", record.Args["page"])
	}

	// 完成
	store.RecordFinish("spider-1", map[string]any{"items": 42}, "")

	record, _ = store.GetRecord("spider-1")
	if record.Status != "finished" {
		t.Fatalf("expected status 'finished', got %q", record.Status)
	}
	if record.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
	if record.Stats["items"] != 42 {
		t.Fatalf("expected stats items=42, got %v", record.Stats["items"])
	}
}

func TestStore_RecordStop(t *testing.T) {
	store := NewStore()
	store.RecordStart("spider-2", "books", nil)
	store.RecordStop("spider-2", map[string]any{"pages": 10})

	record, _ := store.GetRecord("spider-2")
	if record.Status != "stopped" {
		t.Fatalf("expected status 'stopped', got %q", record.Status)
	}
}

func TestStore_RecordError(t *testing.T) {
	store := NewStore()
	store.RecordStart("spider-3", "news", nil)
	store.RecordFinish("spider-3", nil, "connection timeout")

	record, _ := store.GetRecord("spider-3")
	if record.Status != "error" {
		t.Fatalf("expected status 'error', got %q", record.Status)
	}
	if record.Error != "connection timeout" {
		t.Fatalf("expected error 'connection timeout', got %q", record.Error)
	}
}

func TestStore_GetRecordsBySpider(t *testing.T) {
	store := NewStore()
	store.RecordStart("q-1", "quotes", nil)
	store.RecordStart("b-1", "books", nil)
	store.RecordStart("q-2", "quotes", nil)

	records := store.GetRecordsBySpider("quotes", 10)
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	// 应按时间倒序
	if records[0].ID != "q-2" {
		t.Fatalf("expected first record id 'q-2', got %q", records[0].ID)
	}
}

func TestStore_GetRecentRecords(t *testing.T) {
	store := NewStore()
	store.RecordStart("a-1", "a", nil)
	store.RecordStart("b-1", "b", nil)
	store.RecordStart("c-1", "c", nil)

	records := store.GetRecentRecords(2)
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].ID != "c-1" {
		t.Fatalf("expected first record 'c-1', got %q", records[0].ID)
	}
}

func TestStore_MaxRecords(t *testing.T) {
	store := NewStore(WithMaxRecords(3))
	store.RecordStart("1", "a", nil)
	store.RecordStart("2", "a", nil)
	store.RecordStart("3", "a", nil)
	store.RecordStart("4", "a", nil)

	if store.Len() != 3 {
		t.Fatalf("expected 3 records, got %d", store.Len())
	}
	// 最旧的应被裁剪
	if _, ok := store.GetRecord("1"); ok {
		t.Fatal("expected record '1' to be trimmed")
	}
}

func TestStore_GetStats(t *testing.T) {
	store := NewStore()
	store.RecordStart("1", "a", nil)
	store.RecordFinish("1", nil, "")
	store.RecordStart("2", "b", nil)
	store.RecordFinish("2", nil, "error")
	store.RecordStart("3", "a", nil)

	stats := store.GetStats()
	if stats["total_runs"] != 3 {
		t.Fatalf("expected total_runs=3, got %v", stats["total_runs"])
	}
	if stats["finished"] != 1 {
		t.Fatalf("expected finished=1, got %v", stats["finished"])
	}
	if stats["errors"] != 1 {
		t.Fatalf("expected errors=1, got %v", stats["errors"])
	}
	if stats["running"] != 1 {
		t.Fatalf("expected running=1, got %v", stats["running"])
	}
}

// ============================================================================
// EventHub 测试
// ============================================================================

func TestEventHub_SubscribeAndBroadcast(t *testing.T) {
	hub := NewEventHub(nil)
	defer hub.Close()

	id, events, _ := hub.Subscribe()
	if id == 0 {
		t.Fatal("expected non-zero client id")
	}
	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", hub.ClientCount())
	}

	// 广播事件
	hub.Broadcast(&WSEvent{
		Type:      "test_event",
		Timestamp: time.Now(),
		Data:      map[string]any{"key": "value"},
	})

	select {
	case data := <-events:
		var event WSEvent
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatalf("failed to unmarshal event: %v", err)
		}
		if event.Type != "test_event" {
			t.Fatalf("expected type 'test_event', got %q", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	hub.Unsubscribe(id)
	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients after unsubscribe, got %d", hub.ClientCount())
	}
}

func TestEventHub_MultipleClients(t *testing.T) {
	hub := NewEventHub(nil)
	defer hub.Close()

	_, events1, _ := hub.Subscribe()
	_, events2, _ := hub.Subscribe()

	hub.Broadcast(&WSEvent{Type: "multi"})

	// 两个客户端都应收到
	select {
	case <-events1:
	case <-time.After(time.Second):
		t.Fatal("client 1 timeout")
	}
	select {
	case <-events2:
	case <-time.After(time.Second):
		t.Fatal("client 2 timeout")
	}
}

// ============================================================================
// SpiderSpec 测试
// ============================================================================

func TestSpiderSpec_Validate_Success(t *testing.T) {
	spec := &SpiderSpec{
		Name:      "test",
		StartURLs: []string{"https://example.com"},
		Rules: []RuleSpec{
			{
				LinkExtractor: LinkExtractorSpec{
					Allow: []string{`/page/\d+`},
				},
				Follow: boolPtr(true),
			},
		},
	}

	errs := spec.Validate()
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestSpiderSpec_Validate_MissingName(t *testing.T) {
	spec := &SpiderSpec{
		StartURLs: []string{"https://example.com"},
	}

	errs := spec.Validate()
	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, "name is required") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'name is required' error, got %v", errs)
	}
}

func TestSpiderSpec_Validate_InvalidRegex(t *testing.T) {
	spec := &SpiderSpec{
		Name:      "test",
		StartURLs: []string{"https://example.com"},
		Rules: []RuleSpec{
			{
				LinkExtractor: LinkExtractorSpec{
					Allow: []string{"[invalid"},
				},
			},
		},
	}

	errs := spec.Validate()
	if len(errs) == 0 {
		t.Fatal("expected validation errors for invalid regex")
	}
}

func TestSpiderSpec_Validate_InvalidURL(t *testing.T) {
	spec := &SpiderSpec{
		Name:      "test",
		StartURLs: []string{"not-a-url"},
	}

	errs := spec.Validate()
	if len(errs) == 0 {
		t.Fatal("expected validation errors for invalid URL")
	}
}

func TestSpiderSpec_Validate_MutuallyExclusiveExtractor(t *testing.T) {
	spec := &SpiderSpec{
		Name:      "test",
		StartURLs: []string{"https://example.com"},
		ItemSchemas: map[string]ItemSchema{
			"parse": {
				"field": {CSS: "h1", XPath: "//h1"},
			},
		},
	}

	errs := spec.Validate()
	if len(errs) == 0 {
		t.Fatal("expected validation errors for mutually exclusive css/xpath")
	}
}

func TestSpiderSpec_ToFactory(t *testing.T) {
	spec := &SpiderSpec{
		Name:      "test_spider",
		StartURLs: []string{"https://example.com"},
		Rules: []RuleSpec{
			{
				LinkExtractor: LinkExtractorSpec{
					Allow: []string{`/page/\d+`},
				},
				Follow: boolPtr(true),
			},
		},
	}

	factory := spec.ToFactory()
	spider := factory()

	if spider == nil {
		t.Fatal("expected non-nil spider")
	}
	if spider.Name() != "test_spider" {
		t.Fatalf("expected name 'test_spider', got %q", spider.Name())
	}
}

func TestSpiderSpec_ToSettings(t *testing.T) {
	spec := &SpiderSpec{
		Name:           "test",
		StartURLs:      []string{"https://example.com"},
		AllowedDomains: []string{"example.com"},
		Settings: map[string]any{
			"CONCURRENT_REQUESTS": 8,
		},
	}

	settings := spec.ToSettings()
	if settings["CONCURRENT_REQUESTS"] != 8 {
		t.Fatalf("expected CONCURRENT_REQUESTS=8, got %v", settings["CONCURRENT_REQUESTS"])
	}
	domains, ok := settings["ALLOWED_DOMAINS"].([]string)
	if !ok || len(domains) != 1 || domains[0] != "example.com" {
		t.Fatalf("expected ALLOWED_DOMAINS=[example.com], got %v", settings["ALLOWED_DOMAINS"])
	}
}

// ============================================================================
// History API 测试
// ============================================================================

func TestHandleGetHistory(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Store().RecordStart("q-1", "quotes", nil)
	srv.Store().RecordFinish("q-1", map[string]any{"items": 10}, "")
	srv.Store().RecordStart("q-2", "quotes", nil)

	resp, result := doRequest(t, "GET", ts.URL+"/api/history", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data, ok := result.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", result.Data)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 records, got %d", len(data))
	}
}

func TestHandleGetHistory_FilterBySpider(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Store().RecordStart("q-1", "quotes", nil)
	srv.Store().RecordStart("b-1", "books", nil)

	resp, result := doRequest(t, "GET", ts.URL+"/api/history?spider=quotes", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data, ok := result.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", result.Data)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 record, got %d", len(data))
	}
}

func TestHandleGetHistoryRecord(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Store().RecordStart("q-1", "quotes", nil)

	resp, result := doRequest(t, "GET", ts.URL+"/api/history/q-1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be object, got %T", result.Data)
	}
	if data["id"] != "q-1" {
		t.Fatalf("expected id 'q-1', got %v", data["id"])
	}
}

func TestHandleGetHistoryRecord_NotFound(t *testing.T) {
	_, ts := newTestServer(t)

	resp, _ := doRequest(t, "GET", ts.URL+"/api/history/nonexistent", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleGetHistoryStats(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.Store().RecordStart("1", "a", nil)
	srv.Store().RecordFinish("1", nil, "")

	resp, result := doRequest(t, "GET", ts.URL+"/api/history/stats", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be object, got %T", result.Data)
	}
	if data["total_runs"].(float64) != 1 {
		t.Fatalf("expected total_runs=1, got %v", data["total_runs"])
	}
}

// ============================================================================
// SSE 端点测试
// ============================================================================

func TestHandleSSE_Connection(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatalf("failed to connect to SSE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected Content-Type 'text/event-stream', got %q", ct)
	}

	// 读取初始连接事件
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil {
		t.Fatalf("failed to read SSE data: %v", err)
	}
	data := string(buf[:n])
	if !strings.Contains(data, "connected") {
		t.Fatalf("expected 'connected' event, got %q", data)
	}
}

func TestHandleSSEStats(t *testing.T) {
	_, ts := newTestServer(t)

	resp, result := doRequest(t, "GET", ts.URL+"/api/events/stats", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be object, got %T", result.Data)
	}
	if _, ok := data["connected_clients"]; !ok {
		t.Fatal("expected 'connected_clients' in response")
	}
}

// ============================================================================
// Dashboard 静态文件测试
// ============================================================================

func TestDashboard_ServeIndex(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("failed to get dashboard: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected Content-Type containing 'text/html', got %q", ct)
	}
}

// ============================================================================
// 声明式注册完整流程测试
// ============================================================================

func TestDeclarativeSpider_RegisterAndStart(t *testing.T) {
	_, ts := newTestServer(t)

	// 注册声明式爬虫
	body := `{
		"name": "declarative_test",
		"start_urls": ["https://example.com"],
		"rules": [
			{
				"link_extractor": {"allow": ["/page/"]},
				"follow": true
			}
		]
	}`

	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/register", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (message: %s)", resp.StatusCode, result.Message)
	}

	// 验证已注册
	resp, result = doRequest(t, "GET", ts.URL+"/api/spiders", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	spiders, ok := result.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", result.Data)
	}

	found := false
	for _, s := range spiders {
		spider := s.(map[string]any)
		if spider["name"] == "declarative_test" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected 'declarative_test' in spider list")
	}
}

func TestDeclarativeSpider_DuplicateName(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"name": "dup_test", "start_urls": ["https://example.com"]}`

	// 第一次注册
	resp, _ := doRequest(t, "POST", ts.URL+"/api/spiders/register", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for first register, got %d", resp.StatusCode)
	}

	// 第二次注册应冲突
	resp, result := doRequest(t, "POST", ts.URL+"/api/spiders/register", body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate, got %d (message: %s)", resp.StatusCode, result.Message)
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

func boolPtr(v bool) *bool { return &v }

// newTestServer 创建测试用的 Server 和 httptest.Server。
// 注意：此函数在 server_test.go 中已定义，这里仅在需要时使用。
func newTestServerForPhase2(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	srv := NewServer(":0")
	srv.Register("test", newTestSpider)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return srv, ts
}
