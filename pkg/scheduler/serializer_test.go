package scheduler

import (
	"context"
	"testing"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

func TestRequestSerializerBasic(t *testing.T) {
	s := NewRequestSerializer(nil, nil)

	req := shttp.MustNewRequest("https://example.com/page",
		shttp.WithMethod("POST"),
		shttp.WithBody([]byte(`{"key":"value"}`)),
		shttp.WithPriority(5),
		shttp.WithHeader("Content-Type", "application/json"),
	)

	// 序列化
	data, err := s.Serialize(req)
	if err != nil {
		t.Fatalf("serialize failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("serialized data should not be empty")
	}

	// 反序列化
	restored, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}

	// 验证字段
	if restored.URL.String() != "https://example.com/page" {
		t.Errorf("URL mismatch: %s", restored.URL.String())
	}
	if restored.Method != "POST" {
		t.Errorf("Method mismatch: %s", restored.Method)
	}
	if string(restored.Body) != `{"key":"value"}` {
		t.Errorf("Body mismatch: %s", string(restored.Body))
	}
	if restored.Priority != 5 {
		t.Errorf("Priority mismatch: %d", restored.Priority)
	}
	if restored.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("Header mismatch: %s", restored.Headers.Get("Content-Type"))
	}
}

func TestRequestSerializerWithRegistry(t *testing.T) {
	registry := shttp.NewCallbackRegistry()

	// 注册一个简单的回调
	parseFn := func(ctx context.Context, resp *shttp.Response) ([]any, error) {
		return nil, nil
	}
	registry.Register("Parse", parseFn)

	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com",
		shttp.WithCallback(parseFn),
	)

	// 序列化
	data, err := s.Serialize(req)
	if err != nil {
		t.Fatalf("serialize failed: %v", err)
	}

	// 反序列化
	restored, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}

	// 回调应被恢复
	if restored.Callback == nil {
		t.Error("callback should be restored")
	}
}

func TestRequestSerializerRoundTrip(t *testing.T) {
	s := NewRequestSerializer(nil, nil)

	tests := []struct {
		name string
		req  *shttp.Request
	}{
		{
			name: "simple GET",
			req:  shttp.MustNewRequest("https://example.com"),
		},
		{
			name: "POST with body",
			req: shttp.MustNewRequest("https://example.com/api",
				shttp.WithMethod("POST"),
				shttp.WithBody([]byte(`{"data":"test"}`)),
			),
		},
		{
			name: "with meta",
			req: shttp.MustNewRequest("https://example.com",
				shttp.WithMeta(map[string]any{"depth": 3, "source": "crawl"}),
			),
		},
		{
			name: "with priority",
			req: shttp.MustNewRequest("https://example.com",
				shttp.WithPriority(-5),
			),
		},
		{
			name: "with dont_filter",
			req: shttp.MustNewRequest("https://example.com",
				shttp.WithDontFilter(true),
			),
		},
		{
			name: "with flags",
			req: shttp.MustNewRequest("https://example.com",
				shttp.WithFlags("cached", "redirected"),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := s.Serialize(tt.req)
			if err != nil {
				t.Fatalf("serialize failed: %v", err)
			}

			restored, err := s.Deserialize(data)
			if err != nil {
				t.Fatalf("deserialize failed: %v", err)
			}

			if restored.URL.String() != tt.req.URL.String() {
				t.Errorf("URL mismatch: got %s, want %s", restored.URL.String(), tt.req.URL.String())
			}
			if restored.Method != tt.req.Method {
				t.Errorf("Method mismatch: got %s, want %s", restored.Method, tt.req.Method)
			}
			if restored.Priority != tt.req.Priority {
				t.Errorf("Priority mismatch: got %d, want %d", restored.Priority, tt.req.Priority)
			}
			if restored.DontFilter != tt.req.DontFilter {
				t.Errorf("DontFilter mismatch: got %v, want %v", restored.DontFilter, tt.req.DontFilter)
			}
		})
	}
}

func TestRequestSerializerInvalidData(t *testing.T) {
	s := NewRequestSerializer(nil, nil)

	// 无效 JSON
	_, err := s.Deserialize([]byte("not json"))
	if err == nil {
		t.Error("should fail on invalid JSON")
	}

	// 缺少 URL
	_, err = s.Deserialize([]byte(`{"method":"GET"}`))
	if err == nil {
		t.Error("should fail on missing URL")
	}
}

func TestRequestSerializerWithErrback(t *testing.T) {
	registry := shttp.NewCallbackRegistry()

	errbackFn := func(ctx context.Context, err error, req *shttp.Request) ([]any, error) {
		return nil, nil
	}
	registry.RegisterErrback("HandleError", errbackFn)

	s := NewRequestSerializer(registry, nil)

	req := shttp.MustNewRequest("https://example.com",
		shttp.WithErrback(errbackFn),
	)

	// 序列化
	data, err := s.Serialize(req)
	if err != nil {
		t.Fatalf("serialize failed: %v", err)
	}

	// 反序列化
	restored, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}

	// 错误回调应被恢复
	if restored.Errback == nil {
		t.Error("errback should be restored")
	}
}

func TestRequestSerializerNilCallback(t *testing.T) {
	s := NewRequestSerializer(nil, nil)

	req := shttp.MustNewRequest("https://example.com")
	// Callback 和 Errback 都为 nil

	data, err := s.Serialize(req)
	if err != nil {
		t.Fatalf("serialize failed: %v", err)
	}

	restored, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}

	if restored.Callback != nil {
		t.Error("callback should be nil")
	}
	if restored.Errback != nil {
		t.Error("errback should be nil")
	}
}

func TestRequestSerializerUnregisteredCallback(t *testing.T) {
	registry := shttp.NewCallbackRegistry()
	s := NewRequestSerializer(registry, nil)

	// 使用未注册的回调
	unregisteredFn := func(ctx context.Context, resp *shttp.Response) ([]any, error) {
		return nil, nil
	}

	req := shttp.MustNewRequest("https://example.com",
		shttp.WithCallback(unregisteredFn),
	)

	// 序列化应成功（未注册的回调名称为空字符串）
	data, err := s.Serialize(req)
	if err != nil {
		t.Fatalf("serialize failed: %v", err)
	}

	// 反序列化应成功（回调为 nil）
	restored, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}

	// 未注册的回调不会被恢复
	if restored.Callback != nil {
		t.Error("unregistered callback should not be restored")
	}
}
