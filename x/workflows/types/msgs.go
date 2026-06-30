package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// TypeMsgPublishWorkflow identifies workflow publication requests.
	TypeMsgPublishWorkflow = "publish_workflow"
	// TypeMsgUpgradeWorkflow identifies workflow version-upgrade requests.
	TypeMsgUpgradeWorkflow = "upgrade_workflow"
	// TypeMsgDeactivateWorkflow identifies workflow deactivation requests.
	TypeMsgDeactivateWorkflow = "deactivate_workflow"
	// TypeMsgTopUpAuthorBond identifies workflow-author bond top-ups.
	TypeMsgTopUpAuthorBond = "top_up_author_bond"
	// TypeMsgWithdrawBond identifies workflow-author bond withdrawals.
	TypeMsgWithdrawBond = "withdraw_bond"
	// TypeMsgUpdateParams identifies governance parameter updates.
	TypeMsgUpdateParams = "update_params"
)

// MsgPublishWorkflow scaffolds workflow publication.
type MsgPublishWorkflow struct {
	Author       string        `json:"author"`
	WorkflowCard *WorkflowCard `json:"workflow_card"`
	Bond         sdk.Coin      `json:"bond,omitempty"`
}

// MsgUpgradeWorkflow scaffolds workflow version upgrades.
type MsgUpgradeWorkflow struct {
	Author       string        `json:"author"`
	WorkflowID   string        `json:"workflow_id"`
	FromVersion  string        `json:"from_version"`
	WorkflowCard *WorkflowCard `json:"workflow_card"`
}

// MsgDeactivateWorkflow scaffolds workflow deactivation.
type MsgDeactivateWorkflow struct {
	Author     string `json:"author"`
	WorkflowID string `json:"workflow_id"`
	Version    string `json:"version"`
	Reason     string `json:"reason,omitempty"`
}

// MsgTopUpAuthorBond adds unlocked bond balance for a workflow author.
type MsgTopUpAuthorBond struct {
	Author string   `json:"author"`
	Amount sdk.Coin `json:"amount"`
}

// MsgWithdrawBond scaffolds workflow-author bond withdrawals.
type MsgWithdrawBond struct {
	Author string   `json:"author"`
	Amount sdk.Coin `json:"amount,omitempty"`
}

// MsgUpdateParams scaffolds governance parameter updates.
type MsgUpdateParams struct {
	Authority string  `json:"authority"`
	Params    *Params `json:"params"`
}

func (m *MsgPublishWorkflow) Route() string { return RouterKey }
func (m *MsgPublishWorkflow) Type() string  { return TypeMsgPublishWorkflow }

func (m *MsgPublishWorkflow) ValidateBasic() error {
	if err := validateWorkflowMsgIdentifier("author", m.GetAuthor()); err != nil {
		return err
	}
	card := m.GetWorkflowCard()
	if card == nil {
		return fmt.Errorf("workflow_card is required")
	}
	if _, err := WorkflowKey(card.WorkflowId, card.Version); err != nil {
		return err
	}
	if err := StaticCheckWorkflowCard(card); err != nil {
		return err
	}
	return validateCoin("bond", m.Bond, false)
}

func (m *MsgPublishWorkflow) GetAuthor() string {
	if m == nil {
		return ""
	}
	return m.Author
}

func (m *MsgPublishWorkflow) GetWorkflowCard() *WorkflowCard {
	if m == nil {
		return nil
	}
	return m.WorkflowCard
}

func (m *MsgUpgradeWorkflow) Route() string { return RouterKey }
func (m *MsgUpgradeWorkflow) Type() string  { return TypeMsgUpgradeWorkflow }

func (m *MsgUpgradeWorkflow) ValidateBasic() error {
	if err := validateWorkflowMsgIdentifier("author", m.GetAuthor()); err != nil {
		return err
	}
	if _, err := WorkflowKey(m.GetWorkflowID(), m.GetFromVersion()); err != nil {
		return err
	}
	card := m.GetWorkflowCard()
	if card == nil {
		return fmt.Errorf("workflow_card is required")
	}
	if _, err := WorkflowKey(card.WorkflowId, card.Version); err != nil {
		return err
	}
	if card.WorkflowId != m.GetWorkflowID() {
		return fmt.Errorf("workflow_card workflow_id must match request workflow_id")
	}
	if err := StaticCheckWorkflowCard(card); err != nil {
		return err
	}
	return nil
}

func (m *MsgUpgradeWorkflow) GetAuthor() string {
	if m == nil {
		return ""
	}
	return m.Author
}

func (m *MsgUpgradeWorkflow) GetWorkflowID() string {
	if m == nil {
		return ""
	}
	return m.WorkflowID
}

func (m *MsgUpgradeWorkflow) GetFromVersion() string {
	if m == nil {
		return ""
	}
	return m.FromVersion
}

func (m *MsgUpgradeWorkflow) GetWorkflowCard() *WorkflowCard {
	if m == nil {
		return nil
	}
	return m.WorkflowCard
}

func (m *MsgDeactivateWorkflow) Route() string { return RouterKey }
func (m *MsgDeactivateWorkflow) Type() string  { return TypeMsgDeactivateWorkflow }

func (m *MsgDeactivateWorkflow) ValidateBasic() error {
	if err := validateWorkflowMsgIdentifier("author", m.GetAuthor()); err != nil {
		return err
	}
	_, err := WorkflowKey(m.GetWorkflowID(), m.GetVersion())
	return err
}

func (m *MsgDeactivateWorkflow) GetAuthor() string {
	if m == nil {
		return ""
	}
	return m.Author
}

func (m *MsgDeactivateWorkflow) GetWorkflowID() string {
	if m == nil {
		return ""
	}
	return m.WorkflowID
}

func (m *MsgDeactivateWorkflow) GetVersion() string {
	if m == nil {
		return ""
	}
	return m.Version
}

func (m *MsgWithdrawBond) Route() string { return RouterKey }
func (m *MsgWithdrawBond) Type() string  { return TypeMsgWithdrawBond }

func (m *MsgTopUpAuthorBond) Route() string { return RouterKey }
func (m *MsgTopUpAuthorBond) Type() string  { return TypeMsgTopUpAuthorBond }

func (m *MsgTopUpAuthorBond) ValidateBasic() error {
	if err := validateWorkflowMsgIdentifier("author", m.GetAuthor()); err != nil {
		return err
	}
	return validateCoin("amount", m.Amount, true)
}

func (m *MsgTopUpAuthorBond) GetAuthor() string {
	if m == nil {
		return ""
	}
	return m.Author
}

func (m *MsgTopUpAuthorBond) GetAmount() sdk.Coin {
	if m == nil {
		return sdk.Coin{}
	}
	return m.Amount
}

func (m *MsgWithdrawBond) ValidateBasic() error {
	if err := validateWorkflowMsgIdentifier("author", m.GetAuthor()); err != nil {
		return err
	}
	return validateCoin("amount", m.Amount, true)
}

func (m *MsgWithdrawBond) GetAuthor() string {
	if m == nil {
		return ""
	}
	return m.Author
}

func (m *MsgUpdateParams) Route() string { return RouterKey }
func (m *MsgUpdateParams) Type() string  { return TypeMsgUpdateParams }

func (m *MsgUpdateParams) ValidateBasic() error {
	if strings.TrimSpace(m.GetAuthority()) == "" {
		return fmt.Errorf("authority is required")
	}
	if _, err := sdk.AccAddressFromBech32(m.GetAuthority()); err != nil {
		return fmt.Errorf("invalid authority address: %w", err)
	}
	if m.GetParams() == nil {
		return fmt.Errorf("params is required")
	}
	return m.GetParams().Validate()
}

func (m *MsgUpdateParams) GetAuthority() string {
	if m == nil {
		return ""
	}
	return m.Authority
}

func (m *MsgUpdateParams) GetParams() *Params {
	if m == nil {
		return nil
	}
	return m.Params
}

func (m *MsgPublishWorkflow) GetSigners() []sdk.AccAddress {
	author, _ := sdk.AccAddressFromBech32(m.GetAuthor())
	return []sdk.AccAddress{author}
}

func (m *MsgUpgradeWorkflow) GetSigners() []sdk.AccAddress {
	author, _ := sdk.AccAddressFromBech32(m.GetAuthor())
	return []sdk.AccAddress{author}
}

func (m *MsgDeactivateWorkflow) GetSigners() []sdk.AccAddress {
	author, _ := sdk.AccAddressFromBech32(m.GetAuthor())
	return []sdk.AccAddress{author}
}

func (m *MsgTopUpAuthorBond) GetSigners() []sdk.AccAddress {
	author, _ := sdk.AccAddressFromBech32(m.GetAuthor())
	return []sdk.AccAddress{author}
}

func (m *MsgWithdrawBond) GetSigners() []sdk.AccAddress {
	author, _ := sdk.AccAddressFromBech32(m.GetAuthor())
	return []sdk.AccAddress{author}
}

func (m *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	authority, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{authority}
}

func validateWorkflowMsgIdentifier(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must be canonical: %q", field, value)
	}
	if _, err := sdk.AccAddressFromBech32(value); err != nil {
		return fmt.Errorf("invalid %s address: %w", field, err)
	}
	return nil
}

func validateCoin(field string, coin sdk.Coin, required bool) error {
	// A gogoproto value sdk.Coin with no denom and a nil/zero amount represents
	// an unprovided optional field. (After a JSON round-trip an unset coin
	// serializes as {"amount":"0"}, so a zero amount must also count as unset.)
	if coin.Denom == "" && (coin.Amount.IsNil() || coin.Amount.IsZero()) {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	denom := strings.TrimSpace(coin.Denom)
	if denom == "" {
		return fmt.Errorf("%s denom is required", field)
	}
	if denom != coin.Denom {
		return fmt.Errorf("%s denom must be canonical: %q", field, coin.Denom)
	}
	if err := sdk.ValidateDenom(denom); err != nil {
		return fmt.Errorf("%s denom is invalid: %w", field, err)
	}
	if coin.Amount.IsNil() {
		return fmt.Errorf("%s amount is required", field)
	}
	if coin.Amount.IsNegative() {
		return fmt.Errorf("%s amount cannot be negative", field)
	}
	if coin.Amount.IsZero() {
		return fmt.Errorf("%s amount must be positive", field)
	}
	return nil
}
