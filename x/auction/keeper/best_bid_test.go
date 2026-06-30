package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/auction/types"
)

// TestBestBidSelection_LowestPriceWins tests that lowest price bid becomes best.
func TestBestBidSelection_LowestPriceWins(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-best-price",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// First bid at 800k
	bidder1 := newAccountAddr(t)
	bid1, err := keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder1,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	// Verify it's the best bid
	auctionRec, err := keeper.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	assert.Equal(t, bid1.ID, auctionRec.BestBidID)

	// Second bid at 900k (more expensive) - rejected by improvement enforcement
	bidder2 := newAccountAddr(t)
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder2,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 900_000),
		LatencyMs: 800,
	})
	require.Error(t, err, "more expensive bid should be rejected")

	// Best bid should still be bid1
	auctionRec, err = keeper.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	assert.Equal(t, bid1.ID, auctionRec.BestBidID)

	// Third bid at 700k (cheaper) - should become best
	bidder3 := newAccountAddr(t)
	bid3, err := keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder3,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 700_000),
		LatencyMs: 1_500,
	})
	require.NoError(t, err)

	// Best bid should now be bid3
	auctionRec, err = keeper.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	assert.Equal(t, bid3.ID, auctionRec.BestBidID)
}

// TestBestBidSelection_LatencyTiebreaker tests latency breaks price ties.
func TestBestBidSelection_LatencyTiebreaker(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-latency-tie",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// First bid at 500k, 1500ms latency
	bidder1 := newAccountAddr(t)
	bid1, err := keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder1,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_500,
	})
	require.NoError(t, err)

	auctionRec, err := keeper.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	assert.Equal(t, bid1.ID, auctionRec.BestBidID)

	// Second bid at same price but lower latency - should become best
	bidder2 := newAccountAddr(t)
	bid2, err := keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder2,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000, // Lower latency
	})
	require.NoError(t, err)

	// Best bid should now be bid2
	auctionRec, err = keeper.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	assert.Equal(t, bid2.ID, auctionRec.BestBidID)

	// Third bid at same price but higher latency - rejected (does not improve)
	bidder3 := newAccountAddr(t)
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder3,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_200, // Higher than bid2 — not an improvement
	})
	require.Error(t, err, "same price + higher latency should be rejected")

	// Best bid should still be bid2
	auctionRec, err = keeper.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	assert.Equal(t, bid2.ID, auctionRec.BestBidID)
}

// TestBestBidSelection_WinnerAfterFinalize tests finalization records winner.
func TestBestBidSelection_WinnerAfterFinalize(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-winner",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Submit several bids
	bidder1 := newAccountAddr(t)
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder1,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	bidder2 := newAccountAddr(t)
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder2,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 600_000),
		LatencyMs: 1_200,
	})
	require.NoError(t, err)

	// Bid3 must improve on bid2 (600,000). A non-improving bid is rejected.
	bidder3 := newAccountAddr(t)
	bid3, err := keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder3,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000), // Cheaper than bid2
		LatencyMs: 900,
	})
	require.NoError(t, err)

	// Finalize
	finalAuction, winningBid, err := keeper.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// Winner should be bid3 (lowest price after improvement enforcement)
	require.NotNil(t, winningBid)
	assert.Equal(t, bid3.ID, winningBid.ID)
	assert.Equal(t, bid3.ID, finalAuction.WinnerBidID)
	assert.Equal(t, types.AuctionStatusSettled, finalAuction.Status)
}

// TestBestBidSelection_AuctionStateAfterBid verifies auction state update.
func TestBestBidSelection_AuctionStateAfterBid(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-state-update",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Initial state should have empty best bid
	auctionRec, err := keeper.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	assert.Empty(t, auctionRec.BestBidID)
	assert.True(t, auctionRec.BestBidPrice.IsZero())

	// Submit a bid
	bidder := newAccountAddr(t)
	bid, err := keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 750_000),
		LatencyMs: 1_200,
	})
	require.NoError(t, err)

	// Verify auction state is updated
	auctionRec, err = keeper.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	assert.Equal(t, bid.ID, auctionRec.BestBidID)
	assert.Equal(t, sdk.NewInt64Coin(types.DefaultCreditDenom, 750_000), auctionRec.BestBidPrice)
	assert.Equal(t, uint32(1_200), auctionRec.BestBidLatencyMs)
	assert.False(t, auctionRec.BestBidSubmittedAt.IsZero())
}

// TestFinalizeAlreadySettled tests finalizing an already settled auction.
func TestFinalizeAlreadySettled(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-double-finalize",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	bidder := newAccountAddr(t)
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	// First finalize
	_, _, err = keeper.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// Second finalize should not error; returns the previously-winning bid
	finalAuction, winningBid, err := keeper.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	assert.NotNil(t, winningBid) // Winning bid returned on idempotent re-finalize
	assert.Equal(t, types.AuctionStatusSettled, finalAuction.Status)
}
