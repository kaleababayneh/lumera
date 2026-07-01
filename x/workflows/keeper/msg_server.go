package keeper

import (
	"context"
	"fmt"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

// msgServer implements the generated workflows MsgServer.
type msgServer struct {
	keeper *Keeper
}

// NewMsgServerImpl returns an implementation of the workflows MsgServer.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

var _ types.MsgServer = (*msgServer)(nil)

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

func (s *msgServer) PublishWorkflow(ctx context.Context, msg *types.MsgPublishWorkflow) (resp *types.MsgPublishWorkflowResponse, err error) {
	defer recoverWorkflows("workflows/PublishWorkflow", &err)
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
	if err := keeper.PublishWorkflow(ctx, msg); err != nil {
		return nil, err
	}
	card := msg.GetWorkflowCard()
	return &types.MsgPublishWorkflowResponse{WorkflowID: card.GetWorkflowId(), Version: card.GetVersion()}, nil
}

func (s *msgServer) UpgradeWorkflow(ctx context.Context, msg *types.MsgUpgradeWorkflow) (resp *types.MsgUpgradeWorkflowResponse, err error) {
	defer recoverWorkflows("workflows/UpgradeWorkflow", &err)
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
	if err := keeper.UpgradeWorkflow(ctx, msg); err != nil {
		return nil, err
	}
	card := msg.GetWorkflowCard()
	return &types.MsgUpgradeWorkflowResponse{WorkflowID: card.GetWorkflowId(), Version: card.GetVersion()}, nil
}

func (s *msgServer) DeactivateWorkflow(ctx context.Context, msg *types.MsgDeactivateWorkflow) (resp *types.MsgDeactivateWorkflowResponse, err error) {
	defer recoverWorkflows("workflows/DeactivateWorkflow", &err)
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
	if err := keeper.DeactivateWorkflow(ctx, msg); err != nil {
		return nil, err
	}
	return &types.MsgDeactivateWorkflowResponse{}, nil
}

func (s *msgServer) TopUpAuthorBond(ctx context.Context, msg *types.MsgTopUpAuthorBond) (resp *types.MsgTopUpAuthorBondResponse, err error) {
	defer recoverWorkflows("workflows/TopUpAuthorBond", &err)
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
	if err := keeper.TopUpAuthorBond(ctx, msg); err != nil {
		return nil, err
	}
	return &types.MsgTopUpAuthorBondResponse{}, nil
}

func (s *msgServer) WithdrawBond(ctx context.Context, msg *types.MsgWithdrawBond) (resp *types.MsgWithdrawBondResponse, err error) {
	defer recoverWorkflows("workflows/WithdrawBond", &err)
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
	if err := keeper.WithdrawBond(ctx, msg); err != nil {
		return nil, err
	}
	return &types.MsgWithdrawBondResponse{}, nil
}
