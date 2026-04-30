package httpcache

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/dplcz/scrapy-go/internal/utils"
	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// FilesystemCacheStorage 基于文件系统的缓存存储后端。
// 每个请求的缓存数据存储在以请求指纹命名的目录中，包含：
//   - meta.json: 请求/响应元数据（JSON 格式，替代 Scrapy 的 pickle）
//   - response_headers: 响应头（原始格式）
//   - response_body: 响应体
//   - request_headers: 请求头（原始格式）
//   - request_body: 请求体
//
// 对应 Scrapy 的 scrapy.extensions.httpcache.FilesystemCacheStorage。
type FilesystemCacheStorage struct {
	cacheDir       string
	expirationSecs int
	useGzip        bool
	spiderName     string
	logger         *slog.Logger
}

// cacheMetadata 缓存元数据结构。
type cacheMetadata struct {
	URL         string `json:"url"`
	Method      string `json:"method"`
	Status      int    `json:"status"`
	ResponseURL string `json:"response_url"`
	Timestamp   int64  `json:"timestamp"`
}

// FilesystemOption 是 FilesystemCacheStorage 的可选配置函数。
type FilesystemOption func(*FilesystemCacheStorage)

// WithExpirationSecs 设置缓存过期时间（秒），0 表示不过期。
func WithExpirationSecs(secs int) FilesystemOption {
	return func(s *FilesystemCacheStorage) {
		s.expirationSecs = secs
	}
}

// WithGzip 设置是否使用 gzip 压缩存储。
func WithGzip(useGzip bool) FilesystemOption {
	return func(s *FilesystemCacheStorage) {
		s.useGzip = useGzip
	}
}

// WithStorageLogger 设置日志记录器。
func WithStorageLogger(logger *slog.Logger) FilesystemOption {
	return func(s *FilesystemCacheStorage) {
		s.logger = logger
	}
}

// NewFilesystemCacheStorage 创建一个新的文件系统缓存存储后端。
func NewFilesystemCacheStorage(cacheDir string, opts ...FilesystemOption) *FilesystemCacheStorage {
	s := &FilesystemCacheStorage{
		cacheDir: cacheDir,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Open 在 Spider 打开时调用，初始化存储目录。
func (s *FilesystemCacheStorage) Open(spiderName string) error {
	s.spiderName = spiderName
	s.logger.Debug("using filesystem cache storage",
		"cachedir", s.cacheDir,
		"spider", spiderName,
	)
	return nil
}

// Close 在 Spider 关闭时调用。
func (s *FilesystemCacheStorage) Close() error {
	return nil
}

// RetrieveResponse 从缓存中检索响应。
func (s *FilesystemCacheStorage) RetrieveResponse(request *shttp.Request) (*shttp.Response, error) {
	meta, err := s.readMeta(request)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil // 缓存未命中
	}

	rpath := s.getRequestPath(request)

	// 读取响应体
	body, err := s.readFile(filepath.Join(rpath, "response_body"))
	if err != nil {
		return nil, fmt.Errorf("httpcache: read response body: %w", err)
	}

	// 读取响应头
	rawHeaders, err := s.readFile(filepath.Join(rpath, "response_headers"))
	if err != nil {
		return nil, fmt.Errorf("httpcache: read response headers: %w", err)
	}

	headers := parseRawHeaders(rawHeaders)

	responseURL := meta.ResponseURL
	if responseURL == "" {
		responseURL = request.URL.String()
	}

	u, err := url.Parse(responseURL)
	if err != nil {
		u = request.URL
	}

	resp := &shttp.Response{
		URL:     u,
		Status:  meta.Status,
		Headers: headers,
		Body:    body,
		Request: request,
	}

	return resp, nil
}

// StoreResponse 将响应存储到缓存中。
func (s *FilesystemCacheStorage) StoreResponse(request *shttp.Request, response *shttp.Response) error {
	rpath := s.getRequestPath(request)

	// 创建缓存目录
	if err := os.MkdirAll(rpath, 0o755); err != nil {
		return fmt.Errorf("httpcache: create cache dir: %w", err)
	}

	// 写入元数据
	meta := cacheMetadata{
		URL:         request.URL.String(),
		Method:      request.Method,
		Status:      response.Status,
		ResponseURL: response.URL.String(),
		Timestamp:   time.Now().Unix(),
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("httpcache: marshal metadata: %w", err)
	}
	if err := s.writeFile(filepath.Join(rpath, "meta.json"), metaBytes); err != nil {
		return fmt.Errorf("httpcache: write metadata: %w", err)
	}

	// 写入响应头
	rawHeaders := serializeHeaders(response.Headers)
	if err := s.writeFile(filepath.Join(rpath, "response_headers"), rawHeaders); err != nil {
		return fmt.Errorf("httpcache: write response headers: %w", err)
	}

	// 写入响应体
	if err := s.writeFile(filepath.Join(rpath, "response_body"), response.Body); err != nil {
		return fmt.Errorf("httpcache: write response body: %w", err)
	}

	// 写入请求头
	reqHeaders := serializeHeaders(request.Headers)
	if err := s.writeFile(filepath.Join(rpath, "request_headers"), reqHeaders); err != nil {
		return fmt.Errorf("httpcache: write request headers: %w", err)
	}

	// 写入请求体
	if err := s.writeFile(filepath.Join(rpath, "request_body"), request.Body); err != nil {
		return fmt.Errorf("httpcache: write request body: %w", err)
	}

	return nil
}

// getRequestPath 获取请求的缓存目录路径。
// 路径格式：{cacheDir}/{spiderName}/{fingerprint[0:2]}/{fingerprint}
func (s *FilesystemCacheStorage) getRequestPath(request *shttp.Request) string {
	fp := utils.RequestFingerprint(request, nil, false)
	return filepath.Join(s.cacheDir, s.spiderName, fp[:2], fp)
}

// readMeta 读取缓存元数据。
// 如果缓存不存在或已过期，返回 nil。
func (s *FilesystemCacheStorage) readMeta(request *shttp.Request) (*cacheMetadata, error) {
	rpath := s.getRequestPath(request)
	metaPath := filepath.Join(rpath, "meta.json")

	info, err := os.Stat(metaPath)
	if os.IsNotExist(err) {
		return nil, nil // 缓存不存在
	}
	if err != nil {
		return nil, fmt.Errorf("httpcache: stat metadata: %w", err)
	}

	// 检查过期
	if s.expirationSecs > 0 {
		elapsed := time.Since(info.ModTime()).Seconds()
		if elapsed > float64(s.expirationSecs) {
			return nil, nil // 已过期
		}
	}

	data, err := s.readFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("httpcache: read metadata: %w", err)
	}

	var meta cacheMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("httpcache: unmarshal metadata: %w", err)
	}

	return &meta, nil
}

// readFile 读取文件内容，支持 gzip 解压。
func (s *FilesystemCacheStorage) readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var reader io.Reader = f
	if s.useGzip {
		gr, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("httpcache: gzip reader: %w", err)
		}
		defer gr.Close()
		reader = gr
	}

	return io.ReadAll(reader)
}

// writeFile 写入文件内容，支持 gzip 压缩。
// 使用临时文件 + rename 实现原子写入。
func (s *FilesystemCacheStorage) writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// 创建临时文件
	tmpFile, err := os.CreateTemp(dir, ".httpcache-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	// 确保失败时清理临时文件
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	var writer io.Writer = tmpFile
	if s.useGzip {
		gw := gzip.NewWriter(tmpFile)
		if _, err := gw.Write(data); err != nil {
			tmpFile.Close()
			return err
		}
		if err := gw.Close(); err != nil {
			tmpFile.Close()
			return err
		}
	} else {
		if _, err := writer.Write(data); err != nil {
			tmpFile.Close()
			return err
		}
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	// 原子重命名
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	success = true
	return nil
}

// serializeHeaders 将 http.Header 序列化为原始字节格式。
// 格式：每行 "Key: Value\r\n"。
func serializeHeaders(headers http.Header) []byte {
	if headers == nil {
		return nil
	}
	var result []byte
	for key, values := range headers {
		for _, v := range values {
			result = append(result, []byte(key+": "+v+"\r\n")...)
		}
	}
	return result
}

// parseRawHeaders 将原始字节格式的头部解析为 http.Header。
func parseRawHeaders(raw []byte) http.Header {
	headers := make(http.Header)
	if len(raw) == 0 {
		return headers
	}

	lines := splitLines(raw)
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		// 查找第一个 ": " 分隔符
		for i := 0; i < len(line)-1; i++ {
			if line[i] == ':' && i+1 < len(line) && line[i+1] == ' ' {
				key := string(line[:i])
				value := string(line[i+2:])
				headers.Add(key, value)
				break
			}
		}
	}
	return headers
}

// splitLines 按 \r\n 或 \n 分割字节切片。
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			end := i
			if end > start && data[end-1] == '\r' {
				end--
			}
			if end > start {
				lines = append(lines, data[start:end])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
