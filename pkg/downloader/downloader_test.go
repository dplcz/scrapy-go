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

	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/settings"
	"scrapy-go/pkg/signal"
	"scrapy-go/pkg/stats"
)

// dummyDownloadFn 是测试用的空下载函数。
func dummyDownloadFn(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
	return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
}

// ============================================================================
// Slot 测试
// ============================================================================

func TestSlotBasic(t *testing.T) {
	slot := NewSlot(8, 0, false, dummyDownloadFn)
	defer slot.Close()

	if slot.FreeTransferSlots() != 8 {
		t.Errorf("expected 8 free slots, got %d", slot.FreeTransferSlots())
	}
	if !slot.IsIdle() {
		t.Error("new slot should be idle")
	}
	if slot.ActiveCount() != 0 {
		t.Error("new slot should have 0 active")
	}
}

func TestSlotConcurrency(t *testing.T) {
	slot := NewSlot(2, 0, false, dummyDownloadFn)
	defer slot.Close()

	req1 := scrapy_http.MustNewRequest("https://example.com/1")
	req2 := scrapy_http.MustNewRequest("https://example.com/2")
	req3 := scrapy_http.MustNewRequest("https://example.com/3")

	slot.AddTransferring(req1)
	slot.AddTransferring(req2)

	if slot.FreeTransferSlots() != 0 {
		t.Errorf("expected 0 free slots, got %d", slot.FreeTransferSlots())
	}

	slot.RemoveTransferring(req1)
	if slot.FreeTransferSlots() != 1 {
		t.Errorf("expected 1 free slot, got %d", slot.FreeTransferSlots())
	}

	slot.AddTransferring(req3)
	if slot.FreeTransferSlots() != 0 {
		t.Errorf("expected 0 free slots, got %d", slot.FreeTransferSlots())
	}
}

func TestSlotActive(t *testing.T) {
	slot := NewSlot(8, 0, false, dummyDownloadFn)
	defer slot.Close()

	req := scrapy_http.MustNewRequest("https://example.com")
	slot.AddActive(req)

	if slot.IsIdle() {
		t.Error("should not be idle with active request")
	}
	if slot.ActiveCount() != 1 {
		t.Errorf("expected 1 active, got %d", slot.ActiveCount())
	}

	slot.RemoveActive(req)
	if !slot.IsIdle() {
		t.Error("should be idle after removing active request")
	}
}

func TestSlotDownloadDelay(t *testing.T) {
	// 无延迟
	slot1 := NewSlot(8, 0, false, dummyDownloadFn)
	defer slot1.Close()
	if slot1.DownloadDelay() != 0 {
		t.Error("delay should be 0")
	}

	// 固定延迟
	slot2 := NewSlot(8, time.Second, false, dummyDownloadFn)
	defer slot2.Close()
	if slot2.DownloadDelay() != time.Second {
		t.Error("delay should be 1s")
	}

	// 随机延迟 - 测试 getDownloadDelay 内部方法
	slot3 := NewSlot(8, time.Second, true, dummyDownloadFn)
	defer slot3.Close()
	delay := slot3.getDownloadDelay()
	if delay < 500*time.Millisecond || delay >= 1500*time.Millisecond {
		t.Errorf("random delay should be in [0.5s, 1.5s), got %v", delay)
	}
}

// TestSlotEnqueueBasic 测试 Slot 的入队和下载功能。
func TestSlotEnqueueBasic(t *testing.T) {
	downloadCalled := false
	slot := NewSlot(8, 0, false, func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		downloadCalled = true
		return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
	})
	defer slot.Close()

	req := scrapy_http.MustNewRequest("https://example.com")
	slot.AddActive(req)
	defer slot.RemoveActive(req)

	resp, err := slot.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !downloadCalled {
		t.Error("download function should be called")
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

// TestSlotDelayEnforcement 测试 Slot 的延迟控制是否真正串行化。
// 验证同一 Slot 内两次请求之间至少间隔 delay 时间。
func TestSlotDelayEnforcement(t *testing.T) {
	var mu sync.Mutex
	var timestamps []time.Time

	slot := NewSlot(8, 200*time.Millisecond, false, func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
	})
	defer slot.Close()

	// 同时发送 3 个请求
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := scrapy_http.MustNewRequest("https://example.com")
			slot.AddActive(req)
			defer slot.RemoveActive(req)
			slot.Enqueue(context.Background(), req)
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if len(timestamps) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(timestamps))
	}

	// 验证第2个和第1个之间至少间隔 ~200ms
	// 第3个和第2个之间至少间隔 ~200ms
	// 使用 150ms 作为下限（考虑调度抖动）
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap < 150*time.Millisecond {
			t.Errorf("gap between request %d and %d is %v, expected >= 150ms", i-1, i, gap)
		}
	}
}

// TestSlotConcurrencyWithDelay 测试有延迟时并发传输仍然受控。
func TestSlotConcurrencyWithDelay(t *testing.T) {
	var maxConcurrent atomic.Int32
	var currentConcurrent atomic.Int32

	slot := NewSlot(2, 50*time.Millisecond, false, func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		cur := currentConcurrent.Add(1)
		for {
			old := maxConcurrent.Load()
			if cur <= old {
				break
			}
			if maxConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(100 * time.Millisecond) // 模拟下载耗时
		currentConcurrent.Add(-1)
		return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
	})
	defer slot.Close()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := scrapy_http.MustNewRequest("https://example.com")
			slot.AddActive(req)
			defer slot.RemoveActive(req)
			slot.Enqueue(context.Background(), req)
		}()
	}
	wg.Wait()

	if maxConcurrent.Load() > 2 {
		t.Errorf("max concurrent should be <= 2, got %d", maxConcurrent.Load())
	}
}

// TestSlotClose 测试关闭 Slot 后入队应返回错误。
func TestSlotClose(t *testing.T) {
	// 使用一个慢下载函数，确保 Close 能在 processQueue 处理之前生效
	slot := NewSlot(8, 0, false, func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		time.Sleep(time.Second)
		return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
	})

	// 先关闭 Slot
	slot.Close()

	// 等待 processQueue goroutine 退出
	time.Sleep(50 * time.Millisecond)

	req := scrapy_http.MustNewRequest("https://example.com")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := slot.Enqueue(ctx, req)
	if err == nil {
		t.Error("should return error after slot is closed")
	}
}

// ============================================================================
// HTTPDownloadHandler 测试
// ============================================================================

func TestHTTPDownloadHandlerGET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		w.Write([]byte("<html>Hello</html>"))
	}))
	defer server.Close()

	handler := NewHTTPDownloadHandler(10 * time.Second)
	defer handler.Close()

	req := scrapy_http.MustNewRequest(server.URL)
	resp, err := handler.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if resp.Text() != "<html>Hello</html>" {
		t.Errorf("unexpected body: %s", resp.Text())
	}
	if resp.Headers.Get("Content-Type") != "text/html" {
		t.Errorf("unexpected Content-Type: %s", resp.Headers.Get("Content-Type"))
	}
}

func TestHTTPDownloadHandlerPOST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		w.Write(body)
	}))
	defer server.Close()

	handler := NewHTTPDownloadHandler(10 * time.Second)
	defer handler.Close()

	req := scrapy_http.MustNewRequest(server.URL,
		scrapy_http.WithMethod("POST"),
		scrapy_http.WithBody([]byte("test body")),
	)
	resp, err := handler.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text() != "test body" {
		t.Errorf("unexpected body: %s", resp.Text())
	}
}

func TestHTTPDownloadHandlerHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Header.Get("X-Custom")))
	}))
	defer server.Close()

	handler := NewHTTPDownloadHandler(10 * time.Second)
	defer handler.Close()

	req := scrapy_http.MustNewRequest(server.URL,
		scrapy_http.WithHeader("X-Custom", "test-value"),
	)
	resp, err := handler.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text() != "test-value" {
		t.Errorf("custom header not sent: %s", resp.Text())
	}
}

func TestHTTPDownloadHandlerNoAutoRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			w.Header().Set("Location", "/new")
			w.WriteHeader(301)
			return
		}
		w.Write([]byte("new page"))
	}))
	defer server.Close()

	handler := NewHTTPDownloadHandler(10 * time.Second)
	defer handler.Close()

	req := scrapy_http.MustNewRequest(server.URL + "/old")
	resp, err := handler.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 应返回 301 而不是自动跟踪重定向
	if resp.Status != 301 {
		t.Errorf("expected 301 (no auto redirect), got %d", resp.Status)
	}
}

func TestHTTPDownloadHandlerTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("slow"))
	}))
	defer server.Close()

	handler := NewHTTPDownloadHandler(100 * time.Millisecond)
	defer handler.Close()

	req := scrapy_http.MustNewRequest(server.URL)
	_, err := handler.Download(context.Background(), req)
	if err == nil {
		t.Error("should timeout")
	}
}

// ============================================================================
// Downloader 测试
// ============================================================================

func TestDownloaderBasic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	sc := stats.NewMemoryStatsCollector(false, nil)
	sm := signal.NewSignalManager(nil)
	handler := NewHTTPDownloadHandler(10 * time.Second)

	d := NewDownloader(s, handler, sm, sc, nil)
	defer d.Close()

	req := scrapy_http.MustNewRequest(server.URL)
	resp, err := d.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected 200, got %d", resp.Status)
	}
	if resp.Text() != "ok" {
		t.Errorf("unexpected body: %s", resp.Text())
	}
}

func TestDownloaderNeedsBackout(t *testing.T) {
	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 2, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer server.Close()

	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, nil, nil, nil)
	defer d.Close()

	if d.NeedsBackout() {
		t.Error("should not need backout initially")
	}

	// 启动 2 个并发下载
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := scrapy_http.MustNewRequest(server.URL)
			d.Download(context.Background(), req)
		}()
	}

	// 等待请求开始
	time.Sleep(100 * time.Millisecond)
	if !d.NeedsBackout() {
		t.Error("should need backout when at capacity")
	}

	wg.Wait()
}

func TestDownloaderSignals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	sm := signal.NewSignalManager(nil)
	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, sm, nil, nil)
	defer d.Close()

	var reachedCount, leftCount, downloadedCount atomic.Int32

	sm.Connect(func(params map[string]any) error {
		reachedCount.Add(1)
		return nil
	}, signal.RequestReachedDownloader)

	sm.Connect(func(params map[string]any) error {
		leftCount.Add(1)
		return nil
	}, signal.RequestLeftDownloader)

	sm.Connect(func(params map[string]any) error {
		downloadedCount.Add(1)
		return nil
	}, signal.ResponseDownloaded)

	req := scrapy_http.MustNewRequest(server.URL)
	d.Download(context.Background(), req)

	if reachedCount.Load() != 1 {
		t.Errorf("expected 1 request_reached_downloader signal, got %d", reachedCount.Load())
	}
	if leftCount.Load() != 1 {
		t.Errorf("expected 1 request_left_downloader signal, got %d", leftCount.Load())
	}
	if downloadedCount.Load() != 1 {
		t.Errorf("expected 1 response_downloaded signal, got %d", downloadedCount.Load())
	}
}

func TestDownloaderConcurrent(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer server.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 4, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, nil, nil, nil)
	defer d.Close()

	n := 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := scrapy_http.MustNewRequest(server.URL)
			d.Download(context.Background(), req)
		}()
	}
	wg.Wait()

	if int(requestCount.Load()) != n {
		t.Errorf("expected %d requests, got %d", n, requestCount.Load())
	}
}

// TestDownloaderDelayEnforcement 测试 Downloader 级别的延迟控制。
// 验证同一域名的请求之间至少间隔 DOWNLOAD_DELAY。
func TestDownloaderDelayEnforcement(t *testing.T) {
	var mu sync.Mutex
	var timestamps []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer server.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", time.Duration(200*time.Millisecond), settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, nil, nil, nil)
	defer d.Close()

	// 同时发送 3 个请求
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := scrapy_http.MustNewRequest(server.URL)
			d.Download(context.Background(), req)
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if len(timestamps) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(timestamps))
	}

	// 验证请求之间至少间隔 ~200ms（使用 150ms 作为下限，考虑调度抖动）
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap < 150*time.Millisecond {
			t.Errorf("gap between request %d and %d is %v, expected >= 150ms", i-1, i, gap)
		}
	}
}

// TestDownloaderNoConcurrencyLimitWithDelay 测试有延迟时不同域名仍然并行。
func TestDownloaderNoConcurrencyLimitWithDelay(t *testing.T) {
	var requestCount atomic.Int32

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(200)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(200)
	}))
	defer server2.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", time.Duration(100*time.Millisecond), settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, nil, nil, nil)
	defer d.Close()

	start := time.Now()

	// 向两个不同域名各发 1 个请求
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		req := scrapy_http.MustNewRequest(server1.URL)
		d.Download(context.Background(), req)
	}()
	go func() {
		defer wg.Done()
		req := scrapy_http.MustNewRequest(server2.URL)
		d.Download(context.Background(), req)
	}()
	wg.Wait()

	elapsed := time.Since(start)

	if int(requestCount.Load()) != 2 {
		t.Errorf("expected 2 requests, got %d", requestCount.Load())
	}

	// 两个不同域名的请求应该几乎同时完成（不需要串行等待）
	// 总耗时应该远小于 200ms（如果串行则需要 200ms+）
	if elapsed > 300*time.Millisecond {
		t.Errorf("different domains should be parallel, but took %v", elapsed)
	}
}

// ============================================================================
// download_slot 分组测试
// ============================================================================

// TestDownloaderCustomSlotKey 测试通过 Meta 设置自定义 download_slot 分组。
// 验证不同 URL 的请求可以通过 Meta 路由到同一个 Slot。
func TestDownloaderCustomSlotKey(t *testing.T) {
	var mu sync.Mutex
	var timestamps []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer server.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", time.Duration(200*time.Millisecond), settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, nil, nil, nil)
	defer d.Close()

	// 3 个请求都设置相同的自定义 download_slot，应被路由到同一个 Slot
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := scrapy_http.MustNewRequest(server.URL,
				scrapy_http.WithMeta(map[string]any{
					DownloadSlotMetaKey: "my-custom-group",
				}),
			)
			d.Download(context.Background(), req)
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if len(timestamps) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(timestamps))
	}

	// 同一 Slot 内的请求应受 delay 控制，两两之间至少间隔 ~200ms
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap < 150*time.Millisecond {
			t.Errorf("gap between request %d and %d is %v, expected >= 150ms (same custom slot)", i-1, i, gap)
		}
	}

	// 验证 Slot map 中只有一个自定义 key 的 Slot
	d.mu.RLock()
	defer d.mu.RUnlock()
	if _, ok := d.slots["my-custom-group"]; !ok {
		t.Error("expected slot with key 'my-custom-group' to exist")
	}
	if len(d.slots) != 1 {
		t.Errorf("expected 1 slot (custom group), got %d", len(d.slots))
	}
}

// TestDownloaderCustomSlotIsolation 测试不同 download_slot 分组之间的隔离性。
// 验证不同分组的请求各自独立受 delay 控制，互不阻塞。
func TestDownloaderCustomSlotIsolation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", time.Duration(200*time.Millisecond), settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, nil, nil, nil)
	defer d.Close()

	start := time.Now()

	// 向两个不同的自定义 Slot 各发 1 个请求，应并行执行
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		req := scrapy_http.MustNewRequest(server.URL,
			scrapy_http.WithMeta(map[string]any{
				DownloadSlotMetaKey: "group-a",
			}),
		)
		d.Download(context.Background(), req)
	}()
	go func() {
		defer wg.Done()
		req := scrapy_http.MustNewRequest(server.URL,
			scrapy_http.WithMeta(map[string]any{
				DownloadSlotMetaKey: "group-b",
			}),
		)
		d.Download(context.Background(), req)
	}()
	wg.Wait()

	elapsed := time.Since(start)

	// 不同分组应并行，总耗时远小于 400ms（如果串行则需要 400ms+）
	if elapsed > 300*time.Millisecond {
		t.Errorf("different custom slots should be parallel, but took %v", elapsed)
	}

	// 验证创建了两个不同的 Slot
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.slots) != 2 {
		t.Errorf("expected 2 slots (group-a, group-b), got %d", len(d.slots))
	}
	if _, ok := d.slots["group-a"]; !ok {
		t.Error("expected slot 'group-a' to exist")
	}
	if _, ok := d.slots["group-b"]; !ok {
		t.Error("expected slot 'group-b' to exist")
	}
}

// TestDownloaderCustomSlotMetaWriteback 测试 download_slot 的 Meta 回写。
// 验证 Download 完成后，请求的 Meta 中包含正确的 Slot key。
func TestDownloaderCustomSlotMetaWriteback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", 0, settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, nil, nil, nil)
	defer d.Close()

	// 测试1：设置了自定义 download_slot 的请求，Meta 回写为自定义值
	req1 := scrapy_http.MustNewRequest(server.URL,
		scrapy_http.WithMeta(map[string]any{
			DownloadSlotMetaKey: "custom-key",
		}),
	)
	_, err := d.Download(context.Background(), req1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := req1.GetMeta(DownloadSlotMetaKey); !ok || v != "custom-key" {
		t.Errorf("expected meta download_slot='custom-key', got %v", v)
	}

	// 测试2：未设置 download_slot 的请求，Meta 回写为域名
	req2 := scrapy_http.MustNewRequest(server.URL)
	_, err = d.Download(context.Background(), req2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := req2.GetMeta(DownloadSlotMetaKey)
	if !ok {
		t.Fatal("expected meta download_slot to be set after download")
	}
	// httptest 服务器的域名是 127.0.0.1
	if v != "127.0.0.1" {
		t.Errorf("expected meta download_slot='127.0.0.1', got %v", v)
	}
}

// TestDownloaderCustomSlotOverridesDomain 测试自定义 download_slot 覆盖域名分组。
// 验证来自同一域名的请求可以通过不同的 download_slot 被分到不同 Slot。
func TestDownloaderCustomSlotOverridesDomain(t *testing.T) {
	var mu sync.Mutex
	groupATimestamps := make([]time.Time, 0)
	groupBTimestamps := make([]time.Time, 0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		// 通过 URL path 区分分组，记录时间戳
		if r.URL.Path == "/a" {
			groupATimestamps = append(groupATimestamps, time.Now())
		} else {
			groupBTimestamps = append(groupBTimestamps, time.Now())
		}
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer server.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", time.Duration(200*time.Millisecond), settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, nil, nil, nil)
	defer d.Close()

	start := time.Now()

	// 同一域名的请求，通过不同 download_slot 分到两个 Slot
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		req := scrapy_http.MustNewRequest(server.URL+"/a",
			scrapy_http.WithMeta(map[string]any{
				DownloadSlotMetaKey: "slot-a",
			}),
		)
		d.Download(context.Background(), req)
	}()
	go func() {
		defer wg.Done()
		req := scrapy_http.MustNewRequest(server.URL+"/b",
			scrapy_http.WithMeta(map[string]any{
				DownloadSlotMetaKey: "slot-b",
			}),
		)
		d.Download(context.Background(), req)
	}()
	wg.Wait()

	elapsed := time.Since(start)

	// 虽然是同一域名，但不同 Slot 应并行执行
	if elapsed > 300*time.Millisecond {
		t.Errorf("different custom slots on same domain should be parallel, but took %v", elapsed)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(groupATimestamps) != 1 || len(groupBTimestamps) != 1 {
		t.Errorf("expected 1 request per group, got group-a=%d, group-b=%d",
			len(groupATimestamps), len(groupBTimestamps))
	}
}

// TestDownloaderDefaultSlotByDomain 测试默认按域名分组的行为。
// 验证未设置 download_slot 时，同一域名的请求共享同一个 Slot。
func TestDownloaderDefaultSlotByDomain(t *testing.T) {
	var mu sync.Mutex
	var timestamps []time.Time

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer server.Close()

	s := settings.NewEmpty()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", time.Duration(200*time.Millisecond), settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", 10, settings.PriorityDefault)
	s.Set("RANDOMIZE_DOWNLOAD_DELAY", false, settings.PriorityDefault)

	handler := NewHTTPDownloadHandler(10 * time.Second)
	d := NewDownloader(s, handler, nil, nil, nil)
	defer d.Close()

	// 同一域名不同路径的 3 个请求，不设置 download_slot，应共享同一个 Slot
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := scrapy_http.MustNewRequest(fmt.Sprintf("%s/page/%d", server.URL, idx))
			d.Download(context.Background(), req)
		}(i)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if len(timestamps) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(timestamps))
	}

	// 同一域名共享 Slot，应受 delay 控制
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap < 150*time.Millisecond {
			t.Errorf("gap between request %d and %d is %v, expected >= 150ms (same domain slot)", i-1, i, gap)
		}
	}

	// 验证只创建了一个 Slot（按域名 127.0.0.1）
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.slots) != 1 {
		t.Errorf("expected 1 slot (by domain), got %d", len(d.slots))
	}
}

// ============================================================================
// 接口验证
// ============================================================================

func TestDownloadHandlerInterface(t *testing.T) {
	var _ DownloadHandler = (*HTTPDownloadHandler)(nil)
}
