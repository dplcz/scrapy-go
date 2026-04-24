package http

import (
	"net/http"
	"strings"
)

// ============================================================================
// Headers 工具函数
// ============================================================================

// NewHeaders 从 map[string]string 创建 http.Header。
func NewHeaders(headers map[string]string) http.Header {
	h := make(http.Header, len(headers))
	for k, v := range headers {
		h.Set(k, v)
	}
	return h
}

// NewHeadersFromMap 从 map[string][]string 创建 http.Header。
func NewHeadersFromMap(headers map[string][]string) http.Header {
	h := make(http.Header, len(headers))
	for k, vs := range headers {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	return h
}

// MergeHeaders 将 src 中的 Header 合并到 dst 中。
// 如果 override 为 true，则覆盖已存在的 Header；否则仅添加不存在的 Header。
func MergeHeaders(dst, src http.Header, override bool) {
	for key, values := range src {
		if override || dst.Get(key) == "" {
			dst.Del(key)
			for _, v := range values {
				dst.Add(key, v)
			}
		}
	}
}

// HeadersToMap 将 http.Header 转换为 map[string]string。
// 如果一个 Header 有多个值，只取第一个。
func HeadersToMap(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for k := range headers {
		result[k] = headers.Get(k)
	}
	return result
}

// GetContentType 从 Headers 中提取 Content-Type（不含参数部分）。
func GetContentType(headers http.Header) string {
	ct := headers.Get("Content-Type")
	if ct == "" {
		return ""
	}
	// 去掉参数部分，如 "text/html; charset=utf-8" -> "text/html"
	if idx := strings.Index(ct, ";"); idx != -1 {
		ct = ct[:idx]
	}
	return strings.TrimSpace(strings.ToLower(ct))
}

// GetEncoding 从 Content-Type Header 中提取字符编码。
// 如果未指定编码，返回默认值 defaultEncoding。
func GetEncoding(headers http.Header, defaultEncoding string) string {
	ct := headers.Get("Content-Type")
	if ct == "" {
		return defaultEncoding
	}

	for _, part := range strings.Split(ct, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "charset=") {
			charset := strings.TrimPrefix(part, "charset=")
			charset = strings.TrimPrefix(charset, "Charset=")
			charset = strings.TrimPrefix(charset, "CHARSET=")
			// 去掉引号
			charset = strings.Trim(charset, `"'`)
			if charset != "" {
				return charset
			}
		}
	}

	return defaultEncoding
}

// IsTextContentType 判断 Content-Type 是否为文本类型。
func IsTextContentType(headers http.Header) bool {
	ct := GetContentType(headers)
	if ct == "" {
		return false
	}
	return strings.HasPrefix(ct, "text/") ||
		ct == "application/json" ||
		ct == "application/xml" ||
		ct == "application/xhtml+xml" ||
		ct == "application/javascript" ||
		strings.HasSuffix(ct, "+xml") ||
		strings.HasSuffix(ct, "+json")
}
