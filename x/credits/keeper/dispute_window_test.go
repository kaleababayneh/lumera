//go:build cosmos && cosmos_full

package keeper

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

type disputeWindowRegistryKeeper struct {
	disputeWindowSeconds uint32
	publisher            sdk.AccAddress
}

func (m disputeWindowRegistryKeeper) GetToolPublisher(context.Context, string) (sdk.AccAddress, error) {
	if m.publisher != nil {
		return m.publisher, nil
	}
	return nil, fmt.Errorf("publisher lookup not needed in dispute window tests")
}

func (m disputeWindowRegistryKeeper) GetDisputeWindowSeconds(context.Context) uint32 {
	return m.disputeWindowSeconds
}

func TestSettlementDisputeWindowPrefersRegistrySeconds(t *testing.T) {
	ctx, keeper, _, _, _, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: disputeWindowRegistryKeeper{disputeWindowSeconds: 90},
	})

	params := keeper.GetParams(ctx)
	params.DisputeWindowHours = 48
	require.NoError(t, keeper.SetParams(ctx, params))

	require.Equal(t, 90*time.Second, keeper.SettlementDisputeWindow(ctx))
}

func TestSettlementDisputeWindowFallsBackToCreditsHours(t *testing.T) {
	ctx, keeper, _, _, _, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{})

	params := keeper.GetParams(ctx)
	params.DisputeWindowHours = 3
	require.NoError(t, keeper.SetParams(ctx, params))

	require.Equal(t, 3*time.Hour, keeper.SettlementDisputeWindow(ctx))
	require.Equal(t, 3*time.Hour, creditsDisputeWindow(params))
}

func TestSettlementDisputeWindowUsesCanonicalDefaultWithoutOverride(t *testing.T) {
	ctx, keeper, _, _, _, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{})

	params := keeper.GetParams(ctx)
	params.DisputeWindowHours = 0
	require.NoError(t, keeper.SetParams(ctx, params))

	require.Equal(t, types.DefaultDisputeWindowDuration(), keeper.SettlementDisputeWindow(ctx))
	require.Equal(t, types.DefaultDisputeWindowDuration(), creditsDisputeWindow(nil))
}

func TestSettleLockPartialFillUsesRegistryDisputeWindow(t *testing.T) {
	router := newAccAddress()
	publisher := newAccAddress()
	ctx, keeper, bank, _, _, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: disputeWindowRegistryKeeper{
			disputeWindowSeconds: 90,
			publisher:            publisher,
		},
	})

	params := keeper.GetParams(ctx)
	params.DisputeWindowHours = 48
	require.NoError(t, keeper.SetParams(ctx, params))

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000)
	bank.FundAccount(router, sdk.NewCoins(lockAmount))

	lockID, err := keeper.LockCredits(
		ctx,
		router.String(),
		"session-partial-window",
		lockAmount,
		"tool-partial-window",
		"quote-partial-window",
		"policy-v1",
		"intent-hash",
	)
	require.NoError(t, err)

	_, err = keeper.SettleLock(ctx, lockID, sdk.NewInt64Coin(types.DefaultCreditDenom, 250), SettlementRequest{
		ReceiptID: "receipt-partial-window",
		ToolID:    "tool-partial-window",
		Stage:     "partial",
	})
	require.NoError(t, err)

	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_ACTIVE, lock.Status)
	require.NotNil(t, lock.ExpiresAt)
	require.Equal(t, ctx.BlockTime().Add(90*time.Second).Add(time.Hour), lock.ExpiresAt.AsTime())
}
