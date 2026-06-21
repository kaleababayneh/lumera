// Package keeper hosts the core credits keeper implementation.
package keeper

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	cactypes "github.com/LumeraProtocol/lumera/x/cac/types"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// Keep the curator royalty helper referenced so golangci-lint does not fail the cosmos_full build
// when the helper is intentionally left for upcoming settlement refactors.
var _ = Keeper.distributeCuratorRoyalty

// ProcessCACRoyalty handles the special royalty split for cache hits
// Per current CAC policy: origin and serving tools split publisher share 50/50
// Returns the actual amounts distributed to each party for accurate accounting
func (k Keeper) ProcessCACRoyalty(
	ctx context.Context,
	originToolID string,
	servingToolID string,
	publisherAmount sdk.Coins,
) (originAmount, servingAmount sdk.Coins, err error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Validate input - don't process empty amounts
	if publisherAmount.IsZero() {
		return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("publisherAmount cannot be zero")
	}

	// Look up origin tool publisher
	originPublisher, err := k.GetToolPublisher(ctx, originToolID)
	if err != nil {
		return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("failed to get origin tool publisher: %w", err)
	}
	if originPublisher == nil {
		return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("origin tool %s has no publisher address", originToolID)
	}

	// Look up serving tool publisher
	servingPublisher, err := k.GetToolPublisher(ctx, servingToolID)
	if err != nil {
		return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("failed to get serving tool publisher: %w", err)
	}
	if servingPublisher == nil {
		return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("serving tool %s has no publisher address", servingToolID)
	}

	// Calculate split using the shared CAC default so config and settlement stay aligned.
	originShareBPS := cactypes.DefaultRoyaltyOriginBPS

	originAmount = sdk.NewCoins()
	servingAmount = sdk.NewCoins()

	for _, coin := range publisherAmount {
		// Calculate origin share from the configured basis points.
		originQty := coin.Amount.MulRaw(int64(originShareBPS)).QuoRaw(10000)
		if originQty.IsPositive() {
			originAmount = originAmount.Add(sdk.NewCoin(coin.Denom, originQty))
		}

		// Calculate serving share as remainder to avoid rounding dust loss
		// servingQty = total - originQty ensures exact split with no dust
		servingQty := coin.Amount.Sub(originQty)
		if servingQty.IsPositive() {
			servingAmount = servingAmount.Add(sdk.NewCoin(coin.Denom, servingQty))
		}
	}

	// Distribute to origin publisher - MUST succeed to prevent funds getting stuck
	// Fail the entire transaction if distribution fails (no partial success allowed)
	if !originAmount.IsZero() {
		if originPublisher == nil {
			return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("origin tool %s has no publisher address", originToolID)
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(
			sdkCtx, types.ModuleAccountName, originPublisher, originAmount,
		); err != nil {
			return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("failed to send CAC royalty to origin publisher: %w", err)
		}
	}

	// Distribute to serving publisher - MUST succeed to prevent funds getting stuck
	// Fail the entire transaction if distribution fails (no partial success allowed)
	if !servingAmount.IsZero() {
		if servingPublisher == nil {
			return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("serving tool %s has no publisher address", servingToolID)
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(
			sdkCtx, types.ModuleAccountName, servingPublisher, servingAmount,
		); err != nil {
			return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("failed to send CAC royalty to serving publisher: %w", err)
		}
	}

	// Record the royalty distribution (using actual amounts that were sent).
	// Generate unique RecordID using the CAC sequence number. If the sequence
	// allocation fails we MUST return the error rather than fall back to a
	// derived seq: the prior fallback used blockTime.UnixNano() which is
	// identical for every call in the same block, so a second failure in
	// the same block would silently overwrite an earlier royalty record via
	// SaveCACRoyaltyRecord's unconditional Set. Bank transfers above are
	// rolled back atomically by the msg-scoped revert when we return error.
	seq, err := k.state.CACSeq.Next(ctx)
	if err != nil {
		return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("allocate CAC sequence: %w", err)
	}

	// Guard against nil publisher addresses — GetToolPublisher may return nil
	// without error when a tool exists but has no publisher set.
	originPubStr := ""
	if originPublisher != nil {
		originPubStr = originPublisher.String()
	}
	servingPubStr := ""
	if servingPublisher != nil {
		servingPubStr = servingPublisher.String()
	}

	record := &types.CACRoyaltyRecord{
		RecordId:         fmt.Sprintf("cac-%d", seq),
		OriginToolId:     originToolID,
		ServingToolId:    servingToolID,
		OriginPublisher:  originPubStr,
		ServingPublisher: servingPubStr,
		TotalAmount:      types.CoinsToProto(publisherAmount),
		OriginShare:      types.CoinsToProto(originAmount),
		ServingShare:     types.CoinsToProto(servingAmount),
		Timestamp:        sdkCtx.BlockTime(),
	}

	if err := k.SaveCACRoyaltyRecord(ctx, record); err != nil {
		// Previously we logged-and-continued here on the theory that the
		// royalty distribution had already happened via the bank sends
		// above, so losing the audit record was tolerable. That's wrong
		// for two reasons: (1) returning error atomically reverts the
		// bank sends too (msg-scoped revert), so there is no "already
		// happened" state to preserve, and (2) the CACRoyaltyRecord IS
		// the settlement audit trail — silently losing it leaves the
		// bank ledger with no matching accounting row and breaks
		// downstream analytics + dispute resolution.
		return sdk.NewCoins(), sdk.NewCoins(), fmt.Errorf("save CAC royalty record: %w", err)
	}

	// Emit event for tracking (with distributed amounts)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"cac_royalty_distribution",
			sdk.NewAttribute("origin_tool", originToolID),
			sdk.NewAttribute("serving_tool", servingToolID),
			sdk.NewAttribute("origin_share", originAmount.String()),
			sdk.NewAttribute("serving_share", servingAmount.String()),
			sdk.NewAttribute("total_amount", publisherAmount.String()),
		),
	)

	return originAmount, servingAmount, nil
}

// GetToolPublisher retrieves the publisher address for a tool
func (k Keeper) GetToolPublisher(ctx context.Context, toolID string) (sdk.AccAddress, error) {
	// Use the registry keeper to get the actual publisher address
	if k.registryKeeper != nil {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		return k.registryKeeper.GetToolPublisher(sdkCtx, toolID)
	}

	// Fallback for testing when registry keeper is not available
	// Generate a deterministic address based on tool ID using crypto/sha256
	hasher := sha256.New()
	hasher.Write([]byte("tool_publisher_" + toolID))
	hash := hasher.Sum(nil)
	return sdk.AccAddress(hash[:20]), nil
}

// SaveCACRoyaltyRecord persists a CAC royalty record using type-safe Collections API
func (k Keeper) SaveCACRoyaltyRecord(ctx context.Context, record *types.CACRoyaltyRecord) error {
	return k.state.CACRoyalties.Set(ctx, record.RecordId, record)
}

// GetCACRoyaltyStats returns statistics about CAC royalty distributions using type-safe Collections API
func (k Keeper) GetCACRoyaltyStats(ctx context.Context, toolID string) (*types.CACRoyaltyStats, error) {
	stats, err := k.state.CACStats.Get(ctx, toolID)
	if err != nil {
		// If not found, return empty stats (Collections returns ErrNotFound for missing keys)
		if errors.Is(err, collections.ErrNotFound) {
			return &types.CACRoyaltyStats{
				ToolId: toolID,
			}, nil
		}
		return nil, fmt.Errorf("failed to get CAC stats: %w", err)
	}
	return stats, nil
}

// UpdateCACRoyaltyStats updates statistics for CAC royalties using type-safe Collections API
func (k Keeper) UpdateCACRoyaltyStats(
	ctx context.Context,
	toolID string,
	isOrigin bool,
	amount sdk.Coins,
) error {
	// Get existing stats (returns empty if not found)
	stats, err := k.GetCACRoyaltyStats(ctx, toolID)
	if err != nil {
		return err
	}

	// Convert existing proto coins to sdk.Coins for arithmetic
	earned := types.CoinsFromProto(stats.TotalRoyaltiesEarned)
	paid := types.CoinsFromProto(stats.TotalRoyaltiesPaid)

	// Update stats
	if isOrigin {
		stats.TotalCacheHits++
		earned = earned.Add(amount...)
		stats.TotalRoyaltiesEarned = types.CoinsToProto(earned)
	} else {
		paid = paid.Add(amount...)
		stats.TotalRoyaltiesPaid = types.CoinsToProto(paid)
	}
	stats.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime()

	// Save updated stats using Collections API
	return k.state.CACStats.Set(ctx, toolID, stats)
}

// ProcessSettlementWithCAC is a legacy wrapper around ProcessSettlement.
//
// Deprecated: Use ProcessSettlement instead, which includes all bug fixes and improvements.
func (k Keeper) ProcessSettlementWithCAC(ctx context.Context, receipt SettlementRequest) (*SettlementResult, error) {
	// Simply delegate to the fully fixed and validated ProcessSettlement
	// This ensures any accidental callers get the safe, fixed version
	return k.ProcessSettlement(ctx, receipt)
}
