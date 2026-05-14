// Package spider 定义了 scrapy-go 框架的 Spider 接口和基础实现。
//
// # 概述
//
// spider 包是用户与 scrapy-go 框架交互的核心接口层，提供：
//   - [Spider] 接口：定义爬虫的核心契约（名称、初始请求、响应解析、配置）
//   - [Base] 结构体：Spider 接口的默认实现，用户可嵌入并覆盖
//   - [CrawlSpider]：基于规则的自动爬取 Spider，通过 [Rule] 定义链接跟踪策略
//   - [Settings] 结构体：类型安全的 Spider 级别配置
//
// 对应 Scrapy Python 版本中 scrapy.spiders 模块的功能。
//
// # 架构定位
//
// Spider 是 scrapy-go 框架中用户代码的入口点：
//
//	┌─────────────────────────────────────────────────────────┐
//	│                    用户代码                              │
//	│  (实现 Spider 接口 / 嵌入 Base / 使用 CrawlSpider)      │
//	└────────────────────────┬────────────────────────────────┘
//	                         │
//	                         ▼
//	┌─────────────────────────────────────────────────────────┐
//	│                    Crawler                               │
//	│  (读取 CustomSettings、调用 Start、注入 Engine)          │
//	└────────────────────────┬────────────────────────────────┘
//	                         │
//	                         ▼
//	┌─────────────────────────────────────────────────────────┐
//	│                    Engine                                │
//	│  (消费 Start() 产出的请求、调用 Parse 处理响应)          │
//	└─────────────────────────────────────────────────────────┘
//
// # Spider 接口
//
// [Spider] 接口定义了爬虫的核心契约：
//   - Name()：返回爬虫的唯一名称
//   - Start(ctx)：返回初始请求/Item 的 channel
//   - Parse(ctx, response)：默认的响应回调函数
//   - CustomSettings()：返回 Spider 级别的配置覆盖
//   - Closed(reason)：Spider 关闭时的清理回调
//
// # Base 默认实现
//
// [Base] 提供 Spider 接口的默认实现，用户只需嵌入并覆盖需要的方法：
//
//	type MySpider struct {
//	    spider.Base
//	}
//
//	func NewMySpider() *MySpider {
//	    return &MySpider{
//	        Base: spider.Base{
//	            SpiderName: "myspider",
//	            StartURLs:  []string{"https://example.com"},
//	        },
//	    }
//	}
//
//	func (s *MySpider) Parse(ctx context.Context, resp *http.Response) ([]spider.Output, error) {
//	    // 解析响应，返回 Item 或新请求
//	    return []spider.Output{
//	        {Item: map[string]any{"title": resp.CSS("h1").Text()}},
//	    }, nil
//	}
//
// Base.Start() 的默认实现为 StartURLs 中的每个 URL 创建 GET 请求（DontFilter=true）。
//
// # CrawlSpider
//
// [CrawlSpider] 是基于规则的自动爬取 Spider，通过定义 [Rule] 规则列表，
// 自动从响应中提取链接并跟踪：
//
//	cs := &spider.CrawlSpider{
//	    Base: spider.Base{
//	        SpiderName: "crawler",
//	        StartURLs:  []string{"https://example.com"},
//	    },
//	    Rules: []spider.Rule{
//	        {
//	            LinkExtractor: linkextractor.NewHTMLLinkExtractor(
//	                linkextractor.WithAllow(`/page/\d+`),
//	            ),
//	            Callback: parseItem,
//	        },
//	        {
//	            LinkExtractor: linkextractor.NewHTMLLinkExtractor(
//	                linkextractor.WithAllow(`/category/`),
//	            ),
//	            Follow: spider.BoolPtr(true),
//	        },
//	    },
//	}
//
// CrawlSpider 的核心机制：
//   - 多条规则按顺序匹配，同一链接只被第一个匹配的规则处理
//   - Rule.Follow 控制是否从匹配响应中继续提取链接
//   - Rule.ProcessLinks 可在链接提取后进行过滤/修改
//   - Rule.ProcessRequest 可在请求生成后进行修改
//
// # Output 类型
//
// [Output] 是 Spider 回调的统一返回类型，可以是 Request 或 Item：
//
//	// 产出新请求
//	spider.Output{Request: req}
//
//	// 产出数据项
//	spider.Output{Item: myItem}
//
// # Settings 类型安全配置
//
// [Settings] 提供类型安全的 Spider 级别配置，替代 map[string]any：
//
//	func (s *MySpider) CustomSettings() *spider.Settings {
//	    return &spider.Settings{
//	        ConcurrentRequests: spider.IntPtr(4),
//	        DownloadDelay:      spider.DurationPtr(time.Second),
//	        LogLevel:           spider.StringPtr("INFO"),
//	    }
//	}
//
// 所有字段均为指针类型，nil 表示不覆盖框架默认值。
// 辅助函数 [IntPtr]、[StringPtr]、[BoolPtr]、[DurationPtr] 用于快速创建指针值。
//
// # 回调函数类型
//
// 回调函数类型的实际定义位于 pkg/http 包中（以打破循环依赖，TD-003 偿还），
// spider 包通过类型别名重新导出，用户代码可继续使用 spider.CallbackFunc / spider.ErrbackFunc：
//
//   - [CallbackFunc]：响应回调，接收 context 和 Response，返回 []Output
//   - [ErrbackFunc]：错误回调，接收 context、error 和原始 Request，返回 []Output
//
// Output 类型同样定义在 pkg/http 包中，spider 包通过类型别名重新导出。
//
// # 与 Scrapy 的差异
//
//   - 使用 Go 接口替代 Python 类继承（Spider 接口 + Base 嵌入）
//   - Start() 返回 channel 替代 Python 的 generator/iterator
//   - CrawlSpider 的 Callback/Errback 直接接受函数值，舍弃字符串方法名反射
//   - 使用 [Settings] 结构体替代 custom_settings 字典（编译期类型检查）
//   - 使用指针字段表示「未设置」语义（nil = 不覆盖），替代 Python 的 None
//   - Start() 中内置 panic recovery，防止用户代码 panic 导致进程崩溃
//   - 使用 sync.Once 确保 CrawlSpider 规则只编译一次
//
// # Panic 恢复
//
// Base.Start() 和 CrawlSpider.Start() 均内置 panic recovery。
// 如果用户代码在初始请求生成过程中发生 panic，框架会捕获并记录日志，
// 而不会导致整个进程崩溃。
package spider
