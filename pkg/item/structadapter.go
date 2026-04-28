package item

import (
	"reflect"
	"strings"
	"sync"
)

// 对应 Scrapy item tag 的键名常量。
const (
	// TagItem 是推荐的字段名覆盖 tag，优先级最高。
	TagItem = "item"
	// TagJSON 是常见的 json 兼容 tag，在 item tag 缺失时使用。
	TagJSON = "json"
)

// StructAdapter 将 struct（或指向 struct 的指针）适配为 [ItemAdapter]。
//
// 字段名解析顺序：`item` tag 第一个 token → `json` tag 第一个 token → Go 字段名。
// 字段名为 "-" 的字段会被跳过。
// 元数据（除第一个 token 外的其他 tag）可以通过 [ItemAdapter.FieldMeta] 访问。
//
// 可写性：
//   - 传入指向 struct 的指针 → 可写
//   - 传入 struct 值（非指针） → 只读，SetField 返回 [ErrFieldReadOnly]
//
// 对应 Scrapy `itemadapter.AttrsAdapter` / `DataclassAdapter` 的职责，
// 但 Go 通过 reflect 统一处理任意 struct。
type StructAdapter struct {
	raw       any             // 原始 Item
	rv        reflect.Value   // 解引用后的 struct Value
	meta      *structMetadata // 字段元数据（缓存）
	writable  bool            // 是否可写（Item 必须是指针传入）
	presented map[string]bool // map 形式快速判断 "HasField"（这里所有声明字段都视为已存在）
}

// structMetadata 是 struct 类型级别的元数据缓存，按 reflect.Type 维度缓存。
type structMetadata struct {
	// orderedNames 保持字段声明顺序，用于 FieldNames()。
	orderedNames []string
	// fieldByName 用于 O(1) 定位字段。
	fieldByName map[string]*structFieldMeta
}

type structFieldMeta struct {
	name        string
	fieldIndex  []int     // 用于 FieldByIndex（支持匿名嵌套）
	goFieldName string    // 原始 Go 字段名
	tagItem     string    // `item:"..."` 原始 tag
	tagJSON     string    // `json:"..."` 原始 tag
	meta        FieldMeta // 解析后的元数据（懒构建）
}

var structMetaCache sync.Map // map[reflect.Type]*structMetadata

// NewStructAdapter 为 struct 或 *struct 创建 [ItemAdapter]。
//
// 若 item 不是 struct/*struct 或为 nil 指针则返回 nil。
func NewStructAdapter(item any) *StructAdapter {
	if item == nil {
		return nil
	}
	rv := reflect.ValueOf(item)
	writable := false
	switch rv.Kind() {
	case reflect.Ptr:
		if rv.IsNil() {
			return nil
		}
		writable = true
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	meta := getStructMetadata(rv.Type())
	return &StructAdapter{
		raw:      item,
		rv:       rv,
		meta:     meta,
		writable: writable,
	}
}

// Item 实现 [ItemAdapter.Item]。
func (a *StructAdapter) Item() any { return a.raw }

// FieldNames 实现 [ItemAdapter.FieldNames]，按声明顺序返回字段名。
func (a *StructAdapter) FieldNames() []string {
	out := make([]string, len(a.meta.orderedNames))
	copy(out, a.meta.orderedNames)
	return out
}

// GetField 实现 [ItemAdapter.GetField]。
func (a *StructAdapter) GetField(name string) (any, bool) {
	f, ok := a.meta.fieldByName[name]
	if !ok {
		return nil, false
	}
	return a.rv.FieldByIndex(f.fieldIndex).Interface(), true
}

// SetField 实现 [ItemAdapter.SetField]。
func (a *StructAdapter) SetField(name string, value any) error {
	f, ok := a.meta.fieldByName[name]
	if !ok {
		return wrapFieldErr(ErrFieldNotFound, name, a.raw)
	}
	if !a.writable {
		return wrapFieldErr(ErrFieldReadOnly, name, a.raw)
	}
	fv := a.rv.FieldByIndex(f.fieldIndex)
	if !fv.CanSet() {
		return wrapFieldErr(ErrFieldReadOnly, name, a.raw)
	}
	ft := fv.Type()
	if value == nil {
		fv.Set(reflect.Zero(ft))
		return nil
	}
	vv := reflect.ValueOf(value)
	if !vv.Type().AssignableTo(ft) {
		if vv.Type().ConvertibleTo(ft) {
			vv = vv.Convert(ft)
		} else {
			return wrapFieldErr(ErrFieldReadOnly, name, a.raw)
		}
	}
	fv.Set(vv)
	return nil
}

// HasField 实现 [ItemAdapter.HasField]。
//
// 对齐 Scrapy：对于 struct 类型，所有声明字段都视为"已存在"——无论是否被赋值。
// 若需要区分"已设置"与"未设置"，请使用指针字段或由业务层自行维护标志。
func (a *StructAdapter) HasField(name string) bool {
	_, ok := a.meta.fieldByName[name]
	return ok
}

// AsMap 实现 [ItemAdapter.AsMap]，按声明顺序填充 map（Go 的 map 本身无序，
// 此处顺序的保持仅在遍历 FieldNames 时体现）。
func (a *StructAdapter) AsMap() map[string]any {
	out := make(map[string]any, len(a.meta.orderedNames))
	for _, name := range a.meta.orderedNames {
		f := a.meta.fieldByName[name]
		out[name] = a.rv.FieldByIndex(f.fieldIndex).Interface()
	}
	return out
}

// Len 实现 [ItemAdapter.Len]，对于 struct 返回字段总数。
func (a *StructAdapter) Len() int { return len(a.meta.orderedNames) }

// FieldMeta 实现 [ItemAdapter.FieldMeta]。
func (a *StructAdapter) FieldMeta(name string) FieldMeta {
	f, ok := a.meta.fieldByName[name]
	if !ok {
		return nil
	}
	// 懒构建并缓存在 structFieldMeta 上；struct-level metadata cache 全局只构建一次。
	if f.meta != nil {
		return f.meta
	}
	meta := buildFieldMeta(f.tagItem, f.tagJSON)
	f.meta = meta
	return meta
}

// ============================================================================
// 元数据解析（缓存）
// ============================================================================

// getStructMetadata 按 reflect.Type 缓存字段元数据，避免每个 Adapter 实例重复反射。
func getStructMetadata(t reflect.Type) *structMetadata {
	if cached, ok := structMetaCache.Load(t); ok {
		return cached.(*structMetadata)
	}
	md := buildStructMetadata(t)
	actual, _ := structMetaCache.LoadOrStore(t, md)
	return actual.(*structMetadata)
}

// buildStructMetadata 遍历 struct 字段构造元数据。
//
// 规则：
//   - 非导出字段忽略
//   - 字段名：`item` tag 第一个 token 优先，其次 `json` tag，最后 Go 字段名
//   - 字段名为 "-" 的字段跳过
//   - 不展开匿名嵌套（保持简单；如后续需要与 Scrapy 的 Item 行为完全一致，可在 Sprint 9 再增强）
func buildStructMetadata(t reflect.Type) *structMetadata {
	md := &structMetadata{
		fieldByName: make(map[string]*structFieldMeta),
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		itemTag, _ := f.Tag.Lookup(TagItem)
		jsonTag, _ := f.Tag.Lookup(TagJSON)
		name := resolveName(itemTag, jsonTag, f.Name)
		if name == "-" {
			continue
		}
		fm := &structFieldMeta{
			name:        name,
			fieldIndex:  append([]int(nil), f.Index...),
			goFieldName: f.Name,
			tagItem:     itemTag,
			tagJSON:     jsonTag,
		}
		md.orderedNames = append(md.orderedNames, name)
		md.fieldByName[name] = fm
	}
	return md
}

// resolveName 按 item → json → Go 字段名 的顺序解析最终字段名。
func resolveName(itemTag, jsonTag, fallback string) string {
	if token := firstToken(itemTag); token != "" {
		return token
	}
	if token := firstToken(jsonTag); token != "" {
		return token
	}
	return fallback
}

// firstToken 返回逗号分隔字符串的首个 token（去除空白）；全空或 "-" 原样返回。
func firstToken(v string) string {
	if v == "" {
		return ""
	}
	if i := strings.Index(v, ","); i >= 0 {
		return strings.TrimSpace(v[:i])
	}
	return strings.TrimSpace(v)
}

// buildFieldMeta 根据 item/json tag 构建 [FieldMeta]。
//
// 解析规则：item tag 的非首个 token 依次作为 key 或 key=value 收集；
// json tag 的非首个 token 由于是标准库语义（如 omitempty、string），
// 在 meta 中以 `json_<token>=true` 形式记录，供下游 Exporter 可选使用。
func buildFieldMeta(itemTag, jsonTag string) FieldMeta {
	meta := FieldMeta{}
	collectItemTag(meta, itemTag)
	collectJSONTag(meta, jsonTag)
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func collectItemTag(meta FieldMeta, tag string) {
	if tag == "" {
		return
	}
	parts := strings.Split(tag, ",")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i == 0 || p == "" {
			// 首个 token 是字段名，不进入 meta
			continue
		}
		if eq := strings.Index(p, "="); eq > 0 {
			key := strings.TrimSpace(p[:eq])
			value := strings.TrimSpace(p[eq+1:])
			if key != "" {
				meta[key] = value
			}
		} else {
			meta[p] = true
		}
	}
}

func collectJSONTag(meta FieldMeta, tag string) {
	if tag == "" {
		return
	}
	parts := strings.Split(tag, ",")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i == 0 || p == "" {
			continue
		}
		meta["json_"+p] = true
	}
}
