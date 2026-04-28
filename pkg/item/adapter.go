// Package item 提供 scrapy-go 框架的 Item 统一访问抽象（ItemAdapter 体系）。
//
// Scrapy Python 版本依赖独立的 `itemadapter` 库，对同一段 Pipeline/Exporter 代码
// 无差别地适配 dict、Item、attrs、dataclass、pydantic 等多种数据模型。
//
// Go 版本借鉴该思想，但结合语言特性做了以下取舍：
//
//   - 仅保留两种内置适配：map（`map[string]any` / `map[string]string` 等）与 struct
//     （通过 `reflect` 与 struct tag），分别由 [MapAdapter] 与 [StructAdapter] 实现。
//   - 用户可通过实现 [ItemAdapter] 接口自定义适配，函数 [Adapt] 会优先识别。
//   - 舍弃 Python 的动态注册机制与元类魔法；注册适配器工厂通过 [Register] 显式完成。
//   - 字段元数据（Field metadata）以 [FieldMeta] 表达，默认从 struct tag 解析；
//     框架内部仅用于 Feed Export/Pipeline 的辅助能力，不作为运行时校验依据。
//
// 典型使用场景：
//
//	// 1. 在 Pipeline 中读取任意 Item
//	adapter := item.Adapt(it)
//	for _, name := range adapter.FieldNames() {
//	    value, _ := adapter.GetField(name)
//	    // ...
//	}
//
//	// 2. 将 Item 转为 map（Feed Export、日志、审计等）
//	m := item.Adapt(it).AsMap()
//
// 与 Scrapy 原版差异：
//
//   - Scrapy 的 `ItemAdapter` 通过 `__getitem__` / `__setitem__` 动态操作。
//     Go 版本使用显式方法名 `GetField/SetField`，错误通过 error 返回。
//   - Scrapy 的 `is_item` 判断基于 MRO 查找；Go 版本通过 [IsItem] 判断是否能被 [Adapt] 适配。
package item

import (
	"errors"
	"fmt"
)

// ErrFieldNotFound 表示字段不存在。
var ErrFieldNotFound = errors.New("item: field not found")

// ErrFieldReadOnly 表示字段不可写（例如未导出字段或 struct 字段通过值拷贝传入）。
var ErrFieldReadOnly = errors.New("item: field is read-only")

// ErrUnsupportedItem 表示 Item 类型无法被适配。
var ErrUnsupportedItem = errors.New("item: unsupported item type")

// FieldMeta 表示字段元数据。
//
// 对应 Scrapy 的 `Field(**meta)` 机制：一个 `dict[str, Any]`，
// 允许携带任意键值对用于下游 Exporter/Pipeline 读取。
//
// 对于 struct 类型的 Item，元数据默认从 struct tag 解析：
//
//	type Book struct {
//	    Title string `item:"title" serializer:"strip"`
//	}
//
// `item` 标签的第一个 token 作为字段名；其余 tag 以 key=value 或裸 key 形式被收集。
type FieldMeta map[string]any

// Get 返回指定元数据键对应的值。若不存在返回 (nil, false)。
func (m FieldMeta) Get(key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

// GetString 返回 string 类型的元数据值；若不存在或类型不匹配返回空字符串。
func (m FieldMeta) GetString(key string) string {
	v, ok := m.Get(key)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Clone 返回 FieldMeta 的浅拷贝。
func (m FieldMeta) Clone() FieldMeta {
	if m == nil {
		return nil
	}
	out := make(FieldMeta, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ItemAdapter 定义 Item 的统一访问契约。
//
// 实现应当是“薄封装”，即同一 Item 对应同一 Adapter 实例的多次读/写操作，
// 行为与直接读写底层结构一致。
//
// 线程安全性：单个 Adapter 实例的并发访问由调用方保证。
// 通常 Pipeline / Exporter 在 Item 流水线中顺序使用，无需额外加锁。
type ItemAdapter interface {
	// Item 返回底层原始 Item（通常用于需要绕过适配层时）。
	Item() any

	// FieldNames 返回声明的全部字段名。
	//
	//   - 对 map：返回当前 map 中的键（按键名字典序，保证输出稳定）。
	//   - 对 struct：返回声明顺序的导出字段名（受 struct tag 影响）。
	//
	// 对应 Scrapy ItemAdapter 的 `field_names()`。
	FieldNames() []string

	// GetField 读取字段值。
	//
	// 若字段不存在（例如 map 中缺少该键），返回 (nil, false)。
	// 此方法不会返回 error，保持接口简洁；类型相关错误由调用方自行处理。
	GetField(name string) (any, bool)

	// SetField 写入字段值。若字段不可写则返回 [ErrFieldReadOnly]。
	//
	// 对应 Scrapy ItemAdapter 的 `__setitem__`。
	SetField(name string, value any) error

	// HasField 判断字段是否存在（即 GetField 是否会返回 ok=true）。
	//
	// 对应 Scrapy ItemAdapter 的 `__contains__`。
	HasField(name string) bool

	// AsMap 返回所有字段的 map 快照（深度一级拷贝，嵌套对象不克隆）。
	//
	// 对应 Scrapy ItemAdapter 的 `asdict()`，但不递归展开嵌套 Item。
	// Feed Export / 日志 / 审计 等场景建议使用此方法。
	AsMap() map[string]any

	// Len 返回当前已设置的字段数量（非全部声明字段）。
	Len() int

	// FieldMeta 返回字段的元数据。对于 struct，从 struct tag 解析；
	// 对于 map，返回 nil。
	FieldMeta(name string) FieldMeta
}

// IsItem 判断 item 是否能被 [Adapt] 函数适配。
//
// 规则：
//  1. nil → false
//  2. 实现了 [ItemAdapter] 接口 → true
//  3. map（key 为 string） → true
//  4. struct / *struct → true
//  5. 其他 → false
func IsItem(item any) bool {
	if item == nil {
		return false
	}
	if _, ok := item.(ItemAdapter); ok {
		return true
	}
	switch kind := kindOf(item); kind {
	case kindMap, kindStruct:
		return true
	default:
		return false
	}
}

// sanityCheck 内部辅助：确保 Adapter 返回的错误信息中带上 Item 类型便于定位。
func wrapFieldErr(err error, field string, item any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: field=%q item=%T", err, field, item)
}
