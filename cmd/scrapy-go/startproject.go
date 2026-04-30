package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed templates/project/*
var projectTemplates embed.FS

// runStartProject 执行 startproject 命令，创建新的 scrapy-go 项目骨架。
//
// 用法：
//
//	scrapy-go startproject <project_name> [project_dir]
//
// 生成的项目结构：
//
//	<project_dir>/
//	├── main.go              # 入口文件
//	├── settings.go          # 项目配置
//	├── middlewares.go       # 自定义中间件
//	├── pipelines.go         # 自定义 Pipeline
//	├── items.go             # Item 定义
//	├── spiders/             # 爬虫目录
//	│   └── .gitkeep
//	├── go.mod               # Go 模块文件
//	└── scrapy-go.toml       # 框架配置文件
func runStartProject(args []string) error {
	fs := flag.NewFlagSet("startproject", flag.ContinueOnError)
	fs.Usage = printStartProjectUsage

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	positional := fs.Args()
	if len(positional) < 1 || len(positional) > 2 {
		printStartProjectUsage()
		return fmt.Errorf("需要 1-2 个参数: <project_name> [project_dir]")
	}

	projectName := positional[0]

	// 验证项目名称
	if !isValidProjectName(projectName) {
		return fmt.Errorf("项目名称 %q 无效: 必须以字母或下划线开头，只能包含字母、数字和下划线", projectName)
	}

	// 确定项目目录
	projectDir := projectName
	if len(positional) == 2 {
		projectDir = positional[1]
	}

	// 检查目录是否已存在且包含 go.mod
	if _, err := os.Stat(filepath.Join(projectDir, "go.mod")); err == nil {
		return fmt.Errorf("go.mod 已存在于 %s，该目录可能已是一个 Go 项目", projectDir)
	}

	// 创建项目目录
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("创建项目目录失败: %w", err)
	}

	// 创建 spiders 子目录
	spidersDir := filepath.Join(projectDir, "spiders")
	if err := os.MkdirAll(spidersDir, 0o755); err != nil {
		return fmt.Errorf("创建 spiders 目录失败: %w", err)
	}

	// 创建 .gitkeep 文件
	if err := os.WriteFile(filepath.Join(spidersDir, ".gitkeep"), []byte(""), 0o644); err != nil {
		return fmt.Errorf("创建 .gitkeep 失败: %w", err)
	}

	// 模板数据
	data := templateData{
		ProjectName: projectName,
		ModulePath:  projectName,
		ClassName:   toCamelCase(projectName),
		Version:     Version,
	}

	// 渲染并写入模板文件
	templates := []struct {
		tmplPath string
		outPath  string
	}{
		{"templates/project/main.go.tmpl", filepath.Join(projectDir, "main.go")},
		{"templates/project/settings.go.tmpl", filepath.Join(projectDir, "settings.go")},
		{"templates/project/middlewares.go.tmpl", filepath.Join(projectDir, "middlewares.go")},
		{"templates/project/pipelines.go.tmpl", filepath.Join(projectDir, "pipelines.go")},
		{"templates/project/items.go.tmpl", filepath.Join(projectDir, "items.go")},
		{"templates/project/go.mod.tmpl", filepath.Join(projectDir, "go.mod")},
		{"templates/project/scrapy-go.toml.tmpl", filepath.Join(projectDir, "scrapy-go.toml")},
	}

	for _, t := range templates {
		if err := renderTemplate(t.tmplPath, t.outPath, data); err != nil {
			return fmt.Errorf("渲染模板 %s 失败: %w", t.tmplPath, err)
		}
	}

	// 输出成功信息
	absDir, _ := filepath.Abs(projectDir)
	fmt.Printf("已创建新的 scrapy-go 项目 '%s'，位于:\n    %s\n\n", projectName, absDir)
	fmt.Printf("你可以使用以下命令创建第一个爬虫:\n    cd %s\n    scrapy-go genspider example example.com\n", projectDir)

	return nil
}

// templateData 是模板渲染的数据结构。
type templateData struct {
	ProjectName string // 项目名称（小写，用作包名和模块名）
	ModulePath  string // Go 模块路径
	ClassName   string // 驼峰命名（用于类型名）
	Version     string // scrapy-go 版本
}

// isValidProjectName 验证项目名称是否合法。
// 项目名称必须以字母或下划线开头，只能包含字母、数字和下划线。
func isValidProjectName(name string) bool {
	matched, _ := regexp.MatchString(`^[a-zA-Z_][a-zA-Z0-9_]*$`, name)
	return matched
}

// toCamelCase 将下划线分隔的名称转换为大驼峰命名。
// 例如: "my_project" -> "MyProject"
func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	var result strings.Builder
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		result.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			result.WriteString(part[1:])
		}
	}
	// 如果没有下划线，确保首字母大写
	if result.Len() == 0 {
		return strings.ToUpper(s[:1]) + s[1:]
	}
	return result.String()
}

// renderTemplate 读取嵌入的模板文件并渲染到目标路径。
func renderTemplate(tmplPath, outPath string, data templateData) error {
	content, err := projectTemplates.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("读取模板文件失败: %w", err)
	}

	tmpl, err := template.New(filepath.Base(tmplPath)).Parse(string(content))
	if err != nil {
		return fmt.Errorf("解析模板失败: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("渲染模板失败: %w", err)
	}

	return nil
}

// printStartProjectUsage 打印 startproject 命令的帮助信息。
func printStartProjectUsage() {
	fmt.Println(`用法: scrapy-go startproject <project_name> [project_dir]

创建新的 scrapy-go 项目骨架。

参数:
  project_name    项目名称（必须以字母或下划线开头，只能包含字母、数字和下划线）
  project_dir     项目目录（可选，默认与项目名称相同）

生成的项目结构:
  <project_dir>/
  ├── main.go              入口文件
  ├── settings.go          项目配置
  ├── middlewares.go       自定义中间件模板
  ├── pipelines.go         自定义 Pipeline 模板
  ├── items.go             Item 定义
  ├── spiders/             爬虫目录
  │   └── .gitkeep
  ├── go.mod               Go 模块文件
  └── scrapy-go.toml       框架配置文件

示例:
  scrapy-go startproject myproject
  scrapy-go startproject myproject ./projects/myproject`)
}
