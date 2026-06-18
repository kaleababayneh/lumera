// Package logging provides structured, leveled logging with trace correlation for Lumera services.
package logging

import (
	"context"
)

// StructuredLogger defines the interface for structured logging with trace correlation.
// Implementations should be thread-safe and support child logger creation.
type StructuredLogger interface {
	// Debug logs verbose developer-oriented diagnostics.
	Debug(msg string, fields ...Field)
	// Info logs high-level operational events.
	Info(msg string, fields ...Field)
	// Warn logs recoverable anomalies that warrant attention.
	Warn(msg string, fields ...Field)
	// Error logs unrecoverable faults.
	Error(msg string, fields ...Field)

	// DebugContext logs debug with context metadata (trace_id, span_id).
	DebugContext(ctx context.Context, msg string, fields ...Field)
	// InfoContext logs info with context metadata.
	InfoContext(ctx context.Context, msg string, fields ...Field)
	// WarnContext logs warning with context metadata.
	WarnContext(ctx context.Context, msg string, fields ...Field)
	// ErrorContext logs error with context metadata.
	ErrorContext(ctx context.Context, msg string, fields ...Field)

	// With creates a child logger with additional fields attached.
	With(fields ...Field) StructuredLogger
	// WithTrace creates a child logger bound to a trace and span.
	WithTrace(traceID, spanID string) StructuredLogger
	// WithComponent creates a child logger for a specific component.
	WithComponent(component string) StructuredLogger
}

// MoneyValue represents a structured money field with amount and currency.
// This aligns with lumera_ai-kp14 Money standardization.
type MoneyValue struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// Money creates a money field.
func Money(key string, amount, currency string) Field {
	return Field{Key: key, Value: MoneyValue{Amount: amount, Currency: currency}}
}

// Standard field names for consistency across all log points.
const (
	// Trace correlation
	FieldTraceID = "trace_id"
	FieldSpanID  = "span_id"

	// Request metadata
	FieldSessionID = "session_id"
	FieldRequestID = "request_id"
	FieldMethod    = "method"
	FieldPath      = "path"
	FieldStatus    = "status"

	// Tool/Operation context
	FieldToolID    = "tool_id"
	FieldQuoteID   = "quote_id"
	FieldReceiptID = "receipt_id"

	// Economic context
	FieldCostLAC           = "cost_lac"
	FieldBudgetAvailableLC = "budget_available_lac"
	FieldBudgetUsedLAC     = "budget_used_lac"

	// Performance
	FieldDurationMS = "duration_ms"
	FieldLatencyMS  = "latency_ms"

	// Service context
	FieldService   = "service"
	FieldComponent = "component"
	FieldVersion   = "version"

	// Error context
	FieldErrorCode    = "error_code"
	FieldRecoverable  = "recoverable"
	FieldRetryAfterMS = "retry_after_ms"
)
