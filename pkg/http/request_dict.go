// Package http 定义了 scrapy-go 框架的 HTTP 请求和响应模型。
package http

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
)

// ============================================================================
// Request 序列化（ToDict / FromDict）
// ============================================================================

// ToDict 将 Request 转换为可序列化的 map[string]any 字典。
//
// 用于磁盘队列持久化、断点续爬等场景。
// 使用 FromDict 可将字典转换回 Request 对象。
//
// 如果提供了 callbackName/errbackName，将作为 Callback/Errback 的字符串标识
// 存入字典（用于跨进程恢复）。Go 中函数不可序列化，因此需要通过
// CallbackRegistry 注册表模式将函数映射为字符串名称。
//
// 对齐 Scrapy 的 Request.to_dict() 方法。
//
// 用法：
//
//	d := req.ToDict("parse_detail", "handle_error")
//	jsonBytes, _ := json.Marshal(d)
//
//	// 恢复
//	var d2 map[string]any
//	json.Unmarshal(jsonBytes, &d2)
//	req2, _ := http.FromDict(d2, registry)
func (r *Request) ToDict(callbackName, errbackName string) map[string]any {
	d := map[string]any{
		"url":    r.URL.String(),
		"method": r.Method,
	}

	// Headers — 转换为 map[string][]string 以便 JSON 序列化
	if r.Headers != nil && len(r.Headers) > 0 {
		headers := make(map[string][]string, len(r.Headers))
		for k, vs := range r.Headers {
			headers[k] = vs
		}
		d["headers"] = headers
	}

	// Body — 使用 base64 编码以支持二进制内容
	if len(r.Body) > 0 {
		d["body"] = base64.StdEncoding.EncodeToString(r.Body)
	}

	// Cookies — 转换为可序列化的 map 切片
	if len(r.Cookies) > 0 {
		cookies := make([]map[string]any, 0, len(r.Cookies))
		for _, c := range r.Cookies {
			cm := map[string]any{
				"name":  c.Name,
				"value": c.Value,
			}
			if c.Domain != "" {
				cm["domain"] = c.Domain
			}
			if c.Path != "" {
				cm["path"] = c.Path
			}
			if c.Secure {
				cm["secure"] = true
			}
			if c.HttpOnly {
				cm["httponly"] = true
			}
			cookies = append(cookies, cm)
		}
		d["cookies"] = cookies
	}

	// Meta — 仅序列化可 JSON 编码的值，跳过不可序列化的值（如函数、channel 等）
	if r.Meta != nil && len(r.Meta) > 0 {
		meta := make(map[string]any, len(r.Meta))
		for k, v := range r.Meta {
			if isJSONSerializable(v) {
				meta[k] = v
			}
		}
		if len(meta) > 0 {
			d["meta"] = meta
		}
	}

	// Priority
	if r.Priority != 0 {
		d["priority"] = r.Priority
	}

	// DontFilter
	if r.DontFilter {
		d["dont_filter"] = true
	}

	// Callback/Errback — 存储为字符串名称
	if callbackName != "" {
		d["callback"] = callbackName
	}
	if errbackName != "" {
		d["errback"] = errbackName
	}

	// Flags
	if len(r.Flags) > 0 {
		d["flags"] = r.Flags
	}

	// CbKwargs
	if r.CbKwargs != nil && len(r.CbKwargs) > 0 {
		cbKwargs := make(map[string]any, len(r.CbKwargs))
		for k, v := range r.CbKwargs {
			if isJSONSerializable(v) {
				cbKwargs[k] = v
			}
		}
		if len(cbKwargs) > 0 {
			d["cb_kwargs"] = cbKwargs
		}
	}

	// Encoding
	if r.Encoding != "" && r.Encoding != "utf-8" {
		d["encoding"] = r.Encoding
	}

	return d
}

// FromDict 从字典创建 Request 对象。
//
// 如果提供了 CallbackRegistry，将尝试通过字典中的 callback/errback 字符串名称
// 恢复对应的函数引用。registry 可以为 nil（此时不恢复 Callback/Errback）。
//
// 对齐 Scrapy 的 request_from_dict() 函数。
//
// 用法：
//
//	registry := http.NewCallbackRegistry()
//	registry.Register("parse_detail", spider.ParseDetail)
//
//	req, err := http.FromDict(d, registry)
func FromDict(d map[string]any, registry *CallbackRegistry) (*Request, error) {
	// URL（必需）
	rawURL, ok := d["url"].(string)
	if !ok || rawURL == "" {
		return nil, fmt.Errorf("missing or invalid 'url' in dict")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	req := &Request{
		URL:      u,
		Method:   "GET",
		Headers:  make(http.Header),
		Encoding: "utf-8",
	}

	// Method
	if method, ok := d["method"].(string); ok && method != "" {
		req.Method = method
	}

	// Headers
	if headers, ok := d["headers"]; ok {
		switch h := headers.(type) {
		case map[string]any:
			for k, v := range h {
				switch val := v.(type) {
				case string:
					req.Headers.Set(k, val)
				case []any:
					for _, item := range val {
						if s, ok := item.(string); ok {
							req.Headers.Add(k, s)
						}
					}
				case []string:
					for _, s := range val {
						req.Headers.Add(k, s)
					}
				}
			}
		case map[string][]string:
			for k, vs := range h {
				for _, v := range vs {
					req.Headers.Add(k, v)
				}
			}
		}
	}

	// Body — base64 解码
	if body, ok := d["body"].(string); ok && body != "" {
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 body: %w", err)
		}
		req.Body = decoded
	}

	// Cookies
	if cookies, ok := d["cookies"]; ok {
		switch cs := cookies.(type) {
		case []any:
			for _, item := range cs {
				if cm, ok := item.(map[string]any); ok {
					cookie := &http.Cookie{}
					if name, ok := cm["name"].(string); ok {
						cookie.Name = name
					}
					if value, ok := cm["value"].(string); ok {
						cookie.Value = value
					}
					if domain, ok := cm["domain"].(string); ok {
						cookie.Domain = domain
					}
					if path, ok := cm["path"].(string); ok {
						cookie.Path = path
					}
					if secure, ok := cm["secure"].(bool); ok {
						cookie.Secure = secure
					}
					if httpOnly, ok := cm["httponly"].(bool); ok {
						cookie.HttpOnly = httpOnly
					}
					req.Cookies = append(req.Cookies, cookie)
				}
			}
		case []map[string]any:
			for _, cm := range cs {
				cookie := &http.Cookie{}
				if name, ok := cm["name"].(string); ok {
					cookie.Name = name
				}
				if value, ok := cm["value"].(string); ok {
					cookie.Value = value
				}
				if domain, ok := cm["domain"].(string); ok {
					cookie.Domain = domain
				}
				if path, ok := cm["path"].(string); ok {
					cookie.Path = path
				}
				if secure, ok := cm["secure"].(bool); ok {
					cookie.Secure = secure
				}
				if httpOnly, ok := cm["httponly"].(bool); ok {
					cookie.HttpOnly = httpOnly
				}
				req.Cookies = append(req.Cookies, cookie)
			}
		}
	}

	// Meta
	if meta, ok := d["meta"].(map[string]any); ok {
		req.Meta = meta
	}

	// Priority
	if priority, ok := d["priority"]; ok {
		switch p := priority.(type) {
		case int:
			req.Priority = p
		case float64:
			req.Priority = int(p)
		case int64:
			req.Priority = int(p)
		}
	}

	// DontFilter
	if dontFilter, ok := d["dont_filter"].(bool); ok {
		req.DontFilter = dontFilter
	}

	// Callback/Errback — 通过注册表恢复
	// 当 registry 非空且字典中包含 callback/errback 名称时，
	// 必须在注册表中找到对应的函数，否则返回错误。
	// 这确保序列化的回调名称与注册表中的方法名严格匹配。
	if registry != nil {
		if cbName, ok := d["callback"].(string); ok && cbName != "" {
			cb, found := registry.Lookup(cbName)
			if !found {
				return nil, fmt.Errorf("callback %q not found in registry (registered: %v)",
					cbName, registry.Names())
			}
			req.Callback = cb
		}
		if ebName, ok := d["errback"].(string); ok && ebName != "" {
			eb, found := registry.LookupErrback(ebName)
			if !found {
				return nil, fmt.Errorf("errback %q not found in registry (registered: %v)",
					ebName, registry.ErrbackNames())
			}
			req.Errback = eb
		}
	}

	// Flags
	if flags, ok := d["flags"]; ok {
		switch f := flags.(type) {
		case []any:
			for _, item := range f {
				if s, ok := item.(string); ok {
					req.Flags = append(req.Flags, s)
				}
			}
		case []string:
			req.Flags = f
		}
	}

	// CbKwargs
	if cbKwargs, ok := d["cb_kwargs"].(map[string]any); ok {
		req.CbKwargs = cbKwargs
	}

	// Encoding
	if encoding, ok := d["encoding"].(string); ok && encoding != "" {
		req.Encoding = encoding
	}

	return req, nil
}

// ============================================================================
// Request → curl 命令互转
// ============================================================================

// ToCURL 将 Request 转换为 curl 命令字符串。
//
// 对齐 Scrapy 的 request_to_curl() 函数。
//
// 用法：
//
//	req, _ := http.NewRequest("https://example.com",
//	    http.WithMethod("POST"),
//	    http.WithBody([]byte(`{"key":"value"}`)),
//	    http.WithHeader("Content-Type", "application/json"),
//	)
//	curl := req.ToCURL()
//	// 输出: curl -X POST 'https://example.com' -H 'Content-Type: application/json' --data-raw '{"key":"value"}'
func (r *Request) ToCURL() string {
	parts := []string{"curl"}

	// Method
	parts = append(parts, "-X", r.Method)

	// URL
	parts = append(parts, shellQuote(r.URL.String()))

	// Headers
	for k, vs := range r.Headers {
		for _, v := range vs {
			parts = append(parts, "-H", shellQuote(k+": "+v))
		}
	}

	// Cookies
	if len(r.Cookies) > 0 {
		cookieStr := ""
		for i, c := range r.Cookies {
			if i > 0 {
				cookieStr += "; "
			}
			cookieStr += c.Name + "=" + c.Value
		}
		parts = append(parts, "--cookie", shellQuote(cookieStr))
	}

	// Body
	if len(r.Body) > 0 {
		parts = append(parts, "--data-raw", shellQuote(string(r.Body)))
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += " "
		}
		result += part
	}
	return result
}

// ============================================================================
// 内部辅助函数
// ============================================================================

// isJSONSerializable 检查值是否可以被 JSON 序列化。
// 排除函数、channel、复杂指针等不可序列化的类型。
func isJSONSerializable(v any) bool {
	if v == nil {
		return true
	}
	switch v.(type) {
	case bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, string:
		return true
	case []any, []string, []int, []float64, []bool:
		return true
	case map[string]any, map[string]string, map[string]int, map[string]float64:
		return true
	default:
		// 对于其他类型，保守地认为不可序列化
		// 这包括函数、channel、复杂结构体等
		return false
	}
}

// shellQuote 对字符串进行 shell 引用，使用单引号包裹。
// 单引号内的单引号通过 '\'' 转义。
func shellQuote(s string) string {
	result := "'"
	for _, c := range s {
		if c == '\'' {
			result += "'\\''"
		} else {
			result += string(c)
		}
	}
	result += "'"
	return result
}
