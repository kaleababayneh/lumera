package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

// InitGenesis initializes the registry state from genesis. This slice restores
// params + tool cards + bond records; the remaining genesis collections
// (receipts, challenges, ...) are restored as their keeper slices land.
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
	for _, bond := range gs.BondRecords {
		if bond == nil {
			continue
		}
		if err := k.SetBondRecord(ctx, bond); err != nil {
			panic(err)
		}
	}
	for _, receipt := range gs.Receipts {
		if receipt == nil {
			continue
		}
		if err := k.SetUsageReceipt(ctx, receipt); err != nil {
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
	gs.BondRecords = k.GetAllBonds(ctx)
	gs.Receipts = k.GetAllReceipts(ctx)
	return gs
}
