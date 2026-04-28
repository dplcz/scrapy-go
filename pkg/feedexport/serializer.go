// Package feedexport 的字段序列化器注册表。
//
// 对齐 Scrapy 的 BaseItemExporter.serialize_field 机制：
// Exporter 在写入每个字段前，根据 FieldMeta 中的 "serializer" 键查表调用
// 已注册的序列化函数，将原始值转换为导出值。
//
// 典型用法：
//
//	type Product struct {
//	    Price float64 `item:"price,serializer=to_int"`
//	}
//
//	func init() {
//	    feedexport.RegisterSerializer("to_int", func(v any) any {
//	        if f, ok := v.(float64); ok {
//	            return int(f)
//	        }
//	        return v
//	    })
//	}
package feedexport

import (
	"sync"

	"github.com/dplcz/scrapy-go/pkg/item"
)

// SerializeFunc 定义字段序列化函数。
// 接收原始字段值，返回序列化后的值。
//
// 对应 Scrapy 中 Field(serializer=func) 的 serializer 参数。
type SerializeFunc func(value any) any

// serializerRegistry 是全局的序列化器注册表。
var (
	serializerRegistryMu sync.RWMutex
	serializerRegistry   = make(map[string]SerializeFunc)
)

// RegisterSerializer 注册一个命名的字段序列化函数。
// 若 name 已存在，会被覆盖。线程安全。
//
// 对齐 Scrapy 的 Field(serializer=func) 机制，但采用显式注册表模式
// 替代 Python 的动态函数引用。
//
// 用法：
//
//	feedexport.RegisterSerializer("to_int", func(v any) any {
//	    if f, ok := v.(float64); ok {
//	        return int(f)
//	    }
//	    return v
//	})
func RegisterSerializer(name string, fn SerializeFunc) {
	if name == "" || fn == nil {
		return
	}
	serializerRegistryMu.Lock()
	defer serializerRegistryMu.Unlock()
	serializerRegistry[name] = fn
}

// LookupSerializer 按名称查找已注册的序列化函数。
// 返回 nil 和 false 表示未注册。
func LookupSerializer(name string) (SerializeFunc, bool) {
	serializerRegistryMu.RLock()
	defer serializerRegistryMu.RUnlock()
	fn, ok := serializerRegistry[name]
	return fn, ok
}

// ClearSerializers 清空所有已注册的序列化函数（仅用于测试）。
func ClearSerializers() {
	serializerRegistryMu.Lock()
	defer serializerRegistryMu.Unlock()
	serializerRegistry = make(map[string]SerializeFunc)
}

// SerializeField 对齐 Scrapy 的 BaseItemExporter.serialize_field 方法。
//
// 逻辑：
//  1. 从 FieldMeta 中读取 "serializer" 键
//  2. 若命中已注册的 SerializeFunc，调用并返回转换后的值
//  3. 未命中则回退返回原始值（identity）
//
// 参数：
//   - meta: 字段元数据（可为 nil）
//   - name: 字段名（用于日志/调试，当前未使用）
//   - value: 原始字段值
func SerializeField(meta item.FieldMeta, name string, value any) any {
	if meta == nil {
		return value
	}
	serializerName := meta.GetString("serializer")
	if serializerName == "" {
		return value
	}
	fn, ok := LookupSerializer(serializerName)
	if !ok {
		return value
	}
	return fn(value)
}

// serializeItemFields 将 Item 的所有字段通过 serialize_field 钩子处理后返回。
//
// 此函数供 Exporter 内部使用，替代直接读取原始字段值。
// 返回 (字段名列表, 获取序列化后字段值的函数)。
//
// 若 Item 无法适配（Adapt 返回 nil），返回空字段列表。
func serializeItemFields(in any, fieldsToExport []string) (fieldNames []string, getField func(name string) (any, bool)) {
	a := item.Adapt(in)
	if a == nil {
		return nil, func(string) (any, bool) { return nil, false }
	}

	allFields := a.FieldNames()
	fields := filterFields(allFields, fieldsToExport)

	return fields, func(name string) (any, bool) {
		v, ok := a.GetField(name)
		if !ok {
			return nil, false
		}
		meta := a.FieldMeta(name)
		return SerializeField(meta, name, v), true
	}
}
