// Package httpcache 实现了 HTTP 缓存中间件，对应 Scrapy 的 HttpCacheMiddleware。
//
// 该包提供可插拔的缓存存储后端和缓存策略接口，支持文件系统缓存存储
// 和两种缓存策略（DummyPolicy 无条件缓存、RFC2616Policy HTTP 缓存语义）。
//
// 配置项：
//   - HTTPCACHE_ENABLED: 是否启用 HTTP 缓存（默认 false）
//   - HTTPCACHE_DIR: 缓存目录（默认 "httpcache"）
//   - HTTPCACHE_EXPIRATION_SECS: 缓存过期时间（秒），0 表示不过期
//   - HTTPCACHE_GZIP: 是否使用 gzip 压缩存储（默认 false）
//   - HTTPCACHE_IGNORE_HTTP_CODES: 不缓存的 HTTP 状态码列表
//   - HTTPCACHE_IGNORE_SCHEMES: 不缓存的 URL scheme 列表（默认 ["file"]）
//   - HTTPCACHE_IGNORE_MISSING: 缓存未命中时是否忽略请求（默认 false）
//   - HTTPCACHE_POLICY: 缓存策略名称（"dummy" 或 "rfc2616"，默认 "dummy"）
//   - HTTPCACHE_ALWAYS_STORE: RFC2616 策略下是否始终存储响应（默认 false）
//   - HTTPCACHE_IGNORE_RESPONSE_CACHE_CONTROLS: RFC2616 策略下忽略的响应 Cache-Control 指令
package httpcache

import (
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// CacheStorage 定义缓存存储后端接口。
// 实现者负责缓存数据的持久化和检索。
//
// 对应 Scrapy 的 scrapy.extensions.httpcache.FilesystemCacheStorage 等。
type CacheStorage interface {
	// Open 在 Spider 打开时调用，初始化存储后端。
	Open(spiderName string) error

	// Close 在 Spider 关闭时调用，释放存储资源。
	Close() error

	// RetrieveResponse 从缓存中检索响应。
	// 如果缓存未命中，返回 (nil, nil)。
	RetrieveResponse(request *shttp.Request) (*shttp.Response, error)

	// StoreResponse 将响应存储到缓存中。
	StoreResponse(request *shttp.Request, response *shttp.Response) error
}

// CachePolicy 定义缓存策略接口。
// 实现者决定哪些请求/响应应该被缓存，以及缓存是否仍然有效。
//
// 对应 Scrapy 的 scrapy.extensions.httpcache.DummyPolicy / RFC2616Policy。
type CachePolicy interface {
	// ShouldCacheRequest 判断请求是否应该被缓存。
	// 返回 false 时，请求将跳过缓存查找和存储。
	ShouldCacheRequest(request *shttp.Request) bool

	// ShouldCacheResponse 判断响应是否应该被缓存。
	// 返回 false 时，响应不会被存储到缓存中。
	ShouldCacheResponse(response *shttp.Response, request *shttp.Request) bool

	// IsCachedResponseFresh 判断缓存的响应是否仍然新鲜（未过期）。
	// 返回 true 时，直接使用缓存响应，不发起实际请求。
	IsCachedResponseFresh(cachedResponse *shttp.Response, request *shttp.Request) bool

	// IsCachedResponseValid 判断缓存的响应在收到新响应后是否仍然有效。
	// 用于条件请求（If-Modified-Since / If-None-Match）的验证。
	// 返回 true 时，使用缓存响应替代新响应。
	IsCachedResponseValid(cachedResponse *shttp.Response, response *shttp.Response, request *shttp.Request) bool
}
