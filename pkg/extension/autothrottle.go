package extension

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// AutoThrottleExtension 基于延迟反馈的自适应速率调整扩展。
// 对应 Scrapy 的 scrapy.extensions.throttle.AutoThrottle。
//
// 核心算法：
//   - 监听 ResponseReceived 信号，获取每个响应的下载延迟
//   - 使用指数加权移动平均（EWMA）平滑延迟抖动
//   - 根据目标并发数和实际延迟动态计算理想下载延迟
//   - 将计算出的延迟应用到对应域名的下载 Slot
//
// 算法公式（对齐 Scrapy 原版）：
//
//	latency = EWMA(response_latency)
//	target_delay = latency / target_concurrency
//	new_delay = (old_delay + target_delay) / 2.0
//	new_delay = clamp(new_delay, start_delay * MIN_FACTOR, max_delay)
//
// 配置项：
//   - AUTOTHROTTLE_ENABLED — 是否启用自适应限速（默认 false）
//   - AUTOTHROTTLE_START_DELAY — 初始下载延迟（秒），默认 5.0
//   - AUTOTHROTTLE_MAX_DELAY — 最大下载延迟（秒），默认 60.0
//   - AUTOTHROTTLE_TARGET_CONCURRENCY — 目标并发数，默认 1.0
//   - AUTOTHROTTLE_DEBUG — 是否输出调试日志（默认 false）
//
// 统计项：
//   - autothrottle/request_count — 已处理的请求总数
//   - autothrottle/latency_avg — 当前 EWMA 平滑延迟（秒）
//   - autothrottle/delay_adjusted_count — 延迟调整次数
//
// 与 Scrapy 的差异：
//   - 使用 Go 的 sync.Mutex 替代 Python GIL 保护共享状态
//   - 通过 DelayAdjuster 接口回调调整 Slot 延迟，而非直接操作 Slot 对象
//   - 使用 time.Duration 替代 float64 秒数，精度更高
//   - 延迟下限使用 startDelay * minDelayFactor（0.2），避免延迟降为零
type AutoThrottleExtension struct {
	BaseExtension

	signals *signal.Manager
	stats   stats.Collector
	logger  *slog.Logger

	// 配置参数
	enabled           bool
	startDelay        time.Duration
	maxDelay          time.Duration
	targetConcurrency float64
	debug             bool

	// 运行时状态（受 mu 保护）
	mu sync.Mutex
	// slotLatencies 存储每个 Slot（域名）的 EWMA 平滑延迟
	slotLatencies map[string]*slotThrottleState

	// 统计计数器
	requestCount       int64
	delayAdjustedCount int64

	// delayAdjuster 用于回调调整 Slot 的下载延迟
	delayAdjuster DelayAdjuster

	// handlerIDs 存储注册的信号处理器 ID
	handlerIDs []handlerRegistration
}

// slotThrottleState 存储单个 Slot 的限速状态。
type slotThrottleState struct {
	// latencyEWMA 是指数加权移动平均延迟
	latencyEWMA time.Duration
	// currentDelay 是当前应用的下载延迟
	currentDelay time.Duration
}

// DelayAdjuster 定义延迟调整回调接口。
// 由 Downloader 实现，用于将 AutoThrottle 计算出的延迟应用到对应 Slot。
type DelayAdjuster interface {
	// AdjustDelay 调整指定 Slot 的下载延迟。
	// slotKey 为域名或自定义 Slot 标识。
	AdjustDelay(slotKey string, delay time.Duration)
}

// DelayAdjusterFunc 是 DelayAdjuster 的函数适配器。
type DelayAdjusterFunc func(slotKey string, delay time.Duration)

// AdjustDelay 实现 DelayAdjuster 接口。
func (f DelayAdjusterFunc) AdjustDelay(slotKey string, delay time.Duration) {
	f(slotKey, delay)
}

const (
	// minDelayFactor 是延迟下限因子。
	// 延迟不会低于 startDelay * minDelayFactor，避免延迟降为零导致服务器过载。
	// 对齐 Scrapy 的 AUTOTHROTTLE_START_DELAY * 0.2 下限。
	minDelayFactor = 0.2

	// ewmaAlpha 是 EWMA 平滑系数。
	// 值越大，新样本权重越高，响应越灵敏但波动越大。
	// 对齐 Scrapy 默认行为：new_avg = (old_avg + new_sample) / 2.0
	// 等价于 alpha = 0.5。
	ewmaAlpha = 0.5
)

// NewAutoThrottleExtension 创建一个新的 AutoThrottle 扩展。
//
// 参数：
//   - enabled: 是否启用自适应限速
//   - startDelay: 初始下载延迟（秒）
//   - maxDelay: 最大下载延迟（秒）
//   - targetConcurrency: 目标并发数（每个域名）
//   - debug: 是否输出调试日志
//   - adjuster: 延迟调整回调（由 Downloader 提供）
//   - signals: 信号管理器
//   - sc: 统计收集器
//   - logger: 日志记录器
func NewAutoThrottleExtension(
	enabled bool,
	startDelay, maxDelay float64,
	targetConcurrency float64,
	debug bool,
	adjuster DelayAdjuster,
	signals *signal.Manager,
	sc stats.Collector,
	logger *slog.Logger,
) *AutoThrottleExtension {
	if logger == nil {
		logger = slog.Default()
	}
	if targetConcurrency <= 0 {
		targetConcurrency = 1.0
	}
	if startDelay <= 0 {
		startDelay = 5.0
	}
	if maxDelay <= 0 {
		maxDelay = 60.0
	}
	// 确保 maxDelay >= startDelay
	if maxDelay < startDelay {
		maxDelay = startDelay
	}

	return &AutoThrottleExtension{
		signals:           signals,
		stats:             sc,
		logger:            logger,
		enabled:           enabled,
		startDelay:        time.Duration(startDelay * float64(time.Second)),
		maxDelay:          time.Duration(maxDelay * float64(time.Second)),
		targetConcurrency: targetConcurrency,
		debug:             debug,
		delayAdjuster:     adjuster,
		slotLatencies:     make(map[string]*slotThrottleState),
	}
}

// Open 检查配置并注册信号处理器。
// 如果未启用 AutoThrottle，返回 ErrNotConfigured。
func (e *AutoThrottleExtension) Open(ctx context.Context) error {
	if !e.enabled {
		return serrors.ErrNotConfigured
	}

	e.connectSignal(signal.ResponseDownloaded, e.onResponseDownloaded)
	e.connectSignal(signal.SpiderOpened, e.onSpiderOpened)

	e.logger.Info("AutoThrottle extension enabled",
		"start_delay", e.startDelay,
		"max_delay", e.maxDelay,
		"target_concurrency", e.targetConcurrency,
		"debug", e.debug,
	)

	return nil
}

// Close 注销所有信号处理器并清理状态。
func (e *AutoThrottleExtension) Close(ctx context.Context) error {
	for _, reg := range e.handlerIDs {
		e.signals.Disconnect(reg.id, reg.sig)
	}
	e.handlerIDs = nil

	// 更新最终统计
	e.mu.Lock()
	e.stats.SetValue("autothrottle/request_count", e.requestCount)
	e.stats.SetValue("autothrottle/delay_adjusted_count", e.delayAdjustedCount)
	e.mu.Unlock()

	return nil
}

// connectSignal 注册信号处理器并记录 ID。
func (e *AutoThrottleExtension) connectSignal(sig signal.Signal, handler signal.Handler) {
	id := e.signals.Connect(handler, sig)
	e.handlerIDs = append(e.handlerIDs, handlerRegistration{id: id, sig: sig})
}

// onSpiderOpened 初始化统计项。
func (e *AutoThrottleExtension) onSpiderOpened(params map[string]any) error {
	e.stats.SetValue("autothrottle/request_count", int64(0))
	e.stats.SetValue("autothrottle/latency_avg", 0.0)
	e.stats.SetValue("autothrottle/delay_adjusted_count", int64(0))
	return nil
}

// onResponseDownloaded 处理响应下载完成信号，调整下载延迟。
//
// 从信号参数中提取 request 和 response，计算下载延迟，
// 然后使用 EWMA 平滑延迟并调整对应 Slot 的下载延迟。
func (e *AutoThrottleExtension) onResponseDownloaded(params map[string]any) error {
	if params == nil {
		return nil
	}

	// 提取 request 和 response
	reqVal, hasReq := params["request"]
	respVal, hasResp := params["response"]
	if !hasReq || !hasResp {
		return nil
	}

	request, ok := reqVal.(*shttp.Request)
	if !ok || request == nil {
		return nil
	}

	response, ok := respVal.(*shttp.Response)
	if !ok || response == nil {
		return nil
	}

	// 获取 Slot key（域名）
	slotKey := e.getSlotKey(request)
	if slotKey == "" {
		return nil
	}

	// 计算下载延迟
	latency := e.getResponseLatency(request, response)
	if latency <= 0 {
		return nil
	}

	// 调整延迟
	e.adjustDelay(slotKey, latency)

	return nil
}

// getSlotKey 获取请求对应的 Slot key。
// 优先使用 Meta 中的 download_slot，否则使用 URL 的主机名。
func (e *AutoThrottleExtension) getSlotKey(request *shttp.Request) string {
	if v, ok := request.GetMeta("download_slot"); ok {
		if key, ok := v.(string); ok && key != "" {
			return key
		}
	}
	if request.URL != nil {
		return request.URL.Hostname()
	}
	return ""
}

// getResponseLatency 计算响应的下载延迟。
// 使用 Request Meta 中记录的 download_latency。
// 该值由下载器在完成下载后设置。
func (e *AutoThrottleExtension) getResponseLatency(request *shttp.Request, response *shttp.Response) time.Duration {
	// 使用 Request Meta 中的 download_latency
	if v, ok := request.GetMeta("download_latency"); ok {
		switch latency := v.(type) {
		case time.Duration:
			return latency
		case float64:
			return time.Duration(latency * float64(time.Second))
		}
	}

	return 0
}

// adjustDelay 根据响应延迟调整 Slot 的下载延迟。
//
// 算法（对齐 Scrapy 原版 AutoThrottle）：
//  1. 使用 EWMA 平滑延迟：latency_ewma = alpha * new_latency + (1 - alpha) * old_latency
//  2. 计算目标延迟：target_delay = latency_ewma / target_concurrency
//  3. 平滑过渡：new_delay = (old_delay + target_delay) / 2.0
//  4. 限制范围：new_delay = clamp(new_delay, start_delay * MIN_FACTOR, max_delay)
func (e *AutoThrottleExtension) adjustDelay(slotKey string, latency time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.requestCount++

	// 获取或创建 Slot 状态
	state, exists := e.slotLatencies[slotKey]
	if !exists {
		state = &slotThrottleState{
			latencyEWMA:  latency,
			currentDelay: e.startDelay,
		}
		e.slotLatencies[slotKey] = state
	} else {
		// EWMA 平滑延迟
		state.latencyEWMA = time.Duration(
			ewmaAlpha*float64(latency) + (1-ewmaAlpha)*float64(state.latencyEWMA),
		)
	}

	oldDelay := state.currentDelay

	// 计算目标延迟
	targetDelay := time.Duration(float64(state.latencyEWMA) / e.targetConcurrency)

	// 平滑过渡：new_delay = (old_delay + target_delay) / 2.0
	newDelay := (oldDelay + targetDelay) / 2

	// 限制范围
	minDelay := time.Duration(float64(e.startDelay) * minDelayFactor)
	if newDelay < minDelay {
		newDelay = minDelay
	}
	if newDelay > e.maxDelay {
		newDelay = e.maxDelay
	}

	state.currentDelay = newDelay
	e.delayAdjustedCount++

	// 更新统计
	e.stats.SetValue("autothrottle/request_count", e.requestCount)
	e.stats.SetValue("autothrottle/latency_avg", state.latencyEWMA.Seconds())

	// 应用延迟调整
	if e.delayAdjuster != nil {
		e.delayAdjuster.AdjustDelay(slotKey, newDelay)
	}

	// 调试日志
	if e.debug {
		e.logger.Debug(fmt.Sprintf(
			"AutoThrottle: slot=%s latency=%.3fs ewma=%.3fs old_delay=%.3fs new_delay=%.3fs target_concurrency=%.1f",
			slotKey,
			latency.Seconds(),
			state.latencyEWMA.Seconds(),
			oldDelay.Seconds(),
			newDelay.Seconds(),
			e.targetConcurrency,
		))
	}
}

// GetSlotDelay 返回指定 Slot 的当前延迟（用于测试和监控）。
// 如果 Slot 不存在，返回 startDelay。
func (e *AutoThrottleExtension) GetSlotDelay(slotKey string) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()

	if state, ok := e.slotLatencies[slotKey]; ok {
		return state.currentDelay
	}
	return e.startDelay
}

// GetSlotLatencyEWMA 返回指定 Slot 的 EWMA 平滑延迟（用于测试和监控）。
// 如果 Slot 不存在，返回 0。
func (e *AutoThrottleExtension) GetSlotLatencyEWMA(slotKey string) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()

	if state, ok := e.slotLatencies[slotKey]; ok {
		return state.latencyEWMA
	}
	return 0
}

// SlotCount 返回当前跟踪的 Slot 数量（用于测试和监控）。
func (e *AutoThrottleExtension) SlotCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.slotLatencies)
}

// clampDuration 将 duration 限制在 [min, max] 范围内。
// 此函数未使用但保留作为工具函数。
func clampDuration(d, min, max time.Duration) time.Duration {
	if d < min {
		return min
	}
	if d > max {
		return max
	}
	return d
}

// roundDuration 将 duration 四舍五入到毫秒精度，减少日志噪声。
func roundDuration(d time.Duration) time.Duration {
	return time.Duration(math.Round(float64(d)/float64(time.Millisecond))) * time.Millisecond
}
