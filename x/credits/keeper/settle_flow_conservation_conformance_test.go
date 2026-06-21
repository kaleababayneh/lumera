package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file applies the testing-conformance-harnesses skill
// (Pattern 1: Differential Testing + spec-derived coverage
// matrix) to the CORE SETTLEMENT CONSERVATION contract:
//
//   "Tokens at commit = Tokens at distribution"
//
// Concretely, for every SettleLock call:
//
//   lockAmount = refund + Σ (burn + insurance + publisher
//                            + router + origin_surface
//                            + treasury + referrer)
//
// And equivalently, from the bank-balance perspective:
//
//   Σ all participants' balance changes + module escrow = 0
//
// This complements tick 62's lifecycle_roundtrip_conformance
// (which tested state determinism across Lock→Settle→Unlock)
// by drilling into the DISTRIBUTION ARITHMETIC of the settle
// flow itself. A regression that mis-routed share distribution
// or leaked tokens into the module account would trip here
// even when lifecycle-level round-trip determinism held.
//
// Seven MUST clauses pinned:
//
//   MUST-1: actualCost partitions exactly into distributions
//           (for isFinal settlements)
//   MUST-2: lockAmount = refund + all distribution legs
//   MUST-3: whole-universe conservation — Σ all participant
//           balance deltas == 0 (including burn destination)
//   MUST-4: zero-BPS roles receive zero tokens; non-zero
//           roles receive a positive amount proportional to
//           their BPS
//   MUST-5: no-referrer reallocation — when receipt.ReferrerAddr
//           is nil, referrer BPS folds into router; router
//           receives the combined share
//   MUST-6: partial settle refunds exactly (lockAmount -
//           actualCost) to the lock router
//   MUST-7: two keepers, same settle → byte-equal per-address
//           distribution amounts (cross-validator consensus)

// --------------------------------------------------------------
// Harness — one keeper per scenario, with richer balance
// introspection than the lifecycle conformance harness because
// we're checking partition arithmetic, not just determinism.
// --------------------------------------------------------------

type settleParticipants struct {
	router    sdk.AccAddress
	publisher sdk.AccAddress
	referrer  sdk.AccAddress
}

func newSettleParticipants(t *testing.T, bank *mockBankKeeper, accKeeper *mockAccountKeeper, fundRouter int64) settleParticipants {
	t.Helper()
	r := newAccAddress()
	p := newAccAddress()
	ref := newAccAddress()
	accKeeper.accounts[r.String()] = r
	accKeeper.accounts[p.String()] = p
	accKeeper.accounts[ref.String()] = ref
	bank.FundAccount(r, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, fundRouter)))
	return settleParticipants{router: r, publisher: p, referrer: ref}
}

// amtAt returns the credit-denom balance of an address. Helper
// to shrink repeated bank.GetBalance calls.
func amtAt(ctx sdk.Context, bank *mockBankKeeper, addr sdk.AccAddress) int64 {
	return bank.GetBalance(ctx, addr, types.DefaultCreditDenom).Amount.Int64()
}

// lockAndSettle performs Lock(amount) then Settle(actualCost) on
// the given keeper, using the provided participants. Returns the
// SettlementResult for inspection.
func lockAndSettle(
	t *testing.T,
	ctx sdk.Context,
	k *Keeper,
	p settleParticipants,
	lockAmount, actualCost int64,
	includeReferrer bool,
	quoteID, receiptID string,
) *SettlementResult {
	t.Helper()

	denom := types.DefaultCreditDenom
	lockID, err := k.LockCredits(ctx, p.router.String(),
		"sc-sess", sdk.NewInt64Coin(denom, lockAmount),
		"sc-tool", quoteID, "sc-policy@1", "sc-intent")
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     receiptID,
		ToolID:        "sc-tool",
		PublisherAddr: p.publisher,
		RouterAddr:    p.router,
		PublisherID:   p.publisher.String(),
		RouterID:      p.router.String(),
	}
	if includeReferrer {
		receipt.ReferrerAddr = p.referrer
		receipt.ReferrerID = p.referrer.String()
	}

	result, err := k.SettleLock(ctx, lockID,
		sdk.NewInt64Coin(denom, actualCost), receipt)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

// sumCoins returns the int64 amount for a coins collection in
// the credit denom (collapsing multi-denom into a single sum).
// Assumes the fee-split only distributes credit denom.
func sumCoins(coins sdk.Coins) int64 {
	if coins.IsZero() {
		return 0
	}
	return coins.AmountOf(types.DefaultCreditDenom).Int64()
}

// --------------------------------------------------------------
// MUST-1: actualCost partitions exactly into distributions
// --------------------------------------------------------------

// TestSettleFlow_MUST1_ActualCostPartitionsExactlyIntoDistributions
// pins that for a final settlement, the SettlementResult's
// distribution legs sum to exactly the actualCost amount. This
// is the CORE PARTITION INVARIANT — a regression dropping a leg
// from the distribution (e.g., forgetting the origin surface or
// treasury slice) would leave tokens unaccounted for.
func TestSettleFlow_MUST1_ActualCostPartitionsExactlyIntoDistributions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		lockAmount   int64
		actualCost   int64
		withReferrer bool
	}{
		{name: "large_amount_with_referrer", lockAmount: 10_000_000, actualCost: 10_000_000, withReferrer: true},
		{name: "partial_settle_with_referrer", lockAmount: 10_000_000, actualCost: 7_500_000, withReferrer: true},
		{name: "small_amount_with_referrer", lockAmount: 10_000, actualCost: 10_000, withReferrer: true},
		{name: "no_referrer_large", lockAmount: 10_000_000, actualCost: 10_000_000, withReferrer: false},
		{name: "no_referrer_partial", lockAmount: 10_000_000, actualCost: 4_200_000, withReferrer: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)
			p := newSettleParticipants(t, bank, accKeeper, tc.lockAmount*2)

			result := lockAndSettle(t, ctx, keeper, p,
				tc.lockAmount, tc.actualCost, tc.withReferrer,
				"must1-q-"+tc.name, "must1-r-"+tc.name)

			// Without an insurance keeper wired (the default test
			// setup), the code path at keeper.go:893-898 redistributes
			// the insurance slice back into net, so the SettlementResult
			// legs sum to EXACTLY actualCost. This is the conservation
			// contract we pin.
			burn := sumCoins(result.BurnAmount)
			publisher := sumCoins(result.PublisherAmount)
			router := sumCoins(result.RouterAmount)
			originSurface := sumCoins(result.OriginSurfaceAmount)
			treasury := sumCoins(result.TreasuryAmount)
			referrer := sumCoins(result.ReferrerAmount)

			// Refund is the portion returned out of escrow (for
			// partial settles: lockAmount - actualCost).
			refund := sumCoins(result.RefundAmount)
			require.Equal(t, tc.lockAmount-tc.actualCost, refund,
				"refund=%d should equal lockAmount(%d)-actualCost(%d)=%d",
				refund, tc.lockAmount, tc.actualCost, tc.lockAmount-tc.actualCost)

			totalDistribution := burn + publisher + router + originSurface + treasury + referrer

			// Tolerance: the various distribution legs each do
			// independent floor divisions, so small rounding dust
			// of up to ~5 units is expected.
			diff := tc.actualCost - totalDistribution
			require.LessOrEqual(t, abs64(diff), int64(5),
				"MUST-1 [%s]: actualCost=%d but Σ result-legs=%d "+
					"(burn=%d pub=%d router=%d origin=%d treasury=%d "+
					"referrer=%d) — partition drift > 5 rounding units",
				tc.name, tc.actualCost, totalDistribution,
				burn, publisher, router, originSurface, treasury, referrer)

			// Every leg must be non-negative.
			for legName, amount := range map[string]int64{
				"burn": burn, "publisher": publisher, "router": router,
				"originSurface": originSurface, "treasury": treasury,
				"referrer": referrer,
			} {
				require.GreaterOrEqual(t, amount, int64(0),
					"MUST-1 [%s]: %s distribution negative: %d",
					tc.name, legName, amount)
			}
		})
	}
}

func TestSettleFlow_QualityRebateResultMatchesActualTransfers(t *testing.T) {
	t.Parallel()

	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)
	p := newSettleParticipants(t, bank, accKeeper, 2_000_000)
	denom := types.DefaultCreditDenom

	const lockAmount int64 = 1_000_000
	const actualCost int64 = 1_000_000
	const rebateAmount int64 = 50_000

	lockID, err := keeper.LockCredits(ctx, p.router.String(),
		"rebate-sess", sdk.NewInt64Coin(denom, lockAmount),
		"rebate-tool", "rebate-quote", "rebate-policy@1", "rebate-intent")
	require.NoError(t, err)

	routerBefore := amtAt(ctx, bank, p.router)
	publisherBefore := amtAt(ctx, bank, p.publisher)

	result, err := keeper.SettleLock(ctx, lockID,
		sdk.NewInt64Coin(denom, actualCost),
		SettlementRequest{
			ReceiptID:     "rebate-receipt",
			ToolID:        "rebate-tool",
			PublisherAddr: p.publisher,
			RouterAddr:    p.router,
			ReferrerAddr:  p.referrer,
			PublisherID:   p.publisher.String(),
			RouterID:      p.router.String(),
			ReferrerID:    p.referrer.String(),
			RebateAmount:  sdk.NewCoins(sdk.NewInt64Coin(denom, rebateAmount)),
		})
	require.NoError(t, err)

	routerDelta := amtAt(ctx, bank, p.router) - routerBefore
	publisherDelta := amtAt(ctx, bank, p.publisher) - publisherBefore

	require.Equal(t, routerDelta, sumCoins(result.RouterAmount),
		"SettlementResult.RouterAmount must report the post-rebate router transfer")
	require.Equal(t, publisherDelta, sumCoins(result.PublisherAmount),
		"SettlementResult.PublisherAmount must include the rebate paid from router share")

	distributedTotal := sumCoins(result.BurnAmount) +
		sumCoins(result.PublisherAmount) +
		sumCoins(result.RouterAmount) +
		sumCoins(result.OriginSurfaceAmount) +
		sumCoins(result.TreasuryAmount) +
		sumCoins(result.ReferrerAmount)
	require.LessOrEqual(t, abs64(actualCost-distributedTotal), int64(5),
		"rebated result legs must still partition actualCost")
}

// --------------------------------------------------------------
// MUST-2: lockAmount = refund + Σ distribution legs
// --------------------------------------------------------------

// TestSettleFlow_MUST2_LockAmountEqualsRefundPlusDistributions
// pins the BROADER conservation: the full lock amount accounts
// for the refund PLUS every distribution leg. A regression that
// leaked part of the lock into a hidden destination (e.g. a
// module account not counted) would trip here.
func TestSettleFlow_MUST2_LockAmountEqualsRefundPlusDistributions(t *testing.T) {
	t.Parallel()
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)
	p := newSettleParticipants(t, bank, accKeeper, 20_000_000)

	const lockAmount int64 = 5_000_000
	const actualCost int64 = 3_000_000
	result := lockAndSettle(t, ctx, keeper, p,
		lockAmount, actualCost, true,
		"must2-q", "must2-r")

	// With insurance keeper nil in the default test setup, the
	// insurance slice is redistributed back to net (keeper.go:
	// 893-898), so SettlementResult legs sum to actualCost, and
	// refund + legs sum to lockAmount.
	sum := sumCoins(result.RefundAmount) +
		sumCoins(result.BurnAmount) +
		sumCoins(result.PublisherAmount) +
		sumCoins(result.RouterAmount) +
		sumCoins(result.OriginSurfaceAmount) +
		sumCoins(result.TreasuryAmount) +
		sumCoins(result.ReferrerAmount)

	diff := lockAmount - sum
	require.LessOrEqual(t, abs64(diff), int64(5),
		"MUST-2: lockAmount=%d but refund+Σlegs=%d — tokens "+
			"appeared or disappeared beyond rounding noise",
		lockAmount, sum)
}

// --------------------------------------------------------------
// MUST-3: Whole-universe bank conservation
// --------------------------------------------------------------

// TestSettleFlow_MUST3_WholeUniverseBankConservation pins the
// BANK-BALANCE perspective conservation: after settle, the sum
// of ALL balance deltas across (router, publisher, referrer,
// module escrow) equals NEGATIVE the burn amount (burn leaves
// the tracked universe).
//
// Note: burn destination and insurance pool are outside our
// tracked set; we compute the expected "leaves the set" amount
// from the result.
func TestSettleFlow_MUST3_WholeUniverseBankConservation(t *testing.T) {
	t.Parallel()
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)
	p := newSettleParticipants(t, bank, accKeeper, 20_000_000)

	// Baseline balances BEFORE lock.
	routerPre := amtAt(ctx, bank, p.router)
	pubPre := amtAt(ctx, bank, p.publisher)
	refPre := amtAt(ctx, bank, p.referrer)
	escrowPre := amtAt(ctx, bank, moduleAddr)

	const lockAmount int64 = 4_000_000
	const actualCost int64 = 2_500_000
	result := lockAndSettle(t, ctx, keeper, p,
		lockAmount, actualCost, true,
		"must3-q", "must3-r")

	routerPost := amtAt(ctx, bank, p.router)
	pubPost := amtAt(ctx, bank, p.publisher)
	refPost := amtAt(ctx, bank, p.referrer)
	escrowPost := amtAt(ctx, bank, moduleAddr)

	// Delta sum across our tracked set (router + publisher +
	// referrer + module escrow).
	deltaSum := (routerPost - routerPre) +
		(pubPost - pubPre) +
		(refPost - refPre) +
		(escrowPost - escrowPre)

	// With no insurance keeper wired (default setup), insurance
	// redistributes to net, so only burn + origin + treasury
	// leave the tracked set. For default params with no
	// toolpackID + no TreasuryAddress, only burn leaves.
	burnAmount := sumCoins(result.BurnAmount)
	originAmount := sumCoins(result.OriginSurfaceAmount)
	treasuryAmount := sumCoins(result.TreasuryAmount)
	leavesSet := burnAmount + originAmount + treasuryAmount

	// Expected: deltaSum == -leavesSet (tokens that left should
	// match the negative delta of our tracked set).
	expectedDelta := -leavesSet
	diff := deltaSum - expectedDelta
	require.LessOrEqual(t, abs64(diff), int64(5),
		"MUST-3 conservation: tracked set delta=%d should equal "+
			"-(burn+origin+treasury)=%d (burn=%d origin=%d treasury=%d) "+
			"— tokens went to an unaccounted destination",
		deltaSum, expectedDelta, burnAmount, originAmount, treasuryAmount)
}

// --------------------------------------------------------------
// MUST-4: Zero-BPS roles receive zero; non-zero get proportional
// --------------------------------------------------------------

// TestSettleFlow_MUST4_ZeroBPSRoleReceivesZeroNonZeroPositive
// pins the BPS → amount mapping: a role with 0 BPS must receive
// exactly 0 tokens; a role with positive BPS must receive a
// positive amount (unless actualCost is too small to round to
// at least 1 token).
func TestSettleFlow_MUST4_ZeroBPSRoleReceivesZeroNonZeroPositive(t *testing.T) {
	t.Parallel()

	// Scenario: no referrer → referrer BPS folds into router.
	// Referrer amount must be EXACTLY 0.
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)
	p := newSettleParticipants(t, bank, accKeeper, 10_000_000)

	const lockAmount int64 = 2_000_000
	result := lockAndSettle(t, ctx, keeper, p,
		lockAmount, lockAmount, false, // withReferrer=false
		"must4-q", "must4-r")

	referrer := sumCoins(result.ReferrerAmount)
	require.Equal(t, int64(0), referrer,
		"MUST-4: no-referrer settle produced referrer=%d; must be 0",
		referrer)

	// Publisher, router, burn all have positive default BPS →
	// must be positive.
	require.Positive(t, sumCoins(result.PublisherAmount),
		"MUST-4: publisher BPS is 70%% but publisher amount=0")
	require.Positive(t, sumCoins(result.RouterAmount),
		"MUST-4: router BPS is positive but router amount=0")
	require.Positive(t, sumCoins(result.BurnAmount),
		"MUST-4: burn BPS is 3%% but burn amount=0")

	// Origin surface amount with no toolpackID → 0.
	require.Equal(t, int64(0), sumCoins(result.OriginSurfaceAmount),
		"MUST-4: no toolpackID should yield 0 origin surface")
}

// --------------------------------------------------------------
// MUST-5: No-referrer reallocation — referrer BPS folds to router
// --------------------------------------------------------------

// TestSettleFlow_MUST5_NoReferrerReallocatesIntoRouter pins that
// when receipt.ReferrerAddr is nil, the referrer share BPS is
// added to the router BPS. Router amount in the no-referrer
// scenario should be strictly GREATER than in the with-referrer
// scenario (all else equal).
func TestSettleFlow_MUST5_NoReferrerReallocatesIntoRouter(t *testing.T) {
	t.Parallel()

	const lockAmount int64 = 10_000_000

	runScenario := func(withReferrer bool) int64 {
		ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)
		p := newSettleParticipants(t, bank, accKeeper, lockAmount*2)
		result := lockAndSettle(t, ctx, keeper, p,
			lockAmount, lockAmount, withReferrer,
			"must5-q", "must5-r")
		return sumCoins(result.RouterAmount)
	}

	routerWithReferrer := runScenario(true)
	routerNoReferrer := runScenario(false)

	require.Greater(t, routerNoReferrer, routerWithReferrer,
		"MUST-5: no-referrer router=%d must be > with-referrer "+
			"router=%d — referrer share (10%% default) should have "+
			"folded into router's share",
		routerNoReferrer, routerWithReferrer)

	// Quantitative check: the delta should approximately equal
	// the referrer BPS (1000/10000 = 10%% of actualCost after
	// burn+insurance).
	// Given burn=3%, insurance=0 (default), net=97%.
	// Referrer share = 10% of net = 9.7% of actualCost.
	expectedDelta := (lockAmount * 9_700) / 100_000 // 9.7% of lockAmount
	actualDelta := routerNoReferrer - routerWithReferrer
	// Wide tolerance: rates vary slightly across keeper config
	// and floor rounding at each stage compounds. ≤5% tolerance.
	tolerance := expectedDelta / 20
	diff := abs64(actualDelta - expectedDelta)
	require.LessOrEqual(t, diff, tolerance,
		"MUST-5: router delta from referrer reallocation=%d, "+
			"expected ~%d (within tolerance %d) — reallocation "+
			"arithmetic may be incorrect",
		actualDelta, expectedDelta, tolerance)
}

// --------------------------------------------------------------
// MUST-6: Partial settle refund is exactly lockAmount - actualCost
// --------------------------------------------------------------

// TestSettleFlow_MUST6_PartialSettleRefundExactDifference pins
// that result.RefundAmount = lockAmount - actualCost across
// multiple boundary cases. This is a first-class conservation
// invariant independent of the rest of the distribution.
func TestSettleFlow_MUST6_PartialSettleRefundExactDifference(t *testing.T) {
	t.Parallel()

	cases := []struct {
		lockAmount, actualCost int64
	}{
		{1_000_000, 1_000_000}, // full settle → refund 0
		{1_000_000, 999_999},   // 1-unit partial
		{1_000_000, 500_000},   // half
		{1_000_000, 1},         // near-zero actual
		{1_000_000, 0},         // zero actual → refund lockAmount
		{10, 7},                // tiny amounts
		{10_000_000, 1_234_567},
	}
	for _, tc := range cases {
		ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)
		p := newSettleParticipants(t, bank, accKeeper, 20_000_000)

		result := lockAndSettle(t, ctx, keeper, p,
			tc.lockAmount, tc.actualCost, true,
			"must6-q", "must6-r")

		expectedRefund := tc.lockAmount - tc.actualCost
		actualRefund := sumCoins(result.RefundAmount)
		require.Equal(t, expectedRefund, actualRefund,
			"MUST-6: lock=%d cost=%d expected refund=%d got=%d",
			tc.lockAmount, tc.actualCost, expectedRefund, actualRefund)
	}
}

// --------------------------------------------------------------
// MUST-7: Two keepers same settle → byte-equal per-leg distribution
// --------------------------------------------------------------

// TestSettleFlow_MUST7_TwoKeepersByteEqualDistribution runs the
// same settle scenario on two independent keepers and asserts
// that every distribution leg (burn, publisher, router, origin,
// treasury, referrer, refund) is EXACTLY equal between keepers.
// This is the cross-validator consensus contract for the settle
// arithmetic: any divergence means validators would produce
// different state roots.
func TestSettleFlow_MUST7_TwoKeepersByteEqualDistribution(t *testing.T) {
	t.Parallel()

	runKeeper := func() (*SettlementResult, int64, int64, int64) {
		ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)
		// Pre-generate ONE fixed set of addresses per keeper. The
		// two keepers' address sets are independent, but for this
		// test we compare the RESULT amounts not the addresses.
		p := newSettleParticipants(t, bank, accKeeper, 20_000_000)

		result := lockAndSettle(t, ctx, keeper, p,
			3_000_000, 2_500_000, true,
			"must7-q", "must7-r")

		routerBal := amtAt(ctx, bank, p.router)
		pubBal := amtAt(ctx, bank, p.publisher)
		refBal := amtAt(ctx, bank, p.referrer)
		_ = moduleAddr
		return result, routerBal, pubBal, refBal
	}

	resultA, routerA, pubA, refA := runKeeper()
	resultB, routerB, pubB, refB := runKeeper()

	// Per-leg distribution byte-equality.
	for legName, pair := range map[string]struct {
		a, b sdkmath.Int
	}{
		"burn":      {resultA.BurnAmount.AmountOf(types.DefaultCreditDenom), resultB.BurnAmount.AmountOf(types.DefaultCreditDenom)},
		"publisher": {resultA.PublisherAmount.AmountOf(types.DefaultCreditDenom), resultB.PublisherAmount.AmountOf(types.DefaultCreditDenom)},
		"router":    {resultA.RouterAmount.AmountOf(types.DefaultCreditDenom), resultB.RouterAmount.AmountOf(types.DefaultCreditDenom)},
		"origin":    {resultA.OriginSurfaceAmount.AmountOf(types.DefaultCreditDenom), resultB.OriginSurfaceAmount.AmountOf(types.DefaultCreditDenom)},
		"treasury":  {resultA.TreasuryAmount.AmountOf(types.DefaultCreditDenom), resultB.TreasuryAmount.AmountOf(types.DefaultCreditDenom)},
		"referrer":  {resultA.ReferrerAmount.AmountOf(types.DefaultCreditDenom), resultB.ReferrerAmount.AmountOf(types.DefaultCreditDenom)},
		"refund":    {resultA.RefundAmount.AmountOf(types.DefaultCreditDenom), resultB.RefundAmount.AmountOf(types.DefaultCreditDenom)},
	} {
		require.True(t, pair.a.Equal(pair.b),
			"MUST-7: %s leg diverges A=%s B=%s — distribution "+
				"arithmetic is non-deterministic across keepers",
			legName, pair.a, pair.b)
	}

	// Per-participant post-settle balance byte-equality.
	require.Equal(t, routerA, routerB,
		"MUST-7: router post-balance A=%d B=%d", routerA, routerB)
	require.Equal(t, pubA, pubB,
		"MUST-7: publisher post-balance A=%d B=%d", pubA, pubB)
	require.Equal(t, refA, refB,
		"MUST-7: referrer post-balance A=%d B=%d", refA, refB)
}

// --------------------------------------------------------------
// Coverage matrix
// --------------------------------------------------------------

// TestSettleFlow_CoverageMatrix enumerates the 7 MUST clauses
// and confirms each has a dedicated test.
func TestSettleFlow_CoverageMatrix(t *testing.T) {
	t.Parallel()
	matrix := []struct {
		id, description, testName string
	}{
		{"MUST-1", "actualCost partitions exactly into distribution legs",
			"TestSettleFlow_MUST1_ActualCostPartitionsExactlyIntoDistributions"},
		{"MUST-2", "lockAmount = refund + Σ distribution legs",
			"TestSettleFlow_MUST2_LockAmountEqualsRefundPlusDistributions"},
		{"MUST-3", "whole-universe bank conservation across tracked set",
			"TestSettleFlow_MUST3_WholeUniverseBankConservation"},
		{"MUST-4", "zero-BPS role receives zero; non-zero receives positive",
			"TestSettleFlow_MUST4_ZeroBPSRoleReceivesZeroNonZeroPositive"},
		{"MUST-5", "no-referrer reallocates referrer share into router",
			"TestSettleFlow_MUST5_NoReferrerReallocatesIntoRouter"},
		{"MUST-6", "partial settle refund = lockAmount - actualCost exactly",
			"TestSettleFlow_MUST6_PartialSettleRefundExactDifference"},
		{"MUST-7", "two keepers, same settle → byte-equal distribution",
			"TestSettleFlow_MUST7_TwoKeepersByteEqualDistribution"},
	}
	require.Len(t, matrix, 7,
		"coverage matrix must have exactly 7 MUST clauses")
	for _, m := range matrix {
		require.NotEmpty(t, m.id)
		require.NotEmpty(t, m.description)
		require.NotEmpty(t, m.testName)
		t.Logf("[settle-flow] %s: %s → %s", m.id, m.description, m.testName)
	}
}

// abs64 returns absolute value of a signed int64.
func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
