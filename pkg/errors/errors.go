// Package errors 定义了 scrapy-go 框架的所有错误类型。
//
// 包含 sentinel errors（哨兵错误）和带上下文信息的结构化错误类型，
// 对应 Scrapy Python 版本中 scrapy.exceptions 模块的功能。
package errors

import (
	"errors"
	"fmt"
)

// ============================================================================
// Sentinel Errors（哨兵错误）
// ============================================================================

// ErrNotConfigured 表示组件未配置，对应 Scrapy 的 NotConfigured 异常。
// 中间件初始化时返回此错误，框架将跳过该中间件并记录警告日志。
var ErrNotConfigured = errors.New("component not configured")

// ErrIgnoreRequest 表示决定不处理某个请求，对应 Scrapy 的 IgnoreRequest 异常。
var ErrIgnoreRequest = errors.New("request ignored")

// ErrDropItem 表示从 Item Pipeline 中丢弃一个 Item，对应 Scrapy 的 DropItem 异常。
var ErrDropItem = errors.New("item dropped")

// ErrCloseSpider 表示请求关闭 Spider，对应 Scrapy 的 CloseSpider 异常。
var ErrCloseSpider = errors.New("close spider")

// ErrDontCloseSpider 表示请求不要关闭 Spider，对应 Scrapy 的 DontCloseSpider 异常。
// 在 spider_idle 信号处理器中返回此错误可阻止 Spider 关闭。
var ErrDontCloseSpider = errors.New("don't close spider")

// ErrDownloadTimeout 表示下载超时，对应 Scrapy 的 DownloadTimeoutError 异常。
var ErrDownloadTimeout = errors.New("download timeout")

// ErrStopDownload 表示停止下载响应体，对应 Scrapy 的 StopDownload 异常。
var ErrStopDownload = errors.New("stop download")

// ErrInvalidOutput 表示中间件返回了无效的输出类型，对应 Scrapy 的 _InvalidOutput 异常。
var ErrInvalidOutput = errors.New("invalid middleware output")

// ErrNotSupported 表示不支持的功能或方法，对应 Scrapy 的 NotSupported 异常。
var ErrNotSupported = errors.New("not supported")

// ErrConnectionRefused 表示连接被拒绝，对应 Scrapy 的 DownloadConnectionRefusedError。
var ErrConnectionRefused = errors.New("connection refused")

// ErrCannotResolveHost 表示无法解析主机名，对应 Scrapy 的 CannotResolveHostError。
var ErrCannotResolveHost = errors.New("cannot resolve host")

// ErrDownloadFailed 表示下载失败，对应 Scrapy 的 DownloadFailedError。
var ErrDownloadFailed = errors.New("download failed")

// ErrResponseDataLoss 表示响应数据不完整，对应 Scrapy 的 ResponseDataLossError。
var ErrResponseDataLoss = errors.New("response data loss")

// ============================================================================
// 带上下文的结构化错误类型
// ============================================================================

// CloseSpiderError 是带关闭原因的 CloseSpider 错误。
// 使用 errors.Is(err, ErrCloseSpider) 可以匹配此错误。
type CloseSpiderError struct {
	Reason string
}

func (e *CloseSpiderError) Error() string {
	return fmt.Sprintf("close spider: %s", e.Reason)
}

// Is 实现 errors.Is 接口，使 CloseSpiderError 可以匹配 ErrCloseSpider。
func (e *CloseSpiderError) Is(target error) bool {
	return target == ErrCloseSpider
}

// NewCloseSpiderError 创建一个带原因的 CloseSpider 错误。
func NewCloseSpiderError(reason string) *CloseSpiderError {
	return &CloseSpiderError{Reason: reason}
}

// DropItemError 是带消息的 DropItem 错误。
// 使用 errors.Is(err, ErrDropItem) 可以匹配此错误。
type DropItemError struct {
	Message  string
	LogLevel string // 可选的日志级别覆盖
}

func (e *DropItemError) Error() string {
	return fmt.Sprintf("drop item: %s", e.Message)
}

// Is 实现 errors.Is 接口，使 DropItemError 可以匹配 ErrDropItem。
func (e *DropItemError) Is(target error) bool {
	return target == ErrDropItem
}

// NewDropItemError 创建一个带消息的 DropItem 错误。
func NewDropItemError(message string) *DropItemError {
	return &DropItemError{Message: message}
}

// StopDownloadError 是带 fail 标志的 StopDownload 错误。
// Fail 为 true 时，部分响应将传递给 errback；为 false 时传递给 callback。
type StopDownloadError struct {
	Fail bool
}

func (e *StopDownloadError) Error() string {
	if e.Fail {
		return "stop download (fail)"
	}
	return "stop download (no fail)"
}

// Is 实现 errors.Is 接口，使 StopDownloadError 可以匹配 ErrStopDownload。
func (e *StopDownloadError) Is(target error) bool {
	return target == ErrStopDownload
}

// NewStopDownloadError 创建一个 StopDownload 错误。
func NewStopDownloadError(fail bool) *StopDownloadError {
	return &StopDownloadError{Fail: fail}
}

// ErrNewRequest 是一个哨兵错误，表示中间件希望用一个新请求替代当前请求。
// 使用 errors.Is(err, ErrNewRequest) 可以匹配 NewRequestError。
var ErrNewRequest = errors.New("new request")

// NewRequestError 表示中间件需要将当前请求替换为一个新请求。
// 典型场景包括重试（RetryMiddleware）和重定向（RedirectMiddleware）。
//
// 当中间件的 ProcessResponse 或 ProcessException 返回此错误时，
// Manager 会将其传播给 Engine，由 Engine 将新请求重新调度到 Scheduler。
// 这种方式替代了之前通过 Meta 键（如 "_retry_request"、"_redirect_request"）
// 传递新请求的 hack 方式，更加类型安全且符合 Go 的错误处理惯例。
type NewRequestError struct {
	// Request 是需要重新调度的新请求。
	Request any // 实际类型为 *http.Request，使用 any 避免循环依赖
	// Reason 是产生新请求的原因（如 "retry"、"redirect"）。
	Reason string
}

func (e *NewRequestError) Error() string {
	return fmt.Sprintf("new request: %s", e.Reason)
}

// Is 实现 errors.Is 接口，使 NewRequestError 可以匹配 ErrNewRequest。
func (e *NewRequestError) Is(target error) bool {
	return target == ErrNewRequest
}

// NewNewRequestError 创建一个 NewRequestError。
// request 参数的实际类型应为 *http.Request。
func NewNewRequestError(request any, reason string) *NewRequestError {
	return &NewRequestError{Request: request, Reason: reason}
}

// NotConfiguredError 是带消息的 NotConfigured 错误。
type NotConfiguredError struct {
	Message string
}

func (e *NotConfiguredError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("not configured: %s", e.Message)
	}
	return "component not configured"
}

// Is 实现 errors.Is 接口。
func (e *NotConfiguredError) Is(target error) bool {
	return target == ErrNotConfigured
}

// NewNotConfiguredError 创建一个带消息的 NotConfigured 错误。
func NewNotConfiguredError(message string) *NotConfiguredError {
	return &NotConfiguredError{Message: message}
}

// ============================================================================
// 辅助函数
// ============================================================================

// IsRetryable 判断错误是否可重试。
// 以下错误类型被认为是可重试的：
//   - ErrDownloadTimeout
//   - ErrConnectionRefused
//   - ErrDownloadFailed
//   - ErrResponseDataLoss
//   - ErrCannotResolveHost
func IsRetryable(err error) bool {
	return errors.Is(err, ErrDownloadTimeout) ||
		errors.Is(err, ErrConnectionRefused) ||
		errors.Is(err, ErrDownloadFailed) ||
		errors.Is(err, ErrResponseDataLoss) ||
		errors.Is(err, ErrCannotResolveHost)
}
