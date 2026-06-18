//go:build cosmos

package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Message type constants for passport module transactions.
const (
	TypeMsgRegisterPassport   = "register_passport"
	TypeMsgSuspendPassport    = "suspend_passport"
	TypeMsgRevokePassport     = "revoke_passport"
	TypeMsgReactivatePassport = "reactivate_passport"
	TypeMsgSlashStake         = "slash_stake"
	TypeMsgTopUpStake         = "top_up_stake"
	TypeMsgUnregisterPassport = "unregister_passport"
	TypeMsgUpdateParams       = "update_params"
)

// Per-field ValidateBasic caps. Stateless defense-in-depth ceilings;
// sibling modules (insurance, nft, payment_rails) use the same
// 256-byte cap for identifier strings and 4 KiB cap for free-form
// human-readable reason/description text.
const (
	// MaxPassportIDLen caps PassportId across all passport Msgs.
	// PassportIds are keeper-generated hashes/uuids; 256 is ~8x
	// realistic and matches MaxIDLen parity.
	MaxPassportIDLen = 256
	// MaxAgentPubkeyLen caps MsgRegisterPassport.AgentPubkey. Ed448
	// hex is ~114 bytes, ed25519 hex is 64; 512 leaves headroom for
	// "alg:base64(...)" wrappers without admitting megabyte strings
	// that persist in the Passport record and get scanned on every
	// keeper iteration.
	MaxAgentPubkeyLen = 512
	// MaxPassportReasonLen caps free-form reason fields on suspend/
	// revoke/slash. 4 KiB matches MaxClaimReasonLen (x/insurance)
	// and MaxChallengeDescriptionLen — same human-readable shape.
	MaxPassportReasonLen = 4 * 1024
)

var (
	_ sdk.Msg = &MsgRegisterPassport{}
	_ sdk.Msg = &MsgSuspendPassport{}
	_ sdk.Msg = &MsgRevokePassport{}
	_ sdk.Msg = &MsgReactivatePassport{}
	_ sdk.Msg = &MsgSlashStake{}
	_ sdk.Msg = &MsgTopUpStake{}
	_ sdk.Msg = &MsgUnregisterPassport{}
	_ sdk.Msg = &MsgUpdateParams{}
)

// NewMsgRegisterPassport creates a new MsgRegisterPassport.
func NewMsgRegisterPassport(creator, agentPubkey string, stake sdk.Coin) *MsgRegisterPassport {
	return &MsgRegisterPassport{
		Creator:     creator,
		AgentPubkey: agentPubkey,
		Stake:       CoinToProto(stake),
	}
}

func validatePassportID(passportID string) error {
	trimmed := strings.TrimSpace(passportID)
	if trimmed == "" {
		return ErrInvalidPassportID
	}
	if trimmed != passportID {
		return fmt.Errorf("passport_id must be canonical")
	}
	if len(passportID) > MaxPassportIDLen {
		return fmt.Errorf("passport_id length %d exceeds maximum %d", len(passportID), MaxPassportIDLen)
	}
	return nil
}

func validateAgentPubkey(agentPubkey string) error {
	trimmed := strings.TrimSpace(agentPubkey)
	if trimmed == "" {
		return ErrInvalidAgentPubkey
	}
	if trimmed != agentPubkey {
		return fmt.Errorf("agent_pubkey must be canonical")
	}
	if len(agentPubkey) > MaxAgentPubkeyLen {
		return fmt.Errorf("agent_pubkey length %d exceeds maximum %d", len(agentPubkey), MaxAgentPubkeyLen)
	}
	return nil
}

// ValidateBasic performs basic validation.
func (msg *MsgRegisterPassport) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Creator); err != nil {
		return fmt.Errorf("invalid creator address: %w", err)
	}
	if err := validateAgentPubkey(msg.AgentPubkey); err != nil {
		return err
	}
	if msg.Stake == nil {
		return ErrInsufficientStake
	}
	stake := CoinFromProto(msg.Stake)
	if stake.Denom == "" || stake.Amount.IsNil() || !stake.Amount.IsPositive() {
		return ErrInsufficientStake
	}
	return nil
}

// GetSigners returns the expected signers for the message.
func (msg *MsgRegisterPassport) GetSigners() []sdk.AccAddress {
	creator, _ := sdk.AccAddressFromBech32(msg.Creator)
	return []sdk.AccAddress{creator}
}

// NewMsgSuspendPassport creates a new MsgSuspendPassport.
func NewMsgSuspendPassport(authority, passportID, reason string) *MsgSuspendPassport {
	return &MsgSuspendPassport{
		Authority:  authority,
		PassportId: passportID,
		Reason:     reason,
	}
}

// ValidateBasic performs basic validation.
func (msg *MsgSuspendPassport) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return fmt.Errorf("invalid authority address: %w", err)
	}
	if err := validatePassportID(msg.PassportId); err != nil {
		return err
	}
	if len(msg.Reason) > MaxPassportReasonLen {
		return fmt.Errorf("reason length %d exceeds maximum %d", len(msg.Reason), MaxPassportReasonLen)
	}
	return nil
}

// GetSigners returns the expected signers for the message.
func (msg *MsgSuspendPassport) GetSigners() []sdk.AccAddress {
	authority, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{authority}
}

// NewMsgRevokePassport creates a new MsgRevokePassport.
func NewMsgRevokePassport(authority, passportID, reason string) *MsgRevokePassport {
	return &MsgRevokePassport{
		Authority:  authority,
		PassportId: passportID,
		Reason:     reason,
	}
}

// ValidateBasic performs basic validation.
func (msg *MsgRevokePassport) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return fmt.Errorf("invalid authority address: %w", err)
	}
	if err := validatePassportID(msg.PassportId); err != nil {
		return err
	}
	if len(msg.Reason) > MaxPassportReasonLen {
		return fmt.Errorf("reason length %d exceeds maximum %d", len(msg.Reason), MaxPassportReasonLen)
	}
	return nil
}

// GetSigners returns the expected signers for the message.
func (msg *MsgRevokePassport) GetSigners() []sdk.AccAddress {
	authority, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{authority}
}

// NewMsgReactivatePassport creates a new MsgReactivatePassport.
func NewMsgReactivatePassport(owner, passportID string) *MsgReactivatePassport {
	return &MsgReactivatePassport{
		Owner:      owner,
		PassportId: passportID,
	}
}

// ValidateBasic performs basic validation.
func (msg *MsgReactivatePassport) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Owner); err != nil {
		return fmt.Errorf("invalid owner address: %w", err)
	}
	if err := validatePassportID(msg.PassportId); err != nil {
		return err
	}
	return nil
}

// GetSigners returns the expected signers for the message.
func (msg *MsgReactivatePassport) GetSigners() []sdk.AccAddress {
	owner, _ := sdk.AccAddressFromBech32(msg.Owner)
	return []sdk.AccAddress{owner}
}

// NewMsgSlashStake creates a new MsgSlashStake.
func NewMsgSlashStake(authority, passportID string, amount sdk.Coin, reason string) *MsgSlashStake {
	return &MsgSlashStake{
		Authority:  authority,
		PassportId: passportID,
		Amount:     CoinToProto(amount),
		Reason:     reason,
	}
}

// ValidateBasic performs basic validation.
func (msg *MsgSlashStake) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return fmt.Errorf("invalid authority address: %w", err)
	}
	if err := validatePassportID(msg.PassportId); err != nil {
		return err
	}
	if len(msg.Reason) > MaxPassportReasonLen {
		return fmt.Errorf("reason length %d exceeds maximum %d", len(msg.Reason), MaxPassportReasonLen)
	}
	if msg.Amount == nil {
		return fmt.Errorf("slash amount cannot be nil")
	}
	amount := CoinFromProto(msg.Amount)
	if amount.Denom == "" || amount.Amount.IsNil() || !amount.Amount.IsPositive() {
		return fmt.Errorf("slash amount must be positive")
	}
	return nil
}

// GetSigners returns the expected signers for the message.
func (msg *MsgSlashStake) GetSigners() []sdk.AccAddress {
	authority, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{authority}
}

// NewMsgTopUpStake creates a new MsgTopUpStake.
func NewMsgTopUpStake(owner, passportID string, amount sdk.Coin) *MsgTopUpStake {
	return &MsgTopUpStake{
		Owner:      owner,
		PassportId: passportID,
		Amount:     CoinToProto(amount),
	}
}

// ValidateBasic performs basic validation.
func (msg *MsgTopUpStake) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Owner); err != nil {
		return fmt.Errorf("invalid owner address: %w", err)
	}
	if err := validatePassportID(msg.PassportId); err != nil {
		return err
	}
	if msg.Amount == nil {
		return ErrInsufficientStake
	}
	amount := CoinFromProto(msg.Amount)
	if amount.Denom == "" || amount.Amount.IsNil() || !amount.Amount.IsPositive() {
		return ErrInsufficientStake
	}
	return nil
}

// GetSigners returns the expected signers for the message.
func (msg *MsgTopUpStake) GetSigners() []sdk.AccAddress {
	owner, _ := sdk.AccAddressFromBech32(msg.Owner)
	return []sdk.AccAddress{owner}
}

// NewMsgUnregisterPassport creates a new MsgUnregisterPassport.
func NewMsgUnregisterPassport(owner, passportID string) *MsgUnregisterPassport {
	return &MsgUnregisterPassport{
		Owner:      owner,
		PassportId: passportID,
	}
}

// ValidateBasic performs basic validation.
func (msg *MsgUnregisterPassport) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Owner); err != nil {
		return fmt.Errorf("invalid owner address: %w", err)
	}
	if err := validatePassportID(msg.PassportId); err != nil {
		return err
	}
	return nil
}

// GetSigners returns the expected signers for the message.
func (msg *MsgUnregisterPassport) GetSigners() []sdk.AccAddress {
	owner, _ := sdk.AccAddressFromBech32(msg.Owner)
	return []sdk.AccAddress{owner}
}

// NewMsgUpdateParams creates a new MsgUpdateParams.
func NewMsgUpdateParams(authority string, params *Params) *MsgUpdateParams {
	return &MsgUpdateParams{
		Authority: authority,
		Params:    params,
	}
}

// ValidateBasic performs basic validation.
func (msg *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return fmt.Errorf("invalid authority address: %w", err)
	}
	if msg.Params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := msg.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

// GetSigners returns the expected signers for the message.
func (msg *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	authority, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{authority}
}
