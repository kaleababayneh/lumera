//go:build cosmos

package keeper

import (
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// =============================================================================
// Price Aggregation Tests
// =============================================================================

func TestAggregateVotes_MedianCalculation_OddCount(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0", // No outlier filtering
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// Three validators with prices 100, 105, 110 - median should be 105
	prices := []string{"100", "105", "110"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, "105.000000000000000000", agg.MedianPrice)
	assert.Equal(t, int32(3), agg.NumValidators)
}

func TestAggregateVotes_MedianCalculation_EvenCount(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// Four validators: 100, 102, 108, 110 - median = (102+108)/2 = 105
	prices := []string{"100", "102", "108", "110"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, "105.000000000000000000", agg.MedianPrice)
	assert.Equal(t, int32(4), agg.NumValidators)
}

func TestAggregateVotes_MeanCalculation(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// Three validators: 100, 110, 120 - mean = 330/3 = 110
	prices := []string{"100", "110", "120"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, "110.000000000000000000", agg.MeanPrice)
}

func TestAggregateVotes_SingleValidator(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "42.5"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, "42.500000000000000000", agg.MedianPrice)
	assert.Equal(t, "42.500000000000000000", agg.MeanPrice)
	assert.Equal(t, int32(1), agg.NumValidators)
}

func TestAggregateVotes_MultipleAssetPairs(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD", "LUME/USD"},
		MaxVoteAge:        300,
	}))

	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
			{AssetPair: "LUME/USD", Price: "0.25"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val2",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.60"},
			{AssetPair: "LUME/USD", Price: "0.30"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	lacAgg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, "1.550000000000000000", lacAgg.MedianPrice)

	lumeAgg, err := k.GetAggregatedPrice(ctx, "LUME/USD")
	require.NoError(t, err)
	assert.Equal(t, "0.275000000000000000", lumeAgg.MedianPrice)
}

// =============================================================================
// StdDev Filtering Tests
// =============================================================================

func TestAggregateVotes_StdDevFromFilteredPrices(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0.10", // 10% deviation threshold
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// Prices: 100, 101, 102, 300 (outlier)
	// After filtering at 10%, 300 should be removed
	// StdDev should be computed from [100, 101, 102]
	prices := []string{"100", "101", "102", "300"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)

	// Only 3 validators after filtering
	assert.Equal(t, int32(3), agg.NumValidators)

	// StdDev of [100, 101, 102] with mean 101:
	// variance = ((100-101)^2 + (101-101)^2 + (102-101)^2) / 3 = (1 + 0 + 1) / 3 = 0.666...
	// stddev = sqrt(0.666...) ≈ 0.8165
	stdDev, err := sdkmath.LegacyNewDecFromStr(agg.StandardDeviation)
	require.NoError(t, err)
	expected := sdkmath.LegacyMustNewDecFromStr("0.816496580927726")
	diff := stdDev.Sub(expected).Abs()
	assert.True(t, diff.LT(sdkmath.LegacyNewDecWithPrec(1, 4)),
		"stddev=%s want ~%s", stdDev, expected)
}

func TestAggregateVotes_OutlierFiltering_TightThreshold(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0.02", // 2% - very tight threshold
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// Prices: 100, 105 (>2% deviation from median of 102.5)
	prices := []string{"100", "105"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	// When all prices are filtered as outliers, the system should fall back
	// to using unfiltered prices
	assert.True(t, agg.NumValidators > 0, "Should have validators even if all would be outliers")
}

func TestAggregateVotes_OutlierFiltering_NoFiltering(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0", // Disabled
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// Extreme outlier should be included with no filtering
	prices := []string{"100", "101", "1000"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	// All 3 validators should be included
	assert.Equal(t, int32(3), agg.NumValidators)
}

func TestStdDevFromPrices_Uniform(t *testing.T) {
	prices := []sdkmath.LegacyDec{
		sdkmath.LegacyNewDec(100),
		sdkmath.LegacyNewDec(100),
		sdkmath.LegacyNewDec(100),
	}
	mean := sdkmath.LegacyNewDec(100)
	stdDev := stdDevFromPrices(prices, mean)
	assert.True(t, stdDev.IsZero(), "StdDev of uniform values should be zero")
}

func TestStdDevFromPrices_Varied(t *testing.T) {
	// Values: 2, 4, 4, 4, 5, 5, 7, 9 -> mean = 5, variance = 4, stddev = 2
	prices := []sdkmath.LegacyDec{
		sdkmath.LegacyNewDec(2),
		sdkmath.LegacyNewDec(4),
		sdkmath.LegacyNewDec(4),
		sdkmath.LegacyNewDec(4),
		sdkmath.LegacyNewDec(5),
		sdkmath.LegacyNewDec(5),
		sdkmath.LegacyNewDec(7),
		sdkmath.LegacyNewDec(9),
	}
	mean := sdkmath.LegacyNewDec(5)
	stdDev := stdDevFromPrices(prices, mean)
	expected := sdkmath.LegacyNewDec(2)
	diff := stdDev.Sub(expected).Abs()
	assert.True(t, diff.LT(sdkmath.LegacyNewDecWithPrec(1, 4)),
		"StdDev should be ~2, got %s", stdDev)
}

func TestStdDevFromPrices_Empty(t *testing.T) {
	prices := []sdkmath.LegacyDec{}
	mean := sdkmath.LegacyZeroDec()
	stdDev := stdDevFromPrices(prices, mean)
	assert.True(t, stdDev.IsZero())
}

func TestStdDevFromPrices_Single(t *testing.T) {
	prices := []sdkmath.LegacyDec{sdkmath.LegacyNewDec(100)}
	mean := sdkmath.LegacyNewDec(100)
	stdDev := stdDevFromPrices(prices, mean)
	assert.True(t, stdDev.IsZero(), "StdDev of single value should be zero")
}

// =============================================================================
// Duplicate Feed Rejection Tests
// =============================================================================

func TestValidateVote_RejectsDuplicateAssetPair(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
			{AssetPair: "LAC/USD", Price: "1.55"}, // Duplicate
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate asset pair")
}

func TestValidateVote_RejectsDuplicateWithWhitespace(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
			{AssetPair: "  LAC/USD  ", Price: "1.55"}, // Duplicate with whitespace
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate asset pair")
}

func TestAggregateVotes_IgnoresDuplicateFeedsFromValidator(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// Malicious validator tries to stuff duplicates to skew median
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-malicious",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "500"},
			{AssetPair: "LAC/USD", Price: "500"},
			{AssetPair: "LAC/USD", Price: "500"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))
	// Honest validators
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-honest-1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "100"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime),
	}))
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-honest-2",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "102"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)

	// Duplicates from single validator should be ignored
	// Only 2 honest validators should count
	assert.Equal(t, int32(2), agg.NumValidators)
	// Median of [100, 102] = 101
	assert.Equal(t, "101.000000000000000000", agg.MedianPrice)
}

func TestAggregateVotes_RejectsDuplicateAssetFromSingleValidator(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// Single validator submits duplicate asset pair feeds
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1000"},
			{AssetPair: "LAC/USD", Price: "2000"}, // Duplicate
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	// With the validator's duplicate feeds dropped there are no votes left
	// for LAC/USD, so no aggregated price is stored for the period.
	// GetAggregatedPrice must surface that as "not found" rather than a
	// NumValidators=0 record, so downstream consumers don't mistake a
	// missing price for a consensus-derived zero.
	_, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.Error(t, err)
}

func TestAggregateVotes_ValidatorWithUniqueFeeds(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD", "LUME/USD"},
		MaxVoteAge:        300,
	}))

	// Valid: different asset pairs
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
			{AssetPair: "LUME/USD", Price: "0.25"}, // Different asset pair - OK
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	lacAgg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, int32(1), lacAgg.NumValidators)

	lumeAgg, err := k.GetAggregatedPrice(ctx, "LUME/USD")
	require.NoError(t, err)
	assert.Equal(t, int32(1), lumeAgg.NumValidators)
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestAggregateVotes_NoVotes_SpecificAssetAbsent(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// No votes submitted
	require.NoError(t, k.AggregateVotes(ctx))

	_, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.Error(t, err) // Should fail - no price data
}

func TestAggregateVotes_ExpiredVotes(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        60, // 60 seconds max age
	}))

	oldTime := time.Unix(1_700_000_000, 0).UTC()
	ctx = ctx.WithBlockTime(oldTime.Add(120 * time.Second)) // 120 seconds later

	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "100"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(oldTime), // Too old
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	// All votes are stale; aggregation short-circuits before storing any
	// price, so GetAggregatedPrice must return an error rather than a
	// NumValidators=0 record. (ClearValidatorVotes on the all-stale path
	// does not synthesize empty aggregates.)
	_, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.Error(t, err)
}

func TestAggregateVotes_PrecisionHandling(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// High precision prices
	prices := []string{"1.123456789012345678", "1.123456789012345679", "1.123456789012345680"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, int32(3), agg.NumValidators)
	// Should handle 18 decimal precision
	assert.Contains(t, agg.MedianPrice, "1.123456789012345")
}

func TestAggregateVotes_VeryLargePrices(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"BTC/USD"},
		MaxVoteAge:        300,
	}))

	// Very large prices (like BTC)
	prices := []string{"99999", "100000", "100001"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "BTC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "BTC/USD")
	require.NoError(t, err)
	assert.Equal(t, "100000.000000000000000000", agg.MedianPrice)
}

func TestAggregateVotes_VerySmallPrices(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"SHIB/USD"},
		MaxVoteAge:        300,
	}))

	// Very small prices (like meme coins)
	prices := []string{"0.000001", "0.000002", "0.000003"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "SHIB/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "SHIB/USD")
	require.NoError(t, err)
	assert.Equal(t, "0.000002000000000000", agg.MedianPrice)
}
