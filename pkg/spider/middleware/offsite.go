package middleware

import (
	"context"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// OffsiteMiddleware 过滤站外请求。
// 对应 Scrapy 的 scrapy.downloadermiddlewares.offsite.OffsiteMiddleware。
//
// 在 scrapy-go 中将 Offsite 实现为 Spider 中间件（而非下载器中间件），
// 在 ProcessOutput 阶段过滤不属于 Spider.AllowedDomains 的请求。
//
// 过滤规则：
//   - 如果 Spider 未定义 AllowedDomains（或为空），允许所有请求
//   - 如果 Request.DontFilter 为 true，允许该请求
//   - 如果 Request.Meta["allow_offsite"] 为 true，允许该请求
//   - 否则，检查请求 URL 的主机名是否匹配 AllowedDomains（支持子域名）
type OffsiteMiddleware struct {
	BaseSpiderMiddleware

	allowedDomains []string
	hostRegex      *regexp.Regexp
	domainsSeen    map[string]bool
	stats          stats.Collector
	logger         *slog.Logger
}

// NewOffsiteMiddleware 创建一个新的 Offsite 中间件。
// allowedDomains 为允许的域名列表，为空表示允许所有域名。
func NewOffsiteMiddleware(allowedDomains []string, sc stats.Collector, logger *slog.Logger) *OffsiteMiddleware {
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}

	m := &OffsiteMiddleware{
		allowedDomains: allowedDomains,
		domainsSeen:    make(map[string]bool),
		stats:          sc,
		logger:         logger,
	}

	m.hostRegex = m.buildHostRegex(allowedDomains)
	return m
}

// ProcessOutput 过滤站外请求。
func (m *OffsiteMiddleware) ProcessOutput(ctx context.Context, response *shttp.Response, result []spider.Output) ([]spider.Output, error) {
	filtered := make([]spider.Output, 0, len(result))
	for _, output := range result {
		if output.IsRequest() {
			if !m.shouldFollow(output.Request) {
				m.logFiltered(output.Request)
				continue
			}
		}
		filtered = append(filtered, output)
	}
	return filtered, nil
}

// shouldFollow 检查请求是否应该被跟踪。
func (m *OffsiteMiddleware) shouldFollow(request *shttp.Request) bool {
	// DontFilter 的请求始终允许
	if request.DontFilter {
		return true
	}

	// Meta 中 allow_offsite 为 true 时允许
	if allowOffsite, ok := request.GetMeta("allow_offsite"); ok {
		if b, ok := allowOffsite.(bool); ok && b {
			return true
		}
	}

	// 没有配置 AllowedDomains 时允许所有
	if m.hostRegex == nil {
		return true
	}

	// 检查主机名是否匹配
	hostname := extractHostname(request.URL)
	if hostname == "" {
		return false
	}

	return m.hostRegex.MatchString(hostname)
}

// logFiltered 记录被过滤的请求。
func (m *OffsiteMiddleware) logFiltered(request *shttp.Request) {
	domain := extractHostname(request.URL)
	if domain != "" && !m.domainsSeen[domain] {
		m.domainsSeen[domain] = true
		m.logger.Debug("filtered offsite request",
			"domain", domain,
			"url", request.URL.String(),
		)
		m.stats.IncValue("offsite/domains", 1, 0)
	}
	m.stats.IncValue("offsite/filtered", 1, 0)
}

// buildHostRegex 构建域名匹配正则表达式。
// 支持子域名匹配：example.com 匹配 www.example.com、sub.example.com 等。
func (m *OffsiteMiddleware) buildHostRegex(domains []string) *regexp.Regexp {
	if len(domains) == 0 {
		return nil // 允许所有
	}

	var validDomains []string
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		// 跳过 URL 格式的条目（只接受纯域名）
		if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
			m.logger.Warn("allowed_domains accepts only domains, not URLs; ignoring",
				"entry", domain,
			)
			continue
		}
		// 跳过带端口的条目
		if strings.Contains(domain, ":") {
			m.logger.Warn("allowed_domains accepts only domains without ports; ignoring",
				"entry", domain,
			)
			continue
		}
		validDomains = append(validDomains, regexp.QuoteMeta(domain))
	}

	if len(validDomains) == 0 {
		return nil
	}

	pattern := `^(.*\.)?` + "(" + strings.Join(validDomains, "|") + ")$"
	return regexp.MustCompile(pattern)
}

// extractHostname 从 URL 中提取主机名。
func extractHostname(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Hostname()
}
