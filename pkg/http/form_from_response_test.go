package http

import (
	"bytes"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// ============================================================================
// FormRequestFromResponse 测试（P3-012a）
// ============================================================================

// newTestResponse 创建一个用于测试的 Response。
func newTestResponse(rawURL string, body string) *Response {
	u, _ := url.Parse(rawURL)
	return &Response{
		URL:     u,
		Status:  200,
		Headers: make(http.Header),
		Body:    []byte(body),
	}
}

func TestFormRequestFromResponse_BasicForm(t *testing.T) {
	html := `<html><body>
		<form action="/login" method="POST">
			<input type="text" name="username" value="admin">
			<input type="password" name="password" value="">
			<input type="submit" name="submit" value="Login">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/page", html)
	req, err := FormRequestFromResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 URL
	if req.URL.String() != "https://example.com/login" {
		t.Errorf("expected URL https://example.com/login, got %s", req.URL.String())
	}

	// 验证方法
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}

	// 验证 body 包含表单字段
	body := string(req.Body)
	if !strings.Contains(body, "username=admin") {
		t.Errorf("body should contain username=admin, got: %s", body)
	}
	// 提交按钮也应该被包含（默认点击）
	if !strings.Contains(body, "submit=Login") {
		t.Errorf("body should contain submit=Login, got: %s", body)
	}
}

func TestFormRequestFromResponse_GETForm(t *testing.T) {
	html := `<html><body>
		<form action="/search" method="GET">
			<input type="text" name="q" value="golang">
			<input type="submit" value="Search">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "GET" {
		t.Errorf("expected GET, got %s", req.Method)
	}

	// GET 请求，数据应在 URL 查询参数中
	query := req.URL.Query()
	if query.Get("q") != "golang" {
		t.Errorf("expected q=golang, got %s", query.Get("q"))
	}
}

func TestFormRequestFromResponse_NoAction(t *testing.T) {
	html := `<html><body>
		<form method="POST">
			<input type="text" name="field" value="value">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/current-page", html)
	req, err := FormRequestFromResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 没有 action 时使用当前页面 URL
	if req.URL.String() != "https://example.com/current-page" {
		t.Errorf("expected current page URL, got %s", req.URL.String())
	}
}

func TestFormRequestFromResponse_NoMethod(t *testing.T) {
	html := `<html><body>
		<form action="/submit">
			<input type="text" name="field" value="value">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 没有 method 时默认 GET
	if req.Method != "GET" {
		t.Errorf("expected GET (default), got %s", req.Method)
	}
}

func TestFormRequestFromResponse_RelativeAction(t *testing.T) {
	html := `<html><body>
		<form action="../submit" method="POST">
			<input type="text" name="field" value="value">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/path/page", html)
	req, err := FormRequestFromResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.String() != "https://example.com/submit" {
		t.Errorf("expected https://example.com/submit, got %s", req.URL.String())
	}
}

func TestFormRequestFromResponse_NoForm(t *testing.T) {
	html := `<html><body><p>No form here</p></body></html>`

	resp := newTestResponse("https://example.com/", html)
	_, err := FormRequestFromResponse(resp)
	if err == nil {
		t.Fatal("expected error when no form found")
	}
	if !strings.Contains(err.Error(), "no <form>") {
		t.Errorf("error should mention no form: %v", err)
	}
}

func TestFormRequestFromResponse_NilResponse(t *testing.T) {
	_, err := FormRequestFromResponse(nil)
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestFormRequestFromResponse_WithFormData(t *testing.T) {
	html := `<html><body>
		<form action="/login" method="POST">
			<input type="text" name="username" value="default">
			<input type="password" name="password" value="">
			<input type="hidden" name="csrf" value="token123">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp,
		WithFormResponseData(map[string][]string{
			"username": {"admin"},
			"password": {"secret"},
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	// 用户数据应覆盖 HTML 中的默认值
	if !strings.Contains(body, "username=admin") {
		t.Errorf("body should contain username=admin, got: %s", body)
	}
	if !strings.Contains(body, "password=secret") {
		t.Errorf("body should contain password=secret, got: %s", body)
	}
	// 隐藏字段应保留
	if !strings.Contains(body, "csrf=token123") {
		t.Errorf("body should contain csrf=token123, got: %s", body)
	}
}

func TestFormRequestFromResponse_DontClick(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="text" name="field" value="value">
			<input type="submit" name="submit" value="Go">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	// 不应包含提交按钮
	if strings.Contains(body, "submit=Go") {
		t.Errorf("body should not contain submit button when dontClick: %s", body)
	}
}

func TestFormRequestFromResponse_WithClickButton(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="text" name="field" value="value">
			<input type="submit" name="action" value="save">
			<input type="submit" name="action" value="delete">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp,
		WithClickButton(map[string]string{"name": "action", "value": "delete"}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	if !strings.Contains(body, "action=delete") {
		t.Errorf("body should contain action=delete, got: %s", body)
	}
	// 不应包含 save 按钮
	if strings.Contains(body, "action=save") {
		t.Errorf("body should not contain action=save, got: %s", body)
	}
}

func TestFormRequestFromResponse_WithRequestOptions(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="text" name="field" value="value">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp,
		WithRequestOptions(
			WithPriority(10),
			WithMeta(map[string]any{"key": "value"}),
		),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Priority != 10 {
		t.Errorf("expected priority 10, got %d", req.Priority)
	}
	if v, ok := req.GetMeta("key"); !ok || v != "value" {
		t.Errorf("expected meta key=value, got %v", v)
	}
}

// ============================================================================
// 表单定位测试（P3-012b）
// ============================================================================

func TestFormRequestFromResponse_WithFormName(t *testing.T) {
	html := `<html><body>
		<form name="search" action="/search" method="GET">
			<input type="text" name="q" value="">
		</form>
		<form name="login" action="/login" method="POST">
			<input type="text" name="user" value="">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithFormName("login"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.Path != "/login" {
		t.Errorf("expected /login, got %s", req.URL.Path)
	}
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
}

func TestFormRequestFromResponse_WithFormNameNotFound(t *testing.T) {
	html := `<html><body>
		<form name="search" action="/search">
			<input type="text" name="q">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	_, err := FormRequestFromResponse(resp, WithFormName("nonexistent"))
	if err == nil {
		t.Fatal("expected error for nonexistent form name")
	}
}

func TestFormRequestFromResponse_WithFormID(t *testing.T) {
	html := `<html><body>
		<form id="form1" action="/first" method="GET">
			<input type="text" name="a" value="1">
		</form>
		<form id="form2" action="/second" method="POST">
			<input type="text" name="b" value="2">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithFormID("form2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.Path != "/second" {
		t.Errorf("expected /second, got %s", req.URL.Path)
	}
}

func TestFormRequestFromResponse_WithFormIDNotFound(t *testing.T) {
	html := `<html><body>
		<form id="form1" action="/first">
			<input type="text" name="a">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	_, err := FormRequestFromResponse(resp, WithFormID("nonexistent"))
	if err == nil {
		t.Fatal("expected error for nonexistent form id")
	}
}

func TestFormRequestFromResponse_WithFormNumber(t *testing.T) {
	html := `<html><body>
		<form action="/first" method="GET">
			<input type="text" name="a" value="1">
		</form>
		<form action="/second" method="POST">
			<input type="text" name="b" value="2">
		</form>
		<form action="/third" method="POST">
			<input type="text" name="c" value="3">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)

	// 第二个表单（索引 1）
	req, err := FormRequestFromResponse(resp, WithFormNumber(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.Path != "/second" {
		t.Errorf("expected /second, got %s", req.URL.Path)
	}
}

func TestFormRequestFromResponse_WithFormNumberOutOfRange(t *testing.T) {
	html := `<html><body>
		<form action="/only" method="GET">
			<input type="text" name="a">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	_, err := FormRequestFromResponse(resp, WithFormNumber(5))
	if err == nil {
		t.Fatal("expected error for out-of-range form number")
	}
}

func TestFormRequestFromResponse_WithFormCSS(t *testing.T) {
	html := `<html><body>
		<form class="search" action="/search" method="GET">
			<input type="text" name="q" value="">
		</form>
		<form class="login" action="/login" method="POST">
			<input type="text" name="user" value="">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithFormCSS("form.login"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.Path != "/login" {
		t.Errorf("expected /login, got %s", req.URL.Path)
	}
}

func TestFormRequestFromResponse_WithFormXPath(t *testing.T) {
	html := `<html><body>
		<form action="/first" method="GET">
			<input type="text" name="a" value="1">
		</form>
		<form action="/second" method="POST">
			<input type="text" name="b" value="2">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithFormXPath("//form[@action='/second']"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.Path != "/second" {
		t.Errorf("expected /second, got %s", req.URL.Path)
	}
}

func TestFormRequestFromResponse_WithFormXPathAncestor(t *testing.T) {
	html := `<html><body>
		<form action="/target" method="POST">
			<div class="inner">
				<input type="text" name="field" value="value">
			</div>
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	// XPath 指向表单内部元素，应向上查找 <form> 祖先
	req, err := FormRequestFromResponse(resp, WithFormXPath("//div[@class='inner']"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.Path != "/target" {
		t.Errorf("expected /target, got %s", req.URL.Path)
	}
}

func TestFormRequestFromResponse_WithFormCSSNotFound(t *testing.T) {
	html := `<html><body>
		<form action="/form" method="POST">
			<input type="text" name="field">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	_, err := FormRequestFromResponse(resp, WithFormCSS("form.nonexistent"))
	if err == nil {
		t.Fatal("expected error for nonexistent CSS selector")
	}
}

// ============================================================================
// 表单字段提取测试
// ============================================================================

func TestFormRequestFromResponse_HiddenFields(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="hidden" name="csrf" value="abc123">
			<input type="hidden" name="session" value="xyz789">
			<input type="text" name="data" value="">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	if !strings.Contains(body, "csrf=abc123") {
		t.Errorf("body should contain csrf=abc123, got: %s", body)
	}
	if !strings.Contains(body, "session=xyz789") {
		t.Errorf("body should contain session=xyz789, got: %s", body)
	}
}

func TestFormRequestFromResponse_SelectField(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<select name="color">
				<option value="red">Red</option>
				<option value="blue" selected>Blue</option>
				<option value="green">Green</option>
			</select>
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	if !strings.Contains(body, "color=blue") {
		t.Errorf("body should contain color=blue (selected option), got: %s", body)
	}
}

func TestFormRequestFromResponse_SelectFieldNoSelected(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<select name="color">
				<option value="red">Red</option>
				<option value="blue">Blue</option>
			</select>
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	// 没有 selected 时使用第一个 option
	if !strings.Contains(body, "color=red") {
		t.Errorf("body should contain color=red (first option), got: %s", body)
	}
}

func TestFormRequestFromResponse_TextareaField(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<textarea name="comment">Hello World</textarea>
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	if !strings.Contains(body, "comment=Hello") {
		t.Errorf("body should contain comment text, got: %s", body)
	}
}

func TestFormRequestFromResponse_CheckboxChecked(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="checkbox" name="agree" value="yes" checked>
			<input type="checkbox" name="newsletter" value="yes">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	// 只有 checked 的 checkbox 应被包含
	if !strings.Contains(body, "agree=yes") {
		t.Errorf("body should contain agree=yes (checked), got: %s", body)
	}
	if strings.Contains(body, "newsletter") {
		t.Errorf("body should not contain newsletter (unchecked), got: %s", body)
	}
}

func TestFormRequestFromResponse_RadioChecked(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="radio" name="gender" value="male">
			<input type="radio" name="gender" value="female" checked>
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	if !strings.Contains(body, "gender=female") {
		t.Errorf("body should contain gender=female (checked), got: %s", body)
	}
	if strings.Contains(body, "gender=male") {
		t.Errorf("body should not contain gender=male (unchecked), got: %s", body)
	}
}

func TestFormRequestFromResponse_SkipSubmitImageReset(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="text" name="field" value="value">
			<input type="submit" name="sub" value="Submit">
			<input type="image" name="img" src="btn.png">
			<input type="reset" name="rst" value="Reset">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	if !strings.Contains(body, "field=value") {
		t.Errorf("body should contain field=value, got: %s", body)
	}
	// submit/image/reset 不应作为普通字段包含
	if strings.Contains(body, "sub=") || strings.Contains(body, "img=") || strings.Contains(body, "rst=") {
		t.Errorf("body should not contain submit/image/reset inputs, got: %s", body)
	}
}

func TestFormRequestFromResponse_InputWithoutName(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="text" value="no-name">
			<input type="text" name="named" value="has-name">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	if !strings.Contains(body, "named=has-name") {
		t.Errorf("body should contain named=has-name, got: %s", body)
	}
	// 没有 name 的 input 不应被包含
	if strings.Contains(body, "no-name") {
		t.Errorf("body should not contain input without name, got: %s", body)
	}
}

func TestFormRequestFromResponse_InvalidMethod(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="DELETE">
			<input type="text" name="field" value="value">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 无效方法应回退为 GET
	if req.Method != "GET" {
		t.Errorf("expected GET for invalid method, got %s", req.Method)
	}
}

func TestFormRequestFromResponse_ButtonSubmit(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="text" name="field" value="value">
			<button name="btn" value="clicked">Submit</button>
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	// button 默认 type=submit，应被包含
	if !strings.Contains(body, "btn=clicked") {
		t.Errorf("body should contain btn=clicked, got: %s", body)
	}
}

func TestFormRequestFromResponse_EmptyAction(t *testing.T) {
	html := `<html><body>
		<form action="" method="POST">
			<input type="text" name="field" value="value">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/current", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 空 action 应使用当前页面 URL
	if req.URL.String() != "https://example.com/current" {
		t.Errorf("expected current page URL, got %s", req.URL.String())
	}
}

func TestFormRequestFromResponse_ComplexForm(t *testing.T) {
	html := `<html><body>
		<form action="/complex" method="POST">
			<input type="hidden" name="csrf_token" value="abc123">
			<input type="text" name="username" value="">
			<input type="password" name="password" value="">
			<select name="role">
				<option value="user">User</option>
				<option value="admin" selected>Admin</option>
			</select>
			<textarea name="bio">Hello</textarea>
			<input type="checkbox" name="agree" value="1" checked>
			<input type="checkbox" name="newsletter" value="1">
			<input type="radio" name="plan" value="free">
			<input type="radio" name="plan" value="pro" checked>
			<input type="submit" name="action" value="register">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp,
		WithFormResponseData(map[string][]string{
			"username": {"testuser"},
			"password": {"testpass"},
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)

	// 用户提供的数据
	if !strings.Contains(body, "username=testuser") {
		t.Errorf("body should contain username=testuser, got: %s", body)
	}
	if !strings.Contains(body, "password=testpass") {
		t.Errorf("body should contain password=testpass, got: %s", body)
	}

	// 隐藏字段
	if !strings.Contains(body, "csrf_token=abc123") {
		t.Errorf("body should contain csrf_token=abc123, got: %s", body)
	}

	// select 字段
	if !strings.Contains(body, "role=admin") {
		t.Errorf("body should contain role=admin, got: %s", body)
	}

	// checked checkbox
	if !strings.Contains(body, "agree=1") {
		t.Errorf("body should contain agree=1, got: %s", body)
	}

	// checked radio
	if !strings.Contains(body, "plan=pro") {
		t.Errorf("body should contain plan=pro, got: %s", body)
	}

	// submit button
	if !strings.Contains(body, "action=register") {
		t.Errorf("body should contain action=register, got: %s", body)
	}
}

// ============================================================================
// NewMultipartFormRequest 测试（P3-012c）
// ============================================================================

func TestNewMultipartFormRequest_Basic(t *testing.T) {
	fields := []FormField{
		{Name: "title", Value: "My Photo"},
		{Name: "description", Value: "A nice photo"},
	}
	files := []FormFile{
		{FieldName: "file", FileName: "photo.jpg", Content: []byte("fake-image-data")},
	}

	req, err := NewMultipartFormRequest("https://example.com/upload", fields, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 默认 POST
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}

	// Content-Type 应为 multipart/form-data
	ct := req.Headers.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
		t.Errorf("expected multipart/form-data, got %s", ct)
	}

	// 解析 multipart body
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("failed to parse Content-Type: %v", err)
	}
	if mediaType != "multipart/form-data" {
		t.Errorf("expected multipart/form-data, got %s", mediaType)
	}

	reader := multipart.NewReader(bytes.NewReader(req.Body), params["boundary"])

	// 读取所有 parts
	var parts []struct {
		name     string
		filename string
		content  string
	}

	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		var buf bytes.Buffer
		buf.ReadFrom(part)
		parts = append(parts, struct {
			name     string
			filename string
			content  string
		}{
			name:     part.FormName(),
			filename: part.FileName(),
			content:  buf.String(),
		})
	}

	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}

	// 验证字段
	if parts[0].name != "title" || parts[0].content != "My Photo" {
		t.Errorf("unexpected first field: %+v", parts[0])
	}
	if parts[1].name != "description" || parts[1].content != "A nice photo" {
		t.Errorf("unexpected second field: %+v", parts[1])
	}

	// 验证文件
	if parts[2].name != "file" || parts[2].filename != "photo.jpg" || parts[2].content != "fake-image-data" {
		t.Errorf("unexpected file part: %+v", parts[2])
	}
}

func TestNewMultipartFormRequest_MultipleFiles(t *testing.T) {
	files := []FormFile{
		{FieldName: "files", FileName: "a.txt", Content: []byte("content-a")},
		{FieldName: "files", FileName: "b.txt", Content: []byte("content-b")},
	}

	req, err := NewMultipartFormRequest("https://example.com/upload", nil, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ct := req.Headers.Get("Content-Type")
	_, params, _ := mime.ParseMediaType(ct)
	reader := multipart.NewReader(bytes.NewReader(req.Body), params["boundary"])

	fileCount := 0
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		if part.FileName() != "" {
			fileCount++
		}
	}

	if fileCount != 2 {
		t.Errorf("expected 2 files, got %d", fileCount)
	}
}

func TestNewMultipartFormRequest_NoFiles(t *testing.T) {
	fields := []FormField{
		{Name: "key", Value: "value"},
	}

	req, err := NewMultipartFormRequest("https://example.com/upload", fields, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ct := req.Headers.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
		t.Errorf("expected multipart/form-data, got %s", ct)
	}
}

func TestNewMultipartFormRequest_NoFields(t *testing.T) {
	files := []FormFile{
		{FieldName: "file", FileName: "test.txt", Content: []byte("hello")},
	}

	req, err := NewMultipartFormRequest("https://example.com/upload", nil, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
}

func TestNewMultipartFormRequest_CustomContentType(t *testing.T) {
	files := []FormFile{
		{FieldName: "file", FileName: "data.bin", Content: []byte("binary"), ContentType: "application/custom"},
	}

	req, err := NewMultipartFormRequest("https://example.com/upload", nil, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 解析 body 验证文件的 Content-Type
	ct := req.Headers.Get("Content-Type")
	_, params, _ := mime.ParseMediaType(ct)
	reader := multipart.NewReader(bytes.NewReader(req.Body), params["boundary"])

	part, err := reader.NextPart()
	if err != nil {
		t.Fatalf("failed to read part: %v", err)
	}

	partCT := part.Header.Get("Content-Type")
	if partCT != "application/custom" {
		t.Errorf("expected application/custom, got %s", partCT)
	}
}

func TestNewMultipartFormRequest_WithMethodOverride(t *testing.T) {
	req, err := NewMultipartFormRequest("https://example.com/upload",
		nil,
		[]FormFile{{FieldName: "file", FileName: "test.txt", Content: []byte("hello")}},
		WithMethod("PUT"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "PUT" {
		t.Errorf("expected PUT, got %s", req.Method)
	}
}

func TestNewMultipartFormRequest_InvalidURL(t *testing.T) {
	_, err := NewMultipartFormRequest("://invalid", nil, nil)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestMustNewMultipartFormRequest(t *testing.T) {
	req := MustNewMultipartFormRequest("https://example.com/upload",
		[]FormField{{Name: "key", Value: "value"}},
		[]FormFile{{FieldName: "file", FileName: "test.txt", Content: []byte("hello")}},
	)
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
}

func TestMustNewMultipartFormRequest_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid URL")
		}
	}()
	MustNewMultipartFormRequest("://invalid", nil, nil)
}

// ============================================================================
// inferContentType 测试
// ============================================================================

func TestInferContentType(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"image.png", "image/png"},
		{"animation.gif", "image/gif"},
		{"document.pdf", "application/pdf"},
		{"archive.zip", "application/zip"},
		{"data.json", "application/json"},
		{"page.html", "text/html"},
		{"style.css", "text/css"},
		{"script.js", "application/javascript"},
		{"readme.txt", "text/plain"},
		{"data.csv", "text/csv"},
		{"unknown.xyz", "application/octet-stream"},
		{"noext", "application/octet-stream"},
		{"IMAGE.PNG", "image/png"}, // 大小写不敏感
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := inferContentType(tt.filename)
			if result != tt.expected {
				t.Errorf("inferContentType(%q) = %q, want %q", tt.filename, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// 边界情况测试
// ============================================================================

func TestFormRequestFromResponse_MultipleFormsDefaultFirst(t *testing.T) {
	html := `<html><body>
		<form action="/first" method="POST">
			<input type="text" name="a" value="1">
		</form>
		<form action="/second" method="POST">
			<input type="text" name="b" value="2">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 默认使用第一个表单
	if req.URL.Path != "/first" {
		t.Errorf("expected /first (default first form), got %s", req.URL.Path)
	}
}

func TestFormRequestFromResponse_FormWithAbsoluteAction(t *testing.T) {
	html := `<html><body>
		<form action="https://other.com/submit" method="POST">
			<input type="text" name="field" value="value">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp, WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.String() != "https://other.com/submit" {
		t.Errorf("expected https://other.com/submit, got %s", req.URL.String())
	}
}

func TestFormRequestFromResponse_FormCSSInsideForm(t *testing.T) {
	html := `<html><body>
		<form action="/outer" method="POST">
			<div class="wrapper">
				<input type="text" name="field" value="value">
			</div>
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	// CSS 选择器匹配表单内部元素，应向上查找 <form>
	req, err := FormRequestFromResponse(resp, WithFormCSS("div.wrapper"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.URL.Path != "/outer" {
		t.Errorf("expected /outer, got %s", req.URL.Path)
	}
}

func TestNewMultipartFormRequest_EmptyFieldsAndFiles(t *testing.T) {
	req, err := NewMultipartFormRequest("https://example.com/upload", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}

	ct := req.Headers.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
		t.Errorf("expected multipart/form-data, got %s", ct)
	}
}

func TestNewMultipartFormRequest_LargeFile(t *testing.T) {
	// 创建 1MB 的文件内容
	largeContent := bytes.Repeat([]byte("x"), 1024*1024)
	files := []FormFile{
		{FieldName: "file", FileName: "large.bin", Content: largeContent},
	}

	req, err := NewMultipartFormRequest("https://example.com/upload", nil, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Body 应该比文件内容大（包含 multipart 边界和头部）
	if len(req.Body) <= len(largeContent) {
		t.Errorf("body should be larger than file content")
	}
}

func TestNewMultipartFormRequest_WithRequestOptions(t *testing.T) {
	req, err := NewMultipartFormRequest("https://example.com/upload",
		[]FormField{{Name: "key", Value: "value"}},
		nil,
		WithPriority(5),
		WithMeta(map[string]any{"upload": true}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Priority != 5 {
		t.Errorf("expected priority 5, got %d", req.Priority)
	}
	if v, ok := req.GetMeta("upload"); !ok || v != true {
		t.Errorf("expected meta upload=true")
	}
}

// ============================================================================
// locateForm 内部函数测试
// ============================================================================

func TestLocateForm_FormNamePriority(t *testing.T) {
	// 验证定位优先级：formname > formid > formxpath > formnumber
	html := `<html><body>
		<form name="target" id="other" action="/by-name" method="POST">
			<input type="text" name="a" value="1">
		</form>
		<form name="other" id="target" action="/by-id" method="POST">
			<input type="text" name="b" value="2">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)

	// formname 优先
	req, err := FormRequestFromResponse(resp, WithFormName("target"), WithDontClick())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.URL.Path != "/by-name" {
		t.Errorf("expected /by-name (formname priority), got %s", req.URL.Path)
	}
}

// ============================================================================
// matchClickdata 测试
// ============================================================================

func TestFormRequestFromResponse_ClickdataNoMatch(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="text" name="field" value="value">
			<input type="submit" name="action" value="save">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp,
		WithClickButton(map[string]string{"name": "nonexistent"}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	// 没有匹配的按钮，不应包含任何按钮数据
	if strings.Contains(body, "action=") {
		t.Errorf("body should not contain any button data when clickdata doesn't match, got: %s", body)
	}
}

func TestFormRequestFromResponse_NoClickableButtons(t *testing.T) {
	html := `<html><body>
		<form action="/submit" method="POST">
			<input type="text" name="field" value="value">
		</form>
	</body></html>`

	resp := newTestResponse("https://example.com/", html)
	req, err := FormRequestFromResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(req.Body)
	if !strings.Contains(body, "field=value") {
		t.Errorf("body should contain field=value, got: %s", body)
	}
}

// ============================================================================
// 集成测试：FormRequestFromResponse + NewMultipartFormRequest 组合
// ============================================================================

func TestFormRequestFromResponse_FullWorkflow(t *testing.T) {
	// 模拟一个完整的登录表单场景
	html := `<!DOCTYPE html>
	<html>
	<head><title>Login</title></head>
	<body>
		<form id="login-form" action="/api/login" method="POST">
			<input type="hidden" name="_token" value="csrf-abc-123">
			<input type="text" name="email" value="" placeholder="Email">
			<input type="password" name="password" value="" placeholder="Password">
			<select name="remember">
				<option value="0">No</option>
				<option value="1" selected>Yes</option>
			</select>
			<input type="checkbox" name="agree_tos" value="1" checked>
			<button type="submit" name="login" value="1">Login</button>
			<button type="button" name="cancel" value="1">Cancel</button>
		</form>
	</body>
	</html>`

	resp := newTestResponse("https://example.com/login", html)
	req, err := FormRequestFromResponse(resp,
		WithFormID("login-form"),
		WithFormResponseData(map[string][]string{
			"email":    {"user@example.com"},
			"password": {"secret123"},
		}),
		WithRequestOptions(
			WithPriority(10),
			WithCallback(NoCallback),
		),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证请求属性
	if req.URL.String() != "https://example.com/api/login" {
		t.Errorf("unexpected URL: %s", req.URL.String())
	}
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.Priority != 10 {
		t.Errorf("expected priority 10, got %d", req.Priority)
	}
	if !IsNoCallback(req.Callback) {
		t.Error("expected NoCallback")
	}

	body := string(req.Body)

	// 验证所有字段
	expectedFields := map[string]string{
		"_token":    "csrf-abc-123",
		"email":     "user%40example.com", // URL 编码
		"password":  "secret123",
		"remember":  "1",
		"agree_tos": "1",
		"login":     "1",
	}

	for field, value := range expectedFields {
		if !strings.Contains(body, fmt.Sprintf("%s=%s", field, value)) {
			t.Errorf("body should contain %s=%s, got: %s", field, value, body)
		}
	}

	// cancel 按钮（type=button）不应被包含
	if strings.Contains(body, "cancel=") {
		t.Errorf("body should not contain cancel button, got: %s", body)
	}
}
