package linkextractor_test

import (
	"fmt"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/linkextractor"
)

// ExampleNewHTMLLinkExtractor 演示创建链接提取器并提取链接。
func ExampleNewHTMLLinkExtractor() {
	html := []byte(`<html><body>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
		<a href="https://other.com/external">External</a>
		<a href="/download/file.pdf">PDF</a>
	</body></html>`)

	resp := shttp.MustNewResponse("https://example.com", 200,
		shttp.WithResponseBody(html),
	)

	// 创建默认链接提取器（自动过滤 .pdf 等非网页扩展名）
	le := linkextractor.NewHTMLLinkExtractor()
	links := le.ExtractLinks(resp)

	for _, link := range links {
		fmt.Println(link.URL)
	}

	// Output:
	// https://example.com/page/1
	// https://example.com/page/2
	// https://other.com/external
}

// ExampleNewHTMLLinkExtractor_withFilters 演示使用过滤规则提取链接。
func ExampleNewHTMLLinkExtractor_withFilters() {
	html := []byte(`<html><body>
		<a href="/category/books">Books</a>
		<a href="/category/movies">Movies</a>
		<a href="/page/1">Page 1</a>
		<a href="/page/2">Page 2</a>
		<a href="/login">Login</a>
	</body></html>`)

	resp := shttp.MustNewResponse("https://example.com", 200,
		shttp.WithResponseBody(html),
	)

	// 只提取分页链接，排除登录页
	le := linkextractor.NewHTMLLinkExtractor(
		linkextractor.WithAllow(`/page/\d+`),
		linkextractor.WithDeny(`/login`),
	)
	links := le.ExtractLinks(resp)

	for _, link := range links {
		fmt.Println(link.URL, "-", link.Text)
	}

	// Output:
	// https://example.com/page/1 - Page 1
	// https://example.com/page/2 - Page 2
}

// ExampleHTMLLinkExtractor_Matches 演示快速判断 URL 是否匹配过滤规则。
func ExampleHTMLLinkExtractor_Matches() {
	le := linkextractor.NewHTMLLinkExtractor(
		linkextractor.WithAllow(`/product/\d+`),
		linkextractor.WithDenyDomains("ads.example.com"),
	)

	fmt.Println(le.Matches("https://example.com/product/123"))
	fmt.Println(le.Matches("https://example.com/about"))
	fmt.Println(le.Matches("https://ads.example.com/product/456"))

	// Output:
	// true
	// false
	// false
}
