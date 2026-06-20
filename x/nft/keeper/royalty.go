package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/nft/types"
)

// RecordRoyaltyPayout records that a royalty was paid out for a toolpack (the
// actual coin transfer happens in the credits settlement; this is the toolpack
// module's bookkeeping/audit hook). Matches the credits NFTKeeper contract.
//
// NOTE: cumulative per-toolpack royalty stats are deferred to a later slice;
// this slice validates the authority + toolpack, touches the toolpack, and emits
// the audit event.
func (k Keeper) RecordRoyaltyPayout(ctx context.Context, authority string, toolpackID string, amount sdk.Coin) error {
	if authority != k.authority {
		return types.ErrUnauthorized
	}
	id, err := canonicalToolpackID(toolpackID)
	if err != nil {
		return err
	}
	if !amount.IsValid() {
		return fmt.Errorf("invalid royalty amount")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	pack, err := k.toolpacks.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ErrToolpackNotFound
		}
		return err
	}
	now := sdkCtx.BlockTime()
	pack.UpdatedAt = &now
	if err := k.toolpacks.Set(ctx, id, pack); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRoyaltyPayout,
			sdk.NewAttribute(types.AttributeKeyToolpackID, id),
			sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
		),
	)
	return nil
}
