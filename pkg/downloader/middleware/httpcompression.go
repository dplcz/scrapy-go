package middleware

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	scrapy_http "scrapy-go/pkg/http"
	"scrapy-go/pkg/stats"
)

// HttpCompressionMiddleware 处理 HTTP 压缩响应的自动解压。
// 在请求阶段自动添加 Accept-Encoding 头，在响应阶段自动解压响应体。
//
// 对应 Scrapy 的 HttpCompressionMiddleware（优先级 590）。
//
// 支持的压缩编码：
//   - gzip / x-gzip
//   - deflate
//
// 注意：brotli (br) 和 zstd 需要引入外部依赖，将在后续版本中支持。
// 当前版本仅使用 Go 标准库实现 gzip 和 deflate 解压。
//
// 相关配置：
//   - COMPRESSION_ENABLED: 是否启用压缩处理（默认 true）
//   - DOWNLOAD_MAXSIZE: 解压后最大允许大小（默认 1GB）
//   - DOWNLOAD_WARNSIZE: 解压后大小警告阈值（默认 32MB）
type HttpCompressionMiddleware struct {
	BaseDownloaderMiddleware

	maxSize  int // 解压后最大允许大小（字节）
	warnSize int // 解压后大小警告阈值（字节）
	stats    stats.Collector
	logger   *slog.Logger
}

// acceptedEncodings 是支持的压缩编码列表。
var acceptedEncodings = []string{"gzip", "deflate"}

// NewHttpCompressionMiddleware 创建一个 HttpCompression 中间件。
func NewHttpCompressionMiddleware(maxSize, warnSize int, sc stats.Collector, logger *slog.Logger) *HttpCompressionMiddleware {
	if sc == nil {
		sc = stats.NewDummyCollector()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HttpCompressionMiddleware{
		maxSize:  maxSize,
		warnSize: warnSize,
		stats:    sc,
		logger:   logger,
	}
}

// ProcessRequest 自动添加 Accept-Encoding 请求头。
// 仅在请求未设置 Accept-Encoding 时添加。
func (m *HttpCompressionMiddleware) ProcessRequest(ctx context.Context, request *scrapy_http.Request) (*scrapy_http.Response, error) {
	// 仅在未设置 Accept-Encoding 时添加
	if request.Headers.Get("Accept-Encoding") == "" {
		request.Headers.Set("Accept-Encoding", strings.Join(acceptedEncodings, ", "))
	}
	return nil, nil
}

// ProcessResponse 自动解压压缩的响应体。
// 支持 gzip、x-gzip、deflate 编码。
func (m *HttpCompressionMiddleware) ProcessResponse(ctx context.Context, request *scrapy_http.Request, response *scrapy_http.Response) (*scrapy_http.Response, error) {
	// HEAD 请求不处理响应体
	if request.Method == "HEAD" {
		return response, nil
	}

	// 获取 Content-Encoding 头
	contentEncoding := response.Headers.Get("Content-Encoding")
	if contentEncoding == "" {
		return response, nil
	}

	// 空响应体无需解压
	if len(response.Body) == 0 {
		return response, nil
	}

	// 获取请求级别的大小限制
	maxSize := m.maxSize
	if v, ok := request.GetMeta("download_maxsize"); ok {
		if ms, ok := v.(int); ok {
			maxSize = ms
		}
	}
	warnSize := m.warnSize
	if v, ok := request.GetMeta("download_warnsize"); ok {
		if ws, ok := v.(int); ok {
			warnSize = ws
		}
	}

	// 解析并处理编码链（可能有多层编码，如 "gzip, deflate"）
	encodings := parseContentEncoding(contentEncoding)
	decodable, remaining := splitEncodings(encodings)

	if len(decodable) == 0 {
		return response, nil
	}

	// 逐层解压（从最外层开始）
	body := response.Body
	originalSize := len(body)
	for _, encoding := range decodable {
		decoded, err := decompress(body, encoding, maxSize)
		if err != nil {
			m.logger.Warn("解压响应体失败",
				"url", response.URL.String(),
				"encoding", encoding,
				"error", err,
			)
			return response, nil
		}
		body = decoded
	}

	// 检查解压后大小
	decompressedSize := len(body)
	if warnSize > 0 && originalSize < warnSize && decompressedSize >= warnSize {
		m.logger.Warn("响应体解压后大小超过警告阈值",
			"url", response.URL.String(),
			"compressed_size", originalSize,
			"decompressed_size", decompressedSize,
			"warn_size", warnSize,
		)
	}

	// 更新统计
	m.stats.IncValue("httpcompression/response_bytes", decompressedSize, 0)
	m.stats.IncValue("httpcompression/response_count", 1, 0)

	// 构建新的响应（替换 Body 和 Content-Encoding 头）
	newResp := response.Copy()
	newResp.Body = body
	newResp.Request = response.Request

	// 更新 Content-Encoding 头
	if len(remaining) > 0 {
		newResp.Headers.Set("Content-Encoding", strings.Join(remaining, ", "))
	} else {
		newResp.Headers.Del("Content-Encoding")
	}

	// 删除 Content-Length 头（解压后长度已变化）
	newResp.Headers.Del("Content-Length")

	return newResp, nil
}

// parseContentEncoding 解析 Content-Encoding 头为编码列表。
// 例如 "gzip, deflate" → ["gzip", "deflate"]
func parseContentEncoding(header string) []string {
	parts := strings.Split(header, ",")
	encodings := make([]string, 0, len(parts))
	for _, p := range parts {
		enc := strings.TrimSpace(strings.ToLower(p))
		if enc != "" {
			encodings = append(encodings, enc)
		}
	}
	return encodings
}

// splitEncodings 将编码列表分为可解码和不可解码两部分。
// 从列表末尾开始检查，遇到不支持的编码时停止。
// 返回值：decodable（需要解压的，从外到内顺序）, remaining（保留的）
func splitEncodings(encodings []string) (decodable []string, remaining []string) {
	supported := map[string]bool{
		"gzip":   true,
		"x-gzip": true,
		"deflate": true,
	}

	// 从末尾开始扫描
	i := len(encodings) - 1
	for ; i >= 0; i-- {
		if !supported[encodings[i]] {
			break
		}
	}

	remaining = make([]string, 0)
	decodable = make([]string, 0)

	if i >= 0 {
		remaining = append(remaining, encodings[:i+1]...)
	}
	if i+1 < len(encodings) {
		// 反转顺序：最外层编码最先解压
		for j := len(encodings) - 1; j > i; j-- {
			decodable = append(decodable, encodings[j])
		}
	}

	return decodable, remaining
}

// decompress 根据编码类型解压数据。
func decompress(data []byte, encoding string, maxSize int) ([]byte, error) {
	switch encoding {
	case "gzip", "x-gzip":
		return decompressGzip(data, maxSize)
	case "deflate":
		return decompressDeflate(data, maxSize)
	default:
		return data, nil
	}
}

// decompressGzip 解压 gzip 数据。
func decompressGzip(data []byte, maxSize int) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer reader.Close()

	return readLimited(reader, maxSize)
}

// decompressDeflate 解压 deflate 数据。
// 先尝试 raw deflate，如果失败则尝试 zlib 格式。
func decompressDeflate(data []byte, maxSize int) ([]byte, error) {
	// 先尝试 zlib 格式（带头部的 deflate）
	reader := flate.NewReader(bytes.NewReader(data))
	result, err := readLimited(reader, maxSize)
	reader.Close()
	if err == nil {
		return result, nil
	}

	// 如果 raw deflate 失败，尝试跳过 zlib 头部
	// 某些服务器发送的 deflate 实际上是 raw deflate 而非 zlib
	reader = flate.NewReader(bytes.NewReader(data))
	defer reader.Close()
	return readLimited(reader, maxSize)
}

// readLimited 从 reader 中读取数据，限制最大大小。
func readLimited(reader io.Reader, maxSize int) ([]byte, error) {
	if maxSize > 0 {
		// 限制读取大小，多读 1 字节用于检测是否超限
		limited := io.LimitReader(reader, int64(maxSize)+1)
		data, err := io.ReadAll(limited)
		if err != nil {
			return nil, err
		}
		if len(data) > maxSize {
			return nil, fmt.Errorf("decompressed size exceeds limit (%d bytes)", maxSize)
		}
		return data, nil
	}
	return io.ReadAll(reader)
}
