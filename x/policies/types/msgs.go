
package types

import (
	"fmt"
	"strings"

	"github.com/LumeraProtocol/lumera/internal/logging"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ValidateBasic performs stateless validation on MsgCreatePolicy.
func (m *MsgCreatePolicy) ValidateBasic() error {
	if err := validatePolicyAddress("creator", m.GetCreator()); err != nil {
		return err
	}
	policy := m.GetPolicy()
	if policy == nil {
		return fmt.Errorf("policy is required")
	}
	if err := ValidatePolicy(policy); err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdatePolicy.
func (m *MsgUpdatePolicy) ValidateBasic() error {
	if err := validatePolicyAddress("updater", m.GetUpdater()); err != nil {
		return err
	}
	if err := validateCanonicalRequiredString("policy_id", m.GetPolicyId()); err != nil {
		return err
	}
	policy := m.GetPolicy()
	if policy == nil {
		return fmt.Errorf("policy is required")
	}
	// The update target is identified by m.PolicyId; require the embedded
	// policy's PolicyId to either be empty or match, so a malformed update
	// cannot quietly mutate a different policy than the user intended.
	if pid := strings.TrimSpace(policy.PolicyId); pid != "" && pid != m.GetPolicyId() {
		return fmt.Errorf("policy.policy_id %q does not match msg.policy_id %q", logging.RedactPII(pid), logging.RedactPII(m.GetPolicyId()))
	}
	if err := ValidatePolicy(policy); err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}
	if err := ValidatePolicyUpdateReason(m.GetUpdateReason()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgActivatePolicy.
func (m *MsgActivatePolicy) ValidateBasic() error {
	if err := validatePolicyAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validatePolicyReference(m.GetPolicyId(), m.GetVersion()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgDeprecatePolicy.
func (m *MsgDeprecatePolicy) ValidateBasic() error {
	if err := validatePolicyAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validatePolicyReference(m.GetPolicyId(), m.GetVersion()); err != nil {
		return err
	}
	if err := validateOptionalCanonicalString("successor_policy_id", m.GetSuccessorPolicyId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgArchivePolicy.
func (m *MsgArchivePolicy) ValidateBasic() error {
	if err := validatePolicyAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validatePolicyReference(m.GetPolicyId(), m.GetVersion()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdateParams.
func (m *MsgUpdateParams) ValidateBasic() error {
	if err := validatePolicyAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	params := m.GetParams()
	if params == nil {
		return fmt.Errorf("params is required")
	}
	if err := params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func validatePolicyAddress(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if _, err := sdk.AccAddressFromBech32(value); err != nil {
		return fmt.Errorf("invalid %s address: %w", field, err)
	}
	return nil
}

func validatePolicyReference(policyID, version string) error {
	if err := validateCanonicalRequiredString("policy_id", policyID); err != nil {
		return err
	}
	if err := validateCanonicalRequiredString("version", version); err != nil {
		return err
	}
	return nil
}

func validateOptionalCanonicalString(field, value string) error {
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

func (m *MsgCreatePolicy) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetCreator())
	return []sdk.AccAddress{addr}
}

func (m *MsgUpdatePolicy) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetUpdater())
	return []sdk.AccAddress{addr}
}

func (m *MsgActivatePolicy) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}

func (m *MsgDeprecatePolicy) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}

func (m *MsgArchivePolicy) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}

func (m *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}
