// Package item 的 struct tag 增强：字段验证与元数据约束。
//
// 本文件提供基于 struct tag 的字段元数据增强功能，替代 Scrapy Python 版本中
// ItemMeta 元类 + Field 描述符的运行时校验机制。
//
// # 支持的 tag 语法
//
// struct tag 使用 `item` 键，格式为：
//
//	`item:"fieldname,option1,option2=value"`
//
// 支持的选项：
//   - required：字段为必填，Validate 时若为零值则报错
//   - default=value：字段默认值（字符串形式，Validate 时若为零值则填充）
//   - omitempty：序列化时忽略零值字段（与 json tag 语义一致）
//
// # 使用方式
//
//	type Book struct {
//	    Title  string  `item:"title,required"`
//	    Author string  `item:"author,default=Unknown"`
//	    Price  float64 `item:"price"`
//	}
//
//	book := &Book{Title: "Go Programming"}
//	if err := item.Validate(book); err != nil {
//	    // 处理验证错误
//	}
//	// book.Author 已被填充为 "Unknown"
//
// # 与 Scrapy 的对比
//
// Scrapy 的 Field 描述符通过 ItemMeta 元类在类定义时收集字段信息，
// 并在 ItemAdapter 中提供 field_names() / field_meta() 等方法。
// Go 版本通过 struct tag + reflect 在运行时解析，并通过 sync.Map 缓存元数据。
package item

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ============================================================================
// 验证错误
// ============================================================================

// ValidationError 表示 Item 验证失败的错误。
// 包含所有验证失败的字段信息。
type ValidationError struct {
	Errors []FieldError
}

// FieldError 表示单个字段的验证错误。
type FieldError struct {
	Field   string // 字段名（item tag 中的名称）
	Message string // 错误描述
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("item validation failed: field %q: %s", e.Errors[0].Field, e.Errors[0].Message)
	}
	var msgs []string
	for _, fe := range e.Errors {
		msgs = append(msgs, fmt.Sprintf("field %q: %s", fe.Field, fe.Message))
	}
	return fmt.Sprintf("item validation failed: %s", strings.Join(msgs, "; "))
}

// IsValidationError 判断 err 是否为 ValidationError。
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

// ============================================================================
// Tag 选项常量
// ============================================================================

const (
	// TagOptionRequired 标记字段为必填。
	TagOptionRequired = "required"
	// TagOptionDefault 标记字段的默认值前缀。
	TagOptionDefault = "default"
	// TagOptionOmitempty 标记序列化时忽略零值。
	TagOptionOmitempty = "omitempty"
)

// ============================================================================
// Validate 函数
// ============================================================================

// Validate 验证 Item 的字段约束。
//
// 验证规则：
//  1. 对标记了 `required` 的字段，检查是否为零值
//  2. 对标记了 `default=value` 的字段，若为零值则填充默认值
//
// item 必须是指向 struct 的指针（以便填充默认值）。
// 若 item 不是 *struct 则返回 ErrUnsupportedItem。
//
// 处理顺序：先填充默认值，再检查 required 约束。
// 这意味着同时标记 `required` 和 `default` 的字段不会报错（默认值会先被填充）。
func Validate(item any) error {
	if item == nil {
		return ErrUnsupportedItem
	}

	rv := reflect.ValueOf(item)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("%w: Validate requires a non-nil pointer to struct", ErrUnsupportedItem)
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("%w: Validate requires a pointer to struct, got *%s", ErrUnsupportedItem, rv.Kind())
	}

	meta := getStructMetadata(rv.Type())
	var fieldErrors []FieldError

	for _, name := range meta.orderedNames {
		fm := meta.fieldByName[name]
		fv := rv.FieldByIndex(fm.fieldIndex)

		// 解析 tag 选项
		opts := parseTagOptions(fm.tagItem)

		// 1. 填充默认值（若字段为零值且有 default 选项）
		if defaultVal, hasDefault := opts.Default(); hasDefault && fv.IsZero() {
			if err := setFieldFromString(fv, defaultVal); err != nil {
				fieldErrors = append(fieldErrors, FieldError{
					Field:   name,
					Message: fmt.Sprintf("failed to set default value %q: %v", defaultVal, err),
				})
				continue
			}
		}

		// 2. 检查 required 约束
		if opts.IsRequired() && fv.IsZero() {
			fieldErrors = append(fieldErrors, FieldError{
				Field:   name,
				Message: "required field is empty",
			})
		}
	}

	if len(fieldErrors) > 0 {
		return &ValidationError{Errors: fieldErrors}
	}
	return nil
}

// ============================================================================
// Tag 选项解析
// ============================================================================

// tagOptions 表示解析后的 tag 选项集合。
type tagOptions struct {
	options map[string]string // key → value（无值选项 value 为空字符串）
}

// parseTagOptions 解析 item tag 的选项部分。
// 输入格式："fieldname,option1,option2=value"
// 返回除 fieldname 外的所有选项。
func parseTagOptions(tag string) tagOptions {
	opts := tagOptions{options: make(map[string]string)}
	if tag == "" {
		return opts
	}

	parts := strings.Split(tag, ",")
	// 跳过第一个 token（字段名）
	for i := 1; i < len(parts); i++ {
		p := strings.TrimSpace(parts[i])
		if p == "" {
			continue
		}
		if eq := strings.Index(p, "="); eq > 0 {
			key := strings.TrimSpace(p[:eq])
			value := strings.TrimSpace(p[eq+1:])
			opts.options[key] = value
		} else {
			opts.options[p] = ""
		}
	}
	return opts
}

// IsRequired 返回是否标记了 required。
func (o tagOptions) IsRequired() bool {
	_, ok := o.options[TagOptionRequired]
	return ok
}

// Default 返回 default 值。若未设置返回 ("", false)。
func (o tagOptions) Default() (string, bool) {
	v, ok := o.options[TagOptionDefault]
	return v, ok
}

// IsOmitempty 返回是否标记了 omitempty。
func (o tagOptions) IsOmitempty() bool {
	_, ok := o.options[TagOptionOmitempty]
	return ok
}

// Has 返回是否存在指定选项。
func (o tagOptions) Has(key string) bool {
	_, ok := o.options[key]
	return ok
}

// ============================================================================
// 默认值填充
// ============================================================================

// setFieldFromString 将字符串值设置到 reflect.Value 中。
// 支持基本类型：string、int 系列、uint 系列、float 系列、bool。
func setFieldFromString(fv reflect.Value, s string) error {
	if !fv.CanSet() {
		return fmt.Errorf("field is not settable")
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	default:
		return fmt.Errorf("unsupported field type %s for default value", fv.Kind())
	}
	return nil
}

// ============================================================================
// GetTagOptions 公共辅助
// ============================================================================

// GetTagOptions 返回指定字段的 tag 选项。
// 若 item 不是 struct/*struct 或字段不存在，返回空 tagOptions。
//
// 此函数供外部工具（如代码生成器）使用。
func GetTagOptions(item any, fieldName string) tagOptions {
	if item == nil {
		return tagOptions{options: make(map[string]string)}
	}

	rv := reflect.ValueOf(item)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return tagOptions{options: make(map[string]string)}
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return tagOptions{options: make(map[string]string)}
	}

	meta := getStructMetadata(rv.Type())
	fm, ok := meta.fieldByName[fieldName]
	if !ok {
		return tagOptions{options: make(map[string]string)}
	}
	return parseTagOptions(fm.tagItem)
}
