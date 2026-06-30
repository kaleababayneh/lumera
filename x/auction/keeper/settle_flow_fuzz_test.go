package keeper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/auction/types"
	reservetypes "github.com/LumeraProtocol/lumera/x/reserve/types"
)

const maxSettleFlowFuzzAmount int64 = 1_000_000

// FuzzFinalizeSpotAuctionStateTransitions pins the SpotCall settlement state
// machine. An active auction may settle to a winning bid, settle through reserve
// fallback, or expire without capacity; once terminal, a second finalize call is
// idempotent and must not underflow active-auction accounting.
func FuzzFinalizeSpotAuctionStateTransitions(f *testing.F) {
	for _, seed := range []struct {
		mode       uint8
		maxPrice   int64
		bidPrice   int64
		maxLatency uint32
		bidLatency uint32
	}{
		{0, 100_000, 50_000, 1_000, 500},   // no bid, no reserve -> expired
		{1, 100_000, 50_000, 1_000, 500},   // bid -> settled
		{2, 300_000, 50_000, 2_000, 1_000}, // reserve -> settled
		{3, 300_000, 50_000, 2_000, 1_000}, // bid beats reserve fallback
		{7, 1_000_000, 1, 5_000, 1},        // expired-time finalize remains terminal
	} {
		f.Add(seed.mode, seed.maxPrice, seed.bidPrice, seed.maxLatency, seed.bidLatency)
	}

	f.Fuzz(func(t *testing.T, mode uint8, rawMaxPrice, rawBidPrice int64, rawMaxLatency, rawBidLatency uint32) {
		hasBid := mode&1 != 0
		withReserve := mode&2 != 0
		advancePastExpiry := mode&4 != 0

		ctx, auctionKeeper, reserveKeeper, _ := setupAuctionKeeperBase(t, withReserve, false)
		owner := newAccountAddr(t)
		maxPrice := settleFlowAmount(rawMaxPrice)
		bidPrice := settleFlowBidAmount(rawBidPrice, maxPrice)
		maxLatency := settleFlowLatency(rawMaxLatency, 5_000)
		bidLatency := settleFlowLatency(rawBidLatency, maxLatency)
		maxPriceCoin := sdk.NewInt64Coin(types.DefaultCreditDenom, maxPrice)

		const (
			policyID = "policy-settle-flow"
			toolID   = "tool-settle-flow"
		)
		if withReserve {
			_, err := reserveKeeper.CreateCommitment(ctx, reservetypes.ReserveRequest{
				Owner:    owner,
				PolicyID: policyID,
				ToolID:   toolID,
				Tier:     "silver",
				Amount:   sdk.NewInt64Coin(reservetypes.DefaultCreditDenom, maxSettleFlowFuzzAmount),
				Duration: time.Hour,
			})
			require.NoError(t, err)
		}

		auction, err := auctionKeeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    "req-settle-flow",
			ToolID:       toolID,
			PolicyID:     policyID,
			MaxPrice:     maxPriceCoin,
			MaxLatencyMs: maxLatency,
		})
		require.NoError(t, err)
		require.Equal(t, types.AuctionStatusActive, auction.Status)

		active, err := auctionKeeper.getActiveCount(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(1), active)

		var acceptedBid *types.SpotBid
		if hasBid {
			acceptedBid, err = auctionKeeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
				AuctionID: auction.ID,
				Bidder:    newAccountAddr(t),
				Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, bidPrice),
				LatencyMs: bidLatency,
			})
			require.NoError(t, err)
		}

		if advancePastExpiry {
			ctx = ctx.WithBlockTime(auction.ExpiresAt.Add(time.Nanosecond))
		}

		finalAuction, winningBid, err := auctionKeeper.FinalizeSpotAuction(ctx, auction.ID)
		require.NoError(t, err)
		requireTerminalSettleFlow(t, finalAuction)

		if hasBid {
			require.Equal(t, types.AuctionStatusSettled, finalAuction.Status)
			require.NotNil(t, winningBid)
			require.Equal(t, acceptedBid.ID, winningBid.ID)
			require.Equal(t, acceptedBid.ID, finalAuction.WinnerBidID)
			require.False(t, finalAuction.ReserveApplied)
			require.True(t, finalAuction.BestBidPrice.Equal(acceptedBid.Price))
			require.Equal(t, acceptedBid.LatencyMs, finalAuction.BestBidLatencyMs)
		} else if withReserve {
			require.Equal(t, types.AuctionStatusSettled, finalAuction.Status)
			require.Nil(t, winningBid)
			require.True(t, finalAuction.ReserveApplied)
			require.NotEmpty(t, finalAuction.ReserveCommitmentID)
			require.Equal(t, finalAuction.ReserveCommitmentID, finalAuction.WinnerBidID)
			require.Equal(t, maxLatency, finalAuction.BestBidLatencyMs)
			reserveParams, err := reserveKeeper.GetParams(ctx)
			require.NoError(t, err)
			tier, found := reserveParams.FindTier("silver")
			require.True(t, found)
			expectedReservePrice := reservetypes.ApplyDiscount(maxPriceCoin, tier.DiscountBps)
			require.True(t, finalAuction.BestBidPrice.Equal(expectedReservePrice))
			require.False(t, finalAuction.BestBidPrice.Amount.IsNegative())
		} else {
			require.Equal(t, types.AuctionStatusExpired, finalAuction.Status)
			require.Nil(t, winningBid)
			require.Empty(t, finalAuction.WinnerBidID)
			require.False(t, finalAuction.ReserveApplied)
			require.True(t, finalAuction.BestBidPrice.IsZero())
		}

		active, err = auctionKeeper.getActiveCount(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(0), active)

		indexed, err := auctionKeeper.state.AuctionByRequest.Has(ctx, "req-settle-flow")
		require.NoError(t, err)
		require.False(t, indexed)

		again, secondWinningBid, err := auctionKeeper.FinalizeSpotAuction(ctx, auction.ID)
		require.NoError(t, err)
		require.Equal(t, finalAuction.Status, again.Status)
		require.Equal(t, finalAuction.WinnerBidID, again.WinnerBidID)
		require.True(t, finalAuction.BestBidPrice.Equal(again.BestBidPrice))
		if hasBid {
			require.NotNil(t, secondWinningBid)
			require.Equal(t, acceptedBid.ID, secondWinningBid.ID)
		} else {
			require.Nil(t, secondWinningBid)
		}

		active, err = auctionKeeper.getActiveCount(ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(0), active)
	})
}

// TestFinalizeSpotAuction_ReserveExistsButNotApplicableExpires pins the
// branch at spotcall.go:260 where `reserveKeeper.AllocateReserve` returns
// `allocation.Applied == false` — the auction has NO bid AND a reserve
// commitment exists in state but cannot be allocated (stale-denom,
// depleted, or expired commitment). Production falls through to
// AuctionStatusExpired at spotcall.go:269-270.
//
// FuzzFinalizeSpotAuctionStateTransitions (this file, seed modes 0-3)
// covers:
//   - mode=0: no bid, no reserve        → Expired
//   - mode=1: bid, no reserve           → Settled via bid
//   - mode=2: no bid, applicable reserve → Settled via reserve
//   - mode=3: bid, applicable reserve   → Settled via bid (reserve unused)
//
// The missing branch is "no bid, reserve commitment EXISTS but NOT
// applicable" → Expired. The fuzz can't reach it because its reserve
// setup always produces an allocatable commitment (fresh denom, ample
// amount, unexpired). This targeted test exercises the stale-denom
// path: create commitment under denom A, flip reserve params to denom
// B, create auction priced in denom B → AllocateReserve skips the
// A-denom commitment (keeper.go:322-324 stale-denom filter) and
// returns Applied=false → auction expires.
//
// Consensus-critical: if the stale-denom filter silently didn't skip,
// AllocateReserve would panic on sdk.Coin arithmetic with mismatched
// denoms (fixed in 1ec024276's sibling module). Without this test
// pinning the fall-through-to-Expired behavior, a regression that
// e.g. returns a nil error with Applied=true but zero discounted
// price would silently settle auctions with broken economics.
func TestFinalizeSpotAuction_ReserveExistsButNotApplicableExpires(t *testing.T) {
	t.Parallel()
	ctx, auctionKeeper, reserveKeeper, _ := setupAuctionKeeperBase(t, true, false)
	owner := newAccountAddr(t)

	const (
		policyID = "policy-reserve-stale"
		toolID   = "tool-reserve-stale"
	)

	// Step 1: create reserve commitment under the ORIGINAL credit denom
	// (DefaultCreditDenom). This is a valid applicable commitment at
	// creation time.
	_, err := reserveKeeper.CreateCommitment(ctx, reservetypes.ReserveRequest{
		Owner:    owner,
		PolicyID: policyID,
		ToolID:   toolID,
		Tier:     "silver",
		Amount:   sdk.NewInt64Coin(reservetypes.DefaultCreditDenom, 1_000_000_000),
		Duration: time.Hour,
	})
	require.NoError(t, err, "setup: original-denom commitment creation")

	// Step 2: governance-style flip of reserve params to a new credit
	// denom. The pre-existing commitment now carries a STALE denom
	// relative to params. AllocateReserve will skip it (per the
	// stale-denom filter landed in 1ec024276).
	reserveParams, err := reserveKeeper.GetParams(ctx)
	require.NoError(t, err)
	reserveParams.CreditDenom = "stale"
	require.NoError(t, reserveKeeper.SetParams(ctx, reserveParams))

	// Step 3: create auction with MaxPrice in the NEW denom. Auction
	// creation itself doesn't touch the reserve commitment; MaxPrice's
	// denom only matters when AllocateReserve runs at finalize time.
	// Using types.DefaultCreditDenom here means the auction-side
	// price denom happens to match reserve's OLD denom — but the
	// commitment check in AllocateReserve gates on reserve.params.
	// CreditDenom (now "stale"), not on the request denom, so the
	// pre-existing "ulume" commitment is filtered.
	auction, err := auctionKeeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-reserve-stale",
		ToolID:       toolID,
		PolicyID:     policyID,
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000),
		MaxLatencyMs: 1_000,
	})
	require.NoError(t, err)
	require.Equal(t, types.AuctionStatusActive, auction.Status)

	// Step 4: finalize with no bid submitted. Production code path:
	//   - auction.BestBidID == "" (no bid)
	//   - k.reserveKeeper != nil (set via setupAuctionKeeperBase)
	//   - AllocateReserve(...) walks commitments for (owner, policyID,
	//     toolID); each one's RemainingAmount.Denom ("ulume") is
	//     compared against params.CreditDenom ("stale") → no match → skip
	//   - allocation.Applied=false is returned
	//   - spotcall.go:269-270 sets auction.Status = Expired
	finalAuction, winningBid, err := auctionKeeper.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err, "finalize with stale-denom reserve must not error")

	require.Equal(t, types.AuctionStatusExpired, finalAuction.Status,
		"reserve commitment exists but is NOT applicable (stale denom) → "+
			"auction MUST fall through to Expired per spotcall.go:269-270. "+
			"If Status is Settled, reserve-fallback silently ignored the "+
			"denom mismatch and settled on a commitment that AllocateReserve "+
			"should have refused — breaks audit trails and economic "+
			"invariants.")
	require.Nil(t, winningBid,
		"no winning bid when reserve fallback fails")
	require.Empty(t, finalAuction.WinnerBidID,
		"no winner ID when reserve not applicable")
	require.False(t, finalAuction.ReserveApplied,
		"ReserveApplied must stay false — no allocation occurred")
	require.Empty(t, finalAuction.ReserveCommitmentID,
		"ReserveCommitmentID must stay empty")
	require.True(t, finalAuction.BestBidPrice.IsZero(),
		"BestBidPrice must be zero-coin when expired")

	// Active-count cleanup: even on the Expired path, the auction must
	// be decremented from active tracking.
	active, err := auctionKeeper.getActiveCount(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(0), active,
		"active-count must be decremented on any terminal transition")

	// Idempotent re-finalize: a second call on a terminal auction must
	// return the same state without mutating.
	again, _, err := auctionKeeper.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	require.Equal(t, finalAuction.Status, again.Status,
		"re-finalize on terminal Expired must be idempotent")
}

func requireTerminalSettleFlow(t *testing.T, auction *types.SpotAuction) {
	t.Helper()

	require.NotNil(t, auction)
	require.NoError(t, auction.ValidateBasic())
	require.NotEqual(t, types.AuctionStatusActive, auction.Status)
	require.True(t, auction.ExpiresAt.Equal(auction.BestBidSubmittedAt) || !auction.ExpiresAt.Before(auction.CreatedAt))
}

func settleFlowAmount(raw int64) int64 {
	return settleFlowPositiveModulo(raw, maxSettleFlowFuzzAmount)
}

func settleFlowBidAmount(raw, maxPrice int64) int64 {
	return settleFlowPositiveModulo(raw, maxPrice)
}

func settleFlowLatency(raw, max uint32) uint32 {
	if max == 0 {
		max = 1
	}
	return raw%max + 1
}

func settleFlowPositiveModulo(raw, max int64) int64 {
	if max <= 0 {
		return 1
	}
	var value uint64
	if raw < 0 {
		value = uint64(-(raw + 1)) + 1
	} else {
		value = uint64(raw)
	}
	return int64(value%uint64(max)) + 1
}
