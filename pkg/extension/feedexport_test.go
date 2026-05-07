package extension

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	"github.com/dplcz/scrapy-go/pkg/feedexport"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// FeedExport 扩展测试
// ============================================================================

func TestFeedExportExtension_NotConfigured(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewFeedExportExtension(nil, sm, sc, nil)

	err := ext.Open(context.Background())
	if !errors.Is(err, serrors.ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestFeedExportExtension_InvalidConfig(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)

	// 未知 format
	ext := NewFeedExportExtension([]feedexport.FeedConfig{
		{URI: "x.xyz", Format: "bad"},
	}, sm, sc, nil)
	if err := ext.Open(context.Background()); err == nil {
		t.Error("expected error for unknown format")
	}

	// 空 URI
	ext = NewFeedExportExtension([]feedexport.FeedConfig{
		{URI: "", Format: feedexport.FormatJSON},
	}, sm, sc, nil)
	if err := ext.Open(context.Background()); err == nil {
		t.Error("expected error for empty URI")
	}
}

func TestFeedExportExtension_EndToEnd_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewFeedExportExtension([]feedexport.FeedConfig{
		{
			URI:       path,
			Format:    feedexport.FormatJSON,
			Overwrite: true,
			Options:   feedexport.DefaultExporterOptions(),
		},
	}, sm, sc, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer ext.Close(context.Background())

	sm.SendCatchLog(signal.SpiderOpened, map[string]any{"spider": nil})
	sm.SendCatchLog(signal.ItemScraped, map[string]any{
		"item": map[string]any{"a": 1},
	})
	sm.SendCatchLog(signal.ItemScraped, map[string]any{
		"item": map[string]any{"a": 2},
	})
	sm.SendCatchLog(signal.SpiderClosed, map[string]any{"reason": "finished"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	if len(items) != 2 {
		t.Errorf("want 2 items, got %d: %s", len(items), data)
	}

	// 统计：success_count/<uri> 应为 1
	success := sc.GetValue("feedexport/success_count/"+path, 0)
	if success != 1 {
		t.Errorf("success_count: want 1, got %v", success)
	}
}

func TestFeedExportExtension_MultipleFeeds(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "out.json")
	csvPath := filepath.Join(dir, "out.csv")

	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	opts := feedexport.DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "age"}
	ext := NewFeedExportExtension([]feedexport.FeedConfig{
		{URI: jsonPath, Format: feedexport.FormatJSON, Overwrite: true, Options: opts},
		{URI: csvPath, Format: feedexport.FormatCSV, Overwrite: true, Options: opts},
	}, sm, sc, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer ext.Close(context.Background())

	sm.SendCatchLog(signal.SpiderOpened, map[string]any{"spider": nil})
	sm.SendCatchLog(signal.ItemScraped, map[string]any{
		"item": map[string]any{"name": "Alice", "age": 30},
	})
	sm.SendCatchLog(signal.SpiderClosed, map[string]any{"reason": "finished"})

	if _, err := os.Stat(jsonPath); err != nil {
		t.Errorf("json file missing: %v", err)
	}

	csvData, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if !strings.Contains(string(csvData), "name,age") || !strings.Contains(string(csvData), "Alice,30") {
		t.Errorf("unexpected csv output: %s", csvData)
	}
}

func TestFeedExportExtension_Filter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.jsonl")

	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewFeedExportExtension([]feedexport.FeedConfig{
		{
			URI:       path,
			Format:    feedexport.FormatJSONLines,
			Overwrite: true,
			Options:   feedexport.DefaultExporterOptions(),
			Filter: func(item any) bool {
				m, ok := item.(map[string]any)
				if !ok {
					return false
				}
				return m["keep"] == true
			},
		},
	}, sm, sc, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer ext.Close(context.Background())

	sm.SendCatchLog(signal.SpiderOpened, nil)
	sm.SendCatchLog(signal.ItemScraped, map[string]any{
		"item": map[string]any{"keep": true, "id": 1},
	})
	sm.SendCatchLog(signal.ItemScraped, map[string]any{
		"item": map[string]any{"keep": false, "id": 2},
	})
	sm.SendCatchLog(signal.SpiderClosed, nil)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if strings.Contains(content, `"id":2`) {
		t.Errorf("filtered item leaked into output: %s", content)
	}
	if !strings.Contains(content, `"id":1`) {
		t.Errorf("wanted item missing from output: %s", content)
	}
}

func TestFeedExportExtension_CloseSignalsDetached(t *testing.T) {
	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewFeedExportExtension([]feedexport.FeedConfig{
		{URI: filepath.Join(t.TempDir(), "x.jsonl"), Format: feedexport.FormatJSONLines},
	}, sm, sc, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if sm.HandlerCount(signal.ItemScraped) == 0 {
		t.Error("expected ItemScraped handler registered")
	}
	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if sm.HandlerCount(signal.ItemScraped) != 0 {
		t.Error("ItemScraped handler should be disconnected after Close")
	}
	if sm.HandlerCount(signal.SpiderOpened) != 0 {
		t.Error("SpiderOpened handler should be disconnected after Close")
	}
	if sm.HandlerCount(signal.SpiderClosed) != 0 {
		t.Error("SpiderClosed handler should be disconnected after Close")
	}
}

// ============================================================================
// 额外测试：FeedExport 扩展错误路径
// ============================================================================

// TestFeedExportExtension_StoreEmptyTriggersImmediateStart 验证当
// StoreEmpty=true 时，onSpiderOpened 会立即 Start slot（即使没 Item 也创建文件）。
func TestFeedExportExtension_StoreEmptyTriggersImmediateStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store_empty.json")

	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewFeedExportExtension([]feedexport.FeedConfig{
		{
			URI:        path,
			Format:     feedexport.FormatJSON,
			Overwrite:  true,
			StoreEmpty: true,
			Options:    feedexport.DefaultExporterOptions(),
		},
	}, sm, sc, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	sm.SendCatchLog(signal.SpiderOpened, nil)
	// 不发送任何 ItemScraped
	sm.SendCatchLog(signal.SpiderClosed, map[string]any{"reason": "finished"})

	// 文件应该被创建
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.TrimSpace(string(data)) != "[]" {
		t.Errorf("StoreEmpty expected empty JSON array, got %q", data)
	}
	_ = ext.Close(context.Background())
}

// TestFeedExportExtension_CloseBeforeSignals 验证在未收到 SpiderClosed
// 的情况下直接 Close 扩展，defensive 清理能执行，没有泄漏。
func TestFeedExportExtension_CloseBeforeSpiderClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "defensive.jsonl")

	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewFeedExportExtension([]feedexport.FeedConfig{
		{URI: path, Format: feedexport.FormatJSONLines, Overwrite: true, Options: feedexport.DefaultExporterOptions()},
	}, sm, sc, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	sm.SendCatchLog(signal.SpiderOpened, nil)
	sm.SendCatchLog(signal.ItemScraped, map[string]any{"item": map[string]any{"x": 1}})

	// 直接 Close（模拟异常流）
	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 正常写入（由 defensive 清理保证）
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"x":1`) {
		t.Errorf("expected item to be flushed on defensive close, got: %s", data)
	}
}

// TestFeedExportExtension_ItemScrapedWithNilItem 验证当 Signal 参数中
// item 为 nil 时扩展不会 panic。
func TestFeedExportExtension_ItemScrapedWithNilItem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nil.jsonl")

	sm := signal.NewManager(nil)
	sc := stats.NewMemoryCollector(false, nil)
	ext := NewFeedExportExtension([]feedexport.FeedConfig{
		{URI: path, Format: feedexport.FormatJSONLines, Overwrite: true},
	}, sm, sc, nil)
	_ = ext.Open(context.Background())
	defer ext.Close(context.Background())

	sm.SendCatchLog(signal.SpiderOpened, nil)
	sm.SendCatchLog(signal.ItemScraped, map[string]any{"item": nil})
	sm.SendCatchLog(signal.ItemScraped, nil) // params 也是 nil
	sm.SendCatchLog(signal.SpiderClosed, nil)
}

// ============================================================================
// 补充：LogStats / CloseSpider 信号触发测试（覆盖率提升）
// ============================================================================

func TestLogStatsExtension_SignalHandling(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)
	// 使用较短间隔以确保 logStats 至少被触发一次
	ext := NewLogStatsExtension(0.05, sc, sm, nil)

	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// 发送 SpiderOpened 触发 onSpiderOpened -> logLoop
	sm.SendCatchLog(signal.SpiderOpened, map[string]any{"spider": nil})

	// 等待 logLoop 至少执行一次 ticker
	time.Sleep(150 * time.Millisecond)

	// 设置最终 stats 以让 calculateFinalStats 能正常计算
	now := time.Now()
	sc.SetValue("start_time", now.Add(-1*time.Minute))
	sc.SetValue("finish_time", now)
	sc.SetValue("elapsed_time_seconds", 60.0)
	sc.SetValue("response_received_count", 30)
	sc.SetValue("item_scraped_count", 10)

	// 发送 SpiderClosed 触发 onSpiderClosed -> calculateFinalStats
	sm.SendCatchLog(signal.SpiderClosed, map[string]any{"reason": "finished"})

	if err := ext.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 确认最终速率被写入
	if sc.GetValue("responses_per_minute", nil) == nil {
		t.Error("expected responses_per_minute computed by onSpiderClosed")
	}
}

func TestCloseSpiderExtension_SpiderOpenedAndClosed(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	sm := signal.NewManager(nil)

	// 启用按 timeout 关闭（timeout>0，会启动 timer）
	ext := NewCloseSpiderExtension(0.5, 0, 0, 0, sm, sc, nil)
	if err := ext.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// SpiderOpened: 启动 timer
	sm.SendCatchLog(signal.SpiderOpened, map[string]any{"spider": nil})

	// 稍等片刻，让 onSpiderClosed 能清理 timer（不等 timeout 触发）
	sm.SendCatchLog(signal.SpiderClosed, map[string]any{"reason": "finished"})

	_ = ext.Close(context.Background())
}
