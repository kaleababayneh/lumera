
// Package keeper implements the insurance module state transitions and handlers.
package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// FileClaim creates a new insurance claim
func (k Keeper) FileClaim(ctx context.Context, msg *types.MsgFileClaim) (string, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Validate receipt exists and hasn't already been claimed
	_, err := k.state.ClaimsByReceipt.Indexes.Receipt.MatchExact(sdkCtx, msg.ReceiptId)
	if err == nil {
		// If we found a key, a claim already exists for this receipt
		return "", types.ErrDuplicateClaim.Wrapf("claim already exists for receipt %s", msg.ReceiptId)
	} else if !errors.Is(err, collections.ErrNotFound) {
		return "", err
	}
	// ErrNotFound is expected here, meaning no existing claim

	// Verify insurance coverage exists (contribution recorded).
	// Contribution receipt index is Multi, so MatchExact returns an iterator.
	covered, err := k.hasContributionForReceipt(sdkCtx, msg.ReceiptId)
	if err != nil {
		return "", err
	}
	if !covered {
		return "", types.ErrInvalidClaimRequest.Wrapf("no insurance coverage found for receipt %s", msg.ReceiptId)
	}

	// Verify ownership if recorded
	owner, err := k.state.ReceiptOwners.Get(sdkCtx, msg.ReceiptId)
	if err == nil {
		if owner != "" && owner != msg.Claimant {
			return "", types.ErrUnauthorized.Wrapf("claimant %s is not the receipt owner", msg.Claimant)
		}
	} else if !errors.Is(err, collections.ErrNotFound) {
		return "", err
	}

	// Resolve the authoritative publisher from the stored contribution.
	// Contributions are written via MsgProcessContribution (authority-
	// gated), so their PublisherId cannot be spoofed by the claimant.
	//
	// Without this check, a claimant with a legitimate receipt could file
	// a small auto-approve claim with msg.PublisherId pointing at any
	// VICTIM publisher, and the payout path would increment
	// PublisherRisks[victim]:ClaimCount — eventually triggering recidivist
	// payout reductions (up to 50%) on the victim's own legitimate claims.
	// The attacker pays the tool-invocation cost but recovers most of it
	// via the auto-approved claim, so this is a cheap griefing vector.
	//
	// If msg.PublisherId is set and doesn't match the authoritative
	// publisher, reject. If msg.PublisherId is empty, fill it in from
	// the stored contribution so downstream payout accounting targets
	// the correct publisher.
	authoritativePublisher, err := k.publisherIDForReceipt(sdkCtx, msg.ReceiptId)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(msg.PublisherId) != "" && authoritativePublisher != "" &&
		msg.PublisherId != authoritativePublisher {
		return "", types.ErrUnauthorized.Wrapf(
			"claim publisher_id %q does not match receipt %s contribution publisher %q",
			msg.PublisherId, msg.ReceiptId, authoritativePublisher,
		)
	}
	effectivePublisher := authoritativePublisher
	if effectivePublisher == "" {
		effectivePublisher = msg.PublisherId
	}

	// Generate claim ID
	claimSeq, err := k.state.ClaimCounter.Next(sdkCtx)
	if err != nil {
		return "", fmt.Errorf("failed to generate claim ID: %w", err)
	}
	claimID := fmt.Sprintf("claim-%d", claimSeq)

	// Claim-window enforcement happens at EndBlocker, not here:
	// processExpiredClaims in keeper.go:295 uses
	// params.ClaimWindowSeconds as the REVIEW window (auto-approval
	// deadline after filing), not a reject-on-file ceiling. Filings
	// are accepted whenever a matching contribution exists; the
	// review clock starts ticking at FiledAt regardless.

	// Create the claim. Use effectivePublisher (authoritative if the
	// receipt has a contribution; falls back to msg.PublisherId only when
	// no contribution is on file, which shouldn't happen in practice —
	// the hasContributionForReceipt check above already rejected that
	// case — but we keep the fallback defensive).
	claim := &types.Claim{
		Id:            claimID,
		ReceiptId:     msg.ReceiptId,
		ClaimantId:    msg.Claimant,
		ToolId:        msg.ToolId,
		PublisherId:   effectivePublisher,
		ClaimedAmount: msg.ClaimedAmount,
		Reason:        msg.Reason,
		Evidence:      msg.Evidence,
		Status:        types.ClaimStatus_CLAIM_STATUS_PENDING,
		CreatedAt:     timePtr(sdkCtx.BlockTime()),
		UpdatedAt:     timePtr(sdkCtx.BlockTime()),
	}

	// Store the claim
	if err := k.state.ClaimsByReceipt.Set(sdkCtx, claimID, claim); err != nil {
		return "", fmt.Errorf("failed to store claim: %w", err)
	}

	// Update pool metrics
	if err := k.updatePendingClaimsCount(sdkCtx, 1); err != nil {
		return "", fmt.Errorf("failed to update metrics: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeClaimFiled,
			sdk.NewAttribute(types.AttributeKeyClaimID, claimID),
			sdk.NewAttribute(types.AttributeKeyReceiptID, msg.ReceiptId),
			sdk.NewAttribute(types.AttributeKeyClaimant, msg.Claimant),
			sdk.NewAttribute(types.AttributeKeyAmount, msg.ClaimedAmount.String()),
		),
	)

	return claimID, nil
}

// ProcessClaim approves or rejects a claim
func (k Keeper) ProcessClaim(ctx context.Context, msg *types.MsgProcessClaim) error {
	if msg == nil {
		return types.ErrInvalidClaimRequest.Wrap("message cannot be nil")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if err := k.ValidateAuthority(msg.Authority); err != nil {
		return err
	}
	if msg.ClaimId == "" {
		return types.ErrInvalidClaimRequest.Wrap("claim_id is required")
	}

	resolution := strings.ToLower(strings.TrimSpace(msg.Resolution))
	var approved *sdk.Coin
	switch resolution {
	case "approve", "partial":
		if msg.ApprovedAmount.Amount.IsNil() {
			return types.ErrInvalidAmount.Wrap("approved_amount is required for approval")
		}
		coin, err := protoCoinToSDK(msg.ApprovedAmount)
		if err != nil {
			return err
		}
		approved = &coin
	case "reject":
		// nothing
	default:
		return types.ErrInvalidClaimResolution.Wrapf("unknown resolution: %s", msg.Resolution)
	}

	return k.processClaim(sdkCtx, msg.ClaimId, resolution, approved, msg.Notes)
}

// GetClaim retrieves a claim by ID
func (k Keeper) GetClaim(ctx context.Context, claimID string) (*types.Claim, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	claim, err := k.state.ClaimsByReceipt.Get(sdkCtx, claimID)
	if err != nil {
		return nil, types.ErrClaimNotFound.Wrapf("claim %s not found", claimID)
	}
	return claim, nil
}

// GetClaimsByReceipt retrieves all claims for a receipt
func (k Keeper) GetClaimsByReceipt(ctx context.Context, receiptID string) ([]*types.Claim, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var claims []*types.Claim
	iter, err := k.state.ClaimsByReceipt.Indexes.Receipt.MatchExact(sdkCtx, receiptID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return claims, nil // Return empty list if no claims found
		}
		return nil, err
	}

	// Get the single claim for this receipt (unique index)
	if iter != "" {
		claim, err := k.state.ClaimsByReceipt.Get(sdkCtx, iter)
		if err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}

	return claims, nil
}

// GetClaimsByStatus retrieves all claims with a specific status
func (k Keeper) GetClaimsByStatus(ctx context.Context, status types.ClaimStatus) ([]*types.Claim, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var claims []*types.Claim
	// Status is a multi index with composite key (Status, Time).
	// The index key set is Pair[Pair[string, time.Time], string] (refKey, primaryKey).
	// Use PairPrefix to create a partial reference key matching just the status string.
	statusPrefix := collections.PairPrefix[string, time.Time](status.String())
	rng := collections.NewPrefixedPairRange[collections.Pair[string, time.Time], string](statusPrefix)
	if err := k.state.ClaimsByReceipt.Indexes.Status.Walk(sdkCtx, rng,
		func(_ collections.Pair[string, time.Time], primaryKey string) (stop bool, err error) {
			claim, err := k.state.ClaimsByReceipt.Get(sdkCtx, primaryKey)
			if err != nil {
				return true, err
			}
			claims = append(claims, claim)
			return false, nil
		}); err != nil {
		return nil, err
	}

	return claims, nil
}

// updatePendingClaimsCount updates the pending claims counter in metrics
func (k Keeper) updatePendingClaimsCount(ctx sdk.Context, delta int32) error {
	metrics, err := k.state.PoolMetrics.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		metrics = &types.PoolMetrics{PendingClaims: 0}
	}

	if delta > 0 {
		metrics.PendingClaims += uint32(delta)
	} else if delta < 0 {
		reduction := uint32(-delta)
		if reduction >= metrics.PendingClaims {
			metrics.PendingClaims = 0
		} else {
			metrics.PendingClaims -= reduction
		}
	}

	return k.state.PoolMetrics.Set(ctx, metrics)
}

// reserveFundsForClaim marks funds as reserved for an approved claim.
//
// Deprecated: retained for backwards compatibility with legacy simulations.
//
//lint:ignore U1000 kept for backwards compatibility with older tests
func (k Keeper) reserveFundsForClaim(ctx sdk.Context, _ string, amount sdk.Coin) error {
	if amount.Amount.IsNil() {
		return nil
	}
	coin, err := protoCoinToSDK(amount)
	if err != nil {
		return err
	}
	return k.reserveApprovedAmount(ctx, coin)
}
