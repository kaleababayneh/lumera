// Package types declares storage prefixes used by the reserve module.
package types

const (
	// ModuleName defines the module's canonical name.
	ModuleName = "reserve"
	// StoreKey is the primary KV-store key for reserve state.
	StoreKey = ModuleName
	// MemStoreKey is the in-memory store prefix.
	MemStoreKey = "mem_reserve"

	// TypeMsgUpdateParams identifies reserve governance parameter updates.
	TypeMsgUpdateParams = "update_params"

	// AttributeKeyAuthority records the governance authority that updated params.
	AttributeKeyAuthority = "authority"
	// AttributeKeyBeforeCreditDenom records the prior reserve credit denom.
	AttributeKeyBeforeCreditDenom = "before_credit_denom"
	// AttributeKeyAfterCreditDenom records the new reserve credit denom.
	AttributeKeyAfterCreditDenom = "after_credit_denom"
	// AttributeKeyBeforeTierCount records the prior tier count.
	AttributeKeyBeforeTierCount = "before_tier_count"
	// AttributeKeyAfterTierCount records the new tier count.
	AttributeKeyAfterTierCount = "after_tier_count"
)

var (
	// ParamsKeyPrefix stores serialized module parameters.
	ParamsKeyPrefix = []byte{0x01}
	// CommitmentKeyPrefix stores reserve commitments keyed by commitment ID.
	CommitmentKeyPrefix = []byte{0x10}
	// CommitmentByPolicyKeyPrefix indexes commitments by policy ID.
	CommitmentByPolicyKeyPrefix = []byte{0x11}
	// CommitmentSeqKeyPrefix stores the commitment sequence counter.
	CommitmentSeqKeyPrefix = []byte{0x12}
	// CommitmentExpiryKeyPrefix indexes commitments by expiration time.
	CommitmentExpiryKeyPrefix = []byte{0x13}
	// CommitmentByOwnerKeyPrefix indexes commitments by owner address.
	CommitmentByOwnerKeyPrefix = []byte{0x14}
)
