package keeper

import (
	"context"
	"errors"
	"fmt"
	stdmath "math"
	"math/big"
	"strings"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/internal/moneyguard"
	"github.com/LumeraProtocol/lumera/x/router/types"
)

var (
	emaLatencyAlpha = decimal.RequireFromString("0.2")
	emaSuccessAlpha = decimal.RequireFromString("0.1")
)

const discoverySubsidyCreditDecimalPlaces = 6

func sameMetricIdentifier(a, b string) bool {
	switch strings.Compare(a, b) {
	case 0:
		return true
	default:
		return false
	}
}

// RecordActivation records a tool activation or deactivation
func (k Keeper) RecordActivation(ctx context.Context, toolID, sessionID string, activated bool, reason string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check if metrics are enabled
	params, err := k.state.Params.Get(sdkCtx)
	if err != nil {
		return err
	}
	if !params.GetMetricsEnabled() {
		return types.ErrMetricsDisabled
	}

	// Get or create tool metrics
	metrics, err := k.state.ToolMetrics.Get(sdkCtx, toolID)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		metrics = types.NewActivationMetrics(toolID)
	}
	if metrics == nil {
		metrics = types.NewActivationMetrics(toolID)
	}

	// Update metrics
	now := sdkCtx.BlockTime()
	wasActive := len(metrics.ActiveSessions) > 0

	if activated {
		incrementCounter(&metrics.ActivationCount)
		metrics.SetLastActivated(now)

		// Add to active sessions if not already present
		found := false
		for _, s := range metrics.ActiveSessions {
			if sameMetricIdentifier(s, sessionID) {
				found = true
				break
			}
		}
		if !found {
			metrics.ActiveSessions = append(metrics.ActiveSessions, sessionID)
		}
	} else {
		incrementCounter(&metrics.DeactivationCount)

		// Remove from active sessions
		newSessions := []string{}
		for _, s := range metrics.ActiveSessions {
			if !sameMetricIdentifier(s, sessionID) {
				newSessions = append(newSessions, s)
			}
		}
		metrics.ActiveSessions = newSessions
	}

	isActive := len(metrics.ActiveSessions) > 0

	// Update global active tool count if status changed
	if wasActive != isActive {
		global, err := k.getOrInitGlobalMetrics(sdkCtx)
		if err != nil {
			return err
		}

		if isActive {
			incrementCounter(&global.TotalActiveTools)
		} else if global.TotalActiveTools > 0 {
			global.TotalActiveTools--
		}
		if err := k.state.GlobalMetrics.Set(sdkCtx, global); err != nil {
			return err
		}
	}

	// Save metrics
	if err := k.state.ToolMetrics.Set(sdkCtx, toolID, metrics); err != nil {
		return err
	}

	// Record the activation
	activationID := activationStorageKey(sessionID, toolID)
	activation := types.NewToolActivation(activationID, toolID, sessionID, reason, activated, now)

	if err := k.state.ActiveTools.Set(sdkCtx, activationID, activation); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeToolActivation,
			sdk.NewAttribute(types.AttributeKeyToolID, toolID),
			sdk.NewAttribute(types.AttributeKeySessionID, sessionID),
			sdk.NewAttribute(types.AttributeKeyActivated, fmt.Sprintf("%t", activated)),
			sdk.NewAttribute(types.AttributeKeyReason, reason),
		),
	)

	return nil
}

// RecordInvocation records a tool invocation event
func (k Keeper) RecordInvocation(ctx context.Context, toolID, sessionID, userAddress string,
	cost decimal.Decimal, latencyMs uint32, success bool, cacheHit bool, originToolID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check if metrics are enabled
	params, err := k.state.Params.Get(sdkCtx)
	if err != nil {
		return err
	}
	if !params.GetMetricsEnabled() {
		return types.ErrMetricsDisabled
	}

	// Update tool metrics
	metrics, err := k.state.ToolMetrics.Get(sdkCtx, toolID)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		metrics = types.NewActivationMetrics(toolID)
	}
	if metrics == nil {
		metrics = types.NewActivationMetrics(toolID)
	}

	// Update invocation metrics
	incrementCounter(&metrics.InvocationCount)
	metrics.SetTotalVolumeDecimal(metrics.TotalVolumeDecimal().Add(cost))
	now := sdkCtx.BlockTime()
	metrics.SetLastInvoked(now)

	// Update average latency (exponential moving average)
	latencyDec := decimal.NewFromInt(int64(latencyMs))
	currentAvg := metrics.AverageLatencyDecimal()
	if currentAvg.IsZero() {
		metrics.SetAverageLatencyDecimal(latencyDec)
	} else {
		updated := emaLatencyAlpha.Mul(latencyDec).Add(decimal.NewFromInt(1).Sub(emaLatencyAlpha).Mul(currentAvg))
		metrics.SetAverageLatencyDecimal(updated)
	}

	// Update success rate (exponential moving average)
	successVal := decimal.Zero
	if success {
		successVal = decimal.NewFromInt(100)
	}
	if metrics.InvocationCount == 1 {
		metrics.SetSuccessRateDecimal(successVal)
	} else {
		updated := emaSuccessAlpha.Mul(successVal).Add(decimal.NewFromInt(1).Sub(emaSuccessAlpha).Mul(metrics.SuccessRateDecimal()))
		metrics.SetSuccessRateDecimal(updated)
	}

	// Update P95 and P99 latencies (simplified top-2 tracking; P99 >= P95 always).
	if latencyMs > metrics.P99LatencyMs {
		metrics.P95LatencyMs = metrics.P99LatencyMs
		metrics.P99LatencyMs = latencyMs
	} else if latencyMs > metrics.P95LatencyMs {
		metrics.P95LatencyMs = latencyMs
	}

	if err := k.state.ToolMetrics.Set(sdkCtx, toolID, metrics); err != nil {
		return err
	}

	// Update session metrics
	sessionMetrics, err := k.state.SessionMetrics.Get(sdkCtx, sessionID)
	newSession := false
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		sessionMetrics = types.NewSessionMetrics(sessionID, userAddress, now)
		newSession = true
	}
	if sessionMetrics == nil {
		sessionMetrics = types.NewSessionMetrics(sessionID, userAddress, now)
		newSession = true
	}

	if newSession {
		global, err := k.getOrInitGlobalMetrics(sdkCtx)
		if err != nil {
			return err
		}
		incrementCounter(&global.ActiveSessions)
		if err := k.state.GlobalMetrics.Set(sdkCtx, global); err != nil {
			return err
		}
	}

	incrementCounter(&sessionMetrics.ToolsInvoked)
	sessionMetrics.SetTotalSpentDecimal(sessionMetrics.TotalSpentDecimal().Add(cost))

	// Add tool to used list if not present
	found := false
	for _, t := range sessionMetrics.ToolsUsed {
		if sameMetricIdentifier(t, toolID) {
			found = true
			break
		}
	}
	if !found {
		sessionMetrics.ToolsUsed = append(sessionMetrics.ToolsUsed, toolID)
	}

	// Update cache hit rate
	if sessionMetrics.ToolsInvoked > 0 {
		previousInvocations := sessionMetrics.ToolsInvoked - 1
		previousHits := sdkmath.NewIntFromUint64(previousInvocations).
			MulRaw(int64(sessionMetrics.CacheHitRate)).
			AddRaw(50). // Add 0.5 for rounding
			QuoRaw(100)
		hits := previousHits
		if cacheHit {
			hits = hits.AddRaw(1)
		}
		percent := hits.MulRaw(100).Quo(sdkmath.NewIntFromUint64(sessionMetrics.ToolsInvoked))
		if percent.IsNegative() {
			sessionMetrics.CacheHitRate = 0
		} else if percent.IsUint64() {
			rate := percent.Uint64()
			if rate > 100 {
				rate = 100
			}
			sessionMetrics.CacheHitRate = uint32(rate)
		}
	}

	// Update average latency
	if sessionMetrics.AverageLatencyMs == 0 {
		sessionMetrics.AverageLatencyMs = latencyMs
	} else if sessionMetrics.ToolsInvoked > 0 && sessionMetrics.ToolsInvoked <= uint64(stdmath.MaxInt64) {
		prevAvg := int64(sessionMetrics.AverageLatencyMs)
		current := int64(latencyMs)
		n := int64(sessionMetrics.ToolsInvoked)
		delta := current - prevAvg
		updated := prevAvg + delta/n
		if updated < 0 {
			updated = 0
		}
		sessionMetrics.AverageLatencyMs = uint32(updated) //#nosec G115 -- average latency always bounded by latencyMs values
	}

	if err := k.state.SessionMetrics.Set(sdkCtx, sessionID, sessionMetrics); err != nil {
		return err
	}

	// Record CAC hit if applicable
	if cacheHit && originToolID != "" && params.GetCacEnabled() {
		if err := k.RecordCACHit(ctx, originToolID, toolID, cost, params.GetCacRoyaltyBps()); err != nil {
			k.Logger(sdkCtx).Error("failed to record CAC hit", "error", err)
		}
	}

	// Update global metrics
	if err := k.updateGlobalMetrics(ctx, cost, success); err != nil {
		k.Logger(sdkCtx).Error("failed to update global metrics", "error", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeToolInvocation,
			sdk.NewAttribute(types.AttributeKeyToolID, toolID),
			sdk.NewAttribute(types.AttributeKeySessionID, sessionID),
			sdk.NewAttribute(types.AttributeKeyUserAddress, userAddress),
			sdk.NewAttribute(types.AttributeKeyCost, cost.String()),
			sdk.NewAttribute(types.AttributeKeyLatency, fmt.Sprintf("%d", latencyMs)),
			sdk.NewAttribute(types.AttributeKeySuccess, fmt.Sprintf("%t", success)),
			sdk.NewAttribute(types.AttributeKeyCacheHit, fmt.Sprintf("%t", cacheHit)),
		),
	)

	return nil
}

// DiscoverySubsidySpent returns cumulative discovery-subsidy spend for the
// current governance accounting period.
func (k Keeper) DiscoverySubsidySpent(ctx context.Context) (decimal.Decimal, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return k.discoverySubsidySpent(sdkCtx)
}

func (k Keeper) discoverySubsidySpent(ctx sdk.Context) (decimal.Decimal, error) {
	rawSpent, err := k.state.DiscoverySubsidySpent.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return decimal.Zero, nil
		}
		return decimal.Zero, err
	}
	spent, err := types.ParseDecimal(rawSpent)
	if err != nil {
		return decimal.Zero, types.ErrInvalidParams.Wrapf("invalid discovery_subsidy_spent: %v", err)
	}
	if spent.IsNegative() {
		return decimal.Zero, types.ErrInvalidParams.Wrap("discovery_subsidy_spent cannot be negative")
	}
	return spent, nil
}

// DiscoverySubsidySettlementTerms converts router governance accounting into
// credit-denominated coins for the credits/incentives settlement bridge.
func (k Keeper) DiscoverySubsidySettlementTerms(ctx context.Context, denom string) (uint32, sdk.Coin, sdk.Coin, error) {
	trimmedDenom := strings.TrimSpace(denom)
	if trimmedDenom == "" {
		return 0, sdk.Coin{}, sdk.Coin{}, types.ErrInvalidParams.Wrap("denom is required")
	}
	if trimmedDenom != denom {
		return 0, sdk.Coin{}, sdk.Coin{}, types.ErrInvalidParams.Wrap("denom must not contain leading or trailing whitespace")
	}
	if err := sdk.ValidateDenom(denom); err != nil {
		return 0, sdk.Coin{}, sdk.Coin{}, types.ErrInvalidParams.Wrapf("invalid denom: %v", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		return 0, sdk.Coin{}, sdk.Coin{}, err
	}
	spent, err := k.discoverySubsidySpent(sdkCtx)
	if err != nil {
		return 0, sdk.Coin{}, sdk.Coin{}, err
	}
	poolCap, err := types.ParseDecimal(params.GetDiscoverySubsidyPoolCap())
	if err != nil {
		return 0, sdk.Coin{}, sdk.Coin{}, types.ErrInvalidParams.Wrapf("invalid discovery_subsidy_pool_cap: %v", err)
	}

	poolCapCoin, err := discoverySubsidyDecimalToCoin(poolCap, denom)
	if err != nil {
		return 0, sdk.Coin{}, sdk.Coin{}, types.ErrInvalidParams.Wrapf("invalid discovery_subsidy_pool_cap: %v", err)
	}
	spentCoin, err := discoverySubsidyDecimalToCoin(spent, denom)
	if err != nil {
		return 0, sdk.Coin{}, sdk.Coin{}, types.ErrInvalidParams.Wrapf("invalid discovery_subsidy_spent: %v", err)
	}

	return params.GetDiscoverySubsidyBps(), poolCapCoin, spentCoin, nil
}

// RecordDiscoverySubsidyRebate accounts for a user rebate authorized for a real
// exploration-picked invocation. It updates router-local pool-spent accounting
// and session refund metrics; bank transfer from x/incentives remains a separate
// settlement integration step.
func (k Keeper) RecordDiscoverySubsidyRebate(ctx context.Context, sessionID, userAddress string,
	actualCost decimal.Decimal, explorationPick bool) (decimal.Decimal, error) {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return decimal.Zero, types.ErrInvalidParams.Wrap("session_id is required")
	}
	if !sameMetricIdentifier(trimmedSessionID, sessionID) {
		return decimal.Zero, types.ErrInvalidParams.Wrap("session_id must not contain leading or trailing whitespace")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		return decimal.Zero, err
	}
	spent, err := k.discoverySubsidySpent(sdkCtx)
	if err != nil {
		return decimal.Zero, err
	}
	rebate, err := params.DiscoverySubsidyRebate(actualCost, spent, explorationPick)
	if err != nil {
		return decimal.Zero, err
	}
	if rebate.IsZero() {
		return decimal.Zero, nil
	}

	if _, err := k.recordDiscoverySubsidyPaidDecimal(sdkCtx, sessionID, userAddress, actualCost, rebate, explorationPick); err != nil {
		return decimal.Zero, err
	}
	return rebate, nil
}

// RecordDiscoverySubsidyPaid records the exact credit-denominated rebate that
// the incentives module paid. It is intentionally separate from
// RecordDiscoverySubsidyRebate because live incentives settlement may clamp by
// module-account balance after router governance math has capped the rebate.
func (k Keeper) RecordDiscoverySubsidyPaid(ctx context.Context, sessionID, userAddress string,
	actualCost sdk.Coin, rebate sdk.Coin, explorationPick bool) (sdk.Coin, error) {
	zero, err := discoverySubsidyZeroCoin(actualCost.Denom)
	if err != nil {
		return sdk.Coin{}, err
	}
	if !explorationPick || rebate.IsZero() {
		return zero, nil
	}
	if !actualCost.IsValid() || actualCost.IsNegative() {
		return sdk.Coin{}, types.ErrInvalidParams.Wrap("actual_cost must be a valid non-negative coin")
	}
	if !rebate.IsValid() || rebate.IsNegative() {
		return sdk.Coin{}, types.ErrInvalidParams.Wrap("rebate must be a valid non-negative coin")
	}
	if rebate.Denom != actualCost.Denom {
		return sdk.Coin{}, types.ErrInvalidParams.Wrapf("rebate denom %s does not match actual_cost denom %s", rebate.Denom, actualCost.Denom)
	}

	actualCostDecimal := discoverySubsidyCoinToDecimal(actualCost)
	rebateDecimal := discoverySubsidyCoinToDecimal(rebate)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if _, err := k.recordDiscoverySubsidyPaidDecimal(sdkCtx, sessionID, userAddress, actualCostDecimal, rebateDecimal, explorationPick); err != nil {
		return zero, err
	}
	return rebate, nil
}

func (k Keeper) recordDiscoverySubsidyPaidDecimal(sdkCtx sdk.Context, sessionID, userAddress string,
	actualCost decimal.Decimal, rebate decimal.Decimal, explorationPick bool) (decimal.Decimal, error) {
	if !explorationPick || rebate.IsZero() {
		return decimal.Zero, nil
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return decimal.Zero, types.ErrInvalidParams.Wrap("session_id is required")
	}
	if !sameMetricIdentifier(trimmedSessionID, sessionID) {
		return decimal.Zero, types.ErrInvalidParams.Wrap("session_id must not contain leading or trailing whitespace")
	}
	if !moneyguard.IsSafeExponent(actualCost) {
		return decimal.Zero, types.ErrInvalidParams.Wrap("actual_cost exponent out of safe range")
	}
	if !moneyguard.IsSafeExponent(rebate) {
		return decimal.Zero, types.ErrInvalidParams.Wrap("rebate exponent out of safe range")
	}
	if actualCost.IsNegative() {
		return decimal.Zero, types.ErrInvalidParams.Wrap("actual_cost cannot be negative")
	}
	if rebate.IsNegative() {
		return decimal.Zero, types.ErrInvalidParams.Wrap("rebate cannot be negative")
	}

	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		return decimal.Zero, err
	}
	spent, err := k.discoverySubsidySpent(sdkCtx)
	if err != nil {
		return decimal.Zero, err
	}
	poolCap, err := types.ParseDecimal(params.GetDiscoverySubsidyPoolCap())
	if err != nil {
		return decimal.Zero, types.ErrInvalidParams.Wrapf("invalid discovery_subsidy_pool_cap: %v", err)
	}
	remaining := poolCap.Sub(spent)
	if !remaining.IsPositive() {
		return decimal.Zero, nil
	}
	if rebate.GreaterThan(remaining) {
		return decimal.Zero, types.ErrInvalidParams.Wrap("rebate exceeds remaining discovery subsidy pool")
	}

	nextSpent := spent.Add(rebate)
	if err := k.state.DiscoverySubsidySpent.Set(sdkCtx, nextSpent.String()); err != nil {
		return decimal.Zero, err
	}

	sessionMetrics, err := k.state.SessionMetrics.Get(sdkCtx, sessionID)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return decimal.Zero, err
		}
		sessionMetrics = types.NewSessionMetrics(sessionID, userAddress, sdkCtx.BlockTime())
	}
	if sessionMetrics == nil {
		sessionMetrics = types.NewSessionMetrics(sessionID, userAddress, sdkCtx.BlockTime())
	}
	currentRefunded, err := sessionMetrics.TotalRefundedDecimalSafe()
	if err != nil {
		return decimal.Zero, types.ErrInvalidParams.Wrapf("invalid session total_refunded: %v", err)
	}
	sessionMetrics.SetTotalRefundedDecimal(currentRefunded.Add(rebate))
	if err := k.state.SessionMetrics.Set(sdkCtx, sessionID, sessionMetrics); err != nil {
		return decimal.Zero, err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeDiscoverySubsidy,
			sdk.NewAttribute(types.AttributeKeySessionID, sessionID),
			sdk.NewAttribute(types.AttributeKeyUserAddress, userAddress),
			sdk.NewAttribute(types.AttributeKeyCost, actualCost.String()),
			sdk.NewAttribute(types.AttributeKeyRebateAmount, rebate.String()),
			sdk.NewAttribute(types.AttributeKeySubsidySpent, nextSpent.String()),
			sdk.NewAttribute(types.AttributeKeySubsidyPoolCap, params.GetDiscoverySubsidyPoolCap()),
			sdk.NewAttribute(types.AttributeKeyExplorationPick, fmt.Sprintf("%t", explorationPick)),
		),
	)

	return rebate, nil
}

func discoverySubsidyDecimalToCoin(amount decimal.Decimal, denom string) (sdk.Coin, error) {
	if amount.IsZero() {
		return sdk.NewCoin(denom, sdkmath.ZeroInt()), nil
	}
	if amount.IsNegative() {
		return sdk.Coin{}, fmt.Errorf("amount must be non-negative")
	}
	scaled := amount.Shift(discoverySubsidyCreditDecimalPlaces)
	if !scaled.IsInteger() {
		return sdk.Coin{}, fmt.Errorf("amount must not exceed %d decimal places", discoverySubsidyCreditDecimalPlaces)
	}
	bigInt := scaled.BigInt()
	if bigInt == nil || bigInt.Sign() < 0 {
		return sdk.Coin{}, fmt.Errorf("invalid amount")
	}
	return sdk.NewCoin(denom, sdkmath.NewIntFromBigInt(bigInt)), nil
}

func discoverySubsidyCoinToDecimal(coin sdk.Coin) decimal.Decimal {
	return decimal.NewFromBigInt(coin.Amount.BigInt(), 0).Shift(-discoverySubsidyCreditDecimalPlaces)
}

func discoverySubsidyZeroCoin(denom string) (sdk.Coin, error) {
	trimmedDenom := strings.TrimSpace(denom)
	if trimmedDenom == "" {
		return sdk.Coin{}, types.ErrInvalidParams.Wrap("denom is required")
	}
	if trimmedDenom != denom {
		return sdk.Coin{}, types.ErrInvalidParams.Wrap("denom must not contain leading or trailing whitespace")
	}
	if err := sdk.ValidateDenom(denom); err != nil {
		return sdk.Coin{}, types.ErrInvalidParams.Wrapf("invalid denom: %v", err)
	}
	return sdk.NewCoin(denom, sdkmath.ZeroInt()), nil
}

// RecordCACHit records a content-addressed cache hit for royalty tracking
func (k Keeper) RecordCACHit(ctx context.Context, originToolID, consumingToolID string,
	invocationCost decimal.Decimal, royaltyBPS uint32) error {
	// Calculate royalty amount
	royaltyAmount := invocationCost.Mul(decimal.NewFromInt(int64(royaltyBPS))).Div(decimal.NewFromInt(10000))
	return k.RecordCACHitDirect(ctx, originToolID, consumingToolID, royaltyAmount)
}

func validateCACRoyaltyToolID(field, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return types.ErrInvalidParams.Wrapf("%s is required", field)
	}
	if trimmed != value {
		return types.ErrInvalidParams.Wrapf("%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

// RecordCACHitDirect records a content-addressed cache hit with pre-calculated royalty
func (k Keeper) RecordCACHitDirect(ctx context.Context, originToolID, consumingToolID string,
	royaltyAmount decimal.Decimal) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if err := validateCACRoyaltyToolID("origin_tool_id", originToolID); err != nil {
		return err
	}
	if err := validateCACRoyaltyToolID("consuming_tool_id", consumingToolID); err != nil {
		return err
	}

	// Generate record ID
	seq, err := k.state.CACRecordCounter.Next(sdkCtx)
	if err != nil {
		return err
	}
	recordID := fmt.Sprintf("cac-%d", seq)

	// Create CAC record
	record := &types.CACRoyaltyRecord{
		CacheKey:        recordID,
		OriginToolId:    originToolID,
		ConsumingToolId: consumingToolID,
		HitCount:        1,
		BlockHeight:     uint64(sdkCtx.BlockHeight()), //#nosec G115 -- block heights always non-negative
	}
	record.SetRoyaltyAmountDecimal(royaltyAmount)
	record.SetTotalRoyaltiesEarnedDecimal(royaltyAmount)
	record.SetTimestamp(sdkCtx.BlockTime())

	// Check if there's an existing record for this origin-consumer pair via composite index
	compositeKey := types.CACCompositeKey(originToolID, consumingToolID)
	if key, err := k.state.CACRecords.Indexes.Composite.MatchExact(sdkCtx, compositeKey); err == nil {
		if existing, getErr := k.state.CACRecords.Get(sdkCtx, key); getErr == nil {
			record.HitCount += existing.HitCount
			record.SetTotalRoyaltiesEarnedDecimal(
				record.TotalRoyaltiesEarnedDecimal().Add(existing.TotalRoyaltiesEarnedDecimal()),
			)
			recordID = key
			record.CacheKey = key
		}
	}

	// Save the record
	if err := k.state.CACRecords.Set(sdkCtx, recordID, record); err != nil {
		return err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeCACHit,
			sdk.NewAttribute(types.AttributeKeyOriginToolID, originToolID),
			sdk.NewAttribute(types.AttributeKeyConsumingToolID, consumingToolID),
			sdk.NewAttribute(types.AttributeKeyRoyaltyAmount, royaltyAmount.String()),
			sdk.NewAttribute(types.AttributeKeyTotalRoyalties, record.TotalRoyaltiesEarnedDecimal().String()),
		),
	)

	return nil
}

// getOrInitGlobalMetrics retrieves global metrics or initializes them if not found.
func (k Keeper) getOrInitGlobalMetrics(ctx sdk.Context) (*types.GlobalMetrics, error) {
	metrics, err := k.state.GlobalMetrics.Get(ctx)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return nil, err
		}
		metrics = &types.GlobalMetrics{}
		metrics.SetMetricsStart(ctx.BlockTime())
	}
	if metrics == nil {
		metrics = &types.GlobalMetrics{}
		metrics.SetMetricsStart(ctx.BlockTime())
	}
	return metrics, nil
}

// updateGlobalMetrics updates system-wide metrics
func (k Keeper) updateGlobalMetrics(ctx context.Context, amount decimal.Decimal, _ bool) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	metrics, err := k.getOrInitGlobalMetrics(sdkCtx)
	if err != nil {
		return err
	}

	// Update metrics
	incrementCounter(&metrics.TotalInvocations)
	metrics.SetTotalVolumeDecimal(metrics.TotalVolumeDecimal().Add(amount))
	metrics.SetLastUpdated(sdkCtx.BlockTime())
	metrics.BlockHeight = uint64(sdkCtx.BlockHeight()) //#nosec G115 -- block heights always non-negative

	return k.state.GlobalMetrics.Set(sdkCtx, metrics)
}

func incrementCounter(counter *uint64) {
	if counter == nil || *counter == ^uint64(0) {
		return
	}
	*counter++
}

func decimalFromUint64(value uint64) decimal.Decimal {
	return decimal.NewFromBigInt(new(big.Int).SetUint64(value), 0)
}

func activationStorageKey(sessionID, toolID string) string {
	s := strings.TrimSpace(sessionID)
	t := strings.TrimSpace(toolID)
	return fmt.Sprintf("%d|%s|%s", len(s), s, t)
}
