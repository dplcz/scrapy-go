package proxy

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
)

// pool 是 Pool 接口的默认实现。
//
// 设计要点：
//   - 使用 sync.RWMutex 保护代理切片：读多写少（每次 Get 仅读，Refresh 才写）
//   - 单个代理的状态字段使用 atomic 操作，无需获取 mu 锁即可更新
//   - 健康检查独立 goroutine + ctx 控制生命周期，Close 时统一回收
//   - Get 路径不分配内存（候选代理切片复用 sync.Pool）
type pool struct {
	opts     *Options
	strategy Strategy
	provider Provider
	logger   *slog.Logger

	mu      sync.RWMutex
	proxies []*Proxy
	// proxyByURL 用于 Refresh 时按 URL 增量合并，避免重置已有代理的统计。
	proxyByURL map[string]*Proxy

	// candPool 复用 Get 路径的候选切片，减少内存分配。
	candPool sync.Pool

	// 健康检查器与生命周期控制
	checker *HealthChecker
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	closed bool
}

// NewPool 创建一个新的代理池。
//
// 工作流：
//  1. 校验/规范化配置
//  2. 调用 Provider.Fetch 加载初始代理列表
//  3. 启动后台健康检查（如启用）
//
// provider 不能为 nil；如需多来源使用 NewCompositeProvider 组合。
func NewPool(opts *Options, provider Provider) (Pool, error) {
	return NewPoolWithLogger(opts, provider, nil)
}

// NewPoolWithLogger 创建一个新的代理池，并指定日志记录器。
func NewPoolWithLogger(opts *Options, provider Provider, logger *slog.Logger) (Pool, error) {
	if provider == nil {
		return nil, errors.New("proxy: provider is required")
	}
	if opts == nil {
		opts = DefaultOptions()
	}
	opts.normalize()
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	p := &pool{
		opts:       opts,
		strategy:   newStrategyByKind(opts.Strategy),
		provider:   provider,
		logger:     logger,
		proxyByURL: make(map[string]*Proxy),
	}
	p.candPool.New = func() any {
		// 经验值：典型代理池大小 < 64
		s := make([]*Proxy, 0, 16)
		return &s
	}

	// 初始加载
	initCtx, initCancel := context.WithTimeout(context.Background(), opts.HealthCheckTimeout*2)
	defer initCancel()
	if err := p.Refresh(initCtx); err != nil {
		return nil, fmt.Errorf("proxy: initial refresh: %w", err)
	}

	// 创建统一的根 context 控制所有后台 goroutine
	needBackground := opts.HealthCheckEnabled || opts.ProviderRefreshInterval > 0
	var rootCtx context.Context
	if needBackground {
		rootCtx, p.cancel = context.WithCancel(context.Background())
	}

	// 启动后台健康检查
	if opts.HealthCheckEnabled {
		p.checker = newHealthChecker(p, opts, logger)
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.checker.run(rootCtx)
		}()
	}

	// 启动周期性 Provider 刷新
	if opts.ProviderRefreshInterval > 0 {
		p.wg.Add(1)
		go p.runProviderRefresh(rootCtx)
	}

	return p, nil
}

// Get 选择一个健康代理。
func (p *pool) Get(ctx context.Context) (*Proxy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, ErrPoolClosed
	}
	all := p.proxies
	p.mu.RUnlock()

	if len(all) == 0 {
		return nil, ErrNoProxy
	}

	// 复用候选切片
	candPtr := p.candPool.Get().(*[]*Proxy)
	cands := (*candPtr)[:0]
	defer func() {
		*candPtr = cands
		p.candPool.Put(candPtr)
	}()

	for _, pr := range all {
		state := pr.State()
		if state == StateUnhealthy {
			continue
		}
		cands = append(cands, pr)
	}

	if len(cands) == 0 {
		return nil, ErrNoProxy
	}

	chosen := p.strategy.Select(cands)
	if chosen == nil {
		return nil, ErrNoProxy
	}
	chosen.markUsed()
	return chosen, nil
}

// Mark 反馈一次代理使用结果。
func (p *pool) Mark(proxy *Proxy, success bool) {
	if proxy == nil {
		return
	}
	if success {
		proxy.markSuccess()
		// 成功后从 Degraded 恢复为 Healthy
		if proxy.State() == StateDegraded {
			proxy.SetState(StateHealthy)
		}
		return
	}
	failures := proxy.markFailure()
	switch {
	case failures >= int64(p.opts.MaxFailures):
		proxy.SetState(StateUnhealthy)
		p.logger.Warn("proxy marked unhealthy",
			"proxy", proxy.URL,
			"failures", failures,
		)
	case failures >= int64(p.opts.MaxFailures/2)+1:
		// 失败次数过半时降级为 Degraded（仍可使用但建议降权）
		if proxy.State() == StateHealthy {
			proxy.SetState(StateDegraded)
		}
	}
}

// Refresh 触发一次代理列表刷新（从 Provider 重新拉取）。
//
// 增量合并语义：
//   - 已存在的代理保持原有统计与状态
//   - 新增代理添加到池中（StateHealthy）
//   - 不在新列表中的代理被移除（释放）
func (p *pool) Refresh(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	rawList, err := p.provider.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("proxy refresh: fetch from %q: %w", p.provider.Name(), err)
	}

	// 解析每个代理 URL，提取认证信息
	type parsed struct {
		key   string // 用于去重和合并：不含认证的 URL
		entry *Proxy // 仅当为新代理时构造
		raw   string // 原始字符串（仅用于错误日志）
	}
	parsedList := make([]parsed, 0, len(rawList))
	for _, raw := range rawList {
		key, creds, weight, perr := parseProxyEntry(raw)
		if perr != nil {
			p.logger.Warn("proxy refresh: invalid proxy entry",
				"raw", raw,
				"error", perr,
			)
			continue
		}
		parsedList = append(parsedList, parsed{
			key: key,
			entry: &Proxy{
				URL:         key,
				Credentials: creds,
				Weight:      weight,
			},
			raw: raw,
		})
	}

	// 加锁更新 proxies
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrPoolClosed
	}

	newByURL := make(map[string]*Proxy, len(parsedList))
	newProxies := make([]*Proxy, 0, len(parsedList))

	for _, item := range parsedList {
		if existing, ok := p.proxyByURL[item.key]; ok {
			// 已存在则保留统计，仅更新可能变化的元数据
			existing.Weight = item.entry.Weight
			if item.entry.Credentials != "" {
				existing.Credentials = item.entry.Credentials
			}
			newByURL[item.key] = existing
			newProxies = append(newProxies, existing)
		} else {
			newByURL[item.key] = item.entry
			newProxies = append(newProxies, item.entry)
		}
	}

	p.proxies = newProxies
	p.proxyByURL = newByURL
	return nil
}

// Snapshots 返回当前所有代理的只读快照。
func (p *pool) Snapshots() []Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]Snapshot, len(p.proxies))
	for i, pr := range p.proxies {
		out[i] = pr.Snapshot()
	}
	return out
}

// Size 返回代理池总大小。
func (p *pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.proxies)
}

// Healthy 返回当前健康代理数量。
func (p *pool) Healthy() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, pr := range p.proxies {
		if pr.State() == StateHealthy {
			count++
		}
	}
	return count
}

// Close 关闭代理池，停止所有后台 goroutine。
// 多次调用安全。
func (p *pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	return nil
}

// runProviderRefresh 后台周期性调用 Provider.Fetch 刷新代理列表。
// 由 NewPool 在 ProviderRefreshInterval > 0 时启动。
// rootCtx 取消时退出。
func (p *pool) runProviderRefresh(rootCtx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.opts.ProviderRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rootCtx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(rootCtx,
				p.opts.HealthCheckTimeout*3)
			if err := p.Refresh(refreshCtx); err != nil && !errors.Is(err, context.Canceled) {
				p.logger.Warn("proxy provider refresh failed", "error", err)
			}
			cancel()
		}
	}
}

// parseProxyEntry 解析单个代理 URL 字符串。
//
// 输入支持以下格式：
//
//   - "http://host:port"
//   - "http://user:pass@host:port"
//   - "host:port"（默认 http）
//   - "http://host:port|weight"（管道分隔权重）
//
// 返回：不含认证的 URL、Base64 认证、权重。
func parseProxyEntry(raw string) (proxyURL, credentials string, weight int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", 0, ErrInvalidProxy
	}

	// 解析权重（| 分隔）
	weight = 1
	if idx := strings.LastIndex(raw, "|"); idx > 0 {
		wStr := strings.TrimSpace(raw[idx+1:])
		raw = strings.TrimSpace(raw[:idx])
		if wStr != "" {
			var n int
			if _, e := fmt.Sscanf(wStr, "%d", &n); e == nil && n > 0 {
				weight = n
			}
		}
	}

	// 补 scheme
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	u, perr := url.Parse(raw)
	if perr != nil {
		return "", "", 0, fmt.Errorf("%w: %v", ErrInvalidProxy, perr)
	}
	if u.Host == "" {
		return "", "", 0, ErrInvalidProxy
	}

	// 提取认证
	if u.User != nil {
		username := u.User.Username()
		password, _ := u.User.Password()
		credentials = base64.StdEncoding.EncodeToString(
			[]byte(username + ":" + password))
		u.User = nil
	}

	proxyURL = u.String()
	return proxyURL, credentials, weight, nil
}
