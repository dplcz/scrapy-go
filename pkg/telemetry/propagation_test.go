package telemetry

import "testing"

func TestFormatTraceparent(t *testing.T) {
	tests := []struct {
		name     string
		sc       SpanContext
		expected string
	}{
		{
			name: "有效的 SpanContext",
			sc: SpanContext{
				TraceID:    "4bf92f3577b34da6a3ce929d0e0e4736",
				SpanID:     "00f067aa0ba902b7",
				TraceFlags: 0x01,
			},
			expected: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		},
		{
			name: "TraceFlags 为 0",
			sc: SpanContext{
				TraceID:    "abcdef1234567890abcdef1234567890",
				SpanID:     "1234567890abcdef",
				TraceFlags: 0x00,
			},
			expected: "00-abcdef1234567890abcdef1234567890-1234567890abcdef-00",
		},
		{
			name:     "无效的 SpanContext（空 TraceID）",
			sc:       SpanContext{SpanID: "00f067aa0ba902b7"},
			expected: "",
		},
		{
			name:     "无效的 SpanContext（空 SpanID）",
			sc:       SpanContext{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736"},
			expected: "",
		},
		{
			name:     "零值 SpanContext",
			sc:       SpanContext{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatTraceparent(tt.sc)
			if result != tt.expected {
				t.Errorf("FormatTraceparent() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseTraceparent(t *testing.T) {
	tests := []struct {
		name        string
		traceparent string
		wantValid   bool
		wantTraceID string
		wantSpanID  string
		wantFlags   byte
		wantRemote  bool
	}{
		{
			name:        "有效的 traceparent",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			wantValid:   true,
			wantTraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
			wantSpanID:  "00f067aa0ba902b7",
			wantFlags:   0x01,
			wantRemote:  true,
		},
		{
			name:        "flags 为 0",
			traceparent: "00-abcdef1234567890abcdef1234567890-1234567890abcdef-00",
			wantValid:   true,
			wantTraceID: "abcdef1234567890abcdef1234567890",
			wantSpanID:  "1234567890abcdef",
			wantFlags:   0x00,
			wantRemote:  true,
		},
		{
			name:        "空字符串",
			traceparent: "",
			wantValid:   false,
		},
		{
			name:        "无效版本号",
			traceparent: "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			wantValid:   false,
		},
		{
			name:        "格式不完整",
			traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736",
			wantValid:   false,
		},
		{
			name:        "完全无效的字符串",
			traceparent: "not-a-traceparent",
			wantValid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := ParseTraceparent(tt.traceparent)
			if sc.IsValid() != tt.wantValid {
				t.Errorf("ParseTraceparent(%q).IsValid() = %v, want %v", tt.traceparent, sc.IsValid(), tt.wantValid)
				return
			}
			if !tt.wantValid {
				return
			}
			if sc.TraceID != tt.wantTraceID {
				t.Errorf("TraceID = %q, want %q", sc.TraceID, tt.wantTraceID)
			}
			if sc.SpanID != tt.wantSpanID {
				t.Errorf("SpanID = %q, want %q", sc.SpanID, tt.wantSpanID)
			}
			if sc.TraceFlags != tt.wantFlags {
				t.Errorf("TraceFlags = %02x, want %02x", sc.TraceFlags, tt.wantFlags)
			}
			if sc.IsRemote != tt.wantRemote {
				t.Errorf("IsRemote = %v, want %v", sc.IsRemote, tt.wantRemote)
			}
		})
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	original := SpanContext{
		TraceID:    "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:     "00f067aa0ba902b7",
		TraceFlags: 0x01,
	}

	formatted := FormatTraceparent(original)
	if formatted == "" {
		t.Fatal("FormatTraceparent 返回空字符串")
	}

	parsed := ParseTraceparent(formatted)
	if !parsed.IsValid() {
		t.Fatal("ParseTraceparent 返回无效 SpanContext")
	}

	if parsed.TraceID != original.TraceID {
		t.Errorf("TraceID 不匹配: got %q, want %q", parsed.TraceID, original.TraceID)
	}
	if parsed.SpanID != original.SpanID {
		t.Errorf("SpanID 不匹配: got %q, want %q", parsed.SpanID, original.SpanID)
	}
	if parsed.TraceFlags != original.TraceFlags {
		t.Errorf("TraceFlags 不匹配: got %02x, want %02x", parsed.TraceFlags, original.TraceFlags)
	}
	// 注意：IsRemote 在 Parse 后为 true（从字符串恢复标记为远程）
	if !parsed.IsRemote {
		t.Error("从字符串恢复的 SpanContext 应标记为 IsRemote=true")
	}
}
