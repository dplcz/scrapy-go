package crawler_test

import (
	"context"
	"fmt"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// mySpider 是一个示例 Spider，用于演示 Crawler 的基本用法。
type mySpider struct {
	spider.Base
}

func newMySpider() *mySpider {
	return &mySpider{
		Base: spider.Base{
			SpiderName: "example",
			StartURLs:  []string{"https://example.com"},
		},
	}
}

func (s *mySpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	fmt.Printf("Got response: %d %s\n", response.Status, response.URL)
	return nil, nil
}

// ExampleNew 演示创建一个使用默认配置的 Crawler 并运行单个 Spider。
//
// Crawler 是 scrapy-go 的顶层编排器，负责组装所有组件并启动爬取。
// 每个 Crawler 实例只能运行一次，如需多次爬取请创建新实例。
func ExampleNew() {
	// 创建默认配置的 Crawler
	c := crawler.New()

	// 创建 Spider 实例
	sp := newMySpider()

	// 启动爬取（阻塞直到完成）
	// 支持 OS 信号优雅关闭（SIGINT/SIGTERM）
	ctx := context.Background()
	err := c.Run(ctx, sp)
	if err != nil {
		fmt.Printf("crawl failed: %v\n", err)
	}
}

// ExampleNew_withOptions 演示通过 Option 模式自定义 Crawler 配置。
//
// 支持的 Option 包括：
//   - WithSettings：自定义配置
//   - WithLogger：自定义日志记录器
//   - WithStats：自定义统计收集器
//   - WithSignals：自定义信号管理器
func ExampleNew_withOptions() {
	// 创建自定义配置
	s := settings.New()
	s.Set("CONCURRENT_REQUESTS", 32, settings.PriorityCommand)
	s.Set("DOWNLOAD_TIMEOUT", "30s", settings.PriorityCommand)
	s.Set("LOG_LEVEL", "INFO", settings.PriorityCommand)

	// 使用自定义配置创建 Crawler
	c := crawler.New(
		crawler.WithSettings(s),
	)

	// 注册自定义 Pipeline（priority 越小越先执行）
	// c.AddPipeline(&myPipeline{}, "MyPipeline", 300)

	// 注册自定义下载器中间件
	// c.AddDownloaderMiddleware(&myMiddleware{}, "MyMW", 500)

	// 启动爬取
	ctx := context.Background()
	err := c.Run(ctx, newMySpider())
	if err != nil {
		fmt.Printf("crawl failed: %v\n", err)
	}
}

// ExampleRunner_startConcurrent 演示使用 Runner 并发运行多个 Spider。
//
// Runner 管理多个 Crawler 的并发执行，提供：
//   - 统一的 OS 信号处理和优雅关闭
//   - 跨爬虫信号传播（ConnectSignal）
//   - 错误聚合（errors.Join）
func ExampleRunner_startConcurrent() {
	// 创建 Runner
	runner := crawler.NewRunner()

	// 创建多个 Spider
	spiderA := &mySpider{Base: spider.Base{SpiderName: "spider-a", StartURLs: []string{"https://a.example.com"}}}
	spiderB := &mySpider{Base: spider.Base{SpiderName: "spider-b", StartURLs: []string{"https://b.example.com"}}}

	// 并发启动多个爬虫（阻塞直到全部完成）
	ctx := context.Background()
	err := runner.StartConcurrent(ctx,
		crawler.NewJob(crawler.New(), spiderA),
		crawler.NewJob(crawler.New(), spiderB),
	)
	if err != nil {
		fmt.Printf("concurrent crawl failed: %v\n", err)
	}
}

// ExampleRunner_startSequentially 演示使用 Runner 顺序运行多个 Spider。
//
// 顺序模式下，前一个 Spider 完成后再启动下一个。
// 如果 context 被取消，后续未启动的 Spider 将被跳过。
func ExampleRunner_startSequentially() {
	runner := crawler.NewRunner()

	spiderA := &mySpider{Base: spider.Base{SpiderName: "spider-1", StartURLs: []string{"https://first.example.com"}}}
	spiderB := &mySpider{Base: spider.Base{SpiderName: "spider-2", StartURLs: []string{"https://second.example.com"}}}

	// 顺序启动：spider-1 完成后再启动 spider-2
	ctx := context.Background()
	err := runner.StartSequentially(ctx,
		crawler.NewJob(crawler.New(), spiderA),
		crawler.NewJob(crawler.New(), spiderB),
	)
	if err != nil {
		fmt.Printf("sequential crawl failed: %v\n", err)
	}
}
