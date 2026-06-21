// Package types holds shared types and helpers for the incentives module.
//
//revive:disable:var-naming // The module keeps its protobuf types under a canonical types package.
package types

const (
	// ModuleName defines the incentives module name used across keepers and routing.
	ModuleName = "incentives"
	// StoreKey specifies the primary KVStore key for the incentives module.
	StoreKey = ModuleName
	// RouterKey identifies the message routing key for incentives transactions.
	RouterKey = ModuleName
	// QuerierRoute sets the gRPC querier route for the incentives module.
	QuerierRoute = ModuleName

	// ParamsPrefixByte prefixes parameter entries under the module store.
	ParamsPrefixByte = uint8(0x01)
	// BadgesPrefixByte prefixes badge records in state.
	BadgesPrefixByte = uint8(0x02)
	// TierConfigsPrefixByte prefixes tier configuration records.
	TierConfigsPrefixByte = uint8(0x03)
	// MetricSnapshotsPrefixByte prefixes metric snapshot records.
	MetricSnapshotsPrefixByte = uint8(0x04)
	// BadgeEventsPrefixByte prefixes badge event audit trail records.
	BadgeEventsPrefixByte = uint8(0x05)
	// BadgesByTierPrefixByte prefixes the secondary index of badges by tier.
	BadgesByTierPrefixByte = uint8(0x06)
	// BadgesByPublisherPrefixByte prefixes the secondary index of badges by publisher.
	BadgesByPublisherPrefixByte = uint8(0x07)
	// LastEvaluationBlockPrefixByte prefixes the last evaluation block height per tool.
	LastEvaluationBlockPrefixByte = uint8(0x08)
	// LastExpiredBadgeCursorPrefixByte prefixes the badge expiration cursor.
	LastExpiredBadgeCursorPrefixByte = uint8(0x09)
)

var (
	// ParamsPrefix stores the binary prefix for parameters collections.
	ParamsPrefix = []byte{ParamsPrefixByte}
	// BadgesPrefix stores the binary prefix for badge collections.
	BadgesPrefix = []byte{BadgesPrefixByte}
	// TierConfigsPrefix stores the binary prefix for tier configuration collections.
	TierConfigsPrefix = []byte{TierConfigsPrefixByte}
	// MetricSnapshotsPrefix stores the prefix for metric snapshot storage.
	MetricSnapshotsPrefix = []byte{MetricSnapshotsPrefixByte}
	// BadgeEventsPrefix stores the prefix for badge event audit trail.
	BadgeEventsPrefix = []byte{BadgeEventsPrefixByte}
	// BadgesByTierPrefix stores the prefix for badges-by-tier index.
	BadgesByTierPrefix = []byte{BadgesByTierPrefixByte}
	// BadgesByPublisherPrefix stores the prefix for badges-by-publisher index.
	BadgesByPublisherPrefix = []byte{BadgesByPublisherPrefixByte}
	// LastEvaluationBlockPrefix stores the prefix for last evaluation block tracking.
	LastEvaluationBlockPrefix = []byte{LastEvaluationBlockPrefixByte}
	// LastExpiredBadgeCursorPrefix stores the prefix for the badge expiration cursor.
	LastExpiredBadgeCursorPrefix = []byte{LastExpiredBadgeCursorPrefixByte}
)

// ModuleAccountName defines the incentives module account identifier.
const ModuleAccountName = ModuleName
