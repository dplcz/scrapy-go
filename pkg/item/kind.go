package item

import "reflect"

// itemKind 表示 Item 的底层类型分类。
type itemKind int

const (
	kindUnknown itemKind = iota
	kindAdapter          // 已经实现 ItemAdapter 接口
	kindMap              // map[string]any 或其他 key=string 的 map
	kindStruct           // struct 或 *struct
)

// kindOf 返回 item 的分类（去掉一层指针）。
func kindOf(item any) itemKind {
	if item == nil {
		return kindUnknown
	}
	if _, ok := item.(ItemAdapter); ok {
		return kindAdapter
	}

	// 快速路径：常见类型
	switch item.(type) {
	case map[string]any, map[string]string:
		return kindMap
	}

	rv := reflect.ValueOf(item)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return kindUnknown
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			return kindMap
		}
	case reflect.Struct:
		return kindStruct
	}
	return kindUnknown
}
