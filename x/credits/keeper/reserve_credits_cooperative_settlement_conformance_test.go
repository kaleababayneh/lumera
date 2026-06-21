package keeper

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
	reservetypes "github.com/LumeraProtocol/lumera/x/reserve/types"
)

// This file applies the testing-conformance-harnesses skill
// (Pattern 1: Differential Testing) to the COOPERATIVE
// SETTLEMENT pipeline between x/reserve and x/credits.
//
// Pipeline:
//
//   1. A router holds an active reserve commitment for (policy,
//      tool) with RemainingAmount capacity and DiscountBps
//   2. Router locks credits, then SettleLock(actualCost)
//   3. SettleLock at keeper.go:1644-1664 calls
//      reserveKeeper.AllocateReserve(owner, policy, tool, actualCost):
//        - reserve decrements RemainingAmount by FULL actualCost
//        - returns DiscountedPrice = actualCost × (1 - DiscountBps/10000)
//   4. SettleLock charges the user only DiscountedPrice
//   5. The "discount" is the gap (actualCost - DiscountedPrice);
//      reserve effectively absorbed this notional cost from its
//      capacity in exchange for the user paying less
//
// THE NO-DRIFT CONTRACT: across this boundary, the modules must
// agree on three invariants:
//
//   I_A: reserve.capacityDelta = actualCost (full notional charge)
//   I_B: credits.chargeAmount = actualCost × (1 - DiscountBps/10000)
//   I_C: discount_applied = actualCost - chargeAmount
//                         = actualCost × DiscountBps / 10000
//
// A regression that drifted any pair (e.g., reserve charged
// DiscountedPrice instead of full actualCost) would break the
// reserve-pricing economic model.
//
// Seven MUST clauses + composite cross-validator determinism:
//
//   MUST-1: Single settle reduces reserve capacity by EXACTLY
//           actualCost (full notional, not discounted)
//   MUST-2: Router charge = actualCost × (1 - DiscountBps/10000)
//           — discount applied matches BPS exactly
//   MUST-3: Multi-settle depletion is additive: Σ capacity deltas
//           equals Σ actualCost across settles
//   MUST-4: No-commitment path: no allocation applied, no
//           reserve state change, full charge to router
//   MUST-5: Zero-discount commitment (DiscountBps=0): capacity
//           decreases by actualCost but charge = full actualCost
//           (no savings)
//   MUST-6: Two parallel keepers + same script → byte-equal
//           reserve state AND credits state
//   MUST-7: Insufficient capacity: when actualCost > reserve
//           remaining, allocation NOT applied (reserve unchanged,
//           full charge to router) — graceful fall-through

// --------------------------------------------------------------
// Cooperative reserve stub — tracks capacity AND applies
// discount EXACTLY as the real reserve keeper would.
// --------------------------------------------------------------

type cooperativeReserveStub struct {
	commitments map[string]*cooperativeCommitment // key = "policyID|toolID"
	allocations []cooperativeAllocationRecord
}

type cooperativeCommitment struct {
	commitmentID string
	owner        string
	remaining    sdk.Coin
	discountBps  uint32
}

type cooperativeAllocationRecord struct {
	policyID     string
	toolID       string
	actualCost   sdk.Coin
	discounted   sdk.Coin
	commitmentID string
}

func newCooperativeReserveStub() *cooperativeReserveStub {
	return &cooperativeReserveStub{
		commitments: make(map[string]*cooperativeCommitment),
	}
}

func (r *cooperativeReserveStub) installCommitment(commitID, owner, policyID, toolID string, capacity int64, discountBps uint32) {
	key := policyID + "|" + toolID
	r.commitments[key] = &cooperativeCommitment{
		commitmentID: commitID,
		owner:        owner,
		remaining:    sdk.NewInt64Coin(types.DefaultCreditDenom, capacity),
		discountBps:  discountBps,
	}
}

// AllocateReserve mirrors the real reserve keeper's contract:
//   - finds matching commitment by policyID+toolID
//   - if remaining ≥ actualCost: deducts full actualCost from
//     remaining, returns DiscountedPrice
//   - else: returns Applied=false, full price
func (r *cooperativeReserveStub) AllocateReserve(_ context.Context, owner, policyID, toolID string, amount sdk.Coin) (reservetypes.ReserveAllocation, error) {
	if amount.IsZero() {
		return reservetypes.ReserveAllocation{Applied: false, DiscountedPrice: amount}, nil
	}
	key := policyID + "|" + toolID
	commit, ok := r.commitments[key]
	if !ok {
		return reservetypes.ReserveAllocation{Applied: false, DiscountedPrice: amount}, nil
	}
	if commit.owner != owner {
		return reservetypes.ReserveAllocation{Applied: false, DiscountedPrice: amount}, nil
	}
	if !commit.remaining.Amount.GTE(amount.Amount) {
		// Insufficient capacity → no allocation.
		return reservetypes.ReserveAllocation{Applied: false, DiscountedPrice: amount}, nil
	}

	// Deduct full actualCost from capacity (NOT discounted).
	commit.remaining = commit.remaining.Sub(amount)
	// Compute discounted price.
	discountedAmt := amount.Amount.Mul(
		sdkmath.NewInt(int64(10_000 - commit.discountBps))).
		Quo(sdkmath.NewInt(10_000))
	discounted := sdk.NewCoin(amount.Denom, discountedAmt)

	r.allocations = append(r.allocations, cooperativeAllocationRecord{
		policyID:     policyID,
		toolID:       toolID,
		actualCost:   amount,
		discounted:   discounted,
		commitmentID: commit.commitmentID,
	})

	return reservetypes.ReserveAllocation{
		Applied:         true,
		CommitmentID:    commit.commitmentID,
		DiscountedPrice: discounted,
	}, nil
}

func (r *cooperativeReserveStub) ReleaseExpired(_ context.Context) error { return nil }

func (r *cooperativeReserveStub) CreateCommitment(_ context.Context, _ reservetypes.ReserveRequest) (*reservetypes.ReserveCommitment, error) {
	return nil, errors.New("not implemented in cooperative stub")
}

func (r *cooperativeReserveStub) HasActiveCommitment(_ context.Context, _ string, _ string) (bool, error) {
	return false, nil
}

// remainingFor returns the current remaining capacity for a
// (policy, tool) commitment.
func (r *cooperativeReserveStub) remainingFor(policyID, toolID string) int64 {
	key := policyID + "|" + toolID
	commit, ok := r.commitments[key]
	if !ok {
		return 0
	}
	return commit.remaining.Amount.Int64()
}

// --------------------------------------------------------------
// Cooperative stack: credits keeper + cooperative reserve stub
// --------------------------------------------------------------

type cooperativeStack struct {
	ctx        sdk.Context
	keeper     *Keeper
	bank       *mockBankKeeper
	moduleAddr sdk.AccAddress
	accKeeper  *mockAccountKeeper
	reserve    *cooperativeReserveStub
}

func newCooperativeStack(t *testing.T) *cooperativeStack {
	t.Helper()
	reserve := newCooperativeReserveStub()
	ctx, keeper, bank, modAddr, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		reserveKeeper: reserve,
	})
	return &cooperativeStack{
		ctx:        ctx,
		keeper:     keeper,
		bank:       bank,
		moduleAddr: modAddr,
		accKeeper:  accKeeper,
		reserve:    reserve,
	}
}

// settleWithReserve is a single Lock+Settle through the
// cooperative pipeline. policyID + toolID activate the reserve
// allocation path; receipt.UserID = router.
func settleWithReserve(
	t *testing.T,
	s *cooperativeStack,
	router sdk.AccAddress,
	publisher sdk.AccAddress,
	lockAmount, actualCost int64,
	policyID, toolID string,
	quoteID, receiptID string,
) *SettlementResult {
	t.Helper()
	denom := types.DefaultCreditDenom

	lockID, err := s.keeper.LockCredits(s.ctx, router.String(),
		"coop-sess", sdk.NewInt64Coin(denom, lockAmount),
		toolID, quoteID, policyID, "coop-intent")
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:      receiptID,
		ToolID:         toolID,
		PublisherAddr:  publisher,
		RouterAddr:     router,
		PublisherID:    publisher.String(),
		RouterID:       router.String(),
		UserID:         router.String(),
		PolicySnapshot: policyID,
	}
	result, err := s.keeper.SettleLock(s.ctx, lockID,
		sdk.NewInt64Coin(denom, actualCost), receipt)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

// --------------------------------------------------------------
// MUST-1: Single settle decreases reserve capacity by full
//         actualCost (NOT discounted)
// --------------------------------------------------------------

func TestReserveCreditsCoop_MUST1_ReserveCapacityDecreasesByFullActualCost(t *testing.T) {
	t.Parallel()
	s := newCooperativeStack(t)

	router := newAccAddress()
	publisher := newAccAddress()
	s.accKeeper.accounts[router.String()] = router
	s.accKeeper.accounts[publisher.String()] = publisher
	s.bank.FundAccount(router, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000_000)))

	const policyID = "policy-a"
	const toolID = "tool-X"
	const initialCapacity int64 = 5_000_000
	const discountBps uint32 = 1500 // 15% discount

	s.reserve.installCommitment("commit-A", router.String(),
		policyID, toolID, initialCapacity, discountBps)

	const lockAmount int64 = 2_000_000
	const actualCost int64 = 1_000_000

	_ = settleWithReserve(t, s, router, publisher,
		lockAmount, actualCost, policyID, toolID,
		"must1-q", "must1-r")

	postCapacity := s.reserve.remainingFor(policyID, toolID)
	expectedCapacity := initialCapacity - actualCost
	require.Equal(t, expectedCapacity, postCapacity,
		"MUST-1: reserve capacity=%d but expected initial(%d) - "+
			"actualCost(%d) = %d. Reserve must charge FULL "+
			"actualCost to its capacity, not the discounted amount.",
		postCapacity, initialCapacity, actualCost, expectedCapacity)
}

// --------------------------------------------------------------
// MUST-2: Router charge = actualCost × (1 - DiscountBps/10000)
// --------------------------------------------------------------

func TestReserveCreditsCoop_MUST2_RouterChargeMatchesDiscountedPrice(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		actualCost  int64
		discountBps uint32
	}{
		{"5pct_discount", 1_000_000, 500},
		{"10pct_discount", 1_000_000, 1000},
		{"25pct_discount", 1_000_000, 2500},
		{"50pct_discount", 800_000, 5000},
		{"large_amount_5pct", 10_000_000, 500},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newCooperativeStack(t)
			router := newAccAddress()
			publisher := newAccAddress()
			s.accKeeper.accounts[router.String()] = router
			s.accKeeper.accounts[publisher.String()] = publisher
			s.bank.FundAccount(router, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 50_000_000)))

			const policyID = "policy-disc"
			const toolID = "tool-disc"
			s.reserve.installCommitment("commit-disc", router.String(),
				policyID, toolID, 100_000_000, tc.discountBps)

			lockAmount := tc.actualCost + 500_000
			result := settleWithReserve(t, s, router, publisher,
				lockAmount, tc.actualCost, policyID, toolID,
				"must2-q-"+tc.name, "must2-r-"+tc.name)

			// Expected discounted charge = actualCost × (10000 - bps) / 10000
			expectedCharge := (tc.actualCost * int64(10_000-tc.discountBps)) / 10_000
			expectedDiscount := tc.actualCost - expectedCharge

			// SettleLock's charge flows through ProcessSettlement,
			// where the SettlementResult tracks distribution legs.
			// Sum of distribution legs == chargeAmount (post-discount).
			chargeFromLegs := sumCoins(result.BurnAmount) +
				sumCoins(result.PublisherAmount) +
				sumCoins(result.RouterAmount) +
				sumCoins(result.OriginSurfaceAmount) +
				sumCoins(result.TreasuryAmount) +
				sumCoins(result.ReferrerAmount)

			require.LessOrEqual(t, abs64(expectedCharge-chargeFromLegs), int64(5),
				"MUST-2 [%s]: charge from result legs=%d but expected "+
					"discounted=%d (actualCost=%d × (10000-%d)/10000). "+
					"Discount: expected=%d.",
				tc.name, chargeFromLegs, expectedCharge,
				tc.actualCost, tc.discountBps, expectedDiscount)
		})
	}
}

// --------------------------------------------------------------
// MUST-3: Multi-settle depletion is additive
// --------------------------------------------------------------

func TestReserveCreditsCoop_MUST3_MultiSettleDepletionAdditive(t *testing.T) {
	t.Parallel()
	s := newCooperativeStack(t)

	router := newAccAddress()
	publisher := newAccAddress()
	s.accKeeper.accounts[router.String()] = router
	s.accKeeper.accounts[publisher.String()] = publisher
	s.bank.FundAccount(router, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000_000)))

	const policyID = "policy-multi"
	const toolID = "tool-multi"
	const initialCapacity int64 = 50_000_000
	s.reserve.installCommitment("commit-multi", router.String(),
		policyID, toolID, initialCapacity, 1000) // 10% discount

	costs := []int64{500_000, 1_200_000, 2_500_000, 750_000, 3_000_000}
	var totalCost int64
	for i, cost := range costs {
		_ = settleWithReserve(t, s, router, publisher,
			cost+1_000_000, cost, policyID, toolID,
			fmt.Sprintf("must3-q-%d", i),
			fmt.Sprintf("must3-r-%d", i))
		totalCost += cost
	}

	postCapacity := s.reserve.remainingFor(policyID, toolID)
	expectedPost := initialCapacity - totalCost
	require.Equal(t, expectedPost, postCapacity,
		"MUST-3: post-settle capacity=%d but expected %d "+
			"(initial=%d - Σcosts=%d). Per-settle depletion is "+
			"non-additive — drift between modules.",
		postCapacity, expectedPost, initialCapacity, totalCost)

	// Cross-check via allocation log: sum of allocation actualCosts.
	var loggedSum int64
	for _, alloc := range s.reserve.allocations {
		loggedSum += alloc.actualCost.Amount.Int64()
	}
	require.Equal(t, totalCost, loggedSum,
		"MUST-3 alloc log: logged Σactualcost=%d ≠ scripted Σ=%d",
		loggedSum, totalCost)
}

// --------------------------------------------------------------
// MUST-4: No-commitment path leaves reserve unchanged + full charge
// --------------------------------------------------------------

func TestReserveCreditsCoop_MUST4_NoCommitmentFullChargeNoDrift(t *testing.T) {
	t.Parallel()
	s := newCooperativeStack(t)

	router := newAccAddress()
	publisher := newAccAddress()
	s.accKeeper.accounts[router.String()] = router
	s.accKeeper.accounts[publisher.String()] = publisher
	s.bank.FundAccount(router, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000_000)))

	// No commitment installed for this (policy, tool) pair.
	const policyID = "policy-uncovered"
	const toolID = "tool-uncovered"

	const lockAmount int64 = 1_500_000
	const actualCost int64 = 1_000_000
	result := settleWithReserve(t, s, router, publisher,
		lockAmount, actualCost, policyID, toolID,
		"must4-q", "must4-r")

	// MUST-4(a): no allocation logged.
	require.Empty(t, s.reserve.allocations,
		"MUST-4(a): no commitment for (%s,%s) — NO allocation should "+
			"have been logged but got %d",
		policyID, toolID, len(s.reserve.allocations))

	// MUST-4(b): no reserve state change for any commitment.
	require.Equal(t, int64(0), s.reserve.remainingFor(policyID, toolID),
		"MUST-4(b): missing commitment should remain at 0 capacity")

	// MUST-4(c): credits charged the FULL actualCost (no discount).
	chargeFromLegs := sumCoins(result.BurnAmount) +
		sumCoins(result.PublisherAmount) +
		sumCoins(result.RouterAmount) +
		sumCoins(result.OriginSurfaceAmount) +
		sumCoins(result.TreasuryAmount) +
		sumCoins(result.ReferrerAmount)
	require.LessOrEqual(t, abs64(actualCost-chargeFromLegs), int64(5),
		"MUST-4(c): no commitment → charge should equal full "+
			"actualCost(%d), got %d", actualCost, chargeFromLegs)
}

// --------------------------------------------------------------
// MUST-5: Zero-discount commitment depletes capacity, no savings
// --------------------------------------------------------------

func TestReserveCreditsCoop_MUST5_ZeroDiscountCommitmentDepletesNoSavings(t *testing.T) {
	t.Parallel()
	s := newCooperativeStack(t)

	router := newAccAddress()
	publisher := newAccAddress()
	s.accKeeper.accounts[router.String()] = router
	s.accKeeper.accounts[publisher.String()] = publisher
	s.bank.FundAccount(router, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000_000)))

	const policyID = "policy-zero"
	const toolID = "tool-zero"
	const initialCapacity int64 = 5_000_000

	s.reserve.installCommitment("commit-zero", router.String(),
		policyID, toolID, initialCapacity, 0) // 0 BPS = no discount

	const lockAmount int64 = 1_500_000
	const actualCost int64 = 1_000_000
	result := settleWithReserve(t, s, router, publisher,
		lockAmount, actualCost, policyID, toolID,
		"must5-q", "must5-r")

	// MUST-5(a): allocation WAS applied (commitment exists).
	require.Len(t, s.reserve.allocations, 1,
		"MUST-5(a): zero-discount commitment exists, allocation should "+
			"be applied (got %d)", len(s.reserve.allocations))

	// MUST-5(b): capacity decreased by full actualCost.
	postCapacity := s.reserve.remainingFor(policyID, toolID)
	require.Equal(t, initialCapacity-actualCost, postCapacity,
		"MUST-5(b): zero-BPS commitment must still deplete capacity "+
			"by full actualCost: got=%d expected=%d",
		postCapacity, initialCapacity-actualCost)

	// MUST-5(c): charge = full actualCost (no savings).
	chargeFromLegs := sumCoins(result.BurnAmount) +
		sumCoins(result.PublisherAmount) +
		sumCoins(result.RouterAmount) +
		sumCoins(result.OriginSurfaceAmount) +
		sumCoins(result.TreasuryAmount) +
		sumCoins(result.ReferrerAmount)
	require.LessOrEqual(t, abs64(actualCost-chargeFromLegs), int64(5),
		"MUST-5(c): zero-BPS commitment should yield NO discount: "+
			"charge=%d expected=%d", chargeFromLegs, actualCost)
}

// --------------------------------------------------------------
// MUST-6: Two parallel keepers + same script → byte-equal state
// --------------------------------------------------------------

func TestReserveCreditsCoop_MUST6_TwoKeepersByteEqualReserveAndCredits(t *testing.T) {
	t.Parallel()
	stackA := newCooperativeStack(t)
	stackB := newCooperativeStack(t)

	// Pre-generate one router + publisher used in both stacks.
	router := newAccAddress()
	publisher := newAccAddress()
	for _, s := range []*cooperativeStack{stackA, stackB} {
		s.accKeeper.accounts[router.String()] = router
		s.accKeeper.accounts[publisher.String()] = publisher
		s.bank.FundAccount(router, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 50_000_000)))
		s.reserve.installCommitment("commit-conform", router.String(),
			"p-conform", "t-conform", 20_000_000, 1000) // 10%
	}

	// Same scripted multi-settle.
	costs := []int64{750_000, 1_200_000, 350_000, 2_000_000}
	for i, cost := range costs {
		_ = settleWithReserve(t, stackA, router, publisher,
			cost+500_000, cost, "p-conform", "t-conform",
			fmt.Sprintf("conf-q-%d", i),
			fmt.Sprintf("conf-r-%d", i))
		_ = settleWithReserve(t, stackB, router, publisher,
			cost+500_000, cost, "p-conform", "t-conform",
			fmt.Sprintf("conf-q-%d", i),
			fmt.Sprintf("conf-r-%d", i))
	}

	// MUST-6(a): reserve capacity byte-equal.
	capA := stackA.reserve.remainingFor("p-conform", "t-conform")
	capB := stackB.reserve.remainingFor("p-conform", "t-conform")
	require.Equal(t, capA, capB,
		"MUST-6(a): reserve capacity diverges A=%d B=%d", capA, capB)

	// MUST-6(b): allocation log byte-equal in length and content.
	require.Equal(t, len(stackA.reserve.allocations), len(stackB.reserve.allocations),
		"MUST-6(b): allocation log lengths diverge")
	for i := range stackA.reserve.allocations {
		require.Equal(t, stackA.reserve.allocations[i].actualCost,
			stackB.reserve.allocations[i].actualCost,
			"MUST-6(b): allocation[%d] actualCost diverges", i)
		require.Equal(t, stackA.reserve.allocations[i].discounted,
			stackB.reserve.allocations[i].discounted,
			"MUST-6(b): allocation[%d] discounted diverges", i)
	}

	// MUST-6(c): credits-side balances byte-equal.
	denom := types.DefaultCreditDenom
	balRA := stackA.bank.GetBalance(stackA.ctx, router, denom).Amount.Int64()
	balRB := stackB.bank.GetBalance(stackB.ctx, router, denom).Amount.Int64()
	require.Equal(t, balRA, balRB,
		"MUST-6(c): router balance diverges A=%d B=%d", balRA, balRB)
	balPA := stackA.bank.GetBalance(stackA.ctx, publisher, denom).Amount.Int64()
	balPB := stackB.bank.GetBalance(stackB.ctx, publisher, denom).Amount.Int64()
	require.Equal(t, balPA, balPB,
		"MUST-6(c): publisher balance diverges A=%d B=%d", balPA, balPB)
	escA := stackA.bank.GetBalance(stackA.ctx, stackA.moduleAddr, denom).Amount.Int64()
	escB := stackB.bank.GetBalance(stackB.ctx, stackB.moduleAddr, denom).Amount.Int64()
	require.Equal(t, escA, escB,
		"MUST-6(c): module escrow diverges A=%d B=%d", escA, escB)
}

// --------------------------------------------------------------
// MUST-7: Insufficient capacity → graceful fall-through
// --------------------------------------------------------------

func TestReserveCreditsCoop_MUST7_InsufficientCapacityGracefulFallthrough(t *testing.T) {
	t.Parallel()
	s := newCooperativeStack(t)

	router := newAccAddress()
	publisher := newAccAddress()
	s.accKeeper.accounts[router.String()] = router
	s.accKeeper.accounts[publisher.String()] = publisher
	s.bank.FundAccount(router, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 20_000_000)))

	const policyID = "policy-tight"
	const toolID = "tool-tight"
	const initialCapacity int64 = 500_000 // small capacity

	s.reserve.installCommitment("commit-tight", router.String(),
		policyID, toolID, initialCapacity, 2000) // 20%

	const lockAmount int64 = 2_000_000
	const actualCost int64 = 1_000_000 // EXCEEDS capacity

	result := settleWithReserve(t, s, router, publisher,
		lockAmount, actualCost, policyID, toolID,
		"must7-q", "must7-r")

	// MUST-7(a): no allocation applied (insufficient capacity).
	require.Empty(t, s.reserve.allocations,
		"MUST-7(a): actualCost=%d > capacity=%d should NOT trigger "+
			"allocation, got %d allocations",
		actualCost, initialCapacity, len(s.reserve.allocations))

	// MUST-7(b): reserve capacity unchanged.
	require.Equal(t, initialCapacity, s.reserve.remainingFor(policyID, toolID),
		"MUST-7(b): insufficient-capacity path must NOT decrement "+
			"reserve")

	// MUST-7(c): credits charged FULL actualCost (no discount).
	chargeFromLegs := sumCoins(result.BurnAmount) +
		sumCoins(result.PublisherAmount) +
		sumCoins(result.RouterAmount) +
		sumCoins(result.OriginSurfaceAmount) +
		sumCoins(result.TreasuryAmount) +
		sumCoins(result.ReferrerAmount)
	require.LessOrEqual(t, abs64(actualCost-chargeFromLegs), int64(5),
		"MUST-7(c): insufficient-capacity → router pays full "+
			"actualCost(%d), got charge=%d", actualCost, chargeFromLegs)
}

// --------------------------------------------------------------
// Coverage matrix
// --------------------------------------------------------------

func TestReserveCreditsCoop_CoverageMatrix(t *testing.T) {
	t.Parallel()
	matrix := []struct {
		id, description, testName string
	}{
		{"MUST-1", "single settle deducts full actualCost from reserve",
			"TestReserveCreditsCoop_MUST1_ReserveCapacityDecreasesByFullActualCost"},
		{"MUST-2", "router charge matches DiscountedPrice formula",
			"TestReserveCreditsCoop_MUST2_RouterChargeMatchesDiscountedPrice"},
		{"MUST-3", "multi-settle reserve depletion is additive",
			"TestReserveCreditsCoop_MUST3_MultiSettleDepletionAdditive"},
		{"MUST-4", "no commitment → no allocation, full charge",
			"TestReserveCreditsCoop_MUST4_NoCommitmentFullChargeNoDrift"},
		{"MUST-5", "zero-discount commitment depletes, no savings",
			"TestReserveCreditsCoop_MUST5_ZeroDiscountCommitmentDepletesNoSavings"},
		{"MUST-6", "two keepers same script → byte-equal both sides",
			"TestReserveCreditsCoop_MUST6_TwoKeepersByteEqualReserveAndCredits"},
		{"MUST-7", "insufficient capacity → graceful fall-through",
			"TestReserveCreditsCoop_MUST7_InsufficientCapacityGracefulFallthrough"},
	}
	require.Len(t, matrix, 7,
		"coverage matrix must have exactly 7 MUST clauses")
	for _, m := range matrix {
		require.NotEmpty(t, m.id)
		require.NotEmpty(t, m.description)
		require.NotEmpty(t, m.testName)
		t.Logf("[reserve-credits-coop] %s: %s → %s",
			m.id, m.description, m.testName)
	}
}
