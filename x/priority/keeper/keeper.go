// Package keeper implements state management for the priority module.
package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/priority/types"
)

func marshalParams(params *types.Params) ([]byte, error) { return json.Marshal(params) }

func unmarshalParams(bz []byte) (*types.Params, error) {
	if len(bz) == 0 {
		return types.DefaultParams(), nil
	}
	var params types.Params
	if err := json.Unmarshal(bz, &params); err != nil {
		return nil, err
	}
	return &params, nil
}

func marshalAssignment(a types.PriorityAssignment) ([]byte, error) { return json.Marshal(a) }

func unmarshalAssignment(bz []byte) (types.PriorityAssignment, error) {
	var assignment types.PriorityAssignment
	if err := json.Unmarshal(bz, &assignment); err != nil {
		return types.PriorityAssignment{}, err
	}
	if err := assignment.ValidateBasic(); err != nil {
		return types.PriorityAssignment{}, types.ErrInvalidAssignment.Wrap(err.Error())
	}
	return assignment, nil
}

func validateAssignmentKey(policyID string, assignment types.PriorityAssignment) error {
	if assignment.PolicyID != policyID {
		return types.ErrInvalidAssignment.Wrapf("assignment policy id %q does not match key %q", assignment.PolicyID, policyID)
	}
	return nil
}

// State definition.
type State struct {
	Schema      collections.Schema
	Params      collections.Item[[]byte]
	Assignments collections.Map[string, []byte]
}

// Keeper controls priority tier assignments.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService corestore.KVStoreService
	authority    string
	logger       log.Logger
	state        State
}

// NewKeeper instantiates priority keeper.
func NewKeeper(cdc codec.BinaryCodec, storeService corestore.KVStoreService, authority string, logger log.Logger) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsKeyPrefix),
			"params",
			collections.BytesValue,
		),
		Assignments: collections.NewMap(
			sb,
			collections.NewPrefix(types.AssignmentKeyPrefix),
			"assignments",
			collections.StringKey,
			collections.BytesValue,
		),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("priority schema build failed: %w", err))
	}
	state.Schema = schema

	return &Keeper{
		cdc:          cdc,
		storeService: storeService,
		authority:    authority,
		logger:       logger.With("module", fmt.Sprintf("x/%s", types.ModuleName)),
		state:        state,
	}
}

// Logger returns module logger.
func (k *Keeper) Logger() log.Logger { return k.logger }

// GetParams obtains stored params.
func (k *Keeper) GetParams(ctx context.Context) (*types.Params, error) {
	bz, err := k.state.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams(), nil
		}
		return nil, err
	}
	return unmarshalParams(bz)
}

// SetParams validates and stores params.
func (k *Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if err := params.ValidateBasic(); err != nil {
		return err
	}
	bz, err := marshalParams(params)
	if err != nil {
		return err
	}
	return k.state.Params.Set(ctx, bz)
}

// AssignPolicyTier associates a policy with a tier for a duration.
func (k *Keeper) AssignPolicyTier(ctx context.Context, policyID, tierName string, duration time.Duration) error {
	if strings.TrimSpace(policyID) == "" || strings.TrimSpace(policyID) != policyID {
		return types.ErrInvalidAssignment
	}
	if strings.TrimSpace(tierName) == "" || strings.TrimSpace(tierName) != tierName {
		return types.ErrInvalidAssignment
	}
	if duration < 0 {
		return types.ErrInvalidAssignment
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	tier, ok := params.FindTier(tierName)
	if !ok {
		return types.ErrPriorityTierNotFound
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return fmt.Errorf("priority: block time must be set")
	}
	var expires time.Time
	if duration > 0 {
		expires = now.Add(duration)
	}

	assignment := types.PriorityAssignment{
		PolicyID:   policyID,
		Tier:       tier.Name,
		AssignedAt: now,
		ExpiresAt:  expires,
	}
	if err := assignment.ValidateBasic(); err != nil {
		return err
	}

	bz, err := marshalAssignment(assignment)
	if err != nil {
		return err
	}
	return k.state.Assignments.Set(ctx, policyID, bz)
}

// ClearAssignment removes policy tier override.
func (k *Keeper) ClearAssignment(ctx context.Context, policyID string) error {
	return k.state.Assignments.Remove(ctx, policyID)
}

// ResolveAdjustments implements types.PriorityKeeper.
func (k *Keeper) ResolveAdjustments(ctx context.Context, policyID string, defaultLatency uint32, defaultTTL time.Duration) (types.PriorityAdjustments, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return types.PriorityAdjustments{}, err
	}

	tier := params.DefaultTierConfig()
	assignmentBytes, err := k.state.Assignments.Get(ctx, policyID)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return types.PriorityAdjustments{}, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()
	if now.IsZero() {
		return types.PriorityAdjustments{}, fmt.Errorf("priority: block time must be set")
	}

	if err == nil {
		assignment, err := unmarshalAssignment(assignmentBytes)
		if err != nil {
			return types.PriorityAdjustments{}, err
		}
		if err := validateAssignmentKey(policyID, assignment); err != nil {
			return types.PriorityAdjustments{}, err
		}
		if !assignment.ExpiresAt.IsZero() && !assignment.ExpiresAt.After(now) {
			if err := k.state.Assignments.Remove(ctx, policyID); err != nil {
				k.Logger().Error("failed to remove expired assignment", "policy_id", policyID, "error", err)
			}
		} else {
			if override, ok := params.FindTier(assignment.Tier); ok {
				tier = override
			}
		}
	}

	adjustedLatency := defaultLatency
	if tier.MaxLatencyMs > 0 && (adjustedLatency == 0 || tier.MaxLatencyMs < adjustedLatency) {
		adjustedLatency = tier.MaxLatencyMs
	}

	adjustedTTL := defaultTTL
	tierTTL := time.Duration(tier.AuctionTTLMs) * time.Millisecond //#nosec G115 -- TTL bounded by tier configuration
	if tierTTL > 0 && (adjustedTTL == 0 || tierTTL < adjustedTTL) {
		adjustedTTL = tierTTL
	}

	return types.PriorityAdjustments{
		Applied:              true,
		TierName:             tier.Name,
		MaxLatencyMs:         adjustedLatency,
		AuctionTTL:           adjustedTTL,
		SpotDiscountBps:      tier.SpotDiscountBps,
		QueueWeight:          tier.QueueWeight,
		PricingMultiplierBps: tier.PricingMultiplierBps,
		ReservedCapacityBps:  tier.ReservedCapacityBps,
	}, nil
}

// GetAssignment returns the assignment for diagnostics.
func (k *Keeper) GetAssignment(ctx context.Context, policyID string) (*types.PriorityAssignment, bool, error) {
	bz, err := k.state.Assignments.Get(ctx, policyID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	assignment, err := unmarshalAssignment(bz)
	if err != nil {
		return nil, false, err
	}
	if err := validateAssignmentKey(policyID, assignment); err != nil {
		return nil, false, err
	}
	return &assignment, true, nil
}
