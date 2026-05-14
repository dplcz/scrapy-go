package http

import (
	"context"
	"fmt"
	"testing"
)

// ============================================================================
// LookupByFunc Benchmark
// ============================================================================

// BenchmarkLookupByFunc_MethodValue 测试通过 runtime.FuncForPC 策略查找
// method value 的性能（策略 1，最常见路径）。
func BenchmarkLookupByFunc_MethodValue(b *testing.B) {
	spider := &mockSpider{}
	registry := NewCallbackRegistry()
	registry.RegisterSpider(spider)

	cb := spider.ParseDetail

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		name, ok := registry.LookupByFunc(cb)
		if !ok || name != "ParseDetail" {
			b.Fatal("unexpected result")
		}
	}
}

// BenchmarkLookupByFunc_ClosureFallback 测试通过 reflect.Pointer 反向索引
// 查找手动注册闭包的性能（策略 2，fallback 路径）。
func BenchmarkLookupByFunc_ClosureFallback(b *testing.B) {
	registry := NewCallbackRegistry()

	// 手动注册一个匿名闭包（extractFuncName 会返回 ""，走 fallback 路径）
	closure := CallbackFunc(func(ctx context.Context, resp *Response) ([]Output, error) {
		return nil, nil
	})
	registry.Register("MyClosure", closure)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		name, ok := registry.LookupByFunc(closure)
		if !ok || name != "MyClosure" {
			b.Fatal("unexpected result")
		}
	}
}

// BenchmarkLookupByFunc_NilCallback 测试 nil 回调的短路性能。
func BenchmarkLookupByFunc_NilCallback(b *testing.B) {
	registry := NewCallbackRegistry()
	registry.RegisterSpider(&mockSpider{})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, ok := registry.LookupByFunc(nil)
		if ok {
			b.Fatal("unexpected result")
		}
	}
}

// BenchmarkLookupByFunc_NotFound 测试查找未注册回调的性能。
func BenchmarkLookupByFunc_NotFound(b *testing.B) {
	registry := NewCallbackRegistry()
	registry.RegisterSpider(&mockSpider{})

	unregistered := CallbackFunc(func(ctx context.Context, resp *Response) ([]Output, error) {
		return nil, nil
	})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, ok := registry.LookupByFunc(unregistered)
		if ok {
			b.Fatal("unexpected result")
		}
	}
}

// BenchmarkLookupErrbackByFunc_MethodValue 测试 Errback 的反向查找性能。
func BenchmarkLookupErrbackByFunc_MethodValue(b *testing.B) {
	spider := &mockSpider{}
	registry := NewCallbackRegistry()
	registry.RegisterSpider(spider)

	eb := spider.HandleError

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		name, ok := registry.LookupErrbackByFunc(eb)
		if !ok || name != "HandleError" {
			b.Fatal("unexpected result")
		}
	}
}

// ============================================================================
// extractFuncName Benchmark
// ============================================================================

// BenchmarkExtractFuncName_MethodValue 测试从 method value 提取方法名的性能。
func BenchmarkExtractFuncName_MethodValue(b *testing.B) {
	spider := &mockSpider{}
	cb := spider.ParseDetail

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		name := extractFuncName(cb)
		if name != "ParseDetail" {
			b.Fatalf("unexpected name: %q", name)
		}
	}
}

// BenchmarkExtractFuncName_Closure 测试匿名闭包（应返回 ""）的性能。
func BenchmarkExtractFuncName_Closure(b *testing.B) {
	closure := func(ctx context.Context, resp *Response) ([]Output, error) {
		return nil, nil
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		name := extractFuncName(closure)
		if name != "" {
			b.Fatalf("unexpected name: %q", name)
		}
	}
}

// ============================================================================
// restoreMetaTypes Benchmark
// ============================================================================

// BenchmarkRestoreMetaTypes_Simple 测试简单 meta（几个基本类型字段）的类型还原性能。
func BenchmarkRestoreMetaTypes_Simple(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		meta := map[string]any{
			"page":   float64(1),
			"depth":  float64(3),
			"source": "crawl",
			"retry":  true,
		}
		restoreMetaTypes(meta)
	}
}

// BenchmarkRestoreMetaTypes_Nested 测试嵌套 meta 的类型还原性能。
func BenchmarkRestoreMetaTypes_Nested(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		meta := map[string]any{
			"page":  float64(1),
			"depth": float64(3),
			"config": map[string]any{
				"max_retry": float64(5),
				"timeout":   float64(30),
				"tags":      []any{float64(1), float64(2), "tag3"},
			},
			"scores": []any{float64(95), float64(87), float64(100)},
		}
		restoreMetaTypes(meta)
	}
}

// BenchmarkRestoreMetaTypes_NoConversion 测试不需要转换的 meta（全是字符串/bool）。
func BenchmarkRestoreMetaTypes_NoConversion(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		meta := map[string]any{
			"source":   "crawl",
			"category": "news",
			"active":   true,
			"tag":      "important",
		}
		restoreMetaTypes(meta)
	}
}

// BenchmarkRestoreMetaTypes_WithFloat 测试包含真正 float64 值（有小数部分）的 meta。
func BenchmarkRestoreMetaTypes_WithFloat(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		meta := map[string]any{
			"page":  float64(1),
			"score": float64(3.14),
			"ratio": float64(0.75),
		}
		restoreMetaTypes(meta)
	}
}

// BenchmarkRestoreMetaTypes_Empty 测试空 meta 的性能基线。
func BenchmarkRestoreMetaTypes_Empty(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		meta := map[string]any{}
		restoreMetaTypes(meta)
	}
}

// ============================================================================
// LookupByFunc 扩展性测试（不同注册方法数量）
// ============================================================================

// benchSpider10 拥有 10 个 Callback 方法，用于测试注册方法数量对查找性能的影响。
type benchSpider10 struct{}

func (s *benchSpider10) Method01(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}
func (s *benchSpider10) Method02(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}
func (s *benchSpider10) Method03(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}
func (s *benchSpider10) Method04(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}
func (s *benchSpider10) Method05(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}
func (s *benchSpider10) Method06(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}
func (s *benchSpider10) Method07(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}
func (s *benchSpider10) Method08(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}
func (s *benchSpider10) Method09(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}
func (s *benchSpider10) Method10(ctx context.Context, resp *Response) ([]Output, error) {
	return nil, nil
}

// BenchmarkLookupByFunc_10Methods 测试注册 10 个方法时的查找性能。
func BenchmarkLookupByFunc_10Methods(b *testing.B) {
	spider := &benchSpider10{}
	registry := NewCallbackRegistry()
	registry.RegisterSpider(spider)

	cb := spider.Method05 // 查找中间位置的方法

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		name, ok := registry.LookupByFunc(cb)
		if !ok || name != "Method05" {
			b.Fatal("unexpected result")
		}
	}
}

// BenchmarkLookupByFunc_50Methods 测试注册 50 个方法时的查找性能。
// 通过手动注册模拟大量方法的场景。
func BenchmarkLookupByFunc_50Methods(b *testing.B) {
	registry := NewCallbackRegistry()

	// 手动注册 50 个方法（使用 benchSpider10 的方法 + 额外手动注册）
	spider := &benchSpider10{}
	registry.RegisterSpider(spider)

	// 额外手动注册 40 个闭包
	for i := 11; i <= 50; i++ {
		name := fmt.Sprintf("Method%02d", i)
		registry.Register(name, CallbackFunc(func(ctx context.Context, resp *Response) ([]Output, error) {
			return nil, nil
		}))
	}

	cb := spider.Method05

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		name, ok := registry.LookupByFunc(cb)
		if !ok || name != "Method05" {
			b.Fatal("unexpected result")
		}
	}
}

// ============================================================================
// Register Benchmark（注册时建立反向索引的开销）
// ============================================================================

// BenchmarkRegister 测试单次 Register 的性能（含反向索引建立）。
func BenchmarkRegister(b *testing.B) {
	cb := CallbackFunc(func(ctx context.Context, resp *Response) ([]Output, error) {
		return nil, nil
	})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		registry := NewCallbackRegistry()
		registry.Register("Parse", cb)
	}
}

// BenchmarkRegisterSpider 测试 RegisterSpider 自动扫描注册的性能。
func BenchmarkRegisterSpider(b *testing.B) {
	spider := &mockSpider{}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		registry := NewCallbackRegistry()
		registry.RegisterSpider(spider)
	}
}
