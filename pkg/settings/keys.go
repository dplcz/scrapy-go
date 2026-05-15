package settings

import (
	"net/http"
	"time"
)

// ============================================================================
// 类型化配置键常量（TD-004：编译期类型安全）
//
// 所有框架内置配置项均在此定义为类型化常量。
// 使用方式：settings.Get(s, settings.KeyConcurrentRequests) 返回 int 类型。
// ============================================================================

// --- 基础配置 ---

var (
	// KeyBotName 爬虫名称。
	KeyBotName = Key[string]{Name: "BOT_NAME", Default: "scrapybot"}
	// KeyUserAgent 默认 User-Agent 字符串。
	KeyUserAgent = Key[string]{Name: "USER_AGENT", Default: "scrapy-go/1.1.0 (+https://github.com/example/scrapy-go)"}
)

// --- 并发控制 ---

var (
	// KeyConcurrentRequests 全局最大并发请求数。
	KeyConcurrentRequests = Key[int]{Name: "CONCURRENT_REQUESTS", Default: 16}
	// KeyConcurrentRequestsPerDomain 每个域名的最大并发请求数。
	KeyConcurrentRequestsPerDomain = Key[int]{Name: "CONCURRENT_REQUESTS_PER_DOMAIN", Default: 8}
	// KeyConcurrentItems Pipeline 并行处理 Item 的最大数量。
	KeyConcurrentItems = Key[int]{Name: "CONCURRENT_ITEMS", Default: 100}
)

// --- 下载配置 ---

var (
	// KeyDownloadDelay 下载延迟（秒），0 表示无延迟。
	KeyDownloadDelay = Key[int]{Name: "DOWNLOAD_DELAY", Default: 0}
	// KeyDownloadTimeout 下载超时（秒）。
	KeyDownloadTimeout = Key[int]{Name: "DOWNLOAD_TIMEOUT", Default: 180}
	// KeyDownloadTimeoutDuration 下载超时（Duration 类型，用于需要 time.Duration 的场景）。
	KeyDownloadTimeoutDuration = Key[time.Duration]{Name: "DOWNLOAD_TIMEOUT", Default: 180 * time.Second}
	// KeyDownloadMaxSize 最大下载大小（字节）。
	KeyDownloadMaxSize = Key[int]{Name: "DOWNLOAD_MAXSIZE", Default: 1024 * 1024 * 1024}
	// KeyDownloadWarnSize 下载大小警告阈值（字节）。
	KeyDownloadWarnSize = Key[int]{Name: "DOWNLOAD_WARNSIZE", Default: 32 * 1024 * 1024}
	// KeyDownloadFailOnDataloss 数据丢失时是否失败。
	KeyDownloadFailOnDataloss = Key[bool]{Name: "DOWNLOAD_FAIL_ON_DATALOSS", Default: true}
	// KeyRandomizeDownloadDelay 是否随机化下载延迟。
	KeyRandomizeDownloadDelay = Key[bool]{Name: "RANDOMIZE_DOWNLOAD_DELAY", Default: true}
)

// --- 默认请求头 ---

var (
	// KeyDefaultRequestHeaders 默认请求头。
	KeyDefaultRequestHeaders = Key[http.Header]{Name: "DEFAULT_REQUEST_HEADERS", Default: http.Header{
		"Accept":          {"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
		"Accept-Language": {"en"},
	}}
)

// --- 重试配置 ---

var (
	// KeyRetryEnabled 是否启用重试。
	KeyRetryEnabled = Key[bool]{Name: "RETRY_ENABLED", Default: true}
	// KeyRetryTimes 最大重试次数。
	KeyRetryTimes = Key[int]{Name: "RETRY_TIMES", Default: 2}
	// KeyRetryHTTPCodes 触发重试的 HTTP 状态码列表。
	KeyRetryHTTPCodes = Key[[]int]{Name: "RETRY_HTTP_CODES", Default: []int{500, 502, 503, 504, 522, 524, 408, 429}}
	// KeyRetryPriorityAdjust 重试请求优先级调整值。
	KeyRetryPriorityAdjust = Key[int]{Name: "RETRY_PRIORITY_ADJUST", Default: -1}
	// KeyRetryBackoffEnabled 是否启用指数退避。
	KeyRetryBackoffEnabled = Key[bool]{Name: "RETRY_BACKOFF_ENABLED", Default: false}
	// KeyRetryBackoffBaseDelay 退避基础延迟（秒）。
	KeyRetryBackoffBaseDelay = Key[float64]{Name: "RETRY_BACKOFF_BASE_DELAY", Default: 1.0}
	// KeyRetryBackoffMaxDelay 退避最大延迟（秒）。
	KeyRetryBackoffMaxDelay = Key[float64]{Name: "RETRY_BACKOFF_MAX_DELAY", Default: 60.0}
	// KeyRetryBackoffJitter 是否启用退避抖动。
	KeyRetryBackoffJitter = Key[bool]{Name: "RETRY_BACKOFF_JITTER", Default: true}
	// KeyRetryPerStatusMaxTimes 按状态码差异化最大重试次数。
	KeyRetryPerStatusMaxTimes = Key[map[int]int]{Name: "RETRY_PER_STATUS_MAX_TIMES", Default: map[int]int{}}
)

// --- 熔断器配置 ---

var (
	// KeyCircuitBreakerEnabled 是否启用熔断器。
	KeyCircuitBreakerEnabled = Key[bool]{Name: "CIRCUIT_BREAKER_ENABLED", Default: false}
	// KeyCircuitBreakerFailThreshold 连续失败阈值。
	KeyCircuitBreakerFailThreshold = Key[int]{Name: "CIRCUIT_BREAKER_FAIL_THRESHOLD", Default: 5}
	// KeyCircuitBreakerRecoveryTimeout 恢复超时时间（秒）。
	KeyCircuitBreakerRecoveryTimeout = Key[int]{Name: "CIRCUIT_BREAKER_RECOVERY_TIMEOUT", Default: 30}
	// KeyCircuitBreakerHalfOpenMaxRequests 半开状态最大探测请求数。
	KeyCircuitBreakerHalfOpenMaxRequests = Key[int]{Name: "CIRCUIT_BREAKER_HALF_OPEN_MAX_REQUESTS", Default: 1}
	// KeyCircuitBreakerSuccessThreshold 半开状态恢复所需连续成功次数。
	KeyCircuitBreakerSuccessThreshold = Key[int]{Name: "CIRCUIT_BREAKER_SUCCESS_THRESHOLD", Default: 2}
	// KeyCircuitBreakerHTTPCodes 触发熔断的 HTTP 状态码。
	KeyCircuitBreakerHTTPCodes = Key[[]int]{Name: "CIRCUIT_BREAKER_HTTP_CODES", Default: []int{500, 502, 503, 504}}
)

// --- 重定向配置 ---

var (
	// KeyRedirectEnabled 是否启用重定向。
	KeyRedirectEnabled = Key[bool]{Name: "REDIRECT_ENABLED", Default: true}
	// KeyRedirectMaxTimes 最大重定向次数。
	KeyRedirectMaxTimes = Key[int]{Name: "REDIRECT_MAX_TIMES", Default: 20}
	// KeyRedirectPriorityAdjust 重定向请求优先级调整值。
	KeyRedirectPriorityAdjust = Key[int]{Name: "REDIRECT_PRIORITY_ADJUST", Default: 2}
)

// --- 深度控制 ---

var (
	// KeyDepthLimit 爬取深度限制，0 表示无限制。
	KeyDepthLimit = Key[int]{Name: "DEPTH_LIMIT", Default: 0}
	// KeyDepthPriority 深度优先级调整系数。
	KeyDepthPriority = Key[int]{Name: "DEPTH_PRIORITY", Default: 0}
	// KeyDepthStatsVerbose 是否记录各深度请求数统计。
	KeyDepthStatsVerbose = Key[bool]{Name: "DEPTH_STATS_VERBOSE", Default: false}
)

// --- URL 长度限制 ---

var (
	// KeyURLLengthLimit URL 最大长度。
	KeyURLLengthLimit = Key[int]{Name: "URLLENGTH_LIMIT", Default: 2083}
)

// --- Cookies 配置 ---

var (
	// KeyCookiesEnabled 是否启用 Cookie 管理。
	KeyCookiesEnabled = Key[bool]{Name: "COOKIES_ENABLED", Default: true}
	// KeyCookiesDebug 是否启用 Cookie 调试日志。
	KeyCookiesDebug = Key[bool]{Name: "COOKIES_DEBUG", Default: false}
)

// --- HTTP 认证 ---

var (
	// KeyHTTPUser HTTP 认证用户名。
	KeyHTTPUser = Key[string]{Name: "HTTP_USER", Default: ""}
	// KeyHTTPPass HTTP 认证密码。
	KeyHTTPPass = Key[string]{Name: "HTTP_PASS", Default: ""}
	// KeyHTTPAuthDomain HTTP 认证域名限制。
	KeyHTTPAuthDomain = Key[string]{Name: "HTTP_AUTH_DOMAIN", Default: ""}
)

// --- HTTP 压缩 ---

var (
	// KeyCompressionEnabled 是否启用压缩解码。
	KeyCompressionEnabled = Key[bool]{Name: "COMPRESSION_ENABLED", Default: true}
)

// --- HTTP 代理 ---

var (
	// KeyHTTPProxyEnabled 是否启用代理中间件。
	KeyHTTPProxyEnabled = Key[bool]{Name: "HTTPPROXY_ENABLED", Default: true}
	// KeyHTTPProxyAuthEncoding 代理认证信息编码。
	KeyHTTPProxyAuthEncoding = Key[string]{Name: "HTTPPROXY_AUTH_ENCODING", Default: "latin-1"}
)

// --- HTTP 缓存 ---

var (
	// KeyHTTPCacheEnabled 是否启用 HTTP 缓存。
	KeyHTTPCacheEnabled = Key[bool]{Name: "HTTPCACHE_ENABLED", Default: false}
	// KeyHTTPCacheDir 缓存目录。
	KeyHTTPCacheDir = Key[string]{Name: "HTTPCACHE_DIR", Default: "httpcache"}
	// KeyHTTPCacheExpirationSecs 缓存过期时间（秒）。
	KeyHTTPCacheExpirationSecs = Key[int]{Name: "HTTPCACHE_EXPIRATION_SECS", Default: 0}
	// KeyHTTPCacheGzip 是否使用 gzip 压缩缓存。
	KeyHTTPCacheGzip = Key[bool]{Name: "HTTPCACHE_GZIP", Default: false}
	// KeyHTTPCacheIgnoreHTTPCodes 忽略缓存的 HTTP 状态码。
	KeyHTTPCacheIgnoreHTTPCodes = Key[[]int]{Name: "HTTPCACHE_IGNORE_HTTP_CODES", Default: []int{}}
	// KeyHTTPCacheIgnoreSchemes 忽略缓存的 URL scheme。
	KeyHTTPCacheIgnoreSchemes = Key[[]string]{Name: "HTTPCACHE_IGNORE_SCHEMES", Default: []string{"file"}}
	// KeyHTTPCacheIgnoreMissing 是否忽略缓存未命中。
	KeyHTTPCacheIgnoreMissing = Key[bool]{Name: "HTTPCACHE_IGNORE_MISSING", Default: false}
	// KeyHTTPCachePolicy 缓存策略名称。
	KeyHTTPCachePolicy = Key[string]{Name: "HTTPCACHE_POLICY", Default: "dummy"}
	// KeyHTTPCacheAlwaysStore 是否始终存储响应到缓存。
	KeyHTTPCacheAlwaysStore = Key[bool]{Name: "HTTPCACHE_ALWAYS_STORE", Default: false}
	// KeyHTTPCacheIgnoreResponseCacheControls 忽略的响应 Cache-Control 指令。
	KeyHTTPCacheIgnoreResponseCacheControls = Key[[]string]{Name: "HTTPCACHE_IGNORE_RESPONSE_CACHE_CONTROLS", Default: []string{}}
)

// --- Robots.txt ---

var (
	// KeyRobotsTxtObey 是否遵守 robots.txt。
	KeyRobotsTxtObey = Key[bool]{Name: "ROBOTSTXT_OBEY", Default: true}
	// KeyRobotsTxtUserAgent robots.txt 专用 User-Agent。
	KeyRobotsTxtUserAgent = Key[string]{Name: "ROBOTSTXT_USER_AGENT", Default: ""}
)

// --- 调度器配置 ---

var (
	// KeySchedulerDebug 是否启用调度器调试日志。
	KeySchedulerDebug = Key[bool]{Name: "SCHEDULER_DEBUG", Default: false}
	// KeyJobDir 断点续爬目录。
	KeyJobDir = Key[string]{Name: "JOBDIR", Default: ""}
)

// --- 优雅关闭 ---

var (
	// KeyGracefulShutdownTimeout 优雅关闭超时时间（秒）。
	KeyGracefulShutdownTimeout = Key[int]{Name: "GRACEFUL_SHUTDOWN_TIMEOUT", Default: 30}
)

// --- pprof 调试 ---

var (
	// KeyPprofEnabled 是否启用 pprof HTTP 端点。
	KeyPprofEnabled = Key[bool]{Name: "PPROF_ENABLED", Default: false}
	// KeyPprofAddr pprof 监听地址。
	KeyPprofAddr = Key[string]{Name: "PPROF_ADDR", Default: ":6060"}
)

// --- Scraper 配置 ---

var (
	// KeyScraperSlotMaxActiveSize Scraper slot 最大活跃大小（字节）。
	KeyScraperSlotMaxActiveSize = Key[int]{Name: "SCRAPER_SLOT_MAX_ACTIVE_SIZE", Default: 5000000}
)

// --- 日志配置 ---

var (
	// KeyLogEnabled 是否启用日志。
	KeyLogEnabled = Key[bool]{Name: "LOG_ENABLED", Default: true}
	// KeyLogLevel 日志级别。
	KeyLogLevel = Key[string]{Name: "LOG_LEVEL", Default: "DEBUG"}
	// KeyLogFile 日志文件路径。
	KeyLogFile = Key[string]{Name: "LOG_FILE", Default: ""}
)

// --- 统计配置 ---

var (
	// KeyStatsDump 是否在关闭时输出统计信息。
	KeyStatsDump = Key[bool]{Name: "STATS_DUMP", Default: true}
	// KeyLogStatsInterval 统计日志输出间隔（秒）。
	KeyLogStatsInterval = Key[float64]{Name: "LOGSTATS_INTERVAL", Default: 60.0}
)

// --- 关闭条件 ---

var (
	// KeyCloseSpiderErrorCount 达到错误数量自动关闭。
	KeyCloseSpiderErrorCount = Key[int]{Name: "CLOSESPIDER_ERRORCOUNT", Default: 0}
	// KeyCloseSpiderItemCount 达到 Item 数量自动关闭。
	KeyCloseSpiderItemCount = Key[int]{Name: "CLOSESPIDER_ITEMCOUNT", Default: 0}
	// KeyCloseSpiderPageCount 达到页面数量自动关闭。
	KeyCloseSpiderPageCount = Key[int]{Name: "CLOSESPIDER_PAGECOUNT", Default: 0}
	// KeyCloseSpiderTimeout 超时自动关闭（秒）。
	KeyCloseSpiderTimeout = Key[int]{Name: "CLOSESPIDER_TIMEOUT", Default: 0}
)

// --- 内存监控 ---

var (
	// KeyMemUsageEnabled 是否启用内存监控。
	KeyMemUsageEnabled = Key[bool]{Name: "MEMUSAGE_ENABLED", Default: true}
	// KeyMemUsageCheckIntervalSeconds 内存检查间隔（秒）。
	KeyMemUsageCheckIntervalSeconds = Key[float64]{Name: "MEMUSAGE_CHECK_INTERVAL_SECONDS", Default: 60.0}
	// KeyMemUsageLimitMB 内存限制（MB），0 不限制。
	KeyMemUsageLimitMB = Key[int]{Name: "MEMUSAGE_LIMIT_MB", Default: 0}
	// KeyMemUsageWarningMB 内存警告阈值（MB）。
	KeyMemUsageWarningMB = Key[int]{Name: "MEMUSAGE_WARNING_MB", Default: 0}
)

// --- 中间件优先级字典 ---

var (
	// KeyDownloaderMiddlewaresBase 内置下载器中间件优先级字典。
	KeyDownloaderMiddlewaresBase = Key[map[string]int]{Name: "DOWNLOADER_MIDDLEWARES_BASE", Default: map[string]int{
		"RobotsTxt":       100,
		"DownloadTimeout": 300,
		"DefaultHeaders":  400,
		"HttpAuth":        410,
		"UserAgent":       500,
		"CircuitBreaker":  545,
		"Retry":           550,
		"HttpCompression": 590,
		"Redirect":        600,
		"Cookies":         700,
		"HttpProxy":       750,
		"DownloaderStats": 850,
		"HttpCache":       900,
	}}
	// KeyDownloaderMiddlewares 用户下载器中间件优先级字典。
	KeyDownloaderMiddlewares = Key[map[string]int]{Name: "DOWNLOADER_MIDDLEWARES", Default: map[string]int{}}
	// KeySpiderMiddlewaresBase 内置 Spider 中间件优先级字典。
	KeySpiderMiddlewaresBase = Key[map[string]int]{Name: "SPIDER_MIDDLEWARES_BASE", Default: map[string]int{
		"HttpError": 50,
		"Offsite":   500,
		"Referer":   700,
		"UrlLength": 800,
		"Depth":     900,
	}}
	// KeySpiderMiddlewares 用户 Spider 中间件优先级字典。
	KeySpiderMiddlewares = Key[map[string]int]{Name: "SPIDER_MIDDLEWARES", Default: map[string]int{}}
	// KeyItemPipelinesBase 内置 Pipeline 优先级字典。
	KeyItemPipelinesBase = Key[map[string]int]{Name: "ITEM_PIPELINES_BASE", Default: map[string]int{}}
	// KeyItemPipelines 用户 Pipeline 优先级字典。
	KeyItemPipelines = Key[map[string]int]{Name: "ITEM_PIPELINES", Default: map[string]int{}}
	// KeyExtensionsBase 内置扩展优先级字典。
	KeyExtensionsBase = Key[map[string]int]{Name: "EXTENSIONS_BASE", Default: map[string]int{
		"CoreStats":    0,
		"CloseSpider":  0,
		"LogStats":     0,
		"MemoryUsage":  0,
		"FeedExport":   0,
		"AutoThrottle": 0,
	}}
	// KeyExtensions 用户扩展优先级字典。
	KeyExtensions = Key[map[string]int]{Name: "EXTENSIONS", Default: map[string]int{}}
)

// --- 下载处理器 ---

var (
	// KeyDownloadHandlersBase 内置下载处理器映射。
	KeyDownloadHandlersBase = Key[map[string]string]{Name: "DOWNLOAD_HANDLERS_BASE", Default: map[string]string{
		"http":  "HTTPDownloadHandler",
		"https": "HTTPDownloadHandler",
	}}
	// KeyDownloadHandlers 用户下载处理器映射。
	KeyDownloadHandlers = Key[map[string]string]{Name: "DOWNLOAD_HANDLERS", Default: map[string]string{}}
)

// --- HTTP/2 与连接池 ---

var (
	// KeyHTTP2Enabled 是否启用 HTTP/2 协议支持。
	// 启用后默认 Handler 的 ManagedTransport 会设置 ForceAttemptHTTP2=true，
	// 使 HTTPS 请求通过 ALPN 自动协商 HTTP/2。
	KeyHTTP2Enabled = Key[bool]{Name: "HTTP2_ENABLED", Default: false}
	// KeyHTTP2AllowH2C 是否启用 HTTP/2 over cleartext（h2c）支持。
	// 启用后注册 http2.Transport 作为 http:// scheme 的 handler，
	// 允许在不使用 TLS 的情况下使用 HTTP/2 协议（适用于内网/测试场景）。
	KeyHTTP2AllowH2C = Key[bool]{Name: "HTTP2_ALLOW_H2C", Default: false}
	// KeyDownloadProgressEnabled 是否启用下载进度回调。
	KeyDownloadProgressEnabled = Key[bool]{Name: "DOWNLOAD_PROGRESS_ENABLED", Default: false}
	// KeyDownloadProgressMinInterval 进度报告最小间隔（毫秒）。
	KeyDownloadProgressMinInterval = Key[int]{Name: "DOWNLOAD_PROGRESS_MIN_INTERVAL", Default: 100}
	// KeyConnPoolMaxIdleConns 最大空闲连接总数。
	KeyConnPoolMaxIdleConns = Key[int]{Name: "CONNPOOL_MAX_IDLE_CONNS", Default: 100}
	// KeyConnPoolMaxIdleConnsPerHost 每 host 最大空闲连接数。
	KeyConnPoolMaxIdleConnsPerHost = Key[int]{Name: "CONNPOOL_MAX_IDLE_CONNS_PER_HOST", Default: 10}
	// KeyConnPoolMaxConnsPerHost 每 host 最大连接数（0=不限制）。
	KeyConnPoolMaxConnsPerHost = Key[int]{Name: "CONNPOOL_MAX_CONNS_PER_HOST", Default: 0}
	// KeyConnPoolIdleConnTimeout 空闲连接超时（秒）。
	KeyConnPoolIdleConnTimeout = Key[int]{Name: "CONNPOOL_IDLE_CONN_TIMEOUT", Default: 90}
	// KeyConnPoolTLSHandshakeTimeout TLS 握手超时（秒）。
	KeyConnPoolTLSHandshakeTimeout = Key[int]{Name: "CONNPOOL_TLS_HANDSHAKE_TIMEOUT", Default: 10}
	// KeyConnPoolDialTimeout TCP 连接超时（秒）。
	KeyConnPoolDialTimeout = Key[int]{Name: "CONNPOOL_DIAL_TIMEOUT", Default: 30}
	// KeyConnPoolDialKeepalive TCP keep-alive 间隔（秒）。
	KeyConnPoolDialKeepalive = Key[int]{Name: "CONNPOOL_DIAL_KEEPALIVE", Default: 30}
	// KeyConnPoolDisableKeepalives 是否禁用 HTTP keep-alive。
	KeyConnPoolDisableKeepalives = Key[bool]{Name: "CONNPOOL_DISABLE_KEEPALIVES", Default: false}
	// KeyConnPoolWriteBufferSize 写缓冲区大小（0=默认 4KB）。
	KeyConnPoolWriteBufferSize = Key[int]{Name: "CONNPOOL_WRITE_BUFFER_SIZE", Default: 0}
	// KeyConnPoolReadBufferSize 读缓冲区大小（0=默认 4KB）。
	KeyConnPoolReadBufferSize = Key[int]{Name: "CONNPOOL_READ_BUFFER_SIZE", Default: 0}
	// KeyConnPoolTLSInsecureSkipVerify 是否跳过 TLS 证书验证。
	KeyConnPoolTLSInsecureSkipVerify = Key[bool]{Name: "CONNPOOL_TLS_INSECURE_SKIP_VERIFY", Default: false}
)

// --- 去重过滤器 ---

var (
	// KeyDupeFilterDebug 是否启用去重过滤器调试日志。
	KeyDupeFilterDebug = Key[bool]{Name: "DUPEFILTER_DEBUG", Default: false}
)

// --- 下载器统计 ---

var (
	// KeyDownloaderStats 是否启用下载器统计中间件。
	KeyDownloaderStats = Key[bool]{Name: "DOWNLOADER_STATS", Default: true}
)

// --- 数据导出 ---

var (
	// KeyFeedExportEncoding 导出编码。
	KeyFeedExportEncoding = Key[string]{Name: "FEED_EXPORT_ENCODING", Default: ""}
	// KeyFeedExportIndent 导出缩进。
	KeyFeedExportIndent = Key[int]{Name: "FEED_EXPORT_INDENT", Default: 0}
	// KeyFeedStoreEmpty 是否存储空 Feed。
	KeyFeedStoreEmpty = Key[bool]{Name: "FEED_STORE_EMPTY", Default: true}
	// KeyFeedExportBatchItemCount 批量导出 Item 数量。
	KeyFeedExportBatchItemCount = Key[int]{Name: "FEED_EXPORT_BATCH_ITEM_COUNT", Default: 0}
	// KeyFeedURI 导出 URI（旧式单文件输出）。
	KeyFeedURI = Key[string]{Name: "FEED_URI", Default: ""}
	// KeyFeedFormat 导出格式（旧式单文件输出）。
	KeyFeedFormat = Key[string]{Name: "FEED_FORMAT", Default: ""}
)

// --- HTTP 错误过滤 ---

var (
	// KeyHTTPErrorAllowAll 是否允许所有 HTTP 状态码。
	KeyHTTPErrorAllowAll = Key[bool]{Name: "HTTPERROR_ALLOW_ALL", Default: false}
	// KeyHTTPErrorAllowedCodes 允许的非 200 状态码列表。
	KeyHTTPErrorAllowedCodes = Key[[]int]{Name: "HTTPERROR_ALLOWED_CODES", Default: []int{}}
)

// --- Referer 配置 ---

var (
	// KeyRefererEnabled 是否启用 Referer 中间件。
	KeyRefererEnabled = Key[bool]{Name: "REFERER_ENABLED", Default: true}
)

// --- CrawlSpider 配置 ---

var (
	// KeyCrawlSpiderFollowLinks CrawlSpider 是否全局启用链接跟踪。
	KeyCrawlSpiderFollowLinks = Key[bool]{Name: "CRAWLSPIDER_FOLLOW_LINKS", Default: true}
)

// --- AutoThrottle 自适应限速 ---

var (
	// KeyAutoThrottleEnabled 是否启用自适应限速。
	KeyAutoThrottleEnabled = Key[bool]{Name: "AUTOTHROTTLE_ENABLED", Default: false}
	// KeyAutoThrottleStartDelay 初始下载延迟（秒）。
	KeyAutoThrottleStartDelay = Key[float64]{Name: "AUTOTHROTTLE_START_DELAY", Default: 5.0}
	// KeyAutoThrottleMaxDelay 最大下载延迟（秒）。
	KeyAutoThrottleMaxDelay = Key[float64]{Name: "AUTOTHROTTLE_MAX_DELAY", Default: 60.0}
	// KeyAutoThrottleTargetConcurrency 目标并发数（每个域名）。
	KeyAutoThrottleTargetConcurrency = Key[float64]{Name: "AUTOTHROTTLE_TARGET_CONCURRENCY", Default: 1.0}
	// KeyAutoThrottleDebug 是否输出调试日志。
	KeyAutoThrottleDebug = Key[bool]{Name: "AUTOTHROTTLE_DEBUG", Default: false}
)
