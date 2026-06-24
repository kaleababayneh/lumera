package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

// EndBlocker processes challenge lifecycle transitions at the end of each
// block. It walks all challenges and:
//
//  1. Transitions ACTIVE challenges past their EndsAt deadline to SCORING
//     and records the block height for scoring delay enforcement.
//  2. For SCORING challenges, waits scoring_delay_blocks (from params) before
//     normalizing scores, computing rankings, and transitioning to COMPLETED.
//  3. For COMPLETED challenges with unpaid prizes, distributes payouts.
//
// Each challenge is processed independently; a failure in one challenge is
// logged but does not halt processing of subsequent challenges.
func (k *Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()
	blockHeight := sdkCtx.BlockHeight()
	logger := k.Logger(sdkCtx)

	challenges := make([]*types.Challenge, 0)
	for _, status := range []types.ChallengeStatus{
		types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		types.ChallengeStatus_CHALLENGE_STATUS_SCORING,
		types.ChallengeStatus_CHALLENGE_STATUS_COMPLETED,
	} {
		chList, err := k.GetChallengesByStatus(ctx, status)
		if err != nil {
			logger.Error("EndBlocker: failed to load challenges by status", "status", status, "error", err)
			continue
		}
		challenges = append(challenges, chList...)
	}

	params := k.GetParams(ctx)

	for _, ch := range challenges {
		switch ch.Status {
		case types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE:
			// Check if the challenge deadline has passed.
			if ch.EndsAt.IsZero() || blockTime.Before(ch.EndsAt) {
				continue
			}

			logger.Info("EndBlocker: challenge deadline reached, transitioning to SCORING",
				"challenge_id", ch.ChallengeId)

			if err := k.TransitionStatus(ctx, ch.ChallengeId,
				types.ChallengeStatus_CHALLENGE_STATUS_SCORING); err != nil {
				logger.Error("EndBlocker: failed to transition to SCORING",
					"challenge_id", ch.ChallengeId, "error", err)
				continue
			}
			k.observeChallengeExpired(ch)

			// Record the block height when entering SCORING for delay enforcement.
			if err := k.scoringEnteredAt.Set(ctx, ch.ChallengeId, blockHeight); err != nil {
				logger.Error("EndBlocker: failed to record scoring entry block",
					"challenge_id", ch.ChallengeId, "error", err)
			}

			// If scoring delay is zero, score immediately in the same block.
			if params.ScoringDelayBlocks == 0 {
				if err := k.executeScoring(ctx, ch.ChallengeId); err != nil {
					logger.Error("EndBlocker: immediate scoring failed",
						"challenge_id", ch.ChallengeId, "error", err)
				}
			}

		case types.ChallengeStatus_CHALLENGE_STATUS_SCORING:
			// Enforce scoring delay: only score after scoring_delay_blocks
			// have elapsed since the challenge entered SCORING state.
			enteredAt, lookupErr := k.scoringEnteredAt.Get(ctx, ch.ChallengeId)
			if lookupErr != nil {
				// No record means this is a legacy challenge or the record was
				// lost. Fall through and score immediately to avoid permanent stall.
				logger.Info("EndBlocker: no scoring entry record, scoring immediately",
					"challenge_id", ch.ChallengeId)
			} else if blockHeight < enteredAt+int64(params.ScoringDelayBlocks) {
				logger.Debug("EndBlocker: scoring delay not yet elapsed",
					"challenge_id", ch.ChallengeId,
					"entered_at", enteredAt,
					"current", blockHeight,
					"delay", params.ScoringDelayBlocks)
				continue
			}

			if err := k.executeScoring(ctx, ch.ChallengeId); err != nil {
				logger.Error("EndBlocker: scoring failed",
					"challenge_id", ch.ChallengeId, "error", err)
			} else {
				// Clean up the entry record only after successful scoring.
				// Preserving it on failure ensures the delay is respected on retry.
				if err := k.scoringEnteredAt.Remove(ctx, ch.ChallengeId); err != nil {
					logger.Error("EndBlocker: failed to clean up scoring entry record",
						"challenge_id", ch.ChallengeId, "error", err)
				}
			}

		case types.ChallengeStatus_CHALLENGE_STATUS_COMPLETED:
			// Retry prize distribution for challenges that scored but
			// failed payout in a previous block (e.g. bank keeper error).
			if ch.PayoutsComplete {
				continue
			}

			if err := k.DistributePrizes(ctx, ch.ChallengeId); err != nil {
				logger.Error("EndBlocker: retry prize distribution failed",
					"challenge_id", ch.ChallengeId, "error", err)
			}
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		fmt.Sprintf("%s.end_block", types.ModuleName),
		sdk.NewAttribute("challenges_evaluated", fmt.Sprintf("%d", len(challenges))),
	))

	return nil
}

// executeScoring normalizes scores, computes rankings, and distributes prizes
// for a challenge that is in SCORING state.
func (k *Keeper) executeScoring(ctx context.Context, challengeID string) error {
	if err := k.NormalizeScores(ctx, challengeID); err != nil {
		return fmt.Errorf("normalize scores: %w", err)
	}

	if err := k.ScoreAndRankChallenge(ctx, challengeID); err != nil {
		return fmt.Errorf("score and rank: %w", err)
	}

	if err := k.DistributePrizes(ctx, challengeID); err != nil {
		return fmt.Errorf("distribute prizes: %w", err)
	}

	return nil
}
