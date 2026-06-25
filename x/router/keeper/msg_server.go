package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	"github.com/LumeraProtocol/lumera/x/router/types"
)

type msgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

// NewMsgServerImpl returns an implementation of the router Msg service.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

func (m *msgServer) requireKeeper() (*Keeper, error) {
	if m == nil || m.keeper == nil {
		return nil, fmt.Errorf("router keeper not initialized")
	}
	return m.keeper, nil
}

func recoverRouter(action string, err *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		default:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		}
	}
}

type msgServerValidatable interface {
	ValidateBasic() error
}

func validateMsgServerBasic(msg msgServerValidatable) error {
	if err := msg.ValidateBasic(); err != nil {
		return errorsmod.Wrap(types.ErrInvalidParams, err.Error())
	}
	return nil
}

func (m *msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (resp *types.MsgUpdateParamsResponse, err error) {
	defer recoverRouter("router/UpdateParams", &err)
	if msg == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, "message is required")
	}
	if err := validateMsgServerBasic(msg); err != nil {
		return nil, err
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.authority {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "invalid authority; expected %s, got %s", keeper.authority, msg.Authority)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	return &types.MsgUpdateParamsResponse{}, keeper.UpdateParams(ctx, msg.Authority, &msg.Params)
}

func (m *msgServer) RecordActivation(goCtx context.Context, msg *types.MsgRecordActivation) (resp *types.MsgRecordActivationResponse, err error) {
	defer recoverRouter("router/RecordActivation", &err)
	if msg == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, "message is required")
	}
	if err := validateMsgServerBasic(msg); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Verify tool exists and retrieve owner for authorization
	tool, found := keeper.registryKeeper.GetToolCard(ctx, msg.ToolId)
	if !found || tool == nil {
		return nil, types.ErrToolNotFound
	}

	// Only the module authority or the tool owner can record activations
	if msg.Authority != keeper.authority && msg.Authority != tool.Owner {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "only module authority or tool owner can record activations; got %s", msg.Authority)
	}

	if err := keeper.RecordActivation(
		ctx,
		msg.ToolId,
		msg.SessionId,
		msg.Activated,
		msg.Reason,
	); err != nil {
		return nil, err
	}

	return &types.MsgRecordActivationResponse{Success: true}, nil
}

func (m *msgServer) RecordInvocation(goCtx context.Context, msg *types.MsgRecordInvocation) (resp *types.MsgRecordInvocationResponse, err error) {
	defer recoverRouter("router/RecordInvocation", &err)
	if msg == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, "message is required")
	}
	if err := validateMsgServerBasic(msg); err != nil {
		return nil, err
	}

	cost, err := decimal.NewFromString(msg.Cost)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidParams, "invalid cost: %v", err)
	}
	if !moneyguard.IsSafeExponent(cost) {
		return nil, errorsmod.Wrapf(types.ErrInvalidParams, "cost magnitude out of safe range")
	}
	if cost.IsNegative() {
		return nil, errorsmod.Wrapf(types.ErrInvalidParams, "cost cannot be negative: %s", cost)
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	// Verify authority matches module authority
	if msg.Authority != keeper.authority {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "only module authority can record invocations; expected %s, got %s", keeper.authority, msg.Authority)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := keeper.RecordInvocation(
		ctx,
		msg.ToolId,
		msg.SessionId,
		msg.UserAddress,
		cost,
		msg.LatencyMs,
		msg.Success,
		msg.CacheHit,
		msg.OriginToolId,
	); err != nil {
		return nil, err
	}

	return &types.MsgRecordInvocationResponse{Recorded: true}, nil
}

func (m *msgServer) RecordPolicyUpdate(goCtx context.Context, msg *types.MsgRecordPolicyUpdate) (resp *types.MsgRecordPolicyUpdateResponse, err error) {
	defer recoverRouter("router/RecordPolicyUpdate", &err)
	if msg == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, "message is required")
	}
	if err := validateMsgServerBasic(msg); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.authority {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "only module authority can record policy updates; expected %s, got %s", keeper.authority, msg.Authority)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := keeper.RecordPolicyUpdate(
		ctx,
		msg.PolicyId,
		msg.NewVersion,
		msg.PreviousVersion,
		msg.Changes,
		msg.Authority, // Signer
		msg.Reason,
	); err != nil {
		return nil, err
	}

	return &types.MsgRecordPolicyUpdateResponse{Success: true}, nil
}

func (m *msgServer) RecordCACHit(goCtx context.Context, msg *types.MsgRecordCACHit) (resp *types.MsgRecordCACHitResponse, err error) {
	defer recoverRouter("router/RecordCACHit", &err)
	if msg == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, "message is required")
	}
	if err := validateMsgServerBasic(msg); err != nil {
		return nil, err
	}

	royalty, err := decimal.NewFromString(msg.RoyaltyAmount)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrInvalidParams, "invalid royalty amount: %v", err)
	}
	if !moneyguard.IsSafeExponent(royalty) {
		return nil, errorsmod.Wrapf(types.ErrInvalidParams, "royalty amount magnitude out of safe range")
	}
	if royalty.IsNegative() {
		return nil, errorsmod.Wrapf(types.ErrInvalidParams, "royalty amount cannot be negative: %s", royalty)
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.authority {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "only module authority can record CAC hits; expected %s, got %s", keeper.authority, msg.Authority)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := keeper.RecordCACHitDirect(
		ctx,
		msg.OriginToolId,
		msg.ConsumingToolId,
		royalty,
	); err != nil {
		return nil, err
	}

	return &types.MsgRecordCACHitResponse{Recorded: true}, nil
}

func (m *msgServer) AggregateMetrics(goCtx context.Context, msg *types.MsgAggregateMetrics) (resp *types.MsgAggregateMetricsResponse, err error) {
	defer recoverRouter("router/AggregateMetrics", &err)
	if msg == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, "message is required")
	}
	if err := validateMsgServerBasic(msg); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.authority {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "only module authority can aggregate metrics; expected %s, got %s", keeper.authority, msg.Authority)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := keeper.AggregateMetrics(ctx, msg.Force); err != nil {
		return nil, err
	}

	return &types.MsgAggregateMetricsResponse{Aggregated: true}, nil
}
