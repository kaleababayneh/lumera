
package keeper_test

import (
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// TestMicroTransactionInsuranceFraud_Reproduce demonstrates how an attacker can drain the pool
// by making tiny contributions and claiming the auto-approve threshold.
func TestMicroTransactionInsuranceFraud_Reproduce(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	// 1. Setup: Fund pool with "victim" funds (e.g. 1000 ulac)
	fundPoolForTests(t, fixture, 1000)

	// Verify initial pool state
	pool, err := k.GetPoolBalance(ctx)
	require.NoError(t, err)
	initialBalance := pool.AmountOf("ulac")
	require.Equal(t, int64(1000), initialBalance.Int64())

	// 2. Setup params with AutoApproveThreshold > Micro Contribution
	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60
	params.AutoApproveThreshold = "10" // Auto-approve up to 10 ulac
	params.Enabled = true
	err = k.SetParams(ctx, params)
	require.NoError(t, err)

	// 3. Attack: Make a micro-contribution (e.g. 1 ulac)
	// This simulates settling a tiny transaction where the insurance fee is 1 (or rounded down to 1)
	microReceiptID := "receipt-micro-attack"
	recordContributionForTests(t, fixture, microReceiptID, "tool-attack", "publisher-attack", 1)

	// 4. Attack: File a claim for the max auto-approve threshold (10 ulac)
	// Contribution: 1 ulac. Claim: 10 ulac. Profit: 9 ulac.
	msg := &types.MsgFileClaim{
		Claimant:    "cosmos1attacker",
		ReceiptId:   microReceiptID,
		ToolId:      "tool-attack",
		PublisherId: "publisher-attack",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "10",
		},
		Reason: "Auto-approve exploit",
	}

	claimID, err := k.FileClaim(ctx, msg)
	require.NoError(t, err)

	// 5. Advance time to trigger auto-approval in EndBlocker
	newTime := ctx.BlockTime().Add(2 * time.Minute)
	ctx = ctx.WithBlockTime(newTime)

	err = k.EndBlocker(ctx)
	require.NoError(t, err)

	// 6. Verify Claim Flagged for Review (Pending)
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_EXPIRED, claim.Status, "High leverage claim should move to expired review")

	// Verify event
	events := ctx.EventManager().Events()
	foundReview := false
	for _, e := range events {
		if e.Type == "insurance_claim_review_high_leverage" {
			foundReview = true
			break
		}
	}
	assert.True(t, foundReview, "Should emit high leverage review event")

	t.Logf("Attack mitigated: Claim %s flagged for review due to high leverage", claimID)
}

// TestProcessExpiredClaims_ZeroCoverageNoPanic verifies that a claim whose denom
// has no matching contribution doesn't panic on division by zero.
// Regression test for BUG-6: contribDec.IsZero() → Div panics.
func TestProcessExpiredClaims_ZeroCoverageNoPanic(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 1000)

	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60
	params.AutoApproveThreshold = "100"
	params.Enabled = true
	require.NoError(t, k.SetParams(ctx, params))

	receiptID := "receipt-denom-mismatch"
	recordContributionForTests(t, fixture, receiptID, "tool-1", "pub-1", 50)

	// File claim with mismatched denom — contribution is in "ulac",
	// but claim uses "ufoo". coverage.AmountOf("ufoo") returns zero.
	claimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   receiptID,
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ufoo",
			Amount: "5",
		},
		Reason: "denom mismatch test",
	})
	require.NoError(t, err)

	// Advance past review window
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	// Must not panic (previously panicked with shopspring/decimal division by zero)
	require.NotPanics(t, func() {
		err = k.EndBlocker(ctx)
	})
	require.NoError(t, err)

	// Claim should move to EXPIRED so it leaves the pending queue while awaiting manual review
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_EXPIRED, claim.Status,
		"claim with zero coverage denom should move to expired review")

	// Verify the new "no coverage" event was emitted
	found := false
	for _, e := range ctx.EventManager().Events() {
		if e.Type == "insurance_claim_review_no_coverage" {
			found = true
			break
		}
	}
	assert.True(t, found, "should emit insurance_claim_review_no_coverage event")
}

// TestProcessExpiredClaims_InvalidAmountSkipped verifies that a claim with an
// unparseable amount string is skipped rather than silently auto-approved.
// Regression test for BUG-7: dropped decimal.NewFromString error.
func TestProcessExpiredClaims_InvalidAmountSkipped(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 1000)

	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60
	params.AutoApproveThreshold = "100"
	params.Enabled = true
	require.NoError(t, k.SetParams(ctx, params))

	receiptID := "receipt-bad-amount"
	recordContributionForTests(t, fixture, receiptID, "tool-1", "pub-1", 50)

	// File claim with unparseable amount
	claimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:    "cosmos1claimant",
		ReceiptId:   receiptID,
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		ClaimedAmount: &basev1beta1.Coin{
			Denom:  "ulac",
			Amount: "not-a-number",
		},
		Reason: "invalid amount test",
	})
	require.NoError(t, err)

	// Advance past review window
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	err = k.EndBlocker(ctx)
	require.NoError(t, err)

	// Claim must remain PENDING — not silently auto-approved
	claim, err := k.GetClaim(ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_PENDING, claim.Status,
		"claim with unparseable amount should remain pending")

	// Verify it was NOT auto-approved
	for _, e := range ctx.EventManager().Events() {
		assert.NotEqual(t, "insurance_claim_auto_approved", e.Type,
			"invalid-amount claim must not be auto-approved")
	}
}
