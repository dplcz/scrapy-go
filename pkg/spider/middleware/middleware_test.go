package middleware

import (
	"context"
	"errors"
	"testing"

	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/spider"
)

func TestManagerScrapeResponseNormal(t *testing.T) {
	m := NewManager(nil)

	scrapeFunc := func(ctx context.Context, resp *scrapy_http.Response) ([]spider.SpiderOutput, error) {
		return []spider.SpiderOutput{
			{Item: map[string]any{"title": "test"}},
		}, nil
	}

	resp := scrapy_http.MustNewResponse("https://example.com", 200)
	outputs, err := m.ScrapeResponse(context.Background(), scrapeFunc, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}
	if !outputs[0].IsItem() {
		t.Error("output should be item")
	}
}

func TestManagerProcessSpiderInputOrder(t *testing.T) {
	m := NewManager(nil)

	var order []string
	m.AddMiddleware(&inputTrackingMW{name: "mw1", order: &order}, "mw1", 100)
	m.AddMiddleware(&inputTrackingMW{name: "mw2", order: &order}, "mw2", 200)

	scrapeFunc := func(ctx context.Context, resp *scrapy_http.Response) ([]spider.SpiderOutput, error) {
		order = append(order, "callback")
		return nil, nil
	}

	resp := scrapy_http.MustNewResponse("https://example.com", 200)
	m.ScrapeResponse(context.Background(), scrapeFunc, resp)

	// ProcessSpiderInput: 正序 (100 → 200)
	// Callback
	expected := []string{"mw1:input", "mw2:input", "callback"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, exp := range expected {
		if order[i] != exp {
			t.Errorf("step %d: expected %s, got %s", i, exp, order[i])
		}
	}
}

func TestManagerProcessSpiderOutputOrder(t *testing.T) {
	m := NewManager(nil)

	var order []string
	m.AddMiddleware(&outputTrackingMW{name: "mw1", order: &order}, "mw1", 100)
	m.AddMiddleware(&outputTrackingMW{name: "mw2", order: &order}, "mw2", 200)

	scrapeFunc := func(ctx context.Context, resp *scrapy_http.Response) ([]spider.SpiderOutput, error) {
		return []spider.SpiderOutput{{Item: "test"}}, nil
	}

	resp := scrapy_http.MustNewResponse("https://example.com", 200)
	m.ScrapeResponse(context.Background(), scrapeFunc, resp)

	// ProcessSpiderOutput: 逆序 (200 → 100)
	expected := []string{"mw2:output", "mw1:output"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, exp := range expected {
		if order[i] != exp {
			t.Errorf("step %d: expected %s, got %s", i, exp, order[i])
		}
	}
}

func TestManagerProcessSpiderInputError(t *testing.T) {
	m := NewManager(nil)

	testErr := errors.New("input error")
	m.AddMiddleware(&errorInputMW{err: testErr}, "error_mw", 100)

	callbackCalled := false
	scrapeFunc := func(ctx context.Context, resp *scrapy_http.Response) ([]spider.SpiderOutput, error) {
		callbackCalled = true
		return nil, nil
	}

	resp := scrapy_http.MustNewResponse("https://example.com", 200)
	_, err := m.ScrapeResponse(context.Background(), scrapeFunc, resp)

	if callbackCalled {
		t.Error("callback should not be called when input middleware returns error")
	}
	if err == nil {
		t.Error("should return error")
	}
}

func TestManagerProcessSpiderException(t *testing.T) {
	m := NewManager(nil)

	// 添加一个将异常转换为输出的中间件
	m.AddMiddleware(&exceptionRecoveryMW{}, "recovery", 100)

	callbackErr := errors.New("callback error")
	scrapeFunc := func(ctx context.Context, resp *scrapy_http.Response) ([]spider.SpiderOutput, error) {
		return nil, callbackErr
	}

	resp := scrapy_http.MustNewResponse("https://example.com", 200)
	outputs, err := m.ScrapeResponse(context.Background(), scrapeFunc, resp)

	if err != nil {
		t.Fatalf("error should be recovered: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output from recovery, got %d", len(outputs))
	}
	if !outputs[0].IsItem() {
		t.Error("recovered output should be item")
	}
}

func TestManagerCount(t *testing.T) {
	m := NewManager(nil)
	if m.Count() != 0 {
		t.Error("new manager should have 0 middlewares")
	}
	m.AddMiddleware(&BaseSpiderMiddleware{}, "mw1", 100)
	m.AddMiddleware(&BaseSpiderMiddleware{}, "mw2", 200)
	if m.Count() != 2 {
		t.Errorf("expected 2, got %d", m.Count())
	}
}

func TestInterfaceImplementations(t *testing.T) {
	var _ SpiderMiddleware = (*BaseSpiderMiddleware)(nil)
}

// ============================================================================
// 测试辅助类型
// ============================================================================

type inputTrackingMW struct {
	BaseSpiderMiddleware
	name  string
	order *[]string
}

func (m *inputTrackingMW) ProcessSpiderInput(ctx context.Context, response *scrapy_http.Response) error {
	*m.order = append(*m.order, m.name+":input")
	return nil
}

type outputTrackingMW struct {
	BaseSpiderMiddleware
	name  string
	order *[]string
}

func (m *outputTrackingMW) ProcessSpiderOutput(ctx context.Context, response *scrapy_http.Response, result []spider.SpiderOutput) ([]spider.SpiderOutput, error) {
	*m.order = append(*m.order, m.name+":output")
	return result, nil
}

type errorInputMW struct {
	BaseSpiderMiddleware
	err error
}

func (m *errorInputMW) ProcessSpiderInput(ctx context.Context, response *scrapy_http.Response) error {
	return m.err
}

type exceptionRecoveryMW struct {
	BaseSpiderMiddleware
}

func (m *exceptionRecoveryMW) ProcessSpiderException(ctx context.Context, response *scrapy_http.Response, err error) ([]spider.SpiderOutput, error) {
	return []spider.SpiderOutput{
		{Item: map[string]any{"recovered": true, "error": err.Error()}},
	}, nil
}
