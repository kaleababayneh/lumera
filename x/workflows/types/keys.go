// Package types holds shared types and helpers for the workflows module.
package types

const (
	// ModuleName defines the workflows module name used across keepers and routing.
	ModuleName = "workflows"
	// StoreKey specifies the primary KVStore key for the workflows module.
	StoreKey = ModuleName
	// RouterKey identifies the message routing key for workflows transactions.
	RouterKey = ModuleName
	// QuerierRoute sets the gRPC querier route for the workflows module.
	QuerierRoute = ModuleName
)

const (
	// ParamsPrefixByte prefixes parameter entries under the module store.
	ParamsPrefixByte = uint8(0x01)
	// WorkflowPrefixByte prefixes workflow records keyed by workflow_id/version.
	WorkflowPrefixByte = uint8(0x02)
	// AuthorBondPrefixByte prefixes author bond records keyed by author address.
	AuthorBondPrefixByte = uint8(0x03)
	// BundleQuotePrefixByte prefixes workflow bundle quote replay records keyed by bundle_id.
	BundleQuotePrefixByte = uint8(0x04)
)

var (
	// ParamsPrefix stores the binary prefix for parameters collections.
	ParamsPrefix = []byte{ParamsPrefixByte}
	// WorkflowPrefix stores the binary prefix for workflow records.
	WorkflowPrefix = []byte{WorkflowPrefixByte}
	// AuthorBondPrefix stores the binary prefix for author bond records.
	AuthorBondPrefix = []byte{AuthorBondPrefixByte}
	// BundleQuotePrefix stores the binary prefix for workflow bundle quote records.
	BundleQuotePrefix = []byte{BundleQuotePrefixByte}
)

const (
	// ModuleAccountName defines the workflows module account identifier.
	ModuleAccountName = ModuleName
	// BPSDenominator is the canonical basis-point denominator.
	BPSDenominator = uint32(10_000)
)
