package http

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
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

	// 反向索引：函数 reflect.Pointer → 注册名称
	// 用于手动注册的匿名闭包场景（extractFuncName 无法提取方法名时的 fallback）。
	// 对于通过 RegisterSpider 自动注册的方法，优先使用 runtime.FuncForPC 提取方法名。
	callbackPtrs map[uintptr]string
	errbackPtrs  map[uintptr]string
}

// NewCallbackRegistry 创建一个新的回调函数注册表。
func NewCallbackRegistry() *CallbackRegistry {
	return &CallbackRegistry{
		callbacks:    make(map[string]CallbackFunc),
		errbacks:     make(map[string]ErrbackFunc),
		callbackPtrs: make(map[uintptr]string),
		errbackPtrs:  make(map[uintptr]string),
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
			// 使用 reflect.Value.Convert 将匿名函数类型转换为命名类型 CallbackFunc。
			// reflect.Value.Interface() 返回的是匿名函数类型 func(context.Context, *Response) ([]Output, error)，
			// 与命名类型 CallbackFunc 底层相同但 Go 类型系统视为不同类型，直接类型断言会失败。
			// Convert 在底层类型相同时零开销转换，调用时不经过反射。
			cbType := reflect.TypeOf(CallbackFunc(nil))
			cb := boundMethod.Convert(cbType).Interface().(CallbackFunc)
			r.Register(method.Name, cb)
			continue
		}

		// 检查是否匹配 Errback 签名：
		// 绑定方法含 receiver，所以实际签名是 (receiver, context.Context, error, *Request) ([]T, error)
		if matchesErrbackSignature(methodType) {
			boundMethod := v.Method(i)
			ebType := reflect.TypeOf(ErrbackFunc(nil))
			eb := boundMethod.Convert(ebType).Interface().(ErrbackFunc)
			r.RegisterErrback(method.Name, eb)
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

	// 第 1 个返回值：[]Output 类型
	outputSliceType := reflect.TypeOf([]Output{})
	if mt.Out(0) != outputSliceType {
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

	// 第 1 个返回值：[]Output 类型
	outputSliceType := reflect.TypeOf([]Output{})
	if mt.Out(0) != outputSliceType {
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
// 同时建立 reflect.Pointer → 名称的反向索引，用于匿名闭包场景的 fallback 查找。
func (r *CallbackRegistry) Register(name string, cb CallbackFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbacks[name] = cb
	r.callbackPtrs[reflect.ValueOf(cb).Pointer()] = name
}

// RegisterErrback 注册一个错误回调函数。
// name 是错误回调的唯一标识符，eb 是错误回调函数。
// 同时建立 reflect.Pointer → 名称的反向索引，用于匿名闭包场景的 fallback 查找。
func (r *CallbackRegistry) RegisterErrback(name string, eb ErrbackFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errbacks[name] = eb
	r.errbackPtrs[reflect.ValueOf(eb).Pointer()] = name
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

// LookupByFunc 通过回调函数值反向查找其注册名称。
//
// 查找策略（按优先级）：
//  1. 通过 runtime.FuncForPC 从函数值提取方法名，在注册表中验证（适用于 method value）
//  2. 通过 reflect.ValueOf().Pointer() 在反向索引中查找（适用于手动注册的匿名闭包）
//
// 返回注册名称和是否找到的标志。
func (r *CallbackRegistry) LookupByFunc(cb CallbackFunc) (string, bool) {
	if cb == nil {
		return "", false
	}

	// 策略 1：通过 runtime.FuncForPC 提取方法名
	name := extractFuncName(cb)
	if name != "" {
		r.mu.RLock()
		defer r.mu.RUnlock()
		if _, ok := r.callbacks[name]; ok {
			return name, true
		}
		return "", false
	}

	// 策略 2：通过 reflect.Pointer 反向索引查找（fallback，适用于匿名闭包）
	ptr := reflect.ValueOf(cb).Pointer()
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n, ok := r.callbackPtrs[ptr]; ok {
		return n, true
	}
	return "", false
}

// LookupErrbackByFunc 通过错误回调函数值反向查找其注册名称。
//
// 查找策略同 LookupByFunc：优先 runtime.FuncForPC，fallback 到 reflect.Pointer。
//
// 返回注册名称和是否找到的标志。
func (r *CallbackRegistry) LookupErrbackByFunc(eb ErrbackFunc) (string, bool) {
	if eb == nil {
		return "", false
	}

	// 策略 1：通过 runtime.FuncForPC 提取方法名
	name := extractFuncName(eb)
	if name != "" {
		r.mu.RLock()
		defer r.mu.RUnlock()
		if _, ok := r.errbacks[name]; ok {
			return name, true
		}
		return "", false
	}

	// 策略 2：通过 reflect.Pointer 反向索引查找（fallback，适用于匿名闭包）
	ptr := reflect.ValueOf(eb).Pointer()
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n, ok := r.errbackPtrs[ptr]; ok {
		return n, true
	}
	return "", false
}

// extractFuncName 从任意函数值中提取方法名。
//
// 通过 reflect.ValueOf(fn).Pointer() 获取函数代码入口地址，
// 再通过 runtime.FuncForPC 获取全限定函数名，最后解析出方法名。
//
// 返回规则：
//   - method value（如 s.ParseDetail）→ 返回 "ParseDetail"
//   - 匿名闭包 → 返回 ""（包含 "func" 前缀的名称被过滤）
//   - 反射生成的函数值 → 返回 ""（包含 "reflect." 的被过滤）
func extractFuncName(fn any) string {
	pc := reflect.ValueOf(fn).Pointer()
	f := runtime.FuncForPC(pc)
	if f == nil {
		return ""
	}
	fullName := f.Name()

	// 排除反射生成的方法值
	if strings.Contains(fullName, "reflect.") {
		return ""
	}

	// 去掉 -fm 后缀（method value wrapper 的标记）
	name := strings.TrimSuffix(fullName, "-fm")

	// 取最后一个 "." 后面的部分作为方法名
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}

	// 过滤匿名闭包（名称形如 "func1"、"func2" 等）
	if strings.HasPrefix(name, "func") {
		return ""
	}

	return name
}
