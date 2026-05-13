package ratelimit

import "time"

// Options 定义分布式限速器的配置选项。
type Options struct {
	// Addr 是 Redis 服务器地址，格式为 "host:port"。
	// 默认值："localhost:6379"
	Addr string

	// Password 是 Redis 认证密码。
	// 默认值：""（无密码）
	Password string

	// DB 是 Redis 数据库编号。
	// 默认值：0
	DB int

	// KeyPrefix 是所有限速器 Redis Key 的前缀。
	// 用于隔离不同爬虫或不同环境的限速数据。
	// 默认值："scrapy-go:ratelimit"
	//
	// 实际 Key 格式：{KeyPrefix}:{domain}
	KeyPrefix string

	// DefaultRate 是每个窗口周期内允许的最大请求数。
	// 适用于未单独配置速率的域名。
	// 默认值：10
	DefaultRate int

	// DefaultBurst 是突发容量，允许短时间内超过 DefaultRate 的请求数。
	// 突发请求会消耗后续窗口的配额。
	// 默认值：20
	DefaultBurst int

	// Window 是滑动窗口的时间长度。
	// 限速器在每个窗口周期内最多允许 DefaultRate 个请求。
	// 默认值：1s（即 DefaultRate 表示每秒允许的请求数）
	Window time.Duration

	// DomainRates 允许为特定域名配置独立的速率限制。
	// Key 为域名，Value 为该域名每个窗口周期内允许的最大请求数。
	// 未在此 map 中配置的域名使用 DefaultRate。
	// 默认值：nil（所有域名使用 DefaultRate）
	DomainRates map[string]int

	// WaitTimeout 是 Wait 方法的默认超时时间。
	// 当调用 Wait 时，如果传入的 context 没有设置 deadline，
	// 则使用此超时时间作为最大等待时长。
	// 默认值：30s
	WaitTimeout time.Duration

	// DialTimeout 是连接 Redis 的超时时间。
	// 默认值：5s
	DialTimeout time.Duration

	// ReadTimeout 是 Redis 读操作超时时间。
	// 默认值：3s
	ReadTimeout time.Duration

	// WriteTimeout 是 Redis 写操作超时时间。
	// 默认值：3s
	WriteTimeout time.Duration

	// PoolSize 是 Redis 连接池大小。
	// 默认值：10
	PoolSize int

	// KeyExpiration 是限速器 Key 的过期时间。
	// 超过此时间未被访问的域名限速 Key 将自动过期清理。
	// 默认值：1h
	KeyExpiration time.Duration
}

// DefaultOptions 返回默认的限速器配置选项。
func DefaultOptions() *Options {
	return &Options{
		Addr:          "localhost:6379",
		Password:      "",
		DB:            0,
		KeyPrefix:     "scrapy-go:ratelimit",
		DefaultRate:   10,
		DefaultBurst:  20,
		Window:        time.Second,
		DomainRates:   nil,
		WaitTimeout:   30 * time.Second,
		DialTimeout:   5 * time.Second,
		ReadTimeout:   3 * time.Second,
		WriteTimeout:  3 * time.Second,
		PoolSize:      10,
		KeyExpiration: time.Hour,
	}
}

// rateForDomain 返回指定域名的速率限制。
// 如果域名有独立配置则返回独立配置，否则返回默认速率。
func (o *Options) rateForDomain(domain string) int {
	if o.DomainRates != nil {
		if rate, ok := o.DomainRates[domain]; ok {
			return rate
		}
	}
	return o.DefaultRate
}

// keyForDomain 返回指定域名的完整 Redis Key。
func (o *Options) keyForDomain(domain string) string {
	return o.KeyPrefix + ":" + domain
}
