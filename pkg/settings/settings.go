// Package settings 实现了 scrapy-go 框架的多优先级配置系统。
//
// 配置系统支持六级优先级：default → command → addon → project → spider → cmdline，
// 高优先级的配置会覆盖低优先级的同名配置。配置可以被冻结（freeze）以防止修改。
//
// 对应 Scrapy Python 版本中 scrapy.settings 模块的功能。
package settings

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// 优先级定义
// ============================================================================

// Priority 表示配置的优先级。
type Priority int

const (
	PriorityDefault Priority = 0  // 框架默认值
	PriorityCommand Priority = 10 // 命令行命令级别
	PriorityAddon   Priority = 15 // 插件级别
	PriorityProject Priority = 20 // 项目级别
	PrioritySpider  Priority = 30 // Spider 级别
	PriorityCmdline Priority = 40 // 命令行参数级别（最高）
)

// PriorityNames 将优先级名称映射到数值。
var PriorityNames = map[string]Priority{
	"default": PriorityDefault,
	"command": PriorityCommand,
	"addon":   PriorityAddon,
	"project": PriorityProject,
	"spider":  PrioritySpider,
	"cmdline": PriorityCmdline,
}

// ============================================================================
// settingsAttribute 内部类型
// ============================================================================

// settingsAttribute 存储一个配置项的值和优先级。
type settingsAttribute struct {
	value    any
	priority Priority
}

// set 在优先级大于等于当前优先级时更新值。
func (sa *settingsAttribute) set(value any, priority Priority) {
	if priority >= sa.priority {
		sa.value = value
		sa.priority = priority
	}
}

// ============================================================================
// Settings 主结构体
// ============================================================================

// Settings 是配置管理器，支持多优先级配置和冻结机制。
// 线程安全，所有操作通过 RWMutex 保护。
type Settings struct {
	mu         sync.RWMutex
	frozen     bool
	attributes map[string]*settingsAttribute
}

// New 创建一个新的 Settings 实例，并加载默认配置。
func New() *Settings {
	s := &Settings{
		attributes: make(map[string]*settingsAttribute),
	}
	// 加载默认配置
	s.loadDefaults()
	return s
}

// NewEmpty 创建一个空的 Settings 实例（不加载默认配置）。
// 主要用于测试。
func NewEmpty() *Settings {
	return &Settings{
		attributes: make(map[string]*settingsAttribute),
	}
}

// ============================================================================
// 设置方法
// ============================================================================

// Set 以指定优先级设置一个配置项。
// 如果该配置项已存在且当前优先级更高，则不会被覆盖。
func (s *Settings) Set(key string, value any, priority Priority) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.frozen {
		return fmt.Errorf("cannot modify frozen settings: key=%s", key)
	}

	if attr, ok := s.attributes[key]; ok {
		attr.set(value, priority)
	} else {
		s.attributes[key] = &settingsAttribute{value: value, priority: priority}
	}
	return nil
}

// SetDefault 设置一个默认优先级的配置项。
// 等价于 Set(key, value, PriorityDefault)。
func (s *Settings) SetDefault(key string, value any) error {
	return s.Set(key, value, PriorityDefault)
}

// SetIfNotExists 仅在配置项不存在时设置。
func (s *Settings) SetIfNotExists(key string, value any, priority Priority) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.frozen {
		return fmt.Errorf("cannot modify frozen settings: key=%s", key)
	}

	if _, ok := s.attributes[key]; !ok {
		s.attributes[key] = &settingsAttribute{value: value, priority: priority}
	}
	return nil
}

// Update 批量更新配置项。
func (s *Settings) Update(values map[string]any, priority Priority) error {
	for k, v := range values {
		if err := s.Set(k, v, priority); err != nil {
			return err
		}
	}
	return nil
}

// Delete 删除一个配置项（仅当指定优先级 >= 当前优先级时）。
func (s *Settings) Delete(key string, priority Priority) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.frozen {
		return fmt.Errorf("cannot modify frozen settings: key=%s", key)
	}

	if attr, ok := s.attributes[key]; ok {
		if priority >= attr.priority {
			delete(s.attributes, key)
		}
	}
	return nil
}

// ============================================================================
// 获取方法
// ============================================================================

// Get 获取配置值，如果不存在返回 defaultVal。
func (s *Settings) Get(key string, defaultVal any) any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if attr, ok := s.attributes[key]; ok {
		return attr.value
	}
	return defaultVal
}

// GetString 获取字符串类型的配置值。
func (s *Settings) GetString(key string, defaultVal string) string {
	v := s.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

// GetInt 获取整数类型的配置值。
func (s *Settings) GetInt(key string, defaultVal int) int {
	v := s.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
		return defaultVal
	default:
		return defaultVal
	}
}

// GetInt64 获取 int64 类型的配置值。
func (s *Settings) GetInt64(key string, defaultVal int64) int64 {
	v := s.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case int:
		return int64(val)
	case int64:
		return val
	case float64:
		return int64(val)
	case string:
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
		return defaultVal
	default:
		return defaultVal
	}
}

// GetFloat 获取浮点数类型的配置值。
func (s *Settings) GetFloat(key string, defaultVal float64) float64 {
	v := s.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
		return defaultVal
	default:
		return defaultVal
	}
}

// GetBool 获取布尔类型的配置值。
// 支持 true/false、1/0、"true"/"false"、"1"/"0"。
func (s *Settings) GetBool(key string, defaultVal bool) bool {
	v := s.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case string:
		lower := strings.ToLower(val)
		switch lower {
		case "true", "1", "yes":
			return true
		case "false", "0", "no", "":
			return false
		default:
			return defaultVal
		}
	default:
		return defaultVal
	}
}

// GetDuration 获取时间间隔类型的配置值。
// 支持 time.Duration、int（秒）、float64（秒）、string（如 "5s"、"1m30s"）。
func (s *Settings) GetDuration(key string, defaultVal time.Duration) time.Duration {
	v := s.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case time.Duration:
		return val
	case int:
		return time.Duration(val) * time.Second
	case int64:
		return time.Duration(val) * time.Second
	case float64:
		return time.Duration(val * float64(time.Second))
	case string:
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
		// 尝试解析为秒数
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return time.Duration(f * float64(time.Second))
		}
		return defaultVal
	default:
		return defaultVal
	}
}

// GetStringSlice 获取字符串切片类型的配置值。
// 如果值是字符串，按逗号分割。
func (s *Settings) GetStringSlice(key string, defaultVal []string) []string {
	v := s.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
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
	case string:
		if val == "" {
			return []string{}
		}
		parts := strings.Split(val, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p)
			}
		}
		return result
	default:
		return defaultVal
	}
}

// GetStringMap 获取 map[string]any 类型的配置值。
// 如果值是 JSON 字符串，会自动解析。
func (s *Settings) GetStringMap(key string, defaultVal map[string]any) map[string]any {
	v := s.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = v
		}
		return result
	case string:
		var result map[string]any
		if err := json.Unmarshal([]byte(val), &result); err == nil {
			return result
		}
		return defaultVal
	default:
		return defaultVal
	}
}

// GetIntMap 获取 map[string]int 类型的配置值。
// 主要用于组件优先级字典（如 DOWNLOADER_MIDDLEWARES）。
func (s *Settings) GetIntMap(key string, defaultVal map[string]int) map[string]int {
	v := s.Get(key, nil)
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
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
			case float64:
				result[k] = int(num)
			case int64:
				result[k] = int(num)
			}
		}
		return result
	default:
		return defaultVal
	}
}

// ============================================================================
// 组件优先级字典
// ============================================================================

// GetComponentPriorityDictWithBase 获取组件优先级字典，合并 _BASE 后缀的基础配置。
// 例如：GetComponentPriorityDictWithBase("DOWNLOADER_MIDDLEWARES") 会合并
// DOWNLOADER_MIDDLEWARES_BASE 和 DOWNLOADER_MIDDLEWARES 两个配置。
//
// 禁用规则：用户配置中优先级值 < 0 的条目表示禁用该组件（对应 Scrapy 中设置为 None 的行为）。
// 例如：DOWNLOADER_MIDDLEWARES = {"Retry": -1} 表示禁用 Retry 中间件。
func (s *Settings) GetComponentPriorityDictWithBase(name string) map[string]int {
	base := s.GetIntMap(name+"_BASE", nil)
	override := s.GetIntMap(name, nil)

	if base == nil && override == nil {
		return make(map[string]int)
	}

	merged := make(map[string]int)

	// 先加载 BASE
	if base != nil {
		for k, v := range base {
			merged[k] = v
		}
	}

	// 再用用户配置覆盖
	if override != nil {
		for k, v := range override {
			merged[k] = v
		}
	}

	// 过滤掉优先级 < 0 的条目（表示禁用）
	result := make(map[string]int)
	for k, v := range merged {
		if v >= 0 {
			result[k] = v
		}
	}

	return result
}

// ============================================================================
// 冻结与元信息
// ============================================================================

// Freeze 冻结配置，之后任何修改操作都会返回错误。
func (s *Settings) Freeze() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frozen = true
}

// IsFrozen 返回配置是否已冻结。
func (s *Settings) IsFrozen() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.frozen
}

// GetPriority 返回指定配置项的当前优先级，不存在返回 -1。
func (s *Settings) GetPriority(key string) Priority {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if attr, ok := s.attributes[key]; ok {
		return attr.priority
	}
	return -1
}

// Has 检查配置项是否存在。
func (s *Settings) Has(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.attributes[key]
	return ok
}

// Keys 返回所有配置项的键。
func (s *Settings) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0, len(s.attributes))
	for k := range s.attributes {
		keys = append(keys, k)
	}
	return keys
}

// Copy 返回配置的深拷贝。
func (s *Settings) Copy() *Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	newSettings := &Settings{
		frozen:     false, // 拷贝不继承冻结状态
		attributes: make(map[string]*settingsAttribute, len(s.attributes)),
	}

	for k, v := range s.attributes {
		newSettings.attributes[k] = &settingsAttribute{
			value:    v.value,
			priority: v.priority,
		}
	}

	return newSettings
}

// FrozenCopy 返回配置的冻结拷贝。
func (s *Settings) FrozenCopy() *Settings {
	c := s.Copy()
	c.Freeze()
	return c
}

// ToMap 将所有配置导出为 map[string]any。
func (s *Settings) ToMap() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]any, len(s.attributes))
	for k, v := range s.attributes {
		result[k] = v.value
	}
	return result
}
