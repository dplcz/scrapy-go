package httpcache

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	scrapyerrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// HttpCacheMiddleware 实现 HTTP 缓存中间件。
// 在 ProcessRequest 阶段查找缓存并短路返回，
// 在 ProcessResponse 阶段存储可缓存的响应，
// 在 ProcessException 阶段尝试从缓存恢复。
//
// 注册优先级 900（在所有其他中间件之后执行 ProcessRequest，
// 在所有其他中间件之前执行 ProcessResponse）。
//
// 对应 Scrapy 的 scrapy.downloadermiddlewares.httpcache.HttpCacheMiddleware。
//
// 统计项：
//   - httpcache/hit — 缓存命中数
//   - httpcache/miss — 缓存未命中数
//   - httpcache/store — 缓存存储数
//   - httpcache/firsthand — 首次请求数（无缓存记录）
//   - httpcache/revalidate — 缓存重新验证成功数
//   - httpcache/invalidate — 缓存失效数
//   - httpcache/uncacheable — 不可缓存的响应数
//   - httpcache/errorrecovery — 错误恢复数（使用缓存响应替代下载异常）
//   - httpcache/ignore — 忽略的请求数（HTTPCACHE_IGNORE_MISSING 模式）
type HttpCacheMiddleware struct {
	policy        CachePolicy
	storage       CacheStorage
	ignoreMissing bool
	stats         stats.Collector
	logger        *slog.Logger
}

// downloadExceptions 是可以通过缓存恢复的下载异常列表。
// 对应 Scrapy 的 HttpCacheMiddleware.DOWNLOAD_EXCEPTIONS。
var downloadExceptions = []error{
	scrapyerrors.ErrDownloadTimeout,
	scrapyerrors.ErrConnectionRefused,
	scrapyerrors.ErrDownloadFailed,
	scrapyerrors.ErrCannotResolveHost,
	scrapyerrors.ErrResponseDataLoss,
}

// MiddlewareOption 是 HttpCacheMiddleware 的可选配置函数。
type MiddlewareOption func(*HttpCacheMiddleware)

// WithIgnoreMissing 设置缓存未命中时是否忽略请求。
func WithIgnoreMissing(ignoreMissing bool) MiddlewareOption {
	return func(m *HttpCacheMiddleware) {
		m.ignoreMissing = ignoreMissing
	}
}

// WithMiddlewareLogger 设置日志记录器。
func WithMiddlewareLogger(logger *slog.Logger) MiddlewareOption {
	return func(m *HttpCacheMiddleware) {
		m.logger = logger
	}
}

// NewHttpCacheMiddleware 创建一个新的 HTTP 缓存中间件。
func NewHttpCacheMiddleware(policy CachePolicy, storage CacheStorage, sc stats.Collector, opts ...MiddlewareOption) *HttpCacheMiddleware {
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	m := &HttpCacheMiddleware{
		policy:  policy,
		storage: storage,
		stats:   sc,
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Open 在 Spider 打开时调用，初始化缓存存储。
func (m *HttpCacheMiddleware) Open(spiderName string) error {
	return m.storage.Open(spiderName)
}

// Close 在 Spider 关闭时调用，关闭缓存存储。
func (m *HttpCacheMiddleware) Close() error {
	return m.storage.Close()
}

// ProcessRequest 在请求发送到下载器之前调用。
// 查找缓存，如果命中且新鲜则直接返回缓存响应（短路）。
func (m *HttpCacheMiddleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	// 检查 dont_cache Meta
	if dontCache, ok := request.GetMeta("dont_cache"); ok {
		if b, ok := dontCache.(bool); ok && b {
			return nil, nil
		}
	}

	// 检查请求是否可缓存
	if !m.policy.ShouldCacheRequest(request) {
		request.SetMeta("_dont_cache", true) // 标记为不可缓存
		return nil, nil
	}

	// 查找缓存
	cachedResponse, err := m.storage.RetrieveResponse(request)
	if err != nil {
		m.logger.Warn("httpcache: retrieve error",
			"url", request.URL.String(),
			"error", err,
		)
		return nil, nil
	}

	if cachedResponse == nil {
		m.stats.IncValue("httpcache/miss", 1, 0)
		if m.ignoreMissing {
			m.stats.IncValue("httpcache/ignore", 1, 0)
			return nil, scrapyerrors.ErrIgnoreRequest
		}
		return nil, nil // 首次请求
	}

	// 标记为缓存响应
	cachedResponse.Flags = append(cachedResponse.Flags, "cached")

	// 检查缓存是否新鲜
	if m.policy.IsCachedResponseFresh(cachedResponse, request) {
		m.stats.IncValue("httpcache/hit", 1, 0)
		return cachedResponse, nil
	}

	// 缓存过期，保存引用以便在 ProcessResponse 中避免二次查找
	request.SetMeta("cached_response", cachedResponse)

	return nil, nil
}

// ProcessResponse 在下载器返回响应后调用。
// 将可缓存的响应存储到缓存中。
func (m *HttpCacheMiddleware) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	// 检查 dont_cache Meta
	if dontCache, ok := request.GetMeta("dont_cache"); ok {
		if b, ok := dontCache.(bool); ok && b {
			return response, nil
		}
	}

	// 跳过已标记为不可缓存的请求和已缓存的响应
	if _, ok := request.GetMeta("_dont_cache"); ok {
		// 清理临时 Meta
		delete(request.Meta, "_dont_cache")
		return response, nil
	}
	if hasFlag(response.Flags, "cached") {
		return response, nil
	}

	// RFC 2616 要求源服务器设置 Date 头
	if response.Headers.Get("Date") == "" {
		response.Headers.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	}

	// 检查是否有缓存的响应（用于条件验证）
	var cachedResponse *shttp.Response
	if cached, ok := request.GetMeta("cached_response"); ok {
		if cr, ok := cached.(*shttp.Response); ok {
			cachedResponse = cr
		}
		delete(request.Meta, "cached_response")
	}

	if cachedResponse == nil {
		// 首次请求，直接缓存
		m.stats.IncValue("httpcache/firsthand", 1, 0)
		m.cacheResponse(response, request)
		return response, nil
	}

	// 条件验证
	if m.policy.IsCachedResponseValid(cachedResponse, response, request) {
		m.stats.IncValue("httpcache/revalidate", 1, 0)
		return cachedResponse, nil
	}

	m.stats.IncValue("httpcache/invalidate", 1, 0)
	m.cacheResponse(response, request)
	return response, nil
}

// ProcessException 在下载异常时调用。
// 如果有缓存的响应且异常是可恢复的下载异常，则返回缓存响应。
func (m *HttpCacheMiddleware) ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error) {
	// 检查是否有缓存的响应
	cached, ok := request.GetMeta("cached_response")
	if !ok {
		return nil, nil
	}

	cachedResponse, ok := cached.(*shttp.Response)
	if !ok || cachedResponse == nil {
		return nil, nil
	}

	// 清理 Meta
	delete(request.Meta, "cached_response")

	// 检查是否是可恢复的下载异常
	if isDownloadException(err) {
		m.stats.IncValue("httpcache/errorrecovery", 1, 0)
		return cachedResponse, nil
	}

	return nil, nil
}

// cacheResponse 缓存响应。
func (m *HttpCacheMiddleware) cacheResponse(response *shttp.Response, request *shttp.Request) {
	if m.policy.ShouldCacheResponse(response, request) {
		m.stats.IncValue("httpcache/store", 1, 0)
		if err := m.storage.StoreResponse(request, response); err != nil {
			m.logger.Warn("httpcache: store error",
				"url", request.URL.String(),
				"error", err,
			)
		}
	} else {
		m.stats.IncValue("httpcache/uncacheable", 1, 0)
	}
}

// isDownloadException 检查错误是否是可恢复的下载异常。
func isDownloadException(err error) bool {
	for _, target := range downloadExceptions {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// hasFlag 检查标记列表中是否包含指定标记。
func hasFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if f == flag {
			return true
		}
	}
	return false
}
