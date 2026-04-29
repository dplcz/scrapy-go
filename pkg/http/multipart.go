// Package http 定义了 scrapy-go 框架的 HTTP 请求和响应模型。
package http

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"path/filepath"
	"strings"
)

// ============================================================================
// Multipart 文件上传（P3-012c）
// ============================================================================

// FormField 表示 multipart 表单中的一个普通文本字段。
type FormField struct {
	// Name 是字段名。
	Name string

	// Value 是字段值。
	Value string
}

// FormFile 表示 multipart 表单中的一个文件字段。
type FormFile struct {
	// FieldName 是表单字段名（如 "file"、"avatar"）。
	FieldName string

	// FileName 是文件名（如 "photo.jpg"）。
	FileName string

	// Content 是文件内容。
	Content []byte

	// ContentType 是文件的 MIME 类型（可选）。
	// 为空时根据文件扩展名自动推断，默认 "application/octet-stream"。
	ContentType string
}

// NewMultipartFormRequest 创建一个 multipart/form-data 请求，支持文件上传。
//
// 对齐 Scrapy 的 FormRequest 文件上传功能，基于 Go 标准库 mime/multipart 实现。
//
// fields 是普通表单字段列表，files 是文件字段列表。
// 两者都可以为 nil。
//
// 行为规则：
//   - 默认使用 POST 方法
//   - 自动设置 Content-Type 为 multipart/form-data; boundary=...
//   - 文件的 Content-Type 可通过 FormFile.ContentType 指定，为空时自动推断
//
// 用法：
//
//	// 上传文件
//	req, err := http.NewMultipartFormRequest("https://example.com/upload",
//	    []http.FormField{
//	        {Name: "title", Value: "My Photo"},
//	    },
//	    []http.FormFile{
//	        {FieldName: "file", FileName: "photo.jpg", Content: photoBytes},
//	    },
//	)
//
//	// 多文件上传
//	req, err := http.NewMultipartFormRequest("https://example.com/upload",
//	    nil,
//	    []http.FormFile{
//	        {FieldName: "files", FileName: "a.txt", Content: []byte("aaa")},
//	        {FieldName: "files", FileName: "b.txt", Content: []byte("bbb")},
//	    },
//	)
func NewMultipartFormRequest(rawURL string, fields []FormField, files []FormFile, opts ...RequestOption) (*Request, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// 写入普通字段
	for _, field := range fields {
		if err := writer.WriteField(field.Name, field.Value); err != nil {
			return nil, fmt.Errorf("failed to write field %q: %w", field.Name, err)
		}
	}

	// 写入文件字段
	for _, file := range files {
		if err := writeFilePart(writer, file); err != nil {
			return nil, fmt.Errorf("failed to write file %q: %w", file.FileName, err)
		}
	}

	// 关闭 writer（写入结束边界）
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// 构建请求选项
	defaultOpts := []RequestOption{
		WithMethod("POST"),
		WithBody(buf.Bytes()),
		WithHeader("Content-Type", writer.FormDataContentType()),
	}
	allOpts := append(defaultOpts, opts...)

	return NewRequest(rawURL, allOpts...)
}

// MustNewMultipartFormRequest 创建一个 multipart/form-data 请求，如果失败则 panic。
// 仅用于确定参数有效的场景。
func MustNewMultipartFormRequest(rawURL string, fields []FormField, files []FormFile, opts ...RequestOption) *Request {
	req, err := NewMultipartFormRequest(rawURL, fields, files, opts...)
	if err != nil {
		panic(err)
	}
	return req
}

// writeFilePart 将文件写入 multipart writer。
func writeFilePart(writer *multipart.Writer, file FormFile) error {
	// 确定 Content-Type
	contentType := file.ContentType
	if contentType == "" {
		contentType = inferContentType(file.FileName)
	}

	// 创建 MIME 头
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name=%q; filename=%q`, file.FieldName, file.FileName))
	h.Set("Content-Type", contentType)

	part, err := writer.CreatePart(h)
	if err != nil {
		return err
	}

	_, err = io.Copy(part, bytes.NewReader(file.Content))
	return err
}

// inferContentType 根据文件扩展名推断 MIME 类型。
func inferContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	// 常见 MIME 类型映射
	mimeTypes := map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
		".svg":  "image/svg+xml",
		".bmp":  "image/bmp",
		".ico":  "image/x-icon",
		".pdf":  "application/pdf",
		".zip":  "application/zip",
		".gz":   "application/gzip",
		".tar":  "application/x-tar",
		".json": "application/json",
		".xml":  "application/xml",
		".html": "text/html",
		".htm":  "text/html",
		".css":  "text/css",
		".js":   "application/javascript",
		".txt":  "text/plain",
		".csv":  "text/csv",
		".mp3":  "audio/mpeg",
		".mp4":  "video/mp4",
		".avi":  "video/x-msvideo",
		".doc":  "application/msword",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xls":  "application/vnd.ms-excel",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	}

	if mime, ok := mimeTypes[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}
