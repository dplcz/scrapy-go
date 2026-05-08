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
//   - [HTTPDownloadHandler]：基于 net/http 的 HTTP/HTTPS 下载处理器
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
//	            (net/http 客户端)
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
// # 配置项
//
// Downloader 通过 Settings 读取以下配置：
//   - CONCURRENT_REQUESTS：全局最大并发数（默认 16）
//   - CONCURRENT_REQUESTS_PER_DOMAIN：每域名最大并发数（默认 8）
//   - DOWNLOAD_DELAY：请求间隔（默认 0）
//   - RANDOMIZE_DOWNLOAD_DELAY：是否随机化延迟（默认 true）
//   - DOWNLOAD_TIMEOUT：下载超时时间（默认 180s）
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
//
// # 并发安全
//
// [Downloader] 的所有公共方法均为并发安全。
// [Slot] 内部通过 channel 和 semaphore 实现并发控制，无需外部同步。
package downloader
