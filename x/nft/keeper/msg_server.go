package keeper

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/nft/types"
)

type msgServer struct {
	// UnimplementedMsgServer no-ops the nft RPCs not yet ported in this slice
	// (update/deactivate toolpack); they return an "unimplemented" error.
	types.UnimplementedMsgServer
	Keeper
}

// NewMsgServerImpl returns an nft MsgServer backed by the keeper.
func NewMsgServerImpl(k Keeper) types.MsgServer { return &msgServer{Keeper: k} }

var _ types.MsgServer = &msgServer{}

// MintToolpack mints a Toolpack NFT owned by the signing curator.
func (k msgServer) MintToolpack(goCtx context.Context, msg *types.MsgMintToolpack) (*types.MsgMintToolpackResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if _, err := sdk.AccAddressFromBech32(msg.Curator); err != nil {
		return nil, fmt.Errorf("invalid curator address: %w", err)
	}
	id, err := canonicalToolpackID(msg.Id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(msg.PolicyVersion) == "" {
		return nil, fmt.Errorf("policy_version is required")
	}
	if k.HasToolpack(ctx, id) {
		return nil, fmt.Errorf("toolpack %q already exists", id)
	}
	now := ctx.BlockTime()
	pack := &types.ToolpackNFT{
		Id:            id,
		Version:       1,
		Curator:       msg.Curator,
		Tools:         msg.Tools,
		PolicyVersion: msg.PolicyVersion,
		CreatedAt:     &now,
		UpdatedAt:     &now,
	}
	if err := k.SetToolpack(ctx, pack); err != nil {
		return nil, err
	}
	return &types.MsgMintToolpackResponse{}, nil
}

// RecordRoyaltyPayout records a toolpack royalty payout (authority-gated).
func (k msgServer) RecordRoyaltyPayout(goCtx context.Context, msg *types.MsgRecordRoyaltyPayout) (*types.MsgRecordRoyaltyPayoutResponse, error) {
	if err := k.Keeper.RecordRoyaltyPayout(goCtx, msg.Authority, msg.ToolpackId, msg.Amount); err != nil {
		return nil, err
	}
	return &types.MsgRecordRoyaltyPayoutResponse{}, nil
}
