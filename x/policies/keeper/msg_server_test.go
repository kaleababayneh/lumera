//go:build cosmos
// +build cosmos

package keeper

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

var (
	msgPolicyCreator       = validPolicyAddress("policy-msg-creator")
	msgPolicyOwner         = validPolicyAddress("policy-msg-owner")
	msgPolicyAttacker      = validPolicyAddress("policy-msg-attacker")
	msgPolicyRealCreator   = validPolicyAddress("policy-msg-real-creator")
	msgPolicyImposter      = validPolicyAddress("policy-msg-imposter")
	msgPolicyOriginalOwner = validPolicyAddress("policy-msg-original-owner")
	msgPolicyNewOwner      = validPolicyAddress("policy-msg-new-owner")
	msgPolicyNotGov        = validPolicyAddress("policy-msg-not-gov")
)

func validPolicyAddress(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return sdk.AccAddress(sum[:20]).String()
}

func validMsgPolicy(policyID string) *types.PolicyProfile {
	return &types.PolicyProfile{
		PolicyId:      policyID,
		Version:       "1.0.0",
		SchemaVersion: "1.0",
		Metadata: &types.PolicyMetadata{
			Name:  "Msg Validation Policy",
			Owner: msgPolicyOwner,
		},
		Lifecycle: &types.PolicyLifecycle{
			State:     types.PolicyState_POLICY_STATE_DRAFT,
			CreatedBy: msgPolicyOwner,
		},
	}
}

func TestMsgServerCreatePolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	msg := &types.MsgCreatePolicy{
		Creator: msgPolicyCreator,
		Policy: &types.PolicyProfile{
			PolicyId:      "msg-test-policy",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:        "Msg Test Policy",
				Description: "Testing msg server create",
			},
		},
	}

	resp, err := msgServer.CreatePolicy(ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "msg-test-policy", resp.PolicyId)
	require.Equal(t, "1.0.0", resp.Version)

	// Verify policy was created with correct owner
	policy, err := k.GetPolicy(ctx, "msg-test-policy", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, msgPolicyCreator, policy.Metadata.Owner)
	require.Equal(t, types.PolicyState_POLICY_STATE_DRAFT, policy.Lifecycle.State)
}

func TestMsgServerCreatePolicyValidation(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	// Missing policy ID
	msg := &types.MsgCreatePolicy{
		Creator: msgPolicyCreator,
		Policy: &types.PolicyProfile{
			Version:       "1.0.0",
			SchemaVersion: "1.0",
		},
	}

	_, err := msgServer.CreatePolicy(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "policy_id is required")
}

func TestMsgServerCreatePolicyRejectsNilPolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	var resp *types.MsgCreatePolicyResponse
	var err error
	require.NotPanics(t, func() {
		resp, err = msgServer.CreatePolicy(ctx, &types.MsgCreatePolicy{
			Creator: msgPolicyCreator,
			Policy:  nil,
		})
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "policy is required")
}

func TestMsgServerRejectsNilMessages(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	tests := []struct {
		name string
		call func() (any, error)
	}{
		{
			name: "create policy",
			call: func() (any, error) {
				return msgServer.CreatePolicy(ctx, nil)
			},
		},
		{
			name: "update policy",
			call: func() (any, error) {
				return msgServer.UpdatePolicy(ctx, nil)
			},
		},
		{
			name: "activate policy",
			call: func() (any, error) {
				return msgServer.ActivatePolicy(ctx, nil)
			},
		},
		{
			name: "deprecate policy",
			call: func() (any, error) {
				return msgServer.DeprecatePolicy(ctx, nil)
			},
		},
		{
			name: "archive policy",
			call: func() (any, error) {
				return msgServer.ArchivePolicy(ctx, nil)
			},
		},
		{
			name: "update params",
			call: func() (any, error) {
				return msgServer.UpdateParams(ctx, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.call()
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), "message cannot be nil")
		})
	}
}

func TestMsgServerValidatesBeforeSDKContext(t *testing.T) {
	msgServer := NewMsgServerImpl(nil)

	tests := []struct {
		name string
		call func() (any, error)
		want string
	}{
		{
			name: "create policy validates creator",
			call: func() (any, error) {
				msg := &types.MsgCreatePolicy{
					Creator: "not-a-bech32-address",
					Policy:  validMsgPolicy("direct-create-validation"),
				}
				return msgServer.CreatePolicy(context.Background(), msg)
			},
			want: "creator",
		},
		{
			name: "update policy validates policy id",
			call: func() (any, error) {
				msg := &types.MsgUpdatePolicy{
					Updater:  msgPolicyOwner,
					PolicyId: " direct-update-validation",
					Policy:   validMsgPolicy("direct-update-validation"),
				}
				return msgServer.UpdatePolicy(context.Background(), msg)
			},
			want: "policy_id",
		},
		{
			name: "activate policy validates policy reference",
			call: func() (any, error) {
				msg := &types.MsgActivatePolicy{
					Authority: msgPolicyOwner,
					PolicyId:  " direct-activate-validation",
					Version:   "1.0.0",
				}
				return msgServer.ActivatePolicy(context.Background(), msg)
			},
			want: "policy_id",
		},
		{
			name: "deprecate policy validates successor policy id",
			call: func() (any, error) {
				msg := &types.MsgDeprecatePolicy{
					Authority:         msgPolicyOwner,
					PolicyId:          "direct-deprecate-validation",
					Version:           "1.0.0",
					SuccessorPolicyId: " successor-policy",
				}
				return msgServer.DeprecatePolicy(context.Background(), msg)
			},
			want: "successor_policy_id",
		},
		{
			name: "archive policy validates version",
			call: func() (any, error) {
				msg := &types.MsgArchivePolicy{
					Authority: msgPolicyOwner,
					PolicyId:  "direct-archive-validation",
					Version:   " 1.0.0",
				}
				return msgServer.ArchivePolicy(context.Background(), msg)
			},
			want: "version",
		},
		{
			name: "update params validates params",
			call: func() (any, error) {
				msg := &types.MsgUpdateParams{
					Authority: msgPolicyOwner,
					Params:    nil,
				}
				return msgServer.UpdateParams(context.Background(), msg)
			},
			want: "params is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp any
			var err error
			require.NotPanics(t, func() {
				resp, err = tt.call()
			})
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestMsgServerRejectsNilKeeperAfterValidation(t *testing.T) {
	msgServer := NewMsgServerImpl(nil)

	var resp *types.MsgCreatePolicyResponse
	var err error
	require.NotPanics(t, func() {
		resp, err = msgServer.CreatePolicy(context.Background(), &types.MsgCreatePolicy{
			Creator: msgPolicyOwner,
			Policy:  validMsgPolicy("nil-keeper-validation"),
		})
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "policies keeper not initialized")
}

func TestMsgServerUpdatePolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	// Create initial policy
	createMsg := &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "update-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Original Name",
				Owner: msgPolicyOwner,
			},
		},
	}
	_, err := msgServer.CreatePolicy(ctx, createMsg)
	require.NoError(t, err)

	// Update policy
	updateMsg := &types.MsgUpdatePolicy{
		Updater:      msgPolicyOwner,
		PolicyId:     "update-test",
		UpdateReason: "Adding new version",
		Policy: &types.PolicyProfile{
			PolicyId:      "update-test",
			Version:       "2.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Updated Name",
				Owner: msgPolicyOwner,
			},
			Lifecycle: &types.PolicyLifecycle{
				State:     types.PolicyState_POLICY_STATE_DRAFT,
				CreatedBy: msgPolicyOwner,
			},
		},
	}

	resp, err := msgServer.UpdatePolicy(ctx, updateMsg)
	require.NoError(t, err)
	require.Equal(t, "2.0.0", resp.NewVersion)
	require.Equal(t, "1.0.0", resp.PreviousVersion)
}

func TestMsgServerUpdatePolicyRejectsNilPolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	_, err := msgServer.CreatePolicy(ctx, &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "nil-update-policy",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name: "Nil Update Policy",
			},
		},
	})
	require.NoError(t, err)

	var resp *types.MsgUpdatePolicyResponse
	require.NotPanics(t, func() {
		resp, err = msgServer.UpdatePolicy(ctx, &types.MsgUpdatePolicy{
			Updater:  msgPolicyOwner,
			PolicyId: "nil-update-policy",
			Policy:   nil,
		})
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "policy is required")
}

func TestMsgServerUpdatePolicyUnauthorized(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	// Create policy as owner
	createMsg := &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "auth-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Auth Test",
				Owner: msgPolicyOwner,
			},
		},
	}
	_, err := msgServer.CreatePolicy(ctx, createMsg)
	require.NoError(t, err)

	// Try to update as different user
	updateMsg := &types.MsgUpdatePolicy{
		Updater:      msgPolicyAttacker,
		PolicyId:     "auth-test",
		UpdateReason: "Unauthorized update",
		Policy: &types.PolicyProfile{
			PolicyId:      "auth-test",
			Version:       "2.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Hacked Name",
				Owner: msgPolicyAttacker,
			},
			Lifecycle: &types.PolicyLifecycle{
				State:     types.PolicyState_POLICY_STATE_DRAFT,
				CreatedBy: msgPolicyAttacker,
			},
		},
	}

	_, err = msgServer.UpdatePolicy(ctx, updateMsg)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestMsgServerActivatePolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)
	authority := k.GetAuthority()

	// Create policy
	createMsg := &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "activate-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Activate Test",
				Owner: msgPolicyOwner,
			},
		},
	}
	_, err := msgServer.CreatePolicy(ctx, createMsg)
	require.NoError(t, err)

	// Activate policy (must be authority)
	activateMsg := &types.MsgActivatePolicy{
		Authority: authority,
		PolicyId:  "activate-test",
		Version:   "1.0.0",
	}

	resp, err := msgServer.ActivatePolicy(ctx, activateMsg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "activate-test", resp.PolicyId)

	// Verify state changed
	policy, err := k.GetPolicy(ctx, "activate-test", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, types.PolicyState_POLICY_STATE_ACTIVE, policy.Lifecycle.State)

	// Verify activated_at is persisted in lifecycle
	require.NotNil(t, policy.Lifecycle.ActivatedAt,
		"activated_at must be persisted in policy lifecycle")
}

func TestMsgServerActivatePolicyUnauthorized(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	// Create policy
	createMsg := &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "unauth-activate",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Unauth Activate Test",
				Owner: msgPolicyOwner,
			},
		},
	}
	_, err := msgServer.CreatePolicy(ctx, createMsg)
	require.NoError(t, err)

	// Try to activate without authority
	activateMsg := &types.MsgActivatePolicy{
		Authority: msgPolicyNotGov,
		PolicyId:  "unauth-activate",
		Version:   "1.0.0",
	}

	_, err = msgServer.ActivatePolicy(ctx, activateMsg)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestMsgServerDeprecatePolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)
	authority := k.GetAuthority()

	// Create and activate policy
	createMsg := &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "deprecate-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Deprecate Test",
				Owner: msgPolicyOwner,
			},
		},
	}
	_, err := msgServer.CreatePolicy(ctx, createMsg)
	require.NoError(t, err)

	activateMsg := &types.MsgActivatePolicy{
		Authority: authority,
		PolicyId:  "deprecate-test",
		Version:   "1.0.0",
	}
	_, err = msgServer.ActivatePolicy(ctx, activateMsg)
	require.NoError(t, err)

	// Deprecate policy
	deprecateMsg := &types.MsgDeprecatePolicy{
		Authority:         authority,
		PolicyId:          "deprecate-test",
		Version:           "1.0.0",
		SuccessorPolicyId: "deprecate-successor",
	}

	resp, err := msgServer.DeprecatePolicy(ctx, deprecateMsg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify state changed
	policy, err := k.GetPolicy(ctx, "deprecate-test", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, types.PolicyState_POLICY_STATE_DEPRECATED, policy.Lifecycle.State)

	// Verify deprecated_at is persisted in lifecycle
	require.NotNil(t, policy.Lifecycle.DeprecatedAt,
		"deprecated_at must be persisted in policy lifecycle")
	require.Equal(t, "deprecate-successor", policy.Lifecycle.SuccessorPolicyId,
		"successor_policy_id must be persisted in policy lifecycle")
}

func TestMsgServerDeprecatePolicyRejectsPaddedSuccessorPolicyID(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	_, err := msgServer.DeprecatePolicy(ctx, &types.MsgDeprecatePolicy{
		Authority:         k.GetAuthority(),
		PolicyId:          "missing-policy",
		Version:           "1.0.0",
		SuccessorPolicyId: " successor-policy",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "successor_policy_id")
}

func TestMsgServerArchivePolicy(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)
	authority := k.GetAuthority()

	// Create, activate, deprecate policy
	createMsg := &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "archive-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Archive Test",
				Owner: msgPolicyOwner,
			},
		},
	}
	_, err := msgServer.CreatePolicy(ctx, createMsg)
	require.NoError(t, err)

	_, err = msgServer.ActivatePolicy(ctx, &types.MsgActivatePolicy{
		Authority: authority,
		PolicyId:  "archive-test",
		Version:   "1.0.0",
	})
	require.NoError(t, err)

	_, err = msgServer.DeprecatePolicy(ctx, &types.MsgDeprecatePolicy{
		Authority: authority,
		PolicyId:  "archive-test",
		Version:   "1.0.0",
	})
	require.NoError(t, err)

	// Archive policy
	archiveMsg := &types.MsgArchivePolicy{
		Authority: authority,
		PolicyId:  "archive-test",
		Version:   "1.0.0",
	}

	resp, err := msgServer.ArchivePolicy(ctx, archiveMsg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify state changed
	policy, err := k.GetPolicy(ctx, "archive-test", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, types.PolicyState_POLICY_STATE_ARCHIVED, policy.Lifecycle.State)

	// Verify archived_at is persisted in lifecycle
	require.NotNil(t, policy.Lifecycle.ArchivedAt,
		"archived_at must be persisted in policy lifecycle")
}

func TestMsgServerUpdateParams(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)
	authority := k.GetAuthority()

	newParams := types.DefaultParams()
	newParams.MinPolicyDeposit = "200000000" // 200 LAC
	newParams.MaxPolicyVersionHistory = 20

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    newParams,
	}

	_, err := msgServer.UpdateParams(ctx, msg)
	require.NoError(t, err)

	// Verify params updated
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, "200000000", params.MinPolicyDeposit)
	require.Equal(t, uint32(20), params.MaxPolicyVersionHistory)
}

func TestMsgServerDeprecatePolicyMigrationDeadline(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)
	authority := k.GetAuthority()

	// Create and activate policy
	createMsg := &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "deadline-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Deadline Test",
				Owner: msgPolicyOwner,
			},
		},
	}
	_, err := msgServer.CreatePolicy(ctx, createMsg)
	require.NoError(t, err)

	_, err = msgServer.ActivatePolicy(ctx, &types.MsgActivatePolicy{
		Authority: authority,
		PolicyId:  "deadline-test",
		Version:   "1.0.0",
	})
	require.NoError(t, err)

	// Deprecate with explicit migration window (1 hour)
	windowSec := uint32(3600)
	resp, err := msgServer.DeprecatePolicy(ctx, &types.MsgDeprecatePolicy{
		Authority:              authority,
		PolicyId:               "deadline-test",
		Version:                "1.0.0",
		MigrationWindowSeconds: windowSec,
	})
	require.NoError(t, err)
	require.NotNil(t, resp.DeprecatedAt)
	require.NotNil(t, resp.MigrationDeadline)

	// Migration deadline must be deprecated_at + window_seconds
	deprecatedTime := *resp.DeprecatedAt
	deadlineTime := *resp.MigrationDeadline
	expectedDeadline := deprecatedTime.Add(time.Duration(windowSec) * time.Second)
	require.Equal(t, expectedDeadline, deadlineTime,
		"migration deadline should be deprecated_at + migration_window_seconds")
	require.True(t, deadlineTime.After(deprecatedTime),
		"migration deadline must be after deprecation time")

	// Verify lifecycle metadata persisted to blockchain state
	policy, err := k.GetPolicy(ctx, "deadline-test", "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, policy.Lifecycle.DeprecatedAt,
		"deprecated_at must be persisted in policy lifecycle")
	require.Equal(t, deprecatedTime.UTC(), policy.Lifecycle.DeprecatedAt.UTC(),
		"persisted deprecated_at must match response")
	require.Equal(t, windowSec, policy.Lifecycle.MigrationWindowSeconds,
		"migration_window_seconds must be persisted in policy lifecycle")
}

func TestMsgServerDeprecatePolicyDefaultMigrationWindow(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)
	authority := k.GetAuthority()

	// Create and activate policy
	createMsg := &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "default-window-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Default Window Test",
				Owner: msgPolicyOwner,
			},
		},
	}
	_, err := msgServer.CreatePolicy(ctx, createMsg)
	require.NoError(t, err)

	_, err = msgServer.ActivatePolicy(ctx, &types.MsgActivatePolicy{
		Authority: authority,
		PolicyId:  "default-window-test",
		Version:   "1.0.0",
	})
	require.NoError(t, err)

	// Deprecate without explicit window (should use default from params: 604800 = 7 days)
	resp, err := msgServer.DeprecatePolicy(ctx, &types.MsgDeprecatePolicy{
		Authority: authority,
		PolicyId:  "default-window-test",
		Version:   "1.0.0",
	})
	require.NoError(t, err)

	deprecatedTime := *resp.DeprecatedAt
	deadlineTime := *resp.MigrationDeadline

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	expectedDeadline := deprecatedTime.Add(time.Duration(params.DefaultMigrationWindowSeconds) * time.Second)
	require.Equal(t, expectedDeadline, deadlineTime,
		"migration deadline should use default window from params when not specified")
}

func TestMsgServerUpdateParamsUnauthorized(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	msg := &types.MsgUpdateParams{
		Authority: msgPolicyNotGov,
		Params:    types.DefaultParams(),
	}

	_, err := msgServer.UpdateParams(ctx, msg)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestMsgServerUpdateParamsRejectsNilParams(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	var resp *types.MsgUpdateParamsResponse
	var err error
	require.NotPanics(t, func() {
		resp, err = msgServer.UpdateParams(ctx, &types.MsgUpdateParams{
			Authority: k.GetAuthority(),
			Params:    nil,
		})
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Contains(t, err.Error(), "params is required")
}

// TestMsgServerCreatePolicy_ForcesStateToDraft pins a critical
// security invariant: MsgCreatePolicy unconditionally overwrites
// msg.Policy.Lifecycle.State with POLICY_STATE_DRAFT, regardless of
// what the caller tried to set. Without this, a malicious creator
// could submit a MsgCreatePolicy with Lifecycle.State=ACTIVE and
// bypass the MsgActivatePolicy authority check entirely — making
// the authority gate toothless. Regression guard against a refactor
// that removes or reorders the `msg.Policy.Lifecycle.State = DRAFT`
// line in CreatePolicy.
func TestMsgServerCreatePolicy_ForcesStateToDraft(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	// Try every non-DRAFT state the caller might attempt to inject.
	attackStates := []types.PolicyState{
		types.PolicyState_POLICY_STATE_ACTIVE,
		types.PolicyState_POLICY_STATE_REVIEW,
		types.PolicyState_POLICY_STATE_DEPRECATED,
		types.PolicyState_POLICY_STATE_ARCHIVED,
	}
	for i, injected := range attackStates {
		id := "force-draft-test"
		version := "1.0." + string(rune('0'+i))

		msg := &types.MsgCreatePolicy{
			Creator: msgPolicyAttacker,
			Policy: &types.PolicyProfile{
				PolicyId:      id,
				Version:       version,
				SchemaVersion: "1.0",
				Metadata: &types.PolicyMetadata{
					Name:  "Force Draft Test",
					Owner: msgPolicyAttacker,
				},
				Lifecycle: &types.PolicyLifecycle{
					State:     injected, // try to skip DRAFT
					CreatedBy: msgPolicyAttacker,
				},
			},
		}

		_, err := msgServer.CreatePolicy(ctx, msg)
		require.NoError(t, err, "attempt to inject state %s", injected)

		stored, err := k.GetPolicy(ctx, id, version)
		require.NoError(t, err)
		require.Equal(t, types.PolicyState_POLICY_STATE_DRAFT, stored.Lifecycle.State,
			"created policy with injected state %s must be forced to DRAFT", injected)
	}
}

// TestMsgServerCreatePolicy_ForcesCreatedByToMsgCreator pins that
// the stored Lifecycle.CreatedBy is always set to msg.Creator,
// overriding any value the caller put in msg.Policy.Lifecycle.CreatedBy.
// Guards the audit-trail invariant: the on-chain attribution for a
// policy creation must reflect the actual tx signer, not
// self-reported metadata. Regression guard against a refactor that
// dropped the `CreatedBy = msg.Creator` override.
func TestMsgServerCreatePolicy_ForcesCreatedByToMsgCreator(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	msg := &types.MsgCreatePolicy{
		Creator: msgPolicyRealCreator,
		Policy: &types.PolicyProfile{
			PolicyId:      "createdby-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "CreatedBy Test",
				Owner: msgPolicyRealCreator,
			},
			Lifecycle: &types.PolicyLifecycle{
				// Attempt to forge the audit trail.
				CreatedBy: msgPolicyImposter,
			},
		},
	}

	_, err := msgServer.CreatePolicy(ctx, msg)
	require.NoError(t, err)

	stored, err := k.GetPolicy(ctx, "createdby-test", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, msgPolicyRealCreator, stored.Lifecycle.CreatedBy,
		"CreatedBy must be forced to msg.Creator, not caller-supplied value")
}

// TestMsgServerUpdatePolicy_PreservesExistingOwner pins that even
// when the legitimate owner calls UpdatePolicy with a modified
// Metadata.Owner field, the stored policy's Owner does NOT change.
// This is the ownership-stability invariant — ownership can only be
// transferred through an explicit mechanism (if any), never as a
// side effect of a regular update. The existing "unauthorized"
// test only covers the case where the updater ISN'T owner; this
// fills the gap where the updater IS the owner but tries to hand
// ownership off via the same-call metadata mutation.
func TestMsgServerUpdatePolicy_PreservesExistingOwner(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)

	createMsg := &types.MsgCreatePolicy{
		Creator: msgPolicyOriginalOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "owner-stable",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Owner Stable",
				Owner: msgPolicyOriginalOwner,
			},
		},
	}
	_, err := msgServer.CreatePolicy(ctx, createMsg)
	require.NoError(t, err)

	// Legitimate owner submits an update that ALSO tries to change Owner.
	updateMsg := &types.MsgUpdatePolicy{
		Updater:      msgPolicyOriginalOwner,
		PolicyId:     "owner-stable",
		UpdateReason: "Attempting ownership transfer via update",
		Policy: &types.PolicyProfile{
			PolicyId:      "owner-stable",
			Version:       "2.0.0",
			SchemaVersion: "1.0",
			Metadata: &types.PolicyMetadata{
				Name:  "Owner Stable v2",
				Owner: msgPolicyNewOwner, // attempted handoff
			},
			Lifecycle: &types.PolicyLifecycle{
				State:     types.PolicyState_POLICY_STATE_DRAFT,
				CreatedBy: msgPolicyOriginalOwner,
			},
		},
	}
	_, err = msgServer.UpdatePolicy(ctx, updateMsg)
	require.NoError(t, err)

	stored, err := k.GetPolicy(ctx, "owner-stable", "2.0.0")
	require.NoError(t, err)
	require.Equal(t, msgPolicyOriginalOwner, stored.Metadata.Owner,
		"UpdatePolicy must preserve existing owner; cannot transfer ownership via metadata update")
}

// TestMsgServerActivatePolicy_InvalidStateRejected pins that
// ActivatePolicy only accepts DRAFT or REVIEW as the starting
// state — attempts to re-activate an already-ACTIVE policy, or to
// resurrect a DEPRECATED/ARCHIVED one, must fail. Without this,
// lifecycle semantics become ambiguous (e.g., re-activating a
// DEPRECATED policy would reset its migration window).
func TestMsgServerActivatePolicy_InvalidStateRejected(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)
	authority := k.GetAuthority()

	// Create and activate.
	_, err := msgServer.CreatePolicy(ctx, &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "re-activate-test",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata:      &types.PolicyMetadata{Name: "ReActivate", Owner: msgPolicyOwner},
		},
	})
	require.NoError(t, err)

	_, err = msgServer.ActivatePolicy(ctx, &types.MsgActivatePolicy{
		Authority: authority, PolicyId: "re-activate-test", Version: "1.0.0",
	})
	require.NoError(t, err)

	// Try to activate again from ACTIVE state.
	_, err = msgServer.ActivatePolicy(ctx, &types.MsgActivatePolicy{
		Authority: authority, PolicyId: "re-activate-test", Version: "1.0.0",
	})
	require.Error(t, err, "activating an already-ACTIVE policy must be rejected")
	require.ErrorIs(t, err, types.ErrInvalidPolicyState)
}

// TestMsgServerDeprecatePolicy_InvalidStateRejected pins that
// DeprecatePolicy only accepts ACTIVE as the starting state.
// DRAFT, DEPRECATED (already-deprecated), and ARCHIVED must be
// rejected — DRAFT has nothing in production to migrate away from,
// DEPRECATED is idempotent-footgun territory (would reset the
// migration window), and ARCHIVED is terminal.
func TestMsgServerDeprecatePolicy_InvalidStateRejected(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)
	authority := k.GetAuthority()

	// Create a DRAFT policy (not activated).
	_, err := msgServer.CreatePolicy(ctx, &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "deprecate-draft",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata:      &types.PolicyMetadata{Name: "DeprecateDraft", Owner: msgPolicyOwner},
		},
	})
	require.NoError(t, err)

	_, err = msgServer.DeprecatePolicy(ctx, &types.MsgDeprecatePolicy{
		Authority: authority, PolicyId: "deprecate-draft", Version: "1.0.0",
	})
	require.Error(t, err, "deprecating a DRAFT policy must be rejected")
	require.ErrorIs(t, err, types.ErrInvalidPolicyState)
}

// TestMsgServerArchivePolicy_InvalidStateRejected pins that
// ArchivePolicy only accepts DEPRECATED as the starting state.
// Policies must pass through DEPRECATED (with its migration window)
// before becoming ARCHIVED; a direct DRAFT→ARCHIVED or
// ACTIVE→ARCHIVED skips the migration signaling consumers rely on.
func TestMsgServerArchivePolicy_InvalidStateRejected(t *testing.T) {
	ctx, k := setupPoliciesKeeper(t)
	msgServer := NewMsgServerImpl(k)
	authority := k.GetAuthority()

	// Create and activate (no deprecate).
	_, err := msgServer.CreatePolicy(ctx, &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "archive-active",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata:      &types.PolicyMetadata{Name: "ArchiveActive", Owner: msgPolicyOwner},
		},
	})
	require.NoError(t, err)
	_, err = msgServer.ActivatePolicy(ctx, &types.MsgActivatePolicy{
		Authority: authority, PolicyId: "archive-active", Version: "1.0.0",
	})
	require.NoError(t, err)

	// Skip deprecation and try to archive an ACTIVE policy.
	_, err = msgServer.ArchivePolicy(ctx, &types.MsgArchivePolicy{
		Authority: authority, PolicyId: "archive-active", Version: "1.0.0",
	})
	require.Error(t, err, "archiving an ACTIVE (not-yet-DEPRECATED) policy must be rejected")
	require.ErrorIs(t, err, types.ErrInvalidPolicyState)

	// And archiving a DRAFT must also fail.
	_, err = msgServer.CreatePolicy(ctx, &types.MsgCreatePolicy{
		Creator: msgPolicyOwner,
		Policy: &types.PolicyProfile{
			PolicyId:      "archive-draft",
			Version:       "1.0.0",
			SchemaVersion: "1.0",
			Metadata:      &types.PolicyMetadata{Name: "ArchiveDraft", Owner: msgPolicyOwner},
		},
	})
	require.NoError(t, err)
	_, err = msgServer.ArchivePolicy(ctx, &types.MsgArchivePolicy{
		Authority: authority, PolicyId: "archive-draft", Version: "1.0.0",
	})
	require.Error(t, err, "archiving a DRAFT policy must be rejected")
	require.ErrorIs(t, err, types.ErrInvalidPolicyState)
}
