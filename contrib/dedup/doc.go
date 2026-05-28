// Package dedup 提供高级去重策略，是 scrapy-go 框架的 contrib 独立扩展模块。
//
// 本模块作为可选组件提供以下能力：
//
//   - URL 规范化去重：对查询参数排序，移除 utm_*、fbclid 等追踪参数
//   - SimHash 近似去重：基于内容指纹和汉明距离阈值识别相似文章
//   - 组合去重策略：将多个 scheduler.DupeFilter 链式组合，任一策略命中即过滤
//   - 零侵入：未引入此模块时主模块不依赖任何高级去重相关代码
//
// # 与内置 RFPDupeFilter 的关系
//
// 内置 pkg/scheduler.RFPDupeFilter 基于 Method + URL + Body 的精确指纹去重，
// 适合通用请求级去重。本模块在此基础上面向复杂爬取场景增强：
// URLCanonicalDupeFilter 会在计算 URL 指纹前移除常见追踪参数，避免同一页面因
// utm_source、fbclid 等统计参数不同而重复入队；SimHashDupeFilter 则通过请求
// Meta 中的内容字段做近似内容去重，适用于新闻、文章等内容相似但 URL 不同的场景。
//
// # 核心组件
//
//   - URLCanonicalDupeFilter：实现 scheduler.DupeFilter 的 URL 规范化去重过滤器
//   - SimHashDupeFilter：实现 scheduler.DupeFilter 的内容近似去重过滤器
//   - CompositeDupeFilter：组合多个 scheduler.DupeFilter，按 OR 语义过滤重复请求
//   - NewDupeFilter：按 Options 创建默认高级组合过滤器
//
// # 使用示例
//
//	filter, err := dedup.NewDupeFilter(dedup.DefaultOptions())
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	sched := scheduler.NewDefaultScheduler(
//	    scheduler.WithDupeFilter(filter),
//	)
//
// 若需要启用 SimHash 内容近似去重，请在请求入队前将待比较内容写入
// Request.Meta[dedup.MetaContentKey]。未提供内容时，SimHash 策略会自动跳过，
// 不影响 URL 去重链路。
package dedup
