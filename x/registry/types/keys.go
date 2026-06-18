
package types

import (
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// ModuleName defines the module name
	ModuleName = "registry"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_registry"

	// Slash restitution routing (specs/governance/slashing-rules.md
	// §"Restitution Routing"). Shares are in basis points and must sum to
	// exactly 10_000. The burn share is hard-coded per spec ("5% burned to
	// prevent governance gaming incentives") and MUST NOT be parameterised;
	// surfacing these as types-level consts (rather than bond.go local
	// constants) lets genesis validation and cross-package callers assert
	// the sum-to-10000 invariant against a single source of truth.
	//
	// SlashRestitutionUserBps is the share staged as impacted-user credit in
	// the insurance pool (60% per spec).
	SlashRestitutionUserBps uint32 = 6000
	// SlashRestitutionInsuranceBps is the share routed to the insurance
	// reserve for pool replenishment (25% per spec).
	SlashRestitutionInsuranceBps uint32 = 2500
	// SlashRestitutionTreasuryBps is the share routed to the governance
	// treasury (10% per spec) via the Cosmos SDK fee_collector module.
	SlashRestitutionTreasuryBps uint32 = 1000
	// SlashRestitutionBurnBps is the share destroyed to prevent governance
	// from slashing for treasury gain (5%, immutable per spec).
	SlashRestitutionBurnBps uint32 = 500

	// SlashInsuranceModule is the destination module account for user
	// restitution and insurance reserve portions. Declared as a string
	// literal rather than importing x/insurance/types so the cosmos-tag
	// registry build does not pull in the cosmos_full-tagged insurance
	// package.
	SlashInsuranceModule = "insurance"
	// SlashTreasuryModule is the destination module account for the
	// governance treasury portion. fee_collector is the canonical Cosmos SDK
	// treasury module account (authtypes.FeeCollectorName).
	SlashTreasuryModule = "fee_collector"
)

// Store key prefixes
var (
	// ToolCardPrefix is the prefix for tool card storage
	ToolCardPrefix = []byte{0x01}

	// ToolCapsulePrefix is the prefix for tool capsule storage
	ToolCapsulePrefix = []byte{0x14}

	// BondRecordPrefix is the prefix for bond record storage
	BondRecordPrefix = []byte{0x02}

	// ReceiptPrefix is the prefix for receipt storage
	ReceiptPrefix = []byte{0x03}

	// SettlementPrefix is the prefix for settlement record storage
	SettlementPrefix = []byte{0x04}

	// RoyaltyTotalsPrefix stores aggregated CAC royalty state
	RoyaltyTotalsPrefix = []byte{0x05}

	// MetricsPrefix is the prefix for tool metrics
	MetricsPrefix = []byte{0x06}

	// ChallengePrefix is the prefix for challenge storage
	ChallengePrefix = []byte{0x10}

	// ParamsKey is the key for module parameters
	ParamsKey = []byte{0x11}

	// InsurancePoolKey is the key for insurance pool storage
	InsurancePoolKey = []byte{0x07}

	// ReceiptByStatusPrefix is the prefix for indexing receipts by status
	ReceiptByStatusPrefix = []byte{0x08}

	// ToolByOwnerPrefix is the prefix for indexing tools by owner
	ToolByOwnerPrefix = []byte{0x09}

	// ReceiptByToolPrefix is the prefix for indexing receipts by tool
	ReceiptByToolPrefix = []byte{0x0A}

	// PendingReceiptPrefix is the prefix for pending receipts
	PendingReceiptPrefix = []byte{0x0B}

	// SettleableReceiptPrefix is the prefix for settleable receipts
	SettleableReceiptPrefix = []byte{0x0C}

	// EconomicsMetricsKey is the singleton key storing rolling insurance analytics
	EconomicsMetricsKey = []byte{0x0D}

	// ActivationSetPrefix is the prefix for activation set storage
	ActivationSetPrefix = []byte{0x0E}

	// ActivationHistoryPrefix is the prefix for activation history storage
	ActivationHistoryPrefix = []byte{0x0F}

	// CategoryCountsPrefix is the prefix for category count storage
	CategoryCountsPrefix = []byte{0x12}

	// TotalActiveToolsPrefix is the prefix for total active tools count
	TotalActiveToolsPrefix = []byte{0x13}

	// LastSLAEvaluatedToolPrefix is the prefix for the SLA evaluation cursor
	LastSLAEvaluatedToolPrefix = []byte{0x22}

	// LastProcessedChallengePrefix is the prefix for the challenge expiration cursor
	LastProcessedChallengePrefix = []byte{0x23}

	// Parameter store keys
	KeyMinBondAmount           = []byte("MinBondAmount")
	KeyMaxActiveTools          = []byte("MaxActiveTools")
	KeyInsuranceBPS            = []byte("InsuranceBPS")
	KeyCACRoyaltyBPS           = []byte("CACRoyaltyBPS")
	KeyQuoteTTLSeconds         = []byte("QuoteTTLSeconds")
	KeySettlementPeriodBlocks  = []byte("SettlementPeriodBlocks")
	KeyMaxToolsPerCategory     = []byte("MaxToolsPerCategory")
	KeyMinReputation           = []byte("MinReputation")
	KeySlashingGracePeriod     = []byte("SlashingGracePeriod")
	KeyMaxJurisdictions        = []byte("MaxJurisdictions")
	KeyDisputeWindowSeconds    = []byte("DisputeWindowSeconds")
	KeyBurnRateSpendBPS        = []byte("BurnRateSpendBPS")
	KeyBurnRateAcqBPS          = []byte("BurnRateAcqBPS")
	KeyInsurancePoolBPS        = []byte("InsurancePoolBPS")
	KeyQuoteErrorBPS           = []byte("QuoteErrorBPS")
	KeyDisputeStakeLAC         = []byte("DisputeStakeLAC")
	KeyMinBondByCategory       = []byte("MinBondByCategory")
	KeySLOThresholds           = []byte("SLOThresholds")
	KeyQualityRebateBPS        = []byte("QualityRebateBPS")
	KeyCachePolicyDefaults     = []byte("CachePolicyDefaults")
	KeyRevenueSplits           = []byte("RevenueSplits")
	KeyInsuranceTargetUtil     = []byte("InsuranceTargetUtil")
	KeyPremiumAdjustmentBPS    = []byte("PremiumAdjustmentBPS")
	KeyCacheFeeSplit           = []byte("CacheFeeSplit")
	KeyAttestationTTLSeconds   = []byte("AttestationTTLSeconds")
	KeySBOMStalenessSeconds    = []byte("SBOMStalenessSeconds")
	KeyMaxSettlementsPerBlock  = []byte("MaxSettlementsPerBlock")
	KeyMaxReceiptsScanPerBlock = []byte("MaxReceiptsScanPerBlock")

	// SLA slashing parameters
	KeySLASlashGammaBps           = []byte("SLASlashGammaBps")
	KeySLAP95LatencyConsecutive   = []byte("SLAP95LatencyConsecutive")
	KeySLADisputeRateBps          = []byte("SLADisputeRateBps")
	KeySLAMinCalls                = []byte("SLAMinCalls")
	KeySLAMaxSlashPerEpochBps     = []byte("SLAMaxSlashPerEpochBps")
	KeySLARecidivistMultiplierBps = []byte("SLARecidivistMultiplierBps")

	// Challenge resolution deadline
	KeyChallengeResolutionDeadlineSeconds = []byte("ChallengeResolutionDeadlineSeconds")

	// Settled receipt retention
	KeySettledReceiptRetentionSeconds = []byte("SettledReceiptRetentionSeconds")

	// Verified badge scoring parameters
	KeyVerifiedBadgeParams = []byte("VerifiedBadgeParams")

	// Recursive royalty guardrail parameters
	KeyRecursiveRoyaltyMaxDepth        = []byte("RecursiveRoyaltyMaxDepth")
	KeyRecursiveRoyaltyMaxAggregateBps = []byte("RecursiveRoyaltyMaxAggregateBps")
)

// GetToolCardKey returns the store key for a tool card
func GetToolCardKey(toolID string) []byte {
	return append(ToolCardPrefix, []byte(toolID)...)
}

// GetToolCapsuleKey returns the store key for a tool capsule.
func GetToolCapsuleKey(toolID string) []byte {
	return append(ToolCapsulePrefix, []byte(toolID)...)
}

// GetBondRecordKey returns the store key for a bond record
func GetBondRecordKey(toolID string) []byte {
	return append(BondRecordPrefix, []byte(toolID)...)
}

// GetReceiptKey returns the store key for a receipt
func GetReceiptKey(receiptID string) []byte {
	return append(ReceiptPrefix, []byte(receiptID)...)
}

// GetSettlementKey returns the store key for a settlement record
func GetSettlementKey(receiptID string) []byte {
	return append(SettlementPrefix, []byte(receiptID)...)
}

// GetRoyaltyTotalsKey returns the store key for royalty aggregates
func GetRoyaltyTotalsKey(toolID string) []byte {
	return append(RoyaltyTotalsPrefix, []byte(toolID)...)
}

// GetMetricsKey returns the store key for tool metrics
func GetMetricsKey(toolID string) []byte {
	return append(MetricsPrefix, []byte(toolID)...)
}

// GetChallengeKey returns the store key for a challenge
func GetChallengeKey(challengeID string) []byte {
	return append(ChallengePrefix, []byte(challengeID)...)
}

// GetEconomicsMetricsKey returns the singleton key for economics metrics
func GetEconomicsMetricsKey() []byte {
	return EconomicsMetricsKey
}

// GetBondKey returns the store key for a bond (alias for GetBondRecordKey)
func GetBondKey(toolID string) []byte {
	return GetBondRecordKey(toolID)
}

// GetActivationSetKey returns the store key for activation set membership
func GetActivationSetKey(toolID string) []byte {
	return append(ActivationSetPrefix, []byte(toolID)...)
}

// GetActivationHistoryKey returns the store key for activation history
func GetActivationHistoryKey(height int64, toolID string) []byte {
	heightBytes := sdk.Uint64ToBigEndian(uint64(height)) //#nosec G115 -- block heights guaranteed non-negative by Cosmos SDK
	key := append(make([]byte, 0, len(ActivationHistoryPrefix)+len(heightBytes)+len(toolID)), ActivationHistoryPrefix...)
	key = append(key, heightBytes...)
	return append(key, []byte(toolID)...)
}

// Additional key prefixes for registry implementation
var (
	// ToolKeyPrefix for tool storage
	ToolKeyPrefix = []byte{0x20}

	// StatsKeyPrefix for publisher stats
	StatsKeyPrefix = []byte{0x21}

	// ReceiptKeyPrefix for pending receipts
	ReceiptKeyPrefix = []byte{0x22}

	// ChallengeKeyPrefix for challenges
	ChallengeKeyPrefix = []byte{0x23}

	// AuditKeyPrefix for audit events
	AuditKeyPrefix = []byte{0x24}

	// SettlementKeyPrefix for settlements
	SettlementKeyPrefix = []byte{0x25}

	// QueuedReceiptPrefix for queued receipts with metadata
	QueuedReceiptPrefix = []byte{0x26}

	// ReadyIndexPrefix for time-based ready index
	ReadyIndexPrefix = []byte{0x27}

	// QueueSequenceKey for global queue sequence number
	QueueSequenceKey = []byte{0x28}

	// BundleAnchorPrefix stores on-chain receipt bundle merkle root anchors.
	BundleAnchorPrefix = []byte{0x29}

	// ChallengeSequenceKey for deterministic challenge sequence number
	ChallengeSequenceKey = []byte{0x2A}

	// SLATemplatePrefix stores SLA template documents by sla_id.
	SLATemplatePrefix = []byte{0x40}

	// DisputeTermsPrefix stores dispute terms documents by dispute_terms_id.
	DisputeTermsPrefix = []byte{0x41}

	// LaneRegistryPrefix stores lane registry entries by lane_id.
	LaneRegistryPrefix = []byte{0x42}

	// WatcherPrefix stores watcher registry entries by watcher address.
	WatcherPrefix = []byte{0x43}

	// SLOProbeReceiptPrefix stores watcher SLO probe receipts by receipt ID.
	SLOProbeReceiptPrefix = []byte{0x44}

	// SLOProbeAggregatePrefix stores aggregated SLO probe results by tool/window key.
	SLOProbeAggregatePrefix = []byte{0x45}

	// SLOProbeToolIndexPrefix indexes SLO probe receipts by tool ID.
	SLOProbeToolIndexPrefix = []byte{0x46}

	// SLOProbeWindowIndexPrefix indexes SLO probe receipts by window end unix time.
	SLOProbeWindowIndexPrefix = []byte{0x47}

	// ReceiptMirrorStatePrefix stores mirror state snapshots keyed by dedup key.
	ReceiptMirrorStatePrefix = []byte{0x48}

	// ReceiptMirrorReceiptIndexPrefix indexes mirror dedup keys by receipt_id.
	ReceiptMirrorReceiptIndexPrefix = []byte{0x49}

	// ReceiptMirrorSequenceIndexPrefix indexes mirror dedup keys by channel_id + sequence.
	ReceiptMirrorSequenceIndexPrefix = []byte{0x4A}

	// OriginRoutingConfigPrefix stores per-origin routing configs keyed by origin_id.
	OriginRoutingConfigPrefix = []byte{0x4B}

	// EvidenceBundleMirrorStatePrefix stores evidence mirror state snapshots keyed by dedup key.
	EvidenceBundleMirrorStatePrefix = []byte{0x4C}

	// EvidenceBundleMirrorSubjectIndexPrefix indexes evidence mirror dedup keys by subject_kind + subject_id.
	EvidenceBundleMirrorSubjectIndexPrefix = []byte{0x4D}

	// EvidenceBundleMirrorSequenceIndexPrefix indexes evidence mirror dedup keys by channel_id + sequence.
	EvidenceBundleMirrorSequenceIndexPrefix = []byte{0x4E}

	// Secondary index prefixes for ToolCard (Collections IndexedMap)
	ToolCardOwnerIndexPrefix       = []byte{0x30}
	ToolCardCategoryIndexPrefix    = []byte{0x31}
	ToolCardLicenseLaneIndexPrefix = []byte{0x32}
	ToolCardStatusIndexPrefix      = []byte{0x33}

	// Secondary index prefixes for BondRecord (Collections IndexedMap)
	BondRecordOwnerIndexPrefix  = []byte{0x34}
	BondRecordStatusIndexPrefix = []byte{0x35}

	// Secondary index prefixes for Challenge (Collections IndexedMap)
	ChallengeChallengerIndexPrefix = []byte{0x36}
	ChallengeReceiptIndexPrefix    = []byte{0x37}
	ChallengeStatusIndexPrefix     = []byte{0x38}

	// Secondary index prefixes for queued receipts (Collections KeySet indexes).
	QueuedReceiptStatusIndexPrefix = []byte{0x39}
	QueuedReceiptToolIndexPrefix   = []byte{0x3A}
	QueuedReceiptRouterIndexPrefix = []byte{0x3B}
	QueuedReceiptUserIndexPrefix   = []byte{0x3C}

	// SettledDateIndexPrefix indexes settled/failed receipts by processed_at time.
	SettledDateIndexPrefix = []byte{0x3D}
)

// GetToolKey returns the store key for a tool
func GetToolKey(toolID string) []byte {
	return append(ToolKeyPrefix, []byte(toolID)...)
}

// GetStatsKey returns the store key for stats
func GetStatsKey(toolID string) []byte {
	return append(StatsKeyPrefix, []byte(toolID)...)
}

// GetAuditKey returns the store key for an audit event
func GetAuditKey(height int64, toolID string) []byte {
	heightBytes := sdk.Uint64ToBigEndian(uint64(height)) //#nosec G115 -- block heights guaranteed non-negative by Cosmos SDK
	key := append(make([]byte, 0, len(AuditKeyPrefix)+len(heightBytes)+len(toolID)), AuditKeyPrefix...)
	key = append(key, heightBytes...)
	return append(key, []byte(toolID)...)
}

// GetReceiptMirrorStateKey returns the store key for mirror state by dedup key.
func GetReceiptMirrorStateKey(dedupKey string) []byte {
	return append(ReceiptMirrorStatePrefix, []byte(dedupKey)...)
}

// GetReceiptMirrorReceiptIndexKey returns the store key for receipt_id -> dedup key lookups.
func GetReceiptMirrorReceiptIndexKey(receiptID string) []byte {
	return append(ReceiptMirrorReceiptIndexPrefix, []byte(receiptID)...)
}

// GetReceiptMirrorSequenceIndexKey returns the store key for channel/sequence -> dedup key lookups.
func GetReceiptMirrorSequenceIndexKey(channelID string, sequence uint64) []byte {
	channelBytes := []byte(channelID)
	sequenceBytes := sdk.Uint64ToBigEndian(sequence)
	key := append(make([]byte, 0, len(ReceiptMirrorSequenceIndexPrefix)+len(channelBytes)+1+len(sequenceBytes)), ReceiptMirrorSequenceIndexPrefix...)
	key = append(key, channelBytes...)
	key = append(key, '|')
	key = append(key, sequenceBytes...)
	return key
}

// GetEvidenceBundleMirrorStateKey returns the store key for evidence mirror state by dedup key.
func GetEvidenceBundleMirrorStateKey(dedupKey string) []byte {
	return append(EvidenceBundleMirrorStatePrefix, []byte(dedupKey)...)
}

// GetEvidenceBundleMirrorSubjectIndexKey returns the store key for subject lookup (subject_kind + subject_id).
func GetEvidenceBundleMirrorSubjectIndexKey(subjectKind, subjectID string) []byte {
	key := strings.ToLower(strings.TrimSpace(subjectKind)) + "|" + strings.TrimSpace(subjectID)
	return append(EvidenceBundleMirrorSubjectIndexPrefix, []byte(key)...)
}

// GetEvidenceBundleMirrorSequenceIndexKey returns the store key for channel/sequence -> dedup key lookups.
func GetEvidenceBundleMirrorSequenceIndexKey(channelID string, sequence uint64) []byte {
	channelBytes := []byte(channelID)
	sequenceBytes := sdk.Uint64ToBigEndian(sequence)
	key := append(make([]byte, 0, len(EvidenceBundleMirrorSequenceIndexPrefix)+len(channelBytes)+1+len(sequenceBytes)), EvidenceBundleMirrorSequenceIndexPrefix...)
	key = append(key, channelBytes...)
	key = append(key, '|')
	key = append(key, sequenceBytes...)
	return key
}
