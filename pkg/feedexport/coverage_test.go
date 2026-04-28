package feedexport

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// 针对覆盖率薄弱点的补充测试
// ============================================================================

// --- XML encodeValue 覆盖多种类型 ---

func TestXMLExporter_Types(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"b", "i", "i64", "f", "s", "bs", "sl", "mp", "nested"}
	e := NewXMLExporter(&buf, opts)

	_ = e.StartExporting()
	err := e.ExportItem(map[string]any{
		"b":      true,
		"i":      42,
		"i64":    int64(64),
		"f":      3.14,
		"s":      "hello",
		"bs":     []byte("bytes"),
		"sl":     []any{1, "two"},
		"mp":     map[string]any{"k": "v"},
		"nested": map[string]any{"inner": []string{"a", "b"}},
	})
	if err != nil {
		t.Fatalf("ExportItem: %v", err)
	}
	_ = e.FinishExporting()

	out := buf.String()
	checks := []string{
		"<b>true</b>", "<i>42</i>", "<i64>64</i64>",
		"<s>hello</s>", "<bs>bytes</bs>",
		"<mp>", "</mp>",
		"<nested>",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("want output to contain %q; got:\n%s", c, out)
		}
	}
}

func TestXMLExporter_NilValues(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"a"}
	e := NewXMLExporter(&buf, opts)

	_ = e.StartExporting()
	if err := e.ExportItem(map[string]any{"a": nil}); err != nil {
		t.Fatalf("ExportItem with nil: %v", err)
	}
	_ = e.FinishExporting()

	if !strings.Contains(buf.String(), "<a></a>") && !strings.Contains(buf.String(), "<a/>") {
		t.Errorf("expected empty <a/>, got: %s", buf.String())
	}
}

// --- CSV formatValue 覆盖更多类型 ---

func TestCSVExporter_ValueTypes(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"b", "i", "f", "s", "bs", "nil"}
	e := NewCSVExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{
		"b":   true,
		"i":   42,
		"f":   3.5,
		"s":   "hello",
		"bs":  []byte("xyz"),
		"nil": nil,
	})
	_ = e.FinishExporting()

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d:\n%s", len(lines), out)
	}
	row := lines[1]
	// 期望: true,42,3.5,hello,xyz,<empty>
	fields := strings.Split(row, ",")
	if len(fields) != 6 {
		t.Fatalf("want 6 fields, got %d: %q", len(fields), row)
	}
	if fields[0] != "true" {
		t.Errorf("bool: want true, got %q", fields[0])
	}
	if fields[1] != "42" {
		t.Errorf("int: want 42, got %q", fields[1])
	}
	if !strings.HasPrefix(fields[2], "3.5") {
		t.Errorf("float: want 3.5, got %q", fields[2])
	}
	if fields[3] != "hello" {
		t.Errorf("string: want hello, got %q", fields[3])
	}
	if fields[4] != "xyz" {
		t.Errorf("bytes: want xyz, got %q", fields[4])
	}
	if fields[5] != "" {
		t.Errorf("nil: want empty, got %q", fields[5])
	}
}

func TestCSVExporter_StructValues(t *testing.T) {
	var buf bytes.Buffer
	type row struct {
		Name string `item:"name"`
		Age  int    `item:"age"`
	}
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "age"}
	e := NewCSVExporter(&buf, opts)

	_ = e.StartExporting()
	if err := e.ExportItem(row{Name: "Alice", Age: 30}); err != nil {
		t.Fatalf("ExportItem struct: %v", err)
	}
	if err := e.ExportItem(&row{Name: "Bob", Age: 25}); err != nil {
		t.Fatalf("ExportItem *struct: %v", err)
	}
	_ = e.FinishExporting()

	out := buf.String()
	if !strings.Contains(out, "Alice,30") {
		t.Errorf("missing Alice row: %s", out)
	}
	if !strings.Contains(out, "Bob,25") {
		t.Errorf("missing Bob row: %s", out)
	}
}

// --- JSON Exporter struct 值 + 嵌套对象 ---

func TestJSONExporter_StructItem(t *testing.T) {
	var buf bytes.Buffer
	type row struct {
		Name string `item:"name"`
		Age  int    `item:"age"`
	}
	e := NewJSONExporter(&buf, DefaultExporterOptions())

	_ = e.StartExporting()
	_ = e.ExportItem(row{Name: "Alice", Age: 30})
	_ = e.FinishExporting()

	if !strings.Contains(buf.String(), `"name":"Alice"`) {
		t.Errorf("struct export missing name field: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"age":30`) {
		t.Errorf("struct export missing age field: %s", buf.String())
	}
}

// --- FeedSlot.Start + URI()  ---

func TestFeedSlot_StartExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "explicit.jsonl")

	slot, err := NewFeedSlot(FeedConfig{
		URI:       path,
		Format:    FormatJSONLines,
		Overwrite: true,
		Options:   DefaultExporterOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("NewFeedSlot: %v", err)
	}

	ctx := context.Background()
	if err := slot.Start(ctx, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 重复 Start 应幂等不报错
	if err := slot.Start(ctx, nil); err != nil {
		t.Fatalf("repeated Start: %v", err)
	}
	if slot.URI() != path {
		t.Errorf("URI: want %s, got %s", path, slot.URI())
	}
	_ = slot.Close(ctx, nil)
}

// --- extractItem 支持 reflect.Map[string]  ---

func TestExtractReflectMapItem(t *testing.T) {
	type stringer fmt.Stringer
	_ = stringer(nil)

	// 自定义 map 类型（不是 map[string]any）
	type customMap map[string]int
	m := customMap{"x": 10, "y": 20}

	names, get := extractItem(m)
	if len(names) != 2 {
		t.Errorf("want 2 names, got %v", names)
	}
	if v, ok := get("x"); !ok || v.(int) != 10 {
		t.Errorf("get(x) = %v, %v", v, ok)
	}
}

// --- CSV：更多 value 类型 ---

func TestCSVFormatValue_MoreTypes(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"f32", "i64", "strs", "anys", "custom"}
	e := NewCSVExporter(&buf, opts)

	type custom struct{ A int }
	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{
		"f32":    float32(1.5),
		"i64":    int64(100),
		"strs":   []string{"a", "b"},
		"anys":   []any{1, "x"},
		"custom": custom{A: 7},
	})
	_ = e.FinishExporting()

	out := buf.String()
	if !strings.Contains(out, "1.5") || !strings.Contains(out, "100") {
		t.Errorf("missing numeric values: %s", out)
	}
	// 默认 JoinMultivalued 是 ","，但由于已在 CSV 字段内，应被 csv writer quote
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Errorf("missing string slice values: %s", out)
	}
}

// --- JSONLines：struct item ---

func TestJSONLinesExporter_StructItem(t *testing.T) {
	var buf bytes.Buffer
	type row struct {
		X int `item:"x"`
		Y int `item:"y"`
	}
	e := NewJSONLinesExporter(&buf, DefaultExporterOptions())

	_ = e.StartExporting()
	_ = e.ExportItem(row{X: 1, Y: 2})
	_ = e.ExportItem(&row{X: 3, Y: 4})
	_ = e.FinishExporting()

	out := buf.String()
	if !strings.Contains(out, `"x":1`) || !strings.Contains(out, `"x":3`) {
		t.Errorf("jsonlines struct output missing fields: %s", out)
	}
}

// --- XML：字段名非法字符的兜底 ---

func TestXMLExporter_SanitizeFieldName(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"has space", "0starts-with-digit"}
	e := NewXMLExporter(&buf, opts)

	_ = e.StartExporting()
	err := e.ExportItem(map[string]any{
		"has space":          "v1",
		"0starts-with-digit": "v2",
	})
	if err != nil {
		t.Fatalf("ExportItem: %v", err)
	}
	_ = e.FinishExporting()

	if !strings.Contains(buf.String(), "v1") || !strings.Contains(buf.String(), "v2") {
		t.Errorf("expected sanitized field output to include values, got: %s", buf.String())
	}
}

// --- extractItem 处理 nil pointer / 非支持类型 ---

func TestExtractItem_NilPointer(t *testing.T) {
	var p *sampleStruct
	names, _ := extractItem(p)
	if names != nil {
		t.Errorf("nil pointer should produce nil names, got %v", names)
	}
}

func TestExtractItem_UnsupportedType(t *testing.T) {
	names, get := extractItem(42) // 非 map/struct
	if names != nil {
		t.Errorf("unsupported type should produce nil names, got %v", names)
	}
	if _, ok := get("any"); ok {
		t.Error("getter on unsupported item should always return false")
	}
}

func TestExtractItem_StringMap(t *testing.T) {
	m := map[string]string{"k": "v"}
	names, get := extractItem(m)
	if len(names) != 1 || names[0] != "k" {
		t.Errorf("want [k], got %v", names)
	}
	if v, ok := get("k"); !ok || v.(string) != "v" {
		t.Errorf("get(k) = %v, %v", v, ok)
	}
}

func TestExtractReflectMap_NonStringKey(t *testing.T) {
	type intMap map[int]string
	m := intMap{1: "a"}
	names, get := extractItem(m)
	if names != nil {
		t.Errorf("non-string-key map should yield nil names, got %v", names)
	}
	if _, ok := get("1"); ok {
		t.Error("non-string-key map getter should return false")
	}
}

// --- filterFields ---

func TestFilterFields(t *testing.T) {
	all := []string{"a", "b", "c"}
	// 未指定时返回全部
	got := filterFields(all, nil)
	if len(got) != 3 {
		t.Errorf("want all fields, got %v", got)
	}
	// 指定时按指定顺序
	got = filterFields(all, []string{"c", "a"})
	if len(got) != 2 || got[0] != "c" || got[1] != "a" {
		t.Errorf("want [c a], got %v", got)
	}
}

// --- JSON Tag 回退 ---

type jsonTagged struct {
	Alpha string `json:"alpha"`
	Beta  string `json:"beta,omitempty"`
}

func TestResolveStructFieldName_JSONTagFallback(t *testing.T) {
	v := jsonTagged{Alpha: "A", Beta: "B"}
	names, get := extractItem(v)

	want := map[string]string{"alpha": "A", "beta": "B"}
	found := map[string]string{}
	for _, n := range names {
		v, _ := get(n)
		if s, ok := v.(string); ok {
			found[n] = s
		}
	}
	for k, v := range want {
		if found[k] != v {
			t.Errorf("field %s: want %s, got %s", k, v, found[k])
		}
	}
}

// ============================================================================
// 错误路径与边界条件
// ============================================================================

// --- FileStorage 错误路径 ---

func TestFileStorage_InvalidURI(t *testing.T) {
	_, err := NewFileStorage("file://bad%uri", true)
	if err == nil {
		t.Error("expected parse error")
	}
}

func TestFileStorage_DoubleOpen(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStorage(filepath.Join(dir, "x.txt"), true)
	_, err := s.Open(context.Background(), nil)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := s.Open(context.Background(), nil); err == nil {
		t.Error("expected error on double Open")
	}
}

func TestFileStorage_StoreNil(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStorage(filepath.Join(dir, "x.txt"), true)
	// 关闭一个 nil writer 不应该报错
	if err := s.Store(context.Background(), nil); err != nil {
		t.Errorf("Store(nil): %v", err)
	}
}

func TestFileStorage_OpenBadDir(t *testing.T) {
	// 在一个已经存在的文件下再尝试创建子目录，会失败
	dir := t.TempDir()
	filePath := filepath.Join(dir, "blocker")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, _ := NewFileStorage(filepath.Join(filePath, "sub", "out.txt"), true)
	if _, err := s.Open(context.Background(), nil); err == nil {
		t.Error("expected mkdir error under a regular file")
	}
}

// --- fileURIToPath 错误分支 ---

func TestFileURIToPath_ParseError(t *testing.T) {
	_, err := fileURIToPath("file://%zz")
	if err == nil {
		t.Error("expected parse error for invalid percent-encoding")
	}
}

// --- StdoutStorage 的 Store(nil) 分支 ---

func TestStdoutStorage_StoreNil(t *testing.T) {
	s := NewStdoutStorage()
	if err := s.Store(context.Background(), nil); err != nil {
		t.Errorf("Store(nil): %v", err)
	}
}

// --- FeedSlot ExportItem 对已关闭 slot 的保护 ---

func TestFeedSlot_ExportAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "closed.jsonl")
	slot, _ := NewFeedSlot(FeedConfig{
		URI:       path,
		Format:    FormatJSONLines,
		Overwrite: true,
		Options:   DefaultExporterOptions(),
	}, nil)

	ctx := context.Background()
	_ = slot.ExportItem(ctx, nil, map[string]any{"a": 1})
	_ = slot.Close(ctx, nil)

	// 关闭后再 Export 应返回错误而非 panic
	if err := slot.ExportItem(ctx, nil, map[string]any{"a": 2}); err == nil {
		t.Error("expected error exporting after Close")
	}
	// 重复 Close 应幂等
	if err := slot.Close(ctx, nil); err != nil {
		t.Errorf("repeated Close: %v", err)
	}
}

// --- JSON Exporter 嵌套指针 + 深层 map ---

func TestJSONExporter_NestedStructures(t *testing.T) {
	var buf bytes.Buffer
	e := NewJSONExporter(&buf, DefaultExporterOptions())

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{
		"list": []any{1, 2, 3},
		"deep": map[string]any{"inner": map[string]any{"k": "v"}},
		"null": nil,
	})
	_ = e.FinishExporting()

	if !strings.Contains(buf.String(), `"inner"`) {
		t.Errorf("expected nested inner key, got: %s", buf.String())
	}
}

// ============================================================================
// 模拟 writer 错误覆盖错误返回分支
// ============================================================================

// failingWriter 在第 N 次 Write 后返回错误，用于测试错误处理路径
type failingWriter struct {
	calls    int
	failAt   int // 从 1 开始计数；0 表示永不失败
	failed   bool
	returned int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.failAt > 0 && w.calls >= w.failAt {
		w.failed = true
		return 0, fmt.Errorf("simulated write error (call %d)", w.calls)
	}
	w.returned += len(p)
	return len(p), nil
}

// --- JSON Exporter 各类错误分支 ---

func TestJSONExporter_StartError(t *testing.T) {
	fw := &failingWriter{failAt: 1}
	e := NewJSONExporter(fw, DefaultExporterOptions())
	if err := e.StartExporting(); err == nil {
		t.Error("expected StartExporting error due to writer")
	}
}

func TestJSONExporter_FinishWithoutStart(t *testing.T) {
	e := NewJSONExporter(&bytes.Buffer{}, DefaultExporterOptions())
	if err := e.FinishExporting(); err == nil {
		t.Error("expected error finishing without start")
	}
}

func TestJSONExporter_ExportWithoutStart(t *testing.T) {
	e := NewJSONExporter(&bytes.Buffer{}, DefaultExporterOptions())
	if err := e.ExportItem(map[string]any{"a": 1}); err == nil {
		t.Error("expected error exporting without start")
	}
}

func TestJSONExporter_DoubleFinish(t *testing.T) {
	e := NewJSONExporter(&bytes.Buffer{}, DefaultExporterOptions())
	_ = e.StartExporting()
	_ = e.FinishExporting()
	// 第二次 Finish 应无害（幂等）
	if err := e.FinishExporting(); err != nil {
		t.Errorf("repeated Finish: %v", err)
	}
}

// --- JSONLines Exporter 错误分支 ---

func TestJSONLinesExporter_DoubleStart(t *testing.T) {
	e := NewJSONLinesExporter(&bytes.Buffer{}, DefaultExporterOptions())
	_ = e.StartExporting()
	if err := e.StartExporting(); err == nil {
		t.Error("expected error on second Start")
	}
}

func TestJSONLinesExporter_FinishWithoutStart(t *testing.T) {
	e := NewJSONLinesExporter(&bytes.Buffer{}, DefaultExporterOptions())
	if err := e.FinishExporting(); err == nil {
		t.Error("expected error finishing without start")
	}
}

func TestJSONLinesExporter_WriteError(t *testing.T) {
	fw := &failingWriter{failAt: 1}
	e := NewJSONLinesExporter(fw, DefaultExporterOptions())
	_ = e.StartExporting()
	if err := e.ExportItem(map[string]any{"a": 1}); err == nil {
		t.Error("expected write error from failing writer")
	}
}

// --- CSV Exporter 错误分支 ---

func TestCSVExporter_DoubleStart(t *testing.T) {
	e := NewCSVExporter(&bytes.Buffer{}, DefaultExporterOptions())
	_ = e.StartExporting()
	if err := e.StartExporting(); err == nil {
		t.Error("expected error on second Start")
	}
}

func TestCSVExporter_ExportWithoutStart(t *testing.T) {
	e := NewCSVExporter(&bytes.Buffer{}, DefaultExporterOptions())
	if err := e.ExportItem(map[string]any{"a": 1}); err == nil {
		t.Error("expected error exporting without start")
	}
}

func TestCSVExporter_FinishWithoutStart(t *testing.T) {
	e := NewCSVExporter(&bytes.Buffer{}, DefaultExporterOptions())
	if err := e.FinishExporting(); err == nil {
		t.Error("expected error finishing without start")
	}
}

// 如果未指定 fields 且第一条 item 无字段可推断，CSV 会报错
func TestCSVExporter_NoFieldsNoItem(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions() // 未指定 fields
	e := NewCSVExporter(&buf, opts)

	_ = e.StartExporting()
	// 不提供任何 item 就 Finish
	_ = e.FinishExporting()

	if buf.Len() != 0 {
		// 无 item + 无 fields 时应当不写任何内容
		t.Errorf("no items, no fields: want empty output, got %q", buf.String())
	}
}

func TestCSVExporter_AutoFieldsFromFirstItem(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	// 未指定 FieldsToExport，应从第一条 Item 推断
	e := NewCSVExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"a": 1, "b": 2})
	_ = e.ExportItem(map[string]any{"a": 3, "b": 4})
	_ = e.FinishExporting()

	out := buf.String()
	// 第一行必须是 header（字段按字典序：a,b）
	if !strings.HasPrefix(out, "a,b\n") {
		t.Errorf("auto header: want 'a,b\\n', got %q", out)
	}
}

// --- XML Exporter 错误分支 ---

func TestXMLExporter_DoubleStart(t *testing.T) {
	e := NewXMLExporter(&bytes.Buffer{}, DefaultExporterOptions())
	_ = e.StartExporting()
	if err := e.StartExporting(); err == nil {
		t.Error("expected error on second Start")
	}
}

func TestXMLExporter_ExportWithoutStart(t *testing.T) {
	e := NewXMLExporter(&bytes.Buffer{}, DefaultExporterOptions())
	if err := e.ExportItem(map[string]any{"a": 1}); err == nil {
		t.Error("expected error exporting without start")
	}
}

func TestXMLExporter_FinishWithoutStart(t *testing.T) {
	e := NewXMLExporter(&bytes.Buffer{}, DefaultExporterOptions())
	if err := e.FinishExporting(); err == nil {
		t.Error("expected error finishing without start")
	}
}

func TestXMLExporter_ExportAfterFinish(t *testing.T) {
	e := NewXMLExporter(&bytes.Buffer{}, DefaultExporterOptions())
	_ = e.StartExporting()
	_ = e.FinishExporting()
	if err := e.ExportItem(map[string]any{"a": 1}); err == nil {
		t.Error("expected error exporting after finish")
	}
}

func TestXMLExporter_Indent(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.Indent = 2
	e := NewXMLExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"a": "1"})
	_ = e.FinishExporting()

	// 应当有换行和缩进
	if !strings.Contains(buf.String(), "\n") {
		t.Errorf("expected newline in indented output: %s", buf.String())
	}
}

func TestXMLExporter_HeaderWriteError(t *testing.T) {
	fw := &failingWriter{failAt: 1}
	e := NewXMLExporter(fw, DefaultExporterOptions())
	if err := e.StartExporting(); err == nil {
		t.Error("expected write error for failing writer")
	}
}