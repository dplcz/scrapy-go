package utils

import (
	"net/http"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

func TestRequestFingerprint(t *testing.T) {
	// 相同请求应产生相同指纹
	req1 := shttp.MustNewRequest("https://example.com/page?b=2&a=1")
	req2 := shttp.MustNewRequest("https://example.com/page?a=1&b=2")

	fp1 := RequestFingerprint(req1, nil, false)
	fp2 := RequestFingerprint(req2, nil, false)

	if fp1 != fp2 {
		t.Errorf("same URL with different param order should have same fingerprint: %s != %s", fp1, fp2)
	}

	// 不同 URL 应产生不同指纹
	req3 := shttp.MustNewRequest("https://example.com/other")
	fp3 := RequestFingerprint(req3, nil, false)
	if fp1 == fp3 {
		t.Error("different URLs should have different fingerprints")
	}

	// 不同方法应产生不同指纹
	req4 := shttp.MustNewRequest("https://example.com/page?a=1&b=2",
		shttp.WithMethod("POST"),
	)
	fp4 := RequestFingerprint(req4, nil, false)
	if fp1 == fp4 {
		t.Error("different methods should have different fingerprints")
	}

	// 不同 body 应产生不同指纹
	req5 := shttp.MustNewRequest("https://example.com/page?a=1&b=2",
		shttp.WithMethod("POST"),
		shttp.WithBody([]byte("body1")),
	)
	req6 := shttp.MustNewRequest("https://example.com/page?a=1&b=2",
		shttp.WithMethod("POST"),
		shttp.WithBody([]byte("body2")),
	)
	fp5 := RequestFingerprint(req5, nil, false)
	fp6 := RequestFingerprint(req6, nil, false)
	if fp5 == fp6 {
		t.Error("different bodies should have different fingerprints")
	}
}

func TestRequestFingerprintWithHeaders(t *testing.T) {
	req1 := shttp.MustNewRequest("https://example.com",
		shttp.WithHeaders(http.Header{
			"Authorization": {"Bearer token1"},
		}),
	)
	req2 := shttp.MustNewRequest("https://example.com",
		shttp.WithHeaders(http.Header{
			"Authorization": {"Bearer token2"},
		}),
	)

	// 不包含 headers 时指纹相同
	fp1 := RequestFingerprint(req1, nil, false)
	fp2 := RequestFingerprint(req2, nil, false)
	if fp1 != fp2 {
		t.Error("without include_headers, fingerprints should be same")
	}

	// 包含 headers 时指纹不同
	fp3 := RequestFingerprint(req1, []string{"Authorization"}, false)
	fp4 := RequestFingerprint(req2, []string{"Authorization"}, false)
	if fp3 == fp4 {
		t.Error("with include_headers, different header values should produce different fingerprints")
	}
}

func TestRequestFingerprintWithFragments(t *testing.T) {
	req1 := shttp.MustNewRequest("https://example.com/page#section1")
	req2 := shttp.MustNewRequest("https://example.com/page#section2")

	// 不保留 fragment 时指纹相同
	fp1 := RequestFingerprint(req1, nil, false)
	fp2 := RequestFingerprint(req2, nil, false)
	if fp1 != fp2 {
		t.Error("without keep_fragments, fingerprints should be same")
	}

	// 保留 fragment 时指纹不同
	fp3 := RequestFingerprint(req1, nil, true)
	fp4 := RequestFingerprint(req2, nil, true)
	if fp3 == fp4 {
		t.Error("with keep_fragments, different fragments should produce different fingerprints")
	}
}

func TestSimpleFingerprint(t *testing.T) {
	req1 := shttp.MustNewRequest("https://example.com/page")
	req2 := shttp.MustNewRequest("https://example.com/page")
	req3 := shttp.MustNewRequest("https://example.com/other")

	fp1 := SimpleFingerprint(req1)
	fp2 := SimpleFingerprint(req2)
	fp3 := SimpleFingerprint(req3)

	if fp1 != fp2 {
		t.Error("same requests should have same fingerprint")
	}
	if fp1 == fp3 {
		t.Error("different requests should have different fingerprints")
	}

	// 指纹应该是 hex 编码的 SHA1（40 字符）
	if len(fp1) != 40 {
		t.Errorf("fingerprint should be 40 chars (SHA1 hex), got %d", len(fp1))
	}
}
