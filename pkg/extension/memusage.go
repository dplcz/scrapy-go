package extension

import (
	"context"
	"log/slog"
	"runtime"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// MemoryUsageExtension 监控 Go 运行时内存使用情况。
// 对应 Scrapy 的 scrapy.extensions.memusage.MemoryUsage。
//
// 使用 Go 的 runtime.MemStats 替代 Python 的 resource 模块，
// 监控 HeapAlloc（堆内存分配）和 Sys（系统内存占用）。
//
// 功能：
//   - 定期检查内存使用并更新统计
//   - 当内存超过警告阈值时记录警告日志（仅一次）
//   - 当内存超过限制阈值时请求关闭 Spider
//
// 统计项：
//   - memusage/startup — 启动时的内存使用（字节）
//   - memusage/max — 运行期间的最大内存使用（字节）
//   - memusage/limit_reached — 是否触发内存限制（1 表示触发）
//   - memusage/warning_reached — 是否触发内存警告（1 表示触发）
//
// 配置项：
//   - MEMUSAGE_ENABLED — 是否启用内存监控（默认 true）
//   - MEMUSAGE_LIMIT_MB — 内存限制（MB），0 表示不限制
//   - MEMUSAGE_WARNING_MB — 内存警告阈值（MB），0 表示不警告
//   - MEMUSAGE_CHECK_INTERVAL_SECONDS — 检查间隔（秒），默认 60.0
type MemoryUsageExtension struct {
	BaseExtension

	stats   stats.Collector
	signals *signal.Manager
	logger  *slog.Logger

	enabled       bool
	limitBytes    uint64
	warningBytes  uint64
	checkInterval float64

	warned  bool
	closing bool

	// cancelTicker 用于停止定期检查
	cancelTicker context.CancelFunc

	// handlerIDs 存储注册的信号处理器 ID
	handlerIDs []handlerRegistration
}

// NewMemoryUsageExtension 创建一个新的 MemoryUsage 扩展。
func NewMemoryUsageExtension(
	enabled bool,
	limitMB, warningMB int,
	checkInterval float64,
	sc stats.Collector,
	signals *signal.Manager,
	logger *slog.Logger,
) *MemoryUsageExtension {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryUsageExtension{
		stats:         sc,
		signals:       signals,
		logger:        logger,
		enabled:       enabled,
		limitBytes:    uint64(limitMB) * 1024 * 1024,
		warningBytes:  uint64(warningMB) * 1024 * 1024,
		checkInterval: checkInterval,
	}
}

// Open 检查配置并注册信号处理器。
func (e *MemoryUsageExtension) Open(ctx context.Context) error {
	if !e.enabled {
		return serrors.ErrNotConfigured
	}

	e.connectSignal(signal.EngineStarted, e.onEngineStarted)
	e.connectSignal(signal.EngineStopped, e.onEngineStopped)
	return nil
}

// Close 注销所有信号处理器并停止定期检查。
func (e *MemoryUsageExtension) Close(ctx context.Context) error {
	if e.cancelTicker != nil {
		e.cancelTicker()
		e.cancelTicker = nil
	}

	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil
	return nil
}

// connectSignal 注册信号处理器并记录 ID。
func (e *MemoryUsageExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// onEngineStarted 记录启动内存并启动定期检查。
func (e *MemoryUsageExtension) onEngineStarted(params map[string]any) error {
	startupMem := getMemoryUsage()
	e.stats.SetValue("memusage/startup", startupMem)

	ctx, cancel := context.WithCancel(context.Background())
	e.cancelTicker = cancel

	go e.checkLoop(ctx)
	return nil
}

// onEngineStopped 停止定期检查。
func (e *MemoryUsageExtension) onEngineStopped(params map[string]any) error {
	if e.cancelTicker != nil {
		e.cancelTicker()
		e.cancelTicker = nil
	}
	return nil
}

// checkLoop 定期检查内存使用。
func (e *MemoryUsageExtension) checkLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(e.checkInterval * float64(time.Second)))
	defer ticker.Stop()

	// 立即执行一次检查
	e.update()
	e.checkLimit()
	e.checkWarning()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.update()
			e.checkLimit()
			e.checkWarning()
		}
	}
}

// update 更新最大内存使用统计。
func (e *MemoryUsageExtension) update() {
	currentMem := getMemoryUsage()
	e.stats.MaxValue("memusage/max", currentMem)
}

// checkLimit 检查是否超过内存限制。
func (e *MemoryUsageExtension) checkLimit() {
	if e.limitBytes == 0 || e.closing {
		return
	}

	currentMem := getMemoryUsage()
	if currentMem > e.limitBytes {
		e.closing = true
		e.stats.SetValue("memusage/limit_reached", 1)

		limitMB := e.limitBytes / 1024 / 1024
		currentMB := currentMem / 1024 / 1024
		e.logger.Error("memory usage exceeded limit, shutting down",
			"limit_mb", limitMB,
			"current_mb", currentMB,
		)

		// 通过信号请求关闭
		e.signals.SendCatchLog(signal.SpiderIdle, map[string]any{
			"close_reason": "memusage_exceeded",
		})
	}
}

// checkWarning 检查是否超过内存警告阈值。
func (e *MemoryUsageExtension) checkWarning() {
	if e.warningBytes == 0 || e.warned {
		return
	}

	currentMem := getMemoryUsage()
	if currentMem > e.warningBytes {
		e.warned = true
		e.stats.SetValue("memusage/warning_reached", 1)

		warningMB := e.warningBytes / 1024 / 1024
		currentMB := currentMem / 1024 / 1024
		e.logger.Warn("memory usage reached warning threshold",
			"warning_mb", warningMB,
			"current_mb", currentMB,
		)
	}
}

// getMemoryUsage 获取当前 Go 运行时的堆内存分配量（字节）。
// 使用 runtime.MemStats.Sys 作为内存使用指标，
// 它表示从操作系统获取的总内存量，更接近进程实际内存占用。
func getMemoryUsage() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Sys
}
