package feedexport

import (
	"context"
	"io"

	"github.com/dplcz/scrapy-go/pkg/spider"
)

// Format 表示导出格式。
type Format string

const (
	// FormatJSON 表示 JSON 数组格式（所有 Item 被封装在一个 JSON 数组中）。
	FormatJSON Format = "json"

	// FormatJSONLines 表示 JSON Lines 格式（每行一个 JSON 对象）。
	// 也常被简写为 "jsonl" 或 "jl"。
	FormatJSONLines Format = "jsonlines"

	// FormatCSV 表示 CSV 格式。
	FormatCSV Format = "csv"

	// FormatXML 表示 XML 格式。
	FormatXML Format = "xml"
)

// String 返回格式的字符串表示。
func (f Format) String() string {
	return string(f)
}

// NormalizeFormat 归一化格式名，兼容常见别名。
// 例如 "jl" / "jsonl" / "jsonlines" 都归一化为 FormatJSONLines。
func NormalizeFormat(s string) Format {
	switch s {
	case "jl", "jsonl", "jsonlines":
		return FormatJSONLines
	case "json":
		return FormatJSON
	case "csv":
		return FormatCSV
	case "xml":
		return FormatXML
	default:
		return Format(s)
	}
}

// ============================================================================
// ItemExporter 接口
// ============================================================================

// ItemExporter 定义 Item 序列化器接口。
// 对应 Scrapy 的 BaseItemExporter。
//
// 生命周期：
//  1. StartExporting — 开始导出（如写入 JSON 数组的 "["）
//  2. ExportItem     — 反复调用，每次序列化一个 Item
//  3. FinishExporting— 结束导出（如写入 JSON 数组的 "]"）
//
// 实现应当是非并发安全的：同一个 Exporter 由单个 FeedSlot 独占使用，
// Feed Export 扩展通过信号串行化调用。
type ItemExporter interface {
	// StartExporting 开始导出过程，必须在调用 ExportItem 之前调用。
	// 实现可以在此写入格式特定的前缀（如 JSON 的 "["、XML 的根元素开标签）。
	StartExporting() error

	// ExportItem 序列化一个 Item 并写入底层 writer。
	ExportItem(item any) error

	// FinishExporting 结束导出过程。
	// 实现可以在此写入格式特定的后缀（如 JSON 的 "]"、XML 的根元素闭标签）并刷新缓冲。
	FinishExporting() error
}

// ExporterOptions 是 Exporter 的通用配置。
// 不同 Exporter 可能忽略部分字段。
type ExporterOptions struct {
	// Encoding 指定文本编码（如 "utf-8"）。
	// 空字符串表示使用默认编码（通常为 utf-8）。
	Encoding string

	// Indent 指定缩进空格数。
	//   - 0 或负值：紧凑输出（不缩进）
	//   - > 0     ：按此空格数进行缩进
	// 仅 JSON 与 XML 使用；CSV / JSON Lines 忽略此字段。
	Indent int

	// FieldsToExport 指定要导出的字段白名单。
	// 为空时：导出 Item 的全部字段。
	// 导出顺序与 FieldsToExport 保持一致。
	FieldsToExport []string

	// IncludeHeadersLine 仅对 CSV 生效，是否在第一行输出字段名。
	// 默认 true。
	IncludeHeadersLine bool

	// JoinMultivalued 仅对 CSV 生效，用于将切片字段连接为单个字符串的分隔符。
	// 默认 ","。
	JoinMultivalued string

	// ItemElement 仅对 XML 生效，每个 Item 对应的元素名。默认 "item"。
	ItemElement string

	// RootElement 仅对 XML 生效，XML 根元素名。默认 "items"。
	RootElement string
}

// DefaultExporterOptions 返回默认配置。
func DefaultExporterOptions() ExporterOptions {
	return ExporterOptions{
		Encoding:           "utf-8",
		Indent:             0,
		IncludeHeadersLine: true,
		JoinMultivalued:    ",",
		ItemElement:        "item",
		RootElement:        "items",
	}
}

// ============================================================================
// FeedStorage 接口
// ============================================================================

// FeedStorage 定义导出存储后端接口。
// 对应 Scrapy 的 FeedStorageProtocol / IFeedStorage。
//
// 生命周期：
//  1. Open  — Spider 打开时调用，返回一个 io.WriteCloser 供 Exporter 写入。
//  2. Store — Spider 关闭时调用，将 Open 返回的 writer 的内容持久化到最终位置。
//
// 典型实现场景：
//   - FileStorage  : Open 直接打开目标文件，Store 关闭文件句柄即完成。
//   - StdoutStorage: Open 返回 os.Stdout 的包装，Store 空实现。
//
// 线程安全性：单个 FeedStorage 实例由单个 FeedSlot 独占使用，无需并发保护。
type FeedStorage interface {
	// Open 打开存储，返回一个可写入的 io.WriteCloser。
	// sp 为当前 Spider，实现可根据 Spider 名称等信息定制路径。
	// 返回的 WriteCloser 必须由调用方通过 Store 传回以便正确关闭。
	Open(ctx context.Context, sp spider.Spider) (io.WriteCloser, error)

	// Store 将 Open 返回的 writer 的内容持久化。
	// 实现应负责关闭 writer（若尚未关闭）。
	// 对于直接写文件的实现，Store 可能只是关闭句柄；
	// 对于需要两阶段提交的实现（如临时文件 + rename），Store 会执行 rename。
	Store(ctx context.Context, w io.WriteCloser) error
}

// ============================================================================
// ItemFilterFunc
// ============================================================================

// ItemFilterFunc 决定一个 Item 是否应该被导出到某个 Feed。
// 返回 true 表示接受，false 表示过滤。
//
// 对应 Scrapy 的 ItemFilter.accepts；此处采用函数类型而非接口，
// 以契合 Go 的函数式风格并避免不必要的类型层次。
type ItemFilterFunc func(item any) bool

// AcceptAll 是默认过滤器，接受所有 Item。
func AcceptAll(_ any) bool { return true }
