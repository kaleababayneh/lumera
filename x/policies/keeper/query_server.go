
package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	types.UnimplementedQueryServer
	*Keeper
}

// NewQueryServer returns an implementation of the QueryServer interface.
func NewQueryServer(keeper *Keeper) types.QueryServer {
	return &queryServer{Keeper: keeper}
}

func (q queryServer) requireKeeper() (*Keeper, error) {
	if q.Keeper == nil || q.storeService == nil {
		return nil, fmt.Errorf("policies keeper not initialized")
	}
	return q.Keeper, nil
}

// maxPolicyQueryLimit caps results returned by list queries to prevent DoS.
const maxPolicyQueryLimit = 1000

// Policy implements types.QueryServer.
func (q queryServer) Policy(ctx context.Context, req *types.QueryPolicyRequest) (*types.QueryPolicyResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidPolicyID.Wrap("request is nil")
	}
	if err := validatePolicyQueryReference(req.PolicyId, req.Version); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	policy, err := keeper.GetPolicy(ctx, req.PolicyId, req.Version)
	if err != nil {
		return nil, err
	}
	clonedPolicy, err := clonePolicyProfile(policy)
	if err != nil {
		return nil, err
	}

	return &types.QueryPolicyResponse{
		Policy: clonedPolicy,
	}, nil
}

// Policies implements types.QueryServer.
func (q queryServer) Policies(ctx context.Context, req *types.QueryPoliciesRequest) (*types.QueryPoliciesResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidPolicyID.Wrap("request is nil")
	}
	if err := validatePolicyListRequest(req); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	guardedQueryServer := queryServer{Keeper: keeper}

	var policies []*types.PolicyProfile
	var pageRes *query.PageResponse

	// Filter by owner, state, both, or return all.
	switch {
	case req.Owner != "":
		policies, pageRes, err = guardedQueryServer.queryPoliciesByOwner(ctx, req.Owner, req.State, req.Pagination)
		if err != nil {
			return nil, err
		}
	case req.State != types.PolicyState_POLICY_STATE_UNSPECIFIED:
		policies, pageRes, err = guardedQueryServer.queryPoliciesByState(ctx, req.State, req.Pagination)
		if err != nil {
			return nil, err
		}
	default:
		policies, pageRes, err = guardedQueryServer.queryAllPolicies(ctx, req.Pagination)
		if err != nil {
			return nil, err
		}
	}

	return &types.QueryPoliciesResponse{
		Policies:   policies,
		Pagination: pageRes,
	}, nil
}

func validatePolicyQueryReference(policyID, version string) error {
	if strings.TrimSpace(policyID) == "" {
		return types.ErrInvalidPolicyID.Wrap("policy_id is required")
	}
	if strings.TrimSpace(policyID) != policyID {
		return types.ErrInvalidPolicyID.Wrap("policy_id must not contain leading or trailing whitespace")
	}
	if strings.TrimSpace(version) != version {
		return types.ErrInvalidPolicyID.Wrap("version must not contain leading or trailing whitespace")
	}
	return nil
}

func validatePolicyListRequest(req *types.QueryPoliciesRequest) error {
	if err := validatePolicyPaginationRequest(req.Pagination); err != nil {
		return err
	}
	if strings.TrimSpace(req.Owner) != req.Owner {
		return status.Error(codes.InvalidArgument, "owner must not contain leading or trailing whitespace")
	}
	switch req.State {
	case types.PolicyState_POLICY_STATE_UNSPECIFIED,
		types.PolicyState_POLICY_STATE_DRAFT,
		types.PolicyState_POLICY_STATE_REVIEW,
		types.PolicyState_POLICY_STATE_ACTIVE,
		types.PolicyState_POLICY_STATE_DEPRECATED,
		types.PolicyState_POLICY_STATE_ARCHIVED:
		return nil
	default:
		return status.Errorf(codes.InvalidArgument, "state has invalid value %d", req.State)
	}
}

func (q queryServer) queryAllPolicies(ctx context.Context, pagination *query.PageRequest) ([]*types.PolicyProfile, *query.PageResponse, error) {
	policies, pageRes, err := query.CollectionPaginate(
		ctx,
		q.state.Policies,
		boundPolicyPageRequest(pagination),
		func(_ string, policy *types.PolicyProfile) (*types.PolicyProfile, error) {
			return clonePolicyProfile(policy)
		},
	)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "paginate policies: %v", err)
	}

	return policies, pageRes, nil
}

func (q queryServer) queryPoliciesByOwner(ctx context.Context, owner string, state types.PolicyState, pagination *query.PageRequest) ([]*types.PolicyProfile, *query.PageResponse, error) {
	var predicate func(collections.Pair[string, string], collections.NoValue) (bool, error)
	if state != types.PolicyState_POLICY_STATE_UNSPECIFIED {
		predicate = func(key collections.Pair[string, string], _ collections.NoValue) (bool, error) {
			policy, err := q.GetPolicy(ctx, key.K2(), "")
			if err != nil {
				return false, err
			}
			return policyHasState(policy, state), nil
		}
	}

	policies, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.state.PolicyByOwner,
		boundPolicyPageRequest(pagination),
		predicate,
		func(key collections.Pair[string, string], _ collections.NoValue) (*types.PolicyProfile, error) {
			policy, err := q.GetPolicy(ctx, key.K2(), "")
			if err != nil {
				return nil, err
			}
			return clonePolicyProfile(policy)
		},
		query.WithCollectionPaginationPairPrefix[string, string](owner),
	)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "paginate policies by owner: %v", err)
	}

	return policies, pageRes, nil
}

func policyHasState(policy *types.PolicyProfile, state types.PolicyState) bool {
	return policy != nil && policy.Lifecycle != nil && policy.Lifecycle.State == state
}

func (q queryServer) queryPoliciesByState(ctx context.Context, state types.PolicyState, pagination *query.PageRequest) ([]*types.PolicyProfile, *query.PageResponse, error) {
	stateKey := uint32(state)
	policies, pageRes, err := query.CollectionPaginate(
		ctx,
		q.state.PolicyByState,
		boundPolicyPageRequest(pagination),
		func(key collections.Pair[uint32, string], _ collections.NoValue) (*types.PolicyProfile, error) {
			policy, err := q.GetPolicy(ctx, key.K2(), "")
			if err != nil {
				return nil, err
			}
			return clonePolicyProfile(policy)
		},
		query.WithCollectionPaginationPairPrefix[uint32, string](stateKey),
	)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "paginate policies by state: %v", err)
	}

	return policies, pageRes, nil
}

func validatePolicyPaginationRequest(pagination *query.PageRequest) error {
	if pagination == nil || pagination.Offset == 0 || len(pagination.Key) == 0 {
		return nil
	}
	return status.Error(codes.InvalidArgument, "invalid pagination: either offset or key may be set, not both")
}

func boundPolicyPageRequest(pagination *query.PageRequest) *query.PageRequest {
	if pagination == nil {
		return nil
	}

	pageReq := *pagination
	if pageReq.Limit > maxPolicyQueryLimit {
		pageReq.Limit = maxPolicyQueryLimit
	}
	return &pageReq
}

// Params implements types.QueryServer.
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidParams.Wrap("request is nil")
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	params, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	clonedParams, err := cloneParams(params)
	if err != nil {
		return nil, err
	}

	return &types.QueryParamsResponse{
		Params: clonedParams,
	}, nil
}

func clonePolicyProfile(policy *types.PolicyProfile) (*types.PolicyProfile, error) {
	if policy == nil {
		return nil, nil
	}
	cloned, ok := proto.Clone(policy).(*types.PolicyProfile)
	if !ok {
		return nil, status.Error(codes.Internal, "failed to clone policy profile")
	}
	return cloned, nil
}

func cloneParams(params *types.Params) (*types.Params, error) {
	if params == nil {
		return nil, nil
	}
	cloned, ok := proto.Clone(params).(*types.Params)
	if !ok {
		return nil, status.Error(codes.Internal, "failed to clone params")
	}
	return cloned, nil
}
