// 示例爬虫：使用 CrawlSpider 演示基于规则的多页面自动爬取。
//
// 本示例创建一个本地 HTTP 服务器模拟多层级网站，
// 然后使用 CrawlSpider 的 Rule 规则自动提取链接并跟踪。
// 演示 LinkExtractor 的 allow/deny 过滤、多规则匹配、
// ProcessLinks/ProcessRequest 钩子等功能。
//
// 运行方式：go run examples/crawlspider/main.go
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/linkextractor"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 本地测试网站
// ============================================================================

func newLocalSite() *httptest.Server {
	mux := http.NewServeMux()

	// 首页：包含分类链接和文章链接
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>CrawlSpider Demo</title></head>
<body>
<h1>CrawlSpider Demo Site</h1>
<nav>
  <a href="/category/tech">Technology</a>
  <a href="/category/science">Science</a>
  <a href="/about">About Us</a>
  <a href="/contact">Contact</a>
</nav>
<div class="articles">
  <a href="/article/1">Go Concurrency Patterns</a>
  <a href="/article/2">Web Scraping Best Practices</a>
</div>
</body></html>`)
	})

	// 分类页面：包含文章链接
	mux.HandleFunc("/category/tech", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Technology</title></head>
<body>
<h1>Technology Articles</h1>
<div class="articles">
  <a href="/article/1">Go Concurrency Patterns</a>
  <a href="/article/3">Kubernetes Deep Dive</a>
  <a href="/article/4">Docker Best Practices</a>
</div>
<a href="/">Back to Home</a>
</body></html>`)
	})

	mux.HandleFunc("/category/science", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Science</title></head>
<body>
<h1>Science Articles</h1>
<div class="articles">
  <a href="/article/5">Quantum Computing Explained</a>
  <a href="/article/6">Mars Exploration Update</a>
</div>
<a href="/">Back to Home</a>
</body></html>`)
	})

	// 文章页面
	articles := map[string]struct{ title, content, author string }{
		"/article/1": {"Go Concurrency Patterns", "Goroutines and channels are the building blocks...", "Alice"},
		"/article/2": {"Web Scraping Best Practices", "Always respect robots.txt and rate limits...", "Bob"},
		"/article/3": {"Kubernetes Deep Dive", "Container orchestration at scale...", "Charlie"},
		"/article/4": {"Docker Best Practices", "Multi-stage builds and security...", "Alice"},
		"/article/5": {"Quantum Computing Explained", "Qubits and superposition...", "Diana"},
		"/article/6": {"Mars Exploration Update", "Latest findings from Mars rovers...", "Eve"},
	}

	for path, article := range articles {
		p := path
		a := article
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>%s</title></head>
<body>
<article>
  <h1 class="title">%s</h1>
  <span class="author">%s</span>
  <div class="content"><p>%s</p></div>
</article>
<a href="/">Back to Home</a>
</body></html>`, a.title, a.title, a.author, a.content)
		})
	}

	// 静态页面（不需要爬取）
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><h1>About Us</h1><p>This is a demo site.</p></body></html>`)
	})

	mux.HandleFunc("/contact", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><h1>Contact</h1><p>email@example.com</p></body></html>`)
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// ArticleCrawlSpider 爬虫实现
// ============================================================================

// ArticleCrawlSpider 使用 CrawlSpider 自动爬取文章。
// 定义两条规则：
//  1. 跟踪分类页面链接（不提取数据）
//  2. 提取文章页面数据
type ArticleCrawlSpider struct {
	spider.CrawlSpider
	mu       sync.Mutex
	articles []map[string]any
}

// NewArticleCrawlSpider 创建一个新的 ArticleCrawlSpider。
func NewArticleCrawlSpider(baseURL string) *ArticleCrawlSpider {
	s := &ArticleCrawlSpider{}

	s.SpiderName = "articles"
	s.StartURLs = []string{baseURL + "/"}

	// 定义爬取规则
	s.Rules = []spider.Rule{
		// 规则 1：跟踪分类页面（不提取数据，仅跟踪链接）
		{
			LinkExtractor: linkextractor.NewHTMLLinkExtractor(
				linkextractor.WithAllow(`/category/`),
			),
			// Callback 为 nil，Follow 默认为 true
		},
		// 规则 2：提取文章页面数据
		{
			LinkExtractor: linkextractor.NewHTMLLinkExtractor(
				linkextractor.WithAllow(`/article/\d+`),
			),
			Callback: s.parseArticle,
		},
	}

	return s
}

// parseArticle 解析文章页面，提取标题、作者和内容。
func (s *ArticleCrawlSpider) parseArticle(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	title := response.CSS("h1.title::text").Get("")
	author := response.CSS("span.author::text").Get("")
	content := response.CSS("div.content p::text").Get("")

	article := map[string]any{
		"title":   title,
		"author":  author,
		"content": content,
		"url":     response.URL.String(),
	}

	s.mu.Lock()
	s.articles = append(s.articles, article)
	s.mu.Unlock()

	return []spider.Output{
		{Item: article},
	}, nil
}

// CustomSettings 返回 Spider 级别的配置。
func (s *ArticleCrawlSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(4),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("DEBUG"),
	}
}

// ============================================================================
// 主函数
// ============================================================================

func main() {
	// 1. 启动本地测试网站
	site := newLocalSite()
	defer site.Close()
	fmt.Printf("Local test site started: %s\n\n", site.URL)

	// 2. 创建 CrawlSpider
	sp := NewArticleCrawlSpider(site.URL)

	// 3. 创建 Crawler 并运行
	c := crawler.NewDefault()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Starting CrawlSpider...")
	fmt.Println("Rules:")
	fmt.Println("  1. Follow /category/* links (no callback)")
	fmt.Println("  2. Extract /article/* pages (with callback)")
	fmt.Println("============================================================")

	start := time.Now()
	err := c.Run(ctx, sp)
	elapsed := time.Since(start)

	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("Crawl error: %v\n", err)
		os.Exit(1)
	}

	// 4. 输出结果
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Printf("Crawl completed! Collected %d articles in %v:\n\n", len(sp.articles), elapsed)

	for i, article := range sp.articles {
		fmt.Printf("[%d] %s\n", i+1, article["title"])
		fmt.Printf("    Author: %s\n", article["author"])
		fmt.Printf("    Content: %s\n", article["content"])
		fmt.Printf("    URL: %s\n\n", article["url"])
	}
}
