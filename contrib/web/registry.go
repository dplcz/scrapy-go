package web

import (
	"fmt"
	"sort"
	"sync"

	"github.com/dplcz/scrapy-go/pkg/spider"
)

// SpiderFactory 是 Spider 工厂函数类型。
// 每次调用应返回一个全新的 Spider 实例。
type SpiderFactory func() spider.Spider

// CrawlerConfigurator 是可选的 Crawler 配置回调。
// 在每次启动 Spider 时，创建新 Crawler 后、调用 Crawl 之前调用，
// 允许用户为 Crawler 注册 Pipeline、中间件、扩展等。
type CrawlerConfigurator func(c CrawlerConfig)

// CrawlerConfig 定义 Crawler 配置接口，限制用户只能进行安全的配置操作。
// 避免直接暴露 *crawler.Crawler 导致误用。
type CrawlerConfig interface {
	// AddPipeline 注册一个自定义 Item Pipeline。
	AddPipeline(p interface{}, name string, priority int)

	// AddExtension 注册一个自定义扩展。
	AddExtension(ext interface{}, name string, priority int)
}

// spiderEntry 存储 Spider 注册信息。
type spiderEntry struct {
	name         string
	factory      SpiderFactory
	configurator CrawlerConfigurator
}

// Registry 是 Spider 注册表，按名称管理 Spider 工厂函数。
//
// 线程安全：所有公共方法均可被多个 goroutine 安全调用。
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*spiderEntry
}

// NewRegistry 创建一个新的 Spider 注册表。
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]*spiderEntry),
	}
}

// Register 注册一个 Spider 工厂函数。
//
// name 是 Spider 的唯一标识名称，factory 是创建 Spider 实例的工厂函数。
// 可选的 configurator 在每次启动时配置新创建的 Crawler（注册 Pipeline、扩展等）。
//
// 如果 name 已存在，将覆盖之前的注册。
// name 和 factory 不能为空，否则 panic。
func (r *Registry) Register(name string, factory SpiderFactory, configurator ...CrawlerConfigurator) {
	if name == "" {
		panic("web.Registry: spider name must not be empty")
	}
	if factory == nil {
		panic("web.Registry: spider factory must not be nil")
	}

	entry := &spiderEntry{
		name:    name,
		factory: factory,
	}
	if len(configurator) > 0 && configurator[0] != nil {
		entry.configurator = configurator[0]
	}

	r.mu.Lock()
	r.entries[name] = entry
	r.mu.Unlock()
}

// Unregister 移除指定名称的 Spider 注册。
// 如果名称不存在，不执行任何操作。
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	delete(r.entries, name)
	r.mu.Unlock()
}

// Get 获取指定名称的 Spider 注册信息。
// 如果名称不存在，返回 nil 和 false。
func (r *Registry) Get(name string) (SpiderFactory, CrawlerConfigurator, bool) {
	r.mu.RLock()
	entry, ok := r.entries[name]
	r.mu.RUnlock()
	if !ok {
		return nil, nil, false
	}
	return entry.factory, entry.configurator, true
}

// Has 检查指定名称的 Spider 是否已注册。
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	_, ok := r.entries[name]
	r.mu.RUnlock()
	return ok
}

// Names 返回所有已注册的 Spider 名称（按字母排序）。
func (r *Registry) Names() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

// Len 返回已注册的 Spider 数量。
func (r *Registry) Len() int {
	r.mu.RLock()
	n := len(r.entries)
	r.mu.RUnlock()
	return n
}

// MustGet 获取指定名称的 Spider 工厂函数，不存在时 panic。
func (r *Registry) MustGet(name string) SpiderFactory {
	factory, _, ok := r.Get(name)
	if !ok {
		panic(fmt.Sprintf("web.Registry: spider %q not registered", name))
	}
	return factory
}

// ============================================================================
// 声明式 Spider 配置（预留 — P5-005h 实现）
// ============================================================================

// SpiderSpec 是声明式 Spider 配置，通过 JSON 描述爬取规则。
//
// 用户通过 POST /api/spiders/register 提交 SpiderSpec，
// 框架将其转换为 CrawlSpider + Rule 实例并注册到 Registry。
// 无需编写 Go 代码，覆盖 80% 的常见爬取场景（列表页 → 详情页模式）。
//
// 预留结构体，完整实现将在 P5-005h（声明式 Spider 配置引擎）中交付。
type SpiderSpec struct {
	// Name 是 Spider 的唯一标识名称（必填）。
	Name string `json:"name"`

	// StartURLs 是初始爬取 URL 列表（必填，至少一个）。
	StartURLs []string `json:"start_urls"`

	// AllowedDomains 是允许爬取的域名列表（可选）。
	// 为空时允许所有域名。
	AllowedDomains []string `json:"allowed_domains,omitempty"`

	// Rules 是爬取规则列表（可选）。
	// 每条规则定义一个链接提取器和对应的处理方式。
	// 映射到 CrawlSpider.Rules。
	Rules []RuleSpec `json:"rules,omitempty"`

	// ItemSchemas 定义每个回调名称对应的 Item 提取规则（可选）。
	// key 是回调名称（对应 RuleSpec.Callback），value 是字段提取规则。
	// 框架根据 schema 自动从响应中提取数据并生成 Item。
	ItemSchemas map[string]ItemSchema `json:"item_schemas,omitempty"`

	// Settings 是可选的 Spider 级别配置覆盖。
	// key 是配置项名称（如 "CONCURRENT_REQUESTS"），value 是配置值。
	Settings map[string]any `json:"settings,omitempty"`
}

// RuleSpec 是声明式爬取规则，映射到 spider.Rule。
type RuleSpec struct {
	// LinkExtractor 定义链接提取器的配置。
	// 映射到 linkextractor.HTMLLinkExtractor 的选项。
	LinkExtractor LinkExtractorSpec `json:"link_extractor"`

	// Callback 是匹配链接的回调名称（可选）。
	// 对应 ItemSchemas 中的 key，框架根据 schema 自动提取 Item。
	// 为空时仅跟踪链接，不提取数据。
	Callback string `json:"callback,omitempty"`

	// Follow 控制是否从匹配此规则的响应中继续提取链接（可选）。
	// 为 nil 时：有 Callback 默认 false，无 Callback 默认 true。
	Follow *bool `json:"follow,omitempty"`
}

// LinkExtractorSpec 是声明式链接提取器配置，映射到 HTMLLinkExtractor 选项。
type LinkExtractorSpec struct {
	// Allow 是允许的 URL 正则表达式列表。
	// 映射到 linkextractor.WithAllow()。
	Allow []string `json:"allow,omitempty"`

	// Deny 是拒绝的 URL 正则表达式列表。
	// 映射到 linkextractor.WithDeny()。
	Deny []string `json:"deny,omitempty"`

	// AllowDomains 是允许的域名列表。
	// 映射到 linkextractor.WithAllowDomains()。
	AllowDomains []string `json:"allow_domains,omitempty"`

	// DenyDomains 是拒绝的域名列表。
	// 映射到 linkextractor.WithDenyDomains()。
	DenyDomains []string `json:"deny_domains,omitempty"`

	// RestrictCSS 是限制链接提取范围的 CSS 选择器列表。
	// 映射到 linkextractor.WithRestrictCSS()。
	RestrictCSS []string `json:"restrict_css,omitempty"`

	// RestrictXPath 是限制链接提取范围的 XPath 表达式列表。
	// 映射到 linkextractor.WithRestrictXPath()。
	RestrictXPath []string `json:"restrict_xpath,omitempty"`

	// Tags 是要提取链接的 HTML 标签列表（默认 ["a", "area"]）。
	// 映射到 linkextractor.WithTags()。
	Tags []string `json:"tags,omitempty"`

	// Attrs 是要提取链接的 HTML 属性列表（默认 ["href"]）。
	// 映射到 linkextractor.WithAttrs()。
	Attrs []string `json:"attrs,omitempty"`
}

// ItemSchema 定义一个 Item 的字段提取规则。
// key 是字段名称，value 是提取规则。
type ItemSchema map[string]FieldExtractor

// FieldExtractor 定义单个字段的提取方式。
// CSS 和 XPath 二选一；Value 用于特殊值（如 "_response_url"）。
type FieldExtractor struct {
	// CSS 是 CSS 选择器表达式（与 XPath 二选一）。
	// 支持 ::text 和 ::attr(name) 伪元素。
	CSS string `json:"css,omitempty"`

	// XPath 是 XPath 表达式（与 CSS 二选一）。
	XPath string `json:"xpath,omitempty"`

	// Value 是特殊值标识（可选）。
	// 支持的特殊值：
	//   - "_response_url": 当前响应的 URL
	//   - "_timestamp": 当前时间戳
	//   - 其他字符串: 作为字面量值
	Value string `json:"value,omitempty"`

	// Regex 是对提取结果进行正则匹配的表达式（可选）。
	// 应用于 CSS/XPath 提取的结果之上。
	Regex string `json:"regex,omitempty"`

	// Default 是提取失败时的默认值（可选）。
	Default string `json:"default,omitempty"`
}
