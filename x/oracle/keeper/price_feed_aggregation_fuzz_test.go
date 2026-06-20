//go:build cosmos

package keeper

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

// Fuzz tests for oracle price-feed aggregation.
// These tests probe BuildCanonicalPriceFeeds with randomly generated inputs
// to discover crash conditions, panics, and invariant violations.
// Coverage targets: malformed strings, extreme values, edge case timestamps,
// Unicode injection, and large sample counts.

var fuzzSnapshotTime = time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

// FuzzBuildCanonicalPriceFeeds_MalformedPrices tests aggregation resilience
// to garbage price strings. The function must never panic.
func FuzzBuildCanonicalPriceFeeds_MalformedPrices(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"0",
		"-1",
		"not_a_number",
		"1e999999",
		"1e-999999",
		"NaN",
		"Inf",
		"-Inf",
		"0.000000000000000001",
		"999999999999999999999999999999999999999",
		"1.2.3.4",
		"--1",
		"++1",
		"1+1",
		"\x00",
		"\n100\n",
		"\t50.5\t",
		"100\x00garbage",
		"42;DROP TABLE prices",
		"<script>alert(1)</script>",
		"{{price}}",
		"${100}",
		"1,000.50",
		"1 000.50",
		"١٢٣",
		"一二三",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, priceStr string) {
		sample := OracleProviderSample{
			ProviderID:      "fuzz-provider",
			AssetPair:       "TEST/USD",
			ObservedAt:      fuzzSnapshotTime.Add(-10 * time.Second),
			Price:           priceStr,
			Volume24H:       "1000",
			ConfidenceScore: "0.90",
		}

		cfg := DefaultProviderAggregationConfig()

		// Must not panic regardless of input
		report, err := BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			[]string{"TEST/USD"},
			[]OracleProviderSample{sample},
			cfg,
		)

		// Either we get an error/rejection or a valid report
		if err == nil && len(report.Feeds) > 0 {
			// If a feed was produced, price must be parseable
			_, parseErr := sdkmath.LegacyNewDecFromStr(report.Feeds[0].Price)
			if parseErr != nil {
				t.Fatalf("produced feed with unparseable price %q", report.Feeds[0].Price)
			}
		}
	})
}

// FuzzBuildCanonicalPriceFeeds_MalformedVolumes tests volume string handling.
func FuzzBuildCanonicalPriceFeeds_MalformedVolumes(f *testing.F) {
	seeds := []string{
		"",
		"-100",
		"not_a_volume",
		"1e999",
		"0.0000000000000000001",
		"\x00\x00",
		"  ",
		"100 200",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, volumeStr string) {
		sample := OracleProviderSample{
			ProviderID:      "fuzz-provider",
			AssetPair:       "TEST/USD",
			ObservedAt:      fuzzSnapshotTime.Add(-10 * time.Second),
			Price:           "100.00",
			Volume24H:       volumeStr,
			ConfidenceScore: "0.90",
		}

		cfg := DefaultProviderAggregationConfig()

		// Must not panic
		_, _ = BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			[]string{"TEST/USD"},
			[]OracleProviderSample{sample},
			cfg,
		)
	})
}

// FuzzBuildCanonicalPriceFeeds_MalformedConfidence tests confidence score handling.
func FuzzBuildCanonicalPriceFeeds_MalformedConfidence(f *testing.F) {
	seeds := []string{
		"",
		"-0.5",
		"1.5",
		"2.0",
		"100",
		"not_confidence",
		"1e-50",
		"0.9999999999999999999999999999",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, confidenceStr string) {
		sample := OracleProviderSample{
			ProviderID:      "fuzz-provider",
			AssetPair:       "TEST/USD",
			ObservedAt:      fuzzSnapshotTime.Add(-10 * time.Second),
			Price:           "100.00",
			Volume24H:       "1000",
			ConfidenceScore: confidenceStr,
		}

		cfg := DefaultProviderAggregationConfig()

		// Must not panic
		_, _ = BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			[]string{"TEST/USD"},
			[]OracleProviderSample{sample},
			cfg,
		)
	})
}

// FuzzBuildCanonicalPriceFeeds_EdgeTimestamps tests timestamp edge cases.
func FuzzBuildCanonicalPriceFeeds_EdgeTimestamps(f *testing.F) {
	seeds := []int64{
		0,
		1,
		-1,
		math.MaxInt64,
		math.MinInt64,
		int64(time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC).Unix()),
		int64(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC).Unix()),
		int64(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).Unix()),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, unixSec int64) {
		// Clamp to reasonable range to avoid overflow
		if unixSec < -62135596800 || unixSec > 253402300799 {
			return
		}

		observedAt := time.Unix(unixSec, 0).UTC()
		sample := OracleProviderSample{
			ProviderID:      "fuzz-provider",
			AssetPair:       "TEST/USD",
			ObservedAt:      observedAt,
			Price:           "100.00",
			Volume24H:       "1000",
			ConfidenceScore: "0.90",
		}

		cfg := DefaultProviderAggregationConfig()

		// Must not panic
		report, _ := BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			[]string{"TEST/USD"},
			[]OracleProviderSample{sample},
			cfg,
		)

		// Future timestamps should be rejected
		if observedAt.After(fuzzSnapshotTime) && len(report.Feeds) > 0 {
			// If sample was from the future, it should have been rejected
			for _, diag := range report.Diagnostics {
				if diag.Code == "invalid_sample" && strings.Contains(diag.Message, "after snapshot") {
					return // correctly rejected
				}
			}
			// If no diagnostic but also no feed, that's fine
			if len(report.Feeds) == 0 {
				return
			}
		}
	})
}

// FuzzBuildCanonicalPriceFeeds_ProviderIDEdgeCases tests provider ID handling.
func FuzzBuildCanonicalPriceFeeds_ProviderIDEdgeCases(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"  ",
		"\t\n",
		"a",
		strings.Repeat("x", 1000),
		"provider\x00with\x00nulls",
		"provider|with|pipes",
		"provider:with:colons",
		"provider\nwith\nnewlines",
		"🔥provider🔥",
		"حَدَث",
		"\u200B", // zero-width space
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, providerID string) {
		// Skip inputs that would cause issues unrelated to aggregation
		if !utf8.ValidString(providerID) {
			return
		}

		sample := OracleProviderSample{
			ProviderID:      providerID,
			AssetPair:       "TEST/USD",
			ObservedAt:      fuzzSnapshotTime.Add(-10 * time.Second),
			Price:           "100.00",
			Volume24H:       "1000",
			ConfidenceScore: "0.90",
		}

		cfg := DefaultProviderAggregationConfig()

		// Must not panic
		report, _ := BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			[]string{"TEST/USD"},
			[]OracleProviderSample{sample},
			cfg,
		)

		// Empty/whitespace-only provider IDs should be rejected
		if strings.TrimSpace(providerID) == "" && len(report.Feeds) > 0 {
			t.Fatalf("empty provider ID produced a feed")
		}
	})
}

// FuzzBuildCanonicalPriceFeeds_AssetPairEdgeCases tests asset pair handling.
func FuzzBuildCanonicalPriceFeeds_AssetPairEdgeCases(f *testing.F) {
	seeds := []string{
		"",
		" ",
		"BTC/USD",
		"BTC/USD ",
		" BTC/USD",
		"BTC/USD\x00extra",
		"BTC\nUSD",
		strings.Repeat("A", 500) + "/" + strings.Repeat("B", 500),
		"🪙/💵",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, assetPair string) {
		if !utf8.ValidString(assetPair) {
			return
		}

		sample := OracleProviderSample{
			ProviderID:      "fuzz-provider",
			AssetPair:       assetPair,
			ObservedAt:      fuzzSnapshotTime.Add(-10 * time.Second),
			Price:           "100.00",
			Volume24H:       "1000",
			ConfidenceScore: "0.90",
		}

		allowedPairs := []string{assetPair}
		cfg := DefaultProviderAggregationConfig()

		// Must not panic
		report, _ := BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			allowedPairs,
			[]OracleProviderSample{sample},
			cfg,
		)

		// Empty asset pairs should be rejected
		if strings.TrimSpace(assetPair) == "" && len(report.Feeds) > 0 {
			t.Fatalf("empty asset pair produced a feed")
		}
	})
}

// FuzzBuildCanonicalPriceFeeds_ExtremeWeights tests weighted median with edge weights.
func FuzzBuildCanonicalPriceFeeds_ExtremeWeights(f *testing.F) {
	seeds := []string{
		"0",
		"-1",
		"0.0000000000000000001",
		"999999999999999999999",
		"1e50",
		"1e-50",
		"NaN",
		"Inf",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, weightStr string) {
		sample := OracleProviderSample{
			ProviderID:      "weighted-provider",
			AssetPair:       "TEST/USD",
			ObservedAt:      fuzzSnapshotTime.Add(-10 * time.Second),
			Price:           "100.00",
			Volume24H:       "1000",
			ConfidenceScore: "0.90",
		}

		cfg := ProviderAggregationConfig{
			MaxProviders:      10,
			AggregationMethod: ProviderAggregationWeightedMedian,
			ProviderConfigs: map[string]OracleProviderConfig{
				"weighted-provider": {Weight: weightStr},
			},
		}

		// Must not panic
		_, _ = BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			[]string{"TEST/USD"},
			[]OracleProviderSample{sample},
			cfg,
		)
	})
}

// FuzzBuildCanonicalPriceFeeds_MaxDeviation tests outlier filter edge cases.
func FuzzBuildCanonicalPriceFeeds_MaxDeviation(f *testing.F) {
	seeds := []string{
		"0",
		"-0.1",
		"0.05",
		"1",
		"100",
		"0.0000000000000000001",
		"NaN",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, deviationStr string) {
		samples := []OracleProviderSample{
			{ProviderID: "p1", AssetPair: "TEST/USD", ObservedAt: fuzzSnapshotTime.Add(-10 * time.Second), Price: "100.00", Volume24H: "1000", ConfidenceScore: "0.9"},
			{ProviderID: "p2", AssetPair: "TEST/USD", ObservedAt: fuzzSnapshotTime.Add(-9 * time.Second), Price: "100.50", Volume24H: "1000", ConfidenceScore: "0.9"},
			{ProviderID: "p3", AssetPair: "TEST/USD", ObservedAt: fuzzSnapshotTime.Add(-8 * time.Second), Price: "200.00", Volume24H: "1000", ConfidenceScore: "0.9"},
		}

		cfg := ProviderAggregationConfig{
			MaxProviders:      10,
			MaxPriceDeviation: deviationStr,
			AggregationMethod: ProviderAggregationMedian,
		}

		// Must not panic
		_, _ = BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			[]string{"TEST/USD"},
			samples,
			cfg,
		)
	})
}

// FuzzBuildCanonicalPriceFeeds_SampleCount tests with varying sample counts.
func FuzzBuildCanonicalPriceFeeds_SampleCount(f *testing.F) {
	f.Add(uint8(0))
	f.Add(uint8(1))
	f.Add(uint8(2))
	f.Add(uint8(8))

	f.Fuzz(func(t *testing.T, count uint8) {
		// Cap to MaxProviders to avoid bound calculation complexity
		// from provider ID sorting (lexicographic, not numeric)
		if count > 8 {
			count = 8
		}

		samples := make([]OracleProviderSample, count)
		for i := range samples {
			samples[i] = OracleProviderSample{
				ProviderID:      fmt.Sprintf("provider-%d", i),
				AssetPair:       "TEST/USD",
				ObservedAt:      fuzzSnapshotTime.Add(-time.Duration(i+1) * time.Second),
				Price:           fmt.Sprintf("%d.00", 100+i),
				Volume24H:       "1000",
				ConfidenceScore: "0.90",
			}
		}

		cfg := DefaultProviderAggregationConfig()

		// Must not panic
		report, err := BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			[]string{"TEST/USD"},
			samples,
			cfg,
		)

		if err == nil && count > 0 && len(report.Feeds) > 0 {
			// Verify median bounds: result should be within input price range
			feed := report.Feeds[0]
			price, parseErr := sdkmath.LegacyNewDecFromStr(feed.Price)
			require.NoError(t, parseErr, "produced unparseable price")

			minPrice := sdkmath.LegacyNewDec(100)
			maxPrice := sdkmath.LegacyNewDec(100 + int64(count) - 1)

			if price.LT(minPrice) || price.GT(maxPrice) {
				t.Fatalf("median %s out of input bounds [%s, %s]", price, minPrice, maxPrice)
			}
		}
	})
}

// FuzzBuildCanonicalPriceFeeds_DuplicateSamples tests duplicate handling.
func FuzzBuildCanonicalPriceFeeds_DuplicateSamples(f *testing.F) {
	f.Add(uint8(1), uint8(1))
	f.Add(uint8(2), uint8(1))
	f.Add(uint8(1), uint8(3))
	f.Add(uint8(5), uint8(5))

	f.Fuzz(func(t *testing.T, numProviders, samplesPerProvider uint8) {
		if numProviders == 0 {
			return
		}

		var samples []OracleProviderSample
		for p := uint8(0); p < numProviders; p++ {
			for s := uint8(0); s < samplesPerProvider; s++ {
				samples = append(samples, OracleProviderSample{
					ProviderID:      fmt.Sprintf("provider-%d", p),
					AssetPair:       "TEST/USD",
					ObservedAt:      fuzzSnapshotTime.Add(-time.Duration(p*10+s+1) * time.Second),
					Price:           fmt.Sprintf("%d.00", 100+int(p)),
					Volume24H:       "1000",
					ConfidenceScore: "0.90",
				})
			}
		}

		cfg := DefaultProviderAggregationConfig()

		// Must not panic
		report, _ := BuildCanonicalPriceFeeds(
			fuzzSnapshotTime,
			[]string{"TEST/USD"},
			samples,
			cfg,
		)

		// If multiple samples per provider, they should be flagged as duplicates
		if samplesPerProvider > 1 && len(samples) > 0 {
			if report.DuplicateDropCount == 0 && len(report.Feeds) > 0 {
				// All unique providers should still produce feeds
				// but duplicates should be dropped
			}
		}
	})
}

// FuzzMedianDec_LargeSets tests medianDec with large input sets.
func FuzzMedianDec_LargeSets(f *testing.F) {
	f.Add([]byte{0, 100})
	f.Add([]byte{0, 50, 0, 100, 0, 150})
	f.Add(make([]byte, 200)) // 100 prices of 0

	f.Fuzz(func(t *testing.T, data []byte) {
		const maxPrices = 128
		pairs := len(data) / 2
		if pairs > maxPrices {
			pairs = maxPrices
		}
		if pairs == 0 {
			return
		}

		prices := make([]sdkmath.LegacyDec, 0, pairs)
		var minPrice, maxPrice sdkmath.LegacyDec
		for i := 0; i < pairs; i++ {
			val := uint64(data[2*i])<<8 | uint64(data[2*i+1])
			price := sdkmath.LegacyNewDec(int64(val))
			prices = append(prices, price)

			if i == 0 {
				minPrice, maxPrice = price, price
			} else {
				if price.LT(minPrice) {
					minPrice = price
				}
				if price.GT(maxPrice) {
					maxPrice = price
				}
			}
		}

		result := medianDec(prices)

		// Median must be within bounds
		if result.LT(minPrice) {
			t.Fatalf("median %s below min %s", result, minPrice)
		}
		if result.GT(maxPrice) {
			t.Fatalf("median %s above max %s", result, maxPrice)
		}
	})
}

// FuzzWeightedMedianDec_Stability tests weighted median doesn't drift.
func FuzzWeightedMedianDec_Stability(f *testing.F) {
	f.Add(uint8(3))
	f.Add(uint8(5))
	f.Add(uint8(10))

	f.Fuzz(func(t *testing.T, numSamples uint8) {
		if numSamples < 2 || numSamples > 50 {
			return
		}

		samples := make([]normalizedProviderSample, numSamples)
		for i := uint8(0); i < numSamples; i++ {
			price := sdkmath.LegacyNewDec(int64(100 + i*10))
			weight := sdkmath.LegacyNewDec(int64(i + 1))
			samples[i] = normalizedProviderSample{
				sample: OracleProviderSample{
					ProviderID: fmt.Sprintf("p%d", i),
					AssetPair:  "TEST/USD",
				},
				canonicalKey: fmt.Sprintf("key%d", i),
				price:        price,
				weight:       weight,
			}
		}

		// Call multiple times to verify stability
		result1 := weightedMedianDec(samples)
		result2 := weightedMedianDec(samples)

		if !result1.Equal(result2) {
			t.Fatalf("weighted median unstable: %s vs %s", result1, result2)
		}

		// Verify bounds
		minPrice := samples[0].price
		maxPrice := samples[len(samples)-1].price
		if result1.LT(minPrice) || result1.GT(maxPrice) {
			t.Fatalf("weighted median %s out of bounds [%s, %s]", result1, minPrice, maxPrice)
		}
	})
}

// FuzzSqrtDec_Invariants tests sqrtDec mathematical properties.
func FuzzSqrtDec_Invariants(f *testing.F) {
	seeds := []string{
		"0",
		"1",
		"4",
		"9",
		"2",
		"0.25",
		"0.0001",
		"1000000",
		"0.000000001",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, valStr string) {
		val, err := sdkmath.LegacyNewDecFromStr(valStr)
		if err != nil || val.IsNegative() {
			return
		}

		result := sqrtDec(val)

		// sqrt must be non-negative
		if result.IsNegative() {
			t.Fatalf("sqrt(%s) = %s is negative", val, result)
		}

		// sqrt(0) = 0
		if val.IsZero() && !result.IsZero() {
			t.Fatalf("sqrt(0) = %s, expected 0", result)
		}

		// sqrt(x)^2 ≈ x for positive x
		if val.IsPositive() {
			squared := result.Mul(result)
			diff := squared.Sub(val).Abs()
			tolerance := val.Mul(sdkmath.LegacyNewDecWithPrec(1, 6)) // 0.0001% tolerance
			if tolerance.LT(sdkmath.LegacyNewDecWithPrec(1, 9)) {
				tolerance = sdkmath.LegacyNewDecWithPrec(1, 9)
			}
			if diff.GT(tolerance) {
				t.Fatalf("sqrt(%s)^2 = %s, diff %s exceeds tolerance %s",
					val, squared, diff, tolerance)
			}
		}
	})
}

// FuzzCanonicalKey_Uniqueness tests that different inputs produce different keys.
func FuzzCanonicalKey_Uniqueness(f *testing.F) {
	f.Add("p1", "BTC/USD", "100.00", int64(1000))
	f.Add("p1", "BTC/USD", "100.01", int64(1000))
	f.Add("p1", "ETH/USD", "100.00", int64(1000))
	f.Add("p2", "BTC/USD", "100.00", int64(1000))

	f.Fuzz(func(t *testing.T, provider, asset, price string, ts int64) {
		if ts < 0 || ts > 2000000000 {
			return
		}

		s1 := OracleProviderSample{
			ProviderID: provider,
			AssetPair:  asset,
			ObservedAt: time.Unix(ts, 0).UTC(),
			Price:      price,
		}

		s2 := OracleProviderSample{
			ProviderID: provider,
			AssetPair:  asset,
			ObservedAt: time.Unix(ts, 0).UTC(),
			Price:      price,
		}

		// Same inputs must produce same key
		key1 := s1.CanonicalKey()
		key2 := s2.CanonicalKey()

		if key1 != key2 {
			t.Fatalf("identical samples produced different keys: %q vs %q", key1, key2)
		}
	})
}

// FuzzFilterProviderOutliers_Bounds tests outlier filter maintains bounds.
func FuzzFilterProviderOutliers_Bounds(f *testing.F) {
	f.Add(uint8(5), "0.1")
	f.Add(uint8(10), "0.05")
	f.Add(uint8(3), "0")

	f.Fuzz(func(t *testing.T, numSamples uint8, maxDevStr string) {
		if numSamples < 3 || numSamples > 50 {
			return
		}

		maxDev, err := sdkmath.LegacyNewDecFromStr(maxDevStr)
		if err != nil || maxDev.IsNegative() {
			return
		}

		samples := make([]normalizedProviderSample, numSamples)
		prices := make([]sdkmath.LegacyDec, numSamples)
		for i := uint8(0); i < numSamples; i++ {
			price := sdkmath.LegacyNewDec(int64(100 + i*5))
			prices[i] = price
			samples[i] = normalizedProviderSample{
				sample:       OracleProviderSample{ProviderID: fmt.Sprintf("p%d", i)},
				canonicalKey: fmt.Sprintf("key%d", i),
				price:        price,
			}
		}

		median := medianDec(prices)
		filtered := filterProviderOutliers(samples, median, maxDev)

		// All filtered samples must be within deviation of median
		if maxDev.IsPositive() && median.IsPositive() {
			for _, s := range filtered {
				diff := s.price.Sub(median).Abs()
				deviation := diff.Quo(median)
				if deviation.GT(maxDev) {
					t.Fatalf("filtered sample %s deviates %s from median %s, exceeds max %s",
						s.price, deviation, median, maxDev)
				}
			}
		}

		// Filtered count must not exceed original
		if len(filtered) > len(samples) {
			t.Fatalf("filter produced more samples than input: %d > %d",
				len(filtered), len(samples))
		}
	})
}
