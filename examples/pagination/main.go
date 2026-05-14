// 示例爬虫：分页爬取 + 详情页抓取。
//
// 本示例创建一个本地 HTTP 测试服务器，模拟分页列表：
//   - 每个列表页包含一个"下一页"链接和一个"详情页"链接
//   - 详情页仅包含一个随机生成的字符串
//   - 总页数可通过 totalPages 变量手动设置
//
// 运行方式：go run examples/pagination/main.go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/dplcz/scrapy-go/pkg/feedexport"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 配置：手动设置总页数
// ============================================================================

// totalPages 控制测试服务器生成的总页数，可手动修改。
const totalPages = 100

// ============================================================================
// 本地测试服务器
// ============================================================================

// randomString 生成一个随机的十六进制字符串。
func randomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// newPaginationServer 创建一个本地分页测试服务器。
// 列表页路径: /page/1, /page/2, ...
// 详情页路径: /detail/1, /detail/2, ...
func newPaginationServer(pages int) *httptest.Server {
	mux := http.NewServeMux()

	// 根路径重定向到第一页
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/page/1", http.StatusFound)
	})

	// 列表页：每页包含一个详情链接和一个下一页链接
	for i := 1; i <= pages; i++ {
		page := i // 捕获循环变量
		path := fmt.Sprintf("/page/%d", page)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")

			nextPageLink := ""
			if page < pages {
				nextPageLink = fmt.Sprintf(`<a class="next" href="/page/%d">下一页</a>`, page+1)
			}

			html := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>列表页 - 第 %d 页</title></head>
<body>
<h1>分页列表 - 第 %d / %d 页</h1>
<div class="content">
  <a class="detail" href="/detail/%d">查看详情 %d</a>
</div>
<div class="pagination">
  %s
</div>
</body></html>`, page, page, pages, page, page, nextPageLink)
			time.Sleep(150 * time.Millisecond)
			fmt.Fprint(w, html)
		})
	}

	// 详情页：每个详情页包含一个随机字符串
	for i := 1; i <= pages; i++ {
		page := i
		path := fmt.Sprintf("/detail/%d", page)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")

			randomStr := randomString(16) // 生成32字符的随机十六进制字符串

			html := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>详情页 %d</title></head>
<body>
<h1>详情页 %d</h1>
<div class="random-content">%s</div>
</body></html>`, page, page, randomStr)
			time.Sleep(300 * time.Millisecond)
			fmt.Fprint(w, html)
		})
	}

	return httptest.NewServer(mux)
}

// ============================================================================
// PaginationSpider 爬虫实现
// ============================================================================

// DetailItem 表示从详情页提取的数据。
type DetailItem struct {
	Page          int    `json:"page"`
	DetailURL     string `json:"detail_url"`
	RandomContent string `json:"random_content"`
}

// PaginationSpider 演示分页爬取 + 详情页跟踪。
type PaginationSpider struct {
	spider.Base
	mu    sync.Mutex
	items []DetailItem
}

// NewPaginationSpider 创建一个新的分页爬虫。
func NewPaginationSpider(baseURL string) *PaginationSpider {
	return &PaginationSpider{
		Base: spider.Base{
			SpiderName: "pagination",
			StartURLs:  []string{baseURL + "/page/1"},
		},
	}
}

// Parse 解析列表页，提取详情链接和下一页链接。
func (s *PaginationSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	var outputs []spider.Output

	// 提取详情页链接
	detailHref := response.XPath("//a[@class='detail']/@href").Get("")
	pageTemp := strings.Split(response.URL.String(), "/")
	page, _ := strconv.Atoi(pageTemp[len(pageTemp)-1])
	if detailHref != "" {
		detailURL, err := response.URLJoin(detailHref)
		if err == nil {
			req, _ := shttp.NewRequest(detailURL, shttp.WithCallback(s.ParseDetail))
			req.SetMeta("page", page)
			outputs = append(outputs, spider.Output{Request: req})
		}
	}

	// 提取下一页链接
	nextHref := response.CSSAttr("a.next", "href").Get("")
	if nextHref != "" {
		nextURL, err := response.URLJoin(nextHref)
		if err == nil {
			req, _ := shttp.NewRequest(nextURL)
			outputs = append(outputs, spider.Output{Request: req})
		}
	}

	return outputs, nil
}

// ParseDetail 解析详情页，提取随机字符串内容。
func (s *PaginationSpider) ParseDetail(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	randomContent := response.CSS("div.random-content::text").Get("")

	page, _ := response.GetMeta("page")
	item := DetailItem{
		Page:          page.(int),
		DetailURL:     response.URL.String(),
		RandomContent: randomContent,
	}

	s.mu.Lock()
	s.items = append(s.items, item)
	s.mu.Unlock()

	return []spider.Output{{Item: item}}, nil
}

// CustomSettings 返回 Spider 级别的配置。
func (s *PaginationSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(8),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("INFO"),
	}
}

// ============================================================================
// 主函数
// ============================================================================

func main() {
	// 1. 启动本地测试服务器
	site := newPaginationServer(totalPages)
	defer site.Close()
	fmt.Printf("🌐 本地测试服务器已启动: %s\n", site.URL)
	fmt.Printf("📄 总页数: %d\n\n", totalPages)

	// 2. 创建 Spider
	sp := NewPaginationSpider(site.URL)

	// 3. 创建 Crawler 并运行
	c := crawler.NewDefault()
	c.AddFeed(feedexport.FeedConfig{
		URI:       "./output.jsonl",
		Format:    feedexport.FormatJSONLines,
		Overwrite: true,
		Options: feedexport.ExporterOptions{
			Indent: 1,
		},
	})
	c.AddFeed(feedexport.FeedConfig{
		URI:       "./output.xml",
		Format:    feedexport.FormatXML,
		Overwrite: true,
		Options: feedexport.ExporterOptions{
			Indent: 4,
		},
	})
	c.AddFeed(feedexport.FeedConfig{
		URI:       "./output.csv",
		Format:    feedexport.FormatCSV,
		Overwrite: true,
		Options: feedexport.ExporterOptions{
			FieldsToExport: []string{"page", "random_content"},
		},
		Filter: func(item any) bool {
			temp, ok := item.(DetailItem)
			if !ok {
				return false
			}
			if temp.Page%2 == 0 {
				return true
			}
			return false
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("🚀 开始爬取...")
	fmt.Println("============================================================")

	start := time.Now()
	err := c.Run(ctx, sp)
	elapsed := time.Since(start)

	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("❌ 爬取错误: %v\n", err)
		os.Exit(1)
	}

	// 4. 输出结果
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Printf("✅ 爬取完成！共收集 %d 条详情数据，耗时 %v\n\n", len(sp.items), elapsed)
}
