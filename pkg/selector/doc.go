// Package selector 提供 HTML/XML 文档的 CSS 和 XPath 选择器。
//
// # 概述
//
// selector 包是 scrapy-go 框架的 HTML 解析核心，提供链式调用的选择器 API。
// 对应 Scrapy Python 版本中 parsel 库（Selector/SelectorList）的功能。
// 底层使用 [goquery]（CSS 选择器）和 [htmlquery]（XPath 选择器）。
//
// # 核心类型
//
//   - [Selector]：单个 HTML/XML 元素的选择器，支持 CSS 和 XPath 查询
//   - [List]：Selector 的切片，支持批量操作和链式调用
//
// # 使用方式
//
// 从 HTML 字节创建 Selector 并执行查询：
//
//	sel := selector.NewFromBytes(htmlBody)
//
//	// CSS 选择器
//	titles := sel.CSS("h1.title").GetAll()
//	firstLink := sel.CSS("a").Get("")
//
//	// XPath 选择器
//	quotes := sel.XPath("//div[@class='quote']").GetAll()
//	href := sel.XPath("//a/@href").Get("")
//
//	// 属性提取
//	links := sel.CSSAttr("a", "href").GetAll()
//
//	// 链式嵌套查询
//	sel.CSS("div.item").CSS("span.price").GetAll()
//
// 通常不需要直接使用本包，而是通过 [Response] 的快捷方法：
//
//	// 在 Spider 回调中
//	titles := response.CSS("h1.title::text").GetAll()
//	links := response.XPath("//a/@href").GetAll()
//
// # Scrapy 伪元素支持
//
// 本包支持 Scrapy 风格的 CSS 伪元素语法：
//   - ::text — 提取元素的文本内容（通过 [List.GetAll] 获取）
//   - ::attr(name) — 提取元素属性（通过 [Selector.CSSAttr] 方法）
//
// 示例：
//
//	// Scrapy 风格（通过 parsePseudo 内部解析）
//	sel.CSS("span.text::text").GetAll()
//
//	// Go 惯用风格（推荐）
//	sel.CSS("span.text").GetAll()          // 获取文本
//	sel.CSSAttr("a", "href").GetAll()      // 获取属性
//
// # 与 Scrapy/parsel 的差异
//
//   - 使用 goquery（基于 Go 标准库 net/html）替代 lxml/cssselect
//   - 使用 htmlquery（基于 Go 标准库 net/html）替代 lxml.etree
//   - [List] 是 []*Selector 类型别名，而非独立类型（更符合 Go 惯例）
//   - ::attr(name) 伪元素通过独立的 [Selector.CSSAttr] 方法实现（类型更安全）
//   - 解析失败时返回空 Selector（不 panic），保证链式调用安全
//   - 无 re() / re_first() 方法——Go 中直接使用 regexp 包处理
//
// # 性能说明
//
// 每次调用 CSS/XPath 方法都会执行一次查询。如果需要对同一文档执行多次查询，
// 建议复用 Selector 实例而非重复创建：
//
//	sel := selector.NewFromBytes(body)  // 只解析一次
//	titles := sel.CSS("h1").GetAll()
//	links := sel.CSSAttr("a", "href").GetAll()
//
// [goquery]: https://github.com/PuerkitoBio/goquery
// [htmlquery]: https://github.com/antchfx/htmlquery
package selector
