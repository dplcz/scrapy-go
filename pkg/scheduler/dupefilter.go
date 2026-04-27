package scheduler

import (
	"context"
	"log/slog"
	"sync"

	scrapy_http "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/internal/utils"
)

// ============================================================================
// DupeFilter 接口
// ============================================================================

// DupeFilter 定义去重过滤器接口。
// 去重过滤器负责检查请求是否已经被处理过，防止重复爬取。
//
// 对应 Scrapy 的 BaseDupeFilter 抽象类。
type DupeFilter interface {
	// Open 初始化过滤器，在 Spider 打开时调用。
	Open(ctx context.Context) error

	// Close 关闭过滤器，在 Spider 关闭时调用。
	Close(reason string) error

	// RequestSeen 检查请求是否已见过。
	// 如果是新请求，记录并返回 false。
	// 如果是重复请求，返回 true。
	RequestSeen(request *scrapy_http.Request) bool
}

// ============================================================================
// RFPDupeFilter 实现（Request Fingerprint DupeFilter）
// ============================================================================

// RFPDupeFilter 基于请求指纹的去重过滤器。
// 使用请求的 URL（规范化后）、Method 和 Body 计算 SHA1 指纹进行去重。
//
// 对应 Scrapy 的 RFPDupeFilter 类。
type RFPDupeFilter struct {
	mu           sync.Mutex
	fingerprints map[string]struct{}
	logger       *slog.Logger
	debug        bool // 是否输出调试日志（对应 DUPEFILTER_DEBUG）
	logDupes     bool // 是否已输出过重复日志（仅输出一次提示）
}

// NewRFPDupeFilter 创建一个新的基于请求指纹的去重过滤器。
//
// 参数：
//   - logger: 日志记录器，为 nil 时使用默认 logger
//   - debug: 是否输出每个被过滤请求的调试日志
func NewRFPDupeFilter(logger *slog.Logger, debug bool) *RFPDupeFilter {
	if logger == nil {
		logger = slog.Default()
	}
	return &RFPDupeFilter{
		fingerprints: make(map[string]struct{}),
		logger:       logger,
		debug:        debug,
		logDupes:     true,
	}
}

// Open 初始化过滤器。
func (f *RFPDupeFilter) Open(ctx context.Context) error {
	// 内存过滤器无需特殊初始化
	return nil
}

// Close 关闭过滤器。
func (f *RFPDupeFilter) Close(reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// 清空指纹集合释放内存
	f.fingerprints = nil
	return nil
}

// RequestSeen 检查请求是否已见过。
//
// 使用 RequestFingerprint 计算请求指纹，如果指纹已存在于集合中，
// 则认为是重复请求。否则将指纹加入集合并返回 false。
func (f *RFPDupeFilter) RequestSeen(request *scrapy_http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	fp := f.requestFingerprint(request)

	if _, exists := f.fingerprints[fp]; exists {
		f.logDupe(request)
		return true
	}

	f.fingerprints[fp] = struct{}{}
	return false
}

// SeenCount 返回已记录的指纹数量（用于统计和测试）。
func (f *RFPDupeFilter) SeenCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.fingerprints)
}

// requestFingerprint 计算请求的指纹。
func (f *RFPDupeFilter) requestFingerprint(request *scrapy_http.Request) string {
	return utils.RequestFingerprint(request, nil, false)
}

// logDupe 记录重复请求的日志。
// 在 debug 模式下记录每个重复请求；否则仅记录一次提示。
func (f *RFPDupeFilter) logDupe(request *scrapy_http.Request) {
	if f.debug {
		f.logger.Debug("filtered duplicate request",
			"request", request.String(),
		)
	} else if f.logDupes {
		f.logger.Debug("filtered duplicate request (further duplicates will not be shown, set DUPEFILTER_DEBUG=true to see all)",
			"request", request.String(),
		)
		f.logDupes = false
	}
}

// ============================================================================
// NoDupeFilter 实现（不过滤）
// ============================================================================

// NoDupeFilter 是一个不进行任何过滤的去重过滤器。
// 所有请求都被视为新请求。
// 对应 Scrapy 的 BaseDupeFilter（默认不过滤）。
type NoDupeFilter struct{}

// NewNoDupeFilter 创建一个不过滤的去重过滤器。
func NewNoDupeFilter() *NoDupeFilter {
	return &NoDupeFilter{}
}

func (f *NoDupeFilter) Open(ctx context.Context) error              { return nil }
func (f *NoDupeFilter) Close(reason string) error                   { return nil }
func (f *NoDupeFilter) RequestSeen(request *scrapy_http.Request) bool { return false }
