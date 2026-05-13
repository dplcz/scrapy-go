package ratelimit

import (
	"context"
	"log/slog"
	"net/url"

	"github.com/dplcz/scrapy-go/pkg/signal"
)

// RateLimitExtension 是信号驱动的分布式限速扩展。
//
// 通过监听 RequestReachedDownloader 信号，在请求到达下载器时进行限速检查。
// 如果当前域名的请求速率超过限制，阻塞等待直到有可用配额。
//
// 工作流程：
//  1. Open — 注册 RequestReachedDownloader 信号处理器
//  2. 每次请求到达下载器时，提取请求的域名
//  3. 调用 RateLimiter.Wait 阻塞等待限速配额
//  4. Close — 注销信号处理器，关闭限速器
//
// 线程安全：所有方法均可被多个 goroutine 并发调用。
type RateLimitExtension struct {
	limiter RateLimiter
	signals *signal.Manager
	logger  *slog.Logger

	// handlerIDs 存储注册的信号处理器 ID，用于 Close 时注销
	handlerIDs []handlerRegistration
}

// handlerRegistration 存储信号处理器的注册信息。
type handlerRegistration struct {
	id  uint64
	sig signal.Signal
}

// NewRateLimitExtension 创建一个新的限速扩展。
//
// 参数：
//   - limiter: 限速器实现（如 RedisSlidingWindowLimiter）
//   - signals: 框架信号管理器
//   - logger: 日志记录器（nil 使用默认）
func NewRateLimitExtension(limiter RateLimiter, signals *signal.Manager, logger *slog.Logger) *RateLimitExtension {
	if logger == nil {
		logger = slog.Default()
	}
	return &RateLimitExtension{
		limiter: limiter,
		signals: signals,
		logger:  logger,
	}
}

// Open 注册信号处理器，启动限速。
func (e *RateLimitExtension) Open(ctx context.Context) error {
	if e.limiter == nil {
		e.logger.Warn("RateLimitExtension: limiter is nil, rate limiting disabled")
		return nil
	}

	e.connectSignal(signal.RequestReachedDownloader, e.onRequestReachedDownloader)

	e.logger.Info("RateLimitExtension enabled")
	return nil
}

// Close 注销所有信号处理器，关闭限速器。
func (e *RateLimitExtension) Close(ctx context.Context) error {
	// 注销信号处理器
	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil

	// 关闭限速器
	if e.limiter != nil {
		if err := e.limiter.Close(); err != nil {
			e.logger.Error("failed to close rate limiter", "error", err)
			return err
		}
	}

	return nil
}

// connectSignal 注册信号处理器并记录 ID。
func (e *RateLimitExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// onRequestReachedDownloader 在请求到达下载器时进行限速检查。
//
// 从信号参数中提取请求 URL，解析域名，然后调用限速器的 Wait 方法。
// 如果限速器返回错误（如 context 取消），记录警告日志但不阻止请求。
func (e *RateLimitExtension) onRequestReachedDownloader(params map[string]any) error {
	// 提取请求 URL
	rawURL, ok := params["url"].(string)
	if !ok || rawURL == "" {
		return nil
	}

	// 解析域名
	domain := extractDomain(rawURL)
	if domain == "" {
		return nil
	}

	// 阻塞等待限速配额
	ctx := context.Background()
	if err := e.limiter.Wait(ctx, domain); err != nil {
		e.logger.Warn("rate limit wait failed",
			"domain", domain,
			"error", err,
		)
		// 不返回错误，降级为不限速
		return nil
	}

	return nil
}

// extractDomain 从 URL 中提取域名。
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
