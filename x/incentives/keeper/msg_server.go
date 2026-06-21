package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/LumeraProtocol/lumera/x/incentives/types"
)

type msgServer struct {
	types.UnimplementedMsgServer
	keeper Keeper
}

func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

func (m msgServer) requireKeeper() (*Keeper, error) {
	// A zero-value Keeper has no store service; NewKeeper always sets one
	// before building the collections schema.
	if m.keeper.storeService == nil {
		return nil, fmt.Errorf("incentives keeper not initialized")
	}
	return &m.keeper, nil
}

func recoverIncentives(action string, err *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		default:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		}
	}
}

func (m msgServer) RecordMetrics(ctx context.Context, msg *types.MsgRecordMetrics) (resp *types.MsgRecordMetricsResponse, err error) {
	defer recoverIncentives("incentives/RecordMetrics", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.Authority() {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.Authority(), msg.Authority)
	}

	if err := keeper.RecordMetrics(ctx, msg.Metrics); err != nil {
		return nil, err
	}

	return &types.MsgRecordMetricsResponse{}, nil
}

func (m msgServer) RequestEvaluation(ctx context.Context, msg *types.MsgRequestEvaluation) (resp *types.MsgRequestEvaluationResponse, err error) {
	defer recoverIncentives("incentives/RequestEvaluation", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if keeper.registryKeeper != nil {
		publisherAddr, err := sdk.AccAddressFromBech32(msg.Publisher)
		if err != nil {
			return nil, err
		}
		expectedPublisher, err := keeper.registryKeeper.GetToolPublisher(ctx, msg.ToolId)
		if err != nil {
			return nil, err
		}
		if expectedPublisher == nil {
			return nil, types.ErrToolNotRegistered.Wrapf("publisher address missing for tool %s", msg.ToolId)
		}
		if !expectedPublisher.Equals(publisherAddr) {
			return nil, types.ErrPublisherMismatch.Wrapf(
				"tool %s publisher %s does not match requester %s",
				msg.ToolId,
				expectedPublisher.String(),
				msg.Publisher,
			)
		}
	}

	// Fold the tool's on-chain conduct (receipts + upheld disputes) into its
	// metrics before scoring, so reputation reflects real behaviour.
	keeper.refreshUsageMetrics(ctx, msg.ToolId)

	previousTier := types.BadgeTier_BADGE_TIER_NONE
	if prior, found := keeper.GetBadge(ctx, msg.ToolId); found {
		previousTier = prior.Tier
	}

	badge, err := keeper.EvaluateTool(ctx, msg.ToolId)
	if err != nil {
		return nil, err
	}

	return &types.MsgRequestEvaluationResponse{
		PreviousTier:   previousTier,
		NewTier:        badge.Tier,
		CompositeScore: badge.CompositeScore,
	}, nil
}

func (m msgServer) UpdateTierConfig(ctx context.Context, msg *types.MsgUpdateTierConfig) (resp *types.MsgUpdateTierConfigResponse, err error) {
	defer recoverIncentives("incentives/UpdateTierConfig", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.Authority() {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.Authority(), msg.Authority)
	}

	if err := keeper.SetTierConfig(ctx, msg.Config); err != nil {
		return nil, err
	}

	return &types.MsgUpdateTierConfigResponse{}, nil
}

func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (resp *types.MsgUpdateParamsResponse, err error) {
	defer recoverIncentives("incentives/UpdateParams", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.Authority() {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.Authority(), msg.Authority)
	}

	if err := keeper.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

func (m msgServer) RevokeBadge(ctx context.Context, msg *types.MsgRevokeBadge) (resp *types.MsgRevokeBadgeResponse, err error) {
	defer recoverIncentives("incentives/RevokeBadge", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.Authority() {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.Authority(), msg.Authority)
	}

	// RevokeBadge returns the badge as it existed before deletion, so its
	// tier is the previous tier the response advertises.
	badge, err := keeper.RevokeBadge(ctx, msg.ToolId, msg.Reason)
	if err != nil {
		return nil, err
	}

	return &types.MsgRevokeBadgeResponse{PreviousTier: badge.Tier}, nil
}
