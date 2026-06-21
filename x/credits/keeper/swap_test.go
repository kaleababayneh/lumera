package keeper

import (
	"testing"

	"github.com/LumeraProtocol/lumera/x/credits/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestSwapLACtoLUME_InflationBug(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	// User account
	userAddr := newAccAddress()
	accKeeper.accounts[userAddr.String()] = userAddr

	// Setup:
	// 1. User has LUME
	// 2. User swaps LUME -> LAC (module escrows LUME, mints LAC)
	// 3. User swaps LAC -> LUME (module burns LAC, SHOULD send escrowed LUME)

	// Initial LUME supply in bank keeper
	lumeDenom := "ulume"
	lacDenom := types.DefaultCreditDenom

	initialLumeAmount := sdk.NewInt64Coin(lumeDenom, 1_000_000)
	bank.FundAccount(userAddr, sdk.NewCoins(initialLumeAmount))

	// Verify initial state
	require.Equal(t, initialLumeAmount, bank.GetBalance(ctx, userAddr, lumeDenom))
	require.True(t, bank.GetBalance(ctx, moduleAddr, lumeDenom).IsZero())

	// Step 1: Swap LUME to LAC
	// We call the keeper method directly since we are testing the logic
	lumeToSwap := sdk.NewInt64Coin(lumeDenom, 100_000)
	lacReceived, acqBurnAmount, err := keeper.SwapLUMEtoLAC(ctx, userAddr, lumeToSwap)
	require.NoError(t, err)

	// Check balances after swap 1
	// Module should hold the LUME minus acquisition burn (1% default)
	expectedModBalance := lumeToSwap.Sub(acqBurnAmount)
	require.Equal(t, expectedModBalance, bank.GetBalance(ctx, moduleAddr, lumeDenom))
	// User should hold the LAC
	require.Equal(t, lacReceived, bank.GetBalance(ctx, userAddr, lacDenom))
	// User should have less LUME
	remainingLume := initialLumeAmount.Sub(lumeToSwap)
	require.Equal(t, remainingLume, bank.GetBalance(ctx, userAddr, lumeDenom))

	// Track total supply of LUME now
	// User Balance + Module Balance
	userBal := bank.GetBalance(ctx, userAddr, lumeDenom)
	modBal := bank.GetBalance(ctx, moduleAddr, lumeDenom)
	_ = userBal.Add(modBal) // totalSupplyBeforeSwapBack - tracked for potential future supply check

	// Step 2: Swap LAC back to LUME
	// The bug is that this will MINT new LUME instead of sending from module
	// causing total supply to increase.

	// We need to account for burn on swap back
	// SwapLACtoLUME burns 1.5% of LAC input
	lacToSwap := lacReceived // Swap all LAC back
	lumeReceived, lumeBurned, err := keeper.SwapLACtoLUME(ctx, userAddr, lacToSwap)
	require.NoError(t, err)

	// Verify balances
	// User should have received LUME
	newUserBal := bank.GetBalance(ctx, userAddr, lumeDenom)
	require.Equal(t, userBal.Add(lumeReceived), newUserBal)

	// BUG CHECK:
	// If `SwapLACtoLUME` mints new LUME, the module balance will STILL be `lumeToSwap`.
	// If `SwapLACtoLUME` sends escrowed LUME, the module balance will be `lumeToSwap - lumeReceived`.

	newModBal := bank.GetBalance(ctx, moduleAddr, lumeDenom)

	// If bug exists (double minting), module balance didn't decrease
	// because it minted FRESH coins instead of using its balance.
	// But `mockBankKeeper.MintCoins` adds to module balance!
	// Wait, let's look at `mockBankKeeper.MintCoins`:
	// func (bk *mockBankKeeper) MintCoins(...) {
	//    bk.balances[bk.moduleAddr.String()] = bk.balances[bk.moduleAddr.String()].Add(amt...)
	// }
	// So MintCoins INCREASES module balance.
	// And then `SendCoinsFromModuleToAccount` moves it to user.
	// So if it Mints, the module balance effectively stays same (Mint + Send = Net 0 change relative to pre-mint, but absolute +Amt -Amt).
	// BUT the pre-existing escrowed LUME is still there!

	// So:
	// Before Swap Back: Module has 100k LUME (escrowed).
	// If Correct (Unlock): Module sends ~98.5k LUME to user. Module has ~1.5k LUME left.
	// If Bug (Mint): Module Mints 98.5k LUME (Balance becomes 198.5k). Then Sends 98.5k to user. Balance becomes 100k.

	// So we assert that Module Balance should have DECREASED.
	if newModBal.Equal(modBal) {
		t.Fatalf("Inflation Bug Detected: Module balance did not decrease. Escrowed LUME was stranded, new LUME was minted. Module Bal: %s", newModBal)
	}

	// Module loses lumeReceived (sent to user) + LUME burn (destroyed).
	// lumeBurned is returned in LAC denom but same numeric amount is also burned in LUME.
	expectedModBal := modBal.Amount.Sub(lumeReceived.Amount).Sub(lumeBurned.Amount)
	require.True(t, expectedModBal.Equal(newModBal.Amount),
		"Module balance should decrease by the amount returned to user plus burn: expected %s, got %s", expectedModBal, newModBal.Amount)
}
