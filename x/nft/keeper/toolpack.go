package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/nft/types"
)

// SetToolpack stores or updates a toolpack NFT.
func (k Keeper) SetToolpack(ctx sdk.Context, pack *types.ToolpackNFT) error {
	id, err := canonicalToolpackID(pack.GetId())
	if err != nil {
		return err
	}
	return k.toolpacks.Set(ctxOf(ctx), id, pack)
}

// HasToolpack reports whether a toolpack exists.
func (k Keeper) HasToolpack(ctx sdk.Context, id string) bool {
	cid, err := canonicalToolpackID(id)
	if err != nil {
		return false
	}
	has, err := k.toolpacks.Has(ctxOf(ctx), cid)
	if err != nil {
		return false
	}
	return has
}

// GetToolpack retrieves a toolpack NFT by id. Matches the credits NFTKeeper
// contract: (nil, false, nil) when absent so settlement skips the royalty step.
func (k Keeper) GetToolpack(ctx context.Context, id string) (*types.ToolpackNFT, bool, error) {
	cid, err := canonicalToolpackID(id)
	if err != nil {
		return nil, false, err
	}
	pack, err := k.toolpacks.Get(ctx, cid)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return pack, true, nil
}

// GetAllToolpacks returns all toolpacks (deterministic order not guaranteed by
// the map; callers that need ordering should sort).
func (k Keeper) GetAllToolpacks(ctx sdk.Context) []*types.ToolpackNFT {
	out := make([]*types.ToolpackNFT, 0)
	_ = k.toolpacks.Walk(ctxOf(ctx), nil, func(_ string, pack *types.ToolpackNFT) (bool, error) {
		if pack != nil {
			out = append(out, pack)
		}
		return false, nil
	})
	return out
}
