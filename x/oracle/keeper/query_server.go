
package keeper

import (
	"context"
	"strings"

	"github.com/cosmos/gogoproto/proto"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// queryServer implements the oracle Query service
type queryServer struct {
	types.UnimplementedQueryServer
	keeper Keeper
}

// NewQueryServerImpl creates a new Query server backed by the keeper
func NewQueryServerImpl(k Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

func (q queryServer) requireKeeper() (*Keeper, error) {
	if q.keeper.storeKey == nil {
		return nil, types.ErrInternalError.Wrap("oracle keeper not initialized")
	}
	return &q.keeper, nil
}

// maxOracleQueryLimit caps unpaginated list responses to keep public queries bounded.
const maxOracleQueryLimit = 1000

func validateOracleQueryAssetPair(assetPair string) error {
	trimmed := strings.TrimSpace(assetPair)
	if trimmed == "" {
		return types.ErrInvalidAssetPair.Wrap("asset pair cannot be empty")
	}
	if trimmed != assetPair {
		return types.ErrInvalidAssetPair.Wrap("asset pair must not have leading or trailing whitespace")
	}
	if len(assetPair) > types.MaxAssetPairLen {
		return types.ErrInvalidAssetPair.Wrapf("asset pair exceeds %d-byte cap (got %d)", types.MaxAssetPairLen, len(assetPair))
	}
	return nil
}

// Params returns the oracle module parameters
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, types.ErrInternalError.Wrap("nil request")
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	params, err := keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	clonedParams, ok := proto.Clone(params).(*types.Params)
	if !ok {
		return nil, types.ErrInternalError.Wrap("failed to clone params")
	}

	return &types.QueryParamsResponse{
		Params: clonedParams,
	}, nil
}

// PriceFeed returns a specific price feed by asset pair
func (q queryServer) PriceFeed(ctx context.Context, req *types.QueryPriceFeedRequest) (*types.QueryPriceFeedResponse, error) {
	if req == nil {
		return nil, types.ErrInternalError.Wrap("nil request")
	}

	if err := validateOracleQueryAssetPair(req.AssetPair); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	feed, err := keeper.GetPriceFeed(ctx, req.AssetPair)
	if err != nil {
		return nil, err
	}

	clonedFeed, ok := proto.Clone(feed).(*types.PriceFeed)
	if !ok {
		return nil, types.ErrInternalError.Wrap("failed to clone price feed")
	}

	return &types.QueryPriceFeedResponse{
		PriceFeed: clonedFeed,
	}, nil
}

// AllPriceFeeds returns all price feeds
func (q queryServer) AllPriceFeeds(ctx context.Context, req *types.QueryAllPriceFeedsRequest) (*types.QueryAllPriceFeedsResponse, error) {
	if req == nil {
		return nil, types.ErrInternalError.Wrap("nil request")
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	respFeeds := make([]*types.PriceFeed, 0, maxOracleQueryLimit)
	count := 0
	err = keeper.priceFeeds.Walk(ctx, nil, func(_ string, feed *types.PriceFeed) (bool, error) {
		if feed == nil {
			return count >= maxOracleQueryLimit, nil
		}
		clonedFeed, ok := proto.Clone(feed).(*types.PriceFeed)
		if !ok {
			return false, types.ErrInternalError.Wrap("failed to clone price feed")
		}
		respFeeds = append(respFeeds, clonedFeed)
		count++
		return count >= maxOracleQueryLimit, nil
	})
	if err != nil {
		return nil, err
	}

	return &types.QueryAllPriceFeedsResponse{
		PriceFeeds: respFeeds,
	}, nil
}

// AggregatedPrice returns the aggregated price for an asset pair
func (q queryServer) AggregatedPrice(ctx context.Context, req *types.QueryAggregatedPriceRequest) (*types.QueryAggregatedPriceResponse, error) {
	if req == nil {
		return nil, types.ErrInternalError.Wrap("nil request")
	}

	if err := validateOracleQueryAssetPair(req.AssetPair); err != nil {
		return nil, err
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	price, err := keeper.GetAggregatedPrice(ctx, req.AssetPair)
	if err != nil {
		return nil, err
	}

	clonedPrice, ok := proto.Clone(price).(*types.AggregatedPrice)
	if !ok {
		return nil, types.ErrInternalError.Wrap("failed to clone aggregated price")
	}

	return &types.QueryAggregatedPriceResponse{
		AggregatedPrice: clonedPrice,
	}, nil
}
