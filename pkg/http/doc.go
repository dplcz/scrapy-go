// Package http 定义了 scrapy-go 框架的 HTTP 请求和响应模型。
//
// # 概述
//
// http 包是 scrapy-go 框架最底层的数据模型包，不依赖任何其他框架包。
// 它提供了 [Request] 和 [Response] 两个核心类型，以及多种便捷的请求构造器。
// 对应 Scrapy Python 版本中 scrapy.http 模块的功能。
//
// # 核心类型
//
// 本包提供以下核心类型：
//   - [Request]：HTTP 请求，携带 URL、方法、头部、Body、回调函数等信息
//   - [Response]：HTTP 响应，携带状态码、头部、Body，并集成 CSS/XPath 选择器
//   - [CallbackRegistry]：回调函数注册表，用于 Request 序列化/反序列化时恢复函数引用
//   - [FormField] / [FormFile]：Multipart 表单字段和文件定义
//   - [FormLocator]：HTML 表单定位选项
//
// # 请求构造器
//
// 本包提供多种请求构造器，覆盖常见的 HTTP 请求场景：
//
//	┌───────────────────────────────────────────────────────────────┐
//	│                    请求构造器                                 │
//	├───────────────────────────────────────────────────────────────┤
//	│  NewRequest          — 通用 HTTP 请求（GET/POST/...）         │
//	│  NewFormRequest      — 表单请求（URL-encoded）                │
//	│  NewJSONRequest      — JSON API 请求                          │
//	│  NewMultipartFormRequest — 文件上传请求（multipart/form-data）│
//	│  FormRequestFromResponse — 从 HTML 表单自动填充并提交         │
//	│  FromCURL            — 从 curl 命令创建请求                   │
//	│  FromDict            — 从序列化字典恢复请求                   │
//	└───────────────────────────────────────────────────────────────┘
//
// # Functional Options 模式
//
// 所有请求构造器均使用 Functional Options 模式配置，提供类型安全的可选参数：
//
//	req, err := http.NewRequest("https://example.com",
//	    http.WithMethod("POST"),
//	    http.WithHeader("Content-Type", "application/json"),
//	    http.WithBody(jsonBytes),
//	    http.WithCallback(mySpider.ParseDetail),
//	    http.WithPriority(10),
//	)
//
// 可用的 [RequestOption] 包括：
//   - [WithMethod]：设置 HTTP 方法
//   - [WithHeaders] / [WithHeader]：设置请求头
//   - [WithBody] / [WithRawBody]：设置请求体
//   - [WithCookies]：设置 Cookie
//   - [WithMeta]：设置元数据（组件间传递上下文）
//   - [WithPriority]：设置调度优先级
//   - [WithDontFilter]：跳过去重过滤
//   - [WithCallback] / [WithErrback]：设置回调/错误回调
//   - [WithFlags]：设置请求标记
//   - [WithCbKwargs]：设置回调额外参数
//   - [WithEncoding]：设置请求体编码
//   - [WithBasicAuth]：设置 HTTP Basic Auth
//   - [WithUserAgent]：设置 User-Agent
//   - [WithFormData]：设置表单数据
//
// # Response 选择器集成
//
// [Response] 类型集成了 CSS 和 XPath 选择器，可直接在响应上执行查询：
//
//	// CSS 选择器
//	titles := response.CSS("h1.title::text").GetAll()
//	links := response.CSSAttr("a", "href").GetAll()
//
//	// XPath 选择器
//	quotes := response.XPath("//div[@class='quote']/text()").GetAll()
//
//	// 跟踪链接
//	req, err := response.Follow("/next-page", http.WithCallback(mySpider.ParseNext))
//
// # 序列化与反序列化
//
// Request 支持序列化为 map[string]any 字典（用于磁盘队列持久化、断点续爬）：
//
//	// 序列化
//	d := req.ToDict("ParseDetail", "HandleError")
//	jsonBytes, _ := json.Marshal(d)
//
//	// 反序列化
//	var d2 map[string]any
//	json.Unmarshal(jsonBytes, &d2)
//	req2, _ := http.FromDict(d2, registry)
//
// 由于 Go 中函数不可序列化，Callback/Errback 通过 [CallbackRegistry] 注册表
// 将函数映射为字符串名称，实现跨进程恢复。
//
// # Headers 工具函数
//
// 本包提供一组 Headers 工具函数，简化 http.Header 的操作：
//   - [NewHeaders]：从 map[string]string 创建 Header
//   - [NewHeadersFromMap]：从 map[string][]string 创建 Header
//   - [MergeHeaders]：合并两个 Header
//   - [HeadersToMap]：将 Header 转换为 map
//   - [GetContentType]：提取 Content-Type
//   - [GetEncoding]：提取字符编码
//   - [IsTextContentType]：判断是否为文本类型
//
// # 与 Scrapy 的差异
//
//   - 使用 Functional Options 模式替代 Python 的 kwargs 参数
//   - 使用 [CallbackRegistry] 注册表模式替代 Python 的 getattr 反射查找方法
//   - [Response] 直接集成选择器方法（CSS/XPath），无需额外导入
//   - 使用 Go 标准库 net/http.Header 替代 Scrapy 自定义的 Headers 类
//   - [FromCURL] 内置轻量级 shell 词法分析器，替代 Python 的 shlex.split + argparse
//   - [FormRequestFromResponse] 使用 goquery 解析 HTML 表单，替代 lxml
//   - Body 使用 base64 编码序列化（支持二进制内容），替代 Python 的 bytes 直接序列化
//   - 提供 Must* 系列函数（MustNewRequest 等），在确定参数有效时避免错误处理样板代码
//
// # 并发安全
//
// [Request] 和 [Response] 本身不是并发安全的（不应被多个 goroutine 同时修改）。
// 如需在多个 goroutine 间共享，请使用 [Request.Copy] 创建副本。
//
// [CallbackRegistry] 是并发安全的，所有方法均可被多个 goroutine 安全调用。
package http
