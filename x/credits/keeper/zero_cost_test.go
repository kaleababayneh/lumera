
package keeper

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestZeroCostSettlement(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	referrerAddr := newAccAddress()

	// Register test accounts
	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	// 0 cost settlement (free tool)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 0)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-free",
		lockAmount,
		"tool-free",
		"quote-free",
		"policy@1",
		"intent-hash",
	)
	require.NoError(t, err)
	require.NotEmpty(t, lockID)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-free",
		ToolID:        "tool-free",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(5 * time.Second))

	// This should succeed now
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Check that lock is burned (processed)
	locked, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, locked.Status)

	// Module should have returned the full amount to the router (refund)
	// Because cost was 0.
	require.Equal(t, sdk.NewCoins(lockAmount), bank.Balance(routerAddr))
	require.True(t, bank.Balance(moduleAddr).IsZero())
}
