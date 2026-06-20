
package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// ConsensusVersion defines the current x/policies module consensus version.
const ConsensusVersion uint64 = 1

// State encapsulates the policies module collections state.
type State struct {
	Schema              collections.Schema
	Params              collections.Item[*types.Params]
	Policies            collections.Map[string, *types.PolicyProfile] // key: policyID:version
	PolicyVersions      collections.Map[string, string]               // key: policyID -> latest version
	PolicyUpdates       collections.Map[uint64, *types.PolicyUpdate]  // key: sequence number
	PolicyUpdateCounter collections.Sequence
	PolicyAudit         collections.Map[string, *types.PolicyAuditEntry]     // key: auditID
	PolicyAuditCounter  collections.Sequence                                 // unique suffix per audit entry
	PolicyByOwner       collections.KeySet[collections.Pair[string, string]] // key: (owner, policyID)
	PolicyByState       collections.KeySet[collections.Pair[uint32, string]] // key: (state, policyID)
	BudgetUsage         collections.Map[string, uint64]                      // key: encoded budget usage scope
}

// Keeper provides the policies module's state access layer.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService corestore.KVStoreService
	authority    string
	state        State
	logger       log.Logger
}

// NewKeeper constructs a Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	authority string,
) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsPrefix),
			"params",
			collPtrValue[types.Params](cdc),
		),
		Policies: collections.NewMap(
			sb,
			collections.NewPrefix(types.PolicyPrefix),
			"policies",
			collections.StringKey,
			collPtrValue[types.PolicyProfile](cdc),
		),
		PolicyVersions: collections.NewMap(
			sb,
			collections.NewPrefix(types.PolicyVersionPrefix),
			"policy_versions",
			collections.StringKey,
			collections.StringValue,
		),
		PolicyUpdates: collections.NewMap(
			sb,
			collections.NewPrefix(types.PolicyUpdatePrefix),
			"policy_updates",
			collections.Uint64Key,
			collPtrValue[types.PolicyUpdate](cdc),
		),
		PolicyUpdateCounter: collections.NewSequence(
			sb,
			collections.NewPrefix(types.PolicyUpdateCounterPrefix),
			"policy_update_counter",
		),
		PolicyAudit: collections.NewMap(
			sb,
			collections.NewPrefix(types.PolicyAuditPrefix),
			"policy_audit",
			collections.StringKey,
			collPtrValue[types.PolicyAuditEntry](cdc),
		),
		PolicyAuditCounter: collections.NewSequence(
			sb,
			collections.NewPrefix(types.PolicyAuditCounterPrefix),
			"policy_audit_counter",
		),
		PolicyByOwner: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.PolicyByOwnerPrefix),
			"policy_by_owner",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
		),
		PolicyByState: collections.NewKeySet(
			sb,
			collections.NewPrefix(types.PolicyByStatePrefix),
			"policy_by_state",
			collections.PairKeyCodec(collections.Uint32Key, collections.StringKey),
		),
		BudgetUsage: collections.NewMap(
			sb,
			collections.NewPrefix(types.BudgetUsagePrefix),
			"budget_usage",
			collections.StringKey,
			collections.Uint64Value,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build policies schema: %w", err))
	}
	state.Schema = schema

	return &Keeper{
		cdc:          cdc,
		storeService: storeService,
		authority:    authority,
		state:        state,
	}
}

// Logger returns the module logger.
func (k Keeper) Logger(ctx context.Context) log.Logger {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.Logger().With("module", "x/"+types.ModuleName)
}

// GetAuthority returns the module's authority address.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// GetParams returns the current module parameters.
func (k Keeper) GetParams(ctx context.Context) (*types.Params, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.state.Params.Get(sdkCtx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams(), nil
		}
		return nil, fmt.Errorf("failed to get params: %w", err)
	}
	return params, nil
}

// SetParams sets the module parameters.
func (k Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if err := params.Validate(); err != nil {
		return types.ErrInvalidParams.Wrap(err.Error())
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return k.state.Params.Set(sdkCtx, params)
}

// GetPolicy retrieves a policy by ID and version.
// If version is empty, returns the latest version.
func (k Keeper) GetPolicy(ctx context.Context, policyID, version string) (*types.PolicyProfile, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if version == "" {
		// Get latest version
		latestVersion, err := k.state.PolicyVersions.Get(sdkCtx, policyID)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return nil, types.ErrPolicyNotFound.Wrapf("policy %s not found", policyID)
			}
			return nil, fmt.Errorf("failed to get policy version: %w", err)
		}
		version = latestVersion
	}

	key := policyID + ":" + version
	policy, err := k.state.Policies.Get(sdkCtx, key)
	if err != nil {
		return nil, types.ErrPolicyNotFound.Wrapf("policy %s version %s not found", policyID, version)
	}

	return policy, nil
}

// CreatePolicy creates a new policy profile.
func (k Keeper) CreatePolicy(ctx context.Context, policy *types.PolicyProfile) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Validate policy
	if err := types.ValidatePolicy(policy); err != nil {
		return err
	}

	// Check for existing policy with same ID and version
	key := policy.PolicyId + ":" + policy.Version
	has, err := k.state.Policies.Has(sdkCtx, key)
	if err != nil {
		return err
	}
	if has {
		return types.ErrPolicyAlreadyExists.Wrapf("policy %s version %s already exists", policy.PolicyId, policy.Version)
	}

	// Remove old indexes if this is an update
	var oldPolicy *types.PolicyProfile
	oldVersion, err := k.state.PolicyVersions.Get(sdkCtx, policy.PolicyId)
	if err == nil && oldVersion != "" {
		oldPolicy, _ = k.GetPolicy(ctx, policy.PolicyId, oldVersion)
	}

	if oldPolicy != nil {
		if oldPolicy.Metadata != nil && oldPolicy.Metadata.Owner != "" {
			oldOwnerKey := collections.Join(oldPolicy.Metadata.Owner, oldPolicy.PolicyId)
			if err := k.state.PolicyByOwner.Remove(sdkCtx, oldOwnerKey); err != nil {
				return err
			}
		}
		if oldPolicy.Lifecycle != nil {
			oldStateKey := collections.Join(uint32(oldPolicy.Lifecycle.State), oldPolicy.PolicyId)
			if err := k.state.PolicyByState.Remove(sdkCtx, oldStateKey); err != nil {
				return err
			}
		}
	}

	// Save policy
	if err := k.state.Policies.Set(sdkCtx, key, policy); err != nil {
		return err
	}

	// Update latest version
	if err := k.state.PolicyVersions.Set(sdkCtx, policy.PolicyId, policy.Version); err != nil {
		return err
	}

	// Index by owner
	if policy.Metadata != nil && policy.Metadata.Owner != "" {
		ownerKey := collections.Join(policy.Metadata.Owner, policy.PolicyId)
		if err := k.state.PolicyByOwner.Set(sdkCtx, ownerKey); err != nil {
			return err
		}
	}

	// Index by state
	if policy.Lifecycle != nil {
		stateKey := collections.Join(uint32(policy.Lifecycle.State), policy.PolicyId)
		if err := k.state.PolicyByState.Set(sdkCtx, stateKey); err != nil {
			return err
		}
	}

	// Emit event
	owner := ""
	if policy.Metadata != nil {
		owner = policy.Metadata.Owner
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"policy_created",
			sdk.NewAttribute("policy_id", policy.PolicyId),
			sdk.NewAttribute("version", policy.Version),
			sdk.NewAttribute("owner", owner),
		),
	)

	return nil
}

// UpdatePolicy creates a new version of an existing policy.
//
// BUDGET SEMANTICS (lumera_ai-veuki): bumping policy.Version preserves
// every user's accumulated BudgetUsage counters across all periods
// (per-session / per-hour / per-day / per-week / per-month). The
// preservation is enforced because budgetUsageKey keys strictly on
// PolicyId (enforce.go), entirely ignoring the policy.Version. A
// user who is at 90% of their per-day hard limit under v1 will
// continue to be at 90% of their limit under v2 within the same day.
//
// This safe-by-default behavior protects administrators who bump
// the version for routine updates (e.g., allowlist changes, typo fixes)
// from silently doubling user budgets.
//
// Pin test: enforce_test.go::TestEvaluatePolicy_VersionUpgradeResetsBudgetCounter_BudgetBypassRisk.
func (k Keeper) UpdatePolicy(ctx context.Context, updater string, policy *types.PolicyProfile, reason string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if err := types.ValidatePolicyUpdateReason(reason); err != nil {
		return err
	}

	// Get current version
	currentVersion, err := k.state.PolicyVersions.Get(sdkCtx, policy.PolicyId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ErrPolicyNotFound.Wrapf("policy %s not found", policy.PolicyId)
		}
		return fmt.Errorf("failed to get policy version: %w", err)
	}

	// Create new version
	if err := k.CreatePolicy(ctx, policy); err != nil {
		return err
	}

	// Record update
	seq, err := k.state.PolicyUpdateCounter.Next(sdkCtx)
	if err != nil {
		return err
	}

	update := &types.PolicyUpdate{
		PolicyId:        policy.PolicyId,
		Version:         policy.Version,
		PreviousVersion: currentVersion,
		Updater:         updater,
		UpdateReason:    reason,
		BlockHeight:     uint64(sdkCtx.BlockHeight()), //#nosec G115 -- block heights always non-negative
	}

	if err := k.state.PolicyUpdates.Set(sdkCtx, seq, update); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"policy_updated",
			sdk.NewAttribute("policy_id", policy.PolicyId),
			sdk.NewAttribute("version", policy.Version),
			sdk.NewAttribute("previous_version", currentVersion),
			sdk.NewAttribute("updater", updater),
		),
	)

	return nil
}

// SetPolicyState updates the lifecycle state of a policy.
func (k Keeper) SetPolicyState(ctx context.Context, policyID, version string, newState types.PolicyState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	policy, err := k.GetPolicy(ctx, policyID, version)
	if err != nil {
		return err
	}

	if policy.Lifecycle == nil {
		return types.ErrInvalidPolicyVersion.Wrap("policy has no lifecycle configuration")
	}

	oldState := policy.Lifecycle.State

	// Check if this is the latest version
	latestVersion, err := k.state.PolicyVersions.Get(sdkCtx, policyID)
	if err != nil {
		return fmt.Errorf("failed to get latest policy version: %w", err)
	}
	isLatest := (version == latestVersion)

	if isLatest {
		// Remove from old state index
		oldStateKey := collections.Join(uint32(oldState), policyID)
		if err := k.state.PolicyByState.Remove(sdkCtx, oldStateKey); err != nil {
			return err
		}
	}

	// Update policy state
	policy.Lifecycle.State = newState

	// Save updated policy
	key := policyID + ":" + version
	if err := k.state.Policies.Set(sdkCtx, key, policy); err != nil {
		return err
	}

	if isLatest {
		// Add to new state index
		newStateKey := collections.Join(uint32(newState), policyID)
		if err := k.state.PolicyByState.Set(sdkCtx, newStateKey); err != nil {
			return err
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"policy_state_changed",
			sdk.NewAttribute("policy_id", policyID),
			sdk.NewAttribute("version", version),
			sdk.NewAttribute("old_state", fmt.Sprintf("%d", oldState)),
			sdk.NewAttribute("new_state", fmt.Sprintf("%d", newState)),
		),
	)

	return nil
}

// ListPoliciesByOwner returns policies owned by a specific address.
func (k Keeper) ListPoliciesByOwner(ctx context.Context, owner string) ([]*types.PolicyProfile, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var policies []*types.PolicyProfile
	rng := collections.NewPrefixedPairRange[string, string](owner)
	const maxResults = 1000

	err := k.state.PolicyByOwner.Walk(sdkCtx, rng, func(key collections.Pair[string, string]) (stop bool, err error) {
		policyID := key.K2()
		policy, err := k.GetPolicy(ctx, policyID, "")
		if err != nil {
			return false, err
		}
		policies = append(policies, policy)
		return len(policies) >= maxResults, nil
	})

	if err != nil {
		return nil, err
	}

	return policies, nil
}

// ListPoliciesByState returns policies in a specific lifecycle state.
func (k Keeper) ListPoliciesByState(ctx context.Context, state types.PolicyState) ([]*types.PolicyProfile, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var policies []*types.PolicyProfile
	rng := collections.NewPrefixedPairRange[uint32, string](uint32(state))
	const maxResults = 1000

	err := k.state.PolicyByState.Walk(sdkCtx, rng, func(key collections.Pair[uint32, string]) (stop bool, err error) {
		policyID := key.K2()
		policy, err := k.GetPolicy(ctx, policyID, "")
		if err != nil {
			return false, err
		}
		policies = append(policies, policy)
		return len(policies) >= maxResults, nil
	})

	if err != nil {
		return nil, err
	}

	return policies, nil
}

// RollbackPolicy rolls back a policy to a previous version.
// This updates the "latest version" pointer without creating a new version.
func (k Keeper) RollbackPolicy(ctx context.Context, policyID, targetVersion, reason, rolledBackBy string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Verify target version exists
	targetKey := policyID + ":" + targetVersion
	has, err := k.state.Policies.Has(sdkCtx, targetKey)
	if err != nil {
		return err
	}
	if !has {
		return types.ErrPolicyNotFound.Wrapf("policy %s version %s not found", policyID, targetVersion)
	}

	// Get current version for recording
	currentVersion, err := k.state.PolicyVersions.Get(sdkCtx, policyID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.ErrPolicyNotFound.Wrapf("policy %s not found", policyID)
		}
		return fmt.Errorf("failed to get policy version: %w", err)
	}

	// Don't rollback to the same version
	if currentVersion == targetVersion {
		return types.ErrInvalidPolicyVersion.Wrap("target version is already the current version")
	}

	// Remove indexes for the current latest version
	currentPolicy, _ := k.GetPolicy(ctx, policyID, currentVersion)
	if currentPolicy != nil {
		if currentPolicy.Metadata != nil && currentPolicy.Metadata.Owner != "" {
			if err := k.state.PolicyByOwner.Remove(sdkCtx, collections.Join(currentPolicy.Metadata.Owner, policyID)); err != nil {
				return err
			}
		}
		if currentPolicy.Lifecycle != nil {
			if err := k.state.PolicyByState.Remove(sdkCtx, collections.Join(uint32(currentPolicy.Lifecycle.State), policyID)); err != nil {
				return err
			}
		}
	}

	// Update the latest version pointer to target
	if err := k.state.PolicyVersions.Set(sdkCtx, policyID, targetVersion); err != nil {
		return err
	}

	// Add indexes for the new latest version
	targetPolicy, err := k.GetPolicy(ctx, policyID, targetVersion)
	if err != nil {
		return fmt.Errorf("failed to load target policy version %s: %w", targetVersion, err)
	}
	if targetPolicy != nil {
		if targetPolicy.Metadata != nil && targetPolicy.Metadata.Owner != "" {
			if err := k.state.PolicyByOwner.Set(sdkCtx, collections.Join(targetPolicy.Metadata.Owner, policyID)); err != nil {
				return err
			}
		}
		if targetPolicy.Lifecycle != nil {
			if err := k.state.PolicyByState.Set(sdkCtx, collections.Join(uint32(targetPolicy.Lifecycle.State), policyID)); err != nil {
				return err
			}
		}
	}

	// Record the rollback in update history
	seq, err := k.state.PolicyUpdateCounter.Next(sdkCtx)
	if err != nil {
		return err
	}

	update := &types.PolicyUpdate{
		PolicyId:        policyID,
		Version:         targetVersion,
		PreviousVersion: currentVersion,
		Updater:         rolledBackBy,
		UpdateReason:    "ROLLBACK: " + reason,
		BlockHeight:     uint64(sdkCtx.BlockHeight()), //#nosec G115 -- block heights always non-negative
	}

	if err := k.state.PolicyUpdates.Set(sdkCtx, seq, update); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"policy_rollback",
			sdk.NewAttribute("policy_id", policyID),
			sdk.NewAttribute("target_version", targetVersion),
			sdk.NewAttribute("previous_version", currentVersion),
			sdk.NewAttribute("rolled_back_by", rolledBackBy),
			sdk.NewAttribute("reason", reason),
		),
	)

	return nil
}

// ListPolicyVersions returns all versions of a policy.
func (k Keeper) ListPolicyVersions(ctx context.Context, policyID string) ([]*types.PolicyProfile, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var versions []*types.PolicyProfile
	prefix := policyID + ":"
	// Optimize scan by using a range that covers the prefix "policyID:"
	// ':' is ASCII 58, ';' is ASCII 59.
	rng := new(collections.Range[string]).
		StartInclusive(prefix).
		EndExclusive(policyID + ";")

	err := k.state.Policies.Walk(sdkCtx, rng, func(key string, policy *types.PolicyProfile) (stop bool, err error) {
		versions = append(versions, policy)
		return false, nil
	})

	if err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		return nil, types.ErrPolicyNotFound.Wrapf("no versions found for policy %s", policyID)
	}

	return versions, nil
}

// GetPolicyUpdateHistory returns the update history for a policy.
func (k Keeper) GetPolicyUpdateHistory(ctx context.Context, policyID string) ([]*types.PolicyUpdate, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var history []*types.PolicyUpdate

	err := k.state.PolicyUpdates.Walk(sdkCtx, nil, func(_ uint64, update *types.PolicyUpdate) (stop bool, err error) {
		if update.PolicyId == policyID {
			history = append(history, update)
		}
		return false, nil
	})

	if err != nil {
		return nil, err
	}

	return history, nil
}

// RecordAuditEntry records a policy enforcement decision.
func (k Keeper) RecordAuditEntry(ctx context.Context, entry *types.PolicyAuditEntry) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return k.state.PolicyAudit.Set(sdkCtx, entry.AuditId, entry)
}

// ImportState imports genesis state.
func (k Keeper) ImportState(ctx sdk.Context, genesis *types.GenesisState) error {
	if genesis == nil {
		genesis = types.DefaultGenesis()
	}
	if err := genesis.Validate(); err != nil {
		return fmt.Errorf("invalid policies genesis: %w", err)
	}

	if err := k.SetParams(ctx, genesis.Params); err != nil {
		return fmt.Errorf("failed to set params: %w", err)
	}

	// Import policies
	for _, policy := range genesis.Policies {
		if err := k.CreatePolicy(ctx, policy); err != nil {
			return fmt.Errorf("failed to import policy %s: %w", policy.PolicyId, err)
		}
	}

	// Import policy updates
	for i, update := range genesis.PolicyUpdates {
		if update == nil {
			return fmt.Errorf("failed to import policy update %d: update cannot be nil", i)
		}
		if err := k.state.PolicyUpdates.Set(ctx, uint64(i), update); err != nil {
			return fmt.Errorf("failed to import policy update: %w", err)
		}
	}

	// Restore counter
	updateCounter := genesis.PolicyUpdateCounter
	if importedUpdates := uint64(len(genesis.PolicyUpdates)); updateCounter < importedUpdates {
		updateCounter = importedUpdates
	}
	if updateCounter > 0 {
		if err := k.state.PolicyUpdateCounter.Set(ctx, updateCounter); err != nil {
			return fmt.Errorf("failed to set policy update counter: %w", err)
		}
	}

	return nil
}

// ExportState exports genesis state.
func (k Keeper) ExportState(ctx sdk.Context) (*types.GenesisState, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get params: %w", err)
	}

	var policies []*types.PolicyProfile
	err = k.state.Policies.Walk(ctx, nil, func(_ string, policy *types.PolicyProfile) (stop bool, err error) {
		policies = append(policies, policy)
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to export policies: %w", err)
	}

	var updates []*types.PolicyUpdate
	err = k.state.PolicyUpdates.Walk(ctx, nil, func(_ uint64, update *types.PolicyUpdate) (stop bool, err error) {
		updates = append(updates, update)
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to export policy updates: %w", err)
	}

	counter, err := k.state.PolicyUpdateCounter.Peek(ctx)
	if err != nil {
		counter = 0
	}

	return &types.GenesisState{
		Params:              params,
		Policies:            policies,
		PolicyUpdates:       updates,
		PolicyUpdateCounter: counter,
	}, nil
}
