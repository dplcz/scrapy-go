// Package signal 实现了 scrapy-go 框架的信号/事件系统。
//
// # 概述
//
// signal 包提供了一个发布-订阅（Pub/Sub）模式的信号系统，用于框架内部组件之间的
// 松耦合通信。通过 [Manager] 管理信号的注册和分发，通过 [Signal] 枚举定义所有
// 框架内置信号。对应 Scrapy Python 版本中 scrapy.signals 和 scrapy.signalmanager 模块。
//
// # 架构定位
//
// 信号系统是 scrapy-go 框架的事件总线，连接 Engine、Extension、Stats 等组件：
//
//	┌─────────────────────────────────────────────────────────┐
//	│                      Engine                             │
//	│  (发送信号: EngineStarted, SpiderOpened, SpiderIdle...) │
//	└────────────────────────┬────────────────────────────────┘
//	                         │ Send / SendCatchLog
//	                         ▼
//	┌─────────────────────────────────────────────────────────┐
//	│                 Signal Manager                           │
//	│            (路由信号到已注册的处理器)                     │
//	└───┬──────────────┬──────────────┬──────────────┬────────┘
//	    │              │              │              │
//	    ▼              ▼              ▼              ▼
//	┌────────┐   ┌──────────┐  ┌──────────┐  ┌──────────┐
//	│CoreStats│  │CloseSpider│  │ LogStats │  │MemUsage  │
//	└────────┘   └──────────┘  └──────────┘  └──────────┘
//	                    Extension 层
//
// # 信号类型
//
// 框架定义了以下内置信号（按生命周期顺序）：
//
// 引擎生命周期：
//   - [EngineStarted]：引擎启动完成
//   - [EngineStopped]：引擎停止
//
// Spider 生命周期：
//   - [SpiderOpened]：Spider 打开（组件初始化完成）
//   - [SpiderIdle]：Spider 空闲（无待处理请求）
//   - [SpiderClosed]：Spider 关闭
//   - [SpiderError]：Spider 回调发生错误
//
// 请求生命周期：
//   - [RequestScheduled]：请求被调度器接受入队
//   - [RequestDropped]：请求被丢弃（如去重过滤）
//   - [RequestReachedDownloader]：请求到达下载器
//   - [RequestLeftDownloader]：请求离开下载器
//
// 响应生命周期：
//   - [ResponseReceived]：引擎收到下载响应
//   - [ResponseDownloaded]：HTTP 响应下载完成
//   - [HeadersReceived]：收到 HTTP 响应头
//   - [BytesReceived]：收到响应字节数据
//
// Item 生命周期：
//   - [ItemScraped]：Item 成功通过所有 Pipeline
//   - [ItemDropped]：Item 被 Pipeline 丢弃
//   - [ItemError]：Pipeline 处理 Item 时发生错误
//
// 调度器：
//   - [SchedulerEmpty]：调度器队列为空
//
// # 处理器注册
//
// 通过 [Manager.Connect] 注册信号处理器，返回处理器 ID 用于后续注销：
//
//	id := manager.Connect(func(params map[string]any) error {
//	    // 处理信号
//	    return nil
//	}, signal.SpiderOpened)
//
//	// 注销处理器
//	manager.Disconnect(id, signal.SpiderOpened)
//
// # 信号发送
//
// Manager 提供三种信号发送方式：
//   - [Manager.Send]：同步发送，返回所有处理器的错误列表
//   - [Manager.SendCatchLog]：同步发送，自动将错误记录到日志（最常用）
//   - [Manager.SendCatchLogCtx]：带 context 的发送，支持取消
//
// 所有发送方式都保证：即使某个处理器返回错误，后续处理器仍会被调用。
//
// # 特殊错误语义
//
// 信号处理器可以返回特殊错误来影响框架行为：
//   - [errors.ErrDontCloseSpider]：在 SpiderIdle 信号中返回，阻止 Spider 关闭
//   - [errors.ErrCloseSpider]：请求关闭 Spider
//
// 辅助函数 [ContainsDontCloseSpider] 和 [ContainsCloseSpider] 用于检查错误列表中
// 是否包含这些特殊错误。
//
// # 与 Scrapy 的差异
//
//   - 使用 sync.RWMutex 保证并发安全（Scrapy 依赖 GIL + PyDispatcher）
//   - 使用 iota 枚举替代 Python 的 object() 信号标识
//   - 使用 uint64 ID 替代 Python 的 weakref 进行处理器管理
//   - 处理器快照机制：发送信号时先复制处理器列表，避免遍历时被修改
//   - 支持 context.Context 取消（SendCatchLogCtx），Scrapy 无此机制
//   - 舍弃 PyDispatcher 的 sender 参数，使用 params map 传递上下文
//
// # 并发安全
//
// [Manager] 的所有公共方法均为并发安全。信号发送时使用处理器快照，
// 允许处理器在执行过程中注册或注销其他处理器而不会导致死锁。
package signal
