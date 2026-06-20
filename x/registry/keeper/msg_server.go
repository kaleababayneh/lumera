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
// resolve its publisher. NOTE: bond escrow (msg.Bond) is deferred to the bond
// slice; this slice records the tool's owner and timestamps.
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

	return &types.MsgRegisterToolResponse{ToolId: tool.ToolId}, nil
}
