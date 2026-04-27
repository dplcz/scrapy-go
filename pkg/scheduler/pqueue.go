package scheduler

import (
	"container/heap"

	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
)

// ============================================================================
// PriorityQueue 优先级队列
// ============================================================================

// PriorityQueue 是基于 container/heap 的优先级队列。
// 高优先级（Priority 值越大）的请求先出队。
// 相同优先级的请求按 LIFO（后进先出）顺序出队，实现 DFS 爬取策略。
//
// 对应 Scrapy 的 ScrapyPriorityQueue + LifoMemoryQueue 组合。
type PriorityQueue struct {
	items *requestHeap
}

// NewPriorityQueue 创建一个新的优先级队列。
func NewPriorityQueue() *PriorityQueue {
	h := &requestHeap{}
	heap.Init(h)
	return &PriorityQueue{items: h}
}

// Push 将请求推入队列。
func (pq *PriorityQueue) Push(request *scrapy_http.Request) {
	heap.Push(pq.items, &requestEntry{
		request: request,
		index:   0,
		seq:     pq.items.seq,
	})
	pq.items.seq++
}

// Pop 从队列中弹出优先级最高的请求。
// 如果队列为空，返回 nil。
func (pq *PriorityQueue) Pop() *scrapy_http.Request {
	if pq.items.Len() == 0 {
		return nil
	}
	entry := heap.Pop(pq.items).(*requestEntry)
	return entry.request
}

// Peek 查看队列中优先级最高的请求，但不弹出。
// 如果队列为空，返回 nil。
func (pq *PriorityQueue) Peek() *scrapy_http.Request {
	if pq.items.Len() == 0 {
		return nil
	}
	return pq.items.entries[0].request
}

// Len 返回队列中的请求数量。
func (pq *PriorityQueue) Len() int {
	return pq.items.Len()
}

// ============================================================================
// requestEntry 和 requestHeap（heap.Interface 实现）
// ============================================================================

// requestEntry 是优先级队列中的条目。
type requestEntry struct {
	request *scrapy_http.Request
	index   int    // 在 heap 中的索引
	seq     uint64 // 入队序号，用于相同优先级时的 LIFO 排序
}

// requestHeap 实现 heap.Interface。
type requestHeap struct {
	entries []*requestEntry
	seq     uint64 // 全局序号计数器
}

// 为了让 requestHeap 可以直接被当作 []*requestEntry 使用，
// 我们让 PriorityQueue 持有 *requestHeap 指针。

func (h *requestHeap) Len() int {
	return len(h.entries)
}

// Less 定义排序规则：
//  1. 优先级高的排前面（Priority 值越大越优先）
//  2. 相同优先级时，后入队的排前面（LIFO，seq 越大越优先）
func (h *requestHeap) Less(i, j int) bool {
	pi := h.entries[i].request.Priority
	pj := h.entries[j].request.Priority

	if pi != pj {
		return pi > pj // 高优先级在前
	}
	// 相同优先级，LIFO：后入队的先出
	return h.entries[i].seq > h.entries[j].seq
}

func (h *requestHeap) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
	h.entries[i].index = i
	h.entries[j].index = j
}

func (h *requestHeap) Push(x any) {
	entry := x.(*requestEntry)
	entry.index = len(h.entries)
	h.entries = append(h.entries, entry)
}

func (h *requestHeap) Pop() any {
	old := h.entries
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil // 避免内存泄漏
	entry.index = -1
	h.entries = old[:n-1]
	return entry
}
