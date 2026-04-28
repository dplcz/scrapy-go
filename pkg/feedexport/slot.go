package feedexport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// ExporterFactory — 根据格式构造 Exporter
// ============================================================================

// ExporterFactory 根据指定的 Writer 和 Options 构造一个 ItemExporter。
// 框架内置了所有核心格式的工厂函数，用户也可以通过 RegisterExporter 注册自定义格式。
type ExporterFactory func(w io.Writer, opts ExporterOptions) ItemExporter

// defaultExporterFactories 内置格式 → Factory 的注册表。
var defaultExporterFactories = map[Format]ExporterFactory{
	FormatJSON: func(w io.Writer, opts ExporterOptions) ItemExporter {
		return NewJSONExporter(w, opts)
	},
	FormatJSONLines: func(w io.Writer, opts ExporterOptions) ItemExporter {
		return NewJSONLinesExporter(w, opts)
	},
	FormatCSV: func(w io.Writer, opts ExporterOptions) ItemExporter {
		return NewCSVExporter(w, opts)
	},
	FormatXML: func(w io.Writer, opts ExporterOptions) ItemExporter {
		return NewXMLExporter(w, opts)
	},
}

// exporterRegistry 是运行时可扩展的 Exporter 注册表（含内置和用户注册）。
var (
	exporterRegistry   = make(map[Format]ExporterFactory)
	exporterRegistryMu sync.RWMutex
)

func init() {
	for k, v := range defaultExporterFactories {
		exporterRegistry[k] = v
	}
}

// RegisterExporter 注册一个自定义 Exporter 工厂函数。
// 若 format 已存在，会被覆盖。线程安全。
func RegisterExporter(format Format, factory ExporterFactory) {
	exporterRegistryMu.Lock()
	defer exporterRegistryMu.Unlock()
	exporterRegistry[format] = factory
}

// LookupExporter 按格式查找 Exporter 工厂。
// 返回 nil 和 false 表示未注册。
func LookupExporter(format Format) (ExporterFactory, bool) {
	exporterRegistryMu.RLock()
	defer exporterRegistryMu.RUnlock()
	f, ok := exporterRegistry[format]
	return f, ok
}

// NewExporter 根据格式构造一个 Exporter。
// format 会被 NormalizeFormat 归一化后查找。
func NewExporter(format Format, w io.Writer, opts ExporterOptions) (ItemExporter, error) {
	f := NormalizeFormat(string(format))
	factory, ok := LookupExporter(f)
	if !ok {
		return nil, fmt.Errorf("feedexport: unknown format %q", format)
	}
	return factory(w, opts), nil
}

// ============================================================================
// FeedConfig — 单个 Feed 的配置
// ============================================================================

// FeedConfig 描述一个 Feed 导出目标的全部配置。
// 对应 Scrapy 中 FEEDS 字典的一个条目（uri → options）。
type FeedConfig struct {
	// URI 导出目标 URI（如 "output.json"、"file:///tmp/x.csv"、"stdout:"）。
	// 支持 URI 模板占位符，详见 URIParams.Render。
	URI string

	// Format 导出格式。
	Format Format

	// Overwrite 为 true 时覆盖已有文件（仅对 FileStorage 有效）。
	Overwrite bool

	// StoreEmpty 为 true 时即使没有 Item 也会创建输出文件。
	StoreEmpty bool

	// Options 传递给 Exporter 的配置。
	Options ExporterOptions

	// Filter 决定 Item 是否应导出到此 Feed。为空时接受所有 Item。
	Filter ItemFilterFunc

	// Storage 可选地显式指定一个已构造的 FeedStorage。
	// 若为 nil，FeedExport 会通过 NewStorageForURI(URI) 自动构造。
	Storage FeedStorage
}

// ============================================================================
// FeedSlot — 正在进行中的导出任务
// ============================================================================

// FeedSlot 组合一个 Storage 与一个 Exporter，代表一个正在进行中的 Feed 导出任务。
// 对应 Scrapy 的 FeedSlot。
//
// 生命周期：
//  1. NewFeedSlot — 构造
//  2. Start       — 打开 Storage，启动 Exporter
//  3. ExportItem  — 反复调用
//  4. Close       — 结束 Exporter，提交 Storage
//
// 线程安全性：FeedExport 扩展通过监听同步信号 item_scraped 串行化调用，
// 因此 FeedSlot 本身无需加锁。为防御性考虑，核心方法上仍加 mutex。
type FeedSlot struct {
	mu sync.Mutex

	cfg      FeedConfig
	storage  FeedStorage
	writer   io.WriteCloser
	exporter ItemExporter
	logger   *slog.Logger

	spiderName string
	itemCount  int

	started  bool
	finished bool
}

// NewFeedSlot 构造一个 FeedSlot。
// 调用方必须在 Start 之前完成所有配置填充。
func NewFeedSlot(cfg FeedConfig, logger *slog.Logger) (*FeedSlot, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.URI == "" {
		return nil, errors.New("feedexport: FeedConfig.URI is required")
	}
	if cfg.Format == "" {
		return nil, errors.New("feedexport: FeedConfig.Format is required")
	}
	cfg.Format = NormalizeFormat(string(cfg.Format))

	if _, ok := LookupExporter(cfg.Format); !ok {
		return nil, fmt.Errorf("feedexport: unknown format %q", cfg.Format)
	}

	if cfg.Filter == nil {
		cfg.Filter = AcceptAll
	}

	storage := cfg.Storage
	if storage == nil {
		s, err := NewStorageForURI(cfg.URI, cfg.Overwrite)
		if err != nil {
			return nil, err
		}
		storage = s
	}

	return &FeedSlot{
		cfg:     cfg,
		storage: storage,
		logger:  logger,
	}, nil
}

// Start 打开 Storage 并初始化 Exporter。
// 幂等：多次调用只会生效一次。
func (s *FeedSlot) Start(ctx context.Context, sp spider.Spider) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked(ctx, sp)
}

// startLocked 是 Start 的内部实现，调用方需持有 mu。
func (s *FeedSlot) startLocked(ctx context.Context, sp spider.Spider) error {
	if s.started {
		return nil
	}

	if sp != nil {
		s.spiderName = sp.Name()
	}

	w, err := s.storage.Open(ctx, sp)
	if err != nil {
		return fmt.Errorf("feedexport: open storage for %s: %w", s.cfg.URI, err)
	}
	s.writer = w

	exporter, err := NewExporter(s.cfg.Format, w, s.cfg.Options)
	if err != nil {
		_ = w.Close()
		return err
	}
	s.exporter = exporter

	if err := s.exporter.StartExporting(); err != nil {
		_ = w.Close()
		return fmt.Errorf("feedexport: start exporter: %w", err)
	}

	s.started = true
	s.logger.Info("feed export started",
		"uri", s.cfg.URI,
		"format", s.cfg.Format,
	)
	return nil
}

// ExportItem 写入一个 Item。
// 如果 Filter 拒绝此 Item，直接返回 nil。
// 如果 FeedSlot 尚未启动，会自动启动（延迟启动，适配 StoreEmpty=false 场景）。
func (s *FeedSlot) ExportItem(ctx context.Context, sp spider.Spider, item any) error {
	if !s.cfg.Filter(item) {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.finished {
		return errors.New("feedexport: slot already closed")
	}
	if !s.started {
		if err := s.startLocked(ctx, sp); err != nil {
			return err
		}
	}

	if err := s.exporter.ExportItem(item); err != nil {
		return fmt.Errorf("feedexport: export item: %w", err)
	}
	s.itemCount++
	return nil
}

// Close 结束 Exporter 并提交 Storage。
// 如果未曾写入任何 Item 且 StoreEmpty=false，则跳过（不创建空文件）。
// 即使处理过程中出错，也会尽力关闭所有资源。
func (s *FeedSlot) Close(ctx context.Context, sp spider.Spider) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.finished {
		return nil
	}
	s.finished = true

	// 未启动且无 Item：根据 StoreEmpty 决定是否创建空文件
	if !s.started {
		if !s.cfg.StoreEmpty {
			s.logger.Info("feed export skipped (no items and StoreEmpty=false)",
				"uri", s.cfg.URI,
			)
			return nil
		}
		if err := s.startLocked(ctx, sp); err != nil {
			return err
		}
	}

	var firstErr error

	if err := s.exporter.FinishExporting(); err != nil {
		firstErr = fmt.Errorf("feedexport: finish exporter: %w", err)
	}

	if err := s.storage.Store(ctx, s.writer); err != nil {
		if firstErr == nil {
			firstErr = fmt.Errorf("feedexport: store: %w", err)
		}
	}

	if firstErr == nil {
		s.logger.Info("feed export finished",
			"uri", s.cfg.URI,
			"format", s.cfg.Format,
			"items", s.itemCount,
		)
	} else {
		s.logger.Error("feed export closed with error",
			"uri", s.cfg.URI,
			"items", s.itemCount,
			"error", firstErr,
		)
	}
	return firstErr
}

// ItemCount 返回已导出的 Item 数量。
func (s *FeedSlot) ItemCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.itemCount
}

// URI 返回已渲染的目标 URI。
func (s *FeedSlot) URI() string { return s.cfg.URI }
