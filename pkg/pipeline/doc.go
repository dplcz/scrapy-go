// Package pipeline 定义了 scrapy-go 框架的 Item Pipeline 接口和管理器。
//
// # 概述
//
// pipeline 包负责处理 Spider 产出的数据项（Item），支持数据清洗、验证、
// 去重和持久化等操作。Pipeline 按优先级顺序串行处理每个 Item。
// 对应 Scrapy Python 版本中 scrapy.pipelines 模块的功能。
//
// # 核心类型
//
// 本包提供以下核心类型：
//   - [ItemPipeline]：Pipeline 接口，定义 Open/Close/ProcessItem 方法
//   - [Manager]：Pipeline 管理器，按优先级顺序调用 Pipeline 链
//   - [TypedItemPipeline]：泛型 Pipeline 接口（编译期类型约束）
//   - [TypedPipeline]：泛型 Pipeline 到 ItemPipeline 的适配器
//   - [CrawlerAwarePipeline]：可选接口，Pipeline 可获取 Crawler 引用
//   - [Entry]：带优先级的 Pipeline 条目
//
// # 使用方式
//
// 实现基本 Pipeline：
//
//	type CleanPipeline struct{}
//
//	func (p *CleanPipeline) Open(ctx context.Context) error  { return nil }
//	func (p *CleanPipeline) Close(ctx context.Context) error { return nil }
//	func (p *CleanPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
//	    adapter := item.Adapt(item)
//	    // 清洗数据...
//	    return item, nil
//	}
//
// 注册到 Crawler：
//
//	c := crawler.New()
//	c.AddPipeline(&CleanPipeline{}, "CleanPipeline", 300)
//
// # 泛型 Pipeline（TypedPipeline）
//
// Go 1.18+ 泛型允许在编译期约束 Item 类型，消除运行时类型断言：
//
//	type BookPipeline struct{}
//
//	func (p *BookPipeline) Open(ctx context.Context) error  { return nil }
//	func (p *BookPipeline) Close(ctx context.Context) error { return nil }
//	func (p *BookPipeline) ProcessItem(ctx context.Context, book *Book) (*Book, error) {
//	    book.Title = strings.TrimSpace(book.Title)
//	    return book, nil
//	}
//
//	// 注册：通过 TypedPipeline 适配为 ItemPipeline
//	typed := pipeline.NewTypedPipeline[*Book](&BookPipeline{})
//	c.AddPipeline(typed, "BookPipeline", 300)
//
// 当 Item 类型不匹配时，[TypedPipeline] 会跳过处理（透传 Item），
// 允许多个 TypedPipeline 共存于同一 Manager 中。
//
// # CrawlerAwarePipeline
//
// Pipeline 可实现 [CrawlerAwarePipeline] 接口以在初始化时获取框架组件：
//
//	type MyPipeline struct {
//	    stats stats.Collector
//	}
//
//	func (p *MyPipeline) FromCrawler(c pipeline.Crawler) error {
//	    p.stats = c.GetStats()
//	    return nil
//	}
//
// Manager.Open 会在调用 Pipeline.Open 之前调用 FromCrawler。
//
// # 处理流程
//
// Manager.ProcessItem 的处理流程：
//  1. 若启用了 Item 验证，对 struct Item 执行 Validate（填充默认值 + 校验 required）
//  2. 按优先级顺序调用每个 Pipeline 的 ProcessItem
//  3. Pipeline 返回 [errors.ErrDropItem] → 停止后续处理，发出 ItemDropped 信号
//  4. Pipeline 返回其他错误 → 发出 ItemError 信号
//  5. 所有 Pipeline 处理成功 → 发出 ItemScraped 信号
//
// # 信号系统集成
//
// Manager 在处理 Item 时发送以下信号：
//   - ItemScraped：Item 成功通过所有 Pipeline
//   - ItemDropped：Item 被某个 Pipeline 丢弃
//   - ItemError：Pipeline 处理 Item 时发生错误
//
// # 与 Scrapy 的差异
//
//   - 使用 [TypedPipeline] 泛型替代 Python 的 isinstance() 运行时类型检查
//   - 使用 [CrawlerAwarePipeline] 接口替代 Python 的 from_crawler 类方法
//   - Pipeline 优先级数值小的先执行（与 Scrapy 一致）
//   - 使用 Go 的 error 返回值替代 Python 的 DropItem 异常
//   - Manager 内部不做并发控制（由 Scraper 的 CONCURRENT_ITEMS 信号量控制）
//   - 支持可选的 Item 验证（struct tag 驱动的 required/default 校验）
package pipeline
