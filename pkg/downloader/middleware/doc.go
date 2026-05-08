// Package middleware 定义了下载器中间件的接口和内置实现。
//
// # 概述
//
// middleware 包提供下载器中间件的接口定义和一系列内置中间件实现。
// 下载器中间件拦截请求和响应，可以修改、过滤或短路请求处理流程。
// 对应 Scrapy Python 版本中 scrapy.downloadermiddlewares 模块的功能。
//
// # 接口隔离设计（ISP）
//
// Go 版本采用接口隔离原则（Interface Segregation Principle），将原本的
// 单一中间件接口拆分为三个细粒度接口：
//
//   - [RequestProcessor]：仅处理请求（正序调用）
//   - [ResponseProcessor]：仅处理响应（逆序调用）
//   - [ExceptionProcessor]：仅处理异常（逆序调用）
//
// 中间件只需实现自己关心的接口，无需为不需要的方法提供空实现。
// 原有的 [DownloaderMiddleware] 全功能接口保留以实现向后兼容。
//
// # 处理流程
//
// 中间件链的调用顺序：
//
//	请求方向（正序）：
//	  MW1.ProcessRequest → MW2.ProcessRequest → ... → Downloader
//
//	响应方向（逆序）：
//	  Downloader → ... → MW2.ProcessResponse → MW1.ProcessResponse
//
//	异常方向（逆序）：
//	  Error → ... → MW2.ProcessException → MW1.ProcessException
//
// # 返回值语义
//
// ProcessRequest 返回值：
//   - (nil, nil)：继续处理链，将请求传递给下一个中间件
//   - (*Response, nil)：短路，直接返回响应，跳过后续中间件和下载器
//   - (nil, error)：触发 ExceptionProcessor 链
//
// ProcessResponse 返回值：
//   - (*Response, nil)：继续处理链（可修改响应后传递）
//   - (nil, error)：触发异常处理
//
// ProcessException 返回值：
//   - (nil, nil)：继续异常处理链
//   - (*Response, nil)：将异常转换为响应（恢复正常流程）
//   - (nil, error)：继续传播（可能是不同的错误）
//
// # 内置中间件
//
// 本包提供以下内置中间件（按默认优先级排序）：
//
//	┌──────────────────────────────────────────────────────────┐
//	│  优先级  │  中间件                │  功能                 │
//	├──────────────────────────────────────────────────────────┤
//	│   100   │  RobotsTxtMiddleware   │  robots.txt 遵守      │
//	│   300   │  HttpAuthMiddleware    │  HTTP Basic Auth      │
//	│   350   │  DownloadTimeoutMW     │  下载超时控制          │
//	│   400   │  UserAgentMiddleware   │  User-Agent 设置      │
//	│   500   │  RetryMiddleware       │  失败重试             │
//	│   550   │  DefaultHeadersMW      │  默认请求头           │
//	│   600   │  RedirectMiddleware    │  重定向处理           │
//	│   700   │  CookiesMiddleware     │  Cookie 管理          │
//	│   750   │  HttpProxyMiddleware   │  HTTP 代理            │
//	│   810   │  HttpCompressionMW     │  响应解压缩           │
//	│   850   │  StatsMiddleware       │  下载统计             │
//	│   900   │  HttpCacheMiddleware   │  HTTP 缓存            │
//	└──────────────────────────────────────────────────────────┘
//
// # 使用方式
//
// 实现自定义中间件（只需实现关心的接口）：
//
//	// 仅处理请求的中间件
//	type MyRequestMW struct{}
//
//	func (m *MyRequestMW) ProcessRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
//	    req.Headers.Set("X-Custom", "value")
//	    return nil, nil // 继续处理链
//	}
//
// 使用全功能接口（向后兼容）：
//
//	type MyFullMW struct {
//	    middleware.BaseDownloaderMiddleware // 嵌入默认实现
//	}
//
//	func (m *MyFullMW) ProcessRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
//	    // 自定义逻辑
//	    return nil, nil
//	}
//
// # 与 Scrapy 的差异
//
//   - 采用接口隔离原则（ISP），中间件无需实现全部三个方法
//   - 使用 [BaseDownloaderMiddleware] 提供默认空实现（可选嵌入）
//   - 重试和重定向通过 [errors.NewRequestError] 产生新请求，替代 Scrapy 的返回 Request 对象
//   - Cookies 中间件基于 net/http/cookiejar + sync.RWMutex 实现多会话隔离
//   - HttpCompression 中间件支持 gzip/deflate/br(brotli) 三种编码
//   - RobotsTxt 中间件内置 robots.txt 解析器，支持缓存和并发安全
//   - HttpCache 中间件支持 Dummy 策略和 RFC 2616 策略，文件存储后端
package middleware
