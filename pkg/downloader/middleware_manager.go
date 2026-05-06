package downloader

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"

	"github.com/dplcz/scrapy-go/pkg/downloader/middleware"
)

// MiddlewareEntry 表示一个带优先级的中间件条目。
//
// 中间件可以是以下任意接口的实现：
//   - [middleware.RequestProcessor]：处理请求
//   - [middleware.ResponseProcessor]：处理响应
//   - [middleware.ExceptionProcessor]：处理异常
//   - [middleware.DownloaderMiddleware]：全功能（同时满足以上三者）
//
// Manager 通过类型断言在运行时适配，仅调用中间件实际实现的方法。
type MiddlewareEntry struct {
	// Middleware 存储中间件实例。
	// 至少需要实现 RequestProcessor / ResponseProcessor / ExceptionProcessor 之一。
	Middleware any
	Name       string
	Priority   int

	// 缓存类型断言结果，避免每次调用时重复断言。
	reqProc  middleware.RequestProcessor
	respProc middleware.ResponseProcessor
	excProc  middleware.ExceptionProcessor
}

// MiddlewareManager 管理下载器中间件链。
// 对应 Scrapy 的 DownloaderMiddlewareManager。
//
// 支持接口隔离：中间件只需实现关心的细粒度接口，
// Manager 通过类型断言自动适配，跳过未实现的处理阶段。
type MiddlewareManager struct {
	middlewares []MiddlewareEntry // 按优先级排序（正序）
	logger      *slog.Logger
}

// NewMiddlewareManager 创建一个新的中间件管理器。
func NewMiddlewareManager(logger *slog.Logger) *MiddlewareManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &MiddlewareManager{
		logger: logger,
	}
}

// AddMiddleware 添加一个中间件。
// 中间件按优先级排序，优先级数值小的先执行 ProcessRequest。
//
// mw 可以是以下类型之一：
//   - middleware.DownloaderMiddleware（全功能，向后兼容）
//   - middleware.RequestProcessor（仅处理请求）
//   - middleware.ResponseProcessor（仅处理响应）
//   - middleware.ExceptionProcessor（仅处理异常）
//   - 或以上接口的任意组合
func (m *MiddlewareManager) AddMiddleware(mw any, name string, priority int) {
	entry := MiddlewareEntry{
		Middleware: mw,
		Name:       name,
		Priority:   priority,
	}
	// 缓存类型断言结果
	entry.reqProc, _ = mw.(middleware.RequestProcessor)
	entry.respProc, _ = mw.(middleware.ResponseProcessor)
	entry.excProc, _ = mw.(middleware.ExceptionProcessor)

	m.middlewares = append(m.middlewares, entry)
	// 按优先级排序（小的在前）
	sort.Slice(m.middlewares, func(i, j int) bool {
		return m.middlewares[i].Priority < m.middlewares[j].Priority
	})
}

// Count 返回中间件数量。
func (m *MiddlewareManager) Count() int {
	return len(m.middlewares)
}

// Download 执行完整的中间件链处理流程。
// 对应 Scrapy 的 DownloaderMiddlewareManager.download_async。
//
// 处理流程：
//  1. 正序调用 ProcessRequest（仅对实现了 RequestProcessor 的中间件）
//  2. 调用 downloadFunc 执行实际下载
//  3. 逆序调用 ProcessResponse（仅对实现了 ResponseProcessor 的中间件）
//  4. 异常时逆序调用 ProcessException（仅对实现了 ExceptionProcessor 的中间件）
//
// 当中间件返回 NewRequestError 时，该错误会被直接传播给调用方（Engine），
// 由 Engine 负责将新请求重新调度到 Scheduler。
func (m *MiddlewareManager) Download(ctx context.Context, downloadFunc middleware.DownloadFunc, request *shttp.Request) (*shttp.Response, error) {
	// 1. ProcessRequest 链（正序）
	result, err := m.processRequest(ctx, request)
	if err != nil {
		// NewRequestError 直接传播给 Engine
		if errors.Is(err, serrors.ErrNewRequest) {
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

// processRequest 正序调用所有实现了 RequestProcessor 的中间件。
func (m *MiddlewareManager) processRequest(ctx context.Context, request *shttp.Request) (*shttp.Response, error) {
	for _, entry := range m.middlewares {
		if entry.reqProc == nil {
			continue // 跳过未实现 RequestProcessor 的中间件
		}
		resp, err := entry.reqProc.ProcessRequest(ctx, request)
		if err != nil {
			return nil, err
		}
		if resp != nil {
			return resp, nil // 短路
		}
	}
	return nil, nil // 继续到下载器
}

// processResponse 逆序调用所有实现了 ResponseProcessor 的中间件。
// 当中间件返回 NewRequestError 时，直接传播给调用方。
func (m *MiddlewareManager) processResponse(ctx context.Context, request *shttp.Request, response *shttp.Response) (*shttp.Response, error) {
	for i := len(m.middlewares) - 1; i >= 0; i-- {
		if m.middlewares[i].respProc == nil {
			continue // 跳过未实现 ResponseProcessor 的中间件
		}
		resp, err := m.middlewares[i].respProc.ProcessResponse(ctx, request, response)
		if err != nil {
			return nil, err
		}
		response = resp
	}
	return response, nil
}

// processException 逆序调用所有实现了 ExceptionProcessor 的中间件。
// 当中间件返回 NewRequestError 时，直接传播给调用方。
func (m *MiddlewareManager) processException(ctx context.Context, request *shttp.Request, originalErr error) (*shttp.Response, error) {
	for i := len(m.middlewares) - 1; i >= 0; i-- {
		if m.middlewares[i].excProc == nil {
			continue // 跳过未实现 ExceptionProcessor 的中间件
		}
		resp, err := m.middlewares[i].excProc.ProcessException(ctx, request, originalErr)
		if err != nil {
			// NewRequestError 直接传播
			if errors.Is(err, serrors.ErrNewRequest) {
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
