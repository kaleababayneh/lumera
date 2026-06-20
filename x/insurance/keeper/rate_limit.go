
package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// RateLimiter enforces per-receipt uniqueness on claims and
// contributions.
//
// INTENTIONAL SCOPE: today's "rate limiting" means per-receipt
// uniqueness (one claim per receipt, one contribution per receipt)
// via unique-indexes on ClaimsByReceipt.Indexes.Receipt and
// ContribByReceipt.Indexes.Receipt. It is NOT a time-windowed /
// configurable / per-address limiter — and the usual addition
// (per-block caps, governance-tunable thresholds) would be a
// separate dimension, not a refinement of this one. The
// per-receipt dimension is load-bearing for the settle-once
// contract: a duplicate claim or contribution on the same receipt
// is always an error regardless of timing.
//
// See CheckGlobalClaimRate below for the contrasting
// always-pass no-op that WOULD be where a governance-parameter
// time-windowed cap lands.
type RateLimiter struct {
	keeper *Keeper
}

// NewRateLimiter creates a new rate limiter for the insurance module
func NewRateLimiter(k *Keeper) *RateLimiter {
	return &RateLimiter{keeper: k}
}

// CheckClaimRate enforces per-receipt uniqueness: at most one
// claim may exist for a given receiptID. Rejects a duplicate with
// ErrDuplicateClaim. Not a time-windowed limiter — see the
// RateLimiter struct doc for scope rationale.
func (rl *RateLimiter) CheckClaimRate(ctx context.Context, receiptID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check for existing claim using the exact match index
	existingClaimID, err := rl.keeper.state.ClaimsByReceipt.Indexes.Receipt.MatchExact(sdkCtx, receiptID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// No existing claim found, rate limit check passes
			return nil
		}
		return types.ErrRateLimitCheckFailed.Wrapf("failed to check for existing claim: %s", err)
	}

	// If we found an existing claim, return duplicate error
	return types.ErrDuplicateClaim.Wrapf("claim already exists for receipt %s (claim ID: %s)", receiptID, existingClaimID)
}

// CheckContributionRate enforces per-receipt uniqueness on
// contributions: the insurance module accepts at most one
// contribution record per receiptID. Rejects duplicates with
// ErrContributionRateLimitExceeded. Contributions themselves are
// system-initiated (triggered by settlement flows, not user
// input), so there is no per-claimant throttling to apply here —
// the uniqueness check is the load-bearing invariant.
func (rl *RateLimiter) CheckContributionRate(ctx context.Context, receiptID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Prevent duplicate contributions per receipt.
	// ContribByReceipt.Indexes.Receipt is a Multi index, so MatchExact
	// returns an iterator (not ErrNotFound for empty results).
	iter, err := rl.keeper.state.ContribByReceipt.Indexes.Receipt.MatchExact(sdkCtx, receiptID)
	if err != nil {
		return types.ErrRateLimitCheckFailed.Wrapf("failed to check contribution rate: %s", err)
	}
	defer func() { _ = iter.Close() }()

	if iter.Valid() {
		return types.ErrContributionRateLimitExceeded.Wrapf("contribution already recorded for receipt %s", receiptID)
	}

	return nil
}

// CheckGlobalClaimRate verifies global claim rate limits.
//
// Intentionally a no-op today — global (cross-claimant) rate
// limiting is NOT enforced. The insurance module already rate-limits
// per-receipt (one contribution per receiptID, enforced above in
// this file) and per-claim age via processExpiredClaims in
// keeper.go, which bounds MaxClaimsPerBlock. A global cap on
// claims-per-block or claims-per-hour would add another dimension
// but has no governance parameter to drive the threshold.
//
// If a future governance change adds one (e.g., Params.MaxGlobalClaimsPerHour),
// implement the check here by reading the param from state, counting
// recent claims via a block-time-bucketed index, and returning
// ErrContributionRateLimitExceeded (or a new ErrGlobalClaimRateExceeded)
// when over the threshold. The test
// TestRateLimiter_CheckGlobalClaimRate_AlwaysPass in rate_limit_test.go
// currently pins the no-op contract and will fail when the new
// enforcement lands, forcing the test to be updated deliberately.
func (rl *RateLimiter) CheckGlobalClaimRate(ctx context.Context) error {
	_ = ctx
	return nil
}
