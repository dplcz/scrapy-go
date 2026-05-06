// Package pool 提供 Request/Response 对象池，用于减少 GC 压力。
//
// 通过 sync.Pool 复用 HTTP 请求和响应对象，在高并发场景下
// 可以显著减少内存分配和 GC 开销。
//
// 设计决策：
//   - 作为可选优化，仅在 Benchmark 验证 GC 为瓶颈时启用
//   - 提供 Get/Put 接口，由调用方负责在使用完毕后归还对象
//   - Reset 方法确保归还的对象不会泄漏上一次请求的数据
//   - Go 特有优化，Scrapy 无此需求
package pool

import (
	"net/http"
	"net/url"
	"sync"
)

// RequestPool 是 HTTP 请求对象池。
// 通过复用 Request 对象减少内存分配。
var RequestPool = &requestPool{
	pool: sync.Pool{
		New: func() any {
			return &PooledRequest{
				Headers: make(http.Header),
				Meta:    make(map[string]any),
			}
		},
	},
}

// PooledRequest 是可池化的请求对象。
// 包含请求的核心字段，使用完毕后通过 Reset 清理并归还池。
type PooledRequest struct {
	URL      *url.URL
	Method   string
	Headers  http.Header
	Body     []byte
	Meta     map[string]any
	Priority int
}

// Reset 重置请求对象的所有字段，准备归还池。
func (r *PooledRequest) Reset() {
	r.URL = nil
	r.Method = ""
	r.Body = r.Body[:0]
	r.Priority = 0
	// 清理 Headers（保留底层 map 避免重新分配）
	for k := range r.Headers {
		delete(r.Headers, k)
	}
	// 清理 Meta（保留底层 map 避免重新分配）
	for k := range r.Meta {
		delete(r.Meta, k)
	}
}

type requestPool struct {
	pool sync.Pool
}

// Get 从池中获取一个请求对象。
func (p *requestPool) Get() *PooledRequest {
	return p.pool.Get().(*PooledRequest)
}

// Put 将请求对象归还池。
// 归还前会自动调用 Reset 清理数据。
func (p *requestPool) Put(r *PooledRequest) {
	r.Reset()
	p.pool.Put(r)
}

// ResponsePool 是 HTTP 响应对象池。
// 通过复用 Response 对象减少内存分配。
var ResponsePool = &responsePool{
	pool: sync.Pool{
		New: func() any {
			return &PooledResponse{
				Headers: make(http.Header),
			}
		},
	},
}

// PooledResponse 是可池化的响应对象。
// 包含响应的核心字段，使用完毕后通过 Reset 清理并归还池。
type PooledResponse struct {
	URL     *url.URL
	Status  int
	Headers http.Header
	Body    []byte
}

// Reset 重置响应对象的所有字段，准备归还池。
func (r *PooledResponse) Reset() {
	r.URL = nil
	r.Status = 0
	r.Body = r.Body[:0]
	// 清理 Headers（保留底层 map 避免重新分配）
	for k := range r.Headers {
		delete(r.Headers, k)
	}
}

type responsePool struct {
	pool sync.Pool
}

// Get 从池中获取一个响应对象。
func (p *responsePool) Get() *PooledResponse {
	return p.pool.Get().(*PooledResponse)
}

// Put 将响应对象归还池。
// 归还前会自动调用 Reset 清理数据。
func (p *responsePool) Put(r *PooledResponse) {
	r.Reset()
	p.pool.Put(r)
}

// BytesPool 是字节切片对象池。
// 用于复用下载响应体的缓冲区，减少大块内存的分配。
var BytesPool = &bytesPool{
	pool: sync.Pool{
		New: func() any {
			b := make([]byte, 0, 32*1024) // 默认 32KB 初始容量
			return &b
		},
	},
}

type bytesPool struct {
	pool sync.Pool
}

// Get 从池中获取一个字节切片。
func (p *bytesPool) Get() *[]byte {
	return p.pool.Get().(*[]byte)
}

// Put 将字节切片归还池。
// 归还前会重置长度为 0（保留底层数组容量）。
func (p *bytesPool) Put(b *[]byte) {
	*b = (*b)[:0]
	p.pool.Put(b)
}
