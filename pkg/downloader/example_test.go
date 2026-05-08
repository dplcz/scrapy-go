package downloader_test

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/dplcz/scrapy-go/pkg/downloader"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ExampleNewDownloader 演示创建下载器。
func ExampleNewDownloader() {
	// 创建配置
	s := settings.New()
	s.Set("CONCURRENT_REQUESTS", 16, settings.PriorityDefault)
	s.Set("CONCURRENT_REQUESTS_PER_DOMAIN", 8, settings.PriorityDefault)
	s.Set("DOWNLOAD_DELAY", "0s", settings.PriorityDefault)
	s.Set("DOWNLOAD_TIMEOUT", "30s", settings.PriorityDefault)

	// 创建下载器
	handler := downloader.NewHTTPDownloadHandler(30 * time.Second)
	signals := signal.NewManager(nil)
	statsCollector := stats.NewMemoryCollector(false, slog.Default())

	dl := downloader.NewDownloader(s, handler, signals, statsCollector, slog.Default())

	fmt.Println("Total concurrency:", dl.TotalConcurrency())
	fmt.Println("Active count:", dl.ActiveCount())
	fmt.Println("Needs backout:", dl.NeedsBackout())

	// Output:
	// Total concurrency: 16
	// Active count: 0
	// Needs backout: false
}

// ExampleNewHTTPDownloadHandler 演示创建 HTTP 下载处理器。
func ExampleNewHTTPDownloadHandler() {
	// 创建带 30 秒超时的 HTTP 处理器
	handler := downloader.NewHTTPDownloadHandler(30 * time.Second)

	// 处理器实现了 DownloadHandler 接口
	var _ downloader.DownloadHandler = handler

	fmt.Println("Handler created successfully")

	// 关闭处理器
	handler.Close()

	// Output:
	// Handler created successfully
}

// ExampleNewMiddlewareManager 演示创建中间件管理器。
func ExampleNewMiddlewareManager() {
	mgr := downloader.NewMiddlewareManager(slog.Default())

	fmt.Println("Middleware count:", mgr.Count())

	// Output:
	// Middleware count: 0
}
