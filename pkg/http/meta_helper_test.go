package http

import (
	"encoding/json"
	"errors"
	"testing"
)

// ============================================================================
// 测试用结构体
// ============================================================================

type testDetailItem struct {
	Title string   `json:"title"`
	Price float64  `json:"price"`
	Tags  []string `json:"tags"`
}

type testNestedItem struct {
	Name   string         `json:"name"`
	Detail testDetailItem `json:"detail"`
	Scores []int          `json:"scores"`
}

type testItemWithPtr struct {
	ID   int     `json:"id"`
	Name *string `json:"name"`
}

// unexportedItem 没有导出字段的结构体
type unexportedItem struct {
	hidden string //nolint:unused
}

// ============================================================================
// P5-025a: isJSONSerializable 增强测试
// ============================================================================

func TestIsJSONSerializable_BasicTypes(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want bool
	}{
		{"nil", nil, true},
		{"bool", true, true},
		{"int", 42, true},
		{"int64", int64(100), true},
		{"float64", 3.14, true},
		{"string", "hello", true},
		{"[]any", []any{1, "a"}, true},
		{"[]string", []string{"a", "b"}, true},
		{"[]int", []int{1, 2, 3}, true},
		{"[]float64", []float64{1.1, 2.2}, true},
		{"[]bool", []bool{true, false}, true},
		{"map[string]any", map[string]any{"k": "v"}, true},
		{"map[string]string", map[string]string{"k": "v"}, true},
		{"map[string]int", map[string]int{"k": 1}, true},
		{"map[string]float64", map[string]float64{"k": 1.1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isJSONSerializable(tt.val)
			if got != tt.want {
				t.Errorf("isJSONSerializable(%v) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

func TestIsJSONSerializable_Structs(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want bool
	}{
		{"struct value", testDetailItem{Title: "Go", Price: 29.99}, true},
		{"struct pointer", &testDetailItem{Title: "Go", Price: 29.99}, true},
		{"nested struct", testNestedItem{Name: "test", Detail: testDetailItem{Title: "inner"}}, true},
		{"struct slice", []testDetailItem{{Title: "a"}, {Title: "b"}}, true},
		{"struct ptr slice", []*testDetailItem{{Title: "a"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isJSONSerializable(tt.val)
			if got != tt.want {
				t.Errorf("isJSONSerializable(%v) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

func TestIsJSONSerializable_Rejected(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want bool
	}{
		{"func", func() {}, false},
		{"chan", make(chan int), false},
		{"func slice", []func(){func() {}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isJSONSerializable(tt.val)
			if got != tt.want {
				t.Errorf("isJSONSerializable(%v) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

// ============================================================================
// P5-025b: GetMetaAs[T] (Response) 测试
// ============================================================================

func TestGetMetaAs_DirectTypeAssertion(t *testing.T) {
	// 快路径：值本身就是目标类型（未经过磁盘序列化）
	item := testDetailItem{Title: "Go Programming", Price: 49.99, Tags: []string{"go", "programming"}}

	req := MustNewRequest("https://example.com")
	req.SetMeta("item", item)

	resp := &Response{
		Request: req,
	}

	got, err := GetMetaAs[testDetailItem](resp, "item")
	if err != nil {
		t.Fatalf("GetMetaAs failed: %v", err)
	}
	if got.Title != item.Title || got.Price != item.Price {
		t.Errorf("GetMetaAs = %+v, want %+v", got, item)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "go" {
		t.Errorf("GetMetaAs tags = %v, want %v", got.Tags, item.Tags)
	}
}

func TestGetMetaAs_MapConversion(t *testing.T) {
	// 慢路径：模拟磁盘队列反序列化后值变为 map[string]any
	original := testDetailItem{Title: "Go Book", Price: 39.99, Tags: []string{"tech"}}

	// 模拟序列化往返：struct → JSON → map[string]any
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var mapForm map[string]any
	if err := json.Unmarshal(data, &mapForm); err != nil {
		t.Fatalf("json.Unmarshal to map failed: %v", err)
	}

	req := MustNewRequest("https://example.com")
	req.SetMeta("item", mapForm)

	resp := &Response{
		Request: req,
	}

	got, err := GetMetaAs[testDetailItem](resp, "item")
	if err != nil {
		t.Fatalf("GetMetaAs (map conversion) failed: %v", err)
	}
	if got.Title != original.Title || got.Price != original.Price {
		t.Errorf("GetMetaAs = %+v, want %+v", got, original)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "tech" {
		t.Errorf("GetMetaAs tags = %v, want %v", got.Tags, original.Tags)
	}
}

func TestGetMetaAs_NestedStruct(t *testing.T) {
	original := testNestedItem{
		Name:   "nested",
		Detail: testDetailItem{Title: "Inner", Price: 19.99},
		Scores: []int{90, 85, 92},
	}

	// 模拟磁盘反序列化
	data, _ := json.Marshal(original)
	var mapForm map[string]any
	json.Unmarshal(data, &mapForm)

	req := MustNewRequest("https://example.com")
	req.SetMeta("nested", mapForm)

	resp := &Response{Request: req}

	got, err := GetMetaAs[testNestedItem](resp, "nested")
	if err != nil {
		t.Fatalf("GetMetaAs (nested) failed: %v", err)
	}
	if got.Name != original.Name {
		t.Errorf("Name = %q, want %q", got.Name, original.Name)
	}
	if got.Detail.Title != original.Detail.Title || got.Detail.Price != original.Detail.Price {
		t.Errorf("Detail = %+v, want %+v", got.Detail, original.Detail)
	}
	if len(got.Scores) != 3 || got.Scores[0] != 90 {
		t.Errorf("Scores = %v, want %v", got.Scores, original.Scores)
	}
}

func TestGetMetaAs_StructSlice(t *testing.T) {
	original := []testDetailItem{
		{Title: "Book A", Price: 10.0},
		{Title: "Book B", Price: 20.0},
	}

	// 模拟磁盘反序列化
	data, _ := json.Marshal(original)
	var sliceForm []any
	json.Unmarshal(data, &sliceForm)

	req := MustNewRequest("https://example.com")
	req.SetMeta("items", sliceForm)

	resp := &Response{Request: req}

	got, err := GetMetaAs[[]testDetailItem](resp, "items")
	if err != nil {
		t.Fatalf("GetMetaAs (slice) failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Title != "Book A" || got[1].Price != 20.0 {
		t.Errorf("GetMetaAs slice = %+v, want %+v", got, original)
	}
}

func TestGetMetaAs_KeyNotFound(t *testing.T) {
	req := MustNewRequest("https://example.com")
	resp := &Response{Request: req}

	_, err := GetMetaAs[testDetailItem](resp, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
	if !errors.Is(err, ErrMetaKeyNotFound) {
		t.Errorf("error = %v, want ErrMetaKeyNotFound", err)
	}
}

func TestGetMetaAs_NilMeta(t *testing.T) {
	req := MustNewRequest("https://example.com")
	req.Meta = nil
	resp := &Response{Request: req}

	_, err := GetMetaAs[testDetailItem](resp, "item")
	if err == nil {
		t.Fatal("expected error for nil meta")
	}
	if !errors.Is(err, ErrMetaNil) {
		t.Errorf("error = %v, want ErrMetaNil", err)
	}
}

func TestGetMetaAs_NilResponse(t *testing.T) {
	_, err := GetMetaAs[testDetailItem](nil, "item")
	if err == nil {
		t.Fatal("expected error for nil response")
	}
	if !errors.Is(err, ErrMetaNil) {
		t.Errorf("error = %v, want ErrMetaNil", err)
	}
}

func TestGetMetaAs_NilRequest(t *testing.T) {
	resp := &Response{Request: nil}

	_, err := GetMetaAs[testDetailItem](resp, "item")
	if err == nil {
		t.Fatal("expected error for nil request")
	}
	if !errors.Is(err, ErrMetaNil) {
		t.Errorf("error = %v, want ErrMetaNil", err)
	}
}

func TestGetMetaAs_PrimitiveTypes(t *testing.T) {
	req := MustNewRequest("https://example.com")
	req.SetMeta("count", 42)
	req.SetMeta("name", "test")
	req.SetMeta("rate", 3.14)
	req.SetMeta("flag", true)

	resp := &Response{Request: req}

	// int
	count, err := GetMetaAs[int](resp, "count")
	if err != nil || count != 42 {
		t.Errorf("int: got %v, err %v", count, err)
	}

	// string
	name, err := GetMetaAs[string](resp, "name")
	if err != nil || name != "test" {
		t.Errorf("string: got %v, err %v", name, err)
	}

	// float64
	rate, err := GetMetaAs[float64](resp, "rate")
	if err != nil || rate != 3.14 {
		t.Errorf("float64: got %v, err %v", rate, err)
	}

	// bool
	flag, err := GetMetaAs[bool](resp, "flag")
	if err != nil || flag != true {
		t.Errorf("bool: got %v, err %v", flag, err)
	}
}

// ============================================================================
// P5-025c: GetRequestMetaAs[T] (Request) 测试
// ============================================================================

func TestGetRequestMetaAs_DirectTypeAssertion(t *testing.T) {
	item := testDetailItem{Title: "Direct", Price: 9.99}

	req := MustNewRequest("https://example.com")
	req.SetMeta("item", item)

	got, err := GetRequestMetaAs[testDetailItem](req, "item")
	if err != nil {
		t.Fatalf("GetRequestMetaAs failed: %v", err)
	}
	if got.Title != item.Title || got.Price != item.Price {
		t.Errorf("GetRequestMetaAs = %+v, want %+v", got, item)
	}
}

func TestGetRequestMetaAs_MapConversion(t *testing.T) {
	original := testDetailItem{Title: "Map Form", Price: 15.0}

	data, _ := json.Marshal(original)
	var mapForm map[string]any
	json.Unmarshal(data, &mapForm)

	req := MustNewRequest("https://example.com")
	req.SetMeta("item", mapForm)

	got, err := GetRequestMetaAs[testDetailItem](req, "item")
	if err != nil {
		t.Fatalf("GetRequestMetaAs (map) failed: %v", err)
	}
	if got.Title != original.Title || got.Price != original.Price {
		t.Errorf("GetRequestMetaAs = %+v, want %+v", got, original)
	}
}

func TestGetRequestMetaAs_NilRequest(t *testing.T) {
	_, err := GetRequestMetaAs[testDetailItem](nil, "item")
	if err == nil {
		t.Fatal("expected error for nil request")
	}
	if !errors.Is(err, ErrMetaNil) {
		t.Errorf("error = %v, want ErrMetaNil", err)
	}
}

func TestGetRequestMetaAs_KeyNotFound(t *testing.T) {
	req := MustNewRequest("https://example.com")

	_, err := GetRequestMetaAs[testDetailItem](req, "missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !errors.Is(err, ErrMetaKeyNotFound) {
		t.Errorf("error = %v, want ErrMetaKeyNotFound", err)
	}
}

// ============================================================================
// P5-025d: 磁盘队列往返测试
// ============================================================================

func TestGetMetaAs_DiskQueueRoundTrip(t *testing.T) {
	// 模拟完整的磁盘队列往返：
	// SetMeta(struct) → ToDict → JSON → 磁盘 → JSON → FromDict → GetMetaAs[T]

	item := testDetailItem{
		Title: "Disk Round Trip",
		Price: 99.99,
		Tags:  []string{"persistence", "test"},
	}

	// 1. 创建请求并设置结构体 Meta
	req := MustNewRequest("https://example.com",
		WithCallback(NoCallback),
	)
	req.SetMeta("item", item)
	req.SetMeta("page", 3)

	// 2. 序列化为字典（模拟 ToDict）
	d := req.ToDict("parse_detail", "")

	// 3. JSON 编码（模拟写入磁盘）
	jsonBytes, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal dict failed: %v", err)
	}

	// 4. JSON 解码（模拟从磁盘读取）
	var restored map[string]any
	if err := json.Unmarshal(jsonBytes, &restored); err != nil {
		t.Fatalf("json.Unmarshal dict failed: %v", err)
	}

	// 5. 从字典恢复 Request（模拟 FromDict）
	registry := NewCallbackRegistry()
	registry.Register("parse_detail", NoCallback)
	restoredReq, err := FromDict(restored, registry)
	if err != nil {
		t.Fatalf("FromDict failed: %v", err)
	}

	// 6. 使用 GetRequestMetaAs 恢复结构体
	resp := &Response{Request: restoredReq}
	got, err := GetMetaAs[testDetailItem](resp, "item")
	if err != nil {
		t.Fatalf("GetMetaAs after disk round trip failed: %v", err)
	}

	// 7. 验证所有字段正确恢复
	if got.Title != item.Title {
		t.Errorf("Title = %q, want %q", got.Title, item.Title)
	}
	if got.Price != item.Price {
		t.Errorf("Price = %v, want %v", got.Price, item.Price)
	}
	if len(got.Tags) != len(item.Tags) {
		t.Fatalf("Tags len = %d, want %d", len(got.Tags), len(item.Tags))
	}
	for i, tag := range got.Tags {
		if tag != item.Tags[i] {
			t.Errorf("Tags[%d] = %q, want %q", i, tag, item.Tags[i])
		}
	}

	// 验证普通 Meta 值也正确恢复
	page, err := GetMetaAs[int](resp, "page")
	if err != nil {
		t.Fatalf("GetMetaAs[int] failed: %v", err)
	}
	if page != 3 {
		t.Errorf("page = %d, want 3", page)
	}
}

func TestGetMetaAs_DiskQueueRoundTrip_NestedStruct(t *testing.T) {
	original := testNestedItem{
		Name: "nested_roundtrip",
		Detail: testDetailItem{
			Title: "Nested Inner",
			Price: 55.5,
			Tags:  []string{"nested"},
		},
		Scores: []int{100, 95, 88},
	}

	req := MustNewRequest("https://example.com", WithCallback(NoCallback))
	req.SetMeta("data", original)

	// 完整往返
	d := req.ToDict("callback", "")
	jsonBytes, _ := json.Marshal(d)
	var restored map[string]any
	json.Unmarshal(jsonBytes, &restored)

	registry := NewCallbackRegistry()
	registry.Register("callback", NoCallback)
	restoredReq, err := FromDict(restored, registry)
	if err != nil {
		t.Fatalf("FromDict failed: %v", err)
	}

	resp := &Response{Request: restoredReq}
	got, err := GetMetaAs[testNestedItem](resp, "data")
	if err != nil {
		t.Fatalf("GetMetaAs nested round trip failed: %v", err)
	}

	if got.Name != original.Name {
		t.Errorf("Name = %q, want %q", got.Name, original.Name)
	}
	if got.Detail.Title != original.Detail.Title {
		t.Errorf("Detail.Title = %q, want %q", got.Detail.Title, original.Detail.Title)
	}
	if got.Detail.Price != original.Detail.Price {
		t.Errorf("Detail.Price = %v, want %v", got.Detail.Price, original.Detail.Price)
	}
	if len(got.Scores) != 3 || got.Scores[2] != 88 {
		t.Errorf("Scores = %v, want %v", got.Scores, original.Scores)
	}
}

func TestGetMetaAs_StructWithPointerField(t *testing.T) {
	name := "pointer_field"
	original := testItemWithPtr{ID: 42, Name: &name}

	// 模拟磁盘反序列化
	data, _ := json.Marshal(original)
	var mapForm map[string]any
	json.Unmarshal(data, &mapForm)

	req := MustNewRequest("https://example.com")
	req.SetMeta("ptr_item", mapForm)

	resp := &Response{Request: req}
	got, err := GetMetaAs[testItemWithPtr](resp, "ptr_item")
	if err != nil {
		t.Fatalf("GetMetaAs (ptr field) failed: %v", err)
	}
	if got.ID != 42 {
		t.Errorf("ID = %d, want 42", got.ID)
	}
	if got.Name == nil || *got.Name != "pointer_field" {
		t.Errorf("Name = %v, want %q", got.Name, "pointer_field")
	}
}

func TestGetMetaAs_ConversionError(t *testing.T) {
	// 尝试将不兼容的值转换为目标类型
	req := MustNewRequest("https://example.com")
	req.SetMeta("bad", "not a struct")

	resp := &Response{Request: req}

	_, err := GetMetaAs[testDetailItem](resp, "bad")
	if err == nil {
		t.Fatal("expected error for incompatible type conversion")
	}
	if !errors.Is(err, ErrMetaConversion) {
		t.Errorf("error = %v, want ErrMetaConversion", err)
	}
}

// ============================================================================
// isJSONSerializable 与 ToDict 集成测试
// ============================================================================

func TestToDict_StructInMeta(t *testing.T) {
	// 验证增强后的 isJSONSerializable 允许结构体通过
	item := testDetailItem{Title: "Serializable", Price: 25.0}

	req := MustNewRequest("https://example.com")
	req.SetMeta("item", item)
	req.SetMeta("nested", testNestedItem{Name: "n", Detail: item})

	d := req.ToDict("", "")

	meta, ok := d["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta not found in dict")
	}

	// 结构体应该被保留（不再被 isJSONSerializable 拒绝）
	if _, ok := meta["item"]; !ok {
		t.Error("struct item was rejected by isJSONSerializable")
	}
	if _, ok := meta["nested"]; !ok {
		t.Error("nested struct was rejected by isJSONSerializable")
	}
}

func TestToDict_FuncInMeta_Rejected(t *testing.T) {
	// 验证函数仍然被拒绝
	req := MustNewRequest("https://example.com")
	req.SetMeta("callback", func() {})
	req.SetMeta("valid", "keep")

	d := req.ToDict("", "")

	meta, ok := d["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta not found in dict")
	}

	if _, ok := meta["callback"]; ok {
		t.Error("func should be rejected by isJSONSerializable")
	}
	if _, ok := meta["valid"]; !ok {
		t.Error("valid string should be kept")
	}
}

func TestToDict_StructPointerInMeta(t *testing.T) {
	item := &testDetailItem{Title: "Ptr", Price: 10.0}

	req := MustNewRequest("https://example.com")
	req.SetMeta("ptr_item", item)

	d := req.ToDict("", "")

	meta, ok := d["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta not found in dict")
	}

	if _, ok := meta["ptr_item"]; !ok {
		t.Error("struct pointer was rejected by isJSONSerializable")
	}
}

func TestToDict_StructSliceInMeta(t *testing.T) {
	items := []testDetailItem{
		{Title: "A", Price: 1.0},
		{Title: "B", Price: 2.0},
	}

	req := MustNewRequest("https://example.com")
	req.SetMeta("items", items)

	d := req.ToDict("", "")

	meta, ok := d["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta not found in dict")
	}

	if _, ok := meta["items"]; !ok {
		t.Error("struct slice was rejected by isJSONSerializable")
	}
}

// ============================================================================
// isJSONSerializable 边界情况补充测试（提高覆盖率）
// ============================================================================

func TestIsJSONSerializable_MapWithNonStringKey(t *testing.T) {
	// map[int]string 的 key 不是 string，应该走 tryMarshal fallback
	// json.Marshal 对 map[int]string 会返回错误（Go 1.25 中 int key 不支持）
	m := map[int]string{1: "one", 2: "two"}
	got := isJSONSerializable(m)
	// map[int]string 在 json.Marshal 中会失败
	if got {
		t.Errorf("isJSONSerializable(map[int]string) = true, want false")
	}
}

func TestIsJSONSerializable_TypedSlice(t *testing.T) {
	// 自定义类型的切片
	type MyString string
	vals := []MyString{"a", "b", "c"}
	got := isJSONSerializable(vals)
	if !got {
		t.Errorf("isJSONSerializable([]MyString) = false, want true")
	}
}

func TestIsJSONSerializable_MapStringStruct(t *testing.T) {
	// map[string]struct 应该可序列化
	m := map[string]testDetailItem{
		"book": {Title: "Test", Price: 10.0},
	}
	got := isJSONSerializable(m)
	if !got {
		t.Errorf("isJSONSerializable(map[string]struct) = false, want true")
	}
}

func TestIsJSONSerializable_Array(t *testing.T) {
	// 固定长度数组
	arr := [3]int{1, 2, 3}
	got := isJSONSerializable(arr)
	if !got {
		t.Errorf("isJSONSerializable([3]int) = false, want true")
	}
}

func TestIsJSONSerializable_ChanSlice(t *testing.T) {
	// 包含 channel 的切片应该不可序列化
	chans := []chan int{make(chan int)}
	got := isJSONSerializable(chans)
	if got {
		t.Errorf("isJSONSerializable([]chan int) = true, want false")
	}
}

func TestIsJSONSerializable_CacheHit(t *testing.T) {
	// 测试缓存命中路径：同一类型第二次调用应该走缓存
	item1 := testDetailItem{Title: "First"}
	item2 := testDetailItem{Title: "Second"}

	// 第一次调用（缓存 miss）
	got1 := isJSONSerializable(item1)
	// 第二次调用（缓存 hit）
	got2 := isJSONSerializable(item2)

	if got1 != got2 || !got1 {
		t.Errorf("cache inconsistency: got1=%v, got2=%v", got1, got2)
	}
}
