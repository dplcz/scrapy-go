// Package scraper 实现了 scrapy-go 框架的 Scraper 组件。
//
// Scraper 负责处理下载的响应，调用 Spider 回调，
// 并将结果分发到 Item Pipeline 和 Engine。
// 对应 Scrapy Python 版本中 scrapy.core.scraper 模块的功能。
package scraper

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	scrapy_errors "scrapy-go/pkg/errors"
	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/pipeline"
	"scrapy-go/pkg/signal"
	"scrapy-go/pkg/spider"
	spider_mw "scrapy-go/pkg/spider/middleware"
	"scrapy-go/pkg/stats"
)

// Scraper 处理下载的响应，调用 Spider 回调，分发结果。
// 对应 Scrapy 的 Scraper 类。
type Scraper struct {
	spiderMW  *spider_mw.Manager
	itemProc  *pipeline.Manager
	spiderRef spider.Spider
	signals   *signal.SignalManager
	stats     stats.StatsCollector
	logger    *slog.Logger

	// maxActiveSize 控制活跃响应的最大总大小（字节），用于回退机制。
	maxActiveSize int64
	activeSize    atomic.Int64
}

// NewScraper 创建一个新的 Scraper。
func NewScraper(
	spiderMW *spider_mw.Manager,
	itemProc *pipeline.Manager,
	spiderRef spider.Spider,
	signals *signal.SignalManager,
	sc stats.StatsCollector,
	logger *slog.Logger,
	maxActiveSize int,
) *Scraper {
	if spiderMW == nil {
		spiderMW = spider_mw.NewManager(nil)
	}
	if itemProc == nil {
		itemProc = pipeline.NewManager(nil, nil, nil)
	}
	if signals == nil {
		signals = signal.NewSignalManager(nil)
	}
	if sc == nil {
		sc = stats.NewDummyStatsCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}
	if maxActiveSize <= 0 {
		maxActiveSize = 5000000 // 5MB 默认值
	}

	return &Scraper{
		spiderMW:      spiderMW,
		itemProc:      itemProc,
		spiderRef:     spiderRef,
		signals:       signals,
		stats:         sc,
		logger:        logger,
		maxActiveSize: int64(maxActiveSize),
	}
}

// Open 打开 Scraper，初始化 Pipeline。
func (s *Scraper) Open(ctx context.Context) error {
	return s.itemProc.Open(ctx)
}

// Close 关闭 Scraper，释放 Pipeline 资源。
func (s *Scraper) Close(ctx context.Context) error {
	return s.itemProc.Close(ctx)
}

// NeedsBackout 检查是否需要回退（活跃响应大小超过阈值）。
func (s *Scraper) NeedsBackout() bool {
	return s.activeSize.Load() > s.maxActiveSize
}

// Scrape 处理下载的响应。
//
// 处理流程：
//  1. 通过 Spider 中间件链的 ProcessSpiderInput
//  2. 调用 Spider 回调（Request.Callback 或 Spider.Parse）
//  3. 通过 Spider 中间件链的 ProcessSpiderOutput
//  4. 分发结果：Request 返回给 Engine，Item 进入 Pipeline
//
// 返回值：
//   - requests: 需要重新调度的新请求
//   - err: 处理过程中的错误
func (s *Scraper) Scrape(ctx context.Context, response *scrapy_http.Response, request *scrapy_http.Request) ([]*scrapy_http.Request, error) {
	// 追踪活跃大小
	responseSize := int64(len(response.Body))
	if responseSize < 1024 {
		responseSize = 1024 // 最小 1KB
	}
	s.activeSize.Add(responseSize)
	defer func() {
		s.activeSize.Add(-responseSize)
	}()

	// 确定回调函数
	callbackFn := s.resolveCallback(request)

	// 通过 Spider 中间件链处理
	outputs, err := s.spiderMW.ScrapeResponse(ctx, func(ctx context.Context, resp *scrapy_http.Response) ([]spider.SpiderOutput, error) {
		return callbackFn(ctx, resp)
	}, response)

	if err != nil {
		// Spider 回调或中间件异常
		s.handleSpiderError(err, request, response)

		// 检查是否为 CloseSpider 错误
		if errors.Is(err, scrapy_errors.ErrCloseSpider) {
			return nil, err
		}

		return nil, nil // 错误已处理，不传播
	}

	// 分发输出
	return s.processOutputs(ctx, outputs, response)
}

// ScrapeError 处理下载错误（调用 Request.Errback）。
func (s *Scraper) ScrapeError(ctx context.Context, err error, request *scrapy_http.Request) ([]*scrapy_http.Request, error) {
	// 调用 errback
	if request.Errback != nil {
		if errbackFn, ok := request.Errback.(spider.ErrbackFunc); ok {
			outputs, callErr := errbackFn(ctx, err, request)
			if callErr != nil {
				s.handleSpiderError(callErr, request, nil)
				return nil, nil
			}
			return s.processOutputs(ctx, outputs, nil)
		}
	}

	// 无 errback，记录错误
	s.logger.Error("下载失败（无 errback）",
		"request", request.String(),
		"error", err,
	)
	return nil, nil
}

// ============================================================================
// 内部方法
// ============================================================================

// resolveCallback 确定请求的回调函数。
func (s *Scraper) resolveCallback(request *scrapy_http.Request) spider.CallbackFunc {
	if request.Callback != nil {
		if cb, ok := request.Callback.(spider.CallbackFunc); ok {
			return cb
		}
	}
	// 使用 Spider.Parse 作为默认回调
	return s.spiderRef.Parse
}

// processOutputs 分发 Spider 输出。
// 将 Request 收集返回给 Engine，将 Item 发送到 Pipeline。
func (s *Scraper) processOutputs(ctx context.Context, outputs []spider.SpiderOutput, response any) ([]*scrapy_http.Request, error) {
	var newRequests []*scrapy_http.Request

	for _, output := range outputs {
		if output.IsRequest() {
			newRequests = append(newRequests, output.Request)
		} else if output.IsItem() {
			_, err := s.itemProc.ProcessItem(ctx, output.Item, response)
			if err != nil && !errors.Is(err, scrapy_errors.ErrDropItem) {
				// Pipeline 处理错误（非 DropItem），记录但不中断
				s.logger.Error("Pipeline 处理 Item 失败",
					"error", err,
				)
			}
		}
	}

	return newRequests, nil
}

// handleSpiderError 处理 Spider 回调异常。
func (s *Scraper) handleSpiderError(err error, request *scrapy_http.Request, response *scrapy_http.Response) {
	// CloseSpider 不记录为错误
	if errors.Is(err, scrapy_errors.ErrCloseSpider) {
		return
	}

	s.logger.Error("Spider 回调异常",
		"request", request.String(),
		"error", err,
	)

	s.stats.IncValue("spider_exceptions/count", 1, 0)

	s.signals.SendCatchLog(signal.SpiderError, map[string]any{
		"error":    err,
		"request":  request,
		"response": response,
	})
}
