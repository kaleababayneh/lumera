package types

import "strings"

// Module metadata and store identifiers for the router module.
const (
	ModuleName   = "router"
	StoreKey     = ModuleName
	RouterKey    = ModuleName
	QuerierRoute = ModuleName
	MemStoreKey  = "mem_" + ModuleName
)

// Store key prefixes derive the KV-store layout for the router module.
var (
	// Legacy prefixes (kept for compatibility)
	SessionPrefix   = []byte{0x01}
	QuotePrefix     = []byte{0x02}
	CachePrefix     = []byte{0x03}
	MetricsPrefix   = []byte{0x04}
	ActiveSetPrefix = []byte{0x05}
	ReceiptPrefix   = []byte{0x06}

	// New collections-based prefixes
	ParamsKey                    = []byte{0x10}
	GlobalMetricsKey             = []byte{0x11}
	ToolMetricsKeyPrefixVal      = []byte{0x12}
	SessionMetricsKeyPrefixVal   = []byte{0x13}
	CategoryMetricsKeyPrefixVal  = []byte{0x14}
	PolicyUpdatesKeyPrefixVal    = []byte{0x15}
	PolicyUpdateCounterKeyVal    = []byte{0x16}
	CACRecordsKeyPrefixVal       = []byte{0x17}
	CACRecordCounterKeyVal       = []byte{0x18}
	SelectionScoresKeyPrefixVal  = []byte{0x19}
	ActiveToolsKeyPrefixVal      = []byte{0x1A}
	NextAggregationKeyVal        = []byte{0x1B}
	ProcessedNoncesKeyPrefixVal  = []byte{0x1C} // Replay protection nonces
	CacheEntriesKeyPrefixVal     = []byte{0x1D}
	CacheStatsKeyPrefixVal       = []byte{0x1E}
	QuotesKeyPrefixVal           = []byte{0x1F}
	SessionsKeyPrefixVal         = []byte{0x20}
	QuoteSequenceKeyVal          = []byte{0x21}
	LastProcessedToolIDKeyVal    = []byte{0x22}
	LastProcessedSessionIDKeyVal = []byte{0x23}
	LastProcessedNonceKeyVal     = []byte{0x24}
	DiscoverySubsidySpentKeyVal  = []byte{0x25}
	DiscoverySubsidyResetKeyVal  = []byte{0x26}

	// Index prefixes for secondary indexes
	CACOriginIndexPrefix           = []byte{0x30}
	CACConsumerIndexPrefix         = []byte{0x31}
	CACBlockIndexPrefix            = []byte{0x32}
	CACCompositeIndexPrefixVal     = []byte{0x33}
	ActivationSessionIndexPrefix   = []byte{0x34}
	ActivationToolIndexPrefix      = []byte{0x35}
	ActivationActiveIndexPrefix    = []byte{0x36}
	NonceInvocationHashIndexPrefix = []byte{0x37}
)

// ParamsKeyPrefix returns the store prefix for module parameters.
func ParamsKeyPrefix() []byte {
	return ParamsKey
}

// GlobalMetricsKeyPrefix returns the prefix for global metrics state.
func GlobalMetricsKeyPrefix() []byte {
	return GlobalMetricsKey
}

// ToolMetricsKeyPrefix returns the prefix for per-tool metrics.
func ToolMetricsKeyPrefix() []byte {
	return ToolMetricsKeyPrefixVal
}

// SessionMetricsKeyPrefix returns the prefix for session metrics.
func SessionMetricsKeyPrefix() []byte {
	return SessionMetricsKeyPrefixVal
}

// CategoryMetricsKeyPrefix returns the prefix for category metrics state.
func CategoryMetricsKeyPrefix() []byte {
	return CategoryMetricsKeyPrefixVal
}

// PolicyUpdatesKeyPrefix returns the prefix for policy update entries.
func PolicyUpdatesKeyPrefix() []byte {
	return PolicyUpdatesKeyPrefixVal
}

// PolicyUpdateCounterKey returns the key for the policy update sequence.
func PolicyUpdateCounterKey() []byte {
	return PolicyUpdateCounterKeyVal
}

// CACRecordsKeyPrefix returns the prefix for CAC royalty records.
func CACRecordsKeyPrefix() []byte {
	return CACRecordsKeyPrefixVal
}

// CACCompositeIndexPrefix returns the prefix for origin+consumer composite index.
func CACCompositeIndexPrefix() []byte {
	return CACCompositeIndexPrefixVal
}

// CACRecordCounterKey returns the key for the CAC record counter.
func CACRecordCounterKey() []byte {
	return CACRecordCounterKeyVal
}

// SelectionScoresKeyPrefix returns the prefix for selection score storage.
func SelectionScoresKeyPrefix() []byte {
	return SelectionScoresKeyPrefixVal
}

// ActiveToolsKeyPrefix returns the prefix for active tool records.
func ActiveToolsKeyPrefix() []byte {
	return ActiveToolsKeyPrefixVal
}

// NextAggregationKey returns the storage key for the next aggregation height.
func NextAggregationKey() []byte {
	return NextAggregationKeyVal
}

// ProcessedNoncesKeyPrefix returns the prefix for replay protection nonce records.
func ProcessedNoncesKeyPrefix() []byte {
	return ProcessedNoncesKeyPrefixVal
}

// CacheEntriesKeyPrefix returns the prefix for cache entry storage.
func CacheEntriesKeyPrefix() []byte {
	return CacheEntriesKeyPrefixVal
}

// CacheStatsKeyPrefix returns the prefix for per-tool cache metrics.
func CacheStatsKeyPrefix() []byte {
	return CacheStatsKeyPrefixVal
}

// QuotesKeyPrefix returns the prefix for quote records.
func QuotesKeyPrefix() []byte {
	return QuotesKeyPrefixVal
}

// SessionsKeyPrefix returns the prefix for session state records.
func SessionsKeyPrefix() []byte {
	return SessionsKeyPrefixVal
}

// QuoteSequenceKey returns the key for the quote sequence.
func QuoteSequenceKey() []byte {
	return QuoteSequenceKeyVal
}

// LastProcessedToolIDKey returns the key for the last processed tool metrics cursor.
func LastProcessedToolIDKey() []byte {
	return LastProcessedToolIDKeyVal
}

// LastProcessedSessionIDKey returns the key for the last processed session metrics cursor.
func LastProcessedSessionIDKey() []byte {
	return LastProcessedSessionIDKeyVal
}

// LastProcessedNonceKey returns the key for the last processed nonce cursor.
func LastProcessedNonceKey() []byte {
	return LastProcessedNonceKeyVal
}

// DiscoverySubsidySpentKey returns the storage key for cumulative discovery
// subsidy spend in the current governance accounting period.
func DiscoverySubsidySpentKey() []byte {
	return DiscoverySubsidySpentKeyVal
}

// DiscoverySubsidyResetKey returns the storage key for the next block at which
// the discovery subsidy spend counter resets.
func DiscoverySubsidyResetKey() []byte {
	return DiscoverySubsidyResetKeyVal
}

// NonceInvocationHashIndexKeyPrefix returns the prefix for the nonce invocation hash index.
func NonceInvocationHashIndexKeyPrefix() []byte {
	return NonceInvocationHashIndexPrefix
}

// CACCompositeKey returns the canonical key for an origin/consumer pair.
func CACCompositeKey(originToolID, consumingToolID string) string {
	return strings.TrimSpace(originToolID) + "::" + strings.TrimSpace(consumingToolID)
}
