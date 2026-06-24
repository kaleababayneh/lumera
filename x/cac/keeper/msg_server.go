package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/LumeraProtocol/lumera/x/cac/types"
)

// Note: protoCoinsToSDK and sdkCoinsToProto are defined in keeper.go

type msgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

// NewMsgServerImpl returns an implementation of the CAC Msg service.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

func (s *msgServer) requireKeeper() (*Keeper, error) {
	if s == nil || s.keeper == nil {
		return nil, fmt.Errorf("cac keeper not initialized")
	}
	return s.keeper, nil
}

func recoverCAC(action string, err *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		default:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		}
	}
}

func validateCACMsgServerID(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if trimmed != value {
		return "", fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	if len(value) > types.MaxCacheIDLen {
		return "", fmt.Errorf("%s length %d exceeds maximum %d", field, len(value), types.MaxCacheIDLen)
	}
	return value, nil
}

// CacheStore stores content in the content-addressed cache.
func (s *msgServer) CacheStore(goCtx context.Context, msg *types.MsgCacheStore) (resp *types.MsgCacheStoreResponse, err error) {
	defer recoverCAC("cac/CacheStore", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Parse publisher address
	publisherAddr, err := sdk.AccAddressFromBech32(msg.Publisher)
	if err != nil {
		return nil, fmt.Errorf("invalid publisher address: %w", err)
	}

	// Validate tool ID
	toolID, err := validateCACMsgServerID("tool_id", msg.ToolId)
	if err != nil {
		return nil, err
	}

	// Validate request hash
	requestHash, err := validateCACMsgServerID("request_hash", msg.RequestHash)
	if err != nil {
		return nil, err
	}

	// Validate content
	if len(msg.Content) == 0 {
		return nil, fmt.Errorf("content cannot be empty")
	}
	if len(msg.Content) > types.MaxCachedContentBytes {
		return nil, fmt.Errorf("content size %d exceeds maximum %d", len(msg.Content), types.MaxCachedContentBytes)
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	// Store the entry
	entry, err := keeper.StoreEntry(
		sdkCtx,
		publisherAddr,
		toolID,
		requestHash,
		msg.Content,
		msg.TtlSeconds,
		msg.IsDeterministic,
		msg.RoyaltyEligible,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to store cache entry: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("store entry returned nil without error")
	}

	resp = &types.MsgCacheStoreResponse{
		ContentHash: entry.ContentHash,
		Tier:        entry.Tier,
		ExpiresAt:   entry.ExpiresAt,
	}
	return resp, nil
}

// CacheInvalidate invalidates cache entries.
func (s *msgServer) CacheInvalidate(goCtx context.Context, msg *types.MsgCacheInvalidate) (resp *types.MsgCacheInvalidateResponse, err error) {
	defer recoverCAC("cac/CacheInvalidate", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Parse requester address
	requesterAddr, err := sdk.AccAddressFromBech32(msg.Requester)
	if err != nil {
		return nil, fmt.Errorf("invalid requester address: %w", err)
	}

	// Validate target
	if msg.TargetType == types.InvalidationTargetType_INVALIDATION_TARGET_TYPE_UNSPECIFIED {
		return nil, fmt.Errorf("target_type is required")
	}
	targetValue, err := validateCACMsgServerID("target_value", msg.TargetValue)
	if err != nil {
		return nil, err
	}

	var entriesInvalidated uint64
	var bytesFreed uint64

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	// Authorization: only the entry's publisher or the module authority
	// may invalidate cache entries. Before this check was added,
	// CacheInvalidate ran straight through to the keeper helpers with
	// no ownership gate — any bech32 address could wipe any target's
	// entries, collapsing cache hit rate (publisher royalty DoS),
	// forcing recomputation costs onto publishers, and giving an
	// attacker a free availability lever on the cache fabric.
	requesterStr := requesterAddr.String()
	isAuthority := requesterStr == keeper.Authority()

	// Process invalidation based on target type
	switch msg.TargetType {
	case types.InvalidationTargetType_INVALIDATION_TARGET_TYPE_CONTENT_HASH:
		entry, found, lookupErr := keeper.GetEntry(sdkCtx, targetValue)
		if lookupErr != nil && !errors.Is(lookupErr, types.ErrEntryExpired) {
			return nil, fmt.Errorf("failed to look up entry: %w", lookupErr)
		}
		if !found || entry == nil {
			// Nothing to invalidate; emit no event, return zero counts.
			return &types.MsgCacheInvalidateResponse{
				RequestId:          fmt.Sprintf("inv-%s-%d", requesterAddr.String()[:8], sdkCtx.BlockHeight()),
				EntriesInvalidated: 0,
				BytesFreed:         0,
			}, nil
		}
		if !isAuthority && entry.PublisherAddress != requesterStr {
			return nil, types.ErrUnauthorized.Wrapf(
				"only the entry publisher or module authority may invalidate content_hash %s", targetValue)
		}
		freed, err := keeper.InvalidateEntry(sdkCtx, targetValue)
		if err != nil {
			return nil, fmt.Errorf("failed to invalidate by content hash: %w", err)
		}
		if freed > 0 {
			entriesInvalidated = 1
			bytesFreed = freed
		}

	case types.InvalidationTargetType_INVALIDATION_TARGET_TYPE_TOOL_ID:
		// Tool-scope invalidation has broad blast radius (an entire
		// tool's cache is dropped, affecting every publisher whose
		// entries that tool references). Gate to module authority
		// only; publishers who want to drop their own entries should
		// use CONTENT_HASH or REQUEST_HASH targeting.
		if !isAuthority {
			return nil, types.ErrUnauthorized.Wrap(
				"tool_id-scope cache invalidation is restricted to module authority")
		}
		entries, bytes, err := keeper.InvalidateByTool(sdkCtx, targetValue)
		if err != nil {
			return nil, fmt.Errorf("failed to invalidate by tool: %w", err)
		}
		entriesInvalidated = entries
		bytesFreed = bytes

	case types.InvalidationTargetType_INVALIDATION_TARGET_TYPE_REQUEST_HASH:
		// Lookup by request hash and check ownership on every entry
		// before touching any of them — fail-closed semantics so a
		// caller cannot half-invalidate a tuple of co-cached entries
		// where only some are theirs.
		entries, err := keeper.LookupByRequest(sdkCtx, targetValue, "")
		if err != nil {
			return nil, fmt.Errorf("failed to lookup by request hash: %w", err)
		}
		if !isAuthority {
			for _, entry := range entries {
				if entry.PublisherAddress != requesterStr {
					return nil, types.ErrUnauthorized.Wrapf(
						"request_hash %s spans entries owned by other publishers; only the entry publisher or module authority may invalidate", targetValue)
				}
			}
		}
		for _, entry := range entries {
			freed, err := keeper.InvalidateEntry(sdkCtx, entry.ContentHash)
			if err != nil {
				keeper.Logger(sdkCtx).Warn("failed to invalidate entry", "content_hash", entry.ContentHash, "error", err)
				continue
			}
			entriesInvalidated++
			bytesFreed += freed
		}

	default:
		return nil, fmt.Errorf("unsupported invalidation target type: %v", msg.TargetType)
	}

	// Generate request ID
	requestID := fmt.Sprintf("inv-%s-%d", requesterAddr.String()[:8], sdkCtx.BlockHeight())

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeCacheInvalidate,
			sdk.NewAttribute(types.AttributeKeyInvalidationTarget, msg.TargetType.String()),
			sdk.NewAttribute("target_value", targetValue),
			sdk.NewAttribute(types.AttributeKeyEntriesInvalidated, fmt.Sprintf("%d", entriesInvalidated)),
			sdk.NewAttribute(types.AttributeKeyBytesFreed, fmt.Sprintf("%d", bytesFreed)),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	resp = &types.MsgCacheInvalidateResponse{
		RequestId:          requestID,
		EntriesInvalidated: entriesInvalidated,
		BytesFreed:         bytesFreed,
	}
	return resp, nil
}

// RecordCacheHit records a cache hit for royalty distribution.
func (s *msgServer) RecordCacheHit(goCtx context.Context, msg *types.MsgRecordCacheHit) (resp *types.MsgRecordCacheHitResponse, err error) {
	defer recoverCAC("cac/RecordCacheHit", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Parse router address
	_, err = sdk.AccAddressFromBech32(msg.Router)
	if err != nil {
		return nil, fmt.Errorf("invalid router address: %w", err)
	}

	// Validate content hash
	contentHash, err := validateCACMsgServerID("content_hash", msg.ContentHash)
	if err != nil {
		return nil, err
	}

	// Validate tool IDs
	originToolID, err := validateCACMsgServerID("origin_tool_id", msg.OriginToolId)
	if err != nil {
		return nil, err
	}
	servingToolID, err := validateCACMsgServerID("serving_tool_id", msg.ServingToolId)
	if err != nil {
		return nil, err
	}

	// Parse requester address
	requesterAddr, err := sdk.AccAddressFromBech32(msg.RequesterAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid requester address: %w", err)
	}
	if msg.Tier == types.CacheTier_CACHE_TIER_UNSPECIFIED {
		return nil, fmt.Errorf("tier is required")
	}
	if _, ok := types.CacheTier_name[int32(msg.Tier)]; !ok {
		return nil, fmt.Errorf("tier is invalid: %d", msg.Tier)
	}

	// Convert cost saved from proto to SDK coins
	costSaved := protoCoinsToSDK(msg.CostSaved)

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	// Record the cache hit
	hit, err := keeper.RecordCacheHit(
		sdkCtx,
		contentHash,
		originToolID,
		servingToolID,
		requesterAddr,
		costSaved,
		msg.LatencyMs,
		msg.Tier,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to record cache hit: %w", err)
	}

	// Process royalties if enabled
	// params := s.keeper.GetParams(sdkCtx)
	var originRoyalty, servingRoyalty sdk.Coins

	// SECURITY: Automatic royalty processing disabled in MsgRecordCacheHit.
	// This message does not collect funds from the requester, so triggering
	// payout here would drain the module account. Royalties are handled
	// in x/credits ProcessSettlement.

	resp = &types.MsgRecordCacheHitResponse{
		HitId:          hit.HitId,
		OriginRoyalty:  sdkCoinsToProto(originRoyalty),
		ServingRoyalty: sdkCoinsToProto(servingRoyalty),
	}
	return resp, nil
}

// TickDecay runs CAC expiry / decay upkeep.
func (s *msgServer) TickDecay(goCtx context.Context, msg *types.MsgTickDecay) (resp *types.MsgTickDecayResponse, err error) {
	defer recoverCAC("cac/TickDecay", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}
	if msg.Authority != keeper.Authority() {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.Authority(), msg.Authority)
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)
	evicted, err := keeper.TickDecay(sdkCtx, int(msg.Limit))
	if err != nil {
		return nil, fmt.Errorf("tick decay: %w", err)
	}

	return &types.MsgTickDecayResponse{
		EntriesEvicted: evicted,
		TickHeight:     sdkCtx.BlockHeight(),
	}, nil
}

// PromoteTier promotes a cache entry to a higher tier.
func (s *msgServer) PromoteTier(goCtx context.Context, msg *types.MsgPromoteTier) (resp *types.MsgPromoteTierResponse, err error) {
	defer recoverCAC("cac/PromoteTier", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Validate content hash
	contentHash, err := validateCACMsgServerID("content_hash", msg.ContentHash)
	if err != nil {
		return nil, err
	}

	// Validate target tier
	if msg.TargetTier == types.CacheTier_CACHE_TIER_UNSPECIFIED {
		return nil, fmt.Errorf("target_tier is required")
	}
	if _, ok := types.CacheTier_name[int32(msg.TargetTier)]; !ok {
		return nil, fmt.Errorf("target_tier is invalid: %d", msg.TargetTier)
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Check authority
	if msg.Authority != keeper.Authority() {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.Authority(), msg.Authority)
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	// Get current tier
	entry, found, err := keeper.GetEntry(sdkCtx, contentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get entry: %w", err)
	}
	if !found || entry == nil {
		return nil, types.ErrEntryNotFound.Wrapf("content_hash: %s", contentHash)
	}
	previousTier := entry.Tier

	// Promote the entry
	if err := keeper.PromoteEntry(sdkCtx, contentHash, msg.TargetTier); err != nil {
		return nil, fmt.Errorf("failed to promote entry: %w", err)
	}

	resp = &types.MsgPromoteTierResponse{
		PreviousTier: previousTier,
		NewTier:      msg.TargetTier,
	}
	return resp, nil
}

// UpdateParams updates the CAC module parameters.
func (s *msgServer) UpdateParams(goCtx context.Context, msg *types.MsgUpdateParams) (resp *types.MsgUpdateParamsResponse, err error) {
	defer recoverCAC("cac/UpdateParams", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Validate and set params
	if err := msg.Params.Validate(); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	keeper, err := s.requireKeeper()
	if err != nil {
		return nil, err
	}

	// Check authority
	if msg.Authority != keeper.Authority() {
		return nil, sdkerrors.ErrUnauthorized.Wrapf("invalid authority; expected %s, got %s", keeper.Authority(), msg.Authority)
	}

	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	if err := keeper.SetParams(sdkCtx, &msg.Params); err != nil {
		return nil, fmt.Errorf("failed to set params: %w", err)
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.TypeMsgUpdateParams,
			sdk.NewAttribute("authority", msg.Authority),
		),
	)

	resp = &types.MsgUpdateParamsResponse{}
	return resp, nil
}
