package item_test

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/dplcz/scrapy-go/pkg/item"
)

// ============================================================================
// 测试用结构体
// ============================================================================

type book struct {
	Title  string `item:"title"`
	Price  int    `item:"price,required"`
	Author string `json:"author"`
	Hidden string `item:"-"`
	Extra  string // 无 tag，使用字段名
	priv   string //nolint:unused // 非导出，应该被忽略
}

// customItem 实现了 ItemAdapter 接口，验证接口分发。
type customItem struct {
	data map[string]any
}

func (c *customItem) Item() any                         { return c }
func (c *customItem) FieldNames() []string              { return []string{"custom"} }
func (c *customItem) GetField(_ string) (any, bool)     { return "x", true }
func (c *customItem) SetField(_ string, _ any) error    { return nil }
func (c *customItem) HasField(_ string) bool            { return true }
func (c *customItem) AsMap() map[string]any             { return c.data }
func (c *customItem) Len() int                          { return 1 }
func (c *customItem) FieldMeta(_ string) item.FieldMeta { return nil }

// ============================================================================
// Adapt / IsItem
// ============================================================================

func TestIsItem(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"nil", nil, false},
		{"map", map[string]any{"a": 1}, true},
		{"map_string", map[string]string{"a": "b"}, true},
		{"struct_value", book{Title: "T"}, true},
		{"struct_ptr", &book{Title: "T"}, true},
		{"interface", &customItem{}, true},
		{"string", "hello", false},
		{"int", 42, false},
		{"slice", []int{1, 2}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := item.IsItem(c.in); got != c.want {
				t.Fatalf("IsItem(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestAdapt_Nil(t *testing.T) {
	if a := item.Adapt(nil); a != nil {
		t.Fatalf("Adapt(nil) should return nil, got %T", a)
	}
	if m := item.AsMap(nil); m != nil {
		t.Fatalf("AsMap(nil) should return nil, got %v", m)
	}
	if names := item.FieldNames(nil); names != nil {
		t.Fatalf("FieldNames(nil) should return nil, got %v", names)
	}
}

func TestAdapt_Interface(t *testing.T) {
	c := &customItem{data: map[string]any{"a": 1}}
	a := item.Adapt(c)
	if a == nil {
		t.Fatal("Adapt should return non-nil")
	}
	if a.(interface{ FieldNames() []string }) == nil {
		t.Fatal("should be ItemAdapter")
	}
	if got := a.FieldNames(); !reflect.DeepEqual(got, []string{"custom"}) {
		t.Fatalf("custom FieldNames = %v", got)
	}
}

func TestMustAdapt_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustAdapt should panic on unsupported type")
		}
	}()
	item.MustAdapt(42)
}

// ============================================================================
// MapAdapter
// ============================================================================

func TestMapAdapter_Basic(t *testing.T) {
	m := map[string]any{"a": 1, "b": "hello", "c": true}
	a := item.Adapt(m)
	if a == nil {
		t.Fatal("adapt map failed")
	}
	if _, ok := a.(*item.MapAdapter); !ok {
		t.Fatalf("expected *MapAdapter, got %T", a)
	}

	// FieldNames 按字典序
	names := a.FieldNames()
	expected := []string{"a", "b", "c"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("FieldNames = %v, want %v", names, expected)
	}

	// GetField
	v, ok := a.GetField("a")
	if !ok || v.(int) != 1 {
		t.Fatalf("GetField a = %v, ok=%v", v, ok)
	}
	if _, ok := a.GetField("missing"); ok {
		t.Fatal("GetField missing should ok=false")
	}

	// HasField
	if !a.HasField("b") {
		t.Fatal("HasField b should true")
	}
	if a.HasField("missing") {
		t.Fatal("HasField missing should false")
	}

	// Len
	if got := a.Len(); got != 3 {
		t.Fatalf("Len = %d, want 3", got)
	}

	// SetField
	if err := a.SetField("d", 3.14); err != nil {
		t.Fatalf("SetField failed: %v", err)
	}
	if v, _ := a.GetField("d"); v.(float64) != 3.14 {
		t.Fatalf("SetField not applied")
	}

	// AsMap 浅拷贝：修改快照不影响原数据
	snap := a.AsMap()
	snap["a"] = 999
	if v, _ := a.GetField("a"); v.(int) != 1 {
		t.Fatal("AsMap should return shallow copy (top-level)")
	}

	// Item
	if a.Item() == nil {
		t.Fatal("Item returned nil")
	}

	// FieldMeta 对 map 永远 nil
	if a.FieldMeta("a") != nil {
		t.Fatal("FieldMeta for map should be nil")
	}
}

func TestMapAdapter_StringMap(t *testing.T) {
	m := map[string]string{"a": "1", "b": "2"}
	a := item.Adapt(m)
	if a == nil {
		t.Fatal("adapt map[string]string failed")
	}
	if v, ok := a.GetField("a"); !ok || v.(string) != "1" {
		t.Fatalf("GetField a = %v ok=%v", v, ok)
	}
	// 设置兼容类型
	if err := a.SetField("c", "3"); err != nil {
		t.Fatalf("SetField failed: %v", err)
	}
	if v, _ := a.GetField("c"); v.(string) != "3" {
		t.Fatal("SetField value mismatch")
	}
	// 设置 nil → 元素类型零值
	if err := a.SetField("d", nil); err != nil {
		t.Fatalf("SetField nil failed: %v", err)
	}
	if v, _ := a.GetField("d"); v.(string) != "" {
		t.Fatalf("SetField nil should zero-value, got %q", v)
	}
	// 设置不兼容类型 → struct 类型 无法赋值/转换给 string
	if err := a.SetField("e", struct{}{}); err == nil {
		t.Fatal("SetField struct{} to string map should fail")
	}
}

func TestMapAdapter_NilMap(t *testing.T) {
	// 显式传入 nil map，应当创建新的 map
	var m map[string]any
	a := item.Adapt(m)
	if a == nil {
		t.Fatal("adapt nil map[string]any failed")
	}
	if err := a.SetField("x", 1); err != nil {
		t.Fatalf("SetField on nil map failed: %v", err)
	}
}

func TestMapAdapter_GenericMap(t *testing.T) {
	// 非内置快速路径类型 map[string]int
	m := map[string]int{"a": 1, "b": 2}
	a := item.Adapt(m)
	if a == nil {
		t.Fatal("adapt map[string]int failed")
	}
	names := a.FieldNames()
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Fatalf("FieldNames = %v", names)
	}
	if v, _ := a.GetField("a"); v.(int) != 1 {
		t.Fatal("GetField int map")
	}
	if err := a.SetField("c", 3); err != nil {
		t.Fatalf("SetField int map: %v", err)
	}
}

// ============================================================================
// StructAdapter
// ============================================================================

func TestStructAdapter_ReadOnly(t *testing.T) {
	b := book{Title: "T", Price: 10, Author: "A"}
	a := item.Adapt(b)
	if a == nil {
		t.Fatal("adapt struct failed")
	}
	if _, ok := a.(*item.StructAdapter); !ok {
		t.Fatalf("expected *StructAdapter, got %T", a)
	}

	names := a.FieldNames()
	// 注意：Hidden 通过 item:"-" 被跳过；priv 是非导出字段跳过
	expected := []string{"title", "price", "author", "Extra"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("FieldNames = %v, want %v", names, expected)
	}

	if v, _ := a.GetField("title"); v.(string) != "T" {
		t.Fatalf("title = %v", v)
	}
	if v, _ := a.GetField("price"); v.(int) != 10 {
		t.Fatalf("price = %v", v)
	}
	if !a.HasField("author") {
		t.Fatal("HasField author should true")
	}

	// 非指针传入 → 只读
	if err := a.SetField("title", "X"); !errors.Is(err, item.ErrFieldReadOnly) {
		t.Fatalf("SetField on struct value should return ErrFieldReadOnly, got %v", err)
	}

	if got := a.Len(); got != 4 {
		t.Fatalf("Len = %d, want 4", got)
	}

	meta := a.FieldMeta("price")
	if meta == nil {
		t.Fatal("FieldMeta price should not be nil (has required)")
	}
	if v, _ := meta.Get("required"); v != true {
		t.Fatalf("meta required = %v", v)
	}
}

func TestStructAdapter_Writable(t *testing.T) {
	b := &book{Title: "T"}
	a := item.Adapt(b)
	if a == nil {
		t.Fatal("adapt ptr struct failed")
	}
	if err := a.SetField("title", "NewTitle"); err != nil {
		t.Fatalf("SetField: %v", err)
	}
	if b.Title != "NewTitle" {
		t.Fatalf("Title not updated: %s", b.Title)
	}
	// 不存在的字段
	if err := a.SetField("nope", 1); !errors.Is(err, item.ErrFieldNotFound) {
		t.Fatalf("SetField missing should return ErrFieldNotFound, got %v", err)
	}
	// nil → 零值
	if err := a.SetField("title", nil); err != nil {
		t.Fatalf("SetField nil: %v", err)
	}
	if b.Title != "" {
		t.Fatalf("Title should be zero after nil set, got %q", b.Title)
	}
	// 类型转换（int → int）
	if err := a.SetField("price", int64(100)); err != nil {
		t.Fatalf("SetField convertible: %v", err)
	}
	if b.Price != 100 {
		t.Fatalf("Price not set via convert: %d", b.Price)
	}
	// 不可转换类型
	if err := a.SetField("price", "abc"); !errors.Is(err, item.ErrFieldReadOnly) {
		t.Fatalf("SetField incompatible should fail, got %v", err)
	}
}

func TestStructAdapter_AsMap(t *testing.T) {
	b := &book{Title: "T", Price: 10, Author: "A", Extra: "E"}
	a := item.Adapt(b)
	m := a.AsMap()
	if m["title"] != "T" || m["price"] != 10 || m["author"] != "A" || m["Extra"] != "E" {
		t.Fatalf("AsMap = %v", m)
	}
	if _, ok := m["Hidden"]; ok {
		t.Fatal("Hidden should be excluded")
	}
}

func TestStructAdapter_NilPtr(t *testing.T) {
	var b *book
	a := item.Adapt(b)
	if a != nil {
		t.Fatal("Adapt nil pointer should return nil")
	}
}

// ============================================================================
// Register / 自定义工厂
// ============================================================================

type weirdItem struct {
	V string
}

func TestAdapt_CustomFactory(t *testing.T) {
	t.Cleanup(item.ClearRegistered)

	// 为 weirdItem 注册一个自定义工厂——包装成只含 "v" 字段的 map adapter。
	item.Register(func(it any) item.ItemAdapter {
		w, ok := it.(weirdItem)
		if !ok {
			return nil
		}
		return item.NewMapAdapter(map[string]any{"v": w.V})
	})

	a := item.Adapt(weirdItem{V: "hi"})
	if a == nil {
		t.Fatal("custom factory should handle weirdItem")
	}
	if v, _ := a.GetField("v"); v != "hi" {
		t.Fatalf("custom adapter = %v", v)
	}
}

// ============================================================================
// FieldMeta 辅助方法
// ============================================================================

func TestFieldMeta_GetString(t *testing.T) {
	m := item.FieldMeta{"serializer": "strip", "required": true}
	if s := m.GetString("serializer"); s != "strip" {
		t.Fatalf("GetString = %q", s)
	}
	if s := m.GetString("required"); s != "" {
		t.Fatalf("GetString required should be empty (not string)")
	}
	if s := m.GetString("missing"); s != "" {
		t.Fatalf("GetString missing = %q", s)
	}
	if _, ok := item.FieldMeta(nil).Get("x"); ok {
		t.Fatal("nil FieldMeta.Get should return false")
	}
	cl := m.Clone()
	cl["extra"] = 1
	if _, ok := m["extra"]; ok {
		t.Fatal("Clone should not share underlying map")
	}
	if item.FieldMeta(nil).Clone() != nil {
		t.Fatal("nil FieldMeta.Clone should be nil")
	}
}

// ============================================================================
// 边界与覆盖补充
// ============================================================================

type jsonTagged struct {
	Name string `json:"name,omitempty,string"`
	Age  int    `json:"-"`
	Raw  string // 无 tag
}

type itemOnly struct {
	Price int `item:"price,serializer=to_int,required"`
}

func TestStructAdapter_TagResolution(t *testing.T) {
	a := item.Adapt(&jsonTagged{Name: "N", Age: 30, Raw: "R"})
	names := a.FieldNames()
	// json tag 被用作字段名；json:"-" 被跳过；无 tag 使用字段名
	expected := []string{"name", "Raw"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("FieldNames = %v, want %v", names, expected)
	}
	// json tag 的其余 token 进入 meta 以 json_<token>=true
	meta := a.FieldMeta("name")
	if meta == nil {
		t.Fatal("FieldMeta name should not be nil")
	}
	if v, _ := meta.Get("json_omitempty"); v != true {
		t.Fatalf("meta.json_omitempty = %v", v)
	}
	if v, _ := meta.Get("json_string"); v != true {
		t.Fatalf("meta.json_string = %v", v)
	}
	// 无 tag 字段 FieldMeta 为 nil
	if m := a.FieldMeta("Raw"); m != nil {
		t.Fatalf("FieldMeta Raw should be nil, got %v", m)
	}
	// 不存在的字段
	if m := a.FieldMeta("missing"); m != nil {
		t.Fatal("FieldMeta missing should be nil")
	}
}

func TestStructAdapter_ItemTagMeta(t *testing.T) {
	a := item.Adapt(&itemOnly{Price: 10})
	meta := a.FieldMeta("price")
	if meta == nil {
		t.Fatal("FieldMeta price should not be nil")
	}
	if v, _ := meta.Get("serializer"); v != "to_int" {
		t.Fatalf("serializer = %v", v)
	}
	if v, _ := meta.Get("required"); v != true {
		t.Fatalf("required = %v", v)
	}
}

func TestStructAdapter_Item(t *testing.T) {
	b := &book{Title: "T"}
	a := item.Adapt(b)
	if a.Item() != b {
		t.Fatal("Item() should return original pointer")
	}
}

func TestStructAdapter_GetFieldMissing(t *testing.T) {
	b := book{Title: "T"}
	a := item.Adapt(b)
	if _, ok := a.GetField("nope"); ok {
		t.Fatal("GetField missing should ok=false")
	}
}

func TestMapAdapter_GetFieldMissing_Reflect(t *testing.T) {
	m := map[string]int{"a": 1}
	a := item.Adapt(m)
	if _, ok := a.GetField("nope"); ok {
		t.Fatal("GetField missing should ok=false")
	}
	if a.HasField("nope") {
		t.Fatal("HasField missing should false")
	}
	if a.Len() != 1 {
		t.Fatalf("Len = %d", a.Len())
	}
	snap := a.AsMap()
	if snap["a"] != 1 {
		t.Fatalf("AsMap = %v", snap)
	}
}

func TestAdapt_UnsupportedType(t *testing.T) {
	// int / string / slice / 等不支持
	if a := item.Adapt(42); a != nil {
		t.Fatalf("Adapt(int) = %v, want nil", a)
	}
	if a := item.Adapt("s"); a != nil {
		t.Fatalf("Adapt(string) = %v, want nil", a)
	}
	if a := item.Adapt([]int{1}); a != nil {
		t.Fatalf("Adapt(slice) = %v, want nil", a)
	}
	// 非字符串 key 的 map
	if a := item.Adapt(map[int]any{1: "a"}); a != nil {
		t.Fatal("Adapt(map[int]any) should return nil")
	}
	// NewMapAdapter 对 nil 指针
	var m map[string]int
	// nil map via reflect path: NewMapAdapter should return nil
	if a := item.NewMapAdapter(m); a != nil {
		t.Fatal("NewMapAdapter(nil reflect map) should return nil")
	}
	// NewMapAdapter(nil)
	if a := item.NewMapAdapter(nil); a != nil {
		t.Fatal("NewMapAdapter(nil) should return nil")
	}
	// NewStructAdapter on non-struct
	if a := item.NewStructAdapter(42); a != nil {
		t.Fatal("NewStructAdapter(int) should return nil")
	}
	// NewStructAdapter on nil pointer
	var bp *book
	if a := item.NewStructAdapter(bp); a != nil {
		t.Fatal("NewStructAdapter(nil *book) should return nil")
	}
	if a := item.NewStructAdapter(nil); a != nil {
		t.Fatal("NewStructAdapter(nil) should return nil")
	}
}

func TestRegister_Nil(t *testing.T) {
	t.Cleanup(item.ClearRegistered)
	item.Register(nil) // 应该是 no-op
	// 注册一个总是返回 nil 的工厂，验证会回退到内置检测
	item.Register(func(any) item.ItemAdapter { return nil })
	a := item.Adapt(map[string]any{"a": 1})
	if a == nil {
		t.Fatal("fallback to builtin failed")
	}
}
