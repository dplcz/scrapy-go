// Package scheduler 实现了 scrapy-go 框架的请求调度系统。
package scheduler

// Queue 定义了统一的队列接口，用于抽象内存队列和磁盘队列。
//
// 队列操作的是原始字节切片（[]byte），序列化/反序列化职责由调度器层负责。
// 这种设计替代了 Scrapy 中 queuelib 直接存储 Python 对象的方式，
// 更适合 Go 的静态类型系统和显式序列化风格。
//
// 对应 Scrapy 的 queuelib.BaseQueue 接口。
type Queue interface {
	// Push 将数据推入队列。
	Push(data []byte) error

	// Pop 从队列中弹出数据。
	// 如果队列为空，返回 nil, nil。
	Pop() ([]byte, error)

	// Peek 查看队列头部数据但不弹出。
	// 如果队列为空，返回 nil, nil。
	Peek() ([]byte, error)

	// Len 返回队列中的元素数量。
	Len() int

	// Close 关闭队列，释放资源。
	Close() error
}

// PriorityAwareQueue 是支持优先级感知的队列扩展接口。
//
// 在基础 Queue 接口之上增加了按优先级存取的能力，适用于需要按优先级
// 分桶存储的场景（如磁盘队列、Redis 分布式队列等）。
//
// 实现此接口的队列可以通过 WithExternalQueue 注入到 DefaultScheduler 中，
// 实现队列后端的无缝替换（如从磁盘队列切换到 Redis 队列）。
type PriorityAwareQueue interface {
	Queue

	// PushWithPriority 将数据推入指定优先级的桶中。
	PushWithPriority(data []byte, priority int) error

	// PopWithPriority 从最高优先级的桶中弹出数据。
	// 返回数据和对应的优先级。
	// 如果队列为空，返回 nil, 0, nil。
	PopWithPriority() (data []byte, priority int, err error)
}

// MemoryQueue 是基于内存的 LIFO 队列实现。
// 相同优先级的请求按 LIFO（后进先出）顺序出队，实现 DFS 爬取策略。
//
// 对应 Scrapy 的 LifoMemoryQueue。
type MemoryQueue struct {
	items [][]byte
}

// NewMemoryQueue 创建一个新的内存队列。
func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{
		items: make([][]byte, 0),
	}
}

// Push 将数据推入队列尾部。
func (q *MemoryQueue) Push(data []byte) error {
	q.items = append(q.items, data)
	return nil
}

// Pop 从队列尾部弹出数据（LIFO）。
func (q *MemoryQueue) Pop() ([]byte, error) {
	if len(q.items) == 0 {
		return nil, nil
	}
	n := len(q.items)
	data := q.items[n-1]
	q.items[n-1] = nil // 避免内存泄漏
	q.items = q.items[:n-1]
	return data, nil
}

// Peek 查看队列尾部数据但不弹出。
func (q *MemoryQueue) Peek() ([]byte, error) {
	if len(q.items) == 0 {
		return nil, nil
	}
	return q.items[len(q.items)-1], nil
}

// Len 返回队列中的元素数量。
func (q *MemoryQueue) Len() int {
	return len(q.items)
}

// Close 关闭队列。
func (q *MemoryQueue) Close() error {
	q.items = nil
	return nil
}
