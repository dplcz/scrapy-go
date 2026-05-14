package settings

import (
	"net/http"
	"testing"
	"time"
)

// ============================================================================
// 泛型 API 测试（TD-004）
// ============================================================================

func TestGenericGet_Int(t *testing.T) {
	s := New()

	// 使用类型化键获取默认值
	val := Get(s, KeyConcurrentRequests)
	if val != 16 {
		t.Errorf("expected 16, got %d", val)
	}

	// 设置后获取
	s.Set("CONCURRENT_REQUESTS", 32, PriorityProject)
	val = Get(s, KeyConcurrentRequests)
	if val != 32 {
		t.Errorf("expected 32, got %d", val)
	}
}

func TestGenericGet_String(t *testing.T) {
	s := New()

	val := Get(s, KeyBotName)
	if val != "scrapybot" {
		t.Errorf("expected 'scrapybot', got %q", val)
	}

	s.Set("BOT_NAME", "mybot", PriorityProject)
	val = Get(s, KeyBotName)
	if val != "mybot" {
		t.Errorf("expected 'mybot', got %q", val)
	}
}

func TestGenericGet_Bool(t *testing.T) {
	s := New()

	val := Get(s, KeyRetryEnabled)
	if !val {
		t.Error("expected true")
	}

	s.Set("RETRY_ENABLED", false, PriorityProject)
	val = Get(s, KeyRetryEnabled)
	if val {
		t.Error("expected false")
	}
}

func TestGenericGet_Float(t *testing.T) {
	s := New()

	val := Get(s, KeyRetryBackoffBaseDelay)
	if val != 1.0 {
		t.Errorf("expected 1.0, got %f", val)
	}

	s.Set("RETRY_BACKOFF_BASE_DELAY", 2.5, PriorityProject)
	val = Get(s, KeyRetryBackoffBaseDelay)
	if val != 2.5 {
		t.Errorf("expected 2.5, got %f", val)
	}
}

func TestGenericGet_Duration(t *testing.T) {
	s := New()

	val := Get(s, KeyDownloadTimeoutDuration)
	if val != 180*time.Second {
		t.Errorf("expected 180s, got %v", val)
	}

	// 设置为 int 类型（秒），应自动转换
	s.Set("DOWNLOAD_TIMEOUT", 30, PriorityCmdline)
	val = Get(s, KeyDownloadTimeoutDuration)
	if val != 30*time.Second {
		t.Errorf("expected 30s, got %v", val)
	}
}

func TestGenericGet_IntSlice(t *testing.T) {
	s := New()

	val := Get(s, KeyRetryHTTPCodes)
	if len(val) != 8 {
		t.Errorf("expected 8 codes, got %d", len(val))
	}
	if val[0] != 500 {
		t.Errorf("expected first code 500, got %d", val[0])
	}
}

func TestGenericGet_StringSlice(t *testing.T) {
	s := New()

	val := Get(s, KeyHTTPCacheIgnoreSchemes)
	if len(val) != 1 || val[0] != "file" {
		t.Errorf("expected [file], got %v", val)
	}
}

func TestGenericGet_IntMap(t *testing.T) {
	s := New()

	val := Get(s, KeyDownloaderMiddlewaresBase)
	if val["Retry"] != 550 {
		t.Errorf("expected Retry=550, got %d", val["Retry"])
	}
	if val["Cookies"] != 700 {
		t.Errorf("expected Cookies=700, got %d", val["Cookies"])
	}
}

func TestGenericGet_HTTPHeader(t *testing.T) {
	s := New()

	val := Get(s, KeyDefaultRequestHeaders)
	if val.Get("Accept-Language") != "en" {
		t.Errorf("expected Accept-Language=en, got %q", val.Get("Accept-Language"))
	}
}

func TestGenericGet_NotExists(t *testing.T) {
	s := NewEmpty()

	// 配置项不存在时返回 Key 中定义的默认值
	val := Get(s, KeyConcurrentRequests)
	if val != 16 {
		t.Errorf("expected default 16, got %d", val)
	}

	strVal := Get(s, KeyBotName)
	if strVal != "scrapybot" {
		t.Errorf("expected default 'scrapybot', got %q", strVal)
	}
}

func TestGenericGet_TypeConversion(t *testing.T) {
	s := NewEmpty()

	// int64 → int 转换
	s.Set("CONCURRENT_REQUESTS", int64(64), PriorityProject)
	val := Get(s, KeyConcurrentRequests)
	if val != 64 {
		t.Errorf("expected 64 (from int64), got %d", val)
	}

	// float64 → int 转换
	s.Set("CONCURRENT_REQUESTS", float64(128), PriorityCmdline)
	val = Get(s, KeyConcurrentRequests)
	if val != 128 {
		t.Errorf("expected 128 (from float64), got %d", val)
	}

	// string → bool 转换
	s.Set("RETRY_ENABLED", "true", PriorityCmdline)
	boolVal := Get(s, KeyRetryEnabled)
	if !boolVal {
		t.Error("expected true from string 'true'")
	}

	// int → bool 转换
	s.Set("RETRY_ENABLED", 0, PriorityCmdline)
	boolVal = Get(s, KeyRetryEnabled)
	if boolVal {
		t.Error("expected false from int 0")
	}
}

func TestMethodSet(t *testing.T) {
	s := NewEmpty()

	// 使用 Settings.Set 方法设置配置
	err := s.Set(KeyConcurrentRequests.Name, 32, PriorityProject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val := Get(s, KeyConcurrentRequests)
	if val != 32 {
		t.Errorf("expected 32, got %d", val)
	}

	// 使用 Settings.Set 方法设置 string 配置
	err = s.Set(KeyBotName.Name, "testbot", PriorityProject)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	strVal := Get(s, KeyBotName)
	if strVal != "testbot" {
		t.Errorf("expected 'testbot', got %q", strVal)
	}
}

func TestMethodSet_Frozen(t *testing.T) {
	s := NewEmpty()
	s.Freeze()

	err := s.Set(KeyConcurrentRequests.Name, 32, PriorityProject)
	if err == nil {
		t.Error("expected error when setting frozen settings")
	}
}

func TestMustGet_Exists(t *testing.T) {
	s := New()

	// 存在的配置项不应 panic
	val := MustGet(s, KeyConcurrentRequests)
	if val != 16 {
		t.Errorf("expected 16, got %d", val)
	}
}

func TestMustGet_NotExists(t *testing.T) {
	s := NewEmpty()

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for missing key")
		}
	}()

	// 不存在的配置项应 panic
	_ = MustGet(s, KeyConcurrentRequests)
}

func TestKeyString(t *testing.T) {
	if KeyConcurrentRequests.String() != "CONCURRENT_REQUESTS" {
		t.Errorf("expected 'CONCURRENT_REQUESTS', got %q", KeyConcurrentRequests.String())
	}
	if KeyBotName.String() != "BOT_NAME" {
		t.Errorf("expected 'BOT_NAME', got %q", KeyBotName.String())
	}
}

func TestGenericGet_MapIntInt(t *testing.T) {
	s := New()

	val := Get(s, KeyRetryPerStatusMaxTimes)
	if val == nil {
		t.Error("expected non-nil map")
	}
	if len(val) != 0 {
		t.Errorf("expected empty map, got %v", val)
	}

	// 设置并获取
	s.Set("RETRY_PER_STATUS_MAX_TIMES", map[int]int{429: 5, 503: 3}, PriorityProject)
	val = Get(s, KeyRetryPerStatusMaxTimes)
	if val[429] != 5 {
		t.Errorf("expected 429→5, got %d", val[429])
	}
	if val[503] != 3 {
		t.Errorf("expected 503→3, got %d", val[503])
	}
}

func TestGenericGet_Compatibility(t *testing.T) {
	s := New()

	// 验证泛型 API 与旧 API 返回相同结果
	oldVal := s.GetInt("CONCURRENT_REQUESTS", 0)
	newVal := Get(s, KeyConcurrentRequests)
	if oldVal != newVal {
		t.Errorf("old API (%d) != new API (%d)", oldVal, newVal)
	}

	oldStr := s.GetString("BOT_NAME", "")
	newStr := Get(s, KeyBotName)
	if oldStr != newStr {
		t.Errorf("old API (%q) != new API (%q)", oldStr, newStr)
	}

	oldBool := s.GetBool("RETRY_ENABLED", false)
	newBool := Get(s, KeyRetryEnabled)
	if oldBool != newBool {
		t.Errorf("old API (%v) != new API (%v)", oldBool, newBool)
	}
}

func TestGenericGet_HeaderType(t *testing.T) {
	s := NewEmpty()

	headers := http.Header{
		"X-Custom": {"test-value"},
	}
	s.Set("DEFAULT_REQUEST_HEADERS", headers, PriorityProject)

	val := Get(s, KeyDefaultRequestHeaders)
	if val.Get("X-Custom") != "test-value" {
		t.Errorf("expected 'test-value', got %q", val.Get("X-Custom"))
	}
}

// ============================================================================
// 类型转换路径覆盖测试
// ============================================================================

func TestGenericGet_Int64ToInt(t *testing.T) {
	s := NewEmpty()
	s.Set("CONCURRENT_REQUESTS", int64(48), PriorityProject)
	val := Get(s, KeyConcurrentRequests)
	if val != 48 {
		t.Errorf("expected 48 from int64, got %d", val)
	}
}

func TestGenericGet_Float32ToFloat64(t *testing.T) {
	s := NewEmpty()
	s.Set("RETRY_BACKOFF_BASE_DELAY", float32(2.5), PriorityProject)
	val := Get(s, KeyRetryBackoffBaseDelay)
	if val < 2.4 || val > 2.6 {
		t.Errorf("expected ~2.5 from float32, got %f", val)
	}
}

func TestGenericGet_IntToFloat64(t *testing.T) {
	s := NewEmpty()
	s.Set("RETRY_BACKOFF_BASE_DELAY", 3, PriorityProject)
	val := Get(s, KeyRetryBackoffBaseDelay)
	if val != 3.0 {
		t.Errorf("expected 3.0 from int, got %f", val)
	}
}

func TestGenericGet_Int64ToFloat64(t *testing.T) {
	s := NewEmpty()
	s.Set("RETRY_BACKOFF_BASE_DELAY", int64(5), PriorityProject)
	val := Get(s, KeyRetryBackoffBaseDelay)
	if val != 5.0 {
		t.Errorf("expected 5.0 from int64, got %f", val)
	}
}

func TestGenericGet_StringToFloat64(t *testing.T) {
	s := NewEmpty()
	s.Set("RETRY_BACKOFF_BASE_DELAY", "2.5", PriorityProject)
	val := Get(s, KeyRetryBackoffBaseDelay)
	if val != 2.5 {
		t.Errorf("expected 2.5 from string, got %f", val)
	}
}

func TestGenericGet_StringToInt(t *testing.T) {
	s := NewEmpty()
	s.Set("CONCURRENT_REQUESTS", "64", PriorityProject)
	val := Get(s, KeyConcurrentRequests)
	if val != 64 {
		t.Errorf("expected 64 from string, got %d", val)
	}
}

func TestGenericGet_IntToDuration(t *testing.T) {
	s := NewEmpty()
	s.Set("DOWNLOAD_TIMEOUT", 60, PriorityProject)
	val := Get(s, KeyDownloadTimeoutDuration)
	if val != 60*time.Second {
		t.Errorf("expected 60s from int, got %v", val)
	}
}

func TestGenericGet_Int64ToDuration(t *testing.T) {
	s := NewEmpty()
	s.Set("DOWNLOAD_TIMEOUT", int64(45), PriorityProject)
	val := Get(s, KeyDownloadTimeoutDuration)
	if val != 45*time.Second {
		t.Errorf("expected 45s from int64, got %v", val)
	}
}

func TestGenericGet_Float64ToDuration(t *testing.T) {
	s := NewEmpty()
	s.Set("DOWNLOAD_TIMEOUT", 1.5, PriorityProject)
	val := Get(s, KeyDownloadTimeoutDuration)
	if val != 1500*time.Millisecond {
		t.Errorf("expected 1.5s from float64, got %v", val)
	}
}

func TestGenericGet_StringToDuration(t *testing.T) {
	s := NewEmpty()
	s.Set("DOWNLOAD_TIMEOUT", "5s", PriorityProject)
	val := Get(s, KeyDownloadTimeoutDuration)
	if val != 5*time.Second {
		t.Errorf("expected 5s from string, got %v", val)
	}
}

func TestGenericGet_StringNumericToDuration(t *testing.T) {
	s := NewEmpty()
	s.Set("DOWNLOAD_TIMEOUT", "30", PriorityProject)
	val := Get(s, KeyDownloadTimeoutDuration)
	if val != 30*time.Second {
		t.Errorf("expected 30s from numeric string, got %v", val)
	}
}

func TestGenericGet_IntToBool(t *testing.T) {
	s := NewEmpty()
	s.Set("RETRY_ENABLED", 1, PriorityProject)
	val := Get(s, KeyRetryEnabled)
	if !val {
		t.Error("expected true from int 1")
	}

	s.Set("RETRY_ENABLED", int64(0), PriorityCmdline)
	val = Get(s, KeyRetryEnabled)
	if val {
		t.Error("expected false from int64 0")
	}
}

func TestGenericGet_Float64ToBool(t *testing.T) {
	s := NewEmpty()
	s.Set("RETRY_ENABLED", 1.0, PriorityProject)
	val := Get(s, KeyRetryEnabled)
	if !val {
		t.Error("expected true from float64 1.0")
	}

	s.Set("RETRY_ENABLED", 0.0, PriorityCmdline)
	val = Get(s, KeyRetryEnabled)
	if val {
		t.Error("expected false from float64 0.0")
	}
}

func TestGenericGet_AnySliceToStringSlice(t *testing.T) {
	s := NewEmpty()
	s.Set("HTTPCACHE_IGNORE_SCHEMES", []any{"file", "ftp"}, PriorityProject)
	val := Get(s, KeyHTTPCacheIgnoreSchemes)
	if len(val) != 2 || val[0] != "file" || val[1] != "ftp" {
		t.Errorf("expected [file ftp], got %v", val)
	}
}

func TestGenericGet_AnySliceToIntSlice(t *testing.T) {
	s := NewEmpty()
	s.Set("RETRY_HTTP_CODES", []any{500, int64(502), float64(503)}, PriorityProject)
	val := Get(s, KeyRetryHTTPCodes)
	if len(val) != 3 || val[0] != 500 || val[1] != 502 || val[2] != 503 {
		t.Errorf("expected [500 502 503], got %v", val)
	}
}

func TestGenericGet_MapStringAnyToIntMap(t *testing.T) {
	s := NewEmpty()
	s.Set("DOWNLOADER_MIDDLEWARES", map[string]any{
		"Retry":    float64(550),
		"Redirect": int64(600),
	}, PriorityProject)
	val := Get(s, KeyDownloaderMiddlewares)
	if val["Retry"] != 550 {
		t.Errorf("expected Retry=550, got %d", val["Retry"])
	}
	if val["Redirect"] != 600 {
		t.Errorf("expected Redirect=600, got %d", val["Redirect"])
	}
}

func TestGenericGet_NonConvertibleReturnsDefault(t *testing.T) {
	s := NewEmpty()
	// 存储一个完全不兼容的类型
	s.Set("CONCURRENT_REQUESTS", struct{}{}, PriorityProject)
	val := Get(s, KeyConcurrentRequests)
	// 应返回 Key 中定义的默认值
	if val != 16 {
		t.Errorf("expected default 16 for non-convertible type, got %d", val)
	}
}

func TestGenericGet_IntToString(t *testing.T) {
	s := NewEmpty()
	s.Set("BOT_NAME", 42, PriorityProject)
	val := Get(s, KeyBotName)
	if val != "42" {
		t.Errorf("expected '42' from int, got %q", val)
	}
}
