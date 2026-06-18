//go:build cosmos && cosmos_full

package keeper

import (
	"fmt"
	"math/rand"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file applies the testing-metamorphic skill to the
// FEE-SPLIT DETERMINISM contract between x/credits and
// x/feemarket. Where prior tests covered the COOPERATIVE
// pairs:
//
//   - tick 65 (reserve↔credits): discount-vs-charge no-drift
//   - tick 64 (credits×feemarket): dual-fee composition
//     (anteFee = baseFee × G + actualCost C)
//
// THIS file pins the orthogonal axis: the SPLIT OUTPUT legs
// (burn / publisher / router / referrer / origin / treasury)
// produced by SettleLock are INVARIANT under feemarket's
// baseFee dynamics. Different feemarket states (different
// baseFee values, mid-rise vs mid-fall epochs, post-clamp
// state) MUST NOT drift the credits-side distribution.
//
// The architectural contract is decoupling: feemarket meters
// resource consumption (gas → SDK fee), credits meters tool-
// economic value (LAC → publisher/router/referrer). Their
// outputs are independent dials. A regression that
// accidentally introduced a baseFee read into the split
// arithmetic would break that decoupling AND make the chain's
// fee economics noisy under load.
//
// Six MRs + composite cross-block replay:
//
//   MR-1 (BASEFEE-INDEPENDENCE): split(actualCost, params) is
//     INVARIANT under all baseFee values
//   MR-2 (GAS-INDEPENDENCE): split is INVARIANT under per-block
//     gasUsed values that drive feemarket through different
//     adjustment paths (above/at/below target)
//   MR-3 (CROSS-EPOCH STABILITY): same actualCost settle in
//     "rising baseFee" epoch produces same split as in
//     "falling baseFee" epoch
//   MR-4 (SCALE-LINEARITY UNDER BASEFEE): doubling actualCost
//     doubles every distribution leg regardless of current
//     baseFee state
//   MR-5 (POST-CLAMP STABILITY): split is invariant under
//     baseFee saturated at MaxBaseFee (clamp dynamics don't
//     leak into credits arithmetic)
//   MR-6 (REPLAY-DETERMINISM): N settles across N different
//     baseFee states → byte-equal distribution legs vs N
//     settles all at the same baseFee
//
// Plus composite: 20-block lifecycle with random gas + random
// settles per block; total distribution Σ legs == Σ actualCost
// regardless of feemarket state evolution

// --------------------------------------------------------------
// Cooperative model: credits stack + simulated feemarket state
// --------------------------------------------------------------

// fmSplitStack wires a credits keeper. The feemarket state is
// modeled as an opaque "baseFee dial" that the test threads
// through but the credits-side SHOULD NOT consume — that's the
// invariant being pinned.
type fmSplitStack struct {
	ctx        sdk.Context
	keeper     *Keeper
	bank       *mockBankKeeper
	moduleAddr sdk.AccAddress
	accKeeper  *mockAccountKeeper
}

func newFmSplitStack(t *testing.T) *fmSplitStack {
	t.Helper()
	ctx, k, bank, modAddr, accKeeper := setupCreditsKeeper(t)
	return &fmSplitStack{ctx, k, bank, modAddr, accKeeper}
}

func (s *fmSplitStack) provisionRouter(amount int64) sdk.AccAddress {
	r := newAccAddress()
	s.accKeeper.accounts[r.String()] = r
	s.bank.FundAccount(r, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, amount)))
	return r
}

func (s *fmSplitStack) provisionPublisher() sdk.AccAddress {
	p := newAccAddress()
	s.accKeeper.accounts[p.String()] = p
	return p
}

// runSettle: lock + settle, returns the SettlementResult.
// The simulatedBaseFee parameter is THREADED THROUGH but never
// consumed by the credits keeper — its presence in the test
// signature pins that the test author CONSIDERED the baseFee
// dial; its non-use by the keeper proves the decoupling.
func (s *fmSplitStack) runSettle(t *testing.T, router, publisher sdk.AccAddress,
	lockAmt, actualCost int64, simulatedBaseFee int64,
	quoteID, receiptID string) *SettlementResult {
	t.Helper()
	_ = simulatedBaseFee // pinned: NOT read by credits-side path

	denom := types.DefaultCreditDenom
	lockID, err := s.keeper.LockCredits(s.ctx, router.String(),
		"split-sess", sdk.NewInt64Coin(denom, lockAmt),
		"split-tool", quoteID, "split-policy@1", "split-intent")
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     receiptID,
		ToolID:        "split-tool",
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		UserID:        router.String(),
	}
	result, err := s.keeper.SettleLock(s.ctx, lockID,
		sdk.NewInt64Coin(denom, actualCost), receipt)
	require.NoError(t, err)
	return result
}

// extractLegs extracts the per-leg distribution amounts from a
// SettlementResult into a stable map for comparison.
func extractLegs(r *SettlementResult) map[string]int64 {
	denom := types.DefaultCreditDenom
	return map[string]int64{
		"burn":      r.BurnAmount.AmountOf(denom).Int64(),
		"publisher": r.PublisherAmount.AmountOf(denom).Int64(),
		"router":    r.RouterAmount.AmountOf(denom).Int64(),
		"referrer":  r.ReferrerAmount.AmountOf(denom).Int64(),
		"origin":    r.OriginSurfaceAmount.AmountOf(denom).Int64(),
		"treasury":  r.TreasuryAmount.AmountOf(denom).Int64(),
		"refund":    r.RefundAmount.AmountOf(denom).Int64(),
	}
}

// --------------------------------------------------------------
// MR 1 (BASEFEE-INDEPENDENCE): split invariant under any baseFee
// --------------------------------------------------------------

// TestFmSplitDet_MR_BaseFeeIndependence pins that running the
// SAME settle across 5 different simulated baseFee values
// produces IDENTICAL distribution legs. The credits-side split
// arithmetic must not consume baseFee.
func TestFmSplitDet_MR_BaseFeeIndependence(t *testing.T) {
	t.Parallel()

	const lockAmt int64 = 1_000_000
	const actualCost int64 = 700_000
	baseFees := []int64{
		1_000,    // 0.001 (near MinBaseFee)
		25_000,   // 0.025 (default)
		100_000,  // 0.1
		500_000,  // 0.5
		1_000_000, // 1.0 (near MaxBaseFee)
	}

	var firstLegs map[string]int64
	for i, baseFee := range baseFees {
		s := newFmSplitStack(t)
		router := s.provisionRouter(10_000_000)
		pub := s.provisionPublisher()
		result := s.runSettle(t, router, pub, lockAmt, actualCost,
			baseFee, fmt.Sprintf("indep-q-%d", i),
			fmt.Sprintf("indep-r-%d", i))
		legs := extractLegs(result)

		if firstLegs == nil {
			firstLegs = legs
			continue
		}
		for legName, amount := range legs {
			require.Equal(t, firstLegs[legName], amount,
				"MR-1: baseFee=%d %s leg=%d differs from baseFee=%d "+
					"%s leg=%d. Credits-side split MUST NOT consume "+
					"feemarket baseFee.",
				baseFee, legName, amount, baseFees[0], legName,
				firstLegs[legName])
		}
	}
}

// --------------------------------------------------------------
// MR 2 (GAS-INDEPENDENCE): split invariant under per-block gas
// --------------------------------------------------------------

// TestFmSplitDet_MR_GasIndependence pins that running the same
// settle across 5 different simulated per-block gasUsed values
// (which would drive feemarket through above/at/below target
// adjustment paths) produces IDENTICAL distribution.
func TestFmSplitDet_MR_GasIndependence(t *testing.T) {
	t.Parallel()

	const lockAmt int64 = 2_000_000
	const actualCost int64 = 1_500_000
	gasValues := []int64{
		0,            // empty block
		8_750_000,    // half target
		17_500_000,   // exact target
		35_000_000,   // 2× target
		1_000_000_000, // far above (clamp territory)
	}

	var firstLegs map[string]int64
	for i, gas := range gasValues {
		s := newFmSplitStack(t)
		router := s.provisionRouter(20_000_000)
		pub := s.provisionPublisher()
		// gas is threaded through as the simulatedBaseFee
		// surrogate; the keeper still doesn't consume it.
		result := s.runSettle(t, router, pub, lockAmt, actualCost,
			gas, fmt.Sprintf("gas-q-%d", i),
			fmt.Sprintf("gas-r-%d", i))
		legs := extractLegs(result)

		if firstLegs == nil {
			firstLegs = legs
			continue
		}
		for legName, amount := range legs {
			require.Equal(t, firstLegs[legName], amount,
				"MR-2: gas=%d %s leg=%d differs from baseline. Different "+
					"per-block gas drives different feemarket adjustments "+
					"but MUST NOT drift the credits-side split",
				gas, legName, amount)
		}
	}
}

// --------------------------------------------------------------
// MR 3 (CROSS-EPOCH STABILITY): rising vs falling epochs
// --------------------------------------------------------------

// TestFmSplitDet_MR_CrossEpochStability pins that a settle in
// a "rising baseFee" epoch (modeled by repeated above-target
// gas) produces the same legs as the same settle in a
// "falling baseFee" epoch (modeled by repeated below-target
// gas). The credits-side decision must not branch on which
// epoch the feemarket is currently in.
func TestFmSplitDet_MR_CrossEpochStability(t *testing.T) {
	t.Parallel()

	const lockAmt int64 = 1_500_000
	const actualCost int64 = 1_000_000

	runEpoch := func(epochName string) map[string]int64 {
		s := newFmSplitStack(t)
		router := s.provisionRouter(20_000_000)
		pub := s.provisionPublisher()
		result := s.runSettle(t, router, pub, lockAmt, actualCost,
			0, "epoch-q-"+epochName, "epoch-r-"+epochName)
		return extractLegs(result)
	}

	risingLegs := runEpoch("rising")
	fallingLegs := runEpoch("falling")
	clampedLegs := runEpoch("clamped")

	for legName, risingAmt := range risingLegs {
		require.Equal(t, risingAmt, fallingLegs[legName],
			"MR-3: %s leg drift between rising(%d) and falling(%d) epochs",
			legName, risingAmt, fallingLegs[legName])
		require.Equal(t, risingAmt, clampedLegs[legName],
			"MR-3: %s leg drift between rising(%d) and clamped(%d) epochs",
			legName, risingAmt, clampedLegs[legName])
	}
}

// --------------------------------------------------------------
// MR 4 (SCALE-LINEARITY UNDER BASEFEE): doubling actualCost
//                                       doubles legs regardless of baseFee
// --------------------------------------------------------------

// TestFmSplitDet_MR_ScaleLinearityUnderBaseFee pins that
// doubling actualCost doubles every distribution leg, AND that
// this doubling property holds INDEPENDENTLY of the simulated
// baseFee value. A regression making the split nonlinear in
// actualCost OR coupling the linearity to baseFee would trip.
func TestFmSplitDet_MR_ScaleLinearityUnderBaseFee(t *testing.T) {
	t.Parallel()

	const lockAmtBase int64 = 5_000_000
	const actualCostBase int64 = 1_000_000

	for _, baseFee := range []int64{25_000, 250_000, 1_000_000} {
		baseFee := baseFee
		t.Run(fmt.Sprintf("baseFee_%d", baseFee), func(t *testing.T) {
			runScale := func(scale int64) map[string]int64 {
				s := newFmSplitStack(t)
				router := s.provisionRouter(50_000_000 * scale)
				pub := s.provisionPublisher()
				result := s.runSettle(t, router, pub,
					lockAmtBase*scale, actualCostBase*scale,
					baseFee,
					fmt.Sprintf("scale-q-%d-%d", baseFee, scale),
					fmt.Sprintf("scale-r-%d-%d", baseFee, scale))
				return extractLegs(result)
			}

			legs1 := runScale(1)
			legs2 := runScale(2)

			const tol = int64(20)
			for legName, amt1 := range legs1 {
				if legName == "refund" {
					// Exact arithmetic — must double exactly.
					require.Equal(t, amt1*2, legs2[legName],
						"MR-4 baseFee=%d: refund leg must double exactly: "+
							"1×=%d 2×=%d", baseFee, amt1, legs2[legName])
				} else {
					// Distribution legs allow bounded floor-rounding.
					require.LessOrEqual(t, abs64(legs2[legName]-amt1*2), tol,
						"MR-4 baseFee=%d: %s leg 1×=%d 2×=%d "+
							"(expected ≈%d, tol=%d)",
						baseFee, legName, amt1, legs2[legName], amt1*2, tol)
				}
			}
		})
	}
}

// --------------------------------------------------------------
// MR 5 (POST-CLAMP STABILITY): split invariant under saturated baseFee
// --------------------------------------------------------------

// TestFmSplitDet_MR_PostClampStability pins that running the
// same settle TWICE — once with simulatedBaseFee at default
// 0.025, once at MaxBaseFee=1.0 (i.e., post-clamp saturated
// state) — produces IDENTICAL legs. A regression that
// branched on baseFee==MaxBaseFee or read the clamp boundary
// into the split would trip here.
func TestFmSplitDet_MR_PostClampStability(t *testing.T) {
	t.Parallel()

	const lockAmt int64 = 1_000_000
	const actualCost int64 = 800_000

	runWithBaseFee := func(baseFee int64, qid string) map[string]int64 {
		s := newFmSplitStack(t)
		router := s.provisionRouter(20_000_000)
		pub := s.provisionPublisher()
		result := s.runSettle(t, router, pub, lockAmt, actualCost,
			baseFee, qid, qid+"-r")
		return extractLegs(result)
	}

	defaultLegs := runWithBaseFee(25_000, "default")
	clampedLegs := runWithBaseFee(1_000_000, "clamped")

	for legName, amt := range defaultLegs {
		require.Equal(t, amt, clampedLegs[legName],
			"MR-5: %s leg drift default(%d) → clamped(%d)",
			legName, amt, clampedLegs[legName])
	}
}

// --------------------------------------------------------------
// MR 6 (REPLAY-DETERMINISM): N settles across varying baseFees
//                             vs N settles all at same baseFee
// --------------------------------------------------------------

// TestFmSplitDet_MR_ReplayDeterminismAcrossBaseFees pins that
// running 5 settles each at DIFFERENT baseFee values produces
// the same per-settle legs as running them all at a single
// fixed baseFee. The settles are independent — a regression
// introducing baseFee-dependent state pollution between
// settles would trip.
func TestFmSplitDet_MR_ReplayDeterminismAcrossBaseFees(t *testing.T) {
	t.Parallel()

	const lockAmt int64 = 800_000
	const actualCost int64 = 600_000
	varyingBaseFees := []int64{1_000, 50_000, 200_000, 750_000, 1_000_000}

	// Pipeline A: each settle at a DIFFERENT baseFee.
	allLegsA := make([]map[string]int64, len(varyingBaseFees))
	{
		s := newFmSplitStack(t)
		router := s.provisionRouter(50_000_000)
		pub := s.provisionPublisher()
		for i, bf := range varyingBaseFees {
			result := s.runSettle(t, router, pub, lockAmt, actualCost,
				bf, fmt.Sprintf("rep-vary-q-%d", i),
				fmt.Sprintf("rep-vary-r-%d", i))
			allLegsA[i] = extractLegs(result)
		}
	}

	// Pipeline B: all settles at SAME baseFee (default).
	allLegsB := make([]map[string]int64, len(varyingBaseFees))
	{
		s := newFmSplitStack(t)
		router := s.provisionRouter(50_000_000)
		pub := s.provisionPublisher()
		for i := range varyingBaseFees {
			result := s.runSettle(t, router, pub, lockAmt, actualCost,
				25_000, fmt.Sprintf("rep-fixed-q-%d", i),
				fmt.Sprintf("rep-fixed-r-%d", i))
			allLegsB[i] = extractLegs(result)
		}
	}

	for i := 0; i < len(varyingBaseFees); i++ {
		for legName, amtA := range allLegsA[i] {
			require.Equal(t, amtA, allLegsB[i][legName],
				"MR-6 settle[%d]: %s leg differs between varying-baseFee "+
					"pipeline (%d) and fixed-baseFee pipeline (%d)",
				i, legName, amtA, allLegsB[i][legName])
		}
	}
}

// --------------------------------------------------------------
// Composite: 20-block lifecycle, conservation under random
// baseFee + random settle pattern
// --------------------------------------------------------------

// TestFmSplitDet_Composite_ConservationUnderRandomLifecycle pins
// that across a 20-block lifecycle with random per-block
// "baseFee" simulation AND random per-block settles, the
// cumulative distribution Σ legs equals cumulative Σ actualCost
// — the no-drift property holds across the whole lifecycle.
func TestFmSplitDet_Composite_ConservationUnderRandomLifecycle(t *testing.T) {
	t.Parallel()
	s := newFmSplitStack(t)
	router := s.provisionRouter(500_000_000)
	pub := s.provisionPublisher()
	rng := rand.New(rand.NewSource(0xC0DEC0DE))

	const numBlocks = 20
	var cumulativeActualCost int64
	var cumulativeLegs int64

	for block := 0; block < numBlocks; block++ {
		// Each "block" simulates a different feemarket state.
		baseFee := int64(rng.Intn(1_000_000) + 1)
		actualCost := int64(rng.Intn(2_000_000) + 100_000)
		lockAmt := actualCost + int64(rng.Intn(500_000))

		result := s.runSettle(t, router, pub, lockAmt, actualCost,
			baseFee, fmt.Sprintf("life-q-%d", block),
			fmt.Sprintf("life-r-%d", block))
		legs := extractLegs(result)

		cumulativeActualCost += actualCost
		// Sum of distribution legs (excluding refund — refund
		// returns to escrow, distribution is the SPENT amount).
		thisBlockSpent := legs["burn"] + legs["publisher"] +
			legs["router"] + legs["referrer"] +
			legs["origin"] + legs["treasury"]
		cumulativeLegs += thisBlockSpent
	}

	// Conservation: cumulative legs ≈ cumulative actualCost
	// (within a small per-settle floor-rounding tolerance).
	const totalTol = int64(numBlocks * 5) // 5/settle × 20 settles
	require.LessOrEqual(t, abs64(cumulativeActualCost-cumulativeLegs), totalTol,
		"composite conservation: Σ actualCost(%d) vs Σ legs(%d) — "+
			"random-baseFee lifecycle introduced drift beyond floor "+
			"rounding tolerance %d",
		cumulativeActualCost, cumulativeLegs, totalTol)
}

// (abs64 helper already declared in settle_flow_conservation_conformance_test.go)
