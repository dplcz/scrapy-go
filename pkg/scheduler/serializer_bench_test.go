package scheduler

import (
	"context"
	"net/http"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// ============================================================================
// 序列化/反序列化完整流程 Benchmark
// ============================================================================

// benchSpider 用于 benchmark 的 Spider 实现。
type benchSpider struct{}

func (s *benchSpider) Parse(ctx context.Context, resp *shttp.Response) ([]shttp.Output, error) {
	return nil, nil
}

func (s *benchSpider) ParseDetail(ctx context.Context, resp *shttp.Response) ([]shttp.Output, error) {
	return nil, nil
}

func (s *benchSpider) ParseList(ctx context.Context, resp *shttp.Response) ([]shttp.Output, error) {
	return nil, nil
}

func (s *benchSpider) HandleError(ctx context.Context, err error, req *shttp.Request) ([]shttp.Output, error) {
	return nil, nil
}

// newBenchRegistry 创建一个包含 benchSpider 所有方法的注册表。
func newBenchRegistry() (*shttp.CallbackRegistry, *benchSpider) {
	spider := &benchSpider{}
	registry := shttp.NewCallbackRegistry()
	registry.RegisterSpider(spider)
	return registry, spider
}

// BenchmarkSerialize_WithCallback 测试带回调函数的请求序列化性能。
// 这是最常见的场景：每个请求都有 Callback。
func BenchmarkSerialize_WithCallback(b *testing.B) {
	registry, spider := newBenchRegistry()
	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com/detail/123",
		shttp.WithMethod("GET"),
		shttp.WithCallback(spider.ParseDetail),
		shttp.WithMeta(map[string]any{"page": 1, "depth": 2}),
		shttp.WithPriority(5),
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := s.Serialize(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSerialize_WithCallbackAndErrback 测试同时带 Callback 和 Errback 的序列化。
func BenchmarkSerialize_WithCallbackAndErrback(b *testing.B) {
	registry, spider := newBenchRegistry()
	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com/detail/123",
		shttp.WithMethod("POST"),
		shttp.WithCallback(spider.ParseDetail),
		shttp.WithErrback(spider.HandleError),
		shttp.WithBody([]byte(`{"key":"value"}`)),
		shttp.WithHeader("Content-Type", "application/json"),
		shttp.WithMeta(map[string]any{"page": 1, "source": "crawl"}),
		shttp.WithPriority(3),
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := s.Serialize(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSerialize_NilCallback 测试无回调请求的序列化（短路路径）。
func BenchmarkSerialize_NilCallback(b *testing.B) {
	registry, _ := newBenchRegistry()
	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com/page",
		shttp.WithMeta(map[string]any{"depth": 1}),
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := s.Serialize(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSerialize_NoRegistry 测试无注册表时的序列化性能基线。
func BenchmarkSerialize_NoRegistry(b *testing.B) {
	s := NewRequestSerializer(nil, nil)

	req := shttp.MustNewRequest("https://example.com/page",
		shttp.WithMeta(map[string]any{"depth": 1}),
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := s.Serialize(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSerialize_FullRequest 测试包含所有字段的完整请求序列化。
func BenchmarkSerialize_FullRequest(b *testing.B) {
	registry, spider := newBenchRegistry()
	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com/api/v1/data",
		shttp.WithMethod("POST"),
		shttp.WithCallback(spider.ParseDetail),
		shttp.WithErrback(spider.HandleError),
		shttp.WithBody([]byte(`{"query":"test","page":1,"filters":{"category":"news"}}`)),
		shttp.WithHeader("Content-Type", "application/json"),
		shttp.WithHeader("Authorization", "Bearer token123"),
		shttp.WithHeader("Accept", "application/json"),
		shttp.WithCookies([]*http.Cookie{
			{Name: "session", Value: "abc123", Domain: ".example.com"},
			{Name: "lang", Value: "en"},
		}),
		shttp.WithMeta(map[string]any{
			"page":          1,
			"depth":         3,
			"download_slot": "example.com",
			"source":        "crawl",
			"retry_count":   0,
		}),
		shttp.WithPriority(5),
		shttp.WithDontFilter(true),
		shttp.WithFlags("cached", "redirected"),
		shttp.WithCbKwargs(map[string]any{"page": 2, "category": "news"}),
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := s.Serialize(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// 反序列化 Benchmark
// ============================================================================

// BenchmarkDeserialize_WithCallback 测试带回调的请求反序列化性能。
func BenchmarkDeserialize_WithCallback(b *testing.B) {
	registry, spider := newBenchRegistry()
	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com/detail/123",
		shttp.WithCallback(spider.ParseDetail),
		shttp.WithMeta(map[string]any{"page": 1, "depth": 2}),
		shttp.WithPriority(5),
	)

	data, err := s.Serialize(req)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := s.Deserialize(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDeserialize_FullRequest 测试完整请求的反序列化性能。
func BenchmarkDeserialize_FullRequest(b *testing.B) {
	registry, spider := newBenchRegistry()
	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com/api/v1/data",
		shttp.WithMethod("POST"),
		shttp.WithCallback(spider.ParseDetail),
		shttp.WithErrback(spider.HandleError),
		shttp.WithBody([]byte(`{"query":"test"}`)),
		shttp.WithHeader("Content-Type", "application/json"),
		shttp.WithCookies([]*http.Cookie{
			{Name: "session", Value: "abc123"},
		}),
		shttp.WithMeta(map[string]any{
			"page":          1,
			"depth":         3,
			"download_slot": "example.com",
			"source":        "crawl",
		}),
		shttp.WithPriority(5),
		shttp.WithDontFilter(true),
		shttp.WithFlags("cached"),
	)

	data, err := s.Serialize(req)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := s.Deserialize(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDeserialize_WithMetaIntRestore 测试包含大量整数 meta 的反序列化性能，
// 重点衡量 restoreMetaTypes 的开销。
func BenchmarkDeserialize_WithMetaIntRestore(b *testing.B) {
	registry, spider := newBenchRegistry()
	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com/page",
		shttp.WithCallback(spider.Parse),
		shttp.WithMeta(map[string]any{
			"page":        1,
			"depth":       3,
			"retry_count": 0,
			"max_pages":   100,
			"batch_size":  50,
			"offset":      200,
			"total":       5000,
			"score":       3.14, // 真正的 float64，不应被转换
			"ratio":       0.75, // 真正的 float64
			"source":      "crawl",
			"active":      true,
		}),
	)

	data, err := s.Serialize(req)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := s.Deserialize(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// 序列化+反序列化往返 Benchmark
// ============================================================================

// BenchmarkRoundTrip_Typical 测试典型爬虫请求的完整序列化+反序列化往返性能。
// 这模拟了磁盘队列中一个请求的完整生命周期：入队（序列化）→ 出队（反序列化）。
func BenchmarkRoundTrip_Typical(b *testing.B) {
	registry, spider := newBenchRegistry()
	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com/detail/123",
		shttp.WithCallback(spider.ParseDetail),
		shttp.WithMeta(map[string]any{"page": 1, "depth": 2, "source": "list"}),
		shttp.WithPriority(5),
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data, err := s.Serialize(req)
		if err != nil {
			b.Fatal(err)
		}
		_, err = s.Deserialize(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}
