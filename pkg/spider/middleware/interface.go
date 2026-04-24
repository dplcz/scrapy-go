// Package middleware 定义了 Spider 中间件的接口和管理器。
//
// Spider 中间件拦截 Spider 的输入（响应）和输出（请求/Item），
// 可以修改、过滤或添加新的输出项。
// 对应 Scrapy Python 版本中 scrapy.core.spidermw 模块的功能。
package middleware

import (
	"context"

	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/spider"
)

// SpiderMiddleware 定义 Spider 中间件接口。
// 实现者可以选择性实现其中的方法（通过嵌入 BaseSpiderMiddleware）。
type SpiderMiddleware interface {
	// ProcessSpiderInput 在响应传递给 Spider 回调之前调用（正序）。
	// 返回 nil 表示继续处理链。
	// 返回 error 表示跳过 Spider 回调，直接进入 ProcessSpiderException 链。
	ProcessSpiderInput(ctx context.Context, response *scrapy_http.Response) error

	// ProcessSpiderOutput 在 Spider 回调产出结果后调用（逆序）。
	// 可以过滤、修改或添加新的输出项。
	ProcessSpiderOutput(ctx context.Context, response *scrapy_http.Response, result []spider.SpiderOutput) ([]spider.SpiderOutput, error)

	// ProcessSpiderException 在 Spider 回调抛出异常时调用（逆序）。
	// 返回 (nil, nil) 继续异常传播。
	// 返回 (outputs, nil) 用输出替代异常。
	ProcessSpiderException(ctx context.Context, response *scrapy_http.Response, err error) ([]spider.SpiderOutput, error)
}

// BaseSpiderMiddleware 提供默认的空实现。
// 中间件可以嵌入此结构体，只覆盖需要的方法。
type BaseSpiderMiddleware struct{}

func (b *BaseSpiderMiddleware) ProcessSpiderInput(ctx context.Context, response *scrapy_http.Response) error {
	return nil
}

func (b *BaseSpiderMiddleware) ProcessSpiderOutput(ctx context.Context, response *scrapy_http.Response, result []spider.SpiderOutput) ([]spider.SpiderOutput, error) {
	return result, nil
}

func (b *BaseSpiderMiddleware) ProcessSpiderException(ctx context.Context, response *scrapy_http.Response, err error) ([]spider.SpiderOutput, error) {
	return nil, nil
}
