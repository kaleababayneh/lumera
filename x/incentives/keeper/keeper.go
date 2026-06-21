// Package keeper implements the incentives module keeper.
package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/incentives/types"
)

// State encapsulates the module collections state.
type State struct {
	Schema                 collections.Schema
	Params                 collections.Item[*types.Params]
	Badges                 collections.Map[string, *types.Badge]          // key: tool_id
	TierConfigs            collections.Map[int32, *types.TierConfig]      // key: BadgeTier enum value
	MetricSnapshots        collections.Map[string, *types.MetricSnapshot] // key: tool_id (latest)
	BadgeEvents            collections.Map[string, *types.BadgeEvent]     // key: composite (tool_id + timestamp)
	LastEvaluationBlock    collections.Map[string, uint64]                // key: tool_id, value: block height
	LastExpiredBadgeCursor collections.Item[string]                       // last badge tool_id scanned for expiration
}

// Keeper provides the module's state access layer.
type Keeper struct {
	cdc            codec.BinaryCodec
	storeService   corestore.KVStoreService
	accountKeeper  types.AccountKeeper
	bankKeeper     types.BankKeeper
	registryKeeper types.RegistryKeeper
	routerKeeper   types.RouterKeeper
	authority      string
	state          State
	scorer         *types.Scorer
}

// NewKeeper constructs a Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	registryKeeper types.RegistryKeeper,
	routerKeeper types.RouterKeeper,
	authority string,
) Keeper {
	if accountKeeper == nil {
		panic("incentives keeper requires account keeper")
	}

	sb := collections.NewSchemaBuilder(storeService)
	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsPrefix),
			"params",
			collPtrValue[types.Params](cdc),
		),
		Badges: collections.NewMap(
			sb,
			collections.NewPrefix(types.BadgesPrefix),
			"badges",
			collections.StringKey,
			collPtrValue[types.Badge](cdc),
		),
		TierConfigs: collections.NewMap(
			sb,
			collections.NewPrefix(types.TierConfigsPrefix),
			"tier_configs",
			collections.Int32Key,
			collPtrValue[types.TierConfig](cdc),
		),
		MetricSnapshots: collections.NewMap(
			sb,
			collections.NewPrefix(types.MetricSnapshotsPrefix),
			"metric_snapshots",
			collections.StringKey,
			collPtrValue[types.MetricSnapshot](cdc),
		),
		BadgeEvents: collections.NewMap(
			sb,
			collections.NewPrefix(types.BadgeEventsPrefix),
			"badge_events",
			collections.StringKey,
			collPtrValue[types.BadgeEvent](cdc),
		),
		LastEvaluationBlock: collections.NewMap(
			sb,
			collections.NewPrefix(types.LastEvaluationBlockPrefix),
			"last_evaluation_block",
			collections.StringKey,
			collections.Uint64Value,
		),
		LastExpiredBadgeCursor: collections.NewItem(
			sb,
			collections.NewPrefix(types.LastExpiredBadgeCursorPrefix),
			"last_expired_badge_cursor",
			collections.StringValue,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build incentives schema: %w", err))
	}
	state.Schema = schema

	return Keeper{
		cdc:            cdc,
		storeService:   storeService,
		accountKeeper:  accountKeeper,
		bankKeeper:     bankKeeper,
		registryKeeper: registryKeeper,
		routerKeeper:   routerKeeper,
		authority:      authority,
		state:          state,
		scorer:         types.NewDefaultScorer(),
	}
}

// Schema returns the underlying collections schema.
func (k Keeper) Schema() collections.Schema { return k.state.Schema }

// Logger returns a module-prefixed logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// Authority returns the authority address for governance operations.
func (k Keeper) Authority() string { return k.authority }

func validateKeeperToolID(toolID string) error {
	trimmed := strings.TrimSpace(toolID)
	if trimmed == "" {
		return fmt.Errorf("tool_id is required")
	}
	if trimmed != toolID {
		return fmt.Errorf("tool_id must not have leading or trailing whitespace")
	}
	return nil
}

// GetParams retrieves module parameters.
func (k Keeper) GetParams(ctx context.Context) *types.Params {
	params, err := k.state.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams()
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		k.Logger(sdkCtx).Error("incentives params load failed, returning defaults", "error", err)
		return types.DefaultParams()
	}
	if params == nil {
		return types.DefaultParams()
	}
	return params
}

// SetParams updates module parameters.
func (k Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := params.Validate(); err != nil {
		return err
	}
	return k.state.Params.Set(ctx, params)
}

// GetBadge retrieves a badge by tool ID. Expired badges (ExpiresAt <= block
// time) are treated as absent so callers never see stale tier benefits —
// this read-side filter is the authoritative suppression. The underlying
// state entry is left in place so it survives genesis export via
// IterateBadges (used by ExportGenesis), which deliberately returns all
// badges including expired ones for lossless chain migration.
//
// Reaping happens separately: the EndBlocker's ProcessExpiredBadges sweep
// re-evaluates expired badges and revokes them (recording a revocation
// event) when re-evaluation fails. RevokeBadge therefore reads raw state,
// not this filtered accessor — otherwise expired badges would be invisible
// to the sweep and accumulate forever.
func (k Keeper) GetBadge(ctx context.Context, toolID string) (*types.Badge, bool) {
	badge, err := k.state.Badges.Get(ctx, toolID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Error("incentives: failed to read badge", "tool", toolID, "error", err)
		return nil, false
	}
	if badge == nil {
		return nil, false
	}
	if !badge.ExpiresAt.IsZero() {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		if !sdkCtx.BlockTime().Before(badge.ExpiresAt) {
			return nil, false
		}
	}
	return badge, true
}

// SetBadge stores or updates a badge.
func (k Keeper) SetBadge(ctx context.Context, badge *types.Badge) error {
	if badge == nil {
		return fmt.Errorf("badge cannot be nil")
	}
	if err := validateKeeperToolID(badge.ToolId); err != nil {
		return err
	}
	return k.state.Badges.Set(ctx, badge.ToolId, badge)
}

// DeleteBadge removes a badge from state.
func (k Keeper) DeleteBadge(ctx context.Context, toolID string) error {
	if err := validateKeeperToolID(toolID); err != nil {
		return err
	}
	return k.state.Badges.Remove(ctx, toolID)
}

// IterateBadges walks through all badges.
func (k Keeper) IterateBadges(ctx context.Context, cb func(*types.Badge) bool) error {
	return k.state.Badges.Walk(ctx, nil, func(_ string, badge *types.Badge) (bool, error) {
		if badge == nil {
			return false, nil
		}
		return cb(badge), nil
	})
}

// IterateBadgeEvents walks the badge event audit trail in key order.
func (k Keeper) IterateBadgeEvents(ctx context.Context, cb func(*types.BadgeEvent) bool) error {
	return k.state.BadgeEvents.Walk(ctx, nil, func(_ string, event *types.BadgeEvent) (bool, error) {
		if event == nil {
			return false, nil
		}
		return cb(event), nil
	})
}

// RestoreBadgeEvent re-inserts an exported badge event under the same
// composite key EvaluateTool and RevokeBadge use when recording, so a
// genesis round-trip preserves the audit trail under identical keys.
func (k Keeper) RestoreBadgeEvent(ctx context.Context, event *types.BadgeEvent) error {
	if event == nil {
		return fmt.Errorf("badge event cannot be nil")
	}
	if err := validateKeeperToolID(event.ToolId); err != nil {
		return err
	}
	if event.Timestamp.IsZero() {
		return fmt.Errorf("badge event for %s missing timestamp", event.ToolId)
	}
	eventKey := fmt.Sprintf("%s-%d-%s-%d-%d", event.ToolId, event.Timestamp.UnixNano(), event.EventType, int32(event.PreviousTier), int32(event.NewTier))
	return k.state.BadgeEvents.Set(ctx, eventKey, event)
}

// GetTierConfig retrieves a tier configuration.
func (k Keeper) GetTierConfig(ctx context.Context, tier types.BadgeTier) (*types.TierConfig, bool) {
	config, err := k.state.TierConfigs.Get(ctx, int32(tier))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Error("incentives: failed to read tier config", "tier", tier, "error", err)
		return nil, false
	}
	if config == nil {
		return nil, false
	}
	return config, true
}

// SetTierConfig stores or updates a tier configuration.
func (k Keeper) SetTierConfig(ctx context.Context, config *types.TierConfig) error {
	if config == nil {
		return fmt.Errorf("tier config cannot be nil")
	}
	if err := types.ValidateTierConfig(config); err != nil {
		return err
	}
	return k.state.TierConfigs.Set(ctx, int32(config.Tier), config)
}

// GetAllTierConfigs retrieves all tier configurations.
func (k Keeper) GetAllTierConfigs(ctx context.Context) []*types.TierConfig {
	configs := make([]*types.TierConfig, 0)
	if err := k.state.TierConfigs.Walk(ctx, nil, func(_ int32, config *types.TierConfig) (bool, error) {
		if config != nil {
			configs = append(configs, config)
		}
		return false, nil
	}); err != nil {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Error("incentives: failed to walk tier configs", "error", err)
	}
	return configs
}

// RecordMetrics stores a metric snapshot for a tool.
func (k Keeper) RecordMetrics(ctx context.Context, metrics *types.MetricSnapshot) error {
	if metrics == nil {
		return fmt.Errorf("metrics cannot be nil")
	}
	if err := validateKeeperToolID(metrics.ToolId); err != nil {
		return err
	}
	return k.state.MetricSnapshots.Set(ctx, metrics.ToolId, metrics)
}

// GetMetrics retrieves the latest metric snapshot for a tool.
func (k Keeper) GetMetrics(ctx context.Context, toolID string) (*types.MetricSnapshot, bool) {
	metrics, err := k.state.MetricSnapshots.Get(ctx, toolID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Error("incentives: failed to read metrics", "tool", toolID, "error", err)
		return nil, false
	}
	if metrics == nil {
		return nil, false
	}
	return metrics, true
}

// GetAllMetricSnapshots retrieves all metric snapshots stored in state.
func (k Keeper) GetAllMetricSnapshots(ctx context.Context) []*types.MetricSnapshot {
	snapshots := make([]*types.MetricSnapshot, 0)
	if err := k.state.MetricSnapshots.Walk(ctx, nil, func(_ string, snapshot *types.MetricSnapshot) (bool, error) {
		if snapshot != nil {
			snapshots = append(snapshots, snapshot)
		}
		return false, nil
	}); err != nil {
		panic(fmt.Errorf("incentives: failed to walk metric snapshots: %w", err))
	}
	return snapshots
}

// EvaluateTool evaluates a tool and updates its badge if necessary.
func (k Keeper) EvaluateTool(ctx context.Context, toolID string) (*types.Badge, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(ctx)

	if err := validateKeeperToolID(toolID); err != nil {
		return nil, err
	}

	// Get current metrics
	metrics, found := k.GetMetrics(ctx, toolID)
	if !found {
		return nil, types.ErrInsufficientMetrics.Wrapf("no metrics found for tool %s", toolID)
	}

	// Check minimum invocations
	if metrics.TotalInvocations < uint64(params.MinInvocationsForScoring) {
		return nil, types.ErrInsufficientMetrics.Wrapf(
			"tool %s has %d invocations, minimum required is %d",
			toolID, metrics.TotalInvocations, params.MinInvocationsForScoring,
		)
	}

	// Calculate component scores
	components := k.scorer.CalculateAllComponentScores(metrics)

	// Calculate composite score
	compositeScore := k.scorer.CalculateCompositeScore(components)

	// Get all tier configs
	tierConfigs := k.GetAllTierConfigs(ctx)
	if len(tierConfigs) == 0 {
		tierConfigs = types.DefaultTierConfigs()
	}

	// Determine eligible tier
	newTier := k.scorer.DetermineTier(compositeScore, tierConfigs)

	// Get current badge (if any)
	currentBadge, hasBadge := k.GetBadge(ctx, toolID)

	now := sdkCtx.BlockTime()
	blockHeight := sdkCtx.BlockHeight()

	var previousTier types.BadgeTier
	if hasBadge {
		previousTier = currentBadge.Tier
	} else {
		previousTier = types.BadgeTier_BADGE_TIER_NONE
	}

	// Get publisher ID from registry
	publisherID := ""
	if k.registryKeeper != nil {
		if pubAddr, err := k.registryKeeper.GetToolPublisher(ctx, toolID); err == nil && pubAddr != nil {
			publisherID = pubAddr.String()
		}
	}

	// Calculate validity period
	var validityBlocks uint32
	for _, config := range tierConfigs {
		if config.Tier == newTier {
			validityBlocks = config.ValidityPeriodBlocks
			break
		}
	}
	if validityBlocks == 0 {
		validityBlocks = types.BronzeValidityBlocks // Default
	}

	// Calculate expiration time. Use the shared TargetBlockTime so this stays
	// consistent with the "~N days at Xs blocks" comments on BronzeValidityBlocks
	// and siblings — previously these had drifted (params.go said 5s, keeper
	// used 6s) and Bronze ExpiresAt was 20% longer than the comment implied.
	if now.IsZero() {
		return nil, fmt.Errorf("incentives: block time must be set")
	}
	expiresAt := now.Add(time.Duration(validityBlocks) * types.TargetBlockTime)

	// Handle tier changes
	eventType := ""
	reason := ""
	gracePeriodActive := false
	var gracePeriodEndsAt time.Time

	if newTier > previousTier {
		// Upgrade
		if previousTier == types.BadgeTier_BADGE_TIER_NONE {
			eventType = "awarded"
			reason = fmt.Sprintf("achieved tier %v with score %d", newTier, compositeScore)
		} else {
			eventType = "upgraded"
			reason = fmt.Sprintf("upgraded from %v to %v with score %d", previousTier, newTier, compositeScore)
		}
	} else if newTier < previousTier && hasBadge {
		// Downgrade - check grace period
		if currentBadge.GracePeriodActive {
			// Inclusive boundary, mirroring the badge-expiry filter: the
			// grace period ENDS at GracePeriodEndsAt, so a tie means the
			// downgrade applies now rather than one evaluation later.
			if !currentBadge.GracePeriodEndsAt.IsZero() && !now.Before(currentBadge.GracePeriodEndsAt) {
				// Grace period expired, apply downgrade
				eventType = "downgraded"
				reason = fmt.Sprintf("downgraded from %v to %v after grace period", previousTier, newTier)
			} else {
				// Still in grace period, keep current tier
				newTier = previousTier
				gracePeriodActive = true
				gracePeriodEndsAt = currentBadge.GracePeriodEndsAt
			}
		} else {
			// Enter grace period
			gracePeriodEnd := now.Add(time.Duration(params.GracePeriodBlocks) * types.TargetBlockTime)
			currentBadge.GracePeriodActive = true
			currentBadge.GracePeriodEndsAt = gracePeriodEnd
			currentBadge.LastEvaluatedAt = now
			if err := k.SetBadge(ctx, currentBadge); err != nil {
				return nil, fmt.Errorf("failed to update badge grace period: %w", err)
			}
			return currentBadge, nil
		}
	}

	// Calculate consecutive periods
	consecutivePeriods := uint32(0)
	if hasBadge && newTier == previousTier {
		consecutivePeriods = currentBadge.ConsecutivePeriods + 1
	} else if newTier > types.BadgeTier_BADGE_TIER_NONE {
		consecutivePeriods = 1
	}

	// Create or update badge
	badge := &types.Badge{
		ToolId:             toolID,
		PublisherId:        publisherID,
		Tier:               newTier,
		CompositeScore:     compositeScore,
		AwardedAt:          now,
		ExpiresAt:          expiresAt,
		LastEvaluatedAt:    now,
		ComponentScores:    components,
		ConsecutivePeriods: consecutivePeriods,
		GracePeriodActive:  gracePeriodActive,
		GracePeriodEndsAt:  gracePeriodEndsAt,
	}

	if hasBadge && eventType == "" {
		// No change, just update evaluation time
		badge.AwardedAt = currentBadge.AwardedAt
	}

	if err := k.SetBadge(ctx, badge); err != nil {
		return nil, fmt.Errorf("failed to save badge: %w", err)
	}

	// Record last evaluation block
	if err := k.state.LastEvaluationBlock.Set(ctx, toolID, uint64(blockHeight)); err != nil {
		return nil, fmt.Errorf("record last evaluation block for %s: %w", toolID, err)
	}

	// Record badge event if there was a change
	if eventType != "" {
		event := &types.BadgeEvent{
			ToolId:       toolID,
			EventType:    eventType,
			PreviousTier: previousTier,
			NewTier:      newTier,
			Score:        compositeScore,
			Timestamp:    now,
			Reason:       reason,
		}
		// Key includes event_type + tier transition, not just
		// (tool_id, unix_nano). BlockTime is identical for every tx in
		// a Cosmos block, so the prior "{tool_id}-{unix_nano}" form
		// collided whenever two events for the same tool landed in the
		// same block — e.g., an in-block EvaluateTool upgrade A->B
		// followed by a gov-triggered RevokeBadge at RevokeBadge:543
		// (also on this sweep), or two sequential upgrades A->B->C via
		// two msgs in the same block. The later Set silently overwrote
		// the earlier event, losing an audit row that downstream
		// reputation/compliance paths rely on. The expanded key is
		// unique per (tool, block, event_type, tier-transition); the
		// degenerate "same tier-transition twice in a block" case is
		// unreachable because the post-event badge state necessarily
		// differs (an A->B upgrade leaves the tool at B, so the next
		// upgrade must start from B, not A). Same bug class as
		// cac_royalties commit 42d16798e (CAC RecordID collisions on
		// same-block BlockTime.UnixNano fallback).
		eventKey := fmt.Sprintf("%s-%d-%s-%d-%d", toolID, now.UnixNano(), eventType, int32(previousTier), int32(newTier))
		if err := k.state.BadgeEvents.Set(ctx, eventKey, event); err != nil {
			return nil, fmt.Errorf("record badge event for %s: %w", toolID, err)
		}

		// Emit event
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"badge_"+eventType,
				sdk.NewAttribute("tool_id", toolID),
				sdk.NewAttribute("previous_tier", previousTier.String()),
				sdk.NewAttribute("new_tier", newTier.String()),
				sdk.NewAttribute("score", fmt.Sprintf("%d", compositeScore)),
			),
		)
	}

	return badge, nil
}

// CanEvaluate checks if a tool can be evaluated (respects evaluation interval).
func (k Keeper) CanEvaluate(ctx context.Context, toolID string) bool {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(ctx)

	lastBlock, err := k.state.LastEvaluationBlock.Get(ctx, toolID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return true // Never evaluated before
		}
		return false
	}

	currentBlock := uint64(sdkCtx.BlockHeight())
	if currentBlock < lastBlock {
		return true // Stale data; allow re-evaluation
	}
	return currentBlock-lastBlock >= uint64(params.EvaluationIntervalBlocks)
}

// RevokeBadge revokes a badge from a tool.
func (k Keeper) RevokeBadge(ctx context.Context, toolID string, reason string) (*types.Badge, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if err := validateKeeperToolID(toolID); err != nil {
		return nil, err
	}
	if err := types.ValidateRevokeBadgeReason(reason); err != nil {
		return nil, err
	}

	// Read raw state rather than GetBadge: revocation is administrative
	// cleanup of whatever record exists, and GetBadge filters out expired
	// badges — which would make it impossible for the EndBlocker sweep (or
	// governance) to revoke a badge that has already lapsed, leaving an
	// invisible orphan in state forever.
	badge, err := k.state.Badges.Get(ctx, toolID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrBadgeNotFound.Wrapf("no badge found for tool %s", toolID)
		}
		return nil, fmt.Errorf("get badge for %s: %w", toolID, err)
	}
	if badge == nil {
		return nil, types.ErrBadgeNotFound.Wrapf("no badge found for tool %s", toolID)
	}

	if badge.Tier == types.BadgeTier_BADGE_TIER_NONE {
		return nil, types.ErrBadgeAlreadyRevoked
	}

	previousTier := badge.Tier
	now := sdkCtx.BlockTime()

	// Record revocation event. Key format mirrors the EvaluateTool
	// event key (event_type + tier transition in addition to
	// tool_id + unix_nano) so a same-block EvaluateTool emit and a
	// RevokeBadge emit on the same tool land in distinct rows rather
	// than silently overwriting each other — see the note at the
	// matching Set call in EvaluateTool for the full rationale.
	event := &types.BadgeEvent{
		ToolId:       toolID,
		EventType:    "revoked",
		PreviousTier: previousTier,
		NewTier:      types.BadgeTier_BADGE_TIER_NONE,
		Score:        badge.CompositeScore,
		Timestamp:    now,
		Reason:       reason,
	}
	eventKey := fmt.Sprintf("%s-%d-%s-%d-%d", toolID, now.UnixNano(), event.EventType, int32(previousTier), int32(types.BadgeTier_BADGE_TIER_NONE))
	if err := k.state.BadgeEvents.Set(ctx, eventKey, event); err != nil {
		return nil, fmt.Errorf("record revocation event for %s: %w", toolID, err)
	}

	// Delete the badge
	if err := k.DeleteBadge(ctx, toolID); err != nil {
		return nil, fmt.Errorf("failed to delete badge: %w", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"badge_revoked",
			sdk.NewAttribute("tool_id", toolID),
			sdk.NewAttribute("previous_tier", previousTier.String()),
			sdk.NewAttribute("reason", reason),
		),
	)

	return badge, nil
}

// isBadgeActive reports whether a badge exists, has a real tier, and has not
// yet expired relative to the current block time. Expiration is enforced by
// the EndBlocker, but between ticks a badge may linger past its ExpiresAt;
// callers granting tier benefits must treat such badges as inactive so agents
// do not receive routing boosts / discounts / bonuses past the expiry.
func (k Keeper) isBadgeActive(ctx context.Context, toolID string) (*types.Badge, bool) {
	badge, found := k.GetBadge(ctx, toolID)
	if !found {
		return nil, false
	}
	if badge.Tier == types.BadgeTier_BADGE_TIER_NONE {
		return nil, false
	}
	if !badge.ExpiresAt.IsZero() {
		now := sdk.UnwrapSDKContext(ctx).BlockTime()
		if now.After(badge.ExpiresAt) {
			return nil, false
		}
	}
	return badge, true
}

// GetRoutingMultiplier returns the routing weight multiplier for a tool's badge tier.
// Returns 10000 (1.0x) if no badge, unknown tier, or the badge has expired.
func (k Keeper) GetRoutingMultiplier(ctx context.Context, toolID string) uint32 {
	badge, active := k.isBadgeActive(ctx, toolID)
	if !active {
		return types.BasisPointsScale // 1.0x default
	}

	config, found := k.GetTierConfig(ctx, badge.Tier)
	if !found {
		return types.BasisPointsScale
	}

	return config.RoutingWeightMultiplierBps
}

// GetInsuranceDiscount returns the insurance discount for a tool's badge tier.
// Returns 0 if no badge, unknown tier, or the badge has expired.
func (k Keeper) GetInsuranceDiscount(ctx context.Context, toolID string) uint32 {
	badge, active := k.isBadgeActive(ctx, toolID)
	if !active {
		return 0
	}

	config, found := k.GetTierConfig(ctx, badge.Tier)
	if !found {
		return 0
	}

	return config.InsuranceDiscountBps
}

// GetLACBonus returns the LAC bonus for a tool's badge tier.
// Returns 0 if no badge, unknown tier, or the badge has expired.
func (k Keeper) GetLACBonus(ctx context.Context, toolID string) uint32 {
	badge, active := k.isBadgeActive(ctx, toolID)
	if !active {
		return 0
	}

	config, found := k.GetTierConfig(ctx, badge.Tier)
	if !found {
		return 0
	}

	return config.LacBonusBps
}

// MaxBadgesPerBlock bounds how many badges the expiration sweep may visit in
// a single EndBlocker invocation. Chosen to keep the sweep off the critical
// path on large networks; remaining badges are covered on subsequent
// evaluation intervals via the LastExpiredBadgeCursor.
const MaxBadgesPerBlock = 200

// ProcessExpiredBadges walks the Badges store starting from the last
// expiration cursor, re-evaluates (or revokes) any badges whose ExpiresAt
// has passed, and advances the cursor. Capped at MaxBadgesPerBlock per
// invocation so that EndBlocker work stays bounded on large networks.
//
// This method lives on the keeper so the expiration cursor stays behind
// the package boundary — the module's EndBlocker used to poke at the
// keeper's private state directly, which wouldn't compile across packages.
func (k Keeper) ProcessExpiredBadges(ctx sdk.Context) {
	now := ctx.BlockTime()
	var processed int

	startCursor, err := k.state.LastExpiredBadgeCursor.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		ctx.Logger().Error("incentives: failed to get expiration cursor", "error", err)
		return
	}

	var rng collections.Ranger[string]
	if startCursor != "" {
		rng = new(collections.Range[string]).StartExclusive(startCursor)
	}

	iter, err := k.state.Badges.Iterate(ctx, rng)
	if err != nil {
		ctx.Logger().Error("incentives: failed to iterate badges for expiration", "error", err)
		return
	}
	defer func() {
		_ = iter.Close()
	}()

	var lastKey string

	for ; iter.Valid(); iter.Next() {
		processed++
		if processed > MaxBadgesPerBlock {
			break
		}

		key, err := iter.Key()
		if err != nil {
			continue
		}
		lastKey = key

		badge, err := iter.Value()
		if err != nil || badge == nil {
			continue
		}

		// Inclusive boundary to match GetBadge's expiry filter: a badge
		// with ExpiresAt == BlockTime is already invisible to reads, so
		// the sweep must pick it up in the same block rather than one
		// block later.
		if !badge.ExpiresAt.IsZero() && !now.Before(badge.ExpiresAt) {
			if _, err := k.EvaluateTool(ctx, badge.ToolId); err != nil {
				if _, rErr := k.RevokeBadge(ctx, badge.ToolId, "badge expired and re-evaluation failed"); rErr != nil {
					ctx.Logger().Error("incentives: failed to revoke expired badge",
						"tool_id", badge.ToolId, "error", rErr)
				}
			}
		}
	}

	// Persist (or clear) the sweep cursor. A silent Set/Remove failure
	// here would make the sweep stall: the SAME cursor would be read
	// on the next EndBlocker, re-processing the same badges forever
	// and leaving badges past the cursor never re-evaluated/revoked.
	// The expiration pass has already finished its badge work, so the
	// keeper cannot meaningfully abort; log the error loudly so
	// operators see the stall and can investigate the underlying
	// store fault rather than watching badges silently accumulate
	// past expiry.
	if !iter.Valid() {
		if err := k.state.LastExpiredBadgeCursor.Remove(ctx); err != nil {
			ctx.Logger().Error("incentives: failed to clear expiration cursor after exhausting badges", "error", err)
		}
	} else if lastKey != "" {
		if err := k.state.LastExpiredBadgeCursor.Set(ctx, lastKey); err != nil {
			ctx.Logger().Error("incentives: failed to advance expiration cursor", "error", err)
		}
	}
}
