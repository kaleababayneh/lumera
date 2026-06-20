
package keeper

import (
	"context"
	"errors"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	querytypes "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

const maxClaimQueryLimit = 1000

var errInvalidClaimPaginationKey = errors.New("invalid pagination key")

// queryServer implements the x/insurance Query service
type queryServer struct {
	types.UnimplementedQueryServer
	keeper Keeper
}

// NewQueryServerImpl creates a new Query server backed by the keeper
func NewQueryServerImpl(k Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

func validateInsuranceQueryID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return status.Errorf(codes.InvalidArgument, "%s is required", field)
	}
	if trimmed != value {
		return status.Errorf(codes.InvalidArgument, "%s must be canonical", field)
	}
	if len(value) > types.MaxInsuranceIDLen {
		return status.Errorf(codes.InvalidArgument, "%s exceeds %d-byte cap (got %d)", field, types.MaxInsuranceIDLen, len(value))
	}
	return nil
}

func validateOptionalInsuranceQueryID(field, value string) error {
	if value == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value {
		return status.Errorf(codes.InvalidArgument, "%s must be canonical", field)
	}
	if len(value) > types.MaxInsuranceIDLen {
		return status.Errorf(codes.InvalidArgument, "%s exceeds %d-byte cap (got %d)", field, types.MaxInsuranceIDLen, len(value))
	}
	return nil
}

func validateOptionalClaimStatusFilter(value string) error {
	if value == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value {
		return status.Error(codes.InvalidArgument, "status must be canonical")
	}
	if _, ok := types.ClaimStatus_value[value]; !ok {
		return status.Errorf(codes.InvalidArgument, "status has invalid value %q", value)
	}
	return nil
}

func (q queryServer) requireKeeper() (*Keeper, error) {
	if q.keeper.storeService == nil || q.keeper.bankKeeper == nil || q.keeper.accountKeeper == nil || q.keeper.state.ClaimsByReceipt == nil {
		return nil, status.Error(codes.Internal, "insurance keeper not initialized")
	}
	return &q.keeper, nil
}

func validateInsurancePaginationRequest(pageReq *querytypes.PageRequest) error {
	if pageReq == nil {
		return nil
	}
	if pageReq.Reverse {
		return status.Error(codes.Unimplemented, "reverse pagination not supported")
	}
	if len(pageReq.Key) > 0 && pageReq.Offset > 0 {
		return status.Error(codes.InvalidArgument, "pagination key and offset are mutually exclusive")
	}
	return nil
}

// PoolStatus returns the current insurance pool status and metrics
func (q queryServer) PoolStatus(ctx context.Context, req *types.QueryPoolStatusRequest) (*types.QueryPoolStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get pool balance
	poolBalance, err := keeper.GetPoolBalance(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get pool balance: %v", err)
	}

	// Get pool state from collections
	poolState, err := keeper.state.PoolBalance.Get(sdkCtx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "failed to get pool state: %v", err)
	}

	// Get pool metrics
	poolMetrics, err := keeper.state.PoolMetrics.Get(sdkCtx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "failed to get pool metrics: %v", err)
	}

	return &types.QueryPoolStatusResponse{
		Balance: poolBalance,
		State:   clonePoolState(poolState),
		Metrics: clonePoolMetrics(poolMetrics),
	}, nil
}

// GetClaim returns a specific claim by ID
func (q queryServer) GetClaim(ctx context.Context, req *types.QueryGetClaimRequest) (*types.QueryGetClaimResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	if err := validateInsuranceQueryID("claim_id", req.ClaimId); err != nil {
		return nil, err
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Retrieve claim from IndexedMap
	claim, err := keeper.state.ClaimsByReceipt.Get(sdkCtx, req.ClaimId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "claim %s not found", req.ClaimId)
		}
		return nil, status.Errorf(codes.Internal, "failed to get claim: %v", err)
	}

	return &types.QueryGetClaimResponse{
		Claim: cloneClaim(claim),
	}, nil
}

// ListClaims returns all claims with optional filtering by claimant or status
func (q queryServer) ListClaims(ctx context.Context, req *types.QueryListClaimsRequest) (*types.QueryListClaimsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	if err := validateInsurancePaginationRequest(req.Pagination); err != nil {
		return nil, err
	}
	if err := validateOptionalInsuranceQueryID("claimant_id", req.ClaimantId); err != nil {
		return nil, err
	}
	if err := validateOptionalInsuranceQueryID("publisher_id", req.PublisherId); err != nil {
		return nil, err
	}
	if err := validateOptionalClaimStatusFilter(req.Status); err != nil {
		return nil, err
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	pagination := newClaimPagination(req.Pagination)
	claims := make([]*types.Claim, 0, int(pagination.limit))

	applyFilters := func(claim *types.Claim) bool {
		if claim == nil {
			return false
		}
		if req.ClaimantId != "" && claim.ClaimantId != req.ClaimantId {
			return false
		}
		if req.Status != "" && claim.Status.String() != req.Status {
			return false
		}
		if req.PublisherId != "" && claim.PublisherId != req.PublisherId {
			return false
		}
		return true
	}

	pageRes, err := collectClaimPage(sdkCtx, keeper, applyFilters, pagination, &claims)
	if err != nil {
		if errors.Is(err, errInvalidClaimPaginationKey) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "failed to walk claims: %v", err)
	}

	return &types.QueryListClaimsResponse{
		Claims:     cloneClaims(claims),
		Pagination: pageRes,
	}, nil
}

// GetParams returns the current module parameters
func (q queryServer) GetParams(ctx context.Context, req *types.QueryGetParamsRequest) (*types.QueryGetParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	params := keeper.GetParams(ctx)

	return &types.QueryGetParamsResponse{
		Params: cloneParams(params),
	}, nil
}

func clonePoolState(poolState *types.PoolState) *types.PoolState {
	if poolState == nil {
		return nil
	}
	return proto.Clone(poolState).(*types.PoolState)
}

func clonePoolMetrics(metrics *types.PoolMetrics) *types.PoolMetrics {
	if metrics == nil {
		return nil
	}
	return proto.Clone(metrics).(*types.PoolMetrics)
}

func cloneClaim(claim *types.Claim) *types.Claim {
	if claim == nil {
		return nil
	}
	return proto.Clone(claim).(*types.Claim)
}

func cloneClaims(claims []*types.Claim) []*types.Claim {
	if len(claims) == 0 {
		return claims
	}
	cloned := make([]*types.Claim, 0, len(claims))
	for _, claim := range claims {
		cloned = append(cloned, cloneClaim(claim))
	}
	return cloned
}

func cloneParams(params *types.Params) *types.Params {
	if params == nil {
		return nil
	}
	return proto.Clone(params).(*types.Params)
}

type claimPagination struct {
	key        string
	offset     uint64
	limit      uint64
	countTotal bool
}

func newClaimPagination(pageReq *querytypes.PageRequest) claimPagination {
	if pageReq == nil {
		return claimPagination{
			limit:      maxClaimQueryLimit,
			countTotal: true,
		}
	}

	limit := pageReq.GetLimit()
	if limit == 0 {
		limit = 100
	}
	if limit > maxClaimQueryLimit {
		limit = maxClaimQueryLimit
	}

	return claimPagination{
		key:        string(pageReq.GetKey()),
		offset:     pageReq.GetOffset(),
		limit:      limit,
		countTotal: pageReq.GetCountTotal(),
	}
}

func collectClaimPage(
	ctx sdk.Context,
	keeper *Keeper,
	matches func(*types.Claim) bool,
	pagination claimPagination,
	claims *[]*types.Claim,
) (*querytypes.PageResponse, error) {
	var matched uint64
	var remainingOffset = pagination.offset
	keyFound := pagination.key == ""
	pageRes := &querytypes.PageResponse{}

	err := keeper.state.ClaimsByReceipt.Walk(ctx, nil, func(claimID string, claim *types.Claim) (bool, error) {
		if !matches(claim) {
			return false, nil
		}
		matched++

		if !keyFound {
			switch strings.Compare(claimID, pagination.key) {
			case -1:
				return false, nil
			case 0:
				keyFound = true
			default:
				return true, errInvalidClaimPaginationKey
			}
		}

		if remainingOffset > 0 {
			remainingOffset--
			return false, nil
		}

		if uint64(len(*claims)) < pagination.limit {
			*claims = append(*claims, claim)
			return false, nil
		}

		if len(pageRes.NextKey) == 0 {
			pageRes.NextKey = []byte(claimID)
		}
		return !pagination.countTotal, nil
	})
	if err != nil {
		return nil, err
	}
	if !keyFound {
		return nil, errInvalidClaimPaginationKey
	}
	if pagination.countTotal {
		pageRes.Total = matched
	}

	return pageRes, nil
}
