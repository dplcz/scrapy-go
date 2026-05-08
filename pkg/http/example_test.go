package http_test

import (
	"fmt"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// ExampleNewRequest 演示创建基本的 HTTP 请求。
func ExampleNewRequest() {
	// 创建 GET 请求
	req, err := shttp.NewRequest("https://example.com")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println(req.Method, req.URL)

	// 创建带选项的 POST 请求
	req2, err := shttp.NewRequest("https://api.example.com/data",
		shttp.WithMethod("POST"),
		shttp.WithHeader("Content-Type", "application/json"),
		shttp.WithBody([]byte(`{"key":"value"}`)),
		shttp.WithPriority(10),
	)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println(req2.Method, req2.URL)

	// Output:
	// GET https://example.com
	// POST https://api.example.com/data
}

// ExampleNewFormRequest 演示创建表单请求。
func ExampleNewFormRequest() {
	// POST 表单
	req, err := shttp.NewFormRequest("https://example.com/login",
		map[string][]string{
			"username": {"admin"},
			"password": {"secret"},
		},
	)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println(req.Method)
	fmt.Println(req.Headers.Get("Content-Type"))

	// Output:
	// POST
	// application/x-www-form-urlencoded
}

// ExampleNewJSONRequest 演示创建 JSON API 请求。
func ExampleNewJSONRequest() {
	data := map[string]any{
		"name":  "scrapy-go",
		"stars": 1000,
	}

	req, err := shttp.NewJSONRequest("https://api.example.com/repos", data)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println(req.Method)
	fmt.Println(req.Headers.Get("Content-Type"))
	fmt.Println(string(req.Body))

	// Output:
	// POST
	// application/json
	// {"name":"scrapy-go","stars":1000}
}

// ExampleFromCURL 演示从 curl 命令创建请求。
func ExampleFromCURL() {
	req, err := shttp.FromCURL(`curl 'https://api.example.com/data' -H 'Authorization: Bearer token123' -H 'Accept: application/json'`)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println(req.Method, req.URL)
	fmt.Println("Auth:", req.Headers.Get("Authorization"))

	// Output:
	// GET https://api.example.com/data
	// Auth: Bearer token123
}

// ExampleResponse_CSS 演示使用 Response 的 CSS 选择器。
func ExampleResponse_CSS() {
	html := []byte(`<html><body>
		<h1 class="title">Hello World</h1>
		<a href="/page/2">Next</a>
		<a href="/page/3">Last</a>
	</body></html>`)

	resp := shttp.MustNewResponse("https://example.com", 200,
		shttp.WithResponseBody(html),
	)

	// CSS 选择器提取文本
	title := resp.CSS("h1.title").Get("")
	fmt.Println("Title:", title)

	// CSS 属性提取
	links := resp.CSSAttr("a", "href").GetAll()
	fmt.Println("Links:", links)

	// Output:
	// Title: Hello World
	// Links: [/page/2 /page/3]
}

// ExampleResponse_Follow 演示使用 Response.Follow 跟踪链接。
func ExampleResponse_Follow() {
	resp := shttp.MustNewResponse("https://example.com/page/1", 200)

	// 跟踪相对链接
	req, err := resp.Follow("/page/2")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println(req.URL)

	// Output:
	// https://example.com/page/2
}

// ExampleNewHeaders 演示创建和操作 HTTP Headers。
func ExampleNewHeaders() {
	headers := shttp.NewHeaders(map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer token",
	})

	fmt.Println(shttp.GetContentType(headers))
	fmt.Println(shttp.IsTextContentType(headers))

	// Output:
	// application/json
	// true
}

// ExampleCallbackRegistry 演示回调注册表的使用。
func ExampleCallbackRegistry() {
	registry := shttp.NewCallbackRegistry()

	// 手动注册回调
	registry.Register("ParseDetail", func() {})
	registry.Register("ParseList", func() {})

	// 查找回调
	_, found := registry.Lookup("ParseDetail")
	fmt.Println("ParseDetail found:", found)

	_, found = registry.Lookup("Unknown")
	fmt.Println("Unknown found:", found)

	fmt.Println("Total callbacks:", registry.Len())

	// Output:
	// ParseDetail found: true
	// Unknown found: false
	// Total callbacks: 2
}
