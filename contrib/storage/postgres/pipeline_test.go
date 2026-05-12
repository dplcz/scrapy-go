package postgres

import (
	"testing"
)

func TestNewWriter_Validation(t *testing.T) {
	tests := []struct {
		name    string
		opts    *Options
		wantErr bool
	}{
		{
			name:    "nil 选项使用默认值但缺少 DSN",
			opts:    nil,
			wantErr: true,
		},
		{
			name: "缺少 DSN",
			opts: &Options{
				DSN:   "",
				Table: "items",
			},
			wantErr: true,
		},
		{
			name: "缺少 Table",
			opts: &Options{
				DSN:   "postgres://localhost:5432/test",
				Table: "",
			},
			wantErr: true,
		},
		{
			name: "完整配置",
			opts: &Options{
				DSN:   "postgres://localhost:5432/test",
				Table: "items",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWriter(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewWriter() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewPipeline_Validation(t *testing.T) {
	tests := []struct {
		name    string
		opts    []PipelineOption
		wantErr bool
	}{
		{
			name:    "缺少 DSN 和 Table",
			opts:    nil,
			wantErr: true,
		},
		{
			name: "缺少 Table",
			opts: []PipelineOption{
				WithDSN("postgres://localhost:5432/test"),
			},
			wantErr: true,
		},
		{
			name: "完整配置",
			opts: []PipelineOption{
				WithDSN("postgres://localhost:5432/test"),
				WithTable("items"),
				WithBatchSize(50),
			},
			wantErr: false,
		},
		{
			name: "带 Upsert 和 Columns 配置",
			opts: []PipelineOption{
				WithDSN("postgres://localhost:5432/test"),
				WithTable("items"),
				WithColumns([]string{"id", "title", "url"}),
				WithUpsertKey("url"),
				WithMaxPoolSize(8),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPipeline(tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPipeline() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.MaxPoolSize != 4 {
		t.Errorf("默认 MaxPoolSize 不正确: %d", opts.MaxPoolSize)
	}
}

func TestPipelineOptions(t *testing.T) {
	cfg := &pipelineConfig{
		pgOpts:    DefaultOptions(),
		batchSize: 100,
	}

	WithDSN("postgres://custom:5432/mydb")(cfg)
	WithTable("mytable")(cfg)
	WithColumns([]string{"a", "b"})(cfg)
	WithMaxPoolSize(8)(cfg)
	WithBatchSize(50)(cfg)
	WithUpsertKey("id")(cfg)

	if cfg.pgOpts.DSN != "postgres://custom:5432/mydb" {
		t.Errorf("DSN 不正确: %s", cfg.pgOpts.DSN)
	}
	if cfg.pgOpts.Table != "mytable" {
		t.Errorf("Table 不正确: %s", cfg.pgOpts.Table)
	}
	if len(cfg.pgOpts.Columns) != 2 {
		t.Errorf("Columns 数量不正确: %d", len(cfg.pgOpts.Columns))
	}
	if cfg.pgOpts.MaxPoolSize != 8 {
		t.Errorf("MaxPoolSize 不正确: %d", cfg.pgOpts.MaxPoolSize)
	}
	if cfg.batchSize != 50 {
		t.Errorf("BatchSize 不正确: %d", cfg.batchSize)
	}
	if cfg.upsertKey != "id" {
		t.Errorf("UpsertKey 不正确: %s", cfg.upsertKey)
	}
}

func TestBuildUpsertQuery(t *testing.T) {
	w := &Writer{
		opts: &Options{
			Table: "items",
		},
	}

	query := w.buildUpsertQuery([]string{"id", "title", "url"}, "id", 2)

	expected := "INSERT INTO items (id, title, url) VALUES ($1, $2, $3), ($4, $5, $6) ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, url = EXCLUDED.url"
	if query != expected {
		t.Errorf("Upsert 查询不正确:\n  got:  %s\n  want: %s", query, expected)
	}
}

func TestBuildUpsertQuery_SingleRow(t *testing.T) {
	w := &Writer{
		opts: &Options{
			Table: "data",
		},
	}

	query := w.buildUpsertQuery([]string{"key", "value"}, "key", 1)

	expected := "INSERT INTO data (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value"
	if query != expected {
		t.Errorf("Upsert 查询不正确:\n  got:  %s\n  want: %s", query, expected)
	}
}

func TestResolveColumns_FromConfig(t *testing.T) {
	w := &Writer{
		opts: &Options{
			Columns: []string{"a", "b", "c"},
		},
	}

	cols := w.resolveColumns(map[string]any{"x": 1, "y": 2})
	if len(cols) != 3 || cols[0] != "a" {
		t.Errorf("应使用配置中的列名: %v", cols)
	}
}

func TestResolveColumns_FromSample(t *testing.T) {
	w := &Writer{
		opts: &Options{},
	}

	cols := w.resolveColumns(map[string]any{"b": 1, "a": 2, "c": 3})
	if len(cols) != 3 {
		t.Fatalf("列数不正确: %d", len(cols))
	}
	// 应按字典序排列
	if cols[0] != "a" || cols[1] != "b" || cols[2] != "c" {
		t.Errorf("列名应按字典序排列: %v", cols)
	}
}
