package http

import (
	"testing"

	"github.com/tidwall/gjson"
)

// 测试用 JSON 数据
var testJSONBody = []byte(`{
	"code": 200,
	"message": "success",
	"data": {
		"name": "scrapy-go",
		"version": "1.2.0",
		"items": [
			{"id": 1, "name": "item1", "price": 50.5, "active": true},
			{"id": 2, "name": "item2", "price": 150.0, "active": false},
			{"id": 3, "name": "item3", "price": 200.0, "active": true}
		],
		"config": {
			"timeout": 30,
			"retries": 3,
			"debug": false
		},
		"tags": ["web", "crawler", "go"],
		"nested": {
			"deep": {
				"value": "found"
			}
		}
	}
}`)

func newTestJSONResponse() *Response {
	return MustNewResponse("http://example.com/api", 200, WithResponseBody(testJSONBody))
}

// ============================================================================
// JSONGet 测试
// ============================================================================

func TestJSONGet_DotPath(t *testing.T) {
	resp := newTestJSONResponse()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"顶层字段", "code", "200"},
		{"顶层字符串", "message", "success"},
		{"嵌套字段", "data.name", "scrapy-go"},
		{"深层嵌套", "data.nested.deep.value", "found"},
		{"数组索引", "data.items.0.name", "item1"},
		{"数组第二个元素", "data.items.1.name", "item2"},
		{"数组最后一个", "data.items.2.price", "200"},
		{"对象字段", "data.config.timeout", "30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resp.JSONGet(tt.path)
			if !result.Exists() {
				t.Fatalf("路径 %q 不存在", tt.path)
			}
			if result.String() != tt.expected {
				t.Errorf("JSONGet(%q) = %q, 期望 %q", tt.path, result.String(), tt.expected)
			}
		})
	}
}

func TestJSONGet_TypedAccess(t *testing.T) {
	resp := newTestJSONResponse()

	// 整数
	code := resp.JSONGet("code")
	if code.Int() != 200 {
		t.Errorf("Int() = %d, 期望 200", code.Int())
	}

	// 浮点数
	price := resp.JSONGet("data.items.0.price")
	if price.Float() != 50.5 {
		t.Errorf("Float() = %f, 期望 50.5", price.Float())
	}

	// 布尔值
	active := resp.JSONGet("data.items.0.active")
	if !active.Bool() {
		t.Error("Bool() = false, 期望 true")
	}

	// 字符串
	name := resp.JSONGet("data.name")
	if name.String() != "scrapy-go" {
		t.Errorf("String() = %q, 期望 %q", name.String(), "scrapy-go")
	}
}

func TestJSONGet_ArrayProjection(t *testing.T) {
	resp := newTestJSONResponse()

	// 投影：提取所有 items 的 name
	result := resp.JSONGet("data.items.#.name")
	if !result.Exists() {
		t.Fatal("投影路径不存在")
	}

	names := result.Array()
	if len(names) != 3 {
		t.Fatalf("投影结果长度 = %d, 期望 3", len(names))
	}

	expected := []string{"item1", "item2", "item3"}
	for i, name := range names {
		if name.String() != expected[i] {
			t.Errorf("names[%d] = %q, 期望 %q", i, name.String(), expected[i])
		}
	}
}

func TestJSONGet_ConditionalFilter(t *testing.T) {
	resp := newTestJSONResponse()

	// 过滤：price > 100 的 items 的 name
	result := resp.JSONGet(`data.items.#(price>100)#.name`)
	if !result.Exists() {
		t.Fatal("过滤路径不存在")
	}

	names := result.Array()
	if len(names) != 2 {
		t.Fatalf("过滤结果长度 = %d, 期望 2", len(names))
	}

	expected := []string{"item2", "item3"}
	for i, name := range names {
		if name.String() != expected[i] {
			t.Errorf("filtered[%d] = %q, 期望 %q", i, name.String(), expected[i])
		}
	}
}

func TestJSONGet_PathNotExists(t *testing.T) {
	resp := newTestJSONResponse()

	result := resp.JSONGet("data.nonexistent.field")
	if result.Exists() {
		t.Error("不存在的路径应返回 Exists() = false")
	}

	// 不存在的路径返回零值
	if result.String() != "" {
		t.Errorf("不存在路径的 String() = %q, 期望空字符串", result.String())
	}
	if result.Int() != 0 {
		t.Errorf("不存在路径的 Int() = %d, 期望 0", result.Int())
	}
}

func TestJSONGet_EmptyBody(t *testing.T) {
	resp := MustNewResponse("http://example.com/api", 200, WithResponseBody(nil))

	result := resp.JSONGet("data.name")
	if result.Exists() {
		t.Error("空 body 应返回 Exists() = false")
	}
}

func TestJSONGet_NonJSONBody(t *testing.T) {
	resp := MustNewResponse("http://example.com/page", 200,
		WithResponseBody([]byte("<html><body>Hello</body></html>")))

	result := resp.JSONGet("data.name")
	if result.Exists() {
		t.Error("非 JSON body 应返回 Exists() = false")
	}
}

func TestJSONGet_EmptyJSONObject(t *testing.T) {
	resp := MustNewResponse("http://example.com/api", 200,
		WithResponseBody([]byte("{}")))

	result := resp.JSONGet("data")
	if result.Exists() {
		t.Error("空 JSON 对象中不存在的路径应返回 Exists() = false")
	}
}

func TestJSONGet_EmptyJSONArray(t *testing.T) {
	resp := MustNewResponse("http://example.com/api", 200,
		WithResponseBody([]byte("[]")))

	result := resp.JSONGet("0")
	if result.Exists() {
		t.Error("空 JSON 数组中不存在的索引应返回 Exists() = false")
	}
}

func TestJSONGet_ArrayCount(t *testing.T) {
	resp := newTestJSONResponse()

	// # 返回数组长度
	result := resp.JSONGet("data.items.#")
	if result.Int() != 3 {
		t.Errorf("数组长度 = %d, 期望 3", result.Int())
	}
}

// ============================================================================
// JSONGetMany 测试
// ============================================================================

func TestJSONGetMany_Basic(t *testing.T) {
	resp := newTestJSONResponse()

	results := resp.JSONGetMany("data.name", "data.version", "code")
	if len(results) != 3 {
		t.Fatalf("结果长度 = %d, 期望 3", len(results))
	}

	if results[0].String() != "scrapy-go" {
		t.Errorf("results[0] = %q, 期望 %q", results[0].String(), "scrapy-go")
	}
	if results[1].String() != "1.2.0" {
		t.Errorf("results[1] = %q, 期望 %q", results[1].String(), "1.2.0")
	}
	if results[2].Int() != 200 {
		t.Errorf("results[2] = %d, 期望 200", results[2].Int())
	}
}

func TestJSONGetMany_PartialExists(t *testing.T) {
	resp := newTestJSONResponse()

	results := resp.JSONGetMany("data.name", "data.nonexistent", "code")
	if len(results) != 3 {
		t.Fatalf("结果长度 = %d, 期望 3", len(results))
	}

	if !results[0].Exists() {
		t.Error("results[0] 应存在")
	}
	if results[1].Exists() {
		t.Error("results[1] 不应存在")
	}
	if !results[2].Exists() {
		t.Error("results[2] 应存在")
	}
}

func TestJSONGetMany_EmptyPaths(t *testing.T) {
	resp := newTestJSONResponse()

	results := resp.JSONGetMany()
	if len(results) != 0 {
		t.Errorf("空路径列表应返回空结果，实际长度 = %d", len(results))
	}
}

func TestJSONGetMany_EmptyBody(t *testing.T) {
	resp := MustNewResponse("http://example.com/api", 200, WithResponseBody(nil))

	results := resp.JSONGetMany("data.name", "code")
	if len(results) != 2 {
		t.Fatalf("结果长度 = %d, 期望 2", len(results))
	}

	for i, r := range results {
		if r.Exists() {
			t.Errorf("results[%d] 在空 body 中不应存在", i)
		}
	}
}

// ============================================================================
// JSONExists 测试
// ============================================================================

func TestJSONExists_ExistingPath(t *testing.T) {
	resp := newTestJSONResponse()

	tests := []struct {
		path   string
		exists bool
	}{
		{"code", true},
		{"data", true},
		{"data.name", true},
		{"data.items", true},
		{"data.items.0", true},
		{"data.config.debug", true},
		{"data.nested.deep.value", true},
		{"nonexistent", false},
		{"data.nonexistent", false},
		{"data.items.99", false},
		{"data.items.0.nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := resp.JSONExists(tt.path); got != tt.exists {
				t.Errorf("JSONExists(%q) = %v, 期望 %v", tt.path, got, tt.exists)
			}
		})
	}
}

func TestJSONExists_EmptyBody(t *testing.T) {
	resp := MustNewResponse("http://example.com/api", 200, WithResponseBody(nil))

	if resp.JSONExists("data") {
		t.Error("空 body 中 JSONExists 应返回 false")
	}
}

func TestJSONExists_NonJSONBody(t *testing.T) {
	resp := MustNewResponse("http://example.com/page", 200,
		WithResponseBody([]byte("not json content")))

	if resp.JSONExists("data") {
		t.Error("非 JSON body 中 JSONExists 应返回 false")
	}
}

func TestJSONExists_BoolFalseValue(t *testing.T) {
	// 值为 false 的布尔字段应该存在
	resp := newTestJSONResponse()

	if !resp.JSONExists("data.config.debug") {
		t.Error("值为 false 的字段应该存在")
	}
	if !resp.JSONExists("data.items.1.active") {
		t.Error("值为 false 的数组元素字段应该存在")
	}
}

func TestJSONExists_NullValue(t *testing.T) {
	resp := MustNewResponse("http://example.com/api", 200,
		WithResponseBody([]byte(`{"data": null}`)))

	// gjson 中 null 值 Exists() 返回 true
	if !resp.JSONExists("data") {
		t.Error("null 值字段应该存在")
	}
}

// ============================================================================
// JSONForEach 测试
// ============================================================================

func TestJSONForEach_Array(t *testing.T) {
	resp := newTestJSONResponse()

	var names []string
	resp.JSONForEach("data.items", func(key, value gjson.Result) bool {
		names = append(names, value.Get("name").String())
		return true
	})

	if len(names) != 3 {
		t.Fatalf("遍历数组长度 = %d, 期望 3", len(names))
	}

	expected := []string{"item1", "item2", "item3"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("names[%d] = %q, 期望 %q", i, name, expected[i])
		}
	}
}

func TestJSONForEach_Object(t *testing.T) {
	resp := newTestJSONResponse()

	config := make(map[string]string)
	resp.JSONForEach("data.config", func(key, value gjson.Result) bool {
		config[key.String()] = value.String()
		return true
	})

	if len(config) != 3 {
		t.Fatalf("遍历对象长度 = %d, 期望 3", len(config))
	}

	if config["timeout"] != "30" {
		t.Errorf("config[timeout] = %q, 期望 %q", config["timeout"], "30")
	}
	if config["retries"] != "3" {
		t.Errorf("config[retries] = %q, 期望 %q", config["retries"], "3")
	}
	if config["debug"] != "false" {
		t.Errorf("config[debug] = %q, 期望 %q", config["debug"], "false")
	}
}

func TestJSONForEach_EarlyTermination(t *testing.T) {
	resp := newTestJSONResponse()

	var count int
	resp.JSONForEach("data.items", func(key, value gjson.Result) bool {
		count++
		return count < 2 // 只处理前 2 个元素后终止
	})

	if count != 2 {
		t.Errorf("提前终止后遍历次数 = %d, 期望 2", count)
	}
}

func TestJSONForEach_EmptyArray(t *testing.T) {
	resp := MustNewResponse("http://example.com/api", 200,
		WithResponseBody([]byte(`{"items": []}`)))

	var count int
	resp.JSONForEach("items", func(key, value gjson.Result) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("空数组遍历次数 = %d, 期望 0", count)
	}
}

func TestJSONForEach_EmptyObject(t *testing.T) {
	resp := MustNewResponse("http://example.com/api", 200,
		WithResponseBody([]byte(`{"config": {}}`)))

	var count int
	resp.JSONForEach("config", func(key, value gjson.Result) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("空对象遍历次数 = %d, 期望 0", count)
	}
}

func TestJSONForEach_NonExistentPath(t *testing.T) {
	resp := newTestJSONResponse()

	var count int
	resp.JSONForEach("data.nonexistent", func(key, value gjson.Result) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("不存在路径遍历次数 = %d, 期望 0", count)
	}
}

func TestJSONForEach_EmptyBody(t *testing.T) {
	resp := MustNewResponse("http://example.com/api", 200, WithResponseBody(nil))

	var count int
	resp.JSONForEach("data.items", func(key, value gjson.Result) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("空 body 遍历次数 = %d, 期望 0", count)
	}
}

func TestJSONForEach_NonJSONBody(t *testing.T) {
	resp := MustNewResponse("http://example.com/page", 200,
		WithResponseBody([]byte("<html></html>")))

	var count int
	resp.JSONForEach("data", func(key, value gjson.Result) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("非 JSON body 遍历次数 = %d, 期望 0", count)
	}
}

func TestJSONForEach_StringArray(t *testing.T) {
	resp := newTestJSONResponse()

	var tags []string
	resp.JSONForEach("data.tags", func(key, value gjson.Result) bool {
		tags = append(tags, value.String())
		return true
	})

	expected := []string{"web", "crawler", "go"}
	if len(tags) != len(expected) {
		t.Fatalf("tags 长度 = %d, 期望 %d", len(tags), len(expected))
	}
	for i, tag := range tags {
		if tag != expected[i] {
			t.Errorf("tags[%d] = %q, 期望 %q", i, tag, expected[i])
		}
	}
}

// ============================================================================
// 综合场景测试
// ============================================================================

func TestJSON_CoexistenceWithJSONMethod(t *testing.T) {
	// 验证 JSONGet 与现有 JSON(v any) error 方法互不干扰
	resp := newTestJSONResponse()

	// 使用 JSON() 整体反序列化
	var data map[string]any
	if err := resp.JSON(&data); err != nil {
		t.Fatalf("JSON() 反序列化失败: %v", err)
	}
	if data["code"].(float64) != 200 {
		t.Error("JSON() 反序列化结果不正确")
	}

	// 使用 JSONGet 路径查询
	result := resp.JSONGet("code")
	if result.Int() != 200 {
		t.Error("JSONGet 路径查询结果不正确")
	}
}

func TestJSON_RealWorldAPIResponse(t *testing.T) {
	// 模拟真实 API 响应场景
	apiBody := []byte(`{
		"status": "ok",
		"pagination": {
			"page": 1,
			"per_page": 20,
			"total": 100,
			"total_pages": 5
		},
		"results": [
			{
				"id": "abc123",
				"title": "Go 并发编程",
				"author": {"name": "张三", "email": "zhangsan@example.com"},
				"tags": ["go", "concurrency"],
				"stats": {"views": 1500, "likes": 42}
			},
			{
				"id": "def456",
				"title": "Web 爬虫实战",
				"author": {"name": "李四", "email": "lisi@example.com"},
				"tags": ["web", "crawler", "python"],
				"stats": {"views": 3200, "likes": 89}
			}
		]
	}`)

	resp := MustNewResponse("http://api.example.com/articles", 200,
		WithResponseBody(apiBody))

	// 检查分页信息
	if resp.JSONGet("pagination.total").Int() != 100 {
		t.Error("分页总数不正确")
	}

	// 提取所有文章标题
	titles := resp.JSONGet("results.#.title").Array()
	if len(titles) != 2 {
		t.Fatalf("文章数量 = %d, 期望 2", len(titles))
	}
	if titles[0].String() != "Go 并发编程" {
		t.Errorf("第一篇标题 = %q", titles[0].String())
	}

	// 提取所有作者邮箱
	emails := resp.JSONGet("results.#.author.email").Array()
	if len(emails) != 2 {
		t.Fatalf("邮箱数量 = %d, 期望 2", len(emails))
	}

	// 过滤 views > 2000 的文章
	popular := resp.JSONGet(`results.#(stats.views>2000)#.title`).Array()
	if len(popular) != 1 {
		t.Fatalf("热门文章数量 = %d, 期望 1", len(popular))
	}
	if popular[0].String() != "Web 爬虫实战" {
		t.Errorf("热门文章标题 = %q", popular[0].String())
	}

	// 使用 JSONGetMany 一次提取多个字段
	results := resp.JSONGetMany("status", "pagination.page", "pagination.total_pages")
	if results[0].String() != "ok" {
		t.Error("status 不正确")
	}
	if results[1].Int() != 1 {
		t.Error("page 不正确")
	}
	if results[2].Int() != 5 {
		t.Error("total_pages 不正确")
	}

	// 使用 JSONForEach 流式遍历
	var articleIDs []string
	resp.JSONForEach("results", func(key, value gjson.Result) bool {
		articleIDs = append(articleIDs, value.Get("id").String())
		return true
	})
	if len(articleIDs) != 2 {
		t.Fatalf("文章 ID 数量 = %d, 期望 2", len(articleIDs))
	}
	if articleIDs[0] != "abc123" || articleIDs[1] != "def456" {
		t.Errorf("文章 ID 不正确: %v", articleIDs)
	}
}

func TestJSON_LargeNestedStructure(t *testing.T) {
	// 测试深层嵌套结构
	body := []byte(`{
		"level1": {
			"level2": {
				"level3": {
					"level4": {
						"level5": {
							"value": "deep_nested"
						}
					}
				}
			}
		}
	}`)

	resp := MustNewResponse("http://example.com/api", 200, WithResponseBody(body))

	result := resp.JSONGet("level1.level2.level3.level4.level5.value")
	if !result.Exists() {
		t.Fatal("深层嵌套路径不存在")
	}
	if result.String() != "deep_nested" {
		t.Errorf("深层嵌套值 = %q, 期望 %q", result.String(), "deep_nested")
	}
}

func TestJSON_SpecialCharacters(t *testing.T) {
	// 测试包含特殊字符的 JSON
	body := []byte(`{
		"data": {
			"message": "Hello \"World\"",
			"path": "/usr/local/bin",
			"unicode": "你好世界",
			"emoji": "🕷️"
		}
	}`)

	resp := MustNewResponse("http://example.com/api", 200, WithResponseBody(body))

	if resp.JSONGet("data.message").String() != `Hello "World"` {
		t.Errorf("转义字符处理不正确: %q", resp.JSONGet("data.message").String())
	}
	if resp.JSONGet("data.unicode").String() != "你好世界" {
		t.Error("Unicode 处理不正确")
	}
	if resp.JSONGet("data.emoji").String() != "🕷️" {
		t.Error("Emoji 处理不正确")
	}
}

// ============================================================================
// Benchmark 测试
// ============================================================================

func BenchmarkJSONGet(b *testing.B) {
	resp := newTestJSONResponse()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp.JSONGet("data.items.0.name")
	}
}

func BenchmarkJSONGetMany(b *testing.B) {
	resp := newTestJSONResponse()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp.JSONGetMany("data.name", "data.version", "code", "data.items.0.name")
	}
}

func BenchmarkJSONExists(b *testing.B) {
	resp := newTestJSONResponse()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp.JSONExists("data.items.0.name")
	}
}

func BenchmarkJSONForEach(b *testing.B) {
	resp := newTestJSONResponse()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp.JSONForEach("data.items", func(key, value gjson.Result) bool {
			_ = value.Get("name").String()
			return true
		})
	}
}

func BenchmarkJSONGet_vs_JSONUnmarshal(b *testing.B) {
	resp := newTestJSONResponse()

	b.Run("JSONGet", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp.JSONGet("data.items.0.name")
		}
	})

	b.Run("JSON_Unmarshal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var data map[string]any
			_ = resp.JSON(&data)
		}
	})
}
