//go:build cosmos

package oracle

import (
	"strings"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/oracle/keeper"
	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

func setupOracleModuleTest(t *testing.T) (sdk.Context, AppModule, codec.Codec) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()

	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)
	authority := authtypes.NewModuleAddress("gov").String()

	k := keeper.NewKeeper(cdc, runtime.NewKVStoreService(storeKey), authority)

	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)

	return ctx, NewAppModule(k), cdc
}

func TestGenesisRoundTrip(t *testing.T) {
	ctx, am, cdc := setupOracleModuleTest(t)
	now := ctx.BlockTime()

	params := types.DefaultParams()
	params.VotePeriod = 30

	feed := &types.PriceFeed{
		AssetPair:       "LAC/USD",
		Price:           "1.23",
		Volume_24H:      "1000000",
		Timestamp:       now,
		Sources:         []string{"binance", "coinbase"},
		ConfidenceScore: "0.97",
	}
	feed2 := &types.PriceFeed{
		AssetPair:       "ATOM/USD",
		Price:           "9.87",
		Volume_24H:      "500000",
		Timestamp:       now.Add(-time.Minute),
		Sources:         []string{"kraken"},
		ConfidenceScore: "0.93",
	}

	aggregated := &types.AggregatedPrice{
		AssetPair:         "LAC/USD",
		MedianPrice:       "1.22",
		MeanPrice:         "1.23",
		StandardDeviation: "0.01",
		NumValidators:     7,
		BlockHeight:       ctx.BlockHeight(),
		Timestamp:         now,
	}
	aggregated2 := &types.AggregatedPrice{
		AssetPair:         "ATOM/USD",
		MedianPrice:       "9.86",
		MeanPrice:         "9.87",
		StandardDeviation: "0.02",
		NumValidators:     6,
		BlockHeight:       ctx.BlockHeight(),
		Timestamp:         now,
	}

	genesis := &types.GenesisState{
		Params:           params,
		PriceFeeds:       []*types.PriceFeed{feed, feed2},
		AggregatedPrices: []*types.AggregatedPrice{aggregated, aggregated2},
	}

	bz := cdc.MustMarshalJSON(genesis)
	am.InitGenesis(ctx, cdc, bz)

	exported := am.ExportGenesis(ctx, cdc)
	var got types.GenesisState
	cdc.MustUnmarshalJSON(exported, &got)

	require.True(t, proto.Equal(params, got.Params))
	require.Len(t, got.PriceFeeds, 2)
	expectedFeeds := map[string]*types.PriceFeed{
		feed.AssetPair:  normalizeFeed(t, feed),
		feed2.AssetPair: normalizeFeed(t, feed2),
	}
	for _, gotFeed := range got.PriceFeeds {
		require.Contains(t, expectedFeeds, gotFeed.AssetPair)
		require.True(t, proto.Equal(expectedFeeds[gotFeed.AssetPair], gotFeed))
	}

	require.Len(t, got.AggregatedPrices, 2)
	expectedAgg := map[string]*types.AggregatedPrice{
		aggregated.AssetPair:  normalizeAggregate(t, aggregated),
		aggregated2.AssetPair: normalizeAggregate(t, aggregated2),
	}
	for _, gotAgg := range got.AggregatedPrices {
		require.Contains(t, expectedAgg, gotAgg.AssetPair)
		require.True(t, proto.Equal(expectedAgg[gotAgg.AssetPair], gotAgg))
	}
}

func TestGenesisEmptyUsesDefault(t *testing.T) {
	ctx, am, cdc := setupOracleModuleTest(t)

	am.InitGenesis(ctx, cdc, nil)

	exported := am.ExportGenesis(ctx, cdc)
	var got types.GenesisState
	cdc.MustUnmarshalJSON(exported, &got)

	require.True(t, proto.Equal(types.DefaultParams(), got.Params))
	require.Empty(t, got.PriceFeeds)
	require.Empty(t, got.AggregatedPrices)
}

func TestValidateGenesisRejectsDuplicateFeedRows(t *testing.T) {
	_, _, cdc := setupOracleModuleTest(t)

	tests := []struct {
		name    string
		genesis *types.GenesisState
		want    string
	}{
		{
			name: "price feeds",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{
					{AssetPair: "LAC/USD", Price: "1.00"},
					{AssetPair: "LAC/USD", Price: "1.01"},
				},
			},
			want: `duplicate price_feed asset pair "LAC/USD"`,
		},
		{
			name: "aggregated prices",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{AssetPair: "ETH/USD", MedianPrice: "2500.00"},
					{AssetPair: "ETH/USD", MedianPrice: "2501.00"},
				},
			},
			want: `duplicate aggregated_price asset pair "ETH/USD"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bz := cdc.MustMarshalJSON(tt.genesis)
			err := AppModuleBasic{}.ValidateGenesis(cdc, nil, bz)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidateGenesisRejectsUncanonicalAssetPairs(t *testing.T) {
	_, _, cdc := setupOracleModuleTest(t)

	tests := []struct {
		name    string
		genesis *types.GenesisState
		want    string
	}{
		{
			name: "price feed leading whitespace",
			genesis: &types.GenesisState{
				Params:     types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{{AssetPair: " LAC/USD", Price: "1.00"}},
			},
			want: "price_feed[0] asset pair must not have leading or trailing whitespace",
		},
		{
			name: "price feed trailing whitespace",
			genesis: &types.GenesisState{
				Params:     types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{{AssetPair: "LAC/USD\t", Price: "1.00"}},
			},
			want: "price_feed[0] asset pair must not have leading or trailing whitespace",
		},
		{
			name: "aggregated price leading whitespace",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{AssetPair: "\nETH/USD", MedianPrice: "2500.00"},
				},
			},
			want: "aggregated_price[0] asset pair must not have leading or trailing whitespace",
		},
		{
			name: "aggregated price trailing whitespace",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{AssetPair: "ETH/USD ", MedianPrice: "2500.00"},
				},
			},
			want: "aggregated_price[0] asset pair must not have leading or trailing whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bz := cdc.MustMarshalJSON(tt.genesis)
			err := AppModuleBasic{}.ValidateGenesis(cdc, nil, bz)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidateGenesisRejectsUncanonicalDecimalStrings(t *testing.T) {
	_, _, cdc := setupOracleModuleTest(t)

	tests := []struct {
		name    string
		genesis *types.GenesisState
		want    string
	}{
		{
			name: "price feed price leading whitespace",
			genesis: &types.GenesisState{
				Params:     types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{{AssetPair: "LAC/USD", Price: " 1.00"}},
			},
			want: "price_feed[0] price must not have leading or trailing whitespace",
		},
		{
			name: "price feed volume trailing whitespace",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{
					{AssetPair: "LAC/USD", Price: "1.00", Volume_24H: "1000\t"},
				},
			},
			want: "price_feed[0] volume_24h must not have leading or trailing whitespace",
		},
		{
			name: "price feed confidence leading whitespace",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{
					{AssetPair: "LAC/USD", Price: "1.00", ConfidenceScore: "\n0.97"},
				},
			},
			want: "price_feed[0] confidence_score must not have leading or trailing whitespace",
		},
		{
			name: "aggregated median leading whitespace",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{AssetPair: "BTC/USD", MedianPrice: " 42000.00"},
				},
			},
			want: "aggregated_price[0] median price must not have leading or trailing whitespace",
		},
		{
			name: "aggregated mean trailing whitespace",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{AssetPair: "BTC/USD", MedianPrice: "42000.00", MeanPrice: "42001.00 "},
				},
			},
			want: "aggregated_price[0] mean price must not have leading or trailing whitespace",
		},
		{
			name: "aggregated standard deviation leading whitespace",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{AssetPair: "BTC/USD", MedianPrice: "42000.00", StandardDeviation: "\t0.01"},
				},
			},
			want: "aggregated_price[0] standard deviation must not have leading or trailing whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bz := cdc.MustMarshalJSON(tt.genesis)
			err := AppModuleBasic{}.ValidateGenesis(cdc, nil, bz)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidateGenesisRejectsMalformedFeedRows(t *testing.T) {
	_, _, cdc := setupOracleModuleTest(t)

	tests := []struct {
		name    string
		genesis *types.GenesisState
		want    string
	}{
		{
			name: "empty price feed",
			genesis: &types.GenesisState{
				Params:     types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{{}},
			},
			want: "price_feed[0] asset pair cannot be empty",
		},
		{
			name: "negative price feed volume",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{
					{AssetPair: "LAC/USD", Price: "1", Volume_24H: "-0.1"},
				},
			},
			want: "price_feed[0] volume_24h cannot be negative",
		},
		{
			name: "invalid price feed volume",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{
					{AssetPair: "LAC/USD", Price: "1", Volume_24H: "not-a-decimal"},
				},
			},
			want: "invalid price_feed[0] volume_24h",
		},
		{
			name: "negative price feed confidence",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{
					{AssetPair: "LAC/USD", Price: "1", ConfidenceScore: "-0.01"},
				},
			},
			want: "price_feed[0] confidence_score must be between 0 and 1",
		},
		{
			name: "price feed confidence above one",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{
					{AssetPair: "LAC/USD", Price: "1", ConfidenceScore: "1.01"},
				},
			},
			want: "price_feed[0] confidence_score must be between 0 and 1",
		},
		{
			name: "invalid price feed confidence",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{
					{AssetPair: "LAC/USD", Price: "1", ConfidenceScore: "not-a-decimal"},
				},
			},
			want: "invalid price_feed[0] confidence_score",
		},
		{
			name: "invalid aggregated price",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{AssetPair: "BTC/USD", MedianPrice: "0"},
				},
			},
			want: "aggregated_price[0] median price must be positive",
		},
		{
			name: "invalid aggregated mean price",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{
						AssetPair:   "BTC/USD",
						MedianPrice: "1",
						MeanPrice:   "-0.1",
					},
				},
			},
			want: "aggregated_price[0] mean price must be positive",
		},
		{
			name: "invalid aggregated standard deviation",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{
						AssetPair:         "BTC/USD",
						MedianPrice:       "1",
						StandardDeviation: "-0.1",
					},
				},
			},
			want: "aggregated_price[0] standard deviation cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bz := cdc.MustMarshalJSON(tt.genesis)
			err := AppModuleBasic{}.ValidateGenesis(cdc, nil, bz)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestGenesisValidatorRejectsInvalidTimestamps(t *testing.T) {
	tests := []struct {
		name    string
		genesis *types.GenesisState
		want    string
	}{
		{
			name: "price feed timestamp",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				PriceFeeds: []*types.PriceFeed{
					{
						AssetPair: "BTC/USD",
						Price:     "1",
						// year 10000 (> 9999) — out of the valid range
						// validateGenesisTimestamp rejects. Under gogoproto the
						// field is a time.Time value; the only "invalid" state it
						// can represent is an out-of-range year.
						Timestamp: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
			want: "price_feed[0] timestamp is invalid",
		},
		{
			name: "aggregated timestamp",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				AggregatedPrices: []*types.AggregatedPrice{
					{
						AssetPair:   "BTC/USD",
						MedianPrice: "1",
						// year 0 (< 1) — out of the valid range
						// validateGenesisTimestamp rejects. The upstream
						// *timestamppb.Timestamp{Nanos: 1e9} malformation has no
						// gogoproto time.Time equivalent, so we exercise the
						// other out-of-range boundary instead.
						Timestamp: time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
			want: "aggregated_price[0] timestamp is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGenesisState(tt.genesis)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestGenesisValidatorRejectsNilFeedPointers(t *testing.T) {
	err := validateGenesisState(&types.GenesisState{
		Params:     types.DefaultParams(),
		PriceFeeds: []*types.PriceFeed{nil},
	})
	require.ErrorContains(t, err, "price_feed[0] cannot be nil")

	err = validateGenesisState(&types.GenesisState{
		Params:           types.DefaultParams(),
		AggregatedPrices: []*types.AggregatedPrice{nil},
	})
	require.ErrorContains(t, err, "aggregated_price[0] cannot be nil")
}

func TestInitGenesisRejectsInvalidFeedRows(t *testing.T) {
	ctx, am, cdc := setupOracleModuleTest(t)
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		PriceFeeds: []*types.PriceFeed{
			{AssetPair: " LAC/USD ", Price: "1.00"},
		},
	}

	bz := cdc.MustMarshalJSON(genesis)
	requirePanicErrorContains(t, "invalid oracle genesis: price_feed[0] asset pair must not have leading or trailing whitespace", func() {
		am.InitGenesis(ctx, cdc, bz)
	})
}

func normalizeFeed(t *testing.T, feed *types.PriceFeed) *types.PriceFeed {
	t.Helper()
	clone := proto.Clone(feed).(*types.PriceFeed)
	clone.Price = normalizeDecString(t, clone.Price)
	return clone
}

func normalizeAggregate(t *testing.T, agg *types.AggregatedPrice) *types.AggregatedPrice {
	t.Helper()
	clone := proto.Clone(agg).(*types.AggregatedPrice)
	clone.MedianPrice = normalizeDecString(t, clone.MedianPrice)
	if strings.TrimSpace(clone.MeanPrice) != "" {
		clone.MeanPrice = normalizeDecString(t, clone.MeanPrice)
	}
	if strings.TrimSpace(clone.StandardDeviation) != "" {
		clone.StandardDeviation = normalizeDecString(t, clone.StandardDeviation)
	}
	return clone
}

func normalizeDecString(t *testing.T, value string) string {
	t.Helper()
	dec, err := sdkmath.LegacyNewDecFromStr(strings.TrimSpace(value))
	require.NoError(t, err)
	return dec.String()
}

func requirePanicErrorContains(t *testing.T, contains string, f func()) {
	t.Helper()

	defer func() {
		panicValue := recover()
		require.NotNil(t, panicValue)
		err, ok := panicValue.(error)
		require.Truef(t, ok, "panic value is %T, not error", panicValue)
		require.ErrorContains(t, err, contains)
	}()

	f()
}
