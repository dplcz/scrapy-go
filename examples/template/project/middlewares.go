// scrapy-go middlewares 模板
//
// 在此处定义自定义的下载器中间件和 Spider 中间件。
//
// 下载器中间件注册方式：
//
//	c := crawler.NewDefault()
//	c.AddDownloaderMiddleware(&MyDownloaderMiddleware{}, "MyDL", 543)
//	c.Run(ctx, sp)
//
// Spider 中间件注册方式：
//
//	c.AddSpiderMiddleware(&MySpiderMiddleware{}, "MySM", 543)
package project

import (
	"context"

	dmiddle "github.com/dplcz/scrapy-go/pkg/downloader/middleware"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
	smiddle "github.com/dplcz/scrapy-go/pkg/spider/middleware"
)

// ============================================================================
// 下载器中间件
// ============================================================================

// MyDownloaderMiddleware 是自定义下载器中间件模板。
// 嵌入 BaseDownloaderMiddleware 提供默认实现，只需覆盖需要的方法。
//
// 不需要定义所有方法。未定义的方法等同于不修改传入的对象。
type MyDownloaderMiddleware struct {
	dmiddle.BaseDownloaderMiddleware
}

// ProcessRequest 在请求发送到下载器之前调用（正序执行）。
//
// 返回值：
//   - (nil, nil)       — 继续处理链
//   - (*Response, nil)  — 短路，直接返回响应，不再调用后续中间件和下载器
//   - (nil, error)      — 触发 ProcessException 链
func (m *MyDownloaderMiddleware) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	return nil, nil
}

// ProcessResponse 在下载器返回响应后调用（逆序执行）。
//
// 返回值：
//   - (*Response, nil)  — 继续处理链（可修改响应）
//   - (nil, error)      — 触发异常处理
func (m *MyDownloaderMiddleware) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	return response, nil
}

// ProcessException 在下载异常时调用（逆序执行）。
//
// 返回值：
//   - (nil, nil)        — 继续异常处理链
//   - (*Response, nil)  — 将异常转换为响应
//   - (nil, error)      — 继续传播（可能是不同的错误）
func (m *MyDownloaderMiddleware) ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error) {
	return nil, nil
}

// ============================================================================
// Spider 中间件
// ============================================================================

// MySpiderMiddleware 是自定义 Spider 中间件模板。
// 嵌入 BaseSpiderMiddleware 提供默认实现，只需覆盖需要的方法。
//
// 不需要定义所有方法。未定义的方法等同于不修改传入的对象。
type MySpiderMiddleware struct {
	smiddle.BaseSpiderMiddleware
}

// ProcessSpiderInput 在响应传递给 Spider 回调之前调用（正序执行）。
//
// 返回值：
//   - nil   — 继续处理链
//   - error — 跳过 Spider 回调，直接进入 ProcessSpiderException 链
func (m *MySpiderMiddleware) ProcessSpiderInput(ctx context.Context, response *shttp.Response) error {
	return nil
}

// ProcessOutput 在 Spider 回调产出结果后调用（逆序执行）。
//
// 可以过滤、修改或添加新的输出项。
// 必须返回 Request 或 Item 的切片。
func (m *MySpiderMiddleware) ProcessOutput(ctx context.Context, response *shttp.Response, result []spider.Output) ([]spider.Output, error) {
	return result, nil
}

// ProcessSpiderException 在 Spider 回调抛出异常时调用（逆序执行）。
// 对齐 Scrapy 的 process_spider_exception(self, response, exception, spider) 方法。
//
// 返回值：
//   - (nil, nil)      — 继续异常传播
//   - (outputs, nil)  — 用输出替代异常
func (m *MySpiderMiddleware) ProcessSpiderException(ctx context.Context, response *shttp.Response, err error) ([]spider.Output, error) {
	return nil, nil
}
