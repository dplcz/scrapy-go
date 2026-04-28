package item

import (
	"fmt"
	"sync"
)

// AdapterFactory 将原始 Item 适配为 [ItemAdapter]。
// 若当前工厂无法处理给定类型，应返回 nil。
type AdapterFactory func(item any) ItemAdapter

// ============================================================================
// 工厂注册表
// ============================================================================

var (
	customFactoriesMu sync.RWMutex
	customFactories   []AdapterFactory
)

// Register 注册一个自定义的 [AdapterFactory]。
//
// 注册顺序决定优先级：后注册的工厂会先尝试匹配。若工厂对给定 Item 返回 nil，
// 则退回到内置的自动检测（接口 → map → struct）。
//
// 典型使用场景：为第三方 ORM 结构体、protobuf Message 等提供自定义适配。
func Register(factory AdapterFactory) {
	if factory == nil {
		return
	}
	customFactoriesMu.Lock()
	customFactories = append(customFactories, factory)
	customFactoriesMu.Unlock()
}

// ClearRegistered 清空所有通过 [Register] 注册的工厂（仅用于测试）。
func ClearRegistered() {
	customFactoriesMu.Lock()
	customFactories = nil
	customFactoriesMu.Unlock()
}

// ============================================================================
// 自动检测工厂
// ============================================================================

// Adapt 自动检测并返回合适的 [ItemAdapter] 实现。
//
// 检测顺序：
//  1. nil → 返回 nil
//  2. item 自身实现 [ItemAdapter] 接口 → 直接返回（零开销包装）
//  3. 用户注册的自定义工厂（按注册逆序尝试）
//  4. key=string 的 map → [NewMapAdapter]
//  5. struct / *struct → [NewStructAdapter]
//  6. 其他类型 → 返回 nil
//
// 对应 Scrapy 的 `ItemAdapter(item)` 构造函数。
func Adapt(item any) ItemAdapter {
	if item == nil {
		return nil
	}

	// 1. 已实现接口：直接复用
	if a, ok := item.(ItemAdapter); ok {
		return a
	}

	// 2. 自定义工厂
	customFactoriesMu.RLock()
	factories := customFactories
	customFactoriesMu.RUnlock()
	for i := len(factories) - 1; i >= 0; i-- {
		if a := factories[i](item); a != nil {
			return a
		}
	}

	// 3. 内置自动检测
	switch kindOf(item) {
	case kindMap:
		return NewMapAdapter(item)
	case kindStruct:
		return NewStructAdapter(item)
	}
	return nil
}

// MustAdapt 与 [Adapt] 类似，但在无法适配时 panic。
//
// 常用于"调用方已通过 [IsItem] 保证类型正确"的场景。
func MustAdapt(item any) ItemAdapter {
	a := Adapt(item)
	if a == nil {
		panic(fmt.Errorf("%w: %T", ErrUnsupportedItem, item))
	}
	return a
}

// AsMap 是 `Adapt(item).AsMap()` 的便捷封装。
// 若 item 无法适配则返回 nil。
func AsMap(item any) map[string]any {
	a := Adapt(item)
	if a == nil {
		return nil
	}
	return a.AsMap()
}

// FieldNames 是 `Adapt(item).FieldNames()` 的便捷封装。
func FieldNames(item any) []string {
	a := Adapt(item)
	if a == nil {
		return nil
	}
	return a.FieldNames()
}
