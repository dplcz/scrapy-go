package proxy

import (
	"sync/atomic"
	"time"
)

// State 表示代理的健康状态。
type State int32

const (
	// StateHealthy 表示代理正常可用。
	StateHealthy State = iota

	// StateDegraded 表示代理可用但近期出现失败，权重已被降低。
	// 仍可参与选择但概率会降低。
	StateDegraded

	// StateUnhealthy 表示代理已被标记为不健康，
	// Pool 在 Get 时会跳过此状态的代理。
	// 健康检查通过后可恢复为 StateHealthy。
	StateUnhealthy
)

// String 返回状态的可读字符串。
func (s State) String() string {
	switch s {
	case StateHealthy:
		return "healthy"
	case StateDegraded:
		return "degraded"
	case StateUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// Proxy 表示代理池中的一个代理实体。
//
// 字段说明：
//   - URL 为不含认证信息的代理 URL（如 "http://proxy.example.com:8080"）
//   - Credentials 为 Base64 编码的 "user:password"，无认证时为空
//   - Weight 为初始权重，用于加权轮询策略
//   - 其余统计字段使用 atomic 操作以支持高并发无锁访问
//
// 线程安全：所有字段更新均通过 atomic 包完成，可被多个 goroutine 并发访问。
// URL/Credentials/Weight 在 Pool 初始化后不可变，无需锁保护。
type Proxy struct {
	// URL 是代理服务器 URL（不含认证信息）。
	// 例如："http://proxy.example.com:8080"。
	URL string

	// Credentials 是 Base64 编码的 "user:password" 认证信息。
	// 无认证时为空字符串。
	Credentials string

	// Weight 是初始权重（仅 StrategyWeighted 使用）。
	// 0 或负值会被规范化为 1。
	Weight int

	// state 是当前健康状态（atomic int32 存储）。
	state atomic.Int32

	// failures 是连续失败次数。
	// 健康检查或下载失败时递增；成功时重置为 0。
	failures atomic.Int64

	// successes 是历史累计成功次数（用于统计与监控）。
	successes atomic.Int64

	// totalUsed 是历史累计使用次数（用于策略层 RoundRobin 与监控）。
	totalUsed atomic.Int64

	// lastUsedUnix 是上一次被选中的 Unix 时间戳（秒）。
	lastUsedUnix atomic.Int64

	// lastCheckedUnix 是上一次健康检查的 Unix 时间戳（秒）。
	lastCheckedUnix atomic.Int64
}

// State 返回代理当前的健康状态。
func (p *Proxy) State() State {
	return State(p.state.Load())
}

// SetState 原子地更新代理的健康状态。
func (p *Proxy) SetState(s State) {
	p.state.Store(int32(s))
}

// Failures 返回当前连续失败次数。
func (p *Proxy) Failures() int64 {
	return p.failures.Load()
}

// Successes 返回历史累计成功次数。
func (p *Proxy) Successes() int64 {
	return p.successes.Load()
}

// TotalUsed 返回历史累计使用次数。
func (p *Proxy) TotalUsed() int64 {
	return p.totalUsed.Load()
}

// LastUsed 返回上一次被选中的时间。
func (p *Proxy) LastUsed() time.Time {
	ts := p.lastUsedUnix.Load()
	if ts == 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0)
}

// LastChecked 返回上一次健康检查的时间。
func (p *Proxy) LastChecked() time.Time {
	ts := p.lastCheckedUnix.Load()
	if ts == 0 {
		return time.Time{}
	}
	return time.Unix(ts, 0)
}

// markUsed 在被 Pool 选中时调用，更新使用统计。
// 不暴露给外部，仅 Pool 内部使用。
func (p *Proxy) markUsed() {
	p.totalUsed.Add(1)
	p.lastUsedUnix.Store(time.Now().Unix())
}

// markSuccess 在使用成功后由 Pool.Mark 调用。
func (p *Proxy) markSuccess() {
	p.successes.Add(1)
	p.failures.Store(0) // 成功后重置连续失败计数
}

// markFailure 在使用失败后由 Pool.Mark 调用。
// 返回更新后的连续失败次数。
func (p *Proxy) markFailure() int64 {
	return p.failures.Add(1)
}

// markChecked 由健康检查器在每次探测后调用。
func (p *Proxy) markChecked() {
	p.lastCheckedUnix.Store(time.Now().Unix())
}

// Snapshot 返回当前代理的只读快照，便于监控展示。
// 字段值为调用时刻的瞬时值，由 atomic 操作保证读取一致性。
type Snapshot struct {
	URL         string
	State       State
	Weight      int
	Failures    int64
	Successes   int64
	TotalUsed   int64
	LastUsed    time.Time
	LastChecked time.Time
}

// Snapshot 返回当前代理的只读快照。
func (p *Proxy) Snapshot() Snapshot {
	return Snapshot{
		URL:         p.URL,
		State:       p.State(),
		Weight:      p.Weight,
		Failures:    p.Failures(),
		Successes:   p.Successes(),
		TotalUsed:   p.TotalUsed(),
		LastUsed:    p.LastUsed(),
		LastChecked: p.LastChecked(),
	}
}
