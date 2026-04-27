// scrapy-go basic 爬虫模板
//
// 最基础的爬虫模板，包含 Parse 回调。
// 适用于大多数爬取场景。
//
// 运行方式：go run examples/template/spiders/basic.go
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"os"
)

// ============================================================================
// BasicSpider — 基础爬虫
// ============================================================================

// BasicSpider 是最基础的爬虫实现。
// 嵌入 spider.Base 获得默认的 Name()、Start()、Closed() 等方法，
// 只需实现 Parse() 方法即可。
type BasicSpider struct {
	spider.Base
}

// NewBasicSpider 创建一个新的 BasicSpider。
func NewBasicSpider() *BasicSpider {
	return &BasicSpider{
		Base: spider.Base{
			SpiderName: "basic", // 对齐 Scrapy: name = "basic"
			StartURLs: []string{ // 对齐 Scrapy: start_urls = [...]
				"https://example.com",
			},
		},
	}
}

// Parse 是默认的响应回调函数。
//
// 可用的响应解析方法：
//   - response.CSS("selector")           — CSS 选择器
//   - response.XPath("//xpath")          — XPath 选择器
//   - response.Text()                    — 响应文本
//   - response.JSON(&target)             — JSON 反序列化
//   - response.URLJoin(relativeURL)      — 拼接相对 URL
func (s *BasicSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	var outputs []spider.Output

	// TODO: 在此处实现页面解析逻辑
	_ = response

	return outputs, nil
}

// CustomSettings 返回 Spider 级别的配置覆盖（可选）。
// 返回 nil 表示使用框架默认配置。
// func (s *BasicSpider) CustomSettings() *spider.Settings {
// 	return &spider.Settings{
// 		ConcurrentRequests: spider.IntPtr(16),
// 		DownloadDelay:      spider.DurationPtr(0),
// 		LogLevel:           spider.StringPtr("INFO"),
// 	}
// }

func main() {
	sp := NewBasicSpider()
	c := crawler.NewDefault()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.Run(ctx, sp); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "Crawl error: %v\n", err)
		os.Exit(1)
	}
}
