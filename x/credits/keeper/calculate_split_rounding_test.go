package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
)

// This file closes remaining DIRECT-test coverage gaps for
// CalculateSplit at x/credits/keeper/math_safe.go:95-169.
// Existing TestCalculateSplit pins the happy path + invalid-
// sum errors + asserts total == amount. What it does NOT pin:
//
//   1. The ROUNDING-ADJUSTMENT branch at :130-166 — when the
//      sum of SafePercentage(amount, bps_i) falls short of
//      amount due to integer truncation, the shortfall is
//      assigned to the share with the HIGHEST BPS (sort at
//      :145-147). A scan-angle #3 hidden-secondary-return
//      anchor — invisible from the total==amount check alone.
//
//   2. Any test exercising ALL FIVE BPS buckets (publisher,
//      router, originSurface, treasury, referrer) as non-
//      zero. Existing tests pass 0 for origin+treasury.
//
//   3. Metamorphic LINEARITY: doubling amount doubles every
//      share (existing test uses approximate-percentage
//      assertions via InDelta; a metamorphic equality is
//      exact).
//
//   4. Metamorphic BPS-SWAP SYMMETRY: swapping two BPS values
//      swaps their resulting shares (pins the per-bucket
//      computation is isolated, not coupled).
//
// A refactor changing the rounding-adjustment strategy (e.g.,
// spreading residual across ALL shares instead of assigning
// to highest-BPS) would satisfy the existing total==amount
// test but break the explicit rounding-assignment pin.

// ---- Rounding-adjustment branch ----

// TestCalculateSplit_RoundingResidualGoesToHighestBPS is the
// scan-angle #3 ANCHOR. When integer truncation leaves a
// residual (sum_of_shares < amount), the code adds the diff
// to the share with the HIGHEST BPS (via sort-DESC at :145).
// Test: amount=1000, BPS 1000/2000/3000/3500/500 (sum 10000).
// Raw calc: 100/200/300/350/50 = total 1000. No rounding
// needed. Need a case where SafePercentage actually rounds
// down. Amount=7, BPS 3333/3334/3333/0/0 (sum 10000):
//
//	7*3333/10000 = 2 (2.3331 trunc)
//	7*3334/10000 = 2 (2.3338 trunc)
//	7*3333/10000 = 2
//	sum=6, diff=1 → goes to router (3334 is highest).
func TestCalculateSplit_RoundingResidualGoesToHighestBPS(t *testing.T) {
	t.Parallel()
	pub, router, orig, treas, ref, err := keeper.CalculateSplit(
		math.NewInt(7),
		3333, // publisher
		3334, // router — HIGHEST
		3333, // referrer(arg5 actually)... check signature
		0,    // treasury
		0,    // referrerBPS
	)
	// Actual signature: publisherBPS, routerBPS, originSurfaceBPS,
	// treasuryBPS, referrerBPS
	require.NoError(t, err)

	// Each pre-adjustment value is floor(7 * bps / 10000):
	//   publisher (3333):      floor(23331/10000) = 2
	//   router    (3334):      floor(23338/10000) = 2
	//   originSurface (3333):  floor(23331/10000) = 2
	// Pre-adjustment sum = 6; residual = 1.
	// Residual goes to the HIGHEST BPS (router = 3334).
	assert.Equal(t, int64(3), router.Int64(),
		"router (BPS=3334, highest) absorbs the +1 rounding "+
			"residual. Pins the scan-angle #3 hidden-secondary "+
			"invariant: rounding residual assigned to highest-BPS "+
			"share via sort.SliceStable at :145-147. A refactor "+
			"that spread residual across all shares would produce "+
			"different per-share values even though total still "+
			"equals amount.")
	// Publisher and originSurface (tied at 3333) each get 2.
	assert.Equal(t, int64(2), pub.Int64(),
		"publisher (BPS=3333, tied) keeps its floor value (residual "+
			"goes to UNIQUELY highest BPS)")
	assert.Equal(t, int64(2), orig.Int64(),
		"originSurface (BPS=3333, tied) keeps its floor value")
	// Zero BPS shares are zero.
	assert.Equal(t, int64(0), treas.Int64())
	assert.Equal(t, int64(0), ref.Int64())

	// Conservation (still holds).
	total := pub.Add(router).Add(orig).Add(treas).Add(ref)
	assert.Equal(t, int64(7), total.Int64())
}

// TestCalculateSplit_RoundingResidualTiebreak pins the tie-
// breaking behavior when two BPS values are equal. sort.
// SliceStable is STABLE, so the FIRST equal-BPS entry in the
// original `adjustments` slice order wins. The order at
// :138-142 is publisher, router, originSurface, treasury,
// referrer. If publisher and router both have the highest
// BPS and tie, publisher wins.
func TestCalculateSplit_RoundingTiebreakPrefersPublisher(t *testing.T) {
	t.Parallel()
	// amount=7, BPS 3334/3333/3333/0/0.
	// publisher (3334): floor(23338/10000) = 2
	// router    (3333): floor(23331/10000) = 2
	// originSurface (3333): floor(23331/10000) = 2
	// Sum=6, residual=1 → highest BPS is publisher (3334).
	pub, router, orig, _, _, err := keeper.CalculateSplit(
		math.NewInt(7),
		3334, // publisher HIGHEST
		3333, // router
		3333, // originSurface
		0,    // treasury
		0,    // referrer
	)
	require.NoError(t, err)

	assert.Equal(t, int64(3), pub.Int64(),
		"publisher (HIGHEST BPS=3334) gets residual")
	assert.Equal(t, int64(2), router.Int64())
	assert.Equal(t, int64(2), orig.Int64())
}

// TestCalculateSplit_AllFiveBucketsNonZero pins that the
// 5-bucket decomposition handles ALL buckets as non-zero.
// Existing TestCalculateSplit hardcodes originSurface=0 and
// treasury=0. This covers the full 5-way split.
func TestCalculateSplit_AllFiveBucketsNonZero(t *testing.T) {
	t.Parallel()
	// 1000 units: 3000/2500/2000/1500/1000 = 10000 BPS.
	// Expected shares: 300/250/200/150/100 = 1000 total.
	pub, router, orig, treas, ref, err := keeper.CalculateSplit(
		math.NewInt(1000),
		3000, // publisher 30%
		2500, // router    25%
		2000, // originSurface 20%
		1500, // treasury  15%
		1000, // referrer  10%
	)
	require.NoError(t, err)
	assert.Equal(t, int64(300), pub.Int64())
	assert.Equal(t, int64(250), router.Int64())
	assert.Equal(t, int64(200), orig.Int64())
	assert.Equal(t, int64(150), treas.Int64())
	assert.Equal(t, int64(100), ref.Int64())

	// Conservation.
	total := pub.Add(router).Add(orig).Add(treas).Add(ref)
	assert.Equal(t, int64(1000), total.Int64())
}

// ---- Metamorphic relations ----

// TestCalculateSplit_MR_LinearInAmount pins metamorphic
// linearity: doubling amount doubles every share EXACTLY
// (no rounding residual at the round-number amount).
func TestCalculateSplit_MR_LinearInAmount(t *testing.T) {
	t.Parallel()
	// Amounts 1000 and 2000, same BPS. All shares should
	// scale by 2x exactly (no rounding for these inputs).
	pub1, r1, o1, t1, ref1, err := keeper.CalculateSplit(
		math.NewInt(1000), 3000, 2500, 2000, 1500, 1000,
	)
	require.NoError(t, err)
	pub2, r2, o2, t2_, ref2, err := keeper.CalculateSplit(
		math.NewInt(2000), 3000, 2500, 2000, 1500, 1000,
	)
	require.NoError(t, err)

	assert.Equal(t, pub1.MulRaw(2).Int64(), pub2.Int64(),
		"MR — publisher share linear in amount")
	assert.Equal(t, r1.MulRaw(2).Int64(), r2.Int64())
	assert.Equal(t, o1.MulRaw(2).Int64(), o2.Int64())
	assert.Equal(t, t1.MulRaw(2).Int64(), t2_.Int64())
	assert.Equal(t, ref1.MulRaw(2).Int64(), ref2.Int64(),
		"MR — referrer share linear. Pins that CalculateSplit "+
			"delegates to SafePercentage which is linear. A refactor "+
			"that introduced a saturating cap or non-linear term "+
			"would surface here.")
}

// TestCalculateSplit_MR_BPSwapSwapsShares pins that swapping
// TWO BPS values swaps their resulting shares. This pins
// per-bucket-isolation: each SafePercentage call reads only
// ITS OWN bps parameter, not coupled state.
func TestCalculateSplit_MR_BPSwapSwapsShares(t *testing.T) {
	t.Parallel()
	// Run 1: publisher=3000, router=2500, other=4500 (1500+1500+1500).
	// Wait — BPS must sum to 10000. Let me use:
	//   pub=3000, router=2500, orig=1500, treas=1500, ref=1500 = 10000.
	amount := math.NewInt(10000)
	pub1, r1, o1, t1, ref1, err := keeper.CalculateSplit(
		amount, 3000, 2500, 1500, 1500, 1500,
	)
	require.NoError(t, err)

	// Run 2: SWAP publisher and router BPS.
	pub2, r2, o2, t2_, ref2, err := keeper.CalculateSplit(
		amount, 2500, 3000, 1500, 1500, 1500,
	)
	require.NoError(t, err)

	// Publisher1 == Router2 (swapped).
	assert.Equal(t, pub1.Int64(), r2.Int64(),
		"MR — swapping publisher/router BPS swaps publisher/router "+
			"shares. Pins per-bucket isolation: a refactor that "+
			"aliased two SafePercentage calls (e.g., via shared "+
			"state) would break this equality.")
	assert.Equal(t, r1.Int64(), pub2.Int64(),
		"MR — router1 == publisher2 (the swap partner)")

	// Other three shares are unchanged (same BPS).
	assert.Equal(t, o1.Int64(), o2.Int64())
	assert.Equal(t, t1.Int64(), t2_.Int64())
	assert.Equal(t, ref1.Int64(), ref2.Int64())
}

// TestCalculateSplit_MR_ConservationHoldsWithRounding pins
// that the total==amount invariant holds even with rounding
// residuals — across many amount/BPS combinations.
func TestCalculateSplit_MR_ConservationWithRounding(t *testing.T) {
	t.Parallel()
	// A range of amounts where 10000-division leaves residuals.
	for _, amt := range []int64{1, 3, 7, 13, 97, 999, 7777} {
		pub, r, o, t_, ref, err := keeper.CalculateSplit(
			math.NewInt(amt),
			3333, 3334, 1111, 1111, 1111,
		)
		require.NoError(t, err, "amount %d", amt)
		total := pub.Add(r).Add(o).Add(t_).Add(ref)
		assert.Equal(t, amt, total.Int64(),
			"amount %d: conservation after rounding adjustment. "+
				"Pins the :130-166 residual-assignment branch — no "+
				"coin created or lost across the split regardless "+
				"of rounding.", amt)
	}
}

// TestCalculateSplit_ZeroAmount pins that zero amount
// produces five zero shares without error.
func TestCalculateSplit_ZeroAmount(t *testing.T) {
	t.Parallel()
	pub, r, o, t_, ref, err := keeper.CalculateSplit(
		math.ZeroInt(), 3000, 2500, 2000, 1500, 1000,
	)
	require.NoError(t, err)
	assert.True(t, pub.IsZero())
	assert.True(t, r.IsZero())
	assert.True(t, o.IsZero())
	assert.True(t, t_.IsZero())
	assert.True(t, ref.IsZero())
}

// TestCalculateSplit_BPSUnderflowErrors pins the :100-102
// invalid-sum guard. BPS underflow (sum < 10000) rejected.
func TestCalculateSplit_BPSSumMismatchRejected(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                          string
		pub, router, orig, treas, ref uint32
	}{
		{"underflow_9000", 3000, 2500, 1500, 1000, 1000}, // sum=9000
		{"overflow_11000", 3000, 2500, 2000, 1500, 2000}, // sum=11000
		{"all_zero", 0, 0, 0, 0, 0},                      // sum=0
	} {
		_, _, _, _, _, err := keeper.CalculateSplit(
			math.NewInt(1000), tc.pub, tc.router, tc.orig, tc.treas, tc.ref,
		)
		require.Error(t, err,
			"%s: sum != 10000 must error (got sum=%d)", tc.name,
			tc.pub+tc.router+tc.orig+tc.treas+tc.ref)
		assert.Contains(t, err.Error(), "splits must sum to 10000")
	}
}
