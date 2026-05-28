package dedup

import (
	"fmt"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

func BenchmarkCanonicalRequestFingerprint(b *testing.B) {
	req := shttp.MustNewRequest("https://example.com/articles?id=123&utm_source=news&b=2&a=1")
	for i := 0; i < b.N; i++ {
		_ = CanonicalRequestFingerprint(req, nil)
	}
}

func BenchmarkSimHash(b *testing.B) {
	content := []byte("Scrapy-Go provides high performance asynchronous crawling with scheduler, downloader, scraper and pipeline components.")
	for i := 0; i < b.N; i++ {
		_ = SimHashBytes(content)
	}
}

func BenchmarkSimHashDupeFilter_AddFingerprint(b *testing.B) {
	filter, err := NewSimHashDupeFilter(DefaultSimHashOptions())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filter.AddFingerprint(SimHash(fmt.Sprintf("article %d content", i)))
	}
}
