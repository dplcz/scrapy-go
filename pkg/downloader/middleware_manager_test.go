package downloader

import (
	"context"
	"errors"
	"testing"

	"github.com/dplcz/scrapy-go/pkg/downloader/middleware"
	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// ============================================================================
// MiddlewareManager 测试
// ============================================================================

func TestMiddlewareManagerDownloadNormal(t *testing.T) {
	m := NewMiddlewareManager(nil)

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
}

func TestMiddlewareManagerProcessRequestOrder(t *testing.T) {
	m := NewMiddlewareManager(nil)

	var order []string
	mw1 := &orderTrackingMW{name: "mw1", order: &order}
	mw2 := &orderTrackingMW{name: "mw2", order: &order}
	mw3 := &orderTrackingMW{name: "mw3", order: &order}

	m.AddMiddleware(mw1, "mw1", 100)
	m.AddMiddleware(mw2, "mw2", 200)
	m.AddMiddleware(mw3, "mw3", 300)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		order = append(order, "download")
		return shttp.MustNewResponse(req.URL.String(), 200, shttp.WithRequest(req)), nil
	}

	req := shttp.MustNewRequest("https://example.com")
	m.Download(context.Background(), downloadFunc, req)

	// ProcessRequest: 正序 (100 → 200 → 300)
	// Download
	// ProcessResponse: 逆序 (300 → 200 → 100)
	expected := []string{
		"mw1:request", "mw2:request", "mw3:request",
		"download",
		"mw3:response", "mw2:response", "mw1:response",
	}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, exp := range expected {
		if order[i] != exp {
			t.Errorf("step %d: expected %s, got %s", i, exp, order[i])
		}
	}
}

func TestMiddlewareManagerProcessRequestShortCircuit(t *testing.T) {
	m := NewMiddlewareManager(nil)

	// mw1 直接返回响应（短路）
	m.AddMiddleware(&shortCircuitMW{}, "short", 100)

	downloadCalled := false
	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		downloadCalled = true
		return nil, nil
	}

	req := shttp.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if downloadCalled {
		t.Error("download should not be called when middleware short-circuits")
	}
	if resp.Status != 403 {
		t.Errorf("expected status 403, got %d", resp.Status)
	}
}

func TestMiddlewareManagerProcessException(t *testing.T) {
	m := NewMiddlewareManager(nil)

	// 添加一个将异常转换为响应的中间件
	m.AddMiddleware(&exceptionHandlerMW{}, "handler", 100)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return nil, serrors.ErrDownloadTimeout
	}

	req := shttp.MustNewRequest("https://example.com")
	resp, err := m.Download(context.Background(), downloadFunc, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != 504 {
		t.Errorf("expected status 504, got %d", resp.Status)
	}
}

func TestMiddlewareManagerProcessExceptionUnhandled(t *testing.T) {
	m := NewMiddlewareManager(nil)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return nil, serrors.ErrDownloadTimeout
	}

	req := shttp.MustNewRequest("https://example.com")
	_, err := m.Download(context.Background(), downloadFunc, req)
	if !errors.Is(err, serrors.ErrDownloadTimeout) {
		t.Errorf("expected ErrDownloadTimeout, got %v", err)
	}
}

func TestMiddlewareManagerNewRequestErrorPropagation(t *testing.T) {
	m := NewMiddlewareManager(nil)

	// 添加一个返回 NewRequestError 的中间件（模拟重试/重定向）
	newReq := shttp.MustNewRequest("https://example.com/retry")
	m.AddMiddleware(&newRequestMW{newReq: newReq}, "newreq", 100)

	downloadFunc := func(ctx context.Context, req *shttp.Request) (*shttp.Response, error) {
		return shttp.MustNewResponse(req.URL.String(), 500, shttp.WithRequest(req)), nil
	}

	req := shttp.MustNewRequest("https://example.com")
	_, err := m.Download(context.Background(), downloadFunc, req)

	// NewRequestError 应该被传播给调用方
	if !errors.Is(err, serrors.ErrNewRequest) {
		t.Fatalf("expected ErrNewRequest, got %v", err)
	}

	var newReqErr *serrors.NewRequestError
	if !errors.As(err, &newReqErr) {
		t.Fatal("should be able to extract NewRequestError")
	}
	if extractedReq, ok := newReqErr.Request.(*shttp.Request); ok {
		if extractedReq.URL.String() != "https://example.com/retry" {
			t.Errorf("expected retry URL, got %s", extractedReq.URL.String())
		}
	} else {
		t.Fatal("NewRequestError.Request should be *http.Request")
	}
}

func TestMiddlewareManagerCount(t *testing.T) {
	m := NewMiddlewareManager(nil)
	if m.Count() != 0 {
		t.Error("new manager should have 0 middlewares")
	}
	m.AddMiddleware(&middleware.BaseDownloaderMiddleware{}, "mw1", 100)
	m.AddMiddleware(&middleware.BaseDownloaderMiddleware{}, "mw2", 200)
	if m.Count() != 2 {
		t.Errorf("expected 2 middlewares, got %d", m.Count())
	}
}

// ============================================================================
// 测试辅助类型
// ============================================================================

type orderTrackingMW struct {
	middleware.BaseDownloaderMiddleware
	name  string
	order *[]string
}

func (m *orderTrackingMW) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	*m.order = append(*m.order, m.name+":request")
	return nil, nil
}

func (m *orderTrackingMW) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	*m.order = append(*m.order, m.name+":response")
	return response, nil
}

type shortCircuitMW struct {
	middleware.BaseDownloaderMiddleware
}

func (m *shortCircuitMW) ProcessRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	return shttp.MustNewResponse(request.URL.String(), 403), nil
}

type exceptionHandlerMW struct {
	middleware.BaseDownloaderMiddleware
}

func (m *exceptionHandlerMW) ProcessException(ctx context.Context, request *shttp.Request, err error) (*shttp.Response, error) {
	if errors.Is(err, serrors.ErrDownloadTimeout) {
		return shttp.MustNewResponse(request.URL.String(), 504), nil
	}
	return nil, nil
}

// newRequestMW 是一个测试用中间件，在 ProcessResponse 中返回 NewRequestError。
type newRequestMW struct {
	middleware.BaseDownloaderMiddleware
	newReq *shttp.Request
}

func (m *newRequestMW) ProcessResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	return nil, serrors.NewNewRequestError(m.newReq, "test")
}
