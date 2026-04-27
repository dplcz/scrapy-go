// 示例爬虫：使用本地静态网站演示 scrapy-go 框架的完整爬取流程。
//
// 本示例创建一个本地 HTTP 服务器模拟 quotes 网站，
// 然后使用 scrapy-go 框架爬取所有引用数据。
// 使用 CSS 选择器（goquery）解析 HTML 内容。
//
// 运行方式：go run examples/quotes/main.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 本地测试网站
// ============================================================================

func newLocalQuotesSite() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Quotes to Scrape</title></head>
<body>
<h1>Quotes to Scrape</h1>
<div class="quote">
  <span class="text">"The world as we have created it is a process of our thinking."</span>
  <span class="author">Albert Einstein</span>
  <div class="tags"><a class="tag">change</a><a class="tag">thinking</a></div>
</div>
<div class="quote">
  <span class="text">"It is our choices that show what we truly are."</span>
  <span class="author">J.K. Rowling</span>
  <div class="tags"><a class="tag">abilities</a><a class="tag">choices</a></div>
</div>
<nav><ul class="pager"><li class="next"><a href="/page/2">Next →</a></li></ul></nav>
</body></html>`)
	})

	mux.HandleFunc("/page/2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Quotes to Scrape - Page 2</title></head>
<body>
<h1>Quotes to Scrape</h1>
<div class="quote">
  <span class="text">"There are only two ways to live your life."</span>
  <span class="author">Albert Einstein</span>
  <div class="tags"><a class="tag">inspirational</a><a class="tag">life</a></div>
</div>
<div class="quote">
  <span class="text">"The person, be it gentleman or lady, who has not pleasure in a good novel, must be intolerably stupid."</span>
  <span class="author">Jane Austen</span>
  <div class="tags"><a class="tag">books</a><a class="tag">humor</a></div>
</div>
<nav><ul class="pager"><li class="next"><a href="/page/3">Next →</a></li></ul></nav>
</body></html>`)
	})

	mux.HandleFunc("/page/3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Quotes to Scrape - Page 3</title></head>
<body>
<h1>Quotes to Scrape</h1>
<div class="quote">
  <span class="text">"A day without sunshine is like, you know, night."</span>
  <span class="author">Steve Martin</span>
  <div class="tags"><a class="tag">humor</a><a class="tag">obvious</a></div>
</div>
<div class="quote">
  <span class="text">"This life is what you make it."</span>
  <span class="author">Marilyn Monroe</span>
  <div class="tags"><a class="tag">life</a><a class="tag">love</a></div>
</div>
</body></html>`)
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// QuotesSpider 爬虫实现
// ============================================================================

// QuotesSpider 爬取本地 quotes 网站的引用数据。
// 使用 CSS 选择器解析 HTML 内容。
type QuotesSpider struct {
	spider.Base
	mu    sync.Mutex
	items []map[string]any
}

// NewQuotesSpider 创建一个新的 QuotesSpider。
func NewQuotesSpider(baseURL string) *QuotesSpider {
	return &QuotesSpider{
		Base: spider.Base{
			SpiderName: "quotes",
			StartURLs:  []string{baseURL + "/"},
		},
	}
}

// Parse 解析响应，使用 CSS 选择器提取引用数据和下一页链接。
func (s *QuotesSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	var outputs []spider.Output

	// 使用 CSS 选择器提取每条引用
	quotes := response.CSS("div.quote")
	for _, q := range quotes {
		text := q.CSS("span.text::text").Get("")
		author := q.XPath(`./span[contains(@class,"author")]/text()`).Get("")
		tags := q.CSS("a.tag::text").GetAll()

		item := map[string]any{
			"text":   text,
			"author": author,
			"tags":   tags,
			"url":    response.URL.String(),
		}
		s.mu.Lock()
		s.items = append(s.items, item)
		s.mu.Unlock()
		outputs = append(outputs, spider.Output{Item: item})
	}

	// 使用 CSS 选择器提取下一页链接
	nextURL := response.CSSAttr("li.next a", "href").Get("")
	if nextURL != "" {
		absURL, err := response.URLJoin(nextURL)
		if err == nil {
			req, _ := scrapy_http.NewRequest(absURL)
			outputs = append(outputs, spider.Output{Request: req})
		}
	}

	return outputs, nil
}

// CustomSettings 返回 Spider 级别的配置。
func (s *QuotesSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(2),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("DEBUG"),
	}
}

// ============================================================================
// 主函数
// ============================================================================

func main() {
	// 1. 启动本地测试网站
	site := newLocalQuotesSite()
	defer site.Close()
	fmt.Printf("Local test site started: %s\n\n", site.URL)

	// 2. 创建 Spider
	sp := NewQuotesSpider(site.URL)

	// 3. 创建 Crawler 并运行
	c := crawler.NewDefault()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("Starting crawl...")
	fmt.Println("============================================================")

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("Crawl error: %v\n", err)
		os.Exit(1)
	}

	// 4. 输出结果
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Printf("Crawl completed! Collected %d quotes:\n\n", len(sp.items))

	for i, item := range sp.items {
		fmt.Printf("[%d] %s\n", i+1, item["text"])
		fmt.Printf("    — %s\n", item["author"])
		if tags, ok := item["tags"].([]string); ok && len(tags) > 0 {
		fmt.Printf("    Tags: %v\n", tags)
		}
		fmt.Printf("    Source: %s\n\n", item["url"])
	}
}
