package server

import (
	"net/http"
	"testing"
	"time"
)

func TestBenchServer_StartAndClose(t *testing.T) {
	srv := New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close()

	if addr == "" {
		t.Fatal("expected non-empty address")
	}

	// 验证服务器可响应
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("failed to get root: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestBenchServer_Endpoints(t *testing.T) {
	srv := New(WithResponseSize(2048))
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close()

	baseURL := "http://" + addr

	tests := []struct {
		name     string
		path     string
		wantCode int
	}{
		{"root", "/", http.StatusOK},
		{"html", "/html", http.StatusOK},
		{"json", "/json", http.StatusOK},
		{"empty", "/empty", http.StatusOK},
		{"stats", "/stats", http.StatusOK},
		{"reset", "/reset", http.StatusOK},
		{"links", "/links/5", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(baseURL + tt.path)
			if err != nil {
				t.Fatalf("failed to get %s: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantCode {
				t.Errorf("path %s: expected status %d, got %d", tt.path, tt.wantCode, resp.StatusCode)
			}
		})
	}
}

func TestBenchServer_Latency(t *testing.T) {
	srv := New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close()

	start := time.Now()
	resp, err := http.Get("http://" + addr + "/latency?ms=100")
	if err != nil {
		t.Fatalf("failed to get latency endpoint: %v", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if elapsed < 90*time.Millisecond {
		t.Errorf("expected at least 90ms latency, got %v", elapsed)
	}
}

func TestBenchServer_Stats(t *testing.T) {
	srv := New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close()

	// 发送几个请求
	for i := 0; i < 5; i++ {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		resp.Body.Close()
	}

	stats := srv.GetStats()
	if stats.TotalRequests != 5 {
		t.Errorf("expected 5 total requests, got %d", stats.TotalRequests)
	}

	// 重置统计
	srv.ResetStats()
	stats = srv.GetStats()
	if stats.TotalRequests != 0 {
		t.Errorf("expected 0 total requests after reset, got %d", stats.TotalRequests)
	}
}

func TestBenchServer_BaseURL(t *testing.T) {
	srv := New()
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close()

	baseURL := srv.BaseURL()
	if baseURL != "http://"+addr {
		t.Errorf("expected base URL http://%s, got %s", addr, baseURL)
	}
}
