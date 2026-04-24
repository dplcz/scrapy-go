package selector

import (
	"testing"
)

const testHTML = `<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
<h1 class="title">Page Title</h1>
<div class="quotes">
  <div class="quote" id="q1">
    <span class="text">"The world as we have created it is a process of our thinking."</span>
    <span class="author">Albert Einstein</span>
    <div class="tags">
      <a class="tag" href="/tag/change">change</a>
      <a class="tag" href="/tag/thinking">thinking</a>
    </div>
  </div>
  <div class="quote" id="q2">
    <span class="text">"It is our choices that show what we truly are."</span>
    <span class="author">J.K. Rowling</span>
    <div class="tags">
      <a class="tag" href="/tag/abilities">abilities</a>
      <a class="tag" href="/tag/choices">choices</a>
    </div>
  </div>
</div>
<nav>
  <ul class="pager">
    <li class="next"><a href="/page/2">Next →</a></li>
  </ul>
</nav>
<div class="empty"></div>
</body>
</html>`

// ============================================================================
// Selector 创建测试
// ============================================================================

func TestNewFromBytes(t *testing.T) {
	sel := NewFromBytes([]byte(testHTML))
	if sel == nil {
		t.Fatal("NewFromBytes should not return nil")
	}
	if sel.node == nil {
		t.Fatal("Selector should have a node")
	}
}

func TestNewFromText(t *testing.T) {
	sel := NewFromText(testHTML)
	if sel == nil {
		t.Fatal("NewFromText should not return nil")
	}
}

func TestNewFromBytesInvalidHTML(t *testing.T) {
	// 即使 HTML 无效，也不应 panic
	sel := NewFromBytes([]byte("<div><span>unclosed"))
	if sel == nil {
		t.Fatal("should handle invalid HTML gracefully")
	}
}

func TestNewFromBytesEmpty(t *testing.T) {
	sel := NewFromBytes([]byte(""))
	if sel == nil {
		t.Fatal("should handle empty input")
	}
}

// ============================================================================
// CSS 选择器测试
// ============================================================================

func TestCSSBasic(t *testing.T) {
	sel := NewFromText(testHTML)
	results := sel.CSS("div.quote")
	if results.Len() != 2 {
		t.Errorf("expected 2 quotes, got %d", results.Len())
	}
}

func TestCSSNested(t *testing.T) {
	sel := NewFromText(testHTML)
	quotes := sel.CSS("div.quote")
	// 在第一个 quote 中查找 author
	if quotes.Len() < 1 {
		t.Fatal("expected at least 1 quote")
	}
	authors := quotes[0].CSS("span.author")
	if authors.Len() != 1 {
		t.Errorf("expected 1 author, got %d", authors.Len())
	}
	if authors.Get("") != "Albert Einstein" {
		t.Errorf("expected 'Albert Einstein', got %q", authors.Get(""))
	}
}

func TestCSSPseudoText(t *testing.T) {
	sel := NewFromText(testHTML)
	texts := sel.CSS("span.text::text")
	if texts.Len() != 2 {
		t.Errorf("expected 2 texts, got %d", texts.Len())
	}
	all := texts.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 text values, got %d", len(all))
	}
	expected := `"The world as we have created it is a process of our thinking."`
	if all[0] != expected {
		t.Errorf("expected %q, got %q", expected, all[0])
	}
}

func TestCSSNoMatch(t *testing.T) {
	sel := NewFromText(testHTML)
	results := sel.CSS("div.nonexistent")
	if results.Len() != 0 {
		t.Errorf("expected 0 results, got %d", results.Len())
	}
}

func TestCSSGetDefault(t *testing.T) {
	sel := NewFromText(testHTML)
	results := sel.CSS("div.nonexistent")
	val := results.Get("default_value")
	if val != "default_value" {
		t.Errorf("expected 'default_value', got %q", val)
	}
}

func TestCSSOnTextSelector(t *testing.T) {
	// 对文本 Selector 执行 CSS 应返回空
	sel := newTextSelector("hello")
	results := sel.CSS("div")
	if results != nil {
		t.Error("CSS on text selector should return nil")
	}
}

// ============================================================================
// CSSAttr 测试
// ============================================================================

func TestCSSAttr(t *testing.T) {
	sel := NewFromText(testHTML)
	hrefs := sel.CSSAttr("a.tag", "href")
	if hrefs.Len() != 4 {
		t.Errorf("expected 4 tag links, got %d", hrefs.Len())
	}
	all := hrefs.GetAll()
	if len(all) < 4 {
		t.Fatalf("expected 4 href values, got %d", len(all))
	}
	if all[0] != "/tag/change" {
		t.Errorf("expected '/tag/change', got %q", all[0])
	}
}

func TestCSSAttrNoMatch(t *testing.T) {
	sel := NewFromText(testHTML)
	results := sel.CSSAttr("a.tag", "data-nonexistent")
	if results.Len() != 0 {
		t.Errorf("expected 0 results for nonexistent attr, got %d", results.Len())
	}
}

func TestCSSAttrOnTextSelector(t *testing.T) {
	sel := newTextSelector("hello")
	results := sel.CSSAttr("a", "href")
	if results != nil {
		t.Error("CSSAttr on text selector should return nil")
	}
}

// ============================================================================
// XPath 测试
// ============================================================================

func TestXPathBasic(t *testing.T) {
	sel := NewFromText(testHTML)
	results := sel.XPath("//div[@class='quote']")
	if results.Len() != 2 {
		t.Errorf("expected 2 quotes, got %d", results.Len())
	}
}

func TestXPathText(t *testing.T) {
	sel := NewFromText(testHTML)
	results := sel.XPath("//span[@class='author']/text()")
	if results.Len() != 2 {
		t.Errorf("expected 2 authors, got %d", results.Len())
	}
	all := results.GetAll()
	if len(all) < 2 {
		t.Fatalf("expected 2 author values, got %d", len(all))
	}
	if all[0] != "Albert Einstein" {
		t.Errorf("expected 'Albert Einstein', got %q", all[0])
	}
	if all[1] != "J.K. Rowling" {
		t.Errorf("expected 'J.K. Rowling', got %q", all[1])
	}
}

func TestXPathAttr(t *testing.T) {
	sel := NewFromText(testHTML)
	results := sel.XPath("//a[@class='tag']/@href")
	if results.Len() != 4 {
		t.Errorf("expected 4 href attrs, got %d", results.Len())
	}
}

func TestXPathNested(t *testing.T) {
	sel := NewFromText(testHTML)
	quotes := sel.XPath("//div[@class='quote']")
	if quotes.Len() < 1 {
		t.Fatal("expected at least 1 quote")
	}
	// 在第一个 quote 中查找 tags
	tags := quotes[0].XPath(".//a[@class='tag']/text()")
	if tags.Len() != 2 {
		t.Errorf("expected 2 tags in first quote, got %d", tags.Len())
	}
}

func TestXPathNoMatch(t *testing.T) {
	sel := NewFromText(testHTML)
	results := sel.XPath("//div[@class='nonexistent']")
	if results.Len() != 0 {
		t.Errorf("expected 0 results, got %d", results.Len())
	}
}

func TestXPathInvalidExpr(t *testing.T) {
	sel := NewFromText(testHTML)
	// 无效的 XPath 表达式不应 panic
	results := sel.XPath("[invalid")
	if results != nil && results.Len() != 0 {
		t.Error("invalid XPath should return empty results")
	}
}

func TestXPathOnTextSelector(t *testing.T) {
	sel := newTextSelector("hello")
	results := sel.XPath("//div")
	if results != nil {
		t.Error("XPath on text selector should return nil")
	}
}

// ============================================================================
// List 方法测试
// ============================================================================

func TestListGetAll(t *testing.T) {
	sel := NewFromText(testHTML)
	authors := sel.CSS("span.author::text")
	all := authors.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 authors, got %d", len(all))
	}
	if all[0] != "Albert Einstein" {
		t.Errorf("expected 'Albert Einstein', got %q", all[0])
	}
	if all[1] != "J.K. Rowling" {
		t.Errorf("expected 'J.K. Rowling', got %q", all[1])
	}
}

func TestListGet(t *testing.T) {
	sel := NewFromText(testHTML)
	title := sel.CSS("h1.title::text").Get("no title")
	if title != "Page Title" {
		t.Errorf("expected 'Page Title', got %q", title)
	}
}

func TestListGetEmpty(t *testing.T) {
	sel := NewFromText(testHTML)
	val := sel.CSS("div.nonexistent::text").Get("fallback")
	if val != "fallback" {
		t.Errorf("expected 'fallback', got %q", val)
	}
}

func TestListFirst(t *testing.T) {
	sel := NewFromText(testHTML)
	first := sel.CSS("div.quote").First()
	if first == nil {
		t.Fatal("First() should not return nil")
	}
	author := first.CSS("span.author::text").Get("")
	if author != "Albert Einstein" {
		t.Errorf("expected 'Albert Einstein', got %q", author)
	}
}

func TestListFirstEmpty(t *testing.T) {
	sel := NewFromText(testHTML)
	first := sel.CSS("div.nonexistent").First()
	if first != nil {
		t.Error("First() on empty list should return nil")
	}
}

func TestListAttr(t *testing.T) {
	sel := NewFromText(testHTML)
	href := sel.CSS("li.next a").Attr("href")
	if href != "/page/2" {
		t.Errorf("expected '/page/2', got %q", href)
	}
}

func TestListAttrAll(t *testing.T) {
	sel := NewFromText(testHTML)
	hrefs := sel.CSS("a.tag").AttrAll("href")
	if len(hrefs) != 4 {
		t.Errorf("expected 4 hrefs, got %d", len(hrefs))
	}
}

func TestListAttrEmpty(t *testing.T) {
	sel := NewFromText(testHTML)
	href := sel.CSS("div.nonexistent").Attr("href")
	if href != "" {
		t.Errorf("expected empty string, got %q", href)
	}
}

func TestListCSS(t *testing.T) {
	sel := NewFromText(testHTML)
	// 链式 CSS 查询
	tags := sel.CSS("div.quote").CSS("a.tag::text")
	all := tags.GetAll()
	if len(all) != 4 {
		t.Errorf("expected 4 tags, got %d: %v", len(all), all)
	}
}

func TestListXPath(t *testing.T) {
	sel := NewFromText(testHTML)
	// 链式 XPath 查询
	quotes := sel.CSS("div.quote")
	authors := quotes.XPath(".//span[@class='author']/text()")
	all := authors.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 authors, got %d: %v", len(all), all)
	}
}

func TestListCSSAttr(t *testing.T) {
	sel := NewFromText(testHTML)
	hrefs := sel.CSS("div.quote").CSSAttr("a.tag", "href")
	all := hrefs.GetAll()
	if len(all) != 4 {
		t.Errorf("expected 4 hrefs, got %d: %v", len(all), all)
	}
}

// ============================================================================
// Selector 方法测试
// ============================================================================

func TestSelectorGet(t *testing.T) {
	sel := NewFromText(testHTML)
	quotes := sel.CSS("div.quote")
	if quotes.Len() < 1 {
		t.Fatal("expected at least 1 quote")
	}
	// Get 返回元素的文本内容
	text := quotes[0].Get("")
	if text == "" {
		t.Error("Get should return non-empty text for element")
	}
}

func TestSelectorGetDefault(t *testing.T) {
	sel := NewFromText(testHTML)
	empty := sel.CSS("div.empty")
	if empty.Len() < 1 {
		t.Fatal("expected at least 1 empty div")
	}
	val := empty[0].Get("default")
	if val != "default" {
		t.Errorf("expected 'default' for empty element, got %q", val)
	}
}

func TestSelectorGetHTML(t *testing.T) {
	sel := NewFromText(testHTML)
	title := sel.CSS("h1.title")
	if title.Len() < 1 {
		t.Fatal("expected at least 1 title")
	}
	html := title[0].GetHTML()
	if html == "" {
		t.Error("GetHTML should return non-empty HTML")
	}
	if !containsStr(html, "Page Title") {
		t.Errorf("GetHTML should contain 'Page Title', got %q", html)
	}
}

func TestSelectorAttr(t *testing.T) {
	sel := NewFromText(testHTML)
	quotes := sel.CSS("div.quote")
	if quotes.Len() < 1 {
		t.Fatal("expected at least 1 quote")
	}
	id, ok := quotes[0].Attr("id")
	if !ok {
		t.Error("should find id attribute")
	}
	if id != "q1" {
		t.Errorf("expected 'q1', got %q", id)
	}
}

func TestSelectorAttrNotFound(t *testing.T) {
	sel := NewFromText(testHTML)
	quotes := sel.CSS("div.quote")
	if quotes.Len() < 1 {
		t.Fatal("expected at least 1 quote")
	}
	_, ok := quotes[0].Attr("data-nonexistent")
	if ok {
		t.Error("should not find nonexistent attribute")
	}
}

func TestSelectorHasClass(t *testing.T) {
	sel := NewFromText(testHTML)
	quotes := sel.CSS("div.quote")
	if quotes.Len() < 1 {
		t.Fatal("expected at least 1 quote")
	}
	if !quotes[0].HasClass("quote") {
		t.Error("should have class 'quote'")
	}
	if quotes[0].HasClass("nonexistent") {
		t.Error("should not have class 'nonexistent'")
	}
}

func TestTextSelectorGet(t *testing.T) {
	sel := newTextSelector("hello world")
	if sel.Get("") != "hello world" {
		t.Errorf("expected 'hello world', got %q", sel.Get(""))
	}
}

func TestTextSelectorGetHTML(t *testing.T) {
	sel := newTextSelector("hello world")
	if sel.GetHTML() != "hello world" {
		t.Errorf("expected 'hello world', got %q", sel.GetHTML())
	}
}

func TestTextSelectorAttr(t *testing.T) {
	sel := newTextSelector("hello")
	_, ok := sel.Attr("href")
	if ok {
		t.Error("text selector should not have attributes")
	}
}

func TestTextSelectorHasClass(t *testing.T) {
	sel := newTextSelector("hello")
	if sel.HasClass("any") {
		t.Error("text selector should not have classes")
	}
}

// ============================================================================
// parsePseudo 测试
// ============================================================================

func TestParsePseudo(t *testing.T) {
	tests := []struct {
		input      string
		wantQuery  string
		wantPseudo string
	}{
		{"div.quote", "div.quote", ""},
		{"span.text::text", "span.text", "text"},
		{"a::attr(href)", "a", "attr"},
		{"div.class::text", "div.class", "text"},
		{"h1", "h1", ""},
	}

	for _, tt := range tests {
		query, pseudo := parsePseudo(tt.input)
		if query != tt.wantQuery {
			t.Errorf("parsePseudo(%q): query = %q, want %q", tt.input, query, tt.wantQuery)
		}
		if pseudo != tt.wantPseudo {
			t.Errorf("parsePseudo(%q): pseudo = %q, want %q", tt.input, pseudo, tt.wantPseudo)
		}
	}
}

// ============================================================================
// 综合场景测试
// ============================================================================

func TestRealWorldScenario(t *testing.T) {
	// 模拟真实爬虫场景：从 quotes 页面提取数据
	sel := NewFromText(testHTML)

	// 提取所有引用
	quotes := sel.CSS("div.quote")
	if quotes.Len() != 2 {
		t.Fatalf("expected 2 quotes, got %d", quotes.Len())
	}

	// 提取第一条引用的详细信息
	q1 := quotes[0]
	text := q1.CSS("span.text::text").Get("")
	author := q1.CSS("span.author::text").Get("")
	tags := q1.CSSAttr("a.tag", "href").GetAll()

	if text != `"The world as we have created it is a process of our thinking."` {
		t.Errorf("unexpected text: %q", text)
	}
	if author != "Albert Einstein" {
		t.Errorf("unexpected author: %q", author)
	}
	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}

	// 提取下一页链接
	nextURL := sel.CSSAttr("li.next a", "href").Get("")
	if nextURL != "/page/2" {
		t.Errorf("expected '/page/2', got %q", nextURL)
	}
}

func TestXPathRealWorldScenario(t *testing.T) {
	// 使用 XPath 实现相同的提取
	sel := NewFromText(testHTML)

	// 提取所有引用
	quotes := sel.XPath("//div[@class='quote']")
	if quotes.Len() != 2 {
		t.Fatalf("expected 2 quotes, got %d", quotes.Len())
	}

	// 提取第一条引用
	q1 := quotes[0]
	text := q1.XPath(".//span[@class='text']/text()").Get("")
	author := q1.XPath(".//span[@class='author']/text()").Get("")

	if text != `"The world as we have created it is a process of our thinking."` {
		t.Errorf("unexpected text: %q", text)
	}
	if author != "Albert Einstein" {
		t.Errorf("unexpected author: %q", author)
	}

	// 提取下一页链接
	nextURL := sel.XPath("//li[@class='next']/a/@href").Get("")
	if nextURL != "/page/2" {
		t.Errorf("expected '/page/2', got %q", nextURL)
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
