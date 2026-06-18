package logging

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// ZerologLogger implements StructuredLogger using zerolog.
type ZerologLogger struct {
	logger zerolog.Logger
}

// NewZerolog creates a new zerolog-based structured logger.
func NewZerolog(service, level string) *ZerologLogger {
	return NewZerologWithWriter(os.Stdout, service, level)
}

// NewZerologWithWriter creates a new zerolog-based logger with a custom writer.
func NewZerologWithWriter(w io.Writer, service, level string) *ZerologLogger {
	lvl := parseZerologLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zl := zerolog.New(w).
		Level(lvl).
		With().
		Timestamp().
		Str(FieldService, service).
		Logger()
	return &ZerologLogger{logger: zl}
}

func parseZerologLevel(level string) zerolog.Level {
	normalized := strings.ToLower(strings.TrimSpace(level))
	switch normalized {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// Debug logs verbose developer-oriented diagnostics.
func (l *ZerologLogger) Debug(msg string, fields ...Field) {
	l.log(l.logger.Debug(), msg, fields)
}

// Info logs high-level operational events.
func (l *ZerologLogger) Info(msg string, fields ...Field) {
	l.log(l.logger.Info(), msg, fields)
}

// Warn logs recoverable anomalies that warrant attention.
func (l *ZerologLogger) Warn(msg string, fields ...Field) {
	l.log(l.logger.Warn(), msg, fields)
}

// Error logs unrecoverable faults.
func (l *ZerologLogger) Error(msg string, fields ...Field) {
	l.log(l.logger.Error(), msg, fields)
}

// DebugContext logs debug with context metadata (trace_id, span_id).
func (l *ZerologLogger) DebugContext(ctx context.Context, msg string, fields ...Field) {
	l.logWithContext(ctx, l.logger.Debug(), msg, fields)
}

// InfoContext logs info with context metadata.
func (l *ZerologLogger) InfoContext(ctx context.Context, msg string, fields ...Field) {
	l.logWithContext(ctx, l.logger.Info(), msg, fields)
}

// WarnContext logs warning with context metadata.
func (l *ZerologLogger) WarnContext(ctx context.Context, msg string, fields ...Field) {
	l.logWithContext(ctx, l.logger.Warn(), msg, fields)
}

// ErrorContext logs error with context metadata.
func (l *ZerologLogger) ErrorContext(ctx context.Context, msg string, fields ...Field) {
	l.logWithContext(ctx, l.logger.Error(), msg, fields)
}

// With creates a child logger with additional fields attached.
func (l *ZerologLogger) With(fields ...Field) StructuredLogger {
	ctx := l.logger.With()
	for _, f := range fields {
		ctx = addFieldToContext(ctx, f)
	}
	return &ZerologLogger{logger: ctx.Logger()}
}

// WithTrace creates a child logger bound to a trace and span.
func (l *ZerologLogger) WithTrace(traceID, spanID string) StructuredLogger {
	zl := l.logger.With().
		Str(FieldTraceID, traceID).
		Str(FieldSpanID, spanID).
		Logger()
	return &ZerologLogger{logger: zl}
}

// WithComponent creates a child logger for a specific component.
func (l *ZerologLogger) WithComponent(component string) StructuredLogger {
	zl := l.logger.With().
		Str(FieldComponent, component).
		Logger()
	return &ZerologLogger{logger: zl}
}

func (l *ZerologLogger) log(event *zerolog.Event, msg string, fields []Field) {
	for _, f := range fields {
		event = addField(event, f)
	}
	event.Msg(RedactPII(msg))
}

func (l *ZerologLogger) logWithContext(ctx context.Context, event *zerolog.Event, msg string, fields []Field) {
	// Extract trace context from OpenTelemetry
	if ctx != nil {
		span := trace.SpanFromContext(ctx)
		if span != nil {
			sc := span.SpanContext()
			if sc.IsValid() {
				event = event.Str(FieldTraceID, sc.TraceID().String())
				event = event.Str(FieldSpanID, sc.SpanID().String())
			}
		}
	}

	for _, f := range fields {
		event = addField(event, f)
	}
	event.Msg(RedactPII(msg))
}

func addField(event *zerolog.Event, f Field) *zerolog.Event {
	if f.Value == nil {
		return event.Interface(f.Key, nil)
	}

	switch v := f.Value.(type) {
	case string:
		return event.Str(f.Key, RedactPII(v))
	case int:
		return event.Int(f.Key, v)
	case int64:
		return event.Int64(f.Key, v)
	case float64:
		if label, ok := nonFiniteFloatString(v); ok {
			return event.Str(f.Key, label)
		}
		return event.Float64(f.Key, v)
	case float32:
		if label, ok := nonFiniteFloatString(float64(v)); ok {
			return event.Str(f.Key, label)
		}
		return event.Interface(f.Key, v)
	case bool:
		return event.Bool(f.Key, v)
	case time.Duration:
		return event.Dur(f.Key, v)
	case time.Time:
		return event.Time(f.Key, v)
	case []string:
		return event.Strs(f.Key, v)
	case error:
		return event.Err(v)
	case MoneyValue:
		return event.Interface(f.Key, v)
	default:
		return event.Interface(f.Key, v)
	}
}

func addFieldToContext(ctx zerolog.Context, f Field) zerolog.Context {
	if f.Value == nil {
		return ctx.Interface(f.Key, nil)
	}

	switch v := f.Value.(type) {
	case string:
		return ctx.Str(f.Key, RedactPII(v))
	case int:
		return ctx.Int(f.Key, v)
	case int64:
		return ctx.Int64(f.Key, v)
	case float64:
		if label, ok := nonFiniteFloatString(v); ok {
			return ctx.Str(f.Key, label)
		}
		return ctx.Float64(f.Key, v)
	case float32:
		if label, ok := nonFiniteFloatString(float64(v)); ok {
			return ctx.Str(f.Key, label)
		}
		return ctx.Interface(f.Key, v)
	case bool:
		return ctx.Bool(f.Key, v)
	case time.Duration:
		return ctx.Dur(f.Key, v)
	case time.Time:
		return ctx.Time(f.Key, v)
	case []string:
		return ctx.Strs(f.Key, v)
	case MoneyValue:
		return ctx.Interface(f.Key, v)
	default:
		return ctx.Interface(f.Key, v)
	}
}
