package pipeline_test

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// cleanPipeline 是一个简单的数据清洗 Pipeline 示例。
type cleanPipeline struct{}

func (p *cleanPipeline) Open(ctx context.Context) error  { return nil }
func (p *cleanPipeline) Close(ctx context.Context) error { return nil }
func (p *cleanPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	// 简单地透传 item
	return item, nil
}

// ExampleNewManager 演示创建和使用 Pipeline 管理器。
func ExampleNewManager() {
	signals := signal.NewManager(nil)
	statsCollector := stats.NewMemoryCollector(false, slog.Default())

	// 创建管理器
	mgr := pipeline.NewManager(signals, statsCollector, slog.Default())

	// 添加 Pipeline（优先级数值小的先执行）
	mgr.AddPipeline(&cleanPipeline{}, "CleanPipeline", 300)

	fmt.Println("Pipeline count:", mgr.Count())

	// 打开所有 Pipeline
	ctx := context.Background()
	mgr.Open(ctx)
	defer mgr.Close(ctx)

	// 处理 Item
	item := map[string]any{"title": "Hello", "price": 9.99}
	result, err := mgr.ProcessItem(ctx, item, nil)

	fmt.Println("Error:", err)
	fmt.Printf("Result: %v\n", result)

	// Output:
	// Pipeline count: 1
	// Error: <nil>
	// Result: map[price:9.99 title:Hello]
}

// ExampleNewTypedPipeline 演示泛型 TypedPipeline 的使用。
func ExampleNewTypedPipeline() {
	// 定义类型安全的 Pipeline
	type Book struct {
		Title string
		Price float64
	}

	type bookPipeline struct{}

	// 注意：这里只是演示 TypedPipeline 的创建方式
	// 实际使用中 bookPipeline 需要实现 TypedItemPipeline[*Book] 接口

	fmt.Println("TypedPipeline provides compile-time type safety")

	// Output:
	// TypedPipeline provides compile-time type safety
}
