// 示例爬虫：使用本地静态网站演示 scrapy-go 框架的完整爬取流程。
//
// 本示例创建一个本地 HTTP 服务器模拟 quotes 网站，
// 然后使用 scrapy-go 框架爬取所有引用数据。
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

	"scrapy-go/pkg/crawler"
	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/spider"
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
type QuotesSpider struct {
	spider.BaseSpider
	mu    sync.Mutex
	items []map[string]string
}

// NewQuotesSpider 创建一个新的 QuotesSpider。
func NewQuotesSpider(baseURL string) *QuotesSpider {
	return &QuotesSpider{
		BaseSpider: spider.BaseSpider{
			SpiderName: "quotes",
			StartURLs:  []string{baseURL + "/"},
		},
	}
}

// Parse 解析响应，提取引用数据和下一页链接。
func (s *QuotesSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.SpiderOutput, error) {
	var outputs []spider.SpiderOutput
	body := response.Text()

	// 简单的文本解析提取引用
	// 在实际项目中应使用 goquery 等 HTML 解析库
	quotes := extractQuotes(body)
	for _, q := range quotes {
		item := map[string]string{
			"text":   q.text,
			"author": q.author,
			"url":    response.URL.String(),
		}
		s.mu.Lock()
		s.items = append(s.items, item)
		s.mu.Unlock()
		outputs = append(outputs, spider.SpiderOutput{Item: item})
	}

	// 提取下一页链接
	if nextURL := extractNextPageURL(body); nextURL != "" {
		absURL, err := response.URLJoin(nextURL)
		if err == nil {
			req, _ := scrapy_http.NewRequest(absURL)
			outputs = append(outputs, spider.SpiderOutput{Request: req})
		}
	}

	return outputs, nil
}

// CustomSettings 返回 Spider 级别的配置。
func (s *QuotesSpider) CustomSettings() *spider.SpiderSettings {
	return &spider.SpiderSettings{
		ConcurrentRequests: spider.IntPtr(2),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("DEBUG"),
	}
}

// ============================================================================
// 简单的 HTML 解析辅助函数
// ============================================================================

type quote struct {
	text   string
	author string
}

func extractQuotes(body string) []quote {
	var quotes []quote
	pos := 0
	for {
		// 查找 <span class="text">
		textStart := indexOf(body, `<span class="text">`, pos)
		if textStart == -1 {
			break
		}
		textStart += len(`<span class="text">`)
		textEnd := indexOf(body, `</span>`, textStart)
		if textEnd == -1 {
			break
		}
		text := body[textStart:textEnd]

		// 查找 <span class="author">
		authorStart := indexOf(body, `<span class="author">`, textEnd)
		if authorStart == -1 {
			break
		}
		authorStart += len(`<span class="author">`)
		authorEnd := indexOf(body, `</span>`, authorStart)
		if authorEnd == -1 {
			break
		}
		author := body[authorStart:authorEnd]

		quotes = append(quotes, quote{text: text, author: author})
		pos = authorEnd
	}
	return quotes
}

func extractNextPageURL(body string) string {
	marker := `<li class="next"><a href="`
	idx := indexOf(body, marker, 0)
	if idx == -1 {
		return ""
	}
	start := idx + len(marker)
	end := indexOf(body, `"`, start)
	if end == -1 {
		return ""
	}
	return body[start:end]
}

func indexOf(s, substr string, start int) int {
	if start >= len(s) {
		return -1
	}
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ============================================================================
// 主函数
// ============================================================================

func main() {
	// 1. 启动本地测试网站
	site := newLocalQuotesSite()
	defer site.Close()
	fmt.Printf("本地测试网站已启动: %s\n\n", site.URL)

	// 2. 创建 Spider
	sp := NewQuotesSpider(site.URL)

	// 3. 创建 Crawler 并运行
	// 使用 NewDefault 一行创建，日志级别由 Spider 的 CustomSettings 中的 LOG_LEVEL 控制
	c := crawler.NewDefault()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("开始爬取...")
	fmt.Println("=" + repeat("=", 59))

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("爬取出错: %v\n", err)
		os.Exit(1)
	}

	// 4. 输出结果
	fmt.Println()
	fmt.Println("=" + repeat("=", 59))
	fmt.Printf("爬取完成！共收集 %d 条引用：\n\n", len(sp.items))

	for i, item := range sp.items {
		fmt.Printf("[%d] %s\n", i+1, item["text"])
		fmt.Printf("    — %s\n", item["author"])
		fmt.Printf("    来源: %s\n\n", item["url"])
	}
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
