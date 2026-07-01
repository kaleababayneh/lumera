package keeper

import (
	"context"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

// queryServer implements the generated workflows QueryServer.
type queryServer struct {
	keeper *Keeper
}

// NewQueryServerImpl returns an implementation of the workflows QueryServer.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

var _ types.QueryServer = (*queryServer)(nil)

func (s *queryServer) require() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, status.Error(codes.Internal, "workflows keeper not initialized")
	}
	return s.keeper, nil
}

// Params returns the module parameters.
func (s *queryServer) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	k, err := s.require()
	if err != nil {
		return nil, err
	}
	p, err := k.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryParamsResponse{
		MinAuthorBondAmount:  p.MinAuthorBondAmount,
		BondDenom:            p.BondDenom,
		WastedWorkBps:        p.WastedWorkBPS,
		MaxWorkflowVersions:  p.MaxWorkflowVersions,
		DisputeWindowSeconds: p.DisputeWindowSeconds,
	}, nil
}

// Workflow returns a published workflow by id + version.
func (s *queryServer) Workflow(ctx context.Context, req *types.QueryWorkflowRequest) (*types.QueryWorkflowResponse, error) {
	if req == nil || strings.TrimSpace(req.WorkflowId) == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow_id is required")
	}
	k, err := s.require()
	if err != nil {
		return nil, err
	}
	record, found, err := k.GetWorkflow(ctx, req.WorkflowId, req.Version)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found || record == nil {
		return nil, status.Error(codes.NotFound, "workflow not found")
	}
	return &types.QueryWorkflowResponse{
		WorkflowId:    record.WorkflowID,
		Version:       record.Version,
		Status:        record.Status,
		AuthorAddress: record.AuthorAddress,
		Card:          record.Card,
		CreatedHeight: record.CreatedHeight,
		UpdatedHeight: record.UpdatedHeight,
	}, nil
}

// AuthorBond returns an author's escrowed/slashed bond.
func (s *queryServer) AuthorBond(ctx context.Context, req *types.QueryAuthorBondRequest) (*types.QueryAuthorBondResponse, error) {
	if req == nil || strings.TrimSpace(req.Author) == "" {
		return nil, status.Error(codes.InvalidArgument, "author is required")
	}
	k, err := s.require()
	if err != nil {
		return nil, err
	}
	record, found, err := k.GetAuthorBond(ctx, req.Author)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found || record == nil {
		return nil, status.Error(codes.NotFound, "author bond not found")
	}
	return &types.QueryAuthorBondResponse{
		AuthorAddress: record.AuthorAddress,
		Bond:          coinOrZero(record.Bond),
		Slashed:       coinOrZero(record.Slashed),
		LockedFor:     record.LockedFor,
		UpdatedHeight: record.UpdatedHeight,
	}, nil
}

// coinOrZero dereferences an optional bond coin, returning a zero coin when nil.
func coinOrZero(c *sdk.Coin) sdk.Coin {
	if c == nil {
		return sdk.Coin{}
	}
	return *c
}
