package downloader

import (
	"context"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// BenchmarkSlotEnqueue 测试 Slot.Enqueue 的内存分配（并行场景）。
// 验证 downloadTask 对象池是否有效减少了每请求的内存分配。
//
// 优化前基线：5 allocs/op, ~374 B/op
// 优化后目标：2 allocs/op, ~158 B/op（-60% allocs, -58% B/op）
func BenchmarkSlotEnqueue(b *testing.B) {
	// 创建一个快速返回的 Slot
	slot := NewSlot(16, 0, false, func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return &shttp.Response{}, nil
	})
	defer slot.Close()

	req := shttp.MustNewRequest("https://example.com")

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := slot.Enqueue(context.Background(), req)
			if err != nil {
				b.Fatalf("unexpected error: %v", err)
			}
			_ = resp
		}
	})
}

// BenchmarkSlotEnqueue_Sequential 测试串行场景下 Slot.Enqueue 的内存分配。
// 串行场景下对象池复用率最高（同一时刻只有一个 task 在使用）。
//
// 优化前基线：5 allocs/op, 320 B/op
// 优化后目标：2 allocs/op, 152 B/op（-60% allocs, -52% B/op）
func BenchmarkSlotEnqueue_Sequential(b *testing.B) {
	slot := NewSlot(1, 0, false, func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return &shttp.Response{}, nil
	})
	defer slot.Close()

	req := shttp.MustNewRequest("https://example.com")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := slot.Enqueue(context.Background(), req)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = resp
	}
}

// BenchmarkDownloadTaskPool 直接测试 downloadTask 对象池的获取和归还性能。
// 验证对象池本身的开销极低（0 allocs/op）。
func BenchmarkDownloadTaskPool(b *testing.B) {
	req := shttp.MustNewRequest("https://example.com")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		task := getDownloadTask(ctx, req)
		putDownloadTask(task)
	}
}

// BenchmarkDownloadTaskPool_Parallel 测试并发场景下对象池的性能。
// 验证在高并发下对象池仍然能有效复用。
func BenchmarkDownloadTaskPool_Parallel(b *testing.B) {
	req := shttp.MustNewRequest("https://example.com")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			task := getDownloadTask(ctx, req)
			putDownloadTask(task)
		}
	})
}
