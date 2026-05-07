package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// ============================================================================
// CallbackRegistry 测试
// ============================================================================

func TestCallbackRegistryBasic(t *testing.T) {
	registry := NewCallbackRegistry()

	if registry.Len() != 0 {
		t.Errorf("expected empty registry, got %d", registry.Len())
	}

	// 注册回调
	cb := func() {}
	registry.Register("parse_detail", cb)

	if registry.Len() != 1 {
		t.Errorf("expected 1 callback, got %d", registry.Len())
	}

	// 查找
	found, ok := registry.Lookup("parse_detail")
	if !ok {
		t.Fatal("expected to find callback")
	}
	if found == nil {
		t.Fatal("found callback should not be nil")
	}

	// 查找不存在的
	_, ok = registry.Lookup("nonexistent")
	if ok {
		t.Error("should not find nonexistent callback")
	}
}

func TestCallbackRegistryErrback(t *testing.T) {
	registry := NewCallbackRegistry()

	eb := func() {}
	registry.RegisterErrback("handle_error", eb)

	if registry.ErrbackLen() != 1 {
		t.Errorf("expected 1 errback, got %d", registry.ErrbackLen())
	}

	found, ok := registry.LookupErrback("handle_error")
	if !ok {
		t.Fatal("expected to find errback")
	}
	if found == nil {
		t.Fatal("found errback should not be nil")
	}

	_, ok = registry.LookupErrback("nonexistent")
	if ok {
		t.Error("should not find nonexistent errback")
	}
}

func TestCallbackRegistryMustLookup(t *testing.T) {
	registry := NewCallbackRegistry()
	cb := func() {}
	registry.Register("parse", cb)

	// 正常查找
	found := registry.MustLookup("parse")
	if found == nil {
		t.Fatal("MustLookup should return callback")
	}

	// panic 测试
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustLookup should panic for nonexistent callback")
		}
	}()
	registry.MustLookup("nonexistent")
}

func TestCallbackRegistryMustLookupErrback(t *testing.T) {
	registry := NewCallbackRegistry()
	eb := func() {}
	registry.RegisterErrback("handle_error", eb)

	found := registry.MustLookupErrback("handle_error")
	if found == nil {
		t.Fatal("MustLookupErrback should return errback")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustLookupErrback should panic for nonexistent errback")
		}
	}()
	registry.MustLookupErrback("nonexistent")
}

func TestCallbackRegistryNames(t *testing.T) {
	registry := NewCallbackRegistry()
	registry.Register("parse_a", func() {})
	registry.Register("parse_b", func() {})
	registry.Register("parse_c", func() {})

	names := registry.Names()
	sort.Strings(names)
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "parse_a" || names[1] != "parse_b" || names[2] != "parse_c" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestCallbackRegistryErrbackNames(t *testing.T) {
	registry := NewCallbackRegistry()
	registry.RegisterErrback("err_a", func() {})
	registry.RegisterErrback("err_b", func() {})

	names := registry.ErrbackNames()
	sort.Strings(names)
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "err_a" || names[1] != "err_b" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestCallbackRegistryOverwrite(t *testing.T) {
	registry := NewCallbackRegistry()

	cb1 := "first"
	cb2 := "second"
	registry.Register("parse", cb1)
	registry.Register("parse", cb2)

	found, ok := registry.Lookup("parse")
	if !ok {
		t.Fatal("expected to find callback")
	}
	if found != "second" {
		t.Error("expected overwritten callback to be 'second'")
	}
}

// ============================================================================
// ToDict 测试
// ============================================================================

func TestToDictBasic(t *testing.T) {
	req := MustNewRequest("https://example.com/page?q=test",
		WithMethod("GET"),
	)

	d := req.ToDict("", "")

	if d["url"] != "https://example.com/page?q=test" {
		t.Errorf("unexpected url: %v", d["url"])
	}
	if d["method"] != "GET" {
		t.Errorf("unexpected method: %v", d["method"])
	}
}

func TestToDictFull(t *testing.T) {
	req := MustNewRequest("https://example.com/api",
		WithMethod("POST"),
		WithHeader("Content-Type", "application/json"),
		WithHeader("X-Custom", "value"),
		WithBody([]byte(`{"key":"value"}`)),
		WithCookies([]*http.Cookie{
			{Name: "session", Value: "abc123", Domain: ".example.com", Secure: true},
			{Name: "lang", Value: "en"},
		}),
		WithMeta(map[string]any{
			"depth":         1,
			"download_slot": "example.com",
		}),
		WithPriority(10),
		WithDontFilter(true),
		WithFlags("cached", "redirected"),
		WithCbKwargs(map[string]any{"page": 2}),
		WithEncoding("gbk"),
	)

	d := req.ToDict("parse_detail", "handle_error")

	// 验证所有字段
	if d["url"] != "https://example.com/api" {
		t.Errorf("unexpected url: %v", d["url"])
	}
	if d["method"] != "POST" {
		t.Errorf("unexpected method: %v", d["method"])
	}
	if d["callback"] != "parse_detail" {
		t.Errorf("unexpected callback: %v", d["callback"])
	}
	if d["errback"] != "handle_error" {
		t.Errorf("unexpected errback: %v", d["errback"])
	}
	if d["priority"] != 10 {
		t.Errorf("unexpected priority: %v", d["priority"])
	}
	if d["dont_filter"] != true {
		t.Errorf("unexpected dont_filter: %v", d["dont_filter"])
	}
	if d["encoding"] != "gbk" {
		t.Errorf("unexpected encoding: %v", d["encoding"])
	}

	// Headers
	headers, ok := d["headers"].(map[string][]string)
	if !ok {
		t.Fatal("headers should be map[string][]string")
	}
	if headers["Content-Type"][0] != "application/json" {
		t.Errorf("unexpected Content-Type: %v", headers["Content-Type"])
	}

	// Body (base64)
	body, ok := d["body"].(string)
	if !ok || body == "" {
		t.Fatal("body should be non-empty base64 string")
	}

	// Cookies
	cookies, ok := d["cookies"].([]map[string]any)
	if !ok || len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %v", d["cookies"])
	}
	if cookies[0]["name"] != "session" || cookies[0]["value"] != "abc123" {
		t.Errorf("unexpected cookie: %v", cookies[0])
	}
	if cookies[0]["domain"] != ".example.com" {
		t.Errorf("unexpected cookie domain: %v", cookies[0]["domain"])
	}
	if cookies[0]["secure"] != true {
		t.Errorf("expected secure cookie")
	}

	// Meta
	meta, ok := d["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta should be map[string]any")
	}
	if meta["depth"] != 1 {
		t.Errorf("unexpected meta depth: %v", meta["depth"])
	}

	// Flags
	flags, ok := d["flags"].([]string)
	if !ok || len(flags) != 2 {
		t.Fatalf("expected 2 flags, got %v", d["flags"])
	}

	// CbKwargs
	cbKwargs, ok := d["cb_kwargs"].(map[string]any)
	if !ok {
		t.Fatal("cb_kwargs should be map[string]any")
	}
	if cbKwargs["page"] != 2 {
		t.Errorf("unexpected cb_kwargs page: %v", cbKwargs["page"])
	}
}

func TestToDictSkipsNonSerializableMeta(t *testing.T) {
	ch := make(chan int)
	req := MustNewRequest("https://example.com",
		WithMeta(map[string]any{
			"depth":    1,
			"callback": func() {}, // 不可序列化
			"channel":  ch,        // 不可序列化
		}),
	)

	d := req.ToDict("", "")

	meta, ok := d["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta should exist")
	}
	if meta["depth"] != 1 {
		t.Errorf("serializable meta should be preserved")
	}
	if _, exists := meta["callback"]; exists {
		t.Error("non-serializable meta should be skipped")
	}
	if _, exists := meta["channel"]; exists {
		t.Error("non-serializable meta should be skipped")
	}
}

func TestToDictDefaultEncoding(t *testing.T) {
	req := MustNewRequest("https://example.com")
	d := req.ToDict("", "")

	// 默认 utf-8 不应出现在字典中
	if _, exists := d["encoding"]; exists {
		t.Error("default encoding 'utf-8' should not be in dict")
	}
}

func TestToDictEmptyCallbackErrback(t *testing.T) {
	req := MustNewRequest("https://example.com")
	d := req.ToDict("", "")

	if _, exists := d["callback"]; exists {
		t.Error("empty callback should not be in dict")
	}
	if _, exists := d["errback"]; exists {
		t.Error("empty errback should not be in dict")
	}
}

func TestToDictZeroPriority(t *testing.T) {
	req := MustNewRequest("https://example.com")
	d := req.ToDict("", "")

	if _, exists := d["priority"]; exists {
		t.Error("zero priority should not be in dict")
	}
}

// ============================================================================
// FromDict 测试
// ============================================================================

func TestFromDictBasic(t *testing.T) {
	d := map[string]any{
		"url":    "https://example.com/page",
		"method": "GET",
	}

	req, err := FromDict(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.String() != "https://example.com/page" {
		t.Errorf("unexpected url: %s", req.URL.String())
	}
	if req.Method != "GET" {
		t.Errorf("unexpected method: %s", req.Method)
	}
}

func TestFromDictFull(t *testing.T) {
	d := map[string]any{
		"url":         "https://example.com/api",
		"method":      "POST",
		"body":        "eyJrZXkiOiJ2YWx1ZSJ9", // base64 of {"key":"value"}
		"priority":    float64(10),            // JSON 数字默认为 float64
		"dont_filter": true,
		"encoding":    "gbk",
		"flags":       []any{"cached", "redirected"},
		"cb_kwargs":   map[string]any{"page": float64(2)},
		"headers": map[string]any{
			"Content-Type": []any{"application/json"},
			"X-Custom":     "single-value",
		},
		"cookies": []any{
			map[string]any{
				"name":   "session",
				"value":  "abc123",
				"domain": ".example.com",
				"secure": true,
			},
		},
		"meta": map[string]any{
			"depth": float64(1),
		},
	}

	req, err := FromDict(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.String() != "https://example.com/api" {
		t.Errorf("unexpected url: %s", req.URL.String())
	}
	if req.Method != "POST" {
		t.Errorf("unexpected method: %s", req.Method)
	}
	if string(req.Body) != `{"key":"value"}` {
		t.Errorf("unexpected body: %s", string(req.Body))
	}
	if req.Priority != 10 {
		t.Errorf("unexpected priority: %d", req.Priority)
	}
	if !req.DontFilter {
		t.Error("expected DontFilter=true")
	}
	if req.Encoding != "gbk" {
		t.Errorf("unexpected encoding: %s", req.Encoding)
	}
	if len(req.Flags) != 2 || req.Flags[0] != "cached" {
		t.Errorf("unexpected flags: %v", req.Flags)
	}
	if req.CbKwargs["page"] != float64(2) {
		t.Errorf("unexpected cb_kwargs: %v", req.CbKwargs)
	}
	if req.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("unexpected Content-Type: %s", req.Headers.Get("Content-Type"))
	}
	if len(req.Cookies) != 1 || req.Cookies[0].Name != "session" {
		t.Errorf("unexpected cookies: %v", req.Cookies)
	}
	if req.Cookies[0].Domain != ".example.com" {
		t.Errorf("unexpected cookie domain: %s", req.Cookies[0].Domain)
	}
	if !req.Cookies[0].Secure {
		t.Error("expected secure cookie")
	}
	if req.Meta["depth"] != float64(1) {
		t.Errorf("unexpected meta: %v", req.Meta)
	}
}

func TestFromDictWithRegistry(t *testing.T) {
	registry := NewCallbackRegistry()

	// 使用真实的回调函数类型
	type mockCallback = func(ctx context.Context, resp *Response) ([]any, error)
	type mockErrback = func(ctx context.Context, err error, req *Request) ([]any, error)

	var parseCalled bool
	var errbackCalled bool

	parseDetail := mockCallback(func(ctx context.Context, resp *Response) ([]any, error) {
		parseCalled = true
		return nil, nil
	})
	handleError := mockErrback(func(ctx context.Context, err error, req *Request) ([]any, error) {
		errbackCalled = true
		return nil, nil
	})

	registry.Register("parse_detail", parseDetail)
	registry.RegisterErrback("handle_error", handleError)

	d := map[string]any{
		"url":      "https://example.com",
		"callback": "parse_detail",
		"errback":  "handle_error",
	}

	req, err := FromDict(d, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Callback == nil {
		t.Fatal("callback should be restored from registry")
	}
	if req.Errback == nil {
		t.Fatal("errback should be restored from registry")
	}

	// 验证回调可以被调用
	cb, ok := req.Callback.(mockCallback)
	if !ok {
		t.Fatal("callback type mismatch")
	}
	cb(context.Background(), nil)
	if !parseCalled {
		t.Error("callback should have been called")
	}

	eb, ok := req.Errback.(mockErrback)
	if !ok {
		t.Fatal("errback type mismatch")
	}
	eb(context.Background(), nil, nil)
	if !errbackCalled {
		t.Error("errback should have been called")
	}
}

func TestFromDictWithUnregisteredCallback(t *testing.T) {
	registry := NewCallbackRegistry()

	d := map[string]any{
		"url":      "https://example.com",
		"callback": "unknown_method",
	}

	// 当 registry 非空但找不到对应回调时，应返回错误
	_, err := FromDict(d, registry)
	if err == nil {
		t.Fatal("expected error for unregistered callback")
	}
	if !strings.Contains(err.Error(), "unknown_method") {
		t.Errorf("error should mention callback name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not found in registry") {
		t.Errorf("error should mention 'not found in registry', got: %v", err)
	}
}

func TestFromDictMissingURL(t *testing.T) {
	d := map[string]any{
		"method": "GET",
	}

	_, err := FromDict(d, nil)
	if err == nil {
		t.Error("expected error for missing URL")
	}
}

func TestFromDictInvalidURL(t *testing.T) {
	d := map[string]any{
		"url": "://invalid",
	}

	_, err := FromDict(d, nil)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestFromDictInvalidBody(t *testing.T) {
	d := map[string]any{
		"url":  "https://example.com",
		"body": "not-valid-base64!!!",
	}

	_, err := FromDict(d, nil)
	if err == nil {
		t.Error("expected error for invalid base64 body")
	}
}

func TestFromDictDefaultValues(t *testing.T) {
	d := map[string]any{
		"url": "https://example.com",
	}

	req, err := FromDict(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "GET" {
		t.Errorf("expected default method GET, got %s", req.Method)
	}
	if req.Encoding != "utf-8" {
		t.Errorf("expected default encoding utf-8, got %s", req.Encoding)
	}
	if req.Priority != 0 {
		t.Errorf("expected default priority 0, got %d", req.Priority)
	}
	if req.DontFilter {
		t.Error("expected default DontFilter=false")
	}
}

// ============================================================================
// ToDict → FromDict 往返测试
// ============================================================================

func TestToDictFromDictRoundTrip(t *testing.T) {
	original := MustNewRequest("https://example.com/api",
		WithMethod("POST"),
		WithHeader("Content-Type", "application/json"),
		WithHeader("Authorization", "Bearer token123"),
		WithBody([]byte(`{"key":"value","nested":{"a":1}}`)),
		WithCookies([]*http.Cookie{
			{Name: "session", Value: "abc123", Domain: ".example.com"},
			{Name: "lang", Value: "en"},
		}),
		WithMeta(map[string]any{
			"depth":         1,
			"download_slot": "example.com",
		}),
		WithPriority(5),
		WithDontFilter(true),
		WithFlags("cached"),
		WithCbKwargs(map[string]any{"page": 2}),
		WithEncoding("gbk"),
	)

	// ToDict
	d := original.ToDict("parse_detail", "handle_error")

	// 序列化为 JSON 再反序列化（模拟磁盘持久化）
	jsonBytes, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var d2 map[string]any
	if err := json.Unmarshal(jsonBytes, &d2); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// FromDict
	registry := NewCallbackRegistry()
	registry.Register("parse_detail", func() {})
	registry.RegisterErrback("handle_error", func() {})

	restored, err := FromDict(d2, registry)
	if err != nil {
		t.Fatalf("FromDict error: %v", err)
	}

	// 验证所有字段
	if restored.URL.String() != original.URL.String() {
		t.Errorf("URL mismatch: %s vs %s", restored.URL.String(), original.URL.String())
	}
	if restored.Method != original.Method {
		t.Errorf("Method mismatch: %s vs %s", restored.Method, original.Method)
	}
	if string(restored.Body) != string(original.Body) {
		t.Errorf("Body mismatch: %s vs %s", string(restored.Body), string(original.Body))
	}
	if restored.Priority != original.Priority {
		t.Errorf("Priority mismatch: %d vs %d", restored.Priority, original.Priority)
	}
	if restored.DontFilter != original.DontFilter {
		t.Errorf("DontFilter mismatch: %v vs %v", restored.DontFilter, original.DontFilter)
	}
	if restored.Encoding != original.Encoding {
		t.Errorf("Encoding mismatch: %s vs %s", restored.Encoding, original.Encoding)
	}
	if restored.Headers.Get("Content-Type") != original.Headers.Get("Content-Type") {
		t.Errorf("Content-Type mismatch")
	}
	if restored.Headers.Get("Authorization") != original.Headers.Get("Authorization") {
		t.Errorf("Authorization mismatch")
	}
	if len(restored.Cookies) != len(original.Cookies) {
		t.Errorf("Cookies count mismatch: %d vs %d", len(restored.Cookies), len(original.Cookies))
	}
	if len(restored.Flags) != len(original.Flags) {
		t.Errorf("Flags count mismatch: %d vs %d", len(restored.Flags), len(original.Flags))
	}
	if restored.Callback == nil {
		t.Error("Callback should be restored")
	}
	if restored.Errback == nil {
		t.Error("Errback should be restored")
	}
}

func TestToDictFromDictMinimal(t *testing.T) {
	original := MustNewRequest("https://example.com")

	d := original.ToDict("", "")

	jsonBytes, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var d2 map[string]any
	if err := json.Unmarshal(jsonBytes, &d2); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	restored, err := FromDict(d2, nil)
	if err != nil {
		t.Fatalf("FromDict error: %v", err)
	}

	if restored.URL.String() != original.URL.String() {
		t.Errorf("URL mismatch: %s vs %s", restored.URL.String(), original.URL.String())
	}
	if restored.Method != "GET" {
		t.Errorf("expected GET, got %s", restored.Method)
	}
}

// ============================================================================
// ToCURL 测试
// ============================================================================

func TestToCURLBasic(t *testing.T) {
	req := MustNewRequest("https://example.com")
	curl := req.ToCURL()

	if !strings.HasPrefix(curl, "curl") {
		t.Errorf("expected curl command, got: %s", curl)
	}
	if !strings.Contains(curl, "-X GET") {
		t.Errorf("expected -X GET, got: %s", curl)
	}
	if !strings.Contains(curl, "https://example.com") {
		t.Errorf("expected URL, got: %s", curl)
	}
}

func TestToCURLWithHeaders(t *testing.T) {
	req := MustNewRequest("https://example.com",
		WithMethod("POST"),
		WithHeader("Content-Type", "application/json"),
		WithBody([]byte(`{"key":"value"}`)),
	)
	curl := req.ToCURL()

	if !strings.Contains(curl, "-X POST") {
		t.Errorf("expected -X POST, got: %s", curl)
	}
	if !strings.Contains(curl, "Content-Type: application/json") {
		t.Errorf("expected Content-Type header, got: %s", curl)
	}
	if !strings.Contains(curl, `--data-raw`) {
		t.Errorf("expected --data-raw, got: %s", curl)
	}
}

func TestToCURLWithCookies(t *testing.T) {
	req := MustNewRequest("https://example.com",
		WithCookies([]*http.Cookie{
			{Name: "session", Value: "abc"},
			{Name: "lang", Value: "en"},
		}),
	)
	curl := req.ToCURL()

	if !strings.Contains(curl, "--cookie") {
		t.Errorf("expected --cookie, got: %s", curl)
	}
	if !strings.Contains(curl, "session=abc") {
		t.Errorf("expected session cookie, got: %s", curl)
	}
}

// ============================================================================
// FromCURL 测试
// ============================================================================

func TestFromCURLBasic(t *testing.T) {
	req, err := FromCURL(`curl 'https://example.com/page'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.String() != "https://example.com/page" {
		t.Errorf("unexpected URL: %s", req.URL.String())
	}
	if req.Method != "GET" {
		t.Errorf("expected GET, got %s", req.Method)
	}
}

func TestFromCURLWithMethod(t *testing.T) {
	req, err := FromCURL(`curl -X POST 'https://example.com/api'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
}

func TestFromCURLWithHeaders(t *testing.T) {
	req, err := FromCURL(`curl 'https://example.com' -H 'Content-Type: application/json' -H 'Authorization: Bearer token123'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("unexpected Content-Type: %s", req.Headers.Get("Content-Type"))
	}
	if req.Headers.Get("Authorization") != "Bearer token123" {
		t.Errorf("unexpected Authorization: %s", req.Headers.Get("Authorization"))
	}
}

func TestFromCURLWithData(t *testing.T) {
	req, err := FromCURL(`curl 'https://example.com/api' -d '{"key":"value"}'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 有 data 但没有 -X 时，默认 POST
	if req.Method != "POST" {
		t.Errorf("expected POST (implicit from -d), got %s", req.Method)
	}
	if string(req.Body) != `{"key":"value"}` {
		t.Errorf("unexpected body: %s", string(req.Body))
	}
}

func TestFromCURLWithDataRaw(t *testing.T) {
	req, err := FromCURL(`curl 'https://example.com/api' --data-raw '{"key":"value"}'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if string(req.Body) != `{"key":"value"}` {
		t.Errorf("unexpected body: %s", string(req.Body))
	}
}

func TestFromCURLWithCookies(t *testing.T) {
	req, err := FromCURL(`curl 'https://example.com' -b 'session=abc123; lang=en'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(req.Cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(req.Cookies))
	}

	cookieMap := make(map[string]string)
	for _, c := range req.Cookies {
		cookieMap[c.Name] = c.Value
	}
	if cookieMap["session"] != "abc123" {
		t.Errorf("unexpected session cookie: %s", cookieMap["session"])
	}
	if cookieMap["lang"] != "en" {
		t.Errorf("unexpected lang cookie: %s", cookieMap["lang"])
	}
}

func TestFromCURLWithCookieHeader(t *testing.T) {
	req, err := FromCURL(`curl 'https://example.com' -H 'Cookie: session=abc; lang=en'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(req.Cookies) != 2 {
		t.Fatalf("expected 2 cookies from Cookie header, got %d", len(req.Cookies))
	}

	cookieMap := make(map[string]string)
	for _, c := range req.Cookies {
		cookieMap[c.Name] = c.Value
	}
	if cookieMap["session"] != "abc" {
		t.Errorf("unexpected session cookie: %s", cookieMap["session"])
	}
}

func TestFromCURLWithBasicAuth(t *testing.T) {
	req, err := FromCURL(`curl 'https://example.com' -u 'user:password'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	auth := req.Headers.Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		t.Errorf("expected Basic auth header, got: %s", auth)
	}
}

func TestFromCURLWithUserAgent(t *testing.T) {
	req, err := FromCURL(`curl 'https://example.com' -A 'MyBot/1.0'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Headers.Get("User-Agent") != "MyBot/1.0" {
		t.Errorf("unexpected User-Agent: %s", req.Headers.Get("User-Agent"))
	}
}

func TestFromCURLWithoutScheme(t *testing.T) {
	req, err := FromCURL(`curl example.com/page`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.Scheme != "http" {
		t.Errorf("expected http scheme, got %s", req.URL.Scheme)
	}
}

func TestFromCURLWithCompressed(t *testing.T) {
	// --compressed 应被安全忽略
	req, err := FromCURL(`curl 'https://example.com' --compressed`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.String() != "https://example.com" {
		t.Errorf("unexpected URL: %s", req.URL.String())
	}
}

func TestFromCURLWithOptions(t *testing.T) {
	// 用户选项应覆盖 curl 解析出的值
	req, err := FromCURL(`curl 'https://example.com'`,
		WithPriority(10),
		WithDontFilter(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Priority != 10 {
		t.Errorf("expected priority 10, got %d", req.Priority)
	}
	if !req.DontFilter {
		t.Error("expected DontFilter=true")
	}
}

func TestFromCURLNotCurl(t *testing.T) {
	_, err := FromCURL(`wget https://example.com`)
	if err == nil {
		t.Error("expected error for non-curl command")
	}
}

func TestFromCURLEmpty(t *testing.T) {
	_, err := FromCURL(``)
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestFromCURLNoURL(t *testing.T) {
	_, err := FromCURL(`curl -H 'Content-Type: text/html'`)
	if err == nil {
		t.Error("expected error for curl without URL")
	}
}

func TestFromCURLComplex(t *testing.T) {
	// 模拟浏览器导出的复杂 curl 命令
	curlCmd := `curl 'https://api.example.com/v1/data' ` +
		`-X POST ` +
		`-H 'Content-Type: application/json' ` +
		`-H 'Accept: application/json' ` +
		`-H 'X-Request-ID: abc-123' ` +
		`-b 'session=token123; csrf=xyz' ` +
		`--data-raw '{"query":"test","page":1}' ` +
		`--compressed`

	req, err := FromCURL(curlCmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.String() != "https://api.example.com/v1/data" {
		t.Errorf("unexpected URL: %s", req.URL.String())
	}
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("unexpected Content-Type: %s", req.Headers.Get("Content-Type"))
	}
	if req.Headers.Get("X-Request-ID") != "abc-123" {
		t.Errorf("unexpected X-Request-ID: %s", req.Headers.Get("X-Request-ID"))
	}
	if string(req.Body) != `{"query":"test","page":1}` {
		t.Errorf("unexpected body: %s", string(req.Body))
	}
	if len(req.Cookies) != 2 {
		t.Errorf("expected 2 cookies, got %d", len(req.Cookies))
	}
}

func TestFromCURLDoubleQuotes(t *testing.T) {
	req, err := FromCURL(`curl "https://example.com/page" -H "Content-Type: text/html"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.String() != "https://example.com/page" {
		t.Errorf("unexpected URL: %s", req.URL.String())
	}
	if req.Headers.Get("Content-Type") != "text/html" {
		t.Errorf("unexpected Content-Type: %s", req.Headers.Get("Content-Type"))
	}
}

func TestFromCURLMethodWithData(t *testing.T) {
	// 显式指定 PUT 方法 + data
	req, err := FromCURL(`curl -X PUT 'https://example.com/api' -d '{"update":true}'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "PUT" {
		t.Errorf("expected PUT, got %s", req.Method)
	}
	if string(req.Body) != `{"update":true}` {
		t.Errorf("unexpected body: %s", string(req.Body))
	}
}

// ============================================================================
// shellSplit 测试
// ============================================================================

func TestShellSplitBasic(t *testing.T) {
	args, err := shellSplit("curl https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 2 || args[0] != "curl" || args[1] != "https://example.com" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestShellSplitSingleQuotes(t *testing.T) {
	args, err := shellSplit("curl 'https://example.com/path with spaces'")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 2 || args[1] != "https://example.com/path with spaces" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestShellSplitDoubleQuotes(t *testing.T) {
	args, err := shellSplit(`curl "https://example.com" -H "Content-Type: text/html"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[1] != "https://example.com" {
		t.Errorf("unexpected URL: %s", args[1])
	}
	if args[3] != "Content-Type: text/html" {
		t.Errorf("unexpected header: %s", args[3])
	}
}

func TestShellSplitEscapedQuotes(t *testing.T) {
	args, err := shellSplit(`curl -d "value with \"quotes\""`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}
	if args[2] != `value with "quotes"` {
		t.Errorf("unexpected value: %s", args[2])
	}
}

func TestShellSplitBackslash(t *testing.T) {
	args, err := shellSplit(`curl https://example.com/path\ with\ spaces`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 2 || args[1] != "https://example.com/path with spaces" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestShellSplitEmpty(t *testing.T) {
	args, err := shellSplit("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 0 {
		t.Errorf("expected empty args, got %v", args)
	}
}

func TestShellSplitUnmatchedSingleQuote(t *testing.T) {
	_, err := shellSplit("curl 'unmatched")
	if err == nil {
		t.Error("expected error for unmatched single quote")
	}
}

func TestShellSplitUnmatchedDoubleQuote(t *testing.T) {
	_, err := shellSplit(`curl "unmatched`)
	if err == nil {
		t.Error("expected error for unmatched double quote")
	}
}

func TestShellSplitMultipleSpaces(t *testing.T) {
	args, err := shellSplit("curl   -X   POST   https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
}

func TestShellSplitMixedQuotes(t *testing.T) {
	args, err := shellSplit(`curl 'https://example.com' -H "Accept: */*"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[1] != "https://example.com" {
		t.Errorf("unexpected URL: %s", args[1])
	}
	if args[3] != "Accept: */*" {
		t.Errorf("unexpected header: %s", args[3])
	}
}

// ============================================================================
// isJSONSerializable 测试
// ============================================================================

func TestIsJSONSerializable(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{"nil", nil, true},
		{"bool", true, true},
		{"int", 42, true},
		{"float64", 3.14, true},
		{"string", "hello", true},
		{"[]any", []any{1, "two"}, true},
		{"[]string", []string{"a", "b"}, true},
		{"map[string]any", map[string]any{"k": "v"}, true},
		{"func", func() {}, false},
		{"chan", make(chan int), false},
		{"struct", struct{ Name string }{"test"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isJSONSerializable(tt.value)
			if result != tt.expected {
				t.Errorf("isJSONSerializable(%v) = %v, want %v", tt.value, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// shellQuote 测试
// ============================================================================

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "'simple'"},
		{"with spaces", "'with spaces'"},
		{"with'quote", "'with'\\''quote'"},
		{"", "''"},
	}

	for _, tt := range tests {
		result := shellQuote(tt.input)
		if result != tt.expected {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// ============================================================================
// ToCURL → FromCURL 往返测试
// ============================================================================

// ============================================================================
// FromDict 额外覆盖率测试
// ============================================================================

func TestFromDictHeadersMapStringSlice(t *testing.T) {
	// 测试 map[string][]string 类型的 headers（直接从 ToDict 产出）
	d := map[string]any{
		"url": "https://example.com",
		"headers": map[string][]string{
			"Content-Type": {"application/json"},
			"Accept":       {"text/html", "application/json"},
		},
	}

	req, err := FromDict(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("unexpected Content-Type: %s", req.Headers.Get("Content-Type"))
	}
	if req.Headers.Get("Accept") != "text/html" {
		t.Errorf("unexpected Accept: %s", req.Headers.Get("Accept"))
	}
}

func TestFromDictCookiesMapSlice(t *testing.T) {
	// 测试 []map[string]any 类型的 cookies（非 JSON 反序列化场景）
	d := map[string]any{
		"url": "https://example.com",
		"cookies": []map[string]any{
			{"name": "session", "value": "abc", "path": "/", "httponly": true},
			{"name": "lang", "value": "en"},
		},
	}

	req, err := FromDict(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(req.Cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(req.Cookies))
	}
	if req.Cookies[0].Path != "/" {
		t.Errorf("expected path '/', got %s", req.Cookies[0].Path)
	}
	if !req.Cookies[0].HttpOnly {
		t.Error("expected httponly cookie")
	}
}

func TestFromDictPriorityTypes(t *testing.T) {
	// 测试 int 类型的 priority
	d1 := map[string]any{
		"url":      "https://example.com",
		"priority": 5,
	}
	req1, err := FromDict(d1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req1.Priority != 5 {
		t.Errorf("expected priority 5, got %d", req1.Priority)
	}

	// 测试 int64 类型的 priority
	d2 := map[string]any{
		"url":      "https://example.com",
		"priority": int64(7),
	}
	req2, err := FromDict(d2, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req2.Priority != 7 {
		t.Errorf("expected priority 7, got %d", req2.Priority)
	}
}

func TestFromDictFlagsStringSlice(t *testing.T) {
	// 测试 []string 类型的 flags（非 JSON 反序列化场景）
	d := map[string]any{
		"url":   "https://example.com",
		"flags": []string{"cached", "redirected"},
	}

	req, err := FromDict(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(req.Flags) != 2 || req.Flags[0] != "cached" {
		t.Errorf("unexpected flags: %v", req.Flags)
	}
}

func TestFromDictNilRegistry(t *testing.T) {
	// 测试 registry 为 nil 时 callback/errback 字段被忽略
	d := map[string]any{
		"url":      "https://example.com",
		"callback": "parse_detail",
		"errback":  "handle_error",
	}

	req, err := FromDict(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Callback != nil {
		t.Error("callback should be nil when registry is nil")
	}
	if req.Errback != nil {
		t.Error("errback should be nil when registry is nil")
	}
}

func TestFromDictHeaderSingleString(t *testing.T) {
	// 测试 headers 中值为单个字符串的情况
	d := map[string]any{
		"url": "https://example.com",
		"headers": map[string]any{
			"X-Single": "value",
		},
	}

	req, err := FromDict(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Headers.Get("X-Single") != "value" {
		t.Errorf("unexpected header: %s", req.Headers.Get("X-Single"))
	}
}

func TestFromCURLMissingArgument(t *testing.T) {
	// 测试选项缺少参数的情况
	_, err := FromCURL(`curl https://example.com -H`)
	if err == nil {
		t.Error("expected error for -H without argument")
	}
}

func TestFromCURLDataWithDollarPrefix(t *testing.T) {
	// 测试 $'...' 格式的 data
	req, err := FromCURL(`curl 'https://example.com' -d $'test data'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(req.Body) != "test data" {
		t.Errorf("unexpected body: %s", string(req.Body))
	}
}

func TestToCURLFromCURLRoundTrip(t *testing.T) {
	original := MustNewRequest("https://example.com/api",
		WithMethod("POST"),
		WithHeader("Content-Type", "application/json"),
		WithBody([]byte(`{"key":"value"}`)),
	)

	curlCmd := original.ToCURL()

	restored, err := FromCURL(curlCmd)
	if err != nil {
		t.Fatalf("FromCURL error: %v", err)
	}

	if restored.URL.String() != original.URL.String() {
		t.Errorf("URL mismatch: %s vs %s", restored.URL.String(), original.URL.String())
	}
	if restored.Method != original.Method {
		t.Errorf("Method mismatch: %s vs %s", restored.Method, original.Method)
	}
	if string(restored.Body) != string(original.Body) {
		t.Errorf("Body mismatch: %s vs %s", string(restored.Body), string(original.Body))
	}
	if restored.Headers.Get("Content-Type") != original.Headers.Get("Content-Type") {
		t.Errorf("Content-Type mismatch")
	}
}

// ============================================================================
// RegisterSpider 自动注册测试
// ============================================================================

// mockSpider 模拟一个 Spider 实现，用于测试 RegisterSpider 自动扫描。
// 方法命名遵循 Go PascalCase 规范。
type mockSpider struct {
	parseCalled       bool
	parseDetailCalled bool
	handleErrorCalled bool
}

// Parse 符合 Callback 签名：(context.Context, *Response) ([]any, error)
func (s *mockSpider) Parse(ctx context.Context, resp *Response) ([]any, error) {
	s.parseCalled = true
	return nil, nil
}

// ParseDetail 符合 Callback 签名
func (s *mockSpider) ParseDetail(ctx context.Context, resp *Response) ([]any, error) {
	s.parseDetailCalled = true
	return []any{"item1"}, nil
}

// HandleError 符合 Errback 签名：(context.Context, error, *Request) ([]any, error)
func (s *mockSpider) HandleError(ctx context.Context, err error, req *Request) ([]any, error) {
	s.handleErrorCalled = true
	return nil, nil
}

// Name 不符合任何回调签名，不应被注册
func (s *mockSpider) Name() string {
	return "mock_spider"
}

// Start 不符合任何回调签名，不应被注册
func (s *mockSpider) Start(ctx context.Context) <-chan any {
	return nil
}

// helperMethod 未导出方法，不应被注册
func (s *mockSpider) helperMethod(ctx context.Context, resp *Response) ([]any, error) {
	return nil, nil
}

func TestRegisterSpiderBasic(t *testing.T) {
	registry := NewCallbackRegistry()
	spider := &mockSpider{}

	registry.RegisterSpider(spider)

	// 应注册 2 个 Callback（Parse、ParseDetail）
	if registry.Len() != 2 {
		t.Errorf("expected 2 callbacks, got %d (names: %v)", registry.Len(), registry.Names())
	}

	// 应注册 1 个 Errback（HandleError）
	if registry.ErrbackLen() != 1 {
		t.Errorf("expected 1 errback, got %d (names: %v)", registry.ErrbackLen(), registry.ErrbackNames())
	}

	// 验证 Callback 名称
	names := registry.Names()
	sort.Strings(names)
	if len(names) != 2 || names[0] != "Parse" || names[1] != "ParseDetail" {
		t.Errorf("unexpected callback names: %v", names)
	}

	// 验证 Errback 名称
	ebNames := registry.ErrbackNames()
	if len(ebNames) != 1 || ebNames[0] != "HandleError" {
		t.Errorf("unexpected errback names: %v", ebNames)
	}
}

func TestRegisterSpiderCallbackInvocation(t *testing.T) {
	registry := NewCallbackRegistry()
	spider := &mockSpider{}

	registry.RegisterSpider(spider)

	// 查找并调用 ParseDetail
	cb, ok := registry.Lookup("ParseDetail")
	if !ok {
		t.Fatal("expected to find ParseDetail callback")
	}

	// 类型断言并调用
	fn, ok := cb.(func(context.Context, *Response) ([]any, error))
	if !ok {
		t.Fatalf("callback type mismatch: %T", cb)
	}

	results, err := fn(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "item1" {
		t.Errorf("unexpected results: %v", results)
	}
	if !spider.parseDetailCalled {
		t.Error("ParseDetail should have been called on the spider instance")
	}
}

func TestRegisterSpiderErrbackInvocation(t *testing.T) {
	registry := NewCallbackRegistry()
	spider := &mockSpider{}

	registry.RegisterSpider(spider)

	// 查找并调用 HandleError
	eb, ok := registry.LookupErrback("HandleError")
	if !ok {
		t.Fatal("expected to find HandleError errback")
	}

	fn, ok := eb.(func(context.Context, error, *Request) ([]any, error))
	if !ok {
		t.Fatalf("errback type mismatch: %T", eb)
	}

	_, err := fn(context.Background(), fmt.Errorf("test error"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !spider.handleErrorCalled {
		t.Error("HandleError should have been called on the spider instance")
	}
}

func TestRegisterSpiderNil(t *testing.T) {
	registry := NewCallbackRegistry()

	// 不应 panic
	registry.RegisterSpider(nil)

	if registry.Len() != 0 {
		t.Errorf("expected empty registry after nil spider, got %d", registry.Len())
	}
}

// mockSpiderNoCallbacks 没有任何符合签名的方法
type mockSpiderNoCallbacks struct{}

func (s *mockSpiderNoCallbacks) Name() string    { return "empty" }
func (s *mockSpiderNoCallbacks) GetData() string { return "data" }

func TestRegisterSpiderNoMatchingMethods(t *testing.T) {
	registry := NewCallbackRegistry()
	spider := &mockSpiderNoCallbacks{}

	registry.RegisterSpider(spider)

	if registry.Len() != 0 {
		t.Errorf("expected 0 callbacks, got %d", registry.Len())
	}
	if registry.ErrbackLen() != 0 {
		t.Errorf("expected 0 errbacks, got %d", registry.ErrbackLen())
	}
}

// ============================================================================
// FromDict 与 RegisterSpider 集成测试
// ============================================================================

func TestFromDictWithRegisteredSpider(t *testing.T) {
	spider := &mockSpider{}
	registry := NewCallbackRegistry()
	registry.RegisterSpider(spider)

	d := map[string]any{
		"url":      "https://example.com",
		"callback": "ParseDetail",
		"errback":  "HandleError",
	}

	req, err := FromDict(d, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Callback == nil {
		t.Fatal("callback should be restored from registry")
	}
	if req.Errback == nil {
		t.Fatal("errback should be restored from registry")
	}

	// 调用恢复的回调验证绑定正确
	fn, ok := req.Callback.(func(context.Context, *Response) ([]any, error))
	if !ok {
		t.Fatalf("callback type mismatch: %T", req.Callback)
	}
	fn(context.Background(), nil)
	if !spider.parseDetailCalled {
		t.Error("restored callback should invoke spider method")
	}
}

func TestFromDictWithUnregisteredErrback(t *testing.T) {
	registry := NewCallbackRegistry()
	registry.Register("Parse", func() {}) // 注册一个回调，使 registry 非空

	d := map[string]any{
		"url":     "https://example.com",
		"errback": "UnknownErrback",
	}

	_, err := FromDict(d, registry)
	if err == nil {
		t.Fatal("expected error for unregistered errback")
	}
	if !strings.Contains(err.Error(), "UnknownErrback") {
		t.Errorf("error should mention errback name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not found in registry") {
		t.Errorf("error should mention 'not found in registry', got: %v", err)
	}
}

func TestFromDictNilRegistryIgnoresCallbacks(t *testing.T) {
	// registry 为 nil 时，callback/errback 字段被静默忽略（不报错）
	d := map[string]any{
		"url":      "https://example.com",
		"callback": "ParseDetail",
		"errback":  "HandleError",
	}

	req, err := FromDict(d, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Callback != nil {
		t.Error("callback should be nil when registry is nil")
	}
	if req.Errback != nil {
		t.Error("errback should be nil when registry is nil")
	}
}

// ============================================================================
// RegisterSpider 与 ToDict/FromDict 往返测试
// ============================================================================

func TestRegisterSpiderRoundTrip(t *testing.T) {
	spider := &mockSpider{}

	// 创建 Request 并序列化
	original := MustNewRequest("https://example.com/detail",
		WithMethod("POST"),
		WithPriority(5),
	)

	d := original.ToDict("ParseDetail", "HandleError")

	// JSON 往返
	jsonBytes, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var d2 map[string]any
	if err := json.Unmarshal(jsonBytes, &d2); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// 使用 RegisterSpider 自动注册后恢复
	registry := NewCallbackRegistry()
	registry.RegisterSpider(spider)

	restored, err := FromDict(d2, registry)
	if err != nil {
		t.Fatalf("FromDict error: %v", err)
	}

	if restored.URL.String() != original.URL.String() {
		t.Errorf("URL mismatch: %s vs %s", restored.URL.String(), original.URL.String())
	}
	if restored.Callback == nil {
		t.Error("Callback should be restored")
	}
	if restored.Errback == nil {
		t.Error("Errback should be restored")
	}

	// 验证恢复的回调绑定到正确的 spider 实例
	fn, ok := restored.Callback.(func(context.Context, *Response) ([]any, error))
	if !ok {
		t.Fatalf("callback type mismatch: %T", restored.Callback)
	}
	fn(context.Background(), nil)
	if !spider.parseDetailCalled {
		t.Error("restored callback should be bound to the original spider instance")
	}
}

// ============================================================================
// matchesCallbackSignature / matchesErrbackSignature 边界测试
// ============================================================================

// mockSpiderMixedSignatures 包含各种签名的方法，用于测试签名匹配的精确性
type mockSpiderMixedSignatures struct{}

// ValidCallback 标准 Callback 签名
func (s *mockSpiderMixedSignatures) ValidCallback(ctx context.Context, resp *Response) ([]any, error) {
	return nil, nil
}

// ValidErrback 标准 Errback 签名
func (s *mockSpiderMixedSignatures) ValidErrback(ctx context.Context, err error, req *Request) ([]any, error) {
	return nil, nil
}

// WrongReturnCount 返回值数量不对
func (s *mockSpiderMixedSignatures) WrongReturnCount(ctx context.Context, resp *Response) error {
	return nil
}

// WrongParamCount 参数数量不对
func (s *mockSpiderMixedSignatures) WrongParamCount(ctx context.Context) ([]any, error) {
	return nil, nil
}

// WrongParamType 参数类型不对（第二个参数不是 *Response）
func (s *mockSpiderMixedSignatures) WrongParamType(ctx context.Context, data string) ([]any, error) {
	return nil, nil
}

// WrongReturnType 返回值类型不对（第一个返回值不是 slice）
func (s *mockSpiderMixedSignatures) WrongReturnType(ctx context.Context, resp *Response) (string, error) {
	return "", nil
}

func TestRegisterSpiderSignatureMatching(t *testing.T) {
	registry := NewCallbackRegistry()
	spider := &mockSpiderMixedSignatures{}

	registry.RegisterSpider(spider)

	// 只有 ValidCallback 应被注册为 callback
	if registry.Len() != 1 {
		t.Errorf("expected 1 callback, got %d (names: %v)", registry.Len(), registry.Names())
	}
	if _, ok := registry.Lookup("ValidCallback"); !ok {
		t.Error("ValidCallback should be registered")
	}

	// 只有 ValidErrback 应被注册为 errback
	if registry.ErrbackLen() != 1 {
		t.Errorf("expected 1 errback, got %d (names: %v)", registry.ErrbackLen(), registry.ErrbackNames())
	}
	if _, ok := registry.LookupErrback("ValidErrback"); !ok {
		t.Error("ValidErrback should be registered")
	}

	// 不匹配的方法不应被注册
	for _, name := range []string{"WrongReturnCount", "WrongParamCount", "WrongParamType", "WrongReturnType"} {
		if _, ok := registry.Lookup(name); ok {
			t.Errorf("%s should NOT be registered as callback", name)
		}
		if _, ok := registry.LookupErrback(name); ok {
			t.Errorf("%s should NOT be registered as errback", name)
		}
	}
}
