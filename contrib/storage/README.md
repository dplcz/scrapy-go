# 📦 scrapy-go/contrib/storage — 通用持久化存储适配器

[![Go Version](https://img.shields.io/badge/Go-1.25.1+-00ADD8?style=flat&logo=go)](https://go.dev/)

`contrib/storage` 是 scrapy-go 框架的通用持久化存储扩展模块，提供 MongoDB、PostgreSQL、Elasticsearch 三种存储后端的 Pipeline 适配器。

## ✨ 特性

- 🔌 **可插拔设计** — 独立 Go 子模块，主模块不引入数据库驱动依赖
- 📦 **批量写入** — 可配置的批量缓冲大小，减少网络往返，提升吞吐量
- 🔄 **Upsert 支持** — 基于唯一键的插入或更新，适用于增量爬取场景
- 🧩 **ItemAdapter 集成** — 自动适配 struct / map 类型的 Item
- 🔧 **自定义转换** — 支持自定义 Item 到 map 的转换函数
- 🛡️ **并发安全** — 内部缓冲区通过 mutex 保护，支持 CONCURRENT_ITEMS 并发

## 📥 安装

```bash
go get github.com/dplcz/scrapy-go/contrib/storage
```

## 🚀 快速开始

### MongoDB

```go
import (
    "github.com/dplcz/scrapy-go/contrib/storage/mongo"
    "github.com/dplcz/scrapy-go/pkg/crawler"
)

p, err := mongo.NewPipeline(
    mongo.WithURI("mongodb://localhost:27017"),
    mongo.WithDatabase("scrapy"),
    mongo.WithCollection("items"),
    mongo.WithBatchSize(100),
)
if err != nil {
    log.Fatal(err)
}

c := crawler.NewDefault()
c.AddPipeline(p, "mongo_storage", 900)
c.Run(ctx, mySpider)
```

### PostgreSQL

```go
import (
    "github.com/dplcz/scrapy-go/contrib/storage/postgres"
    "github.com/dplcz/scrapy-go/pkg/crawler"
)

p, err := postgres.NewPipeline(
    postgres.WithDSN("postgres://user:pass@localhost:5432/scrapy?sslmode=disable"),
    postgres.WithTable("items"),
    postgres.WithBatchSize(100),
)
if err != nil {
    log.Fatal(err)
}

c := crawler.NewDefault()
c.AddPipeline(p, "postgres_storage", 900)
c.Run(ctx, mySpider)
```

### Elasticsearch

```go
import (
    "github.com/dplcz/scrapy-go/contrib/storage/elasticsearch"
    "github.com/dplcz/scrapy-go/pkg/crawler"
)

p, err := elasticsearch.NewPipeline(
    elasticsearch.WithAddresses([]string{"http://localhost:9200"}),
    elasticsearch.WithIndex("scrapy-items"),
    elasticsearch.WithBatchSize(100),
)
if err != nil {
    log.Fatal(err)
}

c := crawler.NewDefault()
c.AddPipeline(p, "es_storage", 900)
c.Run(ctx, mySpider)
```

## 🔄 Upsert 模式

所有适配器均支持 Upsert 模式，通过 `WithUpsertKey` 指定唯一键字段：

### MongoDB Upsert

```go
p, _ := mongo.NewPipeline(
    mongo.WithURI("mongodb://localhost:27017"),
    mongo.WithDatabase("scrapy"),
    mongo.WithCollection("items"),
    mongo.WithUpsertKey("url"),  // 基于 url 字段判断唯一性
)
```

内部使用 `BulkWrite` 的 `UpdateOne` with `upsert: true`。

### PostgreSQL Upsert

```go
p, _ := postgres.NewPipeline(
    postgres.WithDSN("postgres://user:pass@localhost:5432/scrapy?sslmode=disable"),
    postgres.WithTable("items"),
    postgres.WithUpsertKey("url"),  // INSERT ... ON CONFLICT (url) DO UPDATE SET ...
)
```

内部使用 `INSERT ... ON CONFLICT (uniqueKey) DO UPDATE SET ...` 语法。

### Elasticsearch Upsert

```go
p, _ := elasticsearch.NewPipeline(
    elasticsearch.WithIndex("scrapy-items"),
    elasticsearch.WithUpsertKey("url"),  // 使用 url 字段值作为文档 _id
)
```

内部使用 Bulk API 的 `update` action 配合 `doc_as_upsert: true`。

## 🔧 自定义 Item 转换

默认使用 `item.Adapt(item).AsMap()` 将 Item 转换为 `map[string]any`。可通过 `WithConverter` 自定义转换逻辑：

```go
converter := func(item any) (map[string]any, error) {
    book, ok := item.(*Book)
    if !ok {
        return nil, fmt.Errorf("unexpected type: %T", item)
    }
    return map[string]any{
        "title":      book.Title,
        "price":      book.Price,
        "crawled_at": time.Now(),
    }, nil
}

p, _ := mongo.NewPipeline(
    mongo.WithDatabase("scrapy"),
    mongo.WithCollection("books"),
    mongo.WithConverter(converter),
)
```

## ⚙️ 配置参考

### MongoDB 选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `WithURI` | `mongodb://localhost:27017` | MongoDB 连接字符串 |
| `WithDatabase` | — (必填) | 目标数据库名称 |
| `WithCollection` | — (必填) | 目标集合名称 |
| `WithBatchSize` | `100` | 批量写入大小 |
| `WithUpsertKey` | — | Upsert 唯一键字段名 |
| `WithOrdered` | `false` | 批量操作是否有序 |

### PostgreSQL 选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `WithDSN` | — (必填) | PostgreSQL 连接字符串 |
| `WithTable` | — (必填) | 目标表名 |
| `WithColumns` | 自动推断 | 写入的列名列表 |
| `WithBatchSize` | `100` | 批量写入大小 |
| `WithUpsertKey` | — | Upsert 唯一键字段名 |
| `WithMaxPoolSize` | `4` | 连接池最大连接数 |

### Elasticsearch 选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `WithAddresses` | `["http://localhost:9200"]` | ES 节点地址列表 |
| `WithIndex` | — (必填) | 目标索引名称 |
| `WithBatchSize` | `100` | 批量写入大小 |
| `WithUpsertKey` | — | Upsert 唯一键字段名 |
| `WithDocumentIDField` | — | 用作文档 _id 的字段名 |
| `WithUsername` / `WithPassword` | — | 认证凭据 |
| `WithRefresh` | — | 写入后刷新策略 |

## 🏗️ 架构设计

```
contrib/storage/
├── interface.go          # StorageWriter / UpsertWriter / ItemConverter 接口定义
├── base.go               # BasePipeline 通用批量缓冲实现
├── doc.go                # 包文档
├── mongo/
│   └── pipeline.go       # MongoDB Writer + Pipeline（InsertMany + BulkWrite Upsert）
├── postgres/
│   └── pipeline.go       # PostgreSQL Writer + Pipeline（CopyFrom + ON CONFLICT Upsert）
└── elasticsearch/
    └── pipeline.go       # Elasticsearch Writer + Pipeline（Bulk API + doc_as_upsert）
```

### 核心接口

```go
// StorageWriter 通用存储写入接口
type StorageWriter interface {
    Connect(ctx context.Context) error
    Close(ctx context.Context) error
    WriteBatch(ctx context.Context, items []map[string]any) (int, error)
}

// UpsertWriter 扩展接口，支持 Upsert 操作
type UpsertWriter interface {
    StorageWriter
    UpsertBatch(ctx context.Context, uniqueKey string, items []map[string]any) (int, error)
}
```

## 📄 License

MIT
