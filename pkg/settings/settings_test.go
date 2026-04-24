package settings

import (
	"net/http"
	"testing"
	"time"
)

func TestNewSettings(t *testing.T) {
	s := New()

	// 验证默认值已加载
	if s.GetInt("CONCURRENT_REQUESTS", 0) != 16 {
		t.Errorf("unexpected CONCURRENT_REQUESTS: %d", s.GetInt("CONCURRENT_REQUESTS", 0))
	}
	if s.GetInt("CONCURRENT_REQUESTS_PER_DOMAIN", 0) != 8 {
		t.Errorf("unexpected CONCURRENT_REQUESTS_PER_DOMAIN: %d", s.GetInt("CONCURRENT_REQUESTS_PER_DOMAIN", 0))
	}
	if s.GetInt("DOWNLOAD_TIMEOUT", 0) != 180 {
		t.Errorf("unexpected DOWNLOAD_TIMEOUT: %d", s.GetInt("DOWNLOAD_TIMEOUT", 0))
	}
	if !s.GetBool("RETRY_ENABLED", false) {
		t.Error("RETRY_ENABLED should be true by default")
	}
	if s.GetInt("RETRY_TIMES", 0) != 2 {
		t.Errorf("unexpected RETRY_TIMES: %d", s.GetInt("RETRY_TIMES", 0))
	}
}

func TestNewEmptySettings(t *testing.T) {
	s := NewEmpty()

	// 空配置不应有默认值
	if s.Has("CONCURRENT_REQUESTS") {
		t.Error("empty settings should not have CONCURRENT_REQUESTS")
	}
}

func TestSetAndGet(t *testing.T) {
	s := NewEmpty()

	s.Set("KEY1", "value1", PriorityDefault)
	if s.GetString("KEY1", "") != "value1" {
		t.Errorf("unexpected value: %s", s.GetString("KEY1", ""))
	}

	// 高优先级覆盖低优先级
	s.Set("KEY1", "value2", PriorityProject)
	if s.GetString("KEY1", "") != "value2" {
		t.Errorf("unexpected value: %s", s.GetString("KEY1", ""))
	}

	// 低优先级不覆盖高优先级
	s.Set("KEY1", "value3", PriorityDefault)
	if s.GetString("KEY1", "") != "value2" {
		t.Errorf("value should not be overridden by lower priority: %s", s.GetString("KEY1", ""))
	}
}

func TestGetWithDefault(t *testing.T) {
	s := NewEmpty()

	// 不存在的 key 返回默认值
	if s.GetString("MISSING", "default") != "default" {
		t.Error("should return default for missing key")
	}
	if s.GetInt("MISSING", 42) != 42 {
		t.Error("should return default for missing key")
	}
	if s.GetBool("MISSING", true) != true {
		t.Error("should return default for missing key")
	}
	if s.GetFloat("MISSING", 3.14) != 3.14 {
		t.Error("should return default for missing key")
	}
}

func TestGetBool(t *testing.T) {
	s := NewEmpty()

	tests := []struct {
		value    any
		expected bool
	}{
		{true, true},
		{false, false},
		{1, true},
		{0, false},
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
		{"yes", true},
		{"no", false},
	}

	for i, tt := range tests {
		s.Set("KEY", tt.value, PriorityCmdline)
		if s.GetBool("KEY", false) != tt.expected {
			t.Errorf("test %d: expected %v for value %v", i, tt.expected, tt.value)
		}
	}
}

func TestGetInt(t *testing.T) {
	s := NewEmpty()

	s.Set("INT", 42, PriorityDefault)
	if s.GetInt("INT", 0) != 42 {
		t.Error("unexpected int value")
	}

	s.Set("FLOAT", 3.14, PriorityCmdline)
	if s.GetInt("FLOAT", 0) != 3 {
		t.Error("float should be truncated to int")
	}

	s.Set("STRING", "100", PriorityCmdline)
	if s.GetInt("STRING", 0) != 100 {
		t.Error("string should be parsed to int")
	}
}

func TestGetFloat(t *testing.T) {
	s := NewEmpty()

	s.Set("FLOAT", 3.14, PriorityDefault)
	if s.GetFloat("FLOAT", 0) != 3.14 {
		t.Error("unexpected float value")
	}

	s.Set("INT", 42, PriorityCmdline)
	if s.GetFloat("INT", 0) != 42.0 {
		t.Error("int should be converted to float")
	}
}

func TestGetDuration(t *testing.T) {
	s := NewEmpty()

	// time.Duration 类型
	s.Set("DUR1", 5*time.Second, PriorityCmdline)
	if s.GetDuration("DUR1", 0) != 5*time.Second {
		t.Error("unexpected duration")
	}

	// int 类型（秒）
	s.Set("DUR2", 10, PriorityCmdline)
	if s.GetDuration("DUR2", 0) != 10*time.Second {
		t.Error("int should be treated as seconds")
	}

	// float64 类型（秒）
	s.Set("DUR3", 1.5, PriorityCmdline)
	if s.GetDuration("DUR3", 0) != 1500*time.Millisecond {
		t.Error("float should be treated as seconds")
	}

	// string 类型
	s.Set("DUR4", "2s", PriorityCmdline)
	if s.GetDuration("DUR4", 0) != 2*time.Second {
		t.Error("string should be parsed as duration")
	}
}

func TestGetStringSlice(t *testing.T) {
	s := NewEmpty()

	// []string 类型
	s.Set("SLICE1", []string{"a", "b", "c"}, PriorityCmdline)
	result := s.GetStringSlice("SLICE1", nil)
	if len(result) != 3 || result[0] != "a" {
		t.Errorf("unexpected slice: %v", result)
	}

	// string 类型（逗号分割）
	s.Set("SLICE2", "x,y,z", PriorityCmdline)
	result2 := s.GetStringSlice("SLICE2", nil)
	if len(result2) != 3 || result2[0] != "x" {
		t.Errorf("unexpected slice: %v", result2)
	}

	// 空字符串
	s.Set("SLICE3", "", PriorityCmdline)
	result3 := s.GetStringSlice("SLICE3", nil)
	if len(result3) != 0 {
		t.Errorf("empty string should return empty slice: %v", result3)
	}
}

func TestGetStringMap(t *testing.T) {
	s := NewEmpty()

	// map[string]any 类型
	s.Set("MAP1", map[string]any{"key": "value"}, PriorityCmdline)
	result := s.GetStringMap("MAP1", nil)
	if result["key"] != "value" {
		t.Errorf("unexpected map: %v", result)
	}

	// JSON string 类型
	s.Set("MAP2", `{"key":"value"}`, PriorityCmdline)
	result2 := s.GetStringMap("MAP2", nil)
	if result2["key"] != "value" {
		t.Errorf("unexpected map: %v", result2)
	}
}

func TestFreeze(t *testing.T) {
	s := NewEmpty()
	s.Set("KEY", "value", PriorityDefault)

	s.Freeze()

	if !s.IsFrozen() {
		t.Error("should be frozen")
	}

	// 冻结后不能修改
	err := s.Set("KEY", "new_value", PriorityCmdline)
	if err == nil {
		t.Error("should return error when modifying frozen settings")
	}

	// 冻结后仍可读取
	if s.GetString("KEY", "") != "value" {
		t.Error("should still be able to read frozen settings")
	}
}

func TestCopy(t *testing.T) {
	s := NewEmpty()
	s.Set("KEY", "value", PriorityProject)

	copied := s.Copy()

	// 修改拷贝不影响原始
	copied.Set("KEY", "modified", PriorityCmdline)
	if s.GetString("KEY", "") != "value" {
		t.Error("modifying copy should not affect original")
	}

	// 拷贝不继承冻结状态
	s.Freeze()
	copied2 := s.Copy()
	if copied2.IsFrozen() {
		t.Error("copy should not inherit frozen state")
	}
}

func TestFrozenCopy(t *testing.T) {
	s := NewEmpty()
	s.Set("KEY", "value", PriorityDefault)

	fc := s.FrozenCopy()
	if !fc.IsFrozen() {
		t.Error("frozen copy should be frozen")
	}
	if fc.GetString("KEY", "") != "value" {
		t.Error("frozen copy should have same values")
	}
}

func TestUpdate(t *testing.T) {
	s := NewEmpty()

	err := s.Update(map[string]any{
		"KEY1": "value1",
		"KEY2": 42,
		"KEY3": true,
	}, PriorityProject)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.GetString("KEY1", "") != "value1" {
		t.Error("KEY1 should be set")
	}
	if s.GetInt("KEY2", 0) != 42 {
		t.Error("KEY2 should be set")
	}
	if !s.GetBool("KEY3", false) {
		t.Error("KEY3 should be set")
	}
}

func TestGetPriority(t *testing.T) {
	s := NewEmpty()

	// 不存在的 key
	if s.GetPriority("MISSING") != -1 {
		t.Error("should return -1 for missing key")
	}

	s.Set("KEY", "value", PriorityProject)
	if s.GetPriority("KEY") != PriorityProject {
		t.Errorf("unexpected priority: %d", s.GetPriority("KEY"))
	}
}

func TestHas(t *testing.T) {
	s := NewEmpty()

	if s.Has("KEY") {
		t.Error("should not have KEY")
	}

	s.Set("KEY", "value", PriorityDefault)
	if !s.Has("KEY") {
		t.Error("should have KEY")
	}
}

func TestDelete(t *testing.T) {
	s := NewEmpty()
	s.Set("KEY", "value", PriorityProject)

	// 低优先级不能删除
	s.Delete("KEY", PriorityDefault)
	if !s.Has("KEY") {
		t.Error("should not be deleted by lower priority")
	}

	// 高优先级可以删除
	s.Delete("KEY", PriorityCmdline)
	if s.Has("KEY") {
		t.Error("should be deleted by higher priority")
	}
}

func TestGetComponentPriorityDictWithBase(t *testing.T) {
	s := NewEmpty()

	s.Set("DOWNLOADER_MIDDLEWARES_BASE", map[string]int{
		"DefaultHeaders": 400,
		"UserAgent":      500,
		"Retry":          550,
	}, PriorityDefault)

	s.Set("DOWNLOADER_MIDDLEWARES", map[string]int{
		"CustomMW": 300,
	}, PriorityProject)

	result := s.GetComponentPriorityDictWithBase("DOWNLOADER_MIDDLEWARES")

	if len(result) != 4 {
		t.Errorf("expected 4 entries, got %d", len(result))
	}
	if result["DefaultHeaders"] != 400 {
		t.Error("DefaultHeaders should be 400")
	}
	if result["CustomMW"] != 300 {
		t.Error("CustomMW should be 300")
	}
}

func TestGetComponentPriorityDictWithBase_Disable(t *testing.T) {
	s := NewEmpty()

	s.Set("DOWNLOADER_MIDDLEWARES_BASE", map[string]int{
		"DefaultHeaders": 400,
		"UserAgent":      500,
		"Retry":          550,
		"Redirect":       600,
	}, PriorityDefault)

	// 通过设置负数优先级禁用 Retry 中间件
	s.Set("DOWNLOADER_MIDDLEWARES", map[string]int{
		"Retry": -1,
	}, PriorityProject)

	result := s.GetComponentPriorityDictWithBase("DOWNLOADER_MIDDLEWARES")

	if len(result) != 3 {
		t.Errorf("expected 3 entries (Retry disabled), got %d", len(result))
	}
	if _, ok := result["Retry"]; ok {
		t.Error("Retry should be disabled (removed from result)")
	}
	if result["DefaultHeaders"] != 400 {
		t.Error("DefaultHeaders should still be 400")
	}
}

func TestGetComponentPriorityDictWithBase_OverridePriority(t *testing.T) {
	s := NewEmpty()

	s.Set("DOWNLOADER_MIDDLEWARES_BASE", map[string]int{
		"DefaultHeaders": 400,
		"UserAgent":      500,
	}, PriorityDefault)

	// 覆盖 UserAgent 的优先级
	s.Set("DOWNLOADER_MIDDLEWARES", map[string]int{
		"UserAgent": 100,
	}, PriorityProject)

	result := s.GetComponentPriorityDictWithBase("DOWNLOADER_MIDDLEWARES")

	if result["UserAgent"] != 100 {
		t.Errorf("UserAgent priority should be overridden to 100, got %d", result["UserAgent"])
	}
	if result["DefaultHeaders"] != 400 {
		t.Error("DefaultHeaders should still be 400")
	}
}

func TestDefaultRequestHeaders(t *testing.T) {
	s := New()

	v := s.Get("DEFAULT_REQUEST_HEADERS", nil)
	headers, ok := v.(http.Header)
	if !ok {
		t.Fatal("DEFAULT_REQUEST_HEADERS should be http.Header")
	}
	if headers.Get("Accept") == "" {
		t.Error("Accept header should be set")
	}
	if headers.Get("Accept-Language") != "en" {
		t.Error("Accept-Language should be 'en'")
	}
}

func TestKeys(t *testing.T) {
	s := NewEmpty()
	s.Set("A", 1, PriorityDefault)
	s.Set("B", 2, PriorityDefault)
	s.Set("C", 3, PriorityDefault)

	keys := s.Keys()
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
}

func TestToMap(t *testing.T) {
	s := NewEmpty()
	s.Set("A", 1, PriorityDefault)
	s.Set("B", "hello", PriorityDefault)

	m := s.ToMap()
	if m["A"] != 1 {
		t.Error("A should be 1")
	}
	if m["B"] != "hello" {
		t.Error("B should be 'hello'")
	}
}
