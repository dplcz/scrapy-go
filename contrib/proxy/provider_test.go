package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticProvider(t *testing.T) {
	t.Parallel()

	input := []string{
		"http://proxy1.example.com:8080",
		"http://user:pass@proxy2.example.com:8080",
	}
	p := NewStaticProvider(input)

	if p.Name() != "static" {
		t.Errorf("want name 'static', got %q", p.Name())
	}

	got, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 proxies, got %d", len(got))
	}
	for i, want := range input {
		if got[i] != want {
			t.Errorf("idx %d: want %q, got %q", i, want, got[i])
		}
	}

	// 修改源切片不应影响 Provider 内部状态
	input[0] = "modified"
	got2, _ := p.Fetch(context.Background())
	if got2[0] == "modified" {
		t.Error("StaticProvider should defensively copy the input slice")
	}
}

func TestFileProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "proxies.txt")

	content := `# 这是注释行
http://proxy1.example.com:8080
http://user:pass@proxy2.example.com:8080

# 空行也应该被忽略

http://proxy3.example.com:8080
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	p := NewFileProvider(path)
	if p.Name() != "file" {
		t.Errorf("want name 'file', got %q", p.Name())
	}

	got, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("want 3 proxies (注释/空行被忽略), got %d: %v", len(got), got)
	}
}

func TestFileProvider_NotExist(t *testing.T) {
	t.Parallel()

	p := NewFileProvider("/non/existing/path/proxies.txt")
	_, err := p.Fetch(context.Background())
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestFileProvider_CtxCancel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "proxies.txt")
	if err := os.WriteFile(path, []byte("http://x:1"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := NewFileProvider(path)
	if _, err := p.Fetch(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("expected ctx.Canceled, got %v", err)
	}
}

func TestHTTPAPIProvider_JSONArray(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["http://p1:8080","http://p2:8080"]`))
	}))
	defer srv.Close()

	p, err := NewHTTPAPIProvider(&HTTPAPIProviderOptions{
		URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	got, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2, got %d", len(got))
	}
}

func TestHTTPAPIProvider_JSONField(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"proxies":["http://p1","http://p2","http://p3"]}}`))
	}))
	defer srv.Close()

	p, err := NewHTTPAPIProvider(&HTTPAPIProviderOptions{
		URL:            srv.URL,
		ResponseFormat: "json_field",
		JSONFieldPath:  "data.proxies",
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	got, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("want 3, got %d", len(got))
	}
}

func TestHTTPAPIProvider_Lines(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# comment\nhttp://p1\n\nhttp://p2\n"))
	}))
	defer srv.Close()

	p, err := NewHTTPAPIProvider(&HTTPAPIProviderOptions{
		URL:            srv.URL,
		ResponseFormat: "lines",
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	got, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2, got %d: %v", len(got), got)
	}
}

func TestHTTPAPIProvider_BadStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p, _ := NewHTTPAPIProvider(&HTTPAPIProviderOptions{URL: srv.URL})
	_, err := p.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 error, got %v", err)
	}
}

func TestHTTPAPIProvider_InvalidOptions(t *testing.T) {
	t.Parallel()

	cases := []*HTTPAPIProviderOptions{
		nil,
		{URL: ""},
		{URL: "http://x", ResponseFormat: "unknown"},
		{URL: "http://x", ResponseFormat: "json_field"}, // 缺 JSONFieldPath
	}
	for i, c := range cases {
		if _, err := NewHTTPAPIProvider(c); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestHTTPAPIProvider_HeadersAndMethod(t *testing.T) {
	t.Parallel()

	var seenAuth string
	var seenMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenMethod = r.Method
		_, _ = w.Write([]byte(`["http://p1"]`))
	}))
	defer srv.Close()

	p, err := NewHTTPAPIProvider(&HTTPAPIProviderOptions{
		URL:    srv.URL,
		Method: "POST",
		Headers: map[string]string{
			"Authorization": "Bearer test-token",
		},
		Body: []byte(`{"q":1}`),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if seenAuth != "Bearer test-token" {
		t.Errorf("Authorization header not set: %q", seenAuth)
	}
	if seenMethod != "POST" {
		t.Errorf("expected POST, got %s", seenMethod)
	}
}

func TestCompositeProvider(t *testing.T) {
	t.Parallel()

	p1 := NewStaticProvider([]string{"http://a", "http://b"})
	p2 := NewStaticProvider([]string{"http://b", "http://c"}) // b 重复
	p := NewCompositeProvider(p1, p2)

	if p.Name() != "composite" {
		t.Errorf("want name 'composite', got %q", p.Name())
	}

	got, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	// 期望去重后得到 a, b, c 共 3 个
	if len(got) != 3 {
		t.Errorf("want 3 (deduped), got %d: %v", len(got), got)
	}
}

func TestCompositeProvider_PartialFailure(t *testing.T) {
	t.Parallel()

	good := NewStaticProvider([]string{"http://a"})
	bad := NewFileProvider("/nonexistent")
	p := NewCompositeProvider(good, bad)

	got, err := p.Fetch(context.Background())
	if err == nil {
		t.Error("expected error from bad provider")
	}
	// 仍应保留 good 的代理
	if len(got) != 1 || got[0] != "http://a" {
		t.Errorf("want [http://a], got %v", got)
	}
}
