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
// 线程安全：写操作（Connect/Disconnect）通过 Mutex 保护，
// 读操作（Send/SendCatchLog）通过 atomic.Value 实现零锁快速路径（COW 策略）。
// hasHandlers 数组提供 O(1) 无锁快速路径检查。
type Manager struct {
	mu     sync.Mutex // 仅保护写操作（Connect/Disconnect）
	logger *slog.Logger

	// handlers 使用 COW（Copy-on-Write）策略存储每个信号的处理器列表。
	// 每个 atomic.Value 存储 []handlerEntry 类型的不可变快照。
	// Connect/Disconnect 时创建新副本并原子替换，读取时零锁。
	handlers [maxSignal]atomic.Value

	// handlerCounts 使用固定大小数组缓存每个信号是否有处理器。
	// 通过 atomic 操作实现无锁读取，避免热路径上的锁开销。
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
		logger: logger,
	}
}

// loadHandlers 从 atomic.Value 中加载处理器列表（零锁快速路径）。
func (sm *Manager) loadHandlers(sig Signal) []handlerEntry {
	if int(sig) >= maxSignal {
		return nil
	}
	v := sm.handlers[sig].Load()
	if v == nil {
		return nil
	}
	return v.([]handlerEntry)
}

// storeHandlers 将处理器列表存储到 atomic.Value 中（COW 写入路径）。
// 调用者必须持有 sm.mu 锁。
func (sm *Manager) storeHandlers(sig Signal, entries []handlerEntry) {
	if int(sig) >= maxSignal {
		return
	}
	sm.handlers[sig].Store(entries)
	atomic.StoreInt32(&sm.handlerCounts[sig], int32(len(entries)))
}

// Connect 注册一个信号处理器。
// 返回一个处理器 ID，可用于后续 Disconnect。
// COW 策略：创建新的 slice 副本并原子替换，不影响正在读取的 goroutine。
func (sm *Manager) Connect(handler Handler, sig Signal) uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	id := getNextHandlerID()

	// COW：读取当前列表，创建新副本并追加
	current := sm.loadHandlers(sig)
	newEntries := make([]handlerEntry, len(current)+1)
	copy(newEntries, current)
	newEntries[len(current)] = handlerEntry{
		id:      id,
		handler: handler,
	}

	// 原子替换
	sm.storeHandlers(sig, newEntries)
	return id
}

// Disconnect 通过处理器 ID 移除一个信号处理器。
// COW 策略：创建新的 slice 副本（排除目标项）并原子替换。
func (sm *Manager) Disconnect(id uint64, sig Signal) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	current := sm.loadHandlers(sig)
	for i, entry := range current {
		if entry.id == id {
			// COW：创建新副本，排除目标项
			newEntries := make([]handlerEntry, 0, len(current)-1)
			newEntries = append(newEntries, current[:i]...)
			newEntries = append(newEntries, current[i+1:]...)
			sm.storeHandlers(sig, newEntries)
			return
		}
	}
}

// DisconnectAll 移除指定信号的所有处理器。
func (sm *Manager) DisconnectAll(sig Signal) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.storeHandlers(sig, nil)
}

// Send 同步发送信号，调用所有已注册的处理器。
// 返回所有处理器的返回值（包括错误）。
//
// 注意：即使某个处理器返回错误，后续处理器仍会被调用。
// COW 优化：直接通过 atomic.Value 读取处理器列表，零锁开销。
func (sm *Manager) Send(sig Signal, params map[string]any) []error {
	entries := sm.loadHandlers(sig)
	n := len(entries)
	if n == 0 {
		return nil
	}

	// 快速路径：单处理器
	if n == 1 {
		if err := entries[0].handler(params); err != nil {
			return []error{err}
		}
		return nil
	}

	// 多处理器：COW 快照已是不可变的，无需额外复制
	var errs []error
	for _, entry := range entries {
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
// COW 优化：直接通过 atomic.Value 读取处理器列表，零锁开销，无需快照分配。
func (sm *Manager) SendCatchLog(sig Signal, params map[string]any) []error {
	entries := sm.loadHandlers(sig)
	n := len(entries)
	if n == 0 {
		return nil
	}

	// 快速路径：单处理器
	if n == 1 {
		if err := entries[0].handler(params); err != nil {
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

	// 多处理器：COW 快照已是不可变的，无需额外复制
	var errs []error
	for _, entry := range entries {
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
// COW 优化：直接通过 atomic.Value 读取处理器列表，零锁开销。
func (sm *Manager) SendCatchLogCtx(ctx context.Context, sig Signal, params map[string]any) []error {
	entries := sm.loadHandlers(sig)
	n := len(entries)
	if n == 0 {
		return nil
	}

	// 快速路径：单处理器
	if n == 1 {
		select {
		case <-ctx.Done():
			return []error{ctx.Err()}
		default:
			if err := entries[0].handler(params); err != nil {
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

	// 多处理器：COW 快照已是不可变的，无需额外复制
	var errs []error
	for _, entry := range entries {
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
		return false
	}
	return atomic.LoadInt32(&sm.handlerCounts[sig]) > 0
}

// HandlerCount 返回指定信号的处理器数量。
func (sm *Manager) HandlerCount(sig Signal) int {
	entries := sm.loadHandlers(sig)
	return len(entries)
}

// ============================================================================
// 辅助方法
// ============================================================================

// getHandlers 获取指定信号的处理器快照（线程安全，零锁）。
// COW 策略下，返回的 slice 是不可变快照，无需额外复制。
func (sm *Manager) getHandlers(sig Signal) []handlerEntry {
	return sm.loadHandlers(sig)
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