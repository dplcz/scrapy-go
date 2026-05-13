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
