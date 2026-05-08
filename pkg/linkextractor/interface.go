package linkextractor

import (
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// Link 表示从 HTML 页面中提取的链接。
// 对应 Scrapy 的 scrapy.link.Link 类。
type Link struct {
	// URL 是链接的绝对 URL。
	URL string

	// Text 是链接的锚文本。
	Text string

	// Fragment 是 URL 中 # 后的片段标识符。
	Fragment string

	// NoFollow 标记链接是否包含 rel="nofollow" 属性。
	NoFollow bool
}

// String 返回 Link 的字符串表示。
func (l Link) String() string {
	return "Link(url=" + l.URL + ", text=" + l.Text + ")"
}

// LinkExtractor 定义链接提取器接口。
// 实现此接口可自定义链接提取逻辑。
//
// 对应 Scrapy 的 LinkExtractor 基类。
type LinkExtractor interface {
	// ExtractLinks 从 HTTP 响应中提取链接列表。
	// 返回的链接应为绝对 URL，且已经过去重和过滤。
	ExtractLinks(response *shttp.Response) []Link
}
