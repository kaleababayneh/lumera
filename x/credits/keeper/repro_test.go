//go:build cosmos && cosmos_full

package keeper

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestSettlementRecordHasLockID(t *testing.T) {
	// Setup
	registry := &mockRegistryKeeper{
		publishers: make(map[string]sdk.AccAddress),
	}
	ctx, keeper, bank, _, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registry,
	})
	ctx = ctx.WithBlockTime(time.Now().UTC())

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr
	bank.FundAccount(routerAddr, sdk.NewCoins(sdk.NewInt64Coin("ulac", 100_000)))
	registry.publishers["tool-1"] = publisherAddr

	srv := NewMsgServerImpl(keeper)

	// 1. Lock Credits
	lockResp, err := srv.LockCredits(ctx, &types.MsgLockCredits{
		Router:    routerAddr.String(),
		SessionId: "session-check-lockid",
		ToolId:    "tool-1",
		QuoteId:   "quote-check-lockid",
		Amount:    protoCoin("ulac", "50000"),
	})
	require.NoError(t, err)
	lockID := lockResp.LockId

	// 2. Settle Credits
	receiptID := "receipt-check-lockid"
	_, err = srv.SettleCredits(ctx, &types.MsgSettleCredits{
		Router:     routerAddr.String(),
		LockId:     lockID,
		ReceiptId:  receiptID,
		ToolId:     "tool-1",
		Publisher:  publisherAddr.String(),
		ActualCost: protoCoin("ulac", "30000"),
	})
	require.NoError(t, err)

	// 3. Verify Settlement Record
	settlement, found := keeper.GetSettlement(ctx, receiptID)
	require.True(t, found, "settlement record must exist")
	
	assert.Equal(t, lockID, settlement.LockId, "settlement record must contain the correct LockId")
}
