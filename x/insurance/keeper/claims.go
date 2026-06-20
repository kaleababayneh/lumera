// DO NOT enable the todo_claims build tag: this file is a pre-proto-
// migration artifact that was superseded by claim_operations.go. The
// two files both define Keeper.FileClaim, Keeper.ProcessClaim,
// Keeper.GetClaim, and Keeper.GetClaimsByReceipt, and the local Claim
// struct here uses a string Status field + sdk.Coins + *time.Time,
// whereas the production flow in claim_operations.go uses the
// proto-generated types.Claim (typed ClaimStatus enum, v1beta1.Coin,
// *timestamppb.Timestamp). Enabling todo_claims yields 11 compile
// errors (method redeclaration + type mismatches on Keeper.evaluateClaim,
// cdc.Marshal, Status comparison, ResolvedAt/Resolution fields). The
// tag is retained — instead of the file being deleted — so the historical
// pre-proto shape remains discoverable via git history without a blame
// break; see docs/reports/MODES_OF_REASONING_REPORT_AND_ANALYSIS_OF_PROJECT.md for
// the wider context on why claims/bonds were stubbed before the proto
// migration completed.
//
//go:build cosmos && cosmos_full && todo_claims

package keeper

import (
	"context"
	"fmt"
	"time"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
)

// Claim represents an insurance claim against a tool's SLA bond
type Claim struct {
	ClaimID         string
	ReceiptID       string
	ToolID          string
	ClaimantID      string
	ViolationType   string
	Evidence        ClaimEvidence
	RequestedAmount sdk.Coins
	Status          string // "pending", "approved", "rejected", "paid"
	FiledAt         time.Time
	ResolvedAt      *time.Time
	Resolution      string
	PayoutAmount    sdk.Coins
}

// ClaimEvidence contains proof of SLA violation
type ClaimEvidence struct {
	ActualLatencyMs   uint32
	ExpectedLatencyMs uint32
	ActualCost        sdk.Coins
	QuotedCost        sdk.Coins
	ErrorMessage      string
	Timestamp         time.Time
	SignedReceipt     []byte
}

// FileClaim creates a new insurance claim for SLA violation
func (k Keeper) FileClaim(ctx context.Context, claim *Claim) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := k.storeService.OpenKVStore(ctx)

	// Validate claim
	if err := k.validateClaim(ctx, claim); err != nil {
		return fmt.Errorf("invalid claim: %w", err)
	}

	// Generate claim ID
	claim.ClaimID = fmt.Sprintf("claim-%s-%d", claim.ToolID, sdkCtx.BlockTime().Unix())
	claim.FiledAt = sdkCtx.BlockTime()
	claim.Status = "pending"

	// Save claim
	claimKey := types.GetClaimKey(claim.ClaimID)
	bz, err := k.cdc.Marshal(claim)
	if err != nil {
		return fmt.Errorf("failed to marshal claim: %w", err)
	}

	if err := store.Set(claimKey, bz); err != nil {
		return fmt.Errorf("failed to save claim: %w", err)
	}

	// Create receipt index
	receiptIndexKey := types.GetClaimByReceiptIndexKey(claim.ReceiptID, claim.ClaimID)
	if err := store.Set(receiptIndexKey, []byte(claim.ClaimID)); err != nil {
		return fmt.Errorf("failed to create receipt index: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"claim_filed",
			sdk.NewAttribute("claim_id", claim.ClaimID),
			sdk.NewAttribute("tool_id", claim.ToolID),
			sdk.NewAttribute("claimant", claim.ClaimantID),
			sdk.NewAttribute("violation_type", claim.ViolationType),
			sdk.NewAttribute("amount", claim.RequestedAmount.String()),
		),
	)

	return nil
}

// ProcessClaim evaluates and processes an insurance claim
func (k Keeper) ProcessClaim(ctx context.Context, claimID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := k.storeService.OpenKVStore(ctx)

	// Get claim
	claim, err := k.GetClaim(ctx, claimID)
	if err != nil {
		return err
	}

	if claim.Status != "pending" {
		return fmt.Errorf("claim already processed: %s", claim.Status)
	}

	// Evaluate claim
	approved, payoutAmount, reason := k.evaluateClaim(ctx, claim)

	now := sdkCtx.BlockTime()
	claim.ResolvedAt = &now

	if approved {
		claim.Status = "approved"
		claim.Resolution = reason
		claim.PayoutAmount = payoutAmount

		// Find and slash bond
		bonds, err := k.GetToolBonds(ctx, claim.ToolID)
		if err != nil {
			return fmt.Errorf("failed to get tool bonds: %w", err)
		}

		if len(bonds) == 0 {
			claim.Status = "rejected"
			claim.Resolution = "no active bond found"
		} else {
			// Slash the first active bond
			bond := bonds[0]
			if err := k.SlashBond(ctx, bond.BondID, claim.ViolationType, payoutAmount, claim.ClaimID); err != nil {
				return fmt.Errorf("failed to slash bond: %w", err)
			}

			// Pay out to claimant
			// Note: This would require the actual bankKeeper dependency
			// claimantAddr, _ := sdk.AccAddressFromBech32(claim.ClaimantID)
			// if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, claimantAddr, payoutAmount); err != nil {
			//     return fmt.Errorf("failed to pay claim: %w", err)
			// }

			claim.Status = "paid"
		}
	} else {
		claim.Status = "rejected"
		claim.Resolution = reason
		claim.PayoutAmount = sdk.NewCoins()
	}

	// Save updated claim
	claimKey := types.GetClaimKey(claim.ClaimID)
	bz, err := k.cdc.Marshal(claim)
	if err != nil {
		return fmt.Errorf("failed to marshal updated claim: %w", err)
	}

	if err := store.Set(claimKey, bz); err != nil {
		return fmt.Errorf("failed to save updated claim: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"claim_processed",
			sdk.NewAttribute("claim_id", claim.ClaimID),
			sdk.NewAttribute("status", claim.Status),
			sdk.NewAttribute("resolution", claim.Resolution),
			sdk.NewAttribute("payout", claim.PayoutAmount.String()),
		),
	)

	return nil
}

// validateClaim checks if a claim is valid
func (k Keeper) validateClaim(ctx context.Context, claim *Claim) error {
	// Check receipt exists and is valid
	if claim.ReceiptID == "" {
		return fmt.Errorf("receipt ID required")
	}

	// Check tool exists
	// Note: This would require the actual registryKeeper dependency
	// if _, found := k.registryKeeper.GetTool(sdk.UnwrapSDKContext(ctx), claim.ToolID); !found {
	//     return fmt.Errorf("tool not found: %s", claim.ToolID)
	// }

	// Check violation type is valid
	switch claim.ViolationType {
	case "latency", "availability", "error_rate", "cost_overrun":
		// Valid types
	default:
		return fmt.Errorf("invalid violation type: %s", claim.ViolationType)
	}

	// Check requested amount is reasonable
	if claim.RequestedAmount.IsZero() {
		return fmt.Errorf("requested amount cannot be zero")
	}

	return nil
}

// evaluateClaim determines if a claim should be approved and the payout amount
func (k Keeper) evaluateClaim(ctx context.Context, claim *Claim) (bool, sdk.Coins, string) {
	// Get tool's SLA targets from bond
	bonds, err := k.GetToolBonds(ctx, claim.ToolID)
	if err != nil || len(bonds) == 0 {
		return false, sdk.NewCoins(), "no active bond found"
	}

	bond := bonds[0]
	targets := bond.SLOTargets

	switch claim.ViolationType {
	case "latency":
		if targets.MaxLatencyMs > 0 && claim.Evidence.ActualLatencyMs > targets.MaxLatencyMs {
			excess := decimal.NewFromInt(int64(claim.Evidence.ActualLatencyMs - targets.MaxLatencyMs))
			base := decimal.NewFromInt(int64(targets.MaxLatencyMs))
			severity := excess.Div(base)
			payoutAmount := k.CalculateSlashAmount(ctx, bond, "latency", severity)
			return true, payoutAmount, fmt.Sprintf("latency violation: %dms > %dms",
				claim.Evidence.ActualLatencyMs, targets.MaxLatencyMs)
		}

	case "cost_overrun":
		if !claim.Evidence.ActualCost.IsZero() && !claim.Evidence.QuotedCost.IsZero() {
			maxCostVar := decimal.NewFromInt(int64(targets.MaxCostVarBps)).Div(decimal.NewFromInt(10000))
			if maxCostVar.IsZero() {
				break
			}
			for i, actualCoin := range claim.Evidence.ActualCost {
				if i < len(claim.Evidence.QuotedCost) {
					quotedCoin := claim.Evidence.QuotedCost[i]
					if actualCoin.Denom == quotedCoin.Denom {
						// Use string conversion to avoid silent int64 overflow on large amounts.
						actualDec, err1 := decimal.NewFromString(actualCoin.Amount.String())
						quotedDec, err2 := decimal.NewFromString(quotedCoin.Amount.String())
						if err1 != nil || err2 != nil {
							continue
						}
						diff := actualDec.Sub(quotedDec)
						quoted := quotedDec
						if quoted.IsZero() {
							continue
						}
						variance := diff.Div(quoted)
						if variance.GreaterThan(maxCostVar) {
							severity := variance.Div(maxCostVar)
							payoutAmount := k.CalculateSlashAmount(ctx, bond, "cost_overrun", severity)
							variancePct := variance.Mul(decimal.NewFromInt(100))
							maxPct := maxCostVar.Mul(decimal.NewFromInt(100))
							return true, payoutAmount, fmt.Sprintf("cost overrun: %s%% > %s%%",
								variancePct.StringFixed(2), maxPct.StringFixed(2))
						}
					}
				}
			}
		}

	case "error_rate":
		// Would need metrics to evaluate error rate
		// For now, auto-approve if evidence provided
		if claim.Evidence.ErrorMessage != "" {
			payoutAmount := k.CalculateSlashAmount(ctx, bond, "error_rate", decimal.NewFromInt(1))
			return true, payoutAmount, fmt.Sprintf("error occurred: %s", claim.Evidence.ErrorMessage)
		}

	case "availability":
		// Would need uptime metrics to evaluate
		// For now, reject as we can't verify
		return false, sdk.NewCoins(), "availability claims require metric history"
	}

	return false, sdk.NewCoins(), "violation not substantiated"
}

// GetClaim retrieves a claim by ID
func (k Keeper) GetClaim(ctx context.Context, claimID string) (*Claim, error) {
	store := k.storeService.OpenKVStore(ctx)

	claimKey := types.GetClaimKey(claimID)
	bz, err := store.Get(claimKey)
	if err != nil || bz == nil {
		return nil, fmt.Errorf("claim not found: %s", claimID)
	}

	var claim Claim
	if err := k.cdc.Unmarshal(bz, &claim); err != nil {
		return nil, fmt.Errorf("failed to unmarshal claim: %w", err)
	}

	return &claim, nil
}

// GetClaimsByReceipt returns all claims for a given receipt
func (k Keeper) GetClaimsByReceipt(ctx context.Context, receiptID string) ([]*Claim, error) {
	store := k.storeService.OpenKVStore(ctx)

	var claims []*Claim
	prefix := append(types.ClaimsByReceiptIndexPrefix, []byte(receiptID)...)

	iterator, err := store.Iterator(prefix, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		claimID := string(iterator.Value())
		claim, err := k.GetClaim(ctx, claimID)
		if err != nil {
			continue
		}
		claims = append(claims, claim)
	}

	return claims, nil
}

// ProcessClaimsInBlock processes all pending claims that have passed review period
func (k Keeper) ProcessClaimsInBlock(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := k.storeService.OpenKVStore(ctx)

	// Review period is 24 hours
	reviewPeriod := 24 * time.Hour
	cutoffTime := sdkCtx.BlockTime().Add(-reviewPeriod)

	iterator, err := store.Iterator(types.ClaimsKeyPrefix, nil)
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iterator.Close()

	var processedCount int
	for ; iterator.Valid(); iterator.Next() {
		var claim Claim
		if err := k.cdc.Unmarshal(iterator.Value(), &claim); err != nil {
			continue
		}

		// Process pending claims that have passed review period
		if claim.Status == "pending" && claim.FiledAt.Before(cutoffTime) {
			if err := k.ProcessClaim(ctx, claim.ClaimID); err != nil {
				k.Logger(sdkCtx).Error("failed to process claim", "claim_id", claim.ClaimID, "error", err)
			} else {
				processedCount++
			}
		}
	}

	if processedCount > 0 {
		k.Logger(sdkCtx).Info("processed insurance claims", "count", processedCount)
	}

	return nil
}
