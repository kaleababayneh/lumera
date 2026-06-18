//go:build cosmos && cosmos_full

package keeper

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// Fuzz harness for x/credits Lock state machine transitions with a
// focus on UNREACHABLE STATE DETECTION.
//
// The Lock state machine has 5 statuses:
//
//     UNSPECIFIED (0)   — never a valid terminal; should never be produced
//     ACTIVE      (1)   — created by LockCredits
//     RELEASED    (2)   — produced by UnlockCredits on ACTIVE
//     BURNED      (3)   — produced by SettleLock on ACTIVE
//     EXPIRED     (4)   — produced by ExpireLocks on ACTIVE past ExpiresAt
//
// Legal transitions (single-step):
//
//     ACTIVE --UnlockCredits-->  RELEASED
//     ACTIVE --SettleLock------> BURNED
//     ACTIVE --ExpireLocks-----> EXPIRED
//
// All other transitions MUST reject with ErrLockInactive (or
// ErrLockNotFound if the lock doesn't exist). The state machine must
// guarantee:
//
//   1. UNSPECIFIED is unreachable from any public API — a lock that
//      reaches UNSPECIFIED means state corruption.
//   2. Terminal states (RELEASED / BURNED / EXPIRED) are absorbing —
//      no operation transitions OUT of a terminal.
//   3. Transitions are idempotent under repeat calls: calling
//      UnlockCredits on an already-RELEASED lock does not silently
//      succeed or mutate anything; it returns an error.
//   4. Invalid enum values (0x05, 0x06, negative) are rejected as
//      "inactive" the same as terminal states — they are treated as
//      "not ACTIVE" which is the defense-in-depth reading of the
//      `status != ACTIVE` guard.
//
// Correctness criterion: NEVER PANIC, NEVER REACH UNSPECIFIED as a
// post-transition state, NEVER transition out of a terminal state.

// lockStatusLabels maps the 5 defined + 1 invalid-sentinel statuses to
// human labels for diagnostic output.
var lockStatusLabels = map[types.LockStatus]string{
	types.LockStatus_LOCK_STATUS_UNSPECIFIED: "UNSPECIFIED",
	types.LockStatus_LOCK_STATUS_ACTIVE:      "ACTIVE",
	types.LockStatus_LOCK_STATUS_RELEASED:    "RELEASED",
	types.LockStatus_LOCK_STATUS_BURNED:      "BURNED",
	types.LockStatus_LOCK_STATUS_EXPIRED:     "EXPIRED",
}

func labelStatus(s types.LockStatus) string {
	if l, ok := lockStatusLabels[s]; ok {
		return l
	}
	return fmt.Sprintf("UNDEFINED_ENUM(%d)", int32(s))
}

// injectLock writes a lock directly to state in the specified status.
// Bypasses LockCredits to let the fuzz exercise transitions from any
// initial state, including unreachable ones like UNSPECIFIED. For
// ACTIVE locks the bank balance is also adjusted so the escrow
// accounting stays consistent (UnlockCredits reads the module balance).
func injectLock(t *testing.T, ctx sdk.Context, k *Keeper, bank *mockBankKeeper,
	lockID string, status types.LockStatus, amount int64,
) {
	t.Helper()
	lock := &types.Lock{
		LockId:    lockID,
		Router:    sdk.AccAddress([]byte("router_sm_test______")).String(),
		Amount:    &v1beta1.Coin{Denom: types.DefaultCreditDenom, Amount: fmt.Sprintf("%d", amount)},
		Status:    status,
		CreatedAt: timestamppb.New(ctx.BlockTime()),
		ExpiresAt: timestamppb.New(ctx.BlockTime().Add(time.Hour)),
	}
	require.NoError(t, k.SaveLock(ctx, lock))

	// For ACTIVE locks, ensure the module account has at least enough
	// to cover the lock amount so UnlockCredits' SendCoinsFromModuleToAccount
	// doesn't fail for an unrelated reason.
	if status == types.LockStatus_LOCK_STATUS_ACTIVE && amount > 0 {
		require.NoError(t, bank.MintCoins(ctx, types.ModuleAccountName,
			sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, amount))))
	}
}

// FuzzLockStateMachine_UnlockTransitions fuzzes UnlockCredits across
// all possible initial states, verifying that:
//
//   - From ACTIVE: transitions to RELEASED, no error
//   - From any other state (including UNSPECIFIED or invalid enum):
//     returns error, lock status unchanged
//   - Post-state is never UNSPECIFIED (unreachable invariant)
//   - Lock state machine is panic-free
func FuzzLockStateMachine_UnlockTransitions(f *testing.F) {
	// Seed each of the 5 defined statuses plus the invalid-enum case.
	for _, status := range []int32{0, 1, 2, 3, 4, 5, 99, -1} {
		f.Add(status, "reason-test", int64(1000))
	}

	f.Fuzz(func(t *testing.T, rawStatus int32, reason string, amount int64) {
		if !utf8.ValidString(reason) {
			return
		}
		// Clamp amount to avoid overflows in the coin construction.
		if amount <= 0 || amount > 1_000_000_000_000 {
			return
		}
		// Cap reason string length to keep test output readable.
		if len(reason) > 256 {
			return
		}

		ctx, k, bank, _, _ := setupCreditsKeeper(t)
		ctx = ctx.WithBlockTime(time.Now().UTC())

		status := types.LockStatus(rawStatus)
		lockID := "lock-sm-unlock"
		injectLock(t, ctx, k, bank, lockID, status, amount)

		// Record pre-transition state.
		preLock, found := k.GetLock(ctx, lockID)
		require.True(t, found, "lock must exist after injection")
		preStatus := preLock.Status

		// Attempt the transition. Must not panic.
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("UnlockCredits panicked from state %s with reason %q: %v",
						labelStatus(preStatus), reason, r)
				}
			}()
			err = k.UnlockCredits(ctx, lockID, reason)
		}()

		// Fetch post-state.
		postLock, found := k.GetLock(ctx, lockID)
		require.True(t, found, "lock must still exist after transition attempt")

		// Core invariant: if pre-state was any valid non-UNSPECIFIED
		// status, the post-state must remain non-UNSPECIFIED. Pre=
		// UNSPECIFIED is an injected-corruption test input and the
		// correct behavior is to leave it untouched (reject the
		// transition, preserve the corrupt state for observability).
		if preStatus != types.LockStatus_LOCK_STATUS_UNSPECIFIED {
			require.NotEqual(t, types.LockStatus_LOCK_STATUS_UNSPECIFIED, postLock.Status,
				"UnlockCredits produced UNSPECIFIED post-state from pre=%s — "+
					"unreachable state reached via transition path",
				labelStatus(preStatus))
		}

		// Transition rules.
		switch preStatus {
		case types.LockStatus_LOCK_STATUS_ACTIVE:
			// The ONLY legal source state for UnlockCredits.
			require.NoError(t, err,
				"UnlockCredits from ACTIVE rejected: %v (reason=%q)", err, reason)
			require.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, postLock.Status,
				"ACTIVE -> UnlockCredits must reach RELEASED, got %s",
				labelStatus(postLock.Status))
		default:
			// All other initial states must reject.
			require.Error(t, err,
				"UnlockCredits accepted from non-ACTIVE state %s — "+
					"illegal transition", labelStatus(preStatus))
			// Lock status must be unchanged from pre.
			require.Equal(t, preStatus, postLock.Status,
				"rejected UnlockCredits from %s mutated post-state to %s",
				labelStatus(preStatus), labelStatus(postLock.Status))
		}
	})
}

// FuzzLockStateMachine_DoubleUnlockIdempotent probes the terminal-state
// absorption property: once a lock is RELEASED, further UnlockCredits
// calls must all fail identically. Idempotence under repeat operations
// is what prevents a byzantine caller from re-triggering the unlock
// side-effects (module-to-account transfer, event emission).
func FuzzLockStateMachine_DoubleUnlockIdempotent(f *testing.F) {
	f.Add(int64(100), "first", "second")
	f.Add(int64(1_000_000), "", "repeated")
	f.Add(int64(1), "x", "x")

	f.Fuzz(func(t *testing.T, amount int64, reason1, reason2 string) {
		if amount <= 0 || amount > 1_000_000_000_000 {
			return
		}
		if !utf8.ValidString(reason1) || !utf8.ValidString(reason2) {
			return
		}
		if len(reason1) > 256 || len(reason2) > 256 {
			return
		}

		ctx, k, bank, _, _ := setupCreditsKeeper(t)
		ctx = ctx.WithBlockTime(time.Now().UTC())

		lockID := "lock-sm-double"
		injectLock(t, ctx, k, bank, lockID, types.LockStatus_LOCK_STATUS_ACTIVE, amount)

		// First unlock succeeds.
		require.NoError(t, k.UnlockCredits(ctx, lockID, reason1),
			"first UnlockCredits on ACTIVE failed")

		// Second unlock must fail. Status must not regress.
		err := k.UnlockCredits(ctx, lockID, reason2)
		require.Error(t, err, "second UnlockCredits on already-RELEASED succeeded")

		postLock, found := k.GetLock(ctx, lockID)
		require.True(t, found)
		require.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, postLock.Status,
			"status regressed after second UnlockCredits attempt: got %s",
			labelStatus(postLock.Status))
	})
}

// FuzzLockStateMachine_MissingLockHandled probes the NotFound branch.
// Any lockID that doesn't correspond to a stored lock must fail
// cleanly with ErrLockNotFound — no panic, no state mutation, no
// accidental lock creation.
func FuzzLockStateMachine_MissingLockHandled(f *testing.F) {
	f.Add("nonexistent-lock-id")
	f.Add("")
	f.Add(" ")
	f.Add("\x00")
	f.Add(strings.Repeat("x", 1000))
	f.Add("🔥")

	f.Fuzz(func(t *testing.T, lockID string) {
		if !utf8.ValidString(lockID) {
			return
		}

		ctx, k, _, _, _ := setupCreditsKeeper(t)
		ctx = ctx.WithBlockTime(time.Now().UTC())

		// No injection — lock doesn't exist.
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("UnlockCredits panicked on missing lockID %q: %v",
						lockID, r)
				}
			}()
			err = k.UnlockCredits(ctx, lockID, "test")
		}()

		require.Error(t, err, "UnlockCredits on missing lockID %q succeeded", lockID)

		// Verify no lock was created as a side effect.
		_, found := k.GetLock(ctx, lockID)
		require.False(t, found,
			"UnlockCredits on missing lockID %q created a lock record", lockID)
	})
}

// FuzzLockStateMachine_ExpirySweepTransitions fuzzes the ExpireLocks
// sweep. Legal: ACTIVE lock past ExpiresAt → RELEASED (note: the
// ExpireLocks path calls UnlockCredits internally, which transitions
// ACTIVE → RELEASED). Illegal: any terminal state must be left alone.
//
// This locks down the invariant that the expiry index never leaks
// terminal-state locks into the sweep — an over-zealous sweep that
// re-processes RELEASED/BURNED/EXPIRED locks would double-transition
// and corrupt accounting.
func FuzzLockStateMachine_ExpirySweepTransitions(f *testing.F) {
	for _, status := range []int32{0, 1, 2, 3, 4} {
		f.Add(status, int64(1000), int64(7200)) // expired 2h ago
	}

	f.Fuzz(func(t *testing.T, rawStatus int32, amount int64, expiredAgoSec int64) {
		if amount <= 0 || amount > 1_000_000_000 {
			return
		}
		if expiredAgoSec <= 0 || expiredAgoSec > 86_400*365 {
			return
		}

		ctx, k, bank, _, _ := setupCreditsKeeper(t)
		now := time.Now().UTC()
		ctx = ctx.WithBlockTime(now)

		status := types.LockStatus(rawStatus)
		lockID := "lock-sm-expiry"

		// Inject the lock with a past ExpiresAt.
		lock := &types.Lock{
			LockId: lockID,
			Router: sdk.AccAddress([]byte("router_sm_test______")).String(),
			Amount: &v1beta1.Coin{
				Denom: types.DefaultCreditDenom, Amount: fmt.Sprintf("%d", amount),
			},
			Status:    status,
			CreatedAt: timestamppb.New(now.Add(-time.Duration(expiredAgoSec+3600) * time.Second)),
			ExpiresAt: timestamppb.New(now.Add(-time.Duration(expiredAgoSec) * time.Second)),
		}
		require.NoError(t, k.SaveLock(ctx, lock))

		// Mint bank balance for ACTIVE locks so Unlock doesn't fail.
		if status == types.LockStatus_LOCK_STATUS_ACTIVE {
			require.NoError(t, bank.MintCoins(ctx, types.ModuleAccountName,
				sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, amount))))
		}

		// Run the sweep. Must not panic.
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ExpireLocks panicked on pre-state %s: %v",
						labelStatus(status), r)
				}
			}()
			err = k.ExpireLocks(ctx, 10)
		}()
		require.NoError(t, err, "ExpireLocks errored on pre-state %s", labelStatus(status))

		postLock, found := k.GetLock(ctx, lockID)
		if !found {
			// ExpireLocks may have cleaned up a stale lock — acceptable.
			return
		}

		// Invariant: if pre-state was a valid non-UNSPECIFIED enum, the
		// post-state must also be valid. Pre=UNSPECIFIED is documented
		// corrupt state that the sweep correctly refuses to touch — so
		// UNSPECIFIED being preserved is the defensive behavior, not a
		// violation.
		if status != types.LockStatus_LOCK_STATUS_UNSPECIFIED {
			require.NotEqual(t, types.LockStatus_LOCK_STATUS_UNSPECIFIED, postLock.Status,
				"ExpireLocks produced UNSPECIFIED from pre=%s — "+
					"corruption injected via sweep path", labelStatus(status))
		}

		// Terminal states must be absorbing.
		switch status {
		case types.LockStatus_LOCK_STATUS_RELEASED,
			types.LockStatus_LOCK_STATUS_BURNED,
			types.LockStatus_LOCK_STATUS_EXPIRED:
			require.Equal(t, status, postLock.Status,
				"terminal state %s was transitioned by ExpireLocks to %s",
				labelStatus(status), labelStatus(postLock.Status))
		}
	})
}

// FuzzLockStateMachine_UnspecifiedUnreachable explicitly targets the
// UNSPECIFIED-unreachability invariant. No sequence of public API
// calls may leave a lock in UNSPECIFIED. This fuzz constructs random
// sequences of (Lock, Unlock) operations and asserts that every lock
// in state has a status ∈ {ACTIVE, RELEASED, BURNED, EXPIRED} after
// each step.
func FuzzLockStateMachine_UnspecifiedUnreachable(f *testing.F) {
	f.Add(uint8(0b000), int64(100))
	f.Add(uint8(0b111), int64(100))
	f.Add(uint8(0b101), int64(1000))

	f.Fuzz(func(t *testing.T, opMask uint8, amount int64) {
		if amount <= 0 || amount > 1_000_000_000 {
			return
		}

		ctx, k, bank, moduleAddr, _ := setupCreditsKeeper(t)
		_ = moduleAddr
		ctx = ctx.WithBlockTime(time.Now().UTC())

		// Fund a router so Lock operations can succeed.
		routerAddr := sdk.AccAddress([]byte("router_sm_unreach___"))
		require.NoError(t, bank.MintCoins(ctx, types.ModuleAccountName,
			sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, amount*10))))
		require.NoError(t, bank.SendCoinsFromModuleToAccount(ctx,
			types.ModuleAccountName, routerAddr,
			sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, amount*10))))

		// The 3 bits of opMask drive three operations in sequence.
		// Each bit 1 -> lock; 0 -> unlock if any lock is pending.
		var pendingLockID string
		for i := 0; i < 3; i++ {
			doLock := (opMask>>i)&1 == 1
			if doLock {
				lockID, err := k.LockCredits(ctx, routerAddr.String(),
					fmt.Sprintf("session-%d", i),
					sdk.NewInt64Coin(types.DefaultCreditDenom, amount),
					"tool-test",
					fmt.Sprintf("quote-%d", i),
					"policy@1",
					fmt.Sprintf("intent-%d", i),
				)
				if err == nil {
					pendingLockID = lockID
				}
			} else if pendingLockID != "" {
				_ = k.UnlockCredits(ctx, pendingLockID, "fuzz-unlock")
				pendingLockID = ""
			}

			// After every step, enumerate all locks and assert none is
			// UNSPECIFIED.
			require.NoError(t, k.IterateLocks(ctx, func(lock *types.Lock) bool {
				require.NotEqual(t, types.LockStatus_LOCK_STATUS_UNSPECIFIED,
					lock.Status,
					"step %d: lock %s has UNSPECIFIED status — "+
						"unreachable state reached via public API sequence opMask=%b",
					i, lock.LockId, opMask)
				switch lock.Status {
				case types.LockStatus_LOCK_STATUS_ACTIVE,
					types.LockStatus_LOCK_STATUS_RELEASED,
					types.LockStatus_LOCK_STATUS_BURNED,
					types.LockStatus_LOCK_STATUS_EXPIRED:
					// ok
				default:
					t.Fatalf("step %d: lock %s has out-of-enum status %d",
						i, lock.LockId, int32(lock.Status))
				}
				return false
			}))
		}
	})
}

// FuzzLockStateMachine_InvalidEnumValuesRejected probes the "invalid
// enum" branch: if the proto store ever produces a Lock with a status
// outside the defined 0-4 range (e.g. via state corruption or an
// unsafe migration), UnlockCredits must reject it, not silently
// transition it. The `status != ACTIVE` guard should treat ANY
// non-ACTIVE value (defined or not) as inactive.
func FuzzLockStateMachine_InvalidEnumValuesRejected(f *testing.F) {
	f.Add(int32(5))
	f.Add(int32(100))
	f.Add(int32(-1))
	f.Add(int32(^0))

	f.Fuzz(func(t *testing.T, rawStatus int32) {
		// Skip the 5 valid enum values — they're covered elsewhere.
		if rawStatus >= 0 && rawStatus <= 4 {
			return
		}

		ctx, k, bank, _, _ := setupCreditsKeeper(t)
		ctx = ctx.WithBlockTime(time.Now().UTC())

		lockID := "lock-sm-invalid-enum"
		injectLock(t, ctx, k, bank, lockID, types.LockStatus(rawStatus), 100)

		// UnlockCredits MUST reject. Invalid enum = not ACTIVE = inactive.
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("UnlockCredits panicked on invalid-enum status %d: %v",
						rawStatus, r)
				}
			}()
			err = k.UnlockCredits(ctx, lockID, "fuzz")
		}()
		require.Error(t, err,
			"UnlockCredits accepted lock with invalid-enum status %d — "+
				"defense-in-depth missing: corrupted state could be transitioned",
			rawStatus)

		// Status must be unchanged.
		postLock, found := k.GetLock(ctx, lockID)
		require.True(t, found)
		require.Equal(t, types.LockStatus(rawStatus), postLock.Status,
			"rejected UnlockCredits mutated invalid-enum status %d to %d",
			rawStatus, int32(postLock.Status))
	})
}
