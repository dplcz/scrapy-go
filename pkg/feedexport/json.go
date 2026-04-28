package feedexport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// JSONExporter 将 Item 序列化为 JSON 数组。
// 对应 Scrapy 的 JsonItemExporter。
//
// 输出格式示例（indent = 0）：
//
//	[{"a":1},{"b":2}]
//
// 输出格式示例（indent > 0）：
//
//	[
//	  {"a": 1},
//	  {"b": 2}
//	]
//
// 注意：由于 JSON 数组需要在 Item 与 Item 之间插入 ","，
// 本实现会累积状态（firstItem 标记），因此必须按 Start → Export → Finish 顺序使用。
type JSONExporter struct {
	w       io.Writer
	opts    ExporterOptions
	started bool
	closed  bool
	first   bool
}

// NewJSONExporter 创建一个 JSON 数组格式的 Exporter。
func NewJSONExporter(w io.Writer, opts ExporterOptions) *JSONExporter {
	if opts.JoinMultivalued == "" {
		opts.JoinMultivalued = ","
	}
	return &JSONExporter{
		w:     w,
		opts:  opts,
		first: true,
	}
}

// StartExporting 写入 "[" 开始 JSON 数组。
func (e *JSONExporter) StartExporting() error {
	if e.started {
		return errors.New("feedexport: JSONExporter already started")
	}
	e.started = true

	if _, err := e.w.Write([]byte{'['}); err != nil {
		return fmt.Errorf("feedexport: write array start: %w", err)
	}
	e.writeNewlineIfIndent()
	return nil
}

// ExportItem 写入一个 Item。
// 若 Item 与上一个 Item 之间需要分隔符，会自动写入 ","。
func (e *JSONExporter) ExportItem(item any) error {
	if !e.started {
		return errors.New("feedexport: JSONExporter not started")
	}
	if e.closed {
		return errors.New("feedexport: JSONExporter already finished")
	}

	if !e.first {
		// 非第一个 Item，写入分隔符
		if _, err := e.w.Write([]byte{','}); err != nil {
			return fmt.Errorf("feedexport: write separator: %w", err)
		}
		e.writeNewlineIfIndent()
	}
	e.first = false

	dict := buildItemDict(item, e.opts.FieldsToExport)

	data, err := e.marshal(dict)
	if err != nil {
		return fmt.Errorf("feedexport: marshal item: %w", err)
	}
	if _, err := e.w.Write(data); err != nil {
		return fmt.Errorf("feedexport: write item: %w", err)
	}
	return nil
}

// FinishExporting 写入 "]" 结束 JSON 数组。
func (e *JSONExporter) FinishExporting() error {
	if !e.started {
		return errors.New("feedexport: JSONExporter not started")
	}
	if e.closed {
		return nil
	}
	e.closed = true

	e.writeNewlineIfIndent()
	if _, err := e.w.Write([]byte{']'}); err != nil {
		return fmt.Errorf("feedexport: write array end: %w", err)
	}
	return nil
}

// marshal 根据 indent 配置选择紧凑或带缩进的序列化。
func (e *JSONExporter) marshal(v any) ([]byte, error) {
	if e.opts.Indent > 0 {
		// 缩进模式下，使用 json.MarshalIndent 但保持每个 Item 缩进一致
		indent := makeIndent(e.opts.Indent)
		// 外层数组已经换行，每个 Item 缩进 1 级
		data, err := json.MarshalIndent(v, indent, indent)
		if err != nil {
			return nil, err
		}
		// MarshalIndent 输出的首行没有缩进，需要手动补一级
		return append([]byte(indent), data...), nil
	}
	return json.Marshal(v)
}

// writeNewlineIfIndent 在 indent > 0 时写入换行符。
func (e *JSONExporter) writeNewlineIfIndent() {
	if e.opts.Indent > 0 {
		_, _ = e.w.Write([]byte{'\n'})
	}
}

// buildItemDict 根据 fieldsToExport 将 item 转换为 map 便于 json.Marshal。
// 保持字段顺序：通过写入一个 ordered map（使用 json.RawMessage 组装）。
//
// Go 的 encoding/json 在 marshal map 时会按 key 字典序排序（参见 encoding/json 文档），
// 为了保持 fieldsToExport 指定的顺序或 struct 的声明顺序，我们手动构造 JSON。
func buildItemDict(item any, fieldsToExport []string) json.RawMessage {
	fields, getField := serializeItemFields(item, fieldsToExport)

	if len(fields) == 0 {
		return json.RawMessage("{}")
	}

	buf := make([]byte, 0, 64)
	buf = append(buf, '{')
	for i, name := range fields {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyBytes, _ := json.Marshal(name)
		buf = append(buf, keyBytes...)
		buf = append(buf, ':')

		value, ok := getField(name)
		if !ok {
			buf = append(buf, 'n', 'u', 'l', 'l')
			continue
		}
		valBytes, err := json.Marshal(value)
		if err != nil {
			// 序列化失败时回退到字符串表示，避免破坏整体输出
			fallback, _ := json.Marshal(fmt.Sprintf("%v", value))
			valBytes = fallback
		}
		buf = append(buf, valBytes...)
	}
	buf = append(buf, '}')
	return json.RawMessage(buf)
}

// makeIndent 生成指定长度的空格缩进。
func makeIndent(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	return string(out)
}
