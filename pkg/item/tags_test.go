package item

import (
	"errors"
	"testing"
)

// ============================================================================
// 测试用 struct
// ============================================================================

type bookItem struct {
	Title  string  `item:"title,required"`
	Author string  `item:"author,default=Unknown"`
	Price  float64 `item:"price,required"`
	ISBN   string  `item:"isbn"`
}

type configItem struct {
	Host    string `item:"host,required,default=localhost"`
	Port    int    `item:"port,default=8080"`
	Debug   bool   `item:"debug,default=true"`
	Timeout int    `item:"timeout"`
}

type mixedItem struct {
	Name     string  `item:"name,required"`
	Score    float64 `item:"score,default=0.5"`
	Category string  `item:"category,omitempty"`
}

// ============================================================================
// Validate 测试
// ============================================================================

func TestValidateRequiredFieldEmpty(t *testing.T) {
	book := &bookItem{Author: "Test"}
	err := Validate(book)
	if err == nil {
		t.Fatal("expected validation error for empty required fields")
	}

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}

	// Title 和 Price 都是 required 且为零值
	if len(ve.Errors) != 2 {
		t.Fatalf("expected 2 field errors, got %d: %v", len(ve.Errors), ve.Errors)
	}

	fieldNames := make(map[string]bool)
	for _, fe := range ve.Errors {
		fieldNames[fe.Field] = true
	}
	if !fieldNames["title"] {
		t.Error("expected error for field 'title'")
	}
	if !fieldNames["price"] {
		t.Error("expected error for field 'price'")
	}
}

func TestValidateRequiredFieldFilled(t *testing.T) {
	book := &bookItem{Title: "Go Book", Price: 29.99}
	err := Validate(book)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Author 应该被填充为默认值 "Unknown"
	if book.Author != "Unknown" {
		t.Errorf("expected Author='Unknown', got %q", book.Author)
	}
}

func TestValidateDefaultValues(t *testing.T) {
	cfg := &configItem{}
	err := Validate(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Host 有 required + default，default 先填充，required 不报错
	if cfg.Host != "localhost" {
		t.Errorf("expected Host='localhost', got %q", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected Port=8080, got %d", cfg.Port)
	}
	if cfg.Debug != true {
		t.Errorf("expected Debug=true, got %v", cfg.Debug)
	}
}

func TestValidateDefaultNotOverrideExisting(t *testing.T) {
	cfg := &configItem{Host: "example.com", Port: 9090}
	err := Validate(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 已有值不应被默认值覆盖
	if cfg.Host != "example.com" {
		t.Errorf("expected Host='example.com', got %q", cfg.Host)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected Port=9090, got %d", cfg.Port)
	}
}

func TestValidateNilItem(t *testing.T) {
	err := Validate(nil)
	if !errors.Is(err, ErrUnsupportedItem) {
		t.Errorf("expected ErrUnsupportedItem, got %v", err)
	}
}

func TestValidateNonPointer(t *testing.T) {
	book := bookItem{Title: "test"}
	err := Validate(book)
	if !errors.Is(err, ErrUnsupportedItem) {
		t.Errorf("expected ErrUnsupportedItem for non-pointer, got %v", err)
	}
}

func TestValidateNilPointer(t *testing.T) {
	var book *bookItem
	err := Validate(book)
	if !errors.Is(err, ErrUnsupportedItem) {
		t.Errorf("expected ErrUnsupportedItem for nil pointer, got %v", err)
	}
}

func TestValidateAllValid(t *testing.T) {
	book := &bookItem{Title: "Go", Author: "Author", Price: 10.0, ISBN: "123"}
	err := Validate(book)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFloatDefault(t *testing.T) {
	item := &mixedItem{Name: "test"}
	err := Validate(item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Score != 0.5 {
		t.Errorf("expected Score=0.5, got %f", item.Score)
	}
}

// ============================================================================
// parseTagOptions 测试
// ============================================================================

func TestParseTagOptions(t *testing.T) {
	tests := []struct {
		tag      string
		required bool
		defVal   string
		hasDef   bool
	}{
		{"title,required", true, "", false},
		{"author,default=Unknown", false, "Unknown", true},
		{"host,required,default=localhost", true, "localhost", true},
		{"name", false, "", false},
		{"", false, "", false},
		{"field,omitempty", false, "", false},
	}

	for _, tt := range tests {
		opts := parseTagOptions(tt.tag)
		if opts.IsRequired() != tt.required {
			t.Errorf("tag=%q: IsRequired()=%v, want %v", tt.tag, opts.IsRequired(), tt.required)
		}
		defVal, hasDef := opts.Default()
		if hasDef != tt.hasDef {
			t.Errorf("tag=%q: hasDefault=%v, want %v", tt.tag, hasDef, tt.hasDef)
		}
		if defVal != tt.defVal {
			t.Errorf("tag=%q: default=%q, want %q", tt.tag, defVal, tt.defVal)
		}
	}
}

func TestParseTagOptionsOmitempty(t *testing.T) {
	opts := parseTagOptions("field,omitempty")
	if !opts.IsOmitempty() {
		t.Error("expected omitempty=true")
	}
	if opts.IsRequired() {
		t.Error("expected required=false")
	}
}

// ============================================================================
// IsValidationError 测试
// ============================================================================

func TestIsValidationError(t *testing.T) {
	ve := &ValidationError{Errors: []FieldError{{Field: "test", Message: "required"}}}
	if !IsValidationError(ve) {
		t.Error("expected IsValidationError=true")
	}
	if IsValidationError(errors.New("other error")) {
		t.Error("expected IsValidationError=false for non-ValidationError")
	}
}

// ============================================================================
// ValidationError.Error() 测试
// ============================================================================

func TestValidationErrorSingleField(t *testing.T) {
	ve := &ValidationError{Errors: []FieldError{{Field: "title", Message: "required field is empty"}}}
	expected := `item validation failed: field "title": required field is empty`
	if ve.Error() != expected {
		t.Errorf("got %q, want %q", ve.Error(), expected)
	}
}

func TestValidationErrorMultipleFields(t *testing.T) {
	ve := &ValidationError{Errors: []FieldError{
		{Field: "title", Message: "required field is empty"},
		{Field: "price", Message: "required field is empty"},
	}}
	msg := ve.Error()
	if !contains(msg, "title") || !contains(msg, "price") {
		t.Errorf("error message should mention both fields: %s", msg)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// GetTagOptions 测试
// ============================================================================

func TestGetTagOptions(t *testing.T) {
	book := &bookItem{}
	opts := GetTagOptions(book, "title")
	if !opts.IsRequired() {
		t.Error("expected title to be required")
	}

	opts = GetTagOptions(book, "author")
	defVal, hasDef := opts.Default()
	if !hasDef || defVal != "Unknown" {
		t.Errorf("expected default='Unknown', got %q (has=%v)", defVal, hasDef)
	}

	// 不存在的字段
	opts = GetTagOptions(book, "nonexistent")
	if opts.IsRequired() {
		t.Error("nonexistent field should not be required")
	}
}

func TestGetTagOptionsNilItem(t *testing.T) {
	opts := GetTagOptions(nil, "title")
	if opts.IsRequired() {
		t.Error("nil item should return empty options")
	}
}
