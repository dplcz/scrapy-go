// Package http 定义了 scrapy-go 框架的 HTTP 请求和响应模型。
package http

import (
	"net/url"
	"strings"
)

// NewFormRequest 创建一个表单请求。
//
// formdata 是表单数据，键值对形式。值支持单个字符串或字符串切片（多值字段）。
//
// 行为规则（对齐 Scrapy 的 FormRequest）：
//   - 如果未通过 WithMethod 指定 HTTP 方法，默认使用 POST
//   - POST 请求：formdata 编码为 application/x-www-form-urlencoded 写入 Body
//   - GET 请求：formdata 编码为查询参数追加到 URL
//
// 用法：
//
//	// POST 表单
//	req, err := http.NewFormRequest("https://example.com/login",
//	    map[string][]string{"user": {"admin"}, "pass": {"secret"}},
//	)
//
//	// GET 表单（查询参数）
//	req, err := http.NewFormRequest("https://example.com/search",
//	    map[string][]string{"q": {"golang"}},
//	    http.WithMethod("GET"),
//	)
func NewFormRequest(rawURL string, formdata map[string][]string, opts ...RequestOption) (*Request, error) {
	// 先创建一个临时 Request 来确定最终的 method
	// 默认 POST
	method := "POST"
	for _, opt := range opts {
		// 创建一个临时 Request 来探测 method
		probe := &Request{Method: "POST"}
		opt(probe)
		if probe.Method != "POST" {
			method = strings.ToUpper(probe.Method)
		}
	}

	encoded := url.Values(formdata).Encode()

	if strings.ToUpper(method) == "GET" {
		// GET 请求：将 formdata 编码为查询参数
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, err
		}
		if u.RawQuery != "" {
			u.RawQuery += "&" + encoded
		} else {
			u.RawQuery = encoded
		}
		// 构建选项
		allOpts := append([]RequestOption{WithMethod("GET")}, opts...)
		return NewRequest(u.String(), allOpts...)
	}

	// POST 请求：将 formdata 编码为 body
	defaultOpts := []RequestOption{
		WithMethod("POST"),
		WithBody([]byte(encoded)),
		WithHeader("Content-Type", "application/x-www-form-urlencoded"),
	}
	allOpts := append(defaultOpts, opts...)
	return NewRequest(rawURL, allOpts...)
}

// MustNewFormRequest 创建一个表单请求，如果失败则 panic。
// 仅用于确定参数有效的场景。
func MustNewFormRequest(rawURL string, formdata map[string][]string, opts ...RequestOption) *Request {
	req, err := NewFormRequest(rawURL, formdata, opts...)
	if err != nil {
		panic(err)
	}
	return req
}
