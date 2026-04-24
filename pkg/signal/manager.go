package signal

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	scrapy_errors "scrapy-go/pkg/errors"
)

// ============================================================================
// SignalManager 实现
// ============================================================================

// SignalManager 管理信号的注册和分发。
// 对应 Scrapy 的 scrapy.signalmanager.SignalManager。
//
// 线程安全，所有操作通过 RWMutex 保护。
type SignalManager struct {
	mu       sync.RWMutex
	handlers map[Signal][]handlerEntry
	logger   *slog.Logger
}

// handlerEntry 存储处理器及其标识（用于 Disconnect）。
type handlerEntry struct {
	id      uint64
	handler Handler
}

// 全局处理器 ID 计数器
var (
	handlerIDMu sync.Mutex
	nextHandlerID uint64
)

func getNextHandlerID() uint64 {
	handlerIDMu.Lock()
	defer handlerIDMu.Unlock()
	nextHandlerID++
	return nextHandlerID
}

// NewSignalManager 创建一个新的信号管理器。
func NewSignalManager(logger *slog.Logger) *SignalManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &SignalManager{
		handlers: make(map[Signal][]handlerEntry),
		logger:   logger,
	}
}

// Connect 注册一个信号处理器。
// 返回一个处理器 ID，可用于后续 Disconnect。
func (sm *SignalManager) Connect(handler Handler, sig Signal) uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	id := getNextHandlerID()
	sm.handlers[sig] = append(sm.handlers[sig], handlerEntry{
		id:      id,
		handler: handler,
	})
	return id
}

// Disconnect 通过处理器 ID 移除一个信号处理器。
func (sm *SignalManager) Disconnect(id uint64, sig Signal) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entries := sm.handlers[sig]
	for i, entry := range entries {
		if entry.id == id {
			sm.handlers[sig] = append(entries[:i], entries[i+1:]...)
			return
		}
	}
}

// DisconnectAll 移除指定信号的所有处理器。
func (sm *SignalManager) DisconnectAll(sig Signal) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.handlers, sig)
}

// Send 同步发送信号，调用所有已注册的处理器。
// 返回所有处理器的返回值（包括错误）。
//
// 注意：即使某个处理器返回错误，后续处理器仍会被调用。
func (sm *SignalManager) Send(sig Signal, params map[string]any) []error {
	handlers := sm.getHandlers(sig)

	var errs []error
	for _, entry := range handlers {
		if err := entry.handler(params); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// SendCatchLog 发送信号并捕获处理器错误，将错误记录到日志。
// 返回所有处理器的错误。
//
// 这是最常用的信号发送方法，对应 Scrapy 的 send_catch_log。
func (sm *SignalManager) SendCatchLog(sig Signal, params map[string]any) []error {
	handlers := sm.getHandlers(sig)

	var errs []error
	for _, entry := range handlers {
		if err := entry.handler(params); err != nil {
			// DontCloseSpider 和 CloseSpider 是特殊错误，不记录为错误日志
			if errors.Is(err, scrapy_errors.ErrDontCloseSpider) ||
				errors.Is(err, scrapy_errors.ErrCloseSpider) {
				errs = append(errs, err)
				continue
			}

			sm.logger.Error("信号处理器错误",
				"signal", sig.String(),
				"error", err,
			)
			errs = append(errs, err)
		}
	}
	return errs
}

// SendCatchLogCtx 带 context 的信号发送，支持取消。
func (sm *SignalManager) SendCatchLogCtx(ctx context.Context, sig Signal, params map[string]any) []error {
	handlers := sm.getHandlers(sig)

	var errs []error
	for _, entry := range handlers {
		select {
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
			return errs
		default:
			if err := entry.handler(params); err != nil {
				if errors.Is(err, scrapy_errors.ErrDontCloseSpider) ||
					errors.Is(err, scrapy_errors.ErrCloseSpider) {
					errs = append(errs, err)
					continue
				}

				sm.logger.Error("信号处理器错误",
					"signal", sig.String(),
					"error", err,
				)
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// HasHandlers 检查指定信号是否有已注册的处理器。
func (sm *SignalManager) HasHandlers(sig Signal) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.handlers[sig]) > 0
}

// HandlerCount 返回指定信号的处理器数量。
func (sm *SignalManager) HandlerCount(sig Signal) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.handlers[sig])
}

// ============================================================================
// 辅助方法
// ============================================================================

// getHandlers 获取指定信号的处理器快照（线程安全）。
func (sm *SignalManager) getHandlers(sig Signal) []handlerEntry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	entries := sm.handlers[sig]
	if len(entries) == 0 {
		return nil
	}

	// 返回快照，避免在遍历时被修改
	snapshot := make([]handlerEntry, len(entries))
	copy(snapshot, entries)
	return snapshot
}

// ============================================================================
// 辅助函数
// ============================================================================

// ContainsDontCloseSpider 检查错误列表中是否包含 ErrDontCloseSpider。
func ContainsDontCloseSpider(errs []error) bool {
	for _, err := range errs {
		if errors.Is(err, scrapy_errors.ErrDontCloseSpider) {
			return true
		}
	}
	return false
}

// ContainsCloseSpider 检查错误列表中是否包含 ErrCloseSpider。
// 如果包含，返回 CloseSpiderError（含关闭原因）。
func ContainsCloseSpider(errs []error) *scrapy_errors.CloseSpiderError {
	for _, err := range errs {
		var closeErr *scrapy_errors.CloseSpiderError
		if errors.As(err, &closeErr) {
			return closeErr
		}
		if errors.Is(err, scrapy_errors.ErrCloseSpider) {
			return &scrapy_errors.CloseSpiderError{Reason: "cancelled"}
		}
	}
	return nil
}
