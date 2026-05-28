package proxy

import (
	"math/rand/v2"
	"sort"
	"sync/atomic"
)

// roundRobinStrategy 实现轮询策略。
//
// 使用 atomic.Uint64 自增取模实现无锁选择，
// 高并发下零锁竞争。
type roundRobinStrategy struct {
	counter atomic.Uint64
}

// NewRoundRobinStrategy 创建一个轮询策略实例。
func NewRoundRobinStrategy() Strategy {
	return &roundRobinStrategy{}
}

// Select 按递增计数器对候选数取模选择代理。
func (s *roundRobinStrategy) Select(candidates []*Proxy) *Proxy {
	n := len(candidates)
	if n == 0 {
		return nil
	}
	// 先 Add 再取模，避免溢出后短暂回到 0 的偏差累积
	idx := s.counter.Add(1) - 1
	return candidates[idx%uint64(n)]
}

// Name 返回策略名称。
func (s *roundRobinStrategy) Name() string { return string(StrategyRoundRobin) }

// randomStrategy 实现随机策略。
//
// 使用 math/rand/v2 包，无锁实现，
// 不需要用户初始化种子（运行时自动）。
type randomStrategy struct{}

// NewRandomStrategy 创建一个随机策略实例。
func NewRandomStrategy() Strategy {
	return &randomStrategy{}
}

// Select 均匀随机选择一个代理。
func (s *randomStrategy) Select(candidates []*Proxy) *Proxy {
	n := len(candidates)
	if n == 0 {
		return nil
	}
	return candidates[rand.IntN(n)]
}

// Name 返回策略名称。
func (s *randomStrategy) Name() string { return string(StrategyRandom) }

// weightedStrategy 实现加权随机策略。
//
// 性能优化：
//   - 候选列表通常每次调用都不同（健康过滤），所以无法跨调用缓存
//   - 单次调用内使用累积权重数组 + 二分查找 O(log n)，避免线性遍历
//   - 当 n 较小（<= 8）时直接线性遍历，避免分配 cumulative 切片
//
// 注意：candidates 不可修改；策略本身无状态，可被多 goroutine 共享。
type weightedStrategy struct{}

// NewWeightedStrategy 创建一个加权随机策略实例。
func NewWeightedStrategy() Strategy {
	return &weightedStrategy{}
}

// Select 根据 Proxy.Weight 进行加权随机选择。
//
// Weight <= 0 视为 1，避免除零或负权重。
func (s *weightedStrategy) Select(candidates []*Proxy) *Proxy {
	n := len(candidates)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return candidates[0]
	}

	// 小池子：线性扫描（避免堆分配）
	if n <= 8 {
		var total int
		for _, p := range candidates {
			total += weightOf(p)
		}
		if total <= 0 {
			return candidates[rand.IntN(n)]
		}
		pick := rand.IntN(total)
		acc := 0
		for _, p := range candidates {
			acc += weightOf(p)
			if pick < acc {
				return p
			}
		}
		return candidates[n-1]
	}

	// 大池子：累积权重 + 二分查找 O(log n)
	cumulative := make([]int, n)
	total := 0
	for i, p := range candidates {
		total += weightOf(p)
		cumulative[i] = total
	}
	if total <= 0 {
		return candidates[rand.IntN(n)]
	}
	pick := rand.IntN(total)
	idx := sort.SearchInts(cumulative, pick+1)
	return candidates[idx]
}

// Name 返回策略名称。
func (s *weightedStrategy) Name() string { return string(StrategyWeighted) }

// weightOf 返回代理的有效权重，0 或负数视为 1。
func weightOf(p *Proxy) int {
	if p.Weight <= 0 {
		return 1
	}
	return p.Weight
}

// newStrategyByKind 根据策略类型构造对应的策略实例。
// 内部使用，调用方需保证 kind 已通过 Options.validate 校验。
func newStrategyByKind(kind StrategyKind) Strategy {
	switch kind {
	case StrategyRandom:
		return NewRandomStrategy()
	case StrategyWeighted:
		return NewWeightedStrategy()
	default:
		return NewRoundRobinStrategy()
	}
}
