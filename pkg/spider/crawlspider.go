package spider

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/linkextractor"
)

// ProcessLinksFunc 定义链接处理函数类型。
// 接收提取的链接列表，返回处理后的链接列表。
// 可用于过滤、修改或重新排序链接。
type ProcessLinksFunc func(links []linkextractor.Link) []linkextractor.Link

// ProcessRequestFunc 定义请求处理函数类型。
// 接收由链接生成的请求和原始响应，返回处理后的请求。
// 返回 nil 表示丢弃该请求。
type ProcessRequestFunc func(request *shttp.Request, response *shttp.Response) *shttp.Request

// Rule 定义 CrawlSpider 的爬取规则。
// 每条规则包含一个链接提取器和相关的回调配置。
//
// 对应 Scrapy 的 scrapy.spiders.crawl.Rule 类。
// 与 Scrapy 不同的是，Go 版本的 Callback/Errback 直接接受函数值，
// 而非字符串方法名（舍弃 Scrapy 的字符串反射机制）。
type Rule struct {
	// LinkExtractor 是用于提取链接的提取器。
	// 如果为 nil，将使用默认的 HTMLLinkExtractor（提取所有链接）。
	LinkExtractor linkextractor.LinkExtractor

	// Callback 是匹配链接的响应回调函数。
	// 如果为 nil，响应不会被传递给回调，但仍会跟踪链接（如果 Follow 为 true）。
	Callback CallbackFunc

	// Errback 是下载错误的回调函数。
	Errback ErrbackFunc

	// CbKwargs 是传递给回调函数的额外参数。
	CbKwargs map[string]any

	// Follow 控制是否从匹配此规则的响应中继续提取链接。
	// 如果为 nil（未设置），当 Callback 为 nil 时默认为 true，否则为 false。
	// 这与 Scrapy 的行为一致：没有回调的规则默认跟踪链接。
	Follow *bool

	// ProcessLinks 是链接处理函数，在链接被提取后、生成请求前调用。
	// 可用于过滤或修改提取的链接。
	ProcessLinks ProcessLinksFunc

	// ProcessRequest 是请求处理函数，在请求生成后调用。
	// 可用于修改请求属性（如添加 Headers、设置 Meta 等）。
	// 返回 nil 表示丢弃该请求。
	ProcessRequest ProcessRequestFunc
}

// shouldFollow 返回此规则是否应跟踪链接。
func (r *Rule) shouldFollow() bool {
	if r.Follow != nil {
		return *r.Follow
	}
	// 默认行为：没有回调时跟踪链接
	return r.Callback == nil
}

// ============================================================================
// CrawlSpider 实现
// ============================================================================

// CrawlSpider 是基于规则的自动爬取 Spider。
// 通过定义一组 Rule 规则，CrawlSpider 自动从响应中提取链接并跟踪。
//
// 对应 Scrapy 的 scrapy.spiders.CrawlSpider 类。
//
// 与 Scrapy 的主要区别：
//   - Rule 的 Callback/Errback 直接接受函数值，而非字符串方法名
//   - parseWithRules 同步返回 []Output（舍弃 AsyncIterator）
//   - 使用 Go 的组合模式替代 Python 的类继承
//
// 用法：
//
//	TODO 初始化是否过于复杂
//	spider := &MyCrawlSpider{
//	    CrawlSpider: spider.CrawlSpider{
//	        Base: spider.Base{
//	            SpiderName: "myspider",
//	            StartURLs:  []string{"https://example.com"},
//	        },
//	        Rules: []spider.Rule{
//	            {
//	                LinkExtractor: linkextractor.NewHTMLLinkExtractor(
//	                    linkextractor.WithAllow(`/page/\d+`),
//	                ),
//	                Callback: myParseFunc,
//	            },
//	        },
//	    },
//	}
type CrawlSpider struct {
	Base

	// Rules 是爬取规则列表。
	// 多条规则按顺序匹配，同一链接只会被第一个匹配的规则处理。
	Rules []Rule

	// FollowLinks 控制是否全局启用链接跟踪。
	// 对应 Scrapy 的 CRAWLSPIDER_FOLLOW_LINKS 配置。
	// 默认为 true。
	FollowLinks *bool

	// ParseStartURL 是处理初始 URL 响应的回调函数。
	// 对应 Scrapy 的 CrawlSpider.parse_start_url 方法。
	// 如果为 nil，初始 URL 的响应仅用于提取链接。
	ParseStartURL CallbackFunc

	// ProcessResults 是处理回调结果的函数。
	// 对应 Scrapy 的 CrawlSpider.process_results 方法。
	// 如果为 nil，结果不做额外处理。
	ProcessResults func(response *shttp.Response, results []Output) []Output

	// compiledRules 是编译后的规则（内部使用）。
	compiledRules []compiledRule

	// initOnce 确保规则只编译一次。
	initOnce sync.Once

	// defaultLE 是默认的链接提取器（懒初始化）。
	defaultLE linkextractor.LinkExtractor
}

// compiledRule 是编译后的规则（内部使用）。
type compiledRule struct {
	rule  Rule
	index int
}

// Name 返回爬虫名称。
func (cs *CrawlSpider) Name() string {
	return cs.Base.Name()
}

// Start 返回初始请求的 channel。
// CrawlSpider 的初始请求使用内部回调 _parse 处理，
// 该回调会调用 ParseStartURL 并跟踪链接。
func (cs *CrawlSpider) Start(ctx context.Context) <-chan Output {
	cs.compileRules()

	ch := make(chan Output)
	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				if cs.Logger != nil {
					cs.Logger.Error("panic recovered in CrawlSpider.Start",
						"panic", r,
						"stack", stack,
					)
				}
			}
		}()
		for _, rawURL := range cs.StartURLs {
			req, err := shttp.NewRequest(rawURL,
				shttp.WithDontFilter(true),
				shttp.WithCallback(cs.internalParse),
			)
			if err != nil {
				if cs.Logger != nil {
					cs.Logger.Error("failed to create start request",
						"url", rawURL,
						"error", err,
					)
				}
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- Output{Request: req}:
			}
		}
	}()
	return ch
}

// Parse 是默认的响应回调。
// CrawlSpider 不直接使用此方法，而是通过 internalParse 处理。
// 用户不应覆盖此方法，而应使用 Rules 定义回调。
func (cs *CrawlSpider) Parse(ctx context.Context, response *shttp.Response) ([]Output, error) {
	return cs.internalParse(ctx, response)
}

// CustomSettings 返回 Spider 级别的配置。
func (cs *CrawlSpider) CustomSettings() *Settings {
	return cs.Base.CustomSettings()
}

// Closed 在 Spider 关闭时调用。
func (cs *CrawlSpider) Closed(reason string) {
	cs.Base.Closed(reason)
}

// ============================================================================
// 内部方法
// ============================================================================

// compileRules 编译规则列表。
func (cs *CrawlSpider) compileRules() {
	cs.initOnce.Do(func() {
		cs.compiledRules = make([]compiledRule, len(cs.Rules))
		for i, rule := range cs.Rules {
			// 如果规则没有指定 LinkExtractor，使用默认的
			if rule.LinkExtractor == nil {
				if cs.defaultLE == nil {
					cs.defaultLE = linkextractor.NewHTMLLinkExtractor()
				}
				rule.LinkExtractor = cs.defaultLE
			}
			cs.compiledRules[i] = compiledRule{
				rule:  rule,
				index: i,
			}
		}

		if cs.FollowLinks == nil {
			cs.FollowLinks = BoolPtr(true)
		}
	})
}

// isFollowLinksEnabled 返回是否启用链接跟踪。
func (cs *CrawlSpider) isFollowLinksEnabled() bool {
	if cs.FollowLinks == nil {
		return true
	}
	return *cs.FollowLinks
}

// internalParse 是 CrawlSpider 的内部解析方法。
// 对应 Scrapy CrawlSpider 的 _parse 方法。
// 它调用 parseWithRules，使用 ParseStartURL 作为回调。
func (cs *CrawlSpider) internalParse(ctx context.Context, response *shttp.Response) ([]Output, error) {
	cs.compileRules()
	return cs.parseWithRules(ctx, response, cs.ParseStartURL, nil, true)
}

// ruleCallback 是规则匹配链接的内部回调。
// 对应 Scrapy CrawlSpider 的 _callback 方法。
func (cs *CrawlSpider) ruleCallback(ctx context.Context, response *shttp.Response) ([]Output, error) {
	ruleIndex, ok := response.GetMeta("rule")
	if !ok {
		return nil, nil
	}

	idx, ok := ruleIndex.(int)
	if !ok || idx < 0 || idx >= len(cs.compiledRules) {
		return nil, nil
	}

	cr := cs.compiledRules[idx]
	return cs.parseWithRules(ctx, response, cr.rule.Callback, cr.rule.CbKwargs, cr.rule.shouldFollow())
}

// ruleErrback 是规则匹配链接的内部错误回调。
// 对应 Scrapy CrawlSpider 的 _errback 方法。
func (cs *CrawlSpider) ruleErrback(ctx context.Context, err error, request *shttp.Request) ([]Output, error) {
	ruleIndex, ok := request.GetMeta("rule")
	if !ok {
		return nil, nil
	}

	idx, ok := ruleIndex.(int)
	if !ok || idx < 0 || idx >= len(cs.compiledRules) {
		return nil, nil
	}

	cr := cs.compiledRules[idx]
	if cr.rule.Errback != nil {
		return cr.rule.Errback(ctx, err, request)
	}

	return nil, nil
}

// parseWithRules 使用规则解析响应。
// 对应 Scrapy CrawlSpider 的 parse_with_rules 方法。
//
// 处理流程：
//  1. 如果有回调函数，调用回调处理响应
//  2. 如果 follow 为 true 且全局链接跟踪已启用，从响应中提取链接并生成请求
func (cs *CrawlSpider) parseWithRules(
	ctx context.Context,
	response *shttp.Response,
	callback CallbackFunc,
	cbKwargs map[string]any,
	follow bool,
) ([]Output, error) {
	var outputs []Output

	// 1. 调用回调函数
	if callback != nil {
		cbResults, err := callback(ctx, response)
		if err != nil {
			return nil, err
		}

		// 通过 ProcessResults 处理
		if cs.ProcessResults != nil {
			cbResults = cs.ProcessResults(response, cbResults)
		}

		outputs = append(outputs, cbResults...)
	}

	// 2. 跟踪链接
	if follow && cs.isFollowLinksEnabled() {
		linkOutputs := cs.requestsToFollow(response)
		outputs = append(outputs, linkOutputs...)
	}

	return outputs, nil
}

// requestsToFollow 从响应中提取链接并生成跟踪请求。
// 对应 Scrapy CrawlSpider 的 _requests_to_follow 方法。
func (cs *CrawlSpider) requestsToFollow(response *shttp.Response) []Output {
	// 检查响应是否为 HTML
	contentType := response.Headers.Get("Content-Type")
	if contentType != "" &&
		!containsIgnoreCase(contentType, "text/html") &&
		!containsIgnoreCase(contentType, "application/xhtml") {
		return nil
	}

	seen := make(map[string]bool)
	var outputs []Output

	for _, cr := range cs.compiledRules {
		// 提取链接
		links := cr.rule.LinkExtractor.ExtractLinks(response)

		// 过滤已见过的链接
		var newLinks []linkextractor.Link
		for _, link := range links {
			if !seen[link.URL] {
				newLinks = append(newLinks, link)
			}
		}

		// 通过 ProcessLinks 处理
		if cr.rule.ProcessLinks != nil {
			newLinks = cr.rule.ProcessLinks(newLinks)
		}

		for _, link := range newLinks {
			seen[link.URL] = true

			// 构建请求
			req := cs.buildRequest(cr.index, link)
			if req == nil {
				continue
			}

			// 通过 ProcessRequest 处理
			if cr.rule.ProcessRequest != nil {
				req = cr.rule.ProcessRequest(req, response)
				if req == nil {
					continue
				}
			}

			outputs = append(outputs, Output{Request: req})
		}
	}

	return outputs
}

// buildRequest 为链接构建请求。
// 对应 Scrapy CrawlSpider 的 _build_request 方法。
func (cs *CrawlSpider) buildRequest(ruleIndex int, link linkextractor.Link) *shttp.Request {
	req, err := shttp.NewRequest(link.URL,
		shttp.WithCallback(cs.ruleCallback),
		shttp.WithErrback(cs.ruleErrback),
		shttp.WithMeta(map[string]any{
			"rule":      ruleIndex,
			"link_text": link.Text,
		}),
	)
	if err != nil {
		if cs.Logger != nil {
			cs.Logger.Error("failed to create request from link",
				"url", link.URL,
				"error", err,
			)
		}
		return nil
	}
	return req
}

// ============================================================================
// 辅助函数
// ============================================================================

// containsIgnoreCase 检查字符串是否包含子串（忽略大小写）。
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > 0 && len(substr) > 0 &&
				containsLower(toLower(s), toLower(substr)))
}

// toLower 转换为小写。
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// containsLower 检查小写字符串是否包含子串。
func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// SetLogger 设置 CrawlSpider 的日志记录器。
func (cs *CrawlSpider) SetLogger(logger *slog.Logger) {
	cs.Logger = logger
}
