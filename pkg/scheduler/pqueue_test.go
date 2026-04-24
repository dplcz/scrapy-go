package scheduler

import (
	"testing"

	scrapy_http "scrapy-go/pkg/http"
)

func TestPriorityQueueBasic(t *testing.T) {
	pq := NewPriorityQueue()

	// 空队列
	if pq.Len() != 0 {
		t.Error("new queue should be empty")
	}
	if pq.Pop() != nil {
		t.Error("pop from empty queue should return nil")
	}
	if pq.Peek() != nil {
		t.Error("peek from empty queue should return nil")
	}

	// 推入单个请求
	req := scrapy_http.MustNewRequest("https://example.com")
	pq.Push(req)
	if pq.Len() != 1 {
		t.Errorf("expected len 1, got %d", pq.Len())
	}

	// Peek 不弹出
	peeked := pq.Peek()
	if peeked != req {
		t.Error("peek should return the same request")
	}
	if pq.Len() != 1 {
		t.Error("peek should not remove the request")
	}

	// Pop 弹出
	popped := pq.Pop()
	if popped != req {
		t.Error("pop should return the same request")
	}
	if pq.Len() != 0 {
		t.Error("queue should be empty after pop")
	}
}

func TestPriorityQueuePriorityOrder(t *testing.T) {
	pq := NewPriorityQueue()

	// 推入不同优先级的请求
	low := scrapy_http.MustNewRequest("https://example.com/low", scrapy_http.WithPriority(1))
	mid := scrapy_http.MustNewRequest("https://example.com/mid", scrapy_http.WithPriority(5))
	high := scrapy_http.MustNewRequest("https://example.com/high", scrapy_http.WithPriority(10))

	// 按低→中→高顺序推入
	pq.Push(low)
	pq.Push(mid)
	pq.Push(high)

	// 应按高→中→低顺序弹出
	first := pq.Pop()
	if first.URL.Path != "/high" {
		t.Errorf("expected /high, got %s", first.URL.Path)
	}

	second := pq.Pop()
	if second.URL.Path != "/mid" {
		t.Errorf("expected /mid, got %s", second.URL.Path)
	}

	third := pq.Pop()
	if third.URL.Path != "/low" {
		t.Errorf("expected /low, got %s", third.URL.Path)
	}
}

func TestPriorityQueueNegativePriority(t *testing.T) {
	pq := NewPriorityQueue()

	neg := scrapy_http.MustNewRequest("https://example.com/neg", scrapy_http.WithPriority(-5))
	zero := scrapy_http.MustNewRequest("https://example.com/zero", scrapy_http.WithPriority(0))
	pos := scrapy_http.MustNewRequest("https://example.com/pos", scrapy_http.WithPriority(5))

	pq.Push(neg)
	pq.Push(zero)
	pq.Push(pos)

	// 正 > 零 > 负
	first := pq.Pop()
	if first.URL.Path != "/pos" {
		t.Errorf("expected /pos, got %s", first.URL.Path)
	}
	second := pq.Pop()
	if second.URL.Path != "/zero" {
		t.Errorf("expected /zero, got %s", second.URL.Path)
	}
	third := pq.Pop()
	if third.URL.Path != "/neg" {
		t.Errorf("expected /neg, got %s", third.URL.Path)
	}
}

func TestPriorityQueueLIFO(t *testing.T) {
	pq := NewPriorityQueue()

	// 相同优先级的请求应按 LIFO 顺序出队
	first := scrapy_http.MustNewRequest("https://example.com/first", scrapy_http.WithPriority(0))
	second := scrapy_http.MustNewRequest("https://example.com/second", scrapy_http.WithPriority(0))
	third := scrapy_http.MustNewRequest("https://example.com/third", scrapy_http.WithPriority(0))

	pq.Push(first)
	pq.Push(second)
	pq.Push(third)

	// LIFO: third → second → first
	out1 := pq.Pop()
	if out1.URL.Path != "/third" {
		t.Errorf("expected /third (LIFO), got %s", out1.URL.Path)
	}
	out2 := pq.Pop()
	if out2.URL.Path != "/second" {
		t.Errorf("expected /second (LIFO), got %s", out2.URL.Path)
	}
	out3 := pq.Pop()
	if out3.URL.Path != "/first" {
		t.Errorf("expected /first (LIFO), got %s", out3.URL.Path)
	}
}

func TestPriorityQueueMixed(t *testing.T) {
	pq := NewPriorityQueue()

	// 混合优先级和 LIFO
	a := scrapy_http.MustNewRequest("https://example.com/a", scrapy_http.WithPriority(1))
	b := scrapy_http.MustNewRequest("https://example.com/b", scrapy_http.WithPriority(2))
	c := scrapy_http.MustNewRequest("https://example.com/c", scrapy_http.WithPriority(1))
	d := scrapy_http.MustNewRequest("https://example.com/d", scrapy_http.WithPriority(2))

	pq.Push(a) // priority=1, seq=0
	pq.Push(b) // priority=2, seq=1
	pq.Push(c) // priority=1, seq=2
	pq.Push(d) // priority=2, seq=3

	// 先出优先级 2 的（LIFO: d → b），再出优先级 1 的（LIFO: c → a）
	out1 := pq.Pop()
	if out1.URL.Path != "/d" {
		t.Errorf("expected /d, got %s", out1.URL.Path)
	}
	out2 := pq.Pop()
	if out2.URL.Path != "/b" {
		t.Errorf("expected /b, got %s", out2.URL.Path)
	}
	out3 := pq.Pop()
	if out3.URL.Path != "/c" {
		t.Errorf("expected /c, got %s", out3.URL.Path)
	}
	out4 := pq.Pop()
	if out4.URL.Path != "/a" {
		t.Errorf("expected /a, got %s", out4.URL.Path)
	}
}

func TestPriorityQueueLargeScale(t *testing.T) {
	pq := NewPriorityQueue()

	n := 10000
	for i := 0; i < n; i++ {
		req := scrapy_http.MustNewRequest("https://example.com/page",
			scrapy_http.WithPriority(i % 10),
		)
		pq.Push(req)
	}

	if pq.Len() != n {
		t.Errorf("expected %d, got %d", n, pq.Len())
	}

	// 验证出队顺序：优先级递减
	lastPriority := 100
	for pq.Len() > 0 {
		req := pq.Pop()
		if req.Priority > lastPriority {
			t.Errorf("priority should be non-increasing: got %d after %d", req.Priority, lastPriority)
			break
		}
		lastPriority = req.Priority
	}
}
