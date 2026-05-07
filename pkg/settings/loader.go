// Package settings 的 TOML 配置文件加载模块。
//
// 支持从 TOML 文件加载配置，以 PriorityAddon(15) 优先级注入到 Settings 中。
// 优先级层级：PriorityDefault(0) < PriorityAddon(15) < PriorityProject(20)
//
// 配置文件探测顺序：
//  1. SCRAPY_GO_CONFIG 环境变量指定的路径
//  2. 当前目录下的 scrapy-go.toml
//
// 支持的类型：
//   - 标量：int、bool、string、float、duration（如 "5s"、"1m30s"）
//   - 列表：[]int、[]string
//   - 简单 map：map[string]string（如 http.Header）
//
// 不支持从配置文件加载组件优先级字典（Go 静态编译限制）。
package settings

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultConfigFileName 是默认的配置文件名。
const DefaultConfigFileName = "scrapy-go.toml"

// ConfigEnvVar 是指定配置文件路径的环境变量名。
const ConfigEnvVar = "SCRAPY_GO_CONFIG"

// LoadFromFile 从指定的 TOML 文件加载配置到 Settings 中。
// 配置以 PriorityAddon(15) 优先级加载，低于代码中 PriorityProject(20)，
// 高于 PriorityDefault(0)。
//
// TOML 中的键名使用小写下划线格式（如 concurrent_requests），
// 加载时自动转换为大写格式（如 CONCURRENT_REQUESTS）。
//
// 返回加载的配置项数量和可能的错误。
func (s *Settings) LoadFromFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return 0, fmt.Errorf("解析 TOML 配置文件失败: %w", err)
	}

	count := 0
	for key, value := range raw {
		settingsKey := toSettingsKey(key)
		converted := convertTOMLValue(value)
		if err := s.Set(settingsKey, converted, PriorityAddon); err != nil {
			return count, fmt.Errorf("设置配置项 %s 失败: %w", settingsKey, err)
		}
		count++
	}

	return count, nil
}

// LoadFromFileIfExists 尝试从指定路径加载配置文件，如果文件不存在则静默跳过。
// 返回是否成功加载了配置文件。
func (s *Settings) LoadFromFileIfExists(path string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}

	_, err := s.LoadFromFile(path)
	if err != nil {
		return false, err
	}
	return true, nil
}

// AutoLoadConfig 自动探测并加载配置文件。
// 探测顺序：
//  1. SCRAPY_GO_CONFIG 环境变量指定的路径（必须存在，否则返回错误）
//  2. 当前目录下的 scrapy-go.toml（可选，不存在则跳过）
//
// 返回加载的配置文件路径（空字符串表示未加载）和可能的错误。
func (s *Settings) AutoLoadConfig() (string, error) {
	// 优先检查环境变量
	if envPath := os.Getenv(ConfigEnvVar); envPath != "" {
		if _, err := os.Stat(envPath); err != nil {
			return "", fmt.Errorf("环境变量 %s 指定的配置文件不存在: %s", ConfigEnvVar, envPath)
		}
		if _, err := s.LoadFromFile(envPath); err != nil {
			return "", err
		}
		return envPath, nil
	}

	// 尝试加载当前目录下的默认配置文件
	loaded, err := s.LoadFromFileIfExists(DefaultConfigFileName)
	if err != nil {
		return "", err
	}
	if loaded {
		return DefaultConfigFileName, nil
	}

	return "", nil
}

// toSettingsKey 将 TOML 键名（小写下划线）转换为 Settings 键名（大写下划线）。
// 例如：concurrent_requests → CONCURRENT_REQUESTS
func toSettingsKey(key string) string {
	return strings.ToUpper(key)
}

// convertTOMLValue 将 TOML 解析出的值转换为 Settings 支持的类型。
// TOML 库解析后的类型映射：
//   - TOML integer → int64
//   - TOML float → float64
//   - TOML boolean → bool
//   - TOML string → string（尝试解析为 duration）
//   - TOML array → []any（尝试转换为 []int 或 []string）
//   - TOML table → map[string]any
func convertTOMLValue(value any) any {
	switch v := value.(type) {
	case int64:
		// 转换为 int，与 Settings 内部使用的类型一致
		return int(v)
	case float64:
		return v
	case bool:
		return v
	case string:
		// 尝试解析为 duration
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		return v
	case []any:
		return convertTOMLSlice(v)
	case map[string]any:
		return convertTOMLMap(v)
	default:
		return value
	}
}

// convertTOMLSlice 将 TOML 数组转换为类型化的切片。
// 优先尝试转换为 []int，其次 []string，否则保持 []any。
func convertTOMLSlice(slice []any) any {
	if len(slice) == 0 {
		return slice
	}

	// 尝试转换为 []int
	allInt := true
	ints := make([]int, 0, len(slice))
	for _, item := range slice {
		switch v := item.(type) {
		case int64:
			ints = append(ints, int(v))
		case float64:
			// 检查是否为整数值
			if v == float64(int(v)) {
				ints = append(ints, int(v))
			} else {
				allInt = false
			}
		default:
			allInt = false
		}
		if !allInt {
			break
		}
	}
	if allInt {
		return ints
	}

	// 尝试转换为 []string
	allString := true
	strs := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok {
			strs = append(strs, s)
		} else {
			allString = false
			break
		}
	}
	if allString {
		return strs
	}

	// 保持原始类型
	return slice
}

// convertTOMLMap 将 TOML table 转换为适当的 map 类型。
// 如果所有值都是字符串，转换为 map[string]string（适用于 HTTP Header 等）。
// 否则递归转换值并保持 map[string]any。
func convertTOMLMap(m map[string]any) any {
	if len(m) == 0 {
		return m
	}

	// 尝试转换为 map[string]string
	allString := true
	strMap := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			strMap[k] = s
		} else {
			allString = false
			break
		}
	}
	if allString {
		return strMap
	}

	// 递归转换值
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = convertTOMLValue(v)
	}
	return result
}
