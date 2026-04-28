package feedexport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// FileStorage 测试
// ============================================================================

func TestFileStorage_OpenStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	s, err := NewFileStorage(path, true)
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}

	w, err := s.Open(context.Background(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := s.Store(context.Background(), w); err != nil {
		t.Fatalf("Store: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("want 'hello', got %q", data)
	}
}

func TestFileStorage_CreatesMissingDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sub", "out.txt")

	s, err := NewFileStorage(path, true)
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	w, err := s.Open(context.Background(), nil)
	if err != nil {
		t.Fatalf("Open with missing dir: %v", err)
	}
	_, _ = w.Write([]byte("x"))
	_ = s.Store(context.Background(), w)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file at %s, stat err: %v", path, err)
	}
}

func TestFileStorage_OverwriteVsAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	// 先写一次
	s1, _ := NewFileStorage(path, true)
	w1, _ := s1.Open(context.Background(), nil)
	_, _ = w1.Write([]byte("first"))
	_ = s1.Store(context.Background(), w1)

	// 再以 append 模式写
	s2, _ := NewFileStorage(path, false)
	w2, _ := s2.Open(context.Background(), nil)
	_, _ = w2.Write([]byte("second"))
	_ = s2.Store(context.Background(), w2)

	data, _ := os.ReadFile(path)
	if string(data) != "firstsecond" {
		t.Errorf("append mode: want 'firstsecond', got %q", data)
	}

	// 再以 overwrite 模式写
	s3, _ := NewFileStorage(path, true)
	w3, _ := s3.Open(context.Background(), nil)
	_, _ = w3.Write([]byte("third"))
	_ = s3.Store(context.Background(), w3)

	data, _ = os.ReadFile(path)
	if string(data) != "third" {
		t.Errorf("overwrite mode: want 'third', got %q", data)
	}
}

func TestFileStorage_FileURI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	uri := "file://" + path

	s, err := NewFileStorage(uri, true)
	if err != nil {
		t.Fatalf("NewFileStorage with file uri: %v", err)
	}
	if s.Path() != path {
		t.Errorf("Path: want %s, got %s", path, s.Path())
	}
}

// ============================================================================
// StdoutStorage 测试
// ============================================================================

func TestStdoutStorage(t *testing.T) {
	s := NewStdoutStorage()
	w, err := s.Open(context.Background(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if w == nil {
		t.Fatal("Open returned nil writer")
	}
	// Close should be no-op
	if err := s.Store(context.Background(), w); err != nil {
		t.Errorf("Store: %v", err)
	}
}

// ============================================================================
// URI 模板渲染测试
// ============================================================================

func TestURIParams_Render(t *testing.T) {
	p := NewURIParams("myspider")
	p.Time = "2026-04-28T10-00-00"
	p.BatchTime = p.Time

	template := "output-%(name)s-%(time)s.json"
	got := p.Render(template)
	want := "output-myspider-2026-04-28T10-00-00.json"
	if got != want {
		t.Errorf("Render: want %q, got %q", want, got)
	}
}

func TestURIParams_BatchID(t *testing.T) {
	p := NewURIParams("x")
	p.BatchID = 3

	got := p.Render("part-%(batch_id)d.json")
	want := "part-3.json"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestURIParams_UnknownPlaceholder(t *testing.T) {
	p := NewURIParams("spider")
	// 未知占位符保持原样
	template := "x-%(unknown)s-%(name)s"
	got := p.Render(template)
	if !strings.Contains(got, "spider") {
		t.Errorf("want %%spider%% expanded, got %q", got)
	}
	if !strings.Contains(got, "%(unknown)s") {
		t.Errorf("unknown placeholder should remain, got %q", got)
	}
}

// ============================================================================
// NewStorageForURI 测试
// ============================================================================

func TestNewStorageForURI(t *testing.T) {
	tests := []struct {
		uri  string
		kind string
	}{
		{"/tmp/x.json", "file"},
		{"relative/path.json", "file"},
		{"file:///tmp/x.json", "file"},
		{"stdout:", "stdout"},
		{"-", "stdout"},
	}
	for _, tt := range tests {
		s, err := NewStorageForURI(tt.uri, true)
		if err != nil {
			t.Errorf("NewStorageForURI(%q): %v", tt.uri, err)
			continue
		}
		switch tt.kind {
		case "file":
			if _, ok := s.(*FileStorage); !ok {
				t.Errorf("uri %q: want FileStorage, got %T", tt.uri, s)
			}
		case "stdout":
			if _, ok := s.(*StdoutStorage); !ok {
				t.Errorf("uri %q: want StdoutStorage, got %T", tt.uri, s)
			}
		}
	}
}

func TestNewStorageForURI_UnsupportedScheme(t *testing.T) {
	_, err := NewStorageForURI("s3://bucket/key", true)
	if err == nil {
		t.Error("expected error for unsupported scheme")
	}
}

// ============================================================================
// FeedSlot 端到端测试
// ============================================================================

func TestFeedSlot_JSONFileEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	slot, err := NewFeedSlot(FeedConfig{
		URI:       path,
		Format:    FormatJSON,
		Overwrite: true,
		Options:   DefaultExporterOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("NewFeedSlot: %v", err)
	}

	ctx := context.Background()
	if err := slot.ExportItem(ctx, nil, map[string]any{"a": 1}); err != nil {
		t.Fatalf("ExportItem: %v", err)
	}
	if err := slot.ExportItem(ctx, nil, map[string]any{"a": 2}); err != nil {
		t.Fatalf("ExportItem: %v", err)
	}
	if err := slot.Close(ctx, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	if len(items) != 2 {
		t.Errorf("want 2 items, got %d", len(items))
	}
	if slot.ItemCount() != 2 {
		t.Errorf("ItemCount: want 2, got %d", slot.ItemCount())
	}
}

func TestFeedSlot_FilterSkipsItems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.jsonl")

	slot, err := NewFeedSlot(FeedConfig{
		URI:       path,
		Format:    FormatJSONLines,
		Overwrite: true,
		Options:   DefaultExporterOptions(),
		Filter: func(item any) bool {
			m, ok := item.(map[string]any)
			if !ok {
				return false
			}
			return m["ok"] == true
		},
		StoreEmpty: true,
	}, nil)
	if err != nil {
		t.Fatalf("NewFeedSlot: %v", err)
	}

	ctx := context.Background()
	_ = slot.ExportItem(ctx, nil, map[string]any{"ok": true, "v": 1})
	_ = slot.ExportItem(ctx, nil, map[string]any{"ok": false, "v": 2})
	_ = slot.ExportItem(ctx, nil, map[string]any{"ok": true, "v": 3})
	_ = slot.Close(ctx, nil)

	if slot.ItemCount() != 2 {
		t.Errorf("want 2 items (filter), got %d", slot.ItemCount())
	}
}

func TestFeedSlot_StoreEmptyFalse_NoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	slot, _ := NewFeedSlot(FeedConfig{
		URI:        path,
		Format:     FormatJSON,
		Overwrite:  true,
		StoreEmpty: false,
		Options:    DefaultExporterOptions(),
	}, nil)

	ctx := context.Background()
	if err := slot.Close(ctx, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should not exist when StoreEmpty=false and no items; stat err: %v", err)
	}
}

func TestFeedSlot_StoreEmptyTrue_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	slot, _ := NewFeedSlot(FeedConfig{
		URI:        path,
		Format:     FormatJSON,
		Overwrite:  true,
		StoreEmpty: true,
		Options:    DefaultExporterOptions(),
	}, nil)

	ctx := context.Background()
	if err := slot.Close(ctx, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("empty slot: want '[]', got %q", data)
	}
}

func TestFeedSlot_InvalidConfig(t *testing.T) {
	_, err := NewFeedSlot(FeedConfig{URI: "", Format: FormatJSON}, nil)
	if err == nil {
		t.Error("expected error for empty URI")
	}

	_, err = NewFeedSlot(FeedConfig{URI: "x.json", Format: ""}, nil)
	if err == nil {
		t.Error("expected error for empty Format")
	}

	_, err = NewFeedSlot(FeedConfig{URI: "x.json", Format: "nonsense"}, nil)
	if err == nil {
		t.Error("expected error for unknown Format")
	}
}
