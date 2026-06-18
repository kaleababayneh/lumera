//go:build cosmos

// Package types holds shared types and helpers for the passport module.
package types

const (
	// ModuleName defines the passport module name used across keepers and routing.
	ModuleName = "passport"
	// StoreKey specifies the primary KVStore key for the passport module.
	StoreKey = ModuleName
	// RouterKey identifies the message routing key for passport transactions.
	RouterKey = ModuleName
	// QuerierRoute sets the gRPC querier route for the passport module.
	QuerierRoute = ModuleName

	// ParamsPrefixByte prefixes parameter entries under the module store.
	ParamsPrefixByte = uint8(0x01)
	// PassportsPrefixByte prefixes passport records in state.
	PassportsPrefixByte = uint8(0x02)
	// PassportSeqKeyPrefixByte prefixes auto-increment passport sequence counters.
	PassportSeqKeyPrefixByte = uint8(0x03)
	// AgentIndexPrefixByte prefixes the agent_pubkey -> passport_id index.
	AgentIndexPrefixByte = uint8(0x04)
)

var (
	// ParamsPrefix stores the binary prefix for parameters collections.
	ParamsPrefix = []byte{ParamsPrefixByte}
	// PassportsPrefix stores the binary prefix for passport collections.
	PassportsPrefix = []byte{PassportsPrefixByte}
	// PassportSeqKeyPrefix stores the prefix used for passport sequence counters.
	PassportSeqKeyPrefix = []byte{PassportSeqKeyPrefixByte}
	// AgentIndexPrefix stores the prefix for agent pubkey to passport ID index.
	AgentIndexPrefix = []byte{AgentIndexPrefixByte}
)

// ModuleAccountName defines the passport module account identifier.
const ModuleAccountName = ModuleName
