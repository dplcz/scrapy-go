package engine_test

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dplcz/scrapy-go/pkg/downloader"
	"github.com/dplcz/scrapy-go/pkg/engine"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
	"github.com/dplcz/scrapy-go/pkg/scraper"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
	smiddle "github.com/dplcz/scrapy-go/pkg/spider/middleware"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// exampleSpider 是一个用于演示的最小 Spider 实现。
type exampleSpider struct {
	spider.Base
}

func newExampleSpider() *exampleSpider {
	return &exampleSpider{
		Base: spider.Base{
			SpiderName: "example",
			StartURLs:  []string{"https://example.com"},
		},
	}
}

func (s *exampleSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	fmt.Printf("Parsed: %s\n", response.URL)
	return nil, nil
}

// ExampleNewEngine 演示如何手动创建和配置 Engine。
//
// 注意：通常不需要直接使用 Engine，而是通过 crawler.Crawler 自动组装。
// 此示例仅用于说明 Engine 的内部结构和依赖关系。
func ExampleNewEngine() {
	// 创建依赖组件
	sp := newExampleSpider()
	sched := scheduler.NewDefaultScheduler()
	s := settings.New()
	handler := downloader.NewHTTPDownloadHandler(30 * time.Second)
	signals := signal.NewManager(nil)
	statsCollector := stats.NewMemoryCollector(false, slog.Default())
	dl := downloader.NewDownloader(s, handler, signals, statsCollector, slog.Default())
	dlMW := downloader.NewMiddlewareManager(slog.Default())
	spiderMW := smiddle.NewManager(slog.Default())
	pipelines := pipeline.NewManager(signals, statsCollector, slog.Default())
	sc := scraper.NewScraper(spiderMW, pipelines, sp, signals, statsCollector, slog.Default(), 5000000, 100)

	// 创建 Engine
	eng := engine.NewEngine(sp, sched, dl, dlMW, sc, signals, statsCollector, slog.Default(), nil)

	// 配置优雅关闭超时
	eng.SetGracefulShutdownTimeout(10 * time.Second)

	// 启动引擎（阻塞直到完成）
	ctx := context.Background()
	err := eng.Start(ctx)
	if err != nil {
		fmt.Printf("engine error: %v\n", err)
	}
}

// ExampleEngine_Pause 演示 Engine 的暂停和恢复功能。
//
// 暂停期间 Engine 不会从 Scheduler 取新请求，但已在处理中的请求不受影响。
func ExampleEngine_Pause() {
	sp := newExampleSpider()
	sched := scheduler.NewDefaultScheduler()
	s := settings.New()
	handler := downloader.NewHTTPDownloadHandler(30 * time.Second)
	signals := signal.NewManager(nil)
	statsCollector := stats.NewMemoryCollector(false, slog.Default())
	dl := downloader.NewDownloader(s, handler, signals, statsCollector, slog.Default())
	sc := scraper.NewScraper(nil, nil, sp, signals, statsCollector, slog.Default(), 5000000, 100)

	eng := engine.NewEngine(sp, sched, dl, nil, sc, signals, statsCollector, slog.Default(), nil)

	// 在另一个 goroutine 中暂停/恢复
	go func() {
		time.Sleep(1 * time.Second)
		eng.Pause()
		fmt.Println("Engine paused:", eng.IsPaused())

		time.Sleep(2 * time.Second)
		eng.Unpause()
		fmt.Println("Engine resumed:", !eng.IsPaused())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = eng.Start(ctx)
}
