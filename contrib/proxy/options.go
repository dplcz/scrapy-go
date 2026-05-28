package proxy

import (
	"errors"
	"time"
)

// StrategyKind 表示内置的代理选择策略类型。
type StrategyKind string

const (
	// StrategyRoundRobin 轮询策略：按顺序循环选择代理。
	StrategyRoundRobin StrategyKind = "round_robin"

	// StrategyRandom 随机策略：均匀随机选择一个健康代理。
	StrategyRandom StrategyKind = "random"

	// StrategyWeighted 加权随机策略：按 Proxy.Weight 加权随机选择。
	StrategyWeighted StrategyKind = "weighted"
)

// Options 定义代理池的配置选项。
//
// 所有时间字段为 0 时使用默认值；其他字段为零值时使用默认值。
// 推荐通过 DefaultOptions() 获取默认配置后按需调整。
type Options struct {
	// Strategy 是代理选择策略。
	// 默认值：StrategyRoundRobin。
	Strategy StrategyKind

	// MaxFailures 是单个代理被标记为不健康前允许的连续失败次数。
	// 达到此阈值后代理将被置为 StateUnhealthy 并从选择池中剔除，
	// 直到健康检查通过后恢复。
	// 默认值：3。
	MaxFailures int

	// HealthCheckEnabled 是否启用后台健康检查。
	// 默认值：true。
	HealthCheckEnabled bool

	// HealthCheckURL 是健康检查请求的目标 URL。
	// 默认值："http://www.google.com/generate_204"
	// （204 响应表示连通性正常，与 Chrome/Android 系统检测一致）。
	HealthCheckURL string

	// HealthCheckInterval 是两次健康检查之间的时间间隔。
	// 默认值：30s。
	HealthCheckInterval time.Duration

	// HealthCheckTimeout 是单次健康检查请求的超时时间。
	// 默认值：5s。
	HealthCheckTimeout time.Duration

	// HealthCheckExpectedStatus 是健康检查期望的 HTTP 状态码。
	// 收到此状态码视为代理健康。
	// 默认值：204。
	HealthCheckExpectedStatus int

	// RecoveryThreshold 是连续健康检查通过多少次后将不健康的代理恢复为健康。
	// 默认值：1（一次成功即恢复）。
	RecoveryThreshold int

	// ProviderRefreshInterval 是 Provider 周期性刷新的时间间隔。
	// 设置为 0 表示禁用周期性刷新（仅在初始化时拉取一次）。
	// 默认值：0（禁用）。
	ProviderRefreshInterval time.Duration

	// AutoRetryOnFailure 是否在请求失败时自动切换代理重试。
	// 启用后中间件会在 ProcessException 中标记 Meta["proxy_retry"]，
	// 由 RetryMiddleware 协作完成重试。
	// 默认值：true。
	AutoRetryOnFailure bool

	// MaxProxyRetries 是同一请求最多切换代理的次数。
	// 默认值：3。
	MaxProxyRetries int
}

// DefaultOptions 返回默认配置选项。
func DefaultOptions() *Options {
	return &Options{
		Strategy:                  StrategyRoundRobin,
		MaxFailures:               3,
		HealthCheckEnabled:        true,
		HealthCheckURL:            "http://www.google.com/generate_204",
		HealthCheckInterval:       30 * time.Second,
		HealthCheckTimeout:        5 * time.Second,
		HealthCheckExpectedStatus: 204,
		RecoveryThreshold:         1,
		ProviderRefreshInterval:   0,
		AutoRetryOnFailure:        true,
		MaxProxyRetries:           3,
	}
}

// validate 校验配置项的合法性。
func (o *Options) validate() error {
	switch o.Strategy {
	case StrategyRoundRobin, StrategyRandom, StrategyWeighted:
		// ok
	default:
		return errors.New("proxy: invalid Strategy, must be one of round_robin/random/weighted")
	}
	if o.MaxFailures < 1 {
		return errors.New("proxy: MaxFailures must be >= 1")
	}
	if o.HealthCheckInterval < 0 {
		return errors.New("proxy: HealthCheckInterval must be >= 0")
	}
	if o.HealthCheckTimeout <= 0 {
		return errors.New("proxy: HealthCheckTimeout must be > 0")
	}
	if o.RecoveryThreshold < 1 {
		return errors.New("proxy: RecoveryThreshold must be >= 1")
	}
	if o.MaxProxyRetries < 0 {
		return errors.New("proxy: MaxProxyRetries must be >= 0")
	}
	return nil
}

// normalize 将零值字段填充为默认值，保持配置宽容性。
func (o *Options) normalize() {
	def := DefaultOptions()
	if o.Strategy == "" {
		o.Strategy = def.Strategy
	}
	if o.MaxFailures == 0 {
		o.MaxFailures = def.MaxFailures
	}
	if o.HealthCheckURL == "" {
		o.HealthCheckURL = def.HealthCheckURL
	}
	if o.HealthCheckInterval == 0 {
		o.HealthCheckInterval = def.HealthCheckInterval
	}
	if o.HealthCheckTimeout == 0 {
		o.HealthCheckTimeout = def.HealthCheckTimeout
	}
	if o.HealthCheckExpectedStatus == 0 {
		o.HealthCheckExpectedStatus = def.HealthCheckExpectedStatus
	}
	if o.RecoveryThreshold == 0 {
		o.RecoveryThreshold = def.RecoveryThreshold
	}
	if o.MaxProxyRetries == 0 {
		o.MaxProxyRetries = def.MaxProxyRetries
	}
}
