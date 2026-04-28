// 示例爬虫：演示 scrapy-go 的 Feed Export 数据导出系统。
//
// 本示例同时以四种格式（JSON、JSON Lines、CSV、XML）导出相同数据，
// 覆盖 URI 模板、FieldsToExport、StoreEmpty、Filter 等常用选项。
//
// 运行方式：go run examples/feedexport/main.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	"github.com/dplcz/scrapy-go/pkg/feedexport"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 本地测试网站
// ============================================================================

func newBooksSite() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<div class="book"><h2 class="title">Go in Action</h2><span class="price">39.99</span><span class="stock">In stock</span></div>
<div class="book"><h2 class="title">The Go Programming Language</h2><span class="price">49.99</span><span class="stock">In stock</span></div>
<div class="book"><h2 class="title">Learning Go</h2><span class="price">42.00</span><span class="stock">Out of stock</span></div>
<div class="book"><h2 class="title">100 Go Mistakes</h2><span class="price">35.50</span><span class="stock">In stock</span></div>
</body></html>`)
	})
	return httptest.NewServer(mux)
}

// ============================================================================
// 爬虫实现
// ============================================================================

type booksSpider struct {
	spider.Base
}

func newBooksSpider(base string) *booksSpider {
	return &booksSpider{
		Base: spider.Base{
			SpiderName: "books",
			StartURLs:  []string{base + "/"},
		},
	}
}

func (s *booksSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	var out []spider.Output
	for _, b := range response.CSS("div.book") {
		item := map[string]any{
			"title":  b.CSS("h2.title::text").Get(""),
			"price":  b.CSS("span.price::text").Get(""),
			"stock":  b.CSS("span.stock::text").Get(""),
			"source": response.URL.String(),
		}
		out = append(out, spider.Output{Item: item})
	}
	return out, nil
}

// ============================================================================
// main
// ============================================================================

func main() {
	site := newBooksSite()
	defer site.Close()
	outDir := "temp"
	err := os.Mkdir(outDir, 0750)
	if err != nil {
		fmt.Printf("mkdir: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Output dir: %s\n\n", outDir)

	// 通用 Exporter 选项：指定字段顺序
	commonOpts := feedexport.DefaultExporterOptions()
	commonOpts.FieldsToExport = []string{"title", "price", "stock", "source"}

	c := crawler.NewDefault()

	// 1) JSON，URI 模板：输出到带 spider 名的文件
	c.AddFeed(feedexport.FeedConfig{
		URI:       filepath.Join(outDir, "%(name)s.json"),
		Format:    feedexport.FormatJSON,
		Overwrite: true,
		Options:   commonOpts,
	})

	// 2) JSON Lines：逐行追加
	c.AddFeed(feedexport.FeedConfig{
		URI:       filepath.Join(outDir, "books.jsonl"),
		Format:    feedexport.FormatJSONLines,
		Overwrite: true,
		Options:   commonOpts,
	})

	// 3) CSV：只保留在库条目（通过 Filter 过滤）
	c.AddFeed(feedexport.FeedConfig{
		URI:       filepath.Join(outDir, "books-in-stock.csv"),
		Format:    feedexport.FormatCSV,
		Overwrite: true,
		Options:   commonOpts,
		Filter: func(item any) bool {
			m, ok := item.(map[string]any)
			if !ok {
				return false
			}
			return m["stock"] == "In stock"
		},
	})

	// 4) XML：即使没有 Item 也创建文件（StoreEmpty）
	c.AddFeed(feedexport.FeedConfig{
		URI:        filepath.Join(outDir, "books.xml"),
		Format:     feedexport.FormatXML,
		Overwrite:  true,
		StoreEmpty: true,
		Options:    commonOpts,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("Starting crawl with multi-format export...")
	fmt.Println("============================================================")
	start := time.Now()

	if err := c.Run(ctx, newBooksSpider(site.URL)); err != nil &&
		err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("Crawl error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("============================================================")
	fmt.Printf("Crawl completed in %v\n\n", time.Since(start))
	fmt.Println("Generated files:")
	entries, _ := os.ReadDir(outDir)
	for _, e := range entries {
		info, _ := e.Info()
		fmt.Printf("  %s (%d bytes)\n", e.Name(), info.Size())
	}
}
