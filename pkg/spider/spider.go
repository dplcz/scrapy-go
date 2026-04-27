// Package spider 定义了 scrapy-go 框架的 Spider 接口和基础实现。
//
// Spider 是用户定义爬虫逻辑的核心组件，负责产出初始请求和解析响应。
// 对应 Scrapy Python 版本中 scrapy.spiders 模块的功能。
package spider

import (
	"context"
	"log/slog"
	"runtime/debug"

	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
)

// Output 表示 Spider 回调的输出，可以是 Request 或 Item。
// 每个输出只能是 Request 或 Item 之一，不能同时设置。
type Output struct {
	// Request 非 nil 表示产出一个新请求，将被 Engine 调度。
	Request *scrapy_http.Request

	// Item 非 nil 表示产出一个数据项，将被 Item Pipeline 处理。
	Item any
}

// IsRequest 检查输出是否为 Request。
func (o Output) IsRequest() bool {
	return o.Request != nil
}

// IsItem 检查输出是否为 Item。
func (o Output) IsItem() bool {
	return o.Item != nil
}

// CallbackFunc 定义 Spider 回调函数类型。
// 接收 context 和 Response，返回 Output 切片和可能的错误。
type CallbackFunc func(ctx context.Context, response *scrapy_http.Response) ([]Output, error)

// ErrbackFunc 定义 Spider 错误回调函数类型。
// 接收 context、错误和原始请求，返回 Output 切片和可能的错误。
type ErrbackFunc func(ctx context.Context, err error, request *scrapy_http.Request) ([]Output, error)

// Spider 定义爬虫的核心接口。
// 用户通过实现此接口来定义爬取逻辑。
//
// 对应 Scrapy 的 Spider 基类。
type Spider interface {
	// Name 返回爬虫的唯一名称。
	Name() string

	// Start 返回初始请求/Item 的 channel，Engine 从中消费。
	// channel 关闭表示初始请求产出完毕。
	// 对应 Scrapy 的 Spider.start() 方法。
	Start(ctx context.Context) <-chan Output

	// Parse 是默认的响应回调函数。
	// 当 Request.Callback 为 nil 时，Engine 使用此方法处理响应。
	Parse(ctx context.Context, response *scrapy_http.Response) ([]Output, error)

	// CustomSettings 返回 Spider 级别的配置覆盖（可选）。
	// 返回 nil 表示不覆盖任何配置。
	CustomSettings() *Settings

	// Closed 在 Spider 关闭时调用，用于清理资源。
	Closed(reason string)
}

// ============================================================================
// Base 默认实现
// ============================================================================

// Base 提供 Spider 接口的默认实现。
// 用户可以嵌入此结构体，只覆盖需要的方法。
type Base struct {
	// SpiderName 是爬虫名称。
	SpiderName string

	// StartURLs 是初始 URL 列表。
	// Start() 方法会为每个 URL 创建一个 Request。
	StartURLs []string

	// Logger 是 Spider 的日志记录器。
	Logger *slog.Logger
}

// Name 返回爬虫名称。
func (s *Base) Name() string {
	return s.SpiderName
}

// Start 返回初始请求的 channel。
// 默认实现为 StartURLs 中的每个 URL 创建一个 GET 请求（DontFilter=true）。
func (s *Base) Start(ctx context.Context) <-chan Output {
	ch := make(chan Output)
	go func() {
		defer close(ch)
		// panic recovery: 防止初始请求生成中的 panic 导致进程崩溃
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				if s.Logger != nil {
					s.Logger.Error("panic recovered in Spider.Start",
						"panic", r,
						"stack", stack,
					)
				}
			}
		}()
		for _, rawURL := range s.StartURLs {
			req, err := scrapy_http.NewRequest(rawURL,
				scrapy_http.WithDontFilter(true),
			)
			if err != nil {
				if s.Logger != nil {
				s.Logger.Error("failed to create start request",
					"url", rawURL,
					"error", err,
				)
				}
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- Output{Request: req}:
			}
		}
	}()
	return ch
}

// Parse 是默认的响应回调（空实现，子类应覆盖）。
func (s *Base) Parse(ctx context.Context, response *scrapy_http.Response) ([]Output, error) {
	return nil, nil
}

// CustomSettings 返回 nil（不覆盖任何配置）。
func (s *Base) CustomSettings() *Settings {
	return nil
}

// Closed 在 Spider 关闭时调用（空实现）。
func (s *Base) Closed(reason string) {
}
