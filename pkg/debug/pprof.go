// Package debug 提供调试和性能分析工具。
//
// 包含 pprof HTTP 端点扩展，允许在运行时通过标准 Go pprof 工具
// 分析 CPU、内存、goroutine 等性能指标。
// 这是 Go 特有的调试手段，Scrapy 无此功能。
package debug

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // 注册 pprof HTTP 处理器
	"time"
)

// PprofExtension 提供 pprof HTTP 端点，用于运行时性能分析。
//
// 通过 PPROF_ENABLED 配置控制是否启用。
// 启用后在指定端口（默认 6060）提供标准 Go pprof 端点：
//   - /debug/pprof/         — 索引页
//   - /debug/pprof/profile  — CPU profile
//   - /debug/pprof/heap     — 堆内存 profile
//   - /debug/pprof/goroutine — goroutine 堆栈
//   - /debug/pprof/trace    — 执行 trace
//
// 使用方式：
//
//	go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
//	go tool pprof http://localhost:6060/debug/pprof/heap
type PprofExtension struct {
	server *http.Server
	addr   string
	logger *slog.Logger
}

// PprofOption 是 PprofExtension 的配置选项。
type PprofOption func(*PprofExtension)

// WithPprofAddr 设置 pprof 监听地址。
func WithPprofAddr(addr string) PprofOption {
	return func(p *PprofExtension) {
		p.addr = addr
	}
}

// WithPprofLogger 设置日志记录器。
func WithPprofLogger(logger *slog.Logger) PprofOption {
	return func(p *PprofExtension) {
		p.logger = logger
	}
}

// NewPprofExtension 创建一个新的 pprof 扩展。
// 默认监听地址为 :6060。
func NewPprofExtension(opts ...PprofOption) *PprofExtension {
	p := &PprofExtension{
		addr:   ":6060",
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Open 启动 pprof HTTP 服务器。
func (p *PprofExtension) Open(ctx context.Context) error {
	mux := http.DefaultServeMux

	p.server = &http.Server{
		Addr:              p.addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 尝试监听端口
	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		p.logger.Warn("failed to start pprof server, port may be in use",
			"addr", p.addr,
			"error", err,
		)
		return nil // 不阻止爬虫启动
	}

	p.logger.Info("pprof server started", "addr", fmt.Sprintf("http://%s/debug/pprof/", p.addr))

	go func() {
		if err := p.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			p.logger.Error("pprof server error", "error", err)
		}
	}()

	return nil
}

// Close 关闭 pprof HTTP 服务器。
func (p *PprofExtension) Close(ctx context.Context) error {
	if p.server == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := p.server.Shutdown(shutdownCtx); err != nil {
		p.logger.Error("failed to shutdown pprof server", "error", err)
		return err
	}

	p.logger.Debug("pprof server stopped")
	return nil
}
