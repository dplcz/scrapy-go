package downloader

import (
	"testing"
	"time"
)

// ============================================================================
// ConnPoolConfig 测试
// ============================================================================

func TestDefaultConnPoolConfig(t *testing.T) {
	config := DefaultConnPoolConfig()

	if config.MaxIdleConns != 100 {
		t.Errorf("expected MaxIdleConns 100, got %d", config.MaxIdleConns)
	}
	if config.MaxIdleConnsPerHost != 10 {
		t.Errorf("expected MaxIdleConnsPerHost 10, got %d", config.MaxIdleConnsPerHost)
	}
	if config.MaxConnsPerHost != 0 {
		t.Errorf("expected MaxConnsPerHost 0, got %d", config.MaxConnsPerHost)
	}
	if config.IdleConnTimeout != 90*time.Second {
		t.Errorf("expected IdleConnTimeout 90s, got %v", config.IdleConnTimeout)
	}
	if config.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("expected TLSHandshakeTimeout 10s, got %v", config.TLSHandshakeTimeout)
	}
	if config.ExpectContinueTimeout != 1*time.Second {
		t.Errorf("expected ExpectContinueTimeout 1s, got %v", config.ExpectContinueTimeout)
	}
	if config.DisableKeepAlives {
		t.Error("expected DisableKeepAlives false")
	}
	if config.ForceHTTP2 {
		t.Error("expected ForceHTTP2 false")
	}
	if config.DialTimeout != 30*time.Second {
		t.Errorf("expected DialTimeout 30s, got %v", config.DialTimeout)
	}
	if config.DialKeepAlive != 30*time.Second {
		t.Errorf("expected DialKeepAlive 30s, got %v", config.DialKeepAlive)
	}
}

// ============================================================================
// ConnPoolStats 测试
// ============================================================================

func TestConnPoolStats_Snapshot(t *testing.T) {
	stats := &ConnPoolStats{}

	// 初始状态
	snapshot := stats.Snapshot()
	for key, val := range snapshot {
		if val != 0 {
			t.Errorf("initial stat %s should be 0, got %d", key, val)
		}
	}

	// 更新统计
	stats.TotalConnsCreated.Add(5)
	stats.TotalConnsReused.Add(10)
	stats.TotalConnsClosed.Add(3)
	stats.TotalTLSHandshakes.Add(2)
	stats.ActiveConns.Add(4)
	stats.IdleConns.Add(6)

	snapshot = stats.Snapshot()
	if snapshot["conns_created"] != 5 {
		t.Errorf("expected conns_created 5, got %d", snapshot["conns_created"])
	}
	if snapshot["conns_reused"] != 10 {
		t.Errorf("expected conns_reused 10, got %d", snapshot["conns_reused"])
	}
	if snapshot["conns_closed"] != 3 {
		t.Errorf("expected conns_closed 3, got %d", snapshot["conns_closed"])
	}
	if snapshot["tls_handshakes"] != 2 {
		t.Errorf("expected tls_handshakes 2, got %d", snapshot["tls_handshakes"])
	}
	if snapshot["active_conns"] != 4 {
		t.Errorf("expected active_conns 4, got %d", snapshot["active_conns"])
	}
	if snapshot["idle_conns"] != 6 {
		t.Errorf("expected idle_conns 6, got %d", snapshot["idle_conns"])
	}
}

func TestConnPoolStats_Concurrent(t *testing.T) {
	stats := &ConnPoolStats{}

	// 并发更新统计
	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func() {
			stats.TotalConnsCreated.Add(1)
			stats.TotalConnsReused.Add(1)
			stats.ActiveConns.Add(1)
			stats.ActiveConns.Add(-1)
			done <- struct{}{}
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	if stats.TotalConnsCreated.Load() != 100 {
		t.Errorf("expected TotalConnsCreated 100, got %d", stats.TotalConnsCreated.Load())
	}
	if stats.TotalConnsReused.Load() != 100 {
		t.Errorf("expected TotalConnsReused 100, got %d", stats.TotalConnsReused.Load())
	}
	if stats.ActiveConns.Load() != 0 {
		t.Errorf("expected ActiveConns 0, got %d", stats.ActiveConns.Load())
	}
}

// ============================================================================
// ManagedTransport 测试
// ============================================================================

func TestNewManagedTransport_Default(t *testing.T) {
	mt := NewManagedTransport(nil)
	if mt == nil {
		t.Fatal("NewManagedTransport should not return nil")
	}
	if mt.Transport == nil {
		t.Fatal("Transport should not be nil")
	}
	if mt.stats == nil {
		t.Fatal("stats should not be nil")
	}

	// 验证默认配置
	if mt.Transport.MaxIdleConns != 100 {
		t.Errorf("expected MaxIdleConns 100, got %d", mt.Transport.MaxIdleConns)
	}
	if mt.Transport.MaxIdleConnsPerHost != 10 {
		t.Errorf("expected MaxIdleConnsPerHost 10, got %d", mt.Transport.MaxIdleConnsPerHost)
	}
	if !mt.Transport.DisableCompression {
		t.Error("expected DisableCompression true")
	}
}

func TestNewManagedTransport_CustomConfig(t *testing.T) {
	config := &ConnPoolConfig{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 50,
		MaxConnsPerHost:     100,
		IdleConnTimeout:     120 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		DisableKeepAlives:   true,
		ForceHTTP2:          true,
		WriteBufferSize:     8192,
		ReadBufferSize:      8192,
		DialTimeout:         15 * time.Second,
		DialKeepAlive:       60 * time.Second,
	}

	mt := NewManagedTransport(config)

	if mt.Transport.MaxIdleConns != 200 {
		t.Errorf("expected MaxIdleConns 200, got %d", mt.Transport.MaxIdleConns)
	}
	if mt.Transport.MaxIdleConnsPerHost != 50 {
		t.Errorf("expected MaxIdleConnsPerHost 50, got %d", mt.Transport.MaxIdleConnsPerHost)
	}
	if mt.Transport.MaxConnsPerHost != 100 {
		t.Errorf("expected MaxConnsPerHost 100, got %d", mt.Transport.MaxConnsPerHost)
	}
	if mt.Transport.IdleConnTimeout != 120*time.Second {
		t.Errorf("expected IdleConnTimeout 120s, got %v", mt.Transport.IdleConnTimeout)
	}
	if !mt.Transport.DisableKeepAlives {
		t.Error("expected DisableKeepAlives true")
	}
	if !mt.Transport.ForceAttemptHTTP2 {
		t.Error("expected ForceAttemptHTTP2 true")
	}
	if mt.Transport.WriteBufferSize != 8192 {
		t.Errorf("expected WriteBufferSize 8192, got %d", mt.Transport.WriteBufferSize)
	}
	if mt.Transport.ReadBufferSize != 8192 {
		t.Errorf("expected ReadBufferSize 8192, got %d", mt.Transport.ReadBufferSize)
	}
}

func TestManagedTransport_Stats(t *testing.T) {
	mt := NewManagedTransport(nil)
	stats := mt.Stats()
	if stats == nil {
		t.Fatal("Stats should not be nil")
	}
}

// ============================================================================
// ConnPoolConfigFromSettings 测试
// ============================================================================

func TestConnPoolConfigFromSettings_Defaults(t *testing.T) {
	getInt := func(key string, def int) int { return def }
	getDuration := func(key string, def time.Duration) time.Duration { return def }
	getBool := func(key string, def bool) bool { return def }

	config := ConnPoolConfigFromSettings(getInt, getDuration, getBool)

	// 应该返回默认值
	expected := DefaultConnPoolConfig()
	if config.MaxIdleConns != expected.MaxIdleConns {
		t.Errorf("expected MaxIdleConns %d, got %d", expected.MaxIdleConns, config.MaxIdleConns)
	}
	if config.MaxIdleConnsPerHost != expected.MaxIdleConnsPerHost {
		t.Errorf("expected MaxIdleConnsPerHost %d, got %d", expected.MaxIdleConnsPerHost, config.MaxIdleConnsPerHost)
	}
}

func TestConnPoolConfigFromSettings_Custom(t *testing.T) {
	settings := map[string]any{
		"CONNPOOL_MAX_IDLE_CONNS":          200,
		"CONNPOOL_MAX_IDLE_CONNS_PER_HOST": 50,
		"CONNPOOL_MAX_CONNS_PER_HOST":      100,
		"CONNPOOL_IDLE_CONN_TIMEOUT":       120 * time.Second,
		"CONNPOOL_TLS_HANDSHAKE_TIMEOUT":   5 * time.Second,
		"CONNPOOL_DIAL_TIMEOUT":            15 * time.Second,
		"CONNPOOL_DIAL_KEEPALIVE":          60 * time.Second,
		"CONNPOOL_DISABLE_KEEPALIVES":      true,
		"HTTP2_ENABLED":                    true,
		"CONNPOOL_WRITE_BUFFER_SIZE":       8192,
		"CONNPOOL_READ_BUFFER_SIZE":        16384,
	}

	getInt := func(key string, def int) int {
		if v, ok := settings[key]; ok {
			return v.(int)
		}
		return def
	}
	getDuration := func(key string, def time.Duration) time.Duration {
		if v, ok := settings[key]; ok {
			return v.(time.Duration)
		}
		return def
	}
	getBool := func(key string, def bool) bool {
		if v, ok := settings[key]; ok {
			return v.(bool)
		}
		return def
	}

	config := ConnPoolConfigFromSettings(getInt, getDuration, getBool)

	if config.MaxIdleConns != 200 {
		t.Errorf("expected MaxIdleConns 200, got %d", config.MaxIdleConns)
	}
	if config.MaxIdleConnsPerHost != 50 {
		t.Errorf("expected MaxIdleConnsPerHost 50, got %d", config.MaxIdleConnsPerHost)
	}
	if config.MaxConnsPerHost != 100 {
		t.Errorf("expected MaxConnsPerHost 100, got %d", config.MaxConnsPerHost)
	}
	if config.IdleConnTimeout != 120*time.Second {
		t.Errorf("expected IdleConnTimeout 120s, got %v", config.IdleConnTimeout)
	}
	if config.TLSHandshakeTimeout != 5*time.Second {
		t.Errorf("expected TLSHandshakeTimeout 5s, got %v", config.TLSHandshakeTimeout)
	}
	if config.DialTimeout != 15*time.Second {
		t.Errorf("expected DialTimeout 15s, got %v", config.DialTimeout)
	}
	if config.DialKeepAlive != 60*time.Second {
		t.Errorf("expected DialKeepAlive 60s, got %v", config.DialKeepAlive)
	}
	if !config.DisableKeepAlives {
		t.Error("expected DisableKeepAlives true")
	}
	if !config.ForceHTTP2 {
		t.Error("expected ForceHTTP2 true")
	}
	if config.WriteBufferSize != 8192 {
		t.Errorf("expected WriteBufferSize 8192, got %d", config.WriteBufferSize)
	}
	if config.ReadBufferSize != 16384 {
		t.Errorf("expected ReadBufferSize 16384, got %d", config.ReadBufferSize)
	}
}
