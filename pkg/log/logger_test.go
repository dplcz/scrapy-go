package log

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"DEBUG", slog.LevelDebug},
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"WARN", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo}, // 默认 INFO
		{"", slog.LevelInfo},
	}

	for i, tt := range tests {
		result := ParseLevel(tt.input)
		if result != tt.expected {
			t.Errorf("test %d: ParseLevel(%q) = %v, expected %v", i, tt.input, result, tt.expected)
		}
	}
}

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("DEBUG", &buf, false)

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("output should contain 'test message': %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("output should contain 'key=value': %s", output)
	}
}

func TestNewJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger("DEBUG", &buf, false)

	logger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Errorf("output should contain JSON msg: %s", output)
	}
}

func TestNewLoggerNilOutput(t *testing.T) {
	// 不应 panic
	logger := NewLogger("INFO", nil, false)
	if logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestContextFunctions(t *testing.T) {
	ctx := context.Background()

	// 设置 Spider 名称
	ctx = WithSpiderName(ctx, "my_spider")
	if SpiderNameFromContext(ctx) != "my_spider" {
		t.Error("unexpected spider name")
	}

	// 设置组件名称
	ctx = WithComponent(ctx, "downloader")
	if ComponentFromContext(ctx) != "downloader" {
		t.Error("unexpected component name")
	}

	// 空 context
	emptyCtx := context.Background()
	if SpiderNameFromContext(emptyCtx) != "" {
		t.Error("should return empty string for context without spider name")
	}
	if ComponentFromContext(emptyCtx) != "" {
		t.Error("should return empty string for context without component")
	}
}

func TestForSpider(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("DEBUG", &buf, false)

	spiderLogger := ForSpider(logger, "quotes")
	spiderLogger.Info("crawling")

	output := buf.String()
	if !strings.Contains(output, "spider=quotes") {
		t.Errorf("output should contain spider name: %s", output)
	}
}

func TestForComponent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("DEBUG", &buf, false)

	componentLogger := ForComponent(logger, "downloader")
	componentLogger.Info("downloading")

	output := buf.String()
	if !strings.Contains(output, "component=downloader") {
		t.Errorf("output should contain component name: %s", output)
	}
}

func TestForSpiderComponent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger("DEBUG", &buf, false)

	scLogger := ForSpiderComponent(logger, "quotes", "scheduler")
	scLogger.Info("scheduling")

	output := buf.String()
	if !strings.Contains(output, "spider=quotes") {
		t.Errorf("output should contain spider name: %s", output)
	}
	if !strings.Contains(output, "component=scheduler") {
		t.Errorf("output should contain component name: %s", output)
	}
}

func TestLogLevel(t *testing.T) {
	var buf bytes.Buffer
	// 设置为 WARN 级别
	logger := NewLogger("WARN", &buf, false)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")

	output := buf.String()
	if strings.Contains(output, "debug message") {
		t.Error("DEBUG message should not appear at WARN level")
	}
	if strings.Contains(output, "info message") {
		t.Error("INFO message should not appear at WARN level")
	}
	if !strings.Contains(output, "warn message") {
		t.Error("WARN message should appear at WARN level")
	}
}
