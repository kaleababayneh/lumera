package keeper

import (
	"strconv"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gometrics "github.com/hashicorp/go-metrics"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

const (
	metricChallengesIssuedTotal    = "challenges_issued_total"
	metricChallengesExpiredTotal   = "challenges_expired_total"
	metricChallengesRespondedTotal = "challenges_responded_total"
)

func challengeClassLabel(challengeType types.ChallengeType) string {
	return types.ChallengeTypeLabel(challengeType)
}

func challengeMetricLabels(ch *types.Challenge) []gometrics.Label {
	challengeClass := "unspecified"
	if ch != nil {
		challengeClass = challengeClassLabel(ch.ChallengeType)
	}
	return []gometrics.Label{
		telemetry.NewLabel(types.AttributeKeyChallengeClass, challengeClass),
	}
}

func (k *Keeper) observeChallengeIssued(ch *types.Challenge) {
	telemetry.IncrCounterWithLabels(
		[]string{metricChallengesIssuedTotal},
		1,
		challengeMetricLabels(ch),
	)
}

func (k *Keeper) observeChallengeExpired(ch *types.Challenge) {
	telemetry.IncrCounterWithLabels(
		[]string{metricChallengesExpiredTotal},
		1,
		challengeMetricLabels(ch),
	)
}

func (k *Keeper) observeChallengeResponded(ch *types.Challenge) {
	telemetry.IncrCounterWithLabels(
		[]string{metricChallengesRespondedTotal},
		1,
		challengeMetricLabels(ch),
	)
}

func challengeStatusOutcome(status types.ChallengeStatus) string {
	switch status {
	case types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE:
		return "activated"
	case types.ChallengeStatus_CHALLENGE_STATUS_SCORING:
		return "expired"
	case types.ChallengeStatus_CHALLENGE_STATUS_COMPLETED:
		return "completed"
	case types.ChallengeStatus_CHALLENGE_STATUS_CANCELLED:
		return "cancelled"
	default:
		return "unknown"
	}
}

func challengeLifecycleAttributes(
	ctx sdk.Context,
	ch *types.Challenge,
	fromStatus types.ChallengeStatus,
	toStatus types.ChallengeStatus,
) []sdk.Attribute {
	attrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyChallengeID, ""),
		sdk.NewAttribute(types.AttributeKeyChallengeClass, "unspecified"),
		sdk.NewAttribute(types.AttributeKeyCreator, ""),
		sdk.NewAttribute(types.AttributeKeyFromStatus, fromStatus.String()),
		sdk.NewAttribute(types.AttributeKeyToStatus, toStatus.String()),
		sdk.NewAttribute(types.AttributeKeyNewStatus, toStatus.String()),
		sdk.NewAttribute(types.AttributeKeyBlockHeight, strconv.FormatInt(ctx.BlockHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyOutcome, challengeStatusOutcome(toStatus)),
	}

	if ch == nil {
		return attrs
	}

	attrs[0] = sdk.NewAttribute(types.AttributeKeyChallengeID, ch.ChallengeId)
	attrs[1] = sdk.NewAttribute(types.AttributeKeyChallengeClass, challengeClassLabel(ch.ChallengeType))
	attrs[2] = sdk.NewAttribute(types.AttributeKeyCreator, ch.Creator)
	if !ch.EndsAt.IsZero() {
		attrs = append(attrs, sdk.NewAttribute(
			types.AttributeKeyDeadlineUnix,
			strconv.FormatInt(ch.EndsAt.Unix(), 10),
		))
	}
	return attrs
}
