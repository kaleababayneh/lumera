package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Status constants for deposits (string representation for keeper compatibility)
const (
	DepositStatusPending   = "pending"
	DepositStatusPriced    = "priced"
	DepositStatusMinted    = "minted"
	DepositStatusFinalized = "finalized"
	DepositStatusRefunded  = "refunded"

	WithdrawStatusRequested = "requested"
	WithdrawStatusCompleted = "completed"
	WithdrawStatusFailed    = "failed"
)

// DepositRequest captures input for creating a deposit.
// This is a keeper-level input type, not a proto type.
type DepositRequest struct {
	User          string   `json:"user"`
	Amount        sdk.Coin `json:"amount"`
	TxHash        string   `json:"tx_hash"`
	RequestID     string   `json:"request_id"`
	Confirmations uint64   `json:"confirmations"`
	// QuotedPrice is the optional expected price (asset/USD) from a prior quote.
	// If set, slippage is calculated against this price and capped per params.
	QuotedPrice string `json:"quoted_price,omitempty"`
	// SettlementChannelID and SettlementPortID optionally request an
	// immediate IBC settlement record for this deposit.
	SettlementChannelID string `json:"settlement_channel_id,omitempty"`
	SettlementPortID    string `json:"settlement_port_id,omitempty"`
}

// WithdrawRequest captures input for a withdrawal request.
// This is a keeper-level input type, not a proto type.
type WithdrawRequest struct {
	User      string   `json:"user"`
	LacAmount sdk.Coin `json:"lac_amount"`
	Denom     string   `json:"denom"`
	RequestID string   `json:"request_id"`
	// QuotedPrice is the optional expected price (asset/USD) from a prior quote.
	// If set, slippage is calculated against this price and capped per params.
	QuotedPrice string `json:"quoted_price,omitempty"`
	// SettlementChannelID and SettlementPortID optionally request an
	// immediate IBC settlement record for this withdrawal.
	SettlementChannelID string `json:"settlement_channel_id,omitempty"`
	SettlementPortID    string `json:"settlement_port_id,omitempty"`
}
