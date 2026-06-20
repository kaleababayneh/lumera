
// Package keeper implements the policies module keeper and message server.
package keeper

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// timePtr returns a pointer to a copy of t for the gogoproto *time.Time fields.
func timePtr(t time.Time) *time.Time {
	return &t
}

var _ types.MsgServer = msgServer{}

type msgServer struct {
	types.UnimplementedMsgServer
	*Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface.
func NewMsgServerImpl(keeper *Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

func (m msgServer) requireKeeper() (*Keeper, error) {
	if m.Keeper == nil {
		return nil, fmt.Errorf("policies keeper not initialized")
	}
	return m.Keeper, nil
}

func recoverPolicies(action string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panic: %v", action, r)
	}
}

// CreatePolicy implements types.MsgServer.
func (m msgServer) CreatePolicy(ctx context.Context, msg *types.MsgCreatePolicy) (resp *types.MsgCreatePolicyResponse, err error) {
	defer recoverPolicies("policies/CreatePolicy", &err)
	if msg == nil {
		return nil, types.ErrInvalidPolicyID.Wrap("message cannot be nil")
	}
	if msg.Policy == nil {
		return nil, types.ErrInvalidPolicyID.Wrap("policy is required")
	}

	// Set default metadata if not provided
	if msg.Policy.Metadata == nil {
		msg.Policy.Metadata = &types.PolicyMetadata{}
	}
	if msg.Policy.Metadata.Owner == "" {
		msg.Policy.Metadata.Owner = msg.Creator
	}
	if msg.Policy.Metadata.Name == "" {
		msg.Policy.Metadata.Name = msg.Policy.PolicyId
	}

	// Set initial lifecycle state and force DRAFT to prevent unauthorized activation
	if msg.Policy.Lifecycle == nil {
		msg.Policy.Lifecycle = &types.PolicyLifecycle{}
	}
	msg.Policy.Lifecycle.State = types.PolicyState_POLICY_STATE_DRAFT
	msg.Policy.Lifecycle.CreatedBy = msg.Creator

	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	msg.Policy.Lifecycle.CreatedAt = timePtr(sdkCtx.BlockTime())

	// Validate the policy after setting block-time derived metadata.
	if err := types.ValidatePolicy(msg.Policy); err != nil {
		return nil, err
	}

	// Create the policy
	if err := keeper.CreatePolicy(ctx, msg.Policy); err != nil {
		return nil, err
	}

	return &types.MsgCreatePolicyResponse{
		PolicyId:   msg.Policy.PolicyId,
		Version:    msg.Policy.Version,
		PolicyHash: msg.Policy.PolicyHash,
	}, nil
}

// UpdatePolicy implements types.MsgServer.
func (m msgServer) UpdatePolicy(ctx context.Context, msg *types.MsgUpdatePolicy) (resp *types.MsgUpdatePolicyResponse, err error) {
	defer recoverPolicies("policies/UpdatePolicy", &err)
	if msg == nil {
		return nil, types.ErrInvalidPolicyID.Wrap("message cannot be nil")
	}
	if msg.Policy == nil {
		return nil, types.ErrInvalidPolicyID.Wrap("policy is required")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Get existing policy to verify ownership
	existing, err := keeper.GetPolicy(ctx, msg.PolicyId, "")
	if err != nil {
		return nil, err
	}

	// Check authorization - must be owner or have approval
	if existing.Metadata == nil {
		return nil, types.ErrInvalidPolicyID.Wrapf("policy %s has no metadata", msg.PolicyId)
	}
	if existing.Metadata.Owner != msg.Updater {
		return nil, types.ErrUnauthorized.Wrapf("only owner %s can update policy", existing.Metadata.Owner)
	}

	// Enforce metadata owner remains the same
	if msg.Policy.Metadata == nil {
		msg.Policy.Metadata = &types.PolicyMetadata{}
	}
	msg.Policy.Metadata.Owner = existing.Metadata.Owner

	// Force state to DRAFT for the new version
	if msg.Policy.Lifecycle == nil {
		msg.Policy.Lifecycle = &types.PolicyLifecycle{}
	}
	msg.Policy.Lifecycle.State = types.PolicyState_POLICY_STATE_DRAFT
	msg.Policy.Lifecycle.CreatedBy = msg.Updater

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	msg.Policy.Lifecycle.CreatedAt = timePtr(sdkCtx.BlockTime())

	previousVersion := existing.Version

	// Update the policy
	if err := keeper.UpdatePolicy(ctx, msg.Updater, msg.Policy, msg.UpdateReason); err != nil {
		return nil, err
	}

	return &types.MsgUpdatePolicyResponse{
		PolicyId:        msg.PolicyId,
		NewVersion:      msg.Policy.Version,
		PreviousVersion: previousVersion,
		PolicyHash:      msg.Policy.PolicyHash,
	}, nil
}

// ActivatePolicy implements types.MsgServer.
func (m msgServer) ActivatePolicy(ctx context.Context, msg *types.MsgActivatePolicy) (resp *types.MsgActivatePolicyResponse, err error) {
	defer recoverPolicies("policies/ActivatePolicy", &err)
	if msg == nil {
		return nil, types.ErrInvalidPolicyID.Wrap("message cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority
	if keeper.GetAuthority() != msg.Authority {
		return nil, types.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.GetAuthority(), msg.Authority)
	}

	// Get policy
	policy, err := keeper.GetPolicy(ctx, msg.PolicyId, msg.Version)
	if err != nil {
		return nil, err
	}

	// Validate state transition
	if policy.Lifecycle == nil {
		return nil, types.ErrInvalidPolicyState.Wrapf("policy %s:%s has no lifecycle metadata", msg.PolicyId, msg.Version)
	}
	if policy.Lifecycle.State != types.PolicyState_POLICY_STATE_DRAFT &&
		policy.Lifecycle.State != types.PolicyState_POLICY_STATE_REVIEW {
		return nil, types.ErrInvalidPolicyState.Wrapf(
			"cannot activate policy in state %s", policy.Lifecycle.State.String())
	}

	// Update state
	if err := keeper.SetPolicyState(ctx, msg.PolicyId, msg.Version, types.PolicyState_POLICY_STATE_ACTIVE); err != nil {
		return nil, err
	}

	activatedAt := sdkCtx.BlockTime()

	// Persist activated_at to lifecycle
	policy, err = keeper.GetPolicy(ctx, msg.PolicyId, msg.Version)
	if err != nil {
		return nil, err
	}
	policy.Lifecycle.ActivatedAt = timePtr(activatedAt)
	if err := keeper.state.Policies.Set(sdkCtx, msg.PolicyId+":"+msg.Version, policy); err != nil {
		return nil, err
	}

	return &types.MsgActivatePolicyResponse{
		PolicyId:    msg.PolicyId,
		Version:     msg.Version,
		ActivatedAt: timePtr(activatedAt),
	}, nil
}

// DeprecatePolicy implements types.MsgServer.
func (m msgServer) DeprecatePolicy(ctx context.Context, msg *types.MsgDeprecatePolicy) (resp *types.MsgDeprecatePolicyResponse, err error) {
	defer recoverPolicies("policies/DeprecatePolicy", &err)
	if msg == nil {
		return nil, types.ErrInvalidPolicyID.Wrap("message cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority
	if keeper.GetAuthority() != msg.Authority {
		return nil, types.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.GetAuthority(), msg.Authority)
	}

	// Get policy
	policy, err := keeper.GetPolicy(ctx, msg.PolicyId, msg.Version)
	if err != nil {
		return nil, err
	}

	// Validate state transition
	if policy.Lifecycle == nil {
		return nil, types.ErrInvalidPolicyState.Wrapf("policy %s:%s has no lifecycle metadata", msg.PolicyId, msg.Version)
	}
	if policy.Lifecycle.State != types.PolicyState_POLICY_STATE_ACTIVE {
		return nil, types.ErrInvalidPolicyState.Wrapf(
			"cannot deprecate policy in state %s", policy.Lifecycle.State.String())
	}

	// Update state
	if err := keeper.SetPolicyState(ctx, msg.PolicyId, msg.Version, types.PolicyState_POLICY_STATE_DEPRECATED); err != nil {
		return nil, err
	}

	deprecatedAt := sdkCtx.BlockTime()

	// Calculate migration deadline
	params, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get params: %w", err)
	}

	migrationWindow := msg.MigrationWindowSeconds
	if migrationWindow == 0 {
		migrationWindow = params.DefaultMigrationWindowSeconds
	}

	migrationDeadline := deprecatedAt.Add(time.Duration(migrationWindow) * time.Second)

	// Persist deprecated_at and migration_window_seconds to lifecycle
	policy, err = keeper.GetPolicy(ctx, msg.PolicyId, msg.Version)
	if err != nil {
		return nil, err
	}
	policy.Lifecycle.DeprecatedAt = timePtr(deprecatedAt)
	policy.Lifecycle.MigrationWindowSeconds = migrationWindow
	policy.Lifecycle.SuccessorPolicyId = msg.SuccessorPolicyId
	if err := keeper.state.Policies.Set(sdkCtx, msg.PolicyId+":"+msg.Version, policy); err != nil {
		return nil, err
	}

	return &types.MsgDeprecatePolicyResponse{
		PolicyId:          msg.PolicyId,
		Version:           msg.Version,
		DeprecatedAt:      timePtr(deprecatedAt),
		MigrationDeadline: timePtr(migrationDeadline),
	}, nil
}

// ArchivePolicy implements types.MsgServer.
func (m msgServer) ArchivePolicy(ctx context.Context, msg *types.MsgArchivePolicy) (resp *types.MsgArchivePolicyResponse, err error) {
	defer recoverPolicies("policies/ArchivePolicy", &err)
	if msg == nil {
		return nil, types.ErrInvalidPolicyID.Wrap("message cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check authority
	if keeper.GetAuthority() != msg.Authority {
		return nil, types.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.GetAuthority(), msg.Authority)
	}

	// Get policy
	policy, err := keeper.GetPolicy(ctx, msg.PolicyId, msg.Version)
	if err != nil {
		return nil, err
	}

	// Validate state transition
	if policy.Lifecycle == nil {
		return nil, types.ErrInvalidPolicyState.Wrapf("policy %s:%s has no lifecycle metadata", msg.PolicyId, msg.Version)
	}
	if policy.Lifecycle.State != types.PolicyState_POLICY_STATE_DEPRECATED {
		return nil, types.ErrInvalidPolicyState.Wrapf(
			"cannot archive policy in state %s", policy.Lifecycle.State.String())
	}

	// Update state
	if err := keeper.SetPolicyState(ctx, msg.PolicyId, msg.Version, types.PolicyState_POLICY_STATE_ARCHIVED); err != nil {
		return nil, err
	}

	archivedAt := sdkCtx.BlockTime()

	// Persist archived_at to lifecycle
	policy, err = keeper.GetPolicy(ctx, msg.PolicyId, msg.Version)
	if err != nil {
		return nil, err
	}
	policy.Lifecycle.ArchivedAt = timePtr(archivedAt)
	if err := keeper.state.Policies.Set(sdkCtx, msg.PolicyId+":"+msg.Version, policy); err != nil {
		return nil, err
	}

	return &types.MsgArchivePolicyResponse{
		PolicyId:   msg.PolicyId,
		Version:    msg.Version,
		ArchivedAt: timePtr(archivedAt),
	}, nil
}

// UpdateParams implements types.MsgServer.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (resp *types.MsgUpdateParamsResponse, err error) {
	defer recoverPolicies("policies/UpdateParams", &err)
	if msg == nil {
		return nil, types.ErrInvalidParams.Wrap("message cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, types.ErrInvalidParams.Wrap(err.Error())
	}
	keeper, err := m.requireKeeper()
	if err != nil {
		return nil, err
	}
	// Check authority
	if keeper.GetAuthority() != msg.Authority {
		return nil, types.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.GetAuthority(), msg.Authority)
	}
	if err := msg.Params.Validate(); err != nil {
		return nil, types.ErrInvalidParams.Wrap(err.Error())
	}

	// Set params
	if err := keeper.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"update_params",
			sdk.NewAttribute("authority", msg.Authority),
		),
	)

	return &types.MsgUpdateParamsResponse{}, nil
}
