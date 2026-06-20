
package types

const (
	// ModuleName defines the module name
	ModuleName = "insurance"

	// ModuleAccountName defines the module account name
	ModuleAccountName = ModuleName

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey is the message route for the insurance module
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_" + ModuleName
)

// Store key prefixes
var (
	// PoolKey is the key for the insurance pool
	PoolKey = []byte{0x01}

	// BondPrefix stores SLA bond records (todo_bonds feature gate).
	BondPrefix = []byte{0x1C}

	// ClaimsKeyPrefix is the prefix for all claims
	ClaimsKeyPrefix = []byte{0x02}

	// ContributionsKeyPrefix is the prefix for all contributions
	ContributionsKeyPrefix = []byte{0x03}

	// PublisherRiskKeyPrefix is the prefix for publisher risk profiles
	PublisherRiskKeyPrefix = []byte{0x04}

	// PayoutsKeyPrefix is the prefix for all payouts
	PayoutsKeyPrefix = []byte{0x05}

	// ParamsKey is the key for module parameters
	ParamsKey = []byte{0x06}

	// MetricsKey is the key for insurance metrics
	MetricsKey = []byte{0x07}

	// ClaimsByReceiptIndexPrefix indexes claims by receipt ID
	ClaimsByReceiptIndexPrefix = []byte{0x08}

	// ClaimsByStatusIndexPrefix indexes claims by status
	ClaimsByStatusIndexPrefix = []byte{0x09}

	// ClaimSequenceKey is the key for the global claim ID sequence
	ClaimSequenceKey = []byte{0x0A}

	// ContributionSequenceKey is the key for the global contribution ID sequence
	ContributionSequenceKey = []byte{0x0B}

	// PayoutSequenceKey is the key for the global payout ID sequence
	PayoutSequenceKey = []byte{0x0C}

	// Index prefixes for secondary indexes
	ClaimReceiptIndexPrefix     = []byte{0x10}
	ClaimClaimantIndexPrefix    = []byte{0x11}
	ClaimPublisherIndexPrefix   = []byte{0x12}
	ClaimStatusIndexPrefix      = []byte{0x13}
	ContribReceiptIndexPrefix   = []byte{0x14}
	ContribPublisherIndexPrefix = []byte{0x15}
	ContribToolIndexPrefix      = []byte{0x16}
	PayoutClaimIndexPrefix      = []byte{0x17}
	PayoutRecipientIndexPrefix  = []byte{0x18}
	PayoutStatusIndexPrefix     = []byte{0x19}
	PoolBalanceKey              = []byte{0x1A}
	PoolMetricsKeyVal           = []byte{0x1B}
	ClaimCreatedIndexPrefix     = []byte{0x20}
	ReceiptOwnersKeyPrefix      = []byte{0x30}
)

// GetClaimKey returns the store key for a claim
func GetClaimKey(claimID string) []byte {
	return append(ClaimsKeyPrefix, []byte(claimID)...)
}

// GetContributionKey returns the store key for a contribution
func GetContributionKey(contributionID string) []byte {
	return append(ContributionsKeyPrefix, []byte(contributionID)...)
}

// GetPublisherRiskKey returns the store key for a publisher's risk profile
func GetPublisherRiskKey(publisherID string) []byte {
	return append(PublisherRiskKeyPrefix, []byte(publisherID)...)
}

// GetPayoutKey returns the store key for a payout
func GetPayoutKey(payoutID string) []byte {
	return append(PayoutsKeyPrefix, []byte(payoutID)...)
}

// GetClaimByReceiptIndexKey returns the index key for claims by receipt
func GetClaimByReceiptIndexKey(receiptID, claimID string) []byte {
	return append(append(ClaimsByReceiptIndexPrefix, []byte(receiptID)...), []byte(claimID)...)
}

// GetClaimByStatusIndexKey returns the index key for claims by status
// Note: ClaimStatus is now defined in insurance.pb.go
func GetClaimByStatusIndexKey(status string, claimID string) []byte {
	return append(append(ClaimsByStatusIndexPrefix, []byte(status)...), []byte(claimID)...)
}

// ParamsKeyPrefix returns the collections prefix used for module parameters.
func ParamsKeyPrefix() []byte {
	return ParamsKey
}

// PoolBalanceKeyPrefix returns the prefix for pool balance data.
func PoolBalanceKeyPrefix() []byte {
	return PoolBalanceKey
}

// PoolMetricsKeyPrefix returns the prefix for pool metrics state.
func PoolMetricsKeyPrefix() []byte {
	return PoolMetricsKeyVal
}

// GetClaimsKeyPrefix returns the claims store prefix.
func GetClaimsKeyPrefix() []byte {
	return ClaimsKeyPrefix
}

// GetContributionsKeyPrefix returns the contributions store prefix.
func GetContributionsKeyPrefix() []byte {
	return ContributionsKeyPrefix
}

// PublisherRisksKeyPrefix returns the publisher risk profile prefix.
func PublisherRisksKeyPrefix() []byte {
	return PublisherRiskKeyPrefix
}

// GetPayoutsKeyPrefix returns the payouts store prefix.
func GetPayoutsKeyPrefix() []byte {
	return PayoutsKeyPrefix
}

// GetReceiptOwnersKeyPrefix returns the prefix for receipt ownership records.
func GetReceiptOwnersKeyPrefix() []byte {
	return ReceiptOwnersKeyPrefix
}

// ClaimCounterKey returns the storage key for the claim counter.
func ClaimCounterKey() []byte {
	return ClaimSequenceKey
}

// ContribCounterKey returns the storage key for the contribution counter.
func ContribCounterKey() []byte {
	return ContributionSequenceKey
}

// PayoutCounterKey returns the storage key for the payout counter.
func PayoutCounterKey() []byte {
	return PayoutSequenceKey
}
