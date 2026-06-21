package keeper

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

type msgServer struct {
	// UnimplementedMsgServer no-ops the registry RPCs not yet ported in this
	// slice (bonds, disputes, SLA/SLO, receipts, ...). They return an
	// "unimplemented" error until their keeper logic lands.
	types.UnimplementedMsgServer
	Keeper
}

// NewMsgServerImpl returns a registry MsgServer backed by the keeper.
func NewMsgServerImpl(k Keeper) types.MsgServer {
	return &msgServer{Keeper: k}
}

var _ types.MsgServer = &msgServer{}

// RegisterTool registers a ToolCard so it is discoverable and so settlement can
// resolve its publisher, then escrows the publisher's bond (skin-in-the-game).
// The signer must post at least the params' MinBondAmount; msg.Bond may exceed
// it. When MinBondAmount is zero and no bond is provided, no bond is escrowed.
func (k msgServer) RegisterTool(goCtx context.Context, msg *types.MsgRegisterTool) (*types.MsgRegisterToolResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	owner, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return nil, fmt.Errorf("invalid owner address: %w", err)
	}
	if msg.ToolCard == nil {
		return nil, fmt.Errorf("tool_card is required")
	}
	if strings.TrimSpace(msg.ToolCard.ToolId) == "" {
		return nil, fmt.Errorf("tool_id is required")
	}

	tool := msg.ToolCard
	// The signer is the authoritative owner/publisher of record.
	tool.Owner = owner.String()
	now := ctx.BlockTime()
	if tool.RegisteredAt == nil {
		tool.RegisteredAt = &now
	}
	tool.UpdatedAt = &now

	if err := k.SetToolCard(ctx, tool); err != nil {
		return nil, err
	}

	// Escrow the publisher bond. Post the explicit msg.Bond if given, else the
	// required minimum. CreateBond enforces the >= MinBondAmount floor and moves
	// the coins into the registry module account.
	required, err := k.Keeper.requiredBondAmount(ctx)
	if err != nil {
		return nil, err
	}
	bondToPost := msg.Bond
	if bondToPost.IsZero() {
		bondToPost = required
	}
	if !bondToPost.IsZero() {
		if err := k.Keeper.CreateBond(ctx, tool.ToolId, owner, bondToPost); err != nil {
			return nil, err
		}
	}

	return &types.MsgRegisterToolResponse{ToolId: tool.ToolId}, nil
}

// CreateBond creates or tops up a tool's bond, escrowing coins from the owner.
func (k msgServer) CreateBond(goCtx context.Context, msg *types.MsgCreateBond) (*types.MsgCreateBondResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	owner, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return nil, fmt.Errorf("invalid owner address: %w", err)
	}
	if strings.TrimSpace(msg.ToolId) == "" {
		return nil, fmt.Errorf("tool_id is required")
	}
	if err := k.Keeper.CreateBond(ctx, msg.ToolId, owner, msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgCreateBondResponse{}, nil
}

// WithdrawBond returns part (or all) of a tool's bond to its owner.
func (k msgServer) WithdrawBond(goCtx context.Context, msg *types.MsgWithdrawBond) (*types.MsgWithdrawBondResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	owner, err := sdk.AccAddressFromBech32(msg.Owner)
	if err != nil {
		return nil, fmt.Errorf("invalid owner address: %w", err)
	}
	if strings.TrimSpace(msg.ToolId) == "" {
		return nil, fmt.Errorf("tool_id is required")
	}
	if err := k.Keeper.WithdrawBond(ctx, msg.ToolId, owner, msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgWithdrawBondResponse{}, nil
}

// SubmitReceipt anchors a SuperNode-attested Proof-of-Service receipt. The
// signer (msg.Router) is the attesting SuperNode account.
func (k msgServer) SubmitReceipt(goCtx context.Context, msg *types.MsgSubmitReceipt) (*types.MsgSubmitReceiptResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if _, err := sdk.AccAddressFromBech32(msg.Router); err != nil {
		return nil, fmt.Errorf("invalid submitter address: %w", err)
	}
	if msg.Receipt == nil {
		return nil, fmt.Errorf("receipt is required")
	}
	if err := k.Keeper.SubmitReceipt(ctx, msg.Router, msg.Receipt); err != nil {
		return nil, err
	}
	return &types.MsgSubmitReceiptResponse{ReceiptId: msg.Receipt.ReceiptId}, nil
}
