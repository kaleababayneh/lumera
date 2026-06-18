package types

import (
	"fmt"
	"math"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// DefaultCreditDenom is the coin denom used for reserve commitments unless overridden.
	DefaultCreditDenom = "ulac"

	// MaxTiers caps the number of TierConfig entries in Params.
	// Realistic tiering is 3-5 levels (bronze/silver/gold, maybe
	// platinum/diamond). 32 is ~10x realistic and bounds gov-proposal-
	// controlled state bloat — every tier is persisted and iterated
	// on every AllocateReserve call.
	MaxTiers = 32
	// MaxTierNameLen caps an individual TierConfig.Name string.
	// Real names are short identifiers like "bronze", "gold"
	// (5-10 bytes). 64 gives generous headroom while bounding
	// adversarial gov proposals.
	MaxTierNameLen = 64

	maxTierDefaultDurationSec = uint64(math.MaxInt64 / int64(time.Second))
)

// TierConfig defines economics for a reserve tier.
type TierConfig struct {
	Name                string      `json:"name"`
	MinCommitmentAmount sdkmath.Int `json:"min_commitment_amount"`
	DiscountBps         uint32      `json:"discount_bps"`
	DefaultDurationSec  uint64      `json:"default_duration_sec"`
	MaxActivePerPolicy  uint32      `json:"max_active_per_policy"`
	RolloverAllowed     bool        `json:"rollover_allowed"`
}

// Params holds module configuration.
type Params struct {
	CreditDenom string       `json:"credit_denom"`
	Tiers       []TierConfig `json:"tiers"`
}

// DefaultParams returns baseline settings with three tiers.
func DefaultParams() *Params {
	return &Params{
		CreditDenom: DefaultCreditDenom,
		Tiers: []TierConfig{
			{
				Name:                "bronze",
				MinCommitmentAmount: sdkmath.NewInt(100_000),
				DiscountBps:         250, // 2.5%
				DefaultDurationSec:  30 * 24 * 60 * 60,
				MaxActivePerPolicy:  2,
				RolloverAllowed:     true,
			},
			{
				Name:                "silver",
				MinCommitmentAmount: sdkmath.NewInt(500_000),
				DiscountBps:         500,
				DefaultDurationSec:  60 * 24 * 60 * 60,
				MaxActivePerPolicy:  3,
				RolloverAllowed:     true,
			},
			{
				Name:                "gold",
				MinCommitmentAmount: sdkmath.NewInt(1_000_000),
				DiscountBps:         750,
				DefaultDurationSec:  90 * 24 * 60 * 60,
				MaxActivePerPolicy:  4,
				RolloverAllowed:     false,
			},
		},
	}
}

// ValidateBasic performs sanity checks on parameters.
func (p *Params) ValidateBasic() error {
	if p == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := sdk.ValidateDenom(p.CreditDenom); err != nil {
		return fmt.Errorf("invalid credit denom: %w", err)
	}
	if len(p.Tiers) == 0 {
		return fmt.Errorf("at least one tier required")
	}
	if len(p.Tiers) > MaxTiers {
		return fmt.Errorf("tiers exceeds %d-entry cap (got %d)", MaxTiers, len(p.Tiers))
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
	return nil
}

// ValidateBasic checks consistency of a TierConfig.
func (t TierConfig) ValidateBasic() error {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return fmt.Errorf("tier name required")
	}
	if name != t.Name {
		return fmt.Errorf("tier name cannot contain leading or trailing whitespace")
	}
	if len(t.Name) > MaxTierNameLen {
		return fmt.Errorf("tier name exceeds %d-byte cap (got %d)",
			MaxTierNameLen, len(t.Name))
	}
	if !t.MinCommitmentAmount.IsPositive() {
		return fmt.Errorf("tier %s requires positive minimum commitment", t.Name)
	}
	if t.DiscountBps > 10_000 {
		return fmt.Errorf("tier %s discount exceeds 100%%", t.Name)
	}
	if t.DefaultDurationSec == 0 {
		return fmt.Errorf("tier %s requires non-zero duration", t.Name)
	}
	if t.DefaultDurationSec > maxTierDefaultDurationSec {
		return fmt.Errorf("tier %s default duration exceeds maximum safe duration seconds (%d)", t.Name, maxTierDefaultDurationSec)
	}
	if t.MaxActivePerPolicy == 0 {
		return fmt.Errorf("tier %s requires max active per policy > 0", t.Name)
	}
	return nil
}

// FindTier returns tier config by name.
func (p *Params) FindTier(name string) (TierConfig, bool) {
	if p == nil {
		return TierConfig{}, false
	}
	for _, tier := range p.Tiers {
		if tier.Name == name {
			return tier, true
		}
	}
	return TierConfig{}, false
}
