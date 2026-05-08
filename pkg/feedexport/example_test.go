package feedexport_test

import (
	"fmt"

	"github.com/dplcz/scrapy-go/pkg/feedexport"
)

// ExampleNormalizeFormat 演示格式名归一化。
func ExampleNormalizeFormat() {
	fmt.Println(feedexport.NormalizeFormat("json"))
	fmt.Println(feedexport.NormalizeFormat("jl"))
	fmt.Println(feedexport.NormalizeFormat("jsonl"))
	fmt.Println(feedexport.NormalizeFormat("jsonlines"))
	fmt.Println(feedexport.NormalizeFormat("csv"))
	fmt.Println(feedexport.NormalizeFormat("xml"))

	// Output:
	// json
	// jsonlines
	// jsonlines
	// jsonlines
	// csv
	// xml
}

// ExampleDefaultExporterOptions 演示默认导出配置。
func ExampleDefaultExporterOptions() {
	opts := feedexport.DefaultExporterOptions()

	fmt.Println("Encoding:", opts.Encoding)
	fmt.Println("Indent:", opts.Indent)
	fmt.Println("IncludeHeaders:", opts.IncludeHeadersLine)
	fmt.Println("JoinMultivalued:", opts.JoinMultivalued)
	fmt.Println("ItemElement:", opts.ItemElement)
	fmt.Println("RootElement:", opts.RootElement)

	// Output:
	// Encoding: utf-8
	// Indent: 0
	// IncludeHeaders: true
	// JoinMultivalued: ,
	// ItemElement: item
	// RootElement: items
}

// ExampleAcceptAll 演示默认的 Item 过滤器。
func ExampleAcceptAll() {
	// AcceptAll 接受所有 Item
	fmt.Println(feedexport.AcceptAll(map[string]any{"title": "test"}))
	fmt.Println(feedexport.AcceptAll(nil))
	fmt.Println(feedexport.AcceptAll("anything"))

	// Output:
	// true
	// true
	// true
}
