package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// TestLogger — NewTestLoggerWithLevel
// ===========================================================================

func TestNewTestLoggerWithLevel_FiltersDebug(t *testing.T) {
	logger := NewTestLoggerWithLevel("warn")
	logger.Debug("should not appear")
	logger.Info("should not appear either")
	logger.Warn("warning visible")
	logger.Error("error visible")
	assert.Equal(t, 2, logger.Count(""))
	logger.AssertLogged(t, "warning visible")
	logger.AssertLogged(t, "error visible")
}

func TestNewTestLoggerWithLevel_DebugShowsAll(t *testing.T) {
	logger := NewTestLoggerWithLevel("debug")
	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")
	assert.Equal(t, 4, logger.Count(""))
}

// ===========================================================================
// TestLogger — Debug method
// ===========================================================================

func TestTestLogger_Debug(t *testing.T) {
	logger := NewTestLogger()
	logger.Debug("debug msg", String("k", "v"))
	assert.Equal(t, 1, logger.Count("debug"))
	logger.AssertLoggedWithField(t, "debug msg", "k", "v")
}

// ===========================================================================
// TestLogger — Context methods
// ===========================================================================

func TestTestLogger_DebugContext(t *testing.T) {
	logger := NewTestLogger()
	logger.DebugContext(context.Background(), "debug ctx")
	assert.Equal(t, 1, logger.Count("debug"))
}

func TestTestLogger_InfoContext(t *testing.T) {
	logger := NewTestLogger()
	logger.InfoContext(context.Background(), "info ctx", String("k", "v"))
	assert.Equal(t, 1, logger.Count("info"))
	logger.AssertLoggedWithField(t, "info ctx", "k", "v")
}

func TestTestLogger_WarnContext(t *testing.T) {
	logger := NewTestLogger()
	logger.WarnContext(context.Background(), "warn ctx")
	assert.Equal(t, 1, logger.Count("warn"))
}

func TestTestLogger_ErrorContext(t *testing.T) {
	logger := NewTestLogger()
	logger.ErrorContext(context.Background(), "error ctx")
	assert.Equal(t, 1, logger.Count("error"))
}

func TestTestLogger_ContextLevelFiltering(t *testing.T) {
	logger := NewTestLoggerWithLevel("error")
	logger.DebugContext(context.Background(), "d")
	logger.InfoContext(context.Background(), "i")
	logger.WarnContext(context.Background(), "w")
	assert.Equal(t, 0, logger.Count(""))
	logger.ErrorContext(context.Background(), "e")
	assert.Equal(t, 1, logger.Count(""))
}

// ===========================================================================
// TestLogger — With / WithTrace / WithComponent
// ===========================================================================

func TestTestLogger_With(t *testing.T) {
	logger := NewTestLogger()
	child := logger.With(String("session", "s1"))
	child.Info("child msg")

	childTL := child.(*TestLogger)
	entries := childTL.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "s1", entries[0].Fields["session"])
}

func TestTestLogger_ChildLoggersShareParentEntries(t *testing.T) {
	logger := NewTestLogger()

	logger.With(String("session", "s1")).Info("with-child")
	logger.WithTrace("trace-1", "span-1").Info("trace-child")
	logger.WithComponent("router").Info("component-child")

	parentEntries := logger.Entries()
	require.Len(t, parentEntries, 3)
	assert.Equal(t, "with-child", parentEntries[0].Message)
	assert.Equal(t, "s1", parentEntries[0].Fields["session"])
	assert.Equal(t, "trace-child", parentEntries[1].Message)
	assert.Equal(t, "trace-1", parentEntries[1].TraceID)
	assert.Equal(t, "span-1", parentEntries[1].SpanID)
	assert.Equal(t, "component-child", parentEntries[2].Message)
	assert.Equal(t, "router", parentEntries[2].Fields[FieldComponent])
}

func TestTestLogger_WithTrace(t *testing.T) {
	logger := NewTestLogger()
	child := logger.WithTrace("trace-abc", "span-def")
	child.Info("traced msg")

	childTL := child.(*TestLogger)
	entries := childTL.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "trace-abc", entries[0].TraceID)
	assert.Equal(t, "span-def", entries[0].SpanID)
}

func TestTestLogger_WithComponent(t *testing.T) {
	logger := NewTestLogger()
	child := logger.WithComponent("router")
	child.Info("component msg")

	childTL := child.(*TestLogger)
	entries := childTL.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "router", entries[0].Fields[FieldComponent])
}

func TestTestLogger_WithTrace_OverridesContextTrace(t *testing.T) {
	logger := NewTestLogger()
	child := logger.WithTrace("instance-trace", "instance-span")
	// DebugContext with no real OTel context should fall back to instance trace
	child.DebugContext(context.Background(), "msg")

	childTL := child.(*TestLogger)
	entries := childTL.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "instance-trace", entries[0].TraceID)
	assert.Equal(t, "instance-span", entries[0].SpanID)
}

// ===========================================================================
// TestLogger — Entries / Reset / FindEntries
// ===========================================================================

func TestTestLogger_Entries_ReturnsCopy(t *testing.T) {
	logger := NewTestLogger()
	logger.Info("a", String("n", "1"))
	logger.Info("b")

	entries := logger.Entries()
	assert.Len(t, entries, 2)
	// Modifying the copy should not affect the original
	entries[0].Message = "modified"
	entries[0].Fields["n"] = "modified"
	original := logger.Entries()
	assert.Equal(t, "a", original[0].Message)
	assert.Equal(t, "1", original[0].Fields["n"])
}

func TestTestLogger_Reset(t *testing.T) {
	logger := NewTestLogger()
	logger.Info("a")
	logger.Info("b")
	assert.Equal(t, 2, logger.Count(""))

	logger.Reset()
	assert.Equal(t, 0, logger.Count(""))
	assert.Empty(t, logger.Entries())
}

func TestTestLogger_FindEntries(t *testing.T) {
	logger := NewTestLogger()
	logger.Info("target", String("n", "1"))
	logger.Warn("other")
	logger.Info("target", String("n", "2"))

	found := logger.FindEntries("target")
	assert.Len(t, found, 2)
	assert.Equal(t, "1", found[0].Fields["n"])
	assert.Equal(t, "2", found[1].Fields["n"])

	found[0].Fields["n"] = "modified"
	foundAgain := logger.FindEntries("target")
	assert.Equal(t, "1", foundAgain[0].Fields["n"])
}

func TestTestLogger_FindEntries_NoMatch(t *testing.T) {
	logger := NewTestLogger()
	logger.Info("something")
	found := logger.FindEntries("nonexistent")
	assert.Empty(t, found)
}

// ===========================================================================
// TestLogger — AssertLoggedWithFields
// ===========================================================================

func TestTestLogger_AssertLoggedWithFields(t *testing.T) {
	logger := NewTestLogger()
	logger.Info("event", String("tool_id", "t1"), Int("count", 5))

	logger.AssertLoggedWithFields(t, "event", map[string]any{
		"tool_id": "t1",
		"count":   5,
	})
}

// ===========================================================================
// TestLogger — Count
// ===========================================================================

func TestTestLogger_Count_EmptyLevel(t *testing.T) {
	logger := NewTestLogger()
	logger.Info("a")
	logger.Warn("b")
	assert.Equal(t, 2, logger.Count(""))
}

func TestTestLogger_Count_SpecificLevel(t *testing.T) {
	logger := NewTestLogger()
	logger.Info("a")
	logger.Info("b")
	logger.Warn("c")
	assert.Equal(t, 2, logger.Count("info"))
	assert.Equal(t, 1, logger.Count("warn"))
	assert.Equal(t, 0, logger.Count("error"))
}

// ===========================================================================
// ZerologLogger — all methods
// ===========================================================================

func TestZerologLogger_Debug(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "svc", "debug")
	logger.Debug("debug msg", String("k", "v"))
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "debug msg", entry["message"])
	assert.Equal(t, "v", entry["k"])
}

func TestZerologLogger_Warn(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "svc", "warn")
	logger.Warn("warn msg")
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "warn msg", entry["message"])
}

func TestZerologLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "svc", "error")
	logger.Error("error msg")
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "error msg", entry["message"])
}

func TestZerologLogger_DebugContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "svc", "debug")
	logger.DebugContext(context.Background(), "debug ctx msg", String("x", "y"))
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "debug ctx msg", entry["message"])
	assert.Equal(t, "y", entry["x"])
}

func TestZerologLogger_InfoContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "svc", "info")
	logger.InfoContext(context.Background(), "info ctx msg")
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "info ctx msg", entry["message"])
}

func TestZerologLogger_WarnContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "svc", "warn")
	logger.WarnContext(context.Background(), "warn ctx msg")
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "warn ctx msg", entry["message"])
}

func TestZerologLogger_ErrorContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "svc", "error")
	logger.ErrorContext(context.Background(), "error ctx msg")
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "error ctx msg", entry["message"])
}

// ===========================================================================
// addField — all type branches
// ===========================================================================

func TestZerologLogger_AddFieldAllTypes(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "svc", "info")

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dur := 500 * time.Millisecond

	logger.Info("all fields",
		String("s", "str"),
		Int("i", 42),
		Int64("i64", int64(99)),
		Float64("f64", 3.14),
		Bool("b", true),
		Field{Key: "dur", Value: dur},
		Field{Key: "t", Value: now},
		Strings("tags", []string{"a", "b"}),
		Error(errors.New("test-err")),
		Money("cost", "10.00", "LAC"),
		Field{Key: "nil_val", Value: nil},
		Any("custom", []int{1, 2, 3}),
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "str", entry["s"])
	assert.Equal(t, float64(42), entry["i"])
	assert.Equal(t, float64(99), entry["i64"])
	assert.InDelta(t, 3.14, entry["f64"], 0.001)
	assert.Equal(t, true, entry["b"])
	assert.NotNil(t, entry["dur"])
	assert.NotNil(t, entry["t"])
	assert.NotNil(t, entry["tags"])
	assert.Equal(t, "test-err", entry["error"])
	assert.NotNil(t, entry["cost"])
}

func TestZerologLogger_NonFiniteFloatFieldsStayParseable(t *testing.T) {
	var buf bytes.Buffer
	logger := NewZerologWithWriter(&buf, "svc", "info")

	logger.Info("non-finite",
		Float64("nan", math.NaN()),
		Float64("pos_inf", math.Inf(1)),
		Float64("neg_inf", math.Inf(-1)),
		Float64("finite", 1.25),
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "non-finite", entry["message"])
	assert.Equal(t, "NaN", entry["nan"])
	assert.Equal(t, "+Inf", entry["pos_inf"])
	assert.Equal(t, "-Inf", entry["neg_inf"])
	assert.Equal(t, 1.25, entry["finite"])
}

// ===========================================================================
// addFieldToContext — exercised via With
// ===========================================================================

func TestZerologLogger_WithAllFieldTypes(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := NewZerologWithWriter(&buf, "svc", "info")

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dur := 500 * time.Millisecond

	child := baseLogger.With(
		String("s", "str"),
		Int("i", 42),
		Int64("i64", int64(99)),
		Float64("f64", 3.14),
		Bool("b", true),
		Field{Key: "dur", Value: dur},
		Field{Key: "t", Value: now},
		Strings("tags", []string{"a", "b"}),
		Money("cost", "10.00", "LAC"),
		Field{Key: "nil_val", Value: nil},
	)
	child.Info("child with fields")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "str", entry["s"])
	assert.Equal(t, float64(42), entry["i"])
	assert.Equal(t, float64(99), entry["i64"])
	assert.InDelta(t, 3.14, entry["f64"], 0.001)
	assert.Equal(t, true, entry["b"])
}

func TestZerologLogger_WithNonFiniteFloatFieldsStayParseable(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := NewZerologWithWriter(&buf, "svc", "info")

	child := baseLogger.With(
		Float64("nan", math.NaN()),
		Float64("pos_inf", math.Inf(1)),
		Float64("neg_inf", math.Inf(-1)),
		Float64("finite", 1.25),
	)
	child.Info("child")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "child", entry["message"])
	assert.Equal(t, "NaN", entry["nan"])
	assert.Equal(t, "+Inf", entry["pos_inf"])
	assert.Equal(t, "-Inf", entry["neg_inf"])
	assert.Equal(t, 1.25, entry["finite"])
}

// ===========================================================================
// parseZerologLevel
// ===========================================================================

func TestParseZerologLevel(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"debug"},
		{"info"},
		{"warn"},
		{"error"},
		{"unknown"},
		{"  DEBUG  "},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Just ensure no panics
			_ = parseZerologLevel(tt.input)
		})
	}
}

// ===========================================================================
// Logger writeColor with structured fields (covers addField via writeColor)
// ===========================================================================

func TestLogger_WriteColor_AllFieldTypes(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "")

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	logger.Info("color fields",
		String("s", "str"),
		Int("i", 42),
		Bool("b", true),
		Field{Key: "nil_val", Value: nil},
		Field{Key: "t", Value: now.Format(time.RFC3339Nano)},
	)

	out := stripANSI(buf.String())
	assert.Contains(t, out, "color fields")
	assert.Contains(t, out, "s=str")
	assert.Contains(t, out, "i=42")
	assert.Contains(t, out, "b=true")
}

// ===========================================================================
// Logger format-based JSON with trace + component + span
// ===========================================================================

func TestLogger_FormatJSONWithTraceAndComponent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	child := logger.WithTrace("tr-1", "sp-1").WithComponent("api")
	child.Infof("test")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "tr-1", entry["trace_id"])
	assert.Equal(t, "sp-1", entry["span_id"])
	assert.Equal(t, "api", entry["component"])
	assert.Equal(t, "tr-1", entry["correlation_id"])
}

// ===========================================================================
// Logger color output with trace and base fields via format-based methods
// ===========================================================================

func TestLogger_FormatColorWithTraceAndFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "")
	child := logger.With(String("env", "prod")).WithTrace("tr-2", "sp-2")
	child.Infof("color trace")

	out := stripANSI(buf.String())
	assert.Contains(t, out, "color trace")
	assert.Contains(t, out, "trace_id=tr-2")
	assert.Contains(t, out, "env=prod")
}

// ===========================================================================
// Logger DebugContext / ErrorContext format-based methods
// ===========================================================================

func TestLogger_FormatDebugContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "json")
	logger.DebugContext(context.Background(), "debug %s", "ctx")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "debug ctx", entry["message"])
	assert.Equal(t, "debug", entry["level"])
}

func TestLogger_FormatErrorContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "error", "json")
	logger.ErrorContext(context.Background(), "error %s", "ctx")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "error ctx", entry["message"])
	assert.Equal(t, "error", entry["level"])
}

func TestLogger_FormatSuccessContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	logger.SuccessContext(context.Background(), "ok %s", "done")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "ok done", entry["message"])
	assert.Equal(t, "ok", entry["level"])
}

func TestLogger_FormatWarnContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "warn", "json")
	logger.WarnContext(context.Background(), "warn %s", "ctx")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "warn ctx", entry["message"])
	assert.Equal(t, "warn", entry["level"])
}

// ===========================================================================
// traceDetails nil context
// ===========================================================================

func TestTraceDetails_NilContext(t *testing.T) {
	traceID, spanID := traceDetails(context.Background())
	assert.Equal(t, "", traceID)
	assert.Equal(t, "", spanID)
}

// ===========================================================================
// Logger structured level filtering for Warn
// ===========================================================================

func TestLogger_StructuredWarnLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "error", "json")
	logger.Warn("should not appear")
	assert.Empty(t, buf.String())
}

// ===========================================================================
// Logger structured Debug level filtering
// ===========================================================================

func TestLogger_StructuredDebugLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	logger.Debug("should not appear")
	assert.Empty(t, buf.String())
}

// ===========================================================================
// NewZerolog (uses os.Stdout — just ensure no panic)
// ===========================================================================

func TestNewZerolog_NoPanic(t *testing.T) {
	logger := NewZerolog("test-svc", "info")
	assert.NotNil(t, logger)
}

// ===========================================================================
// Logger writeJSON with nil fields
// ===========================================================================

func TestLogger_JSONStructuredNilField(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	logger.Info("test", Field{Key: "nil_key", Value: nil})

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	_, exists := entry["nil_key"]
	assert.False(t, exists) // nil fields excluded
}

// ===========================================================================
// Logger writeColor base field nil exclusion
// ===========================================================================

func TestLogger_ColorBaseFieldNilExcluded(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "")
	child := logger.With(Field{Key: "nil_field", Value: nil})
	child.Infof("test")

	out := stripANSI(buf.String())
	assert.NotContains(t, out, "nil_field")
}

// ===========================================================================
// StructuredLoggerFromContextOrDefault with present logger
// ===========================================================================

func TestStructuredLoggerFromContextOrDefault_Present(t *testing.T) {
	logger := NewTestLogger()
	ctx := WithStructuredLogger(context.Background(), logger)
	result := StructuredLoggerFromContextOrDefault(ctx, NewTestLogger())
	assert.Equal(t, logger, result)
}

// ===========================================================================
// Multiple splits from writeJSON (line count sanity)
// ===========================================================================

func TestLogger_JSONMultipleEntries(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "json")
	logger.Info("a")
	logger.Warn("b")
	logger.Error("c")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 3)
}
