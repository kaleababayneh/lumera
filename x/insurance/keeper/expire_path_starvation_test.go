
package keeper_test

import (
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// Starvation-path coverage for the 16400 fix
// ("insurance EndBlocker leaves oversized expired claims pending, causing
// permanent MaxClaimsPerBlock starvation").
//
// The fix routes three classes of non-auto-approvable claim out of the
// pending queue via expireClaimForManualReview at keeper.go:
//
//   1. Oversized (claim > AutoApproveThreshold) — keeper.go:457-470
//   2. High-leverage (claim > 4× contribution) — keeper.go:411-430
//   3. No coverage (contribution for denom == 0) — keeper.go:388-404
//
// All three branches end with `continue` so that a single failing
// review-path claim cannot starve subsequent claims past the
// MaxClaimsPerBlock cap.
//
// The existing TestEndBlocker_ExpiredReviewClaimsDoNotStarveLaterClaims
// (insurance_nomock_test.go:970) pins the STARVATION property for the
// oversized branch only. The high_leverage and no_coverage branches
// have per-claim tests (reproduce_claim_bug_test.go) that verify they
// transition individual claims to EXPIRED, but NOT the starvation-
// property — that a non-auto-approvable claim on those branches does
// not block a subsequent auto-approvable claim in the same
// MaxClaimsPerBlock budget window.
//
// Without the starvation tests below, a regression that e.g. dropped
// the `continue` in one of the two uncovered branches (so the iterator
// breaks out of the loop on that claim instead of moving to the next)
// would produce a subtle perma-starvation of the queue behind that
// branch. Pane 4's / pane 7's future refactor of the expiry dispatch
// would have no CI signal.

// TestEndBlocker_HighLeverageClaimsDoNotStarveLaterClaims mirrors the
// oversized-branch starvation test but through the HIGH-LEVERAGE
// review path. Setup: claim ≤ AutoApproveThreshold (not oversized) but
// claim > 4× contribution (high leverage).
func TestEndBlocker_HighLeverageClaimsDoNotStarveLaterClaims(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)

	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60
	params.AutoApproveThreshold = "100" // larger than both test claims
	params.MaxClaimsPerBlock = 1        // force starvation vulnerability
	params.Enabled = true
	require.NoError(t, k.SetParams(ctx, params))

	// Tiny contribution so "high leverage" triggers.
	// Contribution = 1 ulac. Max auto-claim at 4× = 4 ulac.
	recordContributionForTests(t, fixture, "receipt-leverage-first", "tool-alpha", "publisher-001", 1)
	// Normal contribution for the second claim.
	recordContributionForTests(t, fixture, "receipt-small-second", "tool-alpha", "publisher-001", 50)

	// First claim: 10 ulac. Under threshold (100), over 4×contrib (4)
	// → high-leverage path → expire to review.
	firstClaimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-leverage-first",
		ToolId:        "tool-alpha",
		PublisherId:   "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "10"},
		Reason:        "high-leverage first",
	})
	require.NoError(t, err)

	// Second claim: 5 ulac. Under threshold (100), under 4×contrib
	// (200) → auto-approve path.
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

	// First claim moves to EXPIRED (high-leverage review path).
	firstClaim, err := k.GetClaim(ctx, firstClaimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_EXPIRED, firstClaim.Status,
		"high-leverage first claim must be moved to EXPIRED out of "+
			"pending queue; otherwise it consumes the MaxClaimsPerBlock "+
			"budget forever and starves later claims")

	// Second claim: depending on whether the MaxClaimsPerBlock=1 window
	// was consumed by the expired-claim-move itself, the second claim
	// is either PENDING (waiting next block) or already APPROVED.
	// The KEY invariant is that within 2 blocks of the review window,
	// the second claim reaches APPROVED — proving the first claim did
	// NOT permanently starve the queue.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Second))
	require.NoError(t, k.EndBlocker(ctx))

	secondClaim, err := k.GetClaim(ctx, secondClaimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, secondClaim.Status,
		"small second claim MUST reach APPROVED within 2 blocks of the "+
			"review window — this proves the high-leverage branch's "+
			"`continue` correctly moves the offending claim out of the "+
			"pending queue without blocking subsequent throughput. "+
			"Pre-fix regression (missing continue) would leave the "+
			"first claim blocking forever.")
}

// TestEndBlocker_NoCoverageClaimsDoNotStarveLaterClaims mirrors the
// oversized-branch starvation test but through the NO-COVERAGE review
// path. Setup: claim with a denom that has zero contribution in the
// pool → `contribDec.IsZero()` branch at keeper.go:388 → expire to
// review.
func TestEndBlocker_NoCoverageClaimsDoNotStarveLaterClaims(t *testing.T) {
	fixture := setupKeeperTest(t)
	ctx := fixture.ctx
	k := fixture.keeper

	fundPoolForTests(t, fixture, 10000)

	params := types.DefaultParams()
	params.ClaimWindowSeconds = 60
	params.AutoApproveThreshold = "100"
	params.MaxClaimsPerBlock = 1
	params.Enabled = true
	require.NoError(t, k.SetParams(ctx, params))

	// Contribution is in "ulac". First claim uses "uother" → no
	// coverage for that denom → no-coverage path.
	recordContributionForTests(t, fixture, "receipt-nocov-first", "tool-alpha", "publisher-001", 50)
	// Second contribution is in ulac for the auto-approve claim.
	recordContributionForTests(t, fixture, "receipt-small-second", "tool-alpha", "publisher-001", 50)

	// First claim: 5 "uother" — no contribution for uother → hits
	// no-coverage review path, moves to EXPIRED.
	firstClaimID, err := k.FileClaim(ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-nocov-first",
		ToolId:        "tool-alpha",
		PublisherId:   "publisher-001",
		ClaimedAmount: &basev1beta1.Coin{Denom: "uother", Amount: "5"},
		Reason:        "no-coverage first",
	})
	require.NoError(t, err)

	// Second claim: 5 ulac — small ulac claim matches contribution,
	// auto-approves.
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
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_EXPIRED, firstClaim.Status,
		"no-coverage first claim must be moved to EXPIRED out of "+
			"pending; otherwise it consumes MaxClaimsPerBlock budget "+
			"indefinitely and starves later claims")

	// Advance a block for the auto-approve path.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Second))
	require.NoError(t, k.EndBlocker(ctx))

	secondClaim, err := k.GetClaim(ctx, secondClaimID)
	require.NoError(t, err)
	assert.Equal(t, types.ClaimStatus_CLAIM_STATUS_APPROVED, secondClaim.Status,
		"small second claim MUST reach APPROVED within 2 blocks — "+
			"proves the no-coverage branch's `continue` correctly moves "+
			"the offending claim out of the pending queue without "+
			"blocking subsequent throughput. Pre-fix regression (missing "+
			"continue) would leave the first claim blocking forever.")
}
