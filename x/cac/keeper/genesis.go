package keeper

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"cosmossdk.io/collections"

	"github.com/LumeraProtocol/lumera/x/cac/types"
)

// InitGenesis restores module state from a genesis export.
func (k Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	if gs == nil {
		return nil
	}
	if err := gs.Validate(); err != nil {
		return err
	}
	if gs.Params != nil {
		if err := k.SetParams(ctx, gs.Params); err != nil {
			return fmt.Errorf("set params: %w", err)
		}
	}
	if gs.LastDecayTick != 0 {
		if err := k.state.LastDecayTick.Set(ctx, gs.LastDecayTick); err != nil {
			return fmt.Errorf("set last decay tick: %w", err)
		}
	}
	for i, entry := range gs.Entries {
		if entry == nil {
			continue
		}
		if entry.ContentHash == "" {
			return fmt.Errorf("entry %d missing content_hash", i)
		}
		// Store the entry itself.
		if err := k.state.Entries.Set(ctx, entry.ContentHash, entry); err != nil {
			return fmt.Errorf("store entry %s: %w", entry.ContentHash, err)
		}
		// Rebuild request index.
		if entry.RequestHash != "" {
			if err := k.state.RequestIndex.Set(ctx, entry.RequestHash, entry.ContentHash); err != nil {
				return fmt.Errorf("index request %s: %w", entry.RequestHash, err)
			}
			if err := k.state.ContentRequestIndex.Set(ctx, collections.Join(entry.ContentHash, entry.RequestHash)); err != nil {
				return fmt.Errorf("index content-request %s: %w", entry.ContentHash, err)
			}
		}
		// Rebuild tool index.
		if entry.ToolId != "" {
			if err := k.state.ToolIndex.Set(ctx, collections.Join(entry.ToolId, entry.ContentHash)); err != nil {
				return fmt.Errorf("index tool %s: %w", entry.ToolId, err)
			}
		}
		// Rebuild expiry index.
		if !entry.ExpiresAt.IsZero() {
			if err := k.state.ExpiryIndex.Set(ctx, collections.Join(entry.ExpiresAt, entry.ContentHash)); err != nil {
				return fmt.Errorf("index expiry for %s: %w", entry.ContentHash, err)
			}
		}
		// Update tier capacity tracking.
		if entry.ContentSizeBytes > 0 {
			if err := k.updateTierUsage(ctx, entry.Tier, int64(entry.ContentSizeBytes)); err != nil {
				return fmt.Errorf("update tier usage for %s: %w", entry.ContentHash, err)
			}
		}
		if gs.EntryHeights != nil {
			if height, ok := gs.EntryHeights[entry.ContentHash]; ok {
				if err := k.state.EntryHeights.Set(ctx, entry.ContentHash, height); err != nil {
					return fmt.Errorf("set entry height for %s: %w", entry.ContentHash, err)
				}
			}
		}
	}

	for _, stat := range gs.ToolStats {
		if stat == nil || stat.ToolId == "" {
			continue
		}
		if stat.HitCount > 0 {
			if err := k.state.ToolHits.Set(ctx, stat.ToolId, stat.HitCount); err != nil {
				return fmt.Errorf("set tool hits for %s: %w", stat.ToolId, err)
			}
		}
		if stat.MissCount > 0 {
			if err := k.state.ToolMisses.Set(ctx, stat.ToolId, stat.MissCount); err != nil {
				return fmt.Errorf("set tool misses for %s: %w", stat.ToolId, err)
			}
		}
		for originToolID, hits := range stat.OriginHits {
			if hits == 0 {
				continue
			}
			if err := k.state.OriginHits.Set(ctx, collections.Join(stat.ToolId, originToolID), hits); err != nil {
				return fmt.Errorf("set origin hits for %s/%s: %w", stat.ToolId, originToolID, err)
			}
		}
	}
	return nil
}

// ExportGenesis exports the full module state for a genesis snapshot.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	gs := types.DefaultGenesisState()
	gs.Params = k.GetParams(ctx)

	iter, err := k.state.Entries.Iterate(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("iterate entries: %w", err)
	}
	defer func() {
		_ = iter.Close()
	}()

	for ; iter.Valid(); iter.Next() {
		entry, err := iter.Value()
		if err != nil {
			return nil, fmt.Errorf("read entry: %w", err)
		}
		if entry != nil {
			gs.Entries = append(gs.Entries, entry)
		}
	}

	if err := k.state.EntryHeights.Walk(ctx, nil, func(contentHash string, height int64) (bool, error) {
		gs.EntryHeights[contentHash] = height
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterate entry heights: %w", err)
	}

	toolIDs := map[string]struct{}{}
	if err := k.state.ToolHits.Walk(ctx, nil, func(toolID string, _ uint64) (bool, error) {
		toolIDs[toolID] = struct{}{}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterate tool hits: %w", err)
	}
	if err := k.state.ToolMisses.Walk(ctx, nil, func(toolID string, _ uint64) (bool, error) {
		toolIDs[toolID] = struct{}{}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterate tool misses: %w", err)
	}
	sortedToolIDs := make([]string, 0, len(toolIDs))
	for toolID := range toolIDs {
		sortedToolIDs = append(sortedToolIDs, toolID)
	}
	sort.Strings(sortedToolIDs)
	for _, toolID := range sortedToolIDs {
		stats, err := k.GetToolStats(ctx, toolID)
		if err != nil {
			return nil, fmt.Errorf("get tool stats for %s: %w", toolID, err)
		}
		gs.ToolStats = append(gs.ToolStats, &types.ToolCacheStats{
			ToolId:     stats.ToolID,
			HitCount:   stats.HitCount,
			MissCount:  stats.MissCount,
			HitRate:    stats.HitRate,
			OriginHits: stats.OriginHits,
		})
	}

	lastDecayTick, err := k.state.LastDecayTick.Get(ctx)
	if err == nil {
		gs.LastDecayTick = lastDecayTick
	} else if !errors.Is(err, collections.ErrNotFound) {
		return nil, fmt.Errorf("load last decay tick: %w", err)
	}

	return gs, nil
}
