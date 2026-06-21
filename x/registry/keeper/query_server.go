package keeper

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/registry/types"
)

// queryServer serves registry queries. It embeds UnimplementedQueryServer so the
// not-yet-ported query RPCs return "unimplemented"; this slice implements the
// focused tool lookups. GetToolPublisher is consumed in-process by the credits
// keeper, not over gRPC.
type queryServer struct {
	types.UnimplementedQueryServer
	k Keeper
}

// NewQueryServerImpl returns a registry QueryServer backed by the keeper.
func NewQueryServerImpl(k Keeper) types.QueryServer {
	return &queryServer{k: k}
}

var _ types.QueryServer = &queryServer{}

// GetTool returns a registered ToolCard (including its owner/publisher) by id.
func (q queryServer) GetTool(goCtx context.Context, req *types.QueryGetToolRequest) (*types.QueryGetToolResponse, error) {
	if req == nil || strings.TrimSpace(req.ToolId) == "" {
		return nil, status.Error(codes.InvalidArgument, "tool_id is required")
	}
	tool, found := q.k.GetToolCard(sdk.UnwrapSDKContext(goCtx), req.ToolId)
	if !found {
		return nil, status.Errorf(codes.NotFound, "tool %q not found", req.ToolId)
	}
	return &types.QueryGetToolResponse{Tool: tool}, nil
}

// GetBond returns the bond record escrowed for a tool by its publisher.
func (q queryServer) GetBond(goCtx context.Context, req *types.QueryGetBondRequest) (*types.QueryGetBondResponse, error) {
	if req == nil || strings.TrimSpace(req.ToolId) == "" {
		return nil, status.Error(codes.InvalidArgument, "tool_id is required")
	}
	bond, found := q.k.GetBondRecord(sdk.UnwrapSDKContext(goCtx), req.ToolId)
	if !found {
		return nil, status.Errorf(codes.NotFound, "bond for tool %q not found", req.ToolId)
	}
	return &types.QueryGetBondResponse{Bond: bond}, nil
}

// GetChallenge returns a receipt dispute by its challenge id.
func (q queryServer) GetChallenge(goCtx context.Context, req *types.QueryGetChallengeRequest) (*types.QueryGetChallengeResponse, error) {
	if req == nil || strings.TrimSpace(req.ChallengeId) == "" {
		return nil, status.Error(codes.InvalidArgument, "challenge_id is required")
	}
	c, found := q.k.GetChallenge(sdk.UnwrapSDKContext(goCtx), req.ChallengeId)
	if !found {
		return nil, status.Errorf(codes.NotFound, "challenge %q not found", req.ChallengeId)
	}
	return &types.QueryGetChallengeResponse{Challenge: c}, nil
}

// GetReceipt returns a stored Proof-of-Service usage receipt by id.
func (q queryServer) GetReceipt(goCtx context.Context, req *types.QueryGetReceiptRequest) (*types.QueryGetReceiptResponse, error) {
	if req == nil || strings.TrimSpace(req.ReceiptId) == "" {
		return nil, status.Error(codes.InvalidArgument, "receipt_id is required")
	}
	receipt, found := q.k.GetUsageReceipt(sdk.UnwrapSDKContext(goCtx), req.ReceiptId)
	if !found {
		return nil, status.Errorf(codes.NotFound, "receipt %q not found", req.ReceiptId)
	}
	return &types.QueryGetReceiptResponse{Receipt: receipt, Status: receipt.Status}, nil
}
