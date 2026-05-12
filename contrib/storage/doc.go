// Package storage 提供 scrapy-go 框架的通用持久化存储适配器。
//
// 本包是 scrapy-go 框架的可插拔扩展模块（contrib），作为独立的 Go 子模块发布，
// 主模块 go.mod 不引入 MongoDB / PostgreSQL / Elasticsearch 相关依赖，
// 实现零侵入的可插拔设计。
//
// # 核心组件
//
//   - StorageWriter：通用存储写入接口，定义批量写入和 Upsert 语义
//   - MongoPipeline：MongoDB 持久化 Pipeline，基于批量 InsertMany + Upsert
//   - PostgresPipeline：PostgreSQL 持久化 Pipeline，基于批量 INSERT + ON CONFLICT upsert
//   - ElasticsearchPipeline：Elasticsearch 持久化 Pipeline，基于 Bulk API 批量写入
//
// # 使用方式
//
// 通过 crawler.AddPipeline 注入存储 Pipeline：
//
//	import (
//	    "github.com/dplcz/scrapy-go/contrib/storage/mongo"
//	    "github.com/dplcz/scrapy-go/pkg/crawler"
//	)
//
//	mongoPipeline, err := mongo.NewPipeline(
//	    mongo.WithURI("mongodb://localhost:27017"),
//	    mongo.WithDatabase("scrapy"),
//	    mongo.WithCollection("items"),
//	    mongo.WithBatchSize(100),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	c := crawler.NewDefault()
//	c.AddPipeline(mongoPipeline, "mongo_storage", 900)
//	c.Run(ctx, mySpider)
//
// # 设计决策
//
//   - 适配器模式：每个存储后端实现 pipeline.ItemPipeline 接口，可直接注册到框架
//   - 批量写入：所有适配器支持可配置的批量大小，减少网络往返
//   - Upsert 语义：支持基于唯一键的插入或更新，适用于增量爬取场景
//   - ItemAdapter 集成：通过 item.Adapt 统一处理 struct / map 类型的 Item
//   - 独立子模块：避免主模块引入重量级数据库驱动依赖，用户按需安装
package storage
