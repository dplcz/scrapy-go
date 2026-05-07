package feedexport

import (
	"github.com/dplcz/scrapy-go/pkg/item"
)

// ============================================================================
// Item 字段提取辅助
//
// Feed Export 通过 pkg/item 的 ItemAdapter 体系统一访问各种 Item 类型
// （map[string]any、map[string]string、任意 struct、用户自定义的 ItemAdapter）。
//
// 本文件保留两个内部辅助函数 extractItem / filterFields，供 exporter 内部使用。
// ============================================================================

// extractItem 将任意 Item 转换为 (fieldNames, getField) 的组合。
//
// 底层通过 [item.Adapt] 统一处理：
//   - 实现了 item.ItemAdapter 接口的值（包括用户自定义类型）
//   - map[string]any / map[string]string / 其他 key=string 的 map（按字典序）
//   - struct / *struct（按声明顺序，支持 `item`/`json` struct tag）
//
// 若 Item 类型无法适配（如 int、string、非 string key 的 map），返回空字段列表。
func extractItem(in any) (fieldNames []string, getField func(name string) (any, bool)) {
	a := item.Adapt(in)
	if a == nil {
		return nil, func(string) (any, bool) { return nil, false }
	}
	return a.FieldNames(), a.GetField
}

// ============================================================================
// 字段过滤
// ============================================================================

// filterFields 根据 fieldsToExport 过滤字段名列表。
// 如果 fieldsToExport 为空，返回 allFields 的副本；
// 否则按 fieldsToExport 的顺序输出（缺失字段保留在结果中，由调用方输出空值）。
func filterFields(allFields, fieldsToExport []string) []string {
	if len(fieldsToExport) == 0 {
		out := make([]string, len(allFields))
		copy(out, allFields)
		return out
	}
	out := make([]string, len(fieldsToExport))
	copy(out, fieldsToExport)
	return out
}
