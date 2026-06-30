package types

const (
	// ModuleName defines the auction module name.
	ModuleName = "auction"

	// StoreKey is the primary module store key.
	StoreKey = ModuleName

	// RouterKey is the message route key (reserved for future Msg wiring).
	RouterKey = ModuleName

	// QuerierRoute is the gRPC query router key.
	QuerierRoute = ModuleName

	// DefaultParamspace defines default module parameters key.
	DefaultParamspace = ModuleName

	// DefaultCreditDenom represents the base credit denomination used for auctions.
	DefaultCreditDenom = "ulac"
)
