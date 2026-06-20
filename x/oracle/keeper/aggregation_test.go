//go:build cosmos
// +build cosmos

package keeper

import (
	"crypto/subtle"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

var testTime = time.Unix(1_700_000_000, 0).UTC()

// --- sqrtDec ---

func TestSqrtDec_Zero(t *testing.T) {
	result := sqrtDec(sdkmath.LegacyZeroDec())
	assert.True(t, result.IsZero())
}

func TestSqrtDec_Negative(t *testing.T) {
	result := sqrtDec(sdkmath.LegacyNewDec(-4))
	assert.True(t, result.IsZero())
}

func TestSqrtDec_PerfectSquare4(t *testing.T) {
	result := sqrtDec(sdkmath.LegacyNewDec(4))
	diff := result.Sub(sdkmath.LegacyNewDec(2)).Abs()
	assert.True(t, diff.LT(sdkmath.LegacyNewDecWithPrec(1, 6)), "sqrt(4) should be ~2, got %s", result)
}

func TestSqrtDec_PerfectSquare9(t *testing.T) {
	result := sqrtDec(sdkmath.LegacyNewDec(9))
	diff := result.Sub(sdkmath.LegacyNewDec(3)).Abs()
	assert.True(t, diff.LT(sdkmath.LegacyNewDecWithPrec(1, 6)), "sqrt(9) should be ~3, got %s", result)
}

func TestSqrtDec_Irrational(t *testing.T) {
	result := sqrtDec(sdkmath.LegacyNewDec(2))
	// sqrt(2) ≈ 1.41421356
	expected := sdkmath.LegacyMustNewDecFromStr("1.41421356")
	diff := result.Sub(expected).Abs()
	assert.True(t, diff.LT(sdkmath.LegacyNewDecWithPrec(1, 4)), "sqrt(2) should be ~1.41421356, got %s", result)
}

func TestSqrtDec_Large(t *testing.T) {
	result := sqrtDec(sdkmath.LegacyNewDec(1_000_000))
	diff := result.Sub(sdkmath.LegacyNewDec(1000)).Abs()
	assert.True(t, diff.LT(sdkmath.LegacyNewDecWithPrec(1, 3)), "sqrt(1000000) should be ~1000, got %s", result)
}

func TestSqrtDec_SmallFraction(t *testing.T) {
	result := sqrtDec(sdkmath.LegacyNewDecWithPrec(25, 2)) // 0.25
	diff := result.Sub(sdkmath.LegacyNewDecWithPrec(5, 1)).Abs()
	assert.True(t, diff.LT(sdkmath.LegacyNewDecWithPrec(1, 6)), "sqrt(0.25) should be ~0.5, got %s", result)
}

// --- filterOutliers ---

func TestFilterOutliers_NoOutliers(t *testing.T) {
	prices := []sdkmath.LegacyDec{
		sdkmath.LegacyNewDec(100),
		sdkmath.LegacyNewDec(101),
		sdkmath.LegacyNewDec(102),
	}
	median := sdkmath.LegacyNewDec(101)
	maxDev := sdkmath.LegacyNewDecWithPrec(10, 2) // 10%

	result := filterOutliers(prices, median, maxDev)
	assert.Len(t, result, 3)
}

func TestFilterOutliers_SomeOutliers(t *testing.T) {
	prices := []sdkmath.LegacyDec{
		sdkmath.LegacyNewDec(100),
		sdkmath.LegacyNewDec(101),
		sdkmath.LegacyNewDec(200), // big outlier
	}
	median := sdkmath.LegacyNewDec(101)
	maxDev := sdkmath.LegacyNewDecWithPrec(10, 2) // 10%

	result := filterOutliers(prices, median, maxDev)
	assert.Len(t, result, 2) // 200 is filtered out
}

func TestFilterOutlierVotes_ExcludesOutlierValidatorsFromRewardSet(t *testing.T) {
	// Mirrors TestFilterOutliers_SomeOutliers but on votes: the outlier
	// validator (v-out, price 200) must be dropped so it is never rewarded
	// for an aggregation it did not shape.
	votes := []priceVoteData{
		{validatorAddr: "v-a", price: sdkmath.LegacyNewDec(100)},
		{validatorAddr: "v-b", price: sdkmath.LegacyNewDec(101)},
		{validatorAddr: "v-out", price: sdkmath.LegacyNewDec(200)},
	}
	median := sdkmath.LegacyNewDec(101)
	maxDev := sdkmath.LegacyNewDecWithPrec(10, 2) // 10%

	result := filterOutlierVotes(votes, median, maxDev)
	require.Len(t, result, 2)
	kept := map[string]bool{}
	for _, v := range result {
		kept[v.validatorAddr] = true
	}
	assert.True(t, kept["v-a"])
	assert.True(t, kept["v-b"])
	assert.False(t, kept["v-out"], "outlier validator must be excluded from the reward set")
}

func TestFilterOutlierVotes_ParityWithFilterOutliers(t *testing.T) {
	// The votes filter and the price filter must keep the same SET, or the
	// reward count would drift from the aggregation's NumValidators.
	votes := []priceVoteData{
		{validatorAddr: "v1", price: sdkmath.LegacyNewDec(100)},
		{validatorAddr: "v2", price: sdkmath.LegacyNewDec(105)},
		{validatorAddr: "v3", price: sdkmath.LegacyNewDec(95)},
		{validatorAddr: "v4", price: sdkmath.LegacyNewDec(500)},
	}
	prices := make([]sdkmath.LegacyDec, len(votes))
	for i, v := range votes {
		prices[i] = v.price
	}
	median := sdkmath.LegacyNewDec(100)
	maxDev := sdkmath.LegacyNewDecWithPrec(10, 2)

	assert.Equal(t, len(filterOutliers(prices, median, maxDev)), len(filterOutlierVotes(votes, median, maxDev)),
		"votes filter must keep the same count as the price filter")
}

func TestFilterOutlierVotes_ZeroMedianAndZeroDeviationKeepAll(t *testing.T) {
	votes := []priceVoteData{
		{validatorAddr: "v1", price: sdkmath.LegacyNewDec(100)},
		{validatorAddr: "v2", price: sdkmath.LegacyNewDec(900)},
	}
	// Non-positive median → keep all (cannot compute deviation).
	assert.Len(t, filterOutlierVotes(votes, sdkmath.LegacyZeroDec(), sdkmath.LegacyNewDecWithPrec(10, 2)), 2)
	// Zero max deviation → filtering disabled, keep all.
	assert.Len(t, filterOutlierVotes(votes, sdkmath.LegacyNewDec(100), sdkmath.LegacyZeroDec()), 2)
}

func TestFilterOutliers_AllOutliers(t *testing.T) {
	prices := []sdkmath.LegacyDec{
		sdkmath.LegacyNewDec(50),
		sdkmath.LegacyNewDec(200),
	}
	median := sdkmath.LegacyNewDec(125)
	maxDev := sdkmath.LegacyNewDecWithPrec(1, 2) // 1% - very tight

	result := filterOutliers(prices, median, maxDev)
	assert.Empty(t, result)
}

func TestFilterOutliers_ZeroMedian(t *testing.T) {
	prices := []sdkmath.LegacyDec{
		sdkmath.LegacyNewDec(100),
		sdkmath.LegacyNewDec(200),
	}
	median := sdkmath.LegacyZeroDec()
	maxDev := sdkmath.LegacyNewDecWithPrec(10, 2)

	// Zero median => return all prices (cannot compute deviation)
	result := filterOutliers(prices, median, maxDev)
	assert.Len(t, result, 2)
}

func TestFilterOutliers_ZeroMaxDeviation(t *testing.T) {
	prices := []sdkmath.LegacyDec{
		sdkmath.LegacyNewDec(100),
		sdkmath.LegacyNewDec(200),
	}
	median := sdkmath.LegacyNewDec(150)
	maxDev := sdkmath.LegacyZeroDec()

	// Zero max deviation => include all (no filtering)
	result := filterOutliers(prices, median, maxDev)
	assert.Len(t, result, 2)
}

// --- filterStaleVotes ---

func TestFilterStaleVotes_AllValid(t *testing.T) {
	now := testTime
	votes := []*types.ValidatorVote{
		{ValidatorAddress: "v1", Timestamp: timestamppb.New(now.Add(-10 * time.Second))},
		{ValidatorAddress: "v2", Timestamp: timestamppb.New(now.Add(-20 * time.Second))},
	}
	result := filterStaleVotes(votes, now, 300)
	assert.Len(t, result, 2)
}

func TestFilterStaleVotes_SomeStale(t *testing.T) {
	now := testTime
	votes := []*types.ValidatorVote{
		{ValidatorAddress: "v1", Timestamp: timestamppb.New(now.Add(-10 * time.Second))},
		{ValidatorAddress: "v2", Timestamp: timestamppb.New(now.Add(-600 * time.Second))}, // stale
	}
	result := filterStaleVotes(votes, now, 300)
	assert.Len(t, result, 1)
	assert.Equal(t, "v1", result[0].ValidatorAddress)
}

func TestFilterStaleVotes_AllStale(t *testing.T) {
	now := testTime
	votes := []*types.ValidatorVote{
		{ValidatorAddress: "v1", Timestamp: timestamppb.New(now.Add(-600 * time.Second))},
		{ValidatorAddress: "v2", Timestamp: timestamppb.New(now.Add(-700 * time.Second))},
	}
	result := filterStaleVotes(votes, now, 300)
	assert.Empty(t, result)
}

func TestFilterStaleVotes_FutureVotes(t *testing.T) {
	now := testTime
	votes := []*types.ValidatorVote{
		{ValidatorAddress: "v1", Timestamp: timestamppb.New(now.Add(60 * time.Second))}, // future
	}
	result := filterStaleVotes(votes, now, 300)
	assert.Empty(t, result) // future votes have negative age, filtered
}

func TestFilterStaleVotes_NilVote(t *testing.T) {
	now := testTime
	votes := []*types.ValidatorVote{
		nil,
		{ValidatorAddress: "v1", Timestamp: timestamppb.New(now.Add(-10 * time.Second))},
	}
	result := filterStaleVotes(votes, now, 300)
	assert.Len(t, result, 1)
}

func TestFilterStaleVotes_NilTimestamp(t *testing.T) {
	now := testTime
	votes := []*types.ValidatorVote{
		{ValidatorAddress: "v1", Timestamp: nil},
		{ValidatorAddress: "v2", Timestamp: timestamppb.New(now.Add(-10 * time.Second))},
	}
	result := filterStaleVotes(votes, now, 300)
	assert.Len(t, result, 1)
}

func TestFilterStaleVotes_MaxAgeZero(t *testing.T) {
	now := testTime
	votes := []*types.ValidatorVote{
		{ValidatorAddress: "v1", Timestamp: timestamppb.New(now.Add(-999 * time.Second))},
	}
	// maxAge <= 0 means no filtering
	result := filterStaleVotes(votes, now, 0)
	assert.Len(t, result, 1)
}

// --- groupVotesByAsset ---

func TestGroupVotesByAsset_Basic(t *testing.T) {
	votes := []*types.ValidatorVote{
		{
			ValidatorAddress: "v1",
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: "1.50"},
				{AssetPair: "ETH/USD", Price: "2000"},
			},
		},
		{
			ValidatorAddress: "v2",
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: "1.52"},
			},
		},
	}
	result := groupVotesByAsset(votes)
	assert.Len(t, result, 2)
	assert.Len(t, result["LAC/USD"], 2)
	assert.Len(t, result["ETH/USD"], 1)
}

func TestGroupVotesByAsset_NilVotes(t *testing.T) {
	votes := []*types.ValidatorVote{nil, nil}
	result := groupVotesByAsset(votes)
	assert.Empty(t, result)
}

func TestGroupVotesByAsset_EmptyAssetPair(t *testing.T) {
	votes := []*types.ValidatorVote{
		{
			ValidatorAddress: "v1",
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "", Price: "1.50"},
			},
		},
	}
	result := groupVotesByAsset(votes)
	assert.Empty(t, result) // empty asset pair is skipped
}

func TestGroupVotesByAsset_InvalidPrice(t *testing.T) {
	votes := []*types.ValidatorVote{
		{
			ValidatorAddress: "v1",
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: "not-a-number"},
			},
		},
	}
	result := groupVotesByAsset(votes)
	assert.Empty(t, result) // invalid price is skipped
}

func TestGroupVotesByAsset_NilFeed(t *testing.T) {
	votes := []*types.ValidatorVote{
		{
			ValidatorAddress: "v1",
			PriceFeeds:       []*types.PriceFeed{nil},
		},
	}
	result := groupVotesByAsset(votes)
	assert.Empty(t, result)
}

func TestGroupVotesByAssetWithDrops_ReportsDuplicates(t *testing.T) {
	votes := []*types.ValidatorVote{
		{
			ValidatorAddress: "valA",
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: "1.00"},
				{AssetPair: "LAC/USD", Price: "1.50"}, // duplicate within same vote
				{AssetPair: "ETH/USD", Price: "2000"},
			},
		},
		{
			ValidatorAddress: "valB",
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: "1.10"},
			},
		},
	}
	result, drops := groupVotesByAssetWithDrops(votes)

	// valA's LAC/USD feeds are both dropped; valB's LAC/USD entry is kept;
	// valA's ETH/USD entry is kept.
	assert.Len(t, result["LAC/USD"], 1, "only valB's LAC/USD entry should remain")
	assert.Equal(t, "valB", result["LAC/USD"][0].validatorAddr)
	assert.Len(t, result["ETH/USD"], 1)

	require.Len(t, drops, 2, "both duplicate LAC/USD submissions from valA must be reported")
	for _, d := range drops {
		assert.Equal(t, "valA", d.Validator)
		assert.Equal(t, "LAC/USD", d.AssetPair)
	}
}

// --- BuildCanonicalPriceFeeds ---

func TestBuildCanonicalPriceFeeds_MedianFiltersInvalidSamples(t *testing.T) {
	cfg := DefaultProviderAggregationConfig()
	cfg.MaxProviders = 4
	cfg.MaxSampleAge = 5 * time.Minute
	cfg.MaxPriceDeviation = "0.10"
	cfg.ProviderConfigs = map[string]OracleProviderConfig{
		"binance": {
			CanonicalAssetPairs: map[string]string{"LACUSDT": "LAC/USD"},
		},
		"coinbase": {
			CanonicalAssetPairs: map[string]string{"LAC-USD": "LAC/USD"},
		},
		"kraken": {},
		"stale":  {},
		"badSig": {},
	}
	cfg.SignatureVerifier = func(sample OracleProviderSample, _ OracleProviderConfig) error {
		expected := "sig:" + sample.ProviderID
		if subtle.ConstantTimeCompare([]byte(sample.Signature), []byte(expected)) != 1 {
			return fmt.Errorf("expected %s", expected)
		}
		return nil
	}

	report, err := BuildCanonicalPriceFeeds(
		testTime,
		[]string{"LAC/USD"},
		[]OracleProviderSample{
			{
				ProviderID:      "binance",
				AssetPair:       "LACUSDT",
				ObservedAt:      testTime.Add(-30 * time.Second),
				Price:           "100",
				Volume24H:       "10",
				ConfidenceScore: "0.90",
				Signature:       "sig:binance",
			},
			{
				ProviderID:      "coinbase",
				AssetPair:       "LAC-USD",
				ObservedAt:      testTime.Add(-20 * time.Second),
				Price:           "101",
				Volume24H:       "20",
				ConfidenceScore: "0.80",
				Signature:       "sig:coinbase",
			},
			{
				ProviderID:      "kraken",
				AssetPair:       "LAC/USD",
				ObservedAt:      testTime.Add(-10 * time.Second),
				Price:           "150",
				Volume24H:       "30",
				ConfidenceScore: "0.70",
				Signature:       "sig:kraken",
			},
			{
				ProviderID:      "stale",
				AssetPair:       "LAC/USD",
				ObservedAt:      testTime.Add(-10 * time.Minute),
				Price:           "99",
				Volume24H:       "5",
				ConfidenceScore: "0.70",
				Signature:       "sig:stale",
			},
			{
				ProviderID:      "badSig",
				AssetPair:       "LAC/USD",
				ObservedAt:      testTime.Add(-15 * time.Second),
				Price:           "100.5",
				Volume24H:       "7",
				ConfidenceScore: "0.60",
				Signature:       "wrong",
			},
		},
		cfg,
	)
	require.NoError(t, err)
	require.Len(t, report.Feeds, 1)

	feed := report.Feeds[0]
	require.Equal(t, "LAC/USD", feed.AssetPair)
	require.Equal(t, "100.500000000000000000", feed.Price)
	require.Equal(t, "30.000000000000000000", feed.Volume_24H)
	require.Equal(t, "0.850000000000000000", feed.ConfidenceScore)
	require.Equal(t, []string{"binance", "coinbase"}, feed.Sources)
	require.NotNil(t, feed.Timestamp)
	require.True(t, feed.Timestamp.AsTime().Equal(testTime))
	require.Equal(t, 3, report.AcceptedSamples)
	require.Equal(t, 2, report.RetainedSamples)
	require.Equal(t, 1, report.FilteredOutliers)

	diagnosticCodes := make(map[string]int)
	for _, diagnostic := range report.Diagnostics {
		diagnosticCodes[diagnostic.Code]++
	}
	require.Equal(t, 1, diagnosticCodes["stale_sample"])
	require.Equal(t, 1, diagnosticCodes["invalid_signature"])
}

func TestBuildCanonicalPriceFeeds_WeightedMedianRespectsProviderBound(t *testing.T) {
	cfg := DefaultProviderAggregationConfig()
	cfg.MaxProviders = 2
	cfg.MaxSampleAge = 5 * time.Minute
	cfg.MaxPriceDeviation = "0.25"
	cfg.AggregationMethod = ProviderAggregationWeightedMedian
	cfg.ProviderConfigs = map[string]OracleProviderConfig{
		"alpha": {Weight: "1"},
		"beta":  {Weight: "4"},
		"gamma": {Weight: "10"},
	}

	report, err := BuildCanonicalPriceFeeds(
		testTime,
		[]string{"LAC/USD"},
		[]OracleProviderSample{
			{
				ProviderID:      "alpha",
				AssetPair:       "LAC/USD",
				ObservedAt:      testTime.Add(-30 * time.Second),
				Price:           "100",
				Volume24H:       "10",
				ConfidenceScore: "0.90",
			},
			{
				ProviderID:      "beta",
				AssetPair:       "LAC/USD",
				ObservedAt:      testTime.Add(-20 * time.Second),
				Price:           "110",
				Volume24H:       "20",
				ConfidenceScore: "0.80",
			},
			{
				ProviderID:      "gamma",
				AssetPair:       "LAC/USD",
				ObservedAt:      testTime.Add(-10 * time.Second),
				Price:           "120",
				Volume24H:       "30",
				ConfidenceScore: "0.70",
			},
		},
		cfg,
	)
	require.NoError(t, err)
	require.Len(t, report.Feeds, 1)

	feed := report.Feeds[0]
	require.Equal(t, "110.000000000000000000", feed.Price)
	require.Equal(t, []string{"alpha", "beta"}, feed.Sources)
	require.Equal(t, 1, report.BoundedDropCount)

	foundBoundDiagnostic := false
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == "provider_limit_exceeded" && diagnostic.ProviderID == "gamma" {
			foundBoundDiagnostic = true
			break
		}
	}
	require.True(t, foundBoundDiagnostic)
}

// --- ValidateVote ---

func TestValidateVote_Valid(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime), // same as block time
	}
	require.NoError(t, k.ValidateVote(ctx, vote))
}

func TestValidateVote_NilVote(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	err := k.ValidateVote(ctx, nil)
	require.Error(t, err)
}

func TestValidateVote_NilTimestamp(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      1,
		Timestamp:        nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp")
}

func TestValidateVote_InvalidTimestamp(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      1,
		Timestamp:        &timestamppb.Timestamp{Nanos: 1_000_000_000},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp invalid")
}

func TestValidateVote_FutureTimestamp(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime.Add(60 * time.Second)),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "future")
}

func TestValidateVote_StaleVote(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	// Default MaxVoteAge is 300 seconds
	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime.Add(-600 * time.Second)),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "age")
}

func TestValidateVote_InvalidPrice(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "0"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime),
	})
	require.Error(t, err)
}

func TestValidateVote_EmptyAssetPair(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "", Price: "1.50"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime),
	})
	require.Error(t, err)
}

func TestValidateVote_AssetPairNotAllowed(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "DOGE/USD", Price: "0.10"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowed")
}

func TestValidateVote_NilPriceFeed(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{nil},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime),
	})
	require.Error(t, err)
}

// --- AggregateVotes (integration) ---

func TestAggregateVotes_NoVotes(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	// No votes => no-op
	require.NoError(t, k.AggregateVotes(ctx))

	prices, err := k.GetAllAggregatedPrices(ctx)
	require.NoError(t, err)
	assert.Empty(t, prices)
}

func TestAggregateVotes_AllStale(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	// Add a stale vote (600s old, max age is 300)
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.50"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime.Add(-600 * time.Second)),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	// Stale votes cleared, no aggregated prices
	prices, err := k.GetAllAggregatedPrices(ctx)
	require.NoError(t, err)
	assert.Empty(t, prices)

	// Votes should be cleared
	votes, err := k.GetAllValidatorVotes(ctx)
	require.NoError(t, err)
	assert.Empty(t, votes)
}

func TestAggregateVotes_SingleVote(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, "1.500000000000000000", agg.MedianPrice)
	assert.Equal(t, "1.500000000000000000", agg.MeanPrice)
	assert.Equal(t, int32(1), agg.NumValidators)
}

func TestAggregateVotes_OddValidators(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	// Three validators with different prices
	for i, price := range []string{"100", "102", "104"} {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: price},
			},
			BlockHeight: int64(i + 1),
			Timestamp:   timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	// Median of [100, 102, 104] = 102
	assert.Equal(t, "102.000000000000000000", agg.MedianPrice)
	assert.Equal(t, int32(3), agg.NumValidators)

	// Votes should be cleared after aggregation
	votes, err := k.GetAllValidatorVotes(ctx)
	require.NoError(t, err)
	assert.Empty(t, votes)
}

func TestAggregateVotes_EvenValidators(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	// Four validators
	for i, price := range []string{"100", "102", "104", "106"} {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: price},
			},
			BlockHeight: int64(i + 1),
			Timestamp:   timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	// Median of [100, 102, 104, 106] = (102+104)/2 = 103
	assert.Equal(t, "103.000000000000000000", agg.MedianPrice)
	assert.Equal(t, int32(4), agg.NumValidators)
}

func TestAggregateVotes_MultipleAssets(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
			{AssetPair: "ETH/USD", Price: "2000"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val2",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.52"},
			{AssetPair: "ETH/USD", Price: "2010"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	lacPrice, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, int32(2), lacPrice.NumValidators)

	ethPrice, err := k.GetAggregatedPrice(ctx, "ETH/USD")
	require.NoError(t, err)
	assert.Equal(t, int32(2), ethPrice.NumValidators)
}

func TestAggregateVotes_OutlierFiltering(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	// Use a tight max deviation of 5%
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0.05",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	// 3 validators: two close, one far outlier
	for i, price := range []string{"100", "101", "200"} {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: price},
			},
			BlockHeight: int64(i + 1),
			Timestamp:   timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	// After outlier filtering, 200 should be removed. Remaining: [100, 101]
	// Median = (100+101)/2 = 100.5
	assert.Equal(t, int32(2), agg.NumValidators)
}

func TestAggregateVotes_OutlierFilteringRecomputesStdDev(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0.05",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	for i, price := range []string{"100", "101", "200"} {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('1'+i)),
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: price},
			},
			BlockHeight: int64(i + 1),
			Timestamp:   timestamppb.New(testTime),
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)

	stdDev, err := sdkmath.LegacyNewDecFromStr(agg.StandardDeviation)
	require.NoError(t, err)
	expected := sdkmath.LegacyMustNewDecFromStr("0.5")
	diff := stdDev.Sub(expected).Abs()
	assert.True(t, diff.LT(sdkmath.LegacyNewDecWithPrec(1, 6)), "stddev=%s want ~%s", stdDev, expected)
}

func TestAggregateVotes_EmitsEvent(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	events := ctx.EventManager().Events()
	found := false
	for _, event := range events {
		if event.Type == types.EventTypeAggregatedPrice {
			found = true
			break
		}
	}
	assert.True(t, found, "expected oracle_aggregated_price event")
}

func TestValidateVote_DuplicateAssetPair(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	err := k.ValidateVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
			{AssetPair: " LAC/USD ", Price: "1.55"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate asset pair")
}

func TestAggregateVotes_IgnoresDuplicateFeedsFromSingleValidator(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0",
		AssetPairs:        []string{"LAC/USD"},
		MaxVoteAge:        300,
	}))

	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-malicious",
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: "LAC/USD", Price: "1000"},
			{AssetPair: "LAC/USD", Price: "1000"},
		},
		BlockHeight: 1,
		Timestamp:   timestamppb.New(testTime),
	}))
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-honest-1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "100"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime),
	}))
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-honest-2",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "101"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(testTime),
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	assert.Equal(t, "100.500000000000000000", agg.MedianPrice)
	assert.Equal(t, "100.500000000000000000", agg.MeanPrice)
	assert.Equal(t, int32(2), agg.NumValidators)
}
