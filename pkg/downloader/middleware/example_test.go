package middleware_test

import (
	"context"
	"fmt"

	"github.com/dplcz/scrapy-go/pkg/downloader/middleware"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// customHeaderMW 是一个仅处理请求的自定义中间件示例。
// 它只实现了 RequestProcessor 接口（接口隔离）。
type customHeaderMW struct{}

func (m *customHeaderMW) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	request.Headers.Set("X-Custom-Header", "scrapy-go")
	return nil, nil // 继续处理链
}

// ExampleRequestProcessor 演示实现仅处理请求的中间件。
func ExampleRequestProcessor() {
	mw := &customHeaderMW{}

	req, _ := shttp.NewRequest("https://example.com")
	resp, err := mw.ProcessRequest(context.Background(), req)

	fmt.Println("Response (nil = continue):", resp)
	fmt.Println("Error:", err)
	fmt.Println("Custom header:", req.Headers.Get("X-Custom-Header"))

	// Output:
	// Response (nil = continue): <nil>
	// Error: <nil>
	// Custom header: scrapy-go
}

// ExampleBaseDownloaderMiddleware 演示使用 BaseDownloaderMiddleware 嵌入默认实现。
func ExampleBaseDownloaderMiddleware() {
	// BaseDownloaderMiddleware 提供所有方法的默认空实现
	var mw middleware.DownloaderMiddleware = &middleware.BaseDownloaderMiddleware{}

	req, _ := shttp.NewRequest("https://example.com")
	resp, _ := shttp.NewResponse("https://example.com", 200)

	// ProcessRequest 默认返回 (nil, nil) — 继续处理链
	r, err := mw.ProcessRequest(context.Background(), req)
	fmt.Println("ProcessRequest:", r, err)

	// ProcessResponse 默认原样返回响应
	r2, err := mw.ProcessResponse(context.Background(), req, resp)
	fmt.Println("ProcessResponse status:", r2.Status, err)

	// ProcessException 默认返回 (nil, nil) — 继续异常链
	r3, err := mw.ProcessException(context.Background(), req, fmt.Errorf("test error"))
	fmt.Println("ProcessException:", r3, err)

	// Output:
	// ProcessRequest: <nil> <nil>
	// ProcessResponse status: 200 <nil>
	// ProcessException: <nil> <nil>
}
