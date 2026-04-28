// 示例：演示 scrapy-go 的 Feed Export 数据导出系统完整 API。
//
// 本示例覆盖 pkg/feedexport 包的所有核心公开函数和类型：
//
// 格式与 Exporter：
//   - FormatJSON / FormatJSONLines / FormatCSV / FormatXML（四种内置格式）
//   - NormalizeFormat（格式别名归一化）
//   - NewJSONExporter / NewJSONLinesExporter / NewCSVExporter / NewXMLExporter
//   - ItemExporter 接口（StartExporting / ExportItem / FinishExporting）
//   - ExporterOptions（Indent / FieldsToExport / IncludeHeadersLine / JoinMultivalued / ItemElement / RootElement）
//   - DefaultExporterOptions
//   - RegisterExporter / LookupExporter / NewExporter（自定义格式注册）
//
// 存储后端：
//   - FileStorage（NewFileStorage / Open / Store / Path）
//   - StdoutStorage（NewStdoutStorage / Open / Store）
//   - NewStorageForURI（自动选择后端）
//
// URI 模板：
//   - URIParams / NewURIParams / Render（占位符渲染）
//
// 序列化器：
//   - RegisterSerializer / LookupSerializer / SerializeField
//
// FeedSlot：
//   - NewFeedSlot / Start / ExportItem / Close / ItemCount / URI
//
// FeedConfig：
//   - URI / Format / Overwrite / StoreEmpty / Options / Filter / Storage
//
// Crawler 集成：
//   - Crawler.AddFeed（代码注入 API）
//
// ItemAdapter 集成：
//   - struct Item + FieldMeta 驱动序列化
//
// 运行方式：go run examples/feedexport/main.go
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dplcz/scrapy-go/pkg/crawler"
	"github.com/dplcz/scrapy-go/pkg/feedexport"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/item"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// 数据模型
// ============================================================================

// Product 演示 struct Item + FieldMeta 驱动的序列化。
// - `item` tag 第一个 token 为字段名
// - serializer=xxx 指定序列化器名称，Feed Export 会自动调用
type Product struct {
	Name     string   `item:"name"`
	Price    float64  `item:"price,serializer=format_cny"`
	Category string   `item:"category"`
	InStock  bool     `item:"in_stock"`
	Tags     []string `item:"tags"`
}

// ============================================================================
// 本地测试网站
// ============================================================================

func newProductSite() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<div class="product"><h2 class="name">Go in Action</h2><span class="price">39.99</span><span class="category">编程</span><span class="stock">true</span><span class="tags">go,programming</span></div>
<div class="product"><h2 class="name">The Go Programming Language</h2><span class="price">49.99</span><span class="category">编程</span><span class="stock">true</span><span class="tags">go,reference</span></div>
<div class="product"><h2 class="name">Learning Go</h2><span class="price">42.00</span><span class="category">编程</span><span class="stock">false</span><span class="tags">go,beginner</span></div>
<div class="product"><h2 class="name">100 Go Mistakes</h2><span class="price">35.50</span><span class="category">编程</span><span class="stock">true</span><span class="tags">go,best-practices</span></div>
<div class="product"><h2 class="name">Go 语言圣经</h2><span class="price">68.00</span><span class="category">编程</span><span class="stock">true</span><span class="tags">go,中文</span></div>
</body></html>`)
	})
	return httptest.NewServer(mux)
}

// ============================================================================
// 爬虫实现（返回 struct Item）
// ============================================================================

type productSpider struct {
	spider.Base
}

func newProductSpider(base string) *productSpider {
	return &productSpider{
		Base: spider.Base{
			SpiderName: "products",
			StartURLs:  []string{base + "/"},
		},
	}
}

func (s *productSpider) Parse(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
	var out []spider.Output
	for _, p := range response.CSS("div.product") {
		inStock := p.CSS("span.stock::text").Get("") == "true"
		tags := strings.Split(p.CSS("span.tags::text").Get(""), ",")
		priceStr := p.CSS("span.price::text").Get("0")
		var price float64
		fmt.Sscanf(priceStr, "%f", &price)

		product := &Product{
			Name:     p.CSS("h2.name::text").Get(""),
			Price:    price,
			Category: p.CSS("span.category::text").Get(""),
			InStock:  inStock,
			Tags:     tags,
		}
		out = append(out, spider.Output{Item: product})
	}
	return out, nil
}

// ============================================================================
// main
// ============================================================================

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          scrapy-go Feed Export 完整 API 示例               ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// Part A: 底层 API 直接使用（不依赖 Crawler）
	demoNormalizeFormat()
	demoExporterOptions()
	demoJSONExporterDirect()
	demoJSONLinesExporterDirect()
	demoCSVExporterDirect()
	demoXMLExporterDirect()
	demoRegisterCustomExporter()
	demoRegisterSerializer()
	demoSerializeFieldWithMeta()
	demoURIParams()
	demoStorageForURI()
	demoFeedSlotDirect()

	// Part B: 通过 Crawler 集成使用（端到端）
	demoCrawlerIntegration()

	fmt.Println("\n✅ 所有 Feed Export API 演示完成！")
}

// ============================================================================
// 演示 1：NormalizeFormat — 格式别名归一化
// ============================================================================

func demoNormalizeFormat() {
	printSection("1. NormalizeFormat — 格式别名归一化")

	fmt.Printf("  NormalizeFormat(\"json\")      = %q\n", feedexport.NormalizeFormat("json"))
	fmt.Printf("  NormalizeFormat(\"jl\")        = %q\n", feedexport.NormalizeFormat("jl"))
	fmt.Printf("  NormalizeFormat(\"jsonl\")     = %q\n", feedexport.NormalizeFormat("jsonl"))
	fmt.Printf("  NormalizeFormat(\"jsonlines\") = %q\n", feedexport.NormalizeFormat("jsonlines"))
	fmt.Printf("  NormalizeFormat(\"csv\")       = %q\n", feedexport.NormalizeFormat("csv"))
	fmt.Printf("  NormalizeFormat(\"xml\")       = %q\n", feedexport.NormalizeFormat("xml"))
	fmt.Printf("  NormalizeFormat(\"custom\")    = %q (未知格式原样返回)\n", feedexport.NormalizeFormat("custom"))

	// Format.String()
	fmt.Printf("  FormatJSON.String()      = %q\n", feedexport.FormatJSON.String())
	fmt.Printf("  FormatJSONLines.String() = %q\n", feedexport.FormatJSONLines.String())
	fmt.Printf("  FormatCSV.String()       = %q\n", feedexport.FormatCSV.String())
	fmt.Printf("  FormatXML.String()       = %q\n", feedexport.FormatXML.String())
}

// ============================================================================
// 演示 2：ExporterOptions — 配置选项
// ============================================================================

func demoExporterOptions() {
	printSection("2. ExporterOptions — 默认配置与自定义")

	// DefaultExporterOptions 返回默认值
	defaults := feedexport.DefaultExporterOptions()
	fmt.Printf("  默认 Encoding:           %q\n", defaults.Encoding)
	fmt.Printf("  默认 Indent:             %d\n", defaults.Indent)
	fmt.Printf("  默认 IncludeHeadersLine: %v\n", defaults.IncludeHeadersLine)
	fmt.Printf("  默认 JoinMultivalued:    %q\n", defaults.JoinMultivalued)
	fmt.Printf("  默认 ItemElement:        %q\n", defaults.ItemElement)
	fmt.Printf("  默认 RootElement:        %q\n", defaults.RootElement)
	fmt.Printf("  默认 FieldsToExport:     %v (空=导出全部)\n", defaults.FieldsToExport)
}

// ============================================================================
// 演示 3：JSONExporter — 直接使用
// ============================================================================

func demoJSONExporterDirect() {
	printSection("3. JSONExporter — 直接使用（紧凑 + 缩进）")

	items := []map[string]any{
		{"name": "Go in Action", "price": 39.99},
		{"name": "Learning Go", "price": 42.00},
	}

	// 3a. 紧凑模式
	var buf bytes.Buffer
	opts := feedexport.DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "price"}
	exporter := feedexport.NewJSONExporter(&buf, opts)

	_ = exporter.StartExporting()
	for _, it := range items {
		_ = exporter.ExportItem(it)
	}
	_ = exporter.FinishExporting()
	fmt.Printf("  紧凑模式:\n  %s\n", buf.String())

	// 3b. 缩进模式（Indent=2）
	buf.Reset()
	opts.Indent = 2
	exporter = feedexport.NewJSONExporter(&buf, opts)

	_ = exporter.StartExporting()
	for _, it := range items {
		_ = exporter.ExportItem(it)
	}
	_ = exporter.FinishExporting()
	fmt.Printf("  缩进模式 (Indent=2):\n")
	for _, line := range strings.Split(buf.String(), "\n") {
		fmt.Printf("  %s\n", line)
	}
}

// ============================================================================
// 演示 4：JSONLinesExporter — 直接使用
// ============================================================================

func demoJSONLinesExporterDirect() {
	printSection("4. JSONLinesExporter — 直接使用")

	var buf bytes.Buffer
	opts := feedexport.DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "price"}
	exporter := feedexport.NewJSONLinesExporter(&buf, opts)

	_ = exporter.StartExporting()
	_ = exporter.ExportItem(map[string]any{"name": "Book A", "price": 10})
	_ = exporter.ExportItem(map[string]any{"name": "Book B", "price": 20})
	_ = exporter.ExportItem(map[string]any{"name": "Book C", "price": 30})
	_ = exporter.FinishExporting()

	fmt.Printf("  输出（每行一个 JSON 对象）:\n")
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		fmt.Printf("  %s\n", line)
	}
}

// ============================================================================
// 演示 5：CSVExporter — 直接使用（含选项演示）
// ============================================================================

func demoCSVExporterDirect() {
	printSection("5. CSVExporter — 直接使用")

	items := []map[string]any{
		{"name": "Go in Action", "price": 39.99, "tags": []string{"go", "programming"}},
		{"name": "Learning Go", "price": 42.00, "tags": []string{"go", "beginner"}},
	}

	// 5a. 默认选项（有表头，逗号连接多值）
	var buf bytes.Buffer
	opts := feedexport.DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "price", "tags"}
	exporter := feedexport.NewCSVExporter(&buf, opts)

	_ = exporter.StartExporting()
	for _, it := range items {
		_ = exporter.ExportItem(it)
	}
	_ = exporter.FinishExporting()
	fmt.Printf("  默认选项（有表头，逗号连接多值）:\n")
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		fmt.Printf("  %s\n", line)
	}

	// 5b. 无表头 + 分号连接多值
	buf.Reset()
	opts.IncludeHeadersLine = false
	opts.JoinMultivalued = ";"
	exporter = feedexport.NewCSVExporter(&buf, opts)

	_ = exporter.StartExporting()
	for _, it := range items {
		_ = exporter.ExportItem(it)
	}
	_ = exporter.FinishExporting()
	fmt.Printf("\n  无表头 + 分号连接多值:\n")
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		fmt.Printf("  %s\n", line)
	}
}

// ============================================================================
// 演示 6：XMLExporter — 直接使用（含自定义元素名）
// ============================================================================

func demoXMLExporterDirect() {
	printSection("6. XMLExporter — 直接使用")

	items := []map[string]any{
		{"name": "Go in Action", "price": 39.99},
		{"name": "Learning Go", "price": 42.00},
	}

	// 6a. 默认元素名（items/item）+ 缩进
	var buf bytes.Buffer
	opts := feedexport.DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "price"}
	opts.Indent = 2
	exporter := feedexport.NewXMLExporter(&buf, opts)

	_ = exporter.StartExporting()
	for _, it := range items {
		_ = exporter.ExportItem(it)
	}
	_ = exporter.FinishExporting()
	fmt.Printf("  默认元素名 + 缩进:\n")
	for _, line := range strings.Split(buf.String(), "\n") {
		fmt.Printf("  %s\n", line)
	}

	// 6b. 自定义元素名（products/product）
	buf.Reset()
	opts.RootElement = "products"
	opts.ItemElement = "product"
	exporter = feedexport.NewXMLExporter(&buf, opts)

	_ = exporter.StartExporting()
	for _, it := range items {
		_ = exporter.ExportItem(it)
	}
	_ = exporter.FinishExporting()
	fmt.Printf("\n  自定义元素名 (products/product):\n")
	for _, line := range strings.Split(buf.String(), "\n") {
		fmt.Printf("  %s\n", line)
	}
}

// ============================================================================
// 演示 7：RegisterExporter — 自定义格式注册
// ============================================================================

// markdownExporter 是一个自定义的 Markdown 表格格式 Exporter。
type markdownExporter struct {
	w      io.Writer
	opts   feedexport.ExporterOptions
	first  bool
	fields []string
}

func (e *markdownExporter) StartExporting() error {
	e.first = true
	return nil
}

func (e *markdownExporter) ExportItem(it any) error {
	adapter := item.Adapt(it)
	if adapter == nil {
		return nil
	}

	fields := e.opts.FieldsToExport
	if len(fields) == 0 {
		fields = adapter.FieldNames()
	}

	// 首个 Item 时输出表头
	if e.first {
		e.fields = fields
		header := "| " + strings.Join(fields, " | ") + " |"
		sep := "|"
		for range fields {
			sep += " --- |"
		}
		fmt.Fprintln(e.w, header)
		fmt.Fprintln(e.w, sep)
		e.first = false
	}

	// 输出数据行
	row := "| "
	for i, name := range e.fields {
		if i > 0 {
			row += " | "
		}
		v, ok := adapter.GetField(name)
		if ok {
			row += fmt.Sprintf("%v", v)
		}
	}
	row += " |"
	fmt.Fprintln(e.w, row)
	return nil
}

func (e *markdownExporter) FinishExporting() error { return nil }

func demoRegisterCustomExporter() {
	printSection("7. RegisterExporter — 自定义 Markdown 表格格式")

	// 注册自定义格式
	mdFormat := feedexport.Format("markdown")
	feedexport.RegisterExporter(mdFormat, func(w io.Writer, opts feedexport.ExporterOptions) feedexport.ItemExporter {
		return &markdownExporter{w: w, opts: opts}
	})

	// LookupExporter 验证注册成功
	factory, ok := feedexport.LookupExporter(mdFormat)
	fmt.Printf("  LookupExporter(\"markdown\"): found=%v, factory=%v\n", ok, factory != nil)

	// NewExporter 通过格式名构造
	var buf bytes.Buffer
	opts := feedexport.DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "price"}
	exporter, err := feedexport.NewExporter(mdFormat, &buf, opts)
	fmt.Printf("  NewExporter(\"markdown\"): err=%v\n", err)

	_ = exporter.StartExporting()
	_ = exporter.ExportItem(map[string]any{"name": "Go in Action", "price": 39.99})
	_ = exporter.ExportItem(map[string]any{"name": "Learning Go", "price": 42.00})
	_ = exporter.FinishExporting()

	fmt.Printf("  Markdown 表格输出:\n")
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		fmt.Printf("  %s\n", line)
	}

	// NewExporter 对未知格式返回错误
	_, err = feedexport.NewExporter("unknown_format", nil, opts)
	fmt.Printf("\n  NewExporter(\"unknown_format\"): err=%v\n", err)
}

// ============================================================================
// 演示 8：RegisterSerializer — 序列化器注册
// ============================================================================

func demoRegisterSerializer() {
	printSection("8. RegisterSerializer — 字段序列化器")

	// 注册 format_cny 序列化器：将 float64 格式化为人民币字符串
	feedexport.RegisterSerializer("format_cny", func(v any) any {
		if f, ok := v.(float64); ok {
			return fmt.Sprintf("¥%.2f", f)
		}
		return v
	})

	// 注册 uppercase 序列化器：将字符串转为大写
	feedexport.RegisterSerializer("uppercase", func(v any) any {
		if s, ok := v.(string); ok {
			return strings.ToUpper(s)
		}
		return v
	})

	// LookupSerializer 验证
	fn, ok := feedexport.LookupSerializer("format_cny")
	fmt.Printf("  LookupSerializer(\"format_cny\"): found=%v\n", ok)
	fmt.Printf("  format_cny(39.99) = %v\n", fn(39.99))

	fn2, ok := feedexport.LookupSerializer("uppercase")
	fmt.Printf("  LookupSerializer(\"uppercase\"): found=%v\n", ok)
	fmt.Printf("  uppercase(\"hello\") = %v\n", fn2("hello"))

	// 未注册的序列化器
	_, ok = feedexport.LookupSerializer("nonexistent")
	fmt.Printf("  LookupSerializer(\"nonexistent\"): found=%v\n", ok)
}

// ============================================================================
// 演示 9：SerializeField — FieldMeta 驱动序列化
// ============================================================================

func demoSerializeFieldWithMeta() {
	printSection("9. SerializeField — FieldMeta 驱动序列化")

	// 有 serializer 元数据的字段
	meta := item.FieldMeta{"serializer": "format_cny"}
	result := feedexport.SerializeField(meta, "price", 49.99)
	fmt.Printf("  SerializeField(meta={serializer:format_cny}, \"price\", 49.99) = %v\n", result)

	// 无 serializer 元数据 → 返回原始值
	metaEmpty := item.FieldMeta{"other": "value"}
	result = feedexport.SerializeField(metaEmpty, "name", "Go Book")
	fmt.Printf("  SerializeField(meta={other:value}, \"name\", \"Go Book\") = %v\n", result)

	// nil meta → 返回原始值
	result = feedexport.SerializeField(nil, "field", 42)
	fmt.Printf("  SerializeField(nil, \"field\", 42) = %v\n", result)

	// 通过 struct Item 演示端到端：Product.Price 有 serializer=format_cny
	product := &Product{Name: "Go in Action", Price: 39.99, Category: "编程", InStock: true}
	adapter := item.Adapt(product)
	priceMeta := adapter.FieldMeta("price")
	priceVal, _ := adapter.GetField("price")
	serialized := feedexport.SerializeField(priceMeta, "price", priceVal)
	fmt.Printf("  Product.Price 通过 FieldMeta 序列化: %v → %v\n", priceVal, serialized)

	// JSON Exporter 会自动调用 SerializeField
	var buf bytes.Buffer
	opts := feedexport.DefaultExporterOptions()
	opts.FieldsToExport = []string{"name", "price"}
	exporter := feedexport.NewJSONExporter(&buf, opts)
	_ = exporter.StartExporting()
	_ = exporter.ExportItem(product) // struct Item，price 字段会被自动序列化
	_ = exporter.FinishExporting()
	fmt.Printf("  JSON 输出（price 自动序列化）: %s\n", buf.String())
}

// ============================================================================
// 演示 10：URIParams — URI 模板渲染
// ============================================================================

func demoURIParams() {
	printSection("10. URIParams — URI 模板占位符渲染")

	// NewURIParams 生成默认参数
	params := feedexport.NewURIParams("my-spider")
	fmt.Printf("  NewURIParams(\"my-spider\"):\n")
	fmt.Printf("    SpiderName: %q\n", params.SpiderName)
	fmt.Printf("    Time:       %q\n", params.Time)
	fmt.Printf("    BatchTime:  %q\n", params.BatchTime)
	fmt.Printf("    BatchID:    %d\n", params.BatchID)

	// Render — 替换占位符
	template1 := "output/%%(name)s/%%(time)s/data.json"
	// 注意：实际使用时占位符不需要双 %，这里用单 % 演示
	template1 = "output/%(name)s/%(time)s/data.json"
	rendered := params.Render(template1)
	fmt.Printf("\n  模板: %q\n  渲染: %q\n", template1, rendered)

	// batch_id 占位符
	template2 := "%(name)s-batch-%(batch_id)d.jsonl"
	rendered = params.Render(template2)
	fmt.Printf("\n  模板: %q\n  渲染: %q\n", template2, rendered)

	// batch_time 占位符
	template3 := "%(name)s-%(batch_time)s.csv"
	rendered = params.Render(template3)
	fmt.Printf("\n  模板: %q\n  渲染: %q\n", template3, rendered)

	// Extra 自定义变量
	params.Extra["env"] = "production"
	params.Extra["region"] = "cn-east"
	template4 := "%(env)s/%(region)s/%(name)s.json"
	rendered = params.Render(template4)
	fmt.Printf("\n  Extra 变量:\n  模板: %q\n  渲染: %q\n", template4, rendered)

	// 不识别的占位符保留原样
	template5 := "%(name)s-%(unknown)s.json"
	rendered = params.Render(template5)
	fmt.Printf("\n  未知占位符:\n  模板: %q\n  渲染: %q\n", template5, rendered)
}

// ============================================================================
// 演示 11：NewStorageForURI — 自动选择存储后端
// ============================================================================

func demoStorageForURI() {
	printSection("11. NewStorageForURI — 自动选择存储后端")

	// 普通路径 → FileStorage
	s1, err := feedexport.NewStorageForURI("output.json", true)
	fmt.Printf("  NewStorageForURI(\"output.json\"): 类型=%T, err=%v\n", s1, err)

	// file:// URI → FileStorage
	s2, err := feedexport.NewStorageForURI("file:///tmp/output.json", false)
	fmt.Printf("  NewStorageForURI(\"file:///tmp/output.json\"): 类型=%T, err=%v\n", s2, err)

	// stdout: → StdoutStorage
	s3, err := feedexport.NewStorageForURI("stdout:", false)
	fmt.Printf("  NewStorageForURI(\"stdout:\"): 类型=%T, err=%v\n", s3, err)

	// "-" → StdoutStorage
	s4, err := feedexport.NewStorageForURI("-", false)
	fmt.Printf("  NewStorageForURI(\"-\"): 类型=%T, err=%v\n", s4, err)

	// 不支持的 scheme → 错误
	_, err = feedexport.NewStorageForURI("s3://bucket/key", false)
	fmt.Printf("  NewStorageForURI(\"s3://...\"): err=%v\n", err)

	// 空 URI → 错误
	_, err = feedexport.NewStorageForURI("", false)
	fmt.Printf("  NewStorageForURI(\"\"): err=%v\n", err)

	// 直接构造 FileStorage
	fs, err := feedexport.NewFileStorage("/tmp/test-feedexport.json", true)
	if err == nil {
		fmt.Printf("\n  NewFileStorage 路径: %q\n", fs.Path())
	}

	// 直接构造 StdoutStorage
	ss := feedexport.NewStdoutStorage()
	fmt.Printf("  NewStdoutStorage 类型: %T\n", ss)
}

// ============================================================================
// 演示 12：FeedSlot — 直接使用导出任务
// ============================================================================

func demoFeedSlotDirect() {
	printSection("12. FeedSlot — 直接使用导出任务")

	outDir := mustTempDir("feedslot-demo")
	defer os.RemoveAll(outDir)

	// 创建 FeedSlot
	cfg := feedexport.FeedConfig{
		URI:       filepath.Join(outDir, "products.json"),
		Format:    feedexport.FormatJSON,
		Overwrite: true,
		Options: feedexport.ExporterOptions{
			Encoding:       "utf-8",
			Indent:         2,
			FieldsToExport: []string{"name", "price", "in_stock"},
		},
		Filter: func(it any) bool {
			// 只导出有库存的产品
			adapter := item.Adapt(it)
			if adapter == nil {
				return false
			}
			v, ok := adapter.GetField("in_stock")
			if !ok {
				return true
			}
			b, _ := v.(bool)
			return b
		},
	}

	slot, err := feedexport.NewFeedSlot(cfg, nil)
	if err != nil {
		fmt.Printf("  NewFeedSlot 错误: %v\n", err)
		return
	}
	fmt.Printf("  NewFeedSlot URI: %q\n", slot.URI())

	// 模拟 Spider
	dummySpider := &productSpider{Base: spider.Base{SpiderName: "demo"}}
	ctx := context.Background()

	// Start
	if err := slot.Start(ctx, dummySpider); err != nil {
		fmt.Printf("  Start 错误: %v\n", err)
		return
	}

	// ExportItem — 导出多个 Item
	products := []*Product{
		{Name: "Go in Action", Price: 39.99, InStock: true},
		{Name: "Learning Go", Price: 42.00, InStock: false},  // 被 Filter 过滤
		{Name: "100 Go Mistakes", Price: 35.50, InStock: true},
	}
	for _, p := range products {
		_ = slot.ExportItem(ctx, dummySpider, p)
	}

	// ItemCount — 查看已导出数量（被过滤的不计入）
	fmt.Printf("  ItemCount(): %d (1 个被 Filter 过滤)\n", slot.ItemCount())

	// Close
	if err := slot.Close(ctx, dummySpider); err != nil {
		fmt.Printf("  Close 错误: %v\n", err)
	}

	// 读取输出文件
	data, _ := os.ReadFile(filepath.Join(outDir, "products.json"))
	fmt.Printf("  输出文件内容:\n")
	for _, line := range strings.Split(string(data), "\n") {
		fmt.Printf("  %s\n", line)
	}

	// 演示 StoreEmpty=false：无 Item 时不创建文件
	cfg2 := feedexport.FeedConfig{
		URI:        filepath.Join(outDir, "empty.json"),
		Format:     feedexport.FormatJSON,
		Overwrite:  true,
		StoreEmpty: false,
	}
	slot2, _ := feedexport.NewFeedSlot(cfg2, nil)
	_ = slot2.Close(ctx, dummySpider) // 不写入任何 Item 直接关闭
	_, err = os.Stat(filepath.Join(outDir, "empty.json"))
	fmt.Printf("\n  StoreEmpty=false, 无 Item: 文件存在=%v\n", err == nil)

	// 演示 StoreEmpty=true：无 Item 也创建文件
	cfg3 := feedexport.FeedConfig{
		URI:        filepath.Join(outDir, "empty-store.json"),
		Format:     feedexport.FormatJSON,
		Overwrite:  true,
		StoreEmpty: true,
	}
	slot3, _ := feedexport.NewFeedSlot(cfg3, nil)
	_ = slot3.Close(ctx, dummySpider)
	info, err := os.Stat(filepath.Join(outDir, "empty-store.json"))
	fmt.Printf("  StoreEmpty=true, 无 Item: 文件存在=%v, 大小=%d bytes\n", err == nil, safeSize(info))
}

// ============================================================================
// 演示 13：Crawler 集成 — 端到端 Feed Export
// ============================================================================

func demoCrawlerIntegration() {
	printSection("13. Crawler.AddFeed — 端到端集成")

	site := newProductSite()
	defer site.Close()

	outDir := mustTempDir("crawler-feedexport")
	defer os.RemoveAll(outDir)

	c := crawler.NewDefault()

	// 1) JSON 格式 — 带缩进 + FieldsToExport + URI 模板
	c.AddFeed(feedexport.FeedConfig{
		URI:       filepath.Join(outDir, "%(name)s.json"),
		Format:    feedexport.FormatJSON,
		Overwrite: true,
		Options: feedexport.ExporterOptions{
			Indent:         2,
			FieldsToExport: []string{"name", "price", "category", "in_stock"},
		},
	})

	// 2) JSON Lines 格式 — 全字段导出
	c.AddFeed(feedexport.FeedConfig{
		URI:       filepath.Join(outDir, "products.jsonl"),
		Format:    feedexport.FormatJSONLines,
		Overwrite: true,
	})

	// 3) CSV 格式 — 只导出有库存的 + 自定义多值分隔符
	c.AddFeed(feedexport.FeedConfig{
		URI:       filepath.Join(outDir, "in-stock.csv"),
		Format:    feedexport.FormatCSV,
		Overwrite: true,
		Options: feedexport.ExporterOptions{
			FieldsToExport:     []string{"name", "price", "tags"},
			IncludeHeadersLine: true,
			JoinMultivalued:    ";",
		},
		Filter: func(it any) bool {
			adapter := item.Adapt(it)
			if adapter == nil {
				return false
			}
			v, ok := adapter.GetField("in_stock")
			if !ok {
				return true
			}
			b, _ := v.(bool)
			return b
		},
	})

	// 4) XML 格式 — 自定义元素名 + StoreEmpty
	c.AddFeed(feedexport.FeedConfig{
		URI:        filepath.Join(outDir, "products.xml"),
		Format:     feedexport.FormatXML,
		Overwrite:  true,
		StoreEmpty: true,
		Options: feedexport.ExporterOptions{
			Indent:         2,
			RootElement:    "catalog",
			ItemElement:    "product",
			FieldsToExport: []string{"name", "price", "category"},
		},
	})

	// 5) 自定义 Storage — 显式指定 StdoutStorage（输出到标准输出）
	// 注意：实际运行时会混入 stdout，此处仅演示 API
	// c.AddFeed(feedexport.FeedConfig{
	//     URI:     "stdout:",
	//     Format:  feedexport.FormatJSONLines,
	//     Storage: feedexport.NewStdoutStorage(),
	// })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fmt.Println("  启动爬虫（struct Item + 4 种格式同时导出）...")
	start := time.Now()
	if err := c.Run(ctx, newProductSpider(site.URL)); err != nil &&
		err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Printf("  爬虫错误: %v\n", err)
		return
	}
	fmt.Printf("  爬虫完成，耗时 %v\n\n", time.Since(start))

	// 展示输出文件
	entries, _ := os.ReadDir(outDir)
	for _, e := range entries {
		info, _ := e.Info()
		fmt.Printf("  📄 %s (%d bytes)\n", e.Name(), info.Size())

		data, _ := os.ReadFile(filepath.Join(outDir, e.Name()))
		content := string(data)
		// 截断过长内容
		lines := strings.Split(content, "\n")
		if len(lines) > 15 {
			for _, line := range lines[:12] {
				fmt.Printf("     %s\n", line)
			}
			fmt.Printf("     ... (共 %d 行)\n", len(lines))
		} else {
			for _, line := range lines {
				fmt.Printf("     %s\n", line)
			}
		}
		fmt.Println()
	}
}

// ============================================================================
// 辅助函数
// ============================================================================

func printSection(title string) {
	fmt.Printf("\n%s\n%s\n", title, strings.Repeat("─", 60))
}

func mustTempDir(prefix string) string {
	dir, err := os.MkdirTemp("", prefix+"-*")
	if err != nil {
		fmt.Printf("创建临时目录失败: %v\n", err)
		os.Exit(1)
	}
	return dir
}

func safeSize(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}