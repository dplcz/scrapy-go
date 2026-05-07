package feedexport

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dplcz/scrapy-go/pkg/item"
)

// ============================================================================
// ItemAdapter 集成测试
//
// 验证 feedexport 的所有 Exporter 能通过 pkg/item.ItemAdapter 统一导出：
//   - map[string]any
//   - struct / *struct
//   - 自实现 item.ItemAdapter 接口的类型
// ============================================================================

type adapterBook struct {
	Title  string `item:"title"`
	Price  int    `item:"price"`
	Author string `json:"author"`
}

// protoItem 是一个自实现 ItemAdapter 接口的示例类型。
type protoItem struct {
	m map[string]any
}

func (p *protoItem) Item() any                         { return p }
func (p *protoItem) FieldNames() []string              { return []string{"x", "y"} }
func (p *protoItem) GetField(name string) (any, bool)  { v, ok := p.m[name]; return v, ok }
func (p *protoItem) SetField(name string, v any) error { p.m[name] = v; return nil }
func (p *protoItem) HasField(name string) bool         { _, ok := p.m[name]; return ok }
func (p *protoItem) AsMap() map[string]any {
	out := make(map[string]any, len(p.m))
	for k, v := range p.m {
		out[k] = v
	}
	return out
}
func (p *protoItem) Len() int                        { return len(p.m) }
func (p *protoItem) FieldMeta(string) item.FieldMeta { return nil }

// ----------------------------------------------------------------------------
// JSON Lines：struct、map、自实现接口 三种类型均能导出
// ----------------------------------------------------------------------------

func TestJSONLines_ItemAdapter_Struct(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONLinesExporter(&buf, ExporterOptions{})
	if err := exp.StartExporting(); err != nil {
		t.Fatal(err)
	}
	if err := exp.ExportItem(&adapterBook{Title: "T", Price: 10, Author: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := exp.FinishExporting(); err != nil {
		t.Fatal(err)
	}

	line := strings.TrimSpace(buf.String())
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("json parse: %v  line=%q", err, line)
	}
	if got["title"] != "T" || int(got["price"].(float64)) != 10 || got["author"] != "A" {
		t.Fatalf("unexpected fields: %v", got)
	}
}

func TestJSONLines_ItemAdapter_Map(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONLinesExporter(&buf, ExporterOptions{})
	_ = exp.StartExporting()
	if err := exp.ExportItem(map[string]any{"a": 1, "b": "x"}); err != nil {
		t.Fatal(err)
	}
	_ = exp.FinishExporting()

	if !strings.Contains(buf.String(), `"a":1`) || !strings.Contains(buf.String(), `"b":"x"`) {
		t.Fatalf("missing fields: %s", buf.String())
	}
}

func TestJSONLines_ItemAdapter_CustomInterface(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONLinesExporter(&buf, ExporterOptions{})
	_ = exp.StartExporting()
	it := &protoItem{m: map[string]any{"x": 1, "y": "hello"}}
	if err := exp.ExportItem(it); err != nil {
		t.Fatal(err)
	}
	_ = exp.FinishExporting()

	// 自定义 Adapter 的 FieldNames 顺序是 [x,y]，确保输出包含
	if !strings.Contains(buf.String(), `"x":1`) || !strings.Contains(buf.String(), `"y":"hello"`) {
		t.Fatalf("custom adapter output = %s", buf.String())
	}
}

// ----------------------------------------------------------------------------
// CSV：struct 与 map 使用同一套导出逻辑，共享 ItemAdapter
// ----------------------------------------------------------------------------

func TestCSV_ItemAdapter_MixedTypes(t *testing.T) {
	var buf bytes.Buffer
	exp := NewCSVExporter(&buf, ExporterOptions{
		IncludeHeadersLine: true,
		FieldsToExport:     []string{"title", "price"},
	})
	if err := exp.StartExporting(); err != nil {
		t.Fatal(err)
	}
	// 第一条是 struct
	if err := exp.ExportItem(&adapterBook{Title: "T1", Price: 10}); err != nil {
		t.Fatal(err)
	}
	// 第二条是 map，字段名对齐
	if err := exp.ExportItem(map[string]any{"title": "T2", "price": 20}); err != nil {
		t.Fatal(err)
	}
	if err := exp.FinishExporting(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	wantLines := []string{"title,price", "T1,10", "T2,20"}
	for _, line := range wantLines {
		if !strings.Contains(out, line) {
			t.Errorf("missing line %q in:\n%s", line, out)
		}
	}
}

// ----------------------------------------------------------------------------
// JSON / XML：Indent 模式补充测试，提升覆盖率
// ----------------------------------------------------------------------------

func TestJSON_IndentMode(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONExporter(&buf, ExporterOptions{Indent: 2})
	_ = exp.StartExporting()
	_ = exp.ExportItem(map[string]any{"a": 1})
	_ = exp.ExportItem(map[string]any{"b": 2})
	_ = exp.FinishExporting()

	out := buf.String()
	if !strings.HasPrefix(out, "[\n") || !strings.HasSuffix(strings.TrimRight(out, "\n"), "]") {
		t.Fatalf("indent mode output = %q", out)
	}
	if !strings.Contains(out, "  ") { // 至少有缩进空格
		t.Fatalf("should contain indent spaces: %q", out)
	}
}

func TestXML_NestedMapAndSlice(t *testing.T) {
	var buf bytes.Buffer
	exp := NewXMLExporter(&buf, ExporterOptions{Indent: 2})
	_ = exp.StartExporting()
	it := map[string]any{
		"tags": []string{"a", "b"},
		"meta": map[string]any{"k": "v"},
		"num":  42,
	}
	if err := exp.ExportItem(it); err != nil {
		t.Fatal(err)
	}
	_ = exp.FinishExporting()

	out := buf.String()
	wants := []string{"<tags>", "<value>a</value>", "<value>b</value>",
		"<meta>", "<k>v</k>", "<num>42</num>"}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
}

// ----------------------------------------------------------------------------
// JSON：extractItem 空字段的分支（空 map、空 struct）
// ----------------------------------------------------------------------------

func TestJSON_EmptyItem(t *testing.T) {
	var buf bytes.Buffer
	exp := NewJSONExporter(&buf, ExporterOptions{})
	_ = exp.StartExporting()
	// 空 map
	if err := exp.ExportItem(map[string]any{}); err != nil {
		t.Fatal(err)
	}
	_ = exp.FinishExporting()

	if !strings.Contains(buf.String(), "{}") {
		t.Fatalf("empty map should produce {}: %s", buf.String())
	}
}

// ----------------------------------------------------------------------------
// CSV：FieldsToExport 指定后首个 Item 字段被严格遵循
// ----------------------------------------------------------------------------

func TestCSV_FieldsToExport_MissingField(t *testing.T) {
	var buf bytes.Buffer
	exp := NewCSVExporter(&buf, ExporterOptions{
		FieldsToExport:     []string{"a", "b", "c"},
		IncludeHeadersLine: true,
	})
	_ = exp.StartExporting()
	// 只提供 a、b，c 应为空
	_ = exp.ExportItem(map[string]any{"a": 1, "b": 2})
	_ = exp.FinishExporting()

	out := buf.String()
	if !strings.Contains(out, "a,b,c\n") {
		t.Fatalf("header wrong: %s", out)
	}
	if !strings.Contains(out, "1,2,\n") {
		t.Fatalf("missing field should be empty: %s", out)
	}
}
