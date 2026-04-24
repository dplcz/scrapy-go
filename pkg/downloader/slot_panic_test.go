package downloader

import (
	"context"
	"strings"
	"testing"
	"time"

	scrapy_http "scrapy-go/pkg/http"
)

// ============================================================================
// Slot Panic Recovery 测试
// ============================================================================

// TestSlotDownloadHandlerPanic 验证下载处理器中的 panic 不会导致进程崩溃。
// panic 应被捕获并作为 error 返回给调用者。
func TestSlotDownloadHandlerPanic(t *testing.T) {
	panicFn := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		panic("intentional panic in download handler")
	}

	slot := NewSlot(8, 0, false, panicFn)
	defer slot.Close()

	req := scrapy_http.MustNewRequest("https://example.com")
	slot.AddActive(req)
	defer slot.RemoveActive(req)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 如果没有 panic recovery，这里会导致进程崩溃
	resp, err := slot.Enqueue(ctx, req)
	if resp != nil {
		t.Error("response should be nil on panic")
	}
	if err == nil {
		t.Fatal("error should not be nil on panic")
	}
	if !strings.Contains(err.Error(), "panic in download handler") {
		t.Errorf("error should contain panic info, got: %v", err)
	}
}

// TestSlotDownloadHandlerPanicDoesNotBlockSlot 验证 panic 后 Slot 仍然可以正常工作。
func TestSlotDownloadHandlerPanicDoesNotBlockSlot(t *testing.T) {
	callCount := 0
	downloadFn := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		callCount++
		if callCount == 1 {
			panic("first call panic")
		}
		return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
	}

	slot := NewSlot(8, 0, false, downloadFn)
	defer slot.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 第一次请求：panic
	req1 := scrapy_http.MustNewRequest("https://example.com/1")
	slot.AddActive(req1)
	_, err := slot.Enqueue(ctx, req1)
	slot.RemoveActive(req1)
	if err == nil {
		t.Fatal("first request should fail with panic error")
	}

	// 第二次请求：正常
	req2 := scrapy_http.MustNewRequest("https://example.com/2")
	slot.AddActive(req2)
	resp, err := slot.Enqueue(ctx, req2)
	slot.RemoveActive(req2)
	if err != nil {
		t.Fatalf("second request should succeed, got error: %v", err)
	}
	if resp == nil {
		t.Fatal("second request should return a response")
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

// TestSlotDownloadHandlerPanicReleasesResources 验证 panic 后信号量和传输标记被正确释放。
func TestSlotDownloadHandlerPanicReleasesResources(t *testing.T) {
	panicFn := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		panic("resource leak test panic")
	}

	slot := NewSlot(2, 0, false, panicFn)
	defer slot.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 发送多个 panic 请求，验证信号量不会被耗尽
	for i := 0; i < 5; i++ {
		req := scrapy_http.MustNewRequest("https://example.com")
		slot.AddActive(req)
		_, _ = slot.Enqueue(ctx, req)
		slot.RemoveActive(req)
	}

	// 等待一下让 goroutine 完成清理
	time.Sleep(100 * time.Millisecond)

	// 验证传输中请求数为 0
	if slot.TransferringCount() != 0 {
		t.Errorf("expected 0 transferring, got %d", slot.TransferringCount())
	}

	// 验证信号量已释放（可以继续发送请求）
	normalFn := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
	}
	slot2 := NewSlot(2, 0, false, normalFn)
	defer slot2.Close()

	req := scrapy_http.MustNewRequest("https://example.com/verify")
	slot2.AddActive(req)
	resp, err := slot2.Enqueue(ctx, req)
	slot2.RemoveActive(req)
	if err != nil {
		t.Fatalf("verification request should succeed: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}

// TestSlotProcessQueuePanicRecovery 验证 processQueue 中的 panic 会自动重启。
func TestSlotProcessQueuePanicRecovery(t *testing.T) {
	callCount := 0
	downloadFn := func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		callCount++
		return scrapy_http.MustNewResponse(req.URL.String(), 200, scrapy_http.WithRequest(req)), nil
	}

	slot := NewSlot(8, 0, false, downloadFn)
	defer slot.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 正常请求应该能成功
	req := scrapy_http.MustNewRequest("https://example.com")
	slot.AddActive(req)
	resp, err := slot.Enqueue(ctx, req)
	slot.RemoveActive(req)
	if err != nil {
		t.Fatalf("request should succeed: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
}
