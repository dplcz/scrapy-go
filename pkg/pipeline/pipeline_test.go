package pipeline

import (
	"context"
	"errors"
	"testing"

	scrapy_errors "scrapy-go/pkg/errors"
	"scrapy-go/pkg/stats"
)

func TestManagerProcessItemNormal(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	m := NewManager(nil, sc, nil)

	m.AddPipeline(&uppercasePipeline{}, "uppercase", 100)

	item, err := m.ProcessItem(context.Background(), map[string]any{"name": "test"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := item.(map[string]any)
	if result["name"] != "TEST" {
		t.Errorf("expected 'TEST', got '%v'", result["name"])
	}

	scraped := sc.GetValue("item_scraped_count", 0)
	if scraped != 1 {
		t.Errorf("expected item_scraped_count=1, got %v", scraped)
	}
}

func TestManagerProcessItemChain(t *testing.T) {
	m := NewManager(nil, nil, nil)

	m.AddPipeline(&addFieldPipeline{field: "step1", value: true}, "step1", 100)
	m.AddPipeline(&addFieldPipeline{field: "step2", value: true}, "step2", 200)

	item, err := m.ProcessItem(context.Background(), map[string]any{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := item.(map[string]any)
	if result["step1"] != true {
		t.Error("step1 should be set")
	}
	if result["step2"] != true {
		t.Error("step2 should be set")
	}
}

func TestManagerProcessItemDrop(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	m := NewManager(nil, sc, nil)

	m.AddPipeline(&dropPipeline{}, "drop", 100)
	m.AddPipeline(&addFieldPipeline{field: "after_drop", value: true}, "after", 200)

	_, err := m.ProcessItem(context.Background(), map[string]any{}, nil)
	if !errors.Is(err, scrapy_errors.ErrDropItem) {
		t.Errorf("expected ErrDropItem, got %v", err)
	}

	dropped := sc.GetValue("item_dropped_count", 0)
	if dropped != 1 {
		t.Errorf("expected item_dropped_count=1, got %v", dropped)
	}
}

func TestManagerProcessItemError(t *testing.T) {
	sc := stats.NewMemoryCollector(false, nil)
	m := NewManager(nil, sc, nil)

	testErr := errors.New("pipeline error")
	m.AddPipeline(&errorPipeline{err: testErr}, "error", 100)

	_, err := m.ProcessItem(context.Background(), map[string]any{}, nil)
	if err != testErr {
		t.Errorf("expected pipeline error, got %v", err)
	}

	errCount := sc.GetValue("item_error_count", 0)
	if errCount != 1 {
		t.Errorf("expected item_error_count=1, got %v", errCount)
	}
}

func TestManagerOpenClose(t *testing.T) {
	m := NewManager(nil, nil, nil)

	p := &lifecyclePipeline{}
	m.AddPipeline(p, "lifecycle", 100)

	err := m.Open(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on open: %v", err)
	}
	if !p.opened {
		t.Error("pipeline should be opened")
	}

	err = m.Close(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on close: %v", err)
	}
	if !p.closed {
		t.Error("pipeline should be closed")
	}
}

func TestManagerCount(t *testing.T) {
	m := NewManager(nil, nil, nil)
	if m.Count() != 0 {
		t.Error("new manager should have 0 pipelines")
	}
	m.AddPipeline(&lifecyclePipeline{}, "p1", 100)
	m.AddPipeline(&lifecyclePipeline{}, "p2", 200)
	if m.Count() != 2 {
		t.Errorf("expected 2, got %d", m.Count())
	}
}

func TestManagerPriorityOrder(t *testing.T) {
	m := NewManager(nil, nil, nil)

	var order []string
	m.AddPipeline(&orderTrackingPipeline{name: "third", order: &order}, "third", 300)
	m.AddPipeline(&orderTrackingPipeline{name: "first", order: &order}, "first", 100)
	m.AddPipeline(&orderTrackingPipeline{name: "second", order: &order}, "second", 200)

	m.ProcessItem(context.Background(), map[string]any{}, nil)

	expected := []string{"first", "second", "third"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, exp := range expected {
		if order[i] != exp {
			t.Errorf("step %d: expected %s, got %s", i, exp, order[i])
		}
	}
}

// ============================================================================
// 测试辅助类型
// ============================================================================

type uppercasePipeline struct{}

func (p *uppercasePipeline) Open(ctx context.Context) error  { return nil }
func (p *uppercasePipeline) Close(ctx context.Context) error { return nil }
func (p *uppercasePipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	if m, ok := item.(map[string]any); ok {
		if name, ok := m["name"].(string); ok {
			result := make(map[string]any)
			for k, v := range m {
				result[k] = v
			}
			result["name"] = toUpper(name)
			return result, nil
		}
	}
	return item, nil
}

func toUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

type addFieldPipeline struct {
	field string
	value any
}

func (p *addFieldPipeline) Open(ctx context.Context) error  { return nil }
func (p *addFieldPipeline) Close(ctx context.Context) error { return nil }
func (p *addFieldPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	if m, ok := item.(map[string]any); ok {
		m[p.field] = p.value
	}
	return item, nil
}

type dropPipeline struct{}

func (p *dropPipeline) Open(ctx context.Context) error  { return nil }
func (p *dropPipeline) Close(ctx context.Context) error { return nil }
func (p *dropPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	return nil, scrapy_errors.NewDropItemError("test drop")
}

type errorPipeline struct {
	err error
}

func (p *errorPipeline) Open(ctx context.Context) error  { return nil }
func (p *errorPipeline) Close(ctx context.Context) error { return nil }
func (p *errorPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	return nil, p.err
}

type lifecyclePipeline struct {
	opened bool
	closed bool
}

func (p *lifecyclePipeline) Open(ctx context.Context) error {
	p.opened = true
	return nil
}
func (p *lifecyclePipeline) Close(ctx context.Context) error {
	p.closed = true
	return nil
}
func (p *lifecyclePipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	return item, nil
}

type orderTrackingPipeline struct {
	name  string
	order *[]string
}

func (p *orderTrackingPipeline) Open(ctx context.Context) error  { return nil }
func (p *orderTrackingPipeline) Close(ctx context.Context) error { return nil }
func (p *orderTrackingPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	*p.order = append(*p.order, p.name)
	return item, nil
}
