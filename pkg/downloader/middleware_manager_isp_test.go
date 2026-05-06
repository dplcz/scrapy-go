package downloader

import (
	"context"
	"testing"

	"github.com/dplcz/scrapy-go/pkg/downloader/middleware"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// ============================================================================
// ISP 接口隔离测试
// ============================================================================

// requestOnlyMW 仅实现 RequestProcessor 接口。
type requestOnlyMW struct {
	called bool
}

func (m *requestOnlyMW) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	m.called = true
	request.Headers.Set("X-Request-Only", "true")
	return nil, nil
}

// responseOnlyMW 仅实现 ResponseProcessor 接口。
type responseOnlyMW struct {
	called bool
}

func (m *responseOnlyMW) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	m.called = true
	return response, nil
}

// exceptionOnlyMW 仅实现 ExceptionProcessor 接口。
type exceptionOnlyMW struct {
	called bool
}

func (m *exceptionOnlyMW) ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error) {
	m.called = true
	return nil, nil
}

// requestResponseMW 同时实现 RequestProcessor 和 ResponseProcessor。
type requestResponseMW struct {
	requestCalled  bool
	responseCalled bool
}

func (m *requestResponseMW) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	m.requestCalled = true
	return nil, nil
}

func (m *requestResponseMW) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	m.responseCalled = true
	return response, nil
}

// TestISPRequestOnlyMiddleware 测试仅实现 RequestProcessor 的中间件。
func TestISPRequestOnlyMiddleware(t *testing.T) {
	m := NewMiddlewareManager(nil)
	mw := &requestOnlyMW{}
	m.AddMiddleware(mw, "request_only", 100)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return shttp.MustNewResponse(req.URL.String(), 200, shttp.WithRequest(req)), nil
	}

	req := shttp.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if !mw.called {
		t.Error("RequestProcessor should have been called")
	}
	if req.Headers.Get("X-Request-Only") != "true" {
		t.Error("request header should have been set by RequestProcessor")
	}
}

// TestISPResponseOnlyMiddleware 测试仅实现 ResponseProcessor 的中间件。
func TestISPResponseOnlyMiddleware(t *testing.T) {
	m := NewMiddlewareManager(nil)
	mw := &responseOnlyMW{}
	m.AddMiddleware(mw, "response_only", 100)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return shttp.MustNewResponse(req.URL.String(), 200, shttp.WithRequest(req)), nil
	}

	req := shttp.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if !mw.called {
		t.Error("ResponseProcessor should have been called")
	}
}

// TestISPExceptionOnlyMiddleware 测试仅实现 ExceptionProcessor 的中间件。
func TestISPExceptionOnlyMiddleware(t *testing.T) {
	m := NewMiddlewareManager(nil)
	mw := &exceptionOnlyMW{}
	m.AddMiddleware(mw, "exception_only", 100)

	downloadErr := context.DeadlineExceeded
	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return nil, downloadErr
	}

	req := shttp.MustNewRequest("https://example.com")
	_, err := m.Download(context.Background(), downloadFunc, req)
	// 异常未被处理（ExceptionProcessor 返回 nil, nil），原始错误传播
	if err != downloadErr {
		t.Errorf("expected original error, got %v", err)
	}
	if !mw.called {
		t.Error("ExceptionProcessor should have been called")
	}
}

// TestISPMixedMiddlewares 测试混合使用细粒度接口和全功能接口的中间件。
func TestISPMixedMiddlewares(t *testing.T) {
	m := NewMiddlewareManager(nil)

	reqOnly := &requestOnlyMW{}
	respOnly := &responseOnlyMW{}
	fullMW := &middleware.BaseDownloaderMiddleware{}

	m.AddMiddleware(reqOnly, "request_only", 100)
	m.AddMiddleware(fullMW, "full", 200)
	m.AddMiddleware(respOnly, "response_only", 300)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return shttp.MustNewResponse(req.URL.String(), 200, shttp.WithRequest(req)), nil
	}

	req := shttp.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if !reqOnly.called {
		t.Error("request_only middleware should have been called")
	}
	if !respOnly.called {
		t.Error("response_only middleware should have been called")
	}
}

// TestISPRequestResponseMiddleware 测试同时实现两个接口的中间件。
func TestISPRequestResponseMiddleware(t *testing.T) {
	m := NewMiddlewareManager(nil)
	mw := &requestResponseMW{}
	m.AddMiddleware(mw, "req_resp", 100)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return shttp.MustNewResponse(req.URL.String(), 200, shttp.WithRequest(req)), nil
	}

	req := shttp.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if !mw.requestCalled {
		t.Error("ProcessRequest should have been called")
	}
	if !mw.responseCalled {
		t.Error("ProcessResponse should have been called")
	}
}

// TestISPBackwardCompatibility 测试全功能接口的向后兼容性。
func TestISPBackwardCompatibility(t *testing.T) {
	m := NewMiddlewareManager(nil)

	// 使用旧的 BaseDownloaderMiddleware 嵌入方式
	var order []string
	mw := &orderTrackingMW{name: "full", order: &order}
	m.AddMiddleware(mw, "full", 100)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		order = append(order, "download")
		return shttp.MustNewResponse(req.URL.String(), 200, shttp.WithRequest(req)), nil
	}

	req := shttp.MustNewRequest("https://example.com")
	_, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"full:request", "download", "full:response"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, exp := range expected {
		if order[i] != exp {
			t.Errorf("step %d: expected %s, got %s", i, exp, order[i])
		}
	}
}

// TestISPSkipsNonImplementors 测试 Manager 正确跳过未实现对应接口的中间件。
func TestISPSkipsNonImplementors(t *testing.T) {
	m := NewMiddlewareManager(nil)

	reqOnly := &requestOnlyMW{}
	excOnly := &exceptionOnlyMW{}

	m.AddMiddleware(reqOnly, "request_only", 100)
	m.AddMiddleware(excOnly, "exception_only", 200)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return shttp.MustNewResponse(req.URL.String(), 200, shttp.WithRequest(req)), nil
	}

	req := shttp.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if !reqOnly.called {
		t.Error("request_only should have been called for ProcessRequest")
	}
	// excOnly 不应该被调用（没有异常发生）
	if excOnly.called {
		t.Error("exception_only should not have been called (no exception)")
	}
}
