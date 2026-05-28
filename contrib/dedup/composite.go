package dedup

import (
	"context"
	"errors"
	"fmt"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/scheduler"
)

// CompositeDupeFilter 将多个 scheduler.DupeFilter 链式组合。
//
// RequestSeen 使用 OR 语义：任一子过滤器判定请求已见过，就立即返回 true。
// 当所有过滤器都认为是新请求时，返回 false。该语义适合将 URL 精确去重、
// URL 规范化去重、内容近似去重等策略组合成一个调度器级去重器。
type CompositeDupeFilter struct {
	filters []scheduler.DupeFilter
}

// NewCompositeDupeFilter 创建组合去重过滤器。
//
// nil 过滤器会被忽略。至少需要传入一个非 nil 过滤器，否则返回空组合，
// 空组合的 RequestSeen 永远返回 false。
func NewCompositeDupeFilter(filters ...scheduler.DupeFilter) *CompositeDupeFilter {
	compact := make([]scheduler.DupeFilter, 0, len(filters))
	for _, filter := range filters {
		if filter != nil {
			compact = append(compact, filter)
		}
	}
	return &CompositeDupeFilter{filters: compact}
}

// Filters 返回子过滤器快照。
func (f *CompositeDupeFilter) Filters() []scheduler.DupeFilter {
	filters := make([]scheduler.DupeFilter, len(f.filters))
	copy(filters, f.filters)
	return filters
}

// Open 依次初始化所有子过滤器。
// 如果中途失败，会关闭已打开的过滤器并返回带上下文的错误。
func (f *CompositeDupeFilter) Open(ctx context.Context) error {
	opened := 0
	for i, filter := range f.filters {
		if err := filter.Open(ctx); err != nil {
			for j := opened - 1; j >= 0; j-- {
				_ = f.filters[j].Close("open_failed")
			}
			return fmt.Errorf("dedup: open filter %d: %w", i, err)
		}
		opened++
	}
	return nil
}

// Close 依次关闭所有子过滤器并聚合错误。
func (f *CompositeDupeFilter) Close(reason string) error {
	var joined error
	for i := len(f.filters) - 1; i >= 0; i-- {
		if err := f.filters[i].Close(reason); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

// RequestSeen 检查请求是否被任一策略判定为重复。
func (f *CompositeDupeFilter) RequestSeen(request *shttp.Request) bool {
	for _, filter := range f.filters {
		if filter.RequestSeen(request) {
			return true
		}
	}
	return false
}
