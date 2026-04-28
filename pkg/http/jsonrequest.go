// Package http 定义了 scrapy-go 框架的 HTTP 请求和响应模型。
package http

import (
	"encoding/json"
	"fmt"
)

// NewJSONRequest 创建一个 JSON 请求。
// 自动设置 Content-Type 为 application/json，Accept 为 application/json，
// 并将 data 序列化为 JSON 作为请求体。
//
// 如果未通过 WithMethod 指定 HTTP 方法，默认使用 POST。
//
// 对齐 Scrapy 的 JsonRequest 类。
//
// 用法：
//
//	req, err := http.NewJSONRequest("https://api.example.com/data",
//	    map[string]any{"key": "value"},
//	)
//
//	req, err := http.NewJSONRequest("https://api.example.com/data",
//	    MyStruct{Name: "test"},
//	    http.WithMethod("PUT"),
//	    http.WithHeader("X-Custom", "value"),
//	)
func NewJSONRequest(rawURL string, data any, opts ...RequestOption) (*Request, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON data: %w", err)
	}

	// 构建默认选项：POST 方法 + JSON body + Content-Type + Accept
	defaultOpts := []RequestOption{
		WithMethod("POST"),
		WithBody(body),
		WithHeader("Content-Type", "application/json"),
		WithHeader("Accept", "application/json, text/javascript, */*; q=0.01"),
	}

	// 用户选项在后面，可以覆盖默认值
	allOpts := append(defaultOpts, opts...)

	return NewRequest(rawURL, allOpts...)
}

// MustNewJSONRequest 创建一个 JSON 请求，如果失败则 panic。
// 仅用于确定参数有效的场景。
func MustNewJSONRequest(rawURL string, data any, opts ...RequestOption) *Request {
	req, err := NewJSONRequest(rawURL, data, opts...)
	if err != nil {
		panic(err)
	}
	return req
}
