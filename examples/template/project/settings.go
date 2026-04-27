// scrapy-go settings 模板
//
// 对齐 Scrapy 的 settings.py.tmpl
// 本文件包含 scrapy-go 项目中常用的配置项。
// 用户可以直接复制本文件到自己的项目中，根据需要取消注释并修改配置值。
//
// 更多配置项请参考 pkg/settings/defaults.go 中的完整默认值列表。
package project

import (
	"time"

	"github.com/dplcz/scrapy-go/pkg/spider"
)

// NewCustomSettings 返回 Spider 级别的自定义配置。
// 对齐 Scrapy 的 custom_settings 字典。
//
// 用法：在 Spider 的 CustomSettings() 方法中返回此函数的结果。
//
//	func (s *MySpider) CustomSettings() *spider.Settings {
//	    return NewCustomSettings()
//	}
func NewCustomSettings() *spider.Settings {
	return &spider.Settings{
		// ====================================================================
		// 并发与限速
		// ====================================================================

		// 全局最大并发请求数（默认 16）
		// 对齐 Scrapy: CONCURRENT_REQUESTS = 16
		ConcurrentRequests: spider.IntPtr(16),

		// 每个域名的最大并发请求数（默认 8）
		// 对齐 Scrapy: CONCURRENT_REQUESTS_PER_DOMAIN = 8
		ConcurrentRequestsPerDomain: spider.IntPtr(8),

		// 最大并发 Item 处理数（默认 100）
		// 对齐 Scrapy: CONCURRENT_ITEMS = 100
		// ConcurrentItems: spider.IntPtr(100),

		// 下载延迟（默认 0，无延迟）
		// 对齐 Scrapy: DOWNLOAD_DELAY = 0
		DownloadDelay: spider.DurationPtr(0),

		// 是否随机化下载延迟（默认 true）
		// 启用后，实际延迟为 [0.5 * delay, 1.5 * delay] 之间的随机值
		// 对齐 Scrapy: RANDOMIZE_DOWNLOAD_DELAY = True
		// RandomizeDownloadDelay: spider.BoolPtr(true),

		// ====================================================================
		// 下载配置
		// ====================================================================

		// 下载超时（默认 180s）
		// 对齐 Scrapy: DOWNLOAD_TIMEOUT = 180
		// DownloadTimeout: spider.DurationPtr(180 * time.Second),

		// 自定义 User-Agent
		// 对齐 Scrapy: USER_AGENT = "scrapy-go/0.1.0"
		// UserAgent: spider.StringPtr("myproject (+http://www.yourdomain.com)"),

		// ====================================================================
		// 重试配置
		// ====================================================================

		// 是否启用重试（默认 true）
		// 对齐 Scrapy: RETRY_ENABLED = True
		// RetryEnabled: spider.BoolPtr(true),

		// 最大重试次数（默认 2）
		// 对齐 Scrapy: RETRY_TIMES = 2
		// RetryTimes: spider.IntPtr(2),

		// 需要重试的 HTTP 状态码列表
		// 对齐 Scrapy: RETRY_HTTP_CODES = [500, 502, 503, 504, 522, 524, 408, 429]
		// RetryHTTPCodes: []int{500, 502, 503, 504, 522, 524, 408, 429},

		// ====================================================================
		// 重定向配置
		// ====================================================================

		// 是否启用重定向（默认 true）
		// 对齐 Scrapy: REDIRECT_ENABLED = True
		// RedirectEnabled: spider.BoolPtr(true),

		// 最大重定向次数（默认 20）
		// 对齐 Scrapy: REDIRECT_MAX_TIMES = 20
		// RedirectMaxTimes: spider.IntPtr(20),

		// ====================================================================
		// 深度控制
		// ====================================================================

		// 爬取深度限制，0 表示无限制（默认 0）
		// 对齐 Scrapy: DEPTH_LIMIT = 0
		// DepthLimit: spider.IntPtr(0),

		// ====================================================================
		// 日志配置
		// ====================================================================

		// 日志级别：DEBUG、INFO、WARN、ERROR（默认 DEBUG）
		// 对齐 Scrapy: LOG_LEVEL = "DEBUG"
		LogLevel: spider.StringPtr("DEBUG"),

		// ====================================================================
		// 统计配置
		// ====================================================================

		// 是否在 Spider 关闭时输出统计信息（默认 true）
		// 对齐 Scrapy: STATS_DUMP = True
		// StatsDump: spider.BoolPtr(true),

		// ====================================================================
		// HTTP 代理
		// ====================================================================

		// 是否启用 HttpProxy 中间件（默认 true）
		// 启用后自动读取环境变量 http_proxy/https_proxy 设置代理
		// 也可通过 Request.Meta["proxy"] 设置请求级代理
		// 对齐 Scrapy: HTTPPROXY_ENABLED = True
		// HttpProxyEnabled: spider.BoolPtr(true),

		// ====================================================================
		// 下载器统计
		// ====================================================================

		// 是否启用下载器统计中间件（默认 true）
		// 统计请求数、响应数、字节数、状态码分布、异常类型等
		// 对齐 Scrapy: DOWNLOADER_STATS = True
		// DownloaderStats: spider.BoolPtr(true),
	}
}

// 以下是 time 包的引用，防止未使用的导入错误
var _ = time.Second
