package elasticsearch

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewWriter_Validation(t *testing.T) {
	tests := []struct {
		name    string
		opts    *Options
		wantErr bool
	}{
		{
			name:    "nil 选项使用默认值但缺少 Index",
			opts:    nil,
			wantErr: true,
		},
		{
			name: "缺少 Addresses",
			opts: &Options{
				Addresses: nil,
				Index:     "items",
			},
			wantErr: true,
		},
		{
			name: "缺少 Index",
			opts: &Options{
				Addresses: []string{"http://localhost:9200"},
				Index:     "",
			},
			wantErr: true,
		},
		{
			name: "完整配置",
			opts: &Options{
				Addresses: []string{"http://localhost:9200"},
				Index:     "items",
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
			name:    "缺少 Index",
			opts:    nil,
			wantErr: true,
		},
		{
			name: "完整配置",
			opts: []PipelineOption{
				WithIndex("items"),
				WithBatchSize(50),
			},
			wantErr: false,
		},
		{
			name: "带认证和 Upsert 配置",
			opts: []PipelineOption{
				WithAddresses([]string{"http://es1:9200", "http://es2:9200"}),
				WithIndex("items"),
				WithUsername("elastic"),
				WithPassword("changeme"),
				WithUpsertKey("url"),
				WithDocumentIDField("id"),
				WithRefresh("wait_for"),
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
	if len(opts.Addresses) != 1 || opts.Addresses[0] != "http://localhost:9200" {
		t.Errorf("默认 Addresses 不正确: %v", opts.Addresses)
	}
}

func TestPipelineOptions(t *testing.T) {
	cfg := &pipelineConfig{
		esOpts:    DefaultOptions(),
		batchSize: 100,
	}

	WithAddresses([]string{"http://es1:9200"})(cfg)
	WithIndex("myindex")(cfg)
	WithUsername("user")(cfg)
	WithPassword("pass")(cfg)
	WithDocumentIDField("doc_id")(cfg)
	WithRefresh("true")(cfg)
	WithBatchSize(50)(cfg)
	WithUpsertKey("url")(cfg)

	if cfg.esOpts.Addresses[0] != "http://es1:9200" {
		t.Errorf("Addresses 不正确: %v", cfg.esOpts.Addresses)
	}
	if cfg.esOpts.Index != "myindex" {
		t.Errorf("Index 不正确: %s", cfg.esOpts.Index)
	}
	if cfg.esOpts.Username != "user" {
		t.Errorf("Username 不正确: %s", cfg.esOpts.Username)
	}
	if cfg.esOpts.Password != "pass" {
		t.Errorf("Password 不正确: %s", cfg.esOpts.Password)
	}
	if cfg.esOpts.DocumentIDField != "doc_id" {
		t.Errorf("DocumentIDField 不正确: %s", cfg.esOpts.DocumentIDField)
	}
	if cfg.esOpts.Refresh != "true" {
		t.Errorf("Refresh 不正确: %s", cfg.esOpts.Refresh)
	}
	if cfg.batchSize != 50 {
		t.Errorf("BatchSize 不正确: %d", cfg.batchSize)
	}
	if cfg.upsertKey != "url" {
		t.Errorf("UpsertKey 不正确: %s", cfg.upsertKey)
	}
}

func TestBuildBulkBody_Index(t *testing.T) {
	w := &Writer{
		opts: &Options{
			Index:           "test-index",
			DocumentIDField: "id",
		},
	}

	items := []map[string]any{
		{"id": "1", "title": "First"},
		{"id": "2", "title": "Second"},
	}

	body, err := w.buildBulkBody("index", "", items)
	if err != nil {
		t.Fatalf("buildBulkBody 失败: %v", err)
	}

	lines := splitNDJSON(body)
	if len(lines) != 4 { // 2 items × 2 lines each
		t.Fatalf("应有 4 行 NDJSON，实际: %d", len(lines))
	}

	// 验证第一个 action 行
	var action map[string]map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &action); err != nil {
		t.Fatalf("解析 action 行失败: %v", err)
	}
	if action["index"]["_index"] != "test-index" {
		t.Errorf("index 不正确: %v", action["index"]["_index"])
	}
	if action["index"]["_id"] != "1" {
		t.Errorf("_id 不正确: %v", action["index"]["_id"])
	}
}

func TestBuildBulkBody_Update(t *testing.T) {
	w := &Writer{
		opts: &Options{
			Index: "test-index",
		},
	}

	items := []map[string]any{
		{"url": "http://a.com", "title": "A"},
	}

	body, err := w.buildBulkBody("update", "url", items)
	if err != nil {
		t.Fatalf("buildBulkBody 失败: %v", err)
	}

	lines := splitNDJSON(body)
	if len(lines) != 2 {
		t.Fatalf("应有 2 行 NDJSON，实际: %d", len(lines))
	}

	// 验证 action 行
	var action map[string]map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &action); err != nil {
		t.Fatalf("解析 action 行失败: %v", err)
	}
	if action["update"]["_id"] != "http://a.com" {
		t.Errorf("_id 不正确: %v", action["update"]["_id"])
	}

	// 验证数据行
	var doc map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &doc); err != nil {
		t.Fatalf("解析数据行失败: %v", err)
	}
	if doc["doc_as_upsert"] != true {
		t.Error("doc_as_upsert 应为 true")
	}
}

func TestBuildBulkBody_NoDocID(t *testing.T) {
	w := &Writer{
		opts: &Options{
			Index: "test-index",
		},
	}

	items := []map[string]any{
		{"title": "No ID"},
	}

	body, err := w.buildBulkBody("index", "", items)
	if err != nil {
		t.Fatalf("buildBulkBody 失败: %v", err)
	}

	lines := splitNDJSON(body)
	var action map[string]map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &action); err != nil {
		t.Fatalf("解析 action 行失败: %v", err)
	}
	if _, ok := action["index"]["_id"]; ok {
		t.Error("无 DocumentIDField 时不应设置 _id")
	}
}

func TestBulkResponse_Parse(t *testing.T) {
	respJSON := `{
		"errors": false,
		"items": [
			{"index": {"_index": "test", "_id": "1", "status": 201}},
			{"index": {"_index": "test", "_id": "2", "status": 201}}
		]
	}`

	var resp bulkResponse
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Errors {
		t.Error("Errors 应为 false")
	}
	if len(resp.Items) != 2 {
		t.Errorf("Items 数量不正确: %d", len(resp.Items))
	}
}

func TestBulkResponse_WithErrors(t *testing.T) {
	respJSON := `{
		"errors": true,
		"items": [
			{"index": {"_index": "test", "_id": "1", "status": 201}},
			{"index": {"_index": "test", "_id": "2", "status": 400, "error": {"type": "mapper_parsing_exception", "reason": "failed to parse"}}}
		]
	}`

	var resp bulkResponse
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !resp.Errors {
		t.Error("Errors 应为 true")
	}

	// 统计错误
	errorCount := 0
	for _, item := range resp.Items {
		for _, result := range item {
			if result.Error != nil {
				errorCount++
			}
		}
	}
	if errorCount != 1 {
		t.Errorf("错误数量不正确: %d", errorCount)
	}
}

// splitNDJSON 将 NDJSON 缓冲区按行分割。
func splitNDJSON(buf *bytes.Buffer) []string {
	var lines []string
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			lines = append(lines, string(line))
		}
	}
	return lines
}
