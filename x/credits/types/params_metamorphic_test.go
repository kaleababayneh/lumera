//go:build cosmos

package types

import (
	"math"
	"testing"
	"time"
)

// FuzzLockTTLClampingInvariants pins oracle-free metamorphic relations for
// the LockTTL clamping helper. These relations must hold regardless of the
// specific parameter values or requested duration:
//
//  1. Idempotence: LockTTL(LockTTL(x)) == LockTTL(x) for any x. Applying
//     clamping twice must yield the same result as applying it once.
//
//  2. Upper bound: LockTTL(any) <= effectiveMax, where effectiveMax is
//     MaxLockTtlSeconds (or DefaultMaxLockTTLSeconds if zero).
//
//  3. Non-negative: LockTTL(any) >= 0. The helper must never return a
//     negative duration.
//
//  4. Identity within bounds: if 0 < requested <= effectiveMax, then
//     LockTTL(requested) == requested. Values within bounds pass through.
func FuzzLockTTLClampingInvariants(f *testing.F) {
	seeds := []struct {
		defaultTTL, maxTTL uint32
		requested          int64 // nanoseconds
	}{
		{120, 3600, 0},                          // zero request
		{120, 3600, int64(60 * time.Second)},    // within bounds
		{120, 3600, int64(2 * time.Hour)},       // exceeds max
		{120, 3600, int64(-1 * time.Second)},    // negative
		{0, 0, int64(5 * time.Minute)},          // zero params (fallbacks)
		{300, 600, int64(600 * time.Second)},    // exactly at max
		{120, 3600, int64(120 * time.Second)},   // exactly at default
		{1, 1, int64(10 * time.Second)},         // tiny max
		{3600, 7200, int64(7200 * time.Second)}, // exactly at large max
	}
	for _, s := range seeds {
		f.Add(s.defaultTTL, s.maxTTL, s.requested)
	}

	f.Fuzz(func(t *testing.T, defaultTTL, maxTTL uint32, requestedNs int64) {
		// Skip pathological inputs that would overflow duration.
		if requestedNs > int64(math.MaxInt64/2) || requestedNs < int64(math.MinInt64/2) {
			return
		}
		// Cap TTL params to reasonable values to avoid overflow.
		if defaultTTL > 86400*365 || maxTTL > 86400*365 {
			return
		}

		p := &Params{
			DefaultLockTtlSeconds: defaultTTL,
			MaxLockTtlSeconds:     maxTTL,
		}
		requested := time.Duration(requestedNs)

		result := p.LockTTL(requested)

		// Compute effective max (the helper's fallback logic).
		effectiveMax := time.Duration(maxTTL) * time.Second
		if effectiveMax <= 0 {
			effectiveMax = time.Duration(DefaultMaxLockTTLSeconds) * time.Second
		}

		// --- Relation 1: Idempotence ---
		secondPass := p.LockTTL(result)
		if result != secondPass {
			t.Fatalf("idempotence violated: LockTTL(%v)=%v, LockTTL(LockTTL(%v))=%v",
				requested, result, requested, secondPass)
		}

		// --- Relation 2: Upper bound ---
		if result > effectiveMax {
			t.Fatalf("upper bound violated: LockTTL(%v)=%v > effectiveMax=%v",
				requested, result, effectiveMax)
		}

		// --- Relation 3: Non-negative ---
		if result < 0 {
			t.Fatalf("non-negative violated: LockTTL(%v)=%v < 0",
				requested, result)
		}

		// --- Relation 4: Identity within bounds ---
		if requested > 0 && requested <= effectiveMax {
			if result != requested {
				t.Fatalf("identity violated: 0 < requested=%v <= effectiveMax=%v, but LockTTL=%v != requested",
					requested, effectiveMax, result)
			}
		}
	})
}

// FuzzLockTTLNilParamsInvariant pins that nil Params returns the package
// default rather than panicking, for any requested duration.
func FuzzLockTTLNilParamsInvariant(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(60 * time.Second))
	f.Add(int64(-1 * time.Second))
	f.Add(int64(time.Hour))

	f.Fuzz(func(t *testing.T, requestedNs int64) {
		if requestedNs > int64(math.MaxInt64/2) || requestedNs < int64(math.MinInt64/2) {
			return
		}
		var p *Params
		requested := time.Duration(requestedNs)

		result := p.LockTTL(requested)
		expected := time.Duration(DefaultLockTTLSeconds) * time.Second

		// Nil params always returns the package default, regardless of request.
		if result != expected {
			t.Fatalf("nil params invariant violated: LockTTL(%v)=%v, expected=%v",
				requested, result, expected)
		}
	})
}

// FuzzParamsValidateBPSBoundConsistency pins the metamorphic relation that
// valid BPS parameters form a consistent partial order:
//
//	MinBurnRateSpendBps <= BurnRateSpendBps <= MaxBurnRateSpendBps
//	MinBurnRateSpendBps <= DeathSpiralBurnRateCapBps <= MaxBurnRateSpendBps
//
// This test verifies that:
//  1. Parameters satisfying these constraints validate successfully.
//  2. Parameters violating min > max or burn rate outside bounds fail.
//  3. The validation logic is order-consistent (transitivity holds).
func FuzzParamsValidateBPSBoundConsistency(f *testing.F) {
	// Seeds: (min, burn, max, deathCap) in BPS
	seeds := []struct{ min, burn, max, deathCap uint32 }{
		{50, 300, 1000, 150},  // defaults
		{0, 0, 0, 0},          // zeros
		{100, 100, 100, 100},  // all equal
		{0, 500, 10000, 5000}, // wide range
		{500, 300, 1000, 150}, // burn below min (invalid)
		{50, 1500, 1000, 150}, // burn above max (invalid)
		{500, 600, 400, 450},  // min > max (invalid)
		{50, 300, 1000, 30},   // deathCap below min (invalid)
		{50, 300, 1000, 2000}, // deathCap above max (invalid)
	}
	for _, s := range seeds {
		f.Add(s.min, s.burn, s.max, s.deathCap)
	}

	f.Fuzz(func(t *testing.T, minBps, burnBps, maxBps, deathCapBps uint32) {
		// Skip values that would trigger the "exceeds MaxBasisPoints" errors
		// so we can focus on the inter-field ordering constraints.
		if minBps > MaxBasisPoints || burnBps > MaxBasisPoints ||
			maxBps > MaxBasisPoints || deathCapBps > MaxBasisPoints {
			return
		}

		p := DefaultParams()
		p.MinBurnRateSpendBps = minBps
		p.BurnRateSpendBps = burnBps
		p.MaxBurnRateSpendBps = maxBps
		p.DeathSpiralBurnRateCapBps = deathCapBps
		// Enable the BurnRateAdjustmentEpoch to activate bound checking.
		p.BurnRateAdjustmentEpoch = 100

		// Ensure BurnRateSpendBps + InsuranceBps <= MaxBasisPoints.
		// InsuranceBps defaults to 300; adjust if needed.
		if uint64(burnBps)+uint64(p.InsuranceBps) > MaxBasisPoints {
			p.InsuranceBps = 0
		}

		err := p.Validate()

		// Compute expected validity based on the ordering constraints.
		minMaxOK := minBps <= maxBps
		burnInBounds := minBps <= burnBps && burnBps <= maxBps
		deathCapInBounds := minBps <= deathCapBps && deathCapBps <= maxBps

		expectedValid := minMaxOK && burnInBounds && deathCapInBounds

		// --- Relation: Validity matches constraint satisfaction ---
		if expectedValid && err != nil {
			t.Fatalf("expected valid params to pass: min=%d burn=%d max=%d deathCap=%d; got err=%v",
				minBps, burnBps, maxBps, deathCapBps, err)
		}
		if !expectedValid && err == nil {
			t.Fatalf("expected invalid params to fail: min=%d burn=%d max=%d deathCap=%d; minMaxOK=%v burnInBounds=%v deathCapInBounds=%v",
				minBps, burnBps, maxBps, deathCapBps, minMaxOK, burnInBounds, deathCapInBounds)
		}
	})
}

// FuzzParamsValidateBurnInsuranceSum pins the metamorphic relation that
// BurnRateSpendBps + InsuranceBps must not exceed MaxBasisPoints (10000).
// This ensures total deductions cannot exceed 100% of a settlement.
func FuzzParamsValidateBurnInsuranceSum(f *testing.F) {
	seeds := []struct{ burn, insurance uint32 }{
		{300, 300},   // valid: 600 total
		{5000, 5000}, // valid: 10000 total (exactly at limit)
		{5001, 5000}, // invalid: 10001 total
		{9999, 1},    // valid: 10000 total
		{9999, 2},    // invalid: 10001 total
		{0, 0},       // valid: 0 total
		{10000, 0},   // valid: 10000 total
		{0, 10000},   // valid: 10000 total
	}
	for _, s := range seeds {
		f.Add(s.burn, s.insurance)
	}

	f.Fuzz(func(t *testing.T, burnBps, insuranceBps uint32) {
		// Skip individual values exceeding max (separate validation).
		if burnBps > MaxBasisPoints || insuranceBps > MaxBasisPoints {
			return
		}

		p := DefaultParams()
		p.BurnRateSpendBps = burnBps
		p.InsuranceBps = insuranceBps
		// Ensure burn rate is within min/max bounds for valid epoch params.
		p.MinBurnRateSpendBps = 0
		p.MaxBurnRateSpendBps = MaxBasisPoints
		p.DeathSpiralBurnRateCapBps = burnBps // keep in bounds
		if p.DeathSpiralBurnRateCapBps > MaxBasisPoints {
			p.DeathSpiralBurnRateCapBps = MaxBasisPoints
		}
		p.BurnRateAdjustmentEpoch = 0 // disable epoch-specific checks

		err := p.Validate()

		sum := uint64(burnBps) + uint64(insuranceBps)
		expectedValid := sum <= MaxBasisPoints

		if expectedValid && err != nil {
			t.Fatalf("expected valid sum=%d (burn=%d + insurance=%d) to pass; got err=%v",
				sum, burnBps, insuranceBps, err)
		}
		if !expectedValid && err == nil {
			t.Fatalf("expected invalid sum=%d (burn=%d + insurance=%d) to fail; got nil error",
				sum, burnBps, insuranceBps)
		}
	})
}

// FuzzParamsValidateTTLOrdering pins the metamorphic relation that
// DefaultLockTtlSeconds must not exceed MaxLockTtlSeconds when both
// are positive. This ensures the default is always reachable.
func FuzzParamsValidateTTLOrdering(f *testing.F) {
	seeds := []struct{ defaultTTL, maxTTL uint32 }{
		{120, 3600},  // valid: default < max
		{3600, 3600}, // valid: equal
		{5000, 3000}, // invalid: default > max
		{1, 1},       // valid: equal at minimum
		{0, 100},     // invalid: default is zero
		{100, 0},     // invalid: max is zero
	}
	for _, s := range seeds {
		f.Add(s.defaultTTL, s.maxTTL)
	}

	f.Fuzz(func(t *testing.T, defaultTTL, maxTTL uint32) {
		p := DefaultParams()
		p.DefaultLockTtlSeconds = defaultTTL
		p.MaxLockTtlSeconds = maxTTL

		err := p.Validate()

		// Expected validity: both positive AND default <= max.
		bothPositive := defaultTTL > 0 && maxTTL > 0
		orderOK := defaultTTL <= maxTTL
		expectedValid := bothPositive && orderOK

		if expectedValid && err != nil {
			t.Fatalf("expected valid TTL (default=%d max=%d) to pass; got err=%v",
				defaultTTL, maxTTL, err)
		}
		if !expectedValid && err == nil {
			t.Fatalf("expected invalid TTL (default=%d max=%d) to fail; bothPositive=%v orderOK=%v",
				defaultTTL, maxTTL, bothPositive, orderOK)
		}
	})
}

// FuzzDisputeWindowDurationFallback pins the metamorphic relation for
// DisputeWindowDuration:
//
//  1. Zero hours -> canonical registry default
//  2. Positive hours -> hours * time.Hour
//  3. Nil params -> canonical registry default
//
// The key invariant is: either you get the override OR the default,
// never a mix or undefined behavior.
func FuzzDisputeWindowDurationFallback(f *testing.F) {
	f.Add(uint32(0))
	f.Add(uint32(1))
	f.Add(uint32(24))
	f.Add(uint32(168)) // week

	f.Fuzz(func(t *testing.T, hours uint32) {
		// Cap to reasonable values.
		if hours > 8760 { // 1 year
			return
		}

		p := DefaultParams()
		p.DisputeWindowHours = hours

		result := DisputeWindowDuration(p)
		canonical := DefaultDisputeWindowDuration()

		if hours == 0 {
			// Zero hours must fall through to canonical default.
			if result != canonical {
				t.Fatalf("zero hours fallback violated: got=%v expected=%v",
					result, canonical)
			}
		} else {
			// Positive hours must return hours override.
			expected := time.Duration(hours) * time.Hour
			if result != expected {
				t.Fatalf("positive hours override violated: hours=%d got=%v expected=%v",
					hours, result, expected)
			}
		}

		// Also verify nil params returns canonical.
		nilResult := DisputeWindowDuration(nil)
		if nilResult != canonical {
			t.Fatalf("nil params must return canonical; got=%v expected=%v",
				nilResult, canonical)
		}
	})
}
