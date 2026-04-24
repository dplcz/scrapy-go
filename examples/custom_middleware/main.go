// 示例爬虫：演示自定义下载器中间件和 Spider 中间件的使用。
//
// 本示例展示四个自定义中间件：
//  1. AuthMiddleware（下载器）— 为请求注入 API Token 认证头
//  2. LoggingMiddleware（下载器）— 记录请求/响应的详细耗时信息
//  3. CacheMiddleware（下载器）— 对相同 URL 的请求进行内存缓存，避免重复下载
//  4. ItemStatsMiddleware（Spider）— 统计 Spider 产出的 Item 和 Request 数量
//
// 本地 httptest 服务器模拟一个需要认证的分页 API，
// 未携带正确 Token 的请求将返回 401 Unauthorized。
//
// 运行方式：go run examples/custom_middleware/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	"scrapy-go/pkg/crawler"
	dl_mw "scrapy-go/pkg/downloader/middleware"
	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/spider"
	spider_mw "scrapy-go/pkg/spider/middleware"
)

// ============================================================================
// 数据模型
// ============================================================================

// Article 表示一篇文章。
type Article struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Tags   string `json:"tags"`
}

// ArticleAPIResponse 表示文章 API 的分页响应。
type ArticleAPIResponse struct {
	Page     int       `json:"page"`
	Total    int       `json:"total_pages"`
	Articles []Article `json:"articles"`
	NextPage string    `json:"next_page,omitempty"`
}

// ============================================================================
// 本地测试 API 服务器（需要认证）
// ============================================================================

const validAPIToken = "scrapy-go-secret-token-2026"

func newLocalArticleAPI() *httptest.Server {
	mux := http.NewServeMux()

	// 认证检查中间件
	authCheck := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if token != "Bearer "+validAPIToken {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": "缺少有效的 API Token，请在 Authorization 头中携带 Bearer Token",
				})
				return
			}
			next(w, r)
		}
	}

	// 文章列表 API（需要认证）
	mux.HandleFunc("/api/articles", authCheck(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}

		// 模拟服务器处理延迟
		time.Sleep(50 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Request-ID", fmt.Sprintf("req-%s-%d", page, time.Now().UnixNano()))

		var resp ArticleAPIResponse
		switch page {
		case "1":
			resp = ArticleAPIResponse{
				Page:  1,
				Total: 3,
				Articles: []Article{
					{ID: 1, Title: "Go 并发编程实战", Author: "张三", Tags: "Go,并发,goroutine"},
					{ID: 2, Title: "深入理解 Go 接口", Author: "李四", Tags: "Go,接口,设计模式"},
					{ID: 3, Title: "Go 错误处理最佳实践", Author: "王五", Tags: "Go,错误处理"},
				},
				NextPage: "/api/articles?page=2",
			}
		case "2":
			resp = ArticleAPIResponse{
				Page:  2,
				Total: 3,
				Articles: []Article{
					{ID: 4, Title: "使用 Go 构建微服务", Author: "赵六", Tags: "Go,微服务,gRPC"},
					{ID: 5, Title: "Go 性能优化指南", Author: "张三", Tags: "Go,性能,pprof"},
					{ID: 6, Title: "Go 模块管理详解", Author: "李四", Tags: "Go,模块,依赖管理"},
				},
				NextPage: "/api/articles?page=3",
			}
		case "3":
			resp = ArticleAPIResponse{
				Page:  3,
				Total: 3,
				Articles: []Article{
					{ID: 7, Title: "Go 泛型入门与进阶", Author: "王五", Tags: "Go,泛型,类型参数"},
					{ID: 8, Title: "Go 测试驱动开发", Author: "赵六", Tags: "Go,测试,TDD"},
				},
				// 最后一页没有 NextPage
			}
		default:
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "page not found"})
			return
		}

		json.NewEncoder(w).Encode(resp)
	}))

	// 文章详情 API（需要认证，用于演示缓存中间件）
	mux.HandleFunc("/api/article/", authCheck(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{
			"id":      1,
			"title":   "Go 并发编程实战",
			"author":  "张三",
			"content": "这是一篇关于 Go 并发编程的详细文章...",
		})
	}))

	return httptest.NewServer(mux)
}

// ============================================================================
// 自定义中间件 1：AuthMiddleware — 认证中间件
// ============================================================================

// AuthMiddleware 为每个请求自动注入 API Token 认证头。
// 这是最常见的自定义中间件场景之一：统一处理认证逻辑，
// 避免在每个请求中手动设置 Token。
//
// 建议优先级：450（在 DefaultHeaders(400) 之后，UserAgent(500) 之前）
type AuthMiddleware struct {
	dl_mw.BaseDownloaderMiddleware // 嵌入基础实现，只需覆盖 ProcessRequest
	token                          string
}

func NewAuthMiddleware(token string) *AuthMiddleware {
	return &AuthMiddleware{token: token}
}

// ProcessRequest 在请求发送前注入 Authorization 头。
func (m *AuthMiddleware) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	if m.token != "" {
		request.Headers.Set("Authorization", "Bearer "+m.token)
	}
	return nil, nil // 返回 nil 表示继续处理链
}

// ============================================================================
// 自定义中间件 2：LoggingMiddleware — 请求/响应日志中间件
// ============================================================================

// LoggingMiddleware 记录每个请求的发送和响应的接收，包含耗时统计。
// 通过在 ProcessRequest 中记录开始时间（存入 Meta），
// 在 ProcessResponse 中计算耗时并打印日志。
//
// 建议优先级：50（最外层，最先接触请求、最后接触响应）
type LoggingMiddleware struct {
	dl_mw.BaseDownloaderMiddleware
	logger *slog.Logger
}

func NewLoggingMiddleware(logger *slog.Logger) *LoggingMiddleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoggingMiddleware{logger: logger}
}

// ProcessRequest 记录请求开始时间。
func (m *LoggingMiddleware) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	// 将请求开始时间存入 Meta，供 ProcessResponse 计算耗时
	request.SetMeta("_logging_start_time", time.Now())
	m.logger.Info("📤 发送请求",
		"method", request.Method,
		"url", request.URL.String(),
	)
	return nil, nil
}

// ProcessResponse 记录响应信息和耗时。
func (m *LoggingMiddleware) ProcessResponse(ctx context.Context, request *scrapy_http.Request, response *scrapy_http.Response) (*scrapy_http.Response, error) {
	elapsed := "unknown"
	if startTime, ok := request.GetMeta("_logging_start_time"); ok {
		if t, ok := startTime.(time.Time); ok {
			elapsed = time.Since(t).String()
		}
	}

	// 检查是否命中缓存
	cached := ""
	if hit, ok := request.GetMeta("_cache_hit"); ok {
		if b, ok := hit.(bool); ok && b {
			cached = " [缓存命中]"
		}
	}

	m.logger.Info("📥 收到响应"+cached,
		"status", response.Status,
		"url", response.URL.String(),
		"body_size", len(response.Body),
		"elapsed", elapsed,
	)
	return response, nil
}

// ProcessException 记录请求异常。
func (m *LoggingMiddleware) ProcessException(ctx context.Context, request *scrapy_http.Request, err error) (*scrapy_http.Response, error) {
	m.logger.Error("❌ 请求异常",
		"url", request.URL.String(),
		"error", err.Error(),
	)
	return nil, nil // 不处理异常，继续传播
}

// ============================================================================
// 自定义中间件 3：CacheMiddleware — 内存缓存中间件
// ============================================================================

// CacheMiddleware 对相同 URL 的 GET 请求进行内存缓存。
// 当同一个 URL 被多次请求时，第二次及之后的请求将直接返回缓存的响应，
// 不再发起实际的 HTTP 请求（短路）。
//
// 这在需要重复访问同一页面（如详情页去重失败时）的场景中非常有用。
//
// 建议优先级：900（在所有其他中间件之后，最接近下载器）
type CacheMiddleware struct {
	dl_mw.BaseDownloaderMiddleware
	mu     sync.RWMutex
	cache  map[string]*scrapy_http.Response
	hits   int
	misses int
	logger *slog.Logger
}

func NewCacheMiddleware(logger *slog.Logger) *CacheMiddleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &CacheMiddleware{
		cache:  make(map[string]*scrapy_http.Response),
		logger: logger,
	}
}

// ProcessRequest 检查缓存，如果命中则直接返回缓存的响应（短路）。
func (m *CacheMiddleware) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	// 只缓存 GET 请求
	if request.Method != "GET" {
		return nil, nil
	}

	// 检查是否设置了 dont_cache meta
	if dontCache, ok := request.GetMeta("dont_cache"); ok {
		if dc, ok := dontCache.(bool); ok && dc {
			return nil, nil
		}
	}

	cacheKey := request.URL.String()

	m.mu.RLock()
	cached, ok := m.cache[cacheKey]
	m.mu.RUnlock()

	if ok {
		m.mu.Lock()
		m.hits++
		m.mu.Unlock()

		m.logger.Debug("🎯 缓存命中", "url", cacheKey)

		// 返回缓存响应的副本（避免并发修改）
		resp := cached.Copy()
		resp.Request = request
		// 标记为缓存命中，供 LoggingMiddleware 使用
		request.SetMeta("_cache_hit", true)
		return resp, nil // 短路：不再调用后续中间件和下载器
	}

	m.mu.Lock()
	m.misses++
	m.mu.Unlock()
	return nil, nil // 缓存未命中，继续处理链
}

// ProcessResponse 将响应存入缓存。
func (m *CacheMiddleware) ProcessResponse(ctx context.Context, request *scrapy_http.Request, response *scrapy_http.Response) (*scrapy_http.Response, error) {
	// 只缓存 GET 请求的成功响应
	if request.Method != "GET" || response.Status != 200 {
		return response, nil
	}

	cacheKey := request.URL.String()

	m.mu.Lock()
	m.cache[cacheKey] = response.Copy()
	m.mu.Unlock()

	return response, nil
}

// Stats 返回缓存统计信息。
func (m *CacheMiddleware) Stats() (hits, misses int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hits, m.misses
}

// ============================================================================
// 自定义 Spider 中间件：ItemStatsMiddleware — 统计 Spider 产出
// ============================================================================

// ItemStatsMiddleware 统计 Spider 回调产出的 Item 和 Request 数量。
// 它在 ProcessSpiderOutput 中遍历所有输出项进行计数，
// 并在 ProcessSpiderInput 中记录每个响应的处理。
//
// 建议优先级：100（尽早拦截输入，尽晚拦截输出，确保统计完整）
type ItemStatsMiddleware struct {
	spider_mw.BaseSpiderMiddleware
	mu           sync.Mutex
	itemCount    int
	requestCount int
	pageCount    int
}

// ProcessSpiderInput 记录响应处理次数。
func (m *ItemStatsMiddleware) ProcessSpiderInput(ctx context.Context, response *scrapy_http.Response) error {
	m.mu.Lock()
	m.pageCount++
	m.mu.Unlock()
	return nil
}

// ProcessSpiderOutput 统计产出的 Item 和 Request 数量。
func (m *ItemStatsMiddleware) ProcessSpiderOutput(ctx context.Context, response *scrapy_http.Response, result []spider.SpiderOutput) ([]spider.SpiderOutput, error) {
	m.mu.Lock()
	for _, output := range result {
		if output.IsItem() {
			m.itemCount++
		} else if output.IsRequest() {
			m.requestCount++
		}
	}
	m.mu.Unlock()
	return result, nil
}

// Stats 返回统计信息。
func (m *ItemStatsMiddleware) Stats() (items, requests, pages int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.itemCount, m.requestCount, m.pageCount
}

// ============================================================================
// ArticleSpider — 文章爬虫
// ============================================================================

type ArticleSpider struct {
	spider.BaseSpider
	mu       sync.Mutex
	articles []Article
}

func NewArticleSpider(baseURL string) *ArticleSpider {
	return &ArticleSpider{
		BaseSpider: spider.BaseSpider{
			SpiderName: "articles",
			StartURLs: []string{
				baseURL + "/api/articles?page=1",
			},
		},
	}
}

// Parse 解析文章列表 API 响应。
func (s *ArticleSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.SpiderOutput, error) {
	var apiResp ArticleAPIResponse
	if err := response.JSON(&apiResp); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	var outputs []spider.SpiderOutput

	// 提取文章数据
	for _, article := range apiResp.Articles {
		s.mu.Lock()
		s.articles = append(s.articles, article)
		s.mu.Unlock()
		outputs = append(outputs, spider.SpiderOutput{Item: article})
	}

	// 对第一页的第一篇文章，请求详情页（用于演示缓存中间件）
	// 在 ParseDetail 回调中会再次请求同一 URL，第二次将命中缓存
	if apiResp.Page == 1 && len(apiResp.Articles) > 0 {
		detailURL := response.URL.Scheme + "://" + response.URL.Host +
			fmt.Sprintf("/api/article/%d", apiResp.Articles[0].ID)
		// 显式转换为 spider.CallbackFunc，确保 Scraper 中的类型断言能成功
		cb := spider.CallbackFunc(s.ParseDetail)
		req, err := scrapy_http.NewRequest(detailURL,
			scrapy_http.WithCallback(cb),
		)
		if err == nil {
			outputs = append(outputs, spider.SpiderOutput{Request: req})
		}
	}

	// 如果有下一页，生成新请求
	if apiResp.NextPage != "" {
		nextURL, err := response.URLJoin(apiResp.NextPage)
		if err == nil {
			req, _ := scrapy_http.NewRequest(nextURL)
			outputs = append(outputs, spider.SpiderOutput{Request: req})
		}
	}

	return outputs, nil
}

// ParseDetail 解析文章详情页响应。
// 第一次调用时会再次请求同一 URL（跳过去重），第二次将命中缓存。
func (s *ArticleSpider) ParseDetail(ctx context.Context, response *scrapy_http.Response) ([]spider.SpiderOutput, error) {
	cached := ""
	if hit, ok := response.GetMeta("_cache_hit"); ok {
		if b, ok := hit.(bool); ok && b {
			cached = " [来自缓存]"
		}
	}
	fmt.Printf("  📄 获取到文章详情%s: %s\n", cached, response.URL.String())

	// 如果这是第一次请求（非缓存命中），再请求一次同一 URL 以演示缓存
	if cached == "" {
		cb := spider.CallbackFunc(s.ParseDetail)
		req, err := scrapy_http.NewRequest(response.URL.String(),
			scrapy_http.WithCallback(cb),
			scrapy_http.WithDontFilter(true), // 跳过去重，确保请求能发出
		)
		if err == nil {
			return []spider.SpiderOutput{{Request: req}}, nil
		}
	}

	return nil, nil
}

// CustomSettings 返回 Spider 级别的配置。
func (s *ArticleSpider) CustomSettings() *spider.SpiderSettings {
	return &spider.SpiderSettings{
		ConcurrentRequests: spider.IntPtr(2),
		DownloadDelay:      spider.DurationPtr(0),
		LogLevel:           spider.StringPtr("WARN"),
	}
}

// ============================================================================
// 主函数
// ============================================================================

func main() {
	// 1. 启动本地测试 API 服务器
	api := newLocalArticleAPI()
	defer api.Close()
	fmt.Printf("📡 本地认证 API 已启动: %s/api/articles\n", api.URL)
	fmt.Printf("   API Token: %s\n\n", validAPIToken)

	// 2. 创建 Spider
	sp := NewArticleSpider(api.URL)

	// 3. 创建 Crawler
	c := crawler.NewDefault()

	// 4. 注册自定义中间件（按优先级从小到大排列）
	//
	// 中间件执行顺序：
	//   ProcessRequest（正序）: Logging(50) → Auth(450) → [内置中间件] → Cache(900)
	//   ProcessResponse（逆序）: Cache(900) → [内置中间件] → Auth(450) → Logging(50)
	//
	// 这意味着：
	//   - LoggingMiddleware 最先看到请求、最后看到响应（可以准确计算总耗时）
	//   - AuthMiddleware 在内置中间件之前注入 Token
	//   - CacheMiddleware 最接近下载器，可以在实际下载前短路返回缓存

	loggingMW := NewLoggingMiddleware(nil)
	authMW := NewAuthMiddleware(validAPIToken)
	cacheMW := NewCacheMiddleware(nil)

	c.AddDownloaderMiddleware(loggingMW, "Logging", 50)
	c.AddDownloaderMiddleware(authMW, "Auth", 450)
	c.AddDownloaderMiddleware(cacheMW, "Cache", 900)

	// 4.1 注册自定义 Spider 中间件
	//
	// Spider 中间件执行顺序：
	//   ProcessSpiderInput（正序）: ItemStats(100)
	//   ProcessSpiderOutput（逆序）: ItemStats(100)
	//
	// ItemStatsMiddleware 统计 Spider 回调产出的 Item 和 Request 数量
	itemStatsMW := &ItemStatsMiddleware{}
	c.AddSpiderMiddleware(itemStatsMW, "ItemStats", 100)

	// 5. 运行爬虫
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("🚀 开始爬取文章数据...")
	fmt.Println("=" + repeat("=", 59))

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("❌ 爬取出错: %v\n", err)
		os.Exit(1)
	}

	// 6. 输出结果
	fmt.Println()
	fmt.Println("=" + repeat("=", 59))
	fmt.Printf("📊 爬取完成！共收集 %d 篇文章：\n\n", len(sp.articles))

	for _, a := range sp.articles {
		fmt.Printf("  [%d] %s — %s  (%s)\n", a.ID, a.Title, a.Author, a.Tags)
	}

	// 7. 输出缓存统计
	hits, misses := cacheMW.Stats()
	fmt.Printf("\n📈 缓存统计: 命中 %d 次, 未命中 %d 次\n", hits, misses)

	// 8. 输出 Spider 中间件统计
	items, requests, pages := itemStatsMW.Stats()
	fmt.Printf("🕷️ Spider 中间件统计: 处理 %d 个页面, 产出 %d 个 Item, %d 个 Request\n", pages, items, requests)
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
