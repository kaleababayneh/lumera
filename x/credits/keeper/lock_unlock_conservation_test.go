package keeper

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file adds METAMORPHIC tests for Lock↔Unlock COIN
// CONSERVATION in x/credits.
//
// LockCredits (keeper.go:1320-1433) escrows coins from the router
// into the module account. UnlockCredits (keeper.go:1437-1503)
// returns the full locked Amount from the module back to the router.
// These two operations form a round-trip that must be COIN-
// CONSERVATIVE: no coins created, no coins destroyed.
//
// Existing TestUnlockCredits_Success pins ONE happy-path round-
// trip with a single lock at a single denom. This file adds MRs
// covering the broader conservation invariants that callers
// (storefront, router, insurance) rely on:
//
//   EQUIVALENCE (round-trip identity):
//     - RouterBalanceRoundTripPreservedAcrossLockUnlock
//     - ModuleEscrowIsZeroAfterRoundTrip
//
//   CONSERVATION (sum in = sum out):
//     - UnlockReturnsExactlyLockedAmount
//     - SameRouterMultipleLocksEscrowSum
//     - MultipleRoutersBalancesPartitioned
//
//   EQUIVALENCE (cancel vs expire):
//     - CancelUnlockAndExpireUnlockPreserveSameBalance
//
//   INCLUSIVE (idempotence / error path):
//     - UnlockTwiceRejectedPreservesConservation
//
// A refactor introducing a fee, partial refund, rounding, or
// forgotten bank call would surface here as a balance mismatch.

// snapshotBalances captures bank balances for a set of addresses
// in the given denom. Used to assert balance conservation across
// operations.
func snapshotBalances(
	t *testing.T,
	bank *mockBankKeeper,
	ctx sdk.Context,
	denom string,
	addrs ...sdk.AccAddress,
) map[string]int64 {
	t.Helper()
	snap := make(map[string]int64, len(addrs))
	for _, a := range addrs {
		snap[a.String()] = bank.GetBalance(ctx, a, denom).Amount.Int64()
	}
	return snap
}

// makeFundedRouter creates a router address, funds it with the
// given amount of DefaultCreditDenom, and registers it with the
// account keeper. Returns the address.
func makeFundedRouter(t *testing.T, bank *mockBankKeeper, accKeeper *mockAccountKeeper, amount int64) sdk.AccAddress {
	t.Helper()
	addr := newAccAddress()
	accKeeper.accounts[addr.String()] = addr
	bank.FundAccount(addr, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, amount)))
	return addr
}

// --------------------------------------------------------------
// MR — EQUIVALENCE: round-trip identity
// --------------------------------------------------------------

// TestLockUnlockConservation_MR_RouterBalanceRoundTripPreserved
// is the canonical identity pin: for any Lock→Unlock cycle, the
// router's balance is IDENTICAL to its pre-lock balance.
func TestLockUnlockConservation_MR_RouterBalanceRoundTripIsIdentity(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	const initialFunding = int64(1_000_000)
	router := makeFundedRouter(t, bank, accKeeper, initialFunding)

	before := snapshotBalances(t, bank, ctx, types.DefaultCreditDenom, router)
	require.Equal(t, initialFunding, before[router.String()])

	// Lock → Unlock round-trip.
	lockID, err := keeper.LockCredits(ctx, router.String(), "session-rt",
		sdk.NewInt64Coin(types.DefaultCreditDenom, 250_000),
		"tool-rt", "quote-rt", "v1", "intent-rt")
	require.NoError(t, err)
	require.NoError(t, keeper.UnlockCredits(ctx, lockID, "cancelled"))

	after := snapshotBalances(t, bank, ctx, types.DefaultCreditDenom, router)

	assert.Equal(t, before[router.String()], after[router.String()],
		"MR round-trip identity: router balance MUST be identical "+
			"pre-lock and post-unlock. before=%d after=%d. Pins that "+
			"Lock+Unlock is COIN-CONSERVATIVE end-to-end — a refactor "+
			"introducing a fee or burn on either op would surface as "+
			"a balance mismatch here.",
		before[router.String()], after[router.String()])
}

// TestLockUnlockConservation_MR_ModuleEscrowZeroAfterUnlock pins
// the mirror: the module's escrow balance is zero after Unlock
// (all coins returned). A leak here would accumulate over time.
func TestLockUnlockConservation_MR_ModuleEscrowZeroAfterUnlock(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	router := makeFundedRouter(t, bank, accKeeper, 500_000)

	require.Equal(t, int64(0),
		bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom).Amount.Int64(),
		"precondition: module escrow is zero")

	lockID, err := keeper.LockCredits(ctx, router.String(), "session-m",
		sdk.NewInt64Coin(types.DefaultCreditDenom, 123_456),
		"tool-m", "quote-m", "v1", "intent-m")
	require.NoError(t, err)

	// Mid-cycle: escrow holds the locked amount.
	midEscrow := bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom).Amount.Int64()
	assert.Equal(t, int64(123_456), midEscrow,
		"mid-cycle: module holds the locked amount in escrow")

	require.NoError(t, keeper.UnlockCredits(ctx, lockID, "cancelled"))

	afterEscrow := bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom).Amount.Int64()
	assert.Equal(t, int64(0), afterEscrow,
		"MR escrow-zero: module escrow MUST be zero after unlock. "+
			"Pins the complementary side of the router round-trip — "+
			"a refactor forgetting to return coins on the unlock "+
			"path would leave escrow non-zero (silent leak).")
}

// --------------------------------------------------------------
// MR — CONSERVATION: sum in = sum out
// --------------------------------------------------------------

// TestLockUnlockConservation_MR_UnlockReturnsExactlyLockedAmount
// pins the pointwise conservation per lock: for each lock L with
// Amount A, UnlockCredits(L) returns EXACTLY A coins — no more,
// no less. A refactor inserting a rounding or fee step would
// surface here.
func TestLockUnlockConservation_MR_UnlockReturnsExactLockAmount(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	router := makeFundedRouter(t, bank, accKeeper, 10_000_000)

	// Lock several distinct amounts; unlock; verify each return
	// amount exactly matches its lock.
	amounts := []int64{1, 100, 12345, 999_999}
	for i, amt := range amounts {
		beforeRouter := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()

		lockID, err := keeper.LockCredits(ctx, router.String(),
			"session-x"+string(rune('0'+i)),
			sdk.NewInt64Coin(types.DefaultCreditDenom, amt),
			"tool-x", "quote-x"+string(rune('0'+i)), "v1", "intent-x")
		require.NoError(t, err, "lock %d", i)

		midRouter := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()
		require.Equal(t, beforeRouter-amt, midRouter,
			"router loses exactly amt after lock")

		require.NoError(t, keeper.UnlockCredits(ctx, lockID, "cancelled"))

		afterRouter := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()
		assert.Equal(t, beforeRouter, afterRouter,
			"MR exact-amount: amt=%d locked, amt=%d returned on "+
				"unlock. before=%d mid=%d after=%d. Pins no-fee-no-"+
				"loss conservation at the per-lock level.",
			amt, amt, beforeRouter, midRouter, afterRouter)
	}
}

// TestLockUnlockConservation_MR_SameRouterMultipleLocksEscrowSum
// pins that N concurrent locks by the SAME router escrow their
// sum. Ensures no partial-escrow bug (e.g., second lock
// overwrites first escrow reference).
func TestLockUnlockConservation_MR_SameRouterMultipleLocksEscrowSum(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	router := makeFundedRouter(t, bank, accKeeper, 10_000_000)
	initialRouter := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()

	amounts := []int64{100_000, 250_000, 75_000}
	var sum int64
	lockIDs := make([]string, 0, 3)
	for i, amt := range amounts {
		id, err := keeper.LockCredits(ctx, router.String(),
			"session-sum"+string(rune('0'+i)),
			sdk.NewInt64Coin(types.DefaultCreditDenom, amt),
			"tool-sum", "quote-sum"+string(rune('0'+i)), "v1", "intent-sum")
		require.NoError(t, err)
		lockIDs = append(lockIDs, id)
		sum += amt
	}

	// Escrow equals sum of all 3 amounts.
	escrow := bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom).Amount.Int64()
	assert.Equal(t, sum, escrow,
		"MR sum-escrow: module holds sum(locked) = %d, not any "+
			"individual amount. Pins no-overwrite — a refactor that "+
			"indexed escrow by router (overwriting per-lock state) "+
			"would hold only the latest amount here.",
		sum)

	// Router balance decreased by the sum.
	postLockRouter := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()
	assert.Equal(t, initialRouter-sum, postLockRouter,
		"router balance decreased by sum of locked amounts")

	// Unlock all → full return.
	for _, id := range lockIDs {
		require.NoError(t, keeper.UnlockCredits(ctx, id, "cancelled"))
	}

	finalRouter := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()
	assert.Equal(t, initialRouter, finalRouter,
		"after unlocking all 3, router balance fully restored")
	assert.Equal(t, int64(0),
		bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom).Amount.Int64(),
		"escrow drains to zero")
}

// TestLockUnlockConservation_MR_MultipleRoutersBalancesPartitioned
// pins that each router's balance changes ONLY by their own
// lock/unlock activity — no cross-router interference. Critical
// for multi-tenant correctness.
func TestLockUnlockConservation_MR_MultipleRoutersPartitioned(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	rA := makeFundedRouter(t, bank, accKeeper, 500_000)
	rB := makeFundedRouter(t, bank, accKeeper, 700_000)
	rC := makeFundedRouter(t, bank, accKeeper, 300_000)

	before := snapshotBalances(t, bank, ctx, types.DefaultCreditDenom, rA, rB, rC)

	// Each router locks a distinct amount.
	lockA, err := keeper.LockCredits(ctx, rA.String(), "session-A",
		sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000),
		"tool-x", "quote-A", "v1", "intent-A")
	require.NoError(t, err)

	lockB, err := keeper.LockCredits(ctx, rB.String(), "session-B",
		sdk.NewInt64Coin(types.DefaultCreditDenom, 200_000),
		"tool-x", "quote-B", "v1", "intent-B")
	require.NoError(t, err)

	_, err = keeper.LockCredits(ctx, rC.String(), "session-C",
		sdk.NewInt64Coin(types.DefaultCreditDenom, 50_000),
		"tool-x", "quote-C", "v1", "intent-C")
	require.NoError(t, err)

	// Mid: each router decreased by OWN amount only.
	mid := snapshotBalances(t, bank, ctx, types.DefaultCreditDenom, rA, rB, rC)
	assert.Equal(t, before[rA.String()]-100_000, mid[rA.String()],
		"router A decreased by 100k only, not affected by B/C locks")
	assert.Equal(t, before[rB.String()]-200_000, mid[rB.String()],
		"router B decreased by 200k only")
	assert.Equal(t, before[rC.String()]-50_000, mid[rC.String()],
		"router C decreased by 50k only")

	// Unlock A and B only. C remains locked.
	require.NoError(t, keeper.UnlockCredits(ctx, lockA, "cancelled"))
	require.NoError(t, keeper.UnlockCredits(ctx, lockB, "cancelled"))

	after := snapshotBalances(t, bank, ctx, types.DefaultCreditDenom, rA, rB, rC)
	assert.Equal(t, before[rA.String()], after[rA.String()],
		"MR partition: A fully restored by own unlock")
	assert.Equal(t, before[rB.String()], after[rB.String()],
		"MR partition: B fully restored by own unlock")
	assert.Equal(t, before[rC.String()]-50_000, after[rC.String()],
		"MR partition: C STILL short its locked amount — not "+
			"accidentally refunded by A or B's unlocks. A refactor "+
			"that iterated all locks on any unlock would cross-"+
			"refund and silently corrupt multi-tenant accounting.")
}

// --------------------------------------------------------------
// MR — EQUIVALENCE: cancel-unlock ≡ expire-unlock
// --------------------------------------------------------------

// TestLockUnlockConservation_MR_CancelAndExpirePreserveSameBalance
// pins that the two unlock paths — explicit UnlockCredits call
// and automatic expire via ExpireLocks — leave the SAME balance
// state. Conservation holds regardless of which code path fires.
func TestLockUnlockConservation_MR_CancelVsExpirePreserveBalance(t *testing.T) {
	mkKeeper := func(reason string, useExpire bool) (rtBalance int64, initBalance int64) {
		ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

		// Short TTL for the expire path.
		params := keeper.GetParams(ctx)
		params.MaxLockTtlSeconds = 10
		params.DefaultLockTtlSeconds = 10
		require.NoError(t, keeper.SetParams(ctx, params))

		ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0).UTC())
		router := makeFundedRouter(t, bank, accKeeper, 1_000_000)
		initBalance = bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()

		lockID, err := keeper.LockCredits(ctx, router.String(),
			"session-"+reason,
			sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000),
			"tool", "quote-"+reason, "v1", "intent-"+reason)
		require.NoError(t, err)

		if useExpire {
			// Advance past expiry + ExpireLocks.
			ctx = ctx.WithBlockTime(ctx.BlockTime().Add(20 * time.Second))
			require.NoError(t, keeper.ExpireLocks(ctx, 0))
		} else {
			require.NoError(t, keeper.UnlockCredits(ctx, lockID, reason))
		}

		rtBalance = bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()
		return rtBalance, initBalance
	}

	cancelBalance, cancelInit := mkKeeper("cancelled", false)
	expireBalance, expireInit := mkKeeper("expired", true)

	assert.Equal(t, cancelInit, cancelBalance,
		"cancel path: router fully restored")
	assert.Equal(t, expireInit, expireBalance,
		"expire path: router fully restored")
	assert.Equal(t, cancelBalance, expireBalance,
		"MR path-equivalence: cancel-unlock and expire-unlock "+
			"return the SAME final router balance. Pins that "+
			"ExpireLocks's automatic UnlockCredits call runs the "+
			"identical coin-return code — a refactor diverging the "+
			"two would surface as asymmetric conservation.")
}

// --------------------------------------------------------------
// MR — INCLUSIVE: idempotence via rejection
// --------------------------------------------------------------

// TestLockUnlockConservation_MR_UnlockTwiceRejectedPreservesConservation
// pins that a second UnlockCredits on an already-released lock
// is REJECTED, and does not double-return coins. Pins the
// ErrLockInactive guard at :1447-1449 under a conservation lens.
func TestLockUnlockConservation_MR_DoubleUnlockRejectedNoDoubleReturn(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	router := makeFundedRouter(t, bank, accKeeper, 500_000)
	initBalance := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()

	lockID, err := keeper.LockCredits(ctx, router.String(), "session-dbl",
		sdk.NewInt64Coin(types.DefaultCreditDenom, 150_000),
		"tool-dbl", "quote-dbl", "v1", "intent-dbl")
	require.NoError(t, err)

	// First unlock — succeeds.
	require.NoError(t, keeper.UnlockCredits(ctx, lockID, "cancelled"))
	mid := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()
	require.Equal(t, initBalance, mid, "first unlock restores balance")

	// Second unlock — MUST fail with ErrLockInactive.
	err = keeper.UnlockCredits(ctx, lockID, "cancelled")
	require.Error(t, err,
		"MR idempotent-reject: double-unlock MUST fail. Pins "+
			":1447-1449 ErrLockInactive guard — a refactor that "+
			"processed it anyway would DOUBLE-RETURN coins, minting "+
			"free funds from the module account.")

	// Balance unchanged (no double-return).
	final := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64()
	assert.Equal(t, initBalance, final,
		"after rejected double-unlock: balance UNCHANGED. "+
			"Conservation holds — no free coins minted.")
}

// --------------------------------------------------------------
// MR — EQUIVALENCE: full lifecycle zero-sum
// --------------------------------------------------------------

// TestLockUnlockConservation_MR_FullLifecycleZeroSum pins that
// after an N-lock, N-unlock sequence, the TOTAL supply across
// router + module is preserved. The module is stateful through
// the lifecycle; the net must balance to zero delta.
func TestLockUnlockConservation_MR_FullLifecycleTotalSupplyPreserved(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	router := makeFundedRouter(t, bank, accKeeper, 5_000_000)

	// Capture total supply (router + module) at start.
	start := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64() +
		bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom).Amount.Int64()

	lockIDs := make([]string, 0, 4)
	for i, amt := range []int64{100_000, 200_000, 50_000, 350_000} {
		id, err := keeper.LockCredits(ctx, router.String(),
			"session-ls"+string(rune('0'+i)),
			sdk.NewInt64Coin(types.DefaultCreditDenom, amt),
			"tool-ls", "quote-ls"+string(rune('0'+i)), "v1", "intent-ls")
		require.NoError(t, err)
		lockIDs = append(lockIDs, id)

		// After each lock, total supply unchanged (router − amt +
		// module + amt = router + module pre-lock).
		inter := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64() +
			bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom).Amount.Int64()
		require.Equal(t, start, inter,
			"after lock %d: total supply preserved (no burn, no mint)", i)
	}

	for i, id := range lockIDs {
		require.NoError(t, keeper.UnlockCredits(ctx, id, "cancelled"))
		inter := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64() +
			bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom).Amount.Int64()
		require.Equal(t, start, inter,
			"after unlock %d: total supply preserved", i)
	}

	end := bank.GetBalance(ctx, router, types.DefaultCreditDenom).Amount.Int64() +
		bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom).Amount.Int64()

	assert.Equal(t, start, end,
		"MR total-supply-preserved: full N-lock+N-unlock lifecycle "+
			"preserves supply EXACTLY (neither burn nor mint). "+
			"start=%d end=%d. The strongest conservation pin — any "+
			"coin creation/destruction on EITHER path would surface.",
		start, end)
}
