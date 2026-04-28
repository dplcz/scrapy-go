package scraper

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// TestConcurrentItemsBasic 验证多个 Item 可以并发处理。
func TestConcurrentItemsBasic(t *testing.T) {
	sp := &testSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	pm := pipeline.NewManager(nil, sc, nil)

	var processedCount atomic.Int32
	pm.AddPipeline(&trackingPipeline{processed: &processedCount}, "tracking", 100)

	// concurrentItems = 10
	s := NewScraper(nil, pm, sp, nil, sc, nil, 0, 10)
	s.Open(context.Background())
	defer s.Close(context.Background())

	// 产出 5 个 Item
	outputs := make([]spider.Output, 5)
	for i := 0; i < 5; i++ {
		outputs[i] = spider.Output{Item: map[string]any{"index": i}}
	}

	_, err := s.processOutputs(context.Background(), outputs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 等待所有 Item 处理完毕
	s.itemWg.Wait()

	if count := processedCount.Load(); count != 5 {
		t.Errorf("expected 5 items processed, got %d", count)
	}
}

// TestConcurrentItemsLimit 验证并发上限控制。
func TestConcurrentItemsLimit(t *testing.T) {
	sp := &testSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	pm := pipeline.NewManager(nil, sc, nil)

	var maxConcurrent atomic.Int32
	var currentConcurrent atomic.Int32

	pm.AddPipeline(&concurrencyTrackingPipeline{
		maxConcurrent:     &maxConcurrent,
		currentConcurrent: &currentConcurrent,
		delay:             50 * time.Millisecond,
	}, "tracking", 100)

	// concurrentItems = 3（限制并发为 3）
	s := NewScraper(nil, pm, sp, nil, sc, nil, 0, 3)
	s.Open(context.Background())
	defer s.Close(context.Background())

	// 产出 10 个 Item
	outputs := make([]spider.Output, 10)
	for i := 0; i < 10; i++ {
		outputs[i] = spider.Output{Item: map[string]any{"index": i}}
	}

	_, err := s.processOutputs(context.Background(), outputs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 等待所有 Item 处理完毕
	s.itemWg.Wait()

	// 最大并发不应超过 concurrentItems (3)
	if max := maxConcurrent.Load(); max > 3 {
		t.Errorf("max concurrent items should be <= 3, got %d", max)
	}
	// 应该有并发（max > 1）
	if max := maxConcurrent.Load(); max < 2 {
		t.Errorf("expected some concurrency (max > 1), got %d", max)
	}
}

// TestConcurrentItemsCloseWaitsForInflight 验证 Close 等待 in-flight Item。
func TestConcurrentItemsCloseWaitsForInflight(t *testing.T) {
	sp := &testSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	pm := pipeline.NewManager(nil, sc, nil)

	var processedCount atomic.Int32
	pm.AddPipeline(&slowPipeline{
		processed: &processedCount,
		delay:     100 * time.Millisecond,
	}, "slow", 100)

	s := NewScraper(nil, pm, sp, nil, sc, nil, 0, 10)
	s.Open(context.Background())

	// 产出 3 个 Item
	outputs := []spider.Output{
		{Item: map[string]any{"a": 1}},
		{Item: map[string]any{"b": 2}},
		{Item: map[string]any{"c": 3}},
	}

	s.processOutputs(context.Background(), outputs, nil)

	// Close 应等待所有 Item 处理完毕
	s.Close(context.Background())

	if count := processedCount.Load(); count != 3 {
		t.Errorf("expected 3 items processed after Close, got %d", count)
	}
}

// TestConcurrentItemsMixedOutputs 验证 Request 和 Item 混合输出。
func TestConcurrentItemsMixedOutputs(t *testing.T) {
	sp := &testSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	pm := pipeline.NewManager(nil, sc, nil)

	var processedCount atomic.Int32
	pm.AddPipeline(&trackingPipeline{processed: &processedCount}, "tracking", 100)

	s := NewScraper(nil, pm, sp, nil, sc, nil, 0, 10)
	s.Open(context.Background())
	defer s.Close(context.Background())

	outputs := []spider.Output{
		{Request: shttp.MustNewRequest("https://example.com/1")},
		{Item: map[string]any{"a": 1}},
		{Request: shttp.MustNewRequest("https://example.com/2")},
		{Item: map[string]any{"b": 2}},
		{Item: map[string]any{"c": 3}},
	}

	newReqs, err := s.processOutputs(context.Background(), outputs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 应返回 2 个 Request
	if len(newReqs) != 2 {
		t.Errorf("expected 2 requests, got %d", len(newReqs))
	}

	// 等待 Item 处理完毕
	s.itemWg.Wait()

	if count := processedCount.Load(); count != 3 {
		t.Errorf("expected 3 items processed, got %d", count)
	}
}

// TestConcurrentItemsPanicRecovery 验证 Pipeline panic 不会导致进程崩溃。
func TestConcurrentItemsPanicRecovery(t *testing.T) {
	sp := &testSpider{}
	sc := stats.NewMemoryCollector(false, nil)
	pm := pipeline.NewManager(nil, sc, nil)
	pm.AddPipeline(&panicPipeline{}, "panic", 100)

	s := NewScraper(nil, pm, sp, nil, sc, nil, 0, 10)
	s.Open(context.Background())
	defer s.Close(context.Background())

	outputs := []spider.Output{
		{Item: map[string]any{"trigger": "panic"}},
	}

	// 不应 panic
	s.processOutputs(context.Background(), outputs, nil)
	s.itemWg.Wait()

	// 验证 panic 被记录到统计
	panicCount := sc.GetValue("spider_exceptions/panic", 0)
	if panicCount != 1 {
		t.Errorf("expected spider_exceptions/panic=1, got %v", panicCount)
	}
}

// TestConcurrentItemsDefaultValue 验证默认并发数为 100。
func TestConcurrentItemsDefaultValue(t *testing.T) {
	sp := &testSpider{}
	s := NewScraper(nil, nil, sp, nil, nil, nil, 0, 0)
	if s.concurrentItems != 100 {
		t.Errorf("expected default concurrentItems=100, got %d", s.concurrentItems)
	}
	if cap(s.itemSem) != 100 {
		t.Errorf("expected itemSem capacity=100, got %d", cap(s.itemSem))
	}
}

// ============================================================================
// 测试辅助类型
// ============================================================================

// trackingPipeline 追踪处理的 Item 数量。
type trackingPipeline struct {
	processed *atomic.Int32
}

func (p *trackingPipeline) Open(ctx context.Context) error  { return nil }
func (p *trackingPipeline) Close(ctx context.Context) error { return nil }
func (p *trackingPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	p.processed.Add(1)
	return item, nil
}

// concurrencyTrackingPipeline 追踪最大并发数。
type concurrencyTrackingPipeline struct {
	maxConcurrent     *atomic.Int32
	currentConcurrent *atomic.Int32
	delay             time.Duration
	mu                sync.Mutex
}

func (p *concurrencyTrackingPipeline) Open(ctx context.Context) error  { return nil }
func (p *concurrencyTrackingPipeline) Close(ctx context.Context) error { return nil }
func (p *concurrencyTrackingPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	current := p.currentConcurrent.Add(1)
	defer p.currentConcurrent.Add(-1)

	// 更新最大并发数
	for {
		old := p.maxConcurrent.Load()
		if current <= old {
			break
		}
		if p.maxConcurrent.CompareAndSwap(old, current) {
			break
		}
	}

	// 模拟处理延迟
	time.Sleep(p.delay)
	return item, nil
}

// slowPipeline 模拟慢速处理。
type slowPipeline struct {
	processed *atomic.Int32
	delay     time.Duration
}

func (p *slowPipeline) Open(ctx context.Context) error  { return nil }
func (p *slowPipeline) Close(ctx context.Context) error { return nil }
func (p *slowPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	time.Sleep(p.delay)
	p.processed.Add(1)
	return item, nil
}

// panicPipeline 在处理时 panic。
type panicPipeline struct{}

func (p *panicPipeline) Open(ctx context.Context) error  { return nil }
func (p *panicPipeline) Close(ctx context.Context) error { return nil }
func (p *panicPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	panic("intentional panic in pipeline test")
}
