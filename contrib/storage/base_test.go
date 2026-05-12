package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// ============================================================================
// Mock StorageWriter
// ============================================================================

// MockWriter 是用于测试的 StorageWriter mock 实现。
type MockWriter struct {
	mu           sync.Mutex
	connected    bool
	closed       bool
	batches      [][]map[string]any
	upsertCalls  []upsertCall
	connectErr   error
	closeErr     error
	writeErr     error
	upsertErr    error
	writtenCount int
}

type upsertCall struct {
	UniqueKey string
	Items     []map[string]any
}

func NewMockWriter() *MockWriter {
	return &MockWriter{}
}

func (m *MockWriter) Connect(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *MockWriter) Close(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeErr != nil {
		return m.closeErr
	}
	m.closed = true
	return nil
}

func (m *MockWriter) WriteBatch(_ context.Context, items []map[string]any) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.batches = append(m.batches, items)
	m.writtenCount += len(items)
	return len(items), nil
}

func (m *MockWriter) UpsertBatch(_ context.Context, uniqueKey string, items []map[string]any) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upsertErr != nil {
		return 0, m.upsertErr
	}
	m.upsertCalls = append(m.upsertCalls, upsertCall{UniqueKey: uniqueKey, Items: items})
	m.writtenCount += len(items)
	return len(items), nil
}

func (m *MockWriter) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func (m *MockWriter) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *MockWriter) BatchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.batches)
}

func (m *MockWriter) UpsertCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.upsertCalls)
}

func (m *MockWriter) TotalWritten() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writtenCount
}

func (m *MockWriter) AllBatches() [][]map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]map[string]any, len(m.batches))
	copy(result, m.batches)
	return result
}

// ============================================================================
// BasePipeline 测试
// ============================================================================

func TestBasePipeline_OpenClose(t *testing.T) {
	mock := NewMockWriter()
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 10,
	})

	ctx := context.Background()

	if err := p.Open(ctx); err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	if !mock.IsConnected() {
		t.Fatal("Writer 应已连接")
	}

	if err := p.Close(ctx); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if !mock.IsClosed() {
		t.Fatal("Writer 应已关闭")
	}
}

func TestBasePipeline_OpenError(t *testing.T) {
	mock := NewMockWriter()
	mock.connectErr = fmt.Errorf("连接失败")
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 10,
	})

	ctx := context.Background()
	if err := p.Open(ctx); err == nil {
		t.Fatal("Open 应返回错误")
	}
}

func TestBasePipeline_ProcessItem_MapItem(t *testing.T) {
	mock := NewMockWriter()
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 3,
	})

	ctx := context.Background()
	_ = p.Open(ctx)

	// 插入 2 条，不触发刷新
	for i := 0; i < 2; i++ {
		item := map[string]any{"title": fmt.Sprintf("item_%d", i)}
		result, err := p.ProcessItem(ctx, item)
		if err != nil {
			t.Fatalf("ProcessItem 失败: %v", err)
		}
		if result == nil {
			t.Fatal("ProcessItem 不应返回 nil")
		}
	}
	if mock.BatchCount() != 0 {
		t.Fatalf("不应触发刷新，实际 batch 数: %d", mock.BatchCount())
	}

	// 第 3 条触发刷新
	item := map[string]any{"title": "item_2"}
	_, err := p.ProcessItem(ctx, item)
	if err != nil {
		t.Fatalf("ProcessItem 失败: %v", err)
	}
	if mock.BatchCount() != 1 {
		t.Fatalf("应触发 1 次刷新，实际: %d", mock.BatchCount())
	}
	if mock.TotalWritten() != 3 {
		t.Fatalf("应写入 3 条，实际: %d", mock.TotalWritten())
	}
}

func TestBasePipeline_ProcessItem_StructItem(t *testing.T) {
	type Book struct {
		Title  string `item:"title"`
		Author string `item:"author"`
	}

	mock := NewMockWriter()
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 1,
	})

	ctx := context.Background()
	_ = p.Open(ctx)

	book := &Book{Title: "Go 编程", Author: "张三"}
	_, err := p.ProcessItem(ctx, book)
	if err != nil {
		t.Fatalf("ProcessItem 失败: %v", err)
	}

	if mock.BatchCount() != 1 {
		t.Fatalf("应触发 1 次刷新，实际: %d", mock.BatchCount())
	}

	batches := mock.AllBatches()
	if len(batches[0]) != 1 {
		t.Fatalf("批次应包含 1 条记录，实际: %d", len(batches[0]))
	}

	record := batches[0][0]
	if record["title"] != "Go 编程" {
		t.Errorf("title 不匹配: %v", record["title"])
	}
	if record["author"] != "张三" {
		t.Errorf("author 不匹配: %v", record["author"])
	}
}

func TestBasePipeline_Close_FlushesRemaining(t *testing.T) {
	mock := NewMockWriter()
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 100, // 大 batch size，不会自动触发
	})

	ctx := context.Background()
	_ = p.Open(ctx)

	// 插入 5 条
	for i := 0; i < 5; i++ {
		item := map[string]any{"id": i}
		_, _ = p.ProcessItem(ctx, item)
	}

	if mock.BatchCount() != 0 {
		t.Fatal("Close 前不应有刷新")
	}

	// Close 应刷新剩余数据
	_ = p.Close(ctx)
	if mock.TotalWritten() != 5 {
		t.Fatalf("Close 后应写入 5 条，实际: %d", mock.TotalWritten())
	}
}

func TestBasePipeline_UpsertMode(t *testing.T) {
	mock := NewMockWriter()
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 2,
		UpsertKey: "url",
	})

	ctx := context.Background()
	_ = p.Open(ctx)

	_, _ = p.ProcessItem(ctx, map[string]any{"url": "http://a.com", "title": "A"})
	_, _ = p.ProcessItem(ctx, map[string]any{"url": "http://b.com", "title": "B"})

	if mock.UpsertCallCount() != 1 {
		t.Fatalf("应触发 1 次 upsert，实际: %d", mock.UpsertCallCount())
	}
}

func TestBasePipeline_WriteError(t *testing.T) {
	mock := NewMockWriter()
	mock.writeErr = fmt.Errorf("写入失败")
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 1,
	})

	ctx := context.Background()
	_ = p.Open(ctx)

	_, err := p.ProcessItem(ctx, map[string]any{"id": 1})
	if err == nil {
		t.Fatal("ProcessItem 应返回错误")
	}
}

func TestBasePipeline_CustomConverter(t *testing.T) {
	mock := NewMockWriter()
	converter := func(item any) (map[string]any, error) {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("不支持的类型")
		}
		// 自定义转换：添加额外字段
		m["source"] = "scrapy-go"
		return m, nil
	}

	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 1,
		Converter: converter,
	})

	ctx := context.Background()
	_ = p.Open(ctx)

	_, err := p.ProcessItem(ctx, map[string]any{"title": "test"})
	if err != nil {
		t.Fatalf("ProcessItem 失败: %v", err)
	}

	batches := mock.AllBatches()
	if batches[0][0]["source"] != "scrapy-go" {
		t.Error("自定义转换器未生效")
	}
}

func TestBasePipeline_ConcurrentProcessItem(t *testing.T) {
	mock := NewMockWriter()
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 10,
	})

	ctx := context.Background()
	_ = p.Open(ctx)

	// 并发写入 100 条
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			item := map[string]any{"id": idx}
			_, _ = p.ProcessItem(ctx, item)
		}(i)
	}
	wg.Wait()

	// Close 刷新剩余
	_ = p.Close(ctx)

	if mock.TotalWritten() != 100 {
		t.Fatalf("应写入 100 条，实际: %d", mock.TotalWritten())
	}
}

func TestBasePipeline_EmptyFlush(t *testing.T) {
	mock := NewMockWriter()
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 10,
	})

	ctx := context.Background()
	// 空缓冲区刷新不应报错
	if err := p.Flush(ctx); err != nil {
		t.Fatalf("空缓冲区 Flush 不应报错: %v", err)
	}
	if mock.BatchCount() != 0 {
		t.Fatal("空缓冲区不应触发写入")
	}
}

func TestBasePipeline_NilItem(t *testing.T) {
	mock := NewMockWriter()
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 10,
	})

	ctx := context.Background()
	_ = p.Open(ctx)

	// nil item 应返回错误
	_, err := p.ProcessItem(ctx, nil)
	if err == nil {
		t.Fatal("nil item 应返回错误")
	}
}

func TestBasePipeline_UnsupportedItemType(t *testing.T) {
	mock := NewMockWriter()
	p := NewBasePipeline(BasePipelineConfig{
		Writer:    mock,
		BatchSize: 10,
	})

	ctx := context.Background()
	_ = p.Open(ctx)

	// 不支持的类型应返回错误
	_, err := p.ProcessItem(ctx, 42)
	if err == nil {
		t.Fatal("不支持的 item 类型应返回错误")
	}
}

func TestDefaultConverter_Map(t *testing.T) {
	m := map[string]any{"key": "value"}
	result, err := defaultConverter(m)
	if err != nil {
		t.Fatalf("defaultConverter 失败: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("key 不匹配: %v", result["key"])
	}
}

func TestDefaultConverter_Struct(t *testing.T) {
	type Item struct {
		Name string `item:"name"`
		Age  int    `item:"age"`
	}

	result, err := defaultConverter(&Item{Name: "test", Age: 18})
	if err != nil {
		t.Fatalf("defaultConverter 失败: %v", err)
	}
	if result["name"] != "test" {
		t.Errorf("name 不匹配: %v", result["name"])
	}
}

func TestDefaultConverter_Nil(t *testing.T) {
	_, err := defaultConverter(nil)
	if err == nil {
		t.Fatal("nil 应返回错误")
	}
}

func TestDefaultConverter_UnsupportedType(t *testing.T) {
	_, err := defaultConverter(42)
	if err == nil {
		t.Fatal("不支持的类型应返回错误")
	}
}

// ============================================================================
// 接口满足性检查
// ============================================================================

var (
	_ StorageWriter = (*MockWriter)(nil)
	_ UpsertWriter  = (*MockWriter)(nil)
)
