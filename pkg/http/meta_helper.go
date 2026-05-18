package http

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrMetaKeyNotFound 表示 Meta 中不存在指定的 key。
var ErrMetaKeyNotFound = errors.New("meta: key not found")

// ErrMetaNil 表示 Meta 为 nil。
var ErrMetaNil = errors.New("meta: map is nil")

// ErrMetaConversion 表示 Meta 值无法转换为目标类型。
var ErrMetaConversion = errors.New("meta: type conversion failed")

// GetMetaAs 从 Response 关联的 Request.Meta 中获取指定 key 的值，
// 并尝试将其转换为类型 T。
//
// 转换策略（按优先级）：
//  1. 快路径：直接类型断言 — 如果值本身就是 T 类型（未经过磁盘序列化的内存请求），
//     零分配零开销，立即返回。
//  2. 慢路径：JSON 往返转换 — 如果直接断言失败（通常是磁盘队列反序列化后值变为
//     map[string]any），通过 json.Marshal + json.Unmarshal 将值转换为目标结构体。
//     单次约 ~2μs，仅在断点续爬恢复时触发。
//
// 用法：
//
//	type DetailItem struct {
//	    Title string `json:"title"`
//	    Price float64 `json:"price"`
//	}
//
//	// 在 ParseDetail 回调中恢复 Meta 中的结构体
//	item, err := http.GetMetaAs[DetailItem](response, "item")
//	if err != nil {
//	    return nil, err
//	}
//	fmt.Println(item.Title, item.Price)
func GetMetaAs[T any](resp *Response, key string) (T, error) {
	if resp == nil || resp.Request == nil {
		var zero T
		return zero, ErrMetaNil
	}
	return metaConvert[T](resp.Request.Meta, key)
}

// GetRequestMetaAs 从 Request.Meta 中获取指定 key 的值，
// 并尝试将其转换为类型 T。
//
// 与 GetMetaAs 共享核心转换逻辑（metaConvert），提供 Request 端的对称 API。
//
// 转换策略同 GetMetaAs：快路径直接类型断言，慢路径 JSON 往返转换。
//
// 用法：
//
//	type PageInfo struct {
//	    PageNum int    `json:"page_num"`
//	    NextURL string `json:"next_url"`
//	}
//
//	info, err := http.GetRequestMetaAs[PageInfo](request, "page_info")
//	if err != nil {
//	    return nil, err
//	}
func GetRequestMetaAs[T any](req *Request, key string) (T, error) {
	if req == nil {
		var zero T
		return zero, ErrMetaNil
	}
	return metaConvert[T](req.Meta, key)
}

// metaConvert 是 GetMetaAs 和 GetRequestMetaAs 的共享核心逻辑。
//
// 转换策略：
//  1. 检查 meta 是否为 nil → 返回 ErrMetaNil
//  2. 检查 key 是否存在 → 返回 ErrMetaKeyNotFound
//  3. 快路径：直接类型断言 → 成功则零开销返回
//  4. 慢路径：json.Marshal + json.Unmarshal → 兼容磁盘反序列化后的 map 形态
func metaConvert[T any](meta map[string]any, key string) (T, error) {
	var zero T

	if meta == nil {
		return zero, ErrMetaNil
	}

	v, ok := meta[key]
	if !ok {
		return zero, fmt.Errorf("%w: %q", ErrMetaKeyNotFound, key)
	}

	// 快路径：直接类型断言（零分配）
	if typed, ok := v.(T); ok {
		return typed, nil
	}

	// 慢路径：JSON 往返转换
	// 适用于磁盘队列反序列化后值变为 map[string]any 的场景
	data, err := json.Marshal(v)
	if err != nil {
		return zero, fmt.Errorf("%w: cannot marshal meta value for key %q: %v",
			ErrMetaConversion, key, err)
	}

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return zero, fmt.Errorf("%w: cannot unmarshal to %T for key %q: %v",
			ErrMetaConversion, zero, key, err)
	}

	return result, nil
}
