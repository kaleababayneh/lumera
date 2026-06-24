package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/LumeraProtocol/lumera/x/challenges/types"
)

type msgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

// NewMsgServerImpl returns an implementation of the challenges MsgServer.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

func (s *msgServer) requireKeeper() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, fmt.Errorf("challenges keeper not initialized")
	}
	return s.keeper, nil
}

func recoverChallenges(action string, err *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		default:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		}
	}
}

// CreateChallenge creates a new grand challenge with an escrowed prize pool.
func (s *msgServer) CreateChallenge(
	goCtx context.Context,
	msg *types.MsgCreateChallenge,
) (resp *types.MsgCreateChallengeResponse, err error) {
	defer recoverChallenges("CreateChallenge", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	// Validate creator address.
	if _, addrErr := sdk.AccAddressFromBech32(msg.Creator); addrErr != nil {
		return nil, fmt.Errorf("invalid creator address: %w", addrErr)
	}

	// Validate fields.
	if strings.TrimSpace(msg.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	const maxTitleLen = 256
	const maxDescriptionLen = 8192
	if len(msg.Title) > maxTitleLen {
		return nil, fmt.Errorf("title exceeds maximum length (%d bytes, max %d)", len(msg.Title), maxTitleLen)
	}
	if len(msg.Description) > maxDescriptionLen {
		return nil, fmt.Errorf("description exceeds maximum length (%d bytes, max %d)", len(msg.Description), maxDescriptionLen)
	}

	// Validate prize pool meets minimum threshold.
	params := keeper.GetParams(sdkCtx)
	if !isProtoCoinZero(msg.PrizePool) {
		prizeAmt := msg.PrizePool.Amount
		if !prizeAmt.IsNil() && prizeAmt.LT(math.NewIntFromUint64(params.MinPrizePoolLac)) {
			return nil, fmt.Errorf("%w: prize pool %s below minimum %d",
				ErrPrizeBelowMinimum, prizeAmt.String(), params.MinPrizePoolLac)
		}
	}

	// Build the challenge proto.
	ch := &types.Challenge{
		Creator:            msg.Creator,
		Title:              msg.Title,
		Description:        msg.Description,
		ChallengeType:      msg.ChallengeType,
		PrizePool:          msg.PrizePool,
		EntryFee:           msg.EntryFee,
		ScoringWeights:     msg.ScoringWeights,
		PrizeDistribution:  msg.PrizeDistribution,
		RequiredCategories: msg.RequiredCategories,
		MinBadgeTier:       msg.MinBadgeTier,
		MaxParticipants:    msg.MaxParticipants,
		StartsAt:           msg.StartsAt,
		EndsAt:             msg.EndsAt,
	}

	challengeID, createErr := keeper.CreateChallenge(sdkCtx, ch)
	if createErr != nil {
		return nil, fmt.Errorf("failed to create challenge: %w", createErr)
	}

	// Escrow prize pool if configured.
	if err := keeper.EscrowPrizePool(sdkCtx, challengeID); err != nil {
		return nil, fmt.Errorf("failed to escrow prize pool: %w", err)
	}

	return &types.MsgCreateChallengeResponse{
		ChallengeId: challengeID,
	}, nil
}

// JoinChallenge registers a tool as a participant in a challenge.
func (s *msgServer) JoinChallenge(
	goCtx context.Context,
	msg *types.MsgJoinChallenge,
) (resp *types.MsgJoinChallengeResponse, err error) {
	defer recoverChallenges("JoinChallenge", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	challengeID := msg.ChallengeId
	toolID := msg.ToolId

	// Collect entry fee from publisher if the challenge defines one.
	ch, chErr := keeper.GetChallenge(sdkCtx, challengeID)
	if chErr != nil {
		return nil, fmt.Errorf("challenge %s not found: %w", challengeID, chErr)
	}

	var entryFeePaid sdk.Coin
	if !isProtoCoinZero(ch.EntryFee) && keeper.bankKeeper != nil {
		publisherAddr, _ := sdk.AccAddressFromBech32(msg.Publisher) // already validated above
		sdkFee, feeErr := protoToSDKCoin(ch.EntryFee)
		if feeErr != nil {
			return nil, fmt.Errorf("invalid entry fee: %w", feeErr)
		}
		if err := keeper.bankKeeper.SendCoinsFromAccountToModule(
			sdkCtx, publisherAddr, types.ModuleName, sdk.NewCoins(sdkFee),
		); err != nil {
			return nil, fmt.Errorf("%w: failed to collect entry fee: %v", ErrInsufficientFee, err)
		}
		entryFeePaid = ch.EntryFee
	}

	participant := &types.Participant{
		ChallengeId:  challengeID,
		ToolId:       toolID,
		PublisherId:  msg.Publisher,
		RegisteredAt: sdkCtx.BlockTime(),
		EntryFeePaid: entryFeePaid,
	}

	if err := keeper.RegisterParticipant(sdkCtx, participant); err != nil {
		return nil, fmt.Errorf("failed to join challenge: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeChallengeJoined,
		sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
		sdk.NewAttribute(types.AttributeKeyToolID, toolID),
		sdk.NewAttribute(types.AttributeKeyPublisher, msg.Publisher),
	))

	return &types.MsgJoinChallengeResponse{}, nil
}

// SubmitResult records a scored submission for a tool in a challenge.
func (s *msgServer) SubmitResult(
	goCtx context.Context,
	msg *types.MsgSubmitResult,
) (resp *types.MsgSubmitResultResponse, err error) {
	defer recoverChallenges("SubmitResult", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	challengeID := msg.ChallengeId
	toolID := msg.ToolId

	// Verify the submitter is the tool's registered publisher.
	participant, pErr := keeper.GetParticipant(sdkCtx, challengeID, toolID)
	if pErr != nil {
		return nil, fmt.Errorf("tool %s not registered in challenge %s: %w", toolID, challengeID, pErr)
	}
	if participant.PublisherId != msg.Submitter {
		return nil, fmt.Errorf("unauthorized: submitter %s is not the publisher of tool %s", msg.Submitter, toolID)
	}

	submission := &types.Submission{
		ChallengeId:          challengeID,
		ToolId:               toolID,
		LatencyScore:         msg.LatencyScore,
		CostScore:            msg.CostScore,
		AccuracyScore:        msg.AccuracyScore,
		ReliabilityScore:     msg.ReliabilityScore,
		ConformanceScore:     msg.ConformanceScore,
		GoldenTaskResultHash: msg.GoldenTaskResultHash,
	}

	if err := keeper.RecordSubmission(sdkCtx, submission); err != nil {
		return nil, fmt.Errorf("failed to submit result: %w", err)
	}

	// Retrieve the stored submission to get the computed composite.
	stored, getErr := keeper.GetSubmission(sdkCtx, challengeID, toolID)
	var compositeScore uint32
	if getErr != nil {
		sdkCtx.Logger().With("module", "x/challenges").Error("GetSubmission failed after successful RecordSubmission",
			"challenge_id", challengeID, "tool_id", toolID, "error", getErr)
	} else if stored != nil {
		compositeScore = stored.CompositeScore
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeSubmissionRecorded,
		sdk.NewAttribute(types.AttributeKeyChallengeID, challengeID),
		sdk.NewAttribute(types.AttributeKeyToolID, toolID),
		sdk.NewAttribute(types.AttributeKeySubmitter, msg.Submitter),
		sdk.NewAttribute(types.AttributeKeyCompositeScore, fmt.Sprintf("%d", compositeScore)),
	))

	return &types.MsgSubmitResultResponse{
		CompositeScore: compositeScore,
	}, nil
}

// ActivateChallenge moves a challenge from DRAFT to ACTIVE.
func (s *msgServer) ActivateChallenge(
	goCtx context.Context,
	msg *types.MsgActivateChallenge,
) (resp *types.MsgActivateChallengeResponse, err error) {
	defer recoverChallenges("ActivateChallenge", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	challengeID := msg.ChallengeId

	if err := keeper.ActivateChallenge(sdkCtx, challengeID, msg.Creator); err != nil {
		return nil, fmt.Errorf("failed to activate challenge: %w", err)
	}

	return &types.MsgActivateChallengeResponse{}, nil
}

// CancelChallenge cancels a challenge and triggers a prize pool refund.
func (s *msgServer) CancelChallenge(
	goCtx context.Context,
	msg *types.MsgCancelChallenge,
) (resp *types.MsgCancelChallengeResponse, err error) {
	defer recoverChallenges("CancelChallenge", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	challengeID := msg.ChallengeId

	if err := keeper.CancelChallenge(sdkCtx, challengeID, msg.Creator); err != nil {
		return nil, fmt.Errorf("failed to cancel challenge: %w", err)
	}

	// Refund escrowed prize pool.
	if err := keeper.RefundPrizePool(sdkCtx, challengeID); err != nil {
		return nil, fmt.Errorf("failed to refund prize pool: %w", err)
	}

	// Refund entry fees to participants.
	if err := keeper.RefundEntryFees(sdkCtx, challengeID); err != nil {
		return nil, fmt.Errorf("failed to refund entry fees: %w", err)
	}

	return &types.MsgCancelChallengeResponse{}, nil
}

// UpdateParams updates the challenges module parameters (governance only).
func (s *msgServer) UpdateParams(
	goCtx context.Context,
	msg *types.MsgUpdateParams,
) (resp *types.MsgUpdateParamsResponse, err error) {
	defer recoverChallenges("UpdateParams", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Check authority.
	if msg.Authority != keeper.authority {
		return nil, sdkerrors.ErrUnauthorized.Wrapf(
			"invalid authority; expected %s, got %s",
			keeper.authority,
			msg.Authority,
		)
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	if err := keeper.SetParams(sdkCtx, msg.Params); err != nil {
		return nil, fmt.Errorf("failed to set params: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeParamsUpdated,
			sdk.NewAttribute(types.AttributeKeyAuthority, msg.Authority),
		),
	)

	return &types.MsgUpdateParamsResponse{}, nil
}
