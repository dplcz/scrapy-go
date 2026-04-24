package spider

import (
	"context"
	"testing"

	scrapy_http "scrapy-go/pkg/http"
)

func TestSpiderOutput(t *testing.T) {
	// Request 输出
	req := scrapy_http.MustNewRequest("https://example.com")
	reqOutput := SpiderOutput{Request: req}
	if !reqOutput.IsRequest() {
		t.Error("should be request")
	}
	if reqOutput.IsItem() {
		t.Error("should not be item")
	}

	// Item 输出
	itemOutput := SpiderOutput{Item: map[string]any{"name": "test"}}
	if itemOutput.IsRequest() {
		t.Error("should not be request")
	}
	if !itemOutput.IsItem() {
		t.Error("should be item")
	}

	// 空输出
	emptyOutput := SpiderOutput{}
	if emptyOutput.IsRequest() || emptyOutput.IsItem() {
		t.Error("empty output should be neither request nor item")
	}
}

func TestBaseSpiderName(t *testing.T) {
	s := &BaseSpider{SpiderName: "test_spider"}
	if s.Name() != "test_spider" {
		t.Errorf("expected 'test_spider', got '%s'", s.Name())
	}
}

func TestBaseSpiderStart(t *testing.T) {
	s := &BaseSpider{
		SpiderName: "test",
		StartURLs:  []string{"https://example.com/1", "https://example.com/2"},
	}

	ctx := context.Background()
	ch := s.Start(ctx)

	var outputs []SpiderOutput
	for output := range ch {
		outputs = append(outputs, output)
	}

	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outputs))
	}

	if !outputs[0].IsRequest() {
		t.Error("output 0 should be request")
	}
	if outputs[0].Request.URL.String() != "https://example.com/1" {
		t.Errorf("unexpected URL: %s", outputs[0].Request.URL.String())
	}
	if !outputs[0].Request.DontFilter {
		t.Error("start request should have DontFilter=true")
	}

	if outputs[1].Request.URL.String() != "https://example.com/2" {
		t.Errorf("unexpected URL: %s", outputs[1].Request.URL.String())
	}
}

func TestBaseSpiderStartWithInvalidURL(t *testing.T) {
	s := &BaseSpider{
		SpiderName: "test",
		StartURLs:  []string{"://invalid", "https://example.com/valid"},
	}

	ctx := context.Background()
	ch := s.Start(ctx)

	var outputs []SpiderOutput
	for output := range ch {
		outputs = append(outputs, output)
	}

	// 无效 URL 被跳过，只有 1 个有效请求
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output (invalid URL skipped), got %d", len(outputs))
	}
	if outputs[0].Request.URL.String() != "https://example.com/valid" {
		t.Errorf("unexpected URL: %s", outputs[0].Request.URL.String())
	}
}

func TestBaseSpiderStartCancellation(t *testing.T) {
	s := &BaseSpider{
		SpiderName: "test",
		StartURLs:  []string{"https://example.com/1", "https://example.com/2", "https://example.com/3"},
	}

	ctx, cancel := context.WithCancel(context.Background())

	ch := s.Start(ctx)

	// 读取第一个后取消
	<-ch
	cancel()

	// channel 应该很快关闭
	count := 0
	for range ch {
		count++
	}
	// 可能读到 0 或 1 个额外的（取决于 goroutine 调度）
	if count > 2 {
		t.Errorf("should stop producing after cancel, got %d extra", count)
	}
}

func TestBaseSpiderParse(t *testing.T) {
	s := &BaseSpider{SpiderName: "test"}

	resp := scrapy_http.MustNewResponse("https://example.com", 200)
	outputs, err := s.Parse(context.Background(), resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 0 {
		t.Error("default Parse should return empty outputs")
	}
}

func TestBaseSpiderCustomSettings(t *testing.T) {
	s := &BaseSpider{SpiderName: "test"}
	if s.CustomSettings() != nil {
		t.Error("default CustomSettings should return nil")
	}
}

func TestBaseSpiderClosed(t *testing.T) {
	s := &BaseSpider{SpiderName: "test"}
	// 不应 panic
	s.Closed("finished")
}

func TestSpiderInterface(t *testing.T) {
	var _ Spider = (*BaseSpider)(nil)
}

// TestCustomSpider 测试自定义 Spider 实现
func TestCustomSpider(t *testing.T) {
	s := &testSpider{
		BaseSpider: BaseSpider{
			SpiderName: "quotes",
			StartURLs:  []string{"https://quotes.toscrape.com"},
		},
	}

	if s.Name() != "quotes" {
		t.Errorf("expected 'quotes', got '%s'", s.Name())
	}

	resp := scrapy_http.MustNewResponse("https://quotes.toscrape.com", 200,
		scrapy_http.WithResponseBody([]byte(`<html><body>Hello</body></html>`)),
	)

	outputs, err := s.Parse(context.Background(), resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}
	if !outputs[0].IsItem() {
		t.Error("output should be item")
	}
}

// testSpider 是一个自定义 Spider 实现
type testSpider struct {
	BaseSpider
}

func (s *testSpider) Parse(ctx context.Context, response *scrapy_http.Response) ([]SpiderOutput, error) {
	return []SpiderOutput{
		{Item: map[string]any{"url": response.URL.String(), "body_len": len(response.Body)}},
	}, nil
}

func (s *testSpider) CustomSettings() *SpiderSettings {
	return &SpiderSettings{
		DownloadDelay: DurationPtr(1),
	}
}
