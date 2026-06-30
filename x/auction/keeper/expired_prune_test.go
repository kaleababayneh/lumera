package keeper

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/auction/types"
)

// ---------------------------------------------------------------------------
// ProcessExpiredAuctions
// ---------------------------------------------------------------------------

func TestProcessExpiredAuctionsEmptyStore(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	require.NoError(t, k.ProcessExpiredAuctions(ctx, 100))
}

func TestProcessExpiredAuctionsNoneExpired(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-not-expired",
		ToolID:       "tool-a",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Don't advance time — auction is still active (TTL = 30s).
	require.NoError(t, k.ProcessExpiredAuctions(ctx, 100))

	// Auction should remain active.
	a, err := k.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusActive, a.Status)
}

func TestProcessExpiredAuctionsFinalizesExpired(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-expire-1",
		ToolID:       "tool-a",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Advance past expiry (default TTL = 30s).
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	require.NoError(t, k.ProcessExpiredAuctions(ctx, 100))

	// Auction should be expired (no bids → expired status).
	a, err := k.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusExpired, a.Status)

	// Active counter should be 0.
	active, err := k.state.ActiveAuctions.Get(ctx)
	if err == nil {
		require.Equal(t, uint64(0), active)
	}

	// Should be indexed for pruning (AuctionsBySettledDate).
	var found bool
	_ = k.state.AuctionsBySettledDate.Walk(ctx, nil, func(key collections.Pair[time.Time, string]) (bool, error) {
		if key.K2() == auction.ID {
			found = true
			return true, nil
		}
		return false, nil
	})
	require.True(t, found, "auction should be in settled-date index after finalization")
}

func TestProcessExpiredAuctionsSettlesWithBid(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-settle-1",
		ToolID:       "tool-b",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000),
		LatencyMs: 1_500,
	})
	require.NoError(t, err)

	// Advance past expiry.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	require.NoError(t, k.ProcessExpiredAuctions(ctx, 100))

	a, err := k.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusSettled, a.Status)
	require.NotEmpty(t, a.WinnerBidID)
}

func TestProcessExpiredAuctionsRespectsLimit(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Create 3 auctions.
	for i := 0; i < 3; i++ {
		_, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-limit-" + string(rune('a'+i)),
			ToolID:       "tool-limit-" + string(rune('a'+i)),
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
			MaxLatencyMs: 2_000,
		})
		require.NoError(t, err)
	}

	// Advance past expiry.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(2 * time.Minute))

	// Process with limit=1 — only 1 should be finalized.
	require.NoError(t, k.ProcessExpiredAuctions(ctx, 1))

	// Count how many are still active.
	activeCount := 0
	_ = k.state.Auctions.Walk(ctx, nil, func(id string, a types.SpotAuction) (bool, error) {
		if a.Status == types.AuctionStatusActive {
			activeCount++
		}
		return false, nil
	})
	require.Equal(t, 2, activeCount, "only 1 of 3 expired auctions should have been finalized")
}

func TestProcessExpiredAuctionsDefaultLimit(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)

	// limit <= 0 should default to 100, not panic.
	require.NoError(t, k.ProcessExpiredAuctions(ctx, 0))
	require.NoError(t, k.ProcessExpiredAuctions(ctx, -1))
}

// ---------------------------------------------------------------------------
// PruneAuctions
// ---------------------------------------------------------------------------

func TestPruneAuctionsEmptyStore(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	require.NoError(t, k.PruneAuctions(ctx, time.Hour, 100))
}

func TestPruneAuctionsRetentionNotElapsed(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-prune-young",
		ToolID:       "tool-prune",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Advance past expiry and finalize.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Minute))
	_, _, err = k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// Prune with retention=1h. Auction was settled < 1h ago — should survive.
	require.NoError(t, k.PruneAuctions(ctx, time.Hour, 100))

	_, err = k.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err, "auction should still exist — retention not elapsed")
}

func TestPruneAuctionsDeletesOldSettled(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-prune-old",
		ToolID:       "tool-prune-old",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Advance past expiry and finalize.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Minute))
	_, _, err = k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// Advance well past retention period.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(3 * time.Hour))

	require.NoError(t, k.PruneAuctions(ctx, time.Hour, 100))

	// Auction record should be removed.
	_, err = k.state.Auctions.Get(ctx, auction.ID)
	require.Error(t, err, "auction should have been pruned")
}

func TestPruneAuctionsDeletesBids(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)
	bidder := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-prune-bids",
		ToolID:       "tool-prune-bids",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	bid, err := k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000),
		LatencyMs: 1_500,
	})
	require.NoError(t, err)

	// Finalize (settles with bid).
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Minute))
	_, _, err = k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// Advance past retention.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(3 * time.Hour))

	require.NoError(t, k.PruneAuctions(ctx, time.Hour, 100))

	// Bid should also be deleted.
	_, err = k.state.Bids.Get(ctx, bid.ID)
	require.Error(t, err, "bid should have been pruned along with auction")
}

func TestPruneAuctionsRespectsLimit(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Create and finalize 3 auctions.
	var ids []string
	for i := 0; i < 3; i++ {
		a, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-pl-" + string(rune('a'+i)),
			ToolID:       "tool-pl-" + string(rune('a'+i)),
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
			MaxLatencyMs: 2_000,
		})
		require.NoError(t, err)
		ids = append(ids, a.ID)
	}

	// Finalize all.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Minute))
	for _, id := range ids {
		_, _, err := k.FinalizeSpotAuction(ctx, id)
		require.NoError(t, err)
	}

	// Advance past retention.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(3 * time.Hour))

	// Prune with limit=1.
	require.NoError(t, k.PruneAuctions(ctx, time.Hour, 1))

	// At least 2 should still exist.
	remaining := 0
	_ = k.state.Auctions.Walk(ctx, nil, func(id string, a types.SpotAuction) (bool, error) {
		remaining++
		return false, nil
	})
	require.GreaterOrEqual(t, remaining, 2, "limit=1 should only prune 1 auction")
}

func TestPruneAuctionsDefaultLimit(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	require.NoError(t, k.PruneAuctions(ctx, time.Hour, 0))
	require.NoError(t, k.PruneAuctions(ctx, time.Hour, -1))
}

func TestPruneAuctionsRemovesSettledIndex(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-idx-clean",
		ToolID:       "tool-idx-clean",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Minute))
	_, _, err = k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// Verify settled index entry exists before prune.
	var foundBefore bool
	_ = k.state.AuctionsBySettledDate.Walk(ctx, nil, func(key collections.Pair[time.Time, string]) (bool, error) {
		if key.K2() == auction.ID {
			foundBefore = true
			return true, nil
		}
		return false, nil
	})
	require.True(t, foundBefore, "settled index should exist before prune")

	// Advance past retention and prune.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(3 * time.Hour))
	require.NoError(t, k.PruneAuctions(ctx, time.Hour, 100))

	// Settled index entry should be gone.
	var foundAfter bool
	_ = k.state.AuctionsBySettledDate.Walk(ctx, nil, func(key collections.Pair[time.Time, string]) (bool, error) {
		if key.K2() == auction.ID {
			foundAfter = true
			return true, nil
		}
		return false, nil
	})
	require.False(t, foundAfter, "settled index should be cleaned up after prune")
}

// Regression for lumera_ai-auc01: an auction whose ExpiresAt equals the current
// block time exactly must be processed in the same block, not silently deferred
// to the next. Before the fix, the range used EndInclusive((now, "")) which
// excluded every pair (now, auction_id) since auction_id > "".
func TestProcessExpiredAuctionsIncludesExactBlockTime(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-exact-time",
		ToolID:       "tool-a",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Advance block time to exactly the auction's ExpiresAt — the boundary
	// case that the buggy range query missed.
	ctx = ctx.WithBlockTime(auction.ExpiresAt)

	require.NoError(t, k.ProcessExpiredAuctions(ctx, 100))

	a, err := k.state.Auctions.Get(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusExpired, a.Status,
		"auction expiring at exactly block time must be finalized in this block")
}

func TestPruneAuctionsAtExactCutoffBoundary(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-prune-boundary",
		ToolID:       "tool-prune-boundary",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Finalize the auction so it enters AuctionsBySettledDate.
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(time.Minute))
	_, _, err = k.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)

	// Advance block time so BlockTime - retention equals the settlement time
	// exactly — this is the boundary the old EndInclusive((cutoff, "")) range
	// would have skipped because a non-empty auction ID sorts after "".
	retention := time.Hour
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(retention))

	require.NoError(t, k.PruneAuctions(ctx, retention, 100))

	_, err = k.state.Auctions.Get(ctx, auction.ID)
	require.Error(t, err, "auction whose settlement time equals the exact cutoff must be pruned")
}
