package settings_test

import (
	"fmt"
	"time"

	"github.com/dplcz/scrapy-go/pkg/settings"
)

// ExampleNew 演示创建 Settings 并使用多优先级配置。
func ExampleNew() {
	// 创建 Settings（自动加载框架默认配置）
	s := settings.New()

	// 获取默认值
	fmt.Println("CONCURRENT_REQUESTS:", s.GetInt("CONCURRENT_REQUESTS", 0))
	fmt.Println("RETRY_ENABLED:", s.GetBool("RETRY_ENABLED", false))

	// 以 Project 优先级覆盖配置
	s.Set("CONCURRENT_REQUESTS", 32, settings.PriorityProject)
	fmt.Println("CONCURRENT_REQUESTS (after override):", s.GetInt("CONCURRENT_REQUESTS", 0))

	// 低优先级无法覆盖高优先级
	s.Set("CONCURRENT_REQUESTS", 8, settings.PriorityDefault)
	fmt.Println("CONCURRENT_REQUESTS (low priority ignored):", s.GetInt("CONCURRENT_REQUESTS", 0))

	// Output:
	// CONCURRENT_REQUESTS: 16
	// RETRY_ENABLED: true
	// CONCURRENT_REQUESTS (after override): 32
	// CONCURRENT_REQUESTS (low priority ignored): 32
}

// ExampleSettings_Freeze 演示配置冻结机制。
func ExampleSettings_Freeze() {
	s := settings.NewEmpty()
	s.Set("KEY", "value", settings.PriorityProject)

	// 冻结配置
	s.Freeze()
	fmt.Println("Frozen:", s.IsFrozen())

	// 冻结后修改会返回错误
	err := s.Set("KEY", "new_value", settings.PriorityCmdline)
	fmt.Println("Error:", err != nil)

	// 值未被修改
	fmt.Println("Value:", s.GetString("KEY", ""))

	// Output:
	// Frozen: true
	// Error: true
	// Value: value
}

// ExampleSettings_GetDuration 演示获取时间间隔配置。
func ExampleSettings_GetDuration() {
	s := settings.NewEmpty()

	// 支持多种格式
	s.Set("TIMEOUT_DURATION", 30*time.Second, settings.PriorityDefault)
	s.Set("TIMEOUT_INT", 60, settings.PriorityDefault)
	s.Set("TIMEOUT_STRING", "5m30s", settings.PriorityDefault)

	fmt.Println("Duration:", s.GetDuration("TIMEOUT_DURATION", 0))
	fmt.Println("Int (seconds):", s.GetDuration("TIMEOUT_INT", 0))
	fmt.Println("String:", s.GetDuration("TIMEOUT_STRING", 0))

	// Output:
	// Duration: 30s
	// Int (seconds): 1m0s
	// String: 5m30s
}

// ExampleSettings_GetComponentPriorityDictWithBase 演示组件优先级字典的合并与禁用。
func ExampleSettings_GetComponentPriorityDictWithBase() {
	s := settings.NewEmpty()

	// 设置基础中间件配置
	s.Set("DOWNLOADER_MIDDLEWARES_BASE", map[string]int{
		"Retry":    550,
		"Redirect": 600,
		"Cookies":  700,
	}, settings.PriorityDefault)

	// 用户覆盖：禁用 Cookies，添加自定义中间件
	s.Set("DOWNLOADER_MIDDLEWARES", map[string]int{
		"Cookies":      -1,  // 禁用
		"MyMiddleware": 800, // 添加
	}, settings.PriorityProject)

	result := s.GetComponentPriorityDictWithBase("DOWNLOADER_MIDDLEWARES")
	fmt.Println("Retry:", result["Retry"])
	fmt.Println("Redirect:", result["Redirect"])
	_, hasCookies := result["Cookies"]
	fmt.Println("Cookies disabled:", !hasCookies)
	fmt.Println("MyMiddleware:", result["MyMiddleware"])

	// Output:
	// Retry: 550
	// Redirect: 600
	// Cookies disabled: true
	// MyMiddleware: 800
}
