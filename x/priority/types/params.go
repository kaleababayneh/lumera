package types

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const maxAuctionTTLMs = uint64(math.MaxInt64 / int64(time.Millisecond))

// Tier describes priority configuration for policies.
type Tier struct {
	Name            string `json:"name"`
	MaxLatencyMs    uint32 `json:"max_latency_ms"`
	AuctionTTLMs    uint64 `json:"auction_ttl_ms"`
	SpotDiscountBps uint32 `json:"spot_discount_bps"`
	Description     string `json:"description"`

	// QueueWeight is the multiplier for queue advancement (1x=100, 2x=200, etc.).
	// Higher weight means faster queue progression.
	QueueWeight uint32 `json:"queue_weight"`

	// PricingMultiplierBps is the cost multiplier in basis points (100 = 1x, 150 = 1.5x, 250 = 2.5x).
	// Applied to base tool pricing for this tier.
	PricingMultiplierBps uint32 `json:"pricing_multiplier_bps"`

	// ReservedCapacityBps is the percentage of total capacity reserved for this tier (in basis points).
	// E.g., 2000 = 20% reserved capacity. Tiers can use lower-tier capacity if their reserved is full.
	ReservedCapacityBps uint32 `json:"reserved_capacity_bps"`
}

// Params houses global tier configuration.
type Params struct {
	DefaultTier string `json:"default_tier"`
	Tiers       []Tier `json:"tiers"`
}

// DefaultParams returns Standard/Priority/Express/Enterprise tier config per specs.
func DefaultParams() *Params {
	return &Params{
		DefaultTier: "standard",
		Tiers: []Tier{
			{
				Name:                 "standard",
				MaxLatencyMs:         2_500,
				AuctionTTLMs:         uint64((45 * time.Second) / time.Millisecond),
				SpotDiscountBps:      0,
				QueueWeight:          100,  // 1x queue advancement
				PricingMultiplierBps: 100,  // 1x pricing
				ReservedCapacityBps:  7000, // 70% capacity (remaining after higher tiers)
				Description:          "Default queue position, standard pricing",
			},
			{
				Name:                 "priority",
				MaxLatencyMs:         1_800,
				AuctionTTLMs:         uint64((35 * time.Second) / time.Millisecond),
				SpotDiscountBps:      100,
				QueueWeight:          200,  // 2x queue advancement
				PricingMultiplierBps: 150,  // 1.5x pricing
				ReservedCapacityBps:  1000, // 10% reserved capacity
				Description:          "Preferred lane with faster queue progression",
			},
			{
				Name:                 "express",
				MaxLatencyMs:         1_200,
				AuctionTTLMs:         uint64((25 * time.Second) / time.Millisecond),
				SpotDiscountBps:      200,
				QueueWeight:          400,  // 4x queue advancement
				PricingMultiplierBps: 250,  // 2.5x pricing
				ReservedCapacityBps:  2000, // 20% reserved capacity
				Description:          "Express lane with reserved capacity pool",
			},
			{
				Name:                 "enterprise",
				MaxLatencyMs:         800,
				AuctionTTLMs:         uint64((15 * time.Second) / time.Millisecond),
				SpotDiscountBps:      300,
				QueueWeight:          1000, // 10x queue advancement
				PricingMultiplierBps: 0,    // Custom pricing (negotiated)
				ReservedCapacityBps:  0,    // Dedicated pools (100% allocation, managed separately)
				Description:          "Dedicated capacity pools with custom SLAs",
			},
		},
	}
}

// ValidateBasic ensures params consistency.
func (p *Params) ValidateBasic() error {
	if p == nil {
		return fmt.Errorf("params cannot be nil")
	}
	defaultTier := strings.TrimSpace(p.DefaultTier)
	if defaultTier == "" {
		return fmt.Errorf("default tier required")
	}
	if defaultTier != p.DefaultTier {
		return fmt.Errorf("default tier cannot contain leading or trailing whitespace")
	}
	if len(p.Tiers) == 0 {
		return fmt.Errorf("tiers required")
	}
	names := map[string]struct{}{}
	for _, tier := range p.Tiers {
		if err := tier.ValidateBasic(); err != nil {
			return err
		}
		if _, exists := names[tier.Name]; exists {
			return fmt.Errorf("duplicate tier name: %s", tier.Name)
		}
		names[tier.Name] = struct{}{}
	}
	if _, exists := names[p.DefaultTier]; !exists {
		return fmt.Errorf("default tier %s missing", p.DefaultTier)
	}
	if total := p.TotalReservedCapacityBps(); total > 10_000 {
		return fmt.Errorf("total reserved capacity %d bps exceeds 100%% (10000 bps)", total)
	}
	return nil
}

// ValidateBasic for Tier.
func (t Tier) ValidateBasic() error {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return fmt.Errorf("tier name required")
	}
	if name != t.Name {
		return fmt.Errorf("tier name cannot contain leading or trailing whitespace")
	}
	if t.MaxLatencyMs == 0 {
		return fmt.Errorf("tier %s max latency must be >0", t.Name)
	}
	if t.AuctionTTLMs == 0 {
		return fmt.Errorf("tier %s auction ttl must be >0", t.Name)
	}
	if t.AuctionTTLMs > maxAuctionTTLMs {
		return fmt.Errorf("tier %s auction ttl exceeds maximum safe duration milliseconds (%d)", t.Name, maxAuctionTTLMs)
	}
	if t.SpotDiscountBps > 10_000 {
		return fmt.Errorf("tier %s spot discount invalid (max 10000 bps)", t.Name)
	}
	if t.QueueWeight == 0 {
		return fmt.Errorf("tier %s queue weight must be >0", t.Name)
	}
	if t.PricingMultiplierBps > 10_000 {
		return fmt.Errorf("tier %s pricing multiplier invalid (max 10000 bps)", t.Name)
	}
	if t.ReservedCapacityBps > 10_000 {
		return fmt.Errorf("tier %s reserved capacity invalid (max 10000 bps)", t.Name)
	}
	return nil
}

// FindTier returns tier by name.
func (p *Params) FindTier(name string) (Tier, bool) {
	if p == nil {
		return Tier{}, false
	}
	for _, tier := range p.Tiers {
		if tier.Name == name {
			return tier, true
		}
	}
	return Tier{}, false
}

// DefaultTierConfig returns default tier record.
func (p *Params) DefaultTierConfig() Tier {
	tier, ok := p.FindTier(p.DefaultTier)
	if !ok {
		return Tier{
			Name:                 "standard",
			MaxLatencyMs:         2_500,
			AuctionTTLMs:         uint64((45 * time.Second) / time.Millisecond),
			QueueWeight:          100,
			PricingMultiplierBps: 100,
			ReservedCapacityBps:  7000,
		}
	}
	return tier
}

// TotalReservedCapacityBps computes sum of reserved capacity across all tiers.
// Used to validate that total doesn't exceed 100% (10000 bps).
func (p *Params) TotalReservedCapacityBps() uint64 {
	if p == nil {
		return 0
	}
	var total uint64
	for _, tier := range p.Tiers {
		total += uint64(tier.ReservedCapacityBps)
	}
	return total
}

// CapacityAllocation describes the absolute capacity allocated to a tier.
type CapacityAllocation struct {
	TierName         string
	ReservedCapacity uint64 // absolute capacity reserved for this tier
	TotalCapacity    uint64 // total system capacity (for reference)
}

// ComputeCapacityAllocations calculates absolute capacity for each tier given total system capacity.
// totalCapacity is the total number of concurrent slots/workers available.
// Returns allocations for each tier in the order they appear in Params.Tiers.
func (p *Params) ComputeCapacityAllocations(totalCapacity uint64) []CapacityAllocation {
	if p == nil || totalCapacity == 0 {
		return nil
	}
	allocations := make([]CapacityAllocation, 0, len(p.Tiers))
	for _, tier := range p.Tiers {
		reserved := (totalCapacity * uint64(tier.ReservedCapacityBps)) / 10_000
		allocations = append(allocations, CapacityAllocation{
			TierName:         tier.Name,
			ReservedCapacity: reserved,
			TotalCapacity:    totalCapacity,
		})
	}
	return allocations
}
