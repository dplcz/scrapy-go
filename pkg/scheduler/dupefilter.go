package scheduler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/dplcz/scrapy-go/internal/utils"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
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
	RequestSeen(request *shttp.Request) bool
}

// ============================================================================
// RFPDupeFilter 实现（Request Fingerprint DupeFilter）
// ============================================================================

// RFPDupeFilter 基于请求指纹的去重过滤器。
// 使用请求的 URL（规范化后）、Method 和 Body 计算 SHA1 指纹进行去重。
//
// 使用 sync.Map 实现高并发无锁去重（P4-007j 优化）：
//   - RequestSeen 使用 LoadOrStore 原子操作，无需全局锁
//   - 适合写少读多的去重场景（大部分请求是新请求）
//
// 当配置了 jobDir 时，支持指纹集合的持久化：
//   - Open 时从磁盘加载已有指纹（用于断点续爬）
//   - Close 时将指纹集合保存到磁盘
//
// 对应 Scrapy 的 RFPDupeFilter 类。
type RFPDupeFilter struct {
	fingerprints sync.Map       // map[string]struct{}，无锁并发安全
	seenCount    atomic.Int64   // 已记录的指纹数量（用于统计和持久化）
	mu           sync.Mutex     // 仅保护持久化操作（Open/Close）
	logger       *slog.Logger
	debug        bool           // 是否输出调试日志（对应 DUPEFILTER_DEBUG）
	logDupes     atomic.Bool    // 是否已输出过重复日志（仅输出一次提示）
	jobDir       string         // 持久化目录（空字符串表示不持久化）
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
	df := &RFPDupeFilter{
		logger: logger,
		debug:  debug,
	}
	df.logDupes.Store(true)
	return df
}

// NewPersistentRFPDupeFilter 创建一个支持持久化的请求指纹去重过滤器。
//
// 当 jobDir 非空时，指纹集合会在 Open 时从磁盘加载，
// 在 Close 时保存到磁盘，实现断点续爬时的去重状态恢复。
//
// 参数：
//   - logger: 日志记录器，为 nil 时使用默认 logger
//   - debug: 是否输出每个被过滤请求的调试日志
//   - jobDir: 持久化目录路径
func NewPersistentRFPDupeFilter(logger *slog.Logger, debug bool, jobDir string) *RFPDupeFilter {
	df := NewRFPDupeFilter(logger, debug)
	df.jobDir = jobDir
	return df
}

// Open 初始化过滤器。
// 如果配置了 jobDir，会从磁盘加载已有的指纹集合。
func (f *RFPDupeFilter) Open(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.jobDir != "" {
		if err := f.loadFingerprints(); err != nil {
			return fmt.Errorf("failed to load dupefilter state: %w", err)
		}
		count := f.seenCount.Load()
		if count > 0 {
			f.logger.Info("loaded dupefilter fingerprints from disk",
				"count", count,
				"jobdir", f.jobDir,
			)
		}
	}

	return nil
}

// Close 关闭过滤器。
// 如果配置了 jobDir，会将指纹集合保存到磁盘。
func (f *RFPDupeFilter) Close(reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.jobDir != "" {
		if err := f.saveFingerprints(); err != nil {
			f.logger.Error("failed to save dupefilter state", "error", err)
			return err
		}
	}

	// 清空指纹集合释放内存
	f.fingerprints = sync.Map{}
	f.seenCount.Store(0)
	return nil
}

// RequestSeen 检查请求是否已见过。
//
// 使用 RequestFingerprint 计算请求指纹，通过 sync.Map.LoadOrStore
// 原子操作实现无锁并发去重。
func (f *RFPDupeFilter) RequestSeen(request *shttp.Request) bool {
	fp := f.requestFingerprint(request)

	// LoadOrStore 是原子操作：如果 key 已存在返回 true，否则存入并返回 false
	_, loaded := f.fingerprints.LoadOrStore(fp, struct{}{})
	if loaded {
		f.logDupe(request)
		return true
	}

	f.seenCount.Add(1)
	return false
}

// SeenCount 返回已记录的指纹数量（用于统计和测试）。
func (f *RFPDupeFilter) SeenCount() int {
	return int(f.seenCount.Load())
}

// requestFingerprint 计算请求的指纹。
func (f *RFPDupeFilter) requestFingerprint(request *shttp.Request) string {
	return utils.RequestFingerprint(request, nil, false)
}

// logDupe 记录重复请求的日志。
// 在 debug 模式下记录每个重复请求；否则仅记录一次提示。
func (f *RFPDupeFilter) logDupe(request *shttp.Request) {
	if f.debug {
		f.logger.Debug("filtered duplicate request",
			"request", request.String(),
		)
	} else if f.logDupes.CompareAndSwap(true, false) {
		f.logger.Debug("filtered duplicate request (further duplicates will not be shown, set DUPEFILTER_DEBUG=true to see all)",
			"request", request.String(),
		)
	}
}

// ============================================================================
// RFPDupeFilter 持久化方法
// ============================================================================

// fingerprintFile 返回指纹持久化文件路径。
func (f *RFPDupeFilter) fingerprintFile() string {
	return filepath.Join(f.jobDir, "requests.seen")
}

// loadFingerprints 从磁盘加载指纹集合。
// 文件格式：每行一个指纹（SHA1 十六进制字符串）。
func (f *RFPDupeFilter) loadFingerprints() error {
	filename := f.fingerprintFile()
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在，正常启动
		}
		return err
	}
	defer file.Close()

	var count int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fp := scanner.Text()
		if fp != "" {
			f.fingerprints.Store(fp, struct{}{})
			count++
		}
	}

	f.seenCount.Store(count)
	return scanner.Err()
}

// saveFingerprints 将指纹集合保存到磁盘。
// 使用临时文件 + os.Rename 实现原子写入。
func (f *RFPDupeFilter) saveFingerprints() error {
	if err := os.MkdirAll(f.jobDir, 0o755); err != nil {
		return fmt.Errorf("failed to create jobdir: %w", err)
	}

	filename := f.fingerprintFile()
	tmpFile := filename + ".tmp"

	file, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create temp fingerprint file: %w", err)
	}

	writer := bufio.NewWriter(file)
	var writeErr error
	f.fingerprints.Range(func(key, value any) bool {
		if _, err := writer.WriteString(key.(string) + "\n"); err != nil {
			writeErr = err
			return false
		}
		return true
	})
	if writeErr != nil {
		file.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("failed to write fingerprint: %w", writeErr)
	}

	if err := writer.Flush(); err != nil {
		file.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("failed to flush fingerprint file: %w", err)
	}

	if err := file.Close(); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to close fingerprint file: %w", err)
	}

	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename fingerprint file: %w", err)
	}

	return nil
}

// ============================================================================
// PersistentDupeFilter 状态序列化（JSON 格式，用于调试和监控）
// ============================================================================

// DupeFilterStats 返回去重过滤器的统计信息。
func (f *RFPDupeFilter) DupeFilterStats() map[string]any {
	stats := map[string]any{
		"seen_count": int(f.seenCount.Load()),
		"persistent": f.jobDir != "",
	}
	if f.jobDir != "" {
		stats["jobdir"] = f.jobDir
	}
	return stats
}

// ExportState 导出去重过滤器状态为 JSON（用于调试）。
func (f *RFPDupeFilter) ExportState() ([]byte, error) {
	state := struct {
		SeenCount  int64  `json:"seen_count"`
		Persistent bool   `json:"persistent"`
		JobDir     string `json:"jobdir,omitempty"`
	}{
		SeenCount:  f.seenCount.Load(),
		Persistent: f.jobDir != "",
		JobDir:     f.jobDir,
	}

	return json.Marshal(state)
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

func (f *NoDupeFilter) Open(ctx context.Context) error          { return nil }
func (f *NoDupeFilter) Close(reason string) error               { return nil }
func (f *NoDupeFilter) RequestSeen(request *shttp.Request) bool { return false }
