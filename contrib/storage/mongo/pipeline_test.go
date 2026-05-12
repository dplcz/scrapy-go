package mongo

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
			name:    "nil 选项使用默认值但缺少 Database",
			opts:    nil,
			wantErr: true,
		},
		{
			name: "缺少 URI",
			opts: &Options{
				URI:        "",
				Database:   "test",
				Collection: "items",
			},
			wantErr: true,
		},
		{
			name: "缺少 Database",
			opts: &Options{
				URI:        "mongodb://localhost:27017",
				Database:   "",
				Collection: "items",
			},
			wantErr: true,
		},
		{
			name: "缺少 Collection",
			opts: &Options{
				URI:        "mongodb://localhost:27017",
				Database:   "test",
				Collection: "",
			},
			wantErr: true,
		},
		{
			name: "完整配置",
			opts: &Options{
				URI:        "mongodb://localhost:27017",
				Database:   "test",
				Collection: "items",
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
			name:    "缺少 Database 和 Collection",
			opts:    nil,
			wantErr: true,
		},
		{
			name: "缺少 Collection",
			opts: []PipelineOption{
				WithDatabase("test"),
			},
			wantErr: true,
		},
		{
			name: "完整配置",
			opts: []PipelineOption{
				WithDatabase("test"),
				WithCollection("items"),
				WithBatchSize(50),
			},
			wantErr: false,
		},
		{
			name: "带 Upsert 配置",
			opts: []PipelineOption{
				WithDatabase("test"),
				WithCollection("items"),
				WithUpsertKey("url"),
				WithOrdered(true),
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
	if opts.URI != "mongodb://localhost:27017" {
		t.Errorf("默认 URI 不正确: %s", opts.URI)
	}
	if opts.Ordered != false {
		t.Error("默认 Ordered 应为 false")
	}
}

func TestPipelineOptions(t *testing.T) {
	cfg := &pipelineConfig{
		mongoOpts: DefaultOptions(),
		batchSize: 100,
	}

	WithURI("mongodb://custom:27017")(cfg)
	WithDatabase("mydb")(cfg)
	WithCollection("mycol")(cfg)
	WithBatchSize(50)(cfg)
	WithUpsertKey("id")(cfg)
	WithOrdered(true)(cfg)

	if cfg.mongoOpts.URI != "mongodb://custom:27017" {
		t.Errorf("URI 不正确: %s", cfg.mongoOpts.URI)
	}
	if cfg.mongoOpts.Database != "mydb" {
		t.Errorf("Database 不正确: %s", cfg.mongoOpts.Database)
	}
	if cfg.mongoOpts.Collection != "mycol" {
		t.Errorf("Collection 不正确: %s", cfg.mongoOpts.Collection)
	}
	if cfg.batchSize != 50 {
		t.Errorf("BatchSize 不正确: %d", cfg.batchSize)
	}
	if cfg.upsertKey != "id" {
		t.Errorf("UpsertKey 不正确: %s", cfg.upsertKey)
	}
	if !cfg.mongoOpts.Ordered {
		t.Error("Ordered 应为 true")
	}
}
