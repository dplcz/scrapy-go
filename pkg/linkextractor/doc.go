// Package linkextractor 提供链接提取器接口和实现。
//
// # 概述
//
// linkextractor 包用于从 HTML 响应中提取链接，是 CrawlSpider 自动爬取的核心组件。
// 对应 Scrapy Python 版本中 scrapy.linkextractors 模块的功能。
//
// # 核心类型
//
//   - [LinkExtractor]：链接提取器接口，定义 ExtractLinks 方法
//   - [HTMLLinkExtractor]：基于 goquery 的 HTML 链接提取器实现
//   - [Link]：提取的链接结构体，包含 URL、锚文本、Fragment 和 NoFollow 标记
//
// # 使用方式
//
// 创建链接提取器并从响应中提取链接：
//
//	// 提取所有链接
//	le := linkextractor.NewHTMLLinkExtractor()
//	links := le.ExtractLinks(response)
//
//	// 使用过滤规则
//	le := linkextractor.NewHTMLLinkExtractor(
//	    linkextractor.WithAllow(`/page/\d+`),           // 只提取分页链接
//	    linkextractor.WithDeny(`/login`, `/logout`),    // 排除登录/登出
//	    linkextractor.WithAllowDomains("example.com"),  // 限制域名
//	    linkextractor.WithRestrictCSS("nav.pagination"),// 限制提取范围
//	)
//	links := le.ExtractLinks(response)
//
// # 过滤规则
//
// [HTMLLinkExtractor] 支持多种过滤规则（按优先级从高到低）：
//
//  1. allow — URL 正则白名单（空列表表示允许所有）
//  2. deny — URL 正则黑名单
//  3. allowDomains — 域名白名单（支持子域名匹配）
//  4. denyDomains — 域名黑名单
//  5. denyExtensions — 文件扩展名黑名单（默认排除图片/视频/文档等）
//  6. restrictText — 锚文本正则过滤
//  7. restrictCSS / restrictXPath — 限制提取范围到特定 HTML 区域
//
// # 与 CrawlSpider 集成
//
// linkextractor 通常与 CrawlSpider 的 Rule 规则配合使用：
//
//	rules := []crawlspider.Rule{
//	    {
//	        LinkExtractor: linkextractor.NewHTMLLinkExtractor(
//	            linkextractor.WithAllow(`/category/`),
//	        ),
//	        Callback: spider.ParseCategory,
//	        Follow:   true,
//	    },
//	}
//
// # 配置选项
//
// 使用 Functional Options 模式配置 [HTMLLinkExtractor]：
//   - [WithAllow] / [WithDeny]：URL 正则过滤
//   - [WithAllowDomains] / [WithDenyDomains]：域名过滤
//   - [WithRestrictCSS] / [WithRestrictXPath]：限制提取范围
//   - [WithTags] / [WithAttrs]：自定义扫描的标签和属性
//   - [WithUnique]：链接去重（默认启用）
//   - [WithStripFragment]：去除 URL fragment（默认启用）
//   - [WithDenyExtensions]：文件扩展名过滤
//   - [WithRestrictText]：锚文本过滤
//   - [WithCanonicalize]：URL 规范化
//
// # 与 Scrapy 的差异
//
//   - 重命名为 HTMLLinkExtractor（Scrapy 的 LxmlLinkExtractor 基于 lxml，Go 版本基于 goquery）
//   - 使用 Functional Options 模式替代 Python 的构造函数参数
//   - 使用 Go 标准库 regexp 替代 Python re 模块
//   - 默认的 denyExtensions 与 Scrapy 的 IGNORED_EXTENSIONS 保持一致
//   - [Link] 是值类型（struct），而非引用类型
//   - [HTMLLinkExtractor.Matches] 方法可在不解析 HTML 的情况下快速判断 URL 是否匹配
package linkextractor
