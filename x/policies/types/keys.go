
// Package types holds shared types and helpers for the policies module.
//
//revive:disable:var-naming // The module keeps its protobuf types under a canonical types package.
package types

const (
	// ModuleName defines the policies module name used across keepers and routing.
	ModuleName = "policies"
	// StoreKey specifies the primary KVStore key for the policies module.
	StoreKey = ModuleName
	// RouterKey identifies the message routing key for policies transactions.
	RouterKey = ModuleName
	// QuerierRoute sets the gRPC querier route for the policies module.
	QuerierRoute = ModuleName

	// ParamsPrefixByte prefixes parameter entries under the module store.
	ParamsPrefixByte = uint8(0x01)
	// PolicyPrefixByte prefixes policy profile records in state.
	PolicyPrefixByte = uint8(0x02)
	// PolicyVersionPrefixByte prefixes policy version tracking.
	PolicyVersionPrefixByte = uint8(0x03)
	// PolicyUpdatePrefixByte prefixes policy update history records.
	PolicyUpdatePrefixByte = uint8(0x04)
	// PolicyUpdateCounterPrefixByte prefixes the policy update sequence counter.
	PolicyUpdateCounterPrefixByte = uint8(0x05)
	// PolicyAuditPrefixByte prefixes policy audit trail entries.
	PolicyAuditPrefixByte = uint8(0x06)
	// PolicyByOwnerPrefixByte prefixes the owner-to-policy index.
	PolicyByOwnerPrefixByte = uint8(0x07)
	// PolicyByStatePrefixByte prefixes the state-to-policy index.
	PolicyByStatePrefixByte = uint8(0x08)
	// PolicyAuditCounterPrefixByte prefixes the per-module audit sequence counter,
	// used to make audit IDs unique across same-block evaluations of the same
	// (policy, tool, user) tuple.
	PolicyAuditCounterPrefixByte = uint8(0x09)
	// BudgetUsagePrefixByte prefixes persisted policy budget usage counters.
	BudgetUsagePrefixByte = uint8(0x0A)
)

var (
	// ParamsPrefix stores the binary prefix for parameters collections.
	ParamsPrefix = []byte{ParamsPrefixByte}
	// PolicyPrefix stores the binary prefix for policy profile collections.
	PolicyPrefix = []byte{PolicyPrefixByte}
	// PolicyVersionPrefix stores the prefix for policy version tracking.
	PolicyVersionPrefix = []byte{PolicyVersionPrefixByte}
	// PolicyUpdatePrefix stores the prefix for policy update history.
	PolicyUpdatePrefix = []byte{PolicyUpdatePrefixByte}
	// PolicyUpdateCounterPrefix stores the prefix for the update sequence counter.
	PolicyUpdateCounterPrefix = []byte{PolicyUpdateCounterPrefixByte}
	// PolicyAuditPrefix stores the prefix for audit trail entries.
	PolicyAuditPrefix = []byte{PolicyAuditPrefixByte}
	// PolicyByOwnerPrefix stores the prefix for the owner-to-policy index.
	PolicyByOwnerPrefix = []byte{PolicyByOwnerPrefixByte}
	// PolicyByStatePrefix stores the prefix for the state-to-policy index.
	PolicyByStatePrefix = []byte{PolicyByStatePrefixByte}
	// PolicyAuditCounterPrefix stores the prefix for the audit sequence counter.
	PolicyAuditCounterPrefix = []byte{PolicyAuditCounterPrefixByte}
	// BudgetUsagePrefix stores accumulated usage counters for budget enforcement.
	BudgetUsagePrefix = []byte{BudgetUsagePrefixByte}
)

// ModuleAccountName defines the policies module account identifier.
const ModuleAccountName = ModuleName
