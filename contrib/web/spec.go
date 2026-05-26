package web

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/linkextractor"
	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// SpiderSpec → CrawlSpider 转换引擎
// ============================================================================

// Validate 校验 SpiderSpec 的合法性。
// 返回所有校验错误的列表。
func (spec *SpiderSpec) Validate() []string {
	var errs []string

	if spec.Name == "" {
		errs = append(errs, "name is required")
	}
	if len(spec.StartURLs) == 0 {
		errs = append(errs, "at least one start_url is required")
	}

	// 校验 URL 格式
	for i, u := range spec.StartURLs {
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			errs = append(errs, fmt.Sprintf("start_urls[%d]: must start with http:// or https://", i))
		}
	}

	// 校验规则
	for i, rule := range spec.Rules {
		// 校验正则表达式
		for j, pattern := range rule.LinkExtractor.Allow {
			if _, err := regexp.Compile(pattern); err != nil {
				errs = append(errs, fmt.Sprintf("rules[%d].link_extractor.allow[%d]: invalid regex: %s", i, j, err))
			}
		}
		for j, pattern := range rule.LinkExtractor.Deny {
			if _, err := regexp.Compile(pattern); err != nil {
				errs = append(errs, fmt.Sprintf("rules[%d].link_extractor.deny[%d]: invalid regex: %s", i, j, err))
			}
		}

		// 如果有 callback，检查 item_schemas 中是否有对应定义
		if rule.Callback != "" && spec.ItemSchemas != nil {
			if _, ok := spec.ItemSchemas[rule.Callback]; !ok {
				// 不是错误，只是警告（callback 可以不提取 item）
			}
		}
	}

	// 校验 ItemSchemas
	for name, schema := range spec.ItemSchemas {
		for field, extractor := range schema {
			if extractor.CSS == "" && extractor.XPath == "" && extractor.Value == "" {
				errs = append(errs, fmt.Sprintf("item_schemas[%q][%q]: must specify css, xpath, or value", name, field))
			}
			if extractor.CSS != "" && extractor.XPath != "" {
				errs = append(errs, fmt.Sprintf("item_schemas[%q][%q]: css and xpath are mutually exclusive", name, field))
			}
			if extractor.Regex != "" {
				if _, err := regexp.Compile(extractor.Regex); err != nil {
					errs = append(errs, fmt.Sprintf("item_schemas[%q][%q].regex: invalid regex: %s", name, field, err))
				}
			}
		}
	}

	return errs
}

// ToFactory 将 SpiderSpec 转换为 SpiderFactory 函数。
//
// 转换逻辑：
//  1. 将 RuleSpec 列表转换为 spider.Rule 列表
//  2. 为每个有 Callback 的规则生成基于 ItemSchema 的数据提取回调
//  3. 构建 CrawlSpider 实例并返回工厂函数
func (spec *SpiderSpec) ToFactory() SpiderFactory {
	// 预编译正则表达式（在工厂外部编译，避免每次创建时重复编译）
	compiledSchemas := make(map[string]*compiledItemSchema)
	for name, schema := range spec.ItemSchemas {
		compiledSchemas[name] = compileItemSchema(schema)
	}

	return func() spider.Spider {
		// 构建 Rules
		rules := make([]spider.Rule, 0, len(spec.Rules))
		for _, ruleSpec := range spec.Rules {
			rule := buildRule(ruleSpec, compiledSchemas)
			rules = append(rules, rule)
		}

		cs := &spider.CrawlSpider{
			Base: spider.Base{
				SpiderName: spec.Name,
				StartURLs:  spec.StartURLs,
			},
			Rules: rules,
		}

		// 设置 AllowedDomains（通过 Offsite 中间件生效）
		if len(spec.AllowedDomains) > 0 {
			cs.Base.StartURLs = spec.StartURLs
		}

		return cs
	}
}

// ToSettings 将 SpiderSpec 中的 Settings 转换为 map[string]any。
// 包含 AllowedDomains 的注入。
func (spec *SpiderSpec) ToSettings() map[string]any {
	result := make(map[string]any)

	// 复制用户自定义 settings
	for k, v := range spec.Settings {
		result[k] = v
	}

	// 注入 AllowedDomains
	if len(spec.AllowedDomains) > 0 {
		result["ALLOWED_DOMAINS"] = spec.AllowedDomains
	}

	return result
}

// ============================================================================
// 内部类型和辅助函数
// ============================================================================

// compiledItemSchema 是预编译的 Item 提取规则。
type compiledItemSchema struct {
	fields map[string]*compiledFieldExtractor
}

// compiledFieldExtractor 是预编译的字段提取器。
type compiledFieldExtractor struct {
	css      string
	xpath    string
	value    string
	regex    *regexp.Regexp
	defValue string
}

// compileItemSchema 预编译 ItemSchema。
func compileItemSchema(schema ItemSchema) *compiledItemSchema {
	compiled := &compiledItemSchema{
		fields: make(map[string]*compiledFieldExtractor, len(schema)),
	}

	for name, extractor := range schema {
		fe := &compiledFieldExtractor{
			css:      extractor.CSS,
			xpath:    extractor.XPath,
			value:    extractor.Value,
			defValue: extractor.Default,
		}
		if extractor.Regex != "" {
			fe.regex, _ = regexp.Compile(extractor.Regex)
		}
		compiled.fields[name] = fe
	}

	return compiled
}

// buildRule 将 RuleSpec 转换为 spider.Rule。
func buildRule(ruleSpec RuleSpec, schemas map[string]*compiledItemSchema) spider.Rule {
	// 构建 LinkExtractor 选项
	opts := buildLinkExtractorOpts(ruleSpec.LinkExtractor)
	le := linkextractor.NewHTMLLinkExtractor(opts...)

	rule := spider.Rule{
		LinkExtractor: le,
		Follow:        ruleSpec.Follow,
	}

	// 如果有 callback，生成数据提取回调
	if ruleSpec.Callback != "" {
		schema := schemas[ruleSpec.Callback]
		rule.Callback = makeExtractCallback(schema)
	}

	return rule
}

// buildLinkExtractorOpts 将 LinkExtractorSpec 转换为 Option 列表。
func buildLinkExtractorOpts(spec LinkExtractorSpec) []linkextractor.Option {
	var opts []linkextractor.Option

	if len(spec.Allow) > 0 {
		opts = append(opts, linkextractor.WithAllow(spec.Allow...))
	}
	if len(spec.Deny) > 0 {
		opts = append(opts, linkextractor.WithDeny(spec.Deny...))
	}
	if len(spec.AllowDomains) > 0 {
		opts = append(opts, linkextractor.WithAllowDomains(spec.AllowDomains...))
	}
	if len(spec.DenyDomains) > 0 {
		opts = append(opts, linkextractor.WithDenyDomains(spec.DenyDomains...))
	}
	if len(spec.RestrictCSS) > 0 {
		opts = append(opts, linkextractor.WithRestrictCSS(spec.RestrictCSS...))
	}
	if len(spec.RestrictXPath) > 0 {
		opts = append(opts, linkextractor.WithRestrictXPath(spec.RestrictXPath...))
	}
	if len(spec.Tags) > 0 {
		opts = append(opts, linkextractor.WithTags(spec.Tags...))
	}
	if len(spec.Attrs) > 0 {
		opts = append(opts, linkextractor.WithAttrs(spec.Attrs...))
	}

	return opts
}

// makeExtractCallback 生成基于 ItemSchema 的数据提取回调函数。
//
// 回调逻辑：
//  1. 遍历 schema 中的每个字段定义
//  2. 根据 CSS/XPath/Value 提取数据
//  3. 如果有 Regex，对提取结果进行正则匹配
//  4. 如果提取失败，使用默认值
//  5. 将所有字段组装为 map[string]any 作为 Item 输出
func makeExtractCallback(schema *compiledItemSchema) spider.CallbackFunc {
	return func(ctx context.Context, response *shttp.Response) ([]shttp.Output, error) {
		if schema == nil || len(schema.fields) == 0 {
			return nil, nil
		}

		item := make(map[string]any, len(schema.fields))

		for fieldName, fe := range schema.fields {
			var value string

			switch {
			case fe.value != "":
				// 特殊值处理
				value = resolveSpecialValue(fe.value, response)
			case fe.css != "":
				// CSS 选择器提取
				value = extractByCSS(response, fe.css)
			case fe.xpath != "":
				// XPath 提取
				value = extractByXPath(response, fe.xpath)
			}

			// 正则匹配
			if fe.regex != nil && value != "" {
				matches := fe.regex.FindStringSubmatch(value)
				if len(matches) > 1 {
					value = matches[1]
				} else if len(matches) == 1 {
					value = matches[0]
				} else {
					value = ""
				}
			}

			// 默认值
			if value == "" && fe.defValue != "" {
				value = fe.defValue
			}

			item[fieldName] = value
		}

		return []shttp.Output{{Item: item}}, nil
	}
}

// resolveSpecialValue 解析特殊值标识。
func resolveSpecialValue(value string, response *shttp.Response) string {
	switch value {
	case "_response_url":
		return response.URL.String()
	case "_timestamp":
		return time.Now().Format(time.RFC3339)
	default:
		// 作为字面量值
		return value
	}
}

// extractByCSS 使用 CSS 选择器从响应中提取文本。
func extractByCSS(response *shttp.Response, cssExpr string) string {
	// 处理 ::attr(name) 伪元素
	if strings.Contains(cssExpr, "::attr(") {
		idx := strings.Index(cssExpr, "::attr(")
		sel := strings.TrimSpace(cssExpr[:idx])
		attrPart := cssExpr[idx+7:]
		attrName := strings.TrimSuffix(attrPart, ")")
		return response.CSSAttr(sel, attrName).Get("")
	}

	// CSS 选择器（支持 ::text 伪元素，由 selector 包内部处理）
	return response.CSS(cssExpr).Get("")
}

// extractByXPath 使用 XPath 从响应中提取文本。
func extractByXPath(response *shttp.Response, xpathExpr string) string {
	return response.XPath(xpathExpr).Get("")
}
