package types

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Deep metamorphic tests for the SpotBid BetterThan comparator, beyond
// the three strict-partial-order properties (irreflexivity, antisymmetry,
// transitivity) already covered by spotcall_test.go and the existing
// bid_ordering_fuzz_test.go.
//
// These tests encode metamorphic relations (MRs) — input transformations
// under which the output must change in a predictable way, or not at all.
// MRs catch comparator bugs that unit tests miss because there is no
// obvious "correct" answer for a random set of bids — but there IS a
// correct INVARIANT: e.g. shifting every clock by the same delta must
// preserve ordering, or removing a dominated bid must not change the
// winner.
//
// Why this matters
// ----------------
// BetterThan is the tie-break spine of the auction module. Every
// auction with 3+ bidders relies on a sort-stable comparator; a
// violation of any of these MRs produces non-deterministic auction
// outcomes that depend on sort pivot choice — the chain would fork on
// any block containing an auction settlement.
//
// The MRs covered below:
//   1. Full sort-order stability under input permutation (not just best)
//   2. Monotonic degradation: degrading a bid cannot raise its ranking
//   3. Monotonic improvement: improving a bid cannot lower its ranking
//   4. Uniform price-shift invariance: +delta to all prices preserves ranking
//   5. Uniform clock-shift invariance: +delta to all SubmittedAt preserves ranking
//   6. Dominated-bid removal invariance: removing a strictly-worse bid
//      preserves the winner
//   7. Duplicate-insertion invariance: inserting a duplicate of the
//      winner preserves the winner set (modulo tiebreak semantics)
//   8. Worst-bid symmetry: "worst" defined via BetterThan must lose
//      to every other bid in a strict ranking chain

func metamorphicBid(id string, price int64, latency uint32, submittedAt time.Time) SpotBid {
	return SpotBid{
		ID:          id,
		AuctionID:   "auction-metamorphic",
		Bidder:      "lumera1qypqxpq9qcrsszg2pvxq6rs0zqg3yyczxf0zx",
		Price:       sdk.NewInt64Coin(DefaultCreditDenom, price),
		LatencyMs:   latency,
		SubmittedAt: submittedAt,
	}
}

// sortBids returns a sorted copy of the input. Uses BetterThan as the
// less-than relation, matching the production sort.Slice usage. If
// BetterThan is non-transitive, sort.Slice is UB — these tests surface
// that.
func sortBids(bids []SpotBid) []SpotBid {
	sorted := make([]SpotBid, len(bids))
	copy(sorted, bids)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].BetterThan(sorted[j])
	})
	return sorted
}

// idsOf extracts the ID slice from a bid slice for easy comparison.
func idsOf(bids []SpotBid) []string {
	out := make([]string, len(bids))
	for i, b := range bids {
		out[i] = b.ID
	}
	return out
}

// TestSpotBid_MR_SortOrderPermutationStability proves that sort order is
// stable across input permutations — not just the "best" (tested by
// pane 5) but the FULL ranked list. Any comparator bug that breaks
// transitivity in edge cases (rounding, tiebreaker chain) makes
// sort.Slice produce different results for the same input set.
func TestSpotBid_MR_SortOrderPermutationStability(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	// Build a set with all-distinct ordering keys so the ranking is
	// unambiguous regardless of tiebreaker chain.
	bids := []SpotBid{
		metamorphicBid("a", 100, 100, now),
		metamorphicBid("b", 200, 200, now.Add(1*time.Second)),
		metamorphicBid("c", 300, 300, now.Add(2*time.Second)),
		metamorphicBid("d", 400, 400, now.Add(3*time.Second)),
		metamorphicBid("e", 500, 500, now.Add(4*time.Second)),
	}

	canonicalOrder := idsOf(sortBids(bids))

	// Shuffle 20 times with different seeds; every permutation must
	// sort to the same ID sequence.
	for seed := int64(1); seed <= 20; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			shuffled := make([]SpotBid, len(bids))
			copy(shuffled, bids)
			rng := rand.New(rand.NewSource(seed))
			rng.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			got := idsOf(sortBids(shuffled))
			require.Equal(t, canonicalOrder, got,
				"seed %d: sort order differs from canonical — input "+
					"permutation leaks into sort result. Auction settlement "+
					"would produce different winners depending on insertion "+
					"order.", seed)
		})
	}
}

// TestSpotBid_MR_MonotonicDegradation proves that degrading a bid's
// metric (raising price, raising latency, delaying submittedAt) never
// causes it to rise in the ranking. A violation would mean the
// comparator has an inverted sense on some axis, which would silently
// let worse bids win auctions.
func TestSpotBid_MR_MonotonicDegradation(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	cases := []struct {
		name    string
		base    SpotBid
		degrade func(SpotBid) SpotBid
	}{
		{
			"raise_price",
			metamorphicBid("x", 100, 500, now),
			func(b SpotBid) SpotBid {
				b.Price = sdk.NewInt64Coin(DefaultCreditDenom, 500)
				return b
			},
		},
		{
			"raise_latency",
			metamorphicBid("x", 100, 100, now),
			func(b SpotBid) SpotBid { b.LatencyMs = 800; return b },
		},
		{
			"delay_submit_time",
			metamorphicBid("x", 100, 500, now),
			func(b SpotBid) SpotBid { b.SubmittedAt = now.Add(time.Hour); return b },
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			worse := tc.degrade(tc.base)

			// Original must be at least as good as degraded, and strictly
			// better on the dimension we degraded.
			require.True(t, tc.base.BetterThan(worse),
				"degrading %s failed to make the bid worse: base=%+v degraded=%+v",
				tc.name, tc.base, worse)
			require.False(t, worse.BetterThan(tc.base),
				"degraded bid claims to beat its original: base=%+v degraded=%+v",
				tc.base, worse)
		})
	}
}

// TestSpotBid_MR_MonotonicImprovement is the mirror: improving a
// metric never lowers the ranking. Catches comparator logic that
// accidentally flips sign when an input crosses a boundary.
func TestSpotBid_MR_MonotonicImprovement(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	cases := []struct {
		name    string
		base    SpotBid
		improve func(SpotBid) SpotBid
	}{
		{
			"lower_price",
			metamorphicBid("x", 500, 500, now),
			func(b SpotBid) SpotBid {
				b.Price = sdk.NewInt64Coin(DefaultCreditDenom, 100)
				return b
			},
		},
		{
			"lower_latency",
			metamorphicBid("x", 500, 800, now),
			func(b SpotBid) SpotBid { b.LatencyMs = 100; return b },
		},
		{
			"earlier_submit_time",
			metamorphicBid("x", 500, 500, now.Add(time.Hour)),
			func(b SpotBid) SpotBid { b.SubmittedAt = now; return b },
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			better := tc.improve(tc.base)

			require.True(t, better.BetterThan(tc.base),
				"improved bid failed to beat original: base=%+v improved=%+v",
				tc.base, better)
			require.False(t, tc.base.BetterThan(better),
				"original claims to beat its improved version: base=%+v improved=%+v",
				tc.base, better)
		})
	}
}

// TestSpotBid_MR_UniformPriceShiftPreservesOrder proves that adding the
// same non-negative amount to every bid's price preserves the relative
// ordering. Tests that price comparison uses relative, not absolute,
// value — an off-by-one floor or ceiling somewhere in the comparator
// would surface here.
func TestSpotBid_MR_UniformPriceShiftPreservesOrder(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	base := []SpotBid{
		metamorphicBid("a", 100, 500, now),
		metamorphicBid("b", 200, 300, now),
		metamorphicBid("c", 300, 100, now),
		metamorphicBid("d", 150, 200, now),
	}

	baseOrder := idsOf(sortBids(base))

	shifts := []int64{0, 1, 100, 1_000_000, 1_000_000_000}
	for _, shift := range shifts {
		shift := shift
		t.Run(fmt.Sprintf("shift_%d", shift), func(t *testing.T) {
			t.Parallel()
			shifted := make([]SpotBid, len(base))
			for i, b := range base {
				shifted[i] = b
				shifted[i].Price = sdk.NewInt64Coin(
					DefaultCreditDenom,
					b.Price.Amount.Int64()+shift,
				)
			}
			got := idsOf(sortBids(shifted))
			require.Equal(t, baseOrder, got,
				"price shift %d changed sort order — comparator is "+
					"not translation-invariant. Absolute price thresholds "+
					"would produce different auction winners for the same "+
					"relative bid distribution.", shift)
		})
	}
}

// TestSpotBid_MR_UniformClockShiftPreservesOrder proves that shifting
// every SubmittedAt by the same duration preserves ordering. A
// violation would mean the comparator treats timestamps absolutely
// rather than relatively — auction winners would depend on wall-clock
// time, not on submission order within the auction.
func TestSpotBid_MR_UniformClockShiftPreservesOrder(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	base := []SpotBid{
		metamorphicBid("a", 100, 500, now),
		metamorphicBid("b", 100, 500, now.Add(time.Second)),
		metamorphicBid("c", 100, 500, now.Add(2*time.Second)),
		metamorphicBid("d", 100, 500, now.Add(3*time.Second)),
	}

	baseOrder := idsOf(sortBids(base))

	shifts := []time.Duration{
		0,
		time.Nanosecond,
		time.Second,
		time.Hour,
		24 * time.Hour * 365, // one year
	}
	for _, shift := range shifts {
		shift := shift
		t.Run(fmt.Sprintf("shift_%s", shift), func(t *testing.T) {
			t.Parallel()
			shifted := make([]SpotBid, len(base))
			for i, b := range base {
				shifted[i] = b
				shifted[i].SubmittedAt = b.SubmittedAt.Add(shift)
			}
			got := idsOf(sortBids(shifted))
			require.Equal(t, baseOrder, got,
				"clock shift %s changed sort order — tiebreaker is not "+
					"duration-invariant. Auction winners would depend on "+
					"absolute wall-clock time.", shift)
		})
	}
}

// TestSpotBid_MR_DominatedBidRemovalPreservesWinner proves that
// removing a bid that loses to the winner does not change the winner.
// Catches comparator bugs where the winner depends on the full
// competitor set (e.g. an accidentally-averaging tiebreaker).
func TestSpotBid_MR_DominatedBidRemovalPreservesWinner(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	// "winner" beats everyone else on price; the rest vary on dimensions
	// that don't matter once the winner is chosen.
	winner := metamorphicBid("winner", 100, 500, now)
	dominated := []SpotBid{
		metamorphicBid("loser-1", 200, 100, now.Add(time.Second)),
		metamorphicBid("loser-2", 300, 200, now.Add(2*time.Second)),
		metamorphicBid("loser-3", 400, 300, now.Add(3*time.Second)),
		metamorphicBid("loser-4", 500, 400, now.Add(4*time.Second)),
	}

	allBids := append([]SpotBid{winner}, dominated...)
	fullWinner := bestBidByComparator(allBids)
	require.Equal(t, winner.ID, fullWinner.ID, "setup: winner must win the full set")

	// Remove each dominated bid one at a time and verify the winner
	// is still the same.
	for i := range dominated {
		i := i
		t.Run(fmt.Sprintf("remove_%s", dominated[i].ID), func(t *testing.T) {
			t.Parallel()
			reduced := append([]SpotBid{winner}, append([]SpotBid{}, dominated[:i]...)...)
			reduced = append(reduced, dominated[i+1:]...)

			got := bestBidByComparator(reduced)
			require.Equal(t, winner.ID, got.ID,
				"removing dominated bid %s changed the winner to %s — "+
					"winner depends on full competitor set",
				dominated[i].ID, got.ID)
		})
	}

	// Remove ALL dominated bids; winner still wins.
	t.Run("remove_all_dominated", func(t *testing.T) {
		t.Parallel()
		got := bestBidByComparator([]SpotBid{winner})
		require.Equal(t, winner.ID, got.ID)
	})
}

// TestSpotBid_MR_DuplicateInsertionPreservesWinner proves that
// inserting a bid that ties on every axis with the winner cannot cause
// a different bid to win — the winner set must be stable under
// duplicate insertion.
func TestSpotBid_MR_DuplicateInsertionPreservesWinner(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	winner := metamorphicBid("winner", 100, 500, now)
	others := []SpotBid{
		metamorphicBid("other-1", 200, 100, now.Add(time.Second)),
		metamorphicBid("other-2", 300, 200, now.Add(2*time.Second)),
	}

	// Duplicate the winner with a distinct ID but all-identical
	// ordering keys. BetterThan(winner, dup) and BetterThan(dup, winner)
	// must both be false (incomparable under partial-order semantics).
	dup := winner
	dup.ID = "winner-duplicate"

	require.False(t, winner.BetterThan(dup),
		"duplicate bids must be incomparable (neither strictly beats)")
	require.False(t, dup.BetterThan(winner),
		"duplicate bids must be incomparable (neither strictly beats)")

	// Insert the duplicate at each position in the bid list; one of
	// {winner, dup} must always win (tiebreaker may pick either, but
	// never one of the "other" bids).
	original := append([]SpotBid{winner}, others...)
	originalWinner := bestBidByComparator(original).ID
	require.Equal(t, winner.ID, originalWinner)

	for pos := 0; pos <= len(original); pos++ {
		pos := pos
		t.Run(fmt.Sprintf("insert_at_%d", pos), func(t *testing.T) {
			t.Parallel()
			withDup := make([]SpotBid, 0, len(original)+1)
			withDup = append(withDup, original[:pos]...)
			withDup = append(withDup, dup)
			withDup = append(withDup, original[pos:]...)

			got := bestBidByComparator(withDup)
			require.True(t, got.ID == winner.ID || got.ID == dup.ID,
				"inserting winner-duplicate at position %d caused %q to "+
					"win — duplicate insertion should only ever elect "+
					"the original or the duplicate",
				pos, got.ID)
		})
	}
}

// TestSpotBid_MR_WorstBidLosesToAllOthers proves that the "worst" bid
// (strictly dominated on every axis) loses to every other bid. Symmetric
// with the best-bid invariant and catches comparator bugs where the
// minimum of BetterThan doesn't align with the maximum.
func TestSpotBid_MR_WorstBidLosesToAllOthers(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	// Strict chain: higher price, higher latency, later submit time.
	bids := []SpotBid{
		metamorphicBid("best", 100, 100, now),
		metamorphicBid("middle-1", 200, 200, now.Add(1*time.Second)),
		metamorphicBid("middle-2", 300, 300, now.Add(2*time.Second)),
		metamorphicBid("middle-3", 400, 400, now.Add(3*time.Second)),
		metamorphicBid("worst", 500, 500, now.Add(4*time.Second)),
	}

	worst := bids[len(bids)-1]
	for i := 0; i < len(bids)-1; i++ {
		require.True(t, bids[i].BetterThan(worst),
			"bid %s should beat worst bid %s", bids[i].ID, worst.ID)
		require.False(t, worst.BetterThan(bids[i]),
			"worst bid %s should not beat %s", worst.ID, bids[i].ID)
	}
}

// TestSpotBid_MR_SortInvariantsAcrossSizes proves sort-order stability
// for a range of input sizes. Catches size-dependent comparator bugs
// (e.g. a hand-rolled sort that only handles 2-bid tiebreaks and falls
// back to index order for n>=3).
func TestSpotBid_MR_SortInvariantsAcrossSizes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()

	// Build a large, strictly-ordered set.
	const maxN = 20
	allBids := make([]SpotBid, maxN)
	for i := 0; i < maxN; i++ {
		allBids[i] = metamorphicBid(
			fmt.Sprintf("bid-%02d", i),
			int64(100+i*10),
			uint32(100+i*5),
			now.Add(time.Duration(i)*time.Second),
		)
	}

	// For each subset size, verify sort is permutation-stable.
	for n := 2; n <= maxN; n++ {
		n := n
		t.Run(fmt.Sprintf("n_%d", n), func(t *testing.T) {
			t.Parallel()
			subset := append([]SpotBid{}, allBids[:n]...)
			canonical := idsOf(sortBids(subset))

			// Try 5 random permutations.
			for seed := int64(1); seed <= 5; seed++ {
				shuffled := make([]SpotBid, n)
				copy(shuffled, subset)
				rng := rand.New(rand.NewSource(seed + int64(n)*100))
				rng.Shuffle(len(shuffled), func(i, j int) {
					shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
				})
				got := idsOf(sortBids(shuffled))
				require.Equal(t, canonical, got,
					"n=%d seed=%d: sort unstable at this size", n, seed)
			}
		})
	}
}
