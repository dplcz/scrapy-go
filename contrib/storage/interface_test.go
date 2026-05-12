package storage

import (
	"testing"
)

func TestStorageWriter_InterfaceCompliance(t *testing.T) {
	// 验证接口定义的完整性
	var _ StorageWriter = (*MockWriter)(nil)
	var _ UpsertWriter = (*MockWriter)(nil)
}

func TestItemConverter_Type(t *testing.T) {
	// 验证 ItemConverter 函数类型
	var conv ItemConverter = func(item any) (map[string]any, error) {
		return map[string]any{"test": true}, nil
	}

	result, err := conv("anything")
	if err != nil {
		t.Fatalf("ItemConverter 失败: %v", err)
	}
	if result["test"] != true {
		t.Error("ItemConverter 结果不正确")
	}
}
