package errors

import (
	"errors"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	// 测试所有 sentinel errors 不为 nil
	sentinels := []error{
		ErrNotConfigured,
		ErrIgnoreRequest,
		ErrDropItem,
		ErrCloseSpider,
		ErrDontCloseSpider,
		ErrDownloadTimeout,
		ErrStopDownload,
		ErrInvalidOutput,
		ErrNotSupported,
		ErrConnectionRefused,
		ErrCannotResolveHost,
		ErrDownloadFailed,
		ErrResponseDataLoss,
	}

	for _, err := range sentinels {
		if err == nil {
			t.Error("sentinel error should not be nil")
		}
		if err.Error() == "" {
			t.Error("sentinel error message should not be empty")
		}
	}
}

func TestCloseSpiderError(t *testing.T) {
	err := NewCloseSpiderError("item_count_exceeded")

	// 测试 Error() 方法
	if err.Error() != "close spider: item_count_exceeded" {
		t.Errorf("unexpected error message: %s", err.Error())
	}

	// 测试 errors.Is 匹配
	if !errors.Is(err, ErrCloseSpider) {
		t.Error("CloseSpiderError should match ErrCloseSpider")
	}

	// 测试不匹配其他错误
	if errors.Is(err, ErrDropItem) {
		t.Error("CloseSpiderError should not match ErrDropItem")
	}

	// 测试 Reason 字段
	if err.Reason != "item_count_exceeded" {
		t.Errorf("unexpected reason: %s", err.Reason)
	}
}

func TestDropItemError(t *testing.T) {
	err := NewDropItemError("duplicate item")

	// 测试 Error() 方法
	if err.Error() != "drop item: duplicate item" {
		t.Errorf("unexpected error message: %s", err.Error())
	}

	// 测试 errors.Is 匹配
	if !errors.Is(err, ErrDropItem) {
		t.Error("DropItemError should match ErrDropItem")
	}

	// 测试不匹配其他错误
	if errors.Is(err, ErrCloseSpider) {
		t.Error("DropItemError should not match ErrCloseSpider")
	}
}

func TestStopDownloadError(t *testing.T) {
	// 测试 fail=true
	errFail := NewStopDownloadError(true)
	if errFail.Error() != "stop download (fail)" {
		t.Errorf("unexpected error message: %s", errFail.Error())
	}
	if !errFail.Fail {
		t.Error("Fail should be true")
	}
	if !errors.Is(errFail, ErrStopDownload) {
		t.Error("StopDownloadError should match ErrStopDownload")
	}

	// 测试 fail=false
	errNoFail := NewStopDownloadError(false)
	if errNoFail.Error() != "stop download (no fail)" {
		t.Errorf("unexpected error message: %s", errNoFail.Error())
	}
	if errNoFail.Fail {
		t.Error("Fail should be false")
	}
}

func TestNotConfiguredError(t *testing.T) {
	// 带消息
	err := NewNotConfiguredError("missing API key")
	if err.Error() != "not configured: missing API key" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
	if !errors.Is(err, ErrNotConfigured) {
		t.Error("NotConfiguredError should match ErrNotConfigured")
	}

	// 空消息
	errEmpty := NewNotConfiguredError("")
	if errEmpty.Error() != "component not configured" {
		t.Errorf("unexpected error message: %s", errEmpty.Error())
	}
}

func TestIsRetryable(t *testing.T) {
	retryableErrors := []error{
		ErrDownloadTimeout,
		ErrConnectionRefused,
		ErrDownloadFailed,
		ErrResponseDataLoss,
		ErrCannotResolveHost,
	}

	for _, err := range retryableErrors {
		if !IsRetryable(err) {
			t.Errorf("error should be retryable: %v", err)
		}
	}

	nonRetryableErrors := []error{
		ErrNotConfigured,
		ErrIgnoreRequest,
		ErrDropItem,
		ErrCloseSpider,
		ErrDontCloseSpider,
	}

	for _, err := range nonRetryableErrors {
		if IsRetryable(err) {
			t.Errorf("error should not be retryable: %v", err)
		}
	}
}

func TestErrorWrapping(t *testing.T) {
	// 测试 errors.As 可以提取具体错误类型
	var closeErr *CloseSpiderError
	err := NewCloseSpiderError("timeout")
	if !errors.As(err, &closeErr) {
		t.Error("should be able to extract CloseSpiderError")
	}
	if closeErr.Reason != "timeout" {
		t.Errorf("unexpected reason: %s", closeErr.Reason)
	}

	var dropErr *DropItemError
	err2 := NewDropItemError("invalid")
	if !errors.As(err2, &dropErr) {
		t.Error("should be able to extract DropItemError")
	}
	if dropErr.Message != "invalid" {
		t.Errorf("unexpected message: %s", dropErr.Message)
	}
}
