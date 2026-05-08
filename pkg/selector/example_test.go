package selector_test

import (
	"fmt"

	"github.com/dplcz/scrapy-go/pkg/selector"
)

// ExampleNewFromBytes 演示从 HTML 字节创建 Selector 并执行查询。
func ExampleNewFromBytes() {
	html := []byte(`<html><body>
		<h1 class="title">Hello World</h1>
		<ul>
			<li class="item">Item 1</li>
			<li class="item">Item 2</li>
			<li class="item">Item 3</li>
		</ul>
	</body></html>`)

	sel := selector.NewFromBytes(html)

	// CSS 选择器
	title := sel.CSS("h1.title").Get("")
	fmt.Println("Title:", title)

	// 获取所有列表项
	items := sel.CSS("li.item").GetAll()
	fmt.Println("Items:", items)

	// Output:
	// Title: Hello World
	// Items: [Item 1 Item 2 Item 3]
}

// ExampleSelector_CSSAttr 演示使用 CSS 选择器提取属性。
func ExampleSelector_CSSAttr() {
	html := []byte(`<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
		<a href="/page/3">Page 3</a>
	</body></html>`)

	sel := selector.NewFromBytes(html)

	// 提取所有链接的 href 属性
	links := sel.CSSAttr("a", "href").GetAll()
	fmt.Println("Links:", links)

	// 获取第一个链接
	first := sel.CSSAttr("a", "href").Get("")
	fmt.Println("First:", first)

	// Output:
	// Links: [/page/1 /page/2 /page/3]
	// First: /page/1
}

// ExampleSelector_XPath 演示使用 XPath 选择器。
func ExampleSelector_XPath() {
	html := []byte(`<html><body>
		<div class="quote">
			<span class="text">Quote 1</span>
			<span class="author">Author 1</span>
		</div>
		<div class="quote">
			<span class="text">Quote 2</span>
			<span class="author">Author 2</span>
		</div>
	</body></html>`)

	sel := selector.NewFromBytes(html)

	// XPath 查询
	texts := sel.XPath("//span[@class='text']").GetAll()
	fmt.Println("Texts:", texts)

	authors := sel.XPath("//span[@class='author']").GetAll()
	fmt.Println("Authors:", authors)

	// Output:
	// Texts: [Quote 1 Quote 2]
	// Authors: [Author 1 Author 2]
}

// ExampleList_CSS 演示 List 的链式查询。
func ExampleList_CSS() {
	html := []byte(`<html><body>
		<div class="product">
			<span class="name">Product A</span>
			<span class="price">$10</span>
		</div>
		<div class="product">
			<span class="name">Product B</span>
			<span class="price">$20</span>
		</div>
	</body></html>`)

	sel := selector.NewFromBytes(html)

	// 链式查询：先选择所有产品，再从中提取价格
	prices := sel.CSS("div.product").CSS("span.price").GetAll()
	fmt.Println("Prices:", prices)

	// 获取第一个产品名称
	firstName := sel.CSS("div.product").CSS("span.name").Get("")
	fmt.Println("First:", firstName)

	// Output:
	// Prices: [$10 $20]
	// First: Product A
}
