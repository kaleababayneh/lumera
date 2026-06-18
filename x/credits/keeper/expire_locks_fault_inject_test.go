
package keeper

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// Fault-injection tests for the Logger.Error branches added in commit
// b5cf2c93 (ExpireLocks pending-settlement bump path,
// x/credits/keeper/keeper.go:1873-1880). Before b5cf2c93 both writes
// were `_ = k.SaveLock(...)` / `_ = k.state.LockExpiry.Set(...)` —
// silently-discarded errors. The commit replaced them with
// `k.Logger.Error(...)` calls so a mid-bump store failure leaves a
// diagnostic trail instead of orphaning the lock from the expiry walk.
//
// Closing bead lumera_ai-2ry1y: pin that each of the two independent
// error branches actually routes through Logger.Error with the
// expected keys (lock_id + new_expires_at) when its specific write
// fails, and that ExpireLocks continues past the failure rather than
// short-circuiting — the pre-fix best-effort semantics are preserved;
// only observability was added.

// =============================================================================
// Fault-injection infrastructure
// =============================================================================

// faultArming is a shared atomic handle that controls whether a write
// should fail, keyed by prefix-prefix-match on the underlying KV store
// key. armed=nil means no fault (pass-through). A non-nil armed func
// runs on every Set — returning a non-nil error causes the Set to fail
// with that error; returning nil lets the Set through to the inner
// store.
//
// Exposing arming as a shared pointer lets the test set up state with
// faults disabled, then flip the fault on just before calling
// ExpireLocks — otherwise setup itself (SetParams, LockCredits,
// CreateSettlement) would hit the injected failure.
type faultArming struct {
	armed atomic.Pointer[func(key, value []byte) error]
}

func (f *faultArming) Arm(fn func(key, value []byte) error) {
	f.armed.Store(&fn)
}

func (f *faultArming) Disarm() {
	f.armed.Store(nil)
}

func (f *faultArming) check(key, value []byte) error {
	if p := f.armed.Load(); p != nil {
		return (*p)(key, value)
	}
	return nil
}

// faultyKVStoreService wraps a corestore.KVStoreService with a
// prefix-conditional Set fault. Read, Delete, and iterator operations
// always pass through — the commit b5cf2c93 error branches are
// Set-only, so the narrower fault surface matches the failure mode.
type faultyKVStoreService struct {
	inner corestore.KVStoreService
	fault *faultArming
}

func (s *faultyKVStoreService) OpenKVStore(ctx context.Context) corestore.KVStore {
	return &faultyKVStore{inner: s.inner.OpenKVStore(ctx), fault: s.fault}
}

type faultyKVStore struct {
	inner corestore.KVStore
	fault *faultArming
}

func (s *faultyKVStore) Get(key []byte) ([]byte, error) { return s.inner.Get(key) }
func (s *faultyKVStore) Has(key []byte) (bool, error)   { return s.inner.Has(key) }
func (s *faultyKVStore) Delete(key []byte) error        { return s.inner.Delete(key) }
func (s *faultyKVStore) Iterator(start, end []byte) (corestore.Iterator, error) {
	return s.inner.Iterator(start, end)
}
func (s *faultyKVStore) ReverseIterator(start, end []byte) (corestore.Iterator, error) {
	return s.inner.ReverseIterator(start, end)
}
func (s *faultyKVStore) Set(key, value []byte) error {
	if err := s.fault.check(key, value); err != nil {
		return err
	}
	return s.inner.Set(key, value)
}

// =============================================================================
// Capturing logger
// =============================================================================

type capturedLog struct {
	level string
	msg   string
	kv    []any
}

// capturingLogger satisfies cosmossdk.io/log.Logger and records every
// call at Error level with its message and key-value pairs. With() is
// a no-op (returns self) — the x/credits Logger helper wraps the base
// logger with module=x/credits via .With(), so the capture has to
// survive that layer.
type capturingLogger struct {
	entries *[]capturedLog
}

func newCapturingLogger() *capturingLogger {
	e := make([]capturedLog, 0)
	return &capturingLogger{entries: &e}
}

func (l *capturingLogger) Info(string, ...any)                          {}
func (l *capturingLogger) InfoContext(context.Context, string, ...any)  {}
func (l *capturingLogger) Warn(string, ...any)                          {}
func (l *capturingLogger) WarnContext(context.Context, string, ...any)  {}
func (l *capturingLogger) Debug(string, ...any)                         {}
func (l *capturingLogger) DebugContext(context.Context, string, ...any) {}
func (l *capturingLogger) Error(msg string, kv ...any) {
	*l.entries = append(*l.entries, capturedLog{level: "error", msg: msg, kv: append([]any(nil), kv...)})
}
func (l *capturingLogger) ErrorContext(_ context.Context, msg string, kv ...any) {
	*l.entries = append(*l.entries, capturedLog{level: "error", msg: msg, kv: append([]any(nil), kv...)})
}
func (l *capturingLogger) With(...any) log.Logger { return l }
func (l *capturingLogger) Impl() any              { return l }

// kvLookup returns the first value associated with `key` in a
// key/value slice, or nil if not found. Mirrors the
// cosmossdk.io/log convention where kv pairs are flat `[k1, v1, k2, v2, ...]`.
func kvLookup(kv []any, key string) (any, bool) {
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok && k == key {
			return kv[i+1], true
		}
	}
	return nil, false
}

// =============================================================================
// Keeper setup with fault injection
// =============================================================================

// setupCreditsKeeperFaulty mirrors setupCreditsKeeperWithOptions but
// wraps the KV store service with faultyKVStoreService and installs a
// capturingLogger on the sdk.Context so k.Logger(ctx).Error calls are
// captured. Returns a DISARMED fault handle — the test flips it on
// right before the call whose failure path is under test.
func setupCreditsKeeperFaulty(t *testing.T) (sdk.Context, *Keeper, *mockBankKeeper, *mockAccountKeeper, *faultArming, *capturingLogger) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	innerLogger := log.NewNopLogger()
	cms := rootmulti.NewStore(db, innerLogger)
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	bankKeeper := newMockBankKeeper(types.ModuleAccountName, moduleAddr)
	accountKeeper := &mockAccountKeeper{
		moduleAddr: moduleAddr,
		accounts:   make(map[string]sdk.AccAddress),
	}

	fault := &faultArming{}
	storeService := &faultyKVStoreService{
		inner: runtime.NewKVStoreService(storeKey),
		fault: fault,
	}

	keeper := NewKeeper(
		cdc,
		storeService,
		bankKeeper,
		accountKeeper,
		nil,
		nil,
		noopReserveKeeper{},
		nil,
		authtypes.NewModuleAddress("gov").String(),
	)

	capLogger := newCapturingLogger()
	header := tmproto.Header{Time: time.Now().UTC()}
	ctx := sdk.NewContext(cms, header, false, capLogger)

	require.NoError(t, keeper.SetParams(ctx, types.DefaultParams()))

	return ctx, &keeper, bankKeeper, accountKeeper, fault, capLogger
}

// =============================================================================
// Helpers to prepare a lock + pending settlement in the bump branch
// precondition: lock exists, its expiry-index entry is present, and a
// PENDING settlement is bound via LockReceipts.
// =============================================================================

type bumpFixture struct {
	lockID            string
	receiptID         string
	originalExpiresAt time.Time
	advancedTime      time.Time
}

func prepareBumpFixture(t *testing.T, ctx sdk.Context, keeper *Keeper, bank *mockBankKeeper, accKeeper *mockAccountKeeper) (sdk.Context, bumpFixture) {
	t.Helper()

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	start := time.Now().UTC().Truncate(time.Second)
	ctx = ctx.WithBlockTime(start)

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-fault",
		lockAmount,
		"tool-fault",
		"quote-fault",
		"policy@fault",
		"intent-fault",
	)
	require.NoError(t, err)

	const receiptID = "receipt-fault"
	require.NoError(t, keeper.state.LockReceipts.Set(ctx, lockID, receiptID))
	require.NoError(t, keeper.CreateSettlement(ctx, &types.SettlementRecord{
		Id:        receiptID,
		Status:    types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Timestamp: timestamppb.New(start),
		ToolId:    "tool-fault",
		RouterId:  routerAddr.String(),
	}))

	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	originalExpiresAt := lock.ExpiresAt.AsTime()

	// Advance past the lock's ExpiresAt so the expiry walk picks it up.
	advancedTime := originalExpiresAt.Add(30 * time.Second)
	ctx = ctx.WithBlockTime(advancedTime)

	return ctx, bumpFixture{
		lockID:            lockID,
		receiptID:         receiptID,
		originalExpiresAt: originalExpiresAt,
		advancedTime:      advancedTime,
	}
}

// findErrorEntry returns the first captured entry with msg == want, or
// fails the test with the full set of captured messages for diagnostic
// clarity.
func findErrorEntry(t *testing.T, capLogger *capturingLogger, want string) capturedLog {
	t.Helper()
	for _, e := range *capLogger.entries {
		if e.msg == want {
			return e
		}
	}
	var seen []string
	for _, e := range *capLogger.entries {
		seen = append(seen, e.msg)
	}
	t.Fatalf("expected Logger.Error entry %q, got %v", want, seen)
	return capturedLog{}
}

// =============================================================================
// Tests
// =============================================================================

var errInjectedSet = errors.New("injected Set failure")

// TestExpireLocks_SaveLockErrorIsLoggedNotSwallowed exercises
// keeper.go:1873-1875 — the first of the two `if err != nil` branches
// added in commit b5cf2c93. When SaveLock on the bumped lock fails
// (mid-write of the Locks prefix), the error MUST flow to
// Logger.Error with lock_id + new_expires_at keys so ops can alert on
// the divergence; ExpireLocks MUST continue (per best-effort bump
// semantics — the pre-fix `_ = k.SaveLock(...)` didn't halt the flow,
// and b5cf2c93 preserved that behavior).
//
// Regression guarded: if a future refactor drops the Logger.Error
// back to a silent discard, this test fails because no captured entry
// matches "failed to save bumped lock expiry".
func TestExpireLocks_SaveLockErrorIsLoggedNotSwallowed(t *testing.T) {
	ctx, keeper, bank, accKeeper, fault, capLogger := setupCreditsKeeperFaulty(t)

	ctx, fx := prepareBumpFixture(t, ctx, keeper, bank, accKeeper)

	// Arm fault on the Locks prefix (0x02). Every subsequent Set whose
	// key starts with 0x02 fails with errInjectedSet. This matches
	// exactly the SaveLock write — which is
	// `k.state.Locks.Set(ctx, lock.LockId, lock)` behind a
	// types.LocksPrefix = {0x02} collection.
	//
	// The LockExpiry write uses prefix 0x09, so the second branch is
	// NOT affected — the test isolates branch 1.
	fault.Arm(func(key, _ []byte) error {
		if bytes.HasPrefix(key, types.LocksPrefix) {
			return errInjectedSet
		}
		return nil
	})
	t.Cleanup(fault.Disarm)

	// ExpireLocks must return nil — the bump path swallows both
	// failures after logging (best-effort write semantics, unchanged
	// by b5cf2c93). A future fix that short-circuits ExpireLocks on
	// SaveLock failure would flip this assertion and is a separate
	// design decision, not a silent refactor.
	require.NoError(t, keeper.ExpireLocks(ctx, 10),
		"ExpireLocks must continue past a SaveLock failure in the bump "+
			"branch — b5cf2c93 only added logging, did not change the "+
			"best-effort write semantics")

	// Logger.Error MUST have fired with "failed to save bumped lock expiry".
	entry := findErrorEntry(t, capLogger, "failed to save bumped lock expiry")

	// The log must carry the structured keys ops rules will key off:
	//   lock_id         → identifies which lock orphaned
	//   new_expires_at  → identifies the intended bump target so a
	//                     reconciliation job can restore the index
	gotLockID, ok := kvLookup(entry.kv, "lock_id")
	require.True(t, ok, "log entry missing lock_id key: %+v", entry.kv)
	require.Equal(t, fx.lockID, gotLockID,
		"lock_id in log must match the lock whose SaveLock failed")

	gotNewExpires, ok := kvLookup(entry.kv, "new_expires_at")
	require.True(t, ok, "log entry missing new_expires_at key: %+v", entry.kv)
	expectedNewExpires := fx.advancedTime.Add(time.Hour)
	gotTime, ok := gotNewExpires.(time.Time)
	require.True(t, ok, "new_expires_at must be a time.Time, got %T", gotNewExpires)
	require.WithinDuration(t, expectedNewExpires, gotTime, time.Second,
		"new_expires_at in log must be BlockTime + 1h")

	// Also carry the underlying error so ops tooling can classify the
	// failure class (store-write vs marshal vs context-cancel, etc.).
	gotErr, ok := kvLookup(entry.kv, "error")
	require.True(t, ok, "log entry missing error key: %+v", entry.kv)
	require.ErrorIs(t, gotErr.(error), errInjectedSet,
		"logged error must wrap the injected Set failure so the class "+
			"is preserved for alerting rules")
}

// TestExpireLocks_LockExpirySetErrorIsLoggedNotSwallowed exercises
// the second `if err != nil` branch in the bump path: the
// LockExpiry.Set that reinserts the bumped index entry fails.
//
// Contract pinned here (post-CacheContext-atomicity fix):
//  1. ExpireLocks returns nil (best-effort per-block sweep — a single
//     failed lock must not break the whole batch; BeginBlocker cannot
//     return error without halting the chain).
//  2. Logger.Error fires with "failed to reinsert lock expiry index"
//     carrying lock_id / new_expires_at / error so ops alerting and
//     reconciliation tooling can key off the structured fields.
//  3. The partial-write is rolled back via CacheContext.discard: the
//     lock's ExpiresAt stays at the OLD value, and the OLD expiry
//     index entry survives intact. The lock remains reachable by the
//     next ExpireLocks sweep. The prior implementation did NOT use
//     CacheContext and left a permanent orphan (lock.ExpiresAt
//     advanced in store, but no corresponding index entry); this test
//     now pins the atomic-bump invariant so a future silent refactor
//     that removes CacheContext regresses back into the orphan state.
func TestExpireLocks_LockExpirySetErrorIsLoggedNotSwallowed(t *testing.T) {
	ctx, keeper, bank, accKeeper, fault, capLogger := setupCreditsKeeperFaulty(t)

	ctx, fx := prepareBumpFixture(t, ctx, keeper, bank, accKeeper)

	// Arm fault on the LockExpiry prefix (0x09). SaveLock uses 0x02,
	// so branch 1 is unaffected — SaveLock succeeds, then LockExpiry.Set
	// fails. This is the orphaned-index failure mode.
	fault.Arm(func(key, _ []byte) error {
		if bytes.HasPrefix(key, types.LockExpiryPrefix) {
			return errInjectedSet
		}
		return nil
	})
	t.Cleanup(fault.Disarm)

	require.NoError(t, keeper.ExpireLocks(ctx, 10),
		"ExpireLocks must continue past a LockExpiry.Set failure — "+
			"b5cf2c93 only added logging, did not change best-effort "+
			"write semantics")

	entry := findErrorEntry(t, capLogger, "failed to reinsert lock expiry index")

	gotLockID, ok := kvLookup(entry.kv, "lock_id")
	require.True(t, ok, "log entry missing lock_id key: %+v", entry.kv)
	require.Equal(t, fx.lockID, gotLockID,
		"lock_id in log must match the lock whose expiry index was lost")

	gotNewExpires, ok := kvLookup(entry.kv, "new_expires_at")
	require.True(t, ok, "log entry missing new_expires_at key: %+v", entry.kv)
	expectedNewExpires := fx.advancedTime.Add(time.Hour)
	gotTime, ok := gotNewExpires.(time.Time)
	require.True(t, ok, "new_expires_at must be a time.Time, got %T", gotNewExpires)
	require.WithinDuration(t, expectedNewExpires, gotTime, time.Second,
		"new_expires_at in log must be BlockTime + 1h so ops can "+
			"reconstruct the missing index entry during recovery")

	gotErr, ok := kvLookup(entry.kv, "error")
	require.True(t, ok, "log entry missing error key: %+v", entry.kv)
	require.ErrorIs(t, gotErr.(error), errInjectedSet)

	// Corroborating state check: CacheContext.discard rolled back the
	// entire bump. Despite SaveLock succeeding inside the cache, the
	// subsequent LockExpiry.Set failure drops the cache so lock.ExpiresAt
	// stays at its ORIGINAL value (fx.originalExpiresAt, seeded by
	// LockCredits inside prepareBumpFixture) and the OLD expiry index
	// entry is still present at that timestamp. Together these preserve
	// the sweep invariant "every active lock is reachable via
	// LockExpiry" so the lock is retried on the next ExpireLocks sweep
	// instead of being orphaned.
	bumped, found := keeper.GetLock(ctx, fx.lockID)
	require.True(t, found)
	require.WithinDuration(t, fx.originalExpiresAt, bumped.ExpiresAt.AsTime(), time.Second,
		"atomicity contract: when LockExpiry.Set fails, CacheContext.discard "+
			"must roll back SaveLock too so lock.ExpiresAt stays at its original "+
			"value — anything else would orphan the lock from the expiry walk")

	// Sanity-check: originalExpiresAt and the would-have-been bumped
	// time must differ so the assertion above actually distinguishes
	// bump-happened from bump-discarded. advancedTime = originalExpiresAt
	// + 30s and expectedNewExpires = advancedTime + 1h, so they're 1h
	// 30s apart — well outside the 1-second WithinDuration tolerance.
	require.False(t, fx.originalExpiresAt.Equal(expectedNewExpires),
		"test setup: originalExpiresAt must differ from newExpires for the "+
			"atomicity assertion above to distinguish the two states")
}

// TestExpireLocks_BothWritesSucceed_NoErrorLogged is the negative
// control for the two fault-inject tests above. With no fault armed,
// neither Logger.Error branch should fire — otherwise a future
// refactor that accidentally logs on the happy path (e.g., demoting a
// `.Debug` to `.Error`) wouldn't be caught by the positive-fault
// tests and would flood ops alerting.
func TestExpireLocks_BothWritesSucceed_NoErrorLogged(t *testing.T) {
	ctx, keeper, bank, accKeeper, _, capLogger := setupCreditsKeeperFaulty(t)

	ctx, _ = prepareBumpFixture(t, ctx, keeper, bank, accKeeper)

	require.NoError(t, keeper.ExpireLocks(ctx, 10))

	for _, e := range *capLogger.entries {
		require.NotEqual(t, "failed to save bumped lock expiry", e.msg,
			"happy path must NOT log SaveLock failure")
		require.NotEqual(t, "failed to reinsert lock expiry index", e.msg,
			"happy path must NOT log LockExpiry.Set failure")
	}
}

// The fault-injection harness below only asserts keeper-owned state.
// mockBankKeeper is not sdk.Context-aware, so CacheContext rollbacks
// do not revert its in-memory balances the way the real bank keeper's
// store-backed writes do in production.

func TestLockCredits_LateIndexFailureRollsBackKeeperState(t *testing.T) {
	ctx, keeper, bank, accKeeper, fault, _ := setupCreditsKeeperFaulty(t)

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC().Truncate(time.Second))
	params := keeper.GetParams(ctx)
	ttl := time.Duration(params.MaxLockTtlSeconds) * time.Second
	if ttl == 0 {
		ttl = time.Hour
	}
	expectedExpiry := ctx.BlockTime().Add(ttl)

	fault.Arm(func(key, _ []byte) error {
		if bytes.HasPrefix(key, types.LockExpiryPrefix) {
			return errInjectedSet
		}
		return nil
	})
	t.Cleanup(fault.Disarm)

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-atomic-lock",
		lockAmount,
		"tool-atomic-lock",
		"quote-atomic-lock",
		"policy@atomic",
		"intent-atomic-lock",
	)
	require.Error(t, err)
	require.ErrorIs(t, err, errInjectedSet)
	require.Empty(t, lockID)

	_, found := keeper.GetLock(ctx, "lock-1")
	require.False(t, found, "late LockExpiry failure must discard the cached lock row")

	_, err = keeper.state.LocksByQuote.Get(ctx, "quote-atomic-lock")
	require.ErrorIs(t, err, collections.ErrNotFound,
		"late LockExpiry failure must also discard the quote idempotency index")

	hasExpiry, err := keeper.state.LockExpiry.Has(ctx, collections.Join(expectedExpiry, "lock-1"))
	require.NoError(t, err)
	require.False(t, hasExpiry,
		"late LockExpiry failure must not leave behind an expiry index entry for a discarded lock")
}

func TestUnlockCredits_FinalizedIndexFailureRollsBackKeeperState(t *testing.T) {
	ctx, keeper, bank, accKeeper, fault, _ := setupCreditsKeeperFaulty(t)

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC().Truncate(time.Second))

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-atomic-unlock",
		lockAmount,
		"tool-atomic-unlock",
		"quote-atomic-unlock",
		"policy@atomic",
		"intent-atomic-unlock",
	)
	require.NoError(t, err)

	lockBefore, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.NotNil(t, lockBefore.ExpiresAt)

	fault.Arm(func(key, _ []byte) error {
		if bytes.HasPrefix(key, types.FinalizedLocksPrefix) {
			return errInjectedSet
		}
		return nil
	})
	t.Cleanup(fault.Disarm)

	err = keeper.UnlockCredits(ctx, lockID, "cancelled")
	require.Error(t, err)
	require.ErrorIs(t, err, errInjectedSet)

	lockAfter, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, lockAfter.Status,
		"late FinalizedLocks failure must discard the released lock state")
	require.Empty(t, lockAfter.LastError,
		"late FinalizedLocks failure must not persist the unlock reason")

	hasExpiry, err := keeper.state.LockExpiry.Has(ctx, collections.Join(lockBefore.ExpiresAt.AsTime(), lockID))
	require.NoError(t, err)
	require.True(t, hasExpiry,
		"late FinalizedLocks failure must preserve the original expiry index entry")

	hasFinalized, err := keeper.state.FinalizedLocks.Has(ctx, collections.Join(ctx.BlockTime(), lockID))
	require.NoError(t, err)
	require.False(t, hasFinalized,
		"late FinalizedLocks failure must not leave a pruning index row behind")
}

func TestSettleLock_FinalizedIndexFailureRollsBackKeeperState(t *testing.T) {
	ctx, keeper, bank, accKeeper, fault, _ := setupCreditsKeeperFaulty(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC().Truncate(time.Second))

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-atomic-settle",
		lockAmount,
		"tool-atomic-settle",
		"quote-atomic-settle",
		"policy@atomic",
		"intent-atomic-settle",
	)
	require.NoError(t, err)

	lockBefore, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.NotNil(t, lockBefore.ExpiresAt)

	settleCtx := ctx.WithBlockTime(ctx.BlockTime().Add(45 * time.Second))
	receipt := SettlementRequest{
		ReceiptID:     "receipt-atomic-settle",
		ToolID:        "tool-atomic-settle",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	}

	fault.Arm(func(key, _ []byte) error {
		if bytes.HasPrefix(key, types.FinalizedLocksPrefix) {
			return errInjectedSet
		}
		return nil
	})
	t.Cleanup(fault.Disarm)

	result, err := keeper.SettleLock(settleCtx, lockID, actualCost, receipt)
	require.Error(t, err)
	require.ErrorIs(t, err, errInjectedSet)
	require.Nil(t, result)

	lockAfter, found := keeper.GetLock(settleCtx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, lockAfter.Status,
		"late FinalizedLocks failure must discard the burned lock state")

	hasExpiry, err := keeper.state.LockExpiry.Has(settleCtx, collections.Join(lockBefore.ExpiresAt.AsTime(), lockID))
	require.NoError(t, err)
	require.True(t, hasExpiry,
		"late FinalizedLocks failure must preserve the original expiry index entry")

	hasFinalized, err := keeper.state.FinalizedLocks.Has(settleCtx, collections.Join(settleCtx.BlockTime(), lockID))
	require.NoError(t, err)
	require.False(t, hasFinalized,
		"late FinalizedLocks failure must not leave a pruning index row behind")

	_, err = keeper.state.LockReceipts.Get(settleCtx, lockID)
	require.ErrorIs(t, err, collections.ErrNotFound,
		"late FinalizedLocks failure must discard the lock-to-receipt binding")

	_, found = keeper.GetSettlement(settleCtx, receipt.ReceiptID)
	require.False(t, found,
		"late FinalizedLocks failure must discard the cached settlement record")
}
