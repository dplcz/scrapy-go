package http

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sync"

	"github.com/dplcz/scrapy-go/pkg/item"
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
	// JSON 反序列化时所有数字都会变为 float64，需要将可以无损转换为 int 的值还原，
	// 避免用户代码中 meta["key"].(int) 类型断言失败。
	if meta, ok := d["meta"].(map[string]any); ok {
		req.Meta = restoreMetaTypes(meta)
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

// restoreMetaTypes 递归处理 JSON 反序列化后的 meta map，
// 将可以无损转换为 int 的 float64 值还原为 int。
//
// 背景：Go 的 encoding/json 在反序列化到 any（interface{}）类型时，
// 所有 JSON 数字都会被解码为 float64。这导致用户在序列化前通过
// req.SetMeta("page", 1) 设置的 int 值，在反序列化后变成 float64，
// 使得 meta["page"].(int) 类型断言失败。
//
// 转换规则：
//   - float64 值如果没有小数部分（如 1.0、42.0），转换为 int
//   - float64 值如果有小数部分（如 3.14），保持 float64 不变
//   - 嵌套的 map[string]any 递归处理
//   - 嵌套的 []any 中的元素也递归处理
//   - 其他类型保持不变
func restoreMetaTypes(meta map[string]any) map[string]any {
	for k, v := range meta {
		meta[k] = restoreValue(v)
	}
	return meta
}

// restoreValue 递归还原单个值的类型。
func restoreValue(v any) any {
	switch val := v.(type) {
	case float64:
		// 如果 float64 没有小数部分，且在 int 范围内，转换为 int
		intVal := int(val)
		if float64(intVal) == val {
			return intVal
		}
		return val
	case map[string]any:
		// 递归处理嵌套 map
		for k, item := range val {
			val[k] = restoreValue(item)
		}
		return val
	case []any:
		// 递归处理嵌套 slice
		for i, item := range val {
			val[i] = restoreValue(item)
		}
		return val
	default:
		return val
	}
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

// serializableCache 缓存已判定过的类型的序列化结果，避免重复反射。
// key 为 reflect.Type，value 为 bool。
var serializableCache sync.Map

// isJSONSerializable 检查值是否可以被 JSON 序列化。
//
// 判断策略（按优先级）：
//  1. nil → true
//  2. 基础类型（bool/int*/uint*/float*/string）→ true（快路径，零反射）
//  3. 常见复合类型（[]any/[]string/map[string]any 等）→ true（快路径）
//  4. 实现 ItemAdapter 接口 → true
//  5. reflect.Kind 判断：
//     - Func/Chan/UnsafePointer → false（不可序列化）
//     - Struct → true（带导出字段的结构体允许通过）
//     - Ptr → 递归检查指向的元素类型
//     - Slice/Array → 递归检查元素类型
//     - Map（key 为 string）→ true
//     - Interface → false（空接口嵌套，无法确定实际类型）
//  6. 最终 fallback：尝试 json.Marshal，结果缓存
func isJSONSerializable(v any) bool {
	if v == nil {
		return true
	}

	// 快路径：基础类型和常见复合类型，零反射开销
	switch v.(type) {
	case bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, string:
		return true
	case []any, []string, []int, []float64, []bool:
		return true
	case map[string]any, map[string]string, map[string]int, map[string]float64:
		return true
	}

	// ItemAdapter 接口放行
	if _, ok := v.(item.ItemAdapter); ok {
		return true
	}

	// 反射路径：基于 Kind 判断
	return isTypeSerializable(reflect.TypeOf(v))
}

// isTypeSerializable 基于 reflect.Type 判断类型是否可 JSON 序列化。
// 结果会被缓存到 serializableCache 中，避免重复反射。
func isTypeSerializable(t reflect.Type) bool {
	if t == nil {
		return false
	}

	// 查缓存
	if cached, ok := serializableCache.Load(t); ok {
		return cached.(bool)
	}

	result := checkTypeSerializable(t)
	serializableCache.Store(t, result)
	return result
}

// checkTypeSerializable 执行实际的类型序列化检查。
func checkTypeSerializable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		// 不可序列化的类型
		return false

	case reflect.Struct:
		// 结构体：检查是否有至少一个导出字段
		return hasExportedFields(t)

	case reflect.Ptr:
		// 指针：递归检查指向的元素类型
		return isTypeSerializable(t.Elem())

	case reflect.Slice, reflect.Array:
		// 切片/数组：递归检查元素类型
		return isTypeSerializable(t.Elem())

	case reflect.Map:
		// Map：key 必须为 string 类型（JSON 要求）
		if t.Key().Kind() == reflect.String {
			return true
		}
		return false

	case reflect.Interface:
		// 空接口嵌套，无法在编译期确定实际类型，保守拒绝
		return false

	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true

	default:
		// 未知类型：尝试 json.Marshal 作为最终 fallback
		return tryMarshal(t)
	}
}

// hasExportedFields 检查结构体是否有至少一个导出字段。
func hasExportedFields(t reflect.Type) bool {
	for i := range t.NumField() {
		if t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

// tryMarshal 尝试对给定类型的零值进行 JSON 序列化，作为最终 fallback。
// 结果通过 serializableCache 缓存，避免重复尝试。
func tryMarshal(t reflect.Type) bool {
	v := reflect.New(t).Elem().Interface()
	_, err := json.Marshal(v)
	return err == nil
}

// shellQuote 对字符串进行 shell 引用，使用单引号包裹。
// 单引号内的单引号通过 '\” 转义。
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
