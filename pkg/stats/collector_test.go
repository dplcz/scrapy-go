package stats

import (
	"sync"
	"testing"
)

func TestMemoryStatsCollector(t *testing.T) {
	c := NewMemoryStatsCollector(false, nil)

	// 测试 SetValue / GetValue
	c.SetValue("key1", "value1")
	if c.GetValue("key1", nil) != "value1" {
		t.Error("unexpected value")
	}

	// 测试不存在的 key 返回默认值
	if c.GetValue("missing", "default") != "default" {
		t.Error("should return default for missing key")
	}

	// 测试 IncValue
	c.IncValue("counter", 1, 0)
	if c.GetValue("counter", 0) != 1 {
		t.Errorf("unexpected counter value: %v", c.GetValue("counter", 0))
	}
	c.IncValue("counter", 5, 0)
	if c.GetValue("counter", 0) != 6 {
		t.Errorf("unexpected counter value: %v", c.GetValue("counter", 0))
	}

	// 测试 IncValue 从指定 start 开始
	c.IncValue("new_counter", 1, 100)
	if c.GetValue("new_counter", 0) != 101 {
		t.Errorf("unexpected counter value: %v", c.GetValue("new_counter", 0))
	}

	// 测试 MaxValue
	c.MaxValue("max_key", 10)
	if c.GetValue("max_key", 0) != 10 {
		t.Error("unexpected max value")
	}
	c.MaxValue("max_key", 5)
	if c.GetValue("max_key", 0) != 10 {
		t.Error("max should not decrease")
	}
	c.MaxValue("max_key", 20)
	if c.GetValue("max_key", 0) != 20 {
		t.Error("max should increase")
	}

	// 测试 MinValue
	c.MinValue("min_key", 10)
	if c.GetValue("min_key", 0) != 10 {
		t.Error("unexpected min value")
	}
	c.MinValue("min_key", 20)
	if c.GetValue("min_key", 0) != 10 {
		t.Error("min should not increase")
	}
	c.MinValue("min_key", 5)
	if c.GetValue("min_key", 0) != 5 {
		t.Error("min should decrease")
	}

	// 测试 GetStats
	stats := c.GetStats()
	if len(stats) == 0 {
		t.Error("stats should not be empty")
	}

	// 测试 ClearStats
	c.ClearStats()
	if len(c.GetStats()) != 0 {
		t.Error("stats should be empty after clear")
	}
}

func TestMemoryStatsCollectorConcurrency(t *testing.T) {
	c := NewMemoryStatsCollector(false, nil)

	var wg sync.WaitGroup
	n := 1000

	// 并发递增
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.IncValue("concurrent_counter", 1, 0)
		}()
	}
	wg.Wait()

	val := c.GetValue("concurrent_counter", 0)
	if val != n {
		t.Errorf("expected %d, got %v", n, val)
	}
}

func TestDummyStatsCollector(t *testing.T) {
	c := NewDummyStatsCollector()

	// 所有操作都是空操作
	c.SetValue("key", "value")
	if c.GetValue("key", "default") != "default" {
		t.Error("dummy collector should always return default")
	}

	c.IncValue("counter", 1, 0)
	if c.GetValue("counter", 0) != 0 {
		t.Error("dummy collector should not store values")
	}

	stats := c.GetStats()
	if len(stats) != 0 {
		t.Error("dummy collector should return empty stats")
	}

	// 不应 panic
	c.Open()
	c.Close("finished")
	c.ClearStats()
	c.MaxValue("key", 10)
	c.MinValue("key", 10)
	c.SetStats(map[string]any{"key": "value"})
}

func TestCompareValues(t *testing.T) {
	tests := []struct {
		a, b     any
		expected int
	}{
		{1, 2, -1},
		{2, 1, 1},
		{1, 1, 0},
		{1.5, 2.5, -1},
		{int64(10), int64(5), 1},
		{uint(3), uint(3), 0},
	}

	for i, tt := range tests {
		result := compareValues(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("test %d: compareValues(%v, %v) = %d, expected %d", i, tt.a, tt.b, result, tt.expected)
		}
	}
}
