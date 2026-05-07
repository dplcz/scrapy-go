// Package server 提供用于性能基准测试的本地 HTTP 服务器。
//
// 该服务器设计为轻量级、低开销，用于测量 scrapy-go 框架的吞吐量（QPS）
// 和内存效率，而不受外部网络因素影响。
//
// 对应 Scrapy 项目中 extras/qps-bench-server.py 的功能，
// 但使用 Go 标准库实现以获得更高的服务端性能，确保瓶颈在客户端框架。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// BenchServer 是用于性能基准测试的本地 HTTP 服务器。
// 提供多种端点用于不同的测试场景。
type BenchServer struct {
	server   *http.Server
	listener net.Listener

	// 统计指标
	totalRequests  atomic.Int64
	concurrent     atomic.Int64
	maxConcurrent  atomic.Int64
	startTime      time.Time

	// 用于 QPS 计算的滑动窗口
	mu          sync.Mutex
	requestTimes []time.Time

	// 配置
	responseBody []byte
}

// Option 是 BenchServer 的配置选项。
type Option func(*BenchServer)

// WithResponseSize 设置响应体大小（字节数）。
// 服务器将返回指定大小的随机 HTML 内容。
func WithResponseSize(size int) Option {
	return func(s *BenchServer) {
		s.responseBody = generateHTML(size)
	}
}

// Stats 包含服务器的运行时统计信息。
type Stats struct {
	TotalRequests int64   `json:"total_requests"`
	Concurrent    int64   `json:"concurrent"`
	MaxConcurrent int64   `json:"max_concurrent"`
	Uptime        string  `json:"uptime"`
	QPS           float64 `json:"qps"`
}

// New 创建一个新的 BenchServer。
func New(opts ...Option) *BenchServer {
	s := &BenchServer{
		requestTimes: make([]time.Time, 0, 10000),
		responseBody: generateHTML(1024), // 默认 1KB 响应
	}

	for _, opt := range opts {
		opt(s)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/html", s.handleHTML)
	mux.HandleFunc("/json", s.handleJSON)
	mux.HandleFunc("/empty", s.handleEmpty)
	mux.HandleFunc("/latency", s.handleLatency)
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/reset", s.handleReset)
	mux.HandleFunc("/links/", s.handleLinks)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// Start 启动服务器并返回监听地址。
// 使用随机端口以避免端口冲突。
func (s *BenchServer) Start() (string, error) {
	var err error
	s.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to listen: %w", err)
	}

	s.startTime = time.Now()

	go func() {
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			// 服务器异常退出，忽略（测试中会通过 Close 正常关闭）
		}
	}()

	return s.listener.Addr().String(), nil
}

// StartOnPort 在指定端口启动服务器。
func (s *BenchServer) StartOnPort(port int) (string, error) {
	var err error
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.startTime = time.Now()

	go func() {
		if err := s.server.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			// 服务器异常退出
		}
	}()

	return s.listener.Addr().String(), nil
}

// Close 优雅关闭服务器。
func (s *BenchServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// Addr 返回服务器监听地址。
func (s *BenchServer) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// BaseURL 返回服务器的基础 URL。
func (s *BenchServer) BaseURL() string {
	return "http://" + s.Addr()
}

// GetStats 返回当前统计信息。
func (s *BenchServer) GetStats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()

	qps := s.calculateQPS()
	return Stats{
		TotalRequests: s.totalRequests.Load(),
		Concurrent:    s.concurrent.Load(),
		MaxConcurrent: s.maxConcurrent.Load(),
		Uptime:        time.Since(s.startTime).String(),
		QPS:           qps,
	}
}

// ResetStats 重置统计信息。
func (s *BenchServer) ResetStats() {
	s.totalRequests.Store(0)
	s.concurrent.Store(0)
	s.maxConcurrent.Store(0)
	s.startTime = time.Now()

	s.mu.Lock()
	s.requestTimes = s.requestTimes[:0]
	s.mu.Unlock()
}

// ============================================================================
// HTTP 处理器
// ============================================================================

// handleRoot 处理根路径请求，返回最小响应。
// 用于纯 QPS 吞吐量测试（最小服务端开销）。
func (s *BenchServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	s.trackRequest()
	defer s.trackRequestDone()

	// 支持可选的延迟参数
	if latencyStr := r.URL.Query().Get("latency"); latencyStr != "" {
		if latency, err := strconv.ParseFloat(latencyStr, 64); err == nil && latency > 0 {
			time.Sleep(time.Duration(latency * float64(time.Second)))
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<html><body>ok</body></html>"))
}

// handleHTML 返回预生成的 HTML 响应。
// 用于测试包含 HTML 解析的场景。
func (s *BenchServer) handleHTML(w http.ResponseWriter, r *http.Request) {
	s.trackRequest()
	defer s.trackRequestDone()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(s.responseBody)
}

// handleJSON 返回 JSON 响应。
// 用于测试 JSON 解析场景。
func (s *BenchServer) handleJSON(w http.ResponseWriter, r *http.Request) {
	s.trackRequest()
	defer s.trackRequestDone()

	data := map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UnixMilli(),
		"data": map[string]any{
			"id":    rand.Intn(10000),
			"title": "Benchmark Test Item",
			"price": rand.Float64() * 100,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
}

// handleEmpty 返回空响应（仅 200 状态码）。
// 用于测量框架纯开销（无响应体处理）。
func (s *BenchServer) handleEmpty(w http.ResponseWriter, r *http.Request) {
	s.trackRequest()
	defer s.trackRequestDone()

	w.WriteHeader(http.StatusOK)
}

// handleLatency 返回带有可配置延迟的响应。
// 通过 ?ms=100 参数指定延迟毫秒数。
func (s *BenchServer) handleLatency(w http.ResponseWriter, r *http.Request) {
	s.trackRequest()
	defer s.trackRequestDone()

	msStr := r.URL.Query().Get("ms")
	if msStr == "" {
		msStr = "100"
	}

	ms, err := strconv.Atoi(msStr)
	if err != nil || ms < 0 {
		ms = 100
	}

	time.Sleep(time.Duration(ms) * time.Millisecond)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<html><body>delayed response</body></html>"))
}

// handleStats 返回服务器统计信息。
func (s *BenchServer) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.GetStats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleReset 重置服务器统计信息。
func (s *BenchServer) handleReset(w http.ResponseWriter, r *http.Request) {
	s.ResetStats()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("stats reset"))
}

// handleLinks 返回包含多个链接的页面。
// 路径格式：/links/{n} 返回包含 n 个链接的页面。
// 用于测试链接提取和跟踪场景。
func (s *BenchServer) handleLinks(w http.ResponseWriter, r *http.Request) {
	s.trackRequest()
	defer s.trackRequestDone()

	// 解析链接数量
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/links/"), "/")
	n := 10
	if len(parts) > 0 {
		if parsed, err := strconv.Atoi(parts[0]); err == nil && parsed > 0 {
			n = parsed
		}
	}

	// 限制最大链接数
	if n > 1000 {
		n = 1000
	}

	var sb strings.Builder
	sb.WriteString("<html><body><h1>Links Page</h1><ul>")
	for i := 0; i < n; i++ {
		sb.WriteString(fmt.Sprintf(`<li><a href="/page/%d">Page %d</a></li>`, i, i))
	}
	sb.WriteString("</ul></body></html>")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(sb.String()))
}

// ============================================================================
// 内部方法
// ============================================================================

// trackRequest 记录请求开始。
func (s *BenchServer) trackRequest() {
	s.totalRequests.Add(1)
	current := s.concurrent.Add(1)

	// 更新最大并发数（CAS 循环）
	for {
		max := s.maxConcurrent.Load()
		if current <= max {
			break
		}
		if s.maxConcurrent.CompareAndSwap(max, current) {
			break
		}
	}

	// 记录请求时间（用于 QPS 计算）
	s.mu.Lock()
	s.requestTimes = append(s.requestTimes, time.Now())
	s.mu.Unlock()
}

// trackRequestDone 记录请求完成。
func (s *BenchServer) trackRequestDone() {
	s.concurrent.Add(-1)
}

// calculateQPS 计算最近 3 秒内的 QPS。
// 必须在持有 mu 锁的情况下调用。
func (s *BenchServer) calculateQPS() float64 {
	now := time.Now()
	cutoff := now.Add(-3 * time.Second)

	// 找到窗口起始位置
	startIdx := 0
	for startIdx < len(s.requestTimes) && s.requestTimes[startIdx].Before(cutoff) {
		startIdx++
	}

	// 清理过期数据
	if startIdx > 0 {
		s.requestTimes = s.requestTimes[startIdx:]
	}

	count := len(s.requestTimes)
	if count == 0 {
		return 0
	}

	elapsed := now.Sub(s.requestTimes[0]).Seconds()
	if elapsed <= 0 {
		return float64(count)
	}

	return float64(count) / elapsed
}

// generateHTML 生成指定大小的 HTML 内容。
func generateHTML(size int) []byte {
	if size <= 0 {
		size = 1024
	}

	header := "<html><head><title>Benchmark Page</title></head><body>"
	footer := "</body></html>"
	overhead := len(header) + len(footer)

	if size <= overhead {
		return []byte(header + footer)
	}

	// 填充内容
	contentSize := size - overhead
	var sb strings.Builder
	sb.Grow(size)
	sb.WriteString(header)

	// 生成段落填充
	paragraph := "<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.</p>\n"
	for sb.Len()+len(paragraph)+len(footer) <= size {
		sb.WriteString(paragraph)
	}

	// 填充剩余字节
	remaining := contentSize - (sb.Len() - len(header))
	if remaining > 0 {
		sb.WriteString(strings.Repeat("x", remaining))
	}

	sb.WriteString(footer)
	return []byte(sb.String())
}
