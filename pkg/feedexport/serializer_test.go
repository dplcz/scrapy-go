package feedexport

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dplcz/scrapy-go/pkg/item"
)

// ============================================================================
// SerializerRegistry 测试
// ============================================================================

func TestRegisterSerializer(t *testing.T) {
	defer ClearSerializers()

	RegisterSerializer("to_upper", func(v any) any {
		if s, ok := v.(string); ok {
			return strings.ToUpper(s)
		}
		return v
	})

	fn, ok := LookupSerializer("to_upper")
	if !ok {
		t.Fatal("expected to_upper serializer to be registered")
	}
	if fn == nil {
		t.Fatal("expected non-nil serializer function")
	}

	result := fn("hello")
	if result != "HELLO" {
		t.Errorf("expected HELLO, got %v", result)
	}
}

func TestLookupSerializer_NotFound(t *testing.T) {
	defer ClearSerializers()

	fn, ok := LookupSerializer("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent serializer")
	}
	if fn != nil {
		t.Error("expected nil function for nonexistent serializer")
	}
}

func TestRegisterSerializer_Override(t *testing.T) {
	defer ClearSerializers()

	RegisterSerializer("test", func(v any) any { return "first" })
	RegisterSerializer("test", func(v any) any { return "second" })

	fn, ok := LookupSerializer("test")
	if !ok {
		t.Fatal("expected serializer to be registered")
	}
	if fn(nil) != "second" {
		t.Error("expected override to take effect")
	}
}

func TestRegisterSerializer_NilAndEmpty(t *testing.T) {
	defer ClearSerializers()

	// 空名称不注册
	RegisterSerializer("", func(v any) any { return v })
	if _, ok := LookupSerializer(""); ok {
		t.Error("empty name should not be registered")
	}

	// nil 函数不注册
	RegisterSerializer("test", nil)
	if _, ok := LookupSerializer("test"); ok {
		t.Error("nil function should not be registered")
	}
}

func TestClearSerializers(t *testing.T) {
	RegisterSerializer("temp", func(v any) any { return v })
	ClearSerializers()
	if _, ok := LookupSerializer("temp"); ok {
		t.Error("expected serializer to be cleared")
	}
}

// ============================================================================
// SerializeField 测试
// ============================================================================

func TestSerializeField_NilMeta(t *testing.T) {
	result := SerializeField(nil, "name", "hello")
	if result != "hello" {
		t.Errorf("expected hello, got %v", result)
	}
}

func TestSerializeField_NoSerializerKey(t *testing.T) {
	meta := item.FieldMeta{"other": "value"}
	result := SerializeField(meta, "name", "hello")
	if result != "hello" {
		t.Errorf("expected hello, got %v", result)
	}
}

func TestSerializeField_SerializerNotRegistered(t *testing.T) {
	defer ClearSerializers()

	meta := item.FieldMeta{"serializer": "unknown_serializer"}
	result := SerializeField(meta, "name", "hello")
	if result != "hello" {
		t.Errorf("expected hello (fallback), got %v", result)
	}
}

func TestSerializeField_SerializerRegistered(t *testing.T) {
	defer ClearSerializers()

	RegisterSerializer("to_int", func(v any) any {
		if f, ok := v.(float64); ok {
			return int(f)
		}
		return v
	})

	meta := item.FieldMeta{"serializer": "to_int"}
	result := SerializeField(meta, "price", 9.99)
	if result != 9 {
		t.Errorf("expected 9, got %v", result)
	}
}

// ============================================================================
// Struct + FieldMeta 端到端序列化测试
// ============================================================================

type productItem struct {
	Name  string  `item:"name"`
	Price float64 `item:"price,serializer=to_int"`
	Tags  string  `item:"tags,serializer=to_upper"`
}

func TestSerializeItemFields_StructWithSerializer(t *testing.T) {
	defer ClearSerializers()

	RegisterSerializer("to_int", func(v any) any {
		if f, ok := v.(float64); ok {
			return int(f)
		}
		return v
	})
	RegisterSerializer("to_upper", func(v any) any {
		if s, ok := v.(string); ok {
			return strings.ToUpper(s)
		}
		return v
	})

	p := &productItem{Name: "Widget", Price: 9.99, Tags: "electronics"}
	fields, getField := serializeItemFields(p, nil)

	// 验证字段名
	expectedFields := []string{"name", "price", "tags"}
	if len(fields) != len(expectedFields) {
		t.Fatalf("expected %d fields, got %d: %v", len(expectedFields), len(fields), fields)
	}
	for i, exp := range expectedFields {
		if fields[i] != exp {
			t.Errorf("field[%d] = %q, want %q", i, fields[i], exp)
		}
	}

	// name 无 serializer，原样返回
	if v, ok := getField("name"); !ok || v != "Widget" {
		t.Errorf("name = %v, %v; want Widget, true", v, ok)
	}

	// price 有 serializer=to_int，应转为 int
	if v, ok := getField("price"); !ok {
		t.Error("price not found")
	} else if intVal, ok := v.(int); !ok || intVal != 9 {
		t.Errorf("price = %v (%T), want 9 (int)", v, v)
	}

	// tags 有 serializer=to_upper，应转为大写
	if v, ok := getField("tags"); !ok || v != "ELECTRONICS" {
		t.Errorf("tags = %v, want ELECTRONICS", v)
	}
}

func TestSerializeItemFields_MapNoSerializer(t *testing.T) {
	defer ClearSerializers()

	m := map[string]any{"a": 1, "b": "hello"}
	fields, getField := serializeItemFields(m, nil)

	// map 没有 FieldMeta，所有值原样返回
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if v, ok := getField("a"); !ok || v != 1 {
		t.Errorf("a = %v, %v", v, ok)
	}
	if v, ok := getField("b"); !ok || v != "hello" {
		t.Errorf("b = %v, %v", v, ok)
	}
}

func TestSerializeItemFields_FieldsToExport(t *testing.T) {
	defer ClearSerializers()

	RegisterSerializer("to_int", func(v any) any {
		if f, ok := v.(float64); ok {
			return int(f)
		}
		return v
	})

	p := &productItem{Name: "Widget", Price: 9.99, Tags: "electronics"}
	fields, getField := serializeItemFields(p, []string{"price", "name"})

	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d: %v", len(fields), fields)
	}
	if fields[0] != "price" || fields[1] != "name" {
		t.Errorf("fields = %v, want [price, name]", fields)
	}

	// price 仍应被序列化
	if v, _ := getField("price"); v != 9 {
		t.Errorf("price = %v, want 9", v)
	}
}

// ============================================================================
// Exporter 集成测试：验证 serialize_field 钩子在各 Exporter 中生效
// ============================================================================

func TestJSONExporter_WithSerializer(t *testing.T) {
	defer ClearSerializers()

	RegisterSerializer("to_int", func(v any) any {
		if f, ok := v.(float64); ok {
			return int(f)
		}
		return v
	})

	var buf bytes.Buffer
	e := NewJSONExporter(&buf, DefaultExporterOptions())

	_ = e.StartExporting()
	_ = e.ExportItem(&productItem{Name: "Widget", Price: 9.99, Tags: "go"})
	_ = e.FinishExporting()

	var items []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &items); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	// price 应该是整数（JSON 中为 float64，但值应为 9）
	price := items[0]["price"]
	if p, ok := price.(float64); !ok || p != 9 {
		t.Errorf("price = %v (%T), want 9", price, price)
	}
}

func TestJSONLinesExporter_WithSerializer(t *testing.T) {
	defer ClearSerializers()

	RegisterSerializer("to_int", func(v any) any {
		if f, ok := v.(float64); ok {
			return int(f)
		}
		return v
	})

	var buf bytes.Buffer
	e := NewJSONLinesExporter(&buf, DefaultExporterOptions())

	_ = e.StartExporting()
	_ = e.ExportItem(&productItem{Name: "Widget", Price: 9.99, Tags: "go"})
	_ = e.FinishExporting()

	var obj map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &obj); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}

	price := obj["price"]
	if p, ok := price.(float64); !ok || p != 9 {
		t.Errorf("price = %v (%T), want 9", price, price)
	}
}

func TestCSVExporter_WithSerializer(t *testing.T) {
	defer ClearSerializers()

	RegisterSerializer("to_int", func(v any) any {
		if f, ok := v.(float64); ok {
			return int(f)
		}
		return v
	})
	RegisterSerializer("to_upper", func(v any) any {
		if s, ok := v.(string); ok {
			return strings.ToUpper(s)
		}
		return v
	})

	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "price", "tags"}
	e := NewCSVExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(&productItem{Name: "Widget", Price: 9.99, Tags: "electronics"})
	_ = e.FinishExporting()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + data), got %d:\n%s", len(lines), buf.String())
	}

	// 数据行应包含序列化后的值
	dataLine := lines[1]
	// price 应为 "9"（整数），tags 应为 "ELECTRONICS"
	if !strings.Contains(dataLine, "9") {
		t.Errorf("expected price=9 in CSV data, got: %s", dataLine)
	}
	if !strings.Contains(dataLine, "ELECTRONICS") {
		t.Errorf("expected tags=ELECTRONICS in CSV data, got: %s", dataLine)
	}
}

func TestXMLExporter_WithSerializer(t *testing.T) {
	defer ClearSerializers()

	RegisterSerializer("to_int", func(v any) any {
		if f, ok := v.(float64); ok {
			return int(f)
		}
		return v
	})

	var buf bytes.Buffer
	opts := DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "price"}
	e := NewXMLExporter(&buf, opts)

	_ = e.StartExporting()
	_ = e.ExportItem(&productItem{Name: "Widget", Price: 9.99})
	_ = e.FinishExporting()

	out := buf.String()
	// price 应为 "9"（整数的字符串表示）
	if !strings.Contains(out, "<price>9</price>") {
		t.Errorf("expected <price>9</price> in XML output, got:\n%s", out)
	}
}
