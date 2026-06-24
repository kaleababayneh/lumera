package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

// disputeIndexSeparator is retained for backward-compatible reference
// in any external code that imported it before the length-prefix fix.
// New code should not use it; disputeSubmissionKey is the canonical
// encoder.
const disputeIndexSeparator = "\x1f"

// FileDispute records a user contestation for a challenge submission or ranking.
// The challenges module identifies submissions by (challenge_id, tool_id), so
// callers must provide both fields on the Dispute.
func (k *Keeper) FileDispute(ctx context.Context, dispute *types.Dispute) (uint64, error) {
	if dispute == nil {
		return 0, types.ErrNilDispute
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	candidate := *dispute
	if candidate.Status == types.DisputeStatusUnspecified {
		candidate.Status = types.DisputeStatusFiled
	}
	if candidate.Status != types.DisputeStatusFiled {
		return 0, types.ErrInvalidDisputeStatus.Wrapf("new dispute must start as filed, got %s", candidate.Status.String())
	}
	if candidate.FiledAt == 0 {
		candidate.FiledAt = sdkCtx.BlockTime().UnixMilli()
	}

	validationCandidate := candidate
	if validationCandidate.ID == 0 {
		validationCandidate.ID = 1
	}
	if err := validationCandidate.Validate(); err != nil {
		return 0, err
	}
	if _, err := k.GetChallenge(ctx, candidate.ChallengeID); err != nil {
		return 0, err
	}
	if err := k.ensureDisputableSubmission(ctx, candidate.ChallengeID, candidate.ToolID); err != nil {
		return 0, err
	}
	open, err := k.hasOpenDisputeForSubmission(ctx, candidate.ChallengeID, candidate.ToolID)
	if err != nil {
		return 0, err
	}
	if open {
		return 0, types.ErrInvalidDisputeStatus.Wrapf(
			"open dispute already exists for challenge %s tool %s",
			candidate.ChallengeID,
			candidate.ToolID,
		)
	}

	if candidate.ID == 0 {
		id, err := k.nextDisputeID(ctx)
		if err != nil {
			return 0, fmt.Errorf("next dispute id: %w", err)
		}
		candidate.ID = id
	}
	if err := candidate.Validate(); err != nil {
		return 0, err
	}
	if err := k.setDispute(ctx, &candidate); err != nil {
		return 0, err
	}
	*dispute = candidate

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeDisputeFiled,
		sdk.NewAttribute(types.AttributeKeyDisputeID, strconv.FormatUint(candidate.ID, 10)),
		sdk.NewAttribute(types.AttributeKeyChallengeID, candidate.ChallengeID),
		sdk.NewAttribute(types.AttributeKeyToolID, candidate.ToolID),
		sdk.NewAttribute(types.AttributeKeySubmissionID, disputeSubmissionKey(candidate.ChallengeID, candidate.ToolID)),
		sdk.NewAttribute(types.AttributeKeyFiler, candidate.FiledBy),
		sdk.NewAttribute(types.AttributeKeyReason, candidate.Reason),
		sdk.NewAttribute(types.AttributeKeyToStatus, candidate.Status.String()),
	))
	emitDisputeStatusChanged(sdkCtx, &candidate, types.DisputeStatusUnspecified)

	return candidate.ID, nil
}

// emitDisputeStatusChanged fires the uniform lifecycle-transition
// event declared in x/challenges/types/events.go (lumera_ai-x2jq4
// scaffolding). FileDispute, ResolveDispute, and WithdrawDispute
// each emit their specific event AND this uniform one, so downstream
// indexers and the policy-filter feedback path can subscribe to a
// single event type to observe every dispute state transition
// without having to listen for three distinct events.
//
// fromStatus is DisputeStatusUnspecified on the initial File path
// (the zero value represents "no prior state" — the dispute did not
// exist before). On Resolve and Withdraw, fromStatus is the
// pre-transition status as observed before ApplyResolution /
// Withdraw mutated the struct.
func emitDisputeStatusChanged(sdkCtx sdk.Context, dispute *types.Dispute, fromStatus types.DisputeStatus) {
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeDisputeStatusChanged,
		sdk.NewAttribute(types.AttributeKeyDisputeID, strconv.FormatUint(dispute.ID, 10)),
		sdk.NewAttribute(types.AttributeKeyChallengeID, dispute.ChallengeID),
		sdk.NewAttribute(types.AttributeKeyToolID, dispute.ToolID),
		sdk.NewAttribute(types.AttributeKeyFromStatus, fromStatus.String()),
		sdk.NewAttribute(types.AttributeKeyToStatus, dispute.Status.String()),
	))
}

// GetDispute returns a stored dispute by ID.
func (k *Keeper) GetDispute(ctx context.Context, id uint64) (*types.Dispute, error) {
	encoded, err := k.disputes.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrDisputeNotFound
		}
		return nil, err
	}
	return decodeDispute(encoded)
}

// GetDisputesBySubmission returns all disputes filed against a challenge/tool
// submission key in deterministic ID order.
func (k *Keeper) GetDisputesBySubmission(ctx context.Context, challengeID, toolID string) ([]*types.Dispute, error) {
	indexKey := disputeSubmissionKey(challengeID, toolID)
	rng := collections.NewPrefixedPairRange[string, uint64](indexKey)
	return k.collectIndexedDisputes(ctx, k.disputeBySubmission, rng)
}

// GetDisputesByFiler returns all disputes filed by a user address in
// deterministic ID order.
func (k *Keeper) GetDisputesByFiler(ctx context.Context, filer string) ([]*types.Dispute, error) {
	rng := collections.NewPrefixedPairRange[string, uint64](strings.TrimSpace(filer))
	return k.collectIndexedDisputes(ctx, k.disputeByFiler, rng)
}

// ResolveDispute applies an arbitrator decision to a dispute. The resolver must
// be the module authority or the challenge creator. Upheld disputes disqualify
// the disputed tool, which removes it from subsequent scoring/ranking.
func (k *Keeper) ResolveDispute(
	ctx context.Context,
	disputeID uint64,
	status types.DisputeStatus,
	resolvedBy string,
	outcome string,
) error {
	resolvedBy = strings.TrimSpace(resolvedBy)
	outcome = strings.TrimSpace(outcome)
	dispute, err := k.GetDispute(ctx, disputeID)
	if err != nil {
		return err
	}
	if dispute.Status.IsTerminal() {
		return types.ErrDisputeAlreadyResolved.Wrapf("dispute %d is %s", disputeID, dispute.Status.String())
	}

	ch, err := k.GetChallenge(ctx, dispute.ChallengeID)
	if err != nil {
		return err
	}
	if resolvedBy != ch.Creator && resolvedBy != k.authority {
		return types.ErrUnauthorized.Wrapf("resolver %s is neither challenge creator nor module authority", resolvedBy)
	}

	fromStatus := dispute.Status
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := dispute.ApplyResolution(status, resolvedBy, sdkCtx.BlockTime().UnixMilli(), outcome); err != nil {
		return types.ErrInvalidDisputeTransition.Wrap(err.Error())
	}
	if status == types.DisputeStatusUpheld {
		reason := outcome
		if reason == "" {
			reason = fmt.Sprintf("upheld dispute %d", dispute.ID)
		}
		if err := k.DisqualifyParticipant(ctx, dispute.ChallengeID, dispute.ToolID, resolvedBy, reason); err != nil {
			return err
		}
	}
	if err := k.setDispute(ctx, dispute); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeDisputeResolved,
		sdk.NewAttribute(types.AttributeKeyDisputeID, strconv.FormatUint(dispute.ID, 10)),
		sdk.NewAttribute(types.AttributeKeyChallengeID, dispute.ChallengeID),
		sdk.NewAttribute(types.AttributeKeyToolID, dispute.ToolID),
		sdk.NewAttribute(types.AttributeKeyResolvedBy, resolvedBy),
		sdk.NewAttribute(types.AttributeKeyFromStatus, fromStatus.String()),
		sdk.NewAttribute(types.AttributeKeyToStatus, dispute.Status.String()),
		sdk.NewAttribute(types.AttributeKeyOutcome, outcome),
	))
	emitDisputeStatusChanged(sdkCtx, dispute, fromStatus)

	return nil
}

// WithdrawDispute lets the original filer close a dispute before an arbitrator
// decision.
func (k *Keeper) WithdrawDispute(ctx context.Context, disputeID uint64, filer string) error {
	filer = strings.TrimSpace(filer)
	dispute, err := k.GetDispute(ctx, disputeID)
	if err != nil {
		return err
	}
	if dispute.FiledBy != filer {
		return types.ErrUnauthorized.Wrapf("only filer %s can withdraw dispute %d", dispute.FiledBy, disputeID)
	}

	fromStatus := dispute.Status
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := dispute.Withdraw(sdkCtx.BlockTime().UnixMilli()); err != nil {
		return types.ErrInvalidDisputeTransition.Wrap(err.Error())
	}
	if err := k.setDispute(ctx, dispute); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeDisputeWithdrawn,
		sdk.NewAttribute(types.AttributeKeyDisputeID, strconv.FormatUint(dispute.ID, 10)),
		sdk.NewAttribute(types.AttributeKeyChallengeID, dispute.ChallengeID),
		sdk.NewAttribute(types.AttributeKeyToolID, dispute.ToolID),
		sdk.NewAttribute(types.AttributeKeyFiler, filer),
		sdk.NewAttribute(types.AttributeKeyFromStatus, fromStatus.String()),
		sdk.NewAttribute(types.AttributeKeyToStatus, dispute.Status.String()),
	))
	emitDisputeStatusChanged(sdkCtx, dispute, fromStatus)

	return nil
}

func (k *Keeper) setDispute(ctx context.Context, dispute *types.Dispute) error {
	if err := dispute.Validate(); err != nil {
		return err
	}
	encoded, err := encodeDispute(dispute)
	if err != nil {
		return err
	}
	if err := k.disputes.Set(ctx, dispute.ID, encoded); err != nil {
		return err
	}
	if err := k.disputeBySubmission.Set(ctx, collections.Join(disputeSubmissionKey(dispute.ChallengeID, dispute.ToolID), dispute.ID)); err != nil {
		return err
	}
	return k.disputeByFiler.Set(ctx, collections.Join(dispute.FiledBy, dispute.ID))
}

func (k *Keeper) nextDisputeID(ctx context.Context) (uint64, error) {
	id, err := k.disputeSequence.Next(ctx)
	if err != nil {
		return 0, err
	}
	return id + 1, nil
}

func (k *Keeper) ensureDisputableSubmission(ctx context.Context, challengeID, toolID string) error {
	if _, err := k.GetSubmission(ctx, challengeID, toolID); err == nil {
		return nil
	} else if !errors.Is(err, ErrSubmissionNotFound) {
		return err
	}
	if _, err := k.GetRanking(ctx, challengeID, toolID); err == nil {
		return nil
	} else if !errors.Is(err, ErrRankingNotFound) {
		return err
	}
	return ErrSubmissionNotFound.Wrapf("no submission or ranking for challenge %s tool %s", challengeID, toolID)
}

func (k *Keeper) hasOpenDisputeForSubmission(ctx context.Context, challengeID, toolID string) (bool, error) {
	disputes, err := k.GetDisputesBySubmission(ctx, challengeID, toolID)
	if err != nil {
		return false, err
	}
	for _, dispute := range disputes {
		if !dispute.Status.IsTerminal() {
			return true, nil
		}
	}
	return false, nil
}

func (k *Keeper) collectIndexedDisputes(
	ctx context.Context,
	index collections.KeySet[collections.Pair[string, uint64]],
	rng collections.Ranger[collections.Pair[string, uint64]],
) ([]*types.Dispute, error) {
	var disputes []*types.Dispute
	err := index.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
		dispute, err := k.GetDispute(ctx, key.K2())
		if err != nil {
			return false, err
		}
		disputes = append(disputes, dispute)
		return false, nil
	})
	return disputes, err
}

func encodeDispute(dispute *types.Dispute) (string, error) {
	bz, err := json.Marshal(dispute)
	if err != nil {
		return "", fmt.Errorf("marshal dispute %d: %w", dispute.ID, err)
	}
	return string(bz), nil
}

func decodeDispute(encoded string) (*types.Dispute, error) {
	var dispute types.Dispute
	if err := json.Unmarshal([]byte(encoded), &dispute); err != nil {
		return nil, fmt.Errorf("unmarshal dispute: %w", err)
	}
	if err := dispute.Validate(); err != nil {
		return nil, err
	}
	return &dispute, nil
}

// disputeSubmissionKey produces a collision-free index key for the
// (challengeID, toolID) submission pair. Uses length-prefix encoding
// ("LEN:value|" per part) instead of a raw separator-byte concat to
// guarantee injectivity regardless of what bytes the IDs contain.
//
// The previous "\x1f"-separated form (still defined as
// disputeIndexSeparator above) was vulnerable to a key-collision
// where ChallengeID="legit", ToolID="tool\x1fA" and ChallengeID=
// "legit\x1ftool", ToolID="A" both produced "legit\x1ftool\x1fA".
// While not exploitable as a consensus attack (challenge-id uniqueness
// is enforced upstream and ID creation is privileged), the merge of
// independent dispute scopes was a latent data-corruption vector.
//
// Pattern matches budgetUsageKey at x/policies/keeper/enforce.go:356-364
// (also length-prefixed for the same reason). Closes lumera_ai-cmvg0.
func disputeSubmissionKey(challengeID, toolID string) string {
	c := strings.TrimSpace(challengeID)
	t := strings.TrimSpace(toolID)
	return strconv.Itoa(len(c)) + ":" + c + "|" + strconv.Itoa(len(t)) + ":" + t + "|"
}
