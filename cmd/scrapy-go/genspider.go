package main

import (
	"embed"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates/spiders/*
var spiderTemplates embed.FS

// spiderTemplateData 是爬虫模板渲染的数据结构。
type spiderTemplateData struct {
	Name      string // 爬虫名称
	ClassName string // 驼峰命名的类型名（如 QuotesSpider）
	Domain    string // 目标域名
	URL       string // 完整 URL（含 scheme）
	Module    string // 模块名（文件名安全）
}

// isInProject 检测当前目录是否为 scrapy-go 项目（存在 scrapy-go.toml）。
func isInProject() bool {
	_, err := os.Stat("scrapy-go.toml")
	return err == nil
}

// runGenSpider 执行 genspider 命令，使用模板生成新的爬虫文件。
//
// 用法：
//
//	scrapy-go genspider [options] <name> <domain>
//
// 选项：
//
//	-t <template>  指定模板（basic 或 crawl，默认 basic）
//	-l             列出可用模板
//
// 必须在 scrapy-go 项目中（当前目录存在 scrapy-go.toml）执行，
// 爬虫文件将生成到 spiders/ 目录，使用 package spiders。
func runGenSpider(args []string) error {
	fs := flag.NewFlagSet("genspider", flag.ContinueOnError)
	fs.Usage = printGenSpiderUsage

	templateName := fs.String("t", "basic", "指定模板（basic 或 crawl）")
	listMode := fs.Bool("l", false, "列出可用模板")

	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError 模式下，-h/--help 会返回 flag.ErrHelp
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	// 列出可用模板
	if *listMode {
		listSpiderTemplates()
		return nil
	}

	// 验证位置参数
	positional := fs.Args()
	if len(positional) != 2 {
		printGenSpiderUsage()
		return fmt.Errorf("需要 2 个参数: <name> <domain>")
	}

	name := positional[0]
	domain := positional[1]

	// 验证爬虫名称
	if !isValidProjectName(name) {
		return fmt.Errorf("爬虫名称 %q 无效: 必须以字母或下划线开头，只能包含字母、数字和下划线", name)
	}

	// 提取域名并补全 URL scheme
	domainStr := extractDomain(domain)
	fullURL := verifyURLScheme(domain)

	// 检测是否在 scrapy-go 项目中
	if !isInProject() {
		return fmt.Errorf("当前目录不是 scrapy-go 项目（未找到 scrapy-go.toml），请在项目根目录下执行此命令")
	}

	// 确定输出路径：输出到 spiders/ 目录
	fileName := sanitizeFileName(name) + ".go"
	spidersDir := "spiders"
	if err := os.MkdirAll(spidersDir, 0o755); err != nil {
		return fmt.Errorf("创建 spiders 目录失败: %w", err)
	}
	outputFile := filepath.Join(spidersDir, fileName)

	// 检查文件是否已存在
	if _, err := os.Stat(outputFile); err == nil {
		return fmt.Errorf("文件 %s 已存在，使用其他名称或删除已有文件", outputFile)
	}

	// 确定模板路径
	tmplPath := fmt.Sprintf("templates/spiders/project/%s.go.tmpl", *templateName)
	if _, err := spiderTemplates.ReadFile(tmplPath); err != nil {
		return fmt.Errorf("模板 %q 不存在，使用 -l 查看可用模板", *templateName)
	}

	// 准备模板数据
	data := spiderTemplateData{
		Name:      name,
		ClassName: toSpiderClassName(name),
		Domain:    domainStr,
		URL:       fullURL,
		Module:    sanitizeFileName(name),
	}

	// 渲染模板
	if err := renderSpiderTemplate(tmplPath, outputFile, data); err != nil {
		return err
	}

	fmt.Printf("已创建爬虫 '%s'（使用模板 '%s'）:\n    %s\n", name, *templateName, outputFile)
	return nil
}

// extractDomain 从 URL 字符串中提取域名。
func extractDomain(rawURL string) string {
	// 如果没有 scheme，添加一个用于解析
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + strings.TrimLeft(rawURL, "/")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		// 回退：直接使用输入作为域名
		return strings.TrimRight(rawURL, "/")
	}
	return u.Hostname()
}

// verifyURLScheme 检查 URL 是否有 scheme，没有则添加 https。
func verifyURLScheme(rawURL string) string {
	if strings.Contains(rawURL, "://") {
		return rawURL
	}
	return "https://" + strings.TrimLeft(rawURL, "/")
}

// sanitizeFileName 将名称转换为安全的文件名。
// 将连字符和点替换为下划线。
func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}

// toSpiderClassName 将爬虫名称转换为 Go 类型名。
// 例如: "quotes" -> "QuotesSpider", "my_spider" -> "MySpiderSpider"
func toSpiderClassName(name string) string {
	camel := toCamelCase(name)
	if !strings.HasSuffix(strings.ToLower(camel), "spider") {
		camel += "Spider"
	}
	return camel
}

// listSpiderTemplates 列出所有可用的爬虫模板。
func listSpiderTemplates() {
	fmt.Println("可用模板:")
	fmt.Println("  basic    基础爬虫模板（默认）")
	fmt.Println("  crawl    基于规则的 CrawlSpider 模板")
}

// renderSpiderTemplate 渲染爬虫模板到目标文件。
func renderSpiderTemplate(tmplPath, outPath string, data spiderTemplateData) error {
	content, err := spiderTemplates.ReadFile(tmplPath)
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

// printGenSpiderUsage 打印 genspider 命令的帮助信息。
func printGenSpiderUsage() {
	fmt.Println(`用法: scrapy-go genspider [options] <name> <domain>

使用模板生成新的爬虫文件。

必须在 scrapy-go 项目根目录下（存在 scrapy-go.toml）执行，
爬虫文件将生成到 spiders/ 目录，使用 package spiders。

参数:
  name      爬虫名称（必须以字母或下划线开头，只能包含字母、数字和下划线）
  domain    目标域名（如 quotes.toscrape.com）

选项:
  -t <name>    指定模板（默认: basic）
  -l           列出可用模板

可用模板:
  basic    基础爬虫模板（默认），包含 Parse 回调
  crawl    基于规则的 CrawlSpider 模板，支持自动链接提取

示例:
  scrapy-go genspider quotes quotes.toscrape.com
  scrapy-go genspider -t crawl articles blog.example.com`)
}