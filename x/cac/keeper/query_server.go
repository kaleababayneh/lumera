package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"

	"github.com/LumeraProtocol/lumera/x/cac/types"
)

type queryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

// NewQueryServerImpl returns an implementation of the CAC Query service.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

func (s *queryServer) requireKeeper() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, fmt.Errorf("cac keeper not initialized")
	}
	return s.keeper, nil
}

// GetCacheEntry returns a specific cache entry by content hash.
func (s *queryServer) GetCacheEntry(goCtx context.Context, req *types.QueryGetCacheEntryRequest) (*types.QueryGetCacheEntryResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}

	contentHash, err := validateCACMsgServerID("content_hash", req.ContentHash)
	if err != nil {
		return nil, err
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	entry, found, err := keeper.GetEntry(sdkCtx, contentHash)
	if err != nil {
		// If entry expired, still return it but mark as not found
		if errors.Is(err, types.ErrEntryExpired) {
			clonedEntry, cloneErr := cloneCacheEntry(entry)
			if cloneErr != nil {
				return nil, cloneErr
			}
			return &types.QueryGetCacheEntryResponse{
				Entry: clonedEntry,
				Found: false,
			}, nil
		}
		return nil, fmt.Errorf("failed to get cache entry: %w", err)
	}
	clonedEntry, err := cloneCacheEntry(entry)
	if err != nil {
		return nil, err
	}

	return &types.QueryGetCacheEntryResponse{
		Entry: clonedEntry,
		Found: found,
	}, nil
}

// LookupByRequest looks up cache entries by request hash.
func (s *queryServer) LookupByRequest(goCtx context.Context, req *types.QueryLookupByRequestRequest) (*types.QueryLookupByRequestResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}

	requestHash, err := validateCACMsgServerID("request_hash", req.RequestHash)
	if err != nil {
		return nil, err
	}

	toolID := req.ToolId
	if toolID != "" {
		toolID, err = validateCACMsgServerID("tool_id", req.ToolId)
		if err != nil {
			return nil, err
		}
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	entries, err := keeper.LookupByRequest(sdkCtx, requestHash, toolID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup by request: %w", err)
	}
	clonedEntries, err := cloneCacheEntries(entries)
	if err != nil {
		return nil, err
	}

	return &types.QueryLookupByRequestResponse{
		Entries: clonedEntries,
	}, nil
}

// GetCacheStats returns cache performance statistics.
func (s *queryServer) GetCacheStats(goCtx context.Context, req *types.QueryGetCacheStatsRequest) (*types.QueryGetCacheStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	stats, err := keeper.GetStats(sdkCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache stats: %w", err)
	}
	clonedStats, err := cloneCacheStats(stats)
	if err != nil {
		return nil, err
	}

	return &types.QueryGetCacheStatsResponse{
		Stats: clonedStats,
	}, nil
}

// ListToolEntries lists all cache entries for a specific tool.
func (s *queryServer) ListToolEntries(goCtx context.Context, req *types.QueryListToolEntriesRequest) (*types.QueryListToolEntriesResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}

	toolID, err := validateCACMsgServerID("tool_id", req.ToolId)
	if err != nil {
		return nil, err
	}

	// Default limits
	limit := req.Limit
	if limit == 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := req.Offset

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	var entries []*types.CacheEntry
	var visibleCount uint64

	// Use ToolIndex for efficient lookup
	rng := collections.NewPrefixedPairRange[string, string](toolID)
	err = keeper.state.ToolIndex.Walk(sdkCtx, rng, func(key collections.Pair[string, string]) (bool, error) {
		contentHash := key.K2()

		// Load entry
		entry, found, err := keeper.GetEntry(sdkCtx, contentHash)
		if err != nil {
			// Skip errors/expired during list
			return false, nil
		}
		if !found || entry == nil {
			return false, nil
		}

		// Apply pagination over visible entries only; expired entries can remain
		// in the tool index until cleanup but should not affect query pages.
		if visibleCount >= offset && uint64(len(entries)) < limit {
			clonedEntry, err := cloneCacheEntry(entry)
			if err != nil {
				return false, err
			}
			entries = append(entries, clonedEntry)
		}

		visibleCount++
		return false, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list tool entries: %w", err)
	}

	return &types.QueryListToolEntriesResponse{
		Entries:    entries,
		TotalCount: visibleCount,
	}, nil
}

// GetParams returns the module parameters.
func (s *queryServer) GetParams(goCtx context.Context, req *types.QueryGetParamsRequest) (*types.QueryGetParamsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	params := keeper.GetParams(sdkCtx)
	clonedParams, err := cloneCacheParams(params)
	if err != nil {
		return nil, err
	}

	return &types.QueryGetParamsResponse{
		Params: clonedParams,
	}, nil
}

// GetToolStats returns per-tool CAC hit/miss counters and origin-hit detail.
func (s *queryServer) GetToolStats(goCtx context.Context, req *types.QueryGetToolStatsRequest) (*types.QueryGetToolStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("empty request")
	}

	toolID, err := validateCACMsgServerID("tool_id", req.ToolId)
	if err != nil {
		return nil, err
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	stats, err := keeper.GetToolStats(sdkCtx, toolID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool stats: %w", err)
	}

	return &types.QueryGetToolStatsResponse{
		Stats: &types.ToolCacheStats{
			ToolId:     stats.ToolID,
			HitCount:   stats.HitCount,
			MissCount:  stats.MissCount,
			HitRate:    stats.HitRate,
			OriginHits: cloneOriginHits(stats.OriginHits),
		},
	}, nil
}

// deepCopyProto copies src into dst via a gogo marshal/unmarshal round-trip.
// proto.Clone panics on gogo customtype fields (math.Int/LegacyDec inside
// sdk.Coin: "merger not found for type:big.Word"), so it cannot be used here.
func deepCopyProto(src, dst proto.Message) bool {
	raw, err := proto.Marshal(src)
	if err != nil {
		return false
	}
	if err := proto.Unmarshal(raw, dst); err != nil {
		return false
	}
	return true
}

func cloneCacheEntry(entry *types.CacheEntry) (*types.CacheEntry, error) {
	if entry == nil {
		return nil, nil
	}
	cloned := &types.CacheEntry{}
	if !deepCopyProto(entry, cloned) {
		return nil, fmt.Errorf("unable to clone cache entry %s", entry.ContentHash)
	}
	return cloned, nil
}

func cloneCacheEntries(entries []*types.CacheEntry) ([]*types.CacheEntry, error) {
	if entries == nil {
		return nil, nil
	}
	cloned := make([]*types.CacheEntry, 0, len(entries))
	for _, entry := range entries {
		clonedEntry, err := cloneCacheEntry(entry)
		if err != nil {
			return nil, err
		}
		cloned = append(cloned, clonedEntry)
	}
	return cloned, nil
}

func cloneCacheStats(stats *types.CacheStats) (*types.CacheStats, error) {
	if stats == nil {
		return nil, nil
	}
	cloned := &types.CacheStats{}
	if !deepCopyProto(stats, cloned) {
		return nil, fmt.Errorf("unable to clone cache stats")
	}
	return cloned, nil
}

func cloneCacheParams(params *types.CacheParams) (*types.CacheParams, error) {
	if params == nil {
		return nil, nil
	}
	cloned := &types.CacheParams{}
	if !deepCopyProto(params, cloned) {
		return nil, fmt.Errorf("unable to clone cache params")
	}
	return cloned, nil
}

func cloneOriginHits(originHits map[string]uint64) map[string]uint64 {
	if originHits == nil {
		return nil
	}
	cloned := make(map[string]uint64, len(originHits))
	for toolID, hits := range originHits {
		cloned[toolID] = hits
	}
	return cloned
}
