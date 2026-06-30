package types

import (
	"testing"
)

// TestComputeCapacityAllocations_SumBoundInvariantMetamorphic asserts
// the spec property that the sum of per-tier ReservedCapacity never
// exceeds totalCapacity. ValidateBasic caps TotalReservedCapacityBps
// at 10_000 (100%). With validated params, the integer-division used
// for per-tier allocation cannot over-allocate. This is load-bearing
// because a regression that changed the rounding mode (e.g. ceil-
// rounded) could push the sum beyond totalCapacity and oversubscribe
// the scheduler.
func TestComputeCapacityAllocations_SumBoundInvariantMetamorphic(t *testing.T) {
	t.Parallel()
	params := DefaultParams()
	if err := params.ValidateBasic(); err != nil {
		t.Fatalf("DefaultParams should validate: %v", err)
	}
	for _, total := range []uint64{0, 1, 50, 99, 100, 127, 1000, 10_000, 1_234_567} {
		allocs := params.ComputeCapacityAllocations(total)
		if total == 0 {
			if allocs != nil {
				t.Fatalf("zero-capacity must return nil, got %+v", allocs)
			}
			continue
		}
		var sum uint64
		for _, a := range allocs {
			sum += a.ReservedCapacity
		}
		if sum > total {
			t.Fatalf("sum reserved %d > totalCapacity %d at total=%d (allocs=%+v)",
				sum, total, total, allocs)
		}
	}
}

// TestComputeCapacityAllocations_OrderPreservationMetamorphic pins
// that the allocation slice is returned in the same order as the
// Params.Tiers slice. Schedulers iterate tier → allocation by index
// assuming this; a regression that re-sorted (e.g. by capacity or
// name) would break that pairing and misroute traffic.
func TestComputeCapacityAllocations_OrderPreservationMetamorphic(t *testing.T) {
	t.Parallel()
	params := DefaultParams()
	allocs := params.ComputeCapacityAllocations(10_000)
	if len(allocs) != len(params.Tiers) {
		t.Fatalf("len(allocs)=%d, len(tiers)=%d", len(allocs), len(params.Tiers))
	}
	for i, tier := range params.Tiers {
		if allocs[i].TierName != tier.Name {
			t.Fatalf("tier[%d]: alloc.TierName=%q, want %q", i, allocs[i].TierName, tier.Name)
		}
		if allocs[i].TotalCapacity != 10_000 {
			t.Fatalf("tier[%d]: TotalCapacity=%d, want 10000",
				i, allocs[i].TotalCapacity)
		}
	}
}

// TestComputeCapacityAllocations_ScaleLinearityMetamorphic asserts
// that when totalCapacity is chosen so 10_000 bps divides it cleanly
// (i.e. totalCapacity % 10_000 == 0), scaling totalCapacity by k
// scales every reserved allocation by k exactly. This rules out
// regressions that inserted a per-tier floor, ceiling, or non-linear
// transform on top of the straight integer-division mapping.
func TestComputeCapacityAllocations_ScaleLinearityMetamorphic(t *testing.T) {
	t.Parallel()
	params := DefaultParams()
	const base uint64 = 10_000 // bps-clean
	baseAllocs := params.ComputeCapacityAllocations(base)
	for _, k := range []uint64{2, 3, 5, 10, 100} {
		scaledAllocs := params.ComputeCapacityAllocations(base * k)
		if len(scaledAllocs) != len(baseAllocs) {
			t.Fatalf("length differs at k=%d: base=%d scaled=%d", k, len(baseAllocs), len(scaledAllocs))
		}
		for i := range baseAllocs {
			if scaledAllocs[i].ReservedCapacity != baseAllocs[i].ReservedCapacity*k {
				t.Fatalf("scale-linearity violated for tier %q at k=%d: base=%d scaled=%d (want %d)",
					baseAllocs[i].TierName, k,
					baseAllocs[i].ReservedCapacity,
					scaledAllocs[i].ReservedCapacity,
					baseAllocs[i].ReservedCapacity*k)
			}
		}
	}
}

// TestComputeCapacityAllocations_ZeroBpsMeansZeroReservationMetamorphic
// asserts that a tier with ReservedCapacityBps == 0 always gets zero
// reserved slots, regardless of totalCapacity. The enterprise tier
// in DefaultParams has this property (it uses dedicated pools); a
// regression that assigned "at least 1 slot" as a minimum would
// quietly siphon capacity away from the scheduled tiers.
func TestComputeCapacityAllocations_ZeroBpsMeansZeroReservationMetamorphic(t *testing.T) {
	t.Parallel()
	params := DefaultParams()
	for _, total := range []uint64{1, 100, 10_000, 1_000_000, 1 << 30} {
		allocs := params.ComputeCapacityAllocations(total)
		for i, a := range allocs {
			if params.Tiers[i].ReservedCapacityBps == 0 && a.ReservedCapacity != 0 {
				t.Fatalf("tier %q has 0 bps but got %d reserved at total=%d",
					a.TierName, a.ReservedCapacity, total)
			}
		}
	}
}

// TestComputeCapacityAllocations_PerTierFormulaMetamorphic pins the
// documented formula: ReservedCapacity = total * bps / 10_000 with
// integer division. Exercised across a two-tier param set with
// representative splits. Rejects any change that reordered the
// operations (e.g. total/10_000*bps would lose precision for total <
// 10_000).
func TestComputeCapacityAllocations_PerTierFormulaMetamorphic(t *testing.T) {
	t.Parallel()
	p := &Params{
		DefaultTier: "a",
		Tiers: []Tier{
			{Name: "a", MaxLatencyMs: 1, AuctionTTLMs: 1, QueueWeight: 1, ReservedCapacityBps: 3333},
			{Name: "b", MaxLatencyMs: 1, AuctionTTLMs: 1, QueueWeight: 1, ReservedCapacityBps: 6667},
		},
	}
	for _, total := range []uint64{0, 1, 10, 100, 1000, 9_999, 10_000, 50_000, 1_000_001} {
		allocs := p.ComputeCapacityAllocations(total)
		if total == 0 {
			if allocs != nil {
				t.Fatalf("zero capacity must return nil, got %+v", allocs)
			}
			continue
		}
		for i, a := range allocs {
			want := (total * uint64(p.Tiers[i].ReservedCapacityBps)) / 10_000
			if a.ReservedCapacity != want {
				t.Fatalf("tier %q total=%d: ReservedCapacity=%d, want %d",
					a.TierName, total, a.ReservedCapacity, want)
			}
		}
	}
}

// TestTotalReservedCapacityBps_AdditivityMetamorphic asserts that
// adding a tier to Params increases TotalReservedCapacityBps by
// exactly that tier's ReservedCapacityBps. Validators rely on the
// 10_000-bps cap applied to this sum; a regression that e.g.
// max()'d instead of summed would silently accept overflow
// configurations that oversubscribe the scheduler.
func TestTotalReservedCapacityBps_AdditivityMetamorphic(t *testing.T) {
	t.Parallel()
	p := &Params{
		DefaultTier: "a",
		Tiers: []Tier{
			{Name: "a", MaxLatencyMs: 1, AuctionTTLMs: 1, QueueWeight: 1, ReservedCapacityBps: 1500},
		},
	}
	base := p.TotalReservedCapacityBps()
	if base != 1500 {
		t.Fatalf("base total = %d, want 1500", base)
	}
	// Append a tier and assert the sum rises by exactly that tier's bps.
	p.Tiers = append(p.Tiers, Tier{Name: "b", MaxLatencyMs: 1, AuctionTTLMs: 1, QueueWeight: 1, ReservedCapacityBps: 2500})
	if got := p.TotalReservedCapacityBps(); got != base+2500 {
		t.Fatalf("after appending 2500 bps: got %d, want %d", got, base+2500)
	}
	// And again with a 0-bps tier — must not change the sum.
	p.Tiers = append(p.Tiers, Tier{Name: "c", MaxLatencyMs: 1, AuctionTTLMs: 1, QueueWeight: 1, ReservedCapacityBps: 0})
	if got := p.TotalReservedCapacityBps(); got != base+2500 {
		t.Fatalf("after appending 0 bps: got %d, want %d (unchanged)", got, base+2500)
	}
}

// TestTotalReservedCapacityBps_DoesNotWrapUint32 pins that the
// aggregate used by ValidateBasic is wider than the per-tier bps
// field. A uint32 accumulator can wrap on a large governance tier
// list and make an oversized reserve schedule look acceptable.
func TestTotalReservedCapacityBps_DoesNotWrapUint32(t *testing.T) {
	t.Parallel()

	const (
		perTierBps uint32 = 10_000
		maxUint32         = ^uint32(0)
	)
	tierCount := int(maxUint32/perTierBps) + 2
	p := &Params{Tiers: make([]Tier, tierCount)}
	for i := range p.Tiers {
		p.Tiers[i].ReservedCapacityBps = perTierBps
	}

	got := p.TotalReservedCapacityBps()
	want := uint64(tierCount) * uint64(perTierBps)
	if uint64(got) != want {
		t.Fatalf("total reserved capacity wrapped: got %d, want %d", got, want)
	}
	if uint64(got) <= uint64(maxUint32) {
		t.Fatalf("total reserved capacity stayed within uint32 range: got %d", got)
	}
}

// TestFindTier_OrderAndExactMatchMetamorphic asserts two contract
// properties: (1) FindTier uses exact-string match (case-sensitive);
// (2) when multiple tiers share a name (shouldn't happen post-
// validation, but FindTier is used before validation too), the first
// occurrence wins. A regression that case-folded or did prefix
// matching would let a typo'd tier lookup silently succeed with the
// wrong tier.
func TestFindTier_OrderAndExactMatchMetamorphic(t *testing.T) {
	t.Parallel()
	p := &Params{
		DefaultTier: "standard",
		Tiers: []Tier{
			{Name: "standard", MaxLatencyMs: 1000},
			{Name: "priority", MaxLatencyMs: 500},
			{Name: "STANDARD", MaxLatencyMs: 9999}, // case variant: must NOT match "standard"
			{Name: "standard", MaxLatencyMs: 7777}, // duplicate: first-occurrence wins
		},
	}
	// Exact match: gets the first "standard" (1000).
	tier, ok := p.FindTier("standard")
	if !ok {
		t.Fatal("expected FindTier to find 'standard'")
	}
	if tier.MaxLatencyMs != 1000 {
		t.Fatalf("first-occurrence tier MaxLatencyMs=%d, want 1000 (got duplicate)", tier.MaxLatencyMs)
	}
	// Case-variant lookup must match its own exact casing, not fold.
	tier, ok = p.FindTier("STANDARD")
	if !ok {
		t.Fatal("expected FindTier to find 'STANDARD' exactly")
	}
	if tier.MaxLatencyMs != 9999 {
		t.Fatalf("STANDARD MaxLatencyMs=%d, want 9999", tier.MaxLatencyMs)
	}
	// Fuzzy variants must not match.
	for _, q := range []string{"Standard", "stand", "standardx", "  standard", ""} {
		if _, ok := p.FindTier(q); ok {
			t.Fatalf("FindTier(%q) should NOT match", q)
		}
	}
}
