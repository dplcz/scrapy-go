// Package http 定义了 scrapy-go 框架的 HTTP 请求和响应模型。
package http

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

// CallbackRegistry 是回调函数注册表，用于 Request 序列化/反序列化时
// 通过方法名字符串恢复 Callback/Errback 函数引用。
//
// 在 Go 中，函数不可序列化，因此磁盘队列等场景需要将 Callback/Errback
// 转换为字符串名称进行持久化，恢复时通过注册表查找对应的函数。
//
// 这是 Go 的注册表模式（Registry Pattern），替代 Scrapy 中通过
// getattr(spider, method_name) 反射查找方法的方式。
//
// 推荐使用 RegisterSpider 自动注册，无需手动逐个注册：
//
//	registry := http.NewCallbackRegistry()
//	registry.RegisterSpider(spider) // 自动扫描并注册所有符合签名的方法
//
// 也支持手动注册：
//
//	registry.Register("ParseDetail", spider.ParseDetail)
//	registry.Register("ParseList", spider.ParseList)
//
// 方法命名规范：
//   - 回调方法必须是导出方法（首字母大写），遵循 Go PascalCase 命名规范
//   - 注册表中的名称即为 Go 方法名（如 "ParseDetail"、"ParseList"）
//   - Callback 签名：func(ctx context.Context, resp *http.Response) ([]Output, error)
//   - Errback 签名：func(ctx context.Context, err error, req *http.Request) ([]Output, error)
//
// 序列化/反序列化示例：
//
//	// 序列化时
//	d := req.ToDict("ParseDetail", "HandleError")
//
//	// 反序列化时
//	req, err := http.FromDict(d, registry)
type CallbackRegistry struct {
	mu        sync.RWMutex
	callbacks map[string]CallbackFunc
	errbacks  map[string]ErrbackFunc
}

// NewCallbackRegistry 创建一个新的回调函数注册表。
func NewCallbackRegistry() *CallbackRegistry {
	return &CallbackRegistry{
		callbacks: make(map[string]CallbackFunc),
		errbacks:  make(map[string]ErrbackFunc),
	}
}

// RegisterSpider 通过 reflect 自动扫描 spider 实例上所有符合
// Callback/Errback 签名的导出方法，并注册到注册表中。
//
// 这是推荐的注册方式，用户无需手动逐个注册回调函数。
// 方法名直接作为注册表中的键（Go PascalCase 导出名）。
//
// Callback 签名匹配规则（绑定方法，不含 receiver）：
//   - 入参：(context.Context, *Response)
//   - 返回：(slice, error)
//
// Errback 签名匹配规则（绑定方法，不含 receiver）：
//   - 入参：(context.Context, error, *Request)
//   - 返回：(slice, error)
//
// 用法：
//
//	type MySpider struct { spider.Base }
//	func (s *MySpider) ParseDetail(ctx context.Context, resp *http.Response) ([]spider.Output, error) { ... }
//	func (s *MySpider) HandleError(ctx context.Context, err error, req *http.Request) ([]spider.Output, error) { ... }
//
//	registry := http.NewCallbackRegistry()
//	registry.RegisterSpider(&MySpider{})
//	// 自动注册: "ParseDetail" → callback, "HandleError" → errback
func (r *CallbackRegistry) RegisterSpider(spider any) {
	if spider == nil {
		return
	}

	v := reflect.ValueOf(spider)
	t := v.Type()

	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)

		// 只处理导出方法
		if !method.IsExported() {
			continue
		}

		// 获取绑定方法的类型（不含 receiver）
		methodType := method.Type

		// 检查是否匹配 Callback 签名：
		// 绑定方法含 receiver，所以实际签名是 (receiver, context.Context, *Response) ([]T, error)
		if matchesCallbackSignature(methodType) {
			// 获取绑定后的方法值（已绑定 receiver）
			boundMethod := v.Method(i)
			r.Register(method.Name, boundMethod.Interface())
			continue
		}

		// 检查是否匹配 Errback 签名：
		// 绑定方法含 receiver，所以实际签名是 (receiver, context.Context, error, *Request) ([]T, error)
		if matchesErrbackSignature(methodType) {
			boundMethod := v.Method(i)
			r.RegisterErrback(method.Name, boundMethod.Interface())
		}
	}
}

// matchesCallbackSignature 检查方法类型是否匹配 Callback 签名。
// 方法类型包含 receiver，所以入参数量为 3：(receiver, context.Context, *Response)
// 返回值为 2：(slice, error)
func matchesCallbackSignature(mt reflect.Type) bool {
	// 入参：receiver + context.Context + *Response = 3
	if mt.NumIn() != 3 {
		return false
	}
	// 返回值：slice + error = 2
	if mt.NumOut() != 2 {
		return false
	}

	// 第 1 个入参（index 1，跳过 receiver）：context.Context
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if !mt.In(1).Implements(contextType) {
		return false
	}

	// 第 2 个入参（index 2）：*Response
	responseType := reflect.TypeOf((*Response)(nil))
	if mt.In(2) != responseType {
		return false
	}

	// 第 1 个返回值：slice 类型
	if mt.Out(0).Kind() != reflect.Slice {
		return false
	}

	// 第 2 个返回值：error 接口
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !mt.Out(1).Implements(errorType) {
		return false
	}

	return true
}

// matchesErrbackSignature 检查方法类型是否匹配 Errback 签名。
// 方法类型包含 receiver，所以入参数量为 4：(receiver, context.Context, error, *Request)
// 返回值为 2：(slice, error)
func matchesErrbackSignature(mt reflect.Type) bool {
	// 入参：receiver + context.Context + error + *Request = 4
	if mt.NumIn() != 4 {
		return false
	}
	// 返回值：slice + error = 2
	if mt.NumOut() != 2 {
		return false
	}

	// 第 1 个入参（index 1，跳过 receiver）：context.Context
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if !mt.In(1).Implements(contextType) {
		return false
	}

	// 第 2 个入参（index 2）：error 接口
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !mt.In(2).Implements(errorType) {
		return false
	}

	// 第 3 个入参（index 3）：*Request
	requestType := reflect.TypeOf((*Request)(nil))
	if mt.In(3) != requestType {
		return false
	}

	// 第 1 个返回值：slice 类型
	if mt.Out(0).Kind() != reflect.Slice {
		return false
	}

	// 第 2 个返回值：error 接口
	if !mt.Out(1).Implements(errorType) {
		return false
	}

	return true
}

// Register 注册一个回调函数。
// name 是回调的唯一标识符（通常是 Spider 方法名），cb 是回调函数。
func (r *CallbackRegistry) Register(name string, cb CallbackFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbacks[name] = cb
}

// RegisterErrback 注册一个错误回调函数。
// name 是错误回调的唯一标识符，eb 是错误回调函数。
func (r *CallbackRegistry) RegisterErrback(name string, eb ErrbackFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errbacks[name] = eb
}

// Lookup 通过名称查找已注册的回调函数。
// 返回回调函数和是否找到的标志。
func (r *CallbackRegistry) Lookup(name string) (CallbackFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cb, ok := r.callbacks[name]
	return cb, ok
}

// LookupErrback 通过名称查找已注册的错误回调函数。
// 返回错误回调函数和是否找到的标志。
func (r *CallbackRegistry) LookupErrback(name string) (ErrbackFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	eb, ok := r.errbacks[name]
	return eb, ok
}

// MustLookup 通过名称查找已注册的回调函数，未找到时 panic。
func (r *CallbackRegistry) MustLookup(name string) CallbackFunc {
	cb, ok := r.Lookup(name)
	if !ok {
		panic(fmt.Sprintf("callback %q not registered", name))
	}
	return cb
}

// MustLookupErrback 通过名称查找已注册的错误回调函数，未找到时 panic。
func (r *CallbackRegistry) MustLookupErrback(name string) ErrbackFunc {
	eb, ok := r.LookupErrback(name)
	if !ok {
		panic(fmt.Sprintf("errback %q not registered", name))
	}
	return eb
}

// Names 返回所有已注册的回调函数名称。
func (r *CallbackRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.callbacks))
	for name := range r.callbacks {
		names = append(names, name)
	}
	return names
}

// ErrbackNames 返回所有已注册的错误回调函数名称。
func (r *CallbackRegistry) ErrbackNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.errbacks))
	for name := range r.errbacks {
		names = append(names, name)
	}
	return names
}

// Len 返回已注册的回调函数数量。
func (r *CallbackRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.callbacks)
}

// ErrbackLen 返回已注册的错误回调函数数量。
func (r *CallbackRegistry) ErrbackLen() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.errbacks)
}