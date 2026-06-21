package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file applies the testing-metamorphic skill to LOCK +
// UNLOCK ATOMICITY under "concurrent" same-block operations.
// In Cosmos SDK there's no goroutine concurrency at the keeper
// layer; "concurrent" means MULTIPLE LOCK/UNLOCK CALLS WITHIN
// THE SAME BLOCK from one or many routers.
//
// The ESCROW ACCUMULATOR invariant: at any moment, the module
// account's bank balance for the credit denom EQUALS the sum
// of Amount over all locks with Status=ACTIVE. A refactor
// introducing a race or partial-write would break this
// conservation.
//
// Builds on:
//   - tick 29 lock_unlock_conservation_test.go (round-trip
//     identity, partition, double-unlock rejection)
//   - tick 28 expire_locks_metamorphic_test.go (sweep timing)
//
// THIS FILE adds: concurrent (same-block) interleaved Lock+
// Unlock atomicity. The accumulator must be CORRECT after
// every step regardless of how operations are interleaved.

// computeActiveLockedSum sums Amount over all ACTIVE locks in
// state. This is the EXPECTED escrow balance.
func computeActiveLockedSum(t *testing.T, ctx sdk.Context, k *Keeper) int64 {
	t.Helper()
	var sum int64
	require.NoError(t, k.state.Locks.Walk(ctx, nil, func(_ string, lock *types.Lock) (bool, error) {
		if lock != nil && lock.Status == types.LockStatus_LOCK_STATUS_ACTIVE && lock.Amount != nil {
			amt, ok := sdkmath.NewIntFromString(lock.Amount.Amount)
			if !ok {
				return false, nil
			}
			sum += amt.Int64()
		}
		return false, nil
	}))
	return sum
}

// --------------------------------------------------------------
// MR 1: ESCROW ACCUMULATOR CONSERVATION INVARIANT
// --------------------------------------------------------------

// TestLockUnlockAtomicity_MR_EscrowEqualsSumOfActiveLocksAfterEachStep
// pins the CORE accumulator invariant: at every step of a
// multi-Lock multi-Unlock sequence, the module's escrow
// balance EQUALS the sum of Amounts over all currently-ACTIVE
// locks. A race that double-credited or lost funds would
// surface here.
func TestLockUnlockAtomicity_MR_EscrowEqualsActiveLockSumAtEveryStep(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	// Multiple routers each fund a wallet.
	const routerCount = 4
	routers := make([]sdk.AccAddress, routerCount)
	for i := 0; i < routerCount; i++ {
		r := makeFundedRouter(t, bank, accKeeper, 5_000_000)
		routers[i] = r
	}

	// Sequence of operations (all in same block — same ctx).
	type op struct {
		kind    string // "lock" | "unlock"
		router  int    // index into routers
		amount  int64  // for lock
		lockIdx int    // for unlock — index into per-router locks tracker
	}
	script := []op{
		{kind: "lock", router: 0, amount: 100_000},
		{kind: "lock", router: 1, amount: 200_000},
		{kind: "lock", router: 0, amount: 50_000},
		{kind: "unlock", router: 0, lockIdx: 0}, // unlock first router's first lock
		{kind: "lock", router: 2, amount: 300_000},
		{kind: "lock", router: 3, amount: 400_000},
		{kind: "unlock", router: 1, lockIdx: 0},
		{kind: "lock", router: 0, amount: 75_000},
		{kind: "unlock", router: 2, lockIdx: 0},
		{kind: "unlock", router: 3, lockIdx: 0},
	}

	perRouterLocks := make(map[int][]string)
	for i := 0; i < routerCount; i++ {
		perRouterLocks[i] = []string{}
	}

	denom := types.DefaultCreditDenom

	for stepIdx, o := range script {
		switch o.kind {
		case "lock":
			quoteID := "quote-acc-" + string(rune('a'+stepIdx))
			lockID, err := keeper.LockCredits(ctx, routers[o.router].String(),
				"sess-acc",
				sdk.NewInt64Coin(denom, o.amount),
				"tool-acc", quoteID, "v1", "intent-acc")
			require.NoError(t, err, "step %d", stepIdx)
			perRouterLocks[o.router] = append(perRouterLocks[o.router], lockID)
		case "unlock":
			lockID := perRouterLocks[o.router][o.lockIdx]
			require.NoError(t, keeper.UnlockCredits(ctx, lockID, "test"),
				"step %d", stepIdx)
		}

		// MR check: escrow == sum of active-lock amounts.
		moduleBalance := bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
		expectedSum := computeActiveLockedSum(t, ctx, keeper)
		require.Equal(t, expectedSum, moduleBalance,
			"step %d (%s router=%d): escrow=%d but sum-of-active-"+
				"locks=%d. Accumulator corruption — atomicity "+
				"violated.", stepIdx, o.kind, o.router, moduleBalance,
			expectedSum)
	}

	// Final: all unlocks done for routers 0,1,2,3 first lock; but
	// router 0 has 2 active locks remaining (50k and 75k).
	finalActiveSum := computeActiveLockedSum(t, ctx, keeper)
	finalEscrow := bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
	require.Equal(t, finalActiveSum, finalEscrow,
		"FINAL: escrow == active-sum holds end-to-end")
	require.Equal(t, int64(50_000+75_000), finalActiveSum,
		"FINAL active-locked-sum == 125k (router 0's two unsettled)")
}

// --------------------------------------------------------------
// MR 2: TOTAL SUPPLY PRESERVED ACROSS OPERATION SEQUENCE
// --------------------------------------------------------------

// TestLockUnlockAtomicity_MR_TotalSupplyPreservedThroughout pins
// the strongest conservation invariant: across an N-operation
// sequence, the SUM of (router1 + router2 + ... + module
// escrow) is EXACTLY preserved at every intermediate step.
// Catches any coin creation or destruction (mint/burn bug).
func TestLockUnlockAtomicity_MR_TotalSupplyPreservedAtEveryStep(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	const routerCount = 3
	routers := make([]sdk.AccAddress, routerCount)
	for i := 0; i < routerCount; i++ {
		routers[i] = makeFundedRouter(t, bank, accKeeper, 5_000_000)
	}

	denom := types.DefaultCreditDenom

	totalSupply := func() int64 {
		var sum int64
		for _, r := range routers {
			sum += bank.GetBalance(ctx, r, denom).Amount.Int64()
		}
		sum += bank.GetBalance(ctx, moduleAddr, denom).Amount.Int64()
		return sum
	}

	startSupply := totalSupply()
	require.Equal(t, int64(routerCount*5_000_000), startSupply,
		"precondition: supply == sum of router fundings")

	// Run a 12-step interleaved Lock+Unlock script.
	type op struct {
		kind    string
		router  int
		amount  int64
		lockIdx int
	}
	script := []op{
		{kind: "lock", router: 0, amount: 100_000},
		{kind: "lock", router: 1, amount: 200_000},
		{kind: "unlock", router: 0, lockIdx: 0},
		{kind: "lock", router: 2, amount: 50_000},
		{kind: "lock", router: 0, amount: 75_000},
		{kind: "unlock", router: 1, lockIdx: 0},
		{kind: "lock", router: 1, amount: 150_000},
		{kind: "unlock", router: 0, lockIdx: 1},
		{kind: "unlock", router: 2, lockIdx: 0},
		{kind: "lock", router: 0, amount: 25_000},
		{kind: "unlock", router: 1, lockIdx: 1},
		{kind: "unlock", router: 0, lockIdx: 2},
	}

	perRouterLocks := make(map[int][]string)

	for stepIdx, o := range script {
		switch o.kind {
		case "lock":
			quoteID := "quote-supply-" + string(rune('a'+stepIdx))
			lockID, err := keeper.LockCredits(ctx, routers[o.router].String(),
				"sess-supply",
				sdk.NewInt64Coin(denom, o.amount),
				"tool-supply", quoteID,
				"v1", "intent-supply")
			require.NoError(t, err)
			perRouterLocks[o.router] = append(perRouterLocks[o.router], lockID)
		case "unlock":
			lockID := perRouterLocks[o.router][o.lockIdx]
			require.NoError(t, keeper.UnlockCredits(ctx, lockID, "test"))
		}

		// Conservation check after every step.
		require.Equal(t, startSupply, totalSupply(),
			"step %d (%s): total supply changed from %d. "+
				"Coin creation or destruction detected — atomicity "+
				"break.", stepIdx, o.kind, startSupply)
	}
}

// --------------------------------------------------------------
// MR 3: PER-ROUTER PARTITIONING UNDER INTERLEAVE
// --------------------------------------------------------------

// TestLockUnlockAtomicity_MR_PerRouterPartitioningUnderRace pins
// that interleaved Lock+Unlock from MULTIPLE routers don't
// cross-affect each other. Each router's net balance change
// is exactly equal to (sum of THEIR completed Locks - sum of
// THEIR completed Unlocks). No leakage between routers.
func TestLockUnlockAtomicity_MR_PerRouterPartitioningUnderInterleave(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	const routerCount = 3
	routers := make([]sdk.AccAddress, routerCount)
	startBalances := make([]int64, routerCount)
	for i := 0; i < routerCount; i++ {
		routers[i] = makeFundedRouter(t, bank, accKeeper, 5_000_000)
		startBalances[i] = bank.GetBalance(ctx, routers[i], types.DefaultCreditDenom).Amount.Int64()
	}

	// Interleaved script: track per-router unique-locked-still-
	// active total. After all unlocks, balance returns to start
	// minus any still-locked.
	type op struct {
		kind    string
		router  int
		amount  int64
		lockIdx int
	}
	script := []op{
		{kind: "lock", router: 0, amount: 100_000},
		{kind: "lock", router: 1, amount: 200_000},
		{kind: "lock", router: 2, amount: 300_000},
		{kind: "unlock", router: 1, lockIdx: 0},
		{kind: "lock", router: 0, amount: 50_000},
		{kind: "unlock", router: 0, lockIdx: 0}, // unlock router 0's FIRST lock
		{kind: "unlock", router: 2, lockIdx: 0},
		// Final state: router 0 has 1 active lock (50k); router 1
		// none; router 2 none.
	}
	perRouterLocks := make(map[int][]string)

	for stepIdx, o := range script {
		switch o.kind {
		case "lock":
			quoteID := "quote-part-" + string(rune('a'+stepIdx))
			lockID, err := keeper.LockCredits(ctx, routers[o.router].String(),
				"sess-part", sdk.NewInt64Coin(types.DefaultCreditDenom, o.amount),
				"tool-part", quoteID, "v1", "intent-part")
			require.NoError(t, err)
			perRouterLocks[o.router] = append(perRouterLocks[o.router], lockID)
		case "unlock":
			lockID := perRouterLocks[o.router][o.lockIdx]
			require.NoError(t, keeper.UnlockCredits(ctx, lockID, "test"))
		}
	}

	// Expected per-router net change:
	// router 0: locked 100k+50k, unlocked 100k → net -50k (1 active)
	// router 1: locked 200k, unlocked 200k → net 0
	// router 2: locked 300k, unlocked 300k → net 0
	expectedDelta := []int64{-50_000, 0, 0}
	for i := 0; i < routerCount; i++ {
		current := bank.GetBalance(ctx, routers[i], types.DefaultCreditDenom).Amount.Int64()
		actualDelta := current - startBalances[i]
		assert.Equal(t, expectedDelta[i], actualDelta,
			"router %d: balance delta %d != expected %d. Per-"+
				"router partitioning violated under interleaved "+
				"Lock+Unlock — funds leaked across routers.",
			i, actualDelta, expectedDelta[i])
	}
}

// --------------------------------------------------------------
// MR 4: DOUBLE-UNLOCK SAFETY UNDER INTERLEAVE
// --------------------------------------------------------------

// TestLockUnlockAtomicity_MR_DoubleUnlockUnderInterleaveRejected
// pins that even WITHIN an interleaved sequence of OTHER
// router's operations, attempting to double-unlock a specific
// lock fails with the SAME error as in isolation. The
// rejection guard (ErrLockInactive at :1447-1449) is not
// affected by other state changes happening in between.
func TestLockUnlockAtomicity_MR_DoubleUnlockUnderInterleaveStillRejected(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerA := makeFundedRouter(t, bank, accKeeper, 1_000_000)
	routerB := makeFundedRouter(t, bank, accKeeper, 1_000_000)

	denom := types.DefaultCreditDenom

	lockA, err := keeper.LockCredits(ctx, routerA.String(), "sess-A",
		sdk.NewInt64Coin(denom, 100_000),
		"tool-A", "quote-A", "v1", "intent-A")
	require.NoError(t, err)

	lockB, err := keeper.LockCredits(ctx, routerB.String(), "sess-B",
		sdk.NewInt64Coin(denom, 200_000),
		"tool-B", "quote-B", "v1", "intent-B")
	require.NoError(t, err)

	// Unlock A.
	require.NoError(t, keeper.UnlockCredits(ctx, lockA, "first-cancel"))

	// Inter-leave: lock another from B (different router; should
	// not affect A's status).
	lockB2, err := keeper.LockCredits(ctx, routerB.String(), "sess-B2",
		sdk.NewInt64Coin(denom, 50_000),
		"tool-B2", "quote-B2", "v1", "intent-B2")
	require.NoError(t, err)

	// Double-unlock A (after B's interleaved op): MUST fail.
	err = keeper.UnlockCredits(ctx, lockA, "duplicate-cancel")
	assert.Error(t, err,
		"double-unlock A MUST fail even after B's interleaved "+
			"operations changed state. Pins that ErrLockInactive "+
			"guard at :1447-1449 is state-independent — other "+
			"locks' lifecycle changes do not affect A's already-"+
			"released status.")

	// B and B2 still consistent.
	require.NoError(t, keeper.UnlockCredits(ctx, lockB, "B-cancel"))
	require.NoError(t, keeper.UnlockCredits(ctx, lockB2, "B2-cancel"))

	// Final: routerA balance restored fully; routerB likewise.
	assert.Equal(t, int64(1_000_000),
		bank.GetBalance(ctx, routerA, denom).Amount.Int64(),
		"routerA fully restored")
	assert.Equal(t, int64(1_000_000),
		bank.GetBalance(ctx, routerB, denom).Amount.Int64(),
		"routerB fully restored")
}

// --------------------------------------------------------------
// MR 5: CROSS-KEEPER REPLAY DETERMINISM
// --------------------------------------------------------------

// TestLockUnlockAtomicity_MR_TwoKeepersIdenticalScriptIdenticalState
// pins that two independent keepers fed the same Lock+Unlock
// script produce IDENTICAL final state (escrow + per-router
// balances). Cross-validator consensus invariant.
func TestLockUnlockAtomicity_MR_TwoKeepersIdenticalScriptIdenticalState(t *testing.T) {
	ctxA, kA, bankA, modAddrA, accA := setupCreditsKeeper(t)
	ctxB, kB, bankB, modAddrB, accB := setupCreditsKeeper(t)

	denom := types.DefaultCreditDenom

	// newAccAddress is non-deterministic (uses random secp256k1
	// keys), so we pre-generate router addresses ONCE and use
	// them across both keepers. Fund + register independently
	// in each keeper.
	routers := []sdk.AccAddress{newAccAddress(), newAccAddress()}
	for _, r := range routers {
		accA.accounts[r.String()] = r
		accB.accounts[r.String()] = r
		bankA.FundAccount(r, sdk.NewCoins(sdk.NewInt64Coin(denom, 2_000_000)))
		bankB.FundAccount(r, sdk.NewCoins(sdk.NewInt64Coin(denom, 2_000_000)))
	}
	routersA := routers
	routersB := routers

	type op struct {
		kind    string
		router  int
		amount  int64
		lockIdx int
	}
	script := []op{
		{kind: "lock", router: 0, amount: 100_000},
		{kind: "lock", router: 1, amount: 200_000},
		{kind: "lock", router: 0, amount: 50_000},
		{kind: "unlock", router: 0, lockIdx: 0},
		{kind: "unlock", router: 1, lockIdx: 0},
	}

	perRouterLocksA := make(map[int][]string)
	perRouterLocksB := make(map[int][]string)

	for stepIdx, o := range script {
		switch o.kind {
		case "lock":
			// Distinct quote ID per Lock so the idempotency
			// check at LockCredits doesn't dedupe with prior
			// locks (which would return the same lockID and
			// break the perRouterLocks indexing).
			quoteID := "quote-c-" + string(rune('a'+stepIdx))
			idA, err := kA.LockCredits(ctxA, routersA[o.router].String(),
				"sess-conform", sdk.NewInt64Coin(denom, o.amount),
				"tool-c", quoteID, "v1", "intent-c")
			require.NoError(t, err)
			idB, err := kB.LockCredits(ctxB, routersB[o.router].String(),
				"sess-conform", sdk.NewInt64Coin(denom, o.amount),
				"tool-c", quoteID, "v1", "intent-c")
			require.NoError(t, err)
			require.Equal(t, idA, idB,
				"lock IDs match across keepers for same op")
			perRouterLocksA[o.router] = append(perRouterLocksA[o.router], idA)
			perRouterLocksB[o.router] = append(perRouterLocksB[o.router], idB)
		case "unlock":
			require.NoError(t, kA.UnlockCredits(ctxA,
				perRouterLocksA[o.router][o.lockIdx], "test"))
			require.NoError(t, kB.UnlockCredits(ctxB,
				perRouterLocksB[o.router][o.lockIdx], "test"))
		}
	}

	// Final state byte-equal.
	for i := 0; i < 2; i++ {
		balA := bankA.GetBalance(ctxA, routersA[i], denom).Amount.Int64()
		balB := bankB.GetBalance(ctxB, routersB[i], denom).Amount.Int64()
		require.Equal(t, balA, balB,
			"router %d balance diverges across keepers a=%d b=%d",
			i, balA, balB)
	}
	escrowA := bankA.GetBalance(ctxA, modAddrA, denom).Amount.Int64()
	escrowB := bankB.GetBalance(ctxB, modAddrB, denom).Amount.Int64()
	require.Equal(t, escrowA, escrowB,
		"module escrow diverges across keepers a=%d b=%d",
		escrowA, escrowB)
}
