package keeper

import (
	"context"
	"errors"
	"fmt"

	"github.com/LumeraProtocol/lumera/x/passport/types"
)

// msgServer implements the MsgServer interface.
type msgServer struct {
	types.UnimplementedMsgServer
	Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface.
func NewMsgServerImpl(keeper *Keeper) types.MsgServer {
	if keeper == nil {
		return &msgServer{}
	}
	return &msgServer{Keeper: *keeper}
}

var _ types.MsgServer = msgServer{}

var (
	errEmptyRequest         = errors.New("empty request")
	errKeeperNotInitialized = errors.New("passport keeper not initialized")
)

func (k msgServer) requireKeeper() (*Keeper, error) {
	if k.bankKeeper == nil || k.accountKeeper == nil {
		return nil, errKeeperNotInitialized
	}
	return &k.Keeper, nil
}

func recoverPassport(action string, err *error) {
	if r := recover(); r != nil {
		*err = errors.New(action + " panic: " + fmt.Sprint(r))
	}
}

// RegisterPassport handles MsgRegisterPassport.
func (k msgServer) RegisterPassport(ctx context.Context, msg *types.MsgRegisterPassport) (resp *types.MsgRegisterPassportResponse, err error) {
	defer recoverPassport("passport/RegisterPassport", &err)
	if msg == nil {
		return nil, errEmptyRequest
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := k.requireKeeper()
	if err != nil {
		return nil, err
	}

	passport, err := keeper.RegisterPassport(ctx, msg)
	if err != nil {
		return nil, err
	}

	return &types.MsgRegisterPassportResponse{
		PassportId: passport.PassportId,
	}, nil
}

// SuspendPassport handles MsgSuspendPassport.
func (k msgServer) SuspendPassport(ctx context.Context, msg *types.MsgSuspendPassport) (resp *types.MsgSuspendPassportResponse, err error) {
	defer recoverPassport("passport/SuspendPassport", &err)
	if msg == nil {
		return nil, errEmptyRequest
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := k.requireKeeper()
	if err != nil {
		return nil, err
	}

	if err := keeper.SuspendPassport(ctx, msg); err != nil {
		return nil, err
	}

	return &types.MsgSuspendPassportResponse{}, nil
}

// RevokePassport handles MsgRevokePassport.
func (k msgServer) RevokePassport(ctx context.Context, msg *types.MsgRevokePassport) (resp *types.MsgRevokePassportResponse, err error) {
	defer recoverPassport("passport/RevokePassport", &err)
	if msg == nil {
		return nil, errEmptyRequest
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := k.requireKeeper()
	if err != nil {
		return nil, err
	}

	if err := keeper.RevokePassport(ctx, msg); err != nil {
		return nil, err
	}

	return &types.MsgRevokePassportResponse{}, nil
}

// ReactivatePassport handles MsgReactivatePassport.
func (k msgServer) ReactivatePassport(ctx context.Context, msg *types.MsgReactivatePassport) (resp *types.MsgReactivatePassportResponse, err error) {
	defer recoverPassport("passport/ReactivatePassport", &err)
	if msg == nil {
		return nil, errEmptyRequest
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := k.requireKeeper()
	if err != nil {
		return nil, err
	}

	if err := keeper.ReactivatePassport(ctx, msg); err != nil {
		return nil, err
	}

	return &types.MsgReactivatePassportResponse{}, nil
}

// SlashStake handles MsgSlashStake.
func (k msgServer) SlashStake(ctx context.Context, msg *types.MsgSlashStake) (resp *types.MsgSlashStakeResponse, err error) {
	defer recoverPassport("passport/SlashStake", &err)
	if msg == nil {
		return nil, errEmptyRequest
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := k.requireKeeper()
	if err != nil {
		return nil, err
	}

	remaining, err := keeper.SlashStake(ctx, msg)
	if err != nil {
		return nil, err
	}

	return &types.MsgSlashStakeResponse{
		RemainingStake: remaining,
	}, nil
}

// TopUpStake handles MsgTopUpStake.
func (k msgServer) TopUpStake(ctx context.Context, msg *types.MsgTopUpStake) (resp *types.MsgTopUpStakeResponse, err error) {
	defer recoverPassport("passport/TopUpStake", &err)
	if msg == nil {
		return nil, errEmptyRequest
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := k.requireKeeper()
	if err != nil {
		return nil, err
	}

	stake, err := keeper.TopUpStake(ctx, msg)
	if err != nil {
		return nil, err
	}

	return &types.MsgTopUpStakeResponse{
		Stake: stake,
	}, nil
}

// UnregisterPassport handles MsgUnregisterPassport.
func (k msgServer) UnregisterPassport(ctx context.Context, msg *types.MsgUnregisterPassport) (resp *types.MsgUnregisterPassportResponse, err error) {
	defer recoverPassport("passport/UnregisterPassport", &err)
	if msg == nil {
		return nil, errEmptyRequest
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := k.requireKeeper()
	if err != nil {
		return nil, err
	}

	refunded, err := keeper.UnregisterPassport(ctx, msg)
	if err != nil {
		return nil, err
	}

	return &types.MsgUnregisterPassportResponse{
		RefundedStake: refunded,
	}, nil
}

// UpdateParams handles MsgUpdateParams.
func (k msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (resp *types.MsgUpdateParamsResponse, err error) {
	defer recoverPassport("passport/UpdateParams", &err)
	if msg == nil {
		return nil, errEmptyRequest
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := k.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.Authority() {
		return nil, types.ErrUnauthorized
	}
	if err := keeper.SetParams(ctx, &msg.Params); err != nil {
		return nil, err
	}
	return &types.MsgUpdateParamsResponse{}, nil
}
