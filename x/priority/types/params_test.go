package types

import (
	"testing"
	"time"
)

func TestParamsValidateBasic(t *testing.T) {
	t.Parallel()

	if err := DefaultParams().ValidateBasic(); err != nil {
		t.Fatalf("expected default params to be valid, got %v", err)
	}

	var nilParams *Params
	if err := nilParams.ValidateBasic(); err == nil {
		t.Fatalf("expected error for nil params")
	}

	missingDefault := DefaultParams()
	missingDefault.DefaultTier = ""
	if err := missingDefault.ValidateBasic(); err == nil {
		t.Fatalf("expected error for missing default tier")
	}

	paddedDefault := DefaultParams()
	paddedDefault.DefaultTier = " standard "
	if err := paddedDefault.ValidateBasic(); err == nil {
		t.Fatalf("expected error for padded default tier")
	}

	duplicate := DefaultParams()
	duplicate.Tiers = append(duplicate.Tiers, duplicate.Tiers[0])
	if err := duplicate.ValidateBasic(); err == nil {
		t.Fatalf("expected error for duplicate tier name")
	}

	excess := DefaultParams()
	excess.Tiers[0].ReservedCapacityBps = 10_000
	excess.Tiers[1].ReservedCapacityBps = 10_000
	if err := excess.ValidateBasic(); err == nil {
		t.Fatalf("expected error for reserved capacity over 10000 bps")
	}

	noTiers := &Params{
		DefaultTier: "standard",
		Tiers:       []Tier{},
	}
	if err := noTiers.ValidateBasic(); err == nil {
		t.Fatalf("expected error for empty tiers")
	}
}

func TestTierValidateBasic(t *testing.T) {
	t.Parallel()

	tier := DefaultParams().Tiers[0]
	if err := tier.ValidateBasic(); err != nil {
		t.Fatalf("expected tier to be valid, got %v", err)
	}

	tests := []struct {
		name    string
		modify  func(*Tier)
		wantErr bool
	}{
		{
			name:    "empty name",
			modify:  func(t *Tier) { t.Name = "" },
			wantErr: true,
		},
		{
			name:    "padded name",
			modify:  func(t *Tier) { t.Name = " standard " },
			wantErr: true,
		},
		{
			name:    "zero max latency",
			modify:  func(t *Tier) { t.MaxLatencyMs = 0 },
			wantErr: true,
		},
		{
			name:    "zero auction ttl",
			modify:  func(t *Tier) { t.AuctionTTLMs = 0 },
			wantErr: true,
		},
		{
			name:    "auction ttl exceeds time.Duration limit",
			modify:  func(t *Tier) { t.AuctionTTLMs = maxAuctionTTLMs + 1 },
			wantErr: true,
		},
		{
			name:    "spot discount over 10000 bps",
			modify:  func(t *Tier) { t.SpotDiscountBps = 10001 },
			wantErr: true,
		},
		{
			name:    "zero queue weight",
			modify:  func(t *Tier) { t.QueueWeight = 0 },
			wantErr: true,
		},
		{
			name:    "pricing multiplier over 10000 bps",
			modify:  func(t *Tier) { t.PricingMultiplierBps = 10001 },
			wantErr: true,
		},
		{
			name:    "reserved capacity over 10000 bps",
			modify:  func(t *Tier) { t.ReservedCapacityBps = 10001 },
			wantErr: true,
		},
		{
			name:    "spot discount at boundary",
			modify:  func(t *Tier) { t.SpotDiscountBps = 10000 },
			wantErr: false,
		},
		{
			name:    "pricing multiplier at boundary",
			modify:  func(t *Tier) { t.PricingMultiplierBps = 10000 },
			wantErr: false,
		},
		{
			name:    "reserved capacity at boundary",
			modify:  func(t *Tier) { t.ReservedCapacityBps = 10000 },
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			modified := tier
			tc.modify(&modified)
			err := modified.ValidateBasic()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.name, err)
			}
		})
	}
}

func TestFindTierAndDefaults(t *testing.T) {
	t.Parallel()

	params := DefaultParams()
	if _, ok := params.FindTier("standard"); !ok {
		t.Fatalf("expected to find standard tier")
	}
	if _, ok := params.FindTier("missing"); ok {
		t.Fatalf("did not expect to find missing tier")
	}

	params.DefaultTier = "missing"
	fallback := params.DefaultTierConfig()
	if fallback.Name == "" {
		t.Fatalf("expected fallback tier name")
	}
}

// TestDefaultTierConfig_ResolvesToConfiguredTier pins the happy
// path: when DefaultTier names a tier that exists in the Tiers
// slice, DefaultTierConfig returns THAT tier verbatim rather than
// the hardcoded fallback. Regression guard against a refactor that
// accidentally always returns the fallback (a collapse that would
// silently give every passport the same priority settings
// regardless of governance configuration).
func TestDefaultTierConfig_ResolvesToConfiguredTier(t *testing.T) {
	t.Parallel()
	params := DefaultParams()
	params.DefaultTier = "express" // one of the seeded tiers

	got := params.DefaultTierConfig()
	if got.Name != "express" {
		t.Fatalf("DefaultTierConfig name=%q; want 'express' (configured default)", got.Name)
	}
	// Spot-check a distinctive field to prove it's NOT the fallback.
	// Express has MaxLatencyMs=1200; fallback has 2500.
	if got.MaxLatencyMs != 1_200 {
		t.Errorf("Express MaxLatencyMs=%d; want 1200 (NOT fallback's 2500)", got.MaxLatencyMs)
	}
	if got.QueueWeight != 400 {
		t.Errorf("Express QueueWeight=%d; want 400 (NOT fallback's 100)", got.QueueWeight)
	}
}

// TestDefaultTierConfig_FallbackValuesPinnedExactly pins the exact
// hardcoded fallback values returned when DefaultTier is unset or
// doesn't match any configured tier. The existing companion test
// only checks Name != "" — this fills the gap so a refactor that
// silently changed any of the fallback numbers (e.g. flipped
// QueueWeight from 100 to 200) surfaces as a test failure. These
// values represent the "baseline safe defaults" downstream
// callers assume when governance configuration is broken, so they
// must stay stable until changed intentionally.
func TestDefaultTierConfig_FallbackValuesPinnedExactly(t *testing.T) {
	t.Parallel()
	params := DefaultParams()
	params.DefaultTier = "nonexistent-tier"
	fb := params.DefaultTierConfig()

	if fb.Name != "standard" {
		t.Errorf("fallback Name = %q; want 'standard'", fb.Name)
	}
	if fb.MaxLatencyMs != 2_500 {
		t.Errorf("fallback MaxLatencyMs = %d; want 2500", fb.MaxLatencyMs)
	}
	// Fallback AuctionTTLMs is 45 seconds in ms.
	if fb.AuctionTTLMs != uint64(45*time.Second/time.Millisecond) {
		t.Errorf("fallback AuctionTTLMs = %d; want %d (45s)", fb.AuctionTTLMs, uint64(45*time.Second/time.Millisecond))
	}
	if fb.QueueWeight != 100 {
		t.Errorf("fallback QueueWeight = %d; want 100 (1x baseline)", fb.QueueWeight)
	}
	if fb.PricingMultiplierBps != 100 {
		t.Errorf("fallback PricingMultiplierBps = %d; want 100 (1x baseline)", fb.PricingMultiplierBps)
	}
	if fb.ReservedCapacityBps != 7000 {
		t.Errorf("fallback ReservedCapacityBps = %d; want 7000 (70%% baseline)", fb.ReservedCapacityBps)
	}
}

func TestComputeCapacityAllocations(t *testing.T) {
	t.Parallel()

	params := &Params{
		DefaultTier: "standard",
		Tiers: []Tier{
			{Name: "standard", ReservedCapacityBps: 2500},
			{Name: "priority", ReservedCapacityBps: 7500},
		},
	}

	allocations := params.ComputeCapacityAllocations(100)
	if len(allocations) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(allocations))
	}
	if allocations[0].ReservedCapacity != 25 {
		t.Fatalf("expected standard reserved 25, got %d", allocations[0].ReservedCapacity)
	}
	if allocations[1].ReservedCapacity != 75 {
		t.Fatalf("expected priority reserved 75, got %d", allocations[1].ReservedCapacity)
	}
}

func TestComputeCapacityAllocationsEdgeCases(t *testing.T) {
	t.Parallel()

	// nil params
	var nilParams *Params
	allocations := nilParams.ComputeCapacityAllocations(100)
	if allocations != nil {
		t.Fatalf("expected nil allocations for nil params")
	}

	// zero capacity
	params := DefaultParams()
	allocations = params.ComputeCapacityAllocations(0)
	if allocations != nil {
		t.Fatalf("expected nil allocations for zero capacity")
	}
}

func TestTotalReservedCapacityBps(t *testing.T) {
	t.Parallel()

	params := DefaultParams()
	total := params.TotalReservedCapacityBps()
	// standard(7000) + priority(1000) + express(2000) + enterprise(0) = 10000
	if total != 10000 {
		t.Fatalf("expected total 10000, got %d", total)
	}

	var nilParams *Params
	if nilParams.TotalReservedCapacityBps() != 0 {
		t.Fatalf("expected 0 for nil params")
	}
}

func TestFindTierNilParams(t *testing.T) {
	t.Parallel()

	var nilParams *Params
	_, ok := nilParams.FindTier("standard")
	if ok {
		t.Fatalf("expected not found for nil params")
	}
}
