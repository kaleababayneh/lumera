package types

import (
	"math"
	"testing"
	"time"
)

// This file adds BOUNDARY-VALUE fuzz targets for LockTTL.
// Complements the existing params_metamorphic_test.go fuzz
// suite (idempotence, upper bound, non-negative, identity-
// within-bounds). What those general relations do NOT pin:
//
//   - EXACT OUTPUT at boundary inputs. A refactor altering the
//     output formula while preserving "output <= max" would
//     slip past upper-bound checks (e.g. clamp = max/2 instead
//     of max). Identity-within-bounds is a weaker check at
//     equality.
//
//   - MONOTONIC RESPONSE across the full input space. Existing
//     relations don't test pairs of inputs.
//
//   - CROSS-PARAMETER INTERACTION when both default and max
//     vary. Existing fuzz varies them independently but doesn't
//     pin the specific fallback chain at each edge.
//
// Apply testing-fuzzing skill Archetype 5 (structure-aware):
// primitives packed as fuzz inputs map to valid (Params,
// Duration) configurations. Oracle uses boundary-specific
// expected outputs.

// FuzzLockTTL_ZeroRequestedReturnsDefaultExactly pins that
// requested=0 always returns params.DefaultLockTtlSeconds as
// a Duration (not clamped to some other value, not Min, not
// Max). The helper treats 0 as "unset" → uses Default.
func FuzzLockTTL_ZeroRequestedReturnsDefaultExactly(f *testing.F) {
	seeds := []uint32{120, 1, 3600, 0, 86400}
	for _, s := range seeds {
		f.Add(s, uint32(86400)) // (defaultTTL, maxTTL)
	}

	f.Fuzz(func(t *testing.T, defaultTTL, maxTTL uint32) {
		// Skip overflow-prone configs.
		if defaultTTL > 86400*365 || maxTTL > 86400*365 {
			return
		}
		// Skip configs where default > max — the helper
		// compensates via double-fallback; we want to pin the
		// "default returned when default valid" case.
		if defaultTTL > maxTTL || maxTTL == 0 {
			return
		}

		p := &Params{
			DefaultLockTtlSeconds: defaultTTL,
			MaxLockTtlSeconds:     maxTTL,
		}
		result := p.LockTTL(0)

		// When requested==0 AND defaultTTL>0, output MUST equal
		// defaultTTL in seconds.
		if defaultTTL > 0 {
			expected := time.Duration(defaultTTL) * time.Second
			if result != expected {
				t.Fatalf("zero-requested expected default=%v, got %v "+
					"(default=%d max=%d)", expected, result, defaultTTL,
					maxTTL)
			}
			return
		}

		// When defaultTTL==0 AND maxTTL>0, the helper falls
		// through to DefaultLockTTLSeconds package constant
		// (params.go:214-216). Pin that chain.
		expected := time.Duration(DefaultLockTTLSeconds) * time.Second
		// Also clamp to maxTTL if default > max (not this branch
		// since we guarded default > max above, but defensive).
		maxDur := time.Duration(maxTTL) * time.Second
		if expected > maxDur {
			expected = maxDur
		}
		if result != expected {
			t.Fatalf("zero-requested with default=0 expected "+
				"package default %v (clamped to %v), got %v. "+
				"Pins the params.go:214-216 second fallback.",
				time.Duration(DefaultLockTTLSeconds)*time.Second,
				expected, result)
		}
	})
}

// FuzzLockTTL_OverMaxReturnsMaxExactly pins that requested >
// maxTTL returns EXACTLY maxTTL (not min, not max/2, not some
// fraction). The clamp is hard, not rate-limited.
func FuzzLockTTL_OverMaxReturnsMaxExactly(f *testing.F) {
	seeds := []struct {
		defaultTTL, maxTTL uint32
		overBy             int64 // seconds over max
	}{
		{120, 3600, 1},          // 1s over
		{120, 3600, 3600},       // 2x max
		{120, 3600, 86400 * 30}, // 30 days over
		{1, 1, 1000000},         // tiny max, huge over
	}
	for _, s := range seeds {
		f.Add(s.defaultTTL, s.maxTTL, s.overBy)
	}

	f.Fuzz(func(t *testing.T, defaultTTL, maxTTL uint32, overBySec int64) {
		if defaultTTL > 86400*365 || maxTTL > 86400*365 {
			return
		}
		if maxTTL == 0 {
			return // skip the zero-max fallback case
		}
		// Normalize overBy to positive range avoiding overflow.
		if overBySec < 0 || overBySec > int64(math.MaxInt64/time.Second.Nanoseconds()/2) {
			return
		}

		p := &Params{
			DefaultLockTtlSeconds: defaultTTL,
			MaxLockTtlSeconds:     maxTTL,
		}

		maxDur := time.Duration(maxTTL) * time.Second
		requested := maxDur + time.Duration(overBySec)*time.Second
		// Guard against overflow on the addition.
		if requested < maxDur {
			return
		}

		result := p.LockTTL(requested)

		// Precondition: this is overflow-safe since we required
		// default <= max via the params construction. But if
		// default > max, the identity-within-bounds path wouldn't
		// apply. Skip that edge.
		if defaultTTL > maxTTL {
			return
		}

		if result != maxDur {
			t.Fatalf("over-max expected %v, got %v (requested=%v "+
				"max=%v default=%d). Pins the clamp-to-max "+
				"semantics — a refactor clamping to some other "+
				"value would produce a different output here.",
				maxDur, result, requested, maxDur, defaultTTL)
		}
	})
}

// FuzzLockTTL_NegativeRequestedReturnsDefaultExactly pins that
// negative requested values are treated the same as zero —
// returning the default. Complement to FuzzLockTTL_Zero...
func FuzzLockTTL_NegativeRequestedReturnsDefaultExactly(f *testing.F) {
	seeds := []struct {
		defaultTTL, maxTTL uint32
		negNs              int64
	}{
		{120, 3600, -1},
		{120, 3600, int64(-1 * time.Second)},
		{120, 3600, int64(-1 * time.Hour)},
		{1, 1, int64(-86400 * time.Second)},
	}
	for _, s := range seeds {
		f.Add(s.defaultTTL, s.maxTTL, s.negNs)
	}

	f.Fuzz(func(t *testing.T, defaultTTL, maxTTL uint32, negNs int64) {
		// Require input is actually negative.
		if negNs >= 0 {
			return
		}
		// Skip overflow-prone magnitudes.
		if negNs < int64(math.MinInt64/2) {
			return
		}
		if defaultTTL > 86400*365 || maxTTL > 86400*365 {
			return
		}
		if defaultTTL == 0 || maxTTL == 0 || defaultTTL > maxTTL {
			return
		}

		p := &Params{
			DefaultLockTtlSeconds: defaultTTL,
			MaxLockTtlSeconds:     maxTTL,
		}
		result := p.LockTTL(time.Duration(negNs))

		expected := time.Duration(defaultTTL) * time.Second
		if result != expected {
			t.Fatalf("negative-requested (%v) expected default=%v, "+
				"got %v. Pins params.go:211-213 — a refactor that "+
				"distinguished zero from negative (e.g. rejected "+
				"negatives with an error) would break this.",
				time.Duration(negNs), expected, result)
		}
	})
}

// FuzzLockTTL_MonotonicInRequestedBetweenZeroAndMax pins that
// for two in-bound requested values r1 <= r2, LockTTL(r1) <=
// LockTTL(r2). Within the in-bound range this is the identity,
// so r1 = r2 for the output — but the MR holds through the
// clamp too: if r1 is in-bound and r2 > max, result1 < result2
// = max (monotonic but not equal). Combined MR in both regimes.
func FuzzLockTTL_MonotonicAcrossFullRange(f *testing.F) {
	seeds := []struct {
		defaultTTL, maxTTL uint32
		r1Ns, r2Ns         int64
	}{
		{120, 3600, 100, 200},
		{120, 3600, 100, int64(time.Hour.Nanoseconds())},
		{120, 3600, int64(time.Hour.Nanoseconds()), int64(2 * time.Hour.Nanoseconds())}, // both over max
		{120, 3600, 0, 100},                                                             // from zero up
		{120, 3600, -1000, 100},                                                         // negative to positive
	}
	for _, s := range seeds {
		f.Add(s.defaultTTL, s.maxTTL, s.r1Ns, s.r2Ns)
	}

	f.Fuzz(func(t *testing.T, defaultTTL, maxTTL uint32, r1Ns, r2Ns int64) {
		if defaultTTL > 86400*365 || maxTTL > 86400*365 {
			return
		}
		if defaultTTL == 0 || maxTTL == 0 || defaultTTL > maxTTL {
			return
		}
		// Canonicalize r1 <= r2.
		if r1Ns > r2Ns {
			r1Ns, r2Ns = r2Ns, r1Ns
		}
		// Skip overflow-prone magnitudes.
		if r1Ns < int64(math.MinInt64/2) || r2Ns > int64(math.MaxInt64/2) {
			return
		}

		p := &Params{
			DefaultLockTtlSeconds: defaultTTL,
			MaxLockTtlSeconds:     maxTTL,
		}
		r1 := p.LockTTL(time.Duration(r1Ns))
		r2 := p.LockTTL(time.Duration(r2Ns))

		// MR: LockTTL is NON-DECREASING across input pairs where
		// both map to the IN-BOUND or CLAMP regime. The tricky
		// case is when r1 < 0 (falls to default) and r2 is
		// positive and in-bounds — then default vs r2 depends on
		// how r2 compares to default. The identity-in-bounds
		// property ensures r2 flows through; but default may be
		// LESS than r2.
		//
		// So the invariant simplifies to: r2 >= r1 ⇒ LockTTL(r2)
		// >= LockTTL(r1), EXCEPT when r1 <= 0 (falls back to
		// default which may exceed a small-positive r2).
		//
		// Skip the r1 <= 0, r2 > 0 case (mixed regime).
		if r1Ns <= 0 && r2Ns > 0 {
			return
		}

		if r2 < r1 {
			t.Fatalf("monotonicity violated: requested r1=%v r2=%v "+
				"(r1 <= r2) but LockTTL(r1)=%v > LockTTL(r2)=%v. "+
				"Pins NON-DECREASING response. default=%d max=%d",
				time.Duration(r1Ns), time.Duration(r2Ns), r1, r2,
				defaultTTL, maxTTL)
		}
	})
}

// FuzzLockTTL_ExactlyAtMaxReturnsMax pins the TIGHT upper
// boundary: requested == maxDur returns EXACTLY maxDur (not
// maxDur-1, not clamped as if over). The helper uses strict
// `> maxTTL` comparison at :217, so equality passes through.
func FuzzLockTTL_ExactlyAtMaxReturnsMax(f *testing.F) {
	seeds := []uint32{1, 120, 3600, 86400}
	for _, s := range seeds {
		f.Add(s) // maxTTL
	}

	f.Fuzz(func(t *testing.T, maxTTL uint32) {
		if maxTTL == 0 || maxTTL > 86400*365 {
			return
		}

		p := &Params{
			DefaultLockTtlSeconds: maxTTL, // default == max so both valid
			MaxLockTtlSeconds:     maxTTL,
		}
		exactlyMax := time.Duration(maxTTL) * time.Second
		result := p.LockTTL(exactlyMax)

		if result != exactlyMax {
			t.Fatalf("exactly-at-max: expected %v, got %v. Pins the "+
				"strict `> maxTTL` comparison at params.go:217 — a "+
				"refactor to `>=` would clamp equality cases, "+
				"shortening requested full-max TTLs by 1 second.",
				exactlyMax, result)
		}
	})
}

// FuzzLockTTL_UpperBoundHoldsUnderAllConfigs is a SAFETY fuzz
// target that pins the most fundamental invariant over the
// WIDEST input range: result is ALWAYS <= effectiveMax for
// ANY configuration. Single-parameter probe. Complements
// existing FuzzLockTTLClampingInvariants with a narrower
// contract (just the upper bound) over a wider seed space.
func FuzzLockTTL_UpperBoundAlwaysHolds(f *testing.F) {
	seeds := []struct {
		defaultTTL, maxTTL uint32
		reqNs              int64
	}{
		{0, 0, 0},
		{1, 1, 1},
		{120, 3600, int64(10 * time.Hour)},
		{86400, 86400, int64(time.Hour * 24 * 365)},
	}
	for _, s := range seeds {
		f.Add(s.defaultTTL, s.maxTTL, s.reqNs)
	}

	f.Fuzz(func(t *testing.T, defaultTTL, maxTTL uint32, reqNs int64) {
		if reqNs > int64(math.MaxInt64/2) || reqNs < int64(math.MinInt64/2) {
			return
		}
		if defaultTTL > 86400*365 || maxTTL > 86400*365 {
			return
		}

		p := &Params{
			DefaultLockTtlSeconds: defaultTTL,
			MaxLockTtlSeconds:     maxTTL,
		}
		result := p.LockTTL(time.Duration(reqNs))

		effectiveMax := time.Duration(maxTTL) * time.Second
		if effectiveMax <= 0 {
			effectiveMax = time.Duration(DefaultMaxLockTTLSeconds) * time.Second
		}

		if result > effectiveMax {
			t.Fatalf("upper bound violated: result=%v > effectiveMax"+
				"=%v (default=%d max=%d requested=%v)", result,
				effectiveMax, defaultTTL, maxTTL,
				time.Duration(reqNs))
		}
		if result < 0 {
			t.Fatalf("non-negative violated: result=%v < 0",
				result)
		}
	})
}
