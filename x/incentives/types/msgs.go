package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxRevokeBadgeReasonLen caps the governance revocation rationale. 4 KiB
// matches sibling free-form reason fields such as insurance claims and
// passport revocations.
const MaxRevokeBadgeReasonLen = 4 * 1024

func validateSignerAddress(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if _, err := sdk.AccAddressFromBech32(value); err != nil {
		return fmt.Errorf("invalid %s address: %w", field, err)
	}
	return nil
}

func validateToolID(value string) error {
	return validateToolIDField("tool_id", value)
}

func validateToolIDField(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

// ValidateRevokeBadgeReason validates the free-form badge revocation reason.
func ValidateRevokeBadgeReason(reason string) error {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return fmt.Errorf("reason is required")
	}
	if trimmed != reason {
		return fmt.Errorf("reason must not contain leading or trailing whitespace")
	}
	if len(reason) > MaxRevokeBadgeReasonLen {
		return fmt.Errorf("reason exceeds %d-byte cap (got %d)", MaxRevokeBadgeReasonLen, len(reason))
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRecordMetrics.
func (m *MsgRecordMetrics) ValidateBasic() error {
	if err := validateSignerAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	toolID := m.GetToolId()
	if err := validateToolID(toolID); err != nil {
		return err
	}
	metrics := m.GetMetrics()
	if metrics == nil {
		return fmt.Errorf("metrics is required")
	}
	if err := validateToolIDField("metrics.tool_id", metrics.GetToolId()); err != nil {
		return err
	}
	if metrics.GetToolId() != toolID {
		return fmt.Errorf("metrics.tool_id must match tool_id")
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRequestEvaluation.
func (m *MsgRequestEvaluation) ValidateBasic() error {
	if err := validateSignerAddress("publisher", m.GetPublisher()); err != nil {
		return err
	}
	if err := validateToolID(m.GetToolId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdateTierConfig.
func (m *MsgUpdateTierConfig) ValidateBasic() error {
	if err := validateSignerAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if m.GetConfig() == nil {
		return fmt.Errorf("config is required")
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdateParams.
func (m *MsgUpdateParams) ValidateBasic() error {
	if err := validateSignerAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if m.GetParams() == nil {
		return fmt.Errorf("params is required")
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRevokeBadge.
func (m *MsgRevokeBadge) ValidateBasic() error {
	if err := validateSignerAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validateToolID(m.GetToolId()); err != nil {
		return err
	}
	if err := ValidateRevokeBadgeReason(m.GetReason()); err != nil {
		return err
	}
	return nil
}

func (m *MsgRecordMetrics) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}

func (m *MsgRequestEvaluation) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetPublisher())
	return []sdk.AccAddress{addr}
}

func (m *MsgUpdateTierConfig) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}

func (m *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}

func (m *MsgRevokeBadge) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}
