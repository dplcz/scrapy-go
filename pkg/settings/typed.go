package settings

import (
	"fmt"
	"time"
)

// ============================================================================
// 泛型类型化配置键（TD-004：编译期类型安全增强）
// ============================================================================

// Key 是类型化的配置键，将配置项名称与其值类型在编译期绑定。
// 通过泛型参数 T 约束配置值的类型，消除运行时类型断言的风险。
//
// 使用示例：
//
//	val := settings.Get(s, settings.KeyConcurrentRequests) // 返回 int，编译期确定
type Key[T any] struct {
	// Name 是配置项的字符串键名（如 "CONCURRENT_REQUESTS"）。
	Name string
	// Default 是该配置项的默认值，当配置中不存在时返回此值。
	Default T
}

// String 返回配置键的字符串表示。
func (k Key[T]) String() string {
	return k.Name
}

// ============================================================================
// 泛型获取函数
// ============================================================================

// Get 以类型安全的方式获取配置值。
// 通过 Key[T] 的泛型参数在编译期确定返回类型，无需调用者手动指定默认值。
//
// 如果配置项不存在，返回 Key 中定义的默认值。
// 如果存储的值类型与 T 不匹配，尝试进行合理的类型转换（与 GetInt/GetString 等行为一致）。
func Get[T any](s *Settings, key Key[T]) T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	attr, ok := s.attributes[key.Name]
	if !ok {
		return key.Default
	}

	// 尝试直接类型断言
	if val, ok := attr.value.(T); ok {
		return val
	}

	// 对于基础类型，委托给已有的类型转换逻辑
	return convertValue[T](attr.value, key.Default)
}

// MustGet 以类型安全的方式获取配置值，如果配置项不存在则 panic。
// 适用于必须存在的配置项（如框架初始化阶段）。
func MustGet[T any](s *Settings, key Key[T]) T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	attr, ok := s.attributes[key.Name]
	if !ok {
		panic(fmt.Sprintf("settings: required key %q not found", key.Name))
	}

	if val, ok := attr.value.(T); ok {
		return val
	}

	return convertValue[T](attr.value, key.Default)
}

// Set 以类型安全的方式设置配置值。
// 通过 Key[T] 的泛型参数在编译期约束值的类型。
func Set[T any](s *Settings, key Key[T], value T, priority Priority) error {
	return s.Set(key.Name, value, priority)
}

// ============================================================================
// 内部类型转换
// ============================================================================

// convertValue 尝试将 any 类型的值转换为目标类型 T。
// 复用了 GetInt/GetString/GetBool 等方法中的转换逻辑。
func convertValue[T any](value any, defaultVal T) T {
	// 通过 interface{} 中间变量判断目标类型
	var zero T
	switch any(zero).(type) {
	case int:
		if result, ok := convertToInt(value); ok {
			if v, ok := any(result).(T); ok {
				return v
			}
		}
	case int64:
		if result, ok := convertToInt64(value); ok {
			if v, ok := any(result).(T); ok {
				return v
			}
		}
	case float64:
		if result, ok := convertToFloat64(value); ok {
			if v, ok := any(result).(T); ok {
				return v
			}
		}
	case string:
		result := convertToString(value)
		if v, ok := any(result).(T); ok {
			return v
		}
	case bool:
		if result, ok := convertToBool(value); ok {
			if v, ok := any(result).(T); ok {
				return v
			}
		}
	case time.Duration:
		if result, ok := convertToDuration(value); ok {
			if v, ok := any(result).(T); ok {
				return v
			}
		}
	case []string:
		result := convertToStringSlice(value)
		if result != nil {
			if v, ok := any(result).(T); ok {
				return v
			}
		}
	case []int:
		result := convertToIntSlice(value)
		if result != nil {
			if v, ok := any(result).(T); ok {
				return v
			}
		}
	case map[string]int:
		result := convertToIntMap(value)
		if result != nil {
			if v, ok := any(result).(T); ok {
				return v
			}
		}
	case map[string]any:
		result := convertToStringAnyMap(value)
		if result != nil {
			if v, ok := any(result).(T); ok {
				return v
			}
		}
	}

	return defaultVal
}

// convertToInt 将 any 值转换为 int，返回转换是否成功。
func convertToInt(value any) (int, bool) {
	switch val := value.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case float32:
		return int(val), true
	case string:
		if i, err := parseInt(val); err == nil {
			return i, true
		}
	}
	return 0, false
}

// convertToInt64 将 any 值转换为 int64，返回转换是否成功。
func convertToInt64(value any) (int64, bool) {
	switch val := value.(type) {
	case int:
		return int64(val), true
	case int64:
		return val, true
	case float64:
		return int64(val), true
	case string:
		if i, err := parseInt64(val); err == nil {
			return i, true
		}
	}
	return 0, false
}

// convertToFloat64 将 any 值转换为 float64，返回转换是否成功。
func convertToFloat64(value any) (float64, bool) {
	switch val := value.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		if f, err := parseFloat64(val); err == nil {
			return f, true
		}
	}
	return 0, false
}

// convertToString 将 any 值转换为 string。
func convertToString(value any) string {
	switch val := value.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

// convertToBool 将 any 值转换为 bool，返回转换是否成功。
func convertToBool(value any) (bool, bool) {
	switch val := value.(type) {
	case bool:
		return val, true
	case int:
		return val != 0, true
	case int64:
		return val != 0, true
	case float64:
		return val != 0, true
	case string:
		switch val {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no", "":
			return false, true
		}
	}
	return false, false
}

// convertToDuration 将 any 值转换为 time.Duration，返回转换是否成功。
func convertToDuration(value any) (time.Duration, bool) {
	switch val := value.(type) {
	case time.Duration:
		return val, true
	case int:
		return time.Duration(val) * time.Second, true
	case int64:
		return time.Duration(val) * time.Second, true
	case float64:
		return time.Duration(val * float64(time.Second)), true
	case string:
		if d, err := time.ParseDuration(val); err == nil {
			return d, true
		}
		if f, err := parseFloat64(val); err == nil {
			return time.Duration(f * float64(time.Second)), true
		}
	}
	return 0, false
}

// convertToStringSlice 将 any 值转换为 []string。
func convertToStringSlice(value any) []string {
	switch val := value.(type) {
	case []string:
		result := make([]string, len(val))
		copy(result, val)
		return result
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result
	}
	return nil
}

// convertToIntSlice 将 any 值转换为 []int。
func convertToIntSlice(value any) []int {
	switch val := value.(type) {
	case []int:
		result := make([]int, len(val))
		copy(result, val)
		return result
	case []any:
		result := make([]int, 0, len(val))
		for _, item := range val {
			switch v := item.(type) {
			case int:
				result = append(result, v)
			case int64:
				result = append(result, int(v))
			case float64:
				result = append(result, int(v))
			default:
				return nil
			}
		}
		return result
	}
	return nil
}

// convertToIntMap 将 any 值转换为 map[string]int。
func convertToIntMap(value any) map[string]int {
	switch val := value.(type) {
	case map[string]int:
		result := make(map[string]int, len(val))
		for k, v := range val {
			result[k] = v
		}
		return result
	case map[string]any:
		result := make(map[string]int, len(val))
		for k, v := range val {
			switch num := v.(type) {
			case int:
				result[k] = num
			case int64:
				result[k] = int(num)
			case float64:
				result[k] = int(num)
			}
		}
		return result
	}
	return nil
}

// convertToStringAnyMap 将 any 值转换为 map[string]any。
func convertToStringAnyMap(value any) map[string]any {
	switch val := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = v
		}
		return result
	}
	return nil
}

// ============================================================================
// 内部解析辅助函数
// ============================================================================

func parseInt(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

func parseInt64(s string) (int64, error) {
	var i int64
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

func parseFloat64(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%g", &f)
	return f, err
}
