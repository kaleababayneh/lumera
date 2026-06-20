package keeper

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/nft/types"
)

type queryServer struct {
	types.UnimplementedQueryServer
	k Keeper
}

// NewQueryServerImpl returns an nft QueryServer backed by the keeper.
func NewQueryServerImpl(k Keeper) types.QueryServer { return &queryServer{k: k} }

var _ types.QueryServer = &queryServer{}

// Toolpack returns a minted ToolpackNFT (including its curator) by id.
func (q queryServer) Toolpack(goCtx context.Context, req *types.QueryToolpackRequest) (*types.QueryToolpackResponse, error) {
	if req == nil || strings.TrimSpace(req.Id) == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	pack, found, err := q.k.GetToolpack(goCtx, req.Id)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if !found {
		return nil, status.Errorf(codes.NotFound, "toolpack %q not found", req.Id)
	}
	return &types.QueryToolpackResponse{Toolpack: pack}, nil
}
