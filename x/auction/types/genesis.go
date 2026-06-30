// Package types holds shared types and helpers for the auction module.
package types

import (
	"fmt"
	"time"
)

// GenesisState defines the auction module's genesis state.
type GenesisState struct {
	// Params defines module parameters.
	Params Params `json:"params"`
	// Auctions contains all auctions to restore.
	Auctions []SpotAuction `json:"auctions"`
	// Bids contains all bids to restore.
	Bids []SpotBid `json:"bids"`
	// AuctionSeq is the sequence counter for auction IDs.
	AuctionSeq uint64 `json:"auction_seq"`
	// BidSeq is the sequence counter for bid IDs.
	BidSeq uint64 `json:"bid_seq"`
	// ActiveAuctionCount tracks the number of currently active auctions.
	ActiveAuctionCount uint64 `json:"active_auction_count"`
}

// NewGenesisState creates a new genesis state instance.
func NewGenesisState(params Params, auctions []SpotAuction, bids []SpotBid, auctionSeq, bidSeq, activeCount uint64) *GenesisState {
	return &GenesisState{
		Params:             params,
		Auctions:           auctions,
		Bids:               bids,
		AuctionSeq:         auctionSeq,
		BidSeq:             bidSeq,
		ActiveAuctionCount: activeCount,
	}
}

// DefaultGenesis returns the default genesis state for the auction module.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:             DefaultParams(),
		Auctions:           []SpotAuction{},
		Bids:               []SpotBid{},
		AuctionSeq:         0,
		BidSeq:             0,
		ActiveAuctionCount: 0,
	}
}

// Validate performs validation on the genesis state.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}

	if err := gs.Params.ValidateBasic(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	if err := gs.validateAuctions(); err != nil {
		return err
	}

	if err := gs.validateBids(); err != nil {
		return err
	}

	if err := gs.validateSequences(); err != nil {
		return err
	}

	return nil
}

// validateAuctions checks auction entries for consistency.
func (gs *GenesisState) validateAuctions() error {
	seen := make(map[string]struct{}, len(gs.Auctions))
	liveRequestIDs := make(map[string]string)
	activeCount := uint64(0)

	for i, auction := range gs.Auctions {
		if auction.ID == "" {
			return fmt.Errorf("auction at index %d has empty id", i)
		}
		if _, dup := seen[auction.ID]; dup {
			return fmt.Errorf("duplicate auction id %s", auction.ID)
		}
		seen[auction.ID] = struct{}{}

		if err := auction.ValidateBasic(); err != nil {
			return fmt.Errorf("invalid auction %s: %w", auction.ID, err)
		}
		if auction.BestBidID != "" {
			if err := validateBidTimestampInAuctionWindow(auction, "best bid metadata", auction.BestBidSubmittedAt); err != nil {
				return err
			}
		}

		if isGenesisLiveAuction(auction.Status) {
			if !auction.ExpiresAt.After(auction.CreatedAt) {
				return fmt.Errorf("live auction %s expires at must be after created at", auction.ID)
			}
			if existing, dup := liveRequestIDs[auction.RequestID]; dup {
				return fmt.Errorf("duplicate live auction request id %s for auctions %s and %s", auction.RequestID, existing, auction.ID)
			}
			liveRequestIDs[auction.RequestID] = auction.ID
			activeCount++
		}
	}

	if gs.ActiveAuctionCount != activeCount {
		return fmt.Errorf("active auction count mismatch: genesis has %d but found %d active auctions",
			gs.ActiveAuctionCount, activeCount)
	}

	return nil
}

func isGenesisLiveAuction(status AuctionStatus) bool {
	return status == AuctionStatusActive || status == AuctionStatusPending
}

// validateBids checks bid entries for consistency.
func (gs *GenesisState) validateBids() error {
	// Build auction ID set for reference validation
	auctionsByID := make(map[string]SpotAuction, len(gs.Auctions))
	for _, auction := range gs.Auctions {
		auctionsByID[auction.ID] = auction
	}

	seen := make(map[string]struct{}, len(gs.Bids))
	bidsByID := make(map[string]SpotBid, len(gs.Bids))
	bidderPerAuction := make(map[string]map[string]struct{})

	for i, bid := range gs.Bids {
		if bid.ID == "" {
			return fmt.Errorf("bid at index %d has empty id", i)
		}
		if _, dup := seen[bid.ID]; dup {
			return fmt.Errorf("duplicate bid id %s", bid.ID)
		}
		seen[bid.ID] = struct{}{}
		bidsByID[bid.ID] = bid

		if err := bid.ValidateBasic(); err != nil {
			return fmt.Errorf("invalid bid %s: %w", bid.ID, err)
		}

		// Validate auction reference
		auction, exists := auctionsByID[bid.AuctionID]
		if !exists {
			return fmt.Errorf("bid %s references non-existent auction %s", bid.ID, bid.AuctionID)
		}
		if err := validateBidTimestampInAuctionWindow(auction, fmt.Sprintf("bid %s", bid.ID), bid.SubmittedAt); err != nil {
			return err
		}

		// Check for duplicate bidder per auction
		if bidderPerAuction[bid.AuctionID] == nil {
			bidderPerAuction[bid.AuctionID] = make(map[string]struct{})
		}
		if _, dup := bidderPerAuction[bid.AuctionID][bid.Bidder]; dup {
			return fmt.Errorf("duplicate bid from bidder %s for auction %s", bid.Bidder, bid.AuctionID)
		}
		bidderPerAuction[bid.AuctionID][bid.Bidder] = struct{}{}
	}

	if err := gs.validateAuctionBidReferences(bidsByID); err != nil {
		return err
	}

	return nil
}

// validateAuctionBidReferences ensures auction-level bid pointers resolve
// to bids imported for the same auction. Reserve fallback IDs are commitment
// IDs rather than bid IDs, so reserve-applied auctions are excluded here.
func (gs *GenesisState) validateAuctionBidReferences(bidsByID map[string]SpotBid) error {
	for _, auction := range gs.Auctions {
		if auction.ReserveApplied {
			continue
		}
		if err := validateAuctionBidReference(auction, "best bid", auction.BestBidID, bidsByID); err != nil {
			return err
		}
		if err := validateAuctionBidReference(auction, "winner bid", auction.WinnerBidID, bidsByID); err != nil {
			return err
		}
		if err := validateAuctionBestBidMetadata(auction, bidsByID); err != nil {
			return err
		}
	}
	return nil
}

func validateAuctionBidReference(auction SpotAuction, label, bidID string, bidsByID map[string]SpotBid) error {
	if bidID == "" {
		return nil
	}
	bid, exists := bidsByID[bidID]
	if !exists {
		return fmt.Errorf("auction %s references non-existent %s %s", auction.ID, label, bidID)
	}
	if bid.AuctionID != auction.ID {
		return fmt.Errorf("auction %s references %s %s belonging to auction %s", auction.ID, label, bidID, bid.AuctionID)
	}
	if err := validateBidTimestampInAuctionWindow(auction, fmt.Sprintf("%s %s", label, bidID), bid.SubmittedAt); err != nil {
		return err
	}
	return nil
}

func validateAuctionBestBidMetadata(auction SpotAuction, bidsByID map[string]SpotBid) error {
	if auction.BestBidID == "" {
		return nil
	}
	bid := bidsByID[auction.BestBidID]
	if auction.BestBidLatencyMs != bid.LatencyMs {
		return fmt.Errorf("auction %s best bid latency does not match bid %s", auction.ID, bid.ID)
	}
	if !auction.BestBidSubmittedAt.Equal(bid.SubmittedAt) {
		return fmt.Errorf("auction %s best bid submitted_at does not match bid %s", auction.ID, bid.ID)
	}
	if isGenesisLiveAuction(auction.Status) && !auction.BestBidPrice.Equal(bid.Price) {
		return fmt.Errorf("auction %s live best bid price does not match bid %s", auction.ID, bid.ID)
	}
	return nil
}

func validateBidTimestampInAuctionWindow(auction SpotAuction, label string, submittedAt time.Time) error {
	if submittedAt.Before(auction.CreatedAt) || submittedAt.After(auction.ExpiresAt) {
		return fmt.Errorf("%s for auction %s submitted at must be between created at and expires at", label, auction.ID)
	}
	return nil
}

// validateSequences ensures sequence counters are consistent with data.
func (gs *GenesisState) validateSequences() error {
	// Find max auction sequence number from IDs (auc-N format)
	maxAuctionSeq := uint64(0)
	for _, auction := range gs.Auctions {
		var seq uint64
		if _, err := fmt.Sscanf(auction.ID, "auc-%d", &seq); err == nil {
			if seq >= maxAuctionSeq {
				maxAuctionSeq = seq + 1
			}
		}
	}
	if gs.AuctionSeq < maxAuctionSeq {
		return fmt.Errorf("auction sequence %d is less than required minimum %d", gs.AuctionSeq, maxAuctionSeq)
	}

	// Find max bid sequence number from IDs (bid-N format)
	maxBidSeq := uint64(0)
	for _, bid := range gs.Bids {
		var seq uint64
		if _, err := fmt.Sscanf(bid.ID, "bid-%d", &seq); err == nil {
			if seq >= maxBidSeq {
				maxBidSeq = seq + 1
			}
		}
	}
	if gs.BidSeq < maxBidSeq {
		return fmt.Errorf("bid sequence %d is less than required minimum %d", gs.BidSeq, maxBidSeq)
	}

	return nil
}
