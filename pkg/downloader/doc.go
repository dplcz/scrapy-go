// Package downloader 实现了 scrapy-go 框架的下载器系统。
//
// # 概述
//
// downloader 包负责执行 HTTP 请求并返回响应，是框架中实际发起网络请求的组件。
// 通过 Slot 机制实现按域名分组的并发控制和请求延迟。
// 对应 Scrapy Python 版本中 scrapy.core.downloader 模块的功能。
//
// # 核心类型
//
// 本包提供以下核心类型：
//   - [Downloader]：下载器，管理 Slot 和全局并发控制
//   - [Slot]：域名级别的并发和延迟控制器
//   - [DownloadHandler]：下载处理器接口（协议适配层）
//   - [HTTPDownloadHandler]：基于 net/http 的 HTTP/1.1 标准下载处理器
//   - [HTTP2DownloadHandler]：HTTP/2 优化下载处理器，支持多路复用和 ALPN 自动协商
//   - [ProgressHTTPDownloadHandler]：支持下载进度回调的处理器
//   - [ConnPoolConfig]：连接池精细化配置（14 项参数）
//   - [ConnPoolStats]：连接池运行时统计（atomic 无锁）
//   - [ManagedTransport]：带统计功能的 http.Transport 包装
//   - [MiddlewareManager]：下载器中间件管理器
//   - [MiddlewareEntry]：带优先级的中间件条目
//
// # 架构设计
//
// Downloader 采用 Slot 机制实现精细化的并发控制：
//
//	                    Engine
//	                      │
//	                      ▼
//	┌─────────────────────────────────────────┐
//	│              Downloader                  │
//	│  ┌─────────────────────────────────┐    │
//	│  │     全局 active 集合             │    │
//	│  │  (CONCURRENT_REQUESTS 上限)      │    │
//	│  └─────────────────────────────────┘    │
//	│       │           │           │         │
//	│       ▼           ▼           ▼         │
//	│  ┌────────┐ ┌────────┐ ┌────────┐      │
//	│  │ Slot A │ │ Slot B │ │ Slot C │      │
//	│  │(域名A) │ │(域名B) │ │(域名C) │      │
//	│  └────────┘ └────────┘ └────────┘      │
//	└─────────────────────────────────────────┘
//	                      │
//	                      ▼
//	              DownloadHandler
//	       ┌──────────┼──────────┐
//	       ▼          ▼          ▼
//	  HTTP/1.1    HTTP/2     Progress
//	  Handler     Handler    Handler
//
// # Slot 调度模型
//
// 每个 [Slot] 对应一个域名/IP，内部采用队列驱动模型：
//  1. 请求入队到 Slot 的 channel
//  2. processQueue goroutine 串行出队
//  3. 通过 lastSeen 时间戳精确控制请求间隔（DOWNLOAD_DELAY）
//  4. 通过 [semaphore.Weighted] 控制并发传输数（CONCURRENT_REQUESTS_PER_DOMAIN）
//  5. 不同 Slot 之间完全并行
//
// # 中间件管理器
//
// [MiddlewareManager] 管理下载器中间件链，支持接口隔离（ISP）：
//   - ProcessRequest：按优先级正序调用
//   - ProcessResponse：按优先级逆序调用
//   - ProcessException：按优先级逆序调用
//
// 中间件只需实现关心的接口，Manager 通过类型断言自动适配。
//
// # 下载处理器选择
//
// 框架根据配置自动选择合适的下载处理器：
//   - HTTP2_ENABLED=true → [HTTP2DownloadHandler]（HTTP/2 多路复用 + 透明降级）
//   - DOWNLOAD_PROGRESS_ENABLED=true → [ProgressHTTPDownloadHandler]（进度回调）
//   - 默认 → [HTTPDownloadHandler]（HTTP/1.1 标准处理器）
//
// [HTTP2DownloadHandler] 使用 x/net/http2 直接建立 HTTP/2 连接，
// 当服务器不支持 HTTP/2 时自动降级到 HTTP/1.1（通过 fallbackTransport）。
//
// [ProgressHTTPDownloadHandler] 通过 request.Meta["download_progress_callback"] 设置回调函数，
// 支持已知/未知大小响应的进度报告，可配置最小报告间隔避免回调过于频繁。
//
// # 连接池管理
//
// [ConnPoolConfig] 提供 14 项连接池参数的精细化配置：
//   - MaxIdleConns / MaxIdleConnsPerHost / MaxConnsPerHost：连接数控制
//   - IdleConnTimeout / DialTimeout / TLSHandshakeTimeout：超时控制
//   - DisableKeepAlives / ForceHTTP2：协议行为控制
//   - WriteBufferSize / ReadBufferSize：缓冲区大小
//   - TLSInsecureSkipVerify：TLS 验证控制
//
// [ManagedTransport] 包装 http.Transport，通过 [ConnPoolStats] 提供
// 连接创建/复用/关闭/TLS 握手等运行时统计（atomic 无锁，零竞争开销）。
//
// 连接池配置通过 CONNPOOL_* 前缀的 Settings 键名读取，
// 使用 [ConnPoolConfigFromSettings] 函数从 Settings 中构建配置。
//
// # 配置项
//
// Downloader 通过 Settings 读取以下配置：
//   - CONCURRENT_REQUESTS：全局最大并发数（默认 16）
//   - CONCURRENT_REQUESTS_PER_DOMAIN：每域名最大并发数（默认 8）
//   - DOWNLOAD_DELAY：请求间隔（默认 0）
//   - RANDOMIZE_DOWNLOAD_DELAY：是否随机化延迟（默认 true）
//   - DOWNLOAD_TIMEOUT：下载超时时间（默认 180s）
//   - HTTP2_ENABLED：启用 HTTP/2 优化处理器（默认 false）
//   - DOWNLOAD_PROGRESS_ENABLED：启用下载进度回调（默认 false）
//   - DOWNLOAD_PROGRESS_MIN_INTERVAL：进度报告最小间隔（默认 100ms）
//   - CONNPOOL_MAX_IDLE_CONNS：最大空闲连接总数（默认 100）
//   - CONNPOOL_MAX_IDLE_CONNS_PER_HOST：每 host 最大空闲连接数（默认 10）
//   - CONNPOOL_MAX_CONNS_PER_HOST：每 host 最大连接数（默认 0，不限制）
//   - CONNPOOL_IDLE_CONN_TIMEOUT：空闲连接超时（默认 90s）
//   - CONNPOOL_TLS_HANDSHAKE_TIMEOUT：TLS 握手超时（默认 10s）
//   - CONNPOOL_DIAL_TIMEOUT：TCP 连接超时（默认 30s）
//   - CONNPOOL_DISABLE_KEEPALIVES：禁用 HTTP keep-alive（默认 false）
//
// # 信号系统集成
//
// Downloader 在关键节点发送以下信号：
//   - RequestReachedDownloader：请求到达下载器
//   - RequestLeftDownloader：请求离开下载器
//   - ResponseDownloaded：响应下载完成
//
// # 与 Scrapy 的差异
//
//   - 使用 [semaphore.Weighted] 替代 Twisted DeferredSemaphore 控制并发
//   - 使用 goroutine + channel 替代 Twisted callLater 实现延迟控制
//   - 使用 sync.RWMutex 保护共享状态，替代 Twisted 的协作式并发
//   - 全局 active 集合的添加/移除由 Engine 在同步路径中完成（对齐 Scrapy 原版）
//   - Slot GC 机制自动清理空闲超时的 Slot，防止内存泄漏
//   - 禁用 net/http 自动重定向和自动解压，由中间件统一管理
//   - 支持通过 request.Meta["proxy"] 动态设置代理
//   - 内置 HTTP/2 支持（Scrapy 依赖 Twisted 的 h2 库，配置更复杂）
//   - 连接池参数可通过 Settings 精细化配置（Scrapy 受限于 Twisted reactor）
//   - 下载进度回调通过 Meta 传递（Scrapy 通过 Signal 实现，粒度较粗）
//
// # 并发安全
//
// [Downloader] 的所有公共方法均为并发安全。
// [Slot] 内部通过 channel 和 semaphore 实现并发控制，无需外部同步。
package downloader
