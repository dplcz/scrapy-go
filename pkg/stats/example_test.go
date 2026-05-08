package stats_test

import (
	"fmt"
	"log/slog"

	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ExampleNewMemoryCollector 演示创建和使用内存统计收集器。
func ExampleNewMemoryCollector() {
	// 创建统计收集器（dump=false 表示关闭时不输出统计）
	collector := stats.NewMemoryCollector(false, slog.Default())
	collector.Open()

	// 设置统计值
	collector.SetValue("start_time", "2026-05-08T10:00:00Z")

	// 递增计数器
	collector.IncValue("item_scraped_count", 1, 0)
	collector.IncValue("item_scraped_count", 1, 0)
	collector.IncValue("item_scraped_count", 1, 0)

	// 更新极值
	collector.MaxValue("response_time_max", 1.5)
	collector.MaxValue("response_time_max", 2.3)
	collector.MaxValue("response_time_max", 0.8) // 不会更新（小于当前值）

	// 获取统计值
	fmt.Println("items:", collector.GetValue("item_scraped_count", 0))
	fmt.Println("max_time:", collector.GetValue("response_time_max", 0.0))
	fmt.Println("missing:", collector.GetValue("nonexistent", "default"))

	collector.Close("finished")

	// Output:
	// items: 3
	// max_time: 2.3
	// missing: default
}

// ExampleNewDummyCollector 演示空操作统计收集器。
func ExampleNewDummyCollector() {
	// DummyCollector 不收集任何数据，用于禁用统计的场景
	collector := stats.NewDummyCollector()
	collector.Open()

	collector.IncValue("item_scraped_count", 100, 0)
	collector.SetValue("key", "value")

	// 所有 Get 操作返回默认值
	fmt.Println("items:", collector.GetValue("item_scraped_count", 0))
	fmt.Println("stats:", len(collector.GetStats()))

	collector.Close("finished")

	// Output:
	// items: 0
	// stats: 0
}
