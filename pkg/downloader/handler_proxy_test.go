package downloader

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// ============================================================================
// 本地 HTTP CONNECT 代理（测试用）
// ============================================================================
//
// 实现一个最小化的 HTTP 代理服务器，仅支持 CONNECT 隧道方法（HTTPS 代理场景）。
// 工作流程：
//  1. 接受客户端 TCP 连接
//  2. 读取首行：CONNECT host:port HTTP/1.1
//  3. 拨号到目标 host:port，回写 "HTTP/1.1 200 Connection Established\r\n\r\n"
//  4. 双向 io.Copy，转发原始字节流（客户端会在隧道之上做 TLS 握手）
//
// 同时也支持普通 HTTP 代理（绝对 URL 形式的 GET/POST），方便扩展。

// connectProxy 是一个面向测试的 HTTP CONNECT 代理。
type connectProxy struct {
	listener net.Listener
	addr     string

	// connectCount 统计已处理的 CONNECT 请求数，用于断言代理被实际使用。
	connectCount atomic.Int64

	// httpCount 统计普通 HTTP 代理转发请求数。
	httpCount atomic.Int64

	// requireAuth 不为空时启用 Basic 代理认证，等于该值才放行。
	requireAuth string

	wg     sync.WaitGroup
	closed atomic.Bool
}

// newConnectProxy 启动一个本地代理，监听随机端口。
func newConnectProxy(t *testing.T) *connectProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:7890")
	if err != nil {
		t.Fatalf("proxy listen failed: %v", err)
	}
	p := &connectProxy{
		listener: ln,
		addr:     ln.Addr().String(),
	}
	p.wg.Add(1)
	go p.serve()
	return p
}

// URL 返回代理的 http URL，可直接用于 http.ProxyURL / Request.Meta["proxy"]。
func (p *connectProxy) URL() string {
	return "http://" + p.addr
}

func (p *connectProxy) Close() {
	if p.closed.Swap(true) {
		return
	}
	_ = p.listener.Close()
	p.wg.Wait()
}

func (p *connectProxy) serve() {
	defer p.wg.Done()
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			if p.closed.Load() {
				return
			}
			return
		}
		go p.handle(conn)
	}
}

func (p *connectProxy) handle(client net.Conn) {
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(15 * time.Second))

	br := bufio.NewReader(client)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}

	// 校验代理认证（如启用）
	if p.requireAuth != "" {
		got := req.Header.Get("Proxy-Authorization")
		if got != p.requireAuth {
			_, _ = io.WriteString(client,
				"HTTP/1.1 407 Proxy Authentication Required\r\n"+
					"Proxy-Authenticate: Basic realm=\"test\"\r\n"+
					"Content-Length: 0\r\n\r\n")
			return
		}
	}

	if req.Method == http.MethodConnect {
		p.handleConnect(client, br, req)
		return
	}

	p.handleHTTP(client, req)
}

// handleConnect 处理 CONNECT 隧道：拨号目标，回 200，然后双向透传。
func (p *connectProxy) handleConnect(client net.Conn, br *bufio.Reader, req *http.Request) {
	// req.Host 形如 "example.com:443"
	target, err := net.DialTimeout("tcp", req.Host, 10*time.Second)
	if err != nil {
		_, _ = io.WriteString(client,
			"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer target.Close()

	if _, err := io.WriteString(client,
		"HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	p.connectCount.Add(1)

	// 清除 deadline，进入透传
	_ = client.SetReadDeadline(time.Time{})

	// 双向透传。
	//
	// ⚠️ 关键修复：bufio.Reader 在 http.ReadRequest 时可能已经从底层 conn
	// 预读了 CONNECT 报文之后的字节（典型场景：Go 客户端 pipeline 把 TLS
	// ClientHello 紧跟在 CONNECT 之后发出，被 bufio 一并读入 4KB 缓冲）。
	// 因此 client → target 方向必须从 br（而非裸 client）读取，否则会丢失
	// 缓冲中那段字节，导致 target 收到不完整的 ClientHello，TLS 握手失败，
	// 客户端 transport 最终报 "malformed HTTP response"。
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(target, br)
		if tc, ok := target.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, target)
		if cc, ok := client.(*net.TCPConn); ok {
			_ = cc.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

// handleHTTP 处理普通 HTTP 代理（绝对 URL 形式）。
func (p *connectProxy) handleHTTP(client net.Conn, req *http.Request) {
	if req.URL == nil || req.URL.Host == "" {
		_, _ = io.WriteString(client,
			"HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
		return
	}

	// 清理 hop-by-hop 头
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Connection")
	req.RequestURI = ""

	tr := &http.Transport{Proxy: nil}
	defer tr.CloseIdleConnections()

	resp, err := tr.RoundTrip(req)
	if err != nil {
		_, _ = io.WriteString(client,
			"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer resp.Body.Close()
	p.httpCount.Add(1)

	_ = resp.Write(client)
}

// ============================================================================
// 测试：HTTPDownloadHandler 通过 HTTP 代理访问 HTTPS 目标
// ============================================================================

// newInsecureHTTPHandler 返回一个 TLS 校验已关闭的 HTTPDownloadHandler，
// 用于测试自签名证书的 httptest TLS server。
func newInsecureHTTPHandler(timeout time.Duration) *HTTPDownloadHandler {
	h := NewHTTPDownloadHandler(timeout)
	// 在测试中允许跳过自签名证书校验
	if h.transport.TLSClientConfig == nil {
		h.transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	} else {
		h.transport.TLSClientConfig.InsecureSkipVerify = true
	}
	return h
}

// TestHTTPDownloadHandler_ProxyHTTPSViaConnect 验证通过 Request.Meta["proxy"]
// 设置 HTTP 代理后，能够正确访问 HTTPS 目标（CONNECT 隧道）。
func TestHTTPDownloadHandler_ProxyHTTPSViaConnect(t *testing.T) {
	// 1. 启动 HTTPS 目标服务器
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "hello via proxy: %s %s", r.Method, r.URL.Path)
	}))
	defer target.Close()

	// 2. 启动本地 HTTP CONNECT 代理
	proxy := newConnectProxy(t)
	defer proxy.Close()

	// 3. 构造 handler 并发起请求
	handler := newInsecureHTTPHandler(10 * time.Second)
	defer handler.Close()

	req, err := shttp.NewRequest(target.URL + "/foo")
	if err != nil {
		t.Fatal(err)
	}
	req.SetMeta("proxy", proxy.URL())

	resp, err := handler.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("download via proxy failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	expected := "hello via proxy: GET /foo"
	if string(resp.Body) != expected {
		t.Errorf("unexpected body: got %q, want %q", string(resp.Body), expected)
	}

	if got := proxy.connectCount.Load(); got < 1 {
		t.Errorf("expected at least 1 CONNECT through proxy, got %d", got)
	}
}

// TestHTTPDownloadHandler_ProxyHTTPSPOST 验证通过代理发送带 body 的 POST 请求。
func TestHTTPDownloadHandler_ProxyHTTPSPOST(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "echo: %s", body)
	}))
	defer target.Close()

	proxy := newConnectProxy(t)
	defer proxy.Close()

	handler := newInsecureHTTPHandler(10 * time.Second)
	defer handler.Close()

	req, err := shttp.NewRequest(target.URL+"/echo",
		shttp.WithMethod("POST"),
		shttp.WithBody([]byte("payload-via-proxy")),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.SetMeta("proxy", proxy.URL())

	resp, err := handler.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("download via proxy failed: %v", err)
	}

	if resp.Status != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.Status)
	}

	if string(resp.Body) != "echo: payload-via-proxy" {
		t.Errorf("unexpected body: %q", string(resp.Body))
	}

	if got := proxy.connectCount.Load(); got < 1 {
		t.Errorf("expected at least 1 CONNECT through proxy, got %d", got)
	}
}

// TestHTTPDownloadHandler_ProxyConcurrent 验证并发请求场景下代理依然工作。
func TestHTTPDownloadHandler_ProxyConcurrent(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ok-%s", r.URL.Path)
	}))
	defer target.Close()

	proxy := newConnectProxy(t)
	defer proxy.Close()

	handler := newInsecureHTTPHandler(10 * time.Second)
	defer handler.Close()

	const N = 8
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			req, err := shttp.NewRequest(fmt.Sprintf("%s/p%d", target.URL, i))
			if err != nil {
				errCh <- err
				return
			}
			req.SetMeta("proxy", proxy.URL())
			resp, err := handler.Download(context.Background(), req)
			if err != nil {
				errCh <- fmt.Errorf("req %d: %w", i, err)
				return
			}
			want := fmt.Sprintf("ok-/p%d", i)
			if string(resp.Body) != want {
				errCh <- fmt.Errorf("req %d body mismatch: got %q want %q",
					i, string(resp.Body), want)
				return
			}
			errCh <- nil
		}()
	}
	for i := 0; i < N; i++ {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}

// TestHTTPDownloadHandler_ProxyDisabledViaMetaNil 验证 Meta["proxy"] = nil
// （或字符串解析失败）等不合法值时，handler 不会走代理。
func TestHTTPDownloadHandler_NoProxyWhenMetaInvalid(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "direct")
	}))
	defer target.Close()

	proxy := newConnectProxy(t)
	defer proxy.Close()

	handler := newInsecureHTTPHandler(10 * time.Second)
	defer handler.Close()

	req, err := shttp.NewRequest(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	// 非字符串值，会被 getProxyURL 当作未设置
	req.SetMeta("proxy", 123)

	resp, err := handler.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("direct download failed: %v", err)
	}
	if string(resp.Body) != "direct" {
		t.Errorf("unexpected body: %q", string(resp.Body))
	}
	if proxy.connectCount.Load() != 0 {
		t.Errorf("proxy should not be hit, got CONNECT count=%d",
			proxy.connectCount.Load())
	}
}

// ============================================================================
// 对照测试：直接使用 net/http.Transport + 同一个代理
// ============================================================================
//
// 这个对照测试用于排查 handler 实现 bug。如果该测试通过，但上面的 handler
// 测试失败，则说明问题出在 HTTPDownloadHandler 本身（例如手工构造 *http.Request
// 时遗漏了某些字段，导致 transport 走错分支）。

func TestRawNetHTTP_ProxyHTTPS_Sanity(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "raw-net-http")
	}))
	defer target.Close()

	proxy := newConnectProxy(t)
	defer proxy.Close()

	pURL, err := url.Parse(proxy.URL())
	if err != nil {
		t.Fatal(err)
	}

	tr := &http.Transport{
		Proxy:           http.ProxyURL(pURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}

	resp, err := client.Get(target.URL + "/x")
	if err != nil {
		t.Fatalf("raw client.Get failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "raw-net-http") {
		t.Errorf("unexpected body: %q", string(body))
	}
	if proxy.connectCount.Load() < 1 {
		t.Errorf("expected CONNECT through proxy, got 0")
	}
}

func TestRealGet(t *testing.T) {
	p := newConnectProxy(t)
	defer p.Close()

	//pUrl, _ := url.Parse(p.URL())

	//transport := &http.Transport{
	//	Proxy: http.ProxyURL(pUrl),
	//}
	h := NewHTTPDownloadHandler(100 * time.Second)
	req, _ := shttp.NewRequest("https://www.example.com", shttp.WithHeader("A", "b"))
	req.SetMeta("proxy", p.URL())

	resp, err := h.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("client.Get failed: %v", err)
	}
	//body, _ := io.ReadAll(resp.Body)

	if resp.Status != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.Status)
	}
	if p.connectCount.Load() < 1 {
		t.Errorf("expected CONNECT through proxy, got 0")
	}

}
