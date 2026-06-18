package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestZerologLoggerBasic(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "test-service", "debug")

	logger.Info("test_message", String("key", "value"), Int("count", 42))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}

	if entry["message"] != "test_message" {
		t.Errorf("expected message 'test_message', got %v", entry["message"])
	}
	if entry["key"] != "value" {
		t.Errorf("expected key 'value', got %v", entry["key"])
	}
	if entry["count"] != float64(42) {
		t.Errorf("expected count 42, got %v", entry["count"])
	}
	if entry["service"] != "test-service" {
		t.Errorf("expected service 'test-service', got %v", entry["service"])
	}
}

func TestZerologLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "test", "warn")

	logger.Debug("should_not_appear")
	logger.Info("should_not_appear")
	logger.Warn("should_appear")

	// Only warn message should be present
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Errorf("expected 1 log line, got %d", len(lines))
	}

	var entry map[string]any
	if err := json.Unmarshal(lines[0], &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}
	if entry["message"] != "should_appear" {
		t.Errorf("expected message 'should_appear', got %v", entry["message"])
	}
}

func TestZerologLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := NewZerologWithWriter(&buf, "test", "info")

	childLogger := baseLogger.With(String("session_id", "sess-123"))
	childLogger.Info("child_log")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}
	if entry["session_id"] != "sess-123" {
		t.Errorf("expected session_id 'sess-123', got %v", entry["session_id"])
	}
}

func TestZerologLoggerWithTrace(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := NewZerologWithWriter(&buf, "test", "info")

	tracedLogger := baseLogger.WithTrace("tr_abc123", "sp_def456")
	tracedLogger.Info("traced_log")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}
	if entry["trace_id"] != "tr_abc123" {
		t.Errorf("expected trace_id 'tr_abc123', got %v", entry["trace_id"])
	}
	if entry["span_id"] != "sp_def456" {
		t.Errorf("expected span_id 'sp_def456', got %v", entry["span_id"])
	}
}

func TestZerologLoggerWithComponent(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := NewZerologWithWriter(&buf, "test", "info")

	componentLogger := baseLogger.WithComponent("quote_handler")
	componentLogger.Info("component_log")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}
	if entry["component"] != "quote_handler" {
		t.Errorf("expected component 'quote_handler', got %v", entry["component"])
	}
}

func TestZerologRedactsPII(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "test", "info")

	logger.Info("user login", String("auth", "Bearer abc.def.ghi"))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}
	auth := entry["auth"].(string)
	if auth != "Bearer [REDACTED]" {
		t.Errorf("expected auth to be redacted, got %v", auth)
	}
}

func TestFieldHelpers(t *testing.T) {
	tests := []struct {
		name    string
		field   Field
		wantKey string
		wantVal any
	}{
		{"String", String("key", "value"), "key", "value"},
		{"Int", Int("count", 42), "count", 42},
		{"Int64", Int64("big", 123456789), "big", int64(123456789)},
		{"Float64", Float64("ratio", 3.14), "ratio", 3.14},
		{"Bool", Bool("enabled", true), "enabled", true},
		{"Duration", Duration("latency", 500000000), "latency_ms", int64(500)},
		{"Error nil", Error(nil), "error", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.field.Key != tt.wantKey {
				t.Errorf("Key = %v, want %v", tt.field.Key, tt.wantKey)
			}
			if tt.field.Value != tt.wantVal {
				t.Errorf("Value = %v, want %v", tt.field.Value, tt.wantVal)
			}
		})
	}
}

func TestMoneyField(t *testing.T) {
	field := Money("cost", "12.50", "LAC")
	if field.Key != "cost" {
		t.Errorf("Key = %v, want 'cost'", field.Key)
	}
	mv, ok := field.Value.(MoneyValue)
	if !ok {
		t.Fatalf("Value is not MoneyValue, got %T", field.Value)
	}
	if mv.Amount != "12.50" {
		t.Errorf("Amount = %v, want '12.50'", mv.Amount)
	}
	if mv.Currency != "LAC" {
		t.Errorf("Currency = %v, want 'LAC'", mv.Currency)
	}
}

func TestTestLoggerAssertions(t *testing.T) {
	logger := NewTestLogger()

	logger.Info("test_event", String("key", "value"), Int("count", 5))
	logger.Warn("warning_event")
	logger.Error("error_event")

	// Test assertion methods
	logger.AssertLogged(t, "test_event")
	logger.AssertLogged(t, "warning_event")
	logger.AssertLogged(t, "error_event")
	logger.AssertNotLogged(t, "nonexistent_event")
	logger.AssertLoggedWithLevel(t, "warn", "warning_event")
	logger.AssertLoggedWithField(t, "test_event", "key", "value")

	if logger.Count("") != 3 {
		t.Errorf("expected 3 total entries, got %d", logger.Count(""))
	}
	if logger.Count("warn") != 1 {
		t.Errorf("expected 1 warn entry, got %d", logger.Count("warn"))
	}
}

func TestContextFunctions(t *testing.T) {
	logger := NewTestLogger()

	// Test WithStructuredLogger and retrieval
	ctx := WithStructuredLogger(context.Background(), logger)
	retrieved := StructuredLoggerFromContext(ctx)
	if retrieved != logger {
		t.Error("retrieved logger doesn't match original")
	}

	// Test nil context handling (intentionally passing nil to verify defensive behavior)
	//nolint:staticcheck // SA1012: intentionally testing nil context handling
	if StructuredLoggerFromContext(nil) != nil {
		t.Error("expected nil for nil context")
	}

	// Test missing logger
	if StructuredLoggerFromContext(context.Background()) != nil {
		t.Error("expected nil for context without logger")
	}

	// Test default fallback
	defaultLogger := NewTestLogger()
	result := StructuredLoggerFromContextOrDefault(context.Background(), defaultLogger)
	if result != defaultLogger {
		t.Error("expected default logger when not in context")
	}
}

func TestExtractTraceFromContext(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		traceID, spanID := extractTraceFromContext(context.TODO())
		if traceID != "" || spanID != "" {
			t.Fatalf("expected empty trace/span for nil context, got %q/%q", traceID, spanID)
		}
	})

	t.Run("no span", func(t *testing.T) {
		traceID, spanID := extractTraceFromContext(context.Background())
		if traceID != "" || spanID != "" {
			t.Fatalf("expected empty trace/span for context without span, got %q/%q", traceID, spanID)
		}
	})

	t.Run("invalid span context", func(t *testing.T) {
		ctx := trace.ContextWithSpanContext(context.Background(), trace.SpanContext{})
		traceID, spanID := extractTraceFromContext(ctx)
		if traceID != "" || spanID != "" {
			t.Fatalf("expected empty trace/span for invalid span context, got %q/%q", traceID, spanID)
		}
	})

	t.Run("valid span context", func(t *testing.T) {
		sc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
			SpanID:     trace.SpanID{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22},
			TraceFlags: trace.FlagsSampled,
		})
		ctx := trace.ContextWithSpanContext(context.Background(), sc)
		traceID, spanID := extractTraceFromContext(ctx)
		if traceID == "" || spanID == "" {
			t.Fatalf("expected trace/span IDs to be set")
		}
		if traceID != sc.TraceID().String() || spanID != sc.SpanID().String() {
			t.Fatalf("trace/span mismatch: got %q/%q want %q/%q", traceID, spanID, sc.TraceID().String(), sc.SpanID().String())
		}
	})
}
