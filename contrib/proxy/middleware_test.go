package proxy

import (
	"context"
	"errors"
	"net/http"
	"testing"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// fakePool 是测试用的 Pool 实现。
type fakePool struct {
	getFunc      func(ctx context.Context) (*Proxy, error)
	markCalls    []markCall
	closeCalled  bool
	snapshotsRet []Snapshot
}

type markCall struct {
	proxy   *Proxy
	success bool
}

func (f *fakePool) Get(ctx context.Context) (*Proxy, error) {
	if f.getFunc != nil {
		return f.getFunc(ctx)
	}
	return nil, ErrNoProxy
}

func (f *fakePool) Mark(proxy *Proxy, success bool) {
	f.markCalls = append(f.markCalls, markCall{proxy: proxy, success: success})
}

func (f *fakePool) Refresh(_ context.Context) error { return nil }
func (f *fakePool) Snapshots() []Snapshot           { return f.snapshotsRet }
func (f *fakePool) Size() int                       { return 0 }
func (f *fakePool) Healthy() int                    { return 0 }
func (f *fakePool) Close() error                    { f.closeCalled = true; return nil }

func newReq(t *testing.T, rawURL string) *shttp.Request {
	t.Helper()
	r, err := shttp.NewRequest(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMiddleware_AssignProxy(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy.example.com:8080", Credentials: "dXNlcjpwYXNz"}
	fp := &fakePool{
		getFunc: func(_ context.Context) (*Proxy, error) { return pr, nil },
	}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com/path")
	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("ProcessRequest err: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response (continue chain), got %v", resp)
	}

	gotProxy, ok := req.GetMeta(MetaKeyProxy)
	if !ok || gotProxy != pr.URL {
		t.Errorf("Meta[proxy]=%v, want %s", gotProxy, pr.URL)
	}

	gotChosen, ok := req.GetMeta(MetaKeyProxyChosen)
	if !ok || gotChosen != pr {
		t.Errorf("Meta[chosen] not set correctly")
	}

	if got := req.Headers.Get("Proxy-Authorization"); got != "Basic dXNlcjpwYXNz" {
		t.Errorf("Proxy-Authorization=%q", got)
	}

	stats := mw.Stats()
	if stats.TotalAssigned != 1 {
		t.Errorf("TotalAssigned=%d, want 1", stats.TotalAssigned)
	}
}

func TestMiddleware_RespectUserSpecifiedProxy(t *testing.T) {
	t.Parallel()

	fp := &fakePool{
		getFunc: func(_ context.Context) (*Proxy, error) {
			t.Fatal("Pool.Get should not be called when user proxy is set")
			return nil, nil
		},
	}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	req.SetMeta(MetaKeyProxy, "http://user-specified:8080")

	if _, err := mw.ProcessRequest(context.Background(), req); err != nil {
		t.Fatalf("err: %v", err)
	}

	stats := mw.Stats()
	if stats.TotalSkipped != 1 {
		t.Errorf("TotalSkipped=%d, want 1", stats.TotalSkipped)
	}
}

func TestMiddleware_NoProxyAvailable(t *testing.T) {
	t.Parallel()

	fp := &fakePool{
		getFunc: func(_ context.Context) (*Proxy, error) { return nil, ErrNoProxy },
	}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Errorf("expected nil error (degrade to direct), got %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}

	stats := mw.Stats()
	if stats.TotalNoProxy != 1 {
		t.Errorf("TotalNoProxy=%d, want 1", stats.TotalNoProxy)
	}
}

func TestMiddleware_PoolClosedDegrades(t *testing.T) {
	t.Parallel()

	fp := &fakePool{
		getFunc: func(_ context.Context) (*Proxy, error) { return nil, ErrPoolClosed },
	}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	if _, err := mw.ProcessRequest(context.Background(), req); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestMiddleware_GetReturnsOtherError(t *testing.T) {
	t.Parallel()

	customErr := errors.New("custom failure")
	fp := &fakePool{
		getFunc: func(_ context.Context) (*Proxy, error) { return nil, customErr },
	}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	if _, err := mw.ProcessRequest(context.Background(), req); !errors.Is(err, customErr) {
		t.Errorf("expected customErr, got %v", err)
	}
}

func TestMiddleware_ProcessResponse_Success(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080"}
	fp := &fakePool{}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	req.SetMeta(MetaKeyProxyChosen, pr)

	resp := &shttp.Response{Status: 200}
	if _, err := mw.ProcessResponse(context.Background(), req, resp); err != nil {
		t.Fatalf("err: %v", err)
	}

	if len(fp.markCalls) != 1 || !fp.markCalls[0].success {
		t.Errorf("expected one success Mark call, got %+v", fp.markCalls)
	}

	// Meta[chosen] 应被消费
	if _, ok := req.GetMeta(MetaKeyProxyChosen); ok {
		t.Error("MetaKeyProxyChosen should be consumed")
	}
}

func TestMiddleware_ProcessResponse_5xxFails(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080"}
	fp := &fakePool{}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	req.SetMeta(MetaKeyProxyChosen, pr)

	resp := &shttp.Response{Status: 502}
	_, _ = mw.ProcessResponse(context.Background(), req, resp)

	if len(fp.markCalls) != 1 || fp.markCalls[0].success {
		t.Errorf("502 should mark failure, got %+v", fp.markCalls)
	}
}

func TestMiddleware_ProcessResponse_407Fails(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080"}
	fp := &fakePool{}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	req.SetMeta(MetaKeyProxyChosen, pr)

	resp := &shttp.Response{Status: 407}
	_, _ = mw.ProcessResponse(context.Background(), req, resp)

	if len(fp.markCalls) != 1 || fp.markCalls[0].success {
		t.Errorf("407 should mark failure, got %+v", fp.markCalls)
	}
}

func TestMiddleware_ProcessResponse_4xxIgnored(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080"}
	fp := &fakePool{}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	req.SetMeta(MetaKeyProxyChosen, pr)

	resp := &shttp.Response{Status: 404}
	_, _ = mw.ProcessResponse(context.Background(), req, resp)

	// 404 是业务错误，不影响代理评价
	if len(fp.markCalls) != 0 {
		t.Errorf("404 should not call Mark, got %+v", fp.markCalls)
	}
}

func TestMiddleware_ProcessResponse_NoChosenSkipped(t *testing.T) {
	t.Parallel()

	fp := &fakePool{}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com") // 没有 chosen
	resp := &shttp.Response{Status: 502}
	_, _ = mw.ProcessResponse(context.Background(), req, resp)

	if len(fp.markCalls) != 0 {
		t.Errorf("no chosen should skip Mark")
	}
}

func TestMiddleware_ProcessException_Retry(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080"}
	fp := &fakePool{}
	mw := NewMiddlewareWithOptions(fp, &Options{
		AutoRetryOnFailure: true,
		MaxProxyRetries:    3,
	}, nil)

	req := newReq(t, "https://target.example.com")
	req.SetMeta(MetaKeyProxyChosen, pr)

	resp, err := mw.ProcessException(context.Background(), req,
		errors.New("dial tcp: timeout"))

	if resp != nil {
		t.Error("expected nil response")
	}
	var newReqErr *serrors.NewRequestError
	if !errors.As(err, &newReqErr) {
		t.Fatalf("expected NewRequestError, got %v", err)
	}
	if newReqErr.Reason != "proxy_retry" {
		t.Errorf("Reason=%q", newReqErr.Reason)
	}

	newReq, ok := newReqErr.Request.(*shttp.Request)
	if !ok {
		t.Fatal("Request should be *shttp.Request")
	}
	if !newReq.DontFilter {
		t.Error("retry request should set DontFilter=true")
	}
	if got, _ := newReq.GetMeta(MetaKeyProxyRetries); got != 1 {
		t.Errorf("retries counter=%v, want 1", got)
	}
	if _, ok := newReq.GetMeta(MetaKeyProxy); ok {
		t.Error("retry request should clear Meta[proxy]")
	}
	if newReq.Headers.Get("Proxy-Authorization") != "" {
		t.Error("retry request should clear Proxy-Authorization")
	}

	// 失败应该被反馈到 Pool
	if len(fp.markCalls) != 1 || fp.markCalls[0].success {
		t.Errorf("expected failure Mark, got %+v", fp.markCalls)
	}
}

func TestMiddleware_ProcessException_MaxRetriesExceeded(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080"}
	fp := &fakePool{}
	mw := NewMiddlewareWithOptions(fp, &Options{
		AutoRetryOnFailure: true,
		MaxProxyRetries:    2,
	}, nil)

	req := newReq(t, "https://target.example.com")
	req.SetMeta(MetaKeyProxyChosen, pr)
	req.SetMeta(MetaKeyProxyRetries, 2) // 已达上限

	_, err := mw.ProcessException(context.Background(), req,
		errors.New("network down"))
	if err != nil {
		t.Errorf("expected nil error (let original error propagate), got %v", err)
	}
}

func TestMiddleware_ProcessException_AutoRetryDisabled(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080"}
	fp := &fakePool{}
	mw := NewMiddlewareWithOptions(fp, &Options{
		AutoRetryOnFailure: false,
	}, nil)

	req := newReq(t, "https://target.example.com")
	req.SetMeta(MetaKeyProxyChosen, pr)

	if _, err := mw.ProcessException(context.Background(), req,
		errors.New("err")); err != nil {
		t.Errorf("expected nil err, got %v", err)
	}

	// 仍应反馈失败
	if len(fp.markCalls) != 1 || fp.markCalls[0].success {
		t.Errorf("expected failure Mark, got %+v", fp.markCalls)
	}
}

// 编译期接口满足性验证
func TestMiddleware_ImplementsInterfaces(t *testing.T) {
	t.Parallel()

	mw := NewMiddleware(&fakePool{}, nil)

	// 通过类型断言验证接口实现
	var _ interface {
		ProcessRequest(context.Context, *shttp.Request) (*shttp.Response, error)
	} = mw
	var _ interface {
		ProcessResponse(context.Context, *shttp.Request, *shttp.Response) (*shttp.Response, error)
	} = mw
	var _ interface {
		ProcessException(context.Context, *shttp.Request, error) (*shttp.Response, error)
	} = mw
}

// 验证 Headers 为 nil 时也能正确注入 Proxy-Authorization
func TestMiddleware_NilHeadersHandling(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080", Credentials: "dXNlcjpwYXNz"}
	fp := &fakePool{
		getFunc: func(_ context.Context) (*Proxy, error) { return pr, nil },
	}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	// 强制清空 Headers
	req.Headers = nil

	if _, err := mw.ProcessRequest(context.Background(), req); err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := req.Headers.Get("Proxy-Authorization"); got != "Basic dXNlcjpwYXNz" {
		t.Errorf("Proxy-Authorization=%q", got)
	}
}

// 确保中间件本身不持有 Pool 所有权（不会自动 Close）
func TestMiddleware_DoesNotOwnPool(t *testing.T) {
	t.Parallel()

	fp := &fakePool{}
	_ = NewMiddleware(fp, nil)

	if fp.closeCalled {
		t.Error("middleware should not close pool")
	}
}

// 验证常量值，避免与文档不一致
func TestMiddleware_DefaultPriorityValue(t *testing.T) {
	t.Parallel()

	if DefaultPriority >= 750 {
		t.Errorf("DefaultPriority=%d should be < 750 (内置 HttpProxy)", DefaultPriority)
	}
}

// 防止 Stats 在并发场景下出现数据竞争
func TestMiddleware_StatsConcurrency(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080"}
	fp := &fakePool{
		getFunc: func(_ context.Context) (*Proxy, error) { return pr, nil },
	}
	mw := NewMiddleware(fp, nil)

	const N = 100
	done := make(chan struct{}, N)
	for i := 0; i < N; i++ {
		go func() {
			req := newReq(t, "https://target.example.com")
			_, _ = mw.ProcessRequest(context.Background(), req)
			done <- struct{}{}
		}()
	}
	for i := 0; i < N; i++ {
		<-done
	}

	stats := mw.Stats()
	if stats.TotalAssigned != N {
		t.Errorf("TotalAssigned=%d, want %d", stats.TotalAssigned, N)
	}
}

// 边界：未设置 chosen 时 Headers 不应被中间件破坏
func TestMiddleware_PreservesOriginalHeaders(t *testing.T) {
	t.Parallel()

	pr := &Proxy{URL: "http://proxy:8080"}
	fp := &fakePool{
		getFunc: func(_ context.Context) (*Proxy, error) { return pr, nil },
	}
	mw := NewMiddleware(fp, nil)

	req := newReq(t, "https://target.example.com")
	req.Headers = http.Header{}
	req.Headers.Set("User-Agent", "test-agent")

	_, _ = mw.ProcessRequest(context.Background(), req)
	if got := req.Headers.Get("User-Agent"); got != "test-agent" {
		t.Errorf("User-Agent overwritten: %q", got)
	}
}
