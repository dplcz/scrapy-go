package debug

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func getFreePort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

func TestPprofExtension_OpenClose(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	addr := fmt.Sprintf(":%d", port)
	ext := NewPprofExtension(WithPprofAddr(addr))

	ctx := context.Background()

	// Open
	if err := ext.Open(ctx); err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// 等待服务器启动
	time.Sleep(50 * time.Millisecond)

	// 验证 pprof 端点可访问
	resp, err := http.Get(fmt.Sprintf("http://localhost%s/debug/pprof/", addr))
	if err != nil {
		t.Fatalf("failed to access pprof endpoint: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Close
	if err := ext.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// 验证服务器已关闭
	time.Sleep(50 * time.Millisecond)
	_, err = http.Get(fmt.Sprintf("http://localhost%s/debug/pprof/", addr))
	if err == nil {
		t.Error("expected error after server close, but got none")
	}
}

func TestPprofExtension_PortInUse(t *testing.T) {
	// 先占用一个端口
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// 尝试在同一端口启动 pprof
	ext := NewPprofExtension(WithPprofAddr(addr))

	ctx := context.Background()

	// Open 不应返回错误（端口被占用时只记录警告）
	if err := ext.Open(ctx); err != nil {
		t.Fatalf("Open should not fail when port is in use: %v", err)
	}

	// Close 应该安全
	if err := ext.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestPprofExtension_DefaultAddr(t *testing.T) {
	ext := NewPprofExtension()
	if ext.addr != ":6060" {
		t.Errorf("expected default addr :6060, got %s", ext.addr)
	}
}

func TestPprofExtension_CloseWithoutOpen(t *testing.T) {
	ext := NewPprofExtension()
	ctx := context.Background()

	// Close without Open should not panic
	if err := ext.Close(ctx); err != nil {
		t.Fatalf("Close without Open should not fail: %v", err)
	}
}

func TestPprofExtension_HeapEndpoint(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}

	addr := fmt.Sprintf(":%d", port)
	ext := NewPprofExtension(WithPprofAddr(addr))

	ctx := context.Background()
	if err := ext.Open(ctx); err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer ext.Close(ctx)

	time.Sleep(50 * time.Millisecond)

	// 验证 heap 端点
	resp, err := http.Get(fmt.Sprintf("http://localhost%s/debug/pprof/heap", addr))
	if err != nil {
		t.Fatalf("failed to access heap endpoint: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for heap, got %d", resp.StatusCode)
	}
}
