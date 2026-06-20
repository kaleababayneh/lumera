//go:build cosmos

package keeper

import (
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

func setupOracleKeeper(t *testing.T) (sdk.Context, *Keeper) {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()

	cms := rootmulti.NewStore(db, logger)
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)
	authority := authtypes.NewModuleAddress("gov").String()

	keeper := NewKeeper(cdc, runtime.NewKVStoreService(storeKey), authority)

	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	sdkCtx := sdk.NewContext(cms, header, false, logger)

	return sdkCtx, keeper
}

// ---------------------------------------------------------------------------
// Params
// ---------------------------------------------------------------------------

func TestGetParamsDefault(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.NotNil(t, params)
	require.Equal(t, int64(10), params.VotePeriod)
	require.Equal(t, "0.67", params.VoteThreshold)
}

func TestSetParamsRoundTrip(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	p := types.DefaultParams()
	p.VotePeriod = 20
	require.NoError(t, k.SetParams(ctx, p))

	got, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(20), got.VotePeriod)
}

func TestSetParamsNil(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	err := k.SetParams(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestSetParamsInvalid(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	p := types.DefaultParams()
	p.VotePeriod = 0 // invalid
	err := k.SetParams(ctx, p)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Authority
// ---------------------------------------------------------------------------

func TestValidateAuthority(t *testing.T) {
	_, k := setupOracleKeeper(t)
	require.NoError(t, k.ValidateAuthority(k.GetAuthority()))
	require.Error(t, k.ValidateAuthority("wrong-address"))
}

// ---------------------------------------------------------------------------
// PriceFeed CRUD
// ---------------------------------------------------------------------------

func TestSetGetPriceFeed(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	feed := &types.PriceFeed{
		AssetPair: "LAC/USD",
		Price:     "1.50",
		Timestamp: timestamppb.Now(),
	}
	require.NoError(t, k.SetPriceFeed(ctx, feed))

	got, err := k.GetPriceFeed(ctx, "LAC/USD")
	require.NoError(t, err)
	require.Equal(t, "LAC/USD", got.AssetPair)
	require.Equal(t, "1.500000000000000000", got.Price)
}

func TestSetPriceFeedNil(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	err := k.SetPriceFeed(ctx, nil)
	require.Error(t, err)
}

func TestSetPriceFeedEmptyAsset(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	err := k.SetPriceFeed(ctx, &types.PriceFeed{AssetPair: "", Price: "1.0"})
	require.Error(t, err)
}

func TestSetPriceFeedInvalidPrice(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	tests := []struct {
		name  string
		price string
	}{
		{"negative", "-1.0"},
		{"zero", "0"},
		{"not a number", "abc"},
		{"empty", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := k.SetPriceFeed(ctx, &types.PriceFeed{AssetPair: "LAC/USD", Price: tc.price})
			require.Error(t, err)
		})
	}
}

func TestSetPriceFeedInvalidOptionalMetadata(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	tests := []struct {
		name string
		feed *types.PriceFeed
		want string
	}{
		{
			name: "negative volume",
			feed: &types.PriceFeed{
				AssetPair:  "LAC/USD",
				Price:      "1.0",
				Volume_24H: "-0.01",
			},
			want: "volume_24h cannot be negative",
		},
		{
			name: "invalid volume",
			feed: &types.PriceFeed{
				AssetPair:  "LAC/USD",
				Price:      "1.0",
				Volume_24H: "not-a-decimal",
			},
			want: "invalid volume_24h",
		},
		{
			name: "negative confidence",
			feed: &types.PriceFeed{
				AssetPair:       "LAC/USD",
				Price:           "1.0",
				ConfidenceScore: "-0.01",
			},
			want: "confidence_score must be between 0 and 1",
		},
		{
			name: "confidence above one",
			feed: &types.PriceFeed{
				AssetPair:       "LAC/USD",
				Price:           "1.0",
				ConfidenceScore: "1.01",
			},
			want: "confidence_score must be between 0 and 1",
		},
		{
			name: "invalid confidence",
			feed: &types.PriceFeed{
				AssetPair:       "LAC/USD",
				Price:           "1.0",
				ConfidenceScore: "not-a-decimal",
			},
			want: "invalid confidence_score",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := k.SetPriceFeed(ctx, tc.feed)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestGetPriceFeedNotFound(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	_, err := k.GetPriceFeed(ctx, "MISSING/PAIR")
	require.Error(t, err)
}

func TestGetPriceFeedEmptyAsset(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	_, err := k.GetPriceFeed(ctx, "")
	require.Error(t, err)
}

func TestKeeperRejectsNonCanonicalAssetPairInputs(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, sdk.Context, *Keeper) error
	}{
		{
			name: "set price feed leading padding",
			run: func(_ *testing.T, ctx sdk.Context, k *Keeper) error {
				return k.SetPriceFeed(ctx, &types.PriceFeed{
					AssetPair: " LAC/USD",
					Price:     "1.0",
				})
			},
		},
		{
			name: "get price feed trailing padding",
			run: func(t *testing.T, ctx sdk.Context, k *Keeper) error {
				require.NoError(t, k.SetPriceFeed(ctx, &types.PriceFeed{
					AssetPair: "LAC/USD",
					Price:     "1.0",
				}))
				_, err := k.GetPriceFeed(ctx, "LAC/USD ")
				return err
			},
		},
		{
			name: "set aggregated price trailing padding",
			run: func(_ *testing.T, ctx sdk.Context, k *Keeper) error {
				return k.SetAggregatedPrice(ctx, &types.AggregatedPrice{
					AssetPair:   "ETH/USD ",
					MedianPrice: "1.0",
				})
			},
		},
		{
			name: "get aggregated price leading padding",
			run: func(t *testing.T, ctx sdk.Context, k *Keeper) error {
				require.NoError(t, k.SetAggregatedPrice(ctx, &types.AggregatedPrice{
					AssetPair:   "ETH/USD",
					MedianPrice: "1.0",
				}))
				_, err := k.GetAggregatedPrice(ctx, " ETH/USD")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, k := setupOracleKeeper(t)
			err := tt.run(t, ctx, k)
			require.Error(t, err)
			require.ErrorContains(t, err, "asset pair must not have leading or trailing whitespace")
		})
	}
}

func TestSetPriceFeedRejectsNonCanonicalDecimalInputs(t *testing.T) {
	tests := []struct {
		name string
		feed *types.PriceFeed
		want string
	}{
		{
			name: "padded price",
			feed: &types.PriceFeed{
				AssetPair: "LAC/USD",
				Price:     " 1.0",
			},
			want: "price must not have leading or trailing whitespace",
		},
		{
			name: "padded volume",
			feed: &types.PriceFeed{
				AssetPair:  "LAC/USD",
				Price:      "1.0",
				Volume_24H: "1000 ",
			},
			want: "volume_24h must not have leading or trailing whitespace",
		},
		{
			name: "padded confidence score",
			feed: &types.PriceFeed{
				AssetPair:       "LAC/USD",
				Price:           "1.0",
				ConfidenceScore: "\t0.97",
			},
			want: "confidence_score must not have leading or trailing whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, k := setupOracleKeeper(t)
			require.ErrorContains(t, k.SetPriceFeed(ctx, tt.feed), tt.want)
		})
	}
}

func TestGetAllPriceFeeds(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	for _, pair := range []string{"LAC/USD", "ETH/USD"} {
		require.NoError(t, k.SetPriceFeed(ctx, &types.PriceFeed{
			AssetPair: pair,
			Price:     "100.0",
			Timestamp: timestamppb.Now(),
		}))
	}
	feeds, err := k.GetAllPriceFeeds(ctx)
	require.NoError(t, err)
	require.Len(t, feeds, 2)
}

// ---------------------------------------------------------------------------
// AggregatedPrice CRUD
// ---------------------------------------------------------------------------

func TestSetGetAggregatedPrice(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	ap := &types.AggregatedPrice{
		AssetPair:     "ETH/USD",
		MedianPrice:   "3500.00",
		MeanPrice:     "3510.00",
		NumValidators: 5,
		BlockHeight:   10,
		Timestamp:     timestamppb.Now(),
	}
	require.NoError(t, k.SetAggregatedPrice(ctx, ap))

	got, err := k.GetAggregatedPrice(ctx, "ETH/USD")
	require.NoError(t, err)
	require.Equal(t, int32(5), got.NumValidators)
}

func TestSetAggregatedPriceNil(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.Error(t, k.SetAggregatedPrice(ctx, nil))
}

func TestSetAggregatedPriceEmptyAsset(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.Error(t, k.SetAggregatedPrice(ctx, &types.AggregatedPrice{
		AssetPair:   "",
		MedianPrice: "100",
	}))
}

func TestSetAggregatedPriceNegativeMedian(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.Error(t, k.SetAggregatedPrice(ctx, &types.AggregatedPrice{
		AssetPair:   "LAC/USD",
		MedianPrice: "-1",
	}))
}

func TestSetAggregatedPriceInvalidOptionalStats(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	tests := []struct {
		name  string
		price *types.AggregatedPrice
		want  string
	}{
		{
			name: "negative mean",
			price: &types.AggregatedPrice{
				AssetPair:   "LAC/USD",
				MedianPrice: "1",
				MeanPrice:   "-0.1",
			},
			want: "mean price must be positive",
		},
		{
			name: "zero mean",
			price: &types.AggregatedPrice{
				AssetPair:   "LAC/USD",
				MedianPrice: "1",
				MeanPrice:   "0",
			},
			want: "mean price must be positive",
		},
		{
			name: "negative standard deviation",
			price: &types.AggregatedPrice{
				AssetPair:         "LAC/USD",
				MedianPrice:       "1",
				StandardDeviation: "-0.1",
			},
			want: "standard deviation cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, k.SetAggregatedPrice(ctx, tt.price), tt.want)
		})
	}
}

func TestSetAggregatedPriceRejectsNonCanonicalDecimalInputs(t *testing.T) {
	tests := []struct {
		name  string
		price *types.AggregatedPrice
		want  string
	}{
		{
			name: "padded median",
			price: &types.AggregatedPrice{
				AssetPair:   "LAC/USD",
				MedianPrice: " 1",
			},
			want: "median price must not have leading or trailing whitespace",
		},
		{
			name: "padded mean",
			price: &types.AggregatedPrice{
				AssetPair:   "LAC/USD",
				MedianPrice: "1",
				MeanPrice:   "2 ",
			},
			want: "mean price must not have leading or trailing whitespace",
		},
		{
			name: "padded standard deviation",
			price: &types.AggregatedPrice{
				AssetPair:         "LAC/USD",
				MedianPrice:       "1",
				StandardDeviation: "\t0.1",
			},
			want: "standard deviation must not have leading or trailing whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, k := setupOracleKeeper(t)
			require.ErrorContains(t, k.SetAggregatedPrice(ctx, tt.price), tt.want)
		})
	}
}

func TestGetAggregatedPriceNotFound(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	_, err := k.GetAggregatedPrice(ctx, "NOPE/USD")
	require.Error(t, err)
}

func TestGetAllAggregatedPrices(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	for _, pair := range []string{"LAC/USD", "BTC/USD"} {
		require.NoError(t, k.SetAggregatedPrice(ctx, &types.AggregatedPrice{
			AssetPair:   pair,
			MedianPrice: "1000",
			Timestamp:   timestamppb.Now(),
		}))
	}
	prices, err := k.GetAllAggregatedPrices(ctx)
	require.NoError(t, err)
	require.Len(t, prices, 2)
}

// ---------------------------------------------------------------------------
// ValidatorVote CRUD
// ---------------------------------------------------------------------------

func TestSetGetValidatorVote(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.5"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.Now(),
	}
	require.NoError(t, k.SetValidatorVote(ctx, vote))

	got, err := k.GetValidatorVote(ctx, "val1")
	require.NoError(t, err)
	require.Equal(t, int64(1), got.BlockHeight)
}

func TestSetValidatorVoteNil(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.Error(t, k.SetValidatorVote(ctx, nil))
}

func TestSetValidatorVoteEmptyAddr(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.Error(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "",
		BlockHeight:      1,
	}))
}

func TestSetValidatorVoteZeroHeight(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.Error(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val1",
		BlockHeight:      0,
	}))
}

func TestSetValidatorVoteMonotonicHeight(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	vote1 := &types.ValidatorVote{ValidatorAddress: "val1", BlockHeight: 5, Timestamp: timestamppb.Now()}
	require.NoError(t, k.SetValidatorVote(ctx, vote1))

	vote2 := &types.ValidatorVote{ValidatorAddress: "val1", BlockHeight: 3, Timestamp: timestamppb.Now()}
	err := k.SetValidatorVote(ctx, vote2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be greater than")

	// Same height should also fail
	vote3 := &types.ValidatorVote{ValidatorAddress: "val1", BlockHeight: 5, Timestamp: timestamppb.Now()}
	require.Error(t, k.SetValidatorVote(ctx, vote3))

	// Higher height should succeed
	vote4 := &types.ValidatorVote{ValidatorAddress: "val1", BlockHeight: 10, Timestamp: timestamppb.Now()}
	require.NoError(t, k.SetValidatorVote(ctx, vote4))
}

func TestGetValidatorVoteNotFound(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	_, err := k.GetValidatorVote(ctx, "missing-val")
	require.Error(t, err)
}

func TestGetValidatorVoteEmptyAddr(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	_, err := k.GetValidatorVote(ctx, "")
	require.Error(t, err)
}

func TestGetAllValidatorVotes(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	for i := 1; i <= 3; i++ {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('0'+i)),
			BlockHeight:      int64(i),
			Timestamp:        timestamppb.Now(),
		}))
	}
	votes, err := k.GetAllValidatorVotes(ctx)
	require.NoError(t, err)
	require.Len(t, votes, 3)
}

func TestClearValidatorVotes(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	for i := 1; i <= 3; i++ {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val" + string(rune('0'+i)),
			BlockHeight:      int64(i),
			Timestamp:        timestamppb.Now(),
		}))
	}
	require.NoError(t, k.ClearValidatorVotes(ctx))

	votes, err := k.GetAllValidatorVotes(ctx)
	require.NoError(t, err)
	require.Empty(t, votes)
}

// ---------------------------------------------------------------------------
// Pure aggregation helpers
// ---------------------------------------------------------------------------

func TestFilterStaleVotes(t *testing.T) {
	now := time.Unix(1_700_000_100, 0).UTC()
	ts := func(sec int64) *timestamppb.Timestamp {
		return timestamppb.New(time.Unix(sec, 0).UTC())
	}
	votes := []*types.ValidatorVote{
		{ValidatorAddress: "v1", Timestamp: ts(1_700_000_050)}, // 50s ago - ok
		{ValidatorAddress: "v2", Timestamp: ts(1_700_000_000)}, // 100s ago - ok
		{ValidatorAddress: "v3", Timestamp: ts(1_699_999_700)}, // 400s ago - stale
		{ValidatorAddress: "v4", Timestamp: ts(1_700_000_200)}, // future - rejected
		{ValidatorAddress: "v5", Timestamp: nil},               // nil ts - rejected
		nil,                                                    // nil vote - skipped
	}
	valid := filterStaleVotes(votes, now, 300)
	require.Len(t, valid, 2)
	require.Equal(t, "v1", valid[0].ValidatorAddress)
	require.Equal(t, "v2", valid[1].ValidatorAddress)
}

func TestFilterStaleVotesZeroMaxAge(t *testing.T) {
	votes := []*types.ValidatorVote{{ValidatorAddress: "v1"}}
	valid := filterStaleVotes(votes, time.Now(), 0)
	require.Len(t, valid, 1, "maxAge<=0 returns all votes")
}

func TestGroupVotesByAsset(t *testing.T) {
	votes := []*types.ValidatorVote{
		{
			ValidatorAddress: "v1",
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: "1.5"},
				{AssetPair: "ETH/USD", Price: "3500"},
			},
		},
		{
			ValidatorAddress: "v2",
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: "1.6"},
			},
		},
		nil, // should be skipped
		{
			ValidatorAddress: "v3",
			PriceFeeds: []*types.PriceFeed{
				nil,                                // nil feed - skipped
				{AssetPair: "", Price: "1.0"},      // empty asset - skipped
				{AssetPair: "LAC/USD", Price: "x"}, // invalid price - skipped
			},
		},
	}
	grouped := groupVotesByAsset(votes)
	require.Len(t, grouped["LAC/USD"], 2)
	require.Len(t, grouped["ETH/USD"], 1)
}

func TestFilterOutliers(t *testing.T) {
	dec := func(s string) sdkmath.LegacyDec {
		d, _ := sdkmath.LegacyNewDecFromStr(s)
		return d
	}
	prices := []sdkmath.LegacyDec{dec("90"), dec("100"), dec("110"), dec("200")}
	median := dec("105")
	maxDev := dec("0.10") // 10%

	filtered := filterOutliers(prices, median, maxDev)
	// 90 → ~14% deviation (filtered), 100 → ~4.7% (ok), 110 → ~4.7% (ok), 200 → ~90% (filtered)
	require.Len(t, filtered, 2)
}

func TestFilterOutliersZeroMedian(t *testing.T) {
	dec := func(s string) sdkmath.LegacyDec {
		d, _ := sdkmath.LegacyNewDecFromStr(s)
		return d
	}
	prices := []sdkmath.LegacyDec{dec("0"), dec("1")}
	filtered := filterOutliers(prices, sdkmath.LegacyZeroDec(), dec("0.10"))
	require.Len(t, filtered, 2, "zero median returns all prices")
}

func TestFilterOutliersZeroMaxDeviation(t *testing.T) {
	dec := func(s string) sdkmath.LegacyDec {
		d, _ := sdkmath.LegacyNewDecFromStr(s)
		return d
	}
	prices := []sdkmath.LegacyDec{dec("100"), dec("200")}
	filtered := filterOutliers(prices, dec("150"), sdkmath.LegacyZeroDec())
	require.Len(t, filtered, 2, "zero max deviation keeps all")
}

func TestSqrtDec(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"zero", "0", "0"},
		{"one", "1", "1"},
		{"four", "4", "2"},
		{"nine", "9", "3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x, _ := sdkmath.LegacyNewDecFromStr(tc.input)
			result := sqrtDec(x)
			expected, _ := sdkmath.LegacyNewDecFromStr(tc.expect)
			diff := result.Sub(expected).Abs()
			require.True(t, diff.LT(sdkmath.LegacyNewDecWithPrec(1, 6)),
				"sqrtDec(%s) = %s, want ~%s", tc.input, result, tc.expect)
		})
	}
}

func TestSqrtDecNegative(t *testing.T) {
	x, _ := sdkmath.LegacyNewDecFromStr("-4")
	require.True(t, sqrtDec(x).IsZero())
}

// ---------------------------------------------------------------------------
// AggregateVotes end-to-end
// ---------------------------------------------------------------------------

func TestAggregateVotesHappyPath(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	now := ctx.BlockTime()
	ts := timestamppb.New(now)

	for i, price := range []string{"100", "102", "101"} {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val-" + string(rune('A'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        ts,
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	require.Equal(t, int32(3), agg.NumValidators)

	// Votes should be cleared after aggregation
	votes, err := k.GetAllValidatorVotes(ctx)
	require.NoError(t, err)
	require.Empty(t, votes)
}

func TestAggregateVotesNoVotes(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	// No votes submitted — should be no-op
	require.NoError(t, k.AggregateVotes(ctx))
}

func TestAggregateVotesAllStale(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	staleTime := timestamppb.New(ctx.BlockTime().Add(-10 * time.Minute))
	require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
		ValidatorAddress: "val-stale",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "100"}},
		BlockHeight:      1,
		Timestamp:        staleTime,
	}))

	require.NoError(t, k.AggregateVotes(ctx))

	// Votes should still be cleared
	votes, err := k.GetAllValidatorVotes(ctx)
	require.NoError(t, err)
	require.Empty(t, votes)
}

func TestAggregateVotesOutlierFiltering(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	p := types.DefaultParams()
	p.MaxPriceDeviation = "0.05" // 5% deviation max
	require.NoError(t, k.SetParams(ctx, p))

	now := ctx.BlockTime()
	ts := timestamppb.New(now)

	// 3 validators with similar prices, 1 outlier
	prices := []string{"100", "101", "102", "500"}
	for i, price := range prices {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val-" + string(rune('A'+i)),
			PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: price}},
			BlockHeight:      int64(i + 1),
			Timestamp:        ts,
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	agg, err := k.GetAggregatedPrice(ctx, "LAC/USD")
	require.NoError(t, err)
	// The outlier (500) should be filtered out, leaving 3 validators
	require.Equal(t, int32(3), agg.NumValidators)
}

func TestAggregateVotesMultipleAssets(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	now := ctx.BlockTime()
	ts := timestamppb.New(now)

	for i := 0; i < 3; i++ {
		require.NoError(t, k.SetValidatorVote(ctx, &types.ValidatorVote{
			ValidatorAddress: "val-" + string(rune('A'+i)),
			PriceFeeds: []*types.PriceFeed{
				{AssetPair: "LAC/USD", Price: "1.5"},
				{AssetPair: "ETH/USD", Price: "3500"},
			},
			BlockHeight: int64(i + 1),
			Timestamp:   ts,
		}))
	}

	require.NoError(t, k.AggregateVotes(ctx))

	for _, pair := range []string{"LAC/USD", "ETH/USD"} {
		agg, err := k.GetAggregatedPrice(ctx, pair)
		require.NoError(t, err)
		require.Equal(t, int32(3), agg.NumValidators)
	}
}

// ---------------------------------------------------------------------------
// ValidateVote
// ---------------------------------------------------------------------------

func TestValidateVoteHappyPath(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.5"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(ctx.BlockTime()),
	}
	require.NoError(t, k.ValidateVote(ctx, vote))
}

func TestValidateVoteNil(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.Error(t, k.ValidateVote(ctx, nil))
}

func TestValidateVoteNoTimestamp(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.5"}},
		BlockHeight:      1,
		Timestamp:        nil,
	}
	require.Error(t, k.ValidateVote(ctx, vote))
}

func TestValidateVoteFutureTimestamp(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.5"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(ctx.BlockTime().Add(10 * time.Minute)),
	}
	require.Error(t, k.ValidateVote(ctx, vote))
}

func TestValidateVoteStale(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "1.5"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(ctx.BlockTime().Add(-10 * time.Minute)),
	}
	require.Error(t, k.ValidateVote(ctx, vote))
}

func TestValidateVoteInvalidAssetPair(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "NOPE/NOPE", Price: "1.5"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(ctx.BlockTime()),
	}
	err := k.ValidateVote(ctx, vote)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in allowed list")
}

func TestValidateVoteInvalidPrice(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "LAC/USD", Price: "-1"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(ctx.BlockTime()),
	}
	err := k.ValidateVote(ctx, vote)
	require.Error(t, err)
}

func TestValidateVoteEmptyAssetPair(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{{AssetPair: "", Price: "1.5"}},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(ctx.BlockTime()),
	}
	require.Error(t, k.ValidateVote(ctx, vote))
}

func TestValidateVoteNilFeed(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	vote := &types.ValidatorVote{
		ValidatorAddress: "val1",
		PriceFeeds:       []*types.PriceFeed{nil},
		BlockHeight:      1,
		Timestamp:        timestamppb.New(ctx.BlockTime()),
	}
	require.Error(t, k.ValidateVote(ctx, vote))
}

// ---------------------------------------------------------------------------
// Query server
// ---------------------------------------------------------------------------

func TestQueryServerParams(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	qs := NewQueryServerImpl(*k)
	resp, err := qs.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.Params)
	require.Equal(t, int64(10), resp.Params.VotePeriod)
}

func TestQueryServerParamsNilReq(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.Params(ctx, nil)
	require.Error(t, err)
}

func TestQueryServerPriceFeed(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetPriceFeed(ctx, &types.PriceFeed{
		AssetPair: "LAC/USD",
		Price:     "1.5",
		Timestamp: timestamppb.Now(),
	}))

	qs := NewQueryServerImpl(*k)
	resp, err := qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{AssetPair: "LAC/USD"})
	require.NoError(t, err)
	require.Equal(t, "LAC/USD", resp.PriceFeed.AssetPair)
}

func TestQueryServerPriceFeedNilReq(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.PriceFeed(ctx, nil)
	require.Error(t, err)
}

func TestQueryServerPriceFeedEmptyAsset(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{AssetPair: ""})
	require.Error(t, err)
}

func TestQueryServerAllPriceFeeds(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	for _, pair := range []string{"LAC/USD", "ETH/USD"} {
		require.NoError(t, k.SetPriceFeed(ctx, &types.PriceFeed{
			AssetPair: pair, Price: "100", Timestamp: timestamppb.Now(),
		}))
	}
	qs := NewQueryServerImpl(*k)
	resp, err := qs.AllPriceFeeds(ctx, &types.QueryAllPriceFeedsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.PriceFeeds, 2)
}

func TestQueryServerAllPriceFeedsNilReq(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.AllPriceFeeds(ctx, nil)
	require.Error(t, err)
}

func TestQueryServerAggregatedPrice(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetAggregatedPrice(ctx, &types.AggregatedPrice{
		AssetPair:     "BTC/USD",
		MedianPrice:   "50000",
		NumValidators: 10,
		Timestamp:     timestamppb.Now(),
	}))

	qs := NewQueryServerImpl(*k)
	resp, err := qs.AggregatedPrice(ctx, &types.QueryAggregatedPriceRequest{AssetPair: "BTC/USD"})
	require.NoError(t, err)
	require.Equal(t, int32(10), resp.AggregatedPrice.NumValidators)
}

func TestQueryServerAggregatedPriceNilReq(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.AggregatedPrice(ctx, nil)
	require.Error(t, err)
}

func TestQueryServerAggregatedPriceEmptyAsset(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.AggregatedPrice(ctx, &types.QueryAggregatedPriceRequest{AssetPair: ""})
	require.Error(t, err)
}
