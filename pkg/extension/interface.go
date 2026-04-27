// Package extension 定义了 scrapy-go 框架的扩展（Extension）系统。
//
// 扩展通过监听框架信号来实现自定义逻辑，如统计收集、内存监控、条件关闭等。
// 对应 Scrapy Python 版本中 scrapy.extensions 模块和 scrapy.extension 模块的功能。
package extension

import (
	"context"
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
