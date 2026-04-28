# Changelog

本文件记录 scrapy-go 项目的所有重要变更。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

*暂无未发布的变更。*

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

- **示例 `examples/feedexport/main.go`**
  - 演示同一爬取任务同时输出 JSON / JSON Lines / CSV / XML 四种格式
  - 覆盖 `FieldsToExport`、URI 模板（`%(name)s`）、`Filter` 过滤、`StoreEmpty`

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
