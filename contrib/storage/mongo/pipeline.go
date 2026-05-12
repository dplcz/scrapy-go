package mongo

import (
	"context"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/dplcz/scrapy-go/contrib/storage"
)

// Writer 是基于 MongoDB 的 StorageWriter 实现。
//
// 支持批量 InsertMany 和基于唯一键的 Upsert 操作。
// 使用 mongo-driver/v2 官方驱动。
type Writer struct {
	client     *mongo.Client
	collection *mongo.Collection
	opts       *Options
}

// Options 是 MongoDB Writer 的配置选项。
type Options struct {
	// URI 是 MongoDB 连接字符串。
	// 默认值: "mongodb://localhost:27017"
	URI string

	// Database 是目标数据库名称。
	Database string

	// Collection 是目标集合名称。
	Collection string

	// Ordered 控制批量插入是否有序。
	// 设为 false 时，即使某条记录插入失败，其余记录仍会继续插入。
	// 默认值: false
	Ordered bool
}

// DefaultOptions 返回默认的 MongoDB 配置。
func DefaultOptions() *Options {
	return &Options{
		URI:     "mongodb://localhost:27017",
		Ordered: false,
	}
}

// NewWriter 创建一个新的 MongoDB Writer。
func NewWriter(opts *Options) (*Writer, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.URI == "" {
		return nil, fmt.Errorf("mongo: URI 不能为空")
	}
	if opts.Database == "" {
		return nil, fmt.Errorf("mongo: Database 不能为空")
	}
	if opts.Collection == "" {
		return nil, fmt.Errorf("mongo: Collection 不能为空")
	}
	return &Writer{opts: opts}, nil
}

// Connect 建立 MongoDB 连接。
func (w *Writer) Connect(ctx context.Context) error {
	client, err := mongo.Connect(options.Client().ApplyURI(w.opts.URI))
	if err != nil {
		return fmt.Errorf("mongo: 连接失败: %w", err)
	}

	// 验证连接
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("mongo: ping 失败: %w", err)
	}

	w.client = client
	w.collection = client.Database(w.opts.Database).Collection(w.opts.Collection)
	return nil
}

// Close 关闭 MongoDB 连接。
func (w *Writer) Close(ctx context.Context) error {
	if w.client == nil {
		return nil
	}
	return w.client.Disconnect(ctx)
}

// WriteBatch 批量插入多条记录到 MongoDB。
func (w *Writer) WriteBatch(ctx context.Context, items []map[string]any) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	docs := make([]any, len(items))
	for i, item := range items {
		docs[i] = item
	}

	ordered := w.opts.Ordered
	result, err := w.collection.InsertMany(ctx, docs, options.InsertMany().SetOrdered(ordered))
	if err != nil {
		return 0, fmt.Errorf("mongo: InsertMany 失败: %w", err)
	}

	return len(result.InsertedIDs), nil
}

// UpsertBatch 批量执行 Upsert 操作。
// 使用 BulkWrite 的 UpdateOne with upsert 实现。
func (w *Writer) UpsertBatch(ctx context.Context, uniqueKey string, items []map[string]any) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	models := make([]mongo.WriteModel, 0, len(items))
	for _, item := range items {
		keyVal, ok := item[uniqueKey]
		if !ok {
			continue
		}

		filter := bson.M{uniqueKey: keyVal}
		update := bson.M{"$set": item}

		model := mongo.NewUpdateOneModel().
			SetFilter(filter).
			SetUpdate(update).
			SetUpsert(true)
		models = append(models, model)
	}

	if len(models) == 0 {
		return 0, nil
	}

	ordered := w.opts.Ordered
	result, err := w.collection.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(ordered))
	if err != nil {
		return 0, fmt.Errorf("mongo: BulkWrite upsert 失败: %w", err)
	}

	return int(result.UpsertedCount + result.ModifiedCount), nil
}

// 编译期接口满足性检查
var (
	_ storage.StorageWriter = (*Writer)(nil)
	_ storage.UpsertWriter  = (*Writer)(nil)
)

// ============================================================================
// Pipeline
// ============================================================================

// Pipeline 是基于 MongoDB 的 Item Pipeline。
//
// 实现 pipeline.ItemPipeline 接口，可直接注册到 crawler。
// 内部使用 BasePipeline 提供批量缓冲逻辑。
//
// 用法：
//
//	p, err := mongo.NewPipeline(
//	    mongo.WithURI("mongodb://localhost:27017"),
//	    mongo.WithDatabase("scrapy"),
//	    mongo.WithCollection("items"),
//	    mongo.WithBatchSize(100),
//	)
//	c.AddPipeline(p, "mongo_storage", 900)
type Pipeline struct {
	*storage.BasePipeline
}

// PipelineOption 是 Pipeline 的可选配置函数。
type PipelineOption func(*pipelineConfig)

type pipelineConfig struct {
	mongoOpts *Options
	batchSize int
	upsertKey string
	converter storage.ItemConverter
	logger    *slog.Logger
}

// WithURI 设置 MongoDB 连接字符串。
func WithURI(uri string) PipelineOption {
	return func(c *pipelineConfig) {
		c.mongoOpts.URI = uri
	}
}

// WithDatabase 设置目标数据库名称。
func WithDatabase(db string) PipelineOption {
	return func(c *pipelineConfig) {
		c.mongoOpts.Database = db
	}
}

// WithCollection 设置目标集合名称。
func WithCollection(col string) PipelineOption {
	return func(c *pipelineConfig) {
		c.mongoOpts.Collection = col
	}
}

// WithBatchSize 设置批量写入大小。
func WithBatchSize(size int) PipelineOption {
	return func(c *pipelineConfig) {
		c.batchSize = size
	}
}

// WithUpsertKey 设置 Upsert 唯一键字段名。
// 设置后启用 Upsert 模式，基于该字段判断记录唯一性。
func WithUpsertKey(key string) PipelineOption {
	return func(c *pipelineConfig) {
		c.upsertKey = key
	}
}

// WithOrdered 设置批量操作是否有序。
func WithOrdered(ordered bool) PipelineOption {
	return func(c *pipelineConfig) {
		c.mongoOpts.Ordered = ordered
	}
}

// WithConverter 设置自定义 Item 转换函数。
func WithConverter(conv storage.ItemConverter) PipelineOption {
	return func(c *pipelineConfig) {
		c.converter = conv
	}
}

// WithLogger 设置日志器。
func WithLogger(logger *slog.Logger) PipelineOption {
	return func(c *pipelineConfig) {
		c.logger = logger
	}
}

// NewPipeline 创建一个新的 MongoDB Pipeline。
func NewPipeline(opts ...PipelineOption) (*Pipeline, error) {
	cfg := &pipelineConfig{
		mongoOpts: DefaultOptions(),
		batchSize: 100,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	writer, err := NewWriter(cfg.mongoOpts)
	if err != nil {
		return nil, err
	}

	base := storage.NewBasePipeline(storage.BasePipelineConfig{
		Writer:    writer,
		Converter: cfg.converter,
		Logger:    cfg.logger,
		BatchSize: cfg.batchSize,
		UpsertKey: cfg.upsertKey,
	})

	return &Pipeline{BasePipeline: base}, nil
}
