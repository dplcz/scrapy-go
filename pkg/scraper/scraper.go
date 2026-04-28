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
	"sync"
	"sync/atomic"

	serrors "github.com/dplcz/scrapy-go/pkg/errors"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/pipeline"
	"github.com/dplcz/scrapy-go/pkg/signal"
	"github.com/dplcz/scrapy-go/pkg/spider"
	smiddle "github.com/dplcz/scrapy-go/pkg/spider/middleware"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// Scraper 处理下载的响应，调用 Spider 回调，分发结果。
// 对应 Scrapy 的 Scraper 类。
type Scraper struct {
	spiderMW  *smiddle.Manager
	itemProc  *pipeline.Manager
	spiderRef spider.Spider
	signals   *signal.Manager
	stats     stats.Collector
	logger    *slog.Logger

	// maxActiveSize 控制活跃响应的最大总大小（字节），用于回退机制。
	maxActiveSize int64
	activeSize    atomic.Int64

	// concurrentItems 控制同时在 Pipeline 链中处理的 Item 上限。
	// 对齐 Scrapy 的 CONCURRENT_ITEMS 配置（默认 100）。
	concurrentItems int

	// itemSem 是 Item 并发处理的信号量通道。
	// 缓冲区大小等于 concurrentItems。
	itemSem chan struct{}

	// itemWg 用于等待所有 in-flight Item 处理完毕（优雅关闭）。
	itemWg sync.WaitGroup
}

// NewScraper 创建一个新的 Scraper。
func NewScraper(
	spiderMW *smiddle.Manager,
	itemProc *pipeline.Manager,
	spiderRef spider.Spider,
	signals *signal.Manager,
	sc stats.Collector,
	logger *slog.Logger,
	maxActiveSize int,
	concurrentItems int,
) *Scraper {
	if spiderMW == nil {
		spiderMW = smiddle.NewManager(nil)
	}
	if itemProc == nil {
		itemProc = pipeline.NewManager(nil, nil, nil)
	}
	if signals == nil {
		signals = signal.NewManager(nil)
	}
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}
	if maxActiveSize <= 0 {
		maxActiveSize = 5000000 // 5MB 默认值
	}
	if concurrentItems <= 0 {
		concurrentItems = 100 // 对齐 Scrapy 默认值
	}

	return &Scraper{
		spiderMW:        spiderMW,
		itemProc:        itemProc,
		spiderRef:       spiderRef,
		signals:         signals,
		stats:           sc,
		logger:          logger,
		maxActiveSize:   int64(maxActiveSize),
		concurrentItems: concurrentItems,
		itemSem:         make(chan struct{}, concurrentItems),
	}
}

// Open 打开 Scraper，初始化 Pipeline。
func (s *Scraper) Open(ctx context.Context) error {
	return s.itemProc.Open(ctx)
}

// Close 关闭 Scraper，等待 in-flight Item 处理完毕后释放 Pipeline 资源。
func (s *Scraper) Close(ctx context.Context) error {
	// 等待所有 in-flight Item 处理完毕（优雅关闭）
	done := make(chan struct{})
	go func() {
		s.itemWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 所有 Item 处理完毕
	case <-ctx.Done():
		s.logger.Warn("context cancelled while waiting for in-flight items to complete")
	}

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
//  3. 通过 Spider 中间件链的 ProcessOutput
//  4. 分发结果：Request 返回给 Engine，Item 进入 Pipeline
//
// 返回值：
//   - requests: 需要重新调度的新请求
//   - err: 处理过程中的错误
func (s *Scraper) Scrape(ctx context.Context, response *shttp.Response, request *shttp.Request) ([]*shttp.Request, error) {
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
	outputs, err := s.spiderMW.ScrapeResponse(ctx, func(ctx context.Context, resp *shttp.Response) ([]spider.Output, error) {
		return callbackFn(ctx, resp)
	}, response)

	if err != nil {
		// Spider 回调或中间件异常
		s.handleSpiderError(err, request, response)

		// 检查是否为 CloseSpider 错误
		if errors.Is(err, serrors.ErrCloseSpider) {
			return nil, err
		}

		return nil, nil // 错误已处理，不传播
	}

	// 分发输出
	return s.processOutputs(ctx, outputs, response)
}

// ScrapeError 处理下载错误（调用 Request.Errback）。
func (s *Scraper) ScrapeError(ctx context.Context, err error, request *shttp.Request) ([]*shttp.Request, error) {
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
	s.logger.Error("download failed (no errback)",
		"request", request.String(),
		"error", err,
	)
	return nil, nil
}

// ============================================================================
// 内部方法
// ============================================================================

// resolveCallback 确定请求的回调函数。
func (s *Scraper) resolveCallback(request *shttp.Request) spider.CallbackFunc {
	if request.Callback != nil {
		// 检查是否为 NoCallback 哨兵值
		if shttp.IsNoCallback(request.Callback) {
			// NoCallback 表示不需要回调，返回空操作
			return func(ctx context.Context, response *shttp.Response) ([]spider.Output, error) {
				return nil, nil
			}
		}
		if cb, ok := request.Callback.(spider.CallbackFunc); ok {
			return cb
		}
	}
	// 使用 Spider.Parse 作为默认回调
	return s.spiderRef.Parse
}

// processOutputs 分发 Spider 输出。
// 将 Request 收集返回给 Engine，将 Item 发送到 Pipeline。
// Item 之间并发处理（受 CONCURRENT_ITEMS 上限控制），
// 单个 Item 内 Pipeline 链仍按优先级串行（对齐 Scrapy 语义）。
func (s *Scraper) processOutputs(ctx context.Context, outputs []spider.Output, response any) ([]*shttp.Request, error) {
	var newRequests []*shttp.Request

	for _, output := range outputs {
		if output.IsRequest() && output.IsItem() {
			s.logger.Warn("spider output has both Request and Item set, Item will be ignored",
				"request", output.Request.String(),
			)
		}
		if output.IsRequest() {
			newRequests = append(newRequests, output.Request)
		} else if output.IsItem() {
			s.logger.Debug("scraped item", "item", output.Item)
			// 并发处理 Item：获取信号量后在 goroutine 中执行 Pipeline 链
			item := output.Item
			s.itemSem <- struct{}{} // 获取信号量（阻塞直到有空位）
			s.itemWg.Add(1)         // 增加 WaitGroup 计数
			go func(it any) {
				defer s.itemWg.Done()
				defer func() { <-s.itemSem }() // 释放信号量
				// panic recovery: 防止 Pipeline 中的 panic 导致进程崩溃
				defer func() {
					if r := recover(); r != nil {
						s.logger.Error("panic recovered in pipeline processing",
							"panic", r,
						)
						s.stats.IncValue("spider_exceptions/panic", 1, 0)
					}
				}()
				_, err := s.itemProc.ProcessItem(ctx, it, response)
				if err != nil && !errors.Is(err, serrors.ErrDropItem) {
					// Pipeline 处理错误（非 DropItem），记录但不中断
					s.logger.Error("pipeline failed to process item",
						"error", err,
					)
				}
			}(item)
		}
	}

	return newRequests, nil
}

// handleSpiderError 处理 Spider 回调异常。
func (s *Scraper) handleSpiderError(err error, request *shttp.Request, response *shttp.Response) {
	// CloseSpider 不记录为错误
	if errors.Is(err, serrors.ErrCloseSpider) {
		return
	}

	s.logger.Error("spider callback error",
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
