// 示例爬虫：使用 JSONGet/JSONGetMany/JSONForEach 从 JSON API 提取深层嵌套字段。
//
// 本示例演示：
//  1. 本地 httptest 服务器提供嵌套 JSON API（模拟电商商品数据）
//  2. 使用 response.JSONGet 提取深层嵌套字段（零分配路径查询）
//  3. 使用 response.JSONGetMany 一次提取多个字段
//  4. 使用 response.JSONForEach 流式遍历大数组
//  5. 使用 response.JSONExists 条件判断
//  6. 与传统 response.JSON() 整体反序列化的对比
//
// 运行方式：go run examples/json_api/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/tidwall/gjson"
)

// ============================================================================
// 本地 JSON API 服务器（模拟电商 API）
// ============================================================================

func newMockServer() *httptest.Server {
	mux := http.NewServeMux()

	// 商品列表 API
	mux.HandleFunc("/api/products", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"pagination": map[string]any{
				"page":        1,
				"per_page":    10,
				"total":       3,
				"total_pages": 1,
			},
			"data": map[string]any{
				"products": []map[string]any{
					{
						"id":    "prod-001",
						"name":  "Go 语言圣经",
						"price": 89.0,
						"category": map[string]any{
							"id":   "cat-books",
							"name": "技术图书",
						},
						"tags":     []string{"go", "programming", "book"},
						"in_stock": true,
						"seller": map[string]any{
							"name":   "优质书店",
							"rating": 4.8,
						},
					},
					{
						"id":    "prod-002",
						"name":  "机械键盘 Pro",
						"price": 599.0,
						"category": map[string]any{
							"id":   "cat-electronics",
							"name": "电子产品",
						},
						"tags":     []string{"keyboard", "mechanical", "gaming"},
						"in_stock": true,
						"seller": map[string]any{
							"name":   "数码旗舰店",
							"rating": 4.5,
						},
					},
					{
						"id":    "prod-003",
						"name":  "人体工学椅",
						"price": 1299.0,
						"category": map[string]any{
							"id":   "cat-furniture",
							"name": "办公家具",
						},
						"tags":     []string{"chair", "ergonomic", "office"},
						"in_stock": false,
						"seller": map[string]any{
							"name":   "家具工厂",
							"rating": 4.2,
						},
					},
				},
			},
		})
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// Spider 定义
// ============================================================================

// JSONAPISpider 演示使用 gjson 路径查询从 JSON API 提取数据。
type JSONAPISpider struct {
	spider.Base
}

func NewJSONAPISpider(baseURL string) *JSONAPISpider {
	return &JSONAPISpider{
		Base: spider.Base{
			SpiderName: "json_api_spider",
			StartURLs:  []string{baseURL + "/api/products"},
		},
	}
}

func (s *JSONAPISpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	fmt.Println("=" + strings.Repeat("=", 59))
	fmt.Println("📡 Response JSON 选择器（gjson 集成）示例")
	fmt.Println("=" + strings.Repeat("=", 59))

	// ========================================================================
	// 1. JSONExists — 检查字段是否存在
	// ========================================================================
	fmt.Println("\n🔍 [JSONExists] 检查字段存在性：")
	fmt.Printf("  status 存在: %v\n", response.JSONExists("status"))
	fmt.Printf("  error 存在:  %v\n", response.JSONExists("error"))
	fmt.Printf("  data.products 存在: %v\n", response.JSONExists("data.products"))

	// ========================================================================
	// 2. JSONGet — 深层嵌套字段提取
	// ========================================================================
	fmt.Println("\n📦 [JSONGet] 深层嵌套字段提取：")

	// 提取分页信息
	total := response.JSONGet("pagination.total")
	fmt.Printf("  商品总数: %d\n", total.Int())

	// 提取第一个商品的卖家评分（深层嵌套）
	rating := response.JSONGet("data.products.0.seller.rating")
	fmt.Printf("  第一个商品卖家评分: %.1f\n", rating.Float())

	// 数组投影：提取所有商品名称
	names := response.JSONGet("data.products.#.name")
	fmt.Printf("  所有商品名称: %v\n", names.Array())

	// 条件过滤：价格 > 100 的商品名称
	expensive := response.JSONGet(`data.products.#(price>100)#.name`)
	fmt.Printf("  价格>100的商品: %v\n", expensive.Array())

	// 条件过滤：有库存的商品
	inStock := response.JSONGet(`data.products.#(in_stock==true)#.name`)
	fmt.Printf("  有库存的商品: %v\n", inStock.Array())

	// ========================================================================
	// 3. JSONGetMany — 一次提取多个字段
	// ========================================================================
	fmt.Println("\n⚡ [JSONGetMany] 一次扫描多路径提取：")

	results := response.JSONGetMany(
		"status",
		"pagination.page",
		"pagination.total_pages",
		"data.products.#",
	)
	fmt.Printf("  状态: %s\n", results[0].String())
	fmt.Printf("  当前页: %d\n", results[1].Int())
	fmt.Printf("  总页数: %d\n", results[2].Int())
	fmt.Printf("  商品数量: %d\n", results[3].Int())

	// ========================================================================
	// 4. JSONForEach — 流式遍历（避免大数组一次性分配）
	// ========================================================================
	fmt.Println("\n🔄 [JSONForEach] 流式遍历商品列表：")

	response.JSONForEach("data.products", func(key, value gjson.Result) bool {
		name := value.Get("name").String()
		price := value.Get("price").Float()
		category := value.Get("category.name").String()
		seller := value.Get("seller.name").String()

		fmt.Printf("  [%s] %s — ¥%.0f（分类: %s, 卖家: %s）\n",
			value.Get("id").String(), name, price, category, seller)
		return true
	})

	// ========================================================================
	// 5. 与 JSON() 整体反序列化的对比
	// ========================================================================
	fmt.Println("\n📊 [对比] JSONGet vs JSON() 整体反序列化：")
	fmt.Println("  • JSON(v any) — 适合需要完整结构化数据的场景（如映射到 struct）")
	fmt.Println("  • JSONGet(path) — 适合只需提取少量深层字段的场景（零分配、高性能）")
	fmt.Println("  • JSONGetMany — 适合一次提取多个字段（单次扫描，性能优于多次 JSONGet）")
	fmt.Println("  • JSONForEach — 适合遍历大数组（流式处理，避免一次性分配）")

	fmt.Println("\n✅ 示例完成")
	return nil, nil
}

// ============================================================================
// 主函数
// ============================================================================

func main() {
	// 启动模拟服务器
	server := newMockServer()
	defer server.Close()

	// 创建并运行爬虫
	c := crawler.NewDefault()
	c.Run(context.Background(), NewJSONAPISpider(server.URL))
}
