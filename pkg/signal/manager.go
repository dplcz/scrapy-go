package signal

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
)

// ============================================================================
// Manager 实现
// ============================================================================

// Manager 管理信号的注册和分发。
// 对应 Scrapy 的 scrapy.signalmanager.Manager。
//
// 线程安全，所有操作通过 RWMutex 保护。
// hasHandlers 数组提供 O(1) 无锁快速路径检查。
type Manager struct {
	mu       sync.RWMutex
	handlers map[Signal][]handlerEntry
	logger   *slog.Logger

	// hasHandlers 使用固定大小数组缓存每个信号是否有处理器。
	// 通过 atomic 操作实现无锁读取，避免热路径上的 RLock 开销。
	// 数组索引对应 Signal 枚举值。
	handlerCounts [maxSignal]int32
}

// maxSignal 是信号枚举的最大值 + 1，用于固定大小数组。
const maxSignal = 32

// handlerEntry 存储处理器及其标识（用于 Disconnect）。
type handlerEntry struct {
	id      uint64
	handler Handler
}

// 全局处理器 ID 计数器
var (
	handlerIDMu   sync.Mutex
	nextHandlerID uint64
)

func getNextHandlerID() uint64 {
	handlerIDMu.Lock()
	defer handlerIDMu.Unlock()
	nextHandlerID++
	return nextHandlerID
}

// NewManager 创建一个新的信号管理器。
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		handlers: make(map[Signal][]handlerEntry),
		logger:   logger,
	}
}

// Connect 注册一个信号处理器。
// 返回一个处理器 ID，可用于后续 Disconnect。
func (sm *Manager) Connect(handler Handler, sig Signal) uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	id := getNextHandlerID()
	sm.handlers[sig] = append(sm.handlers[sig], handlerEntry{
		id:      id,
		handler: handler,
	})
	// 更新 atomic 计数器
	atomic.StoreInt32(&sm.handlerCounts[sig], int32(len(sm.handlers[sig])))
	return id
}

// Disconnect 通过处理器 ID 移除一个信号处理器。
func (sm *Manager) Disconnect(id uint64, sig Signal) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entries := sm.handlers[sig]
	for i, entry := range entries {
		if entry.id == id {
			sm.handlers[sig] = append(entries[:i], entries[i+1:]...)
			// 更新 atomic 计数器
			atomic.StoreInt32(&sm.handlerCounts[sig], int32(len(sm.handlers[sig])))
			return
		}
	}
}

// DisconnectAll 移除指定信号的所有处理器。
func (sm *Manager) DisconnectAll(sig Signal) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.handlers, sig)
	// 更新 atomic 计数器
	atomic.StoreInt32(&sm.handlerCounts[sig], 0)
}

// Send 同步发送信号，调用所有已注册的处理器。
// 返回所有处理器的返回值（包括错误）。
//
// 注意：即使某个处理器返回错误，后续处理器仍会被调用。
func (sm *Manager) Send(sig Signal, params map[string]any) []error {
	sm.mu.RLock()
	entries := sm.handlers[sig]
	n := len(entries)
	if n == 0 {
		sm.mu.RUnlock()
		return nil
	}

	// 快速路径：单处理器
	if n == 1 {
		h := entries[0].handler
		sm.mu.RUnlock()
		if err := h(params); err != nil {
			return []error{err}
		}
		return nil
	}

	// 多处理器：创建快照后释放锁
	snapshot := make([]handlerEntry, n)
	copy(snapshot, entries)
	sm.mu.RUnlock()

	var errs []error
	for _, entry := range snapshot {
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
// 优化：对于单处理器信号，直接在 RLock 下调用，避免快照分配。
func (sm *Manager) SendCatchLog(sig Signal, params map[string]any) []error {
	sm.mu.RLock()
	entries := sm.handlers[sig]
	n := len(entries)
	if n == 0 {
		sm.mu.RUnlock()
		return nil
	}

	// 快速路径：单处理器，直接在 RLock 下调用（避免快照分配）
	if n == 1 {
		h := entries[0].handler
		sm.mu.RUnlock()
		if err := h(params); err != nil {
			if errors.Is(err, serrors.ErrDontCloseSpider) ||
				errors.Is(err, serrors.ErrCloseSpider) {
				return []error{err}
			}
			sm.logger.Error("signal handler error",
				"signal", sig.String(),
				"error", err,
			)
			return []error{err}
		}
		return nil
	}

	// 多处理器：创建快照后释放锁
	snapshot := make([]handlerEntry, n)
	copy(snapshot, entries)
	sm.mu.RUnlock()

	var errs []error
	for _, entry := range snapshot {
		if err := entry.handler(params); err != nil {
			// DontCloseSpider 和 CloseSpider 是特殊错误，不记录为错误日志
			if errors.Is(err, serrors.ErrDontCloseSpider) ||
				errors.Is(err, serrors.ErrCloseSpider) {
				errs = append(errs, err)
				continue
			}

			sm.logger.Error("signal handler error",
				"signal", sig.String(),
				"error", err,
			)
			errs = append(errs, err)
		}
	}
	return errs
}

// SendCatchLogCtx 带 context 的信号发送，支持取消。
func (sm *Manager) SendCatchLogCtx(ctx context.Context, sig Signal, params map[string]any) []error {
	sm.mu.RLock()
	entries := sm.handlers[sig]
	n := len(entries)
	if n == 0 {
		sm.mu.RUnlock()
		return nil
	}

	// 快速路径：单处理器
	if n == 1 {
		h := entries[0].handler
		sm.mu.RUnlock()
		select {
		case <-ctx.Done():
			return []error{ctx.Err()}
		default:
			if err := h(params); err != nil {
				if errors.Is(err, serrors.ErrDontCloseSpider) ||
					errors.Is(err, serrors.ErrCloseSpider) {
					return []error{err}
				}
				sm.logger.Error("signal handler error",
					"signal", sig.String(),
					"error", err,
				)
				return []error{err}
			}
		}
		return nil
	}

	// 多处理器：创建快照后释放锁
	snapshot := make([]handlerEntry, n)
	copy(snapshot, entries)
	sm.mu.RUnlock()

	var errs []error
	for _, entry := range snapshot {
		select {
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
			return errs
		default:
			if err := entry.handler(params); err != nil {
				if errors.Is(err, serrors.ErrDontCloseSpider) ||
					errors.Is(err, serrors.ErrCloseSpider) {
					errs = append(errs, err)
					continue
				}

				sm.logger.Error("signal handler error",
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
// 使用 atomic 读取，无锁快速路径。
func (sm *Manager) HasHandlers(sig Signal) bool {
	if int(sig) >= maxSignal {
		// 超出预分配范围，回退到加锁检查
		sm.mu.RLock()
		defer sm.mu.RUnlock()
		return len(sm.handlers[sig]) > 0
	}
	return atomic.LoadInt32(&sm.handlerCounts[sig]) > 0
}

// HandlerCount 返回指定信号的处理器数量。
func (sm *Manager) HandlerCount(sig Signal) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.handlers[sig])
}

// ============================================================================
// 辅助方法
// ============================================================================

// getHandlers 获取指定信号的处理器快照（线程安全）。
func (sm *Manager) getHandlers(sig Signal) []handlerEntry {
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
		if errors.Is(err, serrors.ErrDontCloseSpider) {
			return true
		}
	}
	return false
}

// ContainsCloseSpider 检查错误列表中是否包含 ErrCloseSpider。
// 如果包含，返回 CloseSpiderError（含关闭原因）。
func ContainsCloseSpider(errs []error) *serrors.CloseSpiderError {
	for _, err := range errs {
		var closeErr *serrors.CloseSpiderError
		if errors.As(err, &closeErr) {
			return closeErr
		}
		if errors.Is(err, serrors.ErrCloseSpider) {
			return &serrors.CloseSpiderError{Reason: "cancelled"}
		}
	}
	return nil
}
