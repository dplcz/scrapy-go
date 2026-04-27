// scrapy-go pipelines 模板
// 在此处定义 Pipeline，负责处理 Spider 产出的数据项。
// 支持数据清洗、验证、去重和持久化等操作。
//
// 注册方式：
//
//	c := crawler.NewDefault()
//	c.AddPipeline(&MyPipeline{}, "MyPipeline", 300)
//	c.Run(ctx, sp)
//
// Pipeline 按优先级排序，优先级数值小的先执行。
package project

import (
	"context"
)

// MyPipeline 是自定义 Item Pipeline 示例。
//
// 需要实现 pipeline.ItemPipeline 接口的三个方法：
//   - Open(ctx)          — Spider 打开时调用，初始化资源
//   - Close(ctx)         — Spider 关闭时调用，释放资源
//   - ProcessItem(ctx, item) — 处理单个 Item
type MyPipeline struct{}

// Open 在 Spider 打开时调用，用于初始化资源。
// 例如：打开数据库连接、创建输出文件等。
func (p *MyPipeline) Open(ctx context.Context) error {
	return nil
}

// Close 在 Spider 关闭时调用，用于释放资源。
// 例如：关闭数据库连接、刷新缓冲区等。
func (p *MyPipeline) Close(ctx context.Context) error {
	return nil
}

// ProcessItem 处理单个 Item。
//
// 返回值：
//   - (item, nil)              — 继续传递给下一个 Pipeline
//   - (nil, ErrDropItem)       — 丢弃该 Item，后续 Pipeline 不再处理
//   - (nil, error)             — 处理出错
//
// 丢弃 Item 示例：
//
//	import serrors "github.com/dplcz/scrapy-go/pkg/errors"
//	return nil, serrors.ErrDropItem
func (p *MyPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
	return item, nil
}
