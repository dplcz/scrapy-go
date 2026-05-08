// Package crawler 实现了 scrapy-go 框架的顶层编排器。
//
// # 概述
//
// crawler 包是 scrapy-go 框架的入口层，提供两个核心类型：
//   - [Crawler]：单爬虫编排器，组装所有组件（Engine、Scheduler、Downloader、Scraper 等）并启动爬取
//   - [Runner]：多爬虫调度器，支持并发或顺序运行多个 Spider，统一管理生命周期和信号传播
//
// 对应 Scrapy Python 版本中 scrapy.crawler 模块的 Crawler / CrawlerRunner / CrawlerProcess。
//
// # 架构定位
//
// 在 scrapy-go 的分层架构中，crawler 包位于最顶层：
//
//	┌─────────────────────────────────────────────────────┐
//	│                  用户代码                           │
//	├─────────────────────────────────────────────────────┤
//	│  crawler.Crawler / crawler.Runner  ← 本包           │
//	├─────────────────────────────────────────────────────┤
//	│  engine.Engine（核心调度引擎）                      │
//	├──────────┬──────────┬──────────┬────────────────────┤
//	│ Scheduler│Downloader│ Scraper  │ Extension/Signal   │
//	└──────────┴──────────┴──────────┴────────────────────┘
//
// Crawler 负责：
//   - 读取并合并配置（Settings 优先级：Default < Addon < Spider < Command）
//   - 根据配置实例化内置中间件、扩展、Pipeline
//   - 注册用户自定义组件（中间件、Pipeline、扩展、Feed 导出）
//   - 将所有组件注入 Engine 并启动爬取
//   - 监听 OS 信号（SIGINT/SIGTERM）实现优雅关闭
//
// # 使用方式
//
// 最简单的使用方式——创建 Crawler 并运行单个 Spider：
//
//	c := crawler.New()
//	err := c.Run(ctx, mySpider)
//
// 通过 Option 自定义组件：
//
//	c := crawler.New(
//	    crawler.WithSettings(mySettings),
//	    crawler.WithLogger(myLogger),
//	)
//	c.AddPipeline(&JsonPipeline{}, "JSON", 300)
//	c.AddDownloaderMiddleware(&ProxyRotator{}, "ProxyRotator", 750)
//	err := c.Run(ctx, mySpider)
//
// 使用 Runner 并发运行多个 Spider：
//
//	runner := crawler.NewRunner()
//	err := runner.StartConcurrent(ctx,
//	    crawler.NewJob(crawler.New(), spiderA),
//	    crawler.NewJob(crawler.New(), spiderB),
//	)
//
// # 与 Scrapy 的差异
//
// Go 版本在保留 Scrapy 核心设计理念的同时，针对 Go 语言特性做了以下调整：
//   - 舍弃 Python spider_loader（通过字符串名加载 Spider 类）——Go 直接传入 Spider 实例
//   - 舍弃 CrawlerProcess（Twisted reactor 生命周期管理）——Go 无全局 reactor，使用 context 传播取消
//   - 使用 Option 模式替代 Python 的 kwargs 配置
//   - 使用 sync.WaitGroup + errgroup 替代 Twisted Deferred / asyncio.Task
//   - 使用 errors.Join 聚合多个 Crawler 的错误
//   - Crawler 实例只能运行一次（不可复用），确保并发安全
//
// # 并发安全
//
// Crawler 的公共方法（Run、Crawl、Stop、IsCrawling、Spider）均为并发安全。
// Runner 的所有公共方法均可被多个 goroutine 安全调用。
//
// # 优雅关闭
//
// Crawler.Run 会自动安装 OS 信号处理器：
//   - 第一次 SIGINT/SIGTERM：触发优雅关闭，等待 in-flight 请求完成和 Pipeline 排空
//   - 第二次 SIGINT/SIGTERM：强制退出进程
//
// Runner.StartConcurrent/StartSequentially 同样支持两阶段信号处理，
// 并将取消信号广播给所有管理的 Crawler。
package crawler
