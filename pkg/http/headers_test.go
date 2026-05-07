package http

import (
	"net/http"
	"testing"
)

func TestNewHeaders(t *testing.T) {
	h := NewHeaders(map[string]string{
		"Content-Type": "application/json",
		"Accept":       "text/html",
	})

	if h.Get("Content-Type") != "application/json" {
		t.Error("unexpected Content-Type")
	}
	if h.Get("Accept") != "text/html" {
		t.Error("unexpected Accept")
	}
}

func TestNewHeadersFromMap(t *testing.T) {
	h := NewHeadersFromMap(map[string][]string{
		"Accept": {"text/html", "application/json"},
	})

	values := h.Values("Accept")
	if len(values) != 2 {
		t.Errorf("expected 2 values, got %d", len(values))
	}
}

func TestMergeHeaders(t *testing.T) {
	dst := http.Header{
		"Content-Type": {"text/html"},
		"Accept":       {"*/*"},
	}
	src := http.Header{
		"Content-Type": {"application/json"},
		"X-Custom":     {"value"},
	}

	// override=true
	MergeHeaders(dst, src, true)
	if dst.Get("Content-Type") != "application/json" {
		t.Error("Content-Type should be overridden")
	}
	if dst.Get("X-Custom") != "value" {
		t.Error("X-Custom should be added")
	}

	// override=false
	dst2 := http.Header{
		"Content-Type": {"text/html"},
	}
	src2 := http.Header{
		"Content-Type": {"application/json"},
		"X-New":        {"value"},
	}
	MergeHeaders(dst2, src2, false)
	if dst2.Get("Content-Type") != "text/html" {
		t.Error("Content-Type should not be overridden")
	}
	if dst2.Get("X-New") != "value" {
		t.Error("X-New should be added")
	}
}

func TestHeadersToMap(t *testing.T) {
	h := http.Header{
		"Content-Type": {"application/json"},
		"Accept":       {"text/html", "application/xml"},
	}

	m := HeadersToMap(h)
	if m["Content-Type"] != "application/json" {
		t.Error("unexpected Content-Type")
	}
	// 多值只取第一个
	if m["Accept"] != "text/html" {
		t.Error("unexpected Accept")
	}
}

func TestGetContentType(t *testing.T) {
	tests := []struct {
		ct       string
		expected string
	}{
		{"text/html; charset=utf-8", "text/html"},
		{"application/json", "application/json"},
		{"", ""},
		{"TEXT/HTML", "text/html"},
	}

	for i, tt := range tests {
		h := http.Header{"Content-Type": {tt.ct}}
		if tt.ct == "" {
			h = http.Header{}
		}
		result := GetContentType(h)
		if result != tt.expected {
			t.Errorf("test %d: GetContentType(%q) = %q, expected %q", i, tt.ct, result, tt.expected)
		}
	}
}

func TestGetEncoding(t *testing.T) {
	tests := []struct {
		ct       string
		expected string
	}{
		{"text/html; charset=utf-8", "utf-8"},
		{"text/html; charset=gbk", "gbk"},
		{"text/html", "utf-8"},
		{"", "utf-8"},
	}

	for i, tt := range tests {
		h := http.Header{}
		if tt.ct != "" {
			h.Set("Content-Type", tt.ct)
		}
		result := GetEncoding(h, "utf-8")
		if result != tt.expected {
			t.Errorf("test %d: GetEncoding(%q) = %q, expected %q", i, tt.ct, result, tt.expected)
		}
	}
}

func TestIsTextContentType(t *testing.T) {
	tests := []struct {
		ct       string
		expected bool
	}{
		{"text/html", true},
		{"text/plain", true},
		{"application/json", true},
		{"application/xml", true},
		{"application/xhtml+xml", true},
		{"application/javascript", true},
		{"application/atom+xml", true},
		{"image/png", false},
		{"application/octet-stream", false},
		{"", false},
	}

	for i, tt := range tests {
		h := http.Header{}
		if tt.ct != "" {
			h.Set("Content-Type", tt.ct)
		}
		result := IsTextContentType(h)
		if result != tt.expected {
			t.Errorf("test %d: IsTextContentType(%q) = %v, expected %v", i, tt.ct, result, tt.expected)
		}
	}
}
