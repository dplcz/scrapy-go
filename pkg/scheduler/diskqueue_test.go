package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDiskQueueBasic(t *testing.T) {
	dir := t.TempDir()

	dq, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}
	defer dq.Close()

	// 空队列
	if dq.Len() != 0 {
		t.Error("new disk queue should be empty")
	}
	data, err := dq.Pop()
	if err != nil || data != nil {
		t.Error("pop from empty disk queue should return nil, nil")
	}

	// 推入数据
	if err := dq.Push([]byte(`{"url":"https://example.com"}`)); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	if dq.Len() != 1 {
		t.Errorf("expected len 1, got %d", dq.Len())
	}

	// Peek
	data, err = dq.Peek()
	if err != nil {
		t.Fatalf("peek failed: %v", err)
	}
	if data == nil {
		t.Fatal("peek should return data")
	}
	if dq.Len() != 1 {
		t.Error("peek should not remove the item")
	}

	// Pop
	data, err = dq.Pop()
	if err != nil {
		t.Fatalf("pop failed: %v", err)
	}
	if data == nil {
		t.Fatal("pop should return data")
	}
	if dq.Len() != 0 {
		t.Error("queue should be empty after pop")
	}
}

func TestDiskQueuePriority(t *testing.T) {
	dir := t.TempDir()

	dq, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}
	defer dq.Close()

	// 推入不同优先级的数据
	dq.PushWithPriority([]byte(`{"url":"low"}`), 1)
	dq.PushWithPriority([]byte(`{"url":"high"}`), 10)
	dq.PushWithPriority([]byte(`{"url":"mid"}`), 5)

	if dq.Len() != 3 {
		t.Errorf("expected len 3, got %d", dq.Len())
	}

	// 应按优先级从高到低出队
	data, priority, err := dq.PopWithPriority()
	if err != nil {
		t.Fatalf("pop failed: %v", err)
	}
	if priority != 10 {
		t.Errorf("expected priority 10, got %d", priority)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	if m["url"] != "high" {
		t.Errorf("expected 'high', got %v", m["url"])
	}

	data, priority, _ = dq.PopWithPriority()
	if priority != 5 {
		t.Errorf("expected priority 5, got %d", priority)
	}

	data, priority, _ = dq.PopWithPriority()
	if priority != 1 {
		t.Errorf("expected priority 1, got %d", priority)
	}
}

func TestDiskQueueLIFO(t *testing.T) {
	dir := t.TempDir()

	dq, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}
	defer dq.Close()

	// 相同优先级，LIFO 顺序
	dq.PushWithPriority([]byte(`"first"`), 0)
	dq.PushWithPriority([]byte(`"second"`), 0)
	dq.PushWithPriority([]byte(`"third"`), 0)

	data, _ := dq.Pop()
	if string(data) != `"third"` {
		t.Errorf("expected 'third', got %q", string(data))
	}
	data, _ = dq.Pop()
	if string(data) != `"second"` {
		t.Errorf("expected 'second', got %q", string(data))
	}
	data, _ = dq.Pop()
	if string(data) != `"first"` {
		t.Errorf("expected 'first', got %q", string(data))
	}
}

func TestDiskQueuePersistence(t *testing.T) {
	dir := t.TempDir()

	// 第一次：写入数据并关闭
	{
		dq, err := NewDiskQueue(dir)
		if err != nil {
			t.Fatalf("failed to create disk queue: %v", err)
		}

		dq.PushWithPriority([]byte(`{"url":"https://example.com/1"}`), 1)
		dq.PushWithPriority([]byte(`{"url":"https://example.com/2"}`), 2)
		dq.PushWithPriority([]byte(`{"url":"https://example.com/3"}`), 1)

		if err := dq.Close(); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}

	// 验证文件存在
	stateFile := filepath.Join(dir, "state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Fatal("state.json should exist after close")
	}

	// 第二次：重新打开并验证数据
	{
		dq, err := NewDiskQueue(dir)
		if err != nil {
			t.Fatalf("failed to reopen disk queue: %v", err)
		}
		defer dq.Close()

		if dq.Len() != 3 {
			t.Errorf("expected 3 items after reopen, got %d", dq.Len())
		}

		// 优先级 2 的先出队
		data, priority, _ := dq.PopWithPriority()
		if priority != 2 {
			t.Errorf("expected priority 2, got %d", priority)
		}
		var m map[string]any
		json.Unmarshal(data, &m)
		if m["url"] != "https://example.com/2" {
			t.Errorf("expected url /2, got %v", m["url"])
		}

		// 优先级 1 的按 LIFO 出队
		data, priority, _ = dq.PopWithPriority()
		if priority != 1 {
			t.Errorf("expected priority 1, got %d", priority)
		}
		json.Unmarshal(data, &m)
		if m["url"] != "https://example.com/3" {
			t.Errorf("expected url /3 (LIFO), got %v", m["url"])
		}

		data, priority, _ = dq.PopWithPriority()
		if priority != 1 {
			t.Errorf("expected priority 1, got %d", priority)
		}
		json.Unmarshal(data, &m)
		if m["url"] != "https://example.com/1" {
			t.Errorf("expected url /1, got %v", m["url"])
		}
	}
}

func TestDiskQueueEmptyClose(t *testing.T) {
	dir := t.TempDir()

	dq, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}

	// 空队列关闭不应出错
	if err := dq.Close(); err != nil {
		t.Fatalf("close empty queue failed: %v", err)
	}
}

func TestDiskQueuePartialConsume(t *testing.T) {
	dir := t.TempDir()

	// 写入 5 个请求
	{
		dq, err := NewDiskQueue(dir)
		if err != nil {
			t.Fatalf("failed to create disk queue: %v", err)
		}

		for i := 0; i < 5; i++ {
			dq.Push([]byte(`{"url":"https://example.com/` + string(rune('a'+i)) + `"}`))
		}

		// 消费 2 个
		dq.Pop()
		dq.Pop()

		if err := dq.Close(); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}

	// 重新打开，应有 3 个剩余
	{
		dq, err := NewDiskQueue(dir)
		if err != nil {
			t.Fatalf("failed to reopen disk queue: %v", err)
		}
		defer dq.Close()

		if dq.Len() != 3 {
			t.Errorf("expected 3 remaining items, got %d", dq.Len())
		}
	}
}

func TestDiskQueueNegativePriority(t *testing.T) {
	dir := t.TempDir()

	dq, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}
	defer dq.Close()

	dq.PushWithPriority([]byte(`"neg"`), -5)
	dq.PushWithPriority([]byte(`"zero"`), 0)
	dq.PushWithPriority([]byte(`"pos"`), 5)

	// 正 > 零 > 负
	data, priority, _ := dq.PopWithPriority()
	if priority != 5 {
		t.Errorf("expected priority 5, got %d", priority)
	}
	if string(data) != `"pos"` {
		t.Errorf("expected 'pos', got %q", string(data))
	}

	data, priority, _ = dq.PopWithPriority()
	if priority != 0 {
		t.Errorf("expected priority 0, got %d", priority)
	}

	data, priority, _ = dq.PopWithPriority()
	if priority != -5 {
		t.Errorf("expected priority -5, got %d", priority)
	}
}

func TestDiskQueueFlush(t *testing.T) {
	dir := t.TempDir()

	dq, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}
	defer dq.Close()

	dq.Push([]byte(`"test"`))

	// 手动 Flush
	if err := dq.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	// 验证文件存在
	stateFile := filepath.Join(dir, "state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Fatal("state.json should exist after flush")
	}
}

// TestDiskQueueInterface 验证 DiskQueue 实现了 Queue 和 PriorityAwareQueue 接口
func TestDiskQueueInterface(t *testing.T) {
	var _ Queue = (*DiskQueue)(nil)
	var _ PriorityAwareQueue = (*DiskQueue)(nil)
}

func TestDiskQueueCleanupBucketFiles(t *testing.T) {
	dir := t.TempDir()

	// 创建队列并写入多个优先级
	dq, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}

	dq.PushWithPriority([]byte(`"a"`), 1)
	dq.PushWithPriority([]byte(`"b"`), 2)
	dq.PushWithPriority([]byte(`"c"`), 3)

	// 关闭以持久化
	dq.Close()

	// 重新打开并消费所有优先级 3 的数据
	dq2, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}

	// 弹出优先级 3
	dq2.PopWithPriority()

	// 关闭应清理 p3.json
	dq2.Close()

	// 验证 p3.json 被清理
	if _, err := os.Stat(filepath.Join(dir, "p3.json")); !os.IsNotExist(err) {
		t.Error("p3.json should be cleaned up after all items consumed")
	}
}

func TestDiskQueueMultiplePrioritySameBucket(t *testing.T) {
	dir := t.TempDir()

	dq, err := NewDiskQueue(dir)
	if err != nil {
		t.Fatalf("failed to create disk queue: %v", err)
	}
	defer dq.Close()

	// 同一优先级多个元素
	for i := 0; i < 10; i++ {
		dq.PushWithPriority([]byte(fmt.Sprintf(`{"id":%d}`, i)), 5)
	}

	if dq.Len() != 10 {
		t.Errorf("expected 10, got %d", dq.Len())
	}

	// LIFO 出队
	data, _, _ := dq.PopWithPriority()
	var m map[string]any
	json.Unmarshal(data, &m)
	if m["id"] != float64(9) {
		t.Errorf("expected id=9 (LIFO), got %v", m["id"])
	}
}

func TestDiskQueueInvalidDir(t *testing.T) {
	// 尝试在不可写的路径创建队列
	_, err := NewDiskQueue("/proc/nonexistent/queue")
	if err == nil {
		t.Error("should fail on invalid directory")
	}
}