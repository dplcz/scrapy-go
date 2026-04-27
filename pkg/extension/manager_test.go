package extension

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
)

// ============================================================================
// 测试用 Mock 扩展
// ============================================================================

// mockExtension 是一个用于测试的 Mock 扩展。
type mockExtension struct {
	BaseExtension
	openCalled  bool
	closeCalled bool
	openErr     error
	closeErr    error
}

func (m *mockExtension) Open(ctx context.Context) error {
	m.openCalled = true
	return m.openErr
}

func (m *mockExtension) Close(ctx context.Context) error {
	m.closeCalled = true
	return m.closeErr
}

// ============================================================================
// Manager 测试
// ============================================================================

func TestNewManager(t *testing.T) {
	m := NewManager(nil)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 extensions, got %d", m.Count())
	}
}

func TestManager_AddExtension(t *testing.T) {
	m := NewManager(slog.Default())

	ext1 := &mockExtension{}
	ext2 := &mockExtension{}
	ext3 := &mockExtension{}

	m.AddExtension(ext1, "Ext1", 300)
	m.AddExtension(ext2, "Ext2", 100)
	m.AddExtension(ext3, "Ext3", 200)

	if m.Count() != 3 {
		t.Fatalf("expected 3 extensions, got %d", m.Count())
	}

	// 验证按优先级排序
	exts := m.Extensions()
	if exts[0].Name != "Ext2" || exts[0].Priority != 100 {
		t.Errorf("expected first extension to be Ext2(100), got %s(%d)", exts[0].Name, exts[0].Priority)
	}
	if exts[1].Name != "Ext3" || exts[1].Priority != 200 {
		t.Errorf("expected second extension to be Ext3(200), got %s(%d)", exts[1].Name, exts[1].Priority)
	}
	if exts[2].Name != "Ext1" || exts[2].Priority != 300 {
		t.Errorf("expected third extension to be Ext1(300), got %s(%d)", exts[2].Name, exts[2].Priority)
	}
}

func TestManager_Open(t *testing.T) {
	m := NewManager(slog.Default())

	ext1 := &mockExtension{}
	ext2 := &mockExtension{}

	m.AddExtension(ext1, "Ext1", 100)
	m.AddExtension(ext2, "Ext2", 200)

	err := m.Open(context.Background())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}

	if !ext1.openCalled {
		t.Error("ext1.Open was not called")
	}
	if !ext2.openCalled {
		t.Error("ext2.Open was not called")
	}
}

func TestManager_Open_NotConfigured(t *testing.T) {
	m := NewManager(slog.Default())

	ext1 := &mockExtension{}
	ext2 := &mockExtension{openErr: serrors.ErrNotConfigured}
	ext3 := &mockExtension{}

	m.AddExtension(ext1, "Ext1", 100)
	m.AddExtension(ext2, "Ext2", 200)
	m.AddExtension(ext3, "Ext3", 300)

	err := m.Open(context.Background())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}

	// ext2 应该被跳过，只剩 ext1 和 ext3
	if m.Count() != 2 {
		t.Fatalf("expected 2 active extensions, got %d", m.Count())
	}

	exts := m.Extensions()
	if exts[0].Name != "Ext1" {
		t.Errorf("expected first active extension to be Ext1, got %s", exts[0].Name)
	}
	if exts[1].Name != "Ext3" {
		t.Errorf("expected second active extension to be Ext3, got %s", exts[1].Name)
	}
}

func TestManager_Open_Error(t *testing.T) {
	m := NewManager(slog.Default())

	expectedErr := errors.New("init failed")
	ext1 := &mockExtension{}
	ext2 := &mockExtension{openErr: expectedErr}

	m.AddExtension(ext1, "Ext1", 100)
	m.AddExtension(ext2, "Ext2", 200)

	err := m.Open(context.Background())
	if err == nil {
		t.Fatal("expected error from Open")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestManager_Close(t *testing.T) {
	m := NewManager(slog.Default())

	ext1 := &mockExtension{}
	ext2 := &mockExtension{}

	m.AddExtension(ext1, "Ext1", 100)
	m.AddExtension(ext2, "Ext2", 200)

	// 先打开
	_ = m.Open(context.Background())

	// 再关闭
	err := m.Close(context.Background())
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if !ext1.closeCalled {
		t.Error("ext1.Close was not called")
	}
	if !ext2.closeCalled {
		t.Error("ext2.Close was not called")
	}
}

func TestManager_Close_Error(t *testing.T) {
	m := NewManager(slog.Default())

	expectedErr := errors.New("close failed")
	ext1 := &mockExtension{closeErr: expectedErr}
	ext2 := &mockExtension{}

	m.AddExtension(ext1, "Ext1", 100)
	m.AddExtension(ext2, "Ext2", 200)

	_ = m.Open(context.Background())

	err := m.Close(context.Background())
	if err == nil {
		t.Fatal("expected error from Close")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}

	// 即使 ext1 关闭失败，ext2 也应该被关闭
	if !ext2.closeCalled {
		t.Error("ext2.Close was not called despite ext1 failure")
	}
}

func TestManager_Extensions_Snapshot(t *testing.T) {
	m := NewManager(slog.Default())

	ext := &mockExtension{}
	m.AddExtension(ext, "Ext1", 100)

	snapshot := m.Extensions()
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 extension in snapshot, got %d", len(snapshot))
	}

	// 修改快照不应影响原始数据
	snapshot[0].Name = "Modified"
	exts := m.Extensions()
	if exts[0].Name != "Ext1" {
		t.Error("modifying snapshot affected original data")
	}
}

func TestBaseExtension(t *testing.T) {
	ext := &BaseExtension{}

	if err := ext.Open(context.Background()); err != nil {
		t.Errorf("BaseExtension.Open returned error: %v", err)
	}
	if err := ext.Close(context.Background()); err != nil {
		t.Errorf("BaseExtension.Close returned error: %v", err)
	}
}
