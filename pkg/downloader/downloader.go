package downloader

import (
	"context"
	"log/slog"
	"net/url"
	"sync"
	"time"

	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/settings"
	"scrapy-go/pkg/signal"
	"scrapy-go/pkg/stats"
)

const (
	// DownloadSlotMetaKey 是请求 Meta 中存储 Slot key 的键名。
	DownloadSlotMetaKey = "download_slot"

	// 默认 Slot GC 间隔（秒）
	defaultSlotGCInterval = 60 * time.Second
)

// Downloader 管理 HTTP 下载，通过 Slot 机制控制并发和延迟。
//
// 核心调度策略（对齐 Scrapy 原版）：
//   - 每个域名/IP 对应一个 Slot
//   - Slot 内部通过队列驱动串行出队，用 lastSeen 时间戳精确控制请求间隔
//   - 不同 Slot 之间完全并行
//   - 全局通过 active 集合控制总并发数
//
// 对应 Scrapy 的 Downloader 类。
type Downloader struct {
	mu sync.RWMutex

	handler           DownloadHandler
	slots             map[string]*Slot
	active            map[*scrapy_http.Request]struct{}
	totalConcurrency  int
	domainConcurrency int
	randomizeDelay    bool
	downloadDelay     time.Duration
	signals           *signal.Manager
	stats             stats.Collector
	logger            *slog.Logger

	gcTicker *time.Ticker
	gcDone   chan struct{}
}

// NewDownloader 创建一个新的下载器。
func NewDownloader(s *settings.Settings, handler DownloadHandler, signals *signal.Manager, sc stats.Collector, logger *slog.Logger) *Downloader {
	if handler == nil {
		timeout := s.GetDuration("DOWNLOAD_TIMEOUT", 180*time.Second)
		handler = NewHTTPDownloadHandler(timeout)
	}
	if signals == nil {
		signals = signal.NewManager(nil)
	}
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Downloader{
		handler:           handler,
		slots:             make(map[string]*Slot),
		active:            make(map[*scrapy_http.Request]struct{}),
		totalConcurrency:  s.GetInt("CONCURRENT_REQUESTS", 16),
		domainConcurrency: s.GetInt("CONCURRENT_REQUESTS_PER_DOMAIN", 8),
		randomizeDelay:    s.GetBool("RANDOMIZE_DOWNLOAD_DELAY", true),
		downloadDelay:     s.GetDuration("DOWNLOAD_DELAY", 0),
		signals:           signals,
		stats:             sc,
		logger:            logger,
	}
}

// Download 执行下载请求。
//
// 处理流程：
//  1. 获取或创建请求对应的 Slot
//  2. 将请求入队到 Slot 的队列中
//  3. Slot 的 processQueue goroutine 负责：
//     a. 计算并等待延迟（基于 lastSeen 时间戳）
//     b. 获取传输信号量（控制并发）
//     c. 执行实际下载
//  4. 等待下载结果返回
func (d *Downloader) Download(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	// 获取或创建 Slot
	key, slot := d.getSlot(request)
	request.SetMeta(DownloadSlotMetaKey, key)

	// 添加到活跃集合
	d.addActive(request)
	slot.AddActive(request)
	defer func() {
		slot.RemoveActive(request)
		d.removeActive(request)
	}()

	// 发送 request_reached_downloader 信号
	d.signals.SendCatchLog(signal.RequestReachedDownloader, map[string]any{
		"request": request,
	})

	// 将请求入队到 Slot，等待结果
	// Slot 内部的 processQueue 会负责延迟控制和并发控制
	response, err := slot.Enqueue(ctx, request)

	// 发送 request_left_downloader 信号
	d.signals.SendCatchLog(signal.RequestLeftDownloader, map[string]any{
		"request": request,
	})

	if err != nil {
		return nil, err
	}

	// 发送 response_downloaded 信号
	d.signals.SendCatchLog(signal.ResponseDownloaded, map[string]any{
		"response": response,
		"request":  request,
	})

	return response, nil
}

// NeedsBackout 检查是否需要回退（活跃请求数达到总并发上限）。
func (d *Downloader) NeedsBackout() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.active) >= d.totalConcurrency
}

// ActiveCount 返回当前活跃请求数。
func (d *Downloader) ActiveCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.active)
}

// Close 关闭下载器，释放所有资源。
func (d *Downloader) Close() error {
	// 停止 GC
	if d.gcTicker != nil {
		d.gcTicker.Stop()
		close(d.gcDone)
	}

	// 关闭所有 Slot
	d.mu.Lock()
	for _, slot := range d.slots {
		slot.Close()
	}
	d.slots = make(map[string]*Slot)
	d.mu.Unlock()

	// 关闭 handler
	return d.handler.Close()
}

// StartSlotGC 启动 Slot 垃圾回收。
// 定期清理空闲超过指定时间的 Slot。
func (d *Downloader) StartSlotGC() {
	d.gcTicker = time.NewTicker(defaultSlotGCInterval)
	d.gcDone = make(chan struct{})

	go func() {
		for {
			select {
			case <-d.gcDone:
				return
			case <-d.gcTicker.C:
				d.slotGC(defaultSlotGCInterval)
			}
		}
	}()
}

// ============================================================================
// 内部方法
// ============================================================================

// getSlot 获取或创建请求对应的 Slot。
func (d *Downloader) getSlot(request *scrapy_http.Request) (string, *Slot) {
	key := d.getSlotKey(request)

	d.mu.Lock()
	defer d.mu.Unlock()

	if slot, ok := d.slots[key]; ok {
		return key, slot
	}

	// 创建新 Slot，注入实际下载函数
	slot := NewSlot(d.domainConcurrency, d.downloadDelay, d.randomizeDelay, func(ctx context.Context, req *scrapy_http.Request) (*scrapy_http.Response, error) {
		return d.handler.Download(ctx, req)
	})
	d.slots[key] = slot
	return key, slot
}

// getSlotKey 获取请求的 Slot key。
// 优先使用 Meta 中的 download_slot，否则使用域名。
func (d *Downloader) getSlotKey(request *scrapy_http.Request) string {
	if v, ok := request.GetMeta(DownloadSlotMetaKey); ok {
		if key, ok := v.(string); ok {
			return key
		}
	}
	return getHostname(request.URL)
}

// addActive 将请求添加到全局活跃集合。
func (d *Downloader) addActive(request *scrapy_http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.active[request] = struct{}{}
}

// removeActive 从全局活跃集合中移除请求。
func (d *Downloader) removeActive(request *scrapy_http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.active, request)
}

// slotGC 清理空闲超过指定时间的 Slot。
func (d *Downloader) slotGC(maxAge time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for key, slot := range d.slots {
		if slot.IsIdle() && slot.LastSeen().Before(cutoff) {
			slot.Close()
			delete(d.slots, key)
			d.logger.Debug("回收空闲 Slot",
				"slot", key,
			)
		}
	}
}

// getHostname 从 URL 中提取主机名。
func getHostname(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Hostname()
}
