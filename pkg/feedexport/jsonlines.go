package feedexport

import (
	"errors"
	"fmt"
	"io"
)

// JSONLinesExporter 将 Item 序列化为 JSON Lines 格式（每行一个 JSON 对象）。
// 对应 Scrapy 的 JsonLinesItemExporter。
//
// 输出格式示例：
//
//	{"a":1}
//	{"b":2}
//
// 相比 JSONExporter，JSON Lines 格式天然流式友好：
//   - 每个 Item 独立，可以增量读取
//   - 不需要数组开闭标记，Item 之间无依赖
//   - 大数据集场景下更易于处理
type JSONLinesExporter struct {
	w       io.Writer
	opts    ExporterOptions
	started bool
	closed  bool
}

// NewJSONLinesExporter 创建一个 JSON Lines 格式的 Exporter。
func NewJSONLinesExporter(w io.Writer, opts ExporterOptions) *JSONLinesExporter {
	return &JSONLinesExporter{
		w:    w,
		opts: opts,
	}
}

// StartExporting 标记开始导出。JSON Lines 无需写入前缀。
func (e *JSONLinesExporter) StartExporting() error {
	if e.started {
		return errors.New("feedexport: JSONLinesExporter already started")
	}
	e.started = true
	return nil
}

// ExportItem 序列化一个 Item 并追加 "\n"。
func (e *JSONLinesExporter) ExportItem(item any) error {
	if !e.started {
		return errors.New("feedexport: JSONLinesExporter not started")
	}
	if e.closed {
		return errors.New("feedexport: JSONLinesExporter already finished")
	}

	data := buildItemDict(item, e.opts.FieldsToExport)
	if _, err := e.w.Write(data); err != nil {
		return fmt.Errorf("feedexport: write item: %w", err)
	}
	if _, err := e.w.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("feedexport: write newline: %w", err)
	}
	return nil
}

// FinishExporting 标记导出结束。JSON Lines 无需写入后缀。
func (e *JSONLinesExporter) FinishExporting() error {
	if !e.started {
		return errors.New("feedexport: JSONLinesExporter not started")
	}
	e.closed = true
	return nil
}
