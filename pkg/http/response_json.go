package http

import (
	"github.com/tidwall/gjson"
)

// ============================================================================
// JSON 选择器方法（基于 gjson）
// ============================================================================
//
// 以下方法基于 github.com/tidwall/gjson 提供高性能 JSON 路径查询能力。
// 与现有 JSON(v any) error 方法互补共存：
//   - JSON(v any) error — 整体反序列化到 struct/map，适用于需要完整结构化数据的场景
//   - JSONGet/JSONGetMany/JSONExists/JSONForEach — 路径查询，适用于深层嵌套字段提取，
//     零中间分配，直接在 []byte 上查询，性能优于先 Unmarshal 再取值
//
// gjson 路径语法参考：
//   - 点路径：        "data.name"
//   - 数组索引：      "data.items.0"
//   - 数组投影：      "data.items.#.name"（提取所有元素的 name 字段）
//   - 条件过滤：      "data.items.#(price>100)#.name"（过滤 price>100 的元素）
//   - 修饰符管道：    "data.items|@reverse"
//   - 通配符：        "data.*.name"
//
// 更多语法详见 https://github.com/tidwall/gjson/blob/master/SYNTAX.md

// JSONGet 使用 gjson 路径语法查询响应体中的 JSON 值。
//
// 基于 gjson.GetBytes 直接在 []byte 上做路径查询，零中间分配，
// 返回的 gjson.Result 自带 String/Int/Float/Bool/Array/Map/Time/Exists 等访问器，
// 避免 map[string]any 深层断言。
//
// 路径语法示例：
//   - "data.name"                    — 获取嵌套字段
//   - "data.items.0.name"            — 获取数组第一个元素的 name
//   - "data.items.#.name"            — 投影：提取所有元素的 name 字段
//   - "data.items.#(price>100)#.name" — 过滤：price>100 的元素的 name
//
// 用法：
//
//	result := response.JSONGet("data.items.0.name")
//	if result.Exists() {
//	    fmt.Println(result.String())
//	}
func (r *Response) JSONGet(path string) gjson.Result {
	return gjson.GetBytes(r.Body, path)
}

// JSONGetMany 使用 gjson 一次扫描多个路径，性能优于多次调用 JSONGet。
//
// 内部使用 gjson.GetManyBytes 实现单次扫描多路径提取，
// 当需要从同一响应中提取多个字段时，比多次调用 JSONGet 更高效。
//
// 用法：
//
//	results := response.JSONGetMany("data.name", "data.age", "data.email")
//	name := results[0].String()
//	age := results[1].Int()
//	email := results[2].String()
func (r *Response) JSONGetMany(paths ...string) []gjson.Result {
	return gjson.GetManyBytes(r.Body, paths...)
}

// JSONExists 检查指定 gjson 路径在响应体中是否存在。
//
// 等价于 response.JSONGet(path).Exists()，但语义更清晰。
// 适用于条件判断场景，如检查 API 响应中是否包含某个字段。
//
// 用法：
//
//	if response.JSONExists("data.error") {
//	    // 处理错误响应
//	}
func (r *Response) JSONExists(path string) bool {
	return gjson.GetBytes(r.Body, path).Exists()
}

// JSONForEach 流式遍历指定路径下的 JSON 数组或对象。
//
// 避免大数组一次性分配，适用于处理大量数据的场景。
// 对于数组，key 为索引的字符串表示，value 为元素值；
// 对于对象，key 为键名，value 为键值。
//
// iter 回调返回 false 时提前终止遍历。
//
// 用法：
//
//	// 遍历数组
//	response.JSONForEach("data.items", func(key, value gjson.Result) bool {
//	    fmt.Printf("item[%s]: %s\n", key.String(), value.Get("name").String())
//	    return true // 返回 false 提前终止
//	})
//
//	// 遍历对象
//	response.JSONForEach("data.config", func(key, value gjson.Result) bool {
//	    fmt.Printf("%s = %s\n", key.String(), value.String())
//	    return true
//	})
func (r *Response) JSONForEach(path string, iter func(key, value gjson.Result) bool) {
	result := gjson.GetBytes(r.Body, path)
	result.ForEach(iter)
}
