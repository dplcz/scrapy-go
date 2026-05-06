package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
)

// ============================================================================
// 测试用 Item 类型
// ============================================================================

type Book struct {
	Title  string
	Author string
	Price  float64
}

type Article struct {
	Title   string
	Content string
}

// ============================================================================
// 测试用 TypedItemPipeline 实现
// ============================================================================

type bookCleanPipeline struct {
	opened bool
	closed bool
}

func (p *bookCleanPipeline) Open(ctx context.Context) error {
	p.opened = true
	return nil
}

func (p *bookCleanPipeline) Close(ctx context.Context) error {
	p.closed = true
	return nil
}

func (p *bookCleanPipeline) ProcessItem(ctx context.Context, item *Book) (*Book, error) {
	item.Title = strings.TrimSpace(item.Title)
	item.Author = strings.TrimSpace(item.Author)
	return item, nil
}

type bookDropPipeline struct{}

func (p *bookDropPipeline) Open(ctx context.Context) error  { return nil }
func (p *bookDropPipeline) Close(ctx context.Context) error { return nil }
func (p *bookDropPipeline) ProcessItem(ctx context.Context, item *Book) (*Book, error) {
	if item.Price <= 0 {
		return nil, serrors.ErrDropItem
	}
	return item, nil
}

type articlePipeline struct {
	processed bool
}

func (p *articlePipeline) Open(ctx context.Context) error  { return nil }
func (p *articlePipeline) Close(ctx context.Context) error { return nil }
func (p *articlePipeline) ProcessItem(ctx context.Context, item *Article) (*Article, error) {
	p.processed = true
	item.Title = strings.ToUpper(item.Title)
	return item, nil
}

// ============================================================================
// TypedPipeline 测试
// ============================================================================

func TestTypedPipelineProcessItem(t *testing.T) {
	inner := &bookCleanPipeline{}
	tp := NewTypedPipeline[*Book](inner)

	if err := tp.Open(context.Background()); err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if !inner.opened {
		t.Error("inner pipeline should be opened")
	}

	book := &Book{Title: "  Go Programming  ", Author: "  Author  ", Price: 29.99}
	result, err := tp.ProcessItem(context.Background(), book)
	if err != nil {
		t.Fatalf("ProcessItem failed: %v", err)
	}

	resultBook, ok := result.(*Book)
	if !ok {
		t.Fatalf("expected *Book, got %T", result)
	}
	if resultBook.Title != "Go Programming" {
		t.Errorf("expected trimmed title, got %q", resultBook.Title)
	}
	if resultBook.Author != "Author" {
		t.Errorf("expected trimmed author, got %q", resultBook.Author)
	}

	if err := tp.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if !inner.closed {
		t.Error("inner pipeline should be closed")
	}
}

func TestTypedPipelineTypeMismatchSkips(t *testing.T) {
	inner := &bookCleanPipeline{}
	tp := NewTypedPipeline[*Book](inner)
	tp.Open(context.Background())
	defer tp.Close(context.Background())

	// 传入不匹配的类型（Article 而非 Book）
	article := &Article{Title: "test", Content: "content"}
	result, err := tp.ProcessItem(context.Background(), article)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 应该透传原始 item
	resultArticle, ok := result.(*Article)
	if !ok {
		t.Fatalf("expected *Article passthrough, got %T", result)
	}
	if resultArticle.Title != "test" {
		t.Error("article should be unchanged")
	}
}

func TestTypedPipelineDropItem(t *testing.T) {
	inner := &bookDropPipeline{}
	tp := NewTypedPipeline[*Book](inner)
	tp.Open(context.Background())
	defer tp.Close(context.Background())

	book := &Book{Title: "Free Book", Price: 0}
	_, err := tp.ProcessItem(context.Background(), book)
	if !errors.Is(err, serrors.ErrDropItem) {
		t.Errorf("expected ErrDropItem, got %v", err)
	}
}

func TestTypedPipelineMultipleTypes(t *testing.T) {
	// 测试多个 TypedPipeline 共存，各自处理不同类型
	bookPL := NewTypedPipeline[*Book](&bookCleanPipeline{})
	articlePL := &articlePipeline{}
	articleTyped := NewTypedPipeline[*Article](articlePL)

	mgr := NewManager(nil, nil, nil)
	mgr.AddPipeline(bookPL, "BookClean", 100)
	mgr.AddPipeline(articleTyped, "ArticleUpper", 200)
	mgr.Open(context.Background())
	defer mgr.Close(context.Background())

	// 处理 Book：只有 BookClean 生效
	book := &Book{Title: "  hello  ", Author: "  world  ", Price: 10}
	result, err := mgr.ProcessItem(context.Background(), book, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resultBook := result.(*Book)
	if resultBook.Title != "hello" {
		t.Errorf("expected trimmed title, got %q", resultBook.Title)
	}

	// 处理 Article：只有 ArticleUpper 生效
	article := &Article{Title: "test", Content: "content"}
	result, err = mgr.ProcessItem(context.Background(), article, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resultArticle := result.(*Article)
	if resultArticle.Title != "TEST" {
		t.Errorf("expected uppercased title, got %q", resultArticle.Title)
	}
	if !articlePL.processed {
		t.Error("article pipeline should have been called")
	}
}

func TestTypedPipelineWithMapItem(t *testing.T) {
	// TypedPipeline[*Book] 应该跳过 map 类型的 item
	inner := &bookCleanPipeline{}
	tp := NewTypedPipeline[*Book](inner)
	tp.Open(context.Background())
	defer tp.Close(context.Background())

	mapItem := map[string]any{"title": "test"}
	result, err := tp.ProcessItem(context.Background(), mapItem)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 应该透传
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map passthrough, got %T", result)
	}
	if resultMap["title"] != "test" {
		t.Error("map should be unchanged")
	}
}

func TestTypedPipelineImplementsItemPipeline(t *testing.T) {
	// 编译期检查：TypedPipeline 满足 ItemPipeline 接口
	var _ ItemPipeline = NewTypedPipeline[*Book](&bookCleanPipeline{})
}

// ============================================================================
// Manager + ItemAdapter 自动适配测试
// ============================================================================

type validatedItem struct {
	Name  string `item:"name,required"`
	Email string `item:"email,default=noreply@example.com"`
	Age   int    `item:"age"`
}

type passThroughPipeline struct {
	processed bool
}

func (p *passThroughPipeline) Open(ctx context.Context) error  { return nil }
func (p *passThroughPipeline) Close(ctx context.Context) error { return nil }
func (p *passThroughPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	p.processed = true
	return item, nil
}

func TestManagerValidateItemsFillsDefaults(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	mgr.SetValidateItems(true)

	pl := &passThroughPipeline{}
	mgr.AddPipeline(pl, "passthrough", 100)
	mgr.Open(context.Background())
	defer mgr.Close(context.Background())

	item := &validatedItem{Name: "Alice", Age: 30}
	result, err := mgr.ProcessItem(context.Background(), item, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultItem := result.(*validatedItem)
	if resultItem.Email != "noreply@example.com" {
		t.Errorf("expected default email, got %q", resultItem.Email)
	}
	if !pl.processed {
		t.Error("pipeline should have been called")
	}
}

func TestManagerValidateItemsRejectsInvalid(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	mgr.SetValidateItems(true)

	pl := &passThroughPipeline{}
	mgr.AddPipeline(pl, "passthrough", 100)
	mgr.Open(context.Background())
	defer mgr.Close(context.Background())

	// Name is required but empty
	item := &validatedItem{Age: 30}
	_, err := mgr.ProcessItem(context.Background(), item, nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if pl.processed {
		t.Error("pipeline should NOT have been called when validation fails")
	}
}

func TestManagerValidateItemsSkipsMap(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	mgr.SetValidateItems(true)

	pl := &passThroughPipeline{}
	mgr.AddPipeline(pl, "passthrough", 100)
	mgr.Open(context.Background())
	defer mgr.Close(context.Background())

	// map 类型不会被验证
	item := map[string]any{"name": "test"}
	result, err := mgr.ProcessItem(context.Background(), item, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
	if !pl.processed {
		t.Error("pipeline should have been called for map items")
	}
}

func TestManagerValidateItemsDisabled(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	// validateItems 默认为 false

	pl := &passThroughPipeline{}
	mgr.AddPipeline(pl, "passthrough", 100)
	mgr.Open(context.Background())
	defer mgr.Close(context.Background())

	// Name is required but empty - should pass without validation
	item := &validatedItem{Age: 30}
	_, err := mgr.ProcessItem(context.Background(), item, nil)
	if err != nil {
		t.Fatalf("unexpected error (validation should be disabled): %v", err)
	}
	if !pl.processed {
		t.Error("pipeline should have been called")
	}
}

func TestManagerValidateItemsNilItem(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	mgr.SetValidateItems(true)

	pl := &passThroughPipeline{}
	mgr.AddPipeline(pl, "passthrough", 100)
	mgr.Open(context.Background())
	defer mgr.Close(context.Background())

	// nil item 不会触发验证错误
	result, err := mgr.ProcessItem(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// nil 会被透传
	if result != nil {
		t.Error("nil item should pass through")
	}
}

func TestTypedPipelineWithLogger(t *testing.T) {
	inner := &bookCleanPipeline{}
	tp := NewTypedPipeline[*Book](inner, WithTypedLogger(nil))
	if tp == nil {
		t.Fatal("should create TypedPipeline even with nil logger option")
	}
}
