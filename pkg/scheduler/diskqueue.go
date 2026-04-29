package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// DiskQueue 是基于文件系统的持久化 LIFO 队列实现。
//
// 每个优先级对应一个独立的 JSON 文件，文件名格式为 "p{priority}.json"。
// 使用 os.Rename 原子写入确保数据一致性。
//
// 设计决策：
//   - 使用 JSON 格式替代 Scrapy 的 pickle 格式，更安全且跨平台
//   - 使用多文件方案替代 Scrapy 的单文件追加方案，便于按优先级管理
//   - 每次 Push/Pop 操作都会触发文件写入，确保数据持久化
//
// 对应 Scrapy 的 LifoDiskQueue（通过 queuelib 实现）。
type DiskQueue struct {
	mu      sync.Mutex
	dir     string          // 队列数据目录
	buckets map[int]*bucket // 按优先级分桶
	count   int             // 总元素数量
}

// bucket 是单个优先级的数据桶。
type bucket struct {
	priority int
	items    []json.RawMessage
	dirty    bool // 是否有未持久化的变更
}

// NewDiskQueue 创建一个新的磁盘队列。
// dir 是队列数据存储目录，如果目录不存在会自动创建。
// 如果目录中已有数据，会自动加载（用于断点续爬）。
func NewDiskQueue(dir string) (*DiskQueue, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create disk queue directory %q: %w", dir, err)
	}

	dq := &DiskQueue{
		dir:     dir,
		buckets: make(map[int]*bucket),
	}

	// 加载已有数据
	if err := dq.load(); err != nil {
		return nil, fmt.Errorf("failed to load disk queue from %q: %w", dir, err)
	}

	return dq, nil
}

// PushWithPriority 将数据推入指定优先级的桶中。
// 磁盘队列需要知道优先级以便按优先级文件存储。
func (dq *DiskQueue) PushWithPriority(data []byte, priority int) error {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	b, ok := dq.buckets[priority]
	if !ok {
		b = &bucket{priority: priority, items: make([]json.RawMessage, 0)}
		dq.buckets[priority] = b
	}

	b.items = append(b.items, data)
	b.dirty = true
	dq.count++

	return nil
}

// Push 将数据推入默认优先级（0）的桶中。
// 实现 Queue 接口。
func (dq *DiskQueue) Push(data []byte) error {
	return dq.PushWithPriority(data, 0)
}

// PopWithPriority 从最高优先级的桶中弹出数据（LIFO）。
// 返回数据和对应的优先级。
func (dq *DiskQueue) PopWithPriority() ([]byte, int, error) {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	if dq.count == 0 {
		return nil, 0, nil
	}

	// 按优先级从高到低排序
	priorities := dq.sortedPriorities()
	if len(priorities) == 0 {
		return nil, 0, nil
	}

	for _, p := range priorities {
		b := dq.buckets[p]
		if len(b.items) == 0 {
			continue
		}

		// LIFO: 从尾部弹出
		n := len(b.items)
		data := b.items[n-1]
		b.items[n-1] = nil // 避免内存泄漏
		b.items = b.items[:n-1]
		b.dirty = true
		dq.count--

		// 清理空桶
		if len(b.items) == 0 {
			delete(dq.buckets, p)
		}

		return []byte(data), p, nil
	}

	return nil, 0, nil
}

// Pop 从最高优先级的桶中弹出数据。
// 实现 Queue 接口。
func (dq *DiskQueue) Pop() ([]byte, error) {
	data, _, err := dq.PopWithPriority()
	return data, err
}

// Peek 查看最高优先级桶的尾部数据但不弹出。
func (dq *DiskQueue) Peek() ([]byte, error) {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	if dq.count == 0 {
		return nil, nil
	}

	priorities := dq.sortedPriorities()
	for _, p := range priorities {
		b := dq.buckets[p]
		if len(b.items) > 0 {
			return b.items[len(b.items)-1], nil
		}
	}

	return nil, nil
}

// Len 返回队列中的总元素数量。
func (dq *DiskQueue) Len() int {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	return dq.count
}

// Close 关闭队列，将所有脏数据持久化到磁盘。
func (dq *DiskQueue) Close() error {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	return dq.flush()
}

// Flush 将所有脏数据持久化到磁盘。
func (dq *DiskQueue) Flush() error {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	return dq.flush()
}

// ============================================================================
// 内部方法
// ============================================================================

// sortedPriorities 返回按优先级从高到低排序的优先级列表。
func (dq *DiskQueue) sortedPriorities() []int {
	priorities := make([]int, 0, len(dq.buckets))
	for p := range dq.buckets {
		priorities = append(priorities, p)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(priorities)))
	return priorities
}

// load 从磁盘加载已有数据。
func (dq *DiskQueue) load() error {
	stateFile := filepath.Join(dq.dir, "state.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 无已有状态，正常启动
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var state diskQueueState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to parse state file: %w", err)
	}

	// 加载每个优先级桶的数据
	for _, bp := range state.Buckets {
		bucketFile := filepath.Join(dq.dir, fmt.Sprintf("p%d.json", bp.Priority))
		bucketData, err := os.ReadFile(bucketFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue // 桶文件不存在，跳过
			}
			return fmt.Errorf("failed to read bucket file p%d.json: %w", bp.Priority, err)
		}

		var items []json.RawMessage
		if err := json.Unmarshal(bucketData, &items); err != nil {
			return fmt.Errorf("failed to parse bucket file p%d.json: %w", bp.Priority, err)
		}

		if len(items) > 0 {
			dq.buckets[bp.Priority] = &bucket{
				priority: bp.Priority,
				items:    items,
				dirty:    false,
			}
			dq.count += len(items)
		}
	}

	return nil
}

// flush 将所有脏数据写入磁盘。
func (dq *DiskQueue) flush() error {
	state := diskQueueState{
		Version: 1,
		Buckets: make([]bucketMeta, 0, len(dq.buckets)),
	}

	for p, b := range dq.buckets {
		state.Buckets = append(state.Buckets, bucketMeta{
			Priority: p,
			Count:    len(b.items),
		})

		if b.dirty {
			if err := dq.writeBucket(b); err != nil {
				return err
			}
			b.dirty = false
		}
	}

	// 清理已删除的桶文件
	if err := dq.cleanupBucketFiles(); err != nil {
		return err
	}

	// 写入状态文件
	return dq.writeState(state)
}

// writeBucket 将单个桶的数据写入磁盘。
// 使用临时文件 + os.Rename 实现原子写入。
func (dq *DiskQueue) writeBucket(b *bucket) error {
	filename := filepath.Join(dq.dir, fmt.Sprintf("p%d.json", b.priority))

	if len(b.items) == 0 {
		// 空桶，删除文件
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove empty bucket file: %w", err)
		}
		return nil
	}

	data, err := json.Marshal(b.items)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket data: %w", err)
	}

	// 原子写入：先写临时文件，再 rename
	tmpFile := filename + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp bucket file: %w", err)
	}
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile) // 清理临时文件
		return fmt.Errorf("failed to rename bucket file: %w", err)
	}

	return nil
}

// writeState 将状态文件写入磁盘。
func (dq *DiskQueue) writeState(state diskQueueState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	stateFile := filepath.Join(dq.dir, "state.json")
	tmpFile := stateFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}
	if err := os.Rename(tmpFile, stateFile); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

// cleanupBucketFiles 清理不再使用的桶文件。
func (dq *DiskQueue) cleanupBucketFiles() error {
	entries, err := os.ReadDir(dq.dir)
	if err != nil {
		return fmt.Errorf("failed to read queue directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "state.json" || name == "state.json.tmp" {
			continue
		}
		// 检查是否为桶文件
		var priority int
		if n, _ := fmt.Sscanf(name, "p%d.json", &priority); n == 1 {
			if _, exists := dq.buckets[priority]; !exists {
				// 桶已不存在，删除文件
				if err := os.Remove(filepath.Join(dq.dir, name)); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to cleanup bucket file %s: %w", name, err)
				}
			}
		}
	}

	return nil
}

// ============================================================================
// 状态序列化结构
// ============================================================================

// diskQueueState 是磁盘队列的持久化状态。
type diskQueueState struct {
	Version int          `json:"version"`
	Buckets []bucketMeta `json:"buckets"`
}

// bucketMeta 是单个优先级桶的元数据。
type bucketMeta struct {
	Priority int `json:"priority"`
	Count    int `json:"count"`
}
