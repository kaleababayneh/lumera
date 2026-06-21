package keeper

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestMaybeAdjustAdaptiveBurnRateMovesTowardTarget(t *testing.T) {
	ctx, keeper, bank, moduleAddr, _ := setupCreditsKeeper(t)
	ctx = ctx.WithBlockTime(time.Now().UTC()).WithBlockHeight(int64(types.DefaultBurnRateAdjustmentEpoch))

	params := types.DefaultParams()
	params.BurnRateSpendBps = 300
	params.TargetAnnualDeflationBps = 150
	params.MinBurnRateSpendBps = 50
	params.MaxBurnRateSpendBps = 1000
	params.BurnRateAdjustmentEpoch = 100
	params.DeathSpiralSupplyContractionBps = 300
	params.DeathSpiralBurnRateCapBps = 150
	require.NoError(t, keeper.SetParams(ctx, params))

	mintAdaptiveBurnSupply(bank, moduleAddr, params.CreditDenom, 1_000_000)
	populateAdaptiveBurnSettlements(t, ctx, keeper, params.CreditDenom, 120, 100)

	adjustment, err := keeper.MaybeAdjustAdaptiveBurnRate(ctx)
	require.NoError(t, err)
	require.NotNil(t, adjustment)
	require.False(t, adjustment.InsufficientData)
	require.Equal(t, uint32(300), adjustment.OldRateBps)
	require.Equal(t, uint32(275), adjustment.NewRateBps)
	require.Equal(t, int32(-25), adjustment.AdjustmentBps)
	require.Equal(t, "down", adjustment.Direction)
	require.Equal(t, "above_target_band", adjustment.Reason)

	updated := keeper.GetParams(ctx)
	require.Equal(t, uint32(275), updated.BurnRateSpendBps)
}

func TestMaybeAdjustAdaptiveBurnRateHonorsFloor(t *testing.T) {
	ctx, keeper, bank, moduleAddr, _ := setupCreditsKeeper(t)
	ctx = ctx.WithBlockTime(time.Now().UTC()).WithBlockHeight(int64(types.DefaultBurnRateAdjustmentEpoch))

	params := types.DefaultParams()
	params.BurnRateSpendBps = 50
	params.MinBurnRateSpendBps = 50
	params.MaxBurnRateSpendBps = 1000
	params.TargetAnnualDeflationBps = 150
	params.BurnRateAdjustmentEpoch = 100
	require.NoError(t, keeper.SetParams(ctx, params))

	mintAdaptiveBurnSupply(bank, moduleAddr, params.CreditDenom, 1_000_000)
	populateAdaptiveBurnSettlements(t, ctx, keeper, params.CreditDenom, 120, 100)

	adjustment, err := keeper.MaybeAdjustAdaptiveBurnRate(ctx)
	require.NoError(t, err)
	require.NotNil(t, adjustment)
	require.Equal(t, uint32(50), adjustment.NewRateBps)
	require.Equal(t, int32(0), adjustment.AdjustmentBps)
	require.Equal(t, "min_burn_rate", adjustment.ClampReason)
}

func TestMaybeAdjustAdaptiveBurnRateAppliesDeathSpiralCap(t *testing.T) {
	ctx, keeper, bank, moduleAddr, _ := setupCreditsKeeper(t)
	ctx = ctx.WithBlockTime(time.Now().UTC()).WithBlockHeight(int64(types.DefaultBurnRateAdjustmentEpoch))

	params := types.DefaultParams()
	params.BurnRateSpendBps = 300
	params.TargetAnnualDeflationBps = 150
	params.MinBurnRateSpendBps = 50
	params.MaxBurnRateSpendBps = 1000
	params.BurnRateAdjustmentEpoch = 100
	params.DeathSpiralSupplyContractionBps = 300
	params.DeathSpiralBurnRateCapBps = 150
	require.NoError(t, keeper.SetParams(ctx, params))

	mintAdaptiveBurnSupply(bank, moduleAddr, params.CreditDenom, 100_000)
	populateAdaptiveBurnSettlements(t, ctx, keeper, params.CreditDenom, 120, 500)

	adjustment, err := keeper.MaybeAdjustAdaptiveBurnRate(ctx)
	require.NoError(t, err)
	require.NotNil(t, adjustment)
	require.True(t, adjustment.DeathSpiralTriggered)
	require.Equal(t, uint32(275), adjustment.RequestedRateBps)
	require.Equal(t, uint32(150), adjustment.NewRateBps)
	require.Equal(t, "death_spiral_cap", adjustment.Reason)
	require.Contains(t, adjustment.ClampReason, "death_spiral_cap")
}

func mintAdaptiveBurnSupply(bank *mockBankKeeper, moduleAddr sdk.AccAddress, denom string, amount int64) {
	bank.ensureAccount(moduleAddr)
	bank.balances[moduleAddr.String()] = bank.balances[moduleAddr.String()].Add(sdk.NewInt64Coin(denom, amount))
}

func populateAdaptiveBurnSettlements(t *testing.T, ctx sdk.Context, keeper *Keeper, denom string, count int, burnPerSettlement int64) {
	t.Helper()
	for i := 0; i < count; i++ {
		completedAt := ctx.BlockTime().Add(-time.Duration(i%adaptiveBurnWindowDays) * 24 * time.Hour)
		settlement := &types.SettlementRecord{
			Id:          fmt.Sprintf("adaptive-burn-%03d", i),
			Status:      types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
			CompletedAt: timestamppb.New(completedAt),
			BurnAmount:  types.CoinsToProto(sdk.NewCoins(sdk.NewInt64Coin(denom, burnPerSettlement))),
		}
		require.NoError(t, keeper.CreateSettlement(ctx, settlement))
	}
}
