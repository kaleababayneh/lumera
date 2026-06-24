package types

import (
	"fmt"
	"strings"

	"time"
)

// DefaultGenesisState returns a default genesis state for the challenges module.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params: &Params{
			MinPrizePoolLac:       1_000_000,
			MaxDurationBlocks:     100_000,
			MinParticipants:       2,
			EntryFeePercentageBps: 100,
			PlatformFeeBps:        500,
			ScoringDelayBlocks:    10,
		},
	}
}

// Validate checks that Params fields are within acceptable bounds.
func (p *Params) Validate() error {
	if p == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if p.MinParticipants == 0 {
		return fmt.Errorf("min_participants must be > 0")
	}
	if p.PlatformFeeBps > 10_000 {
		return fmt.Errorf("platform_fee_bps must be <= 10000")
	}
	if p.EntryFeePercentageBps > 10_000 {
		return fmt.Errorf("entry_fee_percentage_bps must be <= 10000")
	}
	return nil
}

// Validate performs validation of the genesis state.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if gs.Params != nil {
		if err := gs.Params.Validate(); err != nil {
			return err
		}
	}

	// Validate challenge IDs are unique.
	challengeByID := make(map[string]*Challenge, len(gs.Challenges))
	for i, ch := range gs.Challenges {
		if ch == nil {
			return fmt.Errorf("challenge entry %d cannot be nil", i)
		}
		if err := validateGenesisChallengeID(i, ch.ChallengeId); err != nil {
			return err
		}
		if _, ok := challengeByID[ch.ChallengeId]; ok {
			return fmt.Errorf("duplicate challenge id: %s", ch.ChallengeId)
		}
		challengeByID[ch.ChallengeId] = ch
		if err := validateGenesisTimestamp("challenge "+ch.ChallengeId, "created_at", ch.CreatedAt); err != nil {
			return err
		}
		if err := validateGenesisTimestamp("challenge "+ch.ChallengeId, "starts_at", ch.StartsAt); err != nil {
			return err
		}
		if err := validateGenesisTimestamp("challenge "+ch.ChallengeId, "ends_at", ch.EndsAt); err != nil {
			return err
		}
		if err := validateGenesisTimestamp("challenge "+ch.ChallengeId, "scored_at", ch.ScoredAt); err != nil {
			return err
		}
		if err := validateGenesisChallengeTimeline(ch); err != nil {
			return err
		}
		if err := validateGenesisChallengeStatus(ch); err != nil {
			return err
		}
		if err := validateGenesisChallengeType(ch); err != nil {
			return err
		}
	}

	participantKeys := make(map[string]struct{}, len(gs.Participants))
	for i, p := range gs.Participants {
		if p == nil {
			return fmt.Errorf("participant entry %d cannot be nil", i)
		}
		if err := validateGenesisScopedToolRow("participant", i, p.ChallengeId, p.ToolId, challengeByID, participantKeys); err != nil {
			return err
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("participant entry %d", i), "registered_at", p.RegisteredAt); err != nil {
			return err
		}
	}

	submissionKeys := make(map[string]struct{}, len(gs.Submissions))
	for i, s := range gs.Submissions {
		if s == nil {
			return fmt.Errorf("submission entry %d cannot be nil", i)
		}
		if err := validateGenesisScopedToolRow("submission", i, s.ChallengeId, s.ToolId, challengeByID, submissionKeys); err != nil {
			return err
		}
		if _, ok := participantKeys[genesisScopedToolKey(s.ChallengeId, s.ToolId)]; !ok {
			return fmt.Errorf("submission entry %d references unregistered participant for challenge %s tool %s", i, s.ChallengeId, s.ToolId)
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("submission entry %d", i), "submitted_at", s.SubmittedAt); err != nil {
			return err
		}
		if err := validateGenesisSubmissionWindow(i, s, challengeByID[s.ChallengeId]); err != nil {
			return err
		}
	}

	rankingKeys := make(map[string]struct{}, len(gs.Rankings))
	for i, r := range gs.Rankings {
		if r == nil {
			return fmt.Errorf("ranking entry %d cannot be nil", i)
		}
		if err := validateGenesisScopedToolRow("ranking", i, r.ChallengeId, r.ToolId, challengeByID, rankingKeys); err != nil {
			return err
		}
		if _, ok := participantKeys[genesisScopedToolKey(r.ChallengeId, r.ToolId)]; !ok {
			return fmt.Errorf("ranking entry %d references unregistered participant for challenge %s tool %s", i, r.ChallengeId, r.ToolId)
		}
		if err := validateGenesisTimestamp(fmt.Sprintf("ranking entry %d", i), "claimed_at", r.ClaimedAt); err != nil {
			return err
		}
	}

	return nil
}

func validateGenesisChallengeID(index int, challengeID string) error {
	if challengeID == "" {
		return fmt.Errorf("challenge missing id")
	}
	if err := validateGenesisIdentifier("challenge", index, "challenge_id", challengeID); err != nil {
		return err
	}
	return nil
}

func validateGenesisChallengeTimeline(ch *Challenge) error {
	if !ch.StartsAt.IsZero() && !ch.EndsAt.IsZero() && !ch.EndsAt.After(ch.StartsAt) {
		return fmt.Errorf("challenge %s ends_at must be after starts_at", ch.ChallengeId)
	}
	return nil
}

func validateGenesisChallengeStatus(ch *Challenge) error {
	if _, ok := ChallengeStatus_name[int32(ch.Status)]; !ok {
		return fmt.Errorf("challenge %s has invalid status %d", ch.ChallengeId, ch.Status)
	}
	if ch.Status == ChallengeStatus_CHALLENGE_STATUS_UNSPECIFIED {
		return fmt.Errorf("challenge %s status must be specified", ch.ChallengeId)
	}
	return nil
}

func validateGenesisChallengeType(ch *Challenge) error {
	if ch.ChallengeType == ChallengeType_CHALLENGE_TYPE_UNSPECIFIED {
		return nil
	}
	if !IsKnownChallengeType(ch.ChallengeType) {
		return fmt.Errorf("challenge %s has invalid challenge_type %d", ch.ChallengeId, ch.ChallengeType)
	}
	return nil
}

func validateGenesisSubmissionWindow(index int, s *Submission, ch *Challenge) error {
	if s.SubmittedAt.IsZero() || ch == nil {
		return nil
	}
	submittedAt := s.SubmittedAt
	if !ch.StartsAt.IsZero() && submittedAt.Before(ch.StartsAt) {
		return fmt.Errorf("submission entry %d submitted_at must be at or after challenge %s starts_at", index, s.ChallengeId)
	}
	if !ch.EndsAt.IsZero() && !submittedAt.Before(ch.EndsAt) {
		return fmt.Errorf("submission entry %d submitted_at must be before challenge %s ends_at", index, s.ChallengeId)
	}
	return nil
}

func validateGenesisTimestamp(_, _ string, ts time.Time) error {
	// Gogo stdtime fields are plain time.Time and are always structurally valid;
	// the zero value denotes "unset", which is acceptable in genesis.
	_ = ts
	return nil
}

func validateGenesisScopedToolRow(
	kind string,
	index int,
	challengeID string,
	toolID string,
	challengeByID map[string]*Challenge,
	seen map[string]struct{},
) error {
	if challengeID == "" {
		return fmt.Errorf("%s entry %d missing challenge_id", kind, index)
	}
	if err := validateGenesisIdentifier(kind, index, "challenge_id", challengeID); err != nil {
		return err
	}
	if toolID == "" {
		return fmt.Errorf("%s entry %d missing tool_id", kind, index)
	}
	if err := validateGenesisIdentifier(kind, index, "tool_id", toolID); err != nil {
		return err
	}
	if _, ok := challengeByID[challengeID]; !ok {
		return fmt.Errorf("%s entry %d references unknown challenge id: %s", kind, index, challengeID)
	}
	key := genesisScopedToolKey(challengeID, toolID)
	if _, ok := seen[key]; ok {
		return fmt.Errorf("duplicate %s for challenge %s tool %s", kind, challengeID, toolID)
	}
	seen[key] = struct{}{}
	return nil
}

func genesisScopedToolKey(challengeID, toolID string) string {
	return challengeID + "\x00" + toolID
}

func validateGenesisIdentifier(kind string, index int, field string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s entry %d %s must not be blank", kind, index, field)
	}
	if trimmed != value {
		return fmt.Errorf("%s entry %d %s must not have leading or trailing whitespace", kind, index, field)
	}
	if len(value) > MaxChallengeIDLen {
		return fmt.Errorf("%s entry %d %s length %d exceeds maximum %d", kind, index, field, len(value), MaxChallengeIDLen)
	}
	return nil
}
