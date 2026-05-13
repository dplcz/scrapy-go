package web

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	sslog "github.com/dplcz/scrapy-go/pkg/log"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// runningSpider 跟踪一个正在运行的 Spider 实例。
type runningSpider struct {
	// name 是 Spider 注册名称
	name string

	// id 是本次运行的唯一标识（同名 Spider 可多次启动）
	id string

	// crawler 是本次运行的 Crawler 实例
	crawler *crawler.Crawler

	// spider 是本次运行的 Spider 实例
	spider spider.Spider

	// startTime 是启动时间
	startTime time.Time

	// args 是用户通过 REST API 传入的启动项参数。
	// 这些参数会以 PriorityCmdline 优先级注入到 Crawler 的 Settings 中，
	// 覆盖所有其他级别的同名配置。
	args map[string]any

	// done 在爬虫完成时关闭
	done <-chan error
}

// Server 是 Web 管理 HTTP 服务器。
//
// 通过 REST API 提供 Spider 的注册、启动、停止和统计查询功能。
// 内部使用 crawler.Runner 管理多爬虫并发执行。
//
// 线程安全：所有公共方法均可被多个 goroutine 安全调用。
type Server struct {
	addr     string
	logger   *slog.Logger
	registry *Registry
	runner   *crawler.Runner

	// mu 保护 running 和 idCounter
	mu        sync.RWMutex
	running   map[string]*runningSpider // key: id
	idCounter uint64

	// srv 是底层 HTTP 服务器
	srv *http.Server
}

// ServerOption 是 Server 的可选配置函数。
type ServerOption func(*Server)

// WithLogger 设置 Server 的日志记录器。
func WithLogger(logger *slog.Logger) ServerOption {
	return func(s *Server) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithRegistry 设置自定义的 Spider 注册表。
// 若未设置，Server 会创建一个空的注册表。
func WithRegistry(r *Registry) ServerOption {
	return func(s *Server) {
		if r != nil {
			s.registry = r
		}
	}
}

// WithRunner 设置自定义的 Runner。
// 若未设置，Server 会创建一个默认 Runner（禁用 OS 信号处理，由 Server 统一管理）。
func WithRunner(r *crawler.Runner) ServerOption {
	return func(s *Server) {
		if r != nil {
			s.runner = r
		}
	}
}

// NewServer 创建一个新的 Web 管理服务器。
//
// addr 是 HTTP 监听地址（如 ":8080"）。
func NewServer(addr string, opts ...ServerOption) *Server {
	s := &Server{
		addr:    addr,
		running: make(map[string]*runningSpider),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.logger == nil {
		s.logger = sslog.NewColorLogger("INFO", nil, false)
	}
	if s.registry == nil {
		s.registry = NewRegistry()
	}
	if s.runner == nil {
		s.runner = crawler.NewRunner(
			crawler.WithOSSignalHandling(false),
			crawler.WithRunnerLogger(s.logger),
		)
	}
	return s
}

// Registry 返回 Server 的 Spider 注册表。
// 可用于在 Server 创建后继续注册 Spider。
func (s *Server) Registry() *Registry {
	return s.registry
}

// Register 是注册 Spider 工厂函数的便捷方法。
// 等价于 s.Registry().Register(name, factory, configurator...)。
func (s *Server) Register(name string, factory SpiderFactory, configurator ...CrawlerConfigurator) {
	s.registry.Register(name, factory, configurator...)
}

// ListenAndServe 启动 HTTP 服务器并阻塞直到 context 被取消。
//
// 当 ctx 被取消时，Server 会：
//  1. 优雅关闭 HTTP 服务器（等待活跃连接完成）
//  2. 停止所有正在运行的 Spider
//  3. 等待所有 Spider 退出
func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("web server: listen %s: %w", s.addr, err)
	}

	s.srv = &http.Server{Handler: mux}

	// 监听 context 取消，优雅关闭
	go func() {
		<-ctx.Done()
		s.logger.Info("web server shutting down...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("web server shutdown error", "error", err)
		}

		// 停止所有正在运行的 Spider
		s.runner.Stop()
		s.runner.Wait()
	}()

	s.logger.Info("web server started", "addr", listener.Addr().String())

	if err := s.srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("web server: serve: %w", err)
	}
	return nil
}

// Close 优雅关闭 Server，停止所有 Spider 并关闭 HTTP 服务器。
func (s *Server) Close() error {
	if s.srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	s.runner.Close()
	return nil
}

// ============================================================================
// Spider 生命周期管理
// ============================================================================

// startSpider 启动指定名称的 Spider，返回运行 ID。
//
// args 是用户通过 REST API 传入的可选启动项参数。
// 非 nil 的 args 会以 PriorityCmdline（最高优先级）注入到 Crawler 的 Settings 中，
// 覆盖所有其他级别的同名配置（包括 Spider.CustomSettings）。
func (s *Server) startSpider(ctx context.Context, name string, args map[string]any) (string, error) {
	factory, configurator, ok := s.registry.Get(name)
	if !ok {
		return "", fmt.Errorf("spider %q not registered", name)
	}

	// 创建新的 Spider 和 Crawler 实例
	sp := factory()
	if sp == nil {
		return "", fmt.Errorf("spider factory %q returned nil", name)
	}

	c := crawler.NewDefault()

	// 应用用户配置（Pipeline、扩展等）
	if configurator != nil {
		configurator(&crawlerConfigAdapter{c: c})
	}

	// 注入用户通过 REST API 传入的启动项参数
	if len(args) > 0 {
		if err := c.Settings.Update(args, settings.PriorityCmdline); err != nil {
			return "", fmt.Errorf("failed to apply args to settings: %w", err)
		}
	}

	// 生成唯一运行 ID
	s.mu.Lock()
	s.idCounter++
	id := fmt.Sprintf("%s-%d", name, s.idCounter)
	s.mu.Unlock()

	// 通过 Runner 异步启动
	done := s.runner.Crawl(ctx, c, sp)

	rs := &runningSpider{
		name:      name,
		id:        id,
		crawler:   c,
		spider:    sp,
		startTime: time.Now(),
		args:      args,
		done:      done,
	}

	s.mu.Lock()
	s.running[id] = rs
	s.mu.Unlock()

	// 后台清理：爬虫完成后从 running 中移除
	go func() {
		<-done
		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
		s.logger.Info("spider finished, removed from running list", "id", id, "name", name)
	}()

	s.logger.Info("spider started", "id", id, "name", name)
	return id, nil
}

// stopSpider 停止指定 ID 的 Spider。
func (s *Server) stopSpider(id string) error {
	s.mu.RLock()
	rs, ok := s.running[id]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("running spider %q not found", id)
	}

	rs.crawler.Stop()
	s.logger.Info("spider stop requested", "id", id, "name", rs.name)
	return nil
}

// stopSpiderByName 停止指定名称的所有正在运行的 Spider。
func (s *Server) stopSpiderByName(name string) (int, error) {
	s.mu.RLock()
	var targets []*runningSpider
	for _, rs := range s.running {
		if rs.name == name {
			targets = append(targets, rs)
		}
	}
	s.mu.RUnlock()

	if len(targets) == 0 {
		return 0, fmt.Errorf("no running spider with name %q", name)
	}

	for _, rs := range targets {
		rs.crawler.Stop()
		s.logger.Info("spider stop requested", "id", rs.id, "name", rs.name)
	}
	return len(targets), nil
}

// getSpiderStats 获取指定名称的 Spider 统计数据。
func (s *Server) getSpiderStats(name string) ([]SpiderStats, error) {
	s.mu.RLock()
	var results []SpiderStats
	for _, rs := range s.running {
		if rs.name == name {
			st := SpiderStats{
				ID:        rs.id,
				Name:      rs.name,
				StartTime: rs.startTime,
				Running:   rs.crawler.IsCrawling(),
				Args:      rs.args,
			}
			if rs.crawler.Stats != nil {
				st.Stats = rs.crawler.Stats.GetStats()
			}
			results = append(results, st)
		}
	}
	s.mu.RUnlock()

	if len(results) == 0 {
		// 检查是否注册过
		if !s.registry.Has(name) {
			return nil, fmt.Errorf("spider %q not registered", name)
		}
		return nil, nil
	}
	return results, nil
}

// listSpiders 列出所有已注册的 Spider 及其运行状态。
func (s *Server) listSpiders() []SpiderInfo {
	names := s.registry.Names()

	s.mu.RLock()
	// 统计每个名称的运行实例数
	runningCounts := make(map[string]int)
	for _, rs := range s.running {
		runningCounts[rs.name]++
	}
	s.mu.RUnlock()

	infos := make([]SpiderInfo, 0, len(names))
	for _, name := range names {
		infos = append(infos, SpiderInfo{
			Name:             name,
			RunningInstances: runningCounts[name],
		})
	}
	return infos
}

// ============================================================================
// 数据传输对象
// ============================================================================

// SpiderInfo 表示 Spider 的注册信息和运行状态。
type SpiderInfo struct {
	Name             string `json:"name"`
	RunningInstances int    `json:"running_instances"`
}

// SpiderStats 表示 Spider 的运行统计数据。
type SpiderStats struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	StartTime time.Time      `json:"start_time"`
	Running   bool           `json:"running"`
	Args      map[string]any `json:"args,omitempty"`
	Stats     map[string]any `json:"stats,omitempty"`
}

// ============================================================================
// crawlerConfigAdapter — CrawlerConfig 接口适配器
// ============================================================================

// crawlerConfigAdapter 将 *crawler.Crawler 适配为 CrawlerConfig 接口。
type crawlerConfigAdapter struct {
	c *crawler.Crawler
}

func (a *crawlerConfigAdapter) AddPipeline(p interface{}, name string, priority int) {
	// 类型断言：p 必须实现 pipeline.ItemPipeline 接口
	// 由于 contrib/web 不直接依赖 pipeline 包的具体类型，
	// 这里通过 Crawler 的 AddPipeline 方法间接调用
	if pp, ok := p.(pipelineInterface); ok {
		a.c.AddPipeline(pp, name, priority)
	}
}

func (a *crawlerConfigAdapter) AddExtension(ext interface{}, name string, priority int) {
	if ee, ok := ext.(extensionInterface); ok {
		a.c.AddExtension(ee, name, priority)
	}
}

// pipelineInterface 镜像 pipeline.ItemPipeline 接口，避免循环导入。
type pipelineInterface interface {
	Open(ctx context.Context) error
	Close(ctx context.Context) error
	ProcessItem(ctx context.Context, item any) (any, error)
}

// extensionInterface 镜像 extension.Extension 接口，避免循环导入。
type extensionInterface interface {
	Open(ctx context.Context) error
	Close(ctx context.Context) error
}

// ============================================================================
// 全局统计辅助
// ============================================================================

// getAllStats 获取所有正在运行的 Spider 的统计数据。
func (s *Server) getAllStats() []SpiderStats {
	s.mu.RLock()
	results := make([]SpiderStats, 0, len(s.running))
	for _, rs := range s.running {
		st := SpiderStats{
			ID:        rs.id,
			Name:      rs.name,
			StartTime: rs.startTime,
			Running:   rs.crawler.IsCrawling(),
			Args:      rs.args,
		}
		if rs.crawler.Stats != nil {
			st.Stats = rs.crawler.Stats.GetStats()
		}
		results = append(results, st)
	}
	s.mu.RUnlock()
	return results
}

// 确保 stats.Collector 接口在编译期被引用（避免 unused import）。
var _ stats.Collector = (*stats.MemoryCollector)(nil)
