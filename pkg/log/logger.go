// Package log 提供了 scrapy-go 框架的结构化日志封装。
//
// 基于 Go 标准库 log/slog 包，提供框架级别的日志工具函数，
// 支持日志级别配置和 Spider 上下文关联。
package log

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// ============================================================================
// 日志级别
// ============================================================================

// ParseLevel 将字符串解析为 slog.Level。
// 支持的级别：DEBUG、INFO、WARN/WARNING、ERROR。
// 不区分大小写，无法识别时返回 slog.LevelInfo。
func ParseLevel(level string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ============================================================================
// Logger 创建
// ============================================================================

// NewLogger 创建一个新的 slog.Logger。
//
// 参数：
//   - level: 日志级别字符串（如 "DEBUG"、"INFO"）
//   - output: 输出目标，为 nil 时输出到 os.Stderr
//   - addSource: 是否在日志中添加源代码位置信息
func NewLogger(level string, output io.Writer, addSource bool) *slog.Logger {
	if output == nil {
		output = os.Stderr
	}

	opts := &slog.HandlerOptions{
		Level:     ParseLevel(level),
		AddSource: addSource,
	}

	handler := slog.NewTextHandler(output, opts)
	return slog.New(handler)
}

// NewJSONLogger 创建一个 JSON 格式的 slog.Logger。
func NewJSONLogger(level string, output io.Writer, addSource bool) *slog.Logger {
	if output == nil {
		output = os.Stderr
	}

	opts := &slog.HandlerOptions{
		Level:     ParseLevel(level),
		AddSource: addSource,
	}

	handler := slog.NewJSONHandler(output, opts)
	return slog.New(handler)
}

// NewColorLogger 创建一个带颜色输出的 slog.Logger。
// 不同日志级别使用不同颜色：
//   - DEBUG: cyan
//   - INFO:  green
//   - WARN:  bold yellow
//   - ERROR: bold red
//
// 当输出目标不是终端时（如重定向到文件），颜色自动禁用。
func NewColorLogger(level string, output io.Writer, addSource bool) *slog.Logger {
	if output == nil {
		output = os.Stderr
	}

	opts := &slog.HandlerOptions{
		Level:     ParseLevel(level),
		AddSource: addSource,
	}

	handler := NewColorHandler(output, opts)
	return slog.New(handler)
}

// ============================================================================
// 上下文关联
// ============================================================================

// 上下文键类型（避免键冲突）
type contextKey string

const (
	spiderNameKey contextKey = "spider_name"
	componentKey  contextKey = "component"
)

// WithSpiderName 在 context 中设置 Spider 名称。
func WithSpiderName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, spiderNameKey, name)
}

// WithComponent 在 context 中设置组件名称。
func WithComponent(ctx context.Context, component string) context.Context {
	return context.WithValue(ctx, componentKey, component)
}

// SpiderNameFromContext 从 context 中获取 Spider 名称。
func SpiderNameFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(spiderNameKey).(string); ok {
		return v
	}
	return ""
}

// ComponentFromContext 从 context 中获取组件名称。
func ComponentFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(componentKey).(string); ok {
		return v
	}
	return ""
}

// ============================================================================
// 便捷日志函数
// ============================================================================

// ForSpider 创建一个带 Spider 名称的子 Logger。
func ForSpider(logger *slog.Logger, spiderName string) *slog.Logger {
	return logger.With("spider", spiderName)
}

// ForComponent 创建一个带组件名称的子 Logger。
func ForComponent(logger *slog.Logger, component string) *slog.Logger {
	return logger.With("component", component)
}

// ForSpiderComponent 创建一个同时带 Spider 和组件名称的子 Logger。
func ForSpiderComponent(logger *slog.Logger, spiderName, component string) *slog.Logger {
	return logger.With("spider", spiderName, "component", component)
}
