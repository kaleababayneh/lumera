//go:build cosmos

package keeper

import (
	"testing"

	sdkmath "cosmossdk.io/math"
)

// Property tests for weightedMedianDec and filterProviderOutliers.
// These two helpers sit on the hot path from raw provider samples to
// the canonical price that enters consensus via BuildCanonicalPriceFeeds,
// but neither has had direct test coverage — only end-to-end
// aggregation tests exercised them, so invariant regressions could
// slip past.
//
// Invariants pinned here:
//
//   weightedMedianDec
//     • empty input returns zero
//     • single sample returns that sample's price (regardless of weight)
//     • tied prices across providers resolve deterministically via the
//       provider-id → canonical-key tiebreak documented in the impl
//     • total-weight = 0 falls back to unweighted medianDec
//     • scaling every weight by a positive constant leaves the result
//       unchanged (weight is relative, not absolute)
//     • shuffling inputs leaves the result unchanged (internal sort)
//     • result always lies in [min(prices), max(prices)]
//
//   filterProviderOutliers
//     • empty input → empty output
//     • median ≤ 0 is treated as "no filter" and all samples pass
//     • maxDeviation = 0 is treated as "no filter" and all samples pass
//     • boundary samples (deviation == maxDeviation) pass (LTE cutoff)
//     • samples beyond deviation drop
//     • returned slice is a copy — caller mutation does not leak back

// -----------------------------------------------------------------------------
// weightedMedianDec
// -----------------------------------------------------------------------------

func mkSample(providerID, canonicalKey string, priceUnit, weightUnit int64) normalizedProviderSample {
	return normalizedProviderSample{
		sample: OracleProviderSample{
			ProviderID: providerID,
		},
		canonicalKey: canonicalKey,
		price:        sdkmath.LegacyNewDec(priceUnit),
		weight:       sdkmath.LegacyNewDec(weightUnit),
	}
}

func TestWeightedMedianDec_Empty(t *testing.T) {
	got := weightedMedianDec(nil)
	if !got.IsZero() {
		t.Fatalf("weightedMedianDec(nil) = %s; want 0", got)
	}
}

func TestWeightedMedianDec_Single(t *testing.T) {
	// Single sample must return that sample's price regardless of
	// weight (including zero weight — the function falls back to
	// unweighted median which returns the single value).
	cases := []struct {
		name   string
		weight int64
	}{
		{"nonzero_weight", 5},
		{"zero_weight", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := weightedMedianDec([]normalizedProviderSample{
				mkSample("p1", "k1", 42, tc.weight),
			})
			if !got.Equal(sdkmath.LegacyNewDec(42)) {
				t.Fatalf("weightedMedianDec single = %s; want 42", got)
			}
		})
	}
}

func TestWeightedMedianDec_WeightShiftsResult(t *testing.T) {
	// Three samples with equal weights: median is 20.
	// With sample at price 30 heavily overweighted, the cumulative
	// weight crosses the half-threshold at the heavy sample, so the
	// weighted median moves to 30.
	samples := []normalizedProviderSample{
		mkSample("p1", "k1", 10, 1),
		mkSample("p2", "k2", 20, 1),
		mkSample("p3", "k3", 30, 100), // 98% of weight
	}
	got := weightedMedianDec(samples)
	if !got.Equal(sdkmath.LegacyNewDec(30)) {
		t.Fatalf("weightedMedianDec heavy-high = %s; want 30", got)
	}
}

func TestWeightedMedianDec_TotalWeightZero_FallsBackToUnweightedMedian(t *testing.T) {
	// If all weights are zero, the impl falls back to medianDec over
	// the prices. For 3 samples [10, 20, 30] the unweighted median is 20.
	samples := []normalizedProviderSample{
		mkSample("p1", "k1", 10, 0),
		mkSample("p2", "k2", 20, 0),
		mkSample("p3", "k3", 30, 0),
	}
	got := weightedMedianDec(samples)
	if !got.Equal(sdkmath.LegacyNewDec(20)) {
		t.Fatalf("weightedMedianDec zero-weight fallback = %s; want 20 (unweighted median)", got)
	}
}

func TestWeightedMedianDec_TiedPrices_DeterministicOrder(t *testing.T) {
	// When two samples share the same price, the sort tiebreaker is
	// provider-id → canonical-key. Construct a case where the
	// cumulative-weight cursor stops AT a tied-price region; which
	// of the two tied samples wins depends on deterministic ordering.
	// Both orderings of the input must produce the same output.
	a := mkSample("provider-a", "key-a", 50, 40)
	b := mkSample("provider-b", "key-b", 50, 40)
	other := mkSample("provider-c", "key-c", 100, 20)

	forward := weightedMedianDec([]normalizedProviderSample{a, b, other})
	reversed := weightedMedianDec([]normalizedProviderSample{other, b, a})

	if !forward.Equal(reversed) {
		t.Fatalf("tied prices: forward=%s reversed=%s; must be deterministic", forward, reversed)
	}
	// And the result must equal the tied price (50), since cumulative
	// weight crosses half (50) at the first-or-second tied sample.
	if !forward.Equal(sdkmath.LegacyNewDec(50)) {
		t.Fatalf("tied-price median = %s; want 50", forward)
	}
}

func TestWeightedMedianDec_WeightScaleInvariance(t *testing.T) {
	// Weighted median depends only on relative weights, not absolute
	// magnitudes. Scaling every weight by a positive constant must
	// leave the result unchanged. This pins the normalization
	// semantics of the half-weight threshold.
	base := []normalizedProviderSample{
		mkSample("p1", "k1", 10, 1),
		mkSample("p2", "k2", 20, 3),
		mkSample("p3", "k3", 30, 1),
	}
	baseResult := weightedMedianDec(base)

	scaled := []normalizedProviderSample{
		mkSample("p1", "k1", 10, 100),
		mkSample("p2", "k2", 20, 300),
		mkSample("p3", "k3", 30, 100),
	}
	scaledResult := weightedMedianDec(scaled)

	if !baseResult.Equal(scaledResult) {
		t.Fatalf("weight scale invariance: base=%s scaled=%s; must be equal", baseResult, scaledResult)
	}
}

func TestWeightedMedianDec_PermutationInvariance(t *testing.T) {
	samples := []normalizedProviderSample{
		mkSample("p1", "k1", 10, 2),
		mkSample("p2", "k2", 20, 5),
		mkSample("p3", "k3", 30, 3),
		mkSample("p4", "k4", 40, 4),
	}
	forward := weightedMedianDec(samples)

	reversed := make([]normalizedProviderSample, len(samples))
	for i, s := range samples {
		reversed[len(samples)-1-i] = s
	}
	reversedResult := weightedMedianDec(reversed)

	if !forward.Equal(reversedResult) {
		t.Fatalf("permutation invariance: forward=%s reversed=%s", forward, reversedResult)
	}
}

func TestWeightedMedianDec_ResultWithinBounds(t *testing.T) {
	// Result must always lie within [min(price), max(price)] of the
	// input samples — a weighted median can't invent a price that
	// wasn't submitted by some provider.
	cases := [][]normalizedProviderSample{
		{
			mkSample("p1", "k1", 100, 1),
			mkSample("p2", "k2", 200, 1),
		},
		{
			mkSample("p1", "k1", 5, 10),
			mkSample("p2", "k2", 50, 1),
			mkSample("p3", "k3", 500, 1),
		},
		{
			mkSample("p1", "k1", 1, 1),
			mkSample("p2", "k2", 1_000_000, 1),
		},
	}
	for i, samples := range cases {
		got := weightedMedianDec(samples)
		minP, maxP := samples[0].price, samples[0].price
		for _, s := range samples[1:] {
			if s.price.LT(minP) {
				minP = s.price
			}
			if s.price.GT(maxP) {
				maxP = s.price
			}
		}
		if got.LT(minP) || got.GT(maxP) {
			t.Errorf("case %d: result %s outside [%s, %s]", i, got, minP, maxP)
		}
	}
}

// -----------------------------------------------------------------------------
// filterProviderOutliers
// -----------------------------------------------------------------------------

func TestFilterProviderOutliers_Empty(t *testing.T) {
	got := filterProviderOutliers(nil, sdkmath.LegacyNewDec(100), sdkmath.LegacyNewDecWithPrec(5, 2))
	if len(got) != 0 {
		t.Fatalf("filter empty = len %d; want 0", len(got))
	}
}

func TestFilterProviderOutliers_NonPositiveMedian_NoFilter(t *testing.T) {
	// When median is zero or negative the function treats the input
	// as un-filterable (no meaningful deviation can be computed)
	// and returns a copy of the full set.
	samples := []normalizedProviderSample{
		mkSample("p1", "k1", 10, 1),
		mkSample("p2", "k2", 1_000_000, 1),
	}
	cases := []struct {
		name   string
		median sdkmath.LegacyDec
	}{
		{"zero_median", sdkmath.LegacyZeroDec()},
		{"negative_median", sdkmath.LegacyNewDec(-50)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterProviderOutliers(samples, tc.median, sdkmath.LegacyNewDecWithPrec(1, 2))
			if len(got) != len(samples) {
				t.Fatalf("expected all %d samples kept, got %d", len(samples), len(got))
			}
		})
	}
}

func TestFilterProviderOutliers_ZeroMaxDeviation_NoFilter(t *testing.T) {
	// maxDeviation = 0 is intentionally treated as "no filter"
	// rather than "exact match only" — the aggregation path uses
	// this as the disabled-outlier-filter signal.
	samples := []normalizedProviderSample{
		mkSample("p1", "k1", 10, 1),
		mkSample("p2", "k2", 1_000_000, 1),
	}
	got := filterProviderOutliers(samples, sdkmath.LegacyNewDec(100), sdkmath.LegacyZeroDec())
	if len(got) != len(samples) {
		t.Fatalf("expected all %d samples kept when maxDeviation=0, got %d", len(samples), len(got))
	}
}

func TestFilterProviderOutliers_BoundarySampleKept(t *testing.T) {
	// A sample whose deviation equals maxDeviation exactly must be
	// kept (cutoff is LTE). This locks the inclusive-boundary
	// semantics — a refactor to strict LT would drop samples sitting
	// precisely at the limit.
	median := sdkmath.LegacyNewDec(100)
	maxDev := sdkmath.LegacyNewDecWithPrec(1, 1) // 0.1 → ±10%
	// Sample at 110 = median * (1 + 0.1) → deviation = 0.1 exactly.
	boundary := mkSample("p1", "k1", 110, 1)
	got := filterProviderOutliers([]normalizedProviderSample{boundary}, median, maxDev)
	if len(got) != 1 {
		t.Fatalf("boundary sample dropped; want kept. got len=%d", len(got))
	}
}

func TestFilterProviderOutliers_DropsBeyondDeviation(t *testing.T) {
	// maxDeviation = 10% of median=100 → accept [90, 110].
	// Sample at 200 must be dropped (100% above median).
	median := sdkmath.LegacyNewDec(100)
	maxDev := sdkmath.LegacyNewDecWithPrec(1, 1) // 0.1
	samples := []normalizedProviderSample{
		mkSample("p_good", "k_good", 95, 1),
		mkSample("p_bad", "k_bad", 200, 1),
		mkSample("p_also_good", "k3", 105, 1),
	}
	got := filterProviderOutliers(samples, median, maxDev)
	if len(got) != 2 {
		t.Fatalf("expected 2 kept (good + also_good); got %d", len(got))
	}
	for _, s := range got {
		if s.sample.ProviderID == "p_bad" {
			t.Errorf("outlier p_bad was kept; expected dropped")
		}
	}
}

func TestFilterProviderOutliers_SymmetricAbs(t *testing.T) {
	// Deviation is symmetric (.Abs()) — a sample equally far below
	// the median must be treated identically to one above. Regression
	// guard for a refactor that forgot the Abs() and silently biased
	// filtering to only catch high-side outliers.
	median := sdkmath.LegacyNewDec(100)
	maxDev := sdkmath.LegacyNewDecWithPrec(1, 1) // 0.1
	samples := []normalizedProviderSample{
		mkSample("p_low", "k_low", 50, 1),  // 50% below → drop
		mkSample("p_high", "k_high", 150, 1), // 50% above → drop
	}
	got := filterProviderOutliers(samples, median, maxDev)
	if len(got) != 0 {
		t.Fatalf("expected both dropped (symmetric outliers); got %d", len(got))
	}
}

func TestFilterProviderOutliers_ReturnsCopy(t *testing.T) {
	// The function returns a fresh slice (not an aliased subset of
	// the input). Mutating the output must not corrupt the input.
	original := []normalizedProviderSample{
		mkSample("p1", "k1", 100, 1),
		mkSample("p2", "k2", 105, 1),
	}
	median := sdkmath.LegacyNewDec(100)
	maxDev := sdkmath.LegacyNewDecWithPrec(1, 1)

	got := filterProviderOutliers(original, median, maxDev)
	if len(got) == 0 {
		t.Fatal("expected samples kept (sanity)")
	}
	// Mutate the first element of the output.
	got[0].price = sdkmath.LegacyNewDec(999999)

	if !original[0].price.Equal(sdkmath.LegacyNewDec(100)) {
		t.Fatalf("output mutation leaked into input: original[0].price=%s; want 100",
			original[0].price)
	}
}
