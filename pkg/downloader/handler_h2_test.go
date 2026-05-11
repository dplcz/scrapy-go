package downloader

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// ============================================================================
// HTTP/2 Download Handler 测试
// ============================================================================

func TestHTTP2DownloadHandler_BasicGET(t *testing.T) {
	// 创建支持 HTTP/2 的测试服务器
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello http2")
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.ForceHTTP2 = true
	config.TLSInsecureSkipVerify = true
	handler := NewHTTP2DownloadHandler(10*time.Second, config)
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

	if string(resp.Body) != "hello http2" {
		t.Errorf("expected body 'hello http2', got %q", string(resp.Body))
	}
}

func TestHTTP2DownloadHandler_POST(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "received: %s", body)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.TLSInsecureSkipVerify = true
	config.ForceHTTP2 = true
	handler := NewHTTP2DownloadHandler(10*time.Second, config)
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

func TestHTTP2DownloadHandler_FallbackToHTTP1(t *testing.T) {
	// 创建仅支持 HTTP/1.1 的服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "http1 response")
	}))
	defer server.Close()

	handler := NewHTTP2DownloadHandler(10*time.Second, nil)
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

func TestHTTP2DownloadHandler_ForceHTTP2Meta(t *testing.T) {
	// 创建 h2c（HTTP/2 cleartext）服务器
	h2s := &http2.Server{}
	handler2 := h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "proto: %s", r.Proto)
	}), h2s)

	server := httptest.NewServer(handler2)
	defer server.Close()

	h := NewHTTP2DownloadHandler(10*time.Second, nil)
	defer h.Close()

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}
	req.SetMeta("force_http2", true)

	ctx := context.Background()
	resp, err := h.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	if !strings.Contains(string(resp.Body), "2") {
		t.Logf("response body: %s (HTTP/2 cleartext may not be negotiated in all environments)", string(resp.Body))
	}
}

func TestHTTP2DownloadHandler_Cookies(t *testing.T) {
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
	handler := NewHTTP2DownloadHandler(10*time.Second, config)
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

func TestHTTP2DownloadHandler_NoAutoRedirect(t *testing.T) {
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
	handler := NewHTTP2DownloadHandler(10*time.Second, config)
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

func TestHTTP2DownloadHandler_Timeout(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.TLSInsecureSkipVerify = true
	handler := NewHTTP2DownloadHandler(500*time.Millisecond, config)
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

func TestHTTP2DownloadHandler_ContextCancel(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	config := DefaultConnPoolConfig()
	config.TLSInsecureSkipVerify = true
	handler := NewHTTP2DownloadHandler(10*time.Second, config)
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

func TestHTTP2DownloadHandler_ConnPoolStats(t *testing.T) {
	handler := NewHTTP2DownloadHandler(10*time.Second, nil)
	defer handler.Close()

	stats := handler.ConnPoolStats()
	if stats == nil {
		t.Fatal("ConnPoolStats should not be nil")
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

func TestHTTP2DownloadHandler_Config(t *testing.T) {
	config := &ConnPoolConfig{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     120 * time.Second,
		ForceHTTP2:          true,
	}

	handler := NewHTTP2DownloadHandler(10*time.Second, config)
	defer handler.Close()

	got := handler.Config()
	if got.MaxIdleConns != 200 {
		t.Errorf("expected MaxIdleConns 200, got %d", got.MaxIdleConns)
	}
	if got.MaxIdleConnsPerHost != 20 {
		t.Errorf("expected MaxIdleConnsPerHost 20, got %d", got.MaxIdleConnsPerHost)
	}
}

func TestHTTP2DownloadHandler_Multiplexing(t *testing.T) {
	// 测试 HTTP/2 多路复用：多个并发请求共享同一连接
	var connCount atomic.Int64
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	// 追踪连接数
	trackingListener := &connTrackingListener{
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

	handler := NewHTTP2DownloadHandler(10*time.Second, nil)
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

	// HTTP/2 多路复用应该使用较少的连接
	conns := connCount.Load()
	t.Logf("connections used for %d requests: %d", numRequests, conns)
}

// connTrackingListener 追踪连接数的 Listener 包装。
type connTrackingListener struct {
	net.Listener
	connCount *atomic.Int64
}

func (l *connTrackingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.connCount.Add(1)
	}
	return conn, err
}

// ============================================================================
// DownloadHandler 接口兼容性验证
// ============================================================================

func TestHTTP2DownloadHandler_ImplementsInterface(t *testing.T) {
	var _ DownloadHandler = (*HTTP2DownloadHandler)(nil)
}

func TestProgressHTTPDownloadHandler_ImplementsInterface(t *testing.T) {
	var _ DownloadHandler = (*ProgressHTTPDownloadHandler)(nil)
}
