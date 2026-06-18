//go:build cosmos

package types

import (
	"math"
	"testing"
	"time"
)

// TestReputationResult_ToProto_ClampAndScaleInvariantMetamorphic asserts
// that every output field fits in [0, 1000] — the Clamp01 rails times
// 1000 upper bound from the toU32 helper. A regression that dropped
// Clamp01 or changed the scale factor would let fields overflow uint32
// silently (no panic from Go), then downstream rank comparisons would
// go wrong.
func TestReputationResult_ToProto_ClampAndScaleInvariantMetamorphic(t *testing.T) {
	t.Parallel()
	cases := []ReputationResult{
		// All extremes low (including below-zero and NaN-producing values).
		{Reliability: -0.5, Safety: -100, Latency: math.NaN(), CostDiscipline: 0, Dispute: 0, Longevity: 0, Privacy: 0},
		// All extremes high.
		{Reliability: 1.0, Safety: 1.0, Latency: 1.0, CostDiscipline: 1.0, Dispute: 1.0, Longevity: 1.0, Privacy: 1.0},
		// All way-above-one.
		{Reliability: 100, Safety: 1e10, Latency: math.Inf(+1), CostDiscipline: 42, Dispute: 2.5, Longevity: 999, Privacy: 3},
		// Typical mid-range.
		{Reliability: 0.7, Safety: 0.9, Latency: 0.5, CostDiscipline: 0.6, Dispute: 0.8, Longevity: 0.3, Privacy: 0.2},
	}
	for i, r := range cases {
		proto := r.ToProto(time.Now())
		if proto == nil {
			t.Fatalf("case %d: ToProto returned nil", i)
		}
		for _, pair := range []struct {
			name string
			val  uint32
		}{
			{"Reliability", proto.Reliability},
			{"Quality", proto.Quality},
			{"Trustworthiness", proto.Trustworthiness},
			{"Composite", proto.Composite},
		} {
			if pair.val > 1000 {
				t.Errorf("case %d: %s=%d exceeds 1000", i, pair.name, pair.val)
			}
		}
	}
}

// TestReputationResult_ToProto_FieldMappingMetamorphic pins the
// documented (reliability, safety, dispute) → (reliability, quality,
// trustworthiness) direct mapping. Composite comes from
// BalancedComposite, not any single raw field. A regression that
// e.g. mapped Latency → Quality instead of Safety → Quality would
// silently misreport provider reputation to on-chain viewers.
func TestReputationResult_ToProto_FieldMappingMetamorphic(t *testing.T) {
	t.Parallel()
	// Construct a result where each field has a distinct value so
	// mapping mismatches are easy to spot.
	r := ReputationResult{
		Reliability:    0.80, // → 800
		Safety:         0.90, // → 900 (into Quality)
		Latency:        0.10, // not surfaced directly
		CostDiscipline: 0.20, // not surfaced directly
		Dispute:        0.70, // → 700 (into Trustworthiness)
		Longevity:      0.30, // not surfaced directly
		Privacy:        0.50, // weight 0 in composite
	}
	proto := r.ToProto(time.Now())
	if proto.Reliability != 800 {
		t.Errorf("Reliability mapping broken: got %d, want 800", proto.Reliability)
	}
	if proto.Quality != 900 {
		t.Errorf("Safety→Quality mapping broken: got %d, want 900", proto.Quality)
	}
	if proto.Trustworthiness != 700 {
		t.Errorf("Dispute→Trustworthiness mapping broken: got %d, want 700", proto.Trustworthiness)
	}
	// Composite is Reliability*0.25 + Safety*0.30 + Latency*0.10 +
	// CostDiscipline*0.10 + Dispute*0.15 + Longevity*0.10 + Privacy*0.00
	// = 0.80*0.25 + 0.90*0.30 + 0.10*0.10 + 0.20*0.10 + 0.70*0.15 +
	//   0.30*0.10 + 0.50*0.00
	// = 0.20 + 0.27 + 0.01 + 0.02 + 0.105 + 0.03 + 0 = 0.635
	// Scaled to uint32: round(635) = 635.
	if proto.Composite != 635 {
		t.Errorf("Composite = %d, want 635 (computed from BalancedComposite)", proto.Composite)
	}
}

// TestReputationResult_ToProto_ZeroResultIsAllZerosMetamorphic asserts
// the zero-value result produces an all-zero proto. A brand-new
// PassportSummary with no receipts yet runs through this path; a
// regression that emitted a neutral 500 default instead of 0 would
// silently let new providers look middle-tier instead of unranked.
func TestReputationResult_ToProto_ZeroResultIsAllZerosMetamorphic(t *testing.T) {
	t.Parallel()
	r := ReputationResult{} // all dimensions zero
	proto := r.ToProto(time.Now())
	if proto.Reliability != 0 || proto.Quality != 0 ||
		proto.Trustworthiness != 0 || proto.Composite != 0 {
		t.Fatalf("zero result produced non-zero proto: %+v", proto)
	}
}

// TestReputationResult_ToProto_AllOnesIsMaxMetamorphic is the dual
// of the zero-result test: max-value input (all dims = 1.0) produces
// the proto's documented max (1000 for every field). This also
// exercises the BalancedComposite "all ones" identity where the
// weighted sum collapses to the total weight (1.0 minus privacy's
// 0.0 weight = 1.0).
func TestReputationResult_ToProto_AllOnesIsMaxMetamorphic(t *testing.T) {
	t.Parallel()
	r := ReputationResult{
		Reliability: 1, Safety: 1, Latency: 1, CostDiscipline: 1,
		Dispute: 1, Longevity: 1, Privacy: 1,
	}
	proto := r.ToProto(time.Now())
	if proto.Reliability != 1000 {
		t.Errorf("Reliability=%d, want 1000", proto.Reliability)
	}
	if proto.Quality != 1000 {
		t.Errorf("Quality=%d, want 1000", proto.Quality)
	}
	if proto.Trustworthiness != 1000 {
		t.Errorf("Trustworthiness=%d, want 1000", proto.Trustworthiness)
	}
	// Composite: 0.25+0.30+0.10+0.10+0.15+0.10+0 = 1.0 → 1000.
	if proto.Composite != 1000 {
		t.Errorf("Composite=%d, want 1000 (all weights sum to 1.0)", proto.Composite)
	}
}

// TestReputationResult_ToProto_NaNInputProducesZeroMetamorphic
// exercises the Clamp01 NaN guard at the proto boundary: if any
// dimension is NaN (e.g. from an earlier divide-by-zero upstream),
// its proto field must be 0 rather than a garbage uint32 bit
// pattern. Consensus cannot tolerate non-deterministic NaN ordering
// across validator platforms.
func TestReputationResult_ToProto_NaNInputProducesZeroMetamorphic(t *testing.T) {
	t.Parallel()
	r := ReputationResult{
		Reliability:    math.NaN(),
		Safety:         math.NaN(),
		Latency:        math.NaN(),
		CostDiscipline: math.NaN(),
		Dispute:        math.NaN(),
		Longevity:      math.NaN(),
		Privacy:        math.NaN(),
	}
	proto := r.ToProto(time.Now())
	if proto.Reliability != 0 || proto.Quality != 0 ||
		proto.Trustworthiness != 0 {
		t.Fatalf("NaN dims leaked through: %+v", proto)
	}
	// BalancedComposite over all-NaN may or may not be NaN depending
	// on arithmetic order; toU32 clamps via Clamp01, so the final
	// Composite field must still be 0.
	if proto.Composite != 0 {
		t.Fatalf("Composite=%d on all-NaN input, want 0", proto.Composite)
	}
}

// TestReputationResult_ToProto_RoundingBoundaryMetamorphic asserts
// math.Round behavior at the 0.5 boundary: the helper uses round-
// half-away-from-zero, so e.g. 0.5005 * 1000 = 500.5 rounds to 501.
// Validators compute the same result because math.Round is
// deterministic by IEEE semantics — but any refactor that swapped
// to truncation would shift the boundary silently.
func TestReputationResult_ToProto_RoundingBoundaryMetamorphic(t *testing.T) {
	t.Parallel()
	// Use values clearly below and above the 0.5 boundary to avoid
	// float64 representation noise near exact halves. Values like
	// 0.5005 are actually ≈0.500499999... in float64, which would
	// fall below 0.5 after *1000 and confuse a naive half-boundary
	// test. The pairs below span the boundary by a clear margin.
	cases := []struct {
		name  string
		field float64
		want  uint32
	}{
		{"0.0004 rounds to 0", 0.0004, 0},
		{"0.0006 rounds to 1", 0.0006, 1},
		{"0.5004 rounds to 500", 0.5004, 500},
		{"0.5006 rounds to 501", 0.5006, 501},
		{"0.9994 rounds to 999", 0.9994, 999},
		{"0.9996 rounds to 1000", 0.9996, 1000},
		{"exactly 1.0 stays 1000", 1.0, 1000},
		{"exactly 0.0 stays 0", 0.0, 0},
	}
	for _, tc := range cases {
		r := ReputationResult{Reliability: tc.field}
		proto := r.ToProto(time.Now())
		if proto.Reliability != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, proto.Reliability, tc.want)
		}
	}
}
