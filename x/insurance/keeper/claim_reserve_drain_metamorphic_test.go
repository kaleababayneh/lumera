
package keeper_test

import (
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// This file applies the testing-metamorphic skill to the
// COOPERATIVE CLAIM↔RESERVE drain pipeline within x/insurance.
//
// The "reserve" here is the insurance pool's RESERVED FUNDS
// accounting (PoolState.ReservedFunds). Approving a claim
// reserves capacity from AvailableFunds; paying it out drains
// TotalFunds and releases the reserve. The cooperative contract:
//
//   Approve(X):   Available -= X, Reserved += X       (AvailableFunds capacity → Reserved)
//   Pay(X, Y):    Reserved  -= X, Total -= Y, Available += (X - Y)
//                                              (Y is actual payout ≤ approved X)
//
// And at every step:
//
//   I_1: TotalFunds == AvailableFunds + ReservedFunds
//
// This pipeline drains the pool deterministically — the
// payout amount Y EXACTLY equals the TotalFunds drain. Any
// drift (over-drain via double-spend, under-drain via partial
// release leak) breaks the financial accounting.
//
// Seven MRs spanning the skill's six categories plus composite:
//
//   1. EQUIVALENCE:    pool conservation invariant Total ==
//                      Available + Reserved at every step
//   2. EQUIVALENCE:    full payout (Y=approved) drains Total by
//                      exactly approved amount
//   3. ADDITIVE:       K claims approved+paid → Σ drains == Σ payouts
//   4. PERMUTATIVE:    claim approval+pay order doesn't change
//                      final pool state for the same set
//   5. INVERTIVE:      partial payout (Y<X) recovers (X-Y) to
//                      Available; full payout recovers nothing
//   6. INCLUSIVE:      subset of N approved claims paid → only
//                      that subset drains Total; others remain
//                      Reserved
//   7. MULTIPLICATIVE: scaling all claim amounts by k scales
//                      the pool's final state by k

// --------------------------------------------------------------
// Helpers
// --------------------------------------------------------------

// poolFundsSnapshot extracts the three pool funds via ExportGenesis
// (the only public surface for inspecting internal pool state).
type poolFundsSnapshot struct {
	total     int64
	available int64
	reserved  int64
}

func snapshotPool(t *testing.T, fixture *keeperFixture) poolFundsSnapshot {
	t.Helper()
	gs := fixture.keeper.ExportGenesis(fixture.ctx)
	require.NotNil(t, gs.Pool)
	return poolFundsSnapshot{
		total:     decToInt64(t, gs.Pool.TotalFunds),
		available: decToInt64(t, gs.Pool.AvailableFunds),
		reserved:  decToInt64(t, gs.Pool.ReservedFunds),
	}
}

func decToInt64(t *testing.T, s string) int64 {
	t.Helper()
	if s == "" {
		return 0
	}
	d, err := decimal.NewFromString(s)
	require.NoError(t, err, "decimal parse %q", s)
	return d.IntPart()
}

// fileApprovePay walks one claim through the full lifecycle:
// FileClaim → ProcessClaim(approve, X) → ProcessPayout(claim, Y).
// Returns the claim ID after the lifecycle completes.
func fileApprovePay(
	t *testing.T,
	fixture *keeperFixture,
	receiptID, toolID, publisherID string,
	contributedAmount, approvedAmount, payoutAmount int64,
) string {
	t.Helper()
	recordContributionForTests(t, fixture, receiptID, toolID, publisherID, contributedAmount)

	fileMsg := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   receiptID,
		ToolId:      toolID,
		PublisherId: publisherID,
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: sdkmath.NewInt(approvedAmount).String(),
		},
		Reason: "metamorphic test",
	}
	claimID, err := fixture.keeper.FileClaim(fixture.ctx, fileMsg)
	require.NoError(t, err)

	authority := authtypes.NewModuleAddress("gov").String()
	require.NoError(t, fixture.keeper.ProcessClaim(fixture.ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: sdkmath.NewInt(approvedAmount).String(),
		},
	}))

	if payoutAmount > 0 {
		recipient := sdk.AccAddress("recipient_xxxxxxxxxxx").String()
		var amountField *basev1beta1.Coin
		if payoutAmount != approvedAmount {
			amountField = &basev1beta1.Coin{
				Denom:  "ulac",
				Amount: sdkmath.NewInt(payoutAmount).String(),
			}
		}
		msgServer := keeper.NewMsgServerImpl(fixture.keeper)
		_, err := msgServer.ProcessPayout(fixture.ctx, &types.MsgProcessPayout{
			Authority: authority,
			ClaimId:   claimID,
			Recipient: recipient,
			TxHash:    "tx-" + receiptID,
			Amount:    amountField,
		})
		require.NoError(t, err)
	}
	return claimID
}

// fileApproveOnly walks a claim through FileClaim → ProcessClaim
// (approve), without the payout step. Used to test the
// "approved-but-unpaid" reserved state.
func fileApproveOnly(
	t *testing.T,
	fixture *keeperFixture,
	receiptID, toolID, publisherID string,
	contributedAmount, approvedAmount int64,
) string {
	t.Helper()
	return fileApprovePay(t, fixture, receiptID, toolID, publisherID,
		contributedAmount, approvedAmount, 0)
}

// payApprovedClaim runs the payout for an already-approved claim.
func payApprovedClaim(t *testing.T, fixture *keeperFixture, claimID string, payoutAmount int64) {
	t.Helper()
	authority := authtypes.NewModuleAddress("gov").String()
	amountField := &basev1beta1.Coin{
		Denom:  "ulac",
		Amount: sdkmath.NewInt(payoutAmount).String(),
	}
	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	_, err := msgServer.ProcessPayout(fixture.ctx, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   claimID,
		Recipient: sdk.AccAddress("recipient_xxxxxxxxxxx").String(),
		TxHash:    "tx-" + claimID,
		Amount:    amountField,
	})
	require.NoError(t, err)
}

// --------------------------------------------------------------
// MR 1 (EQUIVALENCE): Total = Available + Reserved at every step
// --------------------------------------------------------------

// TestClaimReserveDrain_MR_PoolConservationInvariantHoldsEveryStep
// pins the load-bearing accounting invariant: at any instant,
// TotalFunds == AvailableFunds + ReservedFunds. A regression
// that double-credited a release or skipped a deduction would
// trip here.
func TestClaimReserveDrain_MR_PoolConservationInvariantHoldsEveryStep(t *testing.T) {
	t.Parallel()
	fixture := setupKeeperTest(t)
	fundPoolForTests(t, fixture, 100_000)

	checkInvariant := func(label string) {
		s := snapshotPool(t, fixture)
		require.Equal(t, s.total, s.available+s.reserved,
			"%s: TotalFunds=%d ≠ AvailableFunds(%d) + ReservedFunds(%d)",
			label, s.total, s.available, s.reserved)
	}

	checkInvariant("after_initial_funding")

	// 5 approval+pay cycles with varied amounts.
	scenarios := []struct {
		contrib, approved, payout int64
	}{
		{500, 500, 500},   // full payout
		{1000, 800, 600},  // partial payout
		{2000, 1500, 0},   // approved but not paid yet
		{750, 750, 750},   // full
		{1200, 1000, 100}, // significantly partial
	}
	for i, sc := range scenarios {
		_ = fileApprovePay(t, fixture,
			fmt.Sprintf("mr1-receipt-%d", i),
			fmt.Sprintf("mr1-tool-%d", i),
			fmt.Sprintf("mr1-pub-%d", i),
			sc.contrib, sc.approved, sc.payout)
		checkInvariant(fmt.Sprintf("after_step_%d", i))
	}
}

// --------------------------------------------------------------
// MR 2 (EQUIVALENCE): Full payout drains Total by exactly approved
// --------------------------------------------------------------

// TestClaimReserveDrain_MR_FullPayoutDrainsExactly pins that
// when payout == approved, TotalFunds drains by exactly the
// approved amount. A regression that double-charged or
// under-drained would trip.
func TestClaimReserveDrain_MR_FullPayoutDrainsExactly(t *testing.T) {
	t.Parallel()
	fixture := setupKeeperTest(t)
	fundPoolForTests(t, fixture, 50_000)

	preTotal := snapshotPool(t, fixture).total

	const approved int64 = 5_000
	_ = fileApprovePay(t, fixture, "mr2-receipt", "mr2-tool", "mr2-pub",
		approved, approved, approved)

	postTotal := snapshotPool(t, fixture).total

	// The recorded contribution adds `approved` to the pool, so
	// the net pool change is contributions added then payout
	// drained: net = +approved (contribution) - approved (payout) = 0.
	// But the contribution-for-tests injects a separate amount.
	// The KEY invariant: payout drained TotalFunds by exactly
	// the payout amount.
	require.Equal(t, preTotal+approved-approved, postTotal,
		"MR-2: full payout=%d should yield net pool delta of "+
			"+contribution(%d) - payout(%d) = 0; preTotal=%d postTotal=%d",
		approved, approved, approved, preTotal, postTotal)
}

// --------------------------------------------------------------
// MR 3 (ADDITIVE): K claims drain Total by Σ payouts
// --------------------------------------------------------------

// TestClaimReserveDrain_MR_KClaimsDrainAdditive pins that K
// independent claims approved+paid drain TotalFunds by exactly
// Σ payouts. A regression batching claims with a hidden surcharge
// would trip.
func TestClaimReserveDrain_MR_KClaimsDrainAdditive(t *testing.T) {
	t.Parallel()
	fixture := setupKeeperTest(t)
	fundPoolForTests(t, fixture, 200_000)

	pre := snapshotPool(t, fixture)

	scenarios := []struct {
		contrib, approved, payout int64
	}{
		{1000, 1000, 1000},
		{2000, 1500, 1500},
		{500, 500, 250},
		{3000, 2500, 1000},
		{1500, 1500, 1500},
	}
	var totalContrib, totalPayout int64
	for i, sc := range scenarios {
		_ = fileApprovePay(t, fixture,
			fmt.Sprintf("mr3-receipt-%d", i),
			fmt.Sprintf("mr3-tool-%d", i),
			fmt.Sprintf("mr3-pub-%d", i),
			sc.contrib, sc.approved, sc.payout)
		totalContrib += sc.contrib
		totalPayout += sc.payout
	}

	post := snapshotPool(t, fixture)
	expectedTotalDelta := totalContrib - totalPayout
	require.Equal(t, pre.total+expectedTotalDelta, post.total,
		"MR-3: pool drain mismatch: pre=%d post=%d expected_delta=%d "+
			"(contrib=%d, payout=%d)",
		pre.total, post.total, expectedTotalDelta, totalContrib, totalPayout)
}

// --------------------------------------------------------------
// MR 4 (PERMUTATIVE): Approval+pay order doesn't change final state
// --------------------------------------------------------------

// TestClaimReserveDrain_MR_OrderingDoesNotAffectFinalState pins
// that processing the same set of claims in different orders
// produces IDENTICAL final pool state. A regression introducing
// per-claim ordering effects would trip.
func TestClaimReserveDrain_MR_OrderingDoesNotAffectFinalState(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		contrib, approved, payout int64
	}{
		{1000, 800, 800},
		{2000, 1500, 1000},
		{500, 500, 250},
		{3000, 3000, 2500},
	}

	runWith := func(order []int) poolFundsSnapshot {
		fixture := setupKeeperTest(t)
		fundPoolForTests(t, fixture, 100_000)
		for _, idx := range order {
			sc := scenarios[idx]
			_ = fileApprovePay(t, fixture,
				fmt.Sprintf("mr4-receipt-%d", idx),
				fmt.Sprintf("mr4-tool-%d", idx),
				fmt.Sprintf("mr4-pub-%d", idx),
				sc.contrib, sc.approved, sc.payout)
		}
		return snapshotPool(t, fixture)
	}

	forwardOrder := runWith([]int{0, 1, 2, 3})
	reverseOrder := runWith([]int{3, 2, 1, 0})
	shuffled := runWith([]int{2, 0, 3, 1})

	require.Equal(t, forwardOrder.total, reverseOrder.total,
		"MR-4: total funds order-dependent forward=%d reverse=%d",
		forwardOrder.total, reverseOrder.total)
	require.Equal(t, forwardOrder.available, reverseOrder.available,
		"MR-4: available funds order-dependent")
	require.Equal(t, forwardOrder.reserved, reverseOrder.reserved,
		"MR-4: reserved funds order-dependent")
	require.Equal(t, forwardOrder.total, shuffled.total,
		"MR-4: total funds order-dependent forward=%d shuffled=%d",
		forwardOrder.total, shuffled.total)
	require.Equal(t, forwardOrder.available, shuffled.available,
		"MR-4: available funds shuffled-dependent")
	require.Equal(t, forwardOrder.reserved, shuffled.reserved,
		"MR-4: reserved funds shuffled-dependent")
}

// --------------------------------------------------------------
// MR 5 (INVERTIVE): Partial payout recovers (X-Y) to Available
// --------------------------------------------------------------

// TestClaimReserveDrain_MR_PartialPayoutRecoversUnusedReserve
// pins that a partial payout (Y < X approved) returns the
// unused (X - Y) portion to AvailableFunds. A regression that
// dropped the unused amount on the floor would trip.
func TestClaimReserveDrain_MR_PartialPayoutRecoversUnusedReserve(t *testing.T) {
	t.Parallel()
	fixture := setupKeeperTest(t)
	fundPoolForTests(t, fixture, 50_000)

	pre := snapshotPool(t, fixture)

	const contrib int64 = 10_000
	const approved int64 = 5_000
	const payout int64 = 2_000

	_ = fileApprovePay(t, fixture, "mr5-receipt", "mr5-tool", "mr5-pub",
		contrib, approved, payout)

	post := snapshotPool(t, fixture)

	// Expected post-state:
	// Total: pre.total + contrib - payout
	// Available: pre.available + contrib - payout
	//   (gained contrib via genesis re-init, then approve took
	//    approved, then full release returned (approved - payout))
	// Reserved: pre.reserved (released back fully)
	require.Equal(t, pre.total+contrib-payout, post.total,
		"MR-5 total: pre=%d, expected delta=%d (contrib(%d)-payout(%d)), got delta=%d",
		pre.total, contrib-payout, contrib, payout, post.total-pre.total)
	require.Equal(t, pre.reserved, post.reserved,
		"MR-5 reserved: full release should leave reserved=pre=%d, got %d",
		pre.reserved, post.reserved)

	// The "unused recovery" amount: approved - payout.
	expectedRecovery := approved - payout
	require.Equal(t, int64(3_000), expectedRecovery,
		"prereq: scenario expects recovery of 3000")

	// Available delta = contrib - payout (genesis funding +
	// contribution, then -approved from reserve, then +(approved-payout)
	// returned).
	expectedAvailDelta := contrib - payout
	require.Equal(t, expectedAvailDelta, post.available-pre.available,
		"MR-5 available delta: expected=%d got=%d",
		expectedAvailDelta, post.available-pre.available)
}

// --------------------------------------------------------------
// MR 6 (INCLUSIVE): Subset paid → only subset drains Total
// --------------------------------------------------------------

// TestClaimReserveDrain_MR_SubsetPaidDrainsOnlySubset pins that
// when N claims are approved but only a subset K is paid, only
// the K paid claims drain TotalFunds; the other (N-K) remain
// Reserved (capacity locked but funds not yet drained). A
// regression that drained on approval (not pay) would trip.
func TestClaimReserveDrain_MR_SubsetPaidDrainsOnlySubset(t *testing.T) {
	t.Parallel()
	fixture := setupKeeperTest(t)
	fundPoolForTests(t, fixture, 100_000)

	pre := snapshotPool(t, fixture)

	// Approve 5 claims, pay only the first 3.
	const claimAmount int64 = 1_000
	const claimContrib int64 = 1_000
	claimIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		claimIDs[i] = fileApproveOnly(t, fixture,
			fmt.Sprintf("mr6-receipt-%d", i),
			fmt.Sprintf("mr6-tool-%d", i),
			fmt.Sprintf("mr6-pub-%d", i),
			claimContrib, claimAmount)
	}

	afterApprove := snapshotPool(t, fixture)
	// All 5 are reserved.
	require.Equal(t, pre.reserved+5*claimAmount, afterApprove.reserved,
		"MR-6 prereq: 5 approved should reserve 5×%d", claimAmount)
	// TotalFunds gained from contributions only (not yet drained).
	require.Equal(t, pre.total+5*claimContrib, afterApprove.total,
		"MR-6 prereq: TotalFunds should grow only by contributions, not approvals")

	// Pay only the first 3.
	for i := 0; i < 3; i++ {
		payApprovedClaim(t, fixture, claimIDs[i], claimAmount)
	}

	post := snapshotPool(t, fixture)
	// Only 3 payouts drained TotalFunds.
	require.Equal(t, afterApprove.total-3*claimAmount, post.total,
		"MR-6: only 3/5 paid → drain by 3×%d, got total=%d expected=%d",
		claimAmount, post.total, afterApprove.total-3*claimAmount)
	// Reserved holds 2 unreleased (5 - 3 paid).
	require.Equal(t, pre.reserved+2*claimAmount, post.reserved,
		"MR-6: reserved should hold 2 unpaid claims")
}

// --------------------------------------------------------------
// MR 7 (MULTIPLICATIVE): Scaling claim amounts scales pool state
// --------------------------------------------------------------

// TestClaimReserveDrain_MR_ScalingClaimAmountsScalesPoolState
// pins that scaling all claim amounts by k scales the pool's
// final state proportionally (modulo fixed funding offsets).
// Tests linearity of the drain accounting under amount changes.
func TestClaimReserveDrain_MR_ScalingClaimAmountsScalesPoolState(t *testing.T) {
	t.Parallel()

	runScenario := func(k int64) (int64, int64) {
		fixture := setupKeeperTest(t)
		fundPoolForTests(t, fixture, 100_000)
		// 3 claims with k-scaled amounts.
		var totalContrib, totalPayout int64
		for i := 0; i < 3; i++ {
			contrib := 500 * k
			approved := 500 * k
			payout := 500 * k
			_ = fileApprovePay(t, fixture,
				fmt.Sprintf("mr7-receipt-k%d-%d", k, i),
				fmt.Sprintf("mr7-tool-%d", i),
				fmt.Sprintf("mr7-pub-%d", i),
				contrib, approved, payout)
			totalContrib += contrib
			totalPayout += payout
		}
		s := snapshotPool(t, fixture)
		return s.total, totalContrib - totalPayout
	}

	for _, k := range []int64{1, 2, 5, 10} {
		total, expectedDelta := runScenario(k)
		// All payouts equal contributions in this scenario, so
		// expectedDelta=0 → pool stays at funding baseline.
		require.Equal(t, int64(0), expectedDelta,
			"prereq: payout=contrib for k=%d", k)
		require.Equal(t, int64(100_000), total,
			"MR-7 k=%d: pool total should remain at funding baseline "+
				"(payout=contrib, net delta=0); got %d",
			k, total)
	}

	// Asymmetric scenario: scale payouts but not contributions.
	runAsym := func(k int64) int64 {
		fixture := setupKeeperTest(t)
		fundPoolForTests(t, fixture, 200_000)
		for i := 0; i < 3; i++ {
			contrib := int64(1_000)
			approved := 500 * k
			payout := 500 * k
			_ = fileApprovePay(t, fixture,
				fmt.Sprintf("mr7-asym-k%d-%d", k, i),
				fmt.Sprintf("mr7-asym-tool-%d", i),
				fmt.Sprintf("mr7-asym-pub-%d", i),
				contrib, approved, payout)
		}
		return snapshotPool(t, fixture).total
	}

	totalK1 := runAsym(1)
	totalK2 := runAsym(2)
	// Delta from baseline grows linearly with k.
	const baseline int64 = 200_000
	const fixedContrib int64 = 3 * 1_000
	deltaK1 := baseline + fixedContrib - totalK1 // = payout for k=1 = 3*500 = 1500
	deltaK2 := baseline + fixedContrib - totalK2 // = payout for k=2 = 3*1000 = 3000
	require.Equal(t, int64(1_500), deltaK1,
		"MR-7 asym k=1: drain should be 3×500=1500, got %d", deltaK1)
	require.Equal(t, int64(3_000), deltaK2,
		"MR-7 asym k=2: drain should be 3×1000=3000, got %d", deltaK2)
	require.Equal(t, deltaK2, deltaK1*2,
		"MR-7 linearity: deltaK2(%d) should equal 2×deltaK1(%d)",
		deltaK2, deltaK1)
}

// --------------------------------------------------------------
// Composite MR: cross-scenario determinism check
// --------------------------------------------------------------

// TestClaimReserveDrain_MR_CompositeDeterminismAcrossKeepers
// runs the SAME mixed claim script on two independent keeper
// fixtures and asserts the final pool state is byte-equal. This
// is the cross-validator consensus contract: claim-drain
// accounting must be deterministic.
func TestClaimReserveDrain_MR_CompositeDeterminismAcrossKeepers(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		contrib, approved, payout int64
	}{
		{500, 500, 250},
		{1500, 1000, 1000},
		{2000, 1500, 750},
		{1000, 1000, 0},   // approved but unpaid
		{750, 500, 500},
	}

	runStack := func() poolFundsSnapshot {
		fixture := setupKeeperTest(t)
		fundPoolForTests(t, fixture, 100_000)
		for i, sc := range scenarios {
			_ = fileApprovePay(t, fixture,
				fmt.Sprintf("composite-receipt-%d", i),
				fmt.Sprintf("composite-tool-%d", i),
				fmt.Sprintf("composite-pub-%d", i),
				sc.contrib, sc.approved, sc.payout)
		}
		return snapshotPool(t, fixture)
	}

	stackA := runStack()
	stackB := runStack()

	require.Equal(t, stackA.total, stackB.total,
		"composite MR: total diverges across keepers A=%d B=%d",
		stackA.total, stackB.total)
	require.Equal(t, stackA.available, stackB.available,
		"composite MR: available diverges A=%d B=%d",
		stackA.available, stackB.available)
	require.Equal(t, stackA.reserved, stackB.reserved,
		"composite MR: reserved diverges A=%d B=%d",
		stackA.reserved, stackB.reserved)
}
