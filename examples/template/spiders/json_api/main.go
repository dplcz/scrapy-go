// Package main 演示了 scrapy-go 的 Request 便捷 API 用法。
//
// 本示例展示了 NewJSONRequest、NewFormRequest、NoCallback 等便捷 API 的使用方式。
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// JSONAPISpider 演示使用 NewJSONRequest 与 JSON API 交互。
type JSONAPISpider struct {
	spider.Base
}

func (s *JSONAPISpider) Name() string { return "json-api" }

func (s *JSONAPISpider) Start(ctx context.Context) <-chan spider.Output {
	ch := make(chan spider.Output)
	go func() {
		defer close(ch)

		// 使用 NewJSONRequest 发送 JSON 请求
		// 自动设置 Content-Type: application/json 和 Accept 头
		// 默认使用 POST 方法
		req, err := shttp.NewJSONRequest("https://httpbin.org/post",
			map[string]any{
				"name":    "scrapy-go",
				"version": "0.3.0",
				"tags":    []string{"crawler", "go", "async"},
			},
		)
		if err != nil {
			log.Printf("创建 JSON 请求失败: %v", err)
			return
		}
		req.DontFilter = true
		ch <- spider.Output{Request: req}

		// 使用 NewJSONRequest + WithMethod 覆盖为 PUT
		req2, err := shttp.NewJSONRequest("https://httpbin.org/put",
			map[string]any{"updated": true},
			shttp.WithMethod("PUT"),
		)
		if err != nil {
			log.Printf("创建 PUT 请求失败: %v", err)
			return
		}
		req2.DontFilter = true
		ch <- spider.Output{Request: req2}

		// 使用 NewFormRequest 发送表单请求
		// 自动设置 Content-Type: application/x-www-form-urlencoded
		// 默认使用 POST 方法
		req3, err := shttp.NewFormRequest("https://httpbin.org/post",
			map[string][]string{
				"username": {"admin"},
				"password": {"secret123"},
			},
		)
		if err != nil {
			log.Printf("创建表单请求失败: %v", err)
			return
		}
		req3.DontFilter = true
		ch <- spider.Output{Request: req3}

		// 使用 NewFormRequest + GET 方法（数据编码为查询参数）
		req4, err := shttp.NewFormRequest("https://httpbin.org/get",
			map[string][]string{
				"q":    {"golang web crawler"},
				"page": {"1"},
			},
			shttp.WithMethod("GET"),
		)
		if err != nil {
			log.Printf("创建 GET 表单请求失败: %v", err)
			return
		}
		req4.DontFilter = true
		ch <- spider.Output{Request: req4}

		// 使用 NoCallback 发送不需要回调的请求
		// 适用于只需要触发请求但不关心响应的场景
		req5 := shttp.MustNewRequest("https://httpbin.org/get",
			shttp.WithCallback(shttp.NoCallback),
			shttp.WithDontFilter(true),
			shttp.WithUserAgent("scrapy-go-example/1.0"),
		)
		ch <- spider.Output{Request: req5}
	}()
	return ch
}

func (s *JSONAPISpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	fmt.Printf("收到响应: %s (状态码: %d)\n", response.URL.String(), response.Status)

	// 解析 JSON 响应
	var data map[string]any
	if err := response.JSON(&data); err == nil {
		if origin, ok := data["origin"]; ok {
			fmt.Printf("  来源 IP: %v\n", origin)
		}
		if jsonData, ok := data["json"]; ok {
			fmt.Printf("  JSON 数据: %v\n", jsonData)
		}
		if form, ok := data["form"]; ok {
			fmt.Printf("  表单数据: %v\n", form)
		}
		if args, ok := data["args"]; ok {
			fmt.Printf("  查询参数: %v\n", args)
		}
	}

	return nil, nil
}

func main() {
	c := crawler.New(crawler.WithSettings(func() *settings.Settings {
		s := settings.New()
		s.Set("LOG_LEVEL", "INFO", settings.PriorityProject)
		return s
	}()))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Run(ctx, &JSONAPISpider{}); err != nil {
		log.Fatalf("爬虫运行失败: %v", err)
	}
}
