//go:build cosmos

// Package types holds shared types and helpers for the credits module.
//
//revive:disable:var-naming // The module keeps its protobuf types under a canonical types package.
package types

const (
	// ModuleName defines the credits module name used across keepers and routing.
	ModuleName = "credits"
	// StoreKey specifies the primary KVStore key for the credits module.
	StoreKey = ModuleName
	// RouterKey identifies the message routing key for credits transactions.
	RouterKey = ModuleName
	// QuerierRoute sets the gRPC querier route for the credits module.
	QuerierRoute = ModuleName

	// ParamsPrefixByte prefixes parameter entries under the module store.
	ParamsPrefixByte = uint8(0x01)
	// LocksPrefixByte prefixes credit lock records in state.
	LocksPrefixByte = uint8(0x02)
	// LockSeqKeyPrefixByte prefixes auto-increment lock sequence counters.
	LockSeqKeyPrefixByte = uint8(0x03)
	// SettlementsPrefixByte prefixes settlement records emitted from receipts.
	SettlementsPrefixByte = uint8(0x04)
	// DisputesPrefixByte prefixes dispute tracking records for challenged receipts.
	DisputesPrefixByte = uint8(0x05)
	// MetricsPrefixByte prefixes aggregate module metrics in state.
	MetricsPrefixByte = uint8(0x06)
	// CACRoyaltyPrefixByte prefixes CAC royalty distribution records.
	CACRoyaltyPrefixByte = uint8(0x07)
	// CACStatsPrefixByte prefixes CAC aggregate statistics.
	CACStatsPrefixByte = uint8(0x08)
	// LockExpiryPrefixByte prefixes the lock expiration index.
	LockExpiryPrefixByte = uint8(0x09)
	// PendingSettlementsPrefixByte prefixes the pending settlements index.
	PendingSettlementsPrefixByte = uint8(0x0A)
	// SettlementsByTimePrefixByte prefixes the settlement completion time index.
	SettlementsByTimePrefixByte = uint8(0x0B)
	// FinalizedLocksPrefixByte prefixes the finalized lock index (for pruning).
	FinalizedLocksPrefixByte = uint8(0x0C)
	// LockReceiptsPrefixByte prefixes the lock-to-receipt binding map.
	LockReceiptsPrefixByte = uint8(0x0D)
	// LocksByQuotePrefixByte prefixes the quote-to-lock index for idempotency.
	LocksByQuotePrefixByte = uint8(0x0E)
	// CACSeqPrefixByte prefixes the CAC record sequence counter.
	CACSeqPrefixByte = uint8(0x0F)
)

var (
	// ParamsPrefix stores the binary prefix for parameters collections.
	ParamsPrefix = []byte{ParamsPrefixByte}
	// LocksPrefix stores the binary prefix for credit lock collections.
	LocksPrefix = []byte{LocksPrefixByte}
	// LockExpiryPrefix stores the prefix for the lock expiration index.
	LockExpiryPrefix = []byte{LockExpiryPrefixByte}
	// LockSeqKeyPrefix stores the prefix used for lock sequence counters.
	LockSeqKeyPrefix = []byte{LockSeqKeyPrefixByte}
	// SettlementPrefix stores the prefix for settlement record storage.
	SettlementPrefix = []byte{SettlementsPrefixByte}
	// PendingSettlementsPrefix stores the prefix for the pending settlements index.
	PendingSettlementsPrefix = []byte{PendingSettlementsPrefixByte}
	// SettlementsByTimePrefix stores the prefix for the settlement completion time index.
	SettlementsByTimePrefix = []byte{SettlementsByTimePrefixByte}
	// FinalizedLocksPrefix stores the prefix for the finalized lock index.
	FinalizedLocksPrefix = []byte{FinalizedLocksPrefixByte}
	// LockReceiptsPrefix stores the prefix for the lock-to-receipt binding map.
	LockReceiptsPrefix = []byte{LockReceiptsPrefixByte}
	// LocksByQuotePrefix stores the prefix for the quote-to-lock index.
	LocksByQuotePrefix = []byte{LocksByQuotePrefixByte}
	// DisputePrefix stores the prefix for dispute records.
	DisputePrefix = []byte{DisputesPrefixByte}
	// MetricsPrefix stores the prefix for aggregated metrics state.
	MetricsPrefix = []byte{MetricsPrefixByte}
	// CACRoyaltyPrefix stores the prefix for CAC royalty state entries.
	CACRoyaltyPrefix = []byte{CACRoyaltyPrefixByte}
	// CACStatsPrefix stores the prefix for CAC statistics entries.
	CACStatsPrefix = []byte{CACStatsPrefixByte}
	// CACSeqPrefix stores the prefix for the CAC record sequence counter.
	CACSeqPrefix = []byte{CACSeqPrefixByte}
)

// ModuleAccountName defines the credits module account identifier.
const ModuleAccountName = ModuleName
