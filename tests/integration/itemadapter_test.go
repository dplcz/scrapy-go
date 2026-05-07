// Package integration 的 itemadapter_test 验证 ItemAdapter 体系在完整爬取
// 流程中的端到端行为：
//
//   - struct Item（带 item/json tag）能被 Feed Export 正确导出
//   - 自实现 item.ItemAdapter 接口的 Item 同样能被处理
//   - Pipeline 可以通过 ItemAdapter 读取异构 Item 的字段
//
// 对应 Phase 2 验收标准 P2-009 中：
//
//	"ItemAdapter + Feed Export | 集成测试 | Feed Export 通过 ItemAdapter 统一导出 struct 和 map 类型的 Item"
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	"github.com/dplcz/scrapy-go/pkg/feedexport"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/item"
	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// struct 类型 Item
// ============================================================================

// adapterBook 是 struct 型 Item，使用 `item` tag 声明字段名。
type adapterBook struct {
	Title  string `item:"title"`
	Price  int    `item:"price"`
	Author string `json:"author"` // json tag 作为回退
	Ignore string `item:"-"`      // 显式忽略
}

// adapterSpider 会产出混合类型的 Item：struct / map / 自定义 ItemAdapter。
type adapterSpider struct {
	spider.Base
	startURL string
}

func (s *adapterSpider) Name() string { return "adapterspider" }

func (s *adapterSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output, 1)
	go func() {
		defer close(ch)
		req, _ := shttp.NewRequest(s.startURL)
		ch <- spider.Output{Request: req}
	}()
	return ch
}

func (s *adapterSpider) Parse(_ context.Context, _ *shttp.Response) ([]spider.Output, error) {
	return []spider.Output{
		{Item: &adapterBook{Title: "Go", Price: 30, Author: "Alice", Ignore: "x"}},
		{Item: map[string]any{"title": "Python", "price": 25, "author": "Bob"}},
		{Item: &customAdapterItem{data: map[string]any{"title": "Rust", "price": 35, "author": "Carol"}}},
	}, nil
}

// customAdapterItem 自实现 item.ItemAdapter 接口。
type customAdapterItem struct {
	data map[string]any
}

func (c *customAdapterItem) Item() any { return c }
func (c *customAdapterItem) FieldNames() []string {
	return []string{"title", "price", "author"} // 固定顺序
}
func (c *customAdapterItem) GetField(name string) (any, bool) { v, ok := c.data[name]; return v, ok }
func (c *customAdapterItem) SetField(name string, v any) error {
	if c.data == nil {
		c.data = map[string]any{}
	}
	c.data[name] = v
	return nil
}
func (c *customAdapterItem) HasField(name string) bool { _, ok := c.data[name]; return ok }
func (c *customAdapterItem) AsMap() map[string]any {
	out := make(map[string]any, len(c.data))
	for k, v := range c.data {
		out[k] = v
	}
	return out
}
func (c *customAdapterItem) Len() int                        { return len(c.data) }
func (c *customAdapterItem) FieldMeta(string) item.FieldMeta { return nil }

// ============================================================================
// TestFeedExport_ItemAdapter_MixedTypes
// ============================================================================

func TestFeedExport_ItemAdapter_MixedTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "mixed.jsonl")
	csvPath := filepath.Join(dir, "mixed.csv")

	sp := &adapterSpider{startURL: server.URL}
	c := crawler.New()

	// JSON Lines：struct + map + 自定义 ItemAdapter 共存
	c.AddFeed(feedexport.FeedConfig{
		URI:    "file://" + jsonlPath,
		Format: feedexport.FormatJSONLines,
	})
	// CSV：固定字段顺序，验证跨类型字段对齐
	c.AddFeed(feedexport.FeedConfig{
		URI:    "file://" + csvPath,
		Format: feedexport.FormatCSV,
		Options: feedexport.ExporterOptions{
			FieldsToExport:     []string{"title", "price", "author"},
			IncludeHeadersLine: true,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("crawl: %v", err)
	}

	// ---- 验证 JSON Lines 输出 ----
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	lines := splitLines(string(data))
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), data)
	}
	titles := map[string]bool{}
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("json parse %q: %v", line, err)
		}
		if t1, ok := m["title"].(string); ok {
			titles[t1] = true
		}
		// struct 的 Ignore 字段应缺失；author 字段应存在
		if _, ok := m["Ignore"]; ok {
			t.Errorf("Ignore field should be excluded: %v", m)
		}
		if _, ok := m["author"]; !ok {
			t.Errorf("author missing: %v", m)
		}
	}
	for _, want := range []string{"Go", "Python", "Rust"} {
		if !titles[want] {
			t.Errorf("missing title %q in: %v", want, titles)
		}
	}

	// ---- 验证 CSV 输出 ----
	csvData, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	csvLines := splitLines(string(csvData))
	// 首行是 header，后 3 行是数据
	if len(csvLines) != 4 {
		t.Fatalf("expected 4 csv lines (header + 3), got %d:\n%s", len(csvLines), csvData)
	}
	if csvLines[0] != "title,price,author" {
		t.Fatalf("csv header = %q", csvLines[0])
	}
	// 验证每行包含对应标题与价格
	expectedRows := map[string]string{
		"Go":     "Go,30,Alice",
		"Python": "Python,25,Bob",
		"Rust":   "Rust,35,Carol",
	}
	seen := map[string]bool{}
	for _, row := range csvLines[1:] {
		for name, want := range expectedRows {
			if row == want {
				seen[name] = true
			}
		}
	}
	for name := range expectedRows {
		if !seen[name] {
			t.Errorf("missing csv row for %s", name)
		}
	}
}

// ============================================================================
// TestItemAdapter_Pipeline — Pipeline 通过 ItemAdapter 统一处理异构 Item
// ============================================================================

// uppercaseTitlePipeline 演示 Pipeline 通过 ItemAdapter 修改 Item 字段，
// 无需关心 Item 是 struct、map 还是自定义类型。
type uppercaseTitlePipeline struct {
	mu    sync.Mutex
	seen  []string
	dropN int
}

func (p *uppercaseTitlePipeline) Open(context.Context) error  { return nil }
func (p *uppercaseTitlePipeline) Close(context.Context) error { return nil }
func (p *uppercaseTitlePipeline) ProcessItem(_ context.Context, it any) (any, error) {
	a := item.Adapt(it)
	if a == nil {
		return it, nil // 无法适配，原样返回
	}
	title, _ := a.GetField("title")
	if s, ok := title.(string); ok {
		upper := ""
		for _, r := range s {
			if r >= 'a' && r <= 'z' {
				r -= 32
			}
			upper += string(r)
		}
		if err := a.SetField("title", upper); err == nil {
			p.mu.Lock()
			p.seen = append(p.seen, upper)
			p.mu.Unlock()
		}
	}
	return a.Item(), nil
}

// 确保 uppercaseTitlePipeline 实现 ItemPipeline 接口。
var _ pipeline.ItemPipeline = (*uppercaseTitlePipeline)(nil)

func TestItemAdapter_Pipeline_ProcessHeterogeneousItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	sp := &adapterSpider{startURL: server.URL}
	c := crawler.New()

	p := &uppercaseTitlePipeline{}
	c.AddPipeline(p, "uppercase", 100)

	// 配合一个 Feed 输出，便于观察 Pipeline 修改是否传递到 exporter
	dir := t.TempDir()
	outPath := filepath.Join(dir, "pipelined.jsonl")
	c.AddFeed(feedexport.FeedConfig{
		URI:    "file://" + outPath,
		Format: feedexport.FormatJSONLines,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("crawl: %v", err)
	}

	// Pipeline 应处理全部 3 个 Item（struct / map / 自定义 adapter）
	p.mu.Lock()
	seen := append([]string(nil), p.seen...)
	p.mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("pipeline processed %d items: %v", len(seen), seen)
	}
	for _, w := range []string{"GO", "PYTHON", "RUST"} {
		found := false
		for _, s := range seen {
			if s == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in pipeline output: %v", w, seen)
		}
	}

	// 导出文件应反映 Pipeline 修改后的 title
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read feed: %v", err)
	}
	for _, w := range []string{"GO", "PYTHON", "RUST"} {
		if !strings.Contains(string(data), fmt.Sprintf("%q", w)) {
			t.Errorf("exported feed should contain %q, got:\n%s", w, data)
		}
	}
}

// ============================================================================
// 辅助
// ============================================================================

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
