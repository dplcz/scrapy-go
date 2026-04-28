package feedexport

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dplcz/scrapy-go/pkg/spider"
)

// ============================================================================
// FileStorage — 本地文件存储后端
// ============================================================================

// FileStorage 将导出数据写入本地文件。
// 对应 Scrapy 的 FileFeedStorage。
//
// 特性：
//   - 支持 "file://" 前缀 URI 或普通路径
//   - 支持目录自动创建（父目录不存在时递归创建）
//   - overwrite=true 时覆盖已有文件，否则以追加模式打开
type FileStorage struct {
	path      string
	overwrite bool
	file      *os.File
}

// NewFileStorage 根据 URI 创建一个本地文件存储。
// URI 可以是：
//   - "output.json"             — 相对路径
//   - "/tmp/output.json"        — 绝对路径
//   - "file:///tmp/output.json" — 带 file:// scheme
func NewFileStorage(uri string, overwrite bool) (*FileStorage, error) {
	path, err := fileURIToPath(uri)
	if err != nil {
		return nil, err
	}
	return &FileStorage{
		path:      path,
		overwrite: overwrite,
	}, nil
}

// Open 打开（或创建）目标文件，返回 io.WriteCloser。
func (s *FileStorage) Open(ctx context.Context, sp spider.Spider) (io.WriteCloser, error) {
	if s.file != nil {
		return nil, fmt.Errorf("feedexport: file storage already open: %s", s.path)
	}

	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("feedexport: create directory %s: %w", dir, err)
		}
	}

	flag := os.O_CREATE | os.O_WRONLY
	if s.overwrite {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_APPEND
	}

	f, err := os.OpenFile(s.path, flag, 0o644)
	if err != nil {
		return nil, fmt.Errorf("feedexport: open file %s: %w", s.path, err)
	}
	s.file = f
	return f, nil
}

// Store 关闭文件句柄。由于 FileStorage 直接写入目标位置，
// 无需额外的 rename 操作。
func (s *FileStorage) Store(ctx context.Context, w io.WriteCloser) error {
	if w == nil {
		return nil
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("feedexport: close file %s: %w", s.path, err)
	}
	s.file = nil
	return nil
}

// Path 返回存储的实际文件路径。
func (s *FileStorage) Path() string {
	return s.path
}

// fileURIToPath 将 URI 转换为文件系统路径。
func fileURIToPath(uri string) (string, error) {
	if !strings.HasPrefix(uri, "file:") {
		return uri, nil
	}
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("feedexport: parse file uri %q: %w", uri, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("feedexport: unsupported scheme %q", u.Scheme)
	}
	// 允许 file:///path 和 file:path 两种形式
	p := u.Path
	if p == "" {
		p = u.Opaque
	}
	// Windows 下的 file:///C:/path 处理：trim leading "/"
	if filepath.VolumeName(strings.TrimLeft(p, "/")) != "" {
		p = strings.TrimLeft(p, "/")
	}
	return p, nil
}

// ============================================================================
// StdoutStorage — 标准输出存储后端
// ============================================================================

// StdoutStorage 将导出数据直接写入 os.Stdout。
// 对应 Scrapy 的 StdoutFeedStorage。
//
// 特性：
//   - Open 返回一个对 os.Stdout 的 no-op wrapper（防止外部关闭 Stdout）
//   - Store 为 no-op
//   - overwrite 选项无意义，保留仅为接口一致性
type StdoutStorage struct{}

// NewStdoutStorage 创建一个标准输出存储。
func NewStdoutStorage() *StdoutStorage {
	return &StdoutStorage{}
}

// Open 返回 Stdout 的安全包装。
func (s *StdoutStorage) Open(ctx context.Context, sp spider.Spider) (io.WriteCloser, error) {
	return &nopCloser{Writer: os.Stdout}, nil
}

// Store no-op（Stdout 不需要后处理）。
func (s *StdoutStorage) Store(ctx context.Context, w io.WriteCloser) error {
	if w == nil {
		return nil
	}
	return w.Close()
}

// nopCloser 是 io.Writer 的 no-op Close 包装，防止 os.Stdout 被意外关闭。
type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

// ============================================================================
// URI 模板渲染
// ============================================================================

// URIParams 存储可用于 URI 模板的变量。
// 对应 Scrapy 的 _get_uri_params 函数返回值。
type URIParams struct {
	// SpiderName 为 Spider.Name()
	SpiderName string

	// Time 为爬取开始的 UTC 时间，格式化字符串 "YYYY-MM-DDTHH-MM-SS"
	Time string

	// BatchTime 等同于 Time（保留兼容性）
	BatchTime string

	// BatchID 批次 ID，从 1 开始，当前 Go 版本不支持分批，恒为 1
	BatchID int

	// Extra 是用户自定义的额外变量
	Extra map[string]string
}

// NewURIParams 生成一个默认的 URIParams。
func NewURIParams(spiderName string) URIParams {
	now := time.Now().UTC().Format("2006-01-02T15-04-05")
	return URIParams{
		SpiderName: spiderName,
		Time:       now,
		BatchTime:  now,
		BatchID:    1,
		Extra:      map[string]string{},
	}
}

// Render 渲染 URI 模板，替换形如 "%(name)s" / "%(batch_id)d" 的占位符。
// 不识别的占位符会保留原字符串。
//
// 支持的占位符：
//   - %(name)s       — Spider 名称
//   - %(time)s       — 爬取开始时间
//   - %(batch_time)s — 等同 time
//   - %(batch_id)d   — 批次 ID
//   - %(<key>)s      — 来自 Extra
func (p URIParams) Render(template string) string {
	replacements := map[string]string{
		"%(name)s":       p.SpiderName,
		"%(time)s":       p.Time,
		"%(batch_time)s": p.BatchTime,
		"%(batch_id)d":   fmt.Sprintf("%d", p.BatchID),
	}
	for k, v := range p.Extra {
		replacements[fmt.Sprintf("%%(%s)s", k)] = v
	}

	result := template
	for placeholder, value := range replacements {
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// ============================================================================
// Storage 解析：根据 URI scheme 自动选择后端
// ============================================================================

// NewStorageForURI 根据 URI 的 scheme 自动选择存储后端。
// 支持：
//   - "stdout:" 或 "-"            → StdoutStorage
//   - "file://...", 相对/绝对路径 → FileStorage
//
// 不支持的 scheme（如 s3://、ftp://）会返回错误。
func NewStorageForURI(uri string, overwrite bool) (FeedStorage, error) {
	if uri == "" {
		return nil, fmt.Errorf("feedexport: empty uri")
	}
	if uri == "-" || uri == "stdout:" || strings.HasPrefix(uri, "stdout:") {
		return NewStdoutStorage(), nil
	}
	// 解析 scheme
	if i := strings.Index(uri, "://"); i > 0 {
		scheme := strings.ToLower(uri[:i])
		switch scheme {
		case "file":
			return NewFileStorage(uri, overwrite)
		default:
			return nil, fmt.Errorf("feedexport: unsupported storage scheme %q in uri %q", scheme, uri)
		}
	}
	// 普通路径
	return NewFileStorage(uri, overwrite)
}
