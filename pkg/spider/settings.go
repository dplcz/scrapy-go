// Package spider 定义了 scrapy-go 框架的 Spider 配置结构体。
//
// Settings 提供类型安全的 Spider 级别配置，
// 替代原有的 map[string]any 方式，避免字符串 key 拼写错误和类型不匹配。
package spider

import "time"

// Settings 定义 Spider 级别的个性化配置。
// 所有字段均为指针类型，nil 表示不覆盖框架默认值。
//
// 用法：
//
//	func (s *MySpider) CustomSettings() *Settings {
//	    return &Settings{
//	        ConcurrentRequests: IntPtr(4),
//	        DownloadDelay:      DurationPtr(time.Second),
//	        LogLevel:           StringPtr("INFO"),
//	    }
//	}
type Settings struct {
	// ========================================================================
	// 并发控制
	// ========================================================================

	// ConcurrentRequests 最大并发请求数（默认 16）。
	ConcurrentRequests *int `json:"concurrent_requests,omitempty"`

	// ConcurrentRequestsPerDomain 每个域名的最大并发请求数（默认 8）。
	ConcurrentRequestsPerDomain *int `json:"concurrent_requests_per_domain,omitempty"`

	// ConcurrentItems 最大并发 Item 处理数（默认 100）。
	ConcurrentItems *int `json:"concurrent_items,omitempty"`

	// ========================================================================
	// 下载配置
	// ========================================================================

	// DownloadDelay 下载延迟（默认 0，无延迟）。
	DownloadDelay *time.Duration `json:"download_delay,omitempty"`

	// DownloadTimeout 下载超时（默认 180s）。
	DownloadTimeout *time.Duration `json:"download_timeout,omitempty"`

	// RandomizeDownloadDelay 是否随机化下载延迟（默认 true）。
	RandomizeDownloadDelay *bool `json:"randomize_download_delay,omitempty"`

	// ========================================================================
	// 重试配置
	// ========================================================================

	// RetryEnabled 是否启用重试（默认 true）。
	RetryEnabled *bool `json:"retry_enabled,omitempty"`

	// RetryTimes 最大重试次数（默认 2）。
	RetryTimes *int `json:"retry_times,omitempty"`

	// RetryHTTPCodes 需要重试的 HTTP 状态码列表。
	RetryHTTPCodes []int `json:"retry_http_codes,omitempty"`

	// ========================================================================
	// 重定向配置
	// ========================================================================

	// RedirectEnabled 是否启用重定向（默认 true）。
	RedirectEnabled *bool `json:"redirect_enabled,omitempty"`

	// RedirectMaxTimes 最大重定向次数（默认 20）。
	RedirectMaxTimes *int `json:"redirect_max_times,omitempty"`

	// ========================================================================
	// 深度控制
	// ========================================================================

	// DepthLimit 爬取深度限制，0 表示无限制（默认 0）。
	DepthLimit *int `json:"depth_limit,omitempty"`

	// ========================================================================
	// 日志配置
	// ========================================================================

	// LogLevel 日志级别：DEBUG、INFO、WARN、ERROR（默认 DEBUG）。
	LogLevel *string `json:"log_level,omitempty"`

	// ========================================================================
	// 调度器配置
	// ========================================================================

	// SchedulerDebug 是否开启调度器调试日志（默认 false）。
	SchedulerDebug *bool `json:"scheduler_debug,omitempty"`

	// ========================================================================
	// 统计配置
	// ========================================================================

	// StatsDump 是否在 Spider 关闭时输出统计信息（默认 true）。
	StatsDump *bool `json:"stats_dump,omitempty"`

	// ========================================================================
	// UserAgent
	// ========================================================================

	// UserAgent 自定义 User-Agent 字符串。
	UserAgent *string `json:"user_agent,omitempty"`

	// ========================================================================
	// 扩展配置（用于不常用或自定义的配置项）
	// ========================================================================

	// Extra 额外的自定义配置项，key 为配置名称。
	// 用于框架未预定义的配置项，会直接合并到 Settings 中。
	Extra map[string]any `json:"extra,omitempty"`
}

// ToMap 将 Settings 转换为 map[string]any。
// 仅包含非 nil（已设置）的字段，nil 字段不会出现在结果中。
func (ss *Settings) ToMap() map[string]any {
	if ss == nil {
		return nil
	}

	m := make(map[string]any)

	// 并发控制
	if ss.ConcurrentRequests != nil {
		m["CONCURRENT_REQUESTS"] = *ss.ConcurrentRequests
	}
	if ss.ConcurrentRequestsPerDomain != nil {
		m["CONCURRENT_REQUESTS_PER_DOMAIN"] = *ss.ConcurrentRequestsPerDomain
	}
	if ss.ConcurrentItems != nil {
		m["CONCURRENT_ITEMS"] = *ss.ConcurrentItems
	}

	// 下载配置
	if ss.DownloadDelay != nil {
		m["DOWNLOAD_DELAY"] = *ss.DownloadDelay
	}
	if ss.DownloadTimeout != nil {
		m["DOWNLOAD_TIMEOUT"] = *ss.DownloadTimeout
	}
	if ss.RandomizeDownloadDelay != nil {
		m["RANDOMIZE_DOWNLOAD_DELAY"] = *ss.RandomizeDownloadDelay
	}

	// 重试配置
	if ss.RetryEnabled != nil {
		m["RETRY_ENABLED"] = *ss.RetryEnabled
	}
	if ss.RetryTimes != nil {
		m["RETRY_TIMES"] = *ss.RetryTimes
	}
	if ss.RetryHTTPCodes != nil {
		m["RETRY_HTTP_CODES"] = ss.RetryHTTPCodes
	}

	// 重定向配置
	if ss.RedirectEnabled != nil {
		m["REDIRECT_ENABLED"] = *ss.RedirectEnabled
	}
	if ss.RedirectMaxTimes != nil {
		m["REDIRECT_MAX_TIMES"] = *ss.RedirectMaxTimes
	}

	// 深度控制
	if ss.DepthLimit != nil {
		m["DEPTH_LIMIT"] = *ss.DepthLimit
	}

	// 日志配置
	if ss.LogLevel != nil {
		m["LOG_LEVEL"] = *ss.LogLevel
	}

	// 调度器配置
	if ss.SchedulerDebug != nil {
		m["SCHEDULER_DEBUG"] = *ss.SchedulerDebug
	}

	// 统计配置
	if ss.StatsDump != nil {
		m["STATS_DUMP"] = *ss.StatsDump
	}

	// UserAgent
	if ss.UserAgent != nil {
		m["USER_AGENT"] = *ss.UserAgent
	}

	// 扩展配置
	for k, v := range ss.Extra {
		m[k] = v
	}

	return m
}

// ============================================================================
// 辅助函数 — 快速创建指针值
// ============================================================================

// IntPtr 返回 int 值的指针。
func IntPtr(v int) *int { return &v }

// StringPtr 返回 string 值的指针。
func StringPtr(v string) *string { return &v }

// BoolPtr 返回 bool 值的指针。
func BoolPtr(v bool) *bool { return &v }

// DurationPtr 返回 time.Duration 值的指针。
func DurationPtr(v time.Duration) *time.Duration { return &v }
