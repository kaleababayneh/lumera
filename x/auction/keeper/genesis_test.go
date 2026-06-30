package keeper

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/auction/types"
)

// makeValidAuction creates a valid SpotAuction for testing with all required fields.
func makeValidAuction(t *testing.T, id, requestID string, status types.AuctionStatus, now time.Time) types.SpotAuction {
	t.Helper()
	return types.SpotAuction{
		ID:           id,
		Owner:        newAccountAddr(t),
		RequestID:    requestID,
		ToolID:       "tool-test",
		PolicyID:     "",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2000,
		CreatedAt:    now,
		ExpiresAt:    now.Add(30 * time.Second),
		Status:       status,
		BestBidPrice: sdk.NewCoin(types.DefaultCreditDenom, sdkmath.ZeroInt()), // Zero coin, not nil
	}
}

// makeValidAuctionWithBid creates a valid SpotAuction with a best bid set.
func makeValidAuctionWithBid(t *testing.T, id, requestID, bestBidID string, status types.AuctionStatus, now time.Time) types.SpotAuction {
	t.Helper()
	return types.SpotAuction{
		ID:                 id,
		Owner:              newAccountAddr(t),
		RequestID:          requestID,
		ToolID:             "tool-test",
		PolicyID:           "",
		MaxPrice:           sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs:       2000,
		CreatedAt:          now,
		ExpiresAt:          now.Add(30 * time.Second),
		Status:             status,
		BestBidID:          bestBidID,
		BestBidPrice:       sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000),
		BestBidLatencyMs:   1500,
		BestBidSubmittedAt: now.Add(5 * time.Second),
	}
}

// TestInitGenesis_DefaultState verifies that InitGenesis works with default values.
func TestInitGenesis_DefaultState(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)

	genesis := types.DefaultGenesis()
	err := keeper.InitGenesis(ctx, genesis)
	require.NoError(t, err)

	// Verify params were set
	params, err := keeper.GetParams(ctx)
	require.NoError(t, err)
	assert.Equal(t, types.DefaultCreditDenom, params.CreditDenom)

	// Verify sequences
	auctions, err := keeper.GetAllAuctions(ctx)
	require.NoError(t, err)
	assert.Empty(t, auctions)

	bids, err := keeper.GetAllBids(ctx)
	require.NoError(t, err)
	assert.Empty(t, bids)
}

// TestInitGenesis_NilGenesis verifies that nil genesis defaults correctly.
func TestInitGenesis_NilGenesis(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)

	err := keeper.InitGenesis(ctx, nil)
	require.NoError(t, err)

	// Should use default params
	params, err := keeper.GetParams(ctx)
	require.NoError(t, err)
	assert.Equal(t, types.DefaultCreditDenom, params.CreditDenom)
}

// TestInitGenesis_WithAuctions verifies auction import.
func TestInitGenesis_WithAuctions(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	now := time.Now().UTC()

	auction1 := makeValidAuction(t, "auc-1", "req-1", types.AuctionStatusActive, now)
	auction1.ToolID = "tool-1"

	auction2 := makeValidAuction(t, "auc-2", "req-2", types.AuctionStatusSettled, now)
	auction2.ToolID = "tool-2"

	genesis := types.NewGenesisState(
		types.DefaultParams(),
		[]types.SpotAuction{auction1, auction2},
		[]types.SpotBid{},
		10, // AuctionSeq
		0,  // BidSeq
		1,  // ActiveAuctionCount (auction1 is active)
	)

	err := keeper.InitGenesis(ctx, genesis)
	require.NoError(t, err)

	// Verify auctions imported
	auctions, err := keeper.GetAllAuctions(ctx)
	require.NoError(t, err)
	assert.Len(t, auctions, 2)

	// Verify specific auction
	auc, err := keeper.GetAuction(ctx, "auc-1")
	require.NoError(t, err)
	assert.Equal(t, "tool-1", auc.ToolID)
	assert.Equal(t, types.AuctionStatusActive, auc.Status)
}

// TestInitGenesis_WithBids verifies bid import with auction references.
func TestInitGenesis_WithBids(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	now := time.Now().UTC()
	bidder := newAccountAddr(t)

	auction := makeValidAuctionWithBid(t, "auc-1", "req-1", "bid-1", types.AuctionStatusActive, now)

	bid := types.SpotBid{
		ID:          "bid-1",
		AuctionID:   "auc-1",
		Bidder:      bidder,
		Price:       sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000),
		LatencyMs:   1500,
		SubmittedAt: now.Add(5 * time.Second),
	}

	genesis := types.NewGenesisState(
		types.DefaultParams(),
		[]types.SpotAuction{auction},
		[]types.SpotBid{bid},
		5, // AuctionSeq
		5, // BidSeq
		1, // ActiveAuctionCount
	)

	err := keeper.InitGenesis(ctx, genesis)
	require.NoError(t, err)

	// Verify bids imported
	bids, err := keeper.GetAllBids(ctx)
	require.NoError(t, err)
	assert.Len(t, bids, 1)

	// Verify specific bid
	retrievedBid, err := keeper.GetBid(ctx, "bid-1")
	require.NoError(t, err)
	assert.Equal(t, bidder, retrievedBid.Bidder)
	assert.Equal(t, "auc-1", retrievedBid.AuctionID)
}

// TestExportGenesis_EmptyState verifies export of empty state.
func TestExportGenesis_EmptyState(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)

	// Initialize with defaults
	err := keeper.InitGenesis(ctx, types.DefaultGenesis())
	require.NoError(t, err)

	// Export
	exported, err := keeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NotNil(t, exported)

	assert.Empty(t, exported.Auctions)
	assert.Empty(t, exported.Bids)
	assert.Equal(t, uint64(0), exported.AuctionSeq)
	assert.Equal(t, uint64(0), exported.BidSeq)
	assert.Equal(t, uint64(0), exported.ActiveAuctionCount)
}

// TestExportGenesis_WithData verifies export with populated state.
func TestExportGenesis_WithData(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Create auction via normal flow
	req := types.CreateAuctionRequest{
		Owner:        owner,
		RequestID:    "req-export",
		ToolID:       "tool-export",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000_000),
		MaxLatencyMs: 2_000,
	}

	auction, err := keeper.CreateSpotAuction(ctx, req)
	require.NoError(t, err)

	// Submit a bid
	bidder := newAccountAddr(t)
	_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
		AuctionID: auction.ID,
		Bidder:    bidder,
		Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000),
		LatencyMs: 1_500,
	})
	require.NoError(t, err)

	// Export
	exported, err := keeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NotNil(t, exported)

	assert.Len(t, exported.Auctions, 1)
	assert.Len(t, exported.Bids, 1)
	assert.Equal(t, uint64(1), exported.ActiveAuctionCount)
	assert.GreaterOrEqual(t, exported.AuctionSeq, uint64(1))
	assert.GreaterOrEqual(t, exported.BidSeq, uint64(1))
}

// TestGenesisRoundtrip verifies Init -> Export -> Init produces same state.
func TestGenesisRoundtrip(t *testing.T) {
	now := time.Now().UTC()
	bidder := newAccountAddr(t)

	auction := makeValidAuctionWithBid(t, "auc-100", "req-100", "bid-100", types.AuctionStatusActive, now)
	auction.ToolID = "tool-roundtrip"
	auction.PolicyID = "policy@2"
	auction.MaxPrice = sdk.NewInt64Coin(types.DefaultCreditDenom, 2_000_000)
	auction.MaxLatencyMs = 3000
	auction.ExpiresAt = now.Add(60 * time.Second)
	auction.BestBidPrice = sdk.NewInt64Coin(types.DefaultCreditDenom, 1_500_000)
	auction.BestBidLatencyMs = 2000
	auction.BestBidSubmittedAt = now.Add(10 * time.Second)

	bid := types.SpotBid{
		ID:          "bid-100",
		AuctionID:   "auc-100",
		Bidder:      bidder,
		Price:       sdk.NewInt64Coin(types.DefaultCreditDenom, 1_500_000),
		LatencyMs:   2000,
		SubmittedAt: now.Add(10 * time.Second),
	}

	customParams := types.Params{
		CreditDenom:              "customdenom",
		DefaultAuctionTTLSeconds: 45,
		MaxActiveAuctions:        256,
		MinBidDecrementBps:       150,
		MaxBidLatencyMs:          6000,
	}

	original := types.NewGenesisState(
		customParams,
		[]types.SpotAuction{auction},
		[]types.SpotBid{bid},
		101, // AuctionSeq
		101, // BidSeq
		1,   // ActiveAuctionCount
	)

	// First keeper: init with original
	ctx1, keeper1 := setupAuctionKeeper(t)
	err := keeper1.InitGenesis(ctx1, original)
	require.NoError(t, err)

	// Export from first keeper
	exported, err := keeper1.ExportGenesis(ctx1)
	require.NoError(t, err)

	// Second keeper: init with exported
	ctx2, keeper2 := setupAuctionKeeper(t)
	err = keeper2.InitGenesis(ctx2, exported)
	require.NoError(t, err)

	// Export from second keeper
	reExported, err := keeper2.ExportGenesis(ctx2)
	require.NoError(t, err)

	// Compare: exported and reExported should match
	assert.Equal(t, len(exported.Auctions), len(reExported.Auctions))
	assert.Equal(t, len(exported.Bids), len(reExported.Bids))
	assert.Equal(t, exported.AuctionSeq, reExported.AuctionSeq)
	assert.Equal(t, exported.BidSeq, reExported.BidSeq)
	assert.Equal(t, exported.ActiveAuctionCount, reExported.ActiveAuctionCount)

	// Verify params match
	assert.Equal(t, exported.Params.CreditDenom, reExported.Params.CreditDenom)
	assert.Equal(t, exported.Params.DefaultAuctionTTLSeconds, reExported.Params.DefaultAuctionTTLSeconds)
	assert.Equal(t, exported.Params.MaxActiveAuctions, reExported.Params.MaxActiveAuctions)
}

// TestInitGenesis_ValidationErrors tests validation failures during init.
func TestInitGenesis_ValidationErrors(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		genesis     *types.GenesisState
		expectError string
	}{
		{
			name: "invalid params - empty denom",
			genesis: types.NewGenesisState(
				types.Params{
					CreditDenom:              "",
					DefaultAuctionTTLSeconds: 30,
					MaxActiveAuctions:        100,
					MinBidDecrementBps:       100,
					MaxBidLatencyMs:          5000,
				},
				nil, nil, 0, 0, 0,
			),
			expectError: "invalid params",
		},
		{
			name: "duplicate auction IDs",
			genesis: func() *types.GenesisState {
				auction := makeValidAuction(t, "auc-dup", "req-1", types.AuctionStatusActive, now)
				auction2 := makeValidAuction(t, "auc-dup", "req-2", types.AuctionStatusActive, now)
				return types.NewGenesisState(
					types.DefaultParams(),
					[]types.SpotAuction{auction, auction2},
					nil, 10, 0, 2,
				)
			}(),
			expectError: "duplicate auction id",
		},
		{
			name: "active count mismatch",
			genesis: func() *types.GenesisState {
				auction := makeValidAuction(t, "auc-1", "req-1", types.AuctionStatusActive, now)
				return types.NewGenesisState(
					types.DefaultParams(),
					[]types.SpotAuction{auction},
					nil, 10, 0, 0, // ActiveCount=0 but there's 1 active
				)
			}(),
			expectError: "active auction count mismatch",
		},
		{
			name: "bid references non-existent auction",
			genesis: func() *types.GenesisState {
				bidder := newAccountAddr(t)
				bid := types.SpotBid{
					ID:          "bid-orphan",
					AuctionID:   "auc-nonexistent",
					Bidder:      bidder,
					Price:       sdk.NewInt64Coin(types.DefaultCreditDenom, 50000),
					LatencyMs:   500,
					SubmittedAt: now,
				}
				return types.NewGenesisState(
					types.DefaultParams(),
					nil,
					[]types.SpotBid{bid},
					0, 10, 0,
				)
			}(),
			expectError: "non-existent auction",
		},
		{
			name: "auction best bid references missing bid",
			genesis: func() *types.GenesisState {
				auction := makeValidAuctionWithBid(t, "auc-1", "req-best-missing", "bid-missing", types.AuctionStatusActive, now)
				return types.NewGenesisState(
					types.DefaultParams(),
					[]types.SpotAuction{auction},
					nil,
					10,
					0,
					1,
				)
			}(),
			expectError: "non-existent best bid",
		},
		{
			name: "auction sequence too low",
			genesis: func() *types.GenesisState {
				auction := makeValidAuction(t, "auc-50", "req-1", types.AuctionStatusSettled, now)
				return types.NewGenesisState(
					types.DefaultParams(),
					[]types.SpotAuction{auction},
					nil, 10, 0, 0, // AuctionSeq=10 but auction ID implies >= 51
				)
			}(),
			expectError: "auction sequence",
		},
		{
			name: "duplicate live auction request id",
			genesis: func() *types.GenesisState {
				auction1 := makeValidAuction(t, "auc-1", "req-duplicate-live", types.AuctionStatusActive, now)
				auction2 := makeValidAuction(t, "auc-2", "req-duplicate-live", types.AuctionStatusPending, now)
				return types.NewGenesisState(
					types.DefaultParams(),
					[]types.SpotAuction{auction1, auction2},
					nil,
					10,
					0,
					2,
				)
			}(),
			expectError: "duplicate live auction request id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, keeper := setupAuctionKeeper(t)
			err := keeper.InitGenesis(ctx, tc.genesis)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectError)
		})
	}
}

func TestInitGenesis_RejectsBidSubmittedOutsideAuctionWindow(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	bidder := newAccountAddr(t)

	auction := makeValidAuction(t, "auc-window", "req-window", types.AuctionStatusActive, now)
	bid := types.SpotBid{
		ID:          "bid-window",
		AuctionID:   auction.ID,
		Bidder:      bidder,
		Price:       sdk.NewInt64Coin(types.DefaultCreditDenom, 800_000),
		LatencyMs:   1500,
		SubmittedAt: auction.ExpiresAt.Add(time.Second),
	}

	genesis := types.NewGenesisState(
		types.DefaultParams(),
		[]types.SpotAuction{auction},
		[]types.SpotBid{bid},
		10,
		10,
		1,
	)

	err := keeper.InitGenesis(ctx, genesis)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "submitted at must be between created at and expires at")
}

func TestInitGenesis_RejectsLiveBestBidMetadataMismatch(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	bidder := newAccountAddr(t)

	auction := makeValidAuctionWithBid(t, "auc-best-metadata", "req-best-metadata", "bid-best-metadata", types.AuctionStatusActive, now)
	bid := types.SpotBid{
		ID:          auction.BestBidID,
		AuctionID:   auction.ID,
		Bidder:      bidder,
		Price:       auction.BestBidPrice,
		LatencyMs:   auction.BestBidLatencyMs + 1,
		SubmittedAt: auction.BestBidSubmittedAt,
	}

	genesis := types.NewGenesisState(
		types.DefaultParams(),
		[]types.SpotAuction{auction},
		[]types.SpotBid{bid},
		10,
		10,
		1,
	)

	err := keeper.InitGenesis(ctx, genesis)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "best bid latency does not match")

	_, getErr := keeper.GetAuction(ctx, auction.ID)
	require.Error(t, getErr)
}

// TestInitGenesis_IndexRebuild verifies that indexes are properly rebuilt.
func TestInitGenesis_IndexRebuild(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	now := time.Now().UTC()

	auction := makeValidAuction(t, "auc-1", "req-idx", types.AuctionStatusActive, now)

	genesis := types.NewGenesisState(
		types.DefaultParams(),
		[]types.SpotAuction{auction},
		[]types.SpotBid{},
		10, 0, 1,
	)

	err := keeper.InitGenesis(ctx, genesis)
	require.NoError(t, err)

	// Verify AuctionByRequest index was rebuilt
	auctionID, err := keeper.state.AuctionByRequest.Get(ctx, "req-idx")
	require.NoError(t, err)
	assert.Equal(t, "auc-1", auctionID)
}

// TestExportGenesis_PreservesSequences verifies sequence counters are exported correctly.
func TestExportGenesis_PreservesSequences(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Create multiple auctions to increment sequences
	for i := 0; i < 5; i++ {
		req := types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    fmt.Sprintf("req-seq-%d", i),
			ToolID:       "tool-seq",
			PolicyID:     "",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000),
			MaxLatencyMs: 1000,
		}
		auction, err := keeper.CreateSpotAuction(ctx, req)
		require.NoError(t, err)

		// Submit a bid for each
		bidder := newAccountAddr(t)
		_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
			AuctionID: auction.ID,
			Bidder:    bidder,
			Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 80_000),
			LatencyMs: 800,
		})
		require.NoError(t, err)
	}

	exported, err := keeper.ExportGenesis(ctx)
	require.NoError(t, err)

	// Sequences should be at least 5 (could be higher depending on implementation)
	assert.GreaterOrEqual(t, exported.AuctionSeq, uint64(5))
	assert.GreaterOrEqual(t, exported.BidSeq, uint64(5))
}

// TestInitGenesis_MultipleStatuses tests auctions with various statuses.
func TestInitGenesis_MultipleStatuses(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	now := time.Now().UTC()

	auctions := []types.SpotAuction{
		makeValidAuction(t, "auc-1", "req-1", types.AuctionStatusActive, now),
		makeValidAuction(t, "auc-2", "req-2", types.AuctionStatusPending, now),
		makeValidAuction(t, "auc-3", "req-3", types.AuctionStatusSettled, now),
		makeValidAuction(t, "auc-4", "req-4", types.AuctionStatusExpired, now),
		makeValidAuction(t, "auc-5", "req-5", types.AuctionStatusCanceled, now),
	}

	// Active count: Active + Pending = 2
	genesis := types.NewGenesisState(
		types.DefaultParams(),
		auctions,
		nil,
		10, 0, 2, // 2 active (Active + Pending)
	)

	err := keeper.InitGenesis(ctx, genesis)
	require.NoError(t, err)

	// Verify all imported
	allAuctions, err := keeper.GetAllAuctions(ctx)
	require.NoError(t, err)
	assert.Len(t, allAuctions, 5)
}

// TestInitGenesis_OnlyActiveAndPendingIndexed pins the
// status-filtered-indexing invariant in InitGenesis: only auctions
// in ACTIVE or PENDING status have their RequestID registered in
// AuctionByRequest and their expiry registered in AuctionsByExpiry.
// Settled, Expired, and Canceled auctions are stored in the main
// Auctions map but MUST NOT leak into the secondary indexes.
//
// Regression guards:
//   - If the Status filter were accidentally removed and all auctions
//     got indexed into AuctionByRequest, a future CreateSpotAuction
//     with a RequestID that happens to match a long-Settled auction
//     would fail with ErrAuctionExists — breaking request-id reuse
//     and causing ghost collisions against historical state.
//   - If the expiry filter were removed, AuctionsByExpiry would
//     accumulate entries for already-terminal auctions, inflating
//     the expired-prune walk with stale work.
//
// The existing TestInitGenesis_MultipleStatuses only checks that
// all 5 auctions land in the Auctions map — it never touches the
// secondary indexes, so a refactor dropping the Status guard would
// pass that test. This test fills the gap.
func TestInitGenesis_OnlyActiveAndPendingIndexed(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	now := time.Now().UTC()

	// Five auctions, one per status. Each has a distinct RequestID
	// so we can verify index membership per-request.
	auctions := []types.SpotAuction{
		makeValidAuction(t, "auc-active", "req-active", types.AuctionStatusActive, now),
		makeValidAuction(t, "auc-pending", "req-pending", types.AuctionStatusPending, now),
		makeValidAuction(t, "auc-settled", "req-settled", types.AuctionStatusSettled, now),
		makeValidAuction(t, "auc-expired", "req-expired", types.AuctionStatusExpired, now),
		makeValidAuction(t, "auc-canceled", "req-canceled", types.AuctionStatusCanceled, now),
	}

	genesis := types.NewGenesisState(
		types.DefaultParams(),
		auctions,
		nil,
		10, 0, 2,
	)
	require.NoError(t, keeper.InitGenesis(ctx, genesis))

	// Positive: Active and Pending auctions MUST have AuctionByRequest entries.
	got, err := keeper.state.AuctionByRequest.Get(ctx, "req-active")
	require.NoError(t, err, "Active RequestID must be indexed")
	assert.Equal(t, "auc-active", got)

	got, err = keeper.state.AuctionByRequest.Get(ctx, "req-pending")
	require.NoError(t, err, "Pending RequestID must be indexed")
	assert.Equal(t, "auc-pending", got)

	// Negative: Settled, Expired, and Canceled auctions MUST NOT have
	// AuctionByRequest entries. ErrNotFound is the expected signal.
	for _, rejected := range []string{"req-settled", "req-expired", "req-canceled"} {
		_, err := keeper.state.AuctionByRequest.Get(ctx, rejected)
		require.Error(t, err, "terminal-status RequestID %q must not be indexed", rejected)
	}

	// Secondary-index verification: AuctionsByExpiry walk must
	// contain ONLY the Active+Pending auctions (2 entries, never 5).
	var indexedIDs []string
	err = keeper.state.AuctionsByExpiry.Walk(ctx, nil, func(key collections.Pair[time.Time, string]) (bool, error) {
		indexedIDs = append(indexedIDs, key.K2())
		return false, nil
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"auc-active", "auc-pending"}, indexedIDs,
		"AuctionsByExpiry must contain ONLY Active+Pending entries (2), not all 5 imported auctions")
}

func TestInitGenesis_PendingAuctionExpiresAfterImport(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	now := ctx.BlockTime()

	pending := makeValidAuction(t, "auc-pending-expired", "req-pending-expired", types.AuctionStatusPending, now)
	pending.ExpiresAt = now.Add(30 * time.Second)

	genesis := types.NewGenesisState(
		types.DefaultParams(),
		[]types.SpotAuction{pending},
		nil,
		10, 0, 1,
	)
	require.NoError(t, keeper.InitGenesis(ctx, genesis))

	ctx = ctx.WithBlockTime(pending.ExpiresAt)
	require.NoError(t, keeper.ProcessExpiredAuctions(ctx, 100))

	stored, err := keeper.GetAuction(ctx, pending.ID)
	require.NoError(t, err)
	assert.Equal(t, types.AuctionStatusExpired, stored.Status)

	activeCount, err := keeper.getActiveCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), activeCount)

	_, err = keeper.state.AuctionByRequest.Get(ctx, pending.RequestID)
	require.Error(t, err)

	indexed, err := keeper.state.AuctionsByExpiry.Has(ctx, collections.Join(pending.ExpiresAt, pending.ID))
	require.NoError(t, err)
	assert.False(t, indexed)
}

// TestExportGenesis_ActiveCountCorrect verifies active count is computed correctly on export.
func TestExportGenesis_ActiveCountCorrect(t *testing.T) {
	ctx, keeper := setupAuctionKeeper(t)
	owner := newAccountAddr(t)

	// Create 3 auctions, finalize 1
	for i := 0; i < 3; i++ {
		req := types.CreateAuctionRequest{
			Owner:        owner,
			RequestID:    fmt.Sprintf("req-active-%d", i),
			ToolID:       "tool-active",
			PolicyID:     "",
			MaxPrice:     sdk.NewInt64Coin(types.DefaultCreditDenom, 100_000),
			MaxLatencyMs: 1000,
		}
		auction, err := keeper.CreateSpotAuction(ctx, req)
		require.NoError(t, err)

		// Finalize the first one to reduce active count
		if i == 0 {
			bidder := newAccountAddr(t)
			_, err = keeper.SubmitSpotBid(ctx, types.SubmitBidRequest{
				AuctionID: auction.ID,
				Bidder:    bidder,
				Price:     sdk.NewInt64Coin(types.DefaultCreditDenom, 50_000),
				LatencyMs: 500,
			})
			require.NoError(t, err)

			_, _, err = keeper.FinalizeSpotAuction(ctx, auction.ID)
			require.NoError(t, err)
		}
	}

	exported, err := keeper.ExportGenesis(ctx)
	require.NoError(t, err)

	// 3 created, 1 finalized = 2 active
	assert.Equal(t, uint64(2), exported.ActiveAuctionCount)
	assert.Len(t, exported.Auctions, 3)
}
