package middleware

import (
	"context"
	"log/slog"
	"sort"

	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// Entry 表示一个带优先级的 Spider 中间件条目。
type Entry struct {
	Middleware SpiderMiddleware
	Name       string
	Priority   int
}

// Manager 管理 Spider 中间件链。
// 对应 Scrapy 的 SpiderMiddlewareManager。
type Manager struct {
	middlewares []Entry // 按优先级排序（正序）
	logger      *slog.Logger
}

// NewManager 创建一个新的 Spider 中间件管理器。
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger: logger,
	}
}

// AddMiddleware 添加一个 Spider 中间件。
// 中间件按优先级排序，优先级数值小的先执行 ProcessSpiderInput。
func (m *Manager) AddMiddleware(mw SpiderMiddleware, name string, priority int) {
	m.middlewares = append(m.middlewares, Entry{
		Middleware: mw,
		Name:       name,
		Priority:   priority,
	})
	sort.Slice(m.middlewares, func(i, j int) bool {
		return m.middlewares[i].Priority < m.middlewares[j].Priority
	})
}

// Count 返回中间件数量。
func (m *Manager) Count() int {
	return len(m.middlewares)
}

// ScrapeResponse 执行完整的 Spider 中间件链处理流程。
// 对应 Scrapy 的 SpiderMiddlewareManager.scrape_response_async。
//
// 处理流程：
//  1. 正序调用 ProcessSpiderInput
//  2. 调用 scrapeFunc（Spider 回调）
//  3. 逆序调用 ProcessOutput
//  4. 异常时逆序调用 ProcessSpiderException
func (m *Manager) ScrapeResponse(
	ctx context.Context,
	scrapeFunc func(ctx context.Context, response *scrapy_http.Response) ([]spider.Output, error),
	response *scrapy_http.Response,
) ([]spider.Output, error) {

	// 1. ProcessSpiderInput 链（正序）
	for _, entry := range m.middlewares {
		if err := entry.Middleware.ProcessSpiderInput(ctx, response); err != nil {
			// 输入处理失败，进入异常处理链
			return m.processSpiderException(ctx, response, err)
		}
	}

	// 2. 调用 Spider 回调
	result, err := scrapeFunc(ctx, response)
	if err != nil {
		// Spider 回调异常，进入异常处理链
		return m.processSpiderException(ctx, response, err)
	}

	// 3. ProcessOutput 链（逆序）
	return m.processOutput(ctx, response, result)
}

// processOutput 逆序调用所有中间件的 ProcessOutput。
func (m *Manager) processOutput(ctx context.Context, response *scrapy_http.Response, result []spider.Output) ([]spider.Output, error) {
	var err error
	for i := len(m.middlewares) - 1; i >= 0; i-- {
		result, err = m.middlewares[i].Middleware.ProcessOutput(ctx, response, result)
		if err != nil {
			// 输出处理异常，进入异常处理链（从当前位置开始）
			return m.processSpiderExceptionFrom(ctx, response, err, i-1)
		}
	}
	return result, nil
}

// processSpiderException 逆序调用所有中间件的 ProcessSpiderException。
func (m *Manager) processSpiderException(ctx context.Context, response *scrapy_http.Response, originalErr error) ([]spider.Output, error) {
	return m.processSpiderExceptionFrom(ctx, response, originalErr, len(m.middlewares)-1)
}

// processSpiderExceptionFrom 从指定索引开始逆序调用 ProcessSpiderException。
func (m *Manager) processSpiderExceptionFrom(ctx context.Context, response *scrapy_http.Response, originalErr error, startIndex int) ([]spider.Output, error) {
	for i := startIndex; i >= 0; i-- {
		result, err := m.middlewares[i].Middleware.ProcessSpiderException(ctx, response, originalErr)
		if err != nil {
			// 中间件返回了新的错误，替换原始错误继续传播
			originalErr = err
			continue
		}
		if result != nil {
			// 异常被转换为输出，通过剩余的 ProcessOutput 链处理
			return m.processOutputFrom(ctx, response, result, i-1)
		}
	}
	return nil, originalErr // 异常未被处理
}

// processOutputFrom 从指定索引开始逆序调用 ProcessOutput。
func (m *Manager) processOutputFrom(ctx context.Context, response *scrapy_http.Response, result []spider.Output, startIndex int) ([]spider.Output, error) {
	var err error
	for i := startIndex; i >= 0; i-- {
		result, err = m.middlewares[i].Middleware.ProcessOutput(ctx, response, result)
		if err != nil {
			return m.processSpiderExceptionFrom(ctx, response, err, i-1)
		}
	}
	return result, nil
}
