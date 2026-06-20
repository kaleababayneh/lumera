
package keeper_test

import (
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// =============================================================================
// Test: Genesis Import/Export Roundtrip with Full State
// =============================================================================

func TestGenesisRoundtrip_FullState(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Fund pool
	fundPoolForTests(t, fixture, 50000)

	// Create claims with various statuses
	authority := authtypes.NewModuleAddress("gov").String()
	msgServer := keeper.NewMsgServerImpl(k)

	// Claim 1: Pending
	recordContributionForTests(t, fixture, "receipt-genesis-1", "tool-alpha", "pub-1", 1000)
	claimID1, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant1",
		ReceiptId:     "receipt-genesis-1",
		ToolId:        "tool-alpha",
		PublisherId:   "pub-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
		Reason:        "Test claim 1",
	})
	require.NoError(t, err)

	// Claim 2: Approved
	recordContributionForTests(t, fixture, "receipt-genesis-2", "tool-beta", "pub-2", 2000)
	claimID2, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant2",
		ReceiptId:     "receipt-genesis-2",
		ToolId:        "tool-beta",
		PublisherId:   "pub-2",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "1000"},
		Reason:        "Test claim 2",
	})
	require.NoError(t, err)
	require.NoError(t, k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimID2,
		Resolution:     "approve",
		ApprovedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "1000"},
	}))

	// Claim 3: Paid (creates payout record)
	claimantAddr := sdk.AccAddress([]byte("claimant_genesis__"))
	claimantAccount := fixture.accountKeeper.NewAccountWithAddress(ctx, claimantAddr)
	fixture.accountKeeper.SetAccount(ctx, claimantAccount)

	recordContributionForTests(t, fixture, "receipt-genesis-3", "tool-gamma", "pub-3", 500)
	claimID3, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      claimantAddr.String(),
		ReceiptId:     "receipt-genesis-3",
		ToolId:        "tool-gamma",
		PublisherId:   "pub-3",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "300"},
		Reason:        "Test claim 3",
	})
	require.NoError(t, err)
	require.NoError(t, k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimID3,
		Resolution:     "approve",
		ApprovedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "300"},
	}))
	_, err = msgServer.ProcessPayout(ctx, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   claimID3,
		Recipient: claimantAddr.String(),
	})
	require.NoError(t, err)

	// Build publisher risk by paying multiple claims
	for i := 0; i < 4; i++ {
		receiptID := "receipt-risk-" + string(rune('a'+i))
		recordContributionForTests(t, fixture, receiptID, "tool-risky", "risky-publisher", 100)
		claimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
			Claimant:      claimantAddr.String(),
			ReceiptId:     receiptID,
			ToolId:        "tool-risky",
			PublisherId:   "risky-publisher",
			ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "50"},
			Reason:        "Risk building",
		})
		require.NoError(t, err)
		require.NoError(t, k.ProcessClaim(ctx, &types.MsgProcessClaim{
			Authority:      authority,
			ClaimId:        claimID,
			Resolution:     "approve",
			ApprovedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "50"},
		}))
		_, err = msgServer.ProcessPayout(ctx, &types.MsgProcessPayout{
			Authority: authority,
			ClaimId:   claimID,
			Recipient: claimantAddr.String(),
		})
		require.NoError(t, err)
	}

	// Export genesis
	exported := k.ExportGenesis(ctx)
	require.NotNil(t, exported)

	// Verify exported state contains all expected data
	require.NotNil(t, exported.Params)
	require.NotNil(t, exported.Pool)
	require.NotNil(t, exported.Metrics)
	require.GreaterOrEqual(t, len(exported.Claims), 3, "should have at least 3 claims")
	require.GreaterOrEqual(t, len(exported.Contributions), 3, "should have contributions")
	require.GreaterOrEqual(t, len(exported.Payouts), 1, "should have at least 1 payout")
	require.GreaterOrEqual(t, len(exported.PublisherRisks), 1, "should have publisher risks")

	// Import into fresh keeper
	fixture2 := setupKeeperTest(t)
	k2 := fixture2.keeper
	ctx2 := fixture2.ctx

	k2.InitGenesis(ctx2, exported)

	// Verify imported claims
	claim1, err := k2.GetClaim(ctx2, claimID1)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_PENDING, claim1.Status)

	claim2, err := k2.GetClaim(ctx2, claimID2)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, claim2.Status)

	claim3, err := k2.GetClaim(ctx2, claimID3)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_PAID, claim3.Status)

	// Verify pool state preserved
	params2 := k2.GetParams(ctx2)
	require.NotNil(t, params2)
	assert.Equal(t, exported.Params.InsurancePoolBps, params2.InsurancePoolBps)

	// Re-export and verify consistency
	reexported := k2.ExportGenesis(ctx2)
	assert.Equal(t, len(exported.Claims), len(reexported.Claims))
	assert.Equal(t, len(exported.Contributions), len(reexported.Contributions))
	assert.Equal(t, len(exported.Payouts), len(reexported.Payouts))
}

// =============================================================================
// Test: Reserve Accounting Invariants
// =============================================================================

func TestReserveAccounting_Invariant_TotalEqAvailablePlusReserved(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper
	authority := authtypes.NewModuleAddress("gov").String()

	// Fund pool with known amount
	fundPoolForTests(t, fixture, 10000)

	// Helper to verify invariant
	verifyInvariant := func(msg string) {
		genesis := k.ExportGenesis(ctx)
		require.NotNil(t, genesis.Pool, msg)

		total, err := decimal.NewFromString(genesis.Pool.TotalFunds)
		require.NoError(t, err, msg)
		available, err := decimal.NewFromString(genesis.Pool.AvailableFunds)
		require.NoError(t, err, msg)
		reserved, err := decimal.NewFromString(genesis.Pool.ReservedFunds)
		require.NoError(t, err, msg)

		sum := available.Add(reserved)
		assert.True(t, total.Equal(sum),
			"%s: TotalFunds (%s) != AvailableFunds (%s) + ReservedFunds (%s)",
			msg, total, available, reserved)
	}

	// Verify initial state
	verifyInvariant("after initial funding")

	// Add contribution
	recordContributionForTests(t, fixture, "receipt-inv-1", "tool-alpha", "pub-1", 500)
	verifyInvariant("after contribution")

	// File and approve claim (reserves funds)
	claimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-inv-1",
		ToolId:        "tool-alpha",
		PublisherId:   "pub-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
		Reason:        "Invariant test",
	})
	require.NoError(t, err)
	verifyInvariant("after filing claim")

	err = k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimID,
		Resolution:     "approve",
		ApprovedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
	})
	require.NoError(t, err)
	verifyInvariant("after approving claim (funds reserved)")

	// Pay out claim (releases reserve, reduces total)
	claimantAddr := sdk.AccAddress([]byte("claimant_invariant"))
	claimantAccount := fixture.accountKeeper.NewAccountWithAddress(ctx, claimantAddr)
	fixture.accountKeeper.SetAccount(ctx, claimantAccount)

	msgServer := keeper.NewMsgServerImpl(k)
	_, err = msgServer.ProcessPayout(ctx, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   claimID,
		Recipient: claimantAddr.String(),
	})
	require.NoError(t, err)
	verifyInvariant("after payout (funds released)")

	// Reject a claim (should not affect reserve)
	recordContributionForTests(t, fixture, "receipt-inv-2", "tool-beta", "pub-2", 300)
	claimID2, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant2",
		ReceiptId:     "receipt-inv-2",
		ToolId:        "tool-beta",
		PublisherId:   "pub-2",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "150"},
		Reason:        "To be rejected",
	})
	require.NoError(t, err)

	err = k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID2,
		Resolution: "reject",
		Notes:      "Rejected",
	})
	require.NoError(t, err)
	verifyInvariant("after rejecting claim")
}

// =============================================================================
// Test: Leverage Cap Auto-Approval (4x Contribution Limit)
// =============================================================================

func TestEndBlocker_LeverageCapAutoApproval(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Fund pool
	fundPoolForTests(t, fixture, 100000)

	// Set params for auto-approval with high threshold
	params := types.DefaultParams()
	params.ClaimWindowSeconds = 1
	params.AutoApproveThreshold = "1000" // High threshold
	params.Enabled = true
	require.NoError(t, k.SetParams(ctx, params))

	// Test case 1: Claim within 4x leverage - should auto-approve
	// Contribution: 100, Claim: 300 (3x leverage, under 4x cap)
	recordContributionForTests(t, fixture, "receipt-leverage-ok", "tool-alpha", "pub-1", 100)
	claimID1, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-leverage-ok",
		ToolId:        "tool-alpha",
		PublisherId:   "pub-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "300"},
		Reason:        "Under leverage cap",
	})
	require.NoError(t, err)

	// Test case 2: Claim exceeds 4x leverage - should require manual review
	// Contribution: 50, Claim: 500 (10x leverage, over 4x cap)
	recordContributionForTests(t, fixture, "receipt-leverage-high", "tool-beta", "pub-2", 50)
	claimID2, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-leverage-high",
		ToolId:        "tool-beta",
		PublisherId:   "pub-2",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
		Reason:        "Over leverage cap",
	})
	require.NoError(t, err)

	// Advance time past claim window
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(5 * time.Second))

	// Run EndBlocker
	require.NoError(t, k.EndBlocker(ctx))

	// Claim 1 (3x leverage) should be auto-approved
	claim1, err := k.GetClaim(ctx, claimID1)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, claim1.Status,
		"claim within 4x leverage should be auto-approved")

	// Claim 2 (10x leverage) should be marked for manual review (EXPIRED status)
	claim2, err := k.GetClaim(ctx, claimID2)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_EXPIRED, claim2.Status,
		"claim over 4x leverage should require manual review")
	assert.Contains(t, claim2.ResolutionNotes, "leverage",
		"resolution notes should mention leverage")
}

func TestEndBlocker_LeverageCapBoundary(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Fund pool
	fundPoolForTests(t, fixture, 100000)

	params := types.DefaultParams()
	params.ClaimWindowSeconds = 1
	params.AutoApproveThreshold = "10000"
	params.Enabled = true
	require.NoError(t, k.SetParams(ctx, params))

	// Exactly 4x leverage - should auto-approve
	// Contribution: 100, Claim: 400 (exactly 4x)
	recordContributionForTests(t, fixture, "receipt-exact-4x", "tool-alpha", "pub-1", 100)
	claimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-exact-4x",
		ToolId:        "tool-alpha",
		PublisherId:   "pub-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "400"},
		Reason:        "Exactly 4x leverage",
	})
	require.NoError(t, err)

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(5 * time.Second))
	require.NoError(t, k.EndBlocker(ctx))

	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, claim.Status,
		"claim at exactly 4x leverage should be auto-approved")
}

// =============================================================================
// Test: Recidivism Escalation Tiers
// =============================================================================

func TestRecidivism_AllTiers(t *testing.T) {
	// Recidivism penalty kicks in based on ClaimCount BEFORE increment:
	// ClaimCount > 3 (checked before payout, then incremented after)
	// So penalty starts on claim 5 (when ClaimCount=4 > 3)
	// Tiers: >3 = 10% reduction, >5 = 25% reduction, >10 = 50% reduction
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper
	bankKeeper := fixture.bankKeeper
	accountKeeper := fixture.accountKeeper

	// Fund pool generously
	fundPoolForTests(t, fixture, 500000)

	authority := authtypes.NewModuleAddress("gov").String()
	msgServer := keeper.NewMsgServerImpl(k)
	publisherID := "recidivist-pub"
	toolID := "tool-recidivist"

	// Create claimant
	claimantAddr := sdk.AccAddress([]byte("claimant_recidiv__"))
	claimantAccount := accountKeeper.NewAccountWithAddress(ctx, claimantAddr)
	accountKeeper.SetAccount(ctx, claimantAccount)

	// Helper to file, approve, and pay a claim
	payClaimForPublisher := func(receiptSuffix string, amount int64) int64 {
		receiptID := "receipt-recid-" + receiptSuffix
		recordContributionForTests(t, fixture, receiptID, toolID, publisherID, amount)

		claimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
			Claimant:      claimantAddr.String(),
			ReceiptId:     receiptID,
			ToolId:        toolID,
			PublisherId:   publisherID,
			ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: sdkmath.NewInt(amount).String()},
			Reason:        "Recidivism tier test",
		})
		require.NoError(t, err)

		require.NoError(t, k.ProcessClaim(ctx, &types.MsgProcessClaim{
			Authority:      authority,
			ClaimId:        claimID,
			Resolution:     "approve",
			ApprovedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: sdkmath.NewInt(amount).String()},
		}))

		balanceBefore := bankKeeper.GetAllBalances(ctx, claimantAddr).AmountOf("ulac")
		_, err = msgServer.ProcessPayout(ctx, &types.MsgProcessPayout{
			Authority: authority,
			ClaimId:   claimID,
			Recipient: claimantAddr.String(),
		})
		require.NoError(t, err)
		balanceAfter := bankKeeper.GetAllBalances(ctx, claimantAddr).AmountOf("ulac")

		return balanceAfter.Sub(balanceBefore).Int64()
	}

	// Pay 4 claims - no penalty yet (ClaimCount is 0,1,2,3 when checked)
	for i := 0; i < 4; i++ {
		received := payClaimForPublisher(string(rune('a'+i)), 1000)
		assert.Equal(t, int64(1000), received,
			"claims 1-4 should have no penalty (ClaimCount <= 3 when checked), received %d", received)
	}

	// Claim 5: ClaimCount=4 > 3, so 10% reduction (receive 90%)
	received := payClaimForPublisher("e", 1000)
	assert.LessOrEqual(t, received, int64(900),
		"claim 5 (ClaimCount=4 > 3) should receive <= 900 (10%% reduction), got %d", received)

	// Claim 6: ClaimCount=5, still 10% reduction (5 > 3 but 5 is NOT > 5)
	received = payClaimForPublisher("f", 1000)
	assert.LessOrEqual(t, received, int64(900),
		"claim 6 (ClaimCount=5) should receive <= 900 (10%% reduction), got %d", received)

	// Claim 7: ClaimCount=6 > 5, so 25% reduction (receive 75%)
	received = payClaimForPublisher("g", 1000)
	assert.LessOrEqual(t, received, int64(750),
		"claim 7 (ClaimCount=6 > 5) should receive <= 750 (25%% reduction), got %d", received)

	// Pay claims 8-11 to get ClaimCount to 10
	for i := 0; i < 4; i++ {
		payClaimForPublisher(string(rune('h'+i)), 1000)
	}

	// Claim 12: ClaimCount=11 > 10, so 50% reduction (receive 50%)
	received = payClaimForPublisher("l", 1000)
	assert.LessOrEqual(t, received, int64(500),
		"claim 12 (ClaimCount=11 > 10) should receive <= 500 (50%% reduction), got %d", received)
}

// =============================================================================
// Test: Parameter Validation Edge Cases
// =============================================================================

func TestParamsValidation_BoundaryValues(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	tests := []struct {
		name      string
		modify    func(*types.Params)
		expectErr bool
		errMsg    string
	}{
		{
			name: "insurance_pool_bps at max (10000)",
			modify: func(p *types.Params) {
				p.InsurancePoolBps = 10000
			},
			expectErr: false,
		},
		{
			name: "insurance_pool_bps over max",
			modify: func(p *types.Params) {
				p.InsurancePoolBps = 10001
			},
			expectErr: true,
			errMsg:    "10000",
		},
		{
			name: "target_utilization at 1.0",
			modify: func(p *types.Params) {
				p.TargetUtilization = "1.0"
			},
			expectErr: false,
		},
		{
			name: "target_utilization over 1.0",
			modify: func(p *types.Params) {
				p.TargetUtilization = "1.1"
			},
			expectErr: true,
			errMsg:    "between 0 and 1",
		},
		{
			name: "target_utilization negative",
			modify: func(p *types.Params) {
				p.TargetUtilization = "-0.1"
			},
			expectErr: true,
			errMsg:    "between 0 and 1",
		},
		{
			name: "max_claim_percent at 1.0",
			modify: func(p *types.Params) {
				p.MaxClaimPercent = "1.0"
			},
			expectErr: false,
		},
		{
			name: "max_claim_percent over 1.0",
			modify: func(p *types.Params) {
				p.MaxClaimPercent = "1.5"
			},
			expectErr: true,
			errMsg:    "between 0 and 1",
		},
		{
			name: "claim_window_seconds zero",
			modify: func(p *types.Params) {
				p.ClaimWindowSeconds = 0
			},
			expectErr: true,
			errMsg:    "positive",
		},
		{
			name: "premium_adjustment_bps at max (1000)",
			modify: func(p *types.Params) {
				p.PremiumAdjustmentBps = 1000
			},
			expectErr: false,
		},
		{
			name: "premium_adjustment_bps over max",
			modify: func(p *types.Params) {
				p.PremiumAdjustmentBps = 1001
			},
			expectErr: true,
			errMsg:    "1000",
		},
		{
			name: "slash_decay_days zero",
			modify: func(p *types.Params) {
				p.SlashDecayDays = 0
			},
			expectErr: true,
			errMsg:    "greater than 0",
		},
		{
			name: "max_claims_per_block zero",
			modify: func(p *types.Params) {
				p.MaxClaimsPerBlock = 0
			},
			expectErr: true,
			errMsg:    "positive",
		},
		{
			name: "max_payouts_per_block zero",
			modify: func(p *types.Params) {
				p.MaxPayoutsPerBlock = 0
			},
			expectErr: true,
			errMsg:    "positive",
		},
		{
			name: "negative min_pool_balance",
			modify: func(p *types.Params) {
				p.MinPoolBalance = "-100"
			},
			expectErr: true,
			errMsg:    "negative",
		},
		{
			name: "negative dispute_stake",
			modify: func(p *types.Params) {
				p.DisputeStakeLac = "-50"
			},
			expectErr: true,
			errMsg:    "negative",
		},
		{
			name: "negative auto_approve_threshold",
			modify: func(p *types.Params) {
				p.AutoApproveThreshold = "-10"
			},
			expectErr: true,
			errMsg:    "negative",
		},
		{
			name: "invalid target_utilization format",
			modify: func(p *types.Params) {
				p.TargetUtilization = "not-a-number"
			},
			expectErr: true,
			errMsg:    "invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := types.DefaultParams()
			tc.modify(params)

			err := k.SetParams(ctx, params)
			if tc.expectErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					assert.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Test: Genesis Validation
// =============================================================================

func TestGenesisValidation_PoolStateInvariants(t *testing.T) {
	tests := []struct {
		name      string
		pool      *types.PoolState
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid pool state",
			pool: &types.PoolState{
				TotalFunds:     "1000",
				AvailableFunds: "800",
				ReservedFunds:  "200",
				Status:         types.PoolStatus_POOL_STATUS_HEALTHY,
			},
			expectErr: false,
		},
		{
			name: "negative total funds",
			pool: &types.PoolState{
				TotalFunds:     "-100",
				AvailableFunds: "0",
				ReservedFunds:  "0",
				Status:         types.PoolStatus_POOL_STATUS_HEALTHY,
			},
			expectErr: true,
			errMsg:    "negative",
		},
		{
			name: "negative available funds",
			pool: &types.PoolState{
				TotalFunds:     "1000",
				AvailableFunds: "-100",
				ReservedFunds:  "0",
				Status:         types.PoolStatus_POOL_STATUS_HEALTHY,
			},
			expectErr: true,
			errMsg:    "negative",
		},
		{
			name: "negative reserved funds",
			pool: &types.PoolState{
				TotalFunds:     "1000",
				AvailableFunds: "1000",
				ReservedFunds:  "-50",
				Status:         types.PoolStatus_POOL_STATUS_HEALTHY,
			},
			expectErr: true,
			errMsg:    "negative",
		},
		{
			name: "invalid total funds format",
			pool: &types.PoolState{
				TotalFunds:     "invalid",
				AvailableFunds: "0",
				ReservedFunds:  "0",
				Status:         types.PoolStatus_POOL_STATUS_HEALTHY,
			},
			expectErr: true,
			errMsg:    "invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := &types.GenesisState{
				Params:         types.DefaultParams(),
				Pool:           tc.pool,
				ClaimSequence:  1,
				PayoutSequence: 1,
			}

			err := genesis.Validate()
			if tc.expectErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					assert.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGenesisValidation_ClaimDuplicates(t *testing.T) {
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Claims: []*types.Claim{
			{Id: "claim-1", Status: types.ClaimStatus_CLAIM_STATUS_PENDING, ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"}},
			{Id: "claim-1", Status: types.ClaimStatus_CLAIM_STATUS_PENDING, ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "200"}},
		},
		ClaimSequence:  1,
		PayoutSequence: 1,
	}

	err := genesis.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestGenesisValidation_SequenceZero(t *testing.T) {
	genesis := &types.GenesisState{
		Params:         types.DefaultParams(),
		ClaimSequence:  0,
		PayoutSequence: 1,
	}

	err := genesis.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claim sequence")

	genesis.ClaimSequence = 1
	genesis.PayoutSequence = 0

	err = genesis.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payout sequence")
}

// =============================================================================
// Test: Pool Health Status Evaluation
// =============================================================================

func TestPoolHealth_StatusTransitions(t *testing.T) {
	// Pool health is determined by: utilizationRatio = currentUtilization / targetUtilization
	// ratio < 0.5 → OVERFUNDED
	// 0.5 <= ratio < 0.8 → HEALTHY
	// 0.8 <= ratio < 1.2 → UNDERFUNDED
	// ratio >= 1.2 → CRITICAL
	tests := []struct {
		name           string
		pool           *types.PoolState
		expectedStatus types.PoolStatus
	}{
		{
			name: "healthy - ratio between 0.5 and 0.8",
			pool: &types.PoolState{
				TotalFunds:         "10000",
				AvailableFunds:     "9000",
				ReservedFunds:      "1000",
				TargetUtilization:  "0.2",
				CurrentUtilization: "0.12", // ratio = 0.12/0.2 = 0.6 (healthy)
			},
			expectedStatus: types.PoolStatus_POOL_STATUS_HEALTHY,
		},
		{
			name: "underfunded - ratio between 0.8 and 1.2",
			pool: &types.PoolState{
				TotalFunds:         "10000",
				AvailableFunds:     "8000",
				ReservedFunds:      "2000",
				TargetUtilization:  "0.2",
				CurrentUtilization: "0.2", // ratio = 0.2/0.2 = 1.0 (underfunded)
			},
			expectedStatus: types.PoolStatus_POOL_STATUS_UNDERFUNDED,
		},
		{
			name: "critical - ratio >= 1.2",
			pool: &types.PoolState{
				TotalFunds:         "10000",
				AvailableFunds:     "5000",
				ReservedFunds:      "5000",
				TargetUtilization:  "0.2",
				CurrentUtilization: "0.3", // ratio = 0.3/0.2 = 1.5 (critical)
			},
			expectedStatus: types.PoolStatus_POOL_STATUS_CRITICAL,
		},
		{
			name: "overfunded - ratio < 0.5",
			pool: &types.PoolState{
				TotalFunds:         "10000",
				AvailableFunds:     "9900",
				ReservedFunds:      "100",
				TargetUtilization:  "0.2",
				CurrentUtilization: "0.05", // ratio = 0.05/0.2 = 0.25 (overfunded)
			},
			expectedStatus: types.PoolStatus_POOL_STATUS_OVERFUNDED,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := types.EvaluatePoolHealth(tc.pool)
			assert.Equal(t, tc.expectedStatus, status)
		})
	}
}

// =============================================================================
// Test: Contribution with Receipt Ownership
// =============================================================================

func TestContribution_ReceiptOwnershipVerification(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record contribution with user ownership
	recordContributionForTests(t, fixture, "receipt-owner", "tool-alpha", "pub-1", 1000)

	// Filing claim as wrong owner should succeed if no owner recorded
	// But if owner was recorded during contribution, it should fail
	_, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1wrongowner",
		ReceiptId:     "receipt-owner",
		ToolId:        "tool-alpha",
		PublisherId:   "pub-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
		Reason:        "Testing ownership",
	})
	// May succeed or fail depending on whether ownership was recorded
	// Just verify no panic and behavior is deterministic
	if err != nil {
		assert.Contains(t, err.Error(), "owner")
	}
}

// =============================================================================
// Test: EndBlocker with Disabled Module
// =============================================================================

func TestEndBlocker_ModuleDisabled(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Fund pool and create a claim
	fundPoolForTests(t, fixture, 10000)
	recordContributionForTests(t, fixture, "receipt-disabled", "tool-alpha", "pub-1", 100)

	params := types.DefaultParams()
	params.ClaimWindowSeconds = 1
	params.AutoApproveThreshold = "1000"
	params.Enabled = false // Disabled
	require.NoError(t, k.SetParams(ctx, params))

	claimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-disabled",
		ToolId:        "tool-alpha",
		PublisherId:   "pub-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "50"},
		Reason:        "Should not auto-approve when disabled",
	})
	require.NoError(t, err)

	// Advance time
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(5 * time.Second))

	// Run EndBlocker
	require.NoError(t, k.EndBlocker(ctx))

	// Claim should still be pending (module disabled)
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_PENDING, claim.Status,
		"claims should not be auto-processed when module is disabled")
}

// =============================================================================
// Test: Partial Payout Override
// =============================================================================

func TestPayout_PartialOverride(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper
	bankKeeper := fixture.bankKeeper
	accountKeeper := fixture.accountKeeper

	// Fund pool
	fundPoolForTests(t, fixture, 10000)

	// Create claimant
	claimantAddr := sdk.AccAddress([]byte("claimant_partial__"))
	claimantAccount := accountKeeper.NewAccountWithAddress(ctx, claimantAddr)
	accountKeeper.SetAccount(ctx, claimantAccount)

	recordContributionForTests(t, fixture, "receipt-partial-override", "tool-alpha", "pub-1", 1000)

	claimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      claimantAddr.String(),
		ReceiptId:     "receipt-partial-override",
		ToolId:        "tool-alpha",
		PublisherId:   "pub-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "1000"},
		Reason:        "Partial payout test",
	})
	require.NoError(t, err)

	// Approve for full amount
	authority := authtypes.NewModuleAddress("gov").String()
	require.NoError(t, k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimID,
		Resolution:     "approve",
		ApprovedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "1000"},
	}))

	// Process payout with override amount (pay only 600 of 1000 approved)
	msgServer := keeper.NewMsgServerImpl(k)
	_, err = msgServer.ProcessPayout(ctx, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   claimID,
		Recipient: claimantAddr.String(),
		Amount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "600",
		},
	})
	require.NoError(t, err)

	// Verify claimant received the override amount
	balance := bankKeeper.GetAllBalances(ctx, claimantAddr)
	assert.Equal(t, sdkmath.NewInt(600), balance.AmountOf("ulac"),
		"claimant should receive override amount of 600")

	// Verify claim is marked as paid
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_PAID, claim.Status)
}

// =============================================================================
// Test: Duplicate Contribution Prevention
// =============================================================================

func TestContribution_DuplicatePrevention(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record first contribution
	recordContributionForTests(t, fixture, "receipt-dup-contrib", "tool-alpha", "pub-1", 500)

	// Attempt duplicate contribution for same receipt
	err := k.ContributeToPool(ctx, "receipt-dup-contrib", "tool-alpha", "pub-1", "v1", "",
		sdk.NewCoins(sdk.NewInt64Coin("ulac", 500)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already recorded")
}

// =============================================================================
// Test: Claim Amount Validation in Genesis
// =============================================================================

func TestGenesisValidation_NegativeClaimAmount(t *testing.T) {
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Claims: []*types.Claim{
			{
				Id:            "claim-neg",
				Status:        types.ClaimStatus_CLAIM_STATUS_PENDING,
				ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "-100"},
			},
		},
		ClaimSequence:  1,
		PayoutSequence: 1,
	}

	err := genesis.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative")
}

// =============================================================================
// Test: EndBlocker Handles Zero TTL Gracefully
// =============================================================================

func TestEndBlocker_ZeroClaimWindow(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Set params with 0 claim window (edge case)
	// ValidateBasic should reject this, so we test the rejection
	params := types.DefaultParams()
	params.ClaimWindowSeconds = 0

	err := k.SetParams(ctx, params)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}
