//go:build cosmos && cosmos_full

package keeper

import (
	"testing"
	"time"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// TestLockStateInvariant_EmptyStatePasses pins the zero-state
// contract of LockStateInvariant at invariants.go:116-156: a keeper
// with no locks must report (msg, broken=false). Registered
// invariants run periodically; a false-positive (broken=true) on
// empty state would halt the chain on a freshly-initialized module.
//
// LockStateInvariant was previously exercised only indirectly via
// TestAllInvariants_Passes (invariants_test.go:309) — which covers
// only the clean-state happy path and can't detect whether a
// regression left the invariant silently tautological.
func TestLockStateInvariant_EmptyStatePasses(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	ctx = ctx.WithBlockTime(time.Now().UTC())

	invariant := LockStateInvariant(*keeper)
	msg, broken := invariant(ctx)

	assert.False(t, broken, "empty state must not break invariant: %s", msg)
}

// TestLockStateInvariant_ValidActiveLockPasses pins the
// happy-path branch: an ACTIVE lock with a positive amount and a
// non-nil ExpiresAt in the FUTURE passes all three checks.
func TestLockStateInvariant_ValidActiveLockPasses(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	now := time.Now().UTC()
	ctx = ctx.WithBlockTime(now)

	lock := &types.Lock{
		LockId:    "lock-valid",
		Router:    "lumera1router",
		Amount:    &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: "1000000"},
		Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		ExpiresAt: timestamppb.New(now.Add(time.Hour)),
	}
	require.NoError(t, keeper.SaveLock(ctx, lock))

	invariant := LockStateInvariant(*keeper)
	_, broken := invariant(ctx)
	assert.False(t, broken, "valid active lock must pass invariant")
}

// TestLockStateInvariant_NonPositiveAmountBreaks pins the
// non-positive-amount check at invariants.go:126-128. A lock with
// zero or negative amount is a data-integrity violation —
// downstream settlement math would produce nonsense results or
// panic on the CoinFromProto path.
func TestLockStateInvariant_NonPositiveAmountBreaks(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	now := time.Now().UTC()
	ctx = ctx.WithBlockTime(now)

	lock := &types.Lock{
		LockId:    "lock-zero",
		Router:    "lumera1router",
		Amount:    &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: "0"},
		Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		ExpiresAt: timestamppb.New(now.Add(time.Hour)),
	}
	require.NoError(t, keeper.SaveLock(ctx, lock))

	invariant := LockStateInvariant(*keeper)
	msg, broken := invariant(ctx)
	assert.True(t, broken, "zero-amount lock must break invariant")
	assert.Contains(t, msg, "lock-zero")
	assert.Contains(t, msg, "non-positive")
}

// TestLockStateInvariant_ActiveLockMissingExpiryBreaks pins the
// nil-ExpiresAt branch at invariants.go:131-132. An ACTIVE lock
// without an expiry is an invariant violation — the expire-locks
// BeginBlocker loop (ExpireLocks at keeper.go:1794) relies on
// ExpiresAt to decide when to reap the lock; a nil expiry would
// leak the lock permanently.
func TestLockStateInvariant_ActiveLockMissingExpiryBreaks(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lock := &types.Lock{
		LockId:    "lock-no-expiry",
		Router:    "lumera1router",
		Amount:    &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: "1000000"},
		Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		ExpiresAt: nil, // intentionally missing
	}
	require.NoError(t, keeper.SaveLock(ctx, lock))

	invariant := LockStateInvariant(*keeper)
	msg, broken := invariant(ctx)
	assert.True(t, broken, "active lock with nil ExpiresAt must break invariant")
	assert.Contains(t, msg, "lock-no-expiry")
	assert.Contains(t, msg, "missing expiry")
}

// TestLockStateInvariant_ActiveLockAlreadyExpiredBreaks pins the
// past-expiry branch at invariants.go:133-135. An ACTIVE lock whose
// ExpiresAt is BEFORE the current block time should have been
// reaped by the expire-locks BeginBlocker already — if it persists
// as ACTIVE, something is leaking. The invariant surfaces this
// leak so the chain halts before economically-significant
// drift accumulates.
func TestLockStateInvariant_ActiveLockAlreadyExpiredBreaks(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	now := time.Now().UTC()
	ctx = ctx.WithBlockTime(now)

	lock := &types.Lock{
		LockId:    "lock-stale",
		Router:    "lumera1router",
		Amount:    &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: "1000000"},
		Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
		ExpiresAt: timestamppb.New(now.Add(-time.Hour)), // expired 1h ago
	}
	require.NoError(t, keeper.SaveLock(ctx, lock))

	invariant := LockStateInvariant(*keeper)
	msg, broken := invariant(ctx)
	assert.True(t, broken, "active lock with past ExpiresAt must break invariant")
	assert.Contains(t, msg, "lock-stale")
	assert.Contains(t, msg, "expired at")
}

// TestLockStateInvariant_UnspecifiedStatusBreaks pins the
// UNSPECIFIED-status branch at invariants.go:136-137. The proto
// enum zero value is LOCK_STATUS_UNSPECIFIED; any persisted lock
// with that value indicates a bug upstream (e.g., direct proto
// unmarshal without status assignment, or a migration that forgot
// to backfill the field). Surfacing it via the invariant prevents
// silent propagation of degraded state.
func TestLockStateInvariant_UnspecifiedStatusBreaks(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	now := time.Now().UTC()
	ctx = ctx.WithBlockTime(now)

	lock := &types.Lock{
		LockId: "lock-unspecified",
		Router: "lumera1router",
		Amount: &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: "1000000"},
		Status: types.LockStatus_LOCK_STATUS_UNSPECIFIED, // zero value
	}
	require.NoError(t, keeper.SaveLock(ctx, lock))

	invariant := LockStateInvariant(*keeper)
	msg, broken := invariant(ctx)
	assert.True(t, broken, "UNSPECIFIED status must break invariant")
	assert.Contains(t, msg, "lock-unspecified")
	assert.Contains(t, msg, "unspecified status")
}

// TestLockStateInvariant_NonActiveLocksSkipExpiryCheck pins an
// often-overlooked detail: the expiry + nil-ExpiresAt checks run
// ONLY for ACTIVE locks (invariants.go:130-135 is `if lock.Status
// == LOCK_STATUS_ACTIVE`). A RELEASED or BURNED lock is allowed
// to have nil ExpiresAt or a past ExpiresAt — they've already
// completed their lifecycle. Without this test, a regression that
// moved the expiry check outside the ACTIVE branch would surface
// as a post-burn breakage of the invariant on historical state.
func TestLockStateInvariant_NonActiveLocksSkipExpiryCheck(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	now := time.Now().UTC()
	ctx = ctx.WithBlockTime(now)

	released := &types.Lock{
		LockId:    "lock-released",
		Router:    "lumera1router",
		Amount:    &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: "1000000"},
		Status:    types.LockStatus_LOCK_STATUS_RELEASED,
		ExpiresAt: timestamppb.New(now.Add(-time.Hour)), // past expiry, but released
	}
	require.NoError(t, keeper.SaveLock(ctx, released))

	burned := &types.Lock{
		LockId: "lock-burned",
		Router: "lumera1router",
		Amount: &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: "500000"},
		Status: types.LockStatus_LOCK_STATUS_BURNED,
		// ExpiresAt intentionally nil
	}
	require.NoError(t, keeper.SaveLock(ctx, burned))

	invariant := LockStateInvariant(*keeper)
	msg, broken := invariant(ctx)
	assert.False(t, broken,
		"RELEASED and BURNED locks must pass invariant even with nil or past ExpiresAt: %s",
		msg)
}

// TestLockStateInvariant_AggregatesMultipleIssues pins that the
// invariant collects ALL violations rather than short-circuiting
// on the first one. When issue aggregation at :119 collects
// issues and reports them together, operators can see the full
// scope of state corruption rather than playing whack-a-mole
// with one issue at a time. Two broken locks → both surface in
// the message.
func TestLockStateInvariant_AggregatesMultipleIssues(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	now := time.Now().UTC()
	ctx = ctx.WithBlockTime(now)

	require.NoError(t, keeper.SaveLock(ctx, &types.Lock{
		LockId: "lock-bad-1",
		Router: "lumera1r1",
		Amount: &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: "0"}, // bad
		Status: types.LockStatus_LOCK_STATUS_ACTIVE,
		ExpiresAt: timestamppb.New(now.Add(time.Hour)),
	}))
	require.NoError(t, keeper.SaveLock(ctx, &types.Lock{
		LockId:    "lock-bad-2",
		Router:    "lumera1r2",
		Amount:    &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: "1000"},
		Status:    types.LockStatus_LOCK_STATUS_UNSPECIFIED, // bad
	}))

	invariant := LockStateInvariant(*keeper)
	msg, broken := invariant(ctx)
	assert.True(t, broken)
	assert.Contains(t, msg, "lock-bad-1",
		"aggregate message must include first broken lock")
	assert.Contains(t, msg, "lock-bad-2",
		"aggregate message must include second broken lock — invariant must "+
			"not short-circuit on first violation")
}
