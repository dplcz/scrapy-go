// Package http 定义了 scrapy-go 框架的 HTTP 请求和响应模型。
//
// 这是框架最底层的数据模型包，不依赖任何其他框架包。
// 对应 Scrapy Python 版本中 scrapy.http 模块的功能。
package http

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// noCallbackSentinel 是 NoCallback 哨兵值的内部标记类型。
type noCallbackSentinel struct{}

// NoCallback 是一个哨兵值，用于显式标记请求不需要回调函数。
// 当 Request.Callback 设置为 NoCallback 时，中间件和 Engine 可以识别
// 该请求不需要调用 Spider.Parse 作为默认回调。
//
// 对齐 Scrapy 的 NO_CALLBACK 哨兵值。
//
// 用法：
//
//	req, _ := http.NewRequest("https://example.com",
//	    http.WithCallback(http.NoCallback),
//	)
var NoCallback CallbackFunc = noCallbackSentinel{}

// IsNoCallback 检查给定的回调是否为 NoCallback 哨兵值。
func IsNoCallback(cb CallbackFunc) bool {
	_, ok := cb.(noCallbackSentinel)
	return ok
}

// CallbackFunc 定义响应回调函数类型。
// 使用 any 类型避免与 spider 包的循环依赖，实际类型为：
//
//	func(ctx context.Context, response *Response) ([]Output, error)
type CallbackFunc = any

// ErrbackFunc 定义错误回调函数类型。
// 使用 any 类型避免与 spider 包的循环依赖，实际类型为：
//
//	func(ctx context.Context, err error, request *Request) ([]Output, error)
type ErrbackFunc = any

// Request 表示一个 HTTP 请求。
// 对应 Scrapy 的 scrapy.http.Request 类。
type Request struct {
	// URL 是请求的目标地址。
	URL *url.URL

	// Method 是 HTTP 方法，默认为 "GET"。
	Method string

	// Headers 是 HTTP 请求头。
	Headers http.Header

	// Body 是请求体。
	Body []byte

	// Cookies 是请求携带的 Cookie。
	Cookies []*http.Cookie

	// Meta 是请求元数据，用于在组件间传递上下文信息。
	// 例如中间件可以通过 Meta 传递代理地址、重试次数等。
	Meta map[string]any

	// Priority 是调度优先级，值越大优先级越高。
	// 内置调度器会优先处理高优先级的请求。
	Priority int

	// DontFilter 为 true 时跳过去重过滤。
	// 通过 start_urls 生成的初始请求默认为 true。
	DontFilter bool

	// Callback 是响应回调函数。
	// 为 nil 时使用 Spider.Parse 作为默认回调。
	Callback CallbackFunc

	// Errback 是错误回调函数。
	// 为 nil 时错误将被记录到日志。
	Errback ErrbackFunc

	// Flags 是请求标记列表，用于日志和调试。
	Flags []string

	// CbKwargs 是传递给回调函数的额外参数。
	CbKwargs map[string]any

	// Encoding 是请求体的编码，默认为 "utf-8"。
	Encoding string
}

// RequestOption 是 Request 的可选配置函数（Functional Options 模式）。
type RequestOption func(*Request)

// NewRequest 创建一个新的 Request。
// rawURL 是请求的目标 URL，必须包含 scheme（如 http:// 或 https://）。
// opts 是可选的配置函数。
func NewRequest(rawURL string, opts ...RequestOption) (*Request, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	if u.Scheme == "" {
		return nil, fmt.Errorf("missing scheme in request URL: %s", rawURL)
	}

	r := &Request{
		URL:      u,
		Method:   "GET",
		Headers:  make(http.Header),
		Encoding: "utf-8",
	}

	for _, opt := range opts {
		opt(r)
	}

	// 规范化 Method
	r.Method = strings.ToUpper(r.Method)

	return r, nil
}

// MustNewRequest 创建一个新的 Request，如果 URL 无效则 panic。
// 仅用于确定 URL 有效的场景（如硬编码的 URL）。
func MustNewRequest(rawURL string, opts ...RequestOption) *Request {
	r, err := NewRequest(rawURL, opts...)
	if err != nil {
		panic(err)
	}
	return r
}

// ============================================================================
// Functional Options
// ============================================================================

// WithMethod 设置 HTTP 方法。
func WithMethod(method string) RequestOption {
	return func(r *Request) {
		r.Method = strings.ToUpper(method)
	}
}

// WithHeaders 设置请求头。
func WithHeaders(headers http.Header) RequestOption {
	return func(r *Request) {
		r.Headers = headers.Clone()
	}
}

// WithHeader 设置单个请求头。
func WithHeader(key, value string) RequestOption {
	return func(r *Request) {
		r.Headers.Set(key, value)
	}
}

// WithBody 设置请求体。
func WithBody(body []byte) RequestOption {
	return func(r *Request) {
		r.Body = body
	}
}

// WithCookies 设置 Cookie。
func WithCookies(cookies []*http.Cookie) RequestOption {
	return func(r *Request) {
		r.Cookies = cookies
	}
}

// WithMeta 设置元数据。
func WithMeta(meta map[string]any) RequestOption {
	return func(r *Request) {
		r.Meta = meta
	}
}

// WithPriority 设置调度优先级。
func WithPriority(priority int) RequestOption {
	return func(r *Request) {
		r.Priority = priority
	}
}

// WithDontFilter 设置是否跳过去重过滤。
func WithDontFilter(dontFilter bool) RequestOption {
	return func(r *Request) {
		r.DontFilter = dontFilter
	}
}

// WithCallback 设置响应回调函数。
func WithCallback(callback CallbackFunc) RequestOption {
	return func(r *Request) {
		r.Callback = callback
	}
}

// WithErrback 设置错误回调函数。
func WithErrback(errback ErrbackFunc) RequestOption {
	return func(r *Request) {
		r.Errback = errback
	}
}

// WithFlags 设置请求标记。
func WithFlags(flags ...string) RequestOption {
	return func(r *Request) {
		r.Flags = flags
	}
}

// WithCbKwargs 设置传递给回调的额外参数。
func WithCbKwargs(kwargs map[string]any) RequestOption {
	return func(r *Request) {
		r.CbKwargs = kwargs
	}
}

// WithEncoding 设置请求体编码。
func WithEncoding(encoding string) RequestOption {
	return func(r *Request) {
		r.Encoding = encoding
	}
}

// WithRawBody 设置原始请求体（字节切片）。
// 这是 WithBody 的别名，语义更明确地表示设置原始字节内容。
func WithRawBody(body []byte) RequestOption {
	return func(r *Request) {
		r.Body = body
	}
}

// WithBasicAuth 设置 HTTP Basic Authentication 请求头。
// 等价于设置 Authorization: Basic base64(user:pass) 头。
func WithBasicAuth(user, pass string) RequestOption {
	return func(r *Request) {
		if r.Meta == nil {
			r.Meta = make(map[string]any)
		}
		r.Meta["http_user"] = user
		r.Meta["http_pass"] = pass
	}
}

// WithUserAgent 设置 User-Agent 请求头。
func WithUserAgent(ua string) RequestOption {
	return func(r *Request) {
		r.Headers.Set("User-Agent", ua)
	}
}

// WithFormData 设置表单数据作为请求体。
// 自动设置 Content-Type 为 application/x-www-form-urlencoded。
// 如果当前方法为 GET，则将表单数据编码为查询参数。
func WithFormData(formdata map[string][]string) RequestOption {
	return func(r *Request) {
		encoded := url.Values(formdata).Encode()
		if strings.ToUpper(r.Method) == "GET" {
			if r.URL != nil {
				if r.URL.RawQuery != "" {
					r.URL.RawQuery += "&" + encoded
				} else {
					r.URL.RawQuery = encoded
				}
			}
		} else {
			r.Body = []byte(encoded)
			r.Headers.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
}

// ============================================================================
// Request 方法
// ============================================================================

// String 返回请求的字符串表示，格式为 "<METHOD URL>"。
func (r *Request) String() string {
	return fmt.Sprintf("<%s %s>", r.Method, r.URL.String())
}

// Copy 返回请求的浅拷贝。
func (r *Request) Copy() *Request {
	newReq := *r

	// 深拷贝 Headers
	if r.Headers != nil {
		newReq.Headers = r.Headers.Clone()
	}

	// 深拷贝 Body
	if r.Body != nil {
		newReq.Body = make([]byte, len(r.Body))
		copy(newReq.Body, r.Body)
	}

	// 深拷贝 Cookies
	if r.Cookies != nil {
		newReq.Cookies = make([]*http.Cookie, len(r.Cookies))
		for i, c := range r.Cookies {
			cc := *c
			newReq.Cookies[i] = &cc
		}
	}

	// 深拷贝 Meta
	if r.Meta != nil {
		newReq.Meta = make(map[string]any, len(r.Meta))
		for k, v := range r.Meta {
			newReq.Meta[k] = v
		}
	}

	// 深拷贝 Flags
	if r.Flags != nil {
		newReq.Flags = make([]string, len(r.Flags))
		copy(newReq.Flags, r.Flags)
	}

	// 深拷贝 CbKwargs
	if r.CbKwargs != nil {
		newReq.CbKwargs = make(map[string]any, len(r.CbKwargs))
		for k, v := range r.CbKwargs {
			newReq.CbKwargs[k] = v
		}
	}

	return &newReq
}

// Replace 创建一个新的 Request，使用当前请求的属性作为默认值，
// 并应用给定的选项覆盖。
func (r *Request) Replace(opts ...RequestOption) *Request {
	newReq := r.Copy()
	for _, opt := range opts {
		opt(newReq)
	}
	return newReq
}

// GetMeta 安全地获取 Meta 中的值。
func (r *Request) GetMeta(key string) (any, bool) {
	if r.Meta == nil {
		return nil, false
	}
	v, ok := r.Meta[key]
	return v, ok
}

// SetMeta 安全地设置 Meta 中的值。
func (r *Request) SetMeta(key string, value any) {
	if r.Meta == nil {
		r.Meta = make(map[string]any)
	}
	r.Meta[key] = value
}
