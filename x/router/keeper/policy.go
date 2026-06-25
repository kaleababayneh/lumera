package keeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

var (
	selectionWeightPerformance   = decimal.RequireFromString("0.25")
	selectionWeightReliability   = decimal.RequireFromString("0.35")
	selectionWeightCostEfficiency = decimal.RequireFromString("0.20")
	selectionWeightReputation    = decimal.RequireFromString("0.20")
)

// RecordPolicyUpdate records a policy version change
func (k Keeper) RecordPolicyUpdate(ctx context.Context, policyID, newVersion, previousVersion string,
	changes map[string]string, updater, reason string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check if metrics are enabled
	params, err := k.state.Params.Get(sdkCtx)
	if err != nil {
		return err
	}
	if !params.GetMetricsEnabled() {
		return types.ErrMetricsDisabled
	}

	// Generate policy update ID
	seq, err := k.state.PolicyUpdateCounter.Next(sdkCtx)
	if err != nil {
		return err
	}

	// Create policy update record
	update := &types.PolicyUpdate{
		PolicyId:        policyID,
		Version:         newVersion,
		PreviousVersion: previousVersion,
		UpdatedAt:       sdkCtx.BlockTime(),
		Updater:         updater,
		UpdateReason:    reason,
		Changes:         changes,
		BlockHeight:     uint64(sdkCtx.BlockHeight()), //#nosec G115 -- block heights always non-negative
	}

	// Save the policy update
	if err := k.state.PolicyUpdates.Set(sdkCtx, seq, update); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypePolicyUpdate,
			sdk.NewAttribute(types.AttributeKeyPolicyID, policyID),
			sdk.NewAttribute(types.AttributeKeyVersion, newVersion),
			sdk.NewAttribute(types.AttributeKeyPreviousVersion, previousVersion),
			sdk.NewAttribute(types.AttributeKeyUpdater, updater),
			sdk.NewAttribute(types.AttributeKeyUpdateReason, reason),
		),
	)

	return nil
}

// GetPolicyUpdates retrieves policy updates with optional filtering, returning newest first.
func (k Keeper) GetPolicyUpdates(ctx context.Context, policyID string, limit uint32) ([]*types.PolicyUpdate, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	var updates []*types.PolicyUpdate
	
	// Get current sequence to start from newest
	seq, err := k.state.PolicyUpdateCounter.Peek(sdkCtx)
	if err != nil {
		return nil, err
	}

	// Decrement sequence to point to the last used ID (Peek returns next available)
	if seq > 0 {
		seq--
	}

	count := uint32(0)
	
	// Iterate backwards from current sequence
	// We scan until we find 'limit' matching updates or run out of history.
	// Since we can't easily skip non-matching keys efficiently without a secondary index,
	// we assume policy updates are sparse or 'limit' is small. 
	// If policyID is provided, this might still scan many records, but at least it starts from recent.
	for i := int64(seq); i >= 0; i-- {
		update, err := k.state.PolicyUpdates.Get(sdkCtx, uint64(i))
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return nil, err
		}

		if policyID != "" && update.GetPolicyId() != policyID {
			continue
		}

		updates = append(updates, update)
		count++

		if limit > 0 && count >= limit {
			break
		}
	}

	return updates, nil
}

// UpdateToolSelectionScore updates the selection score for a tool
func (k Keeper) UpdateToolSelectionScore(ctx context.Context, toolID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get tool metrics
	metrics, err := k.state.ToolMetrics.Get(sdkCtx, toolID)
	if err != nil {
		return types.ErrToolNotFound.Wrapf("tool %s not found", toolID)
	}

	// Calculate scores based on metrics
	score, calcErr := k.calculateToolScore(sdkCtx, metrics)
	if calcErr != nil {
		return calcErr
	}

	// Save the score
	if err := k.state.SelectionScores.Set(sdkCtx, toolID, score); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeScoreUpdate,
			sdk.NewAttribute(types.AttributeKeyToolID, toolID),
			sdk.NewAttribute(types.AttributeKeyOverallScore, score.OverallScoreDecimal().String()),
			sdk.NewAttribute(types.AttributeKeyDataPoints, fmt.Sprintf("%d", score.DataPoints)),
		),
	)

	return nil
}

// calculateToolScore calculates selection scores based on metrics
func (k Keeper) calculateToolScore(ctx sdk.Context, metrics *types.ActivationMetrics) (*types.ToolSelectionScore, error) {
	if metrics == nil {
		return nil, fmt.Errorf("activation metrics is nil")
	}
	avgLatency, err := metrics.AverageLatencyDecimalSafe()
	if err != nil {
		return nil, err
	}

	// Performance score based on latency
	performanceScore := decimal.NewFromInt(100)
	if !avgLatency.IsZero() {
		// Lower latency = higher score
		// Score = 100 * (1 - min(latency/5000, 1))
		normalized := avgLatency.Div(decimal.NewFromInt(5000))
		if normalized.GreaterThan(decimal.NewFromInt(1)) {
			normalized = decimal.NewFromInt(1)
		}
		performanceScore = decimal.NewFromInt(100).Mul(
			decimal.NewFromInt(1).Sub(normalized),
		)
	}

	// Reliability score equals success rate
	reliabilityScore, err := metrics.SuccessRateDecimalSafe()
	if err != nil {
		return nil, err
	}

	// Cost efficiency score based on average cost per invocation
	costEfficiencyScore := decimal.NewFromInt(100)
	totalVolume, err := metrics.TotalVolumeDecimalSafe()
	if err != nil {
		return nil, err
	}
	if metrics.InvocationCount > 0 && !totalVolume.IsZero() {
		avgCost := totalVolume.Div(decimalFromUint64(metrics.InvocationCount))
		// Lower average cost = higher score
		// Score = 100 * (1 - min(avgCost/100, 1))
		normalized := avgCost.Div(decimal.NewFromInt(100))
		if normalized.GreaterThan(decimal.NewFromInt(1)) {
			normalized = decimal.NewFromInt(1)
		}
		costEfficiencyScore = decimal.NewFromInt(100).Mul(
			decimal.NewFromInt(1).Sub(normalized),
		)
	}

	// Reputation score based on usage frequency and recency
	reputationScore := decimal.NewFromInt(50) // Base score
	if metrics.InvocationCount > 100 {
		reputationScore = reputationScore.Add(decimal.NewFromInt(20))
	} else if metrics.InvocationCount > 10 {
		reputationScore = reputationScore.Add(decimal.NewFromInt(10))
	}

	// Add bonus for recent activity
	if lastInvoked := metrics.LastInvokedTime(); lastInvoked != nil {
		age := ctx.BlockTime().Sub(*lastInvoked)
		if age < 24*time.Hour {
			reputationScore = reputationScore.Add(decimal.NewFromInt(20))
		} else if age < 7*24*time.Hour {
			reputationScore = reputationScore.Add(decimal.NewFromInt(10))
		}
	}

	// Cap reputation at 100
	if reputationScore.GreaterThan(decimal.NewFromInt(100)) {
		reputationScore = decimal.NewFromInt(100)
	}

	// Calculate overall score (weighted average)
	overallScore := performanceScore.Mul(selectionWeightPerformance).
		Add(reliabilityScore.Mul(selectionWeightReliability)).
		Add(costEfficiencyScore.Mul(selectionWeightCostEfficiency)).
		Add(reputationScore.Mul(selectionWeightReputation))

	score := types.NewToolSelectionScore(metrics.GetToolId())
	score.DataPoints = metrics.InvocationCount
	score.SetReputationScoreDecimal(reputationScore)
	score.SetPerformanceScoreDecimal(performanceScore)
	score.SetReliabilityScoreDecimal(reliabilityScore)
	score.SetCostEfficiencyScoreDecimal(costEfficiencyScore)
	score.SetOverallScoreDecimal(overallScore)
	score.SetLastCalculated(ctx.BlockTime())

	return score, nil
}
