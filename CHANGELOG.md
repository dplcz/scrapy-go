# Changelog

本文件记录 scrapy-go 项目的所有重要变更。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。


## [v1.2.3] — 2026-05-20

> **🐛 Bug 修复 — ResponseDownloaded 信号发送顺序修正 + download_latency 设置**
>
> 修复下载器中 `ResponseDownloaded` 和 `RequestLeftDownloader` 信号的发送顺序，
> 对齐 Scrapy 原版语义。同时在 `DownloaderStatsMiddleware` 中将 `download_latency`
> 写入 `request.Meta`，供 AutoThrottle、Telemetry 等扩展消费。

### 修复

#### 修复 ResponseDownloaded 信号发送顺序

- 🐛 **问题描述** — `ResponseDownloaded` 信号原本在 `err != nil` 判断之后发送，但位于 `RequestLeftDownloader` 之后，不符合 Scrapy 原版语义（原版中 `response_downloaded` 在 `request_left_downloader` 之前发送）
- 🐛 **修复方案** — 将 `ResponseDownloaded` 信号移至 `err == nil` 判断后、`RequestLeftDownloader` 之前发送，确保顺序与 Scrapy 原版一致

#### 修复 download_latency 未写入 Request Meta

- 🐛 **问题描述** — `DownloaderStatsMiddleware` 计算了下载耗时但未将 `download_latency` 设置到 `request.Meta`，导致 AutoThrottle 和 Telemetry 扩展无法读取下载延迟
- 🐛 **修复方案** — 在 `ProcessResponse` 中计算 `elapsed` 后同时调用 `request.SetMeta("download_latency", elapsed)`

#### 清理 Telemetry 扩展中的无效参数读取

- 🐛 **问题描述** — `TraceExtension.onRequestLeftDownloader` 尝试从 `params["status"]` 和 `params["download_latency"]` 读取数据，但发送信号时从未传入这些参数
- 🐛 **修复方案** — 改为从 `request.GetMeta("download_latency")` 读取延迟；移除对 `params["status"]` 的无效读取；`onResponseReceived` 改为从 `params["response"]` 获取状态码

### 变更

- ♻️ **`RequestLeftDownloader` 信号参数** — 新增 `"error": err` 参数，供 Telemetry 扩展记录失败请求的错误信息
- ♻️ **子模块依赖更新** — `contrib/telemetry`、`contrib/web`、`contrib/redisqueue` 的 `go.sum` 更新以修复 `gjson` 依赖缺失

---

## [v1.2.2] — 2026-05-19

> **🚀 新增 Response JSON 选择器（gjson 集成）— P5-024**
>
> 基于 `github.com/tidwall/gjson` 为 Response 新增高性能 JSON 路径查询能力。
> 直接在 `[]byte` 上做路径查询，零中间分配，性能远优于先 Unmarshal 再取值。
> 与现有 `JSON(v any) error` 整体反序列化方法互补共存，不破坏旧 API。

### 新增

#### P5-024：Response JSON 选择器（gjson 集成）

- 🚀 **`Response.JSONGet(path string) gjson.Result`** — 使用 gjson 路径语法查询响应体中的 JSON 值，返回类型自带 `String/Int/Float/Bool/Array/Map/Time/Exists` 等访问器，避免 `map[string]any` 深层断言
- 🚀 **`Response.JSONGetMany(paths ...string) []gjson.Result`** — 一次扫描多路径提取，性能优于多次 `JSONGet`
- 🚀 **`Response.JSONExists(path string) bool`** — 检查指定路径是否存在，语义清晰的条件判断
- 🚀 **`Response.JSONForEach(path string, iter func(key, value gjson.Result) bool)`** — 流式遍历数组/对象，避免大数组一次性分配，支持提前终止
- 📦 **新增依赖** — `github.com/tidwall/gjson` v1.19.0（传递依赖：`tidwall/match` v1.1.1 + `tidwall/pretty` v1.2.0，体积极小）
- 📖 **示例爬虫** — `examples/json_api/` 演示从 JSON API 提取深层嵌套字段的完整开发体验

### 技术细节

- **选型理由** — gjson 直接在 `[]byte` 上做路径查询、零中间分配，对爬虫高频解析 JSON API 场景更友好；语法直观（点路径 + `#` 投影 + `#(cond)` 过滤 + `|` 修饰符管道）
- **API 命名规范** — 所有新方法以 `JSON` 前缀开头（`JSONGet`/`JSONGetMany`/`JSONExists`/`JSONForEach`），与现有 `JSON(v any) error` 形成清晰族群
- **向后兼容** — 现有 `JSON(v any) error` 方法行为零变更
- **测试覆盖** — 新增 30+ 测试用例，覆盖率 100%（路径不存在、空 body、非 JSON body、嵌套数组投影、条件过滤、ForEach 提前终止等）
- **竞态安全** — `go test -race` 通过，gjson 本身为无状态纯函数调用

---

## [v1.2.1] — 2026-05-19

> **🐛 Bug 修复 — 扩展/中间件组件引用失效问题**
>
> 修复了 `crawl()` 中因 Spider `CustomSettings` 重建 Logger/Signals/Stats 时，
> 导致之前注入的扩展（如 `TraceExtension`/`MetricsExtension`）内部持有的旧引用失效的问题。
> 同时引入 `CrawlerAwareExtension` 接口，让扩展在 `Open` 之前自动获取最新的框架组件引用。

### 修复

#### 修复 Signals/Stats 重建导致扩展引用失效

- 🐛 **问题描述** — 用户在 `Run` 之前通过 `NewMetricsExtension(registry, addr, c.Signals, c.Logger)` 构造扩展时，扩展保存了当时的 `c.Signals` 引用。但 `crawl()` 内部会因 Spider `CustomSettings` 可能覆盖 `LOG_LEVEL` 而重建 `c.Signals`，导致扩展内部的 Signals 引用指向已废弃的旧实例，信号处理器注册到旧实例上永远不会被触发
- 🐛 **修复方案** — 新增 `CrawlerAwareExtension` 可选接口，Extension Manager 在调用 `Open()` 之前自动调用 `FromCrawler(c)`，让扩展获取最新的 Signals/Logger/Stats 引用

#### 修复 `WithStats`/`WithSignals` 注入被覆盖

- 🐛 **问题描述** — 用户通过 `WithStats()`/`WithSignals()` Option 注入的自定义 Stats/Signals，在 `crawl()` 中被无条件覆盖（只要 `!c.customLogger` 为 true）
- 🐛 **修复方案** — 新增 `customSignals`/`customStats` 标记字段，`WithSignals()`/`WithStats()` 设置标记，`crawl()` 中按组件独立判断是否需要重建

### 新增

#### `CrawlerAwareExtension` 接口（`pkg/extension`）

- 🚀 **`extension.Crawler` 接口** — 定义 Extension 可访问的 Crawler 能力子集（`GetSettings()`/`GetStats()`/`GetSignals()`/`GetLogger()`），避免循环依赖
- 🚀 **`CrawlerAwareExtension` 接口** — 扩展可实现 `FromCrawler(c Crawler) error` 方法，在 `Open` 之前由 Manager 自动调用，获取最新的框架组件引用
- 🚀 **`Manager.SetCrawler(c)`** — Extension Manager 新增 `SetCrawler` 方法，由 `buildExtensions` 调用传入 Crawler 引用
- 🚀 **telemetry 扩展适配** — `TraceExtension` 和 `MetricsExtension` 实现 `CrawlerAwareExtension` 接口

### 变更

- ♻️ **`WithStats` Option** — 设置 `customStats = true` 标记，防止 `crawl()` 覆盖用户注入的 Stats
- ♻️ **`WithSignals` Option** — 设置 `customSignals = true` 标记，防止 `crawl()` 覆盖用户注入的 Signals
- ♻️ **`crawl()` 重建逻辑** — 从单一 `if !c.customLogger` 块拆分为三个独立判断（Logger/Signals/Stats），各自尊重对应的 custom 标记

---

## [v1.2.0] — 2026-05-18

> **🚀 v1.2.0 正式发布 — 生产增强里程碑 M7**
>
> 本版本是 scrapy-go 的生产增强里程碑，包含 14 项核心交付物：
> - 📊 **可观测性增强** — Prometheus Label 维度支持 + Grafana Dashboard 模板 + TraceExtension Span 生命周期增强
> - 💾 **持久化存储** — MongoDB/PostgreSQL/Elasticsearch 通用存储适配器
> - 🛡️ **生产稳定性** — 高级重试策略（指数退避+熔断器）+ 分布式限速器 + 内存队列溢出保护
> - 🌐 **分布式增强** — Redis 去重 Pipeline 批量优化 + 轻量级 REST API
> - 🏗️ **架构优化** — 下载器 HTTP/2 重构 + 公共类型包重构 + 泛型 Settings API
> - 🧬 **开发体验** — Meta 结构体序列化 + GetMetaAs 泛型辅助函数 + 断点续爬回调修复
>
> 详细变更日志见各子版本（v1.1.3 ~ v1.1.7）条目。

### 新增

#### P5-007g：Prometheus Label 维度支持

> **引入 `LabeledCounter`/`LabeledGauge`/`LabeledHistogram` 接口扩展，支持按域名/Spider 名称分组指标**

- 🚀 **核心接口定义** — 在 `pkg/telemetry/labeled.go` 中新增 `LabeledCounter`/`LabeledGauge`/`LabeledHistogram` 接口，支持通过 `With(labelValues...)` 获取指定标签值的指标实例
- 🚀 **`LabeledMetricsRegistry` 接口** — 扩展 `MetricsRegistry`，增加 `LabeledCounter()`/`LabeledGauge()`/`LabeledHistogram()` 方法，支持创建带标签维度的指标
- 🚀 **Prometheus 实现** — 在 `contrib/telemetry/prometheus/labeled.go` 中实现：
  - `LabeledCounter`：基于 `prometheus.CounterVec` 实现按标签值分组的计数器
  - `LabeledGauge`：基于 `prometheus.GaugeVec` 实现按标签值分组的仪表盘
  - `LabeledHistogram`：基于 `prometheus.HistogramVec` 实现按标签值分组的直方图
  - `LabeledRegistry`：扩展 `Registry`，支持创建带标签维度的指标，完全兼容 `MetricsRegistry` 接口
- 🛡️ **Noop 实现** — `NoopLabeledCounter`/`NoopLabeledGauge`/`NoopLabeledHistogram`/`NoopLabeledMetricsRegistry` 零开销空操作实现
- 🔒 **线程安全** — `LabeledRegistry` 使用 `sync.RWMutex` + 双重检查锁保护指标注册表；底层 `prometheus.*Vec` 保证并发安全
- ✅ **测试覆盖** — 15 个新测试用例覆盖：
  - 基本操作（Inc/Add/Set/Dec/Observe/ObserveDuration）
  - 幂等创建（同名指标返回同一实例）
  - 并发安全（100 goroutine 并发操作）
  - 接口可赋值性验证
  - 覆盖率 98.2%

#### P5-007h：Grafana Dashboard 模板

> **提供开箱即用的 Grafana JSON 模板（Spider 概览/请求延迟/错误率/队列深度）**

- 🚀 **Dashboard JSON 模板** — `contrib/telemetry/grafana/scrapy-go-dashboard.json`，包含 14 个面板：
  - 🕷️ Spider 概览：状态指示、运行时长、总请求/响应/Item/错误数
  - ⚡ 请求延迟：P50/P90/P99 分位数曲线、请求/响应 QPS 吞吐量
  - 🚨 错误率：错误占比曲线、错误与 Item 丢弃速率
  - 📊 队列深度：活跃请求数、调度器队列深度
  - 🌐 按域名维度：分域名 QPS、分域名延迟 P90
- 🚀 **模板变量** — 支持 `$spider`（Spider 名称过滤）和 `$domain`（域名过滤）动态变量
- 📖 **使用文档** — `contrib/telemetry/grafana/README.md` 包含面板说明、导入方式、Prometheus 抓取配置示例

### 技术细节

- **接口设计** — `LabeledMetricsRegistry` 嵌入 `MetricsRegistry`，保证向后兼容；`LabeledRegistry` 嵌入 `*Registry`，复用基础指标创建能力
- **标签维度** — 支持任意数量的标签名称（`labelNames ...string`），通过 `With(labelValues ...string)` 传入对应值
- **Grafana 兼容性** — Dashboard 使用 `__inputs` 机制，导入时自动提示选择数据源；兼容 Grafana 9.0+

---

## [v1.1.7] — 2026-05-18

> **🧬 P5-025 Meta 结构体序列化 + GetMetaAs 泛型辅助函数 | 🔭 P5-007f TraceExtension Span 生命周期增强**

### 新增

#### P5-025：Meta 结构体序列化 + `GetMetaAs[T]` 泛型辅助函数

> **增强 `isJSONSerializable` 支持结构体序列化，新增 `GetMetaAs[T]` / `GetRequestMetaAs[T]` 泛型辅助函数实现 Meta 值的类型安全还原**

##### P5-025a：`isJSONSerializable` 增强

- 🚀 **基于 `reflect.Kind` 的智能判断** — 使用反射判断值是否可 JSON 序列化（`pkg/http/request_dict.go`）
  - 允许带导出字段的 `Struct`、`*Struct`（指针）、`[]Struct`（切片）通过序列化检查
  - 对 `Func`/`Chan`/`UnsafePointer` 保持拒绝
  - 新增 `json.Marshal` 试探作为最终 fallback（仅对未知类型触发）
  - 结果通过 `sync.Map` 缓存，避免重复反射开销
- ⚡ **快路径零反射** — 基础类型和常见复合类型仍走 type switch 快路径，零反射零分配

##### P5-025b：`GetMetaAs[T]` 泛型辅助函数（Response）

- 🧬 **`GetMetaAs[T any](resp *Response, key string) (T, error)`** — 新增泛型辅助函数（`pkg/http/meta_helper.go`）
  - 快路径：直接类型断言成功时零分配零开销（未经过磁盘序列化的内存请求）
  - 慢路径：`json.Marshal` + `json.Unmarshal` 将 `map[string]any` 转换为目标结构体（兼容磁盘队列反序列化后的 map 形态）
  - 完善的错误处理：`ErrMetaNil`（nil meta）、`ErrMetaKeyNotFound`（key 不存在）、`ErrMetaConversion`（类型转换失败）

##### P5-025c：`GetRequestMetaAs[T]` 泛型辅助函数（Request）

- 🧬 **`GetRequestMetaAs[T any](req *Request, key string) (T, error)`** — Request 端对称 API
  - 与 `GetMetaAs[T]` 共享核心逻辑（内部 `metaConvert[T]` 私有泛型函数）
  - 适用于中间件中需要从 Request.Meta 恢复结构体的场景

##### P5-025d：单元测试

- ✅ **新增 `pkg/http/meta_helper_test.go`** — 30+ 测试用例覆盖：
  - 结构体序列化往返（`SetMeta` → 磁盘队列 → `GetMetaAs[T]` 正确还原）
  - 嵌套结构体、结构体切片、指针字段结构体
  - 直接类型断言快路径
  - 非 JSON 类型拒绝（func/chan/chan slice）
  - key 不存在容错、nil meta 容错、nil response/request 容错
  - `isJSONSerializable` 增强验证（struct/ptr/slice/map/array/cache）
  - 磁盘队列完整往返测试（ToDict → JSON → FromDict → GetMetaAs）

### 增强

#### P5-007f：TraceExtension Span 生命周期增强

> **引入 `sync.Map` 按 Request 指针关联活跃 Span，实现请求-响应完整追踪**

- 🚀 **请求-响应完整追踪** — `onRequestReachedDownloader` 创建子 Span 后按 `*http.Request` 指针存入 `sync.Map`，`onRequestLeftDownloader` 取出并结束对应 Span（`contrib/telemetry/extension.go`）
  - 支持乱序完成：多个并发请求各自独立追踪，不受完成顺序影响
  - 自动记录 HTTP 状态码（`http.status_code`）和下载延迟（`http.duration_ms`）
  - 错误状态码（>= 400）自动设置 `SpanStatusError`
  - 下载错误自动通过 `RecordError` 记录到 Span
- 🛡️ **Close 时清理未完成 Span** — Spider 关闭时自动结束所有活跃请求 Span，设置错误状态 "spider closed before request completed"
- 🔒 **并发安全** — 使用 `sync.Map` 替代普通 map，保证多 goroutine 并发访问安全
- ✅ **新增测试用例** — 4 个新测试覆盖 Span 生命周期增强场景：
  - `TestTraceExtension_SpanLifecycle`：多请求乱序完成、错误状态码、下载错误
  - `TestTraceExtension_SpanCleanupOnClose`：Close 时清理未完成 Span
  - `TestTraceExtension_DuplicateRequestLeft`：重复离开不 panic
  - `TestTraceExtension_RequestLeftWithoutReached`：孤立离开不 panic
- ✅ **更新并发测试** — `TestTraceExtension_ConcurrentSignals` 使用 Request 对象验证并发安全

### 变更

- ♻️ **`isJSONSerializable` 行为放宽** — 原本被拒绝的结构体值现在能被保留到 Meta 序列化中，属于向后兼容的行为放宽（不影响已有代码）
- ♻️ **`onRequestReachedDownloader` 重构** — 从 `params["url"]`/`params["method"]` 字符串参数改为直接使用 `params["request"].(*http.Request)` 对象，与信号系统实际传参对齐
- ♻️ **`onRequestLeftDownloader` 实现** — 从空操作改为完整的 Span 结束逻辑


---

## [v1.1.6] — 2026-05-15

> **🏗️ P5-023 下载器 HTTP/2 架构重构 + ⚡ Scheduler 单队列优先级修复**

### 重构

#### P5-023：下载器 HTTP/2 架构重构

> **删除冗余的 `HTTP2DownloadHandler`，将连接池统计（`ConnPoolStats`）集成至默认 `HTTPDownloadHandler`，新增 h2c 配置支持**

##### P5-023a：删除 `HTTP2DownloadHandler`

- 🗑️ **删除 `handler_h2.go`** — Go 标准库 `net/http` 已原生支持 HTTP/2 ALPN 自动协商，独立的 `HTTP2DownloadHandler` 属于对 Scrapy Python 版本的直译反模式
  - `http2.Transport` 直连方案在对端不支持时会失败后 fallback，造成双倍开销
  - 默认 Handler 自动协商后同样支持 HTTP/2 多路复用
  - Server Push 仅注释预留，未实现
  - 标准库 ALPN 协商失败直接用 HTTP/1.1，无需手动降级
- 🗑️ **删除 `handler_h2_test.go`** — 冗余测试文件，有效用例已迁移至 `handler_config_test.go`

##### P5-023b：连接池统计集成至默认 Handler

- 🚀 **`NewHTTPDownloadHandlerWithConfig(timeout, config)`** — 新增带精细化配置的构造函数（`pkg/downloader/handler.go`）
  - 支持 `ConnPoolConfig` 配置注入（连接池参数、HTTP/2 控制、h2c 支持等）
  - 集成 `ManagedTransport` 连接池运行时统计（`ConnPoolStats`）
  - 默认 `NewHTTPDownloadHandler` 行为完全不变（零影响）
- 📊 **`ConnPoolStats()` / `Config()` 访问器** — 新增连接池统计和配置查询方法
  - 通过 `NewHTTPDownloadHandlerWithConfig` 创建时可用，默认构造返回 nil

##### P5-023c：新增 `ForceHTTP2` / `AllowH2C` 配置项

- ⚙️ **`AllowH2C` 配置项** — 新增 HTTP/2 over cleartext（h2c）支持（`pkg/downloader/connpool.go`）
  - `AllowH2C=true` 时注册 `http2.Transport` 作为 `http://` scheme 的 handler
  - 通过自定义 `DialTLSContext` 建立纯 TCP 连接，实现无 TLS 的 HTTP/2 通信
  - 适用于内网/测试场景
- ⚙️ **`HTTP2_ALLOW_H2C` Settings 键** — 新增配置项（`pkg/settings/keys.go` + `defaults.go`）
- ⚙️ **`HTTP2_ENABLED` 语义更新** — 启用后通过默认 Handler 的 `ForceAttemptHTTP2=true` 实现，不再创建独立 Handler

##### P5-023d：单元测试 + 回归验证

- ✅ **新增 `handler_config_test.go`** — 20+ 测试用例覆盖：
  - `NewHTTPDownloadHandlerWithConfig` 基本功能（GET/POST/Cookies/重定向/超时/Context 取消）
  - `ConnPoolStats` 集成验证（WithConfig 非 nil，默认构造 nil）
  - HTTP/2 ALPN 自动协商验证
  - HTTP/1.1 fallback 验证
  - `AllowH2C` h2c 通信验证
  - `ForceHTTP2` 配置生效验证
  - HTTP/2 多路复用并发测试
  - `ConnPoolConfigFromSettings` AllowH2C 配置读取验证
  - `DownloadHandler` 接口兼容性验证

### 变更

- ♻️ **`crawler.go` Handler 选择逻辑重构** — `HTTP2_ENABLED=true` 时使用 `NewHTTPDownloadHandlerWithConfig` 替代已删除的 `NewHTTP2DownloadHandler`
- 📝 **`doc.go` 文档更新** — 移除 `HTTP2DownloadHandler` 引用，更新架构图和配置说明
- 📝 **`ConnPoolConfig` 文档增强** — 新增 `AllowH2C` 字段说明

### 向后兼容性

- ⚠️ **破坏性变更**：`HTTP2DownloadHandler` / `NewHTTP2DownloadHandler` 已删除
  - **迁移方式**：将 `NewHTTP2DownloadHandler(timeout, config)` 替换为 `NewHTTPDownloadHandlerWithConfig(timeout, config)`
  - `HTTP2_ENABLED=true` 配置仍然有效，框架内部已自动切换到新实现
  - 直接使用 `NewHTTPDownloadHandler(timeout)` 的代码不受影响
- ✅ `NewHTTPDownloadHandler` 默认行为完全不变
- ✅ `ConnPoolConfig` / `ConnPoolStats` / `ManagedTransport` 接口不变
- ✅ 所有现有 Settings 配置键兼容

> **⚡ Scheduler 单队列优先级修复 — 解决跨批次高优先级请求被"饿死"问题**

### 修复

#### Scheduler 全局优先级排序修复

> **修复双锁分离设计导致的跨批次优先级失效问题：当第一批大量低优先级请求未消费完时，回调中产生的高优先级请求无法及时参与全局排序，被低优先级请求"饿死"**

##### 问题根因

- 🐛 **双锁分离 + 双队列 swap 机制** — 原设计使用 `inBuffer`（入队缓冲区）和 `outQueue`（出队队列）两个独立的 `PriorityQueue`，仅当 `outQueue` 完全为空时才触发 swap
- 🐛 **优先级物理隔离** — 两个堆之间无法进行优先级比较，导致新入队的高优先级请求被困在 `inBuffer` 中，必须等待 `outQueue` 中所有低优先级请求消费完毕

##### 解决方案（方案 E：单队列 + 单锁）

- ♻️ **单优先级队列** — 将 `inBuffer` + `outQueue` 合并为单个 `PriorityQueue`（`pq` 字段），所有请求共享同一个优先级堆
- 🔒 **单锁保护** — 将 `enqueueMu` + `dequeueMu` 合并为单个 `sync.Mutex`（`mu` 字段），保证入队和出队操作的全局优先级一致性
- ⚡ **DupeFilter 锁外执行** — `RFPDupeFilter` 使用 `sync.Map.LoadOrStore` 原子操作，去重检查移至队列锁外执行，最小化临界区长度
- 📊 **atomic 无锁快速路径保留** — `HasPendingRequests`/`Len` 仍使用 `atomic.Int64`，零锁开销

##### 性能对比

| Benchmark | Before (双锁) | After (单锁) | 变化 |
|-----------|--------------|-------------|------|
| EnqueueDequeue (单线程) | ~154 ns/op | ~137 ns/op | **-11%** |
| ConcurrentEnqueue c16 | ~596 ns/op | ~761 ns/op | +28% (预期内) |
| ParallelEnqueueDequeue c16 | ~704 ns/op | ~382 ns/op | **-46%** |

##### 新增测试

- 🧪 **`TestCrossBatchPriorityOrdering`** — 验证跨批次入队时全局优先级排序正确性
- 🧪 **`TestCrossBatchPriorityWithConcurrentEnqueue`** — 验证并发入队时优先级排序正确性
- 🧪 **`TestPriorityInterleavedEnqueueDequeue`** — 验证交错入队/出队时优先级正确性
- 🧪 **`TestPriorityUnderHighConcurrency`** — 高并发下优先级排序最终一致性验证
- 📈 **`BenchmarkCrossBatchPriority`** — 跨批次优先级场景基准测试

### 向后兼容性

- ✅ **公共 API 完全不变** — `Scheduler` 接口、`DefaultScheduler` 所有公共方法签名不变
- ✅ **配置项完全不变** — `WithDupeFilter`/`WithJobDir`/`WithMemoryQueueThreshold`/`WithExternalQueue` 等所有 Option 不变
- ✅ **行为语义一致** — 出队优先级（内存 > 磁盘）、溢出保护、断点续爬等功能行为不变
- ⚠️ **内部字段变更** — `enqueueMu`/`dequeueMu`/`inBuffer`/`outQueue` 替换为 `mu`/`pq`（仅影响直接访问内部字段的测试代码）

---

## [v1.1.5] — 2026-05-14

> **🐛 P5-022 断点续爬回调序列化修复 + Meta 类型还原 + 性能基准测试**

### 修复

#### P5-022：断点续爬回调序列化修复

> **修复 `CallbackRegistry` 无法通过函数值反向查找注册名称的 bug，导致断点续爬时回调函数名称无法正确序列化**

##### P5-022a：`CallbackRegistry` 反向索引

- 🎯 **`LookupByFunc` / `LookupErrbackByFunc`** — 新增通过回调函数值反向查找注册名称的方法（`pkg/http/callback_registry.go`）
  - 策略 1：通过 `runtime.FuncForPC` 从函数值提取方法全限定名，解析出方法名后在注册表中验证
  - 策略 2：通过 `reflect.ValueOf().Pointer()` 在反向索引 map 中 O(1) 查找（fallback，适用于手动注册的匿名闭包）
  - 修复根因：原 `fmt.Sprintf("%v", registered) == fmt.Sprintf("%v", cb)` 比较方式失败——通过反射 `v.Method(i).Convert().Interface()` 获取的函数值与用户直接引用的 method value 闭包指针不同
  - ~150ns/op 零分配，O(1) 扩展性（3→50 方法耗时不变）

- 🔗 **反向索引自动维护** — `Register` / `RegisterErrback` 方法同时建立 `reflect.Pointer → 名称` 映射
  - 新增 `callbackPtrs` / `errbackPtrs` 字段（`map[uintptr]string`）
  - 注册时自动写入，无需额外调用

##### P5-022b：`RequestSerializer` 简化

- ♻️ **`lookupCallbackName` / `lookupErrbackName` 重构** — `pkg/scheduler/serializer.go`
  - 从 O(N) 遍历 + `fmt.Sprintf` 比较降为调用 `registry.LookupByFunc` O(1) 查找
  - 代码量减少 ~30 行，逻辑更清晰

##### P5-022c：`restoreMetaTypes` 类型还原

- 🔧 **Meta 类型还原** — 新增 `restoreMetaTypes` 函数（`pkg/http/request_dict.go`）
  - `FromDict` 反序列化后递归处理 meta map，将 JSON 解码产生的 `float64` 无损还原为 `int`
  - 判断条件：`float64(int(val)) == val`，确保无精度损失
  - 嵌套 `map[string]any` 和 `[]any` 递归处理
  - 解决 `encoding/json` 反序列化到 `any` 时所有数字变为 `float64` 的经典问题
  - 用户代码 `meta["page"].(int)` 类型断言不再失败

##### P5-022d：性能基准测试套件

- 📈 **新增基准测试** — `pkg/http/callback_registry_bench_test.go` + `pkg/scheduler/serializer_bench_test.go`
  - `LookupByFunc` 各路径 benchmark（method value / 反向索引 / 未注册）
  - `restoreMetaTypes` benchmark（4 字段 ~250ns/op 零分配）
  - 完整序列化/反序列化/往返 benchmark（~8.8μs/63allocs，支撑 ~11 万次/秒吞吐量）

##### P5-022e：脚手架工具增强

- 🔧 **`genspider` 模板更新** — `cmd/scrapy-go/genspider.go`
  - 生成的爬虫代码自动包含 `CallbackRegistry` 注册示例
- 📋 **`scrapy-go.toml` 配置模板精简** — `cmd/scrapy-go/templates/project/scrapy-go.toml.tmpl`
  - 移除冗余配置项，保留核心配置 + 注释说明

### 向后兼容性

- ✅ 无 API 变更，用户代码零修改
- ✅ 已持久化的 JSON 文件（无 callback 字段）反序列化后回调为 nil，回退到 Spider 默认 `Parse` 方法（与修复前行为一致）
- ✅ 所有测试通过，`go test -race` 无竞态

---

## [v1.1.4] — 2026-05-14

> **🔧 TD-004 Settings 编译期类型安全增强**

### 新增

#### TD-004：泛型类型安全 Settings API

> **通过 Go 泛型为 Settings 系统引入编译期类型安全，消除魔法字符串和运行时类型断言风险**

- 🎯 **`Key[T]` 泛型类型** — 新增 `pkg/settings/typed.go`
  - 将配置项名称与其值类型在编译期绑定
  - 内置默认值，调用者无需重复指定
  - 实现 `String()` 方法，支持日志输出

- 🔑 **类型化配置键常量** — 新增 `pkg/settings/keys.go`
  - 所有框架内置配置项（80+）均定义为 `Key[T]` 类型常量
  - 按功能分组：并发控制、下载配置、重试、熔断器、缓存、扩展等
  - 完整的 GoDoc 注释

- 🛡️ **泛型顶层函数** — `Get[T]` / `Set[T]` / `MustGet[T]`
  - `settings.Get(s, settings.KeyConcurrentRequests)` → 编译期确定返回 `int`
  - `settings.Set(s, settings.KeyBotName, "mybot", settings.PriorityProject)` → 编译期约束值类型
  - `settings.MustGet(s, key)` → 配置项不存在时 panic（适用于必须存在的配置）
  - 自动类型转换：int64→int、float64→int、string→bool 等，与旧 API 行为一致

- ♻️ **Crawler 调用方迁移** — `pkg/crawler/crawler.go`
  - 所有 50+ 处 `GetInt`/`GetString`/`GetBool`/`GetFloat`/`GetDuration` 调用迁移至泛型 API
  - 消除所有魔法字符串和重复的默认值声明
  - 保留旧 API 完全向后兼容

### 技术债务

- ✅ **TD-004 已偿还** — Settings 使用 `any` 类型存储值，缺少编译期类型安全
  - 新增泛型 `Key[T]` + `Get[T]`/`Set[T]` API 提供编译期类型检查
  - 框架核心调用方（`pkg/crawler`）已完成迁移
  - 旧 API（`GetInt`/`GetString` 等）保留向后兼容


> **🔧 类型安全增强 — 消除 `CallbackFunc`/`ErrbackFunc` 的 `any` 类型**
>
> P5-021 偿还了自 MVP 以来的技术债务 TD-003：将 `CallbackFunc`/`ErrbackFunc` 从 `any` 类型别名
> 替换为具体的函数类型定义，消除运行时类型断言，提供编译期类型安全。

### 变更

#### P5-021a：将共享类型下沉至 `pkg/http` 包

- 🏗️ **`Output` 类型迁移** — 将 `Output` 结构体从 `pkg/spider` 迁移至 `pkg/http`
  - `pkg/spider` 通过类型别名 `type Output = shttp.Output` 保持完全向后兼容
  - 用户代码无需任何修改，`spider.Output` 继续可用

- 🔒 **`CallbackFunc` 具体类型定义** — `pkg/http/request.go`
  - 从 `type CallbackFunc = any` 替换为 `type CallbackFunc func(ctx context.Context, response *Response) ([]Output, error)`
  - `pkg/spider` 通过类型别名 `type CallbackFunc = shttp.CallbackFunc` 保持向后兼容
  - 编译期即可捕获回调签名不匹配的错误

- 🔒 **`ErrbackFunc` 具体类型定义** — `pkg/http/request.go`
  - 从 `type ErrbackFunc = any` 替换为 `type ErrbackFunc func(ctx context.Context, err error, request *Request) ([]Output, error)`
  - `pkg/spider` 通过类型别名 `type ErrbackFunc = shttp.ErrbackFunc` 保持向后兼容

#### P5-021b：消除运行时类型断言

- ✅ **`pkg/scraper/scraper.go`** — `resolveCallback` 和 `ScrapeError` 方法
  - 移除 `request.Callback.(spider.CallbackFunc)` 运行时类型断言
  - 移除 `request.Errback.(spider.ErrbackFunc)` 运行时类型断言
  - 直接使用具体类型调用，零运行时开销

#### P5-021c：`NoCallback` 哨兵值重构

- 🔄 **`NoCallback` 实现方式变更** — `pkg/http/request.go`
  - 从 `noCallbackSentinel` 结构体改为哨兵函数值
  - `IsNoCallback` 通过函数指针比较实现，替代接口类型断言

#### P5-021d：`CallbackRegistry` 签名匹配增强

- 🎯 **精确签名匹配** — `pkg/http/callback_registry.go`
  - `matchesCallbackSignature` / `matchesErrbackSignature` 现在精确检查返回类型为 `[]Output`
  - `RegisterSpider` 使用 `reflect.Value.Convert` 将匿名函数类型零开销转换为命名类型 `CallbackFunc`/`ErrbackFunc`

### 技术债务

- ✅ **TD-003 已偿还** — `CallbackFunc`/`ErrbackFunc` 不再使用 `any` 类型，编译期类型安全

### 向后兼容性

- ✅ `spider.Output`、`spider.CallbackFunc`、`spider.ErrbackFunc` 通过类型别名保持完全兼容
- ✅ 所有 examples、contrib 模块、集成测试全部通过
- ✅ `go test -race` 无竞态报告

#### 扩展按需加载优化

> **未启用的扩展不再实例化，减少启动开销和日志噪声**

- ⚡ **扩展工厂前置检查** — `pkg/crawler/crawler.go`
  - `AutoThrottle`：`AUTOTHROTTLE_ENABLED=false` 时直接返回 `nil`，不再实例化
  - `MemoryUsage`：`MEMUSAGE_ENABLED=false` 时直接返回 `nil`
  - `CloseSpider`：所有关闭条件均为 0 时直接返回 `nil`
  - `LogStats`：`LOGSTATS_INTERVAL<=0` 时直接返回 `nil`
  - 此前这些扩展即使未启用也会被实例化，在 `Open()` 阶段通过 `ErrNotConfigured` 跳过

- 🔧 **`EXTENSIONS_BASE` 优先级分配** — `pkg/settings/defaults.go`
  - 从全部 `0`（执行顺序不确定）改为递增优先级：CoreStats(10) → CloseSpider(20) → LogStats(30) → MemoryUsage(40) → FeedExport(50) → AutoThrottle(60)
  - 消除启动时 "multiple extensions share the same priority" 警告

- 🛡️ **默认值保守化调整** — `pkg/settings/defaults.go`
  - `HTTPPROXY_ENABLED`：`true` → `false`（无代理环境下避免不必要的环境变量探测）
  - `MEMUSAGE_ENABLED`：`true` → `false`（轻量场景下避免后台 goroutine 开销）
  - `REFERER_ENABLED`：`true` → `false`（默认不自动添加 Referer 头，需要时显式启用）
  - `ROBOTSTXT_OBEY`：`true` → `false`（默认不遵守 robots.txt，与文档和 Scrapy 默认行为对齐）

---

## [v1.1.3] — 2026-05-13

> **🚀 Post-v1.0 生产增强里程碑 M7 — Sprint 13 生产增强**
>
> v1.1.3 是 scrapy-go 的生产增强版本，包含以下核心交付物：
> - P5-012 分布式限速器（`contrib/ratelimit` 独立模块）✅
> - P5-017 Scheduler 内存队列溢出优化（`pkg/scheduler` 内置增强）✅

### 新增

#### P5-012 分布式限速器（独立模块）

> **基于 Redis 滑动窗口算法的分布式限速器，支持按域名差异化配置**

##### P5-012a：RateLimiter 接口定义

- 🎯 **RateLimiter 接口** — 新增 `contrib/ratelimit/interface.go`
  - `Allow(domain string) bool` — 非阻塞检查请求是否被允许
  - `Wait(ctx context.Context, domain string) error` — 阻塞等待直到有可用配额
  - `Close() error` — 关闭限速器释放资源

##### P5-012b：Redis 滑动窗口限速器

- ⏱️ **RedisSlidingWindowLimiter** — 新增 `contrib/ratelimit/redis_limiter.go`
  - 基于 Redis Sorted Set + Lua 脚本实现滑动窗口算法
  - Lua 脚本原子操作：移除过期记录 → 计数 → 添加新记录，无竞态条件
  - 支持按域名独立限速，不同域名拥有独立的速率窗口
  - `DomainRates` 配置允许为特定域名设置独立速率
  - 优雅降级：Redis 不可用时自动允许所有请求通过
  - `NewRedisSlidingWindowLimiterFromClient` 支持共享 Redis 连接（与 `contrib/redisqueue` 复用）
  - `Stats()` / `Reset()` 辅助方法用于监控和管理

- ⚙️ **Options 配置** — 新增 `contrib/ratelimit/options.go`
  - `DefaultRate` / `DefaultBurst` — 默认速率和突发容量
  - `Window` — 滑动窗口时间长度（默认 1s）
  - `DomainRates` — 按域名差异化速率配置
  - `WaitTimeout` — Wait 方法默认超时（默认 30s）
  - `KeyExpiration` — 限速 Key 自动过期清理（默认 1h）
  - Redis 连接配置（Addr/Password/DB/PoolSize/Timeout）

##### P5-012c：RateLimitExtension

- 🔌 **RateLimitExtension** — 新增 `contrib/ratelimit/extension.go`
  - 实现 `extension.Extension` 接口，可注册到 Crawler 扩展系统
  - 监听 `RequestReachedDownloader` 信号，在请求到达下载器时自动限速
  - 从请求 URL 中提取域名，调用 `RateLimiter.Wait` 阻塞等待配额
  - 信号处理器错误不阻止请求（降级策略）

##### P5-012d：测试与文档

- ✅ **测试覆盖** — 27 个测试全部通过，覆盖率 88.0%，`go test -race` 通过
  - 限速器创建/关闭/幂等关闭
  - Allow 未超限/超限/不同域名/域名独立配置/关闭后降级
  - Wait 未超限/context 取消/关闭后降级
  - 共享 Redis 客户端（Close 不关闭共享连接）
  - Stats/Reset 辅助方法
  - 并发访问安全性（200 goroutine 并发）
  - Extension 生命周期（Open/Close/信号注册注销）
  - 集成测试（Extension + 信号系统端到端）
- 📖 **使用文档** — `contrib/ratelimit/README.md`

#### P5-017：Scheduler 内存队列溢出优化

> **防止大规模爬取场景下内存队列无限增长导致 OOM，自动溢出到磁盘队列**

- 🎯 **内存队列阈值** — `DefaultScheduler` 新增 `memoryQueueThreshold` 字段（`pkg/scheduler/scheduler.go`）
  - 新增 `WithMemoryQueueThreshold(n int)` Option，设置内存队列最大容量阈值
  - 入队时当内存队列请求数超过阈值，可序列化请求自动溢出到磁盘队列
  - 阈值为 0 或负数时不限制（保持原有行为）
  - 新增 `MemoryQueueLen()` 和 `MemoryQueueThreshold()` 方法用于监控

- 💾 **自动临时磁盘队列** — 未配置 `jobDir` 时自动创建临时目录（`pkg/scheduler/scheduler.go`）
  - 使用 `os.MkdirTemp` 创建临时磁盘队列目录（前缀 `scrapy-go-overflow-*`）
  - 爬虫结束时（`Close`）自动清理临时目录，不留残留文件
  - 已配置 `jobDir` 或外部队列时，复用已有磁盘队列，不创建临时目录

- 📊 **溢出统计** — 新增 `scheduler/overflow_to_disk` 统计指标
  - 记录因内存队列超阈值而溢出到磁盘的请求数量
  - 可通过 Stats Collector 监控溢出频率，辅助调优阈值参数

- ✅ **测试覆盖** — 新增 10 个单元测试 + 4 个基准测试，覆盖率 82.2%，`go test -race` 通过
  - `TestMemoryQueueThresholdBasic` — 验证基本阈值触发溢出行为
  - `TestMemoryQueueThresholdZero` — 验证阈值为 0 时不限制
  - `TestMemoryQueueThresholdNegative` — 验证负数阈值被忽略
  - `TestMemoryQueueThresholdWithJobDir` — 验证与 jobDir 配合使用
  - `TestMemoryQueueThresholdDequeueOrder` — 验证出队优先级正确性（内存优先）
  - `TestMemoryQueueThresholdTempDirCleanup` — 验证临时目录清理
  - `TestMemoryQueueThresholdConcurrency` — 验证并发安全性
  - `TestMemoryQueueThresholdAllDequeued` — 验证所有请求可正确出队
  - `TestMemoryQueueThresholdWithExternalQueue` — 验证与外部队列配合
  - `TestMemoryQueueThresholdAccessors` — 验证监控方法正确性
  - `BenchmarkSchedulerWithOverflow` — 不同阈值下的入队/出队性能
  - `BenchmarkSchedulerOverflowBurst` — 突发大量请求时的溢出性能
  - `BenchmarkSchedulerMemoryComparison` — 有无溢出保护的内存占用对比
  - `BenchmarkSchedulerOverflowConcurrent` — 并发场景下溢出保护性能

---

## [v1.1.2] — 2026-05-13

> **🚀 Post-v1.0 生产增强里程碑 M7 — Sprint 13 生态完善**
>
> v1.1.2 是 scrapy-go 的生态完善版本，包含以下核心交付物：
> - P5-005 Phase 1 轻量级 REST API（`contrib/web` 独立模块）✅
> - P5-005 Phase 1 增强：启动项参数注入 + 状态查询回显 ✅
> - P5-008 Redis 去重 Pipeline 批量优化（`contrib/redisqueue` 性能增强）✅

### 新增

#### P5-005 Phase 1 增强：启动项参数注入

> **REST API 启动端点支持用户自定义参数，覆盖爬虫配置并在状态查询中回显**

- 🎯 **启动项参数** — `POST /api/spiders/:name/start` 支持可选 JSON 请求体（`contrib/web/handler.go`）
  - 新增 `args` 字段（`map[string]any`），支持传入框架配置覆盖和自定义业务参数
  - 参数以 `PriorityCmdline`（最高优先级 40）注入 Crawler 的 `Settings`，覆盖所有其他级别配置
  - 向后兼容：无请求体或空请求体时行为不变
  - 启动响应中回显传入的 `args`

- 📊 **状态查询回显** — `GET /api/spiders/:name/stats` 响应中包含启动项参数（`contrib/web/server.go`）
  - `SpiderStats` 新增 `Args` 字段（`json:"args,omitempty"`）
  - `runningSpider` 内部保存启动时传入的 `args`，在统计查询和全局统计中返回
  - 无启动项时 `args` 字段省略（`omitempty`）

- ✅ **测试覆盖** — 新增 6 个测试用例，总计 42 个测试全部通过，覆盖率 86.0%，`go test -race` 通过
  - `TestHandleStartSpider_WithArgs` — 验证带参数启动及响应回显
  - `TestHandleStartSpider_WithEmptyArgs` — 验证空参数处理
  - `TestHandleStartSpider_WithInvalidBody` — 验证无效 JSON 请求体返回 400
  - `TestHandleGetStats_WithArgs` — 验证统计查询中包含启动项参数
  - `TestHandleGetStats_WithoutArgs` — 验证无参数时 args 字段省略
  - `TestIntegration_StartWithArgsAndCheckStats` — 端到端集成测试

#### P5-005 Phase 1：轻量级 REST API（Sprint 12）

> **Post-v1.0 生态完善 — 基于标准库 net/http 的零外部依赖 Web 管理 API**

##### P5-005a：HTTP Server + Spider 注册表

- 🌐 **Web 管理服务器** — 新增 `contrib/web/` 独立 Go 子模块（`contrib/web/server.go`）
  - 基于 Go 标准库 `net/http.ServeMux` 实现路由，零外部 Web 框架依赖
  - 内部使用 `crawler.Runner` 管理多爬虫并发执行，复用框架已有的生命周期管理
  - 支持 `context.Context` 驱动的优雅关闭（HTTP 服务器 + 所有运行中 Spider）
  - Functional Options 模式：`WithLogger` / `WithRunner` / `WithRegistry`

- 🕷️ **Spider 注册表** — 新增 `Registry`（`contrib/web/registry.go`）
  - 按名称注册 `SpiderFactory` 工厂函数，每次启动创建全新 Spider + Crawler 实例
  - 可选 `CrawlerConfigurator` 回调，在启动前为 Crawler 注册 Pipeline、扩展等
  - 线程安全：所有操作通过 `sync.RWMutex` 保护

##### P5-005b：REST API Handlers

- 📡 **REST API 端点** — 4 个核心端点 + 1 个健康检查（`contrib/web/handler.go`）
  - `GET /api/spiders` — 获取已注册 Spider 列表及运行实例数
  - `POST /api/spiders/:name/start` — 按名称启动 Spider，返回唯一运行 ID
  - `POST /api/spiders/:name/stop` — 按名称或 ID 停止 Spider
  - `GET /api/spiders/:name/stats` — 获取指定 Spider 的运行统计数据
  - `GET /api/health` — 健康检查

##### P5-005c：集成测试 + 使用文档

- ✅ **测试覆盖** — 32 个测试全部通过，覆盖率 85.3%，`go test -race` 通过
- 📖 **使用文档** — `contrib/web/README.md`

#### P5-008：Redis 去重 Pipeline 批量优化（Sprint 12）

> **Post-v1.0 性能增强 — 聚合多个 SADD 为 Pipeline 批量提交，减少网络往返**

##### P5-008a：PipelinedRedisDupeFilter 实现

- 🚀 **Pipeline 批量去重** — 新增 `PipelinedRedisDupeFilter`（`contrib/redisqueue/pipelined_dupefilter.go`）
  - 将多个 SADD 命令聚合为 Redis Pipeline 批量提交，一次网络往返处理多个去重请求
  - 后台 goroutine 异步批量提交，避免阻塞调用方
  - 双触发条件：达到批量大小（默认 64）或超过刷新间隔（默认 100ms），以先到者为准
  - 使用 buffered channel 作为请求缓冲区，天然支持背压
  - 每个 SADD 结果通过独立 channel 返回给调用方，保证正确性
  - 实现 `scheduler.DupeFilter` 接口，可直接替换 `RedisDupeFilter`

- ⚙️ **Pipeline 配置选项** — 3 个 Functional Options
  - `WithBatchSize(n)` — Pipeline 批量大小（默认 64）
  - `WithFlushInterval(d)` — Pipeline 刷新间隔（默认 100ms）
  - `WithBufferSize(n)` — 待提交指纹缓冲区大小（默认 4096）

- 🌸 **布隆过滤器支持** — 与 `RedisDupeFilter` 一致的布隆过滤器一级缓存
  - 通过 `BloomFilterEnabled` 配置启用
  - 新请求跳过 Pipeline 提交，进一步减少网络往返

- 📊 **运行时统计** — `PipelineStats()` 返回 Pipeline 运行指标
  - `pipeline_flushes` — Pipeline 刷新次数
  - `pipeline_items` — Pipeline 提交的总指纹数
  - `pending` — 当前缓冲区中待提交的指纹数

- 🔒 **优雅关闭** — Close 时自动排空缓冲区中的剩余数据
  - 通知后台 goroutine 退出
  - 排空 channel 中的剩余数据并刷新到 Redis
  - 等待后台 goroutine 完成后再关闭连接

- 🔗 **共享客户端** — `NewPipelinedRedisDupeFilterFromClient` 支持共享 Redis 连接

##### P5-008b：单元测试 + 基准测试

- ✅ **测试覆盖** — 24 个测试全部通过
  - 功能测试：RequestSeen / SeenCount / Contains / Clear / FlushOnStart
  - 并发测试：50 goroutine × 20 请求并发去重
  - 一致性测试：与 RedisDupeFilter 结果完全一致
  - Pipeline 触发测试：批量大小触发 / 定时器触发
  - 关闭排空测试：Close 时缓冲区数据完整写入 Redis
  - 布隆过滤器测试：基本功能 / 统计 / 禁用 / 并发
  - 配置选项测试：有效值 / 无效值 / 默认值
  - `go test -race` 竞态检测通过
  - 整体覆盖率 90.3%（目标 85%）✅

- 📈 **基准测试** — 逐条 vs Pipeline 吞吐量对比
  - `BenchmarkRedisDupeFilter_RequestSeen` — 逐条模式基准
  - `BenchmarkPipelinedRedisDupeFilter_RequestSeen` — Pipeline 模式基准
  - `Benchmark*_Parallel` — 并行基准测试
  - `Benchmark*_WithBloom_Parallel` — 布隆过滤器 + 并行基准测试
  - `BenchmarkPipelinedRedisDupeFilter_BatchSizes` — 不同批量大小性能对比

---

## [v1.1.1] — 2026-05-12

> **🚀 Post-v1.0 生产增强里程碑 M7 — Sprint 13 完成**
>
> v1.1.1 是 scrapy-go 的生产增强版本，包含以下核心交付物：
> - P5-009 通用持久化存储适配器（`contrib/storage`）✅
> - P5-010 高级重试策略（内置中间件增强）✅

### 新增

#### P5-010：高级重试策略（内置中间件增强）（Sprint 13）

> **Post-v1.0 生产增强 — 指数退避 + 抖动 + 熔断器 + 差异化重试策略**

##### P5-010a：RetryMiddleware 指数退避 + 抖动增强

- ⚡ **指数退避策略** — RetryMiddleware 新增退避延迟计算（`pkg/downloader/middleware/retry.go` 增强）
  - 公式：`delay = base * 2^(attempt-1) + jitter`
  - 支持三种退避策略：`RetryBackoffNone`（默认，向后兼容）/ `RetryBackoffExponential` / `RetryBackoffFixed`
  - 延迟通过 `download_delay` Meta 传递给下一次请求
  - `WithRetryBackoff(baseDelay, maxDelay, jitter)` — 启用指数退避
  - `WithRetryFixedDelay(delay)` — 启用固定延迟退避
  - 最大延迟上限保护，避免无限增长

- 🎲 **随机抖动** — 可选的随机抖动避免重试风暴
  - 抖动范围：`[0, delay * 0.5)`
  - 通过 `RETRY_BACKOFF_JITTER` 配置项控制（默认 true）
  - 使用 `math/rand/v2` 无需手动 seed

##### P5-010b：熔断器中间件

- 🔌 **CircuitBreakerMiddleware** — 域名级别熔断器中间件（`pkg/downloader/middleware/circuitbreaker.go`）
  - 状态机：Closed → Open → Half-Open，三态转换
  - 域名级别独立跟踪：每个域名维护独立的熔断器实例
  - 连续失败达到阈值时自动熔断，暂停对该域名的请求
  - 恢复超时后进入半开状态，允许探测请求通过
  - 探测成功达到阈值后恢复为关闭状态
  - `sync.Map` + 细粒度 `sync.Mutex` 保证并发安全
  - 优先使用 `download_slot` Meta 作为域名标识（与 Downloader Slot 一致）

- ⚙️ **配置项** — 6 个熔断器配置项
  - `CIRCUIT_BREAKER_ENABLED`（默认 false）— 是否启用熔断器
  - `CIRCUIT_BREAKER_FAIL_THRESHOLD`（默认 5）— 连续失败阈值
  - `CIRCUIT_BREAKER_RECOVERY_TIMEOUT`（默认 30s）— 恢复超时时间
  - `CIRCUIT_BREAKER_HALF_OPEN_MAX_REQUESTS`（默认 1）— 半开状态最大探测请求数
  - `CIRCUIT_BREAKER_SUCCESS_THRESHOLD`（默认 2）— 半开状态恢复所需连续成功次数
  - `CIRCUIT_BREAKER_HTTP_CODES`（默认 [500, 502, 503, 504]）— 触发熔断的 HTTP 状态码

- 📊 **统计项** — 5 个运行时统计指标
  - `circuitbreaker/opened` — 熔断器打开次数
  - `circuitbreaker/closed` — 熔断器恢复关闭次数
  - `circuitbreaker/reopened` — 半开状态重新打开次数
  - `circuitbreaker/rejected` — 被熔断器拒绝的请求数
  - `circuitbreaker/half_open` — 转入半开状态次数

- 🔍 **监控接口** — 运行时状态查询
  - `GetBreakerState(domain)` — 获取域名熔断器状态
  - `GetBreakerConsecutiveFails(domain)` — 获取连续失败次数
  - `ResetBreaker(domain)` — 手动重置熔断器

##### P5-010c：差异化重试策略配置

- 🎯 **按状态码差异化重试** — 不同 HTTP 状态码可配置不同的最大重试次数
  - `WithPerStatusMaxRetries(map[int]int{429: 5, 503: 1})` — 429 最多重试 5 次，503 只重试 1 次
  - 优先级：请求级 Meta > 按状态码配置 > 全局配置
  - 通过 `RETRY_PER_STATUS_MAX_TIMES` 配置项设置

- 🔧 **自定义重试条件** — 支持完全自定义的重试判断逻辑
  - `WithRetryCondition(func(statusCode int, err error) bool)` — 替代默认的状态码/错误判断
  - 适用于需要复杂业务逻辑判断是否重试的场景

- ✅ **测试覆盖** — 30 个测试全部通过
  - 中间件包整体覆盖率 89.1%（目标 85%）✅
  - 指数退避测试（首次/二次/最大延迟/抖动/固定延迟/无退避）
  - 差异化重试测试（按状态码/Meta 覆盖/自定义条件）
  - 熔断器状态机测试（关闭/打开/半开/恢复/重新打开）
  - 多域名独立性测试
  - 并发安全测试（50 goroutine）
  - 集成测试（Retry + CircuitBreaker 协同）
  - `go test -race` 竞态检测通过
  - `go vet` 无告警，`gofmt` 格式化通过

##### 新增配置项

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `RETRY_BACKOFF_ENABLED` | false | 启用指数退避 |
| `RETRY_BACKOFF_BASE_DELAY` | 1.0 | 退避基础延迟（秒） |
| `RETRY_BACKOFF_MAX_DELAY` | 60.0 | 退避最大延迟（秒） |
| `RETRY_BACKOFF_JITTER` | true | 启用随机抖动 |
| `RETRY_PER_STATUS_MAX_TIMES` | {} | 按状态码差异化最大重试次数 |
| `CIRCUIT_BREAKER_ENABLED` | false | 启用熔断器 |
| `CIRCUIT_BREAKER_FAIL_THRESHOLD` | 5 | 连续失败阈值 |
| `CIRCUIT_BREAKER_RECOVERY_TIMEOUT` | 30 | 恢复超时（秒） |
| `CIRCUIT_BREAKER_HALF_OPEN_MAX_REQUESTS` | 1 | 半开最大探测数 |
| `CIRCUIT_BREAKER_SUCCESS_THRESHOLD` | 2 | 半开恢复成功次数 |
| `CIRCUIT_BREAKER_HTTP_CODES` | [500,502,503,504] | 触发熔断的状态码 |

##### 变更文件

- `pkg/downloader/middleware/retry.go` — RetryMiddleware 增强（指数退避 + 差异化策略）
- `pkg/downloader/middleware/circuitbreaker.go` — 新增熔断器中间件
- `pkg/downloader/middleware/retry_advanced_test.go` — 新增高级重试策略测试套件（30 个测试用例）
- `pkg/settings/defaults.go` — 新增退避和熔断器配置项 + DOWNLOADER_MIDDLEWARES_BASE 注册 CircuitBreaker(545)
- `pkg/crawler/crawler.go` — Retry 工厂增强 + CircuitBreaker 工厂注册

#### P5-009：通用持久化存储适配器（独立模块）（Sprint 13）

> **Post-v1.0 生产增强 — MongoDB + PostgreSQL + Elasticsearch 持久化 Pipeline**

##### P5-009a：独立 Go 子模块 + 通用接口定义

- 📦 **独立模块** — 创建 `contrib/storage/` 独立 Go 子模块
  - 独立 `go.mod`，依赖 `go.mongodb.org/mongo-driver/v2` + `github.com/jackc/pgx/v5` + `github.com/elastic/go-elasticsearch/v8`
  - 主模块 `go.mod` 不引入数据库驱动依赖，实现零侵入可插拔设计
  - 通过 `go get github.com/dplcz/scrapy-go/contrib/storage` 独立安装

- 🔌 **StorageWriter 接口** — 通用存储写入接口（`contrib/storage/interface.go`）
  - `Connect(ctx) error` — 建立连接
  - `Close(ctx) error` — 关闭连接
  - `WriteBatch(ctx, items) (int, error)` — 批量写入

- 🔄 **UpsertWriter 接口** — 扩展接口，支持 Upsert 操作
  - `UpsertBatch(ctx, uniqueKey, items) (int, error)` — 基于唯一键的批量 Upsert

- 🧩 **BasePipeline** — 通用批量缓冲 Pipeline 基础实现（`contrib/storage/base.go`）
  - 可配置批量大小（默认 100）
  - 缓冲区满时自动触发批量写入
  - Close 时自动刷新剩余缓冲数据
  - `sync.Mutex` 保护缓冲区，支持 CONCURRENT_ITEMS 并发
  - 支持自定义 ItemConverter 或默认使用 `item.Adapt().AsMap()`

##### P5-009b：MongoDB Pipeline 适配器

- 🍃 **mongo.Pipeline** — MongoDB 持久化 Pipeline（`contrib/storage/mongo/pipeline.go`）
  - 实现 `pipeline.ItemPipeline` 接口，可直接注册到 crawler
  - 批量 `InsertMany` 写入，支持有序/无序模式
  - `BulkWrite` + `UpdateOne` with `upsert: true` 实现 Upsert
  - Functional Options 模式配置：`WithURI` / `WithDatabase` / `WithCollection` / `WithBatchSize` / `WithUpsertKey`
  - 编译期 `StorageWriter` + `UpsertWriter` 接口满足性检查

##### P5-009c：PostgreSQL Pipeline 适配器

- 🐘 **postgres.Pipeline** — PostgreSQL 持久化 Pipeline（`contrib/storage/postgres/pipeline.go`）
  - 实现 `pipeline.ItemPipeline` 接口，可直接注册到 crawler
  - `pgx.CopyFrom` 高效批量插入
  - `INSERT ... ON CONFLICT (uniqueKey) DO UPDATE SET ...` 实现 Upsert
  - 自动从 map key 推断列名，或通过 `WithColumns` 显式指定
  - `pgxpool` 连接池管理，可配置最大连接数
  - Functional Options 模式配置：`WithDSN` / `WithTable` / `WithColumns` / `WithBatchSize` / `WithUpsertKey`

##### P5-009d：Elasticsearch Pipeline 适配器

- 🔍 **elasticsearch.Pipeline** — Elasticsearch 持久化 Pipeline（`contrib/storage/elasticsearch/pipeline.go`）
  - 实现 `pipeline.ItemPipeline` 接口，可直接注册到 crawler
  - Bulk API 批量 `index` 写入
  - Bulk API `update` action + `doc_as_upsert: true` 实现 Upsert
  - 支持自定义文档 `_id` 字段（`WithDocumentIDField`）
  - 支持认证（`WithUsername` / `WithPassword`）和刷新策略（`WithRefresh`）
  - Bulk 响应解析：统计成功/失败数量，部分失败时返回详细错误信息

##### P5-009e：测试 + 使用文档

- ✅ **测试覆盖** — 40 个测试全部通过
  - `storage` 核心包覆盖率 89.6%
  - Mock Writer 验证批量缓冲、刷新、并发安全、Upsert 模式
  - 各适配器配置验证、选项设置、SQL/NDJSON 构建逻辑测试
  - `go test -race` 竞态检测通过
  - `go vet` 无告警，`gofmt` 格式化通过

- 📖 **使用文档** — `contrib/storage/README.md`
  - 三种存储后端的快速开始示例
  - Upsert 模式使用说明
  - 自定义 Item 转换函数示例
  - 完整配置参考表
  - 架构设计说明

## [v1.1.0] — 2026-05-12

> **🎉 Post-v1.0 生态完善里程碑 M6 — Sprint 12 全部完成**
>
> v1.1.0 是 scrapy-go 首个 Post-v1.0 生态完善版本，包含以下核心交付物：
> - P5-001 高级下载器特性（HTTP/2 + 连接池 + 进度回调）
> - P5-002 AutoThrottle 自适应限速扩展
> - P5-003 Redis 队列可插拔扩展（`contrib/redisqueue`）
> - P5-007 可观测性具体实现（`contrib/telemetry`）
> - P4-007a~m 性能优化（13 项）
>
> 所有测试通过，`go test -race` 竞态检测通过，`go vet` 无告警，`gofmt` 格式化通过。

### 新增

#### P5-007：可观测性具体实现（独立模块）（Sprint 12）

> **Post-v1.0 生态完善 — OpenTelemetry 追踪 + Prometheus 指标 + 信号驱动扩展**

##### P5-007a：独立 Go 子模块

- 📦 **独立模块** — 创建 `contrib/telemetry/` 独立 Go 子模块
  - 独立 `go.mod`，依赖 `go.opentelemetry.io/otel` v1.43.0 + `github.com/prometheus/client_golang` v1.22.0
  - 主模块 `go.mod` 不引入 OTel/Prometheus 相关依赖，实现零侵入可插拔设计
  - 通过 `go get github.com/dplcz/scrapy-go/contrib/telemetry` 独立安装

##### P5-007b：OpenTelemetry Tracer 适配器

- 🔍 **otel.Tracer** — OpenTelemetry Tracer 适配器（`contrib/telemetry/otel/tracer.go`）
  - 实现 `pkg/telemetry.Tracer` 接口，包装 OTel SDK TracerProvider
  - SpanKind 映射：`telemetry.SpanKindClient` → `oteltrace.SpanKindClient` 等 5 种映射
  - SpanStatus 映射：`telemetry.SpanStatusOK` → `codes.Ok`、`SpanStatusError` → `codes.Error`
  - SpanContext 转换：OTel TraceID/SpanID/TraceFlags/IsRemote → telemetry.SpanContext
  - Attributes 映射：`map[string]string` → `[]otelattr.KeyValue`
  - Shutdown 支持：自动检测 TracerProvider 是否实现 Shutdown 接口
  - 编译期接口实现检查

- 🔍 **otel.Span** — OpenTelemetry Span 适配器
  - 实现 `pkg/telemetry.Span` 接口，包装 OTel trace.Span
  - 支持 End/SetAttributes/SetStatus/RecordError/SpanContext/AddEvent 全部方法
  - nil 安全：nil error 和 nil/空 map 不会 panic

##### P5-007c：Prometheus MetricsRegistry 适配器

- 📊 **prometheus.Registry** — Prometheus MetricsRegistry 适配器（`contrib/telemetry/prometheus/registry.go`）
  - 实现 `pkg/telemetry.MetricsRegistry` 接口，包装 `prometheus.Registry`
  - 使用独立 `prometheus.Registry`（非全局默认），避免与用户代码冲突
  - 双重检查锁定（DCL）保证指标注册的线程安全和幂等性
  - 同名指标多次获取返回同一实例，共享状态
  - nil buckets 自动使用 `telemetry.DefaultHistogramBuckets`
  - `PrometheusRegistry()` 暴露底层 Registry，用于 `promhttp.HandlerFor`
  - `NewRegistryFrom()` 支持复用已有 Prometheus Registry
  - 编译期接口实现检查

- 📊 **prometheus.Counter/Gauge/Histogram** — Prometheus 指标适配器
  - Counter：Inc() + Add(delta)
  - Gauge：Set(value) + Inc() + Dec() + Add(delta)
  - Histogram：Observe(value) + ObserveDuration(d)

##### P5-007d：TraceExtension + MetricsExtension

- 🔌 **TraceExtension** — 信号驱动的分布式追踪扩展（`contrib/telemetry/extension.go`）
  - 实现 `extension.Extension` 接口
  - 监听 7 个信号：SpiderOpened/Closed、RequestReachedDownloader/LeftDownloader、ResponseReceived、SpiderError、ItemScraped
  - SpiderOpened → 创建根 Span "spider.crawl"（SpanKindInternal）
  - RequestReachedDownloader → 创建子 Span "http.request"（SpanKindClient）
  - ResponseReceived → 记录响应事件（status_code + url）
  - SpiderError → RecordError + 错误事件
  - ItemScraped → 记录 Item 事件
  - SpiderClosed → 结束根 Span + 记录关闭原因
  - Close 时自动 Shutdown Tracer，刷新待发送数据

- 📊 **MetricsExtension** — 信号驱动的指标收集扩展（`contrib/telemetry/extension.go`）
  - 实现 `extension.Extension` 接口
  - 监听 8 个信号：SpiderOpened/Closed、RequestReachedDownloader/LeftDownloader、ResponseReceived、ItemScraped/Dropped、SpiderError
  - 预注册 9 个指标：scrapy_requests_total、scrapy_responses_total、scrapy_items_scraped_total、scrapy_items_dropped_total、scrapy_errors_total、scrapy_active_requests、scrapy_spider_state、scrapy_request_duration_seconds、scrapy_spider_elapsed_seconds
  - 支持 `time.Duration` 和 `float64` 两种延迟格式
  - 内置 HTTP `/metrics` 端点（Prometheus 格式）+ `/health` 健康检查
  - 可选 HTTP 端点：addr 为空字符串时不启动 HTTP 服务器

- 🌐 **HTTP 服务器** — 内置 Prometheus HTTP 端点（`contrib/telemetry/server.go`）
  - 自动检测 MetricsRegistry 底层类型，Prometheus 后端使用 `promhttp.HandlerFor`
  - 非 Prometheus 后端提供简单文本端点
  - 支持随机端口（`:0`）和固定端口
  - 优雅关闭：Close 时自动 Shutdown HTTP 服务器

##### P5-007e：集成测试 + 使用文档

- ✅ **OTel Tracer 测试套件** — 16 个测试用例（`contrib/telemetry/otel/tracer_test.go`）
  - 使用 `tracetest.InMemoryExporter` 验证 Span 导出
  - 覆盖 Start/Shutdown/SetAttributes/SetStatus/RecordError/SpanContext/AddEvent
  - 父子 Span 关系验证（共享 TraceID）
  - SpanKind 映射测试（5 种 Kind）
  - 并发安全测试（100 goroutine）
  - 接口可赋值性测试
  - 覆盖率 100%

- ✅ **Prometheus Registry 测试套件** — 12 个测试用例（`contrib/telemetry/prometheus/registry_test.go`）
  - 使用 `prometheus.Registry.Gather()` 验证指标值
  - 覆盖 Counter/Gauge/Histogram 操作 + 幂等性
  - nil buckets 测试
  - 并发安全测试（100 goroutine）
  - 接口可赋值性测试
  - 覆盖率 94.6%

- ✅ **Extension 测试套件** — 17 个测试用例（`contrib/telemetry/extension_test.go`）
  - TraceExtension 生命周期测试（Open/Close + 信号注册/注销）
  - MetricsExtension 生命周期测试
  - Spider 完整生命周期模拟
  - HTTP 端点测试（/metrics + /health）
  - Prometheus 集成测试（验证指标值）
  - nil tracer/registry 安全测试
  - nil params 安全测试
  - 并发信号测试（50/100 goroutine）
  - 无效地址测试
  - `go test -race` 竞态检测通过
  - 总体覆盖率 92.2%

- 📖 **使用文档** — `contrib/telemetry/README.md`
  - 安装说明 + 快速开始（Prometheus 指标 / OTel 追踪 / 同时启用）
  - 采集的指标列表（9 个 Prometheus 指标）
  - 追踪 Span 列表
  - HTTP 端点说明
  - 设计决策说明

##### 变更文件

- `contrib/telemetry/go.mod` — 独立 Go 子模块定义
- `contrib/telemetry/go.sum` — 依赖锁定
- `contrib/telemetry/doc.go` — 包文档
- `contrib/telemetry/extension.go` — TraceExtension + MetricsExtension 实现
- `contrib/telemetry/server.go` — HTTP /metrics 端点实现
- `contrib/telemetry/otel/tracer.go` — OpenTelemetry Tracer 适配器
- `contrib/telemetry/prometheus/registry.go` — Prometheus MetricsRegistry 适配器
- `contrib/telemetry/extension_test.go` — Extension 测试套件（17 个测试用例）
- `contrib/telemetry/otel/tracer_test.go` — OTel Tracer 测试套件（16 个测试用例）
- `contrib/telemetry/prometheus/registry_test.go` — Prometheus Registry 测试套件（12 个测试用例）
- `contrib/telemetry/README.md` — 使用文档

## [v1.0.3] — 2026-05-11

### 新增

#### P5-002：AutoThrottle 自适应限速扩展（Sprint 12）

> **Post-v1.0 生态完善 — 基于延迟反馈的自适应速率调整**

##### P5-002a：基于延迟反馈的自适应速率调整

- 🎛️ **AutoThrottleExtension** — 基于延迟反馈的自适应速率调整扩展（`pkg/extension/autothrottle.go`）
  - 对应 Scrapy 的 `scrapy.extensions.throttle.AutoThrottle`
  - 监听 `ResponseDownloaded` 信号，获取每个响应的下载延迟
  - 使用 EWMA（指数加权移动平均，alpha=0.5）平滑延迟抖动
  - 根据目标并发数和实际延迟动态计算理想下载延迟
  - 算法公式（对齐 Scrapy 原版）：
    - `latency_ewma = alpha * new_latency + (1 - alpha) * old_latency`
    - `target_delay = latency_ewma / target_concurrency`
    - `new_delay = (old_delay + target_delay) / 2.0`
    - `new_delay = clamp(new_delay, start_delay * 0.2, max_delay)`
  - 每个域名（Slot）独立跟踪延迟和调整延迟
  - 通过 `DelayAdjuster` 接口回调调整 Slot 延迟，与 Downloader 解耦

- ⚙️ **配置项** — 5 个 AutoThrottle 配置项
  - `AUTOTHROTTLE_ENABLED`（默认 false）— 是否启用自适应限速
  - `AUTOTHROTTLE_START_DELAY`（默认 5.0）— 初始下载延迟（秒）
  - `AUTOTHROTTLE_MAX_DELAY`（默认 60.0）— 最大下载延迟（秒）
  - `AUTOTHROTTLE_TARGET_CONCURRENCY`（默认 1.0）— 目标并发数（每个域名）
  - `AUTOTHROTTLE_DEBUG`（默认 false）— 是否输出调试日志

- 📊 **统计项** — 3 个运行时统计指标
  - `autothrottle/request_count` — 已处理的请求总数
  - `autothrottle/latency_avg` — 当前 EWMA 平滑延迟（秒）
  - `autothrottle/delay_adjusted_count` — 延迟调整次数

- 🔌 **DelayAdjuster 接口** — 延迟调整回调接口
  - `Downloader` 实现 `DelayAdjuster` 接口，`AdjustDelay` 方法动态调整 Slot 延迟
  - `DelayAdjusterFunc` 函数适配器，方便测试和自定义实现
  - `Slot.SetDelay()` 新增方法，支持运行时动态修改下载延迟

- 🔒 **并发安全** — `sync.Mutex` 保护共享状态
  - `Slot.getDownloadDelay()` 和 `Slot.DownloadDelay()` 加锁读取 delay 字段
  - `Slot.processTask()` 在读取 delay 时加锁，确保 AutoThrottle 动态修改的可见性

##### P5-002b：单元测试

- ✅ **完整测试套件** — 33 个测试用例（`pkg/extension/autothrottle_test.go`）
  - 构造函数测试（默认值、参数校验、边界条件）
  - Open/Close 生命周期测试（信号注册/注销、统计更新）
  - 延迟调整算法测试（首次请求、收敛行为、上下限钳制、多 Slot 独立调整）
  - EWMA 平滑测试（验证指数加权移动平均计算正确性）
  - 信号参数边界测试（nil params、缺少 request/response、零延迟）
  - Table-Driven 测试（多场景参数化验证）
  - 并发安全测试（5 域名 × 20 goroutine 并发访问）
  - `go test -race` 竞态检测通过
  - 扩展包整体测试覆盖率 90.7%

##### 变更文件

- `pkg/extension/autothrottle.go` — AutoThrottle 扩展实现
- `pkg/extension/autothrottle_test.go` — 完整测试套件（33 个测试用例）
- `pkg/extension/doc.go` — 包文档更新（新增 AutoThrottle 说明）
- `pkg/downloader/downloader.go` — 新增 `AdjustDelay` 方法（实现 `DelayAdjuster` 接口）
- `pkg/downloader/slot.go` — 新增 `SetDelay` 方法 + `getDownloadDelay`/`DownloadDelay`/`processTask` 加锁保护
- `pkg/settings/defaults.go` — 新增 5 个 AutoThrottle 配置项默认值 + EXTENSIONS_BASE 注册
- `pkg/crawler/crawler.go` — `builtinExtensionFactories` 注册 AutoThrottle 工厂

## [v1.0.2] — 2026-05-11

### 新增

#### P5-003：Redis 队列可插拔扩展（Sprint 12）

> **Post-v1.0 生态完善 — Redis 分布式队列与去重过滤器**

##### P5-003a：独立 Go 子模块

- 📦 **独立模块** — 创建 `contrib/redisqueue/` 独立 Go 子模块
  - 独立 `go.mod`，依赖 `github.com/redis/go-redis/v9`
  - 主模块 `go.mod` 不引入 Redis 相关依赖，实现零侵入可插拔设计
  - 通过 `go get github.com/dplcz/scrapy-go/contrib/redisqueue` 独立安装

##### P5-003b：RedisQueue 分布式优先级队列

- 🌐 **RedisQueue** — 基于 Redis Sorted Set 的分布式优先级队列（`contrib/redisqueue/redisqueue.go`）
  - 实现 `scheduler.PriorityAwareQueue` 接口，通过 `WithExternalQueue` 注入
  - 使用 Sorted Set score 编码优先级 + 序号（`score = priority × 1e10 + seq`）
  - ZADD 入队，ZPOPMAX 出队，保证多实例并发安全
  - 相同优先级内 LIFO 出队（后入队的 seq 更大，score 更大）
  - 支持负优先级、断点续爬（Redis 持久化保证数据不丢失）
- 🔌 **共享客户端** — `NewRedisQueueFromClient` 支持复用已有 Redis 连接
- 📊 **统计接口** — `Stats()` / `PriorityStats()` / `LenByPriority()` 运行时监控

##### P5-003c：RedisDupeFilter 分布式去重过滤器

- 🔒 **RedisDupeFilter** — 基于 Redis Set 的分布式去重过滤器（`contrib/redisqueue/dupefilter.go`）
  - 实现 `scheduler.DupeFilter` 接口，通过 `WithDupeFilter` 注入
  - 使用 SADD 原子操作，多实例并发安全
  - 指纹算法与 `RFPDupeFilter` 完全一致（URL + Method + Body 的 SHA1）
  - 多实例共享同一 Redis 时自动实现分布式去重
- 🔌 **共享客户端** — `NewRedisDupeFilterFromClient` 支持复用已有 Redis 连接
- 🔍 **查询接口** — `Contains()` 只读查询不修改集合，`SeenCount()` 统计

##### P5-003d：Options 配置结构体

- ⚙️ **Options** — 完整的配置选项（`contrib/redisqueue/options.go`）
  - 连接配置：`Addr` / `Password` / `DB` / `DialTimeout` / `ReadTimeout` / `WriteTimeout` / `PoolSize`
  - Key 配置：`KeyPrefix` / `QueueKey` / `DupeFilterKey` / `StartURLsKey`
  - 行为配置：`FlushOnStart` / `Serializer`
  - `DefaultOptions()` 提供合理默认值

##### P5-003e：集成测试

- ✅ **miniredis 内存 Mock** — 使用 `alicebob/miniredis/v2` 进行完整流程验证
  - 27+ 个测试用例覆盖 RedisQueue / RedisDupeFilter / 集成场景
  - 并发安全测试（10 goroutine × 100 ops）
  - 断点续爬测试（关闭后重新连接恢复数据）
  - 分布式去重测试（多实例共享去重集合）
  - `go test -race` 竞态检测通过
  - 测试覆盖率 ~80%

##### P5-003f：使用文档

- 📖 **README.md** — 完整使用文档（`contrib/redisqueue/README.md`）
  - 快速开始示例
  - 配置选项说明
  - 分布式爬取示例
  - Redis Key 结构说明
  - 与 DiskQueue 对比表
  - Score 编码设计说明

##### P5-003g：本地布隆过滤器一级去重缓存

- 🚀 **布隆过滤器优化** — 可选的本地布隆过滤器一级缓存（`contrib/redisqueue/dupefilter.go` 增强）
  - 通过 `BloomFilterEnabled` 配置项开启，默认关闭
  - 新请求通过布隆过滤器快速判断"不存在"，跳过 Redis 读查询
  - 布隆过滤器判断"可能存在"时穿透到 Redis SADD 精确判断
  - 正确性完全由 Redis 保证，布隆过滤器仅作为性能优化
  - 多机场景下各实例独立维护布隆过滤器，不影响分布式去重正确性
- ⚙️ **配置参数** — `BloomExpectedItems`（预估请求量，默认 100 万）/ `BloomFalsePositiveRate`（误判率，默认 0.1%）
- 📊 **统计接口** — `BloomStats()` 返回命中率、穿透次数等运行时指标
- 🔒 **并发安全** — `sync.Mutex` 保护布隆过滤器写入，`go test -race` 通过
- 📦 **依赖** — 引入 `github.com/bits-and-blooms/bloom/v3`（MIT 许可证）

##### 性能优化

- ⚡ **指纹计算移到锁外** — `RequestSeen` / `Contains` 的 `computeFingerprint` 调用从 `RLock` 内移到锁外
  - 减少锁持有时间，`Close()` 获取写锁时不再等待指纹计算
  - CPU 密集操作（JSON 序列化 + SHA1 + URL 规范化）不再阻塞其他 goroutine

##### 变更文件

- `contrib/redisqueue/go.mod` — 独立模块定义
- `contrib/redisqueue/doc.go` — 包文档
- `contrib/redisqueue/options.go` — 配置选项
- `contrib/redisqueue/redisqueue.go` — Redis 分布式优先级队列
- `contrib/redisqueue/dupefilter.go` — Redis 分布式去重过滤器
- `contrib/redisqueue/fingerprint.go` — 请求指纹计算（与主模块算法一致）
- `contrib/redisqueue/redisqueue_test.go` — 完整测试套件（27+ 测试用例）
- `contrib/redisqueue/README.md` — 使用文档

#### P5-001：高级下载器特性（Sprint 12）

> **Post-v1.0 生态完善 — 高级下载器特性**

##### P5-001a：HTTP/2 专用优化

- 🚀 **HTTP2DownloadHandler** — 新增 HTTP/2 优化的下载处理器（`pkg/downloader/handler_h2.go`）
  - 使用 `golang.org/x/net/http2` 直接建立 HTTP/2 连接
  - 利用 HTTP/2 多路复用特性，单连接支持多并发流
  - 自动降级：当目标不支持 HTTP/2 时回退到 HTTP/1.1
  - 支持 `force_http2` Meta 强制使用 HTTP/2 Transport
  - 通过 `HTTP2_ENABLED` 配置项全局启用
- ⚡ **ALPN 自动协商** — HTTPS 请求通过 TLS ALPN 自动协商最优协议版本
- 🔄 **透明降级** — HTTP/2 连接失败时自动回退到 HTTP/1.1，无需用户干预

##### P5-001b：连接池精细化管理

- 🔧 **ConnPoolConfig** — 新增连接池精细化配置结构体（`pkg/downloader/connpool.go`）
  - `MaxIdleConns` / `MaxIdleConnsPerHost` / `MaxConnsPerHost` 连接数控制
  - `IdleConnTimeout` / `TLSHandshakeTimeout` / `DialTimeout` 超时控制
  - `WriteBufferSize` / `ReadBufferSize` 缓冲区大小配置
  - `DisableKeepAlives` / `ForceHTTP2` 协议行为控制
  - `TLSInsecureSkipVerify` TLS 证书验证控制（测试/内网场景）
- 📊 **ConnPoolStats** — 连接池运行时统计（atomic 无锁）
  - `TotalConnsCreated` / `TotalConnsReused` / `TotalConnsClosed` 累计计数
  - `TotalTLSHandshakes` / `ActiveConns` / `IdleConns` 实时状态
  - `Snapshot()` 方法返回统计快照（用于日志和监控）
- 🏗️ **ManagedTransport** — 带统计功能的 Transport 包装
- ⚙️ **Settings 集成** — 通过 `CONNPOOL_*` 前缀配置项控制连接池参数

##### P5-001c：下载进度回调支持

- 📈 **ProgressHTTPDownloadHandler** — 新增支持下载进度回调的处理器（`pkg/downloader/progress.go`）
  - 通过 `Request.Meta["download_progress_callback"]` 设置进度回调
  - 支持已知大小（Content-Length）和未知大小（chunked）的进度报告
  - 可配置最小报告间隔（`DOWNLOAD_PROGRESS_MIN_INTERVAL`），避免高频回调
  - 无进度回调时零开销（走标准读取路径）
  - 通过 `DOWNLOAD_PROGRESS_ENABLED` 配置项全局启用
- 🎯 **DownloadProgressCallback** — 进度回调函数类型定义
  - 参数：`bytesRead`（已读取字节数）、`totalBytes`（总字节数，-1 表示未知）、`request`（关联请求）
  - 在下载 goroutine 中同步调用，不引入额外 goroutine

##### 新增配置项

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `HTTP2_ENABLED` | false | 启用 HTTP/2 优化下载处理器 |
| `DOWNLOAD_PROGRESS_ENABLED` | false | 启用下载进度回调 |
| `DOWNLOAD_PROGRESS_MIN_INTERVAL` | 100ms | 进度报告最小间隔 |
| `CONNPOOL_MAX_IDLE_CONNS` | 100 | 最大空闲连接总数 |
| `CONNPOOL_MAX_IDLE_CONNS_PER_HOST` | 10 | 每 host 最大空闲连接数 |
| `CONNPOOL_MAX_CONNS_PER_HOST` | 0 | 每 host 最大连接数（0=不限制） |
| `CONNPOOL_IDLE_CONN_TIMEOUT` | 90s | 空闲连接超时 |
| `CONNPOOL_TLS_HANDSHAKE_TIMEOUT` | 10s | TLS 握手超时 |
| `CONNPOOL_DIAL_TIMEOUT` | 30s | TCP 连接超时 |
| `CONNPOOL_DIAL_KEEPALIVE` | 30s | TCP keep-alive 间隔 |
| `CONNPOOL_DISABLE_KEEPALIVES` | false | 禁用 HTTP keep-alive |
| `CONNPOOL_WRITE_BUFFER_SIZE` | 0 | 写缓冲区大小（0=默认 4KB） |
| `CONNPOOL_READ_BUFFER_SIZE` | 0 | 读缓冲区大小（0=默认 4KB） |
| `CONNPOOL_TLS_INSECURE_SKIP_VERIFY` | false | 跳过 TLS 证书验证 |

##### 变更文件

- `pkg/downloader/handler_h2.go` — 新增 HTTP/2 优化下载处理器
- `pkg/downloader/connpool.go` — 新增连接池精细化管理
- `pkg/downloader/progress.go` — 新增下载进度回调支持
- `pkg/downloader/handler_h2_test.go` — HTTP/2 处理器测试（12 个测试用例）
- `pkg/downloader/connpool_test.go` — 连接池管理测试（6 个测试用例）
- `pkg/downloader/progress_test.go` — 进度回调测试（12 个测试用例）
- `pkg/crawler/crawler.go` — 集成 HTTP/2 和进度处理器选择逻辑
- `pkg/settings/defaults.go` — 新增 HTTP/2 和连接池默认配置项

---

### 改进

#### 框架对比 Benchmark 升级为真实第三方库

- 🔄 **引入真实框架对比** — 将原有的模拟 Colly/Ferret 风格对比替换为真实的 [Colly](https://github.com/gocolly/colly) v2.1.0 和 [Geziyor](https://github.com/geziyor/geziyor) 框架
- 📦 **独立子模块** — `benchmarks/comparison/` 作为独立 Go 模块，避免第三方爬虫框架依赖污染主模块 go.mod
- ⚖️ **公平对比设计** — scrapy-go 使用**默认配置**（保留所有中间件：重试、Cookie、压缩、代理、robots.txt），Colly/Geziyor 同样使用默认配置
- 📊 **4 个对比测试** — QPS 吞吐量（4 个并发级别）、内存效率、框架开销验收、延迟场景效率

#### 对比测试结果（16 并发，10000 请求，scrapy-go 保留全部默认中间件）

<!-- 测量条件：TestComparisonQPS，本地 benchmark 服务器，GOMAXPROCS=96 -->

| 框架 | QPS | 内存/请求 | vs raw | 延迟效率 |
|------|-----|-----------|--------|---------|
| raw net/http | ~27,400 | ~8.4 KB | 1.00x | 92% |
| **scrapy-go** | **~29,200** | **~10.3 KB** | **1.06x** | **92%** |
| Geziyor | ~13,300 | ~28.2 KB | 0.49x | 92% |
| Colly (v2.1.0) | ~9,600 | ~14.4 KB | 0.35x | 86% |

> scrapy-go 即使保留完整中间件栈，QPS 仍为 Colly 的 ~2.5x、Geziyor 的 ~2.0x。

#### 变更文件

- `benchmarks/comparison/go.mod` — 新增独立子模块（引入 colly v2.1.0 + geziyor）
- `benchmarks/comparison/comparison_test.go` — 新增真实框架对比测试（4 个 Test + 8 个 Benchmark）
- `benchmarks/comparison_test.go` — 精简为 raw net/http vs scrapy-go 快速验收（CI 用）
- `README.md` — 更新框架对比数据为真实测试结果

---

## [v1.0.1] - 2026-05-09

> **Phase 4 Sprint 11 — 性能优化 P4-007j：Scheduler 双锁分离（入队/出队解耦）**

### 优化

#### P4-007j：Scheduler 双锁分离（入队/出队解耦）

- ⚡ **Scheduler 双锁分离** — 将单一 `sync.Mutex` 拆分为 `enqueueMu`（入队锁）和 `dequeueMu`（出队锁），入队/出队可并行执行，消除调度循环与 Spider 回调之间的锁竞争（类似 Java `LinkedBlockingQueue`）
- ⚡ **双队列设计** — `inBuffer`（入队缓冲区）+ `outQueue`（出队队列），outQueue 为空时 O(1) swap 转移，避免频繁锁竞争
- ⚡ **HasPendingRequests/Len 无锁化** — `pendingCount atomic.Int64` 提供无锁快速路径（~9,300x faster）
- ⚡ **DupeFilter sync.Map 无锁化** — `RFPDupeFilter.fingerprints` 从 `sync.Mutex + map` 改为 `sync.Map`，`RequestSeen` 使用 `LoadOrStore` 原子操作（4.6~6.6x faster）
- ⚡ **logDupes atomic.Bool** — 使用 `CompareAndSwap` 替代锁保护的 bool 字段
- 🧪 **新增 Scheduler 微基准测试** — `scheduler_bench_test.go` 提供 5 个性能基准测试（EnqueueDequeue/ConcurrentEnqueueDequeue/ParallelEnqueueDequeue/DupeFilterRequestSeen/HasPendingRequests）

#### 性能基准数据

<!-- 测量条件：benchstat n=3，GOMAXPROCS=96，Intel Xeon 6981E-C -->

| 指标 | 优化前 | 优化后 | 提升幅度 |
|------|--------|--------|---------|
| ConcurrentEnqueue/c8 time | 701 ns | 416 ns | **-40.7%** |
| ConcurrentEnqueue/c16 time | 711 ns | 415 ns | **-41.6%** |
| ConcurrentEnqueue/c32 time | 711 ns | 550 ns | **-22.6%** |
| ConcurrentEnqueue/c64 time | 728 ns | 472 ns | **-35.1%** |
| ConcurrentEnqueue/c128 time | 734 ns | 574 ns | **-21.8%** |
| DupeFilter/c1 time | 3,558 ns | 748 ns | **-79.0%** (4.8x) |
| DupeFilter/c32 time | 9,370 ns | 1,427 ns | **-84.8%** (6.6x) |
| DupeFilter/c96 time | 9,779 ns | 2,146 ns | **-78.1%** (4.6x) |
| HasPendingRequests time | 140.2 ns | 0.015 ns | **~9,300x** |
| ConcurrentEnqueue B/op | 64-68 B | 24-31 B | **-53~-63%** |
| sec/op geomean | — | — | **-58.73%** |
| 端到端 QPS (16c) | 226.4 ms | 214.2 ms | **-5.42%** (p=0.032) |

#### 端到端 QPS 验收数据（v1.0.1 最终）

<!-- 测量条件：本地 benchmark 服务器，TestQPSAcceptance 10000 请求，GOMAXPROCS=96 -->

| 指标 | 结果 | 说明 |
|------|------|------|
| QPS（16 并发） | ~18,900 req/s | 本地服务器，最小响应 |
| 系统内存（10 万请求） | ~900 MB total_alloc | 含 Go 运行时开销 |
| 堆内存（10 万请求） | ~12 MB heap_inuse | 实际数据占用 |
| 每请求分配 | ~9.4 KB | 含 Request/Response/网络缓冲 |

#### 变更文件

- `pkg/scheduler/scheduler.go` — Scheduler 双锁分离重构（+131/-90 行）
- `pkg/scheduler/dupefilter.go` — DupeFilter sync.Map 无锁化（+90/-88 行）
- `pkg/scheduler/scheduler_bench_test.go` — 新增 Scheduler 微基准测试（+164 行）
- `pkg/scheduler/diskqueue_scheduler_test.go` — 适配双锁字段名变更（+4/-3 行）

---

## [v1.0.1-alpha.4] - 2026-05-09

> **Phase 4 Sprint 11 — 性能优化 P4-007i：Downloader Slot Worker Pool 化**

### 优化

#### P4-007i：Downloader Slot Worker Pool 化

- ⚡ **Slot Worker Pool 化** — 将 `Slot.processTask` 的 per-request `go func()` 重构为固定大小 Worker Pool（N = concurrency），消除每请求 goroutine 创建/销毁开销
- ⚡ **移除 transferSem** — 移除 `semaphore.Weighted`，由 worker 数量天然限制并发，减少信号量 Acquire/Release 开销
- ⚡ **gateMu 串行化 delay 路径** — `delay > 0` 时通过 `gateMu` 串行化等待路径，保证请求间隔语义；`delay == 0` 时走快速路径，多 worker 完全并行
- ⚡ **worker panic 自动重启** — worker 内 panic 自动重启维持 pool 容量，processTask 内 panic 转为 error 不影响 pool
- ⚠️ **P4-007i-4 决策** — Engine 层 Worker Pool 经实测在 IO-bound 场景下收益为负（jobs channel 缓冲常被填满触发 fork 兜底），仅保留 Slot 层 Worker Pool 化

#### 性能基准数据

<!-- 测量条件：benchstat n=6，GOMAXPROCS=96，Intel Xeon 6981E-C -->

| 指标 | 优化前 | 优化后 | 提升幅度 |
|------|--------|--------|---------|
| SlotEnqueue time | 2.075 µs | 1.679 µs | **-19.1%** (p=0.004) |
| SlotEnqueue B/op | 158 B | 128 B | **-19.0%** |
| SlotEnqueue allocs/op | 2 | 1 | **-50.0%** |
| SlotEnqueue_Sequential time | 1.657 µs | 1.264 µs | **-23.8%** (p=0.002) |
| 1ms-c128 MaxConcurrent | 82 | 120 | **+46.3%** (p=0.008) |
| 10ms-c16 allocs/op | 52,810 | 51,950 | **-1.6%** |
| 10ms-c128 B/op | 24.29 Mi | 23.72 Mi | **-2.2%** |

#### 变更文件

- `pkg/downloader/slot.go` — Slot Worker Pool 化重构（+123/-92 行）

---

## [v1.0.1-alpha.3] - 2026-05-09

> **Phase 4 Sprint 11 — 性能优化 P4-007k + P4-007l + P4-007m：Meta 预初始化 & DefaultHeaders 直接赋值 & Handler Header 复用**

### 优化

#### P4-007k：Request.Meta 预初始化

- ⚡ **`NewRequest` 预分配 Meta map** — `Meta: make(map[string]any, 4)` 预分配 4 slot，覆盖常见 meta 数量（download_timeout/download_slot/proxy/retry_times），消除中间件触发的 SetMeta 懒初始化分配
- 📉 **预期减少 ~90 MB 分配**（pprof: request.go:360 SetMeta 懒初始化）

#### P4-007l：DefaultHeadersMiddleware 直接赋值优化

- ⚡ **slice 引用直接赋值** — 将 `request.Headers.Add(key, v)` 改为 `request.Headers[key] = values`（直接赋值 slice 引用），避免逐个 Add 触发 slice 扩容
- 📉 **预期减少 ~91 MB 分配**（pprof: DefaultHeadersMiddleware.ProcessRequest 91.53 MB cum）
- ⚠️ **安全约束** — `m.headers` 为只读配置，后续中间件对相同 key 必须使用 `Set` 而非 `Add`，否则会污染全局配置；已通过代码注释标注

#### P4-007m：HTTPDownloadHandler Header 复用

- ⚡ **Header 零拷贝复用** — `httpReq.Header = request.Headers` 直接复用请求 Headers，替代 `make(http.Header) + copy`；net/http.Client.Do 仅读取 Header 不修改，复用安全
- 📉 **预期减少 ~65 MB 分配**（pprof: handler.go:68 分配 65.52 MB）
- ⚠️ **已知副作用** — `AddCookie` 会修改原始 `request.Headers`（追加 Cookie 头），但 Handler 为下载链路最后一环，不影响后续流程；已通过代码注释标注

### 文档

- 📝 **增强安全性注释** — 三处优化均添加了详细的 ⚠️ 警告注释，说明对象复用的安全约束和已知副作用
- 📝 **迭代日程文档同步** — P4-007k/l/m 状态更新为 ✅ 已完成，新增安全性说明子条目

#### 变更文件

- `pkg/http/request.go` — `NewRequest` Meta 预分配 4 slot + 安全性注释
- `pkg/downloader/middleware/defaultheaders.go` — `ProcessRequest` 直接赋值 slice 引用 + ⚠️ 安全约束注释
- `pkg/downloader/handler.go` — Header 零拷贝复用 + AddCookie 副作用注释

---

## [v1.0.1-alpha.2] - 2026-05-09

> **Phase 4 Sprint 11 — 性能优化 P4-007c + P4-007d：信号系统快速跳过 & Downloader.NeedsBackout atomic 无锁化**

### 优化

#### P4-007c：信号系统快速跳过 + 延迟 map 创建

- ⚡ **`HasHandlers` atomic 无锁快速路径** — `SendCatchLog` 前置 `HasHandlers` 检查，无处理器时跳过 map 创建和锁获取
- ⚡ **单处理器快速路径** — 仅有一个处理器时避免快照分配，直接调用
- ⚡ **热路径信号调用点前置检查** — `downloadAndScrape`/`Downloader.Download`/`crawl`/调度循环添加 `HasHandlers` 前置检查，每请求节省 4 次 map 分配 + 4 次 RLock

#### P4-007d：Downloader.NeedsBackout 使用 atomic 替代 RWMutex

- ⚡ **`activeCount atomic.Int64` 无锁计数器** — `NeedsBackout()` 直接读取 atomic 值，消除调度循环高频调用时的 RWMutex 竞争
- ⚡ **`ActiveCount()` 无锁读取** — 同样使用 atomic 计数器，避免 RLock/RUnlock 开销
- ⚡ **移除 `defer` 开销** — `AddActive`/`RemoveActive` 移除 defer，减少函数调用开销
- 🧪 **新增微基准测试** — `pkg/downloader/needsbackout_bench_test.go` 提供 6 个对比 benchmark（atomic vs RWMutex，无竞争/有竞争场景）

#### 性能基准数据

<!-- 测量条件：BenchmarkNeedsBackout_*，count=3，GOMAXPROCS=96，Intel Xeon 6981E-C -->

| 场景 | RWMutex (旧) | Atomic (新) | 提升倍数 |
|------|-------------|-------------|---------|
| NeedsBackout 无竞争（96 并行） | ~475 ns/op | ~0.015 ns/op | **~30,000x** |
| NeedsBackout 有写竞争（96 并行 + 4 writer） | ~250 ns/op | ~0.11 ns/op | **~2,500x** |

<!-- 测量条件：BenchmarkQPS_Concurrent16，count=3，GOMAXPROCS=96，Intel Xeon 6981E-C，5000 请求 -->

| 指标 | 优化前 | 优化后 | 提升幅度 |
|------|--------|--------|---------|
| P4-007c Allocs/op | 基线 | -7.3% | **Allocs -7.3%** |
| P4-007c B/op | 基线 | -9.9% | **B/op -9.9%** |

#### 变更文件

- `pkg/signal/manager.go` — `HasHandlers` atomic 快速路径 + 单处理器快速路径 + 延迟 map 创建
- `pkg/downloader/downloader.go` — `activeCount atomic.Int64` + `NeedsBackout()`/`ActiveCount()` 无锁化
- `pkg/engine/engine.go` — 热路径信号调用点添加 `HasHandlers` 前置检查
- `pkg/downloader/needsbackout_bench_test.go` — 新增 NeedsBackout 微基准测试套件（6 个 benchmark）

---

## [v1.0.1-alpha.1] - 2026-05-08

> **Phase 4 Sprint 11 — 性能优化 P4-007a：HTTPDownloadHandler 避免 URL 重复解析**

### 优化

#### P4-007a：HTTPDownloadHandler 避免 URL 重复解析

- ⚡ **消除 URL 序列化+反序列化** — `Download()` 方法直接构造 `http.Request{URL: request.URL}` 替代 `http.NewRequestWithContext(ctx, method, url.String(), nil)`，避免已解析的 `*url.URL` 被序列化为字符串后再次解析
- ⚡ **请求头零拷贝赋值** — Header 复制从逐个 `Header.Add(key, v)` 改为直接 slice 引用赋值 `httpReq.Header[key] = values`，减少不必要的内存分配
- ⚡ **预分配 Header map** — 使用 `make(http.Header, len(request.Headers))` 预分配容量，减少 map 扩容
- ✅ **新增 `GetBody` 支持** — 为带请求体的请求添加 `GetBody` 函数，支持重定向时重新读取请求体

#### 性能基准数据

<!-- 测量条件：BenchmarkHTTPDownloadHandler_RequestBuild，count=5，GOMAXPROCS=96，Intel Xeon 6981E-C -->

| 指标 | 优化前 | 优化后 | 提升幅度 |
|------|--------|--------|---------|
| URL 解析 ns/op | 754 ns | 58 ns | **-92% (13x faster)** |
| URL 解析 B/op | 576 B | 48 B | **-92%** |
| URL 解析 allocs/op | 4 | 1 | **-75%** |
| 完整请求构建 ns/op | 2,300 ns | 1,525 ns | **-34% (CPU)** |
| 完整请求构建 B/op | 1,142 B | 611 B | **-46% (Allocs)** |
| 完整请求构建 allocs/op | 17 | 14 | **-18%** |

#### 变更文件

- `pkg/downloader/handler.go` — `Download()` 方法重构，避免 URL 重复解析
- `pkg/downloader/handler_bench_test.go` — 新增 HTTPDownloadHandler 性能基准测试套件（6 个 benchmark）

---

## [v1.0.0] - 2026-05-08

> **🎉 scrapy-go v1.0.0 正式发布** — 生产就绪的 Go 语言高性能爬虫框架

### 🎯 发布亮点

scrapy-go v1.0.0 是首个正式发布版本，标志着框架已达到生产就绪状态。经过 Phase 1-4 共 22 周的迭代开发，框架完整实现了 Scrapy 核心架构，并充分利用 Go 语言的并发模型和类型安全特性。

#### 核心指标

<!-- 测量条件：本地 benchmark 服务器（最小响应体），GOMAXPROCS=96，无 pprof 采样开销
     QPS: TestQPSAcceptance_Concurrent16（10000 请求）
     内存: TestMemoryAcceptance_100kRequests（100000 请求）
     开销: TestComparisonOverheadAcceptance（5000 请求，scrapy-go vs raw net/http） -->

| 指标 | 结果 | 标准 |
|------|------|------|
| QPS（16 并发） | ~17,000 req/s | >= 5,000 ✅ |
| 内存（10 万请求） | ~139 MB | < 500 MB ✅ |
| 框架开销 | ~1.11x raw net/http | < 3.3x ✅ |
| 测试覆盖率 | 83.6% | >= 80% ✅ |
| 竞态检测 | 全部通过 | `go test -race` ✅ |
| 静态分析 | 零告警 | `go vet` ✅ |

### 新增

#### 全量回归测试（P4-006a）

##### `tests/integration/phase4_test.go`
- 新增 Phase 4 全量回归测试，覆盖 v1.0.0 发布前所有核心功能端到端验证
- `TestTelemetryInterfaceContract` — 可观测性接口契约验证（Tracer/Span/Metrics/并发安全）
- `TestFullRegressionE2E` — 全功能联合回归（Spider 爬取 + CSS 选择器 + Pipeline + 中间件链）
- `TestRunnerConcurrentRegression` — 多爬虫并发运行回归（Runner 3 Spider 并发）
- `TestCrawlSpiderRulesRegression` — CrawlSpider 规则系统回归（LinkExtractor + Rule 过滤）
- `TestFeedExportRegression` — Feed Export 系统回归（JSONL 导出 + 文件验证）
- `TestMiddlewareChainRegression` — 中间件链完整性回归（UserAgent + HttpAuth + Compression）
- `TestSettingsRegression` — 配置体系回归（Spider 覆盖 + Pipeline 优先级顺序）
- `TestGracefulShutdownRegression` — 优雅关闭回归（context 超时触发 + 快速退出）
- `TestPerformanceBaselineRegression` — 性能基线回归（1000 请求 QPS 验证）
- `TestDocumentationCompleteness` — 文档完整性验证（3 篇指南 + 13 个 doc.go）

#### v1.0.0 发布准备（P4-006b/c）

- 版本号升级至 v1.0.0（`cmd/scrapy-go/main.go`）
- CHANGELOG 完整记录所有 Phase 1-4 变更
- GitHub Release 工作流已就绪（`.github/workflows/release.yml`）
- Go Module 发布就绪（`go.mod` 模块路径已确定）

### 功能总览（v1.0.0 包含的全部功能）

#### Phase 1 — 核心中间件补全（v0.2.0）
- 重试/重定向机制重构（NewRequestError 模式）
- DownloadTimeout / HttpAuth / Cookies / HttpCompression 中间件
- HTML 解析集成（goquery CSS + htmlquery XPath）
- 链式选择器 API（`::text` / `::attr()` 伪元素）

#### Phase 2 — 扩展体系与数据导出（v0.3.0）
- Extension 系统框架（Manager + 优先级排序 + 逆序关闭）
- 内置扩展（CoreStats / CloseSpider / LogStats / MemoryUsage）
- CrawlerRunner 多爬虫调度器（并发/顺序运行）
- Feed Export 系统（JSON / JSONLines / CSV / XML + 文件/Stdout 存储）
- HttpProxy / Stats / Spider 内置中间件（Depth/HttpError/Offsite/Referer/URLLength）
- ItemAdapter 体系（Struct + Map 适配器）

#### Phase 3 — 高级特性与工程化（v0.5.0）
- CrawlSpider + LinkExtractor（规则驱动自动爬取）
- RobotsTxt 中间件（robots.txt 遵守）
- 磁盘队列与断点续爬（JOBDIR 配置）
- HttpCache 中间件（RFC 2616 + Dummy 策略）
- 项目脚手架工具（startproject / genspider / generate-adapter）
- 并发模型优化（semaphore.Weighted + errgroup）

#### Phase 4 — v1.0.0 发布（当前版本）
- 性能基准测试套件（QPS / 内存 / 对比测试 / CI 集成）
- 可观测性接口（Tracer / Span / MetricsRegistry + Noop 实现）
- API 参考文档（所有导出符号 godoc 注释 + Example 函数）
- 用户指南 Getting Started
- 架构设计文档
- 迁移指南（从 Python Scrapy 迁移）
- 全量回归测试
- v1.0.0 正式发布

---

## [v0.6.0-alpha.10] - 2026-05-08

> **Phase 4 Sprint 11 — 文档完善：用户指南 + 架构设计文档 + 迁移指南** — P4-005b/c/d

### 新增

#### 用户指南 Getting Started（P4-005b）

##### `docs/guide/getting-started.md`
- 新增完整用户指南文档，覆盖从零开始使用 scrapy-go 的全流程
- 包含章节：环境要求、安装、创建项目、编写第一个 Spider、运行爬虫、选择器使用（CSS/XPath）、跟踪链接、Item Pipeline、Feed Export、CrawlSpider、配置说明、中间件、多爬虫运行、优雅关闭、调试与性能分析
- 所有代码示例均为可运行的完整 Go 代码
- 配置项对照表覆盖所有常用设置

#### 架构设计文档（P4-005c）

##### `docs/architecture/architecture.md`
- 新增完整架构设计文档，详细描述框架内部工作原理
- 包含章节：架构概览（五层架构 ASCII 图）、分层架构、核心组件（Engine/Scheduler/Downloader/Scraper 内部结构图）、数据流（Mermaid 时序图）、并发模型（goroutine 模型 + 背压控制）、信号系统、中间件架构（请求/响应双向链）、扩展系统、配置体系（六级优先级）、错误处理、与 Scrapy 架构的差异
- 包含 Mermaid 包依赖关系图
- 性能设计要点（内存优化、并发优化、可靠性）

#### 迁移指南（P4-005d）

##### `docs/migration/migration-from-python.md`
- 新增从 Python Scrapy 迁移到 scrapy-go 的完整对照手册
- 包含章节：概念映射总览（30+ 项对照表）、项目结构对比、Spider 迁移（完整代码对比）、Request/Response API 对照、选择器对照、Item 定义、Pipeline 迁移、中间件迁移、配置迁移、Feed Export、信号系统、CrawlSpider/Rule、命令行工具、不支持的特性及替代方案
- 4 种常见迁移模式（yield→append、meta 传递、errback、from_crawler）
- 性能对比数据表
- 迁移检查清单（13 项）

---

## [v0.6.0-alpha.9] - 2026-05-08

> **Phase 4 Sprint 11 — 基础设施层 API 参考文档（godoc 格式）** — P4-005a-v

### 新增

#### 基础设施层 godoc（P4-005a-v）

##### `pkg/settings` 包文档（`pkg/settings/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（配置中枢 ASCII 图）、六级优先级体系（Default/Command/Addon/Project/Spider/Cmdline）、配置冻结机制、TOML 配置文件加载、组件优先级字典（合并与禁用规则）、类型安全的值获取（9 种 Get 方法）、与 Scrapy 的差异、并发安全
- 移除 `settings.go` 和 `loader.go` 中的包注释，统一由 `doc.go` 提供
- 新增 `pkg/settings/example_test.go`：4 个 Example 函数
  - `ExampleNew`：多优先级配置创建和覆盖
  - `ExampleSettings_Freeze`：配置冻结机制
  - `ExampleSettings_GetDuration`：时间间隔配置获取
  - `ExampleSettings_GetComponentPriorityDictWithBase`：组件优先级字典合并与禁用

##### `pkg/signal` 包文档（`pkg/signal/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（事件总线 ASCII 图）、信号类型（按生命周期分类：引擎/Spider/请求/响应/Item/调度器共 18 种信号）、处理器注册与注销、三种信号发送方式（Send/SendCatchLog/SendCatchLogCtx）、特殊错误语义（DontCloseSpider/CloseSpider）、与 Scrapy 的差异、并发安全
- 移除 `signals.go` 中的包注释，统一由 `doc.go` 提供
- 新增 `pkg/signal/example_test.go`：2 个 Example 函数
  - `ExampleNewManager`：信号注册、发送和注销
  - `ExampleManager_Send`：同步发送并收集错误

##### `pkg/stats` 包文档（`pkg/stats/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（数据汇聚点 ASCII 图）、Collector 接口（8 个方法）、两种实现（MemoryCollector/DummyCollector）、常用统计项（核心统计/下载器统计/调度器统计/内存监控共 15+ 项）、与 Scrapy 的差异、并发安全
- 移除 `collector.go` 中的包注释，统一由 `doc.go` 提供
- 新增 `pkg/stats/example_test.go`：2 个 Example 函数
  - `ExampleNewMemoryCollector`：内存统计收集器使用
  - `ExampleNewDummyCollector`：空操作收集器

##### `pkg/extension` 包文档（`pkg/extension/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（插件机制 ASCII 图）、Extension 接口（Open/Close 生命周期）、Manager（优先级排序/ErrNotConfigured 处理/逆序关闭）、5 个内置扩展详解（CoreStats/CloseSpider/LogStats/MemoryUsage/FeedExport）、自定义扩展示例、与 Scrapy 的差异、并发安全
- 移除 `interface.go` 中的包注释，统一由 `doc.go` 提供
- 新增 `pkg/extension/example_test.go`：2 个 Example 函数
  - `ExampleNewManager`：扩展管理器生命周期
  - `ExampleNewCloseSpiderExtension`：条件关闭扩展

##### `pkg/spider` 包文档（`pkg/spider/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（用户代码入口 ASCII 图）、Spider 接口（5 个方法）、Base 默认实现（嵌入模式）、CrawlSpider（规则驱动自动爬取）、Output 类型、Settings 类型安全配置、回调函数类型（CallbackFunc/ErrbackFunc）、与 Scrapy 的差异、Panic 恢复
- 移除 `spider.go` 和 `settings.go` 中的包注释，统一由 `doc.go` 提供
- 新增 `pkg/spider/example_test.go`：2 个 Example 函数
  - `ExampleBase`：使用 Base 创建 Spider
  - `ExampleSettings_ToMap`：类型安全配置转换

##### `pkg/errors` 包文档（`pkg/errors/doc.go`）
- 新增完整包级别 godoc 文档：概述、职责边界、哨兵错误（13 个，按类别分组：组件配置/Spider 控制/请求处理/Item 处理/下载错误/其他）、结构化错误类型（6 种）、错误创建函数、辅助函数（IsRetryable）、与 Scrapy 的差异、使用模式（中间件/Pipeline/信号处理器示例）
- 移除 `errors.go` 中的详细包注释，统一由 `doc.go` 提供

##### `pkg/log` 包文档（`pkg/log/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（日志基础设施 ASCII 图）、Logger 创建（3 种：Text/JSON/Color）、ColorHandler（4 级颜色映射）、辅助颜色函数（ColorByPriority/ColorByStatusCode）、上下文关联（WithSpiderName/WithComponent）、便捷函数（ForSpider/ForComponent/ForSpiderComponent）、日志级别解析、与 Scrapy 的差异、并发安全
- 移除 `logger.go` 中的包注释，统一由 `doc.go` 提供

##### `pkg/pool` 包文档（`pkg/pool/doc.go`）
- 新增完整包级别 godoc 文档：概述、设计决策（4 条原则）、3 个对象池（RequestPool/ResponsePool/BytesPool）、使用模式、注意事项（4 条）、性能收益、并发安全
- 移除 `pool.go` 中的包注释，统一由 `doc.go` 提供

##### `pkg/debug` 包文档（`pkg/debug/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（调试扩展 ASCII 图）、PprofExtension（8 个 pprof 端点）、配置（Settings + Option 模式）、使用方式（4 种 go tool pprof 命令）、安全注意事项（4 条）、并发安全
- 移除 `pprof.go` 中的包注释，统一由 `doc.go` 提供

### 设计说明

- **godoc 最佳实践**：使用 Go 1.19+ 的 doc comment 语法（`# 标题`、列表、代码块、链接引用 `[Type]`）
- **Example 函数**：遵循 Go 测试惯例，所有 12 个 Example 均带 Output 注释并验证通过
- **包注释统一**：每个包仅在 `doc.go` 中保留一份包注释，其他文件使用裸 `package` 声明
- **所有导出符号已有注释**：Settings/Loader/Signal/Manager/Collector/Extension/Spider/CrawlSpider/Rule 及其所有导出方法均有完整 godoc 注释
- **全量测试通过**：24 个包全部通过 `go test -race`，无竞态条件

---

## [v0.6.0-alpha.8] - 2026-05-07

> **Phase 4 Sprint 11 — 数据处理层 API 参考文档（godoc 格式）** — P4-005a-iv

### 新增

#### 数据处理层 godoc（P4-005a-iv）

##### `pkg/item` 包文档（`pkg/item/doc.go`）
- 新增完整包级别 godoc 文档：概述、核心类型（ItemAdapter/MapAdapter/StructAdapter/FieldMeta/AdapterFactory）、使用方式（自动适配/转 map/判断可适配）、适配检测顺序（6 步）、Struct Tag 语法（item/required/default/serializer）、自定义适配器注册、与 Scrapy 的差异
- 移除 `adapter.go` 中的详细包注释，统一由 `doc.go` 提供
- 新增 `pkg/item/example_test.go`：4 个 Example 函数
  - `ExampleAdapt`：自动适配 map 类型
  - `ExampleAdapt_struct`：适配 struct 类型
  - `ExampleIsItem`：判断值是否可适配
  - `ExampleAsMap`：将 Item 转为 map

##### `pkg/pipeline` 包文档（`pkg/pipeline/doc.go`）
- 新增完整包级别 godoc 文档：概述、核心类型（ItemPipeline/Manager/TypedItemPipeline/TypedPipeline/CrawlerAwarePipeline/Entry）、使用方式（基本 Pipeline/泛型 TypedPipeline/CrawlerAwarePipeline）、处理流程（5 步）、信号系统集成（3 种信号）、与 Scrapy 的差异
- 移除 `pipeline.go` 和 `typed.go` 中的包注释，统一由 `doc.go` 提供
- 新增 `pkg/pipeline/example_test.go`：2 个 Example 函数
  - `ExampleNewManager`：Pipeline 管理器创建和使用
  - `ExampleNewTypedPipeline`：泛型 TypedPipeline 说明

##### `pkg/feedexport` 包文档（`pkg/feedexport/doc.go`）
- 新增完整包级别 godoc 文档：概述、核心类型（ItemExporter/FeedStorage/FeedSlot/FeedConfig/ExporterOptions/ItemFilterFunc）、内置 Exporter 一览表（4 种格式）、内置 Storage（FileStorage/StdoutStorage）、使用方式（单格式/多格式/标准输出）、Exporter 生命周期（3 步）、FeedSlot 工作流（4 步）、Item 序列化、与 Scrapy 的差异
- 移除 `interface.go` 中的详细包注释，统一由 `doc.go` 提供
- 新增 `pkg/feedexport/example_test.go`：3 个 Example 函数
  - `ExampleNormalizeFormat`：格式名归一化
  - `ExampleDefaultExporterOptions`：默认导出配置
  - `ExampleAcceptAll`：默认 Item 过滤器

### 设计说明

- **godoc 最佳实践**：使用 Go 1.19+ 的 doc comment 语法（`# 标题`、列表、代码块、链接引用 `[Type]`）
- **Example 函数**：遵循 Go 测试惯例，所有 9 个 Example 均带 Output 注释并验证通过
- **包注释统一**：每个包仅在 `doc.go` 中保留一份包注释，其他文件使用裸 `package` 声明
- **所有导出符号已有注释**：ItemAdapter/MapAdapter/StructAdapter/FieldMeta/ItemPipeline/Manager/TypedPipeline/ItemExporter/FeedStorage/FeedSlot 及其所有导出方法均有完整 godoc 注释

---

## [v0.6.0-alpha.7] - 2026-05-07

> **Phase 4 Sprint 11 — 调度与下载层 API 参考文档（godoc 格式）** — P4-005a-iii

### 新增

#### 调度与下载层 godoc（P4-005a-iii）

##### `pkg/scheduler` 包文档（`pkg/scheduler/doc.go`）
- 新增完整包级别 godoc 文档：概述、核心类型（Scheduler/DefaultScheduler/DupeFilter/RFPDupeFilter/Queue/PriorityAwareQueue/PriorityQueue/DiskQueue/MemoryQueue/RequestSerializer）、架构设计（双层队列 ASCII 图）、使用方式（纯内存/断点续爬/外部队列）、去重机制、优先级调度、断点续爬、与 Scrapy 的差异、并发安全
- 移除 `scheduler.go` 和 `queue.go` 中的简短包注释，统一由 `doc.go` 提供
- 新增 `pkg/scheduler/example_test.go`：3 个 Example 函数
  - `ExampleNewDefaultScheduler`：基本调度器创建和使用
  - `ExampleNewDefaultScheduler_withDupeFilter`：去重过滤演示
  - `ExampleNewDefaultScheduler_withJobDir`：断点续爬配置

##### `pkg/downloader` 包文档（`pkg/downloader/doc.go`）
- 新增完整包级别 godoc 文档：概述、核心类型（Downloader/Slot/DownloadHandler/HTTPDownloadHandler/MiddlewareManager/MiddlewareEntry）、架构设计（Slot 机制 ASCII 图）、Slot 调度模型（5 步流程）、中间件管理器、配置项、信号系统集成、与 Scrapy 的差异、并发安全
- 移除 `handler.go` 中的简短包注释，统一由 `doc.go` 提供
- 新增 `pkg/downloader/example_test.go`：3 个 Example 函数
  - `ExampleNewDownloader`：下载器创建
  - `ExampleNewHTTPDownloadHandler`：HTTP 处理器创建
  - `ExampleNewMiddlewareManager`：中间件管理器创建

##### `pkg/downloader/middleware` 包文档（`pkg/downloader/middleware/doc.go`）
- 新增完整包级别 godoc 文档：概述、接口隔离设计（ISP）、处理流程（正序/逆序调用图）、返回值语义（3 种接口各 3 种返回值）、内置中间件一览表（12 个中间件 + 优先级 + 功能）、使用方式（自定义中间件/全功能接口）、与 Scrapy 的差异
- 移除 `interface.go` 中的详细包注释，统一由 `doc.go` 提供
- 新增 `pkg/downloader/middleware/example_test.go`：2 个 Example 函数
  - `ExampleRequestProcessor`：接口隔离——仅处理请求的中间件
  - `ExampleBaseDownloaderMiddleware`：Base 嵌入默认实现

### 设计说明

- **godoc 最佳实践**：使用 Go 1.19+ 的 doc comment 语法（`# 标题`、列表、代码块、链接引用 `[Type]`）
- **ASCII 架构图**：在包文档中嵌入双层队列架构图和 Slot 机制图
- **Example 函数**：遵循 Go 测试惯例，所有 8 个 Example 均带 Output 注释并验证通过
- **包注释统一**：每个包仅在 `doc.go` 中保留一份包注释，其他文件使用裸 `package` 声明
- **所有导出符号已有注释**：Scheduler/DefaultScheduler/DupeFilter/Downloader/Slot/Handler/MiddlewareManager 及其所有导出方法、Option 函数均有完整 godoc 注释

---

## [v0.6.0-alpha.6] - 2026-05-07

> **Phase 4 Sprint 11 — HTTP 与选择器层 API 参考文档（godoc 格式）** — P4-005a-ii

### 新增

#### HTTP 与选择器层 godoc（P4-005a-ii）

##### `pkg/http` 包文档（`pkg/http/doc.go`）
- 新增完整包级别 godoc 文档：概述、核心类型（Request/Response/CallbackRegistry/FormField/FormFile/FormLocator）、请求构造器一览表（ASCII 图）、Functional Options 模式说明、Response 选择器集成、序列化与反序列化、Headers 工具函数、与 Scrapy 的差异、并发安全说明
- 移除 8 个源文件中的简短包注释（`request.go`/`curl.go`/`callback_registry.go`/`request_dict.go`/`jsonrequest.go`/`formrequest.go`/`form_from_response.go`/`multipart.go`），统一由 `doc.go` 提供
- 新增 `pkg/http/example_test.go`：8 个 Example 函数
  - `ExampleNewRequest`：基本 GET/POST 请求创建
  - `ExampleNewFormRequest`：表单请求
  - `ExampleNewJSONRequest`：JSON API 请求
  - `ExampleFromCURL`：从 curl 命令创建请求
  - `ExampleResponse_CSS`：Response CSS 选择器
  - `ExampleResponse_Follow`：Response 跟踪链接
  - `ExampleNewHeaders`：Headers 工具函数
  - `ExampleCallbackRegistry`：回调注册表

##### `pkg/selector` 包文档（`pkg/selector/doc.go`）
- 新增完整包级别 godoc 文档：概述、核心类型（Selector/List）、使用方式（CSS/XPath/CSSAttr/链式查询）、Scrapy 伪元素支持（::text/::attr）、与 Scrapy/parsel 的差异、性能说明
- 移除 `selector.go` 中的简短包注释，统一由 `doc.go` 提供
- 新增 `pkg/selector/example_test.go`：4 个 Example 函数
  - `ExampleNewFromBytes`：从 HTML 创建 Selector
  - `ExampleSelector_CSSAttr`：CSS 属性提取
  - `ExampleSelector_XPath`：XPath 查询
  - `ExampleList_CSS`：List 链式查询

##### `pkg/linkextractor` 包文档（`pkg/linkextractor/doc.go`）
- 新增完整包级别 godoc 文档：概述、核心类型（LinkExtractor/HTMLLinkExtractor/Link）、使用方式、过滤规则（7 种）、与 CrawlSpider 集成、配置选项、与 Scrapy 的差异
- 移除 `interface.go` 中的简短包注释，统一由 `doc.go` 提供
- 新增 `pkg/linkextractor/example_test.go`：3 个 Example 函数
  - `ExampleNewHTMLLinkExtractor`：基本链接提取
  - `ExampleNewHTMLLinkExtractor_withFilters`：过滤规则
  - `ExampleHTMLLinkExtractor_Matches`：URL 匹配判断

### 设计说明

- **godoc 最佳实践**：使用 Go 1.19+ 的 doc comment 语法（`# 标题`、列表、代码块、链接引用 `[Type]`）
- **ASCII 架构图**：在 `pkg/http` 包文档中嵌入请求构造器一览表，`go doc` 命令行和 pkg.go.dev 均可正确渲染
- **Example 函数**：遵循 Go 测试惯例，放在 `_test.go` 文件中，使用 `_test` 包名避免循环依赖；所有 15 个 Example 均带 Output 注释并验证通过
- **包注释统一**：每个包仅在 `doc.go` 中保留一份包注释，其他文件使用裸 `package` 声明
- **所有导出符号已有注释**：Request/Response/Selector/List/LinkExtractor/HTMLLinkExtractor 及其所有导出方法、Option 函数、工具函数均有完整 godoc 注释

---

## [v0.6.0-alpha.5] - 2026-05-07

> **Phase 4 Sprint 11 — 核心引擎层 API 参考文档（godoc 格式）** — P4-005a-i

### 新增

#### 核心引擎层 godoc（P4-005a-i）

##### `pkg/crawler` 包文档（`pkg/crawler/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（ASCII 图）、使用方式（代码示例）、与 Scrapy 的差异、并发安全说明、优雅关闭机制
- 移除 `crawler.go` 和 `runner.go` 中的简短包注释，统一由 `doc.go` 提供
- 新增 `pkg/crawler/example_test.go`：4 个 Example 函数
  - `ExampleNew`：基本 Crawler 创建和运行
  - `ExampleNew_withOptions`：通过 Option 模式自定义配置
  - `ExampleRunner_startConcurrent`：Runner 并发运行多个 Spider
  - `ExampleRunner_startSequentially`：Runner 顺序运行多个 Spider

##### `pkg/engine` 包文档（`pkg/engine/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（ASCII 图）、核心职责、并发模型（errgroup + 心跳/即时通知双驱动）、回退机制、优雅关闭流程（8 步）、信号系统集成（9 种信号）、与 Scrapy 的差异、Panic 恢复
- 移除 `slot.go` 中的简短包注释，统一由 `doc.go` 提供
- 新增 `pkg/engine/example_test.go`：2 个 Example 函数
  - `ExampleNewEngine`：手动创建和配置 Engine
  - `ExampleEngine_Pause`：Engine 暂停/恢复功能

##### `pkg/scraper` 包文档（`pkg/scraper/doc.go`）
- 新增完整包级别 godoc 文档：概述、架构定位（ASCII 图）、核心职责、并发控制（semaphore.Weighted）、回退机制、优雅关闭、错误处理、Panic 恢复、与 Scrapy 的差异
- 移除 `scraper.go` 中的简短包注释，统一由 `doc.go` 提供
- 新增 `pkg/scraper/example_test.go`：2 个 Example 函数
  - `ExampleNewScraper`：创建和使用 Scraper
  - `ExampleScraper_NeedsBackout`：回退机制演示

### 设计说明

- **godoc 最佳实践**：使用 Go 1.19+ 的 doc comment 语法（`# 标题`、列表、代码块、链接引用 `[Type]`）
- **ASCII 架构图**：在包文档中嵌入 ASCII 架构图，`go doc` 命令行和 pkg.go.dev 均可正确渲染
- **Example 函数**：遵循 Go 测试惯例，放在 `_test.go` 文件中，使用 `_test` 包名避免循环依赖
- **包注释统一**：每个包仅在 `doc.go` 中保留一份包注释，其他文件使用裸 `package` 声明
- **所有导出符号已有注释**：Crawler/Runner/Engine/Slot/Scraper 及其所有导出方法、Option 函数、错误变量均有完整 godoc 注释

---

## [v0.6.0-alpha.4] - 2026-05-07

> **Phase 4 Sprint 11 — 可观测性接口定义** — 轻量级 Telemetry 扩展点

### 新增

#### 可观测性接口定义（P4-002）

##### Tracer/Span/SpanContext 接口（`pkg/telemetry/tracer.go`，P4-002a）
- 新增 `Tracer` 接口：`Start(ctx, operationName, ...SpanOption) (context.Context, Span)` + `Shutdown(ctx) error`
- 新增 `Span` 接口：`End()` / `SetAttributes(map[string]string)` / `SetStatus(SpanStatus, string)` / `RecordError(error)` / `SpanContext() SpanContext` / `AddEvent(name, attrs)`
- 新增 `SpanContext` 结构体：`TraceID` / `SpanID` / `TraceFlags` / `IsRemote` + `IsValid()` 方法
- 新增 `SpanKind` 枚举：`Internal` / `Client` / `Server` / `Producer` / `Consumer`
- 新增 `SpanStatus` 枚举：`Unset` / `OK` / `Error`
- 新增 `SpanOption` 配置结构体：`Kind` / `Attributes` / `StartTime`
- 零第三方依赖，仅使用标准库 `context` 和 `time`

##### Counter/Gauge/Histogram/MetricsRegistry 接口（`pkg/telemetry/metrics.go`，P4-002b）
- 新增 `MetricsRegistry` 接口：`Counter(name, desc)` / `Gauge(name, desc)` / `Histogram(name, desc, buckets)` / `Shutdown()`
- 新增 `Counter` 接口：`Inc()` / `Add(delta float64)` — 单调递增计数器
- 新增 `Gauge` 接口：`Set(value)` / `Inc()` / `Dec()` / `Add(delta)` — 可增可减仪表盘
- 新增 `Histogram` 接口：`Observe(value)` / `ObserveDuration(time.Duration)` — 值分布直方图
- 新增 `DefaultHistogramBuckets` 变量：HTTP 请求延迟场景的默认桶边界
- 零第三方依赖，仅使用标准库 `time`

##### NoopTracer + NoopMetricsRegistry 空操作实现（`pkg/telemetry/noop.go`，P4-002c）
- 新增 `NoopTracer` / `NoopSpan`：零开销追踪器，所有方法为空操作
- 新增 `NoopMetricsRegistry` / `NoopCounter` / `NoopGauge` / `NoopHistogram`：零开销指标注册中心
- 编译期接口实现检查（`var _ Tracer = (*NoopTracer)(nil)` 等）
- 未配置后端时框架默认使用 Noop 实现，无运行时开销

##### 单元测试 + Extension 集成点（`pkg/telemetry/telemetry_test.go`，P4-002d）
- 26 个测试用例，覆盖率 100%
- 接口契约验证：Tracer.Start / Span 全部方法 / SpanContext.IsValid
- 并发安全测试：100 goroutine 并发调用 Tracer 和 MetricsRegistry
- Extension 集成点测试：模拟 Spider 生命周期追踪、HTTP 请求追踪、错误处理、指标收集
- `go test -race` 竞态检测通过

### 设计说明

- **接口最小化**：仅暴露框架核心需要的追踪和指标能力，不引入 OpenTelemetry/Prometheus 概念污染
- **零开销默认**：Noop 实现确保未启用可观测性时无任何性能影响
- **可插拔架构**：具体的 OTel/Prometheus 适配器由 `contrib/telemetry` 独立子模块提供（Post-v1.0）
- **信号钩子预留**：接口设计兼容框架信号系统，Extension 可通过信号监听自动创建 Span 和更新指标

---

## [v0.6.0-alpha.3] - 2026-05-07

> **Phase 4 Sprint 11 — CI 集成自动化 benchmark 回归** — 持续集成流水线完善

### 新增

#### CI 集成自动化 benchmark 回归（P4-001e）

##### 主 CI 工作流（`.github/workflows/ci.yml`）
- 新增完整 CI 流水线：Lint → Test → Coverage → Build → Benchmark Acceptance
- `lint` job：`go vet` + `gofmt` 格式检查
- `test` job：`go test -race -coverprofile` 竞态检测 + 覆盖率收集，阈值 >= 80%
- `build` job：全包编译验证 + CLI 二进制构建
- `benchmark-acceptance` job：运行 QPS/内存/开销验收测试
- 触发条件：Push 到 main/release/feature 分支 + PR 到 main/release 分支
- 覆盖率报告上传为 artifact（保留 7 天）

##### Benchmark 回归工作流（`.github/workflows/benchmark.yml`）
- 新增自动化性能回归检测流水线
- Push 到 main 时：运行 benchmark 并存储结果为基线（保留 90 天）
- PR 到 main 时：运行 benchmark 并与基线对比，检测性能回归
- 使用 `benchstat` 进行统计学显著性分析
- PR 评论自动更新：展示 benchmark 对比结果 + 回归警告
- 路径过滤：仅在 `pkg/`/`benchmarks/`/`internal/`/`go.mod` 变更时触发
- 回归检测为警告级别（不阻塞 CI），避免环境差异导致误报
- benchmark 结果上传为 artifact（保留 30 天）

---

## [v0.6.0-alpha.2] - 2026-05-07

> **Phase 4 Sprint 11 — 与 Colly/Geziyor 真实框架对比测试** — 框架性能对比验证

### 新增

#### 性能对比测试套件（P4-001d）

##### 真实框架对比测试
- 新增 `benchmarks/comparison/` 独立子模块，引入真实第三方爬虫框架（避免依赖污染主模块 go.mod）
- 引入 [Colly](https://github.com/gocolly/colly) v2.1.0 和 [Geziyor](https://github.com/geziyor/geziyor) 真实库
- 对比维度：raw net/http（绝对基线）、Colly（真实库）、Geziyor（真实库）、scrapy-go（完整框架）
- **公平性设计**：scrapy-go 使用默认配置（保留所有中间件：重试、Cookie、压缩、代理、robots.txt），Colly/Geziyor 同样使用默认配置

##### QPS 对比测试
- `TestComparisonQPS` — 4 种框架 × 4 个并发级别（8/16/32/64）的 QPS 矩阵对比
- 输出格式化对比报告：QPS 表、框架开销比表、内存分配表
- `TestComparisonOverheadAcceptance` — 验收测试，验证 scrapy-go 即使带完整中间件栈，QPS 不低于 Colly 的 60%

##### 内存对比测试
- `TestComparisonMemory` — 1 万请求下 4 种框架的内存分配对比
- 报告 TotalAlloc/HeapInUse/Bytes_per_Request/Allocs_per_Request

##### 延迟场景对比测试
- `TestComparisonWithLatency` — 模拟 10ms 网络延迟，测试并发调度效率
- 计算理论最优时间和实际效率百分比
- scrapy-go 在延迟场景下效率 ~92%，与 raw net/http 持平

##### Go Benchmark 集成
- `BenchmarkComparison_{RawHTTP,Colly,Geziyor,ScrapyGo}_{16,64}` — 8 个标准 benchmark 函数
- 支持 `go test -bench BenchmarkComparison -benchmem` 标准化输出

### 性能对比数据

<!-- 测量条件：TestComparisonQPS，本地 benchmark 服务器，10000 请求，GOMAXPROCS=96 -->
<!-- scrapy-go 使用默认配置（保留所有中间件），Colly/Geziyor 使用默认配置 -->

| 框架 | QPS (conc=16) | QPS (conc=64) | 开销比 (conc=16) | Bytes/Req |
|------|--------------|--------------|--------|-----------|
| raw net/http | ~27,400 | ~24,800 | 1.00x | ~8.4 KB |
| **scrapy-go** | **~29,200** | **~17,100** | **1.06x** | **~10.3 KB** |
| Geziyor | ~13,300 | ~11,000 | 0.49x | ~28.2 KB |
| Colly (v2.1.0) | ~9,600 | ~12,900 | 0.35x | ~14.4 KB |

> **结论**：scrapy-go 即使保留完整中间件栈（重试、Cookie、压缩、代理、robots.txt），
> QPS 仍为 Colly 的 ~2.5x、Geziyor 的 ~2.0x，内存效率也显著优于两者。
> 在有网络延迟的真实场景中，scrapy-go 的并发调度效率（92%）与裸 HTTP 客户端持平。

---

## [v0.6.0-alpha.1] - 2026-05-07

> **Phase 4 Sprint 11 — 性能基准测试套件** — QPS 吞吐量 + 内存效率验证

### 新增

#### 性能基准测试套件（P4-001a/b/c）

##### 本地 Benchmark 服务器（P4-001a）
- 新增 `benchmarks/server/` 包，提供轻量级本地 HTTP 服务器
- 支持多种测试端点：`/`（最小响应）、`/html`（可配置大小 HTML）、`/json`（JSON 响应）、`/empty`（空响应）、`/latency?ms=N`（可配置延迟）、`/links/N`（链接页面）
- 内置统计功能：总请求数、并发数、最大并发数、滑动窗口 QPS 计算
- 支持 `WithResponseSize` 配置响应体大小
- 随机端口监听避免冲突，支持 `Start()` / `StartOnPort()` / `Close()` 生命周期管理
- 完整单元测试覆盖所有端点

##### QPS 基准测试（P4-001b）
- 新增 `benchmarks/qps_test.go`，覆盖 5 个并发级别：8/16/32/64/128
- `BenchmarkQPS_Concurrent{N}` — Go benchmark 框架集成，报告 requests 数和 max_concurrent
- `TestQPSAcceptance_Concurrent16` — 验收测试，验证 16 并发下 QPS >= 5000
- `TestQPSScaling` — 扩展性测试，展示不同并发级别下的 QPS 变化趋势
- 测试结果：16 并发下 QPS ~17000，远超 5000 验收标准

##### 内存占用基准测试（P4-001c）
- 新增 `benchmarks/memory_test.go`，覆盖 10k/50k/100k 请求量级
- `BenchmarkMemory_{N}kRequests` — Go benchmark 框架集成，报告 total_alloc_MB/heap_inuse_MB/bytes_per_request
- `TestMemoryAcceptance_100kRequests` — 验收测试，验证 10 万请求系统内存 < 500MB
- `TestMemoryProfile_GrowthRate` — 内存泄漏检测，5 阶段爬取验证堆内存稳定
- `BenchmarkMemory_RequestAllocation` — 单 Request 对象分配测试（5 allocs/op, 440 B/op）
- `BenchmarkMemory_RequestCopy` — Request.Copy() 分配测试
- `BenchmarkMemory_CrawlerCreation` — Crawler 创建分配测试
- 测试结果：10 万请求系统内存 ~151MB，堆使用仅 ~10MB，远低于 500MB 限制

### 性能指标

| 指标 | 结果 | 验收标准 | 状态 |
|------|------|---------|------|
| QPS（16 并发） | ~17000 | >= 5000 | ✅ 通过 |
| 系统内存（10 万请求） | ~151 MB | < 500 MB | ✅ 通过 |
| 堆内存（10 万请求） | ~10 MB | 无泄漏 | ✅ 通过 |
| 每请求分配 | ~12 KB | — | 基线记录 |
| 竞态检测 | 通过 | `go test -race` | ✅ 通过 |

---

## [v0.5.1] - 2026-05-07

> **配置模板完善** — TOML 模板补全 + 移除无实现配置项

### 新增

- `scrapy-go.toml.tmpl` 模板补充完整配置项：重试、重定向、Cookies、HTTP 代理、HTTP 压缩、缓存、统计、断点续爬、深度控制、关闭条件、优雅关闭、数据导出、调试监控等分组

### 移除

- 移除无实现的 DNS 配置项（`DNSCACHE_ENABLED`、`DNSCACHE_SIZE`、`DNS_TIMEOUT`）
  - Go 的 `net/http` 依赖操作系统 DNS 解析器，OS 层已有缓存
  - 项目中无对应的 DNS 缓存中间件实现，避免空壳配置误导用户

---

## [v0.5.0] - 2026-05-07

> **Phase 3 完成 — 高级特性与工程化** — 生产环境就绪

### 新增

#### TOML 配置文件加载（P3-014）

##### Settings.LoadFromFile 方法（P3-014a/b）
- 引入 `github.com/BurntSushi/toml` 依赖
- 新增 `Settings.LoadFromFile(path string) (int, error)` 方法
- 以 `PriorityAddon`(15) 优先级加载外部配置（低于代码中 `PriorityProject`(20)，高于 `PriorityDefault`(0)）
- 支持标量类型：int、bool、string、float、duration（如 "5s"、"1m30s"）
- 支持列表类型：`[]int`、`[]string`
- 支持简单 map 类型：`map[string]string`（适用于 HTTP Header 等）
- TOML 键名自动转换为大写格式（如 `concurrent_requests` → `CONCURRENT_REQUESTS`）
- 不支持从配置文件加载组件优先级字典（Go 静态编译限制）

##### Crawler 自动探测配置文件（P3-014c）
- Crawler 初始化时自动探测配置文件
- 探测优先级：`SCRAPY_GO_CONFIG` 环境变量 → 当前目录 `scrapy-go.toml`
- 环境变量指定的文件必须存在（否则警告），默认文件不存在则静默跳过
- 新增 `Settings.AutoLoadConfig()` 和 `Settings.LoadFromFileIfExists()` 辅助方法

##### 单元测试 + startproject 模板集成（P3-014d）
- 21 个单元测试覆盖所有类型转换、优先级、错误处理场景
- `startproject` 模板已包含带注释的 `scrapy-go.toml` 示例配置
- 测试覆盖率 ≥ 85%

#### Phase 3 集成测试套件（P3-010）
- 新增 `tests/integration/phase3_test.go`，11 个端到端集成测试场景：
  - CrawlSpider 基于规则的自动爬取
  - RobotsTxt 中间件遵守 robots.txt
  - HttpCache 中间件缓存响应（两次爬取验证缓存命中）
  - FormRequestFromResponse 表单自动提取与提交
  - 优雅关闭（Graceful Shutdown）不丢失 in-flight 请求
  - 接口隔离（ISP）中间件（RequestProcessor/ResponseProcessor 独立工作）
  - TypedPipeline 泛型 Pipeline 处理 Item
  - TOML 配置文件加载（环境变量指定路径）
  - Request 序列化与 curl 互操作（ToDict/FromDict/ToCURL/FromCURL 往返）
  - Multipart 文件上传
  - 磁盘队列与断点续爬（JOBDIR 持久化验证）

### 变更

- `go.mod` 新增 `github.com/BurntSushi/toml v1.6.0` 依赖

### 发布信息

- 所有测试通过，`go test ./... -race` 无竞态
- `go vet ./...` 无告警
- 全局覆盖率 ≥ 75%，核心包 ≥ 85%
- v0.5.0 tag 已打

---

## [v0.5.0-beta.2] - 2026-05-06

> **Sprint 10 功能交付** — 并发模型优化 + 接口隔离优化 + 泛型 Pipeline + Item 体系增强

### 新增

#### 接口隔离优化（P3-008）

##### DL Middleware 细粒度接口拆分（P3-008a）
- 将原有的单一 `DownloaderMiddleware` 接口拆分为三个细粒度接口：
  - `RequestProcessor`：仅处理请求（正序调用）
  - `ResponseProcessor`：仅处理响应（逆序调用）
  - `ExceptionProcessor`：仅处理异常（逆序调用）
- 中间件只需实现自己关心的接口，无需为不需要的方法提供空实现
- 遵循 Go 接口隔离原则（ISP），替代 Scrapy 必须实现全部 3 个方法的约束

##### Manager 类型断言适配（P3-008b）
- `MiddlewareManager.AddMiddleware` 接受 `any` 类型参数
- 通过缓存的类型断言结果在运行时适配，跳过未实现对应接口的中间件
- 零额外内存分配（类型断言结果在 AddMiddleware 时一次性缓存）

##### 向后兼容保证（P3-008c）
- 原有的 `DownloaderMiddleware` 接口保留，组合三个细粒度接口
- `BaseDownloaderMiddleware` 结构体保留，所有已有中间件无需修改
- `Crawler.AddDownloaderMiddleware` 接受 `any` 类型，支持注册细粒度中间件
- 全部已有测试通过，无 API 破坏性变更

#### 泛型 Item Pipeline + Item 体系增强（P3-009）

##### TypedPipeline[T] 泛型包装（P3-009a）
- 新增 `TypedItemPipeline[T]` 泛型接口，编译期约束 Item 类型
- 新增 `TypedPipeline[T]` 适配器，将泛型 Pipeline 包装为通用 `ItemPipeline` 接口
- 类型不匹配时自动跳过（透传 Item），允许多个 TypedPipeline 共存
- 替代 Scrapy 运行时 isinstance() 类型检查

##### Pipeline Manager 集成 ItemAdapter（P3-009b）
- `Manager.SetValidateItems(true)` 启用 Item 自动验证
- 在 Pipeline 链处理前自动对 struct Item 执行 Validate（填充默认值 + 校验 required）
- map 类型 Item 自动跳过验证
- 验证失败发出 `item_error` 信号并记录统计

##### struct tag 字段元数据增强（P3-009c）
- 支持 `item:"name,required"` 标记必填字段
- 支持 `item:"name,default=value"` 标记默认值（支持 string/int/float/bool）
- 支持 `item:"name,omitempty"` 标记序列化时忽略零值
- 新增 `item.Validate(item)` 函数：先填充默认值，再校验 required
- 新增 `item.ValidationError` 结构化错误类型，包含所有失败字段信息
- 替代 Scrapy `ItemMeta` 元类 + `Field` 描述符

##### go generate ItemAdapter 代码生成器（P3-009d）
- 新增 `scrapy-go generate-adapter` CLI 命令
- 从带 `item` struct tag 的结构体自动生成 `ItemAdapter` 实现代码
- 生成的代码使用 switch-case 替代反射，消除运行时反射开销
- 支持 `//go:generate scrapy-go generate-adapter -type=Book` 指令
- 生成代码包含完整的 `ItemAdapter` 接口实现 + `FieldMeta` 元数据

#### 并发模型优化（P3-007）

##### errgroup 管理 Engine 多 goroutine（P3-007a）
- Engine 使用 `golang.org/x/sync/errgroup` 统一管理心跳、初始请求消费和 OS 信号监听 goroutine
- 任一 goroutine 出错自动取消 context，替代 Scrapy Twisted reactor 事件循环
- 主调度循环结束后自动通知其他 goroutine 退出

##### sync.Pool 对象池（P3-007b）
- 新增 `pkg/pool` 包，提供 `RequestPool`、`ResponsePool`、`BytesPool` 三种对象池
- 通过 `sync.Pool` 复用 HTTP 请求/响应对象，减少高并发场景下的 GC 压力
- 提供 `Reset()` 方法确保归还对象不泄漏数据
- 作为可选优化，需 Benchmark 验证 GC 为瓶颈时启用

##### semaphore.Weighted 替代 channel 信号量（P3-007c）
- Downloader Slot 的 `transferSem` 从 `chan struct{}` 替换为 `semaphore.Weighted`
- Scraper 的 `itemSem`（CONCURRENT_ITEMS）从 `chan struct{}` 替换为 `semaphore.Weighted`
- 支持 context 取消时中断信号量获取（channel 信号量不支持此特性）

##### pprof 调试端点（P3-007d）
- 新增 `pkg/debug` 包，提供 `PprofExtension` 扩展
- 通过 `PPROF_ENABLED` 配置控制是否启用
- 在指定端口（默认 `:6060`）提供标准 Go pprof 端点
- 支持 CPU profile、堆内存分析、goroutine 堆栈、执行 trace 等
- Go 特有调试手段，Scrapy 无此功能

##### Engine 优雅关闭（P3-007e）
- 两阶段 SIGINT 处理：第一次触发优雅关闭，第二次强制退出
- 优雅关闭流程：停止取新请求 → 等待 in-flight 请求完成 → Pipeline 排空 → 关闭组件
- `GRACEFUL_SHUTDOWN_TIMEOUT` 配置（默认 30s），超时后强制退出并记录未完成请求
- `slot.closing` 后停止取新请求但不丢弃队列中的剩余请求
- 使用 `sync.Once` 确保关闭流程只执行一次

##### DiskQueue sortedPriorities 优化（P3-007f）
- 维护有序 `[]int` 切片替代每次 Pop/Peek 重新分配 + O(N log N) 排序
- Push 新优先级时二分插入 O(log N)
- 删除空桶时二分移除 O(log N)
- Pop 取最高优先级 O(1)
- 偿还技术债务 TD-010

### 新增配置项

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `GRACEFUL_SHUTDOWN_TIMEOUT` | 30 | 优雅关闭超时时间（秒） |
| `PPROF_ENABLED` | false | 是否启用 pprof HTTP 端点 |
| `PPROF_ADDR` | ":6060" | pprof 监听地址 |

### 新增依赖

| 依赖 | 版本 | 用途 |
|------|------|------|
| `golang.org/x/sync` | v0.20.0 | errgroup + semaphore.Weighted |

### 质量

- 全部单元测试通过（`go test ./... -race` 无竞态）
- `go vet ./...` 无告警
- 新增 `pkg/pool` 包含并发安全测试和 Benchmark
- 新增 `pkg/debug` 包含 pprof 端点可访问性测试
- 核心包测试覆盖率：`pkg/item` 91.3%、`pkg/pipeline` 92.1%、`pkg/downloader` 76.8%
- ISP 接口隔离 7 个专项测试用例
- TypedPipeline 6 个测试用例 + Manager 集成 4 个测试用例
- struct tag 验证 12 个测试用例
- generate-adapter 命令 7 个测试用例

### 技术债务偿还

- **TD-008**：Engine 优雅关闭机制 — 两阶段 SIGINT + in-flight 等待 + Pipeline 排空
- **TD-010**：DiskQueue.sortedPriorities 优化 — 维护有序切片替代每次重新排序

---

## [v0.5.0-beta.1] - 2026-04-30

> **Phase 3 公开体验版** — 高级爬虫功能 + 工程化工具全部可用

### 概览

v0.5.0-beta.1 是 scrapy-go 的首个公开体验版，标志着 Phase 3 核心功能全部完成并进入公测阶段。
本版本包含 CrawlSpider 基于规则的自动爬取、RobotsTxt 中间件、磁盘队列与断点续爬、HttpCache 中间件、
项目脚手架工具等全部高级特性，适合开发者提前试用并反馈。

### 包含功能（自 v0.4.0 以来）

#### HttpCache 中间件（P3-005）
- 可插拔的 `CacheStorage` + `CachePolicy` 接口设计
- `FilesystemCacheStorage` — JSON 元数据 + gzip 压缩 + 原子写入
- `DummyPolicy`（无条件缓存）+ `RFC2616Policy`（HTTP 缓存语义）
- 9 种统计指标 + `dont_cache` Meta + 下载异常错误恢复

#### 项目脚手架工具（P3-006）
- `scrapy-go startproject <name>` — 创建完整项目骨架
- `scrapy-go genspider <name> <domain>` — 生成爬虫文件（basic/crawl 模板）
- `scrapy-go version [-v]` — 版本信息
- 零外部依赖，`go:embed` + `text/template` 实现
- 强制项目检测（`scrapy-go.toml`）+ 项目级 settings 模板

### 质量

- 全部测试通过（`go test ./... -race` 无竞态）
- `go vet ./...` 无告警
- 核心包覆盖率均 ≥80%

### 已知限制

- 并发优化（Phase 3 剩余任务）尚未完成，将在 v0.5.0 正式版中交付
- 本版本为体验版，API 可能在正式版发布前有微调

---

## [v0.5.0-alpha.5] - 2026-04-30

> **Phase 3 Sprint 9** — 爬虫模板增强

### 变更

#### 爬虫模板增强

- **新增包注释** — basic/crawl 模板顶部添加 `// Package spiders` 注释，符合 Go 文档规范
- **新增 `CustomSettings()` 方法** — 支持 Spider 级别的配置覆盖，默认返回 nil（不覆盖任何配置）

### 质量

- 无逻辑变更，仅模板内容增强

---

## [v0.5.0-alpha.4] - 2026-04-30

> **Phase 3 Sprint 9** — genspider 项目检测强制化 + settings 模板升级

### 概览

v0.5.0-alpha.4 强制要求 `genspider` 命令必须在 scrapy-go 项目中执行（检测 `scrapy-go.toml`），
移除 standalone 模式，同时将 `settings.go` 模板从爬虫级配置（`spider.Settings`）升级为
项目级配置（`settings.Settings` + `PriorityProject`），使配置体系更符合 Scrapy 的多层级设计。

### 变更

#### genspider 项目检测强制化

- **必须在项目中执行** — 检测当前目录是否存在 `scrapy-go.toml`，不存在则直接报错退出
- **移除 standalone 模式** — 删除 `templates/spiders/standalone/` 目录及相关逻辑
- **爬虫文件固定输出到 `spiders/`** — 使用 `package spiders`，无 `func main()`
- **错误提示清晰** — `"当前目录不是 scrapy-go 项目（未找到 scrapy-go.toml），请在项目根目录下执行此命令"`

#### settings 模板升级为项目级配置

- **配置系统切换** — 从 `spider.Settings`（结构体指针字段）改为 `settings.New()` + `s.Set(key, value, priority)` 模式
- **使用 `PriorityProject` 优先级** — 正确对齐 Scrapy 的 default < project < spider < cmdline 优先级链
- **配置项丰富化** — 包含 BOT_NAME / CONCURRENT_REQUESTS / CONCURRENT_REQUESTS_PER_DOMAIN / ROBOTSTXT_OBEY / LOG_LEVEL 等项目级配置
- **注释引导** — 提供 DOWNLOAD_TIMEOUT / RETRY / DOWNLOADER_MIDDLEWARES / ITEM_PIPELINES 等配置的注释模板

#### main.go 模板更新

- **显式使用项目配置** — `project.NewProjectSettings()` 获取配置实例
- **Crawler 创建方式** — `crawler.New(crawler.WithSettings(projectSettings))`
- **信号处理** — 内置 `signal.NotifyContext` 支持优雅关闭

### 质量

- 全部 **46** 个测试通过（cmd/scrapy-go 包）
- `go test -race` 无竞态报告
- `go vet` 无告警
- cmd/scrapy-go 包覆盖率 **81.3%**（目标 ≥ 80%）

### 依赖

- 无新增外部依赖

---

## [v0.5.0-alpha.3] - 2026-04-30

> **Phase 3 Sprint 9** — 脚手架项目结构重构

### 概览

v0.5.0-alpha.3 将脚手架生成的项目组件文件（settings.go / middlewares.go / pipelines.go / items.go）
从 `package main`（根目录）分离到 `package project`（`project/` 子目录），使生成的项目结构更符合
Go 多包组织规范，职责分离更清晰。

### 变更

#### 脚手架项目结构重构

- **组件文件迁移到 `project/` 子包** — `settings.go`、`middlewares.go`、`pipelines.go`、`items.go` 的包声明从 `package main` 改为 `package project`，输出路径从项目根目录改为 `project/` 子目录
- **`main.go` 模板更新** — 新增 `_ "{{.ModulePath}}/project"` 空白导入，确保项目级组件包可达
- **注释示例更新** — 中间件和 Pipeline 注册示例中的类型引用添加 `project.` 包前缀（如 `&project.MyPipeline{}`）
- **`startproject.go` 逻辑更新** — 新增 `project/` 子目录创建，模板输出路径同步调整
- **帮助信息更新** — `startproject -h` 输出的项目结构树同步更新

#### 生成的项目结构（变更后）

```
<project_dir>/
├── main.go              入口文件（package main）
├── project/             项目级组件（package project）
│   ├── settings.go      项目配置
│   ├── middlewares.go   自定义中间件模板
│   ├── pipelines.go     自定义 Pipeline 模板
│   └── items.go         Item 定义
├── spiders/             爬虫目录
│   └── .gitkeep
├── go.mod               Go 模块文件
└── scrapy-go.toml       框架配置文件
```

### 质量

- 全部 **42** 个测试通过（cmd/scrapy-go 包）
- `go test -race` 无竞态报告
- `go vet` 无告警
- cmd/scrapy-go 包覆盖率 **81.0%**（目标 ≥ 80%）

### 依赖

- 无新增外部依赖

---

## [v0.5.0-alpha.2] - 2026-04-30

> **Phase 3 Sprint 9** — 项目脚手架工具

### 概览

v0.5.0-alpha.2 实现了项目脚手架工具（`cmd/scrapy-go`），提供 `startproject`、`genspider`、`version` 三个命令，
使用标准库 `os.Args` + `go:embed` + `text/template` 实现，保持零外部依赖。

### 新增

#### 项目脚手架工具（P3-006）

- **`scrapy-go startproject <name> [dir]`** — 创建新的 scrapy-go 项目骨架
  - 使用 `go:embed` 嵌入模板 + `text/template` 渲染
  - 生成 `main.go` / `settings.go` / `middlewares.go` / `pipelines.go` / `items.go` / `go.mod` / `scrapy-go.toml` 等项目文件
  - 自动创建 `spiders/` 目录
  - 项目名称验证（必须以字母或下划线开头，只能包含字母、数字和下划线）
  - 自动检测已有 `go.mod` 防止覆盖
  - `scrapy-go.toml` 包含带注释的常用配置项模板（并发/超时/重试/缓存/日志等）
- **`scrapy-go genspider <name> <domain>`** — 使用模板生成新的爬虫文件
  - 支持 `-t basic`（默认）和 `-t crawl` 两种模板
  - 支持 `-l` 列出可用模板
  - 域名自动提取与 URL scheme 补全（无 scheme 时自动添加 `https://`）
  - 爬虫名称自动转换为 Go 大驼峰类型名（如 `quotes` → `QuotesSpider`）
  - 文件名安全处理（连字符和点替换为下划线）
  - 检测文件已存在防止覆盖
- **`scrapy-go version [-v]`** — 打印版本信息
  - `-v` 显示详细信息（Go 版本、操作系统、架构）
- **子命令分发框架** — 使用标准库 `os.Args` 手动分发（保持零外部依赖）
  - 支持 `-h` / `--help` 帮助信息
  - 兼容 `go generate` 调用（为 Sprint 10 的 `generate-adapter` 子命令预留入口）

### 设计决策

| 决策 | 说明 |
|------|------|
| 零外部依赖 | 使用标准库 `os.Args` 替代 cobra/urfave/cli，保持最小依赖 |
| 舍弃 `crawl`/`list`/`settings` 命令 | Go 静态编译语言无法动态加载 Spider，这些命令在 Go 中无意义 |
| `go:embed` 模板 | 编译期嵌入模板文件，无需运行时文件系统访问 |
| 通过 `go.mod` 检测项目 | 替代 Scrapy 的 `scrapy.cfg` 项目检测机制 |
| 驼峰命名自动转换 | 遵循 Go 命名规范，`my_spider` → `MySpider` |

### 质量

- 新增 **42** 个测试（cmd/scrapy-go 包）
- `go test -race` 无竞态报告
- `go vet` 无告警
- cmd/scrapy-go 包覆盖率 **82.1%**（目标 ≥ 80%）

### 依赖

- 无新增外部依赖（纯标准库实现）

---

## [v0.5.0-alpha.1] - 2026-04-30

> **Phase 3 Sprint 9** — HttpCache 中间件实现

### 概览

v0.5.0-alpha.1 实现了 HttpCache 中间件，提供可插拔的缓存存储后端和缓存策略接口，
支持文件系统缓存存储和两种缓存策略（DummyPolicy 无条件缓存、RFC2616Policy HTTP 缓存语义）。

### 新增

#### HttpCache 中间件（P3-005）

- **`CacheStorage` 接口** — 缓存存储后端抽象（`pkg/downloader/middleware/httpcache/interface.go`）
  - `Open(spiderName)` / `Close()` 生命周期管理
  - `RetrieveResponse(request)` / `StoreResponse(request, response)` 缓存操作
- **`CachePolicy` 接口** — 缓存策略抽象
  - `ShouldCacheRequest` / `ShouldCacheResponse` 缓存决策
  - `IsCachedResponseFresh` / `IsCachedResponseValid` 新鲜度和有效性判断
- **`FilesystemCacheStorage`** — 基于文件系统的缓存存储后端
  - JSON 元数据格式（替代 Scrapy 的 pickle，更安全且跨平台）
  - 支持 gzip 压缩存储（`HTTPCACHE_GZIP` 配置）
  - 原子写入（临时文件 + `os.Rename`）
  - 按请求指纹分桶目录结构：`{cacheDir}/{spiderName}/{fp[0:2]}/{fp}/`
  - 支持过期时间（`HTTPCACHE_EXPIRATION_SECS`）
- **`DummyPolicy`** — 无条件缓存策略
  - 排除指定 scheme（`HTTPCACHE_IGNORE_SCHEMES`，默认 `["file"]`）
  - 排除指定 HTTP 状态码（`HTTPCACHE_IGNORE_HTTP_CODES`）
  - 缓存永不过期，始终使用缓存响应
- **`RFC2616Policy`** — HTTP 缓存语义策略（实验性）
  - Cache-Control 指令完整支持（max-age、no-cache、no-store、must-revalidate、max-stale）
  - Expires 头解析
  - ETag / If-None-Match 条件验证
  - Last-Modified / If-Modified-Since 条件验证
  - 304 Not Modified 响应处理
  - 启发式新鲜度计算（Last-Modified 回退）
  - 永久重定向（300/301/308）无限期缓存
  - `HTTPCACHE_ALWAYS_STORE` 无条件存储模式
  - `HTTPCACHE_IGNORE_RESPONSE_CACHE_CONTROLS` 忽略指定响应 Cache-Control 指令
- **`HttpCacheMiddleware`** — HTTP 缓存中间件，注册优先级 900
  - ProcessRequest：缓存查找 + 命中短路
  - ProcessResponse：缓存存储 + 条件验证
  - ProcessException：下载异常时使用缓存恢复
  - 9 种统计指标（hit/miss/store/firsthand/revalidate/invalidate/uncacheable/errorrecovery/ignore）
  - `dont_cache` Meta 跳过缓存
  - `HTTPCACHE_IGNORE_MISSING` 模式（缓存未命中时忽略请求）
  - 通过 Spider 信号（SpiderOpened/SpiderClosed）管理存储生命周期

### 新增配置项

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `HTTPCACHE_ENABLED` | `false` | 是否启用 HTTP 缓存 |
| `HTTPCACHE_DIR` | `"httpcache"` | 缓存目录 |
| `HTTPCACHE_EXPIRATION_SECS` | `0` | 缓存过期时间（秒），0 不过期 |
| `HTTPCACHE_GZIP` | `false` | 是否使用 gzip 压缩存储 |
| `HTTPCACHE_IGNORE_HTTP_CODES` | `[]` | 不缓存的 HTTP 状态码 |
| `HTTPCACHE_IGNORE_SCHEMES` | `["file"]` | 不缓存的 URL scheme |
| `HTTPCACHE_IGNORE_MISSING` | `false` | 缓存未命中时是否忽略请求 |
| `HTTPCACHE_POLICY` | `"dummy"` | 缓存策略（"dummy" 或 "rfc2616"） |
| `HTTPCACHE_ALWAYS_STORE` | `false` | RFC2616 策略下是否始终存储 |
| `HTTPCACHE_IGNORE_RESPONSE_CACHE_CONTROLS` | `[]` | RFC2616 策略下忽略的 Cache-Control 指令 |

### 质量

- 新增 **72** 个测试（httpcache 包）
- `go test ./... -race` 无竞态报告
- `go vet ./...` 无告警
- httpcache 包覆盖率 **88.9%**（目标 ≥ 85%）

### 依赖

- 无新增外部依赖（全部功能仅使用 Go 标准库）

---

## [v0.4.0] - 2026-04-29

> **Phase 3 Sprint 8 正式发布** — CrawlSpider、RobotsTxt、磁盘队列与断点续爬、Request 序列化

### 概览

v0.4.0 是 scrapy-go 的 Phase 3 Sprint 8 里程碑版本，实现了 CrawlSpider 基于规则的自动爬取、
RobotsTxt 中间件、FormRequest 系列增强、Request 序列化与 curl 互操作、磁盘队列与断点续爬等核心功能。
本版本包含 6 个 alpha 预发布版本的全部变更。

### 新增

#### CrawlSpider 基于规则的自动爬取（P3-001）

- **`HTMLLinkExtractor`** — 基于 goquery 的链接提取器（`pkg/linkextractor/`）
  - 支持 `allow`/`deny` 正则过滤、`allowDomains`/`denyDomains` 域名过滤
  - `restrictCSS`/`restrictXPath` 限定提取范围
  - `canonicalize`/`unique`/`stripFragment` 链接规范化
- **`Rule` 结构体** — Callback/Errback 直接接受函数值（舍弃 Scrapy 字符串方法名反射）
- **`CrawlSpider`** — 基于规则的自动爬取 Spider（`pkg/spider/crawlspider.go`）
  - `parseWithRules` 同步返回 `[]Output`（舍弃 AsyncIterator）
  - 支持 `Follow` 自动跟踪、`ProcessLinks`/`ProcessResults` 钩子

#### RobotsTxt 中间件（P3-002）

- **`RobotsTxtMiddleware`** — robots.txt 遵守中间件，注册优先级 100
  - 内置 robots.txt 解析器（自实现，替代外部依赖）
  - 按 netloc 缓存解析结果（`sync.Once` + `sync.WaitGroup` 替代 Twisted Deferred）
  - 支持通配符 `*` 和 `$` 锚定，最长匹配原则
  - 支持 `ROBOTSTXT_OBEY`/`ROBOTSTXT_USER_AGENT` 配置 + `dont_obey_robotstxt` Meta

#### FormRequest 系列增强（P3-012）

- **`FormRequestFromResponse`** — 从 HTML `<form>` 自动提取 action/method/inputs
  - 5 种表单定位：`WithFormName`/`WithFormID`/`WithFormNumber`/`WithFormXPath`/`WithFormCSS`
  - `WithClickButton` 简化 clickdata（舍弃 Scrapy 坐标点击）
- **`NewMultipartFormRequest`** — multipart/form-data 文件上传
  - 基于 `mime/multipart` 标准库，支持多文件上传、MIME 类型自动推断

#### Request 序列化与 curl 互操作（P3-013）

- **`Request.ToDict` / `FromDict`** — Request 序列化与反序列化（对齐 Scrapy `to_dict`/`request_from_dict`）
  - Body base64 编码、Cookie 完整属性保留、不可序列化 Meta 自动跳过
- **`CallbackRegistry`** — 回调函数注册表
  - `RegisterSpider(spider)` — 通过 reflect 自动扫描注册 Callback/Errback（推荐方式）
  - `FromDict` 回调恢复严格模式 — registry 非空时找不到回调名称直接返回 error
- **`FromCURL` / `ToCURL`** — curl 命令与 Request 互转，自实现 shell 词法分析器

#### 磁盘队列与断点续爬（P3-003）

- **`Queue` 接口** — 统一内存/磁盘队列抽象（`pkg/scheduler/queue.go`）
  - 操作 `[]byte`，序列化职责上移到调度器层（替代 Scrapy `queuelib` 直接存储 Python 对象）
  - `MemoryQueue` 内置内存 LIFO 队列实现
- **`PriorityAwareQueue` 接口** — 优先级感知队列扩展，支持 Redis 等外部队列无缝接入
- **`DiskQueue` 基于文件系统的持久化队列** — `pkg/scheduler/diskqueue.go`
  - JSON 格式存储（替代 Scrapy pickle，更安全且跨平台）
  - 按优先级分桶多文件方案，`os.Rename` 原子写入
  - **每次 Push/Pop 立即写盘**，进程异常退出时数据零丢失（对齐 Scrapy `LifoDiskQueue` 行为）
  - 写盘失败时自动回滚内存状态，保证一致性
- **`RequestSerializer`** — 基于 `ToDict`/`FromDict` + `encoding/json` 的请求序列化器
- **`DefaultScheduler` 扩展** — `WithJobDir` 启用磁盘队列；入队磁盘优先、出队内存优先
- **`RFPDupeFilter` 持久化** — `requests.seen` 文件持久化指纹集合，原子写入
- **`Crawler` 集成 JOBDIR** — 自动创建 `CallbackRegistry` + `PersistentRFPDupeFilter`
- **断点续爬完整流程** — 中断后重启从断点继续，不重复爬取已完成 URL

### 质量

- 全部 **873** 个测试通过
- `go test ./... -race` 无竞态报告
- `go vet ./...` 无告警
- 核心包覆盖率（均 ≥82%）：
  - `selector` 98.0%、`item` 94.9%、`http` 91.8%、`scraper` 91.6%
  - `pipeline` 90.2%、`extension` 89.3%、`spider/middleware` 88.0%
  - `linkextractor` 87.3%、`downloader/middleware` 87.4%、`errors` 87.0%
  - `spider` 85.6%、`feedexport` 85.6%、`signal` 85.4%
  - `scheduler` 82.6%

### 依赖

- 无新增外部依赖（Phase 3 Sprint 8 全部功能仅使用 Go 标准库）

---

<details>
<summary>📦 v0.4.0 预发布版本历史（alpha.1 ~ alpha.6）</summary>

## [v0.4.0-alpha.6] - 2026-04-29

### 改进

#### DiskQueue 即时写盘优化

- **每次 Push/Pop 操作立即写盘** — 替代之前的延迟写入（dirty 标记 + Close 时批量 flush）模式
  - `PushWithPriority` 写入后立即调用 `persistBucketAndState()` 持久化桶数据和状态文件
  - `PopWithPriority` 弹出后立即调用 `persistAfterPop()` 持久化变更，空桶自动删除桶文件
  - 写盘失败时自动回滚内存状态（items + count + buckets map），保证内存与磁盘一致性
- **移除 `dirty` 标记** — `bucket` 结构体不再需要 `dirty bool` 字段
- **移除 `Flush()` 公开方法** — 不再需要手动 flush，每次操作已保证数据安全
- **简化 `Close()` 方法** — 仅做残留空桶文件清理，不再需要批量写盘
- **新增内部方法** — `persistBucketAndState()`、`persistAfterPop()`、`writeCurrentState()` 替代原有 `flush()`
- **新增测试** — `TestDiskQueueImmediatePersist`（模拟崩溃恢复）、`TestDiskQueueImmediatePersistAfterPop`

---

## [v0.4.0-alpha.5] - 2026-04-29

### 新增

#### 磁盘队列与断点续爬（Phase 3 Sprint 8 — P3-003）

- **`Queue` 接口** — 统一的队列抽象（`pkg/scheduler/queue.go`）
- **`DiskQueue` 基于文件系统的持久化队列** — JSON + 按优先级分桶多文件方案
- **`RequestSerializer` 请求序列化器** — 基于 `ToDict`/`FromDict` + `encoding/json`
- **`DefaultScheduler` 扩展支持磁盘队列** — `WithJobDir` 启用断点续爬
- **`RFPDupeFilter` 持久化支持** — `requests.seen` 文件持久化指纹集合
- **`Crawler` 集成 JOBDIR** — 自动创建 `CallbackRegistry` + `PersistentRFPDupeFilter`
- **断点续爬完整流程** — 中断后重启从断点继续，不重复爬取已完成 URL

## [v0.4.0-alpha.4] - 2026-04-29

### 新增

#### Request 序列化与 curl 互操作（Sprint 8 / P3-013）

- **`Request.ToDict(callbackName, errbackName) map[string]any`** — 将 Request 转换为可序列化的字典（对齐 Scrapy `Request.to_dict()`）
  - 所有核心字段序列化：URL/Method/Headers/Body/Cookies/Meta/Priority/DontFilter/Flags/CbKwargs/Encoding
  - Body 使用 base64 编码以支持二进制内容
  - Cookies 转换为 `[]map[string]any`，保留 Name/Value/Domain/Path/Secure/HttpOnly
  - Meta 中不可 JSON 序列化的值（函数、channel 等）自动跳过
  - Callback/Errback 通过字符串名称存储（支撑磁盘队列跨进程恢复）
  - 默认值字段不写入字典（零值 Priority、默认 utf-8 Encoding 等）

- **`FromDict(d map[string]any, registry *CallbackRegistry) (*Request, error)`** — 从字典创建 Request（对齐 Scrapy `request_from_dict()`）
  - 支持 JSON 反序列化后的类型（`float64` → `int` 优先级、`[]any` → `[]string` Flags 等）
  - 支持 `map[string]any` 和 `map[string][]string` 两种 Headers 格式
  - 支持 `[]any` 和 `[]map[string]any` 两种 Cookies 格式
  - Body 自动 base64 解码
  - 通过 `CallbackRegistry` 注册表恢复 Callback/Errback 函数引用（registry 可为 nil）
  - 当 registry 非空但找不到对应回调名称时，直接返回明确错误（包含已注册名称列表，便于排查）

- **`CallbackRegistry`** — 回调函数注册表（替代 Scrapy `getattr(spider, method_name)` 反射）
  - `RegisterSpider(spider any)` — **通过 Go reflect 自动扫描** Spider 实例上所有符合 Callback/Errback 签名的导出方法并注册（推荐方式，用户无需手动注册）
  - Callback 签名匹配：`func(context.Context, *Response) ([]T, error)`
  - Errback 签名匹配：`func(context.Context, error, *Request) ([]T, error)`
  - 方法命名规范：遵循 Go PascalCase 导出命名（如 `ParseDetail`、`HandleError`），方法名即为注册表键
  - `Register(name, cb)` / `RegisterErrback(name, eb)` — 手动注册回调/错误回调
  - `Lookup(name)` / `LookupErrback(name)` — 按名称查找
  - `MustLookup(name)` / `MustLookupErrback(name)` — 查找失败时 panic
  - `Names()` / `ErrbackNames()` — 列出所有已注册名称
  - `sync.RWMutex` 保护并发安全
  - 单元测试覆盖率 100%

- **`Request.ToCURL() string`** — 将 Request 转换为 curl 命令字符串（对齐 Scrapy `request_to_curl()`）
  - 输出包含 Method/URL/Headers/Cookies/Body
  - 使用单引号 shell 引用（正确处理特殊字符）

- **`FromCURL(curlCommand string, opts...) (*Request, error)`** — 从 curl 命令创建 Request（对齐 Scrapy `Request.from_curl()`）
  - 自实现轻量级 shell 词法分析器（替代 Scrapy `shlex.split` + `argparse`）
  - 支持单引号、双引号和反斜杠转义
  - 支持 `-X`/`--request`（方法）、`-H`/`--header`（请求头）、`-b`/`--cookie`（Cookie）、`-d`/`--data`/`--data-raw`（请求体）、`-u`/`--user`（Basic Auth）、`-A`/`--user-agent`（UA）
  - Cookie Header 中的值自动解析为独立 Cookie
  - 有 `--data` 但无 `-X` 时默认 POST（对齐 curl 行为）
  - URL 缺少 scheme 时自动添加 `http://`
  - `--compressed`/`-s`/`-v`/`-k`/`-L` 等安全忽略（对齐 Scrapy `ignore_unknown_options=True`）
  - 用户 `RequestOption` 可覆盖 curl 解析出的值
  - 单元测试覆盖率 89.2%~94.1%

- **`ToDict` → JSON → `FromDict` 往返测试** — 验证序列化/反序列化循环不丢失字段
- **`ToCURL` → `FromCURL` 往返测试** — 验证 curl 命令互转正确性
- **pkg/http 整体覆盖率 91.8%**，全部测试通过，`go test -race` 无竞态

### 质量

- `go test ./pkg/http/... -race`：全部通过
- `go vet ./...`：零告警

---

## [v0.4.0-alpha.3] - 2026-04-29

### 新增

#### FormRequestFromResponse 与 Multipart 文件上传（Sprint 7 / P3-012）

- **`FormRequestFromResponse(resp, opts...)`** — 从 HTTP 响应中自动提取 HTML `<form>` 信息并创建表单请求（对齐 Scrapy `FormRequest.from_response()`）
  - 自动提取 `<form>` 的 action（URL）、method（HTTP 方法）
  - 自动收集表单内所有 `<input>`、`<select>`、`<textarea>` 的 name/value
  - 支持 `<input type="checkbox/radio">` 的 checked 状态检测
  - 支持 `<select>` 的 `selected` 选项提取（无选中项时使用第一个 `<option>`，对齐浏览器行为）
  - 自动跳过 `submit`/`image`/`reset` 类型的 input（不作为普通字段提取）
  - 默认自动包含第一个提交按钮的 name/value（`WithDontClick()` 禁用）
  - 支持 `WithClickButton(map[string]string{...})` 指定点击的提交按钮（舍弃 Scrapy 的坐标点击 `nr` 参数）
  - 用户通过 `WithFormResponseData()` 提供的字段覆盖 HTML 中提取的同名字段
  - 支持相对 URL 和绝对 URL 的 action 解析
  - 无 action 属性时使用当前页面 URL（对齐 HTML 规范）
  - 无 method 属性时默认 GET（对齐 HTML 规范）
  - 无效 method（非 GET/POST）回退为 GET（对齐 Scrapy）
  - 通过 `WithRequestOptions()` 传递底层 Request 选项（Callback/Meta/Priority 等）
  - 单元测试覆盖率 96.4%

- **表单定位选项**（`FormOption`）— 支持 5 种表单定位方式
  - `WithFormName(name)` — 按 `<form name="...">` 属性定位
  - `WithFormID(id)` — 按 `<form id="...">` 属性定位
  - `WithFormNumber(n)` — 按表单在页面中的出现顺序定位（从 0 开始，默认 0）
  - `WithFormXPath(xpath)` — 按 XPath 表达式定位（支持 ancestor 轴向上查找 `<form>` 祖先）
  - `WithFormCSS(css)` — 按 CSS 选择器定位（支持匹配表单内部元素后向上查找 `<form>`）
  - 定位优先级：formname > formid > formcss > formxpath > formnumber

- **`NewMultipartFormRequest(url, fields, files, opts...)`** — multipart/form-data 文件上传请求构造器
  - 基于 Go 标准库 `mime/multipart` 实现
  - `FormField` — 普通文本字段（Name/Value）
  - `FormFile` — 文件字段（FieldName/FileName/Content/ContentType）
  - 支持多文件上传（同一 FieldName 多个文件）
  - 支持自定义文件 Content-Type（`FormFile.ContentType`）
  - 内置 MIME 类型自动推断（30+ 种常见扩展名映射）
  - 默认 POST 方法，自动设置 `Content-Type: multipart/form-data; boundary=...`
  - `MustNewMultipartFormRequest` — panic 版本（用于确定参数有效的场景）
  - 单元测试覆盖率 92.0%

### 质量

- `go test ./pkg/http/... -race`：全部通过
- `go vet ./...`：零告警

---

## [v0.4.0-alpha.2] - 2026-04-29

### 新增

#### RobotsTxt 中间件（Sprint 7 / P3-002）

- **`RobotsTxtMiddleware`** — robots.txt 遵守中间件，注册优先级 100
  - 按 netloc（`scheme://host:port`）缓存 robots.txt 解析结果
  - 使用 `sync.Once` + `sync.WaitGroup` 确保每个 netloc 只下载一次 robots.txt（替代 Scrapy Twisted Deferred）
  - 被 robots.txt 禁止的请求返回 `ErrIgnoreRequest`
  - 支持 `ROBOTSTXT_OBEY` 配置开关（默认 false）
  - 支持 `ROBOTSTXT_USER_AGENT` 配置自定义匹配 User-Agent
  - 支持 `Request.Meta["dont_obey_robotstxt"]` 跳过单个请求的检查
  - 自动跳过 `data:` 和 `file:` 协议的请求
  - 内置 robots.txt 解析器，支持 `User-agent`/`Disallow`/`Allow` 指令
  - 支持通配符 `*` 和结尾锚定 `$` 的路径匹配
  - 最长匹配原则：`Allow` 规则长度 ≥ `Disallow` 规则时优先允许
  - 统计指标：`robotstxt/request_count`、`robotstxt/response_count`、`robotstxt/response_status_count/{status}`、`robotstxt/forbidden`、`robotstxt/exception_count`
  - Functional Options 模式配置（`WithRobotsTxtUserAgent`/`WithRobotsTxtDefaultUserAgent`/`WithRobotsTxtHTTPClient`）
  - 单元测试覆盖率 92.6%

- **新增配置项** — `ROBOTSTXT_OBEY`（默认 false）、`ROBOTSTXT_USER_AGENT`（默认 ""）
- **`DOWNLOADER_MIDDLEWARES_BASE`** — 新增 `RobotsTxt: 100`

### 质量

- `go test ./pkg/downloader/... -race`：全部通过
- `go vet ./...`：零告警

---

## [v0.4.0-alpha.1] - 2026-04-29

### 新增

#### CrawlSpider 基于规则的自动爬取（Sprint 7 / P3-001）

- **`pkg/linkextractor` 包** — 链接提取器接口与实现
  - `LinkExtractor` 接口 — 定义 `ExtractLinks(response) []Link` 契约
  - `Link` 数据模型 — URL/Text/Fragment/NoFollow 四字段
  - `HTMLLinkExtractor` 基于 goquery 的链接提取器（对应 Scrapy `LxmlLinkExtractor`，Go 中重命名）
  - 支持 `allow`/`deny` 正则过滤（`WithAllow`/`WithDeny`）
  - 支持 `allowDomains`/`denyDomains` 域名过滤（含子域名匹配）
  - 支持 `restrictCSS`/`restrictXPath` 限制提取范围
  - 支持 `restrictText` 锚文本正则过滤
  - 支持自定义标签/属性扫描（`WithTags`/`WithAttrs`，默认 `a`/`area` + `href`）
  - 支持 URL 去重（`WithUnique`，默认 true）
  - 支持 Fragment 去除（`WithStripFragment`，默认 true）
  - 支持文件扩展名过滤（`WithDenyExtensions`，默认过滤 90+ 种非网页扩展名）
  - 支持 `<base>` 标签解析
  - 支持 `rel="nofollow"` 检测
  - 全部使用 Functional Options 模式配置
  - `Matches(url)` 方法 — 快速判断 URL 是否匹配过滤规则

- **`Rule` 结构体** — CrawlSpider 爬取规则定义
  - `LinkExtractor` — 链接提取器（nil 时使用默认 HTMLLinkExtractor）
  - `Callback` — 直接接受函数值（舍弃 Scrapy 字符串方法名反射）
  - `Errback` — 错误回调函数
  - `CbKwargs` — 传递给回调的额外参数
  - `Follow` — 是否跟踪链接（nil 时：无 Callback 默认 true，有 Callback 默认 false）
  - `ProcessLinks` — 链接后处理钩子
  - `ProcessRequest` — 请求后处理钩子（返回 nil 丢弃请求）

- **`CrawlSpider` 结构体** — 基于规则的自动爬取 Spider
  - 嵌入 `spider.Base`，实现 `spider.Spider` 接口
  - `Rules` — 多规则列表，按顺序匹配，同一链接只被第一个匹配规则处理（跨规则去重）
  - `FollowLinks` — 全局链接跟踪开关（对应 Scrapy `CRAWLSPIDER_FOLLOW_LINKS`）
  - `ParseStartURL` — 初始 URL 响应回调（对应 Scrapy `parse_start_url`）
  - `ProcessResults` — 回调结果后处理钩子（对应 Scrapy `process_results`）
  - 内部回调机制 — 通过 `Request.Meta["rule"]` 索引规则，`ruleCallback`/`ruleErrback` 分发
  - 非 HTML 响应自动跳过链接提取（检查 Content-Type）
  - `sync.Once` 确保规则只编译一次

- **新增配置项** — `CRAWLSPIDER_FOLLOW_LINKS`（默认 true）

- **新增示例** — `examples/crawlspider/main.go`
  - 本地测试网站模拟多层级站点（首页 → 分类 → 文章）
  - 两条规则演示：跟踪分类页面（无回调）+ 提取文章数据（有回调）
  - CSS 选择器解析文章标题/作者/内容

### 文档

- **完善 Feed Export 示例** — 重写 `examples/feedexport/main.go`，覆盖 `pkg/feedexport` 全部核心 API
  - 四种内置格式（JSON/JSONL/CSV/XML）的直接使用
  - `ExporterOptions` 全部选项（Indent/FieldsToExport/IncludeHeadersLine/JoinMultivalued/ItemElement/RootElement）
  - `RegisterExporter` 自定义 Markdown 表格格式
  - `RegisterSerializer` / `LookupSerializer` / `SerializeField` 序列化器注册表
  - `URIParams` / `NewURIParams` / `Render` URI 模板占位符
  - `NewStorageForURI` / `NewFileStorage` / `NewStdoutStorage` 存储后端
  - `FeedSlot` 直接使用（NewFeedSlot/Start/ExportItem/Close/ItemCount/URI）
  - `FeedConfig` 全部选项（Filter/StoreEmpty/Overwrite/Storage）
  - `Crawler.AddFeed` 端到端集成（struct Item + 4 种格式同时导出）
- **新增 ItemAdapter 示例** — 新增 `examples/itemadapter/main.go`，覆盖 `pkg/item` 全部核心 API
  - `IsItem` 类型判断（可适配/不可适配类型）
  - `MapAdapter`（`map[string]any` / `map[string]string`）
  - `StructAdapter` 可写模式（`*struct`）和只读模式（struct 值）
  - struct tag 解析（`item` tag / `json` tag fallback）
  - `FieldMeta`（Get/GetString/Clone/nil 安全操作）
  - 顶层便捷函数（Adapt/MustAdapt/AsMap/FieldNames）
  - 自定义 `ItemAdapter` 接口实现（OrderedItem）
  - `Register` 自定义 `AdapterFactory`（CSVRow 适配）

### 质量

- `go test ./... -race`：全部通过
- `go vet ./...`：零告警

</details>

---

## [v0.3.0] - 2026-04-28

> **Phase 2 正式发布** — 扩展体系与数据导出

### 概览

v0.3.0 是 scrapy-go 的 Phase 2 里程碑版本，建立了完整的扩展机制、数据导出能力、
多爬虫调度能力和 Request 便捷 API。本版本包含 10 个 alpha 预发布版本的全部变更。

### 新增

#### 扩展系统（Extension System）

- **Extension 接口与 Manager** — 定义 `Extension` 接口（`Open`/`Close` 生命周期），
  `ExtensionManager` 支持优先级排序、`ErrNotConfigured` 自动跳过、逆序关闭
- **4 个内置扩展**：
  - `CoreStats` — 收集核心统计（start_time/finish_time/item_count/response_count）
  - `CloseSpider` — 条件自动关闭（超时/Item 数/页面数/错误数）
  - `LogStats` — 定期输出 RPM/IPM 统计摘要
  - `MemoryUsage` — Go runtime 内存监控（限制/警告）
- **Crawler 集成** — Extension 系统集成到 Crawler 组件编排流程，支持 `EXTENSIONS_BASE`/`EXTENSIONS` 配置

#### Feed Export 数据导出

- **四种内置格式** — JSON、JSON Lines、CSV、XML
- **两种存储后端** — 本地文件（`FileStorage`）、标准输出（`StdoutStorage`）
- **URI 模板占位符** — `%(name)s` / `%(time)s` / `%(batch_id)d` / `%(batch_time)s`
- **FeedExportExtension** — 通过信号系统接入 Spider 生命周期
- **Crawler.AddFeed** — 代码注入 API + `Settings.FEEDS` 配置驱动
- **FieldMeta 驱动序列化** — `RegisterSerializer` 注册表机制，Exporter 根据 `FieldMeta` 自动调用序列化函数

#### Item 体系与 ItemAdapter

- **`pkg/item` 包** — 统一的 Item 访问抽象
  - `ItemAdapter` 接口 + `MapAdapter` / `StructAdapter` 内置实现
  - `Adapt(item)` 自动检测 + `Register(factory)` 自定义工厂注册
  - `FieldMeta` 字段元数据（从 struct tag 自动解析）
- **Feed Export 集成** — 所有 Exporter 通过 `ItemAdapter` 统一读取字段

#### Pipeline FromCrawler 工厂约定

- **`CrawlerAwarePipeline` 可选接口** — Pipeline 可在 Open 前通过 `FromCrawler(c Crawler)` 获取 Crawler 引用
- **对齐 Scrapy** `from_crawler(cls, crawler)` 工厂方法约定

#### CONCURRENT_ITEMS 并发 Pipeline 处理

- **Scraper 并发 Item 处理** — 信号量通道控制同时在 Pipeline 链中的 Item 上限（默认 100）
- **多 Item 并发，单 Item 串行** — 对齐 Scrapy 语义
- **优雅关闭协同** — `Scraper.Close` 等待所有 in-flight Item 处理完毕
- **Panic Recovery** — Item 处理 goroutine 内置 panic recovery

#### Request 便捷 API

- **`NewJSONRequest`** — JSON API 请求构造器（对齐 Scrapy `JsonRequest`）
- **`NewFormRequest`** — 表单请求构造器（对齐 Scrapy `FormRequest`）
- **`NoCallback` 哨兵值** — 显式标记请求不需要回调（对齐 Scrapy `NO_CALLBACK`）
- **便捷 Option** — `WithRawBody` / `WithBasicAuth` / `WithUserAgent` / `WithFormData`

#### CrawlerRunner 多爬虫调度器

- **`Runner` 类型** — 对齐 Scrapy `CrawlerRunner`
  - `Crawl(ctx, c, sp)` — 异步启动单个 Crawler
  - `StartConcurrent(ctx, jobs...)` — 并发运行多个 Spider
  - `StartSequentially(ctx, jobs...)` — 顺序运行多个 Spider
  - `ConnectSignal(sig, handler)` — 跨爬虫信号传播
  - `Stop()` / `Wait()` / `Close()` — 统一生命周期管理
- **Crawler 新增 API** — `Crawl` / `Stop` / `Spider` / `IsCrawling`

#### 下载器中间件

- **HttpProxy 中间件**（优先级 750）— 环境变量代理 + 请求级代理 + 代理认证
- **DownloaderStats 中间件**（优先级 850）— 请求/响应/异常/耗时多维度统计

#### Spider 内置中间件（5 个）

- **HttpError**（优先级 50）— 过滤非 2xx 响应
- **Offsite**（优先级 500）— 站外请求过滤
- **Referer**（优先级 700）— 自动设置 Referer 头
- **UrlLength**（优先级 800）— 过滤超长 URL
- **Depth**（优先级 900）— 爬取深度控制

### 修复

- **Engine closeSpider 收尾顺序** — 修复"先关闭扩展再派发 SpiderClosed 信号"导致的最终指标丢失
- **下载层统计职责归位** — 移除 Engine 越界统计，统一由 CoreStats + DownloaderStats 各司其职
- **Pipeline 统计重复计数** — 统一由 CoreStatsExtension 通过信号机制完成

### 质量

- 全部 **626** 个测试通过
- `go test ./... -race` 无竞态报告
- `go vet ./...` 无告警
- 核心包覆盖率（均 ≥85%）：
  - `scheduler` 98.2%、`selector` 98.0%、`item` 94.9%、`spider` 94.1%
  - `scraper` 91.6%、`pipeline` 90.2%、`extension` 89.3%
  - `spider/middleware` 88.0%、`errors` 87.0%、`downloader/middleware` 86.7%
  - `http` 86.1%、`feedexport` 85.6%、`signal` 85.4%

### 依赖

- 无新增外部依赖（Phase 2 全部功能仅使用 Go 标准库）

---

<details>
<summary>📦 v0.3.0 预发布版本历史（alpha.1 ~ alpha.10）</summary>

## [v0.3.0-alpha.9] - 2026-04-28

### 新增

#### FieldMeta 驱动序列化（Sprint 6 / P2-009-ext1）

对齐 Scrapy 的 `BaseItemExporter.serialize_field` 机制，让 `FieldMeta` 元数据
真正驱动 Feed Export 的字段序列化行为。

- **新增 `pkg/feedexport/serializer.go`**
  - `SerializeFunc` 类型 — 字段序列化函数签名 `func(value any) any`
  - `RegisterSerializer(name, fn)` — 注册命名序列化函数（线程安全，可覆盖）
  - `LookupSerializer(name)` — 按名称查找已注册的序列化函数
  - `ClearSerializers()` — 清空注册表（仅用于测试）
  - `SerializeField(meta, name, value)` — 根据 `FieldMeta` 中的 `serializer` 键
    查表调用，未命中回退原始值
  - `serializeItemFields(item, fieldsToExport)` — 内部辅助函数，替代原有的
    `extractItem`，在读取字段值时自动应用 serialize_field 钩子

- **Exporter 集成**
  - JSON / JSON Lines / CSV / XML 四种 Exporter 均已接入 `serializeItemFields`，
    所有字段在写入前自动经过 serialize_field 钩子处理
  - struct 类型的 Item 可通过 `item:"price,serializer=to_int"` tag 声明序列化器

- **与 Scrapy 原版的差异**
  - Scrapy 的 `serialize_field` 是 Exporter 的虚方法，子类可覆盖；Go 版本采用
    注册表模式（`RegisterSerializer`），更符合 Go 的组合优于继承理念
  - Scrapy 的 `serializer` 是 Field 中的 callable；Go 版本通过名称字符串查表，
    避免 struct tag 中无法嵌入函数引用的限制

- **测试**
  - 新增 `pkg/feedexport/serializer_test.go`，21 个测试用例
  - 覆盖：注册/查找/覆盖/清空、SerializeField 各分支、struct + FieldMeta 端到端、
    四种 Exporter 集成验证
  - `pkg/feedexport` 覆盖率 **85.6%**，满足 Phase 2 核心包 ≥85% 要求

#### Pipeline FromCrawler 工厂约定（Sprint 6 / P2-009-ext2）

对齐 Scrapy 的 `from_crawler(cls, crawler)` 工厂方法约定（需求 13 验收标准 6），
允许 Pipeline 在初始化时获取 Crawler 引用以访问 Settings、Stats、Signals 等框架组件。

- **新增 `pipeline.Crawler` 接口**（`pkg/pipeline/pipeline.go`）
  - `GetSettings() *settings.Settings`
  - `GetStats() stats.Collector`
  - `GetSignals() *signal.Manager`
  - `GetLogger() *slog.Logger`
  - 在 pipeline 包中定义以避免 pipeline → crawler 循环依赖；`crawler.Crawler`
    隐式满足此接口

- **新增 `pipeline.CrawlerAwarePipeline` 可选接口**
  - 嵌入 `ItemPipeline` + `FromCrawler(c Crawler) error`
  - `Manager.Open` 时若 Pipeline 实现该接口且 Crawler 引用已设置，则在调用
    `Pipeline.Open` 之前先调用 `FromCrawler`
  - `FromCrawler` 返回 error 将阻止该 Pipeline 的 Open 调用

- **`Manager.SetCrawler(c Crawler)`** — 设置 Crawler 引用，由 `crawler.Crawler`
  在 `assembleComponents` 中自动调用

- **`crawler.Crawler` 新增 Getter 方法**
  - `GetSettings()` / `GetStats()` / `GetSignals()` / `GetLogger()`
  - 满足 `pipeline.Crawler` 接口

- **测试**
  - 新增 `pkg/pipeline/fromcrawler_test.go`，7 个测试用例
  - 覆盖：FromCrawler 调用、Settings/Stats 访问、非 CrawlerAware Pipeline 不受影响、
    未设置 Crawler 时跳过、FromCrawler 错误传播、混合 Pipeline 执行顺序
  - `pkg/pipeline` 覆盖率 **90.2%**，超过 Phase 2 核心包 ≥85% 要求

- **依赖影响**
  - 无新增外部依赖，仅使用 Go 标准库

---

## [v0.3.0-alpha.8] - 2026-04-28

### 新增

#### Item 体系与 ItemAdapter（Sprint 6 / P2-009）

对齐 Scrapy 的 `itemadapter` 库，为 scrapy-go 提供统一的 Item 访问抽象，使
Pipeline / Exporter / 审计日志等下游组件无需感知底层 Item 类型（struct / map /
自定义类型）。

- **新增包 `pkg/item/`**
  - 核心接口：`ItemAdapter`（`adapter.go`）— `Item / FieldNames / GetField /
    SetField / HasField / AsMap / Len / FieldMeta`
  - 内置实现：
    - `MapAdapter`（`mapadapter.go`）— 适配 `map[string]any` / `map[string]string`
      / 其他 `key=string` 的 map；字段按字典序输出；支持反射路径下的类型转换
    - `StructAdapter`（`structadapter.go`）— 基于 `reflect` 适配任意 struct /
      *struct；字段名按 `item` tag → `json` tag → Go 字段名的优先级解析；
      `item:"-"` 显式跳过；struct-level 元数据通过 `sync.Map` 进程级缓存，避免每次
      Adapter 创建都重复反射
  - 工厂与扩展点：
    - `Adapt(item any) ItemAdapter`（`adapt.go`）— 自动检测：接口实现 → 用户
      注册工厂 → map → struct → nil
    - `MustAdapt(item any) ItemAdapter` — 适配失败时 panic，适合"调用方已保证
      类型正确"的场景
    - `Register(factory AdapterFactory)` — 注册自定义工厂（栈序，后注册先匹配）；
      用于为第三方 ORM、protobuf Message 等提供定制适配
    - `IsItem(item any) bool` — 对齐 Scrapy `is_item`，判断是否能被 `Adapt`
    - `AsMap(item) / FieldNames(item)` — 便捷函数
  - 字段元数据：
    - `FieldMeta` 类型（`map[string]any`）+ `Get / GetString / Clone` 方法
    - 自动解析 struct tag：`item:"name,key1=val1,key2"` 与 `json:"name,omitempty"`
      的非首个 token 进入 meta（JSON 的 token 以 `json_<token>=true` 形式记录）
  - 哨兵错误：`ErrFieldNotFound` / `ErrFieldReadOnly` / `ErrUnsupportedItem`

- **与 Scrapy 原版的差异（有意舍弃 / 重新设计）**
  - 舍弃 Python 的元类（`ItemMeta`）与 MRO 动态分发，改用 Go 的 interface + reflect
  - 舍弃第三方适配层（attrs / dataclass / pydantic），Go 版本只保留 map + struct +
    用户自定义三条路径，新增语言特性支持通过 `Register` 注入
  - `SetField` 显式返回 `error`（Scrapy 通过 `__setitem__` 抛异常）
  - `HasField` 对 struct 按"声明"判定（所有声明字段都视为存在）；对 map 按"键存在"
    判定——这是 Go 类型系统下自然的语义选择，Pipeline 需要"已赋值"语义时应通过
    指针字段或业务层标志位自行维护
  - 字段元数据在首次访问时懒构建并缓存

- **与 Feed Export 集成**
  - `pkg/feedexport/item.go` 重写为 `item.Adapt` 的薄封装，所有 Exporter
    （JSON / JSON Lines / CSV / XML）通过 `ItemAdapter` 统一读取字段，原有的私有
    反射代码全部迁移到 `pkg/item` 以消除重复
  - 新增集成测试：`pkg/feedexport/itemadapter_test.go` — 三种 Item 类型
    （struct / map / 自实现 `ItemAdapter`）在同一 Exporter 中混合导出
  - 端到端集成测试：`tests/integration/itemadapter_test.go`
    - `TestFeedExport_ItemAdapter_MixedTypes` — Crawler 级别同时导出 3 条异构
      Item 到 JSON Lines 与 CSV，验证字段对齐、`item:"-"` 隐藏字段被过滤
    - `TestItemAdapter_Pipeline_ProcessHeterogeneousItems` — 自定义 Pipeline
      通过 `item.Adapt` 统一修改异构 Item 的字段，修改后通过 Feed Export 持久化

- **测试**
  - 单元测试：`pkg/item/adapter_test.go`，覆盖率 **94.9%**，超过 Phase 2 核心包
    ≥85% 要求
  - 集成测试：两个端到端场景（Feed Export + Pipeline）
  - 全部测试通过 `go test ./... -race`，无竞态；`go vet ./...` 无告警

- **依赖影响**
  - 无新增外部依赖，仅使用 Go 标准库（`reflect`、`sync`、`strings`）

### 修复

#### Pipeline 统计计数重复（item_scraped_count / item_dropped_count 双倍计数）

修复 `Pipeline.Manager.ProcessItem` 中 `item_scraped_count` 和 `item_dropped_count` 被重复递增的 bug。

- **问题背景**
  - `Pipeline.Manager.ProcessItem` 在处理成功/丢弃时既直接调用 `stats.IncValue("item_scraped_count"/"item_dropped_count")` 又发出 `ItemScraped`/`ItemDropped` 信号
  - `CoreStatsExtension` 监听这两个信号后再次调用 `stats.IncValue`，导致每个 Item 被计数 **2 次**
  - 该问题与之前修复的 `response_received_count` 重复计数（v0.3.0-alpha.5）属于同一类 bug：Engine/Pipeline 层越界直接操作统计

- **修复内容**
  - 从 `pkg/pipeline/pipeline.go` 的 `ProcessItem` 方法中移除 `item_scraped_count` 和 `item_dropped_count` 的直接 `IncValue` 调用
  - 统计计数统一由 `CoreStatsExtension` 通过信号机制完成，与 Scrapy 原版设计一致
  - `item_error_count` 保留在 Pipeline 中（CoreStats 未监听 `ItemError` 信号），无重复问题
  - 在关键位置添加注释说明职责归属，避免未来误改

- **测试调整**
  - `pkg/pipeline/pipeline_test.go`：移除 `TestManagerProcessItemNormal` 和 `TestManagerProcessItemDrop` 中对 `item_scraped_count`/`item_dropped_count` 的直接断言（这些测试未注册 CoreStats 扩展）
  - `pkg/scraper/scraper_test.go`：移除 `TestScraperWithPipeline` 中对 `item_scraped_count` 的断言
  - 端到端集成测试（`tests/integration/`）通过 Crawler 完整流程验证统计正确性

### 质量

- `go test ./... -race`：全量测试通过，无竞态
- `go vet ./...`：零告警

---

## [v0.3.0-alpha.7] - 2026-04-28

### 新增

#### Feed Export 数据导出系统（Sprint 6 / P2-008）

对齐 Scrapy 的 `scrapy.extensions.feedexport` + `scrapy.exporters`，为 scrapy-go 提供统一的多格式数据导出能力。

- **新增包 `pkg/feedexport/`**
  - 核心接口：`ItemExporter`、`FeedStorage`、`ExporterFactory`（`interface.go`）
  - 内置导出器：
    - `JSONExporter`（`json.go`）— 整体写出 JSON 数组，支持 `FieldsToExport` 保序与 `Indent`
    - `JSONLinesExporter`（`jsonlines.go`）— 逐行 JSON，适合流式/大数据
    - `CSVExporter`（`csv.go`）— 自动写入表头，支持 `JoinMultivalued` 拼接多值字段、字段缺失输出空值
    - `XMLExporter`（`xml.go`）— 支持自定义 `RootElement` / `ItemElement`，非法字段名按 `field_N` 脱敏
  - 存储后端：
    - `FileStorage`（`storage.go`）— 支持相对/绝对路径、`file://` URI、自动创建父目录、`overwrite` 与 `append` 模式
    - `StdoutStorage` — 标准输出，安全包装避免外部关闭 `os.Stdout`
    - `NewStorageForURI` 工厂：根据 URI scheme 自动选择后端
  - `FeedSlot`（`slot.go`）— 封装单条 Feed 的生命周期：`Start/ExportItem/Close`，支持延迟启动（首个 Item 到达才创建文件）、`StoreEmpty` 即时启动、`Filter` 过滤
  - URI 模板渲染：支持 `%(name)s` / `%(time)s` / `%(batch_id)d` / `%(batch_time)s` 占位符，对应 Scrapy `_FeedSlot` 的 URI 参数
  - Item 字段提取：`extractItem` 同时支持 `map[string]any`、`map[string]string`、自定义 `map`、`struct`（通过 `item` tag / `json` tag / 字段名回退）

- **新增扩展 `pkg/extension/feedexport.go`**
  - `FeedExportExtension` 实现 `Extension` 接口，通过信号系统接入 Spider 生命周期：
    - `SpiderOpened` → 为每条 `FeedConfig` 构造 `FeedSlot`，渲染 URI 模板；`StoreEmpty=true` 时立即 Start
    - `ItemScraped` → 分发 Item 到所有 Slot，错误以 `errors.Join` 聚合，同时写入 `feedexport/error_count/<uri>` 统计
    - `SpiderClosed` → 关闭全部 Slot，写入 `feedexport/success_count/<uri>`、`feedexport/failed_count/<uri>`、`feedexport/items_count/<uri>`
  - 配置为空时返回 `ErrNotConfigured`，框架自动跳过
  - `Close` 注销所有信号处理器并执行防御性清理（异常路径下仍能 flush）

- **新增 `Crawler` API**
  - `Crawler.AddFeed(cfg feedexport.FeedConfig)`：以 Go 类型安全方式注入 Feed 配置
  - `Crawler.buildFeedExportConfigs()`：合并 `AddFeed` + `Settings.FEEDS` + `FEED_URI/FEED_FORMAT`（兼容 Scrapy 旧字段）

- **新增默认配置 `pkg/settings/defaults.go`**
  - `FEEDS`（`map[string]map[string]any`，默认空）
  - `FEED_EXPORT_ENCODING` / `FEED_EXPORT_INDENT` / `FEED_STORE_EMPTY` / `FEED_EXPORT_BATCH_ITEM_COUNT`
  - `FEED_URI` / `FEED_FORMAT`（向后兼容 Scrapy 旧字段）
  - `EXTENSIONS_BASE` 新增 `FeedExport: 0`（默认启用但未配置时自动跳过）

- 示例 `examples/feedexport/main.go`
  - 覆盖 `pkg/feedexport` 全部核心 API：四种格式直接使用、ExporterOptions 全部选项、
    RegisterExporter 自定义格式、RegisterSerializer 序列化器、URI 模板、存储后端、
    FeedSlot 直接使用、FeedConfig 全部选项、Crawler.AddFeed 端到端集成
- 示例 `examples/itemadapter/main.go`
  - 覆盖 `pkg/item` 全部核心 API：IsItem、MapAdapter、StructAdapter、struct tag 解析、
    FieldMeta、顶层便捷函数、自定义 ItemAdapter 接口实现、Register 自定义工厂

- **测试**
  - 单元测试：`pkg/feedexport/exporters_test.go`、`storage_test.go`、`coverage_test.go`（覆盖率 **85.2%**，达 Phase 2 核心包 ≥85% 要求）
  - 扩展测试：`pkg/extension/feedexport_test.go`（扩展包覆盖率 **89.3%**）
  - 集成测试：`tests/integration/feedexport_test.go`（10 个端到端用例，含多格式、多 Feed 并行、URI 模板、Settings 驱动、并发压力）
  - 全部测试 `go test -race` 通过，无竞态

- **与 Scrapy 原版的差异（有意舍弃）**
  - 未实现 S3/FTP/GCS 等远程存储（可通过用户自定义 `FeedStorage` 扩展，核心框架只保留本地文件与 Stdout）
  - 未实现 `BATCH_ITEM_COUNT`（分片导出）；配置项保留占位，留待后续迭代
  - 未实现 `PostProcessing`（gzip/lz4 等后处理）；可由用户在 `FeedStorage.Store` 中自行完成
  - 通过 `pkg/item.ItemAdapter` 统一 Item 访问（详见本次 Sprint 6 新增的 Item 体系条目），对等 Scrapy 的 `ItemAdapter` 体系

### 规划（迭代日程登记，尚未实现）

基于 Scrapy 原版 Request API 的对比分析，新增以下三项 Request 便捷 API 规划到迭代日程（详见 `scrapy-go-iteration-schedule.md` v1.7）：

- **P2-011 — Request 便捷 Option 与 JSON 支持**（Sprint 6，预估 3d）
  - 便捷 Option：`WithForm` / `WithRawBody` / `WithBasicAuth` / `WithUserAgent`
  - 独立构造函数：`NewJSONRequest(url, data, opts...) (*Request, error)`（错误显式返回，不以 Option 形式吞错）
  - 独立构造函数：`NewFormRequest(url, formdata, opts...) (*Request, error)`（POST 写 body、GET 写 query）
  - `NoCallback` 哨兵值（对齐 Scrapy `NO_CALLBACK`）
- **P3-012 — FormRequestFromResponse 与 Multipart 支持**（Sprint 7，预估 3d）
  - `FormRequestFromResponse(resp, opts...)`：基于 `pkg/selector` 自动提取 HTML `<form>` 的 action/method/inputs
  - 支持 `formname` / `formid` / `formnumber` / `formxpath` / `formcss` 表单定位
  - `NewMultipartFormRequest(url, fields, files, opts...) (*Request, error)`：基于 `mime/multipart` 标准库，支持文件上传
- **P3-013 — Request 序列化与 curl 互操作**（Sprint 8，预估 2.5d，为 P3-003 磁盘队列前置）
  - `Request.ToDict() map[string]any` / `FromDict(d map[string]any) (*Request, error)`（对齐 Scrapy `request_from_dict`）
  - Callback/Errback 通过 Spider 方法名字符串反查，支撑磁盘队列跨进程恢复
  - `Request.FromCURL(curl string, opts...) (*Request, error)`（对齐 Scrapy `Request.from_curl`）

### 规划变更

- **P3-003 磁盘队列**：工时由 8d 调整为 7d，依赖从"无"改为 P3-013；`P3-003b` 子任务改为"调度器层序列化封装（基于 `Request.ToDict/FromDict` + `encoding/json`）"，与 P3-013a 分层配合
- **P3-004 v0.4.0 发布准备**：依赖追加 `P3-012, P3-013`
- **Phase 2 关键路径**：新增 `P2-011 (Request 便捷 API, 3d)` 并行分支
- **技术债务登记 TD-009**：`FormRequestFromResponse` 不覆盖 JavaScript 动态生成的表单（仅静态 `<form>`）；`from_curl` 不支持 `--data-urlencode` 等复杂选项（优先级：低）
- **舍弃**：`XmlRpcRequest`（Go 生态 XML-RPC 场景稀缺，不纳入规划）

### 依赖影响

- P2-011 / P3-012 / P3-013 **仅依赖 Go 标准库**（`encoding/json` / `net/url` / `mime/multipart`），不引入新的外部依赖

---

## [v0.3.0-alpha.6] - 2026-04-28

### 修复

#### Engine closeSpider 收尾顺序修复（扩展最终指标丢失）

修复 `Engine.closeSpider` 中"先关闭扩展再派发 `SpiderClosed` 信号"导致的最终指标丢失问题。

- **问题背景**
  - 原先的关闭顺序为：`scheduler.Close` → `extensions.Close` → `SpiderClosed` 信号 → `stats.Close`（dump）
  - `CoreStatsExtension` / `LogStatsExtension` / `CloseSpiderExtension` 等扩展在自身 `Close` 中会注销 `SpiderClosed` 处理器
  - 因此当信号派发时处理器已不存在，`finish_time`、`elapsed_time_seconds`、`finish_reason`、`responses_per_minute`、`items_per_minute` 等最终指标无法写入 stats
  - 最终 stats dump 输出缺失这些指标

- **修复内容**
  - 调整 `pkg/engine/engine.go` 中 `closeSpider` 的执行顺序为：`scheduler.Close` → **`SpiderClosed` 信号派发** → `extensions.Close` → `stats.Close`（dump）
  - 该顺序与 Scrapy 原版 `ExecutionEngine.close_spider` 保持一致
  - 在关键位置添加详细注释说明顺序约束与 bug 背景，避免未来误改

- **回归测试**
  - 新增 `TestEngineCoreStatsFinalMetrics`，验证 `start_time` / `finish_time` / `elapsed_time_seconds` / `finish_reason` 在 Spider 结束后确实存在于 stats 中
  - 测试显式断言关闭顺序错误时会立即暴露（`t.Fatal` 附带修复提示）

### 质量

- `go test ./pkg/engine/... -race`：全部通过
- `go test ./... -race`：全量测试通过
- `go vet ./...`：零告警

---

## [v0.3.0-alpha.5] - 2026-04-28

### 修复

#### 下载层统计职责归位（Engine 去越界统计）

对齐 Scrapy 原版"引擎派发信号 + 中间件统计下载层 + 扩展监听信号维护核心指标"的分层设计，修复 Engine 中直接写入下载层统计导致的重复计数问题。

- **移除 `pkg/engine/engine.go` 中的两行越界统计**
  - 删除 `e.stats.IncValue("response_received_count", 1, 0)` — 改由 `CoreStatsExtension` 监听 `ResponseReceived` 信号统一递增
  - 删除 `e.stats.IncValue("downloader/response_status_count/%d", ...)` — 改由 `DownloaderStatsMiddleware.ProcessResponse` 统一统计
- **收益**
  - 消除双写：当 CoreStats 扩展与 DownloaderStats 中间件启用时，指标不再翻倍
  - 职责收敛：Engine 仅负责调度 + 信号派发 + 引擎视角日志，不再穿透下载层抽象
  - 配置生效：`DOWNLOADER_STATS=false` 禁用时，下载层统计能够被真正关闭

### 变更

- `pkg/engine/engine_test.go` `buildTestEngine` 测试夹具同步调整
  - 新增注入 `DownloaderStatsMiddleware`（优先级 850）
  - 新增注入 `CoreStatsExtension`（通过 `extension.Manager` 传入 Engine）
  - 保证 `TestEngineBasicCrawl` 等测试对 `response_received_count` / `downloader/response_status_count/200` 的断言继续生效

### 质量

- 全量测试：442 个测试通过
- `go test ./... -race`：竞态检测通过
- `go vet ./...`：零告警

---

## [v0.3.0-alpha.4] - 2026-04-27

### 新增

#### CrawlerRunner 多爬虫调度器（P2-010）

对应 Scrapy Python 版本的 `CrawlerRunner` / `AsyncCrawlerRunner`，实现了多 Crawler 的并发/顺序调度与统一生命周期管理。

- **`Runner` 类型** — 新增 `pkg/crawler/runner.go`，封装多爬虫调度逻辑
  - `NewRunner(opts ...RunnerOption) *Runner` — 构造器
  - `WithRunnerLogger(logger)` / `WithOSSignalHandling(enabled)` — 可选配置
- **单爬虫异步调度**
  - `Runner.Crawl(ctx, c, sp) <-chan error` — 异步启动单个 Crawler，返回完成通知 channel
- **多爬虫并发/顺序调度**
  - `Runner.StartConcurrent(ctx, jobs...)` — 并发启动多个 Spider，阻塞直到全部完成
  - `Runner.StartSequentially(ctx, jobs...)` — 按 jobs 顺序依次启动，前一个完成后再启动下一个
  - `Job` / `NewJob(c, sp)` — 描述 Crawler + Spider 绑定的爬取任务
- **跨爬虫 Signal 传播**
  - `Runner.ConnectSignal(sig, handler)` — 为所有当前/未来加入的 Crawler 注册同一个信号处理器
  - 通过 Crawler 内部的 `beforeStart` 钩子在组件组装完成、Engine 启动之前注册，保证 `EngineStarted`/`SpiderOpened` 等早期信号能被捕获
- **生命周期控制**
  - `Runner.Stop()` — 请求所有正在运行的 Crawler 优雅停止（立即返回）
  - `Runner.Wait()` — 阻塞等待所有 Crawler 完成（对应 Scrapy 的 `join`）
  - `Runner.Close()` — 停止所有 Crawler 并等待完成，之后不再接受新的 Crawler
  - `Runner.Crawlers()` / `Runner.BootstrapFailed()` — 状态查询
- **OS 信号处理**
  - 默认监听 SIGINT/SIGTERM，两阶段处理：第一次优雅关闭，第二次强制退出（exit code 130）
  - 通过 `WithOSSignalHandling(false)` 关闭内置信号处理（适合测试或外部统一管理信号）

#### Crawler 新增 API

- **`Crawler.Crawl(ctx, sp)`** — 与 `Run` 并列的爬取入口，**不安装 OS 信号处理器**，供 Runner 等上层编排器调用
- **`Crawler.Stop()`** — 请求优雅停止当前运行的爬虫，多次调用安全
- **`Crawler.Spider()`** — 返回关联的 Spider 实例
- **`Crawler.IsCrawling()`** — 查询爬虫是否正在运行
- **单次运行约束** — Crawler 实例只能运行一次，通过 `atomic.Bool.CompareAndSwap` 保护

### 变更

- `Crawler.Run` 逻辑抽取到内部 `crawl` 方法，`Run` 与 `Crawl` 共享核心路径（前者安装 OS 信号处理器，后者不安装）
- 新增内部 `Crawler.onBeforeStart(hook)` 钩子机制（非导出），供 Runner 在组件重建后注入跨爬虫信号处理器
- 修复 Crawler 在 OS 信号到达时 `signal.Notify` 的 cleanup 时机，改用 `defer signal.Stop` 避免 channel 泄漏

### 质量

- 新增 `pkg/crawler/runner_test.go` — 26 个单元测试，覆盖 Job/Option/Crawler 管理/Crawl/StartConcurrent/StartSequentially/ConnectSignal/Stop/Wait/Close/并发安全压力测试
- 新增 `tests/integration/runner_test.go` — 5 个端到端集成测试
  - `TestRunner_E2E_ConcurrentMultipleSpiders` — 5 个 Spider 并发耗时验证
  - `TestRunner_E2E_ConcurrentFiveOrMoreSpiders` — 8 个 Spider 并发（对应全局成功指标）
  - `TestRunner_E2E_SequentialOrder` — 顺序执行正确性验证
  - `TestRunner_E2E_CrossCrawlerSignalPropagation` — 跨爬虫 Signal 传播验证
  - `TestRunner_E2E_StopGracefullyInterruptsAllCrawlers` — 优雅停止验证
- 测试总数：442 个
- `pkg/crawler` 包 runner.go 行级覆盖率：约 80%（installSignalHandler 中的真实信号分支难以稳定单测，已通过启用/禁用两种路径覆盖）
- 竞态检测：`go test ./... -race` 全部通过
- `go vet`：全部通过

### Phase 2 Sprint 5 进度

- ✅ P2-004 内置扩展（上一版已完成）
- ✅ P2-005 Spider 内置中间件（上一版已完成）
- ✅ **P2-010 CrawlerRunner 多爬虫调度器（本版）**

---

## [v0.3.0-alpha.3] - 2026-04-27

### 新增

#### Spider 内置中间件（P2-005，5 个）

- **HttpError 中间件**（优先级 50）— 过滤非 2xx 响应（`pkg/spider/middleware/httperror.go`）
  - 支持 `HTTPERROR_ALLOW_ALL` 全局允许所有状态码
  - 支持 `HTTPERROR_ALLOWED_CODES` 全局允许特定状态码列表
  - 支持 `Request.Meta["handle_httpstatus_all"]` 请求级允许所有
  - 支持 `Request.Meta["handle_httpstatus_list"]` 请求级允许列表
  - 统计：`httperror/response_ignored_count`、`httperror/response_ignored_status_count/{STATUS}`

- **Offsite 中间件**（优先级 500）— 站外请求过滤（`pkg/spider/middleware/offsite.go`）
  - 基于 Spider `AllowedDomains()` 接口过滤站外请求
  - 支持子域名匹配（`example.com` 匹配 `www.example.com`）
  - `Request.DontFilter=true` 或 `Meta["allow_offsite"]=true` 跳过过滤
  - 统计：`offsite/filtered`、`offsite/domains`

- **Referer 中间件**（优先级 700）— 自动设置 Referer 头（`pkg/spider/middleware/referer.go`）
  - 使用简化的 scrapy-default 策略（no-referrer-when-downgrade）
  - HTTPS→HTTP 降级不发送 Referer；本地 scheme（file://、data://）不发送
  - 自动去除 URL 中的 fragment 和认证信息
  - 不覆盖已存在的 Referer 头
  - 配置项：`REFERER_ENABLED`（默认 true）

- **UrlLength 中间件**（优先级 800）— 过滤超长 URL（`pkg/spider/middleware/urllength.go`）
  - 在 ProcessOutput 阶段过滤 URL 长度超过 `URLLENGTH_LIMIT` 的请求
  - 统计：`urllength/request_ignored_count`

- **Depth 中间件**（优先级 900）— 爬取深度控制（`pkg/spider/middleware/depth.go`）
  - 自动为请求设置 `depth` Meta（父响应 depth + 1）
  - `DEPTH_LIMIT` 超过限制的请求被丢弃
  - `DEPTH_PRIORITY` 根据深度调整请求优先级
  - `DEPTH_STATS_VERBOSE` 记录各深度请求数统计
  - 统计：`request_depth_max`、`request_depth_count/{N}`

### 变更
- `SPIDER_MIDDLEWARES_BASE` 默认注册 5 个内置中间件：HttpError(50)、Offsite(500)、Referer(700)、UrlLength(800)、Depth(900)
- Crawler `buildSpiderMiddlewares()` 新增 `builtinSpiderMiddlewareFactories` 注册表
- 新增配置项：`HTTPERROR_ALLOW_ALL`（默认 false）、`HTTPERROR_ALLOWED_CODES`（默认 []）

### 质量
- 新增 35 个 Spider 中间件单元测试
- 测试总数：411 个
- Spider 中间件包覆盖率：82.0%
- 竞态检测：全部通过
- `go vet`：全部通过

---

## [v0.3.0-alpha.2] - 2026-04-27

### 新增

#### 内置扩展实现（P2-004）

- **CoreStats 扩展** — 收集核心统计信息（`pkg/extension/corestats.go`）
  - 监听 `spider_opened`/`spider_closed`/`item_scraped`/`item_dropped`/`response_received` 信号
  - 记录 `start_time`、`finish_time`、`elapsed_time_seconds`、`finish_reason`
  - 通过信号自动递增 `item_scraped_count`、`item_dropped_count`、`response_received_count`

- **CloseSpider 扩展** — 条件自动关闭 Spider（`pkg/extension/closespider.go`）
  - `CLOSESPIDER_TIMEOUT` — 运行超时自动关闭（秒）
  - `CLOSESPIDER_ITEMCOUNT` — 达到 Item 数量自动关闭
  - `CLOSESPIDER_PAGECOUNT` — 达到页面数量自动关闭
  - `CLOSESPIDER_ERRORCOUNT` — 达到错误数量自动关闭
  - 使用原子计数器和 CAS 确保并发安全，所有条件为 0 时返回 `ErrNotConfigured` 自动禁用

- **LogStats 扩展** — 定期输出爬取统计摘要（`pkg/extension/logstats.go`）
  - 定期输出 RPM（每分钟页面数）和 IPM（每分钟 Item 数）
  - Spider 关闭时计算并记录最终平均速率（`responses_per_minute`、`items_per_minute`）
  - `LOGSTATS_INTERVAL` 配置输出间隔（秒），设为 0 自动禁用

- **MemoryUsage 扩展** — Go 运行时内存监控（`pkg/extension/memusage.go`）
  - 使用 `runtime.MemStats.Sys` 监控系统内存占用
  - `MEMUSAGE_LIMIT_MB` — 超过限制自动关闭 Spider
  - `MEMUSAGE_WARNING_MB` — 超过阈值记录警告日志（仅一次）
  - 统计项：`memusage/startup`、`memusage/max`、`memusage/limit_reached`、`memusage/warning_reached`

### 变更
- `EXTENSIONS_BASE` 默认注册 4 个内置扩展：CoreStats、CloseSpider、LogStats、MemoryUsage
- Crawler `buildExtensions()` 新增 `builtinExtensionFactories` 注册表，按配置实例化内置扩展
- 内置扩展通过 `ErrNotConfigured` 机制自动禁用未配置的扩展

### 质量
- 新增 21 个内置扩展单元测试
- 测试总数：376 个
- Extension 包覆盖率：81.6%
- 竞态检测：全部通过
- `go vet`：全部通过

---

## [v0.3.0-alpha.1] - 2026-04-27

### 新增

#### Extension 系统框架（P2-001）
- **Extension 接口** — 定义 `Extension` 接口（`Open`/`Close` 生命周期），提供 `BaseExtension` 默认实现
- **ExtensionManager** — 扩展管理器，支持按优先级排序、`ErrNotConfigured` 自动跳过、逆序关闭
- **Crawler 集成** — Extension 系统集成到 Crawler 组件编排流程，支持 `EXTENSIONS_BASE`/`EXTENSIONS` 配置
- **AddExtension API** — Crawler 新增 `AddExtension(ext, name, priority)` 方法注册自定义扩展

#### HttpProxy 中间件（P2-002，优先级 750）
- **环境变量代理** — 自动读取 `http_proxy`/`HTTP_PROXY`、`https_proxy`/`HTTPS_PROXY` 环境变量
- **请求级代理** — 支持 `Request.Meta["proxy"]` 设置请求级代理，`nil` 值显式禁用代理
- **代理认证** — 支持 `http://user:password@host:port` 格式的代理 URL，自动设置 `Proxy-Authorization` 头
- **配置项** — `HTTPPROXY_ENABLED`（默认 true）

#### DownloaderStats 中间件（P2-003，优先级 850）
- **请求统计** — `downloader/request_count`、`downloader/request_method_count/{METHOD}`、`downloader/request_bytes`
- **响应统计** — `downloader/response_count`、`downloader/response_status_count/{STATUS}`、`downloader/response_bytes`
- **异常统计** — `downloader/exception_count`、`downloader/exception_type_count/{TYPE}`
- **耗时统计** — `downloader/max_download_time`（最大下载耗时）
- **配置项** — `DOWNLOADER_STATS`（默认 true）

### 变更
- `DOWNLOADER_MIDDLEWARES_BASE` 新增 `HttpProxy`(750) 和 `DownloaderStats`(850)
- Engine 构造函数新增 `extensions` 参数，支持扩展系统生命周期管理
- Engine `openSpider` 中初始化扩展，`closeSpider` 中关闭扩展

### 质量
- 新增 30+ 个单元测试（Extension 10 个、HttpProxy 8 个、DownloaderStats 12 个）
- 测试总数：353 个
- Extension 包覆盖率：100%
- 中间件包覆盖率：86.7%
- 竞态检测：全部通过

---

## [v0.2.4] - 2026-04-27

</details>

---

## [v0.2.4] - 2026-04-27

### 新增
- **Brotli 解压支持** — HttpCompression 中间件新增 brotli (br) 编码解压，引入 `andybalholm/brotli` 外部依赖
  - `Accept-Encoding` 请求头自动包含 `br`
  - 响应体 `Content-Encoding: br` 自动解压
  - 支持 maxSize 限制和统计收集

### 依赖
- 新增 `github.com/andybalholm/brotli` v1.2.1 — Brotli 压缩/解压

### 质量
- 新增 4 个 brotli 相关单元测试（解压、maxSize、统计、Accept-Encoding 验证）
- 全量测试通过，竞态检测通过

### 技术债务
- TD-006 已偿还：HttpCompression 现已支持 brotli 解压

---

## [v0.2.3] - 2026-04-24

### 修复
- **NewRequestError 处理** — 在中间件管理器的 `processResponse` 和 `processException` 中添加 `NewRequestError` 的显式检查，确保重试/重定向产生的新请求能正确传播给 Engine 重新调度

### 重构
- **MiddlewareManager 位置调整** — 将下载器中间件管理器从 `pkg/downloader/middleware/manager.go` 移到 `pkg/downloader/middleware_manager.go`
  - `middleware.Manager` → `downloader.MiddlewareManager`
  - `middleware.NewManager()` → `downloader.NewMiddlewareManager()`
  - `middleware.Entry` → `downloader.MiddlewareEntry`
  - 更贴近 Scrapy 原版设计：Manager 是 downloader 的编排层，不是中间件本身
  - Engine 可直接使用 `downloader.MiddlewareManager`，无需 `dmiddle` 包别名
- **测试迁移** — Manager 相关测试从 `middleware/middleware_test.go` 移到 `downloader/middleware_manager_test.go`

### 变更
- 更新所有引用文件的导入路径：`engine.go`、`engine_test.go`、`engine_panic_test.go`、`crawler.go`
- 更新 README 核心组件表格和项目结构描述

---

## [v0.2.2] - 2026-04-24

### 新增
- **Panic Recovery** — 为所有关键 goroutine 添加 panic 恢复机制
  - Engine: `downloadAndScrape`、`consumeStartRequests`
  - Downloader: `processQueue`（自动重启）、下载 goroutine
  - Spider: `Base.Start()` 内部 goroutine
- **PanicError 错误类型** — 新增 `ErrPanic` 哨兵错误和 `PanicError` 结构化错误类型，包含 panic 值和堆栈信息
- **HTTP 状态码统计** — 自动统计每个 HTTP 响应状态码的数量（`downloader/response_status_count/XXX`）
- **Panic 统计** — 自动递增 `spider_exceptions/panic` 计数器

---

## [v0.2.1] - 2026-04-24

### 新增
- **日志英文化** — 所有框架日志信息统一改为英文格式，便于国际化和机器解析
- **彩色日志输出** — 新增 `ColorHandler`（自定义 `slog.Handler`），不同日志级别使用不同 ANSI 颜色，非终端时自动禁用
- **Scrapy 风格列表日志** — 中间件、Pipeline、统计信息使用 Scrapy 风格的一条日志包含完整列表
- **Pipeline 启用日志** — 补充 Pipeline 组件的启用状态日志

---

## [v0.2.0] - 2026-04-24

### 新增

#### 下载器中间件
- **DownloadTimeout 中间件**（优先级 300）— 基于 `context.WithTimeout` 的请求超时控制，支持全局和请求级覆盖
- **HttpAuth 中间件**（优先级 410）— Basic Auth 认证注入，支持域名限制和请求级 Meta 覆盖
- **HttpCompression 中间件**（优先级 590）— 自动添加 `Accept-Encoding` 头，支持 gzip/deflate 响应体解压
- **Cookies 中间件**（优先级 700）— 基于 `net/http/cookiejar` 的多会话 Cookie 管理，支持 `cookiejar` Meta 隔离

#### HTML 解析
- **Selector 包** (`pkg/selector`) — 提供链式调用的 CSS 和 XPath 选择器
  - `Selector.CSS()` — CSS 选择器查询，支持 `::text` 伪元素
  - `Selector.CSSAttr()` — CSS 选择器 + 属性提取（等价于 Scrapy 的 `::attr(name)`）
  - `Selector.XPath()` — XPath 表达式查询
  - `List` — 批量操作：`GetAll()`、`Get()`、`First()`、`Attr()`、`AttrAll()`
- **Response 扩展** — `Response.CSS()`、`Response.CSSAttr()`、`Response.XPath()`、`Response.Selector()` 快捷方法

#### 架构优化
- **NewRequestError 机制** — 重试/重定向通过错误类型传递新请求，替代 Meta 键 hack
- **Slot context 传播** — `downloadTask` 正确传播上游 context，修复超时控制

### 变更
- 标准库 HTTP Transport 禁用自动解压（`DisableCompression: true`），由 HttpCompression 中间件统一管理
- **Go 命名规范修复** — 消除 13 处包名前缀冗余，影响 34 个文件：
  - `spider.SpiderOutput` → `spider.Output`
  - `spider.SpiderSettings` → `spider.Settings`
  - `spider.BaseSpider` → `spider.Base`
  - `signal.SignalManager` → `signal.Manager`
  - `signal.NewSignalManager` → `signal.NewManager`
  - `pipeline.PipelineEntry` → `pipeline.Entry`
  - `middleware.MiddlewareEntry` → `middleware.Entry`（downloader/spider 两个包）
  - `stats.StatsCollector` → `stats.Collector`
  - `stats.MemoryStatsCollector` → `stats.MemoryCollector`
  - `stats.DummyStatsCollector` → `stats.DummyCollector`
  - `selector.SelectorList` → `selector.List`

### 依赖
- 新增 `github.com/PuerkitoBio/goquery` v1.12.0 — CSS 选择器
- 新增 `github.com/antchfx/htmlquery` v1.3.6 — XPath 查询
- 新增 `golang.org/x/net` v0.53.0 — HTML 解析

### 质量
- 测试总数：300 个（含 8 个端到端集成测试）
- Selector 包覆盖率：98.0%
- 中间件包覆盖率：86.4%
- 竞态检测：全部通过
- `go vet`：全部通过

---

## [v0.1.0] - 2026-04-24

### 新增
- 核心框架 MVP 版本
- Engine 调度引擎
- Scheduler 调度器（优先级队列 + 去重过滤）
- Downloader 下载器（并发控制 + Slot 管理）
- Scraper 处理器
- Item Pipeline 管理器
- Signal 信号系统
- Stats 统计收集器
- 内置下载器中间件：DefaultHeaders、UserAgent、Retry、Redirect
- Spider 中间件框架
- Settings 配置系统（优先级覆盖）
- 示例爬虫：quotes、books_json、custom_middleware
