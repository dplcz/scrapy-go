package dedup

import (
	"context"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

func TestSimHashDeterministic(t *testing.T) {
	t.Parallel()

	content := "Scrapy-Go 是一个高性能 Go 爬虫框架，支持异步调度和去重。"
	fp1 := SimHash(content)
	fp2 := SimHash(content)
	if fp1 == 0 {
		t.Fatal("SimHash should not be zero for non-empty content")
	}
	if fp1 != fp2 {
		t.Fatalf("SimHash should be deterministic: %x vs %x", fp1, fp2)
	}
}

func TestHammingDistance(t *testing.T) {
	t.Parallel()

	if got := HammingDistance(0b1010, 0b1001); got != 2 {
		t.Fatalf("HammingDistance() = %d, want 2", got)
	}
	if got := HammingDistance(0, ^uint64(0)); got != 64 {
		t.Fatalf("HammingDistance() = %d, want 64", got)
	}
}

func TestSimHashDupeFilter_MetaContent(t *testing.T) {
	t.Parallel()

	filter, err := NewSimHashDupeFilter(DefaultSimHashOptions())
	if err != nil {
		t.Fatalf("NewSimHashDupeFilter() error = %v", err)
	}
	if err := filter.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer filter.Close("done")

	first := shttp.MustNewRequest("https://example.com/a")
	first.SetMeta(MetaContentKey, "Go crawler framework supports high performance asynchronous scheduling")
	second := shttp.MustNewRequest("https://example.com/b")
	second.SetMeta(MetaContentKey, []byte("Go crawler framework supports high performance asynchronous scheduling"))
	missing := shttp.MustNewRequest("https://example.com/c")

	if filter.RequestSeen(first) {
		t.Fatal("first content should be new")
	}
	if !filter.RequestSeen(second) {
		t.Fatal("same content should be duplicate")
	}
	if filter.RequestSeen(missing) {
		t.Fatal("request without content should skip SimHash strategy")
	}
	if filter.SeenCount() != 1 {
		t.Fatalf("SeenCount() = %d, want 1", filter.SeenCount())
	}
}

func TestSimHashDupeFilter_AddFingerprintThreshold(t *testing.T) {
	t.Parallel()

	filter, err := NewSimHashDupeFilter(&SimHashOptions{HammingThreshold: 1, BandCount: 4})
	if err != nil {
		t.Fatalf("NewSimHashDupeFilter() error = %v", err)
	}
	if filter.AddFingerprint(0) {
		t.Fatal("first fingerprint should be new")
	}
	if !filter.AddFingerprint(1) {
		t.Fatal("fingerprint within threshold should be duplicate")
	}
	if filter.AddFingerprint(^uint64(0)) {
		t.Fatal("far fingerprint should be new")
	}
	if filter.SeenCount() != 2 {
		t.Fatalf("SeenCount() = %d, want 2", filter.SeenCount())
	}
}

func TestSimHashDupeFilter_CustomExtractor(t *testing.T) {
	t.Parallel()

	filter, err := NewSimHashDupeFilter(&SimHashOptions{
		HammingThreshold: 2,
		ContentExtractor: func(request *shttp.Request) ([]byte, bool) {
			return []byte(request.URL.Hostname()), true
		},
	})
	if err != nil {
		t.Fatalf("NewSimHashDupeFilter() error = %v", err)
	}

	first := shttp.MustNewRequest("https://example.com/a")
	second := shttp.MustNewRequest("https://example.com/b")
	if filter.RequestSeen(first) {
		t.Fatal("first host should be new")
	}
	if !filter.RequestSeen(second) {
		t.Fatal("same extracted host should be duplicate")
	}
}

func TestNewSimHashDupeFilter_InvalidOptions(t *testing.T) {
	t.Parallel()

	cases := []*SimHashOptions{
		{HammingThreshold: -1},
		{HammingThreshold: maxHammingThreshold + 1},
		{HammingThreshold: 1, BandCount: -1},
		{HammingThreshold: 1, BandCount: maxBandCount + 1},
	}
	for _, tt := range cases {
		if _, err := NewSimHashDupeFilter(tt); err == nil {
			t.Fatalf("NewSimHashDupeFilter(%+v) expected error", tt)
		}
	}
}
