//go:build cosmos && cosmos_full

package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// RecordPartialFill records a partial settlement for an action.
func (k Keeper) RecordPartialFill(ctx context.Context, req SettlementRequest) error {
	req.Stage = "partial_fill"
	// Ensure ActionID is set (use ReceiptID if missing, as ReceiptID=ActionID convention)
	if req.ActionID == "" {
		req.ActionID = req.ReceiptID
	}

	_, err := k.ProcessSettlement(ctx, req)
	return err
}

// FinalizeAction triggers final settlement for an action.
// It retrieves the accumulated settlement record and processes it as final.
//
// Action-to-receipt identity contract: actions use actionID as the
// receipt key (ReceiptID == ActionID). This is an INTENTIONAL
// collapse — action-scoped settlements don't have an independent
// receipt identifier because the action IS the settlement unit. The
// x/ibc_action module populates ActionID, and the settlement record
// at that key holds the action's cumulative cost + publisher +
// router attribution. If a future feature splits these two
// namespaces, replace GetSettlement(ctx, actionID) with a lookup
// keyed on the new receipt ID and thread that through from the
// caller.
func (k Keeper) FinalizeAction(ctx context.Context, actionID string, lockID string) (*SettlementResult, error) {
	record, found := k.GetSettlement(ctx, actionID)
	if !found {
		return nil, fmt.Errorf("no settlement record found for action %s", actionID)
	}

	if record.Status == types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
		return nil, fmt.Errorf("action %s already finalized", actionID)
	}

	// Reconstruct SettlementRequest from record
	// We need addresses (AccAddress) from string IDs
	publisherAddr, err := sdk.AccAddressFromBech32(record.PublisherId)
	if err != nil {
		return nil, fmt.Errorf("invalid publisher id in record: %w", err)
	}
	routerAddr, err := sdk.AccAddressFromBech32(record.RouterId)
	if err != nil {
		return nil, fmt.Errorf("invalid router id in record: %w", err)
	}
	var referrerAddr sdk.AccAddress
	if record.ReferrerId != "" {
		referrerAddr, err = sdk.AccAddressFromBech32(record.ReferrerId)
		if err != nil {
			return nil, fmt.Errorf("invalid referrer id in record: %w", err)
		}
	}

	// Determine the denom from the accumulated TotalCost, fallback to default if empty.
	totalCost := types.CoinsFromProto(record.TotalCost)
	denom := types.DefaultCreditDenom
	if !totalCost.Empty() {
		denom = totalCost[0].Denom
	}

	// For finalization, we pass zero as the delta since the cost is already accumulated.
	// SettleLock/ProcessSettlement will read the existing TotalCost from the settlement record.
	req := SettlementRequest{
		ReceiptID:      actionID, // Use ActionID as ReceiptID
		ActionID:       actionID,
		ToolID:         record.ToolId,
		TotalAmount:    sdk.NewCoins(), // No new delta, just finalizing existing accumulated cost
		PublisherAddr:  publisherAddr,
		RouterAddr:     routerAddr,
		ReferrerAddr:   referrerAddr,
		CacheHit:       record.CacheHit,
		OriginToolID:   record.OriginToolId,
		PublisherID:    record.PublisherId,
		UserID:         record.UserId,
		RouterID:       record.RouterId,
		ReferrerID:     record.ReferrerId,
		ToolpackID:     record.ToolpackId,
		Stage:          "finalized",
	}

	// Call SettleLock to handle burn/refund/settlement
	// Pass zero actualCost since we're finalizing, not adding new cost.
	// SettleLock will read the accumulated total from the settlement record.
	return k.SettleLock(ctx, lockID, sdk.NewInt64Coin(denom, 0), req)
}
