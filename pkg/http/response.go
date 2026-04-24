package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"scrapy-go/pkg/selector"
)

// Response 表示一个 HTTP 响应。
// 对应 Scrapy 的 scrapy.http.Response 类。
type Response struct {
	// URL 是响应的最终 URL（可能经过重定向）。
	URL *url.URL

	// Status 是 HTTP 状态码。
	Status int

	// Headers 是 HTTP 响应头。
	Headers http.Header

	// Body 是响应体的原始字节。
	Body []byte

	// Request 是关联的原始请求。
	Request *Request

	// Flags 是响应标记列表，用于日志和调试。
	Flags []string

	// Certificate 是 TLS 证书信息（可选）。
	Certificate any

	// IPAddress 是服务器 IP 地址（可选）。
	IPAddress string

	// Protocol 是使用的协议（如 "HTTP/1.1"、"h2"）。
	Protocol string
}

// ResponseOption 是 Response 的可选配置函数。
type ResponseOption func(*Response)

// NewResponse 创建一个新的 Response。
func NewResponse(rawURL string, status int, opts ...ResponseOption) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	resp := &Response{
		URL:     u,
		Status:  status,
		Headers: make(http.Header),
	}

	for _, opt := range opts {
		opt(resp)
	}

	return resp, nil
}

// MustNewResponse 创建一个新的 Response，如果 URL 无效则 panic。
func MustNewResponse(rawURL string, status int, opts ...ResponseOption) *Response {
	resp, err := NewResponse(rawURL, status, opts...)
	if err != nil {
		panic(err)
	}
	return resp
}

// ============================================================================
// Response Options
// ============================================================================

// WithResponseHeaders 设置响应头。
func WithResponseHeaders(headers http.Header) ResponseOption {
	return func(r *Response) {
		r.Headers = headers.Clone()
	}
}

// WithResponseBody 设置响应体。
func WithResponseBody(body []byte) ResponseOption {
	return func(r *Response) {
		r.Body = body
	}
}

// WithRequest 设置关联的原始请求。
func WithRequest(request *Request) ResponseOption {
	return func(r *Response) {
		r.Request = request
	}
}

// WithResponseFlags 设置响应标记。
func WithResponseFlags(flags ...string) ResponseOption {
	return func(r *Response) {
		r.Flags = flags
	}
}

// WithProtocol 设置协议信息。
func WithProtocol(protocol string) ResponseOption {
	return func(r *Response) {
		r.Protocol = protocol
	}
}

// WithIPAddress 设置服务器 IP 地址。
func WithIPAddress(ip string) ResponseOption {
	return func(r *Response) {
		r.IPAddress = ip
	}
}

// ============================================================================
// Response 方法
// ============================================================================

// String 返回响应的字符串表示，格式为 "<STATUS URL>"。
func (r *Response) String() string {
	return fmt.Sprintf("<%d %s>", r.Status, r.URL.String())
}

// Text 返回响应体的文本内容（UTF-8 解码）。
func (r *Response) Text() string {
	return string(r.Body)
}

// JSON 将响应体解析为 JSON 并存储到 v 中。
func (r *Response) JSON(v any) error {
	return json.Unmarshal(r.Body, v)
}

// URLJoin 将相对 URL 解析为绝对 URL。
// 对应 Scrapy Response 的 urljoin 方法。
func (r *Response) URLJoin(ref string) (string, error) {
	refURL, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("invalid reference URL %q: %w", ref, err)
	}
	return r.URL.ResolveReference(refURL).String(), nil
}

// MustURLJoin 将相对 URL 解析为绝对 URL，如果解析失败则 panic。
func (r *Response) MustURLJoin(ref string) string {
	result, err := r.URLJoin(ref)
	if err != nil {
		panic(err)
	}
	return result
}

// Copy 返回响应的浅拷贝。
func (r *Response) Copy() *Response {
	newResp := *r

	// 深拷贝 Headers
	if r.Headers != nil {
		newResp.Headers = r.Headers.Clone()
	}

	// 深拷贝 Body
	if r.Body != nil {
		newResp.Body = make([]byte, len(r.Body))
		copy(newResp.Body, r.Body)
	}

	// 深拷贝 Flags
	if r.Flags != nil {
		newResp.Flags = make([]string, len(r.Flags))
		copy(newResp.Flags, r.Flags)
	}

	return &newResp
}

// Follow 创建一个跟踪链接的新请求。
// 对应 Scrapy Response 的 follow 方法。
func (r *Response) Follow(refURL string, opts ...RequestOption) (*Request, error) {
	absURL, err := r.URLJoin(refURL)
	if err != nil {
		return nil, err
	}
	return NewRequest(absURL, opts...)
}

// MustFollow 创建一个跟踪链接的新请求，如果 URL 无效则 panic。
func (r *Response) MustFollow(refURL string, opts ...RequestOption) *Request {
	req, err := r.Follow(refURL, opts...)
	if err != nil {
		panic(err)
	}
	return req
}

// Meta 返回关联请求的 Meta。
// 如果没有关联请求，返回 nil。
// 这是一个便捷方法，对应 Scrapy Response 的 meta 属性。
func (r *Response) GetMeta(key string) (any, bool) {
	if r.Request == nil {
		return nil, false
	}
	return r.Request.GetMeta(key)
}

// CbKwargs 返回关联请求的 CbKwargs。
// 如果没有关联请求，返回 nil。
func (r *Response) GetCbKwargs() map[string]any {
	if r.Request == nil {
		return nil
	}
	return r.Request.CbKwargs
}

// ============================================================================
// HTML 选择器方法
// ============================================================================

// Selector 返回响应体的 Selector，用于 CSS 和 XPath 查询。
// 对应 Scrapy Response 的 selector 属性。
//
// 用法：
//
//	sel := response.Selector()
//	titles := sel.CSS("h1.title").GetAll()
func (r *Response) Selector() *selector.Selector {
	return selector.NewFromBytes(r.Body)
}

// CSS 使用 CSS 选择器查询响应体，返回匹配的 List。
// 这是 response.Selector().CSS(query) 的快捷方式。
//
// 对应 Scrapy Response 的 css() 方法。
//
// 用法：
//
//	quotes := response.CSS("div.quote")
//	texts := response.CSS("span.text::text").GetAll()
func (r *Response) CSS(query string) selector.List {
	return r.Selector().CSS(query)
}

// CSSAttr 使用 CSS 选择器查询并提取指定属性值。
// 这是 Scrapy 中 css("a::attr(href)") 的 Go 等价实现。
//
// 用法：
//
//	links := response.CSSAttr("a", "href").GetAll()
func (r *Response) CSSAttr(query, attr string) selector.List {
	return r.Selector().CSSAttr(query, attr)
}

// XPath 使用 XPath 表达式查询响应体，返回匹配的 List。
// 这是 response.Selector().XPath(expr) 的快捷方式。
//
// 对应 Scrapy Response 的 xpath() 方法。
//
// 用法：
//
//	quotes := response.XPath("//div[@class='quote']")
//	texts := response.XPath("//span[@class='text']/text()").GetAll()
func (r *Response) XPath(expr string) selector.List {
	return r.Selector().XPath(expr)
}
