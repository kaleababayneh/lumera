// Package types declares oracle module storage keys and constants.
package types

const (
	// ModuleName defines the module name
	ModuleName = "oracle"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_" + ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName
)

// KVStore key prefixes for Collections
var (
	// ParamsKey is the key for module parameters
	ParamsKey = []byte{0x01}

	// PriceFeedPrefix is the prefix for price feed storage
	PriceFeedPrefix = []byte{0x10}

	// AggregatedPricePrefix is the prefix for aggregated price storage
	AggregatedPricePrefix = []byte{0x11}

	// ValidatorVotePrefix is the prefix for validator vote storage
	ValidatorVotePrefix = []byte{0x12}

	// VoteHistoryPrefix is the prefix for vote history storage
	VoteHistoryPrefix = []byte{0x13}

	// RewardAddressPrefix is the prefix for validator reward addresses
	RewardAddressPrefix = []byte{0x14}
)

// Event attribute keys emitted by the oracle module
const (
	EventTypeAggregatedPrice  = "oracle_aggregated_price"
	EventTypeOracleRewardPaid = "oracle_reward_paid"
	AttributeKeyAssetPair     = "asset_pair"
	AttributeKeyMedianPrice   = "median_price"
	AttributeKeyNumValidators = "num_validators"
	AttributeKeyValidator     = "validator"
	AttributeKeyRewardAddress = "reward_address"
	AttributeKeyRewardAmount  = "reward_amount"
)

// Reward defaults for oracle vote incentives.
const (
	DefaultRewardDenom  = "ulac"
	DefaultRewardAmount = int64(1000)
)
