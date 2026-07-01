package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	"github.com/LumeraProtocol/lumera/x/router/types"
)

// CAC (Content-Addressable Cache) configuration constants
const (
	// DefaultCacheHitCostBPS is the cost percentage for cache hits in basis points.
	// 2000 BPS = 20%, meaning cache hits cost 20% of the original invocation cost.
	DefaultCacheHitCostBPS = 2000

	// DefaultCacheRoyaltyBPS is the royalty percentage paid to origin publishers on cache hits.
	// 500 BPS = 5% of the cache hit cost goes to the original publisher.
	DefaultCacheRoyaltyBPS = 500

	// DefaultCacheTTL is the default time-to-live for cache entries.
	DefaultCacheTTL = 24 * time.Hour
)

// ComputeCacheKey generates a deterministic cache key for a tool invocation.
// This is the legacy version without tool version - use ComputeCacheKeyVersioned for version-aware caching.
func (k Keeper) ComputeCacheKey(toolID string, args map[string]string) string {
	return k.ComputeCacheKeyVersioned(toolID, "", args)
}

// ComputeCacheKeyVersioned generates a deterministic cache key that includes the tool version.
// Including the version ensures cache entries are automatically invalidated when a tool is updated.
// The key is computed as SHA256(toolID + version + canonically-sorted args).
func (k Keeper) ComputeCacheKeyVersioned(toolID, version string, args map[string]string) string {
	// Build data structure for deterministic hashing
	data := map[string]interface{}{
		"tool_id": toolID,
		"version": version,
		"args":    args,
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		k.logger.Error("failed to marshal cache key", "tool_id", toolID, "error", err)
		jsonBytes = []byte(toolID)
	}
	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:])
}

// ComputeCacheHitCost calculates the reduced cost for a cache hit.
// Cache hits cost a fraction of the original invocation cost (default 20%).
// Returns (cacheHitCost, royaltyAmount, costSavings).
func (k Keeper) ComputeCacheHitCost(originalCost decimal.Decimal, hitCostBPS, royaltyBPS uint32) (cacheHitCost, royalty, savings decimal.Decimal) {
	if hitCostBPS == 0 {
		hitCostBPS = DefaultCacheHitCostBPS
	}
	if royaltyBPS == 0 {
		royaltyBPS = DefaultCacheRoyaltyBPS
	}

	// Cache hit cost = original * (hitCostBPS / 10000)
	cacheHitCost = originalCost.Mul(decimal.NewFromInt(int64(hitCostBPS))).Div(decimal.NewFromInt(10000))

	// Royalty = cacheHitCost * (royaltyBPS / 10000)
	royalty = cacheHitCost.Mul(decimal.NewFromInt(int64(royaltyBPS))).Div(decimal.NewFromInt(10000))

	// Savings = original - cacheHitCost
	savings = originalCost.Sub(cacheHitCost)

	return cacheHitCost, royalty, savings
}

// GetCachedResult retrieves a cached result if available
func (k Keeper) GetCachedResult(ctx context.Context, toolID string, args map[string]string) (*types.CacheEntry, bool) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheKey := k.ComputeCacheKey(toolID, args)

	if k.cacheKeeper != nil {
		// Try to get from cache keeper first
		if data, found := k.cacheKeeper.Get(ctx, cacheKey); found {
			var entry types.CacheEntry
			if err := json.Unmarshal(data, &entry); err == nil {
				// Update hit count and time
				entry.HitCount++
				entry.SetLastHitAt(sdkCtx.BlockTime())

				// Record cache hit for royalties
				if entry.OriginToolId != "" && entry.OriginToolId != toolID {
					if err := k.cacheKeeper.RecordHit(ctx, toolID, entry.OriginToolId); err != nil {
						k.Logger(sdkCtx).Error("failed to record cache hit", "tool", toolID, "origin", entry.OriginToolId, "error", err)
					}
				}

				return &entry, true
			}
		}
	}

	// Fallback to local storage
	entry, err := k.state.CacheEntries.Get(ctx, cacheKey)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		k.Logger(sdkCtx).Error("failed to load cache entry", "key", cacheKey, "error", err)
		return nil, false
	}

	// Check if expired
	if sdkCtx.BlockTime().After(entry.ExpiresAtTime()) {
		// Clean up expired entry
		if err := k.state.CacheEntries.Remove(ctx, cacheKey); err != nil && !errors.Is(err, collections.ErrNotFound) {
			k.Logger(sdkCtx).Error("failed to remove expired cache entry", "key", cacheKey, "error", err)
		}
		return nil, false
	}

	// Update hit metrics
	entry.HitCount++
	entry.SetLastHitAt(sdkCtx.BlockTime())
	if err := k.SaveCacheEntry(ctx, entry); err != nil {
		k.Logger(sdkCtx).Error("failed to persist cache entry", "key", entry.CacheKey, "error", err)
	}

	return entry, true
}

// SaveCacheEntry stores a tool invocation result in cache
func (k Keeper) SaveCacheEntry(ctx context.Context, entry *types.CacheEntry) error {
	if entry == nil {
		return fmt.Errorf("cache entry is nil")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Set default expiration if not set
	if entry.ExpiresAtTime().IsZero() {
		entry.SetExpiresAt(sdkCtx.BlockTime().Add(24 * time.Hour)) // 24 hour default TTL
	}

	if err := k.state.CacheEntries.Set(ctx, entry.CacheKey, entry); err != nil {
		return fmt.Errorf("failed to persist cache entry: %w", err)
	}

	// Also store in cache keeper if available
	if k.cacheKeeper != nil {
		jsonData, err := json.Marshal(entry)
		if err != nil {
			k.Logger(sdkCtx).Error("failed to marshal cache entry for keeper", "key", entry.CacheKey, "error", err)
		} else {
			ttl := entry.ExpiresAtTime().Sub(sdkCtx.BlockTime())
			if err := k.cacheKeeper.Set(ctx, entry.CacheKey, jsonData, ttl); err != nil {
				k.Logger(sdkCtx).Error("failed to propagate cache entry", "key", entry.CacheKey, "error", err)
			}
		}
	}

	return nil
}

// CacheResult stores a tool invocation result for future use
func (k Keeper) CacheResult(ctx context.Context, toolID string, args map[string]string, response string, originToolID string) error {
	cacheKey := k.ComputeCacheKey(toolID, args)
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	entry := types.NewCacheEntry(cacheKey, toolID)
	entry.OriginToolId = originToolID
	entry.RequestHash = cacheKey // Using cache key as hash for simplicity
	entry.Response = response
	entry.SetCreatedAt(sdkCtx.BlockTime())
	entry.SetExpiresAt(sdkCtx.BlockTime().Add(24 * time.Hour))
	entry.SetLastHitAt(sdkCtx.BlockTime())

	return k.SaveCacheEntry(ctx, entry)
}

// GetCacheStats returns cache statistics for a tool
func (k Keeper) GetCacheStats(ctx context.Context, toolID string) (hits uint64, misses uint64, ratio float64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	stats, err := k.state.CacheStats.Get(ctx, toolID)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			k.Logger(sdkCtx).Error("failed to load cache stats", "tool", toolID, "error", err)
		}
		return 0, 0, 0
	}

	hits = stats.Hits
	misses = stats.Misses
	if total := hits + misses; total > 0 {
		ratio = float64(hits) / float64(total)
	}

	return hits, misses, ratio
}

// RecordCacheHit records a cache hit for metrics
func (k Keeper) RecordCacheHit(ctx context.Context, toolID string) error {
	return k.updateCacheMetrics(ctx, toolID, true)
}

// RecordCacheMiss records a cache miss for metrics
func (k Keeper) RecordCacheMiss(ctx context.Context, toolID string) error {
	return k.updateCacheMetrics(ctx, toolID, false)
}

// updateCacheMetrics updates cache hit/miss statistics
func (k Keeper) updateCacheMetrics(ctx context.Context, toolID string, isHit bool) error {
	stats, err := k.state.CacheStats.Get(ctx, toolID)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("load cache stats for %s: %w", toolID, err)
		}
		stats = &types.CacheStats{}
	}

	if isHit {
		stats.Hits++
	} else {
		stats.Misses++
	}

	return k.state.CacheStats.Set(ctx, toolID, stats)
}

// CleanupExpiredCache removes expired cache entries
func (k Keeper) CleanupExpiredCache(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	keysToDelete := []string{}

	err := k.state.CacheEntries.Walk(ctx, nil, func(key string, entry *types.CacheEntry) (bool, error) {
		if sdkCtx.BlockTime().After(entry.ExpiresAtTime()) {
			keysToDelete = append(keysToDelete, key)
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, key := range keysToDelete {
		if err := k.state.CacheEntries.Remove(ctx, key); err != nil && !errors.Is(err, collections.ErrNotFound) {
			k.Logger(sdkCtx).Error("failed to remove expired cache entry", "key", key, "error", err)
		}
	}

	return nil
}

// GetCachedResultVersioned retrieves a cached result for a specific tool version.
// Returns the cache entry, whether it was found, and the calculated cache hit cost.
func (k Keeper) GetCachedResultVersioned(ctx context.Context, toolID, version string, args map[string]string) (*types.CacheEntry, bool, decimal.Decimal) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheKey := k.ComputeCacheKeyVersioned(toolID, version, args)
	entry, err := k.state.CacheEntries.Get(ctx, cacheKey)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false, decimal.Zero
		}
		k.Logger(sdkCtx).Error("failed to load cache entry", "key", cacheKey, "error", err)
		return nil, false, decimal.Zero
	}

	// Check if expired
	if sdkCtx.BlockTime().After(entry.ExpiresAtTime()) {
		if err := k.state.CacheEntries.Remove(ctx, cacheKey); err != nil && !errors.Is(err, collections.ErrNotFound) {
			k.Logger(sdkCtx).Error("failed to remove expired cache entry", "key", cacheKey, "error", err)
		}
		return nil, false, decimal.Zero
	}

	// Verify version matches (extra safety check)
	if version != "" && entry.ToolVersion != "" && entry.ToolVersion != version {
		return nil, false, decimal.Zero
	}

	// Calculate cache hit cost. Gate NewFromString with moneyguard —
	// entry.OriginalCost comes from persisted state and if a future
	// writer path ever persists an absurd-exponent value (migration
	// boundary, upstream bug, adversarial state injection),
	// ComputeCacheHitCost's Mul/Div/Sub on the parsed decimal would
	// expand big.Int to millions of digits and hang the quote-path
	// inside consensus state-transition logic. Same DoS class as
	// 8438b6354 (grpc_router MaxCost / deriveToolCost) and the
	// sibling strategy/execute BuildGroupReceipt sweep c1ec4b822.
	var cacheHitCost decimal.Decimal
	if entry.OriginalCost != "" {
		originalCost, err := decimal.NewFromString(entry.OriginalCost)
		if err == nil && moneyguard.IsSafeExponent(originalCost) {
			cacheHitCost, _, _ = k.ComputeCacheHitCost(originalCost, 0, 0)
		}
	}

	// Update hit metrics
	entry.HitCount++
	entry.SetLastHitAt(sdkCtx.BlockTime())
	if err := k.SaveCacheEntry(ctx, entry); err != nil {
		k.Logger(sdkCtx).Error("failed to persist cache entry", "key", entry.CacheKey, "error", err)
	}

	// Record hit for metrics
	if err := k.RecordCacheHit(ctx, toolID); err != nil {
		k.Logger(sdkCtx).Error("failed to record cache hit metrics", "tool", toolID, "error", err)
	}

	return entry, true, cacheHitCost
}

// CacheResultWithCost stores a tool invocation result with version and original cost for CAC.
// This enables cost reduction calculations and version-based invalidation on future cache hits.
func (k Keeper) CacheResultWithCost(ctx context.Context, toolID, version string, args map[string]string,
	response string, originToolID string, originalCost decimal.Decimal) error {

	cacheKey := k.ComputeCacheKeyVersioned(toolID, version, args)
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	entry := types.NewCacheEntry(cacheKey, toolID)
	entry.ToolVersion = version
	entry.OriginToolId = originToolID
	entry.RequestHash = cacheKey
	entry.Response = response
	entry.SetOriginalCostDecimal(originalCost)
	entry.SetCreatedAt(sdkCtx.BlockTime())
	entry.SetExpiresAt(sdkCtx.BlockTime().Add(DefaultCacheTTL))
	entry.SetLastHitAt(sdkCtx.BlockTime())

	return k.SaveCacheEntry(ctx, entry)
}

// InvalidateCacheForTool removes all cache entries for a specific tool.
// This should be called when a tool's version changes to ensure stale cached results
// are not returned. Returns the number of entries invalidated.
func (k Keeper) InvalidateCacheForTool(ctx context.Context, toolID string) (int, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	keysToDelete := []string{}

	err := k.state.CacheEntries.Walk(ctx, nil, func(key string, entry *types.CacheEntry) (bool, error) {
		if entry.ToolId == toolID {
			keysToDelete = append(keysToDelete, key)
		}
		return false, nil
	})
	if err != nil {
		return 0, err
	}

	var firstErr error
	for _, key := range keysToDelete {
		if err := k.state.CacheEntries.Remove(ctx, key); err != nil && !errors.Is(err, collections.ErrNotFound) {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove cache entry %s: %w", key, err)
			}
		}
	}

	if len(keysToDelete) > 0 {
		k.Logger(sdkCtx).Info("invalidated cache entries for tool",
			"tool_id", toolID,
			"count", len(keysToDelete))
	}

	return len(keysToDelete), firstErr
}

// InvalidateCacheForToolVersion removes cache entries for a specific tool version.
// More precise than InvalidateCacheForTool - only removes entries matching the exact version.
func (k Keeper) InvalidateCacheForToolVersion(ctx context.Context, toolID, version string) (int, error) {
	keysToDelete := []string{}

	err := k.state.CacheEntries.Walk(ctx, nil, func(key string, entry *types.CacheEntry) (bool, error) {
		if entry.ToolId == toolID && entry.ToolVersion == version {
			keysToDelete = append(keysToDelete, key)
		}
		return false, nil
	})
	if err != nil {
		return 0, err
	}

	var firstErr error
	for _, key := range keysToDelete {
		if err := k.state.CacheEntries.Remove(ctx, key); err != nil && !errors.Is(err, collections.ErrNotFound) {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove cache entry %s: %w", key, err)
			}
		}
	}

	return len(keysToDelete), firstErr
}

// GetCacheSavingsForTool returns total cost savings from cache hits for a tool.
// This is calculated as sum of (original_cost - cache_hit_cost) across all cache hits.
func (k Keeper) GetCacheSavingsForTool(ctx context.Context, toolID string) (totalSavings decimal.Decimal, hitCount uint64) {
	totalSavings = decimal.Zero
	hitCount = 0

	err := k.state.CacheEntries.Walk(ctx, nil, func(_ string, entry *types.CacheEntry) (bool, error) {
		if entry.ToolId == toolID && entry.HitCount > 0 && entry.OriginalCost != "" {
			originalCost, err := decimal.NewFromString(entry.OriginalCost)
			if err != nil || !moneyguard.IsSafeExponent(originalCost) {
				// Skip entries with absurd-exponent costs — one
				// poisoned entry would otherwise hang the whole
				// aggregation (Walk iterates every entry).
				return false, nil
			}
			_, _, savings := k.ComputeCacheHitCost(originalCost, 0, 0)
			totalSavings = totalSavings.Add(savings.Mul(decimal.NewFromInt(int64(entry.HitCount))))
			hitCount += entry.HitCount
		}
		return false, nil
	})
	if err != nil {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		k.Logger(sdkCtx).Error("failed to walk cache entries", "error", err)
	}

	return totalSavings, hitCount
}
