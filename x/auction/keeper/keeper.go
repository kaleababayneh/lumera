// Package keeper implements state management for the auction module.
package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/auction/types"
	prioritytypes "github.com/LumeraProtocol/lumera/x/priority/types"
	reservetypes "github.com/LumeraProtocol/lumera/x/reserve/types"
)

// jsonValueCodec implements collections.ValueCodec for JSON-serializable types.
type jsonValueCodec[T any] struct{}

func (jsonValueCodec[T]) Encode(value T) ([]byte, error) {
	return json.Marshal(value)
}

func (jsonValueCodec[T]) Decode(bz []byte) (T, error) {
	var value T
	if len(bz) == 0 {
		return value, nil
	}
	if err := json.Unmarshal(bz, &value); err != nil {
		return value, err
	}
	return value, nil
}

func (jsonValueCodec[T]) EncodeJSON(value T) ([]byte, error) {
	return json.Marshal(value)
}

func (jsonValueCodec[T]) DecodeJSON(bz []byte) (T, error) {
	return jsonValueCodec[T]{}.Decode(bz)
}

func (jsonValueCodec[T]) Stringify(value T) string {
	bz, err := jsonValueCodec[T]{}.Encode(value)
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return string(bz)
}

func (jsonValueCodec[T]) ValueType() string {
	var zero T
	return fmt.Sprintf("%T", zero)
}

func newJSONCodec[T any]() jsonValueCodec[T] { return jsonValueCodec[T]{} }

// State stores module collections.
type State struct {
	Schema             collections.Schema
	Params             collections.Item[types.Params]
	ActiveAuctions     collections.Item[uint64]
	Auctions           collections.Map[string, types.SpotAuction]
	AuctionSeq         collections.Sequence
	BidSeq             collections.Sequence
	Bids               collections.Map[string, types.SpotBid]
	AuctionByRequest   collections.Map[string, string]
	AuctionBidByBidder collections.Map[collections.Pair[string, string], string]
	AuctionsByExpiry   collections.KeySet[collections.Pair[time.Time, string]]
	AuctionsBySettledDate collections.KeySet[collections.Pair[time.Time, string]]
}

// Keeper manages SpotCall auctions state.
type Keeper struct {
	cdc            codec.BinaryCodec
	storeService   corestore.KVStoreService
	authority      string
	state          State
	logger         log.Logger
	reserveKeeper  reservetypes.ReserveKeeper
	priorityKeeper prioritytypes.PriorityKeeper
}

// NewKeeper constructs the auction keeper.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService corestore.KVStoreService,
	authority string,
	logger log.Logger,
) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)

	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix([]byte{0x01}),
			"params",
			newJSONCodec[types.Params](),
		),
		ActiveAuctions: collections.NewItem(
			sb,
			collections.NewPrefix([]byte{0x02}),
			"active_auctions",
			collections.Uint64Value,
		),
		Auctions: collections.NewMap(
			sb,
			collections.NewPrefix([]byte{0x10}),
			"auctions",
			collections.StringKey,
			newJSONCodec[types.SpotAuction](),
		),
		AuctionSeq: collections.NewSequence(
			sb,
			collections.NewPrefix([]byte{0x11}),
			"auction_seq",
		),
		BidSeq: collections.NewSequence(
			sb,
			collections.NewPrefix([]byte{0x12}),
			"bid_seq",
		),
		Bids: collections.NewMap(
			sb,
			collections.NewPrefix([]byte{0x13}),
			"bids",
			collections.StringKey,
			newJSONCodec[types.SpotBid](),
		),
		AuctionByRequest: collections.NewMap(
			sb,
			collections.NewPrefix([]byte{0x20}),
			"auction_by_request",
			collections.StringKey,
			collections.StringValue,
		),
		AuctionBidByBidder: collections.NewMap(
			sb,
			collections.NewPrefix([]byte{0x21}),
			"auction_bid_by_bidder",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			collections.StringValue,
		),
		AuctionsByExpiry: collections.NewKeySet(
			sb,
			collections.NewPrefix([]byte{0x22}),
			"auctions_by_expiry",
			collections.PairKeyCodec(sdk.TimeKey, collections.StringKey),
		),
		AuctionsBySettledDate: collections.NewKeySet(
			sb,
			collections.NewPrefix([]byte{0x30}),
			"auctions_by_settled_date",
			collections.PairKeyCodec(sdk.TimeKey, collections.StringKey),
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build auction schema: %w", err))
	}
	state.Schema = schema

	return &Keeper{
		cdc:          cdc,
		storeService: storeService,
		authority:    authority,
		state:        state,
		logger:       logger,
	}
}

// Logger returns the module logger enriched with module name.
func (k Keeper) Logger() log.Logger {
	return k.logger.With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// SetReserveKeeper wires the reserve keeper dependency into the auction keeper.
func (k *Keeper) SetReserveKeeper(res reservetypes.ReserveKeeper) {
	k.reserveKeeper = res
}

// SetPriorityKeeper registers the priority keeper used for validator scoring.
func (k *Keeper) SetPriorityKeeper(p prioritytypes.PriorityKeeper) {
	k.priorityKeeper = p
}

// Schema exposes collections schema.
func (k *Keeper) Schema() collections.Schema { return k.state.Schema }

// GetParams returns module parameters, defaulting when unset.
func (k *Keeper) GetParams(ctx context.Context) (*types.Params, error) {
	params, err := k.state.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			def := types.DefaultParams()
			return &def, nil
		}
		return nil, err
	}
	return &params, nil
}

// SetParams validates and stores params.
func (k *Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if err := params.ValidateBasic(); err != nil {
		return err
	}
	return k.state.Params.Set(ctx, *params)
}

func (k *Keeper) getActiveCount(ctx context.Context) (uint64, error) {
	count, err := k.state.ActiveAuctions.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

func (k *Keeper) setActiveCount(ctx context.Context, count uint64) error {
	return k.state.ActiveAuctions.Set(ctx, count)
}

// ProcessExpiredAuctions iterates through active auctions that have passed their expiry time
// and finalizes them (either settling with best bid or expiring).
func (k *Keeper) ProcessExpiredAuctions(ctx context.Context, limit int) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime()

	if limit <= 0 {
		limit = 100 // Default limit
	}

	type expiredAuction struct {
		timestamp time.Time
		auctionID string
	}
	// EndExclusive((now+1ns, "")) catches every pair whose timestamp is <= now
	// regardless of its auction_id suffix. Using EndInclusive((now, "")) skipped
	// auctions that expired at exactly the current block time, because any
	// non-empty auction_id sorts lexicographically after the empty string and
	// fell outside the range.
	var expiredAuctions []expiredAuction
	rng := new(collections.Range[collections.Pair[time.Time, string]]).
		EndExclusive(collections.Join(now.Add(time.Nanosecond), ""))

	// Iterate by expiry time
	err := k.state.AuctionsByExpiry.Walk(ctx, rng, func(key collections.Pair[time.Time, string]) (bool, error) {
		expiredAuctions = append(expiredAuctions, expiredAuction{
			timestamp: key.K1(),
			auctionID: key.K2(),
		})
		return len(expiredAuctions) >= limit, nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk expired auctions: %w", err)
	}

	for _, expired := range expiredAuctions {
		// FinalizeSpotAuction handles expiration logic (settle if bid exists, else expire)
		// and removes from AuctionsByExpiry.
		if _, _, err := k.FinalizeSpotAuction(ctx, expired.auctionID); err != nil {
			if errors.Is(err, types.ErrAuctionNotFound) {
				if rmErr := k.state.AuctionsByExpiry.Remove(ctx, collections.Join(expired.timestamp, expired.auctionID)); rmErr != nil {
					k.Logger().Error("failed to remove orphaned auction expiry index", "id", expired.auctionID, "error", rmErr)
				} else {
					k.Logger().Warn("pruned orphaned auction expiry index", "id", expired.auctionID)
				}
			} else {
				k.Logger().Error("failed to finalize expired auction", "id", expired.auctionID, "error", err)
			}
		}
	}

	return nil
}

// PruneAuctions removes settled/expired/canceled auctions older than the retention period.
func (k *Keeper) PruneAuctions(ctx context.Context, retention time.Duration, limit int) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cutoff := sdkCtx.BlockTime().Add(-retention)

	if limit <= 0 {
		limit = 100
	}

	// EndExclusive((cutoff+1ns, "")) catches every pair whose timestamp is
	// <= cutoff regardless of its auction_id suffix. Using
	// EndInclusive((cutoff, "")) skipped settled auctions whose settlement
	// timestamp landed exactly at the cutoff, since any non-empty auction_id
	// sorts lexicographically after the empty string and fell outside the
	// range — the same boundary bug as ProcessExpiredAuctions.
	var keysToDelete []collections.Pair[time.Time, string]
	rng := new(collections.Range[collections.Pair[time.Time, string]]).
		EndExclusive(collections.Join(cutoff.Add(time.Nanosecond), ""))

	err := k.state.AuctionsBySettledDate.Walk(ctx, rng, func(key collections.Pair[time.Time, string]) (bool, error) {
		keysToDelete = append(keysToDelete, key)
		return len(keysToDelete) >= limit, nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk settled auctions: %w", err)
	}

	for _, key := range keysToDelete {
		auctionID := key.K2()
		_, err := k.state.Auctions.Get(ctx, auctionID)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			k.Logger().Error("failed to load auction for pruning", "id", auctionID, "error", err)
			continue
		}

		// Delete all bids associated with this auction using the AuctionBidByBidder index.
		// The index key is (AuctionID, Bidder), so we can prefix scan by AuctionID.
		var bidderKeys []collections.Pair[string, string]
		bidRange := collections.NewPrefixedPairRange[string, string](auctionID)

		if walkErr := k.state.AuctionBidByBidder.Walk(ctx, bidRange, func(key collections.Pair[string, string], bidID string) (bool, error) {
			// Delete the bid itself
			if err := k.state.Bids.Remove(ctx, bidID); err != nil {
				k.Logger().Error("failed to remove pruned bid", "bid_id", bidID, "error", err)
			}
			bidderKeys = append(bidderKeys, key)
			return false, nil
		}); walkErr != nil {
			k.Logger().Error("failed to walk bid index during pruning", "auction_id", auctionID, "error", walkErr)
		}

		// Delete the index entries
		for _, bKey := range bidderKeys {
			if err := k.state.AuctionBidByBidder.Remove(ctx, bKey); err != nil {
				k.Logger().Error("failed to remove bid index", "key", bKey, "error", err)
			}
		}

		if !errors.Is(err, collections.ErrNotFound) {
			if err := k.state.Auctions.Remove(ctx, auctionID); err != nil {
				k.Logger().Error("failed to remove pruned auction", "id", auctionID, "error", err)
			}
		}

		if err := k.state.AuctionsBySettledDate.Remove(ctx, key); err != nil {
			k.Logger().Error("failed to remove settled index", "id", auctionID, "error", err)
		}
	}

	return nil
}
