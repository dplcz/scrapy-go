package downloader

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
)

// ConnPoolConfig 定义连接池的精细化配置。
// 允许用户根据目标站点特性调整连接池参数，以获得最佳性能。
//
// 对应 Scrapy 中通过 Twisted reactor 配置的连接池参数，
// 但 Go 的 net/http.Transport 提供了更丰富的连接池控制能力。
type ConnPoolConfig struct {
	// MaxIdleConns 控制所有 host 的最大空闲连接总数。
	// 默认值：100。设为 0 表示不限制。
	MaxIdleConns int

	// MaxIdleConnsPerHost 控制每个 host 的最大空闲连接数。
	// 默认值：10。对于高并发单站点爬取，建议设为与 CONCURRENT_REQUESTS_PER_DOMAIN 相同。
	MaxIdleConnsPerHost int

	// MaxConnsPerHost 控制每个 host 的最大连接数（含活跃和空闲）。
	// 默认值：0（不限制）。设置此值可防止对单个站点建立过多连接。
	MaxConnsPerHost int

	// IdleConnTimeout 控制空闲连接在被关闭前的最大存活时间。
	// 默认值：90s。对于长时间运行的爬虫，可适当增大以复用连接。
	IdleConnTimeout time.Duration

	// TLSHandshakeTimeout 控制 TLS 握手的超时时间。
	// 默认值：10s。
	TLSHandshakeTimeout time.Duration

	// ResponseHeaderTimeout 控制等待响应头的超时时间。
	// 默认值：0（不限制，由 DOWNLOAD_TIMEOUT 统一控制）。
	ResponseHeaderTimeout time.Duration

	// ExpectContinueTimeout 控制发送 Expect: 100-continue 后等待服务器响应的超时。
	// 默认值：1s。
	ExpectContinueTimeout time.Duration

	// DisableKeepAlives 禁用 HTTP keep-alive，每次请求使用新连接。
	// 默认值：false。仅在调试或特殊场景下启用。
	DisableKeepAlives bool

	// ForceHTTP2 强制使用 HTTP/2 协议。
	// 默认值：false。启用后将使用 HTTP/2 专用 Transport。
	ForceHTTP2 bool

	// WriteBufferSize 设置 Transport 的写缓冲区大小。
	// 默认值：0（使用标准库默认值 4KB）。
	WriteBufferSize int

	// ReadBufferSize 设置 Transport 的读缓冲区大小。
	// 默认值：0（使用标准库默认值 4KB）。
	ReadBufferSize int

	// DialTimeout 控制建立 TCP 连接的超时时间。
	// 默认值：30s。
	DialTimeout time.Duration

	// DialKeepAlive 控制 TCP keep-alive 探测间隔。
	// 默认值：30s。设为负值禁用 TCP keep-alive。
	DialKeepAlive time.Duration

	// TLSInsecureSkipVerify 跳过 TLS 证书验证。
	// 默认值：false。仅用于测试或信任的内网环境。
	TLSInsecureSkipVerify bool

	// AllowH2C 启用 HTTP/2 over cleartext（h2c）支持。
	// 默认值：false。启用后将注册 http2.Transport 作为 http:// scheme 的 handler，
	// 允许在不使用 TLS 的情况下使用 HTTP/2 协议（适用于内网/测试场景）。
	AllowH2C bool
}

// DefaultConnPoolConfig 返回默认的连接池配置。
func DefaultConnPoolConfig() *ConnPoolConfig {
	return &ConnPoolConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 0,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     false,
		ForceHTTP2:            false,
		WriteBufferSize:       0,
		ReadBufferSize:        0,
		DialTimeout:           30 * time.Second,
		DialKeepAlive:         30 * time.Second,
	}
}

// ConnPoolStats 记录连接池的运行时统计信息。
// 通过 atomic 操作保证并发安全，无锁读取。
type ConnPoolStats struct {
	// TotalConnsCreated 累计创建的连接数。
	TotalConnsCreated atomic.Int64

	// TotalConnsReused 累计复用的连接数。
	TotalConnsReused atomic.Int64

	// TotalConnsClosed 累计关闭的连接数。
	TotalConnsClosed atomic.Int64

	// TotalTLSHandshakes 累计 TLS 握手次数。
	TotalTLSHandshakes atomic.Int64

	// ActiveConns 当前活跃连接数。
	ActiveConns atomic.Int64

	// IdleConns 当前空闲连接数。
	IdleConns atomic.Int64
}

// Snapshot 返回连接池统计的快照（用于日志和监控）。
func (s *ConnPoolStats) Snapshot() map[string]int64 {
	return map[string]int64{
		"conns_created":  s.TotalConnsCreated.Load(),
		"conns_reused":   s.TotalConnsReused.Load(),
		"conns_closed":   s.TotalConnsClosed.Load(),
		"tls_handshakes": s.TotalTLSHandshakes.Load(),
		"active_conns":   s.ActiveConns.Load(),
		"idle_conns":     s.IdleConns.Load(),
	}
}

// ManagedTransport 是对 http.Transport 的包装，提供连接池统计和精细化配置。
type ManagedTransport struct {
	*http.Transport
	stats *ConnPoolStats
}

// NewManagedTransport 根据配置创建一个带统计功能的 Transport。
func NewManagedTransport(config *ConnPoolConfig) *ManagedTransport {
	if config == nil {
		config = DefaultConnPoolConfig()
	}

	dialer := &net.Dialer{
		Timeout:   config.DialTimeout,
		KeepAlive: config.DialKeepAlive,
	}

	stats := &ConnPoolStats{}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.MaxConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		ExpectContinueTimeout: config.ExpectContinueTimeout,
		DisableKeepAlives:     config.DisableKeepAlives,
		DisableCompression:    true, // 由 HttpCompressionMiddleware 统一管理
		ForceAttemptHTTP2:     config.ForceHTTP2,
		WriteBufferSize:       config.WriteBufferSize,
		ReadBufferSize:        config.ReadBufferSize,
		TLSClientConfig: &tls.Config{
			// 启用 HTTP/2 需要 ALPN 协商
			NextProtos:         []string{"h2", "http/1.1"},
			InsecureSkipVerify: config.TLSInsecureSkipVerify,
		},
	}

	// 如果启用 h2c，注册 http2.Transport 作为 http:// scheme 的 handler
	if config.AllowH2C {
		h2cTransport := &http2.Transport{
			AllowHTTP:       true,
			ReadIdleTimeout: config.IdleConnTimeout,
			PingTimeout:     15 * time.Second,
			// h2c 需要自定义 DialTLSContext 来建立纯 TCP 连接（而非 TLS 连接）。
			// 当 AllowHTTP=true 时，http2.Transport 会对 http:// URL 调用 DialTLSContext，
			// 我们需要返回一个普通的 TCP 连接而非 TLS 连接。
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{
					Timeout:   config.DialTimeout,
					KeepAlive: config.DialKeepAlive,
				}).DialContext(ctx, network, addr)
			},
		}
		// 注册 http2.Transport 处理 https:// 以外的 http:// 请求
		transport.RegisterProtocol("http", h2cTransportWrapper{h2cTransport})
	}

	mt := &ManagedTransport{
		Transport: transport,
		stats:     stats,
	}

	return mt
}

// Stats 返回连接池统计信息。
func (mt *ManagedTransport) Stats() *ConnPoolStats {
	return mt.stats
}

// CloseIdleConnections 关闭所有空闲连接。
func (mt *ManagedTransport) CloseIdleConnections() {
	mt.Transport.CloseIdleConnections()
}

// ConnPoolConfigFromSettings 从 Settings 中读取连接池配置。
// 配置键名以 CONNPOOL_ 为前缀。
func ConnPoolConfigFromSettings(getInt func(string, int) int, getDuration func(string, time.Duration) time.Duration, getBool func(string, bool) bool) *ConnPoolConfig {
	config := DefaultConnPoolConfig()

	if v := getInt("CONNPOOL_MAX_IDLE_CONNS", 0); v > 0 {
		config.MaxIdleConns = v
	}
	if v := getInt("CONNPOOL_MAX_IDLE_CONNS_PER_HOST", 0); v > 0 {
		config.MaxIdleConnsPerHost = v
	}
	if v := getInt("CONNPOOL_MAX_CONNS_PER_HOST", 0); v > 0 {
		config.MaxConnsPerHost = v
	}
	if v := getDuration("CONNPOOL_IDLE_CONN_TIMEOUT", 0); v > 0 {
		config.IdleConnTimeout = v
	}
	if v := getDuration("CONNPOOL_TLS_HANDSHAKE_TIMEOUT", 0); v > 0 {
		config.TLSHandshakeTimeout = v
	}
	if v := getDuration("CONNPOOL_RESPONSE_HEADER_TIMEOUT", 0); v > 0 {
		config.ResponseHeaderTimeout = v
	}
	if v := getDuration("CONNPOOL_DIAL_TIMEOUT", 0); v > 0 {
		config.DialTimeout = v
	}
	if v := getDuration("CONNPOOL_DIAL_KEEPALIVE", 0); v > 0 {
		config.DialKeepAlive = v
	}
	config.DisableKeepAlives = getBool("CONNPOOL_DISABLE_KEEPALIVES", false)
	config.ForceHTTP2 = getBool("HTTP2_ENABLED", false)
	config.AllowH2C = getBool("HTTP2_ALLOW_H2C", false)

	if v := getInt("CONNPOOL_WRITE_BUFFER_SIZE", 0); v > 0 {
		config.WriteBufferSize = v
	}
	if v := getInt("CONNPOOL_READ_BUFFER_SIZE", 0); v > 0 {
		config.ReadBufferSize = v
	}

	return config
}

// h2cTransportWrapper 包装 http2.Transport 以实现 http.RoundTripper 接口，
// 用于通过 Transport.RegisterProtocol 注册为 http:// scheme 的处理器。
// 这使得 HTTP/2 over cleartext（h2c）可以在不使用 TLS 的情况下工作。
type h2cTransportWrapper struct {
	*http2.Transport
}

// RoundTrip 实现 http.RoundTripper 接口。
func (w h2cTransportWrapper) RoundTrip(req *http.Request) (*http.Response, error) {
	return w.Transport.RoundTrip(req)
}
