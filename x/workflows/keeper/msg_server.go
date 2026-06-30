package keeper

import (
	"context"
	"fmt"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

// MsgServer is a scaffold-local interface until tx protobuf services land.
type MsgServer interface {
	PublishWorkflow(context.Context, *types.MsgPublishWorkflow) error
	UpgradeWorkflow(context.Context, *types.MsgUpgradeWorkflow) error
	DeactivateWorkflow(context.Context, *types.MsgDeactivateWorkflow) error
	TopUpAuthorBond(context.Context, *types.MsgTopUpAuthorBond) error
	WithdrawBond(context.Context, *types.MsgWithdrawBond) error
	UpdateParams(context.Context, *types.MsgUpdateParams) error
}

type msgServer struct {
	keeper *Keeper
}

// NewMsgServerImpl returns a scaffold message server.
func NewMsgServerImpl(k *Keeper) MsgServer {
	return &msgServer{keeper: k}
}

func (s *msgServer) requireKeeper() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, fmt.Errorf("workflows keeper not initialized")
	}
	return s.keeper, nil
}

func recoverWorkflows(action string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panic: %v", action, r)
	}
}

func (s *msgServer) PublishWorkflow(ctx context.Context, msg *types.MsgPublishWorkflow) (err error) {
	defer recoverWorkflows("workflows/PublishWorkflow", &err)
	if msg == nil {
		return fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return err
	}
	return keeper.PublishWorkflow(ctx, msg)
}

func (s *msgServer) UpgradeWorkflow(ctx context.Context, msg *types.MsgUpgradeWorkflow) (err error) {
	defer recoverWorkflows("workflows/UpgradeWorkflow", &err)
	if msg == nil {
		return fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return err
	}
	return keeper.UpgradeWorkflow(ctx, msg)
}

func (s *msgServer) DeactivateWorkflow(ctx context.Context, msg *types.MsgDeactivateWorkflow) (err error) {
	defer recoverWorkflows("workflows/DeactivateWorkflow", &err)
	if msg == nil {
		return fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return err
	}
	return keeper.DeactivateWorkflow(ctx, msg)
}

func (s *msgServer) TopUpAuthorBond(ctx context.Context, msg *types.MsgTopUpAuthorBond) (err error) {
	defer recoverWorkflows("workflows/TopUpAuthorBond", &err)
	if msg == nil {
		return fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return err
	}
	return keeper.TopUpAuthorBond(ctx, msg)
}

func (s *msgServer) WithdrawBond(ctx context.Context, msg *types.MsgWithdrawBond) (err error) {
	defer recoverWorkflows("workflows/WithdrawBond", &err)
	if msg == nil {
		return fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return err
	}
	return keeper.WithdrawBond(ctx, msg)
}

func (s *msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (err error) {
	defer recoverWorkflows("workflows/UpdateParams", &err)
	if msg == nil {
		return fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return err
	}
	if msg.Authority != keeper.Authority() {
		return fmt.Errorf("invalid authority: got %s want %s", msg.Authority, keeper.Authority())
	}
	return keeper.SetParams(ctx, msg.Params)
}
