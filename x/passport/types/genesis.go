package types

import (
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default genesis state for the passport module.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:    DefaultParams(),
		Passports: []*AgentPassport{},
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	if gs.Params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	seen := make(map[string]bool)
	agentSeen := make(map[string]bool)

	for i, passport := range gs.Passports {
		if passport == nil {
			return fmt.Errorf("passport at index %d is nil", i)
		}
		if err := validatePassportID(passport.PassportId); err != nil {
			return fmt.Errorf("passport at index %d has invalid passport_id: %w", i, err)
		}
		if seen[passport.PassportId] {
			return fmt.Errorf("duplicate passport ID: %s", passport.PassportId)
		}
		seen[passport.PassportId] = true

		agentPubkey := canonicalGenesisAgentPubkey(passport.AgentPubkey)
		if agentPubkey == "" {
			return fmt.Errorf("passport %s has empty agent pubkey", passport.PassportId)
		}
		if len(passport.AgentPubkey) > MaxAgentPubkeyLen {
			return fmt.Errorf(
				"passport %s agent_pubkey length %d exceeds maximum %d",
				passport.PassportId,
				len(passport.AgentPubkey),
				MaxAgentPubkeyLen,
			)
		}
		if agentSeen[agentPubkey] {
			return fmt.Errorf("duplicate agent pubkey: %s", agentPubkey)
		}
		agentSeen[agentPubkey] = true

		if strings.TrimSpace(passport.OwnerAddress) == "" {
			return fmt.Errorf("passport %s has empty owner address", passport.PassportId)
		}
		if _, err := sdk.AccAddressFromBech32(passport.OwnerAddress); err != nil {
			return fmt.Errorf("passport %s has invalid owner address: %w", passport.PassportId, err)
		}
		if err := validateGenesisPassportLifecycle(passport); err != nil {
			return err
		}
		if err := passport.Summary.Validate(); err != nil {
			return fmt.Errorf("passport %s has invalid summary: %w", passport.PassportId, err)
		}
		if err := validatePassportGenesisTimestamps(passport); err != nil {
			return err
		}
	}

	return nil
}

func canonicalGenesisAgentPubkey(agentPubkey string) string {
	return strings.ToLower(strings.TrimSpace(agentPubkey))
}

func validateGenesisPassportLifecycle(passport *AgentPassport) error {
	switch passport.Status {
	case PassportStatus_PASSPORT_STATUS_ACTIVE,
		PassportStatus_PASSPORT_STATUS_SUSPENDED,
		PassportStatus_PASSPORT_STATUS_REVOKED:
	default:
		return fmt.Errorf("passport %s has invalid status: %s", passport.PassportId, passport.Status.String())
	}
	stake := passport.Stake
	if stake.Denom == "" || stake.Amount.IsNil() || !stake.IsPositive() {
		return fmt.Errorf("passport %s stake must be a positive coin", passport.PassportId)
	}
	return nil
}

func validatePassportGenesisTimestamps(passport *AgentPassport) error {
	passportID := passport.PassportId
	if passport.Reputation != nil {
		if err := validateGenesisTimestamp(passportID, "reputation.updated_at", passport.Reputation.GetUpdatedAt()); err != nil {
			return err
		}
	}
	if err := validateGenesisScoreBreakdownTimestamp(passportID, "score_breakdown", passport.ScoreBreakdown); err != nil {
		return err
	}
	if passport.TierState != nil {
		for _, field := range []struct {
			name string
			ts   *time.Time
		}{
			{name: "tier_state.tier_entered_at", ts: passport.TierState.GetTierEnteredAt()},
			{name: "tier_state.promotion_started_at", ts: passport.TierState.GetPromotionStartedAt()},
			{name: "tier_state.lockup_expires_at", ts: passport.TierState.GetLockupExpiresAt()},
		} {
			if err := validateGenesisTimestamp(passportID, field.name, field.ts); err != nil {
				return err
			}
		}
	}
	for i, entry := range passport.TierHistory {
		if entry == nil {
			return fmt.Errorf("passport %s tier_history[%d] cannot be nil", passportID, i)
		}
		prefix := fmt.Sprintf("tier_history[%d]", i)
		if err := validateGenesisScoreBreakdownTimestamp(passportID, prefix+".score_breakdown", entry.ScoreBreakdown); err != nil {
			return err
		}
		if err := validateGenesisTimestamp(passportID, prefix+".transitioned_at", entry.GetTransitionedAt()); err != nil {
			return err
		}
	}
	return nil
}

func validateGenesisScoreBreakdownTimestamp(passportID, field string, score *PassportScoreBreakdown) error {
	if score == nil {
		return nil
	}
	return validateGenesisTimestamp(passportID, field+".updated_at", score.GetUpdatedAt())
}

func validateGenesisTimestamp(passportID, field string, ts *time.Time) error {
	if ts == nil {
		return nil
	}
	// gogoproto stdtime decodes the wire timestamp into a Go time.Time, so a
	// non-nil pointer is already a structurally valid instant. Reject the
	// degenerate zero value, which signals a malformed/unset timestamp that
	// slipped through as a present-but-empty field.
	if ts.IsZero() {
		return fmt.Errorf("passport %s %s is invalid: zero timestamp", passportID, field)
	}
	return nil
}
