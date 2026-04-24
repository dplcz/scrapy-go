package utils

import (
	"testing"
)

func TestCanonicalizeURL(t *testing.T) {
	tests := []struct {
		input         string
		keepFragments bool
		expected      string
	}{
		// scheme 和 host 转小写
		{"HTTP://EXAMPLE.COM/page", false, "http://example.com/page"},
		// 移除默认端口
		{"http://example.com:80/page", false, "http://example.com/page"},
		{"https://example.com:443/page", false, "https://example.com/page"},
		// 保留非默认端口
		{"http://example.com:8080/page", false, "http://example.com:8080/page"},
		// 查询参数排序
		{"http://example.com/page?b=2&a=1", false, "http://example.com/page?a=1&b=2"},
		// 移除 fragment
		{"http://example.com/page#section", false, "http://example.com/page"},
		// 保留 fragment
		{"http://example.com/page#section", true, "http://example.com/page#section"},
		// 空路径补 /
		{"http://example.com", false, "http://example.com/"},
	}

	for i, tt := range tests {
		result := CanonicalizeURL(tt.input, tt.keepFragments)
		if result != tt.expected {
			t.Errorf("test %d: CanonicalizeURL(%q, %v) = %q, expected %q",
				i, tt.input, tt.keepFragments, result, tt.expected)
		}
	}
}

func TestGetURLDomain(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://example.com/page", "example.com"},
		{"https://sub.example.com:8080/page", "sub.example.com"},
		{"invalid", ""},
	}

	for i, tt := range tests {
		result := GetURLDomain(tt.input)
		if result != tt.expected {
			t.Errorf("test %d: GetURLDomain(%q) = %q, expected %q",
				i, tt.input, result, tt.expected)
		}
	}
}

func TestGetURLHost(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://example.com/page", "example.com"},
		{"https://example.com:8080/page", "example.com:8080"},
	}

	for i, tt := range tests {
		result := GetURLHost(tt.input)
		if result != tt.expected {
			t.Errorf("test %d: GetURLHost(%q) = %q, expected %q",
				i, tt.input, result, tt.expected)
		}
	}
}

func TestIsValidURL(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"https://example.com", true},
		{"http://example.com/page", true},
		{"example.com", false},
		{"://invalid", false},
		{"", false},
	}

	for i, tt := range tests {
		result := IsValidURL(tt.input)
		if result != tt.expected {
			t.Errorf("test %d: IsValidURL(%q) = %v, expected %v",
				i, tt.input, result, tt.expected)
		}
	}
}

func TestURLHasScheme(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"about:blank", true},
		{"data:text/html,hello", true},
		{"example.com", false},
		{"/page", false},
	}

	for i, tt := range tests {
		result := URLHasScheme(tt.input)
		if result != tt.expected {
			t.Errorf("test %d: URLHasScheme(%q) = %v, expected %v",
				i, tt.input, result, tt.expected)
		}
	}
}
