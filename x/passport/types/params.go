
package types

import (
	"fmt"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// DefaultMinStakeDenom is the default denomination for minimum stake.
	DefaultMinStakeDenom = "ulume"
	// DefaultMinStakeAmount is the default minimum stake amount (100 LUME = 100_000_000 ulume).
	DefaultMinStakeAmount = 100_000_000
	// DefaultSlashRateBPS is the default slash rate in basis points (500 = 5%).
	DefaultSlashRateBPS = 500
	// DefaultRevocationGraceSeconds is the default grace period before revocation (7 days).
	DefaultRevocationGraceSeconds = 7 * 24 * 60 * 60
	// DefaultCollusionRiskThresholdBPS is the default risk threshold for collusion downgrades (70%).
	DefaultCollusionRiskThresholdBPS = 7000
	// DefaultCollusionVerificationPenaltyBPS halves verification weight when collusion risk crosses threshold.
	DefaultCollusionVerificationPenaltyBPS = 5000
	// DefaultCollusionMaxPayerShareBPS flags single-payer concentration above 60%.
	DefaultCollusionMaxPayerShareBPS = 6000
	// DefaultCollusionMaxPublisherShareBPS flags single-publisher concentration above 60%.
	DefaultCollusionMaxPublisherShareBPS = 6000
	// DefaultCollusionMaxToolShareBPS flags single-tool concentration above 70%.
	DefaultCollusionMaxToolShareBPS = 7000
	maxBasisPoints                  = 10000
)

// DefaultParams returns the default passport module parameters.
func DefaultParams() *Params {
	return &Params{
		MinStake:                        &v1beta1.Coin{Denom: DefaultMinStakeDenom, Amount: fmt.Sprintf("%d", DefaultMinStakeAmount)},
		SlashRateBps:                    DefaultSlashRateBPS,
		RevocationGraceSeconds:          DefaultRevocationGraceSeconds,
		CollusionRiskThresholdBps:       DefaultCollusionRiskThresholdBPS,
		CollusionVerificationPenaltyBps: DefaultCollusionVerificationPenaltyBPS,
		CollusionMaxPayerShareBps:       DefaultCollusionMaxPayerShareBPS,
		CollusionMaxPublisherShareBps:   DefaultCollusionMaxPublisherShareBPS,
		CollusionMaxToolShareBps:        DefaultCollusionMaxToolShareBPS,
	}
}

// Validate performs basic validation on module parameters.
func (p *Params) Validate() error {
	if p.MinStake == nil {
		return fmt.Errorf("min_stake cannot be nil")
	}
	if p.MinStake.Denom == "" {
		return fmt.Errorf("min_stake denom cannot be empty")
	}
	// sdk.NewCoin (called from MinStakeCoin → CoinFromProto) panics on
	// a denom that's non-empty but fails format validation, so reject
	// malformed denoms in Params.Validate where they can still surface
	// as a clean governance/genesis validation error.
	if err := sdk.ValidateDenom(p.MinStake.Denom); err != nil {
		return fmt.Errorf("min_stake denom is invalid: %w", err)
	}
	// MinStake.Amount is a protobuf string; unparseable or negative
	// values cause the same downstream panic. Amount == "" is tolerated
	// as "zero" by sdkmath.NewIntFromString returning ok=false, so treat
	// unparseable amount as an explicit validation error.
	amount, ok := sdkmath.NewIntFromString(p.MinStake.Amount)
	if !ok {
		return fmt.Errorf("min_stake amount is invalid: %q", p.MinStake.Amount)
	}
	if amount.IsNegative() {
		return fmt.Errorf("min_stake amount cannot be negative: %s", amount.String())
	}
	if p.SlashRateBps > maxBasisPoints {
		return fmt.Errorf("slash_rate_bps cannot exceed %d (100%%)", maxBasisPoints)
	}
	if err := validatePositiveBPS("collusion_risk_threshold_bps", p.CollusionRiskThresholdBps); err != nil {
		return err
	}
	if err := validatePositiveBPS("collusion_verification_penalty_bps", p.CollusionVerificationPenaltyBps); err != nil {
		return err
	}
	if err := validatePositiveBPS("collusion_max_payer_share_bps", p.CollusionMaxPayerShareBps); err != nil {
		return err
	}
	if err := validatePositiveBPS("collusion_max_publisher_share_bps", p.CollusionMaxPublisherShareBps); err != nil {
		return err
	}
	if err := validatePositiveBPS("collusion_max_tool_share_bps", p.CollusionMaxToolShareBps); err != nil {
		return err
	}
	return nil
}

func validatePositiveBPS(name string, value uint32) error {
	if value == 0 {
		return fmt.Errorf("%s must be positive", name)
	}
	if value > maxBasisPoints {
		return fmt.Errorf("%s cannot exceed %d (100%%)", name, maxBasisPoints)
	}
	return nil
}

// MinStakeCoin returns the minimum stake as an sdk.Coin.
func (p *Params) MinStakeCoin() sdk.Coin {
	if p.MinStake == nil {
		return sdk.NewCoin(DefaultMinStakeDenom, sdkmath.NewInt(DefaultMinStakeAmount))
	}
	return CoinFromProto(p.MinStake)
}
