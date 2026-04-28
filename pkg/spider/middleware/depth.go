package middleware

import (
	"context"
	"log/slog"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/spider"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// DepthMiddleware 控制爬取深度。
// 对应 Scrapy 的 scrapy.spidermiddlewares.depth.DepthMiddleware。
//
// 功能：
//   - 为每个请求设置 depth Meta（基于父响应的 depth + 1）
//   - 超过 DEPTH_LIMIT 的请求将被丢弃
//   - 根据 DEPTH_PRIORITY 调整请求优先级（depth * priority 递减）
//   - 统计最大爬取深度和各深度请求数
//
// 配置项：
//   - DEPTH_LIMIT: 最大爬取深度（默认 0，不限制）
//   - DEPTH_PRIORITY: 深度优先级调整系数（默认 0，不调整）
//   - DEPTH_STATS_VERBOSE: 是否记录各深度的请求数统计（默认 false）
type DepthMiddleware struct {
	BaseSpiderMiddleware

	maxDepth     int
	priority     int
	verboseStats bool
	stats        stats.Collector
	logger       *slog.Logger
}

// NewDepthMiddleware 创建一个新的 Depth 中间件。
func NewDepthMiddleware(maxDepth, priority int, verboseStats bool, sc stats.Collector, logger *slog.Logger) *DepthMiddleware {
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DepthMiddleware{
		maxDepth:     maxDepth,
		priority:     priority,
		verboseStats: verboseStats,
		stats:        sc,
		logger:       logger,
	}
}

// ProcessOutput 为输出中的请求设置深度信息并过滤超深请求。
func (m *DepthMiddleware) ProcessOutput(ctx context.Context, response *shttp.Response, result []spider.Output) ([]spider.Output, error) {
	// 初始化响应的深度（base case）
	m.initDepth(response)

	filtered := make([]spider.Output, 0, len(result))
	for _, output := range result {
		if output.IsRequest() {
			req := m.processRequest(output.Request, response)
			if req == nil {
				continue // 被深度限制过滤
			}
			output.Request = req
		}
		filtered = append(filtered, output)
	}

	return filtered, nil
}

// initDepth 初始化响应的深度（如果尚未设置）。
func (m *DepthMiddleware) initDepth(response *shttp.Response) {
	if response.Request == nil {
		return
	}
	if _, ok := response.Request.GetMeta("depth"); !ok {
		response.Request.SetMeta("depth", 0)
		if m.verboseStats {
			m.stats.IncValue("request_depth_count/0", 1, 0)
		}
	}
}

// processRequest 处理单个请求的深度逻辑。
func (m *DepthMiddleware) processRequest(request *shttp.Request, response *shttp.Response) *shttp.Request {
	// 获取父响应的深度
	parentDepth := 0
	if response.Request != nil {
		if d, ok := response.Request.GetMeta("depth"); ok {
			if depth, ok := d.(int); ok {
				parentDepth = depth
			}
		}
	}

	depth := parentDepth + 1
	request.SetMeta("depth", depth)

	// 调整优先级
	if m.priority != 0 {
		request.Priority -= depth * m.priority
	}

	// 检查深度限制
	if m.maxDepth > 0 && depth > m.maxDepth {
		m.logger.Debug("ignoring link (depth > limit)",
			"maxdepth", m.maxDepth,
			"depth", depth,
			"url", request.URL.String(),
		)
		return nil
	}

	// 统计
	if m.verboseStats {
		m.stats.IncValue("request_depth_count/"+depthStr(depth), 1, 0)
	}
	m.stats.MaxValue("request_depth_max", depth)

	return request
}

// depthStr 将深度转换为字符串。
func depthStr(d int) string {
	// 简单的数字转字符串，避免引入 strconv
	if d < 10 {
		return string(rune('0' + d))
	}
	buf := [20]byte{}
	pos := len(buf)
	for d > 0 {
		pos--
		buf[pos] = byte('0' + d%10)
		d /= 10
	}
	return string(buf[pos:])
}
