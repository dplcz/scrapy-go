package feedexport

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// XMLExporter 将 Item 序列化为 XML 格式。
// 对应 Scrapy 的 XmlItemExporter。
//
// 输出格式示例（RootElement=items, ItemElement=item）：
//
//	<?xml version="1.0" encoding="utf-8"?>
//	<items>
//	  <item>
//	    <name>Foo</name>
//	    <price>10</price>
//	  </item>
//	</items>
//
// 字段值支持以下递归规则：
//   - 切片/数组：每个元素展开为 <value>…</value> 子节点
//   - map：每个键值对展开为 <key>value</key> 子节点
//   - 其他类型：使用 fmt.Sprint 转为文本
type XMLExporter struct {
	w        io.Writer
	opts     ExporterOptions
	encoder  *xml.Encoder
	started  bool
	closed   bool
	rootName string
	itemName string
}

// NewXMLExporter 创建一个 XML 格式的 Exporter。
func NewXMLExporter(w io.Writer, opts ExporterOptions) *XMLExporter {
	if opts.ItemElement == "" {
		opts.ItemElement = "item"
	}
	if opts.RootElement == "" {
		opts.RootElement = "items"
	}

	enc := xml.NewEncoder(w)
	if opts.Indent > 0 {
		enc.Indent("", strings.Repeat(" ", opts.Indent))
	}
	return &XMLExporter{
		w:        w,
		opts:     opts,
		encoder:  enc,
		rootName: opts.RootElement,
		itemName: opts.ItemElement,
	}
}

// StartExporting 写入 XML 声明和根元素开标签。
func (e *XMLExporter) StartExporting() error {
	if e.started {
		return errors.New("feedexport: XMLExporter already started")
	}
	e.started = true

	encoding := e.opts.Encoding
	if encoding == "" {
		encoding = "utf-8"
	}
	header := fmt.Sprintf(`<?xml version="1.0" encoding="%s"?>`, encoding)
	if _, err := e.w.Write([]byte(header)); err != nil {
		return fmt.Errorf("feedexport: write xml header: %w", err)
	}
	if e.opts.Indent > 0 {
		if _, err := e.w.Write([]byte{'\n'}); err != nil {
			return fmt.Errorf("feedexport: write newline: %w", err)
		}
	}

	if err := e.encoder.EncodeToken(xml.StartElement{Name: xml.Name{Local: e.rootName}}); err != nil {
		return fmt.Errorf("feedexport: write root start: %w", err)
	}
	return nil
}

// ExportItem 写入一个 Item 元素。
func (e *XMLExporter) ExportItem(item any) error {
	if !e.started {
		return errors.New("feedexport: XMLExporter not started")
	}
	if e.closed {
		return errors.New("feedexport: XMLExporter already finished")
	}

	allFields, getField := extractItem(item)
	fields := filterFields(allFields, e.opts.FieldsToExport)

	start := xml.StartElement{Name: xml.Name{Local: e.itemName}}
	if err := e.encoder.EncodeToken(start); err != nil {
		return fmt.Errorf("feedexport: write item start: %w", err)
	}

	for _, name := range fields {
		value, _ := getField(name)
		if err := e.encodeField(name, value); err != nil {
			return err
		}
	}

	if err := e.encoder.EncodeToken(start.End()); err != nil {
		return fmt.Errorf("feedexport: write item end: %w", err)
	}
	return nil
}

// FinishExporting 写入根元素闭标签并刷新缓冲。
func (e *XMLExporter) FinishExporting() error {
	if !e.started {
		return errors.New("feedexport: XMLExporter not started")
	}
	if e.closed {
		return nil
	}
	e.closed = true

	end := xml.EndElement{Name: xml.Name{Local: e.rootName}}
	if err := e.encoder.EncodeToken(end); err != nil {
		return fmt.Errorf("feedexport: write root end: %w", err)
	}
	return e.encoder.Flush()
}

// encodeField 递归编码一个字段。
func (e *XMLExporter) encodeField(name string, value any) error {
	start := xml.StartElement{Name: xml.Name{Local: name}}
	if err := e.encoder.EncodeToken(start); err != nil {
		return fmt.Errorf("feedexport: write field start: %w", err)
	}

	if err := e.encodeValue(value); err != nil {
		return err
	}

	if err := e.encoder.EncodeToken(start.End()); err != nil {
		return fmt.Errorf("feedexport: write field end: %w", err)
	}
	return nil
}

// encodeValue 根据值类型递归输出子节点或文本内容。
func (e *XMLExporter) encodeValue(value any) error {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		return e.encoder.EncodeToken(xml.CharData(v))
	case []byte:
		return e.encoder.EncodeToken(xml.CharData(v))
	case bool:
		return e.encoder.EncodeToken(xml.CharData(strconv.FormatBool(v)))
	case int:
		return e.encoder.EncodeToken(xml.CharData(strconv.Itoa(v)))
	case int64:
		return e.encoder.EncodeToken(xml.CharData(strconv.FormatInt(v, 10)))
	case float64:
		return e.encoder.EncodeToken(xml.CharData(strconv.FormatFloat(v, 'f', -1, 64)))
	case map[string]any:
		for k, sub := range v {
			if err := e.encodeField(k, sub); err != nil {
				return err
			}
		}
		return nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if err := e.encodeField("value", rv.Index(i).Interface()); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			for _, key := range rv.MapKeys() {
				k := key.String()
				if err := e.encodeField(k, rv.MapIndex(key).Interface()); err != nil {
					return err
				}
			}
			return nil
		}
	}

	return e.encoder.EncodeToken(xml.CharData(fmt.Sprintf("%v", value)))
}
