
package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// Keeper maintains the oracle module state with Collections API
type Keeper struct {
	cdc        codec.BinaryCodec
	storeKey   store.KVStoreService
	authority  string // Address with authority to update params (usually gov module)
	bankKeeper types.BankKeeper

	schema collections.Schema

	// Collections-based state (pointer semantics via collPtrValue)
	params           collections.Item[*types.Params]
	priceFeeds       collections.Map[string, *types.PriceFeed]       // asset_pair -> PriceFeed
	aggregatedPrices collections.Map[string, *types.AggregatedPrice] // asset_pair -> AggregatedPrice
	validatorVotes   collections.Map[string, *types.ValidatorVote]   // validator_address -> ValidatorVote
	lastVoteHeights  collections.Map[string, int64]                  // validator_address -> last vote height
	rewardAddresses  collections.Map[string, string]                 // validator_address -> reward address
}

// NewKeeper creates a new oracle keeper
func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey store.KVStoreService,
	authority string,
) *Keeper {
	sb := collections.NewSchemaBuilder(storeKey)

	k := &Keeper{
		cdc:       cdc,
		storeKey:  storeKey,
		authority: authority,
		params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			collPtrValue[types.Params](cdc),
		),
		priceFeeds: collections.NewMap(
			sb,
			types.PriceFeedPrefix,
			"price_feeds",
			collections.StringKey,
			collPtrValue[types.PriceFeed](cdc),
		),
		aggregatedPrices: collections.NewMap(
			sb,
			types.AggregatedPricePrefix,
			"aggregated_prices",
			collections.StringKey,
			collPtrValue[types.AggregatedPrice](cdc),
		),
		validatorVotes: collections.NewMap(
			sb,
			types.ValidatorVotePrefix,
			"validator_votes",
			collections.StringKey,
			collPtrValue[types.ValidatorVote](cdc),
		),
		lastVoteHeights: collections.NewMap(
			sb,
			types.VoteHistoryPrefix,
			"validator_vote_heights",
			collections.StringKey,
			collections.Int64Value,
		),
		rewardAddresses: collections.NewMap(
			sb,
			types.RewardAddressPrefix,
			"oracle_reward_addresses",
			collections.StringKey,
			collections.StringValue,
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build oracle collections schema: %w", err))
	}
	k.schema = schema

	return k
}

// Logger returns a module-specific logger
func (k Keeper) Logger(ctx context.Context) log.Logger {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

func defaultParams() *types.Params {
	return &types.Params{
		VotePeriod:        10,
		VoteThreshold:     "0.67",
		MaxPriceDeviation: "0.10",
		AssetPairs:        []string{"LAC/USD", "ETH/USD", "BTC/USD"},
		MaxVoteAge:        300,
	}
}

// GetAuthority returns the module's authority address
func (k Keeper) GetAuthority() string {
	return k.authority
}

// SetBankKeeper wires the bank keeper dependency for reward distribution.
func (k *Keeper) SetBankKeeper(bk types.BankKeeper) {
	k.bankKeeper = bk
}

func (k Keeper) setRewardAddress(ctx context.Context, validatorAddr, rewardAddr string) error {
	trimmedValidatorAddr := strings.TrimSpace(validatorAddr)
	if trimmedValidatorAddr == "" {
		return types.ErrInvalidRewardAddress.Wrap("validator address cannot be empty")
	}
	if trimmedValidatorAddr != validatorAddr {
		return types.ErrInvalidRewardAddress.Wrap("validator address must not have leading or trailing whitespace")
	}
	if strings.TrimSpace(rewardAddr) == "" {
		return nil
	}
	if _, err := sdk.AccAddressFromBech32(rewardAddr); err != nil {
		return types.ErrInvalidRewardAddress.Wrapf("invalid reward address: %v", err)
	}
	return k.rewardAddresses.Set(ctx, validatorAddr, rewardAddr)
}

func (k Keeper) getRewardAddress(ctx context.Context, validatorAddr string) (string, bool) {
	addr, err := k.rewardAddresses.Get(ctx, validatorAddr)
	if err != nil || strings.TrimSpace(addr) == "" {
		return "", false
	}
	return addr, true
}

// ValidateAuthority checks if the given address matches the module's authority
func (k Keeper) ValidateAuthority(addr string) error {
	if k.authority != addr {
		return types.ErrUnauthorized.Wrapf("expected %s, got %s", k.authority, addr)
	}
	return nil
}

func validateKeeperAssetPair(assetPair string) error {
	trimmed := strings.TrimSpace(assetPair)
	if trimmed == "" {
		return types.ErrInvalidAssetPair.Wrap("asset pair cannot be empty")
	}
	if assetPair != trimmed {
		return types.ErrInvalidAssetPair.Wrap("asset pair must not have leading or trailing whitespace")
	}
	return nil
}

func validateKeeperDecExact(value, field string) error {
	if value != strings.TrimSpace(value) {
		return types.ErrInvalidPrice.Wrapf("%s must not have leading or trailing whitespace", field)
	}
	return nil
}

// GetParams returns the current oracle module parameters
func (k Keeper) GetParams(ctx context.Context) (*types.Params, error) {
	params, err := k.params.Get(ctx)
	if err != nil {
		return defaultParams(), nil
	}
	if params == nil {
		return defaultParams(), nil
	}
	return params, nil
}

// SetParams sets the oracle module parameters
func (k Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if params == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := params.Validate(); err != nil {
		return err
	}
	return k.params.Set(ctx, params)
}

// GetPriceFeed retrieves a price feed for a specific asset pair
func (k Keeper) GetPriceFeed(ctx context.Context, assetPair string) (*types.PriceFeed, error) {
	if err := validateKeeperAssetPair(assetPair); err != nil {
		return nil, err
	}
	feed, err := k.priceFeeds.Get(ctx, assetPair)
	if err != nil {
		return nil, types.ErrPriceFeedNotFound.Wrapf("asset pair: %s", assetPair)
	}
	return feed, nil
}

// SetPriceFeed stores a price feed for a specific asset pair
func (k Keeper) SetPriceFeed(ctx context.Context, feed *types.PriceFeed) error {
	if feed == nil {
		return types.ErrInvalidPrice.Wrap("nil price feed")
	}
	if err := validateKeeperAssetPair(feed.AssetPair); err != nil {
		return err
	}
	if err := validateKeeperDecExact(feed.Price, "price"); err != nil {
		return err
	}
	priceDec, err := sdkmath.LegacyNewDecFromStr(feed.Price)
	if err != nil || !priceDec.IsPositive() {
		return types.ErrInvalidPrice.Wrap("price must be positive (non-zero)")
	}
	feed.Price = priceDec.String()

	if raw := strings.TrimSpace(feed.Volume_24H); raw != "" {
		if err := validateKeeperDecExact(feed.Volume_24H, "volume_24h"); err != nil {
			return err
		}
		volumeDec, err := sdkmath.LegacyNewDecFromStr(raw)
		if err != nil {
			return types.ErrInvalidPrice.Wrapf("invalid volume_24h: %v", err)
		}
		if volumeDec.IsNegative() {
			return types.ErrInvalidPrice.Wrap("volume_24h cannot be negative")
		}
	}

	if raw := strings.TrimSpace(feed.ConfidenceScore); raw != "" {
		if err := validateKeeperDecExact(feed.ConfidenceScore, "confidence_score"); err != nil {
			return err
		}
		confidenceDec, err := sdkmath.LegacyNewDecFromStr(raw)
		if err != nil {
			return types.ErrInvalidPrice.Wrapf("invalid confidence_score: %v", err)
		}
		if confidenceDec.IsNegative() || confidenceDec.GT(sdkmath.LegacyOneDec()) {
			return types.ErrInvalidPrice.Wrap("confidence_score must be between 0 and 1")
		}
	}

	return k.priceFeeds.Set(ctx, feed.AssetPair, feed)
}

// GetAllPriceFeeds retrieves all price feeds
func (k Keeper) GetAllPriceFeeds(ctx context.Context) ([]*types.PriceFeed, error) {
	var feeds []*types.PriceFeed
	err := k.priceFeeds.Walk(ctx, nil, func(_ string, value *types.PriceFeed) (stop bool, err error) {
		feeds = append(feeds, value)
		return false, nil
	})
	return feeds, err
}

// GetAllAggregatedPrices retrieves all aggregated prices.
func (k Keeper) GetAllAggregatedPrices(ctx context.Context) ([]*types.AggregatedPrice, error) {
	var prices []*types.AggregatedPrice
	err := k.aggregatedPrices.Walk(ctx, nil, func(_ string, value *types.AggregatedPrice) (stop bool, err error) {
		prices = append(prices, value)
		return false, nil
	})
	return prices, err
}

// GetAggregatedPrice retrieves the aggregated price for a specific asset pair
func (k Keeper) GetAggregatedPrice(ctx context.Context, assetPair string) (*types.AggregatedPrice, error) {
	if err := validateKeeperAssetPair(assetPair); err != nil {
		return nil, err
	}
	price, err := k.aggregatedPrices.Get(ctx, assetPair)
	if err != nil {
		return nil, types.ErrPriceFeedNotFound.Wrapf("asset pair: %s", assetPair)
	}
	return price, nil
}

// SetAggregatedPrice stores an aggregated price for a specific asset pair
func (k Keeper) SetAggregatedPrice(ctx context.Context, price *types.AggregatedPrice) error {
	if price == nil {
		return types.ErrInvalidPrice.Wrap("nil aggregated price")
	}
	if err := validateKeeperAssetPair(price.AssetPair); err != nil {
		return err
	}
	if err := validateKeeperDecExact(price.MedianPrice, "median price"); err != nil {
		return err
	}
	medianDec, err := sdkmath.LegacyNewDecFromStr(price.MedianPrice)
	if err != nil || !medianDec.IsPositive() {
		return types.ErrInvalidPrice.Wrap("median price must be positive")
	}
	price.MedianPrice = medianDec.String()

	if strings.TrimSpace(price.MeanPrice) != "" {
		if err := validateKeeperDecExact(price.MeanPrice, "mean price"); err != nil {
			return err
		}
		meanDec, err := sdkmath.LegacyNewDecFromStr(price.MeanPrice)
		if err != nil {
			return types.ErrInvalidPrice.Wrapf("invalid mean price: %v", err)
		}
		if !meanDec.IsPositive() {
			return types.ErrInvalidPrice.Wrap("mean price must be positive")
		}
		price.MeanPrice = meanDec.String()
	}

	if strings.TrimSpace(price.StandardDeviation) != "" {
		if err := validateKeeperDecExact(price.StandardDeviation, "standard deviation"); err != nil {
			return err
		}
		stdDec, err := sdkmath.LegacyNewDecFromStr(price.StandardDeviation)
		if err != nil {
			return types.ErrInvalidPrice.Wrapf("invalid standard deviation: %v", err)
		}
		if stdDec.IsNegative() {
			return types.ErrInvalidPrice.Wrap("standard deviation cannot be negative")
		}
		price.StandardDeviation = stdDec.String()
	}
	return k.aggregatedPrices.Set(ctx, price.AssetPair, price)
}

// GetValidatorVote retrieves a validator's vote
func (k Keeper) GetValidatorVote(ctx context.Context, validatorAddr string) (*types.ValidatorVote, error) {
	if strings.TrimSpace(validatorAddr) == "" {
		return nil, types.ErrUnauthorized.Wrap("validator address cannot be empty")
	}
	vote, err := k.validatorVotes.Get(ctx, validatorAddr)
	if err != nil {
		return nil, fmt.Errorf("validator vote not found: %s", validatorAddr)
	}
	return vote, nil
}

// SetValidatorVote stores a validator's vote
func (k Keeper) SetValidatorVote(ctx context.Context, vote *types.ValidatorVote) error {
	if vote == nil {
		return types.ErrUnauthorized.Wrap("nil validator vote")
	}
	validatorAddr := strings.TrimSpace(vote.ValidatorAddress)
	if validatorAddr == "" {
		return types.ErrUnauthorized.Wrap("validator address cannot be empty")
	}
	if vote.BlockHeight <= 0 {
		return types.ErrInvalidVoteExtension.Wrap("block height must be positive")
	}

	lastHeight, err := k.lastVoteHeights.Get(ctx, validatorAddr)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
	} else if vote.BlockHeight <= lastHeight {
		return types.ErrInvalidVoteExtension.Wrapf(
			"vote height %d must be greater than previous %d", vote.BlockHeight, lastHeight,
		)
	}

	if err := k.validatorVotes.Set(ctx, validatorAddr, vote); err != nil {
		return err
	}
	return k.lastVoteHeights.Set(ctx, validatorAddr, vote.BlockHeight)
}

// GetAllValidatorVotes retrieves all validator votes
func (k Keeper) GetAllValidatorVotes(ctx context.Context) ([]*types.ValidatorVote, error) {
	var votes []*types.ValidatorVote
	err := k.validatorVotes.Walk(ctx, nil, func(_ string, value *types.ValidatorVote) (stop bool, err error) {
		votes = append(votes, value)
		return false, nil
	})
	return votes, err
}

// ClearValidatorVotes clears all validator votes (called after aggregation)
func (k Keeper) ClearValidatorVotes(ctx context.Context) error {
	var keys []string
	err := k.validatorVotes.Walk(ctx, nil, func(key string, _ *types.ValidatorVote) (stop bool, err error) {
		keys = append(keys, key)
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, key := range keys {
		if err := k.validatorVotes.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
