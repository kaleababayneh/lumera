package keeper

import (
	"fmt"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	metrics "github.com/hashicorp/go-metrics"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

const (
	adaptiveBurnWindowDays = 30
	adaptiveBurnMinSamples = 100
	adaptiveBurnStepBps    = 25
)

type adaptiveBurnStats struct {
	TrailingBurnAmount     sdkmath.Int
	SampleCount            int
	Supply                 sdkmath.Int
	AnnualizedDeflationBps uint32
	PeriodContractionBps   uint32
}

type AdaptiveBurnAdjustment struct {
	OldRateBps             uint32
	RequestedRateBps       uint32
	NewRateBps             uint32
	AdjustmentBps          int32
	AnnualizedDeflationBps uint32
	PeriodContractionBps   uint32
	TargetDeflationBps     uint32
	TrailingBurnAmount     sdkmath.Int
	SampleCount            int
	Direction              string
	Reason                 string
	ClampReason            string
	InsufficientData       bool
	DeathSpiralTriggered   bool
}

// AdaptiveBurnWindowDuration returns the trailing retention window required by adaptive burn logic.
func AdaptiveBurnWindowDuration() time.Duration {
	return adaptiveBurnWindowDays * 24 * time.Hour
}

// MaybeAdjustAdaptiveBurnRate evaluates adaptive burn at the configured epoch boundary.
func (k Keeper) MaybeAdjustAdaptiveBurnRate(ctx sdk.Context) (*AdaptiveBurnAdjustment, error) {
	params := k.GetParams(ctx)
	if params == nil || params.BurnRateAdjustmentEpoch == 0 {
		return nil, nil
	}
	if ctx.BlockHeight() <= 0 || ctx.BlockHeight()%int64(params.BurnRateAdjustmentEpoch) != 0 {
		return nil, nil
	}

	stats, err := k.collectAdaptiveBurnStats(ctx)
	if err != nil {
		return nil, err
	}

	adjustment := evaluateAdaptiveBurnAdjustment(params, stats)
	k.emitAdaptiveBurnTelemetry(ctx, adjustment)

	logger := k.Logger(ctx)
	if adjustment.InsufficientData {
		logger.Info(
			"adaptive burn skipped: insufficient trailing data",
			"data_points", adjustment.SampleCount,
			"min_required", adaptiveBurnMinSamples,
		)
		return adjustment, nil
	}

	logger.Info(
		"adaptive burn rate evaluation",
		"trailing_burn_lac", adjustment.TrailingBurnAmount.String(),
		"annualized_rate_bps", adjustment.AnnualizedDeflationBps,
		"target_bps", adjustment.TargetDeflationBps,
		"adjustment_bps", adjustment.AdjustmentBps,
		"new_rate_bps", adjustment.NewRateBps,
		"reason", adjustment.Reason,
		"period_contraction_bps", adjustment.PeriodContractionBps,
	)

	if adjustment.ClampReason != "" {
		logger.Warn(
			"adaptive burn adjustment clamped",
			"requested_rate_bps", adjustment.RequestedRateBps,
			"clamped_to_bps", adjustment.NewRateBps,
			"reason", adjustment.ClampReason,
		)
	}

	if adjustment.NewRateBps != adjustment.OldRateBps {
		params.BurnRateSpendBps = adjustment.NewRateBps
		if err := k.SetParams(ctx, params); err != nil {
			return nil, fmt.Errorf("set adaptive burn params: %w", err)
		}
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"adaptive_burn_rate_evaluated",
			sdk.NewAttribute("old_rate_bps", fmt.Sprintf("%d", adjustment.OldRateBps)),
			sdk.NewAttribute("requested_rate_bps", fmt.Sprintf("%d", adjustment.RequestedRateBps)),
			sdk.NewAttribute("new_rate_bps", fmt.Sprintf("%d", adjustment.NewRateBps)),
			sdk.NewAttribute("annualized_deflation_bps", fmt.Sprintf("%d", adjustment.AnnualizedDeflationBps)),
			sdk.NewAttribute("target_annual_deflation_bps", fmt.Sprintf("%d", adjustment.TargetDeflationBps)),
			sdk.NewAttribute("period_contraction_bps", fmt.Sprintf("%d", adjustment.PeriodContractionBps)),
			sdk.NewAttribute("sample_count", fmt.Sprintf("%d", adjustment.SampleCount)),
			sdk.NewAttribute("trailing_burn_lac", adjustment.TrailingBurnAmount.String()),
		),
	)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"adaptive_burn_rate_reason",
			sdk.NewAttribute("direction", adjustment.Direction),
			sdk.NewAttribute("reason", adjustment.Reason),
			sdk.NewAttribute("clamp_reason", adjustment.ClampReason),
			sdk.NewAttribute("death_spiral_triggered", fmt.Sprintf("%t", adjustment.DeathSpiralTriggered)),
		),
	)

	return adjustment, nil
}

func (k Keeper) collectAdaptiveBurnStats(ctx sdk.Context) (adaptiveBurnStats, error) {
	params := k.GetParams(ctx)
	if params == nil {
		return adaptiveBurnStats{}, fmt.Errorf("credits params unavailable")
	}

	windowStart := ctx.BlockTime().Add(-AdaptiveBurnWindowDuration())
	stats := adaptiveBurnStats{
		TrailingBurnAmount: sdkmath.ZeroInt(),
		Supply:             k.bankKeeper.GetSupply(ctx, params.CreditDenom).Amount,
	}

	rng := new(collections.Range[collections.Pair[time.Time, string]]).
		StartInclusive(collections.Join(windowStart, ""))

	if err := k.state.SettlementsByTime.Walk(ctx, rng, func(key collections.Pair[time.Time, string]) (bool, error) {
		settlement, found := k.GetSettlement(ctx, key.K2())
		if !found || settlement == nil || settlement.Status != types.SettlementStatus_SETTLEMENT_STATUS_COMPLETED {
			return false, nil
		}

		stats.SampleCount++
		for _, coin := range types.CoinsFromProto(settlement.BurnAmount) {
			if coin.Denom == params.CreditDenom && coin.Amount.IsPositive() {
				stats.TrailingBurnAmount = stats.TrailingBurnAmount.Add(coin.Amount)
			}
		}
		return false, nil
	}); err != nil {
		return adaptiveBurnStats{}, fmt.Errorf("walk settlements by time: %w", err)
	}

	stats.AnnualizedDeflationBps = scaledRatioBps(stats.TrailingBurnAmount, stats.Supply, 365, adaptiveBurnWindowDays)
	stats.PeriodContractionBps = scaledRatioBps(stats.TrailingBurnAmount, stats.Supply, 1, 1)

	return stats, nil
}

func evaluateAdaptiveBurnAdjustment(params *types.Params, stats adaptiveBurnStats) *AdaptiveBurnAdjustment {
	adjustment := &AdaptiveBurnAdjustment{
		OldRateBps:             params.BurnRateSpendBps,
		RequestedRateBps:       params.BurnRateSpendBps,
		NewRateBps:             params.BurnRateSpendBps,
		AnnualizedDeflationBps: stats.AnnualizedDeflationBps,
		PeriodContractionBps:   stats.PeriodContractionBps,
		TargetDeflationBps:     params.TargetAnnualDeflationBps,
		TrailingBurnAmount:     stats.TrailingBurnAmount,
		SampleCount:            stats.SampleCount,
		Direction:              "none",
		Reason:                 "within_target_band",
	}

	if stats.SampleCount < adaptiveBurnMinSamples || !stats.Supply.IsPositive() {
		adjustment.InsufficientData = true
		adjustment.Reason = "insufficient_data"
		return adjustment
	}

	lowerBound := params.TargetAnnualDeflationBps * 8 / 10
	upperBound := params.TargetAnnualDeflationBps * 12 / 10

	switch {
	case stats.AnnualizedDeflationBps > upperBound:
		if params.BurnRateSpendBps > adaptiveBurnStepBps {
			adjustment.RequestedRateBps = params.BurnRateSpendBps - adaptiveBurnStepBps
		} else {
			adjustment.RequestedRateBps = 0
		}
		adjustment.Direction = "down"
		adjustment.Reason = "above_target_band"
	case stats.AnnualizedDeflationBps < lowerBound:
		adjustment.RequestedRateBps = params.BurnRateSpendBps + adaptiveBurnStepBps
		adjustment.Direction = "up"
		adjustment.Reason = "below_target_band"
	}

	adjustment.NewRateBps = clampUint32(adjustment.RequestedRateBps, params.MinBurnRateSpendBps, params.MaxBurnRateSpendBps)
	if adjustment.NewRateBps != adjustment.RequestedRateBps {
		if adjustment.NewRateBps == params.MinBurnRateSpendBps {
			adjustment.ClampReason = "min_burn_rate"
		} else {
			adjustment.ClampReason = "max_burn_rate"
		}
	}

	if params.DeathSpiralSupplyContractionBps > 0 &&
		stats.PeriodContractionBps > params.DeathSpiralSupplyContractionBps &&
		adjustment.NewRateBps > params.DeathSpiralBurnRateCapBps {
		adjustment.DeathSpiralTriggered = true
		adjustment.NewRateBps = params.DeathSpiralBurnRateCapBps
		if adjustment.ClampReason == "" {
			adjustment.ClampReason = "death_spiral_cap"
		} else {
			adjustment.ClampReason += ",death_spiral_cap"
		}
		adjustment.Reason = "death_spiral_cap"
	}

	adjustment.AdjustmentBps = int32(adjustment.NewRateBps) - int32(adjustment.OldRateBps)
	switch {
	case adjustment.AdjustmentBps > 0:
		adjustment.Direction = "up"
	case adjustment.AdjustmentBps < 0:
		adjustment.Direction = "down"
	default:
		adjustment.Direction = "none"
	}

	return adjustment
}

func clampUint32(value, lower, upper uint32) uint32 {
	if value < lower {
		return lower
	}
	if value > upper {
		return upper
	}
	return value
}

func scaledRatioBps(numerator, denominator sdkmath.Int, multiplier, divisor int64) uint32 {
	if !numerator.IsPositive() || !denominator.IsPositive() || multiplier <= 0 || divisor <= 0 {
		return 0
	}

	value := sdkmath.LegacyNewDecFromInt(numerator).
		MulInt64(int64(types.MaxBasisPoints)).
		MulInt64(multiplier).
		Quo(sdkmath.LegacyNewDecFromInt(denominator).MulInt64(divisor))
	if value.IsNegative() {
		return 0
	}

	result := value.TruncateInt()
	if !result.IsPositive() {
		return 0
	}
	maxUint32 := sdkmath.NewInt(int64(^uint32(0)))
	if result.GT(maxUint32) {
		return ^uint32(0)
	}

	return uint32(result.Uint64())
}

func (k Keeper) emitAdaptiveBurnTelemetry(_ sdk.Context, adjustment *AdaptiveBurnAdjustment) {
	if adjustment == nil {
		return
	}

	telemetry.SetGauge(float32(adjustment.NewRateBps), types.ModuleName, "adaptive_burn_rate_bps")
	telemetry.SetGauge(float32(adjustment.AnnualizedDeflationBps), types.ModuleName, "adaptive_burn_annualized_deflation_bps")
	telemetry.SetGauge(float32(adjustment.PeriodContractionBps), types.ModuleName, "adaptive_burn_period_contraction_bps")
	trailingBurnDec := sdkmath.LegacyNewDecFromInt(adjustment.TrailingBurnAmount)
	telemetry.SetGauge(float32(trailingBurnDec.MustFloat64()), types.ModuleName, "adaptive_burn_trailing_volume_lac")
	telemetry.IncrCounterWithLabels(
		[]string{types.ModuleName, "adaptive_burn_adjustments_total"},
		1,
		[]metrics.Label{telemetry.NewLabel("direction", adjustment.Direction)},
	)
	if adjustment.DeathSpiralTriggered {
		telemetry.IncrCounterWithLabels(
			[]string{types.ModuleName, "adaptive_burn_death_spiral_total"},
			1,
			[]metrics.Label{telemetry.NewLabel("trigger", "true")},
		)
	}
}
