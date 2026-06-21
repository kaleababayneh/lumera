package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// Table-driven unit tests for evaluateAdaptiveBurnAdjustment — the
// pure decision engine that chooses the new burn rate each epoch
// given the module params and the 30-day settlement stats.
//
// Why this needs a dedicated test
// -------------------------------
// MaybeAdjustAdaptiveBurnRate wraps this function behind a full
// keeper context and settlement walk; the existing end-to-end tests
// hit the happy path but the branching matrix inside
// evaluateAdaptiveBurnAdjustment was unpinned. That branching decides
// — every epoch, chain-wide — whether β_spend ticks up, down, clamps
// to min/max, or gets overridden by the death-spiral cap. A silent
// regression here is an irreversible monetary-policy drift that
// every node applies.
//
// The step constant (25 bps = adaptiveBurnStepBps) and minimum
// sample requirement (100 = adaptiveBurnMinSamples) are pinned
// inside the assertions; a refactor that touches either is caught.

// baseParams returns a params fixture with sensible defaults for the
// adaptive-burn fields. Individual tests override only the fields
// they care about, keeping assertions focused on the branch under
// test.
func baseParams() *types.Params {
	return &types.Params{
		BurnRateSpendBps:                300,  // current β_spend
		TargetAnnualDeflationBps:        1000, // 10% target
		MinBurnRateSpendBps:             100,
		MaxBurnRateSpendBps:             500,
		DeathSpiralSupplyContractionBps: 0, // disabled by default
		DeathSpiralBurnRateCapBps:       150,
	}
}

func positiveSupply() sdkmath.Int { return sdkmath.NewInt(1_000_000) }

func TestEvaluateAdaptiveBurnAdjustment(t *testing.T) {
	t.Parallel()

	// targetBand limits derive from the implementation:
	//   lower = target * 8 / 10
	//   upper = target * 12 / 10
	// For a 1000bps target the band is [800, 1200].

	tests := []struct {
		name   string
		params func() *types.Params
		stats  adaptiveBurnStats
		assert func(t *testing.T, got *AdaptiveBurnAdjustment)
	}{
		{
			name:   "insufficient_samples_short_circuits",
			params: baseParams,
			stats: adaptiveBurnStats{
				Supply:      positiveSupply(),
				SampleCount: adaptiveBurnMinSamples - 1, // 99, below threshold
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.True(t, got.InsufficientData)
				require.Equal(t, "insufficient_data", got.Reason)
				require.Equal(t, "none", got.Direction)
				require.Equal(t, uint32(300), got.OldRateBps)
				require.Equal(t, uint32(300), got.NewRateBps,
					"burn rate must NOT change when sample count is too low")
				require.Equal(t, uint32(300), got.RequestedRateBps)
				require.Equal(t, int32(0), got.AdjustmentBps)
			},
		},
		{
			name:   "zero_supply_short_circuits",
			params: baseParams,
			stats: adaptiveBurnStats{
				Supply:      sdkmath.ZeroInt(),
				SampleCount: 500, // plenty of samples, but supply is zero
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.True(t, got.InsufficientData,
					"non-positive supply must short-circuit — otherwise "+
						"the ratio math would divide by zero downstream")
				require.Equal(t, "insufficient_data", got.Reason)
				require.Equal(t, uint32(300), got.NewRateBps)
			},
		},
		{
			name:   "negative_supply_short_circuits",
			params: baseParams,
			stats: adaptiveBurnStats{
				Supply:      sdkmath.NewInt(-1),
				SampleCount: 500,
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.True(t, got.InsufficientData)
				require.Equal(t, "insufficient_data", got.Reason)
			},
		},
		{
			name:   "within_target_band_lower_edge_no_change",
			params: baseParams,
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 800, // exactly lowerBound — NOT below
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.False(t, got.InsufficientData)
				require.Equal(t, "within_target_band", got.Reason)
				require.Equal(t, "none", got.Direction)
				require.Equal(t, uint32(300), got.NewRateBps)
				require.Equal(t, int32(0), got.AdjustmentBps)
				require.Empty(t, got.ClampReason)
			},
		},
		{
			name:   "within_target_band_upper_edge_no_change",
			params: baseParams,
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 1200, // exactly upperBound
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.Equal(t, "within_target_band", got.Reason)
				require.Equal(t, "none", got.Direction)
				require.Equal(t, uint32(300), got.NewRateBps)
			},
		},
		{
			name:   "above_band_decreases_rate_by_step",
			params: baseParams,
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 1500, // above 1200 upper
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.Equal(t, "above_target_band", got.Reason)
				require.Equal(t, "down", got.Direction)
				require.Equal(t, uint32(300-adaptiveBurnStepBps), got.RequestedRateBps)
				require.Equal(t, uint32(300-adaptiveBurnStepBps), got.NewRateBps,
					"275 is still above the min of 100 — no clamp")
				require.Equal(t, int32(-adaptiveBurnStepBps), got.AdjustmentBps)
				require.Empty(t, got.ClampReason)
			},
		},
		{
			name: "above_band_floors_at_zero_when_step_exceeds_rate",
			params: func() *types.Params {
				p := baseParams()
				p.BurnRateSpendBps = 10 // below one step
				p.MinBurnRateSpendBps = 0
				return p
			},
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 2000, // well above band
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.Equal(t, "above_target_band", got.Reason)
				require.Equal(t, uint32(0), got.RequestedRateBps,
					"subtracting the step from 10 would underflow; "+
						"the implementation floors at zero")
				require.Equal(t, uint32(0), got.NewRateBps)
			},
		},
		{
			name:   "below_band_increases_rate_by_step",
			params: baseParams,
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 500, // below 800 lower
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.Equal(t, "below_target_band", got.Reason)
				require.Equal(t, "up", got.Direction)
				require.Equal(t, uint32(300+adaptiveBurnStepBps), got.RequestedRateBps)
				require.Equal(t, uint32(300+adaptiveBurnStepBps), got.NewRateBps)
				require.Equal(t, int32(adaptiveBurnStepBps), got.AdjustmentBps)
				require.Empty(t, got.ClampReason)
			},
		},
		{
			name: "down_step_clamps_to_min_and_sets_ClampReason",
			params: func() *types.Params {
				p := baseParams()
				p.BurnRateSpendBps = 110
				p.MinBurnRateSpendBps = 100 // step would go to 85 — below min
				return p
			},
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 1500,
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.Equal(t, "above_target_band", got.Reason)
				require.Equal(t, uint32(110-adaptiveBurnStepBps), got.RequestedRateBps)
				require.Equal(t, uint32(100), got.NewRateBps,
					"clamp pulled the result up to MinBurnRateSpendBps")
				require.Equal(t, "min_burn_rate", got.ClampReason)
				require.Equal(t, int32(-10), got.AdjustmentBps,
					"delta is old (110) to clamped new (100)")
			},
		},
		{
			name: "up_step_clamps_to_max_and_sets_ClampReason",
			params: func() *types.Params {
				p := baseParams()
				p.BurnRateSpendBps = 490
				p.MaxBurnRateSpendBps = 500 // step would go to 515 — above max
				return p
			},
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 500,
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.Equal(t, "below_target_band", got.Reason)
				require.Equal(t, uint32(490+adaptiveBurnStepBps), got.RequestedRateBps)
				require.Equal(t, uint32(500), got.NewRateBps,
					"clamp pulled the result down to MaxBurnRateSpendBps")
				require.Equal(t, "max_burn_rate", got.ClampReason)
				require.Equal(t, int32(10), got.AdjustmentBps)
			},
		},
		{
			name: "death_spiral_triggers_cap_and_overrides_reason",
			params: func() *types.Params {
				p := baseParams()
				p.BurnRateSpendBps = 300
				p.DeathSpiralSupplyContractionBps = 500 // trip threshold
				p.DeathSpiralBurnRateCapBps = 150       // hard cap
				return p
			},
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 500,  // would normally step UP
				PeriodContractionBps:   1000, // exceeds 500 trip threshold
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.True(t, got.DeathSpiralTriggered,
					"trailing-window contraction > threshold must trip cap")
				require.Equal(t, uint32(150), got.NewRateBps,
					"burn rate must be clamped to DeathSpiralBurnRateCapBps")
				require.Equal(t, "death_spiral_cap", got.Reason,
					"death spiral overrides the below_target_band reason")
				require.Equal(t, "death_spiral_cap", got.ClampReason)
				// Old 300 -> new 150 -> AdjustmentBps -150
				require.Equal(t, int32(-150), got.AdjustmentBps)
				require.Equal(t, "down", got.Direction,
					"Direction recomputed from the sign of AdjustmentBps")
			},
		},
		{
			name: "death_spiral_appends_to_existing_ClampReason",
			params: func() *types.Params {
				p := baseParams()
				p.BurnRateSpendBps = 490
				p.MaxBurnRateSpendBps = 500             // normal up-step would clamp
				p.DeathSpiralSupplyContractionBps = 500 // trip threshold
				p.DeathSpiralBurnRateCapBps = 200       // cap below max-clamp target
				return p
			},
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 500,  // below band → up-step
				PeriodContractionBps:   1000, // death spiral trip
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.True(t, got.DeathSpiralTriggered)
				require.Equal(t, uint32(200), got.NewRateBps,
					"death spiral cap (200) overrides max clamp (500)")
				require.Equal(t, "max_burn_rate,death_spiral_cap", got.ClampReason,
					"ClampReason must append death_spiral_cap onto the "+
						"pre-existing max_burn_rate, preserving audit trail")
			},
		},
		{
			name: "death_spiral_below_cap_no_override",
			params: func() *types.Params {
				p := baseParams()
				p.BurnRateSpendBps = 100 // already below cap
				p.DeathSpiralSupplyContractionBps = 500
				p.DeathSpiralBurnRateCapBps = 150
				return p
			},
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 1000, // within band
				PeriodContractionBps:   1000, // trips threshold
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.False(t, got.DeathSpiralTriggered,
					"current rate 100 <= cap 150 means the death-spiral "+
						"branch's `NewRateBps > cap` guard is false — "+
						"the cap does NOT engage")
				require.Equal(t, uint32(100), got.NewRateBps)
				require.Empty(t, got.ClampReason)
			},
		},
		{
			name: "death_spiral_disabled_when_threshold_is_zero",
			params: func() *types.Params {
				p := baseParams()
				p.BurnRateSpendBps = 300
				p.DeathSpiralSupplyContractionBps = 0 // disabled
				p.DeathSpiralBurnRateCapBps = 150
				return p
			},
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 1000,
				PeriodContractionBps:   10_000, // huge — would trip if enabled
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.False(t, got.DeathSpiralTriggered,
					"zero threshold is the disable flag; the branch "+
						"condition `threshold > 0` must short-circuit")
				require.Equal(t, uint32(300), got.NewRateBps)
			},
		},
		{
			name:   "adjustment_fields_populated_from_stats",
			params: baseParams,
			stats: adaptiveBurnStats{
				Supply:                 positiveSupply(),
				SampleCount:            500,
				AnnualizedDeflationBps: 1000, // within band
				PeriodContractionBps:   111,
				TrailingBurnAmount:     sdkmath.NewInt(42_000),
			},
			assert: func(t *testing.T, got *AdaptiveBurnAdjustment) {
				require.Equal(t, uint32(1000), got.AnnualizedDeflationBps,
					"stats pass-through: AnnualizedDeflationBps")
				require.Equal(t, uint32(111), got.PeriodContractionBps,
					"stats pass-through: PeriodContractionBps")
				require.Equal(t, uint32(1000), got.TargetDeflationBps,
					"params pass-through: TargetAnnualDeflationBps")
				require.Equal(t, int64(42_000), got.TrailingBurnAmount.Int64(),
					"stats pass-through: TrailingBurnAmount")
				require.Equal(t, 500, got.SampleCount)
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			params := tc.params()
			got := evaluateAdaptiveBurnAdjustment(params, tc.stats)
			require.NotNil(t, got,
				"evaluateAdaptiveBurnAdjustment must never return nil")
			require.Equal(t, params.BurnRateSpendBps, got.OldRateBps,
				"OldRateBps must always echo the params input so callers "+
					"can diff old vs new without re-reading params")
			tc.assert(t, got)
		})
	}
}
