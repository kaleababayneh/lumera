// Package logging provides colorized, structured logging utilities for Lumera services.
package logging

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// structuredLoggerKey is the context key for StructuredLogger.
// Uses a different type than the legacy loggerKey to avoid conflicts.
type structuredLoggerKeyType struct{}

var structuredLoggerKey = structuredLoggerKeyType{}

// WithStructuredLogger returns a new context with a StructuredLogger attached.
func WithStructuredLogger(ctx context.Context, logger StructuredLogger) context.Context {
	return context.WithValue(ctx, structuredLoggerKey, logger)
}

// StructuredLoggerFromContext retrieves the StructuredLogger from context.
// Returns nil if no logger is present.
func StructuredLoggerFromContext(ctx context.Context) StructuredLogger {
	if ctx == nil {
		return nil
	}
	l, _ := ctx.Value(structuredLoggerKey).(StructuredLogger)
	return l
}

// StructuredLoggerFromContextOrDefault retrieves the StructuredLogger from context,
// or returns a default logger if not present.
func StructuredLoggerFromContextOrDefault(ctx context.Context, defaultLogger StructuredLogger) StructuredLogger {
	l := StructuredLoggerFromContext(ctx)
	if l != nil {
		return l
	}
	return defaultLogger
}

// extractTraceFromContext extracts trace_id and span_id from an OpenTelemetry context.
func extractTraceFromContext(ctx context.Context) (traceID, spanID string) {
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
