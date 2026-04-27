package utils

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"sort"

	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
)

// RequestFingerprint 计算请求的指纹（SHA1 哈希）。
// 对应 Scrapy 的 scrapy.utils.request.fingerprint 函数。
//
// 指纹基于以下信息计算：
//   - HTTP 方法
//   - 规范化后的 URL
//   - 请求体
//   - 指定的请求头（可选）
//
// 参数：
//   - request: 要计算指纹的请求
//   - includeHeaders: 要包含在指纹中的请求头名称列表（可选）
//   - keepFragments: 是否保留 URL 中的 fragment
func RequestFingerprint(request *scrapy_http.Request, includeHeaders []string, keepFragments bool) string {
	// 构建指纹数据
	data := map[string]any{
		"method": request.Method,
		"url":    CanonicalizeURL(request.URL.String(), keepFragments),
		"body":   hex.EncodeToString(request.Body),
	}

	// 包含指定的请求头
	if len(includeHeaders) > 0 {
		headers := make(map[string][]string)
		// 排序 header 名称以确保一致性
		sortedHeaders := make([]string, len(includeHeaders))
		copy(sortedHeaders, includeHeaders)
		sort.Strings(sortedHeaders)

		for _, h := range sortedHeaders {
			values := request.Headers.Values(h)
			if len(values) > 0 {
				hexValues := make([]string, len(values))
				for i, v := range values {
					hexValues[i] = hex.EncodeToString([]byte(v))
				}
				headers[hex.EncodeToString([]byte(h))] = hexValues
			}
		}
		data["headers"] = headers
	} else {
		data["headers"] = map[string][]string{}
	}

	// JSON 序列化（排序 key 以确保一致性）
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		// 不应该发生，但作为后备方案
		jsonBytes = []byte(request.Method + request.URL.String())
	}

	// 计算 SHA1 哈希
	hash := sha1.Sum(jsonBytes)
	return hex.EncodeToString(hash[:])
}

// SimpleFingerprint 计算请求的简单指纹（仅基于 Method + URL）。
// 适用于不需要考虑请求体和请求头的场景。
func SimpleFingerprint(request *scrapy_http.Request) string {
	data := request.Method + "|" + CanonicalizeURL(request.URL.String(), false)
	hash := sha1.Sum([]byte(data))
	return hex.EncodeToString(hash[:])
}
