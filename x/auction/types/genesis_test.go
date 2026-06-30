package types

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultGenesis(t *testing.T) {
	g := DefaultGenesis()
	require.NotNil(t, g)
	require.Empty(t, g.Auctions)
	require.Empty(t, g.Bids)
	require.Equal(t, uint64(0), g.AuctionSeq)
	require.Equal(t, uint64(0), g.BidSeq)
	require.Equal(t, uint64(0), g.ActiveAuctionCount)
	require.NoError(t, g.Params.ValidateBasic())
}

func TestNewGenesisState(t *testing.T) {
	params := DefaultParams()
	auctions := []SpotAuction{validAuction()}
	bids := []SpotBid{validBid()}

	g := NewGenesisState(params, auctions, bids, 10, 5, 1)

	require.NotNil(t, g)
	require.Equal(t, params, g.Params)
	require.Len(t, g.Auctions, 1)
	require.Len(t, g.Bids, 1)
	require.Equal(t, uint64(10), g.AuctionSeq)
	require.Equal(t, uint64(5), g.BidSeq)
	require.Equal(t, uint64(1), g.ActiveAuctionCount)
}

func TestGenesisState_Validate_NilState(t *testing.T) {
	var g *GenesisState
	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestGenesisState_Validate_DefaultValid(t *testing.T) {
	g := DefaultGenesis()
	require.NoError(t, g.Validate())
}

func TestGenesisState_Validate_InvalidParams(t *testing.T) {
	g := DefaultGenesis()
	g.Params.DefaultAuctionTTLSeconds = 0 // Invalid - must be positive
	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "params")
}

func TestGenesisState_Validate_AuctionWithEmptyID(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	auction.ID = ""
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty id")
}

func TestGenesisState_Validate_DuplicateAuctionID(t *testing.T) {
	g := DefaultGenesis()
	a1 := validAuction()
	a2 := validAuction()
	a2.Owner = "owner2" // Different owner but same ID
	g.Auctions = []SpotAuction{a1, a2}
	g.ActiveAuctionCount = 2

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate auction id")
}

func TestGenesisState_Validate_InvalidAuction(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	auction.MaxLatencyMs = 0 // Invalid
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid auction")
}

func TestGenesisState_Validate_ActiveCountMismatch(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	auction.Status = AuctionStatusActive
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 0 // Should be 1

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "active auction count mismatch")
}

func TestGenesisState_Validate_DuplicateLiveAuctionRequestID(t *testing.T) {
	tests := []struct {
		name   string
		status AuctionStatus
	}{
		{name: "active", status: AuctionStatusActive},
		{name: "pending", status: AuctionStatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := DefaultGenesis()
			a1 := validAuction()
			a1.Status = tt.status
			a2 := validAuction()
			a2.ID = "auc-2"
			a2.Status = tt.status
			a2.RequestID = a1.RequestID
			g.Auctions = []SpotAuction{a1, a2}
			g.ActiveAuctionCount = 2
			g.AuctionSeq = 3

			err := g.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "duplicate live auction request id")
		})
	}
}

func TestGenesisState_Validate_DuplicateTerminalAuctionRequestIDAllowed(t *testing.T) {
	g := DefaultGenesis()
	a1 := validAuction()
	a1.Status = AuctionStatusSettled
	a2 := validAuction()
	a2.ID = "auc-2"
	a2.Status = AuctionStatusExpired
	a2.RequestID = a1.RequestID
	g.Auctions = []SpotAuction{a1, a2}
	g.ActiveAuctionCount = 0
	g.AuctionSeq = 3

	require.NoError(t, g.Validate())
}

func TestGenesisState_Validate_PendingCountedAsActive(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	auction.Status = AuctionStatusPending
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1
	g.AuctionSeq = 2 // auc-1 requires seq >= 2

	require.NoError(t, g.Validate())
}

func TestGenesisState_Validate_LiveAuctionExpiresEqualCreated(t *testing.T) {
	for _, status := range []AuctionStatus{AuctionStatusActive, AuctionStatusPending} {
		t.Run(string(status), func(t *testing.T) {
			g := DefaultGenesis()
			auction := validAuction()
			auction.Status = status
			auction.ExpiresAt = auction.CreatedAt
			g.Auctions = []SpotAuction{auction}
			g.ActiveAuctionCount = 1
			g.AuctionSeq = 2

			err := g.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "expires at must be after created at")
		})
	}
}

func TestGenesisState_Validate_TerminalAuctionExpiresEqualCreatedAllowed(t *testing.T) {
	for _, status := range []AuctionStatus{AuctionStatusSettled, AuctionStatusExpired, AuctionStatusCanceled} {
		t.Run(string(status), func(t *testing.T) {
			g := DefaultGenesis()
			auction := validAuction()
			auction.Status = status
			auction.ExpiresAt = auction.CreatedAt
			g.Auctions = []SpotAuction{auction}
			g.ActiveAuctionCount = 0
			g.AuctionSeq = 2

			require.NoError(t, g.Validate())
		})
	}
}

func TestGenesisState_Validate_SettledNotCountedAsActive(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	auction.Status = AuctionStatusSettled
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 0

	// AuctionSeq must be >= 2 because auc-1 exists
	g.AuctionSeq = 2

	require.NoError(t, g.Validate())
}

func TestGenesisState_Validate_ExpiredNotCountedAsActive(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	auction.Status = AuctionStatusExpired
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 0
	g.AuctionSeq = 2

	require.NoError(t, g.Validate())
}

func TestGenesisState_Validate_CanceledNotCountedAsActive(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	auction.Status = AuctionStatusCanceled
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 0
	g.AuctionSeq = 2

	require.NoError(t, g.Validate())
}

func TestGenesisState_Validate_BidWithEmptyID(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1

	bid := validBid()
	bid.ID = ""
	g.Bids = []SpotBid{bid}

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty id")
}

func TestGenesisState_Validate_DuplicateBidID(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1

	b1 := validBid()
	b2 := validBid()
	b2.Bidder = "bidder2" // Different bidder but same ID
	g.Bids = []SpotBid{b1, b2}

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate bid id")
}

func TestGenesisState_Validate_InvalidBid(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1

	bid := validBid()
	bid.LatencyMs = 0 // Invalid
	g.Bids = []SpotBid{bid}

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid bid")
}

func TestGenesisState_Validate_BidReferencesNonExistentAuction(t *testing.T) {
	g := DefaultGenesis()

	bid := validBid()
	bid.AuctionID = "auc-999" // Non-existent auction
	g.Bids = []SpotBid{bid}
	g.BidSeq = 2

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-existent auction")
}

func TestGenesisState_Validate_AuctionBestBidReferencesMissingBid(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	bestBid := validBid()
	auction.BestBidID = bestBid.ID
	auction.BestBidPrice = bestBid.Price
	auction.BestBidLatencyMs = bestBid.LatencyMs
	auction.BestBidSubmittedAt = bestBid.SubmittedAt
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1
	g.AuctionSeq = 2

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-existent best bid")
}

func TestGenesisState_Validate_AuctionBestBidReferencesAnotherAuction(t *testing.T) {
	g := DefaultGenesis()

	auction := validAuction()
	foreignAuction := validAuction()
	foreignAuction.ID = "auc-2"
	foreignAuction.RequestID = "req-2"

	bestBid := validBid()
	bestBid.ID = "bid-2"
	bestBid.AuctionID = foreignAuction.ID
	auction.BestBidID = bestBid.ID
	auction.BestBidPrice = bestBid.Price
	auction.BestBidLatencyMs = bestBid.LatencyMs
	auction.BestBidSubmittedAt = bestBid.SubmittedAt

	g.Auctions = []SpotAuction{auction, foreignAuction}
	g.Bids = []SpotBid{bestBid}
	g.ActiveAuctionCount = 2
	g.AuctionSeq = 3
	g.BidSeq = 3

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "belonging to auction auc-2")
}

func TestGenesisState_Validate_BidSubmittedOutsideAuctionWindow(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()

	tests := []struct {
		name        string
		submittedAt time.Time
	}{
		{
			name:        "before created at",
			submittedAt: base.Add(-time.Second),
		},
		{
			name:        "after expires at",
			submittedAt: base.Add(31 * time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := DefaultGenesis()
			auction := validAuction()
			auction.CreatedAt = base
			auction.ExpiresAt = base.Add(30 * time.Second)

			bid := validBid()
			bid.AuctionID = auction.ID
			bid.SubmittedAt = tt.submittedAt

			g.Auctions = []SpotAuction{auction}
			g.Bids = []SpotBid{bid}
			g.ActiveAuctionCount = 1
			g.AuctionSeq = 2
			g.BidSeq = 2

			err := g.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "submitted at must be between created at and expires at")
		})
	}
}

func TestGenesisState_Validate_BestBidMetadataSubmittedOutsideAuctionWindow(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()

	g := DefaultGenesis()
	auction := validAuction()
	auction.CreatedAt = base
	auction.ExpiresAt = base.Add(30 * time.Second)

	bid := validBid()
	bid.SubmittedAt = base.Add(10 * time.Second)
	auction.BestBidID = bid.ID
	auction.BestBidPrice = bid.Price
	auction.BestBidLatencyMs = bid.LatencyMs
	auction.BestBidSubmittedAt = auction.ExpiresAt.Add(time.Second)

	g.Auctions = []SpotAuction{auction}
	g.Bids = []SpotBid{bid}
	g.ActiveAuctionCount = 1
	g.AuctionSeq = 2
	g.BidSeq = 2

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "best bid metadata")
	assert.Contains(t, err.Error(), "submitted at must be between created at and expires at")
}

func TestGenesisState_Validate_LiveBestBidMetadataMatchesReferencedBid(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()

	tests := []struct {
		name    string
		mutate  func(*SpotAuction)
		wantErr string
	}{
		{
			name: "price mismatch",
			mutate: func(auction *SpotAuction) {
				auction.BestBidPrice = sdk.NewInt64Coin(DefaultCreditDenom, 499_999)
			},
			wantErr: "live best bid price does not match",
		},
		{
			name: "latency mismatch",
			mutate: func(auction *SpotAuction) {
				auction.BestBidLatencyMs++
			},
			wantErr: "best bid latency does not match",
		},
		{
			name: "submitted_at mismatch",
			mutate: func(auction *SpotAuction) {
				auction.BestBidSubmittedAt = auction.BestBidSubmittedAt.Add(time.Second)
			},
			wantErr: "best bid submitted_at does not match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := DefaultGenesis()
			auction := validAuction()
			auction.CreatedAt = base
			auction.ExpiresAt = base.Add(30 * time.Second)

			bid := validBid()
			bid.AuctionID = auction.ID
			bid.Price = sdk.NewInt64Coin(DefaultCreditDenom, 500_000)
			bid.LatencyMs = 1_000
			bid.SubmittedAt = base.Add(10 * time.Second)

			auction.BestBidID = bid.ID
			auction.BestBidPrice = bid.Price
			auction.BestBidLatencyMs = bid.LatencyMs
			auction.BestBidSubmittedAt = bid.SubmittedAt
			tt.mutate(&auction)

			g.Auctions = []SpotAuction{auction}
			g.Bids = []SpotBid{bid}
			g.ActiveAuctionCount = 1
			g.AuctionSeq = 2
			g.BidSeq = 2

			err := g.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestGenesisState_Validate_SettledBestBidAllowsPriorityDiscountedPrice(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()

	g := DefaultGenesis()
	auction := validAuction()
	auction.CreatedAt = base
	auction.ExpiresAt = base.Add(30 * time.Second)
	auction.Status = AuctionStatusSettled
	auction.PriorityDiscountBps = 1_000

	bid := validBid()
	bid.AuctionID = auction.ID
	bid.Price = sdk.NewInt64Coin(DefaultCreditDenom, 500_000)
	bid.LatencyMs = 1_000
	bid.SubmittedAt = base.Add(10 * time.Second)

	auction.BestBidID = bid.ID
	auction.WinnerBidID = bid.ID
	auction.BestBidPrice = sdk.NewInt64Coin(DefaultCreditDenom, 450_000)
	auction.BestBidLatencyMs = bid.LatencyMs
	auction.BestBidSubmittedAt = bid.SubmittedAt

	g.Auctions = []SpotAuction{auction}
	g.Bids = []SpotBid{bid}
	g.ActiveAuctionCount = 0
	g.AuctionSeq = 2
	g.BidSeq = 2

	require.NoError(t, g.Validate())
}

func TestGenesisState_Validate_BidSubmittedAtAuctionWindowBoundaries(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()

	for _, submittedAt := range []time.Time{base, base.Add(30 * time.Second)} {
		g := DefaultGenesis()
		auction := validAuction()
		auction.CreatedAt = base
		auction.ExpiresAt = base.Add(30 * time.Second)

		bid := validBid()
		bid.SubmittedAt = submittedAt

		g.Auctions = []SpotAuction{auction}
		g.Bids = []SpotBid{bid}
		g.ActiveAuctionCount = 1
		g.AuctionSeq = 2
		g.BidSeq = 2

		require.NoError(t, g.Validate())
	}
}

func TestGenesisState_Validate_DuplicateBidderPerAuction(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1

	b1 := validBid()
	b1.ID = "bid-1"
	b2 := validBid()
	b2.ID = "bid-2"
	// Same bidder for same auction
	g.Bids = []SpotBid{b1, b2}

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate bid from bidder")
}

func TestGenesisState_Validate_AuctionSeqTooLow(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	auction.ID = "auc-5" // Implies sequence >= 6 is needed
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1
	g.AuctionSeq = 3 // Too low

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auction sequence")
}

func TestGenesisState_Validate_BidSeqTooLow(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1

	bid := validBid()
	bid.ID = "bid-10" // Implies sequence >= 11 is needed
	g.Bids = []SpotBid{bid}
	g.BidSeq = 5 // Too low
	g.AuctionSeq = 2

	err := g.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bid sequence")
}

func TestGenesisState_Validate_ValidComplex(t *testing.T) {
	g := DefaultGenesis()

	// Create multiple auctions with different statuses
	a1 := validAuction()
	a1.ID = "auc-1"
	a1.Status = AuctionStatusActive

	a2 := validAuction()
	a2.ID = "auc-2"
	a2.Owner = validAuctionOwner("auction-owner-000002")
	a2.RequestID = "req-2"
	a2.Status = AuctionStatusPending

	a3 := validAuction()
	a3.ID = "auc-3"
	a3.Owner = validAuctionOwner("auction-owner-000003")
	a3.RequestID = "req-3"
	a3.Status = AuctionStatusSettled

	g.Auctions = []SpotAuction{a1, a2, a3}
	g.ActiveAuctionCount = 2 // Active + Pending

	// Create bids for different auctions (use valid bech32 addresses)
	b1 := validBid()
	b1.ID = "bid-1"
	b1.AuctionID = "auc-1"
	// Keep existing valid bech32 bidder from validBid()

	b2 := validBid()
	b2.ID = "bid-2"
	b2.AuctionID = "auc-1"
	b2.Bidder = "cosmos1syavy2npfyt9tcncdtsdzf7kny9lh777pahuux" // Different valid address

	b3 := validBid()
	b3.ID = "bid-3"
	b3.AuctionID = "auc-2"
	// Keep existing valid bech32 bidder from validBid()

	g.Bids = []SpotBid{b1, b2, b3}

	g.AuctionSeq = 4
	g.BidSeq = 4

	require.NoError(t, g.Validate())
}

func TestGenesisState_Validate_NonStandardIDAuctionSeq(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	auction.ID = "custom-auction-id" // Non-standard ID format
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1
	g.AuctionSeq = 0 // Should be fine since ID doesn't match auc-N format

	require.NoError(t, g.Validate())
}

func TestGenesisState_Validate_NonStandardIDBidSeq(t *testing.T) {
	g := DefaultGenesis()
	auction := validAuction()
	g.Auctions = []SpotAuction{auction}
	g.ActiveAuctionCount = 1
	g.AuctionSeq = 2

	bid := validBid()
	bid.ID = "custom-bid-id" // Non-standard ID format
	g.Bids = []SpotBid{bid}
	g.BidSeq = 0 // Should be fine since ID doesn't match bid-N format

	require.NoError(t, g.Validate())
}
