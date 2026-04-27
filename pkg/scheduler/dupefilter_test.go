package scheduler

import (
	"context"
	"testing"

	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
)

func TestRFPDupeFilterBasic(t *testing.T) {
	df := NewRFPDupeFilter(nil, false)
	df.Open(context.Background())
	defer df.Close("finished")

	req1 := scrapy_http.MustNewRequest("https://example.com/page1")
	req2 := scrapy_http.MustNewRequest("https://example.com/page2")

	// 第一次见到应返回 false
	if df.RequestSeen(req1) {
		t.Error("first request should not be seen")
	}
	if df.RequestSeen(req2) {
		t.Error("second request should not be seen")
	}

	// 第二次见到应返回 true
	if !df.RequestSeen(req1) {
		t.Error("duplicate request should be seen")
	}
	if !df.RequestSeen(req2) {
		t.Error("duplicate request should be seen")
	}
}

func TestRFPDupeFilterSameURLDifferentParams(t *testing.T) {
	df := NewRFPDupeFilter(nil, false)
	df.Open(context.Background())
	defer df.Close("finished")

	// 查询参数顺序不同但内容相同的 URL 应被视为重复
	req1 := scrapy_http.MustNewRequest("https://example.com/page?a=1&b=2")
	req2 := scrapy_http.MustNewRequest("https://example.com/page?b=2&a=1")

	if df.RequestSeen(req1) {
		t.Error("first request should not be seen")
	}
	if !df.RequestSeen(req2) {
		t.Error("same URL with different param order should be seen as duplicate")
	}
}

func TestRFPDupeFilterDifferentMethods(t *testing.T) {
	df := NewRFPDupeFilter(nil, false)
	df.Open(context.Background())
	defer df.Close("finished")

	get := scrapy_http.MustNewRequest("https://example.com/page")
	post := scrapy_http.MustNewRequest("https://example.com/page",
		scrapy_http.WithMethod("POST"),
	)

	if df.RequestSeen(get) {
		t.Error("GET should not be seen")
	}
	// 不同方法应被视为不同请求
	if df.RequestSeen(post) {
		t.Error("POST should not be seen (different method)")
	}
}

func TestRFPDupeFilterDifferentBodies(t *testing.T) {
	df := NewRFPDupeFilter(nil, false)
	df.Open(context.Background())
	defer df.Close("finished")

	req1 := scrapy_http.MustNewRequest("https://example.com/api",
		scrapy_http.WithMethod("POST"),
		scrapy_http.WithBody([]byte(`{"key":"value1"}`)),
	)
	req2 := scrapy_http.MustNewRequest("https://example.com/api",
		scrapy_http.WithMethod("POST"),
		scrapy_http.WithBody([]byte(`{"key":"value2"}`)),
	)

	if df.RequestSeen(req1) {
		t.Error("first request should not be seen")
	}
	// 不同 body 应被视为不同请求
	if df.RequestSeen(req2) {
		t.Error("different body should not be seen as duplicate")
	}
}

func TestRFPDupeFilterSeenCount(t *testing.T) {
	df := NewRFPDupeFilter(nil, false)
	df.Open(context.Background())

	for i := 0; i < 100; i++ {
		req := scrapy_http.MustNewRequest("https://example.com/page",
			scrapy_http.WithPriority(i), // 不同优先级不影响指纹
		)
		df.RequestSeen(req)
	}

	// 所有请求 URL 和 Method 相同，指纹应该只有 1 个
	if df.SeenCount() != 1 {
		t.Errorf("expected 1 unique fingerprint, got %d", df.SeenCount())
	}
}

func TestRFPDupeFilterClose(t *testing.T) {
	df := NewRFPDupeFilter(nil, false)
	df.Open(context.Background())

	req := scrapy_http.MustNewRequest("https://example.com")
	df.RequestSeen(req)

	// Close 后指纹集合应被清空
	df.Close("finished")
	// Close 后不应 panic（即使内部 map 为 nil）
}

func TestNoDupeFilter(t *testing.T) {
	df := NewNoDupeFilter()
	df.Open(context.Background())
	defer df.Close("finished")

	req := scrapy_http.MustNewRequest("https://example.com")

	// NoDupeFilter 永远返回 false
	if df.RequestSeen(req) {
		t.Error("NoDupeFilter should never return true")
	}
	if df.RequestSeen(req) {
		t.Error("NoDupeFilter should never return true, even for duplicates")
	}
}

func TestRFPDupeFilterDebugMode(t *testing.T) {
	// debug 模式不应 panic
	df := NewRFPDupeFilter(nil, true)
	df.Open(context.Background())
	defer df.Close("finished")

	req := scrapy_http.MustNewRequest("https://example.com")
	df.RequestSeen(req)
	df.RequestSeen(req) // 触发 debug 日志
}

func TestRFPDupeFilterFragments(t *testing.T) {
	df := NewRFPDupeFilter(nil, false)
	df.Open(context.Background())
	defer df.Close("finished")

	// 默认不保留 fragment，所以这两个 URL 应被视为相同
	req1 := scrapy_http.MustNewRequest("https://example.com/page#section1")
	req2 := scrapy_http.MustNewRequest("https://example.com/page#section2")

	if df.RequestSeen(req1) {
		t.Error("first request should not be seen")
	}
	if !df.RequestSeen(req2) {
		t.Error("different fragments should be seen as duplicate (fragments ignored)")
	}
}
