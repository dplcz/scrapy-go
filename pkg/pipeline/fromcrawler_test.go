package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// CrawlerAwarePipeline 测试
// ============================================================================

// mockCrawler 实现 pipeline.Crawler 接口，用于测试。
type mockCrawler struct {
	settings *settings.Settings
	stats    stats.Collector
	signals  *signal.Manager
	logger   *slog.Logger
}

func newMockCrawler() *mockCrawler {
	return &mockCrawler{
		settings: settings.New(),
		stats:    stats.NewMemoryCollector(false, nil),
		signals:  signal.NewManager(nil),
		logger:   slog.Default(),
	}
}

func (c *mockCrawler) GetSettings() *settings.Settings { return c.settings }
func (c *mockCrawler) GetStats() stats.Collector       { return c.stats }
func (c *mockCrawler) GetSignals() *signal.Manager      { return c.signals }
func (c *mockCrawler) GetLogger() *slog.Logger          { return c.logger }

// crawlerAwarePipeline 是一个实现了 CrawlerAwarePipeline 的测试 Pipeline。
type crawlerAwarePipeline struct {
	crawler       Crawler
	fromCrawlerOK bool
	opened        bool
	closed        bool
}

func (p *crawlerAwarePipeline) FromCrawler(c Crawler) error {
	p.crawler = c
	p.fromCrawlerOK = true
	return nil
}

func (p *crawlerAwarePipeline) Open(ctx context.Context) error {
	p.opened = true
	return nil
}

func (p *crawlerAwarePipeline) Close(ctx context.Context) error {
	p.closed = true
	return nil
}

func (p *crawlerAwarePipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	return item, nil
}

// TestCrawlerAwarePipeline_FromCrawlerCalled 验证实现了 CrawlerAwarePipeline 的
// Pipeline 在 Open 时会收到 Crawler 引用。
func TestCrawlerAwarePipeline_FromCrawlerCalled(t *testing.T) {
	m := NewManager(nil, nil, nil)
	mc := newMockCrawler()
	m.SetCrawler(mc)

	p := &crawlerAwarePipeline{}
	m.AddPipeline(p, "aware", 100)

	if err := m.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if !p.fromCrawlerOK {
		t.Error("FromCrawler should have been called")
	}
	if p.crawler != mc {
		t.Error("FromCrawler should receive the mock crawler")
	}
	if !p.opened {
		t.Error("Open should have been called after FromCrawler")
	}
}

// TestCrawlerAwarePipeline_AccessSettingsAndStats 验证 Pipeline 可以通过
// Crawler 引用访问 Settings 和 Stats。
func TestCrawlerAwarePipeline_AccessSettingsAndStats(t *testing.T) {
	m := NewManager(nil, nil, nil)
	mc := newMockCrawler()
	mc.settings.Set("MY_PIPELINE_KEY", "my_value", settings.PriorityProject)
	m.SetCrawler(mc)

	p := &statsPipeline{}
	m.AddPipeline(p, "stats_pipeline", 100)

	if err := m.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// 验证 Pipeline 通过 FromCrawler 获取到了正确的 Settings
	if p.settingValue != "my_value" {
		t.Errorf("expected setting value 'my_value', got %q", p.settingValue)
	}

	// 通过 Pipeline 处理 Item，验证 Stats 可用
	_, err := m.ProcessItem(context.Background(), map[string]any{"name": "test"}, nil)
	if err != nil {
		t.Fatalf("ProcessItem: %v", err)
	}

	// 验证 Pipeline 通过 Stats 记录了计数
	count := mc.stats.GetValue("my_pipeline/processed", 0)
	if count != 1 {
		t.Errorf("expected stats count 1, got %v", count)
	}
}

// statsPipeline 通过 FromCrawler 获取 Settings 和 Stats。
type statsPipeline struct {
	settingValue string
	stats        stats.Collector
}

func (p *statsPipeline) FromCrawler(c Crawler) error {
	p.settingValue = c.GetSettings().GetString("MY_PIPELINE_KEY", "")
	p.stats = c.GetStats()
	return nil
}

func (p *statsPipeline) Open(ctx context.Context) error  { return nil }
func (p *statsPipeline) Close(ctx context.Context) error { return nil }
func (p *statsPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	if p.stats != nil {
		p.stats.IncValue("my_pipeline/processed", 1, 0)
	}
	return item, nil
}

// TestNonCrawlerAwarePipeline_Unaffected 验证未实现 CrawlerAwarePipeline 的
// Pipeline 不受影响，行为与之前一致。
func TestNonCrawlerAwarePipeline_Unaffected(t *testing.T) {
	m := NewManager(nil, nil, nil)
	mc := newMockCrawler()
	m.SetCrawler(mc)

	p := &lifecyclePipeline{}
	m.AddPipeline(p, "normal", 100)

	if err := m.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if !p.opened {
		t.Error("normal pipeline should be opened")
	}
}

// TestCrawlerAwarePipeline_NoCrawlerSet 验证未设置 Crawler 时，
// CrawlerAwarePipeline 的 FromCrawler 不会被调用，但 Open 仍正常执行。
func TestCrawlerAwarePipeline_NoCrawlerSet(t *testing.T) {
	m := NewManager(nil, nil, nil)
	// 不调用 SetCrawler

	p := &crawlerAwarePipeline{}
	m.AddPipeline(p, "aware", 100)

	if err := m.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if p.fromCrawlerOK {
		t.Error("FromCrawler should NOT have been called when no crawler is set")
	}
	if !p.opened {
		t.Error("Open should still be called even without crawler")
	}
}

// TestCrawlerAwarePipeline_FromCrawlerError 验证 FromCrawler 返回错误时，
// Open 不会被调用，且错误被正确传播。
func TestCrawlerAwarePipeline_FromCrawlerError(t *testing.T) {
	m := NewManager(nil, nil, nil)
	mc := newMockCrawler()
	m.SetCrawler(mc)

	p := &errorFromCrawlerPipeline{}
	m.AddPipeline(p, "error_aware", 100)

	err := m.Open(context.Background())
	if err == nil {
		t.Fatal("expected error from FromCrawler")
	}
	if err.Error() != "from_crawler failed" {
		t.Errorf("unexpected error: %v", err)
	}
	if p.opened {
		t.Error("Open should NOT be called when FromCrawler fails")
	}
}

// errorFromCrawlerPipeline 的 FromCrawler 返回错误。
type errorFromCrawlerPipeline struct {
	opened bool
}

func (p *errorFromCrawlerPipeline) FromCrawler(c Crawler) error {
	return errors.New("from_crawler failed")
}

func (p *errorFromCrawlerPipeline) Open(ctx context.Context) error {
	p.opened = true
	return nil
}

func (p *errorFromCrawlerPipeline) Close(ctx context.Context) error { return nil }
func (p *errorFromCrawlerPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	return item, nil
}

// TestCrawlerAwarePipeline_MixedPipelines 验证混合使用 CrawlerAwarePipeline
// 和普通 Pipeline 时，执行顺序正确。
func TestCrawlerAwarePipeline_MixedPipelines(t *testing.T) {
	m := NewManager(nil, nil, nil)
	mc := newMockCrawler()
	m.SetCrawler(mc)

	aware := &crawlerAwarePipeline{}
	normal := &lifecyclePipeline{}

	m.AddPipeline(normal, "normal", 100)
	m.AddPipeline(aware, "aware", 200)

	if err := m.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if !normal.opened {
		t.Error("normal pipeline should be opened")
	}
	if !aware.fromCrawlerOK {
		t.Error("aware pipeline should have FromCrawler called")
	}
	if !aware.opened {
		t.Error("aware pipeline should be opened")
	}
}
