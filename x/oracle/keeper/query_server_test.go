//go:build cosmos
// +build cosmos

package keeper

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// --- Query: Params ---

func TestQueryParams_Success(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	qs := NewQueryServerImpl(*k)
	resp, err := qs.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Params)
	assert.Equal(t, int64(10), resp.Params.VotePeriod)
}

func TestQueryParams_NilRequest(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.Params(ctx, nil)
	require.Error(t, err)
}

// --- Query: PriceFeed ---

func TestQueryPriceFeed_Success(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetPriceFeed(ctx, &types.PriceFeed{
		AssetPair: "LAC/USD",
		Price:     "1.50",
		Timestamp: timestamppb.New(testTime),
	}))

	qs := NewQueryServerImpl(*k)
	resp, err := qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{AssetPair: "LAC/USD"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "LAC/USD", resp.PriceFeed.AssetPair)
}

func TestQueryPriceFeed_NilRequest(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.PriceFeed(ctx, nil)
	require.Error(t, err)
}

func TestQueryPriceFeed_EmptyAssetPair(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{AssetPair: ""})
	require.Error(t, err)
}

func TestQueryPriceFeed_NotFound(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{AssetPair: "UNKNOWN/PAIR"})
	require.Error(t, err)
}

// --- Query: AllPriceFeeds ---

func TestQueryAllPriceFeeds_Success(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetPriceFeed(ctx, &types.PriceFeed{AssetPair: "LAC/USD", Price: "1.50"}))
	require.NoError(t, k.SetPriceFeed(ctx, &types.PriceFeed{AssetPair: "ETH/USD", Price: "2000"}))

	qs := NewQueryServerImpl(*k)
	resp, err := qs.AllPriceFeeds(ctx, &types.QueryAllPriceFeedsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.PriceFeeds, 2)
}

func TestQueryAllPriceFeeds_NilRequest(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.AllPriceFeeds(ctx, nil)
	require.Error(t, err)
}

func TestQueryAllPriceFeeds_Empty(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	resp, err := qs.AllPriceFeeds(ctx, &types.QueryAllPriceFeedsRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.PriceFeeds)
}

func TestQueryAllPriceFeeds_CapsResponse(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	for i := 0; i < maxOracleQueryLimit+5; i++ {
		require.NoError(t, k.SetPriceFeed(ctx, &types.PriceFeed{
			AssetPair: fmt.Sprintf("ASSET%04d/USD", i),
			Price:     "1.50",
		}))
	}

	qs := NewQueryServerImpl(*k)
	resp, err := qs.AllPriceFeeds(ctx, &types.QueryAllPriceFeedsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.PriceFeeds, maxOracleQueryLimit)
}

func TestQueryAllPriceFeeds_ReturnsClonedFeeds(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetPriceFeed(ctx, &types.PriceFeed{AssetPair: "LAC/USD", Price: "1.50"}))

	qs := NewQueryServerImpl(*k)
	resp, err := qs.AllPriceFeeds(ctx, &types.QueryAllPriceFeedsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.PriceFeeds, 1)

	resp.PriceFeeds[0].Price = "999.0"
	resp.PriceFeeds[0].AssetPair = "MUTATED/PAIR"

	stored, err := qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{AssetPair: "LAC/USD"})
	require.NoError(t, err)
	require.Equal(t, "LAC/USD", stored.PriceFeed.AssetPair)
	require.NotEqual(t, "999.0", stored.PriceFeed.Price)
}

// --- Query: AggregatedPrice ---

func TestQueryAggregatedPrice_Success(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	require.NoError(t, k.SetAggregatedPrice(ctx, &types.AggregatedPrice{
		AssetPair:     "LAC/USD",
		MedianPrice:   "1.50",
		NumValidators: 3,
	}))

	qs := NewQueryServerImpl(*k)
	resp, err := qs.AggregatedPrice(ctx, &types.QueryAggregatedPriceRequest{AssetPair: "LAC/USD"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "LAC/USD", resp.AggregatedPrice.AssetPair)
	assert.Equal(t, int32(3), resp.AggregatedPrice.NumValidators)
}

func TestQueryAggregatedPrice_NilRequest(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.AggregatedPrice(ctx, nil)
	require.Error(t, err)
}

func TestQueryAggregatedPrice_EmptyAssetPair(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.AggregatedPrice(ctx, &types.QueryAggregatedPriceRequest{AssetPair: ""})
	require.Error(t, err)
}

func TestQueryAggregatedPrice_NotFound(t *testing.T) {
	ctx, k := setupOracleKeeper(t)
	qs := NewQueryServerImpl(*k)
	_, err := qs.AggregatedPrice(ctx, &types.QueryAggregatedPriceRequest{AssetPair: "UNKNOWN/PAIR"})
	require.Error(t, err)
}

func TestQueryServer_RejectsInvalidRequestsBeforeZeroKeeper(t *testing.T) {
	qs := NewQueryServerImpl(Keeper{})
	ctx := context.Background()

	_, err := qs.Params(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil request")
	require.NotContains(t, err.Error(), "keeper not initialized")

	_, err = qs.PriceFeed(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil request")
	require.NotContains(t, err.Error(), "keeper not initialized")

	_, err = qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset pair cannot be empty")
	require.NotContains(t, err.Error(), "keeper not initialized")

	_, err = qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{AssetPair: " LAC/USD"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset pair must not have leading or trailing whitespace")
	require.NotContains(t, err.Error(), "keeper not initialized")

	_, err = qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{AssetPair: strings.Repeat("A", types.MaxAssetPairLen+1)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset pair exceeds 64-byte cap")
	require.NotContains(t, err.Error(), "keeper not initialized")

	_, err = qs.AllPriceFeeds(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil request")
	require.NotContains(t, err.Error(), "keeper not initialized")

	_, err = qs.AggregatedPrice(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil request")
	require.NotContains(t, err.Error(), "keeper not initialized")

	_, err = qs.AggregatedPrice(ctx, &types.QueryAggregatedPriceRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset pair cannot be empty")
	require.NotContains(t, err.Error(), "keeper not initialized")

	_, err = qs.AggregatedPrice(ctx, &types.QueryAggregatedPriceRequest{AssetPair: "LAC/USD "})
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset pair must not have leading or trailing whitespace")
	require.NotContains(t, err.Error(), "keeper not initialized")

	_, err = qs.AggregatedPrice(ctx, &types.QueryAggregatedPriceRequest{AssetPair: strings.Repeat("B", types.MaxAssetPairLen+1)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset pair exceeds 64-byte cap")
	require.NotContains(t, err.Error(), "keeper not initialized")
}

func TestQueryServer_RejectsZeroKeeperBeforeStateAccess(t *testing.T) {
	qs := NewQueryServerImpl(Keeper{})
	ctx := context.Background()

	testCases := []struct {
		name  string
		query func() error
	}{
		{
			name: "params",
			query: func() error {
				_, err := qs.Params(ctx, &types.QueryParamsRequest{})
				return err
			},
		},
		{
			name: "price feed",
			query: func() error {
				_, err := qs.PriceFeed(ctx, &types.QueryPriceFeedRequest{AssetPair: "LAC/USD"})
				return err
			},
		},
		{
			name: "all price feeds",
			query: func() error {
				_, err := qs.AllPriceFeeds(ctx, &types.QueryAllPriceFeedsRequest{})
				return err
			},
		},
		{
			name: "aggregated price",
			query: func() error {
				_, err := qs.AggregatedPrice(ctx, &types.QueryAggregatedPriceRequest{AssetPair: "LAC/USD"})
				return err
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.query()
			require.Error(t, err)
			require.Contains(t, err.Error(), "oracle keeper not initialized")
		})
	}
}
