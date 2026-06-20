
package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// MaxToolpackTools caps how many ToolReference entries a
	// MsgMintToolpack or MsgUpdateToolpack may bundle. A curated
	// toolpack typically contains 3-20 tools; 256 is a ~13x-realistic
	// ceiling that bounds attacker-controlled storage + iteration
	// cost (every ToolReference is persisted and serialized on
	// every read). The cap covers both messages so update can't be
	// used to sidestep what mint enforced.
	MaxToolpackTools = 256
	// MaxToolIDLen bounds a single ToolReference.tool_id string.
	// Registry tool IDs are slugs with reverse-dns shape
	// (e.g. "defi.token_price") — ~50 bytes is realistic; 256 is
	// generous and matches the pattern elsewhere in x/.
	MaxToolIDLen = 256
	// MaxToolVersionLen bounds a single ToolReference.version
	// string. Versions follow semver / PEP440 / similar — ~20
	// bytes typical; 64 gives headroom.
	MaxToolVersionLen = 64
)

func validateAddressField(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if _, err := sdk.AccAddressFromBech32(value); err != nil {
		return fmt.Errorf("invalid %s address: %w", field, err)
	}
	return nil
}

func validateRequiredCanonicalIdentifier(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must be canonical", field)
	}
	return nil
}

func validateOptionalCanonicalIdentifier(field, value string) error {
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be canonical", field)
	}
	return nil
}

func validateRoyaltyAmount(amount sdk.Coin) error {
	if err := sdk.ValidateDenom(amount.Denom); err != nil {
		return fmt.Errorf("amount denom is invalid: %w", err)
	}
	if amount.Amount.IsNil() || !amount.Amount.IsPositive() {
		return fmt.Errorf("amount must be positive")
	}
	return nil
}

func validateToolReferences(tools []*ToolReference) error {
	if len(tools) > MaxToolpackTools {
		return fmt.Errorf("tools exceeds %d-entry cap (got %d)",
			MaxToolpackTools, len(tools))
	}
	seen := make(map[string]struct{}, len(tools))
	for i, ref := range tools {
		if ref == nil {
			return fmt.Errorf("tools[%d] is nil", i)
		}
		toolID := strings.TrimSpace(ref.GetToolId())
		if toolID == "" {
			return fmt.Errorf("tools[%d].tool_id is required", i)
		}
		if toolID != ref.GetToolId() {
			return fmt.Errorf("tools[%d].tool_id must be canonical", i)
		}
		if _, exists := seen[toolID]; exists {
			return fmt.Errorf("duplicate tool_id %s", toolID)
		}
		seen[toolID] = struct{}{}
		if len(toolID) > MaxToolIDLen {
			return fmt.Errorf("tools[%d].tool_id exceeds %d-byte cap (got %d)",
				i, MaxToolIDLen, len(toolID))
		}
		version := ref.GetVersion()
		if strings.TrimSpace(version) != version {
			return fmt.Errorf("tools[%d].version must be canonical", i)
		}
		if len(version) > MaxToolVersionLen {
			return fmt.Errorf("tools[%d].version exceeds %d-byte cap (got %d)",
				i, MaxToolVersionLen, len(version))
		}
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgMintToolpack.
func (m *MsgMintToolpack) ValidateBasic() error {
	if err := validateAddressField("curator", m.GetCurator()); err != nil {
		return err
	}
	if err := validateRequiredCanonicalIdentifier("id", m.GetId()); err != nil {
		return err
	}
	if err := validateOptionalCanonicalIdentifier("policy_version", m.GetPolicyVersion()); err != nil {
		return err
	}
	if len(m.GetTools()) == 0 {
		return fmt.Errorf("at least one tool is required")
	}
	if err := validateToolReferences(m.GetTools()); err != nil {
		return err
	}
	if m.GetRoyaltyBps() > MaxRoyaltyBPS {
		return fmt.Errorf("royalty_bps exceeds maximum (%d)", MaxRoyaltyBPS)
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdateToolpack.
func (m *MsgUpdateToolpack) ValidateBasic() error {
	if err := validateAddressField("curator", m.GetCurator()); err != nil {
		return err
	}
	if err := validateRequiredCanonicalIdentifier("id", m.GetId()); err != nil {
		return err
	}
	if err := validateOptionalCanonicalIdentifier("policy_version", m.GetPolicyVersion()); err != nil {
		return err
	}
	if err := validateToolReferences(m.GetTools()); err != nil {
		return err
	}
	if m.GetRoyaltyBps() > MaxRoyaltyBPS {
		return fmt.Errorf("royalty_bps exceeds maximum (%d)", MaxRoyaltyBPS)
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgDeactivateToolpack.
func (m *MsgDeactivateToolpack) ValidateBasic() error {
	if err := validateAddressField("curator", m.GetCurator()); err != nil {
		return err
	}
	if err := validateRequiredCanonicalIdentifier("id", m.GetId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgRecordRoyaltyPayout.
func (m *MsgRecordRoyaltyPayout) ValidateBasic() error {
	if err := validateAddressField("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validateRequiredCanonicalIdentifier("toolpack_id", m.GetToolpackId()); err != nil {
		return err
	}
	if err := validateRoyaltyAmount(m.GetAmount()); err != nil {
		return err
	}
	return nil
}
