// Package engine 实现了 scrapy-go 框架的核心引擎。
//
// Engine 是整个框架的心脏，负责协调 Scheduler、Downloader、Scraper 和 Spider
// 之间的交互，驱动整个爬取流程。
// 对应 Scrapy Python 版本中 scrapy.core.engine 模块的功能。
package engine

import (
	"sync"
	"sync/atomic"

	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
)

// Slot 追踪 Engine 中正在处理的请求。
// 对应 Scrapy 的 _Slot 类。
type Slot struct {
	mu          sync.Mutex
	inprogress  map[*scrapy_http.Request]struct{}
	scheduler   scheduler.Scheduler
	closeIfIdle bool
	closing     atomic.Bool
}

// NewSlot 创建一个新的 Engine Slot。
func NewSlot(sched scheduler.Scheduler, closeIfIdle bool) *Slot {
	return &Slot{
		inprogress:  make(map[*scrapy_http.Request]struct{}),
		scheduler:   sched,
		closeIfIdle: closeIfIdle,
	}
}

// AddRequest 将请求添加到进行中集合。
func (s *Slot) AddRequest(request *scrapy_http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inprogress[request] = struct{}{}
}

// RemoveRequest 从进行中集合移除请求。
func (s *Slot) RemoveRequest(request *scrapy_http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inprogress, request)
}

// InProgressCount 返回进行中的请求数。
func (s *Slot) InProgressCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inprogress)
}

// IsIdle 检查 Slot 是否空闲（无进行中的请求）。
func (s *Slot) IsIdle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inprogress) == 0
}
