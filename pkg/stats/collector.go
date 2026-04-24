// Package stats 实现了 scrapy-go 框架的统计收集系统。
//
// 提供 Collector 接口和基于内存的默认实现，
// 对应 Scrapy Python 版本中 scrapy.statscollectors 模块的功能。
package stats

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// ============================================================================
// Collector 接口
// ============================================================================

// Collector 定义统计收集器接口。
// 所有方法都是线程安全的。
type Collector interface {
	// GetValue 获取统计值，不存在时返回 defaultVal。
	GetValue(key string, defaultVal any) any

	// GetStats 获取所有统计数据的快照。
	GetStats() map[string]any

	// SetValue 设置统计值。
	SetValue(key string, value any)

	// SetStats 替换所有统计数据。
	SetStats(stats map[string]any)

	// IncValue 递增统计值。
	// 如果 key 不存在，从 start 开始计数。
	IncValue(key string, count int, start int)

	// MaxValue 设置统计值为当前值和给定值中的较大者。
	MaxValue(key string, value any)

	// MinValue 设置统计值为当前值和给定值中的较小者。
	MinValue(key string, value any)

	// ClearStats 清空所有统计数据。
	ClearStats()

	// Open 在 Spider 打开时调用。
	Open()

	// Close 在 Spider 关闭时调用。
	Close(reason string)
}

// ============================================================================
// MemoryCollector 实现
// ============================================================================

// MemoryCollector 是基于内存的统计收集器。
// 线程安全，所有操作通过 RWMutex 保护。
type MemoryCollector struct {
	mu     sync.RWMutex
	stats  map[string]any
	dump   bool
	logger *slog.Logger
}

// NewMemoryCollector 创建一个新的内存统计收集器。
func NewMemoryCollector(dump bool, logger *slog.Logger) *MemoryCollector {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryCollector{
		stats:  make(map[string]any),
		dump:   dump,
		logger: logger,
	}
}

// GetValue 获取统计值。
func (c *MemoryCollector) GetValue(key string, defaultVal any) any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if v, ok := c.stats[key]; ok {
		return v
	}
	return defaultVal
}

// GetStats 获取所有统计数据的快照。
func (c *MemoryCollector) GetStats() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]any, len(c.stats))
	for k, v := range c.stats {
		result[k] = v
	}
	return result
}

// SetValue 设置统计值。
func (c *MemoryCollector) SetValue(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats[key] = value
}

// SetStats 替换所有统计数据。
func (c *MemoryCollector) SetStats(stats map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = stats
}

// IncValue 递增统计值。
func (c *MemoryCollector) IncValue(key string, count int, start int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if v, ok := c.stats[key]; ok {
		switch val := v.(type) {
		case int:
			c.stats[key] = val + count
		case int64:
			c.stats[key] = val + int64(count)
		case float64:
			c.stats[key] = val + float64(count)
		default:
			c.stats[key] = start + count
		}
	} else {
		c.stats[key] = start + count
	}
}

// MaxValue 设置统计值为当前值和给定值中的较大者。
func (c *MemoryCollector) MaxValue(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.stats[key]; ok {
		if compareValues(value, existing) > 0 {
			c.stats[key] = value
		}
	} else {
		c.stats[key] = value
	}
}

// MinValue 设置统计值为当前值和给定值中的较小者。
func (c *MemoryCollector) MinValue(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.stats[key]; ok {
		if compareValues(value, existing) < 0 {
			c.stats[key] = value
		}
	} else {
		c.stats[key] = value
	}
}

// ClearStats 清空所有统计数据。
func (c *MemoryCollector) ClearStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = make(map[string]any)
}

// Open 在 Spider 打开时调用。
func (c *MemoryCollector) Open() {
	// 内存统计收集器无需特殊初始化
}

// Close 在 Spider 关闭时调用。
func (c *MemoryCollector) Close(reason string) {
	if c.dump {
		c.dumpStats()
	}
}

// dumpStats 输出统计数据到日志。
func (c *MemoryCollector) dumpStats() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.stats) == 0 {
		return
	}

	// 按 key 排序输出
	keys := make([]string, 0, len(c.stats))
	for k := range c.stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 逐行打印每个统计项
	c.logger.Info("Scrapy stats:")
	for _, k := range keys {
		c.logger.Info(fmt.Sprintf("  %s: %v", k, c.stats[k]))
	}
}

// ============================================================================
// DummyCollector 实现
// ============================================================================

// DummyCollector 是一个空操作的统计收集器，不收集任何数据。
// 用于禁用统计收集的场景。
type DummyCollector struct{}

// NewDummyCollector 创建一个空操作统计收集器。
func NewDummyCollector() *DummyCollector {
	return &DummyCollector{}
}

func (c *DummyCollector) GetValue(key string, defaultVal any) any { return defaultVal }
func (c *DummyCollector) GetStats() map[string]any                { return map[string]any{} }
func (c *DummyCollector) SetValue(key string, value any)          {}
func (c *DummyCollector) SetStats(stats map[string]any)           {}
func (c *DummyCollector) IncValue(key string, count int, start int) {}
func (c *DummyCollector) MaxValue(key string, value any)          {}
func (c *DummyCollector) MinValue(key string, value any)          {}
func (c *DummyCollector) ClearStats()                             {}
func (c *DummyCollector) Open()                                   {}
func (c *DummyCollector) Close(reason string)                     {}

// ============================================================================
// 辅助函数
// ============================================================================

// compareValues 比较两个值的大小。
// 返回值：-1 (a < b), 0 (a == b), 1 (a > b)。
// 仅支持数值类型比较，其他类型返回 0。
func compareValues(a, b any) int {
	af := toFloat64(a)
	bf := toFloat64(b)

	if af < bf {
		return -1
	}
	if af > bf {
		return 1
	}
	return 0
}

// toFloat64 将值转换为 float64。
func toFloat64(v any) float64 {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int8:
		return float64(val)
	case int16:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case uint:
		return float64(val)
	case uint8:
		return float64(val)
	case uint16:
		return float64(val)
	case uint32:
		return float64(val)
	case uint64:
		return float64(val)
	case float32:
		return float64(val)
	case float64:
		return val
	default:
		return 0
	}
}
