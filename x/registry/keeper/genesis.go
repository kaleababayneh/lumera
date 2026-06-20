package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

// InitGenesis initializes the registry state from genesis. This slice restores
// params + tool cards; the remaining genesis collections (bonds, receipts,
// challenges, ...) are restored as their keeper slices land.
func (k Keeper) InitGenesis(ctx sdk.Context, gs *types.GenesisState) {
	if gs == nil {
		gs = types.DefaultGenesis()
	}
	params := gs.Params
	if params == nil {
		params = types.DefaultParams()
	}
	if err := k.SetParams(ctx, params); err != nil {
		panic(err)
	}
	for _, tool := range gs.ToolCards {
		if tool == nil {
			continue
		}
		if err := k.SetToolCard(ctx, tool); err != nil {
			panic(err)
		}
	}
}

// ExportGenesis exports the registry state. This slice exports params + tool
// cards; other collections export empty until their slices land.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs := types.DefaultGenesis()
	p := k.GetParams(ctx)
	gs.Params = &p
	gs.ToolCards = k.GetAllTools(ctx)
	return gs
}
