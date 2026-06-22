package keeper

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// TestExpireLocks_PendingSettlementBumpsExpiry pins the contract of the
// pending-settlement bump branch in ExpireLocks (keeper.go:1841-1874):
// when an expired lock has a PENDING settlement tied to it via
// LockReceipts, ExpireLocks must NOT release the lock, must bump its
// ExpiresAt by one hour, and must keep the LockExpiry index consistent
// (old entry removed, new entry inserted).
//
// The test exists because the bump performs three stores in sequence
// (LockExpiry.Remove + SaveLock + LockExpiry.Set); a regression where
// either the lock record or the index entry is lost would silently
// orphan the lock from the expiry walk. Commit b5cf2c93 replaced the
// SaveLock/Set `_ =` error-discards with Logger.Error calls, and this
// test pins the happy-path invariants that commit was defending.
func TestExpireLocks_PendingSettlementBumpsExpiry(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	start := time.Now().UTC().Truncate(time.Second)
	ctx = ctx.WithBlockTime(start)

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-bump",
		lockAmount,
		"tool-bump",
		"quote-bump",
		"policy@bump",
		"intent-bump",
	)
	require.NoError(t, err)

	// The bump branch runs only if LockReceipts[lockID] resolves to a
	// Settlement whose Status is still PENDING. Wire both up directly
	// instead of going through SettleLock so the lock stays in the
	// expiry-index crosshairs while the settlement is still in-flight.
	const receiptID = "receipt-bump"
	require.NoError(t, keeper.state.LockReceipts.Set(ctx, lockID, receiptID))
	require.NoError(t, keeper.CreateSettlement(ctx, &types.SettlementRecord{
		Id:        receiptID,
		Status:    types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Timestamp: start,
		ToolId:    "tool-bump",
		RouterId:  routerAddr.String(),
	}))

	originalLock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, originalLock.Status)
	originalExpiresAt := originalLock.ExpiresAt

	// Old expiry index entry must be present before expiry runs — this
	// is the precondition the bump branch relies on.
	oldPresentBefore, err := keeper.state.LockExpiry.Has(
		ctx, collections.Join(originalExpiresAt, lockID))
	require.NoError(t, err)
	require.True(t, oldPresentBefore,
		"original expiry index entry must be set before ExpireLocks runs")

	// Advance past the lock's ExpiresAt so the expiry walk picks it up.
	advanced := originalExpiresAt.Add(30 * time.Second)
	ctx = ctx.WithBlockTime(advanced)

	require.NoError(t, keeper.ExpireLocks(ctx, 10))

	// Invariant 1: lock must remain ACTIVE. A regression that releases
	// the lock while settlement is still PENDING would burn credits
	// that are already committed to an in-flight settlement.
	bumped, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, bumped.Status,
		"pending-settlement lock must stay ACTIVE, not released by ExpireLocks")

	// Invariant 2: ExpiresAt must be bumped exactly one hour past the
	// current BlockTime (the bump logic: `sdkCtx.BlockTime().Add(time.Hour)`).
	expectedNewExpires := advanced.Add(time.Hour)
	require.WithinDuration(t, expectedNewExpires, bumped.ExpiresAt,
		time.Second,
		"ExpiresAt must be bumped to BlockTime + 1h; got %v want %v",
		bumped.ExpiresAt, expectedNewExpires)

	// Invariant 3: old expiry index entry must be removed.
	oldPresentAfter, err := keeper.state.LockExpiry.Has(
		ctx, collections.Join(originalExpiresAt, lockID))
	require.NoError(t, err)
	require.False(t, oldPresentAfter,
		"old LockExpiry index entry must be removed when expiry is bumped")

	// Invariant 4: new expiry index entry must be present. A regression
	// here — SaveLock updating ExpiresAt but Set failing silently —
	// would orphan the lock from the expiry walk permanently. b5cf2c93
	// added a Logger.Error on that Set failure; this assertion pins the
	// happy-path requirement that the Set actually landed.
	newPresentAfter, err := keeper.state.LockExpiry.Has(
		ctx, collections.Join(bumped.ExpiresAt, lockID))
	require.NoError(t, err)
	require.True(t, newPresentAfter,
		"new LockExpiry index entry at bumped time must be set so the "+
			"next expiry walk re-picks up the lock once settlement completes")

	// Invariant 5: LockReceipts binding must survive the bump so the
	// next tick can still identify the lock as settlement-pending.
	rebound, err := keeper.state.LockReceipts.Get(ctx, lockID)
	require.NoError(t, err)
	require.Equal(t, receiptID, rebound,
		"LockReceipts binding must survive the expiry bump")
}
