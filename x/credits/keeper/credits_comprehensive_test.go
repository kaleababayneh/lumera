package keeper

import (
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// =============================================================================
// LAC Mint Tests
// =============================================================================

func TestMintCredits_Success(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	recipient := newAccAddress()
	accKeeper.accounts[recipient.String()] = recipient

	amount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1000)

	err := keeper.MintCredits(ctx, recipient, amount, "test mint")
	require.NoError(t, err)

	// Verify recipient received the credits
	balance := bank.GetBalance(ctx, recipient, types.DefaultCreditDenom)
	assert.Equal(t, amount, balance)
}

func TestComprehensive_MintCredits_ZeroAmount(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	recipient := newAccAddress()
	accKeeper.accounts[recipient.String()] = recipient

	// Zero amount should succeed but not mint anything
	amount := sdk.NewInt64Coin(types.DefaultCreditDenom, 0)
	err := keeper.MintCredits(ctx, recipient, amount, "zero mint")
	require.NoError(t, err)

	balance := bank.GetBalance(ctx, recipient, types.DefaultCreditDenom)
	assert.True(t, balance.IsZero())
}

func TestMintCredits_InvalidDenom(t *testing.T) {
	ctx, keeper, _, _, accKeeper := setupCreditsKeeper(t)

	recipient := newAccAddress()
	accKeeper.accounts[recipient.String()] = recipient

	// Wrong denom should fail
	amount := sdk.NewInt64Coin("wrongdenom", 1000)
	err := keeper.MintCredits(ctx, recipient, amount, "bad denom")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected denom")
}

func TestComprehensive_MintCredits_EmptyRecipient(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	amount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1000)
	err := keeper.MintCredits(ctx, sdk.AccAddress{}, amount, "no recipient")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recipient required")
}

func TestMintCredits_NegativeAmount(t *testing.T) {
	ctx, keeper, _, _, accKeeper := setupCreditsKeeper(t)

	recipient := newAccAddress()
	accKeeper.accounts[recipient.String()] = recipient

	// Negative amount should fail
	amount := sdk.Coin{Denom: types.DefaultCreditDenom, Amount: sdkmath.NewInt(-100)}
	err := keeper.MintCredits(ctx, recipient, amount, "negative")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mint amount")
}

// =============================================================================
// LAC Burn Tests
// =============================================================================

func TestBurnCreditsFromAccount_Success(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	sender := newAccAddress()
	accKeeper.accounts[sender.String()] = sender

	// Fund the sender with LAC
	initialAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1000)
	bank.FundAccount(sender, sdk.NewCoins(initialAmount))

	// Also fund module with LUME for the invariant maintenance
	lumeAmount := sdk.NewInt64Coin(types.DefaultLumeDenom, 1000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(lumeAmount))

	// Burn half
	burnAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500)
	err := keeper.BurnCreditsFromAccount(ctx, sender, burnAmount, "test burn")
	require.NoError(t, err)

	// Verify sender lost the credits
	balance := bank.GetBalance(ctx, sender, types.DefaultCreditDenom)
	assert.Equal(t, int64(500), balance.Amount.Int64())
}

func TestComprehensive_BurnCreditsFromAccount_ZeroAmount(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	sender := newAccAddress()
	accKeeper.accounts[sender.String()] = sender

	initialAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1000)
	bank.FundAccount(sender, sdk.NewCoins(initialAmount))

	// Zero amount should succeed without burning
	burnAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 0)
	err := keeper.BurnCreditsFromAccount(ctx, sender, burnAmount, "zero burn")
	require.NoError(t, err)

	balance := bank.GetBalance(ctx, sender, types.DefaultCreditDenom)
	assert.Equal(t, initialAmount, balance)
}

func TestBurnCreditsFromAccount_InsufficientBalance(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	sender := newAccAddress()
	accKeeper.accounts[sender.String()] = sender

	// Fund with small amount
	initialAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 100)
	bank.FundAccount(sender, sdk.NewCoins(initialAmount))

	// Try to burn more than balance
	burnAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500)
	err := keeper.BurnCreditsFromAccount(ctx, sender, burnAmount, "too much")
	require.Error(t, err)
}

func TestBurnCreditsFromAccount_InvalidDenom(t *testing.T) {
	ctx, keeper, _, _, accKeeper := setupCreditsKeeper(t)

	sender := newAccAddress()
	accKeeper.accounts[sender.String()] = sender

	burnAmount := sdk.NewInt64Coin("wrongdenom", 100)
	err := keeper.BurnCreditsFromAccount(ctx, sender, burnAmount, "bad denom")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected denom")
}

func TestComprehensive_BurnCreditsFromAccount_EmptySender(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	burnAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 100)
	err := keeper.BurnCreditsFromAccount(ctx, sdk.AccAddress{}, burnAmount, "no sender")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sender required")
}

// =============================================================================
// Swap Flow Tests (LUME <-> LAC)
// =============================================================================

func TestSwapLUMEtoLAC_Success(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	user := newAccAddress()
	accKeeper.accounts[user.String()] = user

	// Fund user with LUME
	lumeAmount := sdk.NewInt64Coin(types.DefaultLumeDenom, 100_000)
	bank.FundAccount(user, sdk.NewCoins(lumeAmount))

	// Swap LUME to LAC
	swapAmount := sdk.NewInt64Coin(types.DefaultLumeDenom, 10_000)
	lacReceived, burnAmount, err := keeper.SwapLUMEtoLAC(ctx, user, swapAmount)
	require.NoError(t, err)

	// LAC received should be less than LUME input (acquisition burn applied)
	assert.True(t, lacReceived.Amount.LT(swapAmount.Amount),
		"LAC received should be less than LUME input due to acquisition burn")

	// Burn amount + LAC received should equal input
	totalOutput := burnAmount.Amount.Add(lacReceived.Amount)
	assert.Equal(t, swapAmount.Amount, totalOutput,
		"burn + LAC received should equal input")

	// Verify user has LAC
	lacBalance := bank.GetBalance(ctx, user, types.DefaultCreditDenom)
	assert.Equal(t, lacReceived, lacBalance)

	// Verify user has less LUME
	lumeBalance := bank.GetBalance(ctx, user, types.DefaultLumeDenom)
	assert.Equal(t, int64(90_000), lumeBalance.Amount.Int64())
}

func TestSwapLUMEtoLAC_WrongDenom(t *testing.T) {
	ctx, keeper, _, _, accKeeper := setupCreditsKeeper(t)

	user := newAccAddress()
	accKeeper.accounts[user.String()] = user

	// Try to swap wrong denom
	wrongCoin := sdk.NewInt64Coin("wrongdenom", 1000)
	_, _, err := keeper.SwapLUMEtoLAC(ctx, user, wrongCoin)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected ulume token")
}

func TestSwapLUMEtoLAC_InsufficientBalance(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	user := newAccAddress()
	accKeeper.accounts[user.String()] = user

	// Fund with small amount
	bank.FundAccount(user, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultLumeDenom, 100)))

	// Try to swap more than balance
	swapAmount := sdk.NewInt64Coin(types.DefaultLumeDenom, 10_000)
	_, _, err := keeper.SwapLUMEtoLAC(ctx, user, swapAmount)
	require.Error(t, err)
}

func TestSwapLACtoLUME_Success(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	user := newAccAddress()
	accKeeper.accounts[user.String()] = user

	// First, setup: user has LAC, module has LUME reserve
	lacAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(user, sdk.NewCoins(lacAmount))

	// Module needs LUME reserve to back the redemption
	lumeReserve := sdk.NewInt64Coin(types.DefaultLumeDenom, 20_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(lumeReserve))

	// Swap LAC back to LUME
	lumeReceived, burnAmount, err := keeper.SwapLACtoLUME(ctx, user, lacAmount)
	require.NoError(t, err)

	// LUME received should be less than LAC input (redemption burn applied)
	assert.True(t, lumeReceived.Amount.LT(lacAmount.Amount),
		"LUME received should be less than LAC input due to redemption burn")

	// Burn + received should equal input
	totalOutput := burnAmount.Amount.Add(lumeReceived.Amount)
	assert.Equal(t, lacAmount.Amount, totalOutput,
		"burn + LUME received should equal input")

	// Verify user has LUME
	lumeBalance := bank.GetBalance(ctx, user, types.DefaultLumeDenom)
	assert.Equal(t, lumeReceived, lumeBalance)

	// Verify user has no LAC
	lacBalance := bank.GetBalance(ctx, user, types.DefaultCreditDenom)
	assert.True(t, lacBalance.IsZero())
}

func TestSwapLACtoLUME_InsufficientLUMEReserve(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	user := newAccAddress()
	accKeeper.accounts[user.String()] = user

	// User has LAC but module has no LUME reserve
	lacAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(user, sdk.NewCoins(lacAmount))

	// Should fail due to insufficient LUME reserve
	_, _, err := keeper.SwapLACtoLUME(ctx, user, lacAmount)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient")
}

func TestSwapLACtoLUME_WrongDenom(t *testing.T) {
	ctx, keeper, _, _, accKeeper := setupCreditsKeeper(t)

	user := newAccAddress()
	accKeeper.accounts[user.String()] = user

	wrongCoin := sdk.NewInt64Coin("wrongdenom", 1000)
	_, _, err := keeper.SwapLACtoLUME(ctx, user, wrongCoin)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected")
}

func TestSwapRoundTrip_ValueLoss(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	user := newAccAddress()
	accKeeper.accounts[user.String()] = user

	// Start with LUME
	initialLume := sdk.NewInt64Coin(types.DefaultLumeDenom, 100_000)
	bank.FundAccount(user, sdk.NewCoins(initialLume))

	// Swap LUME to LAC
	lacReceived, acqBurn, err := keeper.SwapLUMEtoLAC(ctx, user, initialLume)
	require.NoError(t, err)

	// Module now has LUME (minus acq burn)
	moduleReserve := bank.GetBalance(ctx, moduleAddr, types.DefaultLumeDenom)
	expectedReserve := initialLume.Sub(acqBurn)
	assert.Equal(t, expectedReserve, moduleReserve)

	// Swap LAC back to LUME
	lumeReceived, redemptionBurn, err := keeper.SwapLACtoLUME(ctx, user, lacReceived)
	require.NoError(t, err)

	// Final LUME should be less than initial due to both burns
	assert.True(t, lumeReceived.Amount.LT(initialLume.Amount),
		"Round trip should result in value loss due to burns")

	// Total burned = acquisition burn + redemption burn
	totalBurned := acqBurn.Amount.Add(redemptionBurn.Amount)
	assert.True(t, totalBurned.IsPositive(), "Total burned should be positive")

	t.Logf("Initial: %s, After round trip: %s, Total burned: %s",
		initialLume, lumeReceived, totalBurned)
}

// =============================================================================
// Settlement Split Tests
// =============================================================================

func TestProcessSettlement_BasicSplit(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	// Setup parties
	publisher := newAccAddress()
	router := newAccAddress()
	accKeeper.accounts[publisher.String()] = publisher
	accKeeper.accounts[router.String()] = router

	// Fund module with LAC for distribution
	totalAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(totalAmount))

	receipt := SettlementRequest{
		ReceiptID:     "receipt-001",
		TotalAmount:   sdk.NewCoins(totalAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ToolID:        "test-tool",
	}

	result, err := keeper.ProcessSettlement(ctx, receipt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify splits occurred
	assert.False(t, result.BurnAmount.IsZero(), "Burn amount should be positive")
	assert.False(t, result.PublisherAmount.IsZero(), "Publisher payout should be positive")
	assert.False(t, result.RouterAmount.IsZero(), "Router payout should be positive")

	t.Logf("Settlement result: Burn=%s, Publisher=%s, Router=%s",
		result.BurnAmount, result.PublisherAmount, result.RouterAmount)
}

func TestProcessSettlement_WithReferrer(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	publisher := newAccAddress()
	router := newAccAddress()
	referrer := newAccAddress()
	accKeeper.accounts[publisher.String()] = publisher
	accKeeper.accounts[router.String()] = router
	accKeeper.accounts[referrer.String()] = referrer

	totalAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(totalAmount))

	receipt := SettlementRequest{
		ReceiptID:     "receipt-002",
		TotalAmount:   sdk.NewCoins(totalAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
		ReferrerAddr:  referrer,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ReferrerID:    referrer.String(),
		ToolID:        "test-tool",
	}

	result, err := keeper.ProcessSettlement(ctx, receipt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Referrer should have received something
	assert.False(t, result.ReferrerAmount.IsZero(), "Referrer payout should be positive")

	t.Logf("Settlement with referrer: Referrer=%s", result.ReferrerAmount)
}

func TestProcessSettlement_ZeroCost(t *testing.T) {
	ctx, keeper, _, _, accKeeper := setupCreditsKeeper(t)

	publisher := newAccAddress()
	router := newAccAddress()
	accKeeper.accounts[publisher.String()] = publisher
	accKeeper.accounts[router.String()] = router

	// Zero cost settlement (e.g., fully prepaid via vault)
	receipt := SettlementRequest{
		ReceiptID:     "receipt-zero",
		TotalAmount:   sdk.NewCoins(),
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ToolID:        "test-tool",
	}

	result, err := keeper.ProcessSettlement(ctx, receipt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// All payouts should be zero
	assert.True(t, result.BurnAmount.IsZero(), "Burn should be zero for zero-cost settlement")
}

func TestProcessSettlement_MissingPublisherDoesNotBurn(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	router := newAccAddress()
	accKeeper.accounts[router.String()] = router

	totalAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(totalAmount))
	initialModuleBalance := bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom)

	receipt := SettlementRequest{
		ReceiptID:   "receipt-missing-publisher",
		TotalAmount: sdk.NewCoins(totalAmount),
		RouterAddr:  router,
		RouterID:    router.String(),
		ToolID:      "test-tool",
		Stage:       "finalized",
	}

	_, err := keeper.ProcessSettlement(ctx, receipt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publisher address is missing")

	moduleBalance := bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom)
	assert.Equal(t, initialModuleBalance, moduleBalance, "failed settlement must not burn module funds")
}

func TestProcessSettlement_DuplicateReceipt(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	publisher := newAccAddress()
	router := newAccAddress()
	accKeeper.accounts[publisher.String()] = publisher
	accKeeper.accounts[router.String()] = router

	totalAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(totalAmount.Add(totalAmount))) // Fund enough for both

	receipt := SettlementRequest{
		ReceiptID:     "receipt-dup",
		TotalAmount:   sdk.NewCoins(totalAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ToolID:        "test-tool",
		Stage:         "finalized",
	}

	// First settlement should succeed
	_, err := keeper.ProcessSettlement(ctx, receipt)
	require.NoError(t, err)

	// Second settlement with same receipt ID should fail
	_, err = keeper.ProcessSettlement(ctx, receipt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already completed")
}

func TestProcessSettlement_EmptyReceiptID(t *testing.T) {
	ctx, keeper, _, _, accKeeper := setupCreditsKeeper(t)

	publisher := newAccAddress()
	router := newAccAddress()
	accKeeper.accounts[publisher.String()] = publisher
	accKeeper.accounts[router.String()] = router

	receipt := SettlementRequest{
		ReceiptID:     "", // Empty
		TotalAmount:   sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1000)),
		PublisherAddr: publisher,
		RouterAddr:    router,
	}

	_, err := keeper.ProcessSettlement(ctx, receipt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "receipt id cannot be empty")
}

// =============================================================================
// Adaptive Burn Tests
// =============================================================================

func TestMaybeAdjustAdaptiveBurnRate_NotAtEpoch(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	// Block height not at epoch boundary
	ctx = ctx.WithBlockHeight(50)

	params := types.DefaultParams()
	params.BurnRateAdjustmentEpoch = 100
	require.NoError(t, keeper.SetParams(ctx, params))

	adjustment, err := keeper.MaybeAdjustAdaptiveBurnRate(ctx)
	require.NoError(t, err)
	require.Nil(t, adjustment, "Should not adjust at non-epoch block")
}

func TestMaybeAdjustAdaptiveBurnRate_AtEpoch(t *testing.T) {
	ctx, keeper, bank, moduleAddr, _ := setupCreditsKeeper(t)

	// Set block height to epoch boundary
	ctx = ctx.WithBlockTime(time.Now().UTC()).WithBlockHeight(100)

	params := types.DefaultParams()
	params.BurnRateSpendBps = 200
	params.TargetAnnualDeflationBps = 150
	params.MinBurnRateSpendBps = 50
	params.MaxBurnRateSpendBps = 500
	params.BurnRateAdjustmentEpoch = 100
	require.NoError(t, keeper.SetParams(ctx, params))

	// Fund module with supply
	mintAdaptiveBurnSupply(bank, moduleAddr, params.CreditDenom, 1_000_000)

	// Create some settlements for burn data
	populateAdaptiveBurnSettlements(t, ctx, keeper, params.CreditDenom, 50, 100)

	adjustment, err := keeper.MaybeAdjustAdaptiveBurnRate(ctx)
	require.NoError(t, err)
	// May or may not adjust depending on burn data
	if adjustment != nil {
		t.Logf("Adjustment: old=%d, new=%d, direction=%s",
			adjustment.OldRateBps, adjustment.NewRateBps, adjustment.Direction)
	}
}

func TestMaybeAdjustAdaptiveBurnRate_InsufficientData(t *testing.T) {
	ctx, keeper, bank, moduleAddr, _ := setupCreditsKeeper(t)

	ctx = ctx.WithBlockTime(time.Now().UTC()).WithBlockHeight(100)

	params := types.DefaultParams()
	params.BurnRateSpendBps = 200
	params.BurnRateAdjustmentEpoch = 100
	require.NoError(t, keeper.SetParams(ctx, params))

	// Fund module but don't create any settlements
	mintAdaptiveBurnSupply(bank, moduleAddr, params.CreditDenom, 1_000_000)

	adjustment, err := keeper.MaybeAdjustAdaptiveBurnRate(ctx)
	require.NoError(t, err)
	if adjustment != nil {
		assert.True(t, adjustment.InsufficientData, "Should report insufficient data")
	}
}

func TestMaybeAdjustAdaptiveBurnRate_HonorsCeiling(t *testing.T) {
	ctx, keeper, bank, moduleAddr, _ := setupCreditsKeeper(t)
	ctx = ctx.WithBlockTime(time.Now().UTC()).WithBlockHeight(100)

	params := types.DefaultParams()
	params.BurnRateSpendBps = 500 // At max
	params.MinBurnRateSpendBps = 50
	params.MaxBurnRateSpendBps = 500
	params.TargetAnnualDeflationBps = 1000 // High target to push rate up
	params.BurnRateAdjustmentEpoch = 100
	require.NoError(t, keeper.SetParams(ctx, params))

	mintAdaptiveBurnSupply(bank, moduleAddr, params.CreditDenom, 1_000_000)
	populateAdaptiveBurnSettlements(t, ctx, keeper, params.CreditDenom, 120, 10) // Low burn to push rate up

	adjustment, err := keeper.MaybeAdjustAdaptiveBurnRate(ctx)
	require.NoError(t, err)
	if adjustment != nil {
		assert.LessOrEqual(t, adjustment.NewRateBps, params.MaxBurnRateSpendBps,
			"New rate should not exceed max")
	}
}

// =============================================================================
// BurnLAC Tests
// =============================================================================

func TestBurnLAC_Success(t *testing.T) {
	ctx, keeper, bank, moduleAddr, _ := setupCreditsKeeper(t)

	// Fund module with LAC
	lacAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(lacAmount))

	// Also fund with LUME for the invariant
	lumeAmount := sdk.NewInt64Coin(types.DefaultLumeDenom, 10_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(lumeAmount))

	burnRate := uint32(1000) // 10%
	err := keeper.BurnLAC(ctx, sdk.NewCoins(lacAmount), burnRate)
	require.NoError(t, err)

	// Module should have less LAC
	newBalance := bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom)
	assert.True(t, newBalance.Amount.LT(lacAmount.Amount),
		"Module should have less LAC after burn")
}

func TestBurnLAC_ZeroRate(t *testing.T) {
	ctx, keeper, bank, moduleAddr, _ := setupCreditsKeeper(t)

	lacAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(lacAmount))

	// Zero burn rate should not burn anything
	err := keeper.BurnLAC(ctx, sdk.NewCoins(lacAmount), 0)
	require.NoError(t, err)

	balance := bank.GetBalance(ctx, moduleAddr, types.DefaultCreditDenom)
	assert.Equal(t, lacAmount, balance, "No burn at zero rate")
}

// =============================================================================
// Calculate Burn Amount Tests
// =============================================================================

func TestCalculateBurnAmount_Basic(t *testing.T) {
	tests := []struct {
		name         string
		amount       int64
		rateBPS      uint32
		expectedBurn int64
		expectedNet  int64
	}{
		{
			name:         "10% burn",
			amount:       10000,
			rateBPS:      1000, // 10%
			expectedBurn: 1000,
			expectedNet:  9000,
		},
		{
			name:         "1% burn",
			amount:       10000,
			rateBPS:      100, // 1%
			expectedBurn: 100,
			expectedNet:  9900,
		},
		{
			name:         "0% burn",
			amount:       10000,
			rateBPS:      0,
			expectedBurn: 0,
			expectedNet:  10000,
		},
		{
			name:         "100% burn",
			amount:       10000,
			rateBPS:      10000, // 100%
			expectedBurn: 10000,
			expectedNet:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			burn, net, err := CalculateBurnAmount(sdkmath.NewInt(tc.amount), tc.rateBPS)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedBurn, burn.Int64(), "burn amount mismatch")
			assert.Equal(t, tc.expectedNet, net.Int64(), "net amount mismatch")
		})
	}
}

func TestCalculateBurnAmount_Rounding(t *testing.T) {
	// Test rounding behavior with amounts that don't divide evenly
	amount := sdkmath.NewInt(333)
	rateBPS := uint32(100) // 1%

	burn, net, err := CalculateBurnAmount(amount, rateBPS)
	require.NoError(t, err)

	// Verify burn + net = original
	total := burn.Add(net)
	assert.Equal(t, amount, total, "burn + net should equal original amount")
}

// =============================================================================
// Lock Credits Tests
// =============================================================================

func TestLockCredits_Success(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	router := newAccAddress()
	accKeeper.accounts[router.String()] = router

	// Fund router with LAC
	lacAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(router, sdk.NewCoins(lacAmount))

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 5_000)
	lockID, err := keeper.LockCredits(ctx, router.String(), "session-1", lockAmount, "tool-1", "quote-1", "v1", "")
	require.NoError(t, err)
	require.NotEmpty(t, lockID)

	// Verify lock exists
	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	assert.Equal(t, "session-1", lock.SessionId)
	assert.Equal(t, lockAmount.Amount.String(), lock.Amount.Amount.String())
}

func TestLockCredits_InsufficientBalance(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	router := newAccAddress()
	accKeeper.accounts[router.String()] = router

	// Fund with small amount
	bank.FundAccount(router, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 100)))

	// Try to lock more than balance
	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 5_000)
	_, err := keeper.LockCredits(ctx, router.String(), "session-1", lockAmount, "tool-1", "quote-1", "v1", "")
	require.Error(t, err)
}

// =============================================================================
// Unlock Credits Tests
// =============================================================================

func TestUnlockCredits_Success(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	router := newAccAddress()
	accKeeper.accounts[router.String()] = router

	lacAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(router, sdk.NewCoins(lacAmount))

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 5_000)
	lockID, err := keeper.LockCredits(ctx, router.String(), "session-1", lockAmount, "tool-1", "quote-1", "v1", "")
	require.NoError(t, err)

	// Verify balance decreased
	balanceAfterLock := bank.GetBalance(ctx, router, types.DefaultCreditDenom)
	assert.Equal(t, int64(5_000), balanceAfterLock.Amount.Int64())

	// Unlock
	err = keeper.UnlockCredits(ctx, lockID, "cancelled")
	require.NoError(t, err)

	// Verify balance restored
	balanceAfterUnlock := bank.GetBalance(ctx, router, types.DefaultCreditDenom)
	assert.Equal(t, int64(10_000), balanceAfterUnlock.Amount.Int64())

	// Verify lock is marked as released (not deleted - kept for audit)
	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found, "Lock should still exist for audit purposes")
	assert.Equal(t, types.LockStatus_LOCK_STATUS_RELEASED, lock.Status)
}

func TestUnlockCredits_NonExistentLock(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	err := keeper.UnlockCredits(ctx, "nonexistent-lock", "test")
	require.Error(t, err)
}

// =============================================================================
// Settlement Record Tests
// =============================================================================

func TestCreateAndGetSettlement(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	settlement := &types.SettlementRecord{
		Id:          "settlement-001",
		Status:      types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		ToolId:      "test-tool",
		UserId:      "user-1",
		PublisherId: "publisher-1",
		RouterId:    "router-1",
		Timestamp:   time.Now().UTC(),
	}

	err := keeper.CreateSettlement(ctx, settlement)
	require.NoError(t, err)

	// Retrieve and verify
	retrieved, found := keeper.GetSettlement(ctx, "settlement-001")
	require.True(t, found)
	assert.Equal(t, settlement.Id, retrieved.Id)
	assert.Equal(t, settlement.ToolId, retrieved.ToolId)
	assert.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, retrieved.Status)
}

func TestUpdateSettlement(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	settlement := &types.SettlementRecord{
		Id:        "settlement-002",
		Status:    types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Timestamp: time.Now().UTC(),
	}

	err := keeper.CreateSettlement(ctx, settlement)
	require.NoError(t, err)

	// Update status
	settlement.Status = types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED
	completedAt := time.Now().UTC()
	settlement.CompletedAt = &completedAt

	err = keeper.UpdateSettlement(ctx, settlement)
	require.NoError(t, err)

	// Verify update
	retrieved, found := keeper.GetSettlement(ctx, "settlement-002")
	require.True(t, found)
	assert.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, retrieved.Status)
	assert.NotNil(t, retrieved.CompletedAt)
}

// TestUpdateSettlement_PrimitiveAcceptsCallerTimestamp pins the
// low-level contract of UpdateSettlement: it is a dumb persistence
// primitive that stores whatever Timestamp the caller passed in. The
// dispute-window protection against router-driven resets
// (lumera_ai-1y27e) is enforced ONE LAYER UP, inside
// ProcessSettlement — see TestProcessSettlement_PreservesEarliestPendingTimestamp
// below for the pin on that boundary.
//
// Keeping the primitive dumb matters: non-router callers (genesis
// import, recovery tools, explicit timestamp migration) need the
// ability to set the stored Timestamp exactly. If anyone adds
// timestamp-preservation logic inside UpdateSettlement itself, this
// test flips and the author is forced to move the rule back up to
// ProcessSettlement where it belongs.
func TestUpdateSettlement_PrimitiveAcceptsCallerTimestamp(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	// T1: initial settlement, PENDING, Timestamp=T1.
	t1 := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	initial := &types.SettlementRecord{
		Id:        "settlement-update-primitive",
		Status:    types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Timestamp: t1,
		LockId:    "lock-update-primitive",
	}
	require.NoError(t, keeper.CreateSettlement(ctx, initial))

	// T2: same record re-persisted via UpdateSettlement with a different
	// Timestamp. Primitive must honor the caller's Timestamp — it is
	// NOT the layer that enforces the dispute-window anchor.
	t2 := t1.Add(30 * time.Minute)
	refreshed := &types.SettlementRecord{
		Id:        "settlement-update-primitive",
		Status:    types.SettlementStatus_SETTLEMENT_STATUS_PENDING,
		Timestamp: t2,
		LockId:    "lock-update-primitive",
	}
	require.NoError(t, keeper.UpdateSettlement(ctx, refreshed))

	after, found := keeper.GetSettlement(ctx, "settlement-update-primitive")
	require.True(t, found)
	require.Equal(t, t2.Unix(), after.Timestamp.Unix(),
		"UpdateSettlement must store whatever Timestamp the caller passes — "+
			"moving dispute-window anchoring INTO this primitive would "+
			"break genesis-import, recovery, and migration call sites that "+
			"need to set Timestamp exactly.")
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, after.Status)
}

// TestProcessSettlement_PreservesEarliestPendingTimestamp_DisputeWindowAnchored
// pins the dispute-window anchor rule that closes lumera_ai-1y27e:
// when ProcessSettlement runs on an async/partial-fill receipt that is
// ALREADY PENDING and the new transition is also PENDING, the stored
// Timestamp must stay at the FIRST attempt's block time — not be
// refreshed to the current block time.
//
// Why this matters (lumera_ai-1y27e):
//   - x/credits/abci.go:48 BeginBlocker skips settlements where
//     currentTime.Sub(settlementTime) < disputeWindow.
//   - If the Timestamp refreshed on every PENDING→PENDING
//     ProcessSettlement call, a malicious or compromised router could
//     re-settle once per block to keep `currentTime - settlementTime`
//     below the dispute window forever, blocking BeginBlocker from
//     ever transitioning the settlement to COMPLETED/FAILED.
//   - UnlockCredits (keeper.go:1452) refuses to release the bound
//     lock while PENDING, so the user's credits are stranded.
//
// The fix at x/credits/keeper/keeper.go (ProcessSettlement) preserves
// `existingRecord.Timestamp` on the PENDING→PENDING transition. This
// test exercises the exact path through ProcessSettlement twice at
// different block times and asserts the stored Timestamp is still T1.
//
// Sibling coverage:
//   - TestUpdateSettlement_PrimitiveAcceptsCallerTimestamp above pins
//     that the low-level UpdateSettlement primitive still honors
//     caller-supplied Timestamps — the fix lives in the ProcessSettlement
//     layer, not in the primitive.
//   - TestProcessSettlement_RefreshesTimestampOnFinalize below pins
//     that legitimate terminal transitions (PENDING→COMPLETED) still
//     update the Timestamp, so CompletedAt and Timestamp stay aligned
//     for pruning.
func TestProcessSettlement_PreservesEarliestPendingTimestamp_DisputeWindowAnchored(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	publisher := newAccAddress()
	router := newAccAddress()
	accKeeper.accounts[publisher.String()] = publisher
	accKeeper.accounts[router.String()] = router

	// Fund enough for two accumulated partial settlements.
	totalAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(totalAmount.Add(totalAmount)))

	// T1: first partial settlement. Stage != "" and != "finalized" →
	// isFinal=false → Status=PENDING.
	t1 := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	ctx = ctx.WithBlockTime(t1)
	_, err := keeper.ProcessSettlement(ctx, SettlementRequest{
		ReceiptID:     "receipt-dispute-anchor",
		TotalAmount:   sdk.NewCoins(totalAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ToolID:        "test-tool",
		Stage:         "partial",
	})
	require.NoError(t, err)

	initial, found := keeper.GetSettlement(ctx, "receipt-dispute-anchor")
	require.True(t, found)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, initial.Status,
		"precondition: first partial must land PENDING")
	require.Equal(t, t1.Unix(), initial.Timestamp.Unix(),
		"precondition: first PENDING Timestamp must be T1")
	require.EqualValues(t, 1, initial.FillCount)

	// T2: mid-dispute-window, same receipt settled again. This is the
	// malicious-router-resets-the-clock path — the one that previously
	// reset Timestamp to BlockTime() every time.
	t2 := t1.Add(30 * time.Minute)
	ctx = ctx.WithBlockTime(t2)
	_, err = keeper.ProcessSettlement(ctx, SettlementRequest{
		ReceiptID:     "receipt-dispute-anchor",
		TotalAmount:   sdk.NewCoins(totalAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ToolID:        "test-tool",
		Stage:         "partial",
	})
	require.NoError(t, err)

	after, found := keeper.GetSettlement(ctx, "receipt-dispute-anchor")
	require.True(t, found)

	// FIX POINT (lumera_ai-1y27e): Timestamp must be PRESERVED at T1.
	// If this assertion flips back to T2, the dispute-window anchor
	// regressed and routers can once again strand user credits by
	// re-settling every block.
	require.Equal(t, t1.Unix(), after.Timestamp.Unix(),
		"PENDING→PENDING re-settle must preserve earliest Timestamp (T1). "+
			"Got T2 → BeginBlocker's dispute-window eligibility check resets "+
			"every block and a malicious router can permanently strand the "+
			"bound lock (lumera_ai-1y27e).")
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_PENDING, after.Status,
		"sanity: PENDING→PENDING path, not a terminal transition")
	require.EqualValues(t, 2, after.FillCount,
		"sanity: FillCount must still advance — the anchor rule only "+
			"preserves Timestamp, not the rest of the record")
}

// TestProcessSettlement_RefreshesTimestampOnFinalize pins the carve-out
// in the lumera_ai-1y27e fix: when ProcessSettlement transitions a
// settlement from PENDING to COMPLETED, the Timestamp MUST advance to
// the terminal block time so it stays aligned with CompletedAt for
// downstream pruning (PruneOldSettlements cutoff semantics). The
// dispute-window anchor only applies to PENDING→PENDING.
func TestProcessSettlement_RefreshesTimestampOnFinalize(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	publisher := newAccAddress()
	router := newAccAddress()
	accKeeper.accounts[publisher.String()] = publisher
	accKeeper.accounts[router.String()] = router

	totalAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(totalAmount.Add(totalAmount)))

	// T1: partial settlement → PENDING.
	t1 := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	ctx = ctx.WithBlockTime(t1)
	_, err := keeper.ProcessSettlement(ctx, SettlementRequest{
		ReceiptID:     "receipt-finalize-refresh",
		TotalAmount:   sdk.NewCoins(totalAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ToolID:        "test-tool",
		Stage:         "partial",
	})
	require.NoError(t, err)

	// T2: finalize → COMPLETED. Timestamp must refresh to T2 so it
	// matches CompletedAt.
	t2 := t1.Add(45 * time.Minute)
	ctx = ctx.WithBlockTime(t2)
	_, err = keeper.ProcessSettlement(ctx, SettlementRequest{
		ReceiptID:     "receipt-finalize-refresh",
		TotalAmount:   sdk.NewCoins(totalAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ToolID:        "test-tool",
		Stage:         "finalized",
	})
	require.NoError(t, err)

	final, found := keeper.GetSettlement(ctx, "receipt-finalize-refresh")
	require.True(t, found)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, final.Status)
	require.Equal(t, t2.Unix(), final.Timestamp.Unix(),
		"finalization must refresh Timestamp to the terminal block time — "+
			"the dispute-window anchor is PENDING→PENDING only; "+
			"COMPLETED must have Timestamp==CompletedAt for pruning.")
	require.NotNil(t, final.CompletedAt)
	require.Equal(t, t2.Unix(), final.CompletedAt.Unix(),
		"sanity: CompletedAt tracks the finalize block time")
}

func TestProcessSettlement_TrimsFinalStageBeforeStatus(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	publisher := newAccAddress()
	router := newAccAddress()
	accKeeper.accounts[publisher.String()] = publisher
	accKeeper.accounts[router.String()] = router

	firstAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 10_000)
	finalAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 5_000)
	bank.FundAccount(moduleAddr, sdk.NewCoins(firstAmount.Add(finalAmount)))

	t1 := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	ctx = ctx.WithBlockTime(t1)
	_, err := keeper.ProcessSettlement(ctx, SettlementRequest{
		ReceiptID:     "receipt-padded-final-stage",
		TotalAmount:   sdk.NewCoins(firstAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ToolID:        "test-tool",
		Stage:         "partial",
	})
	require.NoError(t, err)

	t2 := t1.Add(45 * time.Minute)
	ctx = ctx.WithBlockTime(t2)
	_, err = keeper.ProcessSettlement(ctx, SettlementRequest{
		ReceiptID:     "receipt-padded-final-stage",
		TotalAmount:   sdk.NewCoins(finalAmount),
		PublisherAddr: publisher,
		RouterAddr:    router,
		PublisherID:   publisher.String(),
		RouterID:      router.String(),
		ToolID:        "test-tool",
		Stage:         " finalized ",
	})
	require.NoError(t, err)

	final, found := keeper.GetSettlement(ctx, "receipt-padded-final-stage")
	require.True(t, found)
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, final.Status)
	require.Equal(t, "finalized", final.Stage)
	require.NotNil(t, final.CompletedAt)
	require.Equal(t, t2.Unix(), final.CompletedAt.Unix())
}

// =============================================================================
// Pruning Tests
// =============================================================================

func TestPruneOldSettlements(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	// Create old settlements
	oldTime := time.Now().Add(-30 * 24 * time.Hour)
	for i := 0; i < 10; i++ {
		settlement := &types.SettlementRecord{
			Id:          fmt.Sprintf("old-settlement-%d", i),
			Status:      types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
			CompletedAt: &oldTime,
			Timestamp:   oldTime,
		}
		require.NoError(t, keeper.CreateSettlement(ctx, settlement))
	}

	// Create recent settlement
	recentCompletedAt := time.Now().UTC()
	recentSettlement := &types.SettlementRecord{
		Id:          "recent-settlement",
		Status:      types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED,
		CompletedAt: &recentCompletedAt,
		Timestamp:   time.Now().UTC(),
	}
	require.NoError(t, keeper.CreateSettlement(ctx, recentSettlement))

	// Prune settlements older than 7 days
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	err := keeper.PruneOldSettlements(ctx, cutoff, 100)
	require.NoError(t, err)

	// Recent settlement should still exist
	_, found := keeper.GetSettlement(ctx, "recent-settlement")
	assert.True(t, found, "Recent settlement should not be pruned")
}
