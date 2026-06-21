package keeper

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file applies the testing-metamorphic skill to the
// MULTI-LOCKER stress angle of the credits fee-split / Unlock
// pipeline. It is deliberately complementary to
// lock_unlock_atomicity_metamorphic_test.go (tick 51):
//
//   tick 51 pinned:
//     - single-router + small-router-count (≤4) interleaved
//       Lock+Unlock accumulator invariants
//     - 5 MRs covering escrow accumulator, total supply,
//       partitioning, double-unlock, cross-keeper determinism
//
//   THIS FILE pins the FAN-OUT stress angle:
//     - 8-10 distinct routers × 8-10 locks each (up to 100 locks
//       per block, which no prior test exercises at that density)
//     - scale-invariance (MULTIPLICATIVE): doubling lockers OR
//       locks/locker should scale linearly in escrow
//     - interleaved vs partitioned execution (EQUIVALENCE):
//       round-robin cross-locker lock+unlock yields byte-equal
//       final state as sequential-by-locker execution
//     - subset-unlock partition (INCLUSIVE): when a subset of
//       lockers unlock, ONLY their balances recover; other
//       lockers' balances stay exactly untouched
//     - heavy-burst unlock commutativity (PERMUTATIVE): 100
//       locks across 10 routers unlocked in two different
//       orderings produce the identical final state
//
// The CORE invariant that every MR preserves post-operation:
//   moduleEscrow(denom) == Σ { Lock.Amount | Lock.Status == ACTIVE }
//
// And the cross-locker conservation:
//   Σ routerBalances(denom) + moduleEscrow(denom) == TotalSupply

// sumRouterBalances sums the credit-denom balance across a set
// of router addresses — the cross-locker accounting view.
func sumRouterBalances(t *testing.T, ctx sdk.Context, bank *mockBankKeeper, routers []sdk.AccAddress, denom string) int64 {
	t.Helper()
	var total int64
	for _, r := range routers {
		total += bank.GetBalance(ctx, r, denom).Amount.Int64()
	}
	return total
}

// --------------------------------------------------------------
// MR 1 (EQUIVALENCE): Fan-out full-unlock restores every locker
// exactly to their funded balance.
//
// Given N routers each holding M active locks, unlocking EVERY
// lock must: (1) drive escrow to zero, (2) restore each router's
// balance to exactly what they started with. A regression that
// rerouted unlock destination, skipped a lock, or double-credited
// would trip.
// --------------------------------------------------------------

func TestMultiLockerStress_MR_FanOutFullUnlockRestoresAllBalances(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	const routerCount = 8
	const locksPerRouter = 8
	const fundedAmount int64 = 10_000_000
	const lockAmount int64 = 100_000
	denom := types.DefaultCreditDenom

	routers := make([]sdk.AccAddress, routerCount)
	for i := 0; i < routerCount; i++ {
		routers[i] = makeFundedRouter(t, bank, accKeeper, fundedAmount)
	}

	perRouterLocks := make([][]string, routerCount)
	for i := 0; i < routerCount; i++ {
		perRouterLocks[i] = make([]string, 0, locksPerRouter)
	}

	// Create N × M locks: router i creates lock j with a distinct quoteID.
	for i := 0; i < routerCount; i++ {
		for j := 0; j < locksPerRouter; j++ {
			quoteID := fmt.Sprintf("fan-out-q-%d-%d", i, j)
			lockID, err := keeper.LockCredits(ctx, routers[i].String(),
				"sess-fan-out",
				sdk.NewInt64Coin(denom, lockAmount),
				"tool-fan-out", quoteID, "v1", "intent-fan-out")
			require.NoError(t, err, "create lock i=%d j=%d", i, j)
			perRouterLocks[i] = append(perRouterLocks[i], lockID)
		}
	}

	// Verify pre-unlock state: every router has (funded - M × lockAmount),
	// escrow has exactly N × M × lockAmount.
	for i, r := range routers {
		expected := fundedAmount - int64(locksPerRouter)*lockAmount
		got := bank.GetBalance(ctx, r, denom).Amount.Int64()
		require.Equal(t, expected, got,
			"pre-unlock router %d: expected=%d got=%d", i, expected, got)
	}
	expectedEscrow := int64(routerCount) * int64(locksPerRouter) * lockAmount
	require.Equal(t, expectedEscrow, bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64(),
		"pre-unlock escrow should be N×M×lockAmount")

	// Unlock ALL N×M locks — traverse router-major.
	for i := 0; i < routerCount; i++ {
		for j := 0; j < locksPerRouter; j++ {
			require.NoError(t, keeper.UnlockCredits(ctx, perRouterLocks[i][j], "fan-out-test"),
				"unlock i=%d j=%d", i, j)
		}
	}

	// Post-unlock: escrow zero; every router restored to funded.
	finalEscrow := bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
	require.Equal(t, int64(0), finalEscrow,
		"MR-1: post-full-unlock escrow=%d expected 0 — some lock "+
			"didn't fully release its funds", finalEscrow)
	for i, r := range routers {
		got := bank.GetBalance(ctx, r, denom).Amount.Int64()
		require.Equal(t, fundedAmount, got,
			"MR-1: router %d balance=%d expected=%d — a lock "+
				"didn't fully restore to original locker",
			i, got, fundedAmount)
	}

	// Cross-invariant: sum of active locks is zero.
	require.Equal(t, int64(0), computeActiveLockedSum(t, ctx, keeper),
		"MR-1 double-check: all locks should be Status=RELEASED")
}

// --------------------------------------------------------------
// MR 2 (MULTIPLICATIVE): Doubling router count scales escrow
// linearly.
//
// Two scenarios — one with N routers, one with 2N routers, each
// holding M locks of equal amount. The 2N scenario's escrow must
// equal exactly 2 × N's escrow. A regression that introduced per-
// router bias (e.g. first locker pays extra) would trip.
// --------------------------------------------------------------

func TestMultiLockerStress_MR_ScalingLinearInRouterCount(t *testing.T) {
	const baseRouterCount = 4
	const doubleRouterCount = 8
	const locksPerRouter = 5
	const lockAmount int64 = 75_000
	denom := types.DefaultCreditDenom

	runScenario := func(routerCount int) int64 {
		ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)
		for i := 0; i < routerCount; i++ {
			r := makeFundedRouter(t, bank, accKeeper, 10_000_000)
			for j := 0; j < locksPerRouter; j++ {
				quoteID := fmt.Sprintf("scale-q-%d-%d", i, j)
				_, err := keeper.LockCredits(ctx, r.String(),
					"sess-scale", sdk.NewInt64Coin(denom, lockAmount),
					"tool-scale", quoteID, "v1", "intent-scale")
				require.NoError(t, err)
			}
		}
		return bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
	}

	baseEscrow := runScenario(baseRouterCount)
	doubleEscrow := runScenario(doubleRouterCount)

	expectedBase := int64(baseRouterCount) * int64(locksPerRouter) * lockAmount
	require.Equal(t, expectedBase, baseEscrow,
		"MR-2 prereq base: escrow should be N×M×amount")
	require.Equal(t, 2*baseEscrow, doubleEscrow,
		"MR-2: doubling routers should double escrow: base=%d double=%d "+
			"— ratio diverges from expected 1:2",
		baseEscrow, doubleEscrow)
}

// --------------------------------------------------------------
// MR 3 (EQUIVALENCE): Interleaved vs partitioned unlock produce
// identical final state.
//
// Script A: router 0 locks all, unlocks all, then router 1 does
//           same, etc (partitioned-by-locker).
// Script B: round-robin locking across all routers, then round-
//           robin unlocking (interleaved).
//
// Final state (per-router balances + escrow) must be IDENTICAL.
// This is the strongest partitioning contract — a regression that
// introduced cross-locker state leakage (e.g. shared lock counter)
// would trip.
// --------------------------------------------------------------

func TestMultiLockerStress_MR_InterleavedVsPartitionedEquivalence(t *testing.T) {
	const routerCount = 5
	const locksPerRouter = 4
	const lockAmount int64 = 50_000
	denom := types.DefaultCreditDenom

	// Script A: partitioned (router-major: for each router, lock
	// all + unlock all before moving to next).
	escrowA, perRouterBalA := func() (int64, []int64) {
		ctx, keeper, bank, modAddr, accKeeper := setupCreditsKeeper(t)
		routers := make([]sdk.AccAddress, routerCount)
		for i := 0; i < routerCount; i++ {
			routers[i] = makeFundedRouter(t, bank, accKeeper, 10_000_000)
		}
		for i := 0; i < routerCount; i++ {
			locks := make([]string, 0, locksPerRouter)
			for j := 0; j < locksPerRouter; j++ {
				quoteID := fmt.Sprintf("a-%d-%d", i, j)
				id, err := keeper.LockCredits(ctx, routers[i].String(),
					"sess-a", sdk.NewInt64Coin(denom, lockAmount),
					"tool-a", quoteID, "v1", "intent-a")
				require.NoError(t, err)
				locks = append(locks, id)
			}
			for _, id := range locks {
				require.NoError(t, keeper.UnlockCredits(ctx, id, "test-a"))
			}
		}
		escrow := bank.GetBalance(ctx, modAddr, denom).Amount.Int64()
		perRouterBal := make([]int64, routerCount)
		for i, r := range routers {
			perRouterBal[i] = bank.GetBalance(ctx, r, denom).Amount.Int64()
		}
		return escrow, perRouterBal
	}()

	// Script B: interleaved (round-robin lock-phase, then round-robin
	// unlock-phase).
	escrowB, perRouterBalB := func() (int64, []int64) {
		ctx, keeper, bank, modAddr, accKeeper := setupCreditsKeeper(t)
		routers := make([]sdk.AccAddress, routerCount)
		for i := 0; i < routerCount; i++ {
			routers[i] = makeFundedRouter(t, bank, accKeeper, 10_000_000)
		}
		perRouterLocks := make([][]string, routerCount)
		for i := 0; i < routerCount; i++ {
			perRouterLocks[i] = make([]string, 0, locksPerRouter)
		}
		for j := 0; j < locksPerRouter; j++ {
			for i := 0; i < routerCount; i++ {
				quoteID := fmt.Sprintf("b-%d-%d", i, j)
				id, err := keeper.LockCredits(ctx, routers[i].String(),
					"sess-b", sdk.NewInt64Coin(denom, lockAmount),
					"tool-b", quoteID, "v1", "intent-b")
				require.NoError(t, err)
				perRouterLocks[i] = append(perRouterLocks[i], id)
			}
		}
		for j := 0; j < locksPerRouter; j++ {
			for i := 0; i < routerCount; i++ {
				require.NoError(t, keeper.UnlockCredits(ctx,
					perRouterLocks[i][j], "test-b"))
			}
		}
		escrow := bank.GetBalance(ctx, modAddr, denom).Amount.Int64()
		perRouterBal := make([]int64, routerCount)
		for i, r := range routers {
			perRouterBal[i] = bank.GetBalance(ctx, r, denom).Amount.Int64()
		}
		return escrow, perRouterBal
	}()

	require.Equal(t, escrowA, escrowB,
		"MR-3: escrow differs across partitioned(%d) vs interleaved(%d)",
		escrowA, escrowB)
	for i := 0; i < routerCount; i++ {
		require.Equal(t, perRouterBalA[i], perRouterBalB[i],
			"MR-3: router %d balance differs: partitioned=%d interleaved=%d "+
				"— execution-order independence broken",
			i, perRouterBalA[i], perRouterBalB[i])
	}
}

// --------------------------------------------------------------
// MR 4 (INCLUSIVE): Subset-unlock partition — only the unlocking
// subset's balances change.
//
// N routers each hold M locks. A subset of size K unlocks all
// their locks. Only those K routers' balances recover; the
// remaining (N-K) routers' balances and lock state are EXACTLY
// unchanged. A regression that leaked across routers (e.g. shared
// session counter that got modified) would trip.
// --------------------------------------------------------------

func TestMultiLockerStress_MR_SubsetUnlockLeavesOthersUntouched(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	const routerCount = 6
	const locksPerRouter = 5
	const lockAmount int64 = 80_000
	const unlockingSubset = 3
	const fundedAmount int64 = 10_000_000
	denom := types.DefaultCreditDenom

	routers := make([]sdk.AccAddress, routerCount)
	for i := 0; i < routerCount; i++ {
		routers[i] = makeFundedRouter(t, bank, accKeeper, fundedAmount)
	}
	perRouterLocks := make([][]string, routerCount)
	for i := 0; i < routerCount; i++ {
		perRouterLocks[i] = make([]string, 0, locksPerRouter)
		for j := 0; j < locksPerRouter; j++ {
			quoteID := fmt.Sprintf("sub-q-%d-%d", i, j)
			id, err := keeper.LockCredits(ctx, routers[i].String(),
				"sess-sub", sdk.NewInt64Coin(denom, lockAmount),
				"tool-sub", quoteID, "v1", "intent-sub")
			require.NoError(t, err)
			perRouterLocks[i] = append(perRouterLocks[i], id)
		}
	}

	// Snapshot balances for the NON-unlocking routers.
	preBal := make([]int64, routerCount)
	for i, r := range routers {
		preBal[i] = bank.GetBalance(ctx, r, denom).Amount.Int64()
	}

	// Unlock only routers [0, K).
	for i := 0; i < unlockingSubset; i++ {
		for _, id := range perRouterLocks[i] {
			require.NoError(t, keeper.UnlockCredits(ctx, id, "sub-test"))
		}
	}

	// The unlocking subset should be restored to full funded amount.
	for i := 0; i < unlockingSubset; i++ {
		got := bank.GetBalance(ctx, routers[i], denom).Amount.Int64()
		require.Equal(t, fundedAmount, got,
			"MR-4: unlocking router %d balance=%d, expected funded=%d",
			i, got, fundedAmount)
	}

	// The non-unlocking routers MUST be byte-equal to their pre-state.
	for i := unlockingSubset; i < routerCount; i++ {
		got := bank.GetBalance(ctx, routers[i], denom).Amount.Int64()
		require.Equal(t, preBal[i], got,
			"MR-4: non-unlocking router %d balance changed from %d to "+
				"%d — cross-router state leakage",
			i, preBal[i], got)
	}

	// Escrow should have exactly the non-unlocking subset's locks.
	expectedEscrow := int64(routerCount-unlockingSubset) * int64(locksPerRouter) * lockAmount
	require.Equal(t, expectedEscrow, bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64(),
		"MR-4: escrow should be exactly the non-unlocking subset's locks")

	// Active-lock-sum matches escrow.
	require.Equal(t, expectedEscrow, computeActiveLockedSum(t, ctx, keeper),
		"MR-4: active-lock-sum must equal escrow after subset unlock")
}

// --------------------------------------------------------------
// MR 5 (PERMUTATIVE): Unlock order independence across 100 locks.
//
// 100 locks (10 routers × 10 locks) unlocked in two different
// orderings — forward (router-major, lock-index-minor) and
// reverse (router-minor, lock-index-major, all reversed). Final
// state must be byte-identical. This is the heaviest-concurrency
// check in this file; a subtle state-ordering bug would most
// likely surface here.
// --------------------------------------------------------------

func TestMultiLockerStress_MR_UnlockOrderIndependence100Locks(t *testing.T) {
	const routerCount = 10
	const locksPerRouter = 10
	const lockAmount int64 = 42_000
	const fundedAmount int64 = 20_000_000
	denom := types.DefaultCreditDenom

	runOrder := func(orderFn func(routers, locks int) [][2]int) (int64, []int64) {
		ctx, keeper, bank, modAddr, accKeeper := setupCreditsKeeper(t)
		routers := make([]sdk.AccAddress, routerCount)
		for i := 0; i < routerCount; i++ {
			routers[i] = makeFundedRouter(t, bank, accKeeper, fundedAmount)
		}
		perRouterLocks := make([][]string, routerCount)
		for i := 0; i < routerCount; i++ {
			perRouterLocks[i] = make([]string, 0, locksPerRouter)
			for j := 0; j < locksPerRouter; j++ {
				quoteID := fmt.Sprintf("ord-q-%d-%d", i, j)
				id, err := keeper.LockCredits(ctx, routers[i].String(),
					"sess-ord", sdk.NewInt64Coin(denom, lockAmount),
					"tool-ord", quoteID, "v1", "intent-ord")
				require.NoError(t, err)
				perRouterLocks[i] = append(perRouterLocks[i], id)
			}
		}
		for _, ij := range orderFn(routerCount, locksPerRouter) {
			require.NoError(t, keeper.UnlockCredits(ctx,
				perRouterLocks[ij[0]][ij[1]], "ord-test"))
		}
		escrow := bank.GetBalance(ctx, modAddr, denom).Amount.Int64()
		perRouter := make([]int64, routerCount)
		for i, r := range routers {
			perRouter[i] = bank.GetBalance(ctx, r, denom).Amount.Int64()
		}
		return escrow, perRouter
	}

	// Forward: (0,0) (0,1) ... (0,M-1) (1,0) ... (N-1,M-1)
	fwdEscrow, fwdBal := runOrder(func(routers, locks int) [][2]int {
		out := make([][2]int, 0, routers*locks)
		for i := 0; i < routers; i++ {
			for j := 0; j < locks; j++ {
				out = append(out, [2]int{i, j})
			}
		}
		return out
	})
	// Reverse: (N-1,M-1) ... (N-1,0) (N-2,M-1) ... (0,0)
	revEscrow, revBal := runOrder(func(routers, locks int) [][2]int {
		out := make([][2]int, 0, routers*locks)
		for i := routers - 1; i >= 0; i-- {
			for j := locks - 1; j >= 0; j-- {
				out = append(out, [2]int{i, j})
			}
		}
		return out
	})
	// Diagonal: (0,0) (1,1) ... (N-1,M-1) then (0,1) (1,2) ... wrapping
	diagEscrow, diagBal := runOrder(func(routers, locks int) [][2]int {
		out := make([][2]int, 0, routers*locks)
		for offset := 0; offset < locks; offset++ {
			for i := 0; i < routers; i++ {
				j := (i + offset) % locks
				out = append(out, [2]int{i, j})
			}
		}
		return out
	})

	require.Equal(t, fwdEscrow, revEscrow,
		"MR-5: escrow forward=%d reverse=%d — order-dependence",
		fwdEscrow, revEscrow)
	require.Equal(t, fwdEscrow, diagEscrow,
		"MR-5: escrow forward=%d diagonal=%d — order-dependence",
		fwdEscrow, diagEscrow)
	require.Equal(t, int64(0), fwdEscrow,
		"MR-5: all 100 locks unlocked should drive escrow to 0")

	for i := 0; i < routerCount; i++ {
		require.Equal(t, fwdBal[i], revBal[i],
			"MR-5: router %d forward=%d reverse=%d",
			i, fwdBal[i], revBal[i])
		require.Equal(t, fwdBal[i], diagBal[i],
			"MR-5: router %d forward=%d diagonal=%d",
			i, fwdBal[i], diagBal[i])
		require.Equal(t, fundedAmount, fwdBal[i],
			"MR-5: router %d must be fully restored to %d after all "+
				"locks unlocked; got %d",
			i, fundedAmount, fwdBal[i])
	}
}

// --------------------------------------------------------------
// MR 6 (EQUIVALENCE): Total-supply conservation across the
// multi-locker fan-out scenario.
//
// TotalSupply = Σ routerBalances + moduleEscrow must be EXACTLY
// preserved at every intermediate step of a 6-router × 6-lock
// stress sequence with interleaved Lock + Unlock. This catches
// any coin creation or destruction (mint/burn) that would slip
// past the single-router conservation MRs.
// --------------------------------------------------------------

func TestMultiLockerStress_MR_TotalSupplyConservationUnderFanOut(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	const routerCount = 6
	const locksPerRouter = 6
	const fundedAmount int64 = 10_000_000
	const lockAmount int64 = 150_000
	denom := types.DefaultCreditDenom

	routers := make([]sdk.AccAddress, routerCount)
	for i := 0; i < routerCount; i++ {
		routers[i] = makeFundedRouter(t, bank, accKeeper, fundedAmount)
	}
	initialTotal := sumRouterBalances(t, ctx, bank, routers, denom) +
		bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
	require.Equal(t, int64(routerCount)*fundedAmount, initialTotal,
		"prereq: total supply should be N × fundedAmount")

	perRouterLocks := make([][]string, routerCount)
	for i := 0; i < routerCount; i++ {
		perRouterLocks[i] = make([]string, 0, locksPerRouter)
	}

	// Interleaved script: each step is lock-or-unlock across routers.
	type op struct {
		kind    string
		router  int
		lockIdx int
	}
	script := []op{}
	// Round-robin lock phase (6 routers × 3 locks each = 18 ops).
	for j := 0; j < 3; j++ {
		for i := 0; i < routerCount; i++ {
			script = append(script, op{kind: "lock", router: i})
		}
	}
	// Partial unlock (router 0 unlocks first lock, router 2 unlocks first).
	script = append(script, op{kind: "unlock", router: 0, lockIdx: 0})
	script = append(script, op{kind: "unlock", router: 2, lockIdx: 0})
	// Second lock phase (another 3 × 6 = 18 ops).
	for j := 0; j < 3; j++ {
		for i := 0; i < routerCount; i++ {
			script = append(script, op{kind: "lock", router: i})
		}
	}
	// Final scattered unlocks.
	script = append(script, op{kind: "unlock", router: 1, lockIdx: 0})
	script = append(script, op{kind: "unlock", router: 5, lockIdx: 0})
	script = append(script, op{kind: "unlock", router: 3, lockIdx: 0})

	for stepIdx, o := range script {
		switch o.kind {
		case "lock":
			quoteID := fmt.Sprintf("cons-q-%d", stepIdx)
			id, err := keeper.LockCredits(ctx, routers[o.router].String(),
				"sess-cons", sdk.NewInt64Coin(denom, lockAmount),
				"tool-cons", quoteID, "v1", "intent-cons")
			require.NoError(t, err, "step %d lock", stepIdx)
			perRouterLocks[o.router] = append(perRouterLocks[o.router], id)
		case "unlock":
			id := perRouterLocks[o.router][o.lockIdx]
			require.NoError(t, keeper.UnlockCredits(ctx, id, "cons-unlock"),
				"step %d unlock", stepIdx)
		}

		// Total supply invariant at EVERY step.
		current := sumRouterBalances(t, ctx, bank, routers, denom) +
			bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
		require.Equal(t, initialTotal, current,
			"MR-6 step %d (%s router=%d): total supply drifted from %d "+
				"to %d — some step minted or burned coins",
			stepIdx, o.kind, o.router, initialTotal, current)

		// Escrow == sum of active locks.
		escrow := bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
		require.Equal(t, computeActiveLockedSum(t, ctx, keeper), escrow,
			"MR-6 step %d: escrow ≠ active-lock-sum", stepIdx)
	}
}

// --------------------------------------------------------------
// MR 7 (INVERTIVE): Re-Lock after Unlock returns funds to the
// SAME locker (round-trip).
//
// A router that unlocks a lock then immediately re-locks the
// same amount must end up with the same balance as the baseline
// (no-op). Pinning the round-trip identity across the unlock/
// relock boundary: Unlock+Relock should be a no-op for the
// router's balance AND the module's escrow.
// --------------------------------------------------------------

func TestMultiLockerStress_MR_UnlockRelockRoundTripNoOp(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	const routerCount = 5
	const lockAmount int64 = 125_000
	const fundedAmount int64 = 10_000_000
	denom := types.DefaultCreditDenom

	routers := make([]sdk.AccAddress, routerCount)
	lockIDs := make([]string, routerCount)
	for i := 0; i < routerCount; i++ {
		r := makeFundedRouter(t, bank, accKeeper, fundedAmount)
		routers[i] = r
		quoteID := fmt.Sprintf("rt-q-%d", i)
		id, err := keeper.LockCredits(ctx, r.String(),
			"sess-rt", sdk.NewInt64Coin(denom, lockAmount),
			"tool-rt", quoteID, "v1", "intent-rt")
		require.NoError(t, err)
		lockIDs[i] = id
	}

	// Baseline escrow + balances AFTER initial lock phase.
	baselineEscrow := bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
	baselineBal := make([]int64, routerCount)
	for i, r := range routers {
		baselineBal[i] = bank.GetBalance(ctx, r, denom).Amount.Int64()
	}

	// Each router: unlock, then re-lock the same amount with a
	// new quoteID. Net effect should be zero balance change.
	for i := 0; i < routerCount; i++ {
		require.NoError(t, keeper.UnlockCredits(ctx, lockIDs[i], "rt-unlock"))
		quoteID := fmt.Sprintf("rt-q-relock-%d", i)
		_, err := keeper.LockCredits(ctx, routers[i].String(),
			"sess-rt-relock", sdk.NewInt64Coin(denom, lockAmount),
			"tool-rt-relock", quoteID, "v1", "intent-rt-relock")
		require.NoError(t, err)
	}

	// Post-round-trip state MUST match baseline byte-for-byte.
	finalEscrow := bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
	require.Equal(t, baselineEscrow, finalEscrow,
		"MR-7: escrow changed across Unlock+Relock round-trip: "+
			"baseline=%d final=%d",
		baselineEscrow, finalEscrow)
	for i, r := range routers {
		got := bank.GetBalance(ctx, r, denom).Amount.Int64()
		require.Equal(t, baselineBal[i], got,
			"MR-7: router %d balance drifted across round-trip: "+
				"baseline=%d final=%d",
			i, baselineBal[i], got)
	}
}
