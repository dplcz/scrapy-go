// Package middleware 定义了下载器中间件的接口和管理器。
//
// 下载器中间件拦截请求和响应，可以修改、过滤或短路请求处理流程。
// 对应 Scrapy Python 版本中 scrapy.downloadermiddlewares 和
// scrapy.core.downloader.middleware 模块的功能。
//
// # 接口隔离设计（ISP）
//
// Go 版本采用接口隔离原则（Interface Segregation Principle），将原本的
// 单一 DownloaderMiddleware 接口拆分为三个细粒度接口：
//
//   - [RequestProcessor]：仅处理请求（正序调用）
//   - [ResponseProcessor]：仅处理响应（逆序调用）
//   - [ExceptionProcessor]：仅处理异常（逆序调用）
//
// 中间件只需实现自己关心的接口，无需为不需要的方法提供空实现。
// Manager 通过类型断言（type assertion）在运行时适配。
//
// 原有的 [DownloaderMiddleware] 接口保留作为"全功能"便捷接口，
// 同时满足三个细粒度接口，实现完全向后兼容。
//
// 对比 Scrapy Python 版本：Scrapy 要求中间件类必须定义全部三个方法
// （process_request / process_response / process_exception），
// 未实现的方法需要返回 None 或原样传递。Go 版本通过 ISP 消除了这一约束。
package middleware

import (
	"context"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// ============================================================================
// 细粒度接口（ISP）
// ============================================================================

// RequestProcessor 定义请求处理能力。
// 中间件实现此接口以在请求发送到下载器之前进行拦截。
// Manager 按优先级正序调用。
//
// 返回值语义：
//   - (nil, nil): 继续处理链，将请求传递给下一个中间件
//   - (*Response, nil): 短路，直接返回响应，跳过后续中间件和下载器
//   - (nil, error): 触发 ExceptionProcessor 链
type RequestProcessor interface {
	ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error)
}

// ResponseProcessor 定义响应处理能力。
// 中间件实现此接口以在下载器返回响应后进行拦截。
// Manager 按优先级逆序调用。
//
// 返回值语义：
//   - (*Response, nil): 继续处理链（可修改响应后传递）
//   - (nil, error): 触发异常处理
type ResponseProcessor interface {
	ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error)
}

// ExceptionProcessor 定义异常处理能力。
// 中间件实现此接口以在下载异常时进行拦截。
// Manager 按优先级逆序调用。
//
// 返回值语义：
//   - (nil, nil): 继续异常处理链
//   - (*Response, nil): 将异常转换为响应
//   - (nil, error): 继续传播（可能是不同的错误）
type ExceptionProcessor interface {
	ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error)
}

// ============================================================================
// 全功能接口（向后兼容）
// ============================================================================

// DownloaderMiddleware 定义下载器中间件的全功能接口。
// 同时满足 RequestProcessor、ResponseProcessor 和 ExceptionProcessor。
//
// 保留此接口以实现向后兼容：已有的中间件实现无需修改。
// 新的中间件推荐只实现所需的细粒度接口。
type DownloaderMiddleware interface {
	RequestProcessor
	ResponseProcessor
	ExceptionProcessor
}

// ============================================================================
// 基础实现
// ============================================================================

// BaseDownloaderMiddleware 提供默认的空实现。
// 中间件可以嵌入此结构体，只覆盖需要的方法。
//
// 注意：对于新的中间件，推荐直接实现细粒度接口而非嵌入 Base。
// 此结构体保留以兼容已有代码。
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

// ============================================================================
// 下载函数类型
// ============================================================================

// DownloadFunc 定义实际下载函数的类型。
// 由 Downloader 提供，中间件管理器在处理链末端调用。
type DownloadFunc func(ctx context.Context, request *shttp.Request) (*shttp.Response, error)

// ============================================================================
// 编译期接口满足性检查
// ============================================================================

var (
	_ DownloaderMiddleware = (*BaseDownloaderMiddleware)(nil)
	_ RequestProcessor     = (*BaseDownloaderMiddleware)(nil)
	_ ResponseProcessor    = (*BaseDownloaderMiddleware)(nil)
	_ ExceptionProcessor   = (*BaseDownloaderMiddleware)(nil)
)
