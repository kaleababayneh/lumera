package keeper

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/auction/types"
)

// =============================================================================
// Block Time Validation Tests
// =============================================================================

func TestCreateSpotAuction_ZeroBlockTime(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Set block time to zero
	ctx = ctx.WithBlockTime(time.Time{})

	_, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-zero-time",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "block time must be set")
}

func TestSubmitSpotBid_ZeroBlockTime(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	// Create auction with valid time
	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-bid-zero-time",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Set block time to zero for bid
	ctx = ctx.WithBlockTime(time.Time{})

	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "block time must be set")
}

func TestFinalizeSpotAuction_ZeroBlockTime(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Create auction with valid time
	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-finalize-zero-time",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Set block time to zero for finalize
	ctx = ctx.WithBlockTime(time.Time{})

	_, _, err = k.FinalizeSpotAuction(ctx, auction.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "block time must be set")
}

// =============================================================================
// JSON Codec Edge Cases
// =============================================================================

func TestJSONValueCodec_EmptyBytes(t *testing.T) {
	codec := newJSONCodec[types.SpotAuction]()

	// Decode empty bytes should return zero value
	result, err := codec.Decode([]byte{})
	require.NoError(t, err)
	assert.Equal(t, "", result.ID)
}

func TestJSONValueCodec_InvalidJSON(t *testing.T) {
	codec := newJSONCodec[types.SpotAuction]()

	_, err := codec.Decode([]byte("not valid json"))
	require.Error(t, err)
}

func TestJSONValueCodec_Stringify_Valid(t *testing.T) {
	codec := newJSONCodec[types.SpotAuction]()
	auction := types.SpotAuction{
		ID:     "auc-test",
		Owner:  "owner-test",
		ToolID: "tool-test",
	}

	str := codec.Stringify(auction)
	assert.Contains(t, str, "auc-test")
	assert.Contains(t, str, "owner-test")
}

func TestJSONValueCodec_ValueType(t *testing.T) {
	codec := newJSONCodec[types.SpotAuction]()
	vt := codec.ValueType()
	assert.Contains(t, vt, "SpotAuction")
}

// =============================================================================
// GetBid Edge Cases
// =============================================================================

func TestGetBid_Success(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-get-bid",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	bid, err := k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	retrieved, err := k.GetBid(ctx, bid.ID)
	require.NoError(t, err)
	assert.Equal(t, bid.ID, retrieved.ID)
	assert.Equal(t, bidder, retrieved.Bidder)
}

// =============================================================================
// Bid Improvement Tests
// =============================================================================

func TestSubmitSpotBid_MustImproveBestBid(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder1 := newAccountAddr(t)
	bidder2 := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-improve",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// First bid
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder1,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	// Second bid that does NOT improve (same price, worse latency)
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder2,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_500, // Worse latency
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrInvalidBid)
	assert.Contains(t, err.Error(), "does not improve")
}

func TestSubmitSpotBid_SamePriceBetterLatencySucceeds(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder1 := newAccountAddr(t)
	bidder2 := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-same-price-better-lat",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// First bid
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder1,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_500,
	})
	require.NoError(t, err)

	// Second bid with same price but better latency should succeed
	bid2, err := k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder2,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000, // Better latency
	})
	require.NoError(t, err)
	require.NotNil(t, bid2)
}

// =============================================================================
// Auction Latency Validation
// =============================================================================

func TestCreateSpotAuction_LatencyExceedsGlobalCap(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Default MaxBidLatencyMs is 5000
	_, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-high-latency",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 10_000, // Exceeds global cap
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds global cap")
}

func TestSubmitSpotBid_LatencyExceedsAuctionMax(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-bid-latency",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 1_000, // Auction max latency
	})
	require.NoError(t, err)

	// Bid with latency exceeding auction max
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_500, // Exceeds auction max of 1000
	})
	require.ErrorIs(t, err, types.ErrBidLatencyExceeded)
}

func TestSubmitSpotBid_ZeroLatency(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-zero-latency",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 0, // Zero is invalid
	})
	require.ErrorIs(t, err, types.ErrBidLatencyExceeded)
}

// =============================================================================
// Bid Price Validation
// =============================================================================

func TestSubmitSpotBid_ExceedsMaxPrice(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-max-price",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 600_000), // Exceeds max
		LatencyMs: 1_000,
	})
	require.ErrorIs(t, err, types.ErrBidTooExpensive)
}

func TestSubmitSpotBid_InvalidBidderAddress(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-invalid-bidder",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    "not-a-valid-address",
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid bidder address")
}

func TestSubmitSpotBid_ZeroPrice(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-zero-price",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 0),
		LatencyMs: 1_000,
	})
	require.ErrorIs(t, err, types.ErrInvalidBid)
}

// =============================================================================
// Finalize Already Finalized Auction (Idempotency)
// =============================================================================

func TestFinalizeSpotAuction_AlreadySettled(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-already-settled",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Submit bid
	bid, err := k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	// Finalize first time
	finalAuction1, winningBid1, err := k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusSettled, finalAuction1.Status)
	require.NotNil(t, winningBid1)
	require.Equal(t, bid.ID, winningBid1.ID)

	// Finalize again (idempotent)
	finalAuction2, winningBid2, err := k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusSettled, finalAuction2.Status)
	require.NotNil(t, winningBid2)
	require.Equal(t, bid.ID, winningBid2.ID)
}

func TestFinalizeSpotAuction_AlreadyExpired(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-already-expired",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Advance time past expiry
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	// Finalize first time (no bids -> expired)
	finalAuction1, _, err := k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusExpired, finalAuction1.Status)

	// Finalize again (idempotent)
	finalAuction2, _, err := k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusExpired, finalAuction2.Status)
}

// =============================================================================
// Prune Auctions Edge Cases
// =============================================================================

func TestPruneAuctions_EmptyStore(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	err := k.PruneAuctions(ctx, 24*time.Hour, 100)
	require.NoError(t, err)
}

func TestPruneAuctions_NothingOldEnough(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Create and finalize an auction
	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-prune-recent",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Advance and finalize
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))
	_, _, err = k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// Prune with 1 hour retention - auction is only 2 minutes old
	err = k.PruneAuctions(ctx, time.Hour, 100)
	require.NoError(t, err)

	// Auction should still exist
	_, err = k.GetAuction(ctx, auction.ID)
	require.NoError(t, err)
}

func TestPruneAuctions_RemovesOldAuctions(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	// Create and finalize an auction
	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-prune-old",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Submit bid
	bid, err := k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	// Advance and finalize
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))
	_, _, err = k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// Advance time way past retention period
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(48 * time.Hour))

	// Prune with 24 hour retention
	err = k.PruneAuctions(ctx, 24*time.Hour, 100)
	require.NoError(t, err)

	// Auction should be pruned
	_, err = k.GetAuction(ctx, auction.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrAuctionNotFound)

	// Bid should also be pruned
	_, err = k.GetBid(ctx, bid.ID)
	require.Error(t, err)
}

func TestPruneAuctions_RespectsLimit(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Create and finalize multiple auctions
	for i := 0; i < 3; i++ {
		auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    fmt.Sprintf("req-prune-limit-%d", i),
			ToolID:       fmt.Sprintf("tool-%d", i),
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
			MaxLatencyMs: 2_000,
		})
		require.NoError(t, err)

		// Advance and finalize
		ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))
		_, _, err = k.FinalizeSpotAuction(ctx, auction.ID)
		require.NoError(t, err)
	}

	// Advance time way past retention period
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(48 * time.Hour))

	// Prune with limit=1
	err := k.PruneAuctions(ctx, 24*time.Hour, 1)
	require.NoError(t, err)

	// Only 1 should be pruned, count remaining
	auctions, err := k.GetAllAuctions(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, len(auctions))
}

// =============================================================================
// ProcessExpiredAuctions Edge Cases
// =============================================================================

func TestProcessExpiredAuctions_DefaultLimit(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)

	// Zero limit should use default (100)
	err := k.ProcessExpiredAuctions(ctx, 0)
	require.NoError(t, err)

	// Negative limit should also use default
	err = k.ProcessExpiredAuctions(ctx, -1)
	require.NoError(t, err)
}

func TestProcessExpiredAuctions_OrphanedExpiryIndex(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Create an auction
	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-orphan",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Manually remove the auction from Auctions collection but leave expiry index
	err = k.state.Auctions.Remove(ctx, auction.ID)
	require.NoError(t, err)

	// Advance time past expiry
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	// ProcessExpiredAuctions should handle the orphaned index gracefully
	err = k.ProcessExpiredAuctions(ctx, 100)
	require.NoError(t, err)

	// The orphaned index should be cleaned up
	has, err := k.state.AuctionsByExpiry.Has(ctx, collections.Join(auction.ExpiresAt, auction.ID))
	require.NoError(t, err)
	assert.False(t, has)
}

// =============================================================================
// Apply Discount Edge Cases
// =============================================================================

func TestApplyDiscount_OverMaxBps(t *testing.T) {
	// Discount bps > 10000 should be clamped to 10000 (100%)
	coin := sdk.NewInt64Coin("ulac", 10_000)
	result := applyDiscount(coin, 15_000) // 150% discount -> clamped to 100%
	assert.True(t, result.IsZero())
}

func TestApplyDiscount_SmallAmount(t *testing.T) {
	// Small amounts with small discounts
	coin := sdk.NewInt64Coin("ulac", 1)
	result := applyDiscount(coin, 100) // 1% discount
	// 1 * 9900 / 10000 = 0 (truncated)
	assert.True(t, result.IsZero())
}

// =============================================================================
// Auction TTL Edge Cases
// =============================================================================

func TestCreateSpotAuction_DefaultTTL(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Default TTL should be applied
	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-default-ttl",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// ExpiresAt should be CreatedAt + 30 seconds (default)
	expectedExpiry := auction.CreatedAt.Add(30 * time.Second)
	assert.Equal(t, expectedExpiry, auction.ExpiresAt)
}

// =============================================================================
// Negative Price Amount Tests
// =============================================================================

func TestCreateSpotAuction_NegativeMaxPrice(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	_, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-neg-price",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.Coin{Denom: types.DefaultCreditDenom, Amount: sdkmath.NewInt(-100)},
		MaxLatencyMs: 2_000,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}
