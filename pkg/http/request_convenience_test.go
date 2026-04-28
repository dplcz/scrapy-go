package http

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ============================================================================
// NewJSONRequest 测试
// ============================================================================

func TestNewJSONRequest(t *testing.T) {
	data := map[string]any{"key": "value", "count": 42}
	req, err := NewJSONRequest("https://api.example.com/data", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 默认 POST
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}

	// Content-Type
	if ct := req.Headers.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}

	// Accept
	if accept := req.Headers.Get("Accept"); accept != "application/json, text/javascript, */*; q=0.01" {
		t.Errorf("unexpected Accept: %s", accept)
	}

	// Body 是有效 JSON
	var decoded map[string]any
	if err := json.Unmarshal(req.Body, &decoded); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if decoded["key"] != "value" {
		t.Errorf("unexpected key: %v", decoded["key"])
	}
}

func TestNewJSONRequestWithMethodOverride(t *testing.T) {
	data := map[string]string{"name": "test"}
	req, err := NewJSONRequest("https://api.example.com/data", data,
		WithMethod("PUT"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "PUT" {
		t.Errorf("expected PUT, got %s", req.Method)
	}
}

func TestNewJSONRequestWithStruct(t *testing.T) {
	type Payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	data := Payload{Name: "test", Count: 5}
	req, err := NewJSONRequest("https://api.example.com/data", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded Payload
	if err := json.Unmarshal(req.Body, &decoded); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if decoded.Name != "test" || decoded.Count != 5 {
		t.Errorf("unexpected decoded: %+v", decoded)
	}
}

func TestNewJSONRequestInvalidURL(t *testing.T) {
	_, err := NewJSONRequest("://invalid", map[string]string{})
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestNewJSONRequestUnmarshalableData(t *testing.T) {
	// channel 类型无法 JSON 序列化
	_, err := NewJSONRequest("https://example.com", make(chan int))
	if err == nil {
		t.Error("expected error for unmarshalable data")
	}
}

func TestMustNewJSONRequest(t *testing.T) {
	req := MustNewJSONRequest("https://api.example.com/data", map[string]string{"k": "v"})
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}

	// panic 测试
	defer func() {
		if r := recover(); r == nil {
			t.Error("should panic for unmarshalable data")
		}
	}()
	MustNewJSONRequest("https://example.com", make(chan int))
}

// ============================================================================
// NewFormRequest 测试
// ============================================================================

func TestNewFormRequestPOST(t *testing.T) {
	formdata := map[string][]string{
		"user": {"admin"},
		"pass": {"secret"},
	}
	req, err := NewFormRequest("https://example.com/login", formdata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 默认 POST
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}

	// Content-Type
	if ct := req.Headers.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("expected application/x-www-form-urlencoded, got %s", ct)
	}

	// Body 包含表单数据
	body := string(req.Body)
	if body == "" {
		t.Error("body should not be empty")
	}
	// 验证包含关键字段
	if !containsParam(body, "user", "admin") || !containsParam(body, "pass", "secret") {
		t.Errorf("body missing form fields: %s", body)
	}
}

func TestNewFormRequestGET(t *testing.T) {
	formdata := map[string][]string{
		"q":    {"golang"},
		"page": {"1"},
	}
	req, err := NewFormRequest("https://example.com/search", formdata,
		WithMethod("GET"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "GET" {
		t.Errorf("expected GET, got %s", req.Method)
	}

	// Body 应为空（数据在 URL 查询参数中）
	if len(req.Body) != 0 {
		t.Errorf("GET request body should be empty, got: %s", string(req.Body))
	}

	// URL 应包含查询参数
	query := req.URL.Query()
	if query.Get("q") != "golang" {
		t.Errorf("expected q=golang, got %s", query.Get("q"))
	}
	if query.Get("page") != "1" {
		t.Errorf("expected page=1, got %s", query.Get("page"))
	}
}

func TestNewFormRequestGETWithExistingQuery(t *testing.T) {
	formdata := map[string][]string{
		"q": {"test"},
	}
	req, err := NewFormRequest("https://example.com/search?lang=go", formdata,
		WithMethod("GET"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	query := req.URL.Query()
	if query.Get("lang") != "go" {
		t.Errorf("expected lang=go, got %s", query.Get("lang"))
	}
	if query.Get("q") != "test" {
		t.Errorf("expected q=test, got %s", query.Get("q"))
	}
}

func TestNewFormRequestInvalidURL(t *testing.T) {
	_, err := NewFormRequest("://invalid", map[string][]string{})
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestMustNewFormRequest(t *testing.T) {
	req := MustNewFormRequest("https://example.com/login",
		map[string][]string{"user": {"admin"}},
	)
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
}

// ============================================================================
// 便捷 Option 测试
// ============================================================================

func TestWithRawBody(t *testing.T) {
	body := []byte("raw content")
	req := MustNewRequest("https://example.com", WithRawBody(body))
	if string(req.Body) != "raw content" {
		t.Errorf("unexpected body: %s", string(req.Body))
	}
}

func TestWithBasicAuth(t *testing.T) {
	req := MustNewRequest("https://example.com", WithBasicAuth("user", "pass"))
	if req.Meta["http_user"] != "user" {
		t.Errorf("expected http_user=user, got %v", req.Meta["http_user"])
	}
	if req.Meta["http_pass"] != "pass" {
		t.Errorf("expected http_pass=pass, got %v", req.Meta["http_pass"])
	}
}

func TestWithUserAgent(t *testing.T) {
	req := MustNewRequest("https://example.com", WithUserAgent("MyBot/1.0"))
	if ua := req.Headers.Get("User-Agent"); ua != "MyBot/1.0" {
		t.Errorf("expected MyBot/1.0, got %s", ua)
	}
}

func TestWithFormData(t *testing.T) {
	// POST 模式
	req := MustNewRequest("https://example.com",
		WithMethod("POST"),
		WithFormData(map[string][]string{"key": {"value"}}),
	)
	if ct := req.Headers.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("expected application/x-www-form-urlencoded, got %s", ct)
	}
	if string(req.Body) == "" {
		t.Error("body should not be empty for POST form")
	}
}

func TestWithFormDataGET(t *testing.T) {
	req := MustNewRequest("https://example.com",
		WithFormData(map[string][]string{"q": {"test"}}),
	)
	// 默认 GET，formdata 应追加到 URL
	query := req.URL.Query()
	if query.Get("q") != "test" {
		t.Errorf("expected q=test, got %s", query.Get("q"))
	}
}

// ============================================================================
// NoCallback 测试
// ============================================================================

func TestNoCallback(t *testing.T) {
	req := MustNewRequest("https://example.com",
		WithCallback(NoCallback),
	)

	if !IsNoCallback(req.Callback) {
		t.Error("expected NoCallback to be recognized")
	}
}

func TestIsNoCallbackWithNormalCallback(t *testing.T) {
	if IsNoCallback(nil) {
		t.Error("nil should not be NoCallback")
	}
	if IsNoCallback("not a callback") {
		t.Error("string should not be NoCallback")
	}
}

func TestNoCallbackWithHeaders(t *testing.T) {
	req := MustNewRequest("https://example.com",
		WithCallback(NoCallback),
		WithHeader("X-Custom", "value"),
	)

	if !IsNoCallback(req.Callback) {
		t.Error("expected NoCallback to be recognized")
	}
	if req.Headers.Get("X-Custom") != "value" {
		t.Error("headers should be preserved with NoCallback")
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

func containsParam(body, key, value string) bool {
	// 简单检查 URL 编码的参数
	return contains(body, key+"="+value)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// 确保 http.Header 类型被使用（避免 import 未使用警告）
var _ = http.Header{}
