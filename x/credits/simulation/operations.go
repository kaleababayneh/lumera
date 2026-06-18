
package simulation

import (
	"context"
	"fmt"
	"math/rand"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"github.com/LumeraProtocol/lumera/x/credits/keeper"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

//#nosec G101 -- these are simulation operation weight keys, not credentials
const (
	opWeightLockCredits   = "op_weight_credits_lock"
	opWeightSettleCredits = "op_weight_credits_settle"
	opWeightSwapLUMEtoLAC = "op_weight_credits_swap_lume_lac"
	opWeightSwapLACtoLUME = "op_weight_credits_swap_lac_lume"

	defaultWeightLock       = 30
	defaultWeightSettle     = 25
	defaultWeightSwapToLAC  = 15
	defaultWeightSwapToLUME = 10

	creditDenom = "ulac"
	stakeDenom  = "ulume"
)

// WeightedOperations wires credits simulation scenarios into the cosmos simulator.
func WeightedOperations(
	appParams simtypes.AppParams,
	cdc codec.JSONCodec,
	k *keeper.Keeper,
	ak types.AccountKeeper,
	bk types.BankKeeper,
) simulation.WeightedOperations {
	_ = cdc
	_ = ak

	var (
		weightLock       int
		weightSettle     int
		weightSwapToLAC  int
		weightSwapToLUME int
	)

	appParams.GetOrGenerate(opWeightLockCredits, &weightLock, nil,
		func(_ *rand.Rand) { weightLock = defaultWeightLock })
	appParams.GetOrGenerate(opWeightSettleCredits, &weightSettle, nil,
		func(_ *rand.Rand) { weightSettle = defaultWeightSettle })
	appParams.GetOrGenerate(opWeightSwapLUMEtoLAC, &weightSwapToLAC, nil,
		func(_ *rand.Rand) { weightSwapToLAC = defaultWeightSwapToLAC })
	appParams.GetOrGenerate(opWeightSwapLACtoLUME, &weightSwapToLUME, nil,
		func(_ *rand.Rand) { weightSwapToLUME = defaultWeightSwapToLUME })

	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(weightLock, simulateLockCredits(k, bk)),
		simulation.NewWeightedOperation(weightSettle, simulateSettleCredits(k, bk)),
		simulation.NewWeightedOperation(weightSwapToLAC, simulateSwapLUMEtoLAC(k, bk)),
		simulation.NewWeightedOperation(weightSwapToLUME, simulateSwapLACtoLUME(k, bk)),
	}
}

func coin(denom, amount string) *v1beta1.Coin {
	return &v1beta1.Coin{Denom: denom, Amount: amount}
}

// simulateLockCredits locks credits for a simulated tool invocation session.
func simulateLockCredits(k *keeper.Keeper, bk types.BankKeeper) simtypes.Operation {
	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		accs []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) < 2 {
			return simtypes.NoOpMsg(types.ModuleName, "lock_credits", "need at least 2 accounts"), nil, nil
		}

		msgServer := keeper.NewMsgServerImpl(k)

		router, _ := simtypes.RandomAcc(r, accs)
		amount := sdkmath.NewInt(r.Int63n(50_000) + 1_000)
		coins := sdk.NewCoins(sdk.NewCoin(creditDenom, amount))

		if err := mintModuleCoins(ctx, bk, types.ModuleName, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "lock_credits", fmt.Sprintf("mint failed: %v", err)), nil, err
		}
		if err := bk.SendCoinsFromModuleToAccount(ctx, types.ModuleName, router.Address, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "lock_credits", fmt.Sprintf("fund failed: %v", err)), nil, err
		}

		lockAmount := sdkmath.NewInt(r.Int63n(amount.Int64()/2) + 500)
		msg := &types.MsgLockCredits{
			Router:    router.Address.String(),
			SessionId: fmt.Sprintf("sim-session-%d-%d", ctx.BlockHeight(), r.Int63()),
			Amount:    coin(creditDenom, lockAmount.String()),
			ToolId:    fmt.Sprintf("tool-%d", r.Intn(200)),
			QuoteId:   fmt.Sprintf("quote-%d", r.Int63()),
		}

		resp, err := msgServer.LockCredits(ctx, msg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "lock_credits", fmt.Sprintf("lock failed: %v", err)), nil, nil
		}

		comment := fmt.Sprintf("locked %s %s for session %s (lock %s)", lockAmount.String(), creditDenom, msg.SessionId, resp.LockId)
		return simtypes.NewOperationMsgBasic(types.ModuleName, "lock_credits", comment, true, nil), nil, nil
	}
}

// simulateSettleCredits settles an existing lock by creating one and immediately settling.
func simulateSettleCredits(k *keeper.Keeper, bk types.BankKeeper) simtypes.Operation {
	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		accs []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) < 2 {
			return simtypes.NoOpMsg(types.ModuleName, "settle_credits", "need at least 2 accounts"), nil, nil
		}

		msgServer := keeper.NewMsgServerImpl(k)

		router, _ := simtypes.RandomAcc(r, accs)
		lockAmount := sdkmath.NewInt(r.Int63n(40_000) + 2_000)
		coins := sdk.NewCoins(sdk.NewCoin(creditDenom, lockAmount.MulRaw(2)))

		if err := mintModuleCoins(ctx, bk, types.ModuleName, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "settle_credits", fmt.Sprintf("mint failed: %v", err)), nil, err
		}
		if err := bk.SendCoinsFromModuleToAccount(ctx, types.ModuleName, router.Address, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "settle_credits", fmt.Sprintf("fund failed: %v", err)), nil, err
		}

		toolID := fmt.Sprintf("tool-%d", r.Intn(200))
		lockResp, err := msgServer.LockCredits(ctx, &types.MsgLockCredits{
			Router:    router.Address.String(),
			SessionId: fmt.Sprintf("sim-settle-%d-%d", ctx.BlockHeight(), r.Int63()),
			Amount:    coin(creditDenom, lockAmount.String()),
			ToolId:    toolID,
			QuoteId:   fmt.Sprintf("quote-%d", r.Int63()),
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "settle_credits", fmt.Sprintf("lock failed: %v", err)), nil, nil
		}

		pct := int64(r.Intn(51) + 50) // 50-100% of locked
		actualCost := lockAmount.MulRaw(pct).QuoRaw(100)
		if !actualCost.IsPositive() {
			actualCost = sdkmath.NewInt(1)
		}

		settleMsg := &types.MsgSettleCredits{
			Router:     router.Address.String(),
			LockId:     lockResp.LockId,
			ReceiptId:  fmt.Sprintf("receipt-%d", r.Int63()),
			ToolId:     toolID,
			Publisher:  fmt.Sprintf("publisher-%d", r.Intn(50)),
			ActualCost: coin(creditDenom, actualCost.String()),
			CacheHit:   r.Float64() < 0.15,
		}

		if _, err := msgServer.SettleCredits(ctx, settleMsg); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "settle_credits", fmt.Sprintf("settle failed: %v", err)), nil, nil
		}

		comment := fmt.Sprintf("settled lock %s: actual %s/%s %s", lockResp.LockId, actualCost.String(), lockAmount.String(), creditDenom)
		return simtypes.NewOperationMsgBasic(types.ModuleName, "settle_credits", comment, true, nil), nil, nil
	}
}

// simulateSwapLUMEtoLAC swaps LUME tokens for LAC credits.
func simulateSwapLUMEtoLAC(k *keeper.Keeper, bk types.BankKeeper) simtypes.Operation {
	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		accs []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, "swap_lume_lac", "no accounts"), nil, nil
		}

		msgServer := keeper.NewMsgServerImpl(k)

		sender, _ := simtypes.RandomAcc(r, accs)
		amount := sdkmath.NewInt(r.Int63n(100_000) + 1_000)
		coins := sdk.NewCoins(sdk.NewCoin(stakeDenom, amount))

		if err := mintModuleCoins(ctx, bk, types.ModuleName, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "swap_lume_lac", fmt.Sprintf("mint failed: %v", err)), nil, err
		}
		if err := bk.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sender.Address, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "swap_lume_lac", fmt.Sprintf("fund failed: %v", err)), nil, err
		}

		msg := &types.MsgSwapLUMEtoLAC{
			Sender:     sender.Address.String(),
			LumeAmount: coin(stakeDenom, amount.String()),
		}

		if _, err := msgServer.SwapLUMEtoLAC(ctx, msg); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "swap_lume_lac", fmt.Sprintf("swap failed: %v", err)), nil, nil
		}

		comment := fmt.Sprintf("swapped %s %s to LAC for %s", amount.String(), stakeDenom, sender.Address)
		return simtypes.NewOperationMsgBasic(types.ModuleName, "swap_lume_lac", comment, true, nil), nil, nil
	}
}

// simulateSwapLACtoLUME swaps LAC credits back to LUME tokens.
func simulateSwapLACtoLUME(k *keeper.Keeper, bk types.BankKeeper) simtypes.Operation {
	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		accs []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, "swap_lac_lume", "no accounts"), nil, nil
		}

		msgServer := keeper.NewMsgServerImpl(k)

		sender, _ := simtypes.RandomAcc(r, accs)
		amount := sdkmath.NewInt(r.Int63n(50_000) + 500)
		coins := sdk.NewCoins(sdk.NewCoin(creditDenom, amount))

		if err := mintModuleCoins(ctx, bk, types.ModuleName, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "swap_lac_lume", fmt.Sprintf("mint failed: %v", err)), nil, err
		}
		if err := bk.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sender.Address, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "swap_lac_lume", fmt.Sprintf("fund failed: %v", err)), nil, err
		}

		msg := &types.MsgSwapLACtoLUME{
			Sender:    sender.Address.String(),
			LacAmount: coin(creditDenom, amount.String()),
		}

		if _, err := msgServer.SwapLACtoLUME(ctx, msg); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "swap_lac_lume", fmt.Sprintf("swap failed: %v", err)), nil, nil
		}

		comment := fmt.Sprintf("swapped %s %s to LUME for %s", amount.String(), creditDenom, sender.Address)
		return simtypes.NewOperationMsgBasic(types.ModuleName, "swap_lac_lume", comment, true, nil), nil, nil
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type bankMinter interface {
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

func mintModuleCoins(ctx sdk.Context, bk types.BankKeeper, moduleName string, amt sdk.Coins) error {
	if minter, ok := any(bk).(bankMinter); ok {
		return minter.MintCoins(ctx, moduleName, amt)
	}
	return fmt.Errorf("bank keeper does not support MintCoins")
}
