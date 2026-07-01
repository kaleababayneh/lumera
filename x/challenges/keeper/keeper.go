// Package keeper manages state transitions and accounting for x/challenges.
package keeper

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

// Module sentinel errors — aliases to types package for keeper-internal use.
var (
	ErrChallengeNotFound   = types.ErrChallengeNotFound
	ErrInvalidStatus       = types.ErrInvalidStatus
	ErrInvalidTransition   = types.ErrInvalidTransition
	ErrParticipantExists   = types.ErrParticipantExists
	ErrParticipantNotFound = types.ErrParticipantNotFound
	ErrChallengeFull       = types.ErrChallengeFull
	ErrNotCreator          = types.ErrNotCreator
	ErrSubmissionNotFound  = types.ErrSubmissionNotFound
	ErrRankingNotFound     = types.ErrRankingNotFound
	ErrNilChallenge        = types.ErrNilChallenge
	ErrMissingChallengeID  = types.ErrMissingChallengeID
	ErrChallengeNotActive  = types.ErrChallengeNotActive
	ErrPrizeBelowMinimum   = types.ErrPrizeBelowMinimum
	ErrInsufficientFee     = types.ErrInsufficientFee
	ErrChallengeNotStarted = types.ErrChallengeNotStarted
	ErrBadgeTierTooLow     = types.ErrBadgeTierTooLow
	ErrMissingCategory     = types.ErrMissingCategory
)

// Keeper maintains the challenges module state.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService corestore.KVStoreService
	logger       log.Logger

	authority      string
	bankKeeper     types.BankKeeper
	accountKeeper  types.AccountKeeper
	registryKeeper types.RegistryKeeper
	lumeraIDKeeper types.LumeraIDKeeper

	schema              collections.Schema
	params              collections.Item[*types.Params]
	challenges          collections.Map[string, *types.Challenge]
	participants        collections.Map[collections.Pair[string, string], *types.Participant]
	submissions         collections.Map[collections.Pair[string, string], *types.Submission]
	rankings            collections.Map[collections.Pair[string, string], *types.Ranking]
	sequence            collections.Sequence
	disputeSequence     collections.Sequence
	disputes            collections.Map[uint64, string]
	disputeBySubmission collections.KeySet[collections.Pair[string, uint64]]
	disputeByFiler      collections.KeySet[collections.Pair[string, uint64]]
	statusIndex         collections.KeySet[collections.Pair[int32, string]]
	creatorIndex        collections.KeySet[collections.Pair[string, string]]
	toolIndex           collections.KeySet[collections.Pair[string, string]] // toolId -> challengeId
	scoringEnteredAt    collections.Map[string, int64]                       // challengeId -> block height when entered SCORING
	protocolLifecycle   collections.Map[string, string]                      // challengeId -> canonical protocol lifecycle record
}

// Logger returns a module-specific logger.
func (k *Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// NewKeeper creates a new challenges keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	logger log.Logger,
) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	k := &Keeper{
		cdc:          cdc,
		storeService: storeService,
		logger:       logger,
		params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsPrefix),
			"params",
			collPtrValue[types.Params](cdc),
		),
		challenges: collections.NewMap(
			sb,
			collections.NewPrefix(types.ChallengePrefix),
			"challenges",
			collections.StringKey,
			collPtrValue[types.Challenge](cdc),
		),
		participants: collections.NewMap(
			sb,
			collections.NewPrefix(types.ParticipantPrefix),
			"participants",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			collPtrValue[types.Participant](cdc),
		),
		submissions: collections.NewMap(
			sb,
			collections.NewPrefix(types.SubmissionPrefix),
			"submissions",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			collPtrValue[types.Submission](cdc),
		),
		rankings: collections.NewMap(
			sb,
			collections.NewPrefix(types.RankingPrefix),
			"rankings",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			collPtrValue[types.Ranking](cdc),
		),
		sequence: collections.NewSequence(
			sb,
			collections.NewPrefix(types.SequencePrefix),
			"challenge_sequence",
		),
		disputeSequence: collections.NewSequence(
			sb,
			collections.NewPrefix(types.DisputeSequencePrefix),
			"dispute_sequence",
		),
		disputes: collections.NewMap(
			sb,
			collections.NewPrefix(types.DisputePrefix),
			"disputes",
			collections.Uint64Key,
			collections.StringValue,
		),
		disputeBySubmission: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.DisputeSubmissionIndexPrefix),
			"dispute_by_submission",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		disputeByFiler: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.DisputeFilerIndexPrefix),
			"dispute_by_filer",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		statusIndex: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.StatusIndexPrefix),
			"status_index",
			collections.PairKeyCodec(collections.Int32Key, collections.StringKey),
		),
		creatorIndex: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.CreatorIndexPrefix),
			"creator_index",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		toolIndex: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.ToolIndexPrefix),
			"tool_index",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		scoringEnteredAt: collections.NewMap(
			sb,
			collections.NewPrefix(types.ScoringEnteredAtPrefix),
			"scoring_entered_at",
			collections.StringKey,
			collections.Int64Value,
		),
		protocolLifecycle: collections.NewMap(
			sb,
			collections.NewPrefix(protocolLifecyclePrefix),
			"protocol_lifecycle",
			collections.StringKey,
			collections.StringValue,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build challenges schema: %w", err))
	}
	k.schema = schema
	return k
}

// SetBankKeeper sets the bank keeper dependency. This is called during app
// wiring to inject the bank keeper after both keepers are constructed (avoids
// circular dependency during depinject).
func (k *Keeper) SetBankKeeper(bk types.BankKeeper) {
	k.bankKeeper = bk
}

// SetAccountKeeper sets the account keeper dependency.
func (k *Keeper) SetAccountKeeper(ak types.AccountKeeper) {
	k.accountKeeper = ak
}

// SetRegistryKeeper sets the registry keeper dependency.
func (k *Keeper) SetRegistryKeeper(rk types.RegistryKeeper) {
	k.registryKeeper = rk
}

// SetLumeraIDKeeper sets the LumeraID keeper dependency.
func (k *Keeper) SetLumeraIDKeeper(lk types.LumeraIDKeeper) {
	k.lumeraIDKeeper = lk
}

// SetAuthority sets the module governance authority address.
func (k *Keeper) SetAuthority(authority string) {
	k.authority = authority
}

// Authority returns the module governance authority address.
func (k *Keeper) Authority() string {
	return k.authority
}

// BankKeeper returns the bank keeper dependency.
func (k *Keeper) BankKeeper() types.BankKeeper { return k.bankKeeper }

// AccountKeeper returns the account keeper dependency.
func (k *Keeper) AccountKeeper() types.AccountKeeper { return k.accountKeeper }

// LumeraIDKeeper returns the LumeraID keeper dependency.
func (k *Keeper) LumeraIDKeeper() types.LumeraIDKeeper { return k.lumeraIDKeeper }

// ---------------------------------------------------------------------------
// Params
// ---------------------------------------------------------------------------

// GetParams returns the module parameters.
func (k *Keeper) GetParams(ctx context.Context) *types.Params {
	p, err := k.params.Get(ctx)
	if err != nil || p == nil {
		return DefaultParams()
	}
	return p
}

// SetParams stores the module parameters.
func (k *Keeper) SetParams(ctx context.Context, p *types.Params) error {
	if p == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := p.Validate(); err != nil {
		return err
	}
	return k.params.Set(ctx, p)
}

// DefaultParams returns reasonable defaults for the challenges module.
func DefaultParams() *types.Params {
	return &types.Params{
		MinPrizePoolLac:       1_000_000, // 1 LAC
		MaxDurationBlocks:     100_000,
		MinParticipants:       2,
		EntryFeePercentageBps: 100, // 1%
		PlatformFeeBps:        500, // 5%
		ScoringDelayBlocks:    10,
	}
}

// ---------------------------------------------------------------------------
// Challenge CRUD
// ---------------------------------------------------------------------------

// CreateChallenge persists a new challenge, assigns a deterministic ID, and
// updates secondary indexes.
func (k *Keeper) CreateChallenge(ctx context.Context, ch *types.Challenge) (string, error) {
	if ch == nil {
		return "", ErrNilChallenge
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Assign ID from sequence if not provided.
	if ch.ChallengeId == "" {
		seq, err := k.sequence.Next(ctx)
		if err != nil {
			return "", fmt.Errorf("sequence next: %w", err)
		}
		ch.ChallengeId = fmt.Sprintf("challenge-%d", seq)
	}

	if ch.Status == types.ChallengeStatus_CHALLENGE_STATUS_UNSPECIFIED {
		ch.Status = types.ChallengeStatus_CHALLENGE_STATUS_DRAFT
	}
	if ch.CreatedAt.IsZero() {
		ch.CreatedAt = sdkCtx.BlockTime()
	}

	// Apply default scoring weights based on challenge type when not explicitly set.
	if ch.ScoringWeights == nil {
		ch.ScoringWeights = defaultWeightsForType(ch.ChallengeType)
	}

	if err := k.challenges.Set(ctx, ch.ChallengeId, ch); err != nil {
		return "", fmt.Errorf("store challenge: %w", err)
	}

	// Secondary indexes.
	if err := k.statusIndex.Set(ctx, collections.Join(int32(ch.Status), ch.ChallengeId)); err != nil {
		return "", err
	}
	if ch.Creator != "" {
		if err := k.creatorIndex.Set(ctx, collections.Join(ch.Creator, ch.ChallengeId)); err != nil {
			return "", err
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeChallengeCreated,
		sdk.NewAttribute(types.AttributeKeyChallengeID, ch.ChallengeId),
		sdk.NewAttribute(types.AttributeKeyCreator, ch.Creator),
		sdk.NewAttribute(types.AttributeKeyChallengeClass, challengeClassLabel(ch.ChallengeType)),
		sdk.NewAttribute(types.AttributeKeyBlockHeight, strconv.FormatInt(sdkCtx.BlockHeight(), 10)),
		sdk.NewAttribute(types.AttributeKeyNewStatus, ch.Status.String()),
	))
	k.observeChallengeIssued(ch)

	return ch.ChallengeId, nil
}

// GetChallenge retrieves a challenge by ID.
func (k *Keeper) GetChallenge(ctx context.Context, id string) (*types.Challenge, error) {
	ch, err := k.challenges.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, ErrChallengeNotFound
		}
		return nil, err
	}
	return ch, nil
}

// UpdateChallenge replaces a challenge in state and re-indexes.
func (k *Keeper) UpdateChallenge(ctx context.Context, ch *types.Challenge) error {
	if ch == nil {
		return ErrNilChallenge
	}
	if ch.ChallengeId == "" {
		return ErrMissingChallengeID
	}

	// Fetch old to clean stale index entries.
	old, err := k.challenges.Get(ctx, ch.ChallengeId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return ErrChallengeNotFound
		}
		return err
	}

	// Remove old status index.
	if err := k.statusIndex.Remove(ctx, collections.Join(int32(old.Status), old.ChallengeId)); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("remove old status index for %s: %w", old.ChallengeId, err)
	}

	if err := k.challenges.Set(ctx, ch.ChallengeId, ch); err != nil {
		return err
	}

	// Re-insert new status index.
	return k.statusIndex.Set(ctx, collections.Join(int32(ch.Status), ch.ChallengeId))
}

// DeleteChallenge removes a challenge and its indexes.
func (k *Keeper) DeleteChallenge(ctx context.Context, id string) error {
	ch, err := k.challenges.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil // idempotent
		}
		return err
	}

	if err := k.statusIndex.Remove(ctx, collections.Join(int32(ch.Status), ch.ChallengeId)); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("remove status index for %s: %w", ch.ChallengeId, err)
	}
	if ch.Creator != "" {
		if err := k.creatorIndex.Remove(ctx, collections.Join(ch.Creator, ch.ChallengeId)); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("remove creator index for %s: %w", ch.ChallengeId, err)
		}
	}
	return k.challenges.Remove(ctx, id)
}

// GetAllChallenges returns every challenge in state. No result cap is applied
// because ExportGenesis relies on this for a complete state export.
func (k *Keeper) GetAllChallenges(ctx context.Context) ([]*types.Challenge, error) {
	var out []*types.Challenge
	err := k.challenges.Walk(ctx, nil, func(_ string, ch *types.Challenge) (bool, error) {
		out = append(out, ch)
		return false, nil
	})
	return out, err
}

// GetChallengesByStatus returns challenges matching a given status.
// No result cap is applied because EndBlocker and the query layer both rely
// on scanning the full status bucket before they apply their own logic.
func (k *Keeper) GetChallengesByStatus(ctx context.Context, status types.ChallengeStatus) ([]*types.Challenge, error) {
	var out []*types.Challenge
	rng := collections.NewPrefixedPairRange[int32, string](int32(status))
	err := k.statusIndex.Walk(ctx, rng, func(key collections.Pair[int32, string]) (bool, error) {
		ch, getErr := k.challenges.Get(ctx, key.K2())
		if getErr != nil {
			k.Logger(sdk.UnwrapSDKContext(ctx)).Error("indexed challenge not found", "id", key.K2(), "error", getErr)
		} else if ch != nil {
			out = append(out, ch)
		}
		return false, nil
	})
	return out, err
}

// GetChallengesByCreator returns challenges created by a given address.
func (k *Keeper) GetChallengesByCreator(ctx context.Context, creator string) ([]*types.Challenge, error) {
	const maxResults = 1000
	var out []*types.Challenge
	rng := collections.NewPrefixedPairRange[string, string](creator)
	err := k.creatorIndex.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		ch, getErr := k.challenges.Get(ctx, key.K2())
		if getErr != nil {
			k.Logger(sdk.UnwrapSDKContext(ctx)).Error("indexed challenge not found", "id", key.K2(), "error", getErr)
		} else if ch != nil {
			out = append(out, ch)
		}
		return len(out) >= maxResults, nil
	})
	return out, err
}

// GetChallengeIDsByTool returns challenge IDs where a tool is a participant,
// using the toolIndex for O(k) lookup instead of a full participant table scan.
func (k *Keeper) GetChallengeIDsByTool(ctx context.Context, toolID string) ([]string, error) {
	const maxResults = 1000
	var out []string
	rng := collections.NewPrefixedPairRange[string, string](toolID)
	err := k.toolIndex.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		out = append(out, key.K2())
		return len(out) >= maxResults, nil
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Status transitions
// ---------------------------------------------------------------------------

// validTransitions maps each status to its allowed successors.
var validTransitions = map[types.ChallengeStatus][]types.ChallengeStatus{
	types.ChallengeStatus_CHALLENGE_STATUS_DRAFT: {
		types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE,
		types.ChallengeStatus_CHALLENGE_STATUS_CANCELLED,
	},
	types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE: {
		types.ChallengeStatus_CHALLENGE_STATUS_SCORING,
		types.ChallengeStatus_CHALLENGE_STATUS_CANCELLED,
	},
	// SCORING is a one-way funnel: once participants' submissions have been
	// collected and the challenge is being ranked, the only valid exit is
	// COMPLETED. Cancelling from SCORING would let the creator rug-pull the
	// prize pool after observing preliminary rankings. CancelChallenge
	// already rejects this at the user-facing layer; removing the transition
	// here closes the same hole for any future caller that goes through
	// TransitionStatus directly.
	types.ChallengeStatus_CHALLENGE_STATUS_SCORING: {
		types.ChallengeStatus_CHALLENGE_STATUS_COMPLETED,
	},
}

// TransitionStatus moves a challenge to a new status, validating the transition.
func (k *Keeper) TransitionStatus(ctx context.Context, challengeID string, newStatus types.ChallengeStatus) error {
	ch, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}

	fromStatus := ch.Status
	allowed, ok := validTransitions[ch.Status]
	if !ok {
		return fmt.Errorf("%w: no transitions from %s", ErrInvalidTransition, ch.Status)
	}
	valid := false
	for _, s := range allowed {
		if s == newStatus {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, ch.Status, newStatus)
	}

	ch.Status = newStatus

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	switch newStatus {
	case types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE:
		if ch.StartsAt.IsZero() {
			ch.StartsAt = now
		}
	case types.ChallengeStatus_CHALLENGE_STATUS_SCORING:
		if ch.EndsAt.IsZero() {
			ch.EndsAt = now
		}
	case types.ChallengeStatus_CHALLENGE_STATUS_COMPLETED:
		ch.ScoredAt = now
	}

	if err := k.UpdateChallenge(ctx, ch); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeChallengeStatusChanged,
		challengeLifecycleAttributes(sdkCtx, ch, fromStatus, newStatus)...,
	))
	return nil
}

// ActivateChallenge moves a draft challenge to active.
func (k *Keeper) ActivateChallenge(ctx context.Context, challengeID, caller string) error {
	ch, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}
	if ch.Creator != caller {
		return ErrNotCreator
	}

	params := k.GetParams(ctx)
	if ch.ParticipantCount < params.MinParticipants {
		return fmt.Errorf("need at least %d participants, have %d", params.MinParticipants, ch.ParticipantCount)
	}

	return k.TransitionStatus(ctx, challengeID, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE)
}

// CancelChallenge moves a challenge to cancelled (from draft, active, or scoring).
func (k *Keeper) CancelChallenge(ctx context.Context, challengeID, caller string) error {
	ch, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}
	if ch.Creator != caller {
		return ErrNotCreator
	}
	// Creator-initiated cancellation is only allowed before scoring begins.
	// Once a challenge enters SCORING, submissions have been collected and
	// participants are owed payouts; allowing the creator to cancel here
	// would let them rug-pull the prize pool.
	if ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_DRAFT &&
		ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE {
		return fmt.Errorf("%w: creator cannot cancel from %s", ErrInvalidTransition, ch.Status)
	}
	return k.TransitionStatus(ctx, challengeID, types.ChallengeStatus_CHALLENGE_STATUS_CANCELLED)
}

// ---------------------------------------------------------------------------
// Participants
// ---------------------------------------------------------------------------

// RegisterParticipant adds a tool to a challenge.
func (k *Keeper) RegisterParticipant(ctx context.Context, p *types.Participant) error {
	if p == nil || p.ChallengeId == "" || p.ToolId == "" {
		return errors.New("participant requires challenge_id and tool_id")
	}

	ch, err := k.GetChallenge(ctx, p.ChallengeId)
	if err != nil {
		return err
	}

	if ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_DRAFT {
		return fmt.Errorf("%w: can only register in DRAFT status", ErrChallengeNotActive)
	}

	if ch.MaxParticipants > 0 && ch.ParticipantCount >= ch.MaxParticipants {
		return ErrChallengeFull
	}

	// Validate tool eligibility against challenge requirements.
	if k.registryKeeper != nil && len(ch.RequiredCategories) > 0 {
		toolCats, found := k.registryKeeper.GetToolCategories(ctx, p.ToolId)
		if !found {
			return fmt.Errorf("%w: tool %s not found in registry", ErrMissingCategory, p.ToolId)
		}
		catSet := make(map[string]struct{}, len(toolCats))
		for _, c := range toolCats {
			catSet[c] = struct{}{}
		}
		for _, req := range ch.RequiredCategories {
			if _, ok := catSet[req]; !ok {
				return fmt.Errorf("%w: tool %s lacks required category %q", ErrMissingCategory, p.ToolId, req)
			}
		}
	}

	key := collections.Join(p.ChallengeId, p.ToolId)
	has, err := k.participants.Has(ctx, key)
	if err != nil {
		return err
	}
	if has {
		return ErrParticipantExists
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if p.RegisteredAt.IsZero() {
		p.RegisteredAt = sdkCtx.BlockTime()
	}

	if err := k.participants.Set(ctx, key, p); err != nil {
		return err
	}

	// Secondary index: toolId -> challengeId for efficient ToolChallenges queries.
	if err := k.toolIndex.Set(ctx, collections.Join(p.ToolId, p.ChallengeId)); err != nil {
		return err
	}

	// Bump participant count.
	ch.ParticipantCount++
	return k.UpdateChallenge(ctx, ch)
}

// GetParticipant retrieves a participant by challenge and tool IDs.
func (k *Keeper) GetParticipant(ctx context.Context, challengeID, toolID string) (*types.Participant, error) {
	p, err := k.participants.Get(ctx, collections.Join(challengeID, toolID))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, ErrParticipantNotFound
		}
		return nil, err
	}
	return p, nil
}

// GetParticipants returns all participants for a challenge.
func (k *Keeper) GetParticipants(ctx context.Context, challengeID string) ([]*types.Participant, error) {
	var out []*types.Participant
	rng := collections.NewPrefixedPairRange[string, string](challengeID)
	err := k.participants.Walk(ctx, rng, func(_ collections.Pair[string, string], p *types.Participant) (bool, error) {
		out = append(out, p)
		return false, nil
	})
	return out, err
}

// DisqualifyParticipant marks a participant as disqualified. Only the
// challenge creator or the module governance authority may disqualify a
// participant; without this check any account could remove competitors
// from a prize pool or manipulate scoring outcomes.
func (k *Keeper) DisqualifyParticipant(ctx context.Context, challengeID, toolID, caller, reason string) error {
	p, err := k.GetParticipant(ctx, challengeID, toolID)
	if err != nil {
		return err
	}
	ch, err := k.GetChallenge(ctx, challengeID)
	if err != nil {
		return err
	}
	if caller != ch.Creator && caller != k.authority {
		return types.ErrUnauthorized.Wrapf("caller %s is neither challenge creator nor module authority", caller)
	}
	p.Disqualified = true
	p.DisqualificationReason = reason
	return k.participants.Set(ctx, collections.Join(challengeID, toolID), p)
}

// ---------------------------------------------------------------------------
// Submissions
// ---------------------------------------------------------------------------

// RecordSubmission stores a scoring submission for a tool in a challenge.
func (k *Keeper) RecordSubmission(ctx context.Context, s *types.Submission) error {
	if s == nil || s.ChallengeId == "" || s.ToolId == "" {
		return errors.New("submission requires challenge_id and tool_id")
	}

	ch, err := k.GetChallenge(ctx, s.ChallengeId)
	if err != nil {
		return err
	}
	// Only accept submissions in ACTIVE status. Rejecting submissions during
	// SCORING status prevents a race condition where participants could observe
	// preliminary rankings and submit last-minute results to manipulate outcomes.
	if ch.Status != types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE {
		return fmt.Errorf("%w: submissions only accepted in ACTIVE status", ErrChallengeNotActive)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Reject submissions before the challenge's start time.
	if !ch.StartsAt.IsZero() && sdkCtx.BlockTime().Before(ch.StartsAt) {
		return fmt.Errorf("%w: challenge starts at %s", ErrChallengeNotStarted, ch.StartsAt)
	}

	// Reject submissions after the challenge's end time as a secondary guard.
	// The status check above should catch this, but this provides defense-in-depth.
	if !ch.EndsAt.IsZero() && !sdkCtx.BlockTime().Before(ch.EndsAt) {
		return fmt.Errorf("%w: challenge ended at %s", ErrChallengeNotActive, ch.EndsAt)
	}

	// Verify participant is registered and not disqualified.
	p, err := k.GetParticipant(ctx, s.ChallengeId, s.ToolId)
	if err != nil {
		return fmt.Errorf("tool %s not registered: %w", s.ToolId, err)
	}
	if p.Disqualified {
		return fmt.Errorf("tool %s is disqualified", s.ToolId)
	}

	if s.SubmittedAt.IsZero() {
		s.SubmittedAt = sdkCtx.BlockTime()
	}
	if s.BlockHeight == 0 {
		s.BlockHeight = sdkCtx.BlockHeight()
	}

	// Compute composite score if weights are available.
	if s.CompositeScore == 0 && ch.ScoringWeights != nil {
		s.CompositeScore = computeComposite(s, ch.ScoringWeights)
	}

	if err := k.submissions.Set(ctx, collections.Join(s.ChallengeId, s.ToolId), s); err != nil {
		return err
	}
	k.observeChallengeResponded(ch)
	return nil
}

// GetSubmission retrieves a submission by challenge and tool IDs.
func (k *Keeper) GetSubmission(ctx context.Context, challengeID, toolID string) (*types.Submission, error) {
	s, err := k.submissions.Get(ctx, collections.Join(challengeID, toolID))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, ErrSubmissionNotFound
		}
		return nil, err
	}
	return s, nil
}

// GetSubmissions returns all submissions for a challenge.
func (k *Keeper) GetSubmissions(ctx context.Context, challengeID string) ([]*types.Submission, error) {
	var out []*types.Submission
	rng := collections.NewPrefixedPairRange[string, string](challengeID)
	err := k.submissions.Walk(ctx, rng, func(_ collections.Pair[string, string], s *types.Submission) (bool, error) {
		out = append(out, s)
		return false, nil
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Rankings
// ---------------------------------------------------------------------------

// SetRanking stores a ranking entry.
func (k *Keeper) SetRanking(ctx context.Context, r *types.Ranking) error {
	if r == nil || r.ChallengeId == "" || r.ToolId == "" {
		return errors.New("ranking requires challenge_id and tool_id")
	}
	return k.rankings.Set(ctx, collections.Join(r.ChallengeId, r.ToolId), r)
}

// GetRanking retrieves a ranking by challenge and tool IDs.
func (k *Keeper) GetRanking(ctx context.Context, challengeID, toolID string) (*types.Ranking, error) {
	r, err := k.rankings.Get(ctx, collections.Join(challengeID, toolID))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, ErrRankingNotFound
		}
		return nil, err
	}
	return r, nil
}

// GetRankings returns all rankings for a challenge, ordered by storage key.
func (k *Keeper) GetRankings(ctx context.Context, challengeID string) ([]*types.Ranking, error) {
	var out []*types.Ranking
	rng := collections.NewPrefixedPairRange[string, string](challengeID)
	err := k.rankings.Walk(ctx, rng, func(_ collections.Pair[string, string], r *types.Ranking) (bool, error) {
		out = append(out, r)
		return false, nil
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Genesis
// ---------------------------------------------------------------------------

// InitGenesis initializes module state from genesis.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	if gs.Params != nil {
		if err := k.SetParams(ctx, gs.Params); err != nil {
			return err
		}
	}
	for _, ch := range gs.Challenges {
		if err := k.challenges.Set(ctx, ch.ChallengeId, ch); err != nil {
			return err
		}
		if err := k.statusIndex.Set(ctx, collections.Join(int32(ch.Status), ch.ChallengeId)); err != nil {
			return err
		}
		if ch.Creator != "" {
			if err := k.creatorIndex.Set(ctx, collections.Join(ch.Creator, ch.ChallengeId)); err != nil {
				return err
			}
		}
	}
	for _, p := range gs.Participants {
		if err := k.participants.Set(ctx, collections.Join(p.ChallengeId, p.ToolId), p); err != nil {
			return err
		}
		if err := k.toolIndex.Set(ctx, collections.Join(p.ToolId, p.ChallengeId)); err != nil {
			return err
		}
	}
	for _, s := range gs.Submissions {
		if err := k.submissions.Set(ctx, collections.Join(s.ChallengeId, s.ToolId), s); err != nil {
			return err
		}
	}
	for _, r := range gs.Rankings {
		if err := k.rankings.Set(ctx, collections.Join(r.ChallengeId, r.ToolId), r); err != nil {
			return err
		}
	}

	// Restore sequence counter by finding the maximum numeric ID suffix among
	// imported challenges. Using len(gs.Challenges) would be wrong when IDs
	// are non-sequential (e.g. after deletions or external import).
	var maxSeq uint64
	for _, ch := range gs.Challenges {
		if after, ok := strings.CutPrefix(ch.ChallengeId, "challenge-"); ok {
			if n, err := strconv.ParseUint(after, 10, 64); err == nil && n >= maxSeq {
				maxSeq = n + 1
			}
		}
	}
	if maxSeq > 0 {
		if err := k.sequence.Set(ctx, maxSeq); err != nil {
			return fmt.Errorf("failed to restore challenge sequence: %w", err)
		}
	}

	return nil
}

// ExportGenesis exports all module state.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params := k.GetParams(ctx)
	challenges, err := k.GetAllChallenges(ctx)
	if err != nil {
		return nil, err
	}

	var allParticipants []*types.Participant
	if err := k.participants.Walk(ctx, nil, func(_ collections.Pair[string, string], p *types.Participant) (bool, error) {
		allParticipants = append(allParticipants, p)
		return false, nil
	}); err != nil {
		return nil, err
	}

	var allSubmissions []*types.Submission
	if err := k.submissions.Walk(ctx, nil, func(_ collections.Pair[string, string], s *types.Submission) (bool, error) {
		allSubmissions = append(allSubmissions, s)
		return false, nil
	}); err != nil {
		return nil, err
	}

	var allRankings []*types.Ranking
	if err := k.rankings.Walk(ctx, nil, func(_ collections.Pair[string, string], r *types.Ranking) (bool, error) {
		allRankings = append(allRankings, r)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return &types.GenesisState{
		Params:       params,
		Challenges:   challenges,
		Participants: allParticipants,
		Submissions:  allSubmissions,
		Rankings:     allRankings,
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// defaultWeightsForType returns sensible default scoring weights based on
// the challenge type. All weights sum to 10000 BPS (100%).
func defaultWeightsForType(ct types.ChallengeType) *types.ScoringWeight {
	switch ct {
	case types.ChallengeType_CHALLENGE_TYPE_PERFORMANCE:
		return &types.ScoringWeight{
			LatencyWeightBps: 4000, CostWeightBps: 3000,
			AccuracyWeightBps: 1000, ReliabilityWeightBps: 1000, ConformanceWeightBps: 1000,
		}
	case types.ChallengeType_CHALLENGE_TYPE_QUALITY:
		return &types.ScoringWeight{
			LatencyWeightBps: 1000, CostWeightBps: 1000,
			AccuracyWeightBps: 4000, ReliabilityWeightBps: 3000, ConformanceWeightBps: 1000,
		}
	case types.ChallengeType_CHALLENGE_TYPE_CONFORMANCE:
		return &types.ScoringWeight{
			LatencyWeightBps: 1000, CostWeightBps: 1000,
			AccuracyWeightBps: 1000, ReliabilityWeightBps: 1000, ConformanceWeightBps: 6000,
		}
	case types.ChallengeType_CHALLENGE_TYPE_IDENTITY_ATTESTATION,
		types.ChallengeType_CHALLENGE_TYPE_SLO_PROBE,
		types.ChallengeType_CHALLENGE_TYPE_TEE_REPORT,
		types.ChallengeType_CHALLENGE_TYPE_RECEIPT_REPLAY:
		return nil
	default: // COMPOSITE or UNSPECIFIED — equal weights.
		return &types.ScoringWeight{
			LatencyWeightBps: 2000, CostWeightBps: 2000,
			AccuracyWeightBps: 2000, ReliabilityWeightBps: 2000, ConformanceWeightBps: 2000,
		}
	}
}

func computeComposite(s *types.Submission, w *types.ScoringWeight) uint32 {
	totalW := uint64(w.LatencyWeightBps) + uint64(w.CostWeightBps) +
		uint64(w.AccuracyWeightBps) + uint64(w.ReliabilityWeightBps) +
		uint64(w.ConformanceWeightBps)
	if totalW == 0 {
		return 0
	}
	weighted := uint64(s.LatencyScore)*uint64(w.LatencyWeightBps) +
		uint64(s.CostScore)*uint64(w.CostWeightBps) +
		uint64(s.AccuracyScore)*uint64(w.AccuracyWeightBps) +
		uint64(s.ReliabilityScore)*uint64(w.ReliabilityWeightBps) +
		uint64(s.ConformanceScore)*uint64(w.ConformanceWeightBps)
	return uint32(weighted / totalW)
}
