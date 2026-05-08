// Package item 提供 scrapy-go 框架的 Item 统一访问抽象（ItemAdapter 体系）。
//
// # 概述
//
// item 包实现了对不同数据结构（map、struct）的统一访问接口，使得 Pipeline、
// Feed Export 等下游组件无需关心 Item 的具体类型即可读写字段。
// 对应 Scrapy Python 版本中独立的 itemadapter 库的功能。
//
// # 核心类型
//
// 本包提供以下核心类型：
//   - [ItemAdapter]：统一访问接口，定义 GetField/SetField/FieldNames/AsMap 等方法
//   - [MapAdapter]：map 类型的适配器实现（支持 map[string]any、map[string]string 等）
//   - [StructAdapter]：struct 类型的适配器实现（通过 reflect + struct tag 解析）
//   - [FieldMeta]：字段元数据（从 struct tag 解析，用于 Feed Export/Pipeline 辅助）
//   - [AdapterFactory]：自定义适配器工厂函数类型
//
// # 使用方式
//
// 自动适配（推荐）：
//
//	adapter := item.Adapt(anyItem)
//	for _, name := range adapter.FieldNames() {
//	    value, _ := adapter.GetField(name)
//	    fmt.Printf("%s = %v\n", name, value)
//	}
//
// 转为 map（Feed Export、日志、审计等场景）：
//
//	m := item.AsMap(anyItem)
//
// 判断是否可适配：
//
//	if item.IsItem(v) {
//	    adapter := item.Adapt(v)
//	    // ...
//	}
//
// # 适配检测顺序
//
// [Adapt] 函数按以下顺序检测 Item 类型：
//  1. nil → 返回 nil
//  2. 已实现 [ItemAdapter] 接口 → 直接返回（零开销）
//  3. 用户注册的自定义工厂（按注册逆序尝试）
//  4. key=string 的 map → [MapAdapter]
//  5. struct / *struct → [StructAdapter]
//  6. 其他类型 → 返回 nil
//
// # Struct Tag 语法
//
// [StructAdapter] 通过 struct tag 控制字段名和元数据：
//
//	type Book struct {
//	    Title  string  `item:"title"`                    // 字段名为 "title"
//	    Price  float64 `item:"price,required"`           // 必填字段
//	    Author string  `item:"author,default=Unknown"`   // 默认值
//	    ISBN   string  `item:"-"`                        // 忽略此字段
//	}
//
// 支持的 tag 选项：
//   - 第一个 token：字段名（空或 "-" 表示忽略）
//   - required：标记为必填字段（[Validate] 时校验）
//   - default=value：默认值（[Validate] 时填充）
//   - serializer=name：序列化器提示（供 Feed Export 使用）
//
// # 自定义适配器
//
// 通过 [Register] 注册自定义工厂，为第三方类型提供适配：
//
//	item.Register(func(it any) item.ItemAdapter {
//	    if pb, ok := it.(proto.Message); ok {
//	        return &ProtoAdapter{msg: pb}
//	    }
//	    return nil
//	})
//
// # 与 Scrapy 的差异
//
//   - 仅保留 map 和 struct 两种内置适配（舍弃 attrs/dataclass/pydantic）
//   - 使用显式方法名 GetField/SetField 替代 Python 的 __getitem__/__setitem__
//   - 使用 [Register] 显式注册替代 Python 的动态注册机制和元类魔法
//   - 字段元数据从 struct tag 解析，替代 Python 的 Field(**meta) 字典
//   - [Validate] 函数提供编译期可选的字段校验（填充默认值 + 校验 required）
//   - 错误通过 error 返回，不使用 panic
package item
