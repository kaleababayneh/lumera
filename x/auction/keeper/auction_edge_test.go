package keeper

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/auction/types"
	prioritytypes "github.com/LumeraProtocol/lumera/x/priority/types"
)

func validAuctionRequestOwner() string {
	return sdk.AccAddress([]byte("auction-edge-owner01")).String()
}

// ============================================================
// applyDiscount
// ============================================================

func TestApplyDiscount_ZeroBps(t *testing.T) {
	coin := sdk.NewInt64Coin("ulac", 10_000)
	result := applyDiscount(coin, 0)
	assert.Equal(t, coin, result)
}

func TestApplyDiscount_250Bps(t *testing.T) {
	// 250 bps = 2.5% discount → 10000 * 9750 / 10000 = 9750
	coin := sdk.NewInt64Coin("ulac", 10_000)
	result := applyDiscount(coin, 250)
	assert.Equal(t, sdk.NewInt64Coin("ulac", 9_750), result)
}

func TestApplyDiscount_5000Bps(t *testing.T) {
	// 5000 bps = 50% discount → 10000 * 5000 / 10000 = 5000
	coin := sdk.NewInt64Coin("ulac", 10_000)
	result := applyDiscount(coin, 5000)
	assert.Equal(t, sdk.NewInt64Coin("ulac", 5_000), result)
}

func TestApplyDiscount_9999Bps(t *testing.T) {
	// 9999 bps = 99.99% discount → 10000 * 1 / 10000 = 1
	coin := sdk.NewInt64Coin("ulac", 10_000)
	result := applyDiscount(coin, 9999)
	assert.Equal(t, sdk.NewInt64Coin("ulac", 1), result)
}

func TestApplyDiscount_10000Bps(t *testing.T) {
	// 10000 bps = 100% discount → 0
	coin := sdk.NewInt64Coin("ulac", 10_000)
	result := applyDiscount(coin, 10_000)
	assert.True(t, result.IsZero())
}

func TestApplyDiscount_LargeAmount(t *testing.T) {
	// Ensure no overflow with large amounts
	coin := sdk.NewCoin("ulac", sdkmath.NewInt(1_000_000_000_000))
	result := applyDiscount(coin, 100) // 1% discount
	expected := sdk.NewCoin("ulac", sdkmath.NewInt(990_000_000_000))
	assert.Equal(t, expected, result)
}

// ============================================================
// validateCreateRequest
// ============================================================

func TestValidateCreateRequest_EmptyRequestID(t *testing.T) {
	params := types.DefaultParams()
	req := types.CreateAuctionRequest{
		Owner:        validAuctionRequestOwner(),
		RequestID:    "",
		ToolID:       "tool-1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1000),
		MaxLatencyMs: 500,
	}
	err := validateCreateRequest(params, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request id required")
}

func TestValidateCreateRequest_EmptyToolID(t *testing.T) {
	params := types.DefaultParams()
	req := types.CreateAuctionRequest{
		Owner:        validAuctionRequestOwner(),
		RequestID:    "req-1",
		ToolID:       "",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1000),
		MaxLatencyMs: 500,
	}
	err := validateCreateRequest(params, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool id required")
}

func TestValidateCreateRequest_PaddedIdentifiers(t *testing.T) {
	params := types.DefaultParams()
	tests := []struct {
		name    string
		mutate  func(*types.CreateAuctionRequest)
		wantErr string
	}{
		{
			name: "request id",
			mutate: func(req *types.CreateAuctionRequest) {
				req.RequestID = " req-1"
			},
			wantErr: "request id",
		},
		{
			name: "tool id",
			mutate: func(req *types.CreateAuctionRequest) {
				req.ToolID = "tool-1 "
			},
			wantErr: "tool id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := types.CreateAuctionRequest{
				Owner:        validAuctionRequestOwner(),
				RequestID:    "req-1",
				ToolID:       "tool-1",
				MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1000),
				MaxLatencyMs: 500,
			}
			tc.mutate(&req)

			err := validateCreateRequest(params, req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "whitespace")
		})
	}
}

func TestValidateCreateRequest_WrongDenom(t *testing.T) {
	params := types.DefaultParams()
	req := types.CreateAuctionRequest{
		Owner:        validAuctionRequestOwner(),
		RequestID:    "req-1",
		ToolID:       "tool-1",
		MaxPrice:     sdk.NewInt64Coin("uatom", 1000),
		MaxLatencyMs: 500,
	}
	err := validateCreateRequest(params, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denom must be")
}

func TestValidateCreateRequest_ZeroPrice(t *testing.T) {
	params := types.DefaultParams()
	req := types.CreateAuctionRequest{
		Owner:        validAuctionRequestOwner(),
		RequestID:    "req-1",
		ToolID:       "tool-1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 0),
		MaxLatencyMs: 500,
	}
	err := validateCreateRequest(params, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

func TestValidateCreateRequest_ZeroLatency(t *testing.T) {
	params := types.DefaultParams()
	req := types.CreateAuctionRequest{
		Owner:        validAuctionRequestOwner(),
		RequestID:    "req-1",
		ToolID:       "tool-1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1000),
		MaxLatencyMs: 0,
	}
	err := validateCreateRequest(params, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "latency must be > 0")
}

func TestValidateCreateRequest_LatencyExceedsCap(t *testing.T) {
	params := types.DefaultParams()
	req := types.CreateAuctionRequest{
		Owner:        validAuctionRequestOwner(),
		RequestID:    "req-1",
		ToolID:       "tool-1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1000),
		MaxLatencyMs: params.MaxBidLatencyMs + 1,
	}
	err := validateCreateRequest(params, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds global cap")
}

func TestValidateCreateRequest_Valid(t *testing.T) {
	params := types.DefaultParams()
	req := types.CreateAuctionRequest{
		Owner:        validAuctionRequestOwner(),
		RequestID:    "req-1",
		ToolID:       "tool-1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1000),
		MaxLatencyMs: 500,
	}
	err := validateCreateRequest(params, req)
	require.NoError(t, err)
}

func TestSpotCallRejectsMalformedRequestsBeforeStateContext(t *testing.T) {
	_, k := setupAuctionKeeper(t)

	_, err := k.CreateSpotAuction(context.Background(), types.CreateAuctionRequest{
		Owner:        "not-bech32",
		RequestID:    "req-before-context",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
		MaxLatencyMs: 500,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid owner address")

	_, err = k.SubmitSpotBid(context.Background(), types.SubmitBidRequest{
		AuctionID: "auc-before-context",
		Bidder:    "not-bech32",
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500),
		LatencyMs: 100,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid bidder address")

	_, _, err = k.FinalizeSpotAuction(context.Background(), " auc-before-context")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auction id")
	assert.Contains(t, err.Error(), "whitespace")
}

// ============================================================
// validateBid
// ============================================================

func TestValidateBid_InvalidBidder(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	}
	req := types.SubmitBidRequest{
		Bidder:    "not-bech32",
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	}
	err := validateBid(params, auction, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid bidder address")
}

func TestValidateBid_WrongDenom(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	}
	bidder := newAccountAddr(t)
	req := types.SubmitBidRequest{
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin("uatom", 500_000),
		LatencyMs: 1_000,
	}
	err := validateBid(params, auction, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrBidInvalidDenom)
}

func TestValidateBid_ZeroPrice(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	}
	bidder := newAccountAddr(t)
	req := types.SubmitBidRequest{
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 0),
		LatencyMs: 1_000,
	}
	err := validateBid(params, auction, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrInvalidBid)
}

func TestValidateBid_PriceExceedsMax(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		MaxLatencyMs: 2_000,
	}
	bidder := newAccountAddr(t)
	req := types.SubmitBidRequest{
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_001),
		LatencyMs: 1_000,
	}
	err := validateBid(params, auction, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrBidTooExpensive)
}

func TestValidateBid_ZeroLatency(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	}
	bidder := newAccountAddr(t)
	req := types.SubmitBidRequest{
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 0,
	}
	err := validateBid(params, auction, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrBidLatencyExceeded)
}

func TestValidateBid_LatencyExceedsAuctionMax(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 1_500,
	}
	bidder := newAccountAddr(t)
	req := types.SubmitBidRequest{
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_501,
	}
	err := validateBid(params, auction, req)
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrBidLatencyExceeded)
}

func TestValidateBid_Valid(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	}
	bidder := newAccountAddr(t)
	req := types.SubmitBidRequest{
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	}
	err := validateBid(params, auction, req)
	require.NoError(t, err)
}

// ============================================================
// ensureAuctionActive
// ============================================================

func TestEnsureAuctionActive_NotActive(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		Status:       types.AuctionStatusSettled,
		ExpiresAt:    time.Now().Add(time.Hour),
		MaxLatencyMs: 1_000,
	}
	err := ensureAuctionActive(params, auction, time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrAuctionClosed)
}

func TestEnsureAuctionActive_Expired(t *testing.T) {
	params := types.DefaultParams()
	past := time.Now().Add(-time.Hour)
	auction := &types.SpotAuction{
		Status:       types.AuctionStatusActive,
		ExpiresAt:    past,
		MaxLatencyMs: 1_000,
	}
	err := ensureAuctionActive(params, auction, time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrAuctionExpired)
}

func TestEnsureAuctionActive_LatencyExceedsCap(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		Status:       types.AuctionStatusActive,
		ExpiresAt:    time.Now().Add(time.Hour),
		MaxLatencyMs: params.MaxBidLatencyMs + 1,
	}
	err := ensureAuctionActive(params, auction, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds global cap")
}

func TestEnsureAuctionActive_Valid(t *testing.T) {
	params := types.DefaultParams()
	auction := &types.SpotAuction{
		Status:       types.AuctionStatusActive,
		ExpiresAt:    time.Now().Add(time.Hour),
		MaxLatencyMs: 1_000,
	}
	err := ensureAuctionActive(params, auction, time.Now())
	require.NoError(t, err)
}

// ============================================================
// Active auction limit
// ============================================================

func TestCreateSpotAuction_ActiveLimitReached(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	maxActive := params.MaxActiveAuctions

	// Fill up to the limit
	for i := uint32(0); i < maxActive; i++ {
		owner := newAccountAddr(t)
		_, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    fmt.Sprintf("req-limit-%d", i),
			ToolID:       "tool-limit",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
			MaxLatencyMs: 500,
		})
		require.NoError(t, err, "auction %d should succeed", i)
	}

	// Next one should fail
	owner := newAccountAddr(t)
	_, err = k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-limit-overflow",
		ToolID:       "tool-limit",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
		MaxLatencyMs: 500,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "active auction limit reached")
}

// ============================================================
// Duplicate bidder
// ============================================================

func TestSubmitSpotBid_DuplicateBidder(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-dup-bidder",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	bidder := newAccountAddr(t)
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	// Same bidder, same auction
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 400_000),
		LatencyMs: 800,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrBidDuplicate)
}

// ============================================================
// Bid on expired auction
// ============================================================

func TestSubmitSpotBid_ExpiredAuction(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-expired",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Move time past expiration
	ctx = ctx.WithBlockTime(ctx.BlockTime().Add(10 * time.Minute))

	bidder := newAccountAddr(t)
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrAuctionExpired)
}

// ============================================================
// Bid too expensive
// ============================================================

func TestSubmitSpotBid_TooExpensive(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-expensive",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	bidder := newAccountAddr(t)
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 100_001),
		LatencyMs: 1_000,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrBidTooExpensive)
}

// ============================================================
// Bid latency exceeded
// ============================================================

func TestSubmitSpotBid_LatencyExceeded(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-latency",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 1_000,
	})
	require.NoError(t, err)

	bidder := newAccountAddr(t)
	_, err = k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_001,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrBidLatencyExceeded)
}

// ============================================================
// Genesis getters
// ============================================================

func TestGetAuction_NotFound(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	_, err := k.GetAuction(ctx, "nonexistent-auction")
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrAuctionNotFound)
}

func TestGetAuction_Found(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	created, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-get",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
		MaxLatencyMs: 500,
	})
	require.NoError(t, err)

	fetched, err := k.GetAuction(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, "tool-1", fetched.ToolID)
}

func TestGetBid_NotFound(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	_, err := k.GetBid(ctx, "nonexistent-bid")
	require.Error(t, err)
}

func TestGetBid_Found(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-getbid",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	bidder := newAccountAddr(t)
	bid, err := k.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 500_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	fetched, err := k.GetBid(ctx, bid.ID)
	require.NoError(t, err)
	assert.Equal(t, bid.ID, fetched.ID)
	assert.Equal(t, bidder, fetched.Bidder)
}

func TestGetAllAuctions_Empty(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	auctions, err := k.GetAllAuctions(ctx)
	require.NoError(t, err)
	assert.Empty(t, auctions)
}

func TestGetAllAuctions_Multiple(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)

	for i := 0; i < 3; i++ {
		owner := newAccountAddr(t)
		_, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    fmt.Sprintf("req-all-%d", i),
			ToolID:       "tool-all",
			PolicyID:     "policy@1",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
			MaxLatencyMs: 500,
		})
		require.NoError(t, err)
	}

	auctions, err := k.GetAllAuctions(ctx)
	require.NoError(t, err)
	assert.Len(t, auctions, 3)
}

func TestGetAllBids_Empty(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	bids, err := k.GetAllBids(ctx)
	require.NoError(t, err)
	assert.Empty(t, bids)
}

func TestGetAllBids_Multiple(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	auction, err := k.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-allbids",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)

	// Bids must improve (decreasing price) to be accepted.
	for i := 0; i < 4; i++ {
		bidder := newAccountAddr(t)
		_, err := k.SubmitSpotBid(ctx, types.SubmitBidRequest{
			AuctionID: auction.ID,
			Bidder:    bidder,
			Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, int64(800_000-i*100_000)),
			LatencyMs: uint32(1_000 + i*100),
		})
		require.NoError(t, err)
	}

	bids, err := k.GetAllBids(ctx)
	require.NoError(t, err)
	assert.Len(t, bids, 4)
}

// ============================================================
// Finalize nonexistent auction
// ============================================================

func TestFinalizeSpotAuction_NotFound(t *testing.T) {
	ctx, k := setupAuctionKeeper(t)
	_, _, err := k.FinalizeSpotAuction(ctx, "nonexistent")
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrAuctionNotFound)
}

// ============================================================
// Finalize with discount applied
// ============================================================

func TestFinalizeWithPriorityDiscount(t *testing.T) {
	ctx, auctionKeeper, priorityKeeper := setupAuctionKeeperWithPriority(t)

	params, err := priorityKeeper.GetParams(ctx)
	require.NoError(t, err)

	params.Tiers = append(params.Tiers, prioritytypes.Tier{
		Name:                 "gold",
		MaxLatencyMs:         1_500,
		AuctionTTLMs:         uint64((30 * time.Second) / time.Millisecond),
		SpotDiscountBps:      500, // 5% discount
		QueueWeight:          500,
		PricingMultiplierBps: 200,
		ReservedCapacityBps:  0,
		Description:          "Gold tier for test",
	})
	require.NoError(t, priorityKeeper.SetParams(ctx, params))
	require.NoError(t, priorityKeeper.AssignPolicyTier(ctx, "policy-gold", "gold", time.Hour))

	owner := newAccountAddr(t)
	auction, err := auctionKeeper.CreateSpotAuction(ctx, types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-discount",
		ToolID:       "tool-discount",
		PolicyID:     "policy-gold",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(500), auction.PriorityDiscountBps)

	bidder := newAccountAddr(t)
	_, err = auctionKeeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000),
		LatencyMs: 1_000,
	})
	require.NoError(t, err)

	finalAuction, winningBid, err := auctionKeeper.FinalizeSpotAuction(ctx, auction.ID)
	require.NoError(t, err)
	require.NotNil(t, winningBid)

	// With 5% discount: 100_000 * 9500 / 10000 = 95_000
	assert.Equal(t, sdk.NewInt64Coin(types.DefaultCreditDenom, 95_000), finalAuction.BestBidPrice)
}
