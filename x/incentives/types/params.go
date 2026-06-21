// Package types holds shared types and helpers for the incentives module.
//
//revive:disable:var-naming // Package name aligns with Cosmos SDK module layout.
package types

import (
	"fmt"
	"time"
)

// TargetBlockTime is the assumed wall-clock duration of a single block used
// when translating block-count parameters into human-readable durations and
// when computing badge ExpiresAt timestamps. Keep this in lockstep with the
// comments on block-count constants below and with the estimatedBlockTime
// constant in keeper.evaluateAndIssueBadge.
const TargetBlockTime = 6 * time.Second

const (
	// DefaultEvaluationIntervalBlocks is the default number of blocks between badge evaluations.
	//
	// All "~N days/hours" comments in this file assume a 6-second target block
	// time, matching the constant used by keeper.evaluateAndIssueBadge to
	// translate ValidityPeriodBlocks into an ExpiresAt timestamp. Do not change
	// this estimate in isolation — update both places together.
	DefaultEvaluationIntervalBlocks = 1000 // ~1.67 hours at 6s blocks
	// DefaultGracePeriodBlocks is the default grace period before a badge downgrade takes effect.
	DefaultGracePeriodBlocks = 500 // ~50 minutes at 6s blocks
	// DefaultMinInvocationsForScoring is the minimum invocations required before scoring.
	DefaultMinInvocationsForScoring = 10
	// DefaultScoringWindowBlocks is the default number of blocks to consider for scoring.
	DefaultScoringWindowBlocks = 50000 // ~3.5 days at 6s blocks

	// BasisPointsScale is the scaling factor for basis point values (10000 = 100%).
	BasisPointsScale = 10000
	// MaxScore is the maximum composite score value.
	MaxScore = 10000 // Represents 100.00
	// maxDurationBlocks is the largest block count that can be safely
	// represented as a time.Duration before multiplying by TargetBlockTime.
	maxDurationBlocks = uint32((1<<63 - 1) / int64(TargetBlockTime))
)

// Badge tier thresholds (score values are 0-10000, so 6000 = 60.00)
const (
	BronzeMinScore   = 6000 // 60.00
	SilverMinScore   = 7500 // 75.00
	GoldMinScore     = 8500 // 85.00
	PlatinumMinScore = 9500 // 95.00
)

// Tier benefits (in basis points)
const (
	// Routing weight multipliers (10000 = 1.0x, 11000 = 1.1x, etc.)
	BronzeRoutingMultiplierBPS   = 11000 // 1.1x
	SilverRoutingMultiplierBPS   = 12500 // 1.25x
	GoldRoutingMultiplierBPS     = 15000 // 1.5x
	PlatinumRoutingMultiplierBPS = 20000 // 2.0x

	// Insurance discounts (500 = 5%, 1500 = 15%, etc.)
	BronzeInsuranceDiscountBPS   = 500  // 5%
	SilverInsuranceDiscountBPS   = 1500 // 15%
	GoldInsuranceDiscountBPS     = 2500 // 25%
	PlatinumInsuranceDiscountBPS = 4000 // 40%

	// LAC bonus from router share (50 = 0.50%, 150 = 1.50%, etc.)
	BronzeLACBonusBPS   = 50  // 0.50%
	SilverLACBonusBPS   = 150 // 1.50%
	GoldLACBonusBPS     = 300 // 3.00%
	PlatinumLACBonusBPS = 500 // 5.00%

	// Validity periods in blocks. Assume 6-second target block time (see note on
	// DefaultEvaluationIntervalBlocks). Previously these were commented as "5s
	// blocks", which was off by 20% and would have given Bronze badges a
	// ~25-day lifetime — keeper.evaluateAndIssueBadge actually uses 6s, so the
	// block counts are already sized for 30/60/90/180 days.
	BronzeValidityBlocks   = 432000  // ~30 days at 6s blocks
	SilverValidityBlocks   = 864000  // ~60 days
	GoldValidityBlocks     = 1296000 // ~90 days
	PlatinumValidityBlocks = 2592000 // ~180 days
)

// DefaultParams returns the canonical default parameter set for the incentives module.
func DefaultParams() *Params {
	return &Params{
		EvaluationIntervalBlocks: DefaultEvaluationIntervalBlocks,
		GracePeriodBlocks:        DefaultGracePeriodBlocks,
		MinInvocationsForScoring: DefaultMinInvocationsForScoring,
		ScoringWindowBlocks:      DefaultScoringWindowBlocks,
	}
}

// NewParams constructs a Params instance with the provided values.
func NewParams(
	evaluationInterval, gracePeriod, minInvocations, scoringWindow uint32,
) *Params {
	return &Params{
		EvaluationIntervalBlocks: evaluationInterval,
		GracePeriodBlocks:        gracePeriod,
		MinInvocationsForScoring: minInvocations,
		ScoringWindowBlocks:      scoringWindow,
	}
}

// Validate performs basic stateless validation of the parameters.
func (p *Params) Validate() error {
	if p == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if p.EvaluationIntervalBlocks == 0 {
		return fmt.Errorf("evaluation interval blocks must be > 0")
	}
	if p.GracePeriodBlocks == 0 {
		return fmt.Errorf("grace period blocks must be > 0")
	}
	if p.GracePeriodBlocks > maxDurationBlocks {
		return fmt.Errorf("grace period blocks cannot exceed %d", maxDurationBlocks)
	}
	if p.MinInvocationsForScoring == 0 {
		return fmt.Errorf("min invocations for scoring must be > 0")
	}
	if p.ScoringWindowBlocks == 0 {
		return fmt.Errorf("scoring window blocks must be > 0")
	}
	return nil
}

// DefaultTierConfigs returns the default tier configurations per the lumera-verified spec.
func DefaultTierConfigs() []*TierConfig {
	return []*TierConfig{
		{
			Tier:                       BadgeTier_BADGE_TIER_BRONZE,
			Name:                       "Verified Bronze",
			MinimumScore:               uint32(BronzeMinScore),
			RoutingWeightMultiplierBps: BronzeRoutingMultiplierBPS,
			InsuranceDiscountBps:       BronzeInsuranceDiscountBPS,
			LacBonusBps:                BronzeLACBonusBPS,
			ValidityPeriodBlocks:       BronzeValidityBlocks,
			CachePriority:              false,
			FeaturedPlacement:          false,
		},
		{
			Tier:                       BadgeTier_BADGE_TIER_SILVER,
			Name:                       "Verified Silver",
			MinimumScore:               uint32(SilverMinScore),
			RoutingWeightMultiplierBps: SilverRoutingMultiplierBPS,
			InsuranceDiscountBps:       SilverInsuranceDiscountBPS,
			LacBonusBps:                SilverLACBonusBPS,
			ValidityPeriodBlocks:       SilverValidityBlocks,
			CachePriority:              true,
			FeaturedPlacement:          false,
		},
		{
			Tier:                       BadgeTier_BADGE_TIER_GOLD,
			Name:                       "Verified Gold",
			MinimumScore:               uint32(GoldMinScore),
			RoutingWeightMultiplierBps: GoldRoutingMultiplierBPS,
			InsuranceDiscountBps:       GoldInsuranceDiscountBPS,
			LacBonusBps:                GoldLACBonusBPS,
			ValidityPeriodBlocks:       GoldValidityBlocks,
			CachePriority:              true,
			FeaturedPlacement:          true,
		},
		{
			Tier:                       BadgeTier_BADGE_TIER_PLATINUM,
			Name:                       "Verified Platinum",
			MinimumScore:               uint32(PlatinumMinScore),
			RoutingWeightMultiplierBps: PlatinumRoutingMultiplierBPS,
			InsuranceDiscountBps:       PlatinumInsuranceDiscountBPS,
			LacBonusBps:                PlatinumLACBonusBPS,
			ValidityPeriodBlocks:       PlatinumValidityBlocks,
			CachePriority:              true,
			FeaturedPlacement:          true,
		},
	}
}

// ValidateTierConfig performs validation on a tier configuration.
func ValidateTierConfig(config *TierConfig) error {
	if config == nil {
		return fmt.Errorf("tier config cannot be nil")
	}
	if config.Tier == BadgeTier_BADGE_TIER_UNSPECIFIED {
		return fmt.Errorf("tier cannot be unspecified")
	}
	if config.Name == "" {
		return fmt.Errorf("tier name cannot be empty")
	}
	if config.MinimumScore > MaxScore {
		return fmt.Errorf("minimum score cannot exceed %d", MaxScore)
	}
	if config.RoutingWeightMultiplierBps < BasisPointsScale {
		return fmt.Errorf("routing weight multiplier must be at least 1.0x (10000 bps)")
	}
	if config.InsuranceDiscountBps > BasisPointsScale {
		return fmt.Errorf("insurance discount cannot exceed 100%%")
	}
	if config.LacBonusBps > BasisPointsScale {
		return fmt.Errorf("LAC bonus cannot exceed 100%%")
	}
	if config.ValidityPeriodBlocks == 0 {
		return fmt.Errorf("validity period must be > 0")
	}
	if config.ValidityPeriodBlocks > maxDurationBlocks {
		return fmt.Errorf("validity period cannot exceed %d", maxDurationBlocks)
	}
	return nil
}
