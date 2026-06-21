package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
)

// This file closes remaining DIRECT-test coverage for
// SafePercentage at x/credits/keeper/math_safe.go:42-52.
// Existing TestSafePercentage + FuzzSafePercentage cover
// happy paths + conservation but DO NOT pin:
//
//   1. COMPOSITION with SafeMulDiv — SafePercentage is a thin
//      wrapper that calls SafeMulDiv(amount, int64(bps), 10000)
//      after bounds-checking. Pin that the results match.
//   2. UINT32 EXTREME input (> 10000 but within uint32 range)
//      safely rejected without wrap/panic.
//   3. Error message echoes the offending BPS for triage.
//   4. Negative amount pass-through (via SafeMulDiv delegation).
//
// Scan-angle #3 (hidden-secondary-return pinning) on the
// COMPOSITION. SafePercentage is a façade over SafeMulDiv —
// a refactor that inlined the computation and dropped the
// bps-bound guard would bypass the `>10000` reject at :43-47
// while still passing bounds-only unit tests that test the
// façade's public input contract.
//
// Scan-angle #1 (watchdog comparisons + tiebreak arms) on
// the `basisPoints > 10000` strict guard. Pin that 10000
// exactly is accepted; 10001 rejected; max-uint32 rejected
// with a SAFE error (not a wrap that would silently behave
// like a smaller value).

// TestSafePercentage_EquivalentToSafeMulDivWith10000Scale pins
// the composition contract: SafePercentage(a, bps) ==
// SafeMulDiv(a, int64(bps), 10000) for any valid bps.
func TestSafePercentage_EquivalentToSafeMulDivComposition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		amount int64
		bps    uint32
	}{
		{1000, 0},
		{1000, 1},
		{1000, 100},
		{1000, 2500},
		{1000, 5000},
		{1000, 9999},
		{1000, 10000},
		{777, 333},
		{1_000_000, 42},
	} {
		tc := tc
		resultSafePerc, err1 := keeper.SafePercentage(math.NewInt(tc.amount), tc.bps)
		resultSafeMulDiv, err2 := keeper.SafeMulDiv(math.NewInt(tc.amount), int64(tc.bps), 10000)
		require.NoError(t, err1, "amount=%d bps=%d", tc.amount, tc.bps)
		require.NoError(t, err2)

		assert.True(t, resultSafePerc.Equal(resultSafeMulDiv),
			"amount=%d bps=%d: SafePercentage=%s vs SafeMulDiv=%s. "+
				"Pins the COMPOSITION contract: SafePercentage is a "+
				"thin façade over SafeMulDiv with scale=10000. A "+
				"refactor that inlined or diverged the computation "+
				"would surface here.",
			tc.amount, tc.bps, resultSafePerc.String(), resultSafeMulDiv.String())
	}
}

// TestSafePercentage_ExtremeUint32Rejected pins that uint32
// values far above 10000 are SAFELY rejected — not silently
// wrapped to a smaller value via modular arithmetic.
func TestSafePercentage_ExtremeUint32ValuesRejected(t *testing.T) {
	t.Parallel()
	for _, extreme := range []uint32{
		10001,          // just over
		50_000,         // 5x max
		^uint32(0),     // uint32 max (~4.29 billion)
		^uint32(0) - 1, // uint32 max - 1
	} {
		_, err := keeper.SafePercentage(math.NewInt(1000), extreme)
		require.Error(t, err,
			"uint32 %d rejected. Pins :43-47 strict `>10000` "+
				"guard: a refactor using any kind of modular "+
				"reduction (e.g., `bps %% 10001`) would silently "+
				"wrap extreme values to smaller percentages, "+
				"silently accepting what should be rejected.", extreme)
		assert.Contains(t, err.Error(), "exceeds maximum 10000")
	}
}

// TestSafePercentage_ErrorMessageEchoesOffendingBPS pins the
// diagnostic: the error message reports the offending value
// for operator triage (Wrapf formatting at :45-47).
func TestSafePercentage_ErrorMessageEchoesOffendingBPS(t *testing.T) {
	t.Parallel()
	_, err := keeper.SafePercentage(math.NewInt(1000), 12345)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "12345",
		"error message echoes the offending bps value (12345) "+
			"for operator triage. Pins :45-47 Wrapf format: a "+
			"refactor using a generic 'invalid bps' message would "+
			"lose diagnostic signal.")
}

// TestSafePercentage_NegativeAmountPassThrough pins that
// SafePercentage INHERITS the negative-amount pass-through
// contract from SafeMulDiv. Same as TestSafeMulDiv's
// negative-amount test, but through the SafePercentage façade.
func TestSafePercentage_NegativeAmountPassesThroughViaSafeMulDiv(t *testing.T) {
	t.Parallel()
	r, err := keeper.SafePercentage(math.NewInt(-1000), 2500)
	require.NoError(t, err,
		"negative amount NOT rejected by SafePercentage (inherits "+
			"from SafeMulDiv). Pins the composition: a refactor "+
			"adding a positive-only guard in SafePercentage would "+
			"diverge its contract from SafeMulDiv's.")
	assert.Equal(t, int64(-250), r.Int64(),
		"sign preserved through the façade")
}

// TestSafePercentage_BoundaryAt10000ExactlyAccepted pins the
// strict `> 10000` guard. 10000 exactly → returns amount
// (SafeMulDiv full-rate path).
func TestSafePercentage_BPSAt10000ExactlyReturnsAmount(t *testing.T) {
	t.Parallel()
	r, err := keeper.SafePercentage(math.NewInt(12345), 10000)
	require.NoError(t, err,
		"bps == 10000 ACCEPTED (full-rate). Pins :43 strict `> "+
			"10000` guard: a refactor to `>=` would reject the 100%% "+
			"case and break every full-burn, all-royalty configuration.")
	assert.Equal(t, int64(12345), r.Int64(),
		"10000 bps → full amount returned (amount * 10000 / 10000)")
}

// TestSafePercentage_BPSAt10001JustOverRejected pins the
// mirror boundary rejection.
func TestSafePercentage_BPSAt10001JustOverRejected(t *testing.T) {
	t.Parallel()
	_, err := keeper.SafePercentage(math.NewInt(1000), 10001)
	require.Error(t, err,
		"bps == 10001 REJECTED. Pins :43 guard triggers at first "+
			"value above the maximum.")
	assert.Contains(t, err.Error(), "10001",
		"offending value echoed")
}

// TestSafePercentage_MR_LinearInAmountInheritedFromSafeMulDiv
// pins that the linearity MR inherited from SafeMulDiv holds
// through the façade. Sibling to the SafeMulDiv MR test but
// exercises the façade path.
func TestSafePercentage_MR_LinearInAmount(t *testing.T) {
	t.Parallel()
	r1, err := keeper.SafePercentage(math.NewInt(10_000), 1500)
	require.NoError(t, err)
	r2, err := keeper.SafePercentage(math.NewInt(20_000), 1500)
	require.NoError(t, err)

	assert.Equal(t, r1.MulRaw(2).Int64(), r2.Int64(),
		"MR linearity preserved through SafePercentage façade")
}

// TestSafePercentage_ZeroAmountProducesZeroForAllBPS pins the
// zero-amount short-circuit.
func TestSafePercentage_ZeroAmountProducesZero(t *testing.T) {
	t.Parallel()
	for _, bps := range []uint32{0, 1, 100, 5000, 9999, 10000} {
		r, err := keeper.SafePercentage(math.ZeroInt(), bps)
		require.NoError(t, err, "bps=%d", bps)
		assert.True(t, r.IsZero(), "bps=%d: zero amount → zero", bps)
	}
}
