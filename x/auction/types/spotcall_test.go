package types

import (
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func zeroCoin() sdk.Coin {
	return sdk.NewInt64Coin("ulac", 0)
}

func validAuctionOwner(seed string) string {
	return sdk.AccAddress([]byte(seed)).String()
}

func validAuction() SpotAuction {
	now := time.Now().UTC()
	return SpotAuction{
		ID:           "auc-1",
		Owner:        validAuctionOwner("auction-owner-000001"),
		RequestID:    "req-1",
		ToolID:       "tool-1",
		PolicyID:     "policy@1",
		MaxPrice:     sdk.NewInt64Coin("ulac", 1_000_000),
		MaxLatencyMs: 2000,
		CreatedAt:    now,
		ExpiresAt:    now.Add(30 * time.Second),
		Status:       AuctionStatusActive,
		BestBidPrice: zeroCoin(),
	}
}

func TestSpotAuction_ValidateBasic_Valid(t *testing.T) {
	a := validAuction()
	require.NoError(t, a.ValidateBasic())
}

func TestSpotAuction_ValidateBasic_EmptyID(t *testing.T) {
	a := validAuction()
	a.ID = ""
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auction id")
}

func TestSpotAuction_ValidateBasic_EmptyOwner(t *testing.T) {
	a := validAuction()
	a.Owner = ""
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner")
}

func TestSpotAuction_ValidateBasic_InvalidOwner(t *testing.T) {
	a := validAuction()
	a.Owner = "not-a-valid-address"
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner address")
}

func TestSpotAuction_ValidateBasic_EmptyRequestID(t *testing.T) {
	a := validAuction()
	a.RequestID = ""
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request id")
}

func TestSpotAuction_ValidateBasic_EmptyToolID(t *testing.T) {
	a := validAuction()
	a.ToolID = ""
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool id")
}

func TestSpotAuction_ValidateBasic_PaddedIdentifiers(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name    string
		mutate  func(*SpotAuction)
		wantErr string
	}{
		{
			name: "auction id",
			mutate: func(a *SpotAuction) {
				a.ID = " auc-1"
			},
			wantErr: "auction id",
		},
		{
			name: "request id",
			mutate: func(a *SpotAuction) {
				a.RequestID = "req-1 "
			},
			wantErr: "request id",
		},
		{
			name: "tool id",
			mutate: func(a *SpotAuction) {
				a.ToolID = "\ttool-1"
			},
			wantErr: "tool id",
		},
		{
			name: "policy id",
			mutate: func(a *SpotAuction) {
				a.PolicyID = " policy@1 "
			},
			wantErr: "policy id",
		},
		{
			name: "reserve commitment id",
			mutate: func(a *SpotAuction) {
				a.ReserveApplied = true
				a.ReserveCommitmentID = " commit-1"
			},
			wantErr: "reserve commitment id",
		},
		{
			name: "best bid id",
			mutate: func(a *SpotAuction) {
				a.BestBidID = "bid-1 "
				a.BestBidPrice = sdk.NewInt64Coin("ulac", 500_000)
				a.BestBidLatencyMs = 1000
				a.BestBidSubmittedAt = now
			},
			wantErr: "best bid id",
		},
		{
			name: "winner bid id",
			mutate: func(a *SpotAuction) {
				a.WinnerBidID = "\nbid-1"
			},
			wantErr: "winner bid id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := validAuction()
			tc.mutate(&a)
			err := a.ValidateBasic()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "whitespace")
		})
	}
}

func TestSpotAuction_ValidateBasic_PolicyIDTooLong(t *testing.T) {
	a := validAuction()
	a.PolicyID = string(make([]byte, 129))
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy id too long")
}

func TestSpotAuction_ValidateBasic_IdentifierTooLong(t *testing.T) {
	oversized := strings.Repeat("a", 257)
	now := time.Now().UTC()

	tests := []struct {
		name    string
		mutate  func(*SpotAuction)
		wantErr string
	}{
		{
			name: "auction id",
			mutate: func(a *SpotAuction) {
				a.ID = oversized
			},
			wantErr: "auction id",
		},
		{
			name: "request id",
			mutate: func(a *SpotAuction) {
				a.RequestID = oversized
			},
			wantErr: "request id",
		},
		{
			name: "tool id",
			mutate: func(a *SpotAuction) {
				a.ToolID = oversized
			},
			wantErr: "tool id",
		},
		{
			name: "reserve commitment id",
			mutate: func(a *SpotAuction) {
				a.ReserveCommitmentID = oversized
			},
			wantErr: "reserve commitment id",
		},
		{
			name: "best bid id",
			mutate: func(a *SpotAuction) {
				a.BestBidID = oversized
				a.BestBidPrice = sdk.NewInt64Coin("ulac", 500_000)
				a.BestBidLatencyMs = 1000
				a.BestBidSubmittedAt = now
			},
			wantErr: "best bid id",
		},
		{
			name: "winner bid id",
			mutate: func(a *SpotAuction) {
				a.WinnerBidID = oversized
			},
			wantErr: "winner bid id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := validAuction()
			tc.mutate(&a)
			err := a.ValidateBasic()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "exceeds")
		})
	}
}

func TestSpotAuction_ValidateBasic_EmptyPolicyIDOK(t *testing.T) {
	a := validAuction()
	a.PolicyID = ""
	require.NoError(t, a.ValidateBasic())
}

func TestSpotAuction_ValidateBasic_InvalidMaxPrice(t *testing.T) {
	a := validAuction()
	a.MaxPrice = sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(0)}
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max price")
}

func TestSpotAuction_ValidateBasic_ZeroLatency(t *testing.T) {
	a := validAuction()
	a.MaxLatencyMs = 0
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max latency")
}

func TestSpotAuction_ValidateBasic_ZeroCreatedAt(t *testing.T) {
	a := validAuction()
	a.CreatedAt = time.Time{}
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "created at")
}

func TestSpotAuction_ValidateBasic_ExpiresBeforeCreated(t *testing.T) {
	a := validAuction()
	a.ExpiresAt = a.CreatedAt.Add(-time.Second)
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expires at")
}

func TestSpotAuction_ValidateBasic_ZeroExpiresAt(t *testing.T) {
	a := validAuction()
	a.ExpiresAt = time.Time{}
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expires at")
}

func TestSpotAuction_ValidateBasic_InvalidStatus(t *testing.T) {
	a := validAuction()
	a.Status = "BOGUS"
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid auction status")
}

func TestSpotAuction_ValidateBasic_AllStatuses(t *testing.T) {
	for _, status := range []AuctionStatus{
		AuctionStatusPending, AuctionStatusActive,
		AuctionStatusSettled, AuctionStatusExpired, AuctionStatusCanceled,
	} {
		a := validAuction()
		a.Status = status
		require.NoError(t, a.ValidateBasic(), "status %s should be valid", status)
	}
}

func TestSpotAuction_ValidateBasic_PriorityDiscountExceeds100(t *testing.T) {
	a := validAuction()
	a.PriorityDiscountBps = 10_001
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "priority discount")
}

func TestSpotAuction_ValidateBasic_PriorityDiscountMax(t *testing.T) {
	a := validAuction()
	a.PriorityDiscountBps = 10_000
	require.NoError(t, a.ValidateBasic())
}

func TestSpotAuction_ValidateBasic_ReserveAppliedWithoutID(t *testing.T) {
	a := validAuction()
	a.ReserveApplied = true
	a.ReserveCommitmentID = ""
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserve commitment id")
}

func TestSpotAuction_ValidateBasic_ReserveAppliedWithID(t *testing.T) {
	a := validAuction()
	a.ReserveApplied = true
	a.ReserveCommitmentID = "commit-1"
	require.NoError(t, a.ValidateBasic())
}

func TestSpotAuction_ValidateBasic_BestBidNoPriceOK(t *testing.T) {
	a := validAuction()
	a.BestBidID = ""
	a.BestBidPrice = zeroCoin()
	require.NoError(t, a.ValidateBasic())
}

func TestSpotAuction_ValidateBasic_UnsetBestBidPriceNoBidOK(t *testing.T) {
	a := validAuction()
	a.BestBidID = ""
	a.BestBidPrice = sdk.Coin{}

	require.NotPanics(t, func() {
		require.NoError(t, a.ValidateBasic())
	})
}

func TestSpotAuction_ValidateBasic_BestBidNonZeroPriceNoID(t *testing.T) {
	a := validAuction()
	a.BestBidID = ""
	a.BestBidPrice = sdk.NewInt64Coin("ulac", 100)
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "best bid price must be zero")
}

func TestSpotAuction_ValidateBasic_BestBidNilAmountRejects(t *testing.T) {
	a := validAuction()
	a.BestBidID = "bid-1"
	a.BestBidPrice = sdk.Coin{Denom: "ulac"}
	a.BestBidLatencyMs = 1000
	a.BestBidSubmittedAt = time.Now().UTC()

	require.NotPanics(t, func() {
		err := a.ValidateBasic()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "best bid price")
		assert.Contains(t, err.Error(), "amount is nil")
	})
}

func TestSpotAuction_ValidateBasic_BestBidWithFields(t *testing.T) {
	a := validAuction()
	a.BestBidID = "bid-1"
	a.BestBidPrice = sdk.NewInt64Coin("ulac", 500_000)
	a.BestBidLatencyMs = 1000
	a.BestBidSubmittedAt = time.Now().UTC()
	require.NoError(t, a.ValidateBasic())
}

func TestSpotAuction_ValidateBasic_BestBidZeroLatency(t *testing.T) {
	a := validAuction()
	a.BestBidID = "bid-1"
	a.BestBidPrice = sdk.NewInt64Coin("ulac", 500_000)
	a.BestBidLatencyMs = 0
	a.BestBidSubmittedAt = time.Now().UTC()
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "best bid latency")
}

func TestSpotAuction_ValidateBasic_BestBidZeroSubmittedAt(t *testing.T) {
	a := validAuction()
	a.BestBidID = "bid-1"
	a.BestBidPrice = sdk.NewInt64Coin("ulac", 500_000)
	a.BestBidLatencyMs = 1000
	a.BestBidSubmittedAt = time.Time{}
	err := a.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "best bid submitted at")
}

func TestSpotAuction_IsExpired(t *testing.T) {
	now := time.Now().UTC()
	a := validAuction()
	a.ExpiresAt = now

	assert.False(t, a.IsExpired(now.Add(-time.Second)), "before expiry")
	assert.False(t, a.IsExpired(now), "at exact expiry")
	assert.True(t, a.IsExpired(now.Add(time.Second)), "after expiry")
}

func TestSpotAuction_IsActive(t *testing.T) {
	now := time.Now().UTC()
	a := validAuction()
	a.ExpiresAt = now.Add(10 * time.Second)

	a.Status = AuctionStatusActive
	assert.True(t, a.IsActive(now))

	a.Status = AuctionStatusPending
	assert.False(t, a.IsActive(now), "pending is not active")

	a.Status = AuctionStatusSettled
	assert.False(t, a.IsActive(now), "settled is not active")

	a.Status = AuctionStatusActive
	assert.False(t, a.IsActive(now.Add(20*time.Second)), "expired by time")
}

// --- SpotBid tests ---

func validBid() SpotBid {
	return SpotBid{
		ID:          "bid-1",
		AuctionID:   "auc-1",
		Bidder:      "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Price:       sdk.NewInt64Coin("ulac", 500_000),
		LatencyMs:   1000,
		SubmittedAt: time.Now().UTC(),
	}
}

func TestSpotBid_ValidateBasic_Valid(t *testing.T) {
	b := validBid()
	require.NoError(t, b.ValidateBasic())
}

func TestSpotBid_ValidateBasic_EmptyID(t *testing.T) {
	b := validBid()
	b.ID = ""
	err := b.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bid id")
}

func TestSpotBid_ValidateBasic_EmptyAuctionID(t *testing.T) {
	b := validBid()
	b.AuctionID = ""
	err := b.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auction id")
}

func TestSpotBid_ValidateBasic_PaddedIdentifiers(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*SpotBid)
		wantErr string
	}{
		{
			name: "bid id",
			mutate: func(b *SpotBid) {
				b.ID = " bid-1"
			},
			wantErr: "bid id",
		},
		{
			name: "auction id",
			mutate: func(b *SpotBid) {
				b.AuctionID = "auc-1 "
			},
			wantErr: "auction id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := validBid()
			tc.mutate(&b)
			err := b.ValidateBasic()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "whitespace")
		})
	}
}

func TestSpotBid_ValidateBasic_IdentifierTooLong(t *testing.T) {
	oversized := strings.Repeat("a", 257)
	tests := []struct {
		name    string
		mutate  func(*SpotBid)
		wantErr string
	}{
		{
			name: "bid id",
			mutate: func(b *SpotBid) {
				b.ID = oversized
			},
			wantErr: "bid id",
		},
		{
			name: "auction id",
			mutate: func(b *SpotBid) {
				b.AuctionID = oversized
			},
			wantErr: "auction id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := validBid()
			tc.mutate(&b)
			err := b.ValidateBasic()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "exceeds")
		})
	}
}

func TestSpotBid_ValidateBasic_InvalidBidder(t *testing.T) {
	b := validBid()
	b.Bidder = "not-a-valid-address"
	err := b.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bidder address")
}

func TestSpotBid_ValidateBasic_InvalidPrice(t *testing.T) {
	b := validBid()
	b.Price = sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(0)}
	err := b.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bid price")
}

func TestSpotBid_ValidateBasic_NilAmountPriceRejects(t *testing.T) {
	b := validBid()
	b.Price = sdk.Coin{Denom: "ulac"}

	require.NotPanics(t, func() {
		err := b.ValidateBasic()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bid price")
		assert.Contains(t, err.Error(), "amount is nil")
	})
}

func TestSpotBid_ValidateBasic_ZeroLatency(t *testing.T) {
	b := validBid()
	b.LatencyMs = 0
	err := b.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "latency")
}

func TestSpotBid_ValidateBasic_ZeroSubmittedAt(t *testing.T) {
	b := validBid()
	b.SubmittedAt = time.Time{}
	err := b.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "submitted at")
}

func TestSpotBid_BetterThan_LowerPrice(t *testing.T) {
	now := time.Now().UTC()
	b1 := SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 500, SubmittedAt: now}
	b2 := SpotBid{Price: sdk.NewInt64Coin("ulac", 200), LatencyMs: 500, SubmittedAt: now}
	assert.True(t, b1.BetterThan(b2))
	assert.False(t, b2.BetterThan(b1))
}

func TestSpotBid_BetterThan_SamePriceLowerLatency(t *testing.T) {
	now := time.Now().UTC()
	b1 := SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 300, SubmittedAt: now}
	b2 := SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 500, SubmittedAt: now}
	assert.True(t, b1.BetterThan(b2))
	assert.False(t, b2.BetterThan(b1))
}

func TestSpotBid_BetterThan_SamePriceSameLatencyEarlierSubmit(t *testing.T) {
	now := time.Now().UTC()
	b1 := SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 500, SubmittedAt: now.Add(-time.Second)}
	b2 := SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 500, SubmittedAt: now}
	assert.True(t, b1.BetterThan(b2))
	assert.False(t, b2.BetterThan(b1))
}

func TestSpotBid_BetterThan_IdenticalBids(t *testing.T) {
	now := time.Now().UTC()
	b := SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 500, SubmittedAt: now}
	assert.False(t, b.BetterThan(b), "identical bids: neither is strictly better")
}

func TestSpotBid_BetterThan_InvalidOther(t *testing.T) {
	b := SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 500, SubmittedAt: time.Now()}
	other := SpotBid{Price: zeroCoin()} // zero
	assert.True(t, b.BetterThan(other))
}

// TestSpotBid_BetterThan_AntiSymmetryMetamorphic asserts that for
// any two distinct bids, at most one can strictly beat the other —
// BetterThan cannot return true in both directions. The ordering
// drives bid-book tie-break logic; asymmetric preference would let
// the same bid both win and lose depending on which order the
// comparator was called in, producing non-deterministic auctions.
func TestSpotBid_BetterThan_AntiSymmetryMetamorphic(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name string
		a, b SpotBid
	}{
		{
			"price_only",
			SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 200, SubmittedAt: now},
			SpotBid{Price: sdk.NewInt64Coin("ulac", 200), LatencyMs: 200, SubmittedAt: now},
		},
		{
			"latency_only",
			SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 100, SubmittedAt: now},
			SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 400, SubmittedAt: now},
		},
		{
			"submit_time_only",
			SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 100, SubmittedAt: now.Add(-time.Minute)},
			SpotBid{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 100, SubmittedAt: now},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ab := tc.a.BetterThan(tc.b)
			ba := tc.b.BetterThan(tc.a)
			if ab && ba {
				t.Fatalf("both bids claim to be strictly better: a=%+v b=%+v", tc.a, tc.b)
			}
		})
	}
}

// TestSpotBid_BetterThan_TransitivityMetamorphic asserts the
// transitivity property: if a beats b and b beats c, then a must
// beat c. Without this the comparator is not sort-safe and bid-book
// ordering depends on pivot choice — a sort.Slice result becomes
// non-deterministic and auctions with more than two bidders can
// reorder between calls.
func TestSpotBid_BetterThan_TransitivityMetamorphic(t *testing.T) {
	now := time.Now().UTC()
	triples := [][]SpotBid{
		// Pure price-ordering chain.
		{
			{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 500, SubmittedAt: now},
			{Price: sdk.NewInt64Coin("ulac", 200), LatencyMs: 500, SubmittedAt: now},
			{Price: sdk.NewInt64Coin("ulac", 300), LatencyMs: 500, SubmittedAt: now},
		},
		// Price-tied chain broken by latency.
		{
			{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 100, SubmittedAt: now},
			{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 500, SubmittedAt: now},
			{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 800, SubmittedAt: now},
		},
		// Mixed axes.
		{
			{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 200, SubmittedAt: now},
			{Price: sdk.NewInt64Coin("ulac", 100), LatencyMs: 500, SubmittedAt: now},
			{Price: sdk.NewInt64Coin("ulac", 150), LatencyMs: 300, SubmittedAt: now},
		},
	}
	for i, tr := range triples {
		a, b, c := tr[0], tr[1], tr[2]
		ab := a.BetterThan(b)
		bc := b.BetterThan(c)
		ac := a.BetterThan(c)
		if ab && bc && !ac {
			t.Fatalf("case %d: transitivity broken: a->b=%v b->c=%v a->c=%v",
				i, ab, bc, ac)
		}
	}
}

// --- Params tests ---

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	assert.Equal(t, DefaultCreditDenom, p.CreditDenom)
	assert.Equal(t, uint64(30), p.DefaultAuctionTTLSeconds)
	assert.Equal(t, uint32(1024), p.MaxActiveAuctions)
	assert.Equal(t, uint32(100), p.MinBidDecrementBps)
	assert.Equal(t, uint32(5_000), p.MaxBidLatencyMs)
}

func TestDefaultParams_ValidateBasic(t *testing.T) {
	require.NoError(t, DefaultParams().ValidateBasic())
}

func TestParams_ValidateBasic_InvalidDenom(t *testing.T) {
	p := DefaultParams()
	p.CreditDenom = ""
	err := p.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credit denom")
}

func TestParams_ValidateBasic_ZeroTTL(t *testing.T) {
	p := DefaultParams()
	p.DefaultAuctionTTLSeconds = 0
	err := p.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TTL")
}

func TestParams_ValidateBasic_AuctionTTLExceedsDurationLimit(t *testing.T) {
	p := DefaultParams()
	p.DefaultAuctionTTLSeconds = maxAuctionTTLSeconds + 1
	err := p.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum safe duration")
}

func TestParams_ValidateBasic_ZeroMaxAuctions(t *testing.T) {
	p := DefaultParams()
	p.MaxActiveAuctions = 0
	err := p.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max active auctions")
}

func TestParams_ValidateBasic_BidDecrementExceeds10000(t *testing.T) {
	p := DefaultParams()
	p.MinBidDecrementBps = 10_001
	err := p.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bid decrement")
}

func TestParams_ValidateBasic_BidDecrementBoundary(t *testing.T) {
	p := DefaultParams()
	p.MinBidDecrementBps = 10_000
	require.NoError(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_ZeroBidDecrementOK(t *testing.T) {
	p := DefaultParams()
	p.MinBidDecrementBps = 0
	require.NoError(t, p.ValidateBasic())
}

func TestParams_ValidateBasic_ZeroMaxLatency(t *testing.T) {
	p := DefaultParams()
	p.MaxBidLatencyMs = 0
	err := p.ValidateBasic()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max bid latency")
}

func TestParams_AuctionTTL(t *testing.T) {
	p := DefaultParams()
	assert.Equal(t, 30*time.Second, p.AuctionTTL())

	p.DefaultAuctionTTLSeconds = 3600
	assert.Equal(t, time.Hour, p.AuctionTTL())

	p.DefaultAuctionTTLSeconds = 0
	assert.Equal(t, time.Duration(0), p.AuctionTTL())
}

// --- validateCoin and validateCoinNonNegative edge cases ---

func TestValidateCoin_InvalidCoin(t *testing.T) {
	// Invalid coin with empty denom
	c := sdk.Coin{Denom: "", Amount: sdkmath.NewInt(100)}
	err := validateCoin(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "coin invalid")
}

func TestValidateCoin_ZeroAmount(t *testing.T) {
	// Valid denom but zero amount (not positive)
	c := sdk.NewInt64Coin("ulac", 0)
	err := validateCoin(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

func TestValidateCoin_ValidPositive(t *testing.T) {
	c := sdk.NewInt64Coin("ulac", 100)
	require.NoError(t, validateCoin(c))
}

func TestValidateCoinNonNegative_InvalidCoin(t *testing.T) {
	// Invalid coin with empty denom
	c := sdk.Coin{Denom: "", Amount: sdkmath.NewInt(100)}
	err := validateCoinNonNegative(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "coin invalid")
}

func TestValidateCoinNonNegative_NegativeAmount(t *testing.T) {
	// Negative amount coin is invalid per sdk.Coin.IsValid()
	// so it returns "coin invalid" rather than "negative" error
	c := sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(-100)}
	err := validateCoinNonNegative(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestValidateCoinNonNegative_ZeroOK(t *testing.T) {
	// Zero is allowed for non-negative
	c := sdk.NewInt64Coin("ulac", 0)
	require.NoError(t, validateCoinNonNegative(c))
}

func TestValidateCoinNonNegative_PositiveOK(t *testing.T) {
	c := sdk.NewInt64Coin("ulac", 100)
	require.NoError(t, validateCoinNonNegative(c))
}

// --- validateStatus ---
//
// validateStatus gates every status transition stored in state.
// The switch enumerates the 5 valid AuctionStatus values; any
// other value (typically the UNSPECIFIED zero) returns an error.
// Zero direct coverage before; pinning the truth table prevents
// a regression where a refactor dropped one of the cases and
// silently let an invalid status through.

func TestValidateStatus_AllValid(t *testing.T) {
	for _, s := range []AuctionStatus{
		AuctionStatusPending,
		AuctionStatusActive,
		AuctionStatusSettled,
		AuctionStatusExpired,
		AuctionStatusCanceled,
	} {
		require.NoErrorf(t, validateStatus(s), "status %s must be valid", s)
	}
}

func TestValidateStatus_EmptyZeroValueRejected(t *testing.T) {
	// AuctionStatus is a string type; its zero value is "".
	// That empty string is not in the allowed set and must error.
	// Without this guard, genesis imports with an unset status
	// would silently accept.
	err := validateStatus(AuctionStatus(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid auction status")
}

func TestValidateStatus_UnknownValueRejected(t *testing.T) {
	// An arbitrary string that isn't one of the five enum members
	// must be rejected. This catches the common "added a new
	// status value but forgot to update the validator switch" mistake.
	err := validateStatus(AuctionStatus("UNKNOWN_FUTURE_STATUS"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid auction status")
}

// --- validatePolicyID ---
//
// validatePolicyID accepts empty (policy is optional) or non-empty
// strings up to 128 chars. Zero direct coverage.

func TestValidatePolicyID_Empty(t *testing.T) {
	// Empty is explicitly allowed — policy ID is optional on
	// SpotAuction and callers pass "" when none is configured.
	require.NoError(t, validatePolicyID(""))
}

func TestValidatePolicyID_TypicalID(t *testing.T) {
	require.NoError(t, validatePolicyID("policy-alpha@v1"))
}

func TestValidatePolicyID_ExactlyMaxLength(t *testing.T) {
	// Boundary: exactly 128 chars must be accepted (LTE, not LT).
	// Regression guard against a refactor that flipped the
	// comparison to strict greater-than-128 → accepting 129.
	require.NoError(t, validatePolicyID(strings.Repeat("a", 128)))
}

func TestValidatePolicyID_OverMaxRejected(t *testing.T) {
	require.Error(t, validatePolicyID(strings.Repeat("a", 129)))
}
