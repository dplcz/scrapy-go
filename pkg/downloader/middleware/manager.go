package middleware

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	scrapy_errors "scrapy-go/pkg/errors"
	scrapy_http "scrapy-go/pkg/http"
)

// Entry 表示一个带优先级的中间件条目。
type Entry struct {
	Middleware DownloaderMiddleware
	Name      string
	Priority  int
}

// Manager 管理下载器中间件链。
// 对应 Scrapy 的 DownloaderMiddlewareManager。
type Manager struct {
	middlewares []Entry // 按优先级排序（正序）
	logger     *slog.Logger
}

// NewManager 创建一个新的中间件管理器。
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		logger: logger,
	}
}

// AddMiddleware 添加一个中间件。
// 中间件按优先级排序，优先级数值小的先执行 ProcessRequest。
func (m *Manager) AddMiddleware(mw DownloaderMiddleware, name string, priority int) {
	m.middlewares = append(m.middlewares, Entry{
		Middleware: mw,
		Name:      name,
		Priority:  priority,
	})
	// 按优先级排序（小的在前）
	sort.Slice(m.middlewares, func(i, j int) bool {
		return m.middlewares[i].Priority < m.middlewares[j].Priority
	})
}

// Count 返回中间件数量。
func (m *Manager) Count() int {
	return len(m.middlewares)
}

// Download 执行完整的中间件链处理流程。
// 对应 Scrapy 的 DownloaderMiddlewareManager.download_async。
//
// 处理流程：
//  1. 正序调用 ProcessRequest（可短路返回 Response 或触发异常）
//  2. 调用 downloadFunc 执行实际下载
//  3. 逆序调用 ProcessResponse
//  4. 异常时逆序调用 ProcessException
//
// 当中间件返回 NewRequestError 时，该错误会被直接传播给调用方（Engine），
// 由 Engine 负责将新请求重新调度到 Scheduler。
func (m *Manager) Download(ctx context.Context, downloadFunc DownloadFunc, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	// 1. ProcessRequest 链（正序）
	result, err := m.processRequest(ctx, request)
	if err != nil {
		// NewRequestError 直接传播给 Engine
		if errors.Is(err, scrapy_errors.ErrNewRequest) {
			return nil, err
		}
		// 进入 ProcessException 链
		result, err = m.processException(ctx, request, err)
		if err != nil {
			return nil, err
		}
	}

	// 如果中间件没有直接返回响应，调用实际下载函数
	if result == nil {
		result, err = downloadFunc(ctx, request)
		if err != nil {
			// 下载异常，进入 ProcessException 链
			result, err = m.processException(ctx, request, err)
			if err != nil {
				return nil, err
			}
		}
	}

	// 3. ProcessResponse 链（逆序）
	return m.processResponse(ctx, request, result)
}

// processRequest 正序调用所有中间件的 ProcessRequest。
func (m *Manager) processRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	for _, entry := range m.middlewares {
		resp, err := entry.Middleware.ProcessRequest(ctx, request)
		if err != nil {
			return nil, err
		}
		if resp != nil {
			return resp, nil // 短路
		}
	}
	return nil, nil // 继续到下载器
}

// processResponse 逆序调用所有中间件的 ProcessResponse。
// 当中间件返回 NewRequestError 时，直接传播给调用方。
func (m *Manager) processResponse(ctx context.Context, request *scrapy_http.Request, response *scrapy_http.Response) (*scrapy_http.Response, error) {
	for i := len(m.middlewares) - 1; i >= 0; i-- {
		resp, err := m.middlewares[i].Middleware.ProcessResponse(ctx, request, response)
		if err != nil {
			return nil, err
		}
		response = resp
	}
	return response, nil
}

// processException 逆序调用所有中间件的 ProcessException。
// 当中间件返回 NewRequestError 时，直接传播给调用方。
func (m *Manager) processException(ctx context.Context, request *scrapy_http.Request, originalErr error) (*scrapy_http.Response, error) {
	for i := len(m.middlewares) - 1; i >= 0; i-- {
		resp, err := m.middlewares[i].Middleware.ProcessException(ctx, request, originalErr)
		if err != nil {
			// NewRequestError 直接传播
			if errors.Is(err, scrapy_errors.ErrNewRequest) {
				return nil, err
			}
			// 中间件返回了新的错误，替换原始错误继续传播
			originalErr = err
			continue
		}
		if resp != nil {
			return resp, nil // 异常被转换为响应
		}
	}
	return nil, originalErr // 异常未被处理
}
