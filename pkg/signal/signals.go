// Package signal 定义了 scrapy-go 框架的信号/事件系统。
//
// 信号用于框架内部组件之间的松耦合通信，对应 Scrapy Python 版本中
// scrapy.signals 模块和 scrapy.signalmanager 模块的功能。
package signal

// Signal 表示一个信号类型。
// 使用 iota 枚举所有框架内置信号。
type Signal int

const (
	// EngineStarted 在引擎启动后发出。
	EngineStarted Signal = iota

	// EngineStopped 在引擎停止后发出。
	EngineStopped

	// SchedulerEmpty 在调度器队列为空时发出。
	SchedulerEmpty

	// SpiderOpened 在 Spider 打开后发出。
	SpiderOpened

	// SpiderIdle 在 Spider 空闲时发出（无待处理请求、无活跃下载）。
	// 信号处理器可以返回 ErrDontCloseSpider 来阻止 Spider 关闭。
	SpiderIdle

	// SpiderClosed 在 Spider 关闭后发出。
	SpiderClosed

	// SpiderError 在 Spider 回调抛出异常时发出。
	SpiderError

	// RequestScheduled 在请求被调度器接受入队时发出。
	RequestScheduled

	// RequestDropped 在请求被丢弃时发出（如被去重过滤）。
	RequestDropped

	// RequestReachedDownloader 在请求到达下载器时发出。
	RequestReachedDownloader

	// RequestLeftDownloader 在请求离开下载器时发出（下载完成或失败）。
	RequestLeftDownloader

	// ResponseReceived 在引擎收到下载器返回的响应时发出。
	ResponseReceived

	// ResponseDownloaded 在 HTTP 响应下载完成时发出。
	ResponseDownloaded

	// HeadersReceived 在收到 HTTP 响应头时发出。
	HeadersReceived

	// BytesReceived 在收到响应字节数据时发出。
	BytesReceived

	// ItemScraped 在 Item 成功通过所有 Pipeline 处理后发出。
	ItemScraped

	// ItemDropped 在 Item 被 Pipeline 丢弃时发出。
	ItemDropped

	// ItemError 在 Pipeline 处理 Item 时发生错误时发出。
	ItemError
)

// String 返回信号的字符串表示，便于日志输出。
func (s Signal) String() string {
	switch s {
	case EngineStarted:
		return "engine_started"
	case EngineStopped:
		return "engine_stopped"
	case SchedulerEmpty:
		return "scheduler_empty"
	case SpiderOpened:
		return "spider_opened"
	case SpiderIdle:
		return "spider_idle"
	case SpiderClosed:
		return "spider_closed"
	case SpiderError:
		return "spider_error"
	case RequestScheduled:
		return "request_scheduled"
	case RequestDropped:
		return "request_dropped"
	case RequestReachedDownloader:
		return "request_reached_downloader"
	case RequestLeftDownloader:
		return "request_left_downloader"
	case ResponseReceived:
		return "response_received"
	case ResponseDownloaded:
		return "response_downloaded"
	case HeadersReceived:
		return "headers_received"
	case BytesReceived:
		return "bytes_received"
	case ItemScraped:
		return "item_scraped"
	case ItemDropped:
		return "item_dropped"
	case ItemError:
		return "item_error"
	default:
		return "unknown_signal"
	}
}

// Handler 定义信号处理函数类型。
// params 包含信号携带的上下文参数，不同信号携带不同的参数。
// 返回 error 表示处理失败，框架会捕获并记录日志。
// 返回 ErrDontCloseSpider 可以阻止 Spider 关闭（仅在 SpiderIdle 信号中有效）。
// 返回 ErrCloseSpider 可以请求关闭 Spider。
type Handler func(params map[string]any) error
