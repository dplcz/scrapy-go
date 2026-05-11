package redisqueue

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// requestFingerprint 计算请求的指纹（SHA1 哈希）。
//
// 指纹算法与 scrapy-go 主模块的 internal/utils.RequestFingerprint 完全一致，
// 确保 RedisDupeFilter 与 RFPDupeFilter 的去重行为兼容。
//
// 指纹基于以下信息计算：
//   - HTTP 方法
//   - 规范化后的 URL
//   - 请求体
func requestFingerprint(request *shttp.Request) string {
	data := map[string]any{
		"method":  request.Method,
		"url":     canonicalizeURL(request.URL.String(), false),
		"body":    hex.EncodeToString(request.Body),
		"headers": map[string][]string{},
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		jsonBytes = []byte(request.Method + request.URL.String())
	}

	hash := sha1.Sum(jsonBytes)
	return hex.EncodeToString(hash[:])
}

// canonicalizeURL 规范化 URL，用于去重比较。
//
// 规范化规则与 scrapy-go 主模块的 internal/utils.CanonicalizeURL 完全一致：
//  1. 转换 scheme 和 host 为小写
//  2. 移除默认端口（http:80, https:443）
//  3. 对查询参数按 key 排序
//  4. 移除 URL fragment（#后面的部分），除非 keepFragments 为 true
//  5. 规范化路径（移除多余的 /）
func canonicalizeURL(rawURL string, keepFragments bool) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	host := u.Hostname()
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		u.Host = host
	}

	if u.RawQuery != "" {
		params := u.Query()
		sortedKeys := make([]string, 0, len(params))
		for k := range params {
			sortedKeys = append(sortedKeys, k)
		}
		sort.Strings(sortedKeys)

		var parts []string
		for _, k := range sortedKeys {
			values := params[k]
			sort.Strings(values)
			for _, v := range values {
				parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
		}
		u.RawQuery = strings.Join(parts, "&")
	}

	if !keepFragments {
		u.Fragment = ""
		u.RawFragment = ""
	}

	if u.Path == "" {
		u.Path = "/"
	}

	return u.String()
}
