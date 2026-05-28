package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// HealthChecker 后台周期性探测代理可用性。
//
// 工作模式：
//   - 每隔 HealthCheckInterval 触发一次全量探测
//   - 对每个代理使用 ProbeFunc 进行健康检查
//   - 探测并发执行，限制最大并发数避免一次性占用过多文件句柄
//   - 探测结果根据 RecoveryThreshold 决定状态恢复时机
//
// 退出语义：
//   - 通过 ctx 取消信号优雅退出
//   - 退出前等待所有进行中的探测完成
type HealthChecker struct {
	pool       *pool
	opts       *Options
	logger     *slog.Logger
	probe      ProbeFunc
	recoveries sync.Map // map[string]*atomic.Int32 — 每个代理连续成功次数
}

// ProbeFunc 定义健康探测函数签名。
//
// 实现者通过 proxy.URL 作为下一跳代理向 targetURL 发起请求，
// 返回 nil 表示代理健康；返回错误表示代理不可用。
//
// ctx 用于控制单次探测超时（HealthCheckTimeout）。
type ProbeFunc func(ctx context.Context, proxy *Proxy, targetURL string) error

// newHealthChecker 创建一个健康检查器，使用默认 HTTP 探测函数。
func newHealthChecker(p *pool, opts *Options, logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		pool:   p,
		opts:   opts,
		logger: logger,
		probe:  defaultProbe(opts),
	}
}

// run 是 HealthChecker 的主循环，由 NewPool 在 goroutine 中调用。
//
// 循环结构：
//
//	for {
//	    select {
//	    case <-ctx.Done(): return
//	    case <-ticker.C:  执行一轮探测
//	    }
//	}
//
// 退出：当 ctx 被取消时返回，确保等待已开启的探测完成。
func (h *HealthChecker) run(ctx context.Context) {
	ticker := time.NewTicker(h.opts.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkAll(ctx)
		}
	}
}

// checkAll 对池中所有代理执行一轮健康检查。
//
// 并发控制：使用 semaphore（缓冲 channel）限制最大并发探测数为 8，
// 避免大池子时一次性创建过多 socket 连接。
func (h *HealthChecker) checkAll(ctx context.Context) {
	h.pool.mu.RLock()
	if h.pool.closed {
		h.pool.mu.RUnlock()
		return
	}
	// 拷贝切片引用，避免长时间持有锁
	all := make([]*Proxy, len(h.pool.proxies))
	copy(all, h.pool.proxies)
	h.pool.mu.RUnlock()

	if len(all) == 0 {
		return
	}

	const maxConcurrency = 8
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

dispatch:
	for _, pr := range all {
		select {
		case <-ctx.Done():
			break dispatch
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(pr *Proxy) {
			defer wg.Done()
			defer func() { <-sem }()
			h.checkOne(ctx, pr)
		}(pr)
	}

	wg.Wait()
}

// checkOne 探测单个代理的可用性。
//
// 探测结果处理：
//   - 成功：累计连续成功次数，达到 RecoveryThreshold 后将 Unhealthy 恢复为 Healthy
//   - 失败：调用 pool.Mark 累计失败次数，达到 MaxFailures 后置为 Unhealthy
func (h *HealthChecker) checkOne(ctx context.Context, pr *Proxy) {
	probeCtx, cancel := context.WithTimeout(ctx, h.opts.HealthCheckTimeout)
	defer cancel()

	pr.markChecked()

	err := h.probe(probeCtx, pr, h.opts.HealthCheckURL)
	if err != nil {
		// 探测失败：通过 Mark 走统一的失败计数路径
		h.pool.Mark(pr, false)
		// 探测失败时重置恢复计数
		if v, ok := h.recoveries.Load(pr.URL); ok {
			v.(*atomic.Int32).Store(0)
		}
		h.logger.Debug("proxy health check failed",
			"proxy", pr.URL,
			"error", err,
		)
		return
	}

	// 探测成功
	if pr.State() == StateUnhealthy {
		// 累计连续成功次数
		v, _ := h.recoveries.LoadOrStore(pr.URL, &atomic.Int32{})
		count := v.(*atomic.Int32).Add(1)
		if count >= int32(h.opts.RecoveryThreshold) {
			pr.SetState(StateHealthy)
			v.(*atomic.Int32).Store(0)
			h.logger.Info("proxy recovered",
				"proxy", pr.URL,
				"successive_checks", count,
			)
		}
		return
	}

	// 健康代理探测成功：通过 Mark 重置失败计数（保持 successes 统计语义不变）
	// 注意：这里不调用 Mark，避免污染 successes 计数（successes 应反映业务请求成功）
	if pr.State() == StateDegraded {
		pr.SetState(StateHealthy)
	}
}

// defaultProbe 返回基于标准库 net/http 的默认探测函数。
//
// 实现要点：
//   - 每次探测构造独立的 http.Client，避免连接复用带来的状态污染
//   - 通过 http.Transport.Proxy 设置代理；认证通过 Proxy-Authorization 头注入
//   - 严格匹配 HealthCheckExpectedStatus；不匹配视为失败
func defaultProbe(opts *Options) ProbeFunc {
	expectedStatus := opts.HealthCheckExpectedStatus
	return func(ctx context.Context, p *Proxy, targetURL string) error {
		proxyURL, err := url.Parse(p.URL)
		if err != nil {
			return err
		}

		transport := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			// 单次探测使用独立连接，禁用 keep-alive 避免污染下次探测
			DisableKeepAlives: true,
		}
		client := &http.Client{
			Transport: transport,
			// Timeout 由 ctx 控制，此处不重复设置
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return err
		}
		if p.Credentials != "" {
			req.Header.Set("Proxy-Authorization", "Basic "+p.Credentials)
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != expectedStatus {
			return &probeStatusError{Status: resp.StatusCode, Expected: expectedStatus}
		}
		return nil
	}
}

// probeStatusError 健康检查响应状态码不匹配错误。
type probeStatusError struct {
	Status   int
	Expected int
}

func (e *probeStatusError) Error() string {
	return "proxy probe: unexpected status " +
		http.StatusText(e.Status) + " (want " + http.StatusText(e.Expected) + ")"
}
