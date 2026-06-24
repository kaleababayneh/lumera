package keeper

import (
	"context"
	"fmt"
	"sort"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

// ScoreAndRankChallenge computes final rankings for a challenge that is in
// SCORING status by evaluating all non-disqualified submissions and producing
// a deterministic leaderboard.
//
// The method:
//  1. Fetches all submissions for the challenge.
//  2. Filters out disqualified participants.
//  3. Computes composite scores using challenge-specific weights.
//  4. Ranks participants by composite score (descending), breaking ties
//     deterministically by tool ID (lexicographic ascending).
//  5. Persists rankings and transitions the challenge to COMPLETED.
func (k *Keeper) ScoreAndRankChallenge(ctx context.Context, challengeID string) error {
	ch, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	if ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_SCORING {
		return fmt.Errorf("%w: expected SCORING, got %s", ErrInvalidStatus, ch.Status)
	}

	submissions, err := k.GetSubmissions(ctx, challengeID)
	if err != nil {
		return fmt.Errorf("fetch submissions: %w", err)
	}

	// Filter disqualified participants and recompute composite scores.
	var eligible []*types.Submission
	for _, s := range submissions {
		p, pErr := k.GetParticipant(ctx, challengeID, s.ToolId)
		if pErr != nil {
			continue
		}
		if p.Disqualified {
			continue
		}

		// Recompute composite to ensure consistency.
		if ch.ScoringWeights != nil {
			s.CompositeScore = computeComposite(s, ch.ScoringWeights)
		}
		eligible = append(eligible, s)
	}

	if len(eligible) == 0 {
		return fmt.Errorf("no eligible submissions for challenge %s", challengeID)
	}

	// Sort by composite score descending; break ties by tool ID ascending.
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].CompositeScore != eligible[j].CompositeScore {
			return eligible[i].CompositeScore > eligible[j].CompositeScore
		}
		return eligible[i].ToolId < eligible[j].ToolId
	})

	// Persist rankings.
	winnerIDs := make([]string, 0, len(eligible))
	for i, s := range eligible {
		rank := &types.Ranking{
			ChallengeId: challengeID,
			ToolId:      s.ToolId,
			Rank:        uint32(i + 1),
			FinalScore:  s.CompositeScore,
		}
		if err := k.SetRanking(ctx, rank); err != nil {
			return fmt.Errorf("set ranking for %s: %w", s.ToolId, err)
		}
		winnerIDs = append(winnerIDs, s.ToolId)
	}

	// Update challenge with winner list.
	if ch.PrizeDistribution != nil && len(ch.PrizeDistribution.WinnerSharesBps) > 0 {
		maxWinners := len(ch.PrizeDistribution.WinnerSharesBps)
		if maxWinners > len(winnerIDs) {
			maxWinners = len(winnerIDs)
		}
		ch.WinnerToolIds = winnerIDs[:maxWinners]
	} else {
		ch.WinnerToolIds = winnerIDs
	}

	// Persist winner IDs before status transition so completion updates retain
	// the finalized leaderboard payload.
	if err := k.UpdateChallenge(ctx, ch); err != nil {
		return fmt.Errorf("persist winner list: %w", err)
	}

	// Transition to completed.
	if err := k.TransitionStatus(ctx, challengeID, types.ChallengeStatus_CHALLENGE_STATUS_COMPLETED); err != nil {
		return fmt.Errorf("complete challenge: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	attrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
		sdk.NewAttribute(types.AttributeKeyParticipants, fmt.Sprintf("%d", len(eligible))),
	}
	if len(winnerIDs) > 0 {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyToolID, winnerIDs[0]))
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeScored, attrs...))

	return nil
}

// NormalizeScores rescales raw scores within a challenge so that the best
// performer in each dimension receives 10000 (100%) and others are scaled
// proportionally. This is useful before computing composite scores when raw
// scores have different scales.
func (k *Keeper) NormalizeScores(ctx context.Context, challengeID string) error {
	submissions, err := k.GetSubmissions(ctx, challengeID)
	if err != nil {
		return err
	}
	if len(submissions) == 0 {
		return nil
	}

	// Find maximums per dimension.
	var maxLat, maxCost, maxAcc, maxRel, maxConf uint32
	for _, s := range submissions {
		if s.LatencyScore > maxLat {
			maxLat = s.LatencyScore
		}
		if s.CostScore > maxCost {
			maxCost = s.CostScore
		}
		if s.AccuracyScore > maxAcc {
			maxAcc = s.AccuracyScore
		}
		if s.ReliabilityScore > maxRel {
			maxRel = s.ReliabilityScore
		}
		if s.ConformanceScore > maxConf {
			maxConf = s.ConformanceScore
		}
	}

	// Rescale each submission.
	for _, s := range submissions {
		s.LatencyScore = rescale(s.LatencyScore, maxLat)
		s.CostScore = rescale(s.CostScore, maxCost)
		s.AccuracyScore = rescale(s.AccuracyScore, maxAcc)
		s.ReliabilityScore = rescale(s.ReliabilityScore, maxRel)
		s.ConformanceScore = rescale(s.ConformanceScore, maxConf)

		if err := k.submissions.Set(ctx, collections.Join(s.ChallengeId, s.ToolId), s); err != nil {
			return fmt.Errorf("update normalized submission for %s: %w", s.ToolId, err)
		}
	}

	return nil
}

// rescale maps a value to 0-10000 relative to the given maximum.
func rescale(val, max uint32) uint32 {
	if max == 0 {
		return 0
	}
	return uint32(uint64(val) * 10_000 / uint64(max))
}
