package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/dplcz/scrapy-go/pkg/downloader/middleware"
	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// 默认中间件优先级（数值越小越先执行 ProcessRequest）。
//
// 注意：内置 HttpProxyMiddleware 注册在 750；
// 本中间件默认 740（先于内置），从代理池选定代理后写入 Meta["proxy"]，
// 内置中间件检测到 Meta["proxy"] 已存在时跳过，避免双重处理。
const DefaultPriority = 740

// Meta 键约定。
const (
	// MetaKeyProxy 与内置 HttpProxyMiddleware 共用，存储 proxy URL（string）。
	MetaKeyProxy = "proxy"

	// MetaKeyProxyAuth 存储 base64 编码的 "user:pass"，由内置 HttpProxyMiddleware 读取。
	// 实际上内置中间件会在 Headers["Proxy-Authorization"] 中设置；
	// 这里直接由本中间件统一注入，无需额外的 Meta 键。

	// MetaKeyProxyChosen 标记当前请求选择的代理实例（*Proxy），
	// 用于在 ProcessResponse / ProcessException 中反馈结果。
	MetaKeyProxyChosen = "_proxy_pool_chosen"

	// MetaKeyProxyRetries 记录当前请求已切换代理的次数。
	MetaKeyProxyRetries = "_proxy_pool_retries"
)

// Middleware 实现 DownloaderMiddleware 接口，将 ProxyPool 接入下载链路。
//
// 工作流：
//
//  1. ProcessRequest:
//     - 若 Meta["proxy"] 已被显式设置，跳过（用户优先级最高）
//     - 从 Pool 获取代理，写入 Meta["proxy"] 与 Headers["Proxy-Authorization"]
//     - 记录所选代理到 Meta，便于回调反馈
//
//  2. ProcessResponse:
//     - 根据 HTTP 状态码反馈结果（2xx/3xx 视为成功，否则视为失败）
//     - 重置或递增代理的失败计数
//
//  3. ProcessException:
//     - 记录失败到 Pool（请求异常通常意味着代理不可用）
//     - 若启用 AutoRetryOnFailure 且未达最大重试次数，返回 NewRequestError 触发重新调度
type Middleware struct {
	pool   Pool
	logger *slog.Logger
	opts   *Options

	// 监控统计
	totalAssigned atomic.Int64 // 已分配代理的请求总数
	totalSkipped  atomic.Int64 // 跳过（用户已设置）的请求总数
	totalNoProxy  atomic.Int64 // 池为空时的请求数
}

// 编译期接口满足性检查
var (
	_ middleware.RequestProcessor   = (*Middleware)(nil)
	_ middleware.ResponseProcessor  = (*Middleware)(nil)
	_ middleware.ExceptionProcessor = (*Middleware)(nil)
)

// NewMiddleware 创建一个新的代理池中间件。
//
// pool 不能为 nil；logger 为 nil 时使用 slog.Default()。
//
// 中间件不持有 pool 的所有权，调用方需自行管理 pool 的生命周期（包括 Close）。
func NewMiddleware(pool Pool, logger *slog.Logger) *Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &Middleware{
		pool:   pool,
		logger: logger,
		opts:   nil, // 通过 NewMiddlewareWithOptions 设置
	}
}

// NewMiddlewareWithOptions 创建中间件并显式传递 Options。
//
// 当中间件需要访问 AutoRetryOnFailure / MaxProxyRetries 等配置时，
// 应使用此构造函数（否则使用 DefaultOptions）。
func NewMiddlewareWithOptions(pool Pool, opts *Options, logger *slog.Logger) *Middleware {
	mw := NewMiddleware(pool, logger)
	if opts != nil {
		cp := *opts
		cp.normalize()
		mw.opts = &cp
	}
	return mw
}

// effectiveOptions 返回有效配置，若未设置则使用默认。
func (m *Middleware) effectiveOptions() *Options {
	if m.opts != nil {
		return m.opts
	}
	return DefaultOptions()
}

// ProcessRequest 在请求发送前从代理池选取代理并注入到 Request。
func (m *Middleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	// 1. 检查用户是否已显式设置代理（优先级最高）
	if v, ok := request.GetMeta(MetaKeyProxy); ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			m.totalSkipped.Add(1)
			m.logger.Debug("proxy middleware: user-specified proxy, skip pool",
				"proxy", s,
				"url", request.URL.String(),
			)
			return nil, nil
		}
	}

	// 2. 从池中获取代理
	chosen, err := m.pool.Get(ctx)
	if err != nil {
		if errors.Is(err, ErrNoProxy) || errors.Is(err, ErrPoolClosed) {
			m.totalNoProxy.Add(1)
			m.logger.Warn("proxy middleware: no available proxy, request will run direct",
				"url", request.URL.String(),
				"error", err,
			)
			// 降级为不使用代理（让请求继续走直连）
			return nil, nil
		}
		return nil, err
	}

	// 3. 注入代理到 Request
	request.SetMeta(MetaKeyProxy, chosen.URL)
	request.SetMeta(MetaKeyProxyChosen, chosen)

	if chosen.Credentials != "" {
		if request.Headers == nil {
			request.Headers = make(http.Header)
		}
		request.Headers.Set("Proxy-Authorization", "Basic "+chosen.Credentials)
	}

	m.totalAssigned.Add(1)
	m.logger.Debug("proxy middleware: assigned proxy",
		"proxy", chosen.URL,
		"url", request.URL.String(),
	)
	return nil, nil
}

// ProcessResponse 根据响应反馈代理使用结果。
//
// 反馈规则：
//   - 2xx/3xx：视为代理可用（成功）
//   - 407 Proxy Authentication Required：代理认证失败（标记失败）
//   - 5xx：可能是代理或目标服务器问题，视为失败
//   - 其他 4xx：视为成功（业务错误，与代理无关）
func (m *Middleware) ProcessResponse(_ context.Context, request *shttp.Request,
	response *shttp.Response) (*shttp.Response, error) {
	chosen := m.takeChosen(request)
	if chosen == nil {
		return response, nil
	}

	// 状态码判定
	status := 0
	if response != nil {
		status = response.Status
	}

	switch {
	case status >= 200 && status < 400:
		m.pool.Mark(chosen, true)
	case status == http.StatusProxyAuthRequired, status >= 500:
		m.pool.Mark(chosen, false)
		m.logger.Debug("proxy middleware: response indicates proxy failure",
			"proxy", chosen.URL,
			"status", status,
		)
	default:
		// 其他状态码视为业务问题，不影响代理评价
	}

	return response, nil
}

// ProcessException 在下载异常时反馈代理失败，必要时切换代理重试。
func (m *Middleware) ProcessException(_ context.Context, request *shttp.Request,
	originalErr error) (*shttp.Response, error) {
	chosen := m.takeChosen(request)
	if chosen != nil {
		m.pool.Mark(chosen, false)
		m.logger.Debug("proxy middleware: exception, mark proxy failed",
			"proxy", chosen.URL,
			"error", originalErr,
		)
	}

	opts := m.effectiveOptions()
	if !opts.AutoRetryOnFailure {
		return nil, nil
	}

	// 检查重试次数
	retries := 0
	if v, ok := request.GetMeta(MetaKeyProxyRetries); ok {
		if n, ok := v.(int); ok {
			retries = n
		}
	}
	if retries >= opts.MaxProxyRetries {
		m.logger.Warn("proxy middleware: max proxy retries exceeded",
			"url", request.URL.String(),
			"retries", retries,
		)
		return nil, nil // 让原始错误继续传播
	}

	// 构造新请求：清除已选代理，递增重试计数
	newReq := request.Copy()
	delete(newReq.Meta, MetaKeyProxy)
	delete(newReq.Meta, MetaKeyProxyChosen)
	if newReq.Headers != nil {
		newReq.Headers.Del("Proxy-Authorization")
	}
	newReq.SetMeta(MetaKeyProxyRetries, retries+1)
	newReq.DontFilter = true

	// 通过 NewRequestError 触发 Engine 重新调度（对齐 RetryMiddleware 模式）
	return nil, serrors.NewNewRequestError(newReq, "proxy_retry")
}

// takeChosen 从 Request.Meta 中取出已选代理。
// 取出后从 Meta 中移除，避免影响后续中间件。
func (m *Middleware) takeChosen(request *shttp.Request) *Proxy {
	v, ok := request.GetMeta(MetaKeyProxyChosen)
	if !ok {
		return nil
	}
	delete(request.Meta, MetaKeyProxyChosen)
	if p, ok := v.(*Proxy); ok {
		return p
	}
	return nil
}

// Stats 返回中间件统计指标快照。
type Stats struct {
	// TotalAssigned 是已成功分配代理的请求数。
	TotalAssigned int64
	// TotalSkipped 是跳过（用户已显式设置代理）的请求数。
	TotalSkipped int64
	// TotalNoProxy 是因池中无可用代理而降级的请求数。
	TotalNoProxy int64
}

// Stats 返回中间件的运行时统计。
func (m *Middleware) Stats() Stats {
	return Stats{
		TotalAssigned: m.totalAssigned.Load(),
		TotalSkipped:  m.totalSkipped.Load(),
		TotalNoProxy:  m.totalNoProxy.Load(),
	}
}
