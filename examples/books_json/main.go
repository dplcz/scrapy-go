// 示例爬虫：从本地 JSON API 读取多页数据，通过 Pipeline 保存到本地 JSON 文件。
//
// 本示例演示：
//  1. 本地 httptest 服务器提供分页 JSON API（模拟图书数据库）
//  2. Spider 解析 JSON 响应，提取结构化数据
//  3. 自定义 Pipeline 链：数据清洗 → JSON 文件持久化
//
// 运行方式：go run examples/books_json/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	"scrapy-go/pkg/crawler"
	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/spider"
)

// ============================================================================
// 数据模型
// ============================================================================

// Book 表示一本图书的结构化数据。
type Book struct {
	Title   string   `json:"title"`
	Author  string   `json:"author"`
	Price   float64  `json:"price"`
	Tags    []string `json:"tags"`
	InStock bool     `json:"in_stock"`
	PageURL string   `json:"page_url,omitempty"`
}

// APIResponse 表示 JSON API 的分页响应。
type APIResponse struct {
	Page     int    `json:"page"`
	Total    int    `json:"total_pages"`
	Count    int    `json:"count"`
	NextPage string `json:"next_page,omitempty"`
	Books    []Book `json:"books"`
}

// ============================================================================
// 本地 JSON API 服务器
// ============================================================================

func newLocalBookAPI() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/books", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		var resp APIResponse
		switch page {
		case "1":
			resp = APIResponse{
				Page:     1,
				Total:    3,
				Count:    3,
				NextPage: "/api/books?page=2",
				Books: []Book{
					{Title: "Go 语言圣经", Author: "Alan Donovan", Price: 79.00, Tags: []string{"Go", "编程"}, InStock: true},
					{Title: "深入理解计算机系统", Author: "Randal E. Bryant", Price: 139.00, Tags: []string{"计算机", "系统"}, InStock: true},
					{Title: "算法导论", Author: "Thomas H. Cormen", Price: 128.00, Tags: []string{"算法", "数据结构"}, InStock: false},
				},
			}
		case "2":
			resp = APIResponse{
				Page:     2,
				Total:    3,
				Count:    3,
				NextPage: "/api/books?page=3",
				Books: []Book{
					{Title: "设计模式", Author: "Erich Gamma", Price: 65.00, Tags: []string{"设计模式", "面向对象"}, InStock: true},
					{Title: "重构", Author: "Martin Fowler", Price: 99.00, Tags: []string{"重构", "代码质量"}, InStock: true},
					{Title: "代码整洁之道", Author: "Robert C. Martin", Price: 59.00, Tags: []string{"编程", "最佳实践"}, InStock: true},
				},
			}
		case "3":
			resp = APIResponse{
				Page:  3,
				Total: 3,
				Count: 2,
				// 最后一页没有 NextPage
				Books: []Book{
					{Title: "UNIX 编程艺术", Author: "Eric S. Raymond", Price: 89.00, Tags: []string{"UNIX", "编程哲学"}, InStock: false},
					{Title: "编程珠玑", Author: "Jon Bentley", Price: 45.00, Tags: []string{"算法", "编程技巧"}, InStock: true},
				},
			}
		default:
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"error": "page not found"})
			return
		}

		json.NewEncoder(w).Encode(resp)
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// JsonFilePipeline — 将 Item 保存到 JSON 文件
// ============================================================================

// JsonFilePipeline 将收到的 Item 收集起来，在 Close 时写入 JSON 文件。
type JsonFilePipeline struct {
	Path  string
	mu    sync.Mutex
	items []any
}

func (p *JsonFilePipeline) Open(ctx context.Context) error {
	p.items = make([]any, 0)
	return nil
}

func (p *JsonFilePipeline) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := json.MarshalIndent(p.items, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}

	if err := os.WriteFile(p.Path, data, 0644); err != nil {
		return fmt.Errorf("写入文件 %s 失败: %w", p.Path, err)
	}

	fmt.Printf("\n📁 已将 %d 条数据保存到 %s\n", len(p.items), p.Path)
	return nil
}

func (p *JsonFilePipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = append(p.items, item)
	return item, nil
}

// ============================================================================
// CleanPipeline — 数据清洗 Pipeline
// ============================================================================

// CleanPipeline 对 Book 数据进行清洗：
//   - 为每本书添加 page_url 字段（来源页面）
//   - 丢弃缺少 title 或 author 的无效数据
type CleanPipeline struct{}

func (p *CleanPipeline) Open(ctx context.Context) error  { return nil }
func (p *CleanPipeline) Close(ctx context.Context) error { return nil }

func (p *CleanPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	book, ok := item.(*Book)
	if !ok {
		// 非 Book 类型，原样传递
		return item, nil
	}

	// 验证必填字段
	if book.Title == "" || book.Author == "" {
		return nil, fmt.Errorf("drop item: 缺少 title 或 author: %+v", book)
	}

	return book, nil
}

// ============================================================================
// BooksSpider — 图书爬虫
// ============================================================================

// BooksSpider 从本地 JSON API 爬取图书数据。
type BooksSpider struct {
	spider.Base
}

func NewBooksSpider(baseURL string) *BooksSpider {
	return &BooksSpider{
		Base: spider.Base{
			SpiderName: "books",
			StartURLs:  []string{baseURL + "/api/books?page=1"},
		},
	}
}

// Parse 解析 JSON API 响应，提取图书数据和下一页链接。
func (s *BooksSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error) {
	var apiResp APIResponse
	if err := response.JSON(&apiResp); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	var outputs []spider.Output

	// 提取每本书作为 Item
	for i := range apiResp.Books {
		book := apiResp.Books[i]
		book.PageURL = response.URL.String()
		outputs = append(outputs, spider.Output{Item: &book})
	}

	// 如果有下一页，生成新请求
	if apiResp.NextPage != "" {
		nextURL, err := response.URLJoin(apiResp.NextPage)
		if err == nil {
			req, _ := scrapy_http.NewRequest(nextURL)
			outputs = append(outputs, spider.Output{Request: req})
		}
	}

	return outputs, nil
}

func (s *BooksSpider) CustomSettings() *spider.Settings {
	return &spider.Settings{
		ConcurrentRequests: spider.IntPtr(1), // JSON API 按顺序爬取
		DownloadDelay:      spider.DurationPtr(time.Second * 10),
		LogLevel:           spider.StringPtr("DEBUG"),
	}
}

// ============================================================================
// 主函数
// ============================================================================

func main() {
	// 1. 启动本地 JSON API 服务器
	api := newLocalBookAPI()
	defer api.Close()
	fmt.Printf("📡 本地 JSON API 已启动: %s/api/books\n\n", api.URL)

	// 2. 创建 Spider
	sp := NewBooksSpider(api.URL)

	// 3. 定义输出文件路径
	outputPath := "books_output.json"

	// 4. 创建 Crawler，注册 Pipeline 链
	c := crawler.NewDefault()
	c.AddPipeline(&CleanPipeline{}, "Clean", 100)                       // 优先级 100：先清洗
	c.AddPipeline(&JsonFilePipeline{Path: outputPath}, "JsonFile", 300) // 优先级 300：后保存

	// 5. 运行爬虫
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("🚀 开始爬取图书数据...")
	fmt.Println("=" + repeat("=", 59))

	err := c.Run(ctx, sp)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("❌ 爬取出错: %v\n", err)
		os.Exit(1)
	}

	// 6. 读取并展示输出文件内容
	fmt.Println("=" + repeat("=", 59))
	fmt.Printf("\n📖 输出文件内容 (%s):\n\n", outputPath)

	data, err := os.ReadFile(outputPath)
	if err != nil {
		fmt.Printf("❌ 读取输出文件失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))

	// 7. 清理输出文件
	os.Remove(outputPath)
	fmt.Println("🧹 已清理输出文件")
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
