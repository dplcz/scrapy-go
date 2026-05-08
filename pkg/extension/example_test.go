package extension_test

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dplcz/scrapy-go/pkg/extension"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ExampleNewManager 演示创建和使用扩展管理器。
func ExampleNewManager() {
	// 创建扩展管理器
	mgr := extension.NewManager(slog.Default())

	// 创建依赖
	signals := signal.NewManager(nil)
	collector := stats.NewMemoryCollector(false, nil)

	// 添加内置扩展
	coreStats := extension.NewCoreStatsExtension(collector, signals, nil)
	mgr.AddExtension(coreStats, "CoreStats", 0)

	fmt.Println("Extension count:", mgr.Count())

	// 打开所有扩展
	ctx := context.Background()
	err := mgr.Open(ctx)
	fmt.Println("Open error:", err)

	// 模拟 Spider 生命周期信号
	signals.SendCatchLog(signal.SpiderOpened, nil)
	signals.SendCatchLog(signal.ItemScraped, map[string]any{"item": "test"})
	signals.SendCatchLog(signal.ItemScraped, map[string]any{"item": "test2"})
	signals.SendCatchLog(signal.SpiderClosed, map[string]any{"reason": "finished"})

	// 检查统计
	fmt.Println("Items scraped:", collector.GetValue("item_scraped_count", 0))

	// 关闭所有扩展
	mgr.Close(ctx)

	// Output:
	// Extension count: 1
	// Open error: <nil>
	// Items scraped: 2
}

// ExampleNewCloseSpiderExtension 演示 CloseSpider 扩展的条件关闭。
func ExampleNewCloseSpiderExtension() {
	signals := signal.NewManager(nil)
	collector := stats.NewMemoryCollector(false, nil)

	// 创建 CloseSpider 扩展：达到 5 个 Item 后关闭
	ext := extension.NewCloseSpiderExtension(
		0, // timeout（0=禁用）
		5, // itemCount
		0, // pageCount（0=禁用）
		0, // errorCount（0=禁用）
		signals,
		collector,
		slog.Default(),
	)

	ctx := context.Background()
	err := ext.Open(ctx)
	fmt.Println("Open error:", err)

	// 模拟 Item 抓取
	for i := 0; i < 5; i++ {
		signals.SendCatchLog(signal.ItemScraped, nil)
	}

	ext.Close(ctx)
	fmt.Println("Done")

	// Output:
	// Open error: <nil>
	// Done
}
