// Package settings 实现了 scrapy-go 框架的多优先级配置系统。
//
// # 概述
//
// settings 包提供了一个线程安全的配置管理器 [Settings]，支持六级优先级覆盖机制
// 和配置冻结功能。同时提供 TOML 文件加载器，支持从外部配置文件注入配置。
// 对应 Scrapy Python 版本中 scrapy.settings 模块的功能。
//
// # 架构定位
//
// Settings 是 scrapy-go 所有组件的配置中枢，贯穿框架的整个生命周期：
//
//	┌──────────────────────────────────────────────────────────┐
//	│                    配置来源                               │
//	├──────────┬──────────┬──────────┬──────────┬──────────────┤
//	│ Default  │  TOML    │ Project  │  Spider  │   Cmdline    │
//	│ (框架)   │  (文件)  │ (代码)   │ (Spider) │  (命令行)    │
//	└────┬─────┴────┬─────┴────┬─────┴────┬─────┴──────┬───────┘
//	     │          │          │          │            │
//	     ▼          ▼          ▼          ▼            ▼
//	┌──────────────────────────────────────────────────────────┐
//	│              Settings（多优先级合并）                      │
//	└──────────────────────────┬───────────────────────────────┘
//	                           │
//	     ┌─────────────────────┼─────────────────────┐
//	     ▼                     ▼                     ▼
//	┌─────────┐         ┌──────────┐         ┌──────────┐
//	│ Engine  │         │Downloader│         │ Pipeline │  ...
//	└─────────┘         └──────────┘         └──────────┘
//
// # 优先级体系
//
// 配置系统支持六级优先级，高优先级的配置会覆盖低优先级的同名配置：
//
//	PriorityDefault(0) < PriorityCommand(10) < PriorityAddon(15) < PriorityProject(20) < PrioritySpider(30) < PriorityCmdline(40)
//
// 各优先级的典型使用场景：
//   - [PriorityDefault]：框架内置默认值（如 CONCURRENT_REQUESTS=16）
//   - [PriorityCommand]：CLI 命令级别的默认值
//   - [PriorityAddon]：TOML 配置文件加载的值
//   - [PriorityProject]：用户项目代码中设置的值
//   - [PrioritySpider]：Spider.CustomSettings() 返回的值
//   - [PriorityCmdline]：命令行参数覆盖（最高优先级）
//
// # 配置冻结
//
// Settings 支持通过 [Settings.Freeze] 方法冻结配置。冻结后任何修改操作
// （Set、Update、Delete）都会返回错误。这确保了组件初始化完成后配置不会被意外修改。
//
// 典型流程：
//  1. 创建 Settings 并加载默认配置
//  2. 合并 TOML 文件、项目配置、Spider 配置
//  3. 调用 Freeze() 冻结
//  4. 将冻结的 Settings 传递给各组件
//
// # TOML 配置文件
//
// 通过 [Settings.LoadFromFile] 或 [Settings.AutoLoadConfig] 从 TOML 文件加载配置。
// TOML 键名使用小写下划线格式，加载时自动转换为大写格式：
//
//	# scrapy-go.toml
//	concurrent_requests = 32
//	download_timeout = "30s"
//	retry_times = 3
//
// 配置文件探测顺序：
//  1. SCRAPY_GO_CONFIG 环境变量指定的路径
//  2. 当前目录下的 scrapy-go.toml
//
// # 组件优先级字典
//
// 框架使用 map[string]int 类型的「组件优先级字典」管理中间件、Pipeline、扩展的启用和排序。
// [Settings.GetComponentPriorityDictWithBase] 方法合并 _BASE 后缀的基础配置和用户覆盖配置，
// 并过滤掉优先级 < 0 的条目（表示禁用该组件）。
//
// 示例：
//
//	// 框架默认（DOWNLOADER_MIDDLEWARES_BASE）
//	{"Retry": 550, "Redirect": 600, "Cookies": 700}
//
//	// 用户覆盖（DOWNLOADER_MIDDLEWARES）
//	{"Cookies": -1, "MyMiddleware": 800}
//
//	// 合并结果：Cookies 被禁用，MyMiddleware 被添加
//	{"Retry": 550, "Redirect": 600, "MyMiddleware": 800}
//
// # 类型安全的值获取
//
// Settings 提供一系列类型安全的 Get 方法，支持自动类型转换：
//   - [Settings.GetString]：获取字符串
//   - [Settings.GetInt] / [Settings.GetInt64]：获取整数
//   - [Settings.GetFloat]：获取浮点数
//   - [Settings.GetBool]：获取布尔值（支持 true/false、1/0、"yes"/"no"）
//   - [Settings.GetDuration]：获取时间间隔（支持 time.Duration、int 秒、"5s" 字符串）
//   - [Settings.GetStringSlice]：获取字符串切片
//   - [Settings.GetStringMap]：获取 map[string]any
//   - [Settings.GetIntMap]：获取 map[string]int（组件优先级字典）
//
// # 泛型类型安全 API（推荐）
//
// 自 v1.2.0 起，settings 包提供基于泛型的编译期类型安全 API（TD-004 偿还）：
//
//	// 使用类型化键常量，编译期确定返回类型
//	concurrency := settings.Get(s, settings.KeyConcurrentRequests) // 返回 int
//	botName := settings.Get(s, settings.KeyBotName)               // 返回 string
//	enabled := settings.Get(s, settings.KeyRetryEnabled)          // 返回 bool
//
//	// 设置配置（使用简洁的方法调用）
//	s.Set(settings.KeyConcurrentRequests.Name, 32, settings.PriorityProject)
//
//	// 必须存在的配置项（不存在时 panic）
//	timeout := settings.MustGet(s, settings.KeyDownloadTimeoutDuration)
//
// 泛型 API 的优势：
//   - 编译期类型检查：Get 返回类型由 Key[T] 的 T 参数确定
//   - 消除魔法字符串：所有配置键名集中定义为 Key[T] 常量
//   - 内置默认值：Key 中携带默认值，调用者无需重复指定
//   - Set 简洁直观：s.Set(key.Name, value, priority)
//   - 完全向后兼容：旧的 GetInt/GetString 等方法继续可用
//
// # 与 Scrapy 的差异
//
//   - 使用 sync.RWMutex 保证并发安全（Scrapy 依赖 GIL）
//   - 使用 TOML 替代 Python 模块作为配置文件格式
//   - 不支持从配置文件动态加载组件类（Go 静态编译限制）
//   - 使用 Go 的类型断言替代 Python 的 duck typing 进行类型转换
//   - 组件禁用使用 < 0 的优先级值（对应 Scrapy 中设置为 None）
//
// # 并发安全
//
// [Settings] 的所有公共方法均为并发安全，通过 sync.RWMutex 保护。
// 读操作（Get 系列）使用读锁，写操作（Set/Update/Delete）使用写锁。
package settings
