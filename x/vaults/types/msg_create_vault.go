package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MaxVaultIDLen caps PolicyId / ToolId / Tier string lengths on
// MsgCreateVault. Realistic values are slugs under 64 bytes
// (e.g. "policy.enterprise@1.0.0", "defi.token_price", "gold");
// 256 is ~4x realistic. Every value is persisted in the Vault
// record, indexed by owner+tier, and echoed into every
// vault-lifecycle event — unbounded lengths on any of these
// directly bloat on-chain storage.
const MaxVaultIDLen = 256

func validateVaultIdentifier(field, value string, required bool, requiredErr error) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if required {
			return requiredErr
		}
		if value == "" {
			return nil
		}
		return fmt.Errorf("%s must not contain only whitespace", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	if len(value) > MaxVaultIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)",
			field, MaxVaultIDLen, len(value))
	}
	return nil
}

// ValidateBasic performs stateless message validation.
func (m *MsgCreateVault) ValidateBasic() error {
	if m == nil {
		return fmt.Errorf("message cannot be nil")
	}
	if _, err := sdk.AccAddressFromBech32(m.Owner); err != nil {
		return ErrInvalidOwner
	}
	if err := validateVaultIdentifier("policy_id", m.PolicyId, true, ErrInvalidPolicy); err != nil {
		return err
	}
	// tool_id: not required by the historical contract (existing
	// callers may omit it), but when supplied must fit under the
	// same length cap as policy_id.
	if err := validateVaultIdentifier("tool_id", m.ToolId, false, nil); err != nil {
		return err
	}
	if err := validateVaultIdentifier("tier", m.Tier, true, ErrInvalidTier); err != nil {
		return err
	}
	if m.PrepaidAmount.Amount.IsNil() {
		return ErrInvalidAmount
	}
	if _, err := validateVaultCoin("prepaid amount", m.PrepaidAmount, true); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAmount, err)
	}
	return nil
}

// GetSigners identifies the expected message signers.
func (m *MsgCreateVault) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.Owner)
	return []sdk.AccAddress{addr}
}
