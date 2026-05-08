package scheduler_test

import (
	"context"
	"fmt"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
)

// ExampleNewDefaultScheduler 演示创建和使用默认调度器。
func ExampleNewDefaultScheduler() {
	// 创建默认调度器（纯内存模式）
	sched := scheduler.NewDefaultScheduler()

	// 打开调度器
	ctx := context.Background()
	if err := sched.Open(ctx); err != nil {
		fmt.Printf("open failed: %v\n", err)
		return
	}
	defer sched.Close(ctx, "finished")

	// 入队请求
	req1, _ := shttp.NewRequest("https://example.com/page/1")
	req2, _ := shttp.NewRequest("https://example.com/page/2", shttp.WithPriority(10))

	sched.EnqueueRequest(req1)
	sched.EnqueueRequest(req2)

	fmt.Println("Pending:", sched.Len())

	// 出队（高优先级先出）
	next := sched.NextRequest()
	fmt.Println("Next:", next.URL.Path)

	// Output:
	// Pending: 2
	// Next: /page/2
}

// ExampleNewDefaultScheduler_withDupeFilter 演示去重过滤功能。
func ExampleNewDefaultScheduler_withDupeFilter() {
	sched := scheduler.NewDefaultScheduler()
	ctx := context.Background()
	sched.Open(ctx)
	defer sched.Close(ctx, "finished")

	req1, _ := shttp.NewRequest("https://example.com/page/1")
	req2, _ := shttp.NewRequest("https://example.com/page/1") // 重复 URL

	ok1 := sched.EnqueueRequest(req1)
	ok2 := sched.EnqueueRequest(req2) // 被去重过滤

	fmt.Println("First enqueue:", ok1)
	fmt.Println("Second enqueue (filtered):", ok2)
	fmt.Println("Queue length:", sched.Len())

	// Output:
	// First enqueue: true
	// Second enqueue (filtered): false
	// Queue length: 1
}

// ExampleNewDefaultScheduler_withJobDir 演示断点续爬配置。
func ExampleNewDefaultScheduler_withJobDir() {
	// 创建支持断点续爬的调度器
	registry := shttp.NewCallbackRegistry()

	sched := scheduler.NewDefaultScheduler(
		scheduler.WithJobDir("/tmp/scrapy-go-job-1"),
		scheduler.WithCallbackRegistry(registry),
		scheduler.WithDebug(true),
	)

	fmt.Println("Has external queue:", sched.HasExternalQueue())
	// 注意：实际使用时需要调用 Open 初始化磁盘队列
	// Output:
	// Has external queue: false
}
