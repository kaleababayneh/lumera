// Package keeper provides the state management and business logic for the cac module.
package keeper

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"
	"lukechampine.com/blake3"

	"github.com/LumeraProtocol/lumera/x/cac/types"
)

func validateKeeperCacheID(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return "", fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	if len(value) > types.MaxCacheIDLen {
		return "", fmt.Errorf("%s length %d exceeds maximum %d", field, len(value), types.MaxCacheIDLen)
	}
	return value, nil
}

func validateOptionalKeeperCacheID(field, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return validateKeeperCacheID(field, value)
}

// State encapsulates the module collections state.
type State struct {
	Schema              collections.Schema
	Params              collections.Item[*types.CacheParams]
	Entries             collections.Map[string, *types.CacheEntry]                // content_hash -> CacheEntry
	RequestIndex        collections.Map[string, string]                           // request_hash -> content_hash
	ToolIndex           collections.KeySet[collections.Pair[string, string]]      // (tool_id, content_hash) index
	ContentRequestIndex collections.KeySet[collections.Pair[string, string]]      // (content_hash, request_hash) index for deduplication cleanup
	ExpiryIndex         collections.KeySet[collections.Pair[time.Time, string]]   // (expires_at, content_hash) index for TTL eviction
	Stats               collections.Item[*types.CacheStats]                       // Global cache statistics
	EntrySeq            collections.Sequence                                      // Sequence for cache entry IDs
	HitSeq              collections.Sequence                                      // Sequence for cache hit IDs
	TierCapacity        collections.Map[int32, uint64]                            // Tier -> Bytes Used
	ToolHits            collections.Map[string, uint64]                           // tool_id -> hits
	ToolMisses          collections.Map[string, uint64]                           // tool_id -> misses
	OriginHits          collections.Map[collections.Pair[string, string], uint64] // (tool_id, origin_tool_id) -> hits
	EntryHeights        collections.Map[string, int64]                            // content_hash -> created block height
	LastDecayTick       collections.Item[int64]                                   // latest upkeep tick height
}

// Keeper provides the module's state access layer.
type Keeper struct {
	cdc            codec.BinaryCodec
	storeService   corestore.KVStoreService
	bankKeeper     types.BankKeeper
	accountKeeper  types.AccountKeeper
	creditsKeeper  types.CreditsKeeper
	registryKeeper types.RegistryKeeper
	authority      string
	state          State
}

// NewKeeper constructs a Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
	creditsKeeper types.CreditsKeeper,
	registryKeeper types.RegistryKeeper,
	authority string,
) Keeper {
	if bankKeeper == nil {
		panic("cac keeper requires bank keeper")
	}
	if accountKeeper == nil {
		panic("cac keeper requires account keeper")
	}

	sb := collections.NewSchemaBuilder(storeService)
	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsPrefix),
			"params",
			collPtrValue[types.CacheParams](cdc),
		),
		Entries: collections.NewMap(
			sb,
			collections.NewPrefix(types.CacheEntriesPrefix),
			"entries",
			collections.StringKey,
			collPtrValue[types.CacheEntry](cdc),
		),
		RequestIndex: collections.NewMap(
			sb,
			collections.NewPrefix(types.RequestIndexPrefix),
			"request_index",
			collections.StringKey,
			collections.StringValue,
		),
		ToolIndex: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.ToolIndexPrefix),
			"tool_index",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		ContentRequestIndex: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.ContentRequestIndexPrefix),
			"content_request_index",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		ExpiryIndex: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.ExpiryIndexPrefix),
			"expiry_index",
			collections.PairKeyCodec(sdk.TimeKey, collections.StringKey),
		),
		Stats: collections.NewItem(
			sb,
			collections.NewPrefix(types.CacheStatsPrefix),
			"stats",
			collPtrValue[types.CacheStats](cdc),
		),
		EntrySeq: collections.NewSequence(
			sb,
			collections.NewPrefix(types.EntrySeqKeyPrefix),
			"entry_seq",
		),
		HitSeq: collections.NewSequence(
			sb,
			collections.NewPrefix(types.HitSeqKeyPrefix),
			"hit_seq",
		),
		TierCapacity: collections.NewMap(
			sb,
			collections.NewPrefix(types.TierCapacityPrefix),
			"tier_capacity",
			collections.Int32Key,
			collections.Uint64Value,
		),
		ToolHits: collections.NewMap(
			sb,
			collections.NewPrefix(types.ToolHitStatsPrefix),
			"tool_hits",
			collections.StringKey,
			collections.Uint64Value,
		),
		ToolMisses: collections.NewMap(
			sb,
			collections.NewPrefix(types.ToolMissStatsPrefix),
			"tool_misses",
			collections.StringKey,
			collections.Uint64Value,
		),
		OriginHits: collections.NewMap(
			sb,
			collections.NewPrefix(types.OriginHitStatsPrefix),
			"origin_hits",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			collections.Uint64Value,
		),
		EntryHeights: collections.NewMap(
			sb,
			collections.NewPrefix(types.EntryHeightPrefix),
			"entry_heights",
			collections.StringKey,
			collections.Int64Value,
		),
		LastDecayTick: collections.NewItem(
			sb,
			collections.NewPrefix(types.LastDecayTickPrefix),
			"last_decay_tick",
			collections.Int64Value,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build cac schema: %w", err))
	}
	state.Schema = schema

	return Keeper{
		cdc:            cdc,
		storeService:   storeService,
		bankKeeper:     bankKeeper,
		accountKeeper:  accountKeeper,
		creditsKeeper:  creditsKeeper,
		registryKeeper: registryKeeper,
		authority:      authority,
		state:          state,
	}
}

// Schema returns the underlying collections schema.
func (k Keeper) Schema() collections.Schema { return k.state.Schema }

// Logger returns a module-prefixed logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/cac")
}

// Authority exposes the address allowed to perform parameter changes.
func (k Keeper) Authority() string { return k.authority }

// ModuleAddress returns the module account address.
func (k Keeper) ModuleAddress() sdk.AccAddress {
	return k.accountKeeper.GetModuleAddress(types.ModuleAccountName)
}

// GetParams retrieves module parameters.
func (k Keeper) GetParams(ctx context.Context) *types.CacheParams {
	params, err := k.state.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return DefaultParams()
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		k.Logger(sdkCtx).Error("cac params load failed, returning defaults", "error", err)
		return DefaultParams()
	}
	if params == nil {
		return DefaultParams()
	}
	return params
}

// SetParams updates module parameters.
func (k Keeper) SetParams(ctx context.Context, params *types.CacheParams) error {
	if params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	return k.state.Params.Set(ctx, params)
}

// DefaultParams returns the default CAC module parameters.
func DefaultParams() *types.CacheParams {
	return &types.CacheParams{
		DefaultTtlSeconds:      types.DefaultTTLSeconds,
		MaxEntrySizeBytes:      types.DefaultMaxEntrySizeBytes,
		L1CapacityBytes:        types.DefaultL1CapacityBytes,
		L2CapacityBytes:        types.DefaultL2CapacityBytes,
		L3CapacityBytes:        types.DefaultL3CapacityBytes,
		L4CapacityBytes:        types.DefaultL4CapacityBytes,
		RoyaltyOriginBps:       types.DefaultRoyaltyOriginBPS,
		RoyaltyStorageBps:      types.DefaultRoyaltyStorageBPS,
		RoyaltyBandwidthBps:    types.DefaultRoyaltyBandwidthBPS,
		RoyaltyVerificationBps: types.DefaultRoyaltyVerificationBPS,
		RoyaltyGovernanceBps:   types.DefaultRoyaltyGovernanceBPS,
		RoyaltyDecayBps:        types.DefaultRoyaltyDecayBPS,
		BlocksPerDay:           types.DefaultBlocksPerDay,
		MinAccessForPromotion:  types.DefaultMinAccessForPromotion,
		EnableRoyalties:        types.DefaultEnableRoyalties,
	}
}

// StoreEntry stores a new cache entry.
func (k Keeper) StoreEntry(
	ctx context.Context,
	publisher sdk.AccAddress,
	toolID string,
	requestHash string,
	content []byte,
	ttlSeconds uint64,
	isDeterministic bool,
	royaltyEligible bool,
) (*types.CacheEntry, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(ctx)
	toolID, err := validateKeeperCacheID("tool_id", toolID)
	if err != nil {
		return nil, err
	}
	requestHash, err = validateKeeperCacheID("request_hash", requestHash)
	if err != nil {
		return nil, err
	}

	// Validate content size
	if uint64(len(content)) > params.MaxEntrySizeBytes {
		return nil, types.ErrContentTooLarge.Wrapf(
			"content size %d exceeds max %d",
			len(content), params.MaxEntrySizeBytes,
		)
	}
	if uint64(len(content)) > uint64(math.MaxInt64) {
		return nil, types.ErrContentTooLarge.Wrap("content size exceeds int64 limit")
	}

	// Compute content hash
	contentHash := computeContentHash(content)

	// StoreEntry is only called on cache-miss publication paths. Count it as a
	// miss even if the produced content deduplicates to an existing digest.
	if err := k.incrementToolCounter(ctx, k.state.ToolMisses, toolID); err != nil {
		return nil, fmt.Errorf("increment tool miss count: %w", err)
	}

	// Check if entry already exists
	if existing, err := k.state.Entries.Get(ctx, contentHash); err == nil && existing != nil {
		// Update access count instead of creating duplicate
		existing.AccessCount++
		existing.LastAccessAt = sdkCtx.BlockTime()
		if err := k.state.Entries.Set(ctx, contentHash, existing); err != nil {
			return nil, fmt.Errorf("failed to update existing entry: %w", err)
		}

		// Ensure indexes are updated even for existing content (deduplication)
		if err := k.state.RequestIndex.Set(ctx, requestHash, contentHash); err != nil {
			return nil, fmt.Errorf("failed to update request index: %w", err)
		}
		if err := k.state.ContentRequestIndex.Set(ctx, collections.Join(contentHash, requestHash)); err != nil {
			return nil, fmt.Errorf("failed to update content request index: %w", err)
		}
		if err := k.state.ToolIndex.Set(ctx, collections.Join(toolID, contentHash)); err != nil {
			return nil, fmt.Errorf("failed to update tool index: %w", err)
		}

		return existing, nil
	}

	// Determine TTL
	if ttlSeconds == 0 {
		ttlSeconds = params.DefaultTtlSeconds
	}
	if ttlSeconds > types.MaxTTLSeconds {
		return nil, types.ErrInvalidTTL.Wrapf("ttl_seconds exceeds maximum safe duration seconds (%d)", types.MaxTTLSeconds)
	}

	// Determine Tier based on capacity
	tier := types.CacheTier_CACHE_TIER_L2_SSD
	size := uint64(len(content))

	l2Usage := k.getTierUsage(ctx, types.CacheTier_CACHE_TIER_L2_SSD)
	if size > params.L2CapacityBytes || l2Usage > params.L2CapacityBytes-size {
		tier = types.CacheTier_CACHE_TIER_L3_HDD
		l3Usage := k.getTierUsage(ctx, types.CacheTier_CACHE_TIER_L3_HDD)
		if size > params.L3CapacityBytes || l3Usage > params.L3CapacityBytes-size {
			tier = types.CacheTier_CACHE_TIER_L4_COLD
			l4Usage := k.getTierUsage(ctx, types.CacheTier_CACHE_TIER_L4_COLD)
			// Enforce L4 capacity
			if size > params.L4CapacityBytes || l4Usage > params.L4CapacityBytes-size {
				return nil, types.ErrContentTooLarge.Wrap("all cache tiers full (including L4)")
			}
		}
	}

	// Update usage
	if err := k.updateTierUsage(ctx, tier, int64(size)); err != nil {
		return nil, fmt.Errorf("update tier usage: %w", err)
	}

	// Create cache entry
	now := sdkCtx.BlockTime()
	expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)
	entry := &types.CacheEntry{
		ContentHash:      contentHash,
		ToolId:           toolID,
		RequestHash:      requestHash,
		Content:          content,
		ContentSizeBytes: size,
		CreatedAt:        now,
		ExpiresAt:        expiresAt,
		AccessCount:      0,
		LastAccessAt:     now,
		PublisherAddress: publisher.String(),
		TtlSeconds:       ttlSeconds,
		IsDeterministic:  isDeterministic,
		Tier:             tier,
		RoyaltyEligible:  royaltyEligible,
	}

	// Store entry
	if err := k.state.Entries.Set(ctx, contentHash, entry); err != nil {
		return nil, fmt.Errorf("failed to store cache entry: %w", err)
	}
	if err := k.state.EntryHeights.Set(ctx, contentHash, sdkCtx.BlockHeight()); err != nil {
		return nil, fmt.Errorf("failed to store entry height: %w", err)
	}

	// Index by request hash
	if err := k.state.RequestIndex.Set(ctx, requestHash, contentHash); err != nil {
		return nil, fmt.Errorf("failed to index by request hash: %w", err)
	}
	if err := k.state.ContentRequestIndex.Set(ctx, collections.Join(contentHash, requestHash)); err != nil {
		return nil, fmt.Errorf("failed to update content request index: %w", err)
	}

	// Index by tool ID
	if err := k.state.ToolIndex.Set(ctx, collections.Join(toolID, contentHash)); err != nil {
		return nil, fmt.Errorf("failed to index by tool id: %w", err)
	}

	// Index by expiry
	if err := k.state.ExpiryIndex.Set(ctx, collections.Join(expiresAt, contentHash)); err != nil {
		return nil, fmt.Errorf("failed to index by expiry: %w", err)
	}

	// Update stats
	if err := k.incrementStats(ctx, uint64(len(content))); err != nil {
		return nil, fmt.Errorf("increment stats: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeCacheStore,
			sdk.NewAttribute(types.AttributeKeyContentHash, contentHash),
			sdk.NewAttribute(types.AttributeKeyRequestHash, requestHash),
			sdk.NewAttribute(types.AttributeKeyToolID, toolID),
			sdk.NewAttribute(types.AttributeKeyPublisher, publisher.String()),
			sdk.NewAttribute(types.AttributeKeyTier, entry.Tier.String()),
			sdk.NewAttribute(types.AttributeKeySize, fmt.Sprintf("%d", len(content))),
			sdk.NewAttribute(types.AttributeKeyTTL, fmt.Sprintf("%d", ttlSeconds)),
		),
	)

	return entry, nil
}

// GetEntry retrieves a cache entry by content hash.
func (k Keeper) GetEntry(ctx context.Context, contentHash string) (*types.CacheEntry, bool, error) {
	contentHash, err := validateKeeperCacheID("content_hash", contentHash)
	if err != nil {
		return nil, false, err
	}
	entry, err := k.state.Entries.Get(ctx, contentHash)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get cache entry: %w", err)
	}
	if entry == nil {
		return nil, false, nil
	}

	// Check if expired
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if !entry.ExpiresAt.IsZero() && sdkCtx.BlockTime().After(entry.ExpiresAt) {
		return entry, false, types.ErrEntryExpired
	}

	return entry, true, nil
}

// LookupByRequest looks up cache entries by request hash.
func (k Keeper) LookupByRequest(ctx context.Context, requestHash string, toolID string) ([]*types.CacheEntry, error) {
	requestHash, err := validateKeeperCacheID("request_hash", requestHash)
	if err != nil {
		return nil, err
	}
	toolID, err = validateOptionalKeeperCacheID("tool_id", toolID)
	if err != nil {
		return nil, err
	}
	contentHash, err := k.state.RequestIndex.Get(ctx, requestHash)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to lookup request: %w", err)
	}

	entry, found, err := k.GetEntry(ctx, contentHash)
	if err != nil && !errors.Is(err, types.ErrEntryExpired) {
		return nil, err
	}
	if !found || entry == nil {
		return nil, nil
	}

	// Filter by tool if specified
	if toolID != "" && entry.ToolId != toolID {
		// Check if this tool has also indexed this content (deduplicated)
		has, err := k.state.ToolIndex.Has(ctx, collections.Join(toolID, entry.ContentHash))
		if err != nil {
			return nil, err
		}
		if !has {
			return nil, nil
		}
	}

	return []*types.CacheEntry{entry}, nil
}

// RecordCacheHit records a cache hit and updates statistics.
func (k Keeper) RecordCacheHit(
	ctx context.Context,
	contentHash string,
	originToolID string,
	servingToolID string,
	requesterAddr sdk.AccAddress,
	costSaved sdk.Coins,
	latencyMs uint64,
	tier types.CacheTier,
) (*types.CacheHit, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	contentHash, err := validateKeeperCacheID("content_hash", contentHash)
	if err != nil {
		return nil, err
	}
	originToolID, err = validateKeeperCacheID("origin_tool_id", originToolID)
	if err != nil {
		return nil, err
	}
	servingToolID, err = validateKeeperCacheID("serving_tool_id", servingToolID)
	if err != nil {
		return nil, err
	}

	// Generate hit ID
	hitSeq, err := k.state.HitSeq.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate hit ID: %w", err)
	}
	hitID := fmt.Sprintf("hit-%d", hitSeq)

	// Get and update entry
	entry, found, err := k.GetEntry(ctx, contentHash)
	if err != nil && !errors.Is(err, types.ErrEntryExpired) {
		return nil, err
	}
	if !found || entry == nil {
		return nil, types.ErrEntryNotFound.Wrapf("content_hash: %s", contentHash)
	}

	// Update access count
	entry.AccessCount++
	entry.LastAccessAt = sdkCtx.BlockTime()
	if err := k.state.Entries.Set(ctx, contentHash, entry); err != nil {
		return nil, fmt.Errorf("failed to update entry access: %w", err)
	}

	// Check for tier promotion
	params := k.GetParams(ctx)
	if entry.AccessCount >= params.MinAccessForPromotion && entry.Tier > types.CacheTier_CACHE_TIER_L1_MEMORY {
		newTier := entry.Tier - 1 // Promote to higher tier (lower number)
		if err := k.PromoteEntry(ctx, contentHash, newTier); err != nil {
			k.Logger(sdkCtx).Warn("failed to promote entry", "content_hash", contentHash, "error", err)
		}
	}

	// Create hit record
	hit := &types.CacheHit{
		HitId:            hitID,
		ContentHash:      contentHash,
		OriginToolId:     originToolID,
		ServingToolId:    servingToolID,
		RequesterAddress: requesterAddr.String(),
		HitAt:            sdkCtx.BlockTime(),
		CostSaved:        sdkCoinsToProto(costSaved),
		LatencyMs:        latencyMs,
		Tier:             tier,
	}

	// Update cache stats
	if err := k.recordHitStats(ctx, costSaved, latencyMs); err != nil {
		return nil, fmt.Errorf("record hit stats: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeCacheHit,
			sdk.NewAttribute(types.AttributeKeyContentHash, contentHash),
			sdk.NewAttribute(types.AttributeKeyOriginToolID, originToolID),
			sdk.NewAttribute(types.AttributeKeyServingToolID, servingToolID),
			sdk.NewAttribute(types.AttributeKeyTier, tier.String()),
			sdk.NewAttribute(types.AttributeKeyCostSaved, costSaved.String()),
			sdk.NewAttribute(types.AttributeKeyLatencyMs, fmt.Sprintf("%d", latencyMs)),
		),
	)

	if err := k.incrementToolCounter(ctx, k.state.ToolHits, servingToolID); err != nil {
		return nil, fmt.Errorf("increment tool hit count: %w", err)
	}
	if originToolID != "" {
		if err := k.incrementOriginHit(ctx, servingToolID, originToolID); err != nil {
			return nil, fmt.Errorf("increment origin hit count: %w", err)
		}
	}

	return hit, nil
}

// RecordHit is a bead-facing alias for RecordCacheHit.
func (k Keeper) RecordHit(
	ctx context.Context,
	contentHash string,
	originToolID string,
	servingToolID string,
	requesterAddr sdk.AccAddress,
	costSaved sdk.Coins,
	latencyMs uint64,
	tier types.CacheTier,
) (*types.CacheHit, error) {
	return k.RecordCacheHit(ctx, contentHash, originToolID, servingToolID, requesterAddr, costSaved, latencyMs, tier)
}

// PromoteEntry promotes a cache entry to a higher tier.
func (k Keeper) PromoteEntry(ctx context.Context, contentHash string, targetTier types.CacheTier) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	entry, found, err := k.GetEntry(ctx, contentHash)
	if err != nil && !errors.Is(err, types.ErrEntryExpired) {
		return err
	}
	if !found || entry == nil {
		return types.ErrEntryNotFound.Wrapf("content_hash: %s", contentHash)
	}

	previousTier := entry.Tier
	if targetTier >= previousTier {
		return types.ErrPromotionFailed.Wrapf("target tier %v is not higher than current tier %v", targetTier, previousTier)
	}

	// Check capacity
	params := k.GetParams(ctx)
	targetUsage := k.getTierUsage(ctx, targetTier)
	var capacity uint64
	switch targetTier {
	case types.CacheTier_CACHE_TIER_L1_MEMORY:
		capacity = params.L1CapacityBytes
	case types.CacheTier_CACHE_TIER_L2_SSD:
		capacity = params.L2CapacityBytes
	case types.CacheTier_CACHE_TIER_L3_HDD:
		capacity = params.L3CapacityBytes
	case types.CacheTier_CACHE_TIER_L4_COLD:
		capacity = params.L4CapacityBytes
	default:
		capacity = math.MaxUint64
	}

	if entry.ContentSizeBytes > capacity || targetUsage > capacity-entry.ContentSizeBytes {
		return types.ErrPromotionFailed.Wrapf("target tier %v full (usage %d + entry %d > cap %d)", targetTier, targetUsage, entry.ContentSizeBytes, capacity)
	}

	if entry.ContentSizeBytes > uint64(math.MaxInt64) {
		return types.ErrContentTooLarge.Wrap("entry size exceeds int64 limit")
	}

	entry.Tier = targetTier
	if err := k.state.Entries.Set(ctx, contentHash, entry); err != nil {
		return fmt.Errorf("failed to update entry tier: %w", err)
	}

	// Update tier usage
	if err := k.updateTierUsage(ctx, previousTier, -int64(entry.ContentSizeBytes)); err != nil {
		return fmt.Errorf("update tier usage (previous): %w", err)
	}
	if err := k.updateTierUsage(ctx, targetTier, int64(entry.ContentSizeBytes)); err != nil {
		return fmt.Errorf("update tier usage (target): %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeTierPromotion,
			sdk.NewAttribute(types.AttributeKeyContentHash, contentHash),
			sdk.NewAttribute("previous_tier", previousTier.String()),
			sdk.NewAttribute("new_tier", targetTier.String()),
		),
	)

	return nil
}

// InvalidateEntry invalidates a single cache entry by content hash.
func (k Keeper) InvalidateEntry(ctx context.Context, contentHash string) (uint64, error) {
	entry, found, err := k.GetEntry(ctx, contentHash)
	if err != nil && !errors.Is(err, types.ErrEntryExpired) {
		return 0, err
	}
	// GetEntry returns (entry, false, ErrEntryExpired) for expired entries.
	// We still need to remove the record, so check entry != nil, not found.
	if entry == nil {
		return 0, nil
	}
	_ = found // used only by callers; expired entries still need cleanup

	// Remove from entries
	if err := k.state.Entries.Remove(ctx, contentHash); err != nil {
		return 0, fmt.Errorf("failed to remove entry: %w", err)
	}
	if err := k.state.EntryHeights.Remove(ctx, contentHash); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return 0, fmt.Errorf("remove entry height %s: %w", contentHash, err)
	}

	// Clean up request indexes (deduplication)
	rng := collections.NewPrefixedPairRange[string, string](contentHash)
	var requestHashes []string
	if walkErr := k.state.ContentRequestIndex.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		requestHashes = append(requestHashes, key.K2())
		return false, nil
	}); walkErr != nil {
		k.Logger(sdk.UnwrapSDKContext(ctx)).Warn("failed to walk content request index", "content_hash", contentHash, "error", walkErr)
	}

	for _, reqHash := range requestHashes {
		if err := k.state.RequestIndex.Remove(ctx, reqHash); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return 0, fmt.Errorf("remove request index %s: %w", reqHash, err)
		}
		if err := k.state.ContentRequestIndex.Remove(ctx, collections.Join(contentHash, reqHash)); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return 0, fmt.Errorf("remove content request index %s: %w", reqHash, err)
		}
	}

	// Remove from expiry index
	if !entry.ExpiresAt.IsZero() {
		if err := k.state.ExpiryIndex.Remove(ctx, collections.Join(entry.ExpiresAt, contentHash)); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return 0, fmt.Errorf("remove expiry index %s: %w", contentHash, err)
		}
	}

	// Remove from tool index
	if err := k.state.ToolIndex.Remove(ctx, collections.Join(entry.ToolId, contentHash)); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return 0, fmt.Errorf("remove tool index %s/%s: %w", entry.ToolId, contentHash, err)
	}

	// Update stats
	if err := k.decrementStats(ctx, entry.ContentSizeBytes); err != nil {
		return 0, fmt.Errorf("decrement stats: %w", err)
	}

	// Decrement tier capacity
	if err := k.updateTierUsage(ctx, entry.Tier, -int64(entry.ContentSizeBytes)); err != nil {
		return 0, fmt.Errorf("update tier usage: %w", err)
	}

	return entry.ContentSizeBytes, nil
}

// InvalidateByTool invalidates all cache entries for a specific tool.
func (k Keeper) InvalidateByTool(ctx context.Context, toolID string) (uint64, uint64, error) {
	var entriesInvalidated uint64
	var bytesFreed uint64

	// Collect content hashes first to avoid iterator invalidation
	var contentHashes []string
	rng := collections.NewPrefixedPairRange[string, string](toolID)
	err := k.state.ToolIndex.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		contentHashes = append(contentHashes, key.K2())
		return false, nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to walk tool index: %w", err)
	}

	// Invalidate collected entries
	for _, hash := range contentHashes {
		// Clean up the ToolIndex proactively. If this contentHash was deduplicated
		// across multiple tools, InvalidateEntry only removes the index for entry.ToolId.
		// Removing it here ensures we don't leak ghost entries for the other tools.
		if err := k.state.ToolIndex.Remove(ctx, collections.Join(toolID, hash)); err != nil && !errors.Is(err, collections.ErrNotFound) {
			k.Logger(sdk.UnwrapSDKContext(ctx)).Warn("failed to remove tool index proactively", "tool_id", toolID, "content_hash", hash, "error", err)
		}

		freed, err := k.InvalidateEntry(ctx, hash)
		if err != nil {
			return entriesInvalidated, bytesFreed, err
		}
		entriesInvalidated++
		bytesFreed += freed
	}

	return entriesInvalidated, bytesFreed, nil
}

// GetStats returns the current cache statistics.
func (k Keeper) GetStats(ctx context.Context) (*types.CacheStats, error) {
	stats, err := k.state.Stats.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return &types.CacheStats{}, nil
		}
		return nil, err
	}
	return stats, nil
}

// ToolStats summarizes cache hit/miss behavior for a single tool.
type ToolStats struct {
	ToolID     string
	HitCount   uint64
	MissCount  uint64
	HitRate    string
	OriginHits map[string]uint64
}

// RoyaltyDistribution captures the concrete payout/retention breakdown for a
// cache-hit royalty collection.
type RoyaltyDistribution struct {
	OriginPublisher    sdk.Coins
	ServingPublisher   sdk.Coins
	GovernanceRetained sdk.Coins
	OriginShareBps     uint32
	ServingShareBps    uint32
	GovernanceShareBps uint32
	AgeDays            uint64
}

// GetToolStats returns per-tool cache hit/miss counters plus origin-hit
// attribution used by Explorer.
func (k Keeper) GetToolStats(ctx context.Context, toolID string) (*ToolStats, error) {
	stats := &ToolStats{
		ToolID:     toolID,
		HitRate:    "0.0000",
		OriginHits: map[string]uint64{},
	}

	hits, err := k.getCounter(ctx, k.state.ToolHits, toolID)
	if err != nil {
		return nil, fmt.Errorf("load tool hits: %w", err)
	}
	misses, err := k.getCounter(ctx, k.state.ToolMisses, toolID)
	if err != nil {
		return nil, fmt.Errorf("load tool misses: %w", err)
	}
	stats.HitCount = hits
	stats.MissCount = misses
	stats.HitRate = computeHitRate(hits, misses)

	rng := collections.NewPrefixedPairRange[string, string](toolID)
	if err := k.state.OriginHits.Walk(ctx, rng, func(key collections.Pair[string, string], value uint64) (bool, error) {
		stats.OriginHits[key.K2()] = value
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("walk origin hits: %w", err)
	}

	return stats, nil
}

// CollectRoyalty distributes a prefunded cache-hit fee from the CAC module
// account. The serving publisher receives the storage/bandwidth/verification
// shares as a single aggregate payout; governance share plus decay slack stays
// in the module account for later treasury handling.
func (k Keeper) CollectRoyalty(
	ctx context.Context,
	contentHash string,
	originToolID string,
	servingToolID string,
	royaltyPool sdk.Coins,
) (*RoyaltyDistribution, error) {
	if royaltyPool.IsZero() {
		return &RoyaltyDistribution{
			OriginPublisher:    sdk.NewCoins(),
			ServingPublisher:   sdk.NewCoins(),
			GovernanceRetained: sdk.NewCoins(),
		}, nil
	}

	entry, found, err := k.GetEntry(ctx, contentHash)
	if err != nil && !errors.Is(err, types.ErrEntryExpired) {
		return nil, err
	}
	if !found || entry == nil {
		return nil, types.ErrEntryNotFound.Wrapf("content_hash: %s", contentHash)
	}

	params := k.GetParams(ctx)
	if !params.EnableRoyalties || !entry.RoyaltyEligible {
		return &RoyaltyDistribution{
			OriginPublisher:    sdk.NewCoins(),
			ServingPublisher:   sdk.NewCoins(),
			GovernanceRetained: royaltyPool,
			OriginShareBps:     0,
			ServingShareBps:    0,
			GovernanceShareBps: 10000,
			AgeDays:            0,
		}, nil
	}

	originShareBps, servingShareBps, governanceShareBps, ageDays, err := k.currentRoyaltyShares(ctx, contentHash)
	if err != nil {
		return nil, err
	}
	originRoyalty, servingRoyalty, governanceRetained := splitRoyaltyCoins(royaltyPool, originShareBps, servingShareBps)

	if !originRoyalty.IsZero() {
		originPublisher, lookupErr := k.registryKeeper.GetToolPublisher(ctx, originToolID)
		if lookupErr != nil {
			return nil, fmt.Errorf("lookup origin publisher: %w", lookupErr)
		}
		if len(originPublisher) == 0 {
			return nil, types.ErrRoyaltyFailed.Wrapf("missing origin publisher for %s", originToolID)
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, originPublisher, originRoyalty); err != nil {
			return nil, types.ErrRoyaltyFailed.Wrapf("origin payout failed: %v", err)
		}
	}

	if !servingRoyalty.IsZero() {
		servingPublisher, lookupErr := k.registryKeeper.GetToolPublisher(ctx, servingToolID)
		if lookupErr != nil {
			return nil, fmt.Errorf("lookup serving publisher: %w", lookupErr)
		}
		if len(servingPublisher) == 0 {
			return nil, types.ErrRoyaltyFailed.Wrapf("missing serving publisher for %s", servingToolID)
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleAccountName, servingPublisher, servingRoyalty); err != nil {
			return nil, types.ErrRoyaltyFailed.Wrapf("serving payout failed: %v", err)
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeRoyaltyDistributed,
			sdk.NewAttribute(types.AttributeKeyContentHash, contentHash),
			sdk.NewAttribute(types.AttributeKeyOriginToolID, originToolID),
			sdk.NewAttribute(types.AttributeKeyServingToolID, servingToolID),
			sdk.NewAttribute(types.AttributeKeyOriginRoyalty, originRoyalty.String()),
			sdk.NewAttribute(types.AttributeKeyServingRoyalty, servingRoyalty.String()),
			sdk.NewAttribute(types.AttributeKeyGovernanceRoyalty, governanceRetained.String()),
			sdk.NewAttribute("age_days", fmt.Sprintf("%d", ageDays)),
		),
	)

	return &RoyaltyDistribution{
		OriginPublisher:    originRoyalty,
		ServingPublisher:   servingRoyalty,
		GovernanceRetained: governanceRetained,
		OriginShareBps:     originShareBps,
		ServingShareBps:    servingShareBps,
		GovernanceShareBps: governanceShareBps,
		AgeDays:            ageDays,
	}, nil
}

// TickDecay performs CAC upkeep and records the current block-height tick.
func (k Keeper) TickDecay(ctx context.Context, limit int) (uint64, error) {
	evicted, err := k.processExpiredEntries(ctx, limit)
	if err != nil {
		return 0, err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.state.LastDecayTick.Set(ctx, sdkCtx.BlockHeight()); err != nil {
		return evicted, fmt.Errorf("store last decay tick: %w", err)
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeDecayTick,
			sdk.NewAttribute("tick_height", fmt.Sprintf("%d", sdkCtx.BlockHeight())),
			sdk.NewAttribute(types.AttributeKeyEntriesInvalidated, fmt.Sprintf("%d", evicted)),
		),
	)
	return evicted, nil
}

// Helper functions

func computeContentHash(content []byte) string {
	hash := blake3.Sum256(content)
	return "blake3:" + hex.EncodeToString(hash[:])
}

// sdkCoinsToProto returns the []sdk.Coin form stored on proto messages
// (CacheStats.TotalCostSaved, CacheHit royalties). After the gogoproto
// migration these fields are repeated cosmos.base.v1beta1.Coin with
// (gogoproto.castrepeated)=Coins, so the stored representation is already
// []sdk.Coin — this is a 1:1 view, kept as a named helper so the keeper call
// sites read the same as before the migration.
func sdkCoinsToProto(coins sdk.Coins) []sdk.Coin {
	return []sdk.Coin(coins)
}

// protoCoinsToSDK canonicalizes a stored or user-supplied []sdk.Coin into
// sdk.Coins. Silently skips nil-amount, non-positive, and invalid-denom
// entries, then runs the result through sdk.NewCoins to sort and dedupe.
// This helper runs on decoded stored state (stats totals) AND on user-supplied
// MsgRecordCacheHit.CostSaved via msg_server.go, so both vectors need
// defensive conversion: addProtoCoins below calls sdk.Coins.Add, which panics
// on unsorted input. Matches the hardening pattern applied in x/insurance,
// x/registry, x/credits, x/vaults, x/passport.
func protoCoinsToSDK(coins []sdk.Coin) sdk.Coins {
	result := make(sdk.Coins, 0, len(coins))
	for _, c := range coins {
		if err := sdk.ValidateDenom(c.Denom); err != nil {
			continue
		}
		if c.Amount.IsNil() || !c.Amount.IsPositive() {
			continue
		}
		result = append(result, c)
	}
	return sdk.NewCoins(result...)
}

// addProtoCoins adds two stored coin slices, canonicalizing both first.
func addProtoCoins(a, b []sdk.Coin) []sdk.Coin {
	aSDK := protoCoinsToSDK(a)
	bSDK := protoCoinsToSDK(b)
	return []sdk.Coin(aSDK.Add(bSDK...))
}

func (k Keeper) incrementStats(ctx context.Context, sizeBytes uint64) error {
	stats, err := k.GetStats(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("read cache stats: %w", err)
		}
		stats = &types.CacheStats{}
	}
	if stats == nil {
		stats = &types.CacheStats{}
	}
	stats.TotalEntries++
	stats.TotalSizeBytes += sizeBytes
	stats.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime()
	return k.state.Stats.Set(ctx, stats)
}

func (k Keeper) decrementStats(ctx context.Context, sizeBytes uint64) error {
	stats, err := k.GetStats(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("read cache stats: %w", err)
		}
		stats = &types.CacheStats{}
	}
	if stats == nil {
		stats = &types.CacheStats{}
	}
	if stats.TotalEntries > 0 {
		stats.TotalEntries--
	}
	if stats.TotalSizeBytes >= sizeBytes {
		stats.TotalSizeBytes -= sizeBytes
	} else {
		stats.TotalSizeBytes = 0
	}
	stats.EvictionCount++
	stats.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime()
	return k.state.Stats.Set(ctx, stats)
}

func (k Keeper) recordHitStats(ctx context.Context, costSaved sdk.Coins, latencyMs uint64) error {
	stats, err := k.GetStats(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("read cache stats: %w", err)
		}
		stats = &types.CacheStats{}
	}
	if stats == nil {
		stats = &types.CacheStats{}
	}
	stats.HitCount++
	stats.TotalCostSaved = addProtoCoins(stats.TotalCostSaved, sdkCoinsToProto(costSaved))

	// Update average latency (simple moving average)
	if stats.HitCount == 1 {
		stats.AvgLatencyMs = latencyMs
	} else {
		// Weighted average: new_avg = old_avg + (new_value - old_avg) / count
		// Handle unsigned subtraction to avoid underflow
		if latencyMs >= stats.AvgLatencyMs {
			stats.AvgLatencyMs += (latencyMs - stats.AvgLatencyMs) / stats.HitCount
		} else {
			stats.AvgLatencyMs -= (stats.AvgLatencyMs - latencyMs) / stats.HitCount
		}
	}

	// Update hit rate using integer arithmetic for determinism.
	// HitRate is stored as a decimal string, e.g. "0.7500".
	stats.HitRate = hitRateDec(stats.HitCount, stats.MissCount)

	stats.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime()
	return k.state.Stats.Set(ctx, stats)
}

func (k Keeper) recordMiss(ctx context.Context) error {
	stats, err := k.GetStats(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("read cache stats: %w", err)
		}
		stats = &types.CacheStats{}
	}
	if stats == nil {
		stats = &types.CacheStats{}
	}
	stats.MissCount++

	// Update hit rate using integer arithmetic for determinism.
	stats.HitRate = hitRateDec(stats.HitCount, stats.MissCount)

	stats.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime()
	return k.state.Stats.Set(ctx, stats)
}

// computeHitRate calculates hit/(hit+miss) as a 4-decimal string using
// integer arithmetic only, avoiding float64 non-determinism in consensus.
// It feeds the string ToolCacheStats.HitRate explorer field; hitRateDec wraps
// it for the on-chain LegacyDec CacheStats.HitRate field.
func computeHitRate(hits, misses uint64) string {
	if hits == 0 && misses == 0 {
		return "0.0000"
	}
	// Guard against addition overflow.
	if hits > math.MaxUint64-misses {
		return "1.0000"
	}

	total := hits + misses
	// Fast path: use native uint64 when hits*10000 won't overflow.
	if hits <= math.MaxUint64/10000 {
		bps := (hits * 10000) / total
		return fmt.Sprintf("%d.%04d", bps/10000, bps%10000)
	}
	// Slow path for very large counters: use big.Int to avoid overflow.
	bigHits := new(big.Int).SetUint64(hits)
	bigHits.Mul(bigHits, big.NewInt(10000))
	bps := bigHits.Div(bigHits, new(big.Int).SetUint64(total)).Uint64()
	return fmt.Sprintf("%d.%04d", bps/10000, bps%10000)
}

// hitRateDec parses the 4-decimal computeHitRate string into a LegacyDec for
// the on-chain CacheStats.HitRate field. The input is always our own
// controlled format, so a parse error falls back to zero rather than panicking
// inside consensus.
func hitRateDec(hits, misses uint64) sdkmath.LegacyDec {
	d, err := sdkmath.LegacyNewDecFromStr(computeHitRate(hits, misses))
	if err != nil {
		return sdkmath.LegacyZeroDec()
	}
	return d
}

func (k Keeper) getCounter(ctx context.Context, store collections.Map[string, uint64], key string) (uint64, error) {
	value, err := store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return value, nil
}

func (k Keeper) incrementToolCounter(ctx context.Context, store collections.Map[string, uint64], key string) error {
	value, err := k.getCounter(ctx, store, key)
	if err != nil {
		return err
	}
	return store.Set(ctx, key, value+1)
}

func (k Keeper) incrementOriginHit(ctx context.Context, toolID, originToolID string) error {
	key := collections.Join(toolID, originToolID)
	value, err := k.state.OriginHits.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		value = 0
	}
	return k.state.OriginHits.Set(ctx, key, value+1)
}

func (k Keeper) getEntryHeight(ctx context.Context, contentHash string) (int64, error) {
	height, err := k.state.EntryHeights.Get(ctx, contentHash)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return height, nil
}

func (k Keeper) currentRoyaltyShares(ctx context.Context, contentHash string) (originShare, servingShare, governanceShare uint32, ageDays uint64, err error) {
	params := k.GetParams(ctx)
	entryHeight, err := k.getEntryHeight(ctx, contentHash)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("load entry height: %w", err)
	}

	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if entryHeight > 0 && currentHeight > entryHeight && params.BlocksPerDay > 0 {
		ageDays = uint64(currentHeight-entryHeight) / params.BlocksPerDay
	}

	originShare = computeDecayedBPS(params.RoyaltyOriginBps, params.RoyaltyDecayBps, ageDays)
	servingShare = params.RoyaltyStorageBps + params.RoyaltyBandwidthBps + params.RoyaltyVerificationBps
	governanceShare = params.RoyaltyGovernanceBps
	if params.RoyaltyOriginBps > originShare {
		governanceShare += params.RoyaltyOriginBps - originShare
	}
	return originShare, servingShare, governanceShare, ageDays, nil
}

func computeDecayedBPS(baseBps, decayBps uint32, epochs uint64) uint32 {
	if baseBps == 0 || epochs == 0 {
		return baseBps
	}
	if decayBps >= 10000 {
		return baseBps
	}

	factor := decimal.NewFromInt(int64(decayBps)).Div(decimal.NewFromInt(10000))
	value := decimal.NewFromInt(int64(baseBps)).Mul(powDecimal(factor, epochs))
	rounded := value.RoundBank(0).IntPart()
	if rounded < 0 {
		return 0
	}
	if rounded > 10000 {
		return 10000
	}
	return uint32(rounded)
}

func powDecimal(base decimal.Decimal, exponent uint64) decimal.Decimal {
	result := decimal.NewFromInt(1)
	factor := base
	for exponent > 0 {
		if exponent&1 == 1 {
			result = result.Mul(factor)
		}
		exponent >>= 1
		if exponent > 0 {
			factor = factor.Mul(factor)
		}
	}
	return result
}

func splitRoyaltyCoins(total sdk.Coins, originShareBps, servingShareBps uint32) (sdk.Coins, sdk.Coins, sdk.Coins) {
	originCoins := sdk.NewCoins()
	servingCoins := sdk.NewCoins()
	retainedCoins := sdk.NewCoins()

	for _, coin := range total {
		originQty := coin.Amount.MulRaw(int64(originShareBps)).QuoRaw(10000)
		servingQty := coin.Amount.MulRaw(int64(servingShareBps)).QuoRaw(10000)
		retainedQty := coin.Amount.Sub(originQty).Sub(servingQty)

		if originQty.IsPositive() {
			originCoins = originCoins.Add(sdk.NewCoin(coin.Denom, originQty))
		}
		if servingQty.IsPositive() {
			servingCoins = servingCoins.Add(sdk.NewCoin(coin.Denom, servingQty))
		}
		if retainedQty.IsPositive() {
			retainedCoins = retainedCoins.Add(sdk.NewCoin(coin.Denom, retainedQty))
		}
	}

	return originCoins, servingCoins, retainedCoins
}

func (k Keeper) getTierUsage(ctx context.Context, tier types.CacheTier) uint64 {
	usage, err := k.state.TierCapacity.Get(ctx, int32(tier))
	if err != nil {
		return 0
	}
	return usage
}

func (k Keeper) updateTierUsage(ctx context.Context, tier types.CacheTier, delta int64) error {
	usage := k.getTierUsage(ctx, tier)
	if delta > 0 {
		usage += uint64(delta)
	} else {
		dec := uint64(-delta)
		if usage >= dec {
			usage -= dec
		} else {
			usage = 0
		}
	}
	return k.state.TierCapacity.Set(ctx, int32(tier), usage)
}

// ProcessExpiredEntries removes entries that have passed their TTL.
func (k Keeper) ProcessExpiredEntries(ctx context.Context, limit int) error {
	_, err := k.processExpiredEntries(ctx, limit)
	return err
}

func (k Keeper) processExpiredEntries(ctx context.Context, limit int) (uint64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()

	if limit <= 0 {
		limit = 100 // Default limit
	}

	type expiredEntry struct {
		timestamp time.Time
		hash      string
	}
	var expiredHashes []expiredEntry
	rng := new(collections.Range[collections.Pair[time.Time, string]]).
		EndExclusive(collections.Join(now.Add(time.Nanosecond), ""))

	err := k.state.ExpiryIndex.Walk(ctx, rng, func(key collections.Pair[time.Time, string]) (bool, error) {
		expiredHashes = append(expiredHashes, expiredEntry{
			timestamp: key.K1(),
			hash:      key.K2(),
		})
		return len(expiredHashes) >= limit, nil
	})
	if err != nil {
		return 0, err
	}

	var evicted uint64
	for _, expired := range expiredHashes {
		if _, err := k.InvalidateEntry(ctx, expired.hash); err != nil {
			k.Logger(sdkCtx).Error("failed to process expired entry", "content_hash", expired.hash, "error", err)
		} else {
			evicted++
			// Proactively clean up the index entry in case the entry was already deleted
			// and InvalidateEntry returned early.
			if rmErr := k.state.ExpiryIndex.Remove(ctx, collections.Join(expired.timestamp, expired.hash)); rmErr != nil && !errors.Is(rmErr, collections.ErrNotFound) {
				k.Logger(sdkCtx).Error("failed to remove orphaned expiry index", "content_hash", expired.hash, "error", rmErr)
			}

			// Emit eviction event
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					types.EventTypeCacheEvict,
					sdk.NewAttribute(types.AttributeKeyContentHash, expired.hash),
					sdk.NewAttribute("reason", "expired"),
				),
			)
		}
	}

	return evicted, nil
}
