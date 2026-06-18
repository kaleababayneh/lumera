
package keeper

import (
	"testing"

	"github.com/LumeraProtocol/lumera/x/credits/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestZeroFeeParams_Respected(t *testing.T) {
	ctx, keeper, bank, _, _ := setupCreditsKeeper(t)

	// Set params to 0 fees
	params := types.DefaultParams()
	params.BurnRateAcqBps = 0
	params.BurnRateSpendBps = 0
	params.BurnRateAdjustmentEpoch = 0
	params.InsuranceBps = 0
	err := keeper.SetParams(ctx, params)
	require.NoError(t, err)

	// User account
	userAddr := newAccAddress()
	lumeDenom := "ulume"
	initialLumeAmount := sdk.NewInt64Coin(lumeDenom, 100_000)
	bank.FundAccount(userAddr, sdk.NewCoins(initialLumeAmount))

	// Test 1: SwapLUMEtoLAC with 0 fee
	lumeToSwap := sdk.NewInt64Coin(lumeDenom, 10_000)
	lacReceived, burnAmount, err := keeper.SwapLUMEtoLAC(ctx, userAddr, lumeToSwap)
	require.NoError(t, err)

	// Expect 0 burn
	// If bug exists, burnAmount will be 1% (100)
	if !burnAmount.IsZero() {
		t.Fatalf("Bug Reproduced: Expected 0 burn amount for acquisition, got %s", burnAmount)
	}
	// Expect 1:1 swap (since 0 burn)
	require.Equal(t, lumeToSwap.Amount, lacReceived.Amount, "Expected 1:1 swap with 0 fee")

	// Test 2: ProcessSettlement with 0 fee
	totalAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1000))

	// Fund Module Account with LAC so it can burn/distribute
	bank.FundAccount(keeper.ModuleAddress(), totalAmount)

	req := SettlementRequest{
		ReceiptID:     "rec-1",
		ToolID:        "tool-1",
		TotalAmount:   totalAmount,
		PublisherAddr: newAccAddress(),
		RouterAddr:    newAccAddress(),
		ReferrerAddr:  newAccAddress(),
		UserID:        "user-1",
		PublisherID:   "pub-1",
		RouterID:      "router-1",
		ReferrerID:    "ref-1",
	}

	res, err := keeper.ProcessSettlement(ctx, req)
	require.NoError(t, err)

	// Check Burn Amount
	// If bug exists, burn will be 3% (30)
	if !res.BurnAmount.IsZero() {
		t.Fatalf("Bug Reproduced: Expected 0 burn amount for settlement, got %s", res.BurnAmount)
	}
}
