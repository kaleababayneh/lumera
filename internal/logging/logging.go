// Package logging provides colorized, structured logging utilities for Lumera services.
//
// The package supports two output modes:
//   - Color mode: Human-readable colorized output for development
//   - JSON mode: Machine-parseable structured JSON for production
//
// Usage with structured fields:
//
//	log := logging.New("info")
//	log.Info("request completed",
//	    logging.String("method", "POST"),
//	    logging.Int("status", 200),
//	    logging.Duration("latency", elapsed),
//	)
//
// Usage with child loggers:
//
//	reqLog := log.WithTrace(traceID, spanID).With(
//	    logging.String("session_id", sessID),
//	)
//	reqLog.Info("quote generated", logging.String("cost", "0.05"))
//
// Context integration:
//
//	ctx = logging.WithLogger(ctx, log)
//	logging.FromContext(ctx).Info("using logger from context")
package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"go.opentelemetry.io/otel/trace"
)

var (
	bearerTokenPattern = regexp.MustCompile(`(?i)\bbearer\s+[^\s]+`)
	authHeaderPattern  = regexp.MustCompile(`(?i)\b((?:proxy[-_ ]?)?authorization\s*:\s*)([a-z][a-z0-9._~+/-]*)(?:\s+(?:"[^"\r\n]*"|[^\s,\r\n]+))?`)
	jwtPattern         = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)
	apiKeyQueryPattern = regexp.MustCompile(`(?i)(^|[?&;\s])((?:api(?:_|-|%5f|%2d)?key|access(?:_|-|%5f|%2d)token|refresh(?:_|-|%5f|%2d)token|auth(?:_|-|%5f|%2d)token|bearer(?:_|-|%5f|%2d)token|credential|password|passwd|client(?:_|-|%5f|%2d)secret|client(?:_|-|%5f|%2d)assertion|id(?:_|-|%5f|%2d)token|secret|session(?:_|-|%5f|%2d)token|signature|sig|token|authorization|cookie)=)[^\s&;#"'<>\[]+`)
	envSecretPattern   = regexp.MustCompile(`(?i)\b(OPENAI_API_KEY|ANTHROPIC_API_KEY|GROK_API_KEY|GEMINI_API_KEY)=[^\s]+`)
	openAIKeyPattern   = regexp.MustCompile(`\bsk-[A-Za-z0-9]{8,}\b`)
	jsonSecretPattern  = regexp.MustCompile(`(?i)("(?:(?:api[-_]?key)|(?:access[-_]token)|(?:refresh[-_]token)|(?:auth[-_]token)|(?:bearer[-_]token)|(?:credential)|(?:password)|(?:passwd)|(?:client[-_]secret)|(?:client[-_]assertion)|(?:id[-_]token)|(?:secret)|(?:session[-_]token)|(?:signature)|(?:sig)|(?:token)|(?:authorization)|(?:cookie))"\s*:\s*)"([^"]*)"`)
	emailPattern       = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	ipv4Pattern        = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d{1,5})?\b`)
	ipv6BracketPattern = regexp.MustCompile(`\[[0-9a-fA-F:]+\](?::\d{1,5})?\b`)
)

// Level represents structured logging levels supported by the router.
type Level int

const (
	// LevelDebug represents verbose developer-oriented logging.
	LevelDebug Level = iota
	// LevelInfo captures standard informational output.
	LevelInfo
	// LevelWarn highlights recoverable anomalies.
	LevelWarn
	// LevelError indicates critical failures.
	LevelError
)

// Logger provides colorized, leveled logging aligned with Lumera's operator UX goals.
// It implements StructuredLogger and supports both format-based (Infof) and
// field-based (Info) logging methods.
type Logger struct {
	level      Level
	json       bool
	mu         sync.Mutex
	out        io.Writer
	service    string
	component  string
	baseFields []Field
	traceID    string
	spanID     string
}

// New returns a colorized logger honoring the configured minimum level.
func New(level string) *Logger {
	return NewWithFormat(level, "")
}

// NewWithFormat returns a logger honoring the configured level and format.
func NewWithFormat(level, format string) *Logger {
	return &Logger{
		level: parseLevel(level),
		json:  strings.EqualFold(format, "json"),
		// out is left nil to use os.Stdout at write time, enabling test captures
	}
}

// NewWithService returns a logger with service metadata.
func NewWithService(level, format, service string) *Logger {
	return &Logger{
		level:   parseLevel(level),
		json:    strings.EqualFold(format, "json"),
		service: service,
	}
}

// NewWithWriter returns a logger that writes to the specified writer.
// Useful for testing or redirecting output.
func NewWithWriter(w io.Writer, level, format string) *Logger {
	return &Logger{
		level: parseLevel(level),
		json:  strings.EqualFold(format, "json"),
		out:   w,
	}
}

// SetLevel updates the logger's minimum level at runtime.
func (l *Logger) SetLevel(level string) {
	if l == nil {
		return
	}
	parsed := parseLevel(level)
	l.mu.Lock()
	l.level = parsed
	l.mu.Unlock()
}

// SetFormat updates the logger's output format at runtime.
func (l *Logger) SetFormat(format string) error {
	if l == nil {
		return nil
	}
	jsonFormat, err := parseFormat(format)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.json = jsonFormat
	l.mu.Unlock()
	return nil
}

func parseLevel(level string) Level {
	normalized := strings.ToLower(strings.TrimSpace(level))
	switch normalized {
	case "debug":
		return LevelDebug
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func parseFormat(format string) (bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(format))
	switch normalized {
	case "", "color":
		return false, nil
	case "json":
		return true, nil
	default:
		return false, fmt.Errorf("log format must be 'color' or 'json'")
	}
}

// With creates a child logger with additional fields attached to all subsequent logs.
func (l *Logger) With(fields ...Field) *Logger {
	if l == nil {
		return nil
	}
	child := &Logger{
		level:      l.level,
		json:       l.json,
		out:        l.out, // preserve custom writer if set
		service:    l.service,
		component:  l.component,
		baseFields: mergeFields(l.baseFields, fields),
		traceID:    l.traceID,
		spanID:     l.spanID,
	}
	return child
}

// WithTrace creates a child logger bound to a specific trace and span.
func (l *Logger) WithTrace(traceID, spanID string) *Logger {
	if l == nil {
		return nil
	}
	child := &Logger{
		level:      l.level,
		json:       l.json,
		out:        l.out,
		service:    l.service,
		component:  l.component,
		baseFields: l.baseFields,
		traceID:    traceID,
		spanID:     spanID,
	}
	return child
}

// WithComponent creates a child logger for a specific component.
func (l *Logger) WithComponent(component string) *Logger {
	if l == nil {
		return nil
	}
	child := &Logger{
		level:      l.level,
		json:       l.json,
		out:        l.out,
		service:    l.service,
		component:  component,
		baseFields: l.baseFields,
		traceID:    l.traceID,
		spanID:     l.spanID,
	}
	return child
}

// --- Structured logging methods (StructuredLogger interface) ---

// Debug logs a debug message with structured fields.
func (l *Logger) Debug(msg string, fields ...Field) {
	if l == nil || l.level > LevelDebug {
		return
	}
	l.logStructured(context.Background(), "debug", msg, fields)
}

// Info logs an info message with structured fields.
func (l *Logger) Info(msg string, fields ...Field) {
	if l == nil || l.level > LevelInfo {
		return
	}
	l.logStructured(context.Background(), "info", msg, fields)
}

// Warn logs a warning message with structured fields.
func (l *Logger) Warn(msg string, fields ...Field) {
	if l == nil || l.level > LevelWarn {
		return
	}
	l.logStructured(context.Background(), "warn", msg, fields)
}

// Error logs an error message with structured fields.
func (l *Logger) Error(msg string, fields ...Field) {
	if l == nil || l.level > LevelError {
		return
	}
	l.logStructured(context.Background(), "error", msg, fields)
}

// DebugCtx logs a debug message with context metadata and structured fields.
func (l *Logger) DebugCtx(ctx context.Context, msg string, fields ...Field) {
	if l == nil || l.level > LevelDebug {
		return
	}
	l.logStructured(ctx, "debug", msg, fields)
}

// InfoCtx logs an info message with context metadata and structured fields.
func (l *Logger) InfoCtx(ctx context.Context, msg string, fields ...Field) {
	if l == nil || l.level > LevelInfo {
		return
	}
	l.logStructured(ctx, "info", msg, fields)
}

// WarnCtx logs a warning message with context metadata and structured fields.
func (l *Logger) WarnCtx(ctx context.Context, msg string, fields ...Field) {
	if l == nil || l.level > LevelWarn {
		return
	}
	l.logStructured(ctx, "warn", msg, fields)
}

// ErrorCtx logs an error message with context metadata and structured fields.
func (l *Logger) ErrorCtx(ctx context.Context, msg string, fields ...Field) {
	if l == nil || l.level > LevelError {
		return
	}
	l.logStructured(ctx, "error", msg, fields)
}

// logStructured handles the actual structured log output.
func (l *Logger) logStructured(ctx context.Context, level, msg string, fields []Field) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Merge base fields with call-specific fields
	allFields := mergeFields(l.baseFields, fields)

	// Get trace info from context or logger
	traceID, spanID := l.traceID, l.spanID
	if ctxTraceID, ctxSpanID := traceDetails(ctx); ctxTraceID != "" {
		traceID, spanID = ctxTraceID, ctxSpanID
	}

	// Redact message
	msg = RedactPII(msg)

	out := l.out
	if out == nil {
		out = os.Stdout
	}

	if l.json {
		l.writeJSON(out, level, msg, allFields, traceID, spanID)
	} else {
		l.writeColor(out, level, msg, allFields, traceID)
	}
}

func (l *Logger) writeJSON(out io.Writer, level, msg string, fields []Field, traceID, spanID string) {
	entry := make(map[string]any, 8+len(fields))
	entry["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	entry["level"] = level
	entry["message"] = msg

	if l.service != "" {
		entry["service"] = l.service
	}
	if l.component != "" {
		entry["component"] = l.component
	}
	if traceID != "" {
		entry["trace_id"] = traceID
		entry["correlation_id"] = traceID
	}
	if spanID != "" {
		entry["span_id"] = spanID
	}

	// Add structured fields, redacting values as needed
	for _, f := range fields {
		if f.Value != nil {
			entry[f.Key] = redactFieldValue(f)
		}
	}

	enc, err := json.Marshal(entry)
	if err != nil {
		_, _ = fmt.Fprintf(out, "{\"timestamp\":%q,\"level\":\"error\",\"message\":%q}\n",
			time.Now().UTC().Format(time.RFC3339Nano), err.Error())
		return
	}
	_, _ = fmt.Fprintf(out, "%s\n", enc)
}

func (l *Logger) writeColor(out io.Writer, level, msg string, fields []Field, traceID string) {
	var c *color.Color
	var label string
	switch level {
	case "debug":
		c = color.New(color.FgMagenta)
		label = "DEBUG"
	case "info":
		c = color.New(color.FgCyan)
		label = "INFO"
	case "warn":
		c = color.New(color.FgYellow, color.Bold)
		label = "WARN"
	case "error":
		c = color.New(color.FgHiRed, color.Bold)
		label = "ERROR"
	default:
		c = color.New(color.FgWhite)
		label = strings.ToUpper(level)
	}

	prefix := c.Sprintf("[%s]", label)
	var sb strings.Builder
	sb.WriteString(msg)

	// Append trace_id if present
	if traceID != "" {
		sb.WriteString(" trace_id=")
		sb.WriteString(traceID)
	}

	// Append structured fields in key=value format for color mode
	for _, f := range fields {
		if f.Value != nil {
			sb.WriteString(" ")
			sb.WriteString(f.Key)
			sb.WriteString("=")
			fmt.Fprint(&sb, redactFieldValue(f))
		}
	}

	_, _ = fmt.Fprintf(out, "%s %s\n", prefix, sb.String())
}

// redactFieldValue applies redaction to field values that may contain sensitive data.
func redactFieldValue(f Field) any {
	switch v := f.Value.(type) {
	case string:
		return RedactPII(v)
	case float64:
		if label, ok := nonFiniteFloatString(v); ok {
			return label
		}
		return v
	case float32:
		if label, ok := nonFiniteFloatString(float64(v)); ok {
			return label
		}
		return v
	default:
		return v
	}
}

func nonFiniteFloatString(value float64) (string, bool) {
	switch {
	case math.IsNaN(value):
		return "NaN", true
	case math.IsInf(value, 1):
		return "+Inf", true
	case math.IsInf(value, -1):
		return "-Inf", true
	default:
		return "", false
	}
}

// --- Format-based logging methods (backward compatibility) ---

// Debugf prints verbose diagnostics when the logger is configured for debug output.
func (l *Logger) Debugf(format string, args ...any) {
	l.DebugContext(context.Background(), format, args...)
}

// Infof emits informative operational messages.
func (l *Logger) Infof(format string, args ...any) {
	l.InfoContext(context.Background(), format, args...)
}

// Successf highlights successful operations (e.g., service startup or healthy probes).
func (l *Logger) Successf(format string, args ...any) {
	l.SuccessContext(context.Background(), format, args...)
}

// Warnf flags recoverable anomalies that should be examined by operators.
func (l *Logger) Warnf(format string, args ...any) {
	l.WarnContext(context.Background(), format, args...)
}

// Errorf is reserved for unrecoverable conditions.
func (l *Logger) Errorf(format string, args ...any) {
	l.ErrorContext(context.Background(), format, args...)
}

// DebugContext prints verbose diagnostics associated with the provided context.
func (l *Logger) DebugContext(ctx context.Context, format string, args ...any) {
	if l == nil || l.level > LevelDebug {
		return
	}
	l.printFormat(ctx, color.New(color.FgMagenta), "DEBUG", format, args...)
}

// InfoContext logs high-level operational information with context metadata.
func (l *Logger) InfoContext(ctx context.Context, format string, args ...any) {
	if l == nil || l.level > LevelInfo {
		return
	}
	l.printFormat(ctx, color.New(color.FgCyan), "INFO", format, args...)
}

// SuccessContext celebrates successful operations using the vibrant OK palette.
func (l *Logger) SuccessContext(ctx context.Context, format string, args ...any) {
	if l == nil || l.level > LevelInfo {
		return
	}
	l.printFormat(ctx, color.New(color.FgGreen, color.Bold), "OK", format, args...)
}

// WarnContext reports recoverable issues that warrant operator attention.
func (l *Logger) WarnContext(ctx context.Context, format string, args ...any) {
	if l == nil || l.level > LevelWarn {
		return
	}
	l.printFormat(ctx, color.New(color.FgYellow, color.Bold), "WARN", format, args...)
}

// ErrorContext logs unrecoverable faults together with correlation metadata.
func (l *Logger) ErrorContext(ctx context.Context, format string, args ...any) {
	if l == nil || l.level > LevelError {
		return
	}
	l.printFormat(ctx, color.New(color.FgHiRed, color.Bold), "ERROR", format, args...)
}

func (l *Logger) printFormat(ctx context.Context, c *color.Color, label, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	msg := RedactPII(fmt.Sprintf(format, args...))
	traceID, spanID := l.traceID, l.spanID
	if ctxTraceID, ctxSpanID := traceDetails(ctx); ctxTraceID != "" {
		traceID, spanID = ctxTraceID, ctxSpanID
	}

	out := l.out
	if out == nil {
		out = os.Stdout
	}

	if l.json {
		entry := make(map[string]any, 8+len(l.baseFields))
		entry["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
		entry["level"] = strings.ToLower(label)
		entry["message"] = msg

		if l.service != "" {
			entry["service"] = l.service
		}
		if l.component != "" {
			entry["component"] = l.component
		}
		if traceID != "" {
			entry["trace_id"] = traceID
			entry["correlation_id"] = traceID
		}
		if spanID != "" {
			entry["span_id"] = spanID
		}

		// Add base fields
		for _, f := range l.baseFields {
			if f.Value != nil {
				entry[f.Key] = redactFieldValue(f)
			}
		}

		enc, err := json.Marshal(entry)
		if err != nil {
			_, _ = fmt.Fprintf(out, "{\"timestamp\":%q,\"level\":\"error\",\"message\":%q}\n",
				time.Now().UTC().Format(time.RFC3339Nano), err.Error())
			return
		}
		if _, err := fmt.Fprintf(out, "%s\n", enc); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "logger write failed: %v\n", err)
		}
		return
	}

	prefix := c.Sprintf("[%s]", label)
	var sb strings.Builder
	sb.WriteString(msg)
	if traceID != "" {
		sb.WriteString(" trace_id=")
		sb.WriteString(traceID)
	}
	// Append base fields for format-based calls in color mode
	for _, f := range l.baseFields {
		if f.Value != nil {
			sb.WriteString(" ")
			sb.WriteString(f.Key)
			sb.WriteString("=")
			fmt.Fprint(&sb, redactFieldValue(f))
		}
	}

	if _, err := fmt.Fprintf(out, "%s %s\n", prefix, sb.String()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "logger write failed: %v\n", err)
	}
}

func traceDetails(ctx context.Context) (traceID string, spanID string) {
	if ctx == nil {
		return "", ""
	}
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return "", ""
	}
	sc := span.SpanContext()
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}

// RedactPII removes common secret-bearing and personally identifying substrings
// from logs/traces without attempting full privacy classification. It is
// intentionally conservative and focuses on known credential patterns (bearer
// tokens, API keys, cookies, etc) plus high-signal PII like emails and network
// addresses.
func RedactPII(input string) string {
	if input == "" {
		return input
	}
	redacted := input
	redacted = bearerTokenPattern.ReplaceAllString(redacted, "Bearer [REDACTED]")
	redacted = authHeaderPattern.ReplaceAllString(redacted, "$1$2 [REDACTED]")
	redacted = jwtPattern.ReplaceAllString(redacted, "[REDACTED_JWT]")
	redacted = apiKeyQueryPattern.ReplaceAllString(redacted, "${1}${2}[REDACTED]")
	redacted = envSecretPattern.ReplaceAllString(redacted, "$1=[REDACTED]")
	redacted = openAIKeyPattern.ReplaceAllString(redacted, "sk-[REDACTED]")
	redacted = jsonSecretPattern.ReplaceAllString(redacted, `$1"[REDACTED]"`)
	redacted = emailPattern.ReplaceAllString(redacted, "[REDACTED_EMAIL]")
	redacted = ipv6BracketPattern.ReplaceAllString(redacted, "[REDACTED_IP]")
	redacted = ipv4Pattern.ReplaceAllString(redacted, "[REDACTED_IP]")
	return redacted
}
