package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLogger_JSONStructuredFields_AllTypes verifies that every Field helper
// produces a value that survives the JSON-mode encoding round trip with the
// expected key/type. Catches regressions where Field type handling drifts.
func TestLogger_JSONStructuredFields_AllTypes(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "debug", "json")

	log.Info("typed-fields",
		String("s", "hello"),
		Int("i", -7),
		Int64("i64", 1<<40),
		Float64("f", 1.5),
		Bool("b", true),
		Strings("xs", []string{"a", "b"}),
		Money("price", "12.50", "LAC"),
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))

	require.Equal(t, "hello", entry["s"])
	require.EqualValues(t, -7, entry["i"])
	require.EqualValues(t, 1<<40, entry["i64"])
	require.InDelta(t, 1.5, entry["f"], 1e-9)
	require.Equal(t, true, entry["b"])
	require.Equal(t, []any{"a", "b"}, entry["xs"])

	price, ok := entry["price"].(map[string]any)
	require.True(t, ok, "price should serialize as object, got %T", entry["price"])
	require.Equal(t, "12.50", price["amount"])
	require.Equal(t, "LAC", price["currency"])

	require.Equal(t, "info", entry["level"])
	require.Equal(t, "typed-fields", entry["message"])
	require.NotEmpty(t, entry["timestamp"])
}

// TestLogger_With_NestedFieldsAndOverride verifies child loggers inherit
// parent fields, that nested With calls accumulate, and that the most-recent
// With value wins for overlapping keys.
func TestLogger_With_NestedFieldsAndOverride(t *testing.T) {
	var buf bytes.Buffer
	root := NewWithWriter(&buf, "info", "json")

	child := root.
		With(String("service", "router")).
		With(String("region", "us-west-2")).
		With(String("region", "eu-central-1")) // override

	child.Info("hello")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	require.Equal(t, "router", entry["service"])
	require.Equal(t, "eu-central-1", entry["region"])
}

// TestLogger_WithComponent_DoesNotLeakIntoParent verifies that creating a
// component-scoped child logger does not mutate the receiver.
func TestLogger_WithComponent_DoesNotLeakIntoParent(t *testing.T) {
	var buf bytes.Buffer
	root := NewWithWriter(&buf, "info", "json")
	root.WithComponent("settlement").Info("from-child")
	root.Info("from-root")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.Len(t, lines, 2)

	var childEntry, rootEntry map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &childEntry))
	require.NoError(t, json.Unmarshal(lines[1], &rootEntry))
	require.Equal(t, "settlement", childEntry["component"])
	_, hasComponent := rootEntry["component"]
	require.False(t, hasComponent, "parent logger must not gain component from child")
}

// TestLogger_WithTrace_BindsTraceAndSpan confirms WithTrace produces the
// expected trace_id / span_id / correlation_id triplet in JSON output.
func TestLogger_WithTrace_BindsTraceAndSpan(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info", "json").
		WithTrace("trace-abc", "span-xyz")
	log.Info("traced")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	require.Equal(t, "trace-abc", entry["trace_id"])
	require.Equal(t, "trace-abc", entry["correlation_id"])
	require.Equal(t, "span-xyz", entry["span_id"])
}

// TestLogger_NilFieldsExcludedFromJSON verifies that fields with nil values
// are dropped (matches mergeFields/redactFieldValue policy).
func TestLogger_NilFieldsExcludedFromJSON(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info", "json")

	log.Info("nil-field-test",
		String("present", "yes"),
		Field{Key: "absent", Value: nil},
		Error(nil),
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	require.Equal(t, "yes", entry["present"])
	_, hasAbsent := entry["absent"]
	require.False(t, hasAbsent)
	_, hasError := entry["error"]
	require.False(t, hasError, "Error(nil) should not emit an error key")
}

// TestLogger_RedactsBearerTokenInFieldValue ensures string values stored in
// Field also get PII-redacted, not just the message.
func TestLogger_RedactsBearerTokenInFieldValue(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info", "json")

	log.Info("auth-attempt",
		String("authorization", "Bearer eyJsupersecret.token.here"),
		String("email", "user@example.com"),
	)

	out := buf.String()
	require.NotContains(t, out, "supersecret")
	require.NotContains(t, out, "user@example.com")
	require.Contains(t, out, "REDACTED")
}

// TestMergeFields_PreservesNonOverriddenOrder ensures base fields not
// overridden by the additional set keep their relative order before the
// appended additionals.
func TestMergeFields_PreservesNonOverriddenOrder(t *testing.T) {
	base := []Field{
		String("a", "1"),
		String("b", "2"),
		String("c", "3"),
	}
	add := []Field{String("b", "override")}

	got := mergeFields(base, add)
	require.Len(t, got, 3)
	require.Equal(t, "a", got[0].Key)
	require.Equal(t, "1", got[0].Value)
	require.Equal(t, "c", got[1].Key)
	require.Equal(t, "3", got[1].Value)
	require.Equal(t, "b", got[2].Key)
	require.Equal(t, "override", got[2].Value)
}

// TestLogger_ConcurrentChildLoggersIndependent fans out N goroutines, each
// using its own With-derived child logger, and asserts that the per-goroutine
// fields never bleed across entries.
func TestLogger_ConcurrentChildLoggersIndependent(t *testing.T) {
	var buf bytes.Buffer
	var bufMu sync.Mutex
	syncWriter := &lockedWriter{w: &buf, mu: &bufMu}
	root := NewWithWriter(syncWriter, "info", "json")

	const goroutines = 16
	const perGoroutine = 25
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			child := root.With(Int("worker", id))
			for i := 0; i < perGoroutine; i++ {
				child.Info("tick", Int("seq", i))
			}
		}(g)
	}
	wg.Wait()

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.Len(t, lines, goroutines*perGoroutine)
	for _, line := range lines {
		var entry map[string]any
		require.NoError(t, json.Unmarshal(line, &entry))
		// Every entry must have exactly one worker id and one seq.
		_, hasWorker := entry["worker"]
		_, hasSeq := entry["seq"]
		require.True(t, hasWorker)
		require.True(t, hasSeq)
		require.Equal(t, "tick", entry["message"])
	}
}

// TestRedactPII_MultiplePatternsInOneString ensures the redaction pipeline
// handles overlapping/sequential patterns without losing matches.
func TestRedactPII_MultiplePatternsInOneString(t *testing.T) {
	in := "request from user@corp.com via 10.0.0.1 with Bearer xyzabc and OPENAI_API_KEY=sk-abcdef0123"
	out := RedactPII(in)
	for _, leaked := range []string{"user@corp.com", "10.0.0.1", "xyzabc", "sk-abcdef0123"} {
		require.NotContains(t, out, leaked, "leaked %q in %q", leaked, out)
	}
	require.Contains(t, out, "REDACTED")
}

// TestParseLevel_UnknownDefaultsToInfo locks in the documented default-level
// behavior so future refactors don't silently change it.
func TestParseLevel_UnknownDefaultsToInfo(t *testing.T) {
	require.Equal(t, LevelInfo, parseLevel(""))
	require.Equal(t, LevelInfo, parseLevel("not-a-real-level"))
	require.Equal(t, LevelDebug, parseLevel("DEBUG"))
	require.Equal(t, LevelWarn, parseLevel(" warn "))
}

// TestSetFormat_RoundTripJSONColor verifies SetFormat actually toggles the
// output mode at runtime.
func TestSetFormat_RoundTripJSONColor(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info", "")
	log.Info("first")
	require.NoError(t, log.SetFormat("json"))
	log.Info("second")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	require.Contains(t, lines[0], "[INFO]", "first line should still be color-formatted")
	require.True(t, strings.HasPrefix(lines[1], "{"), "second line should be JSON, got %q", lines[1])
}

// lockedWriter wraps an io.Writer with an external mutex so concurrent
// goroutines writing through Logger can share a buffer safely in tests.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// ---------------------------------------------------------------------------
// Nil logger and level threshold edge cases (66.7% -> 100%)
// ---------------------------------------------------------------------------

func TestLogger_Debug_NilLogger(t *testing.T) {
	var log *Logger = nil
	// Should not panic on nil receiver
	require.NotPanics(t, func() {
		log.Debug("this should be a no-op")
	})
}

func TestLogger_Debug_LevelThresholdSkipsDebug(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "info", "json") // info level skips debug
	log.Debug("this should not appear")
	require.Empty(t, buf.String(), "debug message should be skipped at info level")
}

func TestLogger_DebugCtx_NilLogger(t *testing.T) {
	var log *Logger = nil
	require.NotPanics(t, func() {
		log.DebugCtx(context.Background(), "no-op debug ctx")
	})
}

func TestLogger_DebugCtx_LevelThresholdSkipsDebug(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "warn", "json") // warn level skips debug
	log.DebugCtx(context.Background(), "should not appear")
	require.Empty(t, buf.String())
}

func TestLogger_InfoCtx_NilLogger(t *testing.T) {
	var log *Logger = nil
	require.NotPanics(t, func() {
		log.InfoCtx(context.Background(), "no-op info ctx")
	})
}

func TestLogger_InfoCtx_LevelThresholdSkipsInfo(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "warn", "json") // warn level skips info
	log.InfoCtx(context.Background(), "should not appear")
	require.Empty(t, buf.String())
}

func TestLogger_WarnCtx_NilLogger(t *testing.T) {
	var log *Logger = nil
	require.NotPanics(t, func() {
		log.WarnCtx(context.Background(), "no-op warn ctx")
	})
}

func TestLogger_WarnCtx_LevelThresholdSkipsWarn(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "error", "json") // error level skips warn
	log.WarnCtx(context.Background(), "should not appear")
	require.Empty(t, buf.String())
}

func TestLogger_ErrorCtx_NilLogger(t *testing.T) {
	var log *Logger = nil
	require.NotPanics(t, func() {
		log.ErrorCtx(context.Background(), "no-op error ctx")
	})
}

func TestLogger_Error_NilLogger(t *testing.T) {
	var log *Logger = nil
	require.NotPanics(t, func() {
		log.Error("no-op error")
	})
}

func TestLogger_Warn_NilLogger(t *testing.T) {
	var log *Logger = nil
	require.NotPanics(t, func() {
		log.Warn("no-op warn")
	})
}

func TestLogger_Info_NilLogger(t *testing.T) {
	var log *Logger = nil
	require.NotPanics(t, func() {
		log.Info("no-op info")
	})
}

func TestLogger_Info_LevelThreshold(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "error", "json") // error level skips info
	log.Info("should not appear")
	require.Empty(t, buf.String())
}

func TestLogger_Warn_LevelThreshold(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter(&buf, "error", "json") // error level skips warn
	log.Warn("should not appear")
	require.Empty(t, buf.String())
}
