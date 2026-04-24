package http

import (
	"net/http"
	"testing"
)

func TestNewRequest(t *testing.T) {
	// 正常创建
	req, err := NewRequest("https://example.com/page?q=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.URL.String() != "https://example.com/page?q=test" {
		t.Errorf("unexpected URL: %s", req.URL.String())
	}
	if req.Method != "GET" {
		t.Errorf("unexpected method: %s", req.Method)
	}
	if req.Encoding != "utf-8" {
		t.Errorf("unexpected encoding: %s", req.Encoding)
	}

	// 无 scheme
	_, err = NewRequest("example.com/page")
	if err == nil {
		t.Error("should return error for URL without scheme")
	}

	// 无效 URL
	_, err = NewRequest("://invalid")
	if err == nil {
		t.Error("should return error for invalid URL")
	}
}

func TestNewRequestWithOptions(t *testing.T) {
	req, err := NewRequest("https://example.com",
		WithMethod("POST"),
		WithHeader("Content-Type", "application/json"),
		WithBody([]byte(`{"key":"value"}`)),
		WithPriority(10),
		WithDontFilter(true),
		WithMeta(map[string]any{"depth": 1}),
		WithFlags("cached"),
		WithEncoding("gbk"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "POST" {
		t.Errorf("unexpected method: %s", req.Method)
	}
	if req.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("unexpected Content-Type: %s", req.Headers.Get("Content-Type"))
	}
	if string(req.Body) != `{"key":"value"}` {
		t.Errorf("unexpected body: %s", string(req.Body))
	}
	if req.Priority != 10 {
		t.Errorf("unexpected priority: %d", req.Priority)
	}
	if !req.DontFilter {
		t.Error("DontFilter should be true")
	}
	if req.Meta["depth"] != 1 {
		t.Errorf("unexpected meta: %v", req.Meta)
	}
	if len(req.Flags) != 1 || req.Flags[0] != "cached" {
		t.Errorf("unexpected flags: %v", req.Flags)
	}
	if req.Encoding != "gbk" {
		t.Errorf("unexpected encoding: %s", req.Encoding)
	}
}

func TestMustNewRequest(t *testing.T) {
	// 正常创建
	req := MustNewRequest("https://example.com")
	if req.URL.String() != "https://example.com" {
		t.Errorf("unexpected URL: %s", req.URL.String())
	}

	// panic 测试
	defer func() {
		if r := recover(); r == nil {
			t.Error("should panic for invalid URL")
		}
	}()
	MustNewRequest("://invalid")
}

func TestRequestString(t *testing.T) {
	req := MustNewRequest("https://example.com/page")
	expected := "<GET https://example.com/page>"
	if req.String() != expected {
		t.Errorf("unexpected string: %s, expected: %s", req.String(), expected)
	}

	req2 := MustNewRequest("https://example.com/api", WithMethod("POST"))
	expected2 := "<POST https://example.com/api>"
	if req2.String() != expected2 {
		t.Errorf("unexpected string: %s, expected: %s", req2.String(), expected2)
	}
}

func TestRequestCopy(t *testing.T) {
	original := MustNewRequest("https://example.com",
		WithMethod("POST"),
		WithHeader("X-Custom", "value"),
		WithBody([]byte("body")),
		WithMeta(map[string]any{"key": "value"}),
		WithFlags("flag1"),
		WithCbKwargs(map[string]any{"arg": 1}),
	)

	copied := original.Copy()

	// 验证值相同
	if copied.URL.String() != original.URL.String() {
		t.Error("URL should be equal")
	}
	if copied.Method != original.Method {
		t.Error("Method should be equal")
	}

	// 验证深拷贝（修改拷贝不影响原始）
	copied.Headers.Set("X-Custom", "modified")
	if original.Headers.Get("X-Custom") != "value" {
		t.Error("modifying copy should not affect original headers")
	}

	copied.Meta["key"] = "modified"
	if original.Meta["key"] != "value" {
		t.Error("modifying copy should not affect original meta")
	}

	copied.Body[0] = 'X'
	if original.Body[0] != 'b' {
		t.Error("modifying copy should not affect original body")
	}
}

func TestRequestReplace(t *testing.T) {
	original := MustNewRequest("https://example.com",
		WithPriority(5),
	)

	replaced := original.Replace(
		WithPriority(10),
		WithMethod("POST"),
	)

	// 原始不变
	if original.Priority != 5 {
		t.Error("original priority should not change")
	}
	if original.Method != "GET" {
		t.Error("original method should not change")
	}

	// 替换后的值
	if replaced.Priority != 10 {
		t.Errorf("unexpected priority: %d", replaced.Priority)
	}
	if replaced.Method != "POST" {
		t.Errorf("unexpected method: %s", replaced.Method)
	}

	// 保留原始的 URL
	if replaced.URL.String() != original.URL.String() {
		t.Error("URL should be preserved")
	}
}

func TestRequestMeta(t *testing.T) {
	req := MustNewRequest("https://example.com")

	// 初始 Meta 为 nil
	v, ok := req.GetMeta("key")
	if ok || v != nil {
		t.Error("should return nil for non-existent key")
	}

	// SetMeta 自动初始化
	req.SetMeta("key", "value")
	v, ok = req.GetMeta("key")
	if !ok || v != "value" {
		t.Errorf("unexpected meta value: %v", v)
	}
}

func TestResponseBasic(t *testing.T) {
	resp, err := NewResponse("https://example.com", 200,
		WithResponseBody([]byte("<html>Hello</html>")),
		WithResponseHeaders(http.Header{"Content-Type": {"text/html"}}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != 200 {
		t.Errorf("unexpected status: %d", resp.Status)
	}
	if resp.Text() != "<html>Hello</html>" {
		t.Errorf("unexpected text: %s", resp.Text())
	}
	if resp.Headers.Get("Content-Type") != "text/html" {
		t.Errorf("unexpected Content-Type: %s", resp.Headers.Get("Content-Type"))
	}
}

func TestResponseString(t *testing.T) {
	resp := MustNewResponse("https://example.com", 404)
	expected := "<404 https://example.com>"
	if resp.String() != expected {
		t.Errorf("unexpected string: %s, expected: %s", resp.String(), expected)
	}
}

func TestResponseURLJoin(t *testing.T) {
	resp := MustNewResponse("https://example.com/page/1", 200)

	// 相对 URL
	abs, err := resp.URLJoin("/page/2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if abs != "https://example.com/page/2" {
		t.Errorf("unexpected URL: %s", abs)
	}

	// 相对路径
	abs2, err := resp.URLJoin("../other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if abs2 != "https://example.com/other" {
		t.Errorf("unexpected URL: %s", abs2)
	}

	// 绝对 URL
	abs3, err := resp.URLJoin("https://other.com/page")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if abs3 != "https://other.com/page" {
		t.Errorf("unexpected URL: %s", abs3)
	}
}

func TestResponseFollow(t *testing.T) {
	resp := MustNewResponse("https://example.com/page/1", 200)

	req, err := resp.Follow("/page/2", WithPriority(5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.URL.String() != "https://example.com/page/2" {
		t.Errorf("unexpected URL: %s", req.URL.String())
	}
	if req.Priority != 5 {
		t.Errorf("unexpected priority: %d", req.Priority)
	}
}

func TestResponseJSON(t *testing.T) {
	resp := MustNewResponse("https://api.example.com/data", 200,
		WithResponseBody([]byte(`{"name":"test","count":42}`)),
	)

	var data struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := resp.JSON(&data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.Name != "test" {
		t.Errorf("unexpected name: %s", data.Name)
	}
	if data.Count != 42 {
		t.Errorf("unexpected count: %d", data.Count)
	}
}

func TestResponseCopy(t *testing.T) {
	original := MustNewResponse("https://example.com", 200,
		WithResponseBody([]byte("body")),
		WithResponseHeaders(http.Header{"X-Custom": {"value"}}),
		WithResponseFlags("flag1"),
	)

	copied := original.Copy()

	// 验证深拷贝
	copied.Headers.Set("X-Custom", "modified")
	if original.Headers.Get("X-Custom") != "value" {
		t.Error("modifying copy should not affect original headers")
	}

	copied.Body[0] = 'X'
	if original.Body[0] != 'b' {
		t.Error("modifying copy should not affect original body")
	}
}

func TestResponseGetMeta(t *testing.T) {
	req := MustNewRequest("https://example.com",
		WithMeta(map[string]any{"depth": 3}),
	)
	resp := MustNewResponse("https://example.com", 200,
		WithRequest(req),
	)

	v, ok := resp.GetMeta("depth")
	if !ok || v != 3 {
		t.Errorf("unexpected meta value: %v", v)
	}

	// 无关联请求
	resp2 := MustNewResponse("https://example.com", 200)
	v2, ok2 := resp2.GetMeta("depth")
	if ok2 || v2 != nil {
		t.Error("should return nil for response without request")
	}
}
