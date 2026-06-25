package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
	"cosmossdk.io/core/store"
	sdkcodec "github.com/cosmos/cosmos-sdk/codec"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

// State encapsulates the router module's collections-based state management
type State struct {
	// Module parameters
	Params collections.Item[*types.Params]

	// Global metrics
	GlobalMetrics collections.Item[*types.GlobalMetrics]

	// Tool activation metrics
	ToolMetrics collections.Map[string, *types.ActivationMetrics]

	// Session metrics
	SessionMetrics collections.Map[string, *types.SessionMetrics]

	// Category metrics
	CategoryMetrics collections.Map[string, *types.CategoryMetrics]

	// Policy updates
	PolicyUpdates       collections.Map[uint64, *types.PolicyUpdate]
	PolicyUpdateCounter collections.Sequence

	// CAC royalty records
	CACRecords       *collections.IndexedMap[string, *types.CACRoyaltyRecord, CACIndexes]
	CACRecordCounter collections.Sequence

	// Tool selection scores
	SelectionScores collections.Map[string, *types.ToolSelectionScore]

	// Active tools by session
	ActiveTools *collections.IndexedMap[string, *types.ToolActivation, ToolActivationIndexes]

	// Next aggregation block height
	NextAggregation collections.Item[uint64]

	// Cursors for bounded aggregation
	LastProcessedToolID    collections.Item[string]
	LastProcessedSessionID collections.Item[string]
	LastProcessedNonce     collections.Item[string]
	DiscoverySubsidySpent  collections.Item[string]
	DiscoverySubsidyReset  collections.Item[uint64]

	// Replay protection nonces (Phase 2.11.7.2)
	ProcessedNonces *collections.IndexedMap[string, *types.NonceRecord, NonceIndexes]

	// Cached invocation entries
	CacheEntries collections.Map[string, *types.CacheEntry]

	// Cache statistics per tool
	CacheStats collections.Map[string, *types.CacheStats]

	// Stored quotes keyed by quote ID
	Quotes collections.Map[string, *types.QuoteRecord]

	// Quote sequence for collision-free IDs
	QuoteSequence collections.Sequence

	// Session state records keyed by session ID
	Sessions collections.Map[string, *types.SessionState]
}

// CACIndexes defines secondary indexes for CAC royalty records
type CACIndexes struct {
	Origin      *indexes.Multi[string, string, *types.CACRoyaltyRecord]
	Consumer    *indexes.Multi[string, string, *types.CACRoyaltyRecord]
	BlockHeight *indexes.Multi[uint64, string, *types.CACRoyaltyRecord]
	Composite   *indexes.Unique[string, string, *types.CACRoyaltyRecord]
}

// IndexesList returns the set of secondary indexes backing the CAC records map.
func (i CACIndexes) IndexesList() []collections.Index[string, *types.CACRoyaltyRecord] {
	return []collections.Index[string, *types.CACRoyaltyRecord]{
		i.Origin,
		i.Consumer,
		i.BlockHeight,
		i.Composite,
	}
}

// NewCACIndexes constructs CACIndexes wiring origin, consumer, and block height views.
func NewCACIndexes(sb *collections.SchemaBuilder) CACIndexes {
	return CACIndexes{
		Origin: indexes.NewMulti(
			sb, types.CACOriginIndexPrefix, "cac_by_origin",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.CACRoyaltyRecord) (string, error) {
				return v.OriginToolId, nil
			},
		),
		Consumer: indexes.NewMulti(
			sb, types.CACConsumerIndexPrefix, "cac_by_consumer",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.CACRoyaltyRecord) (string, error) {
				return v.ConsumingToolId, nil
			},
		),
		BlockHeight: indexes.NewMulti(
			sb, types.CACBlockIndexPrefix, "cac_by_block",
			collections.Uint64Key, collections.StringKey,
			func(_ string, v *types.CACRoyaltyRecord) (uint64, error) {
				return v.BlockHeight, nil
			},
		),
		Composite: indexes.NewUnique(
			sb, types.CACCompositeIndexPrefix(), "cac_by_composite",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.CACRoyaltyRecord) (string, error) {
				return types.CACCompositeKey(v.OriginToolId, v.ConsumingToolId), nil
			},
		),
	}
}

// ToolActivationIndexes defines secondary indexes for tool activations
type ToolActivationIndexes struct {
	Session *indexes.Multi[string, string, *types.ToolActivation]
	ToolID  *indexes.Multi[string, string, *types.ToolActivation]
	Active  *indexes.Multi[bool, string, *types.ToolActivation]
}

// IndexesList returns the secondary indexes used to query tool activations.
func (i ToolActivationIndexes) IndexesList() []collections.Index[string, *types.ToolActivation] {
	return []collections.Index[string, *types.ToolActivation]{
		i.Session,
		i.ToolID,
		i.Active,
	}
}

// NewToolActivationIndexes constructs indexes for session, tool, and active filters.
func NewToolActivationIndexes(sb *collections.SchemaBuilder) ToolActivationIndexes {
	return ToolActivationIndexes{
		Session: indexes.NewMulti(
			sb, types.ActivationSessionIndexPrefix, "activation_by_session",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.ToolActivation) (string, error) {
				return v.GetSessionId(), nil
			},
		),
		ToolID: indexes.NewMulti(
			sb, types.ActivationToolIndexPrefix, "activation_by_tool",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.ToolActivation) (string, error) {
				return v.GetToolId(), nil
			},
		),
		Active: indexes.NewMulti(
			sb, types.ActivationActiveIndexPrefix, "activation_by_active",
			collections.BoolKey, collections.StringKey,
			func(_ string, v *types.ToolActivation) (bool, error) {
				return v.GetActive(), nil
			},
		),
	}
}

// NonceIndexes defines secondary indexes for nonce records
type NonceIndexes struct {
	InvocationHash *indexes.Multi[string, string, *types.NonceRecord]
}

// IndexesList returns the secondary indexes used to query nonces.
func (i NonceIndexes) IndexesList() []collections.Index[string, *types.NonceRecord] {
	return []collections.Index[string, *types.NonceRecord]{
		i.InvocationHash,
	}
}

// NewNonceIndexes constructs indexes for invocation hash.
func NewNonceIndexes(sb *collections.SchemaBuilder) NonceIndexes {
	return NonceIndexes{
		InvocationHash: indexes.NewMulti(
			sb, types.NonceInvocationHashIndexKeyPrefix(), "nonce_by_invocation_hash",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.NonceRecord) (string, error) {
				return v.InvocationHash, nil
			},
		),
	}
}

// NewState creates a new State instance with all collections initialized
func NewState(cdc sdkcodec.BinaryCodec, storeService store.KVStoreService) State {
	sb := collections.NewSchemaBuilder(storeService)

	// Initialize indexes
	cacIndexes := NewCACIndexes(sb)
	activationIndexes := NewToolActivationIndexes(sb)
	nonceIndexes := NewNonceIndexes(sb)

	state := State{
		Params: collections.NewItem(
			sb,
			types.ParamsKeyPrefix(),
			"params",
			collPtrValue[types.Params](cdc),
		),
		GlobalMetrics: collections.NewItem(
			sb,
			types.GlobalMetricsKeyPrefix(),
			"global_metrics",
			collPtrValue[types.GlobalMetrics](cdc),
		),
		ToolMetrics: collections.NewMap(
			sb,
			types.ToolMetricsKeyPrefix(),
			"tool_metrics",
			collections.StringKey,
			collPtrValue[types.ActivationMetrics](cdc),
		),
		SessionMetrics: collections.NewMap(
			sb,
			types.SessionMetricsKeyPrefix(),
			"session_metrics",
			collections.StringKey,
			collPtrValue[types.SessionMetrics](cdc),
		),
		CategoryMetrics: collections.NewMap(
			sb,
			types.CategoryMetricsKeyPrefix(),
			"category_metrics",
			collections.StringKey,
			collPtrValue[types.CategoryMetrics](cdc),
		),
		PolicyUpdates: collections.NewMap(
			sb,
			types.PolicyUpdatesKeyPrefix(),
			"policy_updates",
			collections.Uint64Key,
			collPtrValue[types.PolicyUpdate](cdc),
		),
		PolicyUpdateCounter: collections.NewSequence(
			sb,
			types.PolicyUpdateCounterKey(),
			"policy_update_counter",
		),
		CACRecordCounter: collections.NewSequence(
			sb,
			types.CACRecordCounterKey(),
			"cac_record_counter",
		),
		SelectionScores: collections.NewMap(
			sb,
			types.SelectionScoresKeyPrefix(),
			"selection_scores",
			collections.StringKey,
			collPtrValue[types.ToolSelectionScore](cdc),
		),
		NextAggregation: collections.NewItem(
			sb,
			types.NextAggregationKey(),
			"next_aggregation",
			collections.Uint64Value,
		),
		LastProcessedToolID: collections.NewItem(
			sb,
			types.LastProcessedToolIDKey(),
			"last_processed_tool_id",
			collections.StringValue,
		),
		LastProcessedSessionID: collections.NewItem(
			sb,
			types.LastProcessedSessionIDKey(),
			"last_processed_session_id",
			collections.StringValue,
		),
		LastProcessedNonce: collections.NewItem(
			sb,
			types.LastProcessedNonceKey(),
			"last_processed_nonce",
			collections.StringValue,
		),
		DiscoverySubsidySpent: collections.NewItem(
			sb,
			types.DiscoverySubsidySpentKey(),
			"discovery_subsidy_spent",
			collections.StringValue,
		),
		DiscoverySubsidyReset: collections.NewItem(
			sb,
			types.DiscoverySubsidyResetKey(),
			"discovery_subsidy_next_reset",
			collections.Uint64Value,
		),
		CacheEntries: collections.NewMap(
			sb,
			types.CacheEntriesKeyPrefix(),
			"cache_entries",
			collections.StringKey,
			collPtrValue[types.CacheEntry](cdc),
		),
		CacheStats: collections.NewMap(
			sb,
			types.CacheStatsKeyPrefix(),
			"cache_stats",
			collections.StringKey,
			collPtrValue[types.CacheStats](cdc),
		),
		Quotes: collections.NewMap(
			sb,
			types.QuotesKeyPrefix(),
			"quotes",
			collections.StringKey,
			collPtrValue[types.QuoteRecord](cdc),
		),
		QuoteSequence: collections.NewSequence(
			sb,
			types.QuoteSequenceKey(),
			"quote_sequence",
		),
		Sessions: collections.NewMap(
			sb,
			types.SessionsKeyPrefix(),
			"sessions",
			collections.StringKey,
			collPtrValue[types.SessionState](cdc),
		),
	}

	// Create indexed maps
	state.CACRecords = collections.NewIndexedMap(
		sb,
		types.CACRecordsKeyPrefix(),
		"cac_records_indexed",
		collections.StringKey,
		collPtrValue[types.CACRoyaltyRecord](cdc),
		cacIndexes,
	)

	state.ActiveTools = collections.NewIndexedMap(
		sb,
		types.ActiveToolsKeyPrefix(),
		"active_tools_indexed",
		collections.StringKey,
		collPtrValue[types.ToolActivation](cdc),
		activationIndexes,
	)

	state.ProcessedNonces = collections.NewIndexedMap(
		sb,
		types.ProcessedNoncesKeyPrefix(),
		"processed_nonces",
		collections.StringKey,
		collPtrValue[types.NonceRecord](cdc),
		nonceIndexes,
	)

	// Build the schema
	if _, err := sb.Build(); err != nil {
		panic(err)
	}

	return state
}
