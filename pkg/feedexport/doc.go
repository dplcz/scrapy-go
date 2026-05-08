// Package feedexport 实现了 scrapy-go 框架的数据导出（Feed Export）系统。
//
// # 概述
//
// feedexport 包将爬取的 Item 数据导出为多种格式（JSON、JSON Lines、CSV、XML），
// 并支持多种存储后端（本地文件、标准输出）。
// 对应 Scrapy Python 版本中 scrapy.extensions.feedexport 和 scrapy.exporters 模块的功能。
//
// # 核心类型
//
// 本包提供以下核心类型：
//   - [ItemExporter]：序列化器接口，将 Item 编码为指定格式的字节流
//   - [FeedStorage]：存储后端接口，管理导出文件的打开、写入和持久化
//   - [FeedSlot]：组合 Exporter 与 Storage，代表一个正在进行中的导出任务
//   - [FeedConfig]：Feed 导出配置（格式、路径、字段白名单等）
//   - [ExporterOptions]：Exporter 的通用配置
//   - [ItemFilterFunc]：Item 过滤函数类型
//
// # 内置 Exporter
//
// 本包提供以下内置序列化器：
//
//	┌────────────────────────────────────────────────────────┐
//	│  格式         │  类型              │  说明              │
//	├────────────────────────────────────────────────────────┤
//	│  JSON         │  JSONExporter      │  JSON 数组格式     │
//	│  JSON Lines   │  JSONLinesExporter │  每行一个 JSON     │
//	│  CSV          │  CSVExporter       │  逗号分隔值        │
//	│  XML          │  XMLExporter       │  XML 文档格式      │
//	└────────────────────────────────────────────────────────┘
//
// # 内置 Storage
//
//   - [FileStorage]：本地文件存储（支持路径模板变量）
//   - [StdoutStorage]：标准输出存储（用于调试和管道输出）
//
// # 使用方式
//
// 通过 Crawler 注册 Feed 导出：
//
//	c := crawler.New()
//	c.AddFeed(feedexport.FeedConfig{
//	    URI:    "output/items.json",
//	    Format: feedexport.FormatJSON,
//	    Fields: []string{"title", "price", "url"},
//	})
//
// 多格式同时导出：
//
//	c.AddFeed(feedexport.FeedConfig{URI: "items.jsonl", Format: feedexport.FormatJSONLines})
//	c.AddFeed(feedexport.FeedConfig{URI: "items.csv", Format: feedexport.FormatCSV})
//
// 使用标准输出（调试）：
//
//	c.AddFeed(feedexport.FeedConfig{URI: "stdout:", Format: feedexport.FormatJSON})
//
// # Exporter 生命周期
//
// [ItemExporter] 的调用顺序：
//  1. StartExporting — 开始导出（写入格式前缀，如 JSON 的 "["）
//  2. ExportItem — 反复调用，每次序列化一个 Item
//  3. FinishExporting — 结束导出（写入格式后缀，如 JSON 的 "]"）
//
// # FeedSlot 工作流
//
// [FeedSlot] 组合 Exporter 和 Storage 的完整工作流：
//  1. Open：通过 Storage.Open 获取 io.WriteCloser
//  2. 创建 Exporter 并调用 StartExporting
//  3. 每个 Item 通过 ItemFilterFunc 过滤后调用 ExportItem
//  4. Close：调用 FinishExporting，然后通过 Storage.Store 持久化
//
// # Item 序列化
//
// 所有 Exporter 内部使用 [item.Adapt] 将 Item 转为统一的字段访问接口，
// 然后根据 ExporterOptions.FieldsToExport 决定导出哪些字段。
// 这使得 Exporter 可以无差别地处理 map 和 struct 类型的 Item。
//
// # 与 Scrapy 的差异
//
//   - 舍弃 S3/GCS/FTP 等远程存储后端，仅保留本地文件和标准输出
//   - 舍弃 PostProcessingManager（gzip/zstd 压缩），可通过 io.Writer 包装实现
//   - 舍弃 ItemFilter 的动态加载机制，改为 [ItemFilterFunc] 函数类型
//   - 基于 io.Writer 而非 Python 的文件句柄抽象
//   - 使用 [item.Adapt] 统一 Item 访问，替代 Python 的 ItemAdapter
//   - Format 使用字符串常量而非类引用
//   - [NormalizeFormat] 兼容常见别名（"jl"/"jsonl"/"jsonlines"）
package feedexport
