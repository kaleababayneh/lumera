
package keeper

import (
	"context"
	"strconv"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/reserve/types"
)

// QueryServer implements the reserve module public query service.
type QueryServer struct {
	types.UnimplementedQueryServer
	keeper *Keeper
}

// NewQueryServerImpl constructs the reserve query server.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &QueryServer{keeper: k}
}

func (q *QueryServer) requireKeeper() (*Keeper, error) {
	if q == nil || q.keeper == nil {
		return nil, status.Error(codes.Internal, "reserve keeper not initialized")
	}
	return q.keeper, nil
}

func validateReserveQueryIdentifier(field, value string, required bool) error {
	if value == "" {
		if required {
			return status.Errorf(codes.InvalidArgument, "%s required", field)
		}
		return nil
	}
	if strings.TrimSpace(value) != value {
		return status.Errorf(codes.InvalidArgument, "%s must not contain leading or trailing whitespace", field)
	}
	if len(value) > types.MaxReserveIdentifierLen {
		return status.Errorf(codes.InvalidArgument, "%s exceeds %d-byte cap", field, types.MaxReserveIdentifierLen)
	}
	return nil
}

func parseReservePagination(page *query.PageRequest) (offset, limit uint64, countTotal bool, err error) {
	limit = 100
	if page == nil {
		return 0, limit, false, nil
	}
	if page.Reverse {
		return 0, 0, false, status.Error(codes.InvalidArgument, "reverse pagination not supported")
	}
	if len(page.Key) > 0 {
		if page.Offset > 0 {
			return 0, 0, false, status.Error(codes.InvalidArgument, "pagination key and offset are mutually exclusive")
		}
		offset, err = strconv.ParseUint(string(page.Key), 10, 64)
		if err != nil {
			return 0, 0, false, status.Error(codes.InvalidArgument, "invalid pagination key")
		}
	} else {
		offset = page.Offset
	}
	if page.Limit > 0 {
		limit = page.Limit
	}
	if limit > maxCommitmentListLimit {
		return 0, 0, false, status.Errorf(codes.InvalidArgument, "pagination limit exceeds %d", maxCommitmentListLimit)
	}
	return offset, limit, page.CountTotal, nil
}

func paginateReserveCommitments(items []*types.ReserveCommitmentSummary, page *query.PageRequest) ([]*types.ReserveCommitmentSummary, *query.PageResponse, error) {
	offset, limit, countTotal, err := parseReservePagination(page)
	if err != nil {
		return nil, nil, err
	}
	total := uint64(len(items))
	if offset >= total {
		resp := &query.PageResponse{}
		if countTotal {
			resp.Total = total
		}
		return []*types.ReserveCommitmentSummary{}, resp, nil
	}
	end := offset + limit
	if end < offset || end > total {
		end = total
	}
	resp := &query.PageResponse{}
	if end < total {
		resp.NextKey = []byte(strconv.FormatUint(end, 10))
	}
	if countTotal {
		resp.Total = total
	}
	return items[offset:end], resp, nil
}

func reserveParamsToResponse(params *types.Params) *types.ReserveParams {
	if params == nil {
		return nil
	}
	tiers := make([]*types.ReserveTierConfig, 0, len(params.Tiers))
	for _, tier := range params.Tiers {
		tiers = append(tiers, &types.ReserveTierConfig{
			Name:                   tier.Name,
			MinCommitmentAmount:    tier.MinCommitmentAmount.String(),
			DiscountBps:            tier.DiscountBps,
			DefaultDurationSeconds: tier.DefaultDurationSec,
			MaxActivePerPolicy:     tier.MaxActivePerPolicy,
			RolloverAllowed:        tier.RolloverAllowed,
		})
	}
	return &types.ReserveParams{
		CreditDenom: params.CreditDenom,
		Tiers:       tiers,
	}
}

func reserveQueryBlockTime(ctx context.Context) (time.Time, error) {
	now := sdk.UnwrapSDKContext(ctx).BlockTime()
	if now.IsZero() {
		return time.Time{}, status.Error(codes.FailedPrecondition, "reserve query requires block time")
	}
	return now, nil
}

func reserveCommitmentSummary(commitment types.ReserveCommitment, params *types.Params, now time.Time) *types.ReserveCommitmentSummary {
	statusValue := types.ReserveCommitmentStatus_RESERVE_COMMITMENT_STATUS_ACTIVE
	switch {
	case !commitment.RemainingAmount.Amount.IsPositive():
		statusValue = types.ReserveCommitmentStatus_RESERVE_COMMITMENT_STATUS_EXHAUSTED
	case !commitment.ExpireTime.After(now):
		statusValue = types.ReserveCommitmentStatus_RESERVE_COMMITMENT_STATUS_EXPIRED
	case params != nil && commitment.RemainingAmount.Denom != params.CreditDenom:
		statusValue = types.ReserveCommitmentStatus_RESERVE_COMMITMENT_STATUS_UNSPECIFIED
	}

	return &types.ReserveCommitmentSummary{
		CommitmentId:    commitment.ID,
		Owner:           commitment.Owner,
		PolicyId:        commitment.PolicyID,
		ToolId:          commitment.ToolID,
		Tier:            commitment.Tier,
		TotalAmount:     commitment.TotalAmount,
		RemainingAmount: commitment.RemainingAmount,
		DiscountBps:     commitment.DiscountBps,
		StartsAt:        commitment.StartTime,
		ExpiresAt:       commitment.ExpireTime,
		RolloverAllowed: commitment.RolloverAllowed,
		Status:          statusValue,
		RemainingBps:    commitment.RemainingRatio(),
	}
}

func reserveCommitmentSummaries(commitments []types.ReserveCommitment, params *types.Params, now time.Time) []*types.ReserveCommitmentSummary {
	summaries := make([]*types.ReserveCommitmentSummary, 0, len(commitments))
	for _, commitment := range commitments {
		summaries = append(summaries, reserveCommitmentSummary(commitment, params, now))
	}
	return summaries
}

func findActiveCommitment(commitments []types.ReserveCommitment, params *types.Params, now time.Time, toolID string) (types.ReserveCommitment, bool) {
	var (
		exactCommit    types.ReserveCommitment
		exactSet       bool
		fallbackCommit types.ReserveCommitment
		fallbackSet    bool
		anyCommit      types.ReserveCommitment
		anySet         bool
	)
	for _, commitment := range commitments {
		if !commitment.ExpireTime.After(now) {
			continue
		}
		if params != nil && commitment.RemainingAmount.Denom != params.CreditDenom {
			continue
		}
		if !commitment.RemainingAmount.Amount.IsPositive() {
			continue
		}
		if toolID == "" {
			if !anySet || preferReserveCommit(commitment, anyCommit) {
				anyCommit = commitment
				anySet = true
			}
			continue
		}
		if commitment.ToolID == toolID {
			if !exactSet || preferReserveCommit(commitment, exactCommit) {
				exactCommit = commitment
				exactSet = true
			}
			continue
		}
		if commitment.ToolID == "" {
			if !fallbackSet || preferReserveCommit(commitment, fallbackCommit) {
				fallbackCommit = commitment
				fallbackSet = true
			}
		}
	}
	switch {
	case exactSet:
		return exactCommit, true
	case fallbackSet:
		return fallbackCommit, true
	case anySet:
		return anyCommit, true
	default:
		return types.ReserveCommitment{}, false
	}
}

// Params retrieves reserve module parameters.
func (q *QueryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request required")
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	params, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryParamsResponse{Params: reserveParamsToResponse(params)}, nil
}

// Commitment retrieves one redacted reserve commitment by ID.
func (q *QueryServer) Commitment(ctx context.Context, req *types.QueryCommitmentRequest) (*types.QueryCommitmentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request required")
	}
	if err := validateReserveQueryIdentifier("commitment_id", req.CommitmentId, true); err != nil {
		return nil, err
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	now, err := reserveQueryBlockTime(ctx)
	if err != nil {
		return nil, err
	}
	params, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	commitment, found, err := keeper.GetCommitment(ctx, req.CommitmentId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !found {
		return nil, status.Error(codes.NotFound, "reserve commitment not found")
	}
	return &types.QueryCommitmentResponse{
		Commitment: reserveCommitmentSummary(*commitment, params, now),
	}, nil
}

// CommitmentsByOwner lists redacted commitments owned by one account.
func (q *QueryServer) CommitmentsByOwner(ctx context.Context, req *types.QueryCommitmentsByOwnerRequest) (*types.QueryCommitmentsByOwnerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request required")
	}
	if strings.TrimSpace(req.Owner) != req.Owner {
		return nil, status.Error(codes.InvalidArgument, "owner must not contain leading or trailing whitespace")
	}
	if _, err := sdk.AccAddressFromBech32(req.Owner); err != nil {
		return nil, status.Error(codes.InvalidArgument, "owner must be a valid bech32 address")
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	now, err := reserveQueryBlockTime(ctx)
	if err != nil {
		return nil, err
	}
	params, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	commitments, err := keeper.ListCommitmentsByOwner(ctx, req.Owner, maxCommitmentListLimit)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	page, pagination, err := paginateReserveCommitments(reserveCommitmentSummaries(commitments, params, now), req.Pagination)
	if err != nil {
		return nil, err
	}
	return &types.QueryCommitmentsByOwnerResponse{
		Commitments: page,
		Pagination:  pagination,
	}, nil
}

// CommitmentsByPolicy lists redacted commitments for one policy.
func (q *QueryServer) CommitmentsByPolicy(ctx context.Context, req *types.QueryCommitmentsByPolicyRequest) (*types.QueryCommitmentsByPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request required")
	}
	if err := validateReserveQueryIdentifier("policy_id", req.PolicyId, true); err != nil {
		return nil, err
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	now, err := reserveQueryBlockTime(ctx)
	if err != nil {
		return nil, err
	}
	params, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	commitments, err := keeper.ListCommitmentsByPolicy(ctx, req.PolicyId, maxCommitmentListLimit)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	page, pagination, err := paginateReserveCommitments(reserveCommitmentSummaries(commitments, params, now), req.Pagination)
	if err != nil {
		return nil, err
	}
	return &types.QueryCommitmentsByPolicyResponse{
		Commitments: page,
		Pagination:  pagination,
	}, nil
}

// ActiveCommitment reports whether a policy currently has a usable commitment.
func (q *QueryServer) ActiveCommitment(ctx context.Context, req *types.QueryActiveCommitmentRequest) (*types.QueryActiveCommitmentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request required")
	}
	if err := validateReserveQueryIdentifier("policy_id", req.PolicyId, true); err != nil {
		return nil, err
	}
	if err := validateReserveQueryIdentifier("tool_id", req.ToolId, false); err != nil {
		return nil, err
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	now, err := reserveQueryBlockTime(ctx)
	if err != nil {
		return nil, err
	}
	params, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	active, err := keeper.HasActiveCommitment(ctx, req.PolicyId, req.ToolId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !active {
		return &types.QueryActiveCommitmentResponse{Active: false}, nil
	}
	commitments, err := keeper.ListCommitmentsByPolicy(ctx, req.PolicyId, maxCommitmentListLimit)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	commitment, found := findActiveCommitment(commitments, params, now, req.ToolId)
	if !found {
		return &types.QueryActiveCommitmentResponse{Active: true}, nil
	}
	return &types.QueryActiveCommitmentResponse{
		Active:     true,
		Commitment: reserveCommitmentSummary(commitment, params, now),
	}, nil
}
