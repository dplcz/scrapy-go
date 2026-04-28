package feedexport

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// CSVExporter 将 Item 序列化为 CSV 格式。
// 对应 Scrapy 的 CsvItemExporter。
//
// 特性：
//   - 第一行可选为字段名标题行（通过 IncludeHeadersLine 控制）
//   - 切片字段自动用 JoinMultivalued 连接（默认 ","）
//   - 字段顺序来自首个 Item，除非 FieldsToExport 显式指定
//   - 复杂类型（map、嵌套 struct）通过 fmt.Sprintf("%v") 格式化
//
// 注意：CSV 格式的字段集合必须在所有 Item 间保持一致——首个 Item 的字段集合
// 将作为表头，后续 Item 中不存在的字段输出空字符串。
type CSVExporter struct {
	w          io.Writer
	opts       ExporterOptions
	writer     *csv.Writer
	started    bool
	closed     bool
	headerDone bool

	// fields 是最终决定的字段顺序，在首个 Item 或 StartExporting 时确定。
	fields []string
}

// NewCSVExporter 创建一个 CSV 格式的 Exporter。
func NewCSVExporter(w io.Writer, opts ExporterOptions) *CSVExporter {
	if opts.JoinMultivalued == "" {
		opts.JoinMultivalued = ","
	}
	return &CSVExporter{
		w:      w,
		opts:   opts,
		writer: csv.NewWriter(w),
	}
}

// StartExporting 标记开始导出。
// 若 FieldsToExport 已指定，会立即写入表头；否则延迟到首个 Item 写入时确定。
func (e *CSVExporter) StartExporting() error {
	if e.started {
		return errors.New("feedexport: CSVExporter already started")
	}
	e.started = true

	if len(e.opts.FieldsToExport) > 0 {
		e.fields = append([]string(nil), e.opts.FieldsToExport...)
		if err := e.writeHeaderIfNeeded(); err != nil {
			return err
		}
	}
	return nil
}

// ExportItem 输出一行 CSV 记录。
func (e *CSVExporter) ExportItem(item any) error {
	if !e.started {
		return errors.New("feedexport: CSVExporter not started")
	}
	if e.closed {
		return errors.New("feedexport: CSVExporter already finished")
	}

	allFields, getField := serializeItemFields(item, nil)

	// 若首次导出且未预设字段，用当前 Item 的字段集合作为表头
	if len(e.fields) == 0 {
		e.fields = append([]string(nil), allFields...)
	}
	if err := e.writeHeaderIfNeeded(); err != nil {
		return err
	}

	row := make([]string, len(e.fields))
	for i, name := range e.fields {
		v, ok := getField(name)
		if !ok {
			row[i] = ""
			continue
		}
		row[i] = e.formatValue(v)
	}

	if err := e.writer.Write(row); err != nil {
		return fmt.Errorf("feedexport: write csv row: %w", err)
	}
	return nil
}

// FinishExporting 刷新缓冲并标记导出结束。
func (e *CSVExporter) FinishExporting() error {
	if !e.started {
		return errors.New("feedexport: CSVExporter not started")
	}
	if e.closed {
		return nil
	}
	e.closed = true

	e.writer.Flush()
	if err := e.writer.Error(); err != nil {
		return fmt.Errorf("feedexport: flush csv: %w", err)
	}
	return nil
}

// writeHeaderIfNeeded 在首次需要时写入表头。
func (e *CSVExporter) writeHeaderIfNeeded() error {
	if e.headerDone || !e.opts.IncludeHeadersLine {
		e.headerDone = true
		return nil
	}
	if len(e.fields) == 0 {
		// 字段还未确定，延迟写入
		return nil
	}
	if err := e.writer.Write(e.fields); err != nil {
		return fmt.Errorf("feedexport: write csv header: %w", err)
	}
	e.headerDone = true
	return nil
}

// formatValue 将字段值格式化为 CSV 可输出的字符串。
func (e *CSVExporter) formatValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	case []string:
		return strings.Join(val, e.opts.JoinMultivalued)
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = e.formatValue(item)
		}
		return strings.Join(parts, e.opts.JoinMultivalued)
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	default:
		return fmt.Sprintf("%v", val)
	}
}
