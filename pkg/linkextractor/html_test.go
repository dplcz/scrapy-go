package linkextractor

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// newTestResponse 创建测试用的 Response。
func newTestResponse(rawURL string, body string) *shttp.Response {
	u, _ := url.Parse(rawURL)
	return &shttp.Response{
		URL:     u,
		Status:  200,
		Headers: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body:    []byte(body),
	}
}

// ============================================================================
// Link 测试
// ============================================================================

func TestLinkString(t *testing.T) {
	link := Link{URL: "https://example.com", Text: "Example"}
	expected := "Link(url=https://example.com, text=Example)"
	if link.String() != expected {
		t.Errorf("expected %q, got %q", expected, link.String())
	}
}

// ============================================================================
// HTMLLinkExtractor 基础测试
// ============================================================================

func TestNewHTMLLinkExtractor_Defaults(t *testing.T) {
	le := NewHTMLLinkExtractor()

	if !le.tags["a"] || !le.tags["area"] {
		t.Error("default tags should include 'a' and 'area'")
	}
	if !le.attrs["href"] {
		t.Error("default attrs should include 'href'")
	}
	if !le.unique {
		t.Error("unique should default to true")
	}
	if !le.stripFragment {
		t.Error("stripFragment should default to true")
	}
}

func TestExtractLinks_BasicHTML(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
		<a href="https://other.com/page/3">Page 3</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor()
	links := le.ExtractLinks(resp)

	if len(links) != 3 {
		t.Fatalf("expected 3 links, got %d", len(links))
	}

	// 检查相对 URL 被解析为绝对 URL
	if links[0].URL != "https://example.com/page/1" {
		t.Errorf("expected https://example.com/page/1, got %s", links[0].URL)
	}
	if links[0].Text != "Page 1" {
		t.Errorf("expected 'Page 1', got %q", links[0].Text)
	}
}

func TestExtractLinks_EmptyResponse(t *testing.T) {
	resp := newTestResponse("https://example.com/", "")
	le := NewHTMLLinkExtractor()
	links := le.ExtractLinks(resp)

	if len(links) != 0 {
		t.Errorf("expected 0 links, got %d", len(links))
	}
}

func TestExtractLinks_NilResponse(t *testing.T) {
	le := NewHTMLLinkExtractor()
	links := le.ExtractLinks(nil)

	if len(links) != 0 {
		t.Errorf("expected 0 links, got %d", len(links))
	}
}

// ============================================================================
// Allow/Deny 正则过滤测试
// ============================================================================

func TestExtractLinks_WithAllow(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
		<a href="/about">About</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithAllow(`/page/\d+`))
	links := le.ExtractLinks(resp)

	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

func TestExtractLinks_WithDeny(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
		<a href="/about">About</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithDeny(`/about`))
	links := le.ExtractLinks(resp)

	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	for _, link := range links {
		if link.URL == "https://example.com/about" {
			t.Error("denied link should not be extracted")
		}
	}
}

func TestExtractLinks_AllowAndDeny(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
		<a href="/page/admin">Admin</a>
		<a href="/about">About</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(
		WithAllow(`/page/`),
		WithDeny(`/page/admin`),
	)
	links := le.ExtractLinks(resp)

	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

// ============================================================================
// 域名过滤测试
// ============================================================================

func TestExtractLinks_WithAllowDomains(t *testing.T) {
	html := `<html><body>
		<a href="https://example.com/page/1">Page 1</a>
		<a href="https://other.com/page/2">Page 2</a>
		<a href="https://sub.example.com/page/3">Page 3</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithAllowDomains("example.com"))
	links := le.ExtractLinks(resp)

	if len(links) != 2 {
		t.Fatalf("expected 2 links (example.com + sub.example.com), got %d", len(links))
	}
}

func TestExtractLinks_WithDenyDomains(t *testing.T) {
	html := `<html><body>
		<a href="https://example.com/page/1">Page 1</a>
		<a href="https://ads.example.com/ad">Ad</a>
		<a href="https://other.com/page/2">Page 2</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithDenyDomains("ads.example.com"))
	links := le.ExtractLinks(resp)

	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	for _, link := range links {
		if link.URL == "https://ads.example.com/ad" {
			t.Error("denied domain link should not be extracted")
		}
	}
}

// ============================================================================
// RestrictCSS/XPath 测试
// ============================================================================

func TestExtractLinks_WithRestrictCSS(t *testing.T) {
	html := `<html><body>
		<div class="content">
			<a href="/page/1">Page 1</a>
		</div>
		<div class="sidebar">
			<a href="/page/2">Page 2</a>
		</div>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithRestrictCSS("div.content"))
	links := le.ExtractLinks(resp)

	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].URL != "https://example.com/page/1" {
		t.Errorf("expected /page/1, got %s", links[0].URL)
	}
}

func TestExtractLinks_WithRestrictXPath(t *testing.T) {
	html := `<html><body>
		<div id="main">
			<a href="/page/1">Page 1</a>
		</div>
		<div id="nav">
			<a href="/page/2">Page 2</a>
		</div>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithRestrictXPath(`//div[@id="main"]`))
	links := le.ExtractLinks(resp)

	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].URL != "https://example.com/page/1" {
		t.Errorf("expected /page/1, got %s", links[0].URL)
	}
}

// ============================================================================
// 去重测试
// ============================================================================

func TestExtractLinks_Unique(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/1">Page 1 Again</a>
		<a href="/page/2">Page 2</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithUnique(true))
	links := le.ExtractLinks(resp)

	if len(links) != 2 {
		t.Fatalf("expected 2 unique links, got %d", len(links))
	}
}

func TestExtractLinks_NotUnique(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/1">Page 1 Again</a>
		<a href="/page/2">Page 2</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithUnique(false))
	links := le.ExtractLinks(resp)

	if len(links) != 3 {
		t.Fatalf("expected 3 links (no dedup), got %d", len(links))
	}
}

// ============================================================================
// Fragment 处理测试
// ============================================================================

func TestExtractLinks_StripFragment(t *testing.T) {
	html := `<html><body>
		<a href="/page/1#section1">Page 1</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithStripFragment(true))
	links := le.ExtractLinks(resp)

	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].URL != "https://example.com/page/1" {
		t.Errorf("expected URL without fragment, got %s", links[0].URL)
	}
	if links[0].Fragment != "section1" {
		t.Errorf("expected fragment 'section1', got %q", links[0].Fragment)
	}
}

func TestExtractLinks_KeepFragment(t *testing.T) {
	html := `<html><body>
		<a href="/page/1#section1">Page 1</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithStripFragment(false))
	links := le.ExtractLinks(resp)

	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].URL != "https://example.com/page/1#section1" {
		t.Errorf("expected URL with fragment, got %s", links[0].URL)
	}
}

// ============================================================================
// NoFollow 测试
// ============================================================================

func TestExtractLinks_NoFollow(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2" rel="nofollow">Page 2</a>
		<a href="/page/3" rel="nofollow noopener">Page 3</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor()
	links := le.ExtractLinks(resp)

	if len(links) != 3 {
		t.Fatalf("expected 3 links, got %d", len(links))
	}
	if links[0].NoFollow {
		t.Error("first link should not be nofollow")
	}
	if !links[1].NoFollow {
		t.Error("second link should be nofollow")
	}
	if !links[2].NoFollow {
		t.Error("third link should be nofollow")
	}
}

// ============================================================================
// 扩展名过滤测试
// ============================================================================

func TestExtractLinks_DenyExtensions(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/files/doc.pdf">PDF</a>
		<a href="/images/photo.jpg">Photo</a>
		<a href="/page/2">Page 2</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor()
	links := le.ExtractLinks(resp)

	// pdf 和 jpg 应被默认扩展名过滤器排除
	if len(links) != 2 {
		t.Fatalf("expected 2 links (pdf and jpg filtered), got %d", len(links))
	}
}

func TestExtractLinks_CustomDenyExtensions(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/files/doc.pdf">PDF</a>
		<a href="/page/2.html">Page 2</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithDenyExtensions([]string{"html"}))
	links := le.ExtractLinks(resp)

	// 只有 .html 被过滤，.pdf 不再被过滤（自定义覆盖默认）
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

// ============================================================================
// RestrictText 测试
// ============================================================================

func TestExtractLinks_RestrictText(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Next Page</a>
		<a href="/page/2">Previous Page</a>
		<a href="/about">About Us</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(WithRestrictText(`Page`))
	links := le.ExtractLinks(resp)

	if len(links) != 2 {
		t.Fatalf("expected 2 links matching 'Page', got %d", len(links))
	}
}

// ============================================================================
// 自定义标签和属性测试
// ============================================================================

func TestExtractLinks_CustomTags(t *testing.T) {
	html := `<html><body>
		<a href="/page/1">Link</a>
		<img src="/image.png" />
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor(
		WithTags("img"),
		WithAttrs("src"),
		WithDenyExtensions(nil),
	)
	links := le.ExtractLinks(resp)

	if len(links) != 1 {
		t.Fatalf("expected 1 link (img src), got %d", len(links))
	}
	if links[0].URL != "https://example.com/image.png" {
		t.Errorf("expected image URL, got %s", links[0].URL)
	}
}

// ============================================================================
// Base URL 测试
// ============================================================================

func TestExtractLinks_BaseTag(t *testing.T) {
	html := `<html><head><base href="https://cdn.example.com/" /></head><body>
		<a href="page/1">Page 1</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor()
	links := le.ExtractLinks(resp)

	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].URL != "https://cdn.example.com/page/1" {
		t.Errorf("expected URL resolved from base tag, got %s", links[0].URL)
	}
}

// ============================================================================
// Matches 测试
// ============================================================================

func TestMatches(t *testing.T) {
	le := NewHTMLLinkExtractor(
		WithAllow(`/page/\d+`),
		WithDenyDomains("ads.example.com"),
	)

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://example.com/page/1", true},
		{"https://example.com/about", false},
		{"https://ads.example.com/page/1", false},
	}

	for _, tt := range tests {
		if got := le.Matches(tt.url); got != tt.expected {
			t.Errorf("Matches(%q) = %v, want %v", tt.url, got, tt.expected)
		}
	}
}

// ============================================================================
// Area 标签测试
// ============================================================================

func TestExtractLinks_AreaTag(t *testing.T) {
	html := `<html><body>
		<map name="map1">
			<area href="/region/1" alt="Region 1" />
			<area href="/region/2" alt="Region 2" />
		</map>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor()
	links := le.ExtractLinks(resp)

	if len(links) != 2 {
		t.Fatalf("expected 2 links from area tags, got %d", len(links))
	}
}

// ============================================================================
// 无效 URL 测试
// ============================================================================

func TestExtractLinks_InvalidURLs(t *testing.T) {
	html := `<html><body>
		<a href="javascript:void(0)">JS Link</a>
		<a href="mailto:test@example.com">Email</a>
		<a href="/page/1">Valid</a>
		<a href="">Empty</a>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	le := NewHTMLLinkExtractor()
	links := le.ExtractLinks(resp)

	// 只有 /page/1 是有效的 http(s) 链接
	if len(links) != 1 {
		t.Fatalf("expected 1 valid link, got %d", len(links))
	}
}

// ============================================================================
// 辅助函数测试
// ============================================================================

func TestIsValidURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"ftp://example.com", true},
		{"file:///tmp/test", true},
		{"javascript:void(0)", false},
		{"mailto:test@example.com", false},
		{"data:text/html,<h1>Hi</h1>", false},
	}

	for _, tt := range tests {
		if got := isValidURL(tt.url); got != tt.expected {
			t.Errorf("isValidURL(%q) = %v, want %v", tt.url, got, tt.expected)
		}
	}
}

func compileRegexps(patterns ...string) []*regexp.Regexp {
	var res []*regexp.Regexp
	for _, p := range patterns {
		re, _ := regexp.Compile(p)
		res = append(res, re)
	}
	return res
}

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		s        string
		patterns []string
		expected bool
	}{
		{"https://example.com/page/1", []string{`/page/\d+`}, true},
		{"https://example.com/about", []string{`/page/\d+`}, false},
		{"https://example.com/page/1", []string{`/about`, `/page/`}, true},
	}

	for _, tt := range tests {
		regexps := compileRegexps(tt.patterns...)
		if got := matchesAny(tt.s, regexps); got != tt.expected {
			t.Errorf("matchesAny(%q, %v) = %v, want %v", tt.s, tt.patterns, got, tt.expected)
		}
	}
}
