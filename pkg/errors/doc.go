// Package errors 定义了 scrapy-go 框架中跨包共享的错误类型。
//
// # 概述
//
// errors 包收纳了需要在多个包之间共享语义契约的错误——即某个包抛出后，
// 其他包需要通过 errors.Is 进行识别并据此改变行为的框架级语义错误。
// 对应 Scrapy Python 版本中 scrapy.exceptions 模块的功能。
//
// # 职责边界
//
// 本包仅包含跨包共享的框架级错误。对于仅在单个包内使用的局部错误
// （如 Runner 生命周期错误、Queue 满错误等），应遵循 Go 官方惯用法
// 就近定义在其所属包中（参考 io.EOF、sql.ErrNoRows 的放置方式）。
//
// # 哨兵错误（Sentinel Errors）
//
// 框架定义了以下哨兵错误，使用 errors.Is 进行匹配：
//
// 组件配置：
//   - [ErrNotConfigured]：组件未配置，框架将跳过该组件
//
// Spider 控制：
//   - [ErrCloseSpider]：请求关闭 Spider
//   - [ErrDontCloseSpider]：阻止 Spider 关闭（在 SpiderIdle 信号中使用）
//
// 请求处理：
//   - [ErrIgnoreRequest]：决定不处理某个请求
//   - [ErrNewRequest]：中间件希望用新请求替代当前请求
//
// Item 处理：
//   - [ErrDropItem]：从 Pipeline 中丢弃一个 Item
//
// 下载错误：
//   - [ErrDownloadTimeout]：下载超时
//   - [ErrDownloadFailed]：下载失败
//   - [ErrConnectionRefused]：连接被拒绝
//   - [ErrCannotResolveHost]：无法解析主机名
//   - [ErrResponseDataLoss]：响应数据不完整
//   - [ErrStopDownload]：停止下载响应体
//
// 其他：
//   - [ErrInvalidOutput]：中间件返回了无效的输出类型
//   - [ErrNotSupported]：不支持的功能或方法
//   - [ErrPanic]：从 panic 中恢复的错误
//
// # 结构化错误类型
//
// 对于需要携带额外上下文信息的错误，包提供了对应的结构化类型：
//
//   - [CloseSpiderError]：带关闭原因的 CloseSpider 错误
//   - [DropItemError]：带消息的 DropItem 错误
//   - [StopDownloadError]：带 fail 标志的 StopDownload 错误
//   - [NewRequestError]：携带新请求的替换错误
//   - [NotConfiguredError]：带消息的 NotConfigured 错误
//   - [PanicError]：带 panic 值和堆栈信息的错误
//
// 所有结构化错误类型都实现了 errors.Is 接口，可以匹配对应的哨兵错误：
//
//	err := errors.NewCloseSpiderError("timeout")
//	errors.Is(err, errors.ErrCloseSpider) // true
//
// # 错误创建
//
// 每种结构化错误都提供了对应的构造函数：
//   - [NewCloseSpiderError](reason)
//   - [NewDropItemError](message)
//   - [NewStopDownloadError](fail)
//   - [NewNewRequestError](request, reason)
//   - [NewNotConfiguredError](message)
//   - [NewPanicError](value, stack)
//
// # 辅助函数
//
// [IsRetryable] 判断错误是否可重试，以下错误类型被认为是可重试的：
//   - ErrDownloadTimeout
//   - ErrConnectionRefused
//   - ErrDownloadFailed
//   - ErrResponseDataLoss
//   - ErrCannotResolveHost
//
// # 与 Scrapy 的差异
//
//   - 使用 Go 的 errors.Is/errors.As 替代 Python 的 isinstance 类型检查
//   - 使用哨兵错误 + 结构化错误的双层设计（Scrapy 只有异常类）
//   - [NewRequestError] 替代 Scrapy 中通过 Meta 键传递新请求的 hack 方式
//   - [PanicError] 是 Go 特有的（Python 无 panic 机制）
//   - [StopDownloadError] 的 Fail 标志决定部分响应传递给 callback 还是 errback
//   - 使用 any 类型避免循环依赖（NewRequestError.Request）
//
// # 使用模式
//
// 中间件中丢弃请求：
//
//	func (m *MyMiddleware) ProcessRequest(req *http.Request) error {
//	    if shouldIgnore(req) {
//	        return errors.ErrIgnoreRequest
//	    }
//	    return nil
//	}
//
// Pipeline 中丢弃 Item：
//
//	func (p *MyPipeline) ProcessItem(ctx context.Context, item any) (any, error) {
//	    if isDuplicate(item) {
//	        return nil, errors.NewDropItemError("duplicate item")
//	    }
//	    return item, nil
//	}
//
// 信号处理器中阻止 Spider 关闭：
//
//	manager.Connect(func(params map[string]any) error {
//	    if hasMoreWork() {
//	        return errors.ErrDontCloseSpider
//	    }
//	    return nil
//	}, signal.SpiderIdle)
package errors
