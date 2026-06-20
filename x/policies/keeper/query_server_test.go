//go:build cosmos
// +build cosmos

package keeper

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

func TestQueryServerPolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	// Create a policy first
	policy := &types.PolicyProfile{
		PolicyId:      "query-test-1",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Query Test Policy",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Query the policy
	req := &types.QueryPolicyRequest{
		PolicyId: "query-test-1",
		Version:  "1.0.0",
	}
	resp, err := queryServer.Policy(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "query-test-1", resp.Policy.PolicyId)
	require.Equal(t, "Query Test Policy", resp.Policy.Metadata.Name)

	resp.Policy.Metadata.Name = "mutated response"
	respAgain, err := queryServer.Policy(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "Query Test Policy", respAgain.Policy.Metadata.Name)
}

func TestQueryServerPolicyLatest(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	// Create v1.0.0
	policy1 := &types.PolicyProfile{
		PolicyId:      "versioned-query",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Version 1",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy1))

	// Create v2.0.0
	policy2 := &types.PolicyProfile{
		PolicyId:      "versioned-query",
		Version:       "2.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Version 2",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy2))

	// Query without version (should get latest)
	req := &types.QueryPolicyRequest{
		PolicyId: "versioned-query",
	}
	resp, err := queryServer.Policy(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "2.0.0", resp.Policy.Version)
	require.Equal(t, "Version 2", resp.Policy.Metadata.Name)
}

func TestQueryServerPolicyNotFound(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	req := &types.QueryPolicyRequest{
		PolicyId: "nonexistent",
		Version:  "1.0.0",
	}
	_, err := queryServer.Policy(ctx, req)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrPolicyNotFound)
}

func TestQueryServerPolicyNilRequest(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	_, err := queryServer.Policy(ctx, nil)
	require.Error(t, err)
}

func TestQueryServerPoliciesRejectsInvalidFiltersBeforeNilKeeper(t *testing.T) {
	queryServer := NewQueryServer(nil)
	ctx := context.Background()

	tests := []struct {
		name string
		req  *types.QueryPoliciesRequest
		want string
	}{
		{
			name: "padded owner",
			req:  &types.QueryPoliciesRequest{Owner: " lumera1owner"},
			want: "owner must not contain leading or trailing whitespace",
		},
		{
			name: "invalid state",
			req:  &types.QueryPoliciesRequest{State: types.PolicyState(999)},
			want: "state has invalid value 999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := queryServer.Policies(ctx, tt.req)
			require.Error(t, err)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Contains(t, err.Error(), tt.want)
			require.NotContains(t, err.Error(), "keeper not initialized")
		})
	}
}

func TestQueryServerPolicies(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	// Create multiple policies
	for i := 1; i <= 5; i++ {
		policy := &types.PolicyProfile{
			PolicyId:      "list-policy-" + string(rune('0'+i)),
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "List Policy",
				Owner: "lumera1owner",
			},
			Lifecycle: &types.PolicyLifecycle{
				State:     types.PolicyState_POLICY_STATE_ACTIVE,
				CreatedBy: "lumera1owner",
			},
		}
		require.NoError(t, k.CreatePolicy(ctx, policy))
	}

	// Query all policies
	req := &types.QueryPoliciesRequest{}
	resp, err := queryServer.Policies(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Policies, 5)
	require.Equal(t, uint64(5), resp.Pagination.Total)

	resp.Policies[0].Metadata.Name = "mutated list response"
	stored, err := queryServer.Policy(ctx, &types.QueryPolicyRequest{
		PolicyId: resp.Policies[0].PolicyId,
		Version:  resp.Policies[0].Version,
	})
	require.NoError(t, err)
	require.Equal(t, "List Policy", stored.Policy.Metadata.Name)
}

func TestQueryServerPoliciesPaginationOffset(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	for _, id := range []string{"page-policy-001", "page-policy-002", "page-policy-003", "page-policy-004", "page-policy-005"} {
		require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy(id, "lumera1owner", types.PolicyState_POLICY_STATE_ACTIVE)))
	}

	resp, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
		Pagination: &query.PageRequest{
			Offset:     2,
			Limit:      2,
			CountTotal: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"page-policy-003", "page-policy-004"}, queryPolicyIDs(resp.Policies))
	require.NotEmpty(t, resp.Pagination.NextKey)
	require.Equal(t, uint64(5), resp.Pagination.Total)
}

func TestQueryServerPoliciesPaginationKey(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	for _, id := range []string{"key-policy-001", "key-policy-002", "key-policy-003", "key-policy-004", "key-policy-005"} {
		require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy(id, "lumera1owner", types.PolicyState_POLICY_STATE_ACTIVE)))
	}

	first, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"key-policy-001", "key-policy-002"}, queryPolicyIDs(first.Policies))
	require.NotEmpty(t, first.Pagination.NextKey)

	second, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
		Pagination: &query.PageRequest{
			Key:   first.Pagination.NextKey,
			Limit: 2,
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"key-policy-003", "key-policy-004"}, queryPolicyIDs(second.Policies))
	require.NotEmpty(t, second.Pagination.NextKey)

	third, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
		Pagination: &query.PageRequest{
			Key:   second.Pagination.NextKey,
			Limit: 2,
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"key-policy-005"}, queryPolicyIDs(third.Policies))
	require.Empty(t, third.Pagination.NextKey)
}

func TestQueryServerPoliciesByOwnerPaginationKey(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	for _, id := range []string{"owner-page-001", "owner-page-002", "owner-page-003"} {
		require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy(id, "lumera1owner-page", types.PolicyState_POLICY_STATE_ACTIVE)))
	}
	require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy("owner-other-001", "lumera1owner-other", types.PolicyState_POLICY_STATE_ACTIVE)))

	first, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
		Owner: "lumera1owner-page",
		Pagination: &query.PageRequest{
			Limit:      2,
			CountTotal: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"owner-page-001", "owner-page-002"}, queryPolicyIDs(first.Policies))
	require.NotEmpty(t, first.Pagination.NextKey)
	require.Equal(t, uint64(3), first.Pagination.Total)

	second, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
		Owner: "lumera1owner-page",
		Pagination: &query.PageRequest{
			Key:   first.Pagination.NextKey,
			Limit: 2,
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"owner-page-003"}, queryPolicyIDs(second.Policies))
	require.Empty(t, second.Pagination.NextKey)
}

func TestQueryServerPoliciesByStatePaginationOffset(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	for _, id := range []string{"state-page-001", "state-page-002", "state-page-003"} {
		require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy(id, "lumera1owner", types.PolicyState_POLICY_STATE_ACTIVE)))
	}
	require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy("state-draft-001", "lumera1owner", types.PolicyState_POLICY_STATE_DRAFT)))

	resp, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
		State: types.PolicyState_POLICY_STATE_ACTIVE,
		Pagination: &query.PageRequest{
			Offset:     1,
			Limit:      1,
			CountTotal: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"state-page-002"}, queryPolicyIDs(resp.Policies))
	require.NotEmpty(t, resp.Pagination.NextKey)
	require.Equal(t, uint64(3), resp.Pagination.Total)
}

func TestQueryServerPoliciesRejectsOffsetAndKey(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy("bad-page-policy", "lumera1owner", types.PolicyState_POLICY_STATE_ACTIVE)))

	_, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
		Pagination: &query.PageRequest{
			Offset: 1,
			Key:    []byte("bad"),
			Limit:  1,
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "either offset or key")
}

func TestQueryServerPoliciesByOwner(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	// Create policies for different owners
	for i := 1; i <= 3; i++ {
		policy := &types.PolicyProfile{
			PolicyId:      "owner1-policy-" + string(rune('0'+i)),
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Owner1 Policy",
				Owner: "lumera1owner1",
			},
			Lifecycle: &types.PolicyLifecycle{
				State:     types.PolicyState_POLICY_STATE_ACTIVE,
				CreatedBy: "lumera1owner1",
			},
		}
		require.NoError(t, k.CreatePolicy(ctx, policy))
	}

	policy := &types.PolicyProfile{
		PolicyId:      "owner2-policy-1",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Owner2 Policy",
			Owner: "lumera1owner2",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner2",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Query by owner1
	req := &types.QueryPoliciesRequest{
		Owner: "lumera1owner1",
	}
	resp, err := queryServer.Policies(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Policies, 3)
}

func TestQueryServerPoliciesByOwnerAndState(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy("owner-state-active-001", "lumera1owner-combined", types.PolicyState_POLICY_STATE_ACTIVE)))
	require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy("owner-state-draft-001", "lumera1owner-combined", types.PolicyState_POLICY_STATE_DRAFT)))
	require.NoError(t, k.CreatePolicy(ctx, queryTestPolicy("owner-state-active-other", "lumera1owner-other", types.PolicyState_POLICY_STATE_ACTIVE)))

	resp, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
		Owner: "lumera1owner-combined",
		State: types.PolicyState_POLICY_STATE_ACTIVE,
		Pagination: &query.PageRequest{
			Limit:      2,
			CountTotal: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"owner-state-active-001"}, queryPolicyIDs(resp.Policies))
	require.Empty(t, resp.Pagination.NextKey)
	require.Equal(t, uint64(1), resp.Pagination.Total)
}

func TestQueryServerPoliciesByState(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	// Create policies with different states
	states := []types.PolicyState{
		types.PolicyState_POLICY_STATE_DRAFT,
		types.PolicyState_POLICY_STATE_DRAFT,
		types.PolicyState_POLICY_STATE_ACTIVE,
		types.PolicyState_POLICY_STATE_ACTIVE,
		types.PolicyState_POLICY_STATE_ACTIVE,
	}

	for i, state := range states {
		policy := &types.PolicyProfile{
			PolicyId:      "state-policy-" + string(rune('0'+i)),
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "State Policy",
				Owner: "lumera1owner",
			},
			Lifecycle: &types.PolicyLifecycle{
				State:     state,
				CreatedBy: "lumera1owner",
			},
		}
		require.NoError(t, k.CreatePolicy(ctx, policy))
	}

	// Query ACTIVE policies
	req := &types.QueryPoliciesRequest{
		State: types.PolicyState_POLICY_STATE_ACTIVE,
	}
	resp, err := queryServer.Policies(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Policies, 3)

	// Query DRAFT policies
	req = &types.QueryPoliciesRequest{
		State: types.PolicyState_POLICY_STATE_DRAFT,
	}
	resp, err = queryServer.Policies(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Policies, 2)
}

func TestQueryServerPoliciesNilRequest(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	_, err := queryServer.Policies(ctx, nil)
	require.Error(t, err)
}

func TestQueryServerValidatesBeforeSDKContext(t *testing.T) {
	ctx := context.Background()
	queryServer := NewQueryServer(nil)
	zeroKeeperQueryServer := NewQueryServer(&Keeper{})

	tests := []struct {
		name       string
		query      func() error
		wantErrMsg string
	}{
		{
			name: "policy nil request",
			query: func() error {
				_, err := queryServer.Policy(ctx, nil)
				return err
			},
			wantErrMsg: "request is nil",
		},
		{
			name: "policy missing id",
			query: func() error {
				_, err := queryServer.Policy(ctx, &types.QueryPolicyRequest{})
				return err
			},
			wantErrMsg: "policy_id is required",
		},
		{
			name: "policy padded id",
			query: func() error {
				_, err := queryServer.Policy(ctx, &types.QueryPolicyRequest{
					PolicyId: " policy-direct",
					Version:  "1.0.0",
				})
				return err
			},
			wantErrMsg: "policy_id must not contain leading or trailing whitespace",
		},
		{
			name: "policy padded version",
			query: func() error {
				_, err := queryServer.Policy(ctx, &types.QueryPolicyRequest{
					PolicyId: "policy-direct",
					Version:  " 1.0.0",
				})
				return err
			},
			wantErrMsg: "version must not contain leading or trailing whitespace",
		},
		{
			name: "policy valid request",
			query: func() error {
				_, err := queryServer.Policy(ctx, &types.QueryPolicyRequest{PolicyId: "policy-direct"})
				return err
			},
			wantErrMsg: "policies keeper not initialized",
		},
		{
			name: "policy zero keeper",
			query: func() error {
				_, err := zeroKeeperQueryServer.Policy(ctx, &types.QueryPolicyRequest{PolicyId: "policy-direct"})
				return err
			},
			wantErrMsg: "policies keeper not initialized",
		},
		{
			name: "policies nil request",
			query: func() error {
				_, err := queryServer.Policies(ctx, nil)
				return err
			},
			wantErrMsg: "request is nil",
		},
		{
			name: "policies invalid pagination",
			query: func() error {
				_, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
					Pagination: &query.PageRequest{
						Offset: 1,
						Key:    []byte("bad"),
					},
				})
				return err
			},
			wantErrMsg: "either offset or key",
		},
		{
			name: "policies all",
			query: func() error {
				_, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{})
				return err
			},
			wantErrMsg: "policies keeper not initialized",
		},
		{
			name: "policies by owner",
			query: func() error {
				_, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{Owner: "lumera1owner"})
				return err
			},
			wantErrMsg: "policies keeper not initialized",
		},
		{
			name: "policies by state",
			query: func() error {
				_, err := queryServer.Policies(ctx, &types.QueryPoliciesRequest{
					State: types.PolicyState_POLICY_STATE_ACTIVE,
				})
				return err
			},
			wantErrMsg: "policies keeper not initialized",
		},
		{
			name: "params nil request",
			query: func() error {
				_, err := queryServer.Params(ctx, nil)
				return err
			},
			wantErrMsg: "request is nil",
		},
		{
			name: "params valid request",
			query: func() error {
				_, err := queryServer.Params(ctx, &types.QueryParamsRequest{})
				return err
			},
			wantErrMsg: "policies keeper not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			require.NotPanics(t, func() {
				err = tt.query()
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErrMsg)
			if tt.wantErrMsg != "policies keeper not initialized" {
				require.NotContains(t, err.Error(), "keeper not initialized")
			}
		})
	}
}

func TestQueryServerParams(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	queryServer := NewQueryServer(k)

	resp, err := queryServer.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Params)
	require.Equal(t, types.DefaultParams().MinPolicyDeposit, resp.Params.MinPolicyDeposit)

	resp.Params.MinPolicyDeposit = "999999ulac"
	respAgain, err := queryServer.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, types.DefaultParams().MinPolicyDeposit, respAgain.Params.MinPolicyDeposit)
}

func queryTestPolicy(policyID, owner string, state types.PolicyState) *types.PolicyProfile {
	return &types.PolicyProfile{
		PolicyId:      policyID,
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Query Test Policy",
			Owner: owner,
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     state,
			CreatedBy: owner,
		},
	}
}

func queryPolicyIDs(policies []*types.PolicyProfile) []string {
	ids := make([]string, 0, len(policies))
	for _, policy := range policies {
		ids = append(ids, policy.PolicyId)
	}
	return ids
}
