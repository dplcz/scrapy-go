package feedexport

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// ============================================================================
// JSON Exporter 测试
// ============================================================================

func TestJSONExporter_Empty(t *testing.T) {
	var buf bytes.Buffer
	e := NewJSONExporter(&buf, DefaultExporterOptions())

	if err := e.StartExporting(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := e.FinishExporting(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if got := buf.String(); got != "[]" {
		t.Errorf("empty export: want %q, got %q", "[]", got)
	}
}

func TestJSONExporter_MultipleItems(t *testing.T) {
	var buf bytes.Buffer
	e := NewJSONExporter(&buf, DefaultExporterOptions())

	items := []map[string]any{
		{"name": "Alice", "age": 30},
		{"name": "Bob", "age": 25},
	}

	if err := e.StartExporting(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for _, it := range items {
		if err := e.ExportItem(it); err != nil {
			t.Fatalf("ExportItem: %v", err)
		}
	}
	if err := e.FinishExporting(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid json output: %v\noutput: %s", err, buf.String())
	}
	if len(got) != len(items) {
		t.Fatalf("want %d items, got %d", len(items), len(got))
	}
	for i, exp := range items {
		if got[i]["name"] != exp["name"] {
			t.Errorf("item[%d].name = %v, want %v", i, got[i]["name"], exp["name"])
		}
	}
}

func TestJSONExporter_FieldOrder(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"c", "a", "b"}
	e := NewJSONExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"a": 1, "b": 2, "c": 3})
	_ = e.FinishExporting()

	want := `[{"c":3,"a":1,"b":2}]`
	if got := buf.String(); got != want {
		t.Errorf("field order: want %q, got %q", want, got)
	}
}

func TestJSONExporter_Indent(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.Indent = 2
	e := NewJSONExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"name": "A"})
	_ = e.ExportItem(map[string]any{"name": "B"})
	_ = e.FinishExporting()

	out := buf.String()
	if !strings.Contains(out, "\n") {
		t.Errorf("expected newlines in indented output, got: %q", out)
	}
	// 必须能被 JSON 解析
	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("indented output not valid JSON: %v\n%s", err, out)
	}
	if len(parsed) != 2 {
		t.Errorf("want 2 items, got %d", len(parsed))
	}
}

func TestJSONExporter_DoubleStart(t *testing.T) {
	e := NewJSONExporter(&bytes.Buffer{}, DefaultExporterOptions())
	if err := e.StartExporting(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := e.StartExporting(); err == nil {
		t.Fatal("expected error on second Start")
	}
}

// ============================================================================
// JSON Lines Exporter 测试
// ============================================================================

func TestJSONLinesExporter(t *testing.T) {
	var buf bytes.Buffer
	e := NewJSONLinesExporter(&buf, DefaultExporterOptions())

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"a": 1})
	_ = e.ExportItem(map[string]any{"b": 2})
	_ = e.FinishExporting()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d:\n%s", len(lines), buf.String())
	}
	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d invalid: %v", i, err)
		}
	}
}

func TestJSONLinesExporter_ExportBeforeStart(t *testing.T) {
	e := NewJSONLinesExporter(&bytes.Buffer{}, DefaultExporterOptions())
	if err := e.ExportItem(map[string]any{"a": 1}); err == nil {
		t.Fatal("expected error exporting before Start")
	}
}

// ============================================================================
// CSV Exporter 测试
// ============================================================================

func TestCSVExporter_WithHeader(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "age"}
	e := NewCSVExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"name": "Alice", "age": 30})
	_ = e.ExportItem(map[string]any{"name": "Bob", "age": 25})
	_ = e.FinishExporting()

	expected := "name,age\nAlice,30\nBob,25\n"
	if got := buf.String(); got != expected {
		t.Errorf("csv output:\nwant %q\ngot  %q", expected, got)
	}
}

func TestCSVExporter_NoHeader(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.IncludeHeadersLine = false
	opts.FieldsToExport = []string{"a"}
	e := NewCSVExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"a": 1})
	_ = e.FinishExporting()

	expected := "1\n"
	if got := buf.String(); got != expected {
		t.Errorf("want %q, got %q", expected, got)
	}
}

func TestCSVExporter_MultiValuedField(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"tags"}
	opts.JoinMultivalued = "|"
	e := NewCSVExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"tags": []string{"go", "scrapy"}})
	_ = e.FinishExporting()

	if !strings.Contains(buf.String(), "go|scrapy") {
		t.Errorf("expected joined multivalued 'go|scrapy' in output, got: %q", buf.String())
	}
}

func TestCSVExporter_MissingFieldOutputsEmpty(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"a", "b", "c"}
	e := NewCSVExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"a": 1, "c": 3}) // 缺 b
	_ = e.FinishExporting()

	// 第二行应该是 "1,,3"
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	if lines[1] != "1,,3" {
		t.Errorf("want '1,,3', got %q", lines[1])
	}
}

// ============================================================================
// XML Exporter 测试
// ============================================================================

func TestXMLExporter_Basic(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"name"}
	e := NewXMLExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"name": "Alice"})
	_ = e.ExportItem(map[string]any{"name": "Bob"})
	_ = e.FinishExporting()

	out := buf.String()
	if !strings.Contains(out, "<?xml") {
		t.Error("missing xml declaration")
	}
	if !strings.Contains(out, "<items>") || !strings.Contains(out, "</items>") {
		t.Error("missing root elements")
	}
	if !strings.Contains(out, "<item>") || !strings.Contains(out, "</item>") {
		t.Error("missing item elements")
	}
	if !strings.Contains(out, "<name>Alice</name>") {
		t.Errorf("missing Alice field in output: %s", out)
	}
	if !strings.Contains(out, "<name>Bob</name>") {
		t.Errorf("missing Bob field in output: %s", out)
	}
}

func TestXMLExporter_CustomElementNames(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.RootElement = "books"
	opts.ItemElement = "book"
	e := NewXMLExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(map[string]any{"title": "Go"})
	_ = e.FinishExporting()

	out := buf.String()
	if !strings.Contains(out, "<books>") || !strings.Contains(out, "</books>") {
		t.Errorf("missing custom root: %s", out)
	}
	if !strings.Contains(out, "<book>") || !strings.Contains(out, "</book>") {
		t.Errorf("missing custom item: %s", out)
	}
}

// ============================================================================
// 字段提取（反射）测试
// ============================================================================

type sampleStruct struct {
	Title  string `item:"title"`
	Price  int    `item:"price"`
	Tags   []string
	Hidden string `item:"-"`
	unexp  int
}

func TestExtractStructItem(t *testing.T) {
	_ = sampleStruct{unexp: 0} // 避免 unused
	obj := sampleStruct{Title: "Go", Price: 42, Tags: []string{"a", "b"}, Hidden: "x"}
	names, get := extractItem(obj)

	// 应该包含 title、price、Tags，不包含 Hidden 和 unexp
	want := map[string]bool{"title": true, "price": true, "Tags": true}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for n := range want {
		if !got[n] {
			t.Errorf("missing field %q in struct extraction; got=%v", n, names)
		}
	}
	if got["Hidden"] {
		t.Error("field tagged '-' should be skipped")
	}
	if got["unexp"] {
		t.Error("unexported field should be skipped")
	}

	if v, ok := get("title"); !ok || v != "Go" {
		t.Errorf("get(title) = %v, %v; want Go, true", v, ok)
	}
}

func TestExtractMapItem(t *testing.T) {
	names, get := extractItem(map[string]any{"b": 2, "a": 1})
	// 按字典序
	want := []string{"a", "b"}
	if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("names = %v, want %v", names, want)
	}
	if v, ok := get("a"); !ok || v != 1 {
		t.Errorf("get(a) = %v, %v", v, ok)
	}
}

func TestExtractItem_Pointer(t *testing.T) {
	obj := &sampleStruct{Title: "Go"}
	names, get := extractItem(obj)
	if len(names) == 0 {
		t.Fatal("expected non-empty fields from struct pointer")
	}
	if v, ok := get("title"); !ok || v != "Go" {
		t.Errorf("get(title) = %v, %v", v, ok)
	}
}

func TestExtractItem_Nil(t *testing.T) {
	names, get := extractItem(nil)
	if names != nil {
		t.Errorf("nil item should yield nil names, got %v", names)
	}
	if _, ok := get("any"); ok {
		t.Error("nil item getter should always return false")
	}
}

// ============================================================================
// NormalizeFormat 测试
// ============================================================================

func TestNormalizeFormat(t *testing.T) {
	cases := map[string]Format{
		"json":      FormatJSON,
		"jsonlines": FormatJSONLines,
		"jl":        FormatJSONLines,
		"jsonl":     FormatJSONLines,
		"csv":       FormatCSV,
		"xml":       FormatXML,
		"unknown":   Format("unknown"),
	}
	for input, want := range cases {
		if got := NormalizeFormat(input); got != want {
			t.Errorf("NormalizeFormat(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNewExporter_Unknown(t *testing.T) {
	_, err := NewExporter("unknown", &bytes.Buffer{}, DefaultExporterOptions())
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestRegisterExporter(t *testing.T) {
	called := false
	RegisterExporter("customtest", func(w io.Writer, opts ExporterOptions) ItemExporter {
		called = true
		return NewJSONLinesExporter(w, opts)
	})

	e, err := NewExporter("customtest", &bytes.Buffer{}, DefaultExporterOptions())
	if err != nil {
		t.Fatalf("NewExporter: %v", err)
	}
	if !called {
		t.Error("expected custom factory to be called")
	}
	if e == nil {
		t.Error("expected non-nil exporter")
	}
}
