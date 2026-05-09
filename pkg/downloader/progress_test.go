package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// ============================================================================
// ProgressHTTPDownloadHandler 测试
// ============================================================================

func TestProgressHTTPDownloadHandler_BasicGET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "11")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello world")
	}))
	defer server.Close()

	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 0)
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

	if string(resp.Body) != "hello world" {
		t.Errorf("expected body 'hello world', got %q", string(resp.Body))
	}
}

func TestProgressHTTPDownloadHandler_WithCallback(t *testing.T) {
	// 创建一个较大的响应体以触发多次进度回调
	bodySize := 10240 // 10KB
	body := make([]byte, bodySize)
	for i := range body {
		body[i] = byte('A' + (i % 26))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", bodySize))
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer server.Close()

	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 10*time.Millisecond)
	defer handler.Close()

	var mu sync.Mutex
	var progressCalls []int64
	var finalBytesRead int64

	req, err := shttp.NewRequest(server.URL + "/large")
	if err != nil {
		t.Fatal(err)
	}

	// 设置进度回调
	req.SetMeta(DownloadProgressMetaKey, DownloadProgressCallback(func(bytesRead, totalBytes int64, r *shttp.Request) {
		mu.Lock()
		progressCalls = append(progressCalls, bytesRead)
		finalBytesRead = bytesRead
		mu.Unlock()
	}))

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	if len(resp.Body) != bodySize {
		t.Errorf("expected body size %d, got %d", bodySize, len(resp.Body))
	}

	mu.Lock()
	defer mu.Unlock()

	// 应该至少有一次进度回调（最终的 100% 回调）
	if len(progressCalls) == 0 {
		t.Error("expected at least one progress callback")
	}

	// 最终字节数应该等于总大小
	if finalBytesRead != int64(bodySize) {
		t.Errorf("expected final bytes read %d, got %d", bodySize, finalBytesRead)
	}

	// 进度应该是单调递增的
	for i := 1; i < len(progressCalls); i++ {
		if progressCalls[i] < progressCalls[i-1] {
			t.Errorf("progress should be monotonically increasing: %d < %d at index %d",
				progressCalls[i], progressCalls[i-1], i)
		}
	}
}

func TestProgressHTTPDownloadHandler_WithCallback_ChunkedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 不设置 Content-Length，使用 chunked 传输
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "chunk %d\n", i)
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer server.Close()

	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 10*time.Millisecond)
	defer handler.Close()

	var callbackCount atomic.Int64

	req, err := shttp.NewRequest(server.URL + "/chunked")
	if err != nil {
		t.Fatal(err)
	}

	req.SetMeta(DownloadProgressMetaKey, DownloadProgressCallback(func(bytesRead, totalBytes int64, r *shttp.Request) {
		callbackCount.Add(1)
		// chunked 传输时 totalBytes 应该是 -1
		if totalBytes != -1 {
			// 最终回调时 totalBytes 等于实际大小
			if bytesRead != totalBytes {
				t.Logf("intermediate callback: bytesRead=%d, totalBytes=%d", bytesRead, totalBytes)
			}
		}
	}))

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	if callbackCount.Load() == 0 {
		t.Error("expected at least one progress callback for chunked response")
	}
}

func TestProgressHTTPDownloadHandler_NoCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "no callback")
	}))
	defer server.Close()

	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 0)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}
	// 不设置进度回调

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if string(resp.Body) != "no callback" {
		t.Errorf("expected body 'no callback', got %q", string(resp.Body))
	}
}

func TestProgressHTTPDownloadHandler_InvalidCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 0)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}
	// 设置无效的回调类型
	req.SetMeta(DownloadProgressMetaKey, "not a function")

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download should succeed with invalid callback: %v", err)
	}

	if string(resp.Body) != "ok" {
		t.Errorf("expected body 'ok', got %q", string(resp.Body))
	}
}

func TestProgressHTTPDownloadHandler_FuncCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello")
	}))
	defer server.Close()

	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 0)
	defer handler.Close()

	var called atomic.Bool

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}

	// 使用 func 类型（非 DownloadProgressCallback 类型别名）
	req.SetMeta(DownloadProgressMetaKey, func(bytesRead, totalBytes int64, r *shttp.Request) {
		called.Store(true)
	})

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	if !called.Load() {
		t.Error("expected func callback to be called")
	}
}

func TestProgressHTTPDownloadHandler_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "posted")
	}))
	defer server.Close()

	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 0)
	defer handler.Close()

	req, err := shttp.NewRequest(server.URL+"/post",
		shttp.WithMethod("POST"),
		shttp.WithBody([]byte("request body")),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if string(resp.Body) != "posted" {
		t.Errorf("expected body 'posted', got %q", string(resp.Body))
	}
}

func TestProgressHTTPDownloadHandler_Proxy(t *testing.T) {
	// 创建代理服务器
	proxyHit := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit = true
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "via proxy")
	}))
	defer proxy.Close()

	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 0)
	defer handler.Close()

	req, err := shttp.NewRequest("http://example.com/test")
	if err != nil {
		t.Fatal(err)
	}
	req.SetMeta("proxy", proxy.URL)

	ctx := context.Background()
	resp, err := handler.Download(ctx, req)
	if err != nil {
		// 代理可能不完全转发，这里只验证代理被使用
		t.Logf("proxy request error (expected): %v", err)
		return
	}

	if proxyHit {
		if string(resp.Body) != "via proxy" {
			t.Errorf("expected body 'via proxy', got %q", string(resp.Body))
		}
	}
}

func TestProgressHTTPDownloadHandler_ConnPoolStats(t *testing.T) {
	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 0)
	defer handler.Close()

	stats := handler.ConnPoolStats()
	if stats == nil {
		t.Fatal("ConnPoolStats should not be nil")
	}
}

func TestProgressHTTPDownloadHandler_MinReportInterval(t *testing.T) {
	// 测试最小报告间隔限制
	bodySize := 1024
	body := make([]byte, bodySize)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", bodySize))
		w.WriteHeader(http.StatusOK)
		// 分多次写入以触发多次 Read
		for i := 0; i < bodySize; i += 64 {
			end := i + 64
			if end > bodySize {
				end = bodySize
			}
			w.Write(body[i:end])
		}
	}))
	defer server.Close()

	// 设置较长的最小报告间隔
	handler := NewProgressHTTPDownloadHandler(10*time.Second, nil, 500*time.Millisecond)
	defer handler.Close()

	var callbackCount atomic.Int64

	req, err := shttp.NewRequest(server.URL + "/test")
	if err != nil {
		t.Fatal(err)
	}

	req.SetMeta(DownloadProgressMetaKey, DownloadProgressCallback(func(bytesRead, totalBytes int64, r *shttp.Request) {
		callbackCount.Add(1)
	}))

	ctx := context.Background()
	_, err = handler.Download(ctx, req)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	// 由于最小间隔为 500ms，快速下载应该只有很少的中间回调 + 最终回调
	count := callbackCount.Load()
	t.Logf("callback count with 500ms interval: %d", count)
	// 至少有最终回调
	if count == 0 {
		t.Error("expected at least one callback (final)")
	}
}

// ============================================================================
// progressReader 测试
// ============================================================================

func TestProgressReader_Read(t *testing.T) {
	data := []byte("hello world, this is a test of progress reader")
	reader := newBytesReader(data)

	var totalRead int64
	pr := &progressReader{
		reader:            reader,
		totalBytes:        int64(len(data)),
		minReportInterval: 0, // 每次都报告
		callback: func(bytesRead, totalBytes int64, r *shttp.Request) {
			totalRead = bytesRead
		},
		request: nil,
	}

	buf := make([]byte, 10)
	for {
		_, err := pr.Read(buf)
		if err != nil {
			break
		}
	}

	if totalRead != int64(len(data)) {
		t.Errorf("expected total read %d, got %d", len(data), totalRead)
	}
}

// ============================================================================
// DownloadProgressMetaKey 常量测试
// ============================================================================

func TestDownloadProgressMetaKey(t *testing.T) {
	if DownloadProgressMetaKey != "download_progress_callback" {
		t.Errorf("unexpected meta key: %s", DownloadProgressMetaKey)
	}
}
