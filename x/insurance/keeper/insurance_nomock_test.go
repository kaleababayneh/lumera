
package keeper_test

import (
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// =============================================================================
// Test: Claim Lifecycle - FileClaim
// =============================================================================

func TestFileClaim_Success(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record an insurance contribution for this receipt so claim can be filed
	recordContributionForTests(t, fixture, "receipt-001", "tool-alpha", "publisher-001", 1000)

	msg := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant123",
		ReceiptId:   "receipt-001",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
		Reason: "SLO violation - response time exceeded threshold",
		Evidence: []*types.Evidence{
			{
				Type:        "log",
				Hash:        "abc123hash",
				Uri:         "ipfs://evidence1",
				Description: "Server logs showing timeout",
			},
		},
	}

	claimID, err := k.FileClaim(ctx, msg)
	require.NoError(t, err)
	require.NotEmpty(t, claimID)
	assert.Equal(t, "claim-1", claimID) // Sequences start at 1 (DefaultGenesis.ClaimSequence)

	// Verify claim was stored
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, claimID, claim.Id)
	assert.Equal(t, msg.ReceiptId, claim.ReceiptId)
	assert.Equal(t, msg.Claimant, claim.ClaimantId)
	assert.Equal(t, msg.ToolId, claim.ToolId)
	assert.Equal(t, msg.PublisherId, claim.PublisherId)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_PENDING, claim.Status)
	assert.Equal(t, msg.Reason, claim.Reason)

	// Verify events emitted
	events := ctx.EventManager().Events()
	found := false
	for _, e := range events {
		if e.Type == types.EventTypeClaimFiled {
			found = true
			break
		}
	}
	assert.True(t, found, "claim_filed event should be emitted")
}

func TestFileClaim_DuplicateClaim(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record contribution for the receipt
	recordContributionForTests(t, fixture, "receipt-dup", "tool-alpha", "publisher-001", 1000)

	msg := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant123",
		ReceiptId:   "receipt-dup",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "50",
		},
		Reason: "First claim",
	}

	// First claim should succeed
	claimID1, err := k.FileClaim(ctx, msg)
	require.NoError(t, err)
	require.NotEmpty(t, claimID1)

	// Second claim for same receipt should fail
	msg2 := &types.MsgFileClaim{
		Claimant:    "cosmos1different",
		ReceiptId:   "receipt-dup", // Same receipt
		ToolId:      "tool-beta",
		PublisherId: "publisher-002",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "75",
		},
		Reason: "Different claim, same receipt",
	}

	_, err = k.FileClaim(ctx, msg2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claim already exists")
}

func TestFileClaim_MultipleUniqueReceipts(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// File claims for different receipts
	receipts := []string{"receipt-a", "receipt-b", "receipt-c"}
	claimIDs := make([]string, 0, len(receipts))

	// Record contributions for all receipts first
	for _, receiptID := range receipts {
		recordContributionForTests(t, fixture, receiptID, "tool-alpha", "publisher-001", 1000)
	}

	for i, receiptID := range receipts {
		msg := &types.MsgFileClaim{
			Claimant:    "cosmos1claimant",
			ReceiptId:   receiptID,
			ToolId:      "tool-alpha",
			PublisherId: "publisher-001",
			ClaimedAmount: &basev1beta1.Coin{
				Denom:  "ulac",
				Amount: "100",
			},
			Reason: "Test claim",
		}

		claimID, err := k.FileClaim(ctx, msg)
		require.NoError(t, err, "claim %d should succeed", i)
		claimIDs = append(claimIDs, claimID)
	}

	// Verify all claims have unique sequential IDs (starting at 1)
	assert.Equal(t, "claim-1", claimIDs[0])
	assert.Equal(t, "claim-2", claimIDs[1])
	assert.Equal(t, "claim-3", claimIDs[2])
}

// =============================================================================
// Test: Claim Lifecycle - ProcessClaim
// =============================================================================

func TestProcessClaim_Approve(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Setup: Fund pool so reserve can work
	fundPoolForTests(t, fixture, 10000)

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-approve-test", "tool-alpha", "publisher-001", 1000)

	// File a claim first
	fileClaim := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-approve-test",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "500",
		},
		Reason: "SLO violation",
	}

	claimID, err := k.FileClaim(ctx, fileClaim)
	require.NoError(t, err)

	// Approve the claim
	authority := authtypes.NewModuleAddress("gov").String()
	processMsg := &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "500",
		},
		Notes: "Claim validated and approved",
	}

	err = k.ProcessClaim(ctx, processMsg)
	require.NoError(t, err)

	// Verify claim status updated
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, claim.Status)
	assert.NotNil(t, claim.ApprovedAmount)
	assert.Equal(t, "500", claim.ApprovedAmount.Amount)
	assert.NotNil(t, claim.ResolvedAt)
}

func TestProcessClaim_Reject(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-reject-test", "tool-alpha", "publisher-001", 1000)

	// File a claim
	fileClaim := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-reject-test",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "1000",
		},
		Reason: "False claim",
	}

	claimID, err := k.FileClaim(ctx, fileClaim)
	require.NoError(t, err)

	// Reject the claim
	authority := authtypes.NewModuleAddress("gov").String()
	processMsg := &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "reject",
		Notes:      "Insufficient evidence",
	}

	err = k.ProcessClaim(ctx, processMsg)
	require.NoError(t, err)

	// Verify claim status
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_REJECTED, claim.Status)
	assert.Nil(t, claim.ApprovedAmount)
}

func TestProcessClaim_PartialApproval(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Setup: Fund pool
	fundPoolForTests(t, fixture, 10000)

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-partial-test", "tool-alpha", "publisher-001", 1000)

	// File a claim for 1000
	fileClaim := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-partial-test",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "1000",
		},
		Reason: "Partial SLO violation",
	}

	claimID, err := k.FileClaim(ctx, fileClaim)
	require.NoError(t, err)

	// Approve only 300 (partial)
	authority := authtypes.NewModuleAddress("gov").String()
	processMsg := &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "partial",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "300",
		},
		Notes: "Partial approval - only 30% of claim validated",
	}

	err = k.ProcessClaim(ctx, processMsg)
	require.NoError(t, err)

	// Verify claim approved with partial amount
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, claim.Status)
	assert.Equal(t, "300", claim.ApprovedAmount.Amount)
}

func TestProcessClaim_AlreadyProcessed(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-double-process", "tool-alpha", "publisher-001", 100)

	// File and approve a claim
	fileClaim := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-double-process",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
		Reason: "Test",
	}

	claimID, err := k.FileClaim(ctx, fileClaim)
	require.NoError(t, err)

	authority := authtypes.NewModuleAddress("gov").String()
	processMsg := &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
	}

	// First process should succeed
	err = k.ProcessClaim(ctx, processMsg)
	require.NoError(t, err)

	// Second process should fail
	processMsg.Notes = "Trying again"
	err = k.ProcessClaim(ctx, processMsg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already processed")
}

func TestProcessClaim_NotFound(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	authority := authtypes.NewModuleAddress("gov").String()
	processMsg := &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    "nonexistent-claim",
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
	}

	err := k.ProcessClaim(ctx, processMsg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestProcessClaim_Unauthorized(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-unauth", "tool-alpha", "publisher-001", 100)

	// File a claim
	fileClaim := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-unauth",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
		Reason: "Test",
	}

	claimID, err := k.FileClaim(ctx, fileClaim)
	require.NoError(t, err)

	// Try to process with wrong authority
	processMsg := &types.MsgProcessClaim{
		Authority:  "cosmos1notauthorized",
		ClaimId:    claimID,
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
	}

	err = k.ProcessClaim(ctx, processMsg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
}

func TestProcessClaim_ApproveWithoutAmount(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-no-amount", "tool-alpha", "publisher-001", 100)

	// File a claim
	fileClaim := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-no-amount",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
		Reason: "Test",
	}

	claimID, err := k.FileClaim(ctx, fileClaim)
	require.NoError(t, err)

	authority := authtypes.NewModuleAddress("gov").String()
	processMsg := &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimID,
		Resolution:     "approve",
		ApprovedAmount: nil, // Missing amount
	}

	err = k.ProcessClaim(ctx, processMsg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "approved_amount is required")
}

func TestProcessClaim_InvalidResolution(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-invalid-res", "tool-alpha", "publisher-001", 100)

	// File a claim
	fileClaim := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-invalid-res",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
		Reason: "Test",
	}

	claimID, err := k.FileClaim(ctx, fileClaim)
	require.NoError(t, err)

	authority := authtypes.NewModuleAddress("gov").String()
	processMsg := &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "invalid_resolution",
	}

	err = k.ProcessClaim(ctx, processMsg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown resolution")
}

// =============================================================================
// Test: GetClaim and GetClaimsByStatus
// =============================================================================

func TestGetClaim_NotFound(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	_, err := k.GetClaim(ctx, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetClaimsByStatus(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)

	// Record contributions and file 3 claims
	for i := 1; i <= 3; i++ {
		receiptID := "receipt-status-" + string(rune('a'+i-1))
		recordContributionForTests(t, fixture, receiptID, "tool-alpha", "publisher-001", 100)

		msg := &types.MsgFileClaim{
			Claimant:    "cosmos1claimant",
			ReceiptId:   receiptID,
			ToolId:      "tool-alpha",
			PublisherId: "publisher-001",
			ClaimedAmount: &basev1beta1.Coin{
				Denom:  "ulac",
				Amount: "100",
			},
			Reason: "Test",
		}
		_, err := k.FileClaim(ctx, msg)
		require.NoError(t, err)
	}

	// All should be pending
	pendingClaims, err := k.GetClaimsByStatus(ctx, types.ClaimStatus_CLAIM_STATUS_PENDING)
	require.NoError(t, err)
	assert.Len(t, pendingClaims, 3)

	// Approve one
	authority := authtypes.NewModuleAddress("gov").String()
	err = k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    "claim-1",
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
	})
	require.NoError(t, err)

	// Now 2 pending, 1 approved
	pendingClaims, err = k.GetClaimsByStatus(ctx, types.ClaimStatus_CLAIM_STATUS_PENDING)
	require.NoError(t, err)
	assert.Len(t, pendingClaims, 2)

	approvedClaims, err := k.GetClaimsByStatus(ctx, types.ClaimStatus_CLAIM_STATUS_APPROVED)
	require.NoError(t, err)
	assert.Len(t, approvedClaims, 1)
}

func TestGetClaimsByReceipt(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-lookup", "tool-alpha", "publisher-001", 100)

	// File a claim
	msg := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-lookup",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
		Reason: "Test",
	}

	_, err := k.FileClaim(ctx, msg)
	require.NoError(t, err)

	// Should find the claim
	claims, err := k.GetClaimsByReceipt(ctx, "receipt-lookup")
	require.NoError(t, err)
	assert.Len(t, claims, 1)
	assert.Equal(t, "receipt-lookup", claims[0].ReceiptId)

	// Should not find claims for unknown receipt - function may return error or empty
	claims, err = k.GetClaimsByReceipt(ctx, "unknown-receipt")
	// The implementation currently returns error for not-found cases
	// This is acceptable behavior - we just verify no panic and return is handled
	if err != nil {
		assert.Contains(t, err.Error(), "not found")
	} else {
		assert.Len(t, claims, 0)
	}
}

// =============================================================================
// Test: Pool Fund Accounting
// =============================================================================

func TestPoolFundAccounting_ContributeToPool(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Ensure both module accounts exist (credits and insurance)
	creditsAccount := fixture.accountKeeper.GetModuleAccount(ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	insuranceAccount := fixture.accountKeeper.GetModuleAccount(ctx, types.ModuleAccountName)
	require.NotNil(t, insuranceAccount)

	// Fund credits module
	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 5000))
	require.NoError(t, fixture.bankKeeper.MintCoins(ctx, creditstypes.ModuleName, initialCoins))

	// Contribute to pool
	contribution := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1000))
	err := k.ContributeToPool(ctx, "receipt-contrib", "tool-1", "publisher-1", "v1", "", contribution)
	require.NoError(t, err)

	// Verify pool balance
	balance, err := k.GetPoolBalance(ctx)
	require.NoError(t, err)
	assert.True(t, balance.AmountOf("ulac").Equal(sdkmath.NewInt(1000)))
}

func TestPoolFundAccounting_ReserveOnApproval(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Fund pool with 10000
	fundPoolForTests(t, fixture, 10000)

	// Record contribution so claim can be filed (adds 500 to pool)
	recordContributionForTests(t, fixture, "receipt-reserve-test", "tool-alpha", "publisher-001", 500)

	// File and approve a claim for 500
	fileClaim := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-reserve-test",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "500",
		},
		Reason: "Test",
	}

	claimID, err := k.FileClaim(ctx, fileClaim)
	require.NoError(t, err)

	authority := authtypes.NewModuleAddress("gov").String()
	err = k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "500",
		},
	})
	require.NoError(t, err)

	// Pool balance should be 10000 + 500 (contribution) = 10500 (in module account)
	// The internal accounting should show 500 reserved
	balance, err := k.GetPoolBalance(ctx)
	require.NoError(t, err)
	assert.True(t, balance.AmountOf("ulac").Equal(sdkmath.NewInt(10500)))
}

func TestPoolFundAccounting_InsufficientFundsForReserve(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Fund pool with only 100
	fundPoolForTests(t, fixture, 100)

	// Record a minimal contribution so claim can be filed (just to satisfy the precondition)
	// Use 1 ulac so total pool is 100 + 1 = 101, still less than 500
	recordContributionForTests(t, fixture, "receipt-insufficient", "tool-alpha", "publisher-001", 1)

	// File a claim for 500
	fileClaim := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-insufficient",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "500",
		},
		Reason: "Test",
	}

	claimID, err := k.FileClaim(ctx, fileClaim)
	require.NoError(t, err)

	// Try to approve - should fail due to insufficient funds
	// Pool has 101 ulac but we're trying to approve 500
	authority := authtypes.NewModuleAddress("gov").String()
	err = k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "500",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient")
}

// =============================================================================
// Test: Pending Claims Counter
// =============================================================================

func TestPendingClaimsCounter(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)

	// Record contributions and file 3 claims
	for i := 1; i <= 3; i++ {
		receiptID := "receipt-counter-" + string(rune('a'+i-1))
		recordContributionForTests(t, fixture, receiptID, "tool-alpha", "publisher-001", 100)

		msg := &types.MsgFileClaim{
			Claimant:    "cosmos1claimant",
			ReceiptId:   receiptID,
			ToolId:      "tool-alpha",
			PublisherId: "publisher-001",
			ClaimedAmount: &basev1beta1.Coin{
				Denom:  "ulac",
				Amount: "100",
			},
			Reason: "Test",
		}
		_, err := k.FileClaim(ctx, msg)
		require.NoError(t, err)
	}

	// Verify pending count via GetClaimsByStatus
	pending, err := k.GetClaimsByStatus(ctx, types.ClaimStatus_CLAIM_STATUS_PENDING)
	require.NoError(t, err)
	assert.Len(t, pending, 3)

	// Approve one claim
	authority := authtypes.NewModuleAddress("gov").String()
	err = k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    "claim-1",
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
	})
	require.NoError(t, err)

	// Reject one claim
	err = k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    "claim-2",
		Resolution: "reject",
		Notes:      "Rejected",
	})
	require.NoError(t, err)

	// Should have 1 pending now
	pending, err = k.GetClaimsByStatus(ctx, types.ClaimStatus_CLAIM_STATUS_PENDING)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
}

// =============================================================================
// Test: Parameters
// =============================================================================

func TestGetSetParams(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Get default params
	params := k.GetParams(ctx)
	require.NotNil(t, params)
	// InsurancePoolBps may be int32 or uint32 depending on proto version
	assert.EqualValues(t, 300, params.InsurancePoolBps) // 3%

	// Update params
	newParams := types.DefaultParams()
	newParams.InsurancePoolBps = 500 // 5%
	newParams.MaxClaimPercent = "0.2"

	err := k.SetParams(ctx, newParams)
	require.NoError(t, err)

	// Verify updated
	retrieved := k.GetParams(ctx)
	assert.EqualValues(t, 500, retrieved.InsurancePoolBps)
	assert.Equal(t, "0.2", retrieved.MaxClaimPercent)
}

func TestSetParams_NilParams(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	err := k.SetParams(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be nil")
}

// =============================================================================
// Test: Genesis
// =============================================================================

func TestGenesisExportImport(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Modify some state
	newParams := types.DefaultParams()
	newParams.InsurancePoolBps = 400
	err := k.SetParams(ctx, newParams)
	require.NoError(t, err)

	// Export genesis
	genesis := k.ExportGenesis(ctx)
	require.NotNil(t, genesis)
	require.NotNil(t, genesis.Params)
	assert.EqualValues(t, 400, genesis.Params.InsurancePoolBps)

	// Create new fixture and import genesis
	fixture2 := setupKeeperTest(t)
	k2 := fixture2.keeper
	ctx2 := fixture2.ctx

	k2.InitGenesis(ctx2, genesis)

	// Verify imported params
	importedParams := k2.GetParams(ctx2)
	assert.EqualValues(t, 400, importedParams.InsurancePoolBps)
}

// =============================================================================
// Test: EndBlocker Auto-Approval
// =============================================================================

func TestEndBlocker_AutoApprovesExpiredClaims(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-auto-approve", "tool-alpha", "publisher-001", 5)

	// Set params with short claim window
	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60     // 1 minute
	params.AutoApproveThreshold = "10" // Auto-approve claims <= 10 ulac
	params.Enabled = true
	err := k.SetParams(ctx, params)
	require.NoError(t, err)

	// File a small claim (under auto-approve threshold)
	msg := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-auto-approve",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "5", // Under threshold
		},
		Reason: "Small claim for auto-approval test",
	}

	claimID, err := k.FileClaim(ctx, msg)
	require.NoError(t, err)

	// Advance time past claim window
	newTime := ctx.BlockTime().Add(2 * time.Minute)
	ctx = ctx.WithBlockTime(newTime)

	// Run EndBlocker
	err = k.EndBlocker(ctx)
	require.NoError(t, err)

	// Claim should be auto-approved
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, claim.Status)
}

func TestEndBlocker_LargeClaimsRequireReview(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-large-claim", "tool-alpha", "publisher-001", 500)

	// Set params
	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60
	params.AutoApproveThreshold = "10"
	params.Enabled = true
	err := k.SetParams(ctx, params)
	require.NoError(t, err)

	// File a large claim (over auto-approve threshold)
	msg := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-large-claim",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "500", // Over threshold
		},
		Reason: "Large claim requiring manual review",
	}

	claimID, err := k.FileClaim(ctx, msg)
	require.NoError(t, err)

	// Advance time past claim window
	newTime := ctx.BlockTime().Add(2 * time.Minute)
	ctx = ctx.WithBlockTime(newTime)

	// Run EndBlocker
	err = k.EndBlocker(ctx)
	require.NoError(t, err)

	// Claim should move to EXPIRED so it no longer consumes future EndBlocker slots.
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_EXPIRED, claim.Status)

	// Should have emitted review_required event
	events := ctx.EventManager().Events()
	foundReviewEvent := false
	for _, e := range events {
		if e.Type == "insurance_claim_review_expired" {
			foundReviewEvent = true
			break
		}
	}
	assert.True(t, foundReviewEvent, "should emit review_expired event for large claims")
}

func TestEndBlocker_ExpiredReviewClaimsDoNotStarveLaterClaims(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)

	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60
	params.AutoApproveThreshold = "10"
	params.MaxClaimsPerBlock = 1
	params.Enabled = true
	require.NoError(t, k.SetParams(ctx, params))

	recordContributionForTests(t, fixture, "receipt-review-first", "tool-alpha", "publisher-001", 500)
	recordContributionForTests(t, fixture, "receipt-small-second", "tool-alpha", "publisher-001", 5)

	firstClaimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-review-first",
		ToolId:        "tool-alpha",
		PublisherId:   "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
		Reason:        "manual review first",
	})
	require.NoError(t, err)

	secondClaimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-small-second",
		ToolId:        "tool-alpha",
		PublisherId:   "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "5"},
		Reason:        "small claim second",
	})
	require.NoError(t, err)

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))
	require.NoError(t, k.EndBlocker(ctx))

	firstClaim, err := k.GetClaim(ctx, firstClaimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_EXPIRED, firstClaim.Status)

	secondClaim, err := k.GetClaim(ctx, secondClaimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_PENDING, secondClaim.Status)

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Second))
	require.NoError(t, k.EndBlocker(ctx))

	secondClaim, err = k.GetClaim(ctx, secondClaimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, secondClaim.Status)
}

func TestProcessClaim_AllowsExpiredReviewClaims(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)
	recordContributionForTests(t, fixture, "receipt-expired-review", "tool-alpha", "publisher-001", 500)

	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60
	params.AutoApproveThreshold = "10"
	params.Enabled = true
	require.NoError(t, k.SetParams(ctx, params))

	claimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-expired-review",
		ToolId:        "tool-alpha",
		PublisherId:   "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
		Reason:        "requires manual review",
	})
	require.NoError(t, err)

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))
	require.NoError(t, k.EndBlocker(ctx))

	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	require.Equal(t, types.ClaimStatus_CLAIM_STATUS_EXPIRED, claim.Status)

	authority := authtypes.NewModuleAddress("gov").String()
	require.NoError(t, k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimID,
		Resolution:     "approve",
		ApprovedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
		Notes:          "approved after review window expiry",
	}))

	claim, err = k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, claim.Status)
}

func TestEndBlocker_MaxClaimsPerBlockLimit(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 100000)

	// Set params with small max claims per block
	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60
	params.AutoApproveThreshold = "100"
	params.MaxClaimsPerBlock = 2 // Only process 2 per block
	params.Enabled = true
	err := k.SetParams(ctx, params)
	require.NoError(t, err)

	// Record contributions and file 5 small claims
	for i := 1; i <= 5; i++ {
		receiptID := "receipt-batch-" + string(rune('a'+i-1))
		recordContributionForTests(t, fixture, receiptID, "tool-alpha", "publisher-001", 10)

		msg := &types.MsgFileClaim{
			Claimant:    "cosmos1claimant",
			ReceiptId:   receiptID,
			ToolId:      "tool-alpha",
			PublisherId: "publisher-001",
			ClaimedAmount: &basev1beta1.Coin{
				Denom:  "ulac",
				Amount: "10",
			},
			Reason: "Batch test",
		}
		_, err := k.FileClaim(ctx, msg)
		require.NoError(t, err)
	}

	// Advance time
	newTime := ctx.BlockTime().Add(2 * time.Minute)
	ctx = ctx.WithBlockTime(newTime)

	// Run EndBlocker - should only process 2
	err = k.EndBlocker(ctx)
	require.NoError(t, err)

	// Check how many were approved
	approved, err := k.GetClaimsByStatus(ctx, types.ClaimStatus_CLAIM_STATUS_APPROVED)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(approved), 2, "should not process more than MaxClaimsPerBlock")

	pending, err := k.GetClaimsByStatus(ctx, types.ClaimStatus_CLAIM_STATUS_PENDING)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(pending), 3, "remaining claims should still be pending")
}

// =============================================================================
// Test: Event Emission
// =============================================================================

func TestEventEmission_ClaimFiled(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-event-test", "tool-alpha", "publisher-001", 100)

	msg := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-event-test",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
		Reason: "Event test",
	}

	_, err := k.FileClaim(ctx, msg)
	require.NoError(t, err)

	events := ctx.EventManager().Events()
	var claimFiledEvent sdk.Event
	for _, e := range events {
		if e.Type == types.EventTypeClaimFiled {
			claimFiledEvent = e
			break
		}
	}

	require.NotEmpty(t, claimFiledEvent.Type)
	assert.Equal(t, types.EventTypeClaimFiled, claimFiledEvent.Type)

	// Verify attributes
	attrs := make(map[string]string)
	for _, attr := range claimFiledEvent.Attributes {
		attrs[attr.Key] = attr.Value
	}
	assert.Equal(t, "claim-1", attrs[types.AttributeKeyClaimID])
	assert.Equal(t, "receipt-event-test", attrs[types.AttributeKeyReceiptID])
}

func TestEventEmission_ClaimProcessed(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)

	// Record contribution so claim can be filed
	recordContributionForTests(t, fixture, "receipt-processed-event", "tool-alpha", "publisher-001", 100)

	// File a claim
	msg := &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   "receipt-processed-event",
		ToolId:      "tool-alpha",
		PublisherId: "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
		Reason: "Process event test",
	}

	claimID, err := k.FileClaim(ctx, msg)
	require.NoError(t, err)

	// Clear events from filing
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	// Approve the claim
	authority := authtypes.NewModuleAddress("gov").String()
	err = k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    claimID,
		Resolution: "approve",
		ApprovedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "100",
		},
	})
	require.NoError(t, err)

	events := ctx.EventManager().Events()
	var processedEvent sdk.Event
	for _, e := range events {
		if e.Type == types.EventTypeClaimProcessed {
			processedEvent = e
			break
		}
	}

	require.NotEmpty(t, processedEvent.Type)

	attrs := make(map[string]string)
	for _, attr := range processedEvent.Attributes {
		attrs[attr.Key] = attr.Value
	}
	assert.Equal(t, claimID, attrs[types.AttributeKeyClaimID])
	assert.Equal(t, "approve", attrs[types.AttributeKeyResolution])
	assert.Equal(t, "CLAIM_STATUS_APPROVED", attrs[types.AttributeKeyStatus])
}

// =============================================================================
// Test: Nil and Empty Input Handling
// =============================================================================

func TestProcessClaim_NilMessage(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	err := k.ProcessClaim(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be nil")
}

func TestProcessClaim_EmptyClaimID(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	authority := authtypes.NewModuleAddress("gov").String()
	err := k.ProcessClaim(ctx, &types.MsgProcessClaim{
		Authority:  authority,
		ClaimId:    "",
		Resolution: "approve",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claim_id is required")
}

// =============================================================================
// Helper Functions
// =============================================================================

// fundPoolForTests funds both the credits module and transfers to insurance pool,
// and properly initializes the pool state with AvailableFunds by re-initializing genesis
func fundPoolForTests(t *testing.T, fixture *keeperFixture, amount int64) {
	t.Helper()

	amountStr := sdkmath.NewInt(amount).String()

	// Ensure module accounts exist
	creditsAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	insuranceAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, types.ModuleAccountName)
	require.NotNil(t, insuranceAccount)

	// Mint coins to credits module
	coins := sdk.NewCoins(sdk.NewInt64Coin("ulac", amount))
	require.NoError(t, fixture.bankKeeper.MintCoins(fixture.ctx, creditstypes.ModuleName, coins))

	// Transfer to insurance module
	err := fixture.keeper.ContributeToPool(fixture.ctx, "test-funding-receipt-"+amountStr, "tool-1", "publisher-1", "v1", "", coins)
	require.NoError(t, err)

	// Re-initialize genesis with the pool state that reflects the contribution
	// This simulates what would happen in a real system where the pool state tracks funds
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Pool: &types.PoolState{
			TotalFunds:         amountStr,
			AvailableFunds:     amountStr,
			ReservedFunds:      "0",
			TotalContributions: amountStr,
			TotalPayouts:       "0",
			TargetUtilization:  "0.2",
			CurrentUtilization: "0",
			Status:             types.PoolStatus_POOL_STATUS_HEALTHY,
		},
		Claims:         []*types.Claim{},
		Contributions:  []*types.Contribution{},
		PublisherRisks: []*types.PublisherRisk{},
		Payouts:        []*types.Payout{},
		Metrics:        types.DefaultPoolMetrics(),
	}
	fixture.keeper.InitGenesis(fixture.ctx, genesis)
}

// recordContributionForTests records an insurance contribution for a receipt
// so that claims can be filed against it. This must be called before FileClaim
// for any receipt that needs insurance coverage.
func recordContributionForTests(t *testing.T, fixture *keeperFixture, receiptID, toolID, publisherID string, amount int64) {
	t.Helper()

	// Ensure module accounts exist
	creditsAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	insuranceAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, types.ModuleAccountName)
	require.NotNil(t, insuranceAccount)

	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	authority := authtypes.NewModuleAddress("gov").String()

	// First, ensure the pool has funds from the credits module
	coins := sdk.NewCoins(sdk.NewInt64Coin("ulac", amount))
	require.NoError(t, fixture.bankKeeper.MintCoins(fixture.ctx, creditstypes.ModuleName, coins))

	// Process the contribution through the msg server
	_, err := msgServer.ProcessContribution(fixture.ctx, &types.MsgProcessContribution{
		Authority:     authority,
		ReceiptId:     receiptID,
		ToolId:        toolID,
		PublisherId:   publisherID,
		PolicyVersion: "v1",
		Amount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: sdkmath.NewInt(amount).String(),
		},
	})
	require.NoError(t, err, "failed to record contribution for receipt %s", receiptID)
}
