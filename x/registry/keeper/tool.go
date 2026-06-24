package keeper

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

func wrapContext(ctx sdk.Context) context.Context { return ctx }

// SetToolCard stores or updates a tool card.
func (k Keeper) SetToolCard(ctx sdk.Context, tool *types.ToolCard) error {
	if tool == nil {
		return fmt.Errorf("SetToolCard: tool card cannot be nil")
	}
	return k.toolCards.Set(wrapContext(ctx), tool.ToolId, tool)
}

// GetToolCard retrieves a tool card by identifier.
func (k Keeper) GetToolCard(ctx sdk.Context, toolID string) (*types.ToolCard, bool) {
	tool, err := k.toolCards.Get(wrapContext(ctx), toolID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(ctx).Error("failed to load tool card", "tool_id", toolID, "error", err)
		return nil, false
	}
	if tool == nil {
		return nil, false
	}
	return tool, true
}

// HasTool checks whether a tool exists in state.
func (k Keeper) HasTool(ctx sdk.Context, toolID string) bool {
	has, err := k.toolCards.Has(wrapContext(ctx), toolID)
	if err != nil {
		k.Logger(ctx).Error("failed to check tool existence", "tool", toolID, "error", err)
		return false
	}
	return has
}

// RemoveToolCard deletes a stored tool card.
func (k Keeper) RemoveToolCard(ctx sdk.Context, toolID string) error {
	if err := k.toolCards.Remove(wrapContext(ctx), toolID); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("remove tool card %s: %w", toolID, err)
	}
	return nil
}

// GetAllTools returns all tool cards sorted by identifier for deterministic iteration.
func (k Keeper) GetAllTools(ctx sdk.Context) []*types.ToolCard {
	tools := make([]*types.ToolCard, 0)
	if err := k.toolCards.Walk(wrapContext(ctx), nil, func(_ string, tool *types.ToolCard) (bool, error) {
		if tool != nil {
			tools = append(tools, tool)
		}
		return false, nil
	}); err != nil {
		k.Logger(ctx).Error("failed to iterate tool cards", "error", err)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].GetToolId() < tools[j].GetToolId() })
	return tools
}

// IterateTools iterates over all tools and applies a callback.
func (k Keeper) IterateTools(ctx sdk.Context, cb func(tool *types.ToolCard) (stop bool)) error {
	return k.toolCards.Walk(wrapContext(ctx), nil, func(_ string, tool *types.ToolCard) (bool, error) {
		return cb(tool), nil
	})
}

// GetToolCategories returns the categories for a tool, or (nil, false) if not found.
func (k Keeper) GetToolCategories(ctx context.Context, toolID string) ([]string, bool) {
	tool, found := k.GetToolCard(sdk.UnwrapSDKContext(ctx), toolID)
	if !found || tool == nil {
		return nil, false
	}
	return tool.Categories, true
}

// GetToolPublisher retrieves the publisher (owner) address for a tool. This is
// the method the credits settlement path consumes to pay tool publishers.
func (k Keeper) GetToolPublisher(ctx context.Context, toolID string) (sdk.AccAddress, error) {
	tool, found := k.GetToolCard(sdk.UnwrapSDKContext(ctx), toolID)
	if !found || tool == nil {
		return nil, types.ErrToolNotFound
	}
	addr, err := sdk.AccAddressFromBech32(tool.Owner)
	if err != nil {
		return nil, err
	}
	return addr, nil
}

// IsToolRegistered reports whether a tool exists. Consumed by the incentives
// module (reputation engine) to validate evaluation requests.
func (k Keeper) IsToolRegistered(ctx context.Context, toolID string) (bool, error) {
	return k.HasTool(sdk.UnwrapSDKContext(ctx), toolID), nil
}

// IsDeterministicTool reports whether a tool produces deterministic output,
// read from its registered CachePolicy.Deterministic flag. Consumed by the cac
// module to decide whether a tool's results are safe to content-address and
// serve from cache. A tool with no cache policy is treated as non-deterministic.
func (k Keeper) IsDeterministicTool(ctx context.Context, toolID string) (bool, error) {
	tool, found := k.GetToolCard(sdk.UnwrapSDKContext(ctx), toolID)
	if !found || tool == nil {
		return false, types.ErrToolNotFound
	}
	if tool.Cache == nil {
		return false, nil
	}
	return tool.Cache.Deterministic, nil
}
