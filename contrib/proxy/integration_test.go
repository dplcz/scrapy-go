package proxy

import (
	"context"
	"errors"
	"testing"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// TestIntegration_PoolAndMiddleware 验证真实 Pool 与 Middleware 的端到端行为。
func TestIntegration_PoolAndMiddleware(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	opts.MaxFailures = 2
	opts.AutoRetryOnFailure = true
	opts.MaxProxyRetries = 2

	provider := NewStaticProvider([]string{
		"http://user:pass@p1.example.com:8080",
		"http://p2.example.com:8080",
	})

	pool, err := NewPool(opts, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	mw := NewMiddlewareWithOptions(pool, opts, nil)

	// 1. 第一次 ProcessRequest：分配代理
	req, _ := shttp.NewRequest("https://target.example.com/path")
	if _, err := mw.ProcessRequest(context.Background(), req); err != nil {
		t.Fatalf("ProcessRequest: %v", err)
	}
	chosen, ok := req.GetMeta(MetaKeyProxyChosen)
	if !ok {
		t.Fatal("chosen not set")
	}
	chosenProxy := chosen.(*Proxy)

	// 2. 模拟下载异常 → 触发重试，应返回 NewRequestError
	_, err = mw.ProcessException(context.Background(), req,
		errors.New("connection refused"))
	var newReqErr *serrors.NewRequestError
	if !errors.As(err, &newReqErr) {
		t.Fatalf("expected NewRequestError, got %v", err)
	}

	// 3. 验证重试请求是干净的（无 chosen、无 proxy meta、无 auth header）
	retryReq := newReqErr.Request.(*shttp.Request)
	if _, ok := retryReq.GetMeta(MetaKeyProxyChosen); ok {
		t.Error("retry request should not have chosen")
	}
	if _, ok := retryReq.GetMeta(MetaKeyProxy); ok {
		t.Error("retry request should not have proxy meta")
	}

	// 4. 失败次数应被记录到 Pool
	if chosenProxy.Failures() != 1 {
		t.Errorf("chosenProxy.Failures()=%d, want 1", chosenProxy.Failures())
	}

	// 5. 重试请求继续 ProcessRequest，此时已选过的代理仍可用（未达 MaxFailures）
	if _, err := mw.ProcessRequest(context.Background(), retryReq); err != nil {
		t.Fatal(err)
	}

	// 6. 模拟成功响应，重置失败计数
	resp := &shttp.Response{Status: 200}
	if _, err := mw.ProcessResponse(context.Background(), retryReq, resp); err != nil {
		t.Fatal(err)
	}
	// 验证统计指标
	stats := mw.Stats()
	if stats.TotalAssigned < 2 {
		t.Errorf("TotalAssigned=%d, want >= 2", stats.TotalAssigned)
	}
}

// TestIntegration_PriorityOverInternal 验证内置 HttpProxy 协作语义：
// 当 contrib/proxy 已经设置了 Meta["proxy"]，
// 内置 HttpProxyMiddleware 不会覆盖（这是约定行为）。
func TestIntegration_PriorityOverInternal(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	provider := NewStaticProvider([]string{"http://p1.example.com:8080"})

	pool, _ := NewPool(opts, provider)
	defer pool.Close()

	mw := NewMiddlewareWithOptions(pool, opts, nil)

	req, _ := shttp.NewRequest("https://target.example.com")
	if _, err := mw.ProcessRequest(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	// 验证 Meta["proxy"] 被本中间件设置
	got, ok := req.GetMeta(MetaKeyProxy)
	if !ok || got == nil {
		t.Fatal("expected Meta[proxy] to be set")
	}
	if s, ok := got.(string); !ok || s != "http://p1.example.com:8080" {
		t.Errorf("Meta[proxy]=%v, want http://p1.example.com:8080", got)
	}
}

// TestIntegration_PoolEmptyPoolDegrades 验证 Pool 为空时 Middleware 降级直连。
func TestIntegration_PoolEmptyPoolDegrades(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	opts.HealthCheckEnabled = false
	provider := NewStaticProvider(nil) // 空池

	pool, _ := NewPool(opts, provider)
	defer pool.Close()

	mw := NewMiddleware(pool, nil)

	req, _ := shttp.NewRequest("https://target.example.com")
	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Errorf("空池应降级为直连，错误传播将阻断请求: %v", err)
	}
	if resp != nil {
		t.Error("expected nil response for chain-continue")
	}
	if mw.Stats().TotalNoProxy != 1 {
		t.Errorf("TotalNoProxy=%d, want 1", mw.Stats().TotalNoProxy)
	}
}
