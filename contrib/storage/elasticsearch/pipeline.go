package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"

	"github.com/dplcz/scrapy-go/contrib/storage"
)

// Writer 是基于 Elasticsearch 的 StorageWriter 实现。
//
// 支持 Bulk API 批量写入，自动索引映射。
// 使用 go-elasticsearch/v8 官方驱动。
type Writer struct {
	client *elasticsearch.Client
	opts   *Options
}

// Options 是 Elasticsearch Writer 的配置选项。
type Options struct {
	// Addresses 是 Elasticsearch 节点地址列表。
	// 默认值: ["http://localhost:9200"]
	Addresses []string

	// Index 是目标索引名称。
	Index string

	// Username 是认证用户名（可选）。
	Username string

	// Password 是认证密码（可选）。
	Password string

	// DocumentIDField 指定用作文档 _id 的字段名。
	// 若为空，则由 Elasticsearch 自动生成 _id。
	DocumentIDField string

	// Refresh 控制写入后是否立即刷新索引。
	// 可选值: "true", "false", "wait_for"
	// 默认值: "" (使用 ES 默认行为)
	Refresh string
}

// DefaultOptions 返回默认的 Elasticsearch 配置。
func DefaultOptions() *Options {
	return &Options{
		Addresses: []string{"http://localhost:9200"},
	}
}

// NewWriter 创建一个新的 Elasticsearch Writer。
func NewWriter(opts *Options) (*Writer, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	if len(opts.Addresses) == 0 {
		return nil, fmt.Errorf("elasticsearch: Addresses 不能为空")
	}
	if opts.Index == "" {
		return nil, fmt.Errorf("elasticsearch: Index 不能为空")
	}
	return &Writer{opts: opts}, nil
}

// Connect 建立 Elasticsearch 连接。
func (w *Writer) Connect(ctx context.Context) error {
	cfg := elasticsearch.Config{
		Addresses: w.opts.Addresses,
		Username:  w.opts.Username,
		Password:  w.opts.Password,
	}

	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("elasticsearch: 创建客户端失败: %w", err)
	}

	// 验证连接
	res, err := client.Info(client.Info.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("elasticsearch: 连接验证失败: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch: 连接验证返回错误: %s", res.Status())
	}

	w.client = client
	return nil
}

// Close 关闭 Elasticsearch 连接。
// go-elasticsearch 客户端无需显式关闭。
func (w *Writer) Close(_ context.Context) error {
	return nil
}

// WriteBatch 使用 Bulk API 批量写入多条记录到 Elasticsearch。
func (w *Writer) WriteBatch(ctx context.Context, items []map[string]any) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	body, err := w.buildBulkBody("index", "", items)
	if err != nil {
		return 0, fmt.Errorf("elasticsearch: 构建 Bulk 请求体失败: %w", err)
	}

	return w.executeBulk(ctx, body)
}

// UpsertBatch 使用 Bulk API 批量执行 Upsert 操作。
// 使用 update action 的 doc_as_upsert 模式。
func (w *Writer) UpsertBatch(ctx context.Context, uniqueKey string, items []map[string]any) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	body, err := w.buildBulkBody("update", uniqueKey, items)
	if err != nil {
		return 0, fmt.Errorf("elasticsearch: 构建 Bulk upsert 请求体失败: %w", err)
	}

	return w.executeBulk(ctx, body)
}

// buildBulkBody 构建 Bulk API 请求体。
// action 为 "index" 或 "update"。
func (w *Writer) buildBulkBody(action string, uniqueKey string, items []map[string]any) (*bytes.Buffer, error) {
	var buf bytes.Buffer

	for _, item := range items {
		// 确定文档 ID
		var docID string
		if uniqueKey != "" {
			if v, ok := item[uniqueKey]; ok {
				docID = fmt.Sprintf("%v", v)
			}
		} else if w.opts.DocumentIDField != "" {
			if v, ok := item[w.opts.DocumentIDField]; ok {
				docID = fmt.Sprintf("%v", v)
			}
		}

		// 构建 action 行
		meta := map[string]any{
			"_index": w.opts.Index,
		}
		if docID != "" {
			meta["_id"] = docID
		}

		actionLine := map[string]any{action: meta}
		if err := json.NewEncoder(&buf).Encode(actionLine); err != nil {
			return nil, err
		}

		// 构建数据行
		if action == "update" {
			// update 模式使用 doc_as_upsert
			docLine := map[string]any{
				"doc":           item,
				"doc_as_upsert": true,
			}
			if err := json.NewEncoder(&buf).Encode(docLine); err != nil {
				return nil, err
			}
		} else {
			// index 模式直接写入文档
			if err := json.NewEncoder(&buf).Encode(item); err != nil {
				return nil, err
			}
		}
	}

	return &buf, nil
}

// executeBulk 执行 Bulk API 请求并解析响应。
func (w *Writer) executeBulk(ctx context.Context, body *bytes.Buffer) (int, error) {
	bulkOpts := []func(*esapi.BulkRequest){
		w.client.Bulk.WithContext(ctx),
		w.client.Bulk.WithIndex(w.opts.Index),
	}
	if w.opts.Refresh != "" {
		bulkOpts = append(bulkOpts, w.client.Bulk.WithRefresh(w.opts.Refresh))
	}

	res, err := w.client.Bulk(body, bulkOpts...)
	if err != nil {
		return 0, fmt.Errorf("elasticsearch: Bulk 请求失败: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return 0, fmt.Errorf("elasticsearch: Bulk 请求返回错误: %s", res.Status())
	}

	// 解析响应
	var bulkResp bulkResponse
	if err := json.NewDecoder(res.Body).Decode(&bulkResp); err != nil {
		return 0, fmt.Errorf("elasticsearch: 解析 Bulk 响应失败: %w", err)
	}

	if bulkResp.Errors {
		// 统计失败数量
		var errMsgs []string
		successCount := 0
		for _, item := range bulkResp.Items {
			for _, result := range item {
				if result.Error != nil {
					errMsgs = append(errMsgs, fmt.Sprintf("[%d] %s: %s",
						result.Status, result.Error.Type, result.Error.Reason))
				} else {
					successCount++
				}
			}
		}
		return successCount, fmt.Errorf("elasticsearch: Bulk 部分失败 (%d 条错误): %s",
			len(errMsgs), strings.Join(errMsgs, "; "))
	}

	return len(bulkResp.Items), nil
}

// bulkResponse 是 Elasticsearch Bulk API 的响应结构。
type bulkResponse struct {
	Errors bool                          `json:"errors"`
	Items  []map[string]bulkItemResponse `json:"items"`
}

type bulkItemResponse struct {
	Index  string         `json:"_index"`
	ID     string         `json:"_id"`
	Status int            `json:"status"`
	Error  *bulkItemError `json:"error,omitempty"`
}

type bulkItemError struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// 编译期接口满足性检查
var (
	_ storage.StorageWriter = (*Writer)(nil)
	_ storage.UpsertWriter  = (*Writer)(nil)
)

// ============================================================================
// Pipeline
// ============================================================================

// Pipeline 是基于 Elasticsearch 的 Item Pipeline。
//
// 实现 pipeline.ItemPipeline 接口，可直接注册到 crawler。
// 内部使用 BasePipeline 提供批量缓冲逻辑。
//
// 用法：
//
//	p, err := elasticsearch.NewPipeline(
//	    elasticsearch.WithAddresses([]string{"http://localhost:9200"}),
//	    elasticsearch.WithIndex("scrapy-items"),
//	    elasticsearch.WithBatchSize(100),
//	)
//	c.AddPipeline(p, "es_storage", 900)
type Pipeline struct {
	*storage.BasePipeline
}

// PipelineOption 是 Pipeline 的可选配置函数。
type PipelineOption func(*pipelineConfig)

type pipelineConfig struct {
	esOpts    *Options
	batchSize int
	upsertKey string
	converter storage.ItemConverter
	logger    *slog.Logger
}

// WithAddresses 设置 Elasticsearch 节点地址列表。
func WithAddresses(addrs []string) PipelineOption {
	return func(c *pipelineConfig) {
		c.esOpts.Addresses = addrs
	}
}

// WithIndex 设置目标索引名称。
func WithIndex(index string) PipelineOption {
	return func(c *pipelineConfig) {
		c.esOpts.Index = index
	}
}

// WithUsername 设置认证用户名。
func WithUsername(username string) PipelineOption {
	return func(c *pipelineConfig) {
		c.esOpts.Username = username
	}
}

// WithPassword 设置认证密码。
func WithPassword(password string) PipelineOption {
	return func(c *pipelineConfig) {
		c.esOpts.Password = password
	}
}

// WithDocumentIDField 设置用作文档 _id 的字段名。
func WithDocumentIDField(field string) PipelineOption {
	return func(c *pipelineConfig) {
		c.esOpts.DocumentIDField = field
	}
}

// WithRefresh 设置写入后的刷新策略。
func WithRefresh(refresh string) PipelineOption {
	return func(c *pipelineConfig) {
		c.esOpts.Refresh = refresh
	}
}

// WithBatchSize 设置批量写入大小。
func WithBatchSize(size int) PipelineOption {
	return func(c *pipelineConfig) {
		c.batchSize = size
	}
}

// WithUpsertKey 设置 Upsert 唯一键字段名。
// 设置后启用 Upsert 模式，使用 update action 的 doc_as_upsert。
func WithUpsertKey(key string) PipelineOption {
	return func(c *pipelineConfig) {
		c.upsertKey = key
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

// NewPipeline 创建一个新的 Elasticsearch Pipeline。
func NewPipeline(opts ...PipelineOption) (*Pipeline, error) {
	cfg := &pipelineConfig{
		esOpts:    DefaultOptions(),
		batchSize: 100,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	writer, err := NewWriter(cfg.esOpts)
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
