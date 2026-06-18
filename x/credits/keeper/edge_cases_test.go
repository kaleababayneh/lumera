//go:build cosmos && cosmos_full

package keeper

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// TestLockCredits_ZeroAmount verifies that the exported keeper path matches the
// Msg service and genesis invariants: lock amounts must be positive.
func TestLockCredits_ZeroAmount(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	zeroAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 0)

	// Fund router with some balance (but we'll lock zero)
	fundAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(fundAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-zero",
		zeroAmount,
		"tool-zero",
		"quote-zero",
		"policy@1",
		"intent-hash-zero",
	)

	require.Error(t, err, "zero amount lock should fail")
	require.Contains(t, err.Error(), "positive")
	require.Empty(t, lockID, "no lock ID should be returned")
	require.Equal(t, sdk.NewCoins(fundAmount), bank.Balance(routerAddr))
	require.True(t, bank.Balance(moduleAddr).IsZero())
}

func TestLockCredits_RejectsNonCreditDenom(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	wrongDenomAmount := sdk.NewInt64Coin("uatom", 1_000_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(wrongDenomAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-wrong-denom",
		wrongDenomAmount,
		"tool-wrong-denom",
		"quote-wrong-denom",
		"policy@1",
		"intent-hash-wrong-denom",
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "expected denom")
	require.Empty(t, lockID)
	require.Equal(t, sdk.NewCoins(wrongDenomAmount), bank.Balance(routerAddr))
	require.True(t, bank.Balance(moduleAddr).IsZero())
}

func TestLockCredits_RejectsMalformedLockIdentity(t *testing.T) {
	ctx, keeper, bank, moduleAddr, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))
	ctx = ctx.WithBlockTime(time.Now().UTC())

	tests := []struct {
		name       string
		sessionID  string
		toolID     string
		toolpackID string
		want       string
	}{
		{
			name:      "empty session",
			sessionID: "",
			toolID:    "tool-valid",
			want:      "session_id",
		},
		{
			name:      "padded session",
			sessionID: " session-valid",
			toolID:    "tool-valid",
			want:      "session_id",
		},
		{
			name:      "empty tool",
			sessionID: "session-valid",
			toolID:    "",
			want:      "tool_id",
		},
		{
			name:      "padded tool",
			sessionID: "session-valid",
			toolID:    "tool-valid ",
			want:      "tool_id",
		},
		{
			name:       "padded toolpack",
			sessionID:  "session-valid",
			toolID:     "tool-valid",
			toolpackID: " toolpack-valid ",
			want:       "toolpack_id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lockID, err := keeper.LockCredits(
				ctx,
				routerAddr.String(),
				tc.sessionID,
				lockAmount,
				tc.toolID,
				"quote-"+tc.name,
				"policy@1",
				"intent-hash-"+tc.name,
				tc.toolpackID,
			)

			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			require.Empty(t, lockID)
			require.Equal(t, sdk.NewCoins(lockAmount), bank.Balance(routerAddr))
			require.True(t, bank.Balance(moduleAddr).IsZero())
		})
	}
}

// TestLockCredits_InsufficientFunds verifies that locking more than available balance fails.
func TestLockCredits_InsufficientFunds(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	// Fund router with small amount
	fundAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 100)
	bank.FundAccount(routerAddr, sdk.NewCoins(fundAmount))

	// Try to lock more than available
	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)

	ctx = ctx.WithBlockTime(time.Now().UTC())

	_, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-insufficient",
		lockAmount,
		"tool-insufficient",
		"quote-insufficient",
		"policy@1",
		"intent-hash-insufficient",
	)

	require.Error(t, err, "locking more than available should fail")
	require.Contains(t, err.Error(), "insufficient")
}

// TestSettleLock_DoubleBurn verifies that settling the same lock twice fails.
func TestSettleLock_DoubleBurn(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	// Create lock
	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-double",
		lockAmount,
		"tool-double",
		"quote-double",
		"policy@1",
		"intent-hash-double",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-double",
		ToolID:        "tool-double",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	}

	// First settlement should succeed
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Second settlement of the same lock should fail
	receipt2 := SettlementRequest{
		ReceiptID:     "receipt-double-2",
		ToolID:        "tool-double",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	}

	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt2)
	require.Error(t, err, "double settlement should fail")
}

// TestReleaseLock_AlreadyBurned verifies that releasing an already settled lock fails.
func TestReleaseLock_AlreadyBurned(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	actualCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000)

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	// Create and settle lock
	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-burned",
		lockAmount,
		"tool-burned",
		"quote-burned",
		"policy@1",
		"intent-hash-burned",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-burned",
		ToolID:        "tool-burned",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))
	_, err = keeper.SettleLock(ctx, lockID, actualCost, receipt)
	require.NoError(t, err)

	// Try to release the burned lock
	err = keeper.UnlockCredits(ctx, lockID, "test-release")
	require.Error(t, err, "releasing burned lock should fail")
}

// TestReleaseLock_DoubleRelease verifies that releasing the same lock twice fails.
func TestReleaseLock_DoubleRelease(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	// Create lock
	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-double-release",
		lockAmount,
		"tool-double-release",
		"quote-double-release",
		"policy@1",
		"intent-hash-double-release",
	)
	require.NoError(t, err)

	// First release should succeed
	err = keeper.UnlockCredits(ctx, lockID, "first-release")
	require.NoError(t, err)

	// Second release should fail
	err = keeper.UnlockCredits(ctx, lockID, "second-release")
	require.Error(t, err, "double release should fail")
}

// TestSettleLock_ZeroActualCostCompletesSettlement verifies that free-tool
// settlements still create records for verification and metrics.
func TestSettleLock_ZeroActualCostCompletesSettlement(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)
	zeroCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 0) // Zero cost

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	// Create lock
	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-cache-hit",
		lockAmount,
		"tool-cache-hit",
		"quote-cache-hit",
		"policy@1",
		"intent-hash-cache-hit",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-cache-hit",
		ToolID:        "tool-cache-hit",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))

	// Zero-cost settlements are allowed (e.g. free tools) to ensure a
	// SettlementRecord is created for verification/metrics.
	result, err := keeper.SettleLock(ctx, lockID, zeroCost, receipt)
	require.NoError(t, err, "zero cost settlement should succeed for free tools")
	require.NotNil(t, result)

	// Verify lock is burned and full amount is refunded
	lock, found := keeper.GetLock(ctx, lockID)
	require.True(t, found)
	require.Equal(t, types.LockStatus_LOCK_STATUS_BURNED, lock.Status)
}

// TestSettleLock_ActualCostExceedsLock verifies that actual cost > locked amount fails.
func TestSettleLock_ActualCostExceedsLock(t *testing.T) {
	ctx, keeper, bank, _, accKeeper := setupCreditsKeeper(t)

	routerAddr := newAccAddress()
	publisherAddr := newAccAddress()
	accKeeper.accounts[routerAddr.String()] = routerAddr
	accKeeper.accounts[publisherAddr.String()] = publisherAddr

	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000)
	excessCost := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000) // More than locked

	bank.FundAccount(routerAddr, sdk.NewCoins(lockAmount))

	ctx = ctx.WithBlockTime(time.Now().UTC())

	// Create lock
	lockID, err := keeper.LockCredits(
		ctx,
		routerAddr.String(),
		"session-excess",
		lockAmount,
		"tool-excess",
		"quote-excess",
		"policy@1",
		"intent-hash-excess",
	)
	require.NoError(t, err)

	receipt := SettlementRequest{
		ReceiptID:     "receipt-excess",
		ToolID:        "tool-excess",
		PublisherAddr: publisherAddr,
		RouterAddr:    routerAddr,
		PublisherID:   publisherAddr.String(),
		RouterID:      routerAddr.String(),
	}

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(30 * time.Second))

	// Settlement with cost exceeding lock should fail
	_, err = keeper.SettleLock(ctx, lockID, excessCost, receipt)
	require.Error(t, err, "settlement with cost exceeding lock should fail")
}

// TestEconomics_BurnRateCalculation verifies burn rate calculations match expected values.
func TestEconomics_BurnRateCalculation(t *testing.T) {
	tests := []struct {
		name         string
		total        int64
		burnRateBPS  uint32
		expectedBurn int64
		expectedRem  int64
	}{
		{
			name:         "3% burn on 1M",
			total:        1_000_000,
			burnRateBPS:  300,
			expectedBurn: 30_000,  // 1M * 3% = 30K
			expectedRem:  970_000, // 1M - 30K = 970K
		},
		{
			name:         "0% burn",
			total:        1_000_000,
			burnRateBPS:  0,
			expectedBurn: 0,
			expectedRem:  1_000_000,
		},
		{
			name:         "10% burn",
			total:        500_000,
			burnRateBPS:  1000,
			expectedBurn: 50_000,
			expectedRem:  450_000,
		},
		{
			name:         "small amount with rounding",
			total:        100,
			burnRateBPS:  300, // 3%
			expectedBurn: 3,   // 100 * 3% = 3
			expectedRem:  97,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			total := sdkmath.NewInt(tc.total)
			burn, remaining, err := CalculateBurnAmount(total, tc.burnRateBPS)
			require.NoError(t, err)
			require.Equal(t, tc.expectedBurn, burn.Int64(),
				"burn amount mismatch: expected %d, got %d", tc.expectedBurn, burn.Int64())
			require.Equal(t, tc.expectedRem, remaining.Int64(),
				"remaining amount mismatch: expected %d, got %d", tc.expectedRem, remaining.Int64())
		})
	}
}

// TestEconomics_SplitCalculation verifies revenue split calculations sum correctly.
func TestEconomics_SplitCalculation(t *testing.T) {
	tests := []struct {
		name          string
		amount        int64
		publisherBPS  uint32
		routerBPS     uint32
		originSurfBPS uint32
		treasuryBPS   uint32
		referrerBPS   uint32
		expectErr     bool
	}{
		{
			name:          "70/20/0/10/0 split",
			amount:        1_000_000,
			publisherBPS:  7000,
			routerBPS:     2000,
			originSurfBPS: 0,
			treasuryBPS:   1000,
			referrerBPS:   0,
			expectErr:     false,
		},
		{
			name:          "equal split 5 ways",
			amount:        1_000_000,
			publisherBPS:  2000,
			routerBPS:     2000,
			originSurfBPS: 2000,
			treasuryBPS:   2000,
			referrerBPS:   2000,
			expectErr:     false,
		},
		{
			name:          "splits don't sum to 10000",
			amount:        1_000_000,
			publisherBPS:  5000,
			routerBPS:     2000,
			originSurfBPS: 1000,
			treasuryBPS:   1000,
			referrerBPS:   0, // Only 9000 total
			expectErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			amount := sdkmath.NewInt(tc.amount)
			pub, router, origin, treasury, ref, err := CalculateSplit(
				amount,
				tc.publisherBPS,
				tc.routerBPS,
				tc.originSurfBPS,
				tc.treasuryBPS,
				tc.referrerBPS,
			)

			if tc.expectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify all splits sum to original amount
			total := pub.Add(router).Add(origin).Add(treasury).Add(ref)
			require.Equal(t, tc.amount, total.Int64(),
				"splits should sum to original amount: expected %d, got %d",
				tc.amount, total.Int64())

			// Verify each split is non-negative
			require.True(t, pub.GTE(sdkmath.ZeroInt()), "publisher split should be >= 0")
			require.True(t, router.GTE(sdkmath.ZeroInt()), "router split should be >= 0")
			require.True(t, origin.GTE(sdkmath.ZeroInt()), "origin surface split should be >= 0")
			require.True(t, treasury.GTE(sdkmath.ZeroInt()), "treasury split should be >= 0")
			require.True(t, ref.GTE(sdkmath.ZeroInt()), "referrer split should be >= 0")
		})
	}
}

// TestEconomics_ValidateRates verifies rate validation catches invalid configurations.
func TestEconomics_ValidateRates(t *testing.T) {
	tests := []struct {
		name          string
		burnRate      uint32
		insuranceRate uint32
		pubShare      uint32
		routerShare   uint32
		originShare   uint32
		treasuryShare uint32
		refShare      uint32
		expectErr     bool
		errContains   string
	}{
		{
			name:          "valid config",
			burnRate:      300,
			insuranceRate: 200,
			pubShare:      7000,
			routerShare:   2000,
			originShare:   0,
			treasuryShare: 1000,
			refShare:      0,
			expectErr:     false,
		},
		{
			name:          "burn rate exceeds 100%",
			burnRate:      10001,
			insuranceRate: 0,
			pubShare:      7000,
			routerShare:   2000,
			originShare:   0,
			treasuryShare: 1000,
			refShare:      0,
			expectErr:     true,
			errContains:   "burn rate",
		},
		{
			name:          "insurance rate exceeds 100%",
			burnRate:      0,
			insuranceRate: 10001,
			pubShare:      7000,
			routerShare:   2000,
			originShare:   0,
			treasuryShare: 1000,
			refShare:      0,
			expectErr:     true,
			errContains:   "insurance rate",
		},
		{
			name:          "combined deductions exceed 100%",
			burnRate:      6000,
			insuranceRate: 5000, // 60% + 50% = 110%
			pubShare:      7000,
			routerShare:   2000,
			originShare:   0,
			treasuryShare: 1000,
			refShare:      0,
			expectErr:     true,
			errContains:   "combined",
		},
		{
			name:          "shares don't sum to 100%",
			burnRate:      300,
			insuranceRate: 200,
			pubShare:      5000, // Only 80% total
			routerShare:   2000,
			originShare:   0,
			treasuryShare: 1000,
			refShare:      0,
			expectErr:     true,
			errContains:   "sum to 10000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRates(
				tc.burnRate,
				tc.insuranceRate,
				tc.pubShare,
				tc.routerShare,
				tc.originShare,
				tc.treasuryShare,
				tc.refShare,
			)

			if tc.expectErr {
				require.Error(t, err)
				if tc.errContains != "" {
					require.Contains(t, err.Error(), tc.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestLockCredits_UnregisteredAccount verifies that locks from unregistered accounts fail.
func TestLockCredits_UnregisteredAccount(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	// Create address but don't register it
	unregisteredAddr := newAccAddress()
	lockAmount := sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000)

	ctx = ctx.WithBlockTime(time.Now().UTC())

	// Try to lock from unregistered account
	_, err := keeper.LockCredits(
		ctx,
		unregisteredAddr.String(),
		"session-unregistered",
		lockAmount,
		"tool-unregistered",
		"quote-unregistered",
		"policy@1",
		"intent-hash-unregistered",
	)

	// Should fail because account isn't funded/registered
	require.Error(t, err, "lock from unregistered account should fail")
}
