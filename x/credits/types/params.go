//go:build cosmos

// Package types holds shared types and helpers for the credits module.
//
//revive:disable:var-naming // Package name aligns with Cosmos SDK module layout.
package types

import (
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
)

const (
	// DefaultCreditDenom defines the denomination for Lumera credits (LAC).
	DefaultCreditDenom = "ulac"
	// DefaultLumeDenom defines the denomination for the LUME token.
	DefaultLumeDenom = "ulume"
	// DefaultLockTTLSeconds sets the default TTL in seconds for credit locks.
	DefaultLockTTLSeconds = 120
	// DefaultMaxLockTTLSeconds caps the maximum TTL allowed for credit locks.
	DefaultMaxLockTTLSeconds = 3600
	// DefaultSettlementGraceSeconds configures the grace period before settlement slashing.
	DefaultSettlementGraceSeconds = 300
	// DefaultMaxSettlementsPerBlock limits settlements processed per block.
	DefaultMaxSettlementsPerBlock = 100
	// DefaultMaxExpiredLocksPerBlock limits expired locks processed per block.
	DefaultMaxExpiredLocksPerBlock = 100
	// DefaultMaxPrunedSettlementsPerBlock limits pruned settlements per block.
	DefaultMaxPrunedSettlementsPerBlock = 100
	// DefaultTreasuryAddress configures the default module treasury address (empty = disabled).
	DefaultTreasuryAddress = ""
	// MinLockAmount defines the minimum acceptable amount for new credit locks.
	MinLockAmount = 1
	// DefaultBurnRateSpendBps is the default burn rate on settlement spend (3%).
	DefaultBurnRateSpendBps = 300
	// DefaultTargetAnnualDeflationBps targets 1.5% annualized burn-driven contraction.
	DefaultTargetAnnualDeflationBps = 150
	// DefaultMinBurnRateSpendBps is the minimum adaptive settlement burn rate (0.5%).
	DefaultMinBurnRateSpendBps = 50
	// DefaultMaxBurnRateSpendBps is the maximum adaptive settlement burn rate (10%).
	DefaultMaxBurnRateSpendBps = 1000
	// DefaultBurnRateAdjustmentEpoch evaluates adaptive burn every 100 blocks.
	DefaultBurnRateAdjustmentEpoch = 100
	// DefaultDeathSpiralSupplyContractionBps caps burn when 30-day contraction exceeds 3%.
	DefaultDeathSpiralSupplyContractionBps = 300
	// DefaultDeathSpiralBurnRateCapBps caps settlement burn at 1.5% during contraction stress.
	DefaultDeathSpiralBurnRateCapBps = 150
	// DefaultOverdraftMaxCreditLineToBondBps keeps overdraft disabled until governance sets a bond-backed limit.
	DefaultOverdraftMaxCreditLineToBondBps = 0
	// DefaultOverdraftLiquidationThresholdBps stays zero while overdraft is disabled.
	DefaultOverdraftLiquidationThresholdBps = 0
	// DefaultBurnRateAcqBps is the default burn rate on LUME to LAC acquisition (1%).
	DefaultBurnRateAcqBps = 100
	// DefaultInsuranceBps is the default insurance contribution on settlement spend (3%).
	DefaultInsuranceBps = 300
	// MaxBasisPoints is the canonical upper bound for percentage parameters.
	MaxBasisPoints = 10_000
	// DefaultDisputeWindowHours is the legacy credits-module override. A zero
	// value means "defer to the canonical registry dispute window".
	DefaultDisputeWindowHours = 0
	// maxDisputeWindowHours is the largest hour count that can be safely
	// represented as a time.Duration before multiplying by time.Hour.
	maxDisputeWindowHours = uint32((1<<63 - 1) / int64(time.Hour))
)

// DefaultParams returns the canonical default parameter set for the credits module.
func DefaultParams() *Params {
	return &Params{
		CreditDenom:                      DefaultCreditDenom,
		DefaultLockTtlSeconds:            DefaultLockTTLSeconds,
		MaxLockTtlSeconds:                DefaultMaxLockTTLSeconds,
		SettlementGracePeriodSeconds:     DefaultSettlementGraceSeconds,
		MaxSettlementsPerBlock:           DefaultMaxSettlementsPerBlock,
		MaxExpiredLocksPerBlock:          DefaultMaxExpiredLocksPerBlock,
		MaxPrunedSettlementsPerBlock:     DefaultMaxPrunedSettlementsPerBlock,
		TreasuryAddress:                  DefaultTreasuryAddress,
		BurnRateSpendBps:                 DefaultBurnRateSpendBps,
		TargetAnnualDeflationBps:         DefaultTargetAnnualDeflationBps,
		MinBurnRateSpendBps:              DefaultMinBurnRateSpendBps,
		MaxBurnRateSpendBps:              DefaultMaxBurnRateSpendBps,
		BurnRateAdjustmentEpoch:          DefaultBurnRateAdjustmentEpoch,
		DeathSpiralSupplyContractionBps:  DefaultDeathSpiralSupplyContractionBps,
		DeathSpiralBurnRateCapBps:        DefaultDeathSpiralBurnRateCapBps,
		OverdraftMaxCreditLineToBondBps:  DefaultOverdraftMaxCreditLineToBondBps,
		OverdraftLiquidationThresholdBps: DefaultOverdraftLiquidationThresholdBps,
		BurnRateAcqBps:                   DefaultBurnRateAcqBps,
		InsuranceBps:                     DefaultInsuranceBps,
		DisputeWindowHours:               DefaultDisputeWindowHours,
	}
}

// NewParams constructs a Params instance with the provided values.
func NewParams(
	denom string,
	defaultTTL, maxTTL, grace uint32,
	treasury string,
	maxSettlementsPerBlock, maxExpiredLocksPerBlock, maxPrunedSettlementsPerBlock uint32,
) *Params {
	params := DefaultParams()
	params.CreditDenom = denom
	params.DefaultLockTtlSeconds = defaultTTL
	params.MaxLockTtlSeconds = maxTTL
	params.SettlementGracePeriodSeconds = grace
	params.MaxSettlementsPerBlock = maxSettlementsPerBlock
	params.MaxExpiredLocksPerBlock = maxExpiredLocksPerBlock
	params.MaxPrunedSettlementsPerBlock = maxPrunedSettlementsPerBlock
	params.TreasuryAddress = treasury
	return params
}

// Validate performs basic stateless validation of the parameters.
func (p *Params) Validate() error {
	if p == nil {
		return fmt.Errorf("params cannot be nil")
	}
	if err := sdk.ValidateDenom(p.CreditDenom); err != nil {
		return fmt.Errorf("invalid credit denom: %w", err)
	}
	if p.DefaultLockTtlSeconds == 0 {
		return fmt.Errorf("default lock ttl must be > 0")
	}
	if p.MaxLockTtlSeconds == 0 {
		return fmt.Errorf("max lock ttl must be > 0")
	}
	if p.DefaultLockTtlSeconds > p.MaxLockTtlSeconds {
		return fmt.Errorf("default lock ttl cannot exceed max lock ttl")
	}
	if p.SettlementGracePeriodSeconds == 0 {
		return fmt.Errorf("settlement grace period must be > 0")
	}
	if p.MaxSettlementsPerBlock == 0 {
		return fmt.Errorf("max settlements per block must be > 0")
	}
	if p.MaxExpiredLocksPerBlock == 0 {
		return fmt.Errorf("max expired locks per block must be > 0")
	}
	if p.MaxPrunedSettlementsPerBlock == 0 {
		return fmt.Errorf("max pruned settlements per block must be > 0")
	}
	if p.TreasuryAddress != "" {
		if _, err := sdk.AccAddressFromBech32(p.TreasuryAddress); err != nil {
			return fmt.Errorf("invalid treasury address: %w", err)
		}
	}
	if p.BurnRateSpendBps > MaxBasisPoints {
		return fmt.Errorf("burn rate spend bps cannot exceed %d", MaxBasisPoints)
	}
	if p.TargetAnnualDeflationBps > MaxBasisPoints {
		return fmt.Errorf("target annual deflation bps cannot exceed %d", MaxBasisPoints)
	}
	if p.MinBurnRateSpendBps > MaxBasisPoints {
		return fmt.Errorf("min burn rate spend bps cannot exceed %d", MaxBasisPoints)
	}
	if p.MaxBurnRateSpendBps > MaxBasisPoints {
		return fmt.Errorf("max burn rate spend bps cannot exceed %d", MaxBasisPoints)
	}
	if p.MinBurnRateSpendBps > p.MaxBurnRateSpendBps {
		return fmt.Errorf("min burn rate spend bps cannot exceed max burn rate spend bps")
	}
	if p.BurnRateAdjustmentEpoch > 0 {
		if p.BurnRateSpendBps < p.MinBurnRateSpendBps {
			return fmt.Errorf("burn rate spend bps cannot be below min burn rate spend bps")
		}
		if p.BurnRateSpendBps > p.MaxBurnRateSpendBps {
			return fmt.Errorf("burn rate spend bps cannot exceed max burn rate spend bps")
		}
	}
	if p.DeathSpiralSupplyContractionBps > MaxBasisPoints {
		return fmt.Errorf("death spiral supply contraction bps cannot exceed %d", MaxBasisPoints)
	}
	if p.DeathSpiralBurnRateCapBps > MaxBasisPoints {
		return fmt.Errorf("death spiral burn rate cap bps cannot exceed %d", MaxBasisPoints)
	}
	if p.DeathSpiralBurnRateCapBps < p.MinBurnRateSpendBps {
		return fmt.Errorf("death spiral burn rate cap bps cannot be below min burn rate spend bps")
	}
	if p.DeathSpiralBurnRateCapBps > p.MaxBurnRateSpendBps {
		return fmt.Errorf("death spiral burn rate cap bps cannot exceed max burn rate spend bps")
	}
	if p.BurnRateAcqBps > MaxBasisPoints {
		return fmt.Errorf("burn rate acq bps cannot exceed %d", MaxBasisPoints)
	}
	if p.InsuranceBps > MaxBasisPoints {
		return fmt.Errorf("insurance bps cannot exceed %d", MaxBasisPoints)
	}
	if uint64(p.BurnRateSpendBps)+uint64(p.InsuranceBps) > MaxBasisPoints {
		return fmt.Errorf("burn rate spend bps + insurance bps cannot exceed %d", MaxBasisPoints)
	}
	if p.DisputeWindowHours > maxDisputeWindowHours {
		return fmt.Errorf("dispute window hours cannot exceed %d", maxDisputeWindowHours)
	}
	if p.OverdraftMaxCreditLineToBondBps > MaxBasisPoints {
		return fmt.Errorf("overdraft max credit line to bond bps cannot exceed %d", MaxBasisPoints)
	}
	if p.OverdraftLiquidationThresholdBps > MaxBasisPoints {
		return fmt.Errorf("overdraft liquidation threshold bps cannot exceed %d", MaxBasisPoints)
	}
	if p.OverdraftMaxCreditLineToBondBps == 0 {
		if p.OverdraftLiquidationThresholdBps != 0 {
			return fmt.Errorf("overdraft liquidation threshold bps requires overdraft max credit line to bond bps")
		}
	} else if p.OverdraftLiquidationThresholdBps == 0 {
		return fmt.Errorf("overdraft liquidation threshold bps must be positive when overdraft max credit line to bond bps is positive")
	}
	return nil
}

// DefaultDisputeWindowDuration returns the canonical dispute window used when
// credits params do not provide an explicit legacy-hour override.
func DefaultDisputeWindowDuration() time.Duration {
	return time.Duration(registrytypes.DefaultDisputeWindowSeconds) * time.Second
}

// DisputeWindowDuration resolves the effective dispute window for credits.
// A zero DisputeWindowHours value defers to the registry's canonical seconds.
func DisputeWindowDuration(p *Params) time.Duration {
	if p != nil && p.DisputeWindowHours > 0 {
		if p.DisputeWindowHours > maxDisputeWindowHours {
			return DefaultDisputeWindowDuration()
		}
		return time.Duration(p.DisputeWindowHours) * time.Hour
	}
	return DefaultDisputeWindowDuration()
}

// LockTTL returns a sanitized lock TTL duration derived from parameters.
func (p *Params) LockTTL(requested time.Duration) time.Duration {
	if p == nil {
		return time.Duration(DefaultLockTTLSeconds) * time.Second
	}
	maxTTL := time.Duration(p.MaxLockTtlSeconds) * time.Second
	if maxTTL <= 0 {
		maxTTL = time.Duration(DefaultMaxLockTTLSeconds) * time.Second
	}
	if requested <= 0 {
		requested = time.Duration(p.DefaultLockTtlSeconds) * time.Second
	}
	if requested <= 0 {
		requested = time.Duration(DefaultLockTTLSeconds) * time.Second
	}
	if requested > maxTTL {
		requested = maxTTL
	}
	return requested
}
