package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

// GetParams returns the router module parameters
func (k Keeper) GetParams(ctx context.Context) (*types.Params, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.state.Params.Get(sdkCtx)
	if err != nil {
		return nil, err
	}
	return params, nil
}

// GetToolRateLimit returns an optional per-tool rate limit override derived from
// the current router Params. When ok is false, callers should fall back to the
// global rate limiting configuration.
func (k Keeper) GetToolRateLimit(ctx context.Context, toolID string) (maxRps uint32, burst uint32, ok bool, err error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		return 0, 0, false, err
	}
	maxRps, burst, ok = params.ToolRateLimitFor(toolID)
	return maxRps, burst, ok, nil
}

// SetParams stores router parameters after ensuring they satisfy the
// invariants documented in specs/ROUTER_API.md and README §5.3. The
// validation defends against configuration that would break active-set bounds,
// session cooldown behavior, or per-block processing limits.
func (k Keeper) SetParams(ctx context.Context, params *types.Params) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Validate params
	if params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := params.Validate(); err != nil {
		return err
	}

	return k.state.Params.Set(sdkCtx, params)
}

// UpdateParams updates the router module parameters via governance
func (k Keeper) UpdateParams(ctx context.Context, authority string, params *types.Params) error {
	// Check authority
	if authority != k.authority {
		return types.ErrUnauthorized.Wrapf("expected %s, got %s", k.authority, authority)
	}

	// Validate and set params
	if params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := params.Validate(); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get old params for event
	oldParams, _ := k.state.Params.Get(sdkCtx)

	// Set new params
	if err := k.state.Params.Set(sdkCtx, params); err != nil {
		return err
	}

	// Emit param update event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeParamUpdate,
			sdk.NewAttribute(types.AttributeKeyAuthority, authority),
			sdk.NewAttribute(types.AttributeKeyOldParams, fmt.Sprintf("%+v", oldParams)),
			sdk.NewAttribute(types.AttributeKeyNewParams, fmt.Sprintf("%+v", params)),
		),
	)

	return nil
}

// ValidateAuthority validates the given authority against the module's authority
func (k Keeper) ValidateAuthority(authority string) error {
	if authority != k.authority {
		return types.ErrUnauthorized.Wrapf("expected %s, got %s", k.authority, authority)
	}
	return nil
}

func (k Keeper) paramsOrDefault(ctx sdk.Context) (*types.Params, error) {
	params, err := k.state.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams(), nil
		}
		return nil, err
	}
	if params == nil {
		return types.DefaultParams(), nil
	}
	return params, nil
}
