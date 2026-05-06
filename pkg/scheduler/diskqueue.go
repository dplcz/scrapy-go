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
//   - 每次 Push/Pop 操作立即写盘，确保进程异常退出时数据不丢失
//   - 无需手动 Flush，Close 仅做清理工作
//   - sortedPriorities 维护有序切片，Push 时二分插入 O(log N)，
//     Pop 取最高优先级 O(1)，避免每次重新分配和排序
//
// 对应 Scrapy 的 LifoDiskQueue（通过 queuelib 实现）。
type DiskQueue struct {
	mu      sync.Mutex
	dir     string          // 队列数据目录
	buckets map[int]*bucket // 按优先级分桶
	count   int             // 总元素数量

	// priorities 维护按优先级从高到低排序的有序切片。
	// Push 新优先级时二分插入 O(log N)，删除空桶时二分移除 O(log N)，
	// Pop 取最高优先级 O(1)。
	// 替代原有的每次 Pop/Peek 重新分配切片 + O(N log N) 排序。
	priorities []int
}

// bucket 是单个优先级的数据桶。
type bucket struct {
	priority int
	items    []json.RawMessage
}

// NewDiskQueue 创建一个新的磁盘队列。
// dir 是队列数据存储目录，如果目录不存在会自动创建。
// 如果目录中已有数据，会自动加载（用于断点续爬）。
func NewDiskQueue(dir string) (*DiskQueue, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create disk queue directory %q: %w", dir, err)
	}

	dq := &DiskQueue{
		dir:        dir,
		buckets:    make(map[int]*bucket),
		priorities: make([]int, 0, 8),
	}

	// 加载已有数据
	if err := dq.load(); err != nil {
		return nil, fmt.Errorf("failed to load disk queue from %q: %w", dir, err)
	}

	return dq, nil
}

// PushWithPriority 将数据推入指定优先级的桶中。
// 每次 Push 操作立即将变更持久化到磁盘，确保数据安全。
func (dq *DiskQueue) PushWithPriority(data []byte, priority int) error {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	b, ok := dq.buckets[priority]
	if !ok {
		b = &bucket{priority: priority, items: make([]json.RawMessage, 0)}
		dq.buckets[priority] = b
		// 二分插入新优先级到有序切片（从高到低）
		dq.insertPriority(priority)
	}

	b.items = append(b.items, data)
	dq.count++

	// 立即持久化变更的桶和状态
	if err := dq.persistBucketAndState(b); err != nil {
		// 回滚内存状态
		b.items = b.items[:len(b.items)-1]
		dq.count--
		if len(b.items) == 0 {
			delete(dq.buckets, priority)
			dq.removePriority(priority)
		}
		return fmt.Errorf("failed to persist after push: %w", err)
	}

	return nil
}

// Push 将数据推入默认优先级（0）的桶中。
// 实现 Queue 接口。
func (dq *DiskQueue) Push(data []byte) error {
	return dq.PushWithPriority(data, 0)
}

// PopWithPriority 从最高优先级的桶中弹出数据（LIFO）。
// 每次 Pop 操作立即将变更持久化到磁盘，确保数据安全。
// 返回数据和对应的优先级。
func (dq *DiskQueue) PopWithPriority() ([]byte, int, error) {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	if dq.count == 0 {
		return nil, 0, nil
	}

	// 直接从有序切片取最高优先级 O(1)
	for i := 0; i < len(dq.priorities); i++ {
		p := dq.priorities[i]
		b := dq.buckets[p]
		if len(b.items) == 0 {
			continue
		}

		// LIFO: 从尾部弹出
		n := len(b.items)
		data := b.items[n-1]
		b.items[n-1] = nil // 避免内存泄漏
		b.items = b.items[:n-1]
		dq.count--

		bucketRemoved := false
		// 清理空桶
		if len(b.items) == 0 {
			delete(dq.buckets, p)
			dq.removePriority(p)
			bucketRemoved = true
		}

		// 立即持久化变更
		if err := dq.persistAfterPop(b, bucketRemoved); err != nil {
			// 回滚内存状态
			if bucketRemoved {
				dq.buckets[p] = b
				dq.insertPriority(p)
			}
			b.items = append(b.items, data)
			dq.count++
			return nil, 0, fmt.Errorf("failed to persist after pop: %w", err)
		}

		return data, p, nil
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

	// 直接从有序切片取最高优先级 O(1)
	for _, p := range dq.priorities {
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

// Close 关闭队列。
// 由于每次 Push/Pop 都已立即持久化，Close 仅做最终的清理工作。
func (dq *DiskQueue) Close() error {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	// 清理可能残留的空桶文件
	return dq.cleanupBucketFiles()
}

// ============================================================================
// 有序优先级切片维护方法
// ============================================================================

// insertPriority 将优先级插入有序切片（从高到低排序）。
// 使用二分查找确定插入位置，时间复杂度 O(log N)。
func (dq *DiskQueue) insertPriority(priority int) {
	// 二分查找插入位置（降序排列）
	idx := sort.Search(len(dq.priorities), func(i int) bool {
		return dq.priorities[i] < priority
	})
	// 在 idx 位置插入
	dq.priorities = append(dq.priorities, 0)
	copy(dq.priorities[idx+1:], dq.priorities[idx:])
	dq.priorities[idx] = priority
}

// removePriority 从有序切片中移除指定优先级。
// 使用二分查找定位，时间复杂度 O(log N)。
func (dq *DiskQueue) removePriority(priority int) {
	// 二分查找目标位置（降序排列）
	idx := sort.Search(len(dq.priorities), func(i int) bool {
		return dq.priorities[i] < priority
	})
	// 向前检查是否找到
	if idx > 0 && dq.priorities[idx-1] == priority {
		idx = idx - 1
	} else if idx < len(dq.priorities) && dq.priorities[idx] == priority {
		// 找到了
	} else {
		return // 未找到
	}
	// 移除
	dq.priorities = append(dq.priorities[:idx], dq.priorities[idx+1:]...)
}

// rebuildPriorities 从 buckets 重建有序优先级切片。
// 仅在 load 时调用一次。
func (dq *DiskQueue) rebuildPriorities() {
	dq.priorities = make([]int, 0, len(dq.buckets))
	for p := range dq.buckets {
		dq.priorities = append(dq.priorities, p)
	}
	// 降序排列
	sort.Sort(sort.Reverse(sort.IntSlice(dq.priorities)))
}

// ============================================================================
// 内部方法
// ============================================================================

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
			}
			dq.count += len(items)
		}
	}

	// 重建有序优先级切片
	dq.rebuildPriorities()

	return nil
}

// persistBucketAndState 将指定桶和状态文件持久化到磁盘。
// 用于 Push 操作后的立即写盘。
func (dq *DiskQueue) persistBucketAndState(b *bucket) error {
	if err := dq.writeBucket(b); err != nil {
		return err
	}
	return dq.writeCurrentState()
}

// persistAfterPop 在 Pop 操作后持久化变更。
// 如果桶已被移除，还需要清理对应的桶文件。
func (dq *DiskQueue) persistAfterPop(b *bucket, bucketRemoved bool) error {
	if bucketRemoved {
		// 桶已空，删除桶文件
		filename := filepath.Join(dq.dir, fmt.Sprintf("p%d.json", b.priority))
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove empty bucket file: %w", err)
		}
	} else {
		if err := dq.writeBucket(b); err != nil {
			return err
		}
	}
	return dq.writeCurrentState()
}

// writeCurrentState 根据当前内存中的 buckets 写入状态文件。
func (dq *DiskQueue) writeCurrentState() error {
	state := diskQueueState{
		Version: 1,
		Buckets: make([]bucketMeta, 0, len(dq.buckets)),
	}
	for p, b := range dq.buckets {
		state.Buckets = append(state.Buckets, bucketMeta{
			Priority: p,
			Count:    len(b.items),
		})
	}
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
