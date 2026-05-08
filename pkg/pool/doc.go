// Package pool 提供 HTTP 请求/响应对象池，用于减少 GC 压力。
//
// # 概述
//
// pool 包通过 sync.Pool 复用 HTTP 请求和响应对象，在高并发爬取场景下
// 可以显著减少内存分配和 GC 开销。这是 Go 特有的性能优化手段，
// Scrapy（Python）无此需求（CPython 使用引用计数 + 分代 GC）。
//
// # 设计决策
//
// 对象池作为可选优化存在，设计原则：
//   - 仅在 Benchmark 验证 GC 为瓶颈时启用
//   - 提供 Get/Put 接口，由调用方负责在使用完毕后归还对象
//   - Reset 方法确保归还的对象不会泄漏上一次请求的数据
//   - 保留底层数据结构容量（map、slice），避免重新分配
//
// # 提供的对象池
//
// [RequestPool]：HTTP 请求对象池
//   - 池化 [PooledRequest] 对象（URL、Method、Headers、Body、Meta、Priority）
//   - Get() 获取对象，Put() 归还并自动 Reset
//   - Headers 和 Meta 的底层 map 在 Reset 时保留容量
//
// [ResponsePool]：HTTP 响应对象池
//   - 池化 [PooledResponse] 对象（URL、Status、Headers、Body）
//   - Get() 获取对象，Put() 归还并自动 Reset
//
// [BytesPool]：字节切片对象池
//   - 池化 []byte 切片（默认 32KB 初始容量）
//   - 用于复用下载响应体的缓冲区，减少大块内存分配
//   - Put 时重置长度为 0，保留底层数组容量
//
// # 使用模式
//
//	// 获取请求对象
//	req := pool.RequestPool.Get()
//	req.URL = parsedURL
//	req.Method = "GET"
//	req.Headers.Set("User-Agent", "scrapy-go")
//
//	// 使用完毕后归还
//	defer pool.RequestPool.Put(req)
//
//	// 获取字节缓冲区
//	buf := pool.BytesPool.Get()
//	defer pool.BytesPool.Put(buf)
//	*buf = append(*buf, responseBody...)
//
// # 注意事项
//
//   - 归还后不要再使用对象（数据已被 Reset 清空）
//   - sync.Pool 中的对象可能在任意时刻被 GC 回收（非确定性缓存）
//   - 对象池适合短生命周期、高频创建的对象
//   - 不适合需要长期持有引用的场景
//
// # 性能收益
//
// 在 100k 请求的 Benchmark 中，对象池可以：
//   - 减少约 30% 的堆内存分配
//   - 降低 GC 暂停时间
//   - 减少每个请求的 allocs/op
//
// # 并发安全
//
// sync.Pool 本身是并发安全的，所有 Get/Put 操作可以被多个 goroutine 安全调用。
package pool
