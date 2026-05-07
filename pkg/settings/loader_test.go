package settings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFromFile_Basic(t *testing.T) {
	// 创建临时 TOML 配置文件
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.toml")

	content := `
# 基础配置
bot_name = "mybot"
concurrent_requests = 32
download_timeout = 60
retry_enabled = true
download_delay = 1.5
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	count, err := s.LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("加载配置文件失败: %v", err)
	}

	if count != 5 {
		t.Errorf("expected 5 config items loaded, got %d", count)
	}

	// 验证值已正确加载
	if s.GetString("BOT_NAME", "") != "mybot" {
		t.Errorf("BOT_NAME should be 'mybot', got %q", s.GetString("BOT_NAME", ""))
	}
	if s.GetInt("CONCURRENT_REQUESTS", 0) != 32 {
		t.Errorf("CONCURRENT_REQUESTS should be 32, got %d", s.GetInt("CONCURRENT_REQUESTS", 0))
	}
	if s.GetInt("DOWNLOAD_TIMEOUT", 0) != 60 {
		t.Errorf("DOWNLOAD_TIMEOUT should be 60, got %d", s.GetInt("DOWNLOAD_TIMEOUT", 0))
	}
	if !s.GetBool("RETRY_ENABLED", false) {
		t.Error("RETRY_ENABLED should be true")
	}
	if s.GetFloat("DOWNLOAD_DELAY", 0) != 1.5 {
		t.Errorf("DOWNLOAD_DELAY should be 1.5, got %f", s.GetFloat("DOWNLOAD_DELAY", 0))
	}
}

func TestLoadFromFile_Priority(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.toml")

	content := `concurrent_requests = 32`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()

	// TOML 配置以 PriorityAddon(15) 加载
	if _, err := s.LoadFromFile(configPath); err != nil {
		t.Fatalf("加载配置文件失败: %v", err)
	}

	// 验证 TOML 配置覆盖了默认值
	if s.GetInt("CONCURRENT_REQUESTS", 0) != 32 {
		t.Errorf("TOML should override default: got %d", s.GetInt("CONCURRENT_REQUESTS", 0))
	}

	// 验证优先级
	if s.GetPriority("CONCURRENT_REQUESTS") != PriorityAddon {
		t.Errorf("priority should be PriorityAddon(15), got %d", s.GetPriority("CONCURRENT_REQUESTS"))
	}

	// Project 级别的配置应该覆盖 TOML 配置
	s.Set("CONCURRENT_REQUESTS", 64, PriorityProject)
	if s.GetInt("CONCURRENT_REQUESTS", 0) != 64 {
		t.Errorf("Project priority should override TOML: got %d", s.GetInt("CONCURRENT_REQUESTS", 0))
	}
}

func TestLoadFromFile_IntSlice(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.toml")

	content := `retry_http_codes = [500, 502, 503, 504, 429]`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	if _, err := s.LoadFromFile(configPath); err != nil {
		t.Fatalf("加载配置文件失败: %v", err)
	}

	v := s.Get("RETRY_HTTP_CODES", nil)
	codes, ok := v.([]int)
	if !ok {
		t.Fatalf("RETRY_HTTP_CODES should be []int, got %T", v)
	}
	if len(codes) != 5 {
		t.Errorf("expected 5 codes, got %d", len(codes))
	}
	if codes[0] != 500 {
		t.Errorf("first code should be 500, got %d", codes[0])
	}
}

func TestLoadFromFile_StringSlice(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.toml")

	content := `httpcache_ignore_schemes = ["file", "ftp"]`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	if _, err := s.LoadFromFile(configPath); err != nil {
		t.Fatalf("加载配置文件失败: %v", err)
	}

	v := s.Get("HTTPCACHE_IGNORE_SCHEMES", nil)
	schemes, ok := v.([]string)
	if !ok {
		t.Fatalf("HTTPCACHE_IGNORE_SCHEMES should be []string, got %T", v)
	}
	if len(schemes) != 2 {
		t.Errorf("expected 2 schemes, got %d", len(schemes))
	}
	if schemes[0] != "file" || schemes[1] != "ftp" {
		t.Errorf("unexpected schemes: %v", schemes)
	}
}

func TestLoadFromFile_Duration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.toml")

	content := `graceful_shutdown_timeout = "30s"`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	if _, err := s.LoadFromFile(configPath); err != nil {
		t.Fatalf("加载配置文件失败: %v", err)
	}

	v := s.Get("GRACEFUL_SHUTDOWN_TIMEOUT", nil)
	d, ok := v.(time.Duration)
	if !ok {
		t.Fatalf("GRACEFUL_SHUTDOWN_TIMEOUT should be time.Duration, got %T", v)
	}
	if d != 30*time.Second {
		t.Errorf("expected 30s, got %v", d)
	}
}

func TestLoadFromFile_Map(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.toml")

	content := `
[default_request_headers]
Accept = "text/html"
Accept-Language = "zh-CN"
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	if _, err := s.LoadFromFile(configPath); err != nil {
		t.Fatalf("加载配置文件失败: %v", err)
	}

	v := s.Get("DEFAULT_REQUEST_HEADERS", nil)
	headers, ok := v.(map[string]string)
	if !ok {
		t.Fatalf("DEFAULT_REQUEST_HEADERS should be map[string]string, got %T", v)
	}
	if headers["Accept"] != "text/html" {
		t.Errorf("Accept should be 'text/html', got %q", headers["Accept"])
	}
	if headers["Accept-Language"] != "zh-CN" {
		t.Errorf("Accept-Language should be 'zh-CN', got %q", headers["Accept-Language"])
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	s := New()
	_, err := s.LoadFromFile("/nonexistent/path/config.toml")
	if err == nil {
		t.Error("should return error for non-existent file")
	}
}

func TestLoadFromFile_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "invalid.toml")

	content := `this is not valid toml [[[`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	_, err := s.LoadFromFile(configPath)
	if err == nil {
		t.Error("should return error for invalid TOML")
	}
}

func TestLoadFromFile_Frozen(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.toml")

	content := `bot_name = "mybot"`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	s.Freeze()

	_, err := s.LoadFromFile(configPath)
	if err == nil {
		t.Error("should return error when settings is frozen")
	}
}

func TestLoadFromFileIfExists_Exists(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.toml")

	content := `bot_name = "mybot"`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	loaded, err := s.LoadFromFileIfExists(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !loaded {
		t.Error("should return true when file exists")
	}
	if s.GetString("BOT_NAME", "") != "mybot" {
		t.Error("config should be loaded")
	}
}

func TestLoadFromFileIfExists_NotExists(t *testing.T) {
	s := New()
	loaded, err := s.LoadFromFileIfExists("/nonexistent/config.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded {
		t.Error("should return false when file does not exist")
	}
}

func TestAutoLoadConfig_EnvVar(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "custom.toml")

	content := `bot_name = "envbot"`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	// 设置环境变量
	t.Setenv(ConfigEnvVar, configPath)

	s := New()
	path, err := s.AutoLoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != configPath {
		t.Errorf("expected path %q, got %q", configPath, path)
	}
	if s.GetString("BOT_NAME", "") != "envbot" {
		t.Error("config should be loaded from env var path")
	}
}

func TestAutoLoadConfig_EnvVar_NotFound(t *testing.T) {
	t.Setenv(ConfigEnvVar, "/nonexistent/config.toml")

	s := New()
	_, err := s.AutoLoadConfig()
	if err == nil {
		t.Error("should return error when env var points to non-existent file")
	}
}

func TestAutoLoadConfig_DefaultFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, DefaultConfigFileName)

	content := `bot_name = "defaultbot"`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	// 切换工作目录
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// 确保没有设置环境变量
	t.Setenv(ConfigEnvVar, "")

	s := New()
	path, err := s.AutoLoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != DefaultConfigFileName {
		t.Errorf("expected path %q, got %q", DefaultConfigFileName, path)
	}
	if s.GetString("BOT_NAME", "") != "defaultbot" {
		t.Error("config should be loaded from default file")
	}
}

func TestAutoLoadConfig_NoFile(t *testing.T) {
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	t.Setenv(ConfigEnvVar, "")

	s := New()
	path, err := s.AutoLoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

func TestToSettingsKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"concurrent_requests", "CONCURRENT_REQUESTS"},
		{"bot_name", "BOT_NAME"},
		{"log_level", "LOG_LEVEL"},
		{"ALREADY_UPPER", "ALREADY_UPPER"},
	}

	for _, tt := range tests {
		result := toSettingsKey(tt.input)
		if result != tt.expected {
			t.Errorf("toSettingsKey(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestConvertTOMLValue_Types(t *testing.T) {
	// int64 → int
	if v := convertTOMLValue(int64(42)); v != 42 {
		t.Errorf("int64 conversion failed: got %v (%T)", v, v)
	}

	// float64 保持不变
	if v := convertTOMLValue(float64(3.14)); v != 3.14 {
		t.Errorf("float64 conversion failed: got %v", v)
	}

	// bool 保持不变
	if v := convertTOMLValue(true); v != true {
		t.Errorf("bool conversion failed: got %v", v)
	}

	// string 保持不变（非 duration）
	if v := convertTOMLValue("hello"); v != "hello" {
		t.Errorf("string conversion failed: got %v", v)
	}

	// duration string → time.Duration
	if v := convertTOMLValue("5s"); v != 5*time.Second {
		t.Errorf("duration conversion failed: got %v (%T)", v, v)
	}

	// 非 duration 的字符串保持不变
	if v := convertTOMLValue("not-a-duration"); v != "not-a-duration" {
		t.Errorf("non-duration string should stay as string: got %v", v)
	}
}

func TestConvertTOMLSlice(t *testing.T) {
	// []int64 → []int
	intSlice := convertTOMLSlice([]any{int64(1), int64(2), int64(3)})
	if ints, ok := intSlice.([]int); !ok || len(ints) != 3 || ints[0] != 1 {
		t.Errorf("int slice conversion failed: got %v (%T)", intSlice, intSlice)
	}

	// []string
	strSlice := convertTOMLSlice([]any{"a", "b", "c"})
	if strs, ok := strSlice.([]string); !ok || len(strs) != 3 || strs[0] != "a" {
		t.Errorf("string slice conversion failed: got %v (%T)", strSlice, strSlice)
	}

	// 混合类型保持 []any
	mixedSlice := convertTOMLSlice([]any{int64(1), "two", true})
	if _, ok := mixedSlice.([]any); !ok {
		t.Errorf("mixed slice should stay as []any: got %T", mixedSlice)
	}

	// 空切片
	emptySlice := convertTOMLSlice([]any{})
	if s, ok := emptySlice.([]any); !ok || len(s) != 0 {
		t.Errorf("empty slice conversion failed: got %v (%T)", emptySlice, emptySlice)
	}
}

func TestConvertTOMLMap(t *testing.T) {
	// 全 string map → map[string]string
	strMap := convertTOMLMap(map[string]any{"key1": "val1", "key2": "val2"})
	if m, ok := strMap.(map[string]string); !ok || m["key1"] != "val1" {
		t.Errorf("string map conversion failed: got %v (%T)", strMap, strMap)
	}

	// 混合类型 map → map[string]any（值被递归转换）
	mixedMap := convertTOMLMap(map[string]any{"name": "test", "count": int64(5)})
	if m, ok := mixedMap.(map[string]any); !ok {
		t.Errorf("mixed map should be map[string]any: got %T", mixedMap)
	} else {
		if m["count"] != 5 {
			t.Errorf("int64 in map should be converted to int: got %v (%T)", m["count"], m["count"])
		}
	}

	// 空 map
	emptyMap := convertTOMLMap(map[string]any{})
	if m, ok := emptyMap.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("empty map conversion failed: got %v (%T)", emptyMap, emptyMap)
	}
}

func TestLoadFromFile_CompleteConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "complete.toml")

	content := `
# 完整配置测试
bot_name = "testbot"
user_agent = "testbot/1.0"
concurrent_requests = 8
concurrent_requests_per_domain = 4
download_delay = 2
download_timeout = 60
retry_enabled = true
retry_times = 3
retry_http_codes = [500, 502, 503]
redirect_enabled = true
redirect_max_times = 10
httpcache_enabled = true
httpcache_dir = "/tmp/cache"
robotstxt_obey = false
log_level = "INFO"
depth_limit = 5
httpcache_ignore_schemes = ["file", "ftp"]
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	count, err := s.LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("加载配置文件失败: %v", err)
	}

	if count != 17 {
		t.Errorf("expected 17 config items, got %d", count)
	}

	// 验证各种类型
	if s.GetString("BOT_NAME", "") != "testbot" {
		t.Error("BOT_NAME mismatch")
	}
	if s.GetInt("CONCURRENT_REQUESTS", 0) != 8 {
		t.Error("CONCURRENT_REQUESTS mismatch")
	}
	if s.GetInt("DOWNLOAD_DELAY", -1) != 2 {
		t.Error("DOWNLOAD_DELAY mismatch")
	}
	if s.GetBool("HTTPCACHE_ENABLED", false) != true {
		t.Error("HTTPCACHE_ENABLED mismatch")
	}
	if s.GetBool("ROBOTSTXT_OBEY", true) != false {
		t.Error("ROBOTSTXT_OBEY mismatch")
	}
	if s.GetString("LOG_LEVEL", "") != "INFO" {
		t.Error("LOG_LEVEL mismatch")
	}
	if s.GetInt("DEPTH_LIMIT", 0) != 5 {
		t.Error("DEPTH_LIMIT mismatch")
	}

	// 验证 []int
	v := s.Get("RETRY_HTTP_CODES", nil)
	codes, ok := v.([]int)
	if !ok || len(codes) != 3 {
		t.Errorf("RETRY_HTTP_CODES should be []int with 3 items, got %T %v", v, v)
	}

	// 验证 []string
	v2 := s.Get("HTTPCACHE_IGNORE_SCHEMES", nil)
	schemes, ok := v2.([]string)
	if !ok || len(schemes) != 2 {
		t.Errorf("HTTPCACHE_IGNORE_SCHEMES should be []string with 2 items, got %T %v", v2, v2)
	}
}

func TestLoadFromFile_DoesNotOverrideHigherPriority(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.toml")

	content := `concurrent_requests = 32`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	s := New()
	// 先设置 Project 级别的值
	s.Set("CONCURRENT_REQUESTS", 64, PriorityProject)

	// TOML 加载（PriorityAddon = 15）不应覆盖 Project（20）
	if _, err := s.LoadFromFile(configPath); err != nil {
		t.Fatalf("加载配置文件失败: %v", err)
	}

	if s.GetInt("CONCURRENT_REQUESTS", 0) != 64 {
		t.Errorf("TOML should not override higher priority: got %d", s.GetInt("CONCURRENT_REQUESTS", 0))
	}
}
