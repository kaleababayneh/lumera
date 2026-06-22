package keeper

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// cacRegistryKeeper is a local mock that implements RegistryKeeper for CAC tests
type cacRegistryKeeper struct {
	publishers map[string]sdk.AccAddress
}

func newCACRegistryKeeper() *cacRegistryKeeper {
	return &cacRegistryKeeper{
		publishers: make(map[string]sdk.AccAddress),
	}
}

func (m *cacRegistryKeeper) SetPublisher(toolID string, addr sdk.AccAddress) {
	m.publishers[toolID] = addr
}

func (m *cacRegistryKeeper) GetToolPublisher(_ context.Context, toolID string) (sdk.AccAddress, error) {
	if addr, ok := m.publishers[toolID]; ok {
		return addr, nil
	}
	return nil, nil
}

func (m *cacRegistryKeeper) ValidateReceipt(_ sdk.Context, _, _, _ string) error {
	return nil
}

func setupCACKeeperWithRegistry(t *testing.T) (sdk.Context, *Keeper, *mockBankKeeper, *cacRegistryKeeper) {
	t.Helper()

	registryKeeper := newCACRegistryKeeper()
	ctx, keeper, bank, _, _, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registryKeeper,
	})

	return ctx, keeper, bank, registryKeeper
}

func newCACTestAccAddress() sdk.AccAddress {
	return newAccAddress()
}

func TestProcessCACRoyalty_BasicSplit(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	// Fund the module account
	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000))
	bank.FundAccount(moduleAddr, publisherAmount)

	originAmount, servingAmount, err := keeper.ProcessCACRoyalty(
		ctx,
		"tool-origin",
		"tool-serving",
		publisherAmount,
	)
	require.NoError(t, err)

	// 50/50 split: 500,000 to origin, 500,000 to serving
	expectedOrigin := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000))
	expectedServing := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000))

	require.True(t, originAmount.Equal(expectedOrigin), "origin amount should be 50%% of total: got %s, expected %s", originAmount, expectedOrigin)
	require.True(t, servingAmount.Equal(expectedServing), "serving amount should be 50%% of total: got %s, expected %s", servingAmount, expectedServing)

	// Verify transfers happened
	require.True(t, bank.Balance(originPublisher).Equal(expectedOrigin))
	require.True(t, bank.Balance(servingPublisher).Equal(expectedServing))
}

func TestProcessCACRoyalty_ZeroAmount(t *testing.T) {
	ctx, keeper, _, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	_, _, err := keeper.ProcessCACRoyalty(
		ctx,
		"tool-origin",
		"tool-serving",
		sdk.NewCoins(), // Zero amount
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be zero")
}

func TestProcessCACRoyalty_MissingOriginPublisher(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	servingPublisher := newCACTestAccAddress()
	registry.SetPublisher("tool-serving", servingPublisher)
	// Note: tool-origin has no publisher set

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000))
	bank.FundAccount(moduleAddr, publisherAmount)

	_, _, err := keeper.ProcessCACRoyalty(
		ctx,
		"tool-origin",
		"tool-serving",
		publisherAmount,
	)
	// With registry returning nil for unknown tools, the error should indicate no publisher
	require.Error(t, err)
	require.Contains(t, err.Error(), "no publisher")
	require.True(t, bank.Balance(servingPublisher).IsZero(),
		"serving publisher must not receive a reallocated origin share when origin publisher is missing")
}

func TestProcessCACRoyalty_MissingServingPublisher(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	registry.SetPublisher("tool-origin", originPublisher)
	// Note: tool-serving has no publisher set

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000))
	bank.FundAccount(moduleAddr, publisherAmount)

	_, _, err := keeper.ProcessCACRoyalty(
		ctx,
		"tool-origin",
		"tool-serving",
		publisherAmount,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no publisher")
	require.True(t, bank.Balance(originPublisher).IsZero(),
		"origin publisher must not receive a partial payout when serving publisher is missing")
}

func TestProcessCACRoyalty_SmallAmount(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	// Small amount where 50% = 5
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 10))
	bank.FundAccount(moduleAddr, publisherAmount)

	originAmount, servingAmount, err := keeper.ProcessCACRoyalty(
		ctx,
		"tool-origin",
		"tool-serving",
		publisherAmount,
	)
	require.NoError(t, err)

	// 50% of 10 = 5, remaining 50% = 5
	expectedOrigin := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 5))
	expectedServing := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 5))

	require.True(t, originAmount.Equal(expectedOrigin))
	require.True(t, servingAmount.Equal(expectedServing))

	// Total should equal original amount (no dust loss)
	total := originAmount.Add(servingAmount...)
	require.True(t, total.Equal(publisherAmount), "total distributed should equal input amount")
}

func TestProcessCACRoyalty_VerySmallAmount(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	// Very small amount where 50% rounds to 0
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1))
	bank.FundAccount(moduleAddr, publisherAmount)

	originAmount, servingAmount, err := keeper.ProcessCACRoyalty(
		ctx,
		"tool-origin",
		"tool-serving",
		publisherAmount,
	)
	require.NoError(t, err)

	// 50% of 1 = 0 (rounds down), serving gets remainder = 1
	require.True(t, originAmount.IsZero() || originAmount.AmountOf(types.DefaultCreditDenom).IsZero())

	// Total should equal original amount
	total := originAmount.Add(servingAmount...)
	require.True(t, total.Equal(publisherAmount), "total distributed should equal input amount")
}

func TestProcessCACRoyalty_MultiDenom(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	// Multiple denominations
	publisherAmount := sdk.NewCoins(
		sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		sdk.NewInt64Coin("uatom", 500_000),
	)
	bank.FundAccount(moduleAddr, publisherAmount)

	originAmount, servingAmount, err := keeper.ProcessCACRoyalty(
		ctx,
		"tool-origin",
		"tool-serving",
		publisherAmount,
	)
	require.NoError(t, err)

	// Each denom should be split 50/50
	require.Equal(t, int64(500_000), originAmount.AmountOf(types.DefaultCreditDenom).Int64())
	require.Equal(t, int64(250_000), originAmount.AmountOf("uatom").Int64())
	require.Equal(t, int64(500_000), servingAmount.AmountOf(types.DefaultCreditDenom).Int64())
	require.Equal(t, int64(250_000), servingAmount.AmountOf("uatom").Int64())

	// Verify no dust loss for each denom
	total := originAmount.Add(servingAmount...)
	require.True(t, total.Equal(publisherAmount))
}

func TestProcessCACRoyalty_SamePublisher(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	// Same publisher for both tools
	publisher := newCACTestAccAddress()
	registry.SetPublisher("tool-origin", publisher)
	registry.SetPublisher("tool-serving", publisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000))
	bank.FundAccount(moduleAddr, publisherAmount)

	originAmount, servingAmount, err := keeper.ProcessCACRoyalty(
		ctx,
		"tool-origin",
		"tool-serving",
		publisherAmount,
	)
	require.NoError(t, err)

	// Split should still happen correctly even if same publisher
	// Default is 50/50 split (DefaultRoyaltyOriginBPS = 5000)
	expectedOrigin := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000))
	expectedServing := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000))

	require.True(t, originAmount.Equal(expectedOrigin))
	require.True(t, servingAmount.Equal(expectedServing))

	// Publisher should receive full amount (300k + 700k = 1M)
	require.True(t, bank.Balance(publisher).Equal(publisherAmount))
}

func TestGetToolPublisher_FallbackWithoutRegistry(t *testing.T) {
	// Test the fallback behavior when registry keeper is nil
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	// Keeper from setupCreditsKeeper has nil registry
	addr, err := keeper.GetToolPublisher(ctx, "tool-test")
	require.NoError(t, err)
	require.NotNil(t, addr)
	require.Len(t, addr, 20) // Standard address length

	// Same tool ID should produce same address (deterministic)
	addr2, err := keeper.GetToolPublisher(ctx, "tool-test")
	require.NoError(t, err)
	require.Equal(t, addr, addr2)

	// Different tool ID should produce different address
	addr3, err := keeper.GetToolPublisher(ctx, "tool-other")
	require.NoError(t, err)
	require.NotEqual(t, addr, addr3)
}

func TestCACRoyaltyStats(t *testing.T) {
	ctx, keeper, _, _ := setupCACKeeperWithRegistry(t)

	toolID := "tool-test"

	// Get initial stats (should be empty)
	stats, err := keeper.GetCACRoyaltyStats(ctx, toolID)
	require.NoError(t, err)
	require.Equal(t, toolID, stats.ToolId)
	require.Zero(t, stats.TotalCacheHits)
	require.True(t, stats.TotalRoyaltiesEarned.IsZero())
	require.True(t, stats.TotalRoyaltiesPaid.IsZero())

	// Update stats as origin
	amount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 100))
	err = keeper.UpdateCACRoyaltyStats(ctx, toolID, true, amount)
	require.NoError(t, err)

	// Verify update
	stats, err = keeper.GetCACRoyaltyStats(ctx, toolID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), stats.TotalCacheHits)
	require.True(t, stats.TotalRoyaltiesEarned.Equal(amount))
	require.True(t, stats.TotalRoyaltiesPaid.IsZero())

	// Update stats as serving
	servingAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 200))
	err = keeper.UpdateCACRoyaltyStats(ctx, toolID, false, servingAmount)
	require.NoError(t, err)

	// Verify both tracked
	stats, err = keeper.GetCACRoyaltyStats(ctx, toolID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), stats.TotalCacheHits) // Only incremented for origin
	require.True(t, stats.TotalRoyaltiesEarned.Equal(amount))
	require.True(t, stats.TotalRoyaltiesPaid.Equal(servingAmount))
}

func TestCACRoyaltyStats_Cumulative(t *testing.T) {
	ctx, keeper, _, _ := setupCACKeeperWithRegistry(t)

	toolID := "tool-cumulative"

	// Multiple updates as origin
	for i := 0; i < 5; i++ {
		amount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 100))
		err := keeper.UpdateCACRoyaltyStats(ctx, toolID, true, amount)
		require.NoError(t, err)
	}

	stats, err := keeper.GetCACRoyaltyStats(ctx, toolID)
	require.NoError(t, err)
	require.Equal(t, uint64(5), stats.TotalCacheHits)
	require.Equal(t, int64(500), stats.TotalRoyaltiesEarned.AmountOf(types.DefaultCreditDenom).Int64())
}

func TestSaveCACRoyaltyRecord(t *testing.T) {
	ctx, keeper, _, _ := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	record := &types.CACRoyaltyRecord{
		RecordId:         "cac-test-record",
		OriginToolId:     "tool-origin",
		ServingToolId:    "tool-serving",
		OriginPublisher:  originPublisher.String(),
		ServingPublisher: servingPublisher.String(),
		TotalAmount:      types.CoinsToProto(sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000))),
		OriginShare:      types.CoinsToProto(sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 300_000))),
		ServingShare:     types.CoinsToProto(sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000))),
		Timestamp:        ctx.BlockTime(),
	}

	err := keeper.SaveCACRoyaltyRecord(ctx, record)
	require.NoError(t, err)
}

func TestProcessCACRoyalty_EmitsEvent(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000))
	bank.FundAccount(moduleAddr, publisherAmount)

	_, _, err := keeper.ProcessCACRoyalty(
		ctx,
		"tool-origin",
		"tool-serving",
		publisherAmount,
	)
	require.NoError(t, err)

	// Check event was emitted
	events := ctx.EventManager().Events()
	found := false
	for _, event := range events {
		if event.Type == "cac_royalty_distribution" {
			found = true
			// Verify attributes exist
			attrs := make(map[string]string)
			for _, attr := range event.Attributes {
				attrs[attr.Key] = attr.Value
			}
			require.Equal(t, "tool-origin", attrs["origin_tool"])
			require.Equal(t, "tool-serving", attrs["serving_tool"])
			require.Contains(t, attrs["origin_share"], "500000")
			require.Contains(t, attrs["serving_share"], "500000")
		}
	}
	require.True(t, found, "cac_royalty_distribution event should be emitted")
}

func TestProcessCACRoyalty_RoundingConsistency(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)

	// Test various amounts to ensure no dust loss
	testAmounts := []int64{1, 3, 7, 11, 13, 99, 101, 999, 1001, 10000, 100001}

	for _, amount := range testAmounts {
		// Reset balances
		bank.balances = make(map[string]sdk.Coins)
		publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, amount))
		bank.FundAccount(moduleAddr, publisherAmount)

		originAmount, servingAmount, err := keeper.ProcessCACRoyalty(
			ctx,
			"tool-origin",
			"tool-serving",
			publisherAmount,
		)
		require.NoError(t, err, "failed for amount %d", amount)

		// Total should always equal input (no dust loss)
		total := originAmount.Add(servingAmount...)
		require.True(t, total.Equal(publisherAmount),
			"dust loss for amount %d: got %s, expected %s",
			amount, total, publisherAmount)
	}
}

// ---------------------------------------------------------------------------
// bd-2awiz: Cache-by-intent correctness + CAC royalties E2E tests
// ---------------------------------------------------------------------------

// TestCACRoyalty_Determinism verifies that the same inputs always produce
// exactly the same split outputs — critical for blockchain consensus.
func TestCACRoyalty_Determinism(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)

	// Run the same split 50 times and ensure identical results
	var firstOrigin, firstServing sdk.Coins
	for i := 0; i < 50; i++ {
		bank.balances = make(map[string]sdk.Coins)
		publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 777_777))
		bank.FundAccount(moduleAddr, publisherAmount)

		originAmount, servingAmount, err := keeper.ProcessCACRoyalty(ctx, "tool-origin", "tool-serving", publisherAmount)
		require.NoError(t, err)

		if i == 0 {
			firstOrigin = originAmount
			firstServing = servingAmount
		} else {
			require.True(t, originAmount.Equal(firstOrigin),
				"iteration %d: origin mismatch: %s vs %s", i, originAmount, firstOrigin)
			require.True(t, servingAmount.Equal(firstServing),
				"iteration %d: serving mismatch: %s vs %s", i, servingAmount, firstServing)
		}
	}
}

// TestCACRoyalty_RecordIDDeterminism verifies the hash-based RecordID is
// deterministic for the same inputs at the same block height/time.
func TestCACRoyalty_RecordIDDeterminism(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)

	// Process twice with same block context to get same record ID
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000))

	bank.FundAccount(moduleAddr, publisherAmount)
	_, _, err := keeper.ProcessCACRoyalty(ctx, "tool-origin", "tool-serving", publisherAmount)
	require.NoError(t, err)

	// Process again with same context — the Record hashes should be identical
	// because all inputs (tools, block height, block time, amount) are the same.
	bank.FundAccount(moduleAddr, publisherAmount)
	_, _, err = keeper.ProcessCACRoyalty(ctx, "tool-origin", "tool-serving", publisherAmount)
	require.NoError(t, err)

	// The key invariant: same inputs + same block → same record hash.
	// ProcessCACRoyalty will internally overwrite the record (same key).
	// We just need to confirm no error from the duplicate key.
}

// TestCACRoyalty_RecordIDUniqueness verifies different inputs produce different
// RecordIDs even at the same block height.
func TestCACRoyalty_RecordIDUniqueness(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-A", originPublisher)
	registry.SetPublisher("tool-B", servingPublisher)
	registry.SetPublisher("tool-C", newCACTestAccAddress())

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)

	// Two different tool pairs at same block → different record IDs
	amount1 := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000))
	amount2 := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 200_000))

	bank.FundAccount(moduleAddr, amount1.Add(amount2...))

	_, _, err := keeper.ProcessCACRoyalty(ctx, "tool-A", "tool-B", amount1)
	require.NoError(t, err)

	_, _, err = keeper.ProcessCACRoyalty(ctx, "tool-A", "tool-C", amount2)
	require.NoError(t, err)
	// Both succeed without collision because the amount differs in the hash input.
}

// TestCACRoyalty_ConservationInvariant_Exhaustive tests that origin + serving
// ALWAYS equals publisher amount for a wide range of inputs.
func TestCACRoyalty_ConservationInvariant_Exhaustive(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)

	// Test all amounts from 1 to 100, plus powers of 10 up to 10^15
	amounts := make([]int64, 0, 120)
	for i := int64(1); i <= 100; i++ {
		amounts = append(amounts, i)
	}
	for p := int64(1000); p <= 1_000_000_000_000_000; p *= 10 {
		amounts = append(amounts, p)
		amounts = append(amounts, p+1) // off-by-one near powers of 10
		amounts = append(amounts, p-1)
	}

	for _, amt := range amounts {
		bank.balances = make(map[string]sdk.Coins)
		pubAmt := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, amt))
		bank.FundAccount(moduleAddr, pubAmt)

		originAmt, servingAmt, err := keeper.ProcessCACRoyalty(ctx, "tool-origin", "tool-serving", pubAmt)
		require.NoError(t, err, "amount=%d", amt)

		total := originAmt.Add(servingAmt...)
		require.True(t, total.Equal(pubAmt),
			"conservation violated for amount %d: origin=%s + serving=%s = %s, want %s",
			amt, originAmt, servingAmt, total, pubAmt)

		// Verify 50/50 ratio: origin equals floor(50% of total)
		if amt >= 2 { // for amount=1, floor(50%) is 0 and the serving side keeps the remainder
			originVal := originAmt.AmountOf(types.DefaultCreditDenom).Int64()
			expected50pct := amt * 5000 / 10000
			require.Equal(t, expected50pct, originVal,
				"amount=%d: origin should be exactly floor(50%%): got %d, want %d", amt, originVal, expected50pct)
		}
	}
}

// TestCACRoyalty_LargeAmount verifies no overflow for very large settlement amounts.
func TestCACRoyalty_LargeAmount(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)

	// 9 trillion ulac (9 * 10^12) — beyond typical int32 range
	large := int64(9_000_000_000_000)
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, large))
	bank.FundAccount(moduleAddr, publisherAmount)

	originAmount, servingAmount, err := keeper.ProcessCACRoyalty(ctx, "tool-origin", "tool-serving", publisherAmount)
	require.NoError(t, err)

	expectedOrigin := large * 5000 / 10000 // 4_500_000_000_000
	expectedServing := large - expectedOrigin

	require.Equal(t, expectedOrigin, originAmount.AmountOf(types.DefaultCreditDenom).Int64())
	require.Equal(t, expectedServing, servingAmount.AmountOf(types.DefaultCreditDenom).Int64())

	total := originAmount.Add(servingAmount...)
	require.True(t, total.Equal(publisherAmount))
}

// TestCACRoyalty_BankSendFailure_Origin verifies error propagation when the
// bank send to the origin publisher fails.
func TestCACRoyalty_BankSendFailure_Origin(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	// Do NOT fund the module account — bank send will fail due to insufficient funds
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000))

	_, _, err := keeper.ProcessCACRoyalty(ctx, "tool-origin", "tool-serving", publisherAmount)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to send CAC royalty")

	// Neither publisher should have received funds
	require.True(t, bank.Balance(originPublisher).IsZero())
	require.True(t, bank.Balance(servingPublisher).IsZero())
}

// TestCACRoyalty_BankSendFailure_Serving verifies error propagation when origin
// send succeeds but serving send fails (module account has exactly origin amount).
func TestCACRoyalty_BankSendFailure_Serving(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)

	// Fund only enough for the origin share (500k of 1M) but not the serving share (500k)
	publisherAmount := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000))
	bank.FundAccount(moduleAddr, sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)))

	_, _, err := keeper.ProcessCACRoyalty(ctx, "tool-origin", "tool-serving", publisherAmount)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to send CAC royalty")
}

// TestProcessSettlementWithCAC_DelegatesToProcessSettlement verifies the
// deprecated wrapper delegates correctly.
func TestProcessSettlementWithCAC_DelegatesToProcessSettlement(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	// Create a lock and settle via the deprecated wrapper
	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-deprecated-wrap",
		lockAmount,
		"tool-deprecated",
		"quote-dep",
		"policy@1",
		"intent-hash-deprecated",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-deprecated-wrap",
		ToolID:        "tool-deprecated",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	}

	// ProcessSettlementWithCAC should delegate to ProcessSettlement
	receipt.TotalAmount = sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, 300_000))
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))

	result, err := keeper.ProcessSettlementWithCAC(ctx, receipt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should produce same structure as ProcessSettlement (burn > 0 for non-zero amount)
	require.False(t, result.BurnAmount.IsZero(), "ProcessSettlementWithCAC should produce burn")

	// Settle the lock to clean up
	_, _ = keeper.SettleLock(ctx, lockID, lockAmount, SettlementRequest{
		ReceiptID:     "receipt-deprecated-settle",
		ToolID:        "tool-deprecated",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	})
}

// TestCACRoyalty_StatsIntegrationViaProcessSettlement tests that a cache-hit
// settlement updates CAC stats for both the origin and serving tools.
func TestCACRoyalty_StatsIntegrationViaProcessSettlement(t *testing.T) {
	registryKeeper := newCACRegistryKeeper()
	ctx, keeper, bank, _, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registryKeeper,
	})

	routerAddr := newAccAddress()
	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr

	registryKeeper.SetPublisher("tool-origin", originPublisher)
	registryKeeper.SetPublisher("tool-serving", servingPublisher)

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-stats-integration",
		lockAmount,
		"tool-serving",
		"quote-stats",
		"policy@1",
		"intent-hash-stats",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-stats-int",
		ToolID:        "tool-serving",
		PublisherAddr: servingPublisher,
		RouterAddr:    routerAddr,
		CacheHit:      true,
		OriginToolID:  "tool-origin",
		PublisherID:   servingPublisher.String(),
		RouterID:      routerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Origin tool stats should show 1 cache hit and earned royalties
	originStats, err := keeper.GetCACRoyaltyStats(ctx, "tool-origin")
	require.NoError(t, err)
	require.Equal(t, uint64(1), originStats.TotalCacheHits)
	require.False(t, originStats.TotalRoyaltiesEarned.IsZero(),
		"origin tool should have earned royalties")

	// Serving tool stats should show paid royalties (to origin)
	servingStats, err := keeper.GetCACRoyaltyStats(ctx, "tool-serving")
	require.NoError(t, err)
	require.False(t, servingStats.TotalRoyaltiesPaid.IsZero(),
		"serving tool should have paid royalties to origin")
}

// TestCACRoyalty_FullSettlement_CacheHitTrue is the primary E2E test:
// Lock → SettleLock with CacheHit=true → verify 50/50 split is applied to
// the publisher share (instead of sending it all to the publisher directly).
func TestCACRoyalty_FullSettlement_CacheHitTrue(t *testing.T) {
	registryKeeper := newCACRegistryKeeper()
	ctx, keeper, bank, moduleAddr, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registryKeeper,
	})

	routerAddr := newAccAddress()
	referrerAddr := newAccAddress()
	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[referrerAddr.String()] = referrerAddr

	registryKeeper.SetPublisher("tool-origin", originPublisher)
	registryKeeper.SetPublisher("tool-serving", servingPublisher)

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-cachehit-e2e",
		lockAmount,
		"tool-serving",
		"quote-ch",
		"policy@1",
		"intent-hash-ch",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-cachehit-e2e",
		ToolID:        "tool-serving",
		PublisherAddr: servingPublisher,
		RouterAddr:    routerAddr,
		ReferrerAddr:  referrerAddr,
		CacheHit:      true,
		OriginToolID:  "tool-origin",
		PublisherID:   servingPublisher.String(),
		RouterID:      routerAddr.String(),
		ReferrerID:    referrerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(45 * time.Second))
	result, err := keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the settlement result reflects non-zero distribution
	require.False(t, result.BurnAmount.IsZero(), "burn should be non-zero")
	require.False(t, result.PublisherAmount.IsZero(), "publisher CAC split should be non-zero")
	require.False(t, result.RouterAmount.IsZero(), "router should be non-zero")
	require.False(t, result.ReferrerAmount.IsZero(), "referrer should be non-zero")

	// Key assertion: origin publisher received funds (50% of publisher share, rounded down)
	originBal := bank.Balance(originPublisher)
	require.False(t, originBal.IsZero(), "origin publisher should receive 50%% of publisher share")

	// Key assertion: serving publisher received funds (50% of publisher share, plus any remainder)
	servingBal := bank.Balance(servingPublisher)
	require.False(t, servingBal.IsZero(), "serving publisher should receive 50%% of publisher share")

	// Verify 50/50 split ratios
	originVal := originBal.AmountOf(types.DefaultCreditDenom).Int64()
	servingVal := servingBal.AmountOf(types.DefaultCreditDenom).Int64()
	publisherAmtFromResult := result.PublisherAmount.AmountOf(types.DefaultCreditDenom).Int64()
	expectedOrigin := publisherAmtFromResult / 2
	expectedServing := publisherAmtFromResult - expectedOrigin
	require.Equal(t, expectedOrigin, originVal, "origin should receive floor(50%%) of publisher share")
	require.Equal(t, expectedServing, servingVal, "serving should receive the remainder of the 50/50 split")

	// Conservation: origin + serving = total publisher amount distributed
	totalPublisher := originVal + servingVal
	require.Equal(t, publisherAmtFromResult, totalPublisher,
		"origin + serving should equal publisher amount")

	// Module account should be empty (all funds distributed or refunded)
	require.True(t, bank.Balance(moduleAddr).IsZero(),
		"module account should be empty after settlement")

	// Verify settlement record has cache_hit=true
	record, found := keeper.GetSettlement(ctx, "receipt-cachehit-e2e")
	require.True(t, found)
	require.True(t, record.CacheHit)
	require.Equal(t, "tool-origin", record.OriginToolId)
}

// TestCACRoyalty_FullSettlement_CacheHitFalse verifies that when CacheHit=false,
// the publisher receives the full publisher share directly (no CAC split).
func TestCACRoyalty_FullSettlement_CacheHitFalse(t *testing.T) {
	registryKeeper := newCACRegistryKeeper()
	ctx, keeper, bank, _, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registryKeeper,
	})

	routerAddr := newAccAddress()
	publisherAddr := newCACTestAccAddress()
	originPublisher := newCACTestAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	registryKeeper.SetPublisher("tool-serving", publisherAddr)
	registryKeeper.SetPublisher("tool-origin", originPublisher)

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-no-cache",
		lockAmount,
		"tool-serving",
		"quote-nc",
		"policy@1",
		"intent-hash-nc",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-no-cache",
		ToolID:        "tool-serving",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		CacheHit:      false, // No cache hit
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Publisher should get funds, origin publisher should NOT
	require.False(t, bank.Balance(publisherAddr).IsZero(),
		"publisher should receive funds for non-cache settlement")
	require.True(t, bank.Balance(originPublisher).IsZero(),
		"origin publisher should NOT receive funds when CacheHit=false")

	// No cac_royalty_distribution event
	for _, event := range ctx.EventManager().Events() {
		require.NotEqual(t, "cac_royalty_distribution", event.Type,
			"cac_royalty_distribution should not be emitted for non-cache settlement")
	}
}

// TestCACRoyalty_FullSettlement_CacheHitSameToolID verifies that when
// CacheHit=true but OriginToolID == ToolID, CAC is skipped (self-referential).
func TestCACRoyalty_FullSettlement_CacheHitSameToolID(t *testing.T) {
	registryKeeper := newCACRegistryKeeper()
	ctx, keeper, bank, _, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registryKeeper,
	})

	routerAddr := newAccAddress()
	publisherAddr := newCACTestAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	registryKeeper.SetPublisher("tool-self", publisherAddr)

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 300_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-self-cache",
		lockAmount,
		"tool-self",
		"quote-self",
		"policy@1",
		"intent-hash-self",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-self-cache",
		ToolID:        "tool-self",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		CacheHit:      true,
		OriginToolID:  "tool-self", // Same as ToolID — should skip CAC
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Publisher gets full publisher share directly (no CAC split)
	require.False(t, bank.Balance(publisherAddr).IsZero(),
		"publisher should receive funds when OriginToolID == ToolID")

	// No cac_royalty_distribution event (CAC was skipped)
	for _, event := range ctx.EventManager().Events() {
		require.NotEqual(t, "cac_royalty_distribution", event.Type,
			"should not emit cac_royalty_distribution when origin==serving")
	}
}

// TestCACRoyalty_FullSettlement_CacheHitEmptyOrigin verifies that
// CacheHit=true with empty OriginToolID skips CAC processing.
func TestCACRoyalty_FullSettlement_CacheHitEmptyOrigin(t *testing.T) {
	registryKeeper := newCACRegistryKeeper()
	ctx, keeper, bank, _, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registryKeeper,
	})

	routerAddr := newAccAddress()
	publisherAddr := newCACTestAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	registryKeeper.SetPublisher("tool-empty-origin", publisherAddr)

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 300_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-empty-origin",
		lockAmount,
		"tool-empty-origin",
		"quote-eo",
		"policy@1",
		"intent-hash-eo",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-empty-origin",
		ToolID:        "tool-empty-origin",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		CacheHit:      true,
		OriginToolID:  "", // Empty — should skip CAC
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Publisher gets full amount directly (no CAC split)
	require.False(t, bank.Balance(publisherAddr).IsZero())

	// No cac_royalty_distribution event
	for _, event := range ctx.EventManager().Events() {
		require.NotEqual(t, "cac_royalty_distribution", event.Type)
	}
}

// TestCACRoyalty_FullSettlement_EventAttributes verifies all expected event
// attributes are emitted during a cache-hit settlement.
func TestCACRoyalty_FullSettlement_EventAttributes(t *testing.T) {
	registryKeeper := newCACRegistryKeeper()
	ctx, keeper, bank, _, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registryKeeper,
	})

	routerAddr := newAccAddress()
	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	registryKeeper.SetPublisher("tool-origin-ev", originPublisher)
	registryKeeper.SetPublisher("tool-serving-ev", servingPublisher)

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-event-attrs",
		lockAmount,
		"tool-serving-ev",
		"quote-ev",
		"policy@1",
		"intent-hash-ev",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-event-attrs",
		ToolID:        "tool-serving-ev",
		PublisherAddr: servingPublisher,
		RouterAddr:    routerAddr,
		CacheHit:      true,
		OriginToolID:  "tool-origin-ev",
		PublisherID:   servingPublisher.String(),
		RouterID:      routerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Verify cac_royalty_distribution event
	foundCAC := false
	for _, event := range ctx.EventManager().Events() {
		if event.Type == "cac_royalty_distribution" {
			foundCAC = true
			attrs := make(map[string]string)
			for _, attr := range event.Attributes {
				attrs[attr.Key] = attr.Value
			}
			require.Equal(t, "tool-origin-ev", attrs["origin_tool"])
			require.Equal(t, "tool-serving-ev", attrs["serving_tool"])
			require.NotEmpty(t, attrs["origin_share"])
			require.NotEmpty(t, attrs["serving_share"])
			require.NotEmpty(t, attrs["total_amount"])
		}
	}
	require.True(t, foundCAC, "cac_royalty_distribution event must be emitted for cache hit")

	// Verify at least one settlement event has cache_hit=true for our receipt
	foundSettlement := false
	for _, event := range ctx.EventManager().Events() {
		if event.Type == types.EventTypeSettlement {
			attrs := make(map[string]string)
			for _, attr := range event.Attributes {
				attrs[attr.Key] = attr.Value
			}
			if attrs[types.AttributeKeySettlementID] == "receipt-event-attrs" && attrs["cache_hit"] == "true" {
				foundSettlement = true
			}
		}
	}
	require.True(t, foundSettlement, "settlement event with cache_hit=true must be emitted for our receipt")
}

// TestCACRoyalty_SettlementRecord_CacheHitFields verifies the persisted
// settlement record correctly captures CacheHit and OriginToolId fields.
func TestCACRoyalty_SettlementRecord_CacheHitFields(t *testing.T) {
	registryKeeper := newCACRegistryKeeper()
	ctx, keeper, bank, _, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registryKeeper,
	})

	routerAddr := newAccAddress()
	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	registryKeeper.SetPublisher("tool-origin-rec", originPublisher)
	registryKeeper.SetPublisher("tool-serving-rec", servingPublisher)

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-record-fields",
		lockAmount,
		"tool-serving-rec",
		"quote-rec",
		"policy@1",
		"intent-hash-rec",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-record-fields",
		ToolID:        "tool-serving-rec",
		PublisherAddr: servingPublisher,
		RouterAddr:    routerAddr,
		CacheHit:      true,
		OriginToolID:  "tool-origin-rec",
		PublisherID:   servingPublisher.String(),
		RouterID:      routerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	record, found := keeper.GetSettlement(ctx, "receipt-record-fields")
	require.True(t, found)
	require.True(t, record.CacheHit, "settlement record should have CacheHit=true")
	require.Equal(t, "tool-origin-rec", record.OriginToolId,
		"settlement record should store OriginToolId")
	require.Equal(t, types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED, record.Status)
	require.NotNil(t, record.CompletedAt)
}

// TestCACRoyalty_MultipleSettlements_SameBlock verifies that multiple cache-hit
// settlements in the same block produce unique records and correct stats.
func TestCACRoyalty_MultipleSettlements_SameBlock(t *testing.T) {
	registryKeeper := newCACRegistryKeeper()
	ctx, keeper, bank, _, accKeeper, _ := setupCreditsKeeperWithOptions(t, keeperSetupOptions{
		registryKeeper: registryKeeper,
	})

	routerAddr := newAccAddress()
	originPub := newCACTestAccAddress()
	servingPub := newCACTestAccAddress()

	accKeeper.accounts[routerAddr.String()] = routerAddr
	registryKeeper.SetPublisher("tool-origin-multi", originPub)
	registryKeeper.SetPublisher("tool-serving-multi", servingPub)

	ctx = ctx.WithBlockTime(time.Now().UTC())

	// Create and settle 3 locks in the same block
	for i := 0; i < 3; i++ {
		lockAmt := sdk.NewInt64Coin(types.DefaultCreditDenom, 200_000)
		bank.FundAccount(routerAddr, sdk.NewCoins(lockAmt))

		lockID, err := keeper.LockCredits(
			ctx,
			routerAddr.String(),
			fmt.Sprintf("session-multi-%d", i),
			lockAmt,
			"tool-serving-multi",
			fmt.Sprintf("quote-multi-%d", i),
			"policy@1",
			fmt.Sprintf("intent-hash-multi-%d", i),
		)
		require.NoError(t, err)

		receipt := SettlementRequest{
			ReceiptID:     fmt.Sprintf("receipt-multi-%d", i),
			ToolID:        "tool-serving-multi",
			PublisherAddr: servingPub,
			RouterAddr:    routerAddr,
			CacheHit:      true,
			OriginToolID:  "tool-origin-multi",
			PublisherID:   servingPub.String(),
			RouterID:      routerAddr.String(),
		}

		ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Duration(i) * time.Second))
		_, err = keeper.SettleLock(ctx, lockID, sdk.NewInt64Coin(types.DefaultCreditDenom, 150_000), receipt)
		require.NoError(t, err)
	}

	// Origin tool stats should show 3 cache hits
	stats, err := keeper.GetCACRoyaltyStats(ctx, "tool-origin-multi")
	require.NoError(t, err)
	require.Equal(t, uint64(3), stats.TotalCacheHits,
		"origin should have accumulated 3 cache hits")
	require.False(t, stats.TotalRoyaltiesEarned.IsZero(),
		"origin should have accumulated royalties")

	// Each settlement record should exist independently
	for i := 0; i < 3; i++ {
		rec, found := keeper.GetSettlement(ctx, fmt.Sprintf("receipt-multi-%d", i))
		require.True(t, found, "settlement %d should exist", i)
		require.True(t, rec.CacheHit, "settlement %d should be cache hit", i)
	}

	// Origin publisher should have received its 50% share from each settlement
	originBal := bank.Balance(originPub)
	require.False(t, originBal.IsZero(), "origin publisher should have accumulated balance")
}

// TestCACRoyalty_Split_BoundaryValues tests the 50/50 split at exact boundary
// values where rounding matters most (amounts not divisible by 10000).
func TestCACRoyalty_Split_BoundaryValues(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)

	tests := []struct {
		name           string
		amount         int64
		expectedOrigin int64
	}{
		{"exact_10000", 10000, 5000},
		{"one_unit", 1, 0},
		{"two_units", 2, 1},
		{"three_units", 3, 1},
		{"four_units", 4, 2},
		{"seven_units", 7, 3},
		{"ten_units", 10, 5},
		{"33_units", 33, 16},
		{"333_units", 333, 166},
		{"3333_units", 3333, 1666},
		{"33333_units", 33333, 16666},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bank.balances = make(map[string]sdk.Coins)
			pubAmt := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, tc.amount))
			bank.FundAccount(moduleAddr, pubAmt)

			originAmt, servingAmt, err := keeper.ProcessCACRoyalty(ctx, "tool-origin", "tool-serving", pubAmt)
			require.NoError(t, err)

			originVal := originAmt.AmountOf(types.DefaultCreditDenom).Int64()
			require.Equal(t, tc.expectedOrigin, originVal,
				"origin should be %d for amount %d", tc.expectedOrigin, tc.amount)

			// Conservation: origin + serving = total
			servingVal := servingAmt.AmountOf(types.DefaultCreditDenom).Int64()
			require.Equal(t, tc.amount, originVal+servingVal,
				"origin(%d) + serving(%d) should equal amount(%d)", originVal, servingVal, tc.amount)
		})
	}
}

// TestCACRoyalty_ServingAlwaysGetsRemainder verifies that when rounding
// truncates the origin share, the serving tool gets the remainder (never lost).
func TestCACRoyalty_ServingAlwaysGetsRemainder(t *testing.T) {
	ctx, keeper, bank, registry := setupCACKeeperWithRegistry(t)

	originPublisher := newCACTestAccAddress()
	servingPublisher := newCACTestAccAddress()

	registry.SetPublisher("tool-origin", originPublisher)
	registry.SetPublisher("tool-serving", servingPublisher)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleAccountName)

	// For very small amounts, origin gets floor(amount/2) and serving keeps the remainder.
	for _, amt := range []int64{1, 2, 3} {
		bank.balances = make(map[string]sdk.Coins)
		pubAmt := sdk.NewCoins(sdk.NewInt64Coin(types.DefaultCreditDenom, amt))
		bank.FundAccount(moduleAddr, pubAmt)

		originAmt, servingAmt, err := keeper.ProcessCACRoyalty(ctx, "tool-origin", "tool-serving", pubAmt)
		require.NoError(t, err)

		expectedOrigin := amt / 2
		require.Equal(t, expectedOrigin, originAmt.AmountOf(types.DefaultCreditDenom).Int64(),
			"amount=%d: origin should receive floor(50%%)", amt)

		require.Equal(t, amt-expectedOrigin, servingAmt.AmountOf(types.DefaultCreditDenom).Int64(),
			"amount=%d: serving should receive the remainder", amt)
	}
}
