package keeper

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

// parseCACSequence extracts the numeric suffix from a "cac-N" key.
// Returns an error for keys that don't match the pattern (e.g. legacy "cac-genesis-N").
func parseCACSequence(key string) (uint64, error) {
	if !strings.HasPrefix(key, "cac-") {
		return 0, fmt.Errorf("not a cac key")
	}
	return strconv.ParseUint(key[4:], 10, 64)
}

// InitGenesis initializes the router module's state from a provided genesis state
func (k Keeper) InitGenesis(ctx context.Context, genState *types.GenesisState) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if genState == nil {
		genState = types.DefaultGenesis()
	}

	if err := genState.Validate(); err != nil {
		panic(fmt.Errorf("invalid router genesis: %w", err))
	}

	// Set params
	if err := k.state.Params.Set(sdkCtx, &genState.Params); err != nil {
		panic(fmt.Errorf("failed to import router params: %w", err))
	}

	// Initialize metrics if provided
	if genState.State != nil {
		genState.State = types.EnsureRouterState(genState.State)
		// Set global metrics
		if genState.State.Metrics != nil {
			if err := k.state.GlobalMetrics.Set(sdkCtx, genState.State.Metrics); err != nil {
				panic(fmt.Errorf("failed to import router global metrics: %w", err))
			}
		}

		// Set next aggregation block
		if genState.State.NextAggregationBlock > 0 {
			if err := k.state.NextAggregation.Set(sdkCtx, genState.State.NextAggregationBlock); err != nil {
				panic(fmt.Errorf("failed to import router next-aggregation block: %w", err))
			}
		}
		if genState.State.DiscoverySubsidyNextResetBlock > 0 {
			if err := k.state.DiscoverySubsidyReset.Set(sdkCtx, genState.State.DiscoverySubsidyNextResetBlock); err != nil {
				panic(fmt.Errorf("failed to import router discovery-subsidy reset block: %w", err))
			}
		}

		// Import category metrics
		for category, metrics := range genState.State.CategoryMetrics {
			if metrics == nil {
				continue
			}
			if err := k.state.CategoryMetrics.Set(sdkCtx, category, metrics); err != nil {
				panic(fmt.Errorf("failed to import category metrics for %s: %w", category, err))
			}
		}

		// Import selection scores
		for toolID, score := range genState.State.SelectionScores {
			if score == nil {
				continue
			}
			if err := k.state.SelectionScores.Set(sdkCtx, toolID, score); err != nil {
				panic(fmt.Errorf("failed to import selection score for %s: %w", toolID, err))
			}
		}

		// Import recent policy updates
		for i, update := range genState.State.RecentPolicyUpdates {
			if update == nil {
				continue
			}
			if err := k.state.PolicyUpdates.Set(sdkCtx, uint64(i), update); err != nil {
				panic(fmt.Errorf("failed to import policy update %d: %w", i, err))
			}
		}
		if len(genState.State.RecentPolicyUpdates) > 0 {
			if err := k.state.PolicyUpdateCounter.Set(sdkCtx, uint64(len(genState.State.RecentPolicyUpdates))); err != nil {
				panic(fmt.Errorf("failed to set policy update counter: %w", err))
			}
		}
	}

	// Initialize tool metrics
	for _, metrics := range genState.ToolMetricsList {
		if metrics == nil {
			continue
		}
		vol, err := metrics.TotalVolumeDecimalSafe()
		if err != nil {
			panic(fmt.Errorf("invalid tool metric volume for %s: %w", metrics.GetToolId(), err))
		}
		metrics.SetTotalVolumeDecimal(vol)
		avg, err := metrics.AverageLatencyDecimalSafe()
		if err != nil {
			panic(fmt.Errorf("invalid tool metric latency for %s: %w", metrics.GetToolId(), err))
		}
		metrics.SetAverageLatencyDecimal(avg)
		success, err := metrics.SuccessRateDecimalSafe()
		if err != nil {
			panic(fmt.Errorf("invalid tool metric success rate for %s: %w", metrics.GetToolId(), err))
		}
		metrics.SetSuccessRateDecimal(success)
		if err := k.state.ToolMetrics.Set(sdkCtx, metrics.GetToolId(), metrics); err != nil {
			panic(fmt.Errorf("failed to import tool metrics for %s: %w", metrics.GetToolId(), err))
		}
	}

	// Initialize CAC records, preserving original store keys from CacheKey.
	var maxCACSeq uint64
	for i, record := range genState.CacRecords {
		if record == nil {
			continue
		}
		amount, err := record.RoyaltyAmountDecimalSafe()
		if err != nil {
			panic(fmt.Errorf("invalid CAC royalty amount for record %d: %w", i, err))
		}
		record.SetRoyaltyAmountDecimal(amount)
		total, err := record.TotalRoyaltiesEarnedDecimalSafe()
		if err != nil {
			panic(fmt.Errorf("invalid CAC total royalties for record %d: %w", i, err))
		}
		record.SetTotalRoyaltiesEarnedDecimal(total)

		// Use the record's own CacheKey (preserves the original store key).
		// Fall back to a synthetic key only for legacy genesis data without CacheKey.
		recordID := record.GetCacheKey()
		if recordID == "" {
			recordID = fmt.Sprintf("cac-genesis-%d", i)
		}
		if err := k.state.CACRecords.Set(sdkCtx, recordID, record); err != nil {
			panic(fmt.Errorf("failed to import CAC record %s: %w", recordID, err))
		}

		// Track the highest sequence from "cac-N" keys so the counter
		// is set high enough to avoid collisions with existing keys.
		if n, parseErr := parseCACSequence(recordID); parseErr == nil && n+1 > maxCACSeq {
			maxCACSeq = n + 1
		}
	}

	// Initialize sessions
	for _, session := range genState.SessionList {
		if session == nil {
			continue
		}
		spent, err := session.TotalSpentDecimalSafe()
		if err != nil {
			panic(fmt.Errorf("invalid session total spent for %s: %w", session.GetSessionId(), err))
		}
		session.SetTotalSpentDecimal(spent)
		refunded, err := session.TotalRefundedDecimalSafe()
		if err != nil {
			panic(fmt.Errorf("invalid session total refunded for %s: %w", session.GetSessionId(), err))
		}
		session.SetTotalRefundedDecimal(refunded)
		if err := k.state.SessionMetrics.Set(sdkCtx, session.GetSessionId(), session); err != nil {
			panic(fmt.Errorf("failed to import session metrics for %s: %w", session.GetSessionId(), err))
		}
	}

	// Set CAC record counter to the higher of (max parsed sequence, record count)
	// so that CACRecordCounter.Next() won't collide with any imported key.
	if maxCACSeq > 0 || len(genState.CacRecords) > 0 {
		counterVal := maxCACSeq
		if uint64(len(genState.CacRecords)) > counterVal {
			counterVal = uint64(len(genState.CacRecords))
		}
		if err := k.state.CACRecordCounter.Set(sdkCtx, counterVal); err != nil {
			panic(fmt.Errorf("failed to set CAC record counter: %w", err))
		}
	}
}

// ExportGenesis returns the router module's exported genesis
func (k Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	genesis := types.DefaultGenesisState()

	// Export params
	params, err := k.state.Params.Get(sdkCtx)
	if err == nil {
		genesis.Params = *params
	} else {
		genesis.Params = *types.DefaultParams()
	}

	// Export state
	genesis.State = types.EnsureRouterState(&types.RouterState{Params: genesis.Params})

	// Export global metrics
	globalMetrics, err := k.state.GlobalMetrics.Get(sdkCtx)
	if err == nil {
		genesis.State.Metrics = globalMetrics
	}

	// Export next aggregation block
	nextAgg, err := k.state.NextAggregation.Get(sdkCtx)
	if err == nil {
		genesis.State.NextAggregationBlock = nextAgg
	}
	nextDiscoverySubsidyReset, err := k.state.DiscoverySubsidyReset.Get(sdkCtx)
	if err == nil {
		genesis.State.DiscoverySubsidyNextResetBlock = nextDiscoverySubsidyReset
	}

	// Export tool metrics
	if err := k.state.ToolMetrics.Walk(sdkCtx, nil,
		func(toolID string, metrics *types.ActivationMetrics) (stop bool, err error) {
			if metrics == nil {
				return false, nil
			}
			genesis.ToolMetricsList = append(genesis.ToolMetricsList, metrics)
			genesis.State.ToolMetrics[toolID] = metrics
			return false, nil
		}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk tool metrics: %w", err))
	}

	// Export CAC records
	if err := k.state.CACRecords.Walk(sdkCtx, nil,
		func(_ string, record *types.CACRoyaltyRecord) (stop bool, err error) {
			if record == nil {
				return false, nil
			}
			genesis.CacRecords = append(genesis.CacRecords, record)
			return false, nil
		}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk CAC records: %w", err))
	}

	// Export sessions
	if err := k.state.SessionMetrics.Walk(sdkCtx, nil,
		func(sessionID string, session *types.SessionMetrics) (stop bool, err error) {
			if session == nil {
				return false, nil
			}
			genesis.SessionList = append(genesis.SessionList, session)
			genesis.State.SessionMetrics[sessionID] = session
			return false, nil
		}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk session metrics: %w", err))
	}

	// Export category metrics
	if err := k.state.CategoryMetrics.Walk(sdkCtx, nil,
		func(category string, metrics *types.CategoryMetrics) (stop bool, err error) {
			if metrics == nil {
				return false, nil
			}
			genesis.State.CategoryMetrics[category] = metrics
			return false, nil
		}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk category metrics: %w", err))
	}

	// Export selection scores
	if err := k.state.SelectionScores.Walk(sdkCtx, nil,
		func(toolID string, score *types.ToolSelectionScore) (stop bool, err error) {
			if score == nil {
				return false, nil
			}
			genesis.State.SelectionScores[toolID] = score
			return false, nil
		}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk selection scores: %w", err))
	}

	// Export recent policy updates (most recent 100).
	// Walk visits keys in ascending order (oldest first), so collect all
	// then keep only the tail to preserve the newest entries.
	var allPolicyUpdates []*types.PolicyUpdate
	if err := k.state.PolicyUpdates.Walk(sdkCtx, nil,
		func(_ uint64, update *types.PolicyUpdate) (stop bool, err error) {
			if update != nil {
				allPolicyUpdates = append(allPolicyUpdates, update)
			}
			return false, nil
		}); err != nil {
		panic(fmt.Errorf("ExportGenesis: failed to walk policy updates: %w", err))
	}
	if len(allPolicyUpdates) > 100 {
		allPolicyUpdates = allPolicyUpdates[len(allPolicyUpdates)-100:]
	}
	genesis.State.RecentPolicyUpdates = allPolicyUpdates

	return genesis
}
