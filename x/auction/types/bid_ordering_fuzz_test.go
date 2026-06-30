package types

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestSpotBid_BetterThan_PermutationBestMetamorphic(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	bids := []SpotBid{
		orderingBid("high-price-fast", 800_000, 100, now),
		orderingBid("low-price-slow", 600_000, 900, now.Add(3*time.Second)),
		orderingBid("low-price-fast", 600_000, 200, now.Add(2*time.Second)),
		orderingBid("low-price-fast-earliest", 600_000, 200, now.Add(time.Second)),
	}

	want := bids[3]
	permutations := [][]SpotBid{
		{bids[0], bids[1], bids[2], bids[3]},
		{bids[3], bids[2], bids[1], bids[0]},
		{bids[1], bids[0], bids[3], bids[2]},
		{bids[2], bids[3], bids[0], bids[1]},
	}

	for i, permutation := range permutations {
		got := bestBidByComparator(permutation)
		require.Equal(t, want.ID, got.ID, "permutation %d picked a different best bid", i)
	}
}

func TestSpotBid_BetterThan_DeterministicFinalTieBreak(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	early := orderingBid("early", 500_000, 250, now)
	late := orderingBid("late", 500_000, 250, now.Add(time.Nanosecond))

	require.True(t, early.BetterThan(late))
	require.False(t, late.BetterThan(early))
	require.False(t, early.BetterThan(early))
}

// FuzzSpotBidBetterThanTransitivity pins the transitivity invariant
// of the BetterThan comparator — the third strict-partial-order
// property alongside antisymmetry and irreflexivity (both covered
// by FuzzSpotBidBetterThanOrderingInvariants).
//
// For any three bids a, b, c:
//
//	a.BetterThan(b) && b.BetterThan(c)  ⇒  a.BetterThan(c)
//
// BetterThan is a STRICT PARTIAL ORDER, not a total order. Bids that
// tie on all ordering keys (price, latency, submittedAt) are
// incomparable — BetterThan returns false in both directions. That
// is valid partial-order semantics: any downstream caller that
// iterates bids via BetterThan is responsible for its own deterministic
// tiebreaker when the comparator can't distinguish the inputs.
//
// Why transitivity matters: any iterative winner-selection helper
// (e.g. bestBidByComparator in this file; hypothetically also
// explorer/indexer sort code) depends on transitivity to produce
// consistent results regardless of the visitor order. A future
// refactor that, e.g., swaps the tiebreaker chain or introduces a
// rounding bucket in price comparison could silently break
// transitivity; this fuzz surfaces that.
//
// Seeds cover: strict price ordering, latency-tiebreaker chains,
// submit-time-tiebreaker chains, mixed cases, and fully-equal bids
// (incomparable under the partial-order semantic).
func FuzzSpotBidBetterThanTransitivity(f *testing.F) {
	for _, seed := range []struct {
		priceA, priceB, priceC       uint64
		latencyA, latencyB, latencyC uint32
		offsetA, offsetB, offsetC    int64
	}{
		// Strict price ordering: a < b < c on price
		{100, 200, 300, 500, 500, 500, 0, 0, 0},
		// Price tie, latency chain: a < b < c on latency
		{100, 100, 100, 100, 200, 300, 0, 0, 0},
		// Full tie through latency, time chain: a earliest, c latest
		{100, 100, 100, 500, 500, 500, 0, 1, 2},
		// Mixed: a wins on price, b vs c on latency
		{100, 200, 200, 500, 400, 500, 0, 0, 0},
		// Equal a and b, strict c: incomparable-tie edge case
		{100, 100, 200, 500, 500, 500, 0, 0, 0},
		// All three identical: fully incomparable triple
		{100, 100, 100, 500, 500, 500, 0, 0, 0},
	} {
		f.Add(
			seed.priceA, seed.latencyA, seed.offsetA,
			seed.priceB, seed.latencyB, seed.offsetB,
			seed.priceC, seed.latencyC, seed.offsetC,
		)
	}

	f.Fuzz(func(t *testing.T,
		priceA uint64, latencyA uint32, offsetA int64,
		priceB uint64, latencyB uint32, offsetB int64,
		priceC uint64, latencyC uint32, offsetC int64,
	) {
		a := orderingBid("a", normalizeBidPrice(priceA), normalizeBidLatency(latencyA), normalizeBidTime(offsetA))
		b := orderingBid("b", normalizeBidPrice(priceB), normalizeBidLatency(latencyB), normalizeBidTime(offsetB))
		c := orderingBid("c", normalizeBidPrice(priceC), normalizeBidLatency(latencyC), normalizeBidTime(offsetC))

		// Transitivity of strict partial order: A<B and B<C implies A<C.
		if a.BetterThan(b) && b.BetterThan(c) {
			require.True(t, a.BetterThan(c),
				"transitivity violated: a.BetterThan(b) && b.BetterThan(c) "+
					"but NOT a.BetterThan(c). Any iterative winner-selection "+
					"helper would produce order-dependent winners. "+
					"a=%+v b=%+v c=%+v", a, b, c)
		}

		// Reverse-direction transitivity (same invariant, swap roles).
		if c.BetterThan(b) && b.BetterThan(a) {
			require.True(t, c.BetterThan(a),
				"reverse transitivity violated: "+
					"c.BetterThan(b) && b.BetterThan(a) but NOT "+
					"c.BetterThan(a). a=%+v b=%+v c=%+v", a, b, c)
		}

		// Permutation-stability check: bestBidByComparator ONLY yields
		// a deterministic winner when the triple is TOTALLY ORDERED
		// (every pair is strictly comparable). When any two bids tie
		// on all ordering keys, the comparator is a strict partial
		// order — incomparable pairs are valid and the iteration
		// order can legitimately pick either tied bid. We detect
		// this case and skip the permutation-match assertion.
		totallyOrdered := func(x, y SpotBid) bool {
			return x.BetterThan(y) || y.BetterThan(x)
		}
		hasTie := !totallyOrdered(a, b) || !totallyOrdered(a, c) || !totallyOrdered(b, c)
		if hasTie {
			// Incomparable pair exists — order-dependent winner is
			// expected behavior of the partial order. Transitivity
			// above is the stronger invariant and still holds.
			return
		}

		// Totally-ordered triple: all permutations MUST produce the
		// same winning bid ID. This validates that bestBidByComparator
		// respects transitivity when ties are absent.
		permutations := [][]SpotBid{
			{a, b, c}, {a, c, b},
			{b, a, c}, {b, c, a},
			{c, a, b}, {c, b, a},
		}
		firstBest := bestBidByComparator(permutations[0])
		for i, perm := range permutations[1:] {
			best := bestBidByComparator(perm)
			require.Equal(t, firstBest.ID, best.ID,
				"totally-ordered triple produced different winner on "+
					"permutation %d: expected %q got %q — transitivity "+
					"broken over {a, b, c}",
				i+1, firstBest.ID, best.ID)
		}
	})
}

func FuzzSpotBidBetterThanOrderingInvariants(f *testing.F) {
	for _, seed := range []struct {
		priceA   uint64
		latencyA uint32
		offsetA  int64
		priceB   uint64
		latencyB uint32
		offsetB  int64
	}{
		{100, 500, 0, 200, 500, 0},
		{100, 250, 0, 100, 500, 0},
		{100, 500, -1, 100, 500, 1},
		{100, 500, 0, 100, 500, 0},
		{1_000_000, 1, 10, 1, 9_999, -10},
	} {
		f.Add(seed.priceA, seed.latencyA, seed.offsetA, seed.priceB, seed.latencyB, seed.offsetB)
	}

	f.Fuzz(func(t *testing.T, priceA uint64, latencyA uint32, offsetA int64, priceB uint64, latencyB uint32, offsetB int64) {
		a := orderingBid("a", normalizeBidPrice(priceA), normalizeBidLatency(latencyA), normalizeBidTime(offsetA))
		b := orderingBid("b", normalizeBidPrice(priceB), normalizeBidLatency(latencyB), normalizeBidTime(offsetB))

		require.Equal(t, expectedBetterBid(a, b), a.BetterThan(b))
		require.Equal(t, expectedBetterBid(b, a), b.BetterThan(a))

		require.False(t, a.BetterThan(a), "a bid must not be strictly better than itself")
		if a.BetterThan(b) && b.BetterThan(a) {
			t.Fatalf("bid ordering is not antisymmetric: a=%+v b=%+v", a, b)
		}

		best := bestBidByComparator([]SpotBid{a, b})
		if b.BetterThan(a) {
			require.Equal(t, b.ID, best.ID)
		} else {
			require.Equal(t, a.ID, best.ID)
		}
	})
}

func orderingBid(id string, price int64, latency uint32, submittedAt time.Time) SpotBid {
	return SpotBid{
		ID:          id,
		AuctionID:   "auction-ordering",
		Bidder:      "lumera1qypqxpq9qcrsszg2pvxq6rs0zqg3yyczxf0zx",
		Price:       sdk.NewInt64Coin(DefaultCreditDenom, price),
		LatencyMs:   latency,
		SubmittedAt: submittedAt,
	}
}

func bestBidByComparator(bids []SpotBid) SpotBid {
	best := bids[0]
	for _, bid := range bids[1:] {
		if bid.BetterThan(best) {
			best = bid
		}
	}
	return best
}

func expectedBetterBid(a, b SpotBid) bool {
	if !b.Price.IsValid() || b.Price.IsZero() {
		return true
	}
	if a.Price.Amount.LT(b.Price.Amount) {
		return true
	}
	if a.Price.Amount.GT(b.Price.Amount) {
		return false
	}
	if a.LatencyMs < b.LatencyMs {
		return true
	}
	if a.LatencyMs > b.LatencyMs {
		return false
	}
	return a.SubmittedAt.Before(b.SubmittedAt)
}

func normalizeBidPrice(price uint64) int64 {
	return int64(price%1_000_000) + 1
}

func normalizeBidLatency(latency uint32) uint32 {
	return latency%10_000 + 1
}

func normalizeBidTime(offset int64) time.Time {
	const maxOffsetSeconds = int64(86_400)
	normalized := offset % maxOffsetSeconds
	return time.Unix(1_700_000_000, 0).UTC().Add(time.Duration(normalized) * time.Second)
}
