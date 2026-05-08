package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// BenchmarkHTTPDownloadHandler_Download 测试 HTTPDownloadHandler.Download 的整体性能。
func BenchmarkHTTPDownloadHandler_Download(b *testing.B) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "<html><body>Hello World</body></html>")
	}))
	defer server.Close()

	handler := NewHTTPDownloadHandler(10 * time.Second)
	defer handler.Close()

	req, _ := shttp.NewRequest(server.URL + "/test/page")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := handler.Download(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		_ = resp
	}
}

// BenchmarkHTTPDownloadHandler_Download_Parallel 测试并发场景下的性能。
func BenchmarkHTTPDownloadHandler_Download_Parallel(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "<html><body>Hello World</body></html>")
	}))
	defer server.Close()

	handler := NewHTTPDownloadHandler(10 * time.Second)
	defer handler.Close()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		req, _ := shttp.NewRequest(server.URL + "/test/page")
		for pb.Next() {
			resp, err := handler.Download(context.Background(), req)
			if err != nil {
				b.Fatal(err)
			}
			_ = resp
		}
	})
}

// BenchmarkHTTPDownloadHandler_URLParsing 专门测试 URL 解析的开销。
// 这个 benchmark 隔离了 URL 序列化+反序列化的开销。
func BenchmarkHTTPDownloadHandler_URLParsing(b *testing.B) {
	req, _ := shttp.NewRequest("http://example.com/path/to/resource?key=value&foo=bar")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 模拟当前实现：URL.String() + http.NewRequestWithContext（内部会再次解析URL）
		httpReq, err := http.NewRequestWithContext(context.Background(), req.Method, req.URL.String(), nil)
		if err != nil {
			b.Fatal(err)
		}
		_ = httpReq
	}
}

// BenchmarkHTTPDownloadHandler_URLDirect 测试直接构造 http.Request 避免重复解析的性能。
func BenchmarkHTTPDownloadHandler_URLDirect(b *testing.B) {
	req, _ := shttp.NewRequest("http://example.com/path/to/resource?key=value&foo=bar")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// 优化方案：直接构造 http.Request，避免 URL 序列化+反序列化
		httpReq := &http.Request{
			Method: req.Method,
			URL:    req.URL,
			Host:   req.URL.Host,
			Header: make(http.Header),
		}
		httpReq = httpReq.WithContext(context.Background())
		_ = httpReq
	}
}

// BenchmarkHTTPDownloadHandler_RequestBuild 测试完整请求构建过程（含 Headers/Cookies）。
func BenchmarkHTTPDownloadHandler_RequestBuild(b *testing.B) {
	req, _ := shttp.NewRequest("http://example.com/path/to/resource?key=value&foo=bar",
		shttp.WithHeaders(http.Header{
			"User-Agent":      {"ScrapyGo/1.0"},
			"Accept":          {"text/html"},
			"Accept-Language": {"en-US"},
		}),
		shttp.WithCookies([]*http.Cookie{
			{Name: "session", Value: "abc123"},
			{Name: "token", Value: "xyz789"},
		}),
	)

	b.Run("CurrentImpl", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			httpReq, _ := http.NewRequestWithContext(context.Background(), req.Method, req.URL.String(), nil)
			for key, values := range req.Headers {
				for _, v := range values {
					httpReq.Header.Add(key, v)
				}
			}
			for _, cookie := range req.Cookies {
				httpReq.AddCookie(cookie)
			}
			_ = httpReq
		}
	})

	b.Run("OptimizedImpl", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			httpReq := &http.Request{
				Method: req.Method,
				URL:    req.URL,
				Host:   req.URL.Host,
				Header: make(http.Header, len(req.Headers)+1),
			}
			httpReq = httpReq.WithContext(context.Background())
			for key, values := range req.Headers {
				for _, v := range values {
					httpReq.Header.Add(key, v)
				}
			}
			for _, cookie := range req.Cookies {
				httpReq.AddCookie(cookie)
			}
			_ = httpReq
		}
	})
}

// BenchmarkHTTPDownloadHandler_BodyRead 测试响应体读取的开销。
func BenchmarkHTTPDownloadHandler_BodyRead(b *testing.B) {
	// 模拟不同大小的响应体
	sizes := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
	}

	for _, s := range sizes {
		b.Run(s.name, func(b *testing.B) {
			body := make([]byte, s.size)
			for i := range body {
				body[i] = 'a'
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprintf("%d", s.size))
				w.WriteHeader(http.StatusOK)
				w.Write(body)
			}))
			defer server.Close()

			handler := NewHTTPDownloadHandler(10 * time.Second)
			defer handler.Close()

			req, _ := shttp.NewRequest(server.URL + "/test")

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				resp, err := handler.Download(context.Background(), req)
				if err != nil {
					b.Fatal(err)
				}
				_ = resp
			}
		})
	}
}

// BenchmarkHTTPDownloadHandler_WithBody 测试带请求体的下载性能。
func BenchmarkHTTPDownloadHandler_WithBody(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	}))
	defer server.Close()

	handler := NewHTTPDownloadHandler(10 * time.Second)
	defer handler.Close()

	reqBody := []byte(`{"key": "value", "data": "some test data for benchmarking"}`)
	req, _ := shttp.NewRequest(server.URL+"/api",
		shttp.WithMethod("POST"),
		shttp.WithBody(reqBody),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := handler.Download(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		_ = resp
	}
}
