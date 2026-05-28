package dedup

import (
	"context"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
)

func TestCanonicalizeURL_DropsTrackingParamsAndSorts(t *testing.T) {
	t.Parallel()

	got := CanonicalizeURL("HTTPS://Example.COM:443/articles?id=10&utm_source=news&b=2&a=1&fbclid=abc#comments", nil)
	want := "https://example.com/articles?a=1&b=2&id=10"
	if got != want {
		t.Fatalf("CanonicalizeURL() = %q, want %q", got, want)
	}
}

func TestCanonicalizeURL_CustomTrackingParams(t *testing.T) {
	t.Parallel()

	opts := &URLCanonicalizerOptions{
		DropTrackingParams:    true,
		TrackingParamNames:    []string{"session_id"},
		TrackingParamPrefixes: []string{"trk_"},
	}
	got := CanonicalizeURL("https://example.com/?q=go&session_id=1&trk_campaign=summer&utm_source=keep", opts)
	want := "https://example.com/?q=go&utm_source=keep"
	if got != want {
		t.Fatalf("CanonicalizeURL() = %q, want %q", got, want)
	}
}

func TestCanonicalizeURL_KeepFragmentsAndTrackingParams(t *testing.T) {
	t.Parallel()

	opts := &URLCanonicalizerOptions{
		KeepFragments:      true,
		DropTrackingParams: false,
	}
	got := CanonicalizeURL("http://example.com:80/path?utm_source=a&b=2#section", opts)
	want := "http://example.com/path?b=2&utm_source=a#section"
	if got != want {
		t.Fatalf("CanonicalizeURL() = %q, want %q", got, want)
	}
}

func TestURLCanonicalDupeFilter(t *testing.T) {
	t.Parallel()

	filter := NewURLCanonicalDupeFilter(nil)
	if err := filter.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer filter.Close("done")

	first := shttp.MustNewRequest("https://example.com/page?b=2&a=1&utm_source=feed")
	second := shttp.MustNewRequest("https://EXAMPLE.com:443/page?a=1&b=2&fbclid=abc")
	if filter.RequestSeen(first) {
		t.Fatal("first request should be new")
	}
	if !filter.RequestSeen(second) {
		t.Fatal("tracking-only URL variant should be duplicate")
	}
	if filter.SeenCount() != 1 {
		t.Fatalf("SeenCount() = %d, want 1", filter.SeenCount())
	}
}

func TestURLCanonicalDupeFilter_MethodAndBodyMatter(t *testing.T) {
	t.Parallel()

	filter := NewURLCanonicalDupeFilter(nil)
	if err := filter.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer filter.Close("done")

	getReq := shttp.MustNewRequest("https://example.com/search?q=go")
	postReq := shttp.MustNewRequest("https://example.com/search?q=go", shttp.WithMethod("POST"), shttp.WithBody([]byte("q=go")))
	postReq2 := shttp.MustNewRequest("https://example.com/search?q=go", shttp.WithMethod("POST"), shttp.WithBody([]byte("q=rust")))

	if filter.RequestSeen(getReq) {
		t.Fatal("GET request should be new")
	}
	if filter.RequestSeen(postReq) {
		t.Fatal("POST request with body should not duplicate GET")
	}
	if filter.RequestSeen(postReq2) {
		t.Fatal("POST request with different body should be new")
	}
	if filter.SeenCount() != 3 {
		t.Fatalf("SeenCount() = %d, want 3", filter.SeenCount())
	}
}

func TestURLCanonicalDupeFilter_WithScheduler(t *testing.T) {
	t.Parallel()

	filter := NewURLCanonicalDupeFilter(nil)
	sched := scheduler.NewDefaultScheduler(scheduler.WithDupeFilter(filter))
	if err := sched.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer sched.Close(context.Background(), "done")

	first := shttp.MustNewRequest("https://example.com/item?id=1&utm_medium=email")
	second := shttp.MustNewRequest("https://example.com/item?id=1&fbclid=tracking")
	if !sched.EnqueueRequest(first) {
		t.Fatal("first enqueue should succeed")
	}
	if sched.EnqueueRequest(second) {
		t.Fatal("tracking-only variant should be filtered")
	}
	if sched.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", sched.Len())
	}
}
