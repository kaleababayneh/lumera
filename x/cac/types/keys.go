
// Package types holds shared types and helpers for the CAC (Content-Addressed Cache) module.
//
//revive:disable:var-naming // The module keeps its protobuf types under a canonical types package.
package types

const (
	// ModuleName defines the CAC module name used across keepers and routing.
	ModuleName = "cac"
	// StoreKey specifies the primary KVStore key for the CAC module.
	StoreKey = ModuleName
	// RouterKey identifies the message routing key for CAC transactions.
	RouterKey = ModuleName
	// QuerierRoute sets the gRPC querier route for the CAC module.
	QuerierRoute = ModuleName

	// ParamsPrefixByte prefixes parameter entries under the module store.
	ParamsPrefixByte = uint8(0x01)
	// CacheEntriesPrefixByte prefixes cache entry records by content hash.
	CacheEntriesPrefixByte = uint8(0x02)
	// RequestIndexPrefixByte prefixes request hash -> content hash lookups.
	RequestIndexPrefixByte = uint8(0x03)
	// ToolIndexPrefixByte prefixes tool_id -> content hashes index.
	ToolIndexPrefixByte = uint8(0x04)
	// CacheHitsPrefixByte prefixes cache hit records.
	CacheHitsPrefixByte = uint8(0x05)
	// CacheStatsPrefixByte prefixes cache statistics.
	CacheStatsPrefixByte = uint8(0x06)
	// InvalidationsPrefixByte prefixes invalidation request records.
	InvalidationsPrefixByte = uint8(0x07)
	// TierCapacityPrefixByte prefixes tier capacity tracking.
	TierCapacityPrefixByte = uint8(0x08)
	// EntrySeqKeyPrefixByte prefixes auto-increment entry sequence counters.
	EntrySeqKeyPrefixByte = uint8(0x09)
	// HitSeqKeyPrefixByte prefixes auto-increment hit sequence counters.
	HitSeqKeyPrefixByte = uint8(0x0A)
	// ContentRequestIndexPrefixByte prefixes content hash -> request hashes set.
	ContentRequestIndexPrefixByte = uint8(0x0B)
	// ExpiryIndexPrefixByte prefixes expiration time -> content hashes index.
	ExpiryIndexPrefixByte = uint8(0x0C)
	// ToolHitStatsPrefixByte prefixes per-tool hit counters.
	ToolHitStatsPrefixByte = uint8(0x0D)
	// ToolMissStatsPrefixByte prefixes per-tool miss counters.
	ToolMissStatsPrefixByte = uint8(0x0E)
	// OriginHitStatsPrefixByte prefixes per-(tool, origin) hit counters.
	OriginHitStatsPrefixByte = uint8(0x0F)
	// EntryHeightPrefixByte prefixes content_hash -> creation-height metadata.
	EntryHeightPrefixByte = uint8(0x10)
	// LastDecayTickPrefixByte prefixes the last successful decay tick height.
	LastDecayTickPrefixByte = uint8(0x11)
)

var (
	// ParamsPrefix stores the binary prefix for parameters collections.
	ParamsPrefix = []byte{ParamsPrefixByte}
	// CacheEntriesPrefix stores the binary prefix for cache entry collections.
	CacheEntriesPrefix = []byte{CacheEntriesPrefixByte}
	// RequestIndexPrefix stores the binary prefix for request hash index.
	RequestIndexPrefix = []byte{RequestIndexPrefixByte}
	// ToolIndexPrefix stores the binary prefix for tool index.
	ToolIndexPrefix = []byte{ToolIndexPrefixByte}
	// CacheHitsPrefix stores the binary prefix for cache hit records.
	CacheHitsPrefix = []byte{CacheHitsPrefixByte}
	// CacheStatsPrefix stores the binary prefix for cache statistics.
	CacheStatsPrefix = []byte{CacheStatsPrefixByte}
	// InvalidationsPrefix stores the binary prefix for invalidation records.
	InvalidationsPrefix = []byte{InvalidationsPrefixByte}
	// TierCapacityPrefix stores the binary prefix for tier capacity tracking.
	TierCapacityPrefix = []byte{TierCapacityPrefixByte}
	// EntrySeqKeyPrefix stores the prefix used for entry sequence counters.
	EntrySeqKeyPrefix = []byte{EntrySeqKeyPrefixByte}
	// HitSeqKeyPrefix stores the prefix used for hit sequence counters.
	HitSeqKeyPrefix = []byte{HitSeqKeyPrefixByte}
	// ContentRequestIndexPrefix stores the prefix for content -> request index.
	ContentRequestIndexPrefix = []byte{ContentRequestIndexPrefixByte}
	// ExpiryIndexPrefix stores the prefix for expiry index.
	ExpiryIndexPrefix = []byte{ExpiryIndexPrefixByte}
	// ToolHitStatsPrefix stores the prefix for per-tool hit counters.
	ToolHitStatsPrefix = []byte{ToolHitStatsPrefixByte}
	// ToolMissStatsPrefix stores the prefix for per-tool miss counters.
	ToolMissStatsPrefix = []byte{ToolMissStatsPrefixByte}
	// OriginHitStatsPrefix stores the prefix for per-(tool, origin) hit counters.
	OriginHitStatsPrefix = []byte{OriginHitStatsPrefixByte}
	// EntryHeightPrefix stores the prefix for content creation heights.
	EntryHeightPrefix = []byte{EntryHeightPrefixByte}
	// LastDecayTickPrefix stores the prefix for the last decay tick height.
	LastDecayTickPrefix = []byte{LastDecayTickPrefixByte}
)

// ModuleAccountName defines the CAC module account identifier.
const ModuleAccountName = ModuleName

// Default parameter values
const (
	// DefaultTTLSeconds is the default time-to-live for cache entries (1 week).
	DefaultTTLSeconds = uint64(7 * 24 * 60 * 60)
	// MaxTTLSeconds is the largest TTL that can be safely converted with
	// time.Duration(ttl) * time.Second without overflowing.
	MaxTTLSeconds = uint64((1<<63 - 1) / 1_000_000_000)
	// DefaultMaxEntrySizeBytes is the default maximum size of a single cache entry (1 MB).
	DefaultMaxEntrySizeBytes = uint64(1 * 1024 * 1024)
	// DefaultL1CapacityBytes is the default L1 (memory) cache capacity (16 GB).
	DefaultL1CapacityBytes = uint64(16 * 1024 * 1024 * 1024)
	// DefaultL2CapacityBytes is the default L2 (SSD) cache capacity (1 TB).
	DefaultL2CapacityBytes = uint64(1 * 1024 * 1024 * 1024 * 1024)
	// DefaultL3CapacityBytes is the default L3 (HDD) cache capacity (100 TB).
	DefaultL3CapacityBytes = uint64(100 * 1024 * 1024 * 1024 * 1024)
	// DefaultL4CapacityBytes is the default L4 (Cold) cache capacity (1 PB).
	DefaultL4CapacityBytes = uint64(1024 * 1024 * 1024 * 1024 * 1024)
	// DefaultRoyaltyOriginBPS is the default basis points for origin tool royalty (50%).
	DefaultRoyaltyOriginBPS = uint32(5000)
	// DefaultRoyaltyStorageBPS is the default storage share basis points (20%).
	DefaultRoyaltyStorageBPS = uint32(2000)
	// DefaultRoyaltyBandwidthBPS is the default bandwidth share basis points (15%).
	DefaultRoyaltyBandwidthBPS = uint32(1500)
	// DefaultRoyaltyVerificationBPS is the default verification share basis points (10%).
	DefaultRoyaltyVerificationBPS = uint32(1000)
	// DefaultRoyaltyGovernanceBPS is the default governance share basis points (5%).
	DefaultRoyaltyGovernanceBPS = uint32(500)
	// DefaultRoyaltyDecayBPS is the daily origin-share decay factor (0.95x).
	DefaultRoyaltyDecayBPS = uint32(9500)
	// DefaultBlocksPerDay converts block-height deltas into whole-day decay epochs.
	DefaultBlocksPerDay = uint64(14400)
	// DefaultMinAccessForPromotion is the default minimum accesses before tier promotion.
	DefaultMinAccessForPromotion = uint64(10)
	// DefaultEnableRoyalties is whether royalties are enabled by default.
	DefaultEnableRoyalties = true
)

// Event types for CAC module
const (
	// EventTypeCacheStore is emitted when content is stored in cache.
	EventTypeCacheStore = "cache_store"
	// EventTypeCacheHit is emitted when a cache hit occurs.
	EventTypeCacheHit = "cache_hit"
	// EventTypeCacheMiss is emitted when a cache miss occurs.
	EventTypeCacheMiss = "cache_miss"
	// EventTypeCacheInvalidate is emitted when cache entries are invalidated.
	EventTypeCacheInvalidate = "cache_invalidate"
	// EventTypeCacheEvict is emitted when cache entries are evicted.
	EventTypeCacheEvict = "cache_evict"
	// EventTypeTierPromotion is emitted when an entry is promoted to a higher tier.
	EventTypeTierPromotion = "tier_promotion"
	// EventTypeRoyaltyDistributed is emitted when cache hit royalties are distributed.
	EventTypeRoyaltyDistributed = "royalty_distributed"
	// EventTypeDecayTick is emitted when CAC upkeep processes expiry/decay work.
	EventTypeDecayTick = "decay_tick"
)

// Attribute keys for CAC events
const (
	// AttributeKeyContentHash is the Blake3 hash of cached content.
	AttributeKeyContentHash = "content_hash"
	// AttributeKeyRequestHash is the Blake3 hash of the original request.
	AttributeKeyRequestHash = "request_hash"
	// AttributeKeyToolID is the tool identifier.
	AttributeKeyToolID = "tool_id"
	// AttributeKeyPublisher is the publisher address.
	AttributeKeyPublisher = "publisher"
	// AttributeKeyTier is the cache tier.
	AttributeKeyTier = "tier"
	// AttributeKeySize is the size in bytes.
	AttributeKeySize = "size"
	// AttributeKeyTTL is the time-to-live in seconds.
	AttributeKeyTTL = "ttl"
	// AttributeKeyOriginToolID is the origin tool ID for cache hits.
	AttributeKeyOriginToolID = "origin_tool_id"
	// AttributeKeyServingToolID is the serving tool ID for cache hits.
	AttributeKeyServingToolID = "serving_tool_id"
	// AttributeKeyOriginRoyalty is the royalty amount for origin tool.
	AttributeKeyOriginRoyalty = "origin_royalty"
	// AttributeKeyServingRoyalty is the royalty amount for serving tool.
	AttributeKeyServingRoyalty = "serving_royalty"
	// AttributeKeyGovernanceRoyalty is the retained governance / treasury amount.
	AttributeKeyGovernanceRoyalty = "governance_royalty"
	// AttributeKeyCostSaved is the estimated cost saved by cache hit.
	AttributeKeyCostSaved = "cost_saved"
	// AttributeKeyLatencyMs is the cache lookup latency in milliseconds.
	AttributeKeyLatencyMs = "latency_ms"
	// AttributeKeyInvalidationTarget is the invalidation target type.
	AttributeKeyInvalidationTarget = "invalidation_target"
	// AttributeKeyEntriesInvalidated is the count of invalidated entries.
	AttributeKeyEntriesInvalidated = "entries_invalidated"
	// AttributeKeyBytesFreed is the bytes freed by invalidation.
	AttributeKeyBytesFreed = "bytes_freed"
)
