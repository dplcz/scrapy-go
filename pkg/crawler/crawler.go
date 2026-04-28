// Package crawler 实现了 scrapy-go 框架的顶层编排器。
//
// Crawler 负责组装所有组件（Engine、Scheduler、Downloader、Scraper 等），
// 提供简洁的 API 供用户启动爬虫。
package crawler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dplcz/scrapy-go/pkg/downloader"
	dmiddle "github.com/dplcz/scrapy-go/pkg/downloader/middleware"
	"github.com/dplcz/scrapy-go/pkg/engine"
	"github.com/dplcz/scrapy-go/pkg/extension"
	"github.com/dplcz/scrapy-go/pkg/feedexport"
	sslog "github.com/dplcz/scrapy-go/pkg/log"
	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
	"github.com/dplcz/scrapy-go/pkg/scraper"
	"github.com/dplcz/scrapy-go/pkg/settings"
	sig "github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
	smiddle "github.com/dplcz/scrapy-go/pkg/spider/middleware"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// Crawler 是顶层编排器，组装所有组件并启动爬虫。
type Crawler struct {
	Settings *settings.Settings
	Stats    stats.Collector
	Signals  *sig.Manager
	Logger   *slog.Logger

	engine     *engine.Engine
	spider     spider.Spider
	scheduler  scheduler.Scheduler
	downloader *downloader.Downloader
	dlMW       *downloader.MiddlewareManager
	spiderMW   *smiddle.Manager
	pipelines  *pipeline.Manager
	scraper    *scraper.Scraper
	extensions *extension.Manager

	// customLogger 标记用户是否通过 WithLogger 自定义 Logger
	customLogger bool

	// userPipelines 存储用户注册的自定义 Pipeline
	userPipelines []pipeline.Entry

	// userDLMiddlewares 存储用户注册的自定义下载器中间件
	userDLMiddlewares []downloader.MiddlewareEntry

	// userSpiderMiddlewares 存储用户注册的自定义 Spider 中间件
	userSpiderMiddlewares []smiddle.Entry

	// userExtensions 存储用户注册的自定义扩展
	userExtensions []extension.Entry

	// userFeedConfigs 存储用户通过 AddFeed 注册的 Feed 导出配置
	userFeedConfigs []feedexport.FeedConfig

	// crawling 标记爬虫是否正在运行（原子操作，并发安全）
	crawling atomic.Bool

	// started 标记爬虫是否已经启动过（Crawler 实例只能运行一次）
	started atomic.Bool

	// cancelMu 保护 cancel 字段的读写
	cancelMu sync.Mutex
	// cancel 是 Run 方法中派生 context 的取消函数，Stop 方法通过它触发优雅关闭
	cancel context.CancelFunc

	// beforeStartHooks 是在组件组装完成、Engine 启动前调用的钩子列表。
	// 供 Runner 等上层编排器在 Signals/Stats 被（重新）创建后注入跨爬虫处理器。
	beforeStartHooks []func(*Crawler)
}

// New 创建一个新的 Crawler，可通过 Option 自定义各组件。
// 未指定的组件将使用合理的默认值。
func New(opts ...Option) *Crawler {
	c := &Crawler{
		Settings: settings.New(),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.initDefaults()
	return c
}

// NewDefault 创建一个使用全部默认配置的 Crawler。
// 这是最简单的创建方式，适用于大多数场景。
// 日志级别由 Settings 中的 LOG_LEVEL 配置控制（默认 DEBUG）。
//
// 用法：
//
//	c := crawler.NewDefault()
//	c.Run(ctx, mySpider)
func NewDefault() *Crawler {
	c := &Crawler{
		Settings: settings.New(),
	}
	c.initDefaults()
	return c
}

// initDefaults 初始化未设置的组件为默认值。
func (c *Crawler) initDefaults() {
	if c.Logger == nil || !c.customLogger {
		c.Logger = newDefaultLogger(c.Settings)
	}
	if c.Signals == nil {
		c.Signals = sig.NewManager(c.Logger)
	}
	if c.Stats == nil {
		c.Stats = stats.NewMemoryCollector(
			c.Settings.GetBool("STATS_DUMP", true),
			c.Logger,
		)
	}
}

// newDefaultLogger 根据 Settings 中的 LOG_LEVEL 创建默认日志记录器。
// 支持的级别：DEBUG、INFO、WARN、ERROR（不区分大小写）。
func newDefaultLogger(s *settings.Settings) *slog.Logger {
	levelStr := s.GetString("LOG_LEVEL", "DEBUG")
	return sslog.NewColorLogger(levelStr, nil, false)
}

// AddPipeline 注册一个自定义 Item Pipeline。
// 必须在 Run 之前调用。priority 值越小越先执行。
//
// 用法：
//
//	c := crawler.NewDefault()
//	c.AddPipeline(&JsonFilePipeline{Path: "output.json"}, "JsonFile", 300)
//	c.Run(ctx, mySpider)
func (c *Crawler) AddPipeline(p pipeline.ItemPipeline, name string, priority int) {
	c.userPipelines = append(c.userPipelines, pipeline.Entry{
		Pipeline: p,
		Name:     name,
		Priority: priority,
	})
}

// AddDownloaderMiddleware 注册一个自定义下载器中间件。
// 必须在 Run 之前调用。priority 值越小越先执行 ProcessRequest。
//
// 用法：
//
//	c := crawler.NewDefault()
//	c.AddDownloaderMiddleware(&MyAuthMiddleware{}, "Auth", 450)
//	c.Run(ctx, mySpider)
func (c *Crawler) AddDownloaderMiddleware(mw dmiddle.DownloaderMiddleware, name string, priority int) {
	c.userDLMiddlewares = append(c.userDLMiddlewares, downloader.MiddlewareEntry{
		Middleware: mw,
		Name:       name,
		Priority:   priority,
	})
}

// AddSpiderMiddleware 注册一个自定义 Spider 中间件。
// 必须在 Run 之前调用。priority 值越小越先执行 ProcessSpiderInput。
//
// Spider 中间件拦截 Spider 的输入（响应）和输出（请求/Item），
// 可以修改、过滤或添加新的输出项。
//
// 用法：
//
//	c := crawler.NewDefault()
//	c.AddSpiderMiddleware(&MyFilterMiddleware{}, "Filter", 500)
//	c.Run(ctx, mySpider)
func (c *Crawler) AddSpiderMiddleware(mw smiddle.SpiderMiddleware, name string, priority int) {
	c.userSpiderMiddlewares = append(c.userSpiderMiddlewares, smiddle.Entry{
		Middleware: mw,
		Name:       name,
		Priority:   priority,
	})
}

// AddExtension 注册一个自定义扩展。
// 必须在 Run 之前调用。priority 值越小越先初始化。
//
// 扩展通过信号系统监听框架事件，实现自定义逻辑。
//
// 用法：
//
//	c := crawler.NewDefault()
//	c.AddExtension(&MyStatsExtension{}, "MyStats", 500)
//	c.Run(ctx, mySpider)
func (c *Crawler) AddExtension(ext extension.Extension, name string, priority int) {
	c.userExtensions = append(c.userExtensions, extension.Entry{
		Extension: ext,
		Name:      name,
		Priority:  priority,
	})
}

// AddFeed 注册一个数据导出（Feed Export）配置。
// 必须在 Run 之前调用。多次调用会追加到配置列表。
//
// 等价于 Scrapy 中在 Settings 的 FEEDS 字典中添加一个条目，
// 但以 Go 类型安全的方式注入配置。
//
// 用法：
//
//	c := crawler.NewDefault()
//	c.AddFeed(feedexport.FeedConfig{
//	    URI:    "output.json",
//	    Format: feedexport.FormatJSON,
//	    Overwrite: true,
//	})
//	c.Run(ctx, mySpider)
func (c *Crawler) AddFeed(cfg feedexport.FeedConfig) {
	c.userFeedConfigs = append(c.userFeedConfigs, cfg)
}

// Run 启动爬虫并阻塞直到完成。
// 支持 OS 信号优雅关闭（SIGINT、SIGTERM）。
//
// 每个 Crawler 实例只能运行一次，再次调用将返回错误。
// 如需并发/顺序运行多个 Spider，请使用 Runner。
func (c *Crawler) Run(ctx context.Context, sp spider.Spider) error {
	if !c.started.CompareAndSwap(false, true) {
		return errors.New("crawler already started; create a new Crawler for each run")
	}

	// 监听 OS 信号（SIGINT/SIGTERM），触发优雅关闭
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		select {
		case s, ok := <-sigCh:
			if !ok {
				return
			}
			if c.Logger != nil {
				c.Logger.Info("received OS signal, shutting down gracefully", "signal", s.String())
			}
			cancel()
		case <-ctx.Done():
		}
	}()

	return c.crawl(ctx, sp)
}

// Crawl 启动爬虫并阻塞直到完成（不安装 OS 信号处理器）。
// 此方法供 Runner 等上层编排器使用，由调用方通过 context 统一管理生命周期和信号处理。
//
// 每个 Crawler 实例只能运行一次，再次调用将返回错误。
func (c *Crawler) Crawl(ctx context.Context, sp spider.Spider) error {
	if !c.started.CompareAndSwap(false, true) {
		return errors.New("crawler already started; create a new Crawler for each run")
	}
	return c.crawl(ctx, sp)
}

// crawl 是 Crawler 的核心爬取逻辑，不安装 OS 信号处理器。
// 调用方必须保证 started 已被标记为 true（通过 Run 或 Crawl 的 CAS）。
func (c *Crawler) crawl(ctx context.Context, sp spider.Spider) error {
	c.spider = sp

	// 应用 Spider 级别的配置
	if spiderSettings := sp.CustomSettings(); spiderSettings != nil {
		if settingsMap := spiderSettings.ToMap(); len(settingsMap) > 0 {
			c.Settings.Update(settingsMap, settings.PrioritySpider)
		}
	}

	// Spider 配置可能覆盖了 LOG_LEVEL，重建受影响的组件
	if !c.customLogger {
		c.Logger = newDefaultLogger(c.Settings)
		c.Signals = sig.NewManager(c.Logger)
		c.Stats = stats.NewMemoryCollector(
			c.Settings.GetBool("STATS_DUMP", true),
			c.Logger,
		)
	}

	// 组装组件
	c.assembleComponents()

	// 执行 Runner 等上层编排器注入的 BeforeStart 钩子
	// （Signals/Stats 可能因 Spider CustomSettings 重建而变更，此时是最晚可注册跨爬虫信号处理器的时机）
	for _, hook := range c.beforeStartHooks {
		hook(c)
	}

	// 创建可取消的 context，保存 cancel 以供 Stop 触发优雅关闭
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.cancelMu.Lock()
	c.cancel = cancel
	c.cancelMu.Unlock()

	defer func() {
		c.cancelMu.Lock()
		c.cancel = nil
		c.cancelMu.Unlock()
	}()

	c.crawling.Store(true)
	defer c.crawling.Store(false)

	c.Logger.Info("spider started",
		"spider", sp.Name(),
		"concurrent_requests", c.Settings.GetInt("CONCURRENT_REQUESTS", 16),
	)

	// 启动引擎
	return c.engine.Start(runCtx)
}

// Stop 请求优雅停止爬虫，立即返回（不等待爬虫退出）。
//
// 多次调用是安全的；若爬虫未在运行，此方法为空操作。
// 如需等待爬虫完全停止，请在 Run/Crawl 返回后再处理。
func (c *Crawler) Stop() {
	c.cancelMu.Lock()
	cancel := c.cancel
	c.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Spider 返回当前 Crawler 关联的 Spider 实例。
// 在 Run/Crawl 调用之前返回 nil。
func (c *Crawler) Spider() spider.Spider {
	return c.spider
}

// IsCrawling 返回爬虫是否正在运行。
func (c *Crawler) IsCrawling() bool {
	return c.crawling.Load()
}

// onBeforeStart 注册一个在组件组装完成、Engine 启动之前调用的钩子。
// 此方法供 Runner 等上层编排器使用，不对外公开。
//
// 钩子执行时机：Crawl/Run 内部在 assembleComponents 之后、engine.Start 之前。
// 此时 Signals/Stats/Logger 均已就绪（可能因 Spider CustomSettings 被重建过）。
//
// 必须在 Crawl/Run 调用之前注册，否则可能错过触发时机。
func (c *Crawler) onBeforeStart(hook func(*Crawler)) {
	if hook == nil {
		return
	}
	c.beforeStartHooks = append(c.beforeStartHooks, hook)
}

// ============================================================================
// 组件组装
// ============================================================================

// assembleComponents 组装所有组件。
func (c *Crawler) assembleComponents() {
	// 1. 调度器
	c.scheduler = scheduler.NewDefaultScheduler(
		scheduler.WithStats(c.Stats),
		scheduler.WithSchedulerLogger(c.Logger),
		scheduler.WithDebug(c.Settings.GetBool("SCHEDULER_DEBUG", false)),
	)

	// 2. 下载器
	timeout := c.Settings.GetDuration("DOWNLOAD_TIMEOUT", 180*time.Second)
	handler := downloader.NewHTTPDownloadHandler(timeout)
	c.downloader = downloader.NewDownloader(c.Settings, handler, c.Signals, c.Stats, c.Logger)

	// 3. 下载器中间件
	c.dlMW = c.buildDownloaderMiddlewares()

	// 4. Spider 中间件
	c.spiderMW = c.buildSpiderMiddlewares()

	// 5. Item Pipeline
	c.pipelines = pipeline.NewManager(c.Signals, c.Stats, c.Logger)
	for _, entry := range c.userPipelines {
		c.pipelines.AddPipeline(entry.Pipeline, entry.Name, entry.Priority)
	}
	c.logEnabledPipelines()

	// 6. 扩展系统
	c.extensions = c.buildExtensions()

	// 7. Scraper
	maxActiveSize := c.Settings.GetInt("SCRAPER_SLOT_MAX_ACTIVE_SIZE", 5000000)
	c.scraper = scraper.NewScraper(c.spiderMW, c.pipelines, c.spider, c.Signals, c.Stats, c.Logger, maxActiveSize)

	// 8. Engine
	c.engine = engine.NewEngine(c.spider, c.scheduler, c.downloader, c.dlMW, c.scraper, c.Signals, c.Stats, c.Logger, c.extensions)
}

// MiddlewareFactory 定义内置中间件的工厂函数类型。
type MiddlewareFactory func(c *Crawler) dmiddle.DownloaderMiddleware

// componentEntry 表示一个启用的组件条目，用于日志打印。
type componentEntry struct {
	name     string
	priority int
	source   string // "builtin" or "custom"
}

// builtinMiddlewareFactories 是内置下载器中间件的注册表。
// key 为中间件名称，与 DOWNLOADER_MIDDLEWARES_BASE 中的名称一一对应。
var builtinMiddlewareFactories = map[string]MiddlewareFactory{
	"DownloadTimeout": func(c *Crawler) dmiddle.DownloaderMiddleware {
		timeout := c.Settings.GetDuration("DOWNLOAD_TIMEOUT", 180*time.Second)
		if timeout <= 0 {
			return nil
		}
		return dmiddle.NewDownloadTimeoutMiddleware(timeout, c.Logger)
	},
	"DefaultHeaders": func(c *Crawler) dmiddle.DownloaderMiddleware {
		defaultHeaders := c.Settings.Get("DEFAULT_REQUEST_HEADERS", nil)
		if headers, ok := defaultHeaders.(http.Header); ok {
			return dmiddle.NewDefaultHeadersMiddleware(headers)
		}
		// 没有配置默认请求头，返回 nil 表示跳过
		return nil
	},
	"HttpAuth": func(c *Crawler) dmiddle.DownloaderMiddleware {
		user := c.Settings.GetString("HTTP_USER", "")
		pass := c.Settings.GetString("HTTP_PASS", "")
		if user == "" && pass == "" {
			return nil
		}
		domain := c.Settings.GetString("HTTP_AUTH_DOMAIN", "")
		return dmiddle.NewHttpAuthMiddleware(user, pass, domain, c.Logger)
	},
	"UserAgent": func(c *Crawler) dmiddle.DownloaderMiddleware {
		userAgent := c.Settings.GetString("USER_AGENT", "scrapy-go/0.1.0")
		return dmiddle.NewUserAgentMiddleware(userAgent)
	},
	"Retry": func(c *Crawler) dmiddle.DownloaderMiddleware {
		if !c.Settings.GetBool("RETRY_ENABLED", true) {
			return nil
		}
		retryTimes := c.Settings.GetInt("RETRY_TIMES", 2)
		retryPriorityAdjust := c.Settings.GetInt("RETRY_PRIORITY_ADJUST", -1)
		retryHTTPCodes := c.getIntSlice("RETRY_HTTP_CODES", []int{500, 502, 503, 504, 522, 524, 408, 429})
		return dmiddle.NewRetryMiddleware(retryTimes, retryHTTPCodes, retryPriorityAdjust, c.Stats, c.Logger)
	},
	"Redirect": func(c *Crawler) dmiddle.DownloaderMiddleware {
		if !c.Settings.GetBool("REDIRECT_ENABLED", true) {
			return nil
		}
		maxRedirects := c.Settings.GetInt("REDIRECT_MAX_TIMES", 20)
		redirectPriorityAdjust := c.Settings.GetInt("REDIRECT_PRIORITY_ADJUST", 2)
		return dmiddle.NewRedirectMiddleware(maxRedirects, redirectPriorityAdjust, c.Logger)
	},
	"HttpCompression": func(c *Crawler) dmiddle.DownloaderMiddleware {
		if !c.Settings.GetBool("COMPRESSION_ENABLED", true) {
			return nil
		}
		maxSize := c.Settings.GetInt("DOWNLOAD_MAXSIZE", 1024*1024*1024)
		warnSize := c.Settings.GetInt("DOWNLOAD_WARNSIZE", 32*1024*1024)
		return dmiddle.NewHttpCompressionMiddleware(maxSize, warnSize, c.Stats, c.Logger)
	},
	"Cookies": func(c *Crawler) dmiddle.DownloaderMiddleware {
		if !c.Settings.GetBool("COOKIES_ENABLED", true) {
			return nil
		}
		debug := c.Settings.GetBool("COOKIES_DEBUG", false)
		return dmiddle.NewCookiesMiddleware(debug, c.Logger)
	},
	"HttpProxy": func(c *Crawler) dmiddle.DownloaderMiddleware {
		if !c.Settings.GetBool("HTTPPROXY_ENABLED", true) {
			return nil
		}
		return dmiddle.NewHttpProxyMiddleware(c.Logger)
	},
	"DownloaderStats": func(c *Crawler) dmiddle.DownloaderMiddleware {
		if !c.Settings.GetBool("DOWNLOADER_STATS", true) {
			return nil
		}
		return dmiddle.NewDownloaderStatsMiddleware(c.Stats, c.Logger)
	},
}

// buildDownloaderMiddlewares 构建下载器中间件链。
// 通过读取 DOWNLOADER_MIDDLEWARES_BASE 和 DOWNLOADER_MIDDLEWARES 配置，
// 决定启用哪些内置中间件及其优先级。
//
// 禁用内置中间件的方式：
//   - 通过配置：在 DOWNLOADER_MIDDLEWARES 中将中间件优先级设为负数（如 {"Retry": -1}）
//   - 通过开关：设置 RETRY_ENABLED=false 或 REDIRECT_ENABLED=false
//
// 修改内置中间件优先级的方式：
//   - 在 DOWNLOADER_MIDDLEWARES 中设置新的优先级（如 {"Retry": 100}）
func (c *Crawler) buildDownloaderMiddlewares() *downloader.MiddlewareManager {
	m := downloader.NewMiddlewareManager(c.Logger)

	// 收集所有中间件条目（内置 + 自定义），用于排序打印和优先级冲突检测
	var allEntries []componentEntry

	// 1. 读取合并后的中间件配置（已过滤掉优先级 < 0 的条目）
	mwConfig := c.Settings.GetComponentPriorityDictWithBase("DOWNLOADER_MIDDLEWARES")

	// 2. 根据配置实例化内置中间件
	for name, priority := range mwConfig {
		if factory, ok := builtinMiddlewareFactories[name]; ok {
			mw := factory(c)
			if mw != nil {
				m.AddMiddleware(mw, name, priority)
				allEntries = append(allEntries, componentEntry{name: name, priority: priority, source: "builtin"})
			}
		}
		// 非内置中间件名称在配置中会被忽略，用户自定义中间件通过 AddDownloaderMiddleware 注册
	}

	// 3. 添加用户通过 AddDownloaderMiddleware 注册的自定义中间件
	for _, entry := range c.userDLMiddlewares {
		m.AddMiddleware(entry.Middleware, entry.Name, entry.Priority)
		allEntries = append(allEntries, componentEntry{name: entry.Name, priority: entry.Priority, source: "custom"})
	}

	// 4. 按优先级排序
	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].priority < allEntries[j].priority
	})

	// 5. 检测优先级冲突
	priorityMap := make(map[int][]string)
	for _, entry := range allEntries {
		priorityMap[entry.priority] = append(priorityMap[entry.priority], entry.name)
	}
	for priority, names := range priorityMap {
		if len(names) > 1 {
			c.Logger.Warn("multiple downloader middlewares share the same priority, execution order may be non-deterministic",
				"priority", priority,
				"middlewares", names,
			)
		}
	}

	// 6. 打印启用列表
	c.logEnabledComponents("downloader middlewares", allEntries)
	return m
}

// SpiderMiddlewareFactory 定义内置 Spider 中间件的工厂函数类型。
type SpiderMiddlewareFactory func(c *Crawler) smiddle.SpiderMiddleware

// builtinSpiderMiddlewareFactories 是内置 Spider 中间件的注册表。
// key 为中间件名称，与 SPIDER_MIDDLEWARES_BASE 中的名称一一对应。
var builtinSpiderMiddlewareFactories = map[string]SpiderMiddlewareFactory{
	"HttpError": func(c *Crawler) smiddle.SpiderMiddleware {
		allowAll := c.Settings.GetBool("HTTPERROR_ALLOW_ALL", false)
		allowCodes := c.getIntSlice("HTTPERROR_ALLOWED_CODES", nil)
		return smiddle.NewHttpErrorMiddleware(allowAll, allowCodes, c.Stats, c.Logger)
	},
	"UrlLength": func(c *Crawler) smiddle.SpiderMiddleware {
		maxLength := c.Settings.GetInt("URLLENGTH_LIMIT", 2083)
		if maxLength <= 0 {
			return nil
		}
		return smiddle.NewUrlLengthMiddleware(maxLength, c.Stats, c.Logger)
	},
	"Depth": func(c *Crawler) smiddle.SpiderMiddleware {
		maxDepth := c.Settings.GetInt("DEPTH_LIMIT", 0)
		priority := c.Settings.GetInt("DEPTH_PRIORITY", 0)
		verbose := c.Settings.GetBool("DEPTH_STATS_VERBOSE", false)
		return smiddle.NewDepthMiddleware(maxDepth, priority, verbose, c.Stats, c.Logger)
	},
	"Offsite": func(c *Crawler) smiddle.SpiderMiddleware {
		// 从 Spider 获取 AllowedDomains（如果有的话）
		var allowedDomains []string
		if c.spider != nil {
			if domainSpider, ok := c.spider.(interface{ AllowedDomains() []string }); ok {
				allowedDomains = domainSpider.AllowedDomains()
			}
		}
		return smiddle.NewOffsiteMiddleware(allowedDomains, c.Stats, c.Logger)
	},
	"Referer": func(c *Crawler) smiddle.SpiderMiddleware {
		if !c.Settings.GetBool("REFERER_ENABLED", true) {
			return nil
		}
		return smiddle.NewRefererMiddleware()
	},
}

// buildSpiderMiddlewares 构建 Spider 中间件链。
// 通过读取 SPIDER_MIDDLEWARES_BASE 和 SPIDER_MIDDLEWARES 配置，
// 决定启用哪些内置中间件及其优先级。
//
// 禁用中间件的方式：
//   - 通过配置：在 SPIDER_MIDDLEWARES 中将中间件优先级设为负数（如 {"Depth": -1}）
//   - 通过开关：设置 REFERER_ENABLED=false
func (c *Crawler) buildSpiderMiddlewares() *smiddle.Manager {
	m := smiddle.NewManager(c.Logger)

	// 收集所有中间件条目（内置 + 自定义），用于排序打印和优先级冲突检测
	var allEntries []componentEntry

	// 1. 读取合并后的中间件配置（已过滤掉优先级 < 0 的条目）
	mwConfig := c.Settings.GetComponentPriorityDictWithBase("SPIDER_MIDDLEWARES")

	// 2. 根据配置实例化内置中间件
	for name, priority := range mwConfig {
		if factory, ok := builtinSpiderMiddlewareFactories[name]; ok {
			mw := factory(c)
			if mw != nil {
				m.AddMiddleware(mw, name, priority)
				allEntries = append(allEntries, componentEntry{name: name, priority: priority, source: "builtin"})
			}
		}
	}

	// 3. 添加用户通过 AddSpiderMiddleware 注册的自定义中间件
	for _, entry := range c.userSpiderMiddlewares {
		m.AddMiddleware(entry.Middleware, entry.Name, entry.Priority)
		allEntries = append(allEntries, componentEntry{name: entry.Name, priority: entry.Priority, source: "custom"})
	}

	// 3. 按优先级排序
	if len(allEntries) > 0 {
		sort.Slice(allEntries, func(i, j int) bool {
			return allEntries[i].priority < allEntries[j].priority
		})

		// 检测优先级冲突
		priorityMap := make(map[int][]string)
		for _, entry := range allEntries {
			priorityMap[entry.priority] = append(priorityMap[entry.priority], entry.name)
		}
		for priority, names := range priorityMap {
			if len(names) > 1 {
				c.Logger.Warn("multiple spider middlewares share the same priority, execution order may be non-deterministic",
					"priority", priority,
					"middlewares", names,
				)
			}
		}
	}

	// 4. 打印启用列表（Scrapy 风格）
	c.logEnabledComponents("spider middlewares", allEntries)
	return m
}

// ExtensionFactory 定义内置扩展的工厂函数类型。
type ExtensionFactory func(c *Crawler) extension.Extension

// buildFeedExportConfigs 构建 Feed Export 扩展所需的配置列表。
//
// 合并来源：
//  1. 用户通过 AddFeed 注册的 FeedConfig
//  2. Settings 中 FEEDS (map[string]map[string]any) 配置
//  3. 兼容性：Settings 中 FEED_URI + FEED_FORMAT（单个输出）
//
// 后两者以 PriorityDefault 附加到结果中，确保代码注入的配置具有最高优先级。
func (c *Crawler) buildFeedExportConfigs() []feedexport.FeedConfig {
	configs := make([]feedexport.FeedConfig, 0, len(c.userFeedConfigs)+1)
	configs = append(configs, c.userFeedConfigs...)

	// 从 FEEDS 设置读取（map[string]map[string]any 形式）
	if raw := c.Settings.Get("FEEDS", nil); raw != nil {
		if feeds, ok := raw.(map[string]map[string]any); ok {
			for uri, opts := range feeds {
				configs = append(configs, feedConfigFromMap(uri, opts, c.Settings))
			}
		} else if feeds, ok := raw.(map[string]any); ok {
			for uri, v := range feeds {
				if opts, ok := v.(map[string]any); ok {
					configs = append(configs, feedConfigFromMap(uri, opts, c.Settings))
				}
			}
		}
	}

	// 向后兼容：FEED_URI + FEED_FORMAT
	if uri := c.Settings.GetString("FEED_URI", ""); uri != "" {
		format := c.Settings.GetString("FEED_FORMAT", "jsonlines")
		configs = append(configs, feedConfigFromMap(uri, map[string]any{
			"format": format,
		}, c.Settings))
	}

	return configs
}

// feedConfigFromMap 从 map 形式的选项构造 FeedConfig，并应用 Settings 中的全局默认值。
func feedConfigFromMap(uri string, opts map[string]any, s *settings.Settings) feedexport.FeedConfig {
	cfg := feedexport.FeedConfig{
		URI:        uri,
		Options:    feedexport.DefaultExporterOptions(),
		StoreEmpty: s.GetBool("FEED_STORE_EMPTY", true),
	}

	// 全局默认
	if enc := s.GetString("FEED_EXPORT_ENCODING", ""); enc != "" {
		cfg.Options.Encoding = enc
	}
	if indent := s.GetInt("FEED_EXPORT_INDENT", 0); indent > 0 {
		cfg.Options.Indent = indent
	}

	// 每条 Feed 独立覆盖
	if v, ok := opts["format"]; ok {
		if s, ok := v.(string); ok {
			cfg.Format = feedexport.NormalizeFormat(s)
		}
	}
	if v, ok := opts["overwrite"]; ok {
		if b, ok := v.(bool); ok {
			cfg.Overwrite = b
		}
	}
	if v, ok := opts["store_empty"]; ok {
		if b, ok := v.(bool); ok {
			cfg.StoreEmpty = b
		}
	}
	if v, ok := opts["encoding"]; ok {
		if s, ok := v.(string); ok && s != "" {
			cfg.Options.Encoding = s
		}
	}
	if v, ok := opts["indent"]; ok {
		switch n := v.(type) {
		case int:
			cfg.Options.Indent = n
		case int64:
			cfg.Options.Indent = int(n)
		case float64:
			cfg.Options.Indent = int(n)
		}
	}
	if v, ok := opts["fields"]; ok {
		switch fs := v.(type) {
		case []string:
			cfg.Options.FieldsToExport = append([]string(nil), fs...)
		case []any:
			names := make([]string, 0, len(fs))
			for _, x := range fs {
				if s, ok := x.(string); ok {
					names = append(names, s)
				}
			}
			cfg.Options.FieldsToExport = names
		}
	}
	if v, ok := opts["item_element"]; ok {
		if s, ok := v.(string); ok {
			cfg.Options.ItemElement = s
		}
	}
	if v, ok := opts["root_element"]; ok {
		if s, ok := v.(string); ok {
			cfg.Options.RootElement = s
		}
	}
	return cfg
}

// builtinExtensionFactories 是内置扩展的注册表。
// key 为扩展名称，与 EXTENSIONS_BASE 中的名称一一对应。
var builtinExtensionFactories = map[string]ExtensionFactory{
	"CoreStats": func(c *Crawler) extension.Extension {
		return extension.NewCoreStatsExtension(c.Stats, c.Signals, c.Logger)
	},
	"CloseSpider": func(c *Crawler) extension.Extension {
		timeout := c.Settings.GetFloat("CLOSESPIDER_TIMEOUT", 0)
		itemCount := c.Settings.GetInt("CLOSESPIDER_ITEMCOUNT", 0)
		pageCount := c.Settings.GetInt("CLOSESPIDER_PAGECOUNT", 0)
		errorCount := c.Settings.GetInt("CLOSESPIDER_ERRORCOUNT", 0)
		return extension.NewCloseSpiderExtension(timeout, itemCount, pageCount, errorCount, c.Signals, c.Stats, c.Logger)
	},
	"LogStats": func(c *Crawler) extension.Extension {
		interval := c.Settings.GetFloat("LOGSTATS_INTERVAL", 60.0)
		return extension.NewLogStatsExtension(interval, c.Stats, c.Signals, c.Logger)
	},
	"MemoryUsage": func(c *Crawler) extension.Extension {
		enabled := c.Settings.GetBool("MEMUSAGE_ENABLED", true)
		limitMB := c.Settings.GetInt("MEMUSAGE_LIMIT_MB", 0)
		warningMB := c.Settings.GetInt("MEMUSAGE_WARNING_MB", 0)
		checkInterval := c.Settings.GetFloat("MEMUSAGE_CHECK_INTERVAL_SECONDS", 60.0)
		return extension.NewMemoryUsageExtension(enabled, limitMB, warningMB, checkInterval, c.Stats, c.Signals, c.Logger)
	},
	"FeedExport": func(c *Crawler) extension.Extension {
		configs := c.buildFeedExportConfigs()
		if len(configs) == 0 {
			// 未配置 FEEDS，跳过（工厂返回 nil 会在 buildExtensions 中被跳过）
			return nil
		}
		return extension.NewFeedExportExtension(configs, c.Signals, c.Stats, c.Logger)
	},
}

// buildExtensions 构建扩展管理器。
// 通过读取 EXTENSIONS_BASE 和 EXTENSIONS 配置，
// 决定启用哪些内置扩展及其优先级。
func (c *Crawler) buildExtensions() *extension.Manager {
	m := extension.NewManager(c.Logger)

	var allEntries []componentEntry

	// 1. 读取合并后的扩展配置（已过滤掉优先级 < 0 的条目）
	extConfig := c.Settings.GetComponentPriorityDictWithBase("EXTENSIONS")

	// 2. 根据配置实例化内置扩展
	for name, priority := range extConfig {
		if factory, ok := builtinExtensionFactories[name]; ok {
			ext := factory(c)
			if ext != nil {
				m.AddExtension(ext, name, priority)
				allEntries = append(allEntries, componentEntry{name: name, priority: priority, source: "builtin"})
			}
		}
	}

	// 3. 添加用户通过 AddExtension 注册的自定义扩展
	for _, entry := range c.userExtensions {
		m.AddExtension(entry.Extension, entry.Name, entry.Priority)
		allEntries = append(allEntries, componentEntry{name: entry.Name, priority: entry.Priority, source: "custom"})
	}

	// 4. 按优先级排序
	if len(allEntries) > 0 {
		sort.Slice(allEntries, func(i, j int) bool {
			return allEntries[i].priority < allEntries[j].priority
		})

		// 检测优先级冲突
		priorityMap := make(map[int][]string)
		for _, entry := range allEntries {
			priorityMap[entry.priority] = append(priorityMap[entry.priority], entry.name)
		}
		for priority, names := range priorityMap {
			if len(names) > 1 {
				c.Logger.Warn("multiple extensions share the same priority, execution order may be non-deterministic",
					"priority", priority,
					"extensions", names,
				)
			}
		}
	}

	// 5. 打印启用列表
	c.logEnabledComponents("extensions", allEntries)
	return m
}

// logEnabledPipelines 打印启用的 Item Pipeline 列表。
func (c *Crawler) logEnabledPipelines() {
	var entries []componentEntry
	for _, entry := range c.userPipelines {
		entries = append(entries, componentEntry{name: entry.Name, priority: entry.Priority, source: "custom"})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].priority < entries[j].priority
	})

	c.logEnabledComponents("item pipelines", entries)
}

// logEnabledComponents 打印启用的组件列表。
func (c *Crawler) logEnabledComponents(componentName string, entries []componentEntry) {
	if len(entries) == 0 {
		c.Logger.Info(fmt.Sprintf("Enabled %s: (none)", componentName))
		return
	}

	var lines []string
	for _, e := range entries {
		color := sslog.ColorByPriority(e.priority)
		lines = append(lines, fmt.Sprintf("  %s%s (%d)%s", color, e.name, e.priority, sslog.ColorReset))
	}
	c.Logger.Info(fmt.Sprintf("Enabled %s:\n%s", componentName, strings.Join(lines, "\n")))
}

// getIntSlice 从配置中获取 int 切片。
func (c *Crawler) getIntSlice(key string, defaultVal []int) []int {
	v := c.Settings.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	if codes, ok := v.([]int); ok {
		return codes
	}
	return defaultVal
}

// ============================================================================
// Option 模式
// ============================================================================

// Option 是 Crawler 的可选配置函数。
type Option func(*Crawler)

// WithSettings 设置自定义配置。
func WithSettings(s *settings.Settings) Option {
	return func(c *Crawler) {
		c.Settings = s
	}
}

// WithLogger 设置自定义日志记录器。
// 使用自定义 Logger 后，Spider 的 CustomSettings 中的 LOG_LEVEL 不会覆盖该 Logger。
func WithLogger(logger *slog.Logger) Option {
	return func(c *Crawler) {
		c.Logger = logger
		c.customLogger = true
	}
}

// WithStats 设置统计收集器。
func WithStats(sc stats.Collector) Option {
	return func(c *Crawler) {
		c.Stats = sc
	}
}

// WithSignals 设置信号管理器。
func WithSignals(sm *sig.Manager) Option {
	return func(c *Crawler) {
		c.Signals = sm
	}
}
