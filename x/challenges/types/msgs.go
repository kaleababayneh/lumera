package types

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// MaxRequiredCategories caps how many category strings a single
	// MsgCreateChallenge may list. Real challenges cite 1-5 categories
	// ("defi", "nlp", "vision", etc.); 64 is ~13x realistic max.
	// Without this cap, a creator could embed 100k+ categories, each
	// of length MaxCategoryLen — bloating the stored Challenge record
	// and slowing every participant-join check (keeper.go:582-591
	// iterates RequiredCategories per-join).
	MaxRequiredCategories = 64
	// MaxCategoryLen caps the individual category string length.
	// Registry categories are short slugs; 128 bytes is generous.
	MaxCategoryLen = 128
	// MaxChallengeIDLen caps challenge/tool identifier strings in all
	// challenge Msgs. 256 matches MaxIDLen across sibling modules
	// (insurance, nft, payment_rails).
	MaxChallengeIDLen = 256
	// MaxChallengeTitleLen caps the human-readable title on
	// MsgCreateChallenge. Real titles are one-liners well under 256
	// bytes; 512 leaves headroom for non-ASCII while rejecting the
	// max-tx-size amplification surface (~10 MiB protobuf admits
	// megabyte-scale titles that persist in the stored Challenge
	// record and get emitted in every list/query response).
	MaxChallengeTitleLen = 512
	// MaxChallengeDescriptionLen caps the free-form description.
	// 4 KiB matches MaxClaimReasonLen in insurance (same shape —
	// human-readable multi-paragraph text of a few KiB in legitimate
	// use, caps at ~10x realistic).
	MaxChallengeDescriptionLen = 4 * 1024
)

func IsKnownChallengeType(challengeType ChallengeType) bool {
	_, ok := ChallengeType_name[int32(challengeType)]
	return ok
}

func IsProtocolChallengeType(challengeType ChallengeType) bool {
	switch challengeType {
	case ChallengeType_CHALLENGE_TYPE_IDENTITY_ATTESTATION,
		ChallengeType_CHALLENGE_TYPE_SLO_PROBE,
		ChallengeType_CHALLENGE_TYPE_TEE_REPORT,
		ChallengeType_CHALLENGE_TYPE_RECEIPT_REPLAY:
		return true
	default:
		return false
	}
}

func ChallengeTypeLabel(challengeType ChallengeType) string {
	switch challengeType {
	case ChallengeType_CHALLENGE_TYPE_PERFORMANCE:
		return "performance"
	case ChallengeType_CHALLENGE_TYPE_QUALITY:
		return "quality"
	case ChallengeType_CHALLENGE_TYPE_CONFORMANCE:
		return "conformance"
	case ChallengeType_CHALLENGE_TYPE_COMPOSITE:
		return "composite"
	case ChallengeType_CHALLENGE_TYPE_IDENTITY_ATTESTATION:
		return "identity_attestation"
	case ChallengeType_CHALLENGE_TYPE_SLO_PROBE:
		return "slo_probe"
	case ChallengeType_CHALLENGE_TYPE_TEE_REPORT:
		return "tee_report"
	case ChallengeType_CHALLENGE_TYPE_RECEIPT_REPLAY:
		return "receipt_replay"
	default:
		return "unspecified"
	}
}

func validateChallengeAddress(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > MaxChallengeIDLen {
		return fmt.Errorf("%s length %d exceeds maximum %d", field, len(value), MaxChallengeIDLen)
	}
	if _, err := sdk.AccAddressFromBech32(value); err != nil {
		return fmt.Errorf("invalid %s address: %w", field, err)
	}
	return nil
}

func validateChallengeIdentifier(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return fmt.Errorf("%s must not have leading or trailing whitespace", field)
	}
	if len(value) > MaxChallengeIDLen {
		return fmt.Errorf("%s length %d exceeds maximum %d", field, len(value), MaxChallengeIDLen)
	}
	return nil
}

func validateChallengeCoin(field string, coin sdk.Coin) error {
	if coin.Denom == "" {
		return fmt.Errorf("%s is required", field)
	}
	if err := sdk.ValidateDenom(coin.Denom); err != nil {
		return fmt.Errorf("%s denom is invalid: %w", field, err)
	}
	if coin.Amount.IsNil() {
		return fmt.Errorf("%s amount must be set", field)
	}
	if coin.Amount.IsNegative() {
		return fmt.Errorf("%s amount must not be negative", field)
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgCreateChallenge.
func (m *MsgCreateChallenge) ValidateBasic() error {
	if err := validateChallengeAddress("creator", m.GetCreator()); err != nil {
		return err
	}
	if strings.TrimSpace(m.GetTitle()) == "" {
		return fmt.Errorf("title is required")
	}
	if len(m.GetTitle()) > MaxChallengeTitleLen {
		return fmt.Errorf("title length %d exceeds maximum %d", len(m.GetTitle()), MaxChallengeTitleLen)
	}
	if len(m.GetDescription()) > MaxChallengeDescriptionLen {
		return fmt.Errorf("description length %d exceeds maximum %d", len(m.GetDescription()), MaxChallengeDescriptionLen)
	}
	if err := validateChallengeCoin("prize_pool", m.GetPrizePool()); err != nil {
		return err
	}
	if challengeType := m.GetChallengeType(); challengeType != ChallengeType_CHALLENGE_TYPE_UNSPECIFIED && !IsKnownChallengeType(challengeType) {
		return fmt.Errorf("challenge_type has invalid value %d", challengeType)
	}
	if m.GetStartsAt().IsZero() {
		return fmt.Errorf("starts_at is required")
	}
	if m.GetEndsAt().IsZero() {
		return fmt.Errorf("ends_at is required")
	}
	if !m.GetStartsAt().IsZero() && !m.GetEndsAt().IsZero() {
		if !m.GetEndsAt().After(m.GetStartsAt()) {
			return fmt.Errorf("ends_at must be after starts_at")
		}
	}
	cats := m.GetRequiredCategories()
	if len(cats) > MaxRequiredCategories {
		return fmt.Errorf("required_categories exceeds %d-entry cap (got %d)",
			MaxRequiredCategories, len(cats))
	}
	for i, c := range cats {
		trimmed := strings.TrimSpace(c)
		if trimmed == "" {
			return fmt.Errorf("required_categories[%d] must not be blank", i)
		}
		if trimmed != c {
			return fmt.Errorf("required_categories[%d] must not have leading or trailing whitespace", i)
		}
		if len(c) > MaxCategoryLen {
			return fmt.Errorf("required_categories[%d] exceeds %d-byte cap (got %d)",
				i, MaxCategoryLen, len(c))
		}
	}
	if w := m.GetScoringWeights(); w != nil {
		total := uint64(w.LatencyWeightBps) + uint64(w.CostWeightBps) + uint64(w.AccuracyWeightBps) +
			uint64(w.ReliabilityWeightBps) + uint64(w.ConformanceWeightBps)
		if total != 10000 {
			return fmt.Errorf("scoring weights must sum to exactly 10000 bps, got %d", total)
		}
	}
	if dist := m.GetPrizeDistribution(); dist != nil {
		var total uint64
		for _, bps := range dist.WinnerSharesBps {
			total += uint64(bps)
		}
		if total > 10000 {
			return fmt.Errorf("prize distribution winner shares cannot exceed 10000 bps, got %d", total)
		}
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgJoinChallenge.
func (m *MsgJoinChallenge) ValidateBasic() error {
	if err := validateChallengeAddress("publisher", m.GetPublisher()); err != nil {
		return err
	}
	if err := validateChallengeIdentifier("challenge_id", m.GetChallengeId()); err != nil {
		return err
	}
	if err := validateChallengeIdentifier("tool_id", m.GetToolId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgSubmitResult.
func (m *MsgSubmitResult) ValidateBasic() error {
	if err := validateChallengeAddress("submitter", m.GetSubmitter()); err != nil {
		return err
	}
	if err := validateChallengeIdentifier("challenge_id", m.GetChallengeId()); err != nil {
		return err
	}
	if err := validateChallengeIdentifier("tool_id", m.GetToolId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgActivateChallenge.
func (m *MsgActivateChallenge) ValidateBasic() error {
	if err := validateChallengeAddress("creator", m.GetCreator()); err != nil {
		return err
	}
	if err := validateChallengeIdentifier("challenge_id", m.GetChallengeId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgCancelChallenge.
func (m *MsgCancelChallenge) ValidateBasic() error {
	if err := validateChallengeAddress("creator", m.GetCreator()); err != nil {
		return err
	}
	if err := validateChallengeIdentifier("challenge_id", m.GetChallengeId()); err != nil {
		return err
	}
	return nil
}

// ValidateBasic performs stateless validation on MsgUpdateParams.
func (m *MsgUpdateParams) ValidateBasic() error {
	if err := validateChallengeAddress("authority", m.GetAuthority()); err != nil {
		return err
	}
	if m.GetParams() == nil {
		return fmt.Errorf("params is required")
	}
	return m.GetParams().Validate()
}

func (m *MsgCreateChallenge) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetCreator())
	return []sdk.AccAddress{addr}
}

func (m *MsgJoinChallenge) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetPublisher())
	return []sdk.AccAddress{addr}
}

func (m *MsgSubmitResult) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetSubmitter())
	return []sdk.AccAddress{addr}
}

func (m *MsgActivateChallenge) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetCreator())
	return []sdk.AccAddress{addr}
}

func (m *MsgCancelChallenge) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetCreator())
	return []sdk.AccAddress{addr}
}

func (m *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(m.GetAuthority())
	return []sdk.AccAddress{addr}
}
