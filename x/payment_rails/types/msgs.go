package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func validatePaymentRailsAddress(field, value string) error {
	address := strings.TrimSpace(value)
	if address == "" {
		return fmt.Errorf("%s is required", field)
	}
	if address != value {
		return fmt.Errorf("%s must be canonical", field)
	}
	if _, err := sdk.AccAddressFromBech32(address); err != nil {
		return fmt.Errorf("invalid %s address: %w", field, err)
	}
	return nil
}

func validatePaymentRailsRequestID(requestID string) error {
	trimmed := strings.TrimSpace(requestID)
	if trimmed == "" {
		return fmt.Errorf("request_id is required")
	}
	if trimmed != requestID {
		return fmt.Errorf("request_id must be canonical")
	}
	if len(requestID) > MaxIDLen {
		return fmt.Errorf("request_id exceeds %d-byte cap (got %d)", MaxIDLen, len(requestID))
	}
	return nil
}

func validatePaymentRailsID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must be canonical", field)
	}
	if len(value) > MaxIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)", field, MaxIDLen, len(value))
	}
	return nil
}

func validatePaymentRailsDenom(field, denom string) error {
	trimmed := strings.TrimSpace(denom)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != denom {
		return fmt.Errorf("%s must be canonical", field)
	}
	if len(denom) > MaxIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)", field, MaxIDLen, len(denom))
	}
	if err := sdk.ValidateDenom(denom); err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	return nil
}

func validatePaymentRailsCoin(field string, coin sdk.Coin) error {
	if coin.Denom == "" && coin.Amount.IsNil() {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := CoinFromProtoSafe(coin)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	if !parsed.Amount.IsPositive() {
		return fmt.Errorf("%s must be positive", field)
	}
	return nil
}

func validatePaymentRailsSettlementRoute(channelID, portID string) error {
	channelTrimmed := strings.TrimSpace(channelID)
	portTrimmed := strings.TrimSpace(portID)
	if channelTrimmed != channelID {
		return fmt.Errorf("settlement_channel_id must be canonical")
	}
	if portTrimmed != portID {
		return fmt.Errorf("settlement_port_id must be canonical")
	}
	if (channelID == "") != (portID == "") {
		return fmt.Errorf("settlement_channel_id and settlement_port_id must be provided together")
	}
	if channelID == "" {
		return nil
	}
	if len(channelID) > MaxIDLen {
		return fmt.Errorf("settlement_channel_id exceeds %d-byte cap (got %d)", MaxIDLen, len(channelID))
	}
	if len(portID) > MaxIDLen {
		return fmt.Errorf("settlement_port_id exceeds %d-byte cap (got %d)", MaxIDLen, len(portID))
	}
	return nil
}

const (
	// MaxIDLen bounds hash/id/request-id/denom/withdraw-id/deposit-id
	// fields carried on payment_rails Msgs. Realistic values are
	// bech32 addresses (~43), tx hashes (~66), UUIDs (~36), and
	// denoms (<128 per sdk.ValidateDenom). 256 is ~4x any realistic
	// shape and matches MaxVaultIDLen for cross-module parity.
	// Every ID is persisted in state, indexed for lookup, and
	// echoed into events — unbounded lengths bloat on-chain storage.
	MaxIDLen = 256
	// MaxDecimalStrLen bounds optional decimal-as-string fields
	// (quoted_price). A realistic price is a decimal under 40
	// characters; 64 is generous.
	MaxDecimalStrLen = 64
	// MaxAcceptedDenoms caps the accepted_denoms slice on
	// MsgUpdateParams. Gov-gated, but defense-in-depth keeps a
	// malformed gov proposal from injecting unbounded denom lists
	// into module params state.
	MaxAcceptedDenoms = 64
)

// ValidateBasic performs stateless validation on MsgCreateDeposit.
func (m *MsgCreateDeposit) ValidateBasic() error {
	if err := validatePaymentRailsAddress("user", m.GetUser()); err != nil {
		return err
	}
	if err := validatePaymentRailsCoin("amount", m.GetAmount()); err != nil {
		return err
	}
	if err := validatePaymentRailsID("tx_hash", m.GetTxHash()); err != nil {
		return err
	}
	if err := validatePaymentRailsRequestID(m.GetRequestId()); err != nil {
		return err
	}
	if len(m.GetQuotedPrice()) > MaxDecimalStrLen {
		return fmt.Errorf("quoted_price exceeds %d-byte cap (got %d)",
			MaxDecimalStrLen, len(m.GetQuotedPrice()))
	}
	if err := validatePaymentRailsSettlementRoute(m.GetSettlementChannelId(), m.GetSettlementPortId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRequestWithdraw.
func (m *MsgRequestWithdraw) ValidateBasic() error {
	if err := validatePaymentRailsAddress("user", m.GetUser()); err != nil {
		return err
	}
	if err := validatePaymentRailsCoin("lac_amount", m.GetLacAmount()); err != nil {
		return err
	}
	if err := validatePaymentRailsDenom("denom", m.GetDenom()); err != nil {
		return err
	}
	if err := validatePaymentRailsRequestID(m.GetRequestId()); err != nil {
		return err
	}
	if len(m.GetQuotedPrice()) > MaxDecimalStrLen {
		return fmt.Errorf("quoted_price exceeds %d-byte cap (got %d)",
			MaxDecimalStrLen, len(m.GetQuotedPrice()))
	}
	if err := validatePaymentRailsSettlementRoute(m.GetSettlementChannelId(), m.GetSettlementPortId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgFinalizeWithdraw.
func (m *MsgFinalizeWithdraw) ValidateBasic() error {
	if err := validatePaymentRailsAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validatePaymentRailsID("withdraw_id", m.GetWithdrawId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRefundDeposit.
func (m *MsgRefundDeposit) ValidateBasic() error {
	if err := validatePaymentRailsAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validatePaymentRailsID("deposit_id", m.GetDepositId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdateParams.
func (m *MsgUpdateParams) ValidateBasic() error {
	if err := validatePaymentRailsAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if m.GetAcqFeeBps() > 10_000 {
		return fmt.Errorf("acq_fee_bps exceeds 100%%")
	}
	if m.GetMaxSlippageBps() > 10_000 {
		return fmt.Errorf("max_slippage_bps exceeds 100%%")
	}
	if m.GetMaxOracleDeviationBps() > 10_000 {
		return fmt.Errorf("max_oracle_deviation_bps exceeds 100%%")
	}
	if len(m.GetAcceptedDenoms()) > MaxAcceptedDenoms {
		return fmt.Errorf("accepted_denoms exceeds %d-entry cap (got %d)",
			MaxAcceptedDenoms, len(m.GetAcceptedDenoms()))
	}
	for i, d := range m.GetAcceptedDenoms() {
		if err := validatePaymentRailsDenom(fmt.Sprintf("accepted_denoms[%d]", i), d); err != nil {
			return err
		}
	}
	return nil
}

func (m *MsgCreateDeposit) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetUser())
	return []sdk.AccAddress{addr}
}

func (m *MsgRequestWithdraw) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetUser())
	return []sdk.AccAddress{addr}
}

func (m *MsgFinalizeWithdraw) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}

func (m *MsgRefundDeposit) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}

func (m *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}
