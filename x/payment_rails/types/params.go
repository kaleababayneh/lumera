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
	// DefaultCreditDenom is the coin denom used for LAC credits.
	DefaultCreditDenom    = "ulac"
	maxOracleStalenessSec = uint64(math.MaxInt64 / int64(time.Second))
	maxWithdrawDelaySec   = uint64(math.MaxInt64 / int64(time.Second))
)

func boundedSecondsDuration(field string, seconds uint64, maxSeconds uint64) (time.Duration, error) {
	if seconds > maxSeconds {
		return 0, fmt.Errorf("%s exceeds maximum safe duration seconds (%d)", field, maxSeconds)
	}
	return time.Duration(seconds) * time.Second, nil
}

// OracleStalenessDuration validates and converts oracle staleness seconds.
func OracleStalenessDuration(seconds uint64) (time.Duration, error) {
	return boundedSecondsDuration("oracle_staleness_sec", seconds, maxOracleStalenessSec)
}

// WithdrawDelayDuration validates and converts withdrawal delay seconds.
func WithdrawDelayDuration(seconds uint64) (time.Duration, error) {
	return boundedSecondsDuration("withdraw_delay_sec", seconds, maxWithdrawDelaySec)
}

// DefaultParams returns default parameters matching the Injective payment rails spec.
func DefaultParams() *Params {
	return &Params{
		CreditDenom:    DefaultCreditDenom,
		AcceptedDenoms: []string{"inj", "usdc", "usdt"},
		OraclePairs: []*DenomOraclePair{
			{Denom: "inj", AssetPair: "INJ/USD"},
			{Denom: "usdc", AssetPair: "USDC/USD"},
			{Denom: "usdt", AssetPair: "USDT/USD"},
		},
		AcqFeeBps:             30,
		MaxSlippageBps:        50,
		MaxOracleDeviationBps: 100,
		OracleTwapWindowSec:   900,
		OracleStalenessSec:    120,
		MinConfirmations:      2,
		MaxTopupsPerHour:      10,
		MaxLacPerDay:          "10000000000",
		MaxDepositLacPerAsset: "2500000000",
		VolumeSpikeBps:        500,
		WithdrawDelaySec:      0,
		PauseConversions:      false,
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
	if len(p.AcceptedDenoms) == 0 {
		return fmt.Errorf("accepted denoms cannot be empty")
	}
	pairs := map[string]string{}
	for i, pair := range p.OraclePairs {
		if pair == nil {
			return fmt.Errorf("oracle_pairs[%d] cannot be nil", i)
		}
		if err := sdk.ValidateDenom(pair.Denom); err != nil {
			return fmt.Errorf("invalid oracle denom %q: %w", pair.Denom, err)
		}
		assetPair := strings.TrimSpace(pair.AssetPair)
		if assetPair == "" {
			return fmt.Errorf("asset_pair required for denom %s", pair.Denom)
		}
		if pair.AssetPair != assetPair {
			return fmt.Errorf("asset_pair for denom %s must not have leading or trailing whitespace", pair.Denom)
		}
		denomKey := strings.ToLower(pair.Denom)
		if _, ok := pairs[denomKey]; ok {
			return fmt.Errorf("duplicate oracle pair for denom %s", pair.Denom)
		}
		pairs[denomKey] = pair.AssetPair
	}
	accepted := map[string]struct{}{}
	for _, denom := range p.AcceptedDenoms {
		if err := sdk.ValidateDenom(denom); err != nil {
			return fmt.Errorf("invalid accepted denom %q: %w", denom, err)
		}
		denomKey := strings.ToLower(denom)
		if _, ok := accepted[denomKey]; ok {
			return fmt.Errorf("duplicate accepted denom %s", denom)
		}
		accepted[denomKey] = struct{}{}
		if _, ok := pairs[denomKey]; !ok {
			return fmt.Errorf("missing oracle pair for denom %s", denom)
		}
	}
	if p.AcqFeeBps > 10_000 {
		return fmt.Errorf("acq_fee_bps exceeds 100%%")
	}
	if p.MaxSlippageBps > 10_000 {
		return fmt.Errorf("max_slippage_bps exceeds 100%%")
	}
	if p.MaxOracleDeviationBps > 10_000 {
		return fmt.Errorf("max_oracle_deviation_bps exceeds 100%%")
	}
	if p.VolumeSpikeBps > 10_000 {
		return fmt.Errorf("volume_spike_bps exceeds 100%%")
	}
	if _, err := OracleStalenessDuration(p.OracleStalenessSec); err != nil {
		return err
	}
	if _, err := WithdrawDelayDuration(p.WithdrawDelaySec); err != nil {
		return err
	}
	maxLacPerDay, ok := sdkmath.NewIntFromString(p.MaxLacPerDay)
	if !ok {
		return fmt.Errorf("invalid max_lac_per_day: %s", p.MaxLacPerDay)
	}
	if maxLacPerDay.IsNegative() {
		return fmt.Errorf("max_lac_per_day must be non-negative")
	}
	maxDepositLacPerAsset, ok := sdkmath.NewIntFromString(p.MaxDepositLacPerAsset)
	if !ok {
		return fmt.Errorf("invalid max_deposit_lac_per_asset: %s", p.MaxDepositLacPerAsset)
	}
	if maxDepositLacPerAsset.IsNegative() {
		return fmt.Errorf("max_deposit_lac_per_asset must be non-negative")
	}
	return nil
}

// FindOraclePair resolves the oracle asset pair for the given denom.
func (p *Params) FindOraclePair(denom string) (string, bool) {
	if p == nil {
		return "", false
	}
	needle := strings.ToLower(strings.TrimSpace(denom))
	for _, pair := range p.OraclePairs {
		if strings.ToLower(pair.Denom) == needle {
			return pair.AssetPair, true
		}
	}
	return "", false
}

// IsAcceptedDenom checks whether denom is allowlisted.
func (p *Params) IsAcceptedDenom(denom string) bool {
	if p == nil {
		return false
	}
	needle := strings.ToLower(strings.TrimSpace(denom))
	for _, allowed := range p.AcceptedDenoms {
		if strings.ToLower(allowed) == needle {
			return true
		}
	}
	return false
}

// MaxLacPerDayInt returns the max_lac_per_day as sdkmath.Int.
func (p *Params) MaxLacPerDayInt() sdkmath.Int {
	if p == nil || p.MaxLacPerDay == "" {
		return sdkmath.ZeroInt()
	}
	val, ok := sdkmath.NewIntFromString(p.MaxLacPerDay)
	if !ok {
		return sdkmath.ZeroInt()
	}
	return val
}

// MaxDepositLacPerAssetInt returns the max_deposit_lac_per_asset as sdkmath.Int.
func (p *Params) MaxDepositLacPerAssetInt() sdkmath.Int {
	if p == nil || p.MaxDepositLacPerAsset == "" {
		return sdkmath.ZeroInt()
	}
	val, ok := sdkmath.NewIntFromString(p.MaxDepositLacPerAsset)
	if !ok {
		return sdkmath.ZeroInt()
	}
	return val
}
