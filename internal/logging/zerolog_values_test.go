package logging

import (
	"testing"

	"github.com/rs/zerolog"
)

// The existing TestParseZerologLevel only verifies the helper
// doesn't panic — it never asserts that "debug" returns
// DebugLevel, that "error" returns ErrorLevel, or that the
// unknown-default is Info (not silently dropping to a lower
// level). A regression swapping info↔warn (or defaulting
// unknown to ErrorLevel) would pass that test while silently
// re-tiering every service's log verbosity in production.
//
// Similarly, the log-field name constants (FieldTraceID,
// FieldService, etc.) are WIRE-LEVEL contracts — every log
// ingestion pipeline, every dashboard filter, every trace
// correlation rule matches on the exact JSON key "trace_id".
// Renaming FieldTraceID to "traceId" would compile, pass all
// existing tests (which reference the constant symbolically),
// and silently break every downstream consumer.
//
// This file adds complement value-pins for both.

// TestParseZerologLevel_ExactMapping pins the documented
// string→zerolog.Level mapping. Cases cover:
//
//   - the four canonical values (debug, info, warn, error);
//   - the unknown-default contract (returns InfoLevel, NOT a
//     silent drop to ErrorLevel or TraceLevel);
//   - case-insensitive match (DEBUG == debug);
//   - whitespace trimming (" info " == info).
//
// A regression swapping any pair, or flipping the unknown
// default to anything other than InfoLevel, is a service-wide
// verbosity retuning that must surface explicitly.
func TestParseZerologLevel_ExactMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want zerolog.Level
	}{
		// Canonical values.
		{"debug", "debug", zerolog.DebugLevel},
		{"info", "info", zerolog.InfoLevel},
		{"warn", "warn", zerolog.WarnLevel},
		{"error", "error", zerolog.ErrorLevel},

		// Unknown → Info default (the documented fallback — NOT
		// a silent drop to a higher/lower tier).
		{"unknown_defaults_to_info", "bogus", zerolog.InfoLevel},
		{"empty_defaults_to_info", "", zerolog.InfoLevel},
		{"whitespace_only_defaults_to_info", "   \t\n", zerolog.InfoLevel},

		// Case-insensitive match.
		{"uppercase_DEBUG", "DEBUG", zerolog.DebugLevel},
		{"mixed_case_Error", "Error", zerolog.ErrorLevel},
		{"uppercase_WARN", "WARN", zerolog.WarnLevel},

		// Whitespace trimming happens before case-folding and
		// before the string match — " info " == "info".
		{"padded_info", "  info  ", zerolog.InfoLevel},
		{"tab_padded_warn", "\twarn\t", zerolog.WarnLevel},
		{"mixed_padding_error", "  ERROR\n", zerolog.ErrorLevel},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseZerologLevel(tc.in); got != tc.want {
				t.Errorf("parseZerologLevel(%q) = %v, want %v — verbosity retune for production services",
					tc.in, got, tc.want)
			}
		})
	}
}

// TestLogFieldConstants_ExactValues pins the wire-level JSON
// key contract for structured log fields. These constants
// flow directly into zerolog's emitted JSON (via
// addField/addFieldToContext) and through to every log
// ingestion pipeline, trace-correlation rule, dashboard
// filter, and SLO query. A rename from "trace_id" to
// "traceId" would compile, pass every symbolic-reference test,
// and silently shatter correlation across the platform.
//
// Grouped by domain so a future addition that forgets to pin
// its new constant here is an obvious omission rather than a
// buried field somewhere.
func TestLogFieldConstants_ExactValues(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		got  string
		want string
	}{
		// Trace correlation — must match OpenTelemetry's
		// exported attribute keys so trace_id/span_id can be
		// joined across log and APM systems.
		"FieldTraceID": {FieldTraceID, "trace_id"},
		"FieldSpanID":  {FieldSpanID, "span_id"},

		// Request metadata — consumed by HTTP access-log
		// dashboards and request-flow replays.
		"FieldSessionID": {FieldSessionID, "session_id"},
		"FieldRequestID": {FieldRequestID, "request_id"},
		"FieldMethod":    {FieldMethod, "method"},
		"FieldPath":      {FieldPath, "path"},
		"FieldStatus":    {FieldStatus, "status"},

		// Tool / operation context.
		"FieldToolID":    {FieldToolID, "tool_id"},
		"FieldQuoteID":   {FieldQuoteID, "quote_id"},
		"FieldReceiptID": {FieldReceiptID, "receipt_id"},

		// Economic context — aligned with lumera_ai-kp14 Money
		// standardization; renaming any of these shifts how
		// billing and audit reports read log data.
		"FieldCostLAC":           {FieldCostLAC, "cost_lac"},
		"FieldBudgetAvailableLC": {FieldBudgetAvailableLC, "budget_available_lac"},
		"FieldBudgetUsedLAC":     {FieldBudgetUsedLAC, "budget_used_lac"},

		// Performance — dashboards bucketize on these exact
		// keys for P50/P95/P99 latency reporting.
		"FieldDurationMS": {FieldDurationMS, "duration_ms"},
		"FieldLatencyMS":  {FieldLatencyMS, "latency_ms"},

		// Service context.
		"FieldService":   {FieldService, "service"},
		"FieldComponent": {FieldComponent, "component"},
		"FieldVersion":   {FieldVersion, "version"},

		// Error context — retry/backoff clients read these
		// keys to honor retry_after_ms; dashboards route
		// unrecoverable errors via error_code/recoverable.
		"FieldErrorCode":    {FieldErrorCode, "error_code"},
		"FieldRecoverable":  {FieldRecoverable, "recoverable"},
		"FieldRetryAfterMS": {FieldRetryAfterMS, "retry_after_ms"},
	}

	for name, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q — wire-level rename; coordinate with log ingestion, dashboards, and retry clients",
				name, tc.got, tc.want)
		}
	}
}

// TestLogFieldConstants_NoCollision pins the uniqueness
// invariant: no two log-field constants share the same JSON
// key. A duplicate (e.g. FieldX = FieldY = "status") would
// mean two semantically-distinct fields emit under the same
// key, silently overwriting each other in the JSON output
// and corrupting every dashboard that aggregates on that
// key. Values-test checks canonical strings; this test
// checks the orthogonal matrix.
func TestLogFieldConstants_NoCollision(t *testing.T) {
	t.Parallel()

	named := map[string]string{
		"FieldTraceID":           FieldTraceID,
		"FieldSpanID":            FieldSpanID,
		"FieldSessionID":         FieldSessionID,
		"FieldRequestID":         FieldRequestID,
		"FieldMethod":            FieldMethod,
		"FieldPath":              FieldPath,
		"FieldStatus":            FieldStatus,
		"FieldToolID":            FieldToolID,
		"FieldQuoteID":           FieldQuoteID,
		"FieldReceiptID":         FieldReceiptID,
		"FieldCostLAC":           FieldCostLAC,
		"FieldBudgetAvailableLC": FieldBudgetAvailableLC,
		"FieldBudgetUsedLAC":     FieldBudgetUsedLAC,
		"FieldDurationMS":        FieldDurationMS,
		"FieldLatencyMS":         FieldLatencyMS,
		"FieldService":           FieldService,
		"FieldComponent":         FieldComponent,
		"FieldVersion":           FieldVersion,
		"FieldErrorCode":         FieldErrorCode,
		"FieldRecoverable":       FieldRecoverable,
		"FieldRetryAfterMS":      FieldRetryAfterMS,
	}

	// Invert: canonical string → set of constant names using it.
	byValue := make(map[string][]string, len(named))
	for name, val := range named {
		byValue[val] = append(byValue[val], name)
	}
	for val, names := range byValue {
		if len(names) > 1 {
			t.Errorf("log-field value %q is emitted by multiple constants %v — duplicate JSON keys would overwrite each other in emitted logs",
				val, names)
		}
	}
}
