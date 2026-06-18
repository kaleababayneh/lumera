
package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
)

// This file adds METAMORPHIC RELATION tests for
// CalculateBurnAmount at x/credits/keeper/math_safe.go:79-91.
// Existing tests pin conservation (burn + net == amount) and
// happy-path values. What they DO NOT pin:
//
//   1. METAMORPHIC LINEARITY in AMOUNT: doubling amount
//      doubles both burn AND remaining.
//   2. METAMORPHIC ADDITIVITY in BPS: for no-rounding inputs,
//      burn(total, a) + burn(total, b) == burn(total, a+b).
//   3. EXACT floor semantics on rounding (specific value for
//      known truncation, not just conservation).
//   4. MONOTONICITY in BPS: higher burnRateBPS → higher or
//      equal burn amount.
//
// Scan-angle #7 — testing-metamorphic skill. MRs pin EXACT
// equalities under input transformations that existing unit
// tests miss. Refactors that introduced clamping, saturation,
// or non-linear terms inside CalculateBurnAmount would pass
// conservation + happy-path assertions but fail these MRs.

// TestCalculateBurnAmount_MR_LinearInAmount pins MR1.
// Doubling amount doubles both burn and remaining exactly
// (no rounding at round-number inputs).
func TestCalculateBurnAmount_MR_LinearInAmount(t *testing.T) {
	t.Parallel()
	const rateBPS = 1500 // 15%
	small := math.NewInt(10_000)
	doubled := math.NewInt(20_000)

	burn1, net1, err := keeper.CalculateBurnAmount(small, rateBPS)
	require.NoError(t, err)
	burn2, net2, err := keeper.CalculateBurnAmount(doubled, rateBPS)
	require.NoError(t, err)

	// burn2 == 2 * burn1 (exact).
	assert.Equal(t, burn1.MulRaw(2).Int64(), burn2.Int64(),
		"MR1 — burn LINEAR in amount. Pins that SafePercentage "+
			"delegation preserves proportional response. A refactor "+
			"introducing a saturating cap inside CalculateBurnAmount "+
			"(rather than externally via ValidateRates bounds) would "+
			"break linearity.")
	assert.Equal(t, net1.MulRaw(2).Int64(), net2.Int64(),
		"MR1 — remaining LINEAR in amount (sibling of burn).")
}

// TestCalculateBurnAmount_MR_AdditiveInBPS pins MR2.
// When total * bps divides evenly, burn(total, a) +
// burn(total, b) equals burn(total, a+b).
func TestCalculateBurnAmount_MR_AdditiveInBPS(t *testing.T) {
	t.Parallel()
	// Use total=10000 so every basis-point yields an integer
	// value (10000 * N / 10000 = N — no rounding).
	const total = 10_000
	rateA := uint32(2000) // 20%
	rateB := uint32(3000) // 30%
	// Combined: 50%.

	burnA, _, err := keeper.CalculateBurnAmount(math.NewInt(total), rateA)
	require.NoError(t, err)
	burnB, _, err := keeper.CalculateBurnAmount(math.NewInt(total), rateB)
	require.NoError(t, err)
	burnCombined, _, err := keeper.CalculateBurnAmount(math.NewInt(total), rateA+rateB)
	require.NoError(t, err)

	assert.Equal(t, burnA.Add(burnB).Int64(), burnCombined.Int64(),
		"MR2 — burn additive in bps when total is divisible. "+
			"burn(10000, 2000) + burn(10000, 3000) = burn(10000, "+
			"5000). Pins the per-bps computation is additive — a "+
			"refactor adding a non-linear term (e.g., bps^2 / "+
			"10000) would break this.")
}

// TestCalculateBurnAmount_MR_MonotonicInBPS pins MR3.
// Higher burnRateBPS produces >= burn amount (never less),
// for the same total.
func TestCalculateBurnAmount_MR_MonotonicInBPS(t *testing.T) {
	t.Parallel()
	total := math.NewInt(1_000_000)

	prev := math.ZeroInt()
	for _, bps := range []uint32{0, 100, 500, 1000, 2500, 5000, 7500, 9999, 10000} {
		burn, _, err := keeper.CalculateBurnAmount(total, bps)
		require.NoError(t, err, "bps=%d", bps)
		assert.True(t, burn.GTE(prev),
			"MR3 — burn non-decreasing in bps. At bps=%d, burn=%s, "+
				"prev=%s. Pins monotonicity: a refactor that "+
				"introduced a non-monotonic transform (e.g., "+
				"saturating caps with different thresholds) would "+
				"produce a dip that breaks the 'higher rate → more "+
				"burned' intuition operators rely on for policy "+
				"tuning.", bps, burn.String(), prev.String())
		prev = burn
	}
}

// TestCalculateBurnAmount_FloorSemantics pins EXACT floor
// behavior for a rounding case. total=333, bps=100 (1%) →
// burn = floor(333 * 100 / 10000) = floor(3.33) = 3;
// remaining = 333 - 3 = 330. Existing rounding test only
// pins conservation; this pins the per-component values.
func TestCalculateBurnAmount_FloorSemanticsExact(t *testing.T) {
	t.Parallel()
	burn, net, err := keeper.CalculateBurnAmount(math.NewInt(333), 100)
	require.NoError(t, err)
	assert.Equal(t, int64(3), burn.Int64(),
		"CRITICAL — burn uses FLOOR truncation: floor(333 * 100 / "+
			"10000) = floor(3.33) = 3. Pins against a refactor to "+
			"ROUND (which would produce 3, same here) — but a "+
			"refactor to CEIL would produce 4 (3.33→4), which "+
			"would inflate burn by 33%% for tiny amounts.")
	assert.Equal(t, int64(330), net.Int64(),
		"remaining = 333 - 3 = 330 (leftover rounding goes to "+
			"net). Pins the 'burn-first, remaining-second' order "+
			"at :80-85: a refactor computing remaining first would "+
			"produce floor-to-floor where the residual could go "+
			"either direction.")
}

// TestCalculateBurnAmount_FloorProducesZeroForDustAmounts
// pins a subtle edge: when total * bps < 10000, floor truncation
// produces burn=0. Important for dust-amount settlement policy.
func TestCalculateBurnAmount_DustAmountsProduceZeroBurn(t *testing.T) {
	t.Parallel()
	// total=10, bps=100 (1%) → burn = floor(10 * 100 / 10000) =
	// floor(0.1) = 0. Entire amount goes to net.
	burn, net, err := keeper.CalculateBurnAmount(math.NewInt(10), 100)
	require.NoError(t, err)
	assert.Equal(t, int64(0), burn.Int64(),
		"dust amount (10 * 1% = 0.1 trunc to 0) → zero burn. "+
			"Pins that floor truncation correctly rounds down to "+
			"zero on sub-basis-point amounts.")
	assert.Equal(t, int64(10), net.Int64(),
		"entire amount preserved as net when burn rounds to 0")

	// Conservation still holds: burn + net == total = 10.
	assert.Equal(t, int64(10), burn.Add(net).Int64())
}

// TestCalculateBurnAmount_MR_ZeroAmountProducesZeroShares pins
// the degenerate zero-amount path.
func TestCalculateBurnAmount_ZeroAmountPath(t *testing.T) {
	t.Parallel()
	for _, bps := range []uint32{0, 100, 5000, 10000} {
		burn, net, err := keeper.CalculateBurnAmount(math.ZeroInt(), bps)
		require.NoError(t, err, "bps=%d should not error on zero amount", bps)
		assert.True(t, burn.IsZero(), "bps=%d: burn is zero", bps)
		assert.True(t, net.IsZero(), "bps=%d: net is zero", bps)
	}
}

// TestCalculateBurnAmount_MR_ConservationHoldsAcrossRounding
// pins that burn + remaining == total for a range of rounding-
// triggering inputs.
func TestCalculateBurnAmount_ConservationAcrossManyInputs(t *testing.T) {
	t.Parallel()
	for _, total := range []int64{1, 3, 7, 333, 1001, 9999, 10001, 1_000_000_001} {
		for _, bps := range []uint32{0, 1, 100, 333, 2500, 5000, 7500, 9999, 10000} {
			burn, net, err := keeper.CalculateBurnAmount(math.NewInt(total), bps)
			require.NoError(t, err, "total=%d bps=%d", total, bps)
			assert.Equal(t, total, burn.Add(net).Int64(),
				"CONSERVATION: total=%d, bps=%d, burn=%s, net=%s — "+
					"sum must equal total across all rounding. Pins "+
					"against a refactor where floor truncation in "+
					"SafePercentage combined with a non-compensating "+
					"SafeSubtract could create or destroy value.",
				total, bps, burn.String(), net.String())
		}
	}
}

// TestCalculateBurnAmount_BPSAtMaxBoundary pins the :244-245
// existing boundary case (bps=10001 rejected) explicitly as a
// floor-complementary anchor. bps=10000 exactly is accepted;
// bps=10001 triggers SafePercentage's bounds guard.
func TestCalculateBurnAmount_BPSAtMaxBoundary(t *testing.T) {
	t.Parallel()
	// bps=10000 exactly accepted (100% burn).
	burn, net, err := keeper.CalculateBurnAmount(math.NewInt(500), 10000)
	require.NoError(t, err)
	assert.Equal(t, int64(500), burn.Int64())
	assert.Equal(t, int64(0), net.Int64())

	// bps=10001 rejected.
	_, _, err = keeper.CalculateBurnAmount(math.NewInt(500), 10001)
	require.Error(t, err,
		"bps=10001 rejected via SafePercentage's underlying guard")
}
