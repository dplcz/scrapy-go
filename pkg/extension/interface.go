package extension

import (
	"context"
	"log/slog"

	"github.com/dplcz/scrapy-go/pkg/settings"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// Extension 定义扩展接口。
// 扩展在 Spider 打开时初始化，在 Spider 关闭时清理资源。
// 扩展通过信号系统监听框架事件，实现自定义逻辑。
//
// 生命周期：
//  1. Open — Spider 打开时调用，用于注册信号处理器和初始化资源
//  2. Close — Spider 关闭时调用，用于注销信号处理器和释放资源
type Extension interface {
	// Open 在 Spider 打开时调用。
	// 扩展应在此方法中注册信号处理器和初始化资源。
	// 返回 ErrNotConfigured 表示该扩展未配置，框架将跳过并记录警告日志。
	Open(ctx context.Context) error

	// Close 在 Spider 关闭时调用。
	// 扩展应在此方法中注销信号处理器和释放资源。
	Close(ctx context.Context) error
}

// BaseExtension 提供默认的空实现。
// 扩展可以嵌入此结构体，只覆盖需要的方法。
type BaseExtension struct{}

// Open 默认实现，不执行任何操作。
func (b *BaseExtension) Open(ctx context.Context) error {
	return nil
}

// Close 默认实现，不执行任何操作。
func (b *BaseExtension) Close(ctx context.Context) error {
	return nil
}

// ============================================================================
// CrawlerAwareExtension 可选接口
// ============================================================================

// Crawler 定义 Extension 可访问的 Crawler 能力子集。
//
// 对齐 Scrapy 的 `from_crawler(cls, crawler)` 中 Extension 可访问的 Crawler 属性。
type Crawler interface {
	GetSettings() *settings.Settings
	GetStats() stats.Collector
	GetSignals() *signal.Manager
	GetLogger() *slog.Logger
}

// CrawlerAwareExtension 是一个可选接口，Extension 可以实现此接口以在 Open 之前
// 获取 Crawler 引用，从而访问最新的 Settings、Stats、Signals、Logger 等框架组件。
//
// 若 Extension 实现了此接口，Manager.Open 会在调用 Extension.Open 之前
// 调用 FromCrawler 并传入 Crawler 引用。
//
// 这解决了扩展在构造时绑定 Signals/Logger 引用，但 Crawler 在 Run 时可能
// 因 Spider CustomSettings 重建这些组件导致引用失效的问题。
//
// 用法：
//
//	func (e *MyExtension) FromCrawler(c extension.Crawler) error {
//	    e.signals = c.GetSignals()
//	    e.logger = c.GetLogger()
//	    return nil
//	}
type CrawlerAwareExtension interface {
	Extension
	// FromCrawler 在 Open 之前调用，传入 Crawler 引用。
	// 返回 error 将阻止该 Extension 的 Open 调用。
	FromCrawler(c Crawler) error
}
