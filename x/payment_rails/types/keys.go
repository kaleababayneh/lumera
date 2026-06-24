// Package types holds shared types and helpers for the payment_rails module.
package types

const (
	// ModuleName defines the payment_rails module name used across keepers and routing.
	ModuleName = "payment_rails"
	// StoreKey specifies the primary KVStore key for the payment_rails module.
	StoreKey = ModuleName
	// RouterKey identifies the message routing key for payment_rails transactions.
	RouterKey = ModuleName
	// QuerierRoute sets the gRPC querier route for the payment_rails module.
	QuerierRoute = ModuleName

	// ParamsPrefixByte prefixes parameter entries under the module store.
	ParamsPrefixByte = uint8(0x01)
	// DepositPrefixByte prefixes deposit records.
	DepositPrefixByte = uint8(0x02)
	// PricingPrefixByte prefixes pricing records.
	PricingPrefixByte = uint8(0x03)
	// MintPrefixByte prefixes mint records.
	MintPrefixByte = uint8(0x04)
	// WithdrawPrefixByte prefixes withdraw records.
	WithdrawPrefixByte = uint8(0x05)
	// DepositSeqPrefixByte prefixes deposit sequence counters.
	DepositSeqPrefixByte = uint8(0x06)
	// WithdrawSeqPrefixByte prefixes withdraw sequence counters.
	WithdrawSeqPrefixByte = uint8(0x07)
	// DepositRequestPrefixByte prefixes deposit request id mapping.
	DepositRequestPrefixByte = uint8(0x08)
	// WithdrawRequestPrefixByte prefixes withdraw request id mapping.
	WithdrawRequestPrefixByte = uint8(0x09)
	// DepositsByUserPrefixByte prefixes deposits indexed by user.
	DepositsByUserPrefixByte = uint8(0x0A)
	// WithdrawalsByUserPrefixByte prefixes withdrawals indexed by user.
	WithdrawalsByUserPrefixByte = uint8(0x0B)
	// UserHourlyPrefixByte prefixes per-user hourly rate limit windows.
	UserHourlyPrefixByte = uint8(0x0C)
	// UserDailyPrefixByte prefixes per-user daily rate limit windows.
	UserDailyPrefixByte = uint8(0x0D)
	// IBCSettlementPrefixByte prefixes IBC settlement records.
	IBCSettlementPrefixByte = uint8(0x0E)
	// IBCSettlementSeqPrefixByte prefixes IBC settlement sequence counters.
	IBCSettlementSeqPrefixByte = uint8(0x0F)
	// IBCSettlementByRefPrefixByte prefixes IBC settlement index by reference ID.
	IBCSettlementByRefPrefixByte = uint8(0x10)
	// IBCSettlementByRequestPrefixByte prefixes IBC settlement index by request ID.
	IBCSettlementByRequestPrefixByte = uint8(0x11)
)

var (
	// ParamsPrefix stores the binary prefix for parameters collections.
	ParamsPrefix = []byte{ParamsPrefixByte}
	// DepositPrefix stores the prefix for deposit records.
	DepositPrefix = []byte{DepositPrefixByte}
	// PricingPrefix stores the prefix for pricing records.
	PricingPrefix = []byte{PricingPrefixByte}
	// MintPrefix stores the prefix for mint records.
	MintPrefix = []byte{MintPrefixByte}
	// WithdrawPrefix stores the prefix for withdraw records.
	WithdrawPrefix = []byte{WithdrawPrefixByte}
	// DepositSeqPrefix stores the prefix for deposit sequence counters.
	DepositSeqPrefix = []byte{DepositSeqPrefixByte}
	// WithdrawSeqPrefix stores the prefix for withdraw sequence counters.
	WithdrawSeqPrefix = []byte{WithdrawSeqPrefixByte}
	// DepositRequestPrefix stores the prefix for deposit request indexes.
	DepositRequestPrefix = []byte{DepositRequestPrefixByte}
	// WithdrawRequestPrefix stores the prefix for withdraw request indexes.
	WithdrawRequestPrefix = []byte{WithdrawRequestPrefixByte}
	// DepositsByUserPrefix stores the prefix for deposit user indexes.
	DepositsByUserPrefix = []byte{DepositsByUserPrefixByte}
	// WithdrawalsByUserPrefix stores the prefix for withdrawal user indexes.
	WithdrawalsByUserPrefix = []byte{WithdrawalsByUserPrefixByte}
	// UserHourlyPrefix stores the prefix for per-user hourly windows.
	UserHourlyPrefix = []byte{UserHourlyPrefixByte}
	// UserDailyPrefix stores the prefix for per-user daily windows.
	UserDailyPrefix = []byte{UserDailyPrefixByte}
	// IBCSettlementPrefix stores the prefix for IBC settlement records.
	IBCSettlementPrefix = []byte{IBCSettlementPrefixByte}
	// IBCSettlementSeqPrefix stores the prefix for IBC settlement sequence counters.
	IBCSettlementSeqPrefix = []byte{IBCSettlementSeqPrefixByte}
	// IBCSettlementByRefPrefix stores the prefix for IBC settlement reference indexes.
	IBCSettlementByRefPrefix = []byte{IBCSettlementByRefPrefixByte}
	// IBCSettlementByRequestPrefix stores the prefix for IBC settlement request indexes.
	IBCSettlementByRequestPrefix = []byte{IBCSettlementByRequestPrefixByte}
)

// ModuleAccountName defines the payment_rails module account identifier.
const ModuleAccountName = ModuleName
