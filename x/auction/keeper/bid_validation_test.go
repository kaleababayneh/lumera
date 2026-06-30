package keeper

import (
	"strings"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/auction/types"
)

// TestBidValidation_DuplicateBidder tests that same bidder cannot bid twice.
func TestBidValidation_DuplicateBidder(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	// Create auction
	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-dup-bidder",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// First bid succeeds
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	// Second bid from same bidder fails
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000),
		LatencyMs: 900,
	})
	require.ErrorIs(t, err, types.ErrBidDuplicate)
}

// TestBidValidation_ExpiredAuction tests bidding on an expired auction.
func TestBidValidation_ExpiredAuction(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	// Create auction
	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-expired",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Advance time past expiry (default TTL is 30 seconds)
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	// Bid should fail
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.ErrorIs(t, err, types.ErrAuctionExpired)
}

// TestBidValidation_ClosedAuction tests bidding on a settled auction.
func TestBidValidation_ClosedAuction(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder1 := newAccountAddr(t)
	bidder2 := newAccountAddr(t)

	// Create auction
	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-closed",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Submit a bid
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder1,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	// Finalize the auction
	_, _, err = keeper.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// New bid should fail
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder2,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000),
		LatencyMs: 800,
	})
	require.ErrorIs(t, err, types.ErrAuctionClosed)
}

// TestBidValidation_TooExpensive tests bid exceeding max price.
func TestBidValidation_TooExpensive(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	// Create auction with low max price
	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-expensive",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Bid with price higher than max
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 200_000),
		LatencyMs: 1_000,
	})
	require.ErrorIs(t, err, types.ErrBidTooExpensive)
}

// TestBidValidation_LatencyExceeded tests bid with latency > max.
func TestBidValidation_LatencyExceeded(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	// Create auction with low max latency
	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-latency",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 500,
	})
	require.NoError(t, err)

	// Bid with latency exceeding max
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 600,
	})
	require.ErrorIs(t, err, types.ErrBidLatencyExceeded)
}

// TestBidValidation_ZeroLatency tests bid with zero latency.
func TestBidValidation_ZeroLatency(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-zero-latency",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Bid with zero latency should fail
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 0,
	})
	require.ErrorIs(t, err, types.ErrBidLatencyExceeded)
}

// TestBidValidation_InvalidDenom tests bid with wrong denomination.
func TestBidValidation_InvalidDenom(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-denom",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Bid with wrong denom
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin("wrongdenom", 500_000),
		LatencyMs: 1_000,
	})
	require.ErrorIs(t, err, types.ErrBidInvalidDenom)
}

// TestBidValidation_InvalidBidderAddress tests bid with invalid address.
func TestBidValidation_InvalidBidderAddress(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-invalid-addr",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Bid with invalid address
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    "invalid-address",
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid bidder address")
}

// TestAuctionLimit_MaxActiveReached tests auction creation when limit reached.
func TestAuctionLimit_MaxActiveReached(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Set very low max active auctions
	params := types.Params{
		CreditDenom:              types.DefaultCreditDenom,
		DefaultAuctionTTLSeconds: 30,
		MaxActiveAuctions:        2,
		MinBidDecrementBps:       100,
		MaxBidLatencyMs:          5_000,
	}
	require.NoError(t, keeper.SetParams(ctx, &params))

	// Create first auction - succeeds
	_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-limit-1",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Create second auction - succeeds
	_, err = keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-limit-2",
		ToolID:       "tool-2",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Create third auction - should fail
	_, err = keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-limit-3",
		ToolID:       "tool-3",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "active auction limit reached")
}

// TestCreateAuction_ValidationErrors tests various auction creation validation.
func TestCreateAuction_ValidationErrors(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	t.Run("empty request id", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "",
			ToolID:       "tool-1",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 2_000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "request id required")
	})

	t.Run("invalid owner", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        "not-a-valid-address",
			RequestID:    "req-invalid-owner",
			ToolID:       "tool-1",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 2_000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner address")
	})

	t.Run("empty tool id", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-empty-tool",
			ToolID:       "",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 2_000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool id required")
	})

	t.Run("padded request id", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    " req-padded",
			ToolID:       "tool-1",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 2_000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "request id")
		assert.Contains(t, err.Error(), "whitespace")
	})

	t.Run("padded tool id", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-padded-tool",
			ToolID:       "tool-1 ",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 2_000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool id")
		assert.Contains(t, err.Error(), "whitespace")
	})

	t.Run("oversized request id", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    strings.Repeat("r", 257),
			ToolID:       "tool-1",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 2_000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "request id")
		assert.Contains(t, err.Error(), "exceeds")
	})

	t.Run("oversized tool id", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-oversized-tool",
			ToolID:       strings.Repeat("t", 257),
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 2_000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool id")
		assert.Contains(t, err.Error(), "exceeds")
	})

	t.Run("invalid denom", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-invalid-denom",
			ToolID:       "tool-1",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin("wrongdenom", 1_000_000),
			MaxLatencyMs: 2_000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "denom must be")
	})

	t.Run("zero max price", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-zero-price",
			ToolID:       "tool-1",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 0),
			MaxLatencyMs: 2_000,
		})
		require.Error(t, err)
	})

	t.Run("zero max latency", func(t *testing.T) {
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-zero-latency",
			ToolID:       "tool-1",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 0,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "max latency must be")
	})

	t.Run("max latency exceeds global cap", func(t *testing.T) {
		// Default MaxBidLatencyMs is 5000
		_, err := keeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-latency-cap",
			ToolID:       "tool-1",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 10_000, // Exceeds default 5000
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds global cap")
	})
}

func TestSubmitSpotBid_RejectsOversizedAuctionID(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)

	_, err := keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: strings.Repeat("a", 257),
		Bidder:    newAccountAddr(t),
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 100),
		LatencyMs: 100,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auction id")
	assert.Contains(t, err.Error(), "exceeds")
}
