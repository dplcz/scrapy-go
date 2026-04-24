package signal

import (
	"context"
	"sync"
	"testing"

	scrapy_errors "scrapy-go/pkg/errors"
)

func TestManagerConnect(t *testing.T) {
	sm := NewManager(nil)

	called := false
	sm.Connect(func(params map[string]any) error {
		called = true
		return nil
	}, SpiderOpened)

	if !sm.HasHandlers(SpiderOpened) {
		t.Error("should have handlers for SpiderOpened")
	}
	if sm.HandlerCount(SpiderOpened) != 1 {
		t.Errorf("expected 1 handler, got %d", sm.HandlerCount(SpiderOpened))
	}

	sm.SendCatchLog(SpiderOpened, nil)
	if !called {
		t.Error("handler should have been called")
	}
}

func TestManagerDisconnect(t *testing.T) {
	sm := NewManager(nil)

	id := sm.Connect(func(params map[string]any) error {
		return nil
	}, SpiderOpened)

	if sm.HandlerCount(SpiderOpened) != 1 {
		t.Error("should have 1 handler")
	}

	sm.Disconnect(id, SpiderOpened)
	if sm.HandlerCount(SpiderOpened) != 0 {
		t.Error("should have 0 handlers after disconnect")
	}
}

func TestManagerDisconnectAll(t *testing.T) {
	sm := NewManager(nil)

	sm.Connect(func(params map[string]any) error { return nil }, SpiderOpened)
	sm.Connect(func(params map[string]any) error { return nil }, SpiderOpened)
	sm.Connect(func(params map[string]any) error { return nil }, SpiderOpened)

	if sm.HandlerCount(SpiderOpened) != 3 {
		t.Error("should have 3 handlers")
	}

	sm.DisconnectAll(SpiderOpened)
	if sm.HandlerCount(SpiderOpened) != 0 {
		t.Error("should have 0 handlers after disconnect all")
	}
}

func TestManagerSend(t *testing.T) {
	sm := NewManager(nil)

	var order []int
	sm.Connect(func(params map[string]any) error {
		order = append(order, 1)
		return nil
	}, EngineStarted)
	sm.Connect(func(params map[string]any) error {
		order = append(order, 2)
		return nil
	}, EngineStarted)
	sm.Connect(func(params map[string]any) error {
		order = append(order, 3)
		return nil
	}, EngineStarted)

	errs := sm.Send(EngineStarted, nil)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("handlers should be called in order: %v", order)
	}
}

func TestManagerSendWithParams(t *testing.T) {
	sm := NewManager(nil)

	var receivedParams map[string]any
	sm.Connect(func(params map[string]any) error {
		receivedParams = params
		return nil
	}, ItemScraped)

	sm.Send(ItemScraped, map[string]any{
		"item":   map[string]any{"name": "test"},
		"spider": "my_spider",
	})

	if receivedParams == nil {
		t.Fatal("params should not be nil")
	}
	if receivedParams["spider"] != "my_spider" {
		t.Errorf("unexpected spider param: %v", receivedParams["spider"])
	}
}

func TestManagerSendCatchLog(t *testing.T) {
	sm := NewManager(nil)

	// 处理器返回普通错误
	sm.Connect(func(params map[string]any) error {
		return scrapy_errors.ErrDownloadFailed
	}, SpiderError)

	errs := sm.SendCatchLog(SpiderError, nil)
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestManagerDontCloseSpider(t *testing.T) {
	sm := NewManager(nil)

	sm.Connect(func(params map[string]any) error {
		return scrapy_errors.ErrDontCloseSpider
	}, SpiderIdle)

	errs := sm.SendCatchLog(SpiderIdle, nil)
	if !ContainsDontCloseSpider(errs) {
		t.Error("should contain DontCloseSpider error")
	}
}

func TestManagerCloseSpider(t *testing.T) {
	sm := NewManager(nil)

	sm.Connect(func(params map[string]any) error {
		return scrapy_errors.NewCloseSpiderError("item_count_exceeded")
	}, SpiderIdle)

	errs := sm.SendCatchLog(SpiderIdle, nil)
	closeErr := ContainsCloseSpider(errs)
	if closeErr == nil {
		t.Fatal("should contain CloseSpider error")
	}
	if closeErr.Reason != "item_count_exceeded" {
		t.Errorf("unexpected reason: %s", closeErr.Reason)
	}
}

func TestManagerSendCatchLogCtx(t *testing.T) {
	sm := NewManager(nil)

	callCount := 0
	sm.Connect(func(params map[string]any) error {
		callCount++
		return nil
	}, EngineStarted)
	sm.Connect(func(params map[string]any) error {
		callCount++
		return nil
	}, EngineStarted)

	// 正常 context
	ctx := context.Background()
	errs := sm.SendCatchLogCtx(ctx, EngineStarted, nil)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}

	// 已取消的 context
	callCount = 0
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	errs = sm.SendCatchLogCtx(cancelCtx, EngineStarted, nil)
	if len(errs) == 0 {
		t.Error("should have context error")
	}
}

func TestManagerConcurrency(t *testing.T) {
	sm := NewManager(nil)

	var mu sync.Mutex
	callCount := 0

	// 并发注册
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.Connect(func(params map[string]any) error {
				mu.Lock()
				callCount++
				mu.Unlock()
				return nil
			}, EngineStarted)
		}()
	}
	wg.Wait()

	if sm.HandlerCount(EngineStarted) != 100 {
		t.Errorf("expected 100 handlers, got %d", sm.HandlerCount(EngineStarted))
	}

	// 并发发送
	sm.SendCatchLog(EngineStarted, nil)
	if callCount != 100 {
		t.Errorf("expected 100 calls, got %d", callCount)
	}
}

func TestSignalString(t *testing.T) {
	tests := []struct {
		sig      Signal
		expected string
	}{
		{EngineStarted, "engine_started"},
		{EngineStopped, "engine_stopped"},
		{SpiderOpened, "spider_opened"},
		{SpiderIdle, "spider_idle"},
		{SpiderClosed, "spider_closed"},
		{SpiderError, "spider_error"},
		{RequestScheduled, "request_scheduled"},
		{ResponseReceived, "response_received"},
		{ItemScraped, "item_scraped"},
		{ItemDropped, "item_dropped"},
		{ItemError, "item_error"},
		{Signal(999), "unknown_signal"},
	}

	for _, tt := range tests {
		if tt.sig.String() != tt.expected {
			t.Errorf("Signal(%d).String() = %s, expected %s", tt.sig, tt.sig.String(), tt.expected)
		}
	}
}

func TestContainsDontCloseSpider(t *testing.T) {
	// 空列表
	if ContainsDontCloseSpider(nil) {
		t.Error("should return false for nil")
	}

	// 不包含
	errs := []error{scrapy_errors.ErrDownloadFailed, scrapy_errors.ErrCloseSpider}
	if ContainsDontCloseSpider(errs) {
		t.Error("should return false")
	}

	// 包含
	errs2 := []error{scrapy_errors.ErrDownloadFailed, scrapy_errors.ErrDontCloseSpider}
	if !ContainsDontCloseSpider(errs2) {
		t.Error("should return true")
	}
}

func TestContainsCloseSpider(t *testing.T) {
	// 空列表
	if ContainsCloseSpider(nil) != nil {
		t.Error("should return nil for nil")
	}

	// 不包含
	errs := []error{scrapy_errors.ErrDownloadFailed}
	if ContainsCloseSpider(errs) != nil {
		t.Error("should return nil")
	}

	// 包含 CloseSpiderError
	errs2 := []error{scrapy_errors.NewCloseSpiderError("timeout")}
	closeErr := ContainsCloseSpider(errs2)
	if closeErr == nil {
		t.Fatal("should return CloseSpiderError")
	}
	if closeErr.Reason != "timeout" {
		t.Errorf("unexpected reason: %s", closeErr.Reason)
	}

	// 包含 sentinel ErrCloseSpider
	errs3 := []error{scrapy_errors.ErrCloseSpider}
	closeErr2 := ContainsCloseSpider(errs3)
	if closeErr2 == nil {
		t.Fatal("should return CloseSpiderError")
	}
	if closeErr2.Reason != "cancelled" {
		t.Errorf("unexpected reason: %s", closeErr2.Reason)
	}
}
