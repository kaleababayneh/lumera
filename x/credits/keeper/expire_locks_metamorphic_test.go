
package keeper

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file adds METAMORPHIC tests for ExpireLocks at
// keeper.go:1794-1880. Existing coverage:
//   - expire_locks_pending_bump_test.go: pins the pending-
//     settlement BUMP branch (happy path).
//   - lock_state_invariant_test.go: pins invariant checks.
//   - credits_comprehensive_test.go: smoke tests.
//
// What's NOT pinned:
//   - DETERMINISM: same input state + same `now` produces the
//     same output state (no non-deterministic lock ordering).
//   - MONOTONICITY IN TIME: later `now` sweeps ≥ locks than
//     earlier `now`.
//   - IDEMPOTENCE: calling ExpireLocks twice at the same now
//     is a no-op after the first call.
//   - BOUNDARY at lock.ExpiresAt == now: same composite-pair
//     range-bound quirk as x/reserve's releaseExpired (tick 26).
//     The bound EndInclusive((now, "")) excludes non-empty IDs
//     at exact time equality, making the effective boundary
//     exclusive at equality.
//   - LIMIT: limit parameter caps iteration.
//   - UNLOCK TRANSITION: post-expire, lock.Status = RELEASED,
//     lock.LastError = "expired", lock absent from LockExpiry,
//     lock present in FinalizedLocks.
//
// Apply testing-metamorphic skill. 4 MR categories covered.

// createLockWithTTL is a helper that creates a lock with the
// given TTL (shorter than params.MaxLockTtlSeconds — but note
// that keeper.LockCredits at :1363 ignores the caller's requested
// TTL and always uses MaxLockTtlSeconds). We set MaxLockTtlSeconds
// directly to produce the desired expiry.
func createLockWithTTL(
	t *testing.T,
	ctx sdk.Context,
	keeper *Keeper,
	bank *mockBankKeeper,
	accKeeper *mockAccountKeeper,
	ttl time.Duration,
	lockAmount int64,
	sessionID, toolID string,
) (string, time.Time) {
	t.Helper()
	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	coin := sdk.NewInt64Coin(types.DefaultCreditDenom, lockAmount)
	bank.FundAccount(routerAddr, sdk.NewCoins(coin))

	// Set params.MaxLockTtlSeconds so LockCredits uses our
	// desired TTL (LockCredits pulls ttl from MaxLockTtlSeconds
	// at keeper.go:1363). Also set Default to match so the
	// default-cannot-exceed-max validation passes.
	params := keeper.GetParams(ctx)
	params.MaxLockTtlSeconds = uint32(ttl.Seconds())
	params.DefaultLockTtlSeconds = uint32(ttl.Seconds())
	require.NoError(t, keeper.SetParams(ctx, params))

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		sessionID,
		coin,
		toolID,
		"quote-"+sessionID,
		"policy@v1",
		"intent-"+sessionID,
	)
	require.NoError(t, err)

	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	return lockID, lock.ExpiresAt.AsTime()
}

// lockInExpiryIndex reports whether the lock is in LockExpiry.
func lockInExpiryIndex(
	t *testing.T,
	ctx sdk.Context,
	keeper *Keeper,
	lockID string,
	expiresAt time.Time,
) bool {
	t.Helper()
	has, err := keeper.state.LockExpiry.Has(ctx, collections.Join(expiresAt, lockID))
	require.NoError(t, err)
	return has
}

// lockStatus returns the lock's current Status or nil if the
// lock is missing. Also returns the LastError field for
// expire-reason assertions.
func lockStatus(
	t *testing.T,
	ctx sdk.Context,
	keeper *Keeper,
	lockID string,
) (types.LockStatus, string, bool) {
	t.Helper()
	lock, found := keeper.GetLock(ctx, lockID)
	if !found {
		return 0, "", false
	}
	return lock.Status, lock.LastError, true
}

// --------------------------------------------------------------
// MR — DETERMINISM: single expire behaves deterministically
// --------------------------------------------------------------

// TestExpireLocks_MR_UnlockTransitionDeterministic is the CORE
// unlock-at-expiry pin. After ExpireLocks runs at a time past
// the lock's ExpiresAt, the lock MUST have:
//   - Status == RELEASED (from ACTIVE)
//   - LastError == "expired"
//   - Be absent from LockExpiry
//   - Be present in FinalizedLocks at the block time
//
// This is deterministic — every run with the same inputs
// produces the same outputs.
func TestExpireLocks_MR_UnlockTransitionDeterministicAtExpiry(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Now().UTC().Truncate(time.Second)
	ctx = ctx.WithBlockTime(baseTime)

	// Create a lock with TTL=10s.
	lockID, expiresAt := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
		10*time.Second, 1_000_000, "session-determ", "tool-determ")

	// Precondition: lock is ACTIVE and in expiry index.
	status, _, found := lockStatus(t, ctx, keeper, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, status)
	require.True(t, lockInExpiryIndex(t, ctx, keeper, lockID, expiresAt))

	// Advance time past expiry + run ExpireLocks.
	sweepTime := expiresAt.Add(5 * time.Second)
	ctx = ctx.WithBlockTime(sweepTime)
	require.NoError(t, keeper.ExpireLocks(ctx, 0))

	// Post-conditions (deterministic unlock transition):
	status, lastError, found := lockStatus(t, ctx, keeper, lockID)
	require.True(t, found, "lock record persists post-expire (status flip)")
	assert.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, status,
		"MR unlock-deterministic: ExpireLocks transitions Status "+
			"from ACTIVE to RELEASED. Pins UnlockCredits:1472 — a "+
			"refactor using a different status (e.g., EXPIRED) "+
			"would change downstream classifier semantics.")
	assert.Equal(t, "expired", lastError,
		"LastError == 'expired' — pins the reason string passed "+
			"to UnlockCredits at :1868. Downstream dashboards use "+
			"this string for breakdown analytics.")

	// Expiry index removed.
	assert.False(t, lockInExpiryIndex(t, ctx, keeper, lockID, expiresAt),
		"lock removed from LockExpiry post-expire (Unlock"+
			"Credits:1480-1482)")

	// FinalizedLocks indexed at sweepTime.
	inFinalized, err := keeper.state.FinalizedLocks.Has(
		ctx, collections.Join(sweepTime, lockID))
	require.NoError(t, err)
	assert.True(t, inFinalized,
		"lock indexed in FinalizedLocks at sweep BlockTime. Pins "+
			"UnlockCredits:1486-1488 — downstream pruning relies "+
			"on this index.")
}

// --------------------------------------------------------------
// MR — MONOTONICITY IN TIME
// --------------------------------------------------------------

// TestExpireLocks_MR_MonotonicInTime pins that later `now`
// unlocks ≥ locks than earlier `now`. Two independent keepers
// with identical lock setups — one swept earlier, one later.
// The later keeper's unlocked-count ≥ the earlier's.
func TestExpireLocks_MR_MonotonicInTime(t *testing.T) {
	mkKeeper := func() (sdk.Context, *Keeper, *mockBankKeeper, *mockAccountKeeper, []string, []time.Time) {
		ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)
		baseTime := time.Unix(1_700_000_000, 0).UTC()
		ctx = ctx.WithBlockTime(baseTime)

		// Set MaxLockTtlSeconds WIDE enough to handle staggered
		// TTLs up to 50s.
		params := keeper.GetParams(ctx)
		params.MaxLockTtlSeconds = 50
		params.DefaultLockTtlSeconds = 10
		require.NoError(t, keeper.SetParams(ctx, params))

		// Create 5 locks with staggered expiry: +10s, +20s, +30s, +40s, +50s.
		ids := make([]string, 0, 5)
		expiries := make([]time.Time, 0, 5)
		for i := 1; i <= 5; i++ {
			ttl := time.Duration(i*10) * time.Second
			id, exp := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
				ttl, 100_000, "session-m"+string(rune('0'+i)), "tool-m")
			ids = append(ids, id)
			expiries = append(expiries, exp)
		}
		return ctx, keeper, bank, accKeeper, ids, expiries
	}

	ctxEarly, kEarly, _, _, idsEarly, _ := mkKeeper()
	ctxLate, kLate, _, _, idsLate, _ := mkKeeper()

	// Advance kEarly to base+25s — should unlock 2 (TTLs 10s, 20s).
	baseTime := ctxEarly.BlockTime()
	ctxEarly = ctxEarly.WithBlockTime(baseTime.Add(25 * time.Second))
	require.NoError(t, kEarly.ExpireLocks(ctxEarly, 0))

	// Advance kLate to base+100s — should unlock ALL 5.
	ctxLate = ctxLate.WithBlockTime(ctxLate.BlockTime().Add(100 * time.Second))
	require.NoError(t, kLate.ExpireLocks(ctxLate, 0))

	// Count RELEASED locks in each keeper.
	releasedEarly := 0
	for _, id := range idsEarly {
		status, _, found := lockStatus(t, ctxEarly, kEarly, id)
		require.True(t, found)
		if status == types.LockStatus_LOCK_STATUS_RELEASED {
			releasedEarly++
		}
	}
	releasedLate := 0
	for _, id := range idsLate {
		status, _, found := lockStatus(t, ctxLate, kLate, id)
		require.True(t, found)
		if status == types.LockStatus_LOCK_STATUS_RELEASED {
			releasedLate++
		}
	}

	assert.GreaterOrEqual(t, releasedLate, releasedEarly,
		"MR monotonic-in-time: later now unlocks ≥ locks than "+
			"earlier. releasedEarly=%d releasedLate=%d. Pins that "+
			"the range bound walks monotonically from Min upward.",
		releasedEarly, releasedLate)
	assert.Equal(t, 2, releasedEarly,
		"at base+25s: 10s and 20s TTL locks swept (2 total)")
	assert.Equal(t, 5, releasedLate,
		"at base+100s: all 5 swept")
}

// --------------------------------------------------------------
// MR — BOUNDARY at ExpiresAt == now (empty-id-suffix quirk)
// --------------------------------------------------------------

// TestExpireLocks_MR_BoundaryAtExactExpiryIsNotSwept pins the
// counter-intuitive boundary: the range bound
// `EndExclusive(collections.Join(currentTime, ""))` is exclusive at
// the lowest pair key for currentTime, and a real lock key
// `(currentTime, "lock-42")` sorts GREATER THAN `(currentTime, "")`
// because "lock-42" > "". So at EXACTLY ExpiresAt == now, the lock
// is NOT swept — it's swept only once `now` > ExpiresAt.
//
// A refactor switching to `PairPrefix(currentTime)` or similar
// "cleaner" range bound would flip this and prematurely expire
// at-tick-boundary locks. Pin the current effective-exclusive-
// at-equality semantics.
func TestExpireLocks_MR_BoundaryAtExactExpiryIsNotSweptDueToEmptyIDSuffix(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)

	lockID, expiresAt := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
		10*time.Second, 500_000, "session-bdy", "tool-bdy")

	// Advance to EXACTLY ExpiresAt.
	ctx = ctx.WithBlockTime(expiresAt)
	require.NoError(t, keeper.ExpireLocks(ctx, 0))

	// Lock should STILL be ACTIVE (not swept at equality).
	status, _, found := lockStatus(t, ctx, keeper, lockID)
	require.True(t, found)
	assert.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, status,
		"MR boundary: lock at ExpiresAt == now is NOT swept. Pins "+
			"keeper range bound `EndExclusive((now, \"\"))` semantics — "+
			"the empty-string suffix excludes equality. A refactor using "+
			"PairPrefix(now) would flip this and sweep at-tick-boundary "+
			"locks prematurely.")
	assert.True(t, lockInExpiryIndex(t, ctx, keeper, lockID, expiresAt),
		"lock remains in LockExpiry index (not swept)")
}

// TestExpireLocks_MR_BoundaryOneNanoPastExpiryIsSwept pins the
// mirror: now = ExpiresAt + 1ns → swept. Pins that the boundary
// is tight at the nanosecond level.
func TestExpireLocks_MR_BoundaryOneNanoPastExpiryIsSwept(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)

	lockID, expiresAt := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
		10*time.Second, 500_000, "session-nano", "tool-nano")

	// Advance to ExpiresAt + 1ns.
	ctx = ctx.WithBlockTime(expiresAt.Add(time.Nanosecond))
	require.NoError(t, keeper.ExpireLocks(ctx, 0))

	status, lastError, found := lockStatus(t, ctx, keeper, lockID)
	require.True(t, found)
	assert.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, status,
		"MR boundary strict: at ExpiresAt + 1ns, lock IS swept. "+
			"Pins the minimum-advance required to trip the boundary.")
	assert.Equal(t, "expired", lastError)
}

// --------------------------------------------------------------
// MR — IDEMPOTENCE
// --------------------------------------------------------------

// TestExpireLocks_MR_SweepIdempotentAtSameNow pins that calling
// ExpireLocks twice at the same `now` produces the same final
// state and no error on the second call. Pins "best-effort
// cleanup" contract — EndBlocker calls every block regardless
// of whether anything expired.
func TestExpireLocks_MR_SweepIdempotentAtSameNow(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)

	lockID, expiresAt := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
		10*time.Second, 300_000, "session-idem", "tool-idem")

	ctx = ctx.WithBlockTime(expiresAt.Add(time.Second))

	// First sweep.
	require.NoError(t, keeper.ExpireLocks(ctx, 0))
	statusAfterFirst, _, _ := lockStatus(t, ctx, keeper, lockID)
	require.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, statusAfterFirst)

	// Second sweep at SAME now: no error, no change.
	require.NoError(t, keeper.ExpireLocks(ctx, 0),
		"MR idempotence: repeat sweep at same now returns nil")

	statusAfterSecond, _, _ := lockStatus(t, ctx, keeper, lockID)
	assert.Equal(t, statusAfterFirst, statusAfterSecond,
		"MR idempotence: second sweep leaves status unchanged. "+
			"Pins the best-effort cleanup contract — a refactor "+
			"that erred on already-released locks would break "+
			"EndBlocker's every-block call.")
}

// --------------------------------------------------------------
// MR — LIMIT PARAMETER
// --------------------------------------------------------------

// TestExpireLocks_MR_LimitCapsIteration pins that the limit
// parameter caps the number of locks swept in a single call.
// Sibling to tick-18's x/reserve release_expired_limit_test.go.
// The credits code uses `len(expiredLocks) >= limit` at :1819.
func TestExpireLocks_MR_LimitCapsSweepCount(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)

	params := keeper.GetParams(ctx)
	params.MaxLockTtlSeconds = 60
	params.DefaultLockTtlSeconds = 10
	require.NoError(t, keeper.SetParams(ctx, params))

	// Create 5 locks all with TTL=10s — all will expire simultaneously.
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		id, _ := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
			10*time.Second, 100_000+int64(i), "session-lim"+string(rune('0'+i)), "tool-lim")
		ids = append(ids, id)
	}

	// Advance past expiry.
	ctx = ctx.WithBlockTime(baseTime.Add(30 * time.Second))

	// Sweep with limit=2 — exactly 2 should be unlocked.
	require.NoError(t, keeper.ExpireLocks(ctx, 2))

	released := 0
	for _, id := range ids {
		status, _, _ := lockStatus(t, ctx, keeper, id)
		if status == types.LockStatus_LOCK_STATUS_RELEASED {
			released++
		}
	}
	assert.Equal(t, 2, released,
		"MR limit-caps-sweep: limit=2 unlocks EXACTLY 2 of 5 "+
			"expired locks. Pins keeper.go:1819 `len(expiredLocks) "+
			">= limit` early-return — a refactor to `>` would drain "+
			"3 instead.")
}

// TestExpireLocks_MR_LimitZeroUsesDefault pins that limit<=0
// falls back to DefaultMaxExpiredLocksPerBlock at :1798-1800.
// Sweep picks up the default number (large enough to handle
// all 5 test locks).
func TestExpireLocks_MR_LimitZeroUsesDefaultCap(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)

	params := keeper.GetParams(ctx)
	params.MaxLockTtlSeconds = 60
	params.DefaultLockTtlSeconds = 10
	require.NoError(t, keeper.SetParams(ctx, params))

	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		id, _ := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
			10*time.Second, 100_000+int64(i), "session-z"+string(rune('0'+i)), "tool-z")
		ids = append(ids, id)
	}

	ctx = ctx.WithBlockTime(baseTime.Add(30 * time.Second))

	// limit=0 → uses default (DefaultMaxExpiredLocksPerBlock ≫ 5).
	require.NoError(t, keeper.ExpireLocks(ctx, 0))

	for _, id := range ids {
		status, _, _ := lockStatus(t, ctx, keeper, id)
		assert.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, status,
			"limit=0 falls back to default; all 5 locks swept")
	}
}

// --------------------------------------------------------------
// MR — INCLUSIVE: active locks untouched
// --------------------------------------------------------------

// TestExpireLocks_MR_NonExpiredLocksUntouched pins that
// ExpireLocks touches ONLY locks past their ExpiresAt. Active
// (not-yet-expired) locks remain ACTIVE regardless of limit.
// The data-loss-guard pin — a refactor widening the sweep range
// would silently cancel valid user locks.
func TestExpireLocks_MR_NonExpiredLocksUntouched(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)

	params := keeper.GetParams(ctx)
	params.MaxLockTtlSeconds = 10 // for the short locks
	params.DefaultLockTtlSeconds = 10
	require.NoError(t, keeper.SetParams(ctx, params))

	// 2 short locks.
	shortIDs := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		id, _ := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
			10*time.Second, 100_000, "session-s"+string(rune('0'+i)), "tool-s")
		shortIDs = append(shortIDs, id)
	}

	// Switch MaxLockTtlSeconds to 10000 and create 2 long locks.
	// DefaultLockTtlSeconds also bumped so Default ≤ Max stays satisfied.
	params.MaxLockTtlSeconds = 10_000
	params.DefaultLockTtlSeconds = 120
	require.NoError(t, keeper.SetParams(ctx, params))
	longIDs := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		id, _ := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
			10_000*time.Second, 100_000, "session-L"+string(rune('0'+i)), "tool-L")
		longIDs = append(longIDs, id)
	}

	// Advance past short-lock expiry (20s) but well before long (10_000s).
	ctx = ctx.WithBlockTime(baseTime.Add(20 * time.Second))
	require.NoError(t, keeper.ExpireLocks(ctx, 100)) // generous limit

	for _, id := range shortIDs {
		status, _, _ := lockStatus(t, ctx, keeper, id)
		assert.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, status,
			"short lock %s expired", id)
	}
	for _, id := range longIDs {
		status, _, _ := lockStatus(t, ctx, keeper, id)
		assert.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, status,
			"MR non-expired-untouched: long lock %s MUST be ACTIVE "+
				"(ExpiresAt > now). Pins the range bound — a refactor "+
				"widening the sweep would silently cancel valid user "+
				"locks, a catastrophic data-loss bug.", id)
	}
}

// --------------------------------------------------------------
// MR — EMPTY SWEEP IS NOOP
// --------------------------------------------------------------

// TestExpireLocks_MR_NoExpiredLocksReturnsNil pins that
// ExpireLocks with nothing expired is a clean no-op (no error,
// no state change). Sibling to tick-26's EmptySweepIsNoop for
// x/reserve.
func TestExpireLocks_MR_EmptySweepIsNoop(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	baseTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(baseTime)

	params := keeper.GetParams(ctx)
	params.MaxLockTtlSeconds = 10_000
	params.DefaultLockTtlSeconds = 120
	require.NoError(t, keeper.SetParams(ctx, params))

	lockID, _ := createLockWithTTL(t, ctx, keeper, bank, accKeeper,
		10_000*time.Second, 100_000, "session-e", "tool-e")

	// Sweep BEFORE any expiry — no-op.
	require.NoError(t, keeper.ExpireLocks(ctx, 0))

	status, _, _ := lockStatus(t, ctx, keeper, lockID)
	assert.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, status,
		"MR empty-sweep: no lock expired, none touched")
}
