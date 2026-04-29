package linkextractor

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/selector"
	"golang.org/x/net/html"
)

// ignoredExtensions 是默认忽略的文件扩展名集合。
// 对应 Scrapy 的 IGNORED_EXTENSIONS。
var ignoredExtensions = map[string]bool{
	// 压缩文件
	".7z": true, ".7zip": true, ".bz2": true, ".rar": true,
	".tar": true, ".tar.gz": true, ".xz": true, ".zip": true,
	// 图片
	".mng": true, ".pct": true, ".bmp": true, ".gif": true,
	".jpg": true, ".jpeg": true, ".png": true, ".pst": true,
	".psp": true, ".tif": true, ".tiff": true, ".ai": true,
	".drw": true, ".dxf": true, ".eps": true, ".ps": true,
	".svg": true, ".cdr": true, ".ico": true, ".webp": true,
	// 音频
	".mp3": true, ".wma": true, ".ogg": true, ".wav": true,
	".ra": true, ".aac": true, ".mid": true, ".au": true, ".aiff": true,
	// 视频
	".3gp": true, ".asf": true, ".asx": true, ".avi": true,
	".mov": true, ".mp4": true, ".mpg": true, ".qt": true,
	".rm": true, ".swf": true, ".wmv": true, ".m4a": true,
	".m4v": true, ".flv": true, ".webm": true,
	// 办公文档
	".xls": true, ".xlsm": true, ".xlsx": true, ".xltm": true, ".xltx": true,
	".potm": true, ".potx": true, ".ppt": true, ".pptm": true, ".pptx": true,
	".pps": true, ".doc": true, ".docb": true, ".docm": true, ".docx": true,
	".dotm": true, ".dotx": true, ".odt": true, ".ods": true, ".odg": true, ".odp": true,
	// 其他
	".css": true, ".pdf": true, ".exe": true, ".bin": true,
	".rss": true, ".dmg": true, ".iso": true, ".apk": true,
	".jar": true, ".sh": true, ".rb": true, ".js": true,
	".hta": true, ".bat": true, ".cpl": true, ".msi": true, ".msp": true, ".py": true,
}

// HTMLLinkExtractor 基于 goquery 的链接提取器。
// 对应 Scrapy 的 LxmlLinkExtractor（Go 中重命名为 HTMLLinkExtractor，因不使用 lxml）。
//
// 支持 allow/deny 正则过滤、域名过滤、restrict CSS/XPath、去重等功能。
// 使用 Functional Options 模式配置。
type HTMLLinkExtractor struct {
	// allow 是允许的 URL 正则表达式列表。
	// 只有匹配至少一个正则的 URL 才会被提取。空列表表示允许所有。
	allow []*regexp.Regexp

	// deny 是拒绝的 URL 正则表达式列表。
	// 匹配任一正则的 URL 将被排除。
	deny []*regexp.Regexp

	// allowDomains 是允许的域名集合。
	allowDomains map[string]bool

	// denyDomains 是拒绝的域名集合。
	denyDomains map[string]bool

	// restrictCSS 是限制链接提取范围的 CSS 选择器列表。
	// 仅从匹配这些选择器的元素中提取链接。
	restrictCSS []string

	// restrictXPath 是限制链接提取范围的 XPath 表达式列表。
	restrictXPath []string

	// tags 是要扫描的 HTML 标签集合（默认 "a", "area"）。
	tags map[string]bool

	// attrs 是要扫描的 HTML 属性集合（默认 "href"）。
	attrs map[string]bool

	// unique 是否对提取的链接去重（默认 true）。
	unique bool

	// stripFragment 是否去除 URL 中的 fragment（默认 true）。
	stripFragment bool

	// denyExtensions 是拒绝的文件扩展名集合。
	denyExtensions map[string]bool

	// restrictText 是限制链接文本的正则表达式列表。
	// 仅提取锚文本匹配至少一个正则的链接。
	restrictText []*regexp.Regexp

	// canonicalize 是否规范化 URL（默认 false）。
	canonicalize bool
}

// Option 是 HTMLLinkExtractor 的配置选项。
type Option func(*HTMLLinkExtractor)

// NewHTMLLinkExtractor 创建一个新的 HTMLLinkExtractor。
// 使用 Functional Options 模式配置。
//
// 用法：
//
//	le := linkextractor.NewHTMLLinkExtractor(
//	    linkextractor.WithAllow(`/page/\d+`),
//	    linkextractor.WithDenyDomains("ads.example.com"),
//	)
//	links := le.ExtractLinks(response)
func NewHTMLLinkExtractor(opts ...Option) *HTMLLinkExtractor {
	le := &HTMLLinkExtractor{
		tags:           map[string]bool{"a": true, "area": true},
		attrs:          map[string]bool{"href": true},
		unique:         true,
		stripFragment:  true,
		denyExtensions: make(map[string]bool),
		allowDomains:   make(map[string]bool),
		denyDomains:    make(map[string]bool),
	}

	// 默认拒绝常见非网页扩展名
	for ext := range ignoredExtensions {
		le.denyExtensions[ext] = true
	}

	for _, opt := range opts {
		opt(le)
	}

	return le
}

// ============================================================================
// Functional Options
// ============================================================================

// WithAllow 设置允许的 URL 正则表达式。
// 支持传入多个正则字符串，只有匹配至少一个的 URL 才会被提取。
func WithAllow(patterns ...string) Option {
	return func(le *HTMLLinkExtractor) {
		for _, p := range patterns {
			if re, err := regexp.Compile(p); err == nil {
				le.allow = append(le.allow, re)
			}
		}
	}
}

// WithDeny 设置拒绝的 URL 正则表达式。
// 匹配任一正则的 URL 将被排除。
func WithDeny(patterns ...string) Option {
	return func(le *HTMLLinkExtractor) {
		for _, p := range patterns {
			if re, err := regexp.Compile(p); err == nil {
				le.deny = append(le.deny, re)
			}
		}
	}
}

// WithAllowDomains 设置允许的域名列表。
func WithAllowDomains(domains ...string) Option {
	return func(le *HTMLLinkExtractor) {
		for _, d := range domains {
			le.allowDomains[d] = true
		}
	}
}

// WithDenyDomains 设置拒绝的域名列表。
func WithDenyDomains(domains ...string) Option {
	return func(le *HTMLLinkExtractor) {
		for _, d := range domains {
			le.denyDomains[d] = true
		}
	}
}

// WithRestrictCSS 设置限制链接提取范围的 CSS 选择器。
func WithRestrictCSS(selectors ...string) Option {
	return func(le *HTMLLinkExtractor) {
		le.restrictCSS = append(le.restrictCSS, selectors...)
	}
}

// WithRestrictXPath 设置限制链接提取范围的 XPath 表达式。
func WithRestrictXPath(exprs ...string) Option {
	return func(le *HTMLLinkExtractor) {
		le.restrictXPath = append(le.restrictXPath, exprs...)
	}
}

// WithTags 设置要扫描的 HTML 标签（默认 "a", "area"）。
func WithTags(tags ...string) Option {
	return func(le *HTMLLinkExtractor) {
		le.tags = make(map[string]bool)
		for _, t := range tags {
			le.tags[t] = true
		}
	}
}

// WithAttrs 设置要扫描的 HTML 属性（默认 "href"）。
func WithAttrs(attrs ...string) Option {
	return func(le *HTMLLinkExtractor) {
		le.attrs = make(map[string]bool)
		for _, a := range attrs {
			le.attrs[a] = true
		}
	}
}

// WithUnique 设置是否对提取的链接去重（默认 true）。
func WithUnique(unique bool) Option {
	return func(le *HTMLLinkExtractor) {
		le.unique = unique
	}
}

// WithStripFragment 设置是否去除 URL 中的 fragment（默认 true）。
func WithStripFragment(strip bool) Option {
	return func(le *HTMLLinkExtractor) {
		le.stripFragment = strip
	}
}

// WithDenyExtensions 设置拒绝的文件扩展名列表。
// 传入不带点号的扩展名（如 "pdf", "jpg"）。
// 传入 nil 或空切片将清除默认的拒绝扩展名。
func WithDenyExtensions(exts []string) Option {
	return func(le *HTMLLinkExtractor) {
		le.denyExtensions = make(map[string]bool)
		for _, ext := range exts {
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			le.denyExtensions[ext] = true
		}
	}
}

// WithRestrictText 设置限制链接文本的正则表达式。
func WithRestrictText(patterns ...string) Option {
	return func(le *HTMLLinkExtractor) {
		for _, p := range patterns {
			if re, err := regexp.Compile(p); err == nil {
				le.restrictText = append(le.restrictText, re)
			}
		}
	}
}

// WithCanonicalize 设置是否规范化 URL（默认 false）。
func WithCanonicalize(canonicalize bool) Option {
	return func(le *HTMLLinkExtractor) {
		le.canonicalize = canonicalize
	}
}

// ============================================================================
// ExtractLinks 实现
// ============================================================================

// ExtractLinks 从 HTTP 响应中提取链接。
// 实现 LinkExtractor 接口。
func (le *HTMLLinkExtractor) ExtractLinks(response *shttp.Response) []Link {
	if response == nil || len(response.Body) == 0 {
		return nil
	}

	baseURL := le.getBaseURL(response)

	// 获取要搜索的文档片段
	sel := selector.NewFromBytes(response.Body)
	docs := le.getRestrictedDocs(sel)

	// 从每个文档片段中提取链接
	var allLinks []Link
	for _, doc := range docs {
		links := le.extractFromSelection(doc, baseURL, response.URL.String())
		allLinks = append(allLinks, links...)
	}

	// 过滤链接
	allLinks = le.filterLinks(allLinks)

	// 去重
	if le.unique {
		allLinks = le.deduplicateLinks(allLinks)
	}

	return allLinks
}

// Matches 检查给定 URL 是否匹配此提取器的过滤规则。
// 可用于在不解析 HTML 的情况下快速判断 URL 是否会被提取。
func (le *HTMLLinkExtractor) Matches(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// 检查域名
	if len(le.allowDomains) > 0 && !le.isDomainAllowed(u.Hostname()) {
		return false
	}
	if len(le.denyDomains) > 0 && le.isDomainDenied(u.Hostname()) {
		return false
	}

	// 检查 allow 正则
	if len(le.allow) > 0 && !matchesAny(rawURL, le.allow) {
		return false
	}

	// 检查 deny 正则
	if len(le.deny) > 0 && matchesAny(rawURL, le.deny) {
		return false
	}

	return true
}

// ============================================================================
// 内部方法
// ============================================================================

// getBaseURL 获取响应的基础 URL。
// 优先使用 HTML 中的 <base> 标签，否则使用响应 URL。
func (le *HTMLLinkExtractor) getBaseURL(response *shttp.Response) string {
	sel := selector.NewFromBytes(response.Body)
	baseHref := sel.CSSAttr("base", "href").Get("")
	if baseHref != "" {
		if u, err := url.Parse(baseHref); err == nil && u.IsAbs() {
			return baseHref
		}
		// 相对 base href，解析为绝对 URL
		if response.URL != nil {
			if u, err := url.Parse(baseHref); err == nil {
				return response.URL.ResolveReference(u).String()
			}
		}
	}
	if response.URL != nil {
		return response.URL.String()
	}
	return ""
}

// getRestrictedDocs 获取限制范围内的文档片段。
// 如果设置了 restrictCSS 或 restrictXPath，仅返回匹配的子文档。
func (le *HTMLLinkExtractor) getRestrictedDocs(sel *selector.Selector) []*selector.Selector {
	if len(le.restrictCSS) == 0 && len(le.restrictXPath) == 0 {
		return []*selector.Selector{sel}
	}

	var docs []*selector.Selector

	for _, css := range le.restrictCSS {
		list := sel.CSS(css)
		for _, s := range list {
			docs = append(docs, s)
		}
	}

	for _, xpath := range le.restrictXPath {
		list := sel.XPath(xpath)
		for _, s := range list {
			docs = append(docs, s)
		}
	}

	if len(docs) == 0 {
		return []*selector.Selector{sel}
	}

	return docs
}

// extractFromSelection 从 goquery Selection 中提取链接。
func (le *HTMLLinkExtractor) extractFromSelection(sel *selector.Selector, baseURL, responseURL string) []Link {
	var links []Link

	// 获取 HTML 内容并用 goquery 解析
	htmlContent := sel.GetHTML()
	if htmlContent == "" {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	// 构建标签选择器
	tagSelector := le.buildTagSelector()
	if tagSelector == "" {
		return nil
	}

	doc.Find(tagSelector).Each(func(_ int, s *goquery.Selection) {
		for attr := range le.attrs {
			val, exists := s.Attr(attr)
			if !exists || val == "" {
				continue
			}

			// 去除空白
			val = strings.TrimSpace(val)

			// 解析为绝对 URL
			absURL := le.resolveURL(val, baseURL, responseURL)
			if absURL == "" {
				continue
			}

			// 检查 URL scheme 是否有效
			if !isValidURL(absURL) {
				continue
			}

			// 提取 fragment
			fragment := ""
			if u, err := url.Parse(absURL); err == nil {
				fragment = u.Fragment
				if le.stripFragment {
					u.Fragment = ""
					absURL = u.String()
				}
			}

			// 提取锚文本
			text := strings.TrimSpace(extractText(s))

			// 检查 nofollow
			nofollow := hasNoFollow(s)

			links = append(links, Link{
				URL:      absURL,
				Text:     text,
				Fragment: fragment,
				NoFollow: nofollow,
			})
		}
	})

	return links
}

// buildTagSelector 构建 CSS 标签选择器字符串。
func (le *HTMLLinkExtractor) buildTagSelector() string {
	tags := make([]string, 0, len(le.tags))
	for tag := range le.tags {
		tags = append(tags, tag)
	}
	return strings.Join(tags, ", ")
}

// resolveURL 将相对 URL 解析为绝对 URL。
func (le *HTMLLinkExtractor) resolveURL(rawURL, baseURL, responseURL string) string {
	// 尝试解析为绝对 URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	if u.IsAbs() {
		return u.String()
	}

	// 使用 baseURL 解析
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	resolved := base.ResolveReference(u)
	return resolved.String()
}

// filterLinks 过滤链接。
func (le *HTMLLinkExtractor) filterLinks(links []Link) []Link {
	filtered := make([]Link, 0, len(links))
	for _, link := range links {
		if le.linkAllowed(link) {
			filtered = append(filtered, link)
		}
	}
	return filtered
}

// linkAllowed 检查链接是否被允许。
func (le *HTMLLinkExtractor) linkAllowed(link Link) bool {
	// 检查 URL 有效性
	if !isValidURL(link.URL) {
		return false
	}

	u, err := url.Parse(link.URL)
	if err != nil {
		return false
	}

	// 检查 allow 正则
	if len(le.allow) > 0 && !matchesAny(link.URL, le.allow) {
		return false
	}

	// 检查 deny 正则
	if len(le.deny) > 0 && matchesAny(link.URL, le.deny) {
		return false
	}

	// 检查允许域名
	if len(le.allowDomains) > 0 && !le.isDomainAllowed(u.Hostname()) {
		return false
	}

	// 检查拒绝域名
	if len(le.denyDomains) > 0 && le.isDomainDenied(u.Hostname()) {
		return false
	}

	// 检查拒绝扩展名
	if len(le.denyExtensions) > 0 && le.hasBlockedExtension(u.Path) {
		return false
	}

	// 检查 restrictText
	if len(le.restrictText) > 0 && !matchesAny(link.Text, le.restrictText) {
		return false
	}

	return true
}

// isDomainAllowed 检查域名是否在允许列表中。
func (le *HTMLLinkExtractor) isDomainAllowed(hostname string) bool {
	if le.allowDomains[hostname] {
		return true
	}
	// 检查子域名匹配
	for domain := range le.allowDomains {
		if strings.HasSuffix(hostname, "."+domain) {
			return true
		}
	}
	return false
}

// isDomainDenied 检查域名是否在拒绝列表中。
func (le *HTMLLinkExtractor) isDomainDenied(hostname string) bool {
	if le.denyDomains[hostname] {
		return true
	}
	for domain := range le.denyDomains {
		if strings.HasSuffix(hostname, "."+domain) {
			return true
		}
	}
	return false
}

// hasBlockedExtension 检查 URL 路径是否包含被拒绝的扩展名。
func (le *HTMLLinkExtractor) hasBlockedExtension(urlPath string) bool {
	ext := strings.ToLower(path.Ext(urlPath))
	return le.denyExtensions[ext]
}

// deduplicateLinks 对链接去重（基于 URL）。
func (le *HTMLLinkExtractor) deduplicateLinks(links []Link) []Link {
	seen := make(map[string]bool)
	result := make([]Link, 0, len(links))
	for _, link := range links {
		key := link.URL
		if le.canonicalize {
			key = canonicalizeURL(key)
		}
		if !seen[key] {
			seen[key] = true
			result = append(result, link)
		}
	}
	return result
}

// ============================================================================
// 辅助函数
// ============================================================================

// matchesAny 检查字符串是否匹配任一正则表达式。
func matchesAny(s string, regexps []*regexp.Regexp) bool {
	for _, re := range regexps {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// isValidURL 检查 URL 是否有效（scheme 为 http/https/file/ftp）。
func isValidURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https", "file", "ftp":
		return true
	}
	return false
}

// extractText 提取 goquery Selection 的文本内容。
func extractText(s *goquery.Selection) string {
	var texts []string
	s.Contents().Each(func(_ int, c *goquery.Selection) {
		if c.Nodes != nil && len(c.Nodes) > 0 && c.Nodes[0].Type == html.TextNode {
			texts = append(texts, c.Nodes[0].Data)
		}
	})
	if len(texts) == 0 {
		return s.Text()
	}
	return strings.Join(texts, "")
}

// hasNoFollow 检查元素是否包含 rel="nofollow"。
func hasNoFollow(s *goquery.Selection) bool {
	rel, exists := s.Attr("rel")
	if !exists {
		return false
	}
	for _, v := range strings.Fields(rel) {
		if strings.EqualFold(v, "nofollow") {
			return true
		}
	}
	return false
}

// canonicalizeURL 规范化 URL（简化实现）。
func canonicalizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	// 规范化：小写 scheme 和 host，排序查询参数
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return u.String()
}
