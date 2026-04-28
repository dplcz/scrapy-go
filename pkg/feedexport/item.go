package feedexport

import (
	"reflect"
	"sort"
	"strings"
)

// ============================================================================
// Item 字段提取辅助
//
// Feed Export 需要统一处理两种常见的 Item 形态：
//  1. map[string]any — 最常见，无 schema
//  2. struct          — 通过反射 + struct tag 读取字段
//
// 为避免引入完整的 ItemAdapter 体系（将在 P2-009 单独实现），此处提供精简的
// 字段提取函数。最终版本将迁移到 pkg/item 包，Feed Export 仅引用其接口。
// ============================================================================

// itemFieldTag 用于 struct tag 的键名，例如：
//
//	type Book struct {
//	    Title string `item:"title"`
//	    Price int    `item:"price"`
//	}
const itemFieldTag = "item"

// extractItem 将任意 Item 转换为 (fieldNames, getField) 的组合。
// fieldNames 保持字段的"自然顺序"：
//   - struct 按声明顺序
//   - map 按字段名字典序（保证输出稳定）
func extractItem(item any) (fieldNames []string, getField func(name string) (any, bool)) {
	if item == nil {
		return nil, func(string) (any, bool) { return nil, false }
	}

	switch v := item.(type) {
	case map[string]any:
		return extractMapItem(v)
	case map[string]string:
		// 常见于简单键值场景，转换为 map[string]any
		m := make(map[string]any, len(v))
		for k, val := range v {
			m[k] = val
		}
		return extractMapItem(m)
	}

	rv := reflect.ValueOf(item)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil, func(string) (any, bool) { return nil, false }
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Struct:
		return extractStructItem(rv)
	case reflect.Map:
		// 泛型的 map，退化成通过反射提取
		return extractReflectMapItem(rv)
	}

	// 其他类型：无字段
	return nil, func(string) (any, bool) { return nil, false }
}

// extractMapItem 从 map[string]any 中提取字段。
func extractMapItem(m map[string]any) ([]string, func(string) (any, bool)) {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)

	return names, func(name string) (any, bool) {
		v, ok := m[name]
		return v, ok
	}
}

// extractReflectMapItem 通过反射从任意 map 中提取字段。
func extractReflectMapItem(rv reflect.Value) ([]string, func(string) (any, bool)) {
	// 仅支持 key 为 string 的 map
	if rv.Type().Key().Kind() != reflect.String {
		return nil, func(string) (any, bool) { return nil, false }
	}

	keys := rv.MapKeys()
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		names = append(names, k.String())
	}
	sort.Strings(names)

	return names, func(name string) (any, bool) {
		v := rv.MapIndex(reflect.ValueOf(name))
		if !v.IsValid() {
			return nil, false
		}
		return v.Interface(), true
	}
}

// extractStructItem 从 struct 中提取字段。
// 字段名优先取 `item` tag，其次取 `json` tag，最后取字段名本身。
// 匿名字段不展开（保持简单），未导出字段跳过。
func extractStructItem(rv reflect.Value) ([]string, func(string) (any, bool)) {
	t := rv.Type()
	type fieldInfo struct {
		name  string
		index int
	}
	fields := make([]fieldInfo, 0, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := resolveStructFieldName(f.Tag, f.Name)
		if name == "-" {
			continue
		}
		fields = append(fields, fieldInfo{name: name, index: i})
	}

	names := make([]string, 0, len(fields))
	indexByName := make(map[string]int, len(fields))
	for _, f := range fields {
		names = append(names, f.name)
		indexByName[f.name] = f.index
	}

	return names, func(name string) (any, bool) {
		idx, ok := indexByName[name]
		if !ok {
			return nil, false
		}
		return rv.Field(idx).Interface(), true
	}
}

// resolveStructFieldName 解析 struct 字段名（优先 item tag，其次 json tag）。
func resolveStructFieldName(tag reflect.StructTag, fallback string) string {
	if v, ok := tag.Lookup(itemFieldTag); ok {
		if name := firstTagToken(v); name != "" {
			return name
		}
	}
	if v, ok := tag.Lookup("json"); ok {
		if name := firstTagToken(v); name != "" {
			return name
		}
	}
	return fallback
}

// firstTagToken 返回 tag 中第一个逗号分隔的 token。
func firstTagToken(v string) string {
	if i := strings.Index(v, ","); i >= 0 {
		return strings.TrimSpace(v[:i])
	}
	return strings.TrimSpace(v)
}

// ============================================================================
// 字段过滤
// ============================================================================

// filterFields 根据 fieldsToExport 过滤字段名列表。
// 如果 fieldsToExport 为空，返回 allFields 的副本；
// 否则返回 fieldsToExport 中与 allFields 交集保持 fieldsToExport 顺序。
func filterFields(allFields, fieldsToExport []string) []string {
	if len(fieldsToExport) == 0 {
		out := make([]string, len(allFields))
		copy(out, allFields)
		return out
	}

	// 即使字段不存在于 Item，也要按 fieldsToExport 的顺序输出（缺失字段输出空值）
	out := make([]string, len(fieldsToExport))
	copy(out, fieldsToExport)
	return out
}
