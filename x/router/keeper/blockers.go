// Package keeper contains the router module's Cosmos SDK keeper logic.
package keeper

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

// BeginBlocker executes at the beginning of each block.
//
// Current responsibilities:
//   - Prune expired replay-protection entries (Phase 2.13.2.4).
//
// NOT included by design: per-block expired-session sweeps. Session
// expiration is handled LAZILY at GetSession time (see
// sessions.go + TestGetSession_ExpiredSessionReset in
// sessions_test.go) — an expired session is reset to a fresh empty
// shell on its next lookup rather than swept proactively. This is
// deliberate: most sessions are touched frequently enough that
// lazy expiration keeps state bounded without the per-block scan
// cost; inactive sessions consume at-most (TTL × user-count)
// bytes until their eventual reset. If session-count growth ever
// outpaces natural lookup traffic, swap this comment's claim and
// add a bounded per-block sweep keyed off a CreatedAtTime range
// query — the existing lazy path stays correct under either
// strategy.
func (k Keeper) BeginBlocker(ctx sdk.Context) error {
	if err := k.CleanupReplayCache(ctx); err != nil {
		return err
	}
	return nil
}

// EndBlocker executes at the end of each block
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	// Check if metrics aggregation is needed
	params, err := k.state.Params.Get(ctx)
	if err != nil {
		return err
	}

	if err := k.advanceDiscoverySubsidyAccountingPeriod(ctx, params); err != nil {
		return err
	}

	if !params.MetricsEnabled {
		return nil
	}

	// Get next aggregation block
	nextAggregation, err := k.state.NextAggregation.Get(ctx)
	if err != nil {
		// Initialize if not set
		nextAggregation = uint64(ctx.BlockHeight()) + uint64(params.MetricsIntervalBlocks) //#nosec G115 -- block heights always non-negative
		if err := k.state.NextAggregation.Set(ctx, nextAggregation); err != nil {
			return err
		}
		return nil
	}

	// Check if it's time to aggregate
	if uint64(ctx.BlockHeight()) >= nextAggregation { //#nosec G115 -- block heights always non-negative
		if err := k.AggregateMetrics(ctx, false); err != nil {
			k.Logger(ctx).Error("failed to aggregate metrics", "error", err)
		}

		// Update next aggregation block
		nextAggregation = uint64(ctx.BlockHeight()) + uint64(params.MetricsIntervalBlocks) //#nosec G115 -- block heights always non-negative
		if err := k.state.NextAggregation.Set(ctx, nextAggregation); err != nil {
			return err
		}
	}

	return nil
}

func (k Keeper) advanceDiscoverySubsidyAccountingPeriod(ctx sdk.Context, params *types.Params) error {
	if params.GetDiscoverySubsidyBps() == 0 || params.GetDiscoverySubsidyPeriodBlocks() == 0 {
		return nil
	}
	if ctx.BlockHeight() < 0 {
		return types.ErrInvalidParams.Wrap("block height cannot be negative")
	}

	currentBlock := uint64(ctx.BlockHeight()) //#nosec G115 -- checked non-negative above
	period := uint64(params.GetDiscoverySubsidyPeriodBlocks())
	nextReset, err := k.state.DiscoverySubsidyReset.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		return k.state.DiscoverySubsidyReset.Set(ctx, currentBlock+period)
	}
	if nextReset == 0 {
		return k.state.DiscoverySubsidyReset.Set(ctx, currentBlock+period)
	}
	if currentBlock < nextReset {
		return nil
	}

	previousSpent, err := k.discoverySubsidySpent(ctx)
	if err != nil {
		return err
	}
	for nextReset <= currentBlock {
		if nextReset > ^uint64(0)-period {
			return types.ErrInvalidParams.Wrap("discovery_subsidy_next_reset_block overflow")
		}
		nextReset += period
	}
	if err := k.state.DiscoverySubsidySpent.Set(ctx, decimal.Zero.String()); err != nil {
		return err
	}
	if err := k.state.DiscoverySubsidyReset.Set(ctx, nextReset); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeDiscoveryReset,
			sdk.NewAttribute(types.AttributeKeyBlockHeight, fmt.Sprintf("%d", currentBlock)),
			sdk.NewAttribute(types.AttributeKeyNextResetBlock, fmt.Sprintf("%d", nextReset)),
			sdk.NewAttribute(types.AttributeKeySubsidyPeriod, fmt.Sprintf("%d", period)),
			sdk.NewAttribute(types.AttributeKeyPreviousSpent, previousSpent.String()),
		),
	)
	return nil
}

// AggregateMetrics performs periodic metrics aggregation
func (k Keeper) AggregateMetrics(ctx sdk.Context, force bool) error {
	params, err := k.state.Params.Get(ctx)
	if err != nil {
		return err
	}

	if !params.MetricsEnabled && !force {
		return types.ErrMetricsDisabled
	}

	toolsProcessed := uint64(0)
	sessionsProcessed := uint64(0)

	// Phase 2.13.2.2: Apply bounds to prevent chain halt
	maxTools := int(params.MaxToolsProcessedPerBlock) //#nosec G115 -- uint32 fits in int
	if maxTools <= 0 {
		maxTools = 100 // Conservative default
	}

	// Aggregate category metrics
	categoryStats := make(map[string]*types.CategoryMetrics)

	// Resume from last processed tool
	startToolID, err := k.state.LastProcessedToolID.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	toolRange := new(collections.Range[string]).StartExclusive(startToolID)
	lastToolID := ""

	// Process tool metrics (BOUNDED - Phase 2.13.2.2)
	err = k.state.ToolMetrics.Walk(ctx, toolRange,
		func(toolID string, metrics *types.ActivationMetrics) (stop bool, err error) {
			// Stop if we've hit the limit
			if int(toolsProcessed) >= maxTools { //#nosec G115 -- bounded by maxTools
				return true, nil
			}
			toolsProcessed++
			lastToolID = toolID

			// Get tool category (falls back to prefix-derived category if not in registry)
			category, found := k.getToolCategoryWithStatus(ctx, toolID)
			if !found && k.registryKeeper != nil {
				return false, nil // Skip tools not found in registry to prevent category pollution
			}

			if _, exists := categoryStats[category]; !exists {
				stats := types.NewCategoryMetrics(category)
				stats.SetLastUpdated(ctx.BlockTime())
				categoryStats[category] = stats
			}

			stats := categoryStats[category]
			stats.TotalTools++
			if len(metrics.ActiveSessions) > 0 {
				stats.ActiveTools++
			}
			stats.TotalInvocations += metrics.InvocationCount
			toolVolume, derr := metrics.TotalVolumeDecimalSafe()
			if derr != nil {
				k.Logger(ctx).Error("invalid tool volume", "tool", toolID, "error", derr)
				toolVolume = decimal.Zero
			}
			currentVolume, derr := stats.TotalVolumeDecimalSafe()
			if derr != nil {
				k.Logger(ctx).Error("invalid category volume", "category", category, "error", derr)
				currentVolume = decimal.Zero
			}
			stats.SetTotalVolumeDecimal(currentVolume.Add(toolVolume))

			// Track top tools (simplified - just first 5)
			if len(stats.TopTools) < 5 {
				stats.TopTools = append(stats.TopTools, toolID)
			}

			// Update selection score
			if metrics.InvocationCount > 0 {
				score, serr := k.calculateToolScore(ctx, metrics)
				if serr != nil {
					k.Logger(ctx).Error("failed to calculate tool score", "tool", toolID, "error", serr)
				} else if err := k.state.SelectionScores.Set(ctx, toolID, score); err != nil {
					return false, fmt.Errorf("update selection score for %s: %w", toolID, err)
				}
			}

			return false, nil
		})

	if err != nil {
		return fmt.Errorf("failed to process tool metrics: %w", err)
	}

	// Update tool cursor
	if int(toolsProcessed) < maxTools {
		// Finished the map, reset cursor
		if err := k.state.LastProcessedToolID.Remove(ctx); err != nil {
			return err
		}
	} else {
		// Hit limit, save cursor
		if err := k.state.LastProcessedToolID.Set(ctx, lastToolID); err != nil {
			return err
		}
	}

	// Save category metrics (sorted keys for deterministic iteration order).
	catKeys := make([]string, 0, len(categoryStats))
	for k := range categoryStats {
		catKeys = append(catKeys, k)
	}
	sort.Strings(catKeys)
	for _, category := range catKeys {
		stats := categoryStats[category]
		if stats.TotalInvocations > 0 {
			totalVolume, derr := stats.TotalVolumeDecimalSafe()
			if derr != nil {
				k.Logger(ctx).Error("invalid category volume during average computation", "category", category, "error", derr)
				totalVolume = decimal.Zero
			}
			avg := decimal.Zero
			if !totalVolume.IsZero() {
				avg = totalVolume.Div(decimalFromUint64(stats.TotalInvocations))
			}
			stats.SetAverageCostDecimal(avg)
		}
		if err := k.state.CategoryMetrics.Set(ctx, category, stats); err != nil {
			return fmt.Errorf("save category metrics for %s: %w", category, err)
		}
	}

	// Process session metrics - clean up old sessions
	sessionsToDelete := []string{}
	ttlSeconds := params.GetSessionTtlSeconds()
	maxSessions := int(params.MaxSessionsProcessedPerBlock)
	if maxSessions <= 0 {
		maxSessions = 100 // Conservative default
	}

	// Resume from last processed session
	startSessionID, err := k.state.LastProcessedSessionID.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	sessionRange := new(collections.Range[string]).StartExclusive(startSessionID)
	lastSessionID := ""

	// Session cleanup (BOUNDED - Phase 2.13.2.2)
	err = k.state.SessionMetrics.Walk(ctx, sessionRange,
		func(sessionID string, metrics *types.SessionMetrics) (stop bool, err error) {
			// Stop if we've hit the limit
			if int(sessionsProcessed) >= maxSessions { //#nosec G115 -- bounded by maxSessions
				return true, nil
			}
			sessionsProcessed++
			lastSessionID = sessionID

			// Check if session has expired
			if metrics.EndedAt.IsZero() {
				if !metrics.StartedAt.IsZero() {
					sessionStart := metrics.StartedAt
					if !sessionStart.IsZero() {
						sessionAge := ctx.BlockTime().Sub(sessionStart)
						if ttlSeconds > 0 && sessionAge > time.Duration(ttlSeconds)*time.Second {
							// Mark session as ended
							now := ctx.BlockTime()
							metrics.EndedAt = now
							if err := k.state.SessionMetrics.Set(ctx, sessionID, metrics); err != nil {
								return false, fmt.Errorf("update session %s: %w", sessionID, err)
							}
							// Decrement active sessions count
							global, err := k.getOrInitGlobalMetrics(ctx)
							if err == nil && global.ActiveSessions > 0 {
								global.ActiveSessions--
								if err := k.state.GlobalMetrics.Set(ctx, global); err != nil {
									return false, fmt.Errorf("update global metrics: %w", err)
								}
							}
						}
					}
				}
			} else {
				// Delete very old sessions (> 2x TTL)
				endedAt := metrics.EndedAt
				if ttlSeconds > 0 && !endedAt.IsZero() {
					sessionAge := ctx.BlockTime().Sub(endedAt)
					if sessionAge > time.Duration(ttlSeconds)*time.Second*2 {
						sessionsToDelete = append(sessionsToDelete, sessionID)
					}
				}
			}

			return false, nil
		})

	if err != nil {
		return fmt.Errorf("failed to process session metrics: %w", err)
	}

	// Update session cursor
	if int(sessionsProcessed) < maxSessions {
		if err := k.state.LastProcessedSessionID.Remove(ctx); err != nil {
			return err
		}
	} else {
		if err := k.state.LastProcessedSessionID.Set(ctx, lastSessionID); err != nil {
			return err
		}
	}

	// Delete old sessions
	for _, sessionID := range sessionsToDelete {
		if err := k.state.SessionMetrics.Remove(ctx, sessionID); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("delete old session %s: %w", sessionID, err)
		}
	}

	// Emit aggregation event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeMetricsAggregate,
			sdk.NewAttribute(types.AttributeKeyBlockHeight, fmt.Sprintf("%d", ctx.BlockHeight())),
			sdk.NewAttribute(types.AttributeKeyToolsProcessed, fmt.Sprintf("%d", toolsProcessed)),
			sdk.NewAttribute(types.AttributeKeySessionsProcessed, fmt.Sprintf("%d", sessionsProcessed)),
		),
	)

	return nil
}

// getToolCategory returns the category for a tool using the registry module
func (k Keeper) getToolCategory(ctx sdk.Context, toolID string) string {
	category, _ := k.getToolCategoryWithStatus(ctx, toolID)
	return category
}

// getToolCategoryWithStatus returns the category and whether the tool exists in the registry
func (k Keeper) getToolCategoryWithStatus(ctx sdk.Context, toolID string) (string, bool) {
	if k.registryKeeper == nil {
		// Fallback for tests/environments without registry
		return categoryFromToolID(toolID), false
	}
	card, found := k.registryKeeper.GetToolCard(ctx, toolID)
	if !found || card == nil {
		return categoryFromToolID(toolID), false
	}
	if len(card.Categories) == 0 {
		return categoryFromToolID(toolID), true
	}
	return card.Categories[0], true
}

func categoryFromToolID(toolID string) string {
	if strings.TrimSpace(toolID) == "" {
		return "general"
	}

	// Deterministic fallback: infer category from the tool ID prefix (e.g. "ai-tool-xyz" → "ai").
	parts := strings.SplitN(toolID, "-", 2)
	if len(parts) < 2 {
		return "general"
	}
	category := strings.TrimSpace(parts[0])
	if category == "" {
		return "general"
	}
	return category
}
