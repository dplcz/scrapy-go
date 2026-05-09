package engine

import (
	"sync"
	"sync/atomic"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
)

// Slot 追踪 Engine 中正在处理的请求。
// 对应 Scrapy 的 _Slot 类。
//
// 优化：使用 atomic.Int64 计数器提供 IsIdle/InProgressCount 的快速路径，
// 避免高频调度循环中的 Mutex 锁开销。inprogress map 仅用于精确追踪请求集合。
type Slot struct {
	mu              sync.Mutex
	inprogress      map[*shttp.Request]struct{}
	inProgressCount atomic.Int64 // atomic 计数器，IsIdle/InProgressCount 快速路径
	scheduler       scheduler.Scheduler
	closeIfIdle     bool
	closing         atomic.Bool
}

// NewSlot 创建一个新的 Engine Slot。
func NewSlot(sched scheduler.Scheduler, closeIfIdle bool) *Slot {
	return &Slot{
		inprogress:  make(map[*shttp.Request]struct{}),
		scheduler:   sched,
		closeIfIdle: closeIfIdle,
	}
}

// AddRequest 将请求添加到进行中集合。
func (s *Slot) AddRequest(request *shttp.Request) {
	s.mu.Lock()
	s.inprogress[request] = struct{}{}
	s.mu.Unlock()
	s.inProgressCount.Add(1)
}

// RemoveRequest 从进行中集合移除请求。
func (s *Slot) RemoveRequest(request *shttp.Request) {
	s.mu.Lock()
	delete(s.inprogress, request)
	s.mu.Unlock()
	s.inProgressCount.Add(-1)
}

// InProgressCount 返回进行中的请求数。
// 使用 atomic 计数器，无锁快速路径。
func (s *Slot) InProgressCount() int {
	return int(s.inProgressCount.Load())
}

// IsIdle 检查 Slot 是否空闲（无进行中的请求）。
// 使用 atomic 计数器，无锁快速路径。
func (s *Slot) IsIdle() bool {
	return s.inProgressCount.Load() == 0
}