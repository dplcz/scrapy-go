package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// runGenerateAdapter 执行 generate-adapter 命令。
//
// 从指定的 Go 源文件中扫描带有 `item` struct tag 的结构体，
// 自动生成对应的 ItemAdapter 实现代码，消除运行时反射开销。
//
// 用法：
//
//	scrapy-go generate-adapter -type=Book -input=items.go -output=items_adapter_gen.go
//
// 或通过 go generate 指令：
//
//	//go:generate scrapy-go generate-adapter -type=Book
//
// 生成的代码实现 item.ItemAdapter 接口，包含：
//   - Item() any
//   - FieldNames() []string
//   - GetField(name string) (any, bool)
//   - SetField(name string, value any) error
//   - HasField(name string) bool
//   - AsMap() map[string]any
//   - Len() int
//   - FieldMeta(name string) item.FieldMeta
func runGenerateAdapter(args []string) error {
	fs := flag.NewFlagSet("generate-adapter", flag.ContinueOnError)
	fs.Usage = printGenerateAdapterUsage

	typeName := fs.String("type", "", "要生成 Adapter 的结构体名称（必填）")
	inputFile := fs.String("input", "", "输入 Go 源文件路径（默认为 $GOFILE 环境变量）")
	outputFile := fs.String("output", "", "输出文件路径（默认为 <type>_adapter_gen.go）")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *typeName == "" {
		return fmt.Errorf("必须指定 -type 参数")
	}

	// 确定输入文件
	input := *inputFile
	if input == "" {
		input = os.Getenv("GOFILE")
	}
	if input == "" {
		return fmt.Errorf("必须指定 -input 参数或通过 go generate 调用（自动设置 $GOFILE）")
	}

	// 确定输出文件
	output := *outputFile
	if output == "" {
		output = strings.ToLower(*typeName) + "_adapter_gen.go"
	}

	// 解析源文件
	structInfo, err := parseStructFromFile(input, *typeName)
	if err != nil {
		return fmt.Errorf("解析结构体失败: %w", err)
	}

	// 生成代码
	code, err := generateAdapterCode(structInfo)
	if err != nil {
		return fmt.Errorf("生成代码失败: %w", err)
	}

	// 格式化代码
	formatted, err := format.Source(code)
	if err != nil {
		// 如果格式化失败，输出原始代码便于调试
		formatted = code
	}

	// 写入输出文件
	outputPath := filepath.Join(filepath.Dir(input), output)
	if err := os.WriteFile(outputPath, formatted, 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	fmt.Printf("已生成: %s (结构体: %s, 字段数: %d)\n", outputPath, *typeName, len(structInfo.Fields))
	return nil
}

// printGenerateAdapterUsage 打印 generate-adapter 命令的帮助信息。
func printGenerateAdapterUsage() {
	fmt.Println(`用法: scrapy-go generate-adapter [选项]

从 Go 结构体自动生成 ItemAdapter 实现代码，消除运行时反射开销。

选项:
  -type string    要生成 Adapter 的结构体名称（必填）
  -input string   输入 Go 源文件路径（默认为 $GOFILE）
  -output string  输出文件路径（默认为 <type>_adapter_gen.go）

示例:
  scrapy-go generate-adapter -type=Book -input=items.go
  scrapy-go generate-adapter -type=Book -input=items.go -output=book_adapter.go

通过 go generate 使用:
  //go:generate scrapy-go generate-adapter -type=Book`)
}

// ============================================================================
// 结构体解析
// ============================================================================

// structInfo 存储解析后的结构体信息。
type structInfo struct {
	Package string
	Name    string
	Fields  []fieldInfo
}

// fieldInfo 存储解析后的字段信息。
type fieldInfo struct {
	GoName    string // Go 字段名
	ItemName  string // item tag 中的字段名
	GoType    string // Go 类型字符串
	Required  bool   // 是否标记 required
	Default   string // 默认值（空字符串表示无默认值）
	Omitempty bool   // 是否标记 omitempty
	RawTag    string // 原始 item tag
}

// parseStructFromFile 从 Go 源文件中解析指定结构体的信息。
func parseStructFromFile(filename, typeName string) (*structInfo, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("解析文件 %s 失败: %w", filename, err)
	}

	info := &structInfo{
		Package: f.Name.Name,
		Name:    typeName,
	}

	// 遍历 AST 查找目标结构体
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != typeName {
			return true
		}

		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}

		found = true
		for _, field := range st.Fields.List {
			if len(field.Names) == 0 {
				continue // 匿名字段，跳过
			}
			if !field.Names[0].IsExported() {
				continue // 非导出字段，跳过
			}

			fi := fieldInfo{
				GoName: field.Names[0].Name,
				GoType: typeToString(field.Type),
			}

			// 解析 tag
			if field.Tag != nil {
				tag := strings.Trim(field.Tag.Value, "`")
				fi.ItemName, fi.Required, fi.Default, fi.Omitempty, fi.RawTag = parseItemTagFromRaw(tag)
			}

			// 如果没有 item tag 名称，使用 Go 字段名
			if fi.ItemName == "" {
				fi.ItemName = fi.GoName
			}

			info.Fields = append(info.Fields, fi)
		}
		return false
	})

	if !found {
		return nil, fmt.Errorf("未找到结构体 %s", typeName)
	}
	if len(info.Fields) == 0 {
		return nil, fmt.Errorf("结构体 %s 没有导出字段", typeName)
	}

	return info, nil
}

// typeToString 将 AST 类型表达式转为字符串。
func typeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return typeToString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + typeToString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeToString(t.Elt)
		}
		return fmt.Sprintf("[%s]%s", typeToString(t.Len), typeToString(t.Elt))
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", typeToString(t.Key), typeToString(t.Value))
	case *ast.InterfaceType:
		return "any"
	case *ast.BasicLit:
		return t.Value
	default:
		return "any"
	}
}

// parseItemTagFromRaw 从原始 struct tag 字符串中提取 item tag 信息。
func parseItemTagFromRaw(rawTag string) (name string, required bool, defaultVal string, omitempty bool, itemTag string) {
	// 查找 item:"..." 部分
	itemTag = extractTagValue(rawTag, "item")
	if itemTag == "" {
		// 退回到 json tag
		jsonTag := extractTagValue(rawTag, "json")
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			name = strings.TrimSpace(parts[0])
			if name == "-" {
				name = ""
			}
		}
		return
	}

	parts := strings.Split(itemTag, ",")
	if len(parts) > 0 {
		name = strings.TrimSpace(parts[0])
		if name == "-" {
			name = ""
			return
		}
	}

	for i := 1; i < len(parts); i++ {
		p := strings.TrimSpace(parts[i])
		switch {
		case p == "required":
			required = true
		case p == "omitempty":
			omitempty = true
		case strings.HasPrefix(p, "default="):
			defaultVal = strings.TrimPrefix(p, "default=")
		}
	}
	return
}

// extractTagValue 从原始 tag 字符串中提取指定 key 的值。
func extractTagValue(rawTag, key string) string {
	search := key + `:"`
	idx := strings.Index(rawTag, search)
	if idx < 0 {
		return ""
	}
	rest := rawTag[idx+len(search):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// ============================================================================
// 代码生成
// ============================================================================

// generateAdapterCode 生成 ItemAdapter 实现代码。
func generateAdapterCode(info *structInfo) ([]byte, error) {
	tmpl, err := template.New("adapter").Parse(adapterTemplate)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, info); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// adapterTemplate 是生成 ItemAdapter 实现的模板。
const adapterTemplate = `// Code generated by scrapy-go generate-adapter; DO NOT EDIT.
//
//go:generate scrapy-go generate-adapter -type={{.Name}}

package {{.Package}}

import (
	"github.com/dplcz/scrapy-go/pkg/item"
)

// {{.Name}}Adapter 是 {{.Name}} 的编译期生成的 ItemAdapter 实现。
// 相比反射版本（StructAdapter），消除了运行时反射开销。
type {{.Name}}Adapter struct {
	item *{{.Name}}
}

// New{{.Name}}Adapter 创建 {{.Name}} 的 ItemAdapter。
func New{{.Name}}Adapter(v *{{.Name}}) *{{.Name}}Adapter {
	return &{{.Name}}Adapter{item: v}
}

// Item 返回底层原始 Item。
func (a *{{.Name}}Adapter) Item() any {
	return a.item
}

// FieldNames 返回所有字段名（按声明顺序）。
func (a *{{.Name}}Adapter) FieldNames() []string {
	return []string{ {{- range $i, $f := .Fields}}{{if $i}}, {{end}}"{{$f.ItemName}}"{{end -}} }
}

// GetField 读取字段值。
func (a *{{.Name}}Adapter) GetField(name string) (any, bool) {
	switch name {
{{- range .Fields}}
	case "{{.ItemName}}":
		return a.item.{{.GoName}}, true
{{- end}}
	}
	return nil, false
}

// SetField 写入字段值。
func (a *{{.Name}}Adapter) SetField(name string, value any) error {
	switch name {
{{- range .Fields}}
	case "{{.ItemName}}":
		v, ok := value.({{.GoType}})
		if !ok {
			return item.ErrFieldReadOnly
		}
		a.item.{{.GoName}} = v
		return nil
{{- end}}
	}
	return item.ErrFieldNotFound
}

// HasField 判断字段是否存在。
func (a *{{.Name}}Adapter) HasField(name string) bool {
	switch name {
{{- range .Fields}}
	case "{{.ItemName}}":
		return true
{{- end}}
	}
	return false
}

// AsMap 返回所有字段的 map 快照。
func (a *{{.Name}}Adapter) AsMap() map[string]any {
	return map[string]any{
{{- range .Fields}}
		"{{.ItemName}}": a.item.{{.GoName}},
{{- end}}
	}
}

// Len 返回字段数量。
func (a *{{.Name}}Adapter) Len() int {
	return {{len .Fields}}
}

// FieldMeta 返回字段的元数据。
func (a *{{.Name}}Adapter) FieldMeta(name string) item.FieldMeta {
	switch name {
{{- range .Fields}}
	case "{{.ItemName}}":
		meta := item.FieldMeta{}
{{- if .Required}}
		meta["required"] = true
{{- end}}
{{- if .Default}}
		meta["default"] = "{{.Default}}"
{{- end}}
{{- if .Omitempty}}
		meta["omitempty"] = true
{{- end}}
		if len(meta) == 0 {
			return nil
		}
		return meta
{{- end}}
	}
	return nil
}

// 编译期接口满足性检查。
var _ item.ItemAdapter = (*{{.Name}}Adapter)(nil)
`
