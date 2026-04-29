package scheduler

import (
	"testing"
)

func TestMemoryQueueBasic(t *testing.T) {
	q := NewMemoryQueue()

	// 空队列
	if q.Len() != 0 {
		t.Error("new queue should be empty")
	}
	data, err := q.Pop()
	if err != nil || data != nil {
		t.Error("pop from empty queue should return nil, nil")
	}
	data, err = q.Peek()
	if err != nil || data != nil {
		t.Error("peek from empty queue should return nil, nil")
	}

	// 推入数据
	if err := q.Push([]byte("hello")); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	if q.Len() != 1 {
		t.Errorf("expected len 1, got %d", q.Len())
	}

	// Peek 不弹出
	data, err = q.Peek()
	if err != nil {
		t.Fatalf("peek failed: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
	if q.Len() != 1 {
		t.Error("peek should not remove the item")
	}

	// Pop 弹出
	data, err = q.Pop()
	if err != nil {
		t.Fatalf("pop failed: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
	if q.Len() != 0 {
		t.Error("queue should be empty after pop")
	}
}

func TestMemoryQueueLIFO(t *testing.T) {
	q := NewMemoryQueue()

	q.Push([]byte("first"))
	q.Push([]byte("second"))
	q.Push([]byte("third"))

	// LIFO: third → second → first
	data, _ := q.Pop()
	if string(data) != "third" {
		t.Errorf("expected 'third', got %q", string(data))
	}
	data, _ = q.Pop()
	if string(data) != "second" {
		t.Errorf("expected 'second', got %q", string(data))
	}
	data, _ = q.Pop()
	if string(data) != "first" {
		t.Errorf("expected 'first', got %q", string(data))
	}
}

func TestMemoryQueueClose(t *testing.T) {
	q := NewMemoryQueue()
	q.Push([]byte("data"))

	if err := q.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

// TestQueueInterface 验证 MemoryQueue 实现了 Queue 接口
func TestQueueInterface(t *testing.T) {
	var _ Queue = (*MemoryQueue)(nil)
}
