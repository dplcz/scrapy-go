package spider_test

import (
	"context"
	"fmt"
	"time"

	"github.com/dplcz/scrapy-go/pkg/spider"
)

// mySpider 是一个简单的 Spider 示例。
type mySpider struct {
	spider.Base
}

// ExampleBase 演示使用 Base 创建简单的 Spider。
func ExampleBase() {
	s := &mySpider{
		Base: spider.Base{
			SpiderName: "example",
			StartURLs: []string{
				"https://example.com/page/1",
				"https://example.com/page/2",
			},
		},
	}

	fmt.Println("Name:", s.Name())

	// Start() 返回初始请求的 channel
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ch := s.Start(ctx)
	count := 0
	for range ch {
		count++
	}
	fmt.Println("Start requests:", count)

	// Output:
	// Name: example
	// Start requests: 2
}

// ExampleSettings_ToMap 演示 Spider 级别的类型安全配置。
func ExampleSettings_ToMap() {
	ss := &spider.Settings{
		ConcurrentRequests: spider.IntPtr(4),
		DownloadDelay:      spider.DurationPtr(time.Second),
		RetryEnabled:       spider.BoolPtr(false),
		LogLevel:           spider.StringPtr("INFO"),
	}

	m := ss.ToMap()
	fmt.Println("CONCURRENT_REQUESTS:", m["CONCURRENT_REQUESTS"])
	fmt.Println("DOWNLOAD_DELAY:", m["DOWNLOAD_DELAY"])
	fmt.Println("RETRY_ENABLED:", m["RETRY_ENABLED"])
	fmt.Println("LOG_LEVEL:", m["LOG_LEVEL"])

	// Output:
	// CONCURRENT_REQUESTS: 4
	// DOWNLOAD_DELAY: 1s
	// RETRY_ENABLED: false
	// LOG_LEVEL: INFO
}
