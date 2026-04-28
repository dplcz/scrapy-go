package item

import (
	"reflect"
	"sort"
)

// MapAdapter 将 key 为 string 的 map 适配为 [ItemAdapter]。
//
// 支持的底层类型：
//   - map[string]any
//   - map[string]string
//   - 其他 key=string 的 map（通过 reflect 访问）
//
// 字段顺序：按键名字典序升序返回，保证 FeedExport / JSON 等场景输出稳定。
// 对应 Scrapy `itemadapter.DictAdapter`。
type MapAdapter struct {
	// raw 是原始 Item（保留以实现 Item() 方法）。
	raw any
	// fast 为 nil 表示需要走 reflect 路径；否则直接操作该 map。
	fast map[string]any
	// rv  仅在 reflect 路径下使用。
	rv reflect.Value
}

// NewMapAdapter 为 map 类型的 Item 创建适配器。
//
// 若 item 不是 key=string 的 map 则返回 nil。
func NewMapAdapter(item any) *MapAdapter {
	if item == nil {
		return nil
	}

	// 快速路径
	switch v := item.(type) {
	case map[string]any:
		if v == nil {
			v = make(map[string]any)
		}
		return &MapAdapter{raw: v, fast: v}
	case map[string]string:
		// map[string]string 没有办法直接 SetField(any)，在此转为桥接的 reflect 视图
		rv := reflect.ValueOf(v)
		return &MapAdapter{raw: v, rv: rv}
	}

	rv := reflect.ValueOf(item)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	if rv.IsNil() {
		return nil
	}
	return &MapAdapter{raw: item, rv: rv}
}

// Item 实现 [ItemAdapter.Item]。
func (a *MapAdapter) Item() any { return a.raw }

// FieldNames 实现 [ItemAdapter.FieldNames]；返回按字典序排序的键名。
func (a *MapAdapter) FieldNames() []string {
	if a.fast != nil {
		names := make([]string, 0, len(a.fast))
		for k := range a.fast {
			names = append(names, k)
		}
		sort.Strings(names)
		return names
	}
	keys := a.rv.MapKeys()
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		names = append(names, k.String())
	}
	sort.Strings(names)
	return names
}

// GetField 实现 [ItemAdapter.GetField]。
func (a *MapAdapter) GetField(name string) (any, bool) {
	if a.fast != nil {
		v, ok := a.fast[name]
		return v, ok
	}
	v := a.rv.MapIndex(reflect.ValueOf(name))
	if !v.IsValid() {
		return nil, false
	}
	return v.Interface(), true
}

// SetField 实现 [ItemAdapter.SetField]。
//
// 对于非 map[string]any 的底层类型，若 value 无法赋值到元素类型则返回 error。
func (a *MapAdapter) SetField(name string, value any) error {
	if a.fast != nil {
		a.fast[name] = value
		return nil
	}
	elemType := a.rv.Type().Elem()
	var vv reflect.Value
	if value == nil {
		vv = reflect.Zero(elemType)
	} else {
		vv = reflect.ValueOf(value)
		if !vv.Type().AssignableTo(elemType) {
			if vv.Type().ConvertibleTo(elemType) {
				vv = vv.Convert(elemType)
			} else {
				return wrapFieldErr(ErrFieldReadOnly, name, a.raw)
			}
		}
	}
	a.rv.SetMapIndex(reflect.ValueOf(name), vv)
	return nil
}

// HasField 实现 [ItemAdapter.HasField]。
func (a *MapAdapter) HasField(name string) bool {
	if a.fast != nil {
		_, ok := a.fast[name]
		return ok
	}
	return a.rv.MapIndex(reflect.ValueOf(name)).IsValid()
}

// AsMap 实现 [ItemAdapter.AsMap]。
//
// 返回浅拷贝，即一级 key 是新的 map，value 仍指向原对象。
func (a *MapAdapter) AsMap() map[string]any {
	if a.fast != nil {
		out := make(map[string]any, len(a.fast))
		for k, v := range a.fast {
			out[k] = v
		}
		return out
	}
	keys := a.rv.MapKeys()
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		out[k.String()] = a.rv.MapIndex(k).Interface()
	}
	return out
}

// Len 实现 [ItemAdapter.Len]。
func (a *MapAdapter) Len() int {
	if a.fast != nil {
		return len(a.fast)
	}
	return a.rv.Len()
}

// FieldMeta 实现 [ItemAdapter.FieldMeta]。map 类型无 struct tag，返回 nil。
func (a *MapAdapter) FieldMeta(string) FieldMeta { return nil }
