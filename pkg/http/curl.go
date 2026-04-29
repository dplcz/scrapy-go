// Package http 定义了 scrapy-go 框架的 HTTP 请求和响应模型。
package http

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// FromCURL 从 curl 命令字符串创建 Request 对象。
//
// 解析 curl 命令中的 URL、HTTP 方法、请求头、Cookie 和请求体。
// 支持以下 curl 选项：
//   - -X, --request: HTTP 方法
//   - -H, --header: 请求头
//   - -b, --cookie: Cookie
//   - -d, --data, --data-raw: 请求体
//   - -u, --user: Basic Auth 认证
//   - -A, --user-agent: User-Agent
//   - --compressed, -s, --silent, -v, --verbose: 安全忽略
//
// 如果 curl 命令中指定了 --data 但未指定 -X，则默认使用 POST 方法。
// 如果 URL 缺少 scheme，自动添加 "http://"。
//
// 对齐 Scrapy 的 Request.from_curl() 类方法。
//
// 用法：
//
//	req, err := http.FromCURL(`curl 'https://example.com/api' -H 'Content-Type: application/json' -d '{"key":"value"}'`)
//	req, err := http.FromCURL(`curl -X POST https://example.com -H 'Authorization: Bearer token'`,
//	    http.WithPriority(10),
//	)
func FromCURL(curlCommand string, opts ...RequestOption) (*Request, error) {
	kwargs, err := curlToRequestKwargs(curlCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to parse curl command: %w", err)
	}

	// 构建 Request 选项
	var reqOpts []RequestOption

	// Method
	if method, ok := kwargs["method"].(string); ok {
		reqOpts = append(reqOpts, WithMethod(method))
	}

	// Headers
	if headers, ok := kwargs["headers"].(http.Header); ok {
		reqOpts = append(reqOpts, WithHeaders(headers))
	}

	// Cookies
	if cookies, ok := kwargs["cookies"].([]*http.Cookie); ok && len(cookies) > 0 {
		reqOpts = append(reqOpts, WithCookies(cookies))
	}

	// Body
	if body, ok := kwargs["body"].(string); ok && body != "" {
		reqOpts = append(reqOpts, WithBody([]byte(body)))
	}

	// 用户选项在后面，可以覆盖解析出的值
	reqOpts = append(reqOpts, opts...)

	// URL
	rawURL, ok := kwargs["url"].(string)
	if !ok || rawURL == "" {
		return nil, fmt.Errorf("no URL found in curl command")
	}

	return NewRequest(rawURL, reqOpts...)
}

// curlToRequestKwargs 将 curl 命令解析为 Request 参数字典。
//
// 自实现轻量级 shell 词法分析器，替代 Scrapy 的 shlex.split + argparse。
func curlToRequestKwargs(curlCommand string) (map[string]any, error) {
	args, err := shellSplit(curlCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to tokenize curl command: %w", err)
	}

	if len(args) == 0 {
		return nil, fmt.Errorf("empty curl command")
	}

	// 第一个参数必须是 "curl"
	if args[0] != "curl" {
		return nil, fmt.Errorf("a curl command must start with \"curl\"")
	}
	args = args[1:]

	result := map[string]any{}
	var (
		rawURL     string
		method     string
		headerList []headerPair
		cookies    = make(map[string]string)
		data       string
		authUser   string
		hasData    bool
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "-X" || arg == "--request":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option %s requires an argument", arg)
			}
			i++
			method = strings.ToUpper(args[i])

		case arg == "-H" || arg == "--header":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option %s requires an argument", arg)
			}
			i++
			headerStr := args[i]
			colonIdx := strings.Index(headerStr, ":")
			if colonIdx < 0 {
				continue // 忽略无效的 header
			}
			name := strings.TrimSpace(headerStr[:colonIdx])
			value := strings.TrimSpace(headerStr[colonIdx+1:])
			headerList = append(headerList, headerPair{name: name, value: value})

		case arg == "-b" || arg == "--cookie":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option %s requires an argument", arg)
			}
			i++
			cookieStr := args[i]
			// 解析 "key=value; key2=value2" 格式
			parseCookieString(cookieStr, cookies)

		case arg == "-d" || arg == "--data" || arg == "--data-raw":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option %s requires an argument", arg)
			}
			i++
			data = args[i]
			// 移除可能的 $ 前缀（某些 curl 导出格式）
			data = strings.TrimPrefix(data, "$")
			hasData = true

		case arg == "-u" || arg == "--user":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option %s requires an argument", arg)
			}
			i++
			authUser = args[i]

		case arg == "-A" || arg == "--user-agent":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("option %s requires an argument", arg)
			}
			i++
			headerList = append(headerList, headerPair{name: "User-Agent", value: args[i]})

		// 安全忽略的选项（不影响请求语义）
		case arg == "--compressed",
			arg == "-s", arg == "--silent",
			arg == "-v", arg == "--verbose",
			arg == "-#", arg == "--progress-bar",
			arg == "-k", arg == "--insecure",
			arg == "-L", arg == "--location",
			arg == "-i", arg == "--include":
			// 忽略

		case strings.HasPrefix(arg, "-"):
			// 未知选项，忽略（对齐 Scrapy 的 ignore_unknown_options=True 默认行为）
			// 如果选项后面跟着一个值参数，也需要跳过
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++ // 跳过值参数
			}

		default:
			// 非选项参数视为 URL
			if rawURL == "" {
				rawURL = arg
			}
		}
	}

	if rawURL == "" {
		return nil, fmt.Errorf("no URL found in curl command")
	}

	// 如果 URL 缺少 scheme，自动添加 http://
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}

	// 确定 HTTP 方法
	if method == "" {
		if hasData {
			method = "POST"
		} else {
			method = "GET"
		}
	}

	result["url"] = rawURL
	result["method"] = method

	// 处理 headers 和从 header 中提取的 cookies
	headers := make(http.Header)
	for _, hp := range headerList {
		if strings.EqualFold(hp.name, "Cookie") {
			// Cookie header 中的值解析为独立的 cookies
			parseCookieString(hp.value, cookies)
		} else {
			headers.Add(hp.name, hp.value)
		}
	}

	// 处理 Basic Auth
	if authUser != "" {
		parts := strings.SplitN(authUser, ":", 2)
		user := parts[0]
		pass := ""
		if len(parts) > 1 {
			pass = parts[1]
		}
		// 生成 Basic Auth header
		authValue := "Basic " + basicAuth(user, pass)
		headers.Set("Authorization", authValue)
	}

	if len(headers) > 0 {
		result["headers"] = headers
	}

	if len(cookies) > 0 {
		cookieSlice := make([]*http.Cookie, 0, len(cookies))
		for name, value := range cookies {
			cookieSlice = append(cookieSlice, &http.Cookie{
				Name:  name,
				Value: value,
			})
		}
		result["cookies"] = cookieSlice
	}

	if hasData {
		result["body"] = data
	}

	return result, nil
}

// headerPair 是一个临时的 header 名值对。
type headerPair struct {
	name  string
	value string
}

// parseCookieString 解析 "key=value; key2=value2" 格式的 cookie 字符串。
func parseCookieString(cookieStr string, cookies map[string]string) {
	pairs := strings.Split(cookieStr, ";")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eqIdx := strings.Index(pair, "=")
		if eqIdx < 0 {
			continue // 忽略没有 = 的 cookie
		}
		name := strings.TrimSpace(pair[:eqIdx])
		value := strings.TrimSpace(pair[eqIdx+1:])
		if name != "" {
			cookies[name] = value
		}
	}
}

// basicAuth 生成 base64 编码的 Basic Auth 凭证。
func basicAuth(user, pass string) string {
	credentials := user + ":" + pass
	return base64.StdEncoding.EncodeToString([]byte(credentials))
}

// ============================================================================
// Shell 词法分析器
// ============================================================================

// shellSplit 将 shell 命令字符串分割为参数列表。
//
// 支持单引号、双引号和反斜杠转义。
// 这是 Go 版本的 Python shlex.split() 实现。
//
// 规则：
//   - 单引号内的内容原样保留（不处理转义）
//   - 双引号内支持 \" 和 \\ 转义
//   - 引号外的反斜杠转义下一个字符
//   - 空白字符（空格、制表符、换行符）分隔参数
func shellSplit(s string) ([]string, error) {
	var (
		args    []string
		current strings.Builder
		inArg   bool
	)

	i := 0
	for i < len(s) {
		c := s[i]

		switch {
		case c == '\'':
			// 单引号：查找匹配的结束引号
			i++
			for i < len(s) && s[i] != '\'' {
				current.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("unmatched single quote")
			}
			i++ // 跳过结束引号
			inArg = true

		case c == '"':
			// 双引号：支持 \" 和 \\ 转义
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					next := s[i+1]
					if next == '"' || next == '\\' || next == '$' || next == '`' {
						current.WriteByte(next)
						i += 2
						continue
					}
				}
				current.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("unmatched double quote")
			}
			i++ // 跳过结束引号
			inArg = true

		case c == '\\':
			// 反斜杠转义
			if i+1 < len(s) {
				current.WriteByte(s[i+1])
				i += 2
			} else {
				return nil, fmt.Errorf("trailing backslash")
			}
			inArg = true

		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			// 空白分隔符
			if inArg {
				args = append(args, current.String())
				current.Reset()
				inArg = false
			}
			i++

		default:
			current.WriteByte(c)
			i++
			inArg = true
		}
	}

	// 处理最后一个参数
	if inArg {
		args = append(args, current.String())
	}

	return args, nil
}
