// Package middleware 定义了下载器中间件的接口和管理器。
//
// 下载器中间件拦截请求和响应，可以修改、过滤或短路请求处理流程。
// 对应 Scrapy Python 版本中 scrapy.downloadermiddlewares 和
// scrapy.core.downloader.middleware 模块的功能。
package middleware

import (
	"context"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// DownloaderMiddleware 定义下载器中间件接口。
// 实现者可以选择性实现其中的方法（通过嵌入 BaseDownloaderMiddleware）。
type DownloaderMiddleware interface {
	// ProcessRequest 在请求发送到下载器之前调用（正序）。
	// 返回值：
	//   - (nil, nil): 继续处理链
	//   - (*Response, nil): 短路，直接返回响应
	//   - (nil, error): 触发 ProcessException 链
	ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error)

	// ProcessResponse 在下载器返回响应后调用（逆序）。
	// 返回值：
	//   - (*Response, nil): 继续处理链（可修改响应）
	//   - (nil, error): 触发异常处理
	ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error)

	// ProcessException 在下载异常时调用（逆序）。
	// 返回值：
	//   - (nil, nil): 继续异常处理链
	//   - (*Response, nil): 将异常转换为响应
	//   - (nil, error): 继续传播（可能是不同的错误）
	ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error)
}

// BaseDownloaderMiddleware 提供默认的空实现。
// 中间件可以嵌入此结构体，只覆盖需要的方法。
type BaseDownloaderMiddleware struct{}

func (b *BaseDownloaderMiddleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	return nil, nil
}

func (b *BaseDownloaderMiddleware) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	return response, nil
}

func (b *BaseDownloaderMiddleware) ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error) {
	return nil, nil
}

// DownloadFunc 定义实际下载函数的类型。
// 由 Downloader 提供，中间件管理器在处理链末端调用。
type DownloadFunc func(ctx context.Context, request *shttp.Request) (*shttp.Response, error)
