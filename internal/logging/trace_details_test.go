package logging

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

// traceDetails in logging.go is a near-duplicate of extractTraceFromContext
// in context.go. Both shape the structured log record's trace_id /
// span_id fields. The extractTraceFromContext variant has three-branch
// coverage (nil ctx / invalid span context / valid span context via
// TestExtractTraceFromContext in structured_test.go); traceDetails only
// had the invalid-SpanContext branch (via context.Background()).
//
// A silent divergence between the two — one returning IDs when the
// other doesn't — would produce inconsistent log tagging across code
// paths that happen to use different helpers. Pinning all three
// branches of traceDetails closes the divergence.

// TestTraceDetails_NilContextReturnsEmpty pins the nil-ctx fast path.
// TestTraceDetails_NilContext in logging_extra_test.go actually passes
// context.Background() (not nil); this test passes literal nil.
func TestTraceDetails_NilContextReturnsEmpty(t *testing.T) {
	//nolint:staticcheck // explicitly testing the nil-ctx branch.
	traceID, spanID := traceDetails(nil)
	require.Empty(t, traceID, "nil ctx must yield empty trace id")
	require.Empty(t, spanID, "nil ctx must yield empty span id")
}

// TestTraceDetails_InvalidSpanContextReturnsEmpty pins the branch
// where a SpanContext exists but is marked invalid (zero IDs). This
// is the realistic "background context with otel initialized but no
// active span" case.
func TestTraceDetails_InvalidSpanContextReturnsEmpty(t *testing.T) {
	ctx := trace.ContextWithSpanContext(context.Background(), trace.SpanContext{})
	traceID, spanID := traceDetails(ctx)
	require.Empty(t, traceID, "invalid span context must yield empty trace id")
	require.Empty(t, spanID, "invalid span context must yield empty span id")
}

// TestTraceDetails_ValidSpanContextReturnsIDs pins the happy path —
// previously unreached. Without this test, a regression that (for
// example) returned empty strings even on valid spans would pass
// silently (the tests would still see empty values and pass).
func TestTraceDetails_ValidSpanContextReturnsIDs(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:     trace.SpanID{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	traceID, spanID := traceDetails(ctx)
	require.Equal(t, sc.TraceID().String(), traceID,
		"valid span must yield its canonical trace id verbatim")
	require.Equal(t, sc.SpanID().String(), spanID,
		"valid span must yield its canonical span id verbatim")
}

// TestTraceDetails_MatchesExtractTraceFromContext pins the invariant
// that the two helpers (traceDetails in logging.go, extractTraceFromContext
// in context.go) return byte-identical outputs for the same input.
// If the two ever diverge, log tagging becomes inconsistent across
// code paths — a hard-to-diagnose telemetry class of bug.
func TestTraceDetails_MatchesExtractTraceFromContext(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0xfe, 0xed, 0xfa, 0xce, 0xca, 0xfe, 0xba, 0xbe, 0xde, 0xad, 0xbe, 0xef, 0x00, 0x11, 0x22, 0x33},
		SpanID:     trace.SpanID{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88},
		TraceFlags: trace.FlagsSampled,
	})

	cases := []struct {
		name string
		ctx  context.Context
	}{
		{"nil_ctx", nil},
		{"background_no_span", context.Background()},
		{"invalid_span", trace.ContextWithSpanContext(context.Background(), trace.SpanContext{})},
		{"valid_span", trace.ContextWithSpanContext(context.Background(), sc)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tTrace, tSpan := traceDetails(tc.ctx)
			eTrace, eSpan := extractTraceFromContext(tc.ctx)
			require.Equal(t, eTrace, tTrace,
				"traceDetails and extractTraceFromContext must return identical trace_id — drift would split log tagging")
			require.Equal(t, eSpan, tSpan,
				"traceDetails and extractTraceFromContext must return identical span_id — drift would split log tagging")
		})
	}
}
