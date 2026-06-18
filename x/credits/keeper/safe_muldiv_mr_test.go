//go:build cosmos && cosmos_full

package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
)

// This file adds METAMORPHIC RELATION tests + FLOOR-SEMANTICS
// tests for SafeMulDiv at x/credits/keeper/math_safe.go:18-40.
// Existing TestSafeMulDiv covers 7 scenarios including bounds
// rejection + large-amount handling. What's NOT pinned:
//
//   1. MR LINEAR IN AMOUNT: SafeMulDiv(2a, r, s) == 2 * SafeMulDiv(a, r, s)
//   2. MR INVERSE IN SCALE: SafeMulDiv(a, r, 2s) == SafeMulDiv(a, r, s) / 2
//   3. MR LINEAR IN RATE: SafeMulDiv(a, 2r, s) == 2 * SafeMulDiv(a, r, s)
//   4. EXACT FLOOR SEMANTICS for non-divisible inputs
//   5. NEGATIVE SCALE rejected (existing test only covers zero)
//   6. NEGATIVE AMOUNT behavior (SafeMulDiv signature accepts
//      sdkmath.Int which CAN be negative; existing tests don't
//      cover this path — pin the documented behavior)
//   7. IDENTITY: SafeMulDiv(a, 1, 1) == a; SafeMulDiv(a, s, s) == a
//
// Scan-angle #7 (testing-metamorphic skill). Refactors
// introducing a saturating cap or non-linear term inside
// SafeMulDiv would satisfy the bounds-rejection tests but
// break the linearity MRs.
//
// Scan-angle #1 (watchdog comparisons + tiebreak arms) on
// the FOUR guards at :19-28. Three use distinct operators:
//   rate < 0       (strict less-than)
//   scale <= 0     (less-than-or-equal — both 0 AND negative
//                  rejected)
//   rate > scale   (strict greater-than — rate == scale
//                  accepted, pinned as 'full rate')

// ---- Metamorphic relations ----

// TestSafeMulDiv_MR_LinearInAmount pins that doubling amount
// doubles the result (exact equality, not bound).
func TestSafeMulDiv_MR_LinearInAmount(t *testing.T) {
	t.Parallel()
	r1, err := keeper.SafeMulDiv(math.NewInt(10_000), 1500, 10_000)
	require.NoError(t, err)
	r2, err := keeper.SafeMulDiv(math.NewInt(20_000), 1500, 10_000)
	require.NoError(t, err)

	assert.Equal(t, r1.MulRaw(2).Int64(), r2.Int64(),
		"MR — SafeMulDiv LINEAR in amount. Pins the `amount.MulRaw"+
			"(rate)` term: a refactor that introduced a saturating "+
			"cap inside (rather than via the rate<=scale guard) "+
			"would break linearity.")
}

// TestSafeMulDiv_MR_LinearInRate pins that doubling rate
// doubles the result (for inputs where no rounding kicks in).
func TestSafeMulDiv_MR_LinearInRate(t *testing.T) {
	t.Parallel()
	r1, err := keeper.SafeMulDiv(math.NewInt(10_000), 1000, 10_000)
	require.NoError(t, err)
	r2, err := keeper.SafeMulDiv(math.NewInt(10_000), 2000, 10_000)
	require.NoError(t, err)

	assert.Equal(t, r1.MulRaw(2).Int64(), r2.Int64(),
		"MR — SafeMulDiv LINEAR in rate.")
}

// TestSafeMulDiv_MR_InverseInScale pins that doubling scale
// halves the result. SafeMulDiv(a, r, 2s) == SafeMulDiv(a, r,
// s) / 2 when s divides a*r evenly.
func TestSafeMulDiv_MR_InverseInScale(t *testing.T) {
	t.Parallel()
	// a*r = 10000 * 4000 = 40_000_000. Divisible by both 10000
	// and 20000.
	rSmall, err := keeper.SafeMulDiv(math.NewInt(10_000), 4000, 10_000)
	require.NoError(t, err)
	rLarge, err := keeper.SafeMulDiv(math.NewInt(10_000), 4000, 20_000)
	require.NoError(t, err)

	// rLarge scale is 2x → result is rSmall/2.
	expected := rSmall.QuoRaw(2)
	assert.Equal(t, expected.Int64(), rLarge.Int64(),
		"MR — SafeMulDiv INVERSE in scale. Pins the final Quo"+
			"Raw(scale) — the fuzz test comment at math_safe_fuzz_"+
			"test.go warns about dropped-Quo regressions which this "+
			"MR catches at specific input values.")
}

// TestSafeMulDiv_MR_Identity pins multiple identity cases.
func TestSafeMulDiv_MR_IdentityCases(t *testing.T) {
	t.Parallel()
	// a * 1 / 1 == a
	r, err := keeper.SafeMulDiv(math.NewInt(12345), 1, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(12345), r.Int64(),
		"IDENTITY: SafeMulDiv(a, 1, 1) == a")

	// a * scale / scale == a (rate == scale full-rate)
	r2, err := keeper.SafeMulDiv(math.NewInt(12345), 10000, 10000)
	require.NoError(t, err)
	assert.Equal(t, int64(12345), r2.Int64(),
		"IDENTITY: SafeMulDiv(a, s, s) == a (full-rate path)")
}

// ---- Floor semantics ----

// TestSafeMulDiv_FloorSemanticsExact pins EXACT floor behavior
// for non-divisible inputs. Existing tests only pin divisible
// cases where rounding doesn't kick in.
func TestSafeMulDiv_FloorSemanticsExact(t *testing.T) {
	t.Parallel()
	// amount=7, rate=100, scale=10000: 7*100/10000 = 700/10000
	// = 0 (floor). The entire computation truncates to zero.
	r, err := keeper.SafeMulDiv(math.NewInt(7), 100, 10000)
	require.NoError(t, err)
	assert.Equal(t, int64(0), r.Int64(),
		"amount=7, rate=100, scale=10000: floor(0.07) = 0. "+
			"Pins against CEIL/ROUND refactors that would change "+
			"dust-amount behavior.")

	// amount=333, rate=1, scale=100: 333/100 = 3 (floor of 3.33)
	r2, err := keeper.SafeMulDiv(math.NewInt(333), 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(3), r2.Int64(),
		"floor(333/100) = 3 (not 4 ceil, not 3 round)")

	// amount=500, rate=3, scale=10: 500*3/10 = 1500/10 = 150 exact
	r3, err := keeper.SafeMulDiv(math.NewInt(500), 3, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(150), r3.Int64())
}

// TestSafeMulDiv_ZeroAmount pins the zero-amount edge.
func TestSafeMulDiv_ZeroAmount(t *testing.T) {
	t.Parallel()
	for _, rate := range []int64{0, 100, 10000} {
		r, err := keeper.SafeMulDiv(math.ZeroInt(), rate, 10000)
		require.NoError(t, err, "rate=%d", rate)
		assert.True(t, r.IsZero(), "rate=%d: zero amount → zero result", rate)
	}
}

// ---- Scan-angle #1 boundary guards ----

// TestSafeMulDiv_NegativeScaleRejected pins the scale <= 0
// guard for NEGATIVE values. Existing test only covers
// scale=0; a refactor to `scale == 0` would let negative
// scales through.
func TestSafeMulDiv_NegativeScaleRejected(t *testing.T) {
	t.Parallel()
	for _, negScale := range []int64{-1, -100, -10000} {
		_, err := keeper.SafeMulDiv(math.NewInt(1000), 100, negScale)
		require.Error(t, err,
			"negative scale %d rejected. Pins :24 `<= 0` guard "+
				"covers BOTH zero and negative. A refactor to `== 0` "+
				"would let negative scales produce negative quotients "+
				"(or panic on QuoRaw with negative divisor).", negScale)
		assert.Contains(t, err.Error(), "scale must be positive")
	}
}

// TestSafeMulDiv_RateEqualScaleAccepted pins the strict `>`
// rate-cap. Rate == scale is ACCEPTED (full-rate = 100%
// conversion).
func TestSafeMulDiv_RateEqualScaleAccepted(t *testing.T) {
	t.Parallel()
	// rate=10000, scale=10000: accepted (100% conversion).
	r, err := keeper.SafeMulDiv(math.NewInt(500), 10000, 10000)
	require.NoError(t, err)
	assert.Equal(t, int64(500), r.Int64(),
		"rate == scale accepted → result == amount. Pins :27 "+
			"strict `> scale` guard: a refactor to `>=` would "+
			"reject the full-rate path used by 100%-burn and "+
			"all-royalty-to-origin configurations.")

	// Small scale: rate=5, scale=5.
	r2, err := keeper.SafeMulDiv(math.NewInt(42), 5, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(42), r2.Int64(),
		"rate==scale==5: result == amount")
}

// TestSafeMulDiv_RateJustOverScaleRejected pins the mirror
// rejection case. rate = scale + 1 → error.
func TestSafeMulDiv_RateJustOverScaleRejected(t *testing.T) {
	t.Parallel()
	_, err := keeper.SafeMulDiv(math.NewInt(1000), 10001, 10000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate cannot exceed scale",
		"rate=10001 vs scale=10000 rejected. Pins :27 strict "+
			"`>` comparison paired with :26 test-case.")
}

// TestSafeMulDiv_NegativeAmountBehavior pins the documented
// behavior for negative amounts. sdkmath.Int.MulRaw on a
// negative value produces a negative product; QuoRaw preserves
// sign. SafeMulDiv does NOT guard against negative amount, so
// it passes through. Pinned so a refactor that ADDED a
// positive-only guard is deliberate.
func TestSafeMulDiv_NegativeAmountPassesThroughWithNegativeResult(t *testing.T) {
	t.Parallel()
	r, err := keeper.SafeMulDiv(math.NewInt(-1000), 2500, 10000)
	require.NoError(t, err,
		"negative amount is NOT rejected by SafeMulDiv. Pins the "+
			"absence of a positive-amount guard: callers that rely "+
			"on NEGATIVE sign to represent refunds (e.g., burn "+
			"reversals) depend on this behavior. A refactor adding "+
			"a `amount.IsNegative()` rejection would need to update "+
			"every such caller.")
	assert.Equal(t, int64(-250), r.Int64(),
		"-1000 * 2500 / 10000 = -250 (sign preserved)")
}

// ---- Robustness across many inputs ----

// TestSafeMulDiv_MR_ConservationWithAddSubCombined pins a
// composition MR: for partition rate = a + b where a+b <=
// scale, SafeMulDiv(amount, a, scale) + SafeMulDiv(amount, b,
// scale) == SafeMulDiv(amount, a+b, scale) when no rounding.
func TestSafeMulDiv_MR_AdditiveInRateWhenDivisible(t *testing.T) {
	t.Parallel()
	// amount=10000 — any rate produces integer quotients.
	a, b := int64(3000), int64(4000)
	combined := a + b

	rA, err := keeper.SafeMulDiv(math.NewInt(10_000), a, 10000)
	require.NoError(t, err)
	rB, err := keeper.SafeMulDiv(math.NewInt(10_000), b, 10000)
	require.NoError(t, err)
	rCombined, err := keeper.SafeMulDiv(math.NewInt(10_000), combined, 10000)
	require.NoError(t, err)

	assert.Equal(t, rA.Add(rB).Int64(), rCombined.Int64(),
		"MR ADDITIVE: SafeMulDiv(a,3000,10000) + SafeMulDiv(a,"+
			"4000,10000) == SafeMulDiv(a,7000,10000). Pins per-rate "+
			"additivity (no non-linear cross-term).")
}

// TestSafeMulDiv_LargeAmountPreservesLinearity pins that the
// very-large-amount path (MaxUint64/2 * 50%) from the existing
// test ALSO respects MR linearity.
func TestSafeMulDiv_LargeAmountLinearityPreserved(t *testing.T) {
	t.Parallel()
	// amount=2^60 (large but well below math.Int limits).
	base, _ := math.NewIntFromString("1152921504606846976")  // 2^60
	doubled, _ := math.NewIntFromString("2305843009213693952") // 2^61

	r1, err := keeper.SafeMulDiv(base, 1500, 10000)
	require.NoError(t, err)
	r2, err := keeper.SafeMulDiv(doubled, 1500, 10000)
	require.NoError(t, err)

	// r2 should equal 2 * r1 (exact — no rounding at these
	// round-power-of-2 inputs).
	expected := r1.MulRaw(2)
	assert.True(t, r2.Equal(expected),
		"large-amount linearity preserved (r1=%s, r2=%s, "+
			"expected 2*r1=%s). Pins that the big.Int backing "+
			"of math.Int has no overflow threshold below "+
			"production-relevant amounts.",
		r1.String(), r2.String(), expected.String())
}
