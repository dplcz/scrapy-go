package item_test

import (
	"fmt"

	"github.com/dplcz/scrapy-go/pkg/item"
)

// ExampleAdapt 演示自动适配不同类型的 Item。
func ExampleAdapt() {
	// 适配 map 类型
	mapItem := map[string]any{
		"title": "Go Programming",
		"price": 29.99,
	}
	adapter := item.Adapt(mapItem)
	fmt.Println("Fields:", adapter.FieldNames())

	title, _ := adapter.GetField("title")
	fmt.Println("Title:", title)

	// Output:
	// Fields: [price title]
	// Title: Go Programming
}

// ExampleAdapt_struct 演示适配 struct 类型的 Item。
func ExampleAdapt_struct() {
	type Book struct {
		Title  string  `item:"title"`
		Price  float64 `item:"price"`
		Author string  `item:"author"`
	}

	book := &Book{Title: "Go in Action", Price: 39.99, Author: "William Kennedy"}
	adapter := item.Adapt(book)

	fmt.Println("Fields:", adapter.FieldNames())
	fmt.Println("Len:", adapter.Len())

	m := adapter.AsMap()
	fmt.Println("Title:", m["title"])

	// Output:
	// Fields: [title price author]
	// Len: 3
	// Title: Go in Action
}

// ExampleIsItem 演示判断值是否可被适配。
func ExampleIsItem() {
	fmt.Println("map:", item.IsItem(map[string]any{"k": "v"}))
	fmt.Println("string:", item.IsItem("hello"))
	fmt.Println("nil:", item.IsItem(nil))

	type Product struct {
		Name string `item:"name"`
	}
	fmt.Println("struct:", item.IsItem(&Product{}))

	// Output:
	// map: true
	// string: false
	// nil: false
	// struct: true
}

// ExampleAsMap 演示将 Item 转为 map。
func ExampleAsMap() {
	data := map[string]any{
		"url":    "https://example.com",
		"status": 200,
	}

	m := item.AsMap(data)
	fmt.Println("url:", m["url"])
	fmt.Println("status:", m["status"])

	// Output:
	// url: https://example.com
	// status: 200
}
