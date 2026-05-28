// Package proxy 提供独立的代理池管理器实现，是 scrapy-go 框架的 contrib 扩展。
//
// 本模块作为可选组件提供以下能力：
//
//   - 多种代理来源（Provider）：静态列表、文件加载、HTTP API 拉取
//   - 多种轮换策略（Strategy）：轮询（RoundRobin）、随机（Random）、加权（Weighted）
//   - 后台健康检查：周期性探测代理可用性，失败自动降权或剔除
//   - 失败自动重试：与 RetryMiddleware 协作，下载失败时自动切换代理
//   - 请求级精细控制：支持通过 Request.Meta["proxy"] 显式指定代理
//   - 零侵入：未引入此模块时主模块不依赖任何代理相关额外代码
//
// # 与内置 HttpProxyMiddleware 的关系
//
// 内置 pkg/downloader/middleware.HttpProxyMiddleware 仍然保留，
// 提供基础的环境变量代理支持（HTTP_PROXY/HTTPS_PROXY）。
// 本模块通过更高优先级注册（默认 priority=740，先于内置 750），
// 当从代理池为请求选定代理后，内置中间件检测到 Meta["proxy"] 已设置时直接跳过，
// 避免双重处理。
//
// # 核心组件
//
//   - Proxy: 代理实体，封装 URL、认证、权重、健康状态
//   - ProxyPool: 代理池接口，提供 Get/Mark/Snapshot 等基础能力
//   - Strategy: 轮换策略接口，由 Pool 在 Get 时调用以选择代理
//   - HealthChecker: 健康检查器，后台 goroutine 周期性探测
//   - Provider: 代理来源接口，负责将外部代理列表注入 Pool
//   - ProxyMiddleware: 实现 DownloaderMiddleware，将代理池接入下载链路
//
// # 设计决策
//
//   - 读多写少场景使用 sync.RWMutex；轮询计数使用 atomic.Uint64 实现无锁
//   - 健康检查后台 goroutine 通过 context 控制生命周期，确保零泄漏
//   - 加权策略使用预构建累积权重数组 + sort.SearchInts 二分查找 O(log n)
//   - Random 策略使用 math/rand/v2，无锁且无需用户初始化种子
//   - 失败重试通过返回 errors.NewRequestError 由 Engine 重新调度，复用现有重试基础设施
//
// # 使用示例
//
//	opts := proxy.DefaultOptions()
//	opts.Strategy = proxy.StrategyWeighted
//	opts.HealthCheckInterval = 30 * time.Second
//
//	provider := proxy.NewStaticProvider([]string{
//	    "http://user:pass@proxy1.example.com:8080",
//	    "http://proxy2.example.com:8080",
//	})
//
//	pool, err := proxy.NewPool(opts, provider)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer pool.Close()
//
//	mw := proxy.NewMiddleware(pool, nil)
//	// 通过 Crawler 注册中间件，优先级建议 740（先于内置 HttpProxy 750）
package proxy
