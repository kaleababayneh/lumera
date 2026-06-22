//go:build cosmos
// +build cosmos

package keeper

import (
	"strings"
	"testing"
	"time"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// tsPtr returns a pointer to t, matching the gogoproto *time.Time fields.
func tsPtr(t time.Time) *time.Time { return &t }

func setupPoliciesKeeper(t *testing.T) (sdk.Context, *Keeper) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)

	keeper := NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		authtypes.NewModuleAddress("gov").String(),
	)

	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	require.NoError(t, keeper.SetParams(ctx, types.DefaultParams()))

	return ctx, keeper
}

func TestGetSetParams(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.NotNil(t, params)
	require.Equal(t, types.DefaultParams().MinPolicyDeposit, params.MinPolicyDeposit)
}

func TestSetParamsRejectsInvalidModuleParams(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	params := types.DefaultParams()
	params.DefaultAuditRetentionDays = 0
	err := k.SetParams(ctx, params)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidParams)
}

func TestCreatePolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId:      "test-policy-1",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:        "Test Policy",
			Description: "A test policy for unit testing",
			Owner:       "lumera1testowner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: "lumera1testowner",
		},
	}

	err := k.CreatePolicy(ctx, policy)
	require.NoError(t, err)

	// Retrieve the policy
	retrieved, err := k.GetPolicy(ctx, "test-policy-1", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "test-policy-1", retrieved.PolicyId)
	require.Equal(t, "1.0.0", retrieved.Version)
	require.Equal(t, "Test Policy", retrieved.Metadata.Name)
}

func TestCreatePolicyDuplicateVersion(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId:      "dup-policy",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Duplicate Policy",
			Owner: "lumera1testowner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: "lumera1testowner",
		},
	}

	err := k.CreatePolicy(ctx, policy)
	require.NoError(t, err)

	// Try to create the same policy again
	err = k.CreatePolicy(ctx, policy)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrPolicyAlreadyExists)
}

func TestGetPolicyLatestVersion(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	// Create v1.0.0
	policy1 := &types.PolicyProfile{
		PolicyId:      "versioned-policy",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Versioned Policy v1",
			Owner: "lumera1testowner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: "lumera1testowner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy1))

	// Create v2.0.0
	policy2 := &types.PolicyProfile{
		PolicyId:      "versioned-policy",
		Version:       "2.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Versioned Policy v2",
			Owner: "lumera1testowner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: "lumera1testowner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy2))

	// Get latest (should be v2.0.0)
	latest, err := k.GetPolicy(ctx, "versioned-policy", "")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", latest.Version)
	require.Equal(t, "Versioned Policy v2", latest.Metadata.Name)

	// Get specific version
	v1, err := k.GetPolicy(ctx, "versioned-policy", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "1.0.0", v1.Version)
}

func TestGetPolicyNotFound(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	_, err := k.GetPolicy(ctx, "nonexistent", "1.0.0")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrPolicyNotFound)
}

func TestSetPolicyState(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId:      "state-test",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "State Test Policy",
			Owner: "lumera1testowner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: "lumera1testowner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Change state to ACTIVE
	err := k.SetPolicyState(ctx, "state-test", "1.0.0", types.PolicyState_POLICY_STATE_ACTIVE)
	require.NoError(t, err)

	// Verify state changed
	retrieved, err := k.GetPolicy(ctx, "state-test", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, types.PolicyState_POLICY_STATE_ACTIVE, retrieved.Lifecycle.State)
}

func TestListPoliciesByOwner(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	owner1 := "lumera1owner1"
	owner2 := "lumera1owner2"

	// Create policies for owner1
	for i := 1; i <= 3; i++ {
		policy := &types.PolicyProfile{
			PolicyId:      "owner1-policy-" + string(rune('0'+i)),
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Owner1 Policy",
				Owner: owner1,
			},
			Lifecycle: &types.PolicyLifecycle{
				State:     types.PolicyState_POLICY_STATE_DRAFT,
				CreatedBy: owner1,
			},
		}
		require.NoError(t, k.CreatePolicy(ctx, policy))
	}

	// Create policy for owner2
	policy := &types.PolicyProfile{
		PolicyId:      "owner2-policy-1",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Owner2 Policy",
			Owner: owner2,
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: owner2,
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// List owner1's policies
	owner1Policies, err := k.ListPoliciesByOwner(ctx, owner1)
	require.NoError(t, err)
	require.Len(t, owner1Policies, 3)

	// List owner2's policies
	owner2Policies, err := k.ListPoliciesByOwner(ctx, owner2)
	require.NoError(t, err)
	require.Len(t, owner2Policies, 1)
}

func TestListPoliciesByState(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	// Create policies in different states
	states := []types.PolicyState{
		types.PolicyState_POLICY_STATE_DRAFT,
		types.PolicyState_POLICY_STATE_DRAFT,
		types.PolicyState_POLICY_STATE_ACTIVE,
	}

	for i, state := range states {
		policy := &types.PolicyProfile{
			PolicyId:      "state-policy-" + string(rune('0'+i)),
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "State Policy",
				Owner: "lumera1testowner",
			},
			Lifecycle: &types.PolicyLifecycle{
				State:     state,
				CreatedBy: "lumera1testowner",
			},
		}
		require.NoError(t, k.CreatePolicy(ctx, policy))
	}

	// List DRAFT policies
	draftPolicies, err := k.ListPoliciesByState(ctx, types.PolicyState_POLICY_STATE_DRAFT)
	require.NoError(t, err)
	require.Len(t, draftPolicies, 2)

	// List ACTIVE policies
	activePolicies, err := k.ListPoliciesByState(ctx, types.PolicyState_POLICY_STATE_ACTIVE)
	require.NoError(t, err)
	require.Len(t, activePolicies, 1)
}

func TestRecordAuditEntry(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	entry := &types.PolicyAuditEntry{
		AuditId:       "audit-001",
		PolicyId:      "test-policy",
		PolicyVersion: "1.0.0",
		UserId:        "lumera1user",
		ToolId:        "tool-alpha",
		Action:        "invoke",
		Allowed:       true,
	}

	err := k.RecordAuditEntry(ctx, entry)
	require.NoError(t, err)
}

func TestRollbackPolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	// Create v1.0.0
	policy1 := &types.PolicyProfile{
		PolicyId:      "rollback-test",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Rollback Test v1",
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
		PolicyId:      "rollback-test",
		Version:       "2.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Rollback Test v2",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy2))

	// Verify v2.0.0 is latest
	latest, err := k.GetPolicy(ctx, "rollback-test", "")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", latest.Version)

	// Rollback to v1.0.0
	err = k.RollbackPolicy(ctx, "rollback-test", "1.0.0", "Reverting to stable version", "lumera1admin")
	require.NoError(t, err)

	// Verify v1.0.0 is now latest
	latest, err = k.GetPolicy(ctx, "rollback-test", "")
	require.NoError(t, err)
	require.Equal(t, "1.0.0", latest.Version)
	require.Equal(t, "Rollback Test v1", latest.Metadata.Name)
}

func TestRollbackPolicyToSameVersion(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId:      "same-version-test",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Same Version Test",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Try to rollback to current version
	err := k.RollbackPolicy(ctx, "same-version-test", "1.0.0", "No-op rollback", "lumera1admin")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidPolicyVersion)
}

func TestRollbackPolicyNotFound(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy := &types.PolicyProfile{
		PolicyId:      "notfound-test",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Not Found Test",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy))

	// Try to rollback to non-existent version
	err := k.RollbackPolicy(ctx, "notfound-test", "9.9.9", "Bad rollback", "lumera1admin")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrPolicyNotFound)
}

func TestListPolicyVersions(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	// Create multiple versions
	versions := []string{"1.0.0", "1.1.0", "2.0.0"}
	for _, v := range versions {
		policy := &types.PolicyProfile{
			PolicyId:      "multi-version",
			Version:       v,
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Version " + v,
				Owner: "lumera1owner",
			},
			Lifecycle: &types.PolicyLifecycle{
				State:     types.PolicyState_POLICY_STATE_ACTIVE,
				CreatedBy: "lumera1owner",
			},
		}
		require.NoError(t, k.CreatePolicy(ctx, policy))
	}

	// List versions
	allVersions, err := k.ListPolicyVersions(ctx, "multi-version")
	require.NoError(t, err)
	require.Len(t, allVersions, 3)

	// Verify all versions are present
	foundVersions := make(map[string]bool)
	for _, p := range allVersions {
		foundVersions[p.Version] = true
	}
	for _, v := range versions {
		require.True(t, foundVersions[v], "version %s not found", v)
	}
}

func TestListPolicyVersionsNotFound(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	_, err := k.ListPolicyVersions(ctx, "nonexistent-policy")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrPolicyNotFound)
}

func TestGetPolicyUpdateHistory(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	// Create initial policy
	policy1 := &types.PolicyProfile{
		PolicyId:      "history-test",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "History Test v1",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy1))

	// Update to v2.0.0
	policy2 := &types.PolicyProfile{
		PolicyId:      "history-test",
		Version:       "2.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "History Test v2",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.UpdatePolicy(ctx, "lumera1owner", policy2, "Adding new features"))

	// Rollback to v1.0.0
	require.NoError(t, k.RollbackPolicy(ctx, "history-test", "1.0.0", "Reverting", "lumera1admin"))

	// Get history
	history, err := k.GetPolicyUpdateHistory(ctx, "history-test")
	require.NoError(t, err)
	require.Len(t, history, 2) // One update, one rollback

	// Verify rollback is recorded
	hasRollback := false
	for _, h := range history {
		if h.UpdateReason == "ROLLBACK: Reverting" {
			hasRollback = true
			require.Equal(t, "1.0.0", h.Version)
			require.Equal(t, "2.0.0", h.PreviousVersion)
		}
	}
	require.True(t, hasRollback, "rollback not found in history")
}

func TestUpdatePolicyRejectsInvalidUpdateReason(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policy1 := &types.PolicyProfile{
		PolicyId:      "reason-test",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Reason Test v1",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policy1))

	tests := []struct {
		name   string
		reason string
	}{
		{
			name:   "leading space",
			reason: " governance update",
		},
		{
			name:   "trailing space",
			reason: "governance update ",
		},
		{
			name:   "oversized",
			reason: strings.Repeat("a", types.MaxPolicyUpdateReasonLen+1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy2 := &types.PolicyProfile{
				PolicyId:      "reason-test",
				Version:       "2.0.0",
				SchemaVersion: "1.0",
				Metadata: &types.PolicyMetadata{
					Name:  "Reason Test v2",
					Owner: "lumera1owner",
				},
				Lifecycle: &types.PolicyLifecycle{
					State:     types.PolicyState_POLICY_STATE_ACTIVE,
					CreatedBy: "lumera1owner",
				},
			}

			err := k.UpdatePolicy(ctx, "lumera1owner", policy2, tt.reason)
			require.Error(t, err)
			require.ErrorContains(t, err, "update_reason")

			_, getErr := k.GetPolicy(ctx, "reason-test", "2.0.0")
			require.Error(t, getErr)
			require.ErrorIs(t, getErr, types.ErrPolicyNotFound)
		})
	}
}

// ---------------------------------------------------------------------------
// ImportState / ExportState genesis round-trip
// ---------------------------------------------------------------------------

func TestImportExportStateRoundTrip(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	// Seed two policies — one with two versions (via CreatePolicy + UpdatePolicy).
	policyA := &types.PolicyProfile{
		PolicyId:      "policy-alpha",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Alpha Policy v1",
			Owner: "lumera1alice",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1alice",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policyA))

	policyA2 := &types.PolicyProfile{
		PolicyId:      "policy-alpha",
		Version:       "2.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Alpha Policy v2",
			Owner: "lumera1alice",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1alice",
		},
	}
	require.NoError(t, k.UpdatePolicy(ctx, "lumera1alice", policyA2, "Improvement"))

	policyB := &types.PolicyProfile{
		PolicyId:      "policy-beta",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Beta Policy",
			Owner: "lumera1bob",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: "lumera1bob",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policyB))

	// Export genesis state.
	exported, err := k.ExportState(ctx)
	require.NoError(t, err)
	require.NotNil(t, exported)

	// Verify exported content.
	require.NotNil(t, exported.Params)
	require.Equal(t, types.DefaultParams().MinPolicyDeposit, exported.Params.MinPolicyDeposit)
	require.Len(t, exported.Policies, 3) // alpha v1, alpha v2, beta v1
	require.Len(t, exported.PolicyUpdates, 1)
	require.Greater(t, exported.PolicyUpdateCounter, uint64(0))

	// Import into a fresh keeper.
	ctx2, k2 := setupPoliciesKeeper(t)
	require.NoError(t, k2.ImportState(ctx2, exported))

	// Verify all policies survived the round-trip.
	alphaV1, err := k2.GetPolicy(ctx2, "policy-alpha", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "Alpha Policy v1", alphaV1.Metadata.Name)

	alphaV2, err := k2.GetPolicy(ctx2, "policy-alpha", "2.0.0")
	require.NoError(t, err)
	require.Equal(t, "Alpha Policy v2", alphaV2.Metadata.Name)

	// Latest version pointer should point to 2.0.0.
	alphaLatest, err := k2.GetPolicy(ctx2, "policy-alpha", "")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", alphaLatest.Version)

	betaV1, err := k2.GetPolicy(ctx2, "policy-beta", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "Beta Policy", betaV1.Metadata.Name)

	// Verify policy update history survived.
	updates, err := k2.GetPolicyUpdateHistory(ctx2, "policy-alpha")
	require.NoError(t, err)
	require.NotEmpty(t, updates)

	// Verify re-export produces equivalent state.
	reExported, err := k2.ExportState(ctx2)
	require.NoError(t, err)
	require.Len(t, reExported.Policies, 3)
	require.Len(t, reExported.PolicyUpdates, len(exported.PolicyUpdates))
}

func TestImportStateFloorsPolicyUpdateCounterToImportedHistory(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	policyV1 := &types.PolicyProfile{
		PolicyId:      "counter-floor",
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Counter Floor v1",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.CreatePolicy(ctx, policyV1))

	policyV2 := &types.PolicyProfile{
		PolicyId:      "counter-floor",
		Version:       "2.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Counter Floor v2",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k.UpdatePolicy(ctx, "lumera1owner", policyV2, "second version"))

	exported, err := k.ExportState(ctx)
	require.NoError(t, err)
	require.Len(t, exported.PolicyUpdates, 1)
	exported.PolicyUpdateCounter = 0

	ctx2, k2 := setupPoliciesKeeper(t)
	require.NoError(t, k2.ImportState(ctx2, exported))

	policyV3 := &types.PolicyProfile{
		PolicyId:      "counter-floor",
		Version:       "3.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Counter Floor v3",
			Owner: "lumera1owner",
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_ACTIVE,
			CreatedBy: "lumera1owner",
		},
	}
	require.NoError(t, k2.UpdatePolicy(ctx2, "lumera1owner", policyV3, "third version"))

	history, err := k2.GetPolicyUpdateHistory(ctx2, "counter-floor")
	require.NoError(t, err)
	require.Len(t, history, 2)
}

func TestImportExportStateEmpty(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	genesis := types.DefaultGenesis()
	require.NoError(t, k.ImportState(ctx, genesis))

	exported, err := k.ExportState(ctx)
	require.NoError(t, err)
	require.NotNil(t, exported.Params)
	require.Empty(t, exported.Policies)
	require.Empty(t, exported.PolicyUpdates)
	require.Equal(t, uint64(0), exported.PolicyUpdateCounter)
}

func TestImportStateNilGenesisUsesDefaults(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	require.NoError(t, k.ImportState(ctx, nil))

	exported, err := k.ExportState(ctx)
	require.NoError(t, err)
	require.True(t, proto.Equal(types.DefaultParams(), exported.Params))
	require.Empty(t, exported.Policies)
	require.Empty(t, exported.PolicyUpdates)
	require.Equal(t, uint64(0), exported.PolicyUpdateCounter)
}

func TestImportStateInvalidParamsReturnsError(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	params := types.DefaultParams()
	params.MinPolicyDeposit = "bad"
	genesis := &types.GenesisState{Params: params}
	require.Error(t, k.ImportState(ctx, genesis))
}

func TestImportStateRejectsNilParams(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	genesis := &types.GenesisState{Policies: []*types.PolicyProfile{}}
	err := k.ImportState(ctx, genesis)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid policies genesis")
	require.Contains(t, err.Error(), "params are required")
}

func TestImportStateInvalidPolicyReturnsError(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Policies: []*types.PolicyProfile{
			{PolicyId: "", Version: "1.0.0"}, // missing PolicyId
		},
	}
	require.Error(t, k.ImportState(ctx, genesis))
}

func TestImportStateRejectsOutOfOrderLifecycleTimestamp(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	base := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)

	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Policies: []*types.PolicyProfile{
			{
				PolicyId:      "policy-alpha",
				Version:       "1.0.0",
				SchemaVersion: "1.0",
				Metadata: &types.PolicyMetadata{
					Name:  "Alpha Policy",
					Owner: "lumera1owner",
				},
				Lifecycle: &types.PolicyLifecycle{
					State:        types.PolicyState_POLICY_STATE_DEPRECATED,
					CreatedBy:    "lumera1owner",
					CreatedAt:    tsPtr(base),
					ActivatedAt:  tsPtr(base.Add(time.Hour)),
					DeprecatedAt: tsPtr(base.Add(-time.Second)),
				},
			},
		},
	}

	err := k.ImportState(ctx, genesis)
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid policies genesis")
	require.ErrorContains(t, err, "lifecycle.deprecated_at cannot be before lifecycle.created_at")

	_, err = k.GetPolicy(ctx, "policy-alpha", "1.0.0")
	require.Error(t, err)
}

func TestImportStateRejectsUncanonicalPolicyIdentity(t *testing.T) {
	tests := []struct {
		name      string
		policyID  string
		version   string
		wantField string
	}{
		{
			name:      "policy_id",
			policyID:  " policy-alpha",
			version:   "1.0.0",
			wantField: "policy_id must not contain leading or trailing whitespace",
		},
		{
			name:      "version",
			policyID:  "policy-alpha",
			version:   "1.0.0 ",
			wantField: "version must not contain leading or trailing whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, k := setupPoliciesKeeper(t)
			genesis := &types.GenesisState{
				Params: types.DefaultParams(),
				Policies: []*types.PolicyProfile{
					{
						PolicyId:      tt.policyID,
						Version:       tt.version,
						SchemaVersion: "1.0",
						Metadata: &types.PolicyMetadata{
							Name:  "Alpha Policy",
							Owner: "lumera1owner",
						},
						Lifecycle: &types.PolicyLifecycle{
							State:     types.PolicyState_POLICY_STATE_ACTIVE,
							CreatedBy: "lumera1owner",
						},
					},
				},
			}

			err := k.ImportState(ctx, genesis)
			require.Error(t, err)
			require.ErrorContains(t, err, "invalid policies genesis")
			require.ErrorContains(t, err, tt.wantField)
		})
	}
}

func TestImportStateRejectsDanglingPolicyUpdateHistory(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Policies: []*types.PolicyProfile{
			{
				PolicyId:      "policy-alpha",
				Version:       "1.0.0",
				SchemaVersion: "1.0",
				Metadata: &types.PolicyMetadata{
					Name:  "Alpha Policy v1",
					Owner: "lumera1owner",
				},
				Lifecycle: &types.PolicyLifecycle{
					State:     types.PolicyState_POLICY_STATE_ACTIVE,
					CreatedBy: "lumera1owner",
				},
			},
		},
		PolicyUpdates: []*types.PolicyUpdate{
			{
				PolicyId:        "policy-alpha",
				Version:         "2.0.0",
				PreviousVersion: "1.0.0",
				Updater:         "lumera1owner",
			},
		},
	}

	err := k.ImportState(ctx, genesis)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid policies genesis")
	require.Contains(t, err.Error(), "policy update policy-alpha:2.0.0")
	require.Contains(t, err.Error(), "unknown policy version")
}

func TestImportStatePolicyByOwnerIndex(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)

	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Policies: []*types.PolicyProfile{
			{
				PolicyId:      "idx-test",
				Version:       "1.0.0",
				SchemaVersion: "1.0",
				Metadata: &types.PolicyMetadata{
					Name:  "Index Test",
					Owner: "lumera1owner",
				},
				Lifecycle: &types.PolicyLifecycle{
					State:     types.PolicyState_POLICY_STATE_ACTIVE,
					CreatedBy: "lumera1owner",
				},
			},
		},
	}
	require.NoError(t, k.ImportState(ctx, genesis))

	// Verify the owner index was rebuilt — ListPoliciesByOwner should return the policy.
	policies, err := k.ListPoliciesByOwner(ctx, "lumera1owner")
	require.NoError(t, err)
	require.Len(t, policies, 1)
	require.Equal(t, "idx-test", policies[0].PolicyId)
}
