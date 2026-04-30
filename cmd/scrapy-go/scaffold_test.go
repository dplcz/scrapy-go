package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// startproject 命令测试
// ============================================================================

func TestRunStartProject_Basic(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myproject")

	err := runStartProject([]string{"myproject", projectDir})
	if err != nil {
		t.Fatalf("runStartProject 失败: %v", err)
	}

	// 验证生成的文件
	expectedFiles := []string{
		"main.go",
		"project/settings.go",
		"project/middlewares.go",
		"project/pipelines.go",
		"project/items.go",
		"go.mod",
		"scrapy-go.toml",
		"spiders/.gitkeep",
	}

	for _, f := range expectedFiles {
		path := filepath.Join(projectDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("期望文件 %s 存在，但未找到", f)
		}
	}
}

func TestRunStartProject_GoModContent(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "testmod")

	err := runStartProject([]string{"testmod", projectDir})
	if err != nil {
		t.Fatalf("runStartProject 失败: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		t.Fatalf("读取 go.mod 失败: %v", err)
	}

	goMod := string(content)
	if !strings.Contains(goMod, "module testmod") {
		t.Errorf("go.mod 应包含 'module testmod'，实际内容:\n%s", goMod)
	}
	if !strings.Contains(goMod, "github.com/dplcz/scrapy-go") {
		t.Errorf("go.mod 应包含 scrapy-go 依赖，实际内容:\n%s", goMod)
	}
}

func TestRunStartProject_MainGoContent(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "demo")

	err := runStartProject([]string{"demo", projectDir})
	if err != nil {
		t.Fatalf("runStartProject 失败: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if err != nil {
		t.Fatalf("读取 main.go 失败: %v", err)
	}

	mainGo := string(content)
	if !strings.Contains(mainGo, "package main") {
		t.Errorf("main.go 应包含 'package main'")
	}
	if !strings.Contains(mainGo, "crawler.New(crawler.WithSettings(projectSettings))") {
		t.Errorf("main.go 应包含 crawler.New(crawler.WithSettings(projectSettings))")
	}
	if !strings.Contains(mainGo, `"demo/project"`) {
		t.Errorf("main.go 应包含 project 子包导入")
	}
	if !strings.Contains(mainGo, "project.NewProjectSettings()") {
		t.Errorf("main.go 应包含 project.NewProjectSettings() 调用")
	}
}

func TestRunStartProject_MiddlewaresContent(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "my_project")

	err := runStartProject([]string{"my_project", projectDir})
	if err != nil {
		t.Fatalf("runStartProject 失败: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(projectDir, "project", "middlewares.go"))
	if err != nil {
		t.Fatalf("读取 project/middlewares.go 失败: %v", err)
	}

	mw := string(content)
	// 验证驼峰命名转换
	if !strings.Contains(mw, "MyProjectDownloaderMiddleware") {
		t.Errorf("middlewares.go 应包含 'MyProjectDownloaderMiddleware'，实际内容:\n%s", mw)
	}
	if !strings.Contains(mw, "MyProjectSpiderMiddleware") {
		t.Errorf("middlewares.go 应包含 'MyProjectSpiderMiddleware'，实际内容:\n%s", mw)
	}
}

func TestRunStartProject_InvalidName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"valid_name", false},
		{"ValidName", false},
		{"_private", false},
		{"123invalid", true},
		{"invalid-name", true},
		{"invalid.name", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "" {
				// 空名称会导致参数数量错误
				err := runStartProject([]string{})
				if err == nil {
					t.Error("期望空参数返回错误")
				}
				return
			}
			dir := t.TempDir()
			projectDir := filepath.Join(dir, "out")
			err := runStartProject([]string{tt.name, projectDir})
			if (err != nil) != tt.wantErr {
				t.Errorf("runStartProject(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestRunStartProject_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "existing")

	// 创建一个已有 go.mod 的目录
	os.MkdirAll(projectDir, 0o755)
	os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module existing"), 0o644)

	err := runStartProject([]string{"existing", projectDir})
	if err == nil {
		t.Error("期望已存在 go.mod 时返回错误")
	}
}

func TestRunStartProject_DefaultDir(t *testing.T) {
	// 测试不指定目录时使用项目名称作为目录
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	err := runStartProject([]string{"autodir"})
	if err != nil {
		t.Fatalf("runStartProject 失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "autodir", "main.go")); os.IsNotExist(err) {
		t.Error("期望在 autodir/ 目录下生成 main.go")
	}
}

// ============================================================================
// genspider 命令测试
// ============================================================================

func TestRunGenSpider_Basic(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// 创建 scrapy-go.toml 标记为项目目录
	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)

	err := runGenSpider([]string{"quotes", "quotes.toscrape.com"})
	if err != nil {
		t.Fatalf("runGenSpider 失败: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "spiders", "quotes.go"))
	if err != nil {
		t.Fatalf("读取生成的文件失败: %v", err)
	}

	code := string(content)
	if !strings.Contains(code, "QuotesSpider") {
		t.Errorf("生成的代码应包含 'QuotesSpider'")
	}
	if !strings.Contains(code, `SpiderName: "quotes"`) {
		t.Errorf("生成的代码应包含爬虫名称 'quotes'")
	}
	if !strings.Contains(code, "https://quotes.toscrape.com") {
		t.Errorf("生成的代码应包含完整 URL")
	}
	if !strings.Contains(code, "package spiders") {
		t.Errorf("生成的代码应使用 'package spiders'")
	}
}

func TestRunGenSpider_CrawlTemplate(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// 创建 scrapy-go.toml 标记为项目目录
	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)

	err := runGenSpider([]string{"-t", "crawl", "articles", "blog.example.com"})
	if err != nil {
		t.Fatalf("runGenSpider 失败: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "spiders", "articles.go"))
	if err != nil {
		t.Fatalf("读取生成的文件失败: %v", err)
	}

	code := string(content)
	if !strings.Contains(code, "ArticlesSpider") {
		t.Errorf("生成的代码应包含 'ArticlesSpider'")
	}
	if !strings.Contains(code, "spider.CrawlSpider") {
		t.Errorf("生成的代码应包含 'spider.CrawlSpider'")
	}
	if !strings.Contains(code, "linkextractor") {
		t.Errorf("生成的代码应包含 'linkextractor'")
	}
	if !strings.Contains(code, "spider.Rule") {
		t.Errorf("生成的代码应包含 'spider.Rule'")
	}
}

func TestRunGenSpider_InvalidTemplate(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// 创建 scrapy-go.toml 标记为项目目录
	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)

	err := runGenSpider([]string{"-t", "nonexistent", "test", "example.com"})
	if err == nil {
		t.Error("期望无效模板返回错误")
	}
}

func TestRunGenSpider_FileExists(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// 创建 scrapy-go.toml 标记为项目目录
	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)
	// 创建已有文件
	os.MkdirAll(filepath.Join(dir, "spiders"), 0o755)
	os.WriteFile(filepath.Join(dir, "spiders", "existing.go"), []byte("package spiders"), 0o644)

	err := runGenSpider([]string{"existing", "example.com"})
	if err == nil {
		t.Error("期望文件已存在时返回错误")
	}
}

func TestRunGenSpider_InvalidName(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	err := runGenSpider([]string{"123invalid", "example.com"})
	if err == nil {
		t.Error("期望无效名称返回错误")
	}
}

func TestRunGenSpider_URLSchemeCompletion(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// 创建 scrapy-go.toml 标记为项目目录
	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)

	err := runGenSpider([]string{"test_spider", "example.com"})
	if err != nil {
		t.Fatalf("runGenSpider 失败: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "spiders", "test_spider.go"))
	if err != nil {
		t.Fatalf("读取生成的文件失败: %v", err)
	}

	code := string(content)
	if !strings.Contains(code, "https://example.com") {
		t.Errorf("生成的代码应自动补全 https scheme，实际内容:\n%s", code)
	}
}

func TestRunGenSpider_ListTemplates(t *testing.T) {
	// 列出模板不应返回错误
	err := runGenSpider([]string{"-l"})
	if err != nil {
		t.Fatalf("列出模板失败: %v", err)
	}
}

func TestRunGenSpider_MissingArgs(t *testing.T) {
	err := runGenSpider([]string{"onlyname"})
	if err == nil {
		t.Error("期望参数不足时返回错误")
	}
}

// ============================================================================
// 辅助函数测试
// ============================================================================

func TestIsValidProjectName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"myproject", true},
		{"my_project", true},
		{"MyProject", true},
		{"_private", true},
		{"project123", true},
		{"123project", false},
		{"my-project", false},
		{"my.project", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidProjectName(tt.name)
			if got != tt.valid {
				t.Errorf("isValidProjectName(%q) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my_project", "MyProject"},
		{"hello", "Hello"},
		{"hello_world_test", "HelloWorldTest"},
		{"single", "Single"},
		{"UPPER", "UPPER"},
		{"a_b_c", "ABC"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toCamelCase(tt.input)
			if got != tt.want {
				t.Errorf("toCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/path", "example.com"},
		{"http://sub.example.com:8080/path", "sub.example.com"},
		{"example.com", "example.com"},
		{"example.com/path", "example.com"},
		{"//example.com/path", "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractDomain(tt.input)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestVerifyURLScheme(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com", "http://example.com"},
		{"example.com", "https://example.com"},
		{"example.com/path", "https://example.com/path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := verifyURLScheme(tt.input)
			if got != tt.want {
				t.Errorf("verifyURLScheme(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToSpiderClassName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"quotes", "QuotesSpider"},
		{"my_spider", "MySpider"},
		{"articles", "ArticlesSpider"},
		{"web_crawler", "WebCrawlerSpider"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toSpiderClassName(tt.input)
			if got != tt.want {
				t.Errorf("toSpiderClassName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"normal", "normal"},
		{"with-dash", "with_dash"},
		{"with.dot", "with_dot"},
		{"mixed-name.test", "mixed_name_test"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFileName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFileName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ============================================================================
// version 命令测试
// ============================================================================

func TestRunVersion(t *testing.T) {
	// version 命令不应 panic
	runVersion(nil)
	runVersion([]string{"-v"})
	runVersion([]string{"--verbose"})
}

// ============================================================================
// 端到端测试：生成的项目结构完整性
// ============================================================================

func TestStartProject_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "e2e_project")

	err := runStartProject([]string{"e2e_project", projectDir})
	if err != nil {
		t.Fatalf("runStartProject 失败: %v", err)
	}

	// 验证 scrapy-go.toml 内容
	tomlContent, err := os.ReadFile(filepath.Join(projectDir, "scrapy-go.toml"))
	if err != nil {
		t.Fatalf("读取 scrapy-go.toml 失败: %v", err)
	}
	toml := string(tomlContent)
	if !strings.Contains(toml, "e2e_project") {
		t.Errorf("scrapy-go.toml 应包含项目名称")
	}
	if !strings.Contains(toml, "concurrent_requests") {
		t.Errorf("scrapy-go.toml 应包含并发配置项")
	}

	// 验证 project/pipelines.go 内容
	pipeContent, err := os.ReadFile(filepath.Join(projectDir, "project", "pipelines.go"))
	if err != nil {
		t.Fatalf("读取 project/pipelines.go 失败: %v", err)
	}
	pipe := string(pipeContent)
	if !strings.Contains(pipe, "E2eProjectPipeline") {
		t.Errorf("pipelines.go 应包含 'E2eProjectPipeline'，实际内容:\n%s", pipe)
	}

	// 验证 project/items.go 内容
	itemsContent, err := os.ReadFile(filepath.Join(projectDir, "project", "items.go"))
	if err != nil {
		t.Fatalf("读取 project/items.go 失败: %v", err)
	}
	items := string(itemsContent)
	if !strings.Contains(items, "ExampleItem") {
		t.Errorf("items.go 应包含 'ExampleItem'")
	}
}

func TestGenSpider_EndToEnd_WithFullURL(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// 创建 scrapy-go.toml 标记为项目目录
	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)

	err := runGenSpider([]string{"books", "https://books.toscrape.com/catalogue/"})
	if err != nil {
		t.Fatalf("runGenSpider 失败: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "spiders", "books.go"))
	if err != nil {
		t.Fatalf("读取生成的文件失败: %v", err)
	}

	code := string(content)
	if !strings.Contains(code, "BooksSpider") {
		t.Errorf("生成的代码应包含 'BooksSpider'")
	}
	if !strings.Contains(code, "https://books.toscrape.com/catalogue/") {
		t.Errorf("生成的代码应保留完整 URL")
	}
}

// ============================================================================
// 覆盖率补充测试
// ============================================================================

func TestPrintUsage(t *testing.T) {
	// 验证 printUsage 不会 panic
	printUsage()
}

func TestPrintStartProjectUsage(t *testing.T) {
	// 验证 printStartProjectUsage 不会 panic
	printStartProjectUsage()
}

func TestPrintGenSpiderUsage(t *testing.T) {
	// 验证 printGenSpiderUsage 不会 panic
	printGenSpiderUsage()
}

func TestPrintVersionUsage(t *testing.T) {
	// 验证 printVersionUsage 不会 panic
	printVersionUsage()
}

func TestRunVersion_Help(t *testing.T) {
	// 验证 -h 选项
	runVersion([]string{"-h"})
	runVersion([]string{"--help"})
}

func TestRunStartProject_Help(t *testing.T) {
	err := runStartProject([]string{"-h"})
	if err != nil {
		t.Errorf("help 不应返回错误: %v", err)
	}
	err = runStartProject([]string{"--help"})
	if err != nil {
		t.Errorf("help 不应返回错误: %v", err)
	}
}

func TestRunGenSpider_Help(t *testing.T) {
	err := runGenSpider([]string{"-h"})
	if err != nil {
		t.Errorf("help 不应返回错误: %v", err)
	}
	err = runGenSpider([]string{"--help"})
	if err != nil {
		t.Errorf("help 不应返回错误: %v", err)
	}
}

func TestRunGenSpider_TemplateOptionMissing(t *testing.T) {
	err := runGenSpider([]string{"-t"})
	if err == nil {
		t.Error("期望 -t 无参数时返回错误")
	}
}

func TestRenderTemplate_InvalidPath(t *testing.T) {
	data := templateData{
		ProjectName: "test",
		ModulePath:  "test",
		ClassName:   "Test",
		Version:     "0.5.0",
	}

	// 测试写入不存在的目录
	err := renderTemplate("templates/project/main.go.tmpl", "/nonexistent/dir/file.go", data)
	if err == nil {
		t.Error("期望写入不存在的目录时返回错误")
	}
}

func TestRenderTemplate_InvalidTemplate(t *testing.T) {
	data := templateData{
		ProjectName: "test",
		ModulePath:  "test",
		ClassName:   "Test",
		Version:     "0.5.0",
	}

	// 测试读取不存在的模板
	err := renderTemplate("templates/project/nonexistent.tmpl", "/tmp/out.go", data)
	if err == nil {
		t.Error("期望读取不存在的模板时返回错误")
	}
}

func TestRenderSpiderTemplate_InvalidPath(t *testing.T) {
	data := spiderTemplateData{
		Name:      "test",
		ClassName: "TestSpider",
		Domain:    "example.com",
		URL:       "https://example.com",
		Module:    "test",
	}

	// 测试写入不存在的目录
	err := renderSpiderTemplate("templates/spiders/project/basic.go.tmpl", "/nonexistent/dir/file.go", data)
	if err == nil {
		t.Error("期望写入不存在的目录时返回错误")
	}
}

func TestRenderSpiderTemplate_InvalidTemplate(t *testing.T) {
	data := spiderTemplateData{
		Name:      "test",
		ClassName: "TestSpider",
		Domain:    "example.com",
		URL:       "https://example.com",
		Module:    "test",
	}

	// 测试读取不存在的模板
	err := renderSpiderTemplate("templates/spiders/nonexistent.go.tmpl", "/tmp/out.go", data)
	if err == nil {
		t.Error("期望读取不存在的模板时返回错误")
	}
}

func TestRunStartProject_TooManyArgs(t *testing.T) {
	err := runStartProject([]string{"a", "b", "c"})
	if err == nil {
		t.Error("期望参数过多时返回错误")
	}
}

func TestRunGenSpider_DashInName(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	// 带连字符的名称应该被拒绝（不是有效的 Go 标识符）
	err := runGenSpider([]string{"my-spider", "example.com"})
	if err == nil {
		t.Error("期望带连字符的名称返回错误")
	}
}

func TestToCamelCase_EmptyParts(t *testing.T) {
	// 测试连续下划线
	got := toCamelCase("a__b")
	if got != "AB" {
		t.Errorf("toCamelCase(\"a__b\") = %q, want \"AB\"", got)
	}
}

func TestToSpiderClassName_AlreadyHasSpider(t *testing.T) {
	// 如果名称已经以 spider 结尾，不应重复添加
	got := toSpiderClassName("my_spider")
	if got != "MySpider" {
		t.Errorf("toSpiderClassName(\"my_spider\") = %q, want \"MySpider\"", got)
	}
}

// ============================================================================
// genspider 项目内检测测试
// ============================================================================

func TestRunGenSpider_InProject(t *testing.T) {
	// 模拟在 scrapy-go 项目中执行 genspider
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// 创建 scrapy-go.toml 标记为项目目录
	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)
	os.Chdir(dir)

	err := runGenSpider([]string{"quotes", "quotes.toscrape.com"})
	if err != nil {
		t.Fatalf("runGenSpider 失败: %v", err)
	}

	// 验证文件生成到 spiders/ 目录
	spiderFile := filepath.Join(dir, "spiders", "quotes.go")
	if _, err := os.Stat(spiderFile); os.IsNotExist(err) {
		t.Fatalf("期望爬虫文件生成到 spiders/quotes.go，但未找到")
	}

	// 验证内容使用 package spiders
	content, err := os.ReadFile(spiderFile)
	if err != nil {
		t.Fatalf("读取生成的文件失败: %v", err)
	}

	code := string(content)
	if !strings.Contains(code, "package spiders") {
		t.Errorf("项目内生成的爬虫应使用 'package spiders'，实际内容:\n%s", code)
	}
	if strings.Contains(code, "func main()") {
		t.Errorf("项目内生成的爬虫不应包含 'func main()'")
	}
	if !strings.Contains(code, "QuotesSpider") {
		t.Errorf("生成的代码应包含 'QuotesSpider'")
	}
	if !strings.Contains(code, "https://quotes.toscrape.com") {
		t.Errorf("生成的代码应包含完整 URL")
	}
}

func TestRunGenSpider_InProject_CrawlTemplate(t *testing.T) {
	// 模拟在 scrapy-go 项目中使用 crawl 模板
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	// 创建 scrapy-go.toml 标记为项目目录
	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)
	os.Chdir(dir)

	err := runGenSpider([]string{"-t", "crawl", "articles", "blog.example.com"})
	if err != nil {
		t.Fatalf("runGenSpider 失败: %v", err)
	}

	// 验证文件生成到 spiders/ 目录
	spiderFile := filepath.Join(dir, "spiders", "articles.go")
	content, err := os.ReadFile(spiderFile)
	if err != nil {
		t.Fatalf("读取生成的文件失败: %v", err)
	}

	code := string(content)
	if !strings.Contains(code, "package spiders") {
		t.Errorf("项目内生成的爬虫应使用 'package spiders'")
	}
	if strings.Contains(code, "func main()") {
		t.Errorf("项目内生成的爬虫不应包含 'func main()'")
	}
	if !strings.Contains(code, "spider.CrawlSpider") {
		t.Errorf("生成的代码应包含 'spider.CrawlSpider'")
	}
	if !strings.Contains(code, "ArticlesSpider") {
		t.Errorf("生成的代码应包含 'ArticlesSpider'")
	}
}

func TestRunGenSpider_InProject_FileExists(t *testing.T) {
	// 在项目中，文件已存在时应报错
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)
	os.MkdirAll(filepath.Join(dir, "spiders"), 0o755)
	os.WriteFile(filepath.Join(dir, "spiders", "existing.go"), []byte("package spiders"), 0o644)
	os.Chdir(dir)

	err := runGenSpider([]string{"existing", "example.com"})
	if err == nil {
		t.Error("期望文件已存在时返回错误")
	}
}

func TestRunGenSpider_NotInProject(t *testing.T) {
	// 不在项目中时，应直接报错
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	err := runGenSpider([]string{"standalone", "example.com"})
	if err == nil {
		t.Fatal("期望不在项目中时返回错误")
	}
	if !strings.Contains(err.Error(), "scrapy-go.toml") {
		t.Errorf("错误信息应提及 scrapy-go.toml，实际: %v", err)
	}
}

func TestIsInProject(t *testing.T) {
	// 无 scrapy-go.toml 时不在项目中
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(dir)

	if isInProject() {
		t.Error("无 scrapy-go.toml 时不应检测为项目")
	}

	// 有 scrapy-go.toml 时在项目中
	os.WriteFile(filepath.Join(dir, "scrapy-go.toml"), []byte("# test"), 0o644)
	if !isInProject() {
		t.Error("有 scrapy-go.toml 时应检测为项目")
	}
}

func TestStartProject_SettingsContent(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myapp")

	err := runStartProject([]string{"myapp", projectDir})
	if err != nil {
		t.Fatalf("runStartProject 失败: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(projectDir, "project", "settings.go"))
	if err != nil {
		t.Fatalf("读取 project/settings.go 失败: %v", err)
	}

	settingsCode := string(content)
	// 验证使用项目级配置系统
	if !strings.Contains(settingsCode, "settings.New()") {
		t.Errorf("settings.go 应使用 settings.New() 创建配置实例")
	}
	if !strings.Contains(settingsCode, "settings.PriorityProject") {
		t.Errorf("settings.go 应使用 settings.PriorityProject 优先级")
	}
	if !strings.Contains(settingsCode, `"BOT_NAME"`) {
		t.Errorf("settings.go 应包含 BOT_NAME 配置")
	}
	if !strings.Contains(settingsCode, `"myapp"`) {
		t.Errorf("settings.go 应包含项目名称 'myapp'")
	}
	if !strings.Contains(settingsCode, "*settings.Settings") {
		t.Errorf("settings.go 应返回 *settings.Settings 类型")
	}
}
