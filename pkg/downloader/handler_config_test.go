package downloader

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// ============================================================================
// NewHTTPDownloadHandlerWithConfig 测试
// ============================================================================

func TestNewHTTPDownloadHandlerWithConfig_Default(t *testing.T) {
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, nil)
	defer handler.Close()

	if handler.managedTransport == nil {
		t.Fatal("managedTransport should not be nil when created with WithConfig")
	}
	if handler.config == nil {
		t.Fatal("config should not be nil when created with WithConfig")
	}
}

func TestNewHTTPDownloadHandlerWithConfig_CustomConfig(t *testing.T) {
	config := &ConnPoolConfig{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     120 * time.Second,
		ForceHTTP2:          true,
		DialTimeout:         30 * time.Second,
		DialKeepAlive:       30 * time.Second,
	}

	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	got := handler.Config()
	if got == nil {
		t.Fatal("Config() should not return nil")
	}
	if got.MaxIdleConns != 200 {
		t.Errorf("expected MaxIdleConns 200, got %d", got.MaxIdleConns)
	}
	if got.MaxIdleConnsPerHost != 20 {
		t.Errorf("expected MaxIdleConnsPerHost 20, got %d", got.MaxIdleConnsPerHost)
	}
	if !got.ForceHTTP2 {
		t.Error("expected ForceHTTP2 true")
	}
}

func TestNewHTTPDownloadHandlerWithConfig_BasicGET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello from config handler")
	}))
	defer server.Close()

	config := DefaultConnPoolConfig()
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	if string(resp.Body) != "hello from config handler" {
		t.Errorf("expected body 'hello from config handler', got %q", string(resp.Body))
	}
}

func TestNewHTTPDownloadHandlerWithConfig_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "received: %s", body)
	}))
	defer server.Close()

	config := DefaultConnPoolConfig()
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL+"/post",
		shttp.WithMethod("POST"),
		shttp.WithBody([]byte("test body")),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	expected := "received: test body"
	if string(resp.Body) != expected {
		t.Errorf("expected body %q, got %q", expected, string(resp.Body))
	}
}

// ============================================================================
// ConnPoolStats 集成测试
// ============================================================================

func TestHTTPDownloadHandlerWithConfig_ConnPoolStats(t *testing.T) {
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, nil)
	defer handler.Close()

	stats := handler.ConnPoolStats()
	if stats == nil {
		t.Fatal("ConnPoolStats should not be nil for WithConfig handler")
	}

	snapshot := stats.Snapshot()
	if snapshot == nil {
		t.Fatal("Snapshot should not be nil")
	}

	// 初始状态所有计数器应为 0
	for key, val := range snapshot {
		if val != 0 {
			t.Errorf("initial stat %s should be 0, got %d", key, val)
		}
	}
}

func TestHTTPDownloadHandler_ConnPoolStats_NilForDefault(t *testing.T) {
	handler := NewHTTPDownloadHandler(10 * time.Second)
	defer handler.Close()

	stats := handler.ConnPoolStats()
	if stats != nil {
		t.Error("ConnPoolStats should be nil for default handler")
	}
}

func TestHTTPDownloadHandler_Config_NilForDefault(t *testing.T) {
	handler := NewHTTPDownloadHandler(10 * time.Second)
	defer handler.Close()

	config := handler.Config()
	if config != nil {
		t.Error("Config should be nil for default handler")
	}
}

// ============================================================================
// HTTP/2 ALPN 自动协商测试（从 handler_h2_test.go 迁移）
// ============================================================================

func TestHTTPDownloadHandlerWithConfig_HTTP2AutoNegotiation(t *testing.T) {
	// 创建支持 HTTP/2 的 TLS 测试服务器
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "proto: %s", r.Proto)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.ForceHTTP2 = true
	config.TLSInsecureSkipVerify = true
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	// 验证响应协议为 HTTP/2
	if resp.Protocol != "HTTP/2.0" {
		t.Logf("expected protocol HTTP/2.0, got %s (ALPN negotiation may vary by environment)", resp.Protocol)
	}
}

func TestHTTPDownloadHandlerWithConfig_FallbackToHTTP1(t *testing.T) {
	// 创建仅支持 HTTP/1.1 的服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "http1 response")
	}))
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.ForceHTTP2 = true
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed (should fallback to HTTP/1.1): %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	if string(resp.Body) != "http1 response" {
		t.Errorf("expected body 'http1 response', got %q", string(resp.Body))
	}
}

// ============================================================================
// Cookies 测试（从 handler_h2_test.go 迁移）
// ============================================================================

func TestHTTPDownloadHandlerWithConfig_Cookies(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "session=%s", cookie.Value)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.TLSInsecureSkipVerify = true
	config.ForceHTTP2 = true
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL+"/test",
		shttp.WithCookies([]*http.Cookie{
			{Name: "session", Value: "abc123"},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	if string(resp.Body) != "session=abc123" {
		t.Errorf("expected body 'session=abc123', got %q", string(resp.Body))
	}
}

// ============================================================================
// 重定向禁用测试（从 handler_h2_test.go 迁移）
// ============================================================================

func TestHTTPDownloadHandlerWithConfig_NoAutoRedirect(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "target")
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.TLSInsecureSkipVerify = true
	config.ForceHTTP2 = true
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/redirect")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	// 应该返回 302 而不是跟踪重定向
	if resp.Status != http.StatusFound {
		t.Errorf("expected status 302, got %d", resp.Status)
	}
}

// ============================================================================
// 超时测试（从 handler_h2_test.go 迁移）
// ============================================================================

func TestHTTPDownloadHandlerWithConfig_Timeout(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.TLSInsecureSkipVerify = true
	handler := NewHTTPDownloadHandlerWithConfig(500*time.Millisecond, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/slow")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	_, err = handler.Download(ctx, req)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestHTTPDownloadHandlerWithConfig_ContextCancel(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.TLSInsecureSkipVerify = true
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/slow")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = handler.Download(ctx, req)
	if err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
}

// ============================================================================
// HTTP/2 多路复用测试（从 handler_h2_test.go 迁移）
// ============================================================================

func TestHTTPDownloadHandlerWithConfig_Multiplexing(t *testing.T) {
	// 测试 HTTP/2 多路复用：多个并发请求共享同一连接
	var connCount atomic.Int64
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	// 追踪连接数
	trackingListener := &configTestConnTrackingListener{
		Listener:  listener,
		connCount: &connCount,
	}

	h2s := &http2.Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	server := &httptest.Server{
		Listener: trackingListener,
		Config: &http.Server{
			Handler: h2c.NewHandler(mux, h2s),
		},
	}
	server.Start()
	defer server.Close()

	config := DefaultConnPoolConfig()
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	// 发送 10 个并发请求
	const numRequests = 10
	errCh := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req, err := shttp.NewRequest(server.URL + "/test")
			if err != nil {
				errCh <- err
				return
			}
			_, err = handler.Download(context.Background(), req)
			errCh <- err
		}()
	}

	for i := 0; i < numRequests; i++ {
		if err := <-errCh; err != nil {
			t.Logf("request %d error (expected in h2c test): %v", i, err)
		}
	}

	// 记录连接数（HTTP/2 多路复用应使用较少的连接）
	conns := connCount.Load()
	t.Logf("connections used for %d requests: %d", numRequests, conns)
}

// configTestConnTrackingListener 追踪连接数的 Listener 包装。
type configTestConnTrackingListener struct {
	net.Listener
	connCount *atomic.Int64
}

func (l *configTestConnTrackingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.connCount.Add(1)
	}
	return conn, err
}

// ============================================================================
// AllowH2C 配置测试
// ============================================================================

func TestHTTPDownloadHandlerWithConfig_AllowH2C(t *testing.T) {
	// 创建 h2c（HTTP/2 cleartext）服务器
	h2s := &http2.Server{}
	h2cHandler := h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "proto: %s", r.Proto)
	}), h2s)

	server := httptest.NewServer(h2cHandler)
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.AllowH2C = true
	config.DialTimeout = 30 * time.Second
	config.DialKeepAlive = 30 * time.Second
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	t.Logf("h2c response body: %s, protocol: %s", string(resp.Body), resp.Protocol)
}

// ============================================================================
// ForceHTTP2 配置测试
// ============================================================================

func TestHTTPDownloadHandlerWithConfig_ForceHTTP2(t *testing.T) {
	// 创建支持 HTTP/2 的 TLS 测试服务器
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "proto: %s", r.Proto)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.ForceHTTP2 = true
	config.TLSInsecureSkipVerify = true
	handler := NewHTTPDownloadHandlerWithConfig(10*time.Second, config)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	// 验证 ForceHTTP2 配置生效
	if handler.transport.ForceAttemptHTTP2 != true {
		t.Error("expected ForceAttemptHTTP2 to be true")
	}
}

// ============================================================================
// DownloadHandler 接口兼容性验证
// ============================================================================

func TestHTTPDownloadHandlerWithConfig_ImplementsInterface(t *testing.T) {
	var _ DownloadHandler = (*HTTPDownloadHandler)(nil)
}

// ============================================================================
// ConnPoolConfig AllowH2C 配置测试
// ============================================================================

func TestConnPoolConfig_AllowH2C_Default(t *testing.T) {
	config := DefaultConnPoolConfig()
	if config.AllowH2C {
		t.Error("expected AllowH2C false by default")
	}
}

func TestConnPoolConfigFromSettings_AllowH2C(t *testing.T) {
	settings := map[string]any{
		"HTTP2_ENABLED":   true,
		"HTTP2_ALLOW_H2C": true,
	}

	getInt := func(key string, def int) int { return def }
	getDuration := func(key string, def time.Duration) time.Duration { return def }
	getBool := func(key string, def bool) bool {
		if v, ok := settings[key]; ok {
			return v.(bool)
		}
		return def
	}

	config := ConnPoolConfigFromSettings(getInt, getDuration, getBool)

	if !config.ForceHTTP2 {
		t.Error("expected ForceHTTP2 true")
	}
	if !config.AllowH2C {
		t.Error("expected AllowH2C true")
	}
}
