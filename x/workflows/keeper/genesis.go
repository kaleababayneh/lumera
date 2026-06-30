package keeper

import (
	"context"
	"fmt"
	"sort"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

// InitGenesis restores module state from a genesis export.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	if gs == nil {
		gs = types.DefaultGenesis()
	}
	if err := gs.Validate(); err != nil {
		return err
	}
	if err := k.SetParams(ctx, gs.Params); err != nil {
		return fmt.Errorf("set params: %w", err)
	}
	for _, workflow := range gs.Workflows {
		if err := k.PutWorkflow(ctx, workflow); err != nil {
			return fmt.Errorf("store workflow %s/%s: %w", workflow.WorkflowID, workflow.Version, err)
		}
	}
	for _, bond := range gs.AuthorBonds {
		if err := k.PutAuthorBond(ctx, bond); err != nil {
			return fmt.Errorf("store author bond %s: %w", bond.AuthorAddress, err)
		}
	}
	for _, quote := range gs.BundleQuotes {
		if err := k.PutBundleQuote(ctx, quote); err != nil {
			return fmt.Errorf("store bundle quote %s: %w", quote.BundleID, err)
		}
	}
	k.EmitLifecycleEvent(ctx, "init_genesis")
	return nil
}

// ExportGenesis exports the full module state for a genesis snapshot.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	gs := types.DefaultGenesis()
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	gs.Params = params

	if err := k.state.Workflows.Walk(ctx, nil, func(_ string, workflow *types.WorkflowRecord) (bool, error) {
		if workflow != nil {
			gs.Workflows = append(gs.Workflows, workflow)
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterate workflows: %w", err)
	}
	sort.Slice(gs.Workflows, func(i, j int) bool {
		left, _ := types.WorkflowKey(gs.Workflows[i].WorkflowID, gs.Workflows[i].Version)
		right, _ := types.WorkflowKey(gs.Workflows[j].WorkflowID, gs.Workflows[j].Version)
		return left < right
	})

	if err := k.state.AuthorBonds.Walk(ctx, nil, func(_ string, bond *types.AuthorBondRecord) (bool, error) {
		if bond != nil {
			gs.AuthorBonds = append(gs.AuthorBonds, bond)
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterate author bonds: %w", err)
	}
	sort.Slice(gs.AuthorBonds, func(i, j int) bool {
		return gs.AuthorBonds[i].AuthorAddress < gs.AuthorBonds[j].AuthorAddress
	})

	if err := k.state.BundleQuotes.Walk(ctx, nil, func(_ string, quote *types.BundleQuoteRecord) (bool, error) {
		if quote != nil {
			gs.BundleQuotes = append(gs.BundleQuotes, quote)
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterate bundle quotes: %w", err)
	}
	sort.Slice(gs.BundleQuotes, func(i, j int) bool {
		return gs.BundleQuotes[i].BundleID < gs.BundleQuotes[j].BundleID
	})

	return gs, nil
}
