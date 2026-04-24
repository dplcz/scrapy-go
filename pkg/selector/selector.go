// Package selector 提供 HTML/XML 文档的 CSS 和 XPath 选择器。
//
// 对应 Scrapy 的 parsel/Selector 模块，提供链式调用的选择器 API。
// 底层使用 goquery（CSS 选择器）和 htmlquery（XPath 选择器）。
//
// 用法：
//
//	sel := selector.NewFromBytes(htmlBody)
//	titles := sel.CSS("h1.title").GetAll()
//	links := sel.XPath("//a/@href").GetAll()
//	firstTitle := sel.CSS("h1.title").Get("")
package selector

import (
	"bytes"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/antchfx/htmlquery"
	"golang.org/x/net/html"
)

// Selector 表示一个 HTML/XML 选择器，支持 CSS 和 XPath 查询。
//
// 对应 Scrapy 的 Selector 类。Selector 可以从 HTML 文本或节点创建，
// 支持链式调用进行嵌套查询。
type Selector struct {
	// node 是底层的 HTML 节点（用于 XPath 查询）
	node *html.Node

	// selection 是 goquery 的 Selection（用于 CSS 查询）
	selection *goquery.Selection

	// text 存储纯文本值（当 Selector 表示文本节点或属性值时）
	text string

	// isText 标记此 Selector 是否表示纯文本值
	isText bool
}

// List 是 Selector 的切片，支持批量操作。
//
// 对应 Scrapy 的 List 类。
type List []*Selector

// NewFromBytes 从 HTML 字节切片创建 Selector。
func NewFromBytes(body []byte) *Selector {
	node, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		// 解析失败时返回空 Selector
		return &Selector{}
	}
	doc := goquery.NewDocumentFromNode(node)
	return &Selector{
		node:      node,
		selection: doc.Selection,
	}
}

// NewFromText 从 HTML 文本字符串创建 Selector。
func NewFromText(text string) *Selector {
	return NewFromBytes([]byte(text))
}

// newFromNode 从 HTML 节点创建 Selector（内部使用）。
func newFromNode(node *html.Node) *Selector {
	doc := goquery.NewDocumentFromNode(node)
	return &Selector{
		node:      node,
		selection: doc.Selection,
	}
}

// newTextSelector 创建一个纯文本 Selector（内部使用）。
func newTextSelector(text string) *Selector {
	return &Selector{
		text:   text,
		isText: true,
	}
}

// ============================================================================
// Selector 方法
// ============================================================================

// CSS 使用 CSS 选择器查询，返回匹配的 List。
//
// 支持 goquery 扩展的伪类选择器：
//   - 标准 CSS 选择器（如 "div.class", "#id", "a[href]"）
//
// 注意：Scrapy 的 ::text 和 ::attr(name) 伪元素在 Go 中通过
// List.GetAll() 和 Selector.Attr() 方法实现。
//
// 用法：
//
//	sel.CSS("div.quote")                    // 选择所有 class="quote" 的 div
//	sel.CSS("span.text").GetAll()           // 获取所有匹配元素的文本
//	sel.CSS("a").Attr("href")              // 获取第一个 <a> 的 href 属性
func (s *Selector) CSS(query string) List {
	if s.isText || s.selection == nil {
		return nil
	}

	// 解析 Scrapy 风格的伪元素
	realQuery, pseudo := parsePseudo(query)

	var results List
	s.selection.Find(realQuery).Each(func(_ int, sel *goquery.Selection) {
		switch pseudo {
		case "text":
			// ::text — 提取直接文本内容
			text := sel.Text()
			if text != "" {
				results = append(results, newTextSelector(text))
			}
		case "attr":
			// ::attr(name) — 在 parsePseudo 中已处理
		default:
			// 无伪元素 — 返回元素 Selector
			for _, node := range sel.Nodes {
				results = append(results, newFromNode(node))
			}
		}
	})

	return results
}

// CSSAttr 使用 CSS 选择器查询并提取指定属性值。
// 这是 Scrapy 中 css("a::attr(href)") 的 Go 等价实现。
//
// 用法：
//
//	links := sel.CSSAttr("a", "href").GetAll()
func (s *Selector) CSSAttr(query, attr string) List {
	if s.isText || s.selection == nil {
		return nil
	}

	var results List
	s.selection.Find(query).Each(func(_ int, sel *goquery.Selection) {
		if val, exists := sel.Attr(attr); exists {
			results = append(results, newTextSelector(val))
		}
	})
	return results
}

// XPath 使用 XPath 表达式查询，返回匹配的 List。
//
// 用法：
//
//	sel.XPath("//div[@class='quote']")           // 选择所有 class="quote" 的 div
//	sel.XPath("//span[@class='text']/text()")    // 获取文本节点
//	sel.XPath("//a/@href")                       // 获取 href 属性
func (s *Selector) XPath(expr string) List {
	if s.isText || s.node == nil {
		return nil
	}

	nodes, err := htmlquery.QueryAll(s.node, expr)
	if err != nil {
		return nil
	}

	var results List
	for _, node := range nodes {
		if node.Type == html.TextNode {
			// 文本节点
			results = append(results, newTextSelector(node.Data))
		} else if node.Type == html.ElementNode && node.FirstChild != nil && node.FirstChild.Type == html.TextNode && node.FirstChild.NextSibling == nil {
			// 只有一个文本子节点的元素（如属性节点的表示）
			// 对于 XPath 属性查询（如 //a/@href），htmlquery 返回的是属性节点
			results = append(results, newFromNode(node))
		} else {
			results = append(results, newFromNode(node))
		}
	}
	return results
}

// Get 返回 Selector 的文本内容。
// 如果 Selector 为空，返回 defaultVal。
//
// 对于文本 Selector，直接返回文本值。
// 对于元素 Selector，返回元素的内部文本。
func (s *Selector) Get(defaultVal string) string {
	if s.isText {
		return s.text
	}
	if s.selection != nil {
		text := s.selection.Text()
		if text != "" {
			return text
		}
	}
	return defaultVal
}

// GetHTML 返回 Selector 对应元素的外部 HTML。
// 如果 Selector 为空，返回空字符串。
func (s *Selector) GetHTML() string {
	if s.isText {
		return s.text
	}
	if s.selection != nil {
		html, err := goquery.OuterHtml(s.selection)
		if err == nil {
			return html
		}
	}
	return ""
}

// Attr 返回元素的指定属性值。
// 如果属性不存在，返回空字符串和 false。
func (s *Selector) Attr(name string) (string, bool) {
	if s.isText || s.selection == nil {
		return "", false
	}
	return s.selection.Attr(name)
}

// HasClass 检查元素是否包含指定的 CSS 类。
func (s *Selector) HasClass(class string) bool {
	if s.isText || s.selection == nil {
		return false
	}
	return s.selection.HasClass(class)
}

// ============================================================================
// List 方法
// ============================================================================

// CSS 对列表中每个 Selector 执行 CSS 查询，合并结果。
func (sl List) CSS(query string) List {
	var results List
	for _, s := range sl {
		results = append(results, s.CSS(query)...)
	}
	return results
}

// CSSAttr 对列表中每个 Selector 执行 CSS 属性查询，合并结果。
func (sl List) CSSAttr(query, attr string) List {
	var results List
	for _, s := range sl {
		results = append(results, s.CSSAttr(query, attr)...)
	}
	return results
}

// XPath 对列表中每个 Selector 执行 XPath 查询，合并结果。
func (sl List) XPath(expr string) List {
	var results List
	for _, s := range sl {
		results = append(results, s.XPath(expr)...)
	}
	return results
}

// GetAll 返回所有 Selector 的文本内容。
// 对应 Scrapy List 的 getall() 方法。
func (sl List) GetAll() []string {
	results := make([]string, 0, len(sl))
	for _, s := range sl {
		text := s.Get("")
		if text != "" {
			results = append(results, text)
		}
	}
	return results
}

// Get 返回第一个 Selector 的文本内容。
// 如果列表为空，返回 defaultVal。
// 对应 Scrapy List 的 get() 方法。
func (sl List) Get(defaultVal string) string {
	if len(sl) == 0 {
		return defaultVal
	}
	return sl[0].Get(defaultVal)
}

// First 返回列表中的第一个 Selector。
// 如果列表为空，返回 nil。
func (sl List) First() *Selector {
	if len(sl) == 0 {
		return nil
	}
	return sl[0]
}

// Len 返回列表长度。
func (sl List) Len() int {
	return len(sl)
}

// Attr 返回第一个元素的指定属性值。
// 如果列表为空或属性不存在，返回空字符串。
func (sl List) Attr(name string) string {
	if len(sl) == 0 {
		return ""
	}
	val, _ := sl[0].Attr(name)
	return val
}

// AttrAll 返回所有元素的指定属性值。
func (sl List) AttrAll(name string) []string {
	var results []string
	for _, s := range sl {
		if val, ok := s.Attr(name); ok {
			results = append(results, val)
		}
	}
	return results
}

// ============================================================================
// 辅助函数
// ============================================================================

// parsePseudo 解析 CSS 查询中的 Scrapy 风格伪元素。
// 支持 ::text 和 ::attr(name)。
// 返回实际的 CSS 查询和伪元素类型。
func parsePseudo(query string) (realQuery string, pseudo string) {
	if idx := strings.Index(query, "::text"); idx != -1 {
		return strings.TrimSpace(query[:idx]), "text"
	}
	if idx := strings.Index(query, "::attr("); idx != -1 {
		return strings.TrimSpace(query[:idx]), "attr"
	}
	return query, ""
}
