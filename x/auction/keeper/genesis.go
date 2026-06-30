package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"github.com/LumeraProtocol/lumera/x/auction/types"
)

// InitGenesis initializes the auction module's state from a genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, genesis *types.GenesisState) error {
	if genesis == nil {
		genesis = types.DefaultGenesis()
	}

	if err := genesis.Validate(); err != nil {
		return fmt.Errorf("invalid genesis state: %w", err)
	}

	// 1. Set params
	if err := k.SetParams(ctx, &genesis.Params); err != nil {
		return fmt.Errorf("set params: %w", err)
	}

	// 2. Set active auction count
	if err := k.setActiveCount(ctx, genesis.ActiveAuctionCount); err != nil {
		return fmt.Errorf("set active auction count: %w", err)
	}

	// 3. Import auctions and build indexes
	for _, auction := range genesis.Auctions {
		if err := k.state.Auctions.Set(ctx, auction.ID, auction); err != nil {
			return fmt.Errorf("import auction %s: %w", auction.ID, err)
		}
		// Rebuild AuctionByRequest index for active auctions
		if auction.Status == types.AuctionStatusActive || auction.Status == types.AuctionStatusPending {
			if auction.RequestID != "" {
				if err := k.state.AuctionByRequest.Set(ctx, auction.RequestID, auction.ID); err != nil {
					return fmt.Errorf("index auction request %s: %w", auction.RequestID, err)
				}
			}
			// Rebuild AuctionsByExpiry index for active auctions
			if err := k.state.AuctionsByExpiry.Set(ctx, collections.Join(auction.ExpiresAt, auction.ID)); err != nil {
				return fmt.Errorf("index auction expiry %s: %w", auction.ID, err)
			}
		}
	}

	// 4. Import bids and build indexes
	for _, bid := range genesis.Bids {
		if err := k.state.Bids.Set(ctx, bid.ID, bid); err != nil {
			return fmt.Errorf("import bid %s: %w", bid.ID, err)
		}
		// Rebuild AuctionBidByBidder index
		bidKey := collections.Join(bid.AuctionID, bid.Bidder)
		if err := k.state.AuctionBidByBidder.Set(ctx, bidKey, bid.ID); err != nil {
			return fmt.Errorf("index bidder bid %s: %w", bid.ID, err)
		}
	}

	// 5. Set sequences
	if err := k.state.AuctionSeq.Set(ctx, genesis.AuctionSeq); err != nil {
		return fmt.Errorf("set auction sequence: %w", err)
	}
	if err := k.state.BidSeq.Set(ctx, genesis.BidSeq); err != nil {
		return fmt.Errorf("set bid sequence: %w", err)
	}

	return nil
}

// ExportGenesis exports the current state to a genesis state.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	// 1. Get params
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, fmt.Errorf("get params: %w", err)
	}

	// 2. Get active auction count
	activeCount, err := k.getActiveCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("get active auction count: %w", err)
	}

	// 3. Export all auctions
	auctions := make([]types.SpotAuction, 0)
	err = k.state.Auctions.Walk(ctx, nil, func(id string, auction types.SpotAuction) (bool, error) {
		auctions = append(auctions, auction)
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("export auctions: %w", err)
	}

	// 4. Export all bids
	bids := make([]types.SpotBid, 0)
	err = k.state.Bids.Walk(ctx, nil, func(id string, bid types.SpotBid) (bool, error) {
		bids = append(bids, bid)
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("export bids: %w", err)
	}

	// 5. Get sequence values
	auctionSeq, err := k.state.AuctionSeq.Peek(ctx)
	if err != nil {
		// Sequence not initialized, use 0
		auctionSeq = 0
	}

	bidSeq, err := k.state.BidSeq.Peek(ctx)
	if err != nil {
		// Sequence not initialized, use 0
		bidSeq = 0
	}

	return types.NewGenesisState(*params, auctions, bids, auctionSeq, bidSeq, activeCount), nil
}

// GetAuction fetches an auction by its ID. Exported for genesis operations.
func (k *Keeper) GetAuction(ctx context.Context, auctionID string) (*types.SpotAuction, error) {
	return k.getAuction(ctx, auctionID)
}

// GetBid fetches a bid by its ID.
func (k *Keeper) GetBid(ctx context.Context, bidID string) (*types.SpotBid, error) {
	bid, err := k.state.Bids.Get(ctx, bidID)
	if err != nil {
		return nil, fmt.Errorf("load bid %s: %w", bidID, err)
	}
	return &bid, nil
}

// GetAllAuctions returns all auctions in state.
func (k *Keeper) GetAllAuctions(ctx context.Context) ([]types.SpotAuction, error) {
	auctions := make([]types.SpotAuction, 0)
	err := k.state.Auctions.Walk(ctx, nil, func(id string, auction types.SpotAuction) (bool, error) {
		auctions = append(auctions, auction)
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("get all auctions: %w", err)
	}
	return auctions, nil
}

// GetAllBids returns all bids in state.
func (k *Keeper) GetAllBids(ctx context.Context) ([]types.SpotBid, error) {
	bids := make([]types.SpotBid, 0)
	err := k.state.Bids.Walk(ctx, nil, func(id string, bid types.SpotBid) (bool, error) {
		bids = append(bids, bid)
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("get all bids: %w", err)
	}
	return bids, nil
}
