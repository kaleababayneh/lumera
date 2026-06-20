
package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// validateInsuranceCoin performs stateless validation on a settlement/claim
// Coin field.
//
// After the gogoproto migration these Coin fields decode to a value
// cosmossdk.io/types.Coin whose Amount is a cosmossdk.io/math.Int — the
// wire decoder already rejects non-integer and symbolic-exponent strings,
// so the old shopspring-exponent DoS guard (validateCoinAmountExponent) is
// no longer required: an attacker can no longer smuggle "1e11100100" past
// the codec into a math.Int. We only need to enforce canonical denom and a
// positive amount.
func validateInsuranceCoin(coin sdk.Coin, field string) error {
	denom := strings.TrimSpace(coin.Denom)
	if denom == "" {
		return fmt.Errorf("%s denom is required", field)
	}
	if denom != coin.Denom {
		return fmt.Errorf("%s denom must be canonical", field)
	}
	if err := sdk.ValidateDenom(denom); err != nil {
		return fmt.Errorf("%s denom is invalid: %w", field, err)
	}
	if coin.Amount.IsNil() {
		return fmt.Errorf("%s amount is required", field)
	}
	if !coin.Amount.IsPositive() {
		return fmt.Errorf("%s coin amount must be positive", field)
	}
	return nil
}

func validateSignerAddress(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if _, err := sdk.AccAddressFromBech32(value); err != nil {
		return fmt.Errorf("invalid %s address: %w", field, err)
	}
	return nil
}

func validateRequiredInsuranceIdentifier(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must be canonical", field)
	}
	if len(value) > MaxInsuranceIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)",
			field, MaxInsuranceIDLen, len(value))
	}
	return nil
}

func validateOptionalInsuranceIdentifier(field, value string) error {
	if value == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value {
		return fmt.Errorf("%s must be canonical", field)
	}
	if len(value) > MaxInsuranceIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)",
			field, MaxInsuranceIDLen, len(value))
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdateParams.
// Full parameter validation is performed by the keeper with cosmos_full build tag.
func (m *MsgUpdateParams) ValidateBasic() error {
	if err := validateSignerAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgProcessContribution.
func (m *MsgProcessContribution) ValidateBasic() error {
	if err := validateSignerAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validateRequiredInsuranceIdentifier("receipt_id", m.GetReceiptId()); err != nil {
		return err
	}
	if err := validateRequiredInsuranceIdentifier("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if err := validateRequiredInsuranceIdentifier("publisher_id", m.GetPublisherId()); err != nil {
		return err
	}
	if err := validateOptionalInsuranceIdentifier("policy_version", m.GetPolicyVersion()); err != nil {
		return err
	}
	if m.GetAmount().Amount.IsNil() {
		return fmt.Errorf("amount is required")
	}
	if err := validateInsuranceCoin(m.GetAmount(), "amount"); err != nil {
		return err
	}
	return nil
}

// MaxEvidenceEntries / MaxEvidenceFieldLen cap the per-claim
// evidence slice to bound attacker-controlled storage on a filed
// claim. Each Evidence entry is 4 strings (type / hash / uri /
// description); a legitimate claim has 1-10 entries with kilobyte-
// scale content. 64 entries × 4 KiB/field = ~1 MiB ceiling per
// claim, which is ~100x any realistic claim payload.
const (
	MaxEvidenceEntries  = 64
	MaxEvidenceFieldLen = 4 * 1024

	// MaxInsuranceIDLen bounds slug/id/address-shaped fields on
	// insurance Msgs — receipt_id, tool_id, publisher_id, claim_id,
	// recipient, claimant. Realistic values are bech32 addresses
	// (~43), UUIDs (~36), or reverse-dns tool IDs (~50); 256 is
	// ~4x realistic and matches the cross-module MaxIDLen constant
	// used by x/vaults and x/payment_rails.
	MaxInsuranceIDLen = 256

	// MaxClaimReasonLen bounds the free-form `reason` string on
	// MsgFileClaim. Unlike IDs, reason is human-readable text —
	// realistic values are 1-3 sentences (<500 bytes). 4 KiB
	// matches MaxEvidenceFieldLen and gives ~8x-10x realistic
	// legitimate headroom before the cap trips.
	MaxClaimReasonLen = 4 * 1024
)

// ValidateBasic performs stateless validation on MsgFileClaim.
func (m *MsgFileClaim) ValidateBasic() error {
	if strings.TrimSpace(m.GetClaimant()) == "" {
		return fmt.Errorf("claimant is required")
	}
	if len(m.GetClaimant()) > MaxInsuranceIDLen {
		return fmt.Errorf("claimant exceeds %d-byte cap (got %d)",
			MaxInsuranceIDLen, len(m.GetClaimant()))
	}
	if _, err := sdk.AccAddressFromBech32(m.GetClaimant()); err != nil {
		return fmt.Errorf("invalid claimant address: %w", err)
	}
	if err := validateRequiredInsuranceIdentifier("receipt_id", m.GetReceiptId()); err != nil {
		return err
	}
	if err := validateRequiredInsuranceIdentifier("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if err := validateOptionalInsuranceIdentifier("publisher_id", m.GetPublisherId()); err != nil {
		return err
	}
	if m.GetClaimedAmount().Amount.IsNil() {
		return fmt.Errorf("claimed_amount is required")
	}
	if err := validateInsuranceCoin(m.GetClaimedAmount(), "claimed_amount"); err != nil {
		return err
	}
	reason := m.GetReason()
	trimmedReason := strings.TrimSpace(reason)
	if trimmedReason == "" {
		return fmt.Errorf("reason is required")
	}
	if trimmedReason != reason {
		return fmt.Errorf("reason must be canonical")
	}
	if len(reason) > MaxClaimReasonLen {
		return fmt.Errorf("reason exceeds %d-byte cap (got %d)",
			MaxClaimReasonLen, len(reason))
	}
	ev := m.GetEvidence()
	if len(ev) > MaxEvidenceEntries {
		return fmt.Errorf("evidence exceeds %d-entry cap (got %d)",
			MaxEvidenceEntries, len(ev))
	}
	for i, e := range ev {
		if e == nil {
			return fmt.Errorf("evidence[%d] is nil", i)
		}
		if len(e.GetType()) > MaxEvidenceFieldLen {
			return fmt.Errorf("evidence[%d].type exceeds %d-byte cap (got %d)",
				i, MaxEvidenceFieldLen, len(e.GetType()))
		}
		if len(e.GetHash()) > MaxEvidenceFieldLen {
			return fmt.Errorf("evidence[%d].hash exceeds %d-byte cap (got %d)",
				i, MaxEvidenceFieldLen, len(e.GetHash()))
		}
		if len(e.GetUri()) > MaxEvidenceFieldLen {
			return fmt.Errorf("evidence[%d].uri exceeds %d-byte cap (got %d)",
				i, MaxEvidenceFieldLen, len(e.GetUri()))
		}
		if len(e.GetDescription()) > MaxEvidenceFieldLen {
			return fmt.Errorf("evidence[%d].description exceeds %d-byte cap (got %d)",
				i, MaxEvidenceFieldLen, len(e.GetDescription()))
		}
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgProcessClaim.
func (m *MsgProcessClaim) ValidateBasic() error {
	if err := validateSignerAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validateRequiredInsuranceIdentifier("claim_id", m.GetClaimId()); err != nil {
		return err
	}
	r := m.GetResolution()
	if r != "approve" && r != "reject" && r != "partial" {
		return fmt.Errorf("resolution must be approve, reject, or partial")
	}
	if !m.GetApprovedAmount().Amount.IsNil() {
		if err := validateInsuranceCoin(m.GetApprovedAmount(), "approved_amount"); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgProcessPayout.
func (m *MsgProcessPayout) ValidateBasic() error {
	if err := validateSignerAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validateRequiredInsuranceIdentifier("claim_id", m.GetClaimId()); err != nil {
		return err
	}
	if strings.TrimSpace(m.GetRecipient()) == "" {
		return fmt.Errorf("recipient is required")
	}
	if len(m.GetRecipient()) > MaxInsuranceIDLen {
		return fmt.Errorf("recipient exceeds %d-byte cap (got %d)",
			MaxInsuranceIDLen, len(m.GetRecipient()))
	}
	if _, err := sdk.AccAddressFromBech32(m.GetRecipient()); err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	if m.GetAmount().Amount.IsNil() {
		return fmt.Errorf("amount is required")
	}
	if err := validateInsuranceCoin(m.GetAmount(), "amount"); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdatePublisherRisk.
func (m *MsgUpdatePublisherRisk) ValidateBasic() error {
	if err := validateSignerAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if err := validateRequiredInsuranceIdentifier("publisher_id", m.GetPublisherId()); err != nil {
		return err
	}
	if err := validateRequiredInsuranceIdentifier("tool_id", m.GetToolId()); err != nil {
		return err
	}
	if m.GetRiskScoreBps() > 10_000 {
		return fmt.Errorf("risk_score_bps exceeds 100%%")
	}
	if m.GetPremiumTier() != strings.TrimSpace(m.GetPremiumTier()) {
		return fmt.Errorf("premium_tier must be canonical")
	}
	notes := m.GetNotes()
	if strings.TrimSpace(notes) != notes {
		return fmt.Errorf("notes must be canonical")
	}
	if len(notes) > MaxClaimReasonLen {
		return fmt.Errorf("notes exceeds %d-byte cap (got %d)", MaxClaimReasonLen, len(notes))
	}
	return nil
}
