// 示例爬虫：演示 scrapy-go 泛型类型安全 Settings API（TD-004）。
//
// 本示例展示如何使用 Key[T] + Get[T]/Set[T] 泛型 API 进行编译期类型安全的配置管理，
// 对比旧式 GetInt/GetString 等方法，展示新 API 的优势：
//   - 编译期类型检查：无法将错误类型的值赋给配置项
//   - 消除魔法字符串：所有配置键名集中定义为 Key[T] 常量
//   - 内置默认值：Key 中携带默认值，调用者无需重复指定
//
// 运行方式：go run examples/typed_settings/main.go
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
	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 本地测试服务器
// ============================================================================

// newLocalSite 创建一个模拟的多页面网站，用于测试爬虫配置。
func newLocalSite() *httptest.Server {
	mux := http.NewServeMux()

	// 首页：文章列表
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Tech Blog</title></head>
<body>
<h1>Tech Blog - 类型安全配置示例</h1>
<div class="article-list">
  <div class="article">
    <h2 class="title"><a href="/article/1">Go 泛型最佳实践</a></h2>
    <span class="author">Alice</span>
    <span class="date">2026-01-15</span>
    <p class="summary">探索 Go 1.18+ 泛型在实际项目中的应用模式...</p>
  </div>
  <div class="article">
    <h2 class="title"><a href="/article/2">并发模式与 Channel</a></h2>
    <span class="author">Bob</span>
    <span class="date">2026-02-20</span>
    <p class="summary">深入理解 Go 并发原语和常见设计模式...</p>
  </div>
  <div class="article">
    <h2 class="title"><a href="/article/3">类型安全的配置系统设计</a></h2>
    <span class="author">Charlie</span>
    <span class="date">2026-03-10</span>
    <p class="summary">如何利用泛型构建编译期类型安全的配置框架...</p>
  </div>
</div>
<nav><a href="/page/2" class="next">下一页</a></nav>
</body></html>`)
	})

	// 第二页
	mux.HandleFunc("/page/2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Tech Blog - Page 2</title></head>
<body>
<h1>Tech Blog - 第 2 页</h1>
<div class="article-list">
  <div class="article">
    <h2 class="title"><a href="/article/4">错误处理的艺术</a></h2>
    <span class="author">Diana</span>
    <span class="date">2026-04-05</span>
    <p class="summary">Go 中优雅的错误处理策略与 errors 包的高级用法...</p>
  </div>
  <div class="article">
    <h2 class="title"><a href="/article/5">性能优化指南</a></h2>
    <span class="author">Eve</span>
    <span class="date">2026-05-01</span>
    <p class="summary">从 pprof 到 benchmark，系统化的 Go 性能优化方法论...</p>
  </div>
</div>
</body></html>`)
	})

	// 文章详情页
	articles := map[string]string{
		"/article/1": `<html><head><title>Go 泛型最佳实践</title></head><body>
<article><h1>Go 泛型最佳实践</h1><div class="content"><p>泛型让我们可以编写类型安全且可复用的代码。Key[T] 模式是一个典型应用。</p></div><span class="word-count">1200</span></article></body></html>`,
		"/article/2": `<html><head><title>并发模式与 Channel</title></head><body>
<article><h1>并发模式与 Channel</h1><div class="content"><p>Channel 是 Go 并发编程的核心原语，理解其底层实现有助于写出高效代码。</p></div><span class="word-count">2500</span></article></body></html>`,
		"/article/3": `<html><head><title>类型安全的配置系统设计</title></head><body>
<article><h1>类型安全的配置系统设计</h1><div class="content"><p>通过泛型 Key[T] 将配置键与值类型绑定，实现编译期类型检查。</p></div><span class="word-count">1800</span></article></body></html>`,
		"/article/4": `<html><head><title>错误处理的艺术</title></head><body>
<article><h1>错误处理的艺术</h1><div class="content"><p>sentinel error、error wrapping、errors.Is/As 构成了 Go 错误处理的三大支柱。</p></div><span class="word-count">2100</span></article></body></html>`,
		"/article/5": `<html><head><title>性能优化指南</title></head><body>
<article><h1>性能优化指南</h1><div class="content"><p>不要过早优化，但要知道何时以及如何优化。pprof 是你最好的朋友。</p></div><span class="word-count">3000</span></article></body></html>`,
	}

	for path, html := range articles {
		content := html // 避免闭包捕获循环变量
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, content)
		})
	}

	return httptest.NewServer(mux)
}

// ============================================================================
// 自定义配置键（展示用户如何定义自己的类型化配置键）
// ============================================================================

// 用户可以定义自己的类型化配置键，与框架内置键使用相同的 Key[T] 类型。
// 这些键同样享有编译期类型安全保障。
var (
	// KeyMinWordCount 文章最小字数过滤阈值。
	KeyMinWordCount = settings.Key[int]{Name: "MIN_WORD_COUNT", Default: 1000}
	// KeyTargetAuthors 目标作者列表（仅爬取这些作者的文章）。
	KeyTargetAuthors = settings.Key[[]string]{Name: "TARGET_AUTHORS", Default: []string{}}
	// KeyEnableDetailPage 是否爬取文章详情页。
	KeyEnableDetailPage = settings.Key[bool]{Name: "ENABLE_DETAIL_PAGE", Default: true}
)

// ============================================================================
// BlogSpider 爬虫实现
// ============================================================================

// BlogSpider 爬取本地 Tech Blog 网站的文章数据。
// 通过泛型 Settings API 读取配置，展示编译期类型安全的优势。
type BlogSpider struct {
	spider.Base
	mu           sync.Mutex
	items        []map[string]any
	enableDetail bool // 从配置中读取
}

// NewBlogSpider 创建一个新的 BlogSpider。
func NewBlogSpider(baseURL string, s *settings.Settings) *BlogSpider {
	startURLs := []string{baseURL + "/", baseURL + "/page/2"}

	// ✅ 使用泛型 API 读取配置，编译期确定返回 bool 类型
	enableDetail := settings.Get(s, KeyEnableDetailPage)
	if enableDetail {
		// 根据配置决定是否将详情页 URL 加入起始列表
		startURLs = append(startURLs,
			baseURL+"/article/1",
			baseURL+"/article/2",
			baseURL+"/article/3",
			baseURL+"/article/4",
			baseURL+"/article/5",
		)
	}

	return &BlogSpider{
		Base: spider.Base{
			SpiderName: "tech_blog",
			StartURLs:  startURLs,
		},
		enableDetail: enableDetail,
	}
}

// AllowedDomains 返回允许爬取的域名列表。
func (s *BlogSpider) AllowedDomains() []string {
	return []string{"127.0.0.1"}
}

// Parse 解析文章列表页，提取文章摘要信息。
func (s *BlogSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	var outputs []spider.Output

	// 判断是否为详情页（URL 包含 /article/）
	if response.URL.Path != "/" && response.URL.Path != "/page/2" {
		return s.ParseDetail(ctx, response)
	}

	articles := response.CSS("div.article")
	for _, article := range articles {
		title := article.CSS("h2.title a::text").Get("")
		author := article.CSS("span.author::text").Get("")
		date := article.CSS("span.date::text").Get("")
		summary := article.CSS("p.summary::text").Get("")

		item := map[string]any{
			"title":   title,
			"author":  author,
			"date":    date,
			"summary": summary,
			"source":  "list",
		}

		s.mu.Lock()
		s.items = append(s.items, item)
		s.mu.Unlock()
		outputs = append(outputs, spider.Output{Item: item})
	}

	return outputs, nil
}

// ParseDetail 解析文章详情页，提取正文内容。
func (s *BlogSpider) ParseDetail(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	title := response.CSS("article h1::text").Get("")
	content := response.CSS("div.content p::text").Get("")
	wordCount := response.CSS("span.word-count::text").Get("")

	item := map[string]any{
		"title":      title,
		"content":    content,
		"word_count": wordCount,
		"url":        response.URL.String(),
		"source":     "detail",
	}

	s.mu.Lock()
	s.items = append(s.items, item)
	s.mu.Unlock()

	return []spider.Output{{Item: item}}, nil
}

// ============================================================================
// 自定义 Pipeline：展示在 Pipeline 中使用泛型 Settings API
// ============================================================================

// WordCountFilterPipeline 根据配置的最小字数过滤文章。
// 展示如何在 Pipeline 中使用泛型 Settings API 读取配置。
type WordCountFilterPipeline struct {
	minWordCount int
	filtered     int
}

// Open 在 Spider 打开时初始化 Pipeline。
func (p *WordCountFilterPipeline) Open(ctx context.Context) error {
	return nil
}

// Close 在 Spider 关闭时清理资源。
func (p *WordCountFilterPipeline) Close(ctx context.Context) error {
	if p.filtered > 0 {
		fmt.Printf("  [WordCountFilter] 共过滤 %d 篇低字数文章\n", p.filtered)
	}
	return nil
}

// ProcessItem 处理 Item，过滤字数不足的文章。
func (p *WordCountFilterPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	m, ok := item.(map[string]any)
	if !ok {
		return item, nil
	}

	// 仅过滤详情页的 Item
	if m["source"] != "detail" {
		return item, nil
	}

	wordCountStr, _ := m["word_count"].(string)
	var wordCount int
	fmt.Sscanf(wordCountStr, "%d", &wordCount)

	if wordCount < p.minWordCount {
		p.filtered++
		return nil, serrors.ErrDropItem
	}

	return item, nil
}

// ============================================================================
// 主函数
// ============================================================================

func main() {
	// 1. 启动本地测试网站
	site := newLocalSite()
	defer site.Close()
	fmt.Printf("🌐 本地测试服务器已启动: %s\n\n", site.URL)

	// ========================================================================
	// 2. 使用泛型 Settings API 配置爬虫（核心演示）
	// ========================================================================
	fmt.Println("📋 配置演示：使用泛型类型安全 Settings API")
	fmt.Println("============================================================")

	s := settings.New()

	// ✅ 使用 settings.Set 进行类型安全的配置设置
	// 编译期约束：第三个参数必须与 Key[T] 的 T 类型匹配
	settings.Set(s, settings.KeyConcurrentRequests, 4, settings.PriorityProject)
	settings.Set(s, settings.KeyDownloadTimeout, 30, settings.PriorityProject)
	settings.Set(s, settings.KeyRetryEnabled, true, settings.PriorityProject)
	settings.Set(s, settings.KeyRetryTimes, 3, settings.PriorityProject)
	settings.Set(s, settings.KeyBotName, "typed-settings-demo", settings.PriorityProject)
	settings.Set(s, settings.KeyUserAgent, "TypedSettingsBot/1.0", settings.PriorityProject)
	settings.Set(s, settings.KeyLogLevel, "INFO", settings.PriorityProject)
	settings.Set(s, settings.KeyRandomizeDownloadDelay, false, settings.PriorityProject)

	// ✅ 设置自定义配置键（用户定义的 Key[T]）
	settings.Set(s, KeyMinWordCount, 1500, settings.PriorityProject)
	settings.Set(s, KeyEnableDetailPage, true, settings.PriorityProject)
	settings.Set(s, KeyTargetAuthors, []string{"Alice", "Bob", "Charlie"}, settings.PriorityProject)

	// ========================================================================
	// 3. 使用 settings.Get 读取配置（编译期确定返回类型）
	// ========================================================================
	fmt.Println()
	fmt.Println("🔍 读取配置（编译期类型安全）：")
	fmt.Println("------------------------------------------------------------")

	// ✅ 返回类型由 Key[T] 的 T 参数在编译期确定，无需类型断言
	concurrency := settings.Get(s, settings.KeyConcurrentRequests) // int
	botName := settings.Get(s, settings.KeyBotName)                // string
	retryEnabled := settings.Get(s, settings.KeyRetryEnabled)      // bool
	retryTimes := settings.Get(s, settings.KeyRetryTimes)          // int
	timeout := settings.Get(s, settings.KeyDownloadTimeout)        // int
	userAgent := settings.Get(s, settings.KeyUserAgent)            // string

	fmt.Printf("  并发请求数 (int):     %d\n", concurrency)
	fmt.Printf("  爬虫名称 (string):    %s\n", botName)
	fmt.Printf("  重试启用 (bool):      %v\n", retryEnabled)
	fmt.Printf("  重试次数 (int):       %d\n", retryTimes)
	fmt.Printf("  下载超时 (int):       %d 秒\n", timeout)
	fmt.Printf("  User-Agent (string):  %s\n", userAgent)

	// ✅ 读取自定义配置键
	minWords := settings.Get(s, KeyMinWordCount)         // int
	enableDetail := settings.Get(s, KeyEnableDetailPage) // bool
	authors := settings.Get(s, KeyTargetAuthors)         // []string

	fmt.Printf("  最小字数 (int):       %d\n", minWords)
	fmt.Printf("  爬取详情 (bool):      %v\n", enableDetail)
	fmt.Printf("  目标作者 ([]string):  %v\n", authors)

	// ========================================================================
	// 4. 演示 MustGet（必须存在的配置项）
	// ========================================================================
	fmt.Println()
	fmt.Println("⚡ MustGet 演示（配置项必须存在，否则 panic）：")
	fmt.Println("------------------------------------------------------------")

	// ✅ MustGet 用于必须存在的配置项，不存在时 panic
	mustConcurrency := settings.MustGet(s, settings.KeyConcurrentRequests)
	fmt.Printf("  MustGet 并发数: %d ✓\n", mustConcurrency)

	// ========================================================================
	// 5. 演示默认值机制
	// ========================================================================
	fmt.Println()
	fmt.Println("📦 默认值演示（未设置的配置项返回 Key 中定义的默认值）：")
	fmt.Println("------------------------------------------------------------")

	// 这些配置项未被显式设置，将返回 Key[T] 中定义的 Default 值
	cacheEnabled := settings.Get(s, settings.KeyHTTPCacheEnabled) // 默认 false
	cookiesEnabled := settings.Get(s, settings.KeyCookiesEnabled) // 默认 true
	depthLimit := settings.Get(s, settings.KeyDepthLimit)         // 默认 0（无限制）
	redirectMax := settings.Get(s, settings.KeyRedirectMaxTimes)  // 默认 20

	fmt.Printf("  HTTP 缓存 (默认 false): %v\n", cacheEnabled)
	fmt.Printf("  Cookies (默认 true):    %v\n", cookiesEnabled)
	fmt.Printf("  深度限制 (默认 0):      %d\n", depthLimit)
	fmt.Printf("  最大重定向 (默认 20):   %d\n", redirectMax)

	// ========================================================================
	// 6. 对比旧 API（向后兼容）
	// ========================================================================
	fmt.Println()
	fmt.Println("🔄 向后兼容：旧 API 与新 API 返回相同结果")
	fmt.Println("------------------------------------------------------------")

	oldConcurrency := s.GetInt("CONCURRENT_REQUESTS", 0)
	newConcurrency := settings.Get(s, settings.KeyConcurrentRequests)
	fmt.Printf("  旧 API: s.GetInt(\"CONCURRENT_REQUESTS\", 0) = %d\n", oldConcurrency)
	fmt.Printf("  新 API: settings.Get(s, KeyConcurrentRequests) = %d\n", newConcurrency)
	fmt.Printf("  结果一致: %v ✓\n", oldConcurrency == newConcurrency)

	// ========================================================================
	// 7. 创建 Crawler 并运行爬虫
	// ========================================================================
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("🕷️ 开始爬取...")
	fmt.Println("============================================================")

	sp := NewBlogSpider(site.URL, s)

	// 创建 Crawler，注入配置
	c := crawler.New(crawler.WithSettings(s))

	// 添加自定义 Pipeline（使用泛型 API 读取的配置值）
	c.AddPipeline(&WordCountFilterPipeline{
		minWordCount: settings.Get(s, KeyMinWordCount), // ✅ 编译期确定 int 类型
	}, "WordCountFilter", 300)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	err := c.Run(ctx, sp)
	elapsed := time.Since(start)

	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("\n❌ 爬取错误: %v\n", err)
		os.Exit(1)
	}

	// ========================================================================
	// 8. 输出结果
	// ========================================================================
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Printf("✅ 爬取完成！耗时 %v，共收集 %d 条数据\n\n", elapsed.Round(time.Millisecond), len(sp.items))

	listItems := 0
	detailItems := 0
	for _, item := range sp.items {
		if item["source"] == "list" {
			listItems++
		} else {
			detailItems++
		}
	}

	fmt.Printf("📊 统计：列表页 %d 条 | 详情页 %d 条\n\n", listItems, detailItems)

	fmt.Println("📝 详情页数据：")
	for _, item := range sp.items {
		if item["source"] != "detail" {
			continue
		}
		fmt.Printf("  📄 %s\n", item["title"])
		fmt.Printf("     字数: %s | URL: %s\n", item["word_count"], item["url"])
		if content, ok := item["content"].(string); ok && len(content) > 50 {
			fmt.Printf("     摘要: %s...\n", content[:50])
		} else if content, ok := item["content"].(string); ok {
			fmt.Printf("     摘要: %s\n", content)
		}
		fmt.Println()
	}

	fmt.Println("============================================================")
	fmt.Println("💡 本示例展示的 TD-004 特性：")
	fmt.Println("   1. Key[T] 泛型类型 — 配置键与值类型编译期绑定")
	fmt.Println("   2. settings.Get(s, key) — 返回类型由 Key[T] 确定，无需类型断言")
	fmt.Println("   3. settings.Set(s, key, val, priority) — 编译期约束值类型")
	fmt.Println("   4. settings.MustGet(s, key) — 必须存在的配置项")
	fmt.Println("   5. 自定义 Key[T] — 用户可定义自己的类型化配置键")
	fmt.Println("   6. 向后兼容 — 旧 API（GetInt/GetString）继续可用")
	fmt.Println("============================================================")
}
