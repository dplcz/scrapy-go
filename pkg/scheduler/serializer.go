package scheduler

import (
	"encoding/json"
	"fmt"
	"log/slog"

	shttp "github.com/dplcz/scrapy-go/pkg/http"
)

// RequestSerializer 负责 Request 与 []byte 之间的序列化/反序列化。
//
// 序列化流程：Request → ToDict(callbackName, errbackName) → JSON → []byte
// 反序列化流程：[]byte → JSON → map[string]any → FromDict(registry) → Request
//
// 设计决策：
//   - 使用 JSON 格式替代 Scrapy 的 pickle，更安全且可读
//   - Callback/Errback 通过 CallbackRegistry 注册表模式恢复
//   - 不可序列化的 Meta 值会被自动跳过（由 ToDict 处理）
//
// 对应 Scrapy 的 squeues._scrapy_serialization_queue 中的序列化逻辑。
type RequestSerializer struct {
	registry *shttp.CallbackRegistry
	logger   *slog.Logger
}

// NewRequestSerializer 创建一个新的请求序列化器。
//
// 参数：
//   - registry: 回调函数注册表，用于序列化/反序列化 Callback/Errback。
//     为 nil 时，序列化不包含回调名称，反序列化不恢复回调。
//   - logger: 日志记录器，为 nil 时使用默认 logger。
func NewRequestSerializer(registry *shttp.CallbackRegistry, logger *slog.Logger) *RequestSerializer {
	if logger == nil {
		logger = slog.Default()
	}
	return &RequestSerializer{
		registry: registry,
		logger:   logger,
	}
}

// Serialize 将 Request 序列化为 JSON 字节切片。
//
// 序列化流程：
//  1. 通过 CallbackRegistry 查找 Callback/Errback 的注册名称
//  2. 调用 Request.ToDict() 生成可序列化的 map
//  3. 使用 encoding/json 编码为字节切片
//
// 如果 Request 的 Callback/Errback 未在注册表中注册，
// 则序列化时不包含回调名称（反序列化后回调为 nil）。
func (s *RequestSerializer) Serialize(req *shttp.Request) ([]byte, error) {
	callbackName := s.lookupCallbackName(req.Callback)
	errbackName := s.lookupErrbackName(req.Errback)

	d := req.ToDict(callbackName, errbackName)

	data, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request %s: %w", req.String(), err)
	}

	return data, nil
}

// Deserialize 将 JSON 字节切片反序列化为 Request。
//
// 反序列化流程：
//  1. 使用 encoding/json 解码字节切片为 map
//  2. 调用 FromDict() 恢复 Request 对象
//  3. 通过 CallbackRegistry 恢复 Callback/Errback 函数引用
func (s *RequestSerializer) Deserialize(data []byte) (*shttp.Request, error) {
	var d map[string]any
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("failed to deserialize request data: %w", err)
	}

	req, err := shttp.FromDict(d, s.registry)
	if err != nil {
		return nil, fmt.Errorf("failed to restore request from dict: %w", err)
	}

	return req, nil
}

// lookupCallbackName 通过注册表查找回调函数的注册名称。
// 如果回调为 nil 或未注册，返回空字符串。
func (s *RequestSerializer) lookupCallbackName(cb shttp.CallbackFunc) string {
	if cb == nil || s.registry == nil {
		return ""
	}

	// 遍历注册表查找匹配的回调
	for _, name := range s.registry.Names() {
		registered, ok := s.registry.Lookup(name)
		if ok && fmt.Sprintf("%v", registered) == fmt.Sprintf("%v", cb) {
			return name
		}
	}

	return ""
}

// lookupErrbackName 通过注册表查找错误回调函数的注册名称。
// 如果错误回调为 nil 或未注册，返回空字符串。
func (s *RequestSerializer) lookupErrbackName(eb shttp.ErrbackFunc) string {
	if eb == nil || s.registry == nil {
		return ""
	}

	// 遍历注册表查找匹配的错误回调
	for _, name := range s.registry.ErrbackNames() {
		registered, ok := s.registry.LookupErrback(name)
		if ok && fmt.Sprintf("%v", registered) == fmt.Sprintf("%v", eb) {
			return name
		}
	}

	return ""
}
