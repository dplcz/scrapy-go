package middleware

import (
	"context"
	"fmt"
	"log/slog"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// HttpErrorMiddleware 过滤非 200 响应。
// 对应 Scrapy 的 scrapy.spidermiddlewares.httperror.HttpErrorMiddleware。
//
// 当响应状态码不在 200-299 范围内时，返回错误跳过 Spider 回调。
// 可通过以下方式允许特定状态码：
//   - 配置 HTTPERROR_ALLOWED_CODES: 全局允许的状态码列表
//   - 配置 HTTPERROR_ALLOW_ALL: 允许所有状态码
//   - Request.Meta["handle_httpstatus_all"]: 请求级允许所有状态码
//   - Request.Meta["handle_httpstatus_list"]: 请求级允许的状态码列表
type HttpErrorMiddleware struct {
	BaseSpiderMiddleware

	allowAll   bool
	allowCodes []int
	stats      stats.Collector
	logger     *slog.Logger
}

// NewHttpErrorMiddleware 创建一个新的 HttpError 中间件。
func NewHttpErrorMiddleware(allowAll bool, allowCodes []int, sc stats.Collector, logger *slog.Logger) *HttpErrorMiddleware {
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HttpErrorMiddleware{
		allowAll:   allowAll,
		allowCodes: allowCodes,
		stats:      sc,
		logger:     logger,
	}
}

// ProcessSpiderInput 检查响应状态码，非 2xx 且不在允许列表中则返回错误。
func (m *HttpErrorMiddleware) ProcessSpiderInput(ctx context.Context, response *shttp.Response) error {
	// 2xx 状态码直接通过
	if response.Status >= 200 && response.Status < 300 {
		return nil
	}

	// 检查请求级 Meta 覆盖
	if response.Request != nil {
		if handleAll, ok := response.Request.GetMeta("handle_httpstatus_all"); ok {
			if b, ok := handleAll.(bool); ok && b {
				return nil
			}
		}
		if handleList, ok := response.Request.GetMeta("handle_httpstatus_list"); ok {
			if codes, ok := handleList.([]int); ok {
				if intSliceContains(codes, response.Status) {
					return nil
				}
				// 请求级列表优先，不再检查全局配置
				return newHttpError(response.Status)
			}
		}
	}

	// 检查全局配置
	if m.allowAll {
		return nil
	}
	if intSliceContains(m.allowCodes, response.Status) {
		return nil
	}

	return newHttpError(response.Status)
}

// ProcessSpiderException 处理 HttpError 异常，记录统计信息。
func (m *HttpErrorMiddleware) ProcessSpiderException(ctx context.Context, response *shttp.Response, err error) ([]spider.Output, error) {
	if httpErr, ok := err.(*HttpError); ok {
		m.stats.IncValue("httperror/response_ignored_count", 1, 0)
		m.stats.IncValue(
			fmt.Sprintf("httperror/response_ignored_status_count/%d", httpErr.Status),
			1, 0,
		)
		m.logger.Info("ignoring response: HTTP status code is not handled or not allowed",
			"status", httpErr.Status,
			"url", response.URL.String(),
		)
		// 返回空输出，吞掉异常
		return []spider.Output{}, nil
	}
	return nil, nil
}

// HttpError 表示非 2xx 响应被过滤。
type HttpError struct {
	Status  int
	Message string
}

func (e *HttpError) Error() string {
	return e.Message
}

// newHttpError 创建 HttpError。
func newHttpError(status int) *HttpError {
	return &HttpError{
		Status:  status,
		Message: fmt.Sprintf("ignoring non-200 response: %d", status),
	}
}

// intSliceContains 检查 int 切片是否包含指定值。
func intSliceContains(s []int, v int) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}
