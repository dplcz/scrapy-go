package dedup

import (
	"errors"

	"github.com/dplcz/scrapy-go/pkg/scheduler"
)

// Options 定义高级组合去重过滤器的配置。
//
// 推荐通过 DefaultOptions 获取默认配置后按需调整。
// 默认启用 URL 规范化去重和 SimHash 内容近似去重。
type Options struct {
	// URLCanonicalization 是否启用 URL 规范化去重。
	URLCanonicalization bool

	// SimHash 是否启用 SimHash 内容近似去重。
	SimHash bool

	// URLOptions 是 URL 规范化去重配置。
	// 为 nil 时使用 DefaultURLCanonicalizerOptions。
	URLOptions *URLCanonicalizerOptions

	// SimHashOptions 是 SimHash 内容近似去重配置。
	// 为 nil 时使用 DefaultSimHashOptions。
	SimHashOptions *SimHashOptions
}

// DefaultOptions 返回高级去重默认配置。
func DefaultOptions() *Options {
	return &Options{
		URLCanonicalization: true,
		SimHash:             true,
		URLOptions:          DefaultURLCanonicalizerOptions(),
		SimHashOptions:      DefaultSimHashOptions(),
	}
}

// NewDupeFilter 根据 Options 创建默认高级组合去重过滤器。
//
// 当 opts 为 nil 时使用 DefaultOptions。至少需要启用一种策略，
// 否则返回错误。
func NewDupeFilter(opts *Options) (*CompositeDupeFilter, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	filters := make([]scheduler.DupeFilter, 0, 2)
	if opts.URLCanonicalization {
		filters = append(filters, NewURLCanonicalDupeFilter(opts.URLOptions))
	}
	if opts.SimHash {
		filter, err := NewSimHashDupeFilter(opts.SimHashOptions)
		if err != nil {
			return nil, err
		}
		filters = append(filters, filter)
	}
	if len(filters) == 0 {
		return nil, errors.New("dedup: at least one dedup strategy must be enabled")
	}

	return NewCompositeDupeFilter(filters...), nil
}
