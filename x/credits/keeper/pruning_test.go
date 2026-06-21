package keeper

import (
	"testing"
	"time"

	"github.com/LumeraProtocol/lumera/x/credits/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestStateBloat_LocksPersistIndefinitely(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr
	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	// 1. Create a lock
	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-bloat",
		lockAmount,
		"tool-bloat",
		"quote-1",
		"policy@1",
		"intent-hash",
	)
	require.NoError(t, err)

	// Verify lock exists
	_, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)

	// 2. Cancel the lock (Unlock)
	// This should index it in FinalizedLocks with current time
	err = keeper.UnlockCredits(ctx, lockID, "cancelled")
	require.NoError(t, err)

	// Verify lock still exists (immediately after unlock)
	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, lock.Status)

	// 3. Simulate passage of time (8 days) - enough to pass prune window (7 days)
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(8 * 24 * time.Hour))

	// 4. Run PruneFinalizedLocks (simulating EndBlock)
	// We prune locks older than "now - 7 days".
	// Since we advanced 8 days, "now - 7 days" is "start + 1 day".
	// The lock was finalized at "start". So it is older than threshold.
	pruneThreshold := ctx.BlockTime().Add(-7 * 24 * time.Hour)
	err = keeper.PruneFinalizedLocks(ctx, pruneThreshold, 100)
	require.NoError(t, err)

	// 5. Verify lock is GONE
	_, found = keeper.GetLock(ctx, lockID)
	require.False(t, found, "Lock should have been pruned")
}
