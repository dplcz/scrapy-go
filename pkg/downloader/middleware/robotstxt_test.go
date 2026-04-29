package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
	"github.com/dplcz/scrapy-go/pkg/stats"
)

// ============================================================================
// parseRobotsTxt 单元测试
// ============================================================================

func TestParseRobotsTxt_Basic(t *testing.T) {
	content := `
User-agent: *
Disallow: /admin/
Disallow: /private/
Allow: /admin/public/
`
	rules := parseRobotsTxt(content)

	if len(rules.groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(rules.groups))
	}

	group := rules.groups[0]
	if len(group.agents) != 1 || group.agents[0] != "*" {
		t.Errorf("expected agent '*', got %v", group.agents)
	}
	if len(group.disallow) != 2 {
		t.Errorf("expected 2 disallow rules, got %d", len(group.disallow))
	}
	if len(group.allow) != 1 {
		t.Errorf("expected 1 allow rule, got %d", len(group.allow))
	}
}

func TestParseRobotsTxt_MultipleGroups(t *testing.T) {
	content := `
User-agent: Googlebot
Disallow: /nogoogle/

User-agent: *
Disallow: /private/
`
	rules := parseRobotsTxt(content)

	if len(rules.groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(rules.groups))
	}

	if rules.groups[0].agents[0] != "googlebot" {
		t.Errorf("expected first group agent 'googlebot', got %q", rules.groups[0].agents[0])
	}
	if rules.groups[1].agents[0] != "*" {
		t.Errorf("expected second group agent '*', got %q", rules.groups[1].agents[0])
	}
}

func TestParseRobotsTxt_Comments(t *testing.T) {
	content := `
# This is a comment
User-agent: * # all bots
Disallow: /secret/ # keep out
Allow: /secret/public/ # but this is ok
`
	rules := parseRobotsTxt(content)

	if len(rules.groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(rules.groups))
	}

	if len(rules.groups[0].disallow) != 1 {
		t.Errorf("expected 1 disallow rule, got %d", len(rules.groups[0].disallow))
	}
	if rules.groups[0].disallow[0] != "/secret/" {
		t.Errorf("expected disallow '/secret/', got %q", rules.groups[0].disallow[0])
	}
}

func TestParseRobotsTxt_EmptyDisallow(t *testing.T) {
	content := `
User-agent: *
Disallow:
`
	rules := parseRobotsTxt(content)

	if len(rules.groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(rules.groups))
	}

	// 空 Disallow 表示允许所有
	if len(rules.groups[0].disallow) != 0 {
		t.Errorf("expected 0 disallow rules for empty Disallow, got %d", len(rules.groups[0].disallow))
	}
}

func TestParseRobotsTxt_MultipleUserAgents(t *testing.T) {
	content := `
User-agent: Googlebot
User-agent: Bingbot
Disallow: /private/
`
	rules := parseRobotsTxt(content)

	if len(rules.groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(rules.groups))
	}

	if len(rules.groups[0].agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(rules.groups[0].agents))
	}
}

// ============================================================================
// isAllowed 单元测试
// ============================================================================

func TestRobotsRules_IsAllowed_BasicDisallow(t *testing.T) {
	rules := parseRobotsTxt(`
User-agent: *
Disallow: /admin/
Disallow: /private/
`)

	tests := []struct {
		path    string
		ua      string
		allowed bool
	}{
		{"/", "scrapy-go", true},
		{"/public/page.html", "scrapy-go", true},
		{"/admin/", "scrapy-go", false},
		{"/admin/settings", "scrapy-go", false},
		{"/private/data", "scrapy-go", false},
		{"/admins", "scrapy-go", true}, // 不是 /admin/ 的前缀
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.path, tt.ua), func(t *testing.T) {
			result := rules.isAllowed(tt.path, tt.ua)
			if result != tt.allowed {
				t.Errorf("isAllowed(%q, %q) = %v, want %v", tt.path, tt.ua, result, tt.allowed)
			}
		})
	}
}

func TestRobotsRules_IsAllowed_AllowOverridesDisallow(t *testing.T) {
	rules := parseRobotsTxt(`
User-agent: *
Disallow: /admin/
Allow: /admin/public/
`)

	tests := []struct {
		path    string
		allowed bool
	}{
		{"/admin/", false},
		{"/admin/settings", false},
		{"/admin/public/", true},
		{"/admin/public/page.html", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := rules.isAllowed(tt.path, "scrapy-go")
			if result != tt.allowed {
				t.Errorf("isAllowed(%q) = %v, want %v", tt.path, result, tt.allowed)
			}
		})
	}
}

func TestRobotsRules_IsAllowed_SpecificUserAgent(t *testing.T) {
	rules := parseRobotsTxt(`
User-agent: Googlebot
Disallow: /nogoogle/

User-agent: *
Disallow: /private/
`)

	tests := []struct {
		path    string
		ua      string
		allowed bool
	}{
		{"/nogoogle/page", "Googlebot/2.1", false},
		{"/nogoogle/page", "scrapy-go", true},
		{"/private/data", "Googlebot/2.1", true}, // Googlebot 组没有禁止 /private/
		{"/private/data", "scrapy-go", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.path, tt.ua), func(t *testing.T) {
			result := rules.isAllowed(tt.path, tt.ua)
			if result != tt.allowed {
				t.Errorf("isAllowed(%q, %q) = %v, want %v", tt.path, tt.ua, result, tt.allowed)
			}
		})
	}
}

func TestRobotsRules_IsAllowed_Wildcard(t *testing.T) {
	rules := parseRobotsTxt(`
User-agent: *
Disallow: /search*result
Disallow: /*.json$
`)

	tests := []struct {
		path    string
		allowed bool
	}{
		{"/search/result", false},
		{"/search_result", false},
		{"/searchXresult", false},
		{"/data.json", false},
		{"/api/data.json", false},
		{"/data.json.bak", true}, // $ 锚定，不匹配
		{"/search", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := rules.isAllowed(tt.path, "scrapy-go")
			if result != tt.allowed {
				t.Errorf("isAllowed(%q) = %v, want %v", tt.path, result, tt.allowed)
			}
		})
	}
}

func TestRobotsRules_IsAllowed_DisallowAll(t *testing.T) {
	rules := parseRobotsTxt(`
User-agent: *
Disallow: /
`)

	if rules.isAllowed("/anything", "scrapy-go") {
		t.Error("expected all paths to be disallowed")
	}
	if rules.isAllowed("/", "scrapy-go") {
		t.Error("expected root to be disallowed")
	}
}

func TestRobotsRules_IsAllowed_AllowAll(t *testing.T) {
	rules := parseRobotsTxt(`
User-agent: *
Disallow:
`)

	if !rules.isAllowed("/anything", "scrapy-go") {
		t.Error("expected all paths to be allowed")
	}
}

func TestRobotsRules_IsAllowed_NoRules(t *testing.T) {
	rules := parseRobotsTxt("")

	if !rules.isAllowed("/anything", "scrapy-go") {
		t.Error("expected all paths to be allowed when no rules")
	}
}

// ============================================================================
// matchPath 单元测试
// ============================================================================

func TestMatchPath(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		match   bool
	}{
		{"/admin/page", "/admin/", true},
		{"/admin", "/admin/", false},
		{"/admin/", "/admin/", true},
		{"/public", "/admin/", false},
		{"/file.json", "/*.json$", true},
		{"/file.json.bak", "/*.json$", false},
		{"/search/result", "/search*result", true},
		{"/", "/", true},
		{"/anything", "/", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.path, tt.pattern), func(t *testing.T) {
			result := matchPath(tt.path, tt.pattern)
			if result != tt.match {
				t.Errorf("matchPath(%q, %q) = %v, want %v", tt.path, tt.pattern, result, tt.match)
			}
		})
	}
}

// ============================================================================
// RobotsTxtMiddleware 集成测试
// ============================================================================

func TestRobotsTxtMiddleware_ProcessRequest_Allowed(t *testing.T) {
	// 启动测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(200)
			fmt.Fprint(w, "User-agent: *\nDisallow: /private/\n")
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, "OK")
	}))
	defer server.Close()

	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRobotsTxtMiddleware(sc, nil,
		WithRobotsTxtHTTPClient(server.Client()),
		WithRobotsTxtDefaultUserAgent("test-bot"),
	)

	// 测试允许的路径
	req, _ := shttp.NewRequest(server.URL+"/public/page.html",
		shttp.WithHeader("User-Agent", "test-bot"),
	)

	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}

	// 验证统计
	reqCount := sc.GetValue("robotstxt/request_count", 0)
	if reqCount != 1 {
		t.Errorf("expected robotstxt/request_count=1, got %v", reqCount)
	}
}

func TestRobotsTxtMiddleware_ProcessRequest_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(200)
			fmt.Fprint(w, "User-agent: *\nDisallow: /private/\n")
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRobotsTxtMiddleware(sc, nil,
		WithRobotsTxtHTTPClient(server.Client()),
		WithRobotsTxtDefaultUserAgent("test-bot"),
	)

	// 测试禁止的路径
	req, _ := shttp.NewRequest(server.URL+"/private/secret.html",
		shttp.WithHeader("User-Agent", "test-bot"),
	)

	_, err := mw.ProcessRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for forbidden path")
	}

	// 验证是 ErrIgnoreRequest
	if !containsIgnoreRequest(err) {
		t.Errorf("expected ErrIgnoreRequest, got %v", err)
	}

	// 验证统计
	forbidden := sc.GetValue("robotstxt/forbidden", 0)
	if forbidden != 1 {
		t.Errorf("expected robotstxt/forbidden=1, got %v", forbidden)
	}
}

func TestRobotsTxtMiddleware_ProcessRequest_DontObeyMeta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(200)
			fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRobotsTxtMiddleware(sc, nil,
		WithRobotsTxtHTTPClient(server.Client()),
	)

	// 设置 dont_obey_robotstxt Meta
	req, _ := shttp.NewRequest(server.URL+"/private/page.html",
		shttp.WithMeta(map[string]any{"dont_obey_robotstxt": true}),
	)

	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error with dont_obey_robotstxt, got %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}

func TestRobotsTxtMiddleware_ProcessRequest_DataURL(t *testing.T) {
	mw := NewRobotsTxtMiddleware(nil, nil)

	req, _ := shttp.NewRequest("data:text/html,<h1>Hello</h1>")

	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error for data: URL, got %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}

func TestRobotsTxtMiddleware_ProcessRequest_FileURL(t *testing.T) {
	mw := NewRobotsTxtMiddleware(nil, nil)

	req, _ := shttp.NewRequest("file:///tmp/test.html")

	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error for file: URL, got %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}

func TestRobotsTxtMiddleware_ProcessRequest_NoRobotsTxt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRobotsTxtMiddleware(sc, nil,
		WithRobotsTxtHTTPClient(server.Client()),
	)

	// 没有 robots.txt 时应该允许所有请求
	req, _ := shttp.NewRequest(server.URL + "/anything")

	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error when no robots.txt, got %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}

func TestRobotsTxtMiddleware_ProcessRequest_RobotsTxtUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(200)
			fmt.Fprint(w, "User-agent: custom-bot\nDisallow: /private/\n\nUser-agent: *\nDisallow:\n")
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRobotsTxtMiddleware(sc, nil,
		WithRobotsTxtHTTPClient(server.Client()),
		WithRobotsTxtUserAgent("custom-bot"),
	)

	// 使用 ROBOTSTXT_USER_AGENT 匹配，应该被禁止
	req, _ := shttp.NewRequest(server.URL+"/private/page.html",
		shttp.WithHeader("User-Agent", "other-bot"),
	)

	_, err := mw.ProcessRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when ROBOTSTXT_USER_AGENT matches disallow rule")
	}
}

func TestRobotsTxtMiddleware_ConcurrentRequests(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			requestCount.Add(1)
			// 模拟网络延迟
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(200)
			fmt.Fprint(w, "User-agent: *\nDisallow: /private/\n")
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRobotsTxtMiddleware(sc, nil,
		WithRobotsTxtHTTPClient(server.Client()),
	)

	// 并发发送多个请求到同一域名
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req, _ := shttp.NewRequest(fmt.Sprintf("%s/page/%d", server.URL, i))
			mw.ProcessRequest(context.Background(), req)
		}(i)
	}
	wg.Wait()

	// 验证 robots.txt 只被下载一次
	count := requestCount.Load()
	if count != 1 {
		t.Errorf("expected robots.txt to be downloaded once, got %d times", count)
	}
}

func TestRobotsTxtMiddleware_MultipleDomains(t *testing.T) {
	// 创建两个测试服务器模拟不同域名
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(200)
			fmt.Fprint(w, "User-agent: *\nDisallow: /private/\n")
			return
		}
		w.WriteHeader(200)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(200)
			fmt.Fprint(w, "User-agent: *\nDisallow: /secret/\n")
			return
		}
		w.WriteHeader(200)
	}))
	defer server2.Close()

	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRobotsTxtMiddleware(sc, nil)

	// server1: /private/ 被禁止
	req1, _ := shttp.NewRequest(server1.URL + "/private/page.html")
	_, err := mw.ProcessRequest(context.Background(), req1)
	if err == nil {
		t.Error("expected error for server1 /private/")
	}

	// server1: /secret/ 允许
	req2, _ := shttp.NewRequest(server1.URL + "/secret/page.html")
	_, err = mw.ProcessRequest(context.Background(), req2)
	if err != nil {
		t.Errorf("expected no error for server1 /secret/, got %v", err)
	}

	// server2: /secret/ 被禁止
	req3, _ := shttp.NewRequest(server2.URL + "/secret/page.html")
	_, err = mw.ProcessRequest(context.Background(), req3)
	if err == nil {
		t.Error("expected error for server2 /secret/")
	}

	// server2: /private/ 允许
	req4, _ := shttp.NewRequest(server2.URL + "/private/page.html")
	_, err = mw.ProcessRequest(context.Background(), req4)
	if err != nil {
		t.Errorf("expected no error for server2 /private/, got %v", err)
	}
}

func TestRobotsTxtMiddleware_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRobotsTxtMiddleware(sc, nil,
		WithRobotsTxtHTTPClient(server.Client()),
	)

	// 服务器错误时应该允许访问
	req, _ := shttp.NewRequest(server.URL + "/anything")
	resp, err := mw.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error when robots.txt returns 500, got %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}

func TestRobotsTxtMiddleware_Stats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.WriteHeader(200)
			fmt.Fprint(w, "User-agent: *\nDisallow: /blocked/\n")
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	sc := stats.NewMemoryCollector(false, nil)
	mw := NewRobotsTxtMiddleware(sc, nil,
		WithRobotsTxtHTTPClient(server.Client()),
	)

	// 发送一个被禁止的请求
	req, _ := shttp.NewRequest(server.URL + "/blocked/page")
	mw.ProcessRequest(context.Background(), req)

	// 验证统计
	if v := sc.GetValue("robotstxt/request_count", 0); v != 1 {
		t.Errorf("expected robotstxt/request_count=1, got %v", v)
	}
	if v := sc.GetValue("robotstxt/response_count", 0); v != 1 {
		t.Errorf("expected robotstxt/response_count=1, got %v", v)
	}
	if v := sc.GetValue("robotstxt/response_status_count/200", 0); v != 1 {
		t.Errorf("expected robotstxt/response_status_count/200=1, got %v", v)
	}
	if v := sc.GetValue("robotstxt/forbidden", 0); v != 1 {
		t.Errorf("expected robotstxt/forbidden=1, got %v", v)
	}
}

func TestRobotsTxtMiddleware_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			// 模拟慢响应
			time.Sleep(2 * time.Second)
			w.WriteHeader(200)
			fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	mw := NewRobotsTxtMiddleware(nil, nil,
		WithRobotsTxtHTTPClient(&http.Client{Timeout: 100 * time.Millisecond}),
	)

	// 使用短超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, _ := shttp.NewRequest(server.URL + "/page")

	// 超时后应该允许访问（下载失败视为无 robots.txt）
	resp, err := mw.ProcessRequest(ctx, req)
	if err != nil {
		t.Fatalf("expected no error on timeout, got %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}
}

// containsIgnoreRequest 检查错误链中是否包含 ErrIgnoreRequest。
func containsIgnoreRequest(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() != "" && (err.Error() == "request ignored" ||
		len(err.Error()) > 16 && err.Error()[:16] == "request ignored:")
}
