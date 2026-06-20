//go:build cosmos

package keeper

import (
	"math/rand"
	"sort"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// Deterministic aggregation conformance tests.
// These tests prove that BuildCanonicalPriceFeeds produces identical outputs
// for identical inputs regardless of:
// - Call order (same inputs across multiple invocations)
// - Input order (shuffled sample slices)
// - Provider registration order
// This is consensus-critical: non-determinism would cause chain splits.

var deterministicTestTime = time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

func makeTestSamples() []OracleProviderSample {
	base := deterministicTestTime.Add(-30 * time.Second)
	return []OracleProviderSample{
		{
			ProviderID:      "provider-alpha",
			AssetPair:       "BTC/USD",
			ObservedAt:      base,
			Price:           "42000.50",
			Volume24H:       "1000000",
			ConfidenceScore: "0.95",
		},
		{
			ProviderID:      "provider-beta",
			AssetPair:       "BTC/USD",
			ObservedAt:      base.Add(time.Second),
			Price:           "42001.25",
			Volume24H:       "950000",
			ConfidenceScore: "0.92",
		},
		{
			ProviderID:      "provider-gamma",
			AssetPair:       "BTC/USD",
			ObservedAt:      base.Add(2 * time.Second),
			Price:           "42002.00",
			Volume24H:       "1100000",
			ConfidenceScore: "0.98",
		},
		{
			ProviderID:      "provider-alpha",
			AssetPair:       "ETH/USD",
			ObservedAt:      base,
			Price:           "2250.75",
			Volume24H:       "500000",
			ConfidenceScore: "0.94",
		},
		{
			ProviderID:      "provider-beta",
			AssetPair:       "ETH/USD",
			ObservedAt:      base.Add(time.Second),
			Price:           "2251.00",
			Volume24H:       "480000",
			ConfidenceScore: "0.91",
		},
	}
}

// TestBuildCanonicalPriceFeeds_RepeatedCallsIdentical proves that calling
// the aggregation function multiple times with the same inputs produces
// byte-identical outputs.
func TestBuildCanonicalPriceFeeds_RepeatedCallsIdentical(t *testing.T) {
	t.Parallel()

	samples := makeTestSamples()
	allowedPairs := []string{"BTC/USD", "ETH/USD"}
	cfg := DefaultProviderAggregationConfig()

	// Call 10 times and verify all results match
	var firstReport ProviderAggregationReport
	for i := 0; i < 10; i++ {
		report, err := BuildCanonicalPriceFeeds(deterministicTestTime, allowedPairs, samples, cfg)
		require.NoError(t, err)

		if i == 0 {
			firstReport = report
			continue
		}

		require.Equal(t, len(firstReport.Feeds), len(report.Feeds),
			"iteration %d: feed count changed", i)

		for j, feed := range report.Feeds {
			require.Equal(t, firstReport.Feeds[j].AssetPair, feed.AssetPair,
				"iteration %d, feed %d: asset pair changed", i, j)
			require.Equal(t, firstReport.Feeds[j].Price, feed.Price,
				"iteration %d, feed %d: price changed — consensus-breaking", i, j)
			require.Equal(t, firstReport.Feeds[j].Volume_24H, feed.Volume_24H,
				"iteration %d, feed %d: volume changed", i, j)
			require.Equal(t, firstReport.Feeds[j].ConfidenceScore, feed.ConfidenceScore,
				"iteration %d, feed %d: confidence changed", i, j)
			require.Equal(t, firstReport.Feeds[j].Sources, feed.Sources,
				"iteration %d, feed %d: sources changed", i, j)
		}
	}
}

// TestBuildCanonicalPriceFeeds_InputOrderIndependent proves that shuffling
// the input sample slice produces identical outputs.
func TestBuildCanonicalPriceFeeds_InputOrderIndependent(t *testing.T) {
	t.Parallel()

	samples := makeTestSamples()
	allowedPairs := []string{"BTC/USD", "ETH/USD"}
	cfg := DefaultProviderAggregationConfig()

	// Get canonical result with original order
	canonical, err := BuildCanonicalPriceFeeds(deterministicTestTime, allowedPairs, samples, cfg)
	require.NoError(t, err)

	// Shuffle and test 20 times with different orderings
	rng := rand.New(rand.NewSource(42))
	for iteration := 0; iteration < 20; iteration++ {
		shuffled := make([]OracleProviderSample, len(samples))
		copy(shuffled, samples)
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		report, err := BuildCanonicalPriceFeeds(deterministicTestTime, allowedPairs, shuffled, cfg)
		require.NoError(t, err)

		require.Equal(t, len(canonical.Feeds), len(report.Feeds),
			"shuffle %d: feed count differs from canonical", iteration)

		for j, feed := range report.Feeds {
			require.Equal(t, canonical.Feeds[j].AssetPair, feed.AssetPair,
				"shuffle %d, feed %d: asset pair order changed", iteration, j)
			require.Equal(t, canonical.Feeds[j].Price, feed.Price,
				"shuffle %d, feed %d: price differs — input order affected aggregation", iteration, j)
			require.Equal(t, canonical.Feeds[j].Sources, feed.Sources,
				"shuffle %d, feed %d: sources differ — sorting unstable", iteration, j)
		}
	}
}

// TestBuildCanonicalPriceFeeds_AllowedPairsOrderIndependent proves that
// the order of allowed asset pairs doesn't affect output.
func TestBuildCanonicalPriceFeeds_AllowedPairsOrderIndependent(t *testing.T) {
	t.Parallel()

	samples := makeTestSamples()
	cfg := DefaultProviderAggregationConfig()

	orderings := [][]string{
		{"BTC/USD", "ETH/USD"},
		{"ETH/USD", "BTC/USD"},
		{"ETH/USD", "BTC/USD", "ETH/USD"}, // duplicates
	}

	var canonical ProviderAggregationReport
	for i, allowedPairs := range orderings {
		report, err := BuildCanonicalPriceFeeds(deterministicTestTime, allowedPairs, samples, cfg)
		require.NoError(t, err)

		if i == 0 {
			canonical = report
			continue
		}

		require.Equal(t, len(canonical.Feeds), len(report.Feeds),
			"ordering %d: feed count differs", i)

		for j, feed := range report.Feeds {
			require.Equal(t, canonical.Feeds[j].Price, feed.Price,
				"ordering %d, feed %d: price differs — allowed pairs order affected result", i, j)
		}
	}
}

// TestBuildCanonicalPriceFeeds_MedianDeterminism proves that median
// calculation is deterministic for both odd and even sample counts.
func TestBuildCanonicalPriceFeeds_MedianDeterminism(t *testing.T) {
	t.Parallel()

	base := deterministicTestTime.Add(-30 * time.Second)

	testCases := []struct {
		name          string
		prices        []string
		expectedPrice string
	}{
		{
			name:          "odd_count_3",
			prices:        []string{"100.00", "101.00", "102.00"},
			expectedPrice: "101.000000000000000000",
		},
		{
			name:          "even_count_4",
			prices:        []string{"100.00", "101.00", "102.00", "103.00"},
			expectedPrice: "101.500000000000000000",
		},
		{
			name:          "even_count_2",
			prices:        []string{"100.00", "200.00"},
			expectedPrice: "150.000000000000000000",
		},
		{
			name:          "single_sample",
			prices:        []string{"42000.00"},
			expectedPrice: "42000.000000000000000000",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			samples := make([]OracleProviderSample, len(tc.prices))
			for i, price := range tc.prices {
				samples[i] = OracleProviderSample{
					ProviderID:      "provider-" + string(rune('a'+i)),
					AssetPair:       "TEST/USD",
					ObservedAt:      base.Add(time.Duration(i) * time.Second),
					Price:           price,
					Volume24H:       "1000",
					ConfidenceScore: "0.90",
				}
			}

			cfg := DefaultProviderAggregationConfig()
			report, err := BuildCanonicalPriceFeeds(deterministicTestTime, []string{"TEST/USD"}, samples, cfg)
			require.NoError(t, err)
			require.Len(t, report.Feeds, 1)
			require.Equal(t, tc.expectedPrice, report.Feeds[0].Price,
				"median calculation produced unexpected result")
		})
	}
}

// TestBuildCanonicalPriceFeeds_SourcesSortDeterminism proves that the
// Sources field is always sorted identically.
func TestBuildCanonicalPriceFeeds_SourcesSortDeterminism(t *testing.T) {
	t.Parallel()

	base := deterministicTestTime.Add(-30 * time.Second)
	providers := []string{"zebra", "alpha", "beta", "omega", "gamma"}

	samples := make([]OracleProviderSample, len(providers))
	for i, provider := range providers {
		samples[i] = OracleProviderSample{
			ProviderID:      provider,
			AssetPair:       "BTC/USD",
			ObservedAt:      base.Add(time.Duration(i) * time.Second),
			Price:           "42000.00",
			Volume24H:       "1000",
			ConfidenceScore: "0.90",
		}
	}

	cfg := DefaultProviderAggregationConfig()
	cfg.MaxProviders = 10 // allow all

	expectedSources := make([]string, len(providers))
	copy(expectedSources, providers)
	sort.Strings(expectedSources)

	// Test with multiple shuffle orderings
	rng := rand.New(rand.NewSource(99))
	for i := 0; i < 10; i++ {
		shuffled := make([]OracleProviderSample, len(samples))
		copy(shuffled, samples)
		rng.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})

		report, err := BuildCanonicalPriceFeeds(deterministicTestTime, []string{"BTC/USD"}, shuffled, cfg)
		require.NoError(t, err)
		require.Len(t, report.Feeds, 1)
		require.Equal(t, expectedSources, report.Feeds[0].Sources,
			"shuffle %d: sources not sorted deterministically", i)
	}
}

// TestBuildCanonicalPriceFeeds_WeightedMedianDeterminism proves that
// weighted median aggregation is deterministic.
func TestBuildCanonicalPriceFeeds_WeightedMedianDeterminism(t *testing.T) {
	t.Parallel()

	base := deterministicTestTime.Add(-30 * time.Second)
	samples := []OracleProviderSample{
		{
			ProviderID:      "provider-heavy",
			AssetPair:       "BTC/USD",
			ObservedAt:      base,
			Price:           "42000.00",
			Volume24H:       "1000000",
			ConfidenceScore: "0.95",
		},
		{
			ProviderID:      "provider-light",
			AssetPair:       "BTC/USD",
			ObservedAt:      base.Add(time.Second),
			Price:           "43000.00",
			Volume24H:       "500000",
			ConfidenceScore: "0.90",
		},
		{
			ProviderID:      "provider-medium",
			AssetPair:       "BTC/USD",
			ObservedAt:      base.Add(2 * time.Second),
			Price:           "42500.00",
			Volume24H:       "750000",
			ConfidenceScore: "0.92",
		},
	}

	cfg := ProviderAggregationConfig{
		MaxProviders:      10,
		AggregationMethod: ProviderAggregationWeightedMedian,
		ProviderConfigs: map[string]OracleProviderConfig{
			"provider-heavy":  {Weight: "3.0"},
			"provider-light":  {Weight: "1.0"},
			"provider-medium": {Weight: "2.0"},
		},
	}

	var firstPrice string
	rng := rand.New(rand.NewSource(123))

	for i := 0; i < 15; i++ {
		shuffled := make([]OracleProviderSample, len(samples))
		copy(shuffled, samples)
		rng.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})

		report, err := BuildCanonicalPriceFeeds(deterministicTestTime, []string{"BTC/USD"}, shuffled, cfg)
		require.NoError(t, err)
		require.Len(t, report.Feeds, 1)

		if i == 0 {
			firstPrice = report.Feeds[0].Price
			continue
		}

		require.Equal(t, firstPrice, report.Feeds[0].Price,
			"shuffle %d: weighted median differs — non-deterministic", i)
	}
}

// TestBuildCanonicalPriceFeeds_OutlierFilterDeterminism proves that
// outlier filtering produces consistent results.
func TestBuildCanonicalPriceFeeds_OutlierFilterDeterminism(t *testing.T) {
	t.Parallel()

	base := deterministicTestTime.Add(-30 * time.Second)
	samples := []OracleProviderSample{
		{ProviderID: "p1", AssetPair: "BTC/USD", ObservedAt: base, Price: "42000.00", Volume24H: "1000", ConfidenceScore: "0.9"},
		{ProviderID: "p2", AssetPair: "BTC/USD", ObservedAt: base.Add(time.Second), Price: "42100.00", Volume24H: "1000", ConfidenceScore: "0.9"},
		{ProviderID: "p3", AssetPair: "BTC/USD", ObservedAt: base.Add(2 * time.Second), Price: "42050.00", Volume24H: "1000", ConfidenceScore: "0.9"},
		{ProviderID: "outlier", AssetPair: "BTC/USD", ObservedAt: base.Add(3 * time.Second), Price: "50000.00", Volume24H: "1000", ConfidenceScore: "0.9"},
	}

	cfg := ProviderAggregationConfig{
		MaxProviders:      10,
		MaxPriceDeviation: "0.05", // 5% max deviation
		AggregationMethod: ProviderAggregationMedian,
	}

	var firstReport ProviderAggregationReport
	rng := rand.New(rand.NewSource(456))

	for i := 0; i < 10; i++ {
		shuffled := make([]OracleProviderSample, len(samples))
		copy(shuffled, samples)
		rng.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})

		report, err := BuildCanonicalPriceFeeds(deterministicTestTime, []string{"BTC/USD"}, shuffled, cfg)
		require.NoError(t, err)

		if i == 0 {
			firstReport = report
			require.Equal(t, 1, report.FilteredOutliers,
				"expected 1 outlier to be filtered")
			continue
		}

		require.Equal(t, firstReport.FilteredOutliers, report.FilteredOutliers,
			"shuffle %d: outlier count differs", i)
		require.Equal(t, firstReport.Feeds[0].Price, report.Feeds[0].Price,
			"shuffle %d: price after outlier filter differs", i)
		require.Equal(t, firstReport.Feeds[0].Sources, report.Feeds[0].Sources,
			"shuffle %d: sources after outlier filter differs", i)
	}
}

// TestBuildCanonicalPriceFeeds_DecimalPrecisionStability proves that
// decimal arithmetic doesn't introduce floating-point drift.
func TestBuildCanonicalPriceFeeds_DecimalPrecisionStability(t *testing.T) {
	t.Parallel()

	base := deterministicTestTime.Add(-30 * time.Second)

	// Use prices that could cause floating-point issues
	samples := []OracleProviderSample{
		{ProviderID: "p1", AssetPair: "TEST/USD", ObservedAt: base, Price: "0.000000000000000001", Volume24H: "1", ConfidenceScore: "0.9"},
		{ProviderID: "p2", AssetPair: "TEST/USD", ObservedAt: base.Add(time.Second), Price: "0.000000000000000002", Volume24H: "1", ConfidenceScore: "0.9"},
		{ProviderID: "p3", AssetPair: "TEST/USD", ObservedAt: base.Add(2 * time.Second), Price: "0.000000000000000003", Volume24H: "1", ConfidenceScore: "0.9"},
	}

	cfg := DefaultProviderAggregationConfig()

	report1, err := BuildCanonicalPriceFeeds(deterministicTestTime, []string{"TEST/USD"}, samples, cfg)
	require.NoError(t, err)

	// Reverse order
	reversed := make([]OracleProviderSample, len(samples))
	for i, s := range samples {
		reversed[len(samples)-1-i] = s
	}

	report2, err := BuildCanonicalPriceFeeds(deterministicTestTime, []string{"TEST/USD"}, reversed, cfg)
	require.NoError(t, err)

	require.Equal(t, report1.Feeds[0].Price, report2.Feeds[0].Price,
		"tiny decimal precision differs with reversed input")
}

// TestBuildCanonicalPriceFeeds_FeedOrderAlwaysSorted proves that output
// feeds are always sorted by asset pair.
func TestBuildCanonicalPriceFeeds_FeedOrderAlwaysSorted(t *testing.T) {
	t.Parallel()

	base := deterministicTestTime.Add(-30 * time.Second)
	pairs := []string{"ZZZ/USD", "AAA/USD", "MMM/USD", "BBB/USD"}

	samples := make([]OracleProviderSample, len(pairs))
	for i, pair := range pairs {
		samples[i] = OracleProviderSample{
			ProviderID:      "provider",
			AssetPair:       pair,
			ObservedAt:      base.Add(time.Duration(i) * time.Second),
			Price:           "100.00",
			Volume24H:       "1000",
			ConfidenceScore: "0.9",
		}
	}

	cfg := DefaultProviderAggregationConfig()
	expectedOrder := []string{"AAA/USD", "BBB/USD", "MMM/USD", "ZZZ/USD"}

	rng := rand.New(rand.NewSource(789))
	for i := 0; i < 10; i++ {
		shuffled := make([]OracleProviderSample, len(samples))
		copy(shuffled, samples)
		rng.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})

		report, err := BuildCanonicalPriceFeeds(deterministicTestTime, pairs, shuffled, cfg)
		require.NoError(t, err)
		require.Len(t, report.Feeds, len(pairs))

		for j, feed := range report.Feeds {
			require.Equal(t, expectedOrder[j], feed.AssetPair,
				"shuffle %d: feed %d not in sorted order", i, j)
		}
	}
}

// TestMedianDec_Determinism proves medianDec is deterministic.
func TestMedianDec_Determinism(t *testing.T) {
	t.Parallel()

	prices := []sdkmath.LegacyDec{
		sdkmath.LegacyMustNewDecFromStr("100.5"),
		sdkmath.LegacyMustNewDecFromStr("200.25"),
		sdkmath.LegacyMustNewDecFromStr("150.75"),
		sdkmath.LegacyMustNewDecFromStr("175.0"),
	}

	result1 := medianDec(prices)

	// Shuffle and recalculate 10 times
	rng := rand.New(rand.NewSource(111))
	for i := 0; i < 10; i++ {
		shuffled := make([]sdkmath.LegacyDec, len(prices))
		copy(shuffled, prices)
		rng.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})

		result := medianDec(shuffled)
		require.True(t, result1.Equal(result),
			"shuffle %d: medianDec not deterministic — got %s, want %s", i, result, result1)
	}
}
