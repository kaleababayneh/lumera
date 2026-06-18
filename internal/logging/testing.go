// Package logging provides colorized, structured logging utilities for Lumera services.
package logging

import (
	"context"
	"sync"
	"testing"
)

// Compile-time check that TestLogger implements StructuredLogger.
var _ StructuredLogger = (*TestLogger)(nil)

// LogEntry represents a captured log entry for test assertions.
type LogEntry struct {
	Level   string
	Message string
	Fields  map[string]any
	TraceID string
	SpanID  string
}

// TestLogger is a Logger implementation that captures log entries for test assertions.
// It implements StructuredLogger and is safe for concurrent use.
type TestLogger struct {
	entries    []LogEntry
	mu         sync.Mutex
	level      Level
	baseFields []Field
	traceID    string
	spanID     string
	component  string
	root       *TestLogger
}

// NewTestLogger creates a new TestLogger that captures all log levels.
func NewTestLogger() *TestLogger {
	return &TestLogger{
		entries: make([]LogEntry, 0),
		level:   LevelDebug,
	}
}

// NewTestLoggerWithLevel creates a new TestLogger with a minimum log level.
func NewTestLoggerWithLevel(level string) *TestLogger {
	return &TestLogger{
		entries: make([]LogEntry, 0),
		level:   parseLevel(level),
	}
}

// Debug logs a debug message with structured fields.
func (l *TestLogger) Debug(msg string, fields ...Field) {
	if l.level > LevelDebug {
		return
	}
	l.record("debug", msg, "", "", fields)
}

// Info logs an info message with structured fields.
func (l *TestLogger) Info(msg string, fields ...Field) {
	if l.level > LevelInfo {
		return
	}
	l.record("info", msg, "", "", fields)
}

// Warn logs a warning message with structured fields.
func (l *TestLogger) Warn(msg string, fields ...Field) {
	if l.level > LevelWarn {
		return
	}
	l.record("warn", msg, "", "", fields)
}

// Error logs an error message with structured fields.
func (l *TestLogger) Error(msg string, fields ...Field) {
	if l.level > LevelError {
		return
	}
	l.record("error", msg, "", "", fields)
}

// DebugContext logs debug with context metadata (trace_id, span_id).
func (l *TestLogger) DebugContext(ctx context.Context, msg string, fields ...Field) {
	if l.level > LevelDebug {
		return
	}
	traceID, spanID := extractTraceFromContext(ctx)
	l.record("debug", msg, traceID, spanID, fields)
}

// InfoContext logs info with context metadata.
func (l *TestLogger) InfoContext(ctx context.Context, msg string, fields ...Field) {
	if l.level > LevelInfo {
		return
	}
	traceID, spanID := extractTraceFromContext(ctx)
	l.record("info", msg, traceID, spanID, fields)
}

// WarnContext logs warning with context metadata.
func (l *TestLogger) WarnContext(ctx context.Context, msg string, fields ...Field) {
	if l.level > LevelWarn {
		return
	}
	traceID, spanID := extractTraceFromContext(ctx)
	l.record("warn", msg, traceID, spanID, fields)
}

// ErrorContext logs error with context metadata.
func (l *TestLogger) ErrorContext(ctx context.Context, msg string, fields ...Field) {
	if l.level > LevelError {
		return
	}
	traceID, spanID := extractTraceFromContext(ctx)
	l.record("error", msg, traceID, spanID, fields)
}

// With creates a child logger with additional fields attached.
func (l *TestLogger) With(fields ...Field) StructuredLogger {
	root := l.captureRoot()
	child := &TestLogger{
		root:       root,
		level:      l.level,
		baseFields: append(append([]Field{}, l.baseFields...), fields...),
		traceID:    l.traceID,
		spanID:     l.spanID,
		component:  l.component,
	}
	return child
}

// WithTrace creates a child logger bound to a trace and span.
func (l *TestLogger) WithTrace(traceID, spanID string) StructuredLogger {
	root := l.captureRoot()
	child := &TestLogger{
		root:       root,
		level:      l.level,
		baseFields: append([]Field{}, l.baseFields...),
		traceID:    traceID,
		spanID:     spanID,
		component:  l.component,
	}
	return child
}

// WithComponent creates a child logger for a specific component.
func (l *TestLogger) WithComponent(component string) StructuredLogger {
	root := l.captureRoot()
	child := &TestLogger{
		root:       root,
		level:      l.level,
		baseFields: append([]Field{}, l.baseFields...),
		traceID:    l.traceID,
		spanID:     l.spanID,
		component:  component,
	}
	return child
}

// record adds a log entry to the captured entries.
func (l *TestLogger) record(level, msg, traceID, spanID string, fields []Field) {
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()

	// Merge base fields with provided fields
	allFields := append(append([]Field{}, l.baseFields...), fields...)

	// Add component if set
	if l.component != "" {
		allFields = append(allFields, String(FieldComponent, l.component))
	}

	// Use instance trace IDs if context didn't provide them
	if traceID == "" {
		traceID = l.traceID
	}
	if spanID == "" {
		spanID = l.spanID
	}

	root.entries = append(root.entries, LogEntry{
		Level:   level,
		Message: msg,
		Fields:  fieldsToMap(allFields),
		TraceID: traceID,
		SpanID:  spanID,
	})
}

// Entries returns a copy of all captured log entries.
func (l *TestLogger) Entries() []LogEntry {
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()
	result := make([]LogEntry, len(root.entries))
	for i := range root.entries {
		result[i] = cloneLogEntry(root.entries[i])
	}
	return result
}

// Reset clears all captured log entries.
func (l *TestLogger) Reset() {
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()
	root.entries = root.entries[:0]
}

// AssertLogged asserts that a log message was recorded.
func (l *TestLogger) AssertLogged(t *testing.T, msg string) {
	t.Helper()
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()
	for _, e := range root.entries {
		if e.Message == msg {
			return
		}
	}
	t.Errorf("expected log message %q not found in %d entries", msg, len(root.entries))
}

// AssertNotLogged asserts that a log message was NOT recorded.
func (l *TestLogger) AssertNotLogged(t *testing.T, msg string) {
	t.Helper()
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()
	for _, e := range root.entries {
		if e.Message == msg {
			t.Errorf("expected log message %q to not be present, but it was", msg)
			return
		}
	}
}

// AssertLoggedWithLevel asserts that a message was logged at a specific level.
func (l *TestLogger) AssertLoggedWithLevel(t *testing.T, level, msg string) {
	t.Helper()
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()
	for _, e := range root.entries {
		if e.Message == msg && e.Level == level {
			return
		}
	}
	t.Errorf("expected log message %q at level %q not found", msg, level)
}

// AssertLoggedWithField asserts that a message was logged with a specific field value.
func (l *TestLogger) AssertLoggedWithField(t *testing.T, msg, field string, value any) {
	t.Helper()
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()
	for _, e := range root.entries {
		if e.Message == msg {
			if v, ok := e.Fields[field]; ok && v == value {
				return
			}
		}
	}
	t.Errorf("expected log %q with field %s=%v not found", msg, field, value)
}

// AssertLoggedWithFields asserts that a message was logged with all specified fields.
func (l *TestLogger) AssertLoggedWithFields(t *testing.T, msg string, expectedFields map[string]any) {
	t.Helper()
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()
	for _, e := range root.entries {
		if e.Message == msg {
			allMatch := true
			for k, v := range expectedFields {
				if actual, ok := e.Fields[k]; !ok || actual != v {
					allMatch = false
					break
				}
			}
			if allMatch {
				return
			}
		}
	}
	t.Errorf("expected log %q with fields %v not found", msg, expectedFields)
}

// FindEntries returns all entries matching the given message.
func (l *TestLogger) FindEntries(msg string) []LogEntry {
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()
	var result []LogEntry
	for _, e := range root.entries {
		if e.Message == msg {
			result = append(result, cloneLogEntry(e))
		}
	}
	return result
}

// Count returns the number of log entries matching the given level.
// If level is empty, returns total count.
func (l *TestLogger) Count(level string) int {
	root := l.captureRoot()
	root.mu.Lock()
	defer root.mu.Unlock()
	if level == "" {
		return len(root.entries)
	}
	count := 0
	for _, e := range root.entries {
		if e.Level == level {
			count++
		}
	}
	return count
}

func (l *TestLogger) captureRoot() *TestLogger {
	if l.root != nil {
		return l.root
	}
	return l
}

func cloneLogEntry(entry LogEntry) LogEntry {
	entry.Fields = cloneLogFields(entry.Fields)
	return entry
}

func cloneLogFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		out[key] = value
	}
	return out
}
