package spider

import (
	"testing"
	"time"
)

func TestSettingsToMapNil(t *testing.T) {
	var ss *Settings
	if m := ss.ToMap(); m != nil {
		t.Errorf("nil Settings.ToMap() should return nil, got %v", m)
	}
}

func TestSettingsToMapEmpty(t *testing.T) {
	ss := &Settings{}
	m := ss.ToMap()
	if len(m) != 0 {
		t.Errorf("empty Settings.ToMap() should return empty map, got %v", m)
	}
}

func TestSettingsToMapConcurrency(t *testing.T) {
	ss := &Settings{
		ConcurrentRequests:          IntPtr(4),
		ConcurrentRequestsPerDomain: IntPtr(2),
		ConcurrentItems:             IntPtr(50),
	}
	m := ss.ToMap()

	assertMapInt(t, m, "CONCURRENT_REQUESTS", 4)
	assertMapInt(t, m, "CONCURRENT_REQUESTS_PER_DOMAIN", 2)
	assertMapInt(t, m, "CONCURRENT_ITEMS", 50)
}

func TestSettingsToMapDownload(t *testing.T) {
	ss := &Settings{
		DownloadDelay:          DurationPtr(2 * time.Second),
		DownloadTimeout:        DurationPtr(30 * time.Second),
		RandomizeDownloadDelay: BoolPtr(false),
	}
	m := ss.ToMap()

	if v, ok := m["DOWNLOAD_DELAY"].(time.Duration); !ok || v != 2*time.Second {
		t.Errorf("DOWNLOAD_DELAY: expected 2s, got %v", m["DOWNLOAD_DELAY"])
	}
	if v, ok := m["DOWNLOAD_TIMEOUT"].(time.Duration); !ok || v != 30*time.Second {
		t.Errorf("DOWNLOAD_TIMEOUT: expected 30s, got %v", m["DOWNLOAD_TIMEOUT"])
	}
	if v, ok := m["RANDOMIZE_DOWNLOAD_DELAY"].(bool); !ok || v != false {
		t.Errorf("RANDOMIZE_DOWNLOAD_DELAY: expected false, got %v", m["RANDOMIZE_DOWNLOAD_DELAY"])
	}
}

func TestSettingsToMapRetry(t *testing.T) {
	ss := &Settings{
		RetryEnabled:   BoolPtr(false),
		RetryTimes:     IntPtr(5),
		RetryHTTPCodes: []int{500, 502},
	}
	m := ss.ToMap()

	if v, ok := m["RETRY_ENABLED"].(bool); !ok || v != false {
		t.Errorf("RETRY_ENABLED: expected false, got %v", m["RETRY_ENABLED"])
	}
	assertMapInt(t, m, "RETRY_TIMES", 5)
	codes, ok := m["RETRY_HTTP_CODES"].([]int)
	if !ok || len(codes) != 2 || codes[0] != 500 || codes[1] != 502 {
		t.Errorf("RETRY_HTTP_CODES: expected [500, 502], got %v", m["RETRY_HTTP_CODES"])
	}
}

func TestSettingsToMapRedirect(t *testing.T) {
	ss := &Settings{
		RedirectEnabled:  BoolPtr(true),
		RedirectMaxTimes: IntPtr(10),
	}
	m := ss.ToMap()

	if v, ok := m["REDIRECT_ENABLED"].(bool); !ok || v != true {
		t.Errorf("REDIRECT_ENABLED: expected true, got %v", m["REDIRECT_ENABLED"])
	}
	assertMapInt(t, m, "REDIRECT_MAX_TIMES", 10)
}

func TestSettingsToMapLogAndScheduler(t *testing.T) {
	ss := &Settings{
		LogLevel:       StringPtr("WARN"),
		SchedulerDebug: BoolPtr(true),
		StatsDump:      BoolPtr(false),
		DepthLimit:     IntPtr(3),
	}
	m := ss.ToMap()

	if v, ok := m["LOG_LEVEL"].(string); !ok || v != "WARN" {
		t.Errorf("LOG_LEVEL: expected WARN, got %v", m["LOG_LEVEL"])
	}
	if v, ok := m["SCHEDULER_DEBUG"].(bool); !ok || v != true {
		t.Errorf("SCHEDULER_DEBUG: expected true, got %v", m["SCHEDULER_DEBUG"])
	}
	if v, ok := m["STATS_DUMP"].(bool); !ok || v != false {
		t.Errorf("STATS_DUMP: expected false, got %v", m["STATS_DUMP"])
	}
	assertMapInt(t, m, "DEPTH_LIMIT", 3)
}

func TestSettingsToMapUserAgent(t *testing.T) {
	ss := &Settings{
		UserAgent: StringPtr("my-bot/1.0"),
	}
	m := ss.ToMap()

	if v, ok := m["USER_AGENT"].(string); !ok || v != "my-bot/1.0" {
		t.Errorf("USER_AGENT: expected my-bot/1.0, got %v", m["USER_AGENT"])
	}
}

func TestSettingsToMapProxy(t *testing.T) {
	ss := &Settings{
		HttpProxyEnabled: BoolPtr(false),
	}
	m := ss.ToMap()

	if v, ok := m["HTTPPROXY_ENABLED"].(bool); !ok || v != false {
		t.Errorf("HTTPPROXY_ENABLED: expected false, got %v", m["HTTPPROXY_ENABLED"])
	}
}

func TestSettingsToMapDownloaderStats(t *testing.T) {
	ss := &Settings{
		DownloaderStats: BoolPtr(false),
	}
	m := ss.ToMap()

	if v, ok := m["DOWNLOADER_STATS"].(bool); !ok || v != false {
		t.Errorf("DOWNLOADER_STATS: expected false, got %v", m["DOWNLOADER_STATS"])
	}
}

func TestSettingsToMapExtra(t *testing.T) {
	ss := &Settings{
		ConcurrentRequests: IntPtr(8),
		Extra: map[string]any{
			"CUSTOM_KEY":    "custom_value",
			"CUSTOM_NUMBER": 42,
		},
	}
	m := ss.ToMap()

	assertMapInt(t, m, "CONCURRENT_REQUESTS", 8)
	if v, ok := m["CUSTOM_KEY"].(string); !ok || v != "custom_value" {
		t.Errorf("CUSTOM_KEY: expected custom_value, got %v", m["CUSTOM_KEY"])
	}
	if v, ok := m["CUSTOM_NUMBER"].(int); !ok || v != 42 {
		t.Errorf("CUSTOM_NUMBER: expected 42, got %v", m["CUSTOM_NUMBER"])
	}
}

func TestSettingsOnlySetFieldsInMap(t *testing.T) {
	// 只设置一个字段，确认其他字段不出现在 map 中
	ss := &Settings{
		LogLevel: StringPtr("ERROR"),
	}
	m := ss.ToMap()

	if len(m) != 1 {
		t.Errorf("expected map with 1 entry, got %d: %v", len(m), m)
	}
	if _, ok := m["CONCURRENT_REQUESTS"]; ok {
		t.Error("CONCURRENT_REQUESTS should not be in map when not set")
	}
}

func TestPtrHelpers(t *testing.T) {
	if v := IntPtr(42); *v != 42 {
		t.Errorf("IntPtr: expected 42, got %d", *v)
	}
	if v := StringPtr("hello"); *v != "hello" {
		t.Errorf("StringPtr: expected hello, got %s", *v)
	}
	if v := BoolPtr(true); *v != true {
		t.Errorf("BoolPtr: expected true, got %v", *v)
	}
	if v := DurationPtr(5 * time.Second); *v != 5*time.Second {
		t.Errorf("DurationPtr: expected 5s, got %v", *v)
	}
}

// assertMapInt 断言 map 中指定 key 的值为 int 且等于 expected。
func assertMapInt(t *testing.T, m map[string]any, key string, expected int) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("%s: key not found in map", key)
		return
	}
	if iv, ok := v.(int); !ok || iv != expected {
		t.Errorf("%s: expected %d, got %v", key, expected, v)
	}
}
