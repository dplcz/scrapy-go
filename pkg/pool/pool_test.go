package pool

import (
	"net/http"
	"net/url"
	"sync"
	"testing"
)

func TestRequestPool_GetPut(t *testing.T) {
	req := RequestPool.Get()
	if req == nil {
		t.Fatal("expected non-nil request from pool")
	}

	// 设置字段
	req.URL, _ = url.Parse("http://example.com")
	req.Method = "POST"
	req.Headers.Set("Content-Type", "application/json")
	req.Body = append(req.Body, []byte(`{"key":"value"}`)...)
	req.Meta["foo"] = "bar"
	req.Priority = 5

	// 归还池
	RequestPool.Put(req)

	// 再次获取（可能是同一个对象）
	req2 := RequestPool.Get()
	if req2.URL != nil {
		t.Error("expected URL to be nil after reset")
	}
	if req2.Method != "" {
		t.Error("expected Method to be empty after reset")
	}
	if len(req2.Headers) != 0 {
		t.Error("expected Headers to be empty after reset")
	}
	if len(req2.Body) != 0 {
		t.Error("expected Body to be empty after reset")
	}
	if len(req2.Meta) != 0 {
		t.Error("expected Meta to be empty after reset")
	}
	if req2.Priority != 0 {
		t.Error("expected Priority to be 0 after reset")
	}
	RequestPool.Put(req2)
}

func TestResponsePool_GetPut(t *testing.T) {
	resp := ResponsePool.Get()
	if resp == nil {
		t.Fatal("expected non-nil response from pool")
	}

	// 设置字段
	resp.URL, _ = url.Parse("http://example.com/page")
	resp.Status = 200
	resp.Headers.Set("Content-Type", "text/html")
	resp.Body = append(resp.Body, []byte("<html></html>")...)

	// 归还池
	ResponsePool.Put(resp)

	// 再次获取
	resp2 := ResponsePool.Get()
	if resp2.URL != nil {
		t.Error("expected URL to be nil after reset")
	}
	if resp2.Status != 0 {
		t.Error("expected Status to be 0 after reset")
	}
	if len(resp2.Headers) != 0 {
		t.Error("expected Headers to be empty after reset")
	}
	if len(resp2.Body) != 0 {
		t.Error("expected Body to be empty after reset")
	}
	ResponsePool.Put(resp2)
}

func TestBytesPool_GetPut(t *testing.T) {
	b := BytesPool.Get()
	if b == nil {
		t.Fatal("expected non-nil bytes from pool")
	}
	if len(*b) != 0 {
		t.Errorf("expected empty slice, got len=%d", len(*b))
	}
	if cap(*b) < 32*1024 {
		t.Errorf("expected cap >= 32KB, got %d", cap(*b))
	}

	// 使用
	*b = append(*b, []byte("hello world")...)

	// 归还
	BytesPool.Put(b)

	// 再次获取
	b2 := BytesPool.Get()
	if len(*b2) != 0 {
		t.Errorf("expected empty slice after put, got len=%d", len(*b2))
	}
	BytesPool.Put(b2)
}

func TestPooledRequest_Reset(t *testing.T) {
	req := &PooledRequest{
		Headers: make(http.Header),
		Meta:    make(map[string]any),
	}

	req.URL, _ = url.Parse("http://test.com")
	req.Method = "GET"
	req.Headers.Set("X-Custom", "value")
	req.Body = []byte("body data")
	req.Meta["key"] = "value"
	req.Priority = 10

	req.Reset()

	if req.URL != nil {
		t.Error("URL should be nil after reset")
	}
	if req.Method != "" {
		t.Error("Method should be empty after reset")
	}
	if len(req.Headers) != 0 {
		t.Error("Headers should be empty after reset")
	}
	if len(req.Body) != 0 {
		t.Error("Body should be empty after reset")
	}
	if len(req.Meta) != 0 {
		t.Error("Meta should be empty after reset")
	}
	if req.Priority != 0 {
		t.Error("Priority should be 0 after reset")
	}
}

func TestPooledResponse_Reset(t *testing.T) {
	resp := &PooledResponse{
		Headers: make(http.Header),
	}

	resp.URL, _ = url.Parse("http://test.com")
	resp.Status = 404
	resp.Headers.Set("X-Custom", "value")
	resp.Body = []byte("not found")

	resp.Reset()

	if resp.URL != nil {
		t.Error("URL should be nil after reset")
	}
	if resp.Status != 0 {
		t.Error("Status should be 0 after reset")
	}
	if len(resp.Headers) != 0 {
		t.Error("Headers should be empty after reset")
	}
	if len(resp.Body) != 0 {
		t.Error("Body should be empty after reset")
	}
}

// TestRequestPool_Concurrent 验证并发安全性。
func TestRequestPool_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				req := RequestPool.Get()
				req.Method = "GET"
				req.Priority = j
				RequestPool.Put(req)
			}
		}()
	}
	wg.Wait()
}

// TestResponsePool_Concurrent 验证并发安全性。
func TestResponsePool_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				resp := ResponsePool.Get()
				resp.Status = 200
				ResponsePool.Put(resp)
			}
		}()
	}
	wg.Wait()
}

// BenchmarkRequestPool 基准测试对比 Pool 与直接分配的性能。
func BenchmarkRequestPool(b *testing.B) {
	b.Run("Pool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := RequestPool.Get()
			req.Method = "GET"
			RequestPool.Put(req)
		}
	})

	b.Run("Direct", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := &PooledRequest{
				Headers: make(http.Header),
				Meta:    make(map[string]any),
			}
			req.Method = "GET"
			_ = req
		}
	})
}

// BenchmarkResponsePool 基准测试对比 Pool 与直接分配的性能。
func BenchmarkResponsePool(b *testing.B) {
	b.Run("Pool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp := ResponsePool.Get()
			resp.Status = 200
			ResponsePool.Put(resp)
		}
	})

	b.Run("Direct", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp := &PooledResponse{
				Headers: make(http.Header),
			}
			resp.Status = 200
			_ = resp
		}
	})
}
