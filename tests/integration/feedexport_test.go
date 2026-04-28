// Package integration 提供 Feed Export 系统的端到端集成测试。
//
// 这些测试验证 Feed Export 在完整爬取流程中与 Crawler、Engine、Pipeline、
// Extension 等组件协同工作，覆盖：
//   - 通过 Crawler.AddFeed 注入配置并导出 JSON/JSON Lines/CSV/XML
//   - 多目标并行导出
//   - URI 模板占位符（%(name)s、%(time)s）
//   - 与 settings.FEEDS 的双向兼容
package integration

import (
	"context"
	"encoding/json"
	"encoding/xml"
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
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 辅助：生成条目的简单 Spider
// ============================================================================

type feedTestSpider struct {
	spider.Base
	startURL string
	count    int
}

func (s *feedTestSpider) Name() string { return "feedtest" }

func (s *feedTestSpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output, 1)
	go func() {
		defer close(ch)
		req, _ := shttp.NewRequest(s.startURL)
		ch <- spider.Output{Request: req}
	}()
	return ch
}

func (s *feedTestSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	outs := make([]spider.Output, 0, s.count)
	for i := 0; i < s.count; i++ {
		outs = append(outs, spider.Output{
			Item: map[string]any{
				"id":    i,
				"name":  fmt.Sprintf("name-%d", i),
				"score": (i + 1) * 10,
			},
		})
	}
	return outs, nil
}

func newFeedTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body>ok</body></html>`)
	})
	return httptest.NewServer(mux)
}

// ============================================================================
// 测试 1：通过 AddFeed 导出 JSON
// ============================================================================

func TestFeedExport_E2E_AddFeedJSON(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	c := crawler.NewDefault()
	c.AddFeed(feedexport.FeedConfig{
		URI:       path,
		Format:    feedexport.FormatJSON,
		Overwrite: true,
		Options:   feedexport.DefaultExporterOptions(),
	})

	sp := &feedTestSpider{startURL: srv.URL, count: 3}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	if len(items) != 3 {
		t.Errorf("want 3 items, got %d: %s", len(items), data)
	}
}

// ============================================================================
// 测试 2：通过 AddFeed 导出 JSON Lines
// ============================================================================

func TestFeedExport_E2E_JSONLines(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "out.jsonl")

	c := crawler.NewDefault()
	c.AddFeed(feedexport.FeedConfig{
		URI:       path,
		Format:    feedexport.FormatJSONLines,
		Overwrite: true,
	})

	sp := &feedTestSpider{startURL: srv.URL, count: 5}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 5 {
		t.Errorf("want 5 jsonlines, got %d:\n%s", len(lines), data)
	}
	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d invalid: %v", i, err)
		}
	}
}

// ============================================================================
// 测试 3：CSV 导出 + 字段指定
// ============================================================================

func TestFeedExport_E2E_CSV(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	opts := feedexport.DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "score"}

	c := crawler.NewDefault()
	c.AddFeed(feedexport.FeedConfig{
		URI:       path,
		Format:    feedexport.FormatCSV,
		Overwrite: true,
		Options:   opts,
	})

	sp := &feedTestSpider{startURL: srv.URL, count: 2}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "name,score\n") {
		t.Errorf("csv header missing or wrong: %q", content)
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("want header + 2 rows, got %d:\n%s", len(lines), content)
	}
}

// ============================================================================
// 测试 4：XML 导出
// ============================================================================

func TestFeedExport_E2E_XML(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "out.xml")

	c := crawler.NewDefault()
	c.AddFeed(feedexport.FeedConfig{
		URI:       path,
		Format:    feedexport.FormatXML,
		Overwrite: true,
		Options:   feedexport.DefaultExporterOptions(),
	})

	sp := &feedTestSpider{startURL: srv.URL, count: 2}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// 验证 XML 可解析
	type xmlItem struct {
		XMLName xml.Name `xml:"item"`
		Items   []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	}
	type xmlRoot struct {
		XMLName xml.Name  `xml:"items"`
		Items   []xmlItem `xml:"item"`
	}
	var parsed xmlRoot
	if err := xml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, data)
	}
	if len(parsed.Items) != 2 {
		t.Errorf("want 2 items in XML, got %d:\n%s", len(parsed.Items), data)
	}
}

// ============================================================================
// 测试 5：多 Feed 并行导出
// ============================================================================

func TestFeedExport_E2E_MultipleFeeds(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "out.json")
	csvPath := filepath.Join(dir, "out.csv")

	opts := feedexport.DefaultExporterOptions()
	opts.FieldsToExport = []string{"id", "name"}

	c := crawler.NewDefault()
	c.AddFeed(feedexport.FeedConfig{URI: jsonPath, Format: feedexport.FormatJSON, Overwrite: true, Options: opts})
	c.AddFeed(feedexport.FeedConfig{URI: csvPath, Format: feedexport.FormatCSV, Overwrite: true, Options: opts})

	sp := &feedTestSpider{startURL: srv.URL, count: 4}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	// JSON 验证
	jsonData, _ := os.ReadFile(jsonPath)
	var jsonItems []map[string]any
	if err := json.Unmarshal(jsonData, &jsonItems); err != nil {
		t.Errorf("json invalid: %v\n%s", err, jsonData)
	}
	if len(jsonItems) != 4 {
		t.Errorf("json want 4, got %d", len(jsonItems))
	}

	// CSV 验证
	csvData, _ := os.ReadFile(csvPath)
	lines := strings.Split(strings.TrimRight(string(csvData), "\n"), "\n")
	if len(lines) != 5 {
		t.Errorf("csv want 5 lines (header+4), got %d:\n%s", len(lines), csvData)
	}
}

// ============================================================================
// 测试 6：URI 模板占位符渲染
// ============================================================================

func TestFeedExport_E2E_URITemplate(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	template := filepath.Join(dir, "out-%(name)s.json")

	c := crawler.NewDefault()
	c.AddFeed(feedexport.FeedConfig{
		URI:       template,
		Format:    feedexport.FormatJSON,
		Overwrite: true,
	})

	sp := &feedTestSpider{startURL: srv.URL, count: 1}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	expected := filepath.Join(dir, "out-feedtest.json")
	if _, err := os.Stat(expected); err != nil {
		// 列出目录以辅助调试
		entries, _ := os.ReadDir(dir)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected file %s; actual files: %v", expected, names)
	}
}

// ============================================================================
// 测试 7：通过 Settings.FEEDS 配置
// ============================================================================

func TestFeedExport_E2E_SettingsFEEDS(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "settings-out.jsonl")

	s := settings.New()
	_ = s.Set("FEEDS", map[string]map[string]any{
		path: {
			"format":    "jsonlines",
			"overwrite": true,
		},
	}, settings.PriorityProject)

	c := crawler.New(crawler.WithSettings(s))

	sp := &feedTestSpider{startURL: srv.URL, count: 3}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("want 3 lines, got %d:\n%s", len(lines), data)
	}
}

// ============================================================================
// 测试 8：空爬取 + StoreEmpty=true 创建空文件
// ============================================================================

func TestFeedExport_E2E_StoreEmpty(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")

	c := crawler.NewDefault()
	c.AddFeed(feedexport.FeedConfig{
		URI:        path,
		Format:     feedexport.FormatJSON,
		Overwrite:  true,
		StoreEmpty: true,
	})

	sp := &feedTestSpider{startURL: srv.URL, count: 0}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.TrimSpace(string(data)) != "[]" {
		t.Errorf("StoreEmpty: want '[]', got %q", data)
	}
}

// ============================================================================
// 测试 9：Filter 过滤
// ============================================================================

func TestFeedExport_E2E_Filter(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "filtered.jsonl")

	c := crawler.NewDefault()
	c.AddFeed(feedexport.FeedConfig{
		URI:       path,
		Format:    feedexport.FormatJSONLines,
		Overwrite: true,
		Filter: func(item any) bool {
			m, ok := item.(map[string]any)
			if !ok {
				return false
			}
			id, _ := m["id"].(int)
			return id%2 == 0 // 仅保留偶数
		},
	})

	sp := &feedTestSpider{startURL: srv.URL, count: 6}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Crawl(ctx, sp); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("want 3 lines (even ids), got %d:\n%s", len(lines), data)
	}
}

// ============================================================================
// 测试 10：并发压力（验证 FeedSlot 的线程安全）
// ============================================================================

func TestFeedExport_E2E_ConcurrentCrawlers(t *testing.T) {
	srv := newFeedTestServer(t)
	defer srv.Close()

	dir := t.TempDir()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			path := filepath.Join(dir, fmt.Sprintf("out-%d.jsonl", idx))
			c := crawler.NewDefault()
			c.AddFeed(feedexport.FeedConfig{
				URI:       path,
				Format:    feedexport.FormatJSONLines,
				Overwrite: true,
			})

			sp := &feedTestSpider{startURL: srv.URL, count: 10}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.Crawl(ctx, sp); err != nil {
				t.Errorf("crawler[%d] Crawl: %v", idx, err)
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("crawler[%d] ReadFile: %v", idx, err)
				return
			}
			lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			if len(lines) != 10 {
				t.Errorf("crawler[%d] want 10 items, got %d", idx, len(lines))
			}
		}(i)
	}
	wg.Wait()
}
