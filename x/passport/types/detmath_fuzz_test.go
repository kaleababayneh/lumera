package types

import (
	"math"
	"testing"
)

// This file applies the testing-fuzzing skill to x/passport's
// deterministic-math primitives (ExpNeg, Log1p, Clamp01) at
// detmath.go + scoring.go:174-182. These are CONSENSUS-CRITICAL:
// they underpin on-chain scoring and any cross-validator
// divergence causes chain-split.
//
// Existing detmath_test.go pins pointwise values, hand-crafted
// MR tests (monotonicity, determinism), and explicit boundary
// cases. This file adds fuzz coverage over the float64 input
// space, cross-validating against the stdlib (math.Exp(-x) /
// math.Log1p(x)) within the documented accuracy tolerances —
// a classic DIFFERENTIAL ORACLE from testing-fuzzing's
// six-patterns taxonomy.
//
// Invariants pinned per target:
//
//   FuzzExpNeg:
//     - never NaN, never Inf
//     - NaN → exactly 0
//     - x < 0 → exactly 1.0
//     - x >= 20 → exactly 0
//     - x in [0, 20) → result ∈ (0, 1], matches math.Exp(-x)
//       within the empirical 1e-4 relative tolerance
//     - deterministic (same input → same output across calls)
//
//   FuzzLog1p:
//     - never NaN, never Inf
//     - NaN and +Inf → exactly 0
//     - x <= 0 → exactly 0
//     - x > 0 → result > 0, matches math.Log1p(x) within the
//       empirical 1e-4 relative tolerance
//     - deterministic
//
//   FuzzClamp01:
//     - result always in [0, 1]
//     - NaN → 0 (consensus invariant)
//     - -Inf → 0, +Inf → 1
//     - in-range passes through unchanged
//
//   FuzzExpNegMonotonicity:
//     - x1 <= x2 ⇒ ExpNeg(x1) >= ExpNeg(x2) (exp is
//       monotonically decreasing; ExpNeg(-x) returns exp(-x))
//
//   FuzzLog1pMonotonicity:
//     - x1 <= x2 ⇒ Log1p(x1) <= Log1p(x2) (log1p is
//       monotonically increasing)

// floatIsFinite returns true iff x is a finite float64 (not
// NaN, not ±Inf).
func floatIsFinite(x float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0)
}

// relErr returns |a - b| / max(|a|, |b|, 1e-300). Uses a tiny
// denom floor to avoid div-by-zero when both values are 0.
func relErr(a, b float64) float64 {
	diff := math.Abs(a - b)
	scale := math.Max(math.Abs(a), math.Abs(b))
	if scale < 1e-300 {
		return diff
	}
	return diff / scale
}

// FuzzExpNeg cross-validates ExpNeg against the stdlib
// math.Exp(-x) within the documented accuracy bound. Exercises
// boundary regions (x ≈ 0, x ≈ 20, x huge) that pointwise tests
// may miss.
func FuzzExpNeg(f *testing.F) {
	// Seed: boundaries + scoring-relevant range.
	seeds := []float64{
		-1.0, -1e-10, 0, 1e-10,
		0.5, 1.0, 2.0, 5.0, 10.0, // mid-range
		19.0, 19.999, 20.0, 20.001, // around the x>=20 cutoff
		100.0, 1e10, math.MaxFloat64, // extreme
		math.NaN(), math.Inf(1), math.Inf(-1), // pathological
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, x float64) {
		got := ExpNeg(x)

		if !floatIsFinite(got) {
			t.Fatalf("ExpNeg(%v) returned non-finite %v", x, got)
		}

		if math.IsNaN(x) {
			if got != 0 {
				t.Fatalf("ExpNeg(NaN) = %v, want 0", got)
			}
			return
		}

		// Explicit boundary guards.
		if x <= 0 {
			if got != 1.0 {
				t.Fatalf("ExpNeg(%v) with x <= 0 returned %v, want 1.0 "+
					"(short-circuit at :26-28)", x, got)
			}
			return
		}
		if x >= 20 {
			if got != 0.0 {
				t.Fatalf("ExpNeg(%v) with x >= 20 returned %v, want 0 "+
					"(short-circuit at :30-32)", x, got)
			}
			return
		}

		// x in (0, 20): result ∈ (0, 1].
		if got <= 0 || got > 1 {
			t.Fatalf("ExpNeg(%v) = %v, out of expected (0, 1] range "+
				"for x ∈ (0, 20)", x, got)
		}

		// DIFFERENTIAL: matches math.Exp(-x) within 1e-4 relative.
		// Note: detmath.go's docstring claims "< 1e-10 relative
		// error across the full range" but empirically the Padé
		// [3,3] approximant combined with invE^n repeated squaring
		// can produce errors up to ~1e-5 (worst at x≈20). The
		// documented bound is incorrect; this fuzz pins the REAL
		// empirical bound so future accuracy regressions (e.g. a
		// reduction from Padé [3,3] to Padé [2,2]) surface without
		// false positives on today's accuracy. Separate follow-up
		// to tighten the implementation OR correct the docstring.
		want := math.Exp(-x)
		if rel := relErr(got, want); rel > 1e-4 {
			t.Fatalf("ExpNeg(%v) = %v diverges from math.Exp(-x) = "+
				"%v by %.2e relative (> 1e-4 empirical bound). "+
				"Accuracy regression beyond today's real behavior.",
				x, got, want, rel)
		}
	})
}

// FuzzLog1p cross-validates Log1p against math.Log1p.
//
// Non-finite inputs clamp to zero before argument reduction, so
// +Inf cannot spin the reduction loop and NaN cannot propagate
// into consensus-visible scores.
func FuzzLog1p(f *testing.F) {
	seeds := []float64{
		-1.0, -1e-10, 0, 1e-10,
		0.1, 0.5, 1.0, 2.0, 10.0, 1000.0,
		1e6, 1e15, math.MaxFloat64,
		math.NaN(), math.Inf(1), math.Inf(-1),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, x float64) {
		got := Log1p(x)

		if !floatIsFinite(got) {
			t.Fatalf("Log1p(%v) returned non-finite %v", x, got)
		}

		if math.IsNaN(x) || math.IsInf(x, 1) {
			if got != 0 {
				t.Fatalf("Log1p(%v) = %v, want 0 for non-finite input", x, got)
			}
			return
		}

		if x <= 0 {
			if got != 0.0 {
				t.Fatalf("Log1p(%v) with x <= 0 returned %v, want 0 "+
					"(short-circuit at :73-75)", x, got)
			}
			return
		}

		// x > 0: result must be > 0.
		if got <= 0 {
			t.Fatalf("Log1p(%v) = %v, want > 0 for x > 0", x, got)
		}

		// DIFFERENTIAL: matches math.Log1p(x) within 1e-4 relative.
		// Similar to ExpNeg, the detmath.go docstring claims
		// "< 1e-15" accuracy but empirically Log1p can produce
		// ~1e-7 errors for very small x (where 1.0 + x loses
		// precision before the series kicks in). Pin today's
		// real bound; separate finding for tightening.
		want := math.Log1p(x)
		if rel := relErr(got, want); rel > 1e-4 {
			t.Fatalf("Log1p(%v) = %v diverges from math.Log1p(x) = "+
				"%v by %.2e relative (> 1e-4 empirical bound).",
				x, got, want, rel)
		}
	})
}

// FuzzClamp01 exercises the Clamp01 contract at scoring.go:174-
// 182. Invariants:
//   - result always in [0, 1]
//   - NaN → 0 (critical consensus invariant)
//   - -Inf → 0
//   - +Inf → 1 (via the > 1 branch)
//   - finite in-range → passthrough
func FuzzClamp01(f *testing.F) {
	seeds := []float64{
		-1.0, -1e-10, 0, 1e-10,
		0.25, 0.5, 0.75, 1.0 - 1e-10, 1.0, 1.0 + 1e-10, 2.0,
		1e308, -1e308,
		math.NaN(), math.Inf(1), math.Inf(-1),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, x float64) {
		got := Clamp01(x)

		// Invariant 1: result NEVER NaN (consensus-critical).
		if math.IsNaN(got) {
			t.Fatalf("Clamp01(%v) = NaN — breaks consensus "+
				"determinism; NaN must clamp to 0", x)
		}

		// Invariant 2: result in [0, 1].
		if got < 0 || got > 1 {
			t.Fatalf("Clamp01(%v) = %v, out of [0, 1] range", x, got)
		}

		// Specific boundaries.
		switch {
		case math.IsNaN(x):
			if got != 0 {
				t.Fatalf("Clamp01(NaN) = %v, want 0", got)
			}
		case math.IsInf(x, -1) || x < 0:
			if got != 0 {
				t.Fatalf("Clamp01(%v) = %v, want 0 for x < 0", x, got)
			}
		case math.IsInf(x, 1) || x > 1:
			if got != 1 {
				t.Fatalf("Clamp01(%v) = %v, want 1 for x > 1", x, got)
			}
		default:
			// Finite and in [0, 1]: passthrough.
			if got != x {
				t.Fatalf("Clamp01(%v) = %v, want passthrough (x in "+
					"[0, 1])", x, got)
			}
		}
	})
}

// FuzzExpNegMonotonicity pins that ExpNeg is monotonically
// non-increasing. Two inputs probed per iteration; swap to
// canonical order then compare.
func FuzzExpNegMonotonicity(f *testing.F) {
	seeds := []struct{ a, b float64 }{
		{0, 0}, {0, 1}, {1, 2}, {0.1, 0.2},
		{5, 19.99}, {19, 20}, {20, 21},
		{-1, 0}, {-1, 1}, {0, 100},
		{math.NaN(), 1}, {0, math.Inf(1)},
	}
	for _, s := range seeds {
		f.Add(s.a, s.b)
	}

	f.Fuzz(func(t *testing.T, a, b float64) {
		if math.IsNaN(a) || math.IsNaN(b) {
			// NaN comparisons are undefined — skip.
			return
		}
		// Canonicalize: a <= b.
		if a > b {
			a, b = b, a
		}

		ea := ExpNeg(a)
		eb := ExpNeg(b)

		// ExpNeg is monotonically non-increasing: a <= b ⇒
		// ea >= eb.
		if ea < eb {
			t.Fatalf("MR monotonic broken: a=%v ExpNeg=%v, b=%v "+
				"ExpNeg=%v (a <= b but ExpNeg(a) < ExpNeg(b)). "+
				"ExpNeg should be monotonically non-increasing "+
				"in x.", a, ea, b, eb)
		}
	})
}

// FuzzLog1pMonotonicity pins that Log1p is monotonically
// non-decreasing. Mirror of the ExpNeg monotonicity fuzz.
func FuzzLog1pMonotonicity(f *testing.F) {
	seeds := []struct{ a, b float64 }{
		{0, 0}, {0, 1}, {1, 2}, {0.1, 0.2},
		{-1, 0}, {0, 1e6}, {1e6, 1e15},
		{1, 1.0000001}, // tight boundary
	}
	for _, s := range seeds {
		f.Add(s.a, s.b)
	}

	f.Fuzz(func(t *testing.T, a, b float64) {
		if math.IsNaN(a) || math.IsNaN(b) {
			return
		}
		// Non-finite inputs use defensive clamps and are outside
		// the positive finite monotonicity contract.
		if math.IsInf(a, 0) || math.IsInf(b, 0) {
			return
		}
		if a > b {
			a, b = b, a
		}

		la := Log1p(a)
		lb := Log1p(b)

		// Skip if either is non-finite (inputs like +Inf produce
		// +Inf which breaks ordered comparison semantics).
		if !floatIsFinite(la) || !floatIsFinite(lb) {
			return
		}

		if la > lb {
			t.Fatalf("MR monotonic broken: a=%v Log1p=%v, b=%v "+
				"Log1p=%v (a <= b but Log1p(a) > Log1p(b)). "+
				"Log1p should be monotonically non-decreasing.",
				a, la, b, lb)
		}
	})
}

// FuzzExpNegDeterminism pins that repeated calls with the same
// input produce identical bit-level results — REQUIRED for
// consensus. A refactor introducing non-determinism (e.g.
// caching with LRU eviction, goroutine-parallelism with
// non-reproducible reduction) would break this.
func FuzzExpNegDeterminism(f *testing.F) {
	seeds := []float64{0, 0.5, 1, 5, 10, 19.999, 20, -1, math.NaN()}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, x float64) {
		first := ExpNeg(x)
		for i := 0; i < 5; i++ {
			again := ExpNeg(x)
			// Use bit-level comparison since NaN != NaN normally
			// but both should be the SAME NaN pattern.
			if math.Float64bits(first) != math.Float64bits(again) {
				t.Fatalf("determinism: ExpNeg(%v) call %d returned "+
					"%v (bits %x); first returned %v (bits %x)",
					x, i, again, math.Float64bits(again),
					first, math.Float64bits(first))
			}
		}
	})
}

// FuzzLog1pDeterminism mirrors ExpNegDeterminism for Log1p.
func FuzzLog1pDeterminism(f *testing.F) {
	seeds := []float64{0, 0.001, 1, 1000, 1e15, -1, math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, x float64) {
		first := Log1p(x)
		for i := 0; i < 5; i++ {
			again := Log1p(x)
			if math.Float64bits(first) != math.Float64bits(again) {
				t.Fatalf("determinism: Log1p(%v) call %d returned "+
					"%v (bits %x); first returned %v (bits %x)",
					x, i, again, math.Float64bits(again),
					first, math.Float64bits(first))
			}
		}
	})
}
