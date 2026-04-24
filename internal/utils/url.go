// Package utils 提供 scrapy-go 框架的内部工具函数。
// 此包不对外导出。
package utils

import (
	"net/url"
	"sort"
	"strings"
)

// CanonicalizeURL 规范化 URL，用于去重比较。
// 对应 Scrapy 中 w3lib.url.canonicalize_url 的功能。
//
// 规范化规则：
//  1. 转换 scheme 和 host 为小写
//  2. 移除默认端口（http:80, https:443）
//  3. 对查询参数按 key 排序
//  4. 移除 URL fragment（#后面的部分），除非 keepFragments 为 true
//  5. 规范化路径（移除多余的 /）
func CanonicalizeURL(rawURL string, keepFragments bool) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	// 1. scheme 和 host 转小写
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// 2. 移除默认端口
	host := u.Hostname()
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		u.Host = host
	}

	// 3. 对查询参数排序
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

	// 4. 移除 fragment
	if !keepFragments {
		u.Fragment = ""
		u.RawFragment = ""
	}

	// 5. 规范化路径
	if u.Path == "" {
		u.Path = "/"
	}

	return u.String()
}

// GetURLDomain 从 URL 中提取域名（不含端口）。
func GetURLDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// GetURLHost 从 URL 中提取 host（含端口）。
func GetURLHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// IsValidURL 检查 URL 是否有效（包含 scheme）。
func IsValidURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Scheme != "" && u.Host != ""
}

// URLHasScheme 检查 URL 是否包含 scheme。
func URLHasScheme(rawURL string) bool {
	return strings.Contains(rawURL, "://") ||
		strings.HasPrefix(rawURL, "about:") ||
		strings.HasPrefix(rawURL, "data:")
}
