package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dplcz/scrapy-go/contrib/storage"
)

// Writer 是基于 PostgreSQL 的 StorageWriter 实现。
//
// 支持批量 INSERT 和基于唯一键的 INSERT ... ON CONFLICT ... DO UPDATE（Upsert）。
// 使用 pgx/v5 驱动的连接池。
type Writer struct {
	pool *pgxpool.Pool
	opts *Options
}

// Options 是 PostgreSQL Writer 的配置选项。
type Options struct {
	// DSN 是 PostgreSQL 连接字符串。
	// 格式: "postgres://user:password@host:port/dbname?sslmode=disable"
	DSN string

	// Table 是目标表名。
	Table string

	// Columns 指定要写入的列名列表。
	// 若为空，则从第一批数据的 map key 自动推断。
	Columns []string

	// MaxPoolSize 是连接池最大连接数。
	// 默认值: 4
	MaxPoolSize int
}

// DefaultOptions 返回默认的 PostgreSQL 配置。
func DefaultOptions() *Options {
	return &Options{
		MaxPoolSize: 4,
	}
}

// NewWriter 创建一个新的 PostgreSQL Writer。
func NewWriter(opts *Options) (*Writer, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.DSN == "" {
		return nil, fmt.Errorf("postgres: DSN 不能为空")
	}
	if opts.Table == "" {
		return nil, fmt.Errorf("postgres: Table 不能为空")
	}
	return &Writer{opts: opts}, nil
}

// Connect 建立 PostgreSQL 连接池。
func (w *Writer) Connect(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(w.opts.DSN)
	if err != nil {
		return fmt.Errorf("postgres: 解析 DSN 失败: %w", err)
	}
	if w.opts.MaxPoolSize > 0 {
		cfg.MaxConns = int32(w.opts.MaxPoolSize)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("postgres: 创建连接池失败: %w", err)
	}

	// 验证连接
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("postgres: ping 失败: %w", err)
	}

	w.pool = pool
	return nil
}

// Close 关闭 PostgreSQL 连接池。
func (w *Writer) Close(_ context.Context) error {
	if w.pool != nil {
		w.pool.Close()
	}
	return nil
}

// WriteBatch 批量插入多条记录到 PostgreSQL。
// 使用 pgx.CopyFrom 实现高效批量插入。
func (w *Writer) WriteBatch(ctx context.Context, items []map[string]any) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	columns := w.resolveColumns(items[0])
	if len(columns) == 0 {
		return 0, fmt.Errorf("postgres: 无法推断列名")
	}

	rows := make([][]any, len(items))
	for i, item := range items {
		row := make([]any, len(columns))
		for j, col := range columns {
			row[j] = item[col]
		}
		rows[i] = row
	}

	copyCount, err := w.pool.CopyFrom(
		ctx,
		pgx.Identifier{w.opts.Table},
		columns,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return 0, fmt.Errorf("postgres: CopyFrom 失败: %w", err)
	}

	return int(copyCount), nil
}

// UpsertBatch 批量执行 Upsert 操作。
// 使用 INSERT ... ON CONFLICT (uniqueKey) DO UPDATE SET ... 语法。
func (w *Writer) UpsertBatch(ctx context.Context, uniqueKey string, items []map[string]any) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	columns := w.resolveColumns(items[0])
	if len(columns) == 0 {
		return 0, fmt.Errorf("postgres: 无法推断列名")
	}

	query := w.buildUpsertQuery(columns, uniqueKey, len(items))

	args := make([]any, 0, len(items)*len(columns))
	for _, item := range items {
		for _, col := range columns {
			args = append(args, item[col])
		}
	}

	batch := &pgx.Batch{}
	batch.Queue(query, args...)

	br := w.pool.SendBatch(ctx, batch)
	defer br.Close()

	tag, err := br.Exec()
	if err != nil {
		return 0, fmt.Errorf("postgres: upsert 失败: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

// resolveColumns 解析列名列表。
// 优先使用配置中指定的列名，否则从 map key 自动推断。
func (w *Writer) resolveColumns(sample map[string]any) []string {
	if len(w.opts.Columns) > 0 {
		return w.opts.Columns
	}
	columns := make([]string, 0, len(sample))
	for k := range sample {
		columns = append(columns, k)
	}
	// 排序保证列顺序稳定
	sort.Strings(columns)
	return columns
}

// buildUpsertQuery 构建 INSERT ... ON CONFLICT ... DO UPDATE 语句。
func (w *Writer) buildUpsertQuery(columns []string, uniqueKey string, rowCount int) string {
	var b strings.Builder

	// INSERT INTO table (col1, col2, ...) VALUES
	b.WriteString("INSERT INTO ")
	b.WriteString(w.opts.Table)
	b.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(col)
	}
	b.WriteString(") VALUES ")

	// ($1, $2, ...), ($3, $4, ...), ...
	paramIdx := 1
	for row := 0; row < rowCount; row++ {
		if row > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for col := 0; col < len(columns); col++ {
			if col > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("$%d", paramIdx))
			paramIdx++
		}
		b.WriteString(")")
	}

	// ON CONFLICT (uniqueKey) DO UPDATE SET col1 = EXCLUDED.col1, ...
	b.WriteString(" ON CONFLICT (")
	b.WriteString(uniqueKey)
	b.WriteString(") DO UPDATE SET ")

	first := true
	for _, col := range columns {
		if col == uniqueKey {
			continue
		}
		if !first {
			b.WriteString(", ")
		}
		b.WriteString(col)
		b.WriteString(" = EXCLUDED.")
		b.WriteString(col)
		first = false
	}

	return b.String()
}

// 编译期接口满足性检查
var (
	_ storage.StorageWriter = (*Writer)(nil)
	_ storage.UpsertWriter  = (*Writer)(nil)
)

// ============================================================================
// Pipeline
// ============================================================================

// Pipeline 是基于 PostgreSQL 的 Item Pipeline。
//
// 实现 pipeline.ItemPipeline 接口，可直接注册到 crawler。
// 内部使用 BasePipeline 提供批量缓冲逻辑。
//
// 用法：
//
//	p, err := postgres.NewPipeline(
//	    postgres.WithDSN("postgres://user:pass@localhost:5432/scrapy?sslmode=disable"),
//	    postgres.WithTable("items"),
//	    postgres.WithBatchSize(100),
//	)
//	c.AddPipeline(p, "postgres_storage", 900)
type Pipeline struct {
	*storage.BasePipeline
}

// PipelineOption 是 Pipeline 的可选配置函数。
type PipelineOption func(*pipelineConfig)

type pipelineConfig struct {
	pgOpts    *Options
	batchSize int
	upsertKey string
	converter storage.ItemConverter
	logger    *slog.Logger
}

// WithDSN 设置 PostgreSQL 连接字符串。
func WithDSN(dsn string) PipelineOption {
	return func(c *pipelineConfig) {
		c.pgOpts.DSN = dsn
	}
}

// WithTable 设置目标表名。
func WithTable(table string) PipelineOption {
	return func(c *pipelineConfig) {
		c.pgOpts.Table = table
	}
}

// WithColumns 设置要写入的列名列表。
func WithColumns(columns []string) PipelineOption {
	return func(c *pipelineConfig) {
		c.pgOpts.Columns = columns
	}
}

// WithMaxPoolSize 设置连接池最大连接数。
func WithMaxPoolSize(size int) PipelineOption {
	return func(c *pipelineConfig) {
		c.pgOpts.MaxPoolSize = size
	}
}

// WithBatchSize 设置批量写入大小。
func WithBatchSize(size int) PipelineOption {
	return func(c *pipelineConfig) {
		c.batchSize = size
	}
}

// WithUpsertKey 设置 Upsert 唯一键字段名。
// 设置后启用 Upsert 模式，使用 INSERT ... ON CONFLICT ... DO UPDATE。
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

// NewPipeline 创建一个新的 PostgreSQL Pipeline。
func NewPipeline(opts ...PipelineOption) (*Pipeline, error) {
	cfg := &pipelineConfig{
		pgOpts:    DefaultOptions(),
		batchSize: 100,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	writer, err := NewWriter(cfg.pgOpts)
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
