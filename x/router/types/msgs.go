package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func validateAuthorityAddress(authority string) error {
	if strings.TrimSpace(authority) == "" {
		return fmt.Errorf("authority is required")
	}
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		return fmt.Errorf("invalid authority address: %w", err)
	}
	return nil
}

func validateNonNegativeDecimalField(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	amount, err := ParseDecimal(value)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	if amount.IsNegative() {
		return fmt.Errorf("%s cannot be negative", field)
	}
	return nil
}

func validateCanonicalRequiredRouterID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

func validateCanonicalOptionalRouterID(field, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdateParams.
func (m *MsgUpdateParams) ValidateBasic() error {
	if err := validateAuthorityAddress(m.GetAuthority()); err != nil {
		return err
	}
	params := m.GetParams()
	if err := params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRecordActivation.
func (m *MsgRecordActivation) ValidateBasic() error {
	if err := validateAuthorityAddress(m.GetAuthority()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredRouterID("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredRouterID("session_id", m.GetSessionId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRecordInvocation.
func (m *MsgRecordInvocation) ValidateBasic() error {
	if err := validateAuthorityAddress(m.GetAuthority()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredRouterID("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredRouterID("session_id", m.GetSessionId()); err != nil {
		return err
	}
	if err := validateCanonicalOptionalRouterID("origin_tool_id", m.GetOriginToolId()); err != nil {
		return err
	}
	if err := validateNonNegativeDecimalField("cost", m.GetCost()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRecordPolicyUpdate.
func (m *MsgRecordPolicyUpdate) ValidateBasic() error {
	if err := validateAuthorityAddress(m.GetAuthority()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredRouterID("policy_id", m.GetPolicyId()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredRouterID("new_version", m.GetNewVersion()); err != nil {
		return err
	}
	if err := validateCanonicalOptionalRouterID("previous_version", m.GetPreviousVersion()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRecordCACHit.
func (m *MsgRecordCACHit) ValidateBasic() error {
	if err := validateAuthorityAddress(m.GetAuthority()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredRouterID("cache_key", m.GetCacheKey()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredRouterID("origin_tool_id", m.GetOriginToolId()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredRouterID("consuming_tool_id", m.GetConsumingToolId()); err != nil {
		return err
	}
	if err := validateNonNegativeDecimalField("royalty_amount", m.GetRoyaltyAmount()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgAggregateMetrics.
func (m *MsgAggregateMetrics) ValidateBasic() error {
	if err := validateAuthorityAddress(m.GetAuthority()); err != nil {
		return err
	}
	return nil
}
