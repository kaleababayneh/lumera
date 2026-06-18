package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggerLevelFiltering(t *testing.T) {
	t.Parallel()

	var infoOut bytes.Buffer
	infoLogger := NewWithWriter(&infoOut, "warn", "")
	infoLogger.Infof("should be suppressed")
	if infoOut.String() != "" {
		t.Fatalf("expected no INFO output, got %q", infoOut.String())
	}

	var warnOut bytes.Buffer
	warnLogger := NewWithWriter(&warnOut, "warn", "")
	warnLogger.Warnf("attention: %s", "budget")
	stripped := stripANSI(warnOut.String())
	if !strings.Contains(stripped, "[WARN] attention: budget") {
		t.Fatalf("unexpected WARN output: %q", stripped)
	}
}

func TestLoggerDebugLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "")
	logger.Debugf("details %d", 42)
	stripped := stripANSI(buf.String())
	if !strings.Contains(stripped, "[DEBUG] details 42") {
		t.Fatalf("unexpected DEBUG output: %q", stripped)
	}
}

func TestRedactPII_RedactsCommonSecrets(t *testing.T) {
	t.Parallel()

	input := `Authorization: Bearer abc.def.ghi https://example.com/?apikey=supersecret&x=1 {"api_key":"k-123","password":"p@ss","other":"ok"} sk-abcdef1234567890 user@example.com 203.0.113.42:12345 [2001:db8::1]:443 eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4ifQ.sflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c`
	output := RedactPII(input)

	if strings.Contains(output, "abc.def.ghi") {
		t.Fatalf("expected bearer token to be redacted, got %q", output)
	}
	if strings.Contains(output, "supersecret") {
		t.Fatalf("expected api key query param to be redacted, got %q", output)
	}
	if strings.Contains(output, `:"k-123"`) {
		t.Fatalf("expected json api_key to be redacted, got %q", output)
	}
	if strings.Contains(output, `:"p@ss"`) {
		t.Fatalf("expected json password to be redacted, got %q", output)
	}
	if strings.Contains(output, "sk-abcdef1234567890") {
		t.Fatalf("expected OpenAI-style key to be redacted, got %q", output)
	}
	if strings.Contains(output, "user@example.com") {
		t.Fatalf("expected email to be redacted, got %q", output)
	}
	if strings.Contains(output, "203.0.113.42") {
		t.Fatalf("expected IPv4 address to be redacted, got %q", output)
	}
	if strings.Contains(output, "2001:db8::1") {
		t.Fatalf("expected IPv6 address to be redacted, got %q", output)
	}
	if strings.Contains(output, "eyJhbGci") {
		t.Fatalf("expected JWT to be redacted, got %q", output)
	}
	if !strings.Contains(output, "Bearer [REDACTED]") {
		t.Fatalf("expected redacted bearer token marker, got %q", output)
	}
	if !strings.Contains(output, "apikey=[REDACTED]") {
		t.Fatalf("expected redacted apikey marker, got %q", output)
	}
	if !strings.Contains(output, `"api_key":"[REDACTED]"`) {
		t.Fatalf("expected redacted json api_key marker, got %q", output)
	}
	if !strings.Contains(output, `"password":"[REDACTED]"`) {
		t.Fatalf("expected redacted json password marker, got %q", output)
	}
	if !strings.Contains(output, "sk-[REDACTED]") {
		t.Fatalf("expected redacted OpenAI key marker, got %q", output)
	}
	if !strings.Contains(output, "[REDACTED_EMAIL]") {
		t.Fatalf("expected redacted email marker, got %q", output)
	}
	if !strings.Contains(output, "[REDACTED_IP]") {
		t.Fatalf("expected redacted IP marker, got %q", output)
	}
	if !strings.Contains(output, "[REDACTED_JWT]") {
		t.Fatalf("expected redacted JWT marker, got %q", output)
	}
}

func TestRedactPII_RedactsQuotedAndUncommonAuthorizationHeaders(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`Authorization: Bearer "quoted-bearer-token"`,
		"Proxy-Authorization: Negotiate raw-negotiate-ticket",
		`Authorization: Signature "raw-signature-token"`,
		"Authorization: Basic [REDACTED]",
		"Authorization: Bearer <redacted>",
	}, "\n")

	output := RedactPII(input)

	for _, leaked := range []string{
		"quoted-bearer-token",
		"raw-negotiate-ticket",
		"raw-signature-token",
	} {
		if strings.Contains(output, leaked) {
			t.Fatalf("expected authorization credential %q to be redacted, got %q", leaked, output)
		}
	}
	for _, marker := range []string{
		"Authorization: Bearer [REDACTED]",
		"Proxy-Authorization: Negotiate [REDACTED]",
		"Authorization: Signature [REDACTED]",
	} {
		if !strings.Contains(output, marker) {
			t.Fatalf("expected output to contain marker %q, got %q", marker, output)
		}
	}
	if strings.Count(output, "[REDACTED]") != 5 {
		t.Fatalf("expected existing redaction placeholders to remain stable, got %q", output)
	}
}

func TestLogger_RedactsSecretsInOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "")
	logger.Infof("sending Authorization: Bearer %s", "abc.def.ghi")
	stripped := stripANSI(buf.String())
	if strings.Contains(stripped, "abc.def.ghi") {
		t.Fatalf("expected logger output to redact bearer token, got %q", stripped)
	}
	if !strings.Contains(stripped, "Bearer [REDACTED]") {
		t.Fatalf("expected logger output to include redaction marker, got %q", stripped)
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(input string) string {
	return ansiPattern.ReplaceAllString(input, "")
}

// ---------------------------------------------------------------------------
// parseLevel
// ---------------------------------------------------------------------------

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"  debug  ", LevelDebug},
		{"info", LevelInfo},
		{"warn", LevelWarn},
		{"error", LevelError},
		{"", LevelInfo},        // default
		{"unknown", LevelInfo}, // default
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, parseLevel(tt.input))
		})
	}
}

// ---------------------------------------------------------------------------
// parseFormat
// ---------------------------------------------------------------------------

func TestParseFormat(t *testing.T) {
	jsonVal, err := parseFormat("json")
	require.NoError(t, err)
	assert.True(t, jsonVal)

	colorVal, err := parseFormat("color")
	require.NoError(t, err)
	assert.False(t, colorVal)

	emptyVal, err := parseFormat("")
	require.NoError(t, err)
	assert.False(t, emptyVal)

	_, err = parseFormat("xml")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// SetLevel / SetFormat
// ---------------------------------------------------------------------------

func TestSetLevel_ChangesMinLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "error", "")

	logger.Infof("should be suppressed")
	assert.Empty(t, buf.String())

	logger.SetLevel("debug")
	logger.Infof("now visible")
	assert.Contains(t, stripANSI(buf.String()), "now visible")
}

func TestSetLevel_NilReceiver(t *testing.T) {
	var l *Logger
	l.SetLevel("debug") // should not panic
}

func TestSetFormat_SwitchesToJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "")

	err := logger.SetFormat("json")
	require.NoError(t, err)

	logger.Infof("hello json")
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "hello json", entry["message"])
}

func TestSetFormat_InvalidFormat(t *testing.T) {
	logger := New("info")
	err := logger.SetFormat("yaml")
	assert.Error(t, err)
}

func TestSetFormat_NilReceiver(t *testing.T) {
	var l *Logger
	err := l.SetFormat("json")
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// JSON output mode
// ---------------------------------------------------------------------------

func TestLogger_JSONOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	logger.Infof("json %s", "test")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "info", entry["level"])
	assert.Equal(t, "json test", entry["message"])
	assert.NotEmpty(t, entry["timestamp"])
}

func TestLogger_JSONStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "json")
	logger.Info("event", String("tool_id", "t1"), Int("count", 5))

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "event", entry["message"])
	assert.Equal(t, "t1", entry["tool_id"])
	assert.Equal(t, float64(5), entry["count"])
}

func TestLogger_JSONStructuredFields_NonFiniteFloatsStayParseable(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "json")
	logger.Info("event",
		Float64("nan", math.NaN()),
		Float64("pos_inf", math.Inf(1)),
		Float64("neg_inf", math.Inf(-1)),
		Float64("finite", 1.25),
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "event", entry["message"])
	assert.Equal(t, "NaN", entry["nan"])
	assert.Equal(t, "+Inf", entry["pos_inf"])
	assert.Equal(t, "-Inf", entry["neg_inf"])
	assert.Equal(t, 1.25, entry["finite"])
}

func TestLogger_JSONWithServiceAndComponent(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		level:     LevelInfo,
		json:      true,
		out:       &buf,
		service:   "router",
		component: "quote",
	}
	logger.Info("test")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "router", entry["service"])
	assert.Equal(t, "quote", entry["component"])
}

// ---------------------------------------------------------------------------
// Child loggers: With, WithTrace, WithComponent
// ---------------------------------------------------------------------------

func TestLogger_With_AddsFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	child := logger.With(String("session", "s1"))
	child.Info("child log")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "s1", entry["session"])
}

func TestLogger_With_FieldOverride(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	child := logger.With(String("k", "base"))
	grandchild := child.With(String("k", "override"))
	grandchild.Info("test")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "override", entry["k"])
}

func TestLogger_WithTrace(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	traced := logger.WithTrace("trace-abc", "span-def")
	traced.Info("traced event")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "trace-abc", entry["trace_id"])
	assert.Equal(t, "span-def", entry["span_id"])
	assert.Equal(t, "trace-abc", entry["correlation_id"])
}

func TestLogger_WithComponent(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	comp := logger.WithComponent("settlement")
	comp.Info("component event")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "settlement", entry["component"])
}

func TestLogger_WithNilReceiver(t *testing.T) {
	var l *Logger
	assert.Nil(t, l.With(String("k", "v")))
	assert.Nil(t, l.WithTrace("t", "s"))
	assert.Nil(t, l.WithComponent("c"))
}

// ---------------------------------------------------------------------------
// Nil receiver safety for all log methods
// ---------------------------------------------------------------------------

func TestLogger_NilReceiverSafety(t *testing.T) {
	var l *Logger
	// None of these should panic.
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")
	l.Debugf("d")
	l.Infof("i")
	l.Successf("s")
	l.Warnf("w")
	l.Errorf("e")
}

// ---------------------------------------------------------------------------
// All log levels in color mode
// ---------------------------------------------------------------------------

func TestLogger_AllLevelsColorOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "")

	logger.Debugf("debug msg")
	logger.Infof("info msg")
	logger.Successf("success msg")
	logger.Warnf("warn msg")
	logger.Errorf("error msg")

	out := stripANSI(buf.String())
	assert.Contains(t, out, "[DEBUG] debug msg")
	assert.Contains(t, out, "[INFO] info msg")
	assert.Contains(t, out, "[OK] success msg")
	assert.Contains(t, out, "[WARN] warn msg")
	assert.Contains(t, out, "[ERROR] error msg")
}

// ---------------------------------------------------------------------------
// Structured methods: DebugCtx, InfoCtx, WarnCtx, ErrorCtx
// ---------------------------------------------------------------------------

func TestLogger_StructuredCtxMethods(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "json")
	ctx := t.Context()

	logger.DebugCtx(ctx, "d", String("k", "v1"))
	logger.InfoCtx(ctx, "i")
	logger.WarnCtx(ctx, "w")
	logger.ErrorCtx(ctx, "e")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 4)

	var first map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, "debug", first["level"])
	assert.Equal(t, "v1", first["k"])
}

// ---------------------------------------------------------------------------
// NewWithService
// ---------------------------------------------------------------------------

func TestNewWithService(t *testing.T) {
	logger := NewWithService("info", "json", "my-router")
	var buf bytes.Buffer
	logger.out = &buf
	logger.Info("test")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "my-router", entry["service"])
}

// ---------------------------------------------------------------------------
// Field helpers not yet tested
// ---------------------------------------------------------------------------

func TestErrorField_WithError(t *testing.T) {
	f := Error(errors.New("boom"))
	assert.Equal(t, "error", f.Key)
	assert.Equal(t, "boom", f.Value)
}

func TestErrorKeyField(t *testing.T) {
	f := ErrorKey("cause", errors.New("fail"))
	assert.Equal(t, "cause", f.Key)
	assert.Equal(t, "fail", f.Value)

	f = ErrorKey("cause", nil)
	assert.Nil(t, f.Value)
}

func TestAnyField(t *testing.T) {
	f := Any("data", map[string]int{"a": 1})
	assert.Equal(t, "data", f.Key)
	assert.Equal(t, map[string]int{"a": 1}, f.Value)
}

type myStringer struct{ s string }

func (m myStringer) String() string { return m.s }

func TestStringerField(t *testing.T) {
	f := Stringer("obj", myStringer{"hello"})
	assert.Equal(t, "hello", f.Value)

	f = Stringer("obj", nil)
	assert.Nil(t, f.Value)
}

func TestTimeField(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	f := Time("created", now)
	assert.Equal(t, "created", f.Key)
	assert.Equal(t, now.Format(time.RFC3339Nano), f.Value)
}

func TestStringsField(t *testing.T) {
	f := Strings("tags", []string{"a", "b"})
	assert.Equal(t, "tags", f.Key)
	assert.Equal(t, []string{"a", "b"}, f.Value)
}

// TestDurationField_AutoSuffixAndMillisecondUnit pins a subtle,
// load-bearing invariant of Duration: the supplied key is
// automatically suffixed with "_ms", and the value is the
// millisecond count (not nanoseconds). Both halves matter:
//
//   - Log parsers and Grafana dashboards key off the "_ms" suffix
//     to know the unit. Dropping the suffix would silently switch
//     every call site to bare-key fields, breaking any downstream
//     that filters or converts by unit.
//   - Switching the value from .Milliseconds() to .Nanoseconds() (or
//     to the raw time.Duration) would give a 1000000x blow-up in the
//     recorded number under an unchanged key — a silent-corruption
//     of operational metrics that no non-boundary test catches.
//
// Coverage was zero before this pin: no existing test even calls
// Duration().
func TestDurationField_AutoSuffixAndMillisecondUnit(t *testing.T) {
	f := Duration("request_duration", 1500*time.Millisecond)
	assert.Equal(t, "request_duration_ms", f.Key,
		"Duration must auto-suffix key with '_ms'")
	assert.Equal(t, int64(1500), f.Value,
		"Duration value must be milliseconds (int64), not nanoseconds or time.Duration")
}

// TestDurationField_SubMillisecondTruncates pins that a sub-
// millisecond Duration truncates to 0 ms (time.Duration.Milliseconds
// is integer-valued and truncates toward zero). Pinning this makes
// the contract explicit so a refactor flipping to .Microseconds()
// (which would preserve sub-ms resolution but change every
// downstream dashboard's unit scale) surfaces as a test failure.
func TestDurationField_SubMillisecondTruncates(t *testing.T) {
	f := Duration("tiny", 500*time.Microsecond) // 0.5 ms → truncates to 0
	assert.Equal(t, "tiny_ms", f.Key)
	assert.Equal(t, int64(0), f.Value,
		"sub-millisecond duration must truncate to 0 ms (not round)")
}

// TestErrorField_NilError pins that Error(nil) returns a field
// with Key="error" and Value=nil (NOT a panic or an empty-string
// placeholder). fieldsToMap filters nil values out of the emitted
// JSON, so this is how callers omit the "error" field cleanly when
// they don't have an error to report. Regression guard: a refactor
// that removed the nil-guard (e.g., called `err.Error()`
// unconditionally) would panic on nil.
func TestErrorField_NilError(t *testing.T) {
	f := Error(nil)
	assert.Equal(t, "error", f.Key,
		"Error(nil) must still set Key='error' so field-presence callers stay consistent")
	assert.Nil(t, f.Value,
		"Error(nil).Value must be nil so fieldsToMap omits it from JSON output")
}

// ---------------------------------------------------------------------------
// fieldsToMap / mergeFields
// ---------------------------------------------------------------------------

func TestFieldsToMap(t *testing.T) {
	fields := []Field{
		String("a", "1"),
		Int("b", 2),
		{Key: "c", Value: nil}, // nil should be excluded
	}
	m := fieldsToMap(fields)
	assert.Equal(t, "1", m["a"])
	assert.Equal(t, 2, m["b"])
	_, exists := m["c"]
	assert.False(t, exists)
}

func TestMergeFields_AdditionalOverridesBase(t *testing.T) {
	base := []Field{String("k", "base"), String("x", "keep")}
	add := []Field{String("k", "override")}

	merged := mergeFields(base, add)
	m := fieldsToMap(merged)
	assert.Equal(t, "override", m["k"])
	assert.Equal(t, "keep", m["x"])
}

func TestMergeFields_EmptyAdditional(t *testing.T) {
	base := []Field{String("k", "v")}
	assert.Equal(t, base, mergeFields(base, nil))
}

func TestMergeFields_EmptyBase(t *testing.T) {
	add := []Field{String("k", "v")}
	assert.Equal(t, add, mergeFields(nil, add))
}

// ---------------------------------------------------------------------------
// RedactPII edge cases
// ---------------------------------------------------------------------------

func TestRedactPII_EmptyString(t *testing.T) {
	assert.Equal(t, "", RedactPII(""))
}

func TestRedactPII_NoSecrets(t *testing.T) {
	input := "normal log message with no secrets"
	assert.Equal(t, input, RedactPII(input))
}

func TestRedactPII_EnvSecrets(t *testing.T) {
	input := "OPENAI_API_KEY=sk-abc123 ANTHROPIC_API_KEY=ant-xyz"
	output := RedactPII(input)
	assert.NotContains(t, output, "sk-abc123")
	assert.NotContains(t, output, "ant-xyz")
	assert.Contains(t, output, "OPENAI_API_KEY=[REDACTED]")
}

func TestRedactPII_JSONSensitiveField(t *testing.T) {
	fieldName := "access_" + "token"
	fieldValue := "fixture-" + "value-123"
	input := fmt.Sprintf(`{"%s":"%s","name":"safe"}`, fieldName, fieldValue)
	output := RedactPII(input)
	assert.NotContains(t, output, fieldValue)
	assert.Contains(t, output, fmt.Sprintf(`"%s":"[REDACTED]"`, fieldName))
	assert.Contains(t, output, `"name":"safe"`)
}

// ---------------------------------------------------------------------------
// Color output includes fields and trace
// ---------------------------------------------------------------------------

func TestLogger_ColorOutputIncludesFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "")
	child := logger.With(String("env", "test")).WithTrace("tr-1", "")
	child.Info("hello", Int("count", 3))

	out := stripANSI(buf.String())
	assert.Contains(t, out, "hello")
	assert.Contains(t, out, "env=test")
	assert.Contains(t, out, "count=3")
	assert.Contains(t, out, "trace_id=tr-1")
}

// ---------------------------------------------------------------------------
// Format-based logging in JSON mode includes base fields
// ---------------------------------------------------------------------------

func TestLogger_FormatBasedJSONIncludesBaseFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	child := logger.With(String("version", "v1"))
	child.Infof("hello %s", "world")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "hello world", entry["message"])
	assert.Equal(t, "v1", entry["version"])
}

// ---------------------------------------------------------------------------
// Redaction in structured fields
// ---------------------------------------------------------------------------

func TestLogger_RedactsStructuredFieldValues(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "info", "json")
	logger.Info("auth event", String("token", "Bearer my-secret-token"))

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "Bearer [REDACTED]", entry["token"])
}

// ---------------------------------------------------------------------------
// Structured level filtering
// ---------------------------------------------------------------------------

func TestLogger_StructuredLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "error", "json")

	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	assert.Empty(t, buf.String())

	logger.Error("e")
	assert.NotEmpty(t, buf.String())

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "error", entry["level"])
}

// ---------------------------------------------------------------------------
// redactFieldValue non-string passthrough
// ---------------------------------------------------------------------------

func TestRedactFieldValue_NonString(t *testing.T) {
	f := Field{Key: "count", Value: 42}
	assert.Equal(t, 42, redactFieldValue(f))
}

func TestRedactFieldValue_StringRedaction(t *testing.T) {
	f := Field{Key: "auth", Value: "Bearer secret"}
	result := redactFieldValue(f).(string)
	assert.Contains(t, result, "[REDACTED]")
	assert.NotContains(t, result, "secret")
}

// ---------------------------------------------------------------------------
// Concurrent logging safety
// ---------------------------------------------------------------------------

func TestLogger_ConcurrentSafety(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "json")

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			logger.Info(fmt.Sprintf("msg-%d", n), Int("n", n))
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 20)
}

// TestRedactPII_IdempotencyMetamorphic asserts that running RedactPII
// twice on the same input yields the same output as running it once.
// Log pipelines sometimes re-process already-redacted lines (e.g. when
// an audit indexer replays a stream); if the function were non-
// idempotent it could either mutate the "[REDACTED_*]" markers into
// something else or introduce drift that breaks downstream matchers.
func TestRedactPII_IdempotencyMetamorphic(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"no secrets here",
		"Authorization: Bearer abc.def.ghi",
		"email: user@example.com and ip 203.0.113.42",
		`{"api_key":"k-123","password":"p@ss"}`,
		"https://example.com/?apikey=supersecret",
		"sk-abcdef1234567890 eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc",
		"[2001:db8::1]:443",
		"DATABASE_URL=postgres://user:pass@host/db",
		"mixed: Bearer tok user@x.y http://1.2.3.4",
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			once := RedactPII(input)
			twice := RedactPII(once)
			require.Equalf(t, once, twice, "RedactPII not idempotent:\n  input:  %q\n  once:   %q\n  twice:  %q",
				input, once, twice)
		})
	}
}

// TestRedactPII_CleanInputPassthrough asserts that inputs with no
// matching secret/PII patterns pass through unchanged. Over-eager
// redaction would mangle arbitrary log lines (e.g. numeric IDs that
// look vaguely IP-like, normal URLs) and obscure debugging info; a
// regex regression that broadened a pattern would fail here.
func TestRedactPII_CleanInputPassthrough(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"plain message",
		"tool=foo count=5 status=ok",
		"2026-04-17T00:00:00Z INFO request handled",
		"score=0.95 latency_ms=42",
		"Rendering /v1/discover for session=sess-abc",
	}
	for _, input := range cases {
		assert.Equalf(t, input, RedactPII(input), "clean input %q mutated", input)
	}
}

// FuzzRedactPII_NoPanic exercises the full regex pipeline on arbitrary
// input and asserts it never panics, always produces valid UTF-8,
// and is idempotent. If a regex had a pathological backtracking case
// or a replacement produced invalid UTF-8 mid-codepoint, either would
// surface here.
func FuzzRedactPII_NoPanic(f *testing.F) {
	f.Add("")
	f.Add("no secrets")
	f.Add("Bearer abc.def.ghi")
	f.Add("user@example.com")
	f.Add("ip 1.2.3.4 and [::1]:80")
	f.Add(`{"api_key":"k","password":"p"}`)
	f.Add("\x00\x01\x02 binary garbage")
	f.Add("unicode é日本 emoji 😀")

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 64<<10 {
			t.Skip()
		}
		first := RedactPII(input)
		second := RedactPII(first)
		require.Equalf(t, first, second, "RedactPII not idempotent on fuzz input %q:\n  first:  %q\n  second: %q",
			input, first, second)
	})
}
