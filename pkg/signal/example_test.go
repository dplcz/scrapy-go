package signal_test

import (
	"fmt"

	"github.com/dplcz/scrapy-go/pkg/signal"
)

// ExampleNewManager 演示创建信号管理器并注册/发送信号。
func ExampleNewManager() {
	// 创建信号管理器
	mgr := signal.NewManager(nil)

	// 注册 SpiderOpened 信号处理器
	id := mgr.Connect(func(params map[string]any) error {
		spiderName := params["spider_name"]
		fmt.Printf("Spider opened: %s\n", spiderName)
		return nil
	}, signal.SpiderOpened)

	// 发送信号
	mgr.SendCatchLog(signal.SpiderOpened, map[string]any{
		"spider_name": "myspider",
	})

	// 注销处理器
	mgr.Disconnect(id, signal.SpiderOpened)

	// 再次发送，无输出
	mgr.SendCatchLog(signal.SpiderOpened, map[string]any{
		"spider_name": "myspider",
	})

	fmt.Println("Done")

	// Output:
	// Spider opened: myspider
	// Done
}

// ExampleManager_Send 演示同步发送信号并收集错误。
func ExampleManager_Send() {
	mgr := signal.NewManager(nil)

	// 注册多个处理器
	mgr.Connect(func(params map[string]any) error {
		fmt.Println("Handler 1")
		return nil
	}, signal.ItemScraped)

	mgr.Connect(func(params map[string]any) error {
		fmt.Println("Handler 2")
		return fmt.Errorf("handler 2 error")
	}, signal.ItemScraped)

	mgr.Connect(func(params map[string]any) error {
		fmt.Println("Handler 3") // 即使 Handler 2 出错，Handler 3 仍会执行
		return nil
	}, signal.ItemScraped)

	// 发送信号
	errs := mgr.Send(signal.ItemScraped, nil)
	fmt.Println("Errors:", len(errs))

	// Output:
	// Handler 1
	// Handler 2
	// Handler 3
	// Errors: 1
}
