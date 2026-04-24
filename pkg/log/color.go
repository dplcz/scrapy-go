package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
	colorWhite  = "\033[37m"

	// Bold variants
	colorBoldRed    = "\033[1;31m"
	colorBoldYellow = "\033[1;33m"
	colorBoldGreen  = "\033[1;32m"
	colorBoldCyan   = "\033[1;36m"
)

// ColorReset 返回 ANSI 重置颜色的转义序列。
const ColorReset = colorReset

// ColorByPriority 根据组件优先级返回对应的 ANSI 颜色。
// 优先级范围与颜色映射：
//   - 0~299 (低优先级/先执行): 绿色
//   - 300~599 (中优先级): 黄色
//   - 600+ (高优先级/后执行): 品红色
func ColorByPriority(priority int) string {
	switch {
	case priority < 300:
		return colorGreen
	case priority < 600:
		return colorYellow
	default:
		return colorMagenta
	}
}

// ColorByStatusCode 根据 HTTP 状态码返回对应的 ANSI 颜色。
// 状态码范围与颜色映射：
//   - 2xx (成功): 绿色
//   - 3xx (重定向): 青色
//   - 4xx (客户端错误): 粗体黄色
//   - 5xx (服务端错误): 粗体红色
//   - 其他: 白色
func ColorByStatusCode(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return colorBoldGreen
	case statusCode >= 300 && statusCode < 400:
		return colorBoldCyan
	case statusCode >= 400 && statusCode < 500:
		return colorBoldYellow
	case statusCode >= 500:
		return colorBoldRed
	default:
		return colorWhite
	}
}

// levelColor returns the ANSI color code for the given log level.
func levelColor(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return colorCyan // DEBUG
	case level < slog.LevelWarn:
		return colorGreen // INFO
	case level < slog.LevelError:
		return colorBoldYellow // WARN
	default:
		return colorBoldRed // ERROR
	}
}

// levelLabel returns a fixed-width label for the given log level.
func levelLabel(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "DBG"
	case level < slog.LevelWarn:
		return "INF"
	case level < slog.LevelError:
		return "WRN"
	default:
		return "ERR"
	}
}

// ColorHandler is a slog.Handler that outputs colored log messages to a terminal.
//
// Log level colors:
//   - DEBUG: cyan
//   - INFO:  green
//   - WARN:  bold yellow
//   - ERROR: bold red
//
// When the output is not a terminal (e.g. piped to a file), colors are automatically disabled.
type ColorHandler struct {
	opts      slog.HandlerOptions
	output    io.Writer
	mu        *sync.Mutex
	attrs     []slog.Attr
	groups    []string
	useColor  bool
}

// NewColorHandler creates a new ColorHandler.
// If output is nil, os.Stderr is used.
// Colors are automatically enabled when output is a terminal.
func NewColorHandler(output io.Writer, opts *slog.HandlerOptions) *ColorHandler {
	if output == nil {
		output = os.Stderr
	}
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}

	return &ColorHandler{
		opts:     *opts,
		output:   output,
		mu:       &sync.Mutex{},
		useColor: isTerminal(output),
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *ColorHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

// Handle formats and writes a log record.
func (h *ColorHandler) Handle(_ context.Context, r slog.Record) error {
	var buf strings.Builder

	// Timestamp
	timeStr := r.Time.Format(time.DateTime)
	if h.useColor {
		buf.WriteString(colorGray)
		buf.WriteString(timeStr)
		buf.WriteString(colorReset)
	} else {
		buf.WriteString(timeStr)
	}
	buf.WriteByte(' ')

	// Level with color
	label := levelLabel(r.Level)
	if h.useColor {
		color := levelColor(r.Level)
		buf.WriteString(color)
		buf.WriteString(label)
		buf.WriteString(colorReset)
	} else {
		buf.WriteString(label)
	}
	buf.WriteByte(' ')

	// Message
	if h.useColor {
		buf.WriteString(colorWhite)
		buf.WriteString(r.Message)
		buf.WriteString(colorReset)
	} else {
		buf.WriteString(r.Message)
	}

	// Source (if enabled)
	if h.opts.AddSource && r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		if f.File != "" {
			buf.WriteByte(' ')
			if h.useColor {
				buf.WriteString(colorGray)
			}
			buf.WriteString(fmt.Sprintf("source=%s:%d", f.File, f.Line))
			if h.useColor {
				buf.WriteString(colorReset)
			}
		}
	}

	// Pre-defined attrs (from With())
	for _, a := range h.attrs {
		h.appendAttr(&buf, a)
	}

	// Record attrs
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(&buf, a)
		return true
	})

	buf.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.output, buf.String())
	return err
}

// WithAttrs returns a new Handler with the given attributes added.
func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs), len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	newAttrs = append(newAttrs, attrs...)

	return &ColorHandler{
		opts:     h.opts,
		output:   h.output,
		mu:       h.mu,
		attrs:    newAttrs,
		groups:   h.groups,
		useColor: h.useColor,
	}
}

// WithGroup returns a new Handler with the given group name.
func (h *ColorHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name

	return &ColorHandler{
		opts:     h.opts,
		output:   h.output,
		mu:       h.mu,
		attrs:    h.attrs,
		groups:   newGroups,
		useColor: h.useColor,
	}
}

// appendAttr appends a formatted key=value pair to the buffer.
func (h *ColorHandler) appendAttr(buf *strings.Builder, a slog.Attr) {
	// Resolve the attribute value
	a.Value = a.Value.Resolve()

	// Skip empty attributes
	if a.Equal(slog.Attr{}) {
		return
	}

	buf.WriteByte(' ')

	// Build the key with group prefix
	key := a.Key
	if len(h.groups) > 0 {
		key = strings.Join(h.groups, ".") + "." + key
	}

	if h.useColor {
		buf.WriteString(colorGray)
		buf.WriteString(key)
		buf.WriteString(colorReset)
		buf.WriteByte('=')
		buf.WriteString(formatValue(a.Value))
	} else {
		buf.WriteString(key)
		buf.WriteByte('=')
		buf.WriteString(formatValue(a.Value))
	}
}

// formatValue formats a slog.Value for display.
func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		// Quote strings that contain spaces
		if strings.ContainsAny(s, " \t\n\"") {
			return fmt.Sprintf("%q", s)
		}
		return s
	default:
		return fmt.Sprintf("%v", v.Any())
	}
}

// isTerminal checks if the writer is a terminal (supports ANSI colors).
func isTerminal(w io.Writer) bool {
	// On Windows, color support depends on the terminal
	if runtime.GOOS == "windows" {
		return false
	}

	if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil {
			return false
		}
		// Check if it's a character device (terminal)
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}
