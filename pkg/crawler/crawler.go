// Package crawler 实现了 scrapy-go 框架的顶层编排器。
//
// Crawler 负责组装所有组件（Engine、Scheduler、Downloader、Scraper 等），
// 提供简洁的 API 供用户启动爬虫。
// 对应 Scrapy Python 版本中 scrapy.crawler 模块的功能。
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

	"scrapy-go/pkg/downloader"
	dl_mw "scrapy-go/pkg/downloader/middleware"
	"scrapy-go/pkg/engine"
	scrapy_log "scrapy-go/pkg/log"
	"scrapy-go/pkg/pipeline"
	"scrapy-go/pkg/scheduler"
	"scrapy-go/pkg/scraper"
	"scrapy-go/pkg/settings"
	sig "scrapy-go/pkg/signal"
	"scrapy-go/pkg/spider"
	spider_mw "scrapy-go/pkg/spider/middleware"
	"scrapy-go/pkg/stats"
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
	spiderMW   *spider_mw.Manager
	pipelines  *pipeline.Manager
	scraper    *scraper.Scraper

	// customLogger 标记用户是否通过 WithLogger 自定义 Logger
	customLogger bool

	// userPipelines 存储用户注册的自定义 Pipeline
	userPipelines []pipeline.Entry

	// userDLMiddlewares 存储用户注册的自定义下载器中间件
	userDLMiddlewares []downloader.MiddlewareEntry

	// userSpiderMiddlewares 存储用户注册的自定义 Spider 中间件
	userSpiderMiddlewares []spider_mw.Entry
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
	return scrapy_log.NewColorLogger(levelStr, nil, false)
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
func (c *Crawler) AddDownloaderMiddleware(mw dl_mw.DownloaderMiddleware, name string, priority int) {
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
func (c *Crawler) AddSpiderMiddleware(mw spider_mw.SpiderMiddleware, name string, priority int) {
	c.userSpiderMiddlewares = append(c.userSpiderMiddlewares, spider_mw.Entry{
		Middleware: mw,
		Name:       name,
		Priority:   priority,
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

	// 6. Scraper
	maxActiveSize := c.Settings.GetInt("SCRAPER_SLOT_MAX_ACTIVE_SIZE", 5000000)
	c.scraper = scraper.NewScraper(c.spiderMW, c.pipelines, c.spider, c.Signals, c.Stats, c.Logger, maxActiveSize)

	// 7. Engine
	c.engine = engine.NewEngine(c.spider, c.scheduler, c.downloader, c.dlMW, c.scraper, c.Signals, c.Stats, c.Logger)
}

// MiddlewareFactory 定义内置中间件的工厂函数类型。
type MiddlewareFactory func(c *Crawler) dl_mw.DownloaderMiddleware

// componentEntry 表示一个启用的组件条目，用于日志打印。
type componentEntry struct {
	name     string
	priority int
	source   string // "builtin" or "custom"
}

// builtinMiddlewareFactories 是内置下载器中间件的注册表。
// key 为中间件名称，与 DOWNLOADER_MIDDLEWARES_BASE 中的名称一一对应。
var builtinMiddlewareFactories = map[string]MiddlewareFactory{
	"DownloadTimeout": func(c *Crawler) dl_mw.DownloaderMiddleware {
		timeout := c.Settings.GetDuration("DOWNLOAD_TIMEOUT", 180*time.Second)
		if timeout <= 0 {
			return nil
		}
		return dl_mw.NewDownloadTimeoutMiddleware(timeout, c.Logger)
	},
	"DefaultHeaders": func(c *Crawler) dl_mw.DownloaderMiddleware {
		defaultHeaders := c.Settings.Get("DEFAULT_REQUEST_HEADERS", nil)
		if headers, ok := defaultHeaders.(http.Header); ok {
			return dl_mw.NewDefaultHeadersMiddleware(headers)
		}
		// 没有配置默认请求头，返回 nil 表示跳过
		return nil
	},
	"HttpAuth": func(c *Crawler) dl_mw.DownloaderMiddleware {
		user := c.Settings.GetString("HTTP_USER", "")
		pass := c.Settings.GetString("HTTP_PASS", "")
		if user == "" && pass == "" {
			return nil
		}
		domain := c.Settings.GetString("HTTP_AUTH_DOMAIN", "")
		return dl_mw.NewHttpAuthMiddleware(user, pass, domain, c.Logger)
	},
	"UserAgent": func(c *Crawler) dl_mw.DownloaderMiddleware {
		userAgent := c.Settings.GetString("USER_AGENT", "scrapy-go/0.1.0")
		return dl_mw.NewUserAgentMiddleware(userAgent)
	},
	"Retry": func(c *Crawler) dl_mw.DownloaderMiddleware {
		if !c.Settings.GetBool("RETRY_ENABLED", true) {
			return nil
		}
		retryTimes := c.Settings.GetInt("RETRY_TIMES", 2)
		retryPriorityAdjust := c.Settings.GetInt("RETRY_PRIORITY_ADJUST", -1)
		retryHTTPCodes := c.getIntSlice("RETRY_HTTP_CODES", []int{500, 502, 503, 504, 522, 524, 408, 429})
		return dl_mw.NewRetryMiddleware(retryTimes, retryHTTPCodes, retryPriorityAdjust, c.Stats, c.Logger)
	},
	"Redirect": func(c *Crawler) dl_mw.DownloaderMiddleware {
		if !c.Settings.GetBool("REDIRECT_ENABLED", true) {
			return nil
		}
		maxRedirects := c.Settings.GetInt("REDIRECT_MAX_TIMES", 20)
		redirectPriorityAdjust := c.Settings.GetInt("REDIRECT_PRIORITY_ADJUST", 2)
		return dl_mw.NewRedirectMiddleware(maxRedirects, redirectPriorityAdjust, c.Logger)
	},
	"HttpCompression": func(c *Crawler) dl_mw.DownloaderMiddleware {
		if !c.Settings.GetBool("COMPRESSION_ENABLED", true) {
			return nil
		}
		maxSize := c.Settings.GetInt("DOWNLOAD_MAXSIZE", 1024*1024*1024)
		warnSize := c.Settings.GetInt("DOWNLOAD_WARNSIZE", 32*1024*1024)
		return dl_mw.NewHttpCompressionMiddleware(maxSize, warnSize, c.Stats, c.Logger)
	},
	"Cookies": func(c *Crawler) dl_mw.DownloaderMiddleware {
		if !c.Settings.GetBool("COOKIES_ENABLED", true) {
			return nil
		}
		debug := c.Settings.GetBool("COOKIES_DEBUG", false)
		return dl_mw.NewCookiesMiddleware(debug, c.Logger)
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

	// 6. 打印启用列表（Scrapy 风格）
	c.logEnabledComponents("downloader middlewares", allEntries)
	return m
}

// buildSpiderMiddlewares 构建 Spider 中间件链。
// 通过读取 SPIDER_MIDDLEWARES_BASE 和 SPIDER_MIDDLEWARES 配置，
// 决定启用哪些内置中间件及其优先级。
//
// 目前没有内置的 Spider 中间件，用户可通过 AddSpiderMiddleware 注册自定义中间件。
// 未来可添加内置中间件（如 DepthMiddleware、HttpErrorMiddleware 等）。
//
// 禁用中间件的方式：
//   - 通过配置：在 SPIDER_MIDDLEWARES 中将中间件优先级设为负数（如 {"Depth": -1}）
func (c *Crawler) buildSpiderMiddlewares() *spider_mw.Manager {
	m := spider_mw.NewManager(c.Logger)

	// 收集所有中间件条目（内置 + 自定义），用于排序打印和优先级冲突检测
	var allEntries []componentEntry

	// 1. 读取合并后的中间件配置（已过滤掉优先级 < 0 的条目）
	// 目前 SPIDER_MIDDLEWARES_BASE 为空，预留给未来内置中间件
	_ = c.Settings.GetComponentPriorityDictWithBase("SPIDER_MIDDLEWARES")

	// 2. 添加用户通过 AddSpiderMiddleware 注册的自定义中间件
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

// logEnabledComponents 以 Scrapy 风格打印启用的组件列表。
// 不同优先级的组件使用不同颜色：
//   - 0~299 (低优先级/先执行): 绿色
//   - 300~599 (中优先级): 黄色
//   - 600+ (高优先级/后执行): 品红色
//
// 输出格式：
//
//	Enabled downloader middlewares:
//	  DownloadTimeout (300)
//	  UserAgent (500)
//	  Retry (550)
func (c *Crawler) logEnabledComponents(componentName string, entries []componentEntry) {
	if len(entries) == 0 {
		c.Logger.Info(fmt.Sprintf("Enabled %s: (none)", componentName))
		return
	}

	var lines []string
	for _, e := range entries {
		color := scrapy_log.ColorByPriority(e.priority)
		lines = append(lines, fmt.Sprintf("  %s%s (%d)%s", color, e.name, e.priority, scrapy_log.ColorReset))
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
