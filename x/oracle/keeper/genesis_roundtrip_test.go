//go:build cosmos

package keeper

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/cosmos/gogoproto/proto"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// initGenesis mimics module.AppModule.InitGenesis for keeper-level testing.
func initGenesis(ctx context.Context, k *Keeper, genesis *types.GenesisState) {
	params := genesis.Params
	if params == nil {
		params = types.DefaultParams()
	}
	if err := k.SetParams(ctx, params); err != nil {
		panic(err)
	}
	for _, feed := range genesis.PriceFeeds {
		if feed == nil {
			continue
		}
		if err := k.SetPriceFeed(ctx, feed); err != nil {
			panic(err)
		}
	}
	for _, agg := range genesis.AggregatedPrices {
		if agg == nil {
			continue
		}
		if err := k.SetAggregatedPrice(ctx, agg); err != nil {
			panic(err)
		}
	}
}

// exportGenesis mimics module.AppModule.ExportGenesis for keeper-level testing.
func exportGenesis(ctx sdk.Context, k *Keeper) *types.GenesisState {
	params, err := k.GetParams(ctx)
	if err != nil {
		panic(err)
	}
	feeds, err := k.GetAllPriceFeeds(ctx)
	if err != nil {
		panic(err)
	}
	aggs, err := k.GetAllAggregatedPrices(ctx)
	if err != nil {
		panic(err)
	}
	return &types.GenesisState{
		Params:           params,
		PriceFeeds:       feeds,
		AggregatedPrices: aggs,
	}
}

// TestGenesisRoundtrip_Oracle verifies that genesis data survives
// InitGenesis -> ExportGenesis without data loss or corruption.
func TestGenesisRoundtrip_Oracle(t *testing.T) {
	t.Parallel()
	ctx, k := setupOracleKeeper(t)

	ts := time.Unix(1700000000, 0).UTC()
	original := &types.GenesisState{
		Params: &types.Params{
			VotePeriod:        15,
			VoteThreshold:     "0.75",
			MaxPriceDeviation: "0.10",
			AssetPairs:        []string{"LAC/USD", "ETH/USD"},
			MaxVoteAge:        300,
		},
		PriceFeeds: []*types.PriceFeed{
			{
				AssetPair:       "LAC/USD",
				Price:           "1.2345",
				Volume_24H:      "5000000",
				Timestamp:       ts,
				Sources:         []string{"binance", "coinbase"},
				ConfidenceScore: "0.95",
			},
			{
				AssetPair:       "ETH/USD",
				Price:           "2500.00",
				Volume_24H:      "12000000",
				Timestamp:       ts,
				Sources:         []string{"kraken"},
				ConfidenceScore: "0.88",
			},
		},
		AggregatedPrices: []*types.AggregatedPrice{
			{
				AssetPair:         "LAC/USD",
				MedianPrice:       "1.2340",
				MeanPrice:         "1.2345",
				StandardDeviation: "0.0005",
				NumValidators:     10,
				BlockHeight:       100,
				Timestamp:         ts,
			},
		},
	}

	initGenesis(ctx, k, original)
	exported := exportGenesis(ctx, k)

	// Verify params roundtrip
	require.Equal(t, original.Params.VotePeriod, exported.Params.VotePeriod,
		"VotePeriod mismatch")
	require.Equal(t, original.Params.VoteThreshold, exported.Params.VoteThreshold,
		"VoteThreshold mismatch")

	// Verify price feeds roundtrip
	require.Len(t, exported.PriceFeeds, len(original.PriceFeeds),
		"PriceFeed count mismatch")
	for i, origFeed := range original.PriceFeeds {
		expFeed := findPriceFeedByPair(exported.PriceFeeds, origFeed.AssetPair)
		require.NotNil(t, expFeed, "missing PriceFeed for %s", origFeed.AssetPair)
		require.Equal(t, origFeed.Price, expFeed.Price,
			"PriceFeed[%d] price mismatch", i)
		require.Equal(t, origFeed.Volume_24H, expFeed.Volume_24H,
			"PriceFeed[%d] volume mismatch", i)
		require.Equal(t, origFeed.ConfidenceScore, expFeed.ConfidenceScore,
			"PriceFeed[%d] confidence mismatch", i)
		require.ElementsMatch(t, origFeed.Sources, expFeed.Sources,
			"PriceFeed[%d] sources mismatch", i)
	}

	// Verify aggregated prices roundtrip
	require.Len(t, exported.AggregatedPrices, len(original.AggregatedPrices),
		"AggregatedPrice count mismatch")
	for i, origAgg := range original.AggregatedPrices {
		expAgg := findAggregatedByPair(exported.AggregatedPrices, origAgg.AssetPair)
		require.NotNil(t, expAgg, "missing AggregatedPrice for %s", origAgg.AssetPair)
		require.Equal(t, origAgg.MedianPrice, expAgg.MedianPrice,
			"AggregatedPrice[%d] median mismatch", i)
		require.Equal(t, origAgg.MeanPrice, expAgg.MeanPrice,
			"AggregatedPrice[%d] mean mismatch", i)
		require.Equal(t, origAgg.StandardDeviation, expAgg.StandardDeviation,
			"AggregatedPrice[%d] stddev mismatch", i)
		require.Equal(t, origAgg.NumValidators, expAgg.NumValidators,
			"AggregatedPrice[%d] num_validators mismatch", i)
		require.Equal(t, origAgg.BlockHeight, expAgg.BlockHeight,
			"AggregatedPrice[%d] block_height mismatch", i)
	}
}

// TestGenesisRoundtrip_Oracle_NilHandling verifies nil-safety.
func TestGenesisRoundtrip_Oracle_NilHandling(t *testing.T) {
	t.Parallel()
	ctx, k := setupOracleKeeper(t)

	genesis := &types.GenesisState{
		Params:           nil, // should default
		PriceFeeds:       []*types.PriceFeed{nil}, // nil entries skipped
		AggregatedPrices: []*types.AggregatedPrice{nil},
	}

	require.NotPanics(t, func() {
		initGenesis(ctx, k, genesis)
	})

	exported := exportGenesis(ctx, k)
	require.NotNil(t, exported.Params, "params should be defaulted")
}

// TestGenesisRoundtrip_Oracle_EmptyGenesis verifies empty genesis handling.
func TestGenesisRoundtrip_Oracle_EmptyGenesis(t *testing.T) {
	t.Parallel()
	ctx, k := setupOracleKeeper(t)

	require.NotPanics(t, func() {
		initGenesis(ctx, k, &types.GenesisState{})
	})

	exported := exportGenesis(ctx, k)
	require.NotNil(t, exported.Params)
	require.Empty(t, exported.PriceFeeds)
	require.Empty(t, exported.AggregatedPrices)
}

// TestProto3Determinism_OracleGenesis verifies proto3 deterministic encoding.
// Same input -> same bytes across multiple marshal calls.
func TestProto3Determinism_OracleGenesis(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1700000000, 0).UTC()
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		PriceFeeds: []*types.PriceFeed{
			{
				AssetPair:       "LAC/USD",
				Price:           "1.2345",
				Volume_24H:      "5000000",
				Timestamp:       ts,
				Sources:         []string{"binance", "coinbase"},
				ConfidenceScore: "0.95",
			},
		},
		AggregatedPrices: []*types.AggregatedPrice{
			{
				AssetPair:         "LAC/USD",
				MedianPrice:       "1.2340",
				MeanPrice:         "1.2345",
				StandardDeviation: "0.0005",
				NumValidators:     10,
				BlockHeight:       100,
				Timestamp:         ts,
			},
		},
	}

	bytes1, err := proto.Marshal(genesis)
	require.NoError(t, err)

	bytes2, err := proto.Marshal(genesis)
	require.NoError(t, err)

	require.Equal(t, bytes1, bytes2,
		"proto3 deterministic encoding violated: same input produced different bytes")

	// Additional: verify across fresh identical struct
	genesis2 := &types.GenesisState{
		Params: types.DefaultParams(),
		PriceFeeds: []*types.PriceFeed{
			{
				AssetPair:       "LAC/USD",
				Price:           "1.2345",
				Volume_24H:      "5000000",
				Timestamp:       time.Unix(1700000000, 0).UTC(),
				Sources:         []string{"binance", "coinbase"},
				ConfidenceScore: "0.95",
			},
		},
		AggregatedPrices: []*types.AggregatedPrice{
			{
				AssetPair:         "LAC/USD",
				MedianPrice:       "1.2340",
				MeanPrice:         "1.2345",
				StandardDeviation: "0.0005",
				NumValidators:     10,
				BlockHeight:       100,
				Timestamp:         time.Unix(1700000000, 0).UTC(),
			},
		},
	}

	bytes3, err := proto.Marshal(genesis2)
	require.NoError(t, err)

	require.Equal(t, bytes1, bytes3,
		"proto3 deterministic encoding violated: equivalent structs produced different bytes")
}

// TestProto3Determinism_PriceFeed verifies proto3 determinism for PriceFeed.
func TestProto3Determinism_PriceFeed(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1700000000, 0).UTC()
	feed := &types.PriceFeed{
		AssetPair:       "ETH/USD",
		Price:           "2500.00",
		Volume_24H:      "12000000",
		Timestamp:       ts,
		Sources:         []string{"binance", "coinbase", "kraken"},
		ConfidenceScore: "0.92",
	}

	bytes1, err := proto.Marshal(feed)
	require.NoError(t, err)

	bytes2, err := proto.Marshal(feed)
	require.NoError(t, err)

	require.Equal(t, bytes1, bytes2,
		"proto3 deterministic encoding violated for PriceFeed")
}

// TestProto3Determinism_AggregatedPrice verifies proto3 determinism for AggregatedPrice.
func TestProto3Determinism_AggregatedPrice(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1700000000, 0).UTC()
	agg := &types.AggregatedPrice{
		AssetPair:         "LAC/USD",
		MedianPrice:       "1.2340",
		MeanPrice:         "1.2345",
		StandardDeviation: "0.0005",
		NumValidators:     10,
		BlockHeight:       100,
		Timestamp:         ts,
	}

	bytes1, err := proto.Marshal(agg)
	require.NoError(t, err)

	bytes2, err := proto.Marshal(agg)
	require.NoError(t, err)

	require.Equal(t, bytes1, bytes2,
		"proto3 deterministic encoding violated for AggregatedPrice")
}

func findPriceFeedByPair(feeds []*types.PriceFeed, pair string) *types.PriceFeed {
	for _, f := range feeds {
		if f != nil && f.AssetPair == pair {
			return f
		}
	}
	return nil
}

func findAggregatedByPair(prices []*types.AggregatedPrice, pair string) *types.AggregatedPrice {
	for _, p := range prices {
		if p != nil && p.AssetPair == pair {
			return p
		}
	}
	return nil
}
