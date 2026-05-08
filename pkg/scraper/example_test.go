package scraper_test

import (
	"context"
	"fmt"
	"log/slog"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/scraper"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
	smiddle "github.com/dplcz/scrapy-go/pkg/spider/middleware"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// exampleSpider 是一个用于演示的最小 Spider 实现。
type exampleSpider struct {
	spider.Base
}

func (s *exampleSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	return []spider.Output{
		{Item: map[string]string{"title": "Example", "url": response.URL.String()}},
	}, nil
}

// ExampleNewScraper 演示如何创建和使用 Scraper。
//
// 注意：通常不需要直接使用 Scraper，而是通过 crawler.Crawler 自动组装。
// 此示例仅用于说明 Scraper 的内部结构和处理流程。
func ExampleNewScraper() {
	// 创建依赖组件
	sp := &exampleSpider{Base: spider.Base{SpiderName: "example"}}
	signals := signal.NewManager(nil)
	statsCollector := stats.NewMemoryCollector(false, slog.Default())
	spiderMW := smiddle.NewManager(slog.Default())
	pipelines := pipeline.NewManager(signals, statsCollector, slog.Default())

	// 创建 Scraper
	// maxActiveSize: 活跃响应最大总大小（5MB）
	// concurrentItems: 同时处理的 Item 上限（100）
	sc := scraper.NewScraper(
		spiderMW,
		pipelines,
		sp,
		signals,
		statsCollector,
		slog.Default(),
		5000000, // maxActiveSize
		100,     // concurrentItems
	)

	// 打开 Scraper（初始化 Pipeline）
	ctx := context.Background()
	if err := sc.Open(ctx); err != nil {
		fmt.Printf("open failed: %v\n", err)
		return
	}

	// 检查是否需要回退（活跃响应大小是否超过阈值）
	fmt.Println("Needs backout:", sc.NeedsBackout())

	// 关闭 Scraper（等待 in-flight Item 处理完毕）
	if err := sc.Close(ctx); err != nil {
		fmt.Printf("close failed: %v\n", err)
	}

	// Output:
	// Needs backout: false
}

// ExampleScraper_NeedsBackout 演示 Scraper 的回退机制。
//
// 当活跃响应的总大小超过 maxActiveSize 时，NeedsBackout 返回 true，
// Engine 会暂停从 Scheduler 取新请求，防止内存过载。
func ExampleScraper_NeedsBackout() {
	sp := &exampleSpider{Base: spider.Base{SpiderName: "example"}}
	signals := signal.NewManager(nil)
	statsCollector := stats.NewMemoryCollector(false, slog.Default())

	// 设置极小的 maxActiveSize 以便演示回退
	sc := scraper.NewScraper(nil, nil, sp, signals, statsCollector, slog.Default(), 1024, 10)

	// 初始状态：不需要回退
	fmt.Println("Initial:", sc.NeedsBackout())

	// Output:
	// Initial: false
}
