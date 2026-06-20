package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/nft/types"
)

// InitGenesis restores toolpacks from genesis.
func (k Keeper) InitGenesis(ctx sdk.Context, gs *types.GenesisState) {
	if gs == nil {
		gs = types.DefaultGenesis()
	}
	for _, pack := range gs.Toolpacks {
		if pack == nil {
			continue
		}
		if err := k.SetToolpack(ctx, pack); err != nil {
			panic(err)
		}
	}
}

// ExportGenesis exports the toolpacks.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs := types.DefaultGenesis()
	gs.Toolpacks = k.GetAllToolpacks(ctx)
	return gs
}
