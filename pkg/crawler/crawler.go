// Package crawler 实现了 scrapy-go 框架的顶层编排器。
//
// Crawler 负责组装所有组件（Engine、Scheduler、Downloader、Scraper 等），
// 提供简洁的 API 供用户启动爬虫。
package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/dplcz/scrapy-go/pkg/downloader"
	dmiddle "github.com/dplcz/scrapy-go/pkg/downloader/middleware"
	"github.com/dplcz/scrapy-go/pkg/engine"
	"github.com/dplcz/scrapy-go/pkg/extension"
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

// Run 启动爬虫并阻塞直到完成。
// 支持 OS 信号优雅关闭（SIGINT、SIGTERM）。
func (c *Crawler) Run(ctx context.Context, sp spider.Spider) error {
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

	// 创建可取消的 context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 监听 OS 信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case s := <-sigCh:
			c.Logger.Info("received OS signal, shutting down gracefully", "signal", s.String())
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()

	c.Logger.Info("spider started",
		"spider", sp.Name(),
		"concurrent_requests", c.Settings.GetInt("CONCURRENT_REQUESTS", 16),
	)

	// 启动引擎
	return c.engine.Start(ctx)
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
